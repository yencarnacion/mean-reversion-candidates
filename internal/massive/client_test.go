package massive

import (
	"net/http"
	"testing"
	"time"
)

func TestRetryableStatus(t *testing.T) {
	tests := []struct {
		status int
		want   bool
	}{
		{0, true},
		{http.StatusOK, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusRequestTimeout, true},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
	}
	for _, tt := range tests {
		if got := retryableStatus(tt.status); got != tt.want {
			t.Fatalf("retryableStatus(%d) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestRetryAfterDelaySeconds(t *testing.T) {
	resp := &http.Response{Header: http.Header{"Retry-After": []string{"2"}}}
	if got := retryAfterDelay(resp); got != 2*time.Second {
		t.Fatalf("retryAfterDelay seconds = %v, want 2s", got)
	}
}

func TestRetryAfterDelayHTTPDate(t *testing.T) {
	when := time.Now().Add(2 * time.Second).UTC().Truncate(time.Second)
	resp := &http.Response{Header: http.Header{"Retry-After": []string{when.Format(http.TimeFormat)}}}
	got := retryAfterDelay(resp)
	if got <= 0 || got > 3*time.Second {
		t.Fatalf("retryAfterDelay date = %v, want positive delay near 2s", got)
	}
}
