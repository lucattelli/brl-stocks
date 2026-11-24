# Critique of the approved design — Codex, round 3

Critic: Codex CLI 0.170.0, model `gpt-6.2-astra`, high reasoning, read-only, 2026-11-23, a fresh session that saw no earlier critique. Reviewed `docs/design.md` as approved in D63 (commit 64e259c), with `critic-brief-design.md`, scoped to the refresh protocol. The text below is the critic's final message, unedited; the coordinator only added this header. 45,406 tokens.

## Verdict

I would not build `Cycle` as written.  
Two processes can obey every lock and still overwrite a newer quote or exceed the source-wide request limit.  
The essential change is to define when a claim authorizes dispatch and commit, and how expiry, removal or a source block revokes that authority.

## Fact check

| Claim | Verdict | Source |
|---|---|---|
| A separate `flock` serializes cooperating writers across cache renames. | Right on local Linux and macOS filesystems if every writer opens the same persistent lock file. Replacing the lock file splits the writers. | [Linux `flock`](https://man7.org/linux/man-pages/man2/flock.2.html), [Apple `flock`](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/flock.2.html) |
| Death releases the lock, and acquisition takes milliseconds. | Death closes the holder’s descriptors. The lock survives a stop or a surviving duplicate descriptor. The time bound is false. Go’s `Flock` takes no context. | [Linux `flock`](https://man7.org/linux/man-pages/man2/flock.2.html), [Go `syscall.Flock`](https://pkg.go.dev/syscall?GOOS=darwin#Flock) |
| Temp-file rename permits lock-free reads. | Right for one completely written file renamed on the same filesystem. Two renames give no atomic pair. Power-loss durability also requires attention to the file and directory. | [Linux `rename`](https://man7.org/linux/man-pages/man2/rename.2.html), [Apple `rename`](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/rename.2.html), [Linux `fsync`](https://man7.org/linux/man-pages/man2/fsync.2.html) |
| Polling `stat` detects changes through a symlink. | It follows the target. Size, inode and mtime can all survive an in-place edit. A preserved mtime can predate the edit. | [`stat`](https://man7.org/linux/man-pages/man2/stat.2.html), [`utimensat`](https://man7.org/linux/man-pages/man2/utimensat.2.html) |
| `Now().Round(0)` and JSON timestamps support age comparisons across sleep and processes. | Right. Both discard Go’s process-local monotonic reading, which can stop during sleep. The resulting wall-clock comparisons follow clock adjustments. | [Go timekeeping](https://pkg.go.dev/time#hdr-Monotonic_Clocks) |
| `Instrument` works as a JSON map key. | Right with the specified value `MarshalText` and pointer `UnmarshalText`. Decode into a fresh map because Go retains existing keys when reusing one. | [Go `encoding/json`](https://pkg.go.dev/encoding/json#Unmarshal) |
| Cancellation stops outstanding requests. | Conditional. Cancellation signals workers asynchronously. Requests already sent still count. QR3’s 100 ms and the proposed round timings are **[unverified]**. | [Go `CancelFunc`](https://pkg.go.dev/context#CancelFunc), [design §4](docs/design.md) |

## Findings

1. **Fatal — Eight workers per process cannot enforce QR5.** At 0 s, B and C claim eight disjoint rows each and pause after unlocking. At 1 s, A receives a refusal and saves the block. At 2 s, B and C resume and each dispatch eight before receiving an answer. That is **16 requests after the first refusal**, against 10. A separate crash case needs only one watchlist. Kill the view between its first 429 and the source write. The forced `print` loop can send eight at claim expiry, then eight more 20 seconds later if the first retrying writer dies in the same gap. The refusal and its write are distinct events.

2. **Fatal — Expiry permits an old commit to win.** A claims X at 0 s, receives price 100 at 1 s and stops before commit. B reclaims at 20 s and commits 101 at 21 s. A resumes at 22 s, rereads and applies “its own result.” The user sees 100. If B instead removed X and completed a successful refresh, A’s old watchlist resurrects X. Re-adding it offline shows a price, violating S15.6. Commit needs to verify ownership against the current row.

3. **Fatal — A stopped lock holder defeats the deadlines.** Stop A inside either the claim or commit critical section at 0 s. B’s due `print` at 2 s can still be waiting at 14 s with a silent source, violating S13.9. A view waiting on the same lock can age past QR6 even with a healthy source. No bounded acquisition or contention outcome is specified.

4. **Serious — Losing source state reopens the floodgate.** At 0 s eight rows are refused. At 6 s replace `source.v1.json` with an empty record, leaving the refusals in cache. `print --refresh` can dispatch eight ordinary requests. Repeat at 12 s and **16** have followed the original refusal. A missing or corrupt source file has no recovery rule. A successful cache claim followed by a failed source write has the same exposure. With an unwritable directory, D60 suppresses `print`, but rereading empty health can also erase the view’s private block. This breaks 6.3, S13.10 and QR5.

5. **Serious — The probe has no complete lifecycle.** At minute five A claims the probe and dies. Honoring `ProbeClaimedAt` forever freezes recovery. Ignoring it lets the two-second loop probe repeatedly. A success must clear the block and reset refusals. One renewed refusal must double the wait exactly once. A timeout, malformed response, unsupported ticker or not-found result has no specified source transition. Nor is there a rule choosing an eligible ticker or preventing a late success from clearing a newer refusal. S7.5 and workflow 6.3 depend on these distinctions.

6. **Serious — Pruning amplifies ordinary polling.** Keep a view on one common row. Alternate plain `print` every two seconds between two lists, each adding eleven distinct rows. Each successful commit deletes the other eleven. Across 23 distinct entries and 300 calls, the prints alone can send **3,300 requests**. D61 accepts extra traffic. This quantifies the cost outside QR4’s fixed-list test.

7. **Serious — Claims outlive completed or abandoned work.** Claim 23 rows at 0 s. At `print`’s 10-second deadline, some remain undispatched. Committing only fetched rows leaves the others claimed until 20 s, contradicting D62’s next-heartbeat retry. A fetch completed at 1 s also needs explicit claim clearing or a force at 7 s skips it. Killing A immediately after claim leaves a `print` at 2 s serving five-minute-old data despite S13.3.

8. **Serious — Keeping a price does not define a merge.** The view holds 100. Empty the cache at 1 s. At 2 s `print` fails and writes failure metadata without a quote. Discarding that entry displays 100 as fresh. Adopting it wholesale loses 100. Workflow 6.2 requires **100, stale, aged from its original success**. A delayed load of an older valid cache can likewise undo a newer cycle result.

9. **Serious — Wall time can suppress work.** Claim at noon, then step the clock back an hour. The claim appears younger than 20 seconds for an hour, and the quote can appear fresh throughout. A forward jump expires a five-minute block immediately. Separately, replace the config at 12:01 preserving yesterday’s mtime after a noon not-found result. Even if the inode change triggers reload, rule 2 suppresses the retry required by 6.4 and S8.4.

10. **Minor — The interval boundary differs.** At exactly 60 seconds rule 6 fetches, while 6.5 and S13.2 require a cache hit. **Excess —** Rule 1’s five-second *claim* check removes no additional rows after its twenty-second check. The five-second *attempt* check matters.

## What holds up

With one intact watchlist, fast answers and ordinary timing, the view plus two-second `print` loop costs **230–253 requests**, within QR4’s 345. Fresh prints send zero. With a saved block and one functioning probe, eight initial requests plus one probe give **9**, within QR5’s 10. Emptying only the quote cache preserves that block. `Classify` correctly keeps a failed cached price stale across close, leaves a failed first fetch at no data, and gives not-found precedence. Config reads preserve QR7. One quote and ten points bound history, though QR8’s 10% byte-size tolerance still needs measurement.
