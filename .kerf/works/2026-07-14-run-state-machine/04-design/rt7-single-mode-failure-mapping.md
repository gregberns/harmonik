# RT7 design note — single-mode failed-Dispatch → Run reopen mapping (Amendment A1)

Status: DECIDED 2026-07-14. Normative home: `specs/run-state-machine.md` §12
Amendment A1 (RSM-031..035). This note carries the decision rationale, the exact
golden string table the re-drive must reproduce, and the scoped RT6 `run.go` change.

## 1. The decision

**Chosen: shell-synthesized `EvModeOutcome{ModeOutcome: ModeFailure, Reason, Detail}`**
for a failed single-mode Dispatch (RSM-031), with terminal strings carried on
event payload / latched path label rather than static `RunConfig` (RSM-032/033).

Rejected alternative: a dedicated new Run event kind (`EvDispatchFailed`) plus a
per-event outcome table in `run.go`. Rejected because:

- RSM-011 already mandates "the run outcome MUST be surfaced to the dispatch side
  as a terminal event" — the mode-outcome event IS that surface, and single-shot
  is a mode whose sub-driver is one Dispatch instance. A second event kind for the
  same semantic ("this run's mode failed") duplicates the edge and doubles the
  transition-table rows for zero behavioral difference.
- The interim mapping is already what the scaffold FakeClock test drives
  (`runshell_test.go:153`), so RT7 composes onto a tested path.
- Purity: the shell converting a Dispatch terminal state into a Run event keeps
  both machines mutually ignorant (Dispatch never learns Run exists; Run never
  reads DispatchState).

**Verdict on the interim mapping:** directionally RIGHT but insufficient alone —
`EvModeOutcome{ModeFailure}` with no payload collapses every single-mode failure
onto the static `cfg.ReopenReason`, which cannot reproduce the goldens (the
reasons interpolate runtime data and differ from the terminal summaries). The
event must carry the strings, and `run.go` needs the scoped change in §4.

## 2. The golden string table (from `beadRunOne`, `internal/daemon/workloop.go`)

The RT7 re-drive MUST preserve these byte-equal. Columns: the
`outcome_emitted` detail (— means no outcome event), the `ReopenBead` reason,
and the `run_failed`/`run_completed` summary (`emitDone` arg). Source lines are
current as of commit 9df61a32.

| # | Path (source) | outcome_emitted | ReopenBead reason | run terminal summary |
|---|---|---|---|---|
| 0a | launch-spec build error (P9 `:4344–:4347`) | — | `build launch spec error: %v` | same string |
| 0b | D2 API-key refusal (P10 `:4376`) | — | `remote run: ANTHROPIC_API_KEY in spawn env (D2 fail-closed)` | same string |
| 0c | worktree-create / prepareRun guard fail (P3–P5, e.g. `:3751`, `:3336`) | — | interpolated per-guard reason (verbatim) | same string |
| 1 | agent_ready_timeout (P13 `:4941–:4961`) | — (emits `agent_ready_timeout` event) | `agent_ready_timeout` (bg ctx) | `agent_ready_timeout` |
| 1b | never-spawned-reaper abort (`:5071`) | — | `never_spawned_reaper: launch_initiated but agent_ready not received within deadline` | same string |
| 2 | escaped-worktree guard (`:5233–:5241`) | — (emits `implementer_escaped_worktree`) | `implementer_escaped_worktree: %d file(s) dirty in main: %s` | same string |
| 3 | no-commit guard (`:5276–:5284`) | — | `no_commit_during_implementer: HEAD did not advance past parent %s at iteration 1 exit=%d` | same string |
| 4 | scenario gate blocked (both branches, `:5292–:5297`, `:5345–:5350`) | `rejected` (sgr.reason) | `sgr.reason` (verbatim) | `sgr.reason` (verbatim) |
| 5 | code-sync fail, agent_completed (`:5301–:5308`) | `rejected` (syncReason) | `code-sync failed (agent_completed): %s` | `code-sync-failed (agent_completed): %s` |
| 6 | code-sync fail, auto-close (`:5354–:5360`) | `rejected` (syncReason) | `code-sync failed (auto-close): %s` | `code-sync-failed (auto-close): %s` |
| 7 | merge fail, agent_completed (`:5310–:5316`) | `rejected` (mergeRes.reason) | `merge-to-main failed: %s` | `merge-failed (agent_completed): %s` |
| 8 | merge fail, auto-close (`:5362–:5369`) | `rejected` (mergeRes.reason) | `merge-to-main failed: %s` | `merge-failed (auto-close): %s` |
| 9 | close success, agent_completed (`:5330`) | `approved` ("") | — (close) | `agent_completed: stop-hook outcome` |
| 10 | close success, auto-close (`:5380`) | `approved` ("") | — (close) | `auto-close: exit=0` |
| 11 | close BrUnavailable transient (`:5323`, `:5374`, `:5395`) | `approved` ("") | — (close attempted) | `close-transient-merged (agent_completed \| auto-close \| noChange-subsumed)` — success=true |
| 12 | close hard error (`:5325`, `:5376`, `:5397`) | `approved` ("") | — | `close-error: %v` — success=false |
| 13 | noChange-subsumed close (`:5388–:5401`) | `approved` ("") | — (close) | `noChange-subsumed: bead found in main` |
| 14 | noChange-timeout, not subsumed (`:5406–:5411`) | — | `noChange-timeout` (bg ctx) | `noChange-timeout: no commit in commitPollTimeout window` |

Note the load-bearing byte differences the static-config design could never
carry: row 5/6 reopen uses `code-sync failed` (space) while the terminal summary
uses `code-sync-failed` (hyphen); rows 5–8 and 11 are parameterized by the
dispatch-terminal branch label, known only at event time.

## 3. Event mapping the shell (RT7 composition root) performs

Provisioning-phase failures (before any Dispatch): the shell feeds
`EvProvisionFailed{Reason, Detail}` carrying the interpolated golden string
(rows 0a–0c); `stepRunResolving`/`stepRunProvisioning` reopen with the payload
reason/summary, config-fallback preserved (§4 item 2 covers this — the same
per-call override on `finalizeReopen`).

Dispatch terminal → Run event synthesized by the shell:

- `Aborted(reason="never_spawned_reaper: …")` (never-spawned reaper cancelled the
  run ctx, `:5071`) → `EvModeOutcome{ModeFailure, Reason: <row 1b string>, Detail:
  <same>}` — RSM-031's Aborted class, no distinct edge (row 1b).
- `Failed(reason="agent_ready_timeout")` → `EvModeOutcome{ModeFailure, Reason:
  "agent_ready_timeout", Detail: "agent_ready_timeout"}` (row 1). The
  `agent_ready_timeout` durable emission and the kill/reap already ride the
  Dispatch machine's SR9 edge; the Run side owns only reopen + terminal. Both the
  reopen and (per hk-4hso5/hk-e3fy) the terminal emission use background context
  shell-side — already mandated by RSM-022.
- `Completed(outcome)` → `EvAgentCompleted` (latches label `agent_completed`).
- `Exited(exit=0, no stop-hook, watcher ok)` → `EvCleanExit` (latches `auto-close`).
- `Stalled(no_change_timeout)` → shell runs `beadAlreadySubsumedInMain` (I/O):
  - subsumed → `EvModeOutcome{ModeSubsumed, Detail: "noChange-subsumed: bead
    found in main", EmitOutcome-flag set}` (row 13; RSM-035).
  - not subsumed → `EvModeOutcome{ModeFailure, Reason: "noChange-timeout",
    Detail: "noChange-timeout: no commit in commitPollTimeout window"}` (row 14).
- Guard results (shell `checkEscape` hook): `EvEscapeDetected{Reason: <row 2
  string>}` / `EvNoCommitGuardReopen{Reason: <row 3 string>}` / `EvGuardsPassed`.
- Gate (shell `runGate` hook = scenario gate for single-mode): blocked →
  `EvGateFailed{Reason: sgr.reason}` (row 4); pass → `EvGatePassed`.
- Merge window (shell `prepareMerge`/`submitMerge` hooks): code-sync failure →
  `EvMergeResult{MergeFatal, MergeStage: "code_sync", MergeReason: syncReason}`
  (rows 5/6); merge failure → `EvMergeResult{MergeFatal|MergeRetryable,
  MergeStage: "merge", MergeReason: mergeRes.reason}` (rows 7/8). Single-mode has
  no retry loop today (`MaxMergeAttempts: 1`), so any failure is terminal-class.
- Close: `EvCloseResult{Close: closed|br_unavailable|error, Detail:
  fmt.Sprintf("close-error: %v", closeErr)}` on the error branch (row 12).

## 4. RT6 `internal/runexec` change — YES, needed; scope

Small, pure, additive (no state-graph change beyond the payload plumbing):

1. **`vocab.go`:** add `MergeStage string` field on `Event` (`"code_sync" |
   "merge"`, empty = merge) and an `EmitOutcome bool` (or reuse a non-empty
   `Detail` + explicit flag) for the subsumed-approved case (RSM-035).
2. **`run.go` `finalizeReopen`:** accept per-call `reason`/`summary` overrides;
   empty falls back to `cfg.ReopenReason` for BOTH (preserving RT6 review-loop/DOT
   behavior and the existing tests unchanged).
3. **`stepRunModeOutcome` `ModeFailure`:** pass `ev.Reason`/`ev.Detail` through.
   `ModeSubsumed`: when the event's emit-outcome flag is set, close with
   `outcome_emitted=approved` and the event-carried summary; otherwise today's
   no-outcome `cfg.NoMergeCloseSummary` close (DOT path unchanged).
4. **`stepRunDispatching`:** `EvAgentCompleted`/`EvCleanExit` latch
   `s.PathLabel = "agent_completed" | "auto-close"` (new `RunState` field).
5. **`stepRunGating` `EvGateFailed`:** when `ev.Reason` is non-empty, prefix
   `ActEmit{outcome_emitted, Detail:"rejected"}` (shell resolves the reason into
   the payload as it does for the merge-rejected prefix) and reopen with
   `ev.Reason` as both reason and summary (row 4). Empty-reason behavior is
   unchanged (RT6 fallback).
6. **`stepRunMerging` / `mergeExhaustedOrFatal`:** compose the row 5–8 strings
   from `(ev.MergeStage, s.PathLabel, ev.MergeReason)` — pure string composition
   of preserved templates, allowed under RSM-020 ("strings preserved as data"):
   - code_sync: reopen `"code-sync failed (" + label + "): " + reason`; summary
     `"code-sync-failed (" + label + "): " + reason`.
   - merge: reopen `"merge-to-main failed: " + reason`; summary
     `"merge-failed (" + label + "): " + reason`.
7. **`stepRunFinalizing`:** `CloseBrUnavailable` → summary
   `"close-transient-merged (" + label + ")"` when a path label is latched, else
   `cfg.BrUnavailableSummary`; `CloseError` → summary from `ev.Detail` when set,
   else `cfg.CloseSummary` (row 12); success → the latched-label close summary
   (rows 9/10/13) via the same event/latch-first, config-fallback rule.

Everything stays a total pure function of `(cfg, state, event)`; no I/O, no
clock, no minting. New transition-table test rows cover each payload branch.

## 5. RSM-029 divergence allowlist impact

The re-drive produces **no new divergences** beyond the four already sanctioned
by RSM-029 (a: resume-liveness bound replaces the fixed grace; b: run-identifier
attribution on the synthetic ready; c: shrunk escape-check window; d: no
transient ref advance during a build failure). Two clarifications the
implementer should expect in golden comparison, both inside (a)-class ordering,
not string changes:

- The `agent_ready_timeout` path's kill/reap/emit now rides the Dispatch SR9
  edge before the Run reopen event is fed; the durable event ORDER
  (`agent_ready_timeout` → bead reopen → `run_failed`) matches today's P13 block.
- A hung LAUNCH (no launched/launch_failed event) now also terminates via the
  same edge (RSM-005 extension, RSM-INV-002) where it formerly wedged — that is
  divergence (a) territory (a formerly-hung run now emits a failure-class event).

## 6. Residual ambiguity

None requiring an operator call. One implementer-discretion point: whether the
subsumed-approved signal is a dedicated `EmitOutcome bool` event field or an
inference from non-empty `Detail` on `ModeSubsumed` — the spec (RSM-035) requires
only that the flag travels on the event; prefer the explicit bool for
JSON-round-trip clarity.
