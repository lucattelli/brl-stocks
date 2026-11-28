# brl-stocks (stock monitor TUI) — Specification

Status: approved, 2026-11-24 (D80), as updated for D79; updated afterwards for D81 (7.3, S6.11) and for D92–D96 (7.1, 7.3, S2.9, S2.10, S9.6, S9.7, S9.11, S12.6–S12.8, S15.10, S18.5, S18.6), and S7.10 and S11.20 (transitions of 6.3 and 6.4 that had no criterion, found by slice 3's scope check), and for D98 (6.3), and for D99–D102 (6.3, 7.2, 7.3, QR11), and S10.4–S10.10 (the sparkline as 7.3 already states it, with no criterion, found by slice 4's scope), and for D104–D107 (7.3, S10.11–S10.22), and the approval stands. Earlier approvals: D52, D67, D72, D78. Open proposals: 0.
Source: `research/grilling-log.md` (decisions D1–D107, facts F1–F40).

Each requirement cites the decision (D#) or fact (F#) it comes from. Anything the log does not decide is marked as a proposal (P#) and listed in section 11.

This spec says what the product does, not how it is built. It names no language, library or internal structure; those belong to the design, and section 10 hands them over.

## 1. Summary

`brl-stocks` is a terminal tool that monitors a watchlist: the current price and day change of up to 20 followed tickers from B3 and the US exchanges (D2, D3, D8, D26). It is a personal tool, kept clean enough to publish (D1), that uses free data with a delay of about 15 minutes (D4, D5). It runs as a full-screen view that refreshes itself, kept open or launched on demand, and as a one-shot command that prints and exits (D6).

## 2. Goals and non-goals

### Goals

- Show the current price and day change of the followed tickers (D3).
- Cover B3 and US tickers in one list, each price in its own currency (D2, D10).
- Three ways to run: always open in a pane with automatic refresh, launched on demand, and one-shot print for scripts or a status bar (D6).
- Work with free data and no setup beyond the watchlist: no key, no account (D5, D7).
- Keep the delay visible: every row carries the time of its quote (D9).
- Fail in the open: old prices are marked stale and the cause is shown (D19, D40).
- A watchlist file written by hand, independent of the data source (D12, D17).
- The data source can be replaced by another (D7).
- Clean enough to publish: no secrets and no personal watchlist in the repository (D1).

### Non-goals

- Portfolio: positions, amounts, cost, returns (D3).
- Analysis, fundamentals, news (D3, D13).
- Alerts (D13).
- A detail view with a chart (D13).
- Editing the watchlist inside the app (D12).
- Currency conversion (D10).
- Real-time B3 quotes, or any paid data (D4, D5).
- A stored price history (D20).
- A custom output template for the one-shot mode (D18).
- Keys beyond those listed in section 7.3 (D32).
- Writing, creating or repairing the config file (D35).
- More than 20 tickers as a design target (D8).
- Forking or adopting an existing tool (D21).
- Pre-market and after-hours prices (D50).
- A second data source in the first version (D50).
- Windows. The tool supports Linux and macOS (D59).
- Two different watchlists in use at the same time. `--config` selects which watchlist a run uses; running instances on different watchlists at once is not supported, because they share the kept quotes (D61, D68).

## 3. Glossary

Terms from the log's glossary come first, unchanged. The others are terms the decisions use, defined here with the decision that fixes their meaning.

| Term | Meaning |
|---|---|
| *TUI* | Text user interface: a full-screen app in the terminal |
| *B3* | Brasil, Bolsa, Balcão: the Brazilian stock exchange |
| *the-2026-dev-env* | The user's public repo of machine setup and config files, linked into `~/.config` by its `devenv-setup` script |
| *Watchlist* | A list of tickers the user follows, without holding data |
| *Ticker* | The short code of an instrument as the user writes it: `PETR4`, `AAPL` (D17) |
| *Market* | Where a ticker trades: B3 or US (D2, D30). Each market is open or closed on its own (D15) |
| *Quote* | The data for one ticker at one moment: last price, day change, day low and high, time of the quote (D9) |
| *Quote time* | The time the source gives for the quote, not the time it was fetched (D9) |
| *Row* | One line of the watchlist on screen or in the one-shot output (D9) |
| *Group* | An optional named section of the watchlist, defined in the config file (D16) |
| *Summary line* | The fixed line above the watchlist with Ibovespa, S&P 500 and USD/BRL (D11) |
| *Sparkline* | A one-line miniature chart of a ticker's prices during the day (D13) |
| *Refresh* | Fetching new quotes from the source (D14) |
| *Refresh interval* | The time between automatic refreshes while a market is open; 60 seconds by default (D14) |
| *Full-screen view* | The TUI, whether kept open in a pane or launched on demand (D6) |
| *One-shot* | The run mode that prints once and exits (D6) |
| *Cache* | The last quotes, kept between runs. It holds no history (D20) |
| *Source* | The service the quotes come from (D7) |
| *Stale* | A row whose last fetch failed, or whose market is open and whose last successful fetch is older than two refresh intervals (D40) |
| *Block* | The source is rate-limiting or refusing the tool (D38) |
| *Not found* | The source does not know the ticker (D39) |

## 4. Domain model

### Watchlist

| Field | Meaning | Source |
|---|---|---|
| tickers | Ordered list of tickers; the order of the file is the default order on screen | D12, D31 |
| groups | Optional ordered list of groups | D16 |
| summary entries | The entries of the summary line; by default Ibovespa, S&P 500, USD/BRL | D11 |
| refresh interval | Seconds; 60 by default | D14 |

Invariants:

- The watchlist holds no positions, amounts or other holding data (D3, D33).
- The watchlist comes only from the config file, or from the built-in example when there is no file. The tool never changes it (D12, D35).
- It contains no notation that belongs to a data source (D17).

### Group

| Field | Meaning | Source |
|---|---|---|
| name | Shown as a section header | D16 |
| tickers | Ordered list of tickers | D16 |

### Ticker

| Field | Meaning | Source |
|---|---|---|
| code | As written by the user, plain: `PETR4`, `AAPL` | D17 |
| market | B3 or US | D2, D30 |

Invariants:

- The market is inferred from the code: four letters followed by one or two digits, with an optional trailing `F`, is B3; anything else is US (D30).
- A `B3:` or `US:` prefix overrides the inference (D30).

### Market

| Field | Meaning | Source |
|---|---|---|
| name | B3 or US | D2 |
| state | Open or closed | D15 |
| currency | R$ for B3, US$ for US | D10 |

Invariants:

- The two markets open and close independently (D15).
- Rows of a closed market are not refreshed automatically (D15). The short period after the close in 6.1 is an exception (D50).

### Quote

| Field | Meaning | Source |
|---|---|---|
| name | The instrument's name | D9 |
| last price | In the market's currency | D9, D10 |
| day change | As a value and as a percent | D9 |
| day low, day high | In the market's currency | D9 |
| quote time | As given by the source | D9 |
| intraday series | The day's prices, for the sparkline | D13 |
| last successful fetch | When the tool last got this quote | D40 |
| last fetch result | Succeeded, failed, or not found | D39, D40 |

Invariants:

- At most one quote is kept per ticker: the last one (D20).
- A quote is never converted to another currency (D10).

### Summary entry

| Field | Meaning | Source |
|---|---|---|
| what | Ibovespa, S&P 500 or USD/BRL | D11 |
| value, day change | The index level or the exchange rate, and its change | D11 |
| hours | Ibovespa follows B3, S&P 500 follows US, USD/BRL has hours of its own | D50 |

## 5. Constraints

External facts that shape behaviour and that nobody chose.

- No free real-time source for B3 was found; about 15 minutes of delay is the free ceiling. Measured lag of B3 quotes on the default source: 902 to 1053 seconds (F1, F2, F15).
- US quotes from the default source lag about 1 second, and USD/BRL about 10 seconds (F2, F15). So the two markets differ in freshness, and only the quote time tells the user which is which.
- The default source, Yahoo's unofficial chart endpoint, is the only free, keyless source found that covers both markets (F2). It is unofficial: it can break or refuse requests without notice, and has done so in waves (F3, F11).
- The default source takes one request per ticker (F3). A refresh of 20 tickers and 3 summary entries is 23 requests.
- One answer from the default source carries everything a row needs, including the intraday series, the day's trading period and the exchange's time zone (F13, F14). Whether the trading period is right on a market holiday is unverified (F14).
- The default source labels FIIs, ETFs and BDRs all as plain equity (F3).
- Sources write B3 tickers in their own ways (a suffix, a prefix, or plain) (F22). The user writes none of them (D17).
- The config folder reaches `~/.config` as a link to a whole folder of a public repository (F16, F17). Anything the tool wrote there would be published.

## 6. Workflows

### 6.1 Market session (one per market; USD/BRL has a session of its own, D50)

| State | Event | Next state | What happens | Source |
|---|---|---|---|---|
| Unknown | The first quote of a ticker of this market arrives | Open or Closed | The market is shown as open or closed from then on | D15 |
| Open | A refresh interval passes | Open | The market's rows are refreshed | D14 |
| Open | The session ends | Closing | The market is shown as closed; its rows are still refreshed | D50 |
| Closing | The closing quote has arrived for every row, or 30 minutes have passed since the close | Closed | Automatic refresh of the market's rows stops; rows show the last close | D15, D50 |
| Closed | The next session starts | Open | Automatic refresh resumes without user action | D14, D15 |
| Closed | A weekday comes on which the market does not trade (holiday) | Closed | The market stays closed and is not refreshed | D15, F14 [unverified] |

### 6.2 Row

| State | Event | Next state | What happens | Source |
|---|---|---|---|---|
| No data | A fetch succeeds | Fresh | The row shows the quote | D9 |
| No data | A fetch fails | No data | The row keeps its placeholders; the error line appears | D19, D50 |
| Any | The source does not know the ticker | Not found | "not found" in place of the price; other rows are unaffected | D39 |
| Fresh | A fetch fails | Stale | The last known prices stay, marked stale with their age | D19, D40 |
| Fresh | The market is open and the last successful fetch becomes older than two refresh intervals | Stale | Marked stale with its age | D40 |
| Fresh | The market closes | Fresh | The row shows the last close; while the market is closed, age alone never makes it stale | D15, D40 |
| Stale, last fetch failed | The market closes | Stale | The row stays stale until a fetch succeeds | D40 |
| Stale | A fetch succeeds | Fresh | The stale mark goes | D40 |
| Stale | A fetch fails | Stale | The wait before the next automatic attempt grows | D19, D50 |
| Not found | An hour passes, or the config changes | Not found or Fresh | The ticker is tried again | D39 |
| Any | The ticker is removed from the config file | (gone) | The row disappears | D12 |

At start, a row whose quote is in the cache begins as Fresh or Stale by the same rule (D20, D40); a row with no cached quote begins as No data.

The wait after failures (D50): after a row's n-th failed fetch in a row, its next automatic attempt comes after one refresh interval × 2^(n−1), at most 30 minutes. The first wait is therefore one interval: 60 seconds by default, then 120, 240 and so on.

### 6.3 Source

| State | Event | Next state | What happens | Source |
|---|---|---|---|---|
| Normal | The source refuses a request (rate limit or access denied) | Blocked | No more fetches; rows keep their prices; the rows whose fetch was refused are stale; the error line says so; a first wait of 5 minutes starts | D19, D38, D50 |
| Normal | A fetch fails for any other reason | Normal | Only the rows that failed are affected (6.2) | D19, D50 |
| Normal | `r` is pressed | Normal | A refresh of every row starts within 2 seconds, including rows waiting after a network error and rows of closed markets; not-found rows keep their hourly wait | D32, D38, D39, D50, D62 |
| Normal | `r` is pressed while a refresh is running | Normal | The press is kept: a forced refresh starts when the running one ends, skipping rows attempted less than 5 seconds ago | D50, D99 |
| Blocked | `r` is pressed, or a one-shot call asks for a forced fetch | Blocked | Nothing is fetched; the status line (view) or the line on the error stream (one-shot) says when the next attempt is | D38, D50, D76 |
| Blocked | The wait ends and the first request after it is answered | Normal | Refresh resumes; the error line goes. A refusal later in that refresh is a new block, with a first wait of 5 minutes | D19, D50, D98 |
| Blocked | The wait ends and the source refuses again | Blocked | The wait doubles, up to 30 minutes | D19, D50 |

### 6.4 Config file

| State | Event | Next state | What happens | Source |
|---|---|---|---|---|
| (start) | No config file at the default path | Example | The built-in example watchlist is shown, with a note saying where to put a config | D35 |
| (start) | A valid config file | Loaded | The watchlist is shown | D12 |
| (start) | An invalid config file, or `--config` names a missing file | (exit) | An error message; exit status 2 | D45, D50 |
| Example | A valid config file appears | Loaded | The watchlist replaces the example within 5 seconds | D12, D50 |
| Loaded | The file changes and is valid | Loaded | Rows are added, removed and reordered within 5 seconds; quotes of the rows that remain are kept; new rows and not-found rows are fetched at once | D12, D39, D50 |
| Loaded or Example | The file changes and is invalid | Same state | The last valid watchlist stays; the error line names the file and the problem | D45 |
| Loaded | The file is removed | Loaded | The last loaded watchlist stays; the error line says the file is gone | D50 |

In every state the tool only reads the config file (D35).

### 6.5 One-shot call

A one-shot call decides per row, in this order:

| Condition of the row | What the call does | Source |
|---|---|---|
| Not found, tried less than an hour ago, config unchanged | No fetch; prints "not found" | D39 |
| The source is blocked | No fetch; prints the cached quote, stale or not by the rule of D40 | D38, D50 |
| A forced fetch was asked for | Fetches, unless the row was attempted less than 5 seconds ago | D23, D50 |
| The row's last fetch failed and its wait has not ended | No fetch; prints the cached quote, marked stale | D19, D50 |
| No quote in the cache | Fetches | D50 |
| Market closed, and the period after the close (6.1) is over | No fetch; prints the cached last close | D15, D50 |
| Market open, last successful fetch at most one refresh interval old | No fetch; prints from the cache | D23, D50 |
| Market open, last successful fetch older than that | Fetches; if the fetch fails, prints the cached quote marked stale | D19, D23 |

Waits and blocks are shared: they hold for the view and for every one-shot call alike (D50).

When a row has no quote at all and another process is already fetching it, a one-shot call waits for that process, at most its 10 seconds, then prints what the cache holds (D73).

When the cache folder cannot be written, a one-shot call fetches nothing at all: it prints what the cache holds, or the `error` state, and warns (D60).

## 7. Behaviour seen from outside

All values in the examples are illustrative.

The product has two surfaces (D6, D18):

- **view**: the full-screen view, opened by `brl-stocks` with no command (7.3).
- **print**: the one-shot command, `brl-stocks print`, as a table (7.4) or with `--json` (7.5).

Every acceptance criterion in section 8 is tagged with the surfaces it applies to. Flags that print and exit (`--help`, `--version`, an unknown flag) count as the surface of the command they are given with: `brl-stocks --version` is `view`, `brl-stocks print --version` is `print` (D77). The config file, the cache, the source and the rules of section 6 are shared by both.

### 7.1 Commands and flags

| Command | What it does | Source |
|---|---|---|
| `brl-stocks` | Opens the full-screen view | D6, D26 |
| `brl-stocks print` | One-shot: prints an aligned table and exits | D6, D18, D41 |
| `brl-stocks print --json` | One-shot: prints a JSON document and exits | D18, D41 |
| `brl-stocks print --refresh` | One-shot with a forced fetch | D23, D41 |
| `--config <path>` | On any command: reads the config from this path | D29 |
| `--help`, `--version` | Print usage or the version and exit with status 0, with or without `print` | D50, D77 |

An unknown command or flag prints a usage message to the error stream and exits with status 2, with or without `print` (D50, D77).

`brl-stocks` with no command needs a terminal. When it has none (its output is redirected, or there is no terminal at all), it writes one line to the error stream that names `brl-stocks print`, and exits with status 2 (D94).

For testing only: when the environment variable `BRL_STOCKS_SOURCE_URL` is set, the default source is reached at that address. It does not appear in the help text and selects no other source (D70).

### 7.2 The config file

- Format: TOML (D28). Written by hand; the tool never writes it (D12, D35).
- Path: `~/.config/brl-stocks/config.toml`, or the path given with `--config` (D29). When `XDG_CONFIG_HOME` is set, `$XDG_CONFIG_HOME/brl-stocks/config.toml` (D50).
- The path may be, or pass through, a symbolic link; the tool reads through it and never replaces it (D29).

Example (D42):

```toml
refresh_seconds = 60                      # optional; default 60
summary = ["IBOV", "SP500", "USDBRL"]     # optional; this is the default; [] hides the line

tickers = ["PETR4", "AAPL"]               # optional; rows shown first, without a header

[[group]]
name = "Brasil"
tickers = ["VALE3", "ITUB4", "MXRF11", "PETR4F"]

[[group]]
name = "US"
tickers = ["MSFT", "US:ABCD1"]
```

| Key | Meaning | Source |
|---|---|---|
| `refresh_seconds` | The refresh interval. Minimum 10; a lower value is treated as 10, with a warning | D14, D42, D50 |
| `summary` | Which summary entries to show, in order. Allowed names: `IBOV`, `SP500`, `USDBRL` | D11, D42 |
| `tickers` | Tickers outside any group | D12, D42 |
| `[[group]]` with `name` and `tickers` | A named group | D16, D42 |

Rules:

- Ticker codes follow D30. Upper and lower case are the same; rows show the code in upper case, without a market prefix (D50).
- A ticker repeated inside one group is shown once. A ticker in two groups is shown in both and fetched once (D50).
- A key the tool does not know produces a warning; the tool still runs (D50).
- Warnings appear in the status line of the view and on the error stream in one-shot (D50).
- More than 20 tickers are all shown, with a warning that the tool is designed for 20 (D8, D50).
- A file that cannot be parsed, a value of the wrong type, an unknown `summary` name, or an empty ticker makes the config invalid (D45).
- A file with no tickers is valid: the view shows no rows (D102).

### 7.3 The full-screen view

Layout, top to bottom: summary line; column header; rows, under group headers; error line (only when there is a problem); status line (D50).

```
IBOV 158.432 +0,32%    S&P 500 6,812.40 -0.11%    USD/BRL 5,4321 +0,08%

TICKER  NAME                     LAST          CHANGE      LOW     HIGH  TIME        TODAY
PETR4   Petrobras PN         R$ 38,42   +0,31  +0,81%    38,02    38,60  10:26       ▂▃▅▆▅▇▆▇▇▆
AAPL    Apple Inc.         US$ 229.87   -1.12  -0.48%   228.90   231.40  20/11       ▇▆▅▅▃▂▃▂▂▃
Brasil
VALE3   Vale ON              R$ 61,07   +0,00  +0,00%    60,80    61,55  10:14 !12m  ▅▅▆▅▅▄▅▅▅▅
XXXX9   —                   not found       —       —        —        —  —
US
MSFT    Microsoft Corp     US$ 512.30   +2.05  +0.40%   509.10   513.00  20/11       ▃▄▄▅▆▆▇▇▇▇

source unreachable: 1 row not updated · next attempt 10:43
B3 open · US closed · sort: file order · ? help
```

- **Summary line.** Fixed above the watchlist; it does not scroll (D11). Its entries are labelled `IBOV`, `S&P 500` and `USD/BRL` (D71). Each entry shows its value and day change percent; indices carry no currency symbol; USD/BRL has four decimals (D50). The Ibovespa is shown in whole points and the S&P 500 with two decimals, as the examples in D51 show them (D65). Each entry's percent follows the format of its entry (D51). When the terminal is too narrow for the whole line, each entry's percent is left out first, then whole entries from the right; an entry is never cut (D96).
- **Row.** Ticker, name, last price, day change (value and percent), day low and high, quote time (D9), then the sparkline (D13).
- **Alignment.** In every row, the ticker and the name start at the same position; the last price, the change value, the percent, the low and the high each end at the same position; the quote time starts at the same position (D18, D71).
- **Currency.** B3 prices carry `R$`, US prices `US$`; nothing is converted (D10).
- **Numbers.** Each row uses the format of its currency: R$ rows a comma for decimals and a dot for thousands (`R$ 1.038,42`), US$ rows a dot for decimals and a comma for thousands (`US$ 1,229.87`). This covers the amounts in the row's currency: the price, the change value and the day low and high (D46). The day change percent is not an amount in a currency; it follows the same format (D64). Prices, including the day low and high, have two decimals (D46); so have the change value and the percent (D65). Only the last price carries the currency symbol, followed by one space; values are rounded half away from zero; a minus sign is the plain hyphen; a change of zero reads `+0,00` or `+0.00` (D66). In the summary line, each entry uses the format of its market, and USD/BRL the R$ format (D51).
- **Day change.** Always with a sign. Green when up, red when down, from the terminal's own palette; no colour when zero (D25, D50). No colour at all when `NO_COLOR` is set (D25).
- **Quote time.** In the machine's local time, as `HH:MM`; as `DD/MM` when the quote is not from today (D50).
- **Sparkline.** The regular session of the current or last trading day, 10 characters wide, earliest on the left, in the colour of the day change (D13, D50). The prices so far are sampled evenly into the 10 characters, always keeping the first and the latest; fewer than 10 prices take one character each, from the left (D104). Each price is drawn with one of the eight blocks `▁▂▃▄▅▆▇█`, on a linear scale from the lowest of the prices it draws (`▁`) to the highest (`█`), with the eight levels evenly spaced, each price at the nearest level; a flat line is all `▄` (D105). For a not-found row, a row with no data yet and a quote whose series has no prices, the sparkline column is blank, not `—`; a stale row and a cached row keep the sparkline of their last successful fetch (D106). The column's header is `TODAY`, dropped with the column; the sparkline starts at the same position in every row, two spaces after the widest quote time with its stale mark (D107).
- **Stale row.** `!` and the age since the last successful fetch after the quote time: `!45s`, `!12m`, `!3h`, `!2d` (D19, D40, D50).
- **Not found.** "not found" in place of the price; `—` in the other data columns (D39, D50).
- **No data yet.** `—` in every data column (D50).
- **Group header.** The group's name on its own line above its rows; tickers outside groups come first with no header (D16, D42).
- **Closed market.** The status line gives the state of each market: `B3 open · US closed`. While the tool has no quote for any instrument of a market, the word is `unknown` (D74). Rows of a closed market show the last close; they are stale only if their last fetch failed (D15, D40, D47).
- **Error line.** At most one line, whatever the number of problems (D19). It gives the cause, the number of rows affected and the time of the next attempt, as `HH:MM` in local time (D79). Summary entries are not rows and are not counted; when only summary entries failed, the line says `summary not updated` instead of a number (D81). The cause is one of four fixed phrases: `source unreachable` (no connection, timeout, server error), `source is rate-limiting`, `source refused access`, `source answer not understood` (D71). When several problems exist, it shows the first of: config problem, block, failed fetches. Not-found rows do not raise it (D50). A config problem reads `invalid config <path>, last valid watchlist kept: <problem>`, `config <path> was deleted: showing the last watchlist` or `cannot read the config <path>: <error>`, with the home folder written `~` (D101).
- **Status line.** Market states, current sort, during a block the time of the next attempt, and any warning (D38, D47, D50). A warning is shortened to fit: the home folder is written `~`, and the wording is short, as in `no config: example watchlist; put yours at ~/.config/brl-stocks/config.toml`. When a warning still does not fit, the current sort is left out before the warning is cut (D95).
- **Narrow terminal.** Columns are dropped in this order as the width shrinks: sparkline, day low and high, name, day change value. Ticker, last price, day change percent and quote time always remain. The view follows a resize (D50).
- **Language.** Interface text is in English (D50).

Keys (D32):

| Key | Action | Source |
|---|---|---|
| `q` | Quit, with exit status 0, leaving the terminal as it was found. Ctrl+C does the same | D32, D50 |
| `r` | Refresh now (see 6.3) | D32, D38 |
| `s` | Cycle the sort: file order → day change (percent, highest first) → ticker (A to Z) → file order | D31, D50 |
| up and down arrows, `j`/`k` | Scroll by one screen line, a row or a group header alike, when the rows do not fit | D32, D50, D93 |
| `?` | Show the list of keys; any key closes it | D32, D50 |

Any other key does nothing while the help is closed (D32); so do the left and right arrows (D92). Sorting works inside each group; groups keep the file's order; rows without a quote go last; the sort is not remembered between runs (D50).

### 7.4 One-shot table

```
IBOV 158.432 +0,32%    S&P 500 6,812.40 -0.11%    USD/BRL 5,4321 +0,08%

TICKER  NAME                     LAST          CHANGE      LOW     HIGH  TIME
PETR4   Petrobras PN         R$ 38,42   +0,31  +0,81%    38,02    38,60  10:26
Brasil
VALE3   Vale ON              R$ 61,07   +0,00  +0,00%    60,80    61,55  10:14 !12m

B3 open · US closed
```

The summary line, a header row, the columns of the standard row without the sparkline, group names on their own lines, and a last line with the state of each market; numbers are formatted and columns aligned as in the view (D46, D51, D71); stale and not-found rows are marked as in the view (D15, D18, D50). The cause of a failure and any warning go to the error stream, one line each, so that standard output stays a table (D19, D50). The failure line gives what the view's error line gives: the cause phrase, the number of rows not updated and the time of the next attempt; with several problems, one line, chosen as in the view (D76). A quote time not from today reads `DD/MM`, as in the view (D75). Colour follows D25: none when the output is piped or `NO_COLOR` is set.

### 7.5 One-shot JSON

One JSON document on standard output (D18). Shape (D43):

```json
{
  "schema_version": 1,
  "state": "partial",
  "generated_at": "2026-11-23T14:41:07Z",
  "error": null,
  "summary": [
    {"id": "IBOV", "name": "Ibovespa", "value": 158432.1, "change": 512.3,
     "change_percent": 0.32, "quote_time": "2026-11-23T14:26:00Z",
     "market_state": "open", "status": "fresh", "fetched_at": "2026-11-23T14:41:05Z"}
  ],
  "groups": [
    {"name": null, "rows": [
      {"ticker": "PETR4", "market": "B3", "name": "Petrobras PN", "currency": "BRL",
       "price": 38.42, "change": 0.31, "change_percent": 0.81,
       "day_low": 38.02, "day_high": 38.60,
       "quote_time": "2026-11-23T14:26:00Z", "market_state": "open",
       "status": "fresh", "fetched_at": "2026-11-23T14:41:05Z"}
    ]},
    {"name": "Brasil", "rows": [
      {"ticker": "XXXX9", "market": "B3", "name": null, "currency": "BRL",
       "price": null, "change": null, "change_percent": null,
       "day_low": null, "day_high": null, "quote_time": null,
       "market_state": null, "status": "not_found", "fetched_at": null}
    ]}
  ]
}
```

- Prices are JSON numbers; times are UTC in RFC 3339; a missing value is `null` (D43). Numbers in the document are neither rounded nor formatted for a currency (D66).
- `status` of a row: `fresh`, `stale`, `not_found` or `no_data` (D43).
- Tickers outside groups are in a first group whose `name` is `null` (D43).
- The intraday series is not in the document (D43).

### 7.6 One-shot result and exit status (D44)

| `state` | Meaning |
|---|---|
| `ok` | Every row is fresh |
| `error` | No row has a quote and at least one fetch failed; `error` holds the reason |
| `stale` | No row is fresh, and at least one row has a quote |
| `partial` | Anything else: some rows are stale, not found or without data |

- Exit status 0 whenever the command line and the config were valid, in any of the four states. A status bar never breaks because the source is down (D44; borrowed from the one-shot contract named in D21, F10).
- Exit status 2 for a usage or config error, with the message on the error stream. With `--json`, standard output still carries a valid document with `state` `error` (D44, D45).
- A one-shot call waits at most 10 seconds for its fetches, then prints what it has (D50).

### 7.7 Files the tool keeps

- The cache lives in `~/.cache/brl-stocks/` (D29), or in `$XDG_CACHE_HOME/brl-stocks/` when that variable is set (D50). It holds the last quotes only (D20).
- A cache that cannot be read is treated as absent, without a message (D48).
- When the cache folder cannot be written, the tool still runs and says so once in a warning (D50). The view goes on fetching; a one-shot call does not fetch, because nothing could pace it between calls (D60).
- With no config file, the tool shows a built-in example watchlist (`PETR4`, `VALE3`, `ITUB4`, `AAPL`, `MSFT`, with the default summary line) and names the path where a config belongs: in the status line of the view, on the error stream in one-shot (D35, D50).

## 8. Stories

"The user" is the person running the tool. "A script" is a program or status bar that calls the one-shot mode.

**Surfaces.** Each criterion carries the surfaces it applies to (section 7), in brackets after its id; a criterion tagged with both holds on each. Many were written in the words of the view. On `print` they read as follows (7.2, 7.4, 6.3, 6.5):

| In the criterion | On `print` |
|---|---|
| the view starts, is open, runs or is drawn; the row or the summary line is drawn; quotes are shown | `brl-stocks print` is run, and the table shows the result |
| at full width | always: the table drops no column (7.4) |
| until its first quote arrives | when no quote arrives during the call (D50) |
| the error line; a warning in the status line | a line on the error stream (D19, D50); a warning is a line of its own, so "exactly one error line" counts the lines that give a cause (7.4) |
| the status line reads (market states) | the last line of the table (D47, D50) |
| the status line shows the time of the next attempt | the line on the error stream that gives the cause (D76) |
| `r` is pressed | `brl-stocks print --refresh` is run (D23, D38, D50) |
| a refresh runs, a refresh succeeds or fails | the fetches of one `brl-stocks print` call |

A row's number formats are those of the table; the JSON carries numbers unformatted (D66).

Criteria that count requests, or that need a source that fails, refuses or does not know a ticker, are run against a stand-in source. How a test puts one in place is a design question (section 10).

### S1 — Watch the list in a full-screen view

As the user, I want a full-screen view of my watchlist, so that I see the price and day change of what I follow at a glance. (D3, D6, D9)

- **S1.1** [view] Given a config with the tickers `PETR4` and `AAPL` and a source that answers, when `brl-stocks` is run with no command, then a full-screen view shows one row for each ticker. (D3, D6)
- **S1.2** [view, print] Given a row whose fetch succeeded, when the view is drawn at full width, then the row shows ticker, name, last price, day change as a value, day change as a percent, day low, day high and quote time. (D9)
- **S1.3** [view] Given the view is open, when `q` is pressed, then the tool exits with status 0. (D32, D50)
- **S1.4** [view] Given the cache holds quotes for the watchlist, when the view starts, then the rows show the cached quotes before any answer from the source has arrived. (D20)
- **S1.5** [view, print] Given no cache, when the view starts, then each row shows its ticker and `—` in the data columns until its first quote arrives. (D50)
- **S1.6** [view] Given a terminal too narrow for the full row, when the view is drawn, then ticker, last price, day change percent and quote time are still shown. (D50)
- **S1.7** [view, print] Given a quote from an earlier day, when the row is drawn, then the quote time is shown as a date (`DD/MM`). (D50, D75)
- **S1.8** [view, print] Given a B3 row whose last price is 1038.42, when the row is drawn, then the price reads `R$ 1.038,42`. (D46)
- **S1.9** [view, print] Given a US row whose last price is 1229.87, when the row is drawn, then the price reads `US$ 1,229.87`. (D46)
- **S1.10** [view, print] Given a B3 row whose day change is +0.31, when the row is drawn, then the change value reads `+0,31`. (D46, D50, D65)
- **S1.11** [view, print] Given a B3 row whose day change is +0.81 percent, when the row is drawn, then the percent reads `+0,81%`. (D50, D64, D65)
- **S1.12** [view, print] Given a US row whose day change is -0.48 percent, when the row is drawn, then the percent reads `-0.48%`. (D64, D65, D66)
- **S1.13** [view, print] Given a B3 row whose day change is exactly zero, when the row is drawn, then the change value reads `+0,00`. (D50, D66)
- **S1.14** [view, print] Given a B3 row whose last price is 38.425, when the row is drawn, then the price reads `R$ 38,43`. (D46, D66)
- **S1.15** [view, print] Given a B3 row whose day low is 38.02, when the row is drawn, then the low reads `38,02`, with no currency symbol. (D46, D66)

### S2 — Summary line

As the user, I want the Ibovespa, the S&P 500 and USD/BRL always in sight, so that I can read my tickers against the market. (D11)

- **S2.1** [view, print] Given a config without a `summary` key, when the view is drawn, then a line above the watchlist shows Ibovespa, S&P 500 and USD/BRL, each with its value and day change percent. (D11, D50)
- **S2.2** [view, print] Given `summary = ["USDBRL"]`, when the view is drawn, then the summary line shows only USD/BRL. (D11, D42)
- **S2.3** [view, print] Given `summary = []`, when the view is drawn, then there is no summary line. (D42)
- **S2.4** [view] Given more rows than fit the screen, when the user scrolls, then the summary line stays in place. (D11)
- **S2.5** [view, print] Given an Ibovespa value of 158432.10, when the summary line is drawn, then the Ibovespa value reads `158.432`. (D51, D65)
- **S2.6** [view, print] Given an S&P 500 value of 6812.4, when the summary line is drawn, then the S&P 500 value reads `6,812.40`. (D51, D65)
- **S2.7** [view, print] Given a USD/BRL rate of 5.4321, when the summary line is drawn, then the USD/BRL value reads `5,4321`. (D50, D51)
- **S2.8** [view, print] Given an Ibovespa day change of +0.32 percent, when the summary line is drawn, then its percent reads `+0,32%`. (D50, D51, D65)
- **S2.9** [view] Given a terminal too narrow for the summary line with even one percent shown, but wide enough for all its entries without their percents, when the view is drawn, then each entry shows its label and value and no percent. (D96)
- **S2.10** [view] Given a terminal too narrow for all summary entries even without their percents, when the view is drawn, then entries are left out from the right and every entry shown is whole. (D96)

### S3 — Two markets, plain tickers, own currency

As the user, I want to write tickers the way I know them and see each price in its own currency, so that my list does not depend on a data source. (D2, D10, D17, D30)

- **S3.1** [view, print] Given the ticker `PETR4` in the config, when its quote is shown, then it is the B3 instrument, priced in `R$`. (D30, D10)
- **S3.2** [view, print] Given the tickers `PETR4F`, `MXRF11` and `AAPL34`, when the config is loaded, then all three are treated as B3. (D30)
- **S3.3** [view, print] Given the tickers `AAPL`, `MSFT` and `F`, when the config is loaded, then all three are treated as US, priced in `US$`. (D30, D10)
- **S3.4** [view, print] Given the ticker `US:ABCD1`, when the config is loaded, then it is treated as US although its shape is that of B3. (D30)
- **S3.5** [view, print] Given the ticker `B3:XYZ`, when the config is loaded, then it is treated as B3 although its shape is not that of B3. (D30)
- **S3.6** [view, print] Given a B3 row and a US row on screen, when quotes are shown, then no price has been converted to the other currency. (D10)
- **S3.7** [view, print] Given the ticker written as `petr4`, when the row is drawn, then it shows `PETR4`. (D50)

### S4 — Groups

As the user, I want to split the watchlist into named groups, so that related tickers sit together. (D16)

- **S4.1** [view, print] Given a config with the groups "Brasil" and "US" in that order, when the view is drawn, then each name appears as a header above its rows, in that order. (D16, D50)
- **S4.2** [view, print] Given a config with no group, when the view is drawn, then no group header appears. (D16, D50)
- **S4.3** [view, print] Given tickers outside groups and a group, when the view is drawn, then the tickers outside groups come first, with no header. (D42, D50)
- **S4.4** [view, print] Given the same ticker in two groups, when a refresh runs, then the ticker is requested from the source once. (D50)

### S5 — Automatic refresh, and markets that are closed

As the user, I want quotes refreshed by themselves while a market is open and left alone when it is closed, so that the screen is current without wasting requests. (D14, D15)

- **S5.1** [view] Given B3 is open and no `refresh_seconds` is set, when the view stays open for 10 minutes, then each B3 row is requested from the source between 9 and 11 times. (D14, D50)
- **S5.2** [view] Given `refresh_seconds = 120` and an open market, when the view stays open for 10 minutes, then each row of that market is requested between 4 and 6 times. (D14, D42, D50)
- **S5.3** [view] Given the US market has been closed for more than 30 minutes and every US row has a quote, when the view stays open for 10 minutes, then no US row is requested. (D15, D50)
- **S5.4** [view, print] Given the US market is closed, when the view is drawn, then US rows show the last close. (D15)
- **S5.5** [view, print] Given the US market is closed and B3 is open, when the view is drawn, then the status line reads `B3 open · US closed`. (D15, D47)
- **S5.6** [view, print] Given a closed market whose rows were last fetched, with success, two days ago, when the view is drawn, then those rows carry no stale mark. (D40)
- **S5.7** [view] Given a closed market and an open view, when the market's next session starts, then its rows are requested within one refresh interval without a key press. (D14, D15, D50)
- **S5.8** [view] Given B3 closed less than 30 minutes ago and a B3 row whose quote time is before the close, when a refresh interval passes, then that row is requested again. (D50)
- **S5.9** [view, print] Given a weekday that is a holiday for a market, when the view is open during that market's usual hours, then the market is shown as closed. (D15; rests on F14, unverified, see section 12)
- **S5.10** [view, print] Given `refresh_seconds = 3`, when the view runs, then rows are requested no more often than every 10 seconds. (D50)
- **S5.11** [view, print] Given the US market before its regular session, when the view is drawn, then US rows show the last regular close, not a pre-market price. (D50)
- **S5.12** [view, print] Given a row whose last fetch failed before its market closed, when the view is drawn after the close, then the row is still marked stale. (D40)

### S6 — When a refresh fails

As the user, I want old prices kept and clearly marked when the source fails, so that I never take an old price for a current one. (D19, D40)

- **S6.1** [view, print] Given rows with quotes and an open market, when a refresh fails, then the rows still show their last known prices. (D19)
- **S6.2** [view, print] Given a row whose last fetch failed, when the view is drawn, then the row is marked stale with its age. (D19, D40)
- **S6.3** [view, print] Given a refresh in which five rows failed, when the view is drawn, then exactly one error line is shown. (D19)
- **S6.4** [view] Given a row that keeps failing with the default interval, when the view stays open, then the automatic attempts for it come 60, 120, 240, 480, 960 and then every 1,800 seconds apart. (D19, D50)
- **S6.5** [view, print] Given stale rows and an error line, when a refresh succeeds for every row, then the stale marks are gone. (D19, D40)
- **S6.6** [view, print] Given stale rows and an error line, when a refresh succeeds for every row, then the error line is gone. (D19)
- **S6.7** [view] Given an open market and a row whose last successful fetch is older than two refresh intervals with no failed fetch since (the machine just woke from sleep), when the view is drawn, then the row is marked stale. (D40)
- **S6.8** [view, print] Given a refresh in which one row fails and the others succeed, when the view is drawn, then only that row is marked stale. (D40)
- **S6.9** [view, print] Given a failed refresh, when the error line is drawn, then it names the cause, the number of rows not updated and the time of the next attempt. (D50, D76, D79)
- **S6.10** [view, print] Given a stale row whose last successful fetch was 12 minutes ago, when the row is drawn, then the mark reads `!12m`. (D50)
- **S6.11** [view, print] Given every row updates and a summary entry fails, when the view is drawn, then the error line says the summary was not updated. (D81)

### S7 — Refresh now, and a source that is blocking

As the user, I want to force a refresh when it can help and be told when it cannot, so that I do not make a block worse. (D32, D38)

- **S7.1** [view, print] Given B3 is open and the source answers, when `r` is pressed, then the first B3 rows are requested within 2 seconds, without waiting for the interval. (D32, D62)
- **S7.2** [view, print] Given a network error has put the next automatic attempt four minutes away, when `r` is pressed, then the first failed rows are requested within 2 seconds. (D38, D62)
- **S7.3** [view, print] Given the source is rate-limiting or refusing the tool, when `r` is pressed, then no request is sent. (D38)
- **S7.4** [view, print] Given the source is rate-limiting or refusing the tool, when `r` is pressed, then the status line shows the time of the next attempt. (D38, D76, D79)
- **S7.5** [view] Given the source refuses a request, when the view stays open and the source keeps refusing, then the next attempts come 5, 10, 20 and then every 30 minutes apart. (D50)
- **S7.6** [view, print] Given a closed market, when `r` is pressed, then its rows are requested too. (D50)
- **S7.7** [view, print] Given `r` was pressed two seconds ago, when `r` is pressed again, then no row is requested a second time. (D50)
- **S7.8** [view, print] Given a not-found row tried ten minutes ago and an unchanged config, when `r` is pressed, then that ticker is not requested. (D39)
- **S7.9** [view] Given B3 is open, 20 B3 rows and a source that answers within one second, when `r` is pressed, then every B3 row is requested before one refresh interval has passed. (D32, D62)
- **S7.10** [view] Given the source refused a request and the view stays open, when the wait ends and the source answers, then the rows are requested again without a key press. (D19, D50)

### S8 — A ticker the source does not know

As the user, I want a mistyped ticker to say so in its own row, so that one error does not hide the rest of the list. (D39)

- **S8.1** [view, print] Given the ticker `XXXX9`, which the source does not know, when the view is drawn, then its row shows "not found" in place of the price. (D39)
- **S8.2** [view, print] Given one not-found row among others, when a refresh runs, then the other rows show fresh quotes. (D39)
- **S8.3** [view] Given a not-found row and an unchanged config, when the view stays open for three hours, then the ticker is requested at least twice and at most four times. (D39)
- **S8.4** [view, print] Given a not-found row, when the config file changes, then the ticker is requested again without waiting for the hour. (D39)
- **S8.5** [view, print] Given a not-found row and no other problem, when the view is drawn, then there is no error line. (D50)

### S9 — Sort, scroll and help

As the user, I want to reorder and scroll the list with a few keys, so that I find what moved most. (D31, D32)

- **S9.1** [view, print] Given the view has just started, when it is drawn, then rows are in the order of the config file. (D31, D42)
- **S9.2** [view] Given file order, when `s` is pressed once, then rows are ordered by day change. (D31)
- **S9.3** [view] Given the order by day change, when `s` is pressed once, then rows are ordered by ticker. (D31)
- **S9.4** [view] Given the order by ticker, when `s` is pressed once, then rows are back in file order. (D31, D50)
- **S9.5** [view] Given two groups and the order by day change, when the view is drawn, then each group's rows run from the highest percent to the lowest. (D50)
- **S9.6** [view] Given more rows than fit the screen, when the down arrow or `j` is pressed, then the list scrolls down by one screen line, whether that line holds a row or a group header. (D32, D93)
- **S9.7** [view] Given more rows than fit the screen and a scrolled list, when the up arrow or `k` is pressed, then the list scrolls up by one screen line, whether that line holds a row or a group header. (D32, D93)
- **S9.8** [view] Given the view is open, when `?` is pressed, then the keys `q`, `r`, `s`, the scroll keys and `?` are listed with their actions. (D32)
- **S9.9** [view] Given the view is open, when a key outside that list is pressed, then nothing changes. (D32)
- **S9.10** [view] Given two groups and the order by day change, when the view is drawn, then the groups are still in file order. (D50)
- **S9.11** [view] Given the view is open and the list scrolled, when the left or right arrow is pressed, then nothing changes. (D92)

### S10 — Sparkline

As the user, I want a small chart of the day in each row, so that I see how the price got where it is. (D13)

- **S10.1** [view] Given a row with a quote from an open market, when the view is drawn at full width, then the row ends with a sparkline of the day's prices so far. (D13)
- **S10.2** [view] Given a row of a closed market, when the view is drawn at full width, then the sparkline shows the last trading day. (D50)
- **S10.3** [view] Given the view, when any key from section 7.3 is pressed, then no alert, detail view or news appears. (D13, D32)
- **S10.4** [view] Given a row whose series has more than 10 prices, when the view is drawn at full width, then its sparkline is 10 characters wide. (D50)
- **S10.5** [view] Given a row whose day change is positive, when it is drawn on a terminal at full width, then its sparkline is in the terminal palette's green. (D25, D50)
- **S10.6** [view] Given a row whose day change is negative, when it is drawn on a terminal at full width, then its sparkline is in the terminal palette's red. (D25, D50)
- **S10.7** [view] Given a row whose day change is exactly zero, when it is drawn on a terminal at full width, then its sparkline has no colour. (D50)
- **S10.8** [view] Given a terminal one cell narrower than the widest row with its sparkline, when the view is drawn, then no row has a sparkline. (D50)
- **S10.9** [view] Given the source's answer holds prices from before the regular session opened or after it closed, when the row is drawn, then its sparkline does not include them. (D50)
- **S10.10** [view] Given a row whose prices rose steadily all day, when the view is drawn at full width, then the lowest character of its sparkline is on the left and the highest on the right. (D50)
- **S10.11** [view] Given a row whose series has 4 prices, when the view is drawn at full width, then its sparkline is 4 characters wide. (D104)
- **S10.12** [view] Given a row whose series has 25 prices, the first of them the day's lowest and the latest the day's highest, when the view is drawn at full width, then its sparkline starts with `▁` and ends with `█`. (D104, D105)
- **S10.13** [view] Given a row whose series is the 10 prices 10, 11, 12, …, 19, when the view is drawn at full width, then its sparkline reads `▁▂▃▃▄▅▆▆▇█`. (D104, D105)
- **S10.14** [view] Given a row whose series holds the same price 10 times, when the view is drawn at full width, then its sparkline reads `▄▄▄▄▄▄▄▄▄▄`. (D104, D105)
- **S10.15** [view] Given a not-found row, when the view is drawn at full width, then its sparkline column is blank. (D106)
- **S10.16** [view] Given a row with no data yet, when the view is drawn at full width, then its sparkline column is blank. (D106)
- **S10.17** [view] Given a quote whose series has no prices, when the view is drawn at full width, then its row's sparkline column is blank. (D106)
- **S10.18** [view] Given a row with a sparkline whose next refresh fails, when the view is drawn at full width, then the stale row shows the same sparkline. (D106)
- **S10.19** [view] Given a cache left by an earlier run whose row had a sparkline, and a source that has not answered yet, when the view opens at full width, then the cached row shows that sparkline. (D106)
- **S10.20** [view] Given the view at full width, when the column header is drawn, then the sparkline's column is headed `TODAY`. (D107)
- **S10.21** [view] Given a terminal one cell narrower than the widest row with its sparkline, when the view is drawn, then the column header has no `TODAY`. (D50, D107)
- **S10.22** [view] Given rows whose quote times and stale marks differ in width, when the view is drawn at full width, then every sparkline starts two spaces after the end of the widest quote time with its stale mark. (D107)

### S11 — The watchlist is a file I edit

As the user, I want the watchlist in a config file that I edit by hand and keep in my dev-env repository, so that it is versioned with the rest of my setup. (D12, D28, D29)

- **S11.1** [view, print] Given a valid file at `~/.config/brl-stocks/config.toml` and no `--config` flag, when the tool starts, then it shows that file's watchlist. (D29)
- **S11.2** [view, print] Given `--config /tmp/other.toml`, when the tool starts, then it shows the watchlist of that file. (D29)
- **S11.3** [view] Given the view is open, when a ticker is added to the config file and the file is saved, then a row for it appears within 5 seconds, without a restart. (D12, D50)
- **S11.4** [view] Given the view is open, when a ticker is removed from the config file and the file is saved, then its row disappears within 5 seconds. (D12, D50)
- **S11.5** [view, print] Given `~/.config/brl-stocks` is a symbolic link to a folder elsewhere, when the tool starts, then it reads the config through the link. (D29)
- **S11.6** [view] Given `~/.config/brl-stocks` is a symbolic link and the view is open, when a ticker is added to the file in the link's target, then a row for it appears within 5 seconds. (D12, D29, D50)
- **S11.7** [view, print] Given `~/.config/brl-stocks` is a symbolic link, when the tool has run in any mode and exited, then the link still exists and points to the same target. (D29)
- **S11.8** [view, print] Given a config file, when the tool has run in any mode and exited, then the file's content is unchanged, byte for byte. (D12, D35)
- **S11.9** [view, print] Given a config file that cannot be parsed, when the tool starts, then it exits with status 2. (D45)
- **S11.10** [view] Given the view is open, when the config file is saved with a syntax error, then the view keeps the last valid watchlist. (D45)
- **S11.11** [view] Given the view is open, when the config file is saved with a syntax error, then the error line names the file and the problem. (D45)
- **S11.12** [view, print] Given a config with a key the tool does not know, when the tool starts, then a warning names the key. (D50)
- **S11.13** [view, print] Given a config with 25 tickers, when the view is drawn, then all 25 rows exist. (D8, D50)
- **S11.14** [view] Given the view is open, when the config file is deleted, then the view keeps the last loaded watchlist. (D50)
- **S11.15** [view, print] Given `XDG_CONFIG_HOME=/tmp/x` and a valid file at `/tmp/x/brl-stocks/config.toml`, when the tool starts without `--config`, then it shows that file's watchlist. (D50)
- **S11.16** [view, print] Given a config file that cannot be parsed, when the tool starts, then a message on the error stream names the file and the problem. (D45)
- **S11.17** [view, print] Given a config with a key the tool does not know, when the tool starts, then the watchlist is shown. (D50)
- **S11.18** [view, print] Given a config with 25 tickers, when the view is drawn, then a warning in the status line says the tool is designed for 20. (D50)
- **S11.19** [view] Given the view is open, when the config file is deleted, then the error line says the file is gone. (D50)
- **S11.20** [view] Given no file at the default config path and the view open with the example watchlist, when a valid config file is saved at that path, then its watchlist replaces the example within 5 seconds. (D12, D50)

### S12 — No config yet

As a new user, I want the tool to run before I have written a config, so that I can try it first. (D35)

- **S12.1** [view, print] Given no file at the default config path, when the tool starts, then it shows a built-in example watchlist. (D35)
- **S12.2** [view, print] Given no file at the default config path, when the tool starts, then it names the path where a config file belongs. (D35)
- **S12.3** [view, print] Given no file at the default config path, when the tool has run in any mode and exited, then no file or folder has been created under the config path. (D35)
- **S12.4** [view, print] Given `--config /tmp/missing.toml` and no such file, when the tool starts, then it exits with status 2. (D50)
- **S12.5** [view, print] Given `--config /tmp/missing.toml` and no such file, when the tool starts, then a message on the error stream names the path. (D50)
- **S12.6** [view] Given no file at the default config path, which is under the home folder, when the view is drawn, then the status line names the path with the home folder written `~`. (D95)
- **S12.7** [view] Given no file at the default config path and a terminal too narrow for the whole status line but wide enough for it without the current sort, when the view is drawn, then the status line does not show the sort. (D95)
- **S12.8** [view] Given no file at the default config path and a terminal too narrow for the whole status line but wide enough for it without the current sort, when the view is drawn, then the status line shows the whole warning. (D95)

### S13 — One-shot table

As a script or the user at a prompt, I want the watchlist printed once, so that I can read it or pipe it without opening the view. (D6, D18, D23)

- **S13.1** [print] Given a valid config, when `brl-stocks print` is run, then it prints an aligned table with one line per row to standard output and exits. (D6, D18, D41)
- **S13.2** [print] Given an open market and a cache whose quotes were fetched at most 60 seconds ago, when `brl-stocks print` is run with the default interval, then no request is sent to the source. (D23)
- **S13.3** [print] Given an open market and a cache whose quotes were fetched 5 minutes ago, when `brl-stocks print` is run, then the quotes are requested from the source before printing. (D23)
- **S13.4** [print] Given an open market and a cache fetched 20 seconds ago, when `brl-stocks print --refresh` is run, then the quotes are requested from the source. (D23, D41)
- **S13.5** [print] Given a market closed for more than 30 minutes, with cached quotes, when `brl-stocks print` is run, then none of that market's rows is requested. (D15, D50)
- **S13.6** [print] Given cached quotes, an open market and a source that cannot be reached, when `brl-stocks print` is run, then the cached quotes are printed, marked stale with their age. (D19)
- **S13.7** [print] Given the output is piped to another program, when `brl-stocks print` is run, then the output contains no colour codes. (D25)
- **S13.8** [print] Given a config with groups, when `brl-stocks print` is run, then each group's name is on a line of its own above its rows. (D16, D50)
- **S13.9** [print] Given a source that does not answer, when `brl-stocks print` is run, then it prints and exits within 11 seconds. (D50)
- **S13.10** [print] Given the source is rate-limiting or refusing the tool, when `brl-stocks print --refresh` is run, then no request is sent. (D50)
- **S13.11** [print] Given an open market and a cache whose quotes were fetched at most 60 seconds ago, when `brl-stocks print` is run, then the table shows the cached quotes. (D23)
- **S13.12** [print] Given a closed market with cached quotes, when `brl-stocks print` is run, then that market's rows show the last close. (D15)
- **S13.13** [print] Given a default summary, when `brl-stocks print` is run, then the first line of the output starts with `IBOV`. (D11, D50, D71)
- **S13.14** [print] Given the US market is closed and B3 is open, when `brl-stocks print` is run, then the last line of the output reads `B3 open · US closed`. (D15, D50)
- **S13.15** [print] Given cached quotes, an open market and a source that cannot be reached, when `brl-stocks print` is run, then exactly one line on the error stream starts with `source unreachable`. (D19, D50, D71)
- **S13.16** [print] Given a row whose fetch failed 30 seconds ago, when `brl-stocks print` is run without `--refresh`, then that row is not requested. (D19, D50)
- **S13.17** [print] Given a B3 row whose last price is 38.42, when `brl-stocks print` is run, then the table shows `R$ 38,42` in that row. (D46)
- **S13.18** [print] Given a US row whose last price is 229.87, when `brl-stocks print` is run, then the table shows `US$ 229.87` in that row. (D46)
- **S13.19** [print] Given a default summary and an Ibovespa value of 158432.10, when `brl-stocks print` is run, then the first line shows `158.432`. (D51, D65)
- **S13.20** [print] Given two rows whose names and prices have different lengths, when `brl-stocks print` is run, then the two prices end at the same column. (D18, D71)
- **S13.21** [print] Given a source that is rate-limiting and cached quotes, when `brl-stocks print` is run, then one line on the error stream starts with `source is rate-limiting`. (D19, D38, D71)
- **S13.22** [print] Given an empty cache and a source that answers, when ten `brl-stocks print` calls are started at the same moment, then every one of them prints a quote for each row. (D73)
- **S13.23** [print] Given an empty cache and a source that answers, when ten `brl-stocks print` calls are started at the same moment, then the source receives one request per row. (D20, D49)
- **S13.24** [print] Given no cache and a source that cannot be reached, when `brl-stocks print` is run, then the last line of the output reads `B3 unknown · US unknown`. (D74)

### S14 — One-shot JSON

As a script, I want the watchlist as JSON, so that I can parse it without scraping a table. (D18)

- **S14.1** [print] Given a valid config, when `brl-stocks print --json` is run, then standard output is exactly one valid JSON document. (D18)
- **S14.2** [print] Given a row with a quote, when `brl-stocks print --json` is run, then the row's object carries ticker, name, last price, day change value, day change percent, day low, day high and quote time. (D9, D18)
- **S14.3** [print] Given any run with `--json`, when the document is read, then it has a `schema_version` of 1. (D43)
- **S14.4** [print] Given every row is fresh, when `brl-stocks print --json` is run, then `state` is `ok`. (D44)
- **S14.5** [print] Given one stale row and the others fresh, when `brl-stocks print --json` is run, then `state` is `partial`. (D44)
- **S14.6** [print] Given no cache and a source that cannot be reached, when `brl-stocks print --json` is run, then standard output is a valid document with `state` `error`. (D44)
- **S14.7** [print] Given no cache and a source that cannot be reached, when `brl-stocks print --json` is run, then the exit status is 0. (D44)
- **S14.8** [print] Given a config file that cannot be parsed, when `brl-stocks print --json` is run, then the exit status is 2. (D44, D45)
- **S14.9** [print] Given a row with a quote, when the document is read, then its prices are JSON numbers. (D43)
- **S14.10** [print] Given a row with a quote, when the document is read, then its times are UTC in RFC 3339. (D43)
- **S14.11** [print] Given a closed market, when `brl-stocks print --json` is run, then the rows of that market have `market_state` `closed`. (D15, D43)
- **S14.12** [print] Given a B3 row whose last price is 38.425, when `brl-stocks print --json` is run, then its `price` is the number 38.425. (D43, D66)

### S15 — The last quotes are kept between runs

As the user, I want the last quotes kept between runs, so that the view starts with data and frequent one-shot calls do not hit the source. (D20, D29)

- **S15.1** [view, print] Given a run that fetched quotes and then ended, when the tool is started again with no network, then the rows show those quotes. (D20)
- **S15.2** [view, print] Given the tool has fetched quotes, when the files it keeps are listed, then they are under `~/.cache/brl-stocks/`. (D29)
- **S15.3** [view] Given the view has run for eight hours of open market, when the cache folder is measured, then its size is within 10% of its size after the first full refresh. (D20, D50)
- **S15.4** [view, print] Given a cache file emptied or filled with garbage, when the tool starts, then it shows the watchlist as it does with no cache. (D48)
- **S15.5** [view] Given a cache folder that cannot be written, when the view runs, then it shows quotes. (D50, D60)
- **S15.6** [view, print] Given a ticker was removed from the config, a refresh then succeeded, and the ticker is added back with no network, when the view is drawn, then the row shows no data. (D50)
- **S15.7** [view, print] Given a cache file emptied or filled with garbage, when the tool starts, then no message about the cache is shown. (D48)
- **S15.8** [view, print] Given a cache folder that cannot be written, when the tool runs, then one warning says the quotes will not be kept. (D50)
- **S15.9** [print] Given a cache folder that cannot be written, when `brl-stocks print` is run, then no request is sent to the source. (D60)
- **S15.10** [view] Given a cache folder under the home folder that cannot be written, when the view is drawn, then the warning in the status line names the folder with the home folder written `~`. (D95)

### S16 — Colour

As the user, I want gains and losses coloured from my terminal's palette, and plain text where colour does not belong, so that the tool fits my setup and my pipes. (D25)

- **S16.1** [view, print] Given a row whose day change is positive, when it is drawn on a terminal, then the change is in the terminal palette's green. (D25)
- **S16.2** [view, print] Given a row whose day change is negative, when it is drawn on a terminal, then the change is in the terminal palette's red. (D25)
- **S16.3** [view, print] Given `NO_COLOR` is set, when the view or the one-shot output is drawn, then it contains no colour codes. (D25)
- **S16.4** [view, print] Given colour is off, when a row is drawn, then the day change still starts with `+` or `-`. (D50)

### S17 — The data source can be replaced

As the user, I want quotes to come from a free source that needs no key, and to be able to change the source later, so that a broken source is not the end of the tool. (D5, D7, D17)

- **S17.1** [view, print] Given a fresh machine with only a config file, no key and no account, when the tool runs, then it shows quotes. (D5, D7)
- **S17.2** [view, print] Given a stand-in source put in place of the default one, when the tool runs with an unchanged config file, then the same rows are shown with the stand-in's quotes. (D7, D17)
- **S17.3** [view, print] Given the first version, when its config file and flags are listed, then none of them selects a data source. (D50)
- **S17.4** [view, print] Given the help text, when it is read, then `BRL_STOCKS_SOURCE_URL` does not appear in it. (D70)

### S18 — Command line

As the user, I want the usual flags to work and a mistyped one to be refused, so that the tool behaves like other command-line tools. (D50)

- **S18.1** [view, print] Given `--version`, when the tool is run, then it prints the version and exits with status 0. (D50, D77)
- **S18.2** [view, print] Given `--help`, when the tool is run, then it prints the usage and exits with status 0. (D50, D77)
- **S18.3** [view, print] Given a flag the tool does not know, when the tool is run, then it exits with status 2. (D50, D77)
- **S18.4** [view, print] Given a flag the tool does not know, when the tool is run, then a usage message is on the error stream. (D50, D77)
- **S18.5** [view] Given no terminal (the output is redirected), when `brl-stocks` is run with no command, then it exits with status 2. (D94)
- **S18.6** [view] Given no terminal (the output is redirected), when `brl-stocks` is run with no command, then one line on the error stream names `brl-stocks print`. (D94)

## 9. Quality requirements

| # | Requirement | Number and how it is measured | Surfaces | Source |
|---|---|---|---|---|
| QR1 | Capacity | Every criterion in section 8 holds with 20 tickers and 3 summary entries. Measured by running the acceptance tests with a 20-ticker config | view, print | D8, D11 |
| QR2 | The view starts with data | With a cache present, rows show cached quotes within 300 ms of launch. Measured from process start until the rows are on screen, 20 tickers, median of 10 runs | view | D20, D50 |
| QR3 | A one-shot call on a fresh cache is cheap | It sends zero requests (D23) and ends within 100 ms (D50). Measured with a counting stand-in source and wall time, 20 tickers, median of 10 runs | print | D23, D50 |
| QR4 | Request load in normal use | With the view open and `brl-stocks print` called every 2 seconds for 10 minutes, open market, 60-second interval, 23 entries: at most 345 requests in total (15 per entry: one per interval, and half as many again for overlap between instances), all instances together. Measured with a counting stand-in source | view, print | D14, D20, F3, D49 |
| QR5 | Request load during a block | From the first refused answer, at most 10 requests in any 10 minutes while the source keeps refusing, all running instances together. Measured with a stand-in source that refuses everything, the view open and `brl-stocks print --refresh` called every 2 seconds | view, print | D38, F3, F11, D49 |
| QR6 | Freshness while healthy | With an open market and a source that answers, no row's last successful fetch is older than two refresh intervals. Measured by sampling the view's rows every 10 seconds for 10 minutes: no stale mark | view | D14, D40 |
| QR7 | The config is never touched | After any run, the config file is byte-identical and a symbolic link in its path still points to the same target. Measured by checksum and link target before and after each acceptance test | view, print | D29, D35 |
| QR8 | No history is kept | The cache holds at most one quote per ticker; its size after 8 hours of running is within 10% of its size after the first full refresh | view, print | D20, D50 |
| QR9 | Nothing personal in the repository | The published repository holds no key, token or personal watchlist. Measured by review before publishing | none: the published repository | D1 |
| QR10 | Free and keyless | The tool shows quotes with no paid plan, key or account. Measured by S17.1 | view, print | D5, D7 |
| QR11 | Keys answer during a fetch | A key press changes the screen within 100 ms, also while a refresh is running. Measured with a stand-in source that answers after 4 seconds, so that the fetch is answered and not given up (D100) | view | D50, D100 |
| QR12 | Privacy on the wire | The tool contacts only its data source, and sends it nothing about the user beyond the symbols requested. Measured by recording the network traffic of a run | view, print | D1, D50 |

## 10. Design handover

Not requirements of this spec. Input for the design phase.

### Inputs: technical decisions already in the log

| Decision | Source |
|---|---|
| Written in Go | D22 |
| Bubble Tea with Lip Gloss draws the full-screen view; version 2 | D27, D58, F18 |
| The config is parsed as TOML with one small dependency | D28 |
| Yahoo's unofficial chart endpoint is the default source, behind a provider interface | D7, F2, F13 |
| The fetching and formatting core is separate from the full-screen interface | D6 |
| The cache is a small on-disk store of the last quotes | D20 |
| The tool must work through a symlinked config folder and never replace the link | D29, F16 |
| Providers that need a key are bring-your-own-key; no secrets in the repository | D1 |
| Installed with `go install` from the repository, Go managed by mise | D36 |
| The converged design points C1–C17 | D53 |
| Cache coordination: a short lock around each write, per-row claims, the fetch outside the lock | D54 |
| A provider call takes one ticker; the tool stops the parallel requests on a refusal | D55 |
| The block record is kept in a file of its own | D56 |
| No step layer between the UI library and the logic | D57 |
| The design is in `docs/design.md` | D63 |
| Verification drives the full-screen view through a pseudo-terminal and one small terminal-emulator library, used by verification only | D86 |
| One request is given up after 5 seconds (design C16) | D100 |

### Questions: technical choices the behaviour forces

Answered by the design run; see `docs/design.md`. One remains:

| Question | Must serve |
|---|---|
| Q62 — How are open, closed and holidays decided, how is the end of the period after the close recognised, and how many checks may a closed or holiday market cost? Waits on research (F14, F26, the closing-quote measurement) | 6.1, S5.3–S5.9, S13.5 |

## 11. Proposals

Additions of this spec that the log does not decide. Open ones are not requirements yet.

| # | Proposal | Where | Status |
|---|---|---|---|
| P1 | The one-shot command is `brl-stocks print`; the flag that forces a fetch (D23) is `--refresh`. (`--json` is D18.) | 7.1, S13 | settled as D41 |
| P2 | Config keys: `refresh_seconds`, `summary` (names `IBOV`, `SP500`, `USDBRL`; `[]` hides the line), top-level `tickers` (shown first, no header), `[[group]]` with `name` and `tickers`. This reads "configurable" in D11 narrowly: which of the three entries, and in what order | 7.2, S2, S4 | settled as D42 |
| P3 | JSON shape: `schema_version`, `state`, `generated_at`, `error`, `summary`, `groups` of `rows`; numbers as numbers, times UTC RFC 3339, `null` for missing; row `status` of `fresh`, `stale`, `not_found`, `no_data`; no intraday series | 7.5, S14 | settled as D43 |
| P4 | One-shot result: `state` is `ok`, `partial`, `stale` or `error`; exit status 0 whenever the command line and config were valid; exit status 2 for usage and config errors, with a valid `error` document under `--json` | 7.6, S14 | settled as D44 |
| P5 | An invalid config at start ends the tool with a message and status 2; an invalid edit while the view is open keeps the last valid watchlist and shows the problem in the error line | 6.4, 7.2, S11 | settled as D45 |
| P6 | One number format for both currencies (dot for decimals, comma for thousands). **Changed by the user:** each row uses its currency's own format | 7.3 | settled as D46, changed |
| P7 | The status line gives each market's state (`B3 open · US closed`); rows of a closed market carry no other mark, and are stale only if their last fetch failed | 7.3, S5.5 | settled as D47 |
| P8 | A cache that cannot be read is treated as absent, with no message. (The log holds this as item B4.) | 7.7, S15.4, S15.7 | settled as D48 |
| P9 | Request limits: normal use at most 15 requests per entry in 10 minutes at a 60-second interval, all instances together (QR4); during a block at most 10 requests in any 10 minutes, all instances together (QR5) | QR4, QR5 | settled as D49 |
| P10 | `--help` and `--version` exist; an unknown command or flag is a usage error with status 2 | 7.1 | settled as D50 |
| P11 | A config change is picked up within 5 seconds of saving, with no key press; quotes of the rows that remain are kept, and new rows are fetched at once | 6.4, S11.3, S11.6 | settled as D50 |
| P12 | If the config file is removed while the view is open, the last loaded watchlist stays and the error line says so | 6.4, S11.14 | settled as D50 |
| P13 | An unknown config key gives a warning; the tool runs | 7.2, S11.12 | settled as D50 |
| P14 | Tickers are case-insensitive; rows show them in upper case without the market prefix | 7.2, S3.7 | settled as D50 |
| P15 | A ticker repeated in one group is shown once; a ticker in two groups is shown in both and fetched once | 7.2, S4.4 | settled as D50 |
| P16 | More than 20 tickers are all shown, with a warning | 7.2, S11.13 | settled as D50 |
| P17 | `refresh_seconds` has a minimum of 10; lower values are treated as 10, with a warning | 7.2, S5.10 | settled as D50 |
| P18 | The built-in example is `PETR4`, `VALE3`, `ITUB4`, `AAPL`, `MSFT` with the default summary; the note on where to put a config is in the status line (view) or on the error stream (one-shot). `--config` naming a missing file is an error with status 2: this reads D35 as covering the default path only | 6.4, 7.7, S12.4, S12.5 | settled as D50 |
| P19 | Screen order: summary line, column header, rows under group headers, error line, status line | 7.3 | settled as D50 |
| P20 | On a narrow terminal, columns go in the order sparkline, day low and high, name, day change value; the view follows a resize | 7.3, S1.6 | settled as D50 |
| P21 | Quote time is local time `HH:MM`, or `DD/MM` when not from today | 7.3, S1.7 | settled as D50 |
| P22 | The stale mark is `!` and the age since the last successful fetch (`!45s`, `!12m`, `!3h`, `!2d`) | 7.3, S6.10 | settled as D50 |
| P23 | A row with no data shows `—` in its data columns; a not-found row shows `—` beside "not found" | 6.2, 7.3, S1.5 | settled as D50 |
| P24 | The day change always has a sign; zero is uncoloured; colour covers the change value, the percent and the sparkline, S1.10, S1.11, S1.13, S2.8 | 7.3, S16.4 | settled as D50 |
| P25 | The sparkline covers the regular session of the current or last trading day, 10 characters wide, earliest on the left | 7.3, S10.2 | settled as D50 |
| P26 | Sort cycle: file order, day change percent (highest first), ticker A to Z. Sorting is inside each group; rows without a quote go last; the status line names the sort; it is not remembered between runs | 7.3, S9 | settled as D50 |
| P27 | Ctrl+C quits like `q` (an addition to the keys of D32); quitting exits with status 0 and restores the terminal; any key closes the help; a scroll key moves one row | 7.3, S1.3 | settled as D50 |
| P28 | Waits: after a row's n-th failure in a row, its next automatic attempt is after interval × 2^(n−1), at most 30 minutes; after a refusal the wait is 5 minutes, doubling to at most 30; a success resets both. Waits and blocks hold for the view and for one-shot calls alike | 6.2, 6.3, 6.5, S6.4, S7.5, S13.16 | settled as D50 |
| P29 | A refusal by the source (rate limit, access denied) is a block; every other failure (no connection, timeout, server error, unreadable answer) is a network error. Which answers the default source gives when it refuses is a fact to look up (section 12) | 6.3 | settled as D50 |
| P30 | The error line gives cause, number of rows affected and time of the next attempt; it shows the first of config problem, block, failed fetches; not-found rows do not raise it. Warnings go to the status line (view) or the error stream (one-shot) | 7.2, 7.3, S6.9, S8.5, S11.18 | settled as D50 |
| P31 | After a market closes, its rows are refreshed until the closing quote has arrived, for at most 30 minutes. This is an exception to D15, and is point C7 of the design run (Q38). The 30 minutes rest on a fact to measure (section 12) | 4, 6.1, S5.3, S5.8 | settled as D50 |
| P32 | A row with no quote is fetched even when its market may be closed (an exception to D15) | 6.5 | settled as D50 |
| P33 | `r` and `--refresh` fetch rows of closed markets too (an exception to D15) and start within 2 seconds. They skip rows attempted less than 5 seconds ago, and `--refresh` obeys a block as `r` does (both limit the forced fetch of D23). Not-found rows keep their hourly wait (D39) | 6.3, 6.5, S7.1, S7.2, S7.6, S7.7, S13.10 | settled as D50 |
| P34 | Summary entries show value and day change percent; indices without a currency symbol; USD/BRL with four decimals. Ibovespa follows B3's hours, S&P 500 the US hours, USD/BRL its own hours as the source reports them (what it reports is a fact to look up, section 12), S2.7 | 4, 6.1, 7.3, S2.1 | settled as D50 |
| P35 | Only regular-session prices are shown. Whether the default source's last price can include pre-market or after-hours trades is a fact to look up (section 12) | 2, S5.11 | settled as D50 |
| P36 | The one-shot table has the summary line, a header row, the standard columns without the sparkline, group names on their own lines, and a last line with the market states; failure causes and warnings go to the error stream | 7.4, S13.8, S13.13–S13.15 | settled as D50 |
| P37 | One-shot freshness is judged per row; a call waits at most 10 seconds for its fetches | 6.5, 7.6, S13.9 | settled as D50 |
| P38 | Timing: cached rows on screen within 300 ms of launch; a one-shot call on a fresh cache within 100 ms; a key press shown within 100 ms | QR2, QR3, QR11 | settled as D50 |
| P39 | The tool contacts only its data source and sends nothing about the user beyond the symbols | QR12 | settled as D50 |
| P40 | Interface text is in English | 7.3 | settled as D50 |
| P41 | The first version ships one data source and no setting to choose another | 2, S17.3 | settled as D50 |
| P42 | If the cache folder cannot be written, the tool runs and warns once | 7.7, S15.5 | settled as D50 |
| P43 | `XDG_CONFIG_HOME` and `XDG_CACHE_HOME` are honoured when set (an addition to the fixed paths of D29) | 7.2, 7.7, S11.15 | settled as D50 |
| P44 | Tolerances for timed criteria: one refresh more or fewer in 10 minutes (S5.1, S5.2); refresh resumes within one interval of the open (S5.7); the cache may vary by 10% in size (S15.3, QR8) | S5.1, S5.2, S5.7, S15.3, QR8 | settled as D50 |
| P45 | The cache drops the quotes of tickers that left the config | S15.6 | settled as D50 |
| P46 | In the summary line, each entry uses the format of its market: Ibovespa the R$ format (`158.432`), S&P 500 the US$ format (`6,812.40`), and USD/BRL the R$ format (`5,4321`) | 7.3 | settled as D51 |
| P47 | The day change percent of a row follows the number format of the row's currency (`+0,81%` on R$ rows, `-0.48%` on US$ rows). D46 covers "each row's numbers"; the percent is the one number of the row that is not an amount in its currency, and the log marks its inclusion as a reading "to confirm". (The summary line's percents are already covered by D51) | 7.3, S1.11, S1.12 | settled as D64 |
| P48 | Decimals: the change value and the percent have two; the Ibovespa is shown in whole points and the S&P 500 with two decimals. For the indices this turns the examples of D51 into a rule. (Prices have two by D46, USD/BRL four by D50) | 7.3, 7.4, S1.10–S1.12, S2.5, S2.6, S2.8, S13.19 | settled as D65 |
| P49 | Formatting details: only the last price carries the currency symbol, followed by one space; values are rounded half away from zero to the decimals shown; a minus sign is the plain hyphen; a change of zero, or one that rounds to zero, reads `+0,00` or `+0.00`; numbers in the JSON output are neither rounded nor formatted | 7.3, 7.5, S1.12–S1.15, S14.12 | settled as D66 |
| P50 | The summary entries are labelled `IBOV`, `S&P 500` and `USD/BRL`, in the view and in the one-shot table | 7.3, S13.13 | settled as D71 |
| P51 | Alignment: ticker, name and quote time start at the same position in every row; last price, change value, percent, low and high end at the same position. This defines "aligned" in D18 | 7.3, 7.4, S13.20 | settled as D71 |
| P52 | The cause on the error line and the error stream is one of four fixed phrases: `source unreachable`, `source is rate-limiting`, `source refused access`, `source answer not understood` | 7.3, S13.15, S13.21 | settled as D71 |
| P53 | In the one-shot table, a quote time not from today reads `DD/MM`, as in the view. This reads D50 (P21), which is worded without a surface, as holding for both | 8 (surfaces), S1.7 | settled as D75 |
| P54 | In one-shot, the line on the error stream for a failed fetch or a block gives what the view's error line gives: it starts with the cause phrase (D71), then the number of rows not updated and the time of the next attempt; with several problems, one such line, chosen by the view's order (block, then failed fetches). 7.4 says only that the cause goes there; 6.3 says "the status line" for a forced one-shot call during a block, and this moves it to the error stream | 6.3, 7.4, S6.3, S6.9, S7.4 | settled as D76 |
| P55 | `--help`, `--version` and the usage error for an unknown flag work both with and without the `print` command. 7.1 lists the flags without saying on which commands | 7, 7.1, S18 | settled as D77 |
| P56 | The time of the next attempt, on the view's error line and on the one-shot error stream, reads `HH:MM` in the machine's local time, like a quote time from today. The 7.3 example shows it so, but examples are illustrative | 7.3, 7.4, S6.9, S7.4 | settled as D79 |

## 12. Open and unverified

| Item | Affects | Blocks the design or the build? |
|---|---|---|
| Whether the trading period in the source's answer is right on a market holiday (F14, backlog; next check on US Thanksgiving, 2026-11-26) | 6.1, S5.9 | Design: no, but it needs a fallback. Build: S5.9 cannot pass until it is known |
| Q62: how open, closed and holidays are decided, and what a closed or holiday market may cost in requests (log: waits on research) | 6.1, S5.3–S5.9; 6.1 says a holiday costs none | Build: yes, for the market-hours part only |
| Which answers the default source gives when it rate-limits or refuses (F3 names one; an unknown ticker is told apart, F27; backlog) | D50, 6.3 | Build: yes, for the source part |
| When B3's closing quote arrives after the close (backlog). A first sample shows it later than 30 minutes (F30) | D50, 6.1, S5.3, S5.8, S13.5 | Build: yes, for the market-hours part; the 30 minutes will be revisited in Q62 |
| What the default source reports for USD/BRL at weekends; on a weekday it reports the whole day as its session (F29) | D50, 6.1 | No |
| Whether the default source's price can include pre-market trades; after-hours trades do not move it (F28, one sample) | D50, S5.11 | No |
| Legal frame for showing and redistributing market data (backlog) | Publishing (D1, D24) | No; blocks publishing only |
| Free-tier numbers of other US sources (F7, backlog) | A future second source | No |
| B3 support in the closest existing tool was never tested (F9) | Nothing in this spec; D21 stands | No |
| D34 and D36 are delivery work outside the product: the dev-env wiring and the install method | Nothing in this spec; D34 is done only when the user asks | No |

## 13. Coverage

Every decision in the log, and where the spec realises it.

| Decision | Realised in |
|---|---|
| D1 | 1, 2, QR9, QR12; the bring-your-own-key note is design (section 10) |
| D2 | 1, 2, 4 (Market), S3 |
| D3 | 1, 2 (goals and non-goals), 4 (Watchlist invariants), S1 |
| D4 | 1, 2 (non-goals), 5 |
| D5 | 2, S17.1, QR10 |
| D6 | 1, 2, 7, 7.1, S1, S13, S18; the separation of the core is design (section 10) |
| D7 | 2, 5, S17; the interface is design (section 10) |
| D8 | 1, 2 (non-goals), 7.2, S11.13, QR1 |
| D9 | 4 (Quote), 7.3, S1.2, S14.2 |
| D10 | 2, 4, 7.3, S3.1, S3.3, S3.6 |
| D11 | 4, 7.3, S2, QR1 |
| D12 | 2, 4, 6.4, 7.2, S11, S11.20 |
| D13 | 2 (non-goals), 7.3, S10 |
| D14 | 4, 6.1, 7.2, S5.1, S5.2, QR4, QR6 |
| D15 | 4 (Market), 6.1, 6.5, 7.3, S5.3–S5.9, S13.5 |
| D16 | 4 (Group), 7.2, 7.3, S4, S13.8 |
| D17 | 2, 4 (Ticker), 5, S3, S17.2 |
| D18 | 2 (non-goals), 7.1, 7.4, 7.5, S13.1, S14 |
| D19 | 2, 6.2, 6.3, 7.3, S6, S13.6, S7.10 |
| D20 | 2 (non-goals), 4 (Quote invariants), 7.7, S1.4, S15, QR2, QR8; the store itself is design (section 10) |
| D21 | 2 (non-goals); scope. Its borrowed one-shot contract is the basis of D44 |
| D22 | design, see section 10 |
| D23 | 6.5, 7.1, S13.2–S13.4, QR3 |
| D24 | process (licence of the repository) |
| D25 | 7.3, 7.4, S10.5, S10.6, S13.7, S16 |
| D26 | 1, 7.1, 7.2, 7.7 |
| D27 | design, see section 10 |
| D28 | 7.2; the parser is design (section 10) |
| D29 | 7.1, 7.2, 7.7, S11.1, S11.2, S11.5–S11.7, S15.2, QR7 |
| D30 | 4 (Ticker), 7.2, S3.1–S3.5 |
| D31 | 4, 7.3, S9.1–S9.4 |
| D32 | 2 (non-goals), 6.3, 7.3, S1.3, S7.1, S7.9, S9.6–S9.9 |
| D33 | 4 (Watchlist invariants), 5 |
| D34 | process (delivery work in another repository); see section 12 |
| D35 | 2 (non-goals), 6.4, 7.7, S11.8, S12, QR7 |
| D36 | process; the install method is in section 10 |
| D37 | process |
| D38 | 6.3, 6.5, 7.3, S7.2–S7.4, QR5 |
| D39 | 6.2, 6.3, 6.4, 6.5, 7.3, S7.8, S8 |
| D40 | 3, 6.2, 7.3, S5.6, S6.2, S6.5, S6.7, S6.8, QR6 |
| D41 | 7.1, S13.1, S13.4 |
| D42 | 7.2, S2.2, S2.3, S4.3, S5.2 |
| D43 | 7.5, S14.3, S14.9–S14.11 |
| D44 | 7.6, S14.4–S14.8; its status 2 extended to the view without a terminal by D94 (7.1, S18.5) |
| D45 | 6.4, 7.2, 7.6, S11.9–S11.11, S11.16, S14.8 |
| D46 | 7.3, 7.4, S1.8–S1.10, S1.14, S1.15, S13.17, S13.18 |
| D47 | 7.3, S5.5 |
| D48 | 7.7, S15.4, S15.7 |
| D49 | QR4, QR5 |
| D50 | The 36 routine proposals P10–P45; each is listed in section 11 with the places that realise it. Also S18 (7.1) |
| D51 | 7.3, 7.4, S2.5–S2.8, S13.19 |
| D52 | process (approval of this spec) |
| D53–D58 | design, see section 10 and `docs/design.md` |
| D59 | 2 (non-goals) |
| D60 | 6.5, 7.7, S15.5, S15.9 |
| D61 | No change: D50's rule on tickers that left the config stands (S15.6) |
| D62 | 6.3, S7.1, S7.2, S7.9 |
| D63 | process (approval of the design document) |
| D64 | 7.3, S1.11, S1.12 |
| D65 | 7.3, 7.4, S1.10–S1.12, S2.5, S2.6, S2.8, S13.19 |
| D66 | 7.3, 7.5, S1.12–S1.15, S14.12 |
| D67 | process (approval of this spec as updated) |
| D68 | 2 (non-goals) |
| D69 | process (approval of the design document) |
| D70 | 7.1, S17.4; its use is design (section 10 and `docs/design.md`) |
| D71 | 7.3, 7.4, S13.13, S13.15, S13.20, S13.21 |
| D72 | process (approval of this spec and of the design document, as updated) |
| D73 | 6.5, S13.22 |
| D74 | 7.3, S13.24 |
| D75 | 7.4, S1.7 |
| D76 | 6.3, 7.4, 8 (surfaces), S6.9, S7.4 |
| D77 | 7, 7.1, S18 |
| D78 | process (approval of this spec, as updated) |
| D79 | 7.3, 7.4, S6.9, S7.4 |
| D80 | process (approval of this spec, as updated) |
| D81 | 7.3, S6.11 |
| D82 | process (how the build is run and lands) |
| D83 | process (how the build is run and lands) |
| D84 | process (how the build is run and lands) |
| D85 | process (how the build is run and lands) |
| D86 | design, see the handover |
| D87 | process (how the build is run and lands) |
| D88 | process (how the build is run and lands) |
| D89 | process (how the build is run and lands) |
| D90 | process (how the build is run and lands) |
| D91 | process (how the build is run and lands) |
| D92 | 7.3, S9.11 |
| D93 | 7.3, S9.6, S9.7 |
| D94 | 7.1, S18.5, S18.6 |
| D95 | 7.3, S12.6, S12.7, S12.8, S15.10 |
| D96 | 7.3, S2.9, S2.10 |
| D97 | process |
| D98 | 6.3 |
| D99 | 6.3 |
| D100 | QR11; design (C16), see the handover |
| D101 | 7.3 |
| D102 | 7.2 |
| D103 | process |
| D104 | 7.3, S10.11, S10.12 |
| D105 | 7.3, S10.12–S10.14 |
| D106 | 7.3, S10.15–S10.19 |
| D107 | 7.3, S10.11, S10.20–S10.22 |
