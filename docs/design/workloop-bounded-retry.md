# Workloop Bounded Retry — Design

> Epic: hk-mb8x4. Filed 2026-05-26 after three independent investigation agents + two reviewer agents converged on the same structural fix.

## Problem

The daemon's `runWorkLoop` (`internal/daemon/workloop.go`) is a single `for {}` loop with **13-14 `continue` statements** across ~470 lines. When `ClaimBead` fails for a reason not caught by the hk-n91y0 string-matching fix, the item is reverted to `pending` and immediately re-eligible, creating an infinite retry loop that starves all other beads.

**Root cause:** Zero retry counting anywhere in the codebase. The loop has no progress invariant — an iteration can cycle without advancing any item toward a terminal state.

**Prior incident (2026-05-26 v65):** hk-isp3y had open dependencies (hk-jtxnr, hk-n51yp). `br claim` rejected it with "cannot claim blocked issue" but `br show` returned status=open. The hk-n91y0 fix only checked ShowBead status for `CoarseStatusBlocked`, missing the open-but-blocked-by-deps case entirely. The daemon retried the same item every 2 seconds indefinitely, blocking all 10 beads in the wave.

## Structural Fix

### 1. Add `Attempts` to `queue.Item`

**File:** `internal/queue/types.go`

```go
type Item struct {
    // ... existing fields ...
    Attempts          int    `json:"attempts"`
    LastFailureReason string `json:"last_failure_reason,omitempty"`
}
```

- Persisted in queue.json (survives daemon restart — intentional)
- Zero-value default is backward-compatible with existing queue.json files
- No schema version bump needed (additive field)

### 2. Enforce `maxItemAttempts` in workloop

**File:** `internal/daemon/workloop.go`

```go
const maxItemAttempts = 3
```

**Increment location:** Phase 3 dispatch-stamp (line ~724), before ClaimBead. Every time the daemon tries to run an item, the counter increments.

**Enforcement:** At dispatch-stamp, if `item.Attempts >= maxItemAttempts`, skip to `evaluateGroupAdvanceWithOutcome(false)` with reason `"max_attempts_exceeded"`. Do not call ClaimBead.

**Claim-failure revert:** Do NOT reset `Attempts` when reverting to pending. The counter is monotonic within a queue lifetime.

### 3. br-ready path (no queue)

**File:** `internal/daemon/workloop.go`

Add `readyPathAttempts map[core.BeadID]int` to the workloop's local state. Same bound, same behavior. Resets on daemon restart (acceptable — the br-ready path is the backward-compat fallback).

### 4. Progress invariant

After this fix, every loop iteration either:
- Dispatches an item (Attempts incremented)
- Marks an item failed (terminal state)
- Sleeps on an empty/paused queue (no item to process)

No iteration can cycle without state change. Infinite loops are structurally impossible.

## Review-loop interaction

The review-loop (implementer → reviewer → re-dispatch on REQUEST_CHANGES) runs entirely inside `beadRunOne`. The `Attempts` counter counts outer-loop dispatches, not inner review iterations. A bead that goes through 3 review iterations still consumes only 1 attempt.

## Existing hk-n91y0 fix

The string-matching fix remains as a **fast-path optimization** — it detects known-permanent errors (blocked deps) on the first attempt and fails immediately without burning 2 more attempts. The counter is the structural backstop.

## Test Plan

### P0 (required before merging the structural fix)
- `TestWorkLoop_ClaimRetryBoundedCount` — permanent ClaimBead error terminates after 3 attempts (hk-8ai2u)
- `TestWorkLoop_AllWaveItemsUnclaimable` — every item fails claim, group terminal (hk-tmhak)

### P1 (required before shipping)
- `TestWorkLoop_ShowBeadErrorRetryBounded` — ShowBead errors don't cause infinite retry (hk-fvpz5)

### P2 (filed, dispatch when ready)
- Unexpected exit codes (hk-cun4l)
- Claim timeout (hk-xorlb)
- Blocked during run (hk-2hygc)
- autoCloseStaleBlockers integration (hk-5t0s6)

### P3 (scale/race)
- 50-item wave with stuck head (hk-oylis, property test)
- Semaphore shutdown race (hk-r6opz)

## Files to change

| File | Change |
|------|--------|
| `internal/queue/types.go` | Add `Attempts`, `LastFailureReason` to `Item` |
| `internal/queue/state.go` | Filter over-limit items in `waveEligible`/`streamEligible` (defense in depth) |
| `internal/daemon/workloop.go` | Enforce max-attempts at dispatch-stamp; add br-ready path map |
| `internal/daemon/workloop_hkn91y0_test.go` | Extend with bounded-retry assertions |
| New test files | Per test plan above |
