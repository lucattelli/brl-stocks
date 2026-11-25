# Verifier's report: the full-screen view, increment 1

Returned 2026-11-25 for `verify-brief.md`. The agent's summary, unedited:

---

I ran the real view on a pseudo-terminal, and all 35 criteria of increment 1 pass, along with QR1, QR2, QR7 and QR12. Nothing failed.

**Checks:** the ordinary run of all files covers 123 top-level checks: 117 pass, 0 fail, and 6 SLOW/LONG ones are skipped by design. It takes about 1 min 45 s. 21 of those checks are new and cover the view, with one subtest per criterion. The earlier one-shot check S6.9 also passes now; the README had it as failing.

**QR2:** all 20 cached rows were on the emulated screen after a median of 28 ms over 10 runs (range 27–30 ms; the limit is 300 ms). The source held its answers during the runs, so every price shown came from the cache. This measures until the output reaches the emulator, not until a real terminal paints it.

**S1.3, leaving the terminal as found:** after both `q` and Ctrl+C:
- the exit status was 0;
- the tool had used the alternate screen and left it;
- the cursor was shown again;
- the terminal mode was identical to before the run;
- no other terminal mode was left switched on.

To prove these checks can fail, a throwaway run killed the view instead of quitting. All three terminal checks caught it, and I removed it afterwards.

**Not verifiable from outside**, each with a hand check now in `verification/README.md`:
- How the view looks on a real terminal. "Palette green/red" is verified as palette index 2/1; the actual colour depends on the user's terminal.
- Terminals other than the emulator.
- A real network outage; here "no network" means the stand-in was stopped.
- The real data source (S17.1/QR10) through the view.

**Ambiguous in the spec, for a verifier:**
- "Full width" has no number of columns; I used 120.
- "Treated as B3/US" (S3.1–S3.5) only shows on the view as the currency of the price.
- "As it was found" doesn't say which terminal state counts. I checked the screen, the cursor and the terminal mode, and recorded any other modes.
- "Palette's green" could include bright green; I accepted both.
- For QR2, "rows on screen" I read as all 20 rows showing a price.

**Emulator library:** it was enough. It gave cell text and colours, whether it was on the alternate screen, cursor visibility, and mode changes. It also answers the tool's terminal queries; I pass those answers back to the tool.

**Changes:** `go mod tidy` added the emulator library and `github.com/charmbracelet/x/ansi` as direct requirements in `go.mod`/`go.sum`. Under `verification/`, I added `view_verify_test.go`, `pty_linux_test.go` and a new section in `README.md`; the existing check files and the stand-in are unchanged.

---

## Coordinator's check and sorting

Rerun by the coordinator: all ordinary checks pass (102.8 s). One real screen read from the evidence (`TestVerifyView_S1_2_S1_8_15_S16_1_2_RowFormatsAndColour__20_tickers_QR1`): summary line, header, 20 rows with R$ and US$ formats and aligned columns, on the alternate screen.

- No failing check.
- The ambiguities: "full width" (any width where nothing is dropped; 120 is one), "treated as B3/US" (the currency is how the spec shows it, 7.3), "as it was found" (screen, cursor and terminal mode are what a person sees), bright green accepted (looser than the design, which emits 32 only), QR2's "rows on screen" (all 20 with a price is the strict reading). None needs a decision. No question.
- Not verified, with routes: the look on a real terminal and other terminals, by hand (commands in `verification/README.md`); a real outage and the real source, by hand (the QR10 check run as `brl-stocks`).
- `github.com/charmbracelet/x/ansi` is a direct requirement because the checks strip codes with it; it already came with Bubble Tea. Nothing new beyond D86's library.
