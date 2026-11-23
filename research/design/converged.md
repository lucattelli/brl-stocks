# Converged design (provisional) — revision 2

Written 2026-11-23 by the coordinator, from `synthesis.md`, the three rebuttals and the approved spec (`docs/spec.md`, decisions D1–D52). Revision 2 follows the first Codex critique (`critique-converged-codex.md`); section 7 lists what changed and why.

**Nothing here is decided.** Every point marked *provisional* is a recommendation that the user has not accepted. This file exists to be attacked.

Fixed inputs: Go (D22), Bubble Tea with Lip Gloss (D27), a hand-edited TOML config the tool never writes (D12, D28, D35), Yahoo's chart endpoint behind a provider interface (D7), an on-disk cache of last quotes only (D20).

## 1. Summary

1. One refresh path, `engine.Cycle`, is shared by the view and by `print`: try the lock, read the cache, plan, fetch, write.
2. No scheduler. A one-second heartbeat reads the wall clock and asks a pure function, `Plan`, what is due. Row states are recomputed at every draw by a pure function, `Classify`.
3. One versioned JSON cache file holds, per instrument, the last quote, the last attempt and the failure count, and, per source, the round stamp and the block record. It is written by temp file and rename. Readers never lock; a fetching process holds a non-blocking `flock` across its round.
4. An attempt is recorded only for a request that was sent and answered (or timed out). A round that starts writes one small stamp; a refusal is written the moment it is seen.
5. The provider interface is one method, one instrument per call, with a typed error. The engine owns the workers and stops the round on a refusal.
6. The view is a plain Bubble Tea model whose `Update` does no I/O. There is no separate event and effect layer.

## 2. Structure

```
cmd/brl-stocks   main: builds Deps (real clock, HTTP client, environment, streams), calls app.Run
deps             Deps: the injected edges; imported by app, engine and tui, imports none of them
app              Run(ctx, args, deps.Deps) int: flags, config path, exit status (D41, D44, D45)
market           Instrument, Quote, Session, Entry, Health, Snapshot, Parse, Plan, Classify, MarketState   (pure)
provider         Provider, Error, Kind        provider/yahoo: the one implementation
cache            File: Load, TryLock, Write
config           Parse([]byte) (Config, []Warning, error)                                                  (pure)
engine           Cycle
render           Rows, Table, JSON, Sparkline, number formats by currency (D46, D51)                       (pure)
tui              the Bubble Tea model
```

Import direction: `app → tui, engine, render, config`; `tui → engine, render, market, deps`; `engine → cache, provider, market`; nothing imports `app`.

```go
package deps
type Deps struct {
    Provider  provider.Provider                  // tests pass a stand-in (S17.2, QR3–QR6)
    Now       func() time.Time                   // time.Now().Round(0) in main
    LookupEnv func(string) (string, bool)        // XDG_*, NO_COLOR (set but empty counts), HOME
    Stdout, Stderr io.Writer
    IsTTY     bool
}

package market
type Market string                                  // "B3", "US", "IDX", "FX"
type Instrument struct{ Market Market; Symbol string }
func (i Instrument) MarshalText() ([]byte, error)   // "B3:PETR4", "IDX:IBOV", "FX:USDBRL"
func (i *Instrument) UnmarshalText(b []byte) error  // the exact inverse; a round-trip test covers all four markets
func Parse(s string) (Instrument, error)            // D30, B3:/US: prefixes
func ParseSummary(s string) (Instrument, error)     // IBOV, SP500, USDBRL (D42)

type Entry struct {
    Quote       *Quote      // nil until the first success
    FetchedAt   time.Time   // last success
    LastAttempt time.Time   // when the last request for it was sent
    Failures    int         // consecutive
    Fail        *Failure    // last failure, nil after a success
}
type Health struct {        // one per source name
    RoundStarted time.Time  // written before the first request of a round
    BlockedUntil time.Time  // zero: not blocked
    Refusals     int        // consecutive, sets the next wait
}
type Snapshot struct {
    Entries map[Instrument]Entry
    Sources map[string]Health
}

type PlanInput struct {
    Want        []Instrument
    Snap        Snapshot
    Source      string
    Interval    time.Duration
    Now         time.Time
    ConfigMTime time.Time   // of the config file in use
    Force       bool        // r and --refresh
}
type PlanResult struct {
    Due     []Instrument
    Probe   bool            // a block's wait has ended: fetch one instrument only
    NextTry time.Time       // for the status line during a block
}
func Plan(in PlanInput) PlanResult
func MarketState(m Market, s Snapshot, now time.Time) (open, known bool)
func Classify(e Entry, marketOpen bool, interval time.Duration, now time.Time) Display // Fresh, Stale, NotFound, NoData; with Age
```

**`Plan`.** First, for the whole source:

- Blocked (`now < BlockedUntil`): nothing is due, with or without `Force` (D38).
- The wait has ended and `Refusals > 0`: `Probe`. The probe instrument is the first wanted entry that has a quote; if none has, the first that is not not-found.
- `RoundStarted` is less than 5 seconds ago: nothing is due. This paces processes that were killed mid-round, and a forced fetch in a loop (C6).

Then per instrument, in order (spec 6.3 and 6.5):

1. Not found: due only when an hour has passed since `LastAttempt`, or when `ConfigMTime` is later than `LastAttempt` (D39). `Force` does not override it.
2. `Force`: due. It ignores the session and the per-entry wait.
3. `Failures > 0`: not due until `LastAttempt + interval × 2^(Failures−1)`, at most 30 minutes (D50, P28).
4. No quote: due, whatever the market (D50, P32).
5. The market is open (`MarketState`): due when `FetchedAt + interval` has passed (D14).
6. After the session end: due at the normal interval until the end plus a settle window, 30 minutes for B3 and `IDX:IBOV`, 5 minutes for the others (C7; D50, P31). The closing quote is not recognised by its time stamp: a last trade just before the bell would never match.
7. Past the predicted next open (the session's start time of day on the next weekday, in the exchange's zone): due. If the answer still carries an old session, due again every 5 minutes for 30 minutes, then the next weekday (C11). A holiday therefore costs at most 7 requests per entry.
8. Otherwise not due: the market is closed (D15).

**`MarketState`**: a market is open when the newest session among its entries contains now. B3 uses the B3 entries and `IBOV`; US uses the US entries and `SP500`; `FX:USDBRL` uses its own session (D47).

**`Classify`** follows D40 with the market's state, not the entry's own session: `Stale` when `Fail != nil`, or when the market is open and `FetchedAt` is older than two intervals. So after a long sleep, an entry that still holds yesterday's session is stale as soon as any entry of its market reports today's.

**The view.**

```go
type model struct {
    cfg config.Config; cfgStamp, cacheStamp fileStamp   // mtime in nanoseconds, size, inode
    snap market.Snapshot; inflight bool
    notice string; sort sortMode; scroll, w, h int; help bool
    d deps.Deps; eng *engine.Engine
}
type beatMsg struct{}                                    // tea.Tick(1s), re-armed on every beat
type stampsMsg struct{ cfg, cache fileStamp }            // result of the stat command
type cycleMsg struct{ snap market.Snapshot; warn error }
type loadedMsg struct{ snap market.Snapshot }
type configMsg struct{ cfg config.Config; stamp fileStamp; err error }
```

- `app.Run` loads the config and the cache before the program starts, so the first frame has data (QR2).
- On `beatMsg`: return a command that stats the config (through the symlink) and the cache; `Update` itself touches no file (QR11). If nothing is in flight and `Plan` has work, start `cycleCmd`.
- On `stampsMsg`: a changed config stamp starts a parse command; a changed cache stamp, with nothing in flight, starts a lock-free `Load` (C4).
- Any snapshot that arrives (`cycleMsg`, `loadedMsg`) is merged into the model's, never assigned: per instrument the entry with the later `LastAttempt` wins, and a quote is never replaced by no quote. The merge is order-independent, so a late `loadedMsg` cannot undo a newer `cycleMsg`, and an emptied cache file cannot blank the screen.
- A config that parses is adopted at once; one that does not keeps the old one and sets the error line (D45). A half-saved file that happens to parse shows for a beat or two, until the stamp changes again. A deleted file keeps the old config (D50, P12).
- `r` is a cycle with `Force`.

**X4, provisional: no `Step` layer.** `Plan`, `Classify`, `MarketState`, `render.Rows` and `config.Parse` are pure and table-tested. The view's behaviour, including out-of-order messages, is tested by sending messages to the model with a stand-in provider and a fixed clock.

**Bubble Tea version, provisional: version 2** (`charm.land/bubbletea/v2`, F18).

## 3. Cache

Files in `~/.cache/brl-stocks/` (or under `XDG_CACHE_HOME`): `cache.v1.json`, and `lock`, an empty file never read or replaced.

```go
package cache
type File struct{ Dir string }
// Missing, empty, corrupt or other-schema file: empty Snapshot, no error, no message (D48).
func (f File) Load() market.Snapshot
// flock(LOCK_EX|LOCK_NB) on Dir/lock. ErrBusy when another process holds it. Never waits.
func (f File) TryLock() (unlock func(), err error)
// Temp file in Dir, rename, remove the temp file on failure. Caller holds the lock.
func (f File) Write(s market.Snapshot) error

package engine
type Engine struct{ P provider.Provider; C cache.File; Now func() time.Time }
type Request struct {
    Want        []market.Instrument
    Base        market.Snapshot   // the caller's memory
    Interval    time.Duration
    ConfigMTime time.Time
    Force       bool
    Deadline    time.Duration     // 10 s for print (D50, P37), 20 s for the view
    MayFetchUnpaced bool          // true for the view only
}
// Never returns a quote error: failures are data in the Snapshot.
// warn is non-nil when the cache could not be locked or written.
func (e *Engine) Cycle(ctx context.Context, r Request) (snap market.Snapshot, warn error)
```

`Cycle`:

1. `TryLock`. Busy: return `Load()` merged with `Base`; the caller shows what it has (**X1, provisional: a try-lock held across the fetch**).
2. Under the lock: `Load`. Disk is the authority; `Base` only supplies wanted instruments that the file lacks, which restores a truncated cache (**X5, provisional: yes, in this narrow form**). `Plan` again. Nothing due: unlock and return.
3. Write `RoundStarted = now`. If this write fails, do not fetch unless `MayFetchUnpaced`.
4. Fetch with 8 workers, 5 seconds per request, the round bounded by `Deadline`. `LastAttempt` is set on an entry when its request is sent. Entries the deadline cut off before sending are untouched and stay due for the next beat.
5. On the first refusal (`RateLimited` or `Blocked`): set `BlockedUntil = now + 5 min × 2^Refusals` (at most 30 minutes), `Refusals++`, **write the file at once**, then cancel the round (D50, P28; QR5).
6. A probe: a refusal doubles the wait as in step 5. An answer of any other kind from the source (a quote, or not found) clears the block, and the same cycle fetches everything due. No answer (timeout, connection error) keeps `Refusals` and sets `BlockedUntil = now + interval`.
7. A success clears the entry's `Failures` and `Fail`. Any other failure is recorded on that entry only (D50, P29; spec 6.3).
8. Prune (see B-prune below), write, unlock.

**X3, provisional: the block record is in the same file**, keyed by source name. If the file is emptied during a block, the next process sends one round that is cut at the first refusal: at most 8 requests, inside QR5's 10.

**Not-found retry (was N3).** There is no config hash. A not-found ticker is retried when the config file's modification time is later than its last attempt. This needs no shared state, works for `print`, and is unaffected by two configs.

**Open, for the user (behaviour):**

- **B-unwritable.** With a cache folder that cannot be written, nothing can pace `print` across calls. Provisional: the view fetches and paces itself from memory, with one warning; `print` does not fetch, prints what the cache holds (or `state: error`), and warns. This keeps D49 and narrows S15.5 ("it shows quotes") to the view.
- **B-prune.** The spec says the cache drops tickers that left the config (D50, P45; S15.6). With two instances on different `--config` files, each erases the other's quotes and they refetch in turn. Provisional: prune by age instead, dropping entries not attempted for 7 days, as all three original designs did. This amends P45 and S15.6.
- **B-cache-size.** QR8 and S15.3 bound the cache to 10% growth, but the intraday series of one quote grows from one point at the open to about 80 at the close. The design keeps one quote per ticker and at most one trading day of series; the criterion needs rewording.
- **B-refresh-now.** S7.1 says every row is requested within 2 seconds of `r`. With 8 workers and 23 rows that holds only if answers are quick. Provisional: read it as "the refresh starts within 2 seconds" (the wording of P33), not as 23 simultaneous requests.

**One-shot.** `Load`, `Plan`. Nothing due: print, no lock, no request (QR3). Otherwise `Cycle` once, then print. Exit status per D44.

## 4. Provider

```go
package provider
type Provider interface {
    Name() string
    // Quote fetches one instrument. Any error is a *Error.
    Quote(ctx context.Context, in market.Instrument) (market.Quote, error)
}
type Kind string
const (
    NotFound    Kind = "not_found"     // this instrument only
    Unsupported Kind = "unsupported"   // this provider does not cover it
    RateLimited Kind = "rate_limited"  // refusal: 429
    Blocked     Kind = "blocked"       // refusal: 401, 403
    Unavailable Kind = "unavailable"   // network, timeout, 5xx
    BadResponse Kind = "bad_response"  // the answer does not parse, or is for another symbol
)
type Error struct{ Kind Kind; Status int; Msg string }
func (k Kind) Refusal() bool           // RateLimited, Blocked

package market
type Quote struct {
    Name, Currency string
    Price, PrevClose, Low, High float64
    Time    time.Time
    Series  []Point            // regular session of one trading day only
    Session Session            // Start, End, TZ (IANA name)
}
func (q Quote) Validate() error  // finite positive prices, low <= high, end > start; used by the provider and on Load

package yahoo
func New(c *http.Client, userAgent string) *Provider   // c refuses redirects to another host (QR12)
func symbol(in market.Instrument) (string, bool)       // PETR4.SA, BRK-B, ^BVSP, ^GSPC, BRL=X
```

- **X2, provisional: one instrument per call.** The engine runs the workers and stops on a refusal, so that rule is written once and holds for every provider, including the tests' stand-in.
- The Yahoo provider checks that the answer is for the symbol it asked for (C9) and sends a User-Agent constant (C12); no user-facing setting exposes it (D50, P41).
- No router, registry or key handling until a second provider exists (C10).
- Change, as value and percent, is computed by `market` from `Price` and `PrevClose`.

## 5. Tests

- Time: every function takes `now`, or reads `Deps.Now`. The heartbeat is a message, so a test sends `beatMsg` after moving the clock.
- Source: a stand-in `Provider` that counts requests and can be slow, fail, refuse or not know a ticker. The Yahoo provider is tested against saved answers through an `http.RoundTripper`.
- Cache: a round trip of every market's instrument through `Write` and `Load`.
- Whole-tool criteria (exit status, output, files untouched) call `app.Run` with a temporary home.

## 6. Open risks

- **Holidays and session data.** Rules 5 to 7 rest on F14, unverified. If the source reports a normal session on a holiday, the market is shown open and polled all day (S5.9 fails). No fallback is designed.
- **A stopped lock holder.** A process stopped (not killed) in the middle of a round keeps the lock; every other process then prints the cache until it resumes or dies. The kernel releases the lock on death only. The alternative is a short lock around a claim, with the fetch outside it.
- **Refusal detection.** Only 401, 403 and 429 count. A refusal sent as 200 with an error body, or as 404, is treated per entry (backlog item).
- **Settle windows** (30 and 5 minutes) wait on a measurement (backlog item).
- **Config stamp.** An edit that keeps size, inode and modification time is not seen.
- **Platforms.** `flock` and atomic rename limit the tool to Unix-like systems (Linux, macOS, the BSDs); not Windows.
- **Thresholds** (8 workers, 5 s per request, the 5-second round pacing) are guesses.

## 7. Changes in revision 2

From the Codex critique of revision 1 (finding numbers are that file's):

| Finding | Change |
|---|---|
| 1, fatal: four workers cannot deliver a forced refresh | 8 workers, 5 s per request, 20 s for the view; rows cut off stay due. The criterion's wording goes to the user (B-refresh-now) |
| 2, fatal: import cycle; the cache key cannot be decoded | `deps` package; `UnmarshalText`; a round-trip test |
| 3, serious: one stamp stood for queued, sent and committed | No per-entry stamp before sending. One round stamp is written first; `LastAttempt` is set on send; a refusal is written at once. The stopped holder stays an open risk |
| 4, serious: invented failures; a probe that never resolves | The two-connection cutoff is deleted. Probe outcomes are defined; the probe avoids not-found tickers |
| 5, serious: no cache, no pacing | `print` does not fetch without a writable cache (B-unwritable); arriving snapshots are merged, never assigned |
| 6, serious: config identity and cache ownership | The config hash is deleted in favour of the modification time; pruning goes to the user (B-prune); memory only restores wanted instruments missing on disk |
| 7, serious: session evidence used inconsistently | `Classify` uses the market's state; the closing quote is a settle window, not a time-stamp match |
| 8, serious: cache size | Reported as a flaw in the criterion (B-cache-size) |
| 9, serious: I/O contracts | Stat in a command; order-independent merge; no cross-host redirects; `LookupEnv` |
| Excess | `LastProbe` and `RetryAfter` removed |
| Wrong fact | Platforms: Unix-like, not only Linux and macOS |

## 8. After round 2

Revision 2 was attacked by a second Codex session (`critique-converged-codex-2.md`). This file was not revised again: the remaining findings go to the user as questions or into the design document, and `synthesis.md` section 9 lists them with the converged points as they now stand (C1–C17). Where that section and this file differ, the synthesis is the later word. In particular: the round stamp of section 3 is replaced by per-row claims if option C of Q39 is chosen, the block record moves to a file of its own if Q41 says so, the 7-day prune is withdrawn in favour of the spec's rule unless Q60 says otherwise, and the cache keeps ten sparkline points per ticker.
