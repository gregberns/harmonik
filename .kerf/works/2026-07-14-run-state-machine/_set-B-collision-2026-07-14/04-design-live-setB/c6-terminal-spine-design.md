# 04-Design / C6 — terminal-spine factoring (the 4×→1 slice)

> Pass 4 design slice for C6. The full machine design lives in
> `c4-runexec-reactor-design.md` §RSM-RX-5 (the `Run` machine's Gating→Merging→Finalizing
> tail — C4 and C6 were designed together). Elaborates pins D8 (terminal spine) + D3 (the
> merge queue the spine submits to). Research: `03-research/c6-terminal-spine/findings.md` (TF).
> Target spec: NEW `specs/run-state-machine.md` §Terminal spine (RSM).

## Current state
The launch→gate→merge→close spine is open-coded 4× (workloop.go :3860-3935, :4103-4164,
:5231-5275, :5277-5324) plus two merge-less close blocks (subsumed :4165, noChange :5330) and
a 6×-duplicated inner close ladder (TF §6). The `runSucceeded *bool` out-param (:3072, written
by `emitDone` :3119) threads success back to the wrapper.

## Target state
- **RSM-SPINE-1.** The four merge/close blocks + two merge-less closes + the 6× close ladder
  become the ONE `Run`-machine tail (`Gating→Merging→Finalizing`, c4 §RSM-RX-5). The distinct
  ENTRY conditions stay distinct events; exact summary/reason strings are preserved as event
  data (parity — incl. the "(agent_completed)"/"(auto-close)" labels).
- **RSM-SPINE-2.** Every real TF divergence survives as an explicit parameter (RunState field
  / event detail): label, successSummary, gateRunner(nil=skip), doPreMergeSync, trailerVerdict
  + re-amend-on-retry, mergeRetries, allowRebaseDroppedFallthrough, ctx(per-run|background),
  emitOutcome, needsAttention, close/reopen reason templates.
- **RSM-SPINE-3 (non-collapsible, kept distinct).** exit-0 auto-close is NOT redundant with
  agent_completed — it is THE terminal path for CompletionProcessExit harnesses (codex/pi, no
  claude stop hook); same tail, different (entry event, label, summary). Shutdown-drain stays a
  distinct terminal edge (bgCtx, no gate/preMergeSync/outcome, direct emitRunCompleted,
  reopen-reason substitution — QM-002a).
- **RSM-SPINE-4.** `runSucceeded *bool` is DELETED — success is the `Run` terminal state the
  wrapper reads (the three consumers: `evaluateGroupAdvanceWithOutcome`,
  `stagedBeadGeneratorEval`, the two Pi-retention defers). `emitDone` → `ActEmitRunTerminal`
  (ctx-swap in effector); `sessiondata.Collect` stays a shell effect (EXCEPT the drain batch).
  `hclifecycle.Machine` stays a PROJECTION (D5), not subsumed.
- **RSM-SPINE-5.** ctx-normalization is explicit: terminal reopens use Background (hk-e3fy) so
  a mid-merge-cancelled run's reopen does not silently no-op — a deliberate, tested change.

## Rationale
Four copies → one is the visible payoff and the DoD "thin driver" criterion. It attaches to
C4's states (D5) and C2's merge queue (D3), so it is naturally the last structural component.
Preserving every TF divergence as a parameter is what keeps it behavior-preserving.

## Requirements traceability
02-components C6 → RSM-SPINE-1..5. Goal "one factored terminal spine, not four" + "no *bool
out-param" (01 §2), success-criteria 1,2 → RSM-SPINE-1/4.

## PLANNER-RECONCILE
None specific to C6 (inherits D3's push-stays-in-window boundary via the merge queue).
