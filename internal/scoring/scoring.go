package scoring

import (
	"math"
	"sort"
	"strconv"
	"time"

	"mean-reversion-candidate/internal/bars"
)

type Config struct {
	LookbackMinutes      int
	RangeLookbackMinutes int
	ATRPeriod            int
	MinDollarVolume      float64
	ExcellentScore       float64
	GoodScore            float64
}

type Result struct {
	Rank             int        `json:"rank"`
	Symbol           string     `json:"symbol"`
	Side             string     `json:"side"`
	Score            float64    `json:"score"`
	Grade            string     `json:"grade"`
	Price            float64    `json:"price"`
	VWAP             float64    `json:"vwap"`
	MoveFromVWAPPct  float64    `json:"move_from_vwap_pct"`
	Return30mPct     float64    `json:"return_30m_pct"`
	ZScore30m        float64    `json:"z_score_30m"`
	RangePositionPct float64    `json:"range_position_pct"`
	DollarVolume     float64    `json:"dollar_volume"`
	ATR14            float64    `json:"atr_14"`
	ATRPercent       float64    `json:"atr_percent"`
	DayMoveATR       float64    `json:"day_move_atr"`
	VWAPStretchATR   float64    `json:"vwap_stretch_atr"`
	LastUpdated      time.Time  `json:"last_updated"`
	Reason           string     `json:"reason"`
	Components       Components `json:"components"`
	ChartURL         string     `json:"chart_url"`
}

type Components struct {
	VWAPExtreme        float64 `json:"vwap_extreme"`
	StatisticalMove    float64 `json:"statistical_move"`
	DailyATRMove       float64 `json:"daily_atr_move"`
	RangeExtension     float64 `json:"range_extension"`
	VolumeConfirmation float64 `json:"volume_confirmation"`
	ReversalEvidence   float64 `json:"reversal_evidence"`
	TrendPenalty       float64 `json:"trend_penalty"`
}

func Rank(all map[string][]bars.Bar, daily map[string][]bars.Bar, cfg Config, asOf time.Time) []Result {
	results := make([]Result, 0, len(all))
	for symbol, series := range all {
		result := Score(symbol, series, daily[symbol], cfg, asOf)
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Symbol < results[j].Symbol
	})
	for i := range results {
		results[i].Rank = i + 1
	}
	return results
}

func Score(symbol string, series, daily []bars.Bar, cfg Config, asOf time.Time) Result {
	series = through(series, asOf)
	if len(series) == 0 {
		return Result{Symbol: symbol, Side: "No data", Grade: "N/A", Reason: "No minute candles available at this time"}
	}
	sort.Slice(series, func(i, j int) bool { return series[i].End.Before(series[j].End) })

	last := series[len(series)-1]
	vwap := sessionVWAP(series)
	dollarVolume := sessionDollarVolume(series)
	lookback := clamp(cfg.LookbackMinutes, 5, len(series)-1)
	rangeLookback := clamp(cfg.RangeLookbackMinutes, 5, len(series))
	ret30 := pctChange(series[len(series)-1-lookback].Close, last.Close)
	minuteReturns := returns(series)
	retVol := stddev(tail(minuteReturns, max(lookback, 10))) * math.Sqrt(float64(max(lookback, 1)))
	z30 := 0.0
	if retVol > 0 {
		z30 = ret30 / retVol
	}
	vwapPct := pctChange(vwap, last.Close)
	atr, priorClose := atrAndPriorClose(daily, max(cfg.ATRPeriod, 1))
	dayMoveATR := ratio(last.Close-priorClose, atr)
	vwapStretchATR := ratio(last.Close-vwap, atr)
	atrPct := pctChange(priorClose, priorClose+atr)
	rangePos := rangePosition(tailBars(series, rangeLookback), last.Close)
	side, direction := sideFromStretch(vwapPct, z30, dayMoveATR)

	vwapExtreme := 25 * cappedAbs(abs(vwapPct)/0.035)
	statMove := 20 * cappedAbs(abs(z30)/2.5)
	dailyATRMove := 20 * cappedAbs(abs(dayMoveATR)/1.5)
	rangeExtension := 12 * rangeExtensionComponent(rangePos, direction)
	volConfirm := 13 * cappedAbs(dollarVolume/math.Max(cfg.MinDollarVolume, 1))
	reversal := 10 * reversalEvidence(series, direction)
	trendPenalty := 15 * trendPersistencePenalty(tail(minuteReturns, 6), direction)
	score := clampFloat(vwapExtreme+statMove+dailyATRMove+rangeExtension+volConfirm+reversal-trendPenalty, 0, 100)

	result := Result{
		Symbol:           symbol,
		Side:             side,
		Score:            round(score, 1),
		Grade:            grade(score, cfg),
		Price:            round(last.Close, 4),
		VWAP:             round(vwap, 4),
		MoveFromVWAPPct:  round(vwapPct*100, 2),
		Return30mPct:     round(ret30*100, 2),
		ZScore30m:        round(z30, 2),
		RangePositionPct: round(rangePos*100, 1),
		DollarVolume:     round(dollarVolume, 0),
		ATR14:            round(atr, 4),
		ATRPercent:       round(atrPct*100, 2),
		DayMoveATR:       round(dayMoveATR, 2),
		VWAPStretchATR:   round(vwapStretchATR, 2),
		LastUpdated:      last.End,
		Components: Components{
			VWAPExtreme:        round(vwapExtreme, 1),
			StatisticalMove:    round(statMove, 1),
			DailyATRMove:       round(dailyATRMove, 1),
			RangeExtension:     round(rangeExtension, 1),
			VolumeConfirmation: round(volConfirm, 1),
			ReversalEvidence:   round(reversal, 1),
			TrendPenalty:       round(trendPenalty, 1),
		},
	}
	result.Reason = reason(result)
	return result
}

func through(series []bars.Bar, asOf time.Time) []bars.Bar {
	if asOf.IsZero() {
		return series
	}
	out := series[:0]
	for _, bar := range series {
		if !bar.End.After(asOf) {
			out = append(out, bar)
		}
	}
	return out
}

func sessionVWAP(series []bars.Bar) float64 {
	var pv, vol float64
	for _, bar := range series {
		price := bar.VWAP
		if price <= 0 {
			price = bar.TypicalPrice()
		}
		pv += price * bar.Volume
		vol += bar.Volume
	}
	if vol == 0 {
		return series[len(series)-1].Close
	}
	return pv / vol
}

func sessionDollarVolume(series []bars.Bar) float64 {
	var total float64
	for _, bar := range series {
		total += bar.Close * bar.Volume
	}
	return total
}

func returns(series []bars.Bar) []float64 {
	if len(series) < 2 {
		return nil
	}
	out := make([]float64, 0, len(series)-1)
	for i := 1; i < len(series); i++ {
		out = append(out, pctChange(series[i-1].Close, series[i].Close))
	}
	return out
}

func rangePosition(series []bars.Bar, price float64) float64 {
	if len(series) == 0 {
		return 0.5
	}
	low := math.Inf(1)
	high := math.Inf(-1)
	for _, bar := range series {
		low = math.Min(low, bar.Low)
		high = math.Max(high, bar.High)
	}
	if high <= low {
		return 0.5
	}
	return clampFloat((price-low)/(high-low), 0, 1)
}

func sideFromStretch(vwapPct, z30, dayMoveATR float64) (string, float64) {
	stretches := []float64{
		ratio(vwapPct, 0.035),
		ratio(z30, 2.5),
		ratio(dayMoveATR, 1.5),
	}
	stretch := stretches[0]
	for _, candidate := range stretches[1:] {
		if math.Abs(candidate) > math.Abs(stretch) {
			stretch = candidate
		}
	}
	switch {
	case stretch < 0:
		return "Long bounce", -1
	case stretch > 0:
		return "Short fade", 1
	default:
		return "Neutral", 0
	}
}

func rangeExtensionComponent(rangePos, direction float64) float64 {
	switch {
	case direction < 0:
		return cappedAbs((0.50 - rangePos) / 0.50)
	case direction > 0:
		return cappedAbs((rangePos - 0.50) / 0.50)
	default:
		return 0
	}
}

func reversalEvidence(series []bars.Bar, direction float64) float64 {
	if len(series) == 0 || direction == 0 {
		return 0
	}
	last := series[len(series)-1]
	span := last.High - last.Low
	if span <= 0 {
		return 0
	}
	closeLocation := (last.Close - last.Low) / span
	if direction < 0 {
		return cappedAbs(closeLocation)
	}
	return cappedAbs(1 - closeLocation)
}

func trendPersistencePenalty(returns []float64, direction float64) float64 {
	if len(returns) == 0 || direction == 0 {
		return 0
	}
	var same int
	for _, ret := range returns {
		if (direction < 0 && ret < 0) || (direction > 0 && ret > 0) {
			same++
		}
	}
	return float64(same) / float64(len(returns))
}

func reason(r Result) string {
	atrPart := "ATR n/a"
	if r.ATR14 > 0 {
		atrPart = fmtFloat(r.DayMoveATR) + " ATR day move"
	}
	return fmtPercent(r.MoveFromVWAPPct) + " from VWAP, " +
		fmtFloat(r.ZScore30m) + "z 30m move, " +
		atrPart + ", " +
		fmtPercent(r.RangePositionPct) + " of 60m range"
}

func atrAndPriorClose(daily []bars.Bar, period int) (float64, float64) {
	if len(daily) < 2 || period <= 0 {
		return 0, 0
	}
	sort.Slice(daily, func(i, j int) bool { return daily[i].End.Before(daily[j].End) })
	priorClose := daily[len(daily)-1].Close
	start := max(1, len(daily)-period)
	var sum float64
	var count int
	for i := start; i < len(daily); i++ {
		prevClose := daily[i-1].Close
		tr := math.Max(
			daily[i].High-daily[i].Low,
			math.Max(math.Abs(daily[i].High-prevClose), math.Abs(daily[i].Low-prevClose)),
		)
		if tr > 0 {
			sum += tr
			count++
		}
	}
	if count == 0 {
		return 0, priorClose
	}
	return sum / float64(count), priorClose
}

func pctChange(from, to float64) float64 {
	if from == 0 {
		return 0
	}
	return (to - from) / from
}

func ratio(numerator, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}

func stddev(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var mean float64
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))
	var ss float64
	for _, v := range values {
		d := v - mean
		ss += d * d
	}
	return math.Sqrt(ss / float64(len(values)))
}

func tail(values []float64, n int) []float64 {
	if n >= len(values) {
		return values
	}
	return values[len(values)-n:]
}

func tailBars(values []bars.Bar, n int) []bars.Bar {
	if n >= len(values) {
		return values
	}
	return values[len(values)-n:]
}

func clamp(v, minV, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func clampFloat(v, minV, maxV float64) float64 {
	return math.Max(minV, math.Min(maxV, v))
}

func cappedAbs(v float64) float64 {
	return clampFloat(math.Abs(v), 0, 1)
}

func abs(v float64) float64 {
	return math.Abs(v)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func grade(score float64, cfg Config) string {
	switch {
	case score >= cfg.ExcellentScore:
		return "A"
	case score >= cfg.GoodScore:
		return "B"
	case score >= 40:
		return "C"
	default:
		return "D"
	}
}

func round(v float64, places int) float64 {
	pow := math.Pow10(places)
	return math.Round(v*pow) / pow
}

func fmtPercent(v float64) string {
	if v > 0 {
		return "+" + fmtFloat(v) + "%"
	}
	return fmtFloat(v) + "%"
}

func fmtFloat(v float64) string {
	return strconvFormat(v)
}

func strconvFormat(v float64) string {
	return strconv.FormatFloat(round(v, 2), 'f', -1, 64)
}
