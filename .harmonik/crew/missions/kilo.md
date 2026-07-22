---
schema_version: 1
crew_name: kilo
queue: kilo-q
epic_id: hk-220lv
goal: "Keeper reliability: fix the three ways a keeper-watched crew silently dies or loses work on restart"
captain_name: captain
---

# Mission: Keeper reliability (P1) — stop keeper-watched crews from silently dying on restart

You are crew member **kilo**, owning the keeper-reliability lane (grouped under lead bead **hk-220lv**) on queue **kilo-q**. Report status to **captain**.

**Why this matters:** these three bugs are *why crews keep dying under the captain*. A keeper watcher that dies with no revive, a restart that discards the handoff a crew paid to write, and a restart that SIGKILLs in-flight work and leaks orphan processes — together they are the "handoff never delivered" abort class (yueh: 3 restarts, 0 completed). Fixing the keeper is the highest-leverage reliability work on the board.

## The subsystem you touch
One subsystem: the **keeper** (the per-orchestrator / per-crew context-fill watcher that drives handoff → /clear → /session-resume before a pane overflows). READ FIRST, in full:
- `.claude/skills/keeper/SKILL.md` — the agent-facing operating contract (two thresholds, command surface, crew-restart re-hydration, `keeper doctor`).
- The keeper source under `cmd/harmonik` and `internal/` (grep for `keeper` — watcher process, `agent brief --wake keeper-restart`, the handoff read path, the run-quiesce/kill path).
- Background research: `plans/2026-07-17-keeper-restart-timing/C6-findings.md` (the C6 findings that opened all three beads).

## What "done" means (the shape, not a checklist)
A keeper-watched crew survives a restart without silently dying or losing work:
- a **dead watcher is detected and auto-revived** — a restart can actually fire because the process that triggers it is alive (hk-220lv);
- a **handoff the crew wrote is consumed on reboot** — the custom-path `HANDOFF-<crew>.md` is read, not cold-re-derived, and the Write "must-Read-first" guard does not fumble a pre-existing handoff (hk-4tjyj);
- a restart **quiesces/checkpoints in-flight runs instead of hard-killing them** — no orphan processes leaking onto the box (hk-bl2k6).

## CRITICAL operating instruction — how you produce commits
The daemon's queue-dispatch reliability is **currently UNPROVEN** on the rolled-back binary — the theme-modal wedge (**hk-8juwz**) can eat dispatched beads. So:
- **IMPLEMENT via your OWN in-crew subagents** (the Agent tool) and **commit reviewed diffs directly on a branch**. Do NOT rely on `harmonik queue submit` daemon-dispatch to produce your commits.
- **Every non-trivial commit gets an independent review** — spawn a reviewer subagent to review the diff before you commit; the verdict lands as the commit's review trailer.
- The **daemon still owns terminal bead status transitions**. Do NOT set beads to `closed` or `in_progress` yourself — leave the terminal transition to the daemon.

## The beads (keeper-reliability lane — run `br show <id>` for full detail)
- **hk-220lv** — Keeper watcher can die silently with no auto-revive. *Intent: add dead-watcher detection + auto-revive so a restart can fire.*
- **hk-4tjyj** — Keeper restart discards a handoff the crew wrote. *Intent: consume the custom-path `HANDOFF-<crew>.md` on reboot instead of cold re-derive; fix the Write guard fumble.*
- **hk-bl2k6** — Keeper restart SIGKILLs in-flight runs, leaking orphan processes. *Intent: quiesce/checkpoint in-flight runs on restart instead of hard-kill.*

## Verification is TWO layers — NOT "one bead cleared"
1. **(a) Heavy in-crew testing during implementation** — run a VERY significant testing process directly via YOUR OWN SUBAGENTS. Beads may not be the right vehicle for this testing; use subagents to exercise the change thoroughly (dead-watcher-revive under a killed watcher, handoff-consumed-on-reboot round-trip, in-flight-run quiesce with no orphan pid leaked, `keeper doctor` liveness). You design the testing; it need not be bead-shaped.
2. **(b) Assessor complete-system test as the GATE** — when implementation is complete, hand to the **assessor** (comms `--to assessor` / captain will route) for a full system test before it is considered done. The bar is "thoroughly tested by the crew + assessor-signed-off," not one bead.

## Shared-file watchpoint (coordination)
The sibling crew (**lima**, dispatch-health lane, epic hk-8juwz) may also touch **internal/daemon** and **cmd/harmonik**. Before finalizing any daemon-file bead, **rebase onto the target branch** so the daemon merge stays clean. If you hit a real conflict on a shared daemon file or test helper, post `--topic status` to captain and it will serialize.

## Model / posture
**Sonnet** — this is an implementation lane, not a design lane. Harness = **CLAUDE** (codex is unproven/unauthorized as a crew driver right now; do NOT route work onto codex).
