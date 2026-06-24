---
schema_version: 1
crew_name: leto
queue: leto-ev
epic_id: hk-uunpf
captain_name: captain
model: sonnet
---

# Crew mission — leto (event-model conformance lane)

> NOTE: this mission file was REPURPOSED 2026-06-24 from the old flywheel-wiring
> lane to the event-model conformance lane (captain directive: fill the 4-instance
> capacity with file-disjoint work). The old flywheel scope is recoverable from
> `codename:flywheel`.

## goal

Event-model durability/replay conformance (Phase 1): close the two
`internal/eventbus` conformance gaps from the event-model audit.

## On boot
1. `harmonik comms join` + confirm identity = leto.
2. `br update hk-uunpf --assignee leto` (mirror for attribution — load-bearing).
3. Post a boot status to captain (`--topic status`) + a journal comment.
4. Arm `harmonik comms recv --agent leto --follow --json`.

## Dispatch scope = TWO beads, STRICT SERIAL (both edit busimpl.go)

These two beads BOTH edit `internal/eventbus/busimpl.go`, so they CANNOT run
concurrently — dispatch ONE, let it CLOSE + merge, THEN dispatch the next:

1. **hk-uunpf FIRST** (P1, G1, EV-016) — the static `fsyncBoundaryEventTypes` map
   (`busimpl.go:115-138`) is missing ≥15 spec-declared F-class event types
   (reviewer_verdict, review_loop_cycle_complete, queue_*, handler_*,
   decision_required/acknowledged, bead_sync_failed, …). They silently downgrade
   to O-class (no fsync) → loss on hard crash violates EV-016/EV-INV-002. Register
   the missing F-class types. G4 (hk-u3q6o, the EventType constants) is ALREADY
   CLOSED, so the constants exist — use them.
2. **hk-0wvmv SECOND** (P2, G3, EV-014d) — `ReplayFrom`/`DeadLetterReplay`
   (`busimpl.go:1085-1092`) both return nil; startup subscriptions with a
   since/offset_checkpoint must get a JSONL-tail replay before live delivery.
   `jsonlwriter.go:ScanAfter` exists but is unwired; `TailTruncationCallback`
   declared but never invoked. Reference the original impl approach in commit
   `fc249711` (eventbus changes ONLY — that commit wedged because it edited
   daemon.go). Supersedes hk-9duau.

## HARD GUARD (load-bearing — fc249711 violated it and wedged on rebase)
Do **NOT** edit paul's daemon hold files: `dot_cascade.go`, `workloop.go`,
`daemon.go`, `stalewatch.go`, `reviewloop.go`. Implement the `internal/eventbus` +
`internal/core` portion ONLY. If daemon-side wiring is genuinely needed, leave a
clear TODO + commit-body note and file a follow-up bead — do NOT block, do NOT
edit those files. This is what keeps the lane file-disjoint from paul.

## queue
Use your OWN named queue `leto-ev` for every `harmonik queue submit`. NEVER the
main queue, NEVER another crew's queue.

## review + test discipline
Every bead dispatches through the daemon's DOT (sonnet triple-review) graph — do
NOT override to single/no-review mode. The spec (`specs/event-model.md`) is
normative; implement to EV-016 / EV-014d / EV-INV-002, do not redesign the bus.
Reviewed AND tested before it counts landed.

## failure handling (escalate, do not self-classify)
On ANY `run_failed` / `run_stale` past launch+30min / unexpected wedge — or if a
fix seems to REQUIRE touching a paul hold file — post to the captain over comms
(`--topic status` or `--topic error`) and HOLD. The captain owns failure triage
and any hold-file routing decision for this lane.

## progress feed (mandatory)
Post `--topic status` to captain AND a `br comment` on each bead close, plus a
timer tick (≤10 min while dispatching, ≤15 min idle/draining) and boot/drain
bookends.

## What you MUST NOT do
- Do NOT `br close` any bead — the daemon closes beads when their work merges.
- Do NOT submit to `main`. Use `--queue leto-ev`.
- Do NOT run hk-uunpf and hk-0wvmv concurrently (busimpl.go collision).
- Do NOT spawn Agent-tool sub-agents for implementation — the daemon queue is the
  dispatch mechanism.

## When the lane drains
Both beads landed → post a completion status and idle on your comms inbox for the
next assignment. Do NOT self-terminate.

## Current State (2026-06-24, batch 2 COMPLETE — HOLDING)

queue_id: (none — leto-ev drained; paused-by-failure from T7's expected run_failed, resume before any next submit)
epic: hk-0639 (CODEX SOAK) — assignee=leto. Original event-model lane (hk-uunpf/hk-0wvmv) DONE; re-tasked to codex soak.
in_flight: none
monitor: re-arm subscribe + comms recv --follow on boot
next_action: **HOLD — idle on comms inbox for next fill-slot pick (operator away).** Batch 2 DONE 3/3 + real-fix shape DONE. Soak shapes ALL proven: md-append (b1 x4), .go-edit (T2 edc871df), with-tests (T5 0c26f5f1), no-op (T7 hk-a8yve correct no_commit), real-bug-fix-with-tests (hk-spx63 → 74f72098, scripts/ops-monitor-check.sh + ops_monitor_check_test.go, full suite 232/0). Captain captured batches 1+2 as conclusive PASS. If captain re-tasks fill-slot: pull next file-disjoint logmine follow-up (not crew:paul / not hold:* / not daemon-core) via the proven recipe.
blockers: none
**FULL handoff incl. the PROVEN RECIPE + per-run steps: `.harmonik/crew/HANDOFF-leto.md` — READ IT FIRST.** Recipe graph (durable): `.harmonik/crew/codex-soak-recipe.dot` (node-attr harness=codex + reviewer_harness=claude-code; NO bead label). Submit DOT mode + workflow_ref, sequential, resume leto-ev before each submit.
translations: hk-0639 = "codex soak epic"; hk-n05u2 = "codex re-canary (PASS)"; ef64h/04cbea35 = "DOT-path codex commit-fallback fix (landed)"; hk-2jxqg = "durable label-vs-node-pin fix (captain-owned)"
stranded (leave for GC, do NOT br close): hk-9cj9f, hk-slvko, hk-a8yve (T7 expected-fail no-op)
