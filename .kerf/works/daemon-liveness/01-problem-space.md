# Problem Space — daemon-liveness

## What we're trying to accomplish

The orchestrator (main claude session driving harmonik) needs a reliable way to know whether a dispatched implementer is **actually making progress** vs **stuck/hung/dead**. Today the daemon kills any implementer that hasn't produced a commit within 10 wall-clock minutes (`pasteinject.go:104 commitPollTimeout`). This is wrong because:

1. **Real implementer work routinely exceeds 10 min** — exploration + multi-file edits + tests. Killing them mid-edit loses the work (often into main's working tree pre-hk-6zylj fix; now just lost).
2. **Trivial work (1-line fixes) also failed at the 10-min wall** on 2026-05-22 — even hk-ortkx (1-line test-arity fix) couldn't land in 10 min on the post-eb43a6b binary. Wall-clock is the wrong signal regardless of bead size.
3. **Operator has no liveness signal during the run** — daemon emits `agent_heartbeat` events at ~5min intervals (we already do this), but nothing watches them. Operator finds out about hangs only at run-end.

## The user's two intuitions (2026-05-23)

1. **Daemon should emit heartbeat the orchestrator can check.** The agent_heartbeat events already exist. The missing piece is an orchestrator-facing surface (subscribe? CLI poll?) that lets the orchestrator (or operator) say "is run X still alive?" without scraping events.jsonl.

2. **Could look at the agent's tmux screen to see if anything is going on.** Less clear what's involved. tmux pane capture is cheap — `tmux capture-pane -t <session>:<window> -p` returns the visible buffer. The daemon could include a "last N bytes of pane" snapshot in heartbeats, or expose a `harmonik inspect <run_id>` command that does it on demand.

## Scope of THIS work (kerf daemon-liveness)

The proper redesign — but NOT the immediate hotfix.

- Replace wall-clock `commitPollTimeout` with heartbeat-staleness check
- Add operator-facing liveness surface (`harmonik inspect <run_id>` returning {last_heartbeat_ts, age, last_pane_capture, current_phase})
- Maintain a kill path for genuinely-stuck implementers (e.g. heartbeat absent for >N min), but NOT a wall-clock budget
- Add per-bead override (some beads legitimately take 30+ min)

## Out of scope

- The deeper problem: this whole brittle implement-→-commit-→-merge pattern is what DOT is meant to replace. Once DOT lands, much of this becomes irrelevant. **`daemon-liveness` is a survival layer until DOT is operational** — keep it minimal.

## Immediate stop-gap (NOT this work — done inline 2026-05-23)

Before this work lands: orchestrator HARD-RULE that every harmonik dispatch arms a heartbeat-staleness watcher in addition to the bash-task + events.jsonl monitors. If heartbeats stop, surface to operator before the 10-min wall kill arrives. Documented in HANDOFF directives.

## Open questions for design pass

- What is the right heartbeat-staleness threshold? (5min? 10min? heartbeat-interval × 3?)
- Should the daemon ever auto-kill, or always surface to operator?
- How does this interact with the existing pasteinject quit-on-commit watchdog?
- What happens when implementer is mid-large-edit and heartbeat would NORMALLY pause (e.g. claude is thinking on a complex tool call)?
- Pane-capture in heartbeat payload — security/privacy concerns? (probably none for local dev, but worth noting)

## Refs

- hk-7srrd P1 — original bead for "replace commitPollTimeout with heartbeat-based check" (will be subsumed by this work)
- hk-6zylj — worktree-escape fix that exposed the wall-kill behavior loudly
- `pasteinject.go:104` — current `commitPollTimeout = 10 * time.Minute`
- `agent_heartbeat` events — already emitted at ~5min intervals
- DOT work `phase-3-dot` — eventual replacement; this is the survival layer until DOT ships
