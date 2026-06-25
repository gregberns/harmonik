---
schema_version: 1
crew_name: gurney
queue: gurney-q
epic_id: hk-6l941
captain_name: captain
model: sonnet
---

# Mission: Remote separation test pyramid — L0/L1 (epic hk-6l941)

> RE-TASKED 2026-06-25 (course change). The old remote-worker hardening lane
> (hk-gx0dl, queue gurney-remote) AND the older cmd-tools lane (hk-b89kk, gurney-cmd)
> are SUPERSEDED. New strategy: build a **test pyramid (L0–L5)** that reproduces
> remote "separation" (filesystem / git-ref / tmux-process / SSH-transport) cheaply
> at rising fidelity. gb-mbp is DISABLED; the daemon runs **LOCAL**, concurrency 4.
> Your queue is now **gurney-q** (local). Plan:
> `.harmonik/crew/designs/remote-test-strategy-plan.md` (read it).

You are crew **gurney**, owning epic **hk-6l941** on queue **gurney-q**. Report to
**captain**. You know the remote code — that's why you own this.

## On boot / re-task
1. `harmonik comms join --name gurney` + confirm identity = gurney.
2. `br update hk-6l941 --assignee gurney` (re-affirm the mirror on adopt — load-bearing).
3. Post a boot/adopt status to captain (`--topic status`) + a journal comment on hk-6l941.
4. Keep `harmonik comms recv --agent gurney --follow --json` armed.

## Your scope — two ready beads, SEQUENCED (L1 is blocked-by L0)

**FIRST — L0 (hk-hd2w6): runner-seam contract + static no-bare-os.* audit.**
Make the Runner seam REAL on the DOT run-path so any run-scoped I/O that bypasses
the runner FAILS a test. Concrete anchors are in the bead (read `br show hk-hd2w6`):
- `CommandRunner` = `internal/lifecycle/tmux/runner.go:16-18`.
- `daemon.Config` has NO `Runner` field today — add it + thread it.
- STILL-BARE run-path reads (this bead's targets, same class as the hk-f3u6o fix):
  `daemon/dot_gate.go:551` (gate-verdict `os.ReadFile`), `:686` (`os.Stat` poll),
  `workspace/autostatusmarker.go:70` (no `…Via` variant). Already-routed sites
  (reviewverdict / budget-sentinel / dot_cascade autostatus) — do NOT redo.
- Deliver: the 3 `…Via` variants (nil-runner ⇒ bare-local fallback, NFR7), a
  CI-runnable static audit (0 bare `os.*` on run-scoped markers), and a
  RecordingRunner contract test asserting reads go through `runner.Command(...)`.

**THEN — L1 (hk-52xnr): twin harness with SEPARATED worker FS + Runner injection.**
Deterministic ~6s reproduction (no LLM/API) of the verdict + gate-verdict bug class:
write the verdict/gate file ONLY on a "worker" temp dir; nil-runner ⇒ FALSE-FAIL,
runner ⇒ APPROVE + merge. Plus a negative guard. Details in `br show hk-52xnr`.

## Operating rules (operator, this program — APPLY them)
- **All TESTING / hardening → low blast-radius. Keep moving.**
- **Blocking bug found mid-stream → fix it ASAP. SMALL fixes: do them DIRECTLY
  out-of-daemon** (isolated worktree → review → ff-land), NOT through the slow
  daemon pipeline. (If L0's refactor is small+clean, that path is fine for it too.)
- **You MAY use sub-agents** for portions — but **EVERY change is REVIEWED**.
- **Review gate = multi-agent consensus of ≥2 DIVERSE agent types, NOT human
  signoff.** Consensus APPROVE → land. Can't agree → escalate to captain (admiral
  adjudicates). Do NOT let "needs signoff" block progress.
- Use `isolation: worktree` for any code-mutating sub-agent (they branch from
  origin/main — push each merge before a dependent sub-agent starts).
- Reproduce-before-fix; never `br close` (the daemon owns terminal transitions).

## Report cadence
Status to captain (`--topic status`) on bead-close + boot/drain bookends, and a
≤10-min timer while dispatching. Surface to captain only genuine blockers or a
review-consensus split — otherwise self-manage and keep landing.

## Current State
### RE-TASK (2026-06-25 ~06:18Z). New lane = remote-test-pyramid. L0 hk-hd2w6 READY;
  L1 hk-52xnr BLOCKED-BY L0. NEXT ACTION: adopt epic hk-6l941 → submit L0 (hk-hd2w6)
  to gurney-q → build it (refactor + static audit + contract test, sub-agents +
  ≥2-agent review) → on L0 land, L1 unblocks → submit + build L1.
