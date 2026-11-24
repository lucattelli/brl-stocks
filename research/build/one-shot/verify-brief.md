# Verifier brief: long checks for the one-shot mode

Second verification run of the one-shot slice (`brl-stocks print`), written on 2026-11-24. It extends the first run, which was done by hand before this brief existed (F32, `verification/README.md`). This run adds only the checks that need real time: QR4 and QR5 at their full 10 minutes, the doubling of the wait after a second refusal (6.3), and the 8-hour cache-size criterion (QR8).

```
You are an independent verifier. A terminal stock monitor (a Go command-line tool) was built, and your job is to find out, by RUNNING THE REAL BUILD, whether it does what its specification says. You did not write it. You do not fix it. A check that fails is a finding to report, never something to adjust until it passes.

Work in /home/lucattelli/Programming/prototyping/brl-stocks (Go; run the toolchain through mise: `mise exec -- go ...`). Use Go only; do not use any other runtime, and do not install anything.

## Read

- docs/spec.md: sections 6 (workflows, especially 6.2, 6.3 and 6.5) and 7 (behaviour seen from outside), story S15, and quality requirements QR4, QR5 and QR8.
- verification/: the stand-in, the checks and the README written by the first verifier, from the spec alone. Reuse its stand-in and helpers (startStandin, newEnv, env.run, the stand-in's /_control and /_requests, the evidence writer).
- Nothing else. Do NOT read internal/, cmd/, models/, research/, docs/design.md or any *_test.go outside verification/. You verify behaviour from outside; knowing the code would bias what you check.

## The product's edges

- Build: `go build -o <tmp>/brl-stocks ./cmd/brl-stocks` (TestMain in verification/print_verify_test.go already does this, together with the stand-in).
- How it is run: `brl-stocks print [--json] [--refresh]`; see spec section 7 for output and exit statuses.
- Config and files: config at $XDG_CONFIG_HOME/brl-stocks/config.toml, cache under $XDG_CACHE_HOME/brl-stocks (or ~/.cache/brl-stocks). newEnv already points HOME, XDG_CONFIG_HOME, XDG_CACHE_HOME and TMPDIR at temporary folders; keep it that way.
- The dependency: by default the tool calls Yahoo's chart endpoint. BRL_STOCKS_SOURCE_URL makes it call an address you give. ALWAYS set it to your local stand-in (the existing helper does, and refuses anything that is not http://127.0.0.1:...). Never call the real service.
- What the tool asks and what the stand-in answers: as the existing stand-in already implements (GET /v8/finance/chart/<symbol>?interval=5m&range=1d; made-up quotes with trading periods relative to now). A refusal is status 429; an unknown ticker is 404 with a JSON error; 503 or a closed port is "unreachable".
- Real-time numbers: the smallest refresh interval is 10 seconds (spec 7.6); the default is 60. After a refusal the tool waits 5 minutes before one probe; a refused probe doubles the wait, up to 30 minutes (6.3, D50).

## Build

Put the new checks in a NEW file, verification/long_verify_test.go, with the same `verification` build tag. Do NOT edit verification/print_verify_test.go: someone else is changing it at the same time. If you need a helper the existing file lacks, add it in your file. If the stand-in needs a new control (for example, a different quote on every answer), add it to verification/standin/main.go without changing what existing controls do.

Each long check is behind its own switch so the ordinary run stays about 1 minute:
- BRL_VERIFY_SLOW=1 for checks up to about 20 minutes;
- BRL_VERIFY_LONG=1 for the full 8-hour run.
Long checks may run in parallel with each other (t.Parallel); each has its own stand-in and folders.

Evidence: as the existing checks do, under verification/evidence/<run>/<check>.txt (already ignored). For long checks, also write the stand-in's full request record, and for QR8 the cache size at every sample.

## What to verify

1. QR4 at its full size, without the view (the full-screen view is not built yet): open market, 60-second interval, 23 entries (20 tickers plus the 3 summary entries), `brl-stocks print` called every 2 seconds for 10 minutes. Assert at most 345 requests in total, and report the count per entry and the largest per entry. Behind BRL_VERIFY_SLOW.
2. QR5 at its full size, and the doubling: a stand-in that refuses everything from the first request, `brl-stocks print --refresh` every 2 seconds. Assert at most 10 requests in any 10-minute window over the whole run. Keep it running long enough to see the wait double: no request from the refused probe at about 5 minutes until about 10 minutes after it, then one probe at about 15 minutes. Report the arrival time of every request relative to the first refusal. Behind BRL_VERIFY_SLOW (about 17 minutes).
3. QR8 (and S15.3 as far as `print` can show it; S15.3 itself needs the view, which does not exist yet): open market the whole time, a stand-in whose quotes change on every answer, `brl-stocks print` once per interval. Measure the cache folder's total size after the first full refresh and at every sample; assert every sample is within 10% of the first. The duration comes from BRL_VERIFY_LONG_DURATION (default 8h) behind BRL_VERIFY_LONG; also provide a scaled run of 20 minutes at a 10-second interval behind BRL_VERIFY_SLOW. Run the scaled one only; do NOT run the 8-hour one yourself. Say in the feature map that the 8-hour run is to be started separately.

Not in scope: the full-screen view, colour, the hourly wait of a not-found row.

## Run and report

Run the ordinary checks once (to confirm nothing broke), then the BRL_VERIFY_SLOW checks once (in parallel, so about 20 minutes). Then update verification/README.md: add the run commands for the new switches, and update the feature-map rows for QR4, QR5, S15.3 and the 6.3 doubling, and add a row for QR8. Change no other rows. Change nothing outside verification/. Do not run git commands that change the repository.

Return a summary of at most 350 words: how many checks, how many pass and fail; every FAILING check with the criterion, what the spec led you to expect and what you observed (quote the output or the record); the QR4 counts; the QR5 arrival times; the QR8 sizes; what you could not verify and why; and anything in the spec that was ambiguous for a verifier.
```
