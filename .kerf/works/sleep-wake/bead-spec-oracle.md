# Ready-to-dispatch BEAD SPEC — Genuine-Drain Oracle (M0)

title: Genuine-drain oracle: daemon predicate for "fleet truly drained" (sleep gate)

type: feature  priority: 1  labels: codename:sleep-wake

## description

PROBLEM
The fleet sleep/wake feature (hk-rl4b) may put long-lived LLM sessions to sleep ONLY
when bead work is TRULY drained. The captain historically false-declares "no work" via
several false-negative sources; a false "no work" sleep stalls the fleet with ready
beads pending — the #1 failure mode. We need a deterministic daemon-side predicate that
is correct against ALL known false-negative sources and fail-closed on any doubt.

EXACT CONTRACT
Add `func (d *Daemon) GenuineDrain(ctx) (DrainResult, error)` in a new
`internal/daemon/draindetect.go`. `DrainResult{State DrainState; Reasons []string}`,
`DrainState ∈ {DRAINED, HAS_WORK, UNSURE}`. Returns DRAINED iff ALL hold:
(1) every named queue is completed/cancelled OR has zero non-terminal items AND is not in
any paused-by-* state AND there is no un-reconciled `.json.failed-*` archive on disk;
(2) every OPEN epic has zero ready children (accounting for ledger-dep gating);
(3) `RunRegistry.Len()==0` and no live in-progress worktree; (4) `br ready --limit 0`
returns zero dispatchable beads. Else HAS_WORK, or UNSURE on any evaluation error.
The caller sleeps ONLY on DRAINED.

FIVE FALSE-NEGATIVE DEFENSES (each → a concrete check)
1. br-ready pagination: current `internal/brcli/ready.go:91` runs `br ready` with NO
   `--limit` → paginated. Oracle MUST use a NEW `ReadyAll` = `br ready --limit 0`; a
   default-paginated empty is NOT trusted.
2. ledger-dep gating: any queue item in `deferred-for-ledger-dep` ⇒ HAS_WORK; AND for
   every OPEN epic E, count children C with `BeadLedger.BlocksEdge(E,C)` true and C
   otherwise-ready ⇒ if >0, HAS_WORK. Reuse `BeadLedger`
   (`internal/queue/validation.go:144`) + bridge (`queueledger_bridge.go`).
3. paused-by-failure: any queue status paused-by-failure/-drain/-budget ⇒ HAS_WORK; AND
   scan `.harmonik/queues/*.json.failed-*` DIRECTLY (bypass the `EnumerateQueueNames`
   filter at `persistence.go:463-485`) — an un-reconciled failed-archive ⇒ HAS_WORK.
4. in-flight runs: `RunRegistry.Len()==0` (`runregistry.go:148`) AND no
   `.harmonik/worktrees/*` live run.
5. kerf-next-empty: oracle MUST NOT consult `kerf next` — it is external and reports
   false-empty for works lacking `bead_filter`. `br ready --limit 0` is authoritative.

FAIL-SAFE
Every check is fail-closed toward HAS_WORK. Any br exec error / non-zero exit / schema
mismatch / ledger lookup error / unreadable queue file / RPC timeout ⇒ UNSURE. The "all
items terminal but queue status not yet rolled" race ⇒ UNSURE, not DRAINED. UNSURE and
HAS_WORK both keep the fleet awake. False sleep requires positive emptiness evidence on
EVERY axis; staying awake is the default on any doubt.

RED-then-GREEN TEST PLAN (build+test FIRST; unit tests in internal/daemon — NOT
daemon-boot scenario tests, so no 30-min commit-budget timeout)
RED (must fail before impl): TestGenuineDrain_PaginatedReadyHidesWork,
_DeferredLedgerDepItemIsWork, _OpenEpicWithReadyChild, _PausedByFailureIsStuck,
_FailedArchiveFileIsStuck, _InFlightRunBlocksDrain, _BrExecErrorIsUnsure,
_LedgerLookupErrorIsUnsure, _KerfNotConsulted; plus one positive
_TrulyDrainedReturnsDrained. Fakes: reuse `internal/queue` BeadLedger test fakes; add a
fake br adapter exposing Ready + ReadyAll; real RunRegistry + QueueStore constructors.

DEFAULTS-PIN NOTE
Pure work-detection predicate. Introduces ZERO threshold/band knobs. MUST NOT read or
alter any keeper warn/act/force/window value (`internal/keeper/thresholds.go` — operator
HARD-NO). Sleep-grace/wake-trigger/bands are POLICY, deferred to a later pass.

ANCHOR FILES
internal/brcli/ready.go:91 · internal/queue/state.go:130-140,233,290,361 ·
internal/queue/types.go:42-139 · internal/queue/persistence.go:289-305,463-485 ·
internal/daemon/queuestore_hkj808w.go:199 · internal/daemon/runregistry.go:148,163 ·
internal/daemon/queueledger_bridge.go · internal/queue/validation.go:144-154

DONE means
- `GenuineDrain` exists in internal/daemon/draindetect.go with the contract above.
- All 10 tests GREEN; the 9 RED-first tests demonstrably fail against a stub before impl.
- New `ReadyAll`/`--limit 0` adapter path added; no existing dispatch path changed.
- No keeper threshold touched; no production sleep behavior wired yet (predicate only —
  M1 wires it).
