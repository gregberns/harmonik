# Plan 003: extqueue v0.1 — external-orchestrator queue control surface

## Objective
Land the queue subsystem (`internal/queue/`) that lets an external orchestrator submit/append/dry-run/status an ordered sequence of `wave` + `stream` group primitives over the daemon's Unix socket, with crash-resilient `.harmonik/queue.json` state.

## Status
mostly-done

## What's done
- Spec drafted, reviewed, integrated, and landed: `specs/queue-model.md` v0.1.1 (commits `e228bc3` v0.1.0, `e2a11ee` gap-closure).
- 6 amended specs (queue-model, execution-model, beads-integration, process-lifecycle, event-model, operator-nfr) all in-tree.
- Implementation epic `hk-lj0pb` opened with 24 child tasks; 23 closed.
- Package scaffold + types + events: T10 (`1cbb83c`), T11 (`24fc...`/`24fc7ab6`), T12 (`7cad636`), T20 (`7a09c08`), T21.
- Persistence: T30 atomic write (`2752279`), T31 startup load (`9b15f62`), T32 unlink (`0b8fd46`).
- Core mechanics: T40 validation (`2c1a7fb`), T41 group state machine (`a928423`), T42 append (`73c78ee`).
- Dispatch: T50 workloop queue-pull rewrite (`bb2baa4`), T51 concurrency primitives.
- Transport: T60 JSON-RPC handlers (`51a3f1c`), T61 error codes (`b76461d`), T62 CLI (`083e14a`).
- Daemon glue: T70 queue DI (`17fa2d5`), full composition wiring as `hk-gi471` (`9925ce7`, closed `9b89471`).
- Tests: T80 wave-lifecycle scenario (`e46fc5b`), T81 paused-by-failure (`384f7a2`), T82 crash-recovery (`8201de3`), T83 validation unit sweep (`7d54202`), T84 enqueue-retirement conformance sensor (`28113ae`).

## What's remaining
- `hk-dji5z` — T71: retire PL-013 Beads-poll idle-wait, replace with socket-block idle (only open child of `hk-lj0pb`).
- "Append-while-running" test gap noted in recent investigation (not yet a bead).
- Deferred v0.2 surface tracked in `specs/queue-model.md §A.3`: queue-resume, queue-clear, queue-remove, multi-orchestrator, stream priorities.
- Optional polish flagged during integration pass: `reconciliation/spec.md:57` BI-013a prose tweak; pre-existing `beads-integration.md:205` WM-007 miscitation (out-of-scope for v0.1).

## References
- code: `internal/queue/` package; composition-root wiring at commit `9925ce7`.
- specs: `specs/queue-model.md`, plus amendments in `specs/{execution-model,beads-integration,process-lifecycle,event-model,operator-nfr}.md`.
- beads: epic `hk-lj0pb` (label `codename:extqueue`); only-open child `hk-dji5z`.
- docs: kerf source at `plans/003_extqueue/source/` (SESSION.md is the single-session summary; 06-integration.md captures the cross-corpus audit; 07-tasks.md is the 22-task decomposition).
- chat-context: full v0.1 kerf cycle completed in one agent-orchestrated session 2026-05-14; implementation landed 2026-05-14 → 2026-05-15 in ~28 commits; daemon composition-root wiring closed today (`hk-gi471` / `9925ce7` / `9b89471`).

## Next steps
- Dispatch `hk-dji5z` (T71 socket-block idle) — last open child of the epic.
- File a bead for the append-while-running test gap if it remains uncovered.
- After T71 closes, close epic `hk-lj0pb`.

## Open questions
- None for v0.1. v0.2 surface defer-list is itself the next plan-shaped decision.
