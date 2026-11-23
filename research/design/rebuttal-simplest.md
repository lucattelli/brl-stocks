# Rebuttal: proposal-simplest

The review found no fatal problem and three serious ones. All three are real; none needs a new mechanism.

## 1. Serious findings

**S1 (view lags when another process wins the claim): CONCEDE.** The replay is correct, and my walk-through ii hid it.
Fix: the tick callback already stats the config; it also stats `cache.json`. When the mtime differs and no refresh is in flight, the view runs `core.Load` in a command and merges the result through the existing `refreshedMsg` path. One field (`cacheMod`), no new message type.

**S2 (failing entries ignore market hours): CONCEDE.** Rule 2 was too broad, and my claim in walk-through i was true only for entries that were not failing.
Fix: rule 2 becomes "no quote: due". `Fails > 0` only lengthens the wait in rule 1; an entry that has a quote always goes on to rules 3 to 5. Rule 4 still picks up a close that failed. For the typo case, `wait` is one hour when the kind is `not_found` or `unsupported`.

**S3 (`State` undefined): CONCEDE.** A signature is not a contract. Definition, in order:
- `error`: no quote.
- `stale`: `Fails > 0`, or the projected session is open and `FetchedAt` is older than two intervals.
- `closed`: now is outside the session the source reported.
- `ok`: otherwise.

Two intervals means rows do not blink during a round, and a killed claimer shows as stale after two minutes. With S1 fixed, the lagging view no longer exists to be mislabelled.

## 2. Facts

- **Lost update costs "one refetch": accept, I was wrong.** The claim survives, so the old quote is served for one more interval. The window stays a few milliseconds; I change the text, not the design.
- **`os.CreateTemp` leaves a file behind: accept.** `defer os.Remove(tmp)` in `save`.
- **Rename is not atomic off Unix: accept.** The design supports Linux and macOS and says so.
- **Bubble Tea version: accept.** It targets v1, `github.com/charmbracelet/bubbletea`.
- **Tick after wake: no contest.** Either clock behaviour gives a tick within about a second.

Minor findings I also take, because each is one line: `fetching` starts `true` (S7), `refresh_seconds` is clamped to a minimum of 10 (S5), a browser-like User-Agent constant in the Yahoo provider.

## 3. The other designs

**I would take:**
1. From robust: the cache-file stamp on the heartbeat. It is the S1 fix.
2. From extensible: a forced refresh skips entries attempted less than 5 seconds ago. That closes my own weak point about `--refresh` in a status bar.
3. From robust: the provider checks that the response's symbol is the one requested. One comparison against a wrong price on a row.

**Still wrong for one person and 20 tickers:**
1. **A lock held across the fetch (both).** It makes every other process depend on the slowest one, needs lock waits and budgets (3 s, 5 s, 8 s, 15 s), and is platform-specific. The reviewer could not widen my race beyond milliseconds and noted that a lock would not have fixed S1 to S3.
2. **`monitor.Step` with events and effects (extensible).** Bubble Tea's `Update` already is a pure function from message to model and commands. A second copy of that pattern, in nine packages, is structure for features the log defers.
3. **A separate source-health state machine (robust).** Four statuses, a second file, and thresholds ("more than half", 2 to 30 minutes) that are guesses. Per-entry counters plus cancelling the round give the same walk-through i with one rule.

## 4. Final positions

- **(a) Cache:** no lock. Atomic rename plus the claim stamp; the worst case is one duplicated round or one interval of an old quote.
- **(b) Provider call:** one ticker per call. No free source in the research can batch, and a batch signature moves concurrency and partial failure into every provider.
