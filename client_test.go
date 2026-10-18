package edgar

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestGetJSON_Success(t *testing.T) {
	const ua = "TestApp test@example.com"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != ua {
			t.Errorf("User-Agent = %q, want %q", got, ua)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name": "Apple Inc."}`))
	}))
	defer srv.Close()

	c := NewClient(ua)
	var out struct {
		Name string `json:"name"`
	}

	if err := c.getJSON(t.Context(), srv.URL, &out); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if out.Name != "Apple Inc." {
		t.Errorf("Name = %q, want %q", out.Name, "Apple Inc.")
	}
}

func TestGetJSON_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Undeclared Automated Tool", http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClient("TestApp test@example.com")
	var out map[string]any
	err := c.getJSON(t.Context(), srv.URL, &out)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusForbidden)
	}
	if apiErr.Body != "Undeclared Automated Tool" {
		t.Errorf("Body = %q", apiErr.Body)
	}
}

func TestSubmissions_Live(t *testing.T) {
	ua := os.Getenv("EDGAR_USER_AGENT")
	if ua == "" {
		t.Skip("set EDGAR_USER_AGENT to run the live test")
	}

	s, err := NewClient(ua).Submissions(t.Context(), 320193)
	if err != nil {
		t.Fatalf("Submissions: %v", err)
	}
	if len(s.RecentFilings) == 0 {
		t.Fatal("no recent filings")
	}
	t.Logf("%s: latest %s filed %s", s.Name,
		s.RecentFilings[0].Form, s.RecentFilings[0].FilingDate.Format(time.DateOnly))

	if tenKs := s.FilingsByForm("10-K"); len(tenKs) > 0 {
		t.Logf("latest 10-K: %s (%s)", tenKs[0].FilingDate.Format(time.DateOnly), tenKs[0].URL())
	}
}
