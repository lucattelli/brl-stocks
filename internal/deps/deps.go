// Package deps holds the edges the tool is given from outside, so that tests
// can replace them (design C15).
package deps

import (
	"io"
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
	IsTTY     bool // whether Stdout is a terminal
	Terminal  *Terminal // where the full-screen view runs; nil: the process's own terminal
}

// Terminal is the full-screen view's edge, for tests (design: the view's
// test boundary). The view draws every frame through Draw and reads the
// user's keys and resizes from Input until it quits.
type Terminal struct {
	Width, Height int          // the size when the view opens
	Input         <-chan Input // keys and resizes, in order; a closed channel: nothing more will come
	Draw          func(Frame)  // called with every frame the view shows
}

// Input is one thing the user does: a key, or, when Key is "", a resize to
// Width by Height.
type Input struct {
	Key           string // one of the Key constants, or one printable character ("q" and "Q" differ)
	Width, Height int
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
	Lines  []string
	Inputs int  // the number of inputs handled so far
	Idle   bool // nothing is running except waits on After; a request to the source in flight is not idle
}
