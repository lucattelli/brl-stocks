//go:build verification

// Package verification checks the one-shot mode of brl-stocks from outside:
// it runs the real binary as a child process against a local stand-in source
// and asserts on what a user or a script would observe. See README.md.
package verification

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Set-up: both binaries are built once.
// ---------------------------------------------------------------------------

var (
	binPath     string // the real brl-stocks binary
	standinPath string // the stand-in source
	evidenceDir string // verification/evidence/<UTC timestamp>
)

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "brl-verify-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binPath = filepath.Join(tmp, "brl-stocks")
	standinPath = filepath.Join(tmp, "standin")
	for _, b := range [][2]string{{binPath, "./cmd/brl-stocks"}, {standinPath, "./verification/standin"}} {
		cmd := exec.Command("go", "build", "-o", b[0], b[1])
		cmd.Dir = ".."
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "build %s: %v\n%s", b[1], err, out)
			os.Exit(1)
		}
	}
	evidenceDir = filepath.Join("evidence", time.Now().UTC().Format("20060102T150405Z"))
	if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

// ---------------------------------------------------------------------------
// The stand-in, driven over HTTP.
// ---------------------------------------------------------------------------

type override struct {
	Name      *string  `json:"name,omitempty"`
	Price     *float64 `json:"price,omitempty"`
	PrevClose *float64 `json:"prev_close,omitempty"`
	Low       *float64 `json:"low,omitempty"`
	High      *float64 `json:"high,omitempty"`
}

type state struct {
	Status           int                 `json:"status"`
	SymbolStatus     map[string]int      `json:"symbol_status"`
	DelayMS          int                 `json:"delay_ms"`
	ClosedMarkets    []string            `json:"closed_markets"`
	ClosedAgoSeconds int64               `json:"closed_ago_seconds"`
	Quotes           map[string]override `json:"quotes"`
}

type record struct {
	Seq        int    `json:"seq"`
	Time       string `json:"time"`
	UnixMS     int64  `json:"unix_ms"`
	AnsweredMS int64  `json:"answered_ms"`
	Symbol     string `json:"symbol"`
	Query      string `json:"query"`
	Status     int    `json:"status"`
}

type standin struct {
	url   string
	cmd   *exec.Cmd
	dead  bool
	final []record // the record as it was when the stand-in was stopped
}

func startStandin(t *testing.T) *standin {
	t.Helper()
	cmd := exec.Command(standinPath, "-addr", "127.0.0.1:0")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "LISTEN http://127.0.0.1:") {
		cmd.Process.Kill()
		t.Fatalf("stand-in did not start: %q %v", line, err)
	}
	s := &standin{url: strings.TrimSpace(strings.TrimPrefix(line, "LISTEN ")), cmd: cmd}
	t.Cleanup(s.stop)
	return s
}

// stop kills the stand-in: from then on its address refuses connections.
func (s *standin) stop() {
	if s.dead {
		return
	}
	s.final = s.requestsLive()
	s.dead = true
	s.cmd.Process.Kill()
	s.cmd.Wait()
}

func (s *standin) set(t *testing.T, st state) {
	t.Helper()
	b, _ := json.Marshal(st)
	resp, err := http.Post(s.url+"/_control", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("stand-in control: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("stand-in control: status %d", resp.StatusCode)
	}
}

func (s *standin) requestsLive() []record {
	resp, err := http.Get(s.url + "/_requests")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var recs []record
	json.NewDecoder(resp.Body).Decode(&recs)
	return recs
}

// requests returns everything the stand-in has received so far.
func (s *standin) requests() []record {
	if s.dead {
		return s.final
	}
	return s.requestsLive()
}

func bySymbol(recs []record) map[string]int {
	m := map[string]int{}
	for _, r := range recs {
		m[r.Symbol]++
	}
	return m
}

func fmtCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, " ")
}

func fmtRecords(recs []record) string {
	if len(recs) == 0 {
		return "  (no requests)\n"
	}
	var b strings.Builder
	for _, r := range recs {
		fmt.Fprintf(&b, "  #%d %s symbol=%s ?%s -> %d\n", r.Seq, r.Time, r.Symbol, r.Query, r.Status)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// The environment of one check, and the evidence it writes.
// ---------------------------------------------------------------------------

const baseConfig = `refresh_seconds = 10
tickers = ["PETR4", "AAPL"]

[[group]]
name = "Brasil"
tickers = ["VALE3", "ITUB4"]

[[group]]
name = "US"
tickers = ["MSFT"]
`

// The same watchlist with the default interval (60 seconds).
var defaultIntervalConfig = strings.TrimPrefix(baseConfig, "refresh_seconds = 10\n")

var (
	b3Syms  = []string{"PETR4.SA", "VALE3.SA", "ITUB4.SA", "^BVSP"}
	usSyms  = []string{"AAPL", "MSFT", "^GSPC"}
	allSyms = []string{"PETR4.SA", "AAPL", "VALE3.SA", "ITUB4.SA", "MSFT", "^BVSP", "^GSPC", "BRL=X"}
	tickers = []string{"PETR4", "AAPL", "VALE3", "ITUB4", "MSFT"}
)

type env struct {
	t           *testing.T
	root        string
	home        string
	cfgHome     string
	cacheHome   string
	tmpDir      string
	cfgPath     string
	src         *standin
	sourceURL   string
	noCacheHome bool // leave XDG_CACHE_HOME unset
	noCfgHome   bool // leave XDG_CONFIG_HOME unset
	mu          sync.Mutex
	ev          strings.Builder
}

// newEnv makes temporary folders for HOME, config and cache, writes the config
// (unless it is empty) and starts a stand-in of its own.
func newEnv(t *testing.T, criterion, config string) *env {
	t.Helper()
	root := t.TempDir()
	e := &env{
		t: t, root: root,
		home:      filepath.Join(root, "home"),
		cfgHome:   filepath.Join(root, "config"),
		cacheHome: filepath.Join(root, "cache"),
		tmpDir:    filepath.Join(root, "tmp"),
	}
	for _, d := range []string{e.home, e.cfgHome, e.cacheHome, e.tmpDir, filepath.Join(e.cfgHome, "brl-stocks")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e.cfgPath = filepath.Join(e.cfgHome, "brl-stocks", "config.toml")
	if config != "" {
		if err := os.WriteFile(e.cfgPath, []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e.src = startStandin(t)
	e.sourceURL = e.src.url
	e.logf("CHECK      %s\nCRITERION  %s\nSTARTED    %s\nSTAND-IN   %s\nCONFIG     %s\n%s\n",
		t.Name(), criterion, time.Now().UTC().Format(time.RFC3339), e.src.url, e.cfgPath, indent(config))
	t.Cleanup(func() {
		verdict := "PASS"
		if t.Failed() {
			verdict = "FAIL"
		} else if t.Skipped() {
			verdict = "SKIP"
		}
		e.logf("\nFULL REQUEST RECORD OF THE STAND-IN\n%s\nVERDICT    %s\n", fmtRecords(e.src.requests()), verdict)
		name := strings.NewReplacer("/", "__", " ", "_").Replace(t.Name()) + ".txt"
		e.mu.Lock()
		defer e.mu.Unlock()
		if err := os.WriteFile(filepath.Join(evidenceDir, name), []byte(e.ev.String()), 0o644); err != nil {
			t.Errorf("evidence: %v", err)
		}
	})
	return e
}

func indent(s string) string {
	if s == "" {
		return "    (none)"
	}
	return "    " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n    ")
}

func (e *env) logf(format string, a ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Fprintf(&e.ev, format, a...)
}

// failf records a failed assertion in the evidence and fails the check.
func (e *env) failf(format string, a ...any) {
	e.t.Helper()
	e.logf("ASSERTION FAILED: "+format+"\n", a...)
	e.t.Errorf(format, a...)
}

func (e *env) sleep(d time.Duration, why string) {
	e.logf("\nWAIT       %s (%s)\n", d, why)
	time.Sleep(d)
}

func (e *env) setSource(st state, what string) {
	e.t.Helper()
	b, _ := json.Marshal(st)
	e.logf("\nSTAND-IN   switched: %s  %s\n", what, b)
	e.src.set(e.t, st)
}

func (e *env) environ() []string {
	if !strings.HasPrefix(e.sourceURL, "http://127.0.0.1:") {
		e.t.Fatalf("refusing to run: source address %q is not a local stand-in", e.sourceURL)
	}
	v := []string{
		"PATH=/usr/bin:/bin",
		"TZ=UTC",
		"HOME=" + e.home,
		"TMPDIR=" + e.tmpDir,
		"BRL_STOCKS_SOURCE_URL=" + e.sourceURL,
	}
	if !e.noCfgHome {
		v = append(v, "XDG_CONFIG_HOME="+e.cfgHome)
	}
	if !e.noCacheHome {
		v = append(v, "XDG_CACHE_HOME="+e.cacheHome)
	}
	return v
}

type result struct {
	stdout, stderr string
	exit           int
	dur            time.Duration
	reqs           []record // what the stand-in received during this run
}

func fileSum(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "(unreadable: " + err.Error() + ")"
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// exec runs the real binary once, without touching the evidence.
func (e *env) exec(args ...string) result {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Env = e.environ()
	cmd.Dir = e.root
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se // pipes, not a terminal
	start := time.Now()
	err := cmd.Run()
	r := result{stdout: so.String(), stderr: se.String(), dur: time.Since(start)}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		r.exit = ee.ExitCode()
	default:
		r.exit = -999
		r.stderr += "\n(verifier: could not run: " + err.Error() + ")"
	}
	if ctx.Err() != nil {
		r.stderr += "\n(verifier: killed after 40 s)"
	}
	return r
}

// run runs the real binary, records the run in the evidence, and checks QR7
// (config byte-identical) around it.
func (e *env) run(args ...string) result {
	e.t.Helper()
	before := len(e.src.requests())
	sumBefore := fileSum(e.cfgPath)
	r := e.exec(args...)
	all := e.src.requests()
	if len(all) >= before {
		r.reqs = all[before:]
	}
	e.logf("\n$ brl-stocks %s\nENV        %s\nEXIT       %d   (wall time %s)\nSTDOUT\n%s\nSTDERR\n%s\nREQUESTS RECEIVED BY THE STAND-IN DURING THIS RUN: %d   per symbol: %s\n%s",
		strings.Join(args, " "), strings.Join(e.environ()[1:], " "), r.exit, r.dur.Round(time.Millisecond),
		indent(r.stdout), indent(r.stderr), len(r.reqs), fmtCounts(bySymbol(r.reqs)), fmtRecords(r.reqs))
	if sumAfter := fileSum(e.cfgPath); sumAfter != sumBefore {
		e.failf("QR7: the config file changed during the run: sha256 %s -> %s", sumBefore, sumAfter)
	}
	return r
}

// ---------------------------------------------------------------------------
// Reading the output as a user would.
// ---------------------------------------------------------------------------

func lines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// rowLines returns the lines of the table whose first word is the ticker.
func rowLines(out, ticker string) []string {
	var got []string
	for _, l := range lines(out) {
		if f := strings.Fields(l); len(f) > 0 && f[0] == ticker {
			got = append(got, l)
		}
	}
	return got
}

func row(e *env, out, ticker string) string {
	e.t.Helper()
	ls := rowLines(out, ticker)
	if len(ls) != 1 {
		e.failf("expected exactly one line for row %s, found %d", ticker, len(ls))
		if len(ls) == 0 {
			return ""
		}
	}
	return ls[0]
}

var (
	priceRE = regexp.MustCompile(`(?:US\$|R\$) -?[0-9][0-9.,]*`)
	staleRE = regexp.MustCompile(`!(\d+)([smhd])`)
)

func hasQuote(line string) bool { return priceRE.MatchString(line) }

func linesWithPrefix(s, prefix string) []string {
	var got []string
	for _, l := range lines(s) {
		if strings.HasPrefix(l, prefix) {
			got = append(got, l)
		}
	}
	return got
}

func (e *env) wantExit(r result, want int) {
	e.t.Helper()
	if r.exit != want {
		e.failf("exit status: want %d, got %d", want, r.exit)
	}
}

func (e *env) wantNoRequests(r result, what string) {
	e.t.Helper()
	if len(r.reqs) != 0 {
		e.failf("%s: want 0 requests at the source, the stand-in received %d (%s)", what, len(r.reqs), fmtCounts(bySymbol(r.reqs)))
	}
}

func (e *env) wantRequested(r result, syms []string, what string) {
	e.t.Helper()
	c := bySymbol(r.reqs)
	for _, s := range syms {
		if c[s] == 0 {
			e.failf("%s: want a request for %s, the stand-in received none (got: %s)", what, s, fmtCounts(c))
		} else if c[s] > 1 {
			e.failf("%s: want one request for %s, the stand-in received %d", what, s, c[s])
		}
	}
}

func (e *env) wantNotRequested(r result, syms []string, what string) {
	e.t.Helper()
	c := bySymbol(r.reqs)
	for _, s := range syms {
		if c[s] != 0 {
			e.failf("%s: want no request for %s, the stand-in received %d", what, s, c[s])
		}
	}
}

// parseDoc checks that standard output is exactly one JSON document.
func parseDoc(e *env, out string) map[string]any {
	e.t.Helper()
	dec := json.NewDecoder(strings.NewReader(out))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		e.failf("standard output is not a JSON document: %v", err)
		return nil
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		e.failf("standard output holds more than one JSON document (after the first: %v, %v)", extra, err)
	}
	return doc
}

// docRows returns the row objects of the document by ticker.
func docRows(doc map[string]any) map[string]map[string]any {
	rows := map[string]map[string]any{}
	groups, _ := doc["groups"].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		rs, _ := gm["rows"].([]any)
		for _, r := range rs {
			if rm, ok := r.(map[string]any); ok {
				if tk, ok := rm["ticker"].(string); ok {
					rows[tk] = rm
				}
			}
		}
	}
	return rows
}

func fp(v float64) *float64 { return &v }
func sp(v string) *string   { return &v }

const pastInterval = 12 * time.Second // a little longer than refresh_seconds = 10

// ---------------------------------------------------------------------------
// S13 - One-shot table
// ---------------------------------------------------------------------------

// S13.1 Given a valid config, when `brl-stocks print` is run, then it prints an
// aligned table with one line per row to standard output and exits.
func TestVerifyS13_01_TableOneLinePerRow(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S13.1", baseConfig)
	r := e.run("print")
	e.wantExit(r, 0)
	for _, tk := range tickers {
		if l := row(e, r.stdout, tk); l != "" && !hasQuote(l) {
			e.failf("row %s carries no price: %q", tk, l)
		}
	}
	if len(linesWithPrefix(r.stdout, "TICKER")) != 1 {
		e.failf("want exactly one header line starting with TICKER")
	}
	// Aligned: the name starts at the same column in every row.
	col := -1
	for tk, name := range map[string]string{"PETR4": "Petrobras PN", "AAPL": "Apple Inc.", "VALE3": "Vale ON", "MSFT": "Microsoft Corp"} {
		l := row(e, r.stdout, tk)
		i := strings.Index(l, name)
		if i < 0 {
			e.failf("row %s does not show the name %q: %q", tk, name, l)
			continue
		}
		c := utf8.RuneCountInString(l[:i])
		if col >= 0 && c != col {
			e.failf("names do not start at the same column (%d and %d)", col, c)
		}
		col = c
	}
}

// Section 7.4 / 7.3 (not a numbered criterion): the row and the summary line
// are formatted as the spec's example shows them.
func TestVerifySec7_4_RowAndSummaryFormat(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "7.3 Numbers / 7.4 example (format of a row and of the summary line)", baseConfig)
	r := e.run("print")
	e.wantExit(r, 0)
	got := strings.Fields(row(e, r.stdout, "PETR4"))
	want := []string{"PETR4", "Petrobras", "PN", "R$", "38,42", "+0,31", "+0,81%", "38,02", "38,60"}
	if len(got) < len(want)+1 || strings.Join(got[:len(want)], " ") != strings.Join(want, " ") {
		e.failf("PETR4 row: want fields %q then the quote time, got %q", want, got)
	} else if !regexp.MustCompile(`^\d\d[:/]\d\d$`).MatchString(got[len(want)]) {
		e.failf("PETR4 row: quote time want HH:MM or DD/MM, got %q", got[len(want)])
	}
	gotUS := strings.Fields(row(e, r.stdout, "AAPL"))
	wantUS := []string{"AAPL", "Apple", "Inc.", "US$", "229.87", "-1.12", "-0.48%", "228.90", "231.40"}
	if len(gotUS) < len(wantUS) || strings.Join(gotUS[:len(wantUS)], " ") != strings.Join(wantUS, " ") {
		e.failf("AAPL row: want fields %q, got %q", wantUS, gotUS)
	}
	gotZero := strings.Fields(row(e, r.stdout, "VALE3"))
	if strings.Join(gotZero, " ") == "" || !strings.Contains(strings.Join(gotZero, " "), "+0,00 +0,00%") {
		e.failf("VALE3 row (change of zero): want `+0,00 +0,00%%`, got %q", gotZero)
	}
	first := ""
	if ls := lines(r.stdout); len(ls) > 0 {
		first = ls[0]
	}
	for _, w := range []string{"IBOV 158.432 +0,32%", "S&P 500 6,812.40 -0.11%", "USD/BRL 5,4321 +0,08%"} {
		if !strings.Contains(first, w) {
			e.failf("summary line: want %q in the first line, got %q", w, first)
		}
	}
}

// S13.2 Given an open market and a cache whose quotes were fetched at most 60
// seconds ago, when `brl-stocks print` is run with the default interval, then
// no request is sent to the source.
// Tested literally: default interval, cache 12 seconds old (older than the
// minimum interval of 10, younger than the default of 60).
func TestVerifyS13_02_FreshCacheNoRequest(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S13.2", defaultIntervalConfig)
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.wantRequested(r1, allSyms, "first run on an empty cache")
	e.sleep(pastInterval, "cache becomes 12 s old; the default interval is 60 s")
	r2 := e.run("print")
	e.wantExit(r2, 0)
	e.wantNoRequests(r2, "cache 12 s old, default interval")
}

// S13.3 Given an open market and a cache whose quotes were fetched 5 minutes
// ago, when `brl-stocks print` is run, then the quotes are requested from the
// source before printing.
// Scaled down: refresh_seconds = 10 and a cache 12 seconds old ("longer than
// the interval") in place of 60 seconds and 5 minutes.
func TestVerifyS13_03_OldCacheIsRequested(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S13.3 (scaled: interval 10 s, cache 12 s old)", baseConfig)
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.setSource(state{Quotes: map[string]override{"PETR4.SA": {Price: fp(39.50)}}}, "PETR4 price changes to 39.50")
	e.sleep(pastInterval, "cache becomes older than the interval")
	r2 := e.run("print")
	e.wantExit(r2, 0)
	e.wantRequested(r2, allSyms, "cache older than the interval")
	if l := row(e, r2.stdout, "PETR4"); !strings.Contains(l, "R$ 39,50") {
		e.failf("requested BEFORE printing: want the new price `R$ 39,50` in the PETR4 row, got %q", l)
	}
}

// S13.4 Given an open market and a cache fetched 20 seconds ago, when
// `brl-stocks print --refresh` is run, then the quotes are requested from the
// source. Tested literally (default interval, 20 seconds).
func TestVerifyS13_04_RefreshForcesFetch(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S13.4", defaultIntervalConfig)
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.sleep(20*time.Second, "cache becomes 20 s old")
	r2 := e.run("print", "--refresh")
	e.wantExit(r2, 0)
	e.wantRequested(r2, allSyms, "print --refresh on a cache 20 s old")
}

// Workflow 6.5 (not a numbered criterion): "A forced fetch was asked for:
// fetches, unless the row was attempted less than 5 seconds ago".
func TestVerifyW6_5_RefreshWithinFiveSecondsDoesNotFetch(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "6.5 row 3: a forced fetch does not fetch a row attempted less than 5 seconds ago", baseConfig)
	r1 := e.run("print")
	e.wantExit(r1, 0)
	r2 := e.run("print", "--refresh")
	e.wantExit(r2, 0)
	e.wantNoRequests(r2, "print --refresh right after a fetch")
}

// S13.5 Given a market closed for more than 30 minutes, with cached quotes,
// when `brl-stocks print` is run, then none of that market's rows is requested.
// B3 closed 2 hours ago; the US market stays open as a control. Interval 10 s,
// second run 12 s later, so only the closed market explains a missing request.
func TestVerifyS13_05_ClosedMarketNotRequested(t *testing.T) {
	t.Parallel()
	// Reworded by D111: "ended more than its settle time ago (100 minutes for
	// B3, 5 for the others)"; before, more than 30 minutes, tested with 2 h.
	// Each variant's period ends that long before every answer; the other
	// market is open, as a control.
	for _, v := range []struct {
		name    string
		market  string
		ago     int64
		closed  []string
		control []string
	}{
		{"b3_ended_101min_ago", "b3", 6060, []string{"PETR4.SA", "VALE3.SA", "ITUB4.SA"}, []string{"AAPL", "MSFT"}},
		{"b3_ended_2h_ago", "b3", 7200, []string{"PETR4.SA", "VALE3.SA", "ITUB4.SA"}, []string{"AAPL", "MSFT"}},
		{"us_ended_6min_ago", "us", 360, []string{"AAPL", "MSFT"}, []string{"PETR4.SA", "VALE3.SA", "ITUB4.SA"}},
	} {
		v := v
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, "S13.5 "+v.name, baseConfig)
			e.setSource(state{ClosedMarkets: []string{v.market}, ClosedAgoSeconds: v.ago}, fmt.Sprintf("%s period ended %d s before each answer", v.market, v.ago))
			r1 := e.run("print")
			e.wantExit(r1, 0)
			e.sleep(pastInterval, "cache becomes older than the interval")
			r2 := e.run("print")
			e.wantExit(r2, 0)
			e.wantNotRequested(r2, v.closed, "rows of the market past its settle time")
			c := bySymbol(r2.reqs)
			for _, s := range v.control {
				if c[s] == 0 {
					e.failf("control: the open market's row %s was expected to be requested (got %s)", s, fmtCounts(c))
				}
			}
			e.logf("NOTE       requests in the second run: %s\n", fmtCounts(c))
		})
	}
}

// unreachable sets up: cached quotes, open market, then a source that cannot
// be reached (status 503, or no connection at all), and a cache older than the
// interval. It returns the run made in that situation.
func unreachable(t *testing.T, criterion, variant string, args ...string) (*env, result) {
	e := newEnv(t, criterion+" ["+variant+"]", baseConfig)
	r1 := e.run("print")
	e.wantExit(r1, 0)
	if variant == "status 503" {
		e.setSource(state{Status: 503}, "every request answered 503")
	} else {
		e.logf("\nSTAND-IN   stopped: its address now refuses connections\n")
		e.src.stop()
	}
	e.sleep(pastInterval, "cache becomes older than the interval")
	return e, e.run(args...)
}

var unreachableVariants = []string{"status 503", "no connection"}

// S13.6 Given cached quotes, an open market and a source that cannot be
// reached, when `brl-stocks print` is run, then the cached quotes are printed,
// marked stale with their age.
func TestVerifyS13_06_UnreachableShowsStaleWithAge(t *testing.T) {
	t.Parallel()
	for _, v := range unreachableVariants {
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			e, r := unreachable(t, "S13.6", v, "print")
			e.wantExit(r, 0)
			for tk, price := range map[string]string{"PETR4": "R$ 38,42", "AAPL": "US$ 229.87", "VALE3": "R$ 61,07", "ITUB4": "R$ 34,15", "MSFT": "US$ 512.30"} {
				l := row(e, r.stdout, tk)
				if !strings.Contains(l, price) {
					e.failf("row %s: want the cached price %q, got %q", tk, price, l)
				}
				m := staleRE.FindStringSubmatch(l)
				if m == nil {
					e.failf("row %s: want a stale mark with the age (like `!12s`), got %q", tk, l)
					continue
				}
				var age int
				fmt.Sscan(m[1], &age)
				if m[2] != "s" || age < 10 || age > 25 {
					e.failf("row %s: the last successful fetch was about 12 s ago, the mark says %q", tk, m[0])
				}
			}
		})
	}
}

// S13.15 Given cached quotes, an open market and a source that cannot be
// reached, when `brl-stocks print` is run, then exactly one line on the error
// stream starts with `source unreachable`.
func TestVerifyS13_15_UnreachableOneErrorLine(t *testing.T) {
	t.Parallel()
	for _, v := range unreachableVariants {
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			e, r := unreachable(t, "S13.15", v, "print")
			e.wantExit(r, 0)
			if n := len(linesWithPrefix(r.stderr, "source unreachable")); n != 1 {
				e.failf("want exactly 1 line on the error stream starting with `source unreachable`, got %d; error stream: %q", n, r.stderr)
			}
			if strings.Contains(r.stdout, "source unreachable") {
				e.failf("the cause of the failure is on standard output; it belongs on the error stream")
			}
		})
	}
}

// S13.7 Given the output is piped to another program, when `brl-stocks print`
// is run, then the output contains no colour codes.
// (Every run of this suite is piped. The rows hold gains and losses.)
func TestVerifyS13_07_PipedNoColour(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S13.7", baseConfig)
	r := e.run("print")
	e.wantExit(r, 0)
	if strings.Contains(r.stdout, "\x1b") {
		e.failf("standard output holds an escape character: %q", r.stdout)
	}
	if strings.Contains(r.stderr, "\x1b") {
		e.failf("the error stream holds an escape character: %q", r.stderr)
	}
	if !strings.Contains(r.stdout, "+0,81%") || !strings.Contains(r.stdout, "-0.48%") {
		e.failf("control: the output was expected to hold a gain and a loss")
	}
}

// S13.8 Given a config with groups, when `brl-stocks print` is run, then each
// group's name is on a line of its own above its rows.
func TestVerifyS13_08_GroupNamesOnOwnLines(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S13.8", baseConfig)
	r := e.run("print")
	e.wantExit(r, 0)
	idx := map[string]int{}
	for i, l := range lines(r.stdout) {
		if f := strings.Fields(l); len(f) > 0 {
			if strings.TrimSpace(l) == "Brasil" || strings.TrimSpace(l) == "US" {
				idx["group:"+strings.TrimSpace(l)] = i
			} else if _, seen := idx[f[0]]; !seen {
				idx[f[0]] = i
			}
		}
	}
	need := func(k string) int {
		i, ok := idx[k]
		if !ok {
			e.failf("no line for %s", k)
			return -1
		}
		return i
	}
	p, a, gb, v, it, gu, m := need("PETR4"), need("AAPL"), need("group:Brasil"), need("VALE3"), need("ITUB4"), need("group:US"), need("MSFT")
	if !(p < a && a < gb && gb+1 == v && v < it && it < gu && gu+1 == m) {
		e.failf("order of lines: PETR4=%d AAPL=%d Brasil=%d VALE3=%d ITUB4=%d US=%d MSFT=%d; want PETR4, AAPL, `Brasil`, VALE3, ITUB4, `US`, MSFT", p, a, gb, v, it, gu, m)
	}
}

// S13.9 Given a source that does not answer, when `brl-stocks print` is run,
// then it prints and exits within 11 seconds.
// The stand-in accepts the connection and holds every answer for 30 seconds.
func TestVerifyS13_09_SilentSourceExitsWithin11s(t *testing.T) {
	t.Parallel()
	t.Run("empty cache", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, "S13.9 [empty cache]", baseConfig)
		e.setSource(state{DelayMS: 30000}, "every answer held for 30 s")
		r := e.run("print")
		e.wantExit(r, 0)
		if r.dur > 11*time.Second {
			e.failf("want exit within 11 s, took %s", r.dur)
		}
		if len(linesWithPrefix(r.stdout, "TICKER")) != 1 || len(rowLines(r.stdout, "PETR4")) != 1 {
			e.failf("want a table printed (header and rows), got %q", r.stdout)
		}
	})
	t.Run("with cache", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, "S13.9 [cached quotes, older than the interval]", baseConfig)
		e.run("print")
		e.setSource(state{DelayMS: 30000}, "every answer held for 30 s")
		e.sleep(pastInterval, "cache becomes older than the interval")
		r := e.run("print")
		e.wantExit(r, 0)
		if r.dur > 11*time.Second {
			e.failf("want exit within 11 s, took %s", r.dur)
		}
		if l := row(e, r.stdout, "PETR4"); !strings.Contains(l, "R$ 38,42") {
			e.failf("want the cached price printed, got %q", l)
		}
	})
}

// S13.10 Given the source is rate-limiting or refusing the tool, when
// `brl-stocks print --refresh` is run, then no request is sent.
// The first `print` meets the refusal. `--refresh` follows 6 seconds later, so
// that the 5-second guard of a forced fetch cannot be what holds it back.
func TestVerifyS13_10_BlockedRefreshSendsNothing(t *testing.T) {
	t.Parallel()
	for name, status := range map[string]int{"rate limit 429": 429, "access denied 403": 403, "access denied 401": 401} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, "S13.10 ["+name+"]", baseConfig)
			e.setSource(state{Status: status}, "every request refused")
			r1 := e.run("print")
			e.wantExit(r1, 0)
			if len(r1.reqs) == 0 {
				e.failf("set-up: the first print was expected to meet the refusal, but sent nothing")
			}
			e.sleep(6*time.Second, "past the 5-second guard of a forced fetch")
			r2 := e.run("print", "--refresh")
			e.wantExit(r2, 0)
			e.wantNoRequests(r2, "print --refresh while the source is blocking")
			r3 := e.run("print")
			e.wantNoRequests(r3, "print while the source is blocking")
		})
	}
}

// S13.11 Given an open market and a cache whose quotes were fetched at most 60
// seconds ago, when `brl-stocks print` is run, then the table shows the cached
// quotes. The source's price changes between the two runs; the second run must
// still show the first price.
func TestVerifyS13_11_FreshCacheShowsCachedQuotes(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S13.11", defaultIntervalConfig)
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.setSource(state{Quotes: map[string]override{"PETR4.SA": {Price: fp(99.99)}, "AAPL": {Price: fp(111.11)}}}, "PETR4 -> 99.99, AAPL -> 111.11")
	e.sleep(3*time.Second, "cache is 3 s old")
	r2 := e.run("print")
	e.wantExit(r2, 0)
	e.wantNoRequests(r2, "cache 3 s old")
	if l := row(e, r2.stdout, "PETR4"); !strings.Contains(l, "R$ 38,42") {
		e.failf("PETR4: want the cached `R$ 38,42`, got %q", l)
	}
	if l := row(e, r2.stdout, "AAPL"); !strings.Contains(l, "US$ 229.87") {
		e.failf("AAPL: want the cached `US$ 229.87`, got %q", l)
	}
	for _, tk := range tickers {
		if l := row(e, r2.stdout, tk); strings.Contains(l, "!") {
			e.failf("row %s is marked stale although its fetch succeeded 3 s ago: %q", tk, l)
		}
	}
}

// S13.12 Given a closed market with cached quotes, when `brl-stocks print` is
// run, then that market's rows show the last close.
func TestVerifyS13_12_ClosedMarketShowsLastClose(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S13.12", baseConfig)
	e.setSource(state{ClosedMarkets: []string{"b3"}}, "B3 closed 2 h ago, last close of PETR4 38.42")
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.setSource(state{ClosedMarkets: []string{"b3"}, Quotes: map[string]override{"PETR4.SA": {Price: fp(99.99)}}}, "the source would now answer 99.99 for PETR4")
	e.sleep(pastInterval, "cache becomes older than the interval")
	r2 := e.run("print")
	e.wantExit(r2, 0)
	l := row(e, r2.stdout, "PETR4")
	if !strings.Contains(l, "R$ 38,42") {
		e.failf("PETR4: want the last close `R$ 38,42`, got %q", l)
	}
	if strings.Contains(l, "!") {
		e.failf("PETR4: a row of a closed market is stale only if its last fetch failed (7.3), got %q", l)
	}
}

// S13.13 Given a default summary, when `brl-stocks print` is run, then the
// first line of the output starts with `IBOV`.
func TestVerifyS13_13_FirstLineStartsWithIBOV(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S13.13", baseConfig)
	r := e.run("print")
	e.wantExit(r, 0)
	if ls := lines(r.stdout); len(ls) == 0 || !strings.HasPrefix(ls[0], "IBOV") {
		e.failf("want the first line to start with IBOV, got %q", r.stdout)
	}
}

// S13.14 Given the US market is closed and B3 is open, when `brl-stocks print`
// is run, then the last line of the output reads `B3 open · US closed`.
func TestVerifyS13_14_LastLineMarketStates(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S13.14", baseConfig)
	e.setSource(state{ClosedMarkets: []string{"us"}}, "US closed 2 h ago, B3 open")
	for i, why := range []string{"run that fetches", "run that prints from the cache"} {
		r := e.run("print")
		e.wantExit(r, 0)
		ls := lines(r.stdout)
		if len(ls) == 0 || ls[len(ls)-1] != "B3 open · US closed" {
			last := ""
			if len(ls) > 0 {
				last = ls[len(ls)-1]
			}
			e.failf("%s (#%d): want the last line `B3 open · US closed`, got %q", why, i+1, last)
		}
	}
}

// S13.16 Given a row whose fetch failed 30 seconds ago, when `brl-stocks print`
// is run without `--refresh`, then that row is not requested.
// Tested literally: default interval; the fetch of VALE3 fails (503) on an
// empty cache, so the row has no quote; 30 seconds later the source is healthy.
// Without the wait, the rule "no quote in the cache: fetches" would request it.
func TestVerifyS13_16_FailedRowNotRequestedDuringWait(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S13.16", defaultIntervalConfig)
	e.setSource(state{SymbolStatus: map[string]int{"VALE3.SA": 503}}, "VALE3 answered 503, the rest normally")
	r1 := e.run("print")
	e.wantExit(r1, 0)
	if bySymbol(r1.reqs)["VALE3.SA"] == 0 {
		e.failf("set-up: VALE3 was expected to be requested (and to fail) in the first run")
	}
	e.setSource(state{}, "healthy again")
	e.sleep(30*time.Second, "the fetch of VALE3 failed 30 s ago")
	r2 := e.run("print")
	e.wantExit(r2, 0)
	e.wantNotRequested(r2, []string{"VALE3.SA"}, "row whose fetch failed 30 s ago")
}

// S13.17 Given a B3 row whose last price is 38.42, when `brl-stocks print` is
// run, then the table shows `R$ 38,42` in that row.
func TestVerifyS13_17_B3PriceFormat(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S13.17", baseConfig)
	r := e.run("print")
	e.wantExit(r, 0)
	if l := row(e, r.stdout, "PETR4"); !strings.Contains(l, "R$ 38,42") {
		e.failf("want `R$ 38,42` in the PETR4 row, got %q", l)
	}
}

// S13.18 Given a US row whose last price is 229.87, when `brl-stocks print` is
// run, then the table shows `US$ 229.87` in that row.
func TestVerifyS13_18_USPriceFormat(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S13.18", baseConfig)
	r := e.run("print")
	e.wantExit(r, 0)
	if l := row(e, r.stdout, "AAPL"); !strings.Contains(l, "US$ 229.87") {
		e.failf("want `US$ 229.87` in the AAPL row, got %q", l)
	}
}

// S13.19 Given a default summary and an Ibovespa value of 158432.10, when
// `brl-stocks print` is run, then the first line shows `158.432`.
func TestVerifyS13_19_IbovespaWholePoints(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S13.19", baseConfig)
	r := e.run("print")
	e.wantExit(r, 0)
	ls := lines(r.stdout)
	if len(ls) == 0 || !strings.Contains(ls[0], "158.432") || strings.Contains(ls[0], "158.432,") {
		e.failf("want `158.432` (whole points) in the first line, got %q", r.stdout)
	}
}

// S13.20 Given two rows whose names and prices have different lengths, when
// `brl-stocks print` is run, then the two prices end at the same column.
func TestVerifyS13_20_PricesEndAtSameColumn(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S13.20", baseConfig)
	e.setSource(state{Quotes: map[string]override{
		"VALE3.SA": {Name: sp("Vale ON Nome Bem Mais Comprido"), Price: fp(1038.42), PrevClose: fp(1001.10), Low: fp(999.90), High: fp(1040.00)},
		"ITUB4.SA": {Name: sp("Itau"), Price: fp(4.15), PrevClose: fp(4.10), Low: fp(4.01), High: fp(4.20)},
	}}, "VALE3: long name, price 1038.42; ITUB4: short name, price 4.15")
	r := e.run("print")
	e.wantExit(r, 0)
	if l := row(e, r.stdout, "VALE3"); !strings.Contains(l, "R$ 1.038,42") {
		e.failf("VALE3: want `R$ 1.038,42` (dot for thousands), got %q", l)
	}
	ends := map[string]int{}
	for _, tk := range tickers {
		l := row(e, r.stdout, tk)
		loc := priceRE.FindStringIndex(l)
		if loc == nil {
			e.failf("row %s shows no price: %q", tk, l)
			continue
		}
		ends[tk] = utf8.RuneCountInString(l[:loc[1]])
	}
	e.logf("PRICE END COLUMNS  %v\n", ends)
	for _, tk := range tickers {
		if ends[tk] != ends["PETR4"] {
			e.failf("prices do not end at the same column: PETR4 at %d, %s at %d", ends["PETR4"], tk, ends[tk])
		}
	}
}

// S13.21 Given a source that is rate-limiting and cached quotes, when
// `brl-stocks print` is run, then one line on the error stream starts with
// `source is rate-limiting`.
// Asserted on the run that meets the refusal, and on the next run, during the
// block (the source is still rate-limiting and the quotes are still cached).
func TestVerifyS13_21_RateLimitErrorLine(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S13.21", baseConfig)
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.setSource(state{Status: 429}, "every request answered 429")
	e.sleep(pastInterval, "cache becomes older than the interval")
	r2 := e.run("print")
	e.wantExit(r2, 0)
	if n := len(linesWithPrefix(r2.stderr, "source is rate-limiting")); n != 1 {
		e.failf("run that meets the refusal: want 1 line on the error stream starting with `source is rate-limiting`, got %d; error stream: %q", n, r2.stderr)
	}
	if l := row(e, r2.stdout, "PETR4"); !strings.Contains(l, "R$ 38,42") {
		e.failf("want the cached quote still printed, got %q", l)
	}
	e.sleep(2*time.Second, "a later call during the block")
	r3 := e.run("print")
	e.wantExit(r3, 0)
	if n := len(linesWithPrefix(r3.stderr, "source is rate-limiting")); n != 1 {
		e.failf("run during the block: want 1 line on the error stream starting with `source is rate-limiting`, got %d; error stream: %q", n, r3.stderr)
	}
	e.wantNoRequests(r3, "run during the block")
}

// ---------------------------------------------------------------------------
// S14 - One-shot JSON
// ---------------------------------------------------------------------------

// S14.1 Given a valid config, when `brl-stocks print --json` is run, then
// standard output is exactly one valid JSON document.
func TestVerifyS14_01_OneJSONDocument(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S14.1", baseConfig)
	r := e.run("print", "--json")
	e.wantExit(r, 0)
	parseDoc(e, r.stdout)
}

// S14.2 Given a row with a quote, when `brl-stocks print --json` is run, then
// the row's object carries ticker, name, last price, day change value, day
// change percent, day low, day high and quote time.
// S14.3 ... it has a `schema_version` of 1.
// S14.9 ... its prices are JSON numbers.
// S14.10 ... its times are UTC in RFC 3339.
func TestVerifyS14_02_03_09_10_RowObject(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S14.2, S14.3, S14.9, S14.10", baseConfig)
	r := e.run("print", "--json")
	e.wantExit(r, 0)
	doc := parseDoc(e, r.stdout)
	if doc == nil {
		return
	}
	if v, _ := doc["schema_version"].(json.Number); v.String() != "1" {
		e.failf("S14.3: want schema_version 1, got %v", doc["schema_version"])
	}
	p := docRows(doc)["PETR4"]
	if p == nil {
		e.failf("no row object for PETR4")
		return
	}
	if p["name"] != "Petrobras PN" {
		e.failf("S14.2: name: want \"Petrobras PN\", got %v", p["name"])
	}
	for k, want := range map[string]float64{"price": 38.42, "change": 0.31, "change_percent": 0.8134, "day_low": 38.02, "day_high": 38.60} {
		n, ok := p[k].(json.Number)
		if !ok {
			e.failf("S14.2/S14.9: %s: want a JSON number, got %T %v", k, p[k], p[k])
			continue
		}
		f, _ := n.Float64()
		if d := f - want; d > 0.0001 || d < -0.0001 {
			e.failf("S14.2: %s: want about %v, got %s", k, want, n)
		}
	}
	for _, k := range []string{"quote_time", "fetched_at"} {
		s, _ := p[k].(string)
		if _, err := time.Parse(time.RFC3339, s); err != nil || !strings.HasSuffix(s, "Z") {
			e.failf("S14.10: %s: want a UTC time in RFC 3339, got %v", k, p[k])
		}
	}
	if s, _ := doc["generated_at"].(string); !strings.HasSuffix(s, "Z") {
		e.failf("S14.10: generated_at: want a UTC time in RFC 3339, got %v", doc["generated_at"])
	} else if _, err := time.Parse(time.RFC3339, s); err != nil {
		e.failf("S14.10: generated_at: %v", err)
	}
}

// S14.4 Given every row is fresh, when `brl-stocks print --json` is run, then
// `state` is `ok`.
func TestVerifyS14_04_StateOK(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S14.4", baseConfig)
	r := e.run("print", "--json")
	e.wantExit(r, 0)
	doc := parseDoc(e, r.stdout)
	if doc == nil {
		return
	}
	for tk, rw := range docRows(doc) {
		if rw["status"] != "fresh" {
			e.failf("set-up: row %s was expected to be fresh, is %v", tk, rw["status"])
		}
	}
	if doc["state"] != "ok" {
		e.failf("want state \"ok\", got %v", doc["state"])
	}
}

// S14.5 Given one stale row and the others fresh, when `brl-stocks print
// --json` is run, then `state` is `partial`.
// VALE3's fetch fails (503) once the cache is older than the interval.
func TestVerifyS14_05_StatePartial(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S14.5", baseConfig)
	e.run("print", "--json")
	e.setSource(state{SymbolStatus: map[string]int{"VALE3.SA": 503}}, "VALE3 answered 503, the rest normally")
	e.sleep(pastInterval, "cache becomes older than the interval")
	r := e.run("print", "--json")
	e.wantExit(r, 0)
	doc := parseDoc(e, r.stdout)
	if doc == nil {
		return
	}
	for tk, rw := range docRows(doc) {
		want := "fresh"
		if tk == "VALE3" {
			want = "stale"
		}
		if rw["status"] != want {
			e.failf("row %s: want status %q, got %v", tk, want, rw["status"])
		}
	}
	if doc["state"] != "partial" {
		e.failf("want state \"partial\", got %v", doc["state"])
	}
}

// S14.6 Given no cache and a source that cannot be reached, when `brl-stocks
// print --json` is run, then standard output is a valid document with `state`
// `error`.
// S14.7 ... then the exit status is 0.
func TestVerifyS14_06_07_NoCacheUnreachable(t *testing.T) {
	t.Parallel()
	for _, v := range unreachableVariants {
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, "S14.6, S14.7 ["+v+"]", baseConfig)
			if v == "status 503" {
				e.setSource(state{Status: 503}, "every request answered 503")
			} else {
				e.logf("\nSTAND-IN   stopped: its address now refuses connections\n")
				e.src.stop()
			}
			r := e.run("print", "--json")
			if r.exit != 0 {
				e.failf("S14.7: want exit status 0, got %d", r.exit)
			}
			doc := parseDoc(e, r.stdout)
			if doc == nil {
				return
			}
			if doc["state"] != "error" {
				e.failf("S14.6: want state \"error\", got %v", doc["state"])
			}
			if doc["error"] == nil {
				e.failf("7.6: with state `error`, `error` holds the reason; got null")
			}
		})
	}
}

// S14.8 Given a config file that cannot be parsed, when `brl-stocks print
// --json` is run, then the exit status is 2.
// 7.6 adds: standard output still carries a valid document with state `error`,
// and the message is on the error stream.
func TestVerifyS14_08_UnparsableConfigExit2(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S14.8 (and 7.6)", "tickers = [\"PETR4\"\nthis is not toml ===\n")
	r := e.run("print", "--json")
	e.wantExit(r, 2)
	e.wantNoRequests(r, "invalid config")
	if strings.TrimSpace(r.stderr) == "" {
		e.failf("7.6: want a message on the error stream, it is empty")
	}
	if doc := parseDoc(e, r.stdout); doc != nil && doc["state"] != "error" {
		e.failf("7.6: want state \"error\", got %v", doc["state"])
	}
	r2 := e.run("print")
	e.wantExit(r2, 2)
}

// S14.11 Given a closed market, when `brl-stocks print --json` is run, then the
// rows of that market have `market_state` `closed`.
func TestVerifyS14_11_MarketStateClosed(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S14.11", baseConfig)
	e.setSource(state{ClosedMarkets: []string{"us"}}, "US closed 2 h ago, B3 open")
	r := e.run("print", "--json")
	e.wantExit(r, 0)
	doc := parseDoc(e, r.stdout)
	if doc == nil {
		return
	}
	for tk, want := range map[string]string{"AAPL": "closed", "MSFT": "closed", "PETR4": "open", "VALE3": "open"} {
		if got := docRows(doc)[tk]["market_state"]; got != want {
			e.failf("row %s: want market_state %q, got %v", tk, want, got)
		}
	}
}

// S14.12 Given a B3 row whose last price is 38.425, when `brl-stocks print
// --json` is run, then its `price` is the number 38.425.
func TestVerifyS14_12_PriceNotRounded(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S14.12", baseConfig)
	e.setSource(state{Quotes: map[string]override{"PETR4.SA": {Price: fp(38.425)}}}, "PETR4 last price 38.425")
	r := e.run("print", "--json")
	e.wantExit(r, 0)
	doc := parseDoc(e, r.stdout)
	if doc == nil {
		return
	}
	n, ok := docRows(doc)["PETR4"]["price"].(json.Number)
	if !ok || n.String() != "38.425" {
		e.failf("want price to be the number 38.425, got %v", docRows(doc)["PETR4"]["price"])
	}
}

// ---------------------------------------------------------------------------
// S15 - The last quotes are kept between runs
// ---------------------------------------------------------------------------

// S15.1 Given a run that fetched quotes and then ended, when the tool is
// started again with no network, then the rows show those quotes.
func TestVerifyS15_01_QuotesKeptWithoutNetwork(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S15.1", baseConfig)
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.logf("\nSTAND-IN   stopped: its address now refuses connections\n")
	e.src.stop()
	check := func(r result, when string) {
		e.wantExit(r, 0)
		for tk, price := range map[string]string{"PETR4": "R$ 38,42", "AAPL": "US$ 229.87", "VALE3": "R$ 61,07", "ITUB4": "R$ 34,15", "MSFT": "US$ 512.30"} {
			if l := row(e, r.stdout, tk); !strings.Contains(l, price) {
				e.failf("%s: row %s: want the kept quote %q, got %q", when, tk, price, l)
			}
		}
	}
	check(e.run("print"), "right after")
	e.sleep(pastInterval, "cache becomes older than the interval")
	check(e.run("print"), "after the interval")
}

// S15.2 Given the tool has fetched quotes, when the files it keeps are listed,
// then they are under `~/.cache/brl-stocks/`.
// XDG_CACHE_HOME is left unset here; HOME is a temporary folder.
func TestVerifyS15_02_FilesUnderHomeCache(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S15.2", baseConfig)
	e.noCacheHome = true
	r := e.run("print")
	e.wantExit(r, 0)
	want := filepath.Join(e.home, ".cache", "brl-stocks") + string(filepath.Separator)
	kept := 0
	filepath.WalkDir(e.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == e.cfgPath {
			return nil
		}
		e.logf("FILE       %s\n", strings.TrimPrefix(p, e.root))
		if strings.HasPrefix(p, want) {
			kept++
		} else {
			e.failf("a file outside ~/.cache/brl-stocks/: %s", strings.TrimPrefix(p, e.root))
		}
		return nil
	})
	if kept == 0 {
		e.failf("no file under ~/.cache/brl-stocks/ after a run that fetched quotes")
	}
}

// S15.4 Given a cache file emptied or filled with garbage, when the tool
// starts, then it shows the watchlist as it does with no cache.
// S15.7 ... then no message about the cache is shown.
// The source answers 503, so the output does not depend on the time: with no
// cache the rows hold no data. The damaged-cache run must print the same.
func TestVerifyS15_04_07_DamagedCacheIsAbsentAndSilent(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"emptied", "garbage"} {
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			base := newEnv(t, "S15.4, S15.7 ["+v+"] (baseline: no cache)", baseConfig)
			base.setSource(state{Status: 503}, "every request answered 503")
			rb := base.run("print")

			e := newEnv(t, "S15.4, S15.7 ["+v+"]", baseConfig)
			r1 := e.run("print")
			e.wantExit(r1, 0)
			n := 0
			filepath.WalkDir(e.cacheHome, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				content := []byte{}
				if v == "garbage" {
					content = []byte("\x00\xff{{{ not a cache ]]] \x01\x02 garbage\n")
				}
				if err := os.WriteFile(p, content, 0o644); err != nil {
					t.Fatal(err)
				}
				e.logf("DAMAGED    %s (%s)\n", strings.TrimPrefix(p, e.root), v)
				n++
				return nil
			})
			if n == 0 {
				e.failf("set-up: no cache file found to damage")
			}
			e.setSource(state{Status: 503}, "every request answered 503")
			r2 := e.run("print")
			e.wantExit(r2, 0)
			e.logf("\nBASELINE (no cache, same source) STDOUT\n%s\nBASELINE STDERR\n%s\n", indent(rb.stdout), indent(rb.stderr))
			if r2.stdout != rb.stdout {
				e.failf("S15.4: standard output differs from a run with no cache:\n--- no cache\n%s\n--- damaged cache\n%s", rb.stdout, r2.stdout)
			}
			if fmtCounts(bySymbol(r2.reqs)) != fmtCounts(bySymbol(rb.reqs)) {
				e.failf("S15.4: requests differ from a run with no cache: %s vs %s", fmtCounts(bySymbol(rb.reqs)), fmtCounts(bySymbol(r2.reqs)))
			}
			if strings.Contains(strings.ToLower(r2.stderr), "cache") || strings.Contains(strings.ToLower(r2.stdout), "cache") {
				e.failf("S15.7: a message about the cache is shown: %q", r2.stderr)
			}
			if r2.stderr != rb.stderr {
				e.failf("S15.7: the error stream differs from a run with no cache:\n--- no cache\n%s\n--- damaged cache\n%s", rb.stderr, r2.stderr)
			}
		})
	}
}

// S15.8 Given a cache folder that cannot be written, when the tool runs, then
// one warning says the quotes will not be kept.
// S15.9 Given a cache folder that cannot be written, when `brl-stocks print`
// is run, then no request is sent to the source.
func TestVerifyS15_08_09_UnwritableCacheFolder(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("running as root: a folder cannot be made unwritable")
	}
	warnings := func(e *env, r result) {
		ls := lines(r.stderr)
		n := 0
		for _, l := range ls {
			if strings.Contains(strings.ToLower(l), "kept") || strings.Contains(strings.ToLower(l), "cache") {
				n++
			}
		}
		if n != 1 {
			e.failf("S15.8: want exactly one warning that the quotes will not be kept, found %d line(s) naming it; error stream: %q", n, r.stderr)
		}
	}
	t.Run("empty folder, read-only", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, "S15.8, S15.9 [the cache folder exists, is empty, mode 0555]", baseConfig)
		dir := filepath.Join(e.cacheHome, "brl-stocks")
		os.MkdirAll(dir, 0o755)
		os.Chmod(dir, 0o555)
		t.Cleanup(func() { os.Chmod(dir, 0o755) })
		r := e.run("print")
		e.wantExit(r, 0)
		e.wantNoRequests(r, "S15.9: unwritable cache folder")
		warnings(e, r)
		r2 := e.run("print", "--refresh")
		e.wantNoRequests(r2, "S15.9: unwritable cache folder, --refresh")
	})
	t.Run("parent read-only, folder missing", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, "S15.8, S15.9 [XDG_CACHE_HOME is mode 0555 and holds no brl-stocks folder]", baseConfig)
		os.Chmod(e.cacheHome, 0o555)
		t.Cleanup(func() { os.Chmod(e.cacheHome, 0o755) })
		r := e.run("print")
		e.wantExit(r, 0)
		e.wantNoRequests(r, "S15.9: unwritable cache folder")
		warnings(e, r)
	})
	t.Run("kept quotes, then read-only", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, "S15.8, S15.9 [quotes kept, then folder 0555 and files 0444, cache older than the interval]", baseConfig)
		r1 := e.run("print")
		e.wantExit(r1, 0)
		dir := filepath.Join(e.cacheHome, "brl-stocks")
		var paths []string
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil {
				paths = append(paths, p)
			}
			return nil
		})
		for i := len(paths) - 1; i >= 0; i-- {
			if st, err := os.Stat(paths[i]); err == nil {
				mode := st.Mode().Perm() &^ 0o222
				os.Chmod(paths[i], mode)
				p, m := paths[i], st.Mode().Perm()
				t.Cleanup(func() { os.Chmod(p, m) })
			}
		}
		e.sleep(pastInterval, "cache becomes older than the interval")
		r2 := e.run("print")
		e.wantExit(r2, 0)
		e.wantNoRequests(r2, "S15.9: unwritable cache folder")
		warnings(e, r2)
		if l := row(e, r2.stdout, "PETR4"); !strings.Contains(l, "R$ 38,42") {
			e.failf("6.5: want what the cache holds printed, got %q", l)
		}
	})
}

// ---------------------------------------------------------------------------
// Quality requirements
// ---------------------------------------------------------------------------

func config20() (string, []string) {
	b3 := []string{"PETR4", "VALE3", "ITUB4", "BBDC4", "ABEV3", "WEGE3", "BBAS3", "MGLU3", "SUZB3", "RENT3"}
	us := []string{"AAPL", "MSFT", "GOOG", "AMZN", "NVDA", "META", "TSLA", "NFLX", "ORCL", "INTC"}
	syms := []string{"^BVSP", "^GSPC", "BRL=X"}
	for _, t := range b3 {
		syms = append(syms, t+".SA")
	}
	syms = append(syms, us...)
	q := func(l []string) string { return `"` + strings.Join(l, `", "`) + `"` }
	return "[[group]]\nname = \"Brasil\"\ntickers = [" + q(b3) + "]\n\n[[group]]\nname = \"US\"\ntickers = [" + q(us) + "]\n", syms
}

// QR3 A one-shot call on a fresh cache is cheap: it sends zero requests and
// ends within 100 ms. Measured with a counting stand-in source and wall time,
// 20 tickers, median of 10 runs.
// Not parallel: the wall time is measured while no other check runs.
func TestVerifyQR3_FreshCacheZeroRequestsWithin100ms(t *testing.T) {
	cfg, syms := config20()
	e := newEnv(t, "QR3", cfg)
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.wantRequested(r1, syms, "first run, empty cache, 20 tickers and 3 summary entries")
	var durs []time.Duration
	for i := 0; i < 10; i++ {
		args := []string{"print"}
		if i%2 == 1 {
			args = append(args, "--json")
		}
		r := e.run(args...)
		e.wantExit(r, 0)
		e.wantNoRequests(r, fmt.Sprintf("fresh cache, run %d", i+1))
		durs = append(durs, r.dur)
	}
	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
	median := (durs[4] + durs[5]) / 2
	e.logf("\nWALL TIMES (sorted) %v   MEDIAN %s\n", durs, median)
	if median > 100*time.Millisecond {
		e.failf("want a median wall time of at most 100 ms, got %s", median)
	}
}

// QR7 After any run, the config file is byte-identical and a symbolic link in
// its path still points to the same target. Measured by checksum and link
// target before and after.
// Here the config folder is a symbolic link, and config.toml inside it is a
// symbolic link too. (Every other check of this suite also compares the
// checksum of its config file around each run.)
func TestVerifyQR7_ConfigNeverTouched(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "QR7", "")
	realDir := filepath.Join(e.root, "dotfiles", "brl-stocks")
	realFile := filepath.Join(e.root, "dotfiles", "watchlist.toml")
	linkDir := filepath.Join(e.cfgHome, "brl-stocks")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(realFile, []byte(baseConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(linkDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realFile, filepath.Join(realDir, "config.toml")); err != nil {
		t.Fatal(err)
	}
	snap := func() string {
		var b strings.Builder
		for _, p := range []string{linkDir, filepath.Join(realDir, "config.toml")} {
			st, err := os.Lstat(p)
			target, _ := os.Readlink(p)
			fmt.Fprintf(&b, "%s: err=%v symlink=%v -> %s\n", strings.TrimPrefix(p, e.root), err, err == nil && st.Mode()&os.ModeSymlink != 0, strings.TrimPrefix(target, e.root))
		}
		st, _ := os.Stat(realFile)
		fmt.Fprintf(&b, "watchlist.toml: sha256=%s mtime=%s mode=%s\n", fileSum(realFile), st.ModTime().UTC().Format(time.RFC3339Nano), st.Mode())
		ents, _ := os.ReadDir(realDir)
		for _, en := range ents {
			fmt.Fprintf(&b, "entry in config folder: %s\n", en.Name())
		}
		ents, _ = os.ReadDir(filepath.Dir(realFile))
		for _, en := range ents {
			fmt.Fprintf(&b, "entry beside the target: %s\n", en.Name())
		}
		return b.String()
	}
	before := snap()
	e.logf("\nBEFORE\n%s", indent(before))
	steps := []struct {
		what string
		st   *state
		wait time.Duration
		args []string
	}{
		{"healthy source", nil, 0, []string{"print"}},
		{"", nil, 0, []string{"print", "--json"}},
		{"", nil, 6 * time.Second, []string{"print", "--refresh"}},
		{"through --config naming the link", nil, 0, []string{"--config", filepath.Join(linkDir, "config.toml"), "print"}},
		{"source answers 503", &state{Status: 503}, pastInterval, []string{"print"}},
		{"source answers 429", &state{Status: 429}, pastInterval, []string{"print"}},
	}
	for _, s := range steps {
		if s.st != nil {
			e.setSource(*s.st, s.what)
		}
		if s.wait > 0 {
			e.sleep(s.wait, "time passes")
		}
		r := e.run(s.args...)
		e.wantExit(r, 0)
		if len(rowLines(r.stdout, "PETR4")) == 0 && !strings.Contains(r.stdout, "PETR4") {
			e.failf("the config was expected to be read through the links (no PETR4 in the output)")
		}
		if after := snap(); after != before {
			e.failf("after `%s`: the config or its links changed:\n--- before\n%s--- after\n%s", strings.Join(s.args, " "), before, after)
		}
	}
	e.logf("\nAFTER\n%s", indent(snap()))
}

// QR4 (scaled down) Request load in normal use. The spec measures 10 minutes
// with the view open, a 60-second interval and `print` every 2 seconds: at
// most 15 requests per entry (one per interval and half as many again).
// Here: no view (it needs a terminal), refresh_seconds = 10, `print` every 2
// seconds for 30 seconds. One request per entry per interval, and half as many
// again: at most 1.5 x 4 = 6 per entry (the intervals starting at 0, 10, 20
// and 30 s), and at least 3 (the quotes must not go unrefreshed).
func TestVerifyQR4_Scaled_PrintEveryTwoSeconds(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "QR4 (scaled: 30 s, interval 10 s, no view)", baseConfig)
	start := time.Now()
	runs := 0
	for time.Since(start) < 30*time.Second {
		next := time.Now().Add(2 * time.Second)
		r := e.run("print")
		e.wantExit(r, 0)
		runs++
		time.Sleep(time.Until(next))
	}
	c := bySymbol(e.src.requests())
	e.logf("\nRUNS %d   REQUESTS PER ENTRY %s\n", runs, fmtCounts(c))
	for _, s := range allSyms {
		if c[s] > 6 {
			e.failf("%s: want at most 6 requests in 30 s with a 10-second interval, the stand-in received %d", s, c[s])
		}
		if c[s] < 3 {
			e.failf("%s: want at least 3 requests in 30 s with a 10-second interval, the stand-in received %d", s, c[s])
		}
	}
}

// runTen starts ten `print` calls at the same moment and returns their results.
func runTen(e *env, args ...string) []result {
	const n = 10
	res := make([]result, n)
	gate := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-gate
			res[i] = e.exec(args...)
		}(i)
	}
	close(gate)
	wg.Wait()
	for i, r := range res {
		e.logf("\n$ brl-stocks %s   (process %d of %d, started together)\nEXIT       %d   (wall time %s)\nSTDOUT\n%s\nSTDERR\n%s\n",
			strings.Join(args, " "), i+1, n, r.exit, r.dur.Round(time.Millisecond), indent(r.stdout), indent(r.stderr))
	}
	return res
}

func lastLine(s string) string {
	ls := lines(s)
	if len(ls) == 0 {
		return ""
	}
	return ls[len(ls)-1]
}

// 6.3 (not a numbered criterion): "Normal | The source refuses a request |
// Blocked | No more fetches; rows keep their prices; the rows whose fetch was
// refused are stale; the error line says so".
// Cached quotes older than the interval, then every request is answered 429.
// Every row whose request the stand-in answered with a refusal must keep its
// price and carry the stale mark (table) or status `stale` (JSON).
func TestVerifyW6_3_RefusedRowsAreStale(t *testing.T) {
	t.Parallel()
	symOf := map[string]string{"PETR4": "PETR4.SA", "AAPL": "AAPL", "VALE3": "VALE3.SA", "ITUB4": "ITUB4.SA", "MSFT": "MSFT"}
	e := newEnv(t, "6.3 row 1: the rows whose fetch was refused are stale", baseConfig)
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.setSource(state{Status: 429}, "every request answered 429")
	e.sleep(pastInterval, "cache becomes older than the interval")
	r2 := e.run("print")
	e.wantExit(r2, 0)
	refused := map[string]bool{}
	for _, r := range r2.reqs {
		if r.Status == 429 {
			refused[r.Symbol] = true
		}
	}
	if len(refused) == 0 {
		e.failf("set-up: no request was refused")
	}
	for _, tk := range tickers {
		l := row(e, r2.stdout, tk)
		if !hasQuote(l) {
			e.failf("row %s lost its price: %q", tk, l)
		}
		if refused[symOf[tk]] && !staleRE.MatchString(l) {
			e.failf("table: the stand-in refused the request for %s (429), but the row carries no stale mark: %q", tk, l)
		}
	}
	r3 := e.run("print", "--json")
	e.wantExit(r3, 0)
	if doc := parseDoc(e, r3.stdout); doc != nil {
		for _, tk := range tickers {
			if rw := docRows(doc)[tk]; refused[symOf[tk]] && rw["status"] != "stale" {
				e.failf("JSON: the stand-in refused the request for %s (429), but its status is %v", tk, rw["status"])
			}
		}
		e.logf("JSON state: %v\n", doc["state"])
	}
}

// QR5 From the first refused answer, at most 10 requests in any 10 minutes
// while the source keeps refusing, all running instances together.
// Part 1 (not in the spec's own measurement, but covered by its number): ten
// `print` calls at the same moment against a source that refuses everything,
// empty cache, 20 tickers and 3 summary entries. Counted: the requests that
// reached the stand-in AFTER it had written its first refusal.
// Not parallel.
func TestVerifyQR5_TenPrintsAgainstRefusingSource(t *testing.T) {
	cfg, _ := config20()
	e := newEnv(t, "QR5: ten `print` at the same moment, source refuses everything (429), 23 entries", cfg)
	e.setSource(state{Status: 429}, "every request answered 429")
	res := runTen(e, "print")
	for i, r := range res {
		if r.exit != 0 {
			e.failf("process %d: exit status %d", i+1, r.exit)
		}
	}
	time.Sleep(500 * time.Millisecond)
	recs := e.src.requests()
	var firstRefusal int64
	for _, r := range recs {
		if r.AnsweredMS > 0 && (firstRefusal == 0 || r.AnsweredMS < firstRefusal) {
			firstRefusal = r.AnsweredMS
		}
	}
	after := 0
	for _, r := range recs {
		if r.UnixMS > firstRefusal {
			after++
		}
	}
	e.logf("\nREQUESTS IN TOTAL %d   ARRIVED AFTER THE FIRST REFUSAL WAS WRITTEN %d   PER SYMBOL %s\n", len(recs), after, fmtCounts(bySymbol(recs)))
	t.Logf("requests in total %d, arrived after the first refusal was written %d", len(recs), after)
	if len(recs) == 0 {
		e.failf("set-up: nothing was requested")
	}
	if after > 10 {
		e.failf("want at most 10 requests after the first refused answer, the stand-in received %d (%d in total)", after, len(recs))
	}
}

// QR5 / 6.3 A block under load: after a refusal, `print --refresh` is run every
// 2 seconds for 30 seconds; the stand-in must receive nothing. (The spec's
// measurement also has the view open and lasts 10 minutes; the view needs a
// terminal and is outside this suite.)
func TestVerifyQR5_BlockUnderLoad(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "QR5 / 6.3: after a refusal, `print --refresh` every 2 s for 30 s sends nothing", baseConfig)
	e.setSource(state{Status: 429}, "every request answered 429")
	r0 := e.run("print")
	e.wantExit(r0, 0)
	if len(r0.reqs) == 0 {
		e.failf("set-up: the first print was expected to meet the refusal")
	}
	if len(r0.reqs) > 10 {
		e.failf("QR5: the call that met the refusal sent %d requests; at most 10 in any 10 minutes", len(r0.reqs))
	}
	before := len(e.src.requests())
	start := time.Now()
	runs := 0
	for time.Since(start) < 30*time.Second {
		next := time.Now().Add(2 * time.Second)
		r := e.run("print", "--refresh")
		e.wantExit(r, 0)
		runs++
		time.Sleep(time.Until(next))
	}
	got := e.src.requests()[before:]
	e.logf("\nRUNS %d OF `print --refresh` IN 30 s   REQUESTS RECEIVED MEANWHILE: %d\n", runs, len(got))
	if len(got) != 0 {
		e.failf("want 0 requests during the block, the stand-in received %d (%s)", len(got), fmtCounts(bySymbol(got)))
	}
}

// 6.3 Recovery (SLOW: a little over 5 minutes; run with BRL_VERIFY_SLOW=1).
// "Blocked | The wait ends and the source answers | Normal | Refresh resumes;
// the error line goes", after "a first wait of 5 minutes".
// The source refuses once and goes back to normal at once. During the wait
// nothing may be sent; once it has ended, the next `print` must lead to quotes
// again. The request record shows how the tool resumed.
func TestVerifySlow_W6_3_RecoveryAfterBlock(t *testing.T) {
	if os.Getenv("BRL_VERIFY_SLOW") != "1" {
		t.Skip("slow check (5 minutes of real time): set BRL_VERIFY_SLOW=1")
	}
	t.Parallel()
	e := newEnv(t, "6.3: Blocked -> the wait of 5 minutes ends and the source answers -> Normal", baseConfig)
	e.setSource(state{Status: 429}, "every request answered 429")
	r0 := e.run("print")
	e.wantExit(r0, 0)
	if len(r0.reqs) == 0 {
		t.Fatal("set-up: the first print was expected to meet the refusal")
	}
	var lastRefusal int64
	for _, r := range r0.reqs {
		if r.AnsweredMS > lastRefusal {
			lastRefusal = r.AnsweredMS
		}
	}
	refusedAt := time.UnixMilli(lastRefusal)
	e.setSource(state{}, "healthy again, right after the refusal")

	for _, at := range []time.Duration{20 * time.Second, 150 * time.Second, 285 * time.Second} {
		e.sleep(time.Until(refusedAt.Add(at)), fmt.Sprintf("until %s after the refusal", at))
		r := e.run("print", "--refresh")
		e.wantExit(r, 0)
		e.wantNoRequests(r, fmt.Sprintf("print --refresh %s after the refusal (the wait is 5 minutes)", at))
	}

	e.sleep(time.Until(refusedAt.Add(5*time.Minute+8*time.Second)), "until 5 min 8 s after the refusal: the wait has ended")
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.logf("\nHOW THE TOOL RESUMED: the first print after the wait led to %d request(s): %s\n", len(r1.reqs), fmtCounts(bySymbol(r1.reqs)))
	t.Logf("first print after the wait: %d request(s): %s", len(r1.reqs), fmtCounts(bySymbol(r1.reqs)))
	if len(r1.reqs) == 0 {
		e.failf("the wait has ended, but the first print sent no request")
	}
	quoted := 0
	for _, tk := range tickers {
		if ls := rowLines(r1.stdout, tk); len(ls) == 1 && hasQuote(ls[0]) {
			quoted++
		}
	}
	e.logf("ROWS WITH A QUOTE AFTER THE FIRST PRINT: %d of %d\n", quoted, len(tickers))
	t.Logf("rows with a quote after the first print: %d of %d", quoted, len(tickers))

	e.sleep(6*time.Second, "a second call")
	r2 := e.run("print")
	e.wantExit(r2, 0)
	e.logf("\nSECOND PRINT AFTER THE WAIT: %d request(s): %s\n", len(r2.reqs), fmtCounts(bySymbol(r2.reqs)))
	t.Logf("second print after the wait: %d request(s): %s", len(r2.reqs), fmtCounts(bySymbol(r2.reqs)))
	for _, tk := range tickers {
		if ls := rowLines(r2.stdout, tk); len(ls) != 1 || !hasQuote(ls[0]) {
			e.failf("after recovery, row %s still shows no quote: %q", tk, ls)
		}
	}
	if quoted == 0 && len(r1.reqs) > 1 {
		e.failf("the first print after the wait sent a full round but printed no quote")
	}
	if n := len(linesWithPrefix(r2.stderr, "source ")); n != 0 {
		e.failf("after recovery the error line should be gone; error stream: %q", r2.stderr)
	}
}

// ---------------------------------------------------------------------------
// S13.22, S13.23 (D73), S13.24 (D74)
// ---------------------------------------------------------------------------

var (
	rowSyms = []string{"PETR4.SA", "AAPL", "VALE3.SA", "ITUB4.SA", "MSFT"} // the watchlist lines of baseConfig
	sumSyms = []string{"^BVSP", "^GSPC", "BRL=X"}                          // the summary entries
)

// S13.22 Given an empty cache and a source that answers, when ten `brl-stocks
// print` calls are started at the same moment, then every one of them prints a
// quote for each row.
// S13.23 Given the same, then the source receives one request per row.
// A row is a watchlist line (glossary); the summary entries are not rows, and
// their counts are reported, not asserted. Table and JSON. Not parallel, so
// that the ten processes are not slowed by other checks.
func TestVerifyS13_22_23_TenPrintsOnEmptyCache(t *testing.T) {
	for _, mode := range []string{"table", "json"} {
		t.Run(mode, func(t *testing.T) {
			args := []string{"print"}
			if mode == "json" {
				args = append(args, "--json")
			}
			e := newEnv(t, "S13.22, S13.23: ten `"+strings.Join(args, " ")+"` at the same moment, empty cache, healthy source", baseConfig)
			res := runTen(e, args...)
			without := 0
			for i, r := range res {
				if r.exit != 0 {
					e.failf("process %d: exit status %d", i+1, r.exit)
				}
				var missing []string
				if mode == "table" {
					for _, tk := range tickers {
						if ls := rowLines(r.stdout, tk); len(ls) != 1 || !hasQuote(ls[0]) {
							missing = append(missing, tk)
						}
					}
					e.logf("process %d: summary line %q\n", i+1, firstLine(r.stdout))
				} else if doc := parseDoc(e, r.stdout); doc != nil {
					rows := docRows(doc)
					for _, tk := range tickers {
						if rw, ok := rows[tk]; !ok || rw["price"] == nil {
							missing = append(missing, tk)
						}
					}
					e.logf("process %d: state %v, summary %v\n", i+1, doc["state"], summaryStatuses(doc))
				}
				if len(missing) > 0 {
					without++
					e.failf("S13.22: process %d: rows without a quote: %v; its output ends with %q", i+1, missing, lastLine(r.stdout))
				}
			}
			c := bySymbol(e.src.requests())
			rc, sc := map[string]int{}, map[string]int{}
			for _, s := range rowSyms {
				rc[s] = c[s]
			}
			for _, s := range sumSyms {
				sc[s] = c[s]
			}
			e.logf("\nPROCESSES WITH A ROW WITHOUT A QUOTE: %d of 10\nREQUESTS PER ROW, TEN PROCESSES TOGETHER: %s\nREQUESTS PER SUMMARY ENTRY: %s\nALL: %s\n",
				without, fmtCounts(rc), fmtCounts(sc), fmtCounts(c))
			t.Logf("rows: %s; summary entries: %s", fmtCounts(rc), fmtCounts(sc))
			for _, s := range rowSyms {
				if c[s] != 1 {
					e.failf("S13.23: want exactly 1 request for the row %s, the stand-in received %d", s, c[s])
				}
			}
		})
	}
}

func firstLine(s string) string {
	if ls := lines(s); len(ls) > 0 {
		return ls[0]
	}
	return ""
}

func summaryStatuses(doc map[string]any) string {
	m := map[string]int{}
	ss, _ := doc["summary"].([]any)
	for _, s := range ss {
		if sm, ok := s.(map[string]any); ok {
			m[fmt.Sprintf("%v:%v", sm["id"], sm["status"])]++
		}
	}
	return fmtCounts(m)
}

// S13.24 Given no cache and a source that cannot be reached, when `brl-stocks
// print` is run, then the last line of the output reads `B3 unknown · US
// unknown`. Two variants: no connection, status 503.
func TestVerifyS13_24_NoCacheUnreachableMarketsUnknown(t *testing.T) {
	t.Parallel()
	for _, v := range unreachableVariants {
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, "S13.24 ["+v+"]", baseConfig)
			if v == "status 503" {
				e.setSource(state{Status: 503}, "every request answered 503")
			} else {
				e.logf("\nSTAND-IN   stopped: its address now refuses connections\n")
				e.src.stop()
			}
			r := e.run("print")
			e.wantExit(r, 0)
			if got := lastLine(r.stdout); got != "B3 unknown · US unknown" {
				e.failf("want the last line `B3 unknown · US unknown`, got %q", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Not-found rows in `print` (6.5 row 1, 6.2, 7.3, 7.6)
// ---------------------------------------------------------------------------

// 6.5 "Not found, tried less than an hour ago, config unchanged | No fetch;
// prints not found" (before the forced-fetch row); 7.3 "Not found: not found
// in place of the price; — in the other data columns", "Not-found rows do not
// raise" the error line; 7.6 a not-found row makes the state `partial`; 6.2
// "Not found | ... or the config changes | ... The ticker is tried again".
// The stand-in answers 404 for XXXX9.SA (B3 tickers are requested with .SA, as
// every other check's record shows).
func TestVerifyW6_5_NotFoundRow(t *testing.T) {
	t.Parallel()
	cfg := "refresh_seconds = 10\ntickers = [\"PETR4\", \"XXXX9\", \"AAPL\"]\n"
	e := newEnv(t, "6.5 row 1, 7.3 Not found, 6.2 Not found rows", cfg)
	e.setSource(state{SymbolStatus: map[string]int{"XXXX9.SA": 404}}, "XXXX9.SA answered 404")

	r1 := e.run("print")
	e.wantExit(r1, 0)
	if bySymbol(r1.reqs)["XXXX9.SA"] == 0 {
		t.Fatalf("set-up: the tool did not request XXXX9.SA (requested: %s)", fmtCounts(bySymbol(r1.reqs)))
	}
	nf := func(r result, when string) {
		e.t.Helper()
		l := row(e, r.stdout, "XXXX9")
		if !strings.Contains(l, "not found") || hasQuote(l) {
			e.failf("%s: want `not found` in place of the price, got %q", when, l)
		}
		dashes := 0
		for _, f := range strings.Fields(l) {
			if f == "—" {
				dashes++
			}
		}
		if dashes != 6 {
			e.failf("%s: want `—` in the 6 other data columns (name, change, percent, low, high, time), got %d in %q", when, dashes, l)
		}
		for _, tk := range []string{"PETR4", "AAPL"} {
			if l := row(e, r.stdout, tk); !hasQuote(l) {
				e.failf("%s: other rows are unaffected: row %s shows no quote: %q", when, tk, l)
			}
		}
		if n := linesWithPrefix(r.stderr, "source "); len(n) != 0 {
			e.failf("%s: not-found rows do not raise the error line; error stream: %q", when, r.stderr)
		}
	}
	nf(r1, "first print")

	rj := e.run("print", "--json")
	e.wantExit(rj, 0)
	if doc := parseDoc(e, rj.stdout); doc != nil {
		rw := docRows(doc)["XXXX9"]
		if rw == nil || rw["status"] != "not_found" || rw["price"] != nil {
			e.failf("JSON: want XXXX9 with status not_found and price null, got %v", rw)
		}
		if doc["state"] != "partial" {
			e.failf("7.6: a not-found row makes the state `partial`, got %v", doc["state"])
		}
	}

	e.sleep(pastInterval, "the other rows' cache becomes older than the interval")
	r2 := e.run("print")
	e.wantExit(r2, 0)
	e.wantNotRequested(r2, []string{"XXXX9.SA"}, "6.5 row 1: not found less than an hour ago")
	e.wantRequested(r2, []string{"PETR4.SA", "AAPL"}, "control: the other rows are older than the interval")
	nf(r2, "print after one interval")

	e.sleep(6*time.Second, "past the 5 seconds of 6.5 row 3")
	r3 := e.run("print", "--refresh")
	e.wantExit(r3, 0)
	e.wantNotRequested(r3, []string{"XXXX9.SA"}, "6.5 row 1 comes before the forced fetch: `print --refresh`")
	nf(r3, "print --refresh")

	cfg2 := strings.Replace(cfg, `"AAPL"]`, `"AAPL", "MSFT"]`, 1)
	e.logf("\nCONFIG CHANGED (MSFT added)\n%s\n", indent(cfg2))
	if err := os.WriteFile(e.cfgPath, []byte(cfg2), 0o644); err != nil {
		t.Fatal(err)
	}
	r4 := e.run("print")
	e.wantExit(r4, 0)
	if bySymbol(r4.reqs)["XXXX9.SA"] != 1 {
		e.failf("6.2 / 6.5: after the config changed, want XXXX9 tried again (1 request), got %d", bySymbol(r4.reqs)["XXXX9.SA"])
	}
}

// ---------------------------------------------------------------------------
// S3 Two markets, plain tickers, own currency
// ---------------------------------------------------------------------------

// S3.1 PETR4 is the B3 instrument, priced in R$. S3.2 PETR4F, MXRF11 and
// AAPL34 are B3. S3.3 AAPL, MSFT and F are US, priced in US$. S3.4 US:ABCD1 is
// US. S3.5 B3:XYZ is B3. Observed: the JSON `market` and `currency` of each row,
// and the currency symbol in the table. 7.2: rows show the code without a
// market prefix.
func TestVerifyS3_01_05_MarketOfATicker(t *testing.T) {
	t.Parallel()
	cfg := `tickers = ["PETR4", "PETR4F", "MXRF11", "AAPL34", "AAPL", "MSFT", "F", "US:ABCD1", "B3:XYZ"]` + "\n"
	e := newEnv(t, "S3.1-S3.5", cfg)
	type want struct{ crit, shown, market, currency, sign string }
	ws := []want{
		{"S3.1", "PETR4", "B3", "BRL", "R$"},
		{"S3.2", "PETR4F", "B3", "BRL", "R$"},
		{"S3.2", "MXRF11", "B3", "BRL", "R$"},
		{"S3.2", "AAPL34", "B3", "BRL", "R$"},
		{"S3.3", "AAPL", "US", "USD", "US$"},
		{"S3.3", "MSFT", "US", "USD", "US$"},
		{"S3.3", "F", "US", "USD", "US$"},
		{"S3.4", "ABCD1", "US", "USD", "US$"},
		{"S3.5", "XYZ", "B3", "BRL", "R$"},
	}
	rj := e.run("print", "--json")
	e.wantExit(rj, 0)
	rows := map[string]map[string]any{}
	if doc := parseDoc(e, rj.stdout); doc != nil {
		rows = docRows(doc)
	}
	r := e.run("print")
	e.wantExit(r, 0)
	for _, w := range ws {
		rw := rows[w.shown]
		if rw == nil {
			e.failf("%s: JSON: no row with ticker %q (tickers: %v)", w.crit, w.shown, keys(rows))
		} else if rw["market"] != w.market || rw["currency"] != w.currency {
			e.failf("%s: %s: want market %s, currency %s; got %v, %v", w.crit, w.shown, w.market, w.currency, rw["market"], rw["currency"])
		}
		ls := rowLines(r.stdout, w.shown)
		if len(ls) != 1 {
			e.failf("%s / 7.2: want one table row shown as %q (without a market prefix), found %d", w.crit, w.shown, len(ls))
			continue
		}
		if !strings.Contains(ls[0], w.sign+" ") {
			e.failf("%s: table row %s: want a price in %s, got %q", w.crit, w.shown, w.sign, ls[0])
		}
	}
	e.logf("\nSYMBOLS REQUESTED: %s\n", fmtCounts(bySymbol(e.src.requests())))
}

func keys(m map[string]map[string]any) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

// S3.7 Given the ticker written as `petr4`, when the row is drawn, then it
// shows `PETR4`. (In `print`, table and JSON.)
func TestVerifyS3_07_LowerCaseTickerShownUpper(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S3.7", "tickers = [\"petr4\"]\n")
	r := e.run("print")
	e.wantExit(r, 0)
	if l := row(e, r.stdout, "PETR4"); !hasQuote(l) {
		e.failf("want a row `PETR4` with a quote, got %q", l)
	}
	if n := len(rowLines(r.stdout, "petr4")); n != 0 {
		e.failf("the row is drawn as `petr4`")
	}
	rj := e.run("print", "--json")
	if doc := parseDoc(e, rj.stdout); doc != nil {
		if _, ok := docRows(doc)["PETR4"]; !ok {
			e.failf("JSON: no row with ticker PETR4 (tickers: %v)", keys(docRows(doc)))
		}
	}
}

// S4.4 Given the same ticker in two groups, when a refresh runs, then the
// ticker is requested from the source once. 7.2: shown in both groups.
// Both an ordinary refresh (empty cache) and a forced one.
func TestVerifyS4_04_TickerInTwoGroupsFetchedOnce(t *testing.T) {
	t.Parallel()
	cfg := "[[group]]\nname = \"A\"\ntickers = [\"PETR4\", \"AAPL\"]\n\n[[group]]\nname = \"B\"\ntickers = [\"VALE3\", \"PETR4\"]\n"
	e := newEnv(t, "S4.4", cfg)
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.wantRequested(r1, []string{"PETR4.SA"}, "S4.4 first refresh")
	if ls := rowLines(r1.stdout, "PETR4"); len(ls) != 2 || !hasQuote(ls[0]) || !hasQuote(ls[1]) {
		e.failf("7.2: want PETR4 shown with a quote in both groups, got %q", ls)
	}
	e.sleep(6*time.Second, "past the 5 seconds of 6.5 row 3")
	r2 := e.run("print", "--refresh")
	e.wantExit(r2, 0)
	e.wantRequested(r2, []string{"PETR4.SA"}, "S4.4 forced refresh")
}

// ---------------------------------------------------------------------------
// S11 The watchlist is a file I edit
// ---------------------------------------------------------------------------

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// wantWatchlist checks that exactly the given tickers have rows with quotes,
// among the tickers named in any config of the check.
func wantWatchlist(e *env, out string, shown, notShown []string, what string) {
	e.t.Helper()
	for _, tk := range shown {
		if ls := rowLines(out, tk); len(ls) != 1 || !hasQuote(ls[0]) {
			e.failf("%s: want a row %s with a quote, got %q", what, tk, ls)
		}
	}
	for _, tk := range notShown {
		if ls := rowLines(out, tk); len(ls) != 0 {
			e.failf("%s: want no row %s (it belongs to another watchlist), got %q", what, tk, ls)
		}
	}
}

// S11.1 Given a valid file at `~/.config/brl-stocks/config.toml` and no
// `--config` flag, when the tool starts, then it shows that file's watchlist.
// XDG_CONFIG_HOME unset; HOME temporary.
func TestVerifyS11_01_DefaultPathUnderHome(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S11.1 (XDG_CONFIG_HOME unset)", "")
	e.noCfgHome = true
	e.cfgPath = filepath.Join(e.home, ".config", "brl-stocks", "config.toml")
	writeFile(t, e.cfgPath, "tickers = [\"ITUB4\", \"MSFT\"]\n")
	r := e.run("print")
	e.wantExit(r, 0)
	wantWatchlist(e, r.stdout, []string{"ITUB4", "MSFT"}, []string{"PETR4", "VALE3", "AAPL"}, "S11.1")
}

// S11.2 Given `--config <other path>`, when the tool starts, then it shows the
// watchlist of that file (and not the one at the default path). Both places of
// the flag: before and after `print` (7.1: "On any command").
func TestVerifyS11_02_ConfigFlag(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S11.2", baseConfig)
	other := filepath.Join(e.root, "other.toml")
	writeFile(t, other, "tickers = [\"ITUB4\"]\n")
	sum := fileSum(other)
	for _, args := range [][]string{{"--config", other, "print"}, {"print", "--config", other}} {
		r := e.run(args...)
		e.wantExit(r, 0)
		wantWatchlist(e, r.stdout, []string{"ITUB4"}, []string{"PETR4", "AAPL", "VALE3", "MSFT"}, "S11.2 `"+strings.Join(args, " ")+"`")
	}
	if fileSum(other) != sum {
		e.failf("S11.8: the file named by --config changed")
	}
}

// S11.15 Given XDG_CONFIG_HOME and a valid file under it, when the tool starts
// without `--config`, then it shows that file's watchlist (and not the one at
// `~/.config`).
func TestVerifyS11_15_XDGConfigHome(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S11.15", "tickers = [\"ITUB4\"]\n")
	writeFile(t, filepath.Join(e.home, ".config", "brl-stocks", "config.toml"), "tickers = [\"MSFT\"]\n")
	r := e.run("print")
	e.wantExit(r, 0)
	wantWatchlist(e, r.stdout, []string{"ITUB4"}, []string{"MSFT", "PETR4", "VALE3", "AAPL"}, "S11.15")
}

// S11.9 Given a config file that cannot be parsed, when the tool starts, then
// it exits with status 2. S11.16 ... then a message on the error stream names
// the file and the problem. 7.2 adds the other kinds of invalid config (wrong
// type, unknown summary name, empty ticker), and 6.4: exit status 2. Also with
// the file named by --config. Nothing is requested.
func TestVerifyS11_09_16_InvalidConfig(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, cfg string }{
		{"cannot be parsed", "tickers = [\"PETR4\"\nthis is not toml ===\n"},
		{"wrong type", "refresh_seconds = \"ten\"\ntickers = [\"PETR4\"]\n"},
		{"unknown summary name", "summary = [\"IBOV\", \"DAX\"]\ntickers = [\"PETR4\"]\n"},
		{"empty ticker", "tickers = [\"PETR4\", \"\"]\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, "S11.9, S11.16, 7.2 ["+c.name+"]", c.cfg)
			other := filepath.Join(e.root, "named.toml")
			writeFile(t, other, c.cfg)
			for _, v := range []struct {
				path string
				args []string
			}{{e.cfgPath, []string{"print"}}, {other, []string{"--config", other, "print"}}} {
				r := e.run(v.args...)
				e.wantExit(r, 2)
				e.wantNoRequests(r, "invalid config")
				if !strings.Contains(r.stderr, v.path) {
					e.failf("S11.16: want the error stream to name the file %s, got %q", v.path, r.stderr)
				}
				rest := strings.TrimSpace(strings.ReplaceAll(r.stderr, v.path, ""))
				if len(rest) < 10 {
					e.failf("S11.16: want the problem named beside the file, the error stream holds only %q", r.stderr)
				}
				e.logf("MESSAGE    %q\n", r.stderr)
			}
		})
	}
}

// S11.12 Given a config with a key the tool does not know, when the tool
// starts, then a warning names the key. S11.17 ... then the watchlist is
// shown. 7.2: warnings go to the error stream in one-shot.
func TestVerifyS11_12_17_UnknownKey(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S11.12, S11.17", "colour_scheme = \"dark\"\n"+baseConfig)
	r := e.run("print")
	e.wantExit(r, 0)
	wantWatchlist(e, r.stdout, tickers, nil, "S11.17")
	if !strings.Contains(r.stderr, "colour_scheme") {
		e.failf("S11.12: want a warning naming `colour_scheme` on the error stream, got %q", r.stderr)
	}
	if strings.Contains(r.stdout, "colour_scheme") {
		e.failf("7.4: the warning is on standard output; it belongs on the error stream")
	}
}

// ---------------------------------------------------------------------------
// S12 No config yet
// ---------------------------------------------------------------------------

// S12.1 Given no file at the default config path, when the tool starts, then
// it shows a built-in example watchlist (7.7: PETR4, VALE3, ITUB4, AAPL, MSFT,
// with the default summary line). S12.2 ... it names the path where a config
// file belongs (on the error stream in one-shot). S12.3 ... when the tool has
// run in any mode and exited, no file or folder has been created under the
// config path. Two variants: XDG_CONFIG_HOME set, and unset (~/.config).
// Modes: print, print --json, print --refresh.
func TestVerifyS12_01_03_NoConfig(t *testing.T) {
	t.Parallel()
	example := []string{"PETR4", "VALE3", "ITUB4", "AAPL", "MSFT"}
	for _, v := range []string{"XDG_CONFIG_HOME set", "XDG_CONFIG_HOME unset"} {
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, "S12.1-S12.3 ["+v+"]", "")
			base := e.cfgHome
			if v == "XDG_CONFIG_HOME unset" {
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
			r := e.run("print")
			e.wantExit(r, 0)
			wantWatchlist(e, r.stdout, example, nil, "S12.1")
			if !strings.HasPrefix(firstLine(r.stdout), "IBOV") {
				e.failf("S12.1 / 7.7: want the default summary line first, got %q", firstLine(r.stdout))
			}
			if !strings.Contains(r.stderr, e.cfgPath) {
				e.failf("S12.2: want the error stream to name %s, got %q", e.cfgPath, r.stderr)
			}
			rj := e.run("print", "--json")
			e.wantExit(rj, 0)
			if doc := parseDoc(e, rj.stdout); doc != nil {
				if got := keys(docRows(doc)); strings.Join(got, " ") != "AAPL ITUB4 MSFT PETR4 VALE3" {
					e.failf("S12.1 (JSON): want the rows %v, got %v", example, got)
				}
			}
			e.sleep(6*time.Second, "past the 5 seconds of 6.5 row 3")
			rr := e.run("print", "--refresh")
			e.wantExit(rr, 0)
			if after := list(); after != before {
				e.failf("S12.3: something was created under %s:\n--- before\n%s--- after\n%s", base, before, after)
			}
		})
	}
}

// S12.4 Given `--config <missing file>`, when the tool starts, then it exits
// with status 2. S12.5 ... then a message on the error stream names the path.
// Also 7.6: with --json, standard output carries a document with state
// `error`. Nothing is requested; the missing file is not created.
func TestVerifyS12_04_05_ConfigFlagMissingFile(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S12.4, S12.5", baseConfig)
	missing := filepath.Join(e.root, "missing.toml")
	for _, args := range [][]string{{"--config", missing, "print"}, {"--config", missing, "print", "--json"}} {
		r := e.run(args...)
		e.wantExit(r, 2)
		e.wantNoRequests(r, "missing config")
		if !strings.Contains(r.stderr, missing) {
			e.failf("S12.5: want the error stream to name %s, got %q", missing, r.stderr)
		}
		if len(args) == 4 {
			if doc := parseDoc(e, r.stdout); doc != nil && doc["state"] != "error" {
				e.failf("7.6: want state \"error\", got %v", doc["state"])
			}
		}
	}
	if _, err := os.Stat(missing); err == nil {
		e.failf("the missing config file was created")
	}
}

// ---------------------------------------------------------------------------
// S17 The data source can be replaced
// ---------------------------------------------------------------------------

// S17.3 Given the first version, when its config file and flags are listed,
// then none of them selects a data source. Observed: the help text names no
// such flag (nor the testing variable, 7.1); `--source` is an unknown flag
// (exit 2); a `source` key in the config is an unknown key (a warning names
// it) and the requests still go to the stand-in.
func TestVerifyS17_03_NothingSelectsASource(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S17.3", "source = \"http://127.0.0.1:1\"\n"+baseConfig)
	for _, args := range [][]string{{"--help"}, {"print", "--help"}} {
		r := e.run(args...)
		e.wantExit(r, 0)
		help := strings.ToLower(r.stdout + r.stderr)
		for _, w := range []string{"source", "url", "provider", "yahoo", "brl_stocks_source_url"} {
			if strings.Contains(help, w) {
				e.failf("`%s`: the help text mentions %q: %q", strings.Join(args, " "), w, r.stdout+r.stderr)
			}
		}
	}
	r := e.run("print", "--source", "http://127.0.0.1:1")
	e.wantExit(r, 2)
	r2 := e.run("print")
	e.wantExit(r2, 0)
	if !strings.Contains(r2.stderr, "source") {
		e.failf("a `source` key: want a warning naming it as unknown, got %q", r2.stderr)
	}
	if len(r2.reqs) == 0 {
		e.failf("with a `source` key, the requests no longer reach the stand-in")
	}
	wantWatchlist(e, r2.stdout, tickers, nil, "S17.3")
}
