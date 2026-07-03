# Change Design — workspace-model.md amendment

## Current state

`workspace-model.md` v0.4.2 §4.7 covers the harmonik session-log directory and metadata sidecar. §4.3.WM-013e covers `.gitignore` hygiene. No requirement covers `.claude/settings.json` materialization.

## Target state

Add 1-2 requirements under §4.7 (or a new §4.7a "Settings.json materialization") covering:

### WM-NEW-1 — Settings.json materialization at workspace creation

For workspaces that will host a `claude-code` agent type session, the workspace manager MUST materialize `${workspace_path}/.claude/settings.json` between WM-003 (worktree creation) and WM-016 (workspace_leased emission). The file's content is owned by `claude-hook-bridge.md §x` (cite forward). Atomic-write discipline matches WM-026: temp file + fsync + rename + fsync(parent_dir). The fsync of the parent directory MUST complete BEFORE the workspace_leased event emits.

Merge semantics: if `${workspace_path}/.claude/settings.json` exists in the worktree at materialization time (inherited from the cloned repo state), the workspace manager MUST attempt to MERGE harmonik's hook entries into the existing file's `hooks` map. Merge rule: for each event type harmonik declares hooks on, harmonik's matcher+hooks group is APPENDED to the existing array (if any). User-declared hooks for the same event continue to fire. If parsing the existing file fails (malformed JSON), the workspace manager MUST overwrite with harmonik's content AND emit `workspace_warning{reason="settings_file_overwritten"}` (already extant event class per workspace-model §X — confirm in spec-draft pass).

Tagging:
- Tags: mechanism
- Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### WM-NEW-2 — `.claude/settings.json` is gitignored at materialization

Extend §4.3.WM-013e's gitignore hygiene set to include `.claude/settings.json`. The materialization step adds this line to the worktree's `.gitignore` if not already present.

Rationale: harmonik's settings.json is per-run-worktree state. Committing it would pollute the task branch.

### Teardown disposition (clarification)

Add a one-sentence clarification to §4.7 (or §4.8) that `.claude/settings.json`, like the harmonik session-log directory, persists across the worktree's lifetime and is not specially purged on terminal-failure (per WM-031 retention) or on successful merge (per WM-030 retention, but not committed by way of gitignore).

## Rationale

The settings.json file is functionally the harmonik analogue of a session sidecar: per-workspace, harmonik-controlled, gitignored, atomic-write at workspace creation. Folding it into the WM-016 ordering gate ensures the handler can rely on the file existing before Claude exec.

## Requirements traceability

- Bridge-spec dependency: bridge cites these WM requirements as the materialization mechanism.
- Cross-spec: HC-NEW-1 (handler-side launch sequence) assumes settings.json exists pre-exec.

## Risk

If the user has hand-edited `.claude/settings.json` in their main checkout to add a security check (e.g., block `rm -rf`), that check WOULD fire in harmonik runs after merge. This is the desired behavior (user safety hooks still apply). But it COULD cause a harmonik run to fail in a way the operator doesn't anticipate. This is an operator-policy concern, not a spec concern; documented in the bridge spec's informative section.
