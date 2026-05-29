package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"mean-reversion-candidate/internal/bars"
	"mean-reversion-candidate/internal/config"
	"mean-reversion-candidate/internal/massive"
	"mean-reversion-candidate/internal/scoring"
	"mean-reversion-candidate/internal/watchlist"

	"github.com/joho/godotenv"
)

type App struct {
	log     *slog.Logger
	cfg     config.Config
	tz      *time.Location
	client  *massive.Client
	symbols []string

	mu      sync.RWMutex
	current Snapshot
}

type Snapshot struct {
	Mode       string           `json:"mode"`
	Status     string           `json:"status"`
	AsOf       time.Time        `json:"as_of"`
	UpdatedAt  time.Time        `json:"updated_at"`
	Symbols    int              `json:"symbols"`
	Rankings   []scoring.Result `json:"rankings"`
	Errors     []string         `json:"errors,omitempty"`
	Formula    Formula          `json:"formula"`
	MarketOpen bool             `json:"market_open"`
}

type Formula struct {
	Summary    string            `json:"summary"`
	Components map[string]string `json:"components"`
}

func main() {
	_ = godotenv.Load()

	var configPath string
	flag.StringVar(&configPath, "config", "config.yaml", "path to config.yaml")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		panic(err)
	}
	apiKey := config.APIKeyFromEnv()

	logger := newLogger(cfg.Logging.Level)
	tz := config.MustLocation(cfg.Timezone)
	items, err := watchlist.LoadCSV(cfg.InputCSV)
	if err != nil {
		panic(err)
	}
	symbols := watchlist.Symbols(items)
	if len(symbols) == 0 {
		panic("input csv did not contain any symbols")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	var massiveClient *massive.Client
	if apiKey == "" {
		logger.Warn("MASSIVE_API_KEY is not set; UI will start but rankings need a key")
	} else {
		massiveClient = massive.New(apiKey, cfg.Massive.CacheDir, logger)
	}

	app := &App{
		log:     logger,
		cfg:     cfg,
		tz:      tz,
		client:  massiveClient,
		symbols: symbols,
	}
	app.setSnapshot(Snapshot{
		Mode:      "live",
		Status:    "starting",
		UpdatedAt: time.Now().In(tz),
		Symbols:   len(symbols),
		Formula:   formula(),
	})

	if cfg.Live.Enabled {
		go app.liveLoop(ctx)
	}

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.ServerPort),
		Handler:           app.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Info("mean-reversion-candidate listening", "url", fmt.Sprintf("http://localhost:%d", cfg.ServerPort), "symbols", len(symbols))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("web", "index.html"))
	})
	mux.HandleFunc("GET /app.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("web", "app.js"))
	})
	mux.HandleFunc("GET /styles.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("web", "styles.css"))
	})
	mux.HandleFunc("GET /api/health", a.handleHealth)
	mux.HandleFunc("GET /api/config", a.handleConfig)
	mux.HandleFunc("GET /api/rankings", a.handleRankings)
	mux.HandleFunc("POST /api/refresh", a.handleRefresh)
	return mux
}

func (a *App) liveLoop(ctx context.Context) {
	a.refreshLive(ctx)
	ticker := time.NewTicker(time.Duration(a.cfg.Live.RefreshSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.refreshLive(ctx)
		}
	}
}

func (a *App) refreshLive(parent context.Context) {
	now := time.Now().In(a.tz)
	from, to, open := a.liveRange(now)
	if !open {
		a.setSnapshot(Snapshot{
			Mode:       "live",
			Status:     "outside configured live window",
			AsOf:       now,
			UpdatedAt:  now,
			Symbols:    len(a.symbols),
			Rankings:   a.snapshot().Rankings,
			Formula:    formula(),
			MarketOpen: false,
		})
		return
	}

	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	a.refreshForRange(ctx, "live", from, to)
}

func (a *App) refreshForRange(ctx context.Context, mode string, from, to time.Time) Snapshot {
	all, errs := a.fetchBars(ctx, from, to)
	daily, dailyErrs := a.fetchDailyBars(ctx, to)
	errs = append(errs, dailyErrs...)
	rankings := scoring.Rank(all, daily, scoringConfig(a.cfg), to)
	a.addChartURLs(rankings, to)
	status := "ready"
	if len(errs) > 0 {
		status = fmt.Sprintf("ready with %d data errors", len(errs))
	}
	snap := Snapshot{
		Mode:       mode,
		Status:     status,
		AsOf:       to.In(a.tz),
		UpdatedAt:  time.Now().In(a.tz),
		Symbols:    len(a.symbols),
		Rankings:   rankings,
		Errors:     errs,
		Formula:    formula(),
		MarketOpen: mode == "live",
	}
	if mode == "live" {
		a.setSnapshot(snap)
	}
	return snap
}

func (a *App) fetchBars(ctx context.Context, from, to time.Time) (map[string][]bars.Bar, []string) {
	if a.client == nil {
		all := make(map[string][]bars.Bar, len(a.symbols))
		for _, symbol := range a.symbols {
			all[symbol] = nil
		}
		return all, []string{"MASSIVE_API_KEY is required in .env to fetch candle data"}
	}

	limit := max(120, int(to.Sub(from)/time.Minute)+10)
	parallel := max(1, a.cfg.Massive.MaxParallelRequests)
	sem := make(chan struct{}, parallel)
	results := make(chan symbolBars, len(a.symbols))
	var wg sync.WaitGroup

	for _, symbol := range a.symbols {
		symbol := symbol
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()

			series, err := a.client.BackfillBars(ctx, symbol, from, to, limit)
			results <- symbolBars{symbol: symbol, bars: series, err: err}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	all := make(map[string][]bars.Bar, len(a.symbols))
	errs := make([]string, 0)
	for res := range results {
		if res.err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", res.symbol, res.err))
			all[res.symbol] = nil
			continue
		}
		all[res.symbol] = res.bars
	}
	for _, symbol := range a.symbols {
		if _, ok := all[symbol]; !ok {
			all[symbol] = nil
		}
	}
	sort.Strings(errs)
	return all, errs
}

func (a *App) fetchDailyBars(ctx context.Context, asOf time.Time) (map[string][]bars.Bar, []string) {
	all := make(map[string][]bars.Bar, len(a.symbols))
	if a.client == nil {
		for _, symbol := range a.symbols {
			all[symbol] = nil
		}
		return all, nil
	}

	to := priorSessionDay(asOf, a.tz)
	from := to.AddDate(0, 0, -45)
	limit := 60
	parallel := max(1, a.cfg.Massive.MaxParallelRequests)
	sem := make(chan struct{}, parallel)
	results := make(chan symbolBars, len(a.symbols))
	var wg sync.WaitGroup

	for _, symbol := range a.symbols {
		symbol := symbol
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()

			series, err := a.client.DailyBars(ctx, symbol, from, to, limit)
			results <- symbolBars{symbol: symbol, bars: series, err: err}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	errs := make([]string, 0)
	for res := range results {
		if res.err != nil {
			errs = append(errs, fmt.Sprintf("%s daily: %v", res.symbol, res.err))
			all[res.symbol] = nil
			continue
		}
		all[res.symbol] = res.bars
	}
	for _, symbol := range a.symbols {
		if _, ok := all[symbol]; !ok {
			all[symbol] = nil
		}
	}
	sort.Strings(errs)
	return all, errs
}

func priorSessionDay(asOf time.Time, tz *time.Location) time.Time {
	local := asOf.In(tz)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, tz)
	return day.AddDate(0, 0, -1)
}

type symbolBars struct {
	symbol string
	bars   []bars.Bar
	err    error
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	snap := a.snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"symbols":    len(a.symbols),
		"status":     snap.Status,
		"updated_at": snap.UpdatedAt,
	})
}

func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"config":  a.cfg,
		"symbols": a.symbols,
		"formula": formula(),
	})
}

func (a *App) handleRankings(w http.ResponseWriter, r *http.Request) {
	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	if mode == "" || mode == "live" {
		writeJSON(w, http.StatusOK, a.snapshot())
		return
	}
	if mode != "historical" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "mode must be live or historical"})
		return
	}

	day, asOf, err := a.parseHistoricalRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	from := combineDayTime(day, a.cfg.Historical.StartTime, a.tz)
	end := combineDayTime(day, a.cfg.Historical.EndTime, a.tz)
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	writeJSON(w, http.StatusOK, a.refreshHistorical(ctx, from, end, asOf))
}

func (a *App) refreshHistorical(ctx context.Context, from, end, asOf time.Time) Snapshot {
	all, errs := a.fetchBars(ctx, from, end)
	daily, dailyErrs := a.fetchDailyBars(ctx, asOf)
	errs = append(errs, dailyErrs...)
	rankings := scoring.Rank(all, daily, scoringConfig(a.cfg), asOf)
	a.addChartURLs(rankings, asOf)
	status := "ready"
	if len(errs) > 0 {
		status = fmt.Sprintf("ready with %d data errors", len(errs))
	}
	return Snapshot{
		Mode:       "historical",
		Status:     status,
		AsOf:       asOf.In(a.tz),
		UpdatedAt:  time.Now().In(a.tz),
		Symbols:    len(a.symbols),
		Rankings:   rankings,
		Errors:     errs,
		Formula:    formula(),
		MarketOpen: false,
	}
}

func (a *App) handleRefresh(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	a.refreshLive(ctx)
	writeJSON(w, http.StatusOK, a.snapshot())
}

func (a *App) parseHistoricalRequest(r *http.Request) (time.Time, time.Time, error) {
	dateRaw := strings.TrimSpace(r.URL.Query().Get("date"))
	timeRaw := strings.TrimSpace(r.URL.Query().Get("time"))
	if dateRaw == "" {
		dateRaw = time.Now().In(a.tz).Format("2006-01-02")
	}
	if timeRaw == "" {
		timeRaw = a.cfg.Historical.EndTime
	}
	day, err := time.ParseInLocation("2006-01-02", dateRaw, a.tz)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid date: %w", err)
	}
	asOf := combineDayTime(day, timeRaw, a.tz)
	start := combineDayTime(day, a.cfg.Historical.StartTime, a.tz)
	end := combineDayTime(day, a.cfg.Historical.EndTime, a.tz)
	if asOf.Before(start) {
		asOf = start
	}
	if asOf.After(end) {
		asOf = end
	}
	return day, asOf, nil
}

func (a *App) liveRange(now time.Time) (time.Time, time.Time, bool) {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, a.tz)
	start := combineDayTime(day, a.cfg.Live.StartTime, a.tz)
	end := combineDayTime(day, a.cfg.Live.EndTime, a.tz)
	if now.Before(start) || now.After(end) {
		return start, now, false
	}
	return start, now, true
}

func combineDayTime(day time.Time, clock string, tz *time.Location) time.Time {
	parsed, err := time.Parse("15:04", clock)
	if err != nil {
		parsed, _ = time.Parse("15:04", "04:00")
	}
	local := day.In(tz)
	return time.Date(local.Year(), local.Month(), local.Day(), parsed.Hour(), parsed.Minute(), 0, 0, tz)
}

func scoringConfig(cfg config.Config) scoring.Config {
	return scoring.Config{
		LookbackMinutes:      cfg.Scoring.LookbackMinutes,
		RangeLookbackMinutes: cfg.Scoring.RangeLookbackMinutes,
		ATRPeriod:            cfg.Scoring.ATRPeriod,
		MinDollarVolume:      cfg.Scoring.MinDollarVolume,
		ExcellentScore:       cfg.Scoring.ExcellentScore,
		GoodScore:            cfg.Scoring.GoodScore,
	}
}

func formula() Formula {
	return Formula{
		Summary: "Score = VWAP extension (25) + 30m statistical move (20) + daily ATR move (20) + 60m range extension (12) + liquidity (13) + reversal evidence (10) - trend persistence penalty (15).",
		Components: map[string]string{
			"VWAP extension":       "How far price is stretched from session VWAP. Bigger stretch means stronger mean-reversion setup.",
			"30m statistical move": "The latest 30 minute return divided by recent minute-return volatility. Around +/-2.5z is extreme.",
			"Daily ATR move":       "Today price move from prior close divided by 14-day ATR. Around +/-1.5 ATR is very stretched.",
			"60m range extension":  "Long bounce candidates score higher near the lower 60m range; short fade candidates score higher near the upper range.",
			"Liquidity":            "Session dollar volume relative to the configured minimum. Thin names are penalized.",
			"Reversal evidence":    "Last candle closing away from the extreme improves the setup.",
			"Trend penalty":        "Repeated one-direction candles reduce score because runaway trends are poor mean-reversion bets.",
		},
	}
}

func (a *App) addChartURLs(rankings []scoring.Result, fallback time.Time) {
	for i := range rankings {
		t := rankings[i].LastUpdated
		if t.IsZero() {
			t = fallback
		}
		rankings[i].ChartURL = a.chartURL(rankings[i], t)
	}
}

func (a *App) chartURL(result scoring.Result, t time.Time) string {
	base := strings.TrimRight(a.cfg.UI.ChartOpenerBaseURL, "/")
	if base == "" {
		base = "http://localhost:8081"
	}
	local := t.In(a.tz)
	signal := "buy"
	if result.Side == "Short fade" {
		signal = "sell"
	}
	return fmt.Sprintf(
		"%s/api/open-chart/%s/%s/%s?signal=%s",
		base,
		result.Symbol,
		local.Format("2006-01-02"),
		local.Format("1504"),
		signal,
	)
}

func (a *App) snapshot() Snapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.current
}

func (a *App) setSnapshot(snapshot Snapshot) {
	a.mu.Lock()
	a.current = snapshot
	a.mu.Unlock()
}

func newLogger(level string) *slog.Logger {
	lvl := new(slog.LevelVar)
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl.Set(slog.LevelDebug)
	case "warn":
		lvl.Set(slog.LevelWarn)
	case "error":
		lvl.Set(slog.LevelError)
	default:
		lvl.Set(slog.LevelInfo)
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
