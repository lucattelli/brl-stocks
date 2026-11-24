// -*- mode: Bluespec; -*-
// Model of the refresh protocol of docs/design.md, section 4:
// "A short lock, per-row claims, fetch outside the lock" (D54), "The block
// record in a file of its own" (D56) and "The refresh protocol in detail".
//
// What is modelled
//   - several processes (a view, print calls) sharing a quote cache and a
//     source file;
//   - claim, send, receive, save-block and commit as separate steps, so that
//     every interleaving between processes is explored;
//   - a process that stalls (it simply takes no step) or is killed (crash);
//   - the source: it answers with its current quote version, or refuses;
//   - the source file being lost, and a row being removed from the watchlist.
//
// What is abstracted
//   - The lock. Every step that reads and writes the files under the lock is
//     one atomic action here. A process stopped while holding the lock only
//     prevents others from stepping, which removes behaviours and cannot
//     break a safety property.
//   - "Looking at the source file before each request" and sending the
//     request are one step. In the real tool a few milliseconds separate them.
//   - Time is a small integer; TTL and WAIT stand for 20 seconds and 5 minutes.
//   - Which rows are due. Any present row without a live claim may be claimed,
//     which covers every rule of Plan and more.
module refresh {
  pure val PROCS = Set("p1", "p2", "p3")
  pure val ROWS = Set("a", "b")
  pure val TTL = 2      // a claim, and a probe claim, expire after TTL ticks
  pure val WAIT = 3     // the wait after a first refusal
  pure val MAXT = 6
  pure val MAXVER = 2
  pure val MAXID = 4    // claims per run; keeps the state space finite

  type Row = {
    present: bool,      // still in the watchlist
    ver: int,           // version of the cached quote; 0 = no quote
    claim: int,         // claim id; 0 = not claimed
    claimAt: int,
    refusedAt: int      // when its last request was refused; 0 = never
  }

  type Src = {
    blockedUntil: int,
    refusals: int,
    probeClaim: int,    // 0 = no probe claimed
    probeAt: int,
    gen: int            // bumped at every write of the record
  }

  type Proc = {
    pc: str,            // "idle" | "fetch" | "saveBlock" | "commit"
    claim: int,
    claimAt: int,
    rows: Set[str],     // the rows of this round
    sent: Set[str],
    got: str -> int,    // answers received: row -> quote version
    probe: bool,
    seenGen: int        // generation of the source record when the probe was claimed
  }

  type Env = { nextId: int, srcVer: int, refusing: bool }

  // Ghost state: not part of the tool, only there to state the properties.
  type Ghost = {
    regress: bool,          // a commit replaced a quote by an older one
    revived: bool,          // a commit wrote to a row that had been removed
    badClear: bool,         // a probe cleared a block saved after the probe started
    refusalSeq: int,        // number of refusals saved so far
    refusalSeqAtProbe: str -> int,
    sendsWhileRefusing: int, // requests sent since the first refusal was received, source still refusing
    firstRefusal: bool,
    probesRefused: int
  }

  var now: int
  var cache: str -> Row
  var src: Src
  var procs: str -> Proc
  var env: Env
  var g: Ghost

  pure val emptySrc: Src = { blockedUntil: 0, refusals: 0, probeClaim: 0, probeAt: 0, gen: 0 }
  pure val idleProc: Proc = {
    pc: "idle", claim: 0, claimAt: 0, rows: Set(), sent: Set(), got: Map(), probe: false, seenGen: 0
  }

  pure def max(a: int, b: int): int = if (a > b) a else b

  // Rule 5: a refusal is in the cache as well. If the source file shows no
  // block while a row was refused less than WAIT ago, the source is blocked.
  pure def blockedUntil(s: Src, c: str -> Row): int =
    ROWS.fold(s.blockedUntil, (acc, r) =>
      if (c.get(r).refusedAt > 0) max(acc, c.get(r).refusedAt + WAIT) else acc)

  pure def claimLive(row: Row, t: int): bool = row.claim != 0 and row.claimAt + TTL > t

  action init = all {
    now' = 1,
    cache' = ROWS.mapBy(r => { present: true, ver: 0, claim: 0, claimAt: 0, refusedAt: 0 }),
    src' = emptySrc,
    procs' = PROCS.mapBy(p => idleProc),
    env' = { nextId: 1, srcVer: 1, refusing: false },
    g' = {
      regress: false, revived: false, badClear: false, refusalSeq: 0,
      refusalSeqAtProbe: PROCS.mapBy(p => 0), sendsWhileRefusing: 0, firstRefusal: false,
      probesRefused: 0
    },
  }

  // ---- a process claims rows for a normal round (atomic, under the lock)
  action claimRows(p: str): bool = {
    val due = ROWS.filter(r => cache.get(r).present and not(claimLive(cache.get(r), now)))
    nondet chosen = due.powerset().oneOf()
    all {
      procs.get(p).pc == "idle",
      env.nextId <= MAXID,
      blockedUntil(src, cache) <= now,
      src.refusals == 0,
      chosen.size() > 0,
      cache' = cache.keys().mapBy(r =>
        if (chosen.contains(r)) { ...cache.get(r), claim: env.nextId, claimAt: now } else cache.get(r)),
      procs' = procs.set(p, { ...idleProc, pc: "fetch", claim: env.nextId, claimAt: now, rows: chosen }),
      env' = { ...env, nextId: env.nextId + 1 },
      now' = now, src' = src, g' = g,
    }
  }

  // ---- a process claims the probe, after a block's wait has ended (rule 6)
  action claimProbe(p: str): bool = {
    val free = ROWS.filter(r => cache.get(r).present and not(claimLive(cache.get(r), now)))
    nondet r = free.oneOf()
    all {
      procs.get(p).pc == "idle",
      env.nextId <= MAXID,
      blockedUntil(src, cache) <= now,
      src.refusals > 0,
      src.probeClaim == 0 or src.probeAt + TTL <= now,
      free.size() > 0,
      cache' = cache.set(r, { ...cache.get(r), claim: env.nextId, claimAt: now }),
      src' = { ...src, probeClaim: env.nextId, probeAt: now, gen: src.gen + 1 },
      procs' = procs.set(p, {
        ...idleProc, pc: "fetch", claim: env.nextId, claimAt: now, rows: Set(r), probe: true,
        seenGen: src.gen + 1
      }),
      env' = { ...env, nextId: env.nextId + 1 },
      g' = { ...g, refusalSeqAtProbe: g.refusalSeqAtProbe.set(p, g.refusalSeq) },
      now' = now,
    }
  }

  // ---- a worker looks at the source file, then sends one request (rule 4)
  action send(p: str): bool = {
    val me = procs.get(p)
    nondet r = me.rows.exclude(me.sent).oneOf()
    all {
      me.pc == "fetch",
      me.rows.exclude(me.sent).size() > 0,
      blockedUntil(src, cache) <= now,
      procs' = procs.set(p, { ...me, sent: me.sent.union(Set(r)) }),
      g' = if (g.firstRefusal and env.refusing)
             { ...g, sendsWhileRefusing: g.sendsWhileRefusing + 1 } else g,
      now' = now, cache' = cache, src' = src, env' = env,
    }
  }

  // ---- the look at the source file finds a block: the round ends (rule 4)
  action stopOnBlock(p: str): bool = all {
    procs.get(p).pc == "fetch",
    blockedUntil(src, cache) > now,
    procs' = procs.set(p, { ...procs.get(p), pc: "commit" }),
    now' = now, cache' = cache, src' = src, env' = env, g' = g,
  }

  // ---- an answer arrives: a quote, or a refusal
  action receive(p: str): bool = {
    val me = procs.get(p)
    val waiting = me.sent.filter(r => not(me.got.keys().contains(r)))
    nondet r = waiting.oneOf()
    all {
      me.pc == "fetch",
      waiting.size() > 0,
      if (env.refusing) all {
        procs' = procs.set(p, { ...me, pc: "saveBlock", rows: me.rows, sent: me.sent.union(Set(r)) }),
        cache' = cache.set(r, { ...cache.get(r), refusedAt: now }),
        g' = { ...g, firstRefusal: true },
      } else all {
        procs' = procs.set(p, { ...me, got: me.got.put(r, env.srcVer) }),
        cache' = cache,
        g' = g,
      },
      now' = now, src' = src, env' = env,
    }
  }

  // ---- a refusal is saved the moment it is seen (atomic, under the lock)
  // A refusal of a request sent before the block was known does not lengthen
  // the wait; only a refused probe doubles it.
  action saveBlock(p: str): bool = {
    val me = procs.get(p)
    val known = src.blockedUntil > now
    val newSrc: Src =
      if (me.probe)
        { blockedUntil: now + 2 * WAIT, refusals: src.refusals + 1, probeClaim: 0, probeAt: 0, gen: src.gen + 1 }
      else if (known)
        src
      else
        { ...src, blockedUntil: now + WAIT, refusals: max(src.refusals, 1), gen: src.gen + 1 }
    all {
      me.pc == "saveBlock",
      src' = newSrc,
      procs' = procs.set(p, { ...me, pc: "commit" }),
      g' = { ...g, refusalSeq: g.refusalSeq + 1,
             probesRefused: if (me.probe) g.probesRefused + 1 else g.probesRefused },
      now' = now, cache' = cache, env' = env,
    }
  }

  // ---- the round's deadline: whatever was not answered is given up
  action deadline(p: str): bool = all {
    procs.get(p).pc == "fetch",
    procs' = procs.set(p, { ...procs.get(p), pc: "commit" }),
    now' = now, cache' = cache, src' = src, env' = env, g' = g,
  }

  // ---- commit (atomic, under the lock): rules 2, 3 and 6
  action commit(p: str): bool = {
    val me = procs.get(p)
    def mine(r: str): bool = cache.get(r).present and cache.get(r).claim == me.claim
    val answered = me.got.keys()
    val probeOk = me.probe and answered.size() > 0
    val clears = probeOk and src.gen == me.seenGen
    all {
      // after an early stop, or when every row of the round has its answer
      me.pc == "commit" or (me.pc == "fetch" and me.rows.forall(r => answered.contains(r))),
      cache' = cache.keys().mapBy(r =>
        if (me.rows.contains(r) and mine(r))
          { ...cache.get(r), claim: 0,
            ver: if (answered.contains(r)) me.got.get(r) else cache.get(r).ver,
            refusedAt: if (answered.contains(r)) 0 else cache.get(r).refusedAt }
        else cache.get(r)),
      src' = if (clears) { ...emptySrc, gen: src.gen + 1 }
             else if (me.probe and src.probeClaim == me.claim) { ...src, probeClaim: 0, gen: src.gen + 1 }
             else src,
      procs' = procs.set(p, idleProc),
      g' = { ...g,
        regress: g.regress or me.rows.exists(r =>
          mine(r) and answered.contains(r) and me.got.get(r) < cache.get(r).ver),
        revived: g.revived or me.rows.exists(r =>
          answered.contains(r) and not(cache.get(r).present) and mine(r)),
        badClear: g.badClear or (clears and g.refusalSeq > g.refusalSeqAtProbe.get(p)),
        sendsWhileRefusing: if (clears) 0 else g.sendsWhileRefusing,
        firstRefusal: if (clears) false else g.firstRefusal,
      },
      now' = now, env' = env,
    }
  }

  // ---- a process is killed: nothing it held in memory reaches the files
  action crash(p: str): bool = all {
    procs.get(p).pc != "idle",
    procs' = procs.set(p, idleProc),
    now' = now, cache' = cache, src' = src, env' = env, g' = g,
  }

  // ---- the environment
  action tick = all {
    now < MAXT,
    now' = now + 1,
    cache' = cache, src' = src, procs' = procs, env' = env, g' = g,
  }

  action newQuote = all {
    env.srcVer < MAXVER,
    env' = { ...env, srcVer: env.srcVer + 1 },
    now' = now, cache' = cache, src' = src, procs' = procs, g' = g,
  }

  action flipSource = all {
    env' = { ...env, refusing: not(env.refusing) },
    g' = if (env.refusing) { ...g, sendsWhileRefusing: 0, firstRefusal: false } else g,
    now' = now, cache' = cache, src' = src, procs' = procs,
  }

  // a row leaves the watchlist (a config edit, committed by some process)
  action removeRow = {
    nondet r = ROWS.oneOf()
    all {
      cache.get(r).present,
      cache' = cache.set(r, { ...cache.get(r), present: false, claim: 0, ver: 0 }),
      now' = now, src' = src, procs' = procs, env' = env, g' = g,
    }
  }

  // the source file is emptied or replaced by an empty one
  action loseSourceFile = all {
    src' = { ...emptySrc, gen: src.gen + 1 },
    now' = now, cache' = cache, procs' = procs, env' = env, g' = g,
  }

  action procStep = {
    nondet p = PROCS.oneOf()
    any {
      claimRows(p), claimProbe(p), send(p), stopOnBlock(p), receive(p),
      saveBlock(p), deadline(p), commit(p), crash(p),
    }
  }

  // the protocol with a source that may refuse, and rows that may be removed
  action step = any { procStep, tick, newQuote, flipSource, removeRow }

  // the same, and the source file may be lost at any moment
  action stepLossy = any { step, loseSourceFile }

  // Narrower steps, so that an exhaustive check runs to completion. Each
  // leaves out what its properties do not depend on.
  // A source that never refuses: for noRegress, noRevival, oneFetcherPerLiveClaim.
  action stepQuiet = any { procStep, tick, newQuote, removeRow }
  // A source that refuses and recovers, with one quote version and no removal:
  // for noBadClear, waitGrowsOnlyOnProbes, boundedSendsWhileRefusing.
  action stepBlocks = any { procStep, tick, flipSource }
  // The same with the source file lost at any moment.
  action stepBlocksLossy = any { stepBlocks, loseSourceFile }

  // ================= properties =================

  // A newer quote is never replaced by an older one (rule 2).
  val noRegress = not(g.regress)

  // A removed row never gets a quote again (rule 2, spec S15.6).
  val noRevival = not(g.revived) and ROWS.forall(r => cache.get(r).present or cache.get(r).ver == 0)

  // A probe never clears a block that was saved after the probe started (rule 6).
  val noBadClear = not(g.badClear)

  // Two processes fetch the same row only when the older claim had expired.
  val oneFetcherPerLiveClaim = PROCS.forall(p => PROCS.forall(q => ROWS.forall(r =>
    (p != q and procs.get(p).pc == "fetch" and procs.get(q).pc == "fetch"
      and procs.get(p).rows.contains(r) and procs.get(q).rows.contains(r))
    implies (procs.get(p).claimAt + TTL <= procs.get(q).claimAt
          or procs.get(q).claimAt + TTL <= procs.get(p).claimAt))))

  // The wait grows only when a probe is refused, not once per refused request.
  val waitGrowsOnlyOnProbes = src.refusals <= g.probesRefused + 1

  // While the source keeps refusing, the requests sent after the first refusal
  // was received stay bounded: what the other processes had already claimed,
  // plus one probe per wait. With 3 processes, 2 rows and 6 ticks: 4 + 2.
  val boundedSendsWhileRefusing = g.sendsWhileRefusing <= 6

  val safety = all {
    noRegress, noRevival, noBadClear, oneFetcherPerLiveClaim, waitGrowsOnlyOnProbes,
    boundedSendsWhileRefusing,
  }

  // ================= scenarios from the third attack round =================

  // Finding 2: A claims, fetches version 1 and stalls; its claim expires; B
  // fetches version 2 and commits; A's late commit must not win.
  run staleCommitTest =
    init
      .then(claimRowsOf("p1", Set("a")))
      .then(send("p1")).then(receive("p1"))
      .then(tick).then(tick)
      .then(newQuote)
      .then(claimRowsOf("p2", Set("a")))
      .then(send("p2")).then(receive("p2")).then(commit("p2"))
      .then(commit("p1"))
      .expect(cache.get("a").ver == 2 and not(g.regress))

  // Finding 1: B holds a claim when A is refused; B must not send afterwards.
  run noSendAfterRefusalTest =
    init
      .then(claimRowsOf("p1", Set("a")))
      .then(claimRowsOf("p2", Set("b")))
      .then(flipSource)
      .then(send("p1")).then(receive("p1")).then(saveBlock("p1"))
      .then(stopOnBlock("p2"))
      .expect(procs.get("p2").sent.size() == 0)

  // helper: a claim of exactly these rows
  action claimRowsOf(p: str, chosen: Set[str]): bool = all {
    procs.get(p).pc == "idle",
    blockedUntil(src, cache) <= now,
    src.refusals == 0,
    chosen.forall(r => cache.get(r).present and not(claimLive(cache.get(r), now))),
    cache' = cache.keys().mapBy(r =>
      if (chosen.contains(r)) { ...cache.get(r), claim: env.nextId, claimAt: now } else cache.get(r)),
    procs' = procs.set(p, { ...idleProc, pc: "fetch", claim: env.nextId, claimAt: now, rows: chosen }),
    env' = { ...env, nextId: env.nextId + 1 },
    now' = now, src' = src, g' = g,
  }
}
