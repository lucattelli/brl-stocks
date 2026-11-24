// Package engine is the one refresh path, shared by the full-screen view and
// by print: plan, claim, fetch, commit (docs/design.md, D53, D54).
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/lucattelli/brl-stocks/internal/cache"
	"github.com/lucattelli/brl-stocks/internal/market"
	"github.com/lucattelli/brl-stocks/internal/provider"
)

const (
	workers        = 8               // requests in parallel (design C16, a guess)
	requestTimeout = 5 * time.Second // per request (design C16, a guess)
	seriesPoints   = 10              // the sparkline's width (D50)
)

// Engine combines the source, the files and the clock.
type Engine struct {
	P     provider.Provider
	C     cache.Files
	Now   func() time.Time
	After func(time.Duration) <-chan time.Time
}

// Request is one refresh.
type Request struct {
	Want            []market.Instrument
	Interval        time.Duration
	ConfigMTime     time.Time
	Force           bool          // r and --refresh
	Deadline        time.Duration // of the whole round
	MayFetchUnpaced bool          // the view may fetch when nothing can be written; print may not (D60)
	WaitForOthers   bool          // print waits for a row another process is fetching, if it has no quote (D73)
}

// Result is what a cycle leaves: the rows, the block record, and a warning
// when the files could not be locked or written.
type Result struct {
	Snap   market.Snapshot
	Health market.Health
	Warn   error
}

type outcome struct {
	sent  bool
	quote *market.Quote
	fail  *market.Failure
	at    time.Time
}

// Cycle refreshes what is due. Failures of the source are data in the
// result, never an error.
func (e *Engine) Cycle(ctx context.Context, r Request) Result {
	res := e.cycle(ctx, r)
	if r.WaitForOthers && e.waitForOthers(r, &res) {
		res = e.cycle(ctx, r) // whatever the other process left undone
	}
	return res
}

// waitForOthers waits while a wanted row has no quote and another process
// holds its claim, at most for the round's deadline (D73). It reports
// whether it waited.
func (e *Engine) waitForOthers(r Request, res *Result) (waited bool) {
	deadline := e.After(r.Deadline)
	for {
		now, pending := e.Now(), false
		for _, in := range r.Want {
			en := res.Snap[in]
			if en.Quote == nil && en.Fail == nil && market.ClaimLive(en, now) {
				pending = true
				break
			}
		}
		if !pending {
			return waited
		}
		waited = true
		select {
		case <-deadline:
			return waited
		case <-e.After(100 * time.Millisecond):
		}
		res.Snap, res.Health = e.C.Load(), e.C.LoadSource(e.P.Name())
	}
}

func (e *Engine) cycle(ctx context.Context, r Request) Result {
	name := e.P.Name()
	res := Result{Snap: e.C.Load(), Health: e.C.LoadSource(name)}
	for pass := 0; pass < 2; pass++ {
		if len(e.plan(r, res.Snap, res.Health).Due) == 0 {
			return res
		}
		// Claim, under the lock: read again, plan against what is on disk,
		// mark the rows of this round.
		unlock, err := e.C.Lock(e.After)
		if err != nil {
			res.Warn = err
			return res // print shows what it has; the view tries again at its next beat
		}
		res.Snap, res.Health = e.C.Load(), e.C.LoadSource(name)
		plan := e.plan(r, res.Snap, res.Health)
		if len(plan.Due) == 0 {
			unlock()
			return res
		}
		now, id := e.Now(), newID()
		for _, in := range plan.Due {
			en := res.Snap[in]
			en.ClaimedAt, en.ClaimID = now, id
			res.Snap[in] = en
		}
		werr := e.C.Write(res.Snap)
		seenGen := res.Health.Gen
		if werr == nil && plan.Probe {
			res.Health.ProbeClaimedAt, res.Health.ProbeClaimID = now, id
			res.Health.Gen++
			res.Health.UpdatedAt = now
			seenGen = res.Health.Gen
			werr = e.C.WriteSource(name, res.Health)
		}
		unlock()
		if werr != nil {
			res.Warn = werr
			if !r.MayFetchUnpaced {
				return res
			}
		}

		outcomes := e.fetch(ctx, r, plan, &res)
		cleared := e.commit(r, id, plan, seenGen, outcomes, &res)
		if !cleared {
			return res
		}
	}
	return res
}

func (e *Engine) plan(r Request, s market.Snapshot, h market.Health) market.PlanResult {
	return market.Plan(market.PlanInput{
		Want: r.Want, Snap: s, Health: h, Interval: r.Interval,
		Now: e.Now(), ConfigMTime: r.ConfigMTime, Force: r.Force,
	})
}

// fetch sends the round's requests, outside the lock. A worker looks at the
// source file before each request; a refusal is saved the moment it is seen
// and no further request is sent (protocol rules 4 and 9).
func (e *Engine) fetch(ctx context.Context, r Request, plan market.PlanResult, res *Result) map[market.Instrument]outcome {
	name := e.P.Name()
	roundCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	deadline := e.After(r.Deadline)
	go func() {
		select {
		case <-deadline:
			cancel()
		case <-roundCtx.Done():
		}
	}()

	var mu sync.Mutex
	out := map[market.Instrument]outcome{}
	stopped := false // a refusal or a block ended the round

	jobs := make(chan market.Instrument, len(plan.Due))
	for _, in := range plan.Due {
		jobs <- in
	}
	close(jobs)

	var wg sync.WaitGroup
	for i := 0; i < workers && i < len(plan.Due); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for in := range jobs {
				mu.Lock()
				stop := stopped
				mu.Unlock()
				if stop || roundCtx.Err() != nil {
					continue
				}
				if !plan.Probe && e.Now().Before(e.C.LoadSource(name).BlockedUntil) {
					mu.Lock()
					stopped = true
					mu.Unlock()
					continue
				}
				q, err := e.one(roundCtx, in)
				at := e.Now()
				mu.Lock()
				f := failureOf(err)
				switch {
				case err == nil:
					out[in] = outcome{sent: true, quote: &q, at: at}
				case f.Refusal():
					// The round sends nothing more. Requests already sent are left
					// to finish: giving them up would not unsend them, and their
					// answers say which rows were refused (spec 6.3).
					out[in] = outcome{sent: true, fail: f, at: at}
					if !stopped {
						stopped = true
						e.saveBlock(name, f.Kind, plan.Probe, res)
					}
				default:
					out[in] = outcome{sent: true, fail: f, at: at}
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}

// one sends one request, with its own time limit.
func (e *Engine) one(ctx context.Context, in market.Instrument) (market.Quote, error) {
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	timer := e.After(requestTimeout)
	go func() {
		select {
		case <-timer:
			cancel()
		case <-reqCtx.Done():
		}
	}()
	return e.P.Quote(reqCtx, in)
}

func failureOf(err error) *market.Failure {
	if err == nil {
		return nil
	}
	var pe *provider.Error
	if errors.As(err, &pe) {
		return &market.Failure{Kind: string(pe.Kind), Msg: pe.Msg}
	}
	return &market.Failure{Kind: string(provider.Unavailable), Msg: err.Error()}
}

// saveBlock writes a refusal at once. Only a refused probe lengthens the
// wait; a refusal of a request sent before the block was known leaves it as
// it is (protocol rule 9).
func (e *Engine) saveBlock(name, kind string, probe bool, res *Result) {
	now := e.Now()
	apply := func(h market.Health) market.Health {
		switch {
		case probe:
			wait := market.BlockWait
			for i := 0; i < h.Refusals && wait < market.MaxWait; i++ {
				wait *= 2
			}
			if wait > market.MaxWait {
				wait = market.MaxWait
			}
			h.BlockedUntil, h.Refusals = now.Add(wait), h.Refusals+1
			h.ProbeClaimedAt, h.ProbeClaimID = time.Time{}, ""
		case now.Before(h.BlockedUntil):
			return h
		default:
			h.BlockedUntil = now.Add(market.BlockWait)
			if h.Refusals < 1 {
				h.Refusals = 1
			}
		}
		h.Kind, h.UpdatedAt = kind, now
		h.Gen++
		return h
	}
	unlock, err := e.C.Lock(e.After)
	if err != nil {
		res.Health, res.Warn = apply(res.Health), err
		return
	}
	defer unlock()
	res.Health = apply(e.C.LoadSource(name))
	if err := e.C.WriteSource(name, res.Health); err != nil {
		res.Warn = err
	}
}

// commit applies this round's results, under the lock, to the rows the round
// still owns, and releases every claim of the round (protocol rules 2, 3, 6).
// It reports whether a probe cleared the block.
func (e *Engine) commit(r Request, id string, plan market.PlanResult, seenGen int, outcomes map[market.Instrument]outcome, res *Result) (cleared bool) {
	name := e.P.Name()
	unlock, err := e.C.Lock(e.After)
	locked := err == nil
	if locked {
		defer unlock()
		res.Snap = e.C.Load()
		if h := e.C.LoadSource(name); !h.UpdatedAt.Before(res.Health.UpdatedAt) || h.Gen >= res.Health.Gen {
			res.Health = h
		}
	} else {
		res.Warn = err
	}
	for _, in := range plan.Due {
		en, ok := res.Snap[in]
		if !ok || en.ClaimID != id {
			continue // re-claimed by another process, or removed: leave it alone
		}
		en.ClaimedAt, en.ClaimID = time.Time{}, ""
		if o := outcomes[in]; o.sent {
			en.LastAttempt = o.at
			if o.quote != nil {
				q := *o.quote
				q.Series = market.Downsample(q.Series, seriesPoints)
				en.Quote, en.FetchedAt, en.Failures, en.Fail = &q, o.at, 0, nil
			} else {
				en.Fail = o.fail
				if o.fail.Refusal() {
					if en.Failures < 1 {
						en.Failures = 1
					}
				} else {
					en.Failures++
				}
			}
		}
		res.Snap[in] = en
	}
	// The cache drops the tickers that left the config (D50, D61).
	wanted := map[market.Instrument]bool{}
	for _, w := range r.Want {
		wanted[w] = true
	}
	for in := range res.Snap {
		if !wanted[in] {
			delete(res.Snap, in)
		}
	}
	if plan.Probe {
		o := outcomes[plan.Due[0]]
		answered := o.sent && (o.quote != nil || o.fail.Unknown())
		switch {
		case answered && res.Health.Gen == seenGen:
			res.Health = market.Health{Gen: res.Health.Gen + 1, UpdatedAt: e.Now()}
			cleared = true
		case o.sent && o.fail.Refusal():
			// saveBlock already lengthened the wait
		case res.Health.ProbeClaimID == id:
			res.Health.BlockedUntil = e.Now().Add(r.Interval)
			res.Health.ProbeClaimedAt, res.Health.ProbeClaimID = time.Time{}, ""
			res.Health.Gen++
			res.Health.UpdatedAt = e.Now()
		}
	}
	if locked {
		if err := e.C.Write(res.Snap); err != nil {
			res.Warn = err
		}
		if plan.Probe {
			if err := e.C.WriteSource(name, res.Health); err != nil {
				res.Warn = err
			}
		}
	}
	return cleared
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
