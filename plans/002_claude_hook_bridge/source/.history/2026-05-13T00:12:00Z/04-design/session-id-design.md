# Design pointer — session-id

This file is a thin pointer. The session-id research thread's conclusions
were folded directly into the consolidated master design.

- **Source research:** `../03-research/session-id/findings.md` (Claude
  `--session-id` flag semantics, `--resume` behavior, UUID format, daemon
  pre-minting feasibility).

- **Where it landed in the master design** (`claude-hook-bridge-design.md`):
  - §D9 (`--session-id` and `--resume` usage — daemon mints UUIDv4 ahead of
    Claude exec; reviewer launches always mint fresh)
  - §D2 (env-var schema — `CLAUDE_SESSION_ID` propagation)
  - §D13 (daemon owns the mint; handler/relay propagate)
