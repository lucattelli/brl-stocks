//go:build verification && linux

// The full-screen view, increment 4 of slice 2: the rest of config, cache and
// source at start (S11.1, S11.5, S11.7, S11.8, S11.9, S11.12, S11.13, S11.15
// to S11.18, S12.1 to S12.5, S15.2, S15.5, S15.6, S15.8, S17.3), through the
// real view on a pseudo-terminal with the helpers of view_verify_test.go. On
// the view the error stream is the terminal: a message at start is read from
// everything the tool wrote (escape sequences removed) and from the screen
// after it exited. See README.md.
package verification

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var escRE = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b\[[0-9;?<>=:]*[ -/]*[@-~]|\x1bP[^\x1b]*\x1b\\|\x1b[@-_]`)

// plainOutput is everything the tool wrote to the terminal, without escape
// sequences and carriage returns.
func (v *view) plainOutput() string {
	v.mu.Lock()
	raw := v.raw.String()
	v.mu.Unlock()
	return strings.ReplaceAll(escRE.ReplaceAllString(raw, ""), "\r", "")
}

// exitAtStart runs the view and waits for the tool to exit by itself.
func (e *env) exitAtStart(extra []string, args ...string) (viewResult, string) {
	e.t.Helper()
	v := e.openView(extra, args...)
	r := v.finish()
	out := v.plainOutput()
	e.logf("OUTPUT (escape sequences removed)\n%s\n", indent(out))
	return r, out
}

// twentyFive is a config of 25 distinct tickers.
func twentyFive() cfgSpec {
	c := cfgSpec{top: []string{"PETR4", "AAPL"}}
	c.top = append(c.top, fillers...)
	return c
}

// ---------------------------------------------------------------------------
// S11 Config paths, links, errors and warnings
// ---------------------------------------------------------------------------

// S11.1 [view, print] Given a valid file at `~/.config/brl-stocks/config.toml`
// and no `--config` flag, when the tool starts, then it shows that file's
// watchlist. XDG_CONFIG_HOME unset; HOME temporary.
func TestVerifyView_S11_1_DefaultPathUnderHome(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S11.1 (view; XDG_CONFIG_HOME unset)", "tickers = [\"VALE3\"]\n")
	e.noCfgHome = true
	e.cfgPath = filepath.Join(e.home, ".config", "brl-stocks", "config.toml")
	writeFile(t, e.cfgPath, "tickers = [\"MSFT\", \"ITUB4\"]\n")
	e.logf("CONFIG AT  %s\n    tickers = [\"MSFT\", \"ITUB4\"]\n(the file under XDG_CONFIG_HOME's folder, not used, holds VALE3)\n", e.cfgPath)
	s, _, _ := e.viewAndQuit("the rows of ~/.config/brl-stocks/config.toml", rowsShown([]string{"MSFT", "ITUB4"}))
	crit(e, "S11.1", "the watchlist of ~/.config/brl-stocks/config.toml is shown", func(c *ck) {
		if len(s.rowLines("VALE3")) != 0 {
			c.failf("VALE3 (not in that file) is shown")
		}
	})
}

// S11.15 [view, print] Given `XDG_CONFIG_HOME=/tmp/x` and a valid file at
// `/tmp/x/brl-stocks/config.toml`, when the tool starts without `--config`,
// then it shows that file's watchlist. A different watchlist is at ~/.config.
func TestVerifyView_S11_15_XDGConfigHome(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S11.15 (view)", "tickers = [\"MSFT\", \"ITUB4\"]\n")
	writeFile(t, filepath.Join(e.home, ".config", "brl-stocks", "config.toml"), "tickers = [\"VALE3\"]\n")
	s, _, _ := e.viewAndQuit("the rows of $XDG_CONFIG_HOME/brl-stocks/config.toml", rowsShown([]string{"MSFT", "ITUB4"}))
	crit(e, "S11.15", "the watchlist under XDG_CONFIG_HOME is shown", func(c *ck) {
		if len(s.rowLines("VALE3")) != 0 {
			c.failf("VALE3 (from ~/.config) is shown")
		}
	})
}

// S11.5 [view, print] Given `~/.config/brl-stocks` is a symbolic link to a
// folder elsewhere, when the tool starts, then it reads the config through the
// link. S11.7 ... when the tool has run and exited, then the link still exists
// and points to the same target. S11.8 Given a config file, when the tool has
// run and exited, then the file's content is unchanged, byte for byte.
// XDG_CONFIG_HOME unset; three view runs: q, Ctrl+C, --config through the link.
func TestVerifyView_S11_5_7_8_ConfigThroughLink(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S11.5, S11.7, S11.8 (view)", "")
	e.noCfgHome = true
	target := filepath.Join(e.root, "dotfiles", "brl-stocks")
	link := filepath.Join(e.home, ".config", "brl-stocks")
	writeFile(t, filepath.Join(target, "config.toml"), baseConfig)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	e.cfgPath = filepath.Join(link, "config.toml")
	content, _ := os.ReadFile(filepath.Join(target, "config.toml"))
	type run struct {
		s          screen
		linkTarget string
		isLink     bool
		bytesSame  bool
	}
	var runs []run
	for _, k := range []struct {
		name string
		b    []byte
		args []string
	}{{"q", []byte("q"), nil}, {"ctrl_c", []byte{0x03}, nil}, {"q, --config through the link", []byte("q"), []string{"--config", e.cfgPath}}} {
		v := e.openView(nil, k.args...)
		s, _ := v.waitStable("every row with a price", 10*time.Second, 300*time.Millisecond, rowsShown(tickers))
		v.logScreen("SCREEN BEFORE "+k.name, s, nil)
		v.quit(k.b, k.name)
		st, err := os.Lstat(link)
		tg, _ := os.Readlink(link)
		now, _ := os.ReadFile(filepath.Join(target, "config.toml"))
		r := run{s: s, linkTarget: tg, isLink: err == nil && st.Mode()&os.ModeSymlink != 0, bytesSame: string(now) == string(content)}
		e.logf("AFTER %s: link %v -> %s; config bytes unchanged: %v\n", k.name, r.isLink, strings.TrimPrefix(tg, e.root), r.bytesSame)
		runs = append(runs, r)
	}
	crit(e, "S11.5", "the config is read through the link", func(c *ck) {
		for i, r := range runs {
			if !rowsShown(tickers)(r.s) {
				c.failf("run %d: the watchlist of the linked file is not shown", i+1)
			}
		}
	})
	crit(e, "S11.7", "the link still exists and points to the same target", func(c *ck) {
		for i, r := range runs {
			if !r.isLink || r.linkTarget != target {
				c.failf("run %d: link %v -> %q, want a link to %q", i+1, r.isLink, r.linkTarget, target)
			}
		}
	})
	crit(e, "S11.8", "the config file is unchanged, byte for byte", func(c *ck) {
		for i, r := range runs {
			if !r.bytesSame {
				c.failf("run %d: the config file changed", i+1)
			}
		}
	})
}

// S11.9 [view, print] Given a config file that cannot be parsed, when the tool
// starts, then it exits with status 2. S11.16 ... then a message on the error
// stream names the file and the problem. Also the other invalid configs of
// 7.2; the default path and --config.
func TestVerifyView_S11_9_16_InvalidConfig(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, cfg string }{
		{"cannot_be_parsed", "tickers = [\"PETR4\"\nthis is not toml ===\n"},
		{"wrong_type", "refresh_seconds = \"ten\"\ntickers = [\"PETR4\"]\n"},
		{"unknown_summary_name", "summary = [\"IBOV\", \"DAX\"]\ntickers = [\"PETR4\"]\n"},
		{"empty_ticker", "tickers = [\"PETR4\", \"\"]\n"},
	}
	for _, cs := range cases {
		cs := cs
		t.Run(cs.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, "S11.9, S11.16 (view) ["+cs.name+"]", cs.cfg)
			other := filepath.Join(e.root, "named.toml")
			writeFile(t, other, cs.cfg)
			type res struct {
				path string
				r    viewResult
				out  string
			}
			var rs []res
			for _, v := range []struct {
				path string
				args []string
			}{{e.cfgPath, nil}, {other, []string{"--config", other}}} {
				r, out := e.exitAtStart(nil, v.args...)
				rs = append(rs, res{v.path, r, out})
			}
			crit(e, "S11.9", "an invalid config: exit status 2", func(c *ck) {
				for _, x := range rs {
					if x.r.exit != 2 {
						c.failf("%s: exit status %d, want 2", x.path, x.r.exit)
					}
					if len(x.r.reqs) != 0 {
						c.failf("%s: %d requests sent", x.path, len(x.r.reqs))
					}
				}
			})
			crit(e, "S11.16", "a message names the file and the problem", func(c *ck) {
				for _, x := range rs {
					if !strings.Contains(x.out, x.path) {
						c.failf("want the message to name %s, the tool wrote %q", x.path, x.out)
						continue
					}
					if rest := strings.TrimSpace(strings.ReplaceAll(x.out, x.path, "")); len(rest) < 10 {
						c.failf("want the problem named beside the file, the tool wrote only %q", x.out)
					}
					if strings.TrimSpace(x.r.after.text()) == "" {
						c.notef("%s: the screen after the exit is blank (the message is not left in sight)", x.path)
					}
					c.notef("message %q", strings.TrimSpace(x.out))
				}
			})
		})
	}
}

// S11.12 [view, print] Given a config with a key the tool does not know, when
// the tool starts, then a warning names the key. S11.17 ... then the watchlist
// is shown. (7.2: warnings in the status line of the view.)
func TestVerifyView_S11_12_17_UnknownKey(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S11.12, S11.17 (view)", "colour_scheme = \"dark\"\n"+baseConfig)
	s, _, _ := e.viewAndQuit("the rows and a warning naming the key", func(s screen) bool {
		return rowsShown(tickers)(s) && s.contains("colour_scheme")
	})
	crit(e, "S11.12", "a warning names the unknown key", func(c *ck) {
		st := statusLine(s)
		if !strings.Contains(st, "colour_scheme") {
			c.failf("the status line does not name `colour_scheme`: %q", st)
		}
		c.notef("status line %q", st)
	})
	crit(e, "S11.17", "the watchlist is shown", func(c *ck) {
		if !rowsShown(tickers)(s) {
			c.failf("not every row is shown")
		}
	})
}

// S11.13 [view, print] Given a config with 25 tickers, when the view is drawn,
// then all 25 rows exist. S11.18 ... then a warning in the status line says
// the tool is designed for 20.
func TestVerifyView_S11_13_18_TwentyFiveTickers(t *testing.T) {
	t.Parallel()
	cfg := twentyFive()
	e := newEnv(t, "S11.13, S11.18 (view)", cfg.String())
	s, _, _ := e.viewAndQuit("25 rows with a price", rowsShown(cfg.all()))
	crit(e, "S11.13", "all 25 rows exist", func(c *ck) {
		if n := len(cfg.all()); n != 25 {
			c.failf("set-up: %d tickers", n)
		}
		for _, tk := range cfg.all() {
			if len(s.rowLines(tk)) != 1 {
				c.failf("row %s missing", tk)
			}
		}
	})
	crit(e, "S11.18", "a warning in the status line says the tool is designed for 20", func(c *ck) {
		st := statusLine(s)
		if !strings.Contains(st, "designed for 20") {
			c.failf("status line %q", st)
		}
		c.notef("status line %q", st)
	})
}

// ---------------------------------------------------------------------------
// S12 No config yet
// ---------------------------------------------------------------------------

// S12.1 [view, print] Given no file at the default config path, when the tool
// starts, then it shows a built-in example watchlist. S12.2 ... then it names
// the path where a config file belongs. S12.3 ... when the tool has run and
// exited, then no file or folder has been created under the config path.
// 7.7: the example is PETR4, VALE3, ITUB4, AAPL, MSFT with the default summary
// line, and the path is named in the status line. XDG_CONFIG_HOME set (to a
// folder outside the home folder: the path is named in full, on a terminal of
// 200 columns so that the long temporary path fits) and unset (the path is
// under the home folder: since D95 it is written `~/.config/brl-stocks/
// config.toml`, and the ordinary 120 columns are enough).
func TestVerifyView_S12_1_3_NoConfig(t *testing.T) {
	t.Parallel()
	example := []string{"PETR4", "VALE3", "ITUB4", "AAPL", "MSFT"}
	for _, variant := range []string{"xdg_set", "xdg_unset"} {
		variant := variant
		t.Run(variant, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, "S12.1 to S12.3 (view) ["+variant+"]", "")
			base := e.cfgHome
			if variant == "xdg_unset" {
				e.noCfgHome = true
				base = filepath.Join(e.home, ".config")
				os.MkdirAll(base, 0o755)
			}
			e.cfgPath = filepath.Join(base, "brl-stocks", "config.toml")
			if err := os.RemoveAll(filepath.Join(base, "brl-stocks")); err != nil {
				t.Fatal(err)
			}
			list := func() string {
				var b strings.Builder
				filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
					if p != base {
						fmt.Fprintln(&b, strings.TrimPrefix(p, e.root))
					}
					return nil
				})
				return b.String()
			}
			before := list()
			named, cols := e.cfgPath, 200
			if variant == "xdg_unset" {
				named, cols = "~/.config/brl-stocks/config.toml", viewCols // D95
			}
			v := e.openViewSized(cols, viewRows, nil)
			s, _ := v.waitStable("the example rows and the config path", 10*time.Second, 300*time.Millisecond, func(s screen) bool {
				return rowsShown(example)(s) && strings.Contains(statusLine(s), named)
			})
			v.logScreen("SCREEN BEFORE QUITTING", s, nil)
			r := v.quit([]byte("q"), "q")
			after := list()
			crit(e, "S12.1", "the built-in example watchlist is shown", func(c *ck) {
				var got []string
				for _, l := range viewBody(s) {
					got = append(got, strings.Fields(l)[0])
				}
				if strings.Join(got, " ") != strings.Join(example, " ") {
					c.failf("want the rows %v, got the lines %v", example, got)
				}
				if f := firstText(s); !ibovRE.MatchString(f) || !spxRE.MatchString(f) || !fxRE.MatchString(f) {
					c.failf("7.7: want the default summary line, got %q", f)
				}
				if r.exit != 0 {
					c.failf("exit %d", r.exit)
				}
			})
			crit(e, "S12.2", "the path where a config belongs is named", func(c *ck) {
				st := statusLine(s)
				if !strings.Contains(st, named) {
					c.failf("want the status line to name %s, got %q", named, st)
				}
				c.notef("status line %q", st)
			})
			crit(e, "S12.3", "nothing is created under the config path", func(c *ck) {
				if after != before {
					c.failf("something was created under %s:\n--- before\n%s--- after\n%s", base, before, after)
				}
			})
		})
	}
}

// The same note at the ordinary width of 120 columns, with the config path
// outside the home folder (so not shortened by D95): logged, not asserted.
// How the warning is shortened and cut is checked by S12.7 and S12.8
// (view_increment6_verify_test.go).
func TestVerifyView_S12_2_PathAt120Columns(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S12.2 (view, 120 columns: observation)", "")
	e.cfgPath = filepath.Join(e.cfgHome, "brl-stocks", "config.toml")
	os.RemoveAll(filepath.Join(e.cfgHome, "brl-stocks"))
	s, _, _ := e.viewAndQuit("the example rows", rowsShown([]string{"PETR4", "VALE3", "ITUB4", "AAPL", "MSFT"}))
	e.logf("OBSERVED   status line at 120 columns %q; the path %q (%d characters) is whole on screen: %v\n",
		statusLine(s), e.cfgPath, len(e.cfgPath), s.contains(e.cfgPath))
}

// S12.4 [view, print] Given `--config /tmp/missing.toml` and no such file,
// when the tool starts, then it exits with status 2. S12.5 ... then a message
// on the error stream names the path.
func TestVerifyView_S12_4_5_ConfigFlagMissingFile(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S12.4, S12.5 (view)", baseConfig)
	missing := filepath.Join(e.root, "missing.toml")
	r, out := e.exitAtStart(nil, "--config", missing)
	crit(e, "S12.4", "--config naming a missing file: exit status 2", func(c *ck) {
		if r.exit != 2 {
			c.failf("exit status %d, want 2", r.exit)
		}
		if len(r.reqs) != 0 {
			c.failf("%d requests sent", len(r.reqs))
		}
	})
	crit(e, "S12.5", "a message names the path", func(c *ck) {
		if !strings.Contains(out, missing) {
			c.failf("want the message to name %s, the tool wrote %q", missing, out)
		}
		c.notef("message %q; screen after the exit blank: %v", strings.TrimSpace(out), strings.TrimSpace(r.after.text()) == "")
	})
}

// ---------------------------------------------------------------------------
// S15 The cache
// ---------------------------------------------------------------------------

// S15.2 [view, print] Given the tool has fetched quotes, when the files it
// keeps are listed, then they are under `~/.cache/brl-stocks/`.
// XDG_CACHE_HOME unset; HOME temporary.
func TestVerifyView_S15_2_FilesUnderHomeCache(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S15.2 (view; XDG_CACHE_HOME unset)", baseConfig)
	e.noCacheHome = true
	before := listFiles(e.root)
	e.viewAndQuit("every row with a price", rowsShown(tickers))
	crit(e, "S15.2", "the files the tool keeps are under ~/.cache/brl-stocks/", func(c *ck) {
		want := filepath.Join(e.home, ".cache", "brl-stocks") + string(filepath.Separator)
		n := 0
		for p := range listFiles(e.root) {
			if before[p] {
				continue
			}
			rel, _ := filepath.Rel(e.root, p)
			c.notef("new file %s", rel)
			if strings.HasPrefix(p, want) {
				n++
			} else {
				c.failf("a file kept outside ~/.cache/brl-stocks/: %s", rel)
			}
		}
		if n == 0 {
			c.failf("no file under ~/.cache/brl-stocks/ after the view fetched quotes")
		}
	})
}

// S15.5 [view] Given a cache folder that cannot be written, when the view
// runs, then it shows quotes. S15.8 [view, print] ... when the tool runs, then
// one warning says the quotes will not be kept. Three ways, as for `print`.
func TestVerifyView_S15_5_8_UnwritableCache(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("running as root: a folder cannot be made unwritable")
	}
	for _, variant := range []string{"empty_folder_0555", "parent_0555_folder_missing", "kept_quotes_then_read_only"} {
		variant := variant
		t.Run(variant, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, "S15.5, S15.8 (view) ["+variant+"]", baseConfig)
			dir := filepath.Join(e.cacheHome, "brl-stocks")
			switch variant {
			case "empty_folder_0555":
				os.MkdirAll(dir, 0o755)
				os.Chmod(dir, 0o555)
				t.Cleanup(func() { os.Chmod(dir, 0o755) })
			case "parent_0555_folder_missing":
				os.Chmod(e.cacheHome, 0o555)
				t.Cleanup(func() { os.Chmod(e.cacheHome, 0o755) })
			case "kept_quotes_then_read_only":
				e.wantExit(e.run("print"), 0)
				var paths []string
				filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
					if err == nil {
						paths = append(paths, p)
					}
					return nil
				})
				for i := len(paths) - 1; i >= 0; i-- {
					if st, err := os.Stat(paths[i]); err == nil {
						p, m := paths[i], st.Mode().Perm()
						os.Chmod(p, m&^0o222)
						t.Cleanup(func() { os.Chmod(p, m) })
					}
				}
				e.sleep(pastInterval, "cache older than the interval")
				e.setSource(state{Quotes: map[string]override{"PETR4.SA": {Price: fp(40.00)}}}, "PETR4 now 40.00")
			}
			wantPETR4 := "R$ 38,42"
			if variant == "kept_quotes_then_read_only" {
				wantPETR4 = "R$ 40,00"
			}
			s, r, _ := e.viewAndQuit("every row with a fetched price", func(s screen) bool {
				return rowsShown(tickers)(s) && rowsShownWith(map[string]string{"PETR4": wantPETR4})(s)
			})
			crit(e, "S15.5", "with an unwritable cache folder the view shows quotes (it fetches)", func(c *ck) {
				if !rowsShown(tickers)(s) {
					c.failf("not every row shows a quote")
				}
				if len(r.reqs) == 0 {
					c.failf("the view sent no request (7.7: the view goes on fetching)")
				}
			})
			crit(e, "S15.8", "one warning says the quotes will not be kept", func(c *ck) {
				var w []string
				for _, l := range s.lines {
					ll := strings.ToLower(l)
					if strings.Contains(ll, "kept") || strings.Contains(ll, "cache") {
						w = append(w, strings.TrimSpace(l))
					}
				}
				if len(w) != 1 {
					c.failf("want exactly one warning about the cache on the screen, got %q", w)
				} else if !strings.Contains(statusLine(s), w[0]) && statusLine(s) != w[0] {
					c.notef("the warning is not in the status line: %q", w[0])
				} else {
					c.notef("status line %q", statusLine(s))
				}
			})
		})
	}
}

// S15.6 [view, print] Given a ticker was removed from the config, a refresh
// then succeeded, and the ticker is added back with no network, when the view
// is drawn, then the row shows no data. Three view runs: PETR4 and AAPL; AAPL
// alone (12 s later, so the view fetches); PETR4 and AAPL again with the
// stand-in stopped.
func TestVerifyView_S15_6_RemovedTickerForgotten(t *testing.T) {
	t.Parallel()
	full := "refresh_seconds = 10\ntickers = [\"PETR4\", \"AAPL\"]\n"
	e := newEnv(t, "S15.6 (view)", full)
	e.viewAndQuit("both rows with a price", rowsShown([]string{"PETR4", "AAPL"}))
	writeFile(t, e.cfgPath, "refresh_seconds = 10\ntickers = [\"AAPL\"]\n")
	e.logf("CONFIG     changed: PETR4 removed\n")
	e.sleep(pastInterval, "cache older than the interval, so the view fetches")
	_, r2, _ := e.viewAndQuit("AAPL with a price", rowsShown([]string{"AAPL"}))
	writeFile(t, e.cfgPath, full)
	e.logf("CONFIG     changed: PETR4 added back\n")
	e.logf("\nSTAND-IN   stopped: its address now refuses connections\n")
	e.src.stop()
	s, _, _ := e.viewAndQuit("both rows", func(s screen) bool {
		return len(s.rowLines("PETR4")) == 1 && rowsShown([]string{"AAPL"})(s)
	})
	crit(e, "S15.6", "the row added back with no network shows no data", func(c *ck) {
		if bySymbol(r2.reqs)["AAPL"] == 0 {
			c.failf("set-up: the second run did not fetch AAPL (a refresh that succeeded)")
		}
		l := s.rowLines("PETR4")[0]
		if hasQuote(l) {
			c.failf("PETR4 shows a quote: %q", l)
		}
		if n := dashCount(strings.Fields(l)[1:]); n < 7 {
			c.failf("PETR4: want `—` in each of the 7 data columns, found %d in %q", n, l)
		}
		c.notef("PETR4 %q", l)
	})
}

// ---------------------------------------------------------------------------
// S17.3 Nothing selects a source
// ---------------------------------------------------------------------------

// S17.3 [view, print] Given the first version, when its config file and flags
// are listed, then none of them selects a data source. `brl-stocks --help`
// names no source; `brl-stocks --source <url>` is refused (exit 2); a `source`
// key in the config is an unknown key: warned about, and the requests still
// reach the stand-in.
func TestVerifyView_S17_3_NothingSelectsASource(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S17.3 (view)", "source = \"http://127.0.0.1:1\"\n"+baseConfig)
	h := e.run("--help")
	bad := e.run("--source", "http://127.0.0.1:1")
	s, r, _ := e.viewAndQuit("the rows and a warning about `source`", func(s screen) bool {
		return rowsShown(tickers)(s) && strings.Contains(statusLine(s), "source")
	})
	crit(e, "S17.3", "no flag or config key selects a source", func(c *ck) {
		help := strings.ToLower(h.stdout + h.stderr)
		for _, w := range []string{"source", "url", "provider", "yahoo", "brl_stocks_source_url"} {
			if strings.Contains(help, w) {
				c.failf("--help mentions %q: %q", w, h.stdout+h.stderr)
			}
		}
		if bad.exit != 2 {
			c.failf("--source: exit %d, want 2", bad.exit)
		}
		if len(r.reqs) == 0 {
			c.failf("with a `source` key, the view's requests no longer reach the stand-in")
		}
		c.notef("status line %q", statusLine(s))
	})
}
