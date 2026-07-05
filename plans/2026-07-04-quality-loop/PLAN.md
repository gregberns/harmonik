# Quality Loop — Root-Cause Plan

**Date:** 2026-07-04 · **Author:** admiral (fleet), on operator directive · **Status:** DRAFT for operator review

---

## The verdict (read this first)

The problem is **not** that harmonik lacks tests. It has a large, genuinely substantial test corpus:
~9,300 test functions across 1,480 test files, 58 scenario files, a coverage gate (95%/90%), a real
end-to-end smoke test, and a fully-built isolated **scratch-daemon harness** that can run workflows end-to-end
and file failures back as beads.

**The problem is that every single quality gate is advisory, fail-open, or masked — and the green checkmark
is fabricated.** The system runs its tests, they fail, and the failure is suppressed in *eleven independent
places*. Nobody has seen a red scenario run in over a month because the pipeline is engineered not to show one.

Your instinct ("this was never tested") is directionally right but the mechanism is worse: **it *is* tested,
on every push, and we are being lied to about the result.** That is a much more fixable problem than "write a
test suite" — the suite exists. We have to stop suppressing it, fix the handful of real failures it's hiding,
and make it block.

**This plan is deliberately NOT "add a couple tests here and there"** — that was the failure mode you predicted
and forbade. Almost none of the work below is writing new tests. It is: turn OFF the suppression, fix the real
failures it hides, and turn ON enforcement — in an order that doesn't brick the fleet.

---

## The eleven suppression points (evidence)

| # | Where the truth is hidden | Evidence |
|---|---|---|
| 1 | CI unit/short job can't fail the run | `.github/workflows/ci.yml:18` — `continue-on-error: true` |
| 2 | Scenario job can't fail the run | `.github/workflows/scenario.yml:31` — `continue-on-error: true` |
| 3 | 42 daemon-resident scenario tests **actually fail every run** (600s timeout → `panic: test timed out` → `Error 2`), masked green by #2 | Identical failure across last 5 CI runs; `Makefile:92` bundles `./internal/daemon/...` into the 10-min scenario budget |
| 4 | Per-commit scenario gate is **fail-open by design** — timeouts/crashes/build-failures print `ALLOWING merge (fail-open)` and exit 0 | `scripts/scenario-gate.sh`, `internal/daemon/scenariogate.go` |
| 5 | **No branch protection at all** on `main` | `gh api …/branches/main/protection` → `404 Branch not protected` |
| 6 | 110 real-daemon E2E tests are **skipped in CI** (CI runs `go test -short`; these `t.Skip` under `-short`) | `internal/daemon/shortskip_hkp258q_test.go`, 110 call sites / 65 files |
| 7 | `kerf finalize` **runs zero tests**; its only gate (`kerf square`) checks status-index + file-existence + deps — purely structural | `kerf/cmd/finalize.go:65-84`, `kerf/cmd/square.go:51-56` |
| 8 | **No epic-acceptance gate** — "all beads closed" = done; the only spec-compliance record is a `03-verify.md` the agent writes and grades itself | `kerf/specs/jig-implementation.md`, jig-system.md |
| 9 | The one epic-ish gate (validation-test requirement) only checks a scenario-test **ID was typed into a file**, not that a passing test exists — kerf spec admits this in writing (cites incident `hk-37zy8`) | `kerf/specs/jig-system.md` §"What this does not guarantee" |
| 10 | **Release has no test gate.** goreleaser's only pre-build hook is `go mod tidy`; release.yml is not gated on CI. Workflow header: *"Dogfooding IS the validation."* | `.goreleaser.yaml:12-14`, `.github/workflows/release.yml`, `specs/release-pipeline.md §1` |
| 11 | The in-place daemon redeploy validates with a **socket dial only** — health-window/last-good prove the process is up, never that dispatch/merge/review work. The one real functional check (`harmonik smoke`) is **orphaned** — never invoked by release, redeploy, or watchdog | `internal/supervise/daemon_watchdog.go:347-356`, `cmd/harmonik/smoke.go` |

Supporting: coverage gate `scripts/coverage-gate.sh` (95%/90%) exists but is only in `make check`, which CI
never calls. `agent-reviewer`'s test-adequacy check is subjective, same-context self-review, and its
`REQUEST_CHANGES` verdict is author-overridable by writing a rationale sentence.

---

## Root cause

**Harmonik has quality *machinery* but no quality *authority*.** Every gate was built with an escape hatch —
`continue-on-error`, fail-open, no branch protection, agent-overridable verdicts, structural-only kerf checks,
"dogfooding is the validation." Each escape hatch was individually reasonable ("don't let a flaky test block the
self-improving fleet"), but together they mean **no failing test can ever stop bad code from reaching
production.** The fleet then spends its throughput finding those defects one at a time, live — the treadmill.

The fix is to convert the machinery from advisory to authoritative, in an order that fixes the real hidden
failures *before* turning on enforcement (otherwise enforcement bricks the fleet's own merge flow).

---

## The plan — five phases, sequenced so we never halt the fleet

> **Critical sequencing rule:** do NOT flip any gate to blocking until the real failures it currently hides are
> fixed. Turning on scenario-blocking *today* — while the 42 daemon tests time out — would block every merge and
> halt the fleet. Fix first (Phase 1), then enforce (Phase 3).

### Phase 0 — See the truth (½ day, do immediately)
Make the real signal visible without yet blocking anything.
- Add a **reporting-only** CI step that runs the tests and posts the *real* pass/fail to the PR/commit (a
  status comment or separate non-`continue-on-error` job that we read but don't gate on yet).
- Stand up a dashboard/log of "what actually failed in the last N runs" so we're working from truth, not the
  fabricated green.
- **Deliverable:** a daily honest red/green, for the first time in a month.

### Phase 1 — Fix the failures the mask is hiding (the real bug work; 3–5 days)
- **Fix the daemon-scenario 600s timeout (point #3).** Split `./internal/daemon/...` out of the 10-minute
  scenario budget, or shard it, or fix the hang that panics it. Until this is green, nothing downstream can gate.
- **Triage the 110 skipped real-daemon E2E tests (point #6):** run them off `-short`, fix or quarantine the
  genuinely-flaky ones with a tracked bead each (no silent skips).
- **Backfill the 9 packages with zero tests** — but only the ones on the critical dispatch/merge/review path;
  this is targeted, not blanket.
- **Deliverable:** `make test-scenario` and the real-daemon E2E set pass green *honestly*, repeatably.

### Phase 2 — Wire the orphaned functional check into the ship path (1–2 days)
- **Gate the in-place redeploy on `harmonik smoke`** — no live binary swap until the 5-signal end-to-end smoke
  passes against the new binary. This closes point #11.
- **Gate release/promote on the real test run** — promote's build-only check (`go build && go vet`) becomes
  build + test + smoke.
- **Deliverable:** a binary cannot reach the fleet without proving dispatch→merge→review actually work.

### Phase 3 — Turn on enforcement (½ day, ONLY after Phase 1 is green)
- Remove `continue-on-error: true` from `ci.yml` and `scenario.yml` (points #1, #2).
- Make the scenario commit-gate **fail-closed** (point #4).
- Add **branch protection** on `main` with required status checks (point #5).
- Wire the **coverage gate into CI** (currently never runs).
- **Deliverable:** failing or undertested code mechanically cannot land. The escape hatches are gone.

### Phase 4 — Close the kerf epic-acceptance gap (2–3 days)
- **`kerf finalize` / epic-close requires a *passing* scenario test**, not a filed ID (points #7–#9). The
  validation-test requirement must query the tracker and confirm the referenced scenario test exists, exercises
  an integration surface, and is green — the `hk-37zy8` failure mode closed.
- Make `agent-reviewer`'s test-adequacy verdict non-overridable by the same author for `bug`/epic-close beads.
- **Deliverable:** an epic cannot be marked done until something proves the assembled epic works end-to-end.

### Phase 5 — The durable fix: continuous self-test daemon (3–4 days; ~80% already built)
This is the mechanism that makes the whole system self-checking instead of relying on humans noticing.
- The **scratch-daemon harness is already shipped** (`scripts/scratch-daemon.sh`: isolated second daemon from a
  separate clone, `batch` runs workflows end-to-end and emits structured pass/fail, `feedback` files failures as
  deduped fleet beads). The **scheduler is already shipped** (`internal/schedule/`, fixed-interval jobs firing
  arbitrary commands). No architectural blocker — isolation is by separate clone; the "two daemons, same repo"
  dead end was already ruled out.
- **What's missing (small):** (a) a wrapper `selftest-cycle.sh` = pull latest main → rebuild → `batch <corpus>`
  → `feedback` on fail → `comms-send` alert; (b) a **curated, versioned corpus of real production workflows**
  (today only throwaway beads exist — this is the one piece of real authoring); (c) **one seeded schedule job**
  (hourly or nightly) reusing the goal-keeper seeding pattern.
- **Deliverable:** every hour/night, an isolated daemon runs real workflows against latest main end-to-end; red
  runs file a bead and ping the captain; green runs are silent. The system finds its own regressions before a
  release does.

---

## What we are explicitly NOT doing
- Not writing a big new unit-test suite — the corpus exists; we're un-suppressing it.
- Not flipping gates to blocking before their hidden failures are fixed (that would halt the fleet).
- Not treating the fabricated green as a data source — Phase 0 replaces it with truth first.

## Sequencing summary
`Phase 0 (see truth)` → `Phase 1 (fix hidden failures)` → `Phase 2 (gate ship path)` **‖** `Phase 4 (kerf gate)`
→ `Phase 3 (enforce, only after P1 green)` → `Phase 5 (continuous self-test daemon = durable fix)`.
Phase 5 can start in parallel any time (its primitives are built); the corpus authoring is the long pole.

## Proposed next step
File this as a kerf epic (`codename:quality-loop`) with Phase 0/1/3 as the first tranche (they're the
un-suppression and are unambiguous), and dispatch Phase 0 immediately. Phases 4 and 5 get their own specs.
Awaiting operator go on scope/sequence before turning any gate to blocking (Phase 3), since that changes the
fleet's own merge flow.
