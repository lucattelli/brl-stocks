//go:build verification && linux

// The full-screen view, increment 2 of slice 2: the summary line, groups and
// order (S2.1 to S2.3, S2.5 to S2.8, S4.1 to S4.4, S9.1), through the real view
// on a pseudo-terminal, with the helpers of view_verify_test.go. QR1 (20
// tickers) for the group checks. See README.md.
package verification

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// viewBody returns the non-blank lines between the column header (TICKER) and
// the status line (the last non-blank line of the screen).
func viewBody(s screen) []string {
	hdr, last := -1, -1
	for i, l := range s.lines {
		if f := strings.Fields(l); len(f) > 0 && f[0] == "TICKER" && hdr < 0 {
			hdr = i
		}
		if strings.TrimSpace(l) != "" {
			last = i
		}
	}
	if hdr < 0 {
		return nil
	}
	var out []string
	for i := hdr + 1; i < last; i++ {
		if strings.TrimSpace(s.lines[i]) != "" {
			out = append(out, strings.TrimSpace(s.lines[i]))
		}
	}
	return out
}

// firstText returns the first non-blank line of the screen.
func firstText(s screen) string {
	for _, l := range s.lines {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}

// openSettled opens the view, waits until every ticker has a row with a
// price and the screen has settled, logs it and quits with q.
func (e *env) openSettled(tks []string, extra func(s screen) bool) (screen, viewResult) {
	e.t.Helper()
	v := e.openView(nil)
	cond := rowsShown(tks)
	if extra != nil {
		cond = func(s screen) bool { return rowsShown(tks)(s) && extra(s) }
	}
	s, _ := v.waitStable("every row with a price", 10*time.Second, 300*time.Millisecond, cond)
	v.logScreen("SCREEN BEFORE QUITTING", s, nil)
	r := v.quit([]byte("q"), "q")
	e.wantExit(result{exit: r.exit}, 0)
	return s, r
}

var (
	ibovRE = regexp.MustCompile(`IBOV (\S+) (\S+%)`)
	spxRE  = regexp.MustCompile(`S&P 500 (\S+) (\S+%)`)
	fxRE   = regexp.MustCompile(`USD/BRL (\S+) (\S+%)`)
)

// S2.1 [view, print] Given a config without a `summary` key, when the view is
// drawn, then a line above the watchlist shows Ibovespa, S&P 500 and USD/BRL,
// each with its value and day change percent.
// S2.5 ... an Ibovespa value of 158432.10 reads `158.432`.
// S2.6 ... an S&P 500 value of 6812.4 reads `6,812.40`.
// S2.7 ... a USD/BRL rate of 5.4321 reads `5,4321`.
// S2.8 ... an Ibovespa day change of +0.32 percent reads `+0,32%`.
// Stand-in: ^BVSP 158432.10 (previous close 157926.70), ^GSPC 6812.40, BRL=X 5.4321.
func TestVerifyView_S2_1_5_8_DefaultSummaryLine(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S2.1, S2.5 to S2.8 (view)", "tickers = [\"PETR4\", \"AAPL\"]\n")
	s, _ := e.openSettled([]string{"PETR4", "AAPL"}, func(s screen) bool {
		f := firstText(s)
		return ibovRE.MatchString(f) && spxRE.MatchString(f) && fxRE.MatchString(f)
	})
	first := firstText(s)
	hdr, firstAt := -1, -1
	for i, l := range s.lines {
		if firstAt < 0 && strings.TrimSpace(l) != "" {
			firstAt = i
		}
		if strings.HasPrefix(strings.TrimSpace(l), "TICKER") {
			hdr = i
			break
		}
	}
	crit(e, "S2.1", "a line above the watchlist shows IBOV, S&P 500 and USD/BRL with value and percent", func(c *ck) {
		for _, re := range []*regexp.Regexp{ibovRE, spxRE, fxRE} {
			if !re.MatchString(first) {
				c.failf("the first line lacks %s: %q", re, first)
			}
		}
		if hdr < 0 || firstAt >= hdr {
			c.failf("the summary line is not above the column header")
		}
		c.notef("summary line %q", first)
	})
	sub := func(id string, re *regexp.Regexp, i int, want, text string) {
		crit(e, id, text, func(c *ck) {
			m := re.FindStringSubmatch(first)
			if m == nil || m[i] != want {
				c.failf("want `%s`, got %q in %q", want, m, first)
			}
		})
	}
	sub("S2.5", ibovRE, 1, "158.432", "the Ibovespa value reads `158.432`")
	sub("S2.6", spxRE, 1, "6,812.40", "the S&P 500 value reads `6,812.40`")
	sub("S2.7", fxRE, 1, "5,4321", "the USD/BRL value reads `5,4321`")
	sub("S2.8", ibovRE, 2, "+0,32%", "the Ibovespa percent reads `+0,32%`")
}

// S2.2 [view, print] Given `summary = ["USDBRL"]`, when the view is drawn,
// then the summary line shows only USD/BRL.
func TestVerifyView_S2_2_SummaryOnlyUSDBRL(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S2.2 (view)", "summary = [\"USDBRL\"]\ntickers = [\"PETR4\", \"AAPL\"]\n")
	s, _ := e.openSettled([]string{"PETR4", "AAPL"}, func(s screen) bool { return fxRE.MatchString(firstText(s)) })
	crit(e, "S2.2", "summary = [\"USDBRL\"] shows only USD/BRL", func(c *ck) {
		first := firstText(s)
		if !regexp.MustCompile(`^USD/BRL \S+ \S+%\s*$`).MatchString(first) {
			c.failf("want the first line to be the USD/BRL entry alone, got %q", first)
		}
		if s.contains("IBOV") || s.contains("S&P 500") {
			c.failf("IBOV or S&P 500 appears on the screen")
		}
	})
}

// S2.3 [view, print] Given `summary = []`, when the view is drawn, then there
// is no summary line.
func TestVerifyView_S2_3_EmptySummaryNoLine(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S2.3 (view)", "summary = []\ntickers = [\"PETR4\", \"AAPL\"]\n")
	s, r := e.openSettled([]string{"PETR4", "AAPL"}, nil)
	crit(e, "S2.3", "summary = [] shows no summary line", func(c *ck) {
		for _, w := range []string{"IBOV", "S&P 500", "USD/BRL"} {
			if s.contains(w) {
				c.failf("%s appears on the screen", w)
			}
		}
		if f := firstText(s); !strings.HasPrefix(strings.TrimSpace(f), "TICKER") {
			c.failf("want the column header as the first line, got %q", f)
		}
		c.notef("requests during the run: %s", fmtCounts(bySymbol(r.reqs)))
	})
}

// S4.1 [view, print] Given a config with the groups "Brasil" and "US" in that
// order, when the view is drawn, then each name appears as a header above its
// rows, in that order. S4.3 Given tickers outside groups and a group, ... the
// tickers outside groups come first, with no header. S9.1 Given the view has
// just started, when it is drawn, then rows are in the order of the config
// file (an order chosen to differ from both other sort orders). QR1.
func TestVerifyView_S4_1_3_S9_1_GroupsAndOrder(t *testing.T) {
	t.Parallel()
	base := cfgSpec{top: []string{"MSFT", "PETR4"}, groups: []groupSpec{{"Brasil", []string{"VALE3", "ITUB4"}}, {"US", []string{"AAPL"}}}}
	sizes(t, base, func(t *testing.T, cfg cfgSpec) {
		e := newEnv(t, "S4.1, S4.3, S9.1 (view; QR1 with 20 tickers)", cfg.String())
		s, _ := e.openSettled(cfg.all(), nil)
		body := viewBody(s)
		idx := func(pred func(string) bool) int {
			for i, l := range body {
				if pred(l) {
					return i
				}
			}
			return -1
		}
		header := func(name string) int { return idx(func(l string) bool { return l == name }) }
		rowAt := func(tk string) int {
			return idx(func(l string) bool { f := strings.Fields(l); return len(f) > 0 && f[0] == tk })
		}
		crit(e, "S4.1", "group names Brasil then US, each a header above its rows, in file order", func(c *ck) {
			hb, hu := header("Brasil"), header("US")
			if hb < 0 || hu < 0 {
				c.failf("want the lines `Brasil` and `US` on their own, got body %q", body)
				return
			}
			if hb >= hu {
				c.failf("want Brasil above US (file order), got lines %d and %d", hb, hu)
			}
			for gi, g := range cfg.groups {
				h := header(g.name)
				next := len(body)
				if gi+1 < len(cfg.groups) {
					next = header(cfg.groups[gi+1].name)
				}
				for _, tk := range g.tickers {
					if i := rowAt(tk); !(i > h && i < next) {
						c.failf("row %s (group %s) at line %d is not between its header (%d) and the next (%d)", tk, g.name, i, h, next)
					}
				}
			}
		})
		crit(e, "S4.3", "tickers outside groups come first, with no header", func(c *ck) {
			for i := 0; i < len(cfg.top); i++ {
				if i >= len(body) {
					c.failf("body too short: %q", body)
					return
				}
				if f := strings.Fields(body[i]); len(f) == 0 || f[0] != cfg.top[i] {
					c.failf("want the top-level rows right below the column header, with no header; line %d is %q", i+1, body[i])
				}
			}
		})
		crit(e, "S9.1", "rows in the order of the config file", func(c *ck) {
			want := cfg.all()
			wantSet := map[string]bool{}
			for _, w := range want {
				wantSet[w] = true
			}
			var got []string
			for _, l := range body {
				if f := strings.Fields(l); len(f) > 0 && wantSet[f[0]] {
					got = append(got, f[0])
				}
			}
			if strings.Join(got, " ") != strings.Join(want, " ") {
				c.failf("want %v, got %v", want, got)
			}
			if !strings.Contains(s.text(), "sort: file order") {
				c.notef("the status line does not say `sort: file order`")
			}
		})
	})
}

// S4.1 again, with the groups in the other order in the file: US, then Brasil.
func TestVerifyView_S4_1_GroupOrderFollowsFile(t *testing.T) {
	t.Parallel()
	cfg := cfgSpec{groups: []groupSpec{{"US", []string{"AAPL"}}, {"Brasil", []string{"VALE3"}}}}
	e := newEnv(t, "S4.1 (view; groups US then Brasil in the file)", cfg.String())
	s, _ := e.openSettled(cfg.all(), nil)
	crit(e, "S4.1", "groups appear in file order (US above Brasil here)", func(c *ck) {
		var got []string
		for _, l := range viewBody(s) {
			got = append(got, strings.Fields(l)[0])
		}
		if want := "US AAPL Brasil VALE3"; strings.Join(got, " ") != want {
			c.failf("want the lines to start with %q, got %q", want, got)
		}
	})
}

// S4.2 [view, print] Given a config with no group, when the view is drawn,
// then no group header appears. QR1.
func TestVerifyView_S4_2_NoGroupNoHeader(t *testing.T) {
	t.Parallel()
	base := cfgSpec{top: []string{"PETR4", "AAPL", "VALE3"}}
	sizes(t, base, func(t *testing.T, cfg cfgSpec) {
		e := newEnv(t, "S4.2 (view; QR1 with 20 tickers)", cfg.String())
		s, _ := e.openSettled(cfg.all(), nil)
		crit(e, "S4.2", "no group header", func(c *ck) {
			known := map[string]bool{}
			for _, tk := range cfg.all() {
				known[tk] = true
			}
			n := 0
			for _, l := range viewBody(s) {
				if !known[strings.Fields(l)[0]] {
					c.failf("a line between the column header and the status line that is not a row: %q", l)
				} else {
					n++
				}
			}
			if n != len(cfg.all()) {
				c.failf("want %d rows, got %d", len(cfg.all()), n)
			}
		})
	})
}

// S4.4 [view, print] Given the same ticker in two groups, when a refresh runs,
// then the ticker is requested from the source once. In this slice the view's
// refresh is its fetch at start (empty cache). 7.2: shown in both groups. QR1.
func TestVerifyView_S4_4_TickerInTwoGroupsFetchedOnce(t *testing.T) {
	t.Parallel()
	base := cfgSpec{groups: []groupSpec{{"A", []string{"PETR4", "AAPL"}}, {"B", []string{"VALE3", "PETR4"}}}}
	sizes(t, base, func(t *testing.T, cfg cfgSpec) {
		e := newEnv(t, "S4.4 (view; QR1 with 20 tickers)", cfg.String())
		v := e.openView(nil)
		others := []string{}
		for _, tk := range cfg.all() {
			if tk != "PETR4" {
				others = append(others, tk)
			}
		}
		s, _ := v.waitStable("PETR4 twice and every other row with a price", 10*time.Second, 300*time.Millisecond, func(s screen) bool {
			ls := s.rowLines("PETR4")
			return len(ls) == 2 && hasQuote(ls[0]) && hasQuote(ls[1]) && rowsShown(others)(s)
		})
		v.logScreen("SCREEN BEFORE QUITTING", s, nil)
		r := v.quit([]byte("q"), "q")
		crit(e, "S4.4", "a ticker in two groups is requested once", func(c *ck) {
			if n := bySymbol(r.reqs)["PETR4.SA"]; n != 1 {
				c.failf("want 1 request for PETR4.SA during the view run, the stand-in received %d (%s)", n, fmtCounts(bySymbol(r.reqs)))
			}
			if ls := s.rowLines("PETR4"); len(ls) != 2 {
				c.failf("7.2: want PETR4 shown in both groups, got %q", ls)
			}
			c.notef("requests: %s", fmtCounts(bySymbol(r.reqs)))
		})
	})
}
