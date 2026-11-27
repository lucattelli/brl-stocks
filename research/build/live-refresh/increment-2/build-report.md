# Builder's report: live refresh, increment 2

Returned 2026-11-25 for the builder section of `briefs.md` (the increment-1 builder, resumed). The agent's summary, unedited:

---

The open view now picks up quotes that other processes fetch. Committed as f1deb03 on slice3/2-build in /home/lucattelli/Programming/prototyping/brl-stocks-wt-build.

**How it works:** At each heartbeat the view checks the timestamps and sizes of the two cache files, and reads them again only if they changed. It merges what it reads without ever replacing a quote with nothing. It replaces the block record only with a later one (protocol rule 5); block behaviour is otherwise unchanged, so Q94 stays open. The changes are in `internal/tui/tui.go` and `internal/cache/cache.go` (new `Stamp()`). In the test driver the file check runs in the driver's loop, when the heartbeat fires.

**Suite time:** My first version made the race run 96 s, because every heartbeat drew the screen twice. Each draw also laid the screen out twice. I fixed both: with -race the app suite now takes 40.2 s and 39.2 s on two runs, and 9.3 s without -race. Everything passes, go vet and gofmt are clean, and the ordinary verification passes (233 s).

**How I showed it:** I ran the built binary under `script` against the stand-in, with `refresh_seconds = 600`:
1. The view opened and sent 2 requests.
2. I set PETR4 to 41.00 on the stand-in and ran `print --refresh` as a second process, which sent 2 more requests.
3. Within 2 s the view's screen showed `41,00`. The total stayed at 4 requests, so the view sent none of its own.
4. I emptied `cache.v1.json` and the view redrew nothing, keeping its quotes.

---

## Coordinator's check and sorting

- Checked: no test file changed; `go vet`, `gofmt` clean; `go test ./... -race -count=1` passes twice (42 s each). The stat and the read run in a command when the heartbeat's wait ends, outside `Update` (D57).
- Q94 was answered meanwhile (D98, as built); nothing to change.
- The view taking in other processes' fetches has no acceptance test in this increment; the builder's run above shows it, and QR4 in the verification measures it. Noted in the PR.
