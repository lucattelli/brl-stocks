//go:build verification && linux

// Close, open and holidays (Q62, D111 to D113), increment 1 of
// research/build/market-hours/scope.md, through the real build on the real
// clock. The settle cases come from a stand-in answer whose trading period
// ended a chosen time before every answer (`closed_ago_seconds`). The holiday
// cases come from an answer that carries yesterday's period, with today's
// usual open (the same hours, `opens_at_unix`) placed a minute ahead and the
// answer kept on yesterday's period (`period_from_unix`, added for these
// checks, with `session_seconds`). S5.9, S5.11, S5.13, S5.14 and S5.15 (to 100
// s after the open) run in the ordinary run; S5.15 to the usual close, S5.16
// and `print` every minute across a holiday's open take more than 5 minutes
// (the second look comes 5 minutes after the open) and are behind
// BRL_VERIFY_SLOW. S5.17 needs the next weekday and is not verified here. The
// reworded S5.3, S5.8 and S13.5 are in their own files. See README.md.
package verification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

type state5 struct {
	state
	OpensAtUnix    map[string]int64      `json:"opens_at_unix,omitempty"`
	PeriodFromUnix map[string]int64      `json:"period_from_unix,omitempty"`
	SessionSeconds int64                 `json:"session_seconds,omitempty"`
	Series         map[string]seriesSpec `json:"series,omitempty"`
}

func (e *env) setHours(st state5, what string) {
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

var (
	b3Rows = []string{"PETR4.SA", "VALE3.SA", "ITUB4.SA"}
	usRows = []string{"AAPL", "MSFT"}
)

// weekdayIn skips the check when t is not a weekday in the zone.
func weekdayIn(tb testing.TB, t time.Time, zone string) {
	if loc, err := time.LoadLocation(zone); err == nil {
		if wd := t.In(loc).Weekday(); wd == time.Saturday || wd == time.Sunday {
			tb.Skipf("%s is not a weekday in %s: no usual open", t.Format(time.RFC3339), zone)
		}
		if wd := t.In(loc).Weekday(); wd == time.Monday {
			tb.Logf("the last session is the previous Friday")
		}
	}
}

// S5.13 [view] Given B3's trading period ended more than 100 minutes ago and
// every B3 row has a quote, when the view stays open for 10 minutes, then no
// B3 row is requested. Scaled: 60 s (six intervals of 10 s). The period ended
// 101 minutes before every answer; `print` leaves the cache. US open (control).
func TestVerifyView_S5_13_B3SettledNotRequested(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S5.13 scaled (60 s): B3's period ended 101 min ago", baseConfig)
	e.setSource(state{ClosedMarkets: []string{"b3"}, ClosedAgoSeconds: 6060}, "B3 period ended 101 min before each answer")
	r0 := e.run("print")
	e.wantExit(r0, 0)
	e.wantRequested(r0, b3Rows, "set-up: print leaves a quote for every B3 row")
	v := e.openRows(tickers, nil)
	v.hold(v.start.Add(60*time.Second), 10*time.Second, nil)
	s := v.snap()
	v.logScreen("SCREEN AFTER 60 s", s, nil)
	r := v.done2()
	crit(e, "S5.13", "no B3 row is requested while the view stays open (scaled)", func(c *ck) {
		for _, sym := range b3Rows {
			if ts := arrivals(r.reqs, sym, v.start.Add(-time.Second), time.Now()); len(ts) != 0 {
				c.failf("%s requested %d times: %s", sym, len(ts), fmtArrivals(ts, v.start))
			}
		}
		c.notef("(a summary entry, not asserted) ^BVSP: %s", fmtArrivals(arrivals(r.reqs, "^BVSP", v.start, time.Now()), v.start))
		a := arrivals(r.reqs, "AAPL", v.start, time.Now())
		c.notef("control: AAPL %d requests; status line %q", len(a), statusLine(s))
		if len(a) < 4 {
			c.failf("set-up: the open US row was requested %d times in 60 s; the view did not refresh", len(a))
		}
	})
}

// S5.14 [view] Given B3's trading period ended less than 100 minutes ago, when
// the view is drawn, then the status line shows B3 as closed. The period ended
// 1, 60 and 99 minutes before every answer. Outside the criteria: the status
// line through 25 s of the settle time (B3 rows still refreshed), recorded.
func TestVerifyView_S5_14_ClosedDuringSettle(t *testing.T) {
	t.Parallel()
	for _, ago := range []int64{60, 3600, 5940} {
		ago := ago
		t.Run(fmt.Sprintf("ended_%ds_ago", ago), func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, fmt.Sprintf("S5.14: B3's period ended %d s ago", ago), baseConfig)
			e.setSource(state{ClosedMarkets: []string{"b3"}, ClosedAgoSeconds: ago}, "B3 period ended before each answer")
			v := e.openRows(tickers, nil)
			var sts []string
			v.hold(v.start.Add(25*time.Second), 5*time.Second, func(i int, s screen) {
				sts = append(sts, fmt.Sprintf("+%s %q", s.at.Sub(v.start).Round(time.Second), statusLine(s)))
			})
			s := v.snap()
			r := v.done2()
			crit(e, "S5.14", "the status line shows B3 as closed during the settle time", func(c *ck) {
				if st := statusLine(s); !strings.HasPrefix(st, "B3 closed") {
					c.failf("status line %q", st)
				}
				for _, x := range sts {
					c.notef("status line %s", x)
					if !strings.Contains(x, "\"B3 closed") {
						c.failf("status line during the settle time: %s", x)
					}
				}
				c.notef("PETR4.SA during the 25 s: %s", fmtArrivals(arrivals(r.reqs, "PETR4.SA", v.start, time.Now()), v.start))
			})
		})
	}
}

// S5.11 [view, print] Given the US market before its regular session, when the
// view is drawn, then US rows show the last regular close, not a pre-market
// price. Today's US session starts 30 minutes from now (`opens_at_unix`); the
// answer carries the previous weekday's period, a regular series ending at
// 229.87 (AAPL's price in the answer) and prices after that period's end
// (235.00 and 236.50, as pre-market trades). Also `print`.
func TestVerifyView_S5_11_PreMarketShowsClose(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S5.11: US before its session, pre-market prices in the answer", baseConfig)
	opens := time.Unix(time.Now().Add(30*time.Minute).Unix(), 0)
	weekdayIn(t, opens, "America/New_York")
	reg := ramp(220, 10)
	last := 229.87
	reg[len(reg)-1] = &last
	e.setHours(state5{OpensAtUnix: map[string]int64{"us": opens.Unix()},
		Series: map[string]seriesSpec{"AAPL": {Closes: reg, After: []float64{235.00, 236.50}}}},
		"US opens in 30 min; AAPL regular close 229.87, pre-market 235.00, 236.50")
	v, s := e.sparkView(viewCols, tickers, nil)
	v.done2()
	r := e.run("print")
	e.wantExit(r, 0)
	crit(e, "S5.11", "US rows show the last regular close, not a pre-market price", func(c *ck) {
		l := strings.Join(s.rowLines("AAPL"), "")
		c.notef("view AAPL %q; status line %q", l, statusLine(s))
		if !strings.Contains(l, "US$ 229.87") || strings.Contains(l, "235.00") || strings.Contains(l, "236.50") {
			c.failf("view: AAPL %q, want US$ 229.87 and no pre-market price", l)
		}
		if !strings.Contains(statusLine(s), "US closed") {
			c.failf("view: status line %q, want US closed before the session", statusLine(s))
		}
		pl := strings.Join(rowLines(r.stdout, "AAPL"), "")
		c.notef("print AAPL %q", pl)
		if !strings.Contains(pl, "US$ 229.87") || strings.Contains(pl, "235.00") || strings.Contains(pl, "236.50") {
			c.failf("print: AAPL %q, want US$ 229.87", pl)
		}
	})
}

// holiday stages a US holiday: the last session was the previous weekday at
// the hours of today's usual open, placed `lead` from now; the answer keeps
// that session until `periodFrom` (zero: never, a holiday). `print` leaves
// the cache before the view opens, so the view's fetch at start sends
// nothing (the market is closed and settled).
func holiday(t *testing.T, what string, lead, periodFrom, session time.Duration) (*env, time.Time) {
	e := newEnv(t, what, baseConfig)
	opens := time.Unix(time.Now().Add(lead).Unix(), 0)
	weekdayIn(t, opens, "America/New_York")
	pf := opens.Add(1000 * time.Hour).Unix()
	if periodFrom > 0 {
		pf = opens.Add(periodFrom).Unix()
	}
	e.setHours(state5{OpensAtUnix: map[string]int64{"us": opens.Unix()}, PeriodFromUnix: map[string]int64{"us": pf},
		SessionSeconds: int64(session / time.Second)},
		fmt.Sprintf("US usual open at %s; the answer carries the last session until %s", opens.UTC().Format("15:04:05"), time.Unix(pf, 0).UTC().Format(time.RFC3339)))
	r0 := e.run("print")
	e.wantExit(r0, 0)
	return e, opens
}

// S5.9 [view, print] Given a weekday that is a holiday for a market, so that
// the source's answer at the market's usual open still carries the last
// session, when the view is open during that market's usual hours, then the
// market is shown as closed.
// S5.15 [view] (partial) Given a weekday that is a holiday for a market, when
// the view stays open from before that market's usual open until its usual
// close, then each row of that market is requested at most twice: here from
// 60 s before the open to 100 s after it (the second look, 5 minutes after the
// open, and the close are in TestVerifySlow_View_S5_15_16).
func TestVerifyView_S5_9_15_HolidayAtOpen(t *testing.T) {
	t.Parallel()
	e, opens := holiday(t, "S5.9, S5.15 (partial): a US holiday, usual open in 60 s", 60*time.Second, 0, 0)
	v := e.openRows(tickers, nil)
	var during []string
	v.hold(opens.Add(100*time.Second), 10*time.Second, func(i int, s screen) {
		if s.at.After(opens.Add(5 * time.Second)) {
			during = append(during, statusLine(s))
		}
	})
	s := v.snap()
	v.logScreen("SCREEN 100 s AFTER THE USUAL OPEN", s, nil)
	r := v.done2()
	crit(e, "S5.9", "on a holiday the market is shown closed during its usual hours", func(c *ck) {
		if len(during) == 0 {
			c.failf("set-up: no sample after the usual open")
		}
		for _, st := range during {
			if !strings.Contains(st, "US closed") {
				c.failf("status line after the usual open %q, want US closed", st)
			}
		}
		c.notef("status lines after the usual open: %q", during)
		for _, tk := range []string{"AAPL", "MSFT"} {
			if !hasQuote(strings.Join(s.rowLines(tk), "")) {
				c.failf("%s: no quote shown", tk)
			}
		}
	})
	crit(e, "S5.15", "each US row requested at most twice from before the open to 100 s after it (partial)", func(c *ck) {
		for _, sym := range usRows {
			ts := arrivals(r.reqs, sym, v.start, time.Now())
			c.notef("%s: %s (usual open at +%s)", sym, fmtArrivals(ts, v.start), opens.Sub(v.start).Round(time.Second))
			if len(ts) > 2 {
				c.failf("%s requested %d times", sym, len(ts))
			}
		}
	})
}

// S5.15 to the usual close, and S5.16 (BRL_VERIFY_SLOW, about 12 and 7 min):
// S5.15 [view] Given a weekday that is a holiday for a market, when the view
// stays open from before that market's usual open until its usual close, then
// each row of that market is requested at most twice. The session is made 10
// minutes long (`session_seconds`) so that the usual close comes within the
// check: open in 60 s, close 10 minutes later, the view held 30 s past it.
// S5.16 [view] Given the source's answer at a market's usual open still carries
// the last session, and its answer 5 minutes later carries that day's session,
// when that second answer arrives, then the market is shown as open. The
// answer switches to today's period 4 minutes after the open.
func TestVerifySlow_View_S5_15_16(t *testing.T) {
	needSlow(t, "about 12 minutes of real time")
	t.Parallel()
	t.Run("S5_15_to_the_close", func(t *testing.T) {
		t.Parallel()
		e, opens := holiday(t, "S5.15: a US holiday from before the open to the close (10-minute session)", 60*time.Second, 0, 10*time.Minute)
		v := e.openRows(tickers, nil)
		v.hold(opens.Add(10*time.Minute+30*time.Second), time.Minute, func(i int, s screen) {
			e.logf("SAMPLE %d at +%s  %q\n", i, s.at.Sub(v.start).Round(time.Second), statusLine(s))
		})
		r := v.done2()
		crit(e, "S5.15", "each US row requested at most twice from before the open until the close", func(c *ck) {
			for _, sym := range usRows {
				ts := arrivals(r.reqs, sym, v.start, time.Now())
				c.notef("%s: %s (open at +%s)", sym, fmtArrivals(ts, v.start), opens.Sub(v.start).Round(time.Second))
				if len(ts) > 2 {
					c.failf("%s requested %d times", sym, len(ts))
				}
			}
		})
	})
	t.Run("S5_16_late_session", func(t *testing.T) {
		t.Parallel()
		e, opens := holiday(t, "S5.16: the answer carries today's session from 4 min after the open", 60*time.Second, 4*time.Minute, 0)
		v := e.openRows(tickers, nil)
		var sw time.Time
		v.hold(opens.Add(6*time.Minute+30*time.Second), 5*time.Second, func(i int, s screen) {
			if sw.IsZero() && strings.Contains(statusLine(s), "US open") {
				sw = s.at
			}
			e.logf("SAMPLE %d at +%s  %q\n", i, s.at.Sub(v.start).Round(time.Second), statusLine(s))
		})
		s := v.snap()
		r := v.done2()
		crit(e, "S5.16", "when the second answer carries today's session, the market is shown open", func(c *ck) {
			ts := arrivals(r.reqs, "AAPL", v.start, time.Now())
			c.notef("AAPL: %s (open at +%s); US open first seen at +%s", fmtArrivals(ts, v.start), opens.Sub(v.start).Round(time.Second), sw.Sub(v.start).Round(time.Second))
			var second time.Time
			for _, x := range ts {
				if x.After(opens.Add(4 * time.Minute)) {
					second = x
					break
				}
			}
			if second.IsZero() {
				c.failf("no request after the answer switched to today's session")
			} else if sw.IsZero() || sw.Before(second) || sw.Sub(second) > 6*time.Second {
				c.failf("US shown open at %v, the second answer at +%s; want open once it arrives", sw, second.Sub(v.start).Round(time.Second))
			}
			if !strings.Contains(statusLine(s), "US open") {
				c.failf("status line at the end %q", statusLine(s))
			}
		})
	})
}

// Outside the criteria (BRL_VERIFY_SLOW, about 9 min): `print` called every
// minute across a holiday's usual open; requests per call recorded, and per
// row asserted at most twice (6.1: a holiday costs at most two requests per
// row, shared by every process).
func TestVerifySlow_Print_HolidayEveryMinute(t *testing.T) {
	needSlow(t, "about 9 minutes of real time")
	t.Parallel()
	e, opens := holiday(t, "print every minute across a holiday's usual open", 90*time.Second, 0, 0)
	start := time.Now()
	var per []string
	e.everyPeriod(9*time.Minute, time.Minute, func(n int, r result) {
		per = append(per, fmt.Sprintf("call %d at %+ds from the open: %s", n, int(time.Since(opens).Seconds()), fmtCounts(bySymbol(r.reqs))))
	}, "print")
	crit(e, "6.1 holiday with print (observation)", "requests per call across the usual open", func(c *ck) {
		for _, p := range per {
			c.notef("%s", p)
		}
		all := e.src.requests()
		for _, sym := range usRows {
			ts := arrivals(all, sym, start, time.Now())
			c.notef("%s after the cache was made: %s", sym, fmtArrivals(ts, opens))
			if len(ts) > 2 {
				c.failf("%s requested %d times across the holiday's open", sym, len(ts))
			}
		}
	})
}
