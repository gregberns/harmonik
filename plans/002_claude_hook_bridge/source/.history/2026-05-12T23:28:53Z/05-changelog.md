# Spec-draft Changelog — claude-hook-bridge kerf

## NEW

- `specs/claude-hook-bridge.md` (v0.1, status `draft` on landing; promoted to `reviewed` after Phase 3 review).
  - 22 normative requirements (CHB-001..022) + 3 invariants (CHB-INV-001..003).
  - Scope: §4.1 settings.json materialization (CHB-001..005), §4.2 env-var schema (CHB-006..007), §4.3 claude_session_id flow (CHB-008..009), §4.4 hook-relay subcommand contract (CHB-010..017), §4.5 hook→progress-message mapping (CHB-013..014), §4.6 daemon-socket protocol (CHB-015..017), §4.7 handler-process responsibilities (CHB-018..020), §4.8 twin parity (CHB-021..022).
  - 4 OQs: CHB-001 SO_PEERCRED check, CHB-002 Notification text in output_chunk, CHB-003 hook-conflict warnings, CHB-004 disableAllHooks override.

## AMENDED

- `specs/handler-contract.md` v0.3.3 → v0.3.4:
  - Add HC-053 (pointer to claude-hook-bridge.md for claude-code).
  - Add HC-054 (hook-bridge one-shot NDJSON connection regime).
  - Add HC-055 (handler-side claude_session_id minting / resume / forbidden-flags discipline).
  - Clarifying sentence on HC-006 pointing forward to HC-055.
  - No IDs renumbered or retired.

- `specs/workspace-model.md` v0.4.2 → v0.4.3:
  - Add new sub-section §4.7a Claude-code settings.json materialization.
  - Add WM-038 (settings.json materialization + atomic-write + merge-with-existing).
  - Extend §4.3 WM-013e gitignore set to include `.claude/settings.json`.
  - Add INFORMATIVE retention paragraph at end of §4.7.

- `specs/process-lifecycle.md` v0.4.1 → v0.4.2:
  - Add one requirement under §4.5 clarifying that hook-bridge relay subprocesses are grandchildren of the daemon and not subject to PL-014 / PL-014a / PL-006. Placeholder ID PL-017a (integration assigns final ID).

- `specs/event-model.md`: NO normative change (recommend skip). Optional glossary additions to §3 deferred.

## DOC UPDATES

- `docs/subsystems/agent-runner.md` — replace line-28 hand-wave with cite-forward shape pointing to claude-hook-bridge.md.
- `docs/subsystems/hook-system.md` — add "Realization at MVH" section pointing to bridge spec.
- `AGENT_INDEX.md` — add bridge spec to normative spec inventory.

## Spec-graph delta

- New normative spec: 1 (claude-hook-bridge).
- New normative requirement IDs: 22 + 3 = 25 (CHB-001..022, CHB-INV-001..003) + 3 (HC-053, HC-054, HC-055) + 1 (WM-038) + 1 (PL-017a) = **30 net-new requirement IDs**.
- Renumbered IDs: 0.
- Retired IDs: 0.
- New event types: 0.
- New OQs: 4 (CHB-001..004) + any integration-pass-generated OQs.

## Cross-spec coordination

The new bridge spec is the load-bearing piece. The four amendments are coordinated:

- HC-054 NAMES the connection regime, CHB consumes it.
- HC-055 declares handler obligations, CHB depends on them.
- WM-038 supplies the materialization mechanism, CHB depends on it.
- PL-017a clarifies subprocess-tree semantics, CHB cites it.

No circular dependencies. Cite direction is CHB → HC, CHB → WM, CHB → PL (CHB depends on the others; the others reference CHB via "cite forward" for context).
