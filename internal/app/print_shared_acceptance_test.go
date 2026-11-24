package app_test

// Acceptance tests, second batch: S13.22-S13.24, the criteria of other
// stories that hold in any mode (tested through `brl-stocks print`), and the
// not-found rows of spec 6.5 and 7.3. Written from docs/spec.md and the
// boundary alone; they reuse the stand-in source, the timer and the helpers
// of print_acceptance_test.go.

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lucattelli/brl-stocks/internal/app"
	"github.com/lucattelli/brl-stocks/internal/deps"
	"github.com/lucattelli/brl-stocks/internal/market"
	"github.com/lucattelli/brl-stocks/internal/provider"
)

// ---------------------------------------------------------------------------
// Helpers this batch needs
// ---------------------------------------------------------------------------

// hasRow reports whether a line of the table has ticker as its first word.
func hasRow(out, ticker string) bool {
	for _, l := range lines(out) {
		if f := strings.Fields(l); len(f) > 0 && f[0] == ticker {
			return true
		}
	}
	return false
}

// rowTickers lists the first word of every line that is one of the given
// tickers, in the order of the output.
func rowTickers(out string, tickers ...string) []string {
	want := map[string]bool{}
	for _, tk := range tickers {
		want[tk] = true
	}
	var got []string
	for _, l := range lines(out) {
		if f := strings.Fields(l); len(f) > 0 && want[f[0]] {
			got = append(got, f[0])
		}
	}
	return got
}

// newHomeWorld is newWorld without XDG_CONFIG_HOME: the default config path is
// then ~/.config/brl-stocks/config.toml (spec 7.2), where config is written.
func newHomeWorld(t *testing.T, start time.Time, config string) *world {
	t.Helper()
	w := newWorld(t, start, "")
	if err := os.RemoveAll(filepath.Dir(w.configPath)); err != nil {
		t.Fatal(err)
	}
	delete(w.env, "XDG_CONFIG_HOME")
	w.configPath = filepath.Join(w.env["HOME"], ".config", "brl-stocks", "config.toml")
	if err := os.MkdirAll(filepath.Dir(w.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return w
}

// newNoConfigWorld is newWorld with nothing at all under XDG_CONFIG_HOME.
func newNoConfigWorld(t *testing.T, start time.Time) *world {
	t.Helper()
	w := newWorld(t, start, "")
	if err := os.RemoveAll(filepath.Dir(w.configPath)); err != nil {
		t.Fatal(err)
	}
	return w
}

// writeFile writes a file in a temporary folder of the test and returns its
// path.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// entriesUnder lists every file and folder under dir, dir itself excluded.
func entriesUnder(t *testing.T, dir string) []string {
	t.Helper()
	var got []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != dir {
			got = append(got, p)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return got
}

// The ten calls of S13.22 and S13.23 run inside a testing/synctest bubble,
// with the bubble's own time.Now and time.After as the clock and the timer.
// Time in the bubble moves only when every goroutine in it is durably
// blocked, so a call that waits for another waits exactly as long as it
// would in real time, without the test choosing when its waits end.

// loadQuotesOpenAt loads quotes whose regular sessions surround now: B3, US
// and USD/BRL all open.
func loadQuotesOpenAt(src *fakeSource, now time.Time) {
	b3 := market.Session{Start: now.Add(-2 * time.Hour), End: now.Add(5 * time.Hour), TZ: "America/Sao_Paulo"}
	us := market.Session{Start: now.Add(-30 * time.Minute), End: now.Add(6 * time.Hour), TZ: "America/New_York"}
	fx := market.Session{Start: now.Add(-6 * time.Hour), End: now.Add(7 * time.Hour), TZ: "America/Sao_Paulo"}
	b3At, usAt := now.Add(-15*time.Minute), now.Add(-time.Second)
	src.answer(petr4, quote("Petrobras PN", "BRL", 38.42, 38.11, b3, b3At))
	src.answer(vale3, quote("Vale ON", "BRL", 61.07, 61.07, b3, b3At))
	src.answer(aapl, quote("Apple Inc.", "USD", 229.87, 230.99, us, usAt))
	src.answer(ibov, quote("Ibovespa", "", 158432.10, 157919.80, b3, b3At))
	src.answer(sp500, quote("S&P 500", "", 6812.40, 6819.90, us, usAt))
	src.answer(usdbrl, quote("USD/BRL", "BRL", 5.4321, 5.4278, fx, now.Add(-10*time.Second)))
}

// slowSource holds every answer of the stand-in for a while of bubble time,
// as a real source takes time to answer.
type slowSource struct {
	*fakeSource
	delay time.Duration
}

func (s slowSource) Quote(ctx context.Context, in market.Instrument) (market.Quote, error) {
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return market.Quote{}, &provider.Error{Kind: provider.Unavailable, Msg: "stand-in: no answer"}
	}
	return s.fakeSource.Quote(ctx, in)
}

// runTogetherInBubble must be called inside synctest.Test. It moves the
// bubble's clock to Monday 3 January 2000, 15:00 UTC, sets up a world whose
// markets are all open then, starts n `print` calls at the same moment
// (a barrier releases them together) and returns their results and the
// world.
func runTogetherInBubble(t *testing.T, n int, args ...string) ([]result, *world) {
	t.Helper()
	time.Sleep(time.Until(time.Date(2000, 1, 3, 15, 0, 0, 0, time.UTC)))
	w := newWorld(t, time.Now(), plainConfig)
	loadQuotesOpenAt(w.src, time.Now())
	src := slowSource{fakeSource: w.src, delay: 300 * time.Millisecond}

	results := make([]result, n)
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	ready.Add(n)
	done.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer done.Done()
			ready.Done()
			<-start
			var out, errOut bytes.Buffer
			code := app.Run(context.Background(), args, deps.Deps{
				Provider: src,
				Now:      time.Now,
				After:    time.After,
				LookupEnv: func(k string) (string, bool) {
					v, ok := w.env[k]
					return v, ok
				},
				Stdout: &out,
				Stderr: &errOut,
				IsTTY:  false,
			})
			results[i] = result{code: code, stdout: out.String(), stderr: errOut.String()}
		}(i)
	}
	ready.Wait()
	began := time.Now()
	close(start)
	done.Wait()
	t.Logf("%d calls took %v of bubble time; requests: PETR4 %d, VALE3 %d, AAPL %d, IBOV %d, SP500 %d, USDBRL %d",
		n, time.Since(began), w.src.count(petr4), w.src.count(vale3), w.src.count(aapl),
		w.src.count(ibov), w.src.count(sp500), w.src.count(usdbrl))
	return results, w
}

var causePhrases = []string{"source unreachable", "source is rate-limiting", "source refused access", "source answer not understood"}

// ---------------------------------------------------------------------------
// S13 - One-shot table (D73, D74)
// ---------------------------------------------------------------------------

// S13.22 Given an empty cache and a source that answers, when ten `brl-stocks
// print` calls are started at the same moment, then every one of them prints
// a quote for each row. (D73)
func TestS13_22_TenCallsTogetherEachPrintAQuoteForEveryRow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given, When
		results, _ := runTogetherInBubble(t, 10, "print")

		// Then
		want := map[string]string{"PETR4": "R$ 38,42", "VALE3": "R$ 61,07", "AAPL": "US$ 229.87"}
		for i, r := range results {
			if r.code != 0 {
				t.Errorf("call %d: exit status %d, want 0\nstderr: %s", i, r.code, r.stderr)
				continue
			}
			for ticker, price := range want {
				if !hasRow(r.stdout, ticker) {
					t.Errorf("call %d: no row %s:\n%s", i, ticker, r.stdout)
					continue
				}
				if line := rowLine(t, r.stdout, ticker); !strings.Contains(line, price) {
					t.Errorf("call %d: row %s shows no quote (%s): %q\nstderr: %s", i, ticker, price, line, r.stderr)
				}
			}
		}
	})
}

// S13.23 Given an empty cache and a source that answers, when ten `brl-stocks
// print` calls are started at the same moment, then the source receives one
// request per row. (D20, D49)
func TestS13_23_TenCallsTogetherSendOneRequestPerRow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given, When
		results, w := runTogetherInBubble(t, 10, "print")

		// Then: the rows of the watchlist (summary entries are not rows, spec 3)
		for _, r := range results {
			if r.code != 0 {
				t.Fatalf("a call exited with status %d, want 0\nstderr: %s", r.code, r.stderr)
			}
		}
		for _, in := range plainRows {
			if got := w.src.count(in); got != 1 {
				t.Errorf("%s requested %d times by ten calls together, want 1", in.Symbol, got)
			}
		}
	})
}

// S13.24 Given no cache and a source that cannot be reached, when `brl-stocks
// print` is run, then the last line of the output reads
// `B3 unknown · US unknown`. (D74)
func TestS13_24_NoQuoteAtAllMeansMarketsUnknown(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	w.src.failAll(provider.Unavailable)

	// When
	r := w.runOK("print")

	// Then
	ls := lines(r.stdout)
	if last := ls[len(ls)-1]; last != "B3 unknown · US unknown" {
		t.Errorf("last line reads %q, want %q", last, "B3 unknown · US unknown")
	}
}

// ---------------------------------------------------------------------------
// S3 - Two markets, plain tickers, own currency
// ---------------------------------------------------------------------------

// S3.1 Given the ticker `PETR4` in the config, when its quote is shown, then
// it is the B3 instrument, priced in `R$`. (D30, D10)
func TestS3_1_PETR4IsB3InReais(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, `tickers = ["PETR4"]`+"\n")
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then
	if w.src.count(petr4) == 0 {
		t.Errorf("the B3 instrument PETR4 was not requested")
	}
	if line := rowLine(t, r.stdout, "PETR4"); !strings.Contains(line, "R$ 38,42") {
		t.Errorf("row PETR4 is not priced in R$ (R$ 38,42): %q", line)
	}
}

// S3.2 Given the tickers `PETR4F`, `MXRF11` and `AAPL34`, when the config is
// loaded, then all three are treated as B3. (D30)
func TestS3_2_B3ShapesAreB3(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, `tickers = ["PETR4F", "MXRF11", "AAPL34"]`+"\n")
	loadQuotes(w.src, w.now, true)
	for _, s := range []string{"PETR4F", "MXRF11", "AAPL34"} {
		w.src.answer(market.Instrument{Market: market.B3, Symbol: s}, quote(s, "BRL", 10, 10, b3Session, w.now.Add(-15*time.Minute)))
	}

	// When
	w.runOK("print")

	// Then
	for _, s := range []string{"PETR4F", "MXRF11", "AAPL34"} {
		if w.src.count(market.Instrument{Market: market.B3, Symbol: s}) == 0 {
			t.Errorf("%s was not requested as a B3 instrument", s)
		}
		if n := w.src.count(market.Instrument{Market: market.US, Symbol: s}); n != 0 {
			t.Errorf("%s was requested %d times as a US instrument", s, n)
		}
	}
}

// S3.3 Given the tickers `AAPL`, `MSFT` and `F`, when the config is loaded,
// then all three are treated as US, priced in `US$`. (D30, D10)
func TestS3_3_OtherShapesAreUSInDollars(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, `tickers = ["AAPL", "MSFT", "F"]`+"\n")
	loadQuotes(w.src, w.now, true)
	ford := market.Instrument{Market: market.US, Symbol: "F"}
	w.src.answer(ford, quote("Ford Motor", "USD", 11.25, 11.10, usSession, w.now.Add(-time.Second)))

	// When
	r := w.runOK("print")

	// Then
	for in, price := range map[market.Instrument]string{aapl: "US$ 229.87", msft: "US$ 512.30", ford: "US$ 11.25"} {
		if w.src.count(in) == 0 {
			t.Errorf("%s was not requested as a US instrument", in.Symbol)
		}
		if n := w.src.count(market.Instrument{Market: market.B3, Symbol: in.Symbol}); n != 0 {
			t.Errorf("%s was requested %d times as a B3 instrument", in.Symbol, n)
		}
		if line := rowLine(t, r.stdout, in.Symbol); !strings.Contains(line, price) {
			t.Errorf("row %s is not priced in US$ (%s): %q", in.Symbol, price, line)
		}
	}
}

// S3.4 Given the ticker `US:ABCD1`, when the config is loaded, then it is
// treated as US although its shape is that of B3. (D30)
func TestS3_4_USPrefixOverridesB3Shape(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, `tickers = ["US:ABCD1"]`+"\n")
	loadQuotes(w.src, w.now, true)
	us := market.Instrument{Market: market.US, Symbol: "ABCD1"}
	w.src.answer(us, quote("Abcd Corp", "USD", 12.50, 12.00, usSession, w.now.Add(-time.Second)))

	// When
	w.runOK("print")

	// Then
	if w.src.count(us) == 0 {
		t.Errorf("ABCD1 was not requested as a US instrument")
	}
	if n := w.src.count(market.Instrument{Market: market.B3, Symbol: "ABCD1"}); n != 0 {
		t.Errorf("ABCD1 was requested %d times as a B3 instrument", n)
	}
}

// S3.5 Given the ticker `B3:XYZ`, when the config is loaded, then it is
// treated as B3 although its shape is not that of B3. (D30)
func TestS3_5_B3PrefixOverridesUSShape(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, `tickers = ["B3:XYZ"]`+"\n")
	loadQuotes(w.src, w.now, true)
	b3 := market.Instrument{Market: market.B3, Symbol: "XYZ"}
	w.src.answer(b3, quote("Xyz SA", "BRL", 7.30, 7.20, b3Session, w.now.Add(-15*time.Minute)))

	// When
	w.runOK("print")

	// Then
	if w.src.count(b3) == 0 {
		t.Errorf("XYZ was not requested as a B3 instrument")
	}
	if n := w.src.count(market.Instrument{Market: market.US, Symbol: "XYZ"}); n != 0 {
		t.Errorf("XYZ was requested %d times as a US instrument", n)
	}
}

// S3.7 Given the ticker written as `petr4`, when the row is drawn, then it
// shows `PETR4`. (D50)
func TestS3_7_LowerCaseTickerIsShownInUpperCase(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, `tickers = ["petr4"]`+"\n")
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then
	if !hasRow(r.stdout, "PETR4") {
		t.Errorf("no row shows PETR4:\n%s", r.stdout)
	}
}

// ---------------------------------------------------------------------------
// S4 - Groups
// ---------------------------------------------------------------------------

// S4.4 Given the same ticker in two groups, when a refresh runs, then the
// ticker is requested from the source once. (D50)
func TestS4_4_TickerInTwoGroupsIsRequestedOnce(t *testing.T) {
	// Given: an empty cache, so the print fetches every row (6.5)
	w := newWorld(t, bothOpen, `[[group]]
name = "Brasil"
tickers = ["PETR4", "VALE3"]

[[group]]
name = "Favourites"
tickers = ["PETR4", "AAPL"]
`)
	loadQuotes(w.src, w.now, true)

	// When
	w.runOK("print")

	// Then
	if w.src.count(vale3) == 0 || w.src.count(aapl) == 0 {
		t.Fatal("given not established: the print with no cache did not fetch the rows")
	}
	if got := w.src.count(petr4); got != 1 {
		t.Errorf("PETR4, in two groups, requested %d times, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// S11 - The watchlist is a file I edit
// ---------------------------------------------------------------------------

// S11.1 Given a valid file at `~/.config/brl-stocks/config.toml` and no
// `--config` flag, when the tool starts, then it shows that file's watchlist.
// (D29)
func TestS11_1_DefaultPathIsRead(t *testing.T) {
	// Given: a watchlist unlike the built-in example
	w := newHomeWorld(t, bothOpen, `tickers = ["ITUB4"]`+"\n")
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then
	if !hasRow(r.stdout, "ITUB4") {
		t.Errorf("row ITUB4 of the file is not shown:\n%s", r.stdout)
	}
	for _, other := range []string{"PETR4", "VALE3", "AAPL", "MSFT"} {
		if hasRow(r.stdout, other) {
			t.Errorf("row %s, not in the file, is shown:\n%s", other, r.stdout)
		}
	}
}

// S11.2 Given `--config /tmp/other.toml`, when the tool starts, then it shows
// the watchlist of that file. (D29)
func TestS11_2_ConfigFlagIsRead(t *testing.T) {
	// Given: a file at the default path, and another one named on the command line
	w := newWorld(t, bothOpen, `tickers = ["PETR4", "VALE3"]`+"\n")
	loadQuotes(w.src, w.now, true)
	other := writeFile(t, "other.toml", `tickers = ["MSFT"]`+"\n")

	// When
	r := w.runOK("--config", other, "print")

	// Then
	if !hasRow(r.stdout, "MSFT") {
		t.Errorf("row MSFT of %s is not shown:\n%s", other, r.stdout)
	}
	for _, tk := range []string{"PETR4", "VALE3"} {
		if hasRow(r.stdout, tk) {
			t.Errorf("row %s of the default file is shown:\n%s", tk, r.stdout)
		}
	}
}

// symlinkedConfig makes ~/.config/brl-stocks a symbolic link to a folder
// elsewhere that holds config.toml. It returns the world and the target.
func symlinkedConfig(t *testing.T, config string) (*world, string) {
	t.Helper()
	w := newHomeWorld(t, bothOpen, "")
	link := filepath.Dir(w.configPath)
	if err := os.RemoveAll(link); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "the-2026-dev-env", "brl-stocks")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	return w, target
}

// S11.5 Given `~/.config/brl-stocks` is a symbolic link to a folder
// elsewhere, when the tool starts, then it reads the config through the link.
// (D29)
func TestS11_5_ConfigIsReadThroughALinkedFolder(t *testing.T) {
	// Given
	w, _ := symlinkedConfig(t, `tickers = ["ITUB4"]`+"\n")
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then
	if !hasRow(r.stdout, "ITUB4") {
		t.Errorf("row ITUB4 of the linked file is not shown:\n%s", r.stdout)
	}
	if hasRow(r.stdout, "PETR4") {
		t.Errorf("row PETR4, not in the linked file, is shown:\n%s", r.stdout)
	}
}

// S11.7 Given `~/.config/brl-stocks` is a symbolic link, when the tool has run
// in any mode and exited, then the link still exists and points to the same
// target. (D29)
func TestS11_7_LinkSurvivesEveryMode(t *testing.T) {
	for _, args := range [][]string{{"print"}, {"print", "--json"}, {"print", "--refresh"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			// Given
			w, target := symlinkedConfig(t, `tickers = ["PETR4"]`+"\n")
			loadQuotes(w.src, w.now, true)
			link := filepath.Dir(w.configPath)

			// When
			w.runOK(args...)

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
func TestS11_8_ConfigFileIsUnchanged(t *testing.T) {
	// A file with comments, odd spacing, an unknown key, lower case, a
	// repeated ticker and no final newline: all things a tool that rewrote
	// the file would change.
	const config = "# my watchlist\nrefresh_seconds   =  5   # too low on purpose\n" +
		"tickers = [ \"petr4\",\"AAPL\" ]\nmystery = 1\n\n[[group]]\nname = \"Brasil\"\ntickers = [\"VALE3\", \"VALE3\"]"
	for _, args := range [][]string{{"print"}, {"print", "--json"}, {"print", "--refresh"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			// Given
			w := newWorld(t, bothOpen, config)
			loadQuotes(w.src, w.now, true)

			// When
			w.runOK(args...)

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

// S11.9 Given a config file that cannot be parsed, when the tool starts, then
// it exits with status 2. (D45)
func TestS11_9_UnparsableConfigExitsWithStatus2(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, "tickers = [\"PETR4\"\n[[group\n")
	loadQuotes(w.src, w.now, true)

	// When
	r := w.run("print")

	// Then
	if r.code != 2 {
		t.Errorf("exit status %d, want 2\nstderr: %s", r.code, r.stderr)
	}
}

// S11.12 Given a config with a key the tool does not know, when the tool
// starts, then a warning names the key. (D50)
func TestS11_12_UnknownKeyIsNamedInAWarning(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, "tickers = [\"PETR4\"]\nfavourite_colour = \"blue\"\n")
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then: one-shot warnings go to the error stream (7.2)
	if !strings.Contains(r.stderr, "favourite_colour") {
		t.Errorf("no warning names the key favourite_colour; error stream: %q", r.stderr)
	}
}

// S11.15 Given `XDG_CONFIG_HOME=/tmp/x` and a valid file at
// `/tmp/x/brl-stocks/config.toml`, when the tool starts without `--config`,
// then it shows that file's watchlist. (D50)
func TestS11_15_XDGConfigHomeIsRead(t *testing.T) {
	// Given: the file under XDG_CONFIG_HOME, and another one at
	// ~/.config/brl-stocks/config.toml, which 7.2 says is then not the path
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
	r := w.runOK("print")

	// Then
	if !hasRow(r.stdout, "ITUB4") {
		t.Errorf("row ITUB4 of the file under XDG_CONFIG_HOME is not shown:\n%s", r.stdout)
	}
	if hasRow(r.stdout, "MSFT") {
		t.Errorf("row MSFT of ~/.config/brl-stocks/config.toml is shown:\n%s", r.stdout)
	}
}

// S11.16 Given a config file that cannot be parsed, when the tool starts, then
// a message on the error stream names the file and the problem. (D45)
func TestS11_16_UnparsableConfigIsNamedOnTheErrorStream(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, "tickers = [\"PETR4\"\n[[group\n")

	// When
	r := w.run("print")

	// Then: the file. The spec fixes no wording for the problem, so all that
	// can be checked is that the line naming the file says more than the path.
	found := false
	for _, l := range lines(r.stderr) {
		if strings.Contains(l, w.configPath) {
			found = true
			if strings.TrimSpace(strings.ReplaceAll(l, w.configPath, "")) == "" {
				t.Errorf("the message names the file but no problem: %q", l)
			}
		}
	}
	if !found {
		t.Errorf("no line of the error stream names %s:\n%s", w.configPath, r.stderr)
	}
}

// S11.17 Given a config with a key the tool does not know, when the tool
// starts, then the watchlist is shown. (D50)
func TestS11_17_UnknownKeyStillShowsTheWatchlist(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, "tickers = [\"PETR4\", \"AAPL\"]\nfavourite_colour = \"blue\"\n")
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then
	for _, tk := range []string{"PETR4", "AAPL"} {
		if !hasRow(r.stdout, tk) {
			t.Errorf("row %s is not shown:\n%s", tk, r.stdout)
		}
	}
}

// ---------------------------------------------------------------------------
// S12 - No config yet
// ---------------------------------------------------------------------------

// S12.1 Given no file at the default config path, when the tool starts, then
// it shows a built-in example watchlist. (D35)
func TestS12_1_NoConfigShowsTheExample(t *testing.T) {
	// Given
	w := newNoConfigWorld(t, bothOpen)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then: the example of 7.7
	for _, tk := range []string{"PETR4", "VALE3", "ITUB4", "AAPL", "MSFT"} {
		if !hasRow(r.stdout, tk) {
			t.Errorf("row %s of the built-in example is not shown:\n%s", tk, r.stdout)
		}
	}
}

// S12.2 Given no file at the default config path, when the tool starts, then
// it names the path where a config file belongs. (D35)
func TestS12_2_NoConfigNamesThePath(t *testing.T) {
	// Given
	w := newNoConfigWorld(t, bothOpen)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then: on the error stream in one-shot (7.7)
	if !strings.Contains(r.stderr, w.configPath) {
		t.Errorf("the error stream does not name %s:\n%s", w.configPath, r.stderr)
	}
}

// S12.3 Given no file at the default config path, when the tool has run in
// any mode and exited, then no file or folder has been created under the
// config path. (D35)
func TestS12_3_NoConfigCreatesNothing(t *testing.T) {
	modes := [][]string{{"print"}, {"print", "--json"}, {"print", "--refresh"}, {"--help"}, {"--version"}}
	for _, args := range modes {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			// Given: XDG_CONFIG_HOME set and empty
			w := newNoConfigWorld(t, bothOpen)
			loadQuotes(w.src, w.now, true)
			cfgHome := w.env["XDG_CONFIG_HOME"]
			if got := entriesUnder(t, cfgHome); len(got) != 0 {
				t.Fatalf("given not established: %s is not empty: %v", cfgHome, got)
			}

			// When
			r := w.runOK(args...)

			// Then
			if args[0] == "print" && args[len(args)-1] != "--json" && !hasRow(r.stdout, "PETR4") {
				t.Fatalf("given not established: the run did not show the example:\n%s", r.stdout)
			}
			if got := entriesUnder(t, cfgHome); len(got) != 0 {
				t.Errorf("created under the config home %s: %v", cfgHome, got)
			}
		})
	}
	for _, args := range modes {
		t.Run("no XDG_CONFIG_HOME/"+strings.Join(args, " "), func(t *testing.T) {
			// Given: no XDG_CONFIG_HOME, nothing under ~/.config
			w := newNoConfigWorld(t, bothOpen)
			delete(w.env, "XDG_CONFIG_HOME")
			loadQuotes(w.src, w.now, true)
			dotConfig := filepath.Join(w.env["HOME"], ".config")

			// When
			w.runOK(args...)

			// Then
			if got := entriesUnder(t, dotConfig); len(got) != 0 {
				t.Errorf("created under %s: %v", dotConfig, got)
			}
			if _, err := os.Stat(filepath.Join(dotConfig, "brl-stocks")); err == nil {
				t.Errorf("the folder %s was created", filepath.Join(dotConfig, "brl-stocks"))
			}
		})
	}
}

// S12.4 Given `--config /tmp/missing.toml` and no such file, when the tool
// starts, then it exits with status 2. (D50)
func TestS12_4_MissingConfigFlagFileExitsWithStatus2(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	missing := filepath.Join(t.TempDir(), "missing.toml")

	// When
	r := w.run("--config", missing, "print")

	// Then
	if r.code != 2 {
		t.Errorf("exit status %d, want 2\nstderr: %s", r.code, r.stderr)
	}
}

// S12.5 Given `--config /tmp/missing.toml` and no such file, when the tool
// starts, then a message on the error stream names the path. (D50)
func TestS12_5_MissingConfigFlagFileIsNamed(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	missing := filepath.Join(t.TempDir(), "missing.toml")

	// When
	r := w.run("--config", missing, "print")

	// Then
	if !strings.Contains(r.stderr, missing) {
		t.Errorf("the error stream does not name %s:\n%s", missing, r.stderr)
	}
}

// ---------------------------------------------------------------------------
// S17 - The data source can be replaced
// ---------------------------------------------------------------------------

// S17.2 Given a stand-in source put in place of the default one, when the
// tool runs with an unchanged config file, then the same rows are shown with
// the stand-in's quotes. (D7, D17)
func TestS17_2_StandInSourceShowsTheSameRowsWithItsQuotes(t *testing.T) {
	// Given: one config file, run against one stand-in and then another
	const config = `tickers = ["PETR4", "AAPL"]

[[group]]
name = "Brasil"
tickers = ["VALE3"]
`
	first := newWorld(t, bothOpen, config)
	loadQuotes(first.src, first.now, true)
	second := newWorld(t, bothOpen, config)
	loadQuotes(second.src, second.now, true)
	second.src.setPrice(petr4, 40.01)
	second.src.setPrice(vale3, 62.02)
	second.src.setPrice(aapl, 233.03)

	// When
	a, b := first.runOK("print"), second.runOK("print")

	// Then: the same rows
	ra := rowTickers(a.stdout, "PETR4", "AAPL", "VALE3")
	rb := rowTickers(b.stdout, "PETR4", "AAPL", "VALE3")
	if strings.Join(ra, " ") != "PETR4 AAPL VALE3" || strings.Join(ra, " ") != strings.Join(rb, " ") {
		t.Errorf("rows differ: %v with one source, %v with the other, want PETR4 AAPL VALE3 for both", ra, rb)
	}
	// Then: each with its own source's quotes
	for tk, price := range map[string]string{"PETR4": "R$ 40,01", "VALE3": "R$ 62,02", "AAPL": "US$ 233.03"} {
		if line := rowLine(t, b.stdout, tk); !strings.Contains(line, price) {
			t.Errorf("row %s does not show the stand-in's quote %s: %q", tk, price, line)
		}
	}
	for tk, price := range map[string]string{"PETR4": "R$ 38,42", "VALE3": "R$ 61,07", "AAPL": "US$ 229.87"} {
		if line := rowLine(t, a.stdout, tk); !strings.Contains(line, price) {
			t.Errorf("row %s does not show the first stand-in's quote %s: %q", tk, price, line)
		}
	}
}

var flagToken = regexp.MustCompile(`--[a-zA-Z][a-zA-Z0-9-]*`)

// S17.3 Given the first version, when its config file and flags are listed,
// then none of them selects a data source. (D50)
func TestS17_3_NoFlagOrKeySelectsASource(t *testing.T) {
	t.Run("flags in the help", func(t *testing.T) {
		// Given
		w := newWorld(t, bothOpen, plainConfig)

		// When
		r := w.runOK("--help")

		// Then: every flag the help lists is one of 7.1, none of which
		// selects a source
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
			if r := w.run(flag, "yahoo", "print"); r.code != 2 {
				t.Errorf("%s yahoo print: exit status %d, want 2 (an unknown flag)", flag, r.code)
			}
		}
	})
	t.Run("source keys are unknown", func(t *testing.T) {
		for _, key := range []string{"source", "provider", "source_url"} {
			// Given
			w := newWorld(t, bothOpen, "tickers = [\"PETR4\"]\n"+key+" = \"http://elsewhere.invalid\"\n")
			loadQuotes(w.src, w.now, true)

			// When
			r := w.runOK("print")

			// Then: a key the tool does not know is named in a warning (7.2)
			if !strings.Contains(r.stderr, key) {
				t.Errorf("the key %s raises no unknown-key warning, so the tool knows it; error stream: %q", key, r.stderr)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// The not-found row in print (spec 6.5 and 7.3)
// ---------------------------------------------------------------------------

var xxxx9 = market.Instrument{Market: market.B3, Symbol: "XXXX9"}

const notFoundConfig = `tickers = ["PETR4", "XXXX9"]` + "\n"

// 6.5: "Not found, tried less than an hour ago, config unchanged | No fetch;
// prints "not found"" (D39)
func TestW6_5_NotFound_TriedLessThanAnHourAgoIsNotFetched(t *testing.T) {
	// Given: a print that found the source does not know XXXX9
	w := newWorld(t, bothOpen, notFoundConfig)
	loadQuotes(w.src, w.now, true)
	w.runOK("print")
	if w.src.count(xxxx9) == 0 {
		t.Fatal("given not established: the print with no cache did not request XXXX9")
	}
	w.advance(30 * time.Minute)
	before := w.src.count(xxxx9)

	// When
	r := w.runOK("print")

	// Then
	if got := w.src.count(xxxx9) - before; got != 0 {
		t.Errorf("XXXX9, not found 30 minutes ago, requested %d times, want 0", got)
	}
	if line := rowLine(t, r.stdout, "XXXX9"); !strings.Contains(line, "not found") {
		t.Errorf("row XXXX9 does not print \"not found\": %q", line)
	}
}

// 7.3: "Not found. "not found" in place of the price; `—` in the other data
// columns (D39, D50)." Through 7.4, the one-shot table marks it the same way.
func TestW6_5_NotFound_RowShowsNotFoundAndDashes(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, notFoundConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then
	line := rowLine(t, r.stdout, "XXXX9")
	if !strings.Contains(line, "not found") {
		t.Errorf("row XXXX9 does not show \"not found\": %q", line)
	}
	if strings.Contains(line, "R$") || strings.Contains(line, "US$") {
		t.Errorf("row XXXX9 shows a price: %q", line)
	}
	// The data columns of the table other than the price: name, change
	// value, change percent, low, high, quote time (7.3, 7.4).
	if got := strings.Count(line, "—"); got != 6 {
		t.Errorf("row XXXX9 has %d `—`, want one in each of the 6 other data columns: %q", got, line)
	}
	// "in place of the price": it ends where the prices of the other rows end
	_, priceEnd := span(t, rowLine(t, r.stdout, "PETR4"), "R$ 38,42")
	if _, end := span(t, line, "not found"); end != priceEnd {
		t.Errorf("\"not found\" ends at column %d, the price of PETR4 at %d:\n%s", end, priceEnd, r.stdout)
	}
}

// 7.3, error line: "Not-found rows do not raise it (D50)." In one-shot the
// cause of a failure goes to the error stream (7.4).
func TestW6_5_NotFound_RaisesNoErrorLine(t *testing.T) {
	// Given: every other row answers
	w := newWorld(t, bothOpen, notFoundConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then
	if line := rowLine(t, r.stdout, "XXXX9"); !strings.Contains(line, "not found") {
		t.Fatalf("given not established: row XXXX9 is not shown as not found: %q", line)
	}
	for _, phrase := range causePhrases {
		if n := linesStarting(r.stderr, phrase); n != 0 {
			t.Errorf("a not-found row raised the error line %q:\n%s", phrase, r.stderr)
		}
	}
}
