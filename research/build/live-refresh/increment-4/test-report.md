# Test report: live refresh (slice 3), increment 4

The test writer's summary, unedited:

> I wrote 10 tests for increment 4 and committed them as 6795a53 on slice3/4-tests, in internal/app/refresh_increment4_acceptance_test.go. All 10 fail on the increment-3 build, which does not watch the config file yet. None passes, panics or hangs, so no shown failures were needed. vet passes and every earlier test still passes.
>
> The tests cover S8.4, S11.3, S11.4, S11.6, S11.10, S11.11, S11.14, S11.19, S11.20 and `TestS15_6_ViewOpen_...`. S11.3 also runs with 20 tickers (19, then a 20th added).
>
> **How they work:**
> - The test writes or deletes the file while the view runs. It lets 2 s pass before each save, then gives the file the clock's time as its modification time.
> - Each criterion is checked on the frame after 5 s of view time.
> - S11.6 writes to the file in the link's target and checks that the link still points to the same place.
> - S11.20 starts with no file at the default path and creates the folder and file there.
>
> **QR7 check changed:** `openOnClock`'s own config check would flag the test's edits, so these tests bypass it. Instead, at the end the file must hold what the test last wrote, or be absent after a delete.
>
> **Added beyond a criterion's text:**
> - S11.10 and S11.14 first require an error line that names the file (from 6.4). Without it, a view that never noticed the change would pass.
> - S11.19 requires the line to name the file and contain a word such as gone, missing, deleted, removed, not found, no such file or does not exist. The spec fixes no wording.
> - S11.11 requires the line to name the full path and say more than the path.
> - S11.14 also requires the example watchlist not to replace the rows.
>
> **Risk:** the error line must show the full temporary path (about 90 characters) within the 160-column frame. A build that shortens or cuts the path would fail S11.10, S11.11, S11.14 and S11.19.

Follow-up (acbe2e9), unedited:

> `errorLineNaming` now accepts the full path, or the same path with HOME written as `~` when the config is under HOME. S11.11's "more than the path" check removes whichever form appears. Nothing else changed. go vet passes, and all 10 increment-4 tests still fail on the increment-3 build. In these tests the config is not under HOME, so for now only the full-path form can match.

## Coordinator's check

- One new file; gofmt and vet clean; all 10 fail on the increment-3 build (rerun by me), none passes at once.
- Sorting: the error line required for S11.10 and S11.14 comes from 6.4's rows (kept). S11.19's list of words only guards against a vacuous pass (kept). Requiring the full path was beyond "names the file": after my request the tests also accept the `~` form of D95. The cut-path risk is left to the builder: it reports if the error line cannot show the path whole.
