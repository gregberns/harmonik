---
schema_version: 2
spawned_by: admiral
pass_id: baseline-26a4cfd9
pin_sha: 26a4cfd9ffc48e71ed37e57f01bca1fcf3d67231
pin_branch: main
measured_against: good-enough-principles.md
verdict: PASS
---

# ASSESSMENT — baseline-26a4cfd9 (post-merge baseline)

## VERDICT: PASS (targeted post-merge baseline)

`origin/main @26a4cfd9` (the PR#31 merge — the entire code-revamp) is a **healthy
post-merge floor**: it builds clean at the pin, the forced-LOCAL core-loop LT is green
end-to-end with a real agent, and every merge-touched Go suite passes under `-race`. No
regressions found; no `found-by:assessor` beads filed. Verdict is my reasoned judgment
over the two legs run, not a bead tally.

Scope was the admiral's targeted baseline: pin-critical core-loop (S2/S3) + merge-touched
suites. **Deliberately NOT** the #32/hk-czb11 proof (yankee's E2E + CI Tier2) — no duplication.

## Legs

- **LT (live-verify / S2+S3 core-loop):** PASS. `make core-loop-lt` driven from a clean
  clone detached to the pin (binary built at 26a4cfd9, git-confirmed). Real pi/ornith agent:
  single-mode dispatch → real HEAD change → landed on `core-loop-proof-integ` with main
  UNCHANGED; GAP gap1/gap3/gap4/t10 all pass. `MATRIX_JSON green=1 red=0 pending=0 skip=0
  all_green:true`, exit 0.
- **XT (exploratory break-test / S7 fault-injection):** PASS. Isolated scratch daemon at the
  pin. FAULT A daemon crash-recovery (SIGKILL mid-dispatch → supervisor revive → restart):
  in-flight run re-adopted, `run_id` not durably `""` (RU-01a), no double-dispatch, no
  fabricated-DONE, health green. FAULT B orphan-on-boot reopen (daemon+supervisor killed
  mid-flight): `QM-002a claim_write_lost` revert → re-dispatch as fresh run_id, landed beads
  protected from reopen (`reconcileOrphanedRunsOnResume` skips already-landed). No wedge, no
  lost-wakeup. (Literal agent-PID kill N/A — ornith harness runs in-process; substrate
  constraint, not a finding.) One low-sev housekeeping finding filed (hk-22ml2).
- **CR (independent review):** deferred to CI Tier2 + the prior per-commit agent-reviewer
  passes on the merged branch; this is a post-merge baseline, not a first-time merge gate.

## Per-suite results

| Suite / package | Result | Evidence |
|---|---|---|
| Core-loop LT `pi:local` | GREEN | MATRIX_JSON all_green, exit 0 |
| internal/codexdriver (`-race`) | PASS | 14.3s, no race |
| internal/hookrelay (`-race`) | PASS | 3.0s, no race |
| internal/lifecycle/tmux (`-race`) | PASS | 17.7s, no race |
| internal/substrate (`-race`) | PASS | 1.7s, no race |
| S7 faults (crash-recovery + orphan-reopen) | GREEN | invariants held; 1 low-sev finding hk-22ml2 |

All regression suites run against a verified-clean clone of the pin (HEAD==26a4cfd9,
empty porcelain), isolated GOCACHE, no cache reuse.

## Claimed-done reconciliation

The pin is a merge commit already gated by CI Tier2 (incl. E2E) before merge; the merge
is present and pushed on origin/main. Nothing claimed-but-unbuilt: the build compiles at
the exact pin and the core-loop actually drives a real agent through to a landed change.

## Findings

- **hk-22ml2** (P3, `found-by:assessor,pr-31,known-issue,daemon,worktree-gc`) — restart-reconcile
  leaks the crashed run's git worktree + `run/<run_id>` ref when it reverts an orphaned dispatched
  item (deterministic, reproduced on both faults). Disk/ref accumulation on a repeatedly-crashing
  long-lived daemon; NOT a correctness fault (nothing lost/double/wedged/fabricated). Passive
  known-issue — evidence, not a gate blocker.

No correctness or fault-tolerance regression. Verdict remains PASS.

## Residual risk (for the admiral)

- **S7 fault-injection RAN (admiral-authorized) and PASSED** — crash-recovery + orphan-reopen
  invariants all held. Literal agent-PID-kill variant N/A under the ornith-in-process harness
  (substrate constraint); the reopen property was covered via the orphan-on-boot path instead.
  Broader H-set (H2 lease-truncate, H4/H5 remote truncated-verdict, H6 concurrent-submit,
  H7 drain-while-emitting) not re-run this pass — covered by prior baselines; available on request.
- **Local box is darwin.** GOOS-sensitive paths (the CHB-017 class) are certified on
  macOS only; CI Tier2 Linux remains the authoritative cross-GOOS green (per the standing
  GOOS-parity note). This baseline does not change that.
- **Live branch tip has advanced past the pin** to `942069eb` (hk-czb11, pushed to
  `origin/hk-czb11-land`) — yankee's unmerged #32 work. It is NOT part of this baseline;
  origin/main remains the pin. All legs excluded it via clean/pin-detached clones.

## Coverage

- RUN: 6 (LT core-loop + 4 regression suites + S7 crash/orphan fault-injection). SKIPPED: 0.
- Critic: broader S7 H-set + Codex/claude/remote cells stay behind opt-in / prior-baseline coverage; targeted by design.
