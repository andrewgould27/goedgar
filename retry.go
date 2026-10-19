package edgar

import (
	"context"
	"crypto/tls"
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 4,
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    30 * time.Second,
	}
}

func WithRetryPolicy(p RetryPolicy) Option {
	return func(c *Client) { c.retry = p.normalized() }
}

func (p RetryPolicy) normalized() RetryPolicy {
	def := DefaultRetryPolicy()
	if p.MaxAttempts < 1 {
		p.MaxAttempts = 1
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = def.BaseDelay
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = def.MaxDelay
	}
	if p.MaxDelay <= p.BaseDelay {
		p.MaxDelay = p.BaseDelay
	}
	return p
}

func (p RetryPolicy) delay(n int) time.Duration {
	ceiling := p.BaseDelay
	for i := 1; i < n && ceiling < p.MaxDelay; i++ {
		ceiling *= 2
	}
	ceiling = min(ceiling, p.MaxDelay)

	half := ceiling / 2
	return half + rand.N(ceiling-half+1)
}

func isRetryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusTooManyRequests,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout:
			return true
		}
		return false
	}

	var certErr *tls.CertificateVerificationError
	return !errors.As(err, &certErr)
}

func parseRetryAfter(h string, now time.Time) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil {
		if secs <= 0 {
			return 0
		}
		const maxSecs = 24 * 60 * 60
		return time.Duration(min(secs, maxSecs)) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
