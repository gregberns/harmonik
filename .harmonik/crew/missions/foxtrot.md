---
schema_version: 1
crew_name: foxtrot
queue: foxtrot-work-q
epic_id: hk-cjqyn
captain_name: captain
model: opus
goal: "Drain the remote/tmux/handlercontract assessor-findings group: hk-cjqyn, hk-9ngiv, hk-btl1n. Fix each INLINE (explicit-paths), independent-review, report, idle."
---

# Mission: foxtrot — remote / tmuxsubstrate / handlercontract assessor group

You are crew **foxtrot** (NATO naming). You own an **assessor-findings group** and report to **captain**.
Drain to empty, then idle — do NOT pick up other work.

## Your group (fix in this order, ONE at a time)
1. **hk-cjqyn** (P2) — tmuxsubstrate remote runWait: ANY WindowPanePID error → exitCodeClean(0); an SSH drop mid-run becomes a false-green auto-close (G8/H4/H5). Gate-weighty.
2. **hk-9ngiv** (P2) — handlercontract watcher: version-negotiation failure loses the ErrProtocolMismatch sentinel (wrapped as generic ErrStructural) → retry-spin.
3. **hk-btl1n** (P3) — remote session Kill: no forceful kill of the remote pane PID (SendKeysQuit + kill-window only) — asymmetry vs the hardened local path.

All in the remote / tmuxsubstrate / handlercontract area — a disjoint file-set from the other crews.

## How you work (INLINE — the daemon worktree-dispatch is currently broken; do NOT `queue submit`)
For each bead: `br show <id>` → reproduce → root-cause → fix in the main working tree → **independent
review** (spawn a reviewer sub-agent; captain gates) → commit **explicit paths only** (NEVER `git add -A`/`.`,
bare `git commit`, `git reset`, or `commit --amend` — shared-index race). Reference the bead id in the
commit subject. Do NOT set in_progress or close — captain/daemon own terminal transitions.

## On boot
0. `harmonik agent brief`.
1. `harmonik comms join --name foxtrot` + confirm identity = foxtrot.
2. Arm `harmonik comms recv --agent foxtrot --follow --json` **via the Monitor tool**.
3. `br update hk-cjqyn --assignee foxtrot` (mirror first bead; re-affirm on each adopt — load-bearing).
4. Post a boot status to captain (`--topic status`).

## Progress feed
`comms --topic status` to captain on each bead-done + a ≤10-min timer while working + boot/drain bookends.

## Keeper restart
Re-read this file, re-join comms as `foxtrot`, re-arm the recv Monitor, re-affirm `--assignee`. Resume the
next unfinished bead.
