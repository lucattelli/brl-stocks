//go:build verification && linux

// The full-screen view, increment 3 of slice 2: market states, stale rows, the
// error line and not-found rows (S5.4, S5.5, S5.6, S5.12, S6.1, S6.2, S6.3,
// S6.7 to S6.11, S8.1, S8.2, S8.5), through the real view on a pseudo-terminal
// with the helpers of view_verify_test.go. QR1 (20 tickers) for the failure
// checks. In this slice the view fetches once when it opens, so "a refresh
// fails" is that fetch at start failing. See README.md.
package verification

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// statusLine is the last line of the screen with text.
func statusLine(s screen) string {
	for i := len(s.lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(s.lines[i]) != "" {
			return strings.TrimSpace(s.lines[i])
		}
	}
	return ""
}

// errorLines are the screen lines that start with one of the four causes.
func errorLines(s screen) []string { return causeLines(s.text()) }

// viewAndQuit opens the view, waits for cond (and a settled screen), logs the
// screen and quits with q. It returns the screen and when the run started.
func (e *env) viewAndQuit(what string, cond func(s screen) bool) (screen, viewResult, time.Time) {
	e.t.Helper()
	v := e.openView(nil)
	s, _ := v.waitStable(what, 10*time.Second, 300*time.Millisecond, cond)
	v.logScreen("SCREEN BEFORE QUITTING", s, nil)
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)
	return s, r, v.start
}

func staleMark(l string) (string, int, string, bool) {
	m := staleRE.FindStringSubmatch(l)
	if m == nil {
		return "", 0, "", false
	}
	n, _ := strconv.Atoi(m[1])
	return m[0], n, m[2], true
}

// ---------------------------------------------------------------------------
// S5 Closed markets
// ---------------------------------------------------------------------------

// S5.4 [view, print] Given the US market is closed, when the view is drawn,
// then US rows show the last close. S5.5 Given the US market is closed and B3
// is open, when the view is drawn, then the status line reads
// `B3 open · US closed`. The stand-in's US market closed 2 h ago; its last
// price is the close.
func TestVerifyView_S5_4_5_USClosed(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S5.4, S5.5 (view)", baseConfig)
	e.setSource(state{ClosedMarkets: []string{"us"}, ClosedAgoSeconds: 7200}, "US closed 2 h ago")
	closeHHMM := time.Unix(time.Now().Unix()-7200, 0).UTC().Format("15:04")
	s, _, _ := e.viewAndQuit("every row with a price and the market states", func(s screen) bool {
		return rowsShown(tickers)(s) && strings.Contains(statusLine(s), "US closed")
	})
	crit(e, "S5.4", "US rows show the last close", func(c *ck) {
		for tk, want := range map[string]string{"AAPL": "US$ 229.87", "MSFT": "US$ 512.30"} {
			p, _, ok := c.vRow(s, tk)
			if !ok {
				continue
			}
			if p.price != want {
				c.failf("row %s: want the close %q, got %q", tk, want, p.price)
			}
			cl := time.Now().Add(-7200 * time.Second)
			if _, ok := hhmmIn(p.rest[4], cl.Add(-2*time.Minute), cl.Add(time.Minute)); !ok {
				c.failf("row %s: want the quote time of the close (about %s), got %q", tk, closeHHMM, p.rest[4])
			}
			if m, _, _, ok := staleMark(s.rowLines(tk)[0]); ok {
				c.failf("row %s: a closed market's row carries a stale mark %q", tk, m)
			}
		}
	})
	crit(e, "S5.5", "the status line reads `B3 open · US closed`", func(c *ck) {
		if st := statusLine(s); !strings.HasPrefix(st, "B3 open · US closed") {
			c.failf("status line %q", st)
		} else {
			c.notef("status line %q", st)
		}
	})
}

// S5.6 [view, print] Given a closed market whose rows were last fetched, with
// success, two days ago, when the view is drawn, then those rows carry no
// stale mark.
// S6.7 [view] Given an open market and a row whose last successful fetch is
// older than two refresh intervals with no failed fetch since, when the view
// is drawn, then the row is marked stale.
// S6.10 (scaled) Given a stale row whose last successful fetch was N ago, the
// mark reads its age.
// One run: `print` leaves the cache (US closed 2 h ago, B3 open, interval
// 10 s); 25 s later (more than two intervals) the source holds its answers and
// the view starts. Scaled: "two days" is 25 s for the last successful fetch
// (the US quote itself is from 2 h ago), "12 minutes" is 25 s.
func TestVerifyView_S5_6_S6_7_S6_10_CachedAges(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S5.6, S6.7, S6.10 scaled (view)", baseConfig)
	e.setSource(state{ClosedMarkets: []string{"us"}, ClosedAgoSeconds: 7200}, "US closed 2 h ago")
	e.wantExit(e.run("print"), 0)
	okAt := time.Now()
	e.sleep(25*time.Second, "more than two intervals (20 s) after the last success")
	e.setSource(state{ClosedMarkets: []string{"us"}, ClosedAgoSeconds: 7200, DelayMS: 20000}, "answers held 20 s")
	b3 := []string{"PETR4", "VALE3", "ITUB4"}
	s, r, _ := e.viewAndQuit("the cached rows, B3 rows marked stale", func(s screen) bool {
		if !rowsShown(tickers)(s) {
			return false
		}
		for _, tk := range b3 {
			if _, _, _, ok := staleMark(s.rowLines(tk)[0]); !ok {
				return false
			}
		}
		return true
	})
	age := int(s.at.Sub(okAt).Seconds())
	crit(e, "S5.6", "rows of a closed market last fetched with success long ago carry no stale mark", func(c *ck) {
		for _, tk := range []string{"AAPL", "MSFT"} {
			if ls := s.rowLines(tk); len(ls) == 1 {
				if m, _, _, ok := staleMark(ls[0]); ok {
					c.failf("row %s (US closed): stale mark %q in %q", tk, m, ls[0])
				}
			}
		}
	})
	crit(e, "S6.7", "an open market's row whose last success is older than two intervals, with no failure since, is stale", func(c *ck) {
		for _, tk := range b3 {
			if ls := s.rowLines(tk); len(ls) != 1 {
				c.failf("row %s missing", tk)
			} else if _, _, _, ok := staleMark(ls[0]); !ok {
				c.failf("row %s: want a stale mark, got %q", tk, ls[0])
			}
		}
		answered := 0
		for _, q := range r.reqs {
			if q.Status != 0 && q.Status != -1 {
				answered++
			}
		}
		c.notef("answers that arrived during the run: %d (none expected)", answered)
	})
	crit(e, "S6.10", fmt.Sprintf("scaled: the mark reads the age since the last success (about %d s)", age), func(c *ck) {
		for _, tk := range b3 {
			if ls := s.rowLines(tk); len(ls) == 1 {
				m, n, unit, ok := staleMark(ls[0])
				if !ok || unit != "s" || n < age-2 || n > age+1 {
					c.failf("row %s: want `!%ds` (±2 s), got %q", tk, age, m)
				} else {
					c.notef("row %s: %q", tk, m)
				}
			}
		}
	})
}

// S6.10 [view, print] literally: a stale row whose last successful fetch was
// 12 minutes ago reads `!12m`. SLOW (about 13 min): the view starts 12 min 20 s
// after the last success, the source holding its answers.
func TestVerifySlow_View_S6_10_TwelveMinutes(t *testing.T) {
	needSlow(t, "S6.10 on the view, literally: 12 minutes")
	t.Parallel()
	e := newEnv(t, "S6.10 (view, literal: 12 minutes)", defaultIntervalConfig)
	e.wantExit(e.run("print"), 0)
	e.sleep(12*time.Minute+20*time.Second, "12 minutes after the last success")
	e.setSource(state{DelayMS: 20000}, "answers held 20 s")
	s, _, _ := e.viewAndQuit("the cached rows marked stale", func(s screen) bool {
		return rowsShown(tickers)(s) && strings.Contains(s.text(), "!12m")
	})
	crit(e, "S6.10", "the mark reads `!12m`", func(c *ck) {
		for _, tk := range tickers {
			if ls := s.rowLines(tk); len(ls) != 1 || !strings.Contains(ls[0], "!12m") {
				c.failf("row %s: want `!12m`, got %q", tk, ls)
			}
		}
	})
}

// S5.12 [view, print] Given a row whose last fetch failed before its market
// closed, when the view is drawn after the close, then the row is still marked
// stale. Stand-in control `open_end_unix`: the session of the open markets
// ends 22 s after the first call. 0 s: `print` fills the cache; +12 s: the
// source answers 503 and a view run's fetch fails; +27 s (after the close):
// the source holds its answers and the view is drawn.
func TestVerifyView_S5_12_FailedBeforeCloseStaysStale(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S5.12 (view)", baseConfig)
	t0 := time.Now()
	end := t0.Add(22 * time.Second).Unix()
	e.setSource2(state2{OpenEndUnix: end}, "open session ends at +22 s")
	e.wantExit(e.run("print"), 0)
	time.Sleep(time.Until(t0.Add(12 * time.Second)))
	e.setSource2(state2{state: state{Status: 503}, OpenEndUnix: end}, "every request answered 503")
	v := e.openView(nil)
	s1, _ := v.waitStable("the failure: an error line", 10*time.Second, 300*time.Millisecond, func(s screen) bool {
		return rowsShown(tickers)(s) && len(errorLines(s)) == 1
	})
	v.logScreen("BEFORE THE CLOSE, SCREEN AFTER THE FAILED FETCH", s1, nil)
	v.quit([]byte("q"), "q")
	time.Sleep(time.Until(t0.Add(27 * time.Second)))
	e.setSource2(state2{state: state{DelayMS: 20000}, OpenEndUnix: end}, "after the close: answers held 20 s")
	s, _, _ := e.viewAndQuit("the rows after the close", func(s screen) bool {
		return rowsShown(tickers)(s) && strings.Contains(statusLine(s), "B3 closed")
	})
	crit(e, "S5.12", "a row whose last fetch failed before the close is still stale after it", func(c *ck) {
		if st := statusLine(s); !strings.HasPrefix(st, "B3 closed · US closed") {
			c.failf("set-up: want the markets closed, status line %q", st)
		}
		for _, tk := range tickers {
			if ls := s.rowLines(tk); len(ls) != 1 {
				c.failf("row %s missing", tk)
			} else if _, _, _, ok := staleMark(ls[0]); !ok {
				c.failf("row %s: want a stale mark after the close, got %q", tk, ls[0])
			}
		}
	})
}

// ---------------------------------------------------------------------------
// S6 Failed fetches
// ---------------------------------------------------------------------------

// S6.1 [view, print] Given rows with quotes and an open market, when a refresh
// fails, then the rows still show their last known prices.
// S6.2 Given a row whose last fetch failed, when the view is drawn, then the
// row is marked stale with its age.
// S6.3 Given a refresh in which five rows failed, when the view is drawn, then
// exactly one error line is shown.
// S6.9 Given a failed refresh, when the error line is drawn, then it names the
// cause, the number of rows not updated and the time of the next attempt.
// Scaled: interval 10 s, cache 12 s old; the view's fetch at start fails
// (status 503, or no connection). QR1.
func TestVerifyView_S6_1_2_3_9_FailedFetch(t *testing.T) {
	t.Parallel()
	base := cfgSpec{interval: 10, top: []string{"PETR4", "AAPL"}, groups: []groupSpec{{"Brasil", []string{"VALE3", "ITUB4"}}, {"US", []string{"MSFT"}}}}
	sizes(t, base, func(t *testing.T, cfg cfgSpec) {
		for _, variant := range unreachableVariants {
			variant := variant
			t.Run(strings.ReplaceAll(variant, " ", "_"), func(t *testing.T) {
				t.Parallel()
				e := newEnv(t, "S6.1, S6.2, S6.3, S6.9 (view) ["+variant+"] (QR1 with 20 tickers)", cfg.String())
				r1 := e.run("print")
				e.wantExit(r1, 0)
				okAt := time.Now()
				before := map[string]string{}
				for _, tk := range cfg.all() {
					if p, ok := parseRow(row(e, r1.stdout, tk), tk); ok {
						before[tk] = p.price
					}
				}
				if variant == "status 503" {
					e.setSource(state{Status: 503}, "every request answered 503")
				} else {
					e.logf("\nSTAND-IN   stopped: its address now refuses connections\n")
					e.src.stop()
				}
				e.sleep(pastInterval, "cache older than the interval")
				s, _, start := e.viewAndQuit("the error line", func(s screen) bool {
					return rowsShown(cfg.all())(s) && len(errorLines(s)) > 0
				})
				n := len(cfg.all())
				age := int(s.at.Sub(okAt).Seconds())
				crit(e, "S6.1", "the rows still show their last known prices", func(c *ck) {
					for _, tk := range cfg.all() {
						if p, _, ok := c.vRow(s, tk); ok && p.price != before[tk] {
							c.failf("row %s: want the last known price %q, got %q", tk, before[tk], p.price)
						}
					}
				})
				crit(e, "S6.2", "a row whose last fetch failed is marked stale with its age", func(c *ck) {
					for _, tk := range cfg.all() {
						ls := s.rowLines(tk)
						if len(ls) != 1 {
							c.failf("row %s missing", tk)
							continue
						}
						m, a, unit, ok := staleMark(ls[0])
						if !ok || unit != "s" || a < age-2 || a > age+1 {
							c.failf("row %s: the last success was about %d s ago, want `!%ds`, got %q in %q", tk, age, age, m, ls[0])
						}
					}
				})
				crit(e, "S6.3", fmt.Sprintf("%d rows failed: exactly one error line", n), func(c *ck) {
					if el := errorLines(s); len(el) != 1 {
						c.failf("want 1 error line, got %d: %q", len(el), el)
					}
				})
				crit(e, "S6.9", "the error line names the cause, the number of rows not updated and the time of the next attempt", func(c *ck) {
					el := errorLines(s)
					if len(el) == 0 {
						c.failf("no error line on the screen")
						return
					}
					l := strings.TrimSpace(el[0])
					c.notef("error line %q", l)
					if !strings.HasPrefix(l, "source unreachable") {
						c.failf("cause: want `source unreachable`, got %q", l)
					}
					found := false
					for _, x := range countIn(l) {
						if x == n {
							found = true
						}
					}
					if !found {
						c.failf("want the number of rows not updated (%d) in the line, found the numbers %v", n, countIn(l))
					}
					if hm, ok := hhmmIn(l, start, s.at.Add(10*time.Second)); !ok {
						c.failf("want the next attempt as HH:MM between %s and %s (one interval after the failure), found %q",
							start.UTC().Format("15:04"), s.at.Add(10*time.Second).UTC().Format("15:04"), hm)
					}
				})
			})
		}
	})
}

// S6.8 [view, print] Given a refresh in which one row fails and the others
// succeed, when the view is drawn, then only that row is marked stale.
// Cache 12 s old; VALE3 answered 503, the rest 200. QR1.
func TestVerifyView_S6_8_OnlyFailedRowStale(t *testing.T) {
	t.Parallel()
	base := cfgSpec{interval: 10, top: []string{"PETR4", "AAPL"}, groups: []groupSpec{{"Brasil", []string{"VALE3", "ITUB4"}}, {"US", []string{"MSFT"}}}}
	sizes(t, base, func(t *testing.T, cfg cfgSpec) {
		e := newEnv(t, "S6.8 (view; QR1 with 20 tickers)", cfg.String())
		e.wantExit(e.run("print"), 0)
		e.sleep(pastInterval, "cache older than the interval")
		e.setSource(state{SymbolStatus: map[string]int{"VALE3.SA": 503}}, "VALE3.SA answered 503")
		s, r, _ := e.viewAndQuit("VALE3 marked stale", func(s screen) bool {
			if !rowsShown(cfg.all())(s) {
				return false
			}
			_, _, _, ok := staleMark(s.rowLines("VALE3")[0])
			return ok && len(errorLines(s)) > 0
		})
		crit(e, "S6.8", "only the row that failed is marked stale", func(c *ck) {
			for _, tk := range cfg.all() {
				ls := s.rowLines(tk)
				if len(ls) != 1 {
					c.failf("row %s missing", tk)
					continue
				}
				m, _, _, ok := staleMark(ls[0])
				if tk == "VALE3" && !ok {
					c.failf("VALE3 (failed): want a stale mark, got %q", ls[0])
				}
				if tk != "VALE3" && ok {
					c.failf("row %s (fetched): stale mark %q", tk, m)
				}
			}
			c.notef("error line %q; VALE3.SA requests %d", errorLines(s), bySymbol(r.reqs)["VALE3.SA"])
		})
	})
}

// S6.11 [view, print] Given every row updates and a summary entry fails, when
// the view is drawn, then the error line says the summary was not updated.
// 7.3: `summary not updated` instead of a number. Empty cache, ^GSPC 503.
func TestVerifyView_S6_11_SummaryEntryFails(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S6.11 (view)", baseConfig)
	e.setSource(state{SymbolStatus: map[string]int{"^GSPC": 503}}, "^GSPC answered 503")
	s, _, _ := e.viewAndQuit("the rows and an error line", func(s screen) bool {
		return rowsShown(tickers)(s) && len(errorLines(s)) > 0
	})
	crit(e, "S6.11", "the error line says `summary not updated`, with no number", func(c *ck) {
		el := errorLines(s)
		if len(el) != 1 {
			c.failf("want one error line, got %q", el)
			return
		}
		l := strings.TrimSpace(el[0])
		c.notef("error line %q", l)
		if !strings.HasPrefix(l, "source unreachable") || !strings.Contains(l, "summary not updated") {
			c.failf("want `source unreachable` and `summary not updated`, got %q", l)
		}
		if nums := countIn(l); len(nums) != 0 {
			c.failf("want no count in the line, found %v", nums)
		}
	})
}

// ---------------------------------------------------------------------------
// S8 Not-found rows
// ---------------------------------------------------------------------------

// S8.1 [view, print] Given the ticker `XXXX9`, which the source does not know,
// when the view is drawn, then its row shows "not found" in place of the
// price. S8.2 Given one not-found row among others, when a refresh runs, then
// the other rows show fresh quotes. S8.5 Given a not-found row and no other
// problem, when the view is drawn, then there is no error line. The stand-in
// answers 404 for XXXX9.SA. QR1.
func TestVerifyView_S8_1_2_5_NotFound(t *testing.T) {
	t.Parallel()
	base := cfgSpec{top: []string{"PETR4", "XXXX9", "AAPL"}}
	sizes(t, base, func(t *testing.T, cfg cfgSpec) {
		e := newEnv(t, "S8.1, S8.2, S8.5 (view; QR1 with 20 tickers)", cfg.String())
		e.setSource(state{SymbolStatus: map[string]int{"XXXX9.SA": 404}}, "XXXX9.SA answered 404")
		var others []string
		for _, tk := range cfg.all() {
			if tk != "XXXX9" {
				others = append(others, tk)
			}
		}
		s, _, _ := e.viewAndQuit("the rows and XXXX9 not found", func(s screen) bool {
			ls := s.rowLines("XXXX9")
			return rowsShown(others)(s) && len(ls) == 1 && strings.Contains(ls[0], "not found")
		})
		crit(e, "S8.1", "XXXX9 shows `not found` in place of the price", func(c *ck) {
			ls := s.rowLines("XXXX9")
			if len(ls) != 1 {
				c.failf("want one row XXXX9, got %q", ls)
				return
			}
			l := ls[0]
			if !strings.Contains(l, "not found") || hasQuote(l) {
				c.failf("want `not found` and no price, got %q", l)
			}
			// 7.3: `—` in the other data columns.
			rest := strings.Fields(strings.Replace(l, "not found", "", 1))[1:]
			if n := dashCount(rest); n != len(rest) || n < 6 {
				c.notef("other data columns: %q (7.3 wants `—` in each)", rest)
			}
			c.notef("%q", l)
		})
		crit(e, "S8.2", "the other rows show fresh quotes", func(c *ck) {
			for _, tk := range others {
				ls := s.rowLines(tk)
				if len(ls) != 1 || !hasQuote(ls[0]) {
					c.failf("row %s: want a quote, got %q", tk, ls)
				} else if m, _, _, ok := staleMark(ls[0]); ok {
					c.failf("row %s: stale mark %q", tk, m)
				}
			}
		})
		crit(e, "S8.5", "no error line", func(c *ck) {
			if el := errorLines(s); len(el) != 0 {
				c.failf("error line(s) shown: %q", el)
			}
			for _, l := range viewBody(s) {
				lo := strings.ToLower(l)
				if strings.HasPrefix(lo, "source ") || strings.Contains(lo, "not updated") {
					c.failf("a line that reads as an error: %q", l)
				}
			}
		})
	})
}
