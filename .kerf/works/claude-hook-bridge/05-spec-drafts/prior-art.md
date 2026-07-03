# Spec-draft pointer — prior-art

The prior-art thread didn't produce its own normative requirements; it
shaped where the consolidated spec content lives. This file is a thin
pointer.

- **Master draft:** `./claude-hook-bridge.md`
- **Amendment files touched by this thread:** `handler-contract-amendment.md`
  (relay/handler/daemon split slots into existing surface)

Requirements influenced (not exclusively owned) by this thread:

- **CHB-010** — Relay subcommand surface (chosen over separate binary by
  precedent from in-repo adapter pattern).
- **CHB-021** — Twin emits the same wire format (twin parity follows the
  existing twin-binary precedent).
- **CHB-022** — Daemon is twin-blind (precedent for daemon ignorance of
  which adapter is producing events).
