# Stale graph finding triage

**Date:** 2026-08-02
**Scope:** Queue dogfood readiness task 7

## Result

All four old graph findings are stale. Each condition has a current source
fix and a focused regression test. No current replacement finding is needed.

The local Beads ledger has no record for any old ID. `br show --format json`
returned `ISSUE_NOT_FOUND` for each ID. This triage made no ledger change. The
daemon owns terminal Beads transitions.

| Old finding | Current source evidence | Focused proof |
|---|---|---|
| `hk-v4wer` | `internal/daemon/dot_cascade_helpers.go` classifies the Stop-hook outcome before a DOT node can succeed. Commit `27c7a5fb8` added the change. | `TestDotNode_FailureSignalAfterACommitFailsTheNode`, `TestDotNode_NonZeroExitWithNoReportFailsTheNode`, and their clean controls in `internal/daemon/dot_node_terminal_test.go` passed. |
| `hk-o4sgg` | `internal/daemon/dot_cascade_core.go` refuses an unreadable pre-node HEAD baseline. Commit `84527a54b` added the change. | `TestDotNode_UnreadableBaselineDoesNotPassANodeThatDidNoWork` and its controls in `internal/daemon/dot_node_baseline_test.go` passed. |
| `hk-fmere` | `internal/daemon/workloop.go` fails a no-review DOT run when it cannot read the post-node HEAD. Commit `9152cc266` added the change. | `TestLegacySingleInput_NoReviewDOTUnreadableHeadDoesNotClose` and its controls in `internal/daemon/single_nocommit_headprobe_test.go` passed. |
| `hk-b4xf2` | `internal/daemon/dot_cascade_core.go` attaches the graph-node lifecycle machine to the run. `internal/daemon/workloop.go` records its terminal state. Commit `4229df241` added both changes. | `TestDotNode_SessionMachineReachesTheRunHandle` and `TestDotNode_SessionReachesATerminalLifecycleState` in `internal/daemon/dot_node_lifecycle_test.go` passed. |

## Verification command

```sh
go test ./internal/daemon -run '^(TestDotNode_(FailureSignalAfterACommitFailsTheNode|CleanReportAfterACommitStillClosesTheBead|NonZeroExitWithNoReportFailsTheNode|CleanExitWithNoReportStillClosesTheBead|MalformedProgressStreamFailsTheNode|UnreadableBaselineDoesNotPassANodeThatDidNoWork|ReadableBaselineFailsANodeThatDidNoWork|HealthyRunWithARunnerStillCloses|SessionMachineReachesTheRunHandle|SessionReachesATerminalLifecycleState)|TestLegacySingleInput_NoReviewDOT(UnreadableHeadDoesNotClose|NoCommitReopens|HealthyRunCloses))$'
```

The command passed on 2026-08-02. It did not start a fleet daemon.
