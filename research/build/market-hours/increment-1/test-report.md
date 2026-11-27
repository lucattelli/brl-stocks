# Test report: close, open and holidays (Q62), increment 1

The test writer's summary, unedited (2026-11-27):

> 14 tests, committed as e59789f on q62/1-tests. The new file is internal/app/markethours_increment1_acceptance_test.go (11 tests), and S5.3, S5.8 and S13.5 were reworded in place. Against the current build 5 fail, 9 pass and none are skipped. vet passes.
>
> **Fail:** S5.9 view and print (the holiday is shown as "US open"), S5.15 (6 holiday requests, want at most 2), S5.5_Holiday, and the reworded S5.8 (no B3 request 95 min after the end).
>
> **Pass, each shown failing on a changed copy:**
> - S5.3 and S13.5 (US): the build already stops US requests 5 min after the end. At 1 min after, both fail with US rows requested.
> - S5.13: a 30-min settle is shorter than 100 min. Opened 10 min after the end, it fails with 10 B3 requests.
> - S5.14: the build already shows B3 closed after the end. One minute before the end, it fails on "B3 open".
> - S5.11 view and print: the stand-in's last price is Friday's close. With a different price, the price check fails.
> - S5.16: the build already shows open on the second check. With the session starting at 15:00 instead, it fails on "US closed".
> - S5.17 and S5.7_Holiday: stopping 1 s before Friday's open, no request is made.
>
> **Points the spec did not tell me:**
> - **S5.11:** the spec does not say what a pre-market price looks like at the boundary. I read it as the source answering with Monday's session not yet begun and Friday's close as the price.
> - **Overnight move:** I put the trading period's overnight move (F43) at 05:00 UTC.
> - **S5.16's late session:** I made the late session start at 14:35.
>
> **Cost:** the package now takes about 150 s with -race. S5.17 alone is 40 s, because it runs 24 h of view time.

## Coordinator's check and sorting

- **My rerun:** vet passes. The S5 and S13.5 tests give the same 5 failures, in 22 s without the race detector. Only the three tests the brief allowed were changed in existing files.
- **Shown failures:** the 9 passing tests each have one, on a changed copy, as listed above. S5.13 is the one that shows the new number: it fails against the old 30 minutes. S5.3 and S13.5 pass at once because the US settle time stays 5 minutes (D113).
- **The points the spec left open:** none needs a question.
  - S5.11: the answer before the open is what F44 measured.
  - 05:00 UTC: F43 measured the switch for the US at 04:50–05:02.
  - 14:35: any start after the second check would do.
- **Cost:** S5.17 runs 24 hours of view time in 40 s. That is noted for the builder: drawing is the cost, as in F41.
