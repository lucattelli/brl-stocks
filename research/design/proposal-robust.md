# Proposal: the most robust design

## 1. Summary

1. One shared code path, `engine.Cycle`, does lock → re-read cache → plan → fetch → merge → write. The view calls it inside a `tea.Cmd`; `print` calls it directly.
2. Scheduling is a pure function of wall-clock time (`MakePlan`), evaluated on a 1-second heartbeat. There are no long timers, so sleep, clock changes and missed ticks cannot desynchronise it.
3. What a row claims (`ok`, `closed`, `stale`, `error`) is recomputed from timestamps at every render (`Classify`), never stored, so old data cannot keep an "ok" label.
4. The cache is two versioned JSON files written by temp file + rename under one `flock`; the source's health (blocked until when) is persisted, so every process honours the same back-off.
5. Providers take a batch, own their politeness, and return typed errors with a scope (ticker or source) and a kind (rate limited, blocked, schema, network…).

## 2. A — Structure

Packages: `market` (types, ticker parsing), `provider` (+ `provider/yahoo`), `cache`, `engine`, `config`, `tui`, `render` (table, JSON), `main`.

```go
package engine

type Entry struct {            // one instrument, as cached
    Quote       *market.Quote  // nil = never fetched
    LastErr     *provider.Error
    LastAttempt time.Time
}
type Status int                // Healthy, Failing, Blocked, Broken
type Health struct {           // one per provider, persisted
    Status      Status
    Failures    int
    NotBefore   time.Time      // no request before this wall-clock time
    LastAttempt time.Time      // written BEFORE the network call
    Last        *provider.Error
}
type State struct { Entries map[market.Instrument]Entry; Health Health }

type Plan struct {
    Due    []market.Instrument
    Probe  bool        // source unhealthy: fetch Due[0] only
    Reason string      // why nothing is due, for the status line
}
// Pure. Due = no quote; or market open (or within 20 min after close, to
// catch the delayed final price) and older than interval; or past the
// predicted next open. Empty if now < Health.NotBefore or < LastAttempt+5s.
func MakePlan(want []market.Instrument, s State, interval time.Duration, now time.Time, force bool) Plan

type Display struct { Kind DisplayKind; Age time.Duration } // OK, Closed, Stale, NoData
// Pure. OK: market open, last attempt succeeded, age <= 2*interval.
// Closed: fetched after session end + 20 min AND now < predicted next open.
// Everything else with a quote is Stale; FetchedAt in the future is Stale.
func Classify(e Entry, h Health, interval time.Duration, now time.Time) Display

type Engine struct { P provider.Provider; C *cache.Store; Now func() time.Time }
// The only function that touches the network. Never runs twice at once
// across processes. Returns ErrBusy if another process holds the lock.
func (e *Engine) Cycle(ctx context.Context, want []market.Instrument, interval time.Duration, force bool) (State, error)
func (e *Engine) Load() (State, error)   // lock-free read, for display
```

**Bubble Tea mapping.** `Update` does no I/O; every read, stat and fetch is a `tea.Cmd`, which Bubble Tea runs in its own goroutine and whose result returns as a message (https://pkg.go.dev/github.com/charmbracelet/bubbletea#Cmd). The only goroutines I create are the provider's bounded workers inside `Cycle`; they all end before `Cycle` returns.

```go
type model struct {
    cfg config.Config; cfgStamp, cacheStamp fileStamp // mtime+size
    state engine.State; inflight bool; lastBeat time.Time
    notice string; sort sortMode; scroll int
}
type beatMsg time.Time           // tea.Tick(1s), re-armed on each beat
type cycleMsg struct{ st engine.State; err error }
type loadedMsg struct{ st engine.State; err error }
type configMsg struct{ cfg config.Config; stamp fileStamp; err error }
```

On `beatMsg`: (1) if wall-clock gap since `lastBeat` > 30 s, treat as wake; (2) stat config and cache, issuing `reloadCmd`/`loadCmd` if a stamp changed; (3) if `!inflight` and `MakePlan` has work, issue `cycleCmd`. `r` is the same with `force=true`. `View` calls `Classify` per row with the current time, so ages and states advance even when nothing is fetched.

Why a heartbeat and not `tea.Tick(60s)`: Go timers use the monotonic clock, which some systems stop during sleep (https://pkg.go.dev/time#hdr-Monotonic_Clocks). Deadlines are stored as wall-clock times and compared with `Round(0)` values. Checking the config on the heartbeat, not on refresh, also makes reload work while every market is closed.

**Back-off** (in `Cycle`, from the batch's source error): network/5xx → 5, 10, 20, 40, 60 s. Rate limited/blocked → 2, 4, 8, 16, 30 min ±10 %, or `Retry-After` if longer. Schema → 5 min, then 30 min. After `NotBefore`, one **probe** request; success restores `Healthy` and the full cycle follows.

**Next open** is predicted as the session start time on the next weekday in the exchange's zone. If the fetch then returns a period that does not contain now (a holiday), the next check moves to that period's start or the following weekday: one request per holiday.

## 3. B — Cache

```
~/.cache/brl-stocks/quotes.v1.json   {schema, tool_version, written_at, entries{ "B3:PETR4": Entry }}
~/.cache/brl-stocks/source.v1.json   {schema, providers{ "yahoo": Health }}
~/.cache/brl-stocks/lock             flock target, never read
```

```go
package cache
type Store struct{ Dir string }
func (s *Store) ReadQuotes() (map[market.Instrument]engine.Entry, error) // no lock
func (s *Store) ReadHealth() (map[string]engine.Health, error)
func (s *Store) TryLock() (unlock func(), err error)   // flock LOCK_EX|LOCK_NB; ErrBusy
func (s *Store) WriteQuotes(...) error                  // caller holds lock
func (s *Store) WriteHealth(...) error
type CorruptError struct{ File, Reason string }         // distinct from fs.ErrNotExist
type VersionError struct{ File string; Schema int }
```

- **Readers never lock.** Writers write a temp file in the same directory, `fsync`, then `rename`, so a reader sees the old or the new file, never a partial one.
- **Only the lock holder fetches and writes.** `flock` is released by the kernel when the process dies, so a crash cannot leave a stale lock (a PID lock file can).
- **Under the lock, `Cycle` re-reads both files**, so if another process fetched while this one waited, nothing is due and no request is made.
- **Write-ahead attempt stamp.** `Cycle` writes `Health.LastAttempt` before the first request, and does not fetch if that write fails. This stops a status bar that kills `print` mid-fetch, or an unwritable cache folder, from producing a fetch every 2 seconds. The view may still fetch without a writable cache: one long-lived process paces itself in memory.
- **Merge, never replace.** Written entries = disk ∪ memory ∪ fetched, newest `FetchedAt` per instrument. Processes with different `--config` files or a partial cycle (only B3 open) do not erase each other. Entries untouched for 7 days are pruned.
- **Loading validates every entry** with the same `Quote.Validate` the provider uses. A failing entry is dropped alone.
- **Missing** → empty, silent. **Corrupt** → empty, with a notice; overwritten at the next cycle. **Other version** → the schema number is in the file name, so old and new binaries use different files and never fight; a mismatched `schema` field inside is a `VersionError`, treated as empty.
- **Corrupt `source.v1.json` fails open** (Healthy). Cost: at most one aborted cycle before the block is rediscovered.

**One-shot freshness** is per instrument: `print` fetches iff `MakePlan` returns work, the same function the view uses, with D23's rule (older than one interval while open). If `TryLock` returns `ErrBusy`, it prints the cache at once; it waits (up to 5 s) only if some ticker has no quote at all. Fetch budget: 8 s total.

## 4. C — Provider interface

```go
package market
type Venue string                       // "B3", "US", "IDX", "FX"
type Instrument struct{ Venue Venue; Code string }  // String(): "B3:PETR4"
func ParseTicker(s string) (Instrument, error)      // D30 rule; in the core
func ParseHeader(s string) (Instrument, error)      // ^BVSP, ^GSPC, USDBRL

type Quote struct {
    Instrument Instrument; Name, Currency string
    Price, PrevClose, DayLow, DayHigh float64
    QuoteTime time.Time
    Series    []Point                   // regular session only
    Session   Session                   // Start, End, TZName, UTCOffset
    FetchedAt time.Time; Provider string
}
// Rejects: non-finite or non-positive prices, low > high, quote time more
// than 5 min ahead, currency not matching venue, session end <= start.
func (q Quote) Validate() error

package provider
type Provider interface {
    Name() string
    // Symbol maps to the source's notation without network; ok=false means
    // this provider does not cover the instrument.
    Symbol(in market.Instrument) (sym string, ok bool)
    // Fetch never returns partial quotes: each instrument is in Quotes or Failed.
    Fetch(ctx context.Context, ins []market.Instrument) Batch
}
type Batch struct {
    Quotes map[market.Instrument]market.Quote
    Failed map[market.Instrument]*Error
    Source *Error                       // non-nil: the whole source is unhealthy
}
type Kind int // NotFound, Unsupported, BadData (ticker scope);
              // Network, Server, RateLimited, Blocked, Schema, Auth (source scope)
type Error struct {
    Kind Kind; Provider string; Instrument *market.Instrument
    HTTPStatus int; RetryAfter time.Duration; Err error
}
func (k Kind) SourceWide() bool
func (e *Error) UserMessage() string    // one line for the status bar / JSON
```

Reasoning:

- **Batch in, typed result out.** The provider owns concurrency (Yahoo: 4 workers, 10 s each) and aborts the batch on the first 429/401/403, because only it knows what its source tolerates. A later provider with a real batch endpoint fits without changing callers.
- **The provider decides scope.** One undecodable response is `BadData` for that ticker. Yahoo promotes to `Schema` when more than half a batch fails to decode, and to `Blocked` when it receives HTML instead of JSON.
- **Yahoo checks that the response's symbol equals the one requested**, so a redirect or cache mix-up cannot put one ticker's price on another's row.
- **Second provider:** `provider.Router` implements `Provider`, sends each instrument to the first member whose `Symbol` returns `ok`, and reports `Unsupported` otherwise. `Health` is keyed by provider name. Keys come from an environment variable read in the constructor (`yahoo.New(opts) (Provider, error)`); a missing key is an `Auth` source error.

[unverified]: whether Yahoo sends `Retry-After`; the exact status and body for an unknown symbol; whether `chartPreviousClose` or `previousClose` is yesterday's close with `range=1d`; holiday behaviour of the trading period (F14).

## 5. Failure walk-throughs

**(i) 429 for ten minutes.** The first cycle sends at most 4 requests (the worker count) before the batch aborts. `Health` becomes `Blocked`, `NotBefore` ≈ +2 min, persisted. Rows keep their prices and turn `stale` with age; status line: "Yahoo is rate limiting (HTTP 429). Prices from 11:41. Next attempt 11:43." Probes of one request go out at about +2, +6 and +14 min; the third succeeds and a full cycle follows. Total during the block: about 6 requests instead of 230. `r` sends one probe if 30 s have passed since the last attempt, otherwise it says when the next attempt is. Every `print` reads `NotBefore`, makes no request, prints stale values and exits 0.

**(ii) `print --json` every 2 s with the view open.** Each call reads the config and two small files, finds everything fresher than the interval, prints, exits: no lock, no network. Near the 60 s mark either process may win `TryLock`. If `print` wins, the view gets `ErrBusy`, then sees the cache stamp change on a later beat and adopts the entries. If the view wins, `print` prints the cache immediately (still `ok`: age < 2 × interval). At most one process fetches per interval.

**(iii) Wake after 14 hours.** The first beat sees a 14-hour wall-clock gap. `View` already shows the truth because `Classify` uses wall time: quotes fetched after yesterday's close stay `closed` only if the predicted next open has not passed; otherwise `stale, 14 h`. `NotBefore` is set to now + 3 s so the network can return, then a cycle runs. If the network is still down: `Network` error, 5/10/20 s retries, status line says so. A `Blocked` deadline that expired during sleep allows one probe, not a full cycle.

**(iv) Config edited.** Within one second the stamp changes and `reloadCmd` parses it. Invalid (for example a half-saved file): the old watchlist stays, the status line shows the line number, and the next stamp change is parsed again. Valid: the removed ticker's row disappears (its cache entry is pruned after 7 days); the added ticker appears as "loading", `MakePlan` finds it due even if its market is closed, and it is fetched within 5 s, alone. A reload never resets back-off, so saving repeatedly cannot bypass a block. A result arriving for the removed ticker goes to the cache and is not shown.

**(v) Cache truncated to zero bytes.** `ReadQuotes` returns `CorruptError{Reason: "empty"}`. The open view keeps its in-memory state, shows "cache unreadable, rewriting", and its next cycle writes memory ∪ fetched. A `print` with no view treats the cache as empty: every ticker is due, it locks, fetches, writes. `source.v1.json` is a separate file, so a block in force survives.

## 6. Dependencies

None beyond the allowed set. `flock` is `syscall.Flock` from the standard library (Linux and macOS only; Windows is unsupported). Time zones use `time.LoadLocation` with `time/tzdata` embedded, falling back to the source's UTC offset.

## 7. Weak points

1. **Open/closed logic rests on unverified data and a guess.** The trading period's holiday behaviour is unknown (F14), half-days are not modelled, and "next weekday, same time" is wrong around daylight-saving changes without tzdata. The design errs toward `stale` and an extra request, not toward a wrong `ok`, but it can show `stale` on a holiday morning for no good reason.
2. **State space.** Health × per-ticker state × lock × wake is far more logic than a naive loop, and `MakePlan`/`Classify` are where bugs will be. They are pure and table-testable, but every threshold (2 min, 30 min, "more than half", 20 min settle) is a guess with no data from Yahoo behind it.
3. **Blocking is detected heuristically and shared.** A single 403 or an HTML page from a captive portal silences every process for minutes. The lock is held across network calls, so a slow fetcher delays others up to its 8–25 s budget, and `flock` is unreliable on network home directories.

## 8. What I rejected

- **Per-ticker timers / `tea.Tick(60s)`:** break across sleep, drift, and cannot be tested without waiting.
- **In-memory back-off only:** each `print` would rediscover the block with real requests.
- **View as a daemon serving `print` over a socket:** a second protocol and new failure modes; D6 does not need it.
- **One cache file per ticker:** removes the merge, but a lock is needed anyway to stop double fetching, and `print` would open 23 files per call.
- **SQLite or bbolt:** a dependency for 23 records.
- **PID lock file:** stale after a crash.
- **fsnotify for config reload:** a dependency, and awkward with symlinks and editors that save by rename; a stat per second is enough.
- **`Quote(ctx, ticker) (Quote, error)` with caller-owned concurrency:** the caller cannot know a source's limits, and a plain `error` cannot carry scope or `Retry-After`.
- **One message per ticker via `Program.Send`:** progressive painting, but goroutines outliving their command; one message per cycle is easier to reason about.
