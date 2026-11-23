# Existing terminal stock monitors (competitors and substitutes)

Research date: 2026-11-23. Scope: terminal stock monitors, one-shot CLIs and status-bar modules, judged against the decisions in `grilling-log.md` (D1 to D6): personal tool, B3 and US, watchlist first, free data with about 15 minutes of delay, three ways to run (always-open pane, on demand, one-shot print).

## 1. Method

- Read `research/grilling-log.md` and `research/market-data-sources-2026-11.md` first. Provider pricing and limits were not re-researched; this report only records which source each tool uses and how it broke.
- Discovery: 4 web searches (English and Portuguese) plus GitHub repository search.
- Metadata (stars, licence, language, last push, archived flag, latest release) from the unauthenticated GitHub API: **24 requests in total**, with a generic User-Agent, on 2026-11-23.
- Features: read each tool's README (raw file from GitHub), one wiki page, one source file (`cmd/root.go` of ticker, to confirm its subcommands) and 12 issue threads through the GitHub web pages.
- Nothing was installed, cloned or run. Every "works with B3" statement is therefore from documentation or issue threads, not from a test. Statements I inferred (for example "uses Yahoo, so `.SA` symbols should resolve") are marked **[unverified]**.
- Raw downloads (READMEs, API responses) were kept in the scratch directory and deleted at the end. Issue content is reported in aggregate, without usernames.
- Limits of the method:
  - "Last push" is GitHub's `pushed_at`, which counts a push to any branch, not only the default branch.
  - Issue searches were keyword searches and include false positives; counts are indicative.
  - Small tools (under about 50 stars) were only read at README level.
- Confidence levels: **High** = GitHub API value or the project's own README or source today. **Medium** = issue thread summary, or a README statement about behaviour I could not test. **Low** = inference or memory.

## 2. Findings

### 2.1 Tools found

Full-screen monitors:

| Tool | What it is |
|---|---|
| [ticker](https://github.com/achannarasappa/ticker) | Watchlist and positions monitor. The reference tool of the category |
| [mop](https://github.com/mop-tracker/mop) | Dense quote table with a market summary header. Oldest tool (2013) |
| [tickrs](https://github.com/tarkah/tickrs) | Chart-first monitor (one tab per symbol, plus a summary mode) |
| [stonks](https://github.com/ericm/stonks) | Prints charts of saved symbols and exits. Not interactive |
| [stocksTUI](https://github.com/andriy-git/stocksTUI) | Multi-tab app: watchlists, news, history, options, FRED data |
| [mkt](https://github.com/stxkxs/mkt) | Nine-tab dashboard with alerts, portfolio, HTTP endpoint, SSH server |
| [stonks-dashboard](https://github.com/pierridotite/stonks-dashboard) | Themed dashboard for stocks and crypto |
| [cointop](https://github.com/cointop-sh/cointop) | Crypto only. Included for interface ideas. Archived |
| [OpenBB](https://github.com/OpenBB-finance/OpenBB) | Data platform with a command-line interface. Not a watchlist monitor |
| [longbridge-terminal](https://github.com/longbridge/longbridge-terminal) | Client for one broker's platform. Needs that broker's account, so out of scope |

One-shot tools and status-bar modules:

| Tool | What it is |
|---|---|
| [ticker.sh](https://github.com/pstadler/ticker.sh) | Shell script that prints quotes. Declared defunct by its README |
| [terminal-stocks](https://github.com/shweshi/terminal-stocks) | `curl terminal-stocks.dev/<TICKER>` service and a Node CLI |
| [tstock](https://github.com/Gbox4/tstock) | Prints one candlestick chart. Needs an Alpha Vantage key |
| [tickerbar](https://github.com/mryll/tickerbar) | Waybar module with JSON output and market-hours logic |
| [tmux-ticker](https://github.com/Brutuski/tmux-ticker) | tmux plugin. US only, scrapes MarketWatch |
| [polystock](https://github.com/zack-ashen/polystock), [PolyStocks](https://github.com/dreamxinxcode/polystocks) | Polybar scripts on the `yahoo_fin` Python package. Both stale |

Seen in search results but not assessed: [tui-ticker](https://github.com/arseem/tui-ticker) (0 stars), [feedtui](https://github.com/muk2/feedtui) (15 stars), [alphai-tui](https://docs.rs/alphai-tui), [stonks-cli](https://pypi.org/project/stonks-cli/), [stop](https://github.com/axxml/stop), [stock-tui](https://github.com/Folgerjun/stock-tui) (Chinese A-shares only).

**Brazilian or B3-focused terminal tools: none found.** Four GitHub repository searches (`bovespa cli`, `b3 stocks terminal`, `brapi cli`, `ibovespa tui`) and two Portuguese web searches returned only data libraries and parsers ([b3quant](https://github.com/renves/b3quant), [b3parser](https://github.com/diogolr/b3parser), [node-bovespa](https://www.github.com/nihey/node-bovespa)), one zero-star script ([StockScreener](https://github.com/Blitk/StockScreener), brapi plus Matplotlib), and web dashboards. Confidence: Medium (absence of evidence; a small unlisted project may exist).

### 2.2 Comparison table

Stars, licence, last push and latest release are from the GitHub API on 2026-11-23 (High confidence). "B3" means: do `.SA` symbols work without changes.

| Tool | Language, TUI framework | Data source | B3 (`PETR4.SA`) | Stars | Licence | Latest release | Last push | Maintained? | One-shot / JSON |
|---|---|---|---|---|---|---|---|---|---|
| ticker | Go, Bubble Tea | Yahoo (default), Coinbase for crypto | **Yes**, confirmed by the maintainer in 2021 ([#162](https://github.com/achannarasappa/ticker/issues/162)) | 6,270 | GPL-3.0 | v5.3.0, 2026-06-21 | 2026-06-28 | Yes, slowly: 3 fix PRs from Aug to Oct 2026 still open | `ticker print` (JSON or CSV), **holdings only**, not the watchlist |
| mop | Go, termbox [unverified] | Yahoo | Likely [unverified]; has a `market` property from the suffix | 2,215 | MIT | No releases | 2026-10-22 | Yes, low volume | No. Requested and declined ([#88](https://github.com/mop-tracker/mop/issues/88)) |
| tickrs | Rust, tui-rs | Yahoo | Quote likely [unverified]; **1W to 6M charts are empty for non-US symbols** ([#184](https://github.com/tarkah/tickrs/issues/184)) | 1,714 | MIT | v0.15.0, 2025-12-15 | 2026-05-19 | Barely: issues from 2024 and 2025 unanswered | No |
| stonks | Go, no TUI (prints charts) | Yahoo | Likely [unverified] | 572 | GPL-3.0 | v1.0.10, 2022-03-28 | 2023-05-22 | No | Is one-shot by nature; no JSON |
| stocksTUI | Python, Textual | Yahoo through `yfinance`; FRED with a key | Likely [unverified] ("all symbols follow Yahoo format") | 160 | GPL-3.0 | v0.1.0-b14, 2026-07-09 | 2026-10-24 | Yes | "CLI mode" `-o <list>` prints a list; JSON not documented |
| mkt | Go, Bubble Tea v2 | Yahoo (stocks), Coinbase WebSocket (crypto) | Not mentioned; README is US only [unverified] | 11 | MIT | v0.3.0, 2026-09-19 | 2026-11-21 | Yes, very young (99 commits) | `mkt watch` streams to stdout; JSON only through a local HTTP server |
| stonks-dashboard | JavaScript, blessed [unverified] | Yahoo chart endpoint, CoinGecko | Likely [unverified]; README names `.L` and `.PA` | 246 | MIT | not checked | 2026-09-07 | Yes | No |
| cointop | Go, gocui [unverified] | CoinGecko, CoinMarketCap | Not applicable | 4,384 | Apache-2.0 | v1.6.10, 2021-11-07 | 2024-04-07 | **Archived** | Not checked |
| OpenBB | Python, command-line (not a TUI) | Many providers | Not checked | 75.7k (web page) | Apache-2.0 | not visible | not checked | Yes | Not a monitor |
| ticker.sh | Shell, `curl` and `jq` | Yahoo | Not applicable | 516 | MIT | none | 2024-01-12 | **Defunct** per README | Was one-shot only |
| terminal-stocks | JavaScript, CLI and `curl` service | Yahoo | Likely [unverified]; examples use `.NS` | 88 | MIT | not checked | 2026-04-23 | Low | One-shot; `--json` and `--csv` for the market summary |
| tstock | Python, prints a chart | Alpha Vantage (key) | Unknown | 637 | GPL-3.0 | none | 2024-10-07 | No | One-shot chart |
| tickerbar | Rust, no TUI (bar module) | CNBC (stocks, undocumented), CoinGecko, Frankfurter, data912 (Argentina), Finnhub (key) | **No B3 provider** documented | 11 | MIT | not checked | 2026-10-22 | Yes | **Yes**: one line of Waybar JSON, and `--output json` with a versioned schema |
| tmux-ticker | Shell, tmux plugin | MarketWatch scraping | **No**: "only US markets" | 43 | MIT | not checked | 2024-02-26 | No | One value per format string |
| polystock | Python, Polybar script | `yahoo_fin` | Unknown | 22 | MIT | none | 2024-03-01 | No | One-shot text |

Sources for the table: each tool's README at the linked repository; for ticker also its [releases page](https://github.com/achannarasappa/ticker/releases) and `cmd/root.go` (the `print` command is described there as "Prints holdings"); for tickrs its [config wiki](https://github.com/tarkah/tickrs/wiki/Config-file). The OpenBB API call returned a redirect (the repository has moved), so its row comes from the web page only.

### 2.3 Features of the main tools

| Feature | ticker | mop | tickrs | stocksTUI | mkt |
|---|---|---|---|---|---|
| Watchlist columns | Name, price, change, change %; optional tags (currency, exchange, quote delay), fundamentals (open, previous close, day range), positions | Last, change, change %, open, low, high, 52-week low and high, volume, average volume, P/E, dividend, yield, market cap | Price and change beside a chart; summary mode lists all symbols | Price table plus tabs for news, history, options | Price, 24h change, volume, sparkline |
| Groups or multiple lists | Yes: `groups` in the config, Tab cycles | No. Open request since 2026-09 ([#156](https://github.com/mop-tracker/mop/issues/156)); `g` groups by advancing or declining | One tab per symbol | Yes: named lists and tags, managed in the app | Yes: named `watchlists`, `[` and `]` cycle |
| Mixed currencies | Yes: one `currency` setting converts everything, or only the summary. Rates are fetched **once at start**. Minor units (GBp) handled since v5.3.0 | Per-quote currency display, fixed in 2025 ([#138](https://github.com/mop-tracker/mop/issues/138)) | Not documented | Not documented | Not documented; examples are in `$` |
| Index or exchange-rate header | No (add `^BVSP`, `BRL=X` as normal rows) | **Yes**: fixed market summary block at the top [details from the screenshot, unverified] | No | Any Yahoo symbol in a list | A "Macro" tab with a fixed US set |
| Sparklines or charts | No | No | Yes: line, candle, kagi; 1D to 5Y | Yes: history chart | Yes: sparklines, candles, 13 indicators |
| Alerts | No. Requested 2026-07 ([#381](https://github.com/achannarasappa/ticker/issues/381)) | No | No | No | Yes: desktop, webhook, ntfy, Pushover |
| Positions | Yes: cost-basis lots | No | No (requested, [#120](https://github.com/tarkah/tickrs/issues/120)) | Options positions | Yes: transactions, tax lots, risk statistics |
| Watchlist configuration | YAML (`~/.ticker.yaml`) or `-w` flag. No in-app editing | JSON profile (`~/.moprc`), **edited in the app** with `+` and `-` | YAML; symbols also by `-s` flag | JSON, **edited in the app** | YAML plus `mkt config add/remove`; alerts edited in the app |
| Refresh | Polling, default 5 s | Default 600 s, pause key | Default 1 s | Configurable | Stocks 15 s, crypto streamed |
| Market-hours awareness | Partial: default sort puts closed markets last; `--show-tags` shows quote delay; pre and post market prices | `market` property; pre and post market colouring requested | Pre and post market flag. "Blank screen on market closed" open since 2021 ([#60](https://github.com/tarkah/tickrs/issues/60)) | "Market-aware caching" skips calls on weekends and holidays | Not mentioned |
| One-shot or JSON | Holdings only (see table above) | None | None | List output, format not documented | Stream and HTTP endpoint |

For interface ideas from the rest:

- **cointop**: htop-like table, vim keys, fuzzy search, favourites, sort shortcuts, offline cache, two interchangeable data APIs (per its README).
- **tickerbar** is the best model for the one-shot mode. Per its README:
  - any failure (dead API, rate limit, bad config) still yields valid JSON and exit code 0, so the bar never breaks;
  - a `state` field (`ok`, `partial`, `stale`, `error`) and per-row `fresh` or `missing`;
  - closed markets are not polled: the last close comes from a cache and the group is marked closed;
  - `NO_COLOR` is honoured;
  - the JSON document has a `schema_version`.
- **stonks-dashboard**: when the API is unreachable it shows cached values tagged `[CACHE]`.
- **mkt**: a symbol no provider can serve is named on stderr and flagged in the interface instead of silently never pricing; its HTTP `/quotes` endpoint feeds tmux and a shell prompt.

### 2.4 Gaps

**What users ask for and do not get** (from open issues):

- **One-shot output of the watchlist.** In ticker, `print` covers only lots of the default group; requests to print watchlist quotes and other groups have been open since 2021 and 2022 ([#180](https://github.com/achannarasappa/ticker/issues/180), [#219](https://github.com/achannarasappa/ticker/issues/219)). In mop the request was discussed in 20 comments and closed with "use `curl` and `jq`" ([#88](https://github.com/mop-tracker/mop/issues/88)).
- **Alternate data sources.** Open in ticker since 2021-02 on the "future-release" milestone ([#64](https://github.com/achannarasappa/ticker/issues/64)); open in tickrs since 2021 ([#131](https://github.com/tarkah/tickrs/issues/131)).
- **Alerts** (ticker #381), **multiple lists** (mop #156), **local caching** (tickrs [#138](https://github.com/tarkah/tickrs/issues/138)), **number formatting** such as thousands separators and fixed decimals (ticker [#204](https://github.com/achannarasappa/ticker/issues/204), [#198](https://github.com/achannarasappa/ticker/issues/198)).
- **Errors shown to the user.** "HTTP response errors are not reported to user" has been open in ticker since 2021 ([#79](https://github.com/achannarasappa/ticker/issues/79)); tickrs users describe an endless loading state with no message ([#176](https://github.com/tarkah/tickrs/issues/176)).
- **tmux flicker** on refresh, open in ticker since 2021 ([#60](https://github.com/achannarasappa/ticker/issues/60)). Relevant to the always-open pane.

**What goes wrong with B3 and other non-US tickers:**

- **Symbol discovery.** The only B3 issue found in any of these trackers is a user typing `PEAB3` instead of `PEAB3.SA`; the answer was "look the symbol up on Yahoo" ([ticker #162](https://github.com/achannarasappa/ticker/issues/162)). No tool adds the suffix or validates the symbol.
- **US assumptions in the code.** tickrs aligns every chart to a US timestamp template, so non-US charts are empty ([#184](https://github.com/tarkah/tickrs/issues/184)). tickerbar applies one calendar per provider, so non-US stocks show "closed" at the wrong times, and it knows no exchange holidays (its README).
- **Currency.** Recurring bugs: GBP against GBp ([ticker #282](https://github.com/achannarasappa/ticker/issues/282)), unsupported currencies shown wrongly ([mop #138](https://github.com/mop-tracker/mop/issues/138)), non-USD lots confusing the valuation ([ticker #223](https://github.com/achannarasappa/ticker/issues/223)). ticker's summary is in USD unless a currency is set, and the exchange rate is read once at start.
- **Region.** Yahoo's consent redirect for European users broke ticker in 2023 and still crashes mop ([mop #152](https://github.com/mop-tracker/mop/issues/152), open since 2026-01). A Brazilian IP is probably not affected [unverified].
- No tool separates FIIs, ETFs and BDRs, and none shows the Ibovespa or USD/BRL by default.

**Does any tool already satisfy the context?** No.

| Requirement | ticker | mkt | tickerbar |
|---|---|---|---|
| B3 and US in one list | Yes | Probably [unverified] | US yes, B3 no |
| Always-open pane with refresh | Yes | Yes | No |
| Launch on demand | Yes | Yes (heavy: seeded with 11 lists, 12 portfolios, 11 alerts) | No |
| One-shot print of the watchlist | **No** (holdings only) | **No** (stream, or HTTP server that must be running) | Yes |
| Market hours for both exchanges | Partial | No | US only |

ticker is the closest for the two interactive modes; tickerbar is the closest for the one-shot mode. Nothing covers all three with B3.

### 2.5 Lessons for a new tool

**Choices that recur in the well-liked tools:**

- A single static binary (Go or Rust) with zero setup and no API key. The three most-starred tools are all like this.
- Yahoo symbols as the user-facing symbol language. Users already know where to look them up.
- A plain-text config in the home or XDG directory. Note the most-reacted open issue in ticker (10 reactions) is about the config file name under `XDG_CONFIG_HOME` ([#40](https://github.com/achannarasappa/ticker/issues/40)): follow XDG from day one.
- Display options as toggles (ticker's `--show-tags`, `--show-fundamentals`) rather than a fixed wide table.
- Groups cycled with one key; sort order switchable; vim keys.
- In-app add and remove of symbols where it exists (mop, stocksTUI) is praised; ticker's file-only approach generates import and editing requests.

**Choices that caused maintenance pain:**

- **Hard dependence on Yahoo's unofficial quote endpoint.** This is the dominant source of bug reports in every Yahoo-based tool. Breakage waves visible in the trackers:
  - 2023-04 to 2023-06: cookie and crumb requirement. ticker took about 33 days and four releases to fix it fully, including the EU consent case ([#247](https://github.com/achannarasappa/ticker/issues/247)). mop had four "Error fetching market data" threads in the same weeks (#106, #108, #112, #113).
  - 2023-08: ticker.sh broke and its maintainer halted the project "until a provider offers free endpoints" ([#45](https://github.com/pstadler/ticker.sh/issues/45)).
  - 2024-03, 2024-09 to 2024-11: new crumb flow. tickrs showed no data for about five months before a fix ([#166](https://github.com/tarkah/tickrs/issues/166)).
  - 2025-02 to 2025-05: HTTP 429 on the crumb request and a limit on symbols per request ([ticker #304](https://github.com/achannarasappa/ticker/issues/304), [#310](https://github.com/achannarasappa/ticker/issues/310), [tickrs #176](https://github.com/tarkah/tickrs/issues/176)).
- Detail that matters for the design: the breakage is concentrated on the **batch quote endpoint with cookie and crumb**. The **chart endpoint** kept working in 2023 at one request per ticker (ticker.sh #45), which matches fact F2. It is not immune: a 2026-02 comment in tickrs #176 reports the chart endpoint rejecting a non-browser User-Agent. A configurable User-Agent was requested there.
- **No provider layer.** ticker's maintainer wrote in 2023 that "some kind of abstraction layer should help mitigate these sorts of issues"; the request is still open. Projects without it either stall (ticker.sh) or patch in a hurry.
- **Scraping a website** (tmux-ticker on MarketWatch) and **a keyed provider with a tiny quota** (tstock on Alpha Vantage): both tools are now stale.
- **Silent failure.** Spinners or blank screens with no error produce "is this abandoned?" threads (23 comments in tickrs #166).
- **Interactive-only design.** Adding a one-shot mode later proved hard enough that both ticker and mop left it undone for years. D6 avoids this if the core is separate from the start.
- **Scope growth.** stocksTUI and mkt added options chains, news, FRED, MCP and SSH servers. Neither has more than 160 stars; the popular tools do one screen well.

## 3. Numbers with confidence levels

| Claim | Value | Confidence |
|---|---|---|
| Tools listed / compared in the table | 17 listed (10 monitors, 7 one-shot or bar tools); 15 in the comparison table | High |
| ticker stars, licence, latest release | 6,270; GPL-3.0; v5.3.0 on 2026-06-21 | High |
| mop stars, licence, last push | 2,215; MIT; 2026-10-22; no tagged releases | High |
| tickrs stars, licence, latest release | 1,714; MIT; v0.15.0 on 2025-12-15 | High |
| cointop | 4,384 stars; archived; last release 2021-11-07 | High |
| mkt | 11 stars; MIT; v0.3.0 on 2026-09-19; 99 commits | High |
| Tools in the table that depend on Yahoo | 10 of 14 (excluding OpenBB) | High |
| Issues matching Yahoo, crumb, 429 or "no data" keywords across 7 repositories | 106 (keyword search, includes false positives) | Low as a count, High as a signal |
| Distinct Yahoo breakage waves seen in trackers, 2023 to 2025 | about 4 (2023-05, 2023-08, 2024-09, 2025-02 to 05) | Medium |
| Time for ticker to fully fix the 2023 break | about 33 days | Medium (thread summary) |
| Time tickrs showed no data in 2024 to 2025 | about 5 months | Medium |
| Issues mentioning Brazil, B3, Bovespa or `.SA` in the 7 trackers | 2 relevant (ticker #162; tickrs #136 "show other markets") | Medium |
| B3-focused terminal monitors found | 0 | Medium (absence of evidence) |
| Existing tools that meet D2 and D6 together | 0 | Medium (nothing was run) |
| `.SA` symbols work in ticker | Yes, per maintainer in 2021; not retested | Medium |
| `.SA` symbols work in mop, tickrs, stocksTUI, mkt | Expected, since all pass symbols to Yahoo | Low [unverified] |

## 4. Build, adopt or fork

**Adopt.** ticker today covers the pane and on-demand modes with B3 symbols, groups and BRL conversion. It is a good stopgap and a good benchmark: installing it for a week would show which gaps actually hurt. It does not meet D6 (no one-shot watchlist output) and has no header for Ibovespa and USD/BRL.

**Fork ticker.** Smallest path to all three modes: add a `print quotes` command and a header. Costs:
- GPL-3.0: the fork must stay GPL. Acceptable for D1 only if that licence is wanted.
- It fixes the stack at Go and Bubble Tea.
- It inherits the fragile part (Yahoo's cookie and crumb session) and no provider layer. The upside is that upstream usually repairs Yahoo breaks within days to weeks.
- Upstream is slow to merge (fix PRs open for 1 to 3 months), so the changes would likely live in the fork.

**Fork mkt.** MIT and architecturally the cleanest (data plane separate from the interface), but it is a young project (99 commits, 11 stars, apparently one author [unverified]) with a very wide scope and US assumptions. Not recommended as a base.

**Build.** Justified, because the gap is real and specific: no tool combines B3 awareness (suffix handling, two calendars, BRL and USD side by side) with a first-class one-shot mode. The scope that the evidence supports is small:
- a fetch-and-format core with a provider interface, Yahoo's chart endpoint first (it survived the breaks that hit the batch endpoint);
- one table screen modelled on ticker (toggles, groups) with mop's summary header;
- a one-shot mode modelled on tickerbar (never fails the caller, `state` field, cache for closed markets, versioned JSON).

My assessment: **build, small, and borrow the designs above**; use ticker in the meantime. Fork ticker only if Go and GPL-3.0 are acceptable and time matters more than the design goals in D6. Confidence: Medium. The main uncertainty is that no tool was run, so ticker's real behaviour with a mixed B3 and US list during market hours is not observed.

## 5. Open questions this raises for the product

1. **Is GPL-3.0 acceptable** if the tool is published? This decides whether forking ticker (or reusing its code) is on the table at all.
2. **Is Go acceptable as the stack?** The two closest tools (ticker, mkt) are Go with Bubble Tea. A different language rules out forking and makes them references only.
3. **Would a week with ticker be enough to cancel the build?** If the pane and on-demand modes are 90% of the use, a small separate one-shot script might cover D6.
4. **What does the one-shot mode print?** Human table, one status-bar line, JSON, or all three. Should it follow tickerbar's rule of always exiting 0 with a `state` field?
5. **Which bar or multiplexer is the target** for the one-shot mode (tmux, polybar, something else on LXQt)? Their input formats differ.
6. **Should the user type `PETR4` or `PETR4.SA`?** Existing tools expose Yahoo symbols as they are. Hiding the suffix is friendlier but needs a rule to tell B3 from US tickers.
7. **Mixed currencies:** show each row in its own currency (the default everywhere), convert to BRL on request (ticker's model), or both? If converted, is a rate read once at start good enough?
8. **Header:** a fixed summary with Ibovespa, S&P 500 and USD/BRL (mop's model), or are those just rows in a group?
9. **Watchlist editing:** file only (ticker), in the app (mop, stocksTUI), or a command such as `add` and `remove` (mkt)? In-app editing means the program writes the user's config file.
10. **Groups** in the first version? Every recent tool has them, and mop users are asking.
11. **Market hours:** should the tool stop polling a closed exchange and serve cached closes (tickerbar's model)? That needs B3 and US calendars with holidays, and a local cache, which no existing tool does for both.
12. **Failure display:** when Yahoo blocks or breaks, what does the pane show, and what does the one-shot mode return? Stale values with an age marker, as in stonks-dashboard, are the pattern users did not complain about.
13. **Provider layer on day one, or after the first break?** The trackers say the break will come; the layer costs design time now.
14. **Refresh interval:** existing defaults run from 1 s to 600 s. With data 15 minutes late (D4) and one request per ticker (F3), what interval is wanted?
15. **Are alerts and sparklines in or out** of the first version? They are the most visible extras in the newer tools, and alerts is a standing request in ticker.
