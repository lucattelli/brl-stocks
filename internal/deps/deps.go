// Package deps holds the edges the tool is given from outside, so that tests
// can replace them (design C15).
package deps

import (
	"io"
	"sync"
	"time"

	"github.com/lucattelli/brl-stocks/internal/provider"
)

// Deps is everything the tool takes from its surroundings.
type Deps struct {
	Provider  provider.Provider                    // the source of quotes
	Now       func() time.Time                     // the clock; wall time without a monotonic reading
	After     func(time.Duration) <-chan time.Time // the timer behind every deadline; time.After outside tests
	LookupEnv func(string) (string, bool)          // HOME, XDG_CONFIG_HOME, XDG_CACHE_HOME, NO_COLOR
	Stdout    io.Writer
	Stderr    io.Writer
	IsTTY     bool      // whether Stdout is a terminal
	Terminal  *Terminal // where the full-screen view runs; nil: the process's own terminal
}

// Terminal is the full-screen view's edge, for tests (design: the view's
// test boundary). The view draws every frame through Draw and reads the
// user's keys and resizes from Input until it quits.
type Terminal struct {
	Width, Height int          // the size when the view opens
	Input         <-chan Input // keys and resizes, in order; a closed channel: nothing more will come
	Draw          func(Frame)  // called with every frame the view shows
	// Clock, when set, is the view's only clock and timer, moved only by an
	// Input with Elapse (design: the view's test boundary, "Time").
	Clock *Clock
}

// Input is one thing the user does: a key, or, when Key is "", a resize to
// Width by Height. When Elapse is set (and Key, Width and Height are not),
// it is not the user's: it lets that much time pass on the view's clock
// (design: the view's test boundary, "Time").
type Input struct {
	Key           string // one of the Key constants, or one printable character ("q" and "Q" differ)
	Width, Height int
	Elapse        time.Duration
}

// The keys that are not a printable character. Any other Key value that is
// not one printable character makes Run panic.
const (
	KeyUp    = "up"
	KeyDown  = "down"
	KeyLeft  = "left"
	KeyRight = "right"
	KeyEnter = "enter"
	KeyEsc   = "esc"
	KeyTab   = "tab"
	KeyCtrlC = "ctrl+c"
)

// Frame is the whole screen after one update of the view.
type Frame struct {
	Width, Height int
	// Lines has exactly Height entries; each holds at most Width terminal
	// cells once SGR codes are taken out, and is not padded. The only SGR
	// codes are ESC[32m (green), ESC[31m (red) and ESC[0m (reset); none
	// at all when NO_COLOR is set.
	Lines []string
	// Now is the view's clock when the frame was drawn.
	Now    time.Time
	Inputs int  // the number of inputs handled so far
	Idle   bool // nothing is running except the view's own waits (on Terminal.Clock when set, else on After); a request to the source in flight is not idle
}

// Clock is a clock for tests of the view: it stands still until the view's
// driver moves it, on an Input with Elapse. A test gives the same Clock to
// the view (Terminal.Clock) and to its stand-in source.
type Clock struct {
	mu    sync.Mutex
	now   time.Time
	seq   int
	waits []clockWait
}

type clockWait struct {
	due time.Time
	seq int
	ch  chan time.Time
}

// NewClock returns a clock standing at start.
func NewClock(start time.Time) *Clock { return &Clock{now: start} }

// Now is the clock's time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After returns a channel that receives the clock's time once d has passed
// on the clock; at once when d is not positive.
func (c *Clock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.seq++
	c.waits = append(c.waits, clockWait{due: c.now.Add(d), seq: c.seq, ch: ch})
	return ch
}

// Step is for the view's driver only; a test never calls it (it moves time
// only by sending an Input with Elapse). It moves the clock to the earliest
// wait due by until (ties in the order the waits were made), fires it and
// reports true; with none due, it moves the clock to until and reports false.
func (c *Clock) Step(until time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	best := -1
	for i, w := range c.waits {
		if w.due.After(until) {
			continue
		}
		if best < 0 || w.due.Before(c.waits[best].due) || (w.due.Equal(c.waits[best].due) && w.seq < c.waits[best].seq) {
			best = i
		}
	}
	if best < 0 {
		if until.After(c.now) {
			c.now = until
		}
		return false
	}
	w := c.waits[best]
	c.waits = append(c.waits[:best], c.waits[best+1:]...)
	if w.due.After(c.now) {
		c.now = w.due
	}
	w.ch <- c.now
	return true
}
