# Verify report: live refresh (slice 3), increment 4

The verifier's summary, unedited:

> Increment 4 has no failing check: all 9 criteria pass, and so does the extra 7.3 error-line-order check. One ordinary run of all files took 6 min 19 s: 187 top-level checks pass, 0 fail, 18 skipped (SLOW and LONG). This increment's file runs in about 25 s. Committed as 19bb6d5 on slice3/4-verify; the checks are in `verification/refresh_increment4_verify_test.go` and the README has a new section. Nothing is scaled. The config is edited while the real view runs at 200 columns, and each change is timed from the write to the screen.
>
> **Criteria (all well under 5 s):**
> - **S11.3:** a row for MSFT appeared, with a price, 22 ms after the save.
> - **S8.4:** the not-found XXXX9 was requested again 10 ms after the change.
> - **S11.4:** the removed row went in 28 ms; the remaining rows kept their quotes with no new request.
> - **S11.6:** a ticker added to the file behind `~/.config/brl-stocks` (a link) showed in 21 ms; the link was unchanged.
> - **S11.10 and S11.11:** after a syntax error, the watchlist stayed. In 22 ms one line appeared: `invalid config <path>: toml: line 4 …, but got '['; showing the last valid watchlist`.
> - **S11.14 and S11.19:** after a delete, the rows stayed and the line read `config <path> was deleted: showing the last watchlist`.
> - **S11.20:** the example watchlist was replaced 21 ms after a config appeared at the default path.
> - **7.3, config problem first:** with every fetch failing (503), a broken config replaced the `source unreachable` line with the config line alone in 21 ms. Once fixed, the fetch line came back in 20 ms.
>
> **Outside the criteria:**
> - A save by rename over the config was followed (23 ms).
> - Two saves 300 ms apart ended on the second.
> - A deleted file written back recovered in 21 ms.
> - Growing the config from 2 to 20 tickers gave all 20 rows quotes in 27 ms.
> - **An emptied file (0 bytes) makes every row vanish with nothing on screen saying why:** it is treated as a valid config with no tickers. An editor that truncates before writing could briefly blank the view.
> - **The config error line is cut at a word with no ellipsis:** at 200 columns, `showing the last valid watchlist` was lost.
> - A reorder, a second rename and the second of two quick saves showed after about 1 s rather than about 20 ms. That is still within 5 s.
>
> **Not verified:** saves by real editors on a real terminal. Hand check: edit the config in your editor while `brl-stocks` is open. Add a ticker, then break a bracket, then fix it, `rm` the file and write it back, watching the screen after each step.

## Coordinator's sorting

- **No fault.**
- **An emptied file empties the view:** the spec does not say whether a config with no tickers is valid (7.2 lists what makes a config invalid; an empty file is not among them). Q98, built on the recommendation (as built).
- **The invalid-config line cut at its end:** part of Q97's wording. Built on the recommendation: the builder reordered it to `invalid config <path>, last valid watchlist kept: <parser message>` (02304ba), so a cut can only take the parser's detail. The checks do not depend on the wording.
- **Changes seen after about 1 s:** the heartbeat (design C1); within S11.3's 5 seconds.
- **Not verified, routed:** saves by real editors on a real terminal, a hand check (above), gathered for the slice's closing PR.
