package render

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lucattelli/brl-stocks/internal/config"
	"github.com/lucattelli/brl-stocks/internal/market"
)

// View is everything the output is drawn from.
type View struct {
	Cfg    config.Config
	Snap   market.Snapshot
	Health market.Health
	Now    time.Time
	Zone   *time.Location // for quote times; the machine's zone
	Colour bool           // colour the day change: a terminal, and NO_COLOR not set (D25)
}

// Colours of the terminal's own palette (D25, spec 7.3).
const (
	green = "\x1b[33m"
	red   = "\x1b[31m"
	reset = "\x1b[0m"
)

// paint colours s by the sign of a day change as it is shown with the given
// decimals; a change that shows as zero stays uncoloured (D50).
func (v View) paint(s string, change float64, decimals int) string {
	r := math.Round(change * math.Pow(10, float64(decimals)))
	switch {
	case !v.Colour || r == 0:
		return s
	case r > 0:
		return green + s + reset
	}
	return red + s + reset
}

// dayChange is the change value of a row with a quote to show, for colour.
func (v View) dayChange(in market.Instrument) (float64, bool) {
	switch st, _ := v.state(in); st {
	case market.NotFound, market.NoData:
		return 0, false
	}
	val, _ := change(v.Snap[in].Quote)
	return val, true
}

const dash = "—"

var labels = map[string]string{"IBOV": "IBOV", "SP500": "S&P 500", "USDBRL": "USD/BRL"}
var names = map[string]string{"IBOV": "Ibovespa", "SP500": "S&P 500", "USDBRL": "USD/BRL"}

func (v View) want() []market.Instrument { return v.Cfg.Want() }

func (v View) state(in market.Instrument) (market.State, time.Duration) {
	open, _ := market.MarketOpen(in.Hours(), v.want(), v.Snap, v.Now)
	return market.Classify(v.Snap[in], open, v.Cfg.Interval, v.Now)
}

// brazilian reports whether an instrument's numbers use the R$ format.
func brazilian(in market.Instrument) bool {
	return in.Market == market.B3 || in.Symbol == "IBOV" || in.Market == market.FX
}

func change(q *market.Quote) (value, percent float64) {
	value = q.Price - q.PrevClose
	if q.PrevClose != 0 {
		percent = value / q.PrevClose * 100
	}
	return value, percent
}

func (v View) quoteTime(t time.Time) string {
	zone := v.Zone
	if zone == nil {
		zone = time.Local
	}
	lt, ln := t.In(zone), v.Now.In(zone)
	if lt.Year() == ln.Year() && lt.YearDay() == ln.YearDay() {
		return lt.Format("15:04")
	}
	return lt.Format("02/01")
}

func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("!%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("!%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("!%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("!%dd", int(d/(24*time.Hour)))
}

// marketLine is the last line of the table: the state of each market (D47).
func (v View) marketLine() string {
	word := func(m market.Market) string {
		open, known := market.MarketOpen(m, v.want(), v.Snap, v.Now)
		switch {
		case !known:
			return "unknown"
		case open:
			return "open"
		}
		return "closed"
	}
	return "B3 " + word(market.B3) + " · US " + word(market.US)
}

func (v View) summaryLine() string {
	var parts []string
	for _, in := range v.Cfg.Summary {
		label := labels[in.Symbol]
		e := v.Snap[in]
		if e.Quote == nil {
			parts = append(parts, label+" "+dash)
			continue
		}
		decimals := 2
		switch in.Symbol {
		case "IBOV":
			decimals = 0
		case "USDBRL":
			decimals = 4
		}
		_, pct := change(e.Quote)
		br := brazilian(in)
		parts = append(parts, label+" "+number(e.Quote.Price, decimals, br)+" "+v.paint(signed(pct, 2, br)+"%", pct, 2))
	}
	return strings.Join(parts, "    ")
}

// Table writes the one-shot table (spec 7.4).
func Table(w io.Writer, v View) {
	header := []string{"TICKER", "NAME", "LAST", "CHANGE", "", "LOW", "HIGH", "TIME"}
	rightAligned := []bool{false, false, true, true, true, true, true, false}
	type line struct {
		cells []string // nil: a group name or a blank line, in text
		text  string
		in    market.Instrument
	}
	lines := []line{{cells: header}}
	for _, g := range v.Cfg.Groups {
		if g.Name != "" {
			lines = append(lines, line{text: g.Name})
		}
		for _, in := range g.Tickers {
			lines = append(lines, line{cells: v.row(in), in: in})
		}
	}
	width := make([]int, len(header))
	for _, l := range lines {
		for i, c := range l.cells {
			if n := utf8.RuneCountInString(c); n > width[i] {
				width[i] = n
			}
		}
	}
	if len(v.Cfg.Summary) > 0 {
		fmt.Fprintln(w, v.summaryLine())
		fmt.Fprintln(w)
	}
	for _, l := range lines {
		if l.cells == nil {
			fmt.Fprintln(w, l.text)
			continue
		}
		var b strings.Builder
		for i, c := range l.cells {
			pad := strings.Repeat(" ", width[i]-utf8.RuneCountInString(c))
			if i > 0 {
				b.WriteString("  ")
			}
			// The change value and percent (columns 3 and 4) are coloured after
			// padding, so that colour codes do not change the alignment.
			if val, ok := v.dayChange(l.in); ok && l.in.Symbol != "" && (i == 3 || i == 4) {
				c = v.paint(c, val, 2)
			}
			if rightAligned[i] {
				b.WriteString(pad + c)
			} else {
				b.WriteString(c + pad)
			}
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, v.marketLine())
}

func (v View) row(in market.Instrument) []string {
	e := v.Snap[in]
	st, old := v.state(in)
	switch st {
	case market.NotFound:
		return []string{in.Symbol, dash, "not found", dash, dash, dash, dash, dash}
	case market.NoData:
		return []string{in.Symbol, dash, dash, dash, dash, dash, dash, dash}
	}
	q := e.Quote
	br := brazilian(in)
	symbol := "US$ "
	if br {
		symbol = "R$ "
	}
	name := q.Name
	if utf8.RuneCountInString(name) > 24 {
		name = string([]rune(name)[:24])
	}
	val, pct := change(q)
	when := v.quoteTime(q.Time)
	if st == market.Stale {
		when += " " + age(old)
	}
	return []string{
		in.Symbol, name, symbol + number(q.Price, 2, br),
		signed(val, 2, br), signed(pct, 2, br) + "%",
		number(q.Low, 2, br), number(q.High, 2, br), when,
	}
}

// Cause gives the one line that says why rows were not updated, or "" when
// nothing is wrong (D19, D38, D50, D71).
func Cause(v View) string {
	want := v.want()
	summary := map[market.Instrument]bool{}
	for _, in := range v.Cfg.Summary {
		summary[in] = true
	}
	// Rows are lines of the watchlist (D9); summary entries are counted
	// apart and named only when no row failed (D81).
	failed, failedSummary := 0, 0
	var kind string
	var next time.Time
	for _, in := range want {
		e := v.Snap[in]
		if e.Fail == nil || e.Fail.Unknown() {
			continue
		}
		if summary[in] {
			failedSummary++
		} else {
			failed++
		}
		if kind == "" {
			kind = e.Fail.Kind
		}
		if t := e.LastAttempt.Add(market.FailureWait(v.Cfg.Interval, e.Failures)); next.IsZero() || t.Before(next) {
			next = t
		}
	}
	until, blockKind := market.EffectiveBlock(v.Health, want, v.Snap)
	blocked := v.Now.Before(until)
	if blocked {
		kind, next = blockKind, until
	}
	if failed == 0 && failedSummary == 0 && !blocked {
		return ""
	}
	phrase := "source unreachable"
	switch kind {
	case "rate_limited":
		phrase = "source is rate-limiting"
	case "blocked":
		phrase = "source refused access"
	case "bad_response":
		phrase = "source answer not understood"
	}
	what := fmt.Sprintf("%d rows", failed)
	switch {
	case failed == 1:
		what = "1 row"
	case failed == 0 && failedSummary > 0:
		what = "summary"
	}
	zone := v.Zone
	if zone == nil {
		zone = time.Local
	}
	return fmt.Sprintf("%s: %s not updated · next attempt %s", phrase, what, next.In(zone).Format("15:04"))
}

// The JSON document of spec 7.5 (D43).
type document struct {
	SchemaVersion int        `json:"schema_version"`
	State         string     `json:"state"`
	GeneratedAt   time.Time  `json:"generated_at"`
	Error         *string    `json:"error"`
	Summary       []jsonItem `json:"summary"`
	Groups        []jsonGrp  `json:"groups"`
}

type jsonGrp struct {
	Name *string   `json:"name"`
	Rows []jsonRow `json:"rows"`
}

type jsonItem struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Value         *float64   `json:"value"`
	Change        *float64   `json:"change"`
	ChangePercent *float64   `json:"change_percent"`
	QuoteTime     *time.Time `json:"quote_time"`
	MarketState   *string    `json:"market_state"`
	Status        string     `json:"status"`
	FetchedAt     *time.Time `json:"fetched_at"`
}

type jsonRow struct {
	Ticker        string     `json:"ticker"`
	Market        string     `json:"market"`
	Name          *string    `json:"name"`
	Currency      string     `json:"currency"`
	Price         *float64   `json:"price"`
	Change        *float64   `json:"change"`
	ChangePercent *float64   `json:"change_percent"`
	DayLow        *float64   `json:"day_low"`
	DayHigh       *float64   `json:"day_high"`
	QuoteTime     *time.Time `json:"quote_time"`
	MarketState   *string    `json:"market_state"`
	Status        string     `json:"status"`
	FetchedAt     *time.Time `json:"fetched_at"`
}

func (v View) marketState(in market.Instrument) *string {
	open, known := market.MarketOpen(in.Hours(), v.want(), v.Snap, v.Now)
	if !known {
		return nil
	}
	s := "closed"
	if open {
		s = "open"
	}
	return &s
}

// JSON writes the one-shot JSON document. Numbers are neither rounded nor
// formatted (D66).
func JSON(w io.Writer, v View) error {
	doc := document{SchemaVersion: 1, GeneratedAt: v.Now.UTC(), Summary: []jsonItem{}, Groups: []jsonGrp{}}
	fresh, withQuote, failed, total := 0, 0, 0, 0
	count := func(in market.Instrument) market.State {
		st, _ := v.state(in)
		e := v.Snap[in]
		total++
		if st == market.Fresh {
			fresh++
		}
		if e.Quote != nil {
			withQuote++
		}
		if e.Fail != nil && !e.Fail.Unknown() {
			failed++
		}
		return st
	}
	for _, in := range v.Cfg.Summary {
		st := count(in)
		item := jsonItem{ID: in.Symbol, Name: names[in.Symbol], Status: string(st), MarketState: v.marketState(in)}
		if e := v.Snap[in]; e.Quote != nil && st != market.NotFound {
			val, pct := change(e.Quote)
			price, qt, ft := e.Quote.Price, e.Quote.Time.UTC(), e.FetchedAt.UTC()
			item.Value, item.Change, item.ChangePercent, item.QuoteTime, item.FetchedAt = &price, &val, &pct, &qt, &ft
		}
		doc.Summary = append(doc.Summary, item)
	}
	for _, g := range v.Cfg.Groups {
		grp := jsonGrp{Rows: []jsonRow{}}
		if g.Name != "" {
			name := g.Name
			grp.Name = &name
		}
		for _, in := range g.Tickers {
			st := count(in)
			currency := "USD"
			if in.Market == market.B3 {
				currency = "BRL"
			}
			row := jsonRow{Ticker: in.Symbol, Market: string(in.Market), Currency: currency, Status: string(st), MarketState: v.marketState(in)}
			if e := v.Snap[in]; e.Quote != nil && st != market.NotFound {
				q := e.Quote
				val, pct := change(q)
				name, price, low, high, qt, ft := q.Name, q.Price, q.Low, q.High, q.Time.UTC(), e.FetchedAt.UTC()
				row.Name, row.Price, row.Change, row.ChangePercent = &name, &price, &val, &pct
				row.DayLow, row.DayHigh, row.QuoteTime, row.FetchedAt = &low, &high, &qt, &ft
			}
			grp.Rows = append(grp.Rows, row)
		}
		doc.Groups = append(doc.Groups, grp)
	}
	// The state of the whole document (D44).
	switch {
	case total > 0 && fresh == total:
		doc.State = "ok"
	case withQuote == 0 && failed > 0:
		doc.State = "error"
		reason := Cause(v)
		doc.Error = &reason
	case fresh == 0 && withQuote > 0:
		doc.State = "stale"
	default:
		doc.State = "partial"
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

// ErrorDocument writes the document for a usage or config error (D44, D45).
func ErrorDocument(w io.Writer, now time.Time, reason string) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(document{SchemaVersion: 1, State: "error", GeneratedAt: now.UTC(), Error: &reason, Summary: []jsonItem{}, Groups: []jsonGrp{}})
}
