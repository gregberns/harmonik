# Change Design — event-model.md amendment

## Decision: NO NORMATIVE CHANGE

Per bridge D12, no new event types are introduced. Relay failure modes route through existing `agent_failed{class, sub_reason}` envelopes; the sub_reason discriminator carries the bridge-specific diagnostic.

The only optional cosmetic change is adding two glossary entries to §3 for cross-reference convenience:
- `hook-relay subprocess` — defined in claude-hook-bridge.md; child of a claude-code handler-subprocess instance, short-lived per-hook-invocation.
- `claude-code` — agent_type whose launch mechanism is defined by claude-hook-bridge.md.

These additions are informative (glossary), not normative; recordable as a NIL amendment to event-model in the integration pass.

## If reviewers DO want a bridge-failure event

Fallback design (not adopted; documented for traceability):

### EV-OPT-1 — `bridge_unreachable` (durability O)

Emitted by the daemon when a watcher-side timeout on the relay-socket-connection regime fires (e.g., 30s without a hook event from a still-running Claude session). Payload: `run_id`, `claude_session_id`, `last_hook_event_at`, `elapsed_ms`.

Rationale for non-adoption: this is the same diagnostic content as `agent_failed{sub_reason=bridge_dial_failed}` plus the silent-hang event's `last_progress_event_at`. Adding a new event multiplies the observability surface without changing any routing decision.

The bridge spec's §10 Conformance section MAY include a recommendation that operator dashboards filter `agent_failed` events on `sub_reason ∈ {bridge_dial_failed, bridge_malformed_hook_payload, bridge_daemon_startup_window_exceeded}` for bridge-specific observability.
