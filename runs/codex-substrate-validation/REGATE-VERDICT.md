# RE-GATE VERDICT — codex-substrate-unblock — **BLOCK**

- **Pin:** `fff3d937` (branch tip `phase1-session-restart-substrate`; NOT merged to main — validated in isolation per admiral's deploy-not-test call)
- **Fixes under test:** hk-daegv=`a0619c1c` (sandbox danger-full-access at codex launch) + hk-qxvc2=`fff3d937` (isolate remote claude `CLAUDE_CONFIG_DIR`; claim-closes hk-g5wkt)
- **Assessor:** assessor · **Verdict posted to:** admiral `--topic gate`
- **Date:** 2026-07-19 (re-gate after keeper-restart; prior verdict was BLOCK on the same two defects)

## Bottom line
**BLOCK.** The fixes pass their unit tests but do **not** clear the live wedge. In the live remote gate the PRIMARY acceptance — the pinned claude review node reaching `agent_ready` and the DOT bead reaching close — **failed again** with `agent_ready_timeout` → `run_failed`, and the codex implement node **landed no commit**. This is exactly the class of fix (green unit tests, dead live behavior) the assessor gate exists to catch. **Do NOT reboot the prod codex daemon.**

## Legs

### Leg A — local, self-contained — PASS (3/3 GREEN)
Fail-closed boundary guard (unit + real-dispatch refusal) + local exec + ssh git-lifecycle all green on the fresh `fff3d937` build. Report: `REGATE-LEG-A.md`. No defects. Binary provenance verified: `harmonik version` = `fff3d937…`; binary contains `sandbox_mode="danger-full-access"`, `codexHeadlessSandbox`, and the fail-closed guard string — **both fixes are compiled in.**

### Leg B — live remote (gb-mbp iso daemon) — **FAIL (the decider)**
Run `019f7bfd-7ffb-7649-b8b9-b300dcc527fa`, bead `lrp-0cu`, DOT workflow (codex implement tier-1 → claude-opus review tier-3).

**Timeline (events.jsonl, typed):**
- `20:07:20` run_started
- `20:09:27` implementer_phase_complete — **`commit_landed: false`**, exit 0, duration 124s; stderr: `ERROR codex_core::tools::router: error=exec_command failed for '/bin/zsh -lc 'rm -rf __pycache__ && git status --short'': CreateProce[ss…]`
- `20:09:31` reviewer_launched (claude-code, `claude-opus-4-8`, workflow_mode=dot)
- `20:13:01` **agent_ready_timeout** (`timeout_ms: 150000`)
- `20:13:01` **run_failed** — `dot: agentic node "review" failed: node "review" agent_ready_timeout`
- `20:13:02` queue_group_completed / queue_paused

**Leg 1 (hk-daegv — remote codex commit LANDS a run/* branch): FAIL.** `commit_landed:false`; run branch `run/019f7bfd-…` created but carries no new commit; codex `exec_command` shell-spawn error while trying `git status`. The fix is in the binary, yet no commit landed.

**Leg 2 / PRIMARY (hk-qxvc2 — review node reaches agent_ready + DOT bead closes): FAIL — but the failure mode SHIFTED.** The review claude agent **did launch and initialize** (worker `~/.claude.json` `hasCompletedOnboarding:true`; session jsonl written), so the config-dir isolation likely cleared the *original* onboarding-modal stall. But the agent then reported *"The session's working context is gone — there's no task for me to act on; the worktree … has been removed"* and never emitted the `agent_ready` handshake → timed out at 150s. Net result identical to the prior runs (201s/189s): **agent_ready never reached, run_failed.**

## Diagnosis (actionable — CORRECTED by the delegated Leg B subagent's process-level evidence)
The two failures are **INDEPENDENT defects, not a causal chain** (my initial "downstream/linked" read was refuted by `ps eww` + rollout evidence). Fix both in parallel. Full evidence: `plans/2026-07-17-assessor-daemon-campaign/runs/codex-substrate-validation/REGATE-LEG-B.md`.

1. **hk-daegv — codex 0.142.0 doesn't honor the sandbox override.** The `-c sandbox_mode="danger-full-access"` flag IS delivered (shares `Options.Args` with the app-server token), but codex-cli **0.142.0** on the worker ignores it for the exec seatbelt — the rollout system prompt shows `sandbox_mode: workspace-write` + `Operation not permitted`. Codex's own commit failed; the edit landed only via the **daemon fallback** (`a627c0e feat(codex): … auto-committed by daemon fallback`). The fix was only verified on codex 0.144.5. **Remedy: adapt to the installed codex version (per [[no-external-version-binding]]) — do NOT pin 0.144.5.**
2. **hk-qxvc2 — `CLAUDE_CONFIG_DIR` never reaches the remote claude process.** The isolated `<worktree>/.harmonik/claude-config/.claude.json` IS seeded (135 KB), but `ps eww` on the live review claude (pid 98702) shows **zero `CLAUDE_CONFIG_DIR`**. The remote claude launches via `/usr/bin/login … zsh -c 'cd <wt> && exec claude …'` with no env prefix; `LaunchSpec.Env` (Step 5a's `CLAUDE_CONFIG_DIR`) is not injected into the remote exec, so claude reads the shared `~/.claude.json` and wedges (`agent_ready_stall_detected` 188s → timeout). **Remedy: propagate `LaunchSpec.Env` into the remote claude exec.** (Note: shared `~/.claude.json` reports `hasCompletedOnboarding:true` and concurrent-write artifacts — `.claude.json.tmp.*`, `.claude-racetest.json` — were present, so the wedging modal is a trust/permissions prompt or config race, not first-run onboarding.)

## Findings (record)
- hk-qxvc2 / hk-daegv are **NOT** resolved by this pin at the live-gate bar — annotated on the existing beads (comment), not re-filed (avoid dup churn per prior hk-36xy5/hk-wwyse consolidation). Both remain gate-blocking.

## Release call
Deploy stays **HELD**. Only a fresh assessor PASS re-opens the prod codex reboot. Admiral owns the release call and the epic→main PR.
