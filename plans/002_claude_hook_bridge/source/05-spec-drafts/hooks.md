# Spec-draft pointer — hooks

The spec content from the hooks research thread is consolidated into
`claude-hook-bridge.md` (the master draft) and the per-affected-spec
amendment files in this directory. This file is a thin pointer.

- **Master draft:** `./claude-hook-bridge.md`
- **Amendment files touched by this thread:** `event-model-amendment.md`,
  `handler-contract-amendment.md`

Requirements that came out of the hooks thread:

- **CHB-003** — Required hook entries (which events the bridge wires).
- **CHB-010** — Relay subcommand surface (one entrypoint, kind-dispatched).
- **CHB-011** — Out-of-scope event kinds are no-op.
- **CHB-012** — Stdin payload schema per hook event.
- **CHB-013** — Hook → progress-message mapping table (the core translation).
- **CHB-014** — Reviewer verdict file read on Stop (reviewer phase).
