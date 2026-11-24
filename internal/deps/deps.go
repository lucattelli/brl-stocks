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
	Provider  provider.Provider           // the source of quotes
	Now       func() time.Time            // the clock; wall time without a monotonic reading
	LookupEnv func(string) (string, bool) // HOME, XDG_CONFIG_HOME, XDG_CACHE_HOME, NO_COLOR
	Stdout    io.Writer
	Stderr    io.Writer
	IsTTY     bool // whether Stdout is a terminal
}
