# Fix — Bug 3 (hk-vpnp): automatic ACT cycle loops + truncates handoff to 0

**Date:** 2026-06-20 · **Branch:** `worktree-agent-a9acc91670372a2f7` (built atop
the merged-to-worktree `89852bb3` restart-now simplification).

Bug 3, per `plans/2026-06-20-keeper-investigation-recovery/README.md` §2:
> Injects `/session-handoff`, times out before `/clear` completes, re-fires with
> a new nonce, and truncated the handoff file to 0 lines between cycles. Observed
> live, nonces `-000001` / `-000002`.

`89852bb3` only fixed the **manual** `restart-now` path. The **automatic** cycle
(`MaybeRun` / `runCycle` in `internal/keeper/cycle.go`) was untouched — this fix
targets it.

## Root cause

Two distinct defects in `runCycle` / `MaybeRun`:

**3b — unconditional truncation (cycle.go step 2).** `runCycle` called
`TruncateHandoffFn(handoffPath)` BEFORE injecting `/session-handoff`, to stop a
stale nonce pre-satisfying the poll (DEFECT-2). But it truncated **whatever was
there**, including a genuine, non-empty operator/agent handoff. When the cycle
then ABORTED (nonce never confirmed → `/clear` never issued), the prior handoff
was already wiped to 0 lines. The next cycle wiped it again. The fence the
truncate provided was redundant: cycle nonces are unique
(`newCycleIDGen` = process-start timestamp + per-process sequence), so the
**current** cycle's nonce can never already be present — only a **prior** cycle's
nonce can. So the destruction bought nothing the unique nonce didn't already.

**3a — escape-hatch re-arm after an abort (cycle.go ~line 536).** After an abort,
`runCycle` sets `lastFiredSID = cf.SessionID` to suppress re-fire. But the
same-SID anti-loop escape hatch resets `lastFiredSID=""` whenever it sees the
same SID drop below `WarnPct` — on the assumption that "a real `/clear` happened
(ClearSettle timeout) so re-arm." After an **abort**, no `/clear` ever ran, so a
same-SID below-warn reading is gauge noise (the truncated handoff / a transient
repaint), not evidence of a clear. The hatch re-armed, the next high-context tick
re-fired a **fresh nonce**, truncated again → the live `-000001`/`-000002` loop.

## Failing → passing test

New namespaced file `internal/keeper/act_loop_hkvpnp_test.go`, two tests using
the existing injectable seams (real on-disk handoff I/O for 3b; spy injector +
oscillating gauge for 3a):

- `TestActLoop_HKVPNP_DoesNotTruncateNonEmptyHandoffOnTimeout` — seeds a real
  non-empty handoff, runs a cycle whose nonce never confirms, asserts `/clear`
  was never issued AND the handoff survived (non-empty).
- `TestActLoop_HKVPNP_DoesNotRefireSecondNonceAfterTimeout` — drives several
  ticks against the same un-cleared, high-context SID with a post-abort below-warn
  dip, asserts only **one** `/session-handoff` is ever injected.

Against pre-fix code: **both FAIL** (3b: "handoff truncated to 0 lines"; 3a:
"re-fired 3 /session-handoff nonces … (loop); want 1"). After the fix: both PASS.

## The fix (minimal, in-spirit)

`internal/keeper/cycle.go` only:

1. **3b:** Step 2 now truncates **only when the handoff already carries a keeper
   nonce that is not the current cycle's** — via new helpers `handoffHasStaleNonce`
   / `isOnlyNonce` (+ const `nonceMarkerPrefix`). A genuine handoff with no keeper
   nonce is preserved. The stale-nonce defense (DEFECT-2) is kept; the destruction
   of real content is removed.
2. **3a:** New `Cycler.lastFireWasAbort` field. Set `true` at the abort site,
   `false` on a completed cycle. Both escape hatches (`MaybeRun` and
   `RunForPrecompact`) now re-arm only when `!lastFireWasAbort` — i.e. only after a
   cycle where `/clear` actually ran. After an abort, re-fire is governed solely by
   the existing rate-limited Gate-6 force-retry path, not by the gauge-noise hatch.

No new abstraction layers; no signature changes; all seams already existed.

## Results

- `go test ./internal/keeper/...` — **green** (188 tests; ran 8×, stable). One
  transient FAIL on the very first run was a pre-existing timing flake in the
  watcher/respawn tests under CPU contention — did not recur across 7 subsequent
  runs and is unrelated to this change.
- `go build ./...` — **OK**.
- `go vet ./internal/keeper/...` — **OK**.
- `go test ./cmd/harmonik/ -run Keeper` — **PASS**.
- No live-tmux/looping smoke run. `tmux ls | wc -l` = 9 before and 9 after the
  suite (no session leak).
