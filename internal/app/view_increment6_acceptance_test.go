package app_test

// Acceptance tests of the full-screen view, increment 6 of slice 2: the
// review's answers (D92-D96). The summary line on a narrow terminal, left and
// right do nothing, warnings shortened in the status line, and no terminal.
// The reworded S9.6 and S9.7 (scrolling by one screen line, D93) are in
// view_increment5_acceptance_test.go and use the grouped scroll helpers here.
// Written from docs/spec.md and the view's test boundary; they reuse the
// helpers of the earlier view tests and of the print acceptance tests.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lucattelli/brl-stocks/internal/deps"
)

// ---------------------------------------------------------------------------
// Helpers of this increment
// ---------------------------------------------------------------------------

// summaryEntries are the default summary entries as loadQuotes makes them,
// left to right (7.2), each as its label, its value and its percent (7.3).
var summaryEntries = []struct{ label, value, percent string }{
	{"IBOV", "158.432", "+0,32%"},
	{"S&P 500", "6,812.40", "-0.11%"},
	{"USD/BRL", "5,4321", "+0,08%"},
}

// cells counts the terminal cells of a line without SGR codes.
func cells(s string) int { return utf8.RuneCountInString(s) }

// fullSummary opens the view on config at full width, checks that its
// summary line is whole there (every label, value and percent), and returns
// the view, the line and the tickers of its rows.
func fullSummary(t *testing.T, config string, tickers []string) (*viewRun, string) {
	t.Helper()
	w := newWorld(t, bothOpen, config)
	loadTwenty(w.src, w.now)
	v := openView(t, w, viewOpts{})
	line := strings.TrimRight(summaryLine(t, v.settled().text(), "IBOV", tickers...), " ")
	for _, e := range summaryEntries {
		if !strings.Contains(line, e.label+" "+e.value+" "+e.percent) {
			t.Fatalf("given not established: at %d columns the summary line does not show %q whole: %q", viewWidth, e.label+" "+e.value+" "+e.percent, line)
		}
	}
	return v, line
}

// withoutPercents is the width of the full-width summary line with each
// entry's percent, and the space before it, left out: the narrowest terminal
// that holds every entry without its percent, at the separators the view
// draws at full width.
func withoutPercents(line string) int {
	n := cells(line)
	for _, e := range summaryEntries {
		n -= cells(" " + e.percent)
	}
	return n
}

// summaryCases are the watchlists the summary tests run with: the
// criterion's and 20 tickers (QR1).
var summaryCases = []struct {
	name    string
	config  string
	tickers []string
}{
	{"criterion", plainConfig, []string{"PETR4", "VALE3", "AAPL"}},
	{"20 tickers", twentyConfig(), twentyTickers()},
}

// statusSegments splits a status line at the separator 7.3 gives the market
// states (`B3 open · US closed`).
func statusSegments(st string) []string {
	var segs []string
	for _, s := range strings.Split(st, "·") {
		if s = strings.TrimSpace(s); s != "" {
			segs = append(segs, s)
		}
	}
	return segs
}

// groupedConfig has the 20 tickers in two groups, so that the list has two
// group header lines among its rows.
func groupedConfig() string {
	return "[[group]]\nname = \"Brasil\"\ntickers = [\"" + strings.Join(twentyB3, `", "`) + "\"]\n\n" +
		"[[group]]\nname = \"US\"\ntickers = [\"" + strings.Join(twentyUS, `", "`) + "\"]\n"
}

// groupedLines are the screen lines of groupedConfig's list in file order:
// each group header, then its rows (7.3).
func groupedLines() []string {
	return append(append(append([]string{"Brasil"}, twentyB3...), "US"), twentyUS...)
}

// groupedScrollView opens the view on groupedConfig on a terminal shorter
// than its list.
func groupedScrollView(t *testing.T) (*viewRun, string) {
	t.Helper()
	w := newWorld(t, bothOpen, groupedConfig())
	loadTwenty(w.src, w.now)
	v := openView(t, w, viewOpts{})
	v.settled()
	v.resize(viewWidth, scrollHeight)
	f := v.settled().text()
	if vis := listOnScreen(f); len(vis) == 0 || len(vis) >= len(groupedLines()) {
		t.Fatalf("given not established: %d of %d list lines on a screen of %d lines, want more than fit:\n%s", len(vis), len(groupedLines()), scrollHeight, f)
	}
	return v, f
}

// listOnScreen lists, top to bottom, the lines of the frame that are one of
// groupedLines: a group header (the name alone on its line) or a row (its
// ticker first).
func listOnScreen(f string) []string {
	is := map[string]bool{}
	for _, l := range groupedLines() {
		is[l] = true
	}
	var got []string
	for _, l := range lines(f) {
		if name := strings.TrimSpace(l); name == "Brasil" || name == "US" {
			got = append(got, name)
			continue
		}
		if fl := strings.Fields(l); len(fl) > 0 && is[fl[0]] && fl[0] != "Brasil" && fl[0] != "US" {
			got = append(got, fl[0])
		}
	}
	return got
}

// firstListLine returns the place in groupedLines of the first list line on
// screen, and checks that the list lines on screen follow it without a gap.
func firstListLine(t *testing.T, f string) int {
	t.Helper()
	vis, all := listOnScreen(f), groupedLines()
	if len(vis) == 0 {
		t.Fatalf("no line of the list on screen:\n%s", f)
	}
	start := -1
	for i, l := range all {
		if l == vis[0] {
			start = i
		}
	}
	for i, l := range vis {
		if start+i >= len(all) || all[start+i] != l {
			t.Fatalf("the list lines on screen %v do not follow the list %v:\n%s", vis, all, f)
		}
	}
	return start
}

// noConfigView opens the view with no config file under a home folder
// (no XDG_CONFIG_HOME: the default path is ~/.config/brl-stocks/config.toml)
// and returns the view and its world.
func noConfigView(t *testing.T) (*viewRun, *world) {
	t.Helper()
	w := newNoConfigWorld(t, bothOpen)
	delete(w.env, "XDG_CONFIG_HOME")
	w.configPath = filepath.Join(w.env["HOME"], ".config", "brl-stocks", "config.toml")
	if _, err := os.Stat(w.configPath); err == nil {
		t.Fatalf("given not established: a file at %s", w.configPath)
	}
	loadQuotes(w.src, w.now, true)
	return openView(t, w, viewOpts{}), w
}

// wideStatus is a width at which the whole status line fits.
const wideStatus = 400

// narrowStatusGiven establishes the given of S12.7 and S12.8: with no
// config, the whole status line (measured at wideStatus columns) and the
// width of it without the current sort. The current sort is the one segment
// that changes when `s` is pressed; `s` is pressed three times to come back
// to file order (7.3). It returns the view at file order, the whole line,
// the sort segment and the width of the line without it.
func narrowStatusGiven(t *testing.T) (v *viewRun, whole, sort string, without int) {
	t.Helper()
	v, _ = noConfigView(t)
	v.settled()
	v.resize(wideStatus, viewHeight)
	_, whole = statusLine(t, v.settled().text())
	whole = strings.TrimRight(whole, " ")
	if cells(whole) >= wideStatus {
		t.Fatalf("given not established: the status line fills all %d columns, so it may be cut: %q", wideStatus, whole)
	}
	v.send("s")
	_, sorted := statusLine(t, v.settled().text())
	a, b := statusSegments(whole), statusSegments(strings.TrimRight(sorted, " "))
	if len(a) != len(b) {
		t.Fatalf("given not established: the status line has %d segments in file order and %d sorted by day change:\n%q\n%q", len(a), len(b), whole, sorted)
	}
	at := -1
	for i := range a {
		if a[i] != b[i] {
			if at >= 0 {
				t.Fatalf("given not established: more than one segment of the status line changes with the sort:\n%q\n%q", whole, sorted)
			}
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("given not established: no segment of the status line changes with the sort:\n%q\n%q", whole, sorted)
	}
	sort = a[at]
	rest := append(append([]string{}, a[:at]...), a[at+1:]...)
	without = cells(strings.Join(rest, " · "))
	v.send("s")
	v.send("s")
	if _, back := statusLine(t, v.settled().text()); strings.TrimRight(back, " ") != whole {
		t.Fatalf("given not established: three presses of s did not bring back the status line of file order:\n%q\n%q", whole, back)
	}
	if without >= cells(whole) {
		t.Fatalf("given not established: the status line without the sort (%d cells) is not narrower than the whole line (%d)", without, cells(whole))
	}
	return v, whole, sort, without
}

// narrowStatusWidths are the widths of the given of S12.7 and S12.8: too
// narrow for the whole status line, wide enough for it without the sort.
func narrowStatusWidths(whole string, without int) []int {
	ws := []int{without}
	if w := cells(whole) - 1; w != without {
		ws = append(ws, w)
	}
	return ws
}

// ---------------------------------------------------------------------------
// S2 - Summary line
// ---------------------------------------------------------------------------

// S2.9 Given a terminal too narrow for the summary line with even one
// percent shown, but wide enough for all its entries without their percents,
// when the view is drawn, then each entry shows its label and value and no
// percent. (D96)
func TestS2_9_View_NarrowSummaryLeavesOutThePercents(t *testing.T) {
	for _, c := range summaryCases {
		t.Run(c.name, func(t *testing.T) {
			// Given: the width of every entry without its percent, at the
			// separators of the full-width line; one percent, and the space
			// before it, would not fit
			v, full := fullSummary(t, c.config, c.tickers)
			width := withoutPercents(full)

			// When
			v.resize(width, viewHeight)
			f := v.settled().text()

			// Then
			line := summaryLine(t, f, "IBOV", c.tickers...)
			for _, e := range summaryEntries {
				if !strings.Contains(line, e.label+" "+e.value) {
					t.Errorf("at %d columns the summary line does not show %q: %q", width, e.label+" "+e.value, line)
				}
			}
			if strings.Contains(line, "%") {
				t.Errorf("at %d columns the summary line shows a percent: %q", width, line)
			}
		})
	}
}

// S2.10 Given a terminal too narrow for all summary entries even without
// their percents, when the view is drawn, then entries are left out from the
// right and every entry shown is whole. (D96)
func TestS2_10_View_NarrowerSummaryLeavesOutWholeEntriesFromTheRight(t *testing.T) {
	// Every entry without its percent, one space between them: no width
	// below this holds all three, whatever the separator.
	least := -1
	for _, e := range summaryEntries {
		least += cells(e.label+" "+e.value) + 1
	}
	for _, c := range summaryCases {
		for _, width := range []int{least - 1, 25} {
			t.Run(c.name+"/"+strconv.Itoa(width)+" columns", func(t *testing.T) {
				// Given
				v, _ := fullSummary(t, c.config, c.tickers)

				// When
				v.resize(width, viewHeight)
				f := v.settled().text()

				// Then: the entries shown are the first ones, at least the
				// first (so that "every entry shown" says something), not all
				line := summaryLine(t, f, "IBOV", c.tickers...)
				shown := 0
				for i, e := range summaryEntries {
					if strings.Contains(line, e.label) {
						if i != shown {
							t.Errorf("at %d columns the summary line shows %s but not the entry before it: %q", width, e.label, line)
						}
						shown++
					}
				}
				if shown == len(summaryEntries) {
					t.Errorf("at %d columns the summary line shows all %d entries, want some left out: %q", width, shown, line)
				}
				// Then: each entry shown is whole (its label and value, and
				// its percent if shown at all), and nothing else is there
				rest := line
				for _, e := range summaryEntries {
					if strings.Contains(rest, e.label+" "+e.value+" "+e.percent) {
						rest = strings.Replace(rest, e.label+" "+e.value+" "+e.percent, "", 1)
					} else if strings.Contains(rest, e.label+" "+e.value) {
						rest = strings.Replace(rest, e.label+" "+e.value, "", 1)
					}
				}
				if strings.TrimSpace(rest) != "" {
					t.Errorf("at %d columns the summary line holds a cut entry (%q left over): %q", width, strings.TrimSpace(rest), line)
				}
			})
		}
	}
}

// ---------------------------------------------------------------------------
// S9 - Scroll
// ---------------------------------------------------------------------------

// S9.11 Given the view is open and the list scrolled, when the left or right
// arrow is pressed, then nothing changes. (D92)
func TestS9_11_View_LeftAndRightDoNothing(t *testing.T) {
	for _, key := range []string{deps.KeyLeft, deps.KeyRight} {
		t.Run(key, func(t *testing.T) {
			// Given: scrolled down by one
			v, f := scrollView(t)
			top := firstVisible(t, f)
			v.send(deps.KeyDown)
			before := v.settled()
			if got := firstVisible(t, before.text()); got != top+1 {
				t.Fatalf("given not established: the down arrow put row %d first, want %d", got+1, top+2)
			}
			requests := v.src.total()

			// When
			v.send(key)
			after := v.settled()

			// Then: the idle frame after it is the idle frame before it
			if done, code := v.exited(); done {
				t.Fatalf("the view exited with status %d on %q", code, key)
			}
			if after.raw() != before.raw() {
				t.Errorf("the screen changed on %q.\nbefore:\n%s\nafter:\n%s", key, before.text(), after.text())
			}
			if n := v.src.total() - requests; n != 0 {
				t.Errorf("%q sent %d requests", key, n)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// S12 - No config yet
// ---------------------------------------------------------------------------

// S12.6 Given no file at the default config path, which is under the home
// folder, when the view is drawn, then the status line names the path with
// the home folder written `~`. (D95)
func TestS12_6_View_NoConfigPathWithTilde(t *testing.T) {
	// Given
	v, w := noConfigView(t)
	home := w.env["HOME"]

	// When
	f := v.settled().text()

	// Then
	_, st := statusLine(t, f)
	if !strings.Contains(st, "~/.config/brl-stocks/config.toml") {
		t.Errorf("the status line does not name ~/.config/brl-stocks/config.toml: %q\n%s", st, f)
	}
	if strings.Contains(st, home) {
		t.Errorf("the status line writes the home folder %s in full: %q", home, st)
	}
}

// S12.7 Given no file at the default config path and a terminal too narrow
// for the whole status line but wide enough for it without the current
// sort, when the view is drawn, then the status line does not show the sort.
// (D95)
func TestS12_7_View_NarrowStatusLineLeavesOutTheSort(t *testing.T) {
	v, whole, sort, without := narrowStatusGiven(t)
	for _, width := range narrowStatusWidths(whole, without) {
		// When
		v.resize(width, viewHeight)
		f := v.settled().text()

		// Then
		_, st := statusLine(t, f)
		if strings.Contains(st, sort) {
			t.Errorf("at %d columns (whole line %d, without the sort %d) the status line shows the sort %q: %q", width, cells(whole), without, sort, st)
		}
	}
}

// S12.8 Given no file at the default config path and a terminal too narrow
// for the whole status line but wide enough for it without the current
// sort, when the view is drawn, then the status line shows the whole
// warning. (D95)
func TestS12_8_View_NarrowStatusLineKeepsTheWholeWarning(t *testing.T) {
	// Given: the warning is what the status line holds with no config and
	// not with one, at the same width and order
	v, whole, _, without := narrowStatusGiven(t)
	cw := newWorld(t, bothOpen, `tickers = ["PETR4", "VALE3", "ITUB4", "AAPL", "MSFT"]`+"\n")
	loadQuotes(cw.src, cw.now, true)
	c := openView(t, cw, viewOpts{})
	c.settled()
	c.resize(wideStatus, viewHeight)
	_, cst := statusLine(t, c.settled().text())
	usual := map[string]bool{}
	for _, s := range statusSegments(cst) {
		usual[s] = true
	}
	var warning []string
	for _, s := range statusSegments(whole) {
		if !usual[s] {
			warning = append(warning, s)
		}
	}
	if len(warning) == 0 {
		t.Fatalf("given not established: no warning in the status line with no config: %q (with a config: %q)", whole, cst)
	}

	for _, width := range narrowStatusWidths(whole, without) {
		// When
		v.resize(width, viewHeight)
		f := v.settled().text()

		// Then
		_, st := statusLine(t, f)
		for _, s := range warning {
			if !strings.Contains(st, s) {
				t.Errorf("at %d columns (whole line %d, without the sort %d) the status line does not show the warning %q whole: %q", width, cells(whole), without, s, st)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// S15 - The last quotes are kept between runs
// ---------------------------------------------------------------------------

// S15.10 Given a cache folder under the home folder that cannot be written,
// when the view is drawn, then the warning in the status line names the
// folder with the home folder written `~`. (D95)
func TestS15_10_View_UnwritableCacheFolderWithTilde(t *testing.T) {
	// Given: no XDG_CACHE_HOME, so the cache is ~/.cache/brl-stocks (7.7),
	// left by a print and then made unwritable
	w := newWorld(t, bothOpen, plainConfig)
	delete(w.env, "XDG_CACHE_HOME")
	home := w.env["HOME"]
	w.cacheDir = filepath.Join(home, ".cache", "brl-stocks")
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.cacheFiles()
	w.lockCache()

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Then
	_, st := statusLine(t, f)
	if !strings.Contains(st, "~/.cache/brl-stocks") {
		t.Errorf("the status line does not name ~/.cache/brl-stocks: %q\n%s", st, f)
	}
	if strings.Contains(st, home) {
		t.Errorf("the status line writes the home folder %s in full: %q", home, st)
	}
}

// ---------------------------------------------------------------------------
// S18 - Command line
// ---------------------------------------------------------------------------

// noTerminal runs `brl-stocks` with no command and no terminal: no
// Terminal, and IsTTY false (the view's test boundary, `IsTTY`).
func noTerminal(t *testing.T) result {
	t.Helper()
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	return runFlags(t, w)
}

// S18.5 Given no terminal (the output is redirected), when `brl-stocks` is
// run with no command, then it exits with status 2. (D94)
func TestS18_5_View_NoTerminalExitsWithStatus2(t *testing.T) {
	if r := noTerminal(t); r.code != 2 {
		t.Errorf("exit status %d with no terminal, want 2\nstderr: %q", r.code, r.stderr)
	}
}

// S18.6 Given no terminal (the output is redirected), when `brl-stocks` is
// run with no command, then one line on the error stream names `brl-stocks
// print`. (D94)
func TestS18_6_View_NoTerminalNamesPrint(t *testing.T) {
	r := noTerminal(t)
	// 7.1: it writes one line to the error stream
	ls := strings.Split(strings.TrimRight(r.stderr, "\n"), "\n")
	if strings.TrimSpace(r.stderr) == "" || len(ls) != 1 {
		t.Errorf("the error stream holds %d lines, want one: %q", len(ls), r.stderr)
	}
	if !strings.Contains(r.stderr, "brl-stocks print") {
		t.Errorf("the error stream does not name `brl-stocks print`: %q", r.stderr)
	}
}
