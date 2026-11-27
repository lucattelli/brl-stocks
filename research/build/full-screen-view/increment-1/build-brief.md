# Builder brief: the full-screen view, increment 1

Written on 2026-11-24. No skill covers this step yet (see `../notes.md`); this brief is its first draft.

```
You implement one increment of a terminal stock monitor, against acceptance tests written beforehand by someone else from the spec. Work in /home/lucattelli/Programming/prototyping/brl-stocks (Go 1.27 through mise: `mise exec -- go ...`).

## Read

- docs/spec.md: sections 6 and 7 fully, section 8's opening, and the criteria of increment 1 in research/build/full-screen-view/scope.md.
- docs/design.md in full: the structure (section 3), the decisions (section 4, especially "No step layer (D57) and Bubble Tea 2 (D58)" and "The view's test boundary"), and the dependencies (section 6).
- research/build/full-screen-view/scope.md.
- The code: internal/ and cmd/. The one-shot command (`print`) is built and verified; reuse its engine, render, config and cache packages rather than duplicating them. Formatting rules shared by both surfaces belong in render.
- The tests: internal/app/view_acceptance_test.go (increment 1, written from the spec, failing now), and the other *_test.go files in internal/app, which must keep passing.

## Do

1. Implement the view so that the increment-1 tests pass: a `tui` package with the Bubble Tea 2 model (`charm.land/bubbletea/v2`, v2.1.2), and in `app` the driver of "The view's test boundary" for a non-nil `Deps.Terminal`, and the real program on the process's terminal when it is nil. In this increment the view fetches once when it opens (one engine cycle, after showing the cache), then shows what it has; no heartbeat, no `r`, no config watching (slice 3).
2. Follow the design. If the design and a test disagree, or a test seems to need something the spec does not say, STOP on that point: do not change the test, do not guess; report it.
3. Add only the dependencies the design names. Lip Gloss is optional: use it only if it helps, and if you do, pin the version and say why. Run `go mod tidy`.
4. Do not edit any *_test.go file. Do not change the spec, the design, the log or anything under research/ or verification/.

## Check

- `mise exec -- go vet ./...` passes; `mise exec -- go test ./... -race -count=1` passes, except tests you report as blocked by point 2.
- `mise exec -- go build ./cmd/brl-stocks`, then run it by hand on a real terminal is not possible here; instead run the built binary with `script -qc "<binary> --config <a temp config>" /dev/null` against a local stand-in only if you write one in a temp folder; never call the real data source. If you cannot, say so.
- Do not run git commands that change the repository's history.

## Return

At most 300 words: what you built (packages, files), the dependencies added with versions, the test counts (pass, fail, skipped), every test you could not make pass and why, every place where the design or the spec left you a choice and what you chose, and anything in the design that turned out wrong.
```
