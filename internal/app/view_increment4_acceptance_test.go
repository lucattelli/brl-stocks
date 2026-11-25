package app_test

// Acceptance tests of the full-screen view, increment 4 of slice 2: the rest
// of config, cache and source at start. Written from docs/spec.md and the
// view's test boundary; they reuse the helpers of the earlier view tests and
// of the print acceptance tests.
//
// A config error at start ends the tool before the view opens (6.4). Through
// the boundary that is seen as app.Run returning, with a Terminal given, the
// exit status, the error stream, and no frame drawn and no request sent.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucattelli/brl-stocks/internal/market"
	"github.com/lucattelli/brl-stocks/internal/provider"
)

// ---------------------------------------------------------------------------
// Helpers of this increment
// ---------------------------------------------------------------------------

// exitStatus waits, with the real-time guard, for Run to return without any
// input, and returns its exit status.
func (r *viewRun) exitStatus() int {
	r.t.Helper()
	select {
	case <-r.done:
		_, code := r.exited()
		return code
	case <-time.After(viewGuard):
		f := r.allFrames()
		last := ""
		if len(f) > 0 {
			last = f[len(f)-1].text()
		}
		r.t.Fatalf("the tool did not exit within %v; %d frames drawn; last frame:\n%s", viewGuard, len(f), last)
		return -1
	}
}

// endsBeforeTheView checks 6.4's "(exit)": the view never opened.
func endsBeforeTheView(t *testing.T, r *viewRun, w *world) {
	t.Helper()
	if n := len(r.allFrames()); n != 0 {
		t.Errorf("the view drew %d frames before the tool exited, want none (6.4: the tool exits at start)", n)
	}
	if n := w.src.total(); n != 0 {
		t.Errorf("%d requests sent before the tool exited, want none", n)
	}
}

// openAndQuit opens the view, waits for it to settle, quits with key and
// checks the exit status 0.
func openAndQuit(t *testing.T, w *world, o viewOpts, key string) string {
	t.Helper()
	v := openView(t, w, o)
	f := v.settled().text()
	if code := v.quit(key); code != 0 {
		t.Fatalf("the view exited with status %d after %q, want 0\nstderr: %q", code, key, v.stderr.String())
	}
	return f
}

var viewExampleTickers = []string{"PETR4", "VALE3", "ITUB4", "AAPL", "MSFT"}

// viewTwentyFive is a config with 25 tickers, every one answered by the source.
func viewTwentyFive(w *world) (string, []string) {
	loadTwenty(w.src, w.now)
	extra := []string{"LREN3", "GGBR4", "CSNA3", "EMBR3", "RADL3"}
	for i, s := range extra {
		p := float64(60+i) + 0.25
		w.src.answer(market.Instrument{Market: market.B3, Symbol: s}, quote(s+" SA", "BRL", p, p, b3Session, w.now.Add(-15*time.Minute)))
	}
	all := append(twentyTickers(), extra...)
	return `tickers = ["` + strings.Join(all, `", "`) + `"]` + "\n", all
}

// ---------------------------------------------------------------------------
// S11 - The watchlist is a file I edit
// ---------------------------------------------------------------------------

// S11.1 Given a valid file at `~/.config/brl-stocks/config.toml` and no
// `--config` flag, when the tool starts, then it shows that file's watchlist.
// (D29)
func TestS11_1_View_DefaultPathIsRead(t *testing.T) {
	// Given: a watchlist unlike the built-in example; no XDG_CONFIG_HOME
	w := newHomeWorld(t, bothOpen, `tickers = ["ITUB4"]`+"\n")
	loadQuotes(w.src, w.now, true)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Then
	assertRowHas(t, f, "ITUB4", "R$ 34,15")
	for _, other := range []string{"PETR4", "VALE3", "AAPL", "MSFT"} {
		if hasRow(f, other) {
			t.Errorf("row %s, not in the file, is shown:\n%s", other, f)
		}
	}
}

// S11.5 Given `~/.config/brl-stocks` is a symbolic link to a folder
// elsewhere, when the tool starts, then it reads the config through the
// link. (D29)
func TestS11_5_View_ConfigIsReadThroughALinkedFolder(t *testing.T) {
	// Given
	w, _ := symlinkedConfig(t, `tickers = ["ITUB4"]`+"\n")
	loadQuotes(w.src, w.now, true)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Then
	assertRowHas(t, f, "ITUB4", "R$ 34,15")
	if hasRow(f, "PETR4") {
		t.Errorf("row PETR4, not in the linked file, is shown:\n%s", f)
	}
}

// S11.7 Given `~/.config/brl-stocks` is a symbolic link, when the tool has
// run in any mode and exited, then the link still exists and points to the
// same target. (D29)
func TestS11_7_View_LinkSurvivesTheView(t *testing.T) {
	for _, key := range []string{"q", "ctrl+c"} {
		t.Run("quit with "+key, func(t *testing.T) {
			// Given
			w, target := symlinkedConfig(t, `tickers = ["PETR4"]`+"\n")
			loadQuotes(w.src, w.now, true)
			link := filepath.Dir(w.configPath)

			// When
			rowLine(t, openAndQuit(t, w, viewOpts{}, key), "PETR4")

			// Then
			fi, err := os.Lstat(link)
			if err != nil {
				t.Fatalf("the link %s is gone: %v", link, err)
			}
			if fi.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("%s is no longer a symbolic link (mode %v)", link, fi.Mode())
			}
			if got, err := os.Readlink(link); err != nil || got != target {
				t.Errorf("the link points to %q (err %v), want %q", got, err, target)
			}
		})
	}
}

// S11.8 Given a config file, when the tool has run in any mode and exited,
// then the file's content is unchanged, byte for byte. (D12, D35)
func TestS11_8_View_ConfigFileIsUnchanged(t *testing.T) {
	// A file with comments, odd spacing, an unknown key, lower case, a
	// repeated ticker and no final newline.
	const config = "# my watchlist\nrefresh_seconds   =  5   # too low on purpose\n" +
		"tickers = [ \"petr4\",\"AAPL\" ]\nmystery = 1\n\n[[group]]\nname = \"Brasil\"\ntickers = [\"VALE3\", \"VALE3\"]"
	for _, key := range []string{"q", "ctrl+c"} {
		t.Run("quit with "+key, func(t *testing.T) {
			// Given
			w := newWorld(t, bothOpen, config)
			loadQuotes(w.src, w.now, true)

			// When
			rowLine(t, openAndQuit(t, w, viewOpts{}, key), "PETR4")

			// Then
			got, err := os.ReadFile(w.configPath)
			if err != nil {
				t.Fatalf("the config file cannot be read after the run: %v", err)
			}
			if !bytes.Equal(got, []byte(config)) {
				t.Errorf("the config file changed.\nbefore: %q\nafter:  %q", config, got)
			}
		})
	}
}

const viewUnparsableConfig = "tickers = [\"PETR4\"\n[[group\n"

// S11.9 Given a config file that cannot be parsed, when the tool starts,
// then it exits with status 2. (D45)
func TestS11_9_View_UnparsableConfigExitsWithStatus2(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, viewUnparsableConfig)
	loadQuotes(w.src, w.now, true)

	// When
	v := openView(t, w, viewOpts{})
	code := v.exitStatus()

	// Then
	if code != 2 {
		t.Errorf("exit status %d, want 2\nstderr: %s", code, v.stderr.String())
	}
	endsBeforeTheView(t, v, w)
}

// S11.12 Given a config with a key the tool does not know, when the tool
// starts, then a warning names the key. (D50)
func TestS11_12_View_UnknownKeyIsNamedInAWarning(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, "tickers = [\"PETR4\"]\nfavourite_colour = \"blue\"\n")
	loadQuotes(w.src, w.now, true)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Then: in the view, warnings are in the status line (7.2)
	if _, st := statusLine(t, f); !strings.Contains(st, "favourite_colour") {
		t.Errorf("the status line does not name the key favourite_colour: %q\n%s", st, f)
	}
}

// S11.13 Given a config with 25 tickers, when the view is drawn, then all 25
// rows exist. (D8, D50)
func TestS11_13_View_TwentyFiveRowsAllExist(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, "")
	config, all := viewTwentyFive(w)
	if err := os.WriteFile(w.configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Then: one line each (40 lines on screen: they all fit)
	for _, tk := range all {
		rowIndex(t, f, tk)
	}
}

// S11.15 Given `XDG_CONFIG_HOME=/tmp/x` and a valid file at
// `/tmp/x/brl-stocks/config.toml`, when the tool starts without `--config`,
// then it shows that file's watchlist. (D50)
func TestS11_15_View_XDGConfigHomeIsRead(t *testing.T) {
	// Given: the file under XDG_CONFIG_HOME, and another one at
	// ~/.config/brl-stocks/config.toml
	w := newWorld(t, bothOpen, `tickers = ["ITUB4"]`+"\n")
	loadQuotes(w.src, w.now, true)
	home := filepath.Join(w.env["HOME"], ".config", "brl-stocks")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(`tickers = ["MSFT"]`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Then
	assertRowHas(t, f, "ITUB4", "R$ 34,15")
	if hasRow(f, "MSFT") {
		t.Errorf("row MSFT of ~/.config/brl-stocks/config.toml is shown:\n%s", f)
	}
}

// S11.16 Given a config file that cannot be parsed, when the tool starts,
// then a message on the error stream names the file and the problem. (D45)
func TestS11_16_View_UnparsableConfigIsNamedOnTheErrorStream(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, viewUnparsableConfig)

	// When
	v := openView(t, w, viewOpts{})
	v.exitStatus()

	// Then: the file. The spec fixes no wording for the problem: the line
	// naming the file must say more than the path.
	stderr := v.stderr.String()
	found := false
	for _, l := range lines(stderr) {
		if strings.Contains(l, w.configPath) {
			found = true
			if strings.TrimSpace(strings.ReplaceAll(l, w.configPath, "")) == "" {
				t.Errorf("the message names the file but no problem: %q", l)
			}
		}
	}
	if !found {
		t.Errorf("no line of the error stream names %s:\n%s", w.configPath, stderr)
	}
}

// S11.17 Given a config with a key the tool does not know, when the tool
// starts, then the watchlist is shown. (D50)
func TestS11_17_View_UnknownKeyStillShowsTheWatchlist(t *testing.T) {
	_, f := viewWith(t, "tickers = [\"PETR4\", \"AAPL\"]\nfavourite_colour = \"blue\"\n", nil)
	assertRowHas(t, f, "PETR4", "R$ 38,42")
	assertRowHas(t, f, "AAPL", "US$ 229.87")
}

// S11.18 Given a config with 25 tickers, when the view is drawn, then a
// warning in the status line says the tool is designed for 20. (D50)
func TestS11_18_View_TwentyFiveRowsWarn(t *testing.T) {
	// Given established: with 20 tickers the status line says nothing of 20
	w20 := newWorld(t, bothOpen, twentyConfig())
	loadTwenty(w20.src, w20.now)
	if _, st := statusLine(t, openView(t, w20, viewOpts{}).settled().text()); strings.Contains(st, "20") {
		t.Fatalf("given not established: the status line already mentions 20 with 20 tickers: %q", st)
	}

	// Given
	w := newWorld(t, bothOpen, "")
	config, _ := viewTwentyFive(w)
	if err := os.WriteFile(w.configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Then: the wording is not fixed; the warning names the 20
	if _, st := statusLine(t, f); !strings.Contains(st, "20") {
		t.Errorf("the status line does not say the tool is designed for 20: %q\n%s", st, f)
	}
}

// ---------------------------------------------------------------------------
// S12 - No config yet
// ---------------------------------------------------------------------------

// S12.1 Given no file at the default config path, when the tool starts,
// then it shows a built-in example watchlist. (D35)
func TestS12_1_View_NoConfigShowsTheExample(t *testing.T) {
	// Given
	w := newNoConfigWorld(t, bothOpen)
	loadQuotes(w.src, w.now, true)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Then: the example of 7.7, with the default summary line
	for _, tk := range viewExampleTickers {
		rowIndex(t, f, tk)
	}
	summaryLine(t, f, "IBOV", viewExampleTickers...)
	summaryLine(t, f, "S&P 500", viewExampleTickers...)
	summaryLine(t, f, "USD/BRL", viewExampleTickers...)
}

// S12.2 Given no file at the default config path, when the tool starts,
// then it names the path where a config file belongs. (D35)
func TestS12_2_View_NoConfigNamesThePath(t *testing.T) {
	// Given: a config home whose path is as short as a real one
	// (~/.config/brl-stocks/config.toml): the status line holds at most the
	// width, and the temporary folders of the test are long
	w := newNoConfigWorld(t, bothOpen)
	short, err := os.MkdirTemp("", "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(short) })
	w.env["XDG_CONFIG_HOME"] = short
	w.configPath = filepath.Join(short, "brl-stocks", "config.toml")
	loadQuotes(w.src, w.now, true)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Then: in the status line of the view (7.7)
	if _, st := statusLine(t, f); !strings.Contains(st, w.configPath) {
		t.Errorf("the status line does not name %s: %q\n%s", w.configPath, st, f)
	}
}

// S12.3 Given no file at the default config path, when the tool has run in
// any mode and exited, then no file or folder has been created under the
// config path. (D35)
func TestS12_3_View_NoConfigCreatesNothing(t *testing.T) {
	t.Run("XDG_CONFIG_HOME set", func(t *testing.T) {
		// Given: XDG_CONFIG_HOME set and empty
		w := newNoConfigWorld(t, bothOpen)
		loadQuotes(w.src, w.now, true)
		cfgHome := w.env["XDG_CONFIG_HOME"]
		if got := entriesUnder(t, cfgHome); len(got) != 0 {
			t.Fatalf("given not established: %s is not empty: %v", cfgHome, got)
		}

		// When
		rowLine(t, openAndQuit(t, w, viewOpts{}, "q"), "PETR4")

		// Then
		if got := entriesUnder(t, cfgHome); len(got) != 0 {
			t.Errorf("created under the config home %s: %v", cfgHome, got)
		}
	})
	t.Run("no XDG_CONFIG_HOME", func(t *testing.T) {
		// Given: nothing under ~/.config
		w := newNoConfigWorld(t, bothOpen)
		delete(w.env, "XDG_CONFIG_HOME")
		loadQuotes(w.src, w.now, true)
		dotConfig := filepath.Join(w.env["HOME"], ".config")

		// When
		rowLine(t, openAndQuit(t, w, viewOpts{}, "q"), "PETR4")

		// Then
		if got := entriesUnder(t, dotConfig); len(got) != 0 {
			t.Errorf("created under %s: %v", dotConfig, got)
		}
		if _, err := os.Stat(dotConfig); err == nil {
			t.Errorf("the folder %s was created", dotConfig)
		}
	})
}

// S12.4 Given `--config /tmp/missing.toml` and no such file, when the tool
// starts, then it exits with status 2. (D50)
func TestS12_4_View_MissingConfigFlagFileExitsWithStatus2(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	missing := filepath.Join(t.TempDir(), "missing.toml")

	// When
	v := openView(t, w, viewOpts{args: []string{"--config", missing}})
	code := v.exitStatus()

	// Then
	if code != 2 {
		t.Errorf("exit status %d, want 2\nstderr: %s", code, v.stderr.String())
	}
	endsBeforeTheView(t, v, w)
}

// S12.5 Given `--config /tmp/missing.toml` and no such file, when the tool
// starts, then a message on the error stream names the path. (D50)
func TestS12_5_View_MissingConfigFlagFileIsNamed(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	missing := filepath.Join(t.TempDir(), "missing.toml")

	// When
	v := openView(t, w, viewOpts{args: []string{"--config", missing}})
	v.exitStatus()

	// Then
	if !strings.Contains(v.stderr.String(), missing) {
		t.Errorf("the error stream does not name %s:\n%s", missing, v.stderr.String())
	}
}

// ---------------------------------------------------------------------------
// S15 - The last quotes are kept between runs
// ---------------------------------------------------------------------------

// S15.2 Given the tool has fetched quotes, when the files it keeps are
// listed, then they are under `~/.cache/brl-stocks/`. (D29)
func TestS15_2_View_KeptFilesAreUnderDotCache(t *testing.T) {
	// Given: no XDG_CACHE_HOME, so the cache is ~/.cache/brl-stocks (7.7);
	// the view fetched and was closed
	w := newWorld(t, bothOpen, plainConfig)
	delete(w.env, "XDG_CACHE_HOME")
	loadQuotes(w.src, w.now, true)
	home := w.env["HOME"]
	assertRowHas(t, openAndQuit(t, w, viewOpts{}, "q"), "PETR4", "R$ 38,42")

	// When
	kept := entriesUnder(t, home)

	// Then: some file is kept, and everything under HOME is ~/.cache or in
	// ~/.cache/brl-stocks
	dotCache := filepath.Join(home, ".cache")
	dir := filepath.Join(dotCache, "brl-stocks")
	files := 0
	for _, p := range kept {
		if p == dotCache || p == dir {
			continue
		}
		if !strings.HasPrefix(p, dir+string(filepath.Separator)) {
			t.Errorf("kept outside ~/.cache/brl-stocks/: %s", p)
			continue
		}
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			files++
		}
	}
	if files == 0 {
		t.Errorf("no file kept under %s after the view fetched quotes; under HOME: %v", dir, kept)
	}
}

// S15.5 Given a cache folder that cannot be written, when the view runs,
// then it shows quotes. (D50, D60)
func TestS15_5_View_UnwritableCacheStillShowsQuotes(t *testing.T) {
	// Given: a cache from a print 5 minutes ago, in a folder that can no
	// longer be written; the source now has other prices
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.lockCache()
	w.advance(5 * time.Minute)
	w.src.setPrice(petr4, 40.01)
	w.src.setPrice(aapl, 233.03)

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Then: the view fetched (D60), and shows what it fetched
	assertRowHas(t, f, "PETR4", "R$ 40,01")
	assertRowHas(t, f, "AAPL", "US$ 233.03")
}

// S15.6 Given a ticker was removed from the config, a refresh then
// succeeded, and the ticker is added back with no network, when the view is
// drawn, then the row shows no data. (D50)
func TestS15_6_View_RemovedThenAddedBackShowsNoData(t *testing.T) {
	// Given: each run reads its own file through --config, so that the
	// config the test started with is never touched (QR7): first PETR4 and
	// VALE3, then PETR4 alone, then both again
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	both := writeFile(t, "both.toml", `tickers = ["PETR4", "VALE3"]`+"\n")
	removed := writeFile(t, "removed.toml", `tickers = ["PETR4"]`+"\n")
	again := writeFile(t, "again.toml", `tickers = ["PETR4", "VALE3"]`+"\n")

	assertRowHas(t, openAndQuit(t, w, viewOpts{args: []string{"--config", both}}, "q"), "VALE3", "R$ 61,07")
	w.advance(5 * time.Minute)
	before := w.src.count(petr4)
	f2 := openAndQuit(t, w, viewOpts{args: []string{"--config", removed}}, "q")
	if hasRow(f2, "VALE3") || w.src.count(petr4) == before {
		t.Fatalf("given not established: the run without VALE3 showed it, or did not refresh PETR4:\n%s", f2)
	}
	w.advance(5 * time.Minute)
	w.src.failAll(provider.Unavailable)

	// When
	f := openView(t, w, viewOpts{args: []string{"--config", again}}).settled().text()

	// Given established: the cache was read (PETR4 shows its kept quote)
	assertRowHas(t, f, "PETR4", "R$ 38,42")

	// Then: VALE3 shows `—` in every data column (7.3, No data yet)
	fields := strings.Fields(rowLine(t, f, "VALE3"))
	if len(fields) < 2 {
		t.Fatalf("row VALE3 shows no data columns: %q", rowLine(t, f, "VALE3"))
	}
	for _, fl := range fields[1:] {
		if fl != "—" {
			t.Errorf("row VALE3 shows %q, want no data (`—` in every data column): %q", fl, rowLine(t, f, "VALE3"))
			break
		}
	}
}

// S15.8 Given a cache folder that cannot be written, when the tool runs,
// then one warning says the quotes will not be kept. (D50)
func TestS15_8_View_UnwritableCacheWarnsOnce(t *testing.T) {
	// Given: two worlds alike, one whose cache folder cannot be written
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.lockCache()
	w.advance(5 * time.Minute)

	control := newWorld(t, bothOpen, plainConfig)
	loadQuotes(control.src, control.now, true)
	control.prime(plainRows...)
	control.advance(5 * time.Minute)

	// When
	f := openView(t, w, viewOpts{}).settled().text()
	c := openView(t, control, viewOpts{}).settled().text()

	// Then: a warning, in the status line (7.2), that the writable one
	// does not show. Its wording is not fixed.
	_, st := statusLine(t, f)
	_, cst := statusLine(t, c)
	if st == cst {
		t.Errorf("the status line is the same with an unwritable cache folder, want a warning: %q", st)
	}
	extra := strings.TrimSpace(strings.Replace(st, cst, "", 1))
	if extra == "" {
		t.Errorf("no warning beyond the usual status line: %q (usual: %q)", st, cst)
	}
	if extra != "" && strings.Count(st, extra) != 1 {
		t.Errorf("the warning %q appears %d times in the status line, want once", extra, strings.Count(st, extra))
	}
}

// ---------------------------------------------------------------------------
// S17 - The data source can be replaced
// ---------------------------------------------------------------------------

// S17.3 Given the first version, when its config file and flags are listed,
// then none of them selects a data source. (D50)
func TestS17_3_View_NoFlagOrKeySelectsASource(t *testing.T) {
	t.Run("flags in the help", func(t *testing.T) {
		w := newWorld(t, bothOpen, plainConfig)
		r := runFlags(t, w, "--help")
		known := map[string]bool{"--config": true, "--json": true, "--refresh": true, "--help": true, "--version": true}
		flags := flagToken.FindAllString(r.stdout+r.stderr, -1)
		if len(flags) == 0 {
			t.Fatalf("given not established: the help lists no flag:\n%s%s", r.stdout, r.stderr)
		}
		for _, f := range flags {
			if !known[f] {
				t.Errorf("the help lists the flag %s, which spec 7.1 does not name", f)
			}
		}
	})
	t.Run("source flags are refused", func(t *testing.T) {
		for _, flag := range []string{"--source", "--provider"} {
			w := newWorld(t, bothOpen, plainConfig)
			loadQuotes(w.src, w.now, true)
			v := openView(t, w, viewOpts{args: []string{flag, "yahoo"}})
			if code := v.exitStatus(); code != 2 {
				t.Errorf("brl-stocks %s yahoo: exit status %d, want 2 (an unknown flag)", flag, code)
			}
		}
	})
	t.Run("source keys are unknown", func(t *testing.T) {
		for _, key := range []string{"source", "provider", "source_url"} {
			w := newWorld(t, bothOpen, "tickers = [\"PETR4\"]\n"+key+" = \"http://elsewhere.invalid\"\n")
			loadQuotes(w.src, w.now, true)
			f := openView(t, w, viewOpts{}).settled().text()
			// a key the tool does not know is named in a warning, in the
			// status line of the view (7.2), and the stand-in still answers
			assertRowHas(t, f, "PETR4", "R$ 38,42")
			if _, st := statusLine(t, f); !strings.Contains(st, key) {
				t.Errorf("the key %s raises no unknown-key warning, so the tool knows it; status line: %q", key, st)
			}
		}
	})
}
