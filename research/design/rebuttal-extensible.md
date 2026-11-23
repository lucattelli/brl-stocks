# Rebuttal: proposal-extensible

## 1. Serious findings

No finding is rebutted outright. The reviewer's facts are right in all six.

- **S1. CONCEDE the waiting, not the lock.** Change: `Update` takes the lock with `LOCK_EX|LOCK_NB` and never waits; a loser shows what it has at once. The round gets one total deadline (15 s), which replaces the "in flight for 30 s is lost" rule, so a process can never open the lock twice. `Timeout` on two instruments in a round becomes source-wide.- **S2. CONCEDE.** `Refresh` creates the cache folder, always returns what it fetched, and reports a cache error as a notice. With an unwritable cache the view paces itself from memory (`Base`); one-shot does not fetch and prints the error state, so a 2 s status bar cannot storm.
- **S3. CONCEDE.** After the session end an entry stays due, at the normal interval, until `Quote.Time >= Session.End` or 30 minutes have passed.
- **S4. CONCEDE.** `r` overrides a `Network` wait. It does not override `RateLimited` or `Blocked`, and then the status line says when the next attempt is.
- **S5. CONCEDE.** Covered by S1: no blocking `Flock`, no goroutine, no polling loop.
- **S6. CONCEDE.** If a probe at the predicted open returns a session that is still in the past, the entry is due again every 5 minutes for 30 minutes, then tomorrow.

## 2. Facts and seams

Facts accepted: `Flock` takes no context; two opens in one process block each other; `Instrument` must implement `TextMarshaler`; the Bubble Tea major version is unaddressed.

- **E1 `monitor.Step`: KEEP.** A `tea.Cmd` is an opaque function, so an `Update` test can only assert on commands by running them. With `Step`, "tick at 10:00:05 emits `Refresh{Want: [PETR4]}`" is a value comparison. S1's double-refresh is exactly the bug such a table test catches. It also confines the open v1/v2 question to one adapter file. Adding it later means rewriting every view test.
- **E2: CONCEDE most.** Drop `Router`, `Supports`, the factory map, `Settings`, `Auth`, the history interface and `SourceOf`. KEEP `Name()` and health keyed by source name in the cache: one map key now, a schema change later.
- **E3 `Store` interface: CONCEDE.** A concrete `cache.File`, tested on `t.TempDir()`.
- **E4 `Base`/`Merge`: KEEP.** S2's fix depends on it: memory is what lets the view survive a disk that fails.- **E5: CONCEDE** `fsync`, `LockWait`, `ErrBusy` as a wait, and `Kind` (markets become B3, US, IDX, FX). KEEP pruning: three lines.

## 3. From the other designs

Take:
1. Robust's `TryLock` with "print at once when busy". Fixes S1, S5 and M1.
2. Robust's `Quote.Validate`, used by the provider and on load. Fixes M5.
3. Robust's schema number in the file name. Fixes M3.

Still wrong:
1. Simplest's claim stamp without a lock. It writes the whole file twice per round and can drop another process's results; it rebuilds a lock from weaker parts.
2. Simplest's `Refresh(ctx, p, cachePath, …)` has no clock parameter, so the engine cannot be tested without the real clock. Its per-entry counters as the only breaker also cost about 35 requests in walk-through (i), against 8.
3. Robust's heuristics inside the provider ("more than half fail → Schema", "HTML → Blocked" for minutes, shared by all processes). A captive portal silences every process, and each threshold is a guess that no fixture can justify.

## 4. Final positions

- **(a) Cache:** a lock held across the fetch, taken non-blocking and never waited on; readers lock-free.
- **(b) Provider call:** a batch. The provider owns concurrency and aborts on a source-wide failure; a one-ticker source loops inside it.
