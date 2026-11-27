package app_test

// Acceptance tests of the full-screen view, increment 5 of slice 2: narrow
// terminal, resize, sort, scroll, help, other keys, and QR11. Written from
// docs/spec.md and the view's test boundary; they reuse the helpers of the
// earlier view tests and of the print acceptance tests.
//
// Two questions are open and not guessed here: Q84 (whether left and right
// scroll) and Q85 (whether scrolling by one row counts group header lines).
// The scroll tests use a watchlist without groups and only up, down, j, k.

import (
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lucattelli/brl-stocks/internal/deps"
)

// ---------------------------------------------------------------------------
// Helpers of this increment
// ---------------------------------------------------------------------------

// resize sends a resize of the terminal to width by height.
func (r *viewRun) resize(width, height int) {
	r.t.Helper()
	select {
	case r.in <- deps.Input{Width: width, Height: height}:
		r.sent++
	case <-time.After(viewGuard):
		r.t.Fatalf("the view takes no resize")
	}
}

// sortConfig has five rows whose day changes all differ: PETR4 +0.81%, MSFT
// +0.40%, VALE3 0, AAPL -0.48%, ITUB4 -1.01%. Its file order is neither the
// order by day change nor A to Z.
const sortConfig = `tickers = ["VALE3", "AAPL", "PETR4", "ITUB4", "MSFT"]` + "\n"

var (
	sortFileOrder  = []string{"VALE3", "AAPL", "PETR4", "ITUB4", "MSFT"}
	sortByChange   = []string{"PETR4", "MSFT", "VALE3", "AAPL", "ITUB4"}
	sortByTicker   = []string{"AAPL", "ITUB4", "MSFT", "PETR4", "VALE3"}
	sortAllTickers = sortFileOrder
	scrollTickers  = twentyTickers()
	scrollHeight   = 10 // fewer lines than the 20 rows need
	narrowWidths   = []int{50, 70}
	outsideKeys    = []string{"x", "1", " ", deps.KeyEnter, deps.KeyTab, deps.KeyEsc}
	scrollDownKeys = []string{deps.KeyDown, "j"}
	scrollUpKeys   = []string{deps.KeyUp, "k"}
	arrowMentions  = []string{"↑", "↓", "up", "down", "arrow"}
	helpKeyTokens  = []string{"q", "r", "s", "?", "j", "k"}
	qr11Limit      = 100 * time.Millisecond
	qr11PollEvery  = time.Millisecond
)

func order(t *testing.T, f string, tickers ...string) string {
	t.Helper()
	return strings.Join(rowTickers(f, tickers...), " ")
}

func assertOrder(t *testing.T, f, what string, want []string) {
	t.Helper()
	if got := order(t, f, want...); got != strings.Join(want, " ") {
		t.Errorf("%s: rows in the order %s, want %s:\n%s", what, got, strings.Join(want, " "), f)
	}
}

// sortedView opens the view on sortConfig and presses `s` n times, checking
// the order after each press on the way (the givens of S9.2-S9.4).
func sortedView(t *testing.T, config string, presses int) (*viewRun, string) {
	t.Helper()
	w := newWorld(t, bothOpen, config)
	loadQuotes(w.src, w.now, true)
	v := openView(t, w, viewOpts{})
	f := v.settled().text()
	for i := 0; i < presses; i++ {
		v.send("s")
		f = v.settled().text()
	}
	return v, f
}

// scrollView opens the view on 20 tickers without groups and makes the
// terminal shorter than the rows need.
func scrollView(t *testing.T) (*viewRun, string) {
	t.Helper()
	w := newWorld(t, bothOpen, twentyConfig())
	loadTwenty(w.src, w.now)
	v := openView(t, w, viewOpts{})
	v.settled()
	v.resize(viewWidth, scrollHeight)
	f := v.settled().text()
	vis := rowTickers(f, scrollTickers...)
	if len(vis) == 0 || len(vis) >= len(scrollTickers) {
		t.Fatalf("given not established: %d of %d rows on a screen of %d lines, want more rows than fit:\n%s", len(vis), len(scrollTickers), scrollHeight, f)
	}
	return v, f
}

// firstVisible returns the place in the file of the first row on screen,
// and checks that the rows on screen follow the file without a gap.
func firstVisible(t *testing.T, f string) int {
	t.Helper()
	vis := rowTickers(f, scrollTickers...)
	if len(vis) == 0 {
		t.Fatalf("no row on screen:\n%s", f)
	}
	start := -1
	for i, tk := range scrollTickers {
		if tk == vis[0] {
			start = i
		}
	}
	for i, tk := range vis {
		if start+i >= len(scrollTickers) || scrollTickers[start+i] != tk {
			t.Fatalf("the rows on screen %v do not follow the file's order %v", vis, scrollTickers)
		}
	}
	return start
}

// ---------------------------------------------------------------------------
// S1 - Narrow terminal
// ---------------------------------------------------------------------------

// S1.6 Given a terminal too narrow for the full row, when the view is drawn,
// then ticker, last price, day change percent and quote time are still
// shown. (D50)
func TestS1_6_View_NarrowTerminalKeepsTheFourColumns(t *testing.T) {
	for _, width := range narrowWidths {
		t.Run(strconv.Itoa(width)+" columns", func(t *testing.T) {
			// Given: rows that need more than this width at full width
			w := newWorld(t, bothOpen, plainConfig)
			loadQuotes(w.src, w.now, true)
			v := openView(t, w, viewOpts{})
			full := v.settled().text()
			if n := utf8.RuneCountInString(strings.TrimRight(rowLine(t, full, "PETR4"), " ")); n <= width {
				t.Fatalf("given not established: the full row is %d columns, not wider than %d", n, width)
			}

			// When: the terminal is that narrow (the view follows a resize)
			v.resize(width, viewHeight)
			d := v.settled()
			f := d.text()

			// Then
			if d.f.Width != width {
				t.Errorf("the frame is %d wide after a resize to %d", d.f.Width, width)
			}
			assertRowHas(t, f, "PETR4", "PETR4", "R$ 38,42", "+0,81%", wantTime(w.now.Add(-15*time.Minute), w.now))
			assertRowHas(t, f, "AAPL", "AAPL", "US$ 229.87", "-0.48%", wantTime(w.now.Add(-time.Second), w.now))
		})
	}
}

// ---------------------------------------------------------------------------
// S2 - Summary line
// ---------------------------------------------------------------------------

// S2.4 Given more rows than fit the screen, when the user scrolls, then the
// summary line stays in place. (D11)
func TestS2_4_View_SummaryLineStaysWhileScrolling(t *testing.T) {
	// Given
	v, before := scrollView(t)
	top := firstVisible(t, before)
	summary := summaryLine(t, before, "IBOV", scrollTickers...)
	at := -1
	for i, l := range lines(before) {
		if l == summary {
			at = i
		}
	}

	// When
	v.send(deps.KeyDown)
	after := v.settled().text()

	// Given established: the list scrolled
	if firstVisible(t, after) == top {
		t.Fatalf("given not established: the list did not scroll on the down arrow:\n%s", after)
	}

	// Then: the same line, at the same place
	if ls := lines(after); at >= len(ls) || ls[at] != summary {
		t.Errorf("the summary line moved or changed when the list scrolled.\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// ---------------------------------------------------------------------------
// S9 - Sort, scroll and help
// ---------------------------------------------------------------------------

// S9.2 Given file order, when `s` is pressed once, then rows are ordered by
// day change. (D31)
func TestS9_2_View_SortByDayChange(t *testing.T) {
	// Given
	v, f := sortedView(t, sortConfig, 0)
	if order(t, f, sortAllTickers...) != strings.Join(sortFileOrder, " ") {
		t.Fatalf("given not established: the view did not start in file order:\n%s", f)
	}

	// When
	v.send("s")

	// Then: percent, highest first (7.3)
	assertOrder(t, v.settled().text(), "after s", sortByChange)
}

// S9.3 Given the order by day change, when `s` is pressed once, then rows
// are ordered by ticker. (D31)
func TestS9_3_View_SortByTicker(t *testing.T) {
	// Given
	v, f := sortedView(t, sortConfig, 1)
	if order(t, f, sortAllTickers...) != strings.Join(sortByChange, " ") {
		t.Fatalf("given not established: one s did not order by day change:\n%s", f)
	}

	// When
	v.send("s")

	// Then: A to Z
	assertOrder(t, v.settled().text(), "after s, s", sortByTicker)
}

// S9.4 Given the order by ticker, when `s` is pressed once, then rows are
// back in file order. (D31, D50)
func TestS9_4_View_SortBackToFileOrder(t *testing.T) {
	// Given
	v, f := sortedView(t, sortConfig, 2)
	if order(t, f, sortAllTickers...) != strings.Join(sortByTicker, " ") {
		t.Fatalf("given not established: two presses of s did not order by ticker:\n%s", f)
	}

	// When
	v.send("s")

	// Then
	assertOrder(t, v.settled().text(), "after s, s, s", sortFileOrder)
}

// groupsUSFirst: the US group comes first in the file. By day change MSFT
// (+0.40%) heads US and PETR4 (+0.81%) heads Brasil, so groups sorted by
// their rows would swap.
const groupsUSFirst = `[[group]]
name = "US"
tickers = ["AAPL", "MSFT"]

[[group]]
name = "Brasil"
tickers = ["VALE3", "ITUB4", "PETR4"]
`

// S9.5 Given two groups and the order by day change, when the view is drawn,
// then each group's rows run from the highest percent to the lowest. (D50)
func TestS9_5_View_SortInsideEachGroup(t *testing.T) {
	// Given
	_, f := sortedView(t, groupsUSFirst, 1)

	// Then
	if got := strings.Join(rowsUnder(t, f, "US"), " "); got != "MSFT AAPL" {
		t.Errorf("group US runs %q, want MSFT AAPL (+0.40%%, -0.48%%):\n%s", got, f)
	}
	if got := strings.Join(rowsUnder(t, f, "Brasil"), " "); got != "PETR4 VALE3 ITUB4" {
		t.Errorf("group Brasil runs %q, want PETR4 VALE3 ITUB4 (+0.81%%, 0, -1.01%%):\n%s", got, f)
	}
}

// rowsUnder lists the rows of sortConfig's tickers on the lines right under
// a group header, up to the first line that is not one of them.
func rowsUnder(t *testing.T, f, header string) []string {
	t.Helper()
	is := map[string]bool{}
	for _, tk := range sortAllTickers {
		is[tk] = true
	}
	var got []string
	for _, l := range lines(f)[lineIndex(t, f, header)+1:] {
		fl := strings.Fields(l)
		if len(fl) == 0 || !is[fl[0]] {
			break
		}
		got = append(got, fl[0])
	}
	return got
}

// S9.6 Given more rows than fit the screen, when the down arrow or `j` is
// pressed, then the list scrolls down by one row. (D32)
func TestS9_6_View_DownOrJScrollsDownOneRow(t *testing.T) {
	for _, key := range scrollDownKeys {
		t.Run(key, func(t *testing.T) {
			// Given: no groups, so a row is a line (Q85 does not arise)
			v, before := scrollView(t)
			top := firstVisible(t, before)

			// When
			v.send(key)

			// Then
			if got := firstVisible(t, v.settled().text()); got != top+1 {
				t.Errorf("after %q the first row on screen is row %d of the file, want %d", key, got+1, top+2)
			}
		})
	}
}

// S9.7 Given more rows than fit the screen and a scrolled list, when the up
// arrow or `k` is pressed, then the list scrolls up by one row. (D32)
func TestS9_7_View_UpOrKScrollsUpOneRow(t *testing.T) {
	for _, key := range scrollUpKeys {
		t.Run(key, func(t *testing.T) {
			// Given: scrolled down twice
			v, before := scrollView(t)
			top := firstVisible(t, before)
			v.send(deps.KeyDown)
			v.send(deps.KeyDown)
			scrolled := firstVisible(t, v.settled().text())
			if scrolled != top+2 {
				t.Fatalf("given not established: two down arrows put row %d first, want %d", scrolled+1, top+3)
			}

			// When
			v.send(key)

			// Then
			if got := firstVisible(t, v.settled().text()); got != scrolled-1 {
				t.Errorf("after %q the first row on screen is row %d of the file, want %d", key, got+1, scrolled)
			}
		})
	}
}

// S9.8 Given the view is open, when `?` is pressed, then the keys `q`, `r`,
// `s`, the scroll keys and `?` are listed with their actions. (D32)
func TestS9_8_View_HelpListsTheKeys(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	v := openView(t, w, viewOpts{})
	before := v.settled().text()

	// When
	v.send("?")
	f := v.settled().text()

	// Then: each key on a line of its own with more words (its action).
	// The wording is not fixed.
	if f == before {
		t.Fatalf("the screen did not change on ?:\n%s", f)
	}
	keyLine := func(key string) bool {
		for _, l := range lines(f) {
			fields := strings.Fields(l)
			for _, fl := range fields {
				if strings.Trim(fl, "`,:/") == key && len(fields) > 1 {
					return true
				}
			}
		}
		return false
	}
	for _, key := range helpKeyTokens {
		if !keyLine(key) {
			t.Errorf("the help does not list the key %q with its action:\n%s", key, f)
		}
	}
	arrows := false
	for _, a := range arrowMentions {
		arrows = arrows || strings.Contains(strings.ToLower(f), a)
	}
	if !arrows {
		t.Errorf("the help does not list the arrow keys:\n%s", f)
	}
}

// S9.9 Given the view is open, when a key outside that list is pressed, then
// nothing changes. (D32)
func TestS9_9_View_OtherKeysDoNothing(t *testing.T) {
	// Given: the view open and settled, help closed
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	v := openView(t, w, viewOpts{})
	before := v.settled()
	rowLine(t, before.text(), "PETR4")
	requests := w.src.total()

	for _, key := range outsideKeys {
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
		if n := w.src.total() - requests; n != 0 {
			t.Errorf("%q sent %d requests", key, n)
		}
		before = after
	}
}

// S9.10 Given two groups and the order by day change, when the view is
// drawn, then the groups are still in file order. (D50)
func TestS9_10_View_GroupsKeepFileOrderWhenSorted(t *testing.T) {
	// Given
	_, f := sortedView(t, groupsUSFirst, 1)
	if order(t, f, "MSFT", "AAPL") != "MSFT AAPL" {
		t.Fatalf("given not established: the view is not ordered by day change after one s:\n%s", f)
	}

	// Then
	if us, brasil := lineIndex(t, f, "US"), lineIndex(t, f, "Brasil"); us > brasil {
		t.Errorf("the group US, first in the file, is drawn below Brasil (lines %d, %d):\n%s", us, brasil, f)
	}
}

// ---------------------------------------------------------------------------
// QR11 - Keys answer during a fetch
// ---------------------------------------------------------------------------

// QR11: A key press changes the screen within 100 ms, also while a refresh
// is running. Measured with a stand-in source that answers after 5 seconds.
// Here the source holds its answers for as long as the test runs (more than
// 5 seconds would be); the 100 ms are real time, from sending the key to the
// first frame that has handled it and differs from the frame before.
func TestQR11_View_KeyChangesTheScreenDuringAFetch(t *testing.T) {
	for _, key := range []string{"s", "?"} {
		t.Run(key, func(t *testing.T) {
			// Given: the fetch at start is running
			w := newWorld(t, bothOpen, plainConfig)
			loadQuotes(w.src, w.now, true)
			v := openView(t, w, viewOpts{hold: true})
			before := v.beforeAnswer("the rows", showsRows("PETR4", "VALE3", "AAPL"))

			// When
			sent := time.Now()
			v.send(key)

			// Then
			deadline := sent.Add(2 * time.Second) // 20 times the limit: a guard, not the measure
			for {
				var changed *drawn
				for _, d := range v.allFrames() {
					if d.f.Inputs == v.sent && d.raw() != before.raw() {
						d := d
						changed = &d
						break
					}
				}
				if changed != nil {
					if took := time.Since(sent); took > qr11Limit {
						t.Errorf("the screen changed %v after %q, during the fetch at start; want within %v", took, key, qr11Limit)
					}
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("the screen did not change within 2s of %q during the fetch at start", key)
				}
				time.Sleep(qr11PollEvery)
			}
		})
	}
}
