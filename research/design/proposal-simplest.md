# Proposal: the simplest design that meets the behaviour

## 1. Summary

1. One function, `core.Refresh`, is the whole engine: load the cache, fetch only the entries that are due, save, return a snapshot. One-shot calls it once; the view calls it from a Bubble Tea command.
2. There is no scheduler. A pure rule, `Entry.Due(now)`, decides per ticker; the view asks it on a one-second tick, one-shot asks it once.
3. All state that matters across processes (last quote, last attempt, failure count) lives in one JSON cache file, replaced atomically. No lock, no daemon.
4. A process stamps "attempting now" into the cache before it fetches. That one stamp gives backoff, the one-shot freshness test and "someone else is already fetching".
5. The provider interface is one method, one ticker per call, with a typed error.

## 2A. Structure

Three packages, about nine files:

```
main.go  tui.go  print.go      package main: flags, Bubble Tea model, table/JSON output
internal/core/                 ticker.go config.go provider.go cache.go refresh.go format.go
internal/yahoo/yahoo.go        the only provider
```

```go
// core/refresh.go
// Refresh loads the cache, fetches every wanted entry that is due (all of them
// if force), saves, and returns the merged state. It never returns an error:
// failures are recorded per entry. At most 5 requests in parallel, 10 s each,
// 15 s for the whole round.
func Refresh(ctx context.Context, p Provider, cachePath string,
    want []Ticker, interval time.Duration, force bool) Snapshot

type Snapshot map[Ticker]Entry

// AnyDue reports whether a Refresh would fetch something. Pure.
func (s Snapshot) AnyDue(want []Ticker, now time.Time, interval time.Duration) bool
```

**One-shot:** load config, `Refresh`, print, exit 0. Nothing else.

**View (Bubble Tea):**

```go
type model struct {
    cfg      core.Config
    cfgMod   time.Time      // mtime of the config last loaded
    snap     core.Snapshot  // preloaded from the cache by main, for instant start
    fetching bool
    notice   string         // config error or "no config" hint
    sort, scroll int; width, height int
}
type tickMsg struct{ now, cfgMod time.Time }
type refreshedMsg struct{ snap core.Snapshot }
type configMsg struct{ cfg core.Config; mod time.Time; err error }
```

- `Init` returns `tea.Batch(tick(), refreshCmd(false))`.
- `tick()` is `tea.Tick(time.Second, …)`; its callback also stats the config file (through the symlink) and puts the mtime in `tickMsg`. `Update` re-arms it each time.
- On `tickMsg`: if the mtime changed, return a command that parses the config (`configMsg`). If `!fetching && snap.AnyDue(…)`, set `fetching` and return `refreshCmd(false)`. The tick also redraws the clock and the ages.
- On `r`: if not fetching, `refreshCmd(true)`. While fetching, `r` is ignored (the status line says "refreshing").
- On `refreshedMsg`: replace `snap`, except that an entry arriving without a quote keeps the quote the model already had (needed for D19 and walk-through v). Clear `fetching`.
- `View` renders from `cfg` order, looking each ticker up in `snap`. The error line is derived from the entries: the shared cause if all failing entries have the same kind, otherwise a count and the first cause, plus the time of the next retry.

**Goroutines.** None of our own that outlive a call. Bubble Tea runs each command in its own goroutine (https://pkg.go.dev/github.com/charmbracelet/bubbletea#Cmd); inside `Refresh` up to five workers fan out and are joined before it returns. `fetching` guarantees one `Refresh` at a time per process. The model is only touched in `Update`.

**Why a fixed tick and not computed timers.** A 60 s timer, a backoff timer and a "market opens at" timer would each need cancelling and re-arming on `r`, reload and wake-up. One tick that asks "is anything due?" has no timer state to get wrong, and it is what makes walk-throughs iii and iv trivial. All comparisons use wall-clock values (`time.Now().Round(0)`), because Go's monotonic clock may stop during sleep (https://pkg.go.dev/time#hdr-Monotonic_Clocks).

## 2B. Cache

One file, `~/.cache/brl-stocks/cache.json`:

```go
// core/cache.go
type Cache struct {
    Version  int              `json:"version"`   // 1
    Provider string           `json:"provider"`  // "yahoo"
    Entries  map[string]Entry `json:"entries"`   // key: canonical ticker, e.g. "B3:PETR4"
}
type Entry struct {
    Quote     *Quote    `json:"quote"`      // nil until the first success
    FetchedAt time.Time `json:"fetched_at"` // last success
    AttemptAt time.Time `json:"attempt_at"` // last attempt, stamped before fetching
    Fails     int       `json:"fails"`      // consecutive failures
    ErrKind   Kind      `json:"err_kind"`
    ErrMsg    string    `json:"err_msg"`
}
func Load(path, provider string) Cache // missing, unreadable, corrupt, other version or provider: empty
func (c Cache) save(path string) error // os.CreateTemp in the same folder, then os.Rename
func (e Entry) Due(now time.Time, interval time.Duration) bool
func (e Entry) State(now time.Time, interval time.Duration) State // ok, closed, stale, error
```

`Due`, in order:

1. `now < AttemptAt + wait(Fails)`: not due. `wait(0)` is the interval; after failures it is 5, 10, 20, 40, 80, 160 s, then 300 s.
2. No quote, or `Fails > 0`: due.
3. `now` falls on a weekday inside the cached session's time-of-day window, in the exchange's time zone, plus 20 minutes (B3's delay): due.
4. `FetchedAt` is before the cached session's end plus 20 minutes and `now` is after it: due once, to pick up the close.
5. Otherwise not due: the market is closed.

Rule 3 projects the last known session onto today instead of trusting its date, so a quote cached yesterday still knows when to start again. `State` uses the real session dates, so a row is marked closed whenever the source's session does not contain now.

`Refresh` does: `Load`; pick due entries; **set `AttemptAt = now` on them and save** (the claim); fetch; `Load` again, apply only its own results, drop entries not attempted for 7 days, save.

- **Half-written data:** rename is atomic on one filesystem; a reader sees the old file or the new one.
- **Two processes fetching at once:** the second one to load sees the claim, finds nothing due and prints what is there. Both can claim only if they load within the same few milliseconds; the cost is one duplicated round. A whole-file write can also drop another process's update in that window; the cost is one refetch.
- **Fresh enough for one-shot:** exactly rule 1 to 5. During market hours this is D23 (one interval). When the market is closed, a 2 s status bar makes no request all night. `--refresh` is `force`.
- **Missing, corrupt, old version:** empty cache, everything due, rewritten by the next save. No migration: the cache holds nothing that cannot be fetched again.

Rejected here: `flock` (works, but platform-specific, and the claim stamp is needed anyway for backoff across one-shot calls).

## 2C. Provider interface

```go
// core/ticker.go
type Market string // "B3", "US", "IDX", "FX"
type Ticker struct { Market Market; Symbol string }  // {B3 PETR4} {US BRK.B} {IDX BVSP} {FX USDBRL}
func ParseTicker(s string) (Ticker, error) // D30 rule, B3:/US: prefixes, ^NAME, USDBRL
func (t Ticker) String() string            // canonical, used as cache key

// core/provider.go
type Provider interface {
    Name() string
    // Quote fetches one ticker. Any error is a *Error.
    Quote(ctx context.Context, t Ticker) (Quote, error)
}
type Quote struct {
    Name, Currency              string
    Price, PrevClose, Low, High float64
    Time    time.Time  // of the last price
    Series  []float64  // intraday closes, oldest first; may be empty
    Session Session
}
type Session struct { Start, End time.Time; TZ string } // TZ: IANA name
type Kind string
const (
    NotFound    Kind = "not_found"    // this ticker only
    Unsupported Kind = "unsupported"  // this provider does not cover the ticker
    RateLimited Kind = "rate_limited" // whole source (429)
    Blocked     Kind = "blocked"      // whole source (401, 403)
    Unavailable Kind = "unavailable"  // network, timeout, 5xx
    BadResponse Kind = "bad_response" // the answer no longer parses
)
type Error struct { Kind Kind; Status int; Msg string }
func (k Kind) SourceWide() bool // RateLimited, Blocked

// yahoo/yahoo.go
func New(c *http.Client) *Provider
func symbol(t core.Ticker) string // PETR4.SA, BRK-B, ^BVSP, BRL=X
```

- Mapping is private to each provider; callers only ever hold a `Ticker`.
- One request (`interval=5m&range=1d`) fills the whole `Quote` (F13, F14). The exact field names (`chartPreviousClose`, `exchangeTimezoneName`, `BRK-B`) are [unverified].
- When a result is `SourceWide`, `Refresh` cancels the rest of the round and records the same failure on every entry it had claimed. This is the only whole-source mechanism; there is no separate breaker state.
- Change (value and percent) is computed by `core`, not by providers.
- **Second provider:** a new package with a constructor that reads its key from an environment variable (never the public config). Single-market coverage is a 15-line `Provider` that routes by `Ticker.Market` to two others; `Unsupported` covers gaps. `main` picks the provider; `Refresh`, the cache and both front ends do not change. I add no `provider` config key and no registry until that day.

## 3. Failure walk-throughs

**(i) HTTP 429 for ten minutes.** The first round sends at most five requests, gets a 429, cancels the rest. Every claimed entry gets `Fails++` and `rate_limited`. Attempts follow at about +5, 15, 35, 75, 155, 315 and 615 s, each stopping at the first 429: roughly 35 requests in ten minutes instead of 230. The view keeps the prices, marks rows stale with their age, and shows "yahoo: rate limited (429), next try in 2m40s". One-shot calls read the same counters, make no request, print `stale`, exit 0. The first success resets each entry. Closed-market entries are never requested.

**(ii) `print --json` every 2 s with the view open.** Each call parses the config, loads the cache, finds nothing due and prints: no network. Once a minute entries become due; whichever process notices first claims and fetches, the other sees the claim. Source load stays at one round per interval. Output during the fetch is the previous quotes.

**(iii) Wake after 14 hours.** The pending tick fires within a second. `Due` compares wall-clock times, so entries in an open session are due immediately; closed ones stay untouched. If the network is not up yet, the round fails and retries at 5, 10, 20 s. Until a success, rows show stale with a 14 h age.

**(iv) Config edited.** Within one second the tick sees a new mtime and the config is parsed. Valid: `cfg` is swapped; the removed ticker disappears at the next draw (its cache entry expires after 7 days); the added one has no entry, so rule 2 makes it due on the next tick and only it is fetched. Invalid: the old config stays and the status line shows the error with its line. A file that briefly does not exist (editor rename) is ignored.

**(v) Cache truncated to zero bytes.** `Load` returns an empty cache. One-shot: everything is due, one full round, file rewritten. View: its next `Refresh` likewise refetches everything, once; if that round fails, the model keeps the quotes it holds in memory, so the screen does not go blank.

## 4. Dependencies

None beyond the standard library, Bubble Tea, Lip Gloss and `github.com/BurntSushi/toml`. Flags use `flag`; terminal detection uses `os.Stdout.Stat()`; pt-BR number formatting is about twenty lines by hand, not `golang.org/x/text`; no file watcher, no lock library, no HTTP client library.

## 5. Weak points

1. **No lock.** The cache is read, modified and written by several processes with only a claim stamp for coordination. Duplicate rounds and lost updates are possible in a window of milliseconds, a process killed mid-fetch leaves its entries claimed for one interval, and `--refresh` from a status bar bypasses every protection.
2. **Market hours are guessed from the last session.** Rule 3 assumes weekdays and the same hours as last time. On a holiday the tool polls a price that does not move all day; a wrong time zone or an odd session for USD/BRL gives wrong open/closed behaviour. All of it rests on F14, which is unverified for holidays.
3. **One-shot blocks and failures are coarse.** Once per interval a status-bar call can block for up to 15 s on a slow source. Yahoo's way of signalling a block is assumed to be 429/401/403; if it answers 200 with an error body or 404, the round is not cut short and the error line misleads. One ticker per call also means a batch-capable provider cannot batch.

## 6. What I rejected

- **A daemon or socket** that owns fetching: solves concurrency properly, but adds a lifecycle to a tool whose worst case is one duplicated round.
- **A scheduler goroutine** pushing results with `Program.Send`: a second owner of timing state beside the model; the tick does the same job with none.
- **Computed timers per ticker or per market:** fewer wake-ups, more state, and wrong after sleep.
- **`fsnotify` for reload:** a dependency, and unreliable through a symlinked folder and editor renames; one `stat` a second is enough.
- **One cache file per ticker, or SQLite:** less write contention, but more files or a dependency, for 23 small records.
- **A batch provider method (`Quotes([]Ticker)`):** moves concurrency and partial-failure handling into every provider; no free source in the research can batch.
- **A separate whole-source circuit breaker and `Retry-After` handling:** per-entry counters plus cancelling the round already give walk-through i.
- **A market calendar:** D15 can be met from the data already fetched.
