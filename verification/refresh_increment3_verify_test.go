//go:build verification && linux

// Live refresh, increment 3 of slice 3 (research/build/live-refresh/scope.md):
// `r` in the real view, in real time. S7.1, S7.2, S7.3, S7.4, S7.6, S7.7, S7.8,
// S7.9, and outside the criteria `r` during a heartbeat refresh that is still
// running and `r` pressed ten times fast. The stand-in stages each given: a
// failure (503), a refusal (429, 403), a closed session, a not-found ticker,
// 20 B3 tickers answered after 900 ms. `r` is pressed more than 5 s after the
// last attempt of the rows it should fetch (the 5-second guard of 6.5). See
// README.md.
package verification

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// pressR presses r and returns the time taken just before the key is
// written (compared with arrivals at millisecond resolution, see arrivals).
func (v *view) pressR(what string) time.Time {
	at := time.Now()
	v.key([]byte("r"), what)
	return at
}

// firstAfter returns the first arrival of sym at or after t.
func firstAfter(recs []record, sym string, t time.Time) (time.Time, bool) {
	ts := arrivals(recs, sym, t, t.Add(24*time.Hour))
	if len(ts) == 0 {
		return time.Time{}, false
	}
	return ts[0], true
}

// holdUntil keeps the view open until t (fails the check if the tool exits).
func (v *view) holdUntil(t time.Time) { v.hold(t, time.Hour, nil) }

// S7.1 [view, print] Given B3 is open and the source answers, when `r` is
// pressed, then the first B3 rows are requested within 2 seconds, without
// waiting for the interval. Default interval (60 s); `r` at +7 s.
func TestVerifyView_S7_1_RRequestsB3Within2s(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S7.1: r in the view, default interval", defaultIntervalConfig)
	v := e.openRows(tickers, nil)
	v.holdUntil(v.start.Add(7 * time.Second))
	at := v.pressR("r")
	v.holdUntil(at.Add(3 * time.Second))
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)
	crit(e, "S7.1", "the first B3 rows are requested within 2 s of r", func(c *ck) {
		var first time.Time
		for _, sym := range []string{"PETR4.SA", "VALE3.SA", "ITUB4.SA"} {
			if ft, ok := firstAfter(r.reqs, sym, at); ok {
				c.notef("%s requested %s after r", sym, ft.Sub(at).Round(time.Millisecond))
				if first.IsZero() || ft.Before(first) {
					first = ft
				}
			} else {
				c.notef("%s not requested after r", sym)
			}
		}
		if first.IsZero() {
			c.failf("no B3 row requested within 3 s of r")
		} else if d := first.Sub(at); d > 2*time.Second {
			c.failf("first B3 row requested %s after r, want within 2 s", d.Round(time.Millisecond))
		}
	})
}

// S7.2 [view, print] Given a network error has put the next automatic attempt
// four minutes away, when `r` is pressed, then the first failed rows are
// requested within 2 seconds.
// Scaled: the default interval and one failure put the next attempt one
// minute away (the error line says so), not four. Every row fails (503) at the
// view's fetch at start; the source answers again; `r` at +7 s.
func TestVerifyView_S7_2_RDuringWaitAfterFailure(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S7.2 scaled: one failure, the next attempt 60 s away; r at +7 s", defaultIntervalConfig)
	e.setSource(state{Status: 503}, "every request answered 503")
	v := e.openView(nil)
	s1, _ := v.waitStable("the error line", 10*time.Second, 300*time.Millisecond, func(s screen) bool { return len(errorLines(s)) == 1 })
	v.logScreen("SCREEN AFTER THE FAILURE", s1, nil)
	e.setSource(state{}, "the source answers again")
	v.holdUntil(v.start.Add(7 * time.Second))
	at := v.pressR("r")
	s2, _ := v.waitStable("rows with prices", 5*time.Second, 300*time.Millisecond, rowsShown(tickers))
	v.logScreen("SCREEN AFTER r", s2, nil)
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)
	crit(e, "S7.2", "the failed rows are requested within 2 s of r, before the next automatic attempt", func(c *ck) {
		el := errorLines(s1)
		c.notef("error line before r %q", el)
		if len(el) == 1 {
			if _, ok := hhmmIn(el[0], v.start.Add(30*time.Second), v.start.Add(2*time.Minute)); !ok {
				c.notef("set-up: the next attempt in %q is not about one minute away", el[0])
			}
		}
		for _, tk := range tickers {
			ft, ok := firstAfter(r.reqs, symOf(tk), at)
			if !ok {
				c.failf("%s not requested after r", tk)
				continue
			}
			c.notef("%s requested %s after r", tk, ft.Sub(at).Round(time.Millisecond))
			if d := ft.Sub(at); d > 2*time.Second {
				c.failf("%s requested %s after r, want within 2 s", tk, d.Round(time.Millisecond))
			}
		}
		if el := errorLines(s2); len(el) != 0 {
			c.notef("error line still shown after r: %q", el)
		}
	})
}

// S7.3 [view, print] Given the source is rate-limiting or refusing the tool,
// when `r` is pressed, then no request is sent.
// S7.4 ... then the status line shows the time of the next attempt.
// The source answers 429 (rate-limiting) or 403 (refusing) to the view's fetch
// at start; `r` at +7 s and again at +9 s; 5 s watched after the last press.
func TestVerifyView_S7_3_4_RDuringBlock(t *testing.T) {
	t.Parallel()
	for _, st := range []int{429, 403} {
		st := st
		t.Run(fmt.Sprint(st), func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, fmt.Sprintf("S7.3, S7.4: the source answers %d; r during the block", st), defaultIntervalConfig)
			e.setSource(state{Status: st}, fmt.Sprintf("every request answered %d", st))
			v := e.openView(nil)
			v.waitStable("the error line of the block", 10*time.Second, 300*time.Millisecond, func(s screen) bool { return len(errorLines(s)) == 1 })
			T, ok := firstRefusal(e.src.requests(), st, v.start.Add(-time.Second))
			v.holdUntil(v.start.Add(7 * time.Second))
			before := v.snap()
			at := v.pressR("r")
			s1, _ := v.waitStable("the screen after r", 3*time.Second, 300*time.Millisecond, func(screen) bool { return true })
			v.logScreen("SCREEN AFTER r", s1, nil)
			v.holdUntil(at.Add(2 * time.Second))
			at2 := v.pressR("r again")
			v.holdUntil(at2.Add(5 * time.Second))
			s2 := v.snap()
			v.logScreen("SCREEN 5 s AFTER THE SECOND r", s2, nil)
			r := v.quit([]byte("q"), "q")
			e.wantExit(result{exit: r.exit}, 0)
			crit(e, "S7.3", "no request is sent after r during the block", func(c *ck) {
				after := 0
				for _, q := range r.reqs {
					if q.UnixMS >= at.UnixMilli() {
						after++
					}
				}
				if after != 0 {
					c.failf("%d requests after r: %s", after, fmtCounts(bySymbol(r.reqs)))
				}
			})
			crit(e, "S7.4", "after r the status line shows the time of the next attempt", func(c *ck) {
				if !ok {
					c.failf("set-up: no refused answer recorded")
					return
				}
				c.notef("status line before r %q", statusLine(before))
				for _, s := range []screen{s1, s2} {
					st := statusLine(s)
					c.notef("status line after r %q; error line %q", st, errorLines(s))
					if hh, ok := hhmmIn(st, T.Add(4*time.Minute), T.Add(6*time.Minute)); !ok {
						c.failf("status line %q: want the next attempt about %s (5 min after the refusal), got %q", st, T.Add(5*time.Minute).UTC().Format("15:04"), hh)
					}
				}
			})
		})
	}
}

// S7.6 [view, print] Given a closed market, when `r` is pressed, then its rows
// are requested too. US closed 2 h ago; `print` leaves the cache; the view
// opens; `r` at +7 s (more than 5 s after print's fetch).
func TestVerifyView_S7_6_RRequestsClosedMarket(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S7.6: US closed 2 h ago; r in the view", defaultIntervalConfig)
	e.setSource(state{ClosedMarkets: []string{"us"}}, "US closed 2 h ago")
	e.wantExit(e.run("print"), 0)
	v := e.openRows(tickers, func(s screen) bool { return strings.Contains(statusLine(s), "US closed") })
	v.holdUntil(v.start.Add(7 * time.Second))
	at := v.pressR("r")
	v.holdUntil(at.Add(3 * time.Second))
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)
	crit(e, "S7.6", "the closed market's rows are requested after r", func(c *ck) {
		for _, sym := range []string{"AAPL", "MSFT"} {
			if pre := arrivals(r.reqs, sym, v.start, at); len(pre) != 0 {
				c.notef("%s requested before r: %s", sym, fmtArrivals(pre, v.start))
			}
			ft, ok := firstAfter(r.reqs, sym, at)
			if !ok {
				c.failf("%s not requested within 3 s of r", sym)
				continue
			}
			c.notef("%s requested %s after r", sym, ft.Sub(at).Round(time.Millisecond))
			if d := ft.Sub(at); d > 2*time.Second {
				c.failf("%s requested %s after r, want within 2 s (6.3)", sym, d.Round(time.Millisecond))
			}
		}
	})
}

// S7.7 [view, print] Given `r` was pressed two seconds ago, when `r` is
// pressed again, then no row is requested a second time. `r` at +7 s, again at
// +9 s; requests counted until 5 s after the second.
// Also (outside the criterion's words, the same rule): `r` pressed ten times
// 50 ms apart, and 30 times 30 ms apart as a held key repeats.
func TestVerifyView_S7_7_RTwice(t *testing.T) {
	t.Parallel()
	for _, p := range []struct {
		name  string
		n     int
		every time.Duration
	}{{"twice_2s", 2, 2 * time.Second}, {"ten_fast_50ms", 10, 50 * time.Millisecond}, {"held_30x30ms", 30, 30 * time.Millisecond}} {
		p := p
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, fmt.Sprintf("S7.7: r pressed %d times %s apart", p.n, p.every), defaultIntervalConfig)
			v := e.openRows(tickers, nil)
			v.holdUntil(v.start.Add(7 * time.Second))
			var first, last time.Time
			for i := 0; i < p.n; i++ {
				if i > 0 {
					time.Sleep(p.every)
				}
				last = v.pressR(fmt.Sprintf("r #%d", i+1))
				if i == 0 {
					first = last
				}
			}
			v.holdUntil(last.Add(5 * time.Second))
			r := v.quit([]byte("q"), "q")
			e.wantExit(result{exit: r.exit}, 0)
			id := "S7.7"
			if p.n > 2 {
				id = "S7.7 " + p.name
			}
			crit(e, id, fmt.Sprintf("%d presses of r: each row requested once", p.n), func(c *ck) {
				for _, sym := range append(symsOf(tickers), "^BVSP", "^GSPC", "BRL=X") {
					ts := arrivals(r.reqs, sym, first, time.Now())
					c.notef("%s: %d after the first r: %s", sym, len(ts), fmtArrivals(ts, first))
					if len(ts) > 1 && sym[0] != '^' && sym != "BRL=X" {
						c.failf("%s requested %d times after %d presses of r", sym, len(ts), p.n)
					}
					if len(ts) == 0 && sym[0] != '^' && sym != "BRL=X" {
						c.failf("%s not requested after r", sym)
					}
				}
			})
		})
	}
}

// S7.8 [view, print] Given a not-found row tried ten minutes ago and an
// unchanged config, when `r` is pressed, then that ticker is not requested.
// Scaled: XXXX9 (404) tried at the view's fetch at start, 7 s before `r`, not
// ten minutes. The other rows are the control.
func TestVerifyView_S7_8_RSkipsNotFound(t *testing.T) {
	t.Parallel()
	cfg := strings.Replace(defaultIntervalConfig, `tickers = ["PETR4", "AAPL"]`, `tickers = ["PETR4", "XXXX9", "AAPL"]`, 1)
	e := newEnv(t, "S7.8 scaled: XXXX9 tried 7 s before r", cfg)
	e.setSource(state{SymbolStatus: map[string]int{"XXXX9.SA": 404}}, "XXXX9.SA answered 404")
	v := e.openRows(tickers, func(s screen) bool { return strings.Contains(strings.Join(s.rowLines("XXXX9"), ""), "not found") })
	v.holdUntil(v.start.Add(7 * time.Second))
	at := v.pressR("r")
	v.holdUntil(at.Add(4 * time.Second))
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)
	crit(e, "S7.8", "XXXX9 is not requested after r; the other rows are", func(c *ck) {
		c.notef("XXXX9.SA before r: %s", fmtArrivals(arrivals(r.reqs, "XXXX9.SA", v.start, at), v.start))
		if ts := arrivals(r.reqs, "XXXX9.SA", at, time.Now()); len(ts) != 0 {
			c.failf("XXXX9.SA requested after r: %s", fmtArrivals(ts, at))
		}
		for _, tk := range tickers {
			if _, ok := firstAfter(r.reqs, symOf(tk), at); !ok {
				c.failf("control: %s not requested after r", tk)
			}
		}
	})
}

// S7.9 [view] Given B3 is open, 20 B3 rows and a source that answers within one
// second, when `r` is pressed, then every B3 row is requested before one
// refresh interval has passed. Default interval (60 s); every answer 900 ms
// late; `r` at +8 s.
func TestVerifyView_S7_9_TwentyB3RowsBeforeInterval(t *testing.T) {
	t.Parallel()
	b3 := []string{"PETR4", "VALE3", "ITUB4", "BBDC4", "ABEV3", "WEGE3", "BBAS3", "MGLU3", "SUZB3", "RENT3",
		"CSAN3", "RADL3", "GGBR4", "CSNA3", "ELET3", "PRIO3", "RAIL3", "EQTL3", "LREN3", "HAPV3"}
	e := newEnv(t, "S7.9: 20 B3 rows, answers after 900 ms, r", cfgSpec{top: b3}.String())
	e.setSource(state{DelayMS: 900}, "every answer after 900 ms")
	v := e.openRows(b3, nil)
	v.holdUntil(v.start.Add(8 * time.Second))
	at := v.pressR("r")
	s, _ := v.waitStable("rows after r", 15*time.Second, 500*time.Millisecond, rowsShown(b3))
	v.logScreen("SCREEN AFTER r", s, nil)
	v.holdUntil(at.Add(5 * time.Second))
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)
	crit(e, "S7.9", "every B3 row is requested before 60 s have passed after r", func(c *ck) {
		var lastAt time.Duration
		for _, tk := range b3 {
			ft, ok := firstAfter(r.reqs, symOf(tk), at)
			if !ok {
				c.failf("%s not requested after r", tk)
				continue
			}
			if d := ft.Sub(at); d > lastAt {
				lastAt = d
			}
			if d := ft.Sub(at); d >= 60*time.Second {
				c.failf("%s requested %s after r, want before one interval (60 s)", tk, d.Round(time.Millisecond))
			}
		}
		c.notef("the last of the 20 B3 rows was requested %s after r", lastAt.Round(time.Millisecond))
	})
}

// Outside the criteria: `r` pressed while a heartbeat refresh is still running
// (answers held 5 s, as QR11 measures; and 4 s). Interval 10 s; once a
// heartbeat request has arrived and is unanswered, r (with_r) or nothing
// (control). 1 s after the held answers the source answers at once again.
// Recorded: requests per ticker until 25 s after the heartbeat started, the
// status of each held request, and the screen 2 s after the held answers (the
// spec sets no time limit on a request of the view, so that screen is not
// asserted). Asserted: 25 s after the heartbeat started no row is stale.
func TestVerifyView_R_DuringRunningHeartbeat(t *testing.T) {
	t.Parallel()
	for _, p := range []struct {
		name  string
		withR bool
		hold  int
	}{{"with_r_5s", true, 5000}, {"control_no_r_5s", false, 5000}, {"with_r_4s", true, 4000}} {
		p := p
		withR, name := p.withR, p.name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, "a heartbeat refresh running (answers held 5 s); "+name, baseConfig)
			v := e.openRows(tickers, nil)
			sw := time.Now()
			e.setSource(state{DelayMS: p.hold}, fmt.Sprintf("answers after %d ms", p.hold))
			var hb record
			e.waitRequest("a heartbeat request in flight", 15*time.Second, func(recs []record) bool {
				for _, q := range recs {
					if q.UnixMS > sw.UnixMilli() && q.Status == 0 {
						hb = q
						return true
					}
				}
				return false
			})
			if hb.Symbol == "" {
				return
			}
			hbAt := time.UnixMilli(hb.UnixMS)
			var at time.Time
			changed := false
			if withR {
				time.Sleep(500 * time.Millisecond)
				before := v.snap().text()
				at = v.pressR("r during the heartbeat refresh")
				_, changed = v.waitForQuiet(before, time.Second)
			}
			held := time.Duration(p.hold) * time.Millisecond
			v.holdUntil(hbAt.Add(held + time.Second))
			e.setSource(state{}, "answers at once again")
			v.holdUntil(hbAt.Add(held + 2*time.Second))
			s7 := v.snap()
			v.logScreen("SCREEN 2 s AFTER THE HELD ANSWERS", s7, nil)
			v.hold(hbAt.Add(25*time.Second), 3*time.Second, func(i int, s screen) {
				e.logf("SAMPLE %d at +%s  PETR4 %q  error %q\n", i, s.at.Sub(hbAt).Round(time.Second), s.rowLines("PETR4"), errorLines(s))
			})
			s25 := v.snap()
			v.logScreen("SCREEN 25 s AFTER THE HEARTBEAT STARTED", s25, nil)
			r := v.quit([]byte("q"), "q")
			e.wantExit(result{exit: r.exit}, 0)
			crit(e, "heartbeat "+name, "rows fresh 2 s after the held answers; requests per ticker", func(c *ck) {
				if withR {
					c.notef("r %s after heartbeat request #%d arrived; screen changed after r: %v", at.Sub(hbAt).Round(time.Millisecond), hb.Seq, changed)
				}
				for _, q := range r.reqs {
					if q.UnixMS >= hbAt.UnixMilli()-1000 && q.UnixMS < hbAt.UnixMilli()+1000 {
						c.notef("held request #%d %s -> status %d (answered +%s)", q.Seq, q.Symbol, q.Status, time.Duration(q.AnsweredMS-hb.UnixMS)*time.Millisecond)
					}
				}
				for _, sym := range append(symsOf(tickers), "^BVSP", "^GSPC", "BRL=X") {
					ts := arrivals(r.reqs, sym, hbAt.Add(-time.Second), hbAt.Add(25*time.Second))
					c.notef("%s: %d: %s", sym, len(ts), fmtArrivals(ts, hbAt))
				}
				c.notef("error line 2 s after the held answers %q, at +25 s %q", errorLines(s7), errorLines(s25))
				for _, tk := range tickers {
					if m, _, _, ok := staleMark(strings.Join(s7.rowLines(tk), "")); ok {
						c.notef("%s stale 2 s after its held answer was written: %s", tk, m)
					}
					if m, _, _, ok := staleMark(strings.Join(s25.rowLines(tk), "")); ok {
						c.failf("%s stale 25 s after the heartbeat started: %s", tk, m)
					}
				}
			})
		})
	}
}

// waitForQuiet reports whether the screen changed from before within d.
func (v *view) waitForQuiet(before string, d time.Duration) (screen, bool) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		s := v.snap()
		if s.text() != before {
			return s, true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return v.snap(), false
}
