//go:build verification && linux

// The full-screen view, increment 6 of slice 2: the review's answers (D92 to
// D96). The summary line on a narrow terminal (S2.9, S2.10), scrolling by
// screen lines with group headers (S9.6, S9.7 reworded), left and right
// arrows (S9.11), warnings shortened in the status line (S12.6 to S12.8,
// S15.10) and no terminal (S18.5, S18.6). Through the real view on a
// pseudo-terminal with the helpers of view_verify_test.go and
// view_increment5_verify_test.go; S18.5 and S18.6 as plain commands with the
// output redirected. See README.md.
package verification

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func runeLen(s string) int { return len([]rune(s)) }

// summaryText is the summary line: the first line with text, unless that is
// the column header (then there is no summary line on screen).
func summaryText(s screen) string {
	f := firstText(s)
	if strings.HasPrefix(strings.TrimSpace(f), "TICKER") {
		return ""
	}
	return f
}

// resizeAndSettle resizes the terminal and waits until the screen changed and
// settled (or 2 s without a change).
func (v *view) resizeAndSettle(cols, rows int) screen {
	v.e.t.Helper()
	before := v.snap().text()
	v.resize(cols, rows)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v.snap().text() != before {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s, _ := v.waitStable(fmt.Sprintf("the view redrawn at %d columns", cols), 3*time.Second, 300*time.Millisecond, func(screen) bool { return true })
	return s
}

// ---------------------------------------------------------------------------
// S2.9, S2.10 The summary line on a narrow terminal
// ---------------------------------------------------------------------------

var sumEntryRE = regexp.MustCompile(`(IBOV|S&P 500|USD/BRL) (\S+)(?: ([+-]\S*%))?`)

type sumEntry struct{ label, value, pct string }

func (x sumEntry) base() string { return x.label + " " + x.value }

// parseSummary returns the entries of a summary line and whatever text is
// left between and around them (a cut entry leaves text behind).
func parseSummary(l string) ([]sumEntry, string) {
	var out []sumEntry
	rest := l
	for _, m := range sumEntryRE.FindAllStringSubmatch(l, -1) {
		out = append(out, sumEntry{m[1], m[2], m[3]})
		rest = strings.Replace(rest, m[0], " ", 1)
	}
	return out, strings.TrimSpace(rest)
}

// S2.9 [view] Given a terminal too narrow for the summary line with even one
// percent shown, but wide enough for all its entries without their percents,
// when the view is drawn, then each entry shows its label and value and no
// percent. S2.10 [view] Given a terminal too narrow for all summary entries
// even without their percents, when the view is drawn, then entries are left
// out from the right and every entry shown is whole.
//
// The widths come from the full line at 120 columns: its entries and the
// spaces between them. W3 is the width of the three entries without percents
// with that separator; W3 + the shortest percent (with its space) is the
// narrowest line with one percent. Widths checked, by resizing one view and by
// starting fresh views at the same widths.
func TestVerifyView_S2_9_10_SummaryNarrow(t *testing.T) {
	t.Parallel()
	cfg := "tickers = [\"PETR4\", \"AAPL\"]\n"
	e := newEnv(t, "S2.9, S2.10 (view)", cfg)
	v := e.openView(nil)
	s0, _ := v.waitStable("the full summary line and the rows", 10*time.Second, 300*time.Millisecond, func(s screen) bool {
		es, _ := parseSummary(firstText(s))
		return len(es) == 3 && es[2].pct != "" && rowsShown([]string{"PETR4", "AAPL"})(s)
	})
	v.logScreen("SCREEN AT 120 COLUMNS", s0, nil)
	full := firstText(s0)
	fes, _ := parseSummary(full)
	if len(fes) != 3 {
		e.failf("set-up: the summary line at 120 columns has %d entries: %q", len(fes), full)
		v.quit([]byte("q"), "q")
		return
	}
	i1 := strings.Index(full, fes[0].base()+" "+fes[0].pct) + len(fes[0].base()+" "+fes[0].pct)
	i2 := strings.Index(full, fes[1].label)
	sep := i2 - i1
	w := func(k int) int { // k entries without percents
		n := (k - 1) * sep
		for _, x := range fes[:k] {
			n += runeLen(x.base())
		}
		return n
	}
	minPct := 1 << 30
	for _, x := range fes {
		if n := 1 + runeLen(x.pct); n < minPct {
			minPct = n
		}
	}
	fullW, w3, w2, w1 := runeLen(full), w(3), w(2), w(1)
	e.logf("OBSERVED   full summary line %q (%d wide), separator %d spaces; without percents: 1 entry %d, 2 entries %d, 3 entries %d; with one percent at least %d\n",
		full, fullW, sep, w1, w2, w3, w3+minPct)
	widths := []int{fullW, fullW - 1, (fullW + w3 + minPct) / 2, w3 + minPct, w3 + minPct - 1, (w3 + w3 + minPct - 1) / 2, w3, w3 - 1, w2, w2 - 1, w1, w1 - 1}

	type obs struct {
		how  string
		cols int
		line string
		s    screen
	}
	var all []obs
	for _, c := range widths {
		s := v.resizeAndSettle(c, viewRows)
		v.logScreen(fmt.Sprintf("SCREEN RESIZED TO %d COLUMNS", c), s, nil)
		all = append(all, obs{"resized", c, summaryText(s), s})
	}
	v.quit([]byte("q"), "q")
	for _, c := range []int{w3 + minPct - 1, w3, w3 - 1, w2 - 1, w1 - 1} {
		v := e.openViewSized(c, viewRows, nil)
		s, _ := v.waitStable(fmt.Sprintf("a summary line and the rows at %d columns", c), 10*time.Second, 400*time.Millisecond, func(s screen) bool {
			return len(s.rowLines("PETR4")) == 1
		})
		v.logScreen(fmt.Sprintf("SCREEN STARTED AT %d COLUMNS", c), s, nil)
		v.quit([]byte("q"), "q")
		all = append(all, obs{"started", c, summaryText(s), s})
	}
	for _, o := range all {
		es, rest := parseSummary(o.line)
		e.logf("OBSERVED   %s at %3d columns: summary %q (%d wide; %d entries; left over %q); widest line %d\n",
			o.how, o.cols, o.line, runeLen(o.line), len(es), rest, widest(o.s))
		if ls := o.s.rowLines("PETR4"); len(ls) == 1 && !hasQuote(ls[0]) {
			e.logf("OBSERVED   %s at %d columns: the PETR4 row has no whole price: %q\n", o.how, o.cols, ls[0])
		}
	}
	// whole: every entry shown is one of the full line's, label and value
	// whole, percent whole or absent, nothing else on the line, in order from
	// the left.
	whole := func(c *ck, o obs) []sumEntry {
		es, rest := parseSummary(o.line)
		if rest != "" {
			c.failf("%s at %d columns: text that is not a whole entry: %q in %q", o.how, o.cols, rest, o.line)
		}
		for i, x := range es {
			if i >= 3 || x.label != fes[i].label {
				c.failf("%s at %d columns: entries are not the full line's from the left: %q", o.how, o.cols, o.line)
				break
			}
			if x.value != fes[i].value || (x.pct != "" && x.pct != fes[i].pct) {
				c.failf("%s at %d columns: entry %q cut (full: %q)", o.how, o.cols, x.base()+" "+x.pct, fes[i].base()+" "+fes[i].pct)
			}
		}
		if runeLen(o.line) > o.cols {
			c.failf("%s at %d columns: the summary line is %d wide", o.how, o.cols, runeLen(o.line))
		}
		return es
	}
	crit(e, "S2.9", "too narrow for one percent, wide enough for all entries: label and value, no percent", func(c *ck) {
		n := 0
		for _, o := range all {
			if o.cols < w3 || o.cols >= w3+minPct {
				continue
			}
			n++
			es := whole(c, o)
			if len(es) != 3 {
				c.failf("%s at %d columns: want the three entries, got %q", o.how, o.cols, o.line)
			}
			for _, x := range es {
				if x.pct != "" {
					c.failf("%s at %d columns: %s shows its percent: %q", o.how, o.cols, x.label, o.line)
				}
			}
		}
		if n == 0 {
			c.failf("set-up: no width between %d and %d", w3, w3+minPct-1)
		}
	})
	crit(e, "S2.10", "too narrow for all entries without percents: left out from the right, every entry whole", func(c *ck) {
		for _, o := range all {
			if o.cols >= w3 {
				continue
			}
			es := whole(c, o)
			want := 0
			for k := 1; k <= 3 && w(k) <= o.cols; k++ {
				want = k
			}
			for _, x := range es {
				if x.pct != "" {
					c.failf("%s at %d columns: an entry is left out but %s shows its percent: %q", o.how, o.cols, x.label, o.line)
				}
			}
			if len(es) != want {
				c.failf("%s at %d columns: want the first %d entries (as many as fit, with the full line's separator), got %d: %q", o.how, o.cols, want, len(es), o.line)
			}
		}
	})
	// Outside the criteria: every width, wholeness only.
	for _, o := range all {
		if o.cols >= w3+minPct {
			es, rest := parseSummary(o.line)
			np := 0
			for _, x := range es {
				if x.pct != "" {
					np++
				}
			}
			e.logf("OBSERVED   (outside S2.9/S2.10) %s at %d columns: %d entries, %d with a percent, left over %q\n", o.how, o.cols, len(es), np, rest)
		}
	}
}

// ---------------------------------------------------------------------------
// S9.6, S9.7 Scroll by one screen line, group headers included (D93)
// ---------------------------------------------------------------------------

// scrollConfig is 25 tickers: two outside groups, then two groups, so that
// the lines under the column header are rows and group headers mixed.
func scrollConfig() (cfgSpec, []string) {
	cfg := cfgSpec{top: []string{"PETR4", "AAPL"}, groups: []groupSpec{
		{"Brasil", fillers[:1]}, {"Outros", fillers[1:]}}}
	lines := []string{"PETR4", "AAPL", "Brasil", fillers[0], "Outros"}
	lines = append(lines, fillers[1:]...)
	return cfg, lines
}

// topLine is the first word of the first line under the column header.
func topLine(s screen) string {
	b := viewBody(s)
	if len(b) == 0 {
		return ""
	}
	return strings.Fields(b[0])[0]
}

// S9.6 [view] Given more rows than fit the screen, when the down arrow or `j`
// is pressed, then the list scrolls down by one screen line, whether that line
// holds a row or a group header. S9.7 ... a scrolled list, when the up arrow
// or `k` is pressed, then the list scrolls up by one screen line, whether that
// line holds a row or a group header. 25 tickers in two groups (and two
// outside groups) on 120 x 15. Lines under the column header: PETR4, AAPL,
// Brasil, BBDC4, Outros, ABEV3, ...
func TestVerifyView_S9_6_7_ScrollByScreenLine(t *testing.T) {
	t.Parallel()
	cfg, lines := scrollConfig()
	e := newEnv(t, "S9.6, S9.7 (view; 25 rows, two group headers, 15 lines)", cfg.String())
	v := e.openViewSized(viewCols, 15, nil)
	s0, _ := v.waitStable("the first rows with a price and the status line", 10*time.Second, 300*time.Millisecond, func(s screen) bool {
		return topLine(s) == "PETR4" && rowsShown([]string{"PETR4", "AAPL"})(s) && strings.Contains(statusLine(s), "sort:")
	})
	v.logScreen("SCREEN BEFORE SCROLLING", s0, nil)
	type step struct {
		key       string
		want      int // index in lines of the expected top line
		s         screen
		changed   bool
		secondWas string // the second line under the header before the key
	}
	var steps []step
	keys := []struct {
		name string
		b    func() []byte
		want int
	}{
		{"j", func() []byte { return []byte("j") }, 1},
		{"down arrow", func() []byte { return v.arrow('B') }, 2}, // the header Brasil to the top
		{"j", func() []byte { return []byte("j") }, 3},           // past it: a row
		{"down arrow", func() []byte { return v.arrow('B') }, 4}, // the header Outros
		{"j", func() []byte { return []byte("j") }, 5},
		{"k", func() []byte { return []byte("k") }, 4},           // back onto the header Outros
		{"up arrow", func() []byte { return v.arrow('A') }, 3},   // a row
		{"k", func() []byte { return []byte("k") }, 2},           // the header Brasil
		{"up arrow", func() []byte { return v.arrow('A') }, 1},
		{"k", func() []byte { return []byte("k") }, 0},
	}
	prev := s0
	for _, k := range keys {
		b := viewBody(prev)
		second := ""
		if len(b) > 1 {
			second = strings.Fields(b[1])[0]
		}
		s, ch := v.pressAndSettle(k.b(), k.name)
		v.logScreen("SCREEN AFTER "+k.name, s, nil)
		steps = append(steps, step{k.name, k.want, s, ch, second})
		prev = s
	}
	r := v.quit([]byte("q"), "q")
	check := func(id, text string, from, to int) {
		crit(e, id, text, func(c *ck) {
			for i := from; i < to; i++ {
				st := steps[i]
				if got := topLine(st.s); got != lines[st.want] {
					c.failf("after %s (#%d): want %q at the top under the column header, got %q", st.key, i+1, lines[st.want], got)
				}
				if id == "S9.6" && topLine(st.s) != st.secondWas {
					c.failf("after %s (#%d): the line that was second (%q) is not at the top (%q)", st.key, i+1, st.secondWas, topLine(st.s))
				}
				c.notef("after %s: top %q", st.key, topLine(st.s))
			}
			if r.exit != 0 {
				c.failf("exit %d", r.exit)
			}
		})
	}
	check("S9.6", "down arrow and `j` scroll down by one screen line, row or group header", 0, 5)
	check("S9.7", "up arrow and `k` scroll up by one screen line, row or group header", 5, 10)
}

// ---------------------------------------------------------------------------
// S9.11 Left and right arrows do nothing (D92)
// ---------------------------------------------------------------------------

// S9.11 [view] Given the view is open and the list scrolled, when the left or
// right arrow is pressed, then nothing changes. The same 25-line list on
// 120 x 15, scrolled by three `j`; right, left, right, left, each watched for
// 2 s (an absence of change can only be watched over a time); both encodings
// of the arrows (CSI and SS3). Then `j` still scrolls (recorded).
func TestVerifyView_S9_11_LeftRightDoNothing(t *testing.T) {
	t.Parallel()
	cfg, lines := scrollConfig()
	e := newEnv(t, "S9.11 (view)", cfg.String())
	v := e.openViewSized(viewCols, 15, nil)
	v.waitStable("the first rows with a price", 10*time.Second, 300*time.Millisecond, func(s screen) bool {
		return topLine(s) == "PETR4" && rowsShown([]string{"PETR4", "AAPL"})(s) && strings.Contains(statusLine(s), "sort:")
	})
	var s0 screen
	for i := 0; i < 3; i++ {
		s0, _ = v.pressAndSettle([]byte("j"), "j")
	}
	s0, _ = v.waitStable("settled after scrolling", 3*time.Second, 500*time.Millisecond, func(screen) bool { return true })
	v.logScreen("SCREEN SCROLLED", s0, nil)
	type res struct {
		key     string
		changed bool
		s       screen
	}
	var rs []res
	for _, k := range []struct {
		name string
		b    []byte
	}{
		{"right arrow", v.arrow('C')}, {"left arrow", v.arrow('D')},
		{"right arrow (CSI)", []byte("\x1b[C")}, {"left arrow (CSI)", []byte("\x1b[D")},
		{"right arrow (SS3)", []byte("\x1bOC")}, {"left arrow (SS3)", []byte("\x1bOD")},
	} {
		s, ch := v.pressAndSettle(k.b, k.name)
		if ch {
			v.logScreen("SCREEN AFTER "+k.name, s, nil)
		}
		rs = append(rs, res{k.name, ch, s})
	}
	after, _ := v.pressAndSettle([]byte("j"), "j (after the arrows)")
	v.logScreen("SCREEN AFTER j", after, nil)
	r := v.quit([]byte("q"), "q")
	e.logf("OBSERVED   after the arrows, j moved the top line to %q (want %q)\n", topLine(after), lines[4])
	crit(e, "S9.11", "on a scrolled list, left and right arrows change nothing", func(c *ck) {
		if topLine(s0) != lines[3] {
			c.failf("set-up: the list is not scrolled by three lines: top %q", topLine(s0))
		}
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
// S12.6 to S12.8, S15.10 Warnings shortened in the status line (D95)
// ---------------------------------------------------------------------------

var exampleTickers = []string{"PETR4", "VALE3", "ITUB4", "AAPL", "MSFT"}

// noConfigEnv is an environment without a config file, whose default config
// path is under the home folder: XDG_CONFIG_HOME unset (~/.config), or set to
// a folder under the home folder (~/xdg). It returns the path as the status
// line should write it.
func noConfigEnv(t *testing.T, what, variant string) (*env, string) {
	e := newEnv(t, what+" ["+variant+"]", "")
	os.RemoveAll(filepath.Join(e.cfgHome, "brl-stocks"))
	switch variant {
	case "xdg_unset":
		e.noCfgHome = true
		os.MkdirAll(filepath.Join(e.home, ".config"), 0o755)
		e.cfgPath = filepath.Join(e.home, ".config", "brl-stocks", "config.toml")
		return e, "~/.config/brl-stocks/config.toml"
	default: // xdg_under_home
		e.cfgHome = filepath.Join(e.home, "xdg")
		os.MkdirAll(e.cfgHome, 0o755)
		e.cfgPath = filepath.Join(e.cfgHome, "brl-stocks", "config.toml")
		return e, "~/xdg/brl-stocks/config.toml"
	}
}

// S12.6 [view] Given no file at the default config path, which is under the
// home folder, when the view is drawn, then the status line names the path
// with the home folder written `~`. XDG_CONFIG_HOME unset, and set under the
// home folder; 120 and 200 columns.
func TestVerifyView_S12_6_ConfigPathWithTilde(t *testing.T) {
	t.Parallel()
	for _, variant := range []string{"xdg_unset", "xdg_under_home"} {
		for _, cols := range []int{viewCols, 200} {
			variant, cols := variant, cols
			t.Run(fmt.Sprintf("%s_%d", variant, cols), func(t *testing.T) {
				t.Parallel()
				e, want := noConfigEnv(t, "S12.6 (view)", variant)
				v := e.openViewSized(cols, viewRows, nil)
				s, _ := v.waitStable("the example rows and a status line with the path", 10*time.Second, 300*time.Millisecond, func(s screen) bool {
					return rowsShown(exampleTickers)(s) && strings.Contains(statusLine(s), "config")
				})
				v.logScreen("SCREEN BEFORE QUITTING", s, nil)
				r := v.quit([]byte("q"), "q")
				crit(e, "S12.6", "the status line names the config path with the home folder written ~", func(c *ck) {
					st := statusLine(s)
					if !strings.Contains(st, want) {
						c.failf("want the status line to name %s, got %q", want, st)
					}
					if s.contains(e.home) {
						c.failf("the screen shows the home folder %s", e.home)
					}
					if r.exit != 0 {
						c.failf("exit %d", r.exit)
					}
					c.notef("status line %q (%d wide at %d columns)", st, runeLen(st), cols)
				})
			})
		}
	}
}

// S12.7 [view] Given no file at the default config path and a terminal too
// narrow for the whole status line but wide enough for it without the current
// sort, when the view is drawn, then the status line does not show the sort.
// S12.8 ... then the status line shows the whole warning. The whole line is
// read at 200 columns; "without the current sort" is that line with its
// `sort: ...` part and one ` · ` separator removed. Widths: that width, one
// more, the middle, and one less than the whole line; fresh views and one
// view resized. Outside the criteria (recorded): the whole width, and one
// less than the line without the sort.
func TestVerifyView_S12_7_8_SortLeftOutBeforeWarning(t *testing.T) {
	t.Parallel()
	e, path := noConfigEnv(t, "S12.7, S12.8 (view)", "xdg_unset")
	v := e.openViewSized(200, viewRows, nil)
	s0, _ := v.waitStable("the example rows and the whole status line", 10*time.Second, 300*time.Millisecond, func(s screen) bool {
		return rowsShown(exampleTickers)(s) && strings.Contains(statusLine(s), path) && strings.Contains(statusLine(s), "sort:")
	})
	v.logScreen("SCREEN AT 200 COLUMNS", s0, nil)
	whole := statusLine(s0)
	sortRE := regexp.MustCompile(`( · )?sort: [^·]*[^ ·]( · )?`)
	m := sortRE.FindStringSubmatchIndex(whole)
	if m == nil {
		e.failf("set-up: no sort in the whole status line %q", whole)
		v.quit([]byte("q"), "q")
		return
	}
	noSort := whole[:m[0]] + " · " + whole[m[1]:]
	if m[2] < 0 || m[4] < 0 { // the sort was first or last: no separator to keep
		noSort = whole[:m[0]] + whole[m[1]:]
	}
	warning := strings.TrimSpace(whole[m[1]:])
	lw, lf := runeLen(noSort), runeLen(whole)
	e.logf("OBSERVED   whole status line %q (%d wide); without the sort %q (%d wide); the warning %q\n", whole, lf, noSort, lw, warning)
	type obs struct {
		how  string
		cols int
		s    screen
	}
	var all []obs
	in := []int{lw, lw + 1, (lw + lf) / 2, lf - 1}
	for _, c := range append(append([]int{lf}, in...), lw-1) {
		s := v.resizeAndSettle(c, viewRows)
		v.logScreen(fmt.Sprintf("SCREEN RESIZED TO %d COLUMNS", c), s, nil)
		all = append(all, obs{"resized", c, s})
	}
	v.quit([]byte("q"), "q")
	for _, c := range append([]int{lf}, append(in, lw-1)...) {
		v := e.openViewSized(c, viewRows, nil)
		s, _ := v.waitStable(fmt.Sprintf("the example rows and a status line at %d columns", c), 10*time.Second, 300*time.Millisecond, func(s screen) bool {
			return rowsShown(exampleTickers)(s) && strings.Contains(statusLine(s), "config")
		})
		v.logScreen(fmt.Sprintf("SCREEN STARTED AT %d COLUMNS", c), s, nil)
		v.quit([]byte("q"), "q")
		all = append(all, obs{"started", c, s})
	}
	for _, o := range all {
		st := statusLine(o.s)
		e.logf("OBSERVED   %s at %3d columns: status line %q (%d wide)\n", o.how, o.cols, st, runeLen(st))
	}
	inRange := func(c int) bool { return c >= lw && c < lf }
	crit(e, "S12.7", "too narrow for the whole status line, wide enough without the sort: no sort", func(c *ck) {
		for _, o := range all {
			if !inRange(o.cols) {
				continue
			}
			if st := statusLine(o.s); strings.Contains(st, "sort:") {
				c.failf("%s at %d columns: the status line shows the sort: %q", o.how, o.cols, st)
			}
		}
	})
	crit(e, "S12.8", "too narrow for the whole status line, wide enough without the sort: the whole warning", func(c *ck) {
		for _, o := range all {
			if !inRange(o.cols) {
				continue
			}
			st := statusLine(o.s)
			if !strings.Contains(st, warning) {
				c.failf("%s at %d columns: want the whole warning %q, got %q", o.how, o.cols, warning, st)
			}
			if runeLen(st) > o.cols {
				c.failf("%s at %d columns: the status line is %d wide", o.how, o.cols, runeLen(st))
			}
		}
	})
}

// S15.10 [view] Given a cache folder under the home folder that cannot be
// written, when the view is drawn, then the warning in the status line names
// the folder with the home folder written `~`. XDG_CACHE_HOME unset
// (~/.cache/brl-stocks, mode 0555), and set to ~/xc (~/xc/brl-stocks, 0555).
func TestVerifyView_S15_10_CacheFolderWithTilde(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("running as root: a folder cannot be made unwritable")
	}
	for _, variant := range []string{"xdg_cache_unset", "xdg_cache_under_home"} {
		variant := variant
		t.Run(variant, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, "S15.10 (view) ["+variant+"]", baseConfig)
			want := "~/.cache/brl-stocks"
			dir := filepath.Join(e.home, ".cache", "brl-stocks")
			if variant == "xdg_cache_unset" {
				e.noCacheHome = true
			} else {
				e.cacheHome = filepath.Join(e.home, "xc")
				dir = filepath.Join(e.cacheHome, "brl-stocks")
				want = "~/xc/brl-stocks"
			}
			os.MkdirAll(dir, 0o755)
			os.Chmod(dir, 0o555)
			t.Cleanup(func() { os.Chmod(dir, 0o755) })
			s, _, _ := e.viewAndQuit("every row with a price and a warning about the cache", func(s screen) bool {
				return rowsShown(tickers)(s) && strings.Contains(strings.ToLower(statusLine(s)), "kept")
			})
			crit(e, "S15.10", "the warning names the cache folder with the home folder written ~", func(c *ck) {
				st := statusLine(s)
				if !regexp.MustCompile(regexp.QuoteMeta(want) + `/?($|[\s·])`).MatchString(st) {
					c.failf("want the status line to name %s, got %q", want, st)
				}
				if s.contains(e.home) {
					c.failf("the screen shows the home folder %s", e.home)
				}
				c.notef("status line %q", st)
			})
		})
	}
}

// ---------------------------------------------------------------------------
// S18.5, S18.6 No terminal (D94)
// ---------------------------------------------------------------------------

// ttyRun runs the binary with each of stdin, stdout and stderr on a
// pseudo-terminal or not (stdin /dev/null, stdout and stderr to files), waits
// at most 5 s (then presses q, then kills), and returns the result with
// everything the terminal received.
func (e *env) ttyRun(inTTY, outTTY, errTTY bool, args ...string) (result, string) {
	e.t.Helper()
	p, err := openPTY(viewCols, viewRows)
	if err != nil {
		e.t.Fatalf("pseudo-terminal: %v", err)
	}
	defer p.close()
	var mu sync.Mutex
	var term bytes.Buffer
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := p.master.Read(buf)
			mu.Lock()
			term.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	outF, _ := os.Create(filepath.Join(e.root, "stdout.txt"))
	errF, _ := os.Create(filepath.Join(e.root, "stderr.txt"))
	null, _ := os.Open(os.DevNull)
	defer outF.Close()
	defer errF.Close()
	defer null.Close()
	cmd := exec.Command(binPath, args...)
	cmd.Env = append(e.environ(), "TERM=xterm-256color")
	cmd.Dir = e.root
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, outF, errF
	ctty := -1
	if errTTY {
		cmd.Stderr, ctty = p.slave, 2
	}
	if outTTY {
		cmd.Stdout, ctty = p.slave, 1
	}
	if inTTY {
		cmd.Stdin, ctty = p.slave, 0
	}
	if ctty >= 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: ctty}
	}
	before := len(e.src.requests())
	sum := fileSum(e.cfgPath)
	start := time.Now()
	if err := cmd.Start(); err != nil {
		e.t.Fatalf("start: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var werr error
	note := ""
	select {
	case werr = <-done:
	case <-time.After(5 * time.Second):
		note = " (still running after 5 s: q pressed)"
		p.master.Write([]byte("q"))
		select {
		case werr = <-done:
		case <-time.After(3 * time.Second):
			note = " (still running after 5 s, q did not end it: killed)"
			cmd.Process.Kill()
			werr = <-done
		}
	}
	time.Sleep(100 * time.Millisecond)
	r := result{dur: time.Since(start)}
	var ee *exec.ExitError
	switch {
	case werr == nil:
	case errors.As(werr, &ee):
		r.exit = ee.ExitCode()
	default:
		r.exit = -999
	}
	so, _ := os.ReadFile(outF.Name())
	se, _ := os.ReadFile(errF.Name())
	r.stdout, r.stderr = string(so), string(se)
	if all := e.src.requests(); len(all) >= before {
		r.reqs = all[before:]
	}
	mu.Lock()
	tt := strings.ReplaceAll(escRE.ReplaceAllString(term.String(), ""), "\r", "")
	tl := term.Len()
	mu.Unlock()
	e.logf("\n$ brl-stocks %s   (stdin %s, stdout %s, stderr %s)\nEXIT       %d%s   (wall time %s)\nSTDOUT (file)\n%s\nSTDERR (file)\n%s\nTERMINAL (%d bytes, escape sequences removed)\n%s\nREQUESTS RECEIVED BY THE STAND-IN DURING THIS RUN: %d\n",
		strings.Join(args, " "), map[bool]string{true: "terminal", false: "/dev/null"}[inTTY],
		map[bool]string{true: "terminal", false: "file"}[outTTY], map[bool]string{true: "terminal", false: "file"}[errTTY],
		r.exit, note, r.dur.Round(time.Millisecond), indent(r.stdout), indent(r.stderr), tl, indent(tt), len(r.reqs))
	if fileSum(e.cfgPath) != sum {
		e.failf("QR7: the config file changed during the run")
	}
	return r, tt
}

// S18.5 [view] Given no terminal (the output is redirected), when
// `brl-stocks` is run with no command, then it exits with status 2. S18.6 ...
// then one line on the error stream names `brl-stocks print`. Four runs:
// no terminal at all (pipes, stdin /dev/null), the same with `--config`,
// standard output to a file with stdin and the error stream on a terminal
// (`brl-stocks > out.txt`), and everything to files with stdin on a terminal.
// Recorded only: stdin not a terminal while the output is one; an invalid
// config without a terminal.
func TestVerifyView_S18_5_6_NoTerminal(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S18.5, S18.6 (view: no terminal)", baseConfig)
	type run struct {
		name   string
		r      result
		errOut string // what reached the error stream
	}
	var rs []run
	r := e.run()
	rs = append(rs, run{"pipes", r, r.stderr})
	r = e.run("--config", e.cfgPath)
	rs = append(rs, run{"pipes, --config", r, r.stderr})
	r, term := e.ttyRun(true, false, true)
	rs = append(rs, run{"stdout to a file, stdin and stderr on a terminal", r, term})
	r, _ = e.ttyRun(true, false, false)
	rs = append(rs, run{"stdout and stderr to files, stdin on a terminal", r, r.stderr})

	// Outside the criteria.
	ro, termO := e.ttyRun(false, true, true)
	e.logf("OBSERVED   stdin /dev/null, stdout and stderr on a terminal: exit %d, %d requests, terminal %q\n", ro.exit, len(ro.reqs), strings.TrimSpace(termO))
	bad := filepath.Join(e.root, "bad.toml")
	writeFile(t, bad, "tickers = [\"PETR4\"\nnot toml ===\n")
	rb := e.run("--config", bad)
	e.logf("OBSERVED   invalid config, no terminal: exit %d, error stream %q\n", rb.exit, rb.stderr)
	rm := e.run("--config", filepath.Join(e.root, "missing.toml"))
	e.logf("OBSERVED   missing --config file, no terminal: exit %d, error stream %q\n", rm.exit, rm.stderr)

	crit(e, "S18.5", "no terminal: exit status 2", func(c *ck) {
		for _, x := range rs {
			if x.r.exit != 2 {
				c.failf("%s: exit status %d, want 2", x.name, x.r.exit)
			}
			if len(x.r.reqs) != 0 {
				c.notef("%s: %d requests sent", x.name, len(x.r.reqs))
			}
			if strings.TrimSpace(x.r.stdout) != "" {
				c.notef("%s: standard output %q", x.name, x.r.stdout)
			}
		}
	})
	crit(e, "S18.6", "no terminal: one line on the error stream names `brl-stocks print`", func(c *ck) {
		for _, x := range rs {
			ls := lines(strings.TrimSpace(x.errOut))
			if len(ls) != 1 {
				c.failf("%s: want one line on the error stream, got %d: %q", x.name, len(ls), x.errOut)
				continue
			}
			if !strings.Contains(ls[0], "brl-stocks print") {
				c.failf("%s: the line does not name `brl-stocks print`: %q", x.name, ls[0])
			}
			c.notef("%s: %q", x.name, ls[0])
		}
	})
}
