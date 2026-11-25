# Build report: live refresh (slice 3), increment 4

The builder's summary, unedited:

> The view now picks up config changes while it is open: all 10 increment-4 tests pass, and nothing else fails. Committed as a1687e7 on slice3/4-build in /home/lucattelli/Programming/prototyping/brl-stocks-wt-build (changes in `internal/app/view.go` and `internal/tui/tui.go`).
>
> **How it works:** at each heartbeat the view stats the config file, following any symbolic link. Only when the timestamp, size or existence changes does it read and parse the file again, and that happens outside the update step.
> - **Valid file:** it replaces the watchlist. Removed rows are also dropped from memory, which S15.6 needs. Added rows and not-found rows are fetched at once.
> - **Invalid or deleted file:** the last watchlist stays and the error line says why.
> - **File appearing where the example was shown:** it replaces the example and the example note goes.
>
> **Choices the spec left me:**
> - **Wording:** `invalid config <path>: <parser message>; showing the last valid watchlist`, `config <path> was deleted: showing the last watchlist`, and `cannot read the config <path>: <error>`. The path uses `~` for the home folder, as the status line does. Parser messages are flattened to one line.
> - **Both a config problem and a fetch problem:** they share the one error line, config first, joined by ` · `.
> - **A deleted file that comes back:** it is loaded like any change and the error line clears.
>
> **Path length:** the path comes right after a 15-character prefix, so it shows whole unless it is longer than about 145 columns on a 160-column line. A longer one is cut at the screen edge.
>
> **Checks:** with -race the app suite took 44.9 s on both runs, 13.6 s without. vet and gofmt are clean, and the ordinary verification passes in 429 s. A run of the built binary against the stand-in, with the config reached through a linked folder, showed an added row, then the invalid-file error line, then the deleted-file error line.

Follow-up (b0a68ea), unedited:

> Fixed: when there is a config problem, the error line now shows only that, as spec 7.3 orders. The fetch problem (block or failed fetches) shows again once the config problem is gone. The change is in `internal/tui/tui.go`, committed as b0a68ea on slice3/4-build in /home/lucattelli/Programming/prototyping/brl-stocks-wt-build. gofmt and vet are clean, and `go test ./... -race -count=1` passes in 45.0 s.

## Coordinator's check and sorting

- No test file changed by the build (`internal/app/view.go`, `internal/tui/tui.go` only). gofmt and vet clean; `go test ./... -race -count=1` passes twice, 45 s each.
- **Both problems joined on the error line:** a fault against spec 7.3 ("it shows the first of: config problem, block, failed fetches", D50). Fixed in b0a68ea. No criterion tests the order; the verifier checks it on the real build.
- **Wording of the config error lines:** the spec fixes none (6.4 says the line names the file and the problem, or says it is gone). Q97, built on the builder's wording as the recommendation.
- **A deleted file that comes back:** spec 6.4, "the file changes and is valid". No question.
- **A path longer than about 145 columns is cut at the screen's edge:** as every line of the view is. Noted.
