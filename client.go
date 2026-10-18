package edgar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type Client struct {
	http      *http.Client
	userAgent string
	baseURL   string
	wwwURL    string
	limiter   *rate.Limiter
	log       *slog.Logger

	tickersMu sync.Mutex
	tickers   map[string]Company
}

type Option func(*Client)

func WithLogger(l *slog.Logger) Option {
	return func(c *Client) {
		if l != nil {
			c.log = l
		}
	}
}

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// NewClient returns a Client for the SEC EDGAR APIs.
// userAgent must identify you to the SEC, e.g. "MyApp john@example.com"
// requests without a valid User-Agent are rejected with 403.
func NewClient(userAgent string, opts ...Option) *Client {
	c := &Client{
		http:      &http.Client{Timeout: 30 * time.Second},
		userAgent: userAgent,
		baseURL:   "https://data.sec.gov",
		wwwURL:    "https://www.sec.gov",
		limiter:   rate.NewLimiter(rate.Limit(10), 1),
		log:       slog.New(slog.DiscardHandler),
	}

	for _, o := range opts {
		o(c)
	}

	return c
}

func (c *Client) getJSON(ctx context.Context, endpoint string, v any) error {
	waitStart := time.Now()
	if err := c.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("goedgar: waiting for rate limiter: %w", err)
	}
	rateWait := time.Since(waitStart)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("goedgar: building request for %s: %w", endpoint, err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	c.log.DebugContext(ctx, "edgar request", "method", req.Method, "endpoint", endpoint, "rate_wait", rateWait)

	resp, err := c.http.Do(req)
	if err != nil {
		c.log.DebugContext(ctx, "edgar request failed", "endpoint", endpoint, "err", err)
		return fmt.Errorf("goedgar: requesting %s: %w", endpoint, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	c.log.DebugContext(ctx, "edgar response",
		"endpoint", endpoint, "status", resp.StatusCode, "duration", time.Since(start))

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &APIError{
			URL:        endpoint,
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       strings.TrimSpace(string(snippet)),
		}
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		c.log.DebugContext(ctx, "edgar decode failed", "endpoint", endpoint, "err", err)
		return fmt.Errorf("gogoedgar: decoding %s: %w", endpoint, err)
	}

	return nil
}
