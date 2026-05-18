# Amendment to specs/workspace-model.md (v0.4.2 → v0.4.3)

## Frontmatter

- `version: 0.4.2` → `version: 0.4.3`
- `last-updated: 2026-04-25` → `last-updated: 2026-05-12`

## New requirements

### Add new sub-section §4.7a (after §4.7, before §4.8):

### 4.7a Claude-code settings.json materialization

#### WM-038 — `.claude/settings.json` materialization for claude-code workspaces

For every workspace that will host a `claude-code` agent session (determined by [execution-model.md §4.3] node `agent_type`), the workspace manager MUST materialize a file at `${workspace_path}/.claude/settings.json` between WM-003 (worktree creation) and WM-016 (workspace_leased emission). The file's content is owned by [claude-hook-bridge.md §4.1].

The write MUST follow the atomic-write discipline of WM-026: temp file + fsync + rename + fsync(parent_dir). The parent-directory fsync MUST complete BEFORE `workspace_leased` emits.

If a `.claude/settings.json` file already exists in the worktree at materialization time (inherited from the cloned repo's state), the workspace manager MUST attempt a merge per [claude-hook-bridge.md §4.1 CHB-004]: the bridge-required hook entries are APPENDED to the existing event-type arrays. On malformed-JSON or merge-incompatible existing content, the workspace manager MUST OVERWRITE and emit `workspace_warning{reason="settings_file_overwritten"}` (event-payload schema additive; absent at v0.3.x readers per §6.4).

For workspaces that will NOT host a claude-code agent session, this requirement is a no-op.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### Amendment to §4.3 WM-013e (extend gitignore set)

Append `.claude/settings.json` to the WM-013e gitignore hygiene set. The materialization step (WM-038) MUST add this line to the worktree's `.gitignore` if not already present, in the same atomic-write transaction as the settings.json write.

### Retention disposition (informative addition to §4.7)

Add a one-paragraph note at the end of §4.7 (immediately before §4.8):

> INFORMATIVE: `.claude/settings.json` materialized per §4.7a follows the same lifetime as the session-log directory: it persists across the worktree's lifetime, is preserved on successful merge per WM-030 (but not committed by way of WM-013e gitignore hygiene), and persists on terminal failure per WM-031.

## Revision-history entry

| 2026-05-12 | 0.4.3 | foundation-author | Add §4.7a Claude-code settings.json materialization (WM-038) covering atomic-write discipline, merge-with-existing semantics, and gitignore-hygiene extension. Companion to [claude-hook-bridge.md] new spec. No prior requirement IDs renumbered. Status remains `reviewed`. |
