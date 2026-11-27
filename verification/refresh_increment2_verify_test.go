//go:build verification && linux

// Live refresh, increment 2 of slice 3 (research/build/live-refresh/scope.md):
// failures over time while the real view stays open in real time. S6.4, S6.5,
// S6.6, S7.5, S7.10; shown again on a heartbeat refresh that fails: S6.1,
// S6.2, S6.3, S6.8, S6.9, S6.10, S6.11, S8.2. Also QR4 and QR5 with the view
// open and `brl-stocks print` called every 2 seconds, every instance counted
// at the stand-in. Scaled to the 10-second interval where the criterion's
// times follow the interval (S6.4, QR4); the waits of a block (S7.5) are fixed
// at 5 minutes and up, so the ordinary run sees the first wait end and the
// second one announced, and the full series is behind BRL_VERIFY_SLOW and
// BRL_VERIFY_LONG. See README.md.
package verification

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// firstRefusal returns when the first answer with the status was written.
func firstRefusal(recs []record, status int, after time.Time) (time.Time, bool) {
	var first int64
	for _, r := range recs {
		if r.Status == status && r.AnsweredMS > 0 && r.UnixMS >= after.UnixMilli() && (first == 0 || r.AnsweredMS < first) {
			first = r.AnsweredMS
		}
	}
	return time.UnixMilli(first), first != 0
}

// bursts groups arrivals at or after from into attempts: a request more than
// 3 s after the previous one starts a new attempt. It returns each attempt's
// first arrival and its size.
func bursts(recs []record, from time.Time) ([]time.Time, []int) {
	var at []time.Time
	var n []int
	var last time.Time
	for _, r := range recs {
		t := time.UnixMilli(r.UnixMS)
		if t.Before(from) {
			continue
		}
		if len(at) == 0 || t.Sub(last) > 3*time.Second {
			at = append(at, t)
			n = append(n, 0)
		}
		n[len(n)-1]++
		last = t
	}
	return at, n
}

// waitRequest polls the stand-in until cond holds for its record.
func (e *env) waitRequest(what string, timeout time.Duration, cond func(recs []record) bool) bool {
	e.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond(e.src.requests()) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.failf("set-up: the stand-in's record never showed %s within %s", what, timeout)
	return false
}

// markAge checks a stale mark against the real age since the last success.
func markAge(c *ck, tk, mark string, n int, unit string, age time.Duration) {
	c.t.Helper()
	var want int
	switch unit {
	case "s":
		want = int(age / time.Second)
	case "m":
		want = int(age / time.Minute)
	case "h":
		want = int(age / time.Hour)
	default:
		want = int(age / (24 * time.Hour))
	}
	wantUnit := "s"
	switch {
	case age >= 24*time.Hour:
		wantUnit = "d"
	case age >= time.Hour:
		wantUnit = "h"
	case age >= time.Minute:
		wantUnit = "m"
	}
	c.notef("%s: mark %s, real age %s", tk, mark, age.Round(100*time.Millisecond))
	if unit != wantUnit && !(age%time.Minute < 3*time.Second || age%time.Minute > 57*time.Second) {
		c.failf("%s: mark %s, want the age %s in unit %s", tk, mark, age.Round(time.Second), wantUnit)
	} else if unit == wantUnit && (n < want-2 || n > want+2) {
		c.failf("%s: mark %s, want about %d%s (real age %s)", tk, mark, want, unit, age.Round(time.Second))
	}
}

// ---------------------------------------------------------------------------
// S6.4: the waits of a row that keeps failing
// ---------------------------------------------------------------------------

// s64 runs S6.4 at the given interval for the first len(want) gaps. `print`
// leaves the cache (every row succeeds, XXXX9 not found); then VALE3 answers
// 503 for good and the view opens. The other rows keep answering. Also, on the
// screen after the first failure of that heartbeat refresh: S6.1, S6.2, S6.8,
// S6.9, S8.2; at the end S6.10 (the mark reads the real age).
func s64(t *testing.T, cfgText string, interval time.Duration, want []time.Duration, extra bool) {
	e := newEnv(t, fmt.Sprintf("S6.4 at a %s interval: VALE3 keeps failing (503); gaps %v", interval, want), cfgText)
	e.setSource(state{SymbolStatus: map[string]int{"XXXX9.SA": 404}}, "XXXX9.SA answered 404")
	r0 := e.run("print")
	e.wantExit(r0, 0)
	var lastOK time.Time
	for _, q := range r0.reqs {
		if q.Symbol == "VALE3.SA" && q.Status == 200 {
			lastOK = time.UnixMilli(q.AnsweredMS)
		}
	}
	e.setSource(state{SymbolStatus: map[string]int{"XXXX9.SA": 404, "VALE3.SA": 503}}, "VALE3.SA 503 for good, XXXX9.SA 404")
	others := []string{"PETR4", "AAPL", "ITUB4", "MSFT"}
	v := e.openRows(append(others, "VALE3"), nil)
	// The first failure, on a heartbeat refresh.
	var f1 time.Time
	e.waitRequest("a failed request for VALE3.SA", 2*interval+5*time.Second, func(recs []record) bool {
		for _, q := range recs {
			if q.Symbol == "VALE3.SA" && q.Status == 503 {
				f1 = time.UnixMilli(q.UnixMS)
				return true
			}
		}
		return false
	})
	if f1.IsZero() {
		return
	}
	s1, _ := v.waitStable("VALE3 stale and an error line", 5*time.Second, 300*time.Millisecond, func(s screen) bool {
		ls := s.rowLines("VALE3")
		_, _, _, st := staleMark(strings.Join(ls, ""))
		return len(ls) == 1 && st && len(errorLines(s)) == 1
	})
	v.logScreen("SCREEN AFTER THE FIRST FAILURE OF VALE3", s1, nil)
	var sum time.Duration
	for _, w := range want {
		sum += w
	}
	v.hold(f1.Add(sum+interval/2+2*time.Second), interval, nil)
	end := v.snap()
	endAt := time.Now()
	v.logScreen("SCREEN AT THE END", end, nil)
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)

	crit(e, "S6.4", fmt.Sprintf("VALE3's automatic attempts come %v apart (interval %s)", want, interval), func(c *ck) {
		ts := arrivals(r.reqs, "VALE3.SA", f1, time.Now())
		c.notef("VALE3.SA attempts from the first failure: %s", fmtArrivals(ts, f1))
		for i, w := range want {
			if i+1 >= len(ts) {
				c.failf("gap %d: want an attempt %s after the previous one, none came (attempts %s)", i+1, w, fmtArrivals(ts, f1))
				break
			}
			g := ts[i+1].Sub(ts[i])
			c.notef("gap %d: %s (want %s)", i+1, g.Round(10*time.Millisecond), w)
			if g < w-500*time.Millisecond || g > w+1500*time.Millisecond {
				c.failf("gap %d: %s, want %s", i+1, g.Round(10*time.Millisecond), w)
			}
		}
		if len(ts) > len(want)+1 {
			c.failf("more attempts than expected: %s", fmtArrivals(ts, f1))
		}
		p := arrivals(r.reqs, "PETR4.SA", f1, time.Now())
		c.notef("control: PETR4.SA %d requests in the same time: %s", len(p), fmtArrivals(p, f1))
	})
	if !extra {
		return
	}
	crit(e, "S6.1", "after a heartbeat refresh in which VALE3 failed, it still shows its last price", func(c *ck) {
		if ls := s1.rowLines("VALE3"); len(ls) != 1 || !strings.Contains(ls[0], "R$ 61,07") {
			c.failf("VALE3: want R$ 61,07, got %q", ls)
		}
	})
	crit(e, "S6.2", "VALE3 is marked stale with its age", func(c *ck) {
		ls := s1.rowLines("VALE3")
		if m, n, u, ok := staleMark(strings.Join(ls, "")); !ok {
			c.failf("VALE3: no stale mark in %q", ls)
		} else {
			markAge(c, "VALE3", m, n, u, s1.at.Sub(lastOK))
		}
	})
	crit(e, "S6.8", "only VALE3 is stale", func(c *ck) {
		for _, tk := range others {
			if ls := s1.rowLines(tk); len(ls) != 1 || !hasQuote(ls[0]) {
				c.failf("row %s: want a quote, got %q", tk, ls)
			} else if m, _, _, ok := staleMark(ls[0]); ok {
				c.failf("row %s: stale mark %q", tk, m)
			}
		}
	})
	crit(e, "S6.9", "the error line names the cause, 1 row and the time of the next attempt", func(c *ck) {
		el := errorLines(s1)
		if len(el) != 1 {
			c.failf("want one error line, got %q", el)
			return
		}
		l := el[0]
		c.notef("%q", l)
		if !strings.HasPrefix(strings.TrimSpace(l), "source unreachable") || !strings.Contains(l, "1 row") {
			c.failf("want `source unreachable` and `1 row`, got %q", l)
		}
		if hh, ok := hhmmIn(l, f1.Add(interval-time.Minute), f1.Add(interval+time.Minute)); !ok {
			c.failf("want the next attempt about %s, got %q", f1.Add(interval).UTC().Format("15:04"), hh)
		}
	})
	crit(e, "S8.2", "with a not-found row, the other rows show fresh quotes on the heartbeat refresh", func(c *ck) {
		if ls := s1.rowLines("XXXX9"); len(ls) != 1 || !strings.Contains(ls[0], "not found") {
			c.failf("XXXX9: want not found, got %q", ls)
		}
		for _, tk := range []string{"PETR4", "AAPL", "ITUB4", "MSFT"} {
			ts := arrivals(r.reqs, symOf(tk), v.start, time.Now())
			if len(ts) < 2 {
				c.failf("%s requested %d times while the view was open; want the heartbeat to refresh it", tk, len(ts))
			}
		}
		if n := len(arrivals(r.reqs, "XXXX9.SA", v.start, time.Now())); n != 0 {
			c.notef("XXXX9.SA requested %d times while open", n)
		}
	})
	crit(e, "S6.10", "the mark reads the age since the last successful fetch (scaled: minutes, not 12)", func(c *ck) {
		ls := end.rowLines("VALE3")
		if m, n, u, ok := staleMark(strings.Join(ls, "")); !ok {
			c.failf("VALE3: no stale mark at the end, %q", ls)
		} else {
			markAge(c, "VALE3", m, n, u, endAt.Sub(lastOK))
		}
	})
}

var s64Config = "refresh_seconds = 10\ntickers = [\"PETR4\", \"AAPL\", \"XXXX9\"]\n\n[[group]]\nname = \"Brasil\"\ntickers = [\"VALE3\", \"ITUB4\"]\n\n[[group]]\nname = \"US\"\ntickers = [\"MSFT\"]\n"

// S6.4 [view] Given a row that keeps failing with the default interval, when
// the view stays open, then the automatic attempts for it come 60, 120, 240,
// 480, 960 and then every 1,800 seconds apart.
// Scaled: interval 10 s, so 10, 20, 40, 80 s (2 min 30 s). Literal at 10 s up
// to 320 s: TestVerifySlow_View_S6_4; at 60 s with the cap: TestVerifyLong_View_S6_4.
func TestVerifyView_S6_4_S6_1_2_8_9_10_S8_2_RowKeepsFailing(t *testing.T) {
	t.Parallel()
	s := time.Second
	s64(t, s64Config, 10*s, []time.Duration{10 * s, 20 * s, 40 * s, 80 * s}, true)
}

func TestVerifySlow_View_S6_4_TenSecondIntervalToFiveMinutes(t *testing.T) {
	needSlow(t, "about 11 minutes of real time")
	t.Parallel()
	s := time.Second
	s64(t, s64Config, 10*s, []time.Duration{10 * s, 20 * s, 40 * s, 80 * s, 160 * s, 320 * s}, false)
}

func TestVerifyLong_View_S6_4_DefaultIntervalWithCap(t *testing.T) {
	if needLong(t, "about 1 h 35 min of real time") {
		return
	}
	t.Parallel()
	s := time.Second
	cfg := strings.Replace(s64Config, "refresh_seconds = 10\n", "", 1)
	s64(t, cfg, 60*s, []time.Duration{60 * s, 120 * s, 240 * s, 480 * s, 960 * s, 1800 * s, 1800 * s}, false)
}

func needLong(t *testing.T, what string) bool {
	t.Helper()
	if os.Getenv("BRL_VERIFY_LONG") != "1" {
		t.Skip("long check (" + what + "): set BRL_VERIFY_LONG=1")
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// S6.3, S6.5, S6.6, S6.11 on heartbeat refreshes
// ---------------------------------------------------------------------------

// S6.3 [view, print] Given a refresh in which five rows failed, when the view
// is drawn, then exactly one error line is shown (here a heartbeat refresh;
// also S6.1, S6.2, S6.9 for every row).
// S6.5 [view, print] Given stale rows and an error line, when a refresh
// succeeds for every row, then the stale marks are gone.
// S6.6 ... then the error line is gone.
// S6.11 [view, print] Given every row updates and a summary entry fails, when
// the view is drawn, then the error line says the summary was not updated.
// One view: healthy at start; then 503 for everything until the heartbeat has
// failed; then healthy until the next attempt (one interval later) succeeds;
// then ^GSPC alone 503 until the next heartbeat.
func TestVerifyView_S6_3_5_6_11_FailThenRecover(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S6.3, S6.5, S6.6, S6.11 (and S6.1, S6.2, S6.9) on heartbeat refreshes", baseConfig)
	v := e.openRows(tickers, nil)
	before := v.snap()
	t503 := time.Now()
	e.setSource(state{Status: 503}, "every request answered 503")
	s1, ok1 := v.waitStable("five stale rows and an error line", 20*time.Second, 300*time.Millisecond, func(s screen) bool {
		for _, tk := range tickers {
			ls := s.rowLines(tk)
			if len(ls) != 1 {
				return false
			}
			if _, _, _, ok := staleMark(ls[0]); !ok {
				return false
			}
		}
		return len(errorLines(s)) >= 1
	})
	v.logScreen("SCREEN AFTER THE FAILED HEARTBEAT REFRESH", s1, nil)
	tOK := time.Now()
	e.setSource(state{}, "healthy again")
	s2, ok2 := v.waitStable("no stale mark and no error line", 25*time.Second, 300*time.Millisecond, func(s screen) bool {
		for _, tk := range tickers {
			ls := s.rowLines(tk)
			if len(ls) != 1 || !hasQuote(ls[0]) {
				return false
			}
			if _, _, _, ok := staleMark(ls[0]); ok {
				return false
			}
		}
		return len(errorLines(s)) == 0
	})
	v.logScreen("SCREEN AFTER RECOVERY", s2, nil)
	tSum := time.Now()
	e.setSource(state{SymbolStatus: map[string]int{"^GSPC": 503}}, "^GSPC answered 503")
	s3, ok3 := v.waitStable("an error line about the summary", 25*time.Second, 300*time.Millisecond, func(s screen) bool {
		return len(errorLines(s)) == 1
	})
	v.logScreen("SCREEN AFTER ^GSPC FAILED", s3, nil)
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)

	crit(e, "S6.1", "every row keeps its last price after the failed heartbeat refresh", func(c *ck) {
		for _, tk := range tickers {
			p0 := priceRE.FindString(strings.Join(before.rowLines(tk), ""))
			p1 := priceRE.FindString(strings.Join(s1.rowLines(tk), ""))
			if !ok1 || p0 == "" || p0 != p1 {
				c.failf("%s: price before %q, after the failure %q", tk, p0, p1)
			}
		}
	})
	crit(e, "S6.2", "every row is marked stale with its age", func(c *ck) {
		for _, tk := range tickers {
			ls := s1.rowLines(tk)
			if m, n, u, ok := staleMark(strings.Join(ls, "")); !ok {
				c.failf("%s: no stale mark: %q", tk, ls)
			} else {
				var last time.Time
				for _, q := range r.reqs {
					if q.Symbol == symOf(tk) && q.Status == 200 && q.UnixMS < t503.UnixMilli() {
						last = time.UnixMilli(q.AnsweredMS)
					}
				}
				markAge(c, tk, m, n, u, s1.at.Sub(last))
			}
		}
	})
	crit(e, "S6.3", "five rows failed on a heartbeat refresh: exactly one error line", func(c *ck) {
		if el := errorLines(s1); len(el) != 1 {
			c.failf("want one error line, got %d: %q", len(el), el)
		}
	})
	crit(e, "S6.9", "the error line names the cause, 5 rows and the next attempt", func(c *ck) {
		el := errorLines(s1)
		if len(el) != 1 {
			c.failf("no single error line: %q", el)
			return
		}
		c.notef("%q", el[0])
		if !strings.HasPrefix(strings.TrimSpace(el[0]), "source unreachable") || !strings.Contains(el[0], "5 rows") {
			c.failf("want `source unreachable` and `5 rows`, got %q", el[0])
		}
		if _, ok := hhmmIn(el[0], s1.at.Add(-time.Minute), s1.at.Add(time.Minute)); !ok {
			c.failf("want the next attempt's HH:MM, got %q", el[0])
		}
	})
	crit(e, "S6.5", "after a refresh succeeds for every row, the stale marks are gone", func(c *ck) {
		if !ok2 {
			c.failf("the stale marks were still shown 25 s after the source answered again")
		}
		c.notef("recovered %s after the source answered again", s2.at.Sub(tOK).Round(time.Millisecond))
	})
	crit(e, "S6.6", "after a refresh succeeds for every row, the error line is gone", func(c *ck) {
		if el := errorLines(s2); len(el) != 0 || !ok2 {
			c.failf("error line(s) after recovery: %q", el)
		}
	})
	crit(e, "S6.11", "every row updates and ^GSPC fails: the error line says the summary was not updated", func(c *ck) {
		el := errorLines(s3)
		if !ok3 || len(el) != 1 {
			c.failf("want one error line, got %q", el)
			return
		}
		c.notef("%q, %s after the switch", el[0], s3.at.Sub(tSum).Round(time.Millisecond))
		if !strings.Contains(el[0], "summary not updated") {
			c.failf("want `summary not updated`, got %q", el[0])
		}
		if strings.Contains(el[0], "row") {
			c.failf("want no row count, got %q", el[0])
		}
		for _, tk := range tickers {
			if m, _, _, ok := staleMark(strings.Join(s3.rowLines(tk), "")); ok {
				c.failf("%s: stale mark %q although every row updated", tk, m)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// S7.10 and S7.5: a block while the view stays open
// ---------------------------------------------------------------------------

// S7.10 [view] Given the source refused a request and the view stays open,
// when the wait ends and the source answers, then the rows are requested again
// without a key press.
// S7.5 [view] (the first wait) Given the source refuses a request, ... the
// next attempt comes 5 minutes later.
// The view opens healthy; then every request is answered 429 until the first
// refusal; 3 s later the source answers again. Nothing may be requested for 5
// minutes; then the rows are, and the error line goes. Not scaled: the wait of
// a block does not follow the interval. About 5 min 30 s.
func TestVerifyView_S7_10_S7_5_BlockEndsWithoutKey(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S7.10 and S7.5 (first wait): a block while the view is open", baseConfig)
	v := e.openRows(tickers, nil)
	sw := time.Now()
	e.setSource(state{Status: 429}, "every request answered 429")
	var T time.Time
	e.waitRequest("a refused answer", 20*time.Second, func(recs []record) bool {
		var ok bool
		T, ok = firstRefusal(recs, 429, sw)
		return ok
	})
	if T.IsZero() {
		return
	}
	time.Sleep(3 * time.Second)
	e.setSource(state{}, "the source answers again")
	sBlock, _ := v.waitStable("the error line of the block", 5*time.Second, 300*time.Millisecond, func(s screen) bool { return len(errorLines(s)) == 1 })
	v.logScreen("SCREEN DURING THE BLOCK", sBlock, nil)
	v.hold(T.Add(5*time.Minute+25*time.Second), 30*time.Second, func(i int, s screen) {
		e.logf("SAMPLE %d at +%s  error line %q  status %q\n", i, s.at.Sub(v.start).Round(time.Second), errorLines(s), statusLine(s))
	})
	end := v.snap()
	v.logScreen("SCREEN AFTER THE WAIT", end, nil)
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)
	at, n := bursts(r.reqs, T.Add(time.Second))
	var b strings.Builder
	for i := range at {
		fmt.Fprintf(&b, "+%s(%d) ", at[i].Sub(T).Round(10*time.Millisecond), n[i])
	}
	e.logf("ATTEMPTS AFTER THE FIRST REFUSAL (start, requests): %s\n", b.String())
	crit(e, "S7.5", "after the first refusal the next attempt comes 5 minutes later (the first wait)", func(c *ck) {
		el := errorLines(sBlock)
		c.notef("during the block: %q; status %q", el, statusLine(sBlock))
		if len(el) != 1 || !strings.HasPrefix(strings.TrimSpace(el[0]), "source is rate-limiting") {
			c.failf("want one error line `source is rate-limiting ...`, got %q", el)
		} else if _, ok := hhmmIn(el[0], T.Add(4*time.Minute), T.Add(6*time.Minute)); !ok {
			c.failf("want the next attempt at about %s, got %q", T.Add(5*time.Minute).UTC().Format("15:04"), el[0])
		}
		if len(at) == 0 {
			c.failf("no attempt after the first refusal")
			return
		}
		g := at[0].Sub(T)
		c.notef("first attempt %s after the first refused answer", g.Round(10*time.Millisecond))
		if g < 5*time.Minute-time.Second || g > 5*time.Minute+5*time.Second {
			c.failf("first attempt %s after the first refusal, want 5 min", g.Round(10*time.Millisecond))
		}
	})
	crit(e, "S7.10", "when the wait ends and the source answers, the rows are requested again, no key pressed", func(c *ck) {
		end5 := T.Add(5*time.Minute - time.Second)
		for _, tk := range tickers {
			ts := arrivals(r.reqs, symOf(tk), end5, time.Now())
			c.notef("%s after the wait: %s", tk, fmtArrivals(ts, T))
			if len(ts) == 0 {
				c.failf("%s not requested again after the wait", tk)
			} else if d := ts[0].Sub(T); d > 5*time.Minute+15*time.Second {
				c.failf("%s requested again %s after the refusal, want soon after the 5-minute wait", tk, d.Round(time.Second))
			}
		}
		if el := errorLines(end); len(el) != 0 {
			c.failf("error line still shown after the wait: %q", el)
		}
		for _, tk := range tickers {
			if m, _, _, ok := staleMark(strings.Join(end.rowLines(tk), "")); ok {
				c.failf("%s: stale mark %q after the block ended", tk, m)
			}
		}
	})
}

// s75 is S7.5 at its own length: the view alone, the source refusing
// everything from the first request, attempts timed for the given gaps.
func s75(t *testing.T, want []time.Duration) {
	e := newEnv(t, fmt.Sprintf("S7.5: the source keeps refusing; gaps %v", want), baseConfig)
	e.setSource(state{Status: 429}, "every request answered 429, from the first")
	v := e.openView(nil)
	var T time.Time
	e.waitRequest("a refused answer", 15*time.Second, func(recs []record) bool {
		var ok bool
		T, ok = firstRefusal(recs, 429, v.start.Add(-time.Second))
		return ok
	})
	if T.IsZero() {
		return
	}
	var sum time.Duration
	for _, w := range want {
		sum += w
	}
	v.hold(T.Add(sum+30*time.Second), time.Minute, func(i int, s screen) {
		e.logf("SAMPLE %d at +%s  error line %q  status %q\n", i, s.at.Sub(v.start).Round(time.Second), errorLines(s), statusLine(s))
	})
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)
	crit(e, "S7.5", fmt.Sprintf("attempts come %v apart", want), func(c *ck) {
		at, n := bursts(r.reqs, T.Add(time.Second))
		prev := T
		for i, w := range want {
			if i >= len(at) {
				c.failf("attempt %d: none came", i+1)
				break
			}
			g := at[i].Sub(prev)
			c.notef("attempt %d: %s after the previous (%d requests), want %s", i+1, g.Round(10*time.Millisecond), n[i], w)
			if g < w-time.Second || g > w+5*time.Second {
				c.failf("attempt %d: %s after the previous, want %s", i+1, g.Round(10*time.Millisecond), w)
			}
			prev = at[i]
		}
	})
}

func TestVerifySlow_View_S7_5_FiveAndTenMinutes(t *testing.T) {
	needSlow(t, "about 16 minutes of real time")
	t.Parallel()
	s75(t, []time.Duration{5 * time.Minute, 10 * time.Minute})
}

func TestVerifyLong_View_S7_5_FullSeries(t *testing.T) {
	if needLong(t, "about 1 h 36 min of real time") {
		return
	}
	t.Parallel()
	s75(t, []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 30 * time.Minute, 30 * time.Minute})
}

// ---------------------------------------------------------------------------
// QR4 and QR5 with the view open
// ---------------------------------------------------------------------------

// qr4 opens the view on 20 tickers and 3 summary entries (open market) and
// calls `brl-stocks print` every 2 s for d; every instance's requests are
// counted at the stand-in: at most 1.5 per entry per interval (QR4: 15 per
// entry in 10 intervals), and at most 345 x d/(10 intervals) in total.
func qr4(t *testing.T, d time.Duration, interval int) {
	cfg, syms := config20()
	if interval != 60 {
		cfg = fmt.Sprintf("refresh_seconds = %d\n", interval) + cfg
	}
	e := newEnv(t, fmt.Sprintf("QR4 with the view: view open and print every 2 s for %s, interval %d s, 23 entries", d, interval), cfg)
	var tks []string
	for _, s := range syms {
		if !strings.HasPrefix(s, "^") && s != "BRL=X" {
			tks = append(tks, strings.TrimSuffix(s, ".SA"))
		}
	}
	v := e.openView(nil)
	t0 := v.start
	runs := e.everyPeriod(d, 2*time.Second, nil, "print")
	s := v.snap()
	v.logScreen("SCREEN AT THE END", s, nil)
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)
	recs := e.src.requests()
	crit(e, "QR4", fmt.Sprintf("at most 15 requests per entry per 10 intervals, all instances (%s)", d), func(c *ck) {
		intervals := float64(d) / float64(time.Duration(interval)*time.Second)
		perEntry := int(1.5 * intervals)
		total := 0
		counts := map[string]int{}
		for _, q := range recs {
			if q.UnixMS >= t0.UnixMilli() && q.UnixMS < t0.Add(d).UnixMilli() {
				counts[q.Symbol]++
				total++
			}
		}
		c.notef("%d print calls; %d requests in %s: %s", runs, total, d, fmtCounts(counts))
		for _, sym := range syms {
			if counts[sym] > perEntry {
				c.failf("%s requested %d times, want at most %d", sym, counts[sym], perEntry)
			}
			if counts[sym] < int(intervals)/2 {
				c.failf("set-up: %s requested only %d times; the refresh did not run", sym, counts[sym])
			}
		}
		if limit := perEntry * len(syms); total > limit {
			c.failf("%d requests in total, want at most %d", total, limit)
		}
		for _, tk := range tks {
			if ls := s.rowLines(tk); len(ls) != 1 || !hasQuote(ls[0]) {
				c.failf("view row %s: want a quote at the end, got %q", tk, ls)
			} else if m, _, _, ok := staleMark(ls[0]); ok {
				c.failf("view row %s: stale mark %q", tk, m)
			}
		}
	})
}

// QR4 scaled: 2 minutes at the 10-second interval (12 intervals: at most 18 per
// entry, 414 in total). Full: TestVerifySlow_View_QR4_TenMinutes.
func TestVerifyView_QR4_Scaled_ViewAndPrintEveryTwoSeconds(t *testing.T) {
	t.Parallel()
	qr4(t, 2*time.Minute, 10)
}

func TestVerifySlow_View_QR4_TenMinutes(t *testing.T) {
	needSlow(t, "10 minutes of real time")
	t.Parallel()
	qr4(t, 10*time.Minute, 60)
}

// qr5 opens the view on 20 tickers and 3 summary entries against a source that
// refuses everything, and calls `brl-stocks print --refresh` every 2 s for d
// from the first refused answer; at most 10 requests in any 10 minutes from
// the first refused answer (arrivals after it was written), all instances.
// The second refusal's announced next attempt is checked against S7.5's 10
// minutes.
func qr5(t *testing.T, d time.Duration) {
	cfg, _ := config20()
	cfg = "refresh_seconds = 10\n" + cfg
	e := newEnv(t, fmt.Sprintf("QR5 with the view: source refuses everything; view open and print --refresh every 2 s for %s", d), cfg)
	e.setSource(state{Status: 429}, "every request answered 429, from the first")
	v := e.openView(nil)
	var T time.Time
	e.waitRequest("a refused answer", 15*time.Second, func(recs []record) bool {
		var ok bool
		T, ok = firstRefusal(recs, 429, v.start.Add(-time.Second))
		return ok
	})
	if T.IsZero() {
		return
	}
	runs := e.everyPeriod(time.Until(T.Add(d)), 2*time.Second, nil, "print", "--refresh")
	s := v.snap()
	v.logScreen("SCREEN AT THE END", s, nil)
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)
	recs := e.src.requests()
	crit(e, "QR5", "at most 10 requests in any 10 minutes from the first refused answer, all instances", func(c *ck) {
		var after []time.Time
		inflight := 0
		for _, q := range recs {
			at := time.UnixMilli(q.UnixMS)
			if at.After(T) {
				after = append(after, at)
			} else if q.AnsweredMS >= T.UnixMilli() || q.Status == 429 {
				inflight++
			}
		}
		c.notef("%d print --refresh calls; %d requests before the first refusal was written (in flight then or earlier); %d after it: %s",
			runs, inflight, len(after), fmtArrivals(after, T))
		for i := range after {
			n := 0
			for j := i; j < len(after) && after[j].Sub(after[i]) < 10*time.Minute; j++ {
				n++
			}
			if n > 10 {
				c.failf("%d requests in the 10 minutes from +%s", n, after[i].Sub(T).Round(time.Second))
				break
			}
		}
	})
	crit(e, "S7.5", "after the second refusal, the next attempt announced is 10 minutes later (scaled view of S7.5)", func(c *ck) {
		at, n := bursts(r.reqs, T.Add(time.Second))
		if len(at) == 0 {
			if d > 5*time.Minute+10*time.Second {
				c.failf("no attempt after the first refusal in %s", d)
			}
			return
		}
		c.notef("second attempt %s after the first refusal (%d requests)", at[0].Sub(T).Round(10*time.Millisecond), n[0])
		if g := at[0].Sub(T); g < 5*time.Minute-time.Second || g > 5*time.Minute+5*time.Second {
			c.failf("second attempt %s after the first refusal, want 5 min", g.Round(10*time.Millisecond))
		}
		el := errorLines(s)
		c.notef("error line at the end %q, status %q", el, statusLine(s))
		if len(at) >= 2 {
			g := at[1].Sub(at[0])
			c.notef("third attempt %s after the second (%d requests)", g.Round(10*time.Millisecond), n[1])
			if g < 10*time.Minute-time.Second || g > 10*time.Minute+5*time.Second {
				c.failf("third attempt %s after the second, want 10 min", g.Round(10*time.Millisecond))
			}
			return
		}
		if len(el) != 1 {
			c.failf("want one error line at the end, got %q", el)
		} else if _, ok := hhmmIn(el[0], at[0].Add(9*time.Minute), at[0].Add(11*time.Minute)); !ok {
			c.failf("want the next attempt at about %s (10 min after the second), got %q", at[0].Add(10*time.Minute).UTC().Format("15:04"), el[0])
		}
	})
}

// QR5 scaled: 5 min 20 s from the first refusal (the first wait ends and is
// refused again); the full requirement is TestVerifySlow_View_QR5_SixteenMinutes.
func TestVerifyView_QR5_S7_5_Scaled_BlockWithViewAndPrint(t *testing.T) {
	t.Parallel()
	qr5(t, 5*time.Minute+20*time.Second)
}

func TestVerifySlow_View_QR5_SixteenMinutes(t *testing.T) {
	needSlow(t, "about 16 minutes 30 s of real time")
	t.Parallel()
	qr5(t, 16*time.Minute+30*time.Second)
}
