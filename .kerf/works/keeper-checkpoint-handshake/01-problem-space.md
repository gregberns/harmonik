# Keeper checkpoint handshake

## Summary

The keeper starts a destructive restart cycle at ACT. It submits `/session-handoff` and starts a
short timer before it knows when the agent receives the command. A slow handoff becomes an abort.
Repeated aborts can become a forced process restart.

This policy mistakes useful work and delayed input for agent failure. It also conflicts with the
original goal of an agent-owned transition at a point that preserves session continuity.

## Goals

- Use NOTICE at 170k, WARN at 200k, and HARD at 220k for the first trial.
- Let configuration own all three thresholds and message templates.
- Make Stop a first-class observation and decision point.
- Never let Stop alone authorize `/clear`.
- Let a marked handoff followed by Stop authorize automatic clear and resume.
- Let a marked handoff followed by `restart-now` authorize the same transition.
- Keep observing a requested handoff without classifying normal delay as failure.
- Record enough events to diagnose message delivery, handoff progress, and session transition.
- Keep the pure reactor and narrow port architecture.

## Non-goals

- Do not finalize HARD enforcement in the first normal-path slice.
- Do not infer agent intent from transcript prose.
- Do not require a receipt acknowledgement for restart progress.
- Do not combine Claude and Codex effects in one large watcher.
- Do not change how active Claude subagents survive `/clear`.

## Constraints

- A clear requires a confirmed handoff and a later completion signal or explicit restart request.
- Operator interaction can delay autonomous effects. It cannot suppress safe observation.
- Tmux submission does not prove agent receipt.
- A file mtime change does not prove a complete handoff.
- Existing daemon and build-script work in the shared tree is outside this work.
- The live keeper remains unchanged until focused tests pass.

## Success criteria

- The normative spec separates urgency, opportunity, and restart authority.
- Handoff delay does not emit `cycle_aborted` and does not increment restart escalation.
- The selected NOTICE text and alternate trial lines remain easy to change.
- Tests cover a handoff that arrives after the former deadline.
- Tests cover marked handoff plus later Stop and marked handoff plus `restart-now`.
- Tests prove operator interaction delays effects without hiding Stop or handoff observations.
- The implementation records the decision reason when it waits.

## Affected specifications

- `specs/session-keeper.md`
- `specs/event-model.md` if new durable event types are required
- `specs/agent-input.md` if message receipt becomes a normative event

