# Hand checks: the sparkline (slice 4)

What only a person can judge, from increment 1 (PR #19). Run with your own config.

## 1. The blocks in your terminal and font (#19)

```
brl-stocks
```

On a wide terminal, during or after a trading day. Look for:
- every row with a quote ends with a line of blocks under `TODAY`, two spaces after the quote time;
- green when the day change is up, red when down, no colour at `+0,00`;
- each block one cell wide: the lines end at the same column, and nothing after them is shifted;
- not-found rows have nothing under `TODAY`.

Then narrow the window slowly. The `TODAY` column and its header go before any other column, and come back when you widen it.

## 2. The real source's series (#19, as S17.1)

During the B3 session (10:00–17:55 local), run `brl-stocks`. The B3 rows' lines should show the day so far: a few minutes after the open, short lines, then 10 characters, with the right end following the last price. After the close, the line stays as the day's.
