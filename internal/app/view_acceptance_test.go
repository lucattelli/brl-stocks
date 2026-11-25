package app_test

// Acceptance tests of the full-screen view (`brl-stocks` with no command),
// increment 1 of slice 2: it opens, draws the rows, quits. Written from
// docs/spec.md and the view's test boundary (docs/design.md) before the view
// exists. They drive the tool only through app.Run with a deps.Terminal, and
// reuse the stand-in source, the timer, the world and the readers of
// print_acceptance_test.go and print_shared_acceptance_test.go.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucattelli/brl-stocks/internal/app"
	"github.com/lucattelli/brl-stocks/internal/deps"
	"github.com/lucattelli/brl-stocks/internal/market"
	"github.com/lucattelli/brl-stocks/internal/provider"
)

// ---------------------------------------------------------------------------
// The view's stand-in source: the stand-in of the print tests, which can
// also hold its answers, and which knows how many requests are in flight.
// ---------------------------------------------------------------------------

type viewSource struct {
	*fakeSource
	mu       sync.Mutex
	held     bool
	release  chan struct{}
	inflight int
}

func newViewSource(src *fakeSource) *viewSource {
	return &viewSource{fakeSource: src, release: make(chan struct{})}
}

func (s *viewSource) Quote(ctx context.Context, in market.Instrument) (market.Quote, error) {
	s.mu.Lock()
	s.inflight++
	held, release := s.held, s.release
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inflight--
		s.mu.Unlock()
	}()
	if held {
		select {
		case <-release:
		case <-ctx.Done():
			return market.Quote{}, &provider.Error{Kind: provider.Unavailable, Msg: "stand-in: held, no answer"}
		}
	}
	return s.fakeSource.Quote(ctx, in)
}

// hold makes every request wait, unanswered, until releaseAll.
func (s *viewSource) hold() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held = true
}

// releaseAll lets the held requests, and every later one, through.
func (s *viewSource) releaseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held {
		s.held = false
		close(s.release)
	}
}

func (s *viewSource) inFlight() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inflight
}

// ---------------------------------------------------------------------------
// Running the view
// ---------------------------------------------------------------------------

const (
	viewWidth  = 160 // wide enough for the full row: "at full width"
	viewHeight = 40  // tall enough for 20 rows and the lines around them
	viewGuard  = 10 * time.Second
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// drawn is a frame as the view drew it.
type drawn struct {
	f deps.Frame
}

// text is the frame with its SGR codes taken out, one line per screen line.
func (d drawn) text() string {
	ls := make([]string, len(d.f.Lines))
	for i, l := range d.f.Lines {
		ls[i] = plain(l)
	}
	return strings.Join(ls, "\n")
}

// raw is the frame with its SGR codes.
func (d drawn) raw() string { return strings.Join(d.f.Lines, "\n") }

type viewRun struct {
	t      *testing.T
	src    *viewSource
	in     chan deps.Input
	sent   int
	stdout *lockedBuffer
	stderr *lockedBuffer
	cancel context.CancelFunc

	mu     sync.Mutex
	frames []drawn
	done   chan struct{}
	code   int
}

type viewOpts struct {
	args []string          // the command line; nil: none
	env  map[string]string // added to the world's environment
	hold bool              // the source holds its answers from the start
}

// guardConfig checks, when the test ends, that the file at path is byte for
// byte what it was now (QR7).
func guardConfig(t *testing.T, path string) {
	t.Helper()
	before, err := os.ReadFile(path)
	if err != nil {
		return
	}
	t.Cleanup(func() {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Errorf("QR7: the config file %s changed during the test (err: %v)", path, err)
		}
	})
}

// openView runs `brl-stocks` with the given command line on a Terminal in a
// goroutine, with the world's clock (frozen at its current time), timer,
// environment and source, and collects every frame. The view is closed when
// the test ends.
func openView(t *testing.T, w *world, o viewOpts) *viewRun {
	t.Helper()
	guardConfig(t, w.configPath)
	for i, a := range o.args {
		if a == "--config" && i+1 < len(o.args) {
			guardConfig(t, o.args[i+1])
		}
	}
	src := newViewSource(w.src)
	if o.hold {
		src.hold()
	}
	env := map[string]string{}
	for k, v := range w.env {
		env[k] = v
	}
	for k, v := range o.env {
		env[k] = v
	}
	now := w.now
	ctx, cancel := context.WithCancel(context.Background())
	r := &viewRun{
		t: t, src: src,
		in:     make(chan deps.Input, 64),
		stdout: &lockedBuffer{}, stderr: &lockedBuffer{},
		cancel: cancel,
		done:   make(chan struct{}),
	}
	args := o.args
	if args == nil {
		args = []string{}
	}
	d := deps.Deps{
		Provider: src,
		Now:      func() time.Time { return now },
		After:    w.timer.After,
		LookupEnv: func(k string) (string, bool) {
			v, ok := env[k]
			return v, ok
		},
		Stdout: r.stdout,
		Stderr: r.stderr,
		IsTTY:  true,
		Terminal: &deps.Terminal{
			Width: viewWidth, Height: viewHeight,
			Input: r.in,
			Draw: func(f deps.Frame) {
				f.Lines = append([]string(nil), f.Lines...)
				r.mu.Lock()
				r.frames = append(r.frames, drawn{f: f})
				r.mu.Unlock()
			},
		},
	}
	go func() {
		defer close(r.done)
		code := app.Run(ctx, args, d)
		r.mu.Lock()
		r.code = code
		r.mu.Unlock()
	}()
	t.Cleanup(func() {
		src.releaseAll()
		select {
		case <-r.done:
			return
		default:
		}
		select {
		case r.in <- deps.Input{Key: "q"}:
		default:
		}
		select {
		case <-r.done:
			return
		case <-time.After(2 * time.Second):
		}
		cancel()
		select {
		case <-r.done:
		case <-time.After(5 * time.Second):
			t.Logf("the view did not end after q and a cancelled context")
		}
	})
	return r
}

func (r *viewRun) exited() (bool, int) {
	select {
	case <-r.done:
		r.mu.Lock()
		defer r.mu.Unlock()
		return true, r.code
	default:
		return false, 0
	}
}

// latest waits until the most recent frame satisfies ok, and returns it. It fails the
// test when the view ends first, or after a generous real-time guard.
func (r *viewRun) latest(desc string, ok func(drawn) bool) drawn {
	r.t.Helper()
	deadline := time.Now().Add(viewGuard)
	for {
		r.mu.Lock()
		var last drawn
		n := len(r.frames)
		if n > 0 {
			last = r.frames[n-1]
		}
		r.mu.Unlock()
		if n > 0 && ok(last) {
			return last
		}
		if done, code := r.exited(); done {
			r.mu.Lock()
			n := len(r.frames)
			r.mu.Unlock()
			if n > 0 && ok(last) {
				return last
			}
			r.t.Fatalf("the view ended with status %d after %d frames, before %s\nstdout: %q\nstderr: %q",
				code, n, desc, r.stdout.String(), r.stderr.String())
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("no %s within %v; %d frames drawn; last frame:\n%s", desc, viewGuard, n, last.text())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// settled is the idle frame after the inputs sent so far: nothing runs but
// waits on the timer, so the fetch at start has been answered or timed out.
func (r *viewRun) settled() drawn {
	r.t.Helper()
	sent := r.sent
	return r.latest(fmt.Sprintf("idle frame after %d inputs", sent), func(d drawn) bool {
		return d.f.Idle && d.f.Inputs == sent
	})
}

// beforeAnswer is, with a source that holds its answers, a frame drawn
// before any answer: once a request waits at the source, the latest frame
// after the inputs sent so far that satisfies ok. Such a frame is never
// idle (a request is in flight), so the given needs the in-flight count.
func (r *viewRun) beforeAnswer(desc string, ok func(drawn) bool) drawn {
	r.t.Helper()
	deadline := time.Now().Add(viewGuard)
	for r.src.inFlight() == 0 {
		if done, code := r.exited(); done {
			r.t.Fatalf("the view ended with status %d before it sent a request\nstdout: %q\nstderr: %q",
				code, r.stdout.String(), r.stderr.String())
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("given not established: no request reached the held source within %v", viewGuard)
		}
		time.Sleep(2 * time.Millisecond)
	}
	sent := r.sent
	return r.latest(fmt.Sprintf("frame before any answer (%s) after %d inputs", desc, sent), func(d drawn) bool {
		return d.f.Inputs == sent && ok(d)
	})
}

// showsRows reports whether the frame has a line for each ticker.
func showsRows(tickers ...string) func(drawn) bool {
	return func(d drawn) bool {
		for _, tk := range tickers {
			if !hasRow(d.text(), tk) {
				return false
			}
		}
		return true
	}
}

func (r *viewRun) send(key string) {
	r.t.Helper()
	select {
	case r.in <- deps.Input{Key: key}:
		r.sent++
	case <-time.After(viewGuard):
		r.t.Fatalf("the view takes no input %q", key)
	}
}

// quit sends key and returns the exit status Run gives.
func (r *viewRun) quit(key string) int {
	r.t.Helper()
	r.send(key)
	select {
	case <-r.done:
		_, code := r.exited()
		return code
	case <-time.After(viewGuard):
		r.t.Fatalf("the view did not exit within %v of %q", viewGuard, key)
		return -1
	}
}

// allFrames returns every frame drawn so far.
func (r *viewRun) allFrames() []drawn {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]drawn(nil), r.frames...)
}

// runFlags runs the command line once without a Terminal, with a real-time
// guard: a flag that prints and exits (S17.4, S18).
func runFlags(t *testing.T, w *world, args ...string) result {
	t.Helper()
	guardConfig(t, w.configPath)
	ch := make(chan result, 1)
	go func() { ch <- w.run(args...) }()
	select {
	case r := <-ch:
		return r
	case <-time.After(viewGuard):
		t.Fatalf("brl-stocks %s did not return within %v", strings.Join(args, " "), viewGuard)
		return result{}
	}
}

// wantTime is the quote time as 7.3 shows it: local HH:MM, or DD/MM when
// the quote is not from today (both in the machine's local time).
func wantTime(at, now time.Time) string {
	a, n := at.In(time.Local), now.In(time.Local)
	if a.Year() == n.Year() && a.YearDay() == n.YearDay() {
		return a.Format("15:04")
	}
	return a.Format("02/01")
}

func assertRowHas(t *testing.T, frame, ticker string, texts ...string) {
	t.Helper()
	line := rowLine(t, frame, ticker)
	for _, s := range texts {
		if !strings.Contains(line, s) {
			t.Errorf("row %s does not show %q: %q", ticker, s, line)
		}
	}
}

// ---------------------------------------------------------------------------
// A 20-ticker watchlist (QR1)
// ---------------------------------------------------------------------------

var (
	twentyB3 = []string{"PETR4", "VALE3", "ITUB4", "BBDC4", "ABEV3", "WEGE3", "BBAS3", "BBSE3", "RENT3", "SUZB3"}
	twentyUS = []string{"AAPL", "MSFT", "GOOG", "AMZN", "NVDA", "META", "TSLA", "JPM", "KO", "F"}
)

func twentyConfig() string {
	all := append(append([]string{}, twentyB3...), twentyUS...)
	return `tickers = ["` + strings.Join(all, `", "`) + `"]` + "\n"
}

// loadTwenty loads quotes for the 20 tickers and the summary, and returns
// the price each row shows: the rows of loadQuotes keep theirs, the others
// get a price whose format is plain to read.
func loadTwenty(src *fakeSource, now time.Time) map[string]string {
	loadQuotes(src, now, true)
	want := map[string]string{"PETR4": "R$ 38,42", "VALE3": "R$ 61,07", "ITUB4": "R$ 34,15", "AAPL": "US$ 229.87", "MSFT": "US$ 512.30"}
	for i, s := range twentyB3 {
		if _, ok := want[s]; ok {
			continue
		}
		p := float64(20+i) + 0.25
		src.answer(market.Instrument{Market: market.B3, Symbol: s}, quote(s+" SA", "BRL", p, p, b3Session, now.Add(-15*time.Minute)))
		want[s] = fmt.Sprintf("R$ %d,25", 20+i)
	}
	for i, s := range twentyUS {
		if _, ok := want[s]; ok {
			continue
		}
		p := float64(40+i) + 0.25
		src.answer(market.Instrument{Market: market.US, Symbol: s}, quote(s+" Inc", "USD", p, p, usSession, now.Add(-time.Second)))
		want[s] = fmt.Sprintf("US$ %d.25", 40+i)
	}
	return want
}

// sizes runs f once with the criterion's own watchlist and once with 20
// tickers (QR1).
type watchlistCase struct {
	name   string
	config string
	load   func(src *fakeSource, now time.Time) map[string]string // price shown per row
}

func withTwenty(config string, rows map[string]string) []watchlistCase {
	return []watchlistCase{
		{"criterion", config, func(src *fakeSource, now time.Time) map[string]string {
			loadQuotes(src, now, true)
			return rows
		}},
		{"20 tickers", twentyConfig(), loadTwenty},
	}
}

// ---------------------------------------------------------------------------
// S1 - Watch the list in a full-screen view
// ---------------------------------------------------------------------------

// S1.1 Given a config with the tickers `PETR4` and `AAPL` and a source that
// answers, when `brl-stocks` is run with no command, then a full-screen view
// shows one row for each ticker. (D3, D6)
func TestS1_1_View_OneRowForEachTicker(t *testing.T) {
	for _, c := range withTwenty(`tickers = ["PETR4", "AAPL"]`+"\n", map[string]string{"PETR4": "", "AAPL": ""}) {
		t.Run(c.name, func(t *testing.T) {
			// Given
			w := newWorld(t, bothOpen, c.config)
			rows := c.load(w.src, w.now)

			// When
			v := openView(t, w, viewOpts{})
			f := v.settled().text()

			// Then: exactly one line per ticker
			for tk := range rows {
				rowIndex(t, f, tk)
			}
		})
	}
}

// S1.2 Given a row whose fetch succeeded, when the view is drawn at full
// width, then the row shows ticker, name, last price, day change as a value,
// day change as a percent, day low, day high and quote time. (D9)
func TestS1_2_View_RowShowsEveryColumn(t *testing.T) {
	for _, c := range withTwenty(plainConfig, nil) {
		t.Run(c.name, func(t *testing.T) {
			// Given: PETR4 38.42 after 38.11, low 37.7289, high 38.8042;
			// AAPL 229.87 after 230.99, low 227.5713, high 233.2999
			w := newWorld(t, bothOpen, c.config)
			c.load(w.src, w.now)

			// When
			f := openView(t, w, viewOpts{}).settled().text()

			// Then
			assertRowHas(t, f, "PETR4", "Petrobras PN", "R$ 38,42", "+0,31", "+0,81%", "37,73", "38,80",
				wantTime(w.now.Add(-15*time.Minute), w.now))
			assertRowHas(t, f, "AAPL", "Apple Inc.", "US$ 229.87", "-1.12", "-0.48%", "227.57", "233.30",
				wantTime(w.now.Add(-time.Second), w.now))
		})
	}
}

// S1.3 Given the view is open, when `q` is pressed, then the tool exits with
// status 0. (D32, D50)
func TestS1_3_View_QExitsWithStatus0(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	v := openView(t, w, viewOpts{})
	rowLine(t, v.settled().text(), "PETR4") // the view is open

	// When
	code := v.quit("q")

	// Then
	if code != 0 {
		t.Errorf("exit status after q: %d, want 0\nstderr: %q", code, v.stderr.String())
	}
}

// S1.4 Given the cache holds quotes for the watchlist, when the view starts,
// then the rows show the cached quotes before any answer from the source has
// arrived. (D20)
func TestS1_4_View_CachedQuotesBeforeAnyAnswer(t *testing.T) {
	for _, c := range withTwenty(plainConfig, map[string]string{"PETR4": "R$ 38,42", "VALE3": "R$ 61,07", "AAPL": "US$ 229.87"}) {
		t.Run(c.name, func(t *testing.T) {
			// Given: a cache left by an earlier run, 90 seconds old (due for
			// a fetch, not yet stale); the source now has other prices and
			// holds its answers
			w := newWorld(t, bothOpen, c.config)
			rows := c.load(w.src, w.now)
			w.runOK("print")
			if w.src.total() == 0 {
				t.Fatal("given not established: the earlier print, with no cache, sent no request")
			}
			w.advance(90 * time.Second)
			w.src.setPrice(petr4, 40.01)
			w.src.setPrice(aapl, 233.03)

			// When
			v := openView(t, w, viewOpts{hold: true})
			f := v.beforeAnswer("the cached quotes", func(d drawn) bool {
				for tk, price := range rows {
					if !hasRow(d.text(), tk) || !strings.Contains(rowLine(t, d.text(), tk), price) {
						return false
					}
				}
				return true
			}).text()

			// Then
			for tk, price := range rows {
				assertRowHas(t, f, tk, price)
			}
		})
	}
}

// S1.5 Given no cache, when the view starts, then each row shows its ticker
// and `—` in the data columns until its first quote arrives. (D50)
func TestS1_5_View_NoCacheShowsDashesUntilTheFirstQuote(t *testing.T) {
	for _, c := range withTwenty(plainConfig, map[string]string{"PETR4": "R$ 38,42", "VALE3": "R$ 61,07", "AAPL": "US$ 229.87"}) {
		t.Run(c.name, func(t *testing.T) {
			// Given: no cache, a source that holds its answers
			w := newWorld(t, bothOpen, c.config)
			rows := c.load(w.src, w.now)

			// When
			v := openView(t, w, viewOpts{hold: true})
			tickers := []string{}
			for tk := range rows {
				tickers = append(tickers, tk)
			}
			before := v.beforeAnswer("every row", showsRows(tickers...)).text()

			// Then: before the first quote, the ticker and `—` alone
			for tk := range rows {
				fields := strings.Fields(rowLine(t, before, tk))
				if len(fields) < 2 {
					t.Errorf("row %s shows no data columns: %q", tk, rowLine(t, before, tk))
					continue
				}
				for _, fl := range fields[1:] {
					if fl != "—" {
						t.Errorf("row %s shows %q before its first quote, want `—` in every data column: %q", tk, fl, rowLine(t, before, tk))
						break
					}
				}
			}

			// Then: once the quote arrives, the dashes give way to it
			v.src.releaseAll()
			after := v.settled().text()
			for tk, price := range rows {
				assertRowHas(t, after, tk, price)
			}
		})
	}
}

// S1.7 Given a quote from an earlier day, when the row is drawn, then the
// quote time is shown as a date (`DD/MM`). (D50, D75)
func TestS1_7_View_QuoteFromAnEarlierDayShowsADate(t *testing.T) {
	// Given: Monday 13:30 UTC; the source reports AAPL's Friday close
	w := newWorld(t, onlyB3Open, plainConfig)
	loadQuotes(w.src, w.now, false)
	friday := usFridaySession.End.In(time.Local).Format("02/01")

	// When
	f := openView(t, w, viewOpts{}).settled().text()

	// Then
	assertRowHas(t, f, "AAPL", friday)
}

// rowWith opens the view on one ticker whose quote is q and returns the
// row's line.
func rowWith(t *testing.T, ticker string, in market.Instrument, change func(q *market.Quote)) string {
	t.Helper()
	w := newWorld(t, bothOpen, `tickers = ["`+ticker+`"]`+"\n")
	loadQuotes(w.src, w.now, true)
	w.src.mu.Lock()
	q := w.src.quotes[in]
	change(&q)
	w.src.quotes[in] = q
	w.src.mu.Unlock()
	return rowLine(t, openView(t, w, viewOpts{}).settled().text(), ticker)
}

// S1.8 Given a B3 row whose last price is 1038.42, when the row is drawn,
// then the price reads `R$ 1.038,42`. (D46)
func TestS1_8_View_B3PriceWithThousands(t *testing.T) {
	line := rowWith(t, "PETR4", petr4, func(q *market.Quote) { q.Price = 1038.42 })
	if !strings.Contains(line, "R$ 1.038,42") {
		t.Errorf("row PETR4 does not read R$ 1.038,42: %q", line)
	}
}

// S1.9 Given a US row whose last price is 1229.87, when the row is drawn,
// then the price reads `US$ 1,229.87`. (D46)
func TestS1_9_View_USPriceWithThousands(t *testing.T) {
	line := rowWith(t, "AAPL", aapl, func(q *market.Quote) { q.Price = 1229.87 })
	if !strings.Contains(line, "US$ 1,229.87") {
		t.Errorf("row AAPL does not read US$ 1,229.87: %q", line)
	}
}

// S1.10 Given a B3 row whose day change is +0.31, when the row is drawn,
// then the change value reads `+0,31`. (D46, D50, D65)
func TestS1_10_View_B3ChangeValue(t *testing.T) {
	line := rowWith(t, "PETR4", petr4, func(q *market.Quote) { q.Price, q.PrevClose = 38.42, 38.11 })
	if !strings.Contains(line, "+0,31") {
		t.Errorf("row PETR4 does not read +0,31: %q", line)
	}
}

// S1.11 Given a B3 row whose day change is +0.81 percent, when the row is
// drawn, then the percent reads `+0,81%`. (D50, D64, D65)
func TestS1_11_View_B3ChangePercent(t *testing.T) {
	// 0.81 / 100 of the previous close of 100.00
	line := rowWith(t, "PETR4", petr4, func(q *market.Quote) { q.Price, q.PrevClose = 100.81, 100 })
	if !strings.Contains(line, "+0,81%") {
		t.Errorf("row PETR4 does not read +0,81%%: %q", line)
	}
}

// S1.12 Given a US row whose day change is -0.48 percent, when the row is
// drawn, then the percent reads `-0.48%`. (D64, D65, D66)
func TestS1_12_View_USChangePercent(t *testing.T) {
	line := rowWith(t, "AAPL", aapl, func(q *market.Quote) { q.Price, q.PrevClose = 99.52, 100 })
	if !strings.Contains(line, "-0.48%") {
		t.Errorf("row AAPL does not read -0.48%%: %q", line)
	}
}

// S1.13 Given a B3 row whose day change is exactly zero, when the row is
// drawn, then the change value reads `+0,00`. (D50, D66)
func TestS1_13_View_B3ZeroChange(t *testing.T) {
	line := rowWith(t, "PETR4", petr4, func(q *market.Quote) { q.Price, q.PrevClose = 38.42, 38.42 })
	if !strings.Contains(line, "+0,00") {
		t.Errorf("row PETR4 does not read +0,00: %q", line)
	}
}

// S1.14 Given a B3 row whose last price is 38.425, when the row is drawn,
// then the price reads `R$ 38,43`. (D46, D66)
func TestS1_14_View_B3PriceRoundedHalfAwayFromZero(t *testing.T) {
	line := rowWith(t, "PETR4", petr4, func(q *market.Quote) { q.Price = 38.425 })
	if !strings.Contains(line, "R$ 38,43") {
		t.Errorf("row PETR4 does not read R$ 38,43: %q", line)
	}
}

// S1.15 Given a B3 row whose day low is 38.02, when the row is drawn, then
// the low reads `38,02`, with no currency symbol. (D46, D66)
func TestS1_15_View_B3LowWithoutCurrencySymbol(t *testing.T) {
	line := rowWith(t, "PETR4", petr4, func(q *market.Quote) { q.Low = 38.02 })
	if !strings.Contains(line, "38,02") {
		t.Errorf("row PETR4 does not show the low 38,02: %q", line)
	}
	if strings.Contains(line, "R$ 38,02") || strings.Contains(line, "R$38,02") {
		t.Errorf("the low of row PETR4 carries a currency symbol: %q", line)
	}
}

// ---------------------------------------------------------------------------
// S3 - Two markets, plain tickers, own currency
// ---------------------------------------------------------------------------

// viewWith opens the view on config with the usual quotes and returns the
// settled frame's text.
func viewWith(t *testing.T, config string, extra func(w *world)) (*world, string) {
	t.Helper()
	w := newWorld(t, bothOpen, config)
	loadQuotes(w.src, w.now, true)
	if extra != nil {
		extra(w)
	}
	return w, openView(t, w, viewOpts{}).settled().text()
}

// S3.1 Given the ticker `PETR4` in the config, when its quote is shown, then
// it is the B3 instrument, priced in `R$`. (D30, D10)
func TestS3_1_View_PETR4IsB3InReais(t *testing.T) {
	w, f := viewWith(t, `tickers = ["PETR4"]`+"\n", nil)
	if w.src.count(petr4) == 0 {
		t.Errorf("the B3 instrument PETR4 was not requested")
	}
	assertRowHas(t, f, "PETR4", "R$ 38,42")
}

// S3.2 Given the tickers `PETR4F`, `MXRF11` and `AAPL34`, when the config is
// loaded, then all three are treated as B3. (D30)
func TestS3_2_View_B3ShapesAreB3(t *testing.T) {
	syms := []string{"PETR4F", "MXRF11", "AAPL34"}
	w, f := viewWith(t, `tickers = ["PETR4F", "MXRF11", "AAPL34"]`+"\n", func(w *world) {
		for _, s := range syms {
			w.src.answer(market.Instrument{Market: market.B3, Symbol: s}, quote(s, "BRL", 10, 10, b3Session, w.now.Add(-15*time.Minute)))
		}
	})
	for _, s := range syms {
		rowLine(t, f, s)
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
func TestS3_3_View_OtherShapesAreUSInDollars(t *testing.T) {
	ford := market.Instrument{Market: market.US, Symbol: "F"}
	w, f := viewWith(t, `tickers = ["AAPL", "MSFT", "F"]`+"\n", func(w *world) {
		w.src.answer(ford, quote("Ford Motor", "USD", 11.25, 11.10, usSession, w.now.Add(-time.Second)))
	})
	for in, price := range map[market.Instrument]string{aapl: "US$ 229.87", msft: "US$ 512.30", ford: "US$ 11.25"} {
		if w.src.count(in) == 0 {
			t.Errorf("%s was not requested as a US instrument", in.Symbol)
		}
		if n := w.src.count(market.Instrument{Market: market.B3, Symbol: in.Symbol}); n != 0 {
			t.Errorf("%s was requested %d times as a B3 instrument", in.Symbol, n)
		}
		assertRowHas(t, f, in.Symbol, price)
	}
}

// S3.4 Given the ticker `US:ABCD1`, when the config is loaded, then it is
// treated as US although its shape is that of B3. (D30)
func TestS3_4_View_USPrefixOverridesB3Shape(t *testing.T) {
	us := market.Instrument{Market: market.US, Symbol: "ABCD1"}
	w, f := viewWith(t, `tickers = ["US:ABCD1"]`+"\n", func(w *world) {
		w.src.answer(us, quote("Abcd Corp", "USD", 12.50, 12.00, usSession, w.now.Add(-time.Second)))
	})
	rowLine(t, f, "ABCD1")
	if w.src.count(us) == 0 {
		t.Errorf("ABCD1 was not requested as a US instrument")
	}
	if n := w.src.count(market.Instrument{Market: market.B3, Symbol: "ABCD1"}); n != 0 {
		t.Errorf("ABCD1 was requested %d times as a B3 instrument", n)
	}
}

// S3.5 Given the ticker `B3:XYZ`, when the config is loaded, then it is
// treated as B3 although its shape is not that of B3. (D30)
func TestS3_5_View_B3PrefixOverridesUSShape(t *testing.T) {
	b3 := market.Instrument{Market: market.B3, Symbol: "XYZ"}
	w, f := viewWith(t, `tickers = ["B3:XYZ"]`+"\n", func(w *world) {
		w.src.answer(b3, quote("Xyz SA", "BRL", 7.30, 7.20, b3Session, w.now.Add(-15*time.Minute)))
	})
	rowLine(t, f, "XYZ")
	if w.src.count(b3) == 0 {
		t.Errorf("XYZ was not requested as a B3 instrument")
	}
	if n := w.src.count(market.Instrument{Market: market.US, Symbol: "XYZ"}); n != 0 {
		t.Errorf("XYZ was requested %d times as a US instrument", n)
	}
}

// S3.6 Given a B3 row and a US row on screen, when quotes are shown, then no
// price has been converted to the other currency. (D10)
func TestS3_6_View_NoPriceIsConverted(t *testing.T) {
	// PETR4: 38.42, low 37.7289, high 38.8042, in R$.
	// AAPL: 229.87, low 227.5713, high 233.2999, in US$.
	_, f := viewWith(t, `tickers = ["PETR4", "AAPL"]`+"\n", nil)
	assertRowHas(t, f, "PETR4", "R$ 38,42", "37,73", "38,80")
	assertRowHas(t, f, "AAPL", "US$ 229.87", "227.57", "233.30")
	if strings.Contains(rowLine(t, f, "PETR4"), "US$") {
		t.Errorf("the B3 row shows US$: %q", rowLine(t, f, "PETR4"))
	}
	if strings.Contains(rowLine(t, f, "AAPL"), "R$") {
		t.Errorf("the US row shows R$: %q", rowLine(t, f, "AAPL"))
	}
}

// S3.7 Given the ticker written as `petr4`, when the row is drawn, then it
// shows `PETR4`. (D50)
func TestS3_7_View_LowerCaseTickerIsShownInUpperCase(t *testing.T) {
	_, f := viewWith(t, `tickers = ["petr4"]`+"\n", nil)
	rowLine(t, f, "PETR4")
	if strings.Contains(f, "petr4") {
		t.Errorf("the view shows the ticker as written, petr4:\n%s", f)
	}
}

// ---------------------------------------------------------------------------
// S11 - The watchlist is a file I edit
// ---------------------------------------------------------------------------

// S11.2 Given `--config /tmp/other.toml`, when the tool starts, then it
// shows the watchlist of that file. (D29)
func TestS11_2_View_ConfigFlagIsRead(t *testing.T) {
	// Given: VALE3 at the default path, PETR4 and AAPL in the other file
	w := newWorld(t, bothOpen, `tickers = ["VALE3"]`+"\n")
	loadQuotes(w.src, w.now, true)
	other := writeFile(t, "other.toml", `tickers = ["PETR4", "AAPL"]`+"\n")

	// When
	f := openView(t, w, viewOpts{args: []string{"--config", other}}).settled().text()

	// Then
	assertRowHas(t, f, "PETR4", "R$ 38,42")
	assertRowHas(t, f, "AAPL", "US$ 229.87")
	if hasRow(f, "VALE3") {
		t.Errorf("the view shows VALE3, from the default path, with --config %s:\n%s", other, f)
	}
}

// ---------------------------------------------------------------------------
// S15 - The last quotes are kept between runs
// ---------------------------------------------------------------------------

// S15.1 Given a run that fetched quotes and then ended, when the tool is
// started again with no network, then the rows show those quotes. (D20)
func TestS15_1_View_QuotesOfAnEarlierRunAreShownWithNoNetwork(t *testing.T) {
	for _, c := range withTwenty(plainConfig, map[string]string{"PETR4": "R$ 38,42", "VALE3": "R$ 61,07", "AAPL": "US$ 229.87"}) {
		t.Run(c.name, func(t *testing.T) {
			// Given: an earlier view that fetched, closed with q
			w := newWorld(t, bothOpen, c.config)
			rows := c.load(w.src, w.now)
			earlier := openView(t, w, viewOpts{})
			first := earlier.settled().text()
			for tk, price := range rows {
				if line := rowLine(t, first, tk); !strings.Contains(line, price) {
					t.Fatalf("given not established: the earlier view did not show %s for %s: %q", price, tk, line)
				}
			}
			if code := earlier.quit("q"); code != 0 {
				t.Fatalf("given not established: the earlier view exited with status %d", code)
			}
			w.advance(5 * time.Minute)
			w.src.failAll(provider.Unavailable)

			// When
			f := openView(t, w, viewOpts{}).settled().text()

			// Then
			for tk, price := range rows {
				assertRowHas(t, f, tk, price)
			}
		})
	}
}

// spoiledView is the scenario of S15.4 and S15.7 in the view: one world has
// a cache from an earlier print, every file of it overwritten with content;
// another never had a cache. Both open the view against the same source,
// which holds its answers, at the same moment.
func spoiledView(t *testing.T, content []byte) (spoiled, noCache *viewRun) {
	t.Helper()
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.overwriteCache(content)
	w.src.setPrice(petr4, 39.10)

	control := newWorld(t, bothOpen, plainConfig)
	loadQuotes(control.src, control.now, true)
	control.src.setPrice(petr4, 39.10)

	return openView(t, w, viewOpts{hold: true}), openView(t, control, viewOpts{hold: true})
}

// S15.4 Given a cache file emptied or filled with garbage, when the tool
// starts, then it shows the watchlist as it does with no cache. (D48)
func TestS15_4_View_UnreadableCacheIsTreatedAsNoCache(t *testing.T) {
	for name, content := range spoiledCaches {
		t.Run(name, func(t *testing.T) {
			// Given, When
			spoiled, noCache := spoiledView(t, content)

			// Then: before any answer
			s, n := spoiled.beforeAnswer("the rows", showsRows("PETR4", "VALE3", "AAPL")), noCache.beforeAnswer("the rows", showsRows("PETR4", "VALE3", "AAPL"))
			rowLine(t, n.text(), "PETR4") // with no cache, the watchlist is shown
			if s.raw() != n.raw() {
				t.Errorf("before any answer, the view with a spoiled cache differs from the view with no cache.\nspoiled:\n%s\nno cache:\n%s", s.text(), n.text())
			}

			// Then: once the source has answered
			spoiled.src.releaseAll()
			noCache.src.releaseAll()
			s, n = spoiled.settled(), noCache.settled()
			if s.raw() != n.raw() {
				t.Errorf("after the fetch at start, the view with a spoiled cache differs from the view with no cache.\nspoiled:\n%s\nno cache:\n%s", s.text(), n.text())
			}
		})
	}
}

// S15.7 Given a cache file emptied or filled with garbage, when the tool
// starts, then no message about the cache is shown. (D48)
func TestS15_7_View_UnreadableCacheRaisesNoMessage(t *testing.T) {
	for name, content := range spoiledCaches {
		t.Run(name, func(t *testing.T) {
			// Given, When
			spoiled, noCache := spoiledView(t, content)
			spoiled.beforeAnswer("the rows", showsRows("PETR4", "VALE3", "AAPL"))
			spoiled.src.releaseAll()
			noCache.src.releaseAll()
			rowLine(t, spoiled.settled().text(), "PETR4")
			noCache.settled()

			// Then: no frame mentions the cache, and the error stream says
			// no more than it does with no cache
			for i, d := range spoiled.allFrames() {
				if strings.Contains(strings.ToLower(d.text()), "cache") {
					t.Errorf("frame %d shows a message about the cache:\n%s", i, d.text())
					break
				}
			}
			if s, n := spoiled.stderr.String(), noCache.stderr.String(); s != n || strings.Contains(strings.ToLower(s), "cache") {
				t.Errorf("error stream with a spoiled cache: %q, with no cache: %q", s, n)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// S16 - Colour (the view is always on a terminal)
// ---------------------------------------------------------------------------

// S16.1 Given a row whose day change is positive, when it is drawn on a
// terminal, then the change is in the terminal palette's green. (D25)
func TestS16_1_View_PositiveChangeIsGreen(t *testing.T) {
	// Given: PETR4 at 38.42 after a close of 38.11: +0,31, +0,81%
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	f := openView(t, w, viewOpts{}).settled().raw()

	// Then
	line := rowOnTerminal(t, f, "PETR4")
	assertColoured(t, line, "+0,31", "green", "32", "92")
	assertColoured(t, line, "+0,81%", "green", "32", "92")
}

// S16.2 Given a row whose day change is negative, when it is drawn on a
// terminal, then the change is in the terminal palette's red. (D25)
func TestS16_2_View_NegativeChangeIsRed(t *testing.T) {
	// Given: AAPL at 229.87 after a close of 230.99: -1.12, -0.48%
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	f := openView(t, w, viewOpts{}).settled().raw()

	// Then
	line := rowOnTerminal(t, f, "AAPL")
	assertColoured(t, line, "-1.12", "red", "31", "91")
	assertColoured(t, line, "-0.48%", "red", "31", "91")
}

// S16.3 Given `NO_COLOR` is set, when the view or the one-shot output is
// drawn, then it contains no colour codes. (D25)
func TestS16_3_View_NoColorMeansNoColourCodes(t *testing.T) {
	// Given established: without NO_COLOR, the same view is in colour
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	if on := openView(t, w, viewOpts{}).settled(); !colourCode(on.raw()) {
		t.Fatalf("given not established: without NO_COLOR the view has no colour either, so its absence proves nothing:\n%q", on.raw())
	}

	// Given: NO_COLOR set
	w2 := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w2.src, w2.now, true)

	// When
	v := openView(t, w2, viewOpts{env: map[string]string{"NO_COLOR": "1"}})
	rowLine(t, v.settled().text(), "PETR4")

	// Then: no frame at all holds a colour code
	for i, d := range v.allFrames() {
		if colourCode(d.raw()) {
			t.Errorf("frame %d holds colour codes with NO_COLOR set:\n%q", i, d.raw())
			break
		}
	}
	if colourCode(v.stdout.String()) || colourCode(v.stderr.String()) {
		t.Errorf("the output streams hold colour codes with NO_COLOR set: stdout %q, stderr %q", v.stdout.String(), v.stderr.String())
	}
}

// S16.4 Given colour is off, when a row is drawn, then the day change still
// starts with `+` or `-`. (D50)
func TestS16_4_View_ChangeKeepsItsSignWithoutColour(t *testing.T) {
	// Given: in the view colour is off only through NO_COLOR; PETR4 up,
	// AAPL down, VALE3 unchanged
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	f := openView(t, w, viewOpts{env: map[string]string{"NO_COLOR": "1"}}).settled().raw()

	// Then
	assertRowHas(t, f, "PETR4", "+0,31", "+0,81%")
	assertRowHas(t, f, "AAPL", "-1.12", "-0.48%")
	assertRowHas(t, f, "VALE3", "+0,00", "+0,00%")
}

// ---------------------------------------------------------------------------
// S17 - The data source can be replaced
// ---------------------------------------------------------------------------

// S17.2 Given a stand-in source put in place of the default one, when the
// tool runs with an unchanged config file, then the same rows are shown with
// the stand-in's quotes. (D7, D17)
func TestS17_2_View_StandInSourceShowsTheSameRowsWithItsQuotes(t *testing.T) {
	// Given: one config file, the view opened against one stand-in and then
	// another
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
	a := openView(t, first, viewOpts{}).settled().text()
	b := openView(t, second, viewOpts{}).settled().text()

	// Then: the same rows
	ra := rowTickers(a, "PETR4", "AAPL", "VALE3")
	rb := rowTickers(b, "PETR4", "AAPL", "VALE3")
	if strings.Join(ra, " ") != "PETR4 AAPL VALE3" || strings.Join(ra, " ") != strings.Join(rb, " ") {
		t.Errorf("rows differ: %v with one source, %v with the other, want PETR4 AAPL VALE3 for both", ra, rb)
	}
	// Then: each with its own stand-in's quotes
	for tk, price := range map[string]string{"PETR4": "R$ 40,01", "VALE3": "R$ 62,02", "AAPL": "US$ 233.03"} {
		assertRowHas(t, b, tk, price)
	}
	for tk, price := range map[string]string{"PETR4": "R$ 38,42", "VALE3": "R$ 61,07", "AAPL": "US$ 229.87"} {
		assertRowHas(t, a, tk, price)
	}
}

// S17.4 Given the help text, when it is read, then `BRL_STOCKS_SOURCE_URL`
// does not appear in it. (D70)
func TestS17_4_View_HelpDoesNotNameTheSourceURLVariable(t *testing.T) {
	// Given: the help text of `brl-stocks`, without `print` (D77)
	w := newWorld(t, bothOpen, plainConfig)
	r := runFlags(t, w, "--help")
	help := r.stdout + r.stderr
	if r.code != 0 || strings.TrimSpace(help) == "" {
		t.Fatalf("given not established: brl-stocks --help exited with status %d and printed %q", r.code, help)
	}

	// Then
	if strings.Contains(help, "BRL_STOCKS_SOURCE_URL") {
		t.Errorf("the help text names BRL_STOCKS_SOURCE_URL:\n%s", help)
	}
}

// ---------------------------------------------------------------------------
// S18 - Command line, without `print` (D77)
// ---------------------------------------------------------------------------

// S18.1 Given `--version`, when the tool is run, then it prints the version
// and exits with status 0. (D50, D77)
func TestS18_1_View_VersionExitsZero(t *testing.T) {
	w := newWorld(t, bothOpen, plainConfig)
	r := runFlags(t, w, "--version")
	t.Logf("stdout: %q\nstderr: %q", r.stdout, r.stderr)
	if r.code != 0 {
		t.Errorf("brl-stocks --version: exit status %d, want 0\nstderr: %q", r.code, r.stderr)
	}
	if strings.TrimSpace(r.stdout) == "" {
		t.Errorf("brl-stocks --version printed no version on standard output; stderr: %q", r.stderr)
	}
}

// S18.2 Given `--help`, when the tool is run, then it prints the usage and
// exits with status 0. (D50, D77)
func TestS18_2_View_HelpExitsZero(t *testing.T) {
	w := newWorld(t, bothOpen, plainConfig)
	r := runFlags(t, w, "--help")
	t.Logf("stdout: %q\nstderr: %q", r.stdout, r.stderr)
	if r.code != 0 {
		t.Errorf("brl-stocks --help: exit status %d, want 0\nstderr: %q", r.code, r.stderr)
	}
	if strings.TrimSpace(r.stdout+r.stderr) == "" {
		t.Errorf("brl-stocks --help printed no usage")
	}
}

// S18.3 Given a flag the tool does not know, when the tool is run, then it
// exits with status 2. (D50, D77)
func TestS18_3_View_UnknownFlagExitsTwo(t *testing.T) {
	w := newWorld(t, bothOpen, plainConfig)
	if r := runFlags(t, w, "--no-such-flag"); r.code != 2 {
		t.Errorf("brl-stocks --no-such-flag: exit status %d, want 2\nstderr: %q", r.code, r.stderr)
	}
}

// S18.4 Given a flag the tool does not know, when the tool is run, then a
// usage message is on the error stream. (D50, D77)
func TestS18_4_View_UnknownFlagPrintsUsageToErrorStream(t *testing.T) {
	w := newWorld(t, bothOpen, plainConfig)
	r := runFlags(t, w, "--no-such-flag")
	t.Logf("exit status %d\nstdout: %q\nstderr: %q", r.code, r.stdout, r.stderr)
	if strings.TrimSpace(r.stderr) == "" {
		t.Errorf("brl-stocks --no-such-flag left the error stream empty, want a usage message (exit status %d, stdout %q)", r.code, r.stdout)
	}
}
