//go:build verification && linux

// The full-screen view, increment 1 of slice 2 (research/build/full-screen-view/
// scope.md): `brl-stocks` with no command, run as a child on a pseudo-terminal
// of its own (pty_linux_test.go). What the child writes is read from the
// master side and fed into a terminal emulator (github.com/charmbracelet/x/vt),
// whose screen cells and colours the checks read. Keys are bytes written to
// the master. The emulator's own answers to the tool's queries are written
// back to the master, as a real terminal would. See README.md.
//
// Where several criteria observe one run, each is a subtest named after it
// (crit, from surface_verify_test.go).
package verification

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

const (
	viewCols = 120
	viewRows = 40
)

// ---------------------------------------------------------------------------
// The emulated screen.
// ---------------------------------------------------------------------------

type scell struct {
	content string
	fg, bg  color.Color
}

// screen is a snapshot of the emulator's visible screen.
type screen struct {
	at    time.Time
	alt   bool
	lines []string  // each line's text, trailing blanks removed
	xs    [][]int   // for each line, the cell column of every byte of its text
	cells [][]scell // by line, by column
}

func (s screen) text() string { return strings.Join(s.lines, "\n") }

// trimmed is the screen's text without the blank lines at the end.
func (s screen) trimmed() string { return strings.TrimRight(s.text(), "\n ") }

func (s screen) contains(sub string) bool { return strings.Contains(s.text(), sub) }

// rowLine returns the screen lines whose first word is the ticker.
func (s screen) rowLines(ticker string) []string {
	var got []string
	for _, l := range s.lines {
		if f := strings.Fields(l); len(f) > 0 && f[0] == ticker {
			got = append(got, l)
		}
	}
	return got
}

// lineOf returns the index of the first line whose first word is the ticker.
func (s screen) lineOf(ticker string) int {
	for i, l := range s.lines {
		if f := strings.Fields(l); len(f) > 0 && f[0] == ticker {
			return i
		}
	}
	return -1
}

// tokenCells returns the cells of the first occurrence of tok in line y at
// or after byte offset from.
func (s screen) tokenCells(y int, tok string, from int) []scell {
	if y < 0 || y >= len(s.lines) || from > len(s.lines[y]) {
		return nil
	}
	i := strings.Index(s.lines[y][from:], tok)
	if i < 0 {
		return nil
	}
	i += from
	seen := map[int]bool{}
	var out []scell
	for b := i; b < i+len(tok); b++ {
		x := s.xs[y][b]
		if !seen[x] {
			seen[x] = true
			out = append(out, s.cells[y][x])
		}
	}
	return out
}

// paletteIndex returns the palette index of a colour set from the terminal's
// own palette (SGR 30-37, 90-97, or 38;5;n with n < 16).
func paletteIndex(c color.Color) (int, bool) {
	switch v := c.(type) {
	case ansi.BasicColor:
		return int(v), true
	case ansi.IndexedColor:
		if v < 16 {
			return int(v), true
		}
	}
	return 0, false
}

func fmtColor(c color.Color) string {
	if c == nil {
		return "none"
	}
	if i, ok := paletteIndex(c); ok {
		names := []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}
		if i < 8 {
			return fmt.Sprintf("palette %d (%s)", i, names[i])
		}
		return fmt.Sprintf("palette %d (bright %s)", i, names[i-8])
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%T rgb(%d,%d,%d) (not the palette)", c, r>>8, g>>8, b>>8)
}

func cellsColors(cs []scell) string {
	var parts []string
	for _, c := range cs {
		parts = append(parts, fmt.Sprintf("%q fg=%s", c.content, fmtColor(c.fg)))
	}
	return strings.Join(parts, ", ")
}

// ---------------------------------------------------------------------------
// One run of the view on a pseudo-terminal.
// ---------------------------------------------------------------------------

type view struct {
	e          *env
	args       []string
	p          *pty
	cmd        *exec.Cmd
	emu        *vt.Emulator
	mu         sync.Mutex // guards emu, raw, modes
	raw        bytes.Buffer
	modes      map[string]bool // modes the tool switched on (true) or off (false), last setting
	notify     chan struct{}
	start      time.Time
	done       chan struct{} // closed when the child has exited
	err        error
	tBefore    syscall.Termios
	sumBefore  string
	reqsBefore int
	closed     bool
}

// viewResult is what one run of the view left behind.
type viewResult struct {
	exit        int
	signal      string
	dur         time.Duration
	after       screen // the screen after the tool exited
	reqs        []record
	termiosSame bool
	altEntered  bool
	altLeft     bool // the emulator is on the main screen after the exit
	cursorShown bool // the emulator shows the cursor after the exit
	modesLeftOn []string
	rawLen      int
}

// openView starts `brl-stocks <args>` on a new pseudo-terminal of viewCols x
// viewRows, with TERM=xterm-256color and extra added to the environment.
func (e *env) openView(extra []string, args ...string) *view {
	e.t.Helper()
	return e.openViewSized(viewCols, viewRows, extra, args...)
}

// openViewSized is openView on a terminal of cols x rows.
func (e *env) openViewSized(cols, rows int, extra []string, args ...string) *view {
	e.t.Helper()
	p, err := openPTY(cols, rows)
	if err != nil {
		e.t.Fatalf("pseudo-terminal: %v", err)
	}
	v := &view{e: e, args: args, p: p, emu: vt.NewEmulator(cols, rows),
		modes: map[string]bool{}, notify: make(chan struct{}, 1), done: make(chan struct{})}
	v.emu.SetCallbacks(vt.Callbacks{
		EnableMode:  func(m ansi.Mode) { v.modes[fmt.Sprintf("%v", m)] = true },
		DisableMode: func(m ansi.Mode) { v.modes[fmt.Sprintf("%v", m)] = false },
	})
	if v.tBefore, err = p.termios(); err != nil {
		e.t.Fatalf("termios: %v", err)
	}
	v.sumBefore = fileSum(e.cfgPath)
	v.reqsBefore = len(e.src.requests())

	// The tool's output, into the emulator.
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := p.master.Read(buf)
			if n > 0 {
				v.mu.Lock()
				v.raw.Write(buf[:n])
				v.emu.Write(buf[:n])
				v.mu.Unlock()
				select {
				case v.notify <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()
	// The emulator's answers to the tool's queries, back to the tool.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := v.emu.Read(buf)
			if n > 0 {
				p.master.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	cmd := exec.Command(binPath, args...)
	cmd.Env = append(append(e.environ(), "TERM=xterm-256color"), extra...)
	cmd.Dir = e.root
	cmd.Stdin, cmd.Stdout, cmd.Stderr = p.slave, p.slave, p.slave
	cmd.SysProcAttr = ttyAttr()
	v.cmd = cmd
	e.logf("\n$ brl-stocks %s   (on a pseudo-terminal %s, %dx%d)\nENV        %s TERM=xterm-256color %s\nTERMIOS BEFORE  %s\n",
		strings.Join(args, " "), p.name, cols, rows, strings.Join(e.environ()[1:], " "), strings.Join(extra, " "), fmtTermios(v.tBefore))
	v.start = time.Now()
	if err := cmd.Start(); err != nil {
		p.close()
		e.t.Fatalf("start: %v", err)
	}
	go func() {
		v.err = cmd.Wait()
		close(v.done)
	}()
	e.t.Cleanup(func() {
		if !v.closed {
			select {
			case <-v.done:
			default:
				cmd.Process.Kill()
				<-v.done
			}
			v.emu.Close()
			p.close()
		}
	})
	return v
}

func (v *view) snap() screen {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.snapLocked()
}

func (v *view) snapLocked() screen {
	s := screen{at: time.Now(), alt: v.emu.IsAltScreen()}
	w, h := v.emu.Width(), v.emu.Height()
	for y := 0; y < h; y++ {
		var b strings.Builder
		var xs []int
		row := make([]scell, w)
		for x := 0; x < w; x++ {
			c := v.emu.CellAt(x, y)
			if c == nil {
				row[x] = scell{content: " "}
			} else {
				row[x] = scell{content: c.Content, fg: c.Style.Fg, bg: c.Style.Bg}
			}
			if c != nil && c.Width == 0 {
				continue // the second column of a wide character
			}
			txt := row[x].content
			if txt == "" {
				txt = " "
			}
			b.WriteString(txt)
			for range len(txt) {
				xs = append(xs, x)
			}
		}
		line := strings.TrimRight(b.String(), " ")
		s.lines = append(s.lines, line)
		s.xs = append(s.xs, xs[:len(line)])
		s.cells = append(s.cells, row)
	}
	return s
}

func (v *view) exited() bool {
	select {
	case <-v.done:
		return true
	default:
		return false
	}
}

// waitFor waits until the screen satisfies cond, with a real-time deadline.
// On the deadline (or if the tool exits first) the check fails and the last
// screen is in the evidence.
func (v *view) waitFor(what string, timeout time.Duration, cond func(s screen) bool) (screen, bool) {
	v.e.t.Helper()
	deadline := time.Now().Add(timeout)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		s := v.snap()
		if cond(s) {
			v.e.logf("SCREEN     %s: after %s\n", what, s.at.Sub(v.start).Round(time.Millisecond))
			return s, true
		}
		if time.Now().After(deadline) || v.exited() {
			why := fmt.Sprintf("not within %s", timeout)
			if v.exited() {
				why = "the tool exited first"
			}
			v.e.failf("waiting for the screen to show %s: %s. Screen:\n%s", what, why, indent(s.trimmed()))
			return s, false
		}
		select {
		case <-v.notify:
		case <-tick.C:
		}
	}
}

// waitStable waits for cond and then until the screen has not changed for
// quiet (both within timeout).
func (v *view) waitStable(what string, timeout, quiet time.Duration, cond func(s screen) bool) (screen, bool) {
	v.e.t.Helper()
	deadline := time.Now().Add(timeout)
	s, ok := v.waitFor(what, timeout, cond)
	if !ok {
		return s, false
	}
	last, since := s.text(), time.Now()
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		cur := v.snap()
		if cur.text() != last {
			last, since = cur.text(), time.Now()
			s = cur
			continue
		}
		if time.Since(since) >= quiet && cond(cur) {
			return cur, true
		}
	}
	v.e.failf("the screen did not settle (%s) within %s", what, timeout)
	return s, false
}

// logScreen writes a screen into the evidence, with the colours of the cells
// that matter.
func (v *view) logScreen(label string, s screen, colours map[string][]scell) {
	v.e.logf("\n%s (at +%s, alternate screen: %v)\n%s\n", label, s.at.Sub(v.start).Round(time.Millisecond), s.alt, framed(s))
	keys := make([]string, 0, len(colours))
	for k := range colours {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v.e.logf("COLOURS    %s: %s\n", k, cellsColors(colours[k]))
	}
}

func framed(s screen) string {
	var b strings.Builder
	last := len(s.lines)
	for last > 0 && strings.TrimSpace(s.lines[last-1]) == "" {
		last--
	}
	for i := 0; i < last; i++ {
		fmt.Fprintf(&b, "    %2d|%s\n", i, s.lines[i])
	}
	if last == 0 {
		b.WriteString("    (blank)\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// key writes bytes to the terminal, as a key press.
func (v *view) key(b []byte, what string) {
	v.e.logf("KEY        %s (%q) at +%s\n", what, b, time.Since(v.start).Round(time.Millisecond))
	if _, err := v.p.master.Write(b); err != nil {
		v.e.failf("writing the key %s: %v", what, err)
	}
}

var (
	altOnRE  = regexp.MustCompile(`\x1b\[\?(?:[0-9;]*;)?(?:1049|1047|47)(?:;[0-9;]*)?h`)
	altOffRE = regexp.MustCompile(`\x1b\[\?(?:[0-9;]*;)?(?:1049|1047|47)(?:;[0-9;]*)?l`)
	sgrRE    = regexp.MustCompile(`\x1b\[([0-9;:]*)m`)
)

// quit presses the key and waits for the tool to exit (5 s at most), then
// reads what it left: exit status, screen, terminal mode, request record.
func (v *view) quit(b []byte, what string) viewResult {
	v.e.t.Helper()
	v.key(b, what)
	return v.finish()
}

func (v *view) finish() viewResult {
	v.e.t.Helper()
	var r viewResult
	select {
	case <-v.done:
	case <-time.After(5 * time.Second):
		v.e.failf("the tool did not exit within 5 s of the key; killed")
		v.cmd.Process.Kill()
		<-v.done
	}
	r.dur = time.Since(v.start)
	var ee *exec.ExitError
	switch {
	case v.err == nil:
	case errors.As(v.err, &ee):
		r.exit = ee.ExitCode()
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			r.signal = ws.Signal().String()
		}
	default:
		r.exit = -999
	}
	// Let the last output reach the emulator: wait until the master has been
	// quiet for 150 ms (at most 2 s).
	quietUntil := time.Now().Add(2 * time.Second)
	lastLen, since := -1, time.Now()
	for time.Now().Before(quietUntil) {
		v.mu.Lock()
		n := v.raw.Len()
		v.mu.Unlock()
		if n != lastLen {
			lastLen, since = n, time.Now()
		} else if time.Since(since) > 150*time.Millisecond {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	tAfter, err := v.p.termios()
	if err != nil {
		v.e.failf("termios after the run: %v", err)
	}
	r.termiosSame = err == nil && tAfter == v.tBefore

	v.mu.Lock()
	r.after = v.snapLocked()
	raw := v.raw.String()
	r.rawLen = len(raw)
	r.altLeft = !v.emu.IsAltScreen()
	r.cursorShown = !v.emu.CursorHidden()
	for m, on := range v.modes {
		if on && m != "25" { // 25 (cursor shown) is on in a fresh terminal; reported as cursorShown
			r.modesLeftOn = append(r.modesLeftOn, m)
		}
	}
	v.mu.Unlock()
	sort.Strings(r.modesLeftOn)
	r.altEntered = altOnRE.MatchString(raw)

	all := v.e.src.requests()
	if len(all) >= v.reqsBefore {
		r.reqs = all[v.reqsBefore:]
	}
	v.closed = true
	v.emu.Close()
	v.p.close()

	v.e.logf("EXIT       %d%s   (wall time %s)\nTERMIOS AFTER   %s   (same as before: %v)\n"+
		"TERMINAL   output %d bytes; entered the alternate screen: %v (left it: %v in the output, emulator on main screen: %v); cursor shown at the end: %v; modes left on: %v\n",
		r.exit, map[bool]string{true: " (signal " + r.signal + ")", false: ""}[r.signal != ""], r.dur.Round(time.Millisecond),
		fmtTermios(tAfter), r.termiosSame, r.rawLen, r.altEntered, altOffRE.MatchString(raw), r.altLeft, r.cursorShown, r.modesLeftOn)
	v.logScreen("SCREEN AFTER EXIT", r.after, nil)
	v.e.logf("REQUESTS RECEIVED BY THE STAND-IN DURING THIS RUN: %d   per symbol: %s\n%s",
		len(r.reqs), fmtCounts(bySymbol(r.reqs)), fmtRecords(r.reqs))
	if sumAfter := fileSum(v.e.cfgPath); sumAfter != v.sumBefore {
		v.e.failf("QR7: the config file changed during the view run: sha256 %s -> %s", v.sumBefore, sumAfter)
	}
	return r
}

// colourCodes returns the SGR sequences in the output that set a colour.
func (v *view) colourCodes() []string {
	v.mu.Lock()
	raw := v.raw.String()
	v.mu.Unlock()
	var got []string
	for _, m := range sgrRE.FindAllStringSubmatch(raw, -1) {
		ps := strings.FieldsFunc(m[1], func(r rune) bool { return r == ';' || r == ':' })
		for _, p := range ps {
			var n int
			fmt.Sscanf(p, "%d", &n)
			if (n >= 30 && n <= 38) || (n >= 40 && n <= 48) || (n >= 90 && n <= 97) || (n >= 100 && n <= 107) {
				got = append(got, fmt.Sprintf("%q", m[0]))
				break
			}
		}
	}
	return got
}

// ---------------------------------------------------------------------------
// Reading rows on the screen.
// ---------------------------------------------------------------------------

// rowsShown reports whether every ticker has exactly one line with a price.
func rowsShown(tks []string) func(s screen) bool {
	return func(s screen) bool {
		for _, tk := range tks {
			ls := s.rowLines(tk)
			if len(ls) != 1 || !hasQuote(ls[0]) {
				return false
			}
		}
		return true
	}
}

// rowsShownWith reports whether every ticker's line holds its wanted text.
func rowsShownWith(want map[string]string) func(s screen) bool {
	return func(s screen) bool {
		for tk, w := range want {
			ls := s.rowLines(tk)
			if len(ls) != 1 || !strings.Contains(ls[0], w) {
				return false
			}
		}
		return true
	}
}

// vRow is the one screen line of a ticker, split around its price.
func (c *ck) vRow(s screen, ticker string) (parsedRow, int, bool) {
	c.t.Helper()
	ls := s.rowLines(ticker)
	if len(ls) != 1 {
		c.failf("want exactly one screen line for row %s, found %d", ticker, len(ls))
		return parsedRow{}, -1, false
	}
	l := strings.TrimLeft(ls[0], " ")
	p, ok := parseRow(l, ticker)
	if !ok {
		c.failf("row %s: no price could be read in %q", ticker, ls[0])
	} else if len(p.rest) < 5 {
		c.failf("row %s: want change, percent, low, high and time after the price, got %q", ticker, p.rest)
		ok = false
	}
	return p, s.lineOf(ticker), ok
}

// changeCells returns the cells of a row's change value and percent.
func changeCells(s screen, y int, p parsedRow) ([]scell, []scell) {
	if y < 0 {
		return nil, nil
	}
	from := strings.Index(s.lines[y], p.price)
	if from < 0 {
		return nil, nil
	}
	from += len(p.price)
	val := s.tokenCells(y, p.rest[0], from)
	pct := s.tokenCells(y, p.rest[1], from+strings.Index(s.lines[y][from:], p.rest[0])+len(p.rest[0]))
	return val, pct
}

// ---------------------------------------------------------------------------
// S1 The view, its rows and number formats
// ---------------------------------------------------------------------------

// S1.1 [view] Given a config with the tickers `PETR4` and `AAPL` and a source
// that answers, when `brl-stocks` is run with no command, then a full-screen
// view shows one row for each ticker.
func TestVerifyView_S1_1_OneRowPerTicker(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S1.1 (view)", "tickers = [\"PETR4\", \"AAPL\"]\n")
	v := e.openView(nil)
	s, _ := v.waitStable("a row with a price for PETR4 and AAPL", 10*time.Second, 300*time.Millisecond, rowsShown([]string{"PETR4", "AAPL"}))
	v.logScreen("SCREEN BEFORE QUITTING", s, nil)
	r := v.quit([]byte("q"), "q")
	crit(e, "S1.1", "a full-screen view shows one row for each ticker", func(c *ck) {
		for _, tk := range []string{"PETR4", "AAPL"} {
			if ls := s.rowLines(tk); len(ls) != 1 || !hasQuote(ls[0]) {
				c.failf("want one row with a price for %s, got %q", tk, ls)
			}
		}
		if !s.alt && !r.altEntered {
			c.notef("the view did not use the alternate screen")
		}
		if r.exit != 0 {
			c.failf("exit status after q: want 0, got %d", r.exit)
		}
		c.notef("requests during the run: %s", fmtCounts(bySymbol(r.reqs)))
	})
}

// S1.2, S1.8 to S1.15 and S16.1, S16.2, on the view at full width (120
// columns), on an empty cache, with the stand-in's prices: PETR4 38.42
// (previous close 38.11, low 38.02), AAPL 229.87 (230.99), VALE3 61.07 (61.07),
// BBDC4 1038.42, ABEV3 38.425, NVDA 1229.87. Also QR1 (20 tickers).
//
// S16.1 [view, print] Given a row whose day change is positive, when it is
// drawn on a terminal, then the change is in the terminal palette's green.
// S16.2 ... negative ... the terminal palette's red.
func TestVerifyView_S1_2_S1_8_15_S16_1_2_RowFormatsAndColour(t *testing.T) {
	t.Parallel()
	base := cfgSpec{top: []string{"PETR4", "VALE3", "AAPL", "BBDC4", "ABEV3", "NVDA"}}
	sizes(t, base, func(t *testing.T, cfg cfgSpec) {
		e := newEnv(t, "S1.2, S1.8 to S1.15, S16.1, S16.2 (view; QR1 with 20 tickers)", cfg.String())
		e.setSource(state{Quotes: map[string]override{
			"BBDC4.SA": {Name: sp("Bradesco PN"), Price: fp(1038.42), PrevClose: fp(1030.00), Low: fp(1030.50), High: fp(1040.10)},
			"ABEV3.SA": {Name: sp("Ambev ON"), Price: fp(38.425), PrevClose: fp(38.00), Low: fp(38.10), High: fp(38.50)},
			"NVDA":     {Name: sp("Nvidia Corp"), Price: fp(1229.87), PrevClose: fp(1200.00), Low: fp(1210.00), High: fp(1235.00)},
		}}, "BBDC4 1038.42, ABEV3 38.425, NVDA 1229.87")
		v := e.openView(nil)
		s, _ := v.waitStable("every row with a price", 10*time.Second, 300*time.Millisecond, rowsShown(cfg.all()))
		colours := map[string][]scell{}
		for _, tk := range []string{"PETR4", "AAPL", "VALE3"} {
			if y := s.lineOf(tk); y >= 0 {
				if p, ok := parseRow(strings.TrimLeft(s.lines[y], " "), tk); ok && len(p.rest) >= 2 {
					val, pct := changeCells(s, y, p)
					colours[tk+" change "+p.rest[0]] = val
					colours[tk+" percent "+p.rest[1]] = pct
				}
			}
		}
		v.logScreen("SCREEN BEFORE QUITTING", s, colours)
		r := v.quit([]byte("q"), "q")
		e.wantExit(result{exit: r.exit}, 0)

		crit(e, "S1.2", "a fetched row shows ticker, name, last price, change value, change percent, low, high and quote time", func(c *ck) {
			var hdr []string
			for _, l := range s.lines {
				if f := strings.Fields(l); len(f) > 0 && f[0] == "TICKER" {
					hdr = append(hdr, l)
				}
			}
			if len(hdr) != 1 {
				c.failf("want one header line starting with TICKER, got %q", hdr)
			} else {
				hf := " " + strings.Join(strings.Fields(hdr[0]), " ") + " "
				for _, w := range []string{"TICKER", "NAME", "LAST", "CHANGE", "LOW", "HIGH", "TIME"} {
					if !strings.Contains(hf, " "+w+" ") {
						c.failf("header line lacks %s: %q", w, hdr[0])
					}
				}
			}
			for tk, want := range map[string][]string{
				"PETR4": {"Petrobras PN", "R$ 38,42", "+0,31", "+0,81%", "38,02", "38,60"},
				"AAPL":  {"Apple Inc.", "US$ 229.87", "-1.12", "-0.48%", "228.90", "231.40"},
			} {
				p, _, ok := c.vRow(s, tk)
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
				c.notef("row %s: %q", tk, s.rowLines(tk)[0])
			}
		})
		price := func(id, tk, want, text string) {
			crit(e, id, text, func(c *ck) {
				if p, _, ok := c.vRow(s, tk); ok && p.price != want {
					c.failf("want `%s`, got %q in %q", want, p.price, s.rowLines(tk)[0])
				} else if ok {
					c.notef("%q", s.rowLines(tk)[0])
				}
			})
		}
		price("S1.8", "BBDC4", "R$ 1.038,42", "B3 price 1038.42 reads `R$ 1.038,42`")
		price("S1.9", "NVDA", "US$ 1,229.87", "US price 1229.87 reads `US$ 1,229.87`")
		field := func(id, tk string, i int, want, text string) {
			crit(e, id, text, func(c *ck) {
				if p, _, ok := c.vRow(s, tk); ok && p.rest[i] != want {
					c.failf("want `%s`, got %q in %q", want, p.rest[i], s.rowLines(tk)[0])
				}
			})
		}
		field("S1.10", "PETR4", 0, "+0,31", "B3 change +0.31 reads `+0,31`")
		field("S1.11", "PETR4", 1, "+0,81%", "B3 change +0.81 percent reads `+0,81%`")
		field("S1.12", "AAPL", 1, "-0.48%", "US change -0.48 percent reads `-0.48%`")
		field("S1.13", "VALE3", 0, "+0,00", "B3 change of exactly zero reads `+0,00`")
		price("S1.14", "ABEV3", "R$ 38,43", "B3 price 38.425 reads `R$ 38,43`")
		crit(e, "S1.15", "B3 low 38.02 reads `38,02` with no currency symbol", func(c *ck) {
			if p, _, ok := c.vRow(s, "PETR4"); ok {
				if p.rest[2] != "38,02" {
					c.failf("want the low `38,02`, got %q", p.rest[2])
				}
				l := s.rowLines("PETR4")[0]
				if n := strings.Count(l, "R$") + strings.Count(l, "US$"); n != 1 {
					c.failf("want one currency symbol in the row (the last price's only), found %d in %q", n, l)
				}
			}
		})
		colourOf := func(id, tk string, idx []int, name, text string) {
			crit(e, id, text, func(c *ck) {
				p, y, ok := c.vRow(s, tk)
				if !ok {
					return
				}
				val, pct := changeCells(s, y, p)
				if len(val) == 0 || len(pct) == 0 {
					c.failf("set-up: the change cells of %s could not be located", tk)
					return
				}
				for _, cl := range append(append([]scell(nil), val...), pct...) {
					i, ok := paletteIndex(cl.fg)
					if !ok || (i != idx[0] && i != idx[1]) {
						c.failf("%s change %q %q: cell %q has fg %s, want the palette's %s (index %d or %d)",
							tk, p.rest[0], p.rest[1], cl.content, fmtColor(cl.fg), name, idx[0], idx[1])
						return
					}
				}
				c.notef("%s change cells: %s | %s", tk, cellsColors(val), cellsColors(pct))
			})
		}
		colourOf("S16.1", "PETR4", []int{2, 10}, "green", "a positive change is in the terminal palette's green")
		colourOf("S16.2", "AAPL", []int{1, 9}, "red", "a negative change is in the terminal palette's red")
		// 7.3 (not a numbered criterion of this increment): no colour when zero.
		if y := s.lineOf("VALE3"); y >= 0 {
			if p, ok := parseRow(strings.TrimLeft(s.lines[y], " "), "VALE3"); ok && len(p.rest) >= 2 {
				val, pct := changeCells(s, y, p)
				e.logf("OBSERVED   (7.3, zero change) VALE3: %s | %s\n", cellsColors(val), cellsColors(pct))
			}
		}
	})
}

// S1.3 [view] Given the view is open, when `q` is pressed, then the tool exits
// with status 0. 7.3: "leaving the terminal as it was found. Ctrl+C does the
// same." Asserted: exit 0; if the output entered the alternate screen it left
// it; the cursor is shown; the pseudo-terminal's termios is as before.
func TestVerifyView_S1_3_QuitLeavesTerminal(t *testing.T) {
	t.Parallel()
	for _, k := range []struct {
		name string
		b    []byte
	}{{"q", []byte("q")}, {"ctrl_c", []byte{0x03}}} {
		k := k
		t.Run(k.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, "S1.3 (view), key "+k.name, baseConfig)
			v := e.openView(nil)
			s, _ := v.waitStable("every row with a price", 10*time.Second, 300*time.Millisecond, rowsShown(tickers))
			v.logScreen("SCREEN BEFORE THE KEY", s, nil)
			r := v.quit(k.b, k.name)
			crit(e, "S1.3", "the key exits with status 0, leaving the terminal as it was found", func(c *ck) {
				if r.exit != 0 {
					c.failf("exit status: want 0, got %d (signal %q)", r.exit, r.signal)
				}
				if !r.altLeft {
					c.failf("the terminal is still on the alternate screen after the exit")
				}
				if !r.cursorShown {
					c.failf("the cursor is still hidden after the exit")
				}
				if !r.termiosSame {
					c.failf("the terminal mode (termios) differs from before the run (see TERMIOS lines)")
				}
				c.notef("alternate screen entered: %v, left: %v; cursor shown: %v; termios as before: %v; exit after %s",
					r.altEntered, r.altLeft, r.cursorShown, r.termiosSame, r.dur.Round(time.Millisecond))
				if len(r.modesLeftOn) > 0 {
					c.notef("modes the tool switched on and did not switch off: %v", r.modesLeftOn)
				}
			})
		})
	}
}

// S1.4 [view] Given the cache holds quotes for the watchlist, when the view
// starts, then the rows show the cached quotes before any answer from the
// source has arrived. The cache is left by `print`; then the source holds its
// answers for 20 s and would give other prices. The cache is older than the
// interval (10 s), so the view's fetch at start does go out.
func TestVerifyView_S1_4_CachedRowsBeforeAnswer(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S1.4 (view)", baseConfig)
	e.wantExit(e.run("print"), 0)
	e.sleep(pastInterval, "the cache becomes older than the interval")
	e.setSource(state{DelayMS: 20000, Quotes: map[string]override{
		"PETR4.SA": {Price: fp(40.00)}, "AAPL": {Price: fp(240.00)},
	}}, "answers held 20 s; PETR4 40.00, AAPL 240.00 if they ever arrive")
	cached := map[string]string{"PETR4": "R$ 38,42", "AAPL": "US$ 229.87", "VALE3": "R$ 61,07", "ITUB4": "R$ 34,15", "MSFT": "US$ 512.30"}
	v := e.openView(nil)
	s, ok := v.waitFor("the cached prices in every row", 5*time.Second, rowsShownWith(cached))
	answered := 0
	for _, q := range e.src.requests()[v.reqsBefore:] {
		if q.AnsweredMS != 0 && q.AnsweredMS <= s.at.UnixMilli() {
			answered++
		}
	}
	v.logScreen("SCREEN WITH THE CACHED ROWS", s, nil)
	r := v.quit([]byte("q"), "q")
	crit(e, "S1.4", "the rows show the cached quotes before any answer from the source has arrived", func(c *ck) {
		if !ok {
			c.failf("the cached quotes were not shown")
		}
		if answered != 0 {
			c.failf("%d answers had arrived when the cached rows were seen", answered)
		}
		c.notef("cached rows on screen at +%s; requests sent by then and still held: %d; exit %d",
			s.at.Sub(v.start).Round(time.Millisecond), len(r.reqs), r.exit)
	})
}

// S1.5 [view, print] Given no cache, when the view starts, then each row shows
// its ticker and `—` in the data columns until its first quote arrives. The
// source holds its answers for 20 s.
func TestVerifyView_S1_5_NoCacheDashes(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S1.5 (view)", baseConfig)
	e.setSource(state{DelayMS: 20000}, "answers held 20 s")
	v := e.openView(nil)
	s, ok := v.waitStable("every row with its ticker", 5*time.Second, 300*time.Millisecond, func(s screen) bool {
		for _, tk := range tickers {
			if len(s.rowLines(tk)) != 1 {
				return false
			}
		}
		return true
	})
	v.logScreen("SCREEN WHILE THE ANSWERS ARE HELD", s, nil)
	r := v.quit([]byte("q"), "q")
	crit(e, "S1.5", "each row shows its ticker and `—` in the data columns", func(c *ck) {
		if !ok {
			return
		}
		for _, tk := range tickers {
			l := s.rowLines(tk)[0]
			f := strings.Fields(l)
			if hasQuote(l) {
				c.failf("row %s shows a price before any quote: %q", tk, l)
			}
			// name, last, change, percent, low, high, time
			if n := dashCount(f[1:]); n < 7 {
				c.failf("row %s: want `—` in each of the 7 data columns, found %d in %q", tk, n, l)
			}
			for _, w := range f[1:] {
				if w != "—" {
					c.failf("row %s: a data column holds %q, not `—`: %q", tk, w, l)
					break
				}
			}
		}
		c.notef("exit %d; requests held: %d", r.exit, len(r.reqs))
	})
}

// S1.7 [view, print] Given a quote from an earlier day, when the row is drawn,
// then the quote time is shown as a date (`DD/MM`). The stand-in's US market
// closed 2 days and 1 hour ago; B3 open is the control. TZ=UTC.
func TestVerifyView_S1_7_EarlierDayDate(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S1.7 (view)", "tickers = [\"PETR4\", \"AAPL\"]\n")
	ago := int64(2*86400 + 3600)
	e.setSource(state{ClosedMarkets: []string{"us"}, ClosedAgoSeconds: ago}, "US closed 2 days and 1 hour ago")
	v := e.openView(nil)
	s, _ := v.waitStable("both rows with a price", 10*time.Second, 300*time.Millisecond, rowsShown([]string{"PETR4", "AAPL"}))
	v.logScreen("SCREEN BEFORE QUITTING", s, nil)
	v.quit([]byte("q"), "q")
	crit(e, "S1.7", "a quote from an earlier day shows its time as DD/MM", func(c *ck) {
		want := time.Unix(time.Now().Unix()-ago, 0).UTC().Format("02/01")
		if p, _, ok := c.vRow(s, "AAPL"); ok && p.rest[4] != want {
			c.failf("AAPL (quote from %s): want the time column `%s`, got %q in %q", want, want, p.rest[4], s.rowLines("AAPL")[0])
		}
		if p, _, ok := c.vRow(s, "PETR4"); ok && !regexp.MustCompile(`^\d\d:\d\d$`).MatchString(p.rest[4]) {
			c.failf("control: PETR4 (quote from today) should show HH:MM, got %q", p.rest[4])
		}
	})
}

// ---------------------------------------------------------------------------
// S3 Two markets, plain tickers, own currency
// ---------------------------------------------------------------------------

// S3.1 to S3.5 on the view: the currency of each row ("treated as B3" is read
// as priced in R$, "treated as US" as priced in US$), and the symbol the
// stand-in was asked for. 7.2: rows show the code without a market prefix.
func TestVerifyView_S3_1_5_MarketOfATicker(t *testing.T) {
	t.Parallel()
	cfg := `tickers = ["PETR4", "PETR4F", "MXRF11", "AAPL34", "AAPL", "MSFT", "F", "US:ABCD1", "B3:XYZ"]` + "\n"
	e := newEnv(t, "S3.1 to S3.5 (view)", cfg)
	shown := []string{"PETR4", "PETR4F", "MXRF11", "AAPL34", "AAPL", "MSFT", "F", "ABCD1", "XYZ"}
	v := e.openView(nil)
	s, _ := v.waitStable("every row with a price", 10*time.Second, 300*time.Millisecond, rowsShown(shown))
	v.logScreen("SCREEN BEFORE QUITTING", s, nil)
	r := v.quit([]byte("q"), "q")
	reqd := bySymbol(r.reqs)
	type want struct{ id, shown, sign string }
	ws := []want{
		{"S3.1", "PETR4", "R$"}, {"S3.2", "PETR4F", "R$"}, {"S3.2", "MXRF11", "R$"}, {"S3.2", "AAPL34", "R$"},
		{"S3.3", "AAPL", "US$"}, {"S3.3", "MSFT", "US$"}, {"S3.3", "F", "US$"},
		{"S3.4", "ABCD1", "US$"}, {"S3.5", "XYZ", "R$"},
	}
	for _, id := range []string{"S3.1", "S3.2", "S3.3", "S3.4", "S3.5"} {
		id := id
		crit(e, id, "the market and currency of a ticker", func(c *ck) {
			for _, w := range ws {
				if w.id != id {
					continue
				}
				p, _, ok := c.vRow(s, w.shown)
				if !ok {
					continue
				}
				if !strings.HasPrefix(p.price, w.sign+" ") {
					c.failf("row %s: want a price in %s, got %q", w.shown, w.sign, p.price)
				}
				c.notef("row %s: %q", w.shown, s.rowLines(w.shown)[0])
			}
		})
	}
	e.logf("\nSYMBOLS REQUESTED: %s\n", fmtCounts(reqd))
}

// S3.6 [view, print] Given a B3 row and a US row on screen, when quotes are
// shown, then no price has been converted to the other currency.
func TestVerifyView_S3_6_NoConversion(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S3.6 (view)", "tickers = [\"PETR4\", \"AAPL\"]\n")
	v := e.openView(nil)
	s, _ := v.waitStable("both rows with a price", 10*time.Second, 300*time.Millisecond, rowsShown([]string{"PETR4", "AAPL"}))
	v.logScreen("SCREEN BEFORE QUITTING", s, nil)
	v.quit([]byte("q"), "q")
	crit(e, "S3.6", "no price is converted to the other currency", func(c *ck) {
		for tk, w := range map[string][]string{"PETR4": {"R$ 38,42", "38,02", "38,60"}, "AAPL": {"US$ 229.87", "228.90", "231.40"}} {
			if p, _, ok := c.vRow(s, tk); ok && (p.price != w[0] || p.rest[2] != w[1] || p.rest[3] != w[2]) {
				c.failf("row %s: want the stand-in's own %q, got price %q low %q high %q", tk, w, p.price, p.rest[2], p.rest[3])
			}
		}
	})
}

// S3.7 [view, print] Given the ticker written as `petr4`, when the row is
// drawn, then it shows `PETR4`.
func TestVerifyView_S3_7_LowerCaseShownUpper(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S3.7 (view)", "tickers = [\"petr4\"]\n")
	v := e.openView(nil)
	s, _ := v.waitStable("a PETR4 row with a price", 10*time.Second, 300*time.Millisecond, rowsShown([]string{"PETR4"}))
	v.logScreen("SCREEN BEFORE QUITTING", s, nil)
	v.quit([]byte("q"), "q")
	crit(e, "S3.7", "`petr4` is drawn as `PETR4`", func(c *ck) {
		if ls := s.rowLines("PETR4"); len(ls) != 1 || !hasQuote(ls[0]) {
			c.failf("want one row PETR4 with a price, got %q", ls)
		}
		if s.contains("petr4") {
			c.failf("the screen shows `petr4`")
		}
	})
}

// ---------------------------------------------------------------------------
// S11.2, S15, S16.3, S16.4, S17.2
// ---------------------------------------------------------------------------

// S11.2 [view, print] Given `--config /tmp/other.toml`, when the tool starts,
// then it shows the watchlist of that file. A different watchlist is at the
// default path.
func TestVerifyView_S11_2_ConfigFlag(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S11.2 (view)", "tickers = [\"PETR4\", \"VALE3\"]\n")
	other := filepath.Join(e.root, "elsewhere", "other.toml")
	writeFile(t, other, "tickers = [\"MSFT\", \"ITUB4\"]\n")
	e.logf("OTHER CONFIG %s\n%s\n", other, indent("tickers = [\"MSFT\", \"ITUB4\"]"))
	v := e.openView(nil, "--config", other)
	s, _ := v.waitStable("the rows of the other file", 10*time.Second, 300*time.Millisecond, rowsShown([]string{"MSFT", "ITUB4"}))
	v.logScreen("SCREEN BEFORE QUITTING", s, nil)
	r := v.quit([]byte("q"), "q")
	crit(e, "S11.2", "--config shows the watchlist of that file", func(c *ck) {
		for _, tk := range []string{"MSFT", "ITUB4"} {
			if len(s.rowLines(tk)) != 1 {
				c.failf("want one row %s (from --config), got %d", tk, len(s.rowLines(tk)))
			}
		}
		for _, tk := range []string{"PETR4", "VALE3"} {
			if len(s.rowLines(tk)) != 0 {
				c.failf("row %s from the default path is shown", tk)
			}
		}
		if r.exit != 0 {
			c.failf("exit status %d", r.exit)
		}
	})
}

// S15.1 [view, print] Given a run that fetched quotes and then ended, when the
// tool is started again with no network, then the rows show those quotes. Both
// runs are the view; "no network" is the stand-in stopped (its port refuses
// connections).
func TestVerifyView_S15_1_QuotesKeptWithoutNetwork(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S15.1 (view)", baseConfig)
	want := map[string]string{"PETR4": "R$ 38,42", "AAPL": "US$ 229.87", "VALE3": "R$ 61,07", "ITUB4": "R$ 34,15", "MSFT": "US$ 512.30"}
	v1 := e.openView(nil)
	s1, _ := v1.waitStable("every row with its fetched price", 10*time.Second, 300*time.Millisecond, rowsShownWith(want))
	v1.logScreen("FIRST RUN, SCREEN BEFORE QUITTING", s1, nil)
	v1.quit([]byte("q"), "q")
	e.logf("\nSTAND-IN   stopped: its address now refuses connections\n")
	e.src.stop()
	v2 := e.openView(nil)
	s2, ok := v2.waitStable("every row with the kept price", 10*time.Second, 300*time.Millisecond, rowsShownWith(want))
	v2.logScreen("SECOND RUN (NO NETWORK), SCREEN BEFORE QUITTING", s2, nil)
	v2.quit([]byte("q"), "q")
	crit(e, "S15.1", "with no network, the rows show the quotes of the earlier run", func(c *ck) {
		if !ok {
			c.failf("the kept quotes were not all shown (screen in the evidence)")
		}
	})
}

// S15.4 [view, print] Given a cache file emptied or filled with garbage, when
// the tool starts, then it shows the watchlist as it does with no cache.
// S15.7 ... then no message about the cache is shown.
// A view run leaves the cache; each file in the cache folder is damaged; the
// source then holds its answers, and the screen is compared with a view run on
// no cache against the same held source.
func TestVerifyView_S15_4_7_DamagedCache(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"emptied", "garbage"} {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			settled := func(s screen) bool {
				for _, tk := range tickers {
					if len(s.rowLines(tk)) != 1 {
						return false
					}
				}
				return true
			}
			base := newEnv(t, "S15.4, S15.7 (view) ["+kind+"] baseline: no cache", baseConfig)
			base.setSource(state{DelayMS: 20000}, "answers held 20 s")
			bv := base.openView(nil)
			bs, _ := bv.waitStable("every row", 10*time.Second, 500*time.Millisecond, settled)
			bv.logScreen("BASELINE SCREEN (NO CACHE)", bs, nil)
			bv.quit([]byte("q"), "q")

			e := newEnv(t, "S15.4, S15.7 (view) ["+kind+"]", baseConfig)
			v1 := e.openView(nil)
			v1.waitStable("every row with a price", 10*time.Second, 300*time.Millisecond, rowsShown(tickers))
			v1.quit([]byte("q"), "q")
			n := 0
			filepath.WalkDir(e.cacheHome, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				content := []byte{}
				if kind == "garbage" {
					content = []byte("\x00\xff{{{ not a cache ]]] \x01\x02 garbage\n")
				}
				if err := os.WriteFile(p, content, 0o644); err != nil {
					t.Fatal(err)
				}
				e.logf("DAMAGED    %s (%s)\n", strings.TrimPrefix(p, e.root), kind)
				n++
				return nil
			})
			if n == 0 {
				e.failf("set-up: no cache file found to damage")
			}
			e.setSource(state{DelayMS: 20000}, "answers held 20 s")
			v := e.openView(nil)
			s, _ := v.waitStable("every row", 10*time.Second, 500*time.Millisecond, settled)
			v.logScreen("SCREEN WITH THE DAMAGED CACHE", s, nil)
			r := v.quit([]byte("q"), "q")
			crit(e, "S15.4", "a damaged cache shows the watchlist as with no cache", func(c *ck) {
				if s.trimmed() != bs.trimmed() {
					c.failf("the screen differs from a run with no cache:\n--- no cache\n%s\n--- damaged cache\n%s", framed(bs), framed(s))
				}
			})
			crit(e, "S15.7", "no message about the cache is shown", func(c *ck) {
				for _, sc := range []screen{s, r.after} {
					if strings.Contains(strings.ToLower(sc.text()), "cache") {
						c.failf("the screen mentions the cache:\n%s", framed(sc))
					}
				}
			})
		})
	}
}

// S16.3 [view, print] Given `NO_COLOR` is set, when the view is drawn, then it
// contains no colour codes. S16.4 Given colour is off, when a row is drawn,
// then the day change still starts with `+` or `-`. Also QR1.
func TestVerifyView_S16_3_4_NoColor(t *testing.T) {
	t.Parallel()
	base := cfgSpec{top: []string{"PETR4", "VALE3", "AAPL"}}
	sizes(t, base, func(t *testing.T, cfg cfgSpec) {
		e := newEnv(t, "S16.3, S16.4 (view, NO_COLOR=1; QR1 with 20 tickers)", cfg.String())
		v := e.openView([]string{"NO_COLOR=1"})
		s, _ := v.waitStable("every row with a price", 10*time.Second, 300*time.Millisecond, rowsShown(cfg.all()))
		v.logScreen("SCREEN BEFORE QUITTING", s, nil)
		codes := v.colourCodes()
		var coloured []string
		for y, row := range s.cells {
			for x, cl := range row {
				if cl.fg != nil || cl.bg != nil {
					coloured = append(coloured, fmt.Sprintf("(%d,%d) %q fg=%s bg=%s", x, y, cl.content, fmtColor(cl.fg), fmtColor(cl.bg)))
				}
			}
		}
		v.quit([]byte("q"), "q")
		crit(e, "S16.3", "with NO_COLOR the view contains no colour codes", func(c *ck) {
			if len(codes) > 0 {
				c.failf("the output holds %d colour sequences, e.g. %v", len(codes), codes[:min(5, len(codes))])
			}
			if len(coloured) > 0 {
				c.failf("%d cells on the screen have a colour, e.g. %v", len(coloured), coloured[:min(5, len(coloured))])
			}
		})
		crit(e, "S16.4", "with colour off the change starts with + or -", func(c *ck) {
			for tk, sign := range map[string]string{"PETR4": "+", "VALE3": "+", "AAPL": "-"} {
				if p, _, ok := c.vRow(s, tk); ok && (!strings.HasPrefix(p.rest[0], sign) || !strings.HasPrefix(p.rest[1], sign)) {
					c.failf("row %s: want the change and percent to start with %s, got %q %q", tk, sign, p.rest[0], p.rest[1])
				}
			}
		})
	})
}

// S17.2 [view, print] Given a stand-in source put in place of the default one,
// when the tool runs with an unchanged config file, then the same rows are
// shown with the stand-in's quotes. Every check runs against the stand-in;
// here its prices are made distinctive.
func TestVerifyView_S17_2_StandInQuotes(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S17.2 (view)", baseConfig)
	e.setSource(state{Quotes: map[string]override{
		"PETR4.SA": {Price: fp(12.34), PrevClose: fp(12.00), Low: fp(11.11), High: fp(13.13)},
		"MSFT":     {Price: fp(98.76), PrevClose: fp(99.00), Low: fp(97.97), High: fp(99.99)},
	}}, "PETR4 12.34, MSFT 98.76")
	want := map[string]string{"PETR4": "R$ 12,34", "AAPL": "US$ 229.87", "VALE3": "R$ 61,07", "ITUB4": "R$ 34,15", "MSFT": "US$ 98.76"}
	v := e.openView(nil)
	s, ok := v.waitStable("every row with the stand-in's price", 10*time.Second, 300*time.Millisecond, rowsShownWith(want))
	v.logScreen("SCREEN BEFORE QUITTING", s, nil)
	v.quit([]byte("q"), "q")
	crit(e, "S17.2", "the rows show the stand-in's quotes", func(c *ck) {
		if !ok {
			c.failf("not every row showed the stand-in's price %v", want)
		}
		for _, w := range []string{"Brasil", "US"} {
			found := false
			for _, l := range s.lines {
				if strings.TrimSpace(l) == w {
					found = true
				}
			}
			if !found {
				c.notef("group header %q not seen on a line of its own", w)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// S17.4 and S18, without `print`: plain commands, no terminal.
// ---------------------------------------------------------------------------

// S17.4 [view, print] Given the help text, when it is read, then
// `BRL_STOCKS_SOURCE_URL` does not appear in it. (`brl-stocks --help`.)
func TestVerifyView_S17_4_HelpHidesSourceVariable(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S17.4 (view: --help without print)", baseConfig)
	r := e.run("--help")
	crit(e, "S17.4", "BRL_STOCKS_SOURCE_URL is not in the help text", func(c *ck) {
		if strings.Contains(strings.ToUpper(r.stdout+r.stderr), "BRL_STOCKS_SOURCE_URL") {
			c.failf("the help text names the variable: %q", r.stdout+r.stderr)
		}
		if strings.TrimSpace(r.stdout+r.stderr) == "" {
			c.failf("no help text at all")
		}
	})
}

// S18.1 [view] Given `--version`, when the tool is run, then it prints the
// version and exits with status 0. (`brl-stocks --version`, D77.)
func TestVerifyView_S18_1_Version(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S18.1 (view: --version without print)", baseConfig)
	r := e.run("--version")
	crit(e, "S18.1", "--version prints the version, exit 0", func(c *ck) {
		if r.exit != 0 {
			c.failf("exit status: want 0, got %d", r.exit)
		}
		out := strings.TrimSpace(r.stdout)
		if out == "" || !regexp.MustCompile(`\d|dev|version`).MatchString(strings.ToLower(out)) {
			c.failf("want the version on standard output, got %q (error stream %q)", r.stdout, r.stderr)
		}
		if strings.Contains(r.stdout, "TICKER") || len(r.reqs) != 0 {
			c.failf("drew rows or fetched quotes (%d requests)", len(r.reqs))
		}
		c.notef("standard output %q", r.stdout)
	})
}

// S18.2 [view] Given `--help`, when the tool is run, then it prints the usage
// and exits with status 0. (`brl-stocks --help`.)
func TestVerifyView_S18_2_Help(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S18.2 (view: --help without print)", baseConfig)
	r := e.run("--help")
	crit(e, "S18.2", "--help prints the usage, exit 0", func(c *ck) {
		if r.exit != 0 {
			c.failf("exit status: want 0, got %d", r.exit)
		}
		if !strings.Contains(strings.ToLower(r.stdout+r.stderr), "usage") {
			c.failf("want a usage text, got %q", r.stdout+r.stderr)
		}
		if strings.Contains(r.stdout, "TICKER") || len(r.reqs) != 0 {
			c.failf("drew rows or fetched quotes (%d requests)", len(r.reqs))
		}
	})
}

// S18.3 [view] Given a flag the tool does not know, when the tool is run, then
// it exits with status 2. S18.4 ... a usage message is on the error stream.
// `brl-stocks --frobnicate` and `brl-stocks -z`.
func TestVerifyView_S18_3_4_UnknownFlag(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "S18.3, S18.4 (view: without print)", baseConfig)
	rs := []result{e.run("--frobnicate"), e.run("-z")}
	crit(e, "S18.3", "an unknown flag exits with status 2", func(c *ck) {
		for _, r := range rs {
			if r.exit != 2 {
				c.failf("exit status: want 2, got %d", r.exit)
			}
		}
	})
	crit(e, "S18.4", "an unknown flag puts a usage message on the error stream", func(c *ck) {
		for _, r := range rs {
			if !strings.Contains(strings.ToLower(r.stderr), "usage") {
				c.failf("want a usage message on the error stream, got %q", r.stderr)
			}
			if strings.Contains(r.stdout, "TICKER") || len(r.reqs) != 0 {
				c.failf("drew rows or fetched quotes")
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Quality requirements on the view
// ---------------------------------------------------------------------------

// QR2 The view starts with data: with a cache present, rows show cached quotes
// within 300 ms of launch. Measured from process start until the rows are on
// the emulated screen, 20 tickers, median of 10 runs. The cache is left by
// `print`; the source then holds its answers, so every price on the screen
// comes from the cache. Not parallel: measured while no other check runs.
func TestVerifyView_QR2_CachedRowsWithin300ms(t *testing.T) {
	cfg, _ := config20()
	e := newEnv(t, "QR2 (view)", cfg)
	e.wantExit(e.run("print"), 0)
	tks := regexp.MustCompile(`"([A-Z0-9]+)"`).FindAllStringSubmatch(cfg, -1)
	var names []string
	for _, m := range tks {
		if m[1] != "Brasil" && m[1] != "US" {
			names = append(names, m[1])
		}
	}
	e.setSource(state{DelayMS: 20000}, "answers held 20 s: prices on screen come from the cache")
	var ds []time.Duration
	for i := 0; i < 10; i++ {
		v := e.openView(nil)
		s, ok := v.waitFor(fmt.Sprintf("run %d: all 20 rows with a cached price", i+1), 5*time.Second, rowsShown(names))
		if ok {
			ds = append(ds, s.at.Sub(v.start))
		}
		if i == 0 {
			v.logScreen("RUN 1, SCREEN WITH THE CACHED ROWS", s, nil)
		}
		v.quit([]byte("q"), "q")
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	crit(e, "QR2", "median time from process start to the 20 cached rows on screen, 10 runs, at most 300 ms", func(c *ck) {
		if len(ds) != 10 {
			c.failf("only %d of 10 runs showed the rows", len(ds))
			return
		}
		med := (ds[4] + ds[5]) / 2
		c.notef("sorted: %v; median %s", ds, med.Round(time.Millisecond))
		if med > 300*time.Millisecond {
			c.failf("median %s, want at most 300 ms", med.Round(time.Millisecond))
		}
	})
}

// QR7 The config is never touched: a view run (quit with q, and with Ctrl+C)
// through a config folder and a config file that are both symbolic links.
// Every view run of every check also compares the config's checksum.
func TestVerifyView_QR7_ConfigNeverTouched(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "QR7 (view)", "")
	realDir := filepath.Join(e.root, "dotfiles", "brl-stocks")
	realFile := filepath.Join(e.root, "dotfiles", "watchlist.toml")
	linkDir := filepath.Join(e.cfgHome, "brl-stocks")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, realFile, baseConfig)
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
		for _, d := range []string{realDir, filepath.Dir(realFile)} {
			ents, _ := os.ReadDir(d)
			for _, en := range ents {
				fmt.Fprintf(&b, "entry in %s: %s\n", strings.TrimPrefix(d, e.root), en.Name())
			}
		}
		return b.String()
	}
	before := snap()
	e.logf("\nBEFORE\n%s", indent(before))
	var afters []string
	for _, k := range []struct {
		name string
		b    []byte
		args []string
	}{{"q", []byte("q"), nil}, {"ctrl_c", []byte{0x03}, nil}, {"q, --config naming the link", []byte("q"), []string{"--config", filepath.Join(linkDir, "config.toml")}}} {
		v := e.openView(nil, k.args...)
		s, _ := v.waitStable("every row with a price", 10*time.Second, 300*time.Millisecond, rowsShown(tickers))
		v.logScreen("SCREEN BEFORE "+k.name, s, nil)
		v.quit(k.b, k.name)
		afters = append(afters, snap())
	}
	e.logf("\nAFTER\n%s", indent(afters[len(afters)-1]))
	crit(e, "QR7", "the config file and its links are unchanged after view runs", func(c *ck) {
		for i, a := range afters {
			if a != before {
				c.failf("after view run %d: changed:\n--- before\n%s--- after\n%s", i+1, before, a)
			}
		}
	})
}

// QR12 Privacy on the wire, for view runs: the same recording and proxy trap
// as TestVerifyQR12_PrivacyOnTheWire. Two view runs (the default path, and
// --config at another path), with a not-found ticker among the rows.
func TestVerifyView_QR12_PrivacyOnTheWire(t *testing.T) {
	t.Parallel()
	const group = "Privategroupqr12"
	cfg := "refresh_seconds = 10\ntickers = [\"PETR4\", \"XXXX9\"]\n\n[[group]]\nname = \"" + group + "\"\ntickers = [\"AAPL\", \"VALE3\"]\n"
	e := newEnv(t, "QR12 (view)", cfg)
	e.setSource(state{SymbolStatus: map[string]int{"XXXX9.SA": 404}}, "XXXX9.SA answered 404")
	tr := startTrap(t)
	proxy := "http://" + tr.ln.Addr().String()
	extra := []string{"HTTP_PROXY=" + proxy, "HTTPS_PROXY=" + proxy, "http_proxy=" + proxy, "https_proxy=" + proxy,
		"ALL_PROXY=" + proxy, "all_proxy=" + proxy, "USER=qr12marker", "LOGNAME=qr12marker"}
	other := filepath.Join(e.root, "elsewhere", "watch.toml")
	writeFile(t, other, cfg)
	for _, args := range [][]string{nil, {"--config", other}} {
		v := e.openView(extra, args...)
		s, _ := v.waitStable("the rows with a price", 10*time.Second, 300*time.Millisecond, rowsShown([]string{"PETR4", "AAPL", "VALE3"}))
		v.logScreen("SCREEN BEFORE QUITTING", s, nil)
		v.quit([]byte("q"), "q")
		e.sleep(pastInterval, "the cache becomes older than the interval, so the next run fetches")
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
	allowed := map[string]bool{"PETR4.SA": true, "XXXX9.SA": true, "AAPL": true, "VALE3.SA": true, "^BVSP": true, "^GSPC": true, "BRL=X": true}
	srcHost := strings.TrimPrefix(e.src.url, "http://")
	crit(e, "QR12", "only the source is contacted; it receives nothing about the user beyond the symbols", func(c *ck) {
		if len(recs) == 0 {
			c.failf("set-up: the stand-in received nothing")
		}
		seen := map[string]bool{}
		for _, q := range recs {
			where := fmt.Sprintf("request #%d (%s)", q.Seq, q.Symbol)
			if q.Method != "GET" {
				c.failf("%s: method %s", where, q.Method)
			}
			if !allowed[q.Symbol] {
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
			all := []string{q.Symbol, q.Query}
			for k, vs := range q.Headers {
				switch strings.ToLower(k) {
				case "cookie", "authorization", "proxy-authorization", "referer", "from", "forwarded", "x-forwarded-for", "via":
					c.failf("%s: header %s: %q", where, k, vs)
				}
				seen[fmt.Sprintf("%s: %s", k, strings.Join(vs, ", "))] = true
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
		for h := range seen {
			hs = append(hs, h)
		}
		sort.Strings(hs)
		c.notef("%d requests; every header value seen:\n    %s", len(recs), strings.Join(hs, "\n    "))
		if conns := tr.connections(); len(conns) != 0 {
			c.failf("the proxy trap received %d connection(s): %q", len(conns), conns)
		} else {
			c.notef("the proxy trap at %s received no connection", proxy)
		}
	})
}
