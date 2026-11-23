# Proposal: the design that is easiest to change and to test

## 1. Summary

1. All decisions are pure functions over plain data: `market.Plan` (what is due), `market.Backoff`, `monitor.Step` (state machine), `render.Rows` (what to show). None imports Bubble Tea, `net/http`, `os` or calls `time.Now`.
2. Side effects sit behind three small interfaces: `provider.Provider` (network), `cache.Store` (disk), `func() time.Time` (clock). `refresh.Service` is the only code that combines them.
3. The full-screen view is a thin Bubble Tea adapter: `tea.Msg` → `monitor.Event`, `monitor.Effect` → `tea.Cmd`. One-shot calls `refresh.Service` and `render` directly.
4. The cache is one JSON file, written by temp file and rename under an exclusive `flock` that is held for the whole fetch. Readers never lock. Source back-off state lives in the same file, so all processes share it.
5. Freshness is per instrument, not per file: every mode asks the same `Plan` function.

## 2. A. Structure

```
cmd/brl-stocks  wiring: flags, real clock, http.Client, paths
market          Instrument, Quote, Session, Entry, Snapshot, Parse, Plan, Backoff   (pure)
provider        Provider, Router            provider/yahoo: the one implementation
cache           Store, File, Merge
config          Parse([]byte) (Config, error)                                       (pure)
refresh         Service: lock → load → plan → fetch → save
monitor         State, Event, Effect, Step                                          (pure)
render          Rows, Table, JSON, Sparkline                                        (pure)
tui             tea.Model adapter; View = render.Rows + Lip Gloss
```

```go
package market
type Instrument struct{ Market Market; Kind Kind; Symbol string } // B3|US|Global; Equity|Index|FX; text form "B3:PETR4"
func Parse(s string) (Instrument, error)        // D30, prefixes, header names
type Entry struct {                             // one per instrument, in memory and on disk
    Quote     *Quote                            // nil until the first success
    FetchedAt time.Time                         // last success
    Source    string
    Fail      *Failure                          // last failure, nil after a success
    Failures  int
    RetryAt   time.Time
}
type Health struct{ Failures int; RetryAt time.Time; Fail *Failure } // one per source
type Snapshot struct{ Entries map[Instrument]Entry; Sources map[string]Health }

type PlanInput struct {
    Want     []Instrument
    Snap     Snapshot
    Interval time.Duration
    Now      time.Time
    Force    bool                               // r and --refresh
    SourceOf func(Instrument) string
}
func Plan(in PlanInput) []Instrument            // what to fetch now
func NextDue(e Entry, interval time.Duration) time.Time
func Backoff(k FailKind, failures int, retryAfter time.Duration) time.Duration
```

`NextDue` rules: no quote → now. Session open → `FetchedAt + interval`. Fetched before the session's start → the start. Fetched before its end and now past it → the end (one fetch for the close). Otherwise → the next occurrence of the session's start time of day, in the exchange zone: one probe per closed day. A failed entry → `RetryAt`. `Plan` returns due instruments, except that a source with `Health.RetryAt` in the future yields nothing and a source whose wait has expired yields one probe instrument. `Force` ignores session and per-entry waits but not a source's wait, and skips entries younger than 5 s.

```go
package refresh
type Service struct{ Provider provider.Provider; Store cache.Store; Now func() time.Time }
type Request struct {
    Want     []market.Instrument
    Base     market.Snapshot   // caller's memory; merged with disk before planning
    Interval time.Duration
    Force    bool
    LockWait time.Duration
}
// Quote failures are data in the Snapshot. error is only for the cache (busy, unwritable).
func (s *Service) Refresh(ctx context.Context, r Request) (market.Snapshot, error)

package monitor
type State struct { Cfg config.Config; Snap market.Snapshot; InFlight time.Time; Gen int
                    Sort SortMode; Scroll, W, H int; Notice string; Help bool }
type Event interface{ event() }   // Tick{Now} Key{K} Resize{W,H} Refreshed{Gen,Snap,Err} ConfigLoaded{Cfg} ConfigInvalid{Err}
type Effect interface{ effect() } // Refresh{Gen,Want,Force} CheckConfig{} Quit{}
func Step(s State, e Event) (State, []Effect)

package tui
type Deps struct {
    Refresh    func(context.Context, refresh.Request) (market.Snapshot, error)
    LoadConfig func(since config.Stamp) (config.Config, config.Stamp, bool, error) // stat; parse only if changed
    Now        func() time.Time
    Tick       time.Duration   // 5 s
}
func New(s monitor.State, d Deps) tea.Model
```

**Bubble Tea mapping.** `Init` returns one `tea.Tick` and an initial `Refresh` command; the state already holds the cached snapshot, so the first frame has data. `Update` converts the message, calls `Step`, stores the state, and turns each `Effect` into a `tea.Cmd`. `View` calls `render.Rows(cfg, snap, sort, now)` and styles the rows. `tea.Tick` sends one message and must be re-armed (https://pkg.go.dev/github.com/charmbracelet/bubbletea#Tick); the adapter re-arms it on every `Tick`.

**Scheduling is a 5 s heartbeat, not a timer aimed at the next fetch.** Each `Tick` carries wall-clock `Now`; `Step` runs `Plan` and emits `Refresh` if anything is due and nothing is in flight, plus `CheckConfig`. "Nothing while closed" means no requests; the heartbeat costs one `stat`. It also keeps ages and the header clock current. `r` is `Refresh{Force: true}`.

**Goroutines.** None of ours hold state. Bubble Tea runs each command in its own goroutine; state changes only inside `Update`. The `Refresh` command blocks in `Service.Refresh`; the Yahoo provider fans out to at most 5 workers inside `Fetch` and joins before returning. `Gen` lets `Step` drop a late result. One-shot runs on the main goroutine.

**Clock.** The real clock is `time.Now().Round(0)`, which strips the monotonic reading; the monotonic clock may stop during sleep (https://pkg.go.dev/time#hdr-Monotonic_Clocks), so ages must use wall time.

| Seam | Test it enables | Later change it serves |
|---|---|---|
| `Step` | whole view behaviour as table tests, no terminal | alerts: pure `alerts.Check(prev, next)` on `Refreshed`, new `Notify` effect; detail view: a `Screen` field; editing: new `Key` cases and a `SaveWatchlist` effect |
| `Plan`, `NextDue`, `Backoff` | hours, sleep, back-off with literal times | a different schedule per provider |
| `render.Rows` | golden files for table and JSON | portfolio: extra columns from a second input |
| `Provider` | fake provider; Yahoo against saved JSON through an `http.RoundTripper` | second provider |
| `Store` | in-memory store; `File` on `t.TempDir()` | another storage format |
| `config.Parse` | bytes in, value out | editing: a second, tool-owned watchlist file in the state folder merged as a layer; `config.toml` stays read-only |

## 3. B. Cache

Files in `~/.cache/brl-stocks/`: `quotes.json`, `quotes.lock` (empty, never deleted), and short-lived `quotes.json.<pid>.tmp`.

```json
{"schema":1,
 "sources":{"yahoo":{"failures":0,"retry_at":null}},
 "entries":{"B3:PETR4":{"fetched_at":"…","source":"yahoo","failures":0,
   "quote":{"name":"…","currency":"BRL","price":41.75,"prev_close":42.67,"low":41.6,"high":42.29,
            "time":"…","series":[[1795444800,41.9]],
            "session":{"start":"…","end":"…","tz":"America/Sao_Paulo"}}}}}
```

```go
package cache
type Store interface {
    // Missing, empty, corrupt or other-schema file → empty Snapshot, nil error. Never locks.
    Load() (market.Snapshot, error)
    // Takes the exclusive lock (waiting until ctx ends), reloads, runs fn, writes the result
    // by temp file + fsync + rename, unlocks. ErrBusy if the lock was not obtained.
    Update(ctx context.Context, fn func(market.Snapshot) (market.Snapshot, error)) (market.Snapshot, error)
}
func Merge(a, b market.Snapshot) market.Snapshot // per instrument, the newer attempt wins; pure
```

- **Who writes:** only `Service.Refresh`, inside `Update`, and only if it fetched something. `fn` merges `Base`, plans again against what is on disk, fetches, and records results and health.
- **Half-written data:** rename is atomic within one directory; readers see the old file or the new one.
- **Two processes fetching:** the lock is held across the fetch. The loser waits, then plans again under the lock, finds nothing due and returns the winner's data. No duplicate requests. If the wait runs out (`LockWait`: 3 s one-shot, 15 s view), the caller shows what it has, marked stale. The kernel releases `flock` when a process dies.
- **Fresh enough (D23):** one-shot calls `Load`, then `Plan`. Empty plan → print, with no lock and no network. This covers "at most one interval old" for open markets and "do not fetch" for closed ones.
- **Missing, corrupt, older version:** all are an empty snapshot; the next write replaces the file. Entries untouched for 7 days are dropped at write, by age and not by "absent from my config", because another process may use a different `--config`.

## 4. C. Provider interface

```go
package provider
type Provider interface {
    Name() string
    Supports(market.Instrument) bool
    // One Result per instrument, same order. Never panics, never returns a bare error.
    Fetch(ctx context.Context, ins []market.Instrument) []Result
}
type Result struct{ Instrument market.Instrument; Source string; Quote *market.Quote; Fail *market.Failure }

package market
type FailKind uint8 // NotFound, BadResponse, Timeout | RateLimited, Blocked, Auth, Network
func (k FailKind) SourceWide() bool            // the last four
type Failure struct{ Kind FailKind; Msg string; At time.Time; RetryAfter time.Duration }
type Quote struct { Instrument; Name, Currency string; Price, PrevClose, Low, High float64
                    Time time.Time; Series []Point; Session Session }
type Session struct{ Start, End time.Time; TZ string }

package provider
func NewRouter(ps ...Provider) Provider        // first that Supports wins; none → NotFound
```

- **Notation:** callers pass `Instrument` values, never source symbols. Each provider has a private pure `symbol(Instrument) (string, bool)`: for Yahoo `.SA` for B3, `BRK.B` → `BRK-B`, `^BVSP`, `^GSPC`, `BRL=X` [unverified: the last two mappings].
- **Batch signature:** Yahoo loops with a worker limit; a provider with a multi-symbol endpoint makes one call. Throttling belongs to the provider.
- **Failures:** per-instrument failures land in `Entry.Fail`, source-wide ones in `Snapshot.Sources[name]`. On the first source-wide failure the provider cancels the rest of the batch. `render.Rows` turns both into row states and one status line.
- **Second provider:** `main` holds `map[string]func(Settings) (Provider, error)`; `Settings` carries `Getenv` and an `*http.Client`, so a key comes from the environment (spec §4.1). A US-only provider returns `false` from `Supports` for B3 and the router falls through. Callers see one `Provider`. History for a detail view is an optional interface found by type assertion.

## 5. Failure walk-throughs

**(i) HTTP 429 for ten minutes.** Of the first batch, at most 5 requests go out; the first 429 cancels the rest. `Sources["yahoo"]` gets `RetryAt = now + max(Retry-After, 60 s)`. Rows keep their last quotes, marked stale with age; the status line reads "yahoo: rate limited, retry in 60 s". Later attempts are a single probe request, at waits of 60, 120, then 300 s: about 3 probes in ten minutes. A successful probe clears the health and the same `Refresh` fetches everything due. One-shot calls read the shared `RetryAt` and make no request. Network failures use 5, 10, 20, 40, 60 s.

**(ii) `print --json` every 2 s beside the view.** Each call reads config and cache, gets an empty plan, prints, exits 0: no lock, no request. When entries come due, either the view or a one-shot wins the lock; the other waits and reuses the result. Total load on the source equals the view alone.

**(iii) Wake after 14 hours.** The pending tick fires within 5 s of waking in either clock behaviour. `Step` sees every `NextDue` in the past and emits one `Refresh`. Until it returns, rows show an age of 14 h. A request caught mid-flight fails as `Network` and is retried after 5 s; `Step` treats `InFlight` older than 30 s as lost and `Gen` drops its late answer.

**(iv) Config edited.** The next tick's `CheckConfig` sees a new modification time (through the symlink) and parses. The removed row disappears at once. The added ticker has no entry, so `Plan` returns it alone and it shows "loading" for one request. A file caught half-saved fails to parse: the old config stays, the status line shows the error, the next tick tries again.

**(v) Cache truncated.** `Load` returns an empty snapshot. The open view loses nothing: its memory is `Base`, and the next write restores the file. A one-shot with no view running fetches everything once. Shared back-off state is lost, so one extra batch may be sent to a blocked source.

## 6. Dependencies

None beyond the standard library, Bubble Tea, Lip Gloss and the TOML parser. `flock` comes from `syscall`, flags from `flag`, config changes from `os.Stat`, tests from `testing`.

## 7. Weak points

1. **The lock is held across network I/O.** A stopped or hung holder makes everyone else show stale data until it dies. `syscall.Flock` does not exist on Windows and is unreliable on network home directories, which matters once the tool is published. A 2 s status bar can stack a few waiting processes.
2. **`NextDue` for closed markets rests on unverified session data (F14).** If Yahoo reports a wrong or old session, an open market can be treated as closed until the next daily probe or until `r`. This is the rule most likely to need rework.
3. **More structure than 20 tickers need.** Nine packages and an event/effect layer; `Plan` runs both in `Step` (memory) and in `Service` (disk), and if the two views disagree the view takes the lock every 5 s for nothing.

## 8. What I rejected

- **A scheduler goroutine pushing with `Program.Send`:** state outside `Update`, lifecycle to manage, needs a fake timer to test.
- **One timer aimed at the next due time:** wrong after sleep, must be re-armed on every state change.
- **fsnotify:** a dependency, and fragile with a symlinked folder and editors that save by rename.
- **One file per ticker, or SQLite/bbolt:** no single consistent snapshot or shared source health; a database is a dependency for 23 records.
- **No lock, last writer wins:** simpler, but duplicate fetches against a source known to rate-limit.
- **The view serving one-shots over a socket:** a fourth run mode and a daemon lifecycle.
- **`Quote(ctx, ticker string)` per ticker:** rules out batch endpoints and leaks symbol strings to callers.
- **Provider registration in `init()` or plugins:** hidden coupling; an explicit map in `main` is enough.
- **A decimal type for prices:** display only in version 1; a portfolio should revisit this.
