package app_test

// Acceptance tests of Q62, increment 1: close, open and holidays (spec 6.1,
// section 5, S5; D111-D113). Written from docs/spec.md before the behaviour
// exists. Open or closed comes from the trading period the stand-in returns
// with each quote and the view's clock. A holiday comes from the stand-in:
// at the usual open it keeps answering with the last session's trading
// period (F43), and it moves to the next session overnight. The usual open
// is the last session's start, at the same local time on the next weekday.

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucattelli/brl-stocks/internal/market"
)

// ---------------------------------------------------------------------------
// Thanksgiving week: a US holiday on Thursday 26 November 2026
// ---------------------------------------------------------------------------

var (
	wednesday = monday.Add(2 * 24 * time.Hour)
	thursday  = monday.Add(3 * 24 * time.Hour)
	friday    = monday.Add(4 * 24 * time.Hour)

	// The US sessions of the week the source knows: Wednesday's, then
	// Friday's, an early close at 18:00 UTC. Thursday has none.
	usWednesday = market.Session{Start: wednesday.Add(14*time.Hour + 30*time.Minute), End: wednesday.Add(21 * time.Hour), TZ: "America/New_York"}
	usFriday    = market.Session{Start: friday.Add(14*time.Hour + 30*time.Minute), End: friday.Add(18 * time.Hour), TZ: "America/New_York"}
	// Thursday's usual US open and close: Wednesday's hours, a day later.
	usualOpenThu  = usWednesday.Start.Add(24 * time.Hour)
	usualCloseThu = usWednesday.End.Add(24 * time.Hour)
)

func b3On(day time.Time) market.Session {
	return market.Session{Start: day.Add(13 * time.Hour), End: day.Add(20 * time.Hour), TZ: "America/Sao_Paulo"}
}

func fxOn(day time.Time) market.Session {
	return market.Session{Start: day.Add(9 * time.Hour), End: day.Add(22 * time.Hour), TZ: "America/Sao_Paulo"}
}

// holidayWeek is what the stand-in answers during Thanksgiving week. The
// trading period in an answer moves to the next session overnight, at 05:00
// UTC (F43): Wednesday's US session until Friday 05:00, Friday's after; B3
// trades every weekday.
type holidayWeek struct {
	mu  sync.Mutex
	src *fakeSource
	// usFrom, when set, is the time from which the source answers with
	// sessionLate instead (S5.16)
	usFrom      time.Time
	sessionLate market.Session
}

func (h *holidayWeek) usSessionAt(now time.Time) market.Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.usFrom.IsZero() && !now.Before(h.usFrom) {
		return h.sessionLate
	}
	if !now.Before(friday.Add(5 * time.Hour)) {
		return usFriday
	}
	return usWednesday
}

// dayOf is the weekday whose trading period an answer at now carries.
func dayOf(now time.Time) time.Time {
	d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if now.Before(d.Add(5 * time.Hour)) {
		d = d.Add(-24 * time.Hour)
	}
	return d
}

// lastAt is the time of the last price an answer at now carries for a
// session: now while it trades, its end after it, and before it the end of
// the session before (a day earlier, as these tests need).
func lastAt(s market.Session, now time.Time, lag time.Duration, before time.Time) time.Time {
	switch {
	case now.Before(s.Start):
		return before
	case now.Before(s.End):
		return now.Add(-lag)
	default:
		return s.End
	}
}

// update makes the stand-in's quote for in what the source answers at now.
func (h *holidayWeek) update(in market.Instrument, now time.Time) {
	us := h.usSessionAt(now)
	b3 := b3On(dayOf(now))
	fx := fxOn(dayOf(now))
	usAt := lastAt(us, now, time.Second, usWednesday.End)
	b3At := lastAt(b3, now, 15*time.Minute, b3.End.Add(-24*time.Hour))
	switch in {
	case petr4:
		h.src.answer(in, quote("Petrobras PN", "BRL", 38.42, 38.11, b3, b3At))
	case vale3:
		h.src.answer(in, quote("Vale ON", "BRL", 61.07, 61.07, b3, b3At))
	case ibov:
		h.src.answer(in, quote("Ibovespa", "", 158432.10, 157919.80, b3, b3At))
	case aapl:
		h.src.answer(in, quote("Apple Inc.", "USD", 229.87, 230.99, us, usAt))
	case msft:
		h.src.answer(in, quote("Microsoft Corp", "USD", 512.30, 510.25, us, usAt))
	case sp500:
		h.src.answer(in, quote("S&P 500", "", 6812.40, 6819.90, us, usAt))
	case usdbrl:
		h.src.answer(in, quote("USD/BRL", "BRL", 5.4321, 5.4278, fx, lastAt(fx, now, 10*time.Second, fx.End.Add(-24*time.Hour))))
	}
}

func (h *holidayWeek) updateAll(now time.Time) {
	for _, in := range []market.Instrument{petr4, vale3, ibov, aapl, msft, sp500, usdbrl} {
		h.update(in, now)
	}
}

const holidayConfig = `tickers = ["PETR4", "AAPL", "MSFT"]` + "\n"

// holidayWorld is a world in Thanksgiving week whose cache was filled by a
// print on Wednesday at 22:00 UTC, after both markets closed, and whose
// clock then stands at start.
func holidayWorld(t *testing.T, start time.Time) (*world, *holidayWeek) {
	t.Helper()
	primeAt := wednesday.Add(22 * time.Hour)
	w := clockWorld(t, primeAt, holidayConfig)
	h := &holidayWeek{src: w.src}
	h.updateAll(primeAt)
	w.prime(petr4, aapl, msft)
	w.now = start
	h.updateAll(start)
	return w, h
}

// openHoliday opens the view on the world's clock with the stand-in
// answering as the holiday week says.
func openHoliday(t *testing.T, w *world, h *holidayWeek) *clockRun {
	t.Helper()
	v := openOnClock(t, w)
	v.src.setBefore(func(in market.Instrument, now time.Time) { h.update(in, now) })
	return v
}

// elapseTo lets the view's clock run to at.
func (r *clockRun) elapseTo(at time.Time) drawn {
	r.t.Helper()
	return r.elapse(at.Sub(r.clock.Now()))
}

// lastTableLine is the last line of a print's table.
func lastTableLine(out string) string {
	ls := lines(out)
	return ls[len(ls)-1]
}

// ---------------------------------------------------------------------------
// S5 - Automatic refresh, and markets that are closed
// ---------------------------------------------------------------------------

// S5.9 Given a weekday that is a holiday for a market, so that the source's
// answer at the market's usual open still carries the last session, when the
// view is open during that market's usual hours, then the market is shown as
// closed. (D15, D112, F43)
func TestS5_9_View_HolidayShownClosed(t *testing.T) {
	// Given: Thursday, Thanksgiving; the view opens at 14:00 UTC, before the
	// usual US open (14:30)
	w, h := holidayWorld(t, thursday.Add(14*time.Hour))
	v := openHoliday(t, w, h)
	v.settled(viewGuard)

	// When and then: through the usual hours
	for _, at := range []time.Time{usualOpenThu.Add(time.Minute), usualOpenThu.Add(6 * time.Minute), thursday.Add(17 * time.Hour), usualCloseThu.Add(-time.Minute)} {
		f := v.elapseTo(at)
		if st := marketStates(t, f); !strings.Contains(st, "US closed") {
			t.Errorf("at %v on the US holiday the status line reads %q, want the US market shown closed", at.UTC(), st)
		}
	}
}

// S5.9 on print: the last line of the table gives the market states.
func TestS5_9_Print_HolidayShownClosed(t *testing.T) {
	// Given: Thursday 17:00 UTC, during the usual US hours of a holiday
	w, _ := holidayWorld(t, thursday.Add(17*time.Hour))

	// When
	r := w.runOK("print")

	// Then
	if last := lastTableLine(r.stdout); !strings.Contains(last, "US closed") {
		t.Errorf("on the US holiday the last line reads %q, want the US market shown closed", last)
	}
}

// s511Quotes is what the source answers on Monday before the US session: the
// trading period has moved to Monday's session (F43), not begun; the last
// regular price is Friday's close.
func s511Quotes(src *fakeSource) {
	loadQuotes(src, onlyB3Open, false)
	src.answer(aapl, quote("Apple Inc.", "USD", 229.87, 230.99, usSession, usFridaySession.End))
	src.answer(sp500, quote("S&P 500", "", 6812.40, 6819.90, usSession, usFridaySession.End))
}

// S5.11 Given the US market before its regular session, when the view is
// drawn, then US rows show the last regular close, not a pre-market price.
// (D50)
func TestS5_11_View_BeforeTheSessionShowsTheLastClose(t *testing.T) {
	// Given: Monday 13:30 UTC, an hour before the US session
	w := newWorld(t, onlyB3Open, `tickers = ["PETR4", "AAPL"]`+"\n")
	s511Quotes(w.src)

	// When
	f := openView(t, w, viewOpts{}).settled()

	// Then
	if st := marketStates(t, f); !strings.Contains(st, "US closed") {
		t.Errorf("before the US session the status line reads %q, want the US market closed", st)
	}
	assertRowHas(t, f.text(), "AAPL", "US$ 229.87", wantTime(usFridaySession.End, onlyB3Open))
}

// S5.11 on print.
func TestS5_11_Print_BeforeTheSessionShowsTheLastClose(t *testing.T) {
	w := newWorld(t, onlyB3Open, `tickers = ["PETR4", "AAPL"]`+"\n")
	s511Quotes(w.src)
	r := w.runOK("print")
	if last := lastTableLine(r.stdout); !strings.Contains(last, "US closed") {
		t.Errorf("before the US session the last line reads %q, want the US market closed", last)
	}
	line := rowLine(t, r.stdout, "AAPL")
	for _, want := range []string{"US$ 229.87", wantTime(usFridaySession.End, onlyB3Open)} {
		if !strings.Contains(line, want) {
			t.Errorf("row AAPL does not show %q, the last regular close: %q", want, line)
		}
	}
}

// S5.13 Given B3's trading period ended more than 100 minutes ago and every
// B3 row has a quote, when the view stays open for 10 minutes, then no B3
// row is requested. (D15, D111)
func TestS5_13_View_B3AfterItsSettleTimeIsNotRequested(t *testing.T) {
	// Given: Monday; a print at 19:50 UTC, B3 open, fetched every row; the
	// B3 trading period in the source's answer ended at 20:00; the view
	// opens at 21:41, 101 minutes after
	const config = `tickers = ["PETR4", "VALE3", "AAPL"]` + "\n"
	w := clockWorld(t, b3Session.End.Add(-10*time.Minute), config)
	loadQuotes(w.src, w.now, true)
	w.prime(petr4, vale3)
	w.now = b3Session.End.Add(101 * time.Minute)
	v := openOnClock(t, w)
	f := v.settled(viewGuard).text()
	for _, tk := range []string{"PETR4", "VALE3"} {
		if line := rowLine(t, f, tk); !strings.Contains(line, "R$") {
			t.Fatalf("given not established: B3 row %s has no quote: %q", tk, line)
		}
	}

	// When
	clockAt(t, v.elapse(10*time.Minute), b3Session.End.Add(111*time.Minute))

	// Then
	for _, in := range []market.Instrument{petr4, vale3} {
		if at := v.src.times(in); len(at) != 0 {
			t.Errorf("B3 row %s, its trading period ended more than 100 minutes ago, was requested %d times (at %v), want none", in.Symbol, len(at), at)
		}
	}
}

// S5.14 Given B3's trading period ended less than 100 minutes ago, when the
// view is drawn, then the status line shows B3 as closed. (D111)
func TestS5_14_View_B3ShownClosedDuringItsSettleTime(t *testing.T) {
	for _, after := range []time.Duration{time.Minute, 99 * time.Minute} {
		t.Run(after.String(), func(t *testing.T) {
			// Given: Monday; B3's trading period ended at 20:00 UTC
			start := b3Session.End.Add(after)
			w := clockWorld(t, start, plainConfig)
			loadQuotes(w.src, b3Session.End, true)

			// When
			f := openOnClock(t, w).settled(viewGuard)

			// Then
			if st := marketStates(t, f); !strings.Contains(st, "B3 closed") {
				t.Errorf("%v after B3's trading period ended, the status line reads %q, want B3 shown closed", after, st)
			}
		})
	}
}

// S5.15 Given a weekday that is a holiday for a market, when the view stays
// open from before that market's usual open until its usual close, then
// each row of that market is requested at most twice. (D112)
func TestS5_15_View_HolidayCostsAtMostTwoRequests(t *testing.T) {
	// Given: Thursday, Thanksgiving; the view opens at 14:00 UTC
	w, h := holidayWorld(t, thursday.Add(14*time.Hour))
	v := openHoliday(t, w, h)
	v.settled(viewGuard)

	// When: until the usual US close, 21:00 UTC
	clockAt(t, v.elapseTo(usualCloseThu), usualCloseThu)

	// Then
	for _, in := range []market.Instrument{aapl, msft} {
		if at := v.src.times(in); len(at) > 2 {
			t.Errorf("US row %s was requested %d times on the holiday, want at most 2 (at %v)", in.Symbol, len(at), at)
		}
	}
}

// S5.16 Given the source's answer at a market's usual open still carries the
// last session, and its answer 5 minutes later carries that day's session,
// when that second answer arrives, then the market is shown as open. (D112)
func TestS5_16_View_LateOpenShownOpenOnTheSecondCheck(t *testing.T) {
	// Given: Thursday, not a holiday this time but a late start: the source
	// answers with Wednesday's session at the usual open (14:30 UTC), and
	// from 14:35 with Thursday's, which began at 14:35
	w, h := holidayWorld(t, thursday.Add(14*time.Hour))
	h.mu.Lock()
	h.usFrom = usualOpenThu.Add(5 * time.Minute)
	h.sessionLate = market.Session{Start: usualOpenThu.Add(5 * time.Minute), End: usualCloseThu, TZ: "America/New_York"}
	h.mu.Unlock()
	v := openHoliday(t, w, h)
	v.settled(viewGuard)
	v.elapseTo(usualOpenThu.Add(time.Minute))
	if v.src.requestedFrom(aapl, usualOpenThu, usualOpenThu.Add(time.Minute)) == 0 {
		t.Fatalf("given not established: no answer was asked for at the usual open (AAPL at %v)", v.src.times(aapl))
	}

	// When: the second answer, 5 minutes after the usual open
	f := v.elapseTo(usualOpenThu.Add(5*time.Minute + time.Second))
	second := v.src.requestedFrom(aapl, usualOpenThu.Add(5*time.Minute), usualOpenThu.Add(5*time.Minute+time.Second))
	if second == 0 {
		t.Fatalf("given not established: no second answer was asked for 5 minutes after the usual open (AAPL at %v)", v.src.times(aapl))
	}

	// Then
	if st := marketStates(t, f); !strings.Contains(st, "US open") {
		t.Errorf("after the second answer, which carries that day's session, the status line reads %q, want the US market open", st)
	}
}

// S5.17 Given a weekday that was a holiday for a market, when the view stays
// open until the next weekday's usual open, then that market's rows are
// requested within one refresh interval of that open. (D14, D112)
func TestS5_17_View_AfterTheHolidayRowsRequestedAtTheNextOpen(t *testing.T) {
	// Given: Thursday, Thanksgiving; the view opens at 14:00 UTC; the
	// default interval, 60 seconds
	w, h := holidayWorld(t, thursday.Add(14*time.Hour))
	v := openHoliday(t, w, h)
	v.settled(viewGuard)
	f := v.elapseTo(usualCloseThu)
	if st := marketStates(t, f); !strings.Contains(st, "US closed") {
		t.Fatalf("given not established: the holiday is not shown closed: %q", st)
	}

	// When: until one interval after Friday's usual open, 14:30 UTC
	open := usualOpenThu.Add(24 * time.Hour)
	v.elapseTo(open.Add(60 * time.Second))

	// Then
	for _, in := range []market.Instrument{aapl, msft} {
		if v.src.requestedFrom(in, open, open.Add(60*time.Second)) == 0 {
			t.Errorf("US row %s was not requested within one interval of Friday's usual open (%v); requested at %v", in.Symbol, open.UTC(), v.src.times(in))
		}
	}
}

// ---------------------------------------------------------------------------
// S5.5 and S5.7 shown again with a holiday in the way
// ---------------------------------------------------------------------------

// S5.5 Given the US market is closed and B3 is open, when the view is drawn,
// then the status line reads `B3 open · US closed`. (D15, D47)
func TestS5_5_Holiday_StatusLineB3OpenUSClosed(t *testing.T) {
	// Given: Thursday, Thanksgiving, 17:00 UTC: B3 trades, the US does not
	w, h := holidayWorld(t, thursday.Add(14*time.Hour))
	v := openHoliday(t, w, h)
	v.settled(viewGuard)

	// When
	f := v.elapseTo(thursday.Add(17 * time.Hour))

	// Then
	if st := marketStates(t, f); !strings.Contains(st, "B3 open · US closed") {
		t.Errorf("on the US holiday, B3 open, the status line reads %q, want `B3 open · US closed`", st)
	}
}

// S5.7 Given a closed market and an open view, when the market's next
// session starts, then its rows are requested within one refresh interval
// without a key press. (D14, D15, D50)
func TestS5_7_Holiday_NextSessionAfterTheHoliday(t *testing.T) {
	// Given: the view opens on Thursday, Thanksgiving, at 21:30 UTC, after
	// the usual US close; the US market is closed and its next session is
	// Friday's
	w, h := holidayWorld(t, usualCloseThu.Add(30*time.Minute))
	v := openHoliday(t, w, h)
	if st := marketStates(t, v.settled(viewGuard)); !strings.Contains(st, "US closed") {
		t.Fatalf("given not established: the US market is not shown closed: %q", st)
	}

	// When: no key, until one interval after Friday's session starts
	v.elapseTo(usFriday.Start.Add(60 * time.Second))

	// Then
	for _, in := range []market.Instrument{aapl, msft} {
		if v.src.requestedFrom(in, usFriday.Start, usFriday.Start.Add(60*time.Second)) == 0 {
			t.Errorf("US row %s was not requested within one interval of Friday's session start (%v); requested at %v", in.Symbol, usFriday.Start.UTC(), v.src.times(in))
		}
	}
}
