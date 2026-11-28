# Source measurements, 2026-11-26 to 27 (Q62)

What the default source's chart endpoint reported across a US holiday (Thanksgiving, 26 Nov), a B3 close, the overnight change of session, the B3 and US opens and a US early close (Black Friday, 27 Nov). It's the research for Q62 and backs F43–F47 in `research/grilling-log.md`.

- `poll.py`: the logger. It uses only the Python standard library and was run as `python3 -I poll.py <out> 2026-11-27T19:30:00`. It requested `/v8/finance/chart/{symbol}?interval=5m&range=1d` for AAPL, MSFT, ^GSPC, PETR4.SA, VALE3.SA, ^BVSP and BRL=X, with a generic User-Agent. It polled every 2 minutes during 12:30–13:30, 14:00–15:00, 17:45–19:00 and 19:30–22:00 UTC, and every 10 minutes otherwise.
- `poll.jsonl.gz`: one line per symbol per poll, 2,534 lines, no errors. Each line has the poll time, price, quote time, previous close, day high and low, the regular, pre and post periods, the number of bars and the last three bars.
- `raw.tar.gz`: the source's full answer for each symbol, saved every 30 minutes.
- `*.1328.json`: one snapshot of all symbols, taken at 13:28 UTC on 26 Nov.

To read the log: `zcat poll.jsonl.gz | python3 -I -c 'import sys,json; [print(json.loads(l)) for l in sys.stdin]'`.
