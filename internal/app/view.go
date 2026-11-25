package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/lucattelli/brl-stocks/internal/cache"
	"github.com/lucattelli/brl-stocks/internal/config"
	"github.com/lucattelli/brl-stocks/internal/deps"
	"github.com/lucattelli/brl-stocks/internal/engine"
	"github.com/lucattelli/brl-stocks/internal/tui"
)

const exitTerminal = 1 // the terminal could not be used once the view had it

// noTerminal is the one line for a view with no terminal (D94).
const noTerminal = "brl-stocks: the full-screen view needs a terminal; for redirected output use `brl-stocks print`"

// runView opens the full-screen view (spec 7.3): on d.Terminal when it is
// set (the view's test boundary), otherwise on the process's own terminal.
// With neither, it opens nothing (D94).
func runView(ctx context.Context, o options, d deps.Deps) int {
	if d.Terminal == nil && !d.IsTTY {
		fmt.Fprintln(d.Stderr, noTerminal)
		return exitUsage
	}
	cfg, mtime, notes, example, err := loadConfig(o.configPath, d)
	if err != nil {
		fmt.Fprintln(d.Stderr, "brl-stocks:", err)
		return exitUsage
	}
	if example != "" {
		// Short, to fit the status line (D95).
		notes = append([]string{"no config: example watchlist; put yours at " + tilde(example, d)}, notes...)
	}
	dir := cacheDir(d)
	if d.Provider == nil {
		fmt.Fprintln(d.Stderr, "brl-stocks: no data source")
		return exitNotBuilt
	}
	_, noColour := d.LookupEnv("NO_COLOR")
	now, after := d.Now, d.After
	if d.Terminal != nil && d.Terminal.Clock != nil {
		// The view's only clock (design: the view's test boundary, "Time").
		now, after = d.Terminal.Clock.Now, d.Terminal.Clock.After
	}
	opts := tui.Options{
		Engine:      &engine.Engine{P: d.Provider, C: cache.Files{Dir: dir}, Now: now, After: after},
		Cfg:         cfg,
		ConfigMTime: mtime,
		Notes:       notes,
		Unwritable:  "quotes are not kept: " + tilde(dir, d) + " cannot be written",
		Now:         now,
		Colour:      !noColour,
	}
	path := o.configPath
	if path == "" {
		path = defaultConfigPath(d)
	}
	opts.ReadConfig = configWatcher(path, d)
	opts.ConfigStamp = configStamp(path)
	if d.Terminal != nil {
		return drive(ctx, opts, d.Terminal)
	}
	opts.Ctx = ctx
	opts.Tick = func(dur time.Duration, f func(time.Time) tea.Msg) tea.Cmd {
		return func() tea.Msg { return f(<-after(dur)) }
	}
	p := tea.NewProgram(tui.New(opts), tea.WithContext(ctx))
	if _, err := p.Run(); err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(d.Stderr, "brl-stocks:", err)
		return exitTerminal
	}
	return exitOK
}

// sleep is a command's message asking the driver for one of the view's own
// waits: f's message comes back once d has passed on the clock.
type sleep struct {
	d time.Duration
	f func(time.Time) tea.Msg
}

// waiting is one of the view's own waits, held by the driver.
type waiting struct {
	ch <-chan time.Time
	f  func(time.Time) tea.Msg
}

// drive runs the view's model on a deps.Terminal the way Bubble Tea's loop
// does, without the library's program (design: the view's test boundary).
// Messages are handled one at a time; each command runs in its own
// goroutine and its message comes back to the queue; after every update the
// view becomes a frame. A command counts as running until its message has
// been handled, so the idle frame is the one that shows its result.
//
// With a Clock, the view's own waits (the heartbeat) are held by the driver,
// not by a running command, and the clock moves only on an Input with
// Elapse: while a wait falls due by its end, the clock moves to the earliest,
// fires it, and what it started settles before the next (design, "Time").
// Without a Clock there is no heartbeat, as in slice 2.
func drive(ctx context.Context, opts tui.Options, term *deps.Terminal) int {
	work, stop := context.WithCancel(ctx)
	opts.Ctx = work
	opts.Width, opts.Height = term.Width, term.Height
	clock := term.Clock
	if clock != nil {
		opts.Tick = func(d time.Duration, f func(time.Time) tea.Msg) tea.Cmd {
			return func() tea.Msg { return sleep{d, f} }
		}
	}
	var model tea.Model = tui.New(opts)
	var waits []waiting
	var until *time.Time // the end of the Elapse under way

	type result struct{ msg tea.Msg }
	results := make(chan result)
	var wg sync.WaitGroup
	running := 0
	start := func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		running++
		wg.Add(1)
		go func() {
			defer wg.Done()
			msg := cmd()
			select {
			case results <- result{msg}:
			case <-work.Done():
			}
		}()
	}
	// End: stop the commands' work and wait for them, so that nothing the
	// view started outlives Run.
	end := func() int {
		stop()
		wg.Wait()
		return exitOK
	}

	width, height, inputs := term.Width, term.Height, 0
	draw := func() {
		term.Draw(deps.Frame{
			Width: width, Height: height,
			Lines:  frameLines(model.View().Content, width, height),
			Now:    opts.Now(),
			Inputs: inputs,
			Idle:   running == 0 && until == nil,
		})
	}

	// handle runs one update; it reports whether the view quits.
	var handle func(msg tea.Msg) bool
	handle = func(msg tea.Msg) bool {
		switch msg := msg.(type) {
		case nil:
			return false
		case tea.QuitMsg:
			return true
		case tea.BatchMsg:
			for _, c := range msg {
				start(c)
			}
			return false
		case sleep:
			waits = append(waits, waiting{ch: clock.After(msg.d), f: msg.f})
			return false
		}
		var cmd tea.Cmd
		model, cmd = model.Update(msg)
		start(cmd)
		return false
	}

	// fire handles the view's waits that the clock has fired, in the order
	// they were made; it reports whether the view quits.
	fire := func() bool {
		for i := 0; i < len(waits); {
			select {
			case t := <-waits[i].ch:
				f := waits[i].f
				waits = append(waits[:i], waits[i+1:]...)
				if handle(f(t)) {
					return true
				}
				draw()
			default:
				i++
			}
		}
		return false
	}

	start(model.Init())
	draw()
	input := term.Input
	for {
		if until != nil && running == 0 {
			// Settled: move the clock to the next wait due by the end of
			// the Elapse, or to its end.
			if clock.Step(*until) {
				if fire() {
					return end()
				}
				continue
			}
			until = nil
			inputs++
			draw()
			continue
		}
		next := input
		if until != nil {
			next = nil // an Elapse ends before the next input is read
		}
		select {
		case <-ctx.Done():
			return end()
		case r := <-results:
			running--
			if handle(r.msg) {
				return end()
			}
			draw()
		case in, ok := <-next:
			if !ok {
				input = nil // nothing more will come; the view keeps running
				continue
			}
			if in.Elapse != 0 {
				if clock == nil {
					// No clock to move: counted, and nothing else.
					inputs++
					draw()
					continue
				}
				t := clock.Now()
				if in.Elapse > 0 {
					t = t.Add(in.Elapse)
				}
				until = &t // counted once the clock stands at t, settled
				continue
			}
			inputs++
			if in.Key == "" {
				width, height = in.Width, in.Height
			}
			if handle(toMsg(in)) {
				return end()
			}
			draw()
		}
	}
}

// toMsg turns a test's input into the message the library would give.
func toMsg(in deps.Input) tea.Msg {
	switch in.Key {
	case "":
		return tea.WindowSizeMsg{Width: in.Width, Height: in.Height}
	case deps.KeyUp:
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case deps.KeyDown:
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case deps.KeyLeft:
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case deps.KeyRight:
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case deps.KeyEnter:
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case deps.KeyEsc:
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case deps.KeyTab:
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case deps.KeyCtrlC:
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	r, size := utf8.DecodeRuneInString(in.Key)
	if size != len(in.Key) || r == utf8.RuneError || !unicode.IsPrint(r) {
		panic(fmt.Sprintf("deps.Input: %q is neither a named key nor one printable character", in.Key))
	}
	return tea.KeyPressMsg{Code: r, Text: in.Key}
}

// frameLines splits the view into exactly height lines, each cut to width
// cells once SGR codes are left out; the library's renderer, which would
// otherwise do it, is not in the path.
func frameLines(content string, width, height int) []string {
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, l := range lines {
		lines[i] = cut(l, width)
	}
	return lines
}

// cut keeps the first width cells of s, counting every character as one
// cell and SGR codes as none. A colour left open by the cut is reset.
func cut(s string, width int) string {
	var b strings.Builder
	cells, open := 0, false
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] == ';' || (s[j] >= '0' && s[j] <= '9')) {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				code := s[i : j+1]
				b.WriteString(code)
				open = code != "\x1b[0m" && code != "\x1b[m"
				i = j + 1
				continue
			}
		}
		if cells == width {
			break
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		b.WriteString(s[i : i+size])
		cells++
		i += size
	}
	out := b.String()
	if open {
		out += "\x1b[0m"
	}
	return strings.TrimRight(out, " ")
}

// configStamp looks at the config file through any symbolic link (D29).
func configStamp(path string) tui.ConfigStamp {
	st, err := os.Stat(path)
	if err != nil {
		return tui.ConfigStamp{}
	}
	return tui.ConfigStamp{Exists: true, Mod: st.ModTime(), Size: st.Size()}
}

// configWatcher is the view's look at its config file at each heartbeat: a
// stat, and the file read and parsed again only when the stat changed
// (design C1; spec 6.4, 7.2). It never writes the file (QR7).
func configWatcher(path string, d deps.Deps) func(tui.ConfigStamp) tui.ConfigRead {
	name := tilde(path, d)
	return func(seen tui.ConfigStamp) tui.ConfigRead {
		st := configStamp(path)
		if st == seen {
			return tui.ConfigRead{Stamp: st}
		}
		cr := tui.ConfigRead{Stamp: st, Changed: true}
		if !st.Exists {
			cr.Problem = "config " + name + " was deleted: showing the last watchlist"
			return cr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			cr.Problem = "cannot read the config " + name + ": " + oneLine(err.Error())
			return cr
		}
		cfg, notes, err := config.Parse(data)
		if err != nil {
			cr.Problem = "invalid config " + name + ": " + oneLine(err.Error()) + "; showing the last valid watchlist"
			return cr
		}
		cr.Cfg, cr.Notes = cfg, notes
		return cr
	}
}

// oneLine keeps a message on the one error line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
