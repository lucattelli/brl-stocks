// Package tui is the full-screen view: a Bubble Tea 2 model whose update
// step maps messages to state and commands, and does no file or network
// access itself (D57, D58). Reading the cache and running a refresh are
// commands; their results come back as messages.
package tui

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/lucattelli/brl-stocks/internal/cache"
	"github.com/lucattelli/brl-stocks/internal/config"
	"github.com/lucattelli/brl-stocks/internal/engine"
	"github.com/lucattelli/brl-stocks/internal/market"
	"github.com/lucattelli/brl-stocks/internal/render"
)

// Deadline is how long the view's round of fetches may take (design C16).
const Deadline = 10 * time.Second

// Options is what the view is opened with.
type Options struct {
	Ctx         context.Context // ends the commands' work; the view's own life is the caller's
	Engine      *engine.Engine
	Cfg         config.Config
	ConfigMTime time.Time
	Notes       []string // warnings about the config, for the status line, already short (D95)
	Unwritable  string   // the warning for a cache folder that cannot be written, already short (D95)
	Now         func() time.Time
	Colour      bool // NO_COLOR is not set (D25)
	Width       int  // the size when the view opens; 0 until the terminal says
	Height      int
}

// Model is the state of the view.
type Model struct {
	o      Options
	width  int
	height int
	snap   market.Snapshot
	health market.Health
	// unwritable: the cache folder could not be written at the last cycle
	unwritable bool
	sort       render.Sort
	top        int  // the first line of the list on screen
	help       bool // the list of keys is shown
}

// New makes the model of a view about to open.
func New(o Options) Model {
	if o.Ctx == nil {
		o.Ctx = context.Background()
	}
	return Model{o: o, width: o.Width, height: o.Height, snap: market.Snapshot{}}
}

// The messages of the view.
type (
	// cacheLoaded carries what the cache held when the view opened.
	cacheLoaded struct {
		snap   market.Snapshot
		health market.Health
	}
	// cycleDone carries the result of a refresh.
	cycleDone struct{ res engine.Result }
)

// Init reads the cache; its rows are shown before anything is fetched (D20, QR2).
func (m Model) Init() tea.Cmd {
	files, name := m.o.Engine.C, m.o.Engine.P.Name()
	return func() tea.Msg {
		return cacheLoaded{snap: files.Load(), health: files.LoadSource(name)}
	}
}

// Update handles one message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.top = m.clampTop()
	case tea.KeyPressMsg:
		key := msg.String()
		switch {
		case key == "ctrl+c":
			return m, tea.Quit
		case m.help:
			m.help = false // any key closes the help (7.3)
		case key == "q":
			return m, tea.Quit
		case key == "?":
			m.help = true
		case key == "s":
			m.sort = (m.sort + 1) % 3
		// Scrolling moves the list by one screen line, group headers
		// included; left and right do nothing (Q85, Q84: the recommended
		// answers, pending).
		case key == "down" || key == "j":
			m.top++
		case key == "up" || key == "k":
			m.top--
		}
		m.top = m.clampTop()
	case cacheLoaded:
		m.snap = merge(m.snap, msg.snap)
		m.health = msg.health
		return m, m.cycle()
	case cycleDone:
		m.snap = merge(m.snap, msg.res.Snap)
		m.health = msg.res.Health
		if w := msg.res.Warn; w != nil && !errors.Is(w, cache.ErrBusy) {
			m.unwritable = true
		}
	}
	return m, nil
}

// cycle is one refresh through the shared path (D53). The view may fetch
// when the cache cannot be written, pacing itself from memory (D60).
func (m Model) cycle() tea.Cmd {
	eng, ctx := m.o.Engine, m.o.Ctx
	req := engine.Request{
		Want: m.o.Cfg.Want(), Interval: m.o.Cfg.Interval, ConfigMTime: m.o.ConfigMTime,
		Deadline: Deadline, MayFetchUnpaced: true,
	}
	return func() tea.Msg {
		res := eng.Cycle(ctx, req)
		if res.Warn == nil {
			// A cycle with nothing due writes nothing, so it cannot have
			// found out; the warning comes whenever the folder cannot be
			// written (7.7).
			res.Warn = eng.C.Writable()
		}
		return cycleDone{res: res}
	}
}

// merge combines the rows in memory with the rows read or fetched, and never
// replaces a quote with nothing (C4, C17): the quote and its fetch time come
// from the copy with the later success, the last attempt and the failures
// from the copy with the later attempt (protocol rule 7).
func merge(mem, read market.Snapshot) market.Snapshot {
	out := market.Snapshot{}
	for in, e := range mem {
		out[in] = e
	}
	for in, r := range read {
		e, ok := out[in]
		if !ok {
			out[in] = r
			continue
		}
		if r.Quote != nil && (e.Quote == nil || !r.FetchedAt.Before(e.FetchedAt)) {
			e.Quote, e.FetchedAt = r.Quote, r.FetchedAt
		}
		if !r.LastAttempt.Before(e.LastAttempt) {
			e.LastAttempt, e.Failures, e.Fail = r.LastAttempt, r.Failures, r.Fail
			e.ConfigMTime = r.ConfigMTime
		}
		e.ClaimedAt, e.ClaimID = r.ClaimedAt, r.ClaimID
		out[in] = e
	}
	return out
}

// helpText is the list of keys (7.3).
var helpText = []string{
	"keys",
	"",
	"  q        quit (Ctrl+C too)",
	"  r        refresh now",
	"  s        sort: file order, day change, ticker",
	"  ↑ / k    scroll up one line",
	"  ↓ / j    scroll down one line",
	"  ?        this help; any key closes it",
}

func (m Model) render() render.View {
	return render.View{Cfg: m.o.Cfg, Snap: m.snap, Health: m.health, Now: m.o.Now(),
		Colour: m.o.Colour, Sort: m.sort, Width: m.width}
}

// layout splits the screen: the fixed lines at the top (summary line and
// column header, which do not scroll, D11), the list that scrolls, and the
// foot (error line and status line).
func (m Model) layout() (head, list, foot []string) {
	v := m.render()
	if len(v.Cfg.Summary) > 0 {
		head = append(head, render.SummaryLine(v), "")
	}
	table := render.Lines(v)
	head = append(head, table[0])
	list = table[1:]
	if m.help {
		head, list = nil, helpText
	}
	if c := render.Cause(v); c != "" {
		foot = append(foot, c)
	}
	warnings := append([]string{}, m.o.Notes...)
	if m.unwritable {
		// Said once, in the status line, for as long as it holds (7.7, D60).
		warnings = append(warnings, m.o.Unwritable)
	}
	foot = append(foot, m.statusLine(render.MarketLine(v), warnings))
	return head, list, foot
}

// statusLine is the market states, the current sort and the warnings. When
// the whole line does not fit, the sort is left out before a warning is
// cut (D95).
func (m Model) statusLine(markets string, warnings []string) string {
	whole := strings.Join(append([]string{markets, "sort: " + m.sort.String()}, warnings...), " · ")
	if m.width <= 0 || utf8.RuneCountInString(whole) <= m.width || len(warnings) == 0 {
		return whole
	}
	return strings.Join(append([]string{markets}, warnings...), " · ")
}

// room is how many lines of the list fit on the screen.
func (m Model) room(head, foot []string) int {
	n := m.height - len(head) - len(foot)
	if n < 0 {
		return 0
	}
	return n
}

// clampTop keeps the scroll inside the list: no blank screen past its end.
func (m Model) clampTop() int {
	if m.help || m.height <= 0 {
		return m.top
	}
	head, list, foot := m.layout()
	top, most := m.top, len(list)-m.room(head, foot)
	if top > most {
		top = most
	}
	if top < 0 {
		top = 0
	}
	return top
}

// View draws the screen, top to bottom: summary line, column header, rows
// under their group headers, then at the foot the error line (only when
// there is a problem) and the status line (spec 7.3).
func (m Model) View() tea.View {
	head, list, foot := m.layout()
	var lines []string
	if m.height > 0 {
		if !m.help {
			top := m.clampTop()
			list = list[top:]
		}
		if r := m.room(head, foot); len(list) > r {
			list = list[:r]
		}
		lines = append(append([]string{}, head...), list...)
		for len(lines)+len(foot) < m.height {
			lines = append(lines, "")
		}
		lines = append(lines, foot...)
		if len(lines) > m.height {
			lines = lines[len(lines)-m.height:]
		}
	} else {
		lines = append(append(append([]string{}, head...), list...), foot...)
	}
	tv := tea.NewView(strings.Join(lines, "\n"))
	tv.AltScreen = true
	return tv
}
