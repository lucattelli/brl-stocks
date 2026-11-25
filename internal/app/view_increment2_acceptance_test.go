package app_test

// Acceptance tests of the full-screen view, increment 2 of slice 2: the
// summary line, groups and order. Written from docs/spec.md and the view's
// test boundary; they reuse the helpers of view_acceptance_test.go and of the
// print acceptance tests.

import (
	"strings"
	"testing"

	"github.com/lucattelli/brl-stocks/internal/market"
)

// ---------------------------------------------------------------------------
// Helpers of this increment
// ---------------------------------------------------------------------------

// editQuote changes the quote the stand-in returns for in.
func editQuote(w *world, in market.Instrument, change func(q *market.Quote)) {
	w.src.mu.Lock()
	defer w.src.mu.Unlock()
	q := w.src.quotes[in]
	change(&q)
	w.src.quotes[in] = q
}

// firstRow is the index of the first line whose first word is one of the
// tickers.
func firstRow(t *testing.T, frame string, tickers ...string) int {
	t.Helper()
	want := map[string]bool{}
	for _, tk := range tickers {
		want[tk] = true
	}
	for i, l := range lines(frame) {
		if f := strings.Fields(l); len(f) > 0 && want[f[0]] {
			return i
		}
	}
	t.Fatalf("no row of %v in:\n%s", tickers, frame)
	return -1
}

// summaryLine is the one line above the rows that holds label (7.3: the
// summary line is the top of the layout, above the watchlist).
func summaryLine(t *testing.T, frame, label string, tickers ...string) string {
	t.Helper()
	top := firstRow(t, frame, tickers...)
	found := ""
	for _, l := range lines(frame)[:top] {
		if strings.Contains(l, label) {
			if found != "" {
				t.Fatalf("more than one line above the rows holds %q:\n%s", label, frame)
			}
			found = l
		}
	}
	if found == "" {
		t.Fatalf("no line above the rows holds %q:\n%s", label, frame)
	}
	return found
}

// inOrder checks that texts appear in line in this order.
func inOrder(t *testing.T, line string, texts ...string) {
	t.Helper()
	at := 0
	for _, s := range texts {
		i := strings.Index(line[at:], s)
		if i < 0 {
			t.Errorf("%q is not in %q after the previous items %v", s, line, texts)
			return
		}
		at += i + len(s)
	}
}

// nonBlankAbove lists the lines above the first row that are not blank.
func nonBlankAbove(t *testing.T, frame string, tickers ...string) []string {
	t.Helper()
	var got []string
	for _, l := range lines(frame)[:firstRow(t, frame, tickers...)] {
		if strings.TrimSpace(l) != "" {
			got = append(got, l)
		}
	}
	return got
}

// ---------------------------------------------------------------------------
// S2 - Summary line
// ---------------------------------------------------------------------------

// S2.1 Given a config without a `summary` key, when the view is drawn, then
// a line above the watchlist shows Ibovespa, S&P 500 and USD/BRL, each with
// its value and day change percent. (D11, D50)
func TestS2_1_View_DefaultSummaryLine(t *testing.T) {
	// Given: IBOV 158432.10 after 157919.80 (+0.32%), S&P 500 6812.40 after
	// 6819.90 (-0.11%), USD/BRL 5.4321 after 5.4278 (+0.08%)
	_, f := viewWith(t, plainConfig, nil)

	// Then: one line above the rows, the entries in the default order
	// (7.2), labelled as 7.3 labels them
	line := summaryLine(t, f, "IBOV", "PETR4", "VALE3", "AAPL")
	inOrder(t, line, "IBOV", "158.432", "+0,32%", "S&P 500", "6,812.40", "-0.11%", "USD/BRL", "5,4321", "+0,08%")
}

// S2.2 Given `summary = ["USDBRL"]`, when the view is drawn, then the summary
// line shows only USD/BRL. (D11, D42)
func TestS2_2_View_SummaryOfOnlyUSDBRL(t *testing.T) {
	// Given
	_, f := viewWith(t, `summary = ["USDBRL"]`+"\n"+plainConfig, nil)

	// Then
	line := summaryLine(t, f, "USD/BRL", "PETR4", "VALE3", "AAPL")
	inOrder(t, line, "USD/BRL", "5,4321", "+0,08%")
	for _, other := range []string{"IBOV", "S&P 500", "158.432", "6,812.40"} {
		if strings.Contains(f, other) {
			t.Errorf("the view shows %q with summary = [\"USDBRL\"]:\n%s", other, f)
		}
	}
}

// S2.3 Given `summary = []`, when the view is drawn, then there is no
// summary line. (D42)
func TestS2_3_View_EmptySummaryHidesTheLine(t *testing.T) {
	// Given established: without the key, the summary line is there
	_, with := viewWith(t, plainConfig, nil)
	summaryLine(t, with, "IBOV", "PETR4", "VALE3", "AAPL")

	// Given
	_, f := viewWith(t, "summary = []\n"+plainConfig, nil)

	t.Logf("frame with the summary:\n%s\nframe with summary = []:\n%s", with, f)

	// Then: the rows are drawn, and no summary entry is anywhere
	rowLine(t, f, "PETR4")
	for _, label := range []string{"IBOV", "S&P 500", "USD/BRL", "158.432", "6,812.40", "5,4321"} {
		if strings.Contains(f, label) {
			t.Errorf("the view shows %q with summary = []:\n%s", label, f)
		}
	}
}

// S2.5 Given an Ibovespa value of 158432.10, when the summary line is drawn,
// then the Ibovespa value reads `158.432`. (D51, D65)
func TestS2_5_View_IbovespaValue(t *testing.T) {
	_, f := viewWith(t, plainConfig, func(w *world) {
		editQuote(w, ibov, func(q *market.Quote) { q.Price = 158432.10 })
	})
	inOrder(t, summaryLine(t, f, "IBOV", "PETR4", "VALE3", "AAPL"), "IBOV", "158.432")
}

// S2.6 Given an S&P 500 value of 6812.4, when the summary line is drawn,
// then the S&P 500 value reads `6,812.40`. (D51, D65)
func TestS2_6_View_SP500Value(t *testing.T) {
	_, f := viewWith(t, plainConfig, func(w *world) {
		editQuote(w, sp500, func(q *market.Quote) { q.Price = 6812.4 })
	})
	inOrder(t, summaryLine(t, f, "S&P 500", "PETR4", "VALE3", "AAPL"), "S&P 500", "6,812.40")
}

// S2.7 Given a USD/BRL rate of 5.4321, when the summary line is drawn, then
// the USD/BRL value reads `5,4321`. (D50, D51)
func TestS2_7_View_USDBRLValue(t *testing.T) {
	_, f := viewWith(t, plainConfig, func(w *world) {
		editQuote(w, usdbrl, func(q *market.Quote) { q.Price = 5.4321 })
	})
	inOrder(t, summaryLine(t, f, "USD/BRL", "PETR4", "VALE3", "AAPL"), "USD/BRL", "5,4321")
}

// S2.8 Given an Ibovespa day change of +0.32 percent, when the summary line
// is drawn, then its percent reads `+0,32%`. (D50, D51, D65)
func TestS2_8_View_IbovespaPercent(t *testing.T) {
	// Given: 100320 after a close of 100000, exactly +0.32%
	_, f := viewWith(t, plainConfig, func(w *world) {
		editQuote(w, ibov, func(q *market.Quote) { q.Price, q.PrevClose = 100320, 100000 })
	})
	line := summaryLine(t, f, "IBOV", "PETR4", "VALE3", "AAPL")
	inOrder(t, line, "IBOV", "+0,32%")
	if i, j := strings.Index(line, "+0,32%"), strings.Index(line, "S&P 500"); j >= 0 && i > j {
		t.Errorf("+0,32%% is not the Ibovespa's percent: %q", line)
	}
}

// ---------------------------------------------------------------------------
// S4 - Groups
// ---------------------------------------------------------------------------

// S4.1 Given a config with the groups "Brasil" and "US" in that order, when
// the view is drawn, then each name appears as a header above its rows, in
// that order. (D16, D50)
func TestS4_1_View_GroupHeadersInFileOrder(t *testing.T) {
	cases := []struct {
		name   string
		b3, us []string
		load   func(w *world)
	}{
		{"criterion", []string{"PETR4", "VALE3"}, []string{"AAPL", "MSFT"}, func(w *world) { loadQuotes(w.src, w.now, true) }},
		{"20 tickers", twentyB3, twentyUS, func(w *world) { loadTwenty(w.src, w.now) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Given
			config := "[[group]]\nname = \"Brasil\"\ntickers = [\"" + strings.Join(c.b3, `", "`) + "\"]\n\n" +
				"[[group]]\nname = \"US\"\ntickers = [\"" + strings.Join(c.us, `", "`) + "\"]\n"
			w := newWorld(t, bothOpen, config)
			c.load(w)

			// When
			f := openView(t, w, viewOpts{}).settled().text()

			t.Logf("frame:\n%s", f)

			// Then: Brasil, its rows, US, its rows
			brasil, us := lineIndex(t, f, "Brasil"), lineIndex(t, f, "US")
			if brasil >= us {
				t.Errorf("the header Brasil (line %d) is not above the header US (line %d):\n%s", brasil, us, f)
			}
			for _, tk := range c.b3 {
				if i := rowIndex(t, f, tk); i <= brasil || i >= us {
					t.Errorf("row %s (line %d) is not under the header Brasil (line %d), above US (line %d):\n%s", tk, i, brasil, us, f)
				}
			}
			for _, tk := range c.us {
				if i := rowIndex(t, f, tk); i <= us {
					t.Errorf("row %s (line %d) is not under the header US (line %d):\n%s", tk, i, us, f)
				}
			}
		})
	}
}

// noHeaderBetween checks that every non-blank line from the first row to the
// last row is a row of the tickers, except the named headers allowed.
func noHeaderBetween(t *testing.T, frame string, allowed []string, tickers ...string) {
	t.Helper()
	is := map[string]bool{}
	for _, tk := range tickers {
		is[tk] = true
	}
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	ls := lines(frame)
	first, last := -1, -1
	for i, l := range ls {
		if f := strings.Fields(l); len(f) > 0 && is[f[0]] {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		t.Fatalf("no row of %v in:\n%s", tickers, frame)
	}
	for _, l := range ls[first : last+1] {
		f := strings.Fields(l)
		if len(f) == 0 || is[f[0]] || ok[strings.TrimSpace(l)] {
			continue
		}
		t.Errorf("a line among the rows is neither a row nor an allowed header: %q\n%s", l, frame)
	}
}

// S4.2 Given a config with no group, when the view is drawn, then no group
// header appears. (D16, D50)
func TestS4_2_View_NoGroupNoHeader(t *testing.T) {
	for _, c := range []struct {
		name, config string
		tickers      []string
		load         func(w *world)
	}{
		{"criterion", plainConfig, []string{"PETR4", "VALE3", "AAPL"}, func(w *world) { loadQuotes(w.src, w.now, true) }},
		{"20 tickers", twentyConfig(), append(append([]string{}, twentyB3...), twentyUS...), func(w *world) { loadTwenty(w.src, w.now) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Given
			w := newWorld(t, bothOpen, c.config)
			c.load(w)

			// When
			f := openView(t, w, viewOpts{}).settled().text()

			// Then: nothing among the rows but rows, and above them only the
			// summary line and the column header (7.3, "Layout")
			noHeaderBetween(t, f, nil, c.tickers...)
			if above := nonBlankAbove(t, f, c.tickers...); len(above) != 2 {
				t.Errorf("above the rows, %d lines, want the summary line and the column header: %q", len(above), above)
			}
		})
	}
}

// S4.3 Given tickers outside groups and a group, when the view is drawn, then
// the tickers outside groups come first, with no header. (D42, D50)
func TestS4_3_View_TickersOutsideGroupsComeFirst(t *testing.T) {
	// Given: PETR4 and AAPL outside groups, and the group Brasil
	const config = `tickers = ["PETR4", "AAPL"]

[[group]]
name = "Brasil"
tickers = ["VALE3", "ITUB4"]
`
	w := newWorld(t, bothOpen, config)
	loadQuotes(w.src, w.now, true)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Then: first
	brasil := lineIndex(t, f, "Brasil")
	for _, tk := range []string{"PETR4", "AAPL"} {
		if i := rowIndex(t, f, tk); i >= brasil {
			t.Errorf("row %s (line %d), outside groups, is not above the header Brasil (line %d):\n%s", tk, i, brasil, f)
		}
	}
	for _, tk := range []string{"VALE3", "ITUB4"} {
		if i := rowIndex(t, f, tk); i <= brasil {
			t.Errorf("row %s (line %d) is not under the header Brasil (line %d):\n%s", tk, i, brasil, f)
		}
	}
	// Then: no header over them; above the first row only the summary line
	// and the column header
	if above := nonBlankAbove(t, f, "PETR4", "AAPL", "VALE3", "ITUB4"); len(above) != 2 {
		t.Errorf("above the rows outside groups, %d lines, want the summary line and the column header: %q", len(above), above)
	}
	noHeaderBetween(t, f, []string{"Brasil"}, "PETR4", "AAPL", "VALE3", "ITUB4")
}

// S4.4 Given the same ticker in two groups, when a refresh runs, then the
// ticker is requested from the source once. (D50)
func TestS4_4_View_TickerInTwoGroupsIsRequestedOnce(t *testing.T) {
	// Given: PETR4 in both groups; the refresh is the fetch at start
	const config = `[[group]]
name = "Brasil"
tickers = ["PETR4", "VALE3"]

[[group]]
name = "Favourites"
tickers = ["PETR4", "AAPL"]
`
	w := newWorld(t, bothOpen, config)
	loadQuotes(w.src, w.now, true)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Given established: the fetch at start ran and its rows are shown
	if w.src.count(vale3) == 0 || w.src.count(aapl) == 0 {
		t.Fatalf("given not established: the fetch at start did not request VALE3 and AAPL (%d, %d)", w.src.count(vale3), w.src.count(aapl))
	}
	if !hasRow(f, "PETR4") {
		t.Fatalf("given not established: no PETR4 row:\n%s", f)
	}

	// Then
	if n := w.src.count(petr4); n != 1 {
		t.Errorf("PETR4, in two groups, was requested %d times by one refresh, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// S9 - Sort, scroll and help
// ---------------------------------------------------------------------------

// S9.1 Given the view has just started, when it is drawn, then rows are in
// the order of the config file. (D31, D42)
func TestS9_1_View_RowsInFileOrder(t *testing.T) {
	for _, c := range []struct {
		name    string
		tickers []string
		load    func(w *world)
	}{
		// neither A to Z nor by day change (AAPL -0.48%, ITUB4 -1.01%,
		// MSFT +0.40%, PETR4 +0.81%, VALE3 0)
		{"criterion", []string{"VALE3", "AAPL", "PETR4", "MSFT", "ITUB4"}, func(w *world) { loadQuotes(w.src, w.now, true) }},
		{"20 tickers", append(append([]string{}, twentyUS...), twentyB3...), func(w *world) { loadTwenty(w.src, w.now) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Given
			w := newWorld(t, bothOpen, `tickers = ["`+strings.Join(c.tickers, `", "`)+`"]`+"\n")
			c.load(w)

			// When
			f := openView(t, w, viewOpts{}).settled().text()

			// Then
			if got, want := strings.Join(rowTickers(f, c.tickers...), " "), strings.Join(c.tickers, " "); got != want {
				t.Errorf("rows in the order %s, want the file's order %s:\n%s", got, want, f)
			}
		})
	}
}
