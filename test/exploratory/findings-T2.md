# Exploratory Testing Wave — T2 Findings

**Tester:** T2 (subprocess failure modes)
**Date:** 2026-05-12
**Branch:** main (worktree: agent-a99962cddf5c38f63)

## Summary

Subprocess failure handling is broadly correct for the core modes (non-zero
exit → ReopenBead, context-cancel of hanging twin → ReopenBead, malformed
NDJSON does not crash). Three confirmed findings are filed as beads.

---

## Confirmed Findings

### F-T2-01 — Worktrees not cleaned up after handler failure

**Severity:** functional (P2)

**Repro:**
```bash
go test ./internal/daemon/... -run TestT2_WorktreeLeftAfterFailure -v
```

**Expected:** Worktrees created for a failed run are removed after ReopenBead.

**Actual:** `.harmonik/worktrees/<run_id>/` remains on disk after twin-fail
exits 1 and ReopenBead is called. Each retry creates a new worktree; none are
cleaned up. Accumulation rate = 1 worktree per failure per retry cycle.

**Code location:** `internal/daemon/workloop.go` lines 227–266 — no cleanup
path in either the worktree-creation failure branch or the normal failure branch.

**Note:** Already tracked as bead `hk-fgdgz`; this run confirms it with
measurable accumulation.

---

### F-T2-02 — ReopenBead carries no failure reason; bead history is opaque after failure

**Severity:** functional (P2)

**Repro:**
```bash
# After twin-fail exits 1, check br show:
br show <bead-id> --format json   # status=open; no failure_reason field
# JSONL has it:
cat .harmonik/events/events.jsonl | grep run_failed  # summary="auto-reopen: exit=1"
```

**Expected:** `br show` should surface why a bead was reopened (exit code,
run_id) without requiring the operator to grep the JSONL log.

**Actual:** `brcli.Adapter.ReopenBead` issues a plain `br reopen <bead-id>`
with no `--reason` flag. Failure context is JSONL-only.

**Code location:** `internal/brcli/terminaltransition_bi010.go:229` —
`["reopen", beadID]` args only; `internal/daemon/workloop.go:281` — no reason
forwarded.

---

### F-T2-03 — Malformed NDJSON + exit-0 closes bead as success (watcher violation ignored by work loop)

**Severity:** functional (P2)

**Repro:**
```bash
go test ./internal/daemon/... -run TestT2_MalformedNDJSON -v
# Output: events=[run_started agent_failed run_completed] closed=[t2-bead-malformed]
```

**Expected:** `agent_failed` from watcher should cause the work loop to reopen
the bead; protocol violation + exit-0 should not be treated as success.

**Actual:** Watcher emits `agent_failed` on malformed NDJSON (correct per
HC-007b), but the work loop drives bead disposition solely from exit code. Exit
0 → `CloseBead`. The bead is closed as success despite the agent protocol
violation.

**Code location:** `internal/daemon/workloop.go:277` — `outcome.ExitCode == 0`
is the sole decision gate; watcher failure status is not consulted.

---

## Non-Findings (Working Correctly)

| Scenario | Test | Result |
|---|---|---|
| Non-zero exit → ReopenBead | TestT2_NonZeroExit | PASS |
| SIGKILL during run → ReopenBead | TestT2_SIGKILLDuringRun | PASS |
| Context cancel → hang twin killed, ReopenBead | TestT2_HangTwinCtxCancel | PASS (3.002s) |
| Malformed NDJSON → watcher survives, no crash | TestT2_MalformedNDJSON | PASS |
| Silent exit-0 (no NDJSON) → CloseBead | TestT2_ExitZeroNoSignal | PASS |
| No orphan processes after ctx cancel | TestT2_ProcessGroupCleanup | PASS |
| run_failed payload contains exit code | TestT2_RunFailedEventContainsExitCode | PASS |

## Test Artifacts

Test file: `internal/daemon/t2_scenarios_test.go`

Run all T2 scenarios:
```bash
go test ./internal/daemon/... -run "TestT2_" -v -timeout 90s -p 1
```
