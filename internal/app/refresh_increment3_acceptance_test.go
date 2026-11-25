package app_test

// Acceptance tests of slice 3, increment 3: the `r` key pressed while the
// view is open. Written from docs/spec.md and the view's test boundary
// (docs/design.md, "Time"). Each test establishes its given while the view
// is open, through Elapse and the stand-in's answers, then sends `r` and
// counts requests from the stand-in's own record. "Within 2 seconds" and
// "before one refresh interval" are limits on view time after the key: the
// tests look at the requests made by then, with time moved only by Elapse.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lucattelli/brl-stocks/internal/deps"
	"github.com/lucattelli/brl-stocks/internal/market"
	"github.com/lucattelli/brl-stocks/internal/provider"
)

// ---------------------------------------------------------------------------
// Helpers of this increment
// ---------------------------------------------------------------------------

// press sends key and returns the clock's time when it was sent and the idle
// frame after it.
func (r *clockRun) press(key string) (time.Time, drawn) {
	r.t.Helper()
	at := r.clock.Now()
	r.put(deps.Input{Key: key})
	return at, r.settled(viewGuard)
}

// requestedFrom counts the requests for in at times in [from, to].
func (s *clockSource) requestedFrom(in market.Instrument, from, to time.Time) int {
	return s.requestedIn(in, from.Add(-time.Nanosecond), to)
}

// totalFrom counts the requests for every instrument at times in [from, to].
func (s *clockSource) totalFrom(from, to time.Time) int {
	s.mu.Lock()
	var ins []market.Instrument
	for in := range s.at {
		ins = append(ins, in)
	}
	s.mu.Unlock()
	n := 0
	for _, in := range ins {
		n += s.requestedFrom(in, from, to)
	}
	return n
}

// twentyB3Only is 20 B3 tickers: the ten of the 20-ticker watchlist and ten
// more.
var twentyB3Only = append(append([]string{}, twentyB3...),
	"GGBR4", "CSNA3", "USIM5", "EMBR3", "LREN3", "MGLU3", "RADL3", "HAPV3", "PRIO3", "ELET3")

// ---------------------------------------------------------------------------
// S7 - Refresh now, and a source that is blocking
// ---------------------------------------------------------------------------

// S7.1 Given B3 is open and the source answers, when `r` is pressed, then
// the first B3 rows are requested within 2 seconds, without waiting for the
// interval. (D32, D62)
func TestS7_1_View_RRequestsB3RowsWithinTwoSeconds(t *testing.T) {
	for _, c := range []struct {
		name, config string
		load         func(src *fakeSource, now time.Time)
		b3           []string
	}{
		{"criterion", plainConfig, func(src *fakeSource, now time.Time) { loadQuotes(src, now, true) }, []string{"PETR4", "VALE3"}},
		{"20 tickers", twentyConfig(), func(src *fakeSource, now time.Time) { loadTwenty(src, now) }, twentyB3},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Given: Monday 15:00 UTC, B3 open; the fetch at start answered;
			// 30 seconds on, the next automatic refresh is 30 seconds away
			w := clockWorld(t, bothOpen, c.config)
			c.load(w.src, w.now)
			v := openOnClock(t, w)
			v.settled(viewGuard)
			v.elapse(30 * time.Second)

			// When
			at, _ := v.press("r")
			v.elapse(2 * time.Second)

			// Then
			n := 0
			for _, in := range instruments(c.b3...) {
				n += v.src.requestedFrom(in, at, at.Add(2*time.Second))
			}
			if n == 0 {
				t.Errorf("no B3 row was requested within 2 seconds of r (pressed at %v; PETR4 requested at %v)", at.UTC(), v.src.times(petr4))
			}
		})
	}
}

// S7.2 Given a network error has put the next automatic attempt four
// minutes away, when `r` is pressed, then the first failed rows are
// requested within 2 seconds. (D38, D62)
func TestS7_2_View_RAfterANetworkError(t *testing.T) {
	// Given: PETR4 fetched at start, then every fetch of it fails: after the
	// third failure in a row the next attempt is 240 seconds away (6.2)
	w := clockWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	v, t0, _ := failedOnHeartbeat(t, w, []market.Instrument{petr4}, func(src *fakeSource) { src.fail(petr4, provider.Unavailable) })
	v.elapse(3*time.Minute + time.Second)
	var failed []time.Time
	for _, at := range v.src.times(petr4) {
		if at.After(t0) {
			failed = append(failed, at)
		}
	}
	if len(failed) != 3 {
		t.Fatalf("given not established: %d failed attempts for PETR4 (at %v), want 3, after which the next is 240 seconds away", len(failed), failed)
	}
	if wait := failed[2].Add(240 * time.Second).Sub(v.clock.Now()); wait < 3*time.Minute+30*time.Second {
		t.Fatalf("given not established: the next automatic attempt is %v away, not about four minutes", wait)
	}

	// When
	at, _ := v.press("r")
	v.elapse(2 * time.Second)

	// Then
	if n := v.src.requestedFrom(petr4, at, at.Add(2*time.Second)); n == 0 {
		t.Errorf("failed row PETR4 was not requested within 2 seconds of r (pressed at %v; requested at %v)", at.UTC(), v.src.times(petr4))
	}
}

// blockedOnHeartbeat opens the view with a source that answers, then makes it
// refuse with kind, so the heartbeat refresh starts a block; it returns the
// view and the time of the first refusal.
func blockedOnHeartbeat(t *testing.T, kind provider.Kind) (*clockRun, time.Time) {
	t.Helper()
	w := clockWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	v, t0, f := failedOnHeartbeat(t, w, nil, func(src *fakeSource) { src.failAll(kind) })
	first := attempts(v, t0.Add(time.Nanosecond))
	if len(first) == 0 {
		t.Fatalf("given not established: no request was refused in the 61 seconds after the fetch at start")
	}
	if line := theErrorLine(t, f.text(), "PETR4", "VALE3", "AAPL"); !strings.Contains(line, refusals[kind]) {
		t.Fatalf("given not established: the error line does not show the refusal %q: %q", refusals[kind], line)
	}
	v.elapse(30 * time.Second)
	return v, first[0]
}

// S7.3 Given the source is rate-limiting or refusing the tool, when `r` is
// pressed, then no request is sent. (D38)
func TestS7_3_View_RDuringABlockSendsNothing(t *testing.T) {
	for kind := range refusals {
		t.Run(string(kind), func(t *testing.T) {
			// Given: a block that started 30 seconds ago; its wait is 5 minutes
			v, _ := blockedOnHeartbeat(t, kind)

			// When: r, and a minute more, well before the wait ends
			at, _ := v.press("r")
			v.elapse(time.Minute)

			// Then
			if n := v.src.totalFrom(at, at.Add(time.Minute)); n != 0 {
				t.Errorf("%d requests were sent after r during a block (PETR4 at %v)", n, v.src.times(petr4))
			}
		})
	}
}

// S7.4 Given the source is rate-limiting or refusing the tool, when `r` is
// pressed, then the status line shows the time of the next attempt.
// (D38, D76, D79)
func TestS7_4_View_RDuringABlockShowsTheNextAttempt(t *testing.T) {
	for kind := range refusals {
		t.Run(string(kind), func(t *testing.T) {
			// Given: the first wait of a block is 5 minutes (6.3)
			v, refused := blockedOnHeartbeat(t, kind)
			next := hhmm(refused.Add(5 * time.Minute))

			// When
			_, f := v.press("r")

			// Then
			if _, st := statusLine(t, f.text()); !strings.Contains(st, next) {
				t.Errorf("after r during a block, the status line does not show the time of the next attempt, %s: %q", next, st)
			}
		})
	}
}

// S7.6 Given a closed market, when `r` is pressed, then its rows are
// requested too. (D50)
func TestS7_6_View_RRequestsClosedMarketRows(t *testing.T) {
	// Given: Monday 13:30 UTC, the US market closed since Friday; the fetch
	// at start gave the US rows Friday's close; a minute on, they have not
	// been requested again
	w := clockWorld(t, onlyB3Open, `tickers = ["PETR4", "AAPL", "MSFT"]`+"\n")
	loadQuotes(w.src, w.now, false)
	v := openOnClock(t, w)
	f := v.settled(viewGuard)
	if st := marketStates(t, f); !strings.Contains(st, "US closed") {
		t.Fatalf("given not established: the status line does not show the US market closed: %q", st)
	}
	v.elapse(time.Minute)
	for _, in := range []market.Instrument{aapl, msft} {
		if n := v.src.requestedIn(in, onlyB3Open, v.clock.Now()); n != 0 {
			t.Fatalf("given not established: closed-market row %s was requested %d times by the refreshes after the fetch at start", in.Symbol, n)
		}
	}

	// When
	at, _ := v.press("r")
	v.elapse(2 * time.Second)

	// Then
	for _, in := range []market.Instrument{aapl, msft} {
		if n := v.src.requestedFrom(in, at, at.Add(2*time.Second)); n == 0 {
			t.Errorf("closed-market row %s was not requested after r (pressed at %v; requested at %v)", in.Symbol, at.UTC(), v.src.times(in))
		}
	}
}

// S7.7 Given `r` was pressed two seconds ago, when `r` is pressed again,
// then no row is requested a second time. (D50)
func TestS7_7_View_RTwiceInARow(t *testing.T) {
	for _, c := range []struct {
		name, config string
		load         func(src *fakeSource, now time.Time)
		rows         []string
	}{
		{"criterion", plainConfig, func(src *fakeSource, now time.Time) { loadQuotes(src, now, true) }, []string{"PETR4", "VALE3", "AAPL"}},
		{"20 tickers", twentyConfig(), func(src *fakeSource, now time.Time) { loadTwenty(src, now) }, twentyTickers()},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Given: r pressed 20 seconds after the fetch at start, and its
			// refresh requested the rows; two seconds pass
			w := clockWorld(t, bothOpen, c.config)
			c.load(w.src, w.now)
			v := openOnClock(t, w)
			v.settled(viewGuard)
			v.elapse(20 * time.Second)
			first, _ := v.press("r")
			v.elapse(2 * time.Second)
			for _, in := range instruments(c.rows...) {
				if v.src.requestedFrom(in, first, first.Add(2*time.Second)) == 0 {
					t.Fatalf("given not established: the first r did not request %s within 2 seconds", in.Symbol)
				}
			}

			// When
			second, _ := v.press("r")
			v.elapse(2 * time.Second)

			// Then
			for _, in := range instruments(c.rows...) {
				if n := v.src.requestedFrom(in, first, second.Add(2*time.Second)); n != 1 {
					t.Errorf("row %s was requested %d times by two presses of r two seconds apart, want once (at %v)", in.Symbol, n, v.src.times(in))
				}
			}
		})
	}
}

// S7.8 Given a not-found row tried ten minutes ago and an unchanged config,
// when `r` is pressed, then that ticker is not requested. (D39)
func TestS7_8_View_RLeavesANotFoundRow(t *testing.T) {
	// Given: XXXX9 not found by the fetch at start; ten minutes pass
	w := clockWorld(t, bothOpen, notFoundConfig)
	loadQuotes(w.src, w.now, true)
	v := openOnClock(t, w)
	v.settled(viewGuard)
	f := v.elapse(10 * time.Minute)
	if line := rowLine(t, f.text(), "XXXX9"); !strings.Contains(line, "not found") {
		t.Fatalf("given not established: XXXX9 is not shown as not found: %q", line)
	}
	if at := v.src.times(xxxx9); len(at) != 1 || !at[0].Equal(bothOpen) {
		t.Fatalf("given not established: XXXX9 was not tried exactly once, ten minutes ago (at %v)", at)
	}

	// When
	at, _ := v.press("r")
	v.elapse(time.Minute)

	// Given established: the refresh of r ran
	if v.src.requestedFrom(petr4, at, at.Add(2*time.Second)) == 0 {
		t.Fatalf("given not established: r did not refresh PETR4 within 2 seconds (at %v)", v.src.times(petr4))
	}

	// Then
	if n := v.src.requestedFrom(xxxx9, at, at.Add(time.Minute)); n != 0 {
		t.Errorf("not-found XXXX9, tried ten minutes ago, was requested %d times after r", n)
	}
}

// S7.9 Given B3 is open, 20 B3 rows and a source that answers within one
// second, when `r` is pressed, then every B3 row is requested before one
// refresh interval has passed. (D32, D62)
func TestS7_9_View_RReachesTwentyB3Rows(t *testing.T) {
	// Given: 20 B3 rows and a source that answers at once; refresh_seconds =
	// 600, so the automatic refresh after the fetch at start comes at 600
	// seconds; r is pressed at 10 seconds
	config := "refresh_seconds = 600\n" + `tickers = ["` + strings.Join(twentyB3Only, `", "`) + `"]` + "\n"
	w := clockWorld(t, bothOpen, config)
	loadQuotes(w.src, w.now, true)
	for i, s := range twentyB3Only {
		in := market.Instrument{Market: market.B3, Symbol: s}
		if _, ok := w.src.quotes[in]; ok {
			continue
		}
		p := float64(20+i) + 0.25
		w.src.answer(in, quote(fmt.Sprintf("%s SA", s), "BRL", p, p, b3Session, w.now.Add(-15*time.Minute)))
	}
	v := openOnClock(t, w)
	v.settled(viewGuard)
	v.elapse(10 * time.Second)

	// When
	at, _ := v.press("r")
	automatic := bothOpen.Add(600 * time.Second)
	v.elapse(automatic.Add(-time.Second).Sub(v.clock.Now()))

	// Then: every row requested after r and before the automatic refresh
	// could have requested it (stricter than the interval by 10 seconds)
	var missing []string
	for _, s := range twentyB3Only {
		in := market.Instrument{Market: market.B3, Symbol: s}
		if v.src.requestedFrom(in, at, automatic.Add(-time.Second)) == 0 {
			missing = append(missing, s)
		}
	}
	if len(missing) != 0 {
		t.Errorf("%d of 20 B3 rows were not requested after r before one interval had passed: %v", len(missing), missing)
	}
}
