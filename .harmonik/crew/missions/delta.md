---
schema_version: 1
crew_name: delta
queue: delta-work-q
epic_id: hk-13ff4
captain_name: captain
model: opus
goal: "Drain the sessioncapture+sessioncontext assessor-findings group: hk-13ff4, hk-nqkoz, hk-hi53s, hk-o66xy. Fix each INLINE (explicit-paths), independent-review, report, idle."
---

# Mission: delta — sessioncapture + sessioncontext assessor group

You are crew **delta** (NATO naming). You own an **assessor-findings group** and report to **captain**.
Drain to empty, then idle — do NOT pick up other work.

## Your group (fix in this order, ONE at a time)
1. **hk-13ff4** (P2) — sessioncapture scrub: a secret value containing an escaped quote leaks its tail into the capture corpus.
2. **hk-hi53s** (P2) — sessioncontext SessionIDInterceptor retains entire post-handshake stdout in memory (unbounded buffer growth).
3. **hk-o66xy** (P3) — sessioncontext: handler_capabilities with valid version but empty claude_session_id never fires the callback → version_selected ACK never sent (possible 150s hang).
4. **hk-nqkoz** (P3) — sessioncapture scrub over-redacts author/authority/*token* fields (unanchored substrings) — corrupts replay corpus.

All in the sessioncapture / sessioncontext area — a disjoint file-set from the other crews.

## How you work (INLINE — the daemon worktree-dispatch is currently broken; do NOT `queue submit`)
For each bead: `br show <id>` → reproduce → root-cause → fix in the main working tree → **independent
review** (spawn a reviewer sub-agent; captain gates) → commit **explicit paths only** (NEVER `git add -A`/`.`,
bare `git commit`, `git reset`, or `commit --amend` — shared-index race). Reference the bead id in the
commit subject. Do NOT set in_progress or close — captain/daemon own terminal transitions.

## On boot
0. `harmonik agent brief`.
1. `harmonik comms join --name delta` + confirm identity = delta.
2. Arm `harmonik comms recv --agent delta --follow --json` **via the Monitor tool**.
3. `br update hk-13ff4 --assignee delta` (mirror first bead; re-affirm on each adopt — load-bearing).
4. Post a boot status to captain (`--topic status`).

## Progress feed
`comms --topic status` to captain on each bead-done + a ≤10-min timer while working + boot/drain bookends.

## Keeper restart
Re-read this file, re-join comms as `delta`, re-arm the recv Monitor, re-affirm `--assignee`. Resume the
next unfinished bead.
