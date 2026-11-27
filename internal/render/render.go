package render

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
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
	Sort   Sort           // the order of the rows inside each group; the table keeps file order
	Width  int            // the screen's width, to drop columns from; 0: no limit (the table)

	memo *memo // what one drawing computes once, not per row; set by prepared
}

// memo holds the wanted rows and the state of each market for one drawing.
type memo struct {
	want []market.Instrument
	open map[market.Market][2]bool // open, known
}

// prepared returns v with its memo, made once per drawing.
func (v View) prepared() View {
	if v.memo == nil {
		v.memo = &memo{want: v.Cfg.Want(), open: map[market.Market][2]bool{}}
	}
	return v
}

// marketOpen is market.MarketOpen for v, computed once per market.
func (v View) marketOpen(m market.Market) (open, known bool) {
	if v.memo == nil {
		return market.MarketOpen(m, v.want(), v.Snap, v.Now)
	}
	if r, ok := v.memo.open[m]; ok {
		return r[0], r[1]
	}
	open, known = market.MarketOpen(m, v.memo.want, v.Snap, v.Now)
	v.memo.open[m] = [2]bool{open, known}
	return open, known
}

// Sort is the order of the rows inside each group (D31, D50).
type Sort int

const (
	FileOrder Sort = iota // as in the file
	ByChange              // day change percent, highest first
	ByTicker              // ticker, A to Z
)

// String names the order as the status line gives it.
func (s Sort) String() string {
	switch s {
	case ByChange:
		return "day change"
	case ByTicker:
		return "ticker"
	}
	return "file order"
}

// sorted orders one group's rows by v.Sort; rows without a quote go last,
// in file order (D50).
func (v View) sorted(tickers []market.Instrument) []market.Instrument {
	out := append([]market.Instrument{}, tickers...)
	if v.Sort == FileOrder {
		return out
	}
	sort.SliceStable(out, func(i, j int) bool {
		_, qi := v.dayChange(out[i])
		_, qj := v.dayChange(out[j])
		if qi != qj {
			return qi
		}
		if !qi {
			return false
		}
		if v.Sort == ByTicker {
			return out[i].Symbol < out[j].Symbol
		}
		_, pi := change(v.Snap[out[i]].Quote)
		_, pj := change(v.Snap[out[j]].Quote)
		return pi > pj
	})
	return out
}

// Colours of the terminal's own palette (D25, spec 7.3).
const (
	green = "\x1b[32m"
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

func (v View) want() []market.Instrument {
	if v.memo != nil {
		return v.memo.want
	}
	return v.Cfg.Want()
}

func (v View) state(in market.Instrument) (market.State, time.Duration) {
	open, _ := v.marketOpen(in.Hours())
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
		open, known := v.marketOpen(m)
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

// summaryEntry is one entry of the summary line: its label and value, and
// its percent (coloured), which a narrow screen leaves out first.
type summaryEntry struct {
	base    string
	percent string // "" when the entry has no quote
	cells   int    // the terminal cells of the percent, without colour
}

func (v View) summaryEntries() []summaryEntry {
	var out []summaryEntry
	for _, in := range v.Cfg.Summary {
		label := labels[in.Symbol]
		e := v.Snap[in]
		if e.Quote == nil {
			out = append(out, summaryEntry{base: label + " " + dash})
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
		p := signed(pct, 2, br) + "%"
		out = append(out, summaryEntry{base: label + " " + number(e.Quote.Price, decimals, br), percent: v.paint(p, pct, 2), cells: utf8.RuneCountInString(p)})
	}
	return out
}

const summarySep = "    "

// summaryLine joins the entries. With a width set (the view), a line too
// wide leaves out every entry's percent first, then whole entries from the
// right; an entry is never cut (D96).
func (v View) summaryLine() string {
	entries := v.summaryEntries()
	join := func(n int, percents bool) (string, int) {
		var parts []string
		cells := 0
		for i, e := range entries[:n] {
			s, c := e.base, utf8.RuneCountInString(e.base)
			if percents && e.percent != "" {
				s, c = s+" "+e.percent, c+1+e.cells
			}
			if i > 0 {
				c += len(summarySep)
			}
			parts, cells = append(parts, s), cells+c
		}
		return strings.Join(parts, summarySep), cells
	}
	line, cells := join(len(entries), true)
	if v.Width <= 0 || cells <= v.Width {
		return line
	}
	for n := len(entries); n > 0; n-- {
		if line, cells = join(n, false); cells <= v.Width {
			return line
		}
	}
	return ""
}

// SummaryLine is the line of summary entries (spec 7.3, 7.4), or "" when
// the config hides it.
func SummaryLine(v View) string {
	v = v.prepared()
	if len(v.Cfg.Summary) == 0 {
		return ""
	}
	return v.summaryLine()
}

// MarketLine gives the state of each market: the last line of the table and
// the start of the view's status line (D47, D74).
func MarketLine(v View) string { return v.prepared().marketLine() }

// Lines are the column header and the rows under their group names, aligned
// as one table (D18, D71). The change value and percent are coloured when
// v.Colour is set. Both surfaces draw their rows with it.
func Lines(v View) []string {
	v = v.prepared()
	header := []string{"TICKER", "NAME", "LAST", "CHANGE", "", "LOW", "HIGH", "TIME"}
	rightAligned := []bool{false, false, true, true, true, true, true, false}
	type line struct {
		cells []string // nil: a group name, in text
		text  string
		in    market.Instrument
	}
	lines := []line{{cells: header}}
	for _, g := range v.Cfg.Groups {
		if g.Name != "" {
			lines = append(lines, line{text: g.Name})
		}
		for _, in := range v.sorted(g.Tickers) {
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
	// A narrow screen drops columns in this order: day low and high, name,
	// day change value; ticker, last price, percent and time always remain
	// (D50). The sparkline, first to go, is not drawn yet.
	shown := []bool{true, true, true, true, true, true, true, true}
	total := func() int {
		n, cols := 0, 0
		for i, w := range width {
			if shown[i] {
				n += w
				cols++
			}
		}
		return n + 2*(cols-1)
	}
	for _, drop := range [][]int{{5, 6}, {1}, {3}} {
		if v.Width <= 0 || total() <= v.Width {
			break
		}
		for _, i := range drop {
			shown[i] = false
		}
	}
	if !shown[3] {
		header[4] = header[3]
		if n := utf8.RuneCountInString(header[4]); n > width[4] {
			width[4] = n
		}
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l.cells == nil {
			out = append(out, l.text)
			continue
		}
		var b strings.Builder
		first := true
		for i, c := range l.cells {
			if !shown[i] {
				continue
			}
			pad := strings.Repeat(" ", width[i]-utf8.RuneCountInString(c))
			if !first {
				b.WriteString("  ")
			}
			first = false
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
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return out
}

// Table writes the one-shot table (spec 7.4).
func Table(w io.Writer, v View) {
	v = v.prepared()
	if s := SummaryLine(v); s != "" {
		fmt.Fprintln(w, s)
		fmt.Fprintln(w)
	}
	for _, l := range Lines(v) {
		fmt.Fprintln(w, l)
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
	v = v.prepared()
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
	open, known := v.marketOpen(in.Hours())
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
	v = v.prepared()
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
