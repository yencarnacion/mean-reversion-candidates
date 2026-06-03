package massive

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"mean-reversion-candidate/internal/bars"

	massiverest "github.com/massive-com/client-go/v3/rest"
	"github.com/massive-com/client-go/v3/rest/gen"
)

type Client struct {
	rest     *massiverest.Client
	log      *slog.Logger
	cacheDir string
	cacheMu  sync.Mutex
}

func New(apiKey string, cacheDir string, log *slog.Logger) *Client {
	return &Client{
		rest:     massiverest.NewWithOptions(apiKey, massiverest.WithPagination(false), massiverest.WithTrace(false)),
		log:      log,
		cacheDir: cacheDir,
	}
}

func (c *Client) BackfillBars(ctx context.Context, symbol string, from, to time.Time, limit int) ([]bars.Bar, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return nil, nil
	}
	if to.Before(from) {
		return nil, nil
	}

	cachePath := c.cachePath("minute-bars", symbol, from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano), fmt.Sprintf("%d", limit), "adjusted=true", "sort=asc")
	var cached []bars.Bar
	if c.readCache(cachePath, &cached) {
		return cached, nil
	}

	params := &gen.GetStocksAggregatesParams{
		Adjusted: massiverest.Ptr(true),
		Sort:     massiverest.String("asc"),
		Limit:    massiverest.Int(limit),
	}
	resp, err := c.getStocksAggregatesWithRetry(
		ctx,
		symbol,
		1,
		gen.GetStocksAggregatesParamsTimespan("minute"),
		from.Format("2006-01-02"),
		to.Format("2006-01-02"),
		params,
	)
	if err != nil {
		return nil, err
	}
	if err := massiverest.CheckResponse(resp); err != nil {
		return nil, err
	}
	if resp.JSON200 == nil || resp.JSON200.Results == nil {
		return nil, nil
	}

	out := make([]bars.Bar, 0, len(*resp.JSON200.Results))
	for _, item := range *resp.JSON200.Results {
		start := time.UnixMilli(int64(item.Timestamp))
		end := start.Add(time.Minute)
		if end.Before(from) || start.After(to) {
			continue
		}
		out = append(out, bars.Bar{
			Symbol: symbol,
			Open:   item.O,
			High:   item.H,
			Low:    item.L,
			Close:  item.C,
			Volume: item.V,
			VWAP:   derefFloat(item.Vw),
			Start:  start,
			End:    end,
		})
	}
	c.writeCache(cachePath, out)
	return out, nil
}

func (c *Client) DailyBars(ctx context.Context, symbol string, from, to time.Time, limit int) ([]bars.Bar, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return nil, nil
	}
	if to.Before(from) {
		return nil, nil
	}

	cachePath := c.cachePath("daily-bars", symbol, from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano), fmt.Sprintf("%d", limit), "adjusted=true", "sort=asc")
	var cached []bars.Bar
	if c.readCache(cachePath, &cached) {
		return cached, nil
	}

	params := &gen.GetStocksAggregatesParams{
		Adjusted: massiverest.Ptr(true),
		Sort:     massiverest.String("asc"),
		Limit:    massiverest.Int(limit),
	}
	resp, err := c.getStocksAggregatesWithRetry(
		ctx,
		symbol,
		1,
		gen.GetStocksAggregatesParamsTimespan("day"),
		from.Format("2006-01-02"),
		to.Format("2006-01-02"),
		params,
	)
	if err != nil {
		return nil, err
	}
	if err := massiverest.CheckResponse(resp); err != nil {
		return nil, err
	}
	if resp.JSON200 == nil || resp.JSON200.Results == nil {
		return nil, nil
	}

	out := make([]bars.Bar, 0, len(*resp.JSON200.Results))
	for _, item := range *resp.JSON200.Results {
		start := time.UnixMilli(int64(item.Timestamp))
		out = append(out, bars.Bar{
			Symbol: symbol,
			Open:   item.O,
			High:   item.H,
			Low:    item.L,
			Close:  item.C,
			Volume: item.V,
			VWAP:   derefFloat(item.Vw),
			Start:  start,
			End:    start.Add(24 * time.Hour),
		})
	}
	c.writeCache(cachePath, out)
	return out, nil
}

func (c *Client) AvailableDates(ctx context.Context, symbol string, from, to time.Time) ([]string, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	limit := max(8, int(to.Sub(from)/(24*time.Hour))+8)
	cachePath := c.cachePath("available-dates", symbol, from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano), fmt.Sprintf("%d", limit))
	var cached []string
	if c.readCache(cachePath, &cached) {
		return cached, nil
	}

	params := &gen.GetStocksAggregatesParams{
		Adjusted: massiverest.Ptr(true),
		Sort:     massiverest.String("asc"),
		Limit:    massiverest.Int(limit),
	}
	resp, err := c.getStocksAggregatesWithRetry(
		ctx,
		symbol,
		1,
		gen.GetStocksAggregatesParamsTimespan("day"),
		from.Format("2006-01-02"),
		to.Format("2006-01-02"),
		params,
	)
	if err != nil {
		return nil, err
	}
	if err := massiverest.CheckResponse(resp); err != nil {
		return nil, err
	}
	if resp.JSON200 == nil || resp.JSON200.Results == nil {
		return nil, nil
	}

	out := make([]string, 0, len(*resp.JSON200.Results))
	for _, item := range *resp.JSON200.Results {
		if item.V <= 0 {
			continue
		}
		out = append(out, time.UnixMilli(int64(item.Timestamp)).UTC().Format("2006-01-02"))
	}
	c.writeCache(cachePath, out)
	return out, nil
}

func (c *Client) getStocksAggregatesWithRetry(
	ctx context.Context,
	ticker string,
	multiplier int,
	timespan gen.GetStocksAggregatesParamsTimespan,
	from string,
	to string,
	params *gen.GetStocksAggregatesParams,
) (*gen.GetStocksAggregatesResponse, error) {
	const maxAttempts = 4
	var lastResp *gen.GetStocksAggregatesResponse
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := c.rest.GetStocksAggregatesWithResponse(ctx, ticker, multiplier, timespan, from, to, params)
		lastResp = resp
		lastErr = err
		if err == nil && !retryableStatus(respStatusCode(resp)) {
			return resp, nil
		}
		if err == nil && resp != nil && attempt == maxAttempts {
			return resp, nil
		}
		if err != nil && !isRetryableRequestError(err) {
			return nil, err
		}
		if attempt == maxAttempts {
			break
		}

		delay := retryDelay(respHTTPResponse(resp), attempt)
		c.log.Debug(
			"retry massive aggregate request",
			"symbol", ticker,
			"timespan", string(timespan),
			"attempt", attempt,
			"next_attempt", attempt+1,
			"delay", delay.String(),
			"status", respStatusCode(resp),
			"error", err,
		)
		if err := sleepContext(ctx, delay); err != nil {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return lastResp, nil
}

func isRetryableRequestError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}

func retryableStatus(status int) bool {
	return status == 0 ||
		status == http.StatusRequestTimeout ||
		status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError
}

func retryDelay(resp *http.Response, attempt int) time.Duration {
	if delay := retryAfterDelay(resp); delay > 0 {
		return minDuration(delay, 12*time.Second)
	}
	base := 400 * time.Millisecond
	for i := 1; i < attempt; i++ {
		base *= 2
	}
	jitter := time.Duration(time.Now().UnixNano()%250) * time.Millisecond
	return minDuration(base+jitter, 5*time.Second)
}

func retryAfterDelay(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(raw)
	if err != nil {
		return 0
	}
	delay := time.Until(when)
	if delay <= 0 {
		return 0
	}
	return delay
}

func respStatusCode(resp *gen.GetStocksAggregatesResponse) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode()
}

func respHTTPResponse(resp *gen.GetStocksAggregatesResponse) *http.Response {
	if resp == nil {
		return nil
	}
	return resp.HTTPResponse
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) cachePath(prefix string, parts ...string) string {
	if strings.TrimSpace(c.cacheDir) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return filepath.Join(c.cacheDir, fmt.Sprintf("%s_%x.json", prefix, sum[:16]))
}

func (c *Client) readCache(path string, dst any) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(data, dst); err != nil {
		c.log.Debug("ignore invalid massive cache", "path", path, "error", err)
		return false
	}
	return true
}

func (c *Client) writeCache(path string, payload any) {
	if strings.TrimSpace(path) == "" {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		c.log.Debug("marshal massive cache", "path", path, "error", err)
		return
	}

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		c.log.Debug("create massive cache dir", "dir", filepath.Dir(path), "error", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		c.log.Debug("write massive cache", "path", tmp, "error", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		c.log.Debug("rename massive cache", "path", path, "error", err)
		_ = os.Remove(tmp)
	}
}

func derefFloat(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
