// Command standin is a small HTTP stand-in for the quote source of brl-stocks.
//
// It serves SYNTHETIC quotes (made-up numbers, never real market data) on
//
//	GET /v8/finance/chart/<symbol>?interval=5m&range=1d
//
// with trading periods placed relative to the current time, so that a market
// can be made open or closed. Its behaviour is switched while it runs, and it
// records every request it receives, so that request counts are observed from
// the source's side.
//
// Control endpoints (never called by the tool):
//
//	POST /_control   replace the behaviour (JSON, see type state)
//	GET  /_requests  the request record (JSON array)
//
// On start it prints one line, "LISTEN http://127.0.0.1:<port>", on standard
// output. Standard library only.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// override changes single fields of a symbol's synthetic quote.
type override struct {
	Name      *string  `json:"name,omitempty"`
	Price     *float64 `json:"price,omitempty"`
	PrevClose *float64 `json:"prev_close,omitempty"`
	Low       *float64 `json:"low,omitempty"`
	High      *float64 `json:"high,omitempty"`
}

// state is the switchable behaviour. A POST to /_control replaces all of it.
type state struct {
	// Status, when not 0, is answered to every quote request (429, 403, 503 ...).
	Status int `json:"status"`
	// SymbolStatus answers a status for single symbols (404 for one ticker).
	SymbolStatus map[string]int `json:"symbol_status"`
	// DelayMS delays every answer (a slow source, or one that does not answer).
	DelayMS int `json:"delay_ms"`
	// ClosedMarkets lists the markets that are closed: "b3", "us", "fx".
	ClosedMarkets []string `json:"closed_markets"`
	// ClosedAgoSeconds says how long ago a closed market closed (default 7200).
	ClosedAgoSeconds int64 `json:"closed_ago_seconds"`
	// Quotes overrides fields of single symbols.
	Quotes map[string]override `json:"quotes"`
	// VaryQuotes, when true, changes the price on every answer: the request's
	// sequence number moves it by up to 9.96 (in steps of 0.01) from the
	// synthetic price, and the day low and high follow it. Off by default.
	VaryQuotes bool `json:"vary_quotes"`
	// OpenEndUnix, when not 0, is the end (Unix seconds) of the regular session
	// of every open market, in place of "four hours from now". Added for S5.12:
	// a session that ends while the check runs.
	OpenEndUnix int64 `json:"open_end_unix"`
	// OpensAtUnix makes the session of a market ("b3", "us", "fx") start at a
	// given time (Unix seconds). Before it, the market is closed and its last
	// session is the same hours on the previous weekday in the exchange's time
	// zone (7 hours long); from it on, the market is open with a session of 7
	// hours from that time. Takes precedence over closed_markets. Added for
	// S5.7: a market's next session starts while the view is open.
	OpensAtUnix map[string]int64 `json:"opens_at_unix"`
	// QuoteBeforeCloseSeconds, when not 0, puts the quote time of a market
	// whose session is over that many seconds before its close, in place of at
	// the close. Added for S5.8: a quote time before the close.
	QuoteBeforeCloseSeconds int64 `json:"quote_before_close_seconds"`
}

// record is one request as the source saw it.
type record struct {
	Seq        int    `json:"seq"`
	Time       string `json:"time"`        // arrival, RFC 3339 with nanoseconds, UTC
	UnixMS     int64  `json:"unix_ms"`     // arrival
	AnsweredMS int64  `json:"answered_ms"` // when the answer was written (0 = not yet)
	Symbol     string `json:"symbol"`
	Query      string `json:"query"`
	Status     int    `json:"status"` // status answered (0 = not yet, -1 = client went away)
	// Added for QR12: what else the request carried.
	Method  string              `json:"method,omitempty"`
	Host    string              `json:"host,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
	BodyLen int64               `json:"body_len,omitempty"`
}

type quote struct {
	Name, Currency, TZ          string
	Price, PrevClose, Low, High float64
}

// All numbers are invented for the tests.
var known = map[string]quote{
	"PETR4.SA": {"Petrobras PN", "BRL", "America/Sao_Paulo", 38.42, 38.11, 38.02, 38.60},
	"VALE3.SA": {"Vale ON", "BRL", "America/Sao_Paulo", 61.07, 61.07, 60.80, 61.55},
	"ITUB4.SA": {"Itau Unibanco PN", "BRL", "America/Sao_Paulo", 34.15, 34.50, 34.01, 34.62},
	"AAPL":     {"Apple Inc.", "USD", "America/New_York", 229.87, 230.99, 228.90, 231.40},
	"MSFT":     {"Microsoft Corp", "USD", "America/New_York", 512.30, 510.25, 509.10, 513.00},
	"^BVSP":    {"IBOVESPA", "BRL", "America/Sao_Paulo", 158432.10, 157926.70, 157800.00, 158700.00},
	"^GSPC":    {"S&P 500", "USD", "America/New_York", 6812.40, 6819.90, 6801.00, 6825.50},
	"BRL=X":    {"USD/BRL", "BRL", "Europe/London", 5.4321, 5.4278, 5.4200, 5.4400},
}

func marketOf(symbol string) string {
	switch {
	case symbol == "BRL=X":
		return "fx"
	case strings.HasSuffix(symbol, ".SA"), symbol == "^BVSP":
		return "b3"
	default:
		return "us"
	}
}

func synthetic(symbol string) quote {
	if q, ok := known[symbol]; ok {
		return q
	}
	h := fnv.New32a()
	h.Write([]byte(symbol))
	price := 10 + float64(h.Sum32()%900000)/100 // 10.00 .. 9009.99
	q := quote{Name: "Synthetic " + symbol, Price: price, PrevClose: price - 0.25, Low: price - 0.50, High: price + 0.50}
	if marketOf(symbol) == "b3" {
		q.Currency, q.TZ = "BRL", "America/Sao_Paulo"
	} else {
		q.Currency, q.TZ = "USD", "America/New_York"
	}
	return q
}

type server struct {
	mu   sync.Mutex
	st   state
	recs []record
}

func (s *server) control(w http.ResponseWriter, r *http.Request) {
	var st state
	if err := json.NewDecoder(r.Body).Decode(&st); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.st = st
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) requests(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	out := make([]record, len(s.recs))
	copy(out, s.recs)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (s *server) finish(seq, status int) {
	s.mu.Lock()
	s.recs[seq].Status = status
	s.recs[seq].AnsweredMS = time.Now().UnixMilli()
	s.mu.Unlock()
}

func (s *server) chart(w http.ResponseWriter, r *http.Request) {
	symbol := strings.TrimPrefix(r.URL.Path, "/v8/finance/chart/")
	now := time.Now()

	s.mu.Lock()
	seq := len(s.recs)
	s.recs = append(s.recs, record{
		Seq: seq, Time: now.UTC().Format(time.RFC3339Nano), UnixMS: now.UnixMilli(),
		Symbol: symbol, Query: r.URL.RawQuery,
		Method: r.Method, Host: r.Host, Headers: r.Header.Clone(), BodyLen: bodyLen(r),
	})
	st := s.st
	s.mu.Unlock()

	if st.DelayMS > 0 {
		select {
		case <-time.After(time.Duration(st.DelayMS) * time.Millisecond):
		case <-r.Context().Done():
			s.finish(seq, -1)
			return
		}
	}

	status := st.Status
	if v, ok := st.SymbolStatus[symbol]; ok {
		status = v
	}
	if status != 0 && status != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		desc := http.StatusText(status)
		code := "Error"
		if status == http.StatusNotFound {
			code, desc = "Not Found", "No data found, symbol may be delisted"
		}
		json.NewEncoder(w).Encode(map[string]any{"chart": map[string]any{
			"result": nil, "error": map[string]any{"code": code, "description": desc},
		}})
		s.finish(seq, status)
		return
	}

	q := synthetic(symbol)
	if o, ok := st.Quotes[symbol]; ok {
		if o.Name != nil {
			q.Name = *o.Name
		}
		if o.Price != nil {
			q.Price = *o.Price
		}
		if o.PrevClose != nil {
			q.PrevClose = *o.PrevClose
		}
		if o.Low != nil {
			q.Low = *o.Low
		}
		if o.High != nil {
			q.High = *o.High
		}
	}

	if st.VaryQuotes {
		q.Price += float64(seq%997) / 100
		if q.Price < q.Low {
			q.Low = q.Price
		}
		if q.Price > q.High {
			q.High = q.Price
		}
	}

	closed := false
	for _, m := range st.ClosedMarkets {
		if m == marketOf(symbol) {
			closed = true
		}
	}
	nowS := now.Unix()
	var start, end, quoteTime int64
	if at, ok := st.OpensAtUnix[marketOf(symbol)]; ok && at != 0 {
		if nowS < at {
			loc, err := time.LoadLocation(q.TZ)
			if err != nil {
				loc = time.UTC
			}
			prev := time.Unix(at, 0).In(loc).AddDate(0, 0, -1)
			for prev.Weekday() == time.Saturday || prev.Weekday() == time.Sunday {
				prev = prev.AddDate(0, 0, -1)
			}
			start = prev.Unix()
			end = start + 7*3600
			quoteTime = end - st.QuoteBeforeCloseSeconds
		} else {
			start = at
			end = at + 7*3600
			quoteTime = nowS - 30
			if quoteTime < start {
				quoteTime = start
			}
		}
	} else if closed {
		ago := st.ClosedAgoSeconds
		if ago <= 0 {
			ago = 7200
		}
		end = nowS - ago
		start = end - 8*3600
		quoteTime = end - st.QuoteBeforeCloseSeconds
	} else {
		start = nowS - 2*3600
		end = nowS + 4*3600
		if st.OpenEndUnix != 0 {
			end = st.OpenEndUnix
		}
		quoteTime = nowS - 30
		if quoteTime > end {
			quoteTime = end - st.QuoteBeforeCloseSeconds // the session given by open_end_unix is over
		}
	}

	// A made-up intraday series from the previous close to the last price.
	var ts []int64
	for t := start; t <= quoteTime && len(ts) < 120; t += 300 {
		ts = append(ts, t)
	}
	n := len(ts)
	closes, lows, highs := make([]float64, n), make([]float64, n), make([]float64, n)
	for i := range ts {
		f := 1.0
		if n > 1 {
			f = float64(i) / float64(n-1)
		}
		c := q.PrevClose + (q.Price-q.PrevClose)*f
		closes[i] = c
		lows[i] = c - (c-q.Low)*0.1
		highs[i] = c + (q.High-c)*0.1
	}
	if n > 0 {
		closes[n-1] = q.Price
	}

	body := map[string]any{"chart": map[string]any{
		"result": []any{map[string]any{
			"meta": map[string]any{
				"symbol":               symbol,
				"currency":             q.Currency,
				"longName":             q.Name,
				"regularMarketPrice":   q.Price,
				"chartPreviousClose":   q.PrevClose,
				"regularMarketDayLow":  q.Low,
				"regularMarketDayHigh": q.High,
				"regularMarketTime":    quoteTime,
				"exchangeTimezoneName": q.TZ,
				"currentTradingPeriod": map[string]any{
					"regular": map[string]any{"start": start, "end": end},
				},
			},
			"timestamp": ts,
			"indicators": map[string]any{"quote": []any{map[string]any{
				"close": closes, "low": lows, "high": highs,
			}}},
		}},
		"error": nil,
	}}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(body)
	s.finish(seq, http.StatusOK)
}

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "address to listen on (loopback)")
	flag.Parse()

	s := &server{}
	mux := http.NewServeMux()
	mux.HandleFunc("/_control", s.control)
	mux.HandleFunc("/_requests", s.requests)
	mux.HandleFunc("/v8/finance/chart/", s.chart)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Anything else the tool might ask for is recorded as well.
		s.mu.Lock()
		now := time.Now()
		s.recs = append(s.recs, record{
			Seq: len(s.recs), Time: now.UTC().Format(time.RFC3339Nano), UnixMS: now.UnixMilli(),
			AnsweredMS: now.UnixMilli(), Symbol: "(other path) " + r.URL.Path, Query: r.URL.RawQuery, Status: 404,
			Method: r.Method, Host: r.Host, Headers: r.Header.Clone(), BodyLen: bodyLen(r),
		})
		s.mu.Unlock()
		http.NotFound(w, r)
	})

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "standin:", err)
		os.Exit(1)
	}
	fmt.Printf("LISTEN http://%s\n", ln.Addr())
	if err := http.Serve(ln, mux); err != nil {
		fmt.Fprintln(os.Stderr, "standin:", err)
		os.Exit(1)
	}
}

// bodyLen reads and counts the request body (a GET is expected to carry none).
func bodyLen(r *http.Request) int64 {
	if r.Body == nil {
		return 0
	}
	n, _ := io.Copy(io.Discard, r.Body)
	return n
}
