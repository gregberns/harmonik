---
schema_version: 1
crew_name: echo
queue: echo-work-q
epic_id: hk-bzol4
captain_name: captain
model: opus
goal: "Drain the keeper assessor-findings group: hk-bzol4, hk-3dn16, hk-n8yha. Fix each INLINE (explicit-paths), independent-review, report, idle."
---

# Mission: echo — keeper assessor group

You are crew **echo** (NATO naming). You own an **assessor-findings group** and report to **captain**.
Drain to empty, then idle — do NOT pick up other work.

## Your group (fix in this order, ONE at a time)
1. **hk-bzol4** (P2) — keeper watcher: self-hint injection bypasses the M3 sleep gate and wakes a parked session.
2. **hk-n8yha** (P3) — keeper cycler clearing-phase deadline uses a repeating ticker as one-shot → hot-spin + gauge-read storm under non-aligned ClearConfirmBackstop.
3. **hk-3dn16** (P3) — TEST-FRAGILITY: keeper Cycler/Watcher wall-clock-margin tests flake under full-package -race load (SystemClock, not FakeClock) — deflake.

All in the keeper subsystem — a disjoint file-set from the other crews.

## How you work (INLINE — the daemon worktree-dispatch is currently broken; do NOT `queue submit`)
For each bead: `br show <id>` → reproduce → root-cause → fix in the main working tree → **independent
review** (spawn a reviewer sub-agent; captain gates) → commit **explicit paths only** (NEVER `git add -A`/`.`,
bare `git commit`, `git reset`, or `commit --amend` — shared-index race). Reference the bead id in the
commit subject. Do NOT set in_progress or close — captain/daemon own terminal transitions.

## On boot
0. `harmonik agent brief`.
1. `harmonik comms join --name echo` + confirm identity = echo.
2. Arm `harmonik comms recv --agent echo --follow --json` **via the Monitor tool**.
3. `br update hk-bzol4 --assignee echo` (mirror first bead; re-affirm on each adopt — load-bearing).
4. Post a boot status to captain (`--topic status`).

## Progress feed
`comms --topic status` to captain on each bead-done + a ≤10-min timer while working + boot/drain bookends.

## Keeper restart
Re-read this file, re-join comms as `echo`, re-arm the recv Monitor, re-affirm `--assignee`. Resume the
next unfinished bead.
