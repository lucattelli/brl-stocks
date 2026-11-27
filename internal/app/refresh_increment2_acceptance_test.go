package app_test

// Acceptance tests of slice 3, increment 2: the full-screen view left open
// while the source fails, refuses and recovers. Written from docs/spec.md and
// the view's test boundary (docs/design.md, "Time") before the behaviour
// exists. They reuse the clock helpers of refresh_increment1_acceptance_test.go:
// time passes only through Input{Elapse: d} on the clock the view and the
// stand-in share, and the stand-in answers or fails at once.

import (
	"regexp"
	"sort"
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

// failedOnHeartbeat opens the view at the world's time with a source that
// answers, so the fetch at start succeeds; then makes the source fail as fail
// says and lets one interval and its heartbeat pass (61 seconds), so the
// heartbeat refresh fails. It returns the view, the time it opened and the
// idle frame after the refresh. Each of rows must have been requested by that
// refresh.
func failedOnHeartbeat(t *testing.T, w *world, rows []market.Instrument, fail func(src *fakeSource)) (*clockRun, time.Time, drawn) {
	t.Helper()
	t0 := w.now
	v := openOnClock(t, w)
	v.settled(viewGuard)
	fail(w.src)
	f := v.elapse(61 * time.Second)
	for _, in := range rows {
		if v.src.requestedIn(in, t0, t0.Add(61*time.Second)) == 0 {
			t.Fatalf("given not established: no heartbeat refresh requested %s in the 61 seconds after the fetch at start (at %v)", in.Symbol, v.src.times(in))
		}
	}
	return v, t0, f
}

// attempts groups every request the stand-in received at or after from, for
// every instrument, into attempts: requests less than a second apart are one
// attempt. It returns the time each attempt started.
func attempts(v *clockRun, from time.Time) []time.Time {
	v.src.mu.Lock()
	var all []time.Time
	for _, ts := range v.src.at {
		for _, at := range ts {
			if !at.Before(from) {
				all = append(all, at)
			}
		}
	}
	v.src.mu.Unlock()
	sort.Slice(all, func(i, j int) bool { return all[i].Before(all[j]) })
	var starts []time.Time
	for _, at := range all {
		if len(starts) == 0 || at.Sub(starts[len(starts)-1]) >= time.Second {
			starts = append(starts, at)
		}
	}
	return starts
}

// gapsOf lists the time between consecutive times.
func gapsOf(ts []time.Time) []time.Duration {
	var g []time.Duration
	for i := 1; i < len(ts); i++ {
		g = append(g, ts[i].Sub(ts[i-1]))
	}
	return g
}

// assertGaps checks the gaps between attempts against want, in order. The
// view looks at what is due on its heartbeat, once a second of the clock
// (design, "Time"), so a gap may be up to one second longer than its wait.
func assertGaps(t *testing.T, what string, at []time.Time, want []time.Duration) {
	t.Helper()
	got := gapsOf(at)
	if len(got) < len(want) {
		t.Fatalf("%s: %d attempts after the first, want at least %d; gaps %v (attempts at %v)", what, len(got), len(want), got, at)
	}
	for i, w := range want {
		if got[i] < w || got[i] > w+time.Second {
			t.Errorf("%s: gap %d is %v, want %v (gaps %v)", what, i+1, got[i], w, got[:len(want)])
		}
	}
}

// ---------------------------------------------------------------------------
// S6 - When a refresh fails
// ---------------------------------------------------------------------------

// S6.4 Given a row that keeps failing with the default interval, when the
// view stays open, then the automatic attempts for it come 60, 120, 240, 480,
// 960 and then every 1,800 seconds apart. (D19, D50)
func TestS6_4_View_WaitsAfterFailuresGrow(t *testing.T) {
	// Given: Monday 15:00 UTC, both markets open; PETR4 is fetched at start,
	// and from then on every fetch of it fails; the other rows answer
	w := clockWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	v, t0, _ := failedOnHeartbeat(t, w, []market.Instrument{petr4}, func(src *fakeSource) { src.fail(petr4, provider.Unavailable) })

	// When: the view stays open for two hours more (the waits add up to
	// 7,260 seconds after the first failure)
	v.elapse(2*time.Hour + 5*time.Minute)

	// Then: the attempts from the first failed one
	var failed []time.Time
	for _, at := range v.src.times(petr4) {
		if at.After(t0) {
			failed = append(failed, at)
		}
	}
	assertGaps(t, "automatic attempts for PETR4", failed, []time.Duration{
		60 * time.Second, 120 * time.Second, 240 * time.Second, 480 * time.Second, 960 * time.Second,
		1800 * time.Second, 1800 * time.Second, 1800 * time.Second,
	})
}

// staleThenAnswering opens a view whose heartbeat refresh fails for every row
// and entry, checks that rows are stale and that the error line is shown,
// then lets the source answer again and lets the next attempt pass (the
// first wait after a failure is one interval). It returns the frame after
// that attempt.
func staleThenAnswering(t *testing.T, c failureCase) string {
	t.Helper()
	w := clockWorld(t, bothOpen, c.config)
	c.prices(w)
	rows := instruments(c.tickers...)
	v, _, f := failedOnHeartbeat(t, w, rows, func(src *fakeSource) { src.failAll(provider.Unavailable) })
	failedAt := v.clock.Now()
	for _, tk := range c.tickers {
		if line := rowLine(t, f.text(), tk); !viewStaleMark.MatchString(line) {
			t.Fatalf("given not established: row %s is not stale after the failed refresh: %q", tk, line)
		}
	}
	if line := theErrorLine(t, f.text(), c.tickers...); !strings.Contains(line, "source unreachable") {
		t.Fatalf("given not established: no error line for the failed refresh: %q", line)
	}

	// When: the source answers again; the next attempt (one interval after
	// the failure) succeeds for every row
	w.src.failAll("")
	f = v.elapse(2 * time.Minute)
	for _, in := range rows {
		if v.src.requestedIn(in, failedAt, v.clock.Now()) == 0 {
			t.Fatalf("given not established: row %s was not requested again after the source recovered (at %v)", in.Symbol, v.src.times(in))
		}
	}
	return f.text()
}

// S6.5 Given stale rows and an error line, when a refresh succeeds for every
// row, then the stale marks are gone. (D19, D40)
func TestS6_5_View_RecoveryClearsStaleMarks(t *testing.T) {
	for _, c := range failureCases() {
		t.Run(c.name, func(t *testing.T) {
			f := staleThenAnswering(t, c)
			for _, tk := range c.tickers {
				if line := rowLine(t, f, tk); viewStaleMark.MatchString(line) {
					t.Errorf("row %s is still marked stale after a refresh succeeded for every row: %q", tk, line)
				}
			}
		})
	}
}

// S6.6 Given stale rows and an error line, when a refresh succeeds for every
// row, then the error line is gone. (D19)
func TestS6_6_View_RecoveryClearsTheErrorLine(t *testing.T) {
	for _, c := range failureCases() {
		t.Run(c.name, func(t *testing.T) {
			f := staleThenAnswering(t, c)
			if z := errorZone(t, f, c.tickers...); len(z) != 0 {
				t.Errorf("after a refresh succeeded for every row, lines remain between the rows and the status line: %q\n%s", z, f)
			}
			for _, l := range lines(f) {
				for _, p := range causePhrases {
					if strings.Contains(l, p) {
						t.Errorf("a line still names the cause %q: %q", p, l)
					}
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// S7 - A source that is blocking
// ---------------------------------------------------------------------------

// S7.5 Given the source refuses a request, when the view stays open and the
// source keeps refusing, then the next attempts come 5, 10, 20 and then every
// 30 minutes apart. (D50)
func TestS7_5_View_BlockWaitsGrow(t *testing.T) {
	for kind := range refusals {
		t.Run(string(kind), func(t *testing.T) {
			// Given: the fetch at start succeeds; from then on the source
			// refuses every request
			w := clockWorld(t, bothOpen, plainConfig)
			loadQuotes(w.src, w.now, true)
			v, t0, _ := failedOnHeartbeat(t, w, nil, func(src *fakeSource) { src.failAll(kind) })
			first := attempts(v, t0.Add(time.Nanosecond))
			if len(first) == 0 {
				t.Fatalf("given not established: no request was refused in the 61 seconds after the fetch at start")
			}

			// When: the waits add up to 5,700 seconds after the first refusal
			v.elapse(2*time.Hour + 5*time.Minute)

			// Then
			assertGaps(t, "attempts during the block", attempts(v, first[0]), []time.Duration{
				5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 30 * time.Minute, 30 * time.Minute,
			})
		})
	}
}

// S7.10 Given the source refused a request and the view stays open, when the
// wait ends and the source answers, then the rows are requested again
// without a key press. (D19, D50)
func TestS7_10_View_BlockEndsRowsRequestedAgain(t *testing.T) {
	for _, c := range failureCases() {
		t.Run(c.name, func(t *testing.T) {
			// Given: the fetch at start succeeds; the heartbeat refresh is
			// refused (rate limit), which starts a wait of 5 minutes
			w := clockWorld(t, bothOpen, c.config)
			c.prices(w)
			rows := instruments(c.tickers...)
			v, t0, f := failedOnHeartbeat(t, w, nil, func(src *fakeSource) { src.failAll(provider.RateLimited) })
			first := attempts(v, t0.Add(time.Nanosecond))
			if len(first) == 0 {
				t.Fatalf("given not established: no request was refused in the 61 seconds after the fetch at start")
			}
			if line := theErrorLine(t, f.text(), c.tickers...); !strings.Contains(line, "source is rate-limiting") {
				t.Fatalf("given not established: the error line does not show the refusal: %q", line)
			}

			// When: the source answers again, and the view stays open, with
			// no key, past the end of the wait and one interval more
			w.src.failAll("")
			end := first[0].Add(5*time.Minute + 61*time.Second)
			v.elapse(end.Sub(v.clock.Now()))

			// Then
			for _, in := range rows {
				if v.src.requestedIn(in, first[0].Add(5*time.Minute-time.Nanosecond), end) == 0 {
					t.Errorf("row %s was not requested again after the wait ended (refused at %v; requested at %v)", in.Symbol, first[0].UTC(), v.src.times(in))
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Criteria of slice 2 shown again on a heartbeat refresh (scope, increment 2)
// ---------------------------------------------------------------------------

// S6.1 Given rows with quotes and an open market, when a refresh fails, then
// the rows still show their last known prices. (D19)
func TestS6_1_ViewOpen_FailedHeartbeatRefreshKeepsLastKnownPrices(t *testing.T) {
	for _, c := range failureCases() {
		t.Run(c.name, func(t *testing.T) {
			w := clockWorld(t, bothOpen, c.config)
			prices := c.prices(w)
			_, _, f := failedOnHeartbeat(t, w, instruments(c.tickers...), func(src *fakeSource) { src.failAll(provider.Unavailable) })
			for tk, price := range prices {
				assertRowHas(t, f.text(), tk, price)
			}
		})
	}
}

// S6.2 Given a row whose last fetch failed, when the view is drawn, then the
// row is marked stale with its age. (D19, D40)
func TestS6_2_ViewOpen_RowFailingOnHeartbeatMarkedStaleWithItsAge(t *testing.T) {
	// Given: PETR4 fetched at start; every later fetch of it fails
	w := clockWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	v, t0, _ := failedOnHeartbeat(t, w, []market.Instrument{petr4}, func(src *fakeSource) { src.fail(petr4, provider.Unavailable) })

	// When: 5 minutes after its last successful fetch
	f := v.elapse(t0.Add(5 * time.Minute).Sub(v.clock.Now()))

	// Then
	if line := rowLine(t, f.text(), "PETR4"); !strings.Contains(line, "!5m") {
		t.Errorf("row PETR4, last fetched with success 5 minutes ago, does not carry the stale mark !5m: %q", line)
	}
}

// S6.3 Given a refresh in which five rows failed, when the view is drawn,
// then exactly one error line is shown. (D19)
func TestS6_3_ViewOpen_FiveRowsFailOnHeartbeatOneErrorLine(t *testing.T) {
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
			w := clockWorld(t, bothOpen, c.config)
			c.load(w)
			_, _, f := failedOnHeartbeat(t, w, five, func(src *fakeSource) {
				for i, in := range five {
					kind := provider.Unavailable
					if i%2 == 1 {
						kind = provider.BadResponse
					}
					src.fail(in, kind)
				}
			})
			if z := errorZone(t, f.text(), c.tickers...); len(z) != 1 {
				t.Errorf("%d lines between the rows and the status line, want exactly one error line: %q\n%s", len(z), z, f.text())
			}
			n := 0
			for _, l := range lines(f.text()) {
				for _, p := range causePhrases {
					if strings.Contains(l, p) {
						n++
						break
					}
				}
			}
			if n != 1 {
				t.Errorf("%d lines name a cause, want exactly one:\n%s", n, f.text())
			}
		})
	}
}

// S6.8 Given a refresh in which one row fails and the others succeed, when
// the view is drawn, then only that row is marked stale. (D40)
func TestS6_8_ViewOpen_OnlyTheRowFailingOnHeartbeatIsStale(t *testing.T) {
	for _, c := range failureCases() {
		t.Run(c.name, func(t *testing.T) {
			w := clockWorld(t, bothOpen, c.config)
			c.prices(w)
			_, _, f := failedOnHeartbeat(t, w, instruments(c.tickers...), func(src *fakeSource) { src.fail(vale3, provider.Unavailable) })
			for _, tk := range c.tickers {
				line := rowLine(t, f.text(), tk)
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
func TestS6_9_ViewOpen_ErrorLineOfAFailedHeartbeatRefresh(t *testing.T) {
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
			// Given: every row and entry fails on the heartbeat refresh (not
			// a refusal); the first wait after a failure is one interval
			w := clockWorld(t, bothOpen, c.config)
			c.load(w)
			v, t0, f := failedOnHeartbeat(t, w, instruments(c.tickers...), func(src *fakeSource) { src.failAll(c.kind) })
			at := v.src.times(petr4)
			failedAt := at[len(at)-1]
			if !failedAt.After(t0) {
				t.Fatalf("given not established: PETR4's last request (%v) is not the heartbeat refresh", failedAt)
			}
			next := hhmm(failedAt.Add(60 * time.Second))

			// Then
			line := theErrorLine(t, f.text(), c.tickers...)
			if !strings.HasPrefix(strings.TrimSpace(line), c.cause) {
				t.Errorf("the error line does not name the cause %q: %q", c.cause, line)
			}
			if !regexp.MustCompile(`\b` + strconv.Itoa(len(c.tickers)) + `\b`).MatchString(line) {
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
func TestS6_10_ViewOpen_StaleMarkReadsTwelveMinutes(t *testing.T) {
	w := clockWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	v, t0, _ := failedOnHeartbeat(t, w, []market.Instrument{petr4}, func(src *fakeSource) { src.fail(petr4, provider.Unavailable) })
	f := v.elapse(t0.Add(12 * time.Minute).Sub(v.clock.Now()))
	if line := rowLine(t, f.text(), "PETR4"); !strings.Contains(line, "!12m") {
		t.Errorf("row PETR4, last fetched with success 12 minutes ago, does not read !12m: %q", line)
	}
}

// S6.11 Given every row updates and a summary entry fails, when the view is
// drawn, then the error line says the summary was not updated. (D81)
func TestS6_11_ViewOpen_OnlySummaryFailsOnHeartbeat(t *testing.T) {
	w := clockWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	_, _, f := failedOnHeartbeat(t, w, append([]market.Instrument{ibov}, plainRows...), func(src *fakeSource) { src.fail(ibov, provider.Unavailable) })
	for tk, price := range map[string]string{"PETR4": "R$ 38,42", "VALE3": "R$ 61,07", "AAPL": "US$ 229.87"} {
		if line := rowLine(t, f.text(), tk); !strings.Contains(line, price) || viewStaleMark.MatchString(line) {
			t.Fatalf("given not established: row %s did not update: %q", tk, line)
		}
	}
	if line := theErrorLine(t, f.text(), "PETR4", "VALE3", "AAPL"); !strings.Contains(line, "summary not updated") {
		t.Errorf("the error line does not say `summary not updated`: %q", line)
	}
}

// ---------------------------------------------------------------------------
// S8 - A ticker the source does not know
// ---------------------------------------------------------------------------

// S8.2 Given one not-found row among others, when a refresh runs, then the
// other rows show fresh quotes. (D39)
func TestS8_2_ViewOpen_NotFoundRetriedOnHeartbeatOthersFresh(t *testing.T) {
	// Given: XXXX9 not found at start; an hour on, a heartbeat refresh tries
	// it again, and the source has new prices for the others
	w := clockWorld(t, bothOpen, `tickers = ["PETR4", "XXXX9", "AAPL"]`+"\n")
	loadQuotes(w.src, w.now, true)
	t0 := w.now
	v := openOnClock(t, w)
	v.settled(viewGuard)
	w.src.setPrice(petr4, 39.10)
	w.src.setPrice(aapl, 231.50)

	// When
	f := v.elapse(61 * time.Minute)
	if v.src.requestedIn(xxxx9, t0, v.clock.Now()) == 0 {
		t.Fatalf("given not established: no heartbeat refresh tried XXXX9 again within 61 minutes (at %v)", v.src.times(xxxx9))
	}
	if line := rowLine(t, f.text(), "XXXX9"); !strings.Contains(line, "not found") {
		t.Fatalf("given not established: XXXX9 is not shown as not found: %q", line)
	}

	// Then
	for tk, price := range map[string]string{"PETR4": "R$ 39,10", "AAPL": "US$ 231.50"} {
		line := rowLine(t, f.text(), tk)
		if !strings.Contains(line, price) {
			t.Errorf("row %s does not show its fresh quote %s: %q", tk, price, line)
		}
		if viewStaleMark.MatchString(line) {
			t.Errorf("row %s is marked stale: %q", tk, line)
		}
	}
}
