package goedgar

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

func newTickerServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	tickers, err := os.ReadFile("testdata/company_tickers.json")
	if err != nil {
		t.Fatal(err)
	}
	submissions, err := os.ReadFile("testdata/submissions_apple.json")
	if err != nil {
		t.Fatal(err)
	}

	var tickerHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/files/company_tickers.json", func(w http.ResponseWriter, r *http.Request) {
		tickerHits.Add(1)
		w.Write(tickers)
	})
	mux.HandleFunc("/submissions/CIK0000320193.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write(submissions)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &tickerHits
}

func newTestClient(srvURL string) *Client {
	c := NewClient("TestApp test@example.com")
	c.baseURL = srvURL
	c.wwwURL = srvURL
	return c
}

func TestLookupTicker(t *testing.T) {
	srv, hits := newTickerServer(t)
	c := newTestClient(srv.URL)

	tests := []struct {
		ticker  string
		wantCIK int
	}{
		{"AAPL", 320193},
		{"aapl", 320193},
		{" GOOG ", 1652044},
		{"GOOGL", 1652044},
		{"BRK.B", 1067983},
		{"BRK-B", 1067983},
	}
	for _, tt := range tests {
		co, err := c.LookupTicker(t.Context(), tt.ticker)
		if err != nil {
			t.Errorf("LookupTicker(%q): %v", tt.ticker, err)
			continue
		}
		if co.CIK != tt.wantCIK {
			t.Errorf("LookupTicker(%q).CIK = %d, want %d", tt.ticker, co.CIK, tt.wantCIK)
		}
	}

	if n := hits.Load(); n != 1 {
		t.Errorf("ticker list fetched %d times, want 1 (should be cached)", n)
	}
}

func TestLookupTicker_Unknown(t *testing.T) {
	srv, _ := newTickerServer(t)
	c := newTestClient(srv.URL)

	for _, ticker := range []string{"ZZZZ", ""} {
		_, err := c.LookupTicker(t.Context(), ticker)
		if !errors.Is(err, ErrUnknownTicker) {
			t.Errorf("LookupTicker(%q) err = %v, want ErrUnknownTicker", ticker, err)
		}
	}
}

func TestSubmissionsByTicker(t *testing.T) {
	srv, _ := newTickerServer(t)
	c := newTestClient(srv.URL)

	s, err := c.SubmissionsByTicker(t.Context(), "aapl")
	if err != nil {
		t.Fatalf("SubmissionsByTicker: %v", err)
	}
	if s.Name != "Apple Inc." {
		t.Errorf("Name = %q", s.Name)
	}
}
