package edgar

import (
	"context"
	"encoding/json"
	"errors"
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
	retry     RetryPolicy

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
		retry:     DefaultRetryPolicy(),
		limiter:   rate.NewLimiter(rate.Limit(10), 1),
		log:       slog.New(slog.DiscardHandler),
	}

	for _, o := range opts {
		o(c)
	}

	return c
}

func (c *Client) getJSON(ctx context.Context, endpoint string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("edgar: building request for %s: %w", endpoint, err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	for attempt := 1; ; attempt++ {
		body, err := c.fetch(ctx, req, attempt)
		if err == nil {
			if err := json.Unmarshal(body, v); err != nil {
				return fmt.Errorf("edgar: decoding %s: %w", endpoint, err)
			}
			return nil
		}

		if ctx.Err() != nil || !isRetryable(err) {
			return err
		}
		if attempt >= c.retry.MaxAttempts {
			if attempt > 1 {
				return fmt.Errorf("%w (gave up after %d attempts)", err, attempt)
			}
			return err
		}

		delay := c.retry.delay(attempt)
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
			if apiErr.RetryAfter > c.retry.MaxDelay {
				return err
			}
			delay = max(delay, apiErr.RetryAfter)
		}

		c.log.DebugContext(ctx, "edgar retrying",
			"endpoint", endpoint, "attempt", attempt, "delay", delay, "err", err)

		if serr := sleepCtx(ctx, delay); serr != nil {
			return fmt.Errorf("%w (retry abandoned: %w)", err, serr)
		}
	}
}

func (c *Client) fetch(ctx context.Context, req *http.Request, attempt int) ([]byte, error) {
	endpoint := req.URL.String()

	waitStart := time.Now()
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("edgar: waiting for rate limiter: %w", err)
	}
	rateWait := time.Since(waitStart)

	start := time.Now()
	c.log.DebugContext(ctx, "edgar request", "endpoint", endpoint, "attempt", attempt, "rate_wait", rateWait)

	resp, err := c.http.Do(req.Clone(ctx))
	if err != nil {
		return nil, fmt.Errorf("edgar: requesting %s: %w", endpoint, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	c.log.DebugContext(ctx, "edgar response",
		"endpoint", endpoint, "status", resp.StatusCode, "duration", time.Since(start))

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, &APIError{
			URL:        endpoint,
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       strings.TrimSpace(string(snippet)),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("edgar: reading response from %s: %w", endpoint, err)
	}
	return body, nil
}
