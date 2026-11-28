package app_test

// Acceptance tests of slice 4, increment 1: the sparkline, the last column of
// each row of the full-screen view (spec 7.3 "Sparkline", D13, D50,
// D104-D107). Written from docs/spec.md before the behaviour exists. The
// stand-in source's quotes carry a series (market.Quote.Series, oldest
// first), with every point inside the quote's regular session.

import (
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lucattelli/brl-stocks/internal/deps"
	"github.com/lucattelli/brl-stocks/internal/market"
	"github.com/lucattelli/brl-stocks/internal/provider"
)

// ---------------------------------------------------------------------------
// Helpers of this increment
// ---------------------------------------------------------------------------

// sparkBlocks are the eight blocks of D105, lowest first.
const sparkBlocks = "▁▂▃▄▅▆▇█"

func isBlock(r rune) bool { return strings.ContainsRune(sparkBlocks, r) }

// sparkOf returns the sparkline at the end of a line without SGR codes (the
// run of blocks it ends with, once trailing spaces are taken out) and the
// cell where it starts; "" and -1 when the line ends with none.
func sparkOf(line string) (string, int) {
	rs := []rune(strings.TrimRight(line, " "))
	i := len(rs)
	for i > 0 && isBlock(rs[i-1]) {
		i--
	}
	if i == len(rs) {
		return "", -1
	}
	return string(rs[i:]), i
}

// rowSpark is the sparkline of a ticker's row in a frame.
func rowSpark(t *testing.T, f, ticker string) (string, int) {
	t.Helper()
	return sparkOf(rowLine(t, f, ticker))
}

// blankFrom reports whether the line holds nothing but spaces from cell col
// on.
func blankFrom(line string, col int) bool {
	rs := []rune(line)
	for i := col; i < len(rs); i++ {
		if rs[i] != ' ' {
			return false
		}
	}
	return true
}

// setSeries makes the stand-in's quote for in carry vals as its series, one
// minute apart from the start of the quote's session.
func setSeries(src *fakeSource, in market.Instrument, vals ...float64) {
	src.mu.Lock()
	defer src.mu.Unlock()
	q := src.quotes[in]
	q.Series = nil
	for i, v := range vals {
		q.Series = append(q.Series, market.Point{T: q.Session.Start.Add(time.Duration(i) * time.Minute), V: v})
	}
	src.quotes[in] = q
}

// setAllSeries gives every quote the stand-in knows the series vals.
func setAllSeries(src *fakeSource, vals ...float64) {
	src.mu.Lock()
	var ins []market.Instrument
	for in := range src.quotes {
		ins = append(ins, in)
	}
	src.mu.Unlock()
	for _, in := range ins {
		setSeries(src, in, vals...)
	}
}

// seq is n prices from from, step apart.
func seq(from, step float64, n int) []float64 {
	var v []float64
	for i := 0; i < n; i++ {
		v = append(v, from+step*float64(i))
	}
	return v
}

// A series whose prices sit on the eight levels of D105 exactly: lowest 10,
// highest 17, one level per unit, so no price falls between two levels.
var (
	fiveOnLevels     = []float64{10, 13, 11, 17, 15}
	fiveOnLevelsLine = "▁▄▂█▆"
)

// sparkView opens the view at full width on config, with the source's
// quotes from loadQuotes (US open when usOpen) and every series set to vals,
// and returns its idle frame after the fetch at start.
func sparkView(t *testing.T, config string, usOpen bool, vals ...float64) (*world, *viewRun, drawn) {
	t.Helper()
	now := bothOpen
	if !usOpen {
		now = onlyB3Open
	}
	w := newWorld(t, now, config)
	loadQuotes(w.src, w.now, usOpen)
	setAllSeries(w.src, vals...)
	v := openView(t, w, viewOpts{})
	return w, v, v.settled()
}

// widestRow is the width in cells of the widest row line of the frame.
func widestRow(f string, tickers ...string) int {
	n := 0
	for _, tk := range tickers {
		for _, l := range lines(f) {
			if fs := strings.Fields(l); len(fs) > 0 && fs[0] == tk {
				if c := utf8.RuneCountInString(strings.TrimRight(l, " ")); c > n {
					n = c
				}
			}
		}
	}
	return n
}

// headerLine is the column header: the line that holds TICKER.
func headerLine(t *testing.T, f string) string {
	t.Helper()
	for _, l := range lines(f) {
		if strings.HasPrefix(strings.TrimSpace(l), "TICKER") {
			return l
		}
	}
	t.Fatalf("no column header in:\n%s", f)
	return ""
}

// ---------------------------------------------------------------------------
// S10 - Sparkline
// ---------------------------------------------------------------------------

// S10.1 Given a row with a quote from an open market, when the view is drawn
// at full width, then the row ends with a sparkline of the day's prices so
// far. (D13)
func TestS10_1_View_OpenMarketRowEndsWithTheDaySoFar(t *testing.T) {
	for _, c := range []struct {
		name, config string
		tickers      []string
		load         func(src *fakeSource, now time.Time)
	}{
		{"criterion", plainConfig, []string{"PETR4", "VALE3", "AAPL"}, func(src *fakeSource, now time.Time) { loadQuotes(src, now, true) }},
		{"20 tickers", twentyConfig(), twentyTickers(), func(src *fakeSource, now time.Time) { loadTwenty(src, now) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Given: Monday 15:00 UTC, both markets open; each quote's
			// series is the day so far, five prices on the levels of D105
			w := newWorld(t, bothOpen, c.config)
			c.load(w.src, w.now)
			setAllSeries(w.src, fiveOnLevels...)

			// When
			f := openView(t, w, viewOpts{}).settled().text()

			// Then
			for _, tk := range c.tickers {
				if s, _ := rowSpark(t, f, tk); s != fiveOnLevelsLine {
					t.Errorf("row %s ends with the sparkline %q, want %q (the day's five prices 10, 13, 11, 17, 15): %q", tk, s, fiveOnLevelsLine, rowLine(t, f, tk))
				}
			}
		})
	}
}

// S10.2 Given a row of a closed market, when the view is drawn at full
// width, then the sparkline shows the last trading day. (D50)
func TestS10_2_View_ClosedMarketShowsTheLastTradingDay(t *testing.T) {
	for _, c := range []struct {
		name, config string
		us           []string
		load         func(src *fakeSource, now time.Time)
	}{
		{"criterion", plainConfig, []string{"AAPL"}, func(src *fakeSource, now time.Time) { loadQuotes(src, now, false) }},
		{"20 tickers", twentyConfig(), twentyUS, loadTwentyUSClosed},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Given: Monday 13:30 UTC, the US market closed; the source
			// reports Friday's session, and its series is Friday's
			w := newWorld(t, onlyB3Open, c.config)
			c.load(w.src, w.now)
			for _, in := range instruments(c.us...) {
				setSeries(w.src, in, 17, 15, 10, 13, 11)
			}
			v := openView(t, w, viewOpts{})
			f := v.settled()
			if st := marketStates(t, f); !strings.Contains(st, "US closed") {
				t.Fatalf("given not established: the US market is not shown closed: %q", st)
			}

			// Then
			for _, tk := range c.us {
				if s, _ := rowSpark(t, f.text(), tk); s != "█▆▁▄▂" {
					t.Errorf("closed-market row %s ends with %q, want Friday's sparkline %q: %q", tk, s, "█▆▁▄▂", rowLine(t, f.text(), tk))
				}
			}
		})
	}
}

// S10.3 Given the view, when any key from section 7.3 is pressed, then no
// alert, detail view or news appears. (D13, D32)
func TestS10_3_View_NoKeyOpensAnAlertDetailOrNews(t *testing.T) {
	// Given: rows with a series. What the screen holds besides the rows
	// (summary line, header, any other line), the status line apart (it
	// shows the sort), must stay as it was: an alert, a detail view or news
	// would be a line or a screen of its own.
	_, v, d := sparkView(t, plainConfig, true, fiveOnLevels...)
	tickers := []string{"PETR4", "VALE3", "AAPL"}
	others := func(f string) ([]string, []string) {
		var rest, rows []string
		_, st := statusLine(t, f)
		for _, l := range lines(f) {
			fs := strings.Fields(l)
			switch {
			case len(fs) == 0 || l == st:
			case hasRow(l, "PETR4") || hasRow(l, "VALE3") || hasRow(l, "AAPL"):
				rows = append(rows, fs[0])
			default:
				rest = append(rest, l)
			}
		}
		return rest, rows
	}
	beforeRest, beforeRows := others(d.text())
	if len(beforeRows) != len(tickers) {
		t.Fatalf("given not established: rows %v, want %v:\n%s", beforeRows, tickers, d.text())
	}

	for _, keys := range [][]string{{"r"}, {"s"}, {"s"}, {"s"}, {deps.KeyDown}, {deps.KeyUp}, {"j"}, {"k"}, {"?", deps.KeyEsc}} {
		// When
		for _, k := range keys {
			v.send(k)
		}
		f := v.settled().text()

		// Then
		rest, rows := others(f)
		if strings.Join(rest, "\n") != strings.Join(beforeRest, "\n") {
			t.Errorf("after %q the screen holds other lines than the summary line, header and rows:\nbefore: %q\nafter:  %q\n%s", keys, beforeRest, rest, f)
		}
		if len(rows) != len(tickers) {
			t.Errorf("after %q the rows are %v, want the %d rows of the watchlist:\n%s", keys, rows, len(tickers), f)
		}
	}
	if code := v.quit("q"); code != 0 {
		t.Errorf("q: exit status %d, want 0", code)
	}
}

// S10.4 Given a row whose series has more than 10 prices, when the view is
// drawn at full width, then its sparkline is 10 characters wide. (D50)
func TestS10_4_View_LongSeriesIsTenWide(t *testing.T) {
	_, _, d := sparkView(t, plainConfig, true, seq(10, 0.5, 25)...)
	for _, tk := range []string{"PETR4", "VALE3", "AAPL"} {
		if s, _ := rowSpark(t, d.text(), tk); utf8.RuneCountInString(s) != 10 {
			t.Errorf("row %s, its series 25 prices, has a sparkline %d characters wide, want 10: %q", tk, utf8.RuneCountInString(s), rowLine(t, d.text(), tk))
		}
	}
}

// sparkColours is the colour in force on every character of a row's
// sparkline, on the raw frame.
func sparkColours(t *testing.T, d drawn, ticker string) []string {
	t.Helper()
	s, _ := rowSpark(t, d.text(), ticker)
	if s == "" {
		t.Fatalf("row %s has no sparkline: %q", ticker, rowLine(t, d.text(), ticker))
	}
	line := rowOnTerminal(t, d.raw(), ticker)
	all := foregroundAt(t, line, strings.TrimRight(plain(line), " "))
	return all[len(all)-utf8.RuneCountInString(s):]
}

// S10.5 Given a row whose day change is positive, when it is drawn on a
// terminal at full width, then its sparkline is in the terminal palette's
// green. (D25, D50)
func TestS10_5_View_RisingRowSparklineIsGreen(t *testing.T) {
	// Given: PETR4 38.42 against 38.11
	_, _, d := sparkView(t, plainConfig, true, fiveOnLevels...)
	for _, c := range sparkColours(t, d, "PETR4") {
		if c != "32" && c != "92" {
			t.Errorf("PETR4's sparkline, its day change positive, is in colour %q, want green (SGR 32 or 92): %q", c, rowOnTerminal(t, d.raw(), "PETR4"))
			break
		}
	}
}

// S10.6 Given a row whose day change is negative, when it is drawn on a
// terminal at full width, then its sparkline is in the terminal palette's
// red. (D25, D50)
func TestS10_6_View_FallingRowSparklineIsRed(t *testing.T) {
	// Given: AAPL 229.87 against 230.99
	_, _, d := sparkView(t, plainConfig, true, fiveOnLevels...)
	for _, c := range sparkColours(t, d, "AAPL") {
		if c != "31" && c != "91" {
			t.Errorf("AAPL's sparkline, its day change negative, is in colour %q, want red (SGR 31 or 91): %q", c, rowOnTerminal(t, d.raw(), "AAPL"))
			break
		}
	}
}

// S10.7 Given a row whose day change is exactly zero, when it is drawn on a
// terminal at full width, then its sparkline has no colour. (D50)
func TestS10_7_View_FlatDaySparklineHasNoColour(t *testing.T) {
	// Given: VALE3 61.07 against 61.07
	_, _, d := sparkView(t, plainConfig, true, fiveOnLevels...)
	for _, c := range sparkColours(t, d, "VALE3") {
		if c != "" {
			t.Errorf("VALE3's sparkline, its day change zero, is in colour %q, want none: %q", c, rowOnTerminal(t, d.raw(), "VALE3"))
			break
		}
	}
}

// narrowedByOne opens the view at full width, measures the widest row with
// its sparkline, and resizes the terminal to one cell less.
func narrowedByOne(t *testing.T, config string, tickers []string, load func(src *fakeSource, now time.Time)) (wide, narrow drawn) {
	t.Helper()
	w := newWorld(t, bothOpen, config)
	load(w.src, w.now)
	setAllSeries(w.src, fiveOnLevels...)
	v := openView(t, w, viewOpts{})
	wide = v.settled()
	for _, tk := range tickers {
		if s, _ := rowSpark(t, wide.text(), tk); s == "" {
			t.Fatalf("given not established: at full width row %s has no sparkline: %q", tk, rowLine(t, wide.text(), tk))
		}
	}
	v.resize(widestRow(wide.text(), tickers...)-1, viewHeight)
	return wide, v.settled()
}

// S10.8 Given a terminal one cell narrower than the widest row with its
// sparkline, when the view is drawn, then no row has a sparkline. (D50)
func TestS10_8_View_OneCellNarrowerDropsTheSparkline(t *testing.T) {
	for _, c := range []struct {
		name, config string
		tickers      []string
		load         func(src *fakeSource, now time.Time)
	}{
		{"criterion", plainConfig, []string{"PETR4", "VALE3", "AAPL"}, func(src *fakeSource, now time.Time) { loadQuotes(src, now, true) }},
		{"20 tickers", twentyConfig(), twentyTickers(), func(src *fakeSource, now time.Time) { loadTwenty(src, now) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, d := narrowedByOne(t, c.config, c.tickers, c.load)
			for _, tk := range c.tickers {
				line := rowLine(t, d.text(), tk)
				if strings.ContainsAny(line, sparkBlocks) {
					t.Errorf("row %s still has a sparkline %d cells wide: %q", tk, d.f.Width, line)
				}
			}
		})
	}
}

// S10.10 Given a row whose prices rose steadily all day, when the view is
// drawn at full width, then the lowest character of its sparkline is on the
// left and the highest on the right. (D50)
func TestS10_10_View_RisingDayRunsLeftToRight(t *testing.T) {
	_, _, d := sparkView(t, plainConfig, true, seq(30, 0.25, 15)...)
	s, _ := rowSpark(t, d.text(), "PETR4")
	rs := []rune(s)
	if len(rs) < 2 {
		t.Fatalf("row PETR4 has a sparkline of %d characters: %q", len(rs), rowLine(t, d.text(), "PETR4"))
	}
	lo, hi := rs[0], rs[0]
	for _, r := range rs {
		if r < lo {
			lo = r
		}
		if r > hi {
			hi = r
		}
	}
	if rs[0] != lo || rs[len(rs)-1] != hi {
		t.Errorf("the sparkline of a steadily rising day is %q: want its lowest character (%c) first and its highest (%c) last", s, lo, hi)
	}
}

// S10.11 Given a row whose series has 4 prices, when the view is drawn at
// full width, then its sparkline is 4 characters wide. (D104)
func TestS10_11_View_FourPricesFourCharacters(t *testing.T) {
	_, _, d := sparkView(t, plainConfig, true, 10, 12, 11, 13)
	for _, tk := range []string{"PETR4", "VALE3", "AAPL"} {
		if s, _ := rowSpark(t, d.text(), tk); utf8.RuneCountInString(s) != 4 {
			t.Errorf("row %s, its series 4 prices, has a sparkline %d characters wide (%q), want 4: %q", tk, utf8.RuneCountInString(s), s, rowLine(t, d.text(), tk))
		}
	}
}

// S10.12 Given a row whose series has 25 prices, the first of them the
// day's lowest and the latest the day's highest, when the view is drawn at
// full width, then its sparkline starts with `▁` and ends with `█`.
// (D104, D105)
func TestS10_12_View_FirstAndLatestKept(t *testing.T) {
	vals := []float64{10}
	for i := 1; i < 24; i++ {
		vals = append(vals, 12+float64(i%3))
	}
	vals = append(vals, 20)
	_, _, d := sparkView(t, plainConfig, true, vals...)
	s, _ := rowSpark(t, d.text(), "PETR4")
	if !strings.HasPrefix(s, "▁") || !strings.HasSuffix(s, "█") {
		t.Errorf("the sparkline of 25 prices, lowest first and highest latest, is %q: want it to start with ▁ and end with █", s)
	}
}

// S10.13 Given a row whose series is the 10 prices 10, 11, 12, …, 19, when
// the view is drawn at full width, then its sparkline reads `▁▂▃▃▄▅▆▆▇█`.
// (D104, D105)
func TestS10_13_View_TenRisingPrices(t *testing.T) {
	_, _, d := sparkView(t, plainConfig, true, seq(10, 1, 10)...)
	for _, tk := range []string{"PETR4", "VALE3", "AAPL"} {
		if s, _ := rowSpark(t, d.text(), tk); s != "▁▂▃▃▄▅▆▆▇█" {
			t.Errorf("row %s, its series 10 to 19, reads %q, want ▁▂▃▃▄▅▆▆▇█: %q", tk, s, rowLine(t, d.text(), tk))
		}
	}
}

// S10.23 Given a row whose series is the 3 prices 10, 10.5 and 17, when the
// view is drawn at full width, then its sparkline reads `▁▂█`. (D104, D105,
// D110)
func TestS10_23_View_HalfwayTakesTheHigherLevel(t *testing.T) {
	// Given: on the scale from 10 to 17 the levels are one unit apart, so
	// 10.5 is exactly halfway between the first two
	_, _, d := sparkView(t, plainConfig, true, 10, 10.5, 17)
	for _, tk := range []string{"PETR4", "VALE3", "AAPL"} {
		if s, _ := rowSpark(t, d.text(), tk); s != "▁▂█" {
			t.Errorf("row %s, its series 10, 10.5, 17, reads %q, want ▁▂█: %q", tk, s, rowLine(t, d.text(), tk))
		}
	}
}

// S10.14 Given a row whose series holds the same price 10 times, when the
// view is drawn at full width, then its sparkline reads `▄▄▄▄▄▄▄▄▄▄`.
// (D104, D105)
func TestS10_14_View_FlatSeries(t *testing.T) {
	_, _, d := sparkView(t, plainConfig, true, seq(38.42, 0, 10)...)
	for _, tk := range []string{"PETR4", "VALE3", "AAPL"} {
		if s, _ := rowSpark(t, d.text(), tk); s != "▄▄▄▄▄▄▄▄▄▄" {
			t.Errorf("row %s, its series one price 10 times, reads %q, want ▄▄▄▄▄▄▄▄▄▄: %q", tk, s, rowLine(t, d.text(), tk))
		}
	}
}

// sparkColumn is where the sparkline starts in the frame: the cell where
// the given row's sparkline starts. It fails when that row has none.
func sparkColumn(t *testing.T, f, ticker string) int {
	t.Helper()
	s, col := rowSpark(t, f, ticker)
	if s == "" {
		t.Fatalf("given not established: row %s has no sparkline, so the column is not on screen: %q", ticker, rowLine(t, f, ticker))
	}
	return col
}

// assertBlankColumn checks that a row holds nothing from the sparkline's
// column on: neither blocks nor `—`.
func assertBlankColumn(t *testing.T, f, ticker string, col int) {
	t.Helper()
	if line := rowLine(t, f, ticker); !blankFrom(line, col) {
		t.Errorf("row %s is not blank in the sparkline's column (from cell %d): %q", ticker, col, line)
	}
}

// S10.15 Given a not-found row, when the view is drawn at full width, then
// its sparkline column is blank. (D106)
func TestS10_15_View_NotFoundRowHasABlankSparkline(t *testing.T) {
	_, _, d := sparkView(t, `tickers = ["PETR4", "XXXX9", "AAPL"]`+"\n", true, fiveOnLevels...)
	if line := rowLine(t, d.text(), "XXXX9"); !strings.Contains(line, "not found") {
		t.Fatalf("given not established: XXXX9 is not shown as not found: %q", line)
	}
	assertBlankColumn(t, d.text(), "XXXX9", sparkColumn(t, d.text(), "PETR4"))
}

// S10.16 Given a row with no data yet, when the view is drawn at full width,
// then its sparkline column is blank. (D106)
func TestS10_16_View_NoDataRowHasABlankSparkline(t *testing.T) {
	// Given: no cache; VALE3's fetch at start fails, so it has no data yet
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	setAllSeries(w.src, fiveOnLevels...)
	w.src.fail(vale3, provider.Unavailable)
	f := openView(t, w, viewOpts{}).settled().text()
	if line := rowLine(t, f, "VALE3"); strings.Contains(line, "R$") {
		t.Fatalf("given not established: VALE3 shows a quote: %q", line)
	}
	assertBlankColumn(t, f, "VALE3", sparkColumn(t, f, "PETR4"))
}

// S10.17 Given a quote whose series has no prices, when the view is drawn at
// full width, then its row's sparkline column is blank. (D106)
func TestS10_17_View_EmptySeriesHasABlankSparkline(t *testing.T) {
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	setAllSeries(w.src, fiveOnLevels...)
	setSeries(w.src, vale3)
	f := openView(t, w, viewOpts{}).settled().text()
	assertRowHas(t, f, "VALE3", "R$ 61,07")
	assertBlankColumn(t, f, "VALE3", sparkColumn(t, f, "PETR4"))
}

// S10.18 Given a row with a sparkline whose next refresh fails, when the
// view is drawn at full width, then the stale row shows the same sparkline.
// (D106)
func TestS10_18_View_StaleRowKeepsItsSparkline(t *testing.T) {
	// Given: PETR4 fetched at start with a series; from then on every fetch
	// of it fails
	w := clockWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	setAllSeries(w.src, fiveOnLevels...)
	v := openOnClock(t, w)
	before, _ := rowSpark(t, v.settled(viewGuard).text(), "PETR4")
	if before == "" {
		t.Fatalf("given not established: PETR4 has no sparkline before the failure")
	}
	w.src.fail(petr4, provider.Unavailable)

	// When: the next refresh fails
	f := v.elapse(61 * time.Second).text()
	if v.src.requestedIn(petr4, bothOpen, bothOpen.Add(61*time.Second)) == 0 {
		t.Fatalf("given not established: the next refresh did not request PETR4")
	}
	if !viewStaleMark.MatchString(rowLine(t, f, "PETR4")) {
		t.Fatalf("given not established: PETR4 is not stale: %q", rowLine(t, f, "PETR4"))
	}

	// Then
	if s, _ := rowSpark(t, f, "PETR4"); s != before {
		t.Errorf("stale row PETR4 shows the sparkline %q, want the same as before the failure, %q: %q", s, before, rowLine(t, f, "PETR4"))
	}
}

// S10.19 Given a cache left by an earlier run whose row had a sparkline, and
// a source that has not answered yet, when the view opens at full width,
// then the cached row shows that sparkline. (D106)
func TestS10_19_View_CachedRowShowsItsSparkline(t *testing.T) {
	// Given: an earlier view, opened and closed, showed PETR4 with a
	// sparkline; 12 minutes later the source has another series, and holds
	// its answers
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	setAllSeries(w.src, fiveOnLevels...)
	earlier, _ := rowSpark(t, openAndQuit(t, w, viewOpts{}, "q"), "PETR4")
	if earlier == "" {
		t.Fatalf("given not established: the earlier run showed no sparkline for PETR4")
	}
	w.advance(12 * time.Minute)
	setAllSeries(w.src, seq(10, 1, 10)...)

	// When
	v := openView(t, w, viewOpts{hold: true})
	f := v.beforeAnswer("the cached rows", func(d drawn) bool {
		return hasRow(d.text(), "PETR4") && strings.Contains(rowLine(t, d.text(), "PETR4"), "R$ 38,42")
	}).text()

	// Then
	if s, _ := rowSpark(t, f, "PETR4"); s != earlier {
		t.Errorf("cached row PETR4 shows the sparkline %q before any answer, want the earlier run's %q: %q", s, earlier, rowLine(t, f, "PETR4"))
	}
}

// S10.20 Given the view at full width, when the column header is drawn, then
// the sparkline's column is headed `TODAY`, starting where the sparklines
// start. (D107, D109)
func TestS10_20_View_ColumnHeadedToday(t *testing.T) {
	_, _, d := sparkView(t, plainConfig, true, fiveOnLevels...)
	col := sparkColumn(t, d.text(), "PETR4")
	h := headerLine(t, d.text())
	i := strings.Index(h, "TODAY")
	if i < 0 {
		t.Fatalf("the column header has no TODAY: %q", h)
	}
	if at := utf8.RuneCountInString(h[:i]); at != col {
		t.Errorf("TODAY starts at cell %d of the header, the sparkline at cell %d: %q", at, col, h)
	}
}

// S10.21 Given a terminal one cell narrower than the widest row with its
// sparkline, when the view is drawn, then the column header has no `TODAY`.
// (D50, D107)
func TestS10_21_View_OneCellNarrowerDropsToday(t *testing.T) {
	_, d := narrowedByOne(t, plainConfig, []string{"PETR4", "VALE3", "AAPL"}, func(src *fakeSource, now time.Time) { loadQuotes(src, now, true) })
	if h := headerLine(t, d.text()); strings.Contains(h, "TODAY") {
		t.Errorf("at %d cells, one narrower than the widest row, the header still has TODAY: %q", d.f.Width, h)
	}
}

// S10.22 Given rows whose quote times and stale marks differ in width, when
// the view is drawn at full width, then every sparkline starts two spaces
// after the end of the widest quote time with its stale mark. (D107)
func TestS10_22_View_SparklinesStartTwoAfterTheWidestTime(t *testing.T) {
	// Given: PETR4 fails from the start on, so 12 minutes later it reads
	// !12m; VALE3 fails from 10 minutes on, so its mark is a few minutes;
	// AAPL stays fresh
	w := clockWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	setAllSeries(w.src, fiveOnLevels...)
	v := openOnClock(t, w)
	v.settled(viewGuard)
	w.src.fail(petr4, provider.Unavailable)
	v.elapse(10 * time.Minute)
	w.src.fail(vale3, provider.Unavailable)
	d := v.elapse(2 * time.Minute)
	f := d.text()

	quoteTimes := map[string]time.Time{"PETR4": bothOpen.Add(-15 * time.Minute), "VALE3": bothOpen.Add(-15 * time.Minute), "AAPL": bothOpen.Add(-time.Second)}
	ends := map[string]int{}
	widest := 0
	for tk, at := range quoteTimes {
		line := rowLine(t, f, tk)
		tm := wantTime(at, d.f.Now)
		i := strings.Index(line, tm)
		if i < 0 {
			t.Fatalf("row %s does not show its quote time %s: %q", tk, tm, line)
		}
		end := utf8.RuneCountInString(line[:i]) + utf8.RuneCountInString(tm)
		if m := viewStaleMark.FindStringIndex(line[i:]); m != nil {
			end = utf8.RuneCountInString(line[:i+m[1]])
		}
		ends[tk] = end
		if end > widest {
			widest = end
		}
	}
	if ends["PETR4"] == ends["VALE3"] || ends["PETR4"] == ends["AAPL"] {
		t.Fatalf("given not established: the quote times with their marks do not differ in width (ends %v):\n%s", ends, f)
	}

	// Then
	for tk := range quoteTimes {
		s, col := rowSpark(t, f, tk)
		if s == "" {
			t.Errorf("row %s has no sparkline: %q", tk, rowLine(t, f, tk))
			continue
		}
		if col != widest+2 {
			t.Errorf("row %s's sparkline starts at cell %d, want %d (two after the widest quote time with its mark, ending at %d): %q", tk, col, widest+2, widest, rowLine(t, f, tk))
		}
	}
}

// ---------------------------------------------------------------------------
// Criteria of slice 2 shown again with rows that carry a series
// ---------------------------------------------------------------------------

// S1.6 Given a terminal too narrow for the full row, when the view is drawn,
// then ticker, last price, day change percent and quote time are still
// shown. (D50)
func TestS1_6_Sparkline_NarrowTerminalKeepsTheFourColumns(t *testing.T) {
	for _, width := range narrowWidths {
		t.Run(strconv.Itoa(width)+" columns", func(t *testing.T) {
			w, v, d := sparkView(t, plainConfig, true, seq(10, 1, 10)...)
			if s, _ := rowSpark(t, d.text(), "PETR4"); s == "" {
				t.Fatalf("given not established: at full width PETR4 has no sparkline: %q", rowLine(t, d.text(), "PETR4"))
			}
			if n := widestRow(d.text(), "PETR4"); n <= width {
				t.Fatalf("given not established: the full row is %d columns, not wider than %d", n, width)
			}
			v.resize(width, viewHeight)
			f := v.settled().text()
			assertRowHas(t, f, "PETR4", "PETR4", "R$ 38,42", "+0,81%", wantTime(w.now.Add(-15*time.Minute), w.now))
			assertRowHas(t, f, "AAPL", "AAPL", "US$ 229.87", "-0.48%", wantTime(w.now.Add(-time.Second), w.now))
		})
	}
}

// S16.3 Given `NO_COLOR` is set, when the view or the one-shot output is
// drawn, then it contains no colour codes. (D25)
func TestS16_3_Sparkline_NoColorMeansNoColourCodes(t *testing.T) {
	// Given established: without NO_COLOR, the same view shows sparklines in
	// colour
	_, _, on := sparkView(t, plainConfig, true, fiveOnLevels...)
	if s, _ := rowSpark(t, on.text(), "PETR4"); s == "" || !colourCode(on.raw()) {
		t.Fatalf("given not established: without NO_COLOR, PETR4 has no sparkline or the view no colour:\n%q", on.raw())
	}

	// When: NO_COLOR set
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	setAllSeries(w.src, fiveOnLevels...)
	v := openView(t, w, viewOpts{env: map[string]string{"NO_COLOR": "1"}})
	f := v.settled()
	if s, _ := rowSpark(t, f.text(), "PETR4"); s == "" {
		t.Fatalf("given not established: with NO_COLOR, PETR4 has no sparkline: %q", rowLine(t, f.text(), "PETR4"))
	}

	// Then
	for i, d := range v.allFrames() {
		if colourCode(d.raw()) {
			t.Errorf("frame %d holds colour codes with NO_COLOR set:\n%q", i, d.raw())
			break
		}
	}
}
