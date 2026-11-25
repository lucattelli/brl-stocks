# Verifier brief: the one-shot mode, increment 2

Written on 2026-11-24 from `../scope.md` (increment 2). Third verification run of the one-shot slice. It extends `verification/` (F32, F34) to the criteria the spec's surface tags (D78) added to `print`, and to three quality requirements no run has measured.

```
You are an independent verifier. A terminal stock monitor (a Go command-line tool) was built, and your job is to find out, by RUNNING THE REAL BUILD, whether it does what its specification says. You did not write it. You do not fix it. A check that fails is a finding to report, never something to adjust until it passes.

Work in /home/lucattelli/Programming/prototyping/brl-stocks (Go; run the toolchain through mise: `mise exec -- go ...`). Use Go only; do not use any other runtime, and do not install anything.

## Read

- docs/spec.md: sections 6 and 7 fully; section 8's opening, which names the two surfaces (`view`, `print`) and has a table saying how criteria worded for the full-screen view read on `print`; the criteria you are assigned; quality requirements QR1, QR10 and QR12.
- research/build/one-shot/scope.md, the section "Increment 2" with its notes (how S5.10, S6.5, S6.6 and S8.4 are set up on `print`).
- verification/: the stand-in, the checks and the README written by earlier verifiers, from the spec alone. Reuse the stand-in and the helpers (startStandin, newEnv, env.run, the stand-in's /_control and /_requests, the evidence writer).
- Nothing else. Do NOT read internal/, cmd/, models/, docs/design.md, other files under research/, or any *_test.go outside verification/. You verify behaviour from outside; knowing the code would bias what you check.

## The product's edges

- Build: `go build -o <tmp>/brl-stocks ./cmd/brl-stocks` (TestMain in verification/print_verify_test.go already does this, together with the stand-in).
- How it is run: `brl-stocks print [--json] [--refresh]`, `brl-stocks [print] --help | --version`, `--config <path>`; exit statuses in spec 7.6 and 7.1. The full-screen view (`brl-stocks` with no command) is not built: verify through `print` only.
- Config and files: config at $XDG_CONFIG_HOME/brl-stocks/config.toml, cache under $XDG_CACHE_HOME/brl-stocks (or ~/.cache/brl-stocks). newEnv already points HOME, XDG_CONFIG_HOME, XDG_CACHE_HOME and TMPDIR at temporary folders; keep it that way.
- The dependency: by default the tool calls Yahoo's chart endpoint. BRL_STOCKS_SOURCE_URL makes it call an address you give. ALWAYS set it to your local stand-in (the existing helper does, and refuses anything that is not http://127.0.0.1:...). Never call the real service.
- What the tool asks and what the stand-in answers: as the existing stand-in implements (GET /v8/finance/chart/<symbol>?interval=5m&range=1d; made-up quotes with trading periods relative to now). A refusal is status 429; an unknown ticker is 404 with a JSON error; 503 or a closed port is "unreachable". If a check needs an answer the stand-in cannot give (a price of 1038.42, an earlier day's quote, a closed market), add a control to verification/standin/main.go without changing what existing controls do.
- Real-time numbers: the smallest refresh interval is 10 seconds; the default is 60. A forced fetch is skipped for a row attempted less than 5 seconds ago. After a refusal the tool waits 5 minutes before one probe.

## Build

Put the new checks in a NEW file, verification/surface_verify_test.go, with the same `verification` build tag, each named after its criterion (TestVerifyS1_8_..., TestVerifyQR12_...) with the criterion's text in a comment. Do NOT edit the two existing check files; add helpers you need to your file. Evidence: as the existing checks do, under verification/evidence/<run>/<check>.txt (already ignored).

## What to verify

The 63 criteria of increment 2 in scope.md, each through `brl-stocks print` in its `print` reading. Several may share one run of the tool where they observe the same output, but each criterion gets its own named assertion and evidence line. Also:

- QR1: every criterion in section 8 holds with 20 tickers and 3 summary entries. Run a representative set of your checks (at least the row formats, groups, failed fetches and not-found rows) with a 20-ticker config as well, and say which.
- QR12: the tool contacts only its data source and sends it nothing about the user beyond the symbols requested. Record everything the stand-in receives during a run (path, query, headers) and assert that nothing identifies the user (no user name, home path, config contents, or anything not needed to ask for the symbol). To catch contact with anything other than the source, give the child process an HTTP_PROXY and HTTPS_PROXY pointing at a second local listener that records and refuses every connection, and assert it receives none. Say in the feature map what this does not prove (traffic that ignores proxy settings, name lookups).
- QR10 is measured by S17.1, which needs the real source: do NOT run it. Put it in the feature map with the hand-check commands from the existing README.

Not in scope: S5.9 and S5.11 (they wait on an open question), the full-screen view, and the long checks.

Keep the ordinary run under about 3 minutes in total (the existing checks take about one minute). Use the smallest interval and short waits where a criterion needs time to pass, and say in the feature map where you scaled a criterion's times down.

## Run and report

Run the ordinary checks once (to confirm nothing broke and to run yours). Then update verification/README.md: add a section of feature-map rows for every criterion of increment 2, QR1, QR10 and QR12, each with its check or, where nothing can verify it from outside, the reason, what would make it visible, and the exact commands a person could run to check it by hand. Change no other rows. Change nothing outside verification/. Do not run git commands that change the repository. Another long check (QR8) is running from a separate build in the background; do not stop it or touch its evidence folder.

Return a summary of at most 350 words: how many checks, how many pass and fail; every FAILING check with the criterion, what the spec led you to expect and what you observed (quote the output or the record); what QR1 and QR12 showed; what you could not verify and why; and anything in the spec, or in a `print` reading, that was ambiguous for a verifier.
```
