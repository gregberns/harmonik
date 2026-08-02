# Single-workflow tail inventory

**Date:** 2026-08-02. **Source checked:** `9f52d7f5d`.

## Scope

This is a source inventory. It makes no runtime change.

`beadRunOne` in `internal/daemon/workloop.go` still selects DOT in its
`WorkflowModeDot` case. Its `default` arm then enters the imperative
single-workflow tail. There are two live routes into that arm:

1. `resolveWorkflowMode` in `internal/daemon/moderesolve.go` accepts an
   explicit `workflow:single` bead label.
2. `resolveRunPlan` in `internal/daemon/workloop_runplan.go` accepts a valid
   `RunEnv.ItemWorkflowMode` tier-0 override. `harmonik run --workflow-mode
   single` writes that value to `queue.Item.WorkflowMode`. Queue RPC preserves
   it into the dispatched item.

The default remains the reviewed DOT graph. `resolveRunPlan` evaluates the
label route before it applies tier 0. `emitReviewBypassed` therefore runs only
during label evaluation. A tier-0 `single` item reaches the same imperative
tail with no equivalent audit event. A tier-0 `dot` item can also override a
`workflow:single` label after that label has emitted a false bypass audit.

The tail is not ready for deletion. The graph must first carry, or deliberately
retire, each behavior below.

## Remaining paths

| Path | Current single-only owner | DOT state | Removal prerequisite | Tests to keep or extend |
|---|---|---|---|---|
| Independent tmux session and restart adoption | `beadRunOne` configures `agentlaunch.LaunchInput.ConfigurePerRunSubstrate`, writes `runpkg.Record`, and preserves the record on a surviving run. `adoptDeadRunSessions` and `adoptLiveRunSession` consume that record. | DOT does not configure a per-run substrate. | Fix `hk-jyh5t` and the boot orphan sweep first. Then give graph node launches the same record, adoption, and shutdown disposition. | `survive_shutdown_gate_test.go`, `survive_shutdown_recovery_test.go`, `survive_shutdown_run_resources_test.go`, and `tmuxsubstrate_sessioncreation_bounded_test.go`. |
| Escaped-worktree check | `beadRunOne` calls `runmerge.CheckMainWorkingTreeDirty` inside the merge domain. `emitImplementerEscapedWorktree` reports a failure. | No graph equivalent. | Resolve D2 and `hk-co8g8`. RSM-008 must permit the selected graph behavior before it moves. | `mergeq_domain_rsminv005_test.go` plus a graph-path failure test. |
| No-commit and subsumed-work decision | `noCommitGuardShouldReopen` uses the active target repository. `noChangeTimeoutCh` also closes a subsumed bead as approved. | DOT has a related per-node decision, but its cross-repository target differs. | Move one three-way decision that uses `activeRepo`. Preserve the approved subsumed outcome before removing the timeout branch. | `single_nocommit_headprobe_test.go`, `dot_node_crossrepo_test.go`, and `dot_node_baseline_test.go`. |
| Implementer presence | `OnLaunchedExtra` calls `emitImplPresence` for join. A deferred call emits leave. | DOT has no equivalent launch callback. | Add balanced join and leave around each graph implementer lifecycle. | `socket_commspresence_7t27s_test.go` and a graph-node lifecycle test. |
| Pi launch profile and evidence | The tail fills all Pi fields in `shared.LaunchCtx` from `runPlan.PiProfile`. It writes `pi-stderr.log` from `PiCaptureDir` when a Pi run fails. | Graph node profile support exists in `dispatchDotAgenticNode`, but the tail-only post-mortem write remains. | Preserve the resolved profile through every graph launch. Decide whether retained graph evidence needs the same stderr artifact. | `dot_node_piprofile_test.go`, `dot_postexit_commitfallback_test.go`, and `survive_shutdown_run_resources_test.go`. |
| Run metadata and lifecycle machine | The tail calls `RunHandle.SetAgentType`, `RunHandle.SetMachine`, and `transitionToTerminated`. | DOT now sets its session machine, but still needs parity for all run metadata and terminal paths. | Prove the graph run supplies the handle, terminal transition, stale-watch, dashboard, and bandwidth facts. | `dot_node_lifecycle_test.go`, `bandwidthtuner_test.go`, and `dot_node_terminal_test.go`. |
| Cold-start semaphore | The tail holds and releases the cold-start token around launch readiness. | Graph launches do not take the same token. | Put the token in the shared graph launch path before deletion. | `tmuxsubstrate_terminalreserve_test.go` and a remote DOT saturation test. |
| Dispatch terminal classification | The tail reads stop-hook outcome, process exit, watcher errors, and stderr tail before it feeds the run machine. | DOT has its node-terminal classifier, but the paths must stay behaviorally equal. | Finish the post-exit convergence work. Include failure-after-commit, clean exit, malformed watcher output, and stderr-tail behavior. | `dot_node_terminal_test.go`, `dot_postexit_fixture_test.go`, `single_abort_phasecomplete_test.go`. |
| Shutdown drain | The tail uses `bridge.Drain` when a cancelled run has a committed worktree. | DOT does not drain through the same run tail. | Give graph runs the same committed-work drain or document a replacement state-machine edge. | `mergeq_domain_rsminv005_test.go` and a DOT shutdown-drain test. |
| Scenario gate | The tail uses the normal run spine gate. | The reviewed standard graph carries `commit_gate`. An arbitrary no-review graph can omit it. | The explicit no-review graph must state its audited gate policy. Do not rely on the standard graph for a selector that uses another graph. | `standardgraph_sync_test.go` and a no-review graph conformance test. |
| Review-bypass audit and selector surface | `emitReviewBypassed` runs while `resolveWorkflowMode` evaluates a `workflow:single` label. `resolveRunPlan` then applies a valid `queue.Item.WorkflowMode` tier-0 override. A tier-0 `single` value has no audit. A tier-0 `dot` value can leave a false label-route audit. `harmonik run --workflow-mode single` writes the tier-0 value. `core.WorkflowMode` still declares `single` and `dot`. | A reviewer-less DOT graph is valid, but it has no replacement selector or audit carrier yet. | Resolve the final selector before emitting its audit. The selector and audit must cover the label route and the tier-0 queue-item and CLI route. Test that a tier-0 DOT override emits no bypass audit. Then retire the single enum value, CLI flag value, and queue-item mode value together. Project config is not a dispatch route. It already rejects `daemon.workflow_mode: single`, so its later work is parser-validation retirement only. | `moderesolve_single_override_dot_default_hkgwy_test.go`, `moderesolve_test.go`, `run_w3cp1_boiwe_hiqrl_test.go`, `internal/queue/rpc_test.go`, `scenario_em012a_unlabeled_bead_dot_default_hk982_test.go`, and CLI flag tests. |

## Ownership and ordering

Alpha owns the daemon changes in this inventory. The lane map normally assigns
`internal/core` to Bravo. Its dated Step 13 directive assigns Alpha the final
`internal/core` decision and implementation. That directive is the scoped
exception for this program. Bravo can take the later CLI and queue-item surface
removal after Alpha defines the replacement selector and audit contract.

Do work in this order:

1. Converge post-exit interpretation before moving tail behaviors.
2. Resolve D2 and `hk-co8g8` before porting either guard.
3. Repair the independent-session adoption path before putting it on DOT.
4. Define one graph selector and audit that cover both single routes before
   deleting `WorkflowModeSingle`.
5. Add an explicit audited no-review DOT graph.
6. Delete the tail and retire the single CLI and queue-item surfaces in one
   change. Retire the project-config parser validation only when no `single`
   mode value remains to validate.

`runmerge.SnapshotUntrackedFiles` is above the workflow-mode switch. It is
shared setup with a tail-only reader. It is not itself a tail-only path.

## Step 14 collision note

Step 14's substrate-capability work still overlaps
`bootState.wireWatchersAndObservers` in `internal/daemon/bootstate.go` with
Step 12. It does not remove the single-workflow tail by itself. Keep those
changes serialized when either one edits the shared boot wiring.
