package app_test

// Acceptance tests, increment 2 of the one-shot slice: the criteria tagged
// `print` that increment 1 did not cover (research/build/one-shot/scope.md).
// Written from docs/spec.md and the boundary alone. Criteria worded for the
// full-screen view are read on `brl-stocks print` as spec section 8's table
// says. They reuse the stand-in source, the timer, the world and the output
// helpers of print_acceptance_test.go and print_shared_acceptance_test.go.

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"math"
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

// runVia calls the tool once with the given source, clock and timer, the
// world's environment, and the output piped.
func (w *world) runVia(p provider.Provider, now func() time.Time, after func(time.Duration) <-chan time.Time, args ...string) result {
	w.t.Helper()
	var out, errOut bytes.Buffer
	code := app.Run(context.Background(), args, deps.Deps{
		Provider: p,
		Now:      now,
		After:    after,
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

// recSource is the stand-in source that also records when each request came,
// by the clock it is given.
type recSource struct {
	*fakeSource
	now func() time.Time
	mu  sync.Mutex
	at  map[market.Instrument][]time.Time
}

func newRecSource(f *fakeSource, now func() time.Time) *recSource {
	return &recSource{fakeSource: f, now: now, at: map[market.Instrument][]time.Time{}}
}

func (r *recSource) Quote(ctx context.Context, in market.Instrument) (market.Quote, error) {
	r.mu.Lock()
	r.at[in] = append(r.at[in], r.now())
	r.mu.Unlock()
	return r.fakeSource.Quote(ctx, in)
}

func (r *recSource) times(in market.Instrument) []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Time(nil), r.at[in]...)
}

// hasToken reports whether one whitespace-separated word of line is tok.
func hasToken(line, tok string) bool {
	for _, f := range strings.Fields(line) {
		if f == tok {
			return true
		}
	}
	return false
}

// staleMark is the mark of 7.3: `!` and the age, as `!45s`, `!12m`, `!3h`, `!2d`.
var staleMark = regexp.MustCompile(`!\d+[smhd]`)

// causeLines returns the lines of the error stream that give one of the four
// cause phrases of 7.3.
func causeLines(stderr string) []string {
	var got []string
	for _, l := range lines(stderr) {
		for _, p := range causePhrases {
			if strings.Contains(l, p) {
				got = append(got, l)
				break
			}
		}
	}
	return got
}

// nonBlank returns the lines of out that are not blank.
func nonBlank(out string) []string {
	var got []string
	for _, l := range lines(out) {
		if strings.TrimSpace(l) != "" {
			got = append(got, l)
		}
	}
	return got
}

// hhmm is a time as the machine's local `HH:MM` (7.3).
func hhmm(t time.Time) string { return t.In(time.Local).Format("15:04") }

// assertNoData checks that a row shows its ticker and `—` in every data
// column of the table: name, last price, change value, change percent, low,
// high and quote time (7.3, 7.4).
func assertNoData(t *testing.T, out, ticker string) {
	t.Helper()
	f := strings.Fields(rowLine(t, out, ticker))
	rest := f[1:]
	if len(rest) != 7 {
		t.Errorf("row %s has %d columns after the ticker, want `—` in each of the 7 data columns: %q", ticker, len(rest), f)
		return
	}
	for _, c := range rest {
		if c != "—" {
			t.Errorf("row %s shows %q in a data column, want `—`: %q", ticker, c, f)
			return
		}
	}
}

// bubbleWorld must be called inside synctest.Test: it moves the bubble's
// clock to Monday 3 January 2000, 15:00 UTC, and sets up a world with
// plainConfig whose markets are all open then.
func bubbleWorld(t *testing.T) (*world, *recSource) {
	t.Helper()
	time.Sleep(time.Until(time.Date(2000, 1, 3, 15, 0, 0, 0, time.UTC)))
	w := newWorld(t, time.Now(), plainConfig)
	loadQuotesOpenAt(w.src, time.Now())
	return w, newRecSource(w.src, time.Now)
}

func (w *world) runBubble(p provider.Provider, args ...string) result {
	w.t.Helper()
	r := w.runVia(p, time.Now, time.After, args...)
	if r.code != 0 {
		w.t.Fatalf("brl-stocks %s: exit status %d, want 0\nstderr: %s", strings.Join(args, " "), r.code, r.stderr)
	}
	return r
}

// firstAtOrAfter returns the earliest of the times that is not before start.
func firstAtOrAfter(start time.Time, ts ...[]time.Time) (time.Time, bool) {
	var first time.Time
	found := false
	for _, list := range ts {
		for _, at := range list {
			if !at.Before(start) && (!found || at.Before(first)) {
				first, found = at, true
			}
		}
	}
	return first, found
}

func near(a, b float64) bool { return math.Abs(a-b) <= 1e-9*math.Max(1, math.Abs(b)) }

// ---------------------------------------------------------------------------
// S1 - Row columns and number formats
// ---------------------------------------------------------------------------

// S1.2 [view, print] Given a row whose fetch succeeded, when the view is drawn
// at full width, then the row shows ticker, name, last price, day change as a
// value, day change as a percent, day low, day high and quote time. (D9)
// On print: `brl-stocks print` is run; the table drops no column (7.4).
func TestS1_2_RowShowsEveryColumn(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	at := w.now.Add(-15 * time.Minute)
	q := quote("Petrobras PN", "BRL", 38.42, 38.11, b3Session, at)
	q.Low, q.High = 38.02, 38.60
	w.src.answer(petr4, q)

	// When
	r := w.runOK("print")

	// Then
	line := rowLine(t, r.stdout, "PETR4")
	for what, text := range map[string]string{
		"name": "Petrobras PN", "last price": "R$ 38,42", "change value": "+0,31",
		"change percent": "+0,81%", "day low": "38,02", "day high": "38,60", "quote time": hhmm(at),
	} {
		if !strings.Contains(line, text) {
			t.Errorf("row PETR4 does not show its %s %q: %q", what, text, line)
		}
	}
}

// S1.5 [view, print] Given no cache, when the view starts, then each row shows
// its ticker and `—` in the data columns until its first quote arrives. (D50)
// On print: `brl-stocks print` is run with no cache and no quote arrives
// during the call.
func TestS1_5_NoQuoteYetShowsDashes(t *testing.T) {
	// Given: no cache, and a source that answers nothing
	w := newWorld(t, bothOpen, plainConfig)
	w.src.failAll(provider.Unavailable)

	// When
	r := w.runOK("print")

	// Then
	for _, tk := range []string{"PETR4", "VALE3", "AAPL"} {
		assertNoData(t, r.stdout, tk)
	}
}

// S1.7 [view, print] Given a quote from an earlier day, when the row is drawn,
// then the quote time is shown as a date (`DD/MM`). (D50, D75)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS1_7_QuoteFromAnEarlierDayShowsADate(t *testing.T) {
	// Given: the US market closed since Friday; AAPL's quote is Friday's close
	w := newWorld(t, onlyB3Open, plainConfig)
	loadQuotes(w.src, w.now, false)

	// When
	r := w.runOK("print")

	// Then
	want := usFridaySession.End.In(time.Local).Format("02/01")
	if line := rowLine(t, r.stdout, "AAPL"); !hasToken(line, want) {
		t.Errorf("row AAPL does not show the date of its quote %q: %q", want, line)
	}
}

// formatCase runs print with one quote of the source changed and returns the
// line of the row.
func formatCase(t *testing.T, in market.Instrument, change func(q *market.Quote)) string {
	t.Helper()
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.src.mu.Lock()
	q := w.src.quotes[in]
	change(&q)
	w.src.quotes[in] = q
	w.src.mu.Unlock()
	r := w.runOK("print")
	return rowLine(t, r.stdout, in.Symbol)
}

// S1.8 [view, print] Given a B3 row whose last price is 1038.42, when the row
// is drawn, then the price reads `R$ 1.038,42`. (D46)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS1_8_B3PriceWithThousands(t *testing.T) {
	line := formatCase(t, petr4, func(q *market.Quote) { q.Price, q.PrevClose, q.Low, q.High = 1038.42, 1030, 1020, 1040 })
	if !strings.Contains(line, "R$ 1.038,42") {
		t.Errorf("row PETR4 does not show R$ 1.038,42: %q", line)
	}
}

// S1.9 [view, print] Given a US row whose last price is 1229.87, when the row
// is drawn, then the price reads `US$ 1,229.87`. (D46)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS1_9_USPriceWithThousands(t *testing.T) {
	line := formatCase(t, aapl, func(q *market.Quote) { q.Price, q.PrevClose, q.Low, q.High = 1229.87, 1220, 1210, 1240 })
	if !strings.Contains(line, "US$ 1,229.87") {
		t.Errorf("row AAPL does not show US$ 1,229.87: %q", line)
	}
}

// S1.10 [view, print] Given a B3 row whose day change is +0.31, when the row is
// drawn, then the change value reads `+0,31`. (D46, D50, D65)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS1_10_B3ChangeValue(t *testing.T) {
	line := formatCase(t, petr4, func(q *market.Quote) { q.Price, q.PrevClose = 38.42, 38.11 })
	if !hasToken(line, "+0,31") {
		t.Errorf("row PETR4 does not show the change +0,31: %q", line)
	}
}

// S1.11 [view, print] Given a B3 row whose day change is +0.81 percent, when
// the row is drawn, then the percent reads `+0,81%`. (D50, D64, D65)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS1_11_B3ChangePercent(t *testing.T) {
	// 38.42 against a close of 38.11 is +0.8134 percent
	line := formatCase(t, petr4, func(q *market.Quote) { q.Price, q.PrevClose = 38.42, 38.11 })
	if !hasToken(line, "+0,81%") {
		t.Errorf("row PETR4 does not show the percent +0,81%%: %q", line)
	}
}

// S1.12 [view, print] Given a US row whose day change is -0.48 percent, when
// the row is drawn, then the percent reads `-0.48%`. (D64, D65, D66)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS1_12_USChangePercent(t *testing.T) {
	// 229.87 against a close of 230.99 is -0.4849 percent
	line := formatCase(t, aapl, func(q *market.Quote) { q.Price, q.PrevClose = 229.87, 230.99 })
	if !hasToken(line, "-0.48%") {
		t.Errorf("row AAPL does not show the percent -0.48%%: %q", line)
	}
}

// S1.13 [view, print] Given a B3 row whose day change is exactly zero, when the
// row is drawn, then the change value reads `+0,00`. (D50, D66)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS1_13_B3ZeroChange(t *testing.T) {
	line := formatCase(t, vale3, func(q *market.Quote) { q.Price, q.PrevClose = 61.07, 61.07 })
	if !hasToken(line, "+0,00") {
		t.Errorf("row VALE3 does not show the change +0,00: %q", line)
	}
}

// S1.14 [view, print] Given a B3 row whose last price is 38.425, when the row
// is drawn, then the price reads `R$ 38,43`. (D46, D66)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS1_14_B3PriceRoundedHalfAwayFromZero(t *testing.T) {
	line := formatCase(t, petr4, func(q *market.Quote) { q.Price = 38.425 })
	if !strings.Contains(line, "R$ 38,43") {
		t.Errorf("row PETR4 does not show R$ 38,43: %q", line)
	}
}

// S1.15 [view, print] Given a B3 row whose day low is 38.02, when the row is
// drawn, then the low reads `38,02`, with no currency symbol. (D46, D66)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS1_15_B3LowWithoutCurrencySymbol(t *testing.T) {
	line := formatCase(t, petr4, func(q *market.Quote) { q.Price, q.Low, q.High = 38.42, 38.02, 38.60 })
	f := strings.Fields(line)
	i := -1
	for j, w := range f {
		if w == "38,02" {
			i = j
		}
	}
	if i < 0 {
		t.Fatalf("row PETR4 does not show the low 38,02: %q", line)
	}
	if prev := f[i-1]; prev == "R$" || prev == "US$" {
		t.Errorf("the low 38,02 carries the currency symbol %s: %q", prev, line)
	}
}

// ---------------------------------------------------------------------------
// S2 - Summary line
// ---------------------------------------------------------------------------

// S2.1 [view, print] Given a config without a `summary` key, when the view is
// drawn, then a line above the watchlist shows Ibovespa, S&P 500 and USD/BRL,
// each with its value and day change percent. (D11, D50)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS2_1_DefaultSummaryLine(t *testing.T) {
	// Given: IBOV 158432.10 (+0.32%), S&P 500 6812.40 (-0.11%), USD/BRL 5.4321 (+0.08%)
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then: one line above the rows holds the three entries (labels of 7.3)
	idx := -1
	for i, l := range lines(r.stdout) {
		if strings.Contains(l, "IBOV") && strings.Contains(l, "S&P 500") && strings.Contains(l, "USD/BRL") {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("no line shows IBOV, S&P 500 and USD/BRL:\n%s", r.stdout)
	}
	if first := rowIndex(t, r.stdout, "PETR4"); idx >= first {
		t.Errorf("the summary line is not above the watchlist:\n%s", r.stdout)
	}
	line := lines(r.stdout)[idx]
	for _, tok := range []string{"158.432", "+0,32%", "6,812.40", "-0.11%", "5,4321", "+0,08%"} {
		if !hasToken(line, tok) {
			t.Errorf("the summary line does not show %q: %q", tok, line)
		}
	}
}

// S2.2 [view, print] Given `summary = ["USDBRL"]`, when the view is drawn, then
// the summary line shows only USD/BRL. (D11, D42)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS2_2_SummaryOfOnlyUSDBRL(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, "summary = [\"USDBRL\"]\n"+plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then
	if !strings.Contains(r.stdout, "USD/BRL") || !strings.Contains(r.stdout, "5,4321") {
		t.Errorf("the summary line does not show USD/BRL 5,4321:\n%s", r.stdout)
	}
	for _, other := range []string{"IBOV", "S&P 500", "158.432", "6,812.40"} {
		if strings.Contains(r.stdout, other) {
			t.Errorf("the output shows %q, not in the summary:\n%s", other, r.stdout)
		}
	}
}

// S2.3 [view, print] Given `summary = []`, when the view is drawn, then there
// is no summary line. (D42)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS2_3_EmptySummaryHidesTheLine(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, "summary = []\n"+plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then
	rowLine(t, r.stdout, "PETR4") // the table was printed
	for _, entry := range []string{"IBOV", "S&P 500", "USD/BRL", "158.432", "6,812.40", "5,4321"} {
		if strings.Contains(r.stdout, entry) {
			t.Errorf("the output shows %q with summary = []:\n%s", entry, r.stdout)
		}
	}
}

// summaryCase runs print with the default summary and one summary value
// changed, and returns the first line of the output.
func summaryCase(t *testing.T, in market.Instrument, change func(q *market.Quote)) string {
	t.Helper()
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.src.mu.Lock()
	q := w.src.quotes[in]
	change(&q)
	w.src.quotes[in] = q
	w.src.mu.Unlock()
	r := w.runOK("print")
	return lines(r.stdout)[0]
}

// S2.5 [view, print] Given an Ibovespa value of 158432.10, when the summary
// line is drawn, then the Ibovespa value reads `158.432`. (D51, D65)
// On print: `brl-stocks print` is run; the summary line is the table's first line.
func TestS2_5_IbovespaValue(t *testing.T) {
	line := summaryCase(t, ibov, func(q *market.Quote) { q.Price = 158432.10 })
	if !hasToken(line, "158.432") {
		t.Errorf("the summary line does not show the Ibovespa as 158.432: %q", line)
	}
}

// S2.6 [view, print] Given an S&P 500 value of 6812.4, when the summary line is
// drawn, then the S&P 500 value reads `6,812.40`. (D51, D65)
// On print: `brl-stocks print` is run; the summary line is the table's first line.
func TestS2_6_SP500Value(t *testing.T) {
	line := summaryCase(t, sp500, func(q *market.Quote) { q.Price = 6812.4 })
	if !hasToken(line, "6,812.40") {
		t.Errorf("the summary line does not show the S&P 500 as 6,812.40: %q", line)
	}
}

// S2.7 [view, print] Given a USD/BRL rate of 5.4321, when the summary line is
// drawn, then the USD/BRL value reads `5,4321`. (D50, D51)
// On print: `brl-stocks print` is run; the summary line is the table's first line.
func TestS2_7_USDBRLValue(t *testing.T) {
	line := summaryCase(t, usdbrl, func(q *market.Quote) { q.Price = 5.4321 })
	if !hasToken(line, "5,4321") {
		t.Errorf("the summary line does not show USD/BRL as 5,4321: %q", line)
	}
}

// S2.8 [view, print] Given an Ibovespa day change of +0.32 percent, when the
// summary line is drawn, then its percent reads `+0,32%`. (D50, D51, D65)
// On print: `brl-stocks print` is run; the summary line is the table's first line.
func TestS2_8_IbovespaPercent(t *testing.T) {
	// 158432.10 against a close of 157919.80 is +0.3244 percent; the other
	// entries are -0.11% and +0.08%, so the token is the Ibovespa's
	line := summaryCase(t, ibov, func(q *market.Quote) { q.Price, q.PrevClose = 158432.10, 157919.80 })
	if !hasToken(line, "+0,32%") {
		t.Errorf("the summary line does not show the Ibovespa change as +0,32%%: %q", line)
	}
}

// ---------------------------------------------------------------------------
// S3 - Currency
// ---------------------------------------------------------------------------

// S3.6 [view, print] Given a B3 row and a US row on screen, when quotes are
// shown, then no price has been converted to the other currency. (D10)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS3_6_NoPriceIsConverted(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, `tickers = ["PETR4", "AAPL"]`+"\n")
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then: each price is the source's, in its own currency
	if line := rowLine(t, r.stdout, "PETR4"); !strings.Contains(line, "R$ 38,42") {
		t.Errorf("row PETR4 does not show its own price R$ 38,42: %q", line)
	}
	if line := rowLine(t, r.stdout, "AAPL"); !strings.Contains(line, "US$ 229.87") {
		t.Errorf("row AAPL does not show its own price US$ 229.87: %q", line)
	}
}

// ---------------------------------------------------------------------------
// S4 and S9 - Groups and order
// ---------------------------------------------------------------------------

// isRowLine reports whether the first word of line is one of the tickers.
func isRowLine(line string, tickers ...string) bool {
	f := strings.Fields(line)
	if len(f) == 0 {
		return false
	}
	for _, tk := range tickers {
		if f[0] == tk {
			return true
		}
	}
	return false
}

// S4.1 [view, print] Given a config with the groups "Brasil" and "US" in that
// order, when the view is drawn, then each name appears as a header above its
// rows, in that order. (D16, D50)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS4_1_GroupHeadersInFileOrder(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, `[[group]]
name = "Brasil"
tickers = ["PETR4", "VALE3"]

[[group]]
name = "US"
tickers = ["AAPL", "MSFT"]
`)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then
	brasil, us := lineIndex(t, r.stdout, "Brasil"), lineIndex(t, r.stdout, "US")
	petr, vale := rowIndex(t, r.stdout, "PETR4"), rowIndex(t, r.stdout, "VALE3")
	appl, micr := rowIndex(t, r.stdout, "AAPL"), rowIndex(t, r.stdout, "MSFT")
	if !(brasil < petr && brasil < vale && petr < us && vale < us && us < appl && us < micr) {
		t.Errorf("headers are not Brasil above its rows, then US above its rows:\n%s", r.stdout)
	}
}

// S4.2 [view, print] Given a config with no group, when the view is drawn, then
// no group header appears. (D16, D50)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS4_2_NoGroupNoHeader(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then: 7.4 makes the table the summary line, a header row, the rows,
	// group names and a last line. With no group, the lines that are not
	// rows are those three.
	var other []string
	for _, l := range nonBlank(r.stdout) {
		if !isRowLine(l, "PETR4", "VALE3", "AAPL") {
			other = append(other, l)
		}
	}
	if len(other) != 3 {
		t.Errorf("lines other than rows: %q, want only the summary line, the header row and the last line", other)
	}
}

// S4.3 [view, print] Given tickers outside groups and a group, when the view is
// drawn, then the tickers outside groups come first, with no header.
// (D42, D50)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS4_3_TickersOutsideGroupsComeFirst(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, `tickers = ["AAPL", "MSFT"]

[[group]]
name = "Brasil"
tickers = ["PETR4"]
`)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then: first
	appl, micr := rowIndex(t, r.stdout, "AAPL"), rowIndex(t, r.stdout, "MSFT")
	brasil, petr := lineIndex(t, r.stdout, "Brasil"), rowIndex(t, r.stdout, "PETR4")
	if !(appl < brasil && micr < brasil && brasil < petr) {
		t.Errorf("tickers outside groups do not come before the group:\n%s", r.stdout)
	}
	// Then: with no header. Above them stand only the summary line and the
	// header row (7.4).
	ls := lines(r.stdout)
	var above []string
	for _, l := range ls[:appl] {
		if strings.TrimSpace(l) != "" {
			above = append(above, l)
		}
	}
	if len(above) != 2 {
		t.Errorf("lines above the first row: %q, want only the summary line and the header row", above)
	}
}

// S9.1 [view, print] Given the view has just started, when it is drawn, then
// rows are in the order of the config file. (D31, D42)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS9_1_RowsInFileOrder(t *testing.T) {
	// Given: an order that is neither by ticker nor by day change
	w := newWorld(t, bothOpen, `tickers = ["VALE3", "AAPL", "PETR4", "MSFT"]`+"\n")
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then
	got := strings.Join(rowTickers(r.stdout, "VALE3", "AAPL", "PETR4", "MSFT"), " ")
	if got != "VALE3 AAPL PETR4 MSFT" {
		t.Errorf("rows in the order %q, want the file's order VALE3 AAPL PETR4 MSFT", got)
	}
}

// ---------------------------------------------------------------------------
// S5 - Closed markets, stale marks and the interval floor
// ---------------------------------------------------------------------------

// S5.4 [view, print] Given the US market is closed, when the view is drawn,
// then US rows show the last close. (D15)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS5_4_ClosedUSRowsShowTheLastClose(t *testing.T) {
	// Given: the source reports Friday's session and Friday's close
	w := newWorld(t, onlyB3Open, `tickers = ["PETR4", "AAPL", "MSFT"]`+"\n")
	loadQuotes(w.src, w.now, false)

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

// S5.5 [view, print] Given the US market is closed and B3 is open, when the
// view is drawn, then the status line reads `B3 open · US closed`. (D15, D47)
// On print: the last line of the table reads it.
func TestS5_5_StatusLineB3OpenUSClosed(t *testing.T) {
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

// S5.6 [view, print] Given a closed market whose rows were last fetched, with
// success, two days ago, when the view is drawn, then those rows carry no
// stale mark. (D40)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS5_6_ClosedMarketFetchedTwoDaysAgoIsNotStale(t *testing.T) {
	// Given: Saturday noon, the US closed since Friday; a print fetches the
	// US rows with success. Then two days pass, to Monday noon, before the
	// US session opens.
	saturday := monday.Add(-36 * time.Hour)
	w := newWorld(t, saturday, "summary = []\ntickers = [\"AAPL\", \"MSFT\"]\n")
	loadQuotes(w.src, monday, false)
	w.prime(aapl, msft)
	w.advance(48 * time.Hour)

	// When
	r := w.runOK("print")

	// Then
	for _, tk := range []string{"AAPL", "MSFT"} {
		line := rowLine(t, r.stdout, tk)
		if !strings.Contains(line, "US$") {
			t.Fatalf("given not established: row %s shows no quote: %q", tk, line)
		}
		if strings.Contains(line, "!") {
			t.Errorf("row %s of a closed market, fetched two days ago, carries a stale mark: %q", tk, line)
		}
	}
}

// S5.10 [view, print] Given `refresh_seconds = 3`, when the view runs, then
// rows are requested no more often than every 10 seconds. (D50)
// On print: `brl-stocks print` is called every 2 seconds (scope.md); no row
// is requested twice less than 10 seconds apart.
func TestS5_10_IntervalFloorOfTenSeconds(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, "refresh_seconds = 3\n"+plainConfig)
	loadQuotes(w.src, w.now, true)
	rec := newRecSource(w.src, func() time.Time { return w.now })

	// When: a print every 2 seconds for a minute
	for i := 0; i <= 30; i++ {
		if r := w.runVia(rec, func() time.Time { return w.now }, w.timer.After, "print"); r.code != 0 {
			t.Fatalf("print at +%ds: exit status %d, want 0\nstderr: %s", 2*i, r.code, r.stderr)
		}
		w.advance(2 * time.Second)
	}

	// Then
	for _, in := range plainRows {
		ts := rec.times(in)
		if len(ts) < 2 {
			t.Fatalf("given not established: %s requested %d times in a minute of prints with an open market", in.Symbol, len(ts))
		}
		for i := 1; i < len(ts); i++ {
			if gap := ts[i].Sub(ts[i-1]); gap < 10*time.Second {
				t.Errorf("%s requested twice %v apart, want at least 10s (requests at %v)", in.Symbol, gap, ts)
				break
			}
		}
	}
}

// S5.12 [view, print] Given a row whose last fetch failed before its market
// closed, when the view is drawn after the close, then the row is still marked
// stale. (D40)
// On print: `brl-stocks print` is run after the close, and the table shows the result.
func TestS5_12_FailedBeforeTheCloseStaysStale(t *testing.T) {
	// Given: Monday 20:40 UTC, the US session ends at 21:00. AAPL is fetched,
	// then at 20:45 its fetch fails.
	w := newWorld(t, monday.Add(20*time.Hour+40*time.Minute), "summary = []\ntickers = [\"AAPL\"]\n")
	loadQuotes(w.src, w.now, true)
	w.prime(aapl)
	w.advance(5 * time.Minute)
	w.src.fail(aapl, provider.Unavailable)
	before := w.src.count(aapl)
	w.runOK("print")
	if w.src.count(aapl) == before {
		t.Fatal("given not established: AAPL was not requested with a cache 5 minutes old, so no fetch failed")
	}

	// When: 22:00 UTC, an hour after the close
	w.advance(75 * time.Minute)
	r := w.runOK("print")

	// Then
	if line := rowLine(t, r.stdout, "AAPL"); !staleMark.MatchString(line) {
		t.Errorf("row AAPL, whose fetch failed before the close, is not marked stale after it: %q", line)
	}
}

// ---------------------------------------------------------------------------
// S6 - Failed fetches
// ---------------------------------------------------------------------------

// S6.1 [view, print] Given rows with quotes and an open market, when a refresh
// fails, then the rows still show their last known prices. (D19)
// On print: the fetches of one `brl-stocks print` call fail.
func TestS6_1_FailedRefreshKeepsLastKnownPrices(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(5 * time.Minute)
	w.src.failAll(provider.Unavailable)
	before := w.src.total()

	// When
	r := w.runOK("print")

	// Then
	if w.src.total() == before {
		t.Fatal("given not established: print, with a cache 5 minutes old, sent no request, so no refresh failed")
	}
	for tk, price := range map[string]string{"PETR4": "R$ 38,42", "VALE3": "R$ 61,07", "AAPL": "US$ 229.87"} {
		if line := rowLine(t, r.stdout, tk); !strings.Contains(line, price) {
			t.Errorf("row %s does not show its last known price %s: %q", tk, price, line)
		}
	}
}

// S6.2 [view, print] Given a row whose last fetch failed, when the view is
// drawn, then the row is marked stale with its age. (D19, D40)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS6_2_FailedRowMarkedStaleWithItsAge(t *testing.T) {
	// Given: PETR4 fetched, and five minutes later its fetch fails
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(5 * time.Minute)
	w.src.fail(petr4, provider.Unavailable)

	// When
	r := w.runOK("print")

	// Then: the age since the last successful fetch, as 7.3 writes it
	if line := rowLine(t, r.stdout, "PETR4"); !strings.Contains(line, "!5m") {
		t.Errorf("row PETR4 is not marked stale with its age (!5m): %q", line)
	}
}

// S6.3 [view, print] Given a refresh in which five rows failed, when the view
// is drawn, then exactly one error line is shown. (D19)
// On print: exactly one line on the error stream gives a cause.
func TestS6_3_FiveFailedRowsOneErrorLine(t *testing.T) {
	// Given
	all := []market.Instrument{petr4, vale3, itub4, aapl, msft}
	w := newWorld(t, bothOpen, `tickers = ["PETR4", "VALE3", "ITUB4", "AAPL", "MSFT"]`+"\n")
	loadQuotes(w.src, w.now, true)
	w.prime(all...)
	w.advance(5 * time.Minute)
	before := map[market.Instrument]int{}
	for _, in := range all {
		w.src.fail(in, provider.Unavailable)
		before[in] = w.src.count(in)
	}

	// When
	r := w.runOK("print")

	// Then
	for _, in := range all {
		if w.src.count(in) == before[in] {
			t.Fatalf("given not established: %s was not requested, so it did not fail", in.Symbol)
		}
	}
	if got := causeLines(r.stderr); len(got) != 1 {
		t.Errorf("%d lines of the error stream give a cause, want 1:\n%s", len(got), r.stderr)
	}
}

// staleThenRecovered sets up S6.5 and S6.6: every fetch of a print fails,
// then the source recovers and a later print fetches every row. It returns
// the result of the failed print and of the recovered one.
func staleThenRecovered(t *testing.T) (failed, recovered result) {
	t.Helper()
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(5 * time.Minute)
	w.src.failAll(provider.Unavailable)
	failed = w.runOK("print")
	if !staleMark.MatchString(rowLine(t, failed.stdout, "PETR4")) || len(causeLines(failed.stderr)) == 0 {
		t.Fatalf("given not established: no stale row and error line after a failed print.\nstdout:\n%s\nstderr:\n%s", failed.stdout, failed.stderr)
	}
	w.src.failAll("")
	w.advance(2 * time.Minute)
	before := map[market.Instrument]int{}
	for _, in := range plainRows {
		before[in] = w.src.count(in)
	}
	recovered = w.runOK("print")
	for _, in := range plainRows {
		if w.src.count(in) == before[in] {
			t.Fatalf("when not reached: print, 7 minutes after the last success and 2 after the failure, did not request %s", in.Symbol)
		}
	}
	return failed, recovered
}

// S6.5 [view, print] Given stale rows and an error line, when a refresh
// succeeds for every row, then the stale marks are gone. (D19, D40)
// On print: a first call fails; the fetches of a second call succeed.
func TestS6_5_SuccessfulRefreshRemovesStaleMarks(t *testing.T) {
	_, r := staleThenRecovered(t)
	for _, tk := range []string{"PETR4", "VALE3", "AAPL"} {
		if line := rowLine(t, r.stdout, tk); strings.Contains(line, "!") {
			t.Errorf("row %s is still marked stale after a successful refresh: %q", tk, line)
		}
	}
}

// S6.6 [view, print] Given stale rows and an error line, when a refresh
// succeeds for every row, then the error line is gone. (D19)
// On print: no line on the error stream of the second call gives a cause.
func TestS6_6_SuccessfulRefreshRemovesTheErrorLine(t *testing.T) {
	_, r := staleThenRecovered(t)
	if got := causeLines(r.stderr); len(got) != 0 {
		t.Errorf("the error stream still gives a cause after a successful refresh: %q", got)
	}
}

// S6.8 [view, print] Given a refresh in which one row fails and the others
// succeed, when the view is drawn, then only that row is marked stale. (D40)
// On print: the fetches of one `brl-stocks print` call.
func TestS6_8_OnlyTheFailedRowIsStale(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(5 * time.Minute)
	w.src.fail(petr4, provider.Unavailable)
	before := w.src.count(vale3) + w.src.count(aapl)

	// When
	r := w.runOK("print")

	// Then
	if w.src.count(vale3)+w.src.count(aapl) == before {
		t.Fatal("given not established: the other rows were not refreshed")
	}
	if line := rowLine(t, r.stdout, "PETR4"); !staleMark.MatchString(line) {
		t.Errorf("row PETR4, whose fetch failed, is not marked stale: %q", line)
	}
	for _, tk := range []string{"VALE3", "AAPL"} {
		if line := rowLine(t, r.stdout, tk); strings.Contains(line, "!") {
			t.Errorf("row %s, whose fetch succeeded, is marked stale: %q", tk, line)
		}
	}
}

// S6.9 [view, print] Given a failed refresh, when the error line is drawn, then
// it names the cause, the number of rows not updated and the time of the next
// attempt. (D50, D76)
// On print: the line on the error stream that gives the cause.
func TestS6_9_ErrorLineNamesCauseCountAndNextAttempt(t *testing.T) {
	// Summary entries are not rows and are not counted (7.3, D81).
	cases := map[string]struct {
		fail func(w *world)
		rows string
	}{
		// two of three rows fail; the summary entries answer
		"two rows fail": {func(w *world) {
			w.src.fail(petr4, provider.Unavailable)
			w.src.fail(vale3, provider.Unavailable)
		}, "2"},
		// every fetch fails: the 3 rows and the 3 summary entries
		"every fetch fails": {func(w *world) { w.src.failAll(provider.Unavailable) }, "3"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Given: the fetches fail at 15:05 UTC; the next attempt comes one
			// refresh interval later (6.2), at 15:06
			w := newWorld(t, bothOpen, plainConfig)
			loadQuotes(w.src, w.now, true)
			w.prime(plainRows...)
			w.advance(5 * time.Minute)
			c.fail(w)
			next := w.now.Add(60 * time.Second)

			// When
			r := w.runOK("print")

			// Then
			got := causeLines(r.stderr)
			if len(got) != 1 {
				t.Fatalf("%d lines of the error stream give a cause, want 1:\n%s", len(got), r.stderr)
			}
			line := got[0]
			if !strings.Contains(line, "source unreachable") {
				t.Errorf("the line does not name the cause `source unreachable`: %q", line)
			}
			numbers := regexp.MustCompile(`(^|[^0-9:])([0-9]+)([^0-9:]|$)`).FindAllStringSubmatch(line, -1)
			found := false
			for _, m := range numbers {
				if m[2] != c.rows {
					t.Errorf("the line gives the number %s, want only the number of rows not updated (%s): %q", m[2], c.rows, line)
				} else {
					found = true
				}
			}
			if !found {
				t.Errorf("the line does not give the number of rows not updated (%s): %q", c.rows, line)
			}
			if !strings.Contains(line, hhmm(next)) {
				t.Errorf("the line does not give the time of the next attempt (%s): %q", hhmm(next), line)
			}
		})
	}
}

// S6.11 [view, print] Given every row updates and a summary entry fails, when
// the view is drawn, then the error line says the summary was not updated.
// (D81)
// On print: the line on the error stream that gives a cause says
// `summary not updated` (7.3).
func TestS6_11_OnlySummaryFailedSaysSummaryNotUpdated(t *testing.T) {
	// Given: every row and the summary in the cache; five minutes on, the
	// Ibovespa alone fails
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(5 * time.Minute)
	w.src.fail(ibov, provider.Unavailable)
	before := map[market.Instrument]int{}
	for _, in := range append([]market.Instrument{ibov}, plainRows...) {
		before[in] = w.src.count(in)
	}

	// When
	r := w.runOK("print")

	// Then: the given held (every row updated, the Ibovespa failed)
	for in, n := range before {
		if w.src.count(in) == n {
			t.Fatalf("given not established: %s was not requested", in.Symbol)
		}
	}
	for _, tk := range []string{"PETR4", "VALE3", "AAPL"} {
		if line := rowLine(t, r.stdout, tk); strings.Contains(line, "!") {
			t.Fatalf("given not established: row %s did not update: %q", tk, line)
		}
	}
	got := causeLines(r.stderr)
	if len(got) != 1 {
		t.Fatalf("%d lines of the error stream give a cause, want 1:\n%s", len(got), r.stderr)
	}
	if !strings.Contains(got[0], "summary not updated") {
		t.Errorf("the line giving the cause does not say `summary not updated`: %q", got[0])
	}
}

// S6.10 [view, print] Given a stale row whose last successful fetch was 12
// minutes ago, when the row is drawn, then the mark reads `!12m`. (D50)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS6_10_StaleMarkReadsTwelveMinutes(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(12 * time.Minute)
	w.src.fail(petr4, provider.Unavailable)

	// When
	r := w.runOK("print")

	// Then
	if line := rowLine(t, r.stdout, "PETR4"); !hasToken(line, "!12m") {
		t.Errorf("row PETR4 does not carry the mark !12m: %q", line)
	}
}

// ---------------------------------------------------------------------------
// S7 - Forced fetch (`print --refresh`)
// ---------------------------------------------------------------------------

// S7.1 [view, print] Given B3 is open and the source answers, when `r` is
// pressed, then the first B3 rows are requested within 2 seconds, without
// waiting for the interval. (D32, D62)
// On print: `brl-stocks print --refresh` is run.
func TestS7_1_RefreshRequestsB3RowsWithinTwoSeconds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given: a cache fetched 20 seconds ago, within the interval
		w, rec := bubbleWorld(t)
		w.runBubble(rec, "print")
		if len(rec.times(petr4)) == 0 {
			t.Fatal("given not established: the first print did not fetch PETR4")
		}
		time.Sleep(20 * time.Second)

		// When
		start := time.Now()
		w.runBubble(rec, "print", "--refresh")

		// Then
		first, ok := firstAtOrAfter(start, rec.times(petr4), rec.times(vale3))
		if !ok {
			t.Fatal("print --refresh requested no B3 row")
		}
		if d := first.Sub(start); d > 2*time.Second {
			t.Errorf("first B3 row requested %v after print --refresh started, want within 2s", d)
		}
	})
}

// S7.2 [view, print] Given a network error has put the next automatic attempt
// four minutes away, when `r` is pressed, then the first failed rows are
// requested within 2 seconds. (D38, D62)
// On print: `brl-stocks print --refresh` is run.
func TestS7_2_RefreshRequestsFailedRowsWithinTwoSeconds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given: PETR4 fails three times in a row, so its next automatic
		// attempt is 4 intervals (240 seconds) away (6.2)
		w, rec := bubbleWorld(t)
		w.runBubble(rec, "print")
		w.src.fail(petr4, provider.Unavailable)
		for i, wait := range []time.Duration{5 * time.Minute, 61 * time.Second, 121 * time.Second} {
			time.Sleep(wait)
			n := len(rec.times(petr4))
			w.runBubble(rec, "print")
			if len(rec.times(petr4)) == n {
				t.Fatalf("given not established: failed fetch %d of PETR4 did not happen", i+1)
			}
		}
		// past the 5 seconds within which a forced fetch skips a row (6.5);
		// the next automatic attempt is still about four minutes away
		time.Sleep(10 * time.Second)

		// When
		start := time.Now()
		w.runBubble(rec, "print", "--refresh")

		// Then
		first, ok := firstAtOrAfter(start, rec.times(petr4))
		if !ok {
			t.Fatal("print --refresh did not request the failed row PETR4")
		}
		if d := first.Sub(start); d > 2*time.Second {
			t.Errorf("the failed row was requested %v after print --refresh started, want within 2s", d)
		}
	})
}

// blockedWorld returns a world with a cache, in which a print at 15:05 UTC
// had every request refused with kind, and the time of that refusal.
func blockedWorld(t *testing.T, kind provider.Kind) (*world, time.Time) {
	t.Helper()
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(5 * time.Minute)
	w.src.failAll(kind)
	before := w.src.total()
	refused := w.now
	w.runOK("print")
	if w.src.total() == before {
		t.Fatal("given not established: print, with a cache 5 minutes old, sent no request to be refused")
	}
	return w, refused
}

var refusals = map[provider.Kind]string{
	provider.RateLimited: "source is rate-limiting",
	provider.Blocked:     "source refused access",
}

// S7.3 [view, print] Given the source is rate-limiting or refusing the tool,
// when `r` is pressed, then no request is sent. (D38)
// On print: `brl-stocks print --refresh` is run.
func TestS7_3_RefreshDuringABlockSendsNoRequest(t *testing.T) {
	for kind := range refusals {
		t.Run(string(kind), func(t *testing.T) {
			// Given
			w, _ := blockedWorld(t, kind)
			w.advance(time.Minute)
			before := w.src.total()

			// When
			w.runOK("print", "--refresh")

			// Then
			if got := w.src.total() - before; got != 0 {
				t.Errorf("requests sent during a block: %d, want 0", got)
			}
		})
	}
}

// S7.4 [view, print] Given the source is rate-limiting or refusing the tool,
// when `r` is pressed, then the status line shows the time of the next
// attempt. (D38, D76)
// On print: `brl-stocks print --refresh` is run; the line on the error stream
// that gives the cause shows the time.
func TestS7_4_RefreshDuringABlockShowsTheNextAttempt(t *testing.T) {
	for kind, phrase := range refusals {
		t.Run(string(kind), func(t *testing.T) {
			// Given: refused at 15:05; a block's first wait is 5 minutes (6.3)
			w, refused := blockedWorld(t, kind)
			next := refused.Add(5 * time.Minute)
			w.advance(time.Minute)

			// When
			r := w.runOK("print", "--refresh")

			// Then
			found := false
			for _, l := range causeLines(r.stderr) {
				if strings.Contains(l, phrase) {
					found = true
					if !strings.Contains(l, hhmm(next)) {
						t.Errorf("the line giving the cause does not show the next attempt %s: %q", hhmm(next), l)
					}
				}
			}
			if !found {
				t.Errorf("no line of the error stream gives the cause %q:\n%s", phrase, r.stderr)
			}
		})
	}
}

// S7.6 [view, print] Given a closed market, when `r` is pressed, then its rows
// are requested too. (D50)
// On print: `brl-stocks print --refresh` is run.
func TestS7_6_RefreshRequestsClosedMarketRows(t *testing.T) {
	// Given: the US closed since Friday, its quotes in the cache
	w := newWorld(t, onlyB3Open, `tickers = ["PETR4", "AAPL", "MSFT"]`+"\n")
	loadQuotes(w.src, w.now, false)
	w.prime(petr4, aapl, msft)
	w.advance(5 * time.Minute)
	beforeA, beforeM := w.src.count(aapl), w.src.count(msft)

	// When
	w.runOK("print", "--refresh")

	// Then
	if w.src.count(aapl) == beforeA {
		t.Errorf("AAPL, of the closed US market, was not requested by print --refresh")
	}
	if w.src.count(msft) == beforeM {
		t.Errorf("MSFT, of the closed US market, was not requested by print --refresh")
	}
}

// S7.7 [view, print] Given `r` was pressed two seconds ago, when `r` is pressed
// again, then no row is requested a second time. (D50)
// On print: `brl-stocks print --refresh` is run twice, two seconds apart.
func TestS7_7_SecondRefreshWithinTwoSecondsRequestsNothing(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	w.advance(20 * time.Second)
	before := map[market.Instrument]int{}
	for _, in := range plainRows {
		before[in] = w.src.count(in)
	}
	w.runOK("print", "--refresh")
	for _, in := range plainRows {
		if w.src.count(in) == before[in] {
			t.Fatalf("given not established: the first print --refresh did not request %s", in.Symbol)
		}
		before[in] = w.src.count(in)
	}
	w.advance(2 * time.Second)

	// When
	w.runOK("print", "--refresh")

	// Then
	for _, in := range plainRows {
		if got := w.src.count(in) - before[in]; got != 0 {
			t.Errorf("%s requested %d more times by a second print --refresh two seconds later, want 0", in.Symbol, got)
		}
	}
}

// S7.8 [view, print] Given a not-found row tried ten minutes ago and an
// unchanged config, when `r` is pressed, then that ticker is not requested.
// (D39)
// On print: `brl-stocks print --refresh` is run.
func TestS7_8_RefreshSkipsARecentNotFoundRow(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, notFoundConfig)
	loadQuotes(w.src, w.now, true)
	w.runOK("print")
	if w.src.count(xxxx9) == 0 {
		t.Fatal("given not established: the print with no cache did not request XXXX9")
	}
	w.advance(10 * time.Minute)
	beforeX, beforeP := w.src.count(xxxx9), w.src.count(petr4)

	// When
	w.runOK("print", "--refresh")

	// Then
	if w.src.count(petr4) == beforeP {
		t.Fatal("given not established: print --refresh requested no row at all")
	}
	if got := w.src.count(xxxx9) - beforeX; got != 0 {
		t.Errorf("XXXX9, not found ten minutes ago, requested %d times by print --refresh, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// S8 - Not-found rows
// ---------------------------------------------------------------------------

// S8.1 [view, print] Given the ticker `XXXX9`, which the source does not know,
// when the view is drawn, then its row shows "not found" in place of the
// price. (D39)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS8_1_UnknownTickerShowsNotFound(t *testing.T) {
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
}

// S8.2 [view, print] Given one not-found row among others, when a refresh runs,
// then the other rows show fresh quotes. (D39)
// On print: the fetches of one `brl-stocks print` call.
func TestS8_2_OtherRowsStayFresh(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, `tickers = ["PETR4", "XXXX9", "AAPL"]`+"\n")
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then
	if line := rowLine(t, r.stdout, "XXXX9"); !strings.Contains(line, "not found") {
		t.Fatalf("given not established: row XXXX9 is not shown as not found: %q", line)
	}
	for tk, price := range map[string]string{"PETR4": "R$ 38,42", "AAPL": "US$ 229.87"} {
		line := rowLine(t, r.stdout, tk)
		if !strings.Contains(line, price) {
			t.Errorf("row %s does not show its quote %s: %q", tk, price, line)
		}
		if strings.Contains(line, "!") {
			t.Errorf("row %s is marked stale: %q", tk, line)
		}
	}
}

// S8.4 [view, print] Given a not-found row, when the config file changes, then
// the ticker is requested again without waiting for the hour. (D39)
// On print: a first call finds the row not found; the config's content
// changes; a second call is run.
func TestS8_4_ConfigChangeRetriesNotFound(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, notFoundConfig)
	loadQuotes(w.src, w.now, true)
	w.runOK("print")
	if w.src.count(xxxx9) == 0 {
		t.Fatal("given not established: the print with no cache did not request XXXX9")
	}
	w.advance(10 * time.Minute)
	before := w.src.count(xxxx9)

	// When: the file's content changes
	if err := os.WriteFile(w.configPath, []byte(`tickers = ["XXXX9", "PETR4"]`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.runOK("print")

	// Then
	if w.src.count(xxxx9) == before {
		t.Errorf("XXXX9 was not requested again after the config changed")
	}
}

// S8.5 [view, print] Given a not-found row and no other problem, when the view
// is drawn, then there is no error line. (D50)
// On print: there is no line on the error stream.
func TestS8_5_NotFoundRaisesNoErrorLine(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, notFoundConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print")

	// Then
	if line := rowLine(t, r.stdout, "XXXX9"); !strings.Contains(line, "not found") {
		t.Fatalf("given not established: row XXXX9 is not shown as not found: %q", line)
	}
	if strings.TrimSpace(r.stderr) != "" {
		t.Errorf("the error stream is not empty with only a not-found row:\n%s", r.stderr)
	}
}

// ---------------------------------------------------------------------------
// S11 - More than 20 tickers
// ---------------------------------------------------------------------------

// twentyFive returns 25 B3 tickers, a config listing them, and a world whose
// source knows them all.
func twentyFive(t *testing.T) (*world, []string) {
	t.Helper()
	var tickers, quoted []string
	for c := 'A'; c < 'A'+25; c++ {
		tk := "TST" + string(c) + "3"
		tickers = append(tickers, tk)
		quoted = append(quoted, fmt.Sprintf("%q", tk))
	}
	w := newWorld(t, bothOpen, "tickers = ["+strings.Join(quoted, ", ")+"]\n")
	loadQuotes(w.src, w.now, true)
	for i, tk := range tickers {
		w.src.answer(market.Instrument{Market: market.B3, Symbol: tk},
			quote("Test "+tk, "BRL", 10+float64(i), 10, b3Session, w.now.Add(-15*time.Minute)))
	}
	return w, tickers
}

// S11.13 [view, print] Given a config with 25 tickers, when the view is drawn,
// then all 25 rows exist. (D8, D50)
// On print: `brl-stocks print` is run, and the table shows the result.
func TestS11_13_TwentyFiveRowsAllShown(t *testing.T) {
	// Given
	w, tickers := twentyFive(t)

	// When
	r := w.runOK("print")

	// Then
	for _, tk := range tickers {
		if !hasRow(r.stdout, tk) {
			t.Errorf("row %s is not shown", tk)
		}
	}
}

// S11.18 [view, print] Given a config with 25 tickers, when the view is drawn,
// then a warning in the status line says the tool is designed for 20. (D50)
// On print: a line of its own on the error stream.
func TestS11_18_TwentyFiveRowsWarn(t *testing.T) {
	// Given
	w, _ := twentyFive(t)

	// When
	r := w.runOK("print")

	// Then
	if !regexp.MustCompile(`(^|[^0-9:])20([^0-9:]|$)`).MatchString(r.stderr) {
		t.Errorf("no line of the error stream says the tool is designed for 20:\n%s", r.stderr)
	}
}

// ---------------------------------------------------------------------------
// S14 - JSON
// ---------------------------------------------------------------------------

// S14.2 [print] Given a row with a quote, when `brl-stocks print --json` is
// run, then the row's object carries ticker, name, last price, day change
// value, day change percent, day low, day high and quote time. (D9, D18)
func TestS14_2_RowObjectCarriesTheQuote(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	at := w.now.Add(-15 * time.Minute)
	q := quote("Petrobras PN", "BRL", 38.42, 38.11, b3Session, at)
	q.Low, q.High = 38.02, 38.60
	w.src.answer(petr4, q)

	// When
	r := w.runOK("print", "--json")

	// Then
	row := jsonRow(t, decodeDocument(t, r.stdout), "PETR4")
	if row["name"] != "Petrobras PN" {
		t.Errorf("name is %#v, want \"Petrobras PN\"", row["name"])
	}
	for key, want := range map[string]float64{
		"price": 38.42, "change": 38.42 - 38.11, "change_percent": (38.42 - 38.11) / 38.11 * 100,
		"day_low": 38.02, "day_high": 38.60,
	} {
		got, ok := row[key].(float64)
		if !ok {
			t.Errorf("%s is %#v, want the number %v", key, row[key], want)
		} else if !near(got, want) {
			t.Errorf("%s is %v, want %v", key, got, want)
		}
	}
	s, _ := row["quote_time"].(string)
	if qt, err := time.Parse(time.RFC3339, s); err != nil || !qt.Equal(at) {
		t.Errorf("quote_time is %#v, want the time of the quote %s", row["quote_time"], at.Format(time.RFC3339))
	}
}

// S14.3 [print] Given any run with `--json`, when the document is read, then it
// has a `schema_version` of 1. (D43)
func TestS14_3_SchemaVersionIsOne(t *testing.T) {
	cases := map[string]func(t *testing.T) result{
		"fresh": func(t *testing.T) result {
			w := newWorld(t, bothOpen, plainConfig)
			loadQuotes(w.src, w.now, true)
			return w.run("print", "--json")
		},
		"error": func(t *testing.T) result {
			w := newWorld(t, bothOpen, plainConfig)
			w.src.failAll(provider.Unavailable)
			return w.run("print", "--json")
		},
		"invalid config": func(t *testing.T) result {
			w := newWorld(t, bothOpen, "tickers = [\"PETR4\"\n[[group\n")
			return w.run("print", "--json")
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			doc := decodeDocument(t, run(t).stdout)
			if v, ok := doc["schema_version"].(float64); !ok || v != 1 {
				t.Errorf("schema_version is %#v, want 1", doc["schema_version"])
			}
		})
	}
}

// S14.8 [print] Given a config file that cannot be parsed, when `brl-stocks
// print --json` is run, then the exit status is 2. (D44, D45)
func TestS14_8_UnparsableConfigWithJSONExitsWithStatus2(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, "tickers = [\"PETR4\"\n[[group\n")
	loadQuotes(w.src, w.now, true)

	// When
	r := w.run("print", "--json")

	// Then
	if r.code != 2 {
		t.Errorf("exit status %d, want 2\nstderr: %s", r.code, r.stderr)
	}
}

// S14.9 [print] Given a row with a quote, when the document is read, then its
// prices are JSON numbers. (D43)
func TestS14_9_PricesAreJSONNumbers(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print", "--json")

	// Then
	for _, tk := range []string{"PETR4", "AAPL"} {
		row := jsonRow(t, decodeDocument(t, r.stdout), tk)
		for _, key := range []string{"price", "day_low", "day_high"} {
			if _, ok := row[key].(float64); !ok {
				t.Errorf("%s of %s is %#v, want a JSON number", key, tk, row[key])
			}
		}
	}
}

// S14.10 [print] Given a row with a quote, when the document is read, then its
// times are UTC in RFC 3339. (D43)
func TestS14_10_TimesAreUTCInRFC3339(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.runOK("print", "--json")

	// Then
	row := jsonRow(t, decodeDocument(t, r.stdout), "PETR4")
	for _, key := range []string{"quote_time", "fetched_at"} {
		s, ok := row[key].(string)
		if !ok {
			t.Errorf("%s is %#v, want a time", key, row[key])
			continue
		}
		at, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Errorf("%s is %q, not RFC 3339: %v", key, s, err)
			continue
		}
		if _, off := at.Zone(); off != 0 {
			t.Errorf("%s is %q, not in UTC", key, s)
		}
	}
	if s, _ := row["quote_time"].(string); s != "" {
		if at, err := time.Parse(time.RFC3339, s); err == nil && !at.Equal(w.now.Add(-15*time.Minute)) {
			t.Errorf("quote_time is %q, want the quote's time %s", s, w.now.Add(-15*time.Minute).Format(time.RFC3339))
		}
	}
}

// S14.11 [print] Given a closed market, when `brl-stocks print --json` is run,
// then the rows of that market have `market_state` `closed`. (D15, D43)
func TestS14_11_ClosedMarketRowsAreClosed(t *testing.T) {
	// Given
	w := newWorld(t, onlyB3Open, `tickers = ["PETR4", "AAPL", "MSFT"]`+"\n")
	loadQuotes(w.src, w.now, false)

	// When
	r := w.runOK("print", "--json")

	// Then
	doc := decodeDocument(t, r.stdout)
	for _, tk := range []string{"AAPL", "MSFT"} {
		if got := jsonRow(t, doc, tk)["market_state"]; got != "closed" {
			t.Errorf("market_state of %s is %#v, want \"closed\"", tk, got)
		}
	}
}

// ---------------------------------------------------------------------------
// S15 - Kept files
// ---------------------------------------------------------------------------

// S15.2 [view, print] Given the tool has fetched quotes, when the files it
// keeps are listed, then they are under `~/.cache/brl-stocks/`. (D29)
// On print: `brl-stocks print` is run, then the folders are listed.
func TestS15_2_KeptFilesAreUnderDotCache(t *testing.T) {
	// Given: no XDG_CACHE_HOME, so the cache is under ~/.cache (7.7)
	w := newWorld(t, bothOpen, plainConfig)
	delete(w.env, "XDG_CACHE_HOME")
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)

	// When: every file under HOME and XDG_CONFIG_HOME is listed
	home := w.env["HOME"]
	cache := filepath.Join(home, ".cache", "brl-stocks")
	var kept []string
	err := filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			kept = append(kept, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Then
	if len(kept) == 0 {
		t.Fatalf("no file kept under %s after a print that fetched", home)
	}
	for _, p := range kept {
		if !strings.HasPrefix(p, cache+string(filepath.Separator)) {
			t.Errorf("a file is kept outside %s: %s", cache, p)
		}
	}
	for _, p := range entriesUnder(t, w.env["XDG_CONFIG_HOME"]) {
		if p != w.configPath && p != filepath.Dir(w.configPath) {
			t.Errorf("a file is kept under the config folder: %s", p)
		}
	}
}

// S15.6 [view, print] Given a ticker was removed from the config, a refresh
// then succeeded, and the ticker is added back with no network, when the view
// is drawn, then the row shows no data. (D50)
// On print: three calls; the last, with the ticker back and no network, prints the table.
func TestS15_6_RemovedThenAddedBackShowsNoData(t *testing.T) {
	// Given: VALE3 fetched
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)
	w.prime(plainRows...)
	// VALE3 removed, and a refresh then succeeds
	if err := os.WriteFile(w.configPath, []byte(`tickers = ["PETR4", "AAPL"]`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.advance(5 * time.Minute)
	before := w.src.count(petr4)
	w.runOK("print")
	if w.src.count(petr4) == before {
		t.Fatal("given not established: no refresh ran after VALE3 was removed")
	}
	// VALE3 added back, with no network
	if err := os.WriteFile(w.configPath, []byte(plainConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	w.src.failAll(provider.Unavailable)
	w.advance(time.Minute)

	// When
	r := w.runOK("print")

	// Then
	assertNoData(t, r.stdout, "VALE3")
}

// S15.8 [view, print] Given a cache folder that cannot be written, when the
// tool runs, then one warning says the quotes will not be kept. (D50)
// On print: a line of its own on the error stream.
func TestS15_8_UnwritableCacheWarnsOnce(t *testing.T) {
	// The criterion holds whenever the tool runs (7.7: "the tool still runs
	// and says so once in a warning"), whether or not this call would fetch.
	for name, wait := range map[string]time.Duration{
		"cache old enough to fetch": 5 * time.Minute,
		"cache fresh, no fetch due": 15 * time.Second,
	} {
		t.Run(name, func(t *testing.T) {
			// Given: a cache, and a print with it that warns of nothing
			w := newWorld(t, bothOpen, plainConfig)
			loadQuotes(w.src, w.now, true)
			w.prime(plainRows...)
			w.advance(15 * time.Second)
			if control := w.runOK("print"); strings.TrimSpace(control.stderr) != "" {
				t.Fatalf("given not established: print with a writable cache already writes to the error stream:\n%s", control.stderr)
			}
			w.lockCache()
			w.advance(wait)

			// When
			r := w.runOK("print")

			// Then: one line, other than a cause line, on the error stream
			var warnings []string
			causes := map[string]bool{}
			for _, l := range causeLines(r.stderr) {
				causes[l] = true
			}
			for _, l := range nonBlank(r.stderr) {
				if !causes[l] {
					warnings = append(warnings, l)
				}
			}
			if len(warnings) != 1 {
				t.Errorf("%d warnings on the error stream with a cache folder that cannot be written, want 1: %q", len(warnings), warnings)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// S17 and S18 - Help text and command line
// ---------------------------------------------------------------------------

// S17.4 [view, print] Given the help text, when it is read, then
// `BRL_STOCKS_SOURCE_URL` does not appear in it. (D70)
// On print: the help text of `brl-stocks print --help`.
func TestS17_4_HelpDoesNotNameTheSourceURLVariable(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)

	// When
	r := w.runOK("print", "--help")

	// Then
	if strings.TrimSpace(r.stdout+r.stderr) == "" {
		t.Fatal("given not established: print --help printed nothing")
	}
	if strings.Contains(r.stdout+r.stderr, "BRL_STOCKS_SOURCE_URL") {
		t.Errorf("the help text names BRL_STOCKS_SOURCE_URL:\n%s%s", r.stdout, r.stderr)
	}
}

// S18.1 [view, print] Given `--version`, when the tool is run, then it prints
// the version and exits with status 0. (D50, D77)
// On print: `brl-stocks print --version`.
func TestS18_1_VersionExitsZero(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.run("print", "--version")

	// Then
	if r.code != 0 {
		t.Errorf("exit status %d, want 0\nstderr: %s", r.code, r.stderr)
	}
	if strings.TrimSpace(r.stdout+r.stderr) == "" {
		t.Errorf("print --version printed nothing")
	}
	if hasRow(r.stdout, "PETR4") {
		t.Errorf("print --version printed the table instead of the version:\n%s", r.stdout)
	}
}

// S18.2 [view, print] Given `--help`, when the tool is run, then it prints the
// usage and exits with status 0. (D50, D77)
// On print: `brl-stocks print --help`.
func TestS18_2_HelpExitsZero(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.run("print", "--help")

	// Then
	if r.code != 0 {
		t.Errorf("exit status %d, want 0\nstderr: %s", r.code, r.stderr)
	}
	if strings.TrimSpace(r.stdout+r.stderr) == "" {
		t.Errorf("print --help printed nothing")
	}
	if hasRow(r.stdout, "PETR4") {
		t.Errorf("print --help printed the table instead of the usage:\n%s", r.stdout)
	}
}

// S18.3 [view, print] Given a flag the tool does not know, when the tool is
// run, then it exits with status 2. (D50, D77)
// On print: `brl-stocks print --bogus`.
func TestS18_3_UnknownFlagExitsTwo(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.run("print", "--bogus")

	// Then
	if r.code != 2 {
		t.Errorf("exit status %d, want 2\nstderr: %s", r.code, r.stderr)
	}
}

// S18.4 [view, print] Given a flag the tool does not know, when the tool is
// run, then a usage message is on the error stream. (D50, D77)
// On print: `brl-stocks print --bogus`.
func TestS18_4_UnknownFlagPrintsUsageToErrorStream(t *testing.T) {
	// Given
	w := newWorld(t, bothOpen, plainConfig)
	loadQuotes(w.src, w.now, true)

	// When
	r := w.run("print", "--bogus")

	// Then
	if strings.TrimSpace(r.stderr) == "" {
		t.Errorf("the error stream is empty after an unknown flag")
	}
}
