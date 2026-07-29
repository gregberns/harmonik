# Open defects found during the delete-and-rewrite program

**Why this file exists.** The bead ledger is machine-local and gitignored — beads do not travel between
clones and never reach CI. A defect that lives only as a bead is invisible to everyone but the machine
that filed it. This file is the tracked record; the bead IDs are a convenience for looking up the full
detail on the machine that has the ledger, not the handle for the thing.

**Standing directive (operator, 2026-07-28):** defects found along the way are **recorded, not chased**.
Nothing here is an instruction to stop and fix it. The point of the table below is the *third* column —
several of these are eliminated as a side effect of work already planned, and knowing which ones saves
someone fixing a thing that is about to be deleted.

Found 2026-07-29 unless noted.

---

## Eliminated by planned work — do not fix these directly

| What is wrong | When it goes away | Bead |
|---|---|---|
| **The credential guard covers 1 of 5 dispatch sites, and not the default one.** `d2RemoteAPIKeyRefusal` runs only on the single-mode path. DOT is the default and carries essentially all traffic, so the 2026-05-30 credential-leak gate protects the path that has run twice ever and not the path everything uses. The conformance test repaired 2026-07-28 guards the guard's *shape*, not its *coverage* — which is why nothing caught this. | Launch-path collapse (§Next step 4) — by construction, since one launch path cannot drift from itself | `hk-z4cow` |
| **Launch-failure classification is computed by all five sites and consumed by none.** `classifyLaunchFailure` maps a launch error onto structural event classes; `dispatchSegmentRun.emit` switches only on two other event types and drops both structural classes into `default:`. Four sites then hand-roll the same check themselves; the fifth emits nothing. The purest instance of the 1-of-N pattern in the tree. | Launch-path collapse (§Next step 4) | `hk-q15hi` |

## Being handled right now by the review-loop retirement

Both are cases where `runReviewLoop` is the **sole** non-test home of a behaviour, so deleting the file
removes it from the product entirely — and neither would fail to compile. Third instance of the pattern
that already cost this program the crew idle-reap tests and the D2 conformance test.

| What is wrong | Status | Bead |
|---|---|---|
| **Crash-recovery resume works only in review-loop mode.** `persistClaudeSessionID` is review-loop-only; single-mode and DOT both capture a Claude session id and drop it. So EM-031 resume does not work on the default mode *today*, and would leave the product with `reviewloop.go`. | In flight — lane instructed to port or consciously retire, with evidence, not drop | `hk-5sebh` |
| **The default mode treats every merge failure as terminal.** `Retryable: runmerge.IsRetryableReason` is passed to the terminal spine only by the review-loop. DOT and single-mode do not retry transient merge failures. Compounding it, a DOT failure never charges the retry budget, so the close-with-needs-attention ladder cannot fire on the default mode either. | In flight — lane has already touched `runbridge.go` to give DOT the retry | `hk-dqmw2` |

## Needs deliberate attention — nothing planned will fix these

| What is wrong | Notes | Bead |
|---|---|---|
| **Lost-commit race.** `TestScenario_MultiBead_SerializedNCompletion` drops commits nondeterministically — fails 5/5 including isolated on a quiet box, and *which* beads lose varies run to run. The beads write non-colliding files by construction, so a merge race is the only remaining explanation. **This is the most serious item in this file**: a correctness defect in the merge path, not a test-quality problem. | Undocumented in any prior campaign | `hk-co8g8` |
| **A flapping SSH makes the remote C2 gate pass.** `runAutoStatusInspection` discards `ErrRemoteTransport`, the sentinel that exists specifically to distinguish "SSH failed, inconclusive" from "confirmed absent". The sibling reader in the same package explicitly retries on it. On the **kept** graph path — survives all Phase 3 deletions. Was the only genuine bug among 152 delta-lint findings. | Independent fix | `hk-sbd4l` |
| **Structural protocol mismatch on the 2nd and 3rd dispatch.** `TestScenario_ConcurrentMultiQueue_N2_HappyPath` fails 4/4 with `error_category=structural / sub_reason=protocol_mismatch` on a deterministic dispatch ordinal — not load. It sits on the known-flake allowlist, wrongly. | Second confirmed case of the allowlist absorbing a real defect | `hk-t2d7n` |
| **Non-single-mode runs cannot be adopted after a daemon restart.** `useIndepSession` is declared before the mode switch but assigned only in the single-mode tail, so the shared worktree-cleanup defer's guard can only be false on that one path. Review-loop and DOT runs lose their worktree on shutdown. | Needs the terminal spine collapsed — a *second* step after the launch-path collapse, not the same one | `hk-mh3qy` |
| **An entire tier of test failures is invisible.** `go test -tags scenario ./internal/daemon/` yields eight failures where the untagged run yields one. Root cause established: no assessment ever ran the tagged tier at all — the recipes only `go vet`ed it and the CI workflow carries `continue-on-error: true`. Full per-test disposition in `NEXT_STEPS.md` §5.2. | Fix is §5.1's merge-blocking gate, then §5.2's hardening | `hk-97gcz` |

## One open P0 that is probably wrong

`hk-zobns` — *"Branch-protection deep guard fails open: bead merges to protected target and closes
approved."* Re-measured 2026-07-29: **the guard did not fail open.** The ref that moved was the
*unprotected* `integration` branch that the test's own bead body asks to land on; every assertion about
`main`, `origin/main` and main's reflog passed. The test's premise went stale on 2026-07-06 (`hk-lgykq`)
when merge-target resolution moved to the per-bead `lands_on` and became **stricter**.

The real cost is coverage, not safety: this was the only work-loop exercise of that backstop, so the
backstop has been unasserted for roughly three weeks. Annotated on the bead rather than re-scoped —
changing a P0's priority is the owner's call.

---

## The pattern worth carrying forward

Six of the eleven items above are instances of one shape: **several code paths perform the same
conceptual step, and only one of them actually applies it.** The full catalogue — seventeen instances,
with a step-by-path matrix — is in `ONE-OF-N-DRIFT.md`. The reason it matters for this program is that
the compiler is silent on every one of them: the callers still compile, the tests still pass, and the
guard simply stops being applied on four paths out of five.

That is also the argument for the launch-path collapse being the highest-value remaining move. It does
not fix these one at a time; it makes the class unable to recur, because one path cannot drift from
itself.
