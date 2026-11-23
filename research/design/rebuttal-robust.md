# Rebuttal: proposal-robust

## 1. Serious findings

None was rated fatal. All five serious ones are correct.

- **S1 — CONCEDE.** My two windows do not overlap. Fix: interval polling stops at session end; a closed-market ticker is due whenever `FetchedAt < Session.End + settle`. `Classify` is unchanged, so `Closed` becomes reachable.
- **S2 — CONCEDE.** Fix: `Entry` gains `Failures int`; `MakePlan` skips an entry until `LastAttempt + wait(Failures)` (5, 10, 20, 40, 60 s; `NotFound` waits 1 hour, reset when the config changes).
- **S3 — CONCEDE.** Fix: promotion to `Schema` needs at least 3 decode failures and more than half the batch.
- **S4 — CONCEDE.** Fix: `Entry` gains a persisted `NextCheck time.Time`, set by `Cycle` after each fetch; `MakePlan` reads it and predicts nothing itself.
- **S5 — CONCEDE** that it was contradictory. Rule: `force` ignores session and interval, skips entries attempted under 5 s ago, and never beats `Health.NotBefore`. The only exception is `r` in the view: one probe request, at most once per 60 s. `print --refresh` gets no exception: a status bar can loop it.

## 2. Facts and excess

- **"Clock changes cannot desynchronise it": wrong, accepted.** The claim holds for sleep only. With M1: stamps more than 5 minutes ahead of now are discarded on load, and a fetched entry always wins the merge.
- **M2 accepted.** `Engine.Now` returns `time.Now().Round(0)` and is the only clock; the beat reads it and ignores the tick's payload.
- **M3 accepted.** A changed config is parsed only once its stamp is equal on two consecutive beats.
- **"`tea.Tick(60s)` breaks across sleep": overstated, accepted.** The heartbeat stays: it prevents config reload and ages freezing while markets are closed.
- **E1 wake detection: accept, deleted.**
- **E2: contest in part.** I drop the multi-config rationale. I keep disk ∪ memory ∪ fetched, which prevents the view's quotes being lost when the cache is truncated while the source is blocked, and the 7-day prune, which prevents removed tickers accumulating forever.
- **E3: accept.** `Router` and `Auth` wait for a second provider. HTML with status 200 becomes `Server` (5 to 60 s back-off); only 401, 403 and 429 mean `Blocked`. I keep `Symbol`'s `ok` result and `Unsupported`: they prevent a later one-market provider from changing the interface.
- **E4: accept.** Settle is per venue: 20 minutes for B3, 1 minute otherwise, one fetch each.
- **Missing: accept** a User-Agent option, cancellation on `q`, and temp-file cleanup under the lock.

## 3. The other designs

Take:
1. Simplest: per-entry failure count and attempt stamp (the S2 fix).
2. Extensible: its exact `Force` rule (the S5 fix).
3. Extensible: the caller's memory passed in as `Base` and merged under the lock.

Still wrong:
1. **Simplest has no lock and no source health.** A new entry has `Fails = 0` and is fetched into a blocked source; a lost whole-file write erases failure counters. Its own figure is 35 requests in ten minutes of 429, against my 6.
2. **Simplest's `State` calls a row closed whenever the cached session does not contain now.** After a 14-hour sleep, yesterday's session makes an open market read `closed`.
3. **Extensible hides cache faults and couples the files.** `Load` returns empty with a nil error for a corrupt file, so the user is never told; health lives in the quotes file, so one truncation also forgets a block.

## 4. Final positions

- **(a) Cache:** a lock held across the fetch; only then can two processes never both send requests.
- **(b) Provider call:** a batch; aborting on the first 429 belongs to the code that knows the source.
