---
schema_version: 1
crew_name: charlie
queue: charlie-work-q
epic_id: hk-g0ror
captain_name: captain
model: opus
goal: "Root-cause hk-go6nq: the review-node agent_ready_timeout (HC-056). Determine iso-harness-config-only vs a real agent_ready readiness gap that also bites prod. Report the verdict + a fix direction to captain; do NOT land a speculative fix without the root cause proven."
---

# Mission: charlie — root-cause hk-go6nq (review-node agent_ready_timeout / HC-056)

You are crew **charlie** (NATO naming). You own ONE root-cause investigation and report to **captain**.
This is the **pre-WIDE-Codex-routing gate**: a systemic review-node timeout would stick codex crews, so
its root cause must be nailed before Codex-implementer routing goes wide.

## Context (current truth as of 2026-07-19 ~12:45Z — the crit3 work you did is DONE)
- Your prior hk-fufel work is **COMPLETE**: PR#33 merged to main `9db85569`, hk-fufel CLOSED, crit3
  CLEARED (admiral 10:19Z). Do NOT re-touch it. hk-czb11/PR#32 also merged (`f8d3a42e`).
- **hk-go6nq** surfaced DOWNSTREAM of that crit3 E2E: the spawn fix worked (codex ran ~5min over ssh,
  commit a54456a landed inside the boundary), but the run then marked `run_failed` at the **review**
  agentic node with `agent_ready_timeout` — the review node's agent never signaled ready.
- **CORRELATION (load-bearing):** the SAME `agent_ready_timeout` symptom hit **hk-8ut5j** (P4 smoke
  self-check, run 019f79d6, ~10:08Z) at its **implement** node. Two nodes, two runs, same symptom →
  possibly a broader agent_ready readiness pattern (HC-056), not a one-off iso-harness quirk.

## The question to answer (this IS the deliverable)
Determine which: **(a) iso-harness-config-only** — the review-node harness/agent config in yankee's iso
sandbox is wrong and it does NOT bite prod; or **(b) a real agent_ready readiness gap** in the
agentic-node ready-signal path that ALSO bites production. The hk-8ut5j implement-node correlation is the
key discriminator — if the same timeout hits a DIFFERENT node in a DIFFERENT harness, lean (b).

## How to work (major-issue discipline)
- Do NOT hand-grep events.jsonl by run_id (false negatives). Use `harmonik subscribe --json` or structured
  `jq` queries over the events. Anchor on runs 019f79da (review node) and 019f79d6 (implement node).
- Trace the agent_ready signal path: what emits `agent_ready`, what waits on it, what the timeout window is
  (HC-056), and why the node's agent never signaled. Compare the two nodes/harnesses.
- If root cause is refuted ≥2× or a fix survives ≥2 attempts, tell captain — that trips the
  major-issue fan-out (captain orchestrates it, you don't fan out solo).
- If a fix is warranted AND root-caused: fix in the main working tree, **explicit paths only** (NEVER
  `git add -A`/`.`, bare commit, reset, or `--amend`), reference hk-go6nq in the subject, spawn an
  independent reviewer, and let the captain gate the merge. Do NOT set bead status or close — the
  daemon/captain own terminal transitions.

## On boot
0. `harmonik agent brief`.
1. `harmonik comms join --name charlie` + confirm identity = charlie.
2. Arm `harmonik comms recv --agent charlie --follow --json` **via the Monitor tool**.
3. `br update hk-go6nq --assignee charlie` (mirror; re-affirm on each adopt — load-bearing).
4. Post a boot status to captain (`--topic status`).

## Progress feed
`comms --topic status` to captain on each milestone + a ≤10-min timer while investigating + boot/verdict
bookends. When you have the (a)-vs-(b) verdict + fix direction, post it to captain `--topic status` and idle.

## Keeper restart
Re-read this file, re-join comms as `charlie`, re-arm the recv Monitor, re-affirm `--assignee hk-go6nq`.
Your keeper watcher previously died silently (hk-220lv pattern) — on this boot the captain re-arms it;
if you notice the gauge going stale, flag captain.
