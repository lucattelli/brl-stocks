# Market data sources for a personal stock monitor TUI (B3 and US)

Research date: 2026-11-23. Scope: free or cheap quote sources for B3 (stocks, FIIs, ETFs, BDRs) and US exchanges, for a personal terminal tool that may be open-sourced later.

## 1. Method

- Read the session log (`research/grilling-log.md`). No earlier reports existed.
- Web research on provider pricing pages, documentation, terms of use and issue trackers. Every claim has an inline URL. Claims that come only from third-party summaries, or that I could not open, are marked **[unverified]**.
- Live tests of endpoints that need no key and no account: 20 HTTP requests in total, about 2 s apart, with a generic User-Agent, on 2026-11-23 between 14:07 and 14:10 UTC (11:07 BRT, B3 open, US pre-market). Nothing was signed up for. Raw responses were kept in the scratch directory and deleted after the tests.
- Pages I could not read: Finnhub's pricing page (rendered by JavaScript, returned only a tagline), Financial Modeling Prep's pricing page (HTTP 403), the Twelve Data B3 support article (404). Data for these comes from search snippets and is marked.
- Confidence levels: **High** = seen on the provider's own page or in my own test today. **Medium** = provider page but ambiguous, or two independent third-party sources agree. **Low** = one third-party source, or memory.

## 2. Findings

### 2.1 Live test results (High confidence, measured today)

| Endpoint (no key) | Result | What it shows |
|---|---|---|
| `query1.finance.yahoo.com/v8/finance/chart/PETR4.SA` | 200, JSON | Works. `regularMarketTime` was 904 s to 938 s behind wall clock in three calls: B3 on Yahoo is **15 minutes delayed** |
| Same, for `PETR4F.SA`, `MXRF11.SA` (FII), `BOVA11.SA` (ETF), `AAPL34.SA` (BDR), `^BVSP` | 200 each | All B3 instrument types and the Ibovespa resolve with the `.SA` suffix. Lag 904 s to 1053 s. Yahoo labels all of them `EQUITY`, so it does not tell FII, ETF and BDR apart |
| Same, for `BRL=X` (USD/BRL) | 200 | Lag 10 s: currency is close to real time |
| Same, for `AAPL` (1 minute bars) | 200, 35 kB | Works, with pre and post market fields. US market was closed, so I could not measure the US delay |
| `query1.finance.yahoo.com/v7/finance/quote?symbols=...` (multi-symbol) | **401 Unauthorized** | The batch quote endpoint needs a cookie and crumb. Without them it is one request per ticker on the chart endpoint |
| `brapi.dev/api/quote/PETR4` | 200, JSON | Works with no token for the four sandbox tickers. Price, day range, volume, market cap, P/E, logo |
| `brapi.dev/api/quote/BBAS3` | **401** `MISSING_TOKEN` | Any other ticker needs a token |
| `cotacao.b3.com.br/mds/api/v1/instrumentQuotation/PETR4` | 200, JSON | B3's own site endpoint. Open, min, max, average, current price, change |
| `cotacao.b3.com.br/mds/api/v1/DailyFluctuationHistory/PETR4` | 200, JSON | One price per minute for the day. Last bar 10:53 when server time was 11:08: **15 minutes delayed** |
| `cotacao.b3.com.br/mds/api/v1/DelayedQuotations/PETR4` | 404 | This path, cited in old posts, does not exist |
| `bvmf.bmfbovespa.com.br/InstDados/SerHist/COTAHIST_D19112026.ZIP` | 200, 491 kB zip | B3's official daily file for all instruments is public. Published at about 00:02 UTC the next day. (20 Nov returned 404: national holiday) |
| `stooq.com/q/d/l/?s=aapl.us&i=d` | 200, but an HTML JavaScript browser check, not CSV | Stooq no longer serves plain scripted requests |
| `stooq.com/q/l/?s=aapl.us...` | 404 | The light quote endpoint is gone |

Side observation: brapi's sandbox answer carried a `regularMarketTime` only 21 s old, but its day low (41.66) was staler than Yahoo's 15 minute delayed low (41.64) fetched seconds earlier. The timestamp looks like a refresh time, not a trade time, which fits the "updated every 30 minutes" of the free plan. Confidence: Medium (one sample).

### 2.2 Comparison table

Prices are per month. "Key" means an account and API key are needed. B3 and US columns give the freshness on the free tier, unless noted.

| Provider | Official? | Key | B3 (free tier) | US (free tier) | Free quota | Cheapest paid | Streaming | History | Extras | Terms for this use |
|---|---|---|---|---|---|---|---|---|---|---|
| **Yahoo Finance** (chart endpoint, `yfinance`) | Unofficial | No | 15 min delay (measured) | Real time or near it on Yahoo's site [unverified today] | Undocumented; IP blocks after bursts | None | `yfinance` has `WebSocket` classes; B3 coverage [unverified] | Daily back to 2000 for PETR4; 1 minute bars for the current day (measured) | Dividends, splits, fundamentals, `BRL=X`, `^BVSP`, `^GSPC` | Grey. "Personal use only" per the yfinance README; no licence for the data |
| **brapi.dev** | Private API, not a licensed B3 distributor | Yes (4 sandbox tickers without) | Updated every 30 min | Not covered | 15,000 requests/month, 1 ticker per request, 3 months of daily history | R$ 99.99 (billed yearly): 150,000/month, 15 min updates, 10 tickers/request | No | Free 3 months daily; Pro 10+ years and intraday from 1 min | Paid: dividends, fundamentals, PTAX currency, crypto, macro | Display in your own product allowed; no proxy or wrapper that re-serves the API |
| **B3 site endpoints** (`cotacao.b3.com.br`) | Official source, undocumented endpoint | No | 15 min delay (measured) | Not covered | Undocumented | None | No | Intraday for the current day only | None | Not offered as an API; B3 site terms restrict reuse |
| **B3 daily files** (COTAHIST) | Official | No | End of day (next morning) | Not covered | One file per day | None | No | Yearly files back decades [unverified range] | All instruments, including fractional market | End of day and D-1 data may be redistributed free, per B3's FAQ |
| **HG Brasil Finance** | Private API | Yes | Delay not stated | Not covered | 400 requests/day | R$ 24.90: 1,000/day. R$ 49.90: 10,000/day, 5 tickers/request | No | Paid: 2 to 10 years | Dividends, FIIs, currencies, Ibovespa, Selic, CDI | Not read |
| **Twelve Data** | Licensed vendor | Yes | **Not on free tier**; Grow plan gives end of day only | "Real-time" on Basic per pricing page | 8 credits/min, 800/day | US$ 29 (Grow) | WebSocket only from Pro (US$ 99); trial on lower plans | Yes | Forex, crypto, fundamentals on higher plans | Individual vs business plans; not read in detail |
| **Alpha Vantage** | Licensed vendor | Yes | End of day at best [unverified], `.SAO` suffix [unverified] | End of day; real time or 15 min delay needs an entitlement step | **25 requests/day** | US$ 49.99: 75 requests/min | No | 20+ years daily [unverified] | Forex, fundamentals, indicators | Personal, non-commercial use only without a written agreement |
| **Finnhub** | Licensed vendor | Yes | Not on free tier [unverified] | Real-time US quotes [unverified] | 60 calls/min [unverified, several sources agree] | About US$ 50 per market [unverified] | WebSocket, 50 symbols free [unverified] | Candles are paid [unverified] | News, basic fundamentals, forex | Personal use on free tier [unverified] |
| **Financial Modeling Prep** | Licensed vendor | Yes | Not on free tier [unverified] | End of day on free tier [unverified] | 250 calls/day [unverified] | US$ 29 (about US$ 19 billed yearly): real-time US, 300 calls/min [unverified] | Higher tiers [unverified] | Yes | Strong fundamentals | Personal-use plans; not read |
| **Massive** (formerly Polygon.io) | Licensed vendor | Yes | **Not covered** (US only) | End of day, 2 years of history | 5 calls/min [unverified] | US$ 29 per asset class: 15 min delayed [unverified]. Real time: US$ 199 | WebSocket on paid plans | 2 years free, 20+ years on Advanced | Forex and crypto as separate products | "Individual use only" on the listed plans |
| **Alpaca Market Data** | Broker API | Yes (free account) | Not covered | Real time from IEX only (one venue); full feed 15 min delayed | 200 calls/min [unverified] | US$ 99: all US exchanges | **WebSocket free, 30 symbols** | Yes | Crypto, news | Not read |
| **Stooq** | Unofficial CSV | Now yes (key behind a CAPTCHA) | Coverage [unverified] | End of day | Daily hit limit | None | No | Long daily history | Indices, currencies | No published API terms |
| **Status Invest, Fundamentus, Investidor10** | Scraping | No | Site-dependent | No | None | None | No | No | Fundamentals, FII indicators | Scraping; fragile, terms not permissive |

Sources for the table, by row:

- Yahoo: my tests; yfinance README (https://raw.githubusercontent.com/ranaroussi/yfinance/main/README.md).
- brapi: https://brapi.dev/pricing, https://brapi.dev/legal/terms-of-use, https://brapi.dev/faq/a-brapi-e-distribuidora-licenciada-da-b3, and my tests.
- B3: my tests; https://b3.com.br/en_us/market-data-and-indices/data-services/market-data/distributors/faq/.
- HG Brasil: https://hgbrasil.com/pricing, https://hgbrasil.com/docs/finance/stocks, https://hgbrasil.com/status/finance.
- Twelve Data: https://twelvedata.com/pricing, https://twelvedata.com/exchanges.
- Alpha Vantage: https://www.alphavantage.co/premium/, https://www.alphavantage.co/terms_of_service/.
- Finnhub: search snippets of https://finnhub.io/pricing; https://github.com/finnhubio/Finnhub-API/issues/546 (candles 403 on free plan); https://massive.com/compare/massive-vs-finnhub.md (a competitor's page).
- FMP: search snippet of https://site.financialmodelingprep.com/api/pricing-md (page itself returned 403).
- Massive: https://massive.com/pricing, https://massive.com/compare/massive-vs-yfinance.md; free tier limits from https://apis.io/plans/polygon/polygon-plans-pricing/ and https://apicostcalc.com/blog/polygon-massive-rebrand-api-pricing.html.
- Alpaca: https://docs.alpaca.markets/docs/about-market-data-api, https://apis.io/plans/alpaca/alpaca-plans-pricing/.
- Stooq: my tests; https://github.com/pydata/pandas-datareader/issues/1012; https://apis.io/apis/stooq/stooq-q-api/.
- Scraping targets: https://www.tabnews.com.br/antoniorincon/como-montar-um-screener-da-b3-com-dados-do-fundamentus-duas-urls-e-uma-tabela-html, https://npmjs.com/package/statusinvest, https://statusinvest.com.br/condicoes-de-uso (not read in detail).

### 2.3 Notes per candidate

**Yahoo Finance (unofficial).**
- The only source found that covers B3 and US with one API and no key. Confirmed today for stocks, fractional tickers, FIIs, ETFs, BDRs, the Ibovespa and USD/BRL.
- `yfinance` is actively maintained: version 1.8.0 was uploaded on 2026-11-03 (https://pypi.org/pypi/yfinance/json). It now depends on `curl_cffi` (browser TLS impersonation) and `websockets`, which shows how much effort it takes to stay unblocked.
- It breaks periodically. Waves of `YFRateLimitError` / HTTP 429 were reported across versions 0.2.57 to 0.2.59, including on the cookie and crumb handshake, with some IP blocks lasting more than 24 hours (https://github.com/ranaroussi/yfinance/issues/2422, https://github.com/ranaroussi/yfinance/issues/2480, https://github.com/ranaroussi/yfinance/discussions/2431).
- The plain chart endpoint worked today with a simple User-Agent and no cookie, so a non-Python tool can use it directly, at one request per ticker.
- Terms: the yfinance README says "the Yahoo! finance API is intended for personal use only" and that the project is not affiliated with Yahoo. Yahoo's API terms reportedly bar earning income from its APIs without written permission [unverified, from a competitor's summary at https://massive.com/compare/massive-vs-yfinance.md].

**brapi.dev.**
- Best documented B3-specific API. But the free plan changed shape: 15,000 requests/month, one ticker per request, quotes refreshed every 30 minutes, 3 months of daily history. Dividends, fundamentals and currency are paid. FII detail is Pro only (https://brapi.dev/pricing).
- Monthly (non-annual) prices are not shown; R$ 99.99 and R$ 116.66 are the annual-billing equivalents.
- brapi states it is "não é distribuidora nem redistribuidora licenciada da B3" and has no SLA. Its FAQ allows showing data to the users of your own product, and forbids re-serving it as a feed, proxy, mirror or wrapper (https://brapi.dev/faq/a-brapi-e-distribuidora-licenciada-da-b3). The terms also say never to expose the key in client-side code (https://brapi.dev/legal/terms-of-use). A bring-your-own-key TUI fits this reading, but the terms do not address it by name.
- Budget arithmetic (my calculation): 15,000 requests/month with one ticker per request and 20 tickers gives 750 full refreshes a month, about 34 per trading day. That matches a 30 minute refresh almost exactly, and no faster.

**B3's own endpoints and files.**
- `cotacao.b3.com.br/mds/api/v1/...` is what B3's website uses. It is official data, 15 minutes delayed, keyless, and worked today. It is undocumented, has no stated limits, and the path has changed before (the `DelayedQuotations` path is dead). Treat it like a scraping target.
- The COTAHIST daily file is the one fully legitimate free source: official, complete (includes the fractional market), and B3's FAQ says end of day and D-1 data "may be distributed free of charge". It is end of day only, in a fixed-width text format.
- B3 charges fixed fees to anyone who *distributes* real-time or 15 minute delayed data (same FAQ). A personal tool that fetches for its own user is not a distributor, but a hosted service would be. This is my reading, not legal advice.

**HG Brasil.** 400 requests/day free, multi-ticker requests only from the R$ 49.90 plan. The delay is not published anywhere I could read. Covers FIIs, BDRs, ETFs, dividends, currencies and the Ibovespa. Ticker format is `B3:PETR4`.

**Twelve Data.** B3 is listed, but needs the Grow plan (US$ 29) and is end of day only. That makes it a poor fit for a B3 monitor. For US it is a reasonable free option (8 credits/min, 800/day).

**Alpha Vantage.** 25 requests/day makes the free tier unusable for a refreshing monitor. Its terms restrict the free licence to personal, non-commercial use.

**Finnhub.** By several consistent third-party sources, the strongest free US tier (60 calls/min, WebSocket with 50 symbols), with international markets paid. I could not read its pricing page, so every Finnhub number here is [unverified].

**Massive (Polygon.io).** Rebranded in late 2025; `api.polygon.io` keeps working in parallel [unverified, https://apicostcalc.com/blog/polygon-massive-rebrand-api-pricing.html]. US only. Free tier is end of day, so not useful for live monitoring.

**Alpaca.** Not in the original list, added because it is the cheapest route to *streaming* US data: free WebSocket with 30 symbols from the IEX venue. IEX is a small share of US volume, so prices can differ slightly from the consolidated tape. Needs a free account.

**Stooq.** Effectively closed to scripts since about March 2026: a key obtained through a CAPTCHA page is required, and today the endpoint answered with a JavaScript browser check. Drop it.

**Scraping (Status Invest, Fundamentus, Investidor10).** Useful only for fundamentals and FII indicators. No evidence found of a 2026 block, and none that they still work. Not tested.

### 2.4 Known B3 ticker problems

- **Suffix conventions differ per provider.** Yahoo uses `PETR4.SA`; brapi uses `PETR4`; HG Brasil uses `B3:PETR4`; Alpha Vantage uses `.SAO` [unverified]. A provider layer must map tickers.
- **Fractional market.** `PETR4F` is a separate instrument with its own, much lower volume (57,626 vs 4.86 million at the time of the test). Yahoo and brapi both serve it (https://brapi.dev/quote/PETR4F.md). Price was within R$ 0.01 of the round lot today.
- **Instrument type.** Yahoo reports FIIs, ETFs and BDRs all as `EQUITY`. Telling them apart needs the ticker pattern (ending in 11, 34 and so on, which is ambiguous for units) or a second source.
- **Corporate actions.** Yahoo's adjusted history for B3 has a reputation for errors around splits, bonus shares and ticker changes [unverified, Low confidence, from memory]. It matters for charts and for return calculations, not for a live quote.
- **Trading calendar.** B3 holidays differ from US ones (20 November returned no daily file). A monitor needs to show "market closed" correctly for each exchange.

## 3. Numbers with confidence levels

| Claim | Value | Confidence |
|---|---|---|
| B3 delay on Yahoo's chart endpoint | 15 min (904 s to 1053 s measured) | High |
| B3 delay on B3's own site endpoint | 15 min (measured) | High |
| USD/BRL lag on Yahoo | about 10 s | High (one sample) |
| brapi free tier | 15,000 requests/month, 1 ticker/request, 30 min updates, 3 months history | High |
| brapi cheapest paid | R$ 99.99/month billed yearly (R$ 1,199.90/year), 15 min updates | High |
| HG Brasil free tier | 400 requests/day | High |
| HG Brasil quote delay | not published | n/a |
| Twelve Data free tier | 8 credits/min, 800/day, US only | High |
| Twelve Data B3 | Grow plan (US$ 29), end of day | High |
| Alpha Vantage free tier | 25 requests/day | High |
| Alpha Vantage cheapest paid | US$ 49.99, 75 requests/min | High |
| Massive real-time US | US$ 199 | High |
| Massive free tier | end of day, 2 years, 5 calls/min | Medium |
| Finnhub free tier | 60 calls/min, 50 WebSocket symbols, US only | Medium |
| FMP free tier | 250 calls/day, end of day, US only | Medium |
| Alpaca free tier | IEX real time, 30 WebSocket symbols, 200 calls/min | Medium |
| yfinance latest release | 1.8.0, 2026-11-03 | High |
| Free real-time B3 quotes from any source | none found | Medium (absence of evidence) |

## 4. Recommendation

The main finding: **nobody gives real-time B3 quotes for free. 15 minutes delayed is the realistic free ceiling for B3**, and it is available without a key. For US stocks, real time is available free, with a key.

**(a) B3 only**
1. **Yahoo chart endpoint** (direct, or through `yfinance` if the tool is in Python). 15 min delay, no key, all instrument types, Ibovespa and USD/BRL included. Risk: unofficial, breaks a few times a year.
2. **brapi.dev** as the "proper" option with a user-supplied token. Cleaner terms, but slower on the free plan (30 min) and capped at about 20 tickers refreshed every 30 minutes.
3. **B3 COTAHIST daily file** as a fallback for closing prices, which never breaks and is free to redistribute.

**(b) US only**
1. **Finnhub** free tier, if its numbers are confirmed: real-time quotes and WebSocket, key required.
2. **Alpaca** free tier for streaming (IEX only), key required.
3. **Yahoo chart endpoint** as the no-key default.

**(c) Both markets**
1. **Yahoo** is the only single API that covers both for free. Make it the default provider.
2. Put it behind a **provider interface** from the start, with brapi (B3) and Finnhub or Twelve Data (US) as optional bring-your-own-key providers. The unofficial source will break at some point; the interface turns that from an outage into a configuration change.
3. No cheap paid plan fixes both at once: Twelve Data at US$ 29 gives only end-of-day B3.

## 5. Open questions this raises for the product

1. Is a **15 minute delay** acceptable for B3? If not, the only routes are a broker's platform or a paid licensed feed, which changes the budget question (Q5) completely.
2. Is a **different freshness per market** acceptable (US near real time, B3 delayed), and should the screen show the age of each quote?
3. Is depending on an **unofficial source that will break occasionally** acceptable for a personal tool? And for a published one, where the breakage becomes other people's bug reports?
4. **Key or no key**: is "works with zero setup" a goal? That points to Yahoo. Or is asking the user to create a free account and paste a token acceptable?
5. **How many tickers and what refresh interval?** This decides whether brapi's free quota (about 20 tickers every 30 minutes) or Yahoo's undocumented limits are enough.
6. **Which instrument types** matter: FIIs, BDRs, ETFs, the fractional market? FIIs and dividends push towards brapi's paid plans or a second source.
7. Does the tool need **history and charts**, or only the current quote? Intraday history is free on Yahoo and paid on brapi.
8. Are **USD/BRL and indices** (Ibovespa, S&P 500) part of the watchlist? Yahoo covers both; most B3-only APIs charge for currency.
9. **Language choice**: `yfinance` only helps a Python tool. In Rust or Go the tool calls the chart endpoint itself and owns the breakage.
10. If published: should the README carry a **data terms notice**, and should the legal research item in the backlog be raised in priority? Yahoo's terms are the weak point.

To verify before committing: Finnhub's and FMP's real free-tier terms (pages unreadable today), HG Brasil's actual delay, and the US delay on Yahoo during market hours.
