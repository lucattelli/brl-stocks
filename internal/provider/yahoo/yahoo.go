// Package yahoo is the default source: Yahoo's unofficial chart endpoint,
// one request per instrument (D7, F3, F13).
package yahoo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lucattelli/brl-stocks/internal/market"
	"github.com/lucattelli/brl-stocks/internal/provider"
)

// DefaultURL is where the source is reached unless told otherwise (D70).
const DefaultURL = "https://query1.finance.yahoo.com"

const userAgent = "Mozilla/5.0 (compatible; brl-stocks)"

// Provider talks to the chart endpoint at base.
type Provider struct {
	base   string
	client *http.Client
}

// New returns the provider for the address base. It never follows a redirect
// to another host (QR12).
func New(base string) *Provider {
	base = strings.TrimRight(base, "/")
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 && req.URL.Host != via[0].URL.Host {
				return errors.New("redirect to another host refused")
			}
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
	return &Provider{base: base, client: client}
}

func (p *Provider) Name() string { return "yahoo" }

// Symbol maps an instrument to the source's notation (F22).
func Symbol(in market.Instrument) (string, bool) {
	switch in.Market {
	case market.B3:
		return in.Symbol + ".SA", true
	case market.US:
		return strings.ReplaceAll(in.Symbol, ".", "-"), true
	case market.IDX:
		switch in.Symbol {
		case "IBOV":
			return "^BVSP", true
		case "SP500":
			return "^GSPC", true
		}
	case market.FX:
		if in.Symbol == "USDBRL" {
			return "BRL=X", true
		}
	}
	return "", false
}

type answer struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol               string   `json:"symbol"`
				Currency             string   `json:"currency"`
				LongName             string   `json:"longName"`
				ShortName            string   `json:"shortName"`
				RegularMarketPrice   *float64 `json:"regularMarketPrice"`
				ChartPreviousClose   *float64 `json:"chartPreviousClose"`
				RegularMarketDayLow  float64  `json:"regularMarketDayLow"`
				RegularMarketDayHigh float64  `json:"regularMarketDayHigh"`
				RegularMarketTime    int64    `json:"regularMarketTime"`
				ExchangeTimezoneName string   `json:"exchangeTimezoneName"`
				CurrentTradingPeriod struct {
					Regular struct {
						Start int64 `json:"start"`
						End   int64 `json:"end"`
					} `json:"regular"`
				} `json:"currentTradingPeriod"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Close []*float64 `json:"close"`
					Low   []*float64 `json:"low"`
					High  []*float64 `json:"high"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
	} `json:"chart"`
}

func fail(kind provider.Kind, status int, format string, a ...any) (market.Quote, error) {
	return market.Quote{}, &provider.Error{Kind: kind, Status: status, Msg: fmt.Sprintf(format, a...)}
}

// Quote fetches one instrument. Any error is a *provider.Error.
func (p *Provider) Quote(ctx context.Context, in market.Instrument) (market.Quote, error) {
	sym, ok := Symbol(in)
	if !ok {
		return fail(provider.Unsupported, 0, "no symbol for %s", in)
	}
	u := p.base + "/v8/finance/chart/" + url.PathEscape(sym) + "?interval=5m&range=1d"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fail(provider.Unavailable, 0, "%v", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := p.client.Do(req)
	if err != nil {
		return fail(provider.Unavailable, 0, "%v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fail(provider.Unavailable, resp.StatusCode, "%v", err)
	}
	// An unknown ticker is a 404 with a JSON error (F27); a refusal is a 429,
	// 401 or 403 (F3). Anything else that is not a 200 is the source failing.
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fail(provider.NotFound, 404, "the source does not know %s", sym)
	case resp.StatusCode == http.StatusTooManyRequests:
		return fail(provider.RateLimited, 429, "the source is rate-limiting")
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fail(provider.Blocked, resp.StatusCode, "the source refused access")
	case resp.StatusCode != http.StatusOK:
		return fail(provider.Unavailable, resp.StatusCode, "the source answered %d", resp.StatusCode)
	}
	var a answer
	if err := json.Unmarshal(body, &a); err != nil || len(a.Chart.Result) == 0 {
		return fail(provider.BadResponse, 200, "the answer for %s cannot be read", sym)
	}
	r := a.Chart.Result[0]
	m := r.Meta
	if !strings.EqualFold(m.Symbol, sym) {
		return fail(provider.BadResponse, 200, "asked for %s, got %s", sym, m.Symbol)
	}
	if m.RegularMarketPrice == nil || m.ChartPreviousClose == nil || m.CurrentTradingPeriod.Regular.End <= m.CurrentTradingPeriod.Regular.Start {
		return fail(provider.BadResponse, 200, "the answer for %s lacks a price or a trading period", sym)
	}
	q := market.Quote{
		Name:      strings.Join(strings.Fields(firstNonEmpty(m.LongName, m.ShortName, in.Symbol)), " "),
		Price:     *m.RegularMarketPrice,
		PrevClose: *m.ChartPreviousClose,
		Low:       m.RegularMarketDayLow,
		High:      m.RegularMarketDayHigh,
		Time:      time.Unix(m.RegularMarketTime, 0).UTC(),
		Session: market.Session{
			Start: time.Unix(m.CurrentTradingPeriod.Regular.Start, 0).UTC(),
			End:   time.Unix(m.CurrentTradingPeriod.Regular.End, 0).UTC(),
			TZ:    m.ExchangeTimezoneName,
		},
	}
	switch in.Market {
	case market.B3:
		q.Currency = "BRL"
	case market.US:
		q.Currency = "USD"
	case market.FX:
		q.Currency = "BRL"
	}
	// The series of the regular session; the day's low and high come from it
	// when the source leaves them at zero.
	low, high := math.Inf(1), math.Inf(-1)
	if len(r.Indicators.Quote) > 0 {
		bars := r.Indicators.Quote[0]
		for i, ts := range r.Timestamp {
			t := time.Unix(ts, 0).UTC()
			if t.Before(q.Session.Start) || t.After(q.Session.End) {
				continue
			}
			if i < len(bars.Close) && bars.Close[i] != nil {
				q.Series = append(q.Series, market.Point{T: t, V: *bars.Close[i]})
			}
			if i < len(bars.Low) && bars.Low[i] != nil {
				low = math.Min(low, *bars.Low[i])
			}
			if i < len(bars.High) && bars.High[i] != nil {
				high = math.Max(high, *bars.High[i])
			}
		}
	}
	if q.Low <= 0 {
		q.Low = finiteOr(low, q.Price)
	}
	if q.High <= 0 {
		q.High = finiteOr(high, q.Price)
	}
	if !(q.Price > 0) || math.IsInf(q.Price, 0) || math.IsNaN(q.Price) {
		return fail(provider.BadResponse, 200, "the price for %s is not a positive number", sym)
	}
	return q, nil
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func finiteOr(x, fallback float64) float64 {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return fallback
	}
	return x
}
