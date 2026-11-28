# Hand checks: close, open and holidays (Q62)

Each check runs against the real source. The quote times on screen are local, São Paulo.

## 1. B3's closing price arrives within the settle time (D111)

On a B3 trading day, open the view before 17:00 São Paulo time and leave it open until 18:45.

```sh
brl-stocks
```

What to look for:
- From 17:00 the status line reads `B3 closed` while B3 prices still change (D111 accepts this from November to March).
- Between about 18:05 and 18:35 the B3 rows take one last price, the closing call's result. PETR4 went from 43.74 to 43.19 in that print on 26 Nov (F46).
- After 18:40 the B3 rows stop changing.

## 2. A holiday costs at most two requests and is shown closed (D112)

The next holidays: B3 on 24 Dec (no session) and NYSE on 25 Dec. Run the view through the usual open of that market: 10:00 for B3, 11:30 for the US in São Paulo time.

What to look for:
- At no point does the status line show that market as open.
- The rows of that market keep their last close, with no stale mark.

## 3. The rows come back the next weekday (S5.17)

Leave the view of check 2 open overnight, until the market's next usual open.

What to look for: within a minute of the open, that market's rows are refreshed and the status line shows it as open.
