package edgar

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// newFastClient returns a client with tiny retry delays and no rate limit.
func newFastClient(srvURL string) *Client {
	c := NewClient("TestApp test@example.com", WithRetryPolicy(RetryPolicy{
		MaxAttempts: 4,
		BaseDelay:   time.Millisecond,
		MaxDelay:    10 * time.Millisecond,
	}))
	c.baseURL = srvURL
	c.wwwURL = srvURL
	c.limiter = rate.NewLimiter(rate.Inf, 1)
	return c
}

// flakyServer fails the first `failures` requests with status, then succeeds.
func flakyServer(t *testing.T, failures int32, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) <= failures {
			http.Error(w, http.StatusText(status), status)
			return
		}
		_, _ = w.Write([]byte(`{"name":"Apple Inc."}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

type nameOnly struct {
	Name string `json:"name"`
}

func TestRetry_RecoversFromTransientStatuses(t *testing.T) {
	for _, status := range []int{429, 500, 502, 503, 504} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			srv, hits := flakyServer(t, 2, status)
			c := newFastClient(srv.URL)

			var out nameOnly
			if err := c.getJSON(t.Context(), srv.URL, &out); err != nil {
				t.Fatalf("getJSON: %v", err)
			}
			if out.Name != "Apple Inc." {
				t.Errorf("Name = %q", out.Name)
			}
			if n := hits.Load(); n != 3 {
				t.Errorf("hits = %d, want 3", n)
			}
		})
	}
}

func TestRetry_RecoversFromDroppedConnection(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close() // drop the connection without a response
			return
		}
		_, _ = w.Write([]byte(`{"name":"Apple Inc."}`))
	}))
	defer srv.Close()

	c := newFastClient(srv.URL)
	var out nameOnly
	if err := c.getJSON(t.Context(), srv.URL, &out); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("hits = %d, want 2", n)
	}
}

func TestRetry_DoesNotRetryClientErrors(t *testing.T) {
	for _, status := range []int{400, 403, 404} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			srv, hits := flakyServer(t, 100, status)
			c := newFastClient(srv.URL)

			var out nameOnly
			err := c.getJSON(t.Context(), srv.URL, &out)

			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != status {
				t.Fatalf("err = %v, want APIError %d", err, status)
			}
			if n := hits.Load(); n != 1 {
				t.Errorf("hits = %d, want 1", n)
			}
		})
	}
}

func TestRetry_GivesUpAfterMaxAttempts(t *testing.T) {
	srv, hits := flakyServer(t, 100, http.StatusServiceUnavailable)
	c := newFastClient(srv.URL)

	var out nameOnly
	err := c.getJSON(t.Context(), srv.URL, &out)

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("err = %v, want APIError 503", err)
	}
	if n := hits.Load(); n != 4 {
		t.Errorf("hits = %d, want 4", n)
	}
}

func TestRetry_RetryAfterLongerThanMaxDelay(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newFastClient(srv.URL)
	var out nameOnly
	err := c.getJSON(t.Context(), srv.URL, &out)

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.RetryAfter != 120*time.Second {
		t.Fatalf("err = %v, want APIError with RetryAfter 120s", err)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("hits = %d, want 1 (should not wait 120s or retry early)", n)
	}
}

func TestRetry_StopsWhenContextEnds(t *testing.T) {
	srv, _ := flakyServer(t, 100, http.StatusServiceUnavailable)
	c := newFastClient(srv.URL)
	c.retry.BaseDelay = time.Second
	c.retry.MaxDelay = time.Second

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	var out nameOnly
	err := c.getJSON(ctx, srv.URL, &out)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Errorf("err = %v, want the underlying APIError preserved", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("took %v, should stop promptly when the context ends", elapsed)
	}
}

func TestRetryPolicy_Delay(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 10, BaseDelay: 100 * time.Millisecond, MaxDelay: time.Second}
	ceilings := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		time.Second, // capped
		time.Second,
	}
	for i, ceiling := range ceilings {
		n := i + 1
		for range 100 {
			if d := p.delay(n); d < ceiling/2 || d > ceiling {
				t.Fatalf("delay(%d) = %v, want between %v and %v", n, d, ceiling/2, ceiling)
			}
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"5", 5 * time.Second},
		{" 5 ", 5 * time.Second},
		{"0", 0},
		{"-1", 0},
		{"soon", 0},
		{now.Add(30 * time.Second).Format(http.TimeFormat), 30 * time.Second},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0},
	}
	for _, tt := range tests {
		if got := parseRetryAfter(tt.in, now); got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestRetryPolicy_Normalized(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 6}.normalized()
	def := DefaultRetryPolicy()
	if p.MaxAttempts != 6 || p.BaseDelay != def.BaseDelay || p.MaxDelay != def.MaxDelay {
		t.Errorf("normalized = %+v", p)
	}
	if p := (RetryPolicy{}).normalized(); p.MaxAttempts != 1 {
		t.Errorf("zero MaxAttempts normalized to %d, want 1", p.MaxAttempts)
	}
}
