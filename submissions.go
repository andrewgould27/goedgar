package goedgar

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Submissions struct {
	CIK                  string
	Name                 string
	EntityType           string
	SIC                  string
	SICDescription       string
	Tickers              []string
	Exchanges            []string
	FiscalYearEnd        string
	StateOfIncorporation string

	RecentFilings []Filing

	OlderFilingFiles []string
}

type Filing struct {
	CIK             string
	AccessionNumber string
	Form            string
	FilingDate      time.Time
	ReportDate      time.Time
	PrimaryDocument string
	Description     string
	IsXBRL          bool
}

func (f Filing) URL() string {
	return fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/%s",
		strings.TrimLeft(f.CIK, "0"), strings.ReplaceAll(f.AccessionNumber, "-", ""), f.PrimaryDocument)
}

func (c *Client) Submissions(ctx context.Context, cik int) (*Submissions, error) {
	if cik <= 0 {
		return nil, fmt.Errorf("goedgar: invalid CIK %d", cik)
	}

	endpoint := fmt.Sprintf("%s/submissions/CIK%010d.json", c.baseURL, cik)
	var raw submissionsJSON
	if err := c.getJSON(ctx, endpoint, &raw); err != nil {
		return nil, err
	}

	filings, err := raw.Filings.Recent.toFilings(raw.CIK)
	if err != nil {
		return nil, fmt.Errorf("goedgar: submissions for CIK %d: %w", cik, err)
	}

	older := make([]string, len(raw.Filings.Files))
	for i, f := range raw.Filings.Files {
		older[i] = f.Name
	}

	return &Submissions{
		CIK:                  raw.CIK,
		Name:                 raw.Name,
		EntityType:           raw.EntityType,
		SIC:                  raw.SIC,
		SICDescription:       raw.SICDescription,
		Tickers:              raw.Tickers,
		Exchanges:            raw.Exchanges,
		FiscalYearEnd:        raw.FiscalYearEnd,
		StateOfIncorporation: raw.StateOfIncorporation,
		RecentFilings:        filings,
		OlderFilingFiles:     older,
	}, nil
}

func (s *Submissions) FilingsByForm(forms ...string) []Filing {
	want := make(map[string]struct{}, len(forms))
	for _, f := range forms {
		want[strings.ToUpper(strings.TrimSpace(f))] = struct{}{}
	}

	var out []Filing
	for _, f := range s.RecentFilings {
		if _, ok := want[strings.ToUpper(f.Form)]; ok {
			out = append(out, f)
		}
	}

	return out
}

type submissionsJSON struct {
	CIK                  string   `json:"cik"`
	Name                 string   `json:"name"`
	EntityType           string   `json:"entityType"`
	SIC                  string   `json:"sic"`
	SICDescription       string   `json:"sicDescription"`
	Tickers              []string `json:"tickers"`
	Exchanges            []string `json:"exchanges"`
	FiscalYearEnd        string   `json:"fiscalYearEnd"`
	StateOfIncorporation string   `json:"stateOfIncorporation"`
	Filings              struct {
		Recent recentJSON `json:"recent"`
		Files  []struct {
			Name string `json:"name"`
		} `json:"files"`
	} `json:"filings"`
}

type recentJSON struct {
	AccessionNumber       []string `json:"accessionNumber"`
	FilingDate            []string `json:"filingDate"`
	ReportDate            []string `json:"reportDate"`
	Form                  []string `json:"form"`
	PrimaryDocument       []string `json:"primaryDocument"`
	PrimaryDocDescription []string `json:"primaryDocDescription"`
	IsXBRL                []int    `json:"isXBRL"`
}

func (r recentJSON) toFilings(cik string) ([]Filing, error) {
	n := len(r.AccessionNumber)
	lengths := map[string]int{
		"filingDate":            len(r.FilingDate),
		"reportDate":            len(r.ReportDate),
		"form":                  len(r.Form),
		"primaryDocument":       len(r.PrimaryDocument),
		"primaryDocDescription": len(r.PrimaryDocDescription),
		"isXBRL":                len(r.IsXBRL),
	}
	for field, l := range lengths {
		if l != n {
			return nil, fmt.Errorf("recent filings: %s has %d entries, want %d", field, l, n)
		}
	}

	out := make([]Filing, n)
	for i := range n {
		filed, err := parseDate(r.FilingDate[i])
		if err != nil {
			return nil, fmt.Errorf("recent filings: filingDate[%d]: %w", i, err)
		}
		reported, err := parseDate(r.ReportDate[i])
		if err != nil {
			return nil, fmt.Errorf("recent filings: reportDate[%d]: %w", i, err)
		}
		out[i] = Filing{
			CIK:             cik,
			AccessionNumber: r.AccessionNumber[i],
			Form:            r.Form[i],
			FilingDate:      filed,
			ReportDate:      reported,
			PrimaryDocument: r.PrimaryDocument[i],
			Description:     r.PrimaryDocDescription[i],
			IsXBRL:          r.IsXBRL[i] == 1,
		}
	}
	return out, nil
}

// parseDate parses "2006-01-02", returning the zero time for "".
func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.DateOnly, s)
}
