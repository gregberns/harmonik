# Amendment to specs/event-model.md (v0.4.0 → v0.4.1)

## Frontmatter

- `version: 0.4.0` → `version: 0.4.1`
- `last-updated: 2026-05-12` (unchanged date, version bump only — or skip-no-op alternative below)

## Decision: NO normative changes to event-model.md

Per [claude-hook-bridge.md §4] design, the bridge introduces zero new event types. Relay-failure modes route through existing `agent_failed{class, sub_reason}` envelopes with new sub-reason values declared in [claude-hook-bridge.md §8]. No new EV-NNN requirements. No new §8.x event types.

## Optional glossary additions to §3

If the integration pass decides to publish v0.4.1 for glossary-only convenience cross-refs:

- **hook-relay subprocess** — short-lived subprocess invocation of `harmonik hook-relay <event-kind>` spawned by a claude-code agent subprocess via a settings.json hook; emits a single progress-stream NDJSON message to the daemon socket. Defined normatively in [claude-hook-bridge.md §3].
- **claude-code** — `agent_type` value identifying the Claude Code CLI as the handler subprocess implementation. Launch mechanism defined by [claude-hook-bridge.md].

## Revision-history entry (only if glossary added)

| 2026-05-12 | 0.4.1 | foundation-author | Glossary §3 additions for `hook-relay subprocess` and `claude-code` cross-referencing [claude-hook-bridge.md] new spec. No new event types; no requirement IDs added or renumbered. Status remains `reviewed`. |

## Recommended action

**Skip the amendment.** Glossary cross-refs are nice-to-have, not normative. Keep event-model at v0.4.0. The integration pass MAY revisit if reviewers prefer the explicit glossary.
