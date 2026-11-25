package app_test

// Acceptance tests of slice 3, increment 4: the config file changed while the
// view is open (spec 6.4, 7.2). Written from docs/spec.md and the view's test
// boundary (docs/design.md, "Time"). The test writes or deletes the file in
// its temporary folders while the view runs, and gives every file it writes
// the clock's time as its modification time; it lets time pass with Elapse
// before each save, so a save is strictly newer than what came before.
// "Within 5 seconds" is a limit in view time: the tests look at the frame
// after at most 5 seconds of Elapse.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucattelli/brl-stocks/internal/provider"
)

// ---------------------------------------------------------------------------
// Helpers of this increment
// ---------------------------------------------------------------------------

// configFile is the config file a test edits while the view runs. The tool
// only reads it (QR7): when the test ends, the file holds what the test last
// wrote, or is absent if the test deleted it.
type configFile struct {
	t    *testing.T
	v    *clockRun
	path string
	last []byte // nil: deleted
}

// openEditable opens the view on the world's clock, for a test that edits
// the config file at path. openOnClock's own check that the default config
// never changes does not apply: the test changes it; configFile checks
// instead that only the test did.
func openEditable(t *testing.T, w *world, path string, args ...string) (*clockRun, *configFile) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		content = nil
	}
	wv := *w
	wv.configPath = ""
	v := openOnClock(t, &wv, args...)
	c := &configFile{t: t, v: v, path: path, last: content}
	t.Cleanup(func() {
		got, err := os.ReadFile(c.path)
		switch {
		case c.last == nil && err == nil:
			t.Errorf("QR7: the config file %s exists after the test deleted it", c.path)
		case c.last != nil && (err != nil || !bytes.Equal(got, c.last)):
			t.Errorf("QR7: the config file %s is not what the test last wrote (err: %v)", c.path, err)
		}
	})
	return v, c
}

// save lets 2 seconds pass, then writes content to the file (creating its
// folder if needed) with the clock's time as its modification time.
func (c *configFile) save(content string) time.Time {
	c.t.Helper()
	c.v.elapse(2 * time.Second)
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		c.t.Fatal(err)
	}
	if err := os.WriteFile(c.path, []byte(content), 0o644); err != nil {
		c.t.Fatal(err)
	}
	now := c.v.clock.Now()
	if err := os.Chtimes(c.path, now, now); err != nil {
		c.t.Fatal(err)
	}
	c.last = []byte(content)
	return now
}

// remove lets 2 seconds pass, then deletes the file.
func (c *configFile) remove() time.Time {
	c.t.Helper()
	c.v.elapse(2 * time.Second)
	if err := os.Remove(c.path); err != nil {
		c.t.Fatal(err)
	}
	c.last = nil
	return c.v.clock.Now()
}

// within5s is the frame after 5 seconds of view time.
func (c *configFile) within5s() string {
	c.t.Helper()
	return c.v.elapse(5 * time.Second).text()
}

func assertRows(t *testing.T, f string, present []string, absent []string) {
	t.Helper()
	for _, tk := range present {
		if !hasRow(f, tk) {
			t.Errorf("no row for %s:\n%s", tk, f)
		}
	}
	for _, tk := range absent {
		if hasRow(f, tk) {
			t.Errorf("a row for %s is still shown:\n%s", tk, f)
		}
	}
}

func tickersConfig(tickers ...string) string {
	return `tickers = ["` + strings.Join(tickers, `", "`) + `"]` + "\n"
}

// errorLineNaming is the one error line of the frame, which must name path.
func errorLineNaming(t *testing.T, f, path string, tickers ...string) string {
	t.Helper()
	line := theErrorLine(t, f, tickers...)
	if !strings.Contains(line, path) {
		t.Errorf("the error line does not name the file %s: %q", path, line)
	}
	return line
}

// ---------------------------------------------------------------------------
// S8 - A ticker the source does not know
// ---------------------------------------------------------------------------

// S8.4 Given a not-found row, when the config file changes, then the ticker
// is requested again without waiting for the hour. (D39)
func TestS8_4_ViewOpen_NotFoundRetriedAfterAConfigChange(t *testing.T) {
	// Given: XXXX9 not found by the fetch at start; two minutes pass
	w := clockWorld(t, bothOpen, notFoundConfig)
	loadQuotes(w.src, w.now, true)
	v, c := openEditable(t, w, w.configPath)
	f := v.settled(viewGuard)
	if line := rowLine(t, f.text(), "XXXX9"); !strings.Contains(line, "not found") {
		t.Fatalf("given not established: XXXX9 is not shown as not found: %q", line)
	}
	v.elapse(2 * time.Minute)
	if n := len(v.src.times(xxxx9)); n != 1 {
		t.Fatalf("given not established: XXXX9 was requested %d times before the change, want once (at %v)", n, v.src.times(xxxx9))
	}

	// When: VALE3 is added
	saved := c.save(tickersConfig("PETR4", "XXXX9", "VALE3"))
	c.within5s()

	// Then
	if n := v.src.requestedFrom(xxxx9, saved, saved.Add(5*time.Second)); n == 0 {
		t.Errorf("not-found XXXX9 was not requested again within 5 seconds of the config change at %v (requested at %v)", saved.UTC(), v.src.times(xxxx9))
	}
}

// ---------------------------------------------------------------------------
// S11 - The watchlist is a file I edit
// ---------------------------------------------------------------------------

// S11.3 Given the view is open, when a ticker is added to the config file and
// the file is saved, then a row for it appears within 5 seconds, without a
// restart. (D12, D50)
func TestS11_3_ViewOpen_AddedTickerAppears(t *testing.T) {
	nineteen := twentyTickers()[:19]
	for _, c := range []struct {
		name       string
		before     []string
		added      string
		loadTwenty bool
	}{
		{"criterion", []string{"PETR4", "VALE3", "AAPL"}, "ITUB4", false},
		{"20 tickers", nineteen, twentyTickers()[19], true},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := clockWorld(t, bothOpen, tickersConfig(c.before...))
			if c.loadTwenty {
				loadTwenty(w.src, w.now)
			} else {
				loadQuotes(w.src, w.now, true)
			}
			v, cf := openEditable(t, w, w.configPath)
			f := v.settled(viewGuard).text()
			if hasRow(f, c.added) {
				t.Fatalf("given not established: a row for %s before it was added:\n%s", c.added, f)
			}

			// When
			cf.save(tickersConfig(append(append([]string{}, c.before...), c.added)...))
			f = cf.within5s()

			// Then
			assertRows(t, f, append([]string{c.added}, c.before...), nil)
			if done, _ := v.exitedYet(); done {
				t.Errorf("the view ended: a restart would be needed")
			}
		})
	}
}

// exitedYet reports whether the view has ended.
func (r *clockRun) exitedYet() (bool, int) {
	select {
	case <-r.done:
		r.mu.Lock()
		defer r.mu.Unlock()
		return true, r.code
	default:
		return false, 0
	}
}

// S11.4 Given the view is open, when a ticker is removed from the config file
// and the file is saved, then its row disappears within 5 seconds. (D12, D50)
func TestS11_4_ViewOpen_RemovedTickerDisappears(t *testing.T) {
	w := clockWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	v, c := openEditable(t, w, w.configPath)
	if f := v.settled(viewGuard).text(); !hasRow(f, "VALE3") {
		t.Fatalf("given not established: no VALE3 row before it was removed:\n%s", f)
	}

	// When
	c.save(tickersConfig("PETR4", "AAPL"))
	f := c.within5s()

	// Then
	assertRows(t, f, []string{"PETR4", "AAPL"}, []string{"VALE3"})
}

// S11.6 Given `~/.config/brl-stocks` is a symbolic link and the view is open,
// when a ticker is added to the file in the link's target, then a row for it
// appears within 5 seconds. (D12, D29, D50)
func TestS11_6_ViewOpen_TickerAddedThroughALinkedFolder(t *testing.T) {
	// Given: ~/.config/brl-stocks links to a folder elsewhere; the default
	// config path (no XDG_CONFIG_HOME) goes through it
	w, target := symlinkedConfig(t, tickersConfig("PETR4", "AAPL"))
	inTarget := filepath.Join(target, "config.toml")
	if err := os.Chtimes(inTarget, w.now, w.now); err != nil {
		t.Fatal(err)
	}
	loadQuotes(w.src, w.now, true)
	link := filepath.Dir(w.configPath)
	dest, err := os.Readlink(link)
	if err != nil || dest != target {
		t.Fatalf("given not established: %s is not a link to %s (%q, %v)", link, target, dest, err)
	}
	v, c := openEditable(t, w, inTarget)
	if f := v.settled(viewGuard).text(); !hasRow(f, "PETR4") || hasRow(f, "ITUB4") {
		t.Fatalf("given not established: the view does not show the linked watchlist:\n%s", f)
	}

	// When
	c.save(tickersConfig("PETR4", "AAPL", "ITUB4"))
	f := c.within5s()

	// Then
	assertRows(t, f, []string{"ITUB4", "PETR4", "AAPL"}, nil)
	if dest, err := os.Readlink(link); err != nil || dest != target {
		t.Errorf("QR7: the link %s no longer points to %s (%q, %v)", link, target, dest, err)
	}
}

// savedBroken opens the view on plainConfig, then saves the file with a
// syntax error and returns the frame 5 seconds later and the path.
func savedBroken(t *testing.T) (string, string) {
	t.Helper()
	w := clockWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	v, c := openEditable(t, w, w.configPath)
	if f := v.settled(viewGuard).text(); !hasRow(f, "VALE3") || !hasRow(f, "AAPL") {
		t.Fatalf("given not established: the watchlist is not shown:\n%s", f)
	}
	c.save(viewUnparsableConfig)
	return c.within5s(), w.configPath
}

// S11.10 Given the view is open, when the config file is saved with a syntax
// error, then the view keeps the last valid watchlist. (D45)
func TestS11_10_ViewOpen_BrokenSaveKeepsTheWatchlist(t *testing.T) {
	f, path := savedBroken(t)

	// Given established: the view saw the save (6.4: the error line names
	// the file), so the rows it keeps are kept, not unseen
	errorLineNaming(t, f, path, "PETR4", "VALE3", "AAPL")

	// Then
	assertRows(t, f, []string{"PETR4", "VALE3", "AAPL"}, nil)
}

// S11.11 Given the view is open, when the config file is saved with a syntax
// error, then the error line names the file and the problem. (D45)
func TestS11_11_ViewOpen_BrokenSaveNamedInTheErrorLine(t *testing.T) {
	f, path := savedBroken(t)

	// Then: the spec fixes no wording for the problem: the line must say
	// more than the path
	line := errorLineNaming(t, f, path, "PETR4", "VALE3", "AAPL")
	if strings.TrimSpace(strings.Trim(strings.ReplaceAll(line, path, ""), " :·-")) == "" {
		t.Errorf("the error line names the file but no problem: %q", line)
	}
}

// deleted opens the view on plainConfig, then deletes the file and returns
// the frame 5 seconds later and the path.
func deleted(t *testing.T) (string, string) {
	t.Helper()
	w := clockWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	v, c := openEditable(t, w, w.configPath)
	if f := v.settled(viewGuard).text(); !hasRow(f, "VALE3") || !hasRow(f, "AAPL") {
		t.Fatalf("given not established: the watchlist is not shown:\n%s", f)
	}
	c.remove()
	return c.within5s(), w.configPath
}

// goneWords are ways to say that a file is gone.
var goneWords = []string{"gone", "missing", "deleted", "removed", "not found", "no such file", "does not exist"}

// S11.14 Given the view is open, when the config file is deleted, then the
// view keeps the last loaded watchlist. (D50)
func TestS11_14_ViewOpen_DeletedFileKeepsTheWatchlist(t *testing.T) {
	f, path := deleted(t)

	// Given established: the view saw the deletion (6.4: the error line
	// says the file is gone), so the rows it keeps are kept, not unseen
	errorLineNaming(t, f, path, "PETR4", "VALE3", "AAPL")

	// Then: and not the example watchlist in its place
	assertRows(t, f, []string{"PETR4", "VALE3", "AAPL"}, []string{"ITUB4", "MSFT"})
}

// S11.19 Given the view is open, when the config file is deleted, then the
// error line says the file is gone. (D50)
func TestS11_19_ViewOpen_DeletedFileErrorLine(t *testing.T) {
	f, path := deleted(t)
	line := errorLineNaming(t, f, path, "PETR4", "VALE3", "AAPL")
	found := false
	for _, wd := range goneWords {
		if strings.Contains(strings.ToLower(line), wd) {
			found = true
		}
	}
	if !found {
		t.Errorf("the error line does not say the file is gone (any of %q): %q", goneWords, line)
	}
}

// S11.20 Given no file at the default config path and the view open with the
// example watchlist, when a valid config file is saved at that path, then its
// watchlist replaces the example within 5 seconds. (D12, D50)
func TestS11_20_ViewOpen_ConfigAppearsInPlaceOfTheExample(t *testing.T) {
	// Given: nothing under XDG_CONFIG_HOME
	w := newNoConfigWorld(t, bothOpen)
	loadQuotes(w.src, w.now, true)
	v, c := openEditable(t, w, w.configPath)
	f := v.settled(viewGuard).text()
	for _, tk := range viewExampleTickers {
		if !hasRow(f, tk) {
			t.Fatalf("given not established: the example watchlist is not shown (no %s row):\n%s", tk, f)
		}
	}

	// When
	c.save(tickersConfig("PETR4", "AAPL"))
	f = c.within5s()

	// Then
	assertRows(t, f, []string{"PETR4", "AAPL"}, []string{"VALE3", "ITUB4", "MSFT"})
}

// ---------------------------------------------------------------------------
// S15 - The last quotes are kept between runs, shown again while the view
// is open
// ---------------------------------------------------------------------------

// S15.6 Given a ticker was removed from the config, a refresh then
// succeeded, and the ticker is added back with no network, when the view is
// drawn, then the row shows no data. (D50)
func TestS15_6_ViewOpen_RemovedThenAddedBackShowsNoData(t *testing.T) {
	// Given: VALE3 removed while the view is open; a refresh then succeeds
	w := clockWorld(t, bothOpen, tickersConfig("PETR4", "VALE3"))
	loadQuotes(w.src, w.now, true)
	v, c := openEditable(t, w, w.configPath)
	if f := v.settled(viewGuard).text(); !strings.Contains(rowLine(t, f, "VALE3"), "R$ 61,07") {
		t.Fatalf("given not established: VALE3 does not show its quote:\n%s", f)
	}
	removed := c.save(tickersConfig("PETR4"))
	if f := c.within5s(); hasRow(f, "VALE3") {
		t.Fatalf("given not established: VALE3 is still shown after its removal:\n%s", f)
	}
	f := v.elapse(time.Minute).text()
	if v.src.requestedIn(petr4, removed, v.clock.Now()) == 0 || viewStaleMark.MatchString(rowLine(t, f, "PETR4")) {
		t.Fatalf("given not established: no refresh succeeded after the removal (PETR4 at %v):\n%s", v.src.times(petr4), f)
	}

	// When: no network, and VALE3 is added back
	w.src.failAll(provider.Unavailable)
	c.save(tickersConfig("PETR4", "VALE3"))
	f2 := c.within5s()

	// Given established: the cache's quote for PETR4 is still shown
	assertRowHas(t, f2, "PETR4", "R$ 38,42")

	// Then: VALE3 shows `—` in every data column (7.3, No data yet)
	fields := strings.Fields(rowLine(t, f2, "VALE3"))
	if len(fields) < 2 {
		t.Fatalf("row VALE3 shows no data columns: %q", rowLine(t, f2, "VALE3"))
	}
	for _, fl := range fields[1:] {
		if fl != "—" {
			t.Errorf("row VALE3 shows %q, want no data (`—` in every data column): %q", fl, rowLine(t, f2, "VALE3"))
			break
		}
	}
}
