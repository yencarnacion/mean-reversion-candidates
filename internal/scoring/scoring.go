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
	SessionVolume    float64    `json:"session_volume"`
	ATR14            float64    `json:"atr_14"`
	ATRPercent       float64    `json:"atr_percent"`
	DayMoveATR       float64    `json:"day_move_atr"`
	VWAPStretchATR   float64    `json:"vwap_stretch_atr"`
	Pivots           Pivots     `json:"pivots"`
	LastUpdated      time.Time  `json:"last_updated"`
	Reason           string     `json:"reason"`
	Components       Components `json:"components"`
	ChartURL         string     `json:"chart_url"`
	MiniChart        MiniChart  `json:"mini_chart"`
}

type Pivots struct {
	PP                float64 `json:"pp"`
	R1                float64 `json:"r1"`
	R2                float64 `json:"r2"`
	R3                float64 `json:"r3"`
	S1                float64 `json:"s1"`
	S2                float64 `json:"s2"`
	S3                float64 `json:"s3"`
	Nearest           string  `json:"nearest"`
	DistancePct       float64 `json:"distance_pct"`
	DistanceATR       float64 `json:"distance_atr"`
	DirectionalSignal float64 `json:"directional_signal"`
}

type MiniChart struct {
	PriorClose   float64      `json:"prior_close"`
	SessionStart time.Time    `json:"session_start"`
	SessionEnd   time.Time    `json:"session_end"`
	Points       []ChartPoint `json:"points"`
}

type ChartPoint struct {
	Time  time.Time `json:"time"`
	Price float64   `json:"price"`
	VWAP  float64   `json:"vwap"`
}

type Components struct {
	VWAPExtreme        float64 `json:"vwap_extreme"`
	PivotExtension     float64 `json:"pivot_extension"`
	StatisticalMove    float64 `json:"statistical_move"`
	DailyATRMove       float64 `json:"daily_atr_move"`
	RangeExtension     float64 `json:"range_extension"`
	VolumeConfirmation float64 `json:"volume_confirmation"`
	ReversalEvidence   float64 `json:"reversal_evidence"`
	TrendPenalty       float64 `json:"trend_penalty"`
}

func Rank(all map[string][]bars.Bar, daily map[string][]bars.Bar, priorRegular map[string][]bars.Bar, cfg Config, asOf time.Time) []Result {
	results := make([]Result, 0, len(all))
	for symbol, series := range all {
		result := Score(symbol, series, daily[symbol], priorRegular[symbol], cfg, asOf)
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

func Score(symbol string, series, daily, priorRegular []bars.Bar, cfg Config, asOf time.Time) Result {
	series = through(series, asOf)
	if len(series) == 0 {
		return Result{Symbol: symbol, Side: "No data", Grade: "N/A", Reason: "No minute candles available at this time"}
	}
	sort.Slice(series, func(i, j int) bool { return series[i].End.Before(series[j].End) })

	last := series[len(series)-1]
	vwap := sessionVWAP(series)
	dollarVolume, sessionVolume := sessionDollarVolume(series)
	lookback := clamp(cfg.LookbackMinutes, 1, len(series)-1)
	rangeLookback := clamp(cfg.RangeLookbackMinutes, 1, len(series))
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
	pivots := pivotLevels(priorRegular, last.Close, atr)
	side, direction := sideFromStretch(pivots.DirectionalSignal, vwapPct, z30, dayMoveATR, rangePos)

	vwapExtreme := 20 * cappedAbs(abs(vwapPct)/0.035)
	pivotExtension := 22 * cappedAbs(abs(pivots.DirectionalSignal))
	statMove := 16 * cappedAbs(abs(z30)/2.5)
	dailyATRMove := 16 * cappedAbs(abs(dayMoveATR)/1.5)
	rangeExtension := 10 * rangeExtensionComponent(rangePos, direction)
	volConfirm := 8 * cappedAbs(dollarVolume/math.Max(cfg.MinDollarVolume, 1))
	reversal := 8 * reversalEvidence(series, direction)
	trendPenalty := 12 * trendPersistencePenalty(tail(minuteReturns, 6), direction)
	agreement := setupAgreement(direction, pivots.DirectionalSignal, vwapPct, z30, dayMoveATR)
	score := clampFloat((vwapExtreme+pivotExtension+statMove+dailyATRMove+rangeExtension+volConfirm+reversal)*agreement-trendPenalty, 0, 100)

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
		SessionVolume:    round(sessionVolume, 0),
		ATR14:            round(atr, 4),
		ATRPercent:       round(atrPct*100, 2),
		DayMoveATR:       round(dayMoveATR, 2),
		VWAPStretchATR:   round(vwapStretchATR, 2),
		Pivots:           roundPivots(pivots),
		LastUpdated:      last.End,
		Components: Components{
			VWAPExtreme:        round(vwapExtreme, 1),
			PivotExtension:     round(pivotExtension, 1),
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

func sessionDollarVolume(series []bars.Bar) (float64, float64) {
	var dollars, volume float64
	for _, bar := range series {
		dollars += bar.Close * bar.Volume
		volume += bar.Volume
	}
	return dollars, volume
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

func sideFromStretch(pivotSignal, vwapPct, z30, dayMoveATR, rangePos float64) (string, float64) {
	stretch := pivotSignal*1.35 +
		ratio(vwapPct, 0.035)*0.95 +
		ratio(z30, 2.5)*0.80 +
		ratio(dayMoveATR, 1.5)*0.85 +
		(rangePos-0.50)*0.70
	switch {
	case stretch < -0.12:
		return "Long bounce", -1
	case stretch > 0.12:
		return "Short fade", 1
	default:
		return "Neutral", 0
	}
}

func pivotLevels(priorRegular []bars.Bar, price, atr float64) Pivots {
	if len(priorRegular) == 0 {
		return Pivots{}
	}
	sort.Slice(priorRegular, func(i, j int) bool { return priorRegular[i].End.Before(priorRegular[j].End) })
	high := math.Inf(-1)
	low := math.Inf(1)
	close := 0.0
	for _, bar := range priorRegular {
		if bar.High > 0 {
			high = math.Max(high, bar.High)
		}
		if bar.Low > 0 {
			low = math.Min(low, bar.Low)
		}
		if bar.Close > 0 {
			close = bar.Close
		}
	}
	if close <= 0 || !isFinitePrice(high) || !isFinitePrice(low) || high <= low {
		return Pivots{}
	}

	pp := (high + low + close) / 3
	r1 := 2*pp - low
	s1 := 2*pp - high
	r2 := pp + (high - low)
	s2 := pp - (high - low)
	r3 := high + 2*(pp-low)
	s3 := low - 2*(high-pp)
	p := Pivots{PP: pp, R1: r1, R2: r2, R3: r3, S1: s1, S2: s2, S3: s3}
	p.Nearest, p.DistancePct, p.DistanceATR = nearestPivot(p, price, atr)
	p.DirectionalSignal = pivotDirectionalSignal(p, price, atr)
	return p
}

func pivotDirectionalSignal(p Pivots, price, atr float64) float64 {
	if p.PP <= 0 || price <= 0 {
		return 0
	}
	width := pivotWidth(p)
	if atr > 0 {
		width = math.Max(width, atr*0.45)
	}
	if width <= 0 {
		width = math.Max(p.PP*0.005, 0.01)
	}

	var signal float64
	switch {
	case price >= p.R3:
		signal = 1.30 + cappedAbs((price-p.R3)/width)*0.20
	case price >= p.R2:
		signal = 0.95 + cappedAbs((price-p.R2)/math.Max(p.R3-p.R2, width))*0.25
	case price >= p.R1:
		signal = 0.58 + cappedAbs((price-p.R1)/math.Max(p.R2-p.R1, width))*0.25
	case price >= p.PP:
		signal = cappedAbs((price-p.PP)/math.Max(p.R1-p.PP, width)) * 0.30
	case price <= p.S3:
		signal = -1.30 - cappedAbs((p.S3-price)/width)*0.20
	case price <= p.S2:
		signal = -0.95 - cappedAbs((p.S2-price)/math.Max(p.S2-p.S3, width))*0.25
	case price <= p.S1:
		signal = -0.58 - cappedAbs((p.S1-price)/math.Max(p.S1-p.S2, width))*0.25
	default:
		signal = -cappedAbs((p.PP-price)/math.Max(p.PP-p.S1, width)) * 0.30
	}
	return clampFloat(signal, -1.5, 1.5)
}

func nearestPivot(p Pivots, price, atr float64) (string, float64, float64) {
	levels := []struct {
		name  string
		value float64
	}{
		{"R3", p.R3},
		{"R2", p.R2},
		{"R1", p.R1},
		{"PP", p.PP},
		{"S1", p.S1},
		{"S2", p.S2},
		{"S3", p.S3},
	}
	name := ""
	dist := math.Inf(1)
	for _, level := range levels {
		if level.value <= 0 {
			continue
		}
		candidate := math.Abs(price - level.value)
		if candidate < dist {
			name = level.name
			dist = candidate
		}
	}
	if name == "" || !isFinitePrice(dist) {
		return "", 0, 0
	}
	return name, ratio(dist, price) * 100, ratio(dist, atr)
}

func pivotWidth(p Pivots) float64 {
	levels := []float64{p.S3, p.S2, p.S1, p.PP, p.R1, p.R2, p.R3}
	var sum float64
	var count int
	for i := 1; i < len(levels); i++ {
		if levels[i] > levels[i-1] {
			sum += levels[i] - levels[i-1]
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func setupAgreement(direction, pivotSignal, vwapPct, z30, dayMoveATR float64) float64 {
	if direction == 0 {
		return 0.25
	}
	inputs := []float64{
		pivotSignal,
		ratio(vwapPct, 0.035),
		ratio(z30, 2.5),
		ratio(dayMoveATR, 1.5),
	}
	var agree, oppose int
	for _, input := range inputs {
		switch {
		case direction > 0 && input > 0.15, direction < 0 && input < -0.15:
			agree++
		case direction > 0 && input < -0.15, direction < 0 && input > 0.15:
			oppose++
		}
	}
	return clampFloat(0.82+float64(agree)*0.06-float64(oppose)*0.09, 0.62, 1.08)
}

func roundPivots(p Pivots) Pivots {
	return Pivots{
		PP:                round(p.PP, 4),
		R1:                round(p.R1, 4),
		R2:                round(p.R2, 4),
		R3:                round(p.R3, 4),
		S1:                round(p.S1, 4),
		S2:                round(p.S2, 4),
		S3:                round(p.S3, 4),
		Nearest:           p.Nearest,
		DistancePct:       round(p.DistancePct, 2),
		DistanceATR:       round(p.DistanceATR, 2),
		DirectionalSignal: round(p.DirectionalSignal, 2),
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
	pivotPart := "pivot n/a"
	if r.Pivots.Nearest != "" {
		pivotPart = r.Pivots.Nearest + " nearest pivot"
	}
	return fmtPercent(r.MoveFromVWAPPct) + " from VWAP, " +
		pivotPart + ", " +
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
	if maxV < minV {
		return maxV
	}
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

func isFinitePrice(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
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
