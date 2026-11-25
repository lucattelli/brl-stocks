package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/lucattelli/brl-stocks/internal/cache"
	"github.com/lucattelli/brl-stocks/internal/deps"
	"github.com/lucattelli/brl-stocks/internal/engine"
	"github.com/lucattelli/brl-stocks/internal/tui"
)

const exitTerminal = 1 // the terminal could not be used

// runView opens the full-screen view (spec 7.3): on d.Terminal when it is
// set (the view's test boundary), otherwise on the process's own terminal.
func runView(ctx context.Context, o options, d deps.Deps) int {
	cfg, mtime, notes, err := loadConfig(o.configPath, d)
	if err != nil {
		fmt.Fprintln(d.Stderr, "brl-stocks:", err)
		return exitUsage
	}
	if d.Provider == nil {
		fmt.Fprintln(d.Stderr, "brl-stocks: no data source")
		return exitNotBuilt
	}
	_, noColour := d.LookupEnv("NO_COLOR")
	opts := tui.Options{
		Engine:      &engine.Engine{P: d.Provider, C: cache.Files{Dir: cacheDir(d)}, Now: d.Now, After: d.After},
		Cfg:         cfg,
		ConfigMTime: mtime,
		Notes:       notes,
		Now:         d.Now,
		Colour:      !noColour,
	}
	if d.Terminal != nil {
		return drive(ctx, opts, d.Terminal)
	}
	opts.Ctx = ctx
	p := tea.NewProgram(tui.New(opts), tea.WithContext(ctx))
	if _, err := p.Run(); err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(d.Stderr, "brl-stocks:", err)
		return exitTerminal
	}
	return exitOK
}

// drive runs the view's model on a deps.Terminal the way Bubble Tea's loop
// does, without the library's program (design: the view's test boundary).
// Messages are handled one at a time; each command runs in its own
// goroutine and its message comes back to the queue; after every update the
// view becomes a frame. A command counts as running until its message has
// been handled, so the idle frame is the one that shows its result.
func drive(ctx context.Context, opts tui.Options, term *deps.Terminal) int {
	work, stop := context.WithCancel(ctx)
	opts.Ctx = work
	opts.Width, opts.Height = term.Width, term.Height
	var model tea.Model = tui.New(opts)

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
			Inputs: inputs,
			Idle:   running == 0,
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
		}
		var cmd tea.Cmd
		model, cmd = model.Update(msg)
		start(cmd)
		return false
	}

	start(model.Init())
	draw()
	input := term.Input
	for {
		select {
		case <-ctx.Done():
			return end()
		case r := <-results:
			running--
			if handle(r.msg) {
				return end()
			}
			draw()
		case in, ok := <-input:
			if !ok {
				input = nil // nothing more will come; the view keeps running
				continue
			}
			inputs++
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
