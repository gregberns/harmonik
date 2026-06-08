<!-- PP-TRIAL:v2 2026-06-08 main in-sync origin — CLEAN. Daemon HEALTHY (pid 95878, -c6) on the hk-9vp51-fixed binary; flywheel ONLINE (session-keeper/fwkeeper lane). THIS SESSION (named-queues): re-landed + live-smoke-validated the reverted P0 daemon fix hk-9vp51; shipped the productization batch + standard-bead.dot (T1 hk-p0kum). OPEN THREAD: the DOT dispatch-default flip (hk-30vlb) is HELD — robustifying the gate first (Option A); hk-9ruez hang FIXED, hk-4f5ua scenario-fix IN-FLIGHT, then hk-u830m gate-scope, then flip + live-smoke. **Role handoff with full next-steps: HANDOFF-named-queues.md** (read it). -->

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
