package scoring

import (
	"testing"
	"time"

	"mean-reversion-candidate/internal/bars"
)

func TestScoreRanksDownsideExtensionAsLongBounce(t *testing.T) {
	start := time.Date(2026, 5, 29, 13, 30, 0, 0, time.UTC)
	series := make([]bars.Bar, 0, 40)
	price := 100.0
	for i := 0; i < 40; i++ {
		if i > 25 {
			price -= 0.45
		}
		series = append(series, bars.Bar{
			Symbol: "TEST",
			Open:   price + 0.05,
			High:   price + 0.20,
			Low:    price - 0.25,
			Close:  price,
			Volume: 100000,
			Start:  start.Add(time.Duration(i) * time.Minute),
			End:    start.Add(time.Duration(i+1) * time.Minute),
		})
	}

	result := Score("TEST", series, nil, Config{LookbackMinutes: 30, RangeLookbackMinutes: 60, ATRPeriod: 14, MinDollarVolume: 1000000, ExcellentScore: 75, GoodScore: 60}, time.Time{})
	if result.Side != "Long bounce" {
		t.Fatalf("side = %q, want Long bounce", result.Side)
	}
	if result.Score <= 0 {
		t.Fatalf("score = %v, want positive", result.Score)
	}
}

func TestScoreIncludesATRMove(t *testing.T) {
	start := time.Date(2026, 5, 1, 13, 30, 0, 0, time.UTC)
	daily := make([]bars.Bar, 0, 16)
	for i := 0; i < 16; i++ {
		close := 100.0 + float64(i)*0.1
		daily = append(daily, bars.Bar{
			Symbol: "TEST",
			High:   close + 2,
			Low:    close - 2,
			Close:  close,
			Start:  start.AddDate(0, 0, i-20),
			End:    start.AddDate(0, 0, i-19),
		})
	}
	series := []bars.Bar{
		{Symbol: "TEST", Open: 100, High: 106, Low: 99, Close: 106, Volume: 100000, Start: start, End: start.Add(time.Minute)},
		{Symbol: "TEST", Open: 106, High: 107, Low: 105, Close: 107, Volume: 100000, Start: start.Add(time.Minute), End: start.Add(2 * time.Minute)},
		{Symbol: "TEST", Open: 107, High: 108, Low: 106, Close: 108, Volume: 100000, Start: start.Add(2 * time.Minute), End: start.Add(3 * time.Minute)},
		{Symbol: "TEST", Open: 108, High: 109, Low: 107, Close: 109, Volume: 100000, Start: start.Add(3 * time.Minute), End: start.Add(4 * time.Minute)},
		{Symbol: "TEST", Open: 109, High: 110, Low: 108, Close: 110, Volume: 100000, Start: start.Add(4 * time.Minute), End: start.Add(5 * time.Minute)},
		{Symbol: "TEST", Open: 110, High: 111, Low: 109, Close: 111, Volume: 100000, Start: start.Add(5 * time.Minute), End: start.Add(6 * time.Minute)},
	}

	result := Score("TEST", series, daily, Config{LookbackMinutes: 5, RangeLookbackMinutes: 5, ATRPeriod: 14, MinDollarVolume: 1000000, ExcellentScore: 75, GoodScore: 60}, time.Time{})
	if result.ATR14 <= 0 {
		t.Fatalf("ATR14 = %v, want positive", result.ATR14)
	}
	if result.DayMoveATR <= 0 {
		t.Fatalf("DayMoveATR = %v, want positive", result.DayMoveATR)
	}
	if result.Components.DailyATRMove <= 0 {
		t.Fatalf("DailyATRMove component = %v, want positive", result.Components.DailyATRMove)
	}
}
