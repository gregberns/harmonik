# C6 design — Migration DAG and structural gates

## Current state

CQ-02 supplies a detailed queue/JR/WL DAG and ARCH-00 supplies reproducible
baselines/collision leases, but there is no complete construction/lifecycle/
mode migration and no literal final ARCH-GATE contract.

## Target state

### DAG

RAC imports all 21 `CQ-02.implementation_slices` rows verbatim. In particular
it preserves CQ-02I → CQ-01 → CQ-RECEIPT → CQ-RUN-WAIT/CQ-04, CQ-03 →
JR-01, CQ-RUN-WAIT + CQ-04 + JR-01/JR-02 → JR-03, and JR-03 + CQ-04 →
JR-04 → WL-REC-01. It does not rename their leases, widen their writes, or
replace their intermediate/rollback states.

The non-queue architecture rows compose existing indexed cards:

| Task | Prerequisites (including proposed additions) | Exact writes and lease | Accepted intermediate / rollback unit | RAC condition unlocked |
| --- | --- | --- | --- | --- |
| `INPUT-ACK-CONTRACT-01` **proposed** | none; blocks RAC spec-draft | `specs/handler-contract.md` HC-069/HC-070/§6.1 Ack, `specs/run-state-machine.md` RSM-027, `specs/agent-input.md` AIS-003/AIS-004/AIS-INV-001, its Kerf/review/evidence; `process_phase_scope` | all three owner specs agree, no production change; roll back the coordinated amendment/reviews as one unit | one reviewed Ack vocabulary/semantics for RAC to cite |
| `PS-01` | ARCH-01, PI-L1B, PI-L1C, PI-F0, plus reviewed `INPUT-ACK-CONTRACT-01` | `internal/runloop/phase/phase.go`, `phase_test.go`; `process_phase_scope` | pure phase FSM only, no adapter/caller; roll back directory | frozen lifecycle protocol |
| `PS-HCPL-CONFORMANCE-01` **proposed** | PS-01, reviewed `INPUT-ACK-CONTRACT-01` | `internal/handler/contract_adapter.go` `ContractLaunchMapper`/`ContractHandlerAdapter.AgentType`/`ContractHandlerAdapter.Launch`/`ContractSessionAdapter`/`NewContractHandlerAdapter`/`NewContractSessionAdapter`; `internal/handler/handler.go` `Handler`/`NewHandler`/`launchAssemblyOps`/`defaultLaunchAssemblyOps`/`(*handler).Launch`→`launchWithOps` direct wire-tap/wrapper/recovery/handoff and `(*handler).launchViaSubstrate`→`launchViaSubstrateWithOps` nil- and non-nil-stdout ownership paths; `internal/handler/session.go` `Session`/exact `newSessionWithIDs(ctx context.Context, cmd *exec.Cmd, sessID, runID string, wireTap io.WriteCloser) (Session, *sessionReapHandoff, error)`/`newSessionWithIDsUsingOps`/`sessionConstructionOps.closeParentChildEnds`/`sessionAbortTimeout`/`ownedCloser`/retirement of `cmd.StdinPipe` for guarded direct `os.Pipe` stdin/`sessionReapHandoff.startIO`/`.transferToWatcher`/`.completeNormal`/`.abort`/`.startLegacyOwner`/`bridgeStdout`→`newStdoutBridge` with `stdoutBridge.Done`/`.CloseAndWait`/`.AbortAndWait`/retirement of `abandonStartedSession`/`session.runWait`→`session.finishWait`/`(*session).Wait`; `internal/handler/substrate.go` `substrateLaunchHandoff`/`newSubstrateLaunchHandoff`/`.abort`/`.releaseToLegacySession`/`.transferLegacySessionAndWatcherTap`; `internal/handlercontract/watcher_hc011.go` optional `SpawnWatcherConfig.TakeReapOwnership`/`SpawnWatcher` pre-goroutine transfer and observation-only nil mode/`Watcher.runLoop` transferred reap/finalize/tap-close behavior/`Watcher.Outcome`/`Watcher.LogLocation`; `internal/handlercontract/outcomedelivery.go` decoded outcome capture; `internal/handlercontract/sessionlogloc_hc010.go` decoded log-location capture; `internal/lifecycle/spawnwait_pl014.go` `WaitOwner`; tests `internal/handler/contract_adapter_test.go`, `internal/handler/substrate_hkgql2011_test.go`, `internal/handler/session_waitowner_test.go`, `internal/handler/session_reaphandoff_test.go`, `internal/handler/handler_reaphandoff_test.go`, `internal/handler/substrate_reaphandoff_test.go`, `internal/handlercontract/watcher_reap_test.go`, `internal/handlercontract/watcher_optionalownership_test.go`, `internal/handlercontract/watcher_sessionstate_test.go`, `internal/lifecycle/spawnwait_pl014_test.go`; the 27 unchanged observation-only literals in the seven named existing handlercontract test files require no lease; `process_phase_scope` | target direct `handlercontract.Handler`/`Session` compile through the named adapters, `AgentType` returns the injected identifier, and mapped substrate launches are rejected before contract-adapter legacy Launch; `NewHandler`, legacy Launch's three returns including normal `(Session,nil,nil)`, public `NewSession`, legacy Wait's error return, and all old mode callers remain unchanged; before Start, guarded direct pipes assign the stdin child reader to `cmd.Stdin`, retain the sole parent writer, and provide stdout/stderr child writers plus parent readers; every pre-Start failure closes all acquired guards, and `exec.Cmd.Wait` owns none; immediately after Start one handoff owns every descriptor/tap, then `closeParentChildEnds` retires the parent's child-end copies before any later fallible cut; Session `CloseStdin`, normal completion, and abort share the one stdin parent-writer guard, so EOF reaches the child and no exec/session double-close exists; `startIO` atomically transfers the one stdout-read `ownedCloser` to `stdoutBridge` while stderr remains handoff/session-owned and drain-borrowed; after every Watcher exit and raw reap, successful completion calls `completeNormal`, whose `CloseAndWait` closes the pipe reader and source through their guards before joining the bridge, then closes the writer, so framing-error/cancel/panic exits with unread stdout cannot deadlock; it then closes and joins stderr and finalizes once; failure abort closes only still-handoff-owned guards, invokes the bridge's guarded `AbortAndWait`, joins reap and both auxiliary goroutines within the 5-second bound, then finalizes/closes tap once; direct-stdin EOF/once-only ownership, normal-success, framing-error-with-unread-stdout, and failure tests prove every guard closes once and no auxiliary goroutine survives; non-nil-stdout substrate uses a temporary handoff, aborts every wrapper/validation/tap failure once, then transfers legacy Wait to Session and tap close to observation-only Watcher; nil watcher config remains safe fixture mode; rollback is one commit reverting only this task's listed production hunks plus its new/updated hunks in the ten named test files | direct-path HC/PL conformance, explicit substrate PL-016 compatibility exception for PS-02, and a deletable legacy bridge |
| `PS-02` | PS-01 plus `PS-HCPL-CONFORMANCE-01` | `internal/daemon/tmuxsubstrate.go` `tmuxSubstrate`/`perRunSubstrate`/`tmuxSubstrateSession`; `internal/handler/handler.go` `launchViaSubstrate`; `internal/runlaunch/teardown.go` `ForceTeardownSession`; `internal/runloop/phase/local.go` and tests; `process_phase_scope` | local adapter conforms, no mode call sites; roll back adapter paths/tests | reusable local phase adapter |
| `WL-01` | ARCH-00 | its three exact evidence/test files only; no spine | characterization only; roll back its exact files | workloop fidelity oracle |
| `WL-02A` | ARCH-01, ARCH-GATE, WL-01, CQ-CALLER-EAGER | `internal/daemon/workloop.go` periodic-maintenance regions, `internal/runloop/maintenance.go` and test; `dispatch_spine` | typed maintenance observations, dispatch inline path otherwise intact; rollback owner/call site | maintenance owner |
| `WL-02B` | ARCH-01, ARCH-GATE, WL-01, WL-02A | `workloop.go` dispatch-gating regions, `internal/runloop/dispatchgate.go` and test; `dispatch_spine` | pure allow/delay/stop gate, source/claim unchanged; rollback gate/call site | dispatch permission owner |
| `WL-MUT-01` | ARCH-01, ARCH-GATE, CQ-02I, WL-01, WL-02B | five named non-reservation branches in `workloop.go`, queue transaction calls/tests; `dispatch_spine` | five mutations transactional, reservation/terminal unchanged; roll back those branches | non-reservation queue writes removed |
| `WL-03` | ARCH-01, ARCH-GATE, WL-01, WL-02A, WL-02B, WL-MUT-01 | `workloop.go` selection/pre-screen regions, `internal/orchestrator/dispatchcandidate.go` and test; `dispatch_spine` | immutable queued/direct candidate, no effects; roll back selection owner/call site | OuterQueueLoop source boundary |
| `BR-00` | ARCH-01, JR-00, JR-02, plus reviewed `INPUT-ACK-CONTRACT-01` | its Kerf/spec/evidence only; no production spine | reviewed phase/result contract; roll back artifacts | immutable plan/phase ownership |
| `BR-01` | ARCH-GATE, BR-00 | `workloop.go` resolution sections, `internal/runloop/runplan.go` and test; `dispatch_spine` | all callers use pure resolver, acquisition unchanged; rollback resolver/call sites | queued/direct RunPlan |
| `BR-02A` | BR-01 | placement/worker sections of `workloop.go`, `internal/runloop/placementlease.go` and test; `dispatch_spine` | one placement lease, later resources inline; rollback owner/call site | placement ownership |
| `BR-02B` | BR-02A | tunnel/worktree sections of `workloop.go`, existing transport/workspace adapters, `internal/runloop/workspacelease.go` and test; `dispatch_spine` | typed reverse cleanup, phase inline; rollback owner/call site | tunnel/worktree ownership |
| `BR-02C` | BR-02B | prepared-resource construction/call sites in `workloop.go`, lease types, `internal/runloop/preparedrun.go` and test; `dispatch_spine` | complete prepared input, mode switch inline; rollback prepared type/call sites | constructor-complete run resources |
| `RL-02A` → `RL-02B` → `RL-02C` → `RL-03` | exact card chain; RL-02B additionally PS-02; RL-03 additionally BR-00 | continuity/checkpoint, session/subscription, iteration/verdict, then all `internal/daemon/reviewloop.go`; staged `internal/runloop/continuity/**`, `reviewcycle/**`, new `review_implementer.go`/`review_reviewer.go`; `reviewloop_spine` | one reviewed atom per node; each rolls back only its owner/call-site slice; RL-03 leaves thin coordinator | review executor without shadow tasks |
| `DOT-01` → `DOT-02` → `DOT-GATE-01` → `DOT-03` | exact cards; DOT-02 additionally PS-02/BR-00; DOT-GATE-01 additionally ARCH-GATE; DOT-03 additionally ARCH-GATE | `internal/runloop/dotprogress/**`; `dot_cascade_core.go` `dispatchDotAgenticNode`; `dot_gate.go` gate policy/`executeCognitionGate`; `driveDotWorkflow` and `sub_workflow_runner.go`; exact node adapters/tests; `dot_spine` | pure kernel, then agentic node, then gate, then coordinator; each card's slice is its rollback | DOT executor without duplicate ARCH task |
| `BR-03` | BR-02C, PS-02, RL-03, DOT-03 | mode-switch region of `workloop.go`, `internal/runloop/modeexecutor.go` and tests; `dispatch_spine` | three existing executors behind lossless result; terminal still JR-03; rollback interface/adapters/call site | single/review/DOT boundary |
| `JR-03` | exact CQ/JR/BR prerequisites in its card | typed terminal owner, old terminal/group regions of `workloop.go`, optional `internal/runloop/runbridge.go`, named drain/advance/QM-053 sites/tests; `dispatch_spine` | receipt-aware terminal enabled only after waiter/startup; rollback JR-03 only | C4 terminal composer |
| `WL-DEPS-DELETE-01` **proposed** | WL-03, BR-03, JR-03, JR-04, WL-REC-01, all direct `workLoopDeps` consumers migrated | `internal/daemon/workloop.go` `workLoopDeps`/`newWorkLoopDeps`; `runports.go`; `bootworkloop.go` `buildWorkLoopDeps`; `bootstate.go`/`daemon.go` injection call sites; `eagerfill_em063.go`, `scheduletick.go`, `diskcheck_hksxlb.go`, `workloop_handlerpause_kac8g.go`; `internal/runloop/ports.go` obsolete bundles and focused tests; `composition_spine`, with `dispatch_spine` globally quiesced | zero-field/use proof first, then one compiling deletion cutover; rollback the deletion commit only | temporal assembly and obsolete bundles absent |

The two proposed coordination tasks and one proposed final deletion slice are recorded only
in ARCH-01 evidence; this work does not create cards or index entries. There
are no parallel ARCH review/DOT/single owners.

### Literal ARCH-GATE targets

For a function, `reach` is the number of distinct project-local import paths
directly referenced by selector expressions in that function body, computed
from the Go AST and resolved import aliases. For a file row, it is the number
of distinct project-local paths in the file import table. For `workLoopDeps`,
reach is the number of production files that refer to the declared type,
resolved with `go/types`. Span is inclusive declaration/file lines; cognition
uses `gocognit` (file cognition is the sum of its functions).

Every evidence row has six literal integers:

| Task | Symbol | Baseline span | Target span | Baseline complexity | Target complexity | Baseline reach | Target reach | Forbidden imports |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| WL-02A | `runWorkLoop` | 1667 | 1250 | 888 | 600 | 7 | 6 | [`github.com/gregberns/harmonik/internal/daemon`, `github.com/gregberns/harmonik/internal/queue`] |
| WL-02B | `runWorkLoop` | 1667 | 750 | 888 | 250 | 7 | 4 | [`github.com/gregberns/harmonik/internal/daemon`, `github.com/gregberns/harmonik/internal/queue`, `github.com/gregberns/harmonik/internal/handler`, `github.com/gregberns/harmonik/internal/lifecycle/tmux`] |
| WL-03 | `runWorkLoop` | 1667 | 300 | 888 | 80 | 7 | 2 | [`github.com/gregberns/harmonik/internal/daemon`, `github.com/gregberns/harmonik/internal/queue`, `github.com/gregberns/harmonik/internal/handler`, `github.com/gregberns/harmonik/internal/lifecycle/tmux`] |
| BR-01 | `beadRunOne` | 2298 | 1900 | 396 | 330 | 24 | 20 | [`github.com/gregberns/harmonik/internal/daemon`, `github.com/gregberns/harmonik/internal/queue`, `github.com/gregberns/harmonik/internal/handler`, `github.com/gregberns/harmonik/internal/lifecycle/tmux`] |
| BR-02A | `beadRunOne` | 2298 | 1600 | 396 | 270 | 24 | 17 | [`github.com/gregberns/harmonik/internal/daemon`] |
| BR-02B | `beadRunOne` | 2298 | 1250 | 396 | 200 | 24 | 14 | [`github.com/gregberns/harmonik/internal/daemon`] |
| BR-02C | `beadRunOne` | 2298 | 900 | 396 | 150 | 24 | 11 | [`github.com/gregberns/harmonik/internal/daemon`] |
| BR-03 | `beadRunOne` | 2298 | 250 | 396 | 60 | 24 | 4 | [`github.com/gregberns/harmonik/internal/daemon`, `github.com/gregberns/harmonik/internal/queue`] |
| RL-02A | `runReviewLoop` | 1586 | 1200 | 331 | 260 | 13 | 11 | [`github.com/gregberns/harmonik/internal/daemon`] |
| RL-02B | `runReviewLoop` | 1586 | 800 | 331 | 180 | 13 | 8 | [`github.com/gregberns/harmonik/internal/daemon`] |
| RL-02C | `runReviewLoop` | 1586 | 500 | 331 | 110 | 13 | 6 | [`github.com/gregberns/harmonik/internal/daemon`] |
| RL-03 | `runReviewLoop` | 1586 | 250 | 331 | 60 | 13 | 3 | [`github.com/gregberns/harmonik/internal/daemon`] |
| DOT-01 | `driveDotWorkflow` | 1002 | 1002 | 195 | 195 | 6 | 6 | [`github.com/gregberns/harmonik/internal/daemon`, `github.com/gregberns/harmonik/internal/handler`, `github.com/gregberns/harmonik/internal/lifecycle/tmux`, `github.com/gregberns/harmonik/internal/queue`] |
| DOT-02 | `dispatchDotAgenticNode` | 882 | 200 | 185 | 45 | 11 | 5 | [`github.com/gregberns/harmonik/internal/daemon`, `github.com/gregberns/harmonik/internal/queue`] |
| DOT-GATE-01 | `executeCognitionGate` | 370 | 160 | 46 | 30 | 10 | 5 | [`github.com/gregberns/harmonik/internal/daemon`, `github.com/gregberns/harmonik/internal/queue`] |
| DOT-GATE-01 | `internal/daemon/dot_gate.go` | 887 | 400 | 122 | 70 | 14 | 8 | [`github.com/gregberns/harmonik/internal/daemon`] |
| DOT-03 | `driveDotWorkflow` | 1002 | 300 | 195 | 60 | 6 | 3 | [`github.com/gregberns/harmonik/internal/daemon`, `github.com/gregberns/harmonik/internal/queue`] |
| WL-DEPS-DELETE-01 | `workLoopDeps` | 743 | 0 | 0 | 0 | 7 | 0 | [`github.com/gregberns/harmonik/internal/daemon`] |

ARCH-GATE additionally enforces zero reverse
`internal/runloop -> internal/daemon` imports, at most 12 fields in each new
plan/scope record, and at most three methods per consumer-owned port. Those
shape checks are separate boolean gates; they do not substitute non-integer
values into target rows.

### Completion tests

- no required dependency is assigned after constructor return;
- queued/direct invalid combinations fail to compile because no common
  nullable queue fields exist;
- all six hotspot functions plus `dot_gate.go` meet their assigned rows and no extracted function exceeds its
  predecessor's responsibility set;
- package movement is forbidden until daemon-private reach is zero;
- focused characterization, race, fault/restart, and scenario gates pass at
  each node;
- a rollback never leaves a caller depending on a removed boundary.

## Rationale

The DAG extends rather than reorders CQ-02, serializes all known spines, and
turns “shrink” into reviewable integers. The ceilings are deliberately below
coordinator scale while allowing orchestration functions to remain readable.

## Requirements traceability

Every C6 node names prerequisites, writes/lease, intermediate state, and
rollback; the gate table covers span, cognition, reach, fields, and forbidden
imports for every required hotspot.
