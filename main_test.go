package main

import (
	"testing"
	"time"

	"mean-reversion-candidate/internal/config"
)

func TestLiveRangeUsesLatestCompletedMinute(t *testing.T) {
	tz := config.MustLocation("America/New_York")
	app := &App{
		tz: tz,
		cfg: config.Config{
			Live: config.LiveConfig{StartTime: "04:00", EndTime: "20:00"},
		},
	}

	now := time.Date(2026, 6, 17, 10, 15, 3, 456_000_000, tz)
	from, to, open := app.liveRange(now)
	if !open {
		t.Fatal("liveRange returned closed during live window")
	}
	if got, want := from.Format("15:04:05"), "04:00:00"; got != want {
		t.Fatalf("from = %s, want %s", got, want)
	}
	if got, want := to.Format("15:04:05"), "10:15:00"; got != want {
		t.Fatalf("to = %s, want %s", got, want)
	}
}

func TestLiveRangeAllowsDelayedFinalMinute(t *testing.T) {
	tz := config.MustLocation("America/New_York")
	app := &App{
		tz: tz,
		cfg: config.Config{
			Live: config.LiveConfig{StartTime: "04:00", EndTime: "20:00"},
		},
	}

	_, to, open := app.liveRange(time.Date(2026, 6, 17, 20, 0, 3, 0, tz))
	if !open {
		t.Fatal("liveRange should allow the final configured minute after provider delay")
	}
	if got, want := to.Format("15:04:05"), "20:00:00"; got != want {
		t.Fatalf("to = %s, want %s", got, want)
	}

	_, _, open = app.liveRange(time.Date(2026, 6, 17, 20, 1, 3, 0, tz))
	if open {
		t.Fatal("liveRange should be closed after the final delayed minute")
	}
}

func TestNextLiveRefreshDelayTargetsPostMinuteDelay(t *testing.T) {
	now := time.Date(2026, 6, 17, 10, 15, 1, 500_000_000, time.UTC)
	delay := nextLiveRefreshDelay(now, time.Minute, 3*time.Second)
	if delay != 1500*time.Millisecond {
		t.Fatalf("delay = %s, want 1.5s", delay)
	}

	now = time.Date(2026, 6, 17, 10, 15, 3, 0, time.UTC)
	delay = nextLiveRefreshDelay(now, time.Minute, 3*time.Second)
	if delay != time.Minute {
		t.Fatalf("delay = %s, want 1m", delay)
	}
}
