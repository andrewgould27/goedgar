package edgar

import (
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"
)

func TestSubmissions(t *testing.T) {
	fixture, err := os.ReadFile("testdata/submissions_apple.json")
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := "/submissions/CIK0000320193.json"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(fixture); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient("TestApp test@example.com")
	c.baseURL = srv.URL

	s, err := c.Submissions(t.Context(), 320193)
	if err != nil {
		t.Fatalf("Submissions: %v", err)
	}

	if s.Name != "Apple Inc." {
		t.Errorf("Name = %q", s.Name)
	}
	if len(s.RecentFilings) != 2 {
		t.Fatalf("got %d filings, want 2", len(s.RecentFilings))
	}

	tenK := s.RecentFilings[0]
	if tenK.Form != "10-K" || !tenK.IsXBRL {
		t.Errorf("first filing = %+v", tenK)
	}
	if want := time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC); !tenK.FilingDate.Equal(want) {
		t.Errorf("FilingDate = %v, want %v", tenK.FilingDate, want)
	}
	if want := "https://www.sec.gov/Archives/edgar/data/320193/000032019324000123/aapl-20240928.htm"; tenK.URL() != want {
		t.Errorf("URL = %q, want %q", tenK.URL(), want)
	}

	if eightK := s.RecentFilings[1]; !eightK.ReportDate.IsZero() {
		t.Errorf("8-K ReportDate = %v, want zero", eightK.ReportDate)
	}
}

func TestSubmissions_InvalidCIK(t *testing.T) {
	c := NewClient("TestApp test@example.com")
	if _, err := c.Submissions(t.Context(), 0); err == nil {
		t.Error("expected error for CIK 0")
	}
}

func TestFilingsByform(t *testing.T) {
	s := &Submissions{RecentFilings: []Filing{
		{Form: "10-K", AccessionNumber: "a"},
		{Form: "8-K", AccessionNumber: "b"},
		{Form: "10-K/A", AccessionNumber: "c"},
		{Form: "4", AccessionNumber: "d"},
		{Form: "10-K", AccessionNumber: "e"},
	}}

	tests := []struct {
		name  string
		forms []string
		want  []string
	}{
		{"single form", []string{"10-K"}, []string{"a", "e"}},
		{"with amendment", []string{"10-K", "10-K/A"}, []string{"a", "c", "e"}},
		{"case and spaces", []string{" 8-k "}, []string{"b"}},
		{"no match", []string{"S-1"}, nil},
		{"no forms", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.FilingsByForm(tt.forms...)
			var gotIDs []string
			for _, f := range got {
				gotIDs = append(gotIDs, f.AccessionNumber)
			}
			if !slices.Equal(gotIDs, tt.want) {
				t.Errorf("FilingsByForm(%q) = %v, want %v", tt.forms, gotIDs, tt.want)
			}
		})
	}
}
