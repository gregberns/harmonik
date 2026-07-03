This is the decisive finding. `dispatchDotToolNode` (dot_cascade.go:1232) runs the commit_gate shell node via **`exec.CommandContext(execCtx, "/bin/sh", "-c", node.ToolCommand)` directly on box A** — it does NOT route through the per-run `CommandRunner`/SSHRunner at all. The shell node has no runner parameter. So for a remote run, the commit_gate `go build ./...` executes against **box A's worktree path which does not exist on box A** (the worktree is on gb-mbp). That is the real bug. None of the three plans correctly identified this — all three frame hk-230h as "SSHRunner doesn't forward `cmd.Env`," which is the wrong fix entirely.

I have enough to deliver an unsparing, grounded critique.

---

# ADVERSARIAL ROBUSTNESS REVIEW — Remote-Substrate Phase 1 Test Plans

## VERDICT UP FRONT: all three rest on a WRONG root cause for the gating bug

I verified the gating-bug code directly. **The fix every plan sequences Phase 3 behind is mis-diagnosed**, and that error invalidates each plan's Phase-3a fix dispatch, several Phase-3 assertions, and the "go build ran with box A's env" failure-signature predictions.

**What all three say (WRONG):** hk-230h = "`SSHRunner.Command` (runner.go:92) doesn't forward `cmd.Env`, so the remote commit-gate shell runs with box A's env / a fresh login env." Plans 1 & 3 even prescribe the fix: "inline `cmd.Env` into the remote command (`ssh host -- env K=V -- cmd`)."

**Ground truth (dot_cascade.go:1232 `dispatchDotToolNode`):** the commit_gate shell node does **NOT go through any `CommandRunner`/SSHRunner**. It runs:
```go
cmd := exec.CommandContext(execCtx, "/bin/sh", "-c", node.ToolCommand)
cmd.Dir = wtPath
cmd.Env = append(os.Environ(), env...)
```
— a **plain local `/bin/sh -c` on box A**, with `cmd.Dir = wtPath`. For a remote run, `wtPath` is the **worker's** worktree path (`/Users/gb/harmonik-worker/repo/.harmonik/worktrees/<run_id>`), which **does not exist on box A**. So the gate fails with **chdir/`no such file`**, not an env error. The env is already correctly inherited locally (the comment at :1218 documents the hk-m5axg fix that added `os.Environ()` precisely so `go` is found).

**Consequence:** The real fix is to **route the shell node through the per-run runner** (so it `ssh`'s to the worker and runs `/bin/sh -c` in the worker's worktree), exactly like `fetchBaseOnWorker`/`pushRunBranchOnWorker` already do in `codesync_rs_b8.go`. The env-forwarding fix the plans dispatch would "succeed" against localhost (shared FS, so `cmd.Dir` exists) and **still be wrong on gb-mbp** — the canonical false-confidence trap, and all three walked into it while writing a plan whose entire purpose is to avoid it. **Plan 3 came closest** (its dispatch prompt says "make the commit-gate run via the run's `CommandRunner`") but then contradicts itself by also asking SSHRunner to translate `cmd.Env` — and `dispatchDotToolNode` takes no runner argument today, so the prompt is under-grounded.

**REQUIRED FIX #1 (all plans):** Re-anchor hk-230h to `internal/daemon/dot_cascade.go:1232 dispatchDotToolNode`. The bug is "shell node runs `/bin/sh -c` locally on box A regardless of the run's substrate," not "SSHRunner drops cmd.Env." Investigator must thread the per-run `CommandRunner` into `dispatchDotToolNode` and have it emit `ssh worker -- /bin/sh -c '<cmd>'` with `cd <wtPath>` on the **remote** side. The expected failure signature in Phase 3a is **chdir/no-such-file**, NOT empty-PATH/`go: command not found`. Predicting the wrong signature means the issue-resolution loop chases the wrong artifact.

---

## RANKING

**Plan 3 > Plan 2 > Plan 1**, but all three need the same structural corrections.

- **Plan 3 (best):** strongest verification primitives (V1–V6 as a reusable table, including a `stat -f "%d"` **device-id** check that actually proves separate-FS — the only plan to do so), correct "reproduce-first" discipline in 3a, credits the D2 fail-closed strip, commits workers.yaml before restart (avoids dirty-tree `implementer_escape`), and is honest that localhost tests are necessary-not-sufficient. Still carries the wrong hk-230h root cause.
- **Plan 2 (middle):** uniquely catches the **`~/.zshenv` vs `~/.zprofile` non-interactive-ssh OAuth trap** (the single most likely silent prod failure — and I confirmed workers.yaml's own comment says auth comes from `~/.zshenv`). Correctly flags the DOT HEAD-resolve `chdir` error already recorded on the bead as a *second* live gap. But its PROVE-REMOTE "commit author/committer host resolves to the worker" assertion is **non-deterministic and probably wrong** (git author/committer is identity config, not hostname — see below).
- **Plan 1 (weakest of the three, still solid):** good structure and the clearest billing baseline, but it most confidently asserts the wrong root cause with a fabricated-precise line range (`runner.go:92-98` "never serializes cmd.Env" — the real issue is two files away), and its verification table inherits the same author-host fallacy.

---

## COVERAGE GAPS vs the operator's requirements

1. **D2 fail-closed strip is under-tested as a *positive blocker*, not just a baseline.** I confirmed `workloop.go:2708` **refuses to dispatch a remote run if `ANTHROPIC_API_KEY` is in the spawn env** (`remote run: ANTHROPIC_API_KEY in spawn env (D2 fail-closed)`), and `claudehandler_chb006_024.go:203-338` strips it and re-emits `ANTHROPIC_API_KEY=` / `CLAUDE_CODE_OAUTH_TOKEN=` empty overrides. **Required addition:** an explicit test that with `ANTHROPIC_API_KEY` set in the daemon's own env, a remote dispatch is **refused with that exact reason string** (not merely "health-check fails"). Plans 1/3 gesture at it under offline/Phase-6; none asserts the dispatch-time refusal string at `workloop.go:2708`. This is the actual credit-burn guardrail and must be a first-class assertion.

2. **trivial→escalation is asserted but never *defined or exercised as an escalation path*.** All three test "trivial bead lands" and separately "DOT bead lands," but none tests an **escalation transition** — a bead that starts simple and escalates (e.g. commit-gate FAIL → loop-back-to-implement → eventual close-needs-attention after the cap). The DOT graph (verified) caps the implement↔commit_gate loop at 3 and has an "unconditional fallback → close-needs-attention." **Required addition:** one bead that deliberately exhausts the 3-iteration cap on the worker and asserts it reaches `close-needs-attention` (not a wedge, not silent re-loop). Plans 1 & 3 test a *single* gate-fail-then-recover iteration; none tests cap-exhaustion escalation.

3. **scenario-gate.sh fail-OPEN behavior is asserted nowhere rigorously.** The gate command is `go build ./... && go vet ./... && bash scripts/scenario-gate.sh`, and the DOT comment says scenario tests are fail-open on infra errors. On a remote worker, "infra error" is far more likely (ssh hiccup, missing scenario harness). **Required addition:** prove scenario-gate.sh on the worker fails *open* (transient→self-loop retry, capped at 2) vs. a genuine build/vet failure which fails *closed* (deterministic→implement loop). Plan 1 mentions it in one line; none asserts the transient-vs-deterministic branch split that the DOT node documents.

4. **No plan verifies the daemon actually picked up `--target-branch integration` from `branching.yaml`** beyond "supervise status --json" hand-waving (Plan 3) or "verify in logs" (Plans 1/2). I confirmed `branching.yaml` does not exist, so this is brand-new config. **Required addition:** before dispatching the Phase-5 series, dispatch ONE throwaway bead and assert its merge landed on `integration` — *fail the phase* if it lands on main, rather than discovering leakage after 4–5 beads. (Plans treat the accumulation assertion as post-hoc; it must be a pre-gate.)

5. **Reviewer-on-box-A is asserted by event presence, never by *location*.** All three say "reviewer runs on box A (DEC-C)." Plan 3 alone tries to assert the reviewer worktree is local — but offers no concrete check. **Required addition:** `ls .harmonik/worktrees/` **on box A** shows the reviewer's worktree (or `ssh gb-mbp ls …` shows it absent) during the review window. Otherwise "reviewer on box A" is unproven.

---

## FALSE-CONFIDENCE TRAPS (beyond the root-cause error)

6. **"Commit authored on the worker" via `git log --format='%an %ae %cn %ce'` is a fallacy — present in ALL THREE.** Git author/committer identity is `user.name`/`user.email` config, which is **identical** on box A and gb-mbp if they share a gitconfig (likely — same user `gb`). It says nothing about *where* the commit was created. **The branch is fetched into box A's ref namespace before merge** (`fetchRunBranchBoxA`, codesync_rs_b8.go:80), so by merge time the commit object lives on box A regardless of origin. **Required fix:** drop author-host from the proof set. The defensible proofs that the run executed remotely are: (a) `worker_name=gb-mbp` in `run_started`/`run_completed` (workloop.go:2261/3830 — confirmed), (b) the worktree dir exists **on gb-mbp and is absent on box A** during the run, (c) `pushRunBranchOnWorker` ran (the `run/<id>` branch appeared on origin pushed via the SSH runner), and ideally (d) a side-effect baked into the bead deliverable that only the worker could produce (e.g. the bead writes `hostname` output into its file — then assert the file contains `gb-mbp`). Add (d); it's the only *content-level* proof and none of the plans use it.

7. **"API credit-pool delta = $0" is not machine-checkable in-session and is treated as a hard assertion.** Plans 1 & 3 assert a measured `$0` API delta via "Anthropic Console." That is a manual, eventually-consistent dashboard read — non-deterministic and not scriptable mid-run. **Required fix:** demote the console read to a *corroborating* check and make the **deterministic** billing proof the dispatch-time guardrail (gap #1: D2 refusal string + the empty `ANTHROPIC_API_KEY=`/`CLAUDE_CODE_OAUTH_TOKEN=` overrides in the spawn spec, verifiable from the launch spec / events, plus `ssh gb-mbp 'launchctl getenv ANTHROPIC_API_KEY'` = empty).

8. **Plan 2's offline injection `pkill -f sshd` is wrong and self-defeating.** Killing `sshd` on the worker kills the daemon's *own* control connection but the worker host is still up; worse, `pkill -f sshd` may also nuke the ControlMaster and leave the worker unreachable for *recovery* commands, conflating "worker offline" with "test harness can't talk to worker." `ssh gb-mbp 'sudo ifconfig en0 down'` (Plan 3 / Plan 1's `tailscale down`) is the cleaner partition. **Required fix:** standardize on tailnet-down or `pkill -f claude` (agent death, not transport death) for the two distinct failure modes, and test them **separately** — they exercise different code paths (`IsSSHConnectionFailure`/255 → `worker_offline` at workloop.go:2168/2188/2747 vs. agent-died-but-ssh-alive → no_commit/run_stale).

9. **All three assert "≥2 worktrees coexist" by polling `ls` — racy and easily a false PASS.** With a laptop worker at `max_slots` 3–4, runs can complete fast enough that a single poll never catches two simultaneously, yielding a false negative; conversely a stale worktree from a prior run inflates the count (false positive). **Required fix:** assert concurrency from the **event stream** (≥2 `run_started{worker=gb-mbp}` with no intervening `run_completed` — overlapping run-id lifetimes in `events.jsonl`), corroborated by `ssh gb-mbp 'pgrep -fc claude' ≥ 2`. The `ls` poll is supporting evidence only. Plan 3 says "poll on a tight loop" but doesn't define the deterministic event-overlap check.

---

## MISSING FAILURE MODES / RECOVERY

10. **Stale-binary across the SSH boundary is unaddressed.** All three `go install` on **box A**. But the **worker runs its own `claude`** and (critically) its own checkout at `/Users/gb/harmonik-worker/repo`. None checks the **worker clone's HEAD/binary freshness**. If the worker repo is behind, `fetchBaseOnWorker` may fetch the base SHA fine (it fetches by SHA from origin) — but any worker-side tooling drift (older `go`, older `scripts/scenario-gate.sh`) silently changes gate behavior. **Required addition:** Phase 0 asserts `ssh gb-mbp 'git -C <repo> fetch && go version'` and pins the worker's Go toolchain vs box A's; record both. Plans 3 & 4 mention toolchain drift as a *failure to recover from* but never *pre-check* it.

11. **promote racing daemon merges — only Plan 3 mentions it, none gates it.** The captain-deploy-non-ff lesson is in memory. Plan 5 in all three opens the promote PR "after all N land," but the daemon may still be merging a late bead. **Required addition:** assert `harmonik queue status` shows 0 active/merging before invoking `promote`, and for any push-mode demo, do it in a verified lull. PR-mode is safe (opens a PR, pushes nothing to the target), so the race only bites the optional push-mode demo — call that out.

12. **`-default` orphan-on-restart is mentioned by 1 & 3 but never *asserted clean*.** They include the recreate-if-missing one-liner. **Required addition:** after every `supervise restart`, assert `tmux has-session -t harmonik-<hash>-default` BEFORE the next dispatch — a missing `-default` session is a fleet-wide spawn outage (memory: never kill it; recover by recreating). Make it a checklist gate, not a footnote.

13. **Partial integration-branch state on a mid-series failure.** If bead 3 of a 5-bead Phase-5 series fails or the daemon dies, `integration` has a partial sprint. None of the plans defines the recovery: do you promote the partial set, or hold? **Required addition:** state that `integration` accumulation is resumable — re-dispatch the failed bead onto `integration`, and only `promote --pr` once the intended set is whole; `harmonik reconcile --target-branch integration` (not main) closes any in_progress bead already merged to integration. Plans only invoke reconcile against main.

14. **Merge-conflict across concurrent worktrees is asserted as "0 by construction" — untested as a recovery path.** All three engineer distinct files so conflicts can't happen, then declare merge-serialization proven. That tests the happy path only. **Required addition (at least one plan / the final plan):** deliberately submit two concurrent beads that touch the **same** file, assert the second merge **auto-skips** (the documented behavior), emits the skip, and the skipped bead is **re-dispatchable fresh** (memory: append-to-same-stream stays ineligible → must use a fresh queue). This proves the conflict-recovery machinery, which is the whole point of "one-at-a-time merge with auto-skip."

15. **hk-230h commit-gate gap interaction with single-mode is asserted but not *verified at the routing level*.** Plans correctly use single-mode in Phases 1–2 to dodge the gate. But Plan 2 itself notes the recorded HEAD-resolve `chdir` error appeared and warns "if it appears in single mode, the daemon is defaulting to DOT." **Required addition:** before trusting single-mode as a gate-dodge, assert the submitted run's `workflow_mode` is actually `single` in the queue envelope / run metadata — don't assume `--beads` submit honors it (memory: `beadsToQueueDoc` once stripped `workflow_mode`, landing ~117 beads unreviewed; that exact regression class is live history here).

---

## UNREALISTIC / UNDER-SPECIFIED / WRONG-SEQUENCING

16. **Plan 1 & 2 invoke `harmonik supervise restart` AND a raw `harmonik --project … &` / `pkill` launch interchangeably.** Mixing supervisor-managed and hand-launched daemons risks two daemons / pidfile collision (exit 5) or a supervisor reviving a hand-killed daemon with stale flags. **Required fix:** pick ONE lifecycle path — supervisor-managed via `hk-keeper.sh`/`HK_TARGET_BRANCH` env — and never `pkill` + background-launch in the same plan. Plan 3 is cleanest here (supervise-only).

17. **`harmonik queue set-concurrency N` (Plans 2 & 3) vs `--max-concurrent` restart (Plan 1) — under-specified which actually takes effect for the worker cap.** The worker's `max_slots` (per-worker) and the daemon's global `--max-concurrent` are different ceilings; raising one without the other silently caps concurrency lower than intended, producing a **false-negative** concurrency test (you think you tested 4-wide, you tested 1-wide). **Required fix:** assert BOTH `max_slots` (in workers.yaml, requires restart — confirmed the daemon caches `enabled`/config at boot) AND the global cap are ≥ N, and prove the effective concurrency from the event-overlap check (#9), not from the config you set.

18. **Phase 0 "billing baseline timestamp" (Plan 1) is dead weight** given #7 — a timestamp on a manual dashboard read isn't a test artifact. Replace with the deterministic D2/strip checks.

19. **None sequences the hk-230h fix's own validation through the daemon vs. a worktree sub-agent correctly.** The fix touches `dot_cascade.go` and needs a `//go:build scenario` test that boots a real remote daemon — which (memory) **times out on the daemon's 30-min commit budget**. **Required fix:** author the hk-230h scenario test via a worktree sub-agent + fast local gate + cherry-pick (the scenario-test-authoring convention), NOT by dispatching the test-authoring bead to the daemon. Plan 1 says "author via worktree sub-agent per the scenario-test convention" — good; the final plan must make this mandatory, and the scenario test must run against **gb-mbp**, not localhost (Plan 1 says this; reinforce).

---

## REQUIRED ADDITIONS/FIXES the FINAL plan MUST incorporate (checklist)

1. **Re-anchor hk-230h** to `dot_cascade.go:1232 dispatchDotToolNode` — the bug is "shell node runs `/bin/sh -c` locally on box A with `cmd.Dir=<worker path>`," fix = thread the per-run runner so the gate runs ON the worker; expected failure signature = **chdir/no-such-file**, not env/PATH. Drop the "SSHRunner drops cmd.Env" framing.
2. **Drop "commit author host = worker" from every proof set** (#6); replace with worker-only worktree existence + `worker_name`/`worker_os` metadata + `pushRunBranchOnWorker` evidence + a **bead deliverable that bakes in `hostname`** as content-level remote proof.
3. **Make D2 the deterministic billing proof** (#1, #7): assert the dispatch-time refusal string `remote run: ANTHROPIC_API_KEY in spawn env (D2 fail-closed)` at `workloop.go:2708` and the empty `ANTHROPIC_API_KEY=`/`CLAUDE_CODE_OAUTH_TOKEN=` overrides in the spawn spec; demote the Console `$0` read to corroboration.
4. **Adopt Plan 3's `stat -f "%d"` device-id separate-FS check** as the mandatory localhost-trap guard, and Plan 2's **`~/.zshenv` non-interactive-ssh OAuth check** as a Phase-0 gate (most likely silent prod failure).
5. **Prove concurrency from overlapping run-id lifetimes in the event stream** (#9), with `ls`/`pgrep` as corroboration only; verify BOTH `max_slots` and global `--max-concurrent` ≥ N (#17).
6. **Add escalation tests:** (a) commit-gate cap-exhaustion → `close-needs-attention` on the worker (#2); (b) scenario-gate transient-fail-open vs deterministic-fail-closed branch split (#3).
7. **Add same-file concurrent merge-conflict → auto-skip → fresh-queue re-dispatch** as a real recovery test (#14).
8. **Verify single-mode is actually single** in the run metadata before relying on it to dodge the gate (#15).
9. **Gate Phase 5:** one throwaway bead must land on `integration` before the real series; assert daemon picked up `branching.yaml` (#4); `promote` only in a verified queue lull (#11); reconcile against the **target branch** (integration), define partial-series recovery (#13).
10. **Standardize failure injection** (#8): tailnet-down (transport→`worker_offline`/255) and `pkill -f claude` (agent death→no_commit/run_stale) as two **separate** tests; never `pkill -f sshd`.
11. **Post-restart checklist gate:** assert `-default` session present (#12) and worker re-loaded healthy; use supervise-only lifecycle, never mixed `pkill`+background-launch (#16).
12. **Pre-check worker toolchain/binary freshness** (#10): worker `go version` + repo fetch in Phase 0, pinned vs box A.
13. **Author the hk-230h scenario test via worktree sub-agent + cherry-pick, against gb-mbp not localhost** (#19); reviewer-gate the fix before merge.
14. **Assert reviewer worktree location is box A** concretely (#5).

Strongest elements to carry forward: **Plan 3's** V1–V6 table + device-id check + reproduce-first 3a + dirty-tree-avoidance; **Plan 2's** zshenv-OAuth trap + naming the recorded HEAD-resolve `chdir` as a second live gap; **Plan 1's** explicit fix+deploy cycle with gofumpt gate + `-default` recreate one-liner + the verification-rigor table format. Build the final plan on Plan 3's skeleton, fold in Plan 2's two unique catches, and apply all 14 fixes above — most importantly #1, without which Phase 3 fixes the wrong bug and "passes" only because localhost shares the filesystem.