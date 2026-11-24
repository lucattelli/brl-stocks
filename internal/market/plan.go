package market

import "time"

// The numbers of the protocol. The first group is behaviour (D39, D50); the
// second is design (docs/design.md, section 4).
const (
	BlockWait    = 5 * time.Minute  // wait after a first refusal
	MaxWait      = 30 * time.Minute // longest wait, for a row and for a block
	NotFoundWait = time.Hour
	ForceGuard   = 5 * time.Second // a forced refresh skips a row attempted this recently

	ClaimTTL     = 20 * time.Second // a claim, and a probe claim, expire after this
	settleB3     = 30 * time.Minute // provisional, waits on Q62
	settleOther  = 5 * time.Minute  // provisional, waits on Q62
	reopenWindow = 30 * time.Minute // provisional, waits on Q62
	reopenEvery  = 5 * time.Minute  // provisional, waits on Q62
)

// Failure is the last failed fetch of a row.
type Failure struct {
	Kind string `json:"kind"`
	Msg  string `json:"msg,omitempty"`
}

// Refusal reports whether the failure was the source refusing the tool.
func (f *Failure) Refusal() bool {
	return f != nil && (f.Kind == "rate_limited" || f.Kind == "blocked")
}

// Unknown reports whether the source does not know the ticker.
func (f *Failure) Unknown() bool {
	return f != nil && (f.Kind == "not_found" || f.Kind == "unsupported")
}

// Entry is everything kept about one row.
type Entry struct {
	Quote       *Quote    `json:"quote,omitempty"`
	FetchedAt   time.Time `json:"fetched_at"`   // last success
	ClaimedAt   time.Time `json:"claimed_at"`   // a process is about to request this row
	ClaimID     string    `json:"claim_id"`     // which round made the claim
	LastAttempt time.Time `json:"last_attempt"` // when the last request was answered or gave up
	Failures    int       `json:"failures"`     // consecutive
	Fail        *Failure  `json:"fail,omitempty"`
}

// Health is the block record of one source.
type Health struct {
	BlockedUntil   time.Time `json:"blocked_until"`
	Refusals       int       `json:"refusals"`
	Kind           string    `json:"kind,omitempty"` // of the refusal that started the block
	ProbeClaimedAt time.Time `json:"probe_claimed_at"`
	ProbeClaimID   string    `json:"probe_claim_id,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
	Gen            int       `json:"gen"` // goes up at every write
}

// Snapshot is the content of the quote cache.
type Snapshot map[Instrument]Entry

// EffectiveBlock gives the time until which the source is blocked, and the
// kind of refusal behind it. A refusal is also on the rows, so a lost block
// record does not forget the block (protocol rule 5).
func EffectiveBlock(h Health, want []Instrument, s Snapshot) (time.Time, string) {
	until, kind := h.BlockedUntil, h.Kind
	for _, in := range want {
		e := s[in]
		if e.Fail.Refusal() {
			if t := e.LastAttempt.Add(BlockWait); t.After(until) {
				until, kind = t, e.Fail.Kind
			}
		}
	}
	return until, kind
}

// PlanInput is what Plan decides from.
type PlanInput struct {
	Want        []Instrument
	Snap        Snapshot
	Health      Health
	Interval    time.Duration
	Now         time.Time
	ConfigMTime time.Time
	Force       bool
}

// PlanResult says what to fetch now.
type PlanResult struct {
	Due     []Instrument
	Probe   bool      // a block's wait has ended: fetch Due[0] only
	NextTry time.Time // the end of the block in force, if any
}

// Plan decides which rows are due (spec 6.3 and 6.5; docs/design.md).
func Plan(in PlanInput) PlanResult {
	until, _ := EffectiveBlock(in.Health, in.Want, in.Snap)
	if in.Now.Before(until) {
		return PlanResult{NextTry: until}
	}
	if in.Health.Refusals > 0 {
		if live(in.Health.ProbeClaimedAt, in.Health.ProbeClaimID, in.Now) {
			return PlanResult{}
		}
		var fallback *Instrument
		for i, w := range in.Want {
			e := in.Snap[w]
			if e.Fail.Unknown() || live(e.ClaimedAt, e.ClaimID, in.Now) {
				continue
			}
			if e.Quote != nil {
				return PlanResult{Due: []Instrument{w}, Probe: true}
			}
			if fallback == nil {
				fallback = &in.Want[i]
			}
		}
		if fallback != nil {
			return PlanResult{Due: []Instrument{*fallback}, Probe: true}
		}
		return PlanResult{}
	}
	var due []Instrument
	seen := map[Instrument]bool{}
	for _, w := range in.Want {
		if seen[w] {
			continue
		}
		seen[w] = true
		if rowDue(w, in.Snap[w], in) {
			due = append(due, w)
		}
	}
	return PlanResult{Due: due}
}

// ClaimLive reports whether another round still holds a row.
func ClaimLive(e Entry, now time.Time) bool { return live(e.ClaimedAt, e.ClaimID, now) }

// live reports whether a claim made at t still holds. A stamp in the future
// is void (protocol rule 8).
func live(t time.Time, id string, now time.Time) bool {
	return id != "" && !t.IsZero() && !t.After(now) && now.Before(t.Add(ClaimTTL))
}

func rowDue(w Instrument, e Entry, in PlanInput) bool {
	now := in.Now
	if live(e.ClaimedAt, e.ClaimID, now) {
		return false
	}
	if e.Fail.Unknown() {
		return !now.Before(e.LastAttempt.Add(NotFoundWait)) || in.ConfigMTime.After(e.LastAttempt)
	}
	if in.Force {
		recent := !e.LastAttempt.IsZero() && !e.LastAttempt.After(now) && now.Sub(e.LastAttempt) < ForceGuard
		return !recent
	}
	if e.Failures > 0 {
		return !now.Before(e.LastAttempt.Add(FailureWait(in.Interval, e.Failures)))
	}
	if e.Quote == nil {
		return true
	}
	s := e.Quote.Session
	if !now.Before(s.Start) && now.Before(s.End) {
		return now.Sub(e.FetchedAt) > in.Interval
	}
	settle := settleOther
	if w.Hours() == B3 {
		settle = settleB3
	}
	if !now.Before(s.End) && now.Before(s.End.Add(settle)) {
		return now.Sub(e.FetchedAt) > in.Interval
	}
	open, ok := latestExpectedOpen(s, now)
	if !ok {
		return false
	}
	last := e.FetchedAt
	if e.LastAttempt.After(last) {
		last = e.LastAttempt
	}
	if last.Before(open) {
		return true
	}
	return now.Before(open.Add(reopenWindow)) && now.Sub(last) >= reopenEvery
}

// FailureWait is the wait after a row's n-th failed fetch in a row (D50).
func FailureWait(interval time.Duration, failures int) time.Duration {
	w := interval
	for i := 1; i < failures; i++ {
		w *= 2
		if w >= MaxWait {
			return MaxWait
		}
	}
	if w > MaxWait {
		return MaxWait
	}
	return w
}

// latestExpectedOpen gives the most recent moment, not after now, at which a
// session like s would have started again: the same time of day on a later
// weekday, in the exchange's zone.
func latestExpectedOpen(s Session, now time.Time) (time.Time, bool) {
	loc, err := time.LoadLocation(s.TZ)
	if err != nil || s.TZ == "" {
		loc = time.UTC
	}
	start := s.Start.In(loc)
	var best time.Time
	found := false
	for k := 1; k <= 400; k++ {
		c := start.AddDate(0, 0, k)
		if c.After(now) {
			break
		}
		if wd := c.Weekday(); wd == time.Saturday || wd == time.Sunday {
			continue
		}
		best, found = c, true
	}
	return best, found
}

// MarketOpen reports whether a market is open: whether the newest session
// among its instruments contains now (D47). known is false when no quote of
// that market is at hand.
func MarketOpen(m Market, want []Instrument, s Snapshot, now time.Time) (open, known bool) {
	var newest Session
	for _, w := range want {
		e := s[w]
		if w.Hours() != m || e.Quote == nil {
			continue
		}
		if !known || e.Quote.Session.Start.After(newest.Start) {
			newest, known = e.Quote.Session, true
		}
	}
	return known && !now.Before(newest.Start) && now.Before(newest.End), known
}

// State of a row on screen (spec 6.2).
type State string

const (
	Fresh    State = "fresh"
	Stale    State = "stale"
	NotFound State = "not_found"
	NoData   State = "no_data"
)

// Classify gives the state of a row and the age of its quote (D40).
func Classify(e Entry, marketOpen bool, interval time.Duration, now time.Time) (State, time.Duration) {
	if e.Fail.Unknown() {
		return NotFound, 0
	}
	if e.Quote == nil {
		return NoData, 0
	}
	age := now.Sub(e.FetchedAt)
	if e.Fail != nil || (marketOpen && age > 2*interval) {
		return Stale, age
	}
	return Fresh, age
}

// Downsample keeps at most n points of a series, evenly spaced, so that the
// cache does not grow during the day (design C14).
func Downsample(p []Point, n int) []Point {
	if len(p) <= n || n < 2 {
		return p
	}
	out := make([]Point, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, p[i*(len(p)-1)/(n-1)])
	}
	return out
}
