<!-- PP-TRIAL:v2 2026-06-08 PM main @2d0988e0 in-sync origin — CLEAN, no user blocker. THE FLIP IS DONE: daemon runs --workflow-mode dot (UN-PINNED, pid 53995, -c6), validated end-to-end (hk-m5axg resolved eadda06e — was the commit_gate stripped-env/PATH bug, not the review node; live-smoke hk-03k7n merged ba825032). DO NOT re-pin review-loop (review-loop is only a floor-fallback). Also landed: hk-sah87 (2d0988e0, diff-scaled reviewer budget — merged but NOT yet deployed; running daemon predates it) + hk-fkpb7 partial (40948fde). **LIVE CROSS-AGENT ISSUE: main is RED pre-existing** — TestBI010c_SpecContainsWorkflowLabelDiscipline fails in internal/brcli at ba825032; the DOT commit_gate is package-scoped so any bead touching a red package wedges (this wedged captain T1/T6; commits good, salvage after green). controlpoints filed hk-jk8ii (P1) + is fixing main green; captain dispatch HELD until green. Separate daemon-lane issue: hk-4l7zs (2nd-implement no-spawn wedge). 3 agents share the daemon: named-queues (daemon/queues), flywheel (session-keeper + v0.1.0 release-infra on branch release-infra), controlpoints (captain). Role handoffs: HANDOFF-named-queues.md / HANDOFF-flywheel.md (read yours). -->

Read order (per CLAUDE.md): AGENT_INDEX.md → STATUS.md → TASKS.md. Cross-project rules: `~/.claude/CLAUDE.md`. Dispatch loop: skill `harmonik-dispatch`.

ROLE: orchestrator. Delegate via the daemon queue / sub-agents; keep the main thread minimal. Failed-twice → investigator, don't re-dispatch.

# Where we are (2026-06-02) — CLEAN, nothing blocking
Main `5c51df8f`, `0/0` origin, build green, daemon UP at `--max-concurrent 6` on the latest binary. This was an autonomous overnight run with a peer agent (`flywheel`) while the operator slept. All major work landed + deployed + validated.

## What shipped this session
- **`set-concurrency` (operator ask, `hk-ohiaf`):** runtime-adjustable dispatch ceiling — `harmonik queue set-concurrency N` (no restart; lowering drains-down, never kills). Concurrency now 6.
- **Daemon-reliability cluster (6 fixes, all deployed):** `hk-77q8e` escape-detector (no longer false-fails concurrent beads on a dirty main), `hk-5pg37` reconciler reaps cancel/restart orphans, `hk-4kuvj` `cancel` name-targeting, `hk-a11re` cross-queue dispatch dedup, `hk-6ri5k` deferred-status gating, + set-concurrency. The multi-agent model is materially more robust now.
- **agent-comms event bus (kerf work `agent-comms`, epic `hk-uxm0j`, T1-T13 ALL landed):** `harmonik comms send/recv/who/log/join/leave` + `subscribe --to/--from/--topic`; `agent_message`+`agent_presence` events; durable per-agent cursor (at-least-once, dedupe on `event_id`); one shared `matchAgentMessage` predicate (live+replay). Validated end-to-end; flywheel + named-queues now coordinate THROUGH it.
- **Backlog hygiene:** closed 3 stale beads (`hk-dgwf4` P0 + `hk-hlmup` + `hk-dv8qv` — exit-17 was a misdiagnosis; dv8qv already fixed).

## What changes your plan (READ THIS)
1. **Comms is the `harmonik comms` BUS now — the `.md` outboxes are RETIRED.** Monitor incoming with a persistent `harmonik comms recv --agent <you> --follow --json`. (See the `agent-comms` skill.)
2. **Daemon deploy = `go install ./cmd/harmonik` then `pkill -f "harmonik --project"`.** A keeper (`/tmp/hk-keeper.sh`) auto-revives on the new binary in ~5s at `-c6`. Do NOT manually `tmux`-restart — it loses the pidfile race to the keeper. Change live ceiling via `set-concurrency`.
3. **Named-queue lifecycle verbs are reliable for submit/append but `cancel <name>` is FIXED but verify; pause/resume were flaky pre-fix.** Route concurrent work to your OWN `--queue <name>`, not shared `main`.

## Deferred for the operator (their call, not auto-dispatched)
- `hk-ymav1` — auto-tune `--max-concurrent` from `~/.claude` token-rate (needs operator to calibrate the subscription-token ceiling; design in the bead).
- `hk-ulp7v` (rename refactor), `hk-x6j6r` (eventbus layering move — may want operator input).

## Files to open first
`.kerf/works/agent-comms/` (05-spec-draft.md, 07-tasks.md) · `internal/daemon/workloop.go` (dispatch gate) · `internal/queue/` (comms ops in socket.go) · the `agent-comms` skill.

# Translations glossary
- **named-queues / flywheel** — the two concurrent Claude orchestrator sessions (peers) sharing one daemon. A `/clear` can mis-ID which one you are (on 2026-06-02 flywheel mis-read this very line, thought it was named-queues, and aliased itself `nq-resume` before correcting). Determine your identity by LANE, not this line: daemon+queues+scenario-tests = named-queues; ledger+kerf hygiene = flywheel. Check which HANDOFF-<role>.md is yours.
- **agent-comms bus** — the new `harmonik comms` inter-agent messaging feature (replaces the AGENT_COMMS.md file hack).
- **keeper** — `/tmp/hk-keeper.sh`, the while-loop that auto-revives the daemon on death (at `-c6`).
- **set-concurrency** — runtime daemon dispatch-ceiling RPC (`hk-ohiaf`).
- **T1-T13** — the agent-comms build tasks (named-queues built T1/T2/T4/T6/T7/T8; flywheel T3/T5/T9/T10/T11/T12/T13).

# No hard blockers. Daemon healthy, bus live, both agents idle/resting. Next: deferred beads above, or new work from `kerf next`.
