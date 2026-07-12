# Freeze-and-Carve Execution Plan

> **STATUS: DRAFT v2 — reviewed by an independent 3-lens architecture panel.**
> Authored by `admiral` (operator-directed) from `REPORT.md` + the two live
> exemplars + the `~18:00Z STRATEGIC PIVOT` direction-log entry. v2 folds in an
> independent review panel (sequencing · scope-accuracy · strategic-risk). The
> panel's central finding — *every DoD was "one clean run," which re-imports the
> "green means nothing" disease* — is resolved by the **Acceptance Oracle** below.
> Next: operator ratifies → captain re-stands fresh agents (STEP-0 first).
> History: scaffold `fdf11816` → full draft `6fd2e65b` → this v2.

## Inputs (read first)
- `plans/2026-07-12-codebase-census/REPORT.md` — census: verdicts, 5 root
  problems, Moves 1–4, the hard call, Addendum (two live exemplars).
- `.harmonik/context/direction-log.md` — the `2026-07-12 ~18:00Z STRATEGIC PIVOT` entry.

## The thesis in one paragraph
The domain logic is recoverable; the ~466 incident-pinned regression tests are
real value. The disease is two never-made architectural decisions: (a) the daemon
has no internal boundaries (55k LOC, 85-field god-struct, ~2,380-line `beadRunOne`
in one flat package — verified `internal/daemon/workloop.go:3072`), and (b) both IO
boundaries to agents (tmux paste-inject) and remote workers (SSH-string-per-op +
box-A mutexes) are ack-free channels that can only be re-caulked. The cure is **two
rebuilds behind existing seams and one large extraction, under the regression net
that already exists** — not a rewrite.

---

## The Acceptance Oracle (READ THIS BEFORE ANY MOVE)
> Added in v2 to answer the panel's #1 finding and the operator's core anxiety
> ("I don't know if what it's fixing is real"). This is the **standard of proof**
> every move's Definition-of-Done must meet. It is deliberately stricter than
> "tests pass," because the census proved a green suite can be theater (REPORT
> §4) and a single string-match can fabricate a close (REPORT Addendum).

A move is **DONE** only when ALL of the following hold — never on a single green run:

1. **Repetition, not luck.** The move's happy path completes **N consecutive clean
   runs** (default **N=10**; operator may set per-move) — because the failures we
   are fixing were non-deterministic (resume-hang wedged 5/5, REPORT §4 Addendum),
   so one pass cannot distinguish a fix from luck.
2. **Fault injection, not just happy path.** A deliberate fault (stalled agent on
   relaunch, missing worktree, killed worker, docs-only commit) is injected and the
   system produces a **terminal signal** (output OR `run_stale` OR an honest error)
   — never silence, never a fabricated close.
3. **Out-of-band verification, not self-report.** "Done" is confirmed by evidence
   that does **not** route through the pipeline path being fixed — a diff-content
   assertion, a coverage number, a direct filesystem/git check — because the thing
   under repair cannot be its own oracle (STEP-0's close path judged by the close
   path is circular).
4. **Coverage floor on the carve target.** For any move that refactors daemon core,
   the touched code path has **measured** line/branch coverage from tests that
   actually `exec` the product — not merely "the 466-test suite is green" (the net's
   coverage over `workloop.go` is currently unmeasured; see M1→M3 coverage audit).

Every per-move DoD below is written against this oracle. Where a move says "one run,"
read it as "N consecutive + fault-injection + out-of-band check" per this section.

## Global constraints (apply to every move)
- **Keep the proven core. NO big-bang rewrite.** The regression corpus encodes 20
  days of invariants a rewrite would discard.
- Every phase stays behind the existing regression net — **green per-commit**, and,
  per the Oracle, backed by a **measured coverage floor** on the carve target.
- Structural moves that grow real contracts (**M2 / M3 / M4**) are **kerf-first**: a
  kerf work is created, spec drafted and finalized into `specs/`, before code. This
  plan names *where* each kerf work goes; it does not create them (creation waits on
  ratification, JIT per move).
- **Single-writer on daemon core.** Any move that edits `internal/daemon/workloop.go`
  (**STEP-0a and M3**) is single-owner — no parallel agents inside the daemon core.
- **Pipeline honesty.** STEP-0 must land before anything is trusted to flow through
  the daemon run→QA-gate→resume pipeline. Until then, early work runs out-of-pipeline.
- **Freeze status:** plan-only. No new beads, no dispatch, no execution until the
  operator ratifies and lifts the freeze.

---

## STEP-0 — Prerequisite-zero: make the pipeline trustworthy again
**Why first:** the carve flows *through* the daemon pipeline, and two live exemplars
(REPORT Addendum) prove that pipeline currently fabricates both done-status and
in-progress-status. **STEP-0 runs OUT-OF-PIPELINE** — but NOT via the discredited
"manual salvage" path crews were forced into (REPORT §4 Addendum). Out-of-pipeline
here means: direct agent + **human-reviewed merge** + **out-of-band fix-present
verification** (per Oracle #3), never a pipeline self-close.

### 0a. Resume-hang / QA-execution-gate relaunch hang  *(highest leverage)*
- **Goal:** a resumed implementer/QA pass must never go dead-silent. Today a run
  whose first commit-gate fails (`iteration_count:3`) re-launches the agent, which
  hangs at startup (`model_selected → skills_provisioned → SILENCE`) — no output, no
  heartbeat, no `run_stale`. The bug is in relaunch-on-gate-fail, independent of the
  gate reason.
- **Scope / files:** the agent-relaunch path in `internal/daemon/` — the `beadRunOne`
  resume branch (`workloop.go`), `dot_cascade.go`, `reviewloop.go`, and the
  QA-execution-gate workflow landed ~`0adb6551`.
- **DoD (per Oracle):** a **single deterministic fault-injection test** — stalled
  agent on relaunch → asserts a terminal signal (`output` OR `run_stale`) within a
  bounded liveness window — plus **N=10 consecutive** clean commit-gate-fail→relaunch
  cycles with zero silent hangs. (Drop the earlier "3 historical victims" language —
  those are runs, not fixtures.)
- **Key risk:** highest-blast-radius file; single-writer, no parallel daemon agents.
- **Pipeline:** OUT-OF-PIPELINE.

### 0b. noChange-subsumption false-close  *(data-integrity)*
- **Goal:** the reconcile/close path must not close a bead on a bead-ID **mention**.
  `hk-2hfyt` was closed because its ID appeared in an unrelated docs commit
  (`32dc13f7`) with the fix ABSENT.
- **Scope / files:** the subsumption close-path —
  `internal/daemon/reconciliationcadence_rc020a.go`, `orphansweep.go`,
  `internal/lifecycle/orphansweepbeads.go`, and the noChange branch in
  `workloop.go`/`dot_cascade.go`. Require **fix-content evidence** (a diff touching
  implicated files / a non-docs commit); a bare ID mention in a docs commit must not
  qualify.
- **DoD (per Oracle #3 — out-of-band):** a docs-only commit mentioning a bead ID does
  NOT close that bead (regression test); and STEP-0's own exit gate is verified by a
  **diff-content assertion against the actual files**, NOT by the reconcile path's
  "closed" status (which is the thing under repair). Tune against the historical close
  corpus so legitimate subsumption closes still fire.
- **Key risk:** over-tightening → beads linger open. Validate both directions.
- **Pipeline:** OUT-OF-PIPELINE.

### 0c. Re-land the honest-probe fix under a clean ID
- **Goal:** the gb-mbp fleet-down probe bug is still live behind false-closed
  `hk-2hfyt`. **NOTE (review correction):** `createworktree.go` already has a partial
  probe — `resolveWorktreeHEADViaRunner` (~:141) runs `git -C <wt> rev-parse HEAD` and
  validates non-empty HEAD (~:252) with `cleanupPartialState`. So the fix is NOT a
  blank slate; **first task is to re-derive the exact remaining gap** (stronger
  `--verify` / `.git`-stat probe vs the existing HEAD check) before scoping.
- **Scope / files:** `internal/workspace/createworktree.go` — close the re-derived gap;
  carry the apply-spec preserved on `hk-2hfyt`'s comments.
- **DoD (per Oracle):** the re-derived gap is documented, then closed; a
  fault-injection test covers the missing-worktree/`.git` case and asserts an honest
  failure (not a false success).
- **Cross-move note:** **M4 rewrites this same file** for worker-owned worktrees — M4
  MUST carry 0c's guard forward (see M4). Flag this in the M4 kerf spec.
- **Pipeline:** OUT-OF-PIPELINE.

> **STEP-0 exit gate:** all three landed + **N consecutive** trustworthy end-to-end
> pipeline runs, each confirmed by an **out-of-band** fix-present check (not a
> pipeline self-close). Only then may later moves flow **in-pipeline**.

---

## M1 — Delete the test-theater + dead event-registry surface  *(days)*
> **Sequencing (review change):** M1's file set (operatornfr / specaudit / scenario /
> core) is **disjoint** from STEP-0's (daemon / workspace), so M1 runs
> **concurrently with STEP-0, out-of-pipeline** — it is NOT behind STEP-0's exit gate.
> This removes M1 from the critical path.

- **Goal:** make "green" mean "product code ran."
- **Scope / files:**
  - `internal/operatornfr/` (37 test files): strip the self-asserting fixture-mirror
    tests; **keep** `commandcodes.go`, `exitcode.go`, `securitypolicy_on006_on026.go`,
    `sandboxinvariant_on024.go` + their real tests (all verified present).
  - `internal/specaudit/` (132 test files, ~37.6k LOC verified): collapse the
    markdown-regex assertions into **one CI lint script outside `go test`**.
  - `internal/scenario/`: prune to the harness + ~11 behavioral files that `exec`
    real code (keep `asserteval`, `crashrecovery`, the conformance-corpus harness).
  - Dead event-registry surface in `internal/core/`: remove `pertypecompat_hqwn38.go`
    (388 lines, verified all-vacuous, zero consumers) + the production-dead
    `DecodePayload` / `ValidateEnvelopeSchemaVersion` surface (verified: reached only
    via `DispatchObservational/Synchronous`, which have zero non-test callers).
- **Dependencies / ordering:** none on M2–M4; runs parallel to STEP-0.
- **DoD (per Oracle #4 — mechanical, not vibes):** (1) an explicit **allowlist** of
  files moved to the CI-lint script; (2) a **coverage gate** — every *retained*
  `_test.go` package shows non-zero product coverage under `go test -coverpkg=./internal/...`;
  (3) the deleted surface has zero remaining references. ("test/code ratio you can
  trust" is the intent; the coverage gate is the check.)
- **Key risk:** deleting a genuinely load-bearing test hidden among the theater.
  Mitigation: per-file keep/delete classification with operator sign-off on the
  ambiguous set (open question #4) **before** bulk delete; delete in reviewable batches.
- **Pipeline:** OUT-OF-PIPELINE, concurrent with STEP-0 (low-risk, human-reviewed
  deletion). May shift in-pipeline once STEP-0's gate is met.
- **Kerf:** not required (deletion, no new contract).

### M1→M3 coverage audit (gate, review-added)
Between M1 and M3, run a **coverage audit of `internal/daemon` workloop** and record
line/branch coverage of the run-lifecycle path. **M3 is gated on demonstrated coverage
of the state-machine path**, not on "466 tests pass" — because if the real coverage
lived in the pruned scenario/operatornfr files, the net has a hole exactly where M3's
highest-blast-radius work lands.

---

## M2 — Rebuild the agent input channel behind `handler.Substrate`  *(estimate: revisit — see risk)*
- **Goal:** replace ack-free tmux paste-injection with a **structured-protocol driver**;
  tmux retained **only as an observation window**. Incident weight: 44 tmux beads, 4
  workaround generations. *(Review correction: the motivating "~48 sleep sites" was a
  test-file count; the live path — `tmuxsubstrate.go` + `pasteinject.go` — has ~1 prod
  sleep. The case for rebuild rests on ack-freeness + incident density, not sleep count.)*
- **Scope / files:** build a second `handler.Substrate` impl (seam:
  `internal/handler/substrate.go`) driving claude headless `stream-json` / Agent SDK
  stdin (codex-app-server already under kerf — reuse). Port **one harness** end-to-end;
  run side-by-side; then delete the input stack in `internal/daemon/tmuxsubstrate.go`
  (~2.7k LOC) + `internal/lifecycle/tmux/`.
- **Dependencies / ordering:** after STEP-0's gate. Injects at the already-clean
  `LaunchSpec.Substrate` seam, so it does NOT reach into `beadRunOne` internals — the
  god-function's mess does not make M2 riskier (panel confirmed M2-before-M3 is correct).
- **DoD (per Oracle — matches M4's harness bar):** M2 gets the **same fault-injecting
  integration harness M4 requires** — a stalled-agent injection asserting the new
  substrate emits **output-or-stale** — as a DoD item, **not** a spec paragraph. Plus
  **N consecutive** full bead runs on the structured driver with **zero sleeps and zero
  capture-pane scraping** in its path.
- **Rollback (review-added):** define explicit **abort/rollback criteria + a bake
  window** before deleting the tmux input stack. Do NOT delete the escape hatch on a
  single ported run.
- **Key risk:** a wrong ack/heartbeat contract re-imports the resume-hang on a
  substrate whose escape hatch is deleted. Mitigation: the fault-injection harness
  (above) + bake window are the guard; the kerf spec defines the ack/liveness contract
  and ties it to STEP-0a's liveness guard. **Sizing:** replacing ~5.4k LOC of live IO
  substrate with a new protocol driver + side-by-side port is **materially more than a
  few days** — set the estimate during the kerf design pass, not now.
- **Pipeline:** IN-PIPELINE after STEP-0.
- **Kerf:** **YES** — `codename:agent-input-substrate`. Spec the Substrate protocol
  (input framing, ack, liveness, observation-only tmux) → `specs/` → implement.

---

## M3 — Extract the run-lifecycle state machine from `beadRunOne`  *(the big extraction)*
- **Goal:** bounded blast radius. Replace the ~2,380-line `beadRunOne` (17 params,
  `*bool` out-param, 85-field `workLoopDeps` by value — all verified) with an explicit
  `Run` struct + state machine (claim → worktree → launch → monitor → gate → merge →
  close) in its own sub-package.
- **Scope / files:** source is `internal/daemon/workloop.go`. Extract to a `runexec`
  sub-package with depguard boundaries. **Phase 1 = the merge-coordinator split:**
  `mergeMu` (verified `daemon.go:619`, held over network fetch / build IO) becomes an
  explicit **merge queue**, never held across git push / build / SSH.
- **Dependencies / ordering:** after M2 (fewer live IO paths to model). **M3-phase-1
  (merge split) can start right after STEP-0 + M1** (it does not need M2) — this is the
  one legitimate parallelism in the plan. Gated on the M1→M3 coverage audit.
- **DoD (per Oracle #3/#4 — mechanical):** (1) `runexec` + `merge` sub-packages exist
  with **depguard entries in `.golangci.yml`** (objectively checkable); (2) a
  **static/race lint or test that FAILS if `mergeMu` is held across an IO call**
  (replaces the un-assertable "no mutex held across IO" prose); (3) `beadRunOne` reduced
  to a thin driver — **assert a hard line ceiling** (e.g. ≤ N lines, dispatch delegated
  to state-machine transitions) rather than "thin"; (4) the state-machine path meets the
  **coverage floor** from the M1→M3 audit; (5) every hk- regression test green throughout.
- **Key risk:** 80% of fix commits historically land here → collides with any concurrent
  daemon work. Mitigation: the global daemon feature-freeze (this is the crux of
  freeze-and-carve) + single-writer + small state-transition-at-a-time PRs, each green.
- **Pipeline:** IN-PIPELINE after STEP-0; single-writer on daemon core.
- **Kerf:** **YES** — `codename:run-state-machine`. Spec the `Run` state machine +
  merge-queue contract → `specs/` → implement phase-by-phase.

---

## M4 — Remote rebuild (worker-resident agent, real protocol)  *(after M3)*
- **Goal:** replace stringly-typed RPC through remote login shells (fresh
  `ssh -- '<string>'` per op, ControlMaster disabled, box-A mutexes owning worker
  state, an embedded Python flock script — verified `remotematerialize.go:293–329`,
  `fcntl.flock`) with a **worker-resident agent speaking a real protocol** and
  **worker-owned worktree lifecycle**.
- **Scope / files:** `internal/workspace/remotematerialize.go`, `createworktree.go`,
  the SSHRunner path, and the dual-path branches. *(Review correction on the count:
  bare `runner != nil` non-test = ~49 sites; ~92 only when `runner == nil` is included.
  M4 must collapse **both** arms — the DoD says no `runner`-nil branching **either
  direction**, not just `!= nil`.)*
- **Dependencies / ordering (review-sharpened — split into two):**
  - **(a) Hard dependency on M3-phase-1's merge-queue** — remote merges must thread
    through the merge queue, so mergeMu must no longer be held over network IO first.
  - **(b) M4 owns its own Local/Remote execution-interface extraction.** M3's DoD
    produces `runexec` + `merge` sub-packages but does NOT inherently emit a
    Local/Remote *execution* interface, and 13/15 `runner`-nil sites live outside the
    function M3 refactors. So M4 must **extract that interface itself** — do not assume
    M3 hands it over.
- **DoD (per Oracle):** (1) remote and local runs go through **one interface** (no
  `runner`-nil branching, either arm); (2) no box-A mutex owns worker-side state; (3)
  the embedded Python flock script is gone; (4) **STEP-0c's honest-probe guard is
  preserved** in the rewritten `createworktree.go` (explicit carry-forward); (5) **N
  consecutive** remote bead runs complete with worker-owned worktree lifecycle,
  validated by the **remote fault-injection harness** below; (6) regression net green.
- **Key risk:** remote is the least-observable path — without a real integration
  harness it re-enters "can't tell if it works." **Budget the scenario-package remote
  worker harness inside M4** as a first-class deliverable, not an afterthought.
- **Pipeline:** IN-PIPELINE after STEP-0 + M3-phase-1.
- **Kerf:** **YES** — `codename:remote-substrate`. Spec the worker-agent protocol +
  worktree ownership (incl. the 0c guard carry-forward) → `specs/` → implement.

---

## Carve-and-protect targets (parallel hygiene, not a numbered move)
Per REPORT §5 — fold in as each adjacent move touches them:
- **queue** (`internal/queue/`): the mutex-free, spec-pinned, tested island. Fix its
  **two-writer lost-update path** (`internal/queue/rpc.go` ~:1016, verified) and evict
  the **`HandlerAdapter` grab-bag** daemon knobs are colonizing. The two-writer fix is
  data-integrity like 0b — a natural STEP-0 neighbor.
- **lifecycle-reconcile**: extend the half-existing intent log (BI-031); don't build new.
- **daemon-harness**: fix claude bypassing its own interface + the 380-line codex WAL
  guard as part of M2.

---

## Ordering summary
```
        ┌─ STEP-0  (0a resume-hang · 0b false-close · 0c honest-probe)  OUT-OF-PIPELINE
        │      exit gate: N trustworthy end-to-end runs, out-of-band verified
        ├─ M1   delete test-theater + dead registry   OUT-OF-PIPELINE, CONCURRENT w/ STEP-0
        │
        ▼   (STEP-0 gate met)
   M1→M3 coverage audit  ── gates M3
        │
        ▼
   M2  agent-input Substrate      kerf: agent-input-substrate     IN-PIPELINE
        │            (M3-phase-1 merge split may start here, after STEP-0+M1)
        ▼
   M3  run-state-machine          kerf: run-state-machine         IN-PIPELINE, single-writer
        │
        ▼
   M4  remote rebuild             kerf: remote-substrate          IN-PIPELINE
             depends on: M3-phase-1 merge-queue (hard) + owns its own Local/Remote interface
```
Queue two-writer fix + HandlerAdapter eviction ride alongside STEP-0/M3.

---

## OPEN QUESTIONS (need an operator decision)
1. **[NEW — the crux] What is the independent acceptance oracle?** The Acceptance
   Oracle above proposes: N=10 consecutive runs + fault-injection + out-of-band
   diff/coverage verification. **Confirm or set the standard of proof** — this is the
   literal statement of "I don't know if what it's fixing is real." Every DoD depends
   on your answer (e.g. is N=10 right? is a coverage floor required, and at what %?).
2. **STEP-0 owner & mode.** STEP-0 runs out-of-pipeline (direct agent + human-reviewed
   merge). Single re-stood crew (stilgar owned the resume-hang, hawat the false-close),
   or admiral directs one focused crew? Re-stand fleet only for STEP-0, or stay
   agent-by-agent until M1?
3. **hk-2hfyt disposition (0c).** Fresh clean bead ID (recommended — avoids re-subsuming
   against docs commit `32dc13f7`) vs reopen `hk-2hfyt`? Beads were clean-slated
   (267 closed / `hk-8vnwg` kept), so the re-land is a **new** bead regardless.
4. **M1 test-theater sign-off.** Approve the keep/delete classification of
   `operatornfr` / `specaudit` tests before bulk delete? Recommend a one-pass
   classification → operator ratifies the ambiguous set (census confidence is "mostly
   delete," not "all"; deletion is hard to reverse).
5. **Kerf timing.** Create the three kerf works up front at ratification, or JIT per
   move (recommended, avoids stale specs)? Decide the `codename:` labels now regardless.
6. **Freeze scope during M3.** M3 needs a hard freeze of *other* internal/daemon feature
   work (80% of fix commits land in `workloop.go`). Confirm you accept this — it is the
   crux of freeze-and-carve.
7. **Re-stand sequencing.** Confirm the captain re-stands fresh agents with the STEP-0
   fix in hand (per direction log), rather than resuming the old wedged fleet.

---

## Review status
- [x] Independent architecture-review / consensus pass (3-lens panel: sequencing ·
      scope-accuracy · strategic-risk) — **complete; folded into v2.**
- [ ] Operator ratification — pending (see 7 open questions, #1 is the crux)
- [ ] Freeze lifted for STEP-0 — pending

*On acceptance: settle the Acceptance Oracle (Q1) first — it defines every DoD — then
create the STEP-0 out-of-pipeline work + M1 concurrently. Do not dispatch any carve
bead through the daemon pipeline until STEP-0's exit gate is met.*
