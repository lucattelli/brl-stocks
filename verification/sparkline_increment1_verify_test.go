//go:build verification && linux

// The sparkline, increment 1 of slice 4 (research/build/sparkline/scope.md):
// S10.1 to S10.22 on the real view, with the stand-in serving the series in
// the source's own answer (`series` control: exact prices, gaps, prices
// before the session's start and after its end). Outside the criteria: gaps,
// one price, very large and very small ranges, a narrow terminal resized back,
// `print` and `print --json` with a series in the answer, 20 tickers. Built on
// the recommended answers of Q104 (`TODAY` starts at the sparkline's column)
// and Q105 (a price halfway between two levels takes the higher one; recorded,
// not asserted). See README.md.
package verification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

type seriesSpec struct {
	Closes []*float64 `json:"closes"`
	Before []float64  `json:"before,omitempty"`
	After  []float64  `json:"after,omitempty"`
}

type state4 struct {
	state
	Series map[string]seriesSpec `json:"series,omitempty"`
}

func (e *env) setSeries(st state4, what string) {
	e.t.Helper()
	b, _ := json.Marshal(st)
	e.logf("\nSTAND-IN   switched: %s  %s\n", what, b)
	resp, err := http.Post(e.src.url+"/_control", "application/json", bytes.NewReader(b))
	if err != nil {
		e.t.Fatalf("stand-in control: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		e.t.Fatalf("stand-in control: status %d", resp.StatusCode)
	}
}

func prices(v ...float64) []*float64 {
	out := make([]*float64, len(v))
	for i := range v {
		x := v[i]
		out[i] = &x
	}
	return out
}

func ramp(from float64, n int) []*float64 {
	var v []float64
	for i := 0; i < n; i++ {
		v = append(v, from+float64(i))
	}
	return prices(v...)
}

var (
	sparkRE    = regexp.MustCompile(`[▁▂▃▄▅▆▇█]+`)
	timeMarkRE = regexp.MustCompile(`\b\d\d[:/]\d\d(?: ![0-9]+[smhd])?`)
	blocks     = []rune("▁▂▃▄▅▆▇█")
)

// spark returns a row's sparkline, the cell column it starts at and the line.
func spark(s screen, tk string) (string, int, int) {
	y := s.lineOf(tk)
	if y < 0 {
		return "", -1, -1
	}
	l := s.lines[y]
	m := sparkRE.FindAllStringIndex(l, -1)
	if len(m) == 0 {
		return "", -1, y
	}
	last := m[len(m)-1]
	return l[last[0]:last[1]], s.xs[y][last[0]], y
}

// timeEnd returns the cell column just after a row's quote time with its
// stale mark.
func timeEnd(s screen, y int) int {
	l := s.lines[y]
	m := timeMarkRE.FindAllStringIndex(l, -1)
	if len(m) == 0 {
		return -1
	}
	last := m[len(m)-1]
	return s.xs[y][last[1]-1] + 1
}

func headerLine(s screen) int {
	for i, l := range s.lines {
		if f := strings.Fields(l); len(f) > 0 && f[0] == "TICKER" {
			return i
		}
	}
	return -1
}

// blankFrom reports whether line y has only blanks from cell column x on.
func blankFrom(s screen, y, x int) bool {
	if y < 0 {
		return false
	}
	for c := x; c < len(s.cells[y]); c++ {
		if t := strings.TrimSpace(s.cells[y][c].content); t != "" {
			return false
		}
	}
	return true
}

func widestLine(s screen) int {
	w := 0
	for y, l := range s.lines {
		if l == "" {
			continue
		}
		if x := s.xs[y][len(l)-1] + 1; x > w {
			w = x
		}
	}
	return w
}

// sparkView opens the view with a series state, waits for the rows (and, when
// not nil, for cond), and returns the view and the screen.
func (e *env) sparkView(cols int, tks []string, cond func(s screen) bool) (*view, screen) {
	e.t.Helper()
	v := e.openViewSized(cols, viewRows, nil)
	want := rowsShown(tks)
	if cond != nil {
		want = func(s screen) bool { return rowsShown(tks)(s) && cond(s) }
	}
	s, _ := v.waitStable("every row with a price", 15*time.Second, 300*time.Millisecond, want)
	v.logScreen("SCREEN", s, nil)
	return v, s
}

func wantSpark(c *ck, s screen, tk, want string) {
	c.t.Helper()
	got, _, _ := spark(s, tk)
	c.notef("%s: sparkline %q (want %q); row %q", tk, got, want, strings.Join(s.rowLines(tk), ""))
	if got != want {
		c.failf("%s: sparkline %q, want %q", tk, got, want)
	}
}

const s13 = "▁▂▃▃▄▅▆▆▇█"

// One view: PETR4 (B3 open) rises steadily with 24 made-up prices; AAPL (US
// closed 2 h ago) has the series 10 to 19; VALE3 the same price 10 times; ITUB4
// 4 prices; MSFT 25 prices, the first the lowest and the latest the highest;
// BBDC4 no prices; XXXX9 not found.
// S10.1, S10.2, S10.4, S10.5, S10.6, S10.7, S10.10, S10.11, S10.12, S10.13,
// S10.14, S10.15, S10.17, S10.20 (and Q104).
func TestVerifyView_S10_SparklineShapes(t *testing.T) {
	t.Parallel()
	cfg := "tickers = [\"PETR4\", \"AAPL\", \"VALE3\", \"ITUB4\", \"MSFT\", \"BBDC4\", \"XXXX9\"]\n"
	e := newEnv(t, "S10: sparkline shapes on one screen", cfg)
	m25 := []float64{5}
	for i := 0; i < 23; i++ {
		m25 = append(m25, 6+float64((i*7)%11))
	}
	m25 = append(m25, 30)
	e.setSeries(state4{state: state{ClosedMarkets: []string{"us"}, SymbolStatus: map[string]int{"XXXX9.SA": 404}},
		Series: map[string]seriesSpec{
			"AAPL":     {Closes: ramp(10, 10)},
			"VALE3.SA": {Closes: prices(61, 61, 61, 61, 61, 61, 61, 61, 61, 61)},
			"ITUB4.SA": {Closes: prices(34, 35, 34.5, 36)},
			"MSFT":     {Closes: prices(m25...)},
			"BBDC4.SA": {Closes: []*float64{}},
		}}, "US closed 2 h ago; series for AAPL, VALE3, ITUB4, MSFT, BBDC4; XXXX9 404")
	tks := []string{"PETR4", "AAPL", "VALE3", "ITUB4", "MSFT", "BBDC4"}
	v, s := e.sparkView(viewCols, tks, func(s screen) bool {
		return strings.Contains(strings.Join(s.rowLines("XXXX9"), ""), "not found")
	})
	v.done2()
	hy := headerLine(s)
	_, col, _ := spark(s, "PETR4")
	crit(e, "S10.1", "an open market's row ends with a sparkline of the day so far", func(c *ck) {
		sp, x, y := spark(s, "PETR4")
		if sp == "" {
			c.failf("PETR4: no sparkline in %q", s.rowLines("PETR4"))
			return
		}
		if !blankFrom(s, y, x+len([]rune(sp))) {
			c.failf("PETR4: the row does not end with the sparkline: %q", s.lines[y])
		}
		c.notef("PETR4 %q", s.lines[y])
	})
	crit(e, "S10.2", "a closed market's row shows the last trading day", func(c *ck) {
		c.notef("status line %q", statusLine(s))
		if !strings.Contains(statusLine(s), "US closed") {
			c.failf("set-up: US not closed")
		}
		wantSpark(c, s, "AAPL", s13)
	})
	crit(e, "S10.4", "more than 10 prices: 10 characters", func(c *ck) {
		for _, tk := range []string{"PETR4", "MSFT"} {
			sp, _, _ := spark(s, tk)
			if n := len([]rune(sp)); n != 10 {
				c.failf("%s: sparkline %q is %d wide, want 10", tk, sp, n)
			}
		}
	})
	colour := func(c *ck, tk string, want []int) {
		sp, x, y := spark(s, tk)
		if sp == "" {
			c.failf("%s: no sparkline", tk)
			return
		}
		cs := s.cells[y][x : x+len([]rune(sp))]
		c.notef("%s colours: %s", tk, cellsColors(cs))
		for _, cl := range cs {
			i, ok := paletteIndex(cl.fg)
			if want == nil {
				if cl.fg != nil {
					c.failf("%s: a sparkline cell has the colour %s, want none", tk, fmtColor(cl.fg))
					return
				}
				continue
			}
			if !ok || (i != want[0] && i != want[1]) {
				c.failf("%s: a sparkline cell has %s, want palette %d or %d", tk, fmtColor(cl.fg), want[0], want[1])
				return
			}
		}
	}
	crit(e, "S10.5", "a positive day change: the sparkline in the palette's green", func(c *ck) { colour(c, "PETR4", []int{2, 10}) })
	crit(e, "S10.6", "a negative day change: the sparkline in the palette's red", func(c *ck) { colour(c, "AAPL", []int{1, 9}) })
	crit(e, "S10.7", "a zero day change: no colour", func(c *ck) {
		c.notef("VALE3 row %q", s.rowLines("VALE3"))
		colour(c, "VALE3", nil)
	})
	crit(e, "S10.10", "rising all day: lowest on the left, highest on the right", func(c *ck) {
		sp := []rune(func() string { x, _, _ := spark(s, "PETR4"); return x }())
		if len(sp) == 0 {
			c.failf("no sparkline")
			return
		}
		lo, hi := sp[0], sp[0]
		for _, r := range sp {
			if r < lo {
				lo = r
			}
			if r > hi {
				hi = r
			}
		}
		c.notef("PETR4 %q", string(sp))
		if sp[0] != lo || sp[len(sp)-1] != hi || lo == hi {
			c.failf("PETR4 %q: want the lowest first and the highest last", string(sp))
		}
	})
	crit(e, "S10.11", "4 prices: 4 characters", func(c *ck) {
		sp, _, _ := spark(s, "ITUB4")
		c.notef("ITUB4 %q", sp)
		if n := len([]rune(sp)); n != 4 {
			c.failf("ITUB4: %q is %d wide, want 4", sp, n)
		}
	})
	crit(e, "S10.12", "25 prices, the first lowest, the latest highest: starts ▁, ends █", func(c *ck) {
		sp := []rune(func() string { x, _, _ := spark(s, "MSFT"); return x }())
		c.notef("MSFT %q", string(sp))
		if len(sp) == 0 || sp[0] != '▁' || sp[len(sp)-1] != '█' {
			c.failf("MSFT %q: want ▁ first and █ last", string(sp))
		}
	})
	crit(e, "S10.13", "the prices 10 to 19 read ▁▂▃▃▄▅▆▆▇█", func(c *ck) { wantSpark(c, s, "AAPL", s13) })
	crit(e, "S10.14", "the same price 10 times reads ▄▄▄▄▄▄▄▄▄▄", func(c *ck) { wantSpark(c, s, "VALE3", "▄▄▄▄▄▄▄▄▄▄") })
	crit(e, "S10.15", "a not-found row: the sparkline column is blank", func(c *ck) {
		y := s.lineOf("XXXX9")
		c.notef("XXXX9 %q", strings.Join(s.rowLines("XXXX9"), ""))
		if col < 0 || !blankFrom(s, y, col) {
			c.failf("XXXX9: something in the sparkline column (from %d): %q", col, strings.Join(s.rowLines("XXXX9"), ""))
		}
	})
	crit(e, "S10.17", "a quote whose series has no prices: blank", func(c *ck) {
		y := s.lineOf("BBDC4")
		c.notef("BBDC4 %q", strings.Join(s.rowLines("BBDC4"), ""))
		if !hasQuote(strings.Join(s.rowLines("BBDC4"), "")) {
			c.failf("set-up: BBDC4 has no quote")
		}
		if col < 0 || !blankFrom(s, y, col) {
			c.failf("BBDC4: something in the sparkline column: %q", strings.Join(s.rowLines("BBDC4"), ""))
		}
	})
	crit(e, "S10.20", "the column is headed TODAY (Q104: at the sparkline's column)", func(c *ck) {
		if hy < 0 {
			c.failf("no column header")
			return
		}
		h := s.lines[hy]
		c.notef("header %q", h)
		i := strings.Index(h, "TODAY")
		if i < 0 {
			c.failf("no TODAY in the header %q", h)
			return
		}
		if x := s.xs[hy][i]; x != col {
			c.failf("TODAY starts at column %d, the sparklines at %d (Q104)", x, col)
		}
	})
}

// S10.3 [view] Given the view, when any key from section 7.3 is pressed, then
// no alert, detail view or news appears. Keys: r, s (three times), up, down,
// j, k, ? and a key to close it. After each, every line that is not a row is
// one that was on the screen before (the status line's sort may change).
func TestVerifyView_S10_3_NoDetailOnKeys(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S10.3: keys of 7.3 open nothing", defaultIntervalConfig)
	v, s0 := e.sparkView(viewCols, tickers, nil)
	nonRows := func(s screen) map[string]bool {
		m := map[string]bool{}
		st := statusLine(s)
		for _, l := range s.lines {
			t := strings.TrimSpace(l)
			if t == "" || t == st {
				continue
			}
			if f := strings.Fields(t); len(f) > 0 {
				isRow := false
				for _, tk := range tickers {
					if f[0] == tk {
						isRow = true
					}
				}
				if isRow {
					continue
				}
			}
			m[t] = true
		}
		return m
	}
	base := nonRows(s0)
	var seen []string
	for _, k := range []struct {
		name string
		b    []byte
	}{{"r", []byte("r")}, {"s", []byte("s")}, {"s", []byte("s")}, {"s", []byte("s")}, {"down", v.arrow('B')}, {"up", v.arrow('A')}, {"j", []byte("j")}, {"k", []byte("k")}} {
		s, _ := v.pressAndSettle(k.b, k.name)
		for l := range nonRows(s) {
			if !base[l] {
				seen = append(seen, k.name+": "+l)
			}
		}
	}
	sh, _ := v.pressAndSettle([]byte("?"), "?")
	sx, _ := v.pressAndSettle([]byte("x"), "x (closes the help)")
	for l := range nonRows(sx) {
		if !base[l] {
			seen = append(seen, "after ? and x: "+l)
		}
	}
	v.done2()
	crit(e, "S10.3", "no alert, detail view or news after any key", func(c *ck) {
		c.notef("the help (7.3) after ?: %q", viewBody(sh))
		for _, l := range seen {
			c.failf("a new line appeared: %q", l)
		}
	})
}

// S10.8 [view] Given a terminal one cell narrower than the widest row with its
// sparkline, when the view is drawn, then no row has a sparkline.
// S10.21 ... then the column header has no `TODAY`.
// The widest line is measured at 120 columns; the view is resized to one less,
// started at one less, and (outside the criteria) resized back to 120.
func TestVerifyView_S10_8_21_OneCellNarrower(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S10.8, S10.21: one cell narrower than the widest row", defaultIntervalConfig)
	v, s0 := e.sparkView(viewCols, tickers, nil)
	w := widestLine(s0)
	e.logf("WIDEST LINE at %d columns: %d cells\n", viewCols, w)
	sN := v.resizeAndSettle(w-1, viewRows)
	sW := v.resizeAndSettle(w, viewRows)
	sB := v.resizeAndSettle(viewCols, viewRows)
	v.done2()
	v2 := e.openViewSized(w-1, viewRows, nil)
	sS, _ := v2.waitStable("rows", 10*time.Second, 300*time.Millisecond, rowsShown(tickers))
	v2.logScreen("STARTED ONE CELL NARROWER", sS, nil)
	v2.done2()
	noSpark := func(c *ck, s screen, what string) {
		for _, tk := range tickers {
			if sp, _, _ := spark(s, tk); sp != "" {
				c.failf("%s: %s still has a sparkline %q", what, tk, sp)
			}
		}
	}
	crit(e, "S10.8", "one cell narrower: no row has a sparkline", func(c *ck) {
		c.notef("widest %d; at %d: %q", w, w-1, viewBody(sN))
		noSpark(c, sN, "resized")
		noSpark(c, sS, "started")
		for _, tk := range tickers {
			if sp, _, _ := spark(sW, tk); sp == "" {
				c.failf("control: at %d columns %s has no sparkline", w, tk)
			}
			if sp, _, _ := spark(sB, tk); sp == "" {
				c.failf("resized back to %d: %s has no sparkline (outside the criteria, 7.3 the view follows a resize)", viewCols, tk)
			}
		}
	})
	crit(e, "S10.21", "one cell narrower: no TODAY in the header", func(c *ck) {
		for _, x := range []struct {
			n string
			s screen
		}{{"resized", sN}, {"started", sS}} {
			if hy := headerLine(x.s); hy < 0 || strings.Contains(x.s.lines[hy], "TODAY") {
				c.failf("%s: header %q", x.n, x.s.lines[max(hy, 0)])
			}
		}
		if hy := headerLine(sW); hy < 0 || !strings.Contains(sW.lines[hy], "TODAY") {
			c.failf("control: at %d columns the header has no TODAY", w)
		}
	})
}

// S10.9 [view] Given the source's answer holds prices from before the regular
// session opened or after it closed, when the row is drawn, then its sparkline
// does not include them. AAPL (US closed 2 h ago): before the session 1000 and
// 5, after it 0.01 and 900; PETR4 (B3 open): before the session 1000 and 0.01.
// Both sessions are 10 to 19: the line must read ▁▂▃▃▄▅▆▆▇█ as without them.
func TestVerifyView_S10_9_OutsideTheSession(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S10.9: prices outside the session in the answer", defaultIntervalConfig)
	e.setSeries(state4{state: state{ClosedMarkets: []string{"us"}}, Series: map[string]seriesSpec{
		"AAPL":     {Closes: ramp(10, 10), Before: []float64{1000, 5}, After: []float64{0.01, 900}},
		"PETR4.SA": {Closes: ramp(10, 10), Before: []float64{1000, 0.01}},
		"MSFT":     {Closes: ramp(10, 10), After: []float64{0.01}},
		"VALE3.SA": {Closes: ramp(10, 10)},
	}}, "prices before and after the sessions")
	v, s := e.sparkView(viewCols, tickers, nil)
	v.done2()
	crit(e, "S10.9", "prices before the open and after the close do not change the line", func(c *ck) {
		for _, tk := range []string{"AAPL", "PETR4", "MSFT", "VALE3"} {
			wantSpark(c, s, tk, s13)
		}
	})
}

// S10.16 [view] Given a row with no data yet, when the view is drawn at full
// width, then its sparkline column is blank. Empty cache, answers held 20 s:
// each row is its ticker and the 7 data columns' `—` (S1.5), and nothing after
// the quote time's `—` (no `—`, no block, in the sparkline's place).
func TestVerifyView_S10_16_NoDataYet(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S10.16: no data yet", defaultIntervalConfig)
	e.setSeries(state4{state: state{DelayMS: 20000}}, "answers held 20 s")
	v := e.openViewSized(viewCols, viewRows, nil)
	s0, _ := v.waitStable("every row", 10*time.Second, 300*time.Millisecond, func(s screen) bool {
		for _, tk := range tickers {
			if len(s.rowLines(tk)) != 1 {
				return false
			}
		}
		return true
	})
	v.logScreen("NO DATA YET", s0, nil)
	v.done2()
	crit(e, "S10.16", "a row with no data yet: the sparkline column is blank", func(c *ck) {
		hy := headerLine(s0)
		if hy >= 0 {
			c.notef("header %q", s0.lines[hy])
		}
		for _, tk := range tickers {
			l := strings.Join(s0.rowLines(tk), "")
			f := strings.Fields(l)
			c.notef("%s %q", tk, l)
			if len(f) != 8 || dashCount(f[1:]) != 7 {
				c.failf("%s: want the ticker and 7 `—`, nothing in the sparkline's place; got %q", tk, l)
			}
			if sparkRE.MatchString(l) {
				c.failf("%s: a sparkline with no data: %q", tk, l)
			}
		}
	})
}

// S10.19 [view] Given a cache left by an earlier run whose row had a
// sparkline, and a source that has not answered yet, when the view opens at
// full width, then the cached row shows that sparkline. A view run with
// PETR4's series 10 to 19 leaves the cache; then answers are held 20 s and a
// second view opens.
func TestVerifyView_S10_19_CachedSparkline(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S10.19: the cached row keeps its sparkline", defaultIntervalConfig)
	e.setSeries(state4{Series: map[string]seriesSpec{"PETR4.SA": {Closes: ramp(10, 10)}}}, "PETR4 10 to 19")
	v1, s1 := e.sparkView(viewCols, tickers, nil)
	v1.done2()
	e.setSeries(state4{state: state{DelayMS: 20000}}, "answers held 20 s; no series control")
	v2 := e.openViewSized(viewCols, viewRows, nil)
	s2, _ := v2.waitStable("cached rows", 10*time.Second, 300*time.Millisecond, rowsShown(tickers))
	v2.logScreen("CACHED ROWS, NO ANSWER YET", s2, nil)
	r2 := v2.done2()
	crit(e, "S10.19", "the cached row shows its sparkline before any answer", func(c *ck) {
		for _, q := range r2.reqs {
			if q.Status > 0 {
				c.failf("set-up: an answer arrived during the cached run (%s)", q.Symbol)
			}
		}
		c.notef("requests during the cached run: %d", len(r2.reqs))
		wantSpark(c, s1, "PETR4", s13)
		wantSpark(c, s2, "PETR4", s13)
	})
}

// S10.18 [view] Given a row with a sparkline whose next refresh fails, when the
// view is drawn at full width, then the stale row shows the same sparkline.
// Interval 10 s; PETR4 10 to 19; then 503 for everything until PETR4 is stale.
func TestVerifyView_S10_18_StaleKeepsSparkline(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S10.18: a failed refresh keeps the sparkline", baseConfig)
	e.setSeries(state4{Series: map[string]seriesSpec{"PETR4.SA": {Closes: ramp(10, 10)}}}, "PETR4 10 to 19")
	v, s0 := e.sparkView(viewCols, tickers, nil)
	e.setSeries(state4{state: state{Status: 503}}, "every request answered 503")
	s1, _ := v.waitStable("PETR4 stale", 20*time.Second, 300*time.Millisecond, func(s screen) bool {
		_, _, _, ok := staleMark(strings.Join(s.rowLines("PETR4"), ""))
		return ok
	})
	v.logScreen("STALE", s1, nil)
	v.done2()
	crit(e, "S10.18", "the stale row shows the same sparkline", func(c *ck) {
		b, _, _ := spark(s0, "PETR4")
		wantSpark(c, s1, "PETR4", b)
		for _, tk := range tickers {
			b, _, _ := spark(s0, tk)
			a, _, _ := spark(s1, tk)
			if a != b {
				c.failf("%s: sparkline %q before, %q stale", tk, b, a)
			}
		}
	})
}

// S10.22 [view] Given rows whose quote times and stale marks differ in width,
// when the view is drawn at full width, then every sparkline starts two spaces
// after the end of the widest quote time with its stale mark.
// `print` leaves the cache; 12 s later VALE3 answers 503 to the view's fetch
// (`!12s`), the other rows answer. Interval 10 s. Also with 20 tickers.
func TestVerifyView_S10_22_AlignedAfterWidestTime(t *testing.T) {
	t.Parallel()
	for _, p := range []struct {
		name string
		cfg  cfgSpec
	}{{"own_rows", cfgSpec{interval: 10, top: []string{"PETR4", "VALE3", "AAPL"}}}, {"20_tickers", cfgSpec{interval: 10, top: []string{"PETR4", "VALE3", "AAPL"}}.pad20()}} {
		p := p
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, "S10.22: one stale row; "+p.name, p.cfg.String())
			e.wantExit(e.run("print"), 0)
			time.Sleep(12 * time.Second)
			e.setSource(state{SymbolStatus: map[string]int{"VALE3.SA": 503}}, "VALE3.SA 503")
			v, s := e.sparkView(viewCols, p.cfg.all(), func(s screen) bool {
				_, _, _, ok := staleMark(strings.Join(s.rowLines("VALE3"), ""))
				return ok
			})
			v.done2()
			crit(e, "S10.22", "every sparkline starts two spaces after the widest quote time with its mark", func(c *ck) {
				widest := -1
				for _, tk := range p.cfg.all() {
					if y := s.lineOf(tk); y >= 0 {
						if x := timeEnd(s, y); x > widest {
							widest = x
						}
					}
				}
				c.notef("the widest quote time with its mark ends at column %d", widest)
				for _, tk := range p.cfg.all() {
					sp, x, _ := spark(s, tk)
					if sp == "" {
						c.failf("%s: no sparkline", tk)
						continue
					}
					if x != widest+2 {
						c.failf("%s: sparkline at column %d, want %d (row %q)", tk, x, widest+2, strings.Join(s.rowLines(tk), ""))
					}
				}
				if hy := headerLine(s); hy >= 0 {
					if i := strings.Index(s.lines[hy], "TODAY"); i < 0 || s.xs[hy][i] != widest+2 {
						c.failf("TODAY not at column %d: %q", widest+2, s.lines[hy])
					}
				}
			})
		})
	}
}

// Outside the criteria: a series with gaps, one price, a very large and a very
// small range, a price exactly halfway between two levels (Q105), recorded.
func TestVerifyView_SparklineEdgeCases_Observations(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "observation: gaps, one price, ranges, halfway", defaultIntervalConfig)
	gaps := []*float64{prices(10)[0], nil, prices(12)[0], nil, nil, prices(15)[0], prices(19)[0]}
	e.setSeries(state4{Series: map[string]seriesSpec{
		"PETR4.SA": {Closes: gaps},
		"AAPL":     {Closes: prices(229.87)},
		"VALE3.SA": {Closes: prices(0.01, 50000, 100000, 3, 99999.99)},
		"ITUB4.SA": {Closes: prices(34.150, 34.151, 34.152, 34.151, 34.153)},
		"MSFT":     {Closes: prices(0, 1, 14)},
	}}, "gaps, one price, large and small ranges, halfway")
	v, s := e.sparkView(viewCols, tickers, nil)
	v.done2()
	crit(e, "edge cases (observation)", "the sparklines drawn", func(c *ck) {
		for _, x := range []struct{ tk, what string }{
			{"PETR4", "10, gap, 12, gap, gap, 15, 19"},
			{"AAPL", "one price"},
			{"VALE3", "0.01, 50000, 100000, 3, 99999.99"},
			{"ITUB4", "34.150 to 34.153"},
			{"MSFT", "0, 1, 14 (1 is level 0.5: Q105 says the higher, ▂)"},
		} {
			sp, _, _ := spark(s, x.tk)
			c.notef("%s (%s): %q", x.tk, x.what, sp)
		}
	})
}

// Outside the criteria: `print` and `print --json` with a series in the
// answer: no sparkline in the table (7.4) and no series in the document (7.5).
func TestVerifyPrint_NoSparklineNoSeries(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "print with a series in the answer", baseConfig)
	e.setSeries(state4{Series: map[string]seriesSpec{"PETR4.SA": {Closes: ramp(10, 10)}}}, "PETR4 10 to 19")
	r := e.run("print", "--refresh")
	e.wantExit(r, 0)
	rj := e.run("print", "--json", "--refresh")
	time.Sleep(6 * time.Second)
	rj = e.run("print", "--json", "--refresh")
	e.wantExit(rj, 0)
	crit(e, "7.4 no sparkline", "the table has no sparkline and no TODAY", func(c *ck) {
		if sparkRE.MatchString(r.stdout) || strings.Contains(r.stdout, "TODAY") {
			c.failf("the table holds a sparkline or TODAY:\n%s", r.stdout)
		}
	})
	crit(e, "7.5 no series", "the document has no intraday series", func(c *ck) {
		doc := parseDoc(e, rj.stdout)
		rows := docRows(doc)
		row := rows["PETR4"]
		c.notef("PETR4 keys: %v", keysOf(row))
		for k := range row {
			lk := strings.ToLower(k)
			if strings.Contains(lk, "series") || strings.Contains(lk, "spark") || strings.Contains(lk, "intraday") || strings.Contains(lk, "closes") {
				c.failf("PETR4 has a key %q", k)
			}
		}
		for _, b := range []string{"\"series\"", "sparkline", "intraday", "\"timestamp\""} {
			if strings.Contains(rj.stdout, b) {
				c.failf("the document holds %s", b)
			}
		}
		_ = fmt.Sprint
	})
}

func keysOf(m map[string]any) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	return k
}
