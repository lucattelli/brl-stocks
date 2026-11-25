//go:build verification && linux

// Live refresh, increment 4 of slice 3 (research/build/live-refresh/scope.md):
// the config file changes while the real view is open, in real time. S8.4,
// S11.3, S11.4, S11.6, S11.10, S11.11, S11.14, S11.19, S11.20; 7.3's order of
// problems on the error line (config problem before failed fetches); and,
// outside the criteria, an editor-style save (a temporary file renamed over
// the config), two saves within a second, an emptied file, a deleted file
// that comes back, and a config grown to 20 tickers. The file is edited in the
// check's temporary folders; each change is timed from the write to the
// screen. The views run at 200 columns so that the long temporary paths fit
// on the error line. See README.md.
package verification

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const cfgCols = 200

// otherLines are the lines between the column header and the status line
// that are neither a row of a known ticker nor a known group header: the
// error line, if any.
func otherLines(s screen, known []string, groups []string) []string {
	k := map[string]bool{}
	for _, x := range known {
		k[x] = true
	}
	g := map[string]bool{}
	for _, x := range groups {
		g[x] = true
	}
	var out []string
	for _, l := range viewBody(s) {
		f := strings.Fields(l)
		if len(f) == 0 || g[strings.TrimSpace(l)] || (k[f[0]] && len(f) > 1) {
			continue
		}
		out = append(out, l)
	}
	return out
}

// saveCfg writes the config (in place, as `>` would) and keeps the view's QR7
// baseline in step, since the check itself changes the file.
func (v *view) saveCfg(path, content, what string) time.Time {
	v.e.t.Helper()
	at := time.Now()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		v.e.t.Fatal(err)
	}
	v.sumBefore = fileSum(v.e.cfgPath)
	v.e.logf("SAVE       %s at +%s\n%s\n", what, at.Sub(v.start).Round(time.Millisecond), indent(content))
	return at
}

// renameCfg writes a temporary file beside the config and renames it over the
// config, as many editors save.
func (v *view) renameCfg(path, content, what string) time.Time {
	v.e.t.Helper()
	tmp := filepath.Join(filepath.Dir(path), ".config.toml.swp")
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		v.e.t.Fatal(err)
	}
	at := time.Now()
	if err := os.Rename(tmp, path); err != nil {
		v.e.t.Fatal(err)
	}
	v.sumBefore = fileSum(v.e.cfgPath)
	v.e.logf("SAVE       %s (rename over the config) at +%s\n%s\n", what, at.Sub(v.start).Round(time.Millisecond), indent(content))
	return at
}

func (v *view) removeCfg(path string) time.Time {
	v.e.t.Helper()
	at := time.Now()
	if err := os.Remove(path); err != nil {
		v.e.t.Fatal(err)
	}
	v.sumBefore = fileSum(v.e.cfgPath)
	v.e.logf("REMOVE     the config at +%s\n", at.Sub(v.start).Round(time.Millisecond))
	return at
}

// after waits (10 s at most) for cond after a change at `at`, and returns the
// screen and how long the change took to show.
func (v *view) after(at time.Time, what string, cond func(s screen) bool) (screen, time.Duration, bool) {
	v.e.t.Helper()
	s, ok := v.waitFor(what, at.Add(10*time.Second).Sub(time.Now()), cond)
	d := s.at.Sub(at)
	v.e.logf("CHANGE     %s shown %s after the save (shown: %v)\n", what, d.Round(time.Millisecond), ok)
	v.logScreen("SCREEN: "+what, s, nil)
	return s, d, ok
}

func within5(c *ck, what string, d time.Duration, ok bool) {
	c.t.Helper()
	c.notef("%s: %s after the save", what, d.Round(time.Millisecond))
	if !ok {
		c.failf("%s: not shown within 10 s of the save", what)
	} else if d > 5*time.Second {
		c.failf("%s: shown %s after the save, want within 5 s", what, d.Round(time.Millisecond))
	}
}

func noRow(tk string) func(s screen) bool {
	return func(s screen) bool { return len(s.rowLines(tk)) == 0 }
}

func (e *env) openCfgView(tks []string, cond func(s screen) bool) *view {
	e.t.Helper()
	v := e.openViewSized(cfgCols, viewRows, nil)
	want := rowsShown(tks)
	if cond != nil {
		want = func(s screen) bool { return rowsShown(tks)(s) && cond(s) }
	}
	s, _ := v.waitStable("every row with a price", 15*time.Second, 300*time.Millisecond, want)
	v.logScreen("SCREEN AFTER OPENING", s, nil)
	return v
}

func (v *view) done2() viewResult {
	v.e.t.Helper()
	r := v.quit([]byte("q"), "q")
	v.e.wantExit(result{exit: r.exit}, 0)
	return r
}

var (
	cfgA  = "tickers = [\"PETR4\", \"XXXX9\", \"AAPL\"]\n"
	cfgAM = "tickers = [\"PETR4\", \"XXXX9\", \"AAPL\", \"MSFT\"]\n"
)

// S11.3 [view] Given the view is open, when a ticker is added to the config
// file and the file is saved, then a row for it appears within 5 seconds,
// without a restart.
// S8.4 [view, print] Given a not-found row, when the config file changes, then
// the ticker is requested again without waiting for the hour.
// Default interval (60 s), so no heartbeat explains a request; MSFT added at +3 s.
func TestVerifyView_S11_3_S8_4_TickerAdded(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S11.3, S8.4: MSFT added while the view is open", cfgA)
	e.setSource(state{SymbolStatus: map[string]int{"XXXX9.SA": 404}}, "XXXX9.SA answered 404")
	v := e.openCfgView([]string{"PETR4", "AAPL"}, func(s screen) bool {
		return strings.Contains(strings.Join(s.rowLines("XXXX9"), ""), "not found")
	})
	v.holdUntil(v.start.Add(3 * time.Second))
	at := v.saveCfg(e.cfgPath, cfgAM, "MSFT added")
	_, d1, ok1 := v.after(at, "a row for MSFT", func(s screen) bool { return len(s.rowLines("MSFT")) == 1 })
	_, d2, ok2 := v.after(at, "MSFT with a price", rowsShown([]string{"MSFT"}))
	v.holdUntil(at.Add(5 * time.Second))
	r := v.done2()
	crit(e, "S11.3", "a row for the added ticker within 5 s", func(c *ck) {
		within5(c, "MSFT's row", d1, ok1)
		c.notef("MSFT with a price %s after the save (shown %v); MSFT requested %s after the save", d2.Round(time.Millisecond), ok2,
			fmtArrivals(arrivals(r.reqs, "MSFT", at, time.Now()), at))
	})
	crit(e, "S8.4", "the not-found ticker is requested again after the config changes", func(c *ck) {
		pre := arrivals(r.reqs, "XXXX9.SA", v.start, at)
		ts := arrivals(r.reqs, "XXXX9.SA", at, time.Now())
		c.notef("XXXX9.SA before the save %s; after %s", fmtArrivals(pre, v.start), fmtArrivals(ts, at))
		if len(ts) == 0 {
			c.failf("XXXX9.SA not requested within 5 s of the config change")
		}
	})
}

// S11.4 [view] Given the view is open, when a ticker is removed from the config
// file and the file is saved, then its row disappears within 5 seconds.
// Also recorded (6.4): the remaining rows keep their quotes, and a reorder.
func TestVerifyView_S11_4_TickerRemoved(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S11.4: AAPL removed while the view is open", baseConfig)
	v := e.openCfgView(tickers, nil)
	v.holdUntil(v.start.Add(3 * time.Second))
	cfg := strings.Replace(baseConfig, `tickers = ["PETR4", "AAPL"]`, `tickers = ["PETR4"]`, 1)
	at := v.saveCfg(e.cfgPath, cfg, "AAPL removed")
	s, d, ok := v.after(at, "AAPL's row gone", noRow("AAPL"))
	cfg2 := strings.Replace(cfg, `tickers = ["VALE3", "ITUB4"]`, `tickers = ["ITUB4", "VALE3"]`, 1)
	at2 := v.saveCfg(e.cfgPath, cfg2, "VALE3 and ITUB4 swapped")
	s2, d2, ok2 := v.after(at2, "ITUB4 above VALE3", func(s screen) bool {
		return s.lineOf("ITUB4") >= 0 && s.lineOf("VALE3") > s.lineOf("ITUB4")
	})
	r := v.done2()
	crit(e, "S11.4", "the removed ticker's row disappears within 5 s", func(c *ck) {
		within5(c, "AAPL's row gone", d, ok)
		for _, tk := range []string{"PETR4", "VALE3", "ITUB4", "MSFT"} {
			if ls := s.rowLines(tk); len(ls) != 1 || !hasQuote(ls[0]) {
				c.failf("remaining row %s: want its quote kept, got %q", tk, ls)
			}
		}
		c.notef("reorder shown %s after its save (shown %v); requests after the first save: %s", d2.Round(time.Millisecond), ok2,
			fmtCounts(bySymbol(func() []record {
				var o []record
				for _, q := range r.reqs {
					if q.UnixMS >= at.UnixMilli() {
						o = append(o, q)
					}
				}
				return o
			}())))
		_ = s2
	})
}

// S11.6 [view] Given `~/.config/brl-stocks` is a symbolic link and the view is
// open, when a ticker is added to the file in the link's target, then a row
// for it appears within 5 seconds. XDG_CONFIG_HOME unset.
func TestVerifyView_S11_6_EditThroughLink(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S11.6: the config folder is a link; the target file is edited", "")
	e.noCfgHome = true
	target := filepath.Join(e.root, "dotfiles", "brl-stocks")
	link := filepath.Join(e.home, ".config", "brl-stocks")
	writeFile(t, filepath.Join(target, "config.toml"), cfgA)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	e.cfgPath = filepath.Join(link, "config.toml")
	v := e.openCfgView([]string{"PETR4", "AAPL"}, nil)
	v.holdUntil(v.start.Add(3 * time.Second))
	at := v.saveCfg(filepath.Join(target, "config.toml"), cfgAM, "MSFT added to the file in the link's target")
	_, d, ok := v.after(at, "a row for MSFT", func(s screen) bool { return len(s.rowLines("MSFT")) == 1 })
	v.done2()
	crit(e, "S11.6", "a row for the ticker added through the link within 5 s", func(c *ck) {
		within5(c, "MSFT's row", d, ok)
		if tg, err := os.Readlink(link); err != nil || tg != target {
			c.failf("the link changed: %q %v", tg, err)
		}
	})
}

// S11.10 [view] Given the view is open, when the config file is saved with a
// syntax error, then the view keeps the last valid watchlist.
// S11.11 ... then the error line names the file and the problem.
func TestVerifyView_S11_10_11_SyntaxError(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S11.10, S11.11: a syntax error saved while the view is open", baseConfig)
	v := e.openCfgView(tickers, nil)
	before := v.snap()
	v.holdUntil(v.start.Add(3 * time.Second))
	broken := strings.Replace(baseConfig, `tickers = ["PETR4", "AAPL"]`, `tickers = ["PETR4", "AAPL", "MSFT"`, 1)
	at := v.saveCfg(e.cfgPath, broken, "a syntax error (an unclosed array)")
	s, d, ok := v.after(at, "a line about the config", func(s screen) bool {
		return len(otherLines(s, tickers, []string{"Brasil", "US"})) > 0
	})
	v.holdUntil(at.Add(5 * time.Second))
	s5 := v.snap()
	v.logScreen("SCREEN 5 s AFTER THE SAVE", s5, nil)
	at2 := v.saveCfg(e.cfgPath, baseConfig, "fixed")
	s6, d6, ok6 := v.after(at2, "no config line", func(s screen) bool {
		return len(otherLines(s, tickers, []string{"Brasil", "US"})) == 0
	})
	v.done2()
	crit(e, "S11.10", "the last valid watchlist stays", func(c *ck) {
		for _, tk := range tickers {
			b := priceRE.FindString(strings.Join(before.rowLines(tk), ""))
			a := priceRE.FindString(strings.Join(s5.rowLines(tk), ""))
			if a == "" || a != b {
				c.failf("%s: price before %q, 5 s after the broken save %q", tk, b, a)
			}
		}
		if s5.lineOf("MSFT") >= 0 && len(s5.rowLines("MSFT")) > 1 {
			c.failf("the broken file's rows were taken")
		}
	})
	crit(e, "S11.11", "the error line names the file and the problem", func(c *ck) {
		ol := otherLines(s, tickers, []string{"Brasil", "US"})
		c.notef("shown %s after the save: %q; status line %q", d.Round(time.Millisecond), ol, statusLine(s))
		if !ok || len(ol) != 1 {
			c.failf("want one error line, got %q", ol)
			return
		}
		l := ol[0]
		if !strings.Contains(l, e.cfgPath) && !strings.Contains(l, "config.toml") {
			c.failf("the error line does not name the file %s: %q", e.cfgPath, l)
		}
		rest := l
		if i := strings.Index(l, "config.toml"); i >= 0 {
			rest = l[i+len("config.toml"):]
		}
		if len(strings.TrimSpace(strings.Trim(rest, ":·- "))) < 5 {
			c.failf("the error line names no problem after the file: %q", l)
		}
		c.notef("after the fix: other lines %q, %s after the save (gone: %v)", otherLines(s6, tickers, []string{"Brasil", "US"}), d6.Round(time.Millisecond), ok6)
	})
}

// S11.14 [view] Given the view is open, when the config file is deleted, then
// the view keeps the last loaded watchlist.
// S11.19 ... then the error line says the file is gone.
// Outside the criteria: the file then comes back, with MSFT added.
func TestVerifyView_S11_14_19_Deleted(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S11.14, S11.19: the config deleted while the view is open; then it comes back", cfgA)
	v := e.openCfgView([]string{"PETR4", "AAPL"}, nil)
	v.holdUntil(v.start.Add(3 * time.Second))
	at := v.removeCfg(e.cfgPath)
	known := []string{"PETR4", "XXXX9", "AAPL", "MSFT"}
	s, d, ok := v.after(at, "a line about the config", func(s screen) bool { return len(otherLines(s, known, nil)) > 0 })
	v.holdUntil(at.Add(5 * time.Second))
	s5 := v.snap()
	v.logScreen("SCREEN 5 s AFTER THE DELETE", s5, nil)
	at2 := v.saveCfg(e.cfgPath, cfgAM, "the file comes back, with MSFT")
	s6, d6, ok6 := v.after(at2, "MSFT with a price and no error line", func(s screen) bool {
		return rowsShown([]string{"MSFT"})(s) && len(otherLines(s, known, nil)) == 0
	})
	v.done2()
	crit(e, "S11.14", "the last loaded watchlist stays after the delete", func(c *ck) {
		if !rowsShown([]string{"PETR4", "AAPL"})(s5) || len(s5.rowLines("XXXX9")) != 1 {
			c.failf("the watchlist changed after the delete")
		}
	})
	crit(e, "S11.19", "the error line says the file is gone", func(c *ck) {
		ol := otherLines(s, known, nil)
		c.notef("shown %s after the delete: %q", d.Round(time.Millisecond), ol)
		if !ok || len(ol) != 1 {
			c.failf("want one error line, got %q", ol)
			return
		}
		lo := strings.ToLower(ol[0])
		if !strings.Contains(lo, "config") {
			c.failf("the error line does not name the config: %q", ol[0])
		}
		said := false
		for _, w := range []string{"gone", "deleted", "removed", "missing", "no such", "not found", "does not exist"} {
			said = said || strings.Contains(lo, w)
		}
		if !said {
			c.failf("the error line does not say the file is gone: %q", ol[0])
		}
	})
	crit(e, "deleted file comes back (observation)", "the file saved again with MSFT", func(c *ck) {
		c.notef("MSFT with a price and no error line %s after the file came back (shown: %v); other lines %q", d6.Round(time.Millisecond), ok6, otherLines(s6, known, nil))
	})
}

// S11.20 [view] Given no file at the default config path and the view open with
// the example watchlist, when a valid config file is saved at that path, then
// its watchlist replaces the example within 5 seconds.
func TestVerifyView_S11_20_ConfigAppears(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S11.20: a config appears while the example watchlist is shown", "")
	v := e.openCfgView(exampleTickers, nil)
	v.holdUntil(v.start.Add(3 * time.Second))
	at := v.saveCfg(e.cfgPath, "tickers = [\"MSFT\", \"BBDC4\"]\n", "a config with MSFT and BBDC4")
	s, d, ok := v.after(at, "MSFT and BBDC4 only", func(s screen) bool {
		for _, tk := range []string{"PETR4", "VALE3", "ITUB4", "AAPL"} {
			if len(s.rowLines(tk)) != 0 {
				return false
			}
		}
		return len(s.rowLines("MSFT")) == 1 && len(s.rowLines("BBDC4")) == 1
	})
	v.done2()
	crit(e, "S11.20", "the file's watchlist replaces the example within 5 s", func(c *ck) {
		within5(c, "the watchlist replaced", d, ok)
		c.notef("status line after %q", statusLine(s))
		if strings.Contains(statusLine(s), "no config") {
			c.notef("the no-config warning is still in the status line")
		}
	})
}

// 7.3 Error line: "When several problems exist, it shows the first of: config
// problem, block, failed fetches." The source fails (503) and the error line
// says so; a broken config is saved: the error line shows only the config
// problem; the config is fixed: the fetch problem is shown again.
func TestVerifyView_Sec7_3_ConfigProblemFirst(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "7.3: the config problem before failed fetches on the error line", baseConfig)
	e.setSource(state{Status: 503}, "every request answered 503")
	v := e.openViewSized(cfgCols, viewRows, nil)
	known, grp := tickers, []string{"Brasil", "US"}
	s0, _ := v.waitStable("the fetch problem", 15*time.Second, 300*time.Millisecond, func(s screen) bool { return len(errorLines(s)) == 1 })
	v.logScreen("SCREEN WITH THE FETCH PROBLEM", s0, nil)
	v.holdUntil(v.start.Add(3 * time.Second))
	broken := baseConfig + "\n[[group\n"
	at := v.saveCfg(e.cfgPath, broken, "a broken config")
	s1, d1, ok1 := v.after(at, "a config line in place of the fetch line", func(s screen) bool {
		return len(errorLines(s)) == 0 && len(otherLines(s, known, grp)) == 1
	})
	v.holdUntil(at.Add(3 * time.Second))
	s1b := v.snap()
	at2 := v.saveCfg(e.cfgPath, baseConfig, "the config fixed")
	s2, d2, ok2 := v.after(at2, "the fetch problem again", func(s screen) bool {
		return len(errorLines(s)) == 1 && len(otherLines(s, known, grp)) == 1
	})
	v.done2()
	crit(e, "7.3 config problem first", "with failed fetches, a broken config shows only the config problem; fixed, the fetch problem returns", func(c *ck) {
		c.notef("before: %q", otherLines(s0, known, grp))
		c.notef("broken config, %s after the save: %q; 3 s later: %q", d1.Round(time.Millisecond), otherLines(s1, known, grp), otherLines(s1b, known, grp))
		c.notef("fixed, %s after the save: %q", d2.Round(time.Millisecond), otherLines(s2, known, grp))
		if len(errorLines(s0)) != 1 {
			c.failf("set-up: no fetch problem on the error line before the broken save")
		}
		if !ok1 {
			c.failf("with the broken config the error line is not the config problem alone: %q", otherLines(s1, known, grp))
		} else if ol := otherLines(s1b, known, grp); len(ol) != 1 || len(errorLines(s1b)) != 0 || !strings.Contains(ol[0], "config") {
			c.failf("3 s after the broken save the error line is %q, want the config problem alone", ol)
		}
		if !ok2 {
			c.failf("after the fix the fetch problem did not come back: %q", otherLines(s2, known, grp))
		}
	})
}

// Outside the criteria: an editor-style save (a temporary file renamed over
// the config), two saves 300 ms apart, an emptied file, and a config grown to
// 20 tickers. Recorded, with the time to the screen.
func TestVerifyView_ConfigSaves_Observations(t *testing.T) {
	t.Parallel()
	t.Run("rename_over", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, "observation: an editor-style save (rename over the config)", cfgA)
		v := e.openCfgView([]string{"PETR4", "AAPL"}, nil)
		v.holdUntil(v.start.Add(3 * time.Second))
		at := v.renameCfg(e.cfgPath, cfgAM, "MSFT added")
		_, d, ok := v.after(at, "a row for MSFT", func(s screen) bool { return len(s.rowLines("MSFT")) == 1 })
		at2 := v.renameCfg(e.cfgPath, cfgA, "MSFT removed again")
		_, d2, ok2 := v.after(at2, "MSFT gone", noRow("MSFT"))
		v.done2()
		crit(e, "rename over (observation)", "rows follow a save by rename", func(c *ck) {
			c.notef("added: %s (shown %v); removed by a second rename: %s (shown %v)", d.Round(time.Millisecond), ok, d2.Round(time.Millisecond), ok2)
			if !ok || d > 5*time.Second || !ok2 || d2 > 5*time.Second {
				c.failf("a save by rename was not followed within 5 s (6.4)")
			}
		})
	})
	t.Run("two_saves_300ms", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, "observation: two saves 300 ms apart", cfgA)
		v := e.openCfgView([]string{"PETR4", "AAPL"}, nil)
		v.holdUntil(v.start.Add(3 * time.Second))
		v.saveCfg(e.cfgPath, cfgAM, "first save: MSFT added")
		time.Sleep(300 * time.Millisecond)
		final := "tickers = [\"PETR4\", \"XXXX9\", \"AAPL\", \"ITUB4\"]\n"
		at := v.saveCfg(e.cfgPath, final, "second save: ITUB4 in place of MSFT")
		s, d, ok := v.after(at, "ITUB4 and no MSFT", func(s screen) bool { return len(s.rowLines("ITUB4")) == 1 && len(s.rowLines("MSFT")) == 0 })
		v.holdUntil(at.Add(6 * time.Second))
		s6 := v.snap()
		v.logScreen("SCREEN 6 s AFTER THE SECOND SAVE", s6, nil)
		v.done2()
		crit(e, "two saves (observation)", "the second save wins", func(c *ck) {
			c.notef("second save shown %s after it (shown %v); MSFT rows 6 s later: %d", d.Round(time.Millisecond), ok, len(s6.rowLines("MSFT")))
			if !ok || len(s6.rowLines("MSFT")) != 0 || len(s6.rowLines("ITUB4")) != 1 {
				c.failf("the screen does not follow the last save: %q", viewBody(s6))
			}
			_ = s
		})
	})
	t.Run("emptied", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, "observation: the config emptied (0 bytes)", cfgA)
		v := e.openCfgView([]string{"PETR4", "AAPL"}, nil)
		v.holdUntil(v.start.Add(3 * time.Second))
		at := v.saveCfg(e.cfgPath, "", "emptied")
		v.holdUntil(at.Add(5 * time.Second))
		s := v.snap()
		v.logScreen("SCREEN 5 s AFTER EMPTYING", s, nil)
		v.done2()
		crit(e, "emptied (observation)", "what an empty config shows", func(c *ck) {
			c.notef("body %q; status line %q", viewBody(s), statusLine(s))
		})
	})
	t.Run("grown_to_20", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, "observation: the config grown from 2 to 20 tickers", "tickers = [\"PETR4\", \"AAPL\"]\n")
		v := e.openCfgView([]string{"PETR4", "AAPL"}, nil)
		v.holdUntil(v.start.Add(3 * time.Second))
		c20 := cfgSpec{top: []string{"PETR4", "AAPL"}}.pad20()
		at := v.saveCfg(e.cfgPath, c20.String(), "18 tickers added")
		_, dRows, okRows := v.after(at, "20 rows", func(s screen) bool {
			for _, tk := range c20.all() {
				if len(s.rowLines(tk)) != 1 {
					return false
				}
			}
			return true
		})
		_, dQ, okQ := v.after(at, "20 rows with quotes", rowsShown(c20.all()))
		r := v.done2()
		crit(e, "grown to 20 (observation)", "time until all 20 rows have quotes", func(c *ck) {
			n := 0
			for _, q := range r.reqs {
				if q.UnixMS >= at.UnixMilli() {
					n++
				}
			}
			c.notef("20 rows %s after the save (%v); all with quotes %s (%v); %d requests after the save", dRows.Round(time.Millisecond), okRows, dQ.Round(time.Millisecond), okQ, n)
			if !okQ || dQ > 5*time.Second {
				c.failf("the 18 new rows did not all have quotes within 5 s (6.4: new rows are fetched at once)")
			}
		})
	})
}
