# Exploratory Testing Wave — T4 Findings

**Tester:** T4 (bead-state edge cases)
**Date:** 2026-05-12
**Worktree:** `/Users/gb/github/harmonik/.claude/worktrees/agent-a2e6f2075294ad186`
**Branch:** `worktree-agent-a2e6f2075294ad186`
**Test file:** `internal/daemon/t4_exploratory_test.go`
**All 6 probes run green.**

---

## Infrastructure note

The production `harmonik` binary (`cmd/harmonik/main.go`) does not expose
`--br` or `--handler` flags. `daemon.Config.BrPath` is empty in the production
composition root, so the work loop is skipped when the binary is invoked
directly. All T4 probes therefore used the `daemon.ExportedRunWorkLoop` test
seam (same seam as the unit tests in `workloop_test.go`). No API credits
consumed.

---

## Probes and findings

### T4-S1 Empty queue

**Scenario:** Work loop started with zero ready beads.

**Repro:**
```
go test ./internal/daemon/... -run TestT4_EmptyQueue -v
```

**Expected:** Loop idles, polls repeatedly, exits cleanly on cancel.

**Actual:** PASS. Loop called `Ready` once in the 250 ms observation window,
remained alive (did not exit or panic), returned `nil` on context cancel.

**Severity:** N/A — correct behavior confirmed.

---

### T4-S2 Bead claimed externally (ClaimBead conflict)

**Scenario:** `ClaimBead` returns an error on every call (simulates another
process claiming the bead between `Ready` and `ClaimBead`).

**Repro:**
```
go test ./internal/daemon/... -run TestT4_ClaimConflict -v
```

**Expected:** Loop backs off, does not crash, does not emit `run_started`,
exits cleanly on cancel.

**Actual:** PASS. One `ClaimBead` attempt at 300 ms. No `CloseBead`,
`ReopenBead`, or `run_started` observed. Loop returned `nil` on cancel.

**Severity:** N/A — correct behavior confirmed.

---

### T4-S3 ReopenBead path — non-zero exit, then redispatch

**Scenario:** Handler exits non-zero on first dispatch; bead is re-queued via
`ReopenBead`; second dispatch succeeds (exit 0).

**Repro:**
```
go test ./internal/daemon/... -run TestT4_ReopenThenRedispatch -v
```

**Expected:** `ReopenBead` called; `run_failed` emitted; second dispatch
closes the bead and emits `run_completed`.

**Actual:** PASS. `ReopenBead` called, `run_failed` emitted, bead re-queued,
second dispatch closed bead, `run_completed` emitted.

**Severity:** N/A — correct behavior confirmed.

---

### T4-S4 Bead deleted from DB while in flight (CloseBead error)

**Scenario:** `CloseBead` returns an error on every call (simulates bead
deleted from DB while handler was running). Two beads in queue.

**Repro:**
```
go test ./internal/daemon/... -run TestT4_CloseBeadError -v
```

**Expected:** Loop must not crash or hang; should continue to next bead.

**Actual:** PASS on crash/hang. Two `ClaimBead` calls observed (both beads
dispatched). `run_completed` emitted once. Loop exited cleanly on cancel.

**FINDING T4-F1 (functional):** `run_completed` IS emitted even when
`CloseBead` subsequently fails. Observed event sequence:
`[run_started, run_completed]` with `closeErr` injected. JSONL reports success
while the bead remains un-closed in the DB — a split-brain the reconciliation
pass must detect.

**Spec ref:** `workloop.go` steps 9 & 10; `specs/event-model.md §8.1`.

**Severity:** functional — JSONL/bead-state diverge on CloseBead failure.

---

### T4-S5 Two concurrent work loops — ClaimBead exclusion

**Scenario:** Two `ExportedRunWorkLoop` goroutines share the same stub ledger
with one bead in the ready queue.

**Repro:**
```
go test ./internal/daemon/... -run TestT4_ConcurrentLoops -v
```

**Actual:** Bead closed exactly once; `ClaimBead` called once. The stub's
dequeue-on-first-Ready behavior naturally serialized dispatch.

**FINDING T4-F2 (cosmetic / test-gap):** The stub does not enforce claim
exclusion. If both goroutines called `Ready` simultaneously and both received
the same bead before the queue drained, both would proceed to `ClaimBead` and
beyond. Production `brcli.Adapter.ClaimBead` uses `br update --claim` (atomic
in SQLite WAL mode), which the stub does not model. A real-DB concurrent test
is needed to prove production safety.

**Severity:** cosmetic (test gap, not a daemon bug). Production serialization
is provided by SQLite atomics, not the work loop.

---

### T4-S6 Event ordering — run_completed before or after CloseBead?

**Scenario:** Intercept `CloseBead` to determine whether `run_completed` is
already in the event log when `CloseBead` is called.

**Repro:**
```
go test ./internal/daemon/... -run TestT4_EventOrderingOnCloseError -v
```

**Actual:** `run_completed` is emitted AFTER `CloseBead` is called. In
`workloop.go` the success branch is:

```go
_ = deps.brAdapter.CloseBead(...)              // step 10a
emitRunCompleted(..., true, "auto-close: exit=0")  // step 10b
```

This is correct when `CloseBead` succeeds. But combined with T4-F1: when
`CloseBead` fails the error is discarded and `emitRunCompleted` is called
anyway.

**FINDING T4-F3 (functional):** `CloseBead` errors are silently discarded
(`_ = deps.brAdapter.CloseBead(...)` at `workloop.go:278`). After a CloseBead
failure the loop emits `run_completed` with `success=true` and continues to
the next iteration. No error event, no reopen, no log line. A bead the DB
thinks is `in_progress` (CloseBead failed) appears as `run_completed` in
the JSONL.

**Spec ref:** `workloop.go:278`; `specs/event-model.md §8.1`.

**Severity:** functional — silent error produces misleading JSONL; reconciliation
will detect the split-brain on next startup but operator tooling reads JSONL as
authoritative.

---

## Summary of confirmed findings

| ID     | Severity   | Description |
|--------|------------|-------------|
| T4-F1  | functional | `run_completed` emitted even when `CloseBead` fails (JSONL/bead-state split-brain) |
| T4-F2  | cosmetic   | Concurrent-loop stub test cannot prove production claim exclusion (test gap) |
| T4-F3  | functional | `CloseBead` errors silently discarded at `workloop.go:278`; no error event emitted |

T4-F1 and T4-F3 describe the same root cause from two angles. Dedup candidate
for the synthesizer: merge into one finding.

---

## Behavior confirmed correct (no finding)

| Probe | Verdict |
|-------|---------|
| T4-S1 Empty queue | Loop idles without crash; exits cleanly on cancel |
| T4-S2 ClaimBead conflict | Back-off on failure; no stale event; clean exit |
| T4-S3 ReopenBead → redispatch | Correct reopen + re-dispatch + close cycle |
| T4-S5 Concurrent loops (stub) | Single bead dispatched once within stub constraints |

---

## Pre-existing test flakiness (not T4-introduced)

`TestRunSocketListener_BindsAndSetsMode` in `internal/daemon/socket_test.go`
fails intermittently when run in parallel with other tests (`socket mode = 0755,
want 0600` — umask race between parallel goroutines). Pre-existed T4.
