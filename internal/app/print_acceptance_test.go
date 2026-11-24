package app_test

// Acceptance tests of the one-shot mode (`brl-stocks print`), written from
// docs/spec.md before the implementation. They drive the tool only through
// app.Run and deps.Deps: a stand-in source, a clock the test moves, and
// temporary config and cache folders.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lucattelli/brl-stocks/internal/app"
	"github.com/lucattelli/brl-stocks/internal/deps"
	"github.com/lucattelli/brl-stocks/internal/market"
	"github.com/lucattelli/brl-stocks/internal/provider"
)

// ---------------------------------------------------------------------------
// The stand-in source
// ---------------------------------------------------------------------------

type fakeSource struct {
	mu      sync.Mutex
	quotes  map[market.Instrument]market.Quote
	failing map[market.Instrument]provider.Kind
	down    provider.Kind // when set, every request fails with this kind
	counts  map[market.Instrument]int
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		quotes:  map[market.Instrument]market.Quote{},
		failing: map[market.Instrument]provider.Kind{},
		counts:  map[market.Instrument]int{},
	}
}

func (f *fakeSource) Name() string { return "stand-in" }

func (f *fakeSource) Quote(_ context.Context, in market.Instrument) (market.Quote, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[in]++
	if f.down != "" {
		return market.Quote{}, &provider.Error{Kind: f.down, Msg: "stand-in: source down"}
	}
	if k, ok := f.failing[in]; ok {
		return market.Quote{}, &provider.Error{Kind: k, Msg: "stand-in: told to fail"}
	}
	q, ok := f.quotes[in]
	if !ok {
		return market.Quote{}, &provider.Error{Kind: provider.NotFound, Status: 404, Msg: "stand-in: unknown instrument"}
	}
	return q, nil
}

// answer makes the source return q for in.
func (f *fakeSource) answer(in market.Instrument, q market.Quote) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.failing, in)
	f.quotes[in] = q
}

// setPrice changes only the last price the source returns for in.
func (f *fakeSource) setPrice(in market.Instrument, price float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := f.quotes[in]
	q.Price = price
	f.quotes[in] = q
}

// fail makes every request for in fail with kind.
func (f *fakeSource) fail(in market.Instrument, kind provider.Kind) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failing[in] = kind
}

// failAll makes every request fail with kind, whatever the instrument.
func (f *fakeSource) failAll(kind provider.Kind) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = kind
}

func (f *fakeSource) count(in market.Instrument) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[in]
}

func (f *fakeSource) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.counts {
		n += c
	}
	return n
}

// ---------------------------------------------------------------------------
// Instruments, sessions and quotes
// ---------------------------------------------------------------------------

var (
	petr4  = market.Instrument{Market: market.B3, Symbol: "PETR4"}
	vale3  = market.Instrument{Market: market.B3, Symbol: "VALE3"}
	itub4  = market.Instrument{Market: market.B3, Symbol: "ITUB4"}
	aapl   = market.Instrument{Market: market.US, Symbol: "AAPL"}
	msft   = market.Instrument{Market: market.US, Symbol: "MSFT"}
	ibov   = market.Instrument{Market: market.IDX, Symbol: "IBOV"}
	sp500  = market.Instrument{Market: market.IDX, Symbol: "SP500"}
	usdbrl = market.Instrument{Market: market.FX, Symbol: "USDBRL"}

	// Monday 23 November 2026, 00:00 UTC.
	monday = time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)

	// 15:00 UTC: inside the B3 session and inside the US session of the day.
	bothOpen = monday.Add(15 * time.Hour)
	// 13:30 UTC: inside the B3 session, before the US session of the day.
	// The source still reports Friday's US session, closed for days.
	onlyB3Open = monday.Add(13*time.Hour + 30*time.Minute)

	b3Session       = market.Session{Start: monday.Add(13 * time.Hour), End: monday.Add(20 * time.Hour), TZ: "America/Sao_Paulo"}
	usSession       = market.Session{Start: monday.Add(14*time.Hour + 30*time.Minute), End: monday.Add(21 * time.Hour), TZ: "America/New_York"}
	usFridaySession = market.Session{Start: usSession.Start.Add(-72 * time.Hour), End: usSession.End.Add(-72 * time.Hour), TZ: "America/New_York"}
	fxSession       = market.Session{Start: monday.Add(9 * time.Hour), End: monday.Add(22 * time.Hour), TZ: "America/Sao_Paulo"}
)

func quote(name, currency string, price, prevClose float64, s market.Session, at time.Time) market.Quote {
	low, high := price, prevClose
	if low > high {
		low, high = high, low
	}
	low, high = low*0.99, high*1.01
	return market.Quote{
		Name: name, Currency: currency,
		Price: price, PrevClose: prevClose, Low: low, High: high,
		Time: at,
		Series: []market.Point{
			{T: s.Start, V: prevClose},
			{T: s.Start.Add(10 * time.Minute), V: (price + prevClose) / 2},
			{T: s.Start.Add(20 * time.Minute), V: price},
		},
		Session: s,
	}
}

// loadQuotes tells the source the quotes of every instrument the tests use.
// B3 and USD/BRL are open at `now`. The US market is open when usOpen is
// true; otherwise the source reports Friday's session and Friday's close.
func loadQuotes(src *fakeSource, now time.Time, usOpen bool) {
	b3At := now.Add(-15 * time.Minute)
	us, usAt := usSession, now.Add(-time.Second)
	if !usOpen {
		us, usAt = usFridaySession, usFridaySession.End
	}
	src.answer(petr4, quote("Petrobras PN", "BRL", 38.42, 38.11, b3Session, b3At))
	src.answer(vale3, quote("Vale ON", "BRL", 61.07, 61.07, b3Session, b3At))
	src.answer(itub4, quote("Itau Unibanco PN", "BRL", 34.15, 34.50, b3Session, b3At))
	src.answer(aapl, quote("Apple Inc.", "USD", 229.87, 230.99, us, usAt))
	src.answer(msft, quote("Microsoft Corp", "USD", 512.30, 510.25, us, usAt))
	src.answer(ibov, quote("Ibovespa", "", 158432.10, 157919.80, b3Session, b3At))
	src.answer(sp500, quote("S&P 500", "", 6812.40, 6819.90, us, usAt))
	src.answer(usdbrl, quote("USD/BRL", "BRL", 5.4321, 5.4278, fxSession, now.Add(-10*time.Second)))
}

// ---------------------------------------------------------------------------
// The world a test runs the tool in
// ---------------------------------------------------------------------------

// plainConfig has three rows outside any group and the default summary.
const plainConfig = `tickers = ["PETR4", "VALE3", "AAPL"]
`

var plainRows = []market.Instrument{petr4, vale3, aapl}

type world struct {
	t          *testing.T
	now        time.Time
	src        *fakeSource
	env        map[string]string
	configPath string // the default config path
	cacheDir   string // the folder spec 7.7 names for the cache
}

type result struct {
	code           int
	stdout, stderr string
}

// newWorld gives a home, a config folder and a cache folder of its own, a
// clock set to start, the config written at the default path, and a source
// that knows nothing yet.
func newWorld(t *testing.T, start time.Time, config string) *world {
	t.Helper()
	home, cfg, cache := t.TempDir(), t.TempDir(), t.TempDir()
	w := &world{
		t:   t,
		now: start,
		src: newFakeSource(),
		env: map[string]string{
			"HOME":            home,
			"XDG_CONFIG_HOME": cfg,
			"XDG_CACHE_HOME":  cache,
		},
		configPath: filepath.Join(cfg, "brl-stocks", "config.toml"),
		cacheDir:   filepath.Join(cache, "brl-stocks"),
	}
	if err := os.MkdirAll(filepath.Dir(w.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *world) advance(d time.Duration) { w.now = w.now.Add(d) }

// run calls the tool once, with the output piped (not a terminal).
func (w *world) run(args ...string) result {
	w.t.Helper()
	var out, errOut bytes.Buffer
	code := app.Run(context.Background(), args, deps.Deps{
		Provider: w.src,
		Now:      func() time.Time { return w.now },
		LookupEnv: func(k string) (string, bool) {
			v, ok := w.env[k]
			return v, ok
		},
		Stdout: &out,
		Stderr: &errOut,
		IsTTY:  false,
	})
	return result{code: code, stdout: out.String(), stderr: errOut.String()}
}

// runOK is run for a call whose command line and config are valid: spec 7.6
// gives it exit status 0 in every state.
func (w *world) runOK(args ...string) result {
	w.t.Helper()
	r := w.run(args...)
	if r.code != 0 {
		w.t.Fatalf("brl-stocks %s: exit status %d, want 0\nstderr: %s", strings.Join(args, " "), r.code, r.stderr)
	}
	return r
}

// prime establishes "a cache with quotes fetched now": a first `print` with
// an empty cache, which must fetch every row (spec 6.5, "no quote in the
// cache").
func (w *world) prime(rows ...market.Instrument) result {
	w.t.Helper()
	r := w.runOK("print")
	for _, in := range rows {
		if w.src.count(in) == 0 {
			w.t.Fatalf("given not established: the first print, with no cache, did not request %s", in.Symbol)
		}
	}
	return r
}

// cacheFiles lists every regular file under the cache folder, whatever its
// name.
func (w *world) cacheFiles() []string {
	w.t.Helper()
	var files []string
	err := filepath.WalkDir(w.cacheDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil || len(files) == 0 {
		w.t.Fatalf("given not established: no cache file under %s after a print that fetched (err: %v)", w.cacheDir, err)
	}
	return files
}

// overwriteCache replaces the content of every file of the cache folder.
func (w *world) overwriteCache(content []byte) {
	w.t.Helper()
	for _, p := range w.cacheFiles() {
		if err := os.WriteFile(p, content, 0o644); err != nil {
			w.t.Fatal(err)
		}
	}
}

// lockCache makes the cache folder, and everything in it, impossible to
// write. It skips the test where permissions do not bind (running as root).
func (w *world) lockCache() {
	w.t.Helper()
	var dirs []string
	err := filepath.WalkDir(w.cacheDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, p)
			return nil
		}
		return os.Chmod(p, 0o400)
	})
	if err != nil {
		w.t.Fatalf("given not established: cannot walk the cache folder %s: %v", w.cacheDir, err)
	}
	for _, d := range dirs {
		if err := os.Chmod(d, 0o500); err != nil {
			w.t.Fatal(err)
		}
	}
	w.t.Cleanup(func() {
		for _, d := range dirs {
			_ = os.Chmod(d, 0o700)
		}
	})
	probe := filepath.Join(w.cacheDir, "probe")
	if f, err := os.Create(probe); err == nil {
		f.Close()
		os.Remove(probe)
		w.t.Skip("file permissions do not bind for this user (root?): the cache folder cannot be made unwritable")
	}
}

// ---------------------------------------------------------------------------
// Reading the output
// ---------------------------------------------------------------------------

func lines(out string) []string {
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

// rowIndex returns the index of the one line of the table whose first word is
// the ticker.
func rowIndex(t *testing.T, out, ticker string) int {
	t.Helper()
	found := -1
	for i, l := range lines(out) {
		f := strings.Fields(l)
		if len(f) > 0 && f[0] == ticker {
			if found >= 0 {
				t.Fatalf("more than one line for row %s in:\n%s", ticker, out)
			}
			found = i
		}
	}
	if found < 0 {
		t.Fatalf("no line for row %s in:\n%s", ticker, out)
	}
	return found
}

func rowLine(t *testing.T, out, ticker string) string {
	t.Helper()
	return lines(out)[rowIndex(t, out, ticker)]
}

// lineIndex returns the index of the one line that holds exactly text.
func lineIndex(t *testing.T, out, text string) int {
	t.Helper()
	found := -1
	for i, l := range lines(out) {
		if strings.TrimSpace(l) == text {
			if found >= 0 {
				t.Fatalf("more than one line reading %q in:\n%s", text, out)
			}
			found = i
		}
	}
	if found < 0 {
		t.Fatalf("no line reading %q in:\n%s", text, out)
	}
	return found
}

// span returns the columns (counted in characters) where text starts and
// ends in line.
func span(t *testing.T, line, text string) (start, end int) {
	t.Helper()
	i := strings.Index(line, text)
	if i < 0 {
		t.Fatalf("%q not found in line %q", text, line)
	}
	start = utf8.RuneCountInString(line[:i])
	return start, start + utf8.RuneCountInString(text)
}

func decodeDocument(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("standard output is not a valid JSON document: %v\n%s", err, stdout)
	}
	return doc
}

// jsonRow finds the object of a row in the document of spec 7.5.
func jsonRow(t *testing.T, doc map[string]any, ticker string) map[string]any {
	t.Helper()
	groups, _ := doc["groups"].([]any)
	for _, g := range groups {
		group, _ := g.(map[string]any)
		rows, _ := group["rows"].([]any)
		for _, r := range rows {
			row, _ := r.(map[string]any)
			if row["ticker"] == ticker {
				return row
			}
		}
	}
	t.Fatalf("no row with ticker %q in the document: %v", ticker, doc)
	return nil
}

// ---------------------------------------------------------------------------
// S13 - One-shot table
// ---------------------------------------------------------------------------

// S13.1 Given a valid config, when `brl-stocks print` is run, then it prints
// an aligned table with one line per row to standard output and exits.
// (D6, D18, D41)
func TestS13_1_PrintsAlignedTableWithOneLinePerRow(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then: one line per row
	petr, vale, appl := rowLine(t, r.stdout, "PETR4"), rowLine(t, r.stdout, "VALE3"), rowLine(t, r.stdout, "AAPL")

	// Then: aligned. A column is aligned when its cells start at the same
	// place in every row, or end at the same place in every row.
	aligned := func(column string, cells ...[2]int) {
		t.Helper()
		sameStart, sameEnd := true, true
		for _, c := range cells[1:] {
			sameStart = sameStart && c[0] == cells[0][0]
			sameEnd = sameEnd && c[1] == cells[0][1]
		}
		if !sameStart && !sameEnd {
			t.Errorf("column %s is not aligned (start,end per row: %v) in:\n%s", column, cells, r.stdout)
		}
	}
	cell := func(line, text string) [2]int {
		t.Helper()
		s, e := span(t, line, text)
		return [2]int{s, e}
	}
	aligned("NAME", cell(petr, "Petrobras PN"), cell(vale, "Vale ON"), cell(appl, "Apple Inc."))
	aligned("LAST", cell(petr, "R$ 38,42"), cell(vale, "R$ 61,07"), cell(appl, "US$ 229.87"))
}

// S13.2 Given an open market and a cache whose quotes were fetched at most 60
// seconds ago, when `brl-stocks print` is run with the default interval, then
// no request is sent to the source. (D23)
func TestS13_2_FreshCacheSendsNoRequest(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(60 * time.Second)
	before := w.src.total()

	// When
	w.runOK("print")

	// Then
	if got := w.src.total() - before; got != 0 {
		t.Errorf("requests sent with a cache fetched 60 seconds ago: %d, want 0", got)
	}
}

// S13.3 Given an open market and a cache whose quotes were fetched 5 minutes
// ago, when `brl-stocks print` is run, then the quotes are requested from the
// source before printing. (D23)
func TestS13_3_OldCacheIsRefetchedBeforePrinting(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(5 * time.Minute)
	before := map[market.Instrument]int{}
	for _, in := range plainRows {
		before[in] = w.src.count(in)
	}
	w.src.setPrice(petr4, 39.10)

	// When
	r := w.runOK("print")

	// Then: requested
	for _, in := range plainRows {
		if w.src.count(in) == before[in] {
			t.Errorf("%s was not requested, with a cache fetched 5 minutes ago", in.Symbol)
		}
	}
	// Then: before printing, so the table carries the new quote
	if line := rowLine(t, r.stdout, "PETR4"); !strings.Contains(line, "R$ 39,10") {
		t.Errorf("row PETR4 does not show the quote just fetched (R$ 39,10): %q", line)
	}
}

// S13.4 Given an open market and a cache fetched 20 seconds ago, when
// `brl-stocks print --refresh` is run, then the quotes are requested from the
// source. (D23, D41)
func TestS13_4_RefreshFlagForcesAFetch(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(20 * time.Second)
	before := map[market.Instrument]int{}
	for _, in := range plainRows {
		before[in] = w.src.count(in)
	}

	// When
	w.runOK("print", "--refresh")

	// Then
	for _, in := range plainRows {
		if w.src.count(in) == before[in] {
			t.Errorf("%s was not requested by print --refresh", in.Symbol)
		}
	}
}

// S13.5 Given a market closed for more than 30 minutes, with cached quotes,
// when `brl-stocks print` is run, then none of that market's rows is
// requested. (D15, D50)
func TestS13_5_ClosedMarketRowsAreNotRequested(t *testing.T) {
	// Given: the US market closed since Friday, its quotes in the cache
	w := newWorld(t, onlyB3Open, `tickers = ["PETR4", "AAPL", "MSFT"]`+"\n")
	loadQuotes(w.src, w.now, false)
	w.prime(petr4, aapl, msft)
	w.advance(5 * time.Minute)
	beforeAAPL, beforeMSFT := w.src.count(aapl), w.src.count(msft)

	// When
	w.runOK("print")

	// Then
	if got := w.src.count(aapl) - beforeAAPL; got != 0 {
		t.Errorf("AAPL requested %d times while the US market is closed, want 0", got)
	}
	if got := w.src.count(msft) - beforeMSFT; got != 0 {
		t.Errorf("MSFT requested %d times while the US market is closed, want 0", got)
	}
}

// S13.6 Given cached quotes, an open market and a source that cannot be
// reached, when `brl-stocks print` is run, then the cached quotes are
// printed, marked stale with their age. (D19)
func TestS13_6_UnreachableSourcePrintsCacheMarkedStale(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(12 * time.Minute)
	w.src.failAll(provider.Unavailable)

	// When
	r := w.runOK("print")

	// Then
	line := rowLine(t, r.stdout, "PETR4")
	if !strings.Contains(line, "R$ 38,42") {
		t.Errorf("row PETR4 does not show the cached quote R$ 38,42: %q", line)
	}
	if !strings.Contains(line, "!12m") {
		t.Errorf("row PETR4 is not marked stale with its age (!12m): %q", line)
	}
}

// S13.7 Given the output is piped to another program, when `brl-stocks print`
// is run, then the output contains no colour codes. (D25)
func TestS13_7_PipedOutputHasNoColourCodes(t *testing.T) {
	// Given: rows that went up and down, and an output that is not a terminal
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then
	rowLine(t, r.stdout, "PETR4") // the table was printed
	if strings.Contains(r.stdout, "\x1b") {
		t.Errorf("piped output contains escape codes:\n%q", r.stdout)
	}
}

// S13.8 Given a config with groups, when `brl-stocks print` is run, then each
// group's name is on a line of its own above its rows. (D16, D50)
func TestS13_8_GroupNamesOnTheirOwnLines(t *testing.T) {
	// Given: a config with two groups, at a path given with --config
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	path := filepath.Join(t.TempDir(), "watchlist.toml")
	config := `tickers = ["PETR4"]

[[group]]
name = "Brasil"
tickers = ["VALE3", "ITUB4"]

[[group]]
name = "Tech"
tickers = ["AAPL", "MSFT"]
`
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	// When
	r := w.runOK("--config", path, "print")

	// Then
	brasil, tech := lineIndex(t, r.stdout, "Brasil"), lineIndex(t, r.stdout, "Tech")
	petr := rowIndex(t, r.stdout, "PETR4")
	vale, itub := rowIndex(t, r.stdout, "VALE3"), rowIndex(t, r.stdout, "ITUB4")
	appl, micr := rowIndex(t, r.stdout, "AAPL"), rowIndex(t, r.stdout, "MSFT")
	if !(petr < brasil && brasil < vale && brasil < itub) {
		t.Errorf("group name Brasil is not above its rows and below the rows before it:\n%s", r.stdout)
	}
	if !(vale < tech && itub < tech && tech < appl && tech < micr) {
		t.Errorf("group name Tech is not above its rows and below the rows of Brasil:\n%s", r.stdout)
	}
}

// S13.9 Given a source that does not answer, when `brl-stocks print` is run,
// then it prints and exits within 11 seconds. (D50)
func TestS13_9_SourceThatDoesNotAnswerExitsWithin11Seconds(t *testing.T) {
	t.Skip("cannot be tested through this boundary without real time: Deps has a clock (Now) but no timer, " +
		"so the 10-second limit of spec 7.6 can only be observed by letting a stand-in hang for 10 real seconds")
}

// S13.10 Given the source is rate-limiting or refusing the tool, when
// `brl-stocks print --refresh` is run, then no request is sent. (D50)
func TestS13_10_BlockedSourceGetsNoRequestEvenWithRefresh(t *testing.T) {
	// Given: a call a minute ago was refused, which started a block (6.3)
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.src.failAll(provider.RateLimited)
	w.runOK("print")
	if w.src.total() == 0 {
		t.Fatal("given not established: the first print, with no cache, sent no request")
	}
	w.advance(time.Minute)
	before := w.src.total()

	// When
	w.runOK("print", "--refresh")

	// Then
	if got := w.src.total() - before; got != 0 {
		t.Errorf("requests sent during a block: %d, want 0", got)
	}
}

// S13.11 Given an open market and a cache whose quotes were fetched at most
// 60 seconds ago, when `brl-stocks print` is run, then the table shows the
// cached quotes. (D23)
func TestS13_11_FreshCacheIsWhatTheTableShows(t *testing.T) {
	// Given: a cache 30 seconds old, and a source that has moved on since
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(30 * time.Second)
	w.src.setPrice(petr4, 39.99)
	w.src.setPrice(aapl, 231.11)

	// When
	r := w.runOK("print")

	// Then
	if line := rowLine(t, r.stdout, "PETR4"); !strings.Contains(line, "R$ 38,42") {
		t.Errorf("row PETR4 does not show the cached quote R$ 38,42: %q", line)
	}
	if line := rowLine(t, r.stdout, "AAPL"); !strings.Contains(line, "US$ 229.87") {
		t.Errorf("row AAPL does not show the cached quote US$ 229.87: %q", line)
	}
}

// S13.12 Given a closed market with cached quotes, when `brl-stocks print` is
// run, then that market's rows show the last close. (D15)
func TestS13_12_ClosedMarketRowsShowTheLastClose(t *testing.T) {
	// Given: the US market closed since Friday, Friday's close in the cache
	w := newWorld(t, onlyB3Open, `tickers = ["PETR4", "AAPL", "MSFT"]`+"\n")
	loadQuotes(w.src, w.now, false)
	w.prime(petr4, aapl, msft)
	w.advance(5 * time.Minute)

	// When
	r := w.runOK("print")

	// Then
	if line := rowLine(t, r.stdout, "AAPL"); !strings.Contains(line, "US$ 229.87") {
		t.Errorf("row AAPL does not show the last close US$ 229.87: %q", line)
	}
	if line := rowLine(t, r.stdout, "MSFT"); !strings.Contains(line, "US$ 512.30") {
		t.Errorf("row MSFT does not show the last close US$ 512.30: %q", line)
	}
}

// S13.13 Given a default summary, when `brl-stocks print` is run, then the
// first line of the output is the summary line. (D11, D50)
func TestS13_13_FirstLineIsTheSummaryLine(t *testing.T) {
	// Given: a config that does not set `summary`
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then: the entries of the default summary, in order, as 7.4 labels them
	first := lines(r.stdout)[0]
	at := 0
	for _, label := range []string{"IBOV", "S&P 500", "USD/BRL"} {
		i := strings.Index(first[at:], label)
		if i < 0 {
			t.Fatalf("first line is not the summary line (%q missing, or out of order): %q", label, first)
		}
		at += i + len(label)
	}
}

// S13.14 Given the US market is closed and B3 is open, when `brl-stocks
// print` is run, then the last line of the output reads
// `B3 open · US closed`. (D15, D50)
func TestS13_14_LastLineGivesTheStateOfEachMarket(t *testing.T) {
	// Given
	w := newWorld(t, onlyB3Open, plainConfig)
	loadQuotes(w.src, w.now, false)

	// When
	r := w.runOK("print")

	// Then
	ls := lines(r.stdout)
	if last := ls[len(ls)-1]; last != "B3 open · US closed" {
		t.Errorf("last line reads %q, want %q", last, "B3 open · US closed")
	}
}

// S13.15 Given cached quotes, an open market and a source that cannot be
// reached, when `brl-stocks print` is run, then one line on the error stream
// names the cause. (D19, D50)
func TestS13_15_UnreachableSourceIsNamedOnOneLineOfTheErrorStream(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	primed := w.prime(plainRows...)
	if strings.TrimSpace(primed.stderr) != "" {
		t.Fatalf("given not established: a print with nothing wrong wrote to the error stream: %q", primed.stderr)
	}
	w.advance(5 * time.Minute)
	w.src.failAll(provider.Unavailable)

	// When
	r := w.runOK("print")

	// Then: exactly one line. The spec does not fix its wording, so the
	// text of the cause is not asserted.
	var got []string
	for _, l := range lines(r.stderr) {
		if strings.TrimSpace(l) != "" {
			got = append(got, l)
		}
	}
	if len(got) != 1 {
		t.Errorf("error stream has %d lines, want 1 naming the cause:\n%s", len(got), r.stderr)
	}
}

// S13.16 Given a row whose fetch failed 30 seconds ago, when `brl-stocks
// print` is run without `--refresh`, then that row is not requested.
// (D19, D50)
func TestS13_16_RowThatJustFailedIsNotRequested(t *testing.T) {
	// Given: PETR4 in the cache, then a fetch of PETR4 that failed
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(5 * time.Minute)
	w.src.fail(petr4, provider.Unavailable)
	attempts := w.src.count(petr4)
	w.runOK("print")
	if w.src.count(petr4) == attempts {
		t.Fatal("given not established: PETR4 was not requested with a cache 5 minutes old, so no fetch failed")
	}
	w.advance(30 * time.Second)
	before := w.src.count(petr4)

	// When
	w.runOK("print")

	// Then
	if got := w.src.count(petr4) - before; got != 0 {
		t.Errorf("PETR4 requested %d times, 30 seconds after its fetch failed, want 0", got)
	}
}

// S13.17 Given a B3 row whose last price is 38.42, when `brl-stocks print` is
// run, then the table shows `R$ 38,42` in that row. (D46)
func TestS13_17_B3PriceInReais(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.src.setPrice(petr4, 38.42)

	// When
	r := w.runOK("print")

	// Then
	if line := rowLine(t, r.stdout, "PETR4"); !strings.Contains(line, "R$ 38,42") {
		t.Errorf("row PETR4 does not show R$ 38,42: %q", line)
	}
}

// S13.18 Given a US row whose last price is 229.87, when `brl-stocks print`
// is run, then the table shows `US$ 229.87` in that row. (D46)
func TestS13_18_USPriceInDollars(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.src.setPrice(aapl, 229.87)

	// When
	r := w.runOK("print")

	// Then
	if line := rowLine(t, r.stdout, "AAPL"); !strings.Contains(line, "US$ 229.87") {
		t.Errorf("row AAPL does not show US$ 229.87: %q", line)
	}
}

// S13.19 Given a default summary and an Ibovespa value of 158432.10, when
// `brl-stocks print` is run, then the first line shows `158.432`. (D51, D65)
func TestS13_19_IbovespaInWholePoints(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.src.setPrice(ibov, 158432.10)

	// When
	r := w.runOK("print")

	// Then
	if first := lines(r.stdout)[0]; !strings.Contains(first, "158.432") {
		t.Errorf("first line does not show 158.432: %q", first)
	}
}

// ---------------------------------------------------------------------------
// S15 - The last quotes are kept between runs (as seen through print)
// ---------------------------------------------------------------------------

// S15.1 Given a run that fetched quotes and then ended, when the tool is
// started again with no network, then the rows show those quotes. (D20)
func TestS15_1_QuotesOfAnEarlierRunAreShownWithNoNetwork(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(5 * time.Minute)
	w.src.failAll(provider.Unavailable)

	// When
	r := w.runOK("print")

	// Then
	for ticker, price := range map[string]string{"PETR4": "R$ 38,42", "VALE3": "R$ 61,07", "AAPL": "US$ 229.87"} {
		if line := rowLine(t, r.stdout, ticker); !strings.Contains(line, price) {
			t.Errorf("row %s does not show the quote of the earlier run (%s): %q", ticker, price, line)
		}
	}
}

// spoiledCacheRun is the scenario of S15.4 and S15.7. One world fetches
// quotes, has every file of its cache folder overwritten with content, and
// runs print again at once. A second world, which never had a cache, runs
// print at the same moment against the same source. Both results are
// returned: spoiled first, then the one with no cache.
func spoiledCacheRun(t *testing.T, content []byte) (spoiled, noCache result) {
	t.Helper()
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.overwriteCache(content)
	w.src.setPrice(petr4, 39.10)

	control := newWorld(t, bothOpen, plainConfig)
	loadQuotes(control.src, control.now, true)
	control.src.setPrice(petr4, 39.10)

	return w.runOK("print"), control.runOK("print")
}

var spoiledCaches = map[string][]byte{
	"emptied": {},
	"garbage": []byte("\x00\xff\xfe not a cache {{{ [[[ \x01\n"),
}

// S15.4 Given a cache file emptied or filled with garbage, when the tool
// starts, then it shows the watchlist as it does with no cache. (D48)
func TestS15_4_UnreadableCacheIsTreatedAsNoCache(t *testing.T) {
	for name, content := range spoiledCaches {
		t.Run(name, func(t *testing.T) {
			// Given, When
			spoiled, noCache := spoiledCacheRun(t, content)

			// Then
			rowLine(t, noCache.stdout, "PETR4") // with no cache, the watchlist is shown
			if spoiled.stdout != noCache.stdout {
				t.Errorf("output with a spoiled cache differs from the output with no cache.\nspoiled:\n%s\nno cache:\n%s",
					spoiled.stdout, noCache.stdout)
			}
		})
	}
}

// S15.7 Given a cache file emptied or filled with garbage, when the tool
// starts, then no message about the cache is shown. (D48)
func TestS15_7_UnreadableCacheRaisesNoMessage(t *testing.T) {
	for name, content := range spoiledCaches {
		t.Run(name, func(t *testing.T) {
			// Given, When
			spoiled, noCache := spoiledCacheRun(t, content)

			// Then: the error stream says no more than it does with no cache
			if spoiled.stderr != noCache.stderr {
				t.Errorf("error stream with a spoiled cache: %q, with no cache: %q", spoiled.stderr, noCache.stderr)
			}
			if strings.Contains(strings.ToLower(spoiled.stdout+spoiled.stderr), "cache") {
				t.Errorf("a message about the cache is shown.\nstdout:\n%s\nstderr:\n%s", spoiled.stdout, spoiled.stderr)
			}
		})
	}
}

// S15.9 Given a cache folder that cannot be written, when `brl-stocks print`
// is run, then no request is sent to the source. (D60)
func TestS15_9_UnwritableCacheFolderSendsNoRequest(t *testing.T) {
	// Given: a cache old enough that print would fetch, in a folder that
	// can no longer be written
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(5 * time.Minute)
	w.lockCache()
	before := w.src.total()

	// When
	w.runOK("print")

	// Then
	if got := w.src.total() - before; got != 0 {
		t.Errorf("requests sent with a cache folder that cannot be written: %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// S14 - One-shot JSON
// ---------------------------------------------------------------------------

// S14.1 Given a valid config, when `brl-stocks print --json` is run, then
// standard output is exactly one valid JSON document. (D18)
func TestS14_1_StandardOutputIsExactlyOneJSONDocument(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print", "--json")

	// Then
	dec := json.NewDecoder(strings.NewReader(r.stdout))
	var doc any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("standard output does not start with a valid JSON document: %v\n%s", err, r.stdout)
	}
	var more any
	if err := dec.Decode(&more); err != io.EOF {
		t.Errorf("standard output holds more than one JSON document (second decode: %v):\n%s", err, r.stdout)
	}
}

// S14.4 Given every row is fresh, when `brl-stocks print --json` is run, then
// `state` is `ok`. (D44)
func TestS14_4_StateIsOkWhenEveryRowIsFresh(t *testing.T) {
	// Given: open markets and a source that answers every row
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print", "--json")

	// Then
	if state := decodeDocument(t, r.stdout)["state"]; state != "ok" {
		t.Errorf("state is %v, want \"ok\"", state)
	}
}

// S14.5 Given one stale row and the others fresh, when `brl-stocks print
// --json` is run, then `state` is `partial`. (D44)
func TestS14_5_StateIsPartialWithOneStaleRow(t *testing.T) {
	// Given: every row in the cache; five minutes on, PETR4 alone fails
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(5 * time.Minute)
	w.src.fail(petr4, provider.Unavailable)

	// When
	r := w.runOK("print", "--json")

	// Then
	if state := decodeDocument(t, r.stdout)["state"]; state != "partial" {
		t.Errorf("state is %v, want \"partial\"", state)
	}
}

// S14.6 Given no cache and a source that cannot be reached, when `brl-stocks
// print --json` is run, then standard output is a valid document with `state`
// `error`. (D44)
func TestS14_6_StateIsErrorWithNoCacheAndNoSource(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	w.src.failAll(provider.Unavailable)

	// When
	r := w.run("print", "--json")

	// Then
	if state := decodeDocument(t, r.stdout)["state"]; state != "error" {
		t.Errorf("state is %v, want \"error\"", state)
	}
}

// S14.7 Given no cache and a source that cannot be reached, when `brl-stocks
// print --json` is run, then the exit status is 0. (D44)
func TestS14_7_ExitStatusIsZeroWithNoCacheAndNoSource(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	w.src.failAll(provider.Unavailable)

	// When
	r := w.run("print", "--json")

	// Then
	if r.code != 0 {
		t.Errorf("exit status is %d, want 0", r.code)
	}
}

// S14.12 Given a B3 row whose last price is 38.425, when `brl-stocks print
// --json` is run, then its `price` is the number 38.425. (D43, D66)
func TestS14_12_PriceIsNotRounded(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.src.setPrice(petr4, 38.425)

	// When
	r := w.runOK("print", "--json")

	// Then
	row := jsonRow(t, decodeDocument(t, r.stdout), "PETR4")
	price, isNumber := row["price"].(float64)
	if !isNumber {
		t.Fatalf("price of PETR4 is %#v, want a JSON number", row["price"])
	}
	if price != 38.425 {
		t.Errorf("price of PETR4 is %v, want 38.425", price)
	}
}
