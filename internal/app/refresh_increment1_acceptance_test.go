package app_test

// Acceptance tests of slice 3, increment 1: the full-screen view left open
// while time passes, with a healthy source. Written from docs/spec.md and
// the view's test boundary (docs/design.md, "Time") before the behaviour
// exists. Time passes only through Input{Elapse: d} on a deps.Clock that the
// view (Terminal.Clock) and the stand-in source share; no test sleeps to let
// view time pass. Requests are counted, with their times, from the stand-in's
// own record.

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
// The stand-in source on the view's clock
// ---------------------------------------------------------------------------

// clockSource is the stand-in of the print tests, stamping every request with
// the shared clock's time. It answers at once (it never waits on the clock),
// unless it is told to hold its answers.
type clockSource struct {
	*fakeSource
	clock *deps.Clock

	mu       sync.Mutex
	at       map[market.Instrument][]time.Time
	before   func(in market.Instrument, now time.Time) // runs before each answer; may change what the source answers
	held     bool
	release  chan struct{}
	inflight int
}

func newClockSource(src *fakeSource, clock *deps.Clock) *clockSource {
	return &clockSource{fakeSource: src, clock: clock, at: map[market.Instrument][]time.Time{}, release: make(chan struct{})}
}

func (s *clockSource) Quote(ctx context.Context, in market.Instrument) (market.Quote, error) {
	now := s.clock.Now()
	s.mu.Lock()
	s.at[in] = append(s.at[in], now)
	if s.before != nil {
		s.before(in, now)
	}
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

// times lists when in was requested, on the clock, in order.
func (s *clockSource) times(in market.Instrument) []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.at[in]...)
}

// setBefore sets the function that runs before each answer.
func (s *clockSource) setBefore(f func(in market.Instrument, now time.Time)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.before = f
}

func (s *clockSource) hold() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held = true
}

func (s *clockSource) releaseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held {
		s.held = false
		close(s.release)
	}
}

func (s *clockSource) inFlight() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inflight
}

// requestedIn counts the requests for in at times in (from, to].
func (s *clockSource) requestedIn(in market.Instrument, from, to time.Time) int {
	n := 0
	for _, at := range s.times(in) {
		if at.After(from) && !at.After(to) {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Running the view on a clock
// ---------------------------------------------------------------------------

// clockRun is a view running on a deps.Clock. It keeps only the latest
// frame: hours of view time draw many thousands of frames.
type clockRun struct {
	t      *testing.T
	clock  *deps.Clock
	src    *clockSource
	in     chan deps.Input
	sent   int
	stdout *lockedBuffer
	stderr *lockedBuffer

	mu     sync.Mutex
	last   drawn
	frames int
	done   chan struct{}
	code   int
}

// clockWorld is newWorld whose config file carries the world's start as its
// modification time (design, "Time": every file a test writes while it uses
// a clock gets its time from that clock).
func clockWorld(t *testing.T, start time.Time, config string) *world {
	t.Helper()
	w := newWorld(t, start, config)
	if err := os.Chtimes(w.configPath, start, start); err != nil {
		t.Fatal(err)
	}
	return w
}

// openOnClock runs `brl-stocks` (args, if any) on a Terminal whose Clock
// starts at the world's current time, with the world's environment and a
// clockSource on the world's stand-in. The view is closed when the test ends.
func openOnClock(t *testing.T, w *world, args ...string) *clockRun {
	t.Helper()
	guardConfig(t, w.configPath)
	clock := deps.NewClock(w.now)
	src := newClockSource(w.src, clock)
	env := map[string]string{}
	for k, v := range w.env {
		env[k] = v
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &clockRun{
		t: t, clock: clock, src: src,
		in:     make(chan deps.Input, 64),
		stdout: &lockedBuffer{}, stderr: &lockedBuffer{},
		done: make(chan struct{}),
	}
	if args == nil {
		args = []string{}
	}
	d := deps.Deps{
		Provider: src,
		// With a Clock the view uses neither; they are the same clock, so
		// that nothing else moves time if they were used.
		Now:   clock.Now,
		After: clock.After,
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
				r.last = drawn{f: f}
				r.frames++
				r.mu.Unlock()
			},
			Clock: clock,
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

func (r *clockRun) latestFrame() (drawn, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last, r.frames
}

// wait waits until the latest frame satisfies ok, with a real-time guard
// that fails the test instead of hanging.
func (r *clockRun) wait(desc string, guard time.Duration, ok func(drawn) bool) drawn {
	r.t.Helper()
	deadline := time.Now().Add(guard)
	for {
		last, n := r.latestFrame()
		if n > 0 && ok(last) {
			return last
		}
		select {
		case <-r.done:
			last, n = r.latestFrame()
			if n > 0 && ok(last) {
				return last
			}
			r.mu.Lock()
			code := r.code
			r.mu.Unlock()
			r.t.Fatalf("the view ended with status %d after %d frames, before %s\nstdout: %q\nstderr: %q",
				code, n, desc, r.stdout.String(), r.stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("no %s within %v (real time); %d frames drawn; last frame (inputs %d, idle %v):\n%s",
				desc, guard, n, last.f.Inputs, last.f.Idle, last.text())
		}
		time.Sleep(time.Millisecond)
	}
}

// settled is the idle frame after the inputs sent so far.
func (r *clockRun) settled(guard time.Duration) drawn {
	r.t.Helper()
	sent := r.sent
	return r.wait(fmt.Sprintf("idle frame after %d inputs", sent), guard, func(d drawn) bool {
		return d.f.Idle && d.f.Inputs == sent
	})
}

// put sends one input without waiting for the view to settle.
func (r *clockRun) put(in deps.Input) {
	r.t.Helper()
	select {
	case r.in <- in:
		r.sent++
	case <-time.After(viewGuard):
		r.t.Fatalf("the view takes no input %+v", in)
	}
}

// elapse lets d pass on the view's clock, in steps of at most ten minutes
// (splitting an Elapse gives the same result), and returns the idle frame
// after the last one: the screen after d.
func (r *clockRun) elapse(d time.Duration) drawn {
	r.t.Helper()
	const step = 10 * time.Minute
	var f drawn
	for d > 0 {
		s := d
		if s > step {
			s = step
		}
		r.put(deps.Input{Elapse: s})
		f = r.settled(viewGuard + 3*s/time.Minute*time.Second)
		d -= s
	}
	return f
}

// clockAt establishes that the view's clock stands at want in frame f: the
// view has stayed open that long.
func clockAt(t *testing.T, f drawn, want time.Time) {
	t.Helper()
	if !f.f.Now.Equal(want) {
		t.Fatalf("given not established: the frame after the time step carries the clock at %v, want %v: the view did not stay open that long",
			f.f.Now.UTC(), want.UTC())
	}
}

// marketStates is the status line of a frame.
func marketStates(t *testing.T, f drawn) string {
	t.Helper()
	_, st := statusLine(t, f.text())
	return st
}

// cacheSize is the size in bytes of every regular file under dir.
func cacheSize(t *testing.T, dir string) int64 {
	t.Helper()
	var n int64
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			n += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("cannot measure the cache folder %s: %v", dir, err)
	}
	return n
}

// instruments maps tickers to instruments by the rule of D30, for the
// tickers these tests use.
func instruments(tickers ...string) []market.Instrument {
	var ins []market.Instrument
	for _, tk := range tickers {
		m := market.US
		for _, b := range twentyB3 {
			if tk == b {
				m = market.B3
			}
		}
		if tk == "XXXX9" {
			m = market.B3
		}
		ins = append(ins, market.Instrument{Market: m, Symbol: tk})
	}
	return ins
}

// loadTwentyUSClosed is loadTwenty with the US market closed: the source
// reports Friday's US session and Friday's close.
func loadTwentyUSClosed(src *fakeSource, now time.Time) {
	loadTwenty(src, now)
	loadQuotes(src, now, false)
	for i, s := range twentyUS {
		if s == "AAPL" || s == "MSFT" {
			continue
		}
		p := float64(40+i) + 0.25
		src.answer(market.Instrument{Market: market.US, Symbol: s}, quote(s+" Inc", "USD", p, p, usFridaySession, usFridaySession.End))
	}
}

// ---------------------------------------------------------------------------
// S5 - Automatic refresh, and markets that are closed
// ---------------------------------------------------------------------------

// S5.1 Given B3 is open and no `refresh_seconds` is set, when the view stays
// open for 10 minutes, then each B3 row is requested from the source between
// 9 and 11 times. (D14, D50)
func TestS5_1_View_DefaultIntervalTenMinutes(t *testing.T) {
	for _, c := range []struct {
		name, config string
		load         func(src *fakeSource, now time.Time)
		b3           []string
	}{
		{"criterion", plainConfig, func(src *fakeSource, now time.Time) { loadQuotes(src, now, true) }, []string{"PETR4", "VALE3"}},
		{"20 tickers", twentyConfig(), func(src *fakeSource, now time.Time) { loadTwenty(src, now) }, twentyB3},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Given: Monday 15:00 UTC, B3 open until 20:00; no refresh_seconds
			w := clockWorld(t, bothOpen, c.config)
			c.load(w.src, w.now)
			v := openOnClock(t, w)
			v.settled(viewGuard)

			// When: the view stays open for 10 minutes (the fetch at start
			// counts: the range absorbs it)
			v.elapse(10 * time.Minute)

			// Then
			for _, in := range instruments(c.b3...) {
				if n := len(v.src.times(in)); n < 9 || n > 11 {
					t.Errorf("B3 row %s was requested %d times in 10 minutes, want between 9 and 11 (at %v)", in.Symbol, n, v.src.times(in))
				}
			}
		})
	}
}

// S5.2 Given `refresh_seconds = 120` and an open market, when the view stays
// open for 10 minutes, then each row of that market is requested between 4
// and 6 times. (D14, D42, D50)
func TestS5_2_View_IntervalOf120Seconds(t *testing.T) {
	for _, c := range []struct {
		name, config string
		load         func(src *fakeSource, now time.Time)
		rows         []string
	}{
		{"criterion", "refresh_seconds = 120\n" + plainConfig, func(src *fakeSource, now time.Time) { loadQuotes(src, now, true) }, []string{"PETR4", "VALE3", "AAPL"}},
		{"20 tickers", "refresh_seconds = 120\n" + twentyConfig(), func(src *fakeSource, now time.Time) { loadTwenty(src, now) }, twentyTickers()},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Given: Monday 15:00 UTC, B3 and US both open
			w := clockWorld(t, bothOpen, c.config)
			c.load(w.src, w.now)
			v := openOnClock(t, w)
			v.settled(viewGuard)

			// When
			v.elapse(10 * time.Minute)

			// Then
			for _, in := range instruments(c.rows...) {
				if n := len(v.src.times(in)); n < 4 || n > 6 {
					t.Errorf("row %s was requested %d times in 10 minutes with refresh_seconds = 120, want between 4 and 6 (at %v)", in.Symbol, n, v.src.times(in))
				}
			}
		})
	}
}

// S5.3 Given the US trading period ended more than 5 minutes ago and every
// US row has a quote, when the view stays open for 10 minutes, then no US row
// is requested. (D15, D50, D111, D113)
func TestS5_3_View_ClosedUSRowsAreNotRequested(t *testing.T) {
	for _, c := range []struct {
		name, config string
		load         func(src *fakeSource, now time.Time)
		us           []string
	}{
		{"criterion", plainConfig, func(src *fakeSource, now time.Time) { loadQuotes(src, now, true) }, []string{"AAPL"}},
		{"20 tickers", twentyConfig(), func(src *fakeSource, now time.Time) { loadTwenty(src, now) }, twentyUS},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Given: Monday; the US trading period in the source's answer
			// ended at 21:00 UTC; a print at 20:50, while it was open,
			// fetched every row; the view opens at 21:06, 6 minutes after
			// the end
			usEnd := usSession.End
			w := clockWorld(t, usEnd.Add(-10*time.Minute), c.config)
			c.load(w.src, usEnd.Add(-10*time.Minute))
			w.prime(instruments(c.us...)...)
			w.now = usEnd.Add(6 * time.Minute)
			v := openOnClock(t, w)
			f := v.settled(viewGuard).text()
			for _, tk := range c.us {
				if line := rowLine(t, f, tk); !strings.Contains(line, "US$") {
					t.Fatalf("given not established: US row %s has no quote: %q", tk, line)
				}
			}

			// When
			clockAt(t, v.elapse(10*time.Minute), usEnd.Add(16*time.Minute))

			// Then
			for _, in := range instruments(c.us...) {
				if at := v.src.times(in); len(at) != 0 {
					t.Errorf("US row %s, its trading period ended 6 minutes before the view opened, was requested %d times while the view was open (at %v), want none", in.Symbol, len(at), at)
				}
			}
		})
	}
}

// S5.7 Given a closed market and an open view, when the market's next
// session starts, then its rows are requested within one refresh interval
// without a key press. (D14, D15, D50)
func TestS5_7_View_NextSessionStartsRowsRequested(t *testing.T) {
	// Given: Monday 13:30 UTC; the source reports Friday's US session until
	// Monday's starts at 14:30 UTC (the same hours on the next weekday), and
	// Monday's from then on; the default interval, 60 seconds
	w := clockWorld(t, onlyB3Open, `tickers = ["PETR4", "AAPL", "MSFT"]`+"\n")
	loadQuotes(w.src, w.now, false)
	v := openOnClock(t, w)
	v.src.setBefore(func(in market.Instrument, now time.Time) {
		if !now.Before(usSession.Start) {
			v.src.fakeSource.answer(aapl, quote("Apple Inc.", "USD", 229.87, 230.99, usSession, now.Add(-time.Second)))
			v.src.fakeSource.answer(msft, quote("Microsoft Corp", "USD", 512.30, 510.25, usSession, now.Add(-time.Second)))
			v.src.fakeSource.answer(sp500, quote("S&P 500", "", 6812.40, 6819.90, usSession, now.Add(-time.Second)))
		}
	})
	f := v.settled(viewGuard)
	if st := marketStates(t, f); !strings.Contains(st, "US closed") {
		t.Fatalf("given not established: the status line does not show the US market closed: %q", st)
	}

	// When: the view stays open, with no key, until one interval after the
	// session starts
	end := usSession.Start.Add(60 * time.Second)
	v.elapse(end.Sub(onlyB3Open))

	// Then
	for _, in := range []market.Instrument{aapl, msft} {
		if n := v.src.requestedIn(in, usSession.Start.Add(-time.Nanosecond), end); n == 0 {
			t.Errorf("US row %s was not requested between the session's start (%v) and one interval later; requested at %v",
				in.Symbol, usSession.Start.UTC(), v.src.times(in))
		}
	}
}

// S5.8 Given B3's trading period ended less than 100 minutes ago, when a
// refresh interval passes, then each B3 row is requested again. (D50, D111)
func TestS5_8_View_JustClosedRowIsRequestedAgain(t *testing.T) {
	for _, c := range []struct {
		name, config string
		b3           []string
		load         func(src *fakeSource, now time.Time)
	}{
		{"criterion", `tickers = ["PETR4", "VALE3"]` + "\n", []string{"PETR4", "VALE3"}, func(src *fakeSource, now time.Time) { loadQuotes(src, now, true) }},
		{"20 tickers", twentyConfig(), twentyB3, func(src *fakeSource, now time.Time) { loadTwenty(src, now) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			justClosedRequestedAgain(t, c.config, c.b3, c.load)
		})
	}
}

func justClosedRequestedAgain(t *testing.T, config string, b3 []string, load func(src *fakeSource, now time.Time)) {
	// Given: Monday 21:35 UTC; the B3 trading period in the source's answer
	// ended at 20:00, 95 minutes ago; its quotes are from 19:45; the default
	// interval, 60 seconds
	start := b3Session.End.Add(95 * time.Minute)
	w := clockWorld(t, start, config)
	load(w.src, b3Session.End)
	v := openOnClock(t, w)
	f := v.settled(viewGuard)
	for _, tk := range b3 {
		if line := rowLine(t, f.text(), tk); !strings.Contains(line, "R$") {
			t.Fatalf("given not established: row %s has no quote: %q", tk, line)
		}
	}

	// When: a refresh interval passes, and the heartbeat after it. By 6.5 a
	// quote at most one interval old is not fetched: the row is due only
	// once it is older than one interval, and the view checks what is due at
	// its heartbeat, once a second of the clock (design, "Time"). So the row
	// is requested again within (t0+interval, t0+interval+1s].
	from, to := start.Add(60*time.Second), start.Add(61*time.Second)
	v.elapse(61 * time.Second)

	// Then
	for _, in := range instruments(b3...) {
		if n := v.src.requestedIn(in, from, to); n == 0 {
			t.Errorf("B3 row %s, its trading period ended 95 minutes ago, was not requested again by the heartbeat after one interval, in (%v, %v]; requested at %v",
				in.Symbol, from.UTC(), to.UTC(), v.src.times(in))
		}
	}
}

// S5.10 Given `refresh_seconds = 3`, when the view runs, then rows are
// requested no more often than every 10 seconds. (D50)
func TestS5_10_View_IntervalBelowTenIsTen(t *testing.T) {
	for _, c := range []struct {
		name, config string
		load         func(src *fakeSource, now time.Time)
		rows         []string
	}{
		{"criterion", "refresh_seconds = 3\n" + plainConfig, func(src *fakeSource, now time.Time) { loadQuotes(src, now, true) }, []string{"PETR4", "VALE3", "AAPL"}},
		{"20 tickers", "refresh_seconds = 3\n" + twentyConfig(), func(src *fakeSource, now time.Time) { loadTwenty(src, now) }, twentyTickers()},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Given: Monday 15:00 UTC, both markets open
			w := clockWorld(t, bothOpen, c.config)
			c.load(w.src, w.now)
			v := openOnClock(t, w)
			v.settled(viewGuard)

			// When: the view runs for 2 minutes
			v.elapse(2 * time.Minute)

			// Given established: the view refreshed the rows while it ran,
			// so the time between their requests is shown
			for _, in := range instruments(c.rows...) {
				if n := len(v.src.times(in)); n < 2 {
					t.Fatalf("given not established: row %s was requested %d times in 2 minutes of open market; no refresh ran", in.Symbol, n)
				}
			}

			// Then
			for _, in := range instruments(c.rows...) {
				at := v.src.times(in)
				for i := 1; i < len(at); i++ {
					if gap := at[i].Sub(at[i-1]); gap < 10*time.Second {
						t.Errorf("row %s was requested %v after its previous request, want at least 10s (at %v)", in.Symbol, gap, at)
						break
					}
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// S8 - A ticker the source does not know
// ---------------------------------------------------------------------------

// S8.3 Given a not-found row and an unchanged config, when the view stays
// open for three hours, then the ticker is requested at least twice and at
// most four times. (D39)
func TestS8_3_View_NotFoundRetriedHourly(t *testing.T) {
	// Given: Monday 15:00 UTC; the stand-in does not know XXXX9
	w := clockWorld(t, bothOpen, notFoundConfig)
	loadQuotes(w.src, w.now, true)
	v := openOnClock(t, w)
	f := v.settled(viewGuard).text()
	if line := rowLine(t, f, "XXXX9"); !strings.Contains(line, "not found") {
		t.Fatalf("given not established: row XXXX9 does not show not found: %q", line)
	}

	// When
	v.elapse(3 * time.Hour)

	// Then
	if n := len(v.src.times(xxxx9)); n < 2 || n > 4 {
		t.Errorf("not-found XXXX9 was requested %d times in three hours, want at least 2 and at most 4 (at %v)", n, v.src.times(xxxx9))
	}
}

// ---------------------------------------------------------------------------
// S15 - The last quotes are kept between runs
// ---------------------------------------------------------------------------

// S15.3 Given the view has run for eight hours of open market, when the
// cache folder is measured, then its size is within 10% of its size after
// the first refresh in which every row has its full sparkline. (D20, D50,
// D108)
func TestS15_3_View_CacheSizeAfterEightHours(t *testing.T) {
	// Given: Monday 11:00 UTC; the source reports one long session, 10:00 to
	// 20:00 UTC, for every instrument, so the market is open for the eight
	// hours; every answer carries a new price and quote time
	start := monday.Add(11 * time.Hour)
	long := func(tz string) market.Session {
		return market.Session{Start: monday.Add(10 * time.Hour), End: monday.Add(20 * time.Hour), TZ: tz}
	}
	b3, us := long("America/Sao_Paulo"), long("America/New_York")
	w := clockWorld(t, start, plainConfig)
	at := start.Add(-time.Minute)
	w.src.answer(petr4, quote("Petrobras PN", "BRL", 38.42, 38.11, b3, at))
	w.src.answer(vale3, quote("Vale ON", "BRL", 61.07, 61.07, b3, at))
	w.src.answer(aapl, quote("Apple Inc.", "USD", 229.87, 230.99, us, at))
	w.src.answer(ibov, quote("Ibovespa", "", 158432.10, 157919.80, b3, at))
	w.src.answer(sp500, quote("S&P 500", "", 6812.40, 6819.90, us, at))
	w.src.answer(usdbrl, quote("USD/BRL", "BRL", 5.4321, 5.4278, b3, at))
	// Every answer carries a series of 20 prices, so the fetch at start is
	// the first refresh in which every row has its full sparkline (D108)
	setAllSeries(w.src, seq(10, 0.5, 20)...)
	v := openOnClock(t, w)
	v.src.setBefore(func(in market.Instrument, now time.Time) {
		f := v.src.fakeSource
		f.mu.Lock()
		defer f.mu.Unlock()
		if q, ok := f.quotes[in]; ok {
			q.Price += 0.01
			q.Time = now.Add(-time.Minute)
			f.quotes[in] = q
		}
	})
	v.settled(viewGuard)
	first := cacheSize(t, w.cacheDir)
	if first == 0 {
		t.Fatalf("given not established: the cache folder %s is empty after the first refresh, every row with its full series", w.cacheDir)
	}

	// When: the view runs for eight hours
	v.elapse(8 * time.Hour)
	for _, in := range plainRows {
		if n := len(v.src.times(in)); n < 2 {
			t.Fatalf("given not established: row %s was requested %d times in eight hours of open market; the view did not refresh", in.Symbol, n)
		}
	}
	after := cacheSize(t, w.cacheDir)

	// Then
	if diff := after - first; diff*10 > first || -diff*10 > first {
		t.Errorf("the cache folder holds %d bytes after eight hours, %d after the first refresh with every full sparkline: more than 10%% apart", after, first)
	}
}

// ---------------------------------------------------------------------------
// Criteria of slice 2 shown again while the view is open (scope, increment 1)
// ---------------------------------------------------------------------------

// S4.4 Given the same ticker in two groups, when a refresh runs, then the
// ticker is requested from the source once. (D50)
func TestS4_4_ViewOpen_TickerInTwoGroupsIsRequestedOnceOnAHeartbeatRefresh(t *testing.T) {
	// Given: PETR4 in both groups; Monday 15:00 UTC, both markets open
	const config = `[[group]]
name = "Brasil"
tickers = ["PETR4", "VALE3"]

[[group]]
name = "Favourites"
tickers = ["PETR4", "AAPL"]
`
	w := clockWorld(t, bothOpen, config)
	loadQuotes(w.src, w.now, true)
	v := openOnClock(t, w)
	v.settled(viewGuard)

	// When: the refresh after one interval (60 seconds), not the fetch at
	// start
	v.elapse(90 * time.Second)
	end := bothOpen.Add(90 * time.Second)

	// Given established: a refresh ran while the view was open
	refreshes := v.src.requestedIn(vale3, bothOpen, end)
	if refreshes == 0 || v.src.requestedIn(aapl, bothOpen, end) == 0 {
		t.Fatalf("given not established: no refresh ran in the 90 seconds after the fetch at start (VALE3 at %v, AAPL at %v)", v.src.times(vale3), v.src.times(aapl))
	}

	// Then
	if n := v.src.requestedIn(petr4, bothOpen, end); n != refreshes {
		t.Errorf("PETR4, in two groups, was requested %d times by %d refreshes while the view was open, want once per refresh (at %v)", n, refreshes, v.src.times(petr4))
	}
}

// S6.7 Given an open market and a row whose last successful fetch is older
// than two refresh intervals with no failed fetch since (the machine just
// woke from sleep), when the view is drawn, then the row is marked stale.
// (D40)
func TestS6_7_ViewOpen_RowGoesStaleByAgeWhenItsMarketOpens(t *testing.T) {
	// Given: a print on Saturday fetched the US rows (Friday's close); the
	// view opens on Monday 14:20 UTC, before the US session (14:30), so
	// those rows are fresh. When the session starts, their last successful
	// fetch is two days old with no failed fetch since. The source holds its
	// answers from then on, so the frame is drawn before the fetch that
	// would make them fresh again answers.
	const config = "summary = []\n" + `tickers = ["AAPL", "MSFT"]` + "\n"
	saturday := onlyB3Open.Add(-48 * time.Hour)
	w := clockWorld(t, saturday, config)
	loadQuotes(w.src, w.now, false)
	w.prime(aapl, msft)
	w.now = usSession.Start.Add(-10 * time.Minute)
	v := openOnClock(t, w)
	f := v.settled(viewGuard)
	if line := rowLine(t, f.text(), "AAPL"); !strings.Contains(line, "US$ 229.87") {
		t.Fatalf("given not established: row AAPL does not show its cached quote: %q", line)
	}
	if st := marketStates(t, f); !strings.Contains(st, "US closed") {
		t.Fatalf("given not established: the status line does not show the US market closed before its session: %q", st)
	}

	// When: the view stays open past the session's start
	v.src.hold()
	v.put(deps.Input{Elapse: 20 * time.Minute})
	during := v.sent - 1
	deadline := time.Now().Add(viewGuard)
	for v.src.inFlight() == 0 {
		if last, _ := v.latestFrame(); last.f.Idle && last.f.Inputs == v.sent {
			t.Fatalf("given not established: 20 minutes passed over the US session's start and the view sent no request, so no frame was drawn with the market open before an answer (requests at %v)", v.src.times(aapl))
		}
		if time.Now().After(deadline) {
			t.Fatalf("given not established: no request reached the held source within %v (real time)", viewGuard)
		}
		time.Sleep(time.Millisecond)
	}
	d := v.wait("a frame drawn after the session's start, before any answer", viewGuard, func(d drawn) bool {
		return d.f.Inputs == during && !d.f.Now.Before(usSession.Start)
	})
	if st := marketStates(t, d); !strings.Contains(st, "US open") {
		t.Fatalf("given not established: after the session's start (clock %v) the status line does not show the US market open: %q", d.f.Now.UTC(), st)
	}

	// Then
	for _, tk := range []string{"AAPL", "MSFT"} {
		if line := rowLine(t, d.text(), tk); !viewStaleMark.MatchString(line) {
			t.Errorf("row %s, its market open and its last successful fetch two days old, is not marked stale: %q\n%s", tk, line, d.text())
		}
	}
	v.src.releaseAll()
	v.settled(viewGuard)
}

// S5.4 Given the US market is closed, when the view is drawn, then US rows
// show the last close. (D15)
func TestS5_4_ViewOpen_USClosesRowsShowTheLastClose(t *testing.T) {
	// Given: Monday 20:50 UTC, the US session ends at 21:00; from then on the
	// source answers with the closing price (AAPL 231.11, MSFT 515.55)
	start := monday.Add(20*time.Hour + 50*time.Minute)
	w := clockWorld(t, start, `tickers = ["PETR4", "AAPL", "MSFT"]`+"\n")
	loadQuotes(w.src, w.now, true)
	v := openOnClock(t, w)
	v.src.setBefore(func(in market.Instrument, now time.Time) {
		if !now.Before(usSession.End) {
			v.src.fakeSource.answer(aapl, quote("Apple Inc.", "USD", 231.11, 230.99, usSession, usSession.End))
			v.src.fakeSource.answer(msft, quote("Microsoft Corp", "USD", 515.55, 510.25, usSession, usSession.End))
		}
	})
	f := v.settled(viewGuard)
	if st := marketStates(t, f); !strings.Contains(st, "US open") {
		t.Fatalf("given not established: the status line does not show the US market open at 20:50 UTC: %q", st)
	}

	// When: the market closes while the view is open; 21:40 UTC
	f = v.elapse(50 * time.Minute)
	if st := marketStates(t, f); !strings.Contains(st, "US closed") {
		t.Fatalf("given not established: 40 minutes after the US close the status line does not show it closed: %q", st)
	}

	// Then
	assertRowHas(t, f.text(), "AAPL", "US$ 231.11")
	assertRowHas(t, f.text(), "MSFT", "US$ 515.55")
}

// S5.5 Given the US market is closed and B3 is open, when the view is drawn,
// then the status line reads `B3 open · US closed`. (D15, D47)
func TestS5_5_ViewOpen_USClosesWhileB3IsOpen(t *testing.T) {
	// Given: Friday 27 November 2026, the day after Thanksgiving: the US
	// session ends early, at 18:00 UTC; B3 is open until 20:00 UTC. The view
	// opens at 17:50 UTC with both open.
	day := monday.Add(4 * 24 * time.Hour)
	b3 := market.Session{Start: day.Add(13 * time.Hour), End: day.Add(20 * time.Hour), TZ: "America/Sao_Paulo"}
	us := market.Session{Start: day.Add(14*time.Hour + 30*time.Minute), End: day.Add(18 * time.Hour), TZ: "America/New_York"}
	fx := market.Session{Start: day.Add(9 * time.Hour), End: day.Add(22 * time.Hour), TZ: "America/Sao_Paulo"}
	start := day.Add(17*time.Hour + 50*time.Minute)
	w := clockWorld(t, start, plainConfig)
	usQuotes := func(at time.Time) {
		w.src.answer(aapl, quote("Apple Inc.", "USD", 229.87, 230.99, us, at))
		w.src.answer(sp500, quote("S&P 500", "", 6812.40, 6819.90, us, at))
	}
	w.src.answer(petr4, quote("Petrobras PN", "BRL", 38.42, 38.11, b3, start.Add(-15*time.Minute)))
	w.src.answer(vale3, quote("Vale ON", "BRL", 61.07, 61.07, b3, start.Add(-15*time.Minute)))
	w.src.answer(ibov, quote("Ibovespa", "", 158432.10, 157919.80, b3, start.Add(-15*time.Minute)))
	w.src.answer(usdbrl, quote("USD/BRL", "BRL", 5.4321, 5.4278, fx, start.Add(-10*time.Second)))
	usQuotes(start.Add(-time.Second))
	v := openOnClock(t, w)
	v.src.setBefore(func(in market.Instrument, now time.Time) {
		if !now.Before(us.End) {
			usQuotes(us.End)
		}
	})
	f := v.settled(viewGuard)
	if st := marketStates(t, f); !strings.Contains(st, "B3 open · US open") {
		t.Fatalf("given not established: at 17:50 UTC the status line does not read `B3 open · US open`: %q", st)
	}

	// When and then: 10 minutes after the US close, and 40 minutes after it
	for _, step := range []time.Duration{20 * time.Minute, 30 * time.Minute} {
		f = v.elapse(step)
		if st := marketStates(t, f); !strings.Contains(st, "B3 open · US closed") {
			t.Errorf("at %v, after the US close, the status line reads %q, want it to read `B3 open · US closed`", f.f.Now.UTC(), st)
		}
	}
}

// S5.12 Given a row whose last fetch failed before its market closed, when
// the view is drawn after the close, then the row is still marked stale.
// (D40)
func TestS5_12_ViewOpen_FailedBeforeTheCloseStaysStale(t *testing.T) {
	// Given: a print at 20:40 UTC on Monday fetches; the view opens at 20:50,
	// before the US close at 21:00, and every fetch of AAPL fails from then on
	w := clockWorld(t, monday.Add(20*time.Hour+40*time.Minute), plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(10 * time.Minute)
	w.src.fail(aapl, provider.Unavailable)
	v := openOnClock(t, w)
	v.settled(viewGuard)
	if v.src.requestedIn(aapl, w.now.Add(-time.Nanosecond), usSession.End.Add(-time.Nanosecond)) == 0 {
		t.Fatalf("given not established: the view did not request AAPL before the close, so no fetch of it failed then (at %v)", v.src.times(aapl))
	}

	// When: the view stays open past the close, to 21:40 UTC
	f := v.elapse(50 * time.Minute)
	if st := marketStates(t, f); !strings.Contains(st, "US closed") {
		t.Fatalf("given not established: 40 minutes after the US close the status line does not show it closed: %q", st)
	}

	// Then
	if line := rowLine(t, f.text(), "AAPL"); !viewStaleMark.MatchString(line) {
		t.Errorf("row AAPL, whose last fetch failed before the close, carries no stale mark after it: %q\n%s", line, f.text())
	}
}
