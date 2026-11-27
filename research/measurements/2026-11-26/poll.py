"""Poll the source's chart endpoint for Q62's research (holiday, close, session switch).

Writes one JSON line per symbol per poll to OUT/poll.jsonl, and the raw answer
every 30 minutes to OUT/raw/. Stdlib only. Stops at STOP (UTC).
"""
import datetime as dt
import json
import os
import sys
import time
import urllib.request

OUT = sys.argv[1]
STOP = dt.datetime.fromisoformat(sys.argv[2]).replace(tzinfo=dt.timezone.utc)
SYMS = ["AAPL", "MSFT", "^GSPC", "PETR4.SA", "VALE3.SA", "^BVSP", "BRL=X"]
os.makedirs(os.path.join(OUT, "raw"), exist_ok=True)


def utc(t):
    return dt.datetime.fromtimestamp(t, dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ") if t else None


def period():
    """Seconds between polls: 2 minutes around the B3 close and the US/B3 opens, else 10."""
    n = dt.datetime.now(dt.timezone.utc)
    m = n.hour * 60 + n.minute
    busy = [(19 * 60 + 30, 22 * 60), (12 * 60 + 30, 13 * 60 + 30), (14 * 60, 15 * 60), (17 * 60 + 45, 19 * 60)]
    return 120 if any(a <= m < b for a, b in busy) else 600


last_raw = 0
while dt.datetime.now(dt.timezone.utc) < STOP:
    now = time.time()
    keep_raw = now - last_raw >= 1800
    for s in SYMS:
        q = urllib.parse.quote(s)
        url = f"https://query1.finance.yahoo.com/v8/finance/chart/{q}?interval=5m&range=1d"
        rec = {"polled": utc(now), "symbol": s}
        try:
            req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
            with urllib.request.urlopen(req, timeout=10) as r:
                body = r.read()
            j = json.loads(body)
            res = j["chart"]["result"][0]
            m = res["meta"]
            ctp = m.get("currentTradingPeriod", {})
            ts = res.get("timestamp") or []
            closes = (res.get("indicators", {}).get("quote") or [{}])[0].get("close") or []
            bars = [(utc(t), c) for t, c in zip(ts, closes)][-3:]
            rec.update(
                price=m.get("regularMarketPrice"),
                time=utc(m.get("regularMarketTime")),
                prev=m.get("chartPreviousClose"),
                high=m.get("regularMarketDayHigh"),
                low=m.get("regularMarketDayLow"),
                regular=[utc(ctp.get("regular", {}).get("start")), utc(ctp.get("regular", {}).get("end"))],
                pre=[utc(ctp.get("pre", {}).get("start")), utc(ctp.get("pre", {}).get("end"))],
                post=[utc(ctp.get("post", {}).get("start")), utc(ctp.get("post", {}).get("end"))],
                nbars=len(ts),
                last_bars=bars,
            )
            if keep_raw:
                name = s.replace("^", "IDX_") + "." + dt.datetime.fromtimestamp(now, dt.timezone.utc).strftime("%m%dT%H%M") + ".json"
                with open(os.path.join(OUT, "raw", name), "wb") as f:
                    f.write(body)
        except Exception as e:  # recorded, never fatal
            rec["error"] = repr(e)[:200]
        with open(os.path.join(OUT, "poll.jsonl"), "a") as f:
            f.write(json.dumps(rec) + "\n")
    if keep_raw:
        last_raw = now
    time.sleep(period())
