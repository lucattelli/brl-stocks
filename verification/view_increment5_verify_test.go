//go:build verification && linux

// The full-screen view, increment 5 of slice 2: narrow terminal and keys
// (S1.6, S2.4, S9.2 to S9.10) and QR11, through the real view on a
// pseudo-terminal with the helpers of view_verify_test.go. Resizes are real:
// the pseudo-terminal's size is changed with TIOCSWINSZ (the kernel then
// signals the tool) and SIGWINCH is also sent to the tool; the emulator is
// resized first, as a terminal window would be. Keys are real bytes: the
// arrows as a terminal sends them (CSI A/B, or SS3 A/B when the tool switched
// on application cursor keys), `j`, `k`, `s`, `?`.
//
// Open questions, not checked as if decided: Q84 (whether left and right
// arrows scroll) and Q85 (whether scrolling counts group header lines).
// Scrolling is checked without groups; what left/right and group headers do
// is recorded as an observation only. See README.md.
package verification

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

// resize changes the terminal's size: the emulator, then the pseudo-terminal
// (TIOCSWINSZ), then SIGWINCH to the tool.
func (v *view) resize(cols, rows int) {
	v.e.t.Helper()
	v.mu.Lock()
	v.emu.Resize(cols, rows)
	v.mu.Unlock()
	if err := v.p.setSize(cols, rows); err != nil {
		v.e.failf("TIOCSWINSZ: %v", err)
	}
	syscall.Kill(v.cmd.Process.Pid, syscall.SIGWINCH)
	v.e.logf("RESIZE     %dx%d at +%s (TIOCSWINSZ and SIGWINCH)\n", cols, rows, time.Since(v.start).Round(time.Millisecond))
}

// arrow returns the bytes a terminal sends for an arrow key (A up, B down, C
// right, D left), following the tool's application-cursor-keys mode.
func (v *view) arrow(dir byte) []byte {
	v.mu.Lock()
	app := v.modes["1"]
	v.mu.Unlock()
	if app {
		return []byte{0x1b, 'O', dir}
	}
	return []byte{0x1b, '[', dir}
}

// pressAndSettle presses a key and waits until the screen has changed and
// settled (or, when nothing changes, until 2 s have passed). It returns the
// screen and whether it changed.
func (v *view) pressAndSettle(b []byte, what string) (screen, bool) {
	v.e.t.Helper()
	before := v.snap().text()
	v.key(b, what)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v.snap().text() != before {
			s, _ := v.waitStable("the screen after "+what, 3*time.Second, 200*time.Millisecond, func(screen) bool { return true })
			return s, true
		}
		select {
		case <-v.notify:
		case <-time.After(20 * time.Millisecond):
		}
	}
	return v.snap(), false
}

// visibleRows returns the tickers of the rows on screen, top to bottom.
func visibleRows(s screen, known []string) []string {
	k := map[string]bool{}
	for _, t := range known {
		k[t] = true
	}
	var out []string
	for _, l := range s.lines {
		if f := strings.Fields(l); len(f) > 0 && k[f[0]] {
			out = append(out, f[0])
		}
	}
	return out
}

func indexOf(l []string, x string) int {
	for i, y := range l {
		if y == x {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// S1.6 Narrow terminal
// ---------------------------------------------------------------------------

// S1.6 [view] Given a terminal too narrow for the full row, when the view is
// drawn, then ticker, last price, day change percent and quote time are still
// shown. 7.3: the view follows a resize; columns go in the order sparkline,
// low and high, name, change value (recorded). Two ways: a view started at 40
// columns, and a view started at 120 and resized down to 100, 80, 70, 60, 50
// and 40.
func TestVerifyView_S1_6_NarrowTerminal(t *testing.T) {
	t.Parallel()
	want := map[string][]string{
		"PETR4": {"R$ 38,42", "+0,81%"}, "AAPL": {"US$ 229.87", "-0.48%"},
		"VALE3": {"R$ 61,07", "+0,00%"}, "ITUB4": {"R$ 34,15", "-1,01%"}, "MSFT": {"US$ 512.30", "+0.40%"},
	}
	hhmm := regexp.MustCompile(`\b\d\d:\d\d\b`)
	required := func(s screen) []string {
		var miss []string
		for tk, w := range want {
			ls := s.rowLines(tk)
			if len(ls) != 1 {
				miss = append(miss, tk+": no row")
				continue
			}
			for _, x := range w {
				if !strings.Contains(ls[0], x) {
					miss = append(miss, fmt.Sprintf("%s: %q missing in %q", tk, x, ls[0]))
				}
			}
			if !hhmm.MatchString(ls[0]) {
				miss = append(miss, fmt.Sprintf("%s: no quote time in %q", tk, ls[0]))
			}
		}
		sort.Strings(miss)
		return miss
	}
	columns := func(s screen) string {
		l := ""
		if ls := s.rowLines("PETR4"); len(ls) == 1 {
			l = ls[0]
		}
		var have []string
		for _, c := range []struct{ name, tok string }{{"name", "Petrobras"}, {"value", "+0,31"}, {"low", "38,02"}, {"high", "38,60"}} {
			if strings.Contains(l, c.tok) {
				have = append(have, c.name)
			}
		}
		return fmt.Sprintf("optional columns shown: %v; widest line %d; PETR4 %q", have, widest(s), l)
	}
	t.Run("started_at_40", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, "S1.6 (view, started at 40 columns)", baseConfig)
		v := e.openViewSized(40, viewRows, nil)
		s, _ := v.waitStable("every row with a price", 10*time.Second, 300*time.Millisecond, rowsShown(tickers))
		v.logScreen("SCREEN AT 40 COLUMNS", s, nil)
		v.quit([]byte("q"), "q")
		crit(e, "S1.6", "at 40 columns: ticker, last price, percent and quote time remain", func(c *ck) {
			for _, m := range required(s) {
				c.failf("%s", m)
			}
			c.notef("%s", columns(s))
		})
	})
	t.Run("resized", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, "S1.6 (view, resized from 120 to 40 columns)", baseConfig)
		v := e.openView(nil)
		s, _ := v.waitStable("every row with a price", 10*time.Second, 300*time.Millisecond, rowsShown(tickers))
		full := widest(s)
		e.logf("OBSERVED   full row at 120 columns: widest line %d\n", full)
		type step struct {
			cols int
			s    screen
		}
		var steps []step
		for _, w := range []int{100, 80, 70, 60, 50, 40} {
			v.resize(w, viewRows)
			s, _ := v.waitStable(fmt.Sprintf("the view redrawn at %d columns", w), 5*time.Second, 300*time.Millisecond, func(s screen) bool {
				return rowsShown(tickers)(s) && widest(s) <= w
			})
			v.logScreen(fmt.Sprintf("SCREEN AT %d COLUMNS", w), s, nil)
			e.logf("OBSERVED   %d columns: %s\n", w, columns(s))
			steps = append(steps, step{w, s})
		}
		v.resize(120, viewRows)
		back, _ := v.waitStable("the view back at 120 columns", 5*time.Second, 300*time.Millisecond, func(s screen) bool {
			return rowsShown(tickers)(s) && strings.Contains(strings.Join(s.rowLines("PETR4"), ""), "Petrobras")
		})
		v.logScreen("SCREEN BACK AT 120 COLUMNS", back, nil)
		v.quit([]byte("q"), "q")
		crit(e, "S1.6", "after each resize: ticker, last price, percent and quote time remain", func(c *ck) {
			for _, st := range steps {
				for _, m := range required(st.s) {
					c.failf("at %d columns: %s", st.cols, m)
				}
				if widest(st.s) > st.cols {
					c.failf("at %d columns a line is %d wide", st.cols, widest(st.s))
				}
			}
		})
	})
}

func widest(s screen) int {
	n := 0
	for _, l := range s.lines {
		if w := len([]rune(l)); w > n {
			n = w
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// S9.2 to S9.5, S9.10 Sort
// ---------------------------------------------------------------------------

// Day change percents of the stand-in: PETR4 +0.81, MSFT +0.40, VALE3 0,
// AAPL -0.48, ITUB4 -1.01.
var byChange = []string{"PETR4", "MSFT", "VALE3", "AAPL", "ITUB4"}

// S9.2 [view] Given file order, when `s` is pressed once, then rows are
// ordered by day change. S9.3 ... then by ticker. S9.4 ... back in file order.
// No groups; the file order differs from both other orders.
func TestVerifyView_S9_2_4_SortCycle(t *testing.T) {
	t.Parallel()
	file := []string{"VALE3", "ITUB4", "MSFT", "AAPL", "PETR4"}
	e := newEnv(t, "S9.2, S9.3, S9.4 (view)", cfgSpec{top: file}.String())
	v := e.openView(nil)
	s0, _ := v.waitStable("every row with a price", 10*time.Second, 300*time.Millisecond, rowsShown(file))
	v.logScreen("SCREEN IN FILE ORDER", s0, nil)
	var ss []screen
	for i := 0; i < 3; i++ {
		s, _ := v.pressAndSettle([]byte("s"), "s")
		v.logScreen(fmt.Sprintf("SCREEN AFTER s #%d", i+1), s, nil)
		ss = append(ss, s)
	}
	v.quit([]byte("q"), "q")
	byTicker := append([]string(nil), file...)
	sort.Strings(byTicker)
	check := func(id string, s screen, want []string, text string) {
		crit(e, id, text, func(c *ck) {
			if got := visibleRows(s, file); strings.Join(got, " ") != strings.Join(want, " ") {
				c.failf("want %v, got %v", want, got)
			}
			c.notef("status line %q", statusLine(s))
		})
	}
	check("S9.2", ss[0], byChange, "the first `s`: by day change, highest first")
	check("S9.3", ss[1], byTicker, "the second `s`: by ticker, A to Z")
	check("S9.4", ss[2], file, "the third `s`: back in file order")
}

// S9.5 [view] Given two groups and the order by day change, when the view is
// drawn, then each group's rows run from the highest percent to the lowest.
// S9.10 ... then the groups are still in file order. Groups US (AAPL, MSFT)
// then Brasil (VALE3, ITUB4, PETR4): not alphabetical.
func TestVerifyView_S9_5_10_SortInsideGroups(t *testing.T) {
	t.Parallel()
	cfg := cfgSpec{groups: []groupSpec{{"US", []string{"AAPL", "MSFT"}}, {"Brasil", []string{"VALE3", "ITUB4", "PETR4"}}}}
	e := newEnv(t, "S9.5, S9.10 (view)", cfg.String())
	v := e.openView(nil)
	v.waitStable("every row with a price", 10*time.Second, 300*time.Millisecond, rowsShown(cfg.all()))
	s, _ := v.pressAndSettle([]byte("s"), "s")
	v.logScreen("SCREEN AFTER s", s, nil)
	v.quit([]byte("q"), "q")
	var lines []string
	for _, l := range viewBody(s) {
		lines = append(lines, strings.Fields(l)[0])
	}
	crit(e, "S9.5", "inside each group, rows run from the highest percent to the lowest", func(c *ck) {
		if got, want := strings.Join(lines, " "), "US MSFT AAPL Brasil PETR4 VALE3 ITUB4"; got != want {
			c.failf("want the lines %q, got %q", want, got)
		}
	})
	crit(e, "S9.10", "the groups are still in file order", func(c *ck) {
		if indexOf(lines, "US") < 0 || indexOf(lines, "Brasil") < 0 || indexOf(lines, "US") > indexOf(lines, "Brasil") {
			c.failf("want US above Brasil, got %q", lines)
		}
	})
}

// ---------------------------------------------------------------------------
// S2.4, S9.6, S9.7 Scroll (no groups: Q85 is open)
// ---------------------------------------------------------------------------

// S9.6 [view] Given more rows than fit the screen, when the down arrow or `j`
// is pressed, then the list scrolls down by one row. S9.7 ... a scrolled list,
// when the up arrow or `k` is pressed, then the list scrolls up by one row.
// S2.4 Given more rows than fit the screen, when the user scrolls, then the
// summary line stays in place. 25 tickers without groups on a 120 x 15
// terminal. Observations only: left and right arrows (Q84).
func TestVerifyView_S2_4_S9_6_7_Scroll(t *testing.T) {
	t.Parallel()
	cfg := twentyFive()
	all := cfg.all()
	e := newEnv(t, "S2.4, S9.6, S9.7 (view; 25 rows, 15 lines, no groups)", cfg.String())
	v := e.openViewSized(viewCols, 15, nil)
	s0, _ := v.waitStable("the first rows with a price and the status line", 10*time.Second, 300*time.Millisecond, func(s screen) bool {
		vis := visibleRows(s, all)
		return len(vis) > 3 && vis[0] == all[0] && rowsShown(vis)(s) && strings.Contains(statusLine(s), "sort:")
	})
	v.logScreen("SCREEN BEFORE SCROLLING", s0, nil)
	type step struct {
		key   string
		s     screen
		moved bool
	}
	var steps []step
	press := func(b []byte, name string) {
		s, ch := v.pressAndSettle(b, name)
		v.logScreen("SCREEN AFTER "+name, s, nil)
		steps = append(steps, step{name, s, ch})
	}
	press(v.arrow('B'), "down arrow")
	press([]byte("j"), "j")
	press([]byte("j"), "j")
	press(v.arrow('A'), "up arrow")
	press([]byte("k"), "k")
	// Q84, observation only: left and right arrows on a scrolled list.
	press([]byte("j"), "j")
	sRight, chRight := v.pressAndSettle(v.arrow('C'), "right arrow")
	sLeft, chLeft := v.pressAndSettle(v.arrow('D'), "left arrow")
	v.logScreen("SCREEN AFTER right and left arrows", sLeft, nil)
	v.quit([]byte("q"), "q")

	first := func(s screen) int {
		vis := visibleRows(s, all)
		if len(vis) == 0 {
			return -1
		}
		return indexOf(all, vis[0])
	}
	e.logf("OBSERVED   (Q84) right arrow: screen changed %v, first row %d -> %d; left arrow: changed %v, first row -> %d\n",
		chRight, first(steps[len(steps)-1].s), first(sRight), chLeft, first(sLeft))
	e.logf("OBSERVED   application cursor keys (DECCKM) switched on by the tool: %v\n", v.modes["1"])
	wantFirst := []int{1, 2, 3, 2, 1, 2}
	prev := s0
	moves := func(id, text string, idx []int) {
		crit(e, id, text, func(c *ck) {
			for _, i := range idx {
				st := steps[i]
				p := prev
				if i > 0 {
					p = steps[i-1].s
				}
				if got := first(st.s); got != wantFirst[i] {
					c.failf("after %s: want the first row to be %s (row %d), got row %d; before it was row %d",
						st.key, all[wantFirst[i]], wantFirst[i]+1, got+1, first(p)+1)
				}
				if a, b := len(visibleRows(p, all)), len(visibleRows(st.s, all)); a != b {
					c.notef("after %s: %d rows visible, before %d", st.key, b, a)
				}
			}
		})
	}
	moves("S9.6", "down arrow and `j` scroll down by one row", []int{0, 1, 2})
	moves("S9.7", "up arrow and `k` scroll up by one row", []int{3, 4})
	crit(e, "S2.4", "the summary line stays in place while scrolling", func(c *ck) {
		want := s0.lines[0]
		if !ibovRE.MatchString(want) {
			c.failf("set-up: the first line is not the summary line: %q", want)
		}
		for _, st := range steps {
			if st.s.lines[0] != want {
				c.failf("after %s the first line is %q, want the summary line %q", st.key, st.s.lines[0], want)
			}
		}
	})
}

// Q85, observation only: scrolling with group headers. 25 tickers in two
// groups on a 120 x 15 terminal; `j` three times; what is at the top is
// recorded, nothing is asserted.
func TestVerifyView_Q85_ScrollWithGroups_Observation(t *testing.T) {
	t.Parallel()
	cfg := twentyFive()
	cfg = cfgSpec{groups: []groupSpec{{"Brasil", cfg.top[:2]}, {"Outros", cfg.top[2:]}}}
	e := newEnv(t, "Q85 observation: scrolling with group headers (nothing asserted)", cfg.String())
	v := e.openViewSized(viewCols, 15, nil)
	s, _ := v.waitStable("the first rows", 10*time.Second, 300*time.Millisecond, func(s screen) bool {
		return len(s.rowLines("PETR4")) == 1 && hasQuote(s.rowLines("PETR4")[0])
	})
	top := func(s screen) string {
		b := viewBody(s)
		if len(b) == 0 {
			return ""
		}
		return strings.Fields(b[0])[0]
	}
	e.logf("OBSERVED   (Q85) before scrolling, the first line under the column header: %q\n", top(s))
	for i := 1; i <= 3; i++ {
		s, _ = v.pressAndSettle([]byte("j"), "j")
		e.logf("OBSERVED   (Q85) after j #%d, the first line under the column header: %q\n", i, top(s))
	}
	v.logScreen("SCREEN AFTER THREE j", s, nil)
	v.quit([]byte("q"), "q")
}

// ---------------------------------------------------------------------------
// S9.8, S9.9 Help and other keys
// ---------------------------------------------------------------------------

// S9.8 [view] Given the view is open, when `?` is pressed, then the keys `q`,
// `r`, `s`, the scroll keys and `?` are listed with their actions. 7.3: any
// key closes the help (recorded).
func TestVerifyView_S9_8_Help(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S9.8 (view)", baseConfig)
	v := e.openView(nil)
	s0, _ := v.waitStable("every row with a price", 10*time.Second, 300*time.Millisecond, rowsShown(tickers))
	s, changed := v.pressAndSettle([]byte("?"), "?")
	v.logScreen("SCREEN AFTER ?", s, nil)
	closed, _ := v.pressAndSettle([]byte("x"), "x (any key)")
	v.logScreen("SCREEN AFTER x", closed, nil)
	v.quit([]byte("q"), "q")
	e.logf("OBSERVED   (7.3) after x the list is back: %v\n", closed.trimmed() == s0.trimmed())
	crit(e, "S9.8", "`?` lists q, r, s, the scroll keys and ? with their actions", func(c *ck) {
		if !changed {
			c.failf("the screen did not change after `?`")
			return
		}
		type entry struct {
			keys   []string
			action string
		}
		for _, en := range []entry{
			{[]string{"q"}, "quit"}, {[]string{"r"}, "refresh"}, {[]string{"s"}, "sort"},
			{[]string{"j", "↓", "down"}, "scroll"}, {[]string{"k", "↑", "up"}, "scroll"}, {[]string{"?"}, "help"},
		} {
			found := false
			for _, l := range s.lines {
				ll := strings.ToLower(l)
				if !strings.Contains(ll, en.action) {
					continue
				}
				for _, k := range en.keys {
					if regexp.MustCompile(`(^|[^a-z])` + regexp.QuoteMeta(k) + `($|[^a-z])`).MatchString(ll) {
						found = true
					}
				}
			}
			if !found {
				c.failf("no line lists %v with the action %q", en.keys, en.action)
			}
		}
	})
}

// S9.9 [view] Given the view is open, when a key outside that list is
// pressed, then nothing changes. Keys x, z, 5, space, Tab and Enter, each
// followed by a wait of up to 2 s for a change (none is expected, so the
// absence of a change can only be observed over a time).
func TestVerifyView_S9_9_OtherKeysDoNothing(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S9.9 (view)", baseConfig)
	v := e.openView(nil)
	s0, _ := v.waitStable("every row with a price", 10*time.Second, 500*time.Millisecond, rowsShown(tickers))
	type res struct {
		key     string
		changed bool
		s       screen
	}
	var rs []res
	for _, k := range []struct {
		name string
		b    []byte
	}{{"x", []byte("x")}, {"z", []byte("z")}, {"5", []byte("5")}, {"space", []byte(" ")}, {"Tab", []byte("\t")}, {"Enter", []byte("\r")}} {
		s, ch := v.pressAndSettle(k.b, k.name)
		if ch {
			v.logScreen("SCREEN AFTER "+k.name, s, nil)
		}
		rs = append(rs, res{k.name, ch, s})
	}
	r := v.quit([]byte("q"), "q")
	crit(e, "S9.9", "a key outside the list changes nothing", func(c *ck) {
		for _, x := range rs {
			if x.changed || x.s.trimmed() != s0.trimmed() {
				c.failf("after %s the screen changed:\n%s", x.key, framed(x.s))
			}
		}
		if r.exit != 0 {
			c.failf("exit %d", r.exit)
		}
	})
}

// ---------------------------------------------------------------------------
// QR11 Keys answer during a fetch
// ---------------------------------------------------------------------------

// QR11 A key press changes the screen within 100 ms, also while a refresh is
// running. Measured with a stand-in source that answers after 5 seconds: the
// fetch at start is running; `?`, then any key, then `s` (the status line's
// sort changes), each timed from the write of the key to the first change of
// the emulated screen. Not parallel.
func TestVerifyView_QR11_KeysDuringFetch(t *testing.T) {
	e := newEnv(t, "QR11 (view)", baseConfig)
	e.setSource(state{DelayMS: 5000}, "answers after 5 s")
	v := e.openView(nil)
	v.waitStable("every row", 5*time.Second, 300*time.Millisecond, func(s screen) bool {
		for _, tk := range tickers {
			if len(s.rowLines(tk)) != 1 {
				return false
			}
		}
		return true
	})
	type lat struct {
		key   string
		d     time.Duration
		ok    bool
		atRun time.Duration
	}
	var ls []lat
	for _, k := range []struct {
		name string
		b    []byte
	}{{"?", []byte("?")}, {"x (closes the help)", []byte("x")}, {"s", []byte("s")}, {"s", []byte("s")}} {
		before := v.snap().text()
		at := time.Now()
		v.key(k.b, k.name)
		s, ok := v.waitFor("a change after "+k.name, 2*time.Second, func(s screen) bool { return s.text() != before })
		ls = append(ls, lat{k.name, s.at.Sub(at), ok, at.Sub(v.start)})
		v.waitStable("settled", 2*time.Second, 150*time.Millisecond, func(screen) bool { return true })
	}
	r := v.quit([]byte("q"), "q")
	crit(e, "QR11", "each key changes the screen within 100 ms while the fetch at start runs", func(c *ck) {
		answered := 0
		for _, q := range r.reqs {
			if q.AnsweredMS != 0 && q.Status > 0 && q.AnsweredMS < v.start.Add(ls[len(ls)-1].atRun).UnixMilli() {
				answered++
			}
		}
		if answered != 0 {
			c.notef("%d answers had arrived before the last key", answered)
		}
		for _, l := range ls {
			c.notef("%s at +%s: screen changed after %s", l.key, l.atRun.Round(time.Millisecond), l.d.Round(100*time.Microsecond))
			if !l.ok || l.d > 100*time.Millisecond {
				c.failf("%s: the screen changed after %s (changed: %v), want within 100 ms", l.key, l.d.Round(time.Millisecond), l.ok)
			}
			if l.atRun > 5*time.Second {
				c.failf("set-up: %s was pressed at +%s, after the fetch at start could have ended", l.key, l.atRun.Round(time.Millisecond))
			}
		}
	})
}
