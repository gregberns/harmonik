# Change Design — process-lifecycle.md amendment

## Current state

`process-lifecycle.md` v0.4.1 §4.5 PL-014 covers daemon-child subprocess parentage. Nothing covers grandchildren (relay subprocesses spawned by Claude, which is itself a child of the daemon).

## Target state

One new requirement under §4.5 Agent-subprocess management:

### PL-NEW-1 — Hook-bridge relay subprocesses are not direct daemon children

Some handler subsystems (notably the claude-code bridge per `claude-hook-bridge.md`) cause additional short-lived subprocesses (`harmonik hook-relay`) to be spawned by an agent subprocess (Claude Code). These are GRANDCHILDREN of the daemon, not direct children. PL-014's "daemon child" rule applies to the handler subprocess (Claude itself), not to the relay grandchildren.

Specifically:
- The relay subprocesses are NOT registered with the per-daemon concurrency ceiling (PL-014a). Their fd usage is bounded by Claude's hook-firing rate, not by harmonik's dispatch loop.
- The orphan-sweep (PL-006) does NOT target relay subprocesses. They exit on their own when Claude completes a hook invocation; surviving orphans (relay processes whose parent Claude died mid-invocation) are reaped via OS init-reparenting at daemon death OR by Claude's own process-tree cleanup at SessionEnd.
- The relay's daemon-socket connection is short-lived (single message); the daemon's connection acceptor (PL-003a / per HC-054) treats each connection independently.

Tagging: mechanism

## Rationale

This clarification prevents the orphan-sweep from accidentally killing in-flight relay invocations and prevents the concurrency-ceiling enforcement from miscounting subprocesses. Both would cause incorrect behavior without the explicit carve-out.

## Requirements traceability

- Bridge D1 (subcommand of harmonik), D4 (one-shot connection regime).
- HC-054 (hook-bridge connection regime).
