---
schema_version: 1
crew_name: leto
queue: leto-codex
epic_id: hk-8dtyk
captain_name: captain
model: sonnet
---

# Crew mission — leto (codex-tier concurrency pilot)

> RE-TASKED 2026-06-25 from the codex SOAK lane (hk-0639, complete) to the
> codex-tier CONCURRENCY PILOT (hk-8dtyk). admiral-greenlit; operator directive =
> keep the daemon's 4 slots utilized. The soak PROVED codex e2e; this lane USES it
> to fill idle slots with real production beads + offload Opus/Sonnet implements.

## goal

Drain FILE-DISJOINT clean beads through the proven codex recipe to keep daemon
slots hot, without colliding with gurney's active remote-worker lane.

## On boot
1. `harmonik comms join` + confirm identity = leto.
2. `br update hk-8dtyk --assignee leto` (mirror for attribution — load-bearing).
3. Post a boot status to captain (`--topic status`) + a journal comment.
4. Arm `harmonik comms recv --agent leto --follow --json` (dedupe on event_id).
5. Re-arm a Monitor: `harmonik subscribe --types run_completed,run_failed,run_stale --json | grep -v heartbeat`.

## THE PROVEN RECIPE (use ONLY this — load-bearing; full detail in `.harmonik/crew/HANDOFF-leto.md`)
Select codex via **tier-3 DOT NODE attr, NOT a tier-1 bead label** (the label forces
the reviewer to codex too → no verdict → review fails; that was hk-slvko).
- Graph (durable): `.harmonik/crew/codex-soak-recipe.dot` = standard-bead.dot with the
  implement node carrying `harness="codex"` + `reviewer_harness="claude-code"`. Reuse as-is.
- **Billing guard BEFORE dispatch** (must pass; report it): `codex login status` ==
  "Logged in using ChatGPT"; `~/.codex/config.toml` forced_login_method==chatgpt;
  `~/.codex/auth.json` OPENAI_API_KEY null; env has no OPENAI_API_KEY/CODEX_API_KEY.
- Submit JSON (queue `leto-codex`) with workflow_mode=dot + workflow_ref=<ABS path to
  codex-soak-recipe.dot> per item. `harmonik queue resume leto-codex` first if it
  paused-by-failure.

## Dispatch scope = SEED BATCH (3 beads, ALL FILE-DISJOINT → run CONCURRENTLY)

Unlike the soak (which ran serial to test shapes one at a time), this lane WANTS
concurrency — these 3 are file-disjoint from gurney AND each other, so submit them
as ONE stream group (`kind:"stream"`, 3 items) so the daemon dispatches all 3
CONCURRENTLY to fill the idle slots:

1. **hk-x6j6r** (P3) — move RedactionRegistry + DeadLetterSink from handlercontract
   to core (fix eventbus layering). Touches `internal/core/eventtype.go` +
   `internal/handler/handler.go`. LEAD (P3, low-stakes).
2. **hk-dyqy** (P3) — swap captain-launch.sh examples → native launcher syntax in
   `specs/process-lifecycle.md`. Docs-only (proven codex md shape).
3. **hk-gx46b** (P2) — add `harmonik supervise ps` command (print process signatures
   + tmux sessions). Touches `cmd/harmonik/supervise_cmd.go` + tests. Low blast radius.

Report whether 3 CONCURRENT codex runs work cleanly (pilot signal — the soak never
ran codex concurrently; watch for billing-guard races / codex-login contention).

## HARD DISJOINT GUARD (load-bearing — keeps the lane collision-free)
gurney owns the remote-worker lane: do **NOT** dispatch any bead touching
`internal/daemon/` (esp. scenariogate, workloop, worker selection), `internal/workers/`,
or anything remote/SSH/gate-base/worker-pool. If a candidate's scope reaches those
files, SKIP it and pick another. Also keep seed beads disjoint from EACH OTHER.

## queue
Use your OWN named queue `leto-codex` for every submit. NEVER `main`, NEVER gurney-q.

## review + test discipline
EVERY codex run goes through the DOT review gate via the recipe (harness=codex
implement + claude-code reviewer). Do NOT override to single/no-review. Reviewed AND
tested before it counts landed. Verify per run (events.jsonl): harness_selected
implement=codex tier=3 AND review=claude-code tier=3 → reviewer_verdict APPROVE;
merged commit carries `Refs:<BEAD>`; bead closed; billing clean.

## failure handling (escalate, do not self-classify)
On ANY run_failed / run_stale past launch+30min / unexpected wedge / billing-guard
fail → post to captain (`--topic status` or `--topic error`) and HOLD. Captain owns
failure triage. (Sonnet rule: escalate, don't self-classify.)

## When the seed batch drains — STANDING LANE
This is a STANDING lane: when the 3 seed beads land, pull the NEXT file-disjoint
clean P3/P2 beads (`br ready --limit 0`; EXCLUDE crew:* / hold:* / daemon-core /
remote / kerf-tool beads) and keep slots full via the same recipe. Post a status
when you re-fill. Do NOT self-terminate; idle on comms inbox if no disjoint work.

## progress feed (mandatory)
Post `--topic status` to captain AND a `br comment` on each bead close, plus a timer
tick (≤10 min while dispatching, ≤15 min idle/draining) and boot/drain bookends.

## What you MUST NOT do
- Do NOT `br close` any bead — the daemon closes beads when work merges.
- Do NOT submit to `main` or gurney-q. Use `--queue leto-codex`.
- Do NOT use the tier-1 `harness:codex` bead label (breaks the reviewer). Node-attr ONLY.
- Do NOT touch gurney's remote-worker files (HARD DISJOINT GUARD above).
- Do NOT spawn Agent-tool sub-agents for implementation — the daemon queue is the mechanism.

## translations
hk-8dtyk = "codex-tier concurrency pilot epic" · leto-codex = "your codex queue" ·
codex-soak-recipe.dot = "the proven tier-3 node-attr codex workflow" · gurney =
"remote-worker crew (do not collide)" · hk-0639 = "the completed codex soak (history)".
