# Amendment to specs/process-lifecycle.md (v0.4.1 → v0.4.2)

## Frontmatter

- `version: 0.4.1` → `version: 0.4.2`
- `last-updated: 2026-04-25` → `last-updated: 2026-05-12`

## New requirement

### Add to §4.5 (after PL-017):

#### PL-017a — Hook-bridge relay subprocesses are grandchildren of the daemon

Some handler subsystems (notably the claude-code bridge per [claude-hook-bridge.md]) cause additional short-lived subprocesses (e.g., `harmonik hook-relay`) to be spawned by an agent subprocess. These are GRANDCHILDREN of the daemon, not direct children. PL-014's "daemon child" rule applies to the handler subprocess only.

Specifically:

(a) Relay-grandchild subprocesses are NOT registered with the per-daemon concurrency ceiling (§PL-014a); their fd usage is bounded by the agent subprocess's hook-firing rate, not by harmonik's dispatch loop.

(b) The orphan-sweep §PL-006 MUST NOT target relay-grandchild subprocesses; they exit on their own when the agent subprocess completes its hook invocation, and any survivors (e.g., relay processes whose parent agent died mid-invocation) are reaped via OS init-reparenting at daemon death or by the agent subprocess's own process-tree cleanup at session-end.

(c) The relay's daemon-socket connection regime is governed by [handler-contract.md §4.10 HC-045b] (one-shot NDJSON connections, each independent), NOT by HC-007's long-lived-stream model.

Tags: mechanism

## Revision-history entry

| 2026-05-12 | 0.4.2 | foundation-author | Add PL-017a in §4.5 (gap-filler after PL-017, avoiding collision with existing PL-018 "Daemon is a deterministic Go binary" in §4.6) clarifying that hook-bridge relay subprocesses spawned by an agent subprocess are grandchildren of the daemon and not subject to PL-014, PL-014a, or PL-006. Companion to [claude-hook-bridge.md] new spec. No prior IDs renumbered. Status remains `reviewed`. |
