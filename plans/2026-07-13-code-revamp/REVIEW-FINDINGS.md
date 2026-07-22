# Code-Revamp Plan — Pre-Execution Integration Review (2026-07-13)

> **Reviewer:** admiral, via 3 parallel adversarial sub-agents (dependency/sequencing · completeness/gaps · contradiction/staleness). READ-ONLY review; this doc records findings + the reconciliation applied.
> **Why:** Fable verified each *stream's* claims in isolation. Nobody had checked whether the milestones/decisions/deps/tasks **hang together** before execution. This is that gate.
> **Headline:** No dependency CYCLE; the near-term phase decomposition (Track B/C, M1, M2, M3) is complete and mostly well-specified. Defects concentrate downstream (M4/M5/validation-net) + staleness where P1 landing / operator ratification outran the docs. One finding (TC-4) was flagged by all three reviewers.

---

## Section A — Reconciliation applied to the plan docs (canonical wording)

These align the docs to already-ratified facts (P1 landings + operator decisions C1/C2/C3, B5). Each is the source of truth the apply-agents implement.

**A1 — TC-4 / coverage-carve gate (flagged by ALL 3 reviewers).** Canonical: `internal/substrate` is at **83.1%**, BELOW the 90% `internal/**` floor. `check-carve-coverage` is applied but **LEFT STANDALONE — NOT wired into `check-short`**. Wiring is **GATED on substrate coverage reaching ≥90%** (a fix that touches `internal/**`, outside Track C's config-only scope). Fix everywhere the docs say "wired into check-short" / "substrate passes the 90% floor / gate is green" (TASKS TC-4 acceptance+status; track-c-enforcement §3.2/§6/§coverage).

**A2 — Track C status.** Config applied + verified this session (uncommitted). TC-1/TC-2/TC-3 → **done**. TC-4 → **applied-standalone (wire-in gated on ≥90%)**. TC-5 (grandfather umbrella bead) → stays **needs-operator-signoff**. TC-6 (runexec depguard+baseline) → stays **gated on M3**.

**A3 — specaudit carve-out.** 3 of 132 specaudit test files import product packages. Canonical: **relocate 129** spec-prose regex files to the CI-lint script; the **3 product-importing files STAY under `go test`** (product-import carve-out — this is the recommended disposition, see B3). Fix the residual "zero product code / relocate all 132" in `track-b-m1.md:172` and `TASKS.md` M1-1 (only :160 was corrected earlier). M1-1 acceptance must STATE the 3-file disposition, not just "recorded."

**A4 — ROADMAP body vs its own Corrections block.** Drop `subsystem-proofs → M5` (lines ~90/124) and `quality-system/validation-net → Track C` routing (lines ~73/125) to match the Corrections block: **M5 is net-new** (no existing work to reconcile); **Track C is direct config work with no existing owner**. Also update stale P1 progress ("on T7" / "T6–T14 remaining") — **T6 has landed**.

**A5 — PLAN annotations (MARK as superseded; do NOT reopen).** PLAN is a marked-draft source; add `> SUPERSEDED BY P1:` annotations, don't rewrite: (a) §2 #9 / §3d / §8.3 "dead event-registry path / zero production readers / adopt-or-delete" → `internal/replay` is a LIVE reader; **ADOPT ratified (D6/EV-048)**. (b) §4 B3 / §5 → the **daemon** resume-hang was relocated to **M3-5**; keeper SR9/SK-INV-005 is a **separate keeper invariant**.

**A6 — testing-strategy-uplift.** Align to operator-approved **DECISIONS B5 = SUPERSEDE** (do not re-open as a work). Harvest the 5-layer taxonomy **INTO M1** (M1's coverage-gate result is the input). Fix `track-b-m1.md:244-245` + `TASKS.md:55` which currently say "fold into testing-strategy-uplift / do NOT supersede."

**A7 — M4 prereqs (state IDENTICALLY in ROADMAP + TASKS).** M4 hard-depends on **M3-2 (merge-queue split) + M2-1 (seam input/ack contract)** — NOT "all of M2," and M2-1 must not be omitted.

**A8 — M2/M3 parallelism (ROADMAP + TASKS must agree).** Canonical: **M3-phase-1** (C1 ClockPort, C2 merge-queue) is parallel to M2 — needs neither. **M3-3 (workLoopDeps→ports) does NOT need M2.** Only **M3-4 (reactor Step)** needs **M2-1's seam contract**. Narrow the TASKS "M3-3..M3-6 depend on M2" edge to just M3-4→M2-1. Mark "confirm at M3 design" (design-phase, held).

**A9 — M3's "M1 lands" gate.** Redefine as **"M1-1 landed (specaudit relocate = honest coverage baseline),"** explicitly independent of the operator-gated M1-2/M1-3 (else M3-phase-1 waits on deletions it doesn't need).

**A10 — M1→M3 coverage-audit producer (missing task).** Add a producer task: **"audit current line/branch coverage of the `beadRunOne`/workloop test net BEFORE extraction"** (host: M1 or M3-phase-1). Point the M3 gate at it. Today it's cited as a gate with nothing producing it.

**A11 — M2 keeper-migration edge (UNGUARDED DELETION HAZARD).** M2-3 (retire tmux write verbs) and M2-6 (delete `pasteinject.go`) must depend on **"keeper migrated to the M2-2 structured driver."** Add **keeper** to M2-6's deletion-boundary scope (next to spawn/remote). Without this, M2-6 deletes the input path P1's shipped keeper vertical still uses → restart cycle breaks. This is the same class as the event-registry hazard, which WAS caught; this one was not.

**A12 — Deferred-orphans tracking (in-passing findings homed only in ROADMAP prose).** Add a "Deferred orphans / in-passing findings" section to TASKS.md carrying each with its host: JSONL rotation/retention (Track B/C hygiene) · discarded `_ = Emit` errors + `Type`→enum + `SourceSubsystem` stamping (ride M-phases) · queue `HandlerAdapter` eviction (Track B) · lifecycle-reconcile intent-log BI-031 (own small work) · STEP-0c honest-probe guard (M4) · workspace event-dark instrumentation before rebuild (M4) · real-Claude restart integration test (P1 follow-up/M2) · record-keeper-live apptap recorder (M2-4) · pass-status-lag kerf housekeeping (advance stale passes).

**A13 — Track C commit-ownership.** Add a task: **"commit Track C config (`.golangci.yml`/`coverage-gate.sh`/`Makefile`/`track-c-APPLIED.md`) once P1 lands / at branch handoff."** Currently uncommitted with no owner/trigger → a `/clear` or reset silently discards the whole session's work.

**A14 — gocognit-findings task.** Add: **"P1 author: refactor or operator-accept the 2 gocognit findings — `internal/replay/replay.go:157` (Replay, 48) + `internal/substrate/replay.go:123` (Twin.replay, 31) — will trip P1's `make check-short`."** Relay could not go over comms (bus offline); carried in handoff.

**A15 — M5 gated placeholder.** Add a gated row: **"M5 daemon god-package decompose — HELD; un-hold trigger = M3 first slice (merge-queue) merged → open M5 problem-space. Net-new (NOT `subsystem-proofs`)."**

**A16 — M2/M3 un-hold proof condition (checkable).** Define: **"P1 proves the reactor seam" = P1 lands with `internal/substrate` + `internal/replay` green AND the keeper vertical running on the seam (SK-INV-005 holding).** Re-checked by admiral on P1 merge. Record in the Blocked/gated table.

**A17 — admiral-initiatives.md (my registry, stale).** Update "execution FROZEN / awaiting ratification / STEP-0→M1→…" → **operator ratified 2026-07-13 (DECISIONS all approved); phase map P1 →(Track B ‖ Track C[done]) → M1 → {M2, M3} → M4 → M5; daemon-off freeze PERSISTS (out-of-pipeline execution); STEP-0 dissolved as a front gate (0a→M3-5, 0b→Track B/B2, 0c→M4).**

**A18 — validation-net (see B1 for the scope call).** Correct `track-c-enforcement.md` §5's "`validation-net` (ready, 1/1 beads)" → **hollow: 12 of 13 spec'd beads (incl. flagship VN4 `hk-ukhzu`) are ABSENT from the `br` DB; re-file pending.** Add a TASKS.md tracking row marked **"SCOPE CALL PENDING (B1)"** + a gated "re-file the 12 missing VN beads if kept in scope."

---

## Section B — Operator-reserved (surfaced, NOT silently decided)

**B1 — validation-net scope.** The ROADMAP/reconciliation name `validation-net` (concurrent-merge fixture `RunConcurrentMerge`, flagship N≥3 concurrent-dispatch guard, CI run lane, 21 quarantined-E2E restore) as a protective net that "runs in parallel from day one, like Track C" — but it has **zero tasks** and **12/13 of its beads don't exist**. **DEFAULT (admiral, 2026-07-13, operator-informed): KEEP IN SCOPE; stand up just before M3's merge-queue work** (the concurrency-critical rewrite = exactly when the net must be live), given the project's concurrency-bug history (44 tmux incidents, the concurrent-dispatch wedge). Reversible one-liner anytime before M3. `VN-0` tracks it; the 12 missing beads get re-filed at stand-up time.

**B2 — M3-phase-2 → M2 edge (design-phase, defaulted).** A8's intended edge (only M3-4 needs M2-1) stands as the default; confirm at M3 design when the seam is proven. No operator action.

**B3 — specaudit 3-file disposition (defaulted).** The 3 product-importing files stay under `go test` (not bulk-moved to the markdown-lint script). Default stands unless overridden at M1 execution. No operator action.

---

## Section C — Checked hardest, found SOUND (green-light evidence)

- **No dependency cycle.** Traced P1 → {M2, M3, M4, M5} + the M3 internal chain (C1→C2 phase-1; C3→C4→{C5,C6}, C6←C2). All edges forward; M2 depends only on P1.
- **M5→M3 gating consistent** across ROADMAP / DECISIONS B1 / Track C "reserved for M5" stubs. M5 having no tasks is deliberate (held), not a missing producer.
- **Event-registry deletion hazard correctly caught + reversed** (M1-4 KEEP-guard) — P1's `internal/replay` consumes the path; census-era DELETE reversed to KEEP. (This is exactly why the un-caught keeper/pasteinject case A11 stands out.)
- **Track C landing before M3 is the correct order** (ratchet on before the big `beadRunOne` extraction, so `runexec` is authored under the ceiling, not retrofitted).
- **Numbers consistent everywhere:** `beadRunOne` 2366/2367 (line-span vs brace-matched) / 17 params / 85-field `workLoopDeps`; thresholds funlen 100/60, cyclop 15, gocognit 20, coverage 95/90/0.3pp; blast radius 104 funcs ≥100 lines. ClockPort 32 (keeper `cycle.go`) vs 26 `time.Now()` (daemon `workloop.go`) are two different files, not a conflict.
