# Session Notes

## Session 2 (2026-05-31) — must-fix application + Integration + Tasks → Ready

Picked up `reap` at the Integration pass (passes 1-5 completed in session 1). This session: applied the six change-spec critical-review must-fix items to the C1/C3/C4/C5/C6 spec drafts, completed the Integration pass (`06-integration.md` + `SPEC.md`), completed the Tasks pass (`07-tasks.md`) with bead reconciliation, and advanced to `ready`.

### Critical-review must-fixes applied (all six)

1. **C1/C3 ordering contradiction (was blocking).** C1's PL-006e and C3's QM-002c both fed the survive-check from "the EM-031a re-attached set re-attached by PL-005 step 7" while C1 also runs at step 3 — mutually exclusive (the step-7 set does not exist at step 3). **Resolved via option (a):** one `lifecycle.DiscoverActiveRuns` call at PL-005 step 3 (Beads non-terminal query + git task-branch-tip scan, independent of the step-7 model per `orphansweepbeads.go:50-58`) is the SHARED survive-check source for both C1 (step 3) and C3 (step 8a, set threaded into `LoadQueueAtStartup`). Matches the existing pre-sweep `queue.Load` pattern at `daemon.go:705-741`. Verified `DiscoverActiveRuns` exists at `activerun_em031a.go:314` and supports the Beads+git scan alone.

2. **PL-005 step-1.5 label collision (blocking-for-integration).** C5 (flywheel-lock) and C6 (boot-backoff) both labelled their gate "step 1.5". **Resolved:** 1.5 = flywheel-lock acquire (C5/PL-002c), 1.6 = boot-backoff gate (C6/PL-005c); C6 text now says "after the flywheel-lock gate of step 1.5"; both land together so PL-005 numbering is unambiguous.

3. **Exit-code 25 gap → non-contiguous registry (blocking).** Verified: code 25 (`ExitCodeSupervisorRunning`) is a LOCAL const at `cmd/harmonik/supervise/start.go:23`, NOT in the central registry (`exitcode.go` tops at 0-23); no `supervise` `CommandName` in `commandcodes.go`. **Resolved:** added an ordered integration obligation to C5+C6+integration+SPEC — absorb 25 → add `CommandSupervise` `CommandName`+`CommandExitCodeSet` → add 24 (C5) → add 26 (C6), so `VerifyCommandExitCodeSets` resolves 24/25/26 contiguously. Code-17 dual-meaning (`start.go:19` local vs registry) flagged as separate non-blocking.

4. **C1 `crashevidence.go` anchor.** The must-fix premise was that the file doesn't exist; verified it DOES exist at `internal/workspace/crashevidence.go` (WM-003a) — in the workspace package, not lifecycle. **Resolved:** package-qualified the anchor throughout C1 (PL-006e (iii) + Files table) and C3 (evidence classification), pointing at `internal/workspace/crashevidence.go` + the WM-022 sidecar walk in `workspace.go`.

5. **Event-class / field-name precision.** (a) C3 now cites event-model §8.10.7 explicitly + the class-F enum extension on `queue_item_reconciled.reason` (verified §8.10.7 row + §6.3 schema; reason enum was `claim_write_lost` only). (b) C4's gauge-decrement now cites HC-011 (watcher-ownership + Wait/reap; verified `handler-contract.md:310`) not HC-024 (subprocess-crash); HC-011a for the leak case, HC-024a for socket-EOF. (c) `coordinator_sessions_skipped` confirmed existing (process-lifecycle.md:333, PL-006d) — no change.

### Integration pass

- `06-integration.md` — component map, four shared resources (the pre-sweep `DiscoverActiveRuns` set §2.1; the additive `daemon_orphan_sweep_completed` payload §2.2; the ordered exit-code registry §2.3; the 1.5/1.6 step labels §2.4; the `daemon_generation` counter §2.5; PL-006a/PL-006d provenance §2.6), build order A-G, cross-cutting concerns, runtime ordering dependencies §5.1-§5.4, the six must-fixes closed §6, integration-test strategy §7, completeness self-check §8.
- `SPEC.md` — self-contained assembly: scope/non-goals, hard invariants, the shared integration contract §3, the six normative clauses (PL-006e, PL-006f, QM-002c, PL-014b, PL-019(i), PL-002c, PL-005c) + their ACs, files-and-anchors, testing, traceability. Adds no requirement, changes no decision.
- `integration-review.md` — fresh-context re-read; PASS; verified all must-fix resolutions consistently propagated (grep-confirmed no residual step-7-at-step-3 contradiction; 25→24→26 + `CommandSupervise` + 1.5/1.6 + `DiscoverActiveRuns` consistent across all four artifacts).

### Tasks pass

- `07-tasks.md` — 13 tasks (T1-T13) across build steps A-G + 2 test tasks, each mapped to an existing `codename:reap` bead. Acyclic DAG; coverage table maps every SPEC section + component AC to a task.
- **Bead reconciliation:** 6 beads total (verified via `br list --label codename:reap`). hk-9eury (C1+C2+C3) owns T1/T3/T4/T5/T6; hk-xb5yi (C4) owns T10/T11; hk-li14r (C5) owns T7-portion/T8; hk-7t9g1 (C6) owns T2/T7-portion/T9; hk-a31od (scenario) owns T12; hk-izs8s (exploratory) owns T13.
- **GAPS flagged (no beads created):** (1) the exit-code registry task T7 spans two beads (hk-li14r needs 24/25, hk-7t9g1 needs 26) — resolvable within the existing beads by sequencing hk-li14r first; not a missing bead, a sequencing note. (2) sibling-work ordering (`credfence` first, `pilot` independent) is orchestrator-level, out-of-bead by design. (3) `daemon_generation` source if hk-7t9g1 lands after hk-9eury — soft dependency with documented daemon-start-ns surrogate.
- `tasks-review.md` — fresh-context re-read; PASS; DAG acyclic, both test beads gate the impl beads in the correct direction (test-beads BLOCK impl-beads — already wired).

### Status: ready

`kerf square` passes on status + dependencies; all pass-output artifacts present.

### Key facts verified against the live tree this session

- `internal/lifecycle/orphansweepbeads.go:50-58` — documents the MVH gap (PL-005 step-7 model rebuild not wired as a distinct phase).
- `internal/daemon/daemon.go:705-741` — the existing lightweight pre-sweep `queue.Load` provenance read (the precedent for adding the pre-sweep `DiscoverActiveRuns` call).
- `internal/lifecycle/activerun_em031a.go:314` — `DiscoverActiveRuns(ctx, querier, reader)`; Beads + git-branch-tip union; `ErrBeadsUnavailable` degradation.
- `internal/workspace/crashevidence.go` (+ `crashevidence_wm003a_test.go`) — the real sidecar/Cat-3 evidence location (NOT lifecycle).
- `internal/operatornfr/exitcode.go` — registry "all 24 exit codes (0-23)"; `commandcodes.go` — CommandNames daemon/attach/enqueue/status/pause/stop/upgrade/list/runner (no `supervise`).
- `cmd/harmonik/supervise/start.go:19,23` — local `ExitCodeDaemonDown=17` + `ExitCodeSupervisorRunning=25`.
- `specs/event-model.md:298` — §8.10.7 `queue_item_reconciled` class F, reason enum `claim_write_lost`.
- `specs/handler-contract.md:310,450` — HC-011 (watcher ownership) vs HC-024 (subprocess crash).
- `specs/process-lifecycle.md:333` — `coordinator_sessions_skipped` (PL-006d).
