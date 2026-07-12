# Freeze-and-Carve Execution Plan

> **STATUS: FULL DRAFT — ready for independent architecture review.**
> Expanded by `admiral` (2026-07-12, operator-directed "do the work yourself")
> from `REPORT.md` verdicts + the two live exemplars + the `~18:00Z STRATEGIC
> PIVOT` direction-log entry. Supersedes the preserve-before-teardown scaffold
> (`fdf11816`). Next: independent architecture-review/consensus pass → operator
> ratifies → captain re-stands fresh agents (STEP-0 first).

## Inputs (read first)
- `plans/2026-07-12-codebase-census/REPORT.md` — census: verdicts, 5 root
  problems, Moves 1–4, the hard call, Addendum (two live exemplars).
- `.harmonik/context/direction-log.md` — the `2026-07-12 ~18:00Z STRATEGIC PIVOT` entry.

## The thesis in one paragraph
The domain logic is recoverable; the ~466 incident-pinned regression tests are
real value. The disease is two never-made architectural decisions: (a) the daemon
has no internal boundaries (55k LOC, 85-field god-struct, 2,380-line `beadRunOne`
in one flat package), and (b) both IO boundaries to agents (tmux paste-inject) and
remote workers (SSH-string-per-op + box-A mutexes) are ack-free channels that can
only be re-caulked. The cure is **two rebuilds behind existing seams and one large
extraction, under the regression net that already exists** — not a rewrite.

## Global constraints (apply to every move)
- **Keep the proven core. NO big-bang rewrite.** The regression corpus encodes 20
  days of invariants a rewrite would discard.
- Every phase stays behind the existing **~466-test regression net** — green
  throughout, per-commit, never a batch at the end.
- Structural moves that grow real contracts (**M2 / M3 / M4**) are **kerf-first
  (spec-first)**: a kerf work is created, spec drafted and finalized into `specs/`,
  *before* code. This plan names *where* each kerf work goes; it does not create
  them (creation waits on ratification).
- **Pipeline honesty:** STEP-0 must land before anything is trusted to flow through
  the daemon run→QA-gate→resume pipeline. Until STEP-0 is proven, early carve work
  runs **out-of-pipeline** (direct agent + manual land + human merge).
- **Freeze status:** this document is plan-only. No new beads, no dispatch, no
  execution until the operator ratifies and lifts the freeze.

---

## STEP-0 — Prerequisite-zero: make the pipeline trustworthy again
**Why first:** the carve flows *through* the daemon pipeline. Two live exemplars
(REPORT.md Addendum) prove that pipeline currently fabricates both done-status and
in-progress-status. A Move-1 bead dispatched normally would hit the same wedge. So
STEP-0 repairs the instrument before we rely on its readings. **STEP-0 runs
OUT-OF-PIPELINE** (direct agent + manual land) — it cannot dogfood the thing it fixes.

### 0a. Resume-hang / QA-execution-gate relaunch hang  *(highest leverage)*
- **Goal:** a resumed implementer/QA pass must never go dead-silent. A run that
  completes its first `implement` (commit lands in worktree) then relaunches for the
  commit-gate/QA pass currently emits `model_selected → skills_provisioned → SILENCE`
  — no output, no heartbeat, no `run_stale`. Wedged ~5/5 recent local runs.
- **Scope / files:** the agent-relaunch path in `internal/daemon/` — `workloop.go`
  (`beadRunOne` resume branch), `dot_cascade.go`, `reviewloop.go`, and the
  QA-execution-gate workflow landed ~`0adb6551` (implement→commit_gate→review→qa→close).
  Root signal from the hung payload: relaunch is triggered by a **commit-gate failure**
  (`iteration_count:3`), and the *re-launched* agent hangs at startup — so the bug is
  in relaunch-on-gate-fail, independent of the gate reason.
- **DoD:** (1) a run whose first commit-gate fails **either** re-launches and produces
  output/commit **or** emits `run_stale` and is reaped — never silent; (2) a
  heartbeat/liveness guard fires on a resumed agent that produces no output within a
  bounded window; (3) reproduced-then-fixed on ≥3 of the known victims' shapes
  (hk-2i36s, hk-bl4d6, hk-zeo5y class); (4) a regression test asserts the relaunch
  emits a terminal signal (output OR stale) under a stalled-agent fault injection.
- **Key risk:** overlaps `internal/daemon` workloop right-of-way (hawat's historical
  domain). Single-owner this move; do not parallelize inside the daemon core.
- **Pipeline:** OUT-OF-PIPELINE (repairs the pipeline).
- **Owner shape:** internal/daemon relaunch — no hawat collision if serialized.

### 0b. noChange-subsumption false-close  *(data-integrity)*
- **Goal:** the reconcile/close path must not close a bead on a bead-ID **mention**.
  Today `hk-2hfyt` was closed because its ID string appeared in an *unrelated docs
  commit* (`32dc13f7`), with the actual fix verifiably ABSENT.
- **Scope / files:** the subsumption close-path — `internal/daemon/reconciliationcadence_rc020a.go`,
  `orphansweep.go`, `internal/lifecycle/orphansweepbeads.go`, and the noChange branch
  in `workloop.go`/`dot_cascade.go`. Require **fix-content evidence** (a diff touching
  the implicated files / a non-docs commit) before subsumption closes a bead; a bare
  ID mention in a docs/markdown commit must not qualify.
- **DoD:** a docs-only commit mentioning a bead ID does NOT close that bead (regression
  test); subsumption-close requires evidence beyond a string match.
- **Key risk:** over-tightening could *stop* legitimate subsumption closes → beads
  linger open. Tune against the historical close corpus.
- **Pipeline:** OUT-OF-PIPELINE.

### 0c. Re-land the honest-probe fix under a clean ID
- **Goal:** the gb-mbp fleet-down probe bug is still LIVE behind false-closed
  `hk-2hfyt`. The honest-probe guard is absent from `internal/workspace/createworktree.go`
  (no `rev-parse --verify` / `test -e .git` guard).
- **Scope / files:** `internal/workspace/createworktree.go` — add the honest existence
  probe. Carry the apply-spec preserved on `hk-2hfyt`'s comments.
- **DoD:** createworktree verifies the worktree/`.git` actually exists before reporting
  success; a fault-injection test covers the missing-worktree case.
- **Open question (below):** fresh bead ID vs reopen `hk-2hfyt` — resolve so the fix
  itself isn't re-subsumed against the same docs commit. **Recommendation: fresh ID.**
- **Pipeline:** OUT-OF-PIPELINE.

> **STEP-0 exit gate:** all three landed + one clean end-to-end bead run completes
> through the real pipeline (implement→gate→review→qa→close) with a trustworthy close.
> Only then may later moves flow **in-pipeline**.

---

## M1 — Delete the test-theater + dead event-registry surface  *(days)*
- **Goal:** make "green" mean "product code ran." Today ~50k LOC of "tests" never
  `exec` the product, inflating the suite signal while the daemon bled unprotected.
- **Scope / files:**
  - `internal/operatornfr/` (37 test files): **strip** the self-asserting
    fixture-mirror tests that assert their own constants; **keep** the real code +
    real tests — `commandcodes.go`, `exitcode.go`, `securitypolicy`, `sandboxinvariant`.
  - `internal/specaudit/` (132 test files): collapse the markdown-regex spec-prose
    assertions into **one CI lint script outside `go test`**. specaudit is 37k LOC of
    markdown-regex — it belongs in CI lint, not the unit suite.
  - `internal/scenario/`: prune to the **harness + ~11 behavioral files** that execute
    real code (keep `asserteval`, `crashrecovery`, the conformance corpus harness).
  - Dead event-registry surface in `internal/core/`: remove `pertypecompat_hqwn38.go`
    (388-line all-vacuous compat table, zero consumers) + the production-dead
    `DecodePayload` / `ValidateEnvelopeSchemaVersion` decode/validate surface in
    `eventregistry.go` / `eventreg_hqwn59.go`.
- **Dependencies / ordering:** none on M2–M4; benefits from STEP-0 (so deletions can
  flow in-pipeline). **Do M1 early** — every later move relies on trustworthy green.
- **DoD:** `go test ./...` runs only tests that execute product code; the deleted
  surface has zero references; specaudit runs as a standalone CI lint; the
  test/code ratio is a number you can trust.
- **Key risk:** deleting a test that is *actually* load-bearing (a real invariant
  hidden among the theater). Mitigation: per-file classification with sign-off on the
  ambiguous set (see open questions) before bulk delete; delete in reviewable batches.
- **Pipeline:** IN-PIPELINE once STEP-0 lands (pure deletion, guarded by the net). If
  STEP-0 slips, M1's deletions can also run out-of-pipeline (low-risk, human-reviewed).
- **Kerf:** not required (deletion, no new contract).

---

## M2 — Rebuild the agent input channel behind `handler.Substrate`  *(~5 days)*
- **Goal:** replace ack-free tmux paste-injection (44 incident beads, 4 workaround
  generations, ~48 sleep sites) with a **structured-protocol driver**. tmux is retained
  **only as an observation window**, never the input path.
- **Scope / files:** build a second `handler.Substrate` implementation
  (`internal/handler/substrate.go` is the existing seam) driving claude headless
  `stream-json` / Agent SDK stdin (codex-app-server is already under kerf — reuse).
  Port **one harness** end-to-end; run side-by-side with the tmux path; then delete the
  splash-sleep / paste-verify / enter-retry / pgrep stack in `internal/daemon/tmuxsubstrate.go`
  + `internal/lifecycle/tmux/`.
- **Dependencies / ordering:** after M1 (need trustworthy green to validate the port);
  independent of M3/M4 but naturally precedes them (it's Move 2 = cut the incident source).
- **DoD:** one full bead run completes on the structured driver with **zero sleeps and
  zero capture-pane scraping in its path**; the tmux input stack is deleted for the
  ported harness; regression net green.
- **Key risk:** the structured driver has its own liveness/ack semantics — done wrong it
  re-imports the resume-hang. Design the ack/heartbeat contract explicitly in the kerf
  spec (ties to STEP-0a's liveness guard).
- **Pipeline:** IN-PIPELINE after STEP-0.
- **Kerf:** **YES — create a kerf work** (`codename:agent-input-substrate` or similar).
  Spec the Substrate protocol contract (input framing, ack, liveness, observation-only
  tmux) → finalize into `specs/` → then implement.

---

## M3 — Extract the run-lifecycle state machine from `beadRunOne`  *(the big extraction)*
- **Goal:** give fixes a **bounded blast radius**. Replace the 2,380-line `beadRunOne`
  (17 params, `*bool` out-param, mutable closure flags, 85-field `workLoopDeps` passed
  by value) with an explicit `Run` struct + state machine
  (claim → worktree → launch → monitor → gate → merge → close) in its own sub-package.
- **Scope / files:** `internal/daemon/workloop.go` is the source. Extract to a `runexec`
  sub-package with depguard boundaries. **First phase = the merge-coordinator split:**
  `mergeMu` (currently held over network fetch / build IO in `workloop.go` + `daemon.go`)
  becomes an explicit **merge queue**, never held across git push / build / SSH.
- **Dependencies / ordering:** after M2 (fewer moving IO paths to model); **before M4**
  (M4 slots into the Substrate/interface boundaries this move produces). Merge-coordinator
  split is M3-phase-1 and can start as soon as STEP-0 + M1 land.
- **DoD:** `beadRunOne` is a thin driver over the state machine; **no mutex is held
  across git push / build / SSH**; `internal/daemon` has `runexec` + `merge` sub-packages
  with depguard boundaries; **every hk- regression test green throughout** (the net is
  what makes this a refactor, not a rewrite).
- **Key risk:** highest-blast-radius move; 80% of fix commits historically land here, so
  it collides with any concurrent daemon work. Mitigation: **freeze other internal/daemon
  feature work during M3** (this is the whole point of freeze-and-carve); land in small
  state-transition-at-a-time PRs, each green.
- **Pipeline:** IN-PIPELINE after STEP-0 — but single-writer on the daemon core; do not
  fan out parallel agents inside `workloop.go`.
- **Kerf:** **YES — create a kerf work** (`codename:run-state-machine`). Spec the `Run`
  state machine + merge-queue contract → `specs/` → implement phase-by-phase.

---

## M4 — Remote rebuild (worker-resident agent, real protocol)  *(after M3)*
- **Goal:** replace stringly-typed RPC through remote login shells (fresh
  `ssh -- '<string>'` per op, ControlMaster disabled, box-A mutexes owning worker state,
  embedded 68-line Python flock script, 92 `runner != nil` dual-path branches, 166 fix
  commits since 06-20) with a **worker-resident agent speaking a real protocol** and
  **worker-owned worktree lifecycle**.
- **Scope / files:** `internal/workspace/remotematerialize.go`, `createworktree.go`,
  the SSHRunner path, and the 92 dual-path `runner != nil` sites. Collapse Local/Remote
  into two implementations of **one interface** — the Substrate boundary M3 produces.
- **Dependencies / ordering:** **hard dependency on M3** (the Substrate/interface must
  exist first). Last move.
- **DoD:** remote and local runs go through **one interface** (no `runner != nil`
  branching); no box-A mutex owns worker-side state; the embedded flock script is gone;
  a remote bead run completes with worker-owned worktree lifecycle; regression net green.
- **Key risk:** remote is the least-observable path; the rebuild needs a real
  integration test harness (scenario package) exercising a remote worker, or it re-enters
  "can't tell if it works." Budget for that harness inside M4.
- **Pipeline:** IN-PIPELINE after STEP-0 + M3.
- **Kerf:** **YES — create a kerf work** (`codename:remote-substrate`). Spec the
  worker-agent protocol + worktree ownership → `specs/` → implement.

---

## Carve-and-protect targets (parallel hygiene, not a numbered move)
Per REPORT.md §5 — the proven core to protect while the moves run. Fold these in as
each adjacent move touches them; none is big enough to sequence separately:
- **queue** (`internal/queue/`): the one mutex-free, spec-pinned, tested island. Fix its
  **two-writer lost-update path** (`internal/queue/rpc.go` ~:1016) and **evict the
  `HandlerAdapter` grab-bag** that daemon knobs are colonizing. Treat as load-bearing.
  *(This is a natural STEP-0 / M3 neighbor — the two-writer fix is data-integrity like 0b.)*
- **lifecycle-reconcile**: extend the half-existing intent log (BI-031); do not build new.
- **daemon-harness**: right axes; fix claude bypassing its own interface + the 380-line
  codex WAL guard as part of M2.

---

## Ordering summary
```
STEP-0  (0a resume-hang · 0b false-close · 0c honest-probe)   OUT-OF-PIPELINE
   │     exit gate: one trustworthy end-to-end pipeline run
   ▼
M1  delete test-theater + dead event-registry      IN-PIPELINE (or OOP if STEP-0 slips)
   │
   ▼
M2  agent-input Substrate rebuild        kerf: agent-input-substrate     IN-PIPELINE
   │
   ▼
M3  run-state-machine extraction         kerf: run-state-machine         IN-PIPELINE, single-writer
   │   (phase 1 = merge-coordinator split; can start after STEP-0+M1)
   ▼
M4  remote rebuild                       kerf: remote-substrate          IN-PIPELINE (needs M3)
```
Queue two-writer fix + HandlerAdapter eviction ride alongside STEP-0/M3.

---

## OPEN QUESTIONS (need an operator decision)
1. **STEP-0 owner & mode.** STEP-0 runs out-of-pipeline (direct agent + manual land).
   Who drives it — a single re-stood crew (stilgar owned the resume-hang diagnosis;
   hawat owned the false-close finding), or does the admiral direct one focused crew?
   And do we re-stand the fleet *only* for STEP-0, or stay agent-by-agent until M1?
2. **hk-2hfyt disposition (0c).** Fresh clean bead ID (recommended — avoids re-subsuming
   against docs commit `32dc13f7`) vs reopen `hk-2hfyt`? Beads were clean-slated to 267
   closed / `hk-8vnwg` kept — the re-land bead is effectively a **new** bead regardless.
3. **M1 test-theater sign-off.** Which `operatornfr` / `specaudit` tests are genuinely
   load-bearing vs theater? Recommend a one-pass classification (admiral or a review
   crew) producing a keep/delete list for operator ratification *before* bulk delete —
   deletion is irreversible-ish and the census confidence is "mostly delete," not "all."
4. **Kerf timing.** Create the three kerf works (M2/M3/M4) up front at ratification, or
   just-in-time as each move starts? Recommend JIT (create `agent-input-substrate` when
   M2 begins) to avoid stale specs, but decide `codename:` labels now for tracking.
5. **Freeze scope during M3.** M3 requires freezing *other* internal/daemon feature work
   (80% of fix commits land in `workloop.go`). Confirm the operator accepts a hard daemon
   feature-freeze for M3's duration — this is the crux of freeze-and-carve.
6. **Re-stand sequencing.** The chain is: ratify → captain re-stands fresh agents,
   STEP-0 first. Confirm the captain re-stands with the STEP-0 fix in hand (per direction
   log) rather than resuming the old wedged fleet.

---

## Review status
- [ ] Independent architecture-review / consensus pass (admiral) — pending
- [ ] Operator ratification — pending
- [ ] Freeze lifted for STEP-0 — pending

*On acceptance: create the STEP-0 out-of-pipeline work first; do not dispatch any
carve bead through the daemon pipeline until STEP-0's exit gate is met.*
