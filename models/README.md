# Models

`refresh.qnt` is a Quint model of the refresh protocol in `docs/design.md`,
section 4. Quint is pinned in `mise.toml`.

## Running it

```sh
quint typecheck models/refresh.qnt
quint test models/refresh.qnt                      # two scenarios from the design's attack rounds

# Exhaustive checks with TLC. Keep its working files off /tmp: they reach
# several gigabytes, and /tmp may be held in memory.
export TMPDIR=~/.cache/brl-stocks-tlc && mkdir -p $TMPDIR
quint verify models/refresh.qnt --backend=tlc --step=stepQuiet \
  --invariant="all { noRegress, noRevival, oneFetcherPerLiveClaim }"
quint verify models/refresh.qnt --backend=tlc --step=stepBlocks \
  --invariant="all { noBadClear, waitGrowsOnlyOnProbes, boundedSendsWhileRefusing }"
rm -rf $TMPDIR
```

The two `step` actions each leave out what their properties do not depend on;
with the full `step` the state space is too large to finish.

## Results

Checked on 2026-11-23 with Quint 0.34.0, with `PROCS` reduced to two processes:

| Check | Result |
|---|---|
| `stepQuiet`, three properties | Complete: no violation; depth 28; 1 min 45 s |
| `stepBlocks`, three properties | Complete: no violation; depth 34; 4 min 58 s |
| `step`, all six | Not complete: no violation in any run of up to 20 steps |
| `stepBlocksLossy` (the block file may be lost) | Not run to a result |
| Three processes | Not run |

Random simulation (`quint run`) is too weak for this model: 50,000 runs did
not notice four rules broken on purpose, each of which TLC finds in seconds.
Apalache (`quint verify` without `--backend=tlc`) did not reach depth 12 in
nine minutes.
