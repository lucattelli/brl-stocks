# brl-stocks (stock monitor TUI) — Design

Status: approved, 2026-11-23 (D72), and updated on 2026-11-24 for D73. Earlier approvals: D63, D69. Open (section 7): Q62, and the protocol rules, which no critic attacked; the model proves six properties of them for two processes, and verification ran them in the built tool for the one-shot mode.
Sources: `research/grilling-log.md` (decisions D1–D74, facts F1–F32), `docs/spec.md`, design run in `research/design/`.

This document says how the product is built. What it does is in the spec; this document changes nothing there. Every choice cites the decision that made it and what it serves.

One thing is still open and is marked where it applies: the rules for close, open and holidays wait on research (Q62). The spec was updated for D60 and D62 on 2026-11-23.

## 1. Summary

1. One refresh path, `engine.Cycle`, is shared by the full-screen view and by `print`: plan, claim the rows, fetch, commit (D53).
2. There is no scheduler. A one-second heartbeat reads the wall clock and asks a pure function, `Plan`, what is due; row states are recomputed at every draw by a pure function, `Classify` (D53).
3. Two small versioned JSON files hold all shared state: the quote cache, with each row's last quote, claim, attempt and failure count, and the source file, with the block record. Readers never lock. Writers take a lock only for the read-modify-write; the fetch runs outside it (D54, D56).
4. The provider interface is one method, one ticker per call, with a typed error. The engine runs 8 requests in parallel and stops on a refusal (D55).
5. The view is a plain Bubble Tea 2 model whose update step does no file or network access (D57, D58).

## 2. Fixed inputs

| Decision | Source |
|---|---|
| Written in Go | D22 |
| Bubble Tea with Lip Gloss draws the full-screen view | D27 |
| The config is TOML, hand-edited, never written by the tool; it may sit behind a symbolic link | D12, D28, D29, D35 |
| Yahoo's unofficial chart endpoint is the default source, behind a provider interface | D7 |
| The fetching and formatting core is separate from the full-screen interface | D6 |
| An on-disk cache of the last quotes only | D20 |
| No secrets in the repository; a provider that needs a key reads it from the environment | D1 |
| Installed with `go install`; Go managed by mise | D36 |

## 3. Structure

```
cmd/brl-stocks   main: builds Deps and calls app.Run
deps             Deps: provider, clock, environment lookup, streams
app              Run(ctx, args, deps.Deps) int
market           Instrument, Quote, Session, Entry, Health, Snapshot; Parse, Plan, Classify, MarketState, Downsample
provider         Provider, Error, Kind;  provider/yahoo
cache            Files: Load, LoadSource, Lock, Write, WriteSource
config           Parse
engine           Cycle
render           Rows, Table, JSON, Sparkline, number formats
tui              the Bubble Tea model
```

Imports run one way: `app → tui, engine, render, config`; `tui → engine, render, market, deps`; `engine → cache, provider, market`. Nothing imports `app`.

| Part | Responsibility | Serves | Decision |
|---|---|---|---|
| `app` | Flags, config path, run mode, exit status. The whole tool runs through `Run`, so tests can drive it | S11–S14, QR7 | D41, D44, D45, D53 (C15) |
| `deps` | The injected edges: source, clock (`time.Now().Round(0)`), a timer (`After`, so that a test can make a deadline pass without waiting), `LookupEnv`, output streams | S13.9, S16.3, S17.2, QR3–QR6 | D53 (C15) |
| `market` | Types and every rule about time: what is due, what a row shows, whether a market is open. Pure | S5–S8, 6.1–6.5 | D53 (C1, C11) |
| `provider` | The boundary to a source; `yahoo` maps tickers to its symbols | S17, QR12 | D7, D55 |
| `cache` | The two files and the lock | S15, QR2, QR8 | D54, D56 |
| `config` | Bytes to a config, with warnings. Pure | S11, S12 | D42, D45 |
| `engine` | The one refresh path | S5–S8, S13, QR4, QR5 | D53 (C2), D54, D55 |
| `render` | Rows, the table, the JSON document, number formats per currency. Pure | S1–S4, S9, S10, S13, S14, S16 | D43, D46, D51 |
| `tui` | Keys, layout, heartbeat; turns messages into commands | S1, S9, QR11 | D57, D58 |

Signatures that pin the boundaries down:

```go
package market
type Market string                                  // "B3", "US", "IDX", "FX"
type Instrument struct{ Market Market; Symbol string }
func (i Instrument) MarshalText() ([]byte, error)   // "B3:PETR4", "IDX:IBOV", "FX:USDBRL"
func (i *Instrument) UnmarshalText(b []byte) error  // exact inverse; round-trip tested (C3, F25)

type Entry struct {
    Quote       *Quote      // nil until the first success; Series holds at most 10 points (C14)
    FetchedAt   time.Time   // last success
    ClaimedAt   time.Time   // a process is about to request this row
    ClaimID     string      // which process and round made the claim; random per round
    LastAttempt time.Time   // when the last request for it was answered or timed out
    Failures    int         // consecutive
    Fail        *Failure    // last failure, nil after a success
}
type Health struct {        // one per source name, in the source file
    BlockedUntil time.Time
    Refusals     int
    ProbeClaimedAt time.Time  // expires after 20 seconds, like a row's claim
    UpdatedAt    time.Time    // when this record was last written; the later record wins
    Gen          int          // goes up by one at every write of the record
}
func Plan(in PlanInput) PlanResult      // Want, Snap, Health, Interval, Now, ConfigMTime, Force → Due, Probe, NextTry
func MarketState(m Market, s Snapshot, now time.Time) (open, known bool)
func Classify(e Entry, marketOpen bool, interval time.Duration, now time.Time) Display

package provider
type Provider interface {
    Name() string
    Quote(ctx context.Context, in market.Instrument) (market.Quote, error) // any error is a *Error
}
// Kinds: NotFound, Unsupported (this ticker); RateLimited, Blocked (refusals: 429; 401, 403);
// Unavailable (network, timeout, 5xx); BadResponse (does not parse, or is for another symbol).

package engine
func (e *Engine) Cycle(ctx context.Context, r Request) (snap market.Snapshot, health market.Health, warn error)
```

**`Plan`.** For the whole source first: during a block nothing is due, forced or not (D38); when the wait has ended, one probe. Then per row, in order:

1. Claimed by a process less than 20 seconds ago: not due. A forced refresh also skips a row attempted less than 5 seconds ago (C6). A claim or attempt stamped later than now is ignored.
2. Not found: due only an hour after the last attempt, or when the config file is newer than that attempt (D39, C8). A forced refresh does not override this.
3. Forced: due.
4. After failures: not due until the last attempt plus interval × 2^(failures−1), at most 30 minutes (D50).
5. No quote: due, whatever the market (D50).
6. The market is open: due when the last success is more than one interval old (D14, D23).
7. After the close, before the next open, on a holiday: **provisional, waits on Q62** (see section 7).
8. Otherwise not due (D15).

**`Classify`**, in this order: not found; no data (no quote); stale (the last fetch failed, or the market is open and the last success is older than two intervals, D40); fresh.

## 4. Design decisions

### Converged points C1–C17 (D53)

- **Chosen:** the seventeen points of `research/design/synthesis.md` section 9. The ones that shape the code: a heartbeat instead of a scheduler (C1); one refresh path (C2); the cache as a versioned JSON file written by temp file and rename, with lock-free readers (C3); the view merges what it reads from disk and never replaces a quote with nothing (C4, C17); waits kept per row in the cache (C5); the provider checks the answer's symbol and refuses redirects to another host (C9); a refusal is saved at once and followed by one probe (C13); the cache keeps ten sparkline points per row (C14); tests inject source, clock and environment (C15).
- **Serves:** QR2–QR8, QR11, QR12; S5–S8, S13, S15.
- **Rejected:** a scheduler goroutine, per-row timers, a daemon serving `print`, a file watcher, one file per ticker, an embedded database (`proposal-*.md`, section "What I rejected").
- **Consequence:** the tool wakes once a second even when every market is closed; that costs two `stat` calls. A config change is seen within about two seconds (S11.3 allows five).
- **Risk accepted:** the numbers in C16 (8 requests in parallel, 5 seconds each, rounds of 10 and 20 seconds) are guesses.

### A short lock, per-row claims, fetch outside the lock (D54)

- **Chosen:** `Cycle` runs in three steps. *Claim:* take the lock, re-read both files, plan against what is on disk, mark the rows it will fetch as claimed, write, release. *Fetch:* outside the lock. *Commit:* take the lock, re-read, apply its own results to the rows it fetched, drop rows that are not in its watchlist (D61), write, release. The lock is a `flock` on a separate empty file and is held for milliseconds.
- **Serves:** QR4, S13.2, S13.3, S13.16, S15.
- **Rejected:** a lock held across the fetch (a `print` with another watchlist prints old quotes without fetching; a stopped holder freezes every caller: `critique-converged-codex-2.md`, finding 3). No lock (two writers can erase failure counts or a block: `rebuttal-extensible.md`).
- **Consequence:** two processes with different watchlists can fetch at the same time. A row claimed by a process that then dies is not fetched by anyone for 20 seconds.
- **Risk accepted:** the protocol was attacked once, in the third round, which found three fatal and six serious faults in it as first written. The rules that answer them are in "The refresh protocol in detail" below; those rules have not been attacked.

### One ticker per provider call (D55)

- **Chosen:** `Quote(ctx, instrument)`. The engine owns the 8 workers; on the first refusal it stops handing out work, saves the block, then cancels what is in flight.
- **Serves:** QR5, S7.3, S17.2.
- **Rejected:** a batch call with provider-owned parallelism (`proposal-robust.md`, `proposal-extensible.md`): every provider and stand-in would reimplement stop-on-refusal.
- **Consequence:** a source with a real batch endpoint needs the interface widened.

### The block record in a file of its own (D56)

- **Chosen:** `source.v1.json` holds, per source name, the time until which it is blocked, the count of refusals in a row and the probe claim. It is written under the same lock. The file is the authority: a process always takes its value as read and never merges it with memory.
- **Serves:** QR5, S7.3–S7.5, S13.10.
- **Rejected:** keeping it in the quote cache (`critique-converged-codex-2.md`, finding 2: an emptied or replaced cache forgets the block).
- **Consequence:** the two files can disagree for the instant between two writes; a reader that sees a quote without its block makes at most one extra probe.

### The refresh protocol in detail (D54, D56; after the third attack)

Added after `research/design/critique-design-codex-3.md`. Each rule names the finding it answers.

1. **The lock is never waited on for long.** It is tried without blocking, again every 50 milliseconds, for at most one second. If it is not obtained, the cycle ends: `print` prints what it read, the view tries again at the next heartbeat (finding 3).
2. **A claim has an owner.** A round draws a random claim id and writes it with the time on every row it claims. At commit, a result is applied to a row only if the row is still in the file with that same claim id. A row that was re-claimed by another process, or removed, is left as it is (finding 2).
3. **A commit releases every claim of its round,** including rows the deadline cut off before they were requested, so those are due at the next heartbeat (finding 7, D62).
4. **A worker looks at the source file before it sends each request,** without the lock. A block that another process saved in the meantime ends the round, so rows claimed before a refusal are not requested after it (finding 1, QR5).
5. **A refusal is in the cache as well as in the source file.** A refused row records the kind of its failure. If the source file is missing, empty or shows no block while a wanted row was refused less than five minutes ago, the source is treated as blocked until five minutes after that attempt. The view never drops a block it holds in memory because of an empty read; only a record with a later `UpdatedAt` replaces it (finding 4).
6. **The probe.** When a block's wait has ended, one process claims the probe in the source file; the claim expires after 20 seconds. The probe asks for the first wanted row that has a quote, or else the first that is not not-found; with neither, there is no probe until a row comes due by its own rule. Outcomes: a refusal doubles the wait once; any other answer from the source (a quote, or not found) clears the block and the refusal count; no usable answer (timeout, connection error, server error, unreadable body) keeps the count and sets the next attempt one interval ahead. The record carries a counter that goes up at every write. A probe remembers the counter's value from its own claim, and its success clears the block only if the counter still has that value; two events in the same instant cannot be told apart by their time stamps (finding 5; the counter comes from the model).
7. **Merging two copies of a row** (disk and memory, in the view): the quote and its fetch time come from the copy with the later successful fetch; the last attempt, the failure and the failure count come from the copy with the later attempt. A row with a quote and a later failed attempt is therefore the old price, stale, aged from its last success (finding 8, spec 6.2).
8. **Clocks.** A stamp in the future is void: a claim is ignored, a block that ends more than 30 minutes ahead is cut to 30 minutes. A clock that jumps forward can end a block early; the cost is one probe. In the view, reloading a changed config retries not-found rows whatever the file's modification time; `print` has only the modification time (finding 9).

9. **Only a refused probe lengthens the wait.** A refusal of a request that was sent before the block was known is recorded on its row and leaves the wait as it is. Otherwise eight requests refused together in one round would take the wait from 5 minutes straight to the 30-minute limit (from the model).

The protocol is modelled in `models/refresh.qnt` (Quint). Its six properties: a newer quote is never replaced by an older one; a removed row never gets a quote again; a probe never clears a block saved after it started; two processes fetch the same row only after the older claim expired; the wait grows only on refused probes; requests stay bounded while the source keeps refusing.

### A testing-only address for the source (D70)

- **Chosen:** when the environment variable `BRL_STOCKS_SOURCE_URL` is set, the default source is reached at that address instead of its own. It is absent from the help text.
- **Serves:** verification of the built binary against a local stand-in that stages answers and counts requests (QR4, QR5, S7, S8, S13).
- **Rejected:** a data file passed as an argument (static, counts nothing, skips the network code); a separate test build (not the shipped binary); the real source only (failures cannot be staged).
- **Consequence:** the rule against redirects to another host (QR12) applies to the address in use. Acceptance tests inside the code keep injecting the source through `Deps`; this setting is for runs of the real binary.

### A one-shot call waits for a row that is being fetched (D73)

- **Chosen:** when a wanted row has no quote and another round holds its claim, `print` reads the cache again every 100 milliseconds until the claim is released or its 10 seconds are over, then runs one more cycle for whatever is still due.
- **Serves:** S13.22, S13.23, spec 6.5.
- **Rejected:** printing at once (six or seven of ten simultaneous calls printed empty rows, F32).
- **Consequence:** only rows with no quote at all cause a wait; a row with an old quote is printed as it is.

### No step layer (D57) and Bubble Tea 2 (D58)

- **Chosen:** the model's update step maps messages to state and commands directly. The heartbeat, the result of a `stat`, a loaded config, a loaded cache and a finished cycle are each a message. Version 2 of the library, module `charm.land/bubbletea/v2`.
- **Serves:** S1, S9, QR11.
- **Rejected:** a `Step(State, Event)` function returning effects as data (`proposal-extensible.md`, `rebuttal-extensible.md` E1). Version 1, which has had no release since 2025-09 (F18).
- **Consequence:** tests of the view send messages to the model, run the commands it returns against a stand-in source and a fixed clock, and assert on the rendered rows.

### Platforms (D59)

- **Chosen:** Linux and macOS.
- **Consequence:** no Windows build. The BSDs have the same file lock (F24) and are untested.

### Without a writable cache (D60)

- **Chosen:** if the claim cannot be written, `print` does not fetch; it prints what it could read and warns on the error stream. The view fetches and keeps its claims in memory.
- **Serves:** QR4, QR5.
- **Consequence:** the spec's S15.5 applies to the view only; S15.9 covers `print`.

### Forced refresh (D62)

- **Chosen:** `r` and `--refresh` start a cycle at once; rows are requested 8 at a time until the round's deadline. Rows the deadline cut off were never requested, keep no failure, and are due at the next heartbeat.
- **Consequence:** the spec's S7.1, S7.2 and 6.3 say "starts within 2 seconds"; S7.9 says every row follows within one interval.

## 5. Failure behaviour

| Situation | What happens | Story |
|---|---|---|
| The source answers 429 to everything for ten minutes | Up to 8 requests are in flight. The first refusal stops the hand-out and writes the block (5 minutes). Refused rows turn stale; the error line names the block and the status line the next attempt; `r` sends nothing. At minute five one probe is refused and the wait becomes 10 minutes. At most 9 requests in ten minutes. Every `print` reads the source file and sends nothing | S6.1–S6.3, S7.3–S7.5, S13.10, QR5 |
| `print --json` every 2 seconds beside an open view | Each call loads config and both files, finds nothing due, prints: no lock, no request. When rows come due, the first process to claim them fetches; the other sees the claims and prints the cache. The view sees the cache file change and merges it | S13.2, S14.1, QR3, QR4 |
| The machine wakes after 14 hours with the view open | The next heartbeat reads the wall clock. Rows past their predicted open are due at once. If the network is not up, each row fails and waits one interval; `r` retries at once. Until a success, rows show their age | S6.7, S7.2, S5.7 |
| The config is edited while the view is open | The heartbeat's `stat` sees a new stamp; a command parses the file. Valid: removed rows go, added rows have no quote and are due, not-found rows are due because the file is newer than their last attempt. Invalid: the old config stays and the error line names the problem | S8.4, S11.3–S11.6, S11.10, S11.11 |
| The cache file is emptied | A lock-free load returns nothing, silently. The view keeps what it shows and writes it back at its next commit. A `print` alone finds every row due and fetches once. The block record is in the other file and survives | S15.4, S15.7, QR5 |
| A process is killed in the middle of a round | Its claims expire after 20 seconds and the rows become due again. Nothing is recorded as a failure. A refusal it had already seen is on disk | S13.3, QR5 |
| Two processes hold claims when a third is refused | The third saves the block. The other two read the source file before each request, see the block and end their rounds; their commits release their claims | QR5 |
| A process stops after fetching and resumes after another process refreshed the same row | Its commit finds a different claim id on the row and leaves the newer quote alone | S15, 6.2 |
| A process is stopped while it holds the lock | Others try the lock for one second, then go on without it: `print` prints the cache, the view retries at the next heartbeat | S13.9 |
| The cache folder cannot be written | The view warns once and paces itself from memory. `print` prints what it can read, without fetching, and warns | S15.5 (view), S15.8, QR4 |

## 6. Dependencies

| Dependency | Version | Why | Decision |
|---|---|---|---|
| Bubble Tea | v2.1.2 (checked 2026-11-23, F18) | The full-screen view | D27, D58 |
| Lip Gloss | The release that pairs with Bubble Tea 2; to confirm at the start of the build | Layout and colour from the terminal's palette | D27, D25 |
| A TOML parser (`github.com/BurntSushi/toml` proposed) | To confirm at the start of the build | The config file | D28 |

Nothing else beyond the standard library: flags, HTTP, JSON, the file lock, and number formatting by hand (`proposal-simplest.md`, section 4).

## 7. Risks and unverified facts

| Item | What would show it is wrong | What changes if it is |
|---|---|---|
| **Close, open and holidays (Q62, open).** The provisional rule: refresh for a settle window after the close (30 minutes for B3, 5 for the others); check again at the predicted next open, and every 5 minutes for 30 minutes if the answer still carries the old session. It rests on F14 and costs up to 7 requests per row on a holiday, where spec 6.1 says none | The holiday check on 2026-11-26; a measurement of when B3's closing quote arrives; a day with a late opening (F26) | Rule 7 of `Plan`, `MarketState`, and possibly the spec's 6.1 |
| **The settle window is too short for B3 (F30).** On 2026-11-23 the quote time of a B3 ticker was 65 minutes past the session end the source reports | A watch of one B3 ticker from 19:30 to 21:45 UTC on a trading day | The settle window in rule 7 and the 30 minutes of D50 (P31), through Q62 |
| The rules of "The refresh protocol in detail" were written after the last attack round and were not attacked by a critic. The Quint model checks six properties of them: exhaustively for every run of up to 19 steps with two processes, without a violation; the complete state space and three processes are not yet covered | Tests with two and three processes against a counting stand-in source: a process killed between claim and commit, stopped inside the lock, refused while others hold claims. The model run to completion | `engine.Cycle` |
| **Two watchlists at once are not supported (D68, accepted).** With one shared cache, two `print` loops on different configs prune each other's rows at every commit and refetch them at every call: up to about 3,300 requests in ten minutes for 23 entries (F31). Nothing in the tool prevents it | A block by the source while two configs are in use | A cache per config file, or a way to print one group, would remove it; neither is designed |
| A process killed between receiving a refusal and writing it, twice in a row, lets a second and third round start: up to 8 requests each, above QR5's 10 | Not testable short of fault injection | Accepted; the window is the few milliseconds of one write |
| Only 401, 403 and 429 count as a refusal. A 404 with a JSON "Not Found" error is an unknown ticker (F27) | A refusal that arrives as 200 with an error body, or as a 404 without that body (backlog) | `provider/yahoo`, and the count under QR5 |
| The numbers of C16 | QR11 and S13.9 measured against a slow stand-in | Constants in `engine` |
| A config edit that keeps size, inode and modification time is not seen; one that keeps an older modification time is reloaded but does not retry not-found rows | A test that replaces the file with `cp -p` | The stamp, or C8's rule |
| The Lip Gloss and TOML versions are not checked | The first build | Section 6 |

## 8. Not designed

- A second data source, a router between sources, and API-key handling (D53, C10).
- Any user setting for the source, including how the tool identifies itself (D50, C12).
- Windows (D59).
- Two different watchlists running at the same time: one shared cache, one watchlist at a time (D61, D68).
- A step layer for the view (D57); alerts, a detail view, in-app editing and a portfolio, which were its reasons (D3, D12, D13).
- A market calendar (D15 is met from the data fetched; see Q62).
- Wiring into `the-2026-dev-env` and the install step, which are delivery work (D34, D36).
