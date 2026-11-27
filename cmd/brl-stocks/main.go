// brl-stocks monitors a watchlist of B3 and US tickers in the terminal.
package main

import (
	"context"
	"os"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/lucattelli/brl-stocks/internal/app"
	"github.com/lucattelli/brl-stocks/internal/deps"
	"github.com/lucattelli/brl-stocks/internal/provider/yahoo"
)

func main() {
	// For testing only: the address the default source is reached at (D70).
	base := yahoo.DefaultURL
	if v, ok := os.LookupEnv("BRL_STOCKS_SOURCE_URL"); ok && v != "" {
		base = v
	}
	// A terminal, not just a character device: output sent to /dev/null is
	// redirected too (D94).
	tty := term.IsTerminal(os.Stdout.Fd())
	os.Exit(app.Run(context.Background(), os.Args[1:], deps.Deps{
		Provider:  yahoo.New(base),
		Now:       func() time.Time { return time.Now().Round(0) },
		After:     time.After,
		LookupEnv: os.LookupEnv,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		IsTTY:     tty,
	}))
}
