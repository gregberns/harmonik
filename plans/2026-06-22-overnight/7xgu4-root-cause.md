# hk-7xgu4 — root-cause synthesis (major-issue fan-out, 2026-06-22)

**Bug:** DOT runs intermittently fail with `implementer_budget_exceeded reason=total-budget-stale-active` → `no_progress_detected` → `run_failed "HEAD did not advance"` at iteration 2. This is the bug that killed the operator's #1 (C1 workflow-mode) DOT run on **hk-y3o51** (run `019eedd9-1de4-75b4-8be7-f47942252e3d`, 06:27:52Z) — and 13 earlier occurrences.

**Method:** 6 parallel diagnostic angles + 2 adversarial verifiers + live event-bus trace. wixms (1c84fd1f) does NOT fix it (wixms patches the `review→implement` edge; every one of the 14 occurrences re-enters via the **`commit_gate→implement`** edge).

## The failure, from the live trace (run 019eedd9)

```
start        → implement          05:41      iter-1 implementer
implement    → commit_gate        05:54:32   iter-1 committed CLEAN (commit_landed:true)
commit_gate  → implement          05:54:48   gate FAILED in 16s → bounce to iter-2
implement    → commit_gate        06:27:53   iter-2 NO-OP (commit_landed:false), budget tripped
commit_gate  → review_correctness 06:32:51   gate PASSED on the SAME tree → but...
                                              no_progress_detected → run_failed
```
- Gate-1 FAILED in **16s**; Gate-2 on the same committed tree PASSED in **298s**; a fresh re-run of the exact gate command on the iter-1 tip (74a4af2c) today PASSED in **42s**. 16s is far too fast for a real test failure (that night's genuine test-FAILs took 258–262s).
- The `implementer_resumed` (iter-2) payload carries the false nudge verbatim: *"Your previous pass produced NO commit — HEAD did not advance … you MUST commit."* — but iter-1 DID commit.

## Five defects (adversarially vetted)

### FIX 1 — false "NO commit" resume nudge  *(ROOT CAUSE — HIGH)*
`internal/daemon/dot_cascade.go:733-763`. The back-edge resume message branches only on `fromReviewerRC` (reviewer + REQUEST_CHANGES). A `commit_gate` node is non-reviewer, so a gate→implement bounce ALWAYS falls to the `else` and emits the hardcoded "you produced NO commit" nudge — false when the implementer committed and the gate failed for a build/test reason. The resumed implementer gets a contradictory, non-actionable instruction → idles → 30-min budget trip.
- **Fix (two-part — the proposed one-part fix is INFEASIBLE):**
  1. **Capture step (the harder half, omitted by the naïve fix):** `outcome.Notes` (the 4096-byte gate failure tail, produced at `dot_cascade.go:1855-1895`) is loop-local and discarded; only `lastGatePassed` bool persists (`:436`). Add `lastGateNotes := outcome.Notes` alongside `lastGatePassed` at ~`:436`.
  2. **Deliver step:** in the `else` at `:748`, when `prevNode` is the commit_gate AND `!lastGatePassed`, deliver `lastGateNotes` ("the commit gate failed:\n<notes>\nfix and re-commit") instead of the "NO commit" nudge. Keep the existing nudge ONLY for a genuine no-commit re-entry.
  - **Disambiguate on `lastGatePassed==false && prevNode==commit_gate`, NOT node-type** — else it mis-fires on the genuine no-commit case.

### FIX 2 — transient gate failure misclassified as `deterministic`  *(TRIGGER — HIGH)*
`internal/daemon/dot_cascade.go:1891-1895`. Any non-zero gate exit (1..255) is classed `FailureClassDeterministic` → routed down the fix-loop edge (`workflow.dot:134` / `standard-bead.dot:156`) instead of the transient self-loop. So a transient go-build-cache failure under concurrent gating (5 runs gated at 05:41; the known cache-TOCTOU lineage — b7101d3d / hk-y3frr / hk-guez) bounces a clean, ultimately-gate-green bead.
- **Adversarial note (verifier #1):** the precise framing is NOT "the gate is flaky on identical inputs" — the gate builds the *working tree + go cache*, both of which differed. The actionable root cause is **transient/infra failure misclassification**, not gate logic.
- **Fix:** classify fast build/vet failures whose output matches cache/toolchain-infra signatures as `transient` (self-loop / retry), not `deterministic`. Confirm the cache-contention hardening (paul's y3frr/guez lineage) covers concurrent whole-repo `go build`.

### FIX 3 — no completion exemption for "valid commit + green gate, no reviewer verdict yet"  *(DEFENSE-IN-DEPTH — MED)*
`internal/daemon/dot_cascade.go:523` (no-progress guard) + `:568/:599` (exemptions). Even after Gate-2 PASSED on the valid committed tree (06:32:51 → review_correctness), the run still died on `no_progress` because the exemptions only cover prior-verdict APPROVE / advisory-RC — not "committed + gate-green + no reviewer verdict yet."
- **Fix:** extend the exemption so a gate-green committed tree completes-to-review instead of no-progress-failing. (Precedent: cap-hit salvage `:998-1006`.)

### FIX 4 — DOT implementer budget-watchdog heartbeat starvation  *(LOW-PRI PARITY FOLLOW-UP)*
`internal/daemon/dot_cascade.go:1340`. The per-node heartbeat loop emits to `deps.bus`, bypassing the per-run tap the implementer watchdog reads (`watchdogCh := tap.Subscribe()`, `:1494`). So the DOT implementer watchdog NEVER sees recurring heartbeats (single-mode `workloop.go:3712` correctly uses the tap). Confirmed empirically: 6 heartbeats fired 05:59–06:24, watchdog tripped anyway.
- **Adversarial note (verifier #2): SECONDARY.** The activity-fingerprint path (worktree + pane-output `#{history_size}`) extends the budget for genuinely-working implementers; only a *silent-pane long-think* is exposed. Fix FIX 1 and this incident no longer idles. **Low priority.**
- **Fix (must be scoped or it regresses 3cb51c4b/hk-sj6a):** `emitterTarget := deps.bus; if !isReviewer { emitterTarget = tap }`. The tap is per-node (reviewer XOR implementer), so a `!isReviewer`-scoped route to the tap feeds `watchdogCh` without ever touching `reviewerHBCh`. An UNSCOPED route-through-tap re-opens the reviewer-freeze bug.

## Dispatch plan
All five fixes live in `internal/daemon/dot_cascade.go` (FIX 4 also conceptually pairs with `pasteinject.go`). They touch overlapping regions of `driveDotWorkflow` → **serialize, do NOT parallelize** (same-file collision). Order: FIX 1 → FIX 2 → FIX 3 → FIX 4. Owner: paul (daemon-reliability domain) once its current lane drains. Crews must NOT blind-dispatch hk-7xgu4 itself (reproduces the 60–90min thrash); these scoped fix beads carry the precise specs.
