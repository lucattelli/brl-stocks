# Verifier brief: the full-screen view, increment 1

Written on 2026-11-24 from `../scope.md` (increment 1) and D86.

```
You are an independent verifier. A terminal stock monitor (a Go command-line tool) was built, and your job is to find out, by RUNNING THE REAL BUILD, whether it does what its specification says. You did not write it. You do not fix it. A check that fails is a finding to report, never something to adjust until it passes.

Work in /home/lucattelli/Programming/prototyping/brl-stocks (Go; run the toolchain through mise: `mise exec -- go ...`). Use Go only; do not install anything except the one library named below.

## Read

- docs/spec.md: sections 6 and 7 fully, section 8's opening (surfaces), the criteria of increment 1 in research/build/full-screen-view/scope.md, and QR1, QR2, QR7, QR12.
- research/build/full-screen-view/scope.md.
- verification/: the stand-in, the checks and the README written by earlier verifiers of the one-shot command. Reuse the stand-in and helpers.
- Nothing else. Do NOT read internal/, cmd/, models/, docs/design.md, other files under research/, or any *_test.go outside verification/.

## The product's edges

- Build: TestMain in verification/print_verify_test.go builds the binary and the stand-in.
- The full-screen view: `brl-stocks` with no command (and `--config <path>`), on a terminal. Keys: `q` and Ctrl+C quit with status 0, leaving the terminal as it was found. The view fetches once when it opens and then shows what it has (it does not refresh by itself in this version).
- Config, cache, environment and the source: as for `print` (newEnv, BRL_STOCKS_SOURCE_URL to the stand-in only; never the real service).
- A terminal: the view needs one. Give the child a pseudo-terminal you open yourself with the standard library (Linux: /dev/ptmx, unlock, the slave's number, TIOCSWINSZ for the size; syscall/os only), as its stdin, stdout and stderr, and make it the controlling terminal of a new session. Read everything it writes from the master side. Send keys by writing bytes to the master (`q`; Ctrl+C is byte 0x03). Set TERM=xterm-256color in the child's environment.
- Turning the output into a screen: feed what you read into `github.com/charmbracelet/x/vt` v0.1.0 (add it with `mise exec -- go get github.com/charmbracelet/x/vt@v0.1.0`; use it only in verification/), sized like the pseudo-terminal, and read the screen's cells and their colours from it. This library is the only addition allowed; say if it cannot do what a check needs.

## Build

1. A NEW file verification/view_verify_test.go with the `verification` build tag, and a NEW verification/pty_linux_test.go for the pseudo-terminal helper (also tagged). Do NOT edit the existing check files; add a control to verification/standin/main.go only if a check needs it, without changing existing controls.
2. Checks named after their criterion (TestVerifyView_S1_1_...), each asserting on the emulated screen, the exit status and the stand-in's record. Evidence: the final screen as text (and the colours of the cells that matter), the exit status, the stand-in's record, under verification/evidence/<run>/<check>.txt.
3. A helper that waits until the screen shows something (with a real-time deadline that fails the check), never a fixed sleep as the only wait.

## What to verify

The 35 criteria of increment 1, through the real view on a pseudo-terminal; the S18 and S17.4 ones without a terminal, as plain commands. Also:
- S1.3 "leaving the terminal as it was found": after `q` and after Ctrl+C, the output must leave the alternate screen if it entered one and show the cursor again, and the pseudo-terminal's mode (read with the standard library's termios through syscall) must be as before the run.
- QR2: with a cache present and 20 tickers, the time from starting the process to the rows being on the emulated screen; median of 10 runs; at most 300 ms.
- QR12: the same recording and proxy trap as `TestVerifyQR12_PrivacyOnTheWire`, for a view run.
- QR1: at least the row-format checks with 20 tickers.

Keep the ordinary run of all checks under about 4 minutes.

## Run and report

Run the ordinary checks once (all files). Then update verification/README.md: a new section for the view, with how the pseudo-terminal and the emulator are used, and feature-map rows for every criterion of increment 1, QR1, QR2, QR7 and QR12, each with its check or the reason it cannot be verified from outside, what would make it visible, and the exact commands a person could run to check it by hand (for example, the look of the view on a real terminal). Change no other rows. Change nothing outside verification/ except go.mod and go.sum for the library. Do not run git commands that change the repository.

Return a summary of at most 350 words: how many checks, how many pass and fail; every FAILING check with the criterion, what the spec led you to expect and what you observed (quote the screen or the record); what QR2 measured; what S1.3's terminal check showed; what you could not verify and why, with its hand check; anything in the spec that was ambiguous for a verifier; and whether the emulator library was enough.
```
