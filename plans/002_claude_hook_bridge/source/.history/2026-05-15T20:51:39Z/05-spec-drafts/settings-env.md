# Spec-draft pointer — settings-env

The spec content from the settings/env research thread is consolidated into
`claude-hook-bridge.md` and the per-affected-spec amendments. This file is
a thin pointer.

- **Master draft:** `./claude-hook-bridge.md` §4.1 (settings.json
  materialization) and §4.2 (env-var schema)
- **Amendment files touched by this thread:** `workspace-model-amendment.md`,
  `execution-model-amendment.md`, `handler-contract-amendment.md`

Requirements that came out of the settings-env thread:

- **CHB-001** — `.claude/settings.json` path and ownership.
- **CHB-002** — Materialization ordering and atomic write.
- **CHB-004** — User-settings merge rule.
- **CHB-005** — Gitignore hygiene for `.claude/`.
- **CHB-006** — Required env-var schema (HARMONIK_* + CLAUDE_SESSION_ID).
- **CHB-007** — Forbidden Claude flags.
- **CHB-024** — Startup verification that bridge hooks are not shadowed by
  `settings.local.json`.
