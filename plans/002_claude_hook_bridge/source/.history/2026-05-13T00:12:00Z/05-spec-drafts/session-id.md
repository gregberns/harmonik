# Spec-draft pointer — session-id

The spec content from the session-id research thread is consolidated into
`claude-hook-bridge.md` and the per-affected-spec amendments. This file is
a thin pointer.

- **Master draft:** `./claude-hook-bridge.md` §4.3 (pre-generated
  `claude_session_id` flow)
- **Amendment files touched by this thread:** `handler-contract-amendment.md`,
  `process-lifecycle-amendment.md`

Requirements that came out of the session-id thread:

- **CHB-008** — Session ID minting and propagation (daemon mints UUIDv4
  before Claude exec; flows via `CLAUDE_SESSION_ID` env var).
- **CHB-009** — Reviewer launches always mint fresh session IDs.
- **CHB-023** — Daemon-side `claude_session_id` durability before Claude
  exec (persist before launch so reconnect after crash is sound).
