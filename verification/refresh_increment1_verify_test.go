//go:build verification && linux

// Live refresh, increment 1 of slice 3 (research/build/live-refresh/scope.md):
// the real view left open while real time passes, with a healthy stand-in
// source. S5.1, S5.2, S5.3, S5.7, S5.8, S5.10, S8.3 and S15.3, and QR6, QR11
// and QR8 with the view. Times are scaled down where the criterion's own times
// cannot pass in the ordinary run: the README's feature map says where. The
// checks at the criteria's own lengths are behind BRL_VERIFY_SLOW (10 minutes)
// and BRL_VERIFY_LONG (3 and 8 hours).
//
// Added to the stand-in for these checks, without changing the existing
// controls: `opens_at_unix` (a market's next session starts at a given time)
// and `quote_before_close_seconds` (a quote time before the close).
package verification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// state3 is the stand-in's behaviour with the controls used in this file.
type state3 struct {
	state
	VaryQuotes              bool             `json:"vary_quotes,omitempty"`
	OpensAtUnix             map[string]int64 `json:"opens_at_unix,omitempty"`
	QuoteBeforeCloseSeconds int64            `json:"quote_before_close_seconds,omitempty"`
}

func (e *env) setSource3(st state3, what string) {
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

// hold keeps the view open until the given time, calling fn (when not nil)
// every period with the screen. It fails the check if the tool exits.
func (v *view) hold(until time.Time, period time.Duration, fn func(i int, s screen)) {
	v.e.t.Helper()
	v.e.logf("HOLD       the view stays open until +%s\n", until.Sub(v.start).Round(time.Millisecond))
	next := time.Now().Add(period)
	i := 0
	for time.Now().Before(until) {
		wait := time.Until(next)
		if u := time.Until(until); u < wait {
			wait = u
		}
		if wait < 0 {
			wait = 0
		}
		select {
		case <-v.done:
			v.e.failf("the tool exited at +%s while the view was to stay open", time.Since(v.start).Round(time.Millisecond))
			return
		case <-time.After(wait):
		}
		if fn != nil && !time.Now().Before(next) {
			i++
			fn(i, v.snap())
			next = next.Add(period)
		}
	}
}

// arrivals returns the arrival times of the requests for sym in [from, to).
// The stand-in records arrivals in whole milliseconds (unix_ms, rounded
// down), so from and to are rounded down to the millisecond as well: a
// request in the same millisecond as from, after it, counts.
func arrivals(recs []record, sym string, from, to time.Time) []time.Time {
	from, to = time.UnixMilli(from.UnixMilli()), time.UnixMilli(to.UnixMilli())
	var out []time.Time
	for _, r := range recs {
		at := time.UnixMilli(r.UnixMS)
		if r.Symbol == sym && !at.Before(from) && at.Before(to) {
			out = append(out, at)
		}
	}
	return out
}

func fmtArrivals(ts []time.Time, base time.Time) string {
	if len(ts) == 0 {
		return "(none)"
	}
	var p []string
	for _, t := range ts {
		p = append(p, "+"+t.Sub(base).Round(10*time.Millisecond).String())
	}
	return strings.Join(p, " ")
}

// minGap is the shortest time between two consecutive arrivals.
func minGap(ts []time.Time) time.Duration {
	g := time.Duration(-1)
	for i := 1; i < len(ts); i++ {
		if d := ts[i].Sub(ts[i-1]); g < 0 || d < g {
			g = d
		}
	}
	return g
}

// openRows opens the view and waits until every ticker shows a price.
func (e *env) openRows(tks []string, cond func(s screen) bool) *view {
	e.t.Helper()
	v := e.openView(nil)
	want := rowsShown(tks)
	if cond != nil {
		want = func(s screen) bool { return rowsShown(tks)(s) && cond(s) }
	}
	s, _ := v.waitStable("every row with a price", 15*time.Second, 300*time.Millisecond, want)
	v.logScreen("SCREEN AFTER OPENING", s, nil)
	return v
}

// ---------------------------------------------------------------------------
// S5.1, S5.2: the refresh interval while the view stays open
// ---------------------------------------------------------------------------

// heartbeat opens the view on an empty cache with an open market, keeps it
// open for window, and returns the requests of the run and the view's start.
func heartbeat(e *env, tks []string, window time.Duration) ([]record, time.Time, screen) {
	e.t.Helper()
	v := e.openRows(tks, nil)
	var last screen
	v.hold(v.start.Add(window+500*time.Millisecond), 10*time.Second, func(i int, s screen) {
		last = s
		e.logf("SAMPLE %d at +%s  status line %q\n", i, s.at.Sub(v.start).Round(time.Second), statusLine(s))
	})
	last = v.snap()
	v.logScreen("SCREEN AT THE END OF THE WINDOW", last, nil)
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)
	return r.reqs, v.start, last
}

func wantCounts(c *ck, recs []record, syms []string, from time.Time, window time.Duration, lo, hi int) {
	c.t.Helper()
	for _, sym := range syms {
		ts := arrivals(recs, sym, from, from.Add(window))
		c.notef("%s: %d requests in %s: %s", sym, len(ts), window, fmtArrivals(ts, from))
		if len(ts) < lo || len(ts) > hi {
			c.failf("%s: requested %d times in %s, want between %d and %d", sym, len(ts), window, lo, hi)
		}
	}
}

// S5.1 [view] Given B3 is open and no `refresh_seconds` is set, when the view
// stays open for 10 minutes, then each B3 row is requested from the source
// between 9 and 11 times.
// Scaled: `refresh_seconds = 10` for 100 s (ten intervals, as 10 minutes are
// ten default intervals). The literal check is TestVerifySlow_View_S5_1.
// Also in this run, shown again while the view is open:
// S4.4 [view, print] Given the same ticker in two groups, when a refresh runs,
// then the ticker is requested from the source once (PETR4 in two groups).
// S8.3 [view] (partial) Given a not-found row and an unchanged config, when the
// view stays open for three hours, then the ticker is requested at least twice
// and at most four times: in 100 s, XXXX9 must not be asked for on every
// interval. The full three hours: TestVerifyLong_View_S8_3.
func TestVerifyView_S5_1_S4_4_S8_3_Heartbeat(t *testing.T) {
	t.Parallel()
	cfg := cfgSpec{interval: 10, top: []string{"PETR4", "XXXX9"},
		groups: []groupSpec{{"Brasil", []string{"VALE3", "PETR4"}}, {"US", []string{"AAPL", "PETR4"}}}}
	e := newEnv(t, "S5.1 scaled, S4.4 and S8.3 (partial) with the view open", cfg.String())
	e.setSource(state{SymbolStatus: map[string]int{"XXXX9.SA": 404}}, "XXXX9.SA answered 404")
	window := 100 * time.Second
	recs, start, last := heartbeat(e, []string{"VALE3", "AAPL"}, window)
	crit(e, "S5.1", "each B3 row is requested 9 to 11 times in ten refresh intervals (scaled)", func(c *ck) {
		if st := statusLine(last); !strings.HasPrefix(st, "B3 open") {
			c.failf("set-up: want B3 open, status line %q", st)
		}
		wantCounts(c, recs, []string{"PETR4.SA", "VALE3.SA"}, start, window, 9, 11)
		for _, sym := range []string{"AAPL", "^BVSP", "^GSPC", "BRL=X"} {
			ts := arrivals(recs, sym, start, start.Add(window))
			c.notef("(not asserted) %s: %d requests: %s", sym, len(ts), fmtArrivals(ts, start))
		}
	})
	crit(e, "S4.4", "PETR4, in three places, is requested once per refresh while the view is open", func(c *ck) {
		p := arrivals(recs, "PETR4.SA", start, start.Add(window))
		vl := arrivals(recs, "VALE3.SA", start, start.Add(window))
		c.notef("PETR4.SA %d, VALE3.SA %d; PETR4 shortest gap %s", len(p), len(vl), minGap(p).Round(time.Millisecond))
		if len(p) > len(vl)+1 {
			c.failf("PETR4.SA requested %d times, VALE3.SA %d: more than once per refresh", len(p), len(vl))
		}
		if g := minGap(p); g >= 0 && g < 5*time.Second {
			c.failf("two requests for PETR4.SA %s apart: one refresh asked twice", g.Round(time.Millisecond))
		}
		if n := len(last.rowLines("PETR4")); n != 3 {
			c.failf("want PETR4 shown in its three places (7.2), found %d lines", n)
		}
	})
	crit(e, "S8.3", "a not-found row is not requested on every interval (partial: 100 s of three hours)", func(c *ck) {
		ts := arrivals(recs, "XXXX9.SA", start, start.Add(window))
		c.notef("XXXX9.SA: %d requests: %s", len(ts), fmtArrivals(ts, start))
		if len(ts) > 1 {
			c.failf("XXXX9.SA requested %d times in %s; at most 4 in three hours allows no retry before the hour", len(ts), window)
		}
		if ls := last.rowLines("XXXX9"); len(ls) != 1 || !strings.Contains(ls[0], "not found") {
			c.failf("set-up: want XXXX9 shown as not found, got %q", ls)
		}
	})
}

// S5.2 [view] Given `refresh_seconds = 120` and an open market, when the view
// stays open for 10 minutes, then each row of that market is requested
// between 4 and 6 times.
// Scaled: `refresh_seconds = 20` for 100 s (five intervals, as 10 minutes are
// five intervals of 120 s). The literal check is TestVerifySlow_View_S5_2.
func TestVerifyView_S5_2_IntervalFromConfig(t *testing.T) {
	t.Parallel()
	cfg := strings.Replace(baseConfig, "refresh_seconds = 10", "refresh_seconds = 20", 1)
	e := newEnv(t, "S5.2 scaled: refresh_seconds = 20 for 100 s", cfg)
	window := 100 * time.Second
	recs, start, _ := heartbeat(e, tickers, window)
	crit(e, "S5.2", "each row of the open markets is requested 4 to 6 times in five intervals (scaled)", func(c *ck) {
		wantCounts(c, recs, symsOf(tickers), start, window, 4, 6)
	})
}

// The literal S5.1 and S5.2: 10 minutes of real time each (BRL_VERIFY_SLOW).
func TestVerifySlow_View_S5_1_DefaultIntervalTenMinutes(t *testing.T) {
	needSlow(t, "10 minutes of real time")
	t.Parallel()
	e := newEnv(t, "S5.1 literal: no refresh_seconds, 10 minutes", defaultIntervalConfig)
	recs, start, _ := heartbeat(e, tickers, 10*time.Minute)
	crit(e, "S5.1", "each B3 row is requested 9 to 11 times in 10 minutes", func(c *ck) {
		wantCounts(c, recs, []string{"PETR4.SA", "VALE3.SA", "ITUB4.SA"}, start, 10*time.Minute, 9, 11)
	})
}

func TestVerifySlow_View_S5_2_Interval120TenMinutes(t *testing.T) {
	needSlow(t, "10 minutes of real time")
	t.Parallel()
	cfg := strings.Replace(baseConfig, "refresh_seconds = 10", "refresh_seconds = 120", 1)
	e := newEnv(t, "S5.2 literal: refresh_seconds = 120, 10 minutes", cfg)
	recs, start, _ := heartbeat(e, tickers, 10*time.Minute)
	crit(e, "S5.2", "each row is requested 4 to 6 times in 10 minutes", func(c *ck) {
		wantCounts(c, recs, symsOf(tickers), start, 10*time.Minute, 4, 6)
	})
}

// ---------------------------------------------------------------------------
// S5.3, S5.4, S5.5: a market closed long ago stays unrequested
// ---------------------------------------------------------------------------

// S5.3 [view] Given the US market has been closed for more than 30 minutes and
// every US row has a quote, when the view stays open for 10 minutes, then no US
// row is requested.
// Scaled: 60 s (six intervals of 10 s). The US market closed 2 h ago; `print`
// leaves a quote for every US row in the cache before the view opens. B3 is
// open, as a control that time passed for the view.
// Shown again after that minute with the view open: S5.4 (US rows show the
// last close) and S5.5 (status line `B3 open · US closed`).
func TestVerifyView_S5_3_ClosedMarketNotRequested(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S5.3 scaled (60 s); S5.4, S5.5 with the view open", baseConfig)
	e.setSource(state{ClosedMarkets: []string{"us"}}, "US closed 2 h ago")
	r0 := e.run("print")
	e.wantExit(r0, 0)
	e.wantRequested(r0, []string{"AAPL", "MSFT"}, "set-up: print leaves a quote for every US row")
	window := 60 * time.Second
	v := e.openRows(tickers, nil)
	v.hold(v.start.Add(window), 10*time.Second, nil)
	s := v.snap()
	v.logScreen("SCREEN AFTER 60 s", s, nil)
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)
	crit(e, "S5.3", "no US row is requested while the view stays open (scaled)", func(c *ck) {
		for _, sym := range []string{"AAPL", "MSFT"} {
			if ts := arrivals(r.reqs, sym, v.start.Add(-time.Second), time.Now()); len(ts) != 0 {
				c.failf("%s requested %d times: %s", sym, len(ts), fmtArrivals(ts, v.start))
			}
		}
		ts := arrivals(r.reqs, "^GSPC", v.start.Add(-time.Second), time.Now())
		c.notef("(not a row, not asserted) ^GSPC: %d requests: %s", len(ts), fmtArrivals(ts, v.start))
		b := arrivals(r.reqs, "PETR4.SA", v.start, time.Now())
		c.notef("control: PETR4.SA %d requests: %s", len(b), fmtArrivals(b, v.start))
		if len(b) < 4 {
			c.failf("set-up: the open B3 row was requested %d times in 60 s; the view did not refresh, so the check shows nothing", len(b))
		}
	})
	crit(e, "S5.4", "after a minute open, US rows still show the last close", func(c *ck) {
		for tk, price := range map[string]string{"AAPL": "US$ 229.87", "MSFT": "US$ 512.30"} {
			ls := s.rowLines(tk)
			if len(ls) != 1 || !strings.Contains(ls[0], price) {
				c.failf("row %s: want %s, got %q", tk, price, ls)
			} else if m, _, _, ok := staleMark(ls[0]); ok {
				c.failf("row %s: stale mark %q on a closed market's row", tk, m)
			}
		}
	})
	crit(e, "S5.5", "after a minute open, the status line reads `B3 open · US closed`", func(c *ck) {
		if st := statusLine(s); !strings.HasPrefix(st, "B3 open · US closed") {
			c.failf("status line %q", st)
		} else {
			c.notef("status line %q", st)
		}
	})
}

// ---------------------------------------------------------------------------
// S5.7: the next session starts while the view is open
// ---------------------------------------------------------------------------

// S5.7 [view] Given a closed market and an open view, when the market's next
// session starts, then its rows are requested within one refresh interval
// without a key press.
// The stand-in's B3 session starts 25 s after the view opens (`opens_at_unix`);
// before it, B3's last session was the same hours on the previous weekday
// (closed about 17 h ago). Interval 10 s; no key is pressed. US is open.
func TestVerifyView_S5_7_NextSessionStarts(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S5.7: B3's next session starts 25 s after the view opens", baseConfig)
	opens := time.Unix(time.Now().Add(25*time.Second).Unix(), 0)
	if loc, err := time.LoadLocation("America/Sao_Paulo"); err == nil {
		if wd := opens.In(loc).Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Skip("today is a weekend in São Paulo: no B3 session starts today")
		}
	}
	e.setSource3(state3{OpensAtUnix: map[string]int64{"b3": opens.Unix()}},
		fmt.Sprintf("B3 session starts at %s", opens.UTC().Format(time.RFC3339)))
	v := e.openRows(tickers, func(s screen) bool { return strings.HasPrefix(statusLine(s), "B3 closed") })
	before := v.snap()
	v.hold(opens.Add(13*time.Second), 5*time.Second, func(i int, s screen) {
		e.logf("SAMPLE %d at +%s  status line %q\n", i, s.at.Sub(v.start).Round(time.Second), statusLine(s))
	})
	after := v.snap()
	v.logScreen("SCREEN 13 s AFTER THE SESSION STARTED", after, nil)
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)
	crit(e, "S5.7", "B3 rows are requested within 10 s of the session's start, with no key", func(c *ck) {
		if st := statusLine(before); !strings.HasPrefix(st, "B3 closed") {
			c.failf("set-up: want B3 closed before the session, status line %q", st)
		}
		for _, sym := range []string{"PETR4.SA", "VALE3.SA", "ITUB4.SA"} {
			pre := arrivals(r.reqs, sym, v.start.Add(3*time.Second), opens)
			if len(pre) != 0 {
				c.notef("%s requested while B3 was closed (after the fetch at start): %s", sym, fmtArrivals(pre, v.start))
			}
			ts := arrivals(r.reqs, sym, opens, time.Now())
			if len(ts) == 0 {
				c.failf("%s: no request in the 13 s after the session started (requests: %s)", sym, fmtArrivals(arrivals(r.reqs, sym, v.start, time.Now()), v.start))
				continue
			}
			d := ts[0].Sub(opens)
			c.notef("%s: first request %s after the session started", sym, d.Round(time.Millisecond))
			if d > 10*time.Second {
				c.failf("%s: first request %s after the session started, want within one interval (10 s)", sym, d.Round(time.Millisecond))
			}
		}
		if st := statusLine(after); !strings.HasPrefix(st, "B3 open") {
			c.failf("after the session started the status line reads %q, want `B3 open ...`", st)
		} else {
			c.notef("status line before %q, after %q", statusLine(before), st)
		}
	})
}

// ---------------------------------------------------------------------------
// S5.8: the period after the close
// ---------------------------------------------------------------------------

// S5.8 [view] Given B3 closed less than 30 minutes ago and a B3 row whose quote
// time is before the close, when a refresh interval passes, then that row is
// requested again.
// The stand-in's B3 closed 60 s before each answer; its quote time is 10
// minutes before the close (`quote_before_close_seconds`). Empty cache,
// interval 10 s, the view open 25 s.
// Outside the criterion (Q62: what counts as the closing quote is open): the
// same with the quote time at the close is logged as an observation.
func TestVerifyView_S5_8_RequestedAgainAfterClose(t *testing.T) {
	t.Parallel()
	for _, v := range []struct {
		name   string
		before int64
	}{{"quote_before_close", 600}, {"observation_quote_at_close", 0}} {
		v := v
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, "S5.8: B3 closed 60 s ago, quote time "+map[bool]string{true: "10 min before the close", false: "at the close (observation)"}[v.before > 0], baseConfig)
			e.setSource3(state3{state: state{ClosedMarkets: []string{"b3"}, ClosedAgoSeconds: 60}, QuoteBeforeCloseSeconds: v.before},
				"B3 closed 60 s ago")
			vw := e.openRows(tickers, nil)
			vw.hold(vw.start.Add(25*time.Second), 5*time.Second, nil)
			s := vw.snap()
			vw.logScreen("SCREEN AFTER 25 s", s, nil)
			r := vw.quit([]byte("q"), "q")
			e.wantExit(result{exit: r.exit}, 0)
			if v.before == 0 {
				crit(e, "6.1 closing quote (observation)", "requests of B3 rows when the quote time is the close", func(c *ck) {
					for _, sym := range []string{"PETR4.SA", "VALE3.SA", "ITUB4.SA"} {
						c.notef("%s: %s", sym, fmtArrivals(arrivals(r.reqs, sym, vw.start, time.Now()), vw.start))
					}
					c.notef("status line %q", statusLine(s))
				})
				return
			}
			crit(e, "S5.8", "a B3 row whose quote is before the close is requested again after one interval", func(c *ck) {
				if st := statusLine(s); !strings.HasPrefix(st, "B3 closed") {
					c.failf("set-up: want B3 shown closed, status line %q", st)
				}
				for _, sym := range []string{"PETR4.SA", "VALE3.SA", "ITUB4.SA"} {
					ts := arrivals(r.reqs, sym, vw.start, time.Now())
					c.notef("%s: %s", sym, fmtArrivals(ts, vw.start))
					if len(ts) < 2 {
						c.failf("%s requested %d times in 25 s, want a second request after one interval", sym, len(ts))
						continue
					}
					if g := ts[1].Sub(ts[0]); g > 11*time.Second {
						c.failf("%s: second request %s after the first, want after about one interval (10 s)", sym, g.Round(time.Millisecond))
					}
				}
			})
		})
	}
}

// ---------------------------------------------------------------------------
// S5.10: the interval's floor
// ---------------------------------------------------------------------------

// S5.10 [view, print] Given `refresh_seconds = 3`, when the view runs, then
// rows are requested no more often than every 10 seconds.
// The view open 60 s, open markets, empty cache. A gap is measured between
// arrivals at the stand-in; 50 ms is allowed for the path to it.
func TestVerifyView_S5_10_IntervalFloor(t *testing.T) {
	t.Parallel()
	cfg := strings.Replace(baseConfig, "refresh_seconds = 10", "refresh_seconds = 3", 1)
	e := newEnv(t, "S5.10: refresh_seconds = 3, the view open 60 s", cfg)
	v := e.openRows(tickers, nil)
	first := v.snap()
	v.hold(v.start.Add(60*time.Second), 10*time.Second, nil)
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)
	crit(e, "S5.10", "no row is requested twice within 10 s", func(c *ck) {
		c.notef("status line at start %q", statusLine(first))
		for _, sym := range append(symsOf(tickers), "^BVSP", "^GSPC", "BRL=X") {
			ts := arrivals(r.reqs, sym, v.start, time.Now())
			g := minGap(ts)
			c.notef("%s: %d requests, shortest gap %s: %s", sym, len(ts), g.Round(time.Millisecond), fmtArrivals(ts, v.start))
			if g >= 0 && g < 10*time.Second-50*time.Millisecond {
				c.failf("%s requested twice %s apart, want at least 10 s", sym, g.Round(time.Millisecond))
			}
			if len(ts) > 7 {
				c.failf("%s requested %d times in 60 s, want at most 7", sym, len(ts))
			}
			if strings.HasSuffix(sym, ".SA") || sym == "AAPL" || sym == "MSFT" {
				if len(ts) < 4 {
					c.failf("set-up: %s requested only %d times in 60 s; the view did not refresh", sym, len(ts))
				}
			}
		}
	})
}

// ---------------------------------------------------------------------------
// QR6 and S15.3: three minutes healthy
// ---------------------------------------------------------------------------

// healthy opens the view on 20 tickers and 3 summary entries with an open
// market and a source whose quotes change on every answer, keeps it open for
// d, and samples the screen and the cache folder every period: QR6 (no stale
// mark) and S15.3 / QR8 (the cache folder within 10% of its size after the
// first full refresh).
func healthy(t *testing.T, d, period time.Duration, interval int) {
	cfg, syms := config20()
	if interval != 60 {
		cfg = fmt.Sprintf("refresh_seconds = %d\n", interval) + cfg
	}
	e := newEnv(t, fmt.Sprintf("QR6 and S15.3 / QR8 with the view: %s open, interval %d s, quotes change on every answer, 23 entries", d, interval), cfg)
	e.setSource3(state3{VaryQuotes: true}, "quotes change on every answer")
	var tks []string
	for _, s := range syms {
		if !strings.HasPrefix(s, "^") && s != "BRL=X" {
			tks = append(tks, strings.TrimSuffix(s, ".SA"))
		}
	}
	v := e.openRows(tks, nil)

	// The first full refresh: every entry answered, then the cache written.
	deadline := time.Now().Add(15 * time.Second)
	for {
		answered := map[string]bool{}
		for _, r := range e.src.requests() {
			if r.Status == 200 {
				answered[r.Symbol] = true
			}
		}
		if len(answered) >= len(syms) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("set-up: only %d of %d entries answered within 15 s", len(answered), len(syms))
		}
		time.Sleep(50 * time.Millisecond)
	}
	var base int64
	var names []string
	for prev, since := int64(-1), time.Now(); time.Since(since) < 500*time.Millisecond; time.Sleep(50 * time.Millisecond) {
		base, names = e.cacheSize()
		if base != prev {
			prev, since = base, time.Now()
		}
	}
	e.logf("\nCACHE SIZE AFTER THE FIRST FULL REFRESH (at +%s): %d bytes   files: %s\n",
		time.Since(v.start).Round(time.Millisecond), base, strings.Join(names, " "))
	if base == 0 {
		t.Fatal("set-up: the cache folder is empty after the first full refresh")
	}

	type sample struct {
		at          time.Duration
		cache       int64
		stale       []string
		noPrice     []string
		firstPrices string
	}
	var samples []sample
	lastPrice := ""
	changes := 0
	v.hold(time.Now().Add(d), period, func(i int, s screen) {
		sm := sample{at: s.at.Sub(v.start).Round(time.Second)}
		sm.cache, _ = e.cacheSize()
		for _, tk := range tks {
			ls := s.rowLines(tk)
			if len(ls) != 1 || !hasQuote(ls[0]) {
				sm.noPrice = append(sm.noPrice, tk)
				continue
			}
			if m, _, _, ok := staleMark(ls[0]); ok {
				sm.stale = append(sm.stale, tk+" "+m)
			}
		}
		if ls := s.rowLines("PETR4"); len(ls) == 1 {
			p := priceRE.FindString(ls[0])
			if lastPrice != "" && p != lastPrice {
				changes++
			}
			lastPrice = p
			sm.firstPrices = p
		}
		samples = append(samples, sm)
		e.logf("SAMPLE %d at +%s  cache %d bytes (%+.2f%%)  PETR4 %s  stale %v  without a price %v  status %q\n",
			i, sm.at, sm.cache, 100*(float64(sm.cache)-float64(base))/float64(base), sm.firstPrices, sm.stale, sm.noPrice, statusLine(s))
		if i == 1 || i%6 == 0 {
			v.logScreen(fmt.Sprintf("SCREEN AT SAMPLE %d", i), s, nil)
		}
	})
	s := v.snap()
	v.logScreen("SCREEN AT THE END", s, nil)
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)

	crit(e, "QR6", fmt.Sprintf("no stale mark in any sample every %s for %s", period, d), func(c *ck) {
		bad := 0
		for i, sm := range samples {
			if len(sm.stale) > 0 || len(sm.noPrice) > 0 {
				bad++
				c.failf("sample %d at +%s: stale %v, without a price %v", i+1, sm.at, sm.stale, sm.noPrice)
			}
		}
		c.notef("%d samples, %d with a stale mark; PETR4's price changed %d times between samples", len(samples), bad, changes)
		if min := int(d / period); len(samples) < min {
			c.failf("set-up: %d samples, want at least %d", len(samples), min)
		}
		if changes == 0 {
			c.failf("set-up: PETR4's price never changed on screen; the rows were not refreshed")
		}
	})
	crit(e, "S15.3", fmt.Sprintf("the cache folder stays within 10%% of its size after the first full refresh (%s)", d), func(c *ck) {
		lo, hi := float64(base)*0.9, float64(base)*1.1
		minS, maxS := base, base
		var all []string
		for i, sm := range samples {
			all = append(all, fmt.Sprint(sm.cache))
			if sm.cache < minS {
				minS = sm.cache
			}
			if sm.cache > maxS {
				maxS = sm.cache
			}
			if float64(sm.cache) < lo || float64(sm.cache) > hi {
				c.failf("sample %d at +%s: cache %d bytes, outside 10%% of %d", i+1, sm.at, sm.cache, base)
			}
		}
		end, nm := e.cacheSize()
		c.notef("first %d bytes, min %d (%+.2f%%), max %d (%+.2f%%), at the end %d; files %s; all samples %s",
			base, minS, 100*(float64(minS)-float64(base))/float64(base), maxS, 100*(float64(maxS)-float64(base))/float64(base),
			end, strings.Join(nm, " "), strings.Join(all, " "))
		after := 0
		for _, q := range r.reqs {
			if q.UnixMS > v.start.Add(5*time.Second).UnixMilli() {
				after++
			}
		}
		c.notef("requests after the first refresh: %d", after)
		if want := int(d/(time.Duration(interval)*time.Second)) * len(syms) / 2; after < want {
			c.failf("set-up: %d requests after the first refresh, want at least %d; the cache was not refreshed enough to show anything", after, want)
		}
	})
}

// QR6 [view] With an open market and a source that answers, no row's last
// successful fetch is older than two refresh intervals: the view's rows
// sampled every 10 s, no stale mark. Scaled: 3 minutes in place of 10, at the
// smallest interval (10 s). The literal 10 minutes: TestVerifySlow_View_QR6.
// S15.3 [view] Given the view has run for eight hours of open market, when the
// cache folder is measured, then its size is within 10% of its size after the
// first full refresh. Scaled: 3 minutes at 10 s (about 18 refreshes of 23
// entries, quotes changing on every answer). The eight hours:
// TestVerifyLong_View_QR8_S15_3.
func TestVerifyView_QR6_S15_3_HealthyThreeMinutes(t *testing.T) {
	t.Parallel()
	healthy(t, 3*time.Minute, 10*time.Second, 10)
}

func TestVerifySlow_View_QR6_TenMinutes(t *testing.T) {
	needSlow(t, "10 minutes of real time")
	t.Parallel()
	healthy(t, 10*time.Minute, 10*time.Second, 10)
}

// QR8 and S15.3 with the view as specified: eight hours (BRL_VERIFY_LONG_DURATION)
// at the default 60-second interval, sampled every minute (BRL_VERIFY_LONG).
// Started separately; not run by the ordinary run.
func TestVerifyLong_View_QR8_S15_3_EightHours(t *testing.T) {
	if os.Getenv("BRL_VERIFY_LONG") != "1" {
		t.Skip("long check (8 hours of real time): set BRL_VERIFY_LONG=1")
	}
	t.Parallel()
	healthy(t, longDuration(t, 8*time.Hour), time.Minute, 60)
}

func longDuration(t *testing.T, def time.Duration) time.Duration {
	if v := os.Getenv("BRL_VERIFY_LONG_DURATION"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			t.Fatalf("BRL_VERIFY_LONG_DURATION: %v", err)
		}
		return d
	}
	return def
}

// S8.3 [view] as specified: a not-found row and an unchanged config, the view
// open for three hours: XXXX9 requested at least twice and at most four times
// (BRL_VERIFY_LONG; skipped when BRL_VERIFY_LONG_DURATION is under 3 h).
func TestVerifyLong_View_S8_3_NotFoundThreeHours(t *testing.T) {
	if os.Getenv("BRL_VERIFY_LONG") != "1" {
		t.Skip("long check (3 hours of real time): set BRL_VERIFY_LONG=1")
	}
	if longDuration(t, 8*time.Hour) < 3*time.Hour {
		t.Skip("BRL_VERIFY_LONG_DURATION is under 3 h")
	}
	t.Parallel()
	e := newEnv(t, "S8.3: a not-found row, the view open three hours", "tickers = [\"PETR4\", \"XXXX9\"]\n")
	e.setSource(state{SymbolStatus: map[string]int{"XXXX9.SA": 404}}, "XXXX9.SA answered 404")
	recs, start, _ := heartbeat(e, []string{"PETR4"}, 3*time.Hour)
	crit(e, "S8.3", "XXXX9 requested 2 to 4 times in three hours", func(c *ck) {
		wantCounts(c, recs, []string{"XXXX9.SA"}, start, 3*time.Hour, 2, 4)
	})
}

// ---------------------------------------------------------------------------
// QR11 during a heartbeat refresh
// ---------------------------------------------------------------------------

// QR11 [view] A key press changes the screen within 100 ms, also while a
// refresh is running. Measured with a stand-in source that answers after 5
// seconds: the view opens on a healthy source; then the stand-in holds its
// answers 5 s; once a request of the next heartbeat refresh has arrived and is
// unanswered, `?`, `x`, `s`, `s` are pressed, each timed from the write of the
// key to the first change of the emulated screen. Not parallel.
func TestVerifyView_QR11_KeysDuringHeartbeatRefresh(t *testing.T) {
	e := newEnv(t, "QR11 (view) during a heartbeat refresh", baseConfig)
	v := e.openRows(tickers, nil)
	switched := time.Now()
	e.setSource(state{DelayMS: 5000}, "answers after 5 s")
	var inflight record
	deadline := time.Now().Add(15 * time.Second)
	for inflight.Symbol == "" {
		for _, r := range e.src.requests() {
			if r.UnixMS > switched.UnixMilli() && r.Status == 0 {
				inflight = r
				break
			}
		}
		if inflight.Symbol == "" {
			if time.Now().After(deadline) {
				t.Fatalf("set-up: no heartbeat request arrived within 15 s of the switch")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	e.logf("HEARTBEAT  request #%d for %s arrived at +%s, held 5 s\n", inflight.Seq, inflight.Symbol,
		time.UnixMilli(inflight.UnixMS).Sub(v.start).Round(time.Millisecond))
	type lat struct {
		key string
		d   time.Duration
		ok  bool
		at  time.Time
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
		ls = append(ls, lat{k.name, s.at.Sub(at), ok, at})
		v.waitStable("settled", 2*time.Second, 150*time.Millisecond, func(screen) bool { return true })
	}
	r := v.quit([]byte("q"), "q")
	crit(e, "QR11", "each key changes the screen within 100 ms while a heartbeat refresh runs", func(c *ck) {
		lastKey := ls[len(ls)-1].at
		var rec record
		for _, q := range r.reqs {
			if q.Seq == inflight.Seq {
				rec = q
			}
		}
		if rec.Seq == inflight.Seq && rec.AnsweredMS != 0 && rec.Status > 0 && rec.AnsweredMS < lastKey.UnixMilli() {
			c.failf("set-up: the held request was answered before the last key; the refresh was not running throughout")
		}
		held := 0
		for _, q := range r.reqs {
			if q.UnixMS > switched.UnixMilli() && q.UnixMS < lastKey.UnixMilli() {
				held++
			}
		}
		c.notef("requests of the heartbeat refresh arrived before the last key: %d", held)
		for _, l := range ls {
			c.notef("%s at %s after the held request arrived: screen changed after %s", l.key,
				l.at.Sub(time.UnixMilli(inflight.UnixMS)).Round(time.Millisecond), l.d.Round(100*time.Microsecond))
			if !l.ok || l.d > 100*time.Millisecond {
				c.failf("%s: the screen changed after %s (changed: %v), want within 100 ms", l.key, l.d.Round(time.Millisecond), l.ok)
			}
		}
	})
}
