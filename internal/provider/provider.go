// Package provider is the boundary between the tool and a source of quotes.
package provider

import (
	"context"

	"github.com/lucattelli/brl-stocks/internal/market"
)

// Provider fetches one instrument per call (D55). Any error it returns is a
// *Error.
type Provider interface {
	Name() string
	Quote(ctx context.Context, in market.Instrument) (market.Quote, error)
}

// Kind classifies a failure.
type Kind string

const (
	NotFound    Kind = "not_found"    // the source does not know this instrument
	Unsupported Kind = "unsupported"  // this provider does not cover it
	RateLimited Kind = "rate_limited" // a refusal: the source is rate-limiting
	Blocked     Kind = "blocked"      // a refusal: access denied
	Unavailable Kind = "unavailable"  // network error, timeout, server error
	BadResponse Kind = "bad_response" // the answer does not parse, or is for another symbol
)

// Refusal reports whether the kind means the source is refusing the tool,
// which starts a block (spec 6.3).
func (k Kind) Refusal() bool { return k == RateLimited || k == Blocked }

// Error is the only error type a Provider returns.
type Error struct {
	Kind   Kind
	Status int
	Msg    string
}

func (e *Error) Error() string { return string(e.Kind) + ": " + e.Msg }
