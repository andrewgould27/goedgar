package edgar

import (
	"errors"
	"fmt"
	"time"
)

type APIError struct {
	URL        string
	StatusCode int
	Status     string
	Body       string

	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("goedgar: %s returned %s", e.URL, e.Status)
	}

	return fmt.Sprintf("goedgar: %s returned %s: %s", e.URL, e.Status, e.Body)
}

var ErrUnknownTicker = errors.New("goedgar: unknown ticker")
