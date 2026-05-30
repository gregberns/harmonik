# `.flywheel/` — flywheel operator surface

This directory holds the *operator* configuration for the flywheel orchestrator agent.
The Pi extension that consumes it lives at `../.pi/extensions/flywheel/`.
The runtime state (durable notes, watermark, supervisor lock) lives at `../.harmonik/cognition/` — written by the extension and the (future) `harmonik supervise` command.

## Layout

```
.flywheel/
├── README.md           ← this file
├── goals.md            ← active epics + deferrals + pause signals (operator-edited)
└── skills/             ← fat-skills the agent fetches on demand (read-only at runtime)
    └── README.md
```

## Status (v0 scaffold)

What works now:
- The Pi extension at `../.pi/extensions/flywheel/index.ts` registers two tools (`note`, `reset_context`) and injects context-fullness % into every model call.
- Durable notes append to `.harmonik/cognition/notes.jsonl` and survive context resets.
- On 100% context fullness, the harness forces a save+reseed regardless of the agent.

What is NOT yet wired (intentionally — these are next):
- `harmonik digest` Go subcommand → status sheet builder. Without it, reseed digests are minimal stubs.
- `harmonik supervise` Go subcommand → lifecycle (start/stop/status/attach) + config file at `.harmonik/cognition/config.json`.
- Event ingestion via `harmonik subscribe` → live reactions to daemon events.
- Multi-LLM stratification via `prepareNextTurn`.
- The fat-skills under `skills/`.

## Invoking (manual, while supervise is unbuilt)

From the repo root:

```bash
# install per-extension deps once
( cd .pi/extensions/flywheel && npm install )

# interactive TUI session (auto-discovers .pi/extensions/flywheel)
pi

# headless print mode
pi -p "Read .flywheel/goals.md, then run `kerf next --format=json --only=bead --limit=3` and decide which to dispatch."
```

## Design sources

The authoritative design lives on the kerf bench at `/Users/gb/.kerf/projects/gregberns-harmonik/flywheel/`:

- `01-problem-space.md` — what we're solving and why
- `02-components.md` — affected spec areas
- `04-design/self-managing-architecture.md` — the agent-led + deterministic-floor design (the round-2 synthesis)
- `03-research/` — 22 research components incl. `pi-self-management`, `agent-self-managed-context`, `turn-structure-and-cache`, `harmonik-supervisor-surface`, `layered-instructions`, `kerf-as-priority-source`, `multi-llm-stratification`, `pi-extension-internals-install-memory`, `pi-agent-rust`.
