// Package app is the whole tool behind one function, so that tests can drive
// it without a terminal.
package app

import (
	"context"
	"fmt"

	"github.com/lucattelli/brl-stocks/internal/deps"
)

// Run executes one invocation of brl-stocks. args are the command-line
// arguments without the program name. It returns the exit status.
func Run(ctx context.Context, args []string, d deps.Deps) int {
	fmt.Fprintln(d.Stderr, "brl-stocks: not implemented")
	return 70
}
