package goedgar

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type Company struct {
	CIK    int
	Ticker string
	Name   string
}

func (c *Client) LookupTicker(ctx context.Context, ticker string) (Company, error) {
	key := normalizeTicker(ticker)
	if key == "" {
		return Company{}, fmt.Errorf("%w: %q", ErrUnknownTicker, ticker)
	}

	tickers, err := c.loadTickers(ctx)
	if err != nil {
		return Company{}, err
	}

	co, ok := tickers[key]
	if !ok {
		return Company{}, fmt.Errorf("%w: %q", ErrUnknownTicker, ticker)
	}
	return co, nil
}

func (c *Client) SubmissionsByTicker(ctx context.Context, ticker string) (*Submissions, error) {
	co, err := c.LookupTicker(ctx, ticker)
	if err != nil {
		return nil, err
	}
	return c.Submissions(ctx, co.CIK)
}

func (c *Client) loadTickers(ctx context.Context) (map[string]Company, error) {
	c.tickersMu.Lock()
	defer c.tickersMu.Unlock()

	if c.tickers != nil {
		return c.tickers, nil
	}

	var raw map[string]struct {
		CIK    int    `json:"cik_str"`
		Ticker string `json:"ticker"`
		Title  string `json:"title"`
	}
	if err := c.getJSON(ctx, c.wwwURL+"/files/company_tickers.json", &raw); err != nil {
		return nil, err
	}

	ordered := make([]Company, len(raw))
	for k, e := range raw {
		i, err := strconv.Atoi(k)
		if err != nil || i < 0 || i >= len(raw) {
			return nil, fmt.Errorf("goedgar: ticker list: unexpected key %q", k)
		}
		ordered[i] = Company{CIK: e.CIK, Ticker: e.Ticker, Name: e.Title}
	}

	m := make(map[string]Company, len(ordered))
	for _, co := range ordered {
		key := normalizeTicker(co.Ticker)
		if _, dup := m[key]; key != "" && !dup {
			m[key] = co
		}
	}

	c.tickers = m
	return m, nil
}

func normalizeTicker(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	return strings.ReplaceAll(s, ".", "-")
}
