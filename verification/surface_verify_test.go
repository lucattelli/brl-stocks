//go:build verification

// Increment 2 of the one-shot slice: the criteria of section 8 tagged `print`
// that increment 1 did not cover, each read on `print` as section 8's reading
// table says; QR1 (a representative set again with 20 tickers) and QR12 (what
// reaches the source, and that nothing else is contacted). See README.md.
//
// Several criteria share one run of the tool where they observe the same
// output. Each criterion is then a subtest named after it (for example
// TestVerifyS1_02_08_15_RowFormats/5_rows/S1_08), and writes its own CRITERION
// and RESULT lines into the evidence of the shared run.
package verification

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers of this file.
// ---------------------------------------------------------------------------

// ck is one criterion inside a shared run.
type ck struct {
	e  *env
	t  *testing.T
	id string
}

// crit runs one criterion's assertions as a subtest named after it and writes
// its verdict into the evidence of the shared run.
func crit(e *env, id, text string, fn func(c *ck)) {
	e.t.Helper()
	e.t.Run(strings.ReplaceAll(id, ".", "_"), func(t *testing.T) {
		c := &ck{e: e, t: t, id: id}
		e.logf("\nCRITERION  %s  %s\n", id, text)
		fn(c)
		v := "PASS"
		if t.Failed() {
			v = "FAIL"
		}
		e.logf("RESULT     %s  %s\n", id, v)
	})
}

func (c *ck) failf(format string, a ...any) {
	c.t.Helper()
	msg := fmt.Sprintf(format, a...)
	c.e.logf("ASSERTION FAILED [%s]: %s\n", c.id, msg)
	c.t.Errorf("%s: %s", c.id, msg)
}

func (c *ck) notef(format string, a ...any) {
	c.e.logf("OBSERVED   [%s] %s\n", c.id, fmt.Sprintf(format, a...))
}

// rowOf returns the one table line of a ticker (or "" and a failure).
func (c *ck) rowOf(out, ticker string) string {
	c.t.Helper()
	ls := rowLines(out, ticker)
	if len(ls) != 1 {
		c.failf("want exactly one line for row %s, found %d", ticker, len(ls))
		if len(ls) == 0 {
			return ""
		}
	}
	return ls[0]
}

// parsedRow is a table row split around its price: the name before it, the
// columns after it (change, percent, low, high, time, and a stale mark).
type parsedRow struct {
	name  string
	price string
	rest  []string
}

func parseRow(line, ticker string) (parsedRow, bool) {
	idx := priceRE.FindStringIndex(line)
	if idx == nil || !strings.HasPrefix(line, ticker) {
		return parsedRow{}, false
	}
	return parsedRow{
		name:  strings.TrimSpace(line[len(ticker):idx[0]]),
		price: line[idx[0]:idx[1]],
		rest:  strings.Fields(line[idx[1]:]),
	}, true
}

func (c *ck) parsed(out, ticker string) (parsedRow, bool) {
	c.t.Helper()
	l := c.rowOf(out, ticker)
	p, ok := parseRow(l, ticker)
	if !ok {
		c.failf("row %s: no price could be read in %q", ticker, l)
	} else if len(p.rest) < 5 {
		c.failf("row %s: want change, percent, low, high and time after the price, got %q", ticker, p.rest)
		ok = false
	}
	return p, ok
}

// The four cause phrases of 7.3; a line on the error stream that starts with
// one of them is a cause line ("an error line" on `print`).
var causePhrases = []string{"source unreachable", "source is rate-limiting", "source refused access", "source answer not understood"}

func causeLines(stderr string) []string {
	var got []string
	for _, l := range lines(stderr) {
		for _, p := range causePhrases {
			if strings.HasPrefix(strings.TrimSpace(l), p) {
				got = append(got, l)
				break
			}
		}
	}
	return got
}

var hhmmRE = regexp.MustCompile(`\b([01]\d|2[0-3]):([0-5]\d)\b`)

// hhmmIn reports whether the line holds an HH:MM (UTC, as the child runs with
// TZ=UTC) whose minute lies between the minutes of from and to.
func hhmmIn(line string, from, to time.Time) (string, bool) {
	m := hhmmRE.FindAllString(line, -1)
	if len(m) == 0 {
		return "", false
	}
	for _, s := range m {
		for t := from.UTC().Truncate(time.Minute); !t.After(to.UTC()); t = t.Add(time.Minute) {
			if t.Format("15:04") == s {
				return s, true
			}
		}
	}
	return strings.Join(m, ","), false
}

// countIn returns the integers in a line, apart from the digits of an HH:MM.
func countIn(line string) []int {
	var out []int
	for _, s := range regexp.MustCompile(`\d+`).FindAllString(hhmmRE.ReplaceAllString(line, " "), -1) {
		n, _ := strconv.Atoi(s)
		out = append(out, n)
	}
	return out
}

// state2 is the stand-in's behaviour with the control added for S5.12.
type state2 struct {
	state
	OpenEndUnix int64 `json:"open_end_unix"`
}

func (e *env) setSource2(st state2, what string) {
	e.t.Helper()
	b, _ := json.Marshal(st)
	e.logf("\nSTAND-IN   switched: %s  %s\n", what, b)
	resp, err := http.Post(e.src.url+"/_control", "application/json", bytes.NewReader(b))
	if err != nil {
		e.t.Fatalf("stand-in control: %v", err)
	}
	resp.Body.Close()
}

// fullRecord is the stand-in's record with what was added for QR12.
type fullRecord struct {
	record
	Method  string              `json:"method"`
	Host    string              `json:"host"`
	Headers map[string][]string `json:"headers"`
	BodyLen int64               `json:"body_len"`
}

func (e *env) fullRequests() []fullRecord {
	resp, err := http.Get(e.src.url + "/_requests")
	if err != nil {
		e.t.Fatalf("stand-in record: %v", err)
	}
	defer resp.Body.Close()
	var recs []fullRecord
	json.NewDecoder(resp.Body).Decode(&recs)
	return recs
}

// runEnv is env.run with variables added to the child's environment.
func (e *env) runEnv(extra []string, args ...string) result {
	e.t.Helper()
	before := len(e.src.requests())
	sumBefore := fileSum(e.cfgPath)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Env = append(e.environ(), extra...)
	cmd.Dir = e.root
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
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
	all := e.src.requests()
	if len(all) >= before {
		r.reqs = all[before:]
	}
	e.logf("\n$ brl-stocks %s\nENV        %s %s\nEXIT       %d   (wall time %s)\nSTDOUT\n%s\nSTDERR\n%s\nREQUESTS RECEIVED BY THE STAND-IN DURING THIS RUN: %d   per symbol: %s\n%s",
		strings.Join(args, " "), strings.Join(e.environ()[1:], " "), strings.Join(extra, " "), r.exit, r.dur.Round(time.Millisecond),
		indent(r.stdout), indent(r.stderr), len(r.reqs), fmtCounts(bySymbol(r.reqs)), fmtRecords(r.reqs))
	if sumAfter := fileSum(e.cfgPath); sumAfter != sumBefore {
		e.failf("QR7: the config file changed during the run: sha256 %s -> %s", sumBefore, sumAfter)
	}
	return r
}

// timedRun runs the tool and returns the result with the wall-clock start.
func (e *env) timedRun(args ...string) (result, time.Time) {
	e.t.Helper()
	start := time.Now()
	r := e.run(args...)
	e.logf("STARTED AT %s (unix ms %d)\n", start.UTC().Format(time.RFC3339Nano), start.UnixMilli())
	return r, start
}

// cfg builds a config file. pad fills it up to 20 distinct tickers (QR1).
type cfgSpec struct {
	interval int    // 0: the key is left out (default 60)
	summary  string // "": the key is left out; else the TOML value, e.g. `["USDBRL"]`
	top      []string
	groups   []groupSpec
}

type groupSpec struct {
	name    string
	tickers []string
}

var fillers = []string{"BBDC4", "ABEV3", "WEGE3", "BBAS3", "MGLU3", "SUZB3", "RENT3", "CSAN3", "RADL3", "GGBR4",
	"GOOG", "AMZN", "NVDA", "META", "TSLA", "NFLX", "ORCL", "INTC", "KO", "PEP", "JPM", "CSNA3", "ELET3"}

func (c cfgSpec) all() []string {
	seen := map[string]bool{}
	var out []string
	add := func(l []string) {
		for _, t := range l {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	add(c.top)
	for _, g := range c.groups {
		add(g.tickers)
	}
	return out
}

// pad20 adds filler tickers (to the last group, or to the top-level list when
// there is no group) until the config holds 20 distinct tickers.
func (c cfgSpec) pad20() cfgSpec {
	used := map[string]bool{}
	for _, t := range c.all() {
		used[t] = true
	}
	n := len(c.all())
	out := c
	out.top = append([]string(nil), c.top...)
	out.groups = append([]groupSpec(nil), c.groups...)
	for _, f := range fillers {
		if n >= 20 {
			break
		}
		if used[f] {
			continue
		}
		if len(out.groups) > 0 {
			g := &out.groups[len(out.groups)-1]
			g.tickers = append(append([]string(nil), g.tickers...), f)
		} else {
			out.top = append(out.top, f)
		}
		n++
	}
	return out
}

func (c cfgSpec) String() string {
	q := func(l []string) string {
		if len(l) == 0 {
			return "[]"
		}
		return `["` + strings.Join(l, `", "`) + `"]`
	}
	var b strings.Builder
	if c.interval > 0 {
		fmt.Fprintf(&b, "refresh_seconds = %d\n", c.interval)
	}
	if c.summary != "" {
		fmt.Fprintf(&b, "summary = %s\n", c.summary)
	}
	if len(c.top) > 0 {
		fmt.Fprintf(&b, "tickers = %s\n", q(c.top))
	}
	for _, g := range c.groups {
		fmt.Fprintf(&b, "\n[[group]]\nname = %q\ntickers = %s\n", g.name, q(g.tickers))
	}
	return b.String()
}

// symOf maps a ticker of these checks to the symbol the stand-in sees.
func symOf(tk string) string {
	switch tk {
	case "AAPL", "MSFT", "GOOG", "AMZN", "NVDA", "META", "TSLA", "NFLX", "ORCL", "INTC", "KO", "PEP", "JPM", "F":
		return tk
	}
	return tk + ".SA"
}

func symsOf(tks []string) []string {
	var out []string
	for _, t := range tks {
		out = append(out, symOf(t))
	}
	return out
}

// sizes runs fn once with the check's own config and once (QR1) padded to 20
// tickers, with the default 3 summary entries.
func sizes(t *testing.T, base cfgSpec, fn func(t *testing.T, cfg cfgSpec)) {
	for _, v := range []struct {
		name string
		cfg  cfgSpec
	}{{"own_rows", base}, {"20_tickers_QR1", base.pad20()}} {
		v := v
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()
			fn(t, v.cfg)
		})
	}
}

func dashCount(fields []string) int {
	n := 0
	for _, f := range fields {
		if f == "—" {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// S1 Row columns and number formats
// ---------------------------------------------------------------------------

// S1.2 [view, print] Given a row whose fetch succeeded, when the view is drawn
// at full width, then the row shows ticker, name, last price, day change as a
// value, day change as a percent, day low, day high and quote time.
// S1.8 Given a B3 row whose last price is 1038.42, when the row is drawn, then
// the price reads `R$ 1.038,42`.
// S1.9 Given a US row whose last price is 1229.87, when the row is drawn, then
// the price reads `US$ 1,229.87`.
// S1.10 Given a B3 row whose day change is +0.31, when the row is drawn, then
// the change value reads `+0,31`.
// S1.11 Given a B3 row whose day change is +0.81 percent, when the row is
// drawn, then the percent reads `+0,81%`.
// S1.12 Given a US row whose day change is -0.48 percent, when the row is
// drawn, then the percent reads `-0.48%`.
// S1.13 Given a B3 row whose day change is exactly zero, when the row is
// drawn, then the change value reads `+0,00`.
// S1.14 Given a B3 row whose last price is 38.425, when the row is drawn, then
// the price reads `R$ 38,43`.
// S1.15 Given a B3 row whose day low is 38.02, when the row is drawn, then the
// low reads `38,02`, with no currency symbol.
// On `print`: one `brl-stocks print` on an empty cache; the stand-in's prices:
// PETR4 38.42 (previous close 38.11, low 38.02), AAPL 229.87 (230.99), VALE3
// 61.07 (61.07), BBDC4 1038.42, ABEV3 38.425, NVDA 1229.87. Also QR1.
func TestVerifyS1_02_08_15_RowFormats(t *testing.T) {
	t.Parallel()
	base := cfgSpec{top: []string{"PETR4", "VALE3", "AAPL", "BBDC4", "ABEV3", "NVDA"}}
	sizes(t, base, func(t *testing.T, cfg cfgSpec) {
		e := newEnv(t, "S1.2, S1.8 to S1.15 (and QR1 with 20 tickers)", cfg.String())
		e.setSource(state{Quotes: map[string]override{
			"BBDC4.SA": {Name: sp("Bradesco PN"), Price: fp(1038.42), PrevClose: fp(1030.00), Low: fp(1030.50), High: fp(1040.10)},
			"ABEV3.SA": {Name: sp("Ambev ON"), Price: fp(38.425), PrevClose: fp(38.00), Low: fp(38.10), High: fp(38.50)},
			"NVDA":     {Name: sp("Nvidia Corp"), Price: fp(1229.87), PrevClose: fp(1200.00), Low: fp(1210.00), High: fp(1235.00)},
		}}, "BBDC4 1038.42, ABEV3 38.425, NVDA 1229.87")
		r := e.run("print")
		e.wantExit(r, 0)
		out := r.stdout

		crit(e, "S1.2", "a fetched row shows ticker, name, last price, change value, change percent, low, high and quote time", func(c *ck) {
			if h := linesWithPrefix(out, "TICKER"); len(h) != 1 {
				c.failf("want one header line starting with TICKER, got %q", h)
			} else {
				hf := strings.Fields(h[0])
				for _, w := range []string{"TICKER", "NAME", "LAST", "CHANGE", "LOW", "HIGH", "TIME"} {
					if !strings.Contains(" "+strings.Join(hf, " ")+" ", " "+w+" ") {
						c.failf("header line lacks %s: %q", w, h[0])
					}
				}
			}
			for tk, want := range map[string][]string{
				"PETR4": {"Petrobras PN", "R$ 38,42", "+0,31", "+0,81%", "38,02", "38,60"},
				"AAPL":  {"Apple Inc.", "US$ 229.87", "-1.12", "-0.48%", "228.90", "231.40"},
			} {
				p, ok := c.parsed(out, tk)
				if !ok {
					continue
				}
				got := []string{p.name, p.price, p.rest[0], p.rest[1], p.rest[2], p.rest[3]}
				if strings.Join(got, "|") != strings.Join(want, "|") {
					c.failf("row %s: want name, price, change, percent, low, high %q, got %q", tk, want, got)
				}
				if !regexp.MustCompile(`^\d\d:\d\d$`).MatchString(p.rest[4]) {
					c.failf("row %s: want a quote time HH:MM (quote from today), got %q", tk, p.rest[4])
				}
				c.notef("row %s: %q", tk, c.rowOf(out, tk))
			}
		})
		crit(e, "S1.8", "B3 price 1038.42 reads `R$ 1.038,42`", func(c *ck) {
			if p, ok := c.parsed(out, "BBDC4"); ok && p.price != "R$ 1.038,42" {
				c.failf("want `R$ 1.038,42`, got %q in %q", p.price, c.rowOf(out, "BBDC4"))
			} else if ok {
				c.notef("%q", c.rowOf(out, "BBDC4"))
			}
		})
		crit(e, "S1.9", "US price 1229.87 reads `US$ 1,229.87`", func(c *ck) {
			if p, ok := c.parsed(out, "NVDA"); ok && p.price != "US$ 1,229.87" {
				c.failf("want `US$ 1,229.87`, got %q in %q", p.price, c.rowOf(out, "NVDA"))
			} else if ok {
				c.notef("%q", c.rowOf(out, "NVDA"))
			}
		})
		crit(e, "S1.10", "B3 change +0.31 reads `+0,31`", func(c *ck) {
			if p, ok := c.parsed(out, "PETR4"); ok && p.rest[0] != "+0,31" {
				c.failf("want `+0,31`, got %q", p.rest[0])
			}
		})
		crit(e, "S1.11", "B3 change +0.81 percent reads `+0,81%`", func(c *ck) {
			if p, ok := c.parsed(out, "PETR4"); ok && p.rest[1] != "+0,81%" {
				c.failf("want `+0,81%%`, got %q", p.rest[1])
			}
		})
		crit(e, "S1.12", "US change -0.48 percent reads `-0.48%`", func(c *ck) {
			if p, ok := c.parsed(out, "AAPL"); ok && p.rest[1] != "-0.48%" {
				c.failf("want `-0.48%%`, got %q", p.rest[1])
			}
		})
		crit(e, "S1.13", "B3 change of exactly zero reads `+0,00`", func(c *ck) {
			if p, ok := c.parsed(out, "VALE3"); ok && p.rest[0] != "+0,00" {
				c.failf("want `+0,00`, got %q", p.rest[0])
			}
		})
		crit(e, "S1.14", "B3 price 38.425 reads `R$ 38,43`", func(c *ck) {
			if p, ok := c.parsed(out, "ABEV3"); ok && p.price != "R$ 38,43" {
				c.failf("want `R$ 38,43` (half away from zero, D66), got %q in %q", p.price, c.rowOf(out, "ABEV3"))
			} else if ok {
				c.notef("%q", c.rowOf(out, "ABEV3"))
			}
		})
		crit(e, "S1.15", "B3 low 38.02 reads `38,02` with no currency symbol", func(c *ck) {
			l := c.rowOf(out, "PETR4")
			if p, ok := c.parsed(out, "PETR4"); ok && p.rest[2] != "38,02" {
				c.failf("want the low `38,02`, got %q", p.rest[2])
			}
			if n := strings.Count(l, "R$") + strings.Count(l, "US$"); n != 1 {
				c.failf("want one currency symbol in the row (the last price's only), found %d in %q", n, l)
			}
		})
	})
}

// S1.5 [view, print] Given no cache, when the view starts, then each row shows
// its ticker and `—` in the data columns until its first quote arrives.
// On `print`: no cache and no quote arrives during the call (status 503, or no
// connection).
func TestVerifyS1_05_NoCacheNoQuoteDashes(t *testing.T) {
	t.Parallel()
	for _, v := range unreachableVariants {
		v := v
		t.Run(strings.ReplaceAll(v, " ", "_"), func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, "S1.5 ["+v+"]", baseConfig)
			if v == "status 503" {
				e.setSource(state{Status: 503}, "every request answered 503")
			} else {
				e.logf("\nSTAND-IN   stopped: its address now refuses connections\n")
				e.src.stop()
			}
			r := e.run("print")
			e.wantExit(r, 0)
			crit(e, "S1.5", "each row shows its ticker and `—` in the data columns", func(c *ck) {
				for _, tk := range tickers {
					l := c.rowOf(r.stdout, tk)
					f := strings.Fields(l)
					if len(f) < 2 {
						c.failf("row %s: %q", tk, l)
						continue
					}
					if d := dashCount(f[1:]); d != len(f)-1 || d < 6 {
						c.failf("row %s: want only `—` after the ticker, in every data column (name, last, change, percent, low, high, time), got %q", tk, l)
					} else {
						c.notef("row %s: %d data columns, all `—`", tk, d)
					}
				}
			})
		})
	}
}

// S1.7 [view, print] Given a quote from an earlier day, when the row is drawn,
// then the quote time is shown as a date (`DD/MM`).
// The stand-in's US market closed 2 days and 1 hour ago: the US quote is from
// that day. B3 open is the control (HH:MM). The child runs with TZ=UTC.
func TestVerifyS1_07_EarlierDayQuoteShowsDate(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S1.7", "tickers = [\"PETR4\", \"AAPL\"]\n")
	ago := int64(2*86400 + 3600)
	e.setSource(state{ClosedMarkets: []string{"us"}, ClosedAgoSeconds: ago}, "US closed 2 days and 1 hour ago")
	r := e.run("print")
	e.wantExit(r, 0)
	crit(e, "S1.7", "a quote from an earlier day shows its time as DD/MM", func(c *ck) {
		want := time.Unix(time.Now().Unix()-ago, 0).UTC().Format("02/01")
		if p, ok := c.parsed(r.stdout, "AAPL"); ok && p.rest[4] != want {
			c.failf("AAPL (quote from %s): want the time column `%s`, got %q in %q", want, want, p.rest[4], c.rowOf(r.stdout, "AAPL"))
		}
		if p, ok := c.parsed(r.stdout, "PETR4"); ok && !regexp.MustCompile(`^\d\d:\d\d$`).MatchString(p.rest[4]) {
			c.failf("control: PETR4 (quote from today) should show HH:MM, got %q", p.rest[4])
		}
	})
}

// ---------------------------------------------------------------------------
// S2 Summary line
// ---------------------------------------------------------------------------

// S2.1 [view, print] Given a config without a `summary` key, when the view is
// drawn, then a line above the watchlist shows Ibovespa, S&P 500 and USD/BRL,
// each with its value and day change percent.
// S2.5 Given an Ibovespa value of 158432.10, when the summary line is drawn,
// then the Ibovespa value reads `158.432`.
// S2.6 Given an S&P 500 value of 6812.4, when the summary line is drawn, then
// the S&P 500 value reads `6,812.40`.
// S2.7 Given a USD/BRL rate of 5.4321, when the summary line is drawn, then the
// USD/BRL value reads `5,4321`.
// S2.8 Given an Ibovespa day change of +0.32 percent, when the summary line is
// drawn, then its percent reads `+0,32%`.
// Stand-in: ^BVSP 158432.10 (previous close 157926.70: +0.32%), ^GSPC 6812.40,
// BRL=X 5.4321.
func TestVerifyS2_01_05_08_DefaultSummaryLine(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S2.1, S2.5 to S2.8", "tickers = [\"PETR4\", \"AAPL\"]\n")
	r := e.run("print")
	e.wantExit(r, 0)
	ls := lines(r.stdout)
	first := firstLine(r.stdout)
	entry := func(c *ck, label string) (string, string) {
		m := regexp.MustCompile(regexp.QuoteMeta(label) + ` (\S+) (\S+)`).FindStringSubmatch(first)
		if m == nil {
			c.failf("no `%s <value> <percent>` in the first line %q", label, first)
			return "", ""
		}
		return m[1], m[2]
	}
	crit(e, "S2.1", "without `summary`, a line above the watchlist shows IBOV, S&P 500 and USD/BRL with value and percent", func(c *ck) {
		hdr := -1
		for i, l := range ls {
			if strings.HasPrefix(l, "TICKER") {
				hdr = i
				break
			}
		}
		if hdr <= 0 {
			c.failf("want the summary line above the header line; header at line %d", hdr+1)
		}
		pct := regexp.MustCompile(`^[+-]\d[\d.,]*%$`)
		for _, lb := range []string{"IBOV", "S&P 500", "USD/BRL"} {
			if v, p := entry(c, lb); v != "" && !pct.MatchString(p) {
				c.failf("%s: want a value and a signed percent, got %q %q", lb, v, p)
			}
		}
		c.notef("first line %q", first)
	})
	crit(e, "S2.5", "Ibovespa 158432.10 reads `158.432`", func(c *ck) {
		if v, _ := entry(c, "IBOV"); v != "158.432" {
			c.failf("want `158.432`, got %q", v)
		}
	})
	crit(e, "S2.6", "S&P 500 6812.4 reads `6,812.40`", func(c *ck) {
		if v, _ := entry(c, "S&P 500"); v != "6,812.40" {
			c.failf("want `6,812.40`, got %q", v)
		}
	})
	crit(e, "S2.7", "USD/BRL 5.4321 reads `5,4321`", func(c *ck) {
		if v, _ := entry(c, "USD/BRL"); v != "5,4321" {
			c.failf("want `5,4321`, got %q", v)
		}
	})
	crit(e, "S2.8", "Ibovespa +0.32 percent reads `+0,32%`", func(c *ck) {
		if _, p := entry(c, "IBOV"); p != "+0,32%" {
			c.failf("want `+0,32%%`, got %q", p)
		}
	})
}

// S2.2 [view, print] Given `summary = ["USDBRL"]`, when the view is drawn,
// then the summary line shows only USD/BRL.
func TestVerifyS2_02_SummaryOnlyUSDBRL(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S2.2", "summary = [\"USDBRL\"]\ntickers = [\"PETR4\", \"AAPL\"]\n")
	r := e.run("print")
	e.wantExit(r, 0)
	crit(e, "S2.2", "summary = [\"USDBRL\"] shows only USD/BRL", func(c *ck) {
		first := firstLine(r.stdout)
		if !regexp.MustCompile(`^USD/BRL \S+ \S+%\s*$`).MatchString(first) {
			c.failf("want the first line to be the USD/BRL entry alone, got %q", first)
		}
		if strings.Contains(r.stdout, "IBOV") || strings.Contains(r.stdout, "S&P 500") {
			c.failf("IBOV or S&P 500 appears in the output: %q", r.stdout)
		}
	})
}

// S2.3 [view, print] Given `summary = []`, when the view is drawn, then there
// is no summary line.
func TestVerifyS2_03_EmptySummaryNoLine(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S2.3", "summary = []\ntickers = [\"PETR4\", \"AAPL\"]\n")
	r := e.run("print")
	e.wantExit(r, 0)
	crit(e, "S2.3", "summary = [] shows no summary line", func(c *ck) {
		for _, w := range []string{"IBOV", "S&P 500", "USD/BRL"} {
			if strings.Contains(r.stdout, w) {
				c.failf("%s appears in the output", w)
			}
		}
		for _, l := range lines(r.stdout) {
			if strings.TrimSpace(l) == "" {
				continue
			}
			if !strings.HasPrefix(l, "TICKER") {
				c.failf("want the header line first, got %q", l)
			}
			break
		}
	})
}

// ---------------------------------------------------------------------------
// S3.6 Currency
// ---------------------------------------------------------------------------

// S3.6 [view, print] Given a B3 row and a US row on screen, when quotes are
// shown, then no price has been converted to the other currency.
// Table and JSON compared with the stand-in's own numbers and currencies.
func TestVerifyS3_06_NoConversion(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S3.6", "tickers = [\"PETR4\", \"AAPL\"]\n")
	r := e.run("print")
	e.wantExit(r, 0)
	rj := e.run("print", "--json")
	e.wantExit(rj, 0)
	crit(e, "S3.6", "no price converted to the other currency", func(c *ck) {
		for tk, want := range map[string][3]string{"PETR4": {"R$ 38,42", "BRL", "38.42"}, "AAPL": {"US$ 229.87", "USD", "229.87"}} {
			if p, ok := c.parsed(r.stdout, tk); ok && p.price != want[0] {
				c.failf("table %s: want %q (the source's price, its own currency), got %q", tk, want[0], p.price)
			}
			if doc := parseDoc(e, rj.stdout); doc != nil {
				rw := docRows(doc)[tk]
				if rw == nil || rw["currency"] != want[1] || fmt.Sprint(rw["price"]) != want[2] {
					c.failf("JSON %s: want currency %s and price %s, got %v %v", tk, want[1], want[2], rw["currency"], rw["price"])
				}
			}
		}
	})
}

// ---------------------------------------------------------------------------
// S4 Groups and S9.1 order
// ---------------------------------------------------------------------------

// tableLines returns the lines between the header line and the last line.
func tableBody(out string) []string {
	ls := lines(out)
	hdr := -1
	for i, l := range ls {
		if strings.HasPrefix(l, "TICKER") {
			hdr = i
			break
		}
	}
	if hdr < 0 || len(ls) < hdr+2 {
		return nil
	}
	return ls[hdr+1 : len(ls)-1]
}

// S4.1 [view, print] Given a config with the groups "Brasil" and "US" in that
// order, when the view is drawn, then each name appears as a header above its
// rows, in that order.
// S4.3 [view, print] Given tickers outside groups and a group, when the view
// is drawn, then the tickers outside groups come first, with no header.
// S9.1 [view, print] Given the view has just started, when it is drawn, then
// rows are in the order of the config file.
// The file order (MSFT, PETR4 | VALE3, ITUB4 | AAPL) is neither the order by
// ticker nor by day change. Also QR1.
func TestVerifyS4_01_03_S9_01_GroupsAndOrder(t *testing.T) {
	t.Parallel()
	base := cfgSpec{top: []string{"MSFT", "PETR4"}, groups: []groupSpec{{"Brasil", []string{"VALE3", "ITUB4"}}, {"US", []string{"AAPL"}}}}
	sizes(t, base, func(t *testing.T, cfg cfgSpec) {
		e := newEnv(t, "S4.1, S4.3, S9.1 (and QR1 with 20 tickers)", cfg.String())
		r := e.run("print")
		e.wantExit(r, 0)
		body := tableBody(r.stdout)
		idx := func(pred func(string) bool) int {
			for i, l := range body {
				if pred(l) {
					return i
				}
			}
			return -1
		}
		header := func(name string) int { return idx(func(l string) bool { return strings.TrimSpace(l) == name }) }
		rowAt := func(tk string) int {
			return idx(func(l string) bool { f := strings.Fields(l); return len(f) > 0 && f[0] == tk })
		}
		crit(e, "S4.1", "group names Brasil then US, each a header above its rows, in file order", func(c *ck) {
			hb, hu := header("Brasil"), header("US")
			if hb < 0 || hu < 0 {
				c.failf("want the lines `Brasil` and `US` on their own, got body %q", body)
				return
			}
			if hb >= hu {
				c.failf("want Brasil above US (file order), got lines %d and %d", hb, hu)
			}
			for _, g := range cfg.groups {
				h := header(g.name)
				for _, tk := range g.tickers {
					if i := rowAt(tk); !(i > h) {
						c.failf("row %s (group %s) at line %d is not below its header at line %d", tk, g.name, i, h)
					}
				}
			}
			if i := rowAt("VALE3"); hu >= 0 && i > hu {
				c.failf("VALE3 (Brasil) is below the US header")
			}
		})
		crit(e, "S4.3", "tickers outside groups come first, with no header", func(c *ck) {
			hb := header("Brasil")
			for _, tk := range cfg.top {
				if i := rowAt(tk); i < 0 || (hb >= 0 && i > hb) {
					c.failf("top-level row %s at line %d, want above the first group header (line %d)", tk, i, hb)
				}
			}
			for i := 0; i < len(body) && i < len(cfg.top); i++ {
				if f := strings.Fields(body[i]); len(f) == 0 || f[0] != cfg.top[i] {
					c.failf("want the top-level rows right below the header line, with no header; line %d is %q", i+1, body[i])
				}
			}
		})
		crit(e, "S9.1", "rows in the order of the config file", func(c *ck) {
			var got []string
			want := cfg.all()
			wantSet := map[string]bool{}
			for _, w := range want {
				wantSet[w] = true
			}
			for _, l := range body {
				if f := strings.Fields(l); len(f) > 0 && wantSet[f[0]] {
					got = append(got, f[0])
				}
			}
			if strings.Join(got, " ") != strings.Join(want, " ") {
				c.failf("want %v, got %v", want, got)
			}
		})
	})
}

// S4.1 again, with the groups in the other order in the file: US, then Brasil.
func TestVerifyS4_01_GroupOrderFollowsFile(t *testing.T) {
	t.Parallel()
	cfg := cfgSpec{groups: []groupSpec{{"US", []string{"AAPL"}}, {"Brasil", []string{"VALE3"}}}}
	e := newEnv(t, "S4.1 (groups US then Brasil in the file)", cfg.String())
	r := e.run("print")
	e.wantExit(r, 0)
	crit(e, "S4.1", "groups appear in file order (US above Brasil here)", func(c *ck) {
		body := tableBody(r.stdout)
		var got []string
		for _, l := range body {
			if f := strings.Fields(l); len(f) > 0 {
				got = append(got, f[0])
			}
		}
		want := "US AAPL Brasil VALE3"
		if strings.Join(got, " ") != want {
			c.failf("want the lines to start with %q, got %q", want, got)
		}
	})
}

// S4.2 [view, print] Given a config with no group, when the view is drawn,
// then no group header appears. Also QR1.
func TestVerifyS4_02_NoGroupNoHeader(t *testing.T) {
	t.Parallel()
	base := cfgSpec{top: []string{"PETR4", "AAPL", "VALE3"}}
	sizes(t, base, func(t *testing.T, cfg cfgSpec) {
		e := newEnv(t, "S4.2 (and QR1 with 20 tickers)", cfg.String())
		r := e.run("print")
		e.wantExit(r, 0)
		crit(e, "S4.2", "no group header", func(c *ck) {
			known := map[string]bool{}
			for _, tk := range cfg.all() {
				known[tk] = true
			}
			n := 0
			for _, l := range tableBody(r.stdout) {
				if strings.TrimSpace(l) == "" {
					continue
				}
				if f := strings.Fields(l); !known[f[0]] {
					c.failf("a line that is not a row: %q", l)
				} else {
					n++
				}
			}
			if n != len(cfg.all()) {
				c.failf("want %d rows, got %d", len(cfg.all()), n)
			}
		})
	})
}

// ---------------------------------------------------------------------------
// S5 Closed markets, stale marks, interval floor
// ---------------------------------------------------------------------------

// S5.4 [view, print] Given the US market is closed, when the view is drawn,
// then US rows show the last close.
// S5.5 [view, print] Given the US market is closed and B3 is open, when the
// view is drawn, then the status line reads `B3 open · US closed`.
// On `print` the status line is the last line of the table.
func TestVerifyS5_04_05_USClosed(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S5.4, S5.5", "tickers = [\"PETR4\", \"AAPL\", \"MSFT\"]\n")
	e.setSource(state{ClosedMarkets: []string{"us"}}, "US closed 2 h ago (last close: AAPL 229.87, MSFT 512.30)")
	r := e.run("print")
	e.wantExit(r, 0)
	crit(e, "S5.4", "US rows show the last close", func(c *ck) {
		for tk, want := range map[string]string{"AAPL": "US$ 229.87", "MSFT": "US$ 512.30"} {
			l := c.rowOf(r.stdout, tk)
			if !strings.Contains(l, want) || staleRE.MatchString(l) {
				c.failf("row %s: want the last close %q and no stale mark, got %q", tk, want, l)
			}
		}
	})
	crit(e, "S5.5", "the last line reads `B3 open · US closed`", func(c *ck) {
		if got := strings.TrimSpace(lastLine(r.stdout)); got != "B3 open · US closed" {
			c.failf("want `B3 open · US closed`, got %q", got)
		}
	})
}

// S5.6 [view, print] Given a closed market whose rows were last fetched, with
// success, two days ago, when the view is drawn, then those rows carry no
// stale mark.
// Scaled: the US market closed two days ago (its quote is two days old); its
// rows were last fetched with success 25 s ago, more than two intervals of
// 10 s, which would make an open market's row stale by age (6.2). B3 open is
// the control (fetched again).
func TestVerifyS5_06_ClosedMarketNoStaleByAge(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S5.6 (scaled: last successful fetch 25 s ago, interval 10 s)", "refresh_seconds = 10\ntickers = [\"PETR4\", \"AAPL\"]\n")
	e.setSource(state{ClosedMarkets: []string{"us"}, ClosedAgoSeconds: 2 * 86400}, "US closed two days ago")
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.sleep(25*time.Second, "more than two refresh intervals")
	r2 := e.run("print")
	e.wantExit(r2, 0)
	rj := e.run("print", "--json")
	crit(e, "S5.6", "rows of a closed market last fetched long ago carry no stale mark", func(c *ck) {
		if bySymbol(r2.reqs)["AAPL"] != 0 {
			c.notef("AAPL was requested again (%d); the criterion is then not exercised", bySymbol(r2.reqs)["AAPL"])
		}
		l := c.rowOf(r2.stdout, "AAPL")
		if !hasQuote(l) || staleRE.MatchString(l) {
			c.failf("AAPL: want its quote with no stale mark, got %q", l)
		}
		if doc := parseDoc(e, rj.stdout); doc != nil {
			if s := docRows(doc)["AAPL"]["status"]; s != "fresh" {
				c.failf("JSON AAPL status: want fresh, got %v", s)
			}
		}
	})
}

// S5.10 [view, print] Given `refresh_seconds = 3`, when the view runs, then
// rows are requested no more often than every 10 seconds.
// On `print` (scope.md): `brl-stocks print` every 2 seconds for 32 seconds;
// no symbol is requested twice less than 10 seconds apart.
func TestVerifyS5_10_IntervalFloor(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S5.10", "refresh_seconds = 3\ntickers = [\"PETR4\", \"AAPL\"]\n")
	start := time.Now()
	for i := 0; time.Since(start) < 32*time.Second; i++ {
		next := start.Add(time.Duration(i) * 2 * time.Second)
		if d := time.Until(next); d > 0 {
			time.Sleep(d)
		}
		r := e.run("print")
		e.wantExit(r, 0)
	}
	crit(e, "S5.10", "no row requested twice less than 10 s apart", func(c *ck) {
		at := map[string][]int64{}
		for _, r := range e.src.requests() {
			at[r.Symbol] = append(at[r.Symbol], r.UnixMS)
		}
		for _, s := range []string{"PETR4.SA", "AAPL"} {
			ts := at[s]
			sort.Slice(ts, func(i, j int) bool { return ts[i] < ts[j] })
			if len(ts) < 2 {
				c.failf("%s: want the row refreshed during 32 s (at least 2 requests), got %d", s, len(ts))
			}
			var gaps []string
			for i := 1; i < len(ts); i++ {
				g := ts[i] - ts[i-1]
				gaps = append(gaps, fmt.Sprintf("%.2fs", float64(g)/1000))
				if g < 9950 {
					c.failf("%s: two requests %.2f s apart", s, float64(g)/1000)
				}
			}
			c.notef("%s: %d requests, gaps %v", s, len(ts), gaps)
		}
	})
}

// S5.12 [view, print] Given a row whose last fetch failed before its market
// closed, when the view is drawn after the close, then the row is still marked
// stale.
// The stand-in's session ends 22 s after the first call (open_end_unix). At
// +12 s every request is answered 503 (the rows' fetch fails while the market
// is open); at +27 s, after the close, `print` is run again with the source
// still failing.
func TestVerifyS5_12_FailedBeforeCloseStaysStale(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S5.12", "refresh_seconds = 10\ntickers = [\"PETR4\", \"AAPL\"]\n")
	t0 := time.Now()
	end := t0.Unix() + 22
	e.setSource2(state2{OpenEndUnix: end}, fmt.Sprintf("open markets' session ends at %s", time.Unix(end, 0).UTC().Format(time.RFC3339)))
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.sleep(time.Until(t0.Add(12*time.Second)), "cache older than the interval")
	e.setSource2(state2{state: state{Status: 503}, OpenEndUnix: end}, "every request answered 503, session end unchanged")
	r2 := e.run("print")
	e.wantExit(r2, 0)
	e.sleep(time.Until(t0.Add(27*time.Second)), "past the session's end")
	r3 := e.run("print")
	e.wantExit(r3, 0)
	rj := e.run("print", "--json")
	crit(e, "S5.12", "a row whose fetch failed before the close is still stale after it", func(c *ck) {
		for _, tk := range []string{"PETR4", "AAPL"} {
			if l := c.rowOf(r2.stdout, tk); !staleRE.MatchString(l) {
				c.failf("set-up: before the close, after the failed fetch, row %s is not stale: %q", tk, l)
			}
		}
		if ll := strings.TrimSpace(lastLine(r3.stdout)); ll != "B3 closed · US closed" {
			c.failf("set-up (6.1, the session ends: the market is shown as closed): want the last line `B3 closed · US closed` after the session's end, got %q", ll)
		}
		for _, tk := range []string{"PETR4", "AAPL"} {
			if l := c.rowOf(r3.stdout, tk); !staleRE.MatchString(l) || !hasQuote(l) {
				c.failf("after the close: row %s: want its last price marked stale, got %q", tk, l)
			}
		}
		if doc := parseDoc(e, rj.stdout); doc != nil {
			for _, tk := range []string{"PETR4", "AAPL"} {
				rw := docRows(doc)[tk]
				c.notef("JSON %s: status %v market_state %v", tk, rw["status"], rw["market_state"])
				if rw["status"] != "stale" {
					c.failf("JSON %s: want status stale, got %v", tk, rw["status"])
				}
			}
		}
	})
}

// ---------------------------------------------------------------------------
// S6 Failed fetches
// ---------------------------------------------------------------------------

// S6.1 [view, print] Given rows with quotes and an open market, when a refresh
// fails, then the rows still show their last known prices.
// S6.2 [view, print] Given a row whose last fetch failed, when the view is
// drawn, then the row is marked stale with its age.
// S6.3 [view, print] Given a refresh in which five rows failed, when the view
// is drawn, then exactly one error line is shown.
// S6.9 [view, print] Given a failed refresh, when the error line is drawn, then
// it names the cause, the number of rows not updated and the time of the next
// attempt.
// On `print`: cached quotes 12 s old (interval 10 s), then a `print` whose
// fetches all fail. "Exactly one error line" counts the error-stream lines that
// give a cause (section 8's reading table). The next attempt is one interval
// after the failure (6.2), so its HH:MM lies between the run's start and its
// end plus the interval. Also QR1 (20 rows fail).
func TestVerifyS6_01_02_03_09_FailedRefresh(t *testing.T) {
	t.Parallel()
	base := cfgSpec{interval: 10, top: []string{"PETR4", "AAPL"}, groups: []groupSpec{{"Brasil", []string{"VALE3", "ITUB4"}}, {"US", []string{"MSFT"}}}}
	sizes(t, base, func(t *testing.T, cfg cfgSpec) {
		for _, v := range unreachableVariants {
			v := v
			t.Run(strings.ReplaceAll(v, " ", "_"), func(t *testing.T) {
				t.Parallel()
				e := newEnv(t, "S6.1, S6.2, S6.3, S6.9 ["+v+"] (and QR1 with 20 tickers)", cfg.String())
				r1 := e.run("print")
				e.wantExit(r1, 0)
				before := map[string]string{}
				for _, tk := range cfg.all() {
					if p, ok := parseRow(row(e, r1.stdout, tk), tk); ok {
						before[tk] = p.price
					}
				}
				if v == "status 503" {
					e.setSource(state{Status: 503}, "every request answered 503")
				} else {
					e.logf("\nSTAND-IN   stopped: its address now refuses connections\n")
					e.src.stop()
				}
				e.sleep(pastInterval, "cache older than the interval")
				r, start := e.timedRun("print")
				end := time.Now()
				e.wantExit(r, 0)
				n := len(cfg.all())
				crit(e, "S6.1", "the rows still show their last known prices", func(c *ck) {
					for _, tk := range cfg.all() {
						if p, ok := c.parsed(r.stdout, tk); ok && p.price != before[tk] {
							c.failf("row %s: want the last known price %q, got %q", tk, before[tk], p.price)
						}
					}
				})
				crit(e, "S6.2", "a row whose last fetch failed is marked stale with its age", func(c *ck) {
					for _, tk := range cfg.all() {
						l := c.rowOf(r.stdout, tk)
						m := staleRE.FindStringSubmatch(l)
						if m == nil {
							c.failf("row %s: want a stale mark with the age, got %q", tk, l)
							continue
						}
						age, _ := strconv.Atoi(m[1])
						if m[2] != "s" || age < 10 || age > 25 {
							c.failf("row %s: the last success was about 12 s ago, the mark reads %q", tk, m[0])
						}
					}
				})
				crit(e, "S6.3", fmt.Sprintf("%d rows failed: exactly one line on the error stream gives a cause", n), func(c *ck) {
					if cl := causeLines(r.stderr); len(cl) != 1 {
						c.failf("want 1 cause line, got %d: %q (error stream %q)", len(cl), cl, r.stderr)
					}
				})
				crit(e, "S6.9", "the cause line names the cause, the number of rows not updated and the time of the next attempt", func(c *ck) {
					cl := causeLines(r.stderr)
					if len(cl) == 0 {
						c.failf("no cause line on the error stream: %q", r.stderr)
						return
					}
					l := cl[0]
					c.notef("cause line %q", l)
					if !strings.HasPrefix(l, "source unreachable") {
						c.failf("cause: want `source unreachable`, got %q", l)
					}
					nums := countIn(l)
					found := false
					for _, x := range nums {
						if x == n {
							found = true
						}
					}
					if !found {
						c.failf("want the number of rows not updated (%d) in the line, found the numbers %v", n, nums)
					}
					if s, ok := hhmmIn(l, start, end.Add(10*time.Second)); !ok {
						c.failf("want the next attempt as HH:MM between %s and %s (one interval after the failure), found %q",
							start.UTC().Format("15:04"), end.Add(10*time.Second).UTC().Format("15:04"), s)
					}
				})
			})
		}
	})
}

// S6.5 [view, print] Given stale rows and an error line, when a refresh
// succeeds for every row, then the stale marks are gone.
// S6.6 [view, print] Given stale rows and an error line, when a refresh
// succeeds for every row, then the error line is gone.
// On `print` (scope.md): two calls. The first fails (503) and leaves stale rows
// and a cause line; the source answers again; 12 s later (the wait of one
// interval has ended) a second call fetches every row with success.
func TestVerifyS6_05_06_RecoveryClearsMarks(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S6.5, S6.6", baseConfig)
	r0 := e.run("print")
	e.wantExit(r0, 0)
	e.setSource(state{Status: 503}, "every request answered 503")
	e.sleep(pastInterval, "cache older than the interval")
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.setSource(state{}, "the source answers again")
	e.sleep(pastInterval, "the wait of one interval after the failure ends")
	r2 := e.run("print")
	e.wantExit(r2, 0)
	given := func(c *ck) {
		n := 0
		for _, tk := range tickers {
			if staleRE.MatchString(row(e, r1.stdout, tk)) {
				n++
			}
		}
		if n == 0 || len(causeLines(r1.stderr)) == 0 {
			c.failf("set-up: the failing call did not leave stale rows (%d) and a cause line (%q)", n, r1.stderr)
		}
		ok := map[string]bool{}
		for _, rq := range r2.reqs {
			if rq.Status == 200 {
				ok[rq.Symbol] = true
			}
		}
		for _, s := range symsOf(tickers) {
			if !ok[s] {
				c.failf("set-up: the second call did not fetch %s with success (requests: %s)", s, fmtRecords(r2.reqs))
			}
		}
	}
	crit(e, "S6.5", "after a refresh that succeeds for every row, the stale marks are gone", func(c *ck) {
		given(c)
		for _, tk := range tickers {
			if l := c.rowOf(r2.stdout, tk); staleRE.MatchString(l) || !hasQuote(l) {
				c.failf("row %s: want a quote with no stale mark, got %q", tk, l)
			}
		}
	})
	crit(e, "S6.6", "after a refresh that succeeds for every row, the error line is gone", func(c *ck) {
		given(c)
		if cl := causeLines(r2.stderr); len(cl) != 0 {
			c.failf("want no cause line on the second call's error stream, got %q", cl)
		}
	})
}

// S6.8 [view, print] Given a refresh in which one row fails and the others
// succeed, when the view is drawn, then only that row is marked stale.
// VALE3 answered 503, the others 200, on a cache older than the interval.
// Also QR1.
func TestVerifyS6_08_OnlyFailedRowStale(t *testing.T) {
	t.Parallel()
	base := cfgSpec{interval: 10, top: []string{"PETR4", "AAPL"}, groups: []groupSpec{{"Brasil", []string{"VALE3", "ITUB4"}}, {"US", []string{"MSFT"}}}}
	sizes(t, base, func(t *testing.T, cfg cfgSpec) {
		e := newEnv(t, "S6.8 (and QR1 with 20 tickers)", cfg.String())
		r1 := e.run("print")
		e.wantExit(r1, 0)
		e.setSource(state{SymbolStatus: map[string]int{"VALE3.SA": 503}}, "VALE3.SA answered 503, the rest 200")
		e.sleep(pastInterval, "cache older than the interval")
		r := e.run("print")
		e.wantExit(r, 0)
		crit(e, "S6.8", "only the failed row is stale", func(c *ck) {
			if bySymbol(r.reqs)["VALE3.SA"] == 0 {
				c.failf("set-up: VALE3 was not requested")
			}
			for _, tk := range cfg.all() {
				l := c.rowOf(r.stdout, tk)
				if st := staleRE.MatchString(l); st != (tk == "VALE3") {
					c.failf("row %s: stale mark %v, want %v: %q", tk, st, tk == "VALE3", l)
				}
			}
			c.notef("error stream %q", r.stderr)
		})
	})
}

// S6.9 again, with one row failing among others: the number of rows not
// updated is 1.
func TestVerifyS6_09_OneRowNotUpdated(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S6.9 (one row fails)", baseConfig)
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.setSource(state{SymbolStatus: map[string]int{"VALE3.SA": 503}}, "VALE3.SA answered 503, the rest 200")
	e.sleep(pastInterval, "cache older than the interval")
	r, start := e.timedRun("print")
	end := time.Now()
	e.wantExit(r, 0)
	crit(e, "S6.9", "the cause line names the cause, 1 row not updated and the time of the next attempt", func(c *ck) {
		cl := causeLines(r.stderr)
		if len(cl) != 1 {
			c.failf("want one cause line, got %q (error stream %q)", cl, r.stderr)
			return
		}
		l := cl[0]
		c.notef("cause line %q", l)
		if !strings.HasPrefix(l, "source unreachable") {
			c.failf("cause: want `source unreachable`, got %q", l)
		}
		if nums := countIn(l); len(nums) == 0 || nums[0] != 1 {
			c.failf("want 1 row not updated, found the numbers %v", nums)
		}
		if s, ok := hhmmIn(l, start, end.Add(10*time.Second)); !ok {
			c.failf("want the next attempt as HH:MM about one interval ahead, found %q", s)
		}
	})
}

// S6.10 [view, print] Given a stale row whose last successful fetch was 12
// minutes ago, when the row is drawn, then the mark reads `!12m`.
// Scaled: the last success 12 s ago: the mark reads `!<seconds>s` with the age
// in seconds. The literal 12 minutes: TestVerifySlow_S6_10_TwelveMinutes.
func TestVerifyS6_10_StaleMarkAge(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S6.10 (scaled: 12 s in place of 12 min)", baseConfig)
	r1 := e.run("print")
	e.wantExit(r1, 0)
	okAt := time.Now()
	e.setSource(state{Status: 503}, "every request answered 503")
	e.sleep(pastInterval, "12 s after the last success")
	r := e.run("print")
	age := time.Since(okAt)
	e.wantExit(r, 0)
	crit(e, "S6.10", "the stale mark reads the age since the last success", func(c *ck) {
		l := c.rowOf(r.stdout, "PETR4")
		m := staleRE.FindStringSubmatch(l)
		if m == nil {
			c.failf("no stale mark in %q", l)
			return
		}
		n, _ := strconv.Atoi(m[1])
		if m[2] != "s" || float64(n) < pastInterval.Seconds()-1 || float64(n) > age.Seconds()+1 {
			c.failf("want `!%ds` or so (last success %.1f s before the end of the run), got %q", int(pastInterval.Seconds()), age.Seconds(), m[0])
		}
	})
}

// S6.10 literally: the last success 12 minutes ago, default interval. SLOW.
func TestVerifySlow_S6_10_TwelveMinutes(t *testing.T) {
	needSlow(t, "S6.10 literally: 12 minutes")
	t.Parallel()
	e := newEnv(t, "S6.10 (literal: 12 minutes)", defaultIntervalConfig)
	r1 := e.run("print")
	e.wantExit(r1, 0)
	okAt := time.Now()
	e.setSource(state{Status: 503}, "every request answered 503")
	e.sleep(12*time.Minute+20*time.Second, "12 minutes after the last success")
	r := e.run("print")
	e.wantExit(r, 0)
	crit(e, "S6.10", "the mark reads `!12m`", func(c *ck) {
		c.notef("last success %s ago", time.Since(okAt).Round(time.Second))
		for _, tk := range tickers {
			if l := c.rowOf(r.stdout, tk); !strings.Contains(l, "!12m") {
				c.failf("row %s: want `!12m`, got %q", tk, l)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// S7 Forced fetch (`print --refresh` stands for `r`)
// ---------------------------------------------------------------------------

// earliest returns the earliest request among syms, in ms after start.
func earliest(reqs []record, syms []string, start time.Time) (int64, bool) {
	want := map[string]bool{}
	for _, s := range syms {
		want[s] = true
	}
	best, found := int64(0), false
	for _, r := range reqs {
		if want[r.Symbol] && (!found || r.UnixMS-start.UnixMilli() < best) {
			best, found = r.UnixMS-start.UnixMilli(), true
		}
	}
	return best, found
}

// S7.1 [view, print] Given B3 is open and the source answers, when `r` is
// pressed, then the first B3 rows are requested within 2 seconds, without
// waiting for the interval.
// Default interval (60 s), cache 6 s old: a plain `print` would not fetch.
// Measured from just before the process is started to the stand-in's arrival
// time of the first B3 row request.
func TestVerifyS7_01_RefreshRequestsB3Within2s(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S7.1", "tickers = [\"PETR4\", \"VALE3\", \"AAPL\"]\n")
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.sleep(6*time.Second, "past the 5 s of 6.5 row 3; the default interval has not passed")
	r, start := e.timedRun("print", "--refresh")
	e.wantExit(r, 0)
	crit(e, "S7.1", "the first B3 rows are requested within 2 s", func(c *ck) {
		ms, ok := earliest(r.reqs, []string{"PETR4.SA", "VALE3.SA"}, start)
		if !ok {
			c.failf("no B3 row was requested (requests: %s)", fmtCounts(bySymbol(r.reqs)))
			return
		}
		c.notef("first B3 row request %d ms after the process was started", ms)
		if ms > 2000 {
			c.failf("first B3 row requested %d ms after start, want at most 2000", ms)
		}
	})
}

// S7.2 [view, print] Given a network error has put the next automatic attempt
// four minutes away, when `r` is pressed, then the first failed rows are
// requested within 2 seconds.
// Scaled: one failure with the default interval puts the next attempt 60 s
// away (not 4 minutes); a plain `print` right after shows the wait holds.
func TestVerifyS7_02_RefreshDuringWait(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S7.2 (scaled: wait of 60 s in place of 4 min)", "tickers = [\"PETR4\", \"AAPL\"]\n")
	e.setSource(state{Status: 503}, "every request answered 503")
	r1 := e.run("print")
	e.wantExit(r1, 0)
	r2 := e.run("print")
	e.setSource(state{}, "the source answers again")
	e.sleep(6*time.Second, "past the 5 s of 6.5 row 3")
	r, start := e.timedRun("print", "--refresh")
	e.wantExit(r, 0)
	crit(e, "S7.2", "during the wait after a network error, the failed rows are requested within 2 s", func(c *ck) {
		if len(r1.reqs) == 0 {
			c.failf("set-up: the first call sent nothing")
		}
		if len(r2.reqs) != 0 {
			c.notef("control: a plain print during the wait sent %d request(s) (%s); 6.5 says none", len(r2.reqs), fmtCounts(bySymbol(r2.reqs)))
		}
		ms, ok := earliest(r.reqs, []string{"PETR4.SA", "AAPL"}, start)
		if !ok {
			c.failf("no failed row was requested (requests: %s)", fmtCounts(bySymbol(r.reqs)))
			return
		}
		c.notef("first failed row request %d ms after start", ms)
		if ms > 2000 {
			c.failf("first failed row requested %d ms after start, want at most 2000", ms)
		}
	})
}

// S7.3 [view, print] Given the source is rate-limiting or refusing the tool,
// when `r` is pressed, then no request is sent.
// S7.4 [view, print] Given the source is rate-limiting or refusing the tool,
// when `r` is pressed, then the status line shows the time of the next
// attempt.
// On `print`: the time is on the error-stream line that gives the cause; the
// first wait of a block is 5 minutes from the refusal (6.3), so the HH:MM is
// that of the first refused answer plus 5 minutes (or the next minute).
func TestVerifyS7_03_04_RefreshDuringBlock(t *testing.T) {
	t.Parallel()
	for _, v := range []struct {
		status int
		phrase string
	}{{429, "source is rate-limiting"}, {403, "source refused access"}} {
		v := v
		t.Run(strconv.Itoa(v.status), func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, fmt.Sprintf("S7.3, S7.4 [status %d]", v.status), baseConfig)
			r1 := e.run("print")
			e.wantExit(r1, 0)
			e.setSource(state{Status: v.status}, fmt.Sprintf("every request answered %d", v.status))
			e.sleep(pastInterval, "cache older than the interval")
			r2 := e.run("print")
			e.wantExit(r2, 0)
			var refusedAt int64
			for _, q := range r2.reqs {
				if q.Status == v.status && (refusedAt == 0 || q.AnsweredMS < refusedAt) {
					refusedAt = q.AnsweredMS
				}
			}
			e.sleep(6*time.Second, "past the 5 s of 6.5 row 3")
			r := e.run("print", "--refresh")
			e.wantExit(r, 0)
			crit(e, "S7.3", "print --refresh during a block sends no request", func(c *ck) {
				if refusedAt == 0 {
					c.failf("set-up: no request was refused")
				}
				if len(r.reqs) != 0 {
					c.failf("want 0 requests, the stand-in received %d (%s)", len(r.reqs), fmtCounts(bySymbol(r.reqs)))
				}
			})
			crit(e, "S7.4", "the cause line shows the time of the next attempt", func(c *ck) {
				cl := causeLines(r.stderr)
				if len(cl) != 1 {
					c.failf("want one cause line, got %q (error stream %q)", cl, r.stderr)
					return
				}
				c.notef("cause line %q", cl[0])
				if !strings.HasPrefix(cl[0], v.phrase) {
					c.failf("want the cause `%s`", v.phrase)
				}
				at := time.UnixMilli(refusedAt).Add(5 * time.Minute)
				if s, ok := hhmmIn(cl[0], at, at.Add(time.Minute)); !ok {
					c.failf("want the next attempt %s (5 minutes after the refusal at %s), found %q",
						at.UTC().Format("15:04"), time.UnixMilli(refusedAt).UTC().Format("15:04:05"), s)
				}
			})
		})
	}
}

// S7.6 [view, print] Given a closed market, when `r` is pressed, then its rows
// are requested too. B3 closed 2 h ago.
func TestVerifyS7_06_RefreshRequestsClosedMarket(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S7.6", "refresh_seconds = 10\ntickers = [\"PETR4\", \"VALE3\", \"AAPL\"]\n")
	e.setSource(state{ClosedMarkets: []string{"b3"}}, "B3 closed 2 h ago")
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.sleep(6*time.Second, "past the 5 s of 6.5 row 3")
	r := e.run("print", "--refresh")
	e.wantExit(r, 0)
	crit(e, "S7.6", "print --refresh requests the closed market's rows", func(c *ck) {
		for _, s := range []string{"PETR4.SA", "VALE3.SA"} {
			if bySymbol(r.reqs)[s] == 0 {
				c.failf("%s (B3, closed) was not requested: %s", s, fmtCounts(bySymbol(r.reqs)))
			}
		}
	})
}

// S7.7 [view, print] Given `r` was pressed two seconds ago, when `r` is
// pressed again, then no row is requested a second time.
func TestVerifyS7_07_SecondRefreshWithinTwoSeconds(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S7.7", baseConfig)
	r0 := e.run("print")
	e.wantExit(r0, 0)
	e.sleep(6*time.Second, "past the 5 s of 6.5 row 3")
	r1 := e.run("print", "--refresh")
	e.wantExit(r1, 0)
	e.sleep(2*time.Second, "two seconds after the forced fetch")
	r2 := e.run("print", "--refresh")
	e.wantExit(r2, 0)
	crit(e, "S7.7", "a second --refresh 2 s later requests no row", func(c *ck) {
		if len(r1.reqs) == 0 {
			c.failf("set-up: the first --refresh sent nothing")
		}
		if c2 := bySymbol(r2.reqs); len(r2.reqs) != 0 {
			c.failf("want no request, got %s", fmtCounts(c2))
		}
	})
}

// S7.8 [view, print] Given a not-found row tried ten minutes ago and an
// unchanged config, when `r` is pressed, then that ticker is not requested.
// Scaled: tried 12 s ago (the hourly wait has not ended either way).
func TestVerifyS7_08_RefreshSkipsNotFound(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S7.8 (scaled: tried 12 s ago)", "refresh_seconds = 10\ntickers = [\"PETR4\", \"XXXX9\"]\n")
	e.setSource(state{SymbolStatus: map[string]int{"XXXX9.SA": 404}}, "XXXX9.SA answered 404")
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.sleep(pastInterval, "the not-found row was tried 12 s ago")
	r := e.run("print", "--refresh")
	e.wantExit(r, 0)
	crit(e, "S7.8", "print --refresh does not request a not-found row", func(c *ck) {
		if bySymbol(r1.reqs)["XXXX9.SA"] == 0 {
			c.failf("set-up: XXXX9 was not tried")
		}
		if n := bySymbol(r.reqs)["XXXX9.SA"]; n != 0 {
			c.failf("XXXX9 requested %d time(s)", n)
		}
		if bySymbol(r.reqs)["PETR4.SA"] == 0 {
			c.failf("control: PETR4 was not requested by --refresh")
		}
	})
}

// ---------------------------------------------------------------------------
// S8 Not-found rows
// ---------------------------------------------------------------------------

// S8.1 [view, print] Given the ticker `XXXX9`, which the source does not know,
// when the view is drawn, then its row shows "not found" in place of the
// price.
// S8.2 [view, print] Given one not-found row among others, when a refresh
// runs, then the other rows show fresh quotes.
// S8.5 [view, print] Given a not-found row and no other problem, when the view
// is drawn, then there is no error line.
// Also QR1.
func TestVerifyS8_01_02_05_NotFound(t *testing.T) {
	t.Parallel()
	base := cfgSpec{top: []string{"PETR4", "XXXX9", "AAPL"}}
	sizes(t, base, func(t *testing.T, cfg cfgSpec) {
		e := newEnv(t, "S8.1, S8.2, S8.5 (and QR1 with 20 tickers)", cfg.String())
		e.setSource(state{SymbolStatus: map[string]int{"XXXX9.SA": 404}}, "XXXX9.SA answered 404")
		r := e.run("print")
		e.wantExit(r, 0)
		rj := e.run("print", "--json")
		e.wantExit(rj, 0)
		crit(e, "S8.1", "`not found` in place of the price", func(c *ck) {
			l := c.rowOf(r.stdout, "XXXX9")
			f := strings.Fields(l)
			if !strings.Contains(l, "not found") || hasQuote(l) {
				c.failf("want `not found` and no price, got %q", l)
			} else if len(f) < 4 || f[2] != "not" || f[3] != "found" {
				c.failf("want `not found` in the price column (after the name `—`), got %q", l)
			}
		})
		crit(e, "S8.2", "the other rows show fresh quotes", func(c *ck) {
			var doc map[string]any
			if d := parseDoc(e, rj.stdout); d != nil {
				doc = d
			}
			for _, tk := range cfg.all() {
				if tk == "XXXX9" {
					continue
				}
				if l := c.rowOf(r.stdout, tk); !hasQuote(l) || staleRE.MatchString(l) {
					c.failf("row %s: want a fresh quote, got %q", tk, l)
				}
				if bySymbol(r.reqs)[symOf(tk)] == 0 {
					c.failf("row %s was not fetched by this call", tk)
				}
				if doc != nil {
					if s := docRows(doc)[tk]["status"]; s != "fresh" {
						c.failf("JSON %s: want status fresh, got %v", tk, s)
					}
				}
			}
		})
		crit(e, "S8.5", "no error line", func(c *ck) {
			if cl := causeLines(r.stderr); len(cl) != 0 {
				c.failf("want no cause line, got %q", cl)
			}
			if cl := causeLines(rj.stderr); len(cl) != 0 {
				c.failf("--json: want no cause line, got %q", cl)
			}
			c.notef("error stream %q", r.stderr)
		})
	})
}

// S8.4 [view, print] Given a not-found row, when the config file changes, then
// the ticker is requested again without waiting for the hour.
// On `print` (scope.md): two calls; between them the config's content changes
// (AAPL added). A call in between on the unchanged config is the control.
func TestVerifyS8_04_ConfigChangeRetriesNotFound(t *testing.T) {
	t.Parallel()
	cfg := "tickers = [\"PETR4\", \"XXXX9\"]\n"
	e := newEnv(t, "S8.4", cfg)
	e.setSource(state{SymbolStatus: map[string]int{"XXXX9.SA": 404}}, "XXXX9.SA answered 404")
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.sleep(6*time.Second, "a few seconds pass")
	r2 := e.run("print")
	e.wantExit(r2, 0)
	cfg2 := "tickers = [\"PETR4\", \"XXXX9\", \"AAPL\"]\n"
	e.logf("\nCONFIG CHANGED (AAPL added)\n%s\n", indent(cfg2))
	if err := os.WriteFile(e.cfgPath, []byte(cfg2), 0o644); err != nil {
		t.Fatal(err)
	}
	r3 := e.run("print")
	e.wantExit(r3, 0)
	crit(e, "S8.4", "after the config changes, the not-found ticker is requested again", func(c *ck) {
		if bySymbol(r1.reqs)["XXXX9.SA"] == 0 {
			c.failf("set-up: XXXX9 not tried by the first call")
		}
		if n := bySymbol(r2.reqs)["XXXX9.SA"]; n != 0 {
			c.notef("control: unchanged config, XXXX9 requested %d time(s)", n)
		}
		if n := bySymbol(r3.reqs)["XXXX9.SA"]; n != 1 {
			c.failf("after the change: want XXXX9 requested once, got %d", n)
		}
	})
}

// ---------------------------------------------------------------------------
// S11.13, S11.18 More than 20 tickers
// ---------------------------------------------------------------------------

// S11.13 [view, print] Given a config with 25 tickers, when the view is drawn,
// then all 25 rows exist.
// S11.18 [view, print] Given a config with 25 tickers, when the view is drawn,
// then a warning in the status line says the tool is designed for 20.
// On `print` the warning is a line on the error stream.
func TestVerifyS11_13_18_TwentyFiveTickers(t *testing.T) {
	t.Parallel()
	all := append([]string{"PETR4", "AAPL"}, fillers...)
	cfg := cfgSpec{top: all[:25]}
	e := newEnv(t, "S11.13, S11.18", cfg.String())
	r := e.run("print")
	e.wantExit(r, 0)
	crit(e, "S11.13", "25 tickers: all 25 rows exist", func(c *ck) {
		n := 0
		for _, tk := range cfg.top {
			if len(rowLines(r.stdout, tk)) == 1 {
				n++
			} else {
				c.failf("row %s: %d lines", tk, len(rowLines(r.stdout, tk)))
			}
		}
		c.notef("%d rows", n)
	})
	crit(e, "S11.18", "a warning says the tool is designed for 20", func(c *ck) {
		var w []string
		for _, l := range lines(r.stderr) {
			if regexp.MustCompile(`\b20\b`).MatchString(l) {
				w = append(w, l)
			}
		}
		if len(w) != 1 {
			c.failf("want one line on the error stream naming 20, got %q (error stream %q)", w, r.stderr)
		} else {
			c.notef("warning %q", w[0])
		}
		if strings.Contains(r.stdout, "designed for") {
			c.failf("the warning is on standard output")
		}
	})
}

// ---------------------------------------------------------------------------
// S14 JSON
// ---------------------------------------------------------------------------

// S14.2 [print] Given a row with a quote, when `brl-stocks print --json` is
// run, then the row's object carries ticker, name, last price, day change
// value, day change percent, day low, day high and quote time.
// S14.9 [print] Given a row with a quote, when the document is read, then its
// prices are JSON numbers.
// S14.10 [print] Given a row with a quote, when the document is read, then its
// times are UTC in RFC 3339.
func TestVerifyS14_02_09_10_RowObject(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S14.2, S14.9, S14.10", baseConfig)
	r := e.run("print", "--json")
	e.wantExit(r, 0)
	doc := parseDoc(e, r.stdout)
	if doc == nil {
		return
	}
	rows := docRows(doc)
	crit(e, "S14.2", "the row object carries ticker, name, price, change, percent, low, high and quote time", func(c *ck) {
		want := map[string]map[string]any{
			"PETR4": {"ticker": "PETR4", "name": "Petrobras PN", "price": "38.42", "change": "0.31", "change_percent": "0.81", "day_low": "38.02", "day_high": "38.6"},
			"AAPL":  {"ticker": "AAPL", "name": "Apple Inc.", "price": "229.87", "change": "-1.12", "change_percent": "-0.48", "day_low": "228.9", "day_high": "231.4"},
		}
		for tk, w := range want {
			rw := rows[tk]
			if rw == nil {
				c.failf("no object for %s", tk)
				continue
			}
			for k, v := range w {
				got := fmt.Sprint(rw[k])
				if n, ok := rw[k].(json.Number); ok {
					f, _ := n.Float64()
					wf, _ := strconv.ParseFloat(v.(string), 64)
					if d := f - wf; d > 0.006 || d < -0.006 {
						c.failf("%s.%s: want about %v, got %s", tk, k, v, got)
					}
				} else if got != v {
					c.failf("%s.%s: want %v, got %v", tk, k, v, rw[k])
				}
			}
			if rw["quote_time"] == nil {
				c.failf("%s.quote_time is null", tk)
			}
		}
	})
	crit(e, "S14.9", "prices are JSON numbers", func(c *ck) {
		for tk, rw := range rows {
			for _, k := range []string{"price", "change", "change_percent", "day_low", "day_high"} {
				if _, ok := rw[k].(json.Number); !ok {
					c.failf("%s.%s: want a JSON number, got %T %v", tk, k, rw[k], rw[k])
				}
			}
		}
		ss, _ := doc["summary"].([]any)
		for _, s := range ss {
			sm, _ := s.(map[string]any)
			for _, k := range []string{"value", "change", "change_percent"} {
				if _, ok := sm[k].(json.Number); !ok {
					c.failf("summary %v.%s: want a JSON number, got %T %v", sm["id"], k, sm[k], sm[k])
				}
			}
		}
	})
	crit(e, "S14.10", "times are UTC in RFC 3339", func(c *ck) {
		chk := func(where string, v any) {
			s, ok := v.(string)
			if !ok {
				c.failf("%s: want a time string, got %T %v", where, v, v)
				return
			}
			tm, err := time.Parse(time.RFC3339Nano, s)
			if _, off := tm.Zone(); err != nil || off != 0 {
				c.failf("%s: want UTC in RFC 3339, got %q (%v)", where, s, err)
			}
		}
		chk("generated_at", doc["generated_at"])
		for tk, rw := range rows {
			chk(tk+".quote_time", rw["quote_time"])
			chk(tk+".fetched_at", rw["fetched_at"])
		}
		ss, _ := doc["summary"].([]any)
		for _, s := range ss {
			sm, _ := s.(map[string]any)
			chk(fmt.Sprint(sm["id"])+".quote_time", sm["quote_time"])
			chk(fmt.Sprint(sm["id"])+".fetched_at", sm["fetched_at"])
		}
	})
}

// S14.3 [print] Given any run with `--json`, when the document is read, then it
// has a `schema_version` of 1.
// Three runs: state ok; no cache and no connection (state error); a config
// that cannot be parsed (exit 2).
func TestVerifyS14_03_SchemaVersion(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"ok", "error_no_connection", "config_error"} {
		v := v
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			cfg := baseConfig
			if v == "config_error" {
				cfg = "tickers = [\"PETR4\"\n"
			}
			e := newEnv(t, "S14.3 ["+v+"]", cfg)
			if v == "error_no_connection" {
				e.logf("\nSTAND-IN   stopped\n")
				e.src.stop()
			}
			r := e.run("print", "--json")
			crit(e, "S14.3", "schema_version is 1", func(c *ck) {
				doc := parseDoc(e, r.stdout)
				if doc == nil {
					c.failf("no document")
					return
				}
				if n, _ := doc["schema_version"].(json.Number); n.String() != "1" {
					c.failf("want schema_version 1, got %v", doc["schema_version"])
				}
				c.notef("state %v, exit %d", doc["state"], r.exit)
			})
		})
	}
}

// S14.8 [print] Given a config file that cannot be parsed, when `brl-stocks
// print --json` is run, then the exit status is 2.
func TestVerifyS14_08_UnparsableConfigJSONExit2(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S14.8", "refresh_seconds = = 10\ntickers = [\"PETR4\"]\n")
	r := e.run("print", "--json")
	crit(e, "S14.8", "exit status 2", func(c *ck) {
		if r.exit != 2 {
			c.failf("want exit status 2, got %d", r.exit)
		}
		if len(r.reqs) != 0 {
			c.notef("%d requests were sent", len(r.reqs))
		}
	})
}

// S14.11 [print] Given a closed market, when `brl-stocks print --json` is run,
// then the rows of that market have `market_state` `closed`.
func TestVerifyS14_11_ClosedMarketState(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S14.11", "tickers = [\"PETR4\", \"AAPL\", \"MSFT\"]\n")
	e.setSource(state{ClosedMarkets: []string{"us"}}, "US closed 2 h ago")
	r := e.run("print", "--json")
	e.wantExit(r, 0)
	crit(e, "S14.11", "rows of the closed market have market_state closed", func(c *ck) {
		doc := parseDoc(e, r.stdout)
		if doc == nil {
			return
		}
		rows := docRows(doc)
		for tk, want := range map[string]string{"AAPL": "closed", "MSFT": "closed", "PETR4": "open"} {
			if got := rows[tk]["market_state"]; got != want {
				c.failf("%s: want market_state %s, got %v", tk, want, got)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// S15 Kept files
// ---------------------------------------------------------------------------

func listFiles(root string) map[string]bool {
	m := map[string]bool{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			m[p] = true
		}
		return nil
	})
	return m
}

// S15.2 [view, print] Given the tool has fetched quotes, when the files it
// keeps are listed, then they are under `~/.cache/brl-stocks/`.
// XDG_CACHE_HOME unset, HOME temporary; every file left in the check's folders
// (home, config, cache, tmp) after the run is listed.
func TestVerifyS15_02_KeptFilesUnderHomeCache(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S15.2 (XDG_CACHE_HOME unset)", baseConfig)
	e.noCacheHome = true
	before := listFiles(e.root)
	r := e.run("print")
	e.wantExit(r, 0)
	r2 := e.run("print", "--json")
	e.wantExit(r2, 0)
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
			c.failf("no file under ~/.cache/brl-stocks/ after quotes were fetched")
		}
	})
}

// S15.6 [view, print] Given a ticker was removed from the config, a refresh
// then succeeded, and the ticker is added back with no network, when the view
// is drawn, then the row shows no data.
// On `print`: VALE3 cached; removed from the config; 12 s later a `print`
// fetches the rest with success; VALE3 added back; the stand-in is stopped.
func TestVerifyS15_06_RemovedTickerForgotten(t *testing.T) {
	t.Parallel()
	cfgA := "refresh_seconds = 10\ntickers = [\"PETR4\", \"VALE3\", \"AAPL\"]\n"
	cfgB := "refresh_seconds = 10\ntickers = [\"PETR4\", \"AAPL\"]\n"
	e := newEnv(t, "S15.6", cfgA)
	r1 := e.run("print")
	e.wantExit(r1, 0)
	e.logf("\nCONFIG CHANGED (VALE3 removed)\n%s\n", indent(cfgB))
	os.WriteFile(e.cfgPath, []byte(cfgB), 0o644)
	e.sleep(pastInterval, "cache older than the interval")
	r2 := e.run("print")
	e.wantExit(r2, 0)
	e.logf("\nCONFIG CHANGED (VALE3 added back)\n%s\n", indent(cfgA))
	os.WriteFile(e.cfgPath, []byte(cfgA), 0o644)
	e.logf("\nSTAND-IN   stopped: no network\n")
	e.src.stop()
	r3 := e.run("print")
	e.wantExit(r3, 0)
	crit(e, "S15.6", "a ticker removed, then added back with no network, shows no data", func(c *ck) {
		if !hasQuote(row(e, r1.stdout, "VALE3")) {
			c.failf("set-up: VALE3 had no quote in the first call")
		}
		if bySymbol(r2.reqs)["PETR4.SA"] == 0 || bySymbol(r2.reqs)["AAPL"] == 0 {
			c.failf("set-up: the refresh after the removal did not fetch the other rows")
		}
		l := c.rowOf(r3.stdout, "VALE3")
		if f := strings.Fields(l); hasQuote(l) || len(f) < 2 || dashCount(f[1:]) != len(f)-1 {
			c.failf("want VALE3 with `—` in every data column, got %q", l)
		}
	})
}

// S15.8 [view, print] Given a cache folder that cannot be written, when the
// tool runs, then one warning says the quotes will not be kept.
// The cache folder exists, empty, mode 0555; `print` and `print --json`.
func TestVerifyS15_08_UnwritableCacheOneWarning(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	e := newEnv(t, "S15.8", baseConfig)
	dir := filepath.Join(e.cacheHome, "brl-stocks")
	os.MkdirAll(dir, 0o755)
	os.Chmod(dir, 0o555)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	r := e.run("print")
	e.wantExit(r, 0)
	rj := e.run("print", "--json")
	e.wantExit(rj, 0)
	crit(e, "S15.8", "one warning says the quotes will not be kept", func(c *ck) {
		for _, x := range []result{r, rj} {
			var w []string
			for _, l := range lines(x.stderr) {
				ll := strings.ToLower(l)
				if strings.Contains(ll, "kept") || strings.Contains(ll, "cache") {
					w = append(w, l)
				}
			}
			if len(w) != 1 {
				c.failf("want exactly one warning about the cache, got %q (error stream %q)", w, x.stderr)
			} else {
				c.notef("warning %q", w[0])
			}
		}
	})
}

// ---------------------------------------------------------------------------
// S17.4, S18 Help text and command line (with `print`)
// ---------------------------------------------------------------------------

// S17.4 [view, print] Given the help text, when it is read, then
// `BRL_STOCKS_SOURCE_URL` does not appear in it.
func TestVerifyS17_04_HelpHidesSourceVariable(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S17.4", baseConfig)
	rs := []result{e.run("print", "--help"), e.run("--help")}
	crit(e, "S17.4", "BRL_STOCKS_SOURCE_URL is not in the help text", func(c *ck) {
		for _, r := range rs {
			if strings.Contains(strings.ToUpper(r.stdout+r.stderr), "BRL_STOCKS_SOURCE_URL") {
				c.failf("the help text names the variable: %q", r.stdout+r.stderr)
			}
			if strings.TrimSpace(r.stdout+r.stderr) == "" {
				c.failf("no help text at all")
			}
		}
	})
}

// S18.1 [view, print] Given `--version`, when the tool is run, then it prints
// the version and exits with status 0. (`brl-stocks print --version`, D77.)
func TestVerifyS18_01_PrintVersion(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S18.1", baseConfig)
	r := e.run("print", "--version")
	crit(e, "S18.1", "print --version prints the version, exit 0", func(c *ck) {
		if r.exit != 0 {
			c.failf("exit status: want 0, got %d", r.exit)
		}
		out := strings.TrimSpace(r.stdout)
		if out == "" || !regexp.MustCompile(`\d|dev|version`).MatchString(strings.ToLower(out)) {
			c.failf("want the version on standard output, got %q (error stream %q)", r.stdout, r.stderr)
		}
		if strings.Contains(r.stdout, "TICKER") || len(r.reqs) != 0 {
			c.failf("printed the table or fetched quotes (%d requests)", len(r.reqs))
		}
	})
}

// S18.2 [view, print] Given `--help`, when the tool is run, then it prints the
// usage and exits with status 0. (`brl-stocks print --help`.)
func TestVerifyS18_02_PrintHelp(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S18.2", baseConfig)
	r := e.run("print", "--help")
	crit(e, "S18.2", "print --help prints the usage, exit 0", func(c *ck) {
		if r.exit != 0 {
			c.failf("exit status: want 0, got %d", r.exit)
		}
		txt := strings.ToLower(r.stdout + r.stderr)
		if !strings.Contains(txt, "usage") || !strings.Contains(txt, "print") {
			c.failf("want a usage text naming `print`, got %q", r.stdout+r.stderr)
		}
		if strings.Contains(r.stdout, "TICKER") || len(r.reqs) != 0 {
			c.failf("printed the table or fetched quotes (%d requests)", len(r.reqs))
		}
	})
}

// S18.3 [view, print] Given a flag the tool does not know, when the tool is
// run, then it exits with status 2.
// S18.4 [view, print] Given a flag the tool does not know, when the tool is
// run, then a usage message is on the error stream.
// `brl-stocks print --frobnicate` and `brl-stocks print -z`.
func TestVerifyS18_03_04_PrintUnknownFlag(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S18.3, S18.4", baseConfig)
	rs := []result{e.run("print", "--frobnicate"), e.run("print", "-z")}
	crit(e, "S18.3", "an unknown flag with print exits with status 2", func(c *ck) {
		for _, r := range rs {
			if r.exit != 2 {
				c.failf("exit status: want 2, got %d", r.exit)
			}
		}
	})
	crit(e, "S18.4", "an unknown flag with print puts a usage message on the error stream", func(c *ck) {
		for _, r := range rs {
			if !strings.Contains(strings.ToLower(r.stderr), "usage") {
				c.failf("want a usage message on the error stream, got %q", r.stderr)
			}
			if strings.Contains(r.stdout, "TICKER") {
				c.failf("the table was printed")
			}
		}
	})
}

// ---------------------------------------------------------------------------
// QR12 Privacy on the wire
// ---------------------------------------------------------------------------

// trap is a local listener that records and refuses every connection: it
// stands as HTTP_PROXY and HTTPS_PROXY, so that a request to anything but the
// loopback source would reach it.
type trap struct {
	ln   net.Listener
	mu   sync.Mutex
	seen []string
}

func startTrap(t *testing.T) *trap {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tr := &trap{ln: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.SetReadDeadline(time.Now().Add(time.Second))
			first, _ := bufio.NewReader(conn).ReadString('\n')
			tr.mu.Lock()
			tr.seen = append(tr.seen, fmt.Sprintf("%s from %s: %q", time.Now().UTC().Format(time.RFC3339Nano), conn.RemoteAddr(), first))
			tr.mu.Unlock()
			conn.Close()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return tr
}

func (tr *trap) connections() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return append([]string(nil), tr.seen...)
}

// QR12 [view, print] The tool contacts only its data source, and sends it
// nothing about the user beyond the symbols requested. Measured by recording
// the network traffic of a run.
// On `print`: `print`, `print --json`, `print --refresh`, and `--config` at
// another path, with a not-found ticker among the rows. The child also has
// USER and LOGNAME set to a marker, and HTTP(S)_PROXY / ALL_PROXY pointing at
// a trap that records every connection. Everything the stand-in receives
// (method, path, query, headers, body) is checked against: the markers, the
// verifier's own user name and host name, the check's folders, and the
// config's private content (a group name).
func TestVerifyQR12_PrivacyOnTheWire(t *testing.T) {
	t.Parallel()
	const group = "Privategroupqr12"
	cfg := "refresh_seconds = 10\ntickers = [\"PETR4\", \"XXXX9\"]\n\n[[group]]\nname = \"" + group + "\"\ntickers = [\"AAPL\", \"VALE3\"]\n"
	e := newEnv(t, "QR12", cfg)
	e.setSource(state{SymbolStatus: map[string]int{"XXXX9.SA": 404}}, "XXXX9.SA answered 404")
	tr := startTrap(t)
	proxy := "http://" + tr.ln.Addr().String()
	extra := []string{"HTTP_PROXY=" + proxy, "HTTPS_PROXY=" + proxy, "http_proxy=" + proxy, "https_proxy=" + proxy,
		"ALL_PROXY=" + proxy, "all_proxy=" + proxy, "USER=qr12marker", "LOGNAME=qr12marker"}
	other := filepath.Join(e.root, "elsewhere", "watch.toml")
	writeFile(t, other, cfg)
	var rs []result
	rs = append(rs, e.runEnv(extra, "print"))
	rs = append(rs, e.runEnv(extra, "print", "--json"))
	e.sleep(6*time.Second, "past the 5 s of 6.5 row 3")
	rs = append(rs, e.runEnv(extra, "print", "--refresh"))
	e.sleep(pastInterval, "cache older than the interval")
	rs = append(rs, e.runEnv(extra, "--config", other, "print"))
	for _, r := range rs {
		e.wantExit(r, 0)
	}
	recs := e.fullRequests()
	host, _ := os.Hostname()
	markers := []string{"qr12marker", strings.ToLower(group), strings.ToLower(e.root), strings.ToLower(e.home), "refresh_seconds", "brl-stocks/config", "watch.toml"}
	if u := os.Getenv("USER"); len(u) > 2 {
		markers = append(markers, strings.ToLower(u))
	}
	if len(host) > 2 {
		markers = append(markers, strings.ToLower(host))
	}
	allowedSyms := map[string]bool{"PETR4.SA": true, "XXXX9.SA": true, "AAPL": true, "VALE3.SA": true, "^BVSP": true, "^GSPC": true, "BRL=X": true}
	srcHost := strings.TrimPrefix(e.src.url, "http://")
	crit(e, "QR12", "only the source is contacted; it receives nothing about the user beyond the symbols", func(c *ck) {
		if len(recs) == 0 {
			c.failf("set-up: the stand-in received nothing")
		}
		seenHeaders := map[string]bool{}
		for _, q := range recs {
			where := fmt.Sprintf("request #%d (%s)", q.Seq, q.Symbol)
			if q.Method != "GET" {
				c.failf("%s: method %s", where, q.Method)
			}
			if !allowedSyms[q.Symbol] {
				c.failf("%s: a path for something other than a requested symbol", where)
			}
			if q.Host != srcHost {
				c.failf("%s: Host header %q, want %q", where, q.Host, srcHost)
			}
			if q.BodyLen != 0 {
				c.failf("%s: carries a body of %d bytes", where, q.BodyLen)
			}
			vals, _ := url.ParseQuery(q.Query)
			for k := range vals {
				if k != "interval" && k != "range" {
					c.failf("%s: query parameter %q (%q) beyond interval and range", where, k, q.Query)
				}
			}
			var all []string
			all = append(all, q.Symbol, q.Query)
			for k, vs := range q.Headers {
				switch strings.ToLower(k) {
				case "cookie", "authorization", "proxy-authorization", "referer", "from", "forwarded", "x-forwarded-for", "via":
					c.failf("%s: header %s: %q", where, k, vs)
				}
				seenHeaders[fmt.Sprintf("%s: %s", k, strings.Join(vs, ", "))] = true
				all = append(all, k)
				all = append(all, vs...)
			}
			txt := strings.ToLower(strings.Join(all, "\n"))
			for _, m := range markers {
				if strings.Contains(txt, m) {
					c.failf("%s carries %q", where, m)
				}
			}
		}
		var hs []string
		for h := range seenHeaders {
			hs = append(hs, h)
		}
		sort.Strings(hs)
		c.notef("%d requests; every header value seen:\n    %s", len(recs), strings.Join(hs, "\n    "))
		var qs []string
		seenQ := map[string]bool{}
		for _, q := range recs {
			if !seenQ[q.Query] {
				seenQ[q.Query] = true
				qs = append(qs, q.Query)
			}
		}
		c.notef("query strings seen: %q", qs)
		if conns := tr.connections(); len(conns) != 0 {
			c.failf("the proxy trap received %d connection(s): %q", len(conns), conns)
		} else {
			c.notef("the proxy trap at %s received no connection", proxy)
		}
	})
}

// S6.11 [view, print] Given every row updates and a summary entry fails, when
// the view is drawn, then the error line says the summary was not updated.
// (D81; 7.3: summary entries are not rows and are not counted; when only
// summary entries failed, the line says `summary not updated` instead of a
// number.) On `print`: empty cache, every ticker answers, ^GSPC answered 503.
func TestVerifyS6_11_SummaryEntryFails(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S6.11", baseConfig)
	e.setSource(state{SymbolStatus: map[string]int{"^GSPC": 503}}, "^GSPC (S&P 500) answered 503, every ticker 200")
	r := e.run("print")
	e.wantExit(r, 0)
	crit(e, "S6.11", "a summary entry fails, every row updates: the cause line says the summary was not updated", func(c *ck) {
		for _, tk := range tickers {
			if l := c.rowOf(r.stdout, tk); !hasQuote(l) || staleRE.MatchString(l) {
				c.failf("set-up: row %s did not update: %q", tk, l)
			}
		}
		cl := causeLines(r.stderr)
		if len(cl) != 1 {
			c.failf("want one cause line, got %q (error stream %q)", cl, r.stderr)
			return
		}
		c.notef("cause line %q", cl[0])
		if !strings.HasPrefix(cl[0], "source unreachable") {
			c.failf("cause: want `source unreachable`, got %q", cl[0])
		}
		if !strings.Contains(cl[0], "summary not updated") {
			c.failf("want `summary not updated`, got %q", cl[0])
		}
		if nums := countIn(cl[0]); len(nums) != 0 {
			c.failf("want no count of rows in place of `summary not updated`, found the numbers %v", nums)
		}
	})
}
