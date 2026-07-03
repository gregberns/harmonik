# Research — Settings.json materialization and env-var inheritance

## Source
- https://code.claude.com/docs/en/hooks
- https://code.claude.com/docs/en/cli-reference
- Local: specs/workspace-model.md §4.7, §4.3.WM-013e

## Settings.json file precedence

Settings hierarchy (highest precedence first):
1. `--settings <file-or-json>` flag (overrides all files for the session)
2. `.claude/settings.local.json` (project-local, gitignored)
3. `.claude/settings.json` (project-level, checked into project)
4. `~/.claude/settings.json` (user-level)
5. Managed/policy settings (admin-controlled)

For harmonik, the canonical placement is `${workspace_path}/.claude/settings.json` (project-level for that workspace).

CAVEAT: a project-level `.claude/settings.json` checked into a user's repo might pre-exist in the cloned repo. Two reads of this:
- (a) Treat the workspace's `.claude/settings.json` as harmonik-owned: overwrite at materialization, restore on teardown if applicable.
- (b) Merge harmonik's hooks into a pre-existing `.claude/settings.json` (harder, fragile).

Decision (for design pass): **(a) overwrite.** The workspace is a per-run worktree, not the user's main checkout. The pre-existing settings file is the user's repo state; harmonik's worktree gets a fresh settings file derived from a harmonik template. The `.claude/settings.json` is in the WM-013e gitignore set so it never gets committed.

## Env-var inheritance through Claude

Claude Code is spawned by harmonik (the handler subprocess). Standard POSIX env inheritance:
- harmonik daemon → handler subprocess (via os.exec) inherits daemon env
- handler subprocess sets HARMONIK_* and HARMONIK_SECRET_* + Claude-Code-specific env (e.g. CLAUDE_CODE_*) → exec claude → claude inherits
- claude → spawns hook command (harmonik hook-relay) → relay inherits everything

So static workspace context flows via env-var inheritance without needing to round-trip through the settings.json file.

Reserved env-var namespaces (existing):
- `HARMONIK_*` — harmonik-owned context (HC-028 declares HARMONIK_SECRET_* specifically).
- `CLAUDE_CODE_*` — Claude Code-owned (e.g., CLAUDE_CODE_SKIP_PROMPT_HISTORY).
- `CLAUDE_*` — Claude Code path placeholders (CLAUDE_PROJECT_DIR, CLAUDE_PLUGIN_ROOT, CLAUDE_PLUGIN_DATA).

Proposed harmonik env-var schema for the bridge:
- `HARMONIK_RUN_ID` (UUIDv7) — required.
- `HARMONIK_DAEMON_SOCKET` (path) — required. Defaults to `${HARMONIK_WORKSPACE_PATH}/../../../.harmonik/daemon.sock` in MVH; daemon-explicit setting overrides.
- `HARMONIK_WORKSPACE_PATH` (path) — required.
- `HARMONIK_HANDLER_SESSION_ID` (UUID) — required. Distinct from claude_session_id.
- `HARMONIK_CLAUDE_SESSION_ID` (UUID) — required. Set to the same value passed in --session-id (or --resume target).
- `HARMONIK_WORKFLOW_MODE` (enum) — optional; only set when non-default mode (review-loop, dot).
- `HARMONIK_PHASE` (enum) — optional; set when in a multi-phase mode. Values: implementer-initial, implementer-resume, reviewer, single.
- `HARMONIK_ITERATION_COUNT` (int) — optional; set when in iterative mode. 1..3.
- `HARMONIK_BEAD_ID` (string) — optional; set when run is bead-tied.
- (already specced) `HARMONIK_SECRET_*` (HC-028).

## Atomic write discipline

Per WM-026 (sidecar) and WM-013a (lease-lock): temp-file + fsync + rename + fsync(parent_dir).
Apply the same discipline to `.claude/settings.json` at materialization. The write MUST complete before any subprocess is launched against the workspace (consistent with WM-016 ordering: sidecar + lease-lock before workspace_leased; settings.json belongs in the same gate).

## .gitignore hygiene

`.claude/settings.json` MUST be added to WM-013e's gitignore set (alongside `.harmonik/`):
- Without this, a checkpoint commit would capture the auto-generated settings file.
- The user's main checkout's `.claude/settings.json` (if any) is unaffected — that's in the user's working tree, not the per-run worktree.

Investigation needed at impl time: does `.claude/` need a wildcard `.claude/settings.json` rule, or does the existing `.claude/` rule in some projects cover it? Either way the harmonik-controlled gitignore needs explicit `.claude/settings.json` entry to be self-contained.

## Teardown disposition

`.claude/settings.json` follows the same lifecycle as `.harmonik/sessions/` (WM-030):
- On successful merge: preserved in the worktree (but NOT committed via gitignore).
- On terminal failure: persists per WM-031.
- No explicit cleanup required by the workspace manager beyond worktree-level teardown.
