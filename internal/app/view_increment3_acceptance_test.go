package app_test

// Acceptance tests of the full-screen view, increment 3 of slice 2: market
// states, stale rows, the error line, not-found rows. Written from
// docs/spec.md and the view's test boundary; they reuse the helpers of the
// earlier view tests and of the print acceptance tests. In this slice the
// view fetches once, when it opens: "a refresh" is that fetch at start, and
// "when the view is drawn" after it is the idle frame that follows it.

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lucattelli/brl-stocks/internal/market"
	"github.com/lucattelli/brl-stocks/internal/provider"
)

// ---------------------------------------------------------------------------
// Helpers of this increment
// ---------------------------------------------------------------------------

// viewStaleMark is the mark of 7.3: `!` and an age (`!45s`, `!12m`, `!3h`, `!2d`).
var viewStaleMark = regexp.MustCompile(`!\d+[smhd]\b`)

// statusLine is the last line of the frame that is not blank (7.3, Layout:
// the status line is at the bottom).
func statusLine(t *testing.T, frame string) (int, string) {
	t.Helper()
	ls := lines(frame)
	for i := len(ls) - 1; i >= 0; i-- {
		if strings.TrimSpace(ls[i]) != "" {
			return i, ls[i]
		}
	}
	t.Fatalf("the frame is blank")
	return -1, ""
}

// errorZone lists the lines that are not blank between the last row and the
// status line: where 7.3 puts the error line.
func errorZone(t *testing.T, frame string, tickers ...string) []string {
	t.Helper()
	is := map[string]bool{}
	for _, tk := range tickers {
		is[tk] = true
	}
	ls := lines(frame)
	last := -1
	for i, l := range ls {
		if f := strings.Fields(l); len(f) > 0 && is[f[0]] {
			last = i
		}
	}
	if last < 0 {
		t.Fatalf("no row of %v in:\n%s", tickers, frame)
	}
	st, _ := statusLine(t, frame)
	var got []string
	for i := last + 1; i < st; i++ {
		if strings.TrimSpace(ls[i]) != "" {
			got = append(got, ls[i])
		}
	}
	return got
}

// theErrorLine is the one error line of the frame.
func theErrorLine(t *testing.T, frame string, tickers ...string) string {
	t.Helper()
	z := errorZone(t, frame, tickers...)
	if len(z) != 1 {
		t.Fatalf("%d lines between the rows and the status line, want one error line: %q\n%s", len(z), z, frame)
	}
	return z[0]
}

// primeThenFail leaves a cache from an earlier `print` at the world's time,
// moves the clock on by age, and then makes the source fail as fail says.
func primeThenFail(w *world, age time.Duration, rows []market.Instrument, fail func(src *fakeSource)) {
	w.t.Helper()
	w.prime(rows...)
	w.advance(age)
	fail(w.src)
}

func twentyTickers() []string {
	return append(append([]string{}, twentyB3...), twentyUS...)
}

// failureCase is a watchlist for the failure checks, as the criterion gives
// it and with 20 tickers (QR1).
type failureCase struct {
	name    string
	config  string
	tickers []string
	prices  func(w *world) map[string]string
}

func failureCases() []failureCase {
	return []failureCase{
		{"criterion", plainConfig, []string{"PETR4", "VALE3", "AAPL"}, func(w *world) map[string]string {
			loadQuotes(w.src, w.now, true)
			return map[string]string{"PETR4": "R$ 38,42", "VALE3": "R$ 61,07", "AAPL": "US$ 229.87"}
		}},
		{"20 tickers", twentyConfig(), twentyTickers(), func(w *world) map[string]string {
			return loadTwenty(w.src, w.now)
		}},
	}
}

// ---------------------------------------------------------------------------
// S5 - Markets that are closed
// ---------------------------------------------------------------------------

// S5.4 Given the US market is closed, when the view is drawn, then US rows
// show the last close. (D15)
func TestS5_4_View_ClosedUSRowsShowTheLastClose(t *testing.T) {
	// Given: Monday 13:30 UTC; the source reports Friday's US session and
	// its close, AAPL 229.87 and MSFT 512.30
	w := newWorld(t, onlyB3Open, `tickers = ["PETR4", "AAPL", "MSFT"]`+"\n")
	loadQuotes(w.src, w.now, false)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Then
	assertRowHas(t, f, "AAPL", "US$ 229.87")
	assertRowHas(t, f, "MSFT", "US$ 512.30")
}

// S5.5 Given the US market is closed and B3 is open, when the view is drawn,
// then the status line reads `B3 open · US closed`. (D15, D47)
func TestS5_5_View_StatusLineB3OpenUSClosed(t *testing.T) {
	// Given
	w := newWorld(t, onlyB3Open, plainConfig)
	loadQuotes(w.src, w.now, false)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Then: the status line holds the market states (it also holds the sort)
	if _, st := statusLine(t, f); !strings.Contains(st, "B3 open · US closed") {
		t.Errorf("the status line reads %q, want it to read `B3 open · US closed`:\n%s", st, f)
	}
}

// S5.6 Given a closed market whose rows were last fetched, with success, two
// days ago, when the view is drawn, then those rows carry no stale mark.
// (D40)
func TestS5_6_View_ClosedMarketFetchedTwoDaysAgoIsNotStale(t *testing.T) {
	// Given: a print on Saturday 13:30 UTC fetched the US rows (Friday's
	// close); the view opens on Monday 13:30 UTC, before the US session,
	// with the same quotes at the source
	const config = "summary = []\n" + `tickers = ["AAPL", "MSFT"]` + "\n"
	w := newWorld(t, onlyB3Open.Add(-48*time.Hour), config)
	loadQuotes(w.src, w.now, false)
	w.prime(aapl, msft)
	w.advance(48 * time.Hour)
	before := w.src.count(aapl) + w.src.count(msft)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Given established: the view did not fetch them again, so their last
	// fetch is still the one of two days ago (6.1: a closed market's rows
	// are not refreshed)
	if n := w.src.count(aapl) + w.src.count(msft) - before; n != 0 {
		t.Fatalf("given not established: the view requested the closed market's rows %d times, so their last fetch is not two days old", n)
	}

	// Then
	for _, tk := range []string{"AAPL", "MSFT"} {
		line := rowLine(t, f, tk)
		if !strings.Contains(line, "US$") {
			t.Errorf("row %s shows no quote: %q", tk, line)
		}
		if viewStaleMark.MatchString(line) {
			t.Errorf("row %s of a closed market, fetched two days ago, is marked stale: %q", tk, line)
		}
	}
}

// S5.12 Given a row whose last fetch failed before its market closed, when
// the view is drawn after the close, then the row is still marked stale.
// (D40)
func TestS5_12_View_FailedBeforeTheCloseStaysStale(t *testing.T) {
	// Given: Monday 15:00 UTC a print fetches; at 15:05 a print's fetch of
	// AAPL fails; the US session ends at 21:00 and the view opens at 22:00.
	// AAPL keeps failing, so whatever the view tries, its last fetch failed.
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(5 * time.Minute)
	w.src.fail(aapl, provider.Unavailable)
	before := w.src.count(aapl)
	w.runOK("print")
	if w.src.count(aapl) == before {
		t.Fatalf("given not established: the print at 15:05 did not request AAPL, so no fetch of it failed")
	}
	w.now = monday.Add(22 * time.Hour)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Then
	if line := rowLine(t, f, "AAPL"); !viewStaleMark.MatchString(line) {
		t.Errorf("row AAPL, whose last fetch failed before the close, carries no stale mark after it: %q\n%s", line, f)
	}
}

// ---------------------------------------------------------------------------
// S6 - When a refresh fails
// ---------------------------------------------------------------------------

// S6.1 Given rows with quotes and an open market, when a refresh fails, then
// the rows still show their last known prices. (D19)
func TestS6_1_View_FailedRefreshKeepsLastKnownPrices(t *testing.T) {
	for _, c := range failureCases() {
		t.Run(c.name, func(t *testing.T) {
			// Given: quotes from a print 5 minutes ago; now the source is
			// unreachable
			w := newWorld(t, bothOpen, c.config)
			prices := c.prices(w)
			primeThenFail(w, 5*time.Minute, []market.Instrument{petr4, aapl}, func(src *fakeSource) { src.failAll(provider.Unavailable) })

			// When
			v := openView(t, w, viewOpts{})
			f := v.settled().text()

			// Then
			for tk, price := range prices {
				assertRowHas(t, f, tk, price)
			}
		})
	}
}

// S6.2 Given a row whose last fetch failed, when the view is drawn, then the
// row is marked stale with its age. (D19, D40)
func TestS6_2_View_FailedRowMarkedStaleWithItsAge(t *testing.T) {
	// Given: PETR4 last fetched with success 5 minutes ago; its fetch at
	// start fails
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	primeThenFail(w, 5*time.Minute, plainRows, func(src *fakeSource) { src.fail(petr4, provider.Unavailable) })
	before := w.src.count(petr4)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Given established: the view did try PETR4
	if w.src.count(petr4) == before {
		t.Fatalf("given not established: the view did not request PETR4, so no fetch of it failed")
	}

	// Then: `!` and the age since the last successful fetch, 5 minutes
	if line := rowLine(t, f, "PETR4"); !strings.Contains(line, "!5m") {
		t.Errorf("row PETR4 does not carry the stale mark with its age, !5m: %q", line)
	}
}

// S6.3 Given a refresh in which five rows failed, when the view is drawn,
// then exactly one error line is shown. (D19)
func TestS6_3_View_FiveFailedRowsOneErrorLine(t *testing.T) {
	five := []market.Instrument{petr4, vale3, itub4, aapl, msft}
	for _, c := range []struct {
		name, config string
		tickers      []string
		load         func(w *world)
	}{
		{"criterion", `tickers = ["PETR4", "VALE3", "ITUB4", "AAPL", "MSFT"]` + "\n", []string{"PETR4", "VALE3", "ITUB4", "AAPL", "MSFT"},
			func(w *world) { loadQuotes(w.src, w.now, true) }},
		{"20 tickers", twentyConfig(), twentyTickers(), func(w *world) { loadTwenty(w.src, w.now) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Given: five rows fail, the rest answer; the five fail in two
			// different ways
			w := newWorld(t, bothOpen, c.config)
			c.load(w)
			for i, in := range five {
				kind := provider.Unavailable
				if i%2 == 1 {
					kind = provider.BadResponse
				}
				w.src.fail(in, kind)
			}

			// When
			f := openView(t, w, viewOpts{}).settled().text()

			// Then
			z := errorZone(t, f, c.tickers...)
			if len(z) != 1 {
				t.Errorf("%d lines between the rows and the status line, want exactly one error line: %q\n%s", len(z), z, f)
			}
			n := 0
			for _, l := range lines(f) {
				for _, p := range causePhrases {
					if strings.Contains(l, p) {
						n++
						break
					}
				}
			}
			if n != 1 {
				t.Errorf("%d lines name a cause, want exactly one:\n%s", n, f)
			}
		})
	}
}

// heldStale is the scenario of S6.7 and S6.10 (scope, notes): a cache left
// by a print 12 minutes ago, no failed fetch since, an open market, and a
// source that holds its answers, so the view shows the cached rows before
// its fetch at start.
func heldStale(t *testing.T) string {
	t.Helper()
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(12 * time.Minute)
	v := openView(t, w, viewOpts{hold: true})
	return v.beforeAnswer("the cached rows", func(d drawn) bool {
		return hasRow(d.text(), "PETR4") && strings.Contains(rowLine(t, d.text(), "PETR4"), "R$ 38,42")
	}).text()
}

// S6.7 Given an open market and a row whose last successful fetch is older
// than two refresh intervals with no failed fetch since (the machine just
// woke from sleep), when the view is drawn, then the row is marked stale.
// (D40)
func TestS6_7_View_OldFetchWithoutFailureIsStale(t *testing.T) {
	f := heldStale(t)
	if line := rowLine(t, f, "PETR4"); !viewStaleMark.MatchString(line) {
		t.Errorf("row PETR4, last fetched 12 minutes ago with the market open, is not marked stale: %q\n%s", line, f)
	}
}

// S6.8 Given a refresh in which one row fails and the others succeed, when
// the view is drawn, then only that row is marked stale. (D40)
func TestS6_8_View_OnlyTheFailedRowIsStale(t *testing.T) {
	for _, c := range failureCases() {
		t.Run(c.name, func(t *testing.T) {
			// Given: quotes from a print 5 minutes ago; now only VALE3 fails
			w := newWorld(t, bothOpen, c.config)
			c.prices(w)
			primeThenFail(w, 5*time.Minute, []market.Instrument{petr4, vale3, aapl}, func(src *fakeSource) { src.fail(vale3, provider.Unavailable) })
			before := w.src.count(vale3)

			// When
			f := openView(t, w, viewOpts{}).settled().text()

			// Given established
			if w.src.count(vale3) == before {
				t.Fatalf("given not established: the view did not request VALE3, so its fetch did not fail")
			}

			// Then
			for _, tk := range c.tickers {
				line := rowLine(t, f, tk)
				if stale := viewStaleMark.MatchString(line); stale != (tk == "VALE3") {
					t.Errorf("row %s: marked stale %v, want %v: %q", tk, stale, tk == "VALE3", line)
				}
			}
		})
	}
}

// S6.9 Given a failed refresh, when the error line is drawn, then it names
// the cause, the number of rows not updated and the time of the next
// attempt. (D50, D76, D79)
func TestS6_9_View_ErrorLineNamesCauseCountAndNextAttempt(t *testing.T) {
	for _, c := range []struct {
		name, config, cause string
		kind                provider.Kind
		tickers             []string
		load                func(w *world)
	}{
		{"unreachable", plainConfig, "source unreachable", provider.Unavailable, []string{"PETR4", "VALE3", "AAPL"},
			func(w *world) { loadQuotes(w.src, w.now, true) }},
		{"not understood", plainConfig, "source answer not understood", provider.BadResponse, []string{"PETR4", "VALE3", "AAPL"},
			func(w *world) { loadQuotes(w.src, w.now, true) }},
		{"20 tickers", twentyConfig(), "source unreachable", provider.Unavailable, twentyTickers(),
			func(w *world) { loadTwenty(w.src, w.now) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Given: every row and summary entry fails (not a refusal); the
			// first wait after a failure is one interval, 60 seconds (6.2)
			w := newWorld(t, bothOpen, c.config)
			c.load(w)
			w.src.failAll(c.kind)
			next := w.now.Add(60 * time.Second).In(time.Local).Format("15:04")

			// When
			f := openView(t, w, viewOpts{}).settled().text()

			// Then
			line := theErrorLine(t, f, c.tickers...)
			if !strings.HasPrefix(strings.TrimSpace(line), c.cause) {
				t.Errorf("the error line does not name the cause %q: %q", c.cause, line)
			}
			count := regexp.MustCompile(`\b` + strconv.Itoa(len(c.tickers)) + `\b`)
			if !count.MatchString(line) {
				t.Errorf("the error line does not give the number of rows not updated, %d: %q", len(c.tickers), line)
			}
			if !strings.Contains(line, next) {
				t.Errorf("the error line does not give the time of the next attempt, %s: %q", next, line)
			}
		})
	}
}

// S6.10 Given a stale row whose last successful fetch was 12 minutes ago,
// when the row is drawn, then the mark reads `!12m`. (D50)
func TestS6_10_View_StaleMarkReadsTwelveMinutes(t *testing.T) {
	f := heldStale(t)
	if line := rowLine(t, f, "PETR4"); !strings.Contains(line, "!12m") {
		t.Errorf("row PETR4, last fetched 12 minutes ago, does not read !12m: %q\n%s", line, f)
	}
}

// S6.11 Given every row updates and a summary entry fails, when the view is
// drawn, then the error line says the summary was not updated. (D81)
func TestS6_11_View_OnlySummaryFailedSaysSummaryNotUpdated(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.src.fail(ibov, provider.Unavailable)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Given established: every row updated
	for tk, price := range map[string]string{"PETR4": "R$ 38,42", "VALE3": "R$ 61,07", "AAPL": "US$ 229.87"} {
		if line := rowLine(t, f, tk); !strings.Contains(line, price) || viewStaleMark.MatchString(line) {
			t.Fatalf("given not established: row %s did not update: %q", tk, line)
		}
	}

	// Then
	if line := theErrorLine(t, f, "PETR4", "VALE3", "AAPL"); !strings.Contains(line, "summary not updated") {
		t.Errorf("the error line does not say `summary not updated`: %q", line)
	}
}

// ---------------------------------------------------------------------------
// S8 - A ticker the source does not know
// ---------------------------------------------------------------------------

// S8.1 Given the ticker `XXXX9`, which the source does not know, when the
// view is drawn, then its row shows "not found" in place of the price. (D39)
func TestS8_1_View_UnknownTickerShowsNotFound(t *testing.T) {
	// Given: the stand-in has no quote for XXXX9
	w := newWorld(t, bothOpen, notFoundConfig)
	loadQuotes(w.src, w.now, true)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Then
	line := rowLine(t, f, "XXXX9")
	if !strings.Contains(line, "not found") {
		t.Fatalf("row XXXX9 does not show \"not found\": %q", line)
	}
	if strings.Contains(line, "R$") || strings.Contains(line, "US$") {
		t.Errorf("row XXXX9 shows a price: %q", line)
	}
	// "in place of the price": where the prices end (7.3, Alignment)
	_, priceEnd := span(t, rowLine(t, f, "PETR4"), "R$ 38,42")
	if _, end := span(t, line, "not found"); end != priceEnd {
		t.Errorf("\"not found\" ends at column %d, the price of PETR4 at %d:\n%s", end, priceEnd, f)
	}
}

// S8.2 Given one not-found row among others, when a refresh runs, then the
// other rows show fresh quotes. (D39)
func TestS8_2_View_OtherRowsStayFresh(t *testing.T) {
	// Given: the refresh is the fetch at start; the source has new prices
	w := newWorld(t, bothOpen, `tickers = ["PETR4", "XXXX9", "AAPL"]`+"\n")
	loadQuotes(w.src, w.now, true)
	w.src.setPrice(petr4, 39.10)
	w.src.setPrice(aapl, 231.50)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Given established
	if line := rowLine(t, f, "XXXX9"); !strings.Contains(line, "not found") {
		t.Fatalf("given not established: XXXX9 is not shown as not found: %q", line)
	}

	// Then
	for tk, price := range map[string]string{"PETR4": "R$ 39,10", "AAPL": "US$ 231.50"} {
		line := rowLine(t, f, tk)
		if !strings.Contains(line, price) {
			t.Errorf("row %s does not show its fresh quote %s: %q", tk, price, line)
		}
		if viewStaleMark.MatchString(line) {
			t.Errorf("row %s is marked stale: %q", tk, line)
		}
	}
}

// S8.5 Given a not-found row and no other problem, when the view is drawn,
// then there is no error line. (D50)
func TestS8_5_View_NotFoundRaisesNoErrorLine(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, notFoundConfig)
	loadQuotes(w.src, w.now, true)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Given established
	if line := rowLine(t, f, "XXXX9"); !strings.Contains(line, "not found") {
		t.Fatalf("given not established: XXXX9 is not shown as not found: %q", line)
	}

	// Then
	if z := errorZone(t, f, "PETR4", "XXXX9"); len(z) != 0 {
		t.Errorf("lines between the rows and the status line, want none: %q\n%s", z, f)
	}
	for _, p := range append(causePhrases, "not updated") {
		if strings.Contains(f, p) {
			t.Errorf("the view shows %q with only a not-found row:\n%s", p, f)
		}
	}
}
