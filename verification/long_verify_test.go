//go:build verification

// Long checks of the one-shot mode: QR4 and QR5 at their full 10 minutes, the
// doubling of the wait after a second refusal (6.3), and the cache size over
// time (QR8, S15.3 as far as `print` can show it). See README.md.
//
// BRL_VERIFY_SLOW=1 turns on the checks of up to about 20 minutes;
// BRL_VERIFY_LONG=1 the full QR8 run (BRL_VERIFY_LONG_DURATION, default 8h).
package verification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func needSlow(t *testing.T, what string) {
	t.Helper()
	if os.Getenv("BRL_VERIFY_SLOW") != "1" {
		t.Skip("slow check (" + what + "): set BRL_VERIFY_SLOW=1")
	}
}

// varyState is the stand-in's state with the vary_quotes control, which the
// state type of print_verify_test.go does not carry.
type varyState struct {
	state
	VaryQuotes bool `json:"vary_quotes"`
}

func (e *env) setVarying(what string) {
	e.t.Helper()
	b, _ := json.Marshal(varyState{VaryQuotes: true})
	e.logf("\nSTAND-IN   switched: %s  %s\n", what, b)
	resp, err := http.Post(e.src.url+"/_control", "application/json", bytes.NewReader(b))
	if err != nil {
		e.t.Fatalf("stand-in control: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		e.t.Fatalf("stand-in control: status %d", resp.StatusCode)
	}
}

// everyPeriod runs `brl-stocks <args>` every period until d has passed and
// returns the number of runs. after, when not nil, is called after each run.
func (e *env) everyPeriod(d, period time.Duration, after func(n int, r result), args ...string) int {
	e.t.Helper()
	start := time.Now()
	n := 0
	for time.Since(start) < d {
		next := time.Now().Add(period)
		r := e.run(args...)
		e.wantExit(r, 0)
		n++
		if after != nil {
			after(n, r)
		}
		time.Sleep(time.Until(next))
	}
	return n
}

// ---------------------------------------------------------------------------
// QR4 at its full size (without the view, which is not built yet).
// ---------------------------------------------------------------------------

// QR4 "With the view open and `brl-stocks print` called every 2 seconds for
// 10 minutes, open market, 60-second interval, 23 entries: at most 345
// requests in total (15 per entry ...)". The view is not part of this run.
func TestVerifySlow_QR4_Full_PrintEveryTwoSecondsTenMinutes(t *testing.T) {
	needSlow(t, "10 minutes of real time")
	t.Parallel()
	cfg, syms := config20() // no refresh_seconds: the default 60
	e := newEnv(t, "QR4 full size: print every 2 s for 10 min, open market, interval 60 s, 23 entries, no view", cfg)
	runs := e.everyPeriod(10*time.Minute, 2*time.Second, nil, "print")
	recs := e.src.requests()
	c := bySymbol(recs)
	largest, largestSym := 0, ""
	for _, s := range syms {
		if c[s] > largest {
			largest, largestSym = c[s], s
		}
	}
	e.logf("\nRUNS %d   REQUESTS IN TOTAL %d   LARGEST PER ENTRY %d (%s)\nPER ENTRY %s\n", runs, len(recs), largest, largestSym, fmtCounts(c))
	t.Logf("runs %d, requests in total %d, largest per entry %d (%s); per entry: %s", runs, len(recs), largest, largestSym, fmtCounts(c))
	for s := range c {
		known := false
		for _, k := range syms {
			known = known || k == s
		}
		if !known {
			e.failf("a request for %q, which is not one of the 23 entries", s)
		}
	}
	if len(recs) > 345 {
		e.failf("QR4: want at most 345 requests in 10 minutes, the stand-in received %d", len(recs))
	}
	for _, s := range syms {
		if c[s] == 0 {
			e.failf("set-up: entry %s was never requested", s)
		}
	}
}

// ---------------------------------------------------------------------------
// QR5 at its full size, and the doubling of the wait (6.3).
// ---------------------------------------------------------------------------

// QR5 "From the first refused answer, at most 10 requests in any 10 minutes
// while the source keeps refusing, all running instances together", with
// `brl-stocks print --refresh` every 2 seconds (no view: it is not built).
// 6.3: a first wait of 5 minutes after the refusal; "The wait ends and the
// source refuses again | Blocked | The wait doubles, up to 30 minutes".
// Expected: after the first call, nothing until about 5 minutes after the
// first refusal; a probe then; nothing until about 10 minutes after the
// refused probe; another probe at about 15 minutes.
func TestVerifySlow_QR5_Full_And_W6_3_WaitDoubles(t *testing.T) {
	needSlow(t, "about 17 minutes of real time")
	t.Parallel()
	cfg, _ := config20()
	e := newEnv(t, "QR5 full size and 6.3 doubling: source refuses everything, print --refresh every 2 s, 23 entries", cfg)
	e.setSource(state{Status: 429}, "every request answered 429, from the first request")

	r0 := e.run("print", "--refresh")
	e.wantExit(r0, 0)
	var first int64
	for _, r := range e.src.requests() {
		if r.Status == 429 && r.AnsweredMS > 0 && (first == 0 || r.AnsweredMS < first) {
			first = r.AnsweredMS
		}
	}
	if first == 0 {
		t.Fatal("set-up: the first call met no refusal")
	}
	firstAt := time.UnixMilli(first)
	// Until 16 min 30 s after the first refusal: the second probe is expected
	// at about 15 minutes.
	end := firstAt.Add(16*time.Minute + 30*time.Second)
	runs := 1 + e.everyPeriod(time.Until(end), 2*time.Second, nil, "print", "--refresh")

	recs := e.src.requests()
	var b strings.Builder
	var after []record // arrived after the first refused answer was written
	for _, r := range recs {
		rel := time.Duration(r.UnixMS-first) * time.Millisecond
		fmt.Fprintf(&b, "  #%d %+10.3fs %s -> %d\n", r.Seq, rel.Seconds(), r.Symbol, r.Status)
		if r.UnixMS > first {
			after = append(after, r)
		}
	}
	e.logf("\nRUNS %d   REQUESTS IN TOTAL %d   ARRIVED AFTER THE FIRST REFUSAL WAS WRITTEN %d\nARRIVAL TIMES RELATIVE TO THE FIRST REFUSED ANSWER\n%s", runs, len(recs), len(after), b.String())
	t.Logf("requests in total %d, after the first refusal %d; arrival times relative to the first refusal:\n%s", len(recs), len(after), b.String())

	// Any 10-minute window: it suffices to start one at every arrival.
	worst, worstAt := 0, int64(0)
	for i, r := range after {
		n := 0
		for _, s := range after[i:] {
			if s.UnixMS < r.UnixMS+10*60*1000 {
				n++
			}
		}
		if n > worst {
			worst, worstAt = n, r.UnixMS-first
		}
	}
	e.logf("MOST REQUESTS IN ONE 10-MINUTE WINDOW: %d (window starting %.3fs after the first refusal)\n", worst, float64(worstAt)/1000)
	if worst > 10 {
		e.failf("QR5: want at most 10 requests in any 10 minutes after the first refused answer, a window starting %.3fs after it holds %d", float64(worstAt)/1000, worst)
	}
	for _, r := range recs {
		if r.Status != 429 && r.Status != -1 {
			e.failf("set-up: request #%d was answered %d, not 429", r.Seq, r.Status)
		}
	}

	// The doubling. Requests are grouped into probes: arrivals less than 30 s
	// apart belong to one probe.
	type probe struct{ from, to, answered int64 }
	var probes []probe
	for _, r := range after {
		rel := r.UnixMS - first
		if len(probes) > 0 && rel-probes[len(probes)-1].to < 30000 {
			probes[len(probes)-1].to = rel
			if r.AnsweredMS-first > probes[len(probes)-1].answered {
				probes[len(probes)-1].answered = r.AnsweredMS - first
			}
			continue
		}
		probes = append(probes, probe{rel, rel, r.AnsweredMS - first})
	}
	// The first call's own requests (arrived within 11 s of the first refusal,
	// while it was still running) are not a probe.
	var later []probe
	for _, p := range probes {
		if p.from > 11000 {
			later = append(later, p)
		}
	}
	var ps []string
	for _, p := range later {
		ps = append(ps, fmt.Sprintf("%.1fs..%.1fs", float64(p.from)/1000, float64(p.to)/1000))
	}
	e.logf("PROBES AFTER THE FIRST CALL (relative to the first refusal): %s\n", strings.Join(ps, ", "))
	t.Logf("probes after the first call: %v", ps)
	const slack = 15000 // a call every 2 s, plus the time of a call
	if len(later) < 1 {
		e.failf("6.3: want a probe at about 5 minutes after the first refusal, the stand-in received nothing after the first call")
		return
	}
	if p := later[0]; p.from < 5*60*1000-2000 || p.from > 5*60*1000+slack {
		e.failf("6.3: want the first probe at about 5 min (300 s) after the first refusal, it came at %.1fs", float64(p.from)/1000)
	}
	if len(later) < 2 {
		e.failf("6.3: want a second probe about 10 minutes after the refused first probe, none came (run ended %.0fs after the first refusal)", float64(end.UnixMilli()-first)/1000)
		return
	}
	gap := later[1].from - later[0].answered
	e.logf("WAIT AFTER THE REFUSED PROBE: %.1fs (from its answer to the next probe)\n", float64(gap)/1000)
	if gap < 10*60*1000-2000 || gap > 10*60*1000+slack {
		e.failf("6.3: want the wait to double to 10 min (600 s) after the refused probe; the next probe came %.1fs after it", float64(gap)/1000)
	}
	if len(later) > 2 {
		e.failf("6.3: want no request after the second probe within the run (the wait is now 20 min); probes: %v", ps)
	}
}

// ---------------------------------------------------------------------------
// QR8 and S15.3 (as far as `print` can show it).
// ---------------------------------------------------------------------------

// cacheSize returns the total size in bytes of the regular files under the
// tool's cache folder, and their names.
func (e *env) cacheSize() (int64, []string) {
	dir := filepath.Join(e.cacheHome, "brl-stocks")
	var total int64
	var names []string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Mode().IsRegular() {
			total += info.Size()
			rel, _ := filepath.Rel(dir, p)
			names = append(names, fmt.Sprintf("%s(%d)", rel, info.Size()))
		}
		return nil
	})
	sort.Strings(names)
	return total, names
}

// qr8 runs `print` once per interval (interval + 1 s, so that every call finds
// the cache older than one interval and fetches) for d, against a source whose
// quotes change on every answer, and checks that the cache folder stays within
// 10% of its size after the first full refresh.
func qr8(t *testing.T, d time.Duration, interval int) {
	cfg, syms := config20()
	if interval != 60 {
		cfg = fmt.Sprintf("refresh_seconds = %d\n", interval) + cfg
	}
	e := newEnv(t, fmt.Sprintf("QR8 / S15.3 (print only): %s of open market, interval %d s, quotes change on every answer, 23 entries", d, interval), cfg)
	e.setVarying("quotes change on every answer")

	r0 := e.run("print")
	e.wantExit(r0, 0)
	e.wantRequested(r0, syms, "first full refresh")
	base, names := e.cacheSize()
	e.logf("\nCACHE SIZE AFTER THE FIRST FULL REFRESH: %d bytes   files: %s\n", base, strings.Join(names, " "))
	if base == 0 {
		t.Fatal("set-up: the cache folder is empty after the first full refresh")
	}
	lo, hi := float64(base)*0.9, float64(base)*1.1
	var sizes []string
	minS, maxS := base, base
	fetched := 0
	period := time.Duration(interval+1) * time.Second
	time.Sleep(period)
	runs := e.everyPeriod(d, period, func(n int, r result) {
		fetched += len(r.reqs)
		s, nm := e.cacheSize()
		sizes = append(sizes, fmt.Sprint(s))
		e.logf("SAMPLE %d  %s  cache %d bytes (%+.2f%%)  requests this run %d  files: %s\n",
			n, time.Now().UTC().Format(time.RFC3339), s, 100*(float64(s)-float64(base))/float64(base), len(r.reqs), strings.Join(nm, " "))
		if s < minS {
			minS = s
		}
		if s > maxS {
			maxS = s
		}
		if float64(s) < lo || float64(s) > hi {
			e.failf("QR8: sample %d: cache %d bytes, outside 10%% of %d bytes after the first full refresh", n, s, base)
		}
	}, "print")
	e.logf("\nSAMPLES %d   FIRST %d   MIN %d   MAX %d   REQUESTS AFTER THE FIRST REFRESH %d\nALL SIZES %s\n",
		runs, base, minS, maxS, fetched, strings.Join(sizes, " "))
	t.Logf("samples %d, first %d bytes, min %d, max %d, requests after the first refresh %d", runs, base, minS, maxS, fetched)
	if fetched < (runs-1)*len(syms)/2 {
		e.failf("set-up: the quotes were refreshed too little to test QR8 (%d requests in %d runs)", fetched, runs)
	}
}

// QR8 scaled: 20 minutes at a 10-second interval (BRL_VERIFY_SLOW).
func TestVerifySlow_QR8_Scaled_CacheSizeTwentyMinutes(t *testing.T) {
	needSlow(t, "20 minutes of real time")
	t.Parallel()
	qr8(t, 20*time.Minute, 10)
}

// QR8 as specified: 8 hours (BRL_VERIFY_LONG_DURATION) at the default
// 60-second interval (BRL_VERIFY_LONG).
func TestVerifyLong_QR8_CacheSizeEightHours(t *testing.T) {
	if os.Getenv("BRL_VERIFY_LONG") != "1" {
		t.Skip("long check (8 hours of real time): set BRL_VERIFY_LONG=1")
	}
	t.Parallel()
	d := 8 * time.Hour
	if v := os.Getenv("BRL_VERIFY_LONG_DURATION"); v != "" {
		var err error
		if d, err = time.ParseDuration(v); err != nil {
			t.Fatalf("BRL_VERIFY_LONG_DURATION: %v", err)
		}
	}
	qr8(t, d, 60)
}
