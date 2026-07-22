# Track C — APPLIED (record)

> Applied 2026-07-13 (finishing an interrupted impl run). Source of truth: `track-c-enforcement.md`.
> **Uncommitted by design** — P1 owns commits on branch `phase1-session-restart-substrate`.
> Write scope was exactly: `.golangci.yml`, `scripts/coverage-gate.sh`, `Makefile`, this file.
> No `internal/**`, `specs/**`, or `.go` source touched.

## 1. `.golangci.yml` — complexity ratchet + depguard (already on disk when this run began; VERIFIED)

Applied per §1/§2 and re-verified against the doc — **no discrepancies found, nothing re-done**:

- **Complexity linters enabled** (ratchet via `--new-from-rev`, grandfathers all existing violators,
  zero `//nolint`): `funlen` (100 lines / 60 stmts, ignore-comments), `cyclop`
  (max-complexity 15, package-average disabled), `gocognit` (min-complexity 20). `gocyclo`
  intentionally omitted (cyclop is the maintained superset).
- **Complexity exclusion** for `_test.go` + `internal/scenario/` + `internal/specaudit/`.
- **queue↔uuid depguard fix** (§2.3): added `github.com/google/uuid` to the `queue` allow-list
  (`internal/queue/rpc.go:32` uses `uuid.NewV7`; was a latent allow-list violation). Adding an
  allowed edge only *removes* potential findings — safe under the ratchet.
- **`runexec` depguard rule** pre-authored (§2.2): matches zero files today (package is M3), becomes
  load-bearing when M3 lands; deny edges on `internal/daemon` + `internal/workloop` are the point.
- **Dead-rule cleanup** (§2.1): `policy`, `agentrunner`, `hook`, `memory`, `adapter-br`, `adapter-ntm`,
  `handler-impls` comment-marked "reserved for M5" (they matched zero files on disk — false confidence).

### Config validity — the load-bearing check (shared live with P1)
```
.tools/golangci-lint config verify  →  exit 0   (config parses clean)
```
**No unparseable-config risk to P1's builds.**

## 2. Ratchet verification — `golangci-lint run --new-from-rev=origin/main`

Ran the exact CI merge-gate command (after fetching `origin/main`). Result: **grandfathering CONFIRMED.**

- **ZERO unchanged legacy giants flagged.** Explicitly grepped the output for `beadRunOne`,
  `runWorkLoop`, `startWithHooks`, `mergeRunBranchToMain`, `handleSocketConn`, `workloop.go`,
  `daemon/daemon.go`, `cmd/harmonik/main.go` → **none present**. The 104 funcs ≥100 lines are all
  silently grandfathered, as designed.
- **Every finding is on new/changed code** vs `origin/main` — i.e. P1's in-flight edits
  (`internal/keeper/cycle.go`, `internal/replay/*`, `internal/substrate/*`, one new
  `internal/core/*_prop_test.go`). This is the ratchet **working**.
- **The two Track-C complexity findings** (both `gocognit`, the only complexity linter that fired):
  - `internal/replay/replay.go:157` — `Replay` cognitive complexity 48 (> 20)
  - `internal/substrate/replay.go:123` — `(*Twin).replay` cognitive complexity 31 (> 20)
  Both are P1's **brand-new** functions (files absent on `origin/main`) → correct new-code capping.
  **NOT silenced** with `//nolint` (per instruction). These are P1/M3's to refactor or the operator's
  to accept — see §4.

Full finding tally on the changed set (all pre-existing linters except the 2 gocognit): depguard 1,
errcheck 5, exhaustive 1, gocognit 2, gocritic 26, gosec 2, prealloc 2, revive 9, unparam 1 = 49.
The 47 non-complexity findings come from linters that were **already enabled before Track C**; they
would flag P1's changed code regardless of Track C. The depguard finding
(`internal/core/keeperinteriorevents_prop_test.go` importing `pgregory.net/rapid`) is from the
pre-existing `core` rule — **NOT** caused by any Track C edit (my only depguard change to a live rule
was adding an *allowed* edge to `queue`).

**Ratchet on unchanged code: GREEN (grandfathered clean).** Exit 1 is due solely to findings on
new/changed lines — the intended behavior.

## 3. Coverage carve (§3)

- **Pruned stale `HIGH_THRESHOLD_PACKAGES`** in `scripts/coverage-gate.sh`: removed
  `internal/orchestrator` and `internal/reconciler` (both confirmed absent on disk). Remaining
  high-threshold (95%) set: core, workspace, eventbus, handler. `bash -n` clean; gate still runs.
- **Added `check-carve-coverage` Makefile target** (scoped, fast). Measures only the carve targets
  (`internal/substrate`; `internal/runexec` when M3 lands) via `coverage-gate.sh`, inheriting the 90%
  floor + 0.3pp regression gate. Thresholds unchanged (95 spec-core / 90 floor / 0.3pp).
- **Fail-closed tightening (Fable note b):** the doc's snippet used `go test … || true`, which
  swallows compile failures. Replaced with a `set -e` block that only tests carve dirs **present on
  disk** (so absent `runexec` is a clean vacuous pass, NOT a spurious "no such file or directory"
  hard-fail) and lets a genuine compile/test failure propagate. Verified: `make check-carve-coverage`
  tests only `./internal/substrate/...`, then fails on the coverage floor (exit non-zero) — proving
  fail-closed.

### ⚠ NOT wired into `check-short` — would block P1's merge gate
The doc assumed "substrate is the only carve package today → gate is green." **That premise is now
false: `internal/substrate` is at 83.1%, below the 90% floor.** `make check-carve-coverage` is
therefore **RED today**. Wiring it into the shared `check-short` recipe now would break every merge
(including P1's) until substrate coverage reaches 90%. I could not fix that under the hard constraint
(no `internal/**` edits). The target is added to the Makefile (the "scoped merge-time step" exists) but
left OUT of the live `check-short` recipe, with a comment explaining the wire-in condition. Wire it in
once substrate clears 90% (P1's watcher/substrate work is in flight) or seed a substrate baseline
entry and lean on the 0.3pp regression gate.

Note: the existing `coverage.baseline` records many internal packages already well below the 90% floor
(daemon 58.9%, eventbus 58.4%, handler 83.0%), so the FLOOR gate is aspirational repo-wide and is not
run on the merge gate today — consistent with leaving the carve gate unwired for now.

## 4. Follow-ups (not done here — out of scope / need owner)
- File the umbrella grandfather bead `codename:complexity-grandfather` (§4) — deferred (daemon off; no
  bead writes this run).
- The two new gocognit hits on P1's `Replay` / `(*Twin).replay` will trip the merge gate; P1/M3 should
  refactor or the operator accepts them. Left un-silenced per instruction.
- Wire `check-carve-coverage` into `check-short` once substrate ≥ 90%.

## Files left modified in the working tree (all uncommitted)
- `.golangci.yml` — complexity linters + settings + exclusions + queue↔uuid + runexec rule + dead-rule
  comment-marks (present when this run started; verified correct, not re-done).
- `scripts/coverage-gate.sh` — pruned stale orchestrator/reconciler high-threshold entries.
- `Makefile` — added `check-carve-coverage` target (not wired into check-short).
- `plans/2026-07-13-code-revamp/track-c-APPLIED.md` — this record.
