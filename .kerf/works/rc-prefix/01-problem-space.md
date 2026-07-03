# 01 — Problem Space: Per-project prefix for Remote-Control session labels

**Work:** rc-prefix
**Date:** 2026-06-20
**Source material:** `plans/2026-06-20-remote-control-session-prefix/00-PLAN.md` (full design + locked decisions). Gate spike completed (see §Constraints).

## Goal & motivation

Harmonik launches every crew/captain/watchdog as `claude --remote-control "<agent-name>"`
(`captain`, `paul`, `chani`, …). That name is the **display label in Claude Code's
Remote-Control registry** — the list in the iOS app / claude.ai session picker, which is
**global per host/account, not per-project**. When two harmonik projects run at once, both
register `captain`, `paul`, … and become indistinguishable in the picker.

**Who benefits:** the operator, who runs multiple harmonik projects concurrently and needs
to tell their sessions apart in the Claude Code session picker.

## Solution direction (confirmed)

Fold a per-project prefix into the explicit name: `--remote-control "hk-paul"`. This is the
*only* approach that works — Claude Code's `--remote-control-session-name-prefix` flag
applies **only to auto-generated names and is ignored when an explicit name is passed**
(verified against current Claude Code docs). Harmonik always passes an explicit name.

## In scope

- A single per-project **slug** stored in harmonik config (`.harmonik/config.yaml`).
- Prepending `<slug>-` to the `--remote-control` label at all four launch sites: daemon
  crew launch, captain CLI launcher, captain bash, watchdog bash.
- Default-deriving the slug like the beads prefix; `harmonik init` writes it.
- A read-side path so bash launchers can fetch the slug (mirrors `harmonik project-hash`).

## Out of scope

- tmux session names (already namespaced by a 12-char project hash — no true collision).
- Renaming existing beads or the beads `issue_prefix`.
- The `HARMONIK_AGENT` env var, crew-registry name, tmux name, `--session-id` — all stay
  **bare** (unchanged). Only the cosmetic Claude-side label changes.

## Constraints

- **Gate cleared (read-only spike, 2026-06-20):** nothing in harmonik reads the
  `--remote-control` label back as an identity key. Comms, crew registry/wake, keeper
  rebind, orphan sweep, crew status polling all key off `HARMONIK_AGENT` / crew-registry
  name / `--session-id` / tmux name. ⇒ Prefixing the label is isolated and safe.
- **Backward compatible:** empty/absent slug ⇒ exact current behavior (bare label).
- **Embedded-asset re-sync:** editing `captain-launch.sh` requires `cp` to
  `cmd/harmonik/captain-tools/…` or `TestCaptainLaunchShEmbedInSync` goes RED.
- **Resume parity:** `--resume` re-launch must reuse the *same* prefixed label as the
  original `--session-id` launch, or the picker shows a renamed session.

## Locked decisions (operator, 2026-06-20)

1. **One slug** drives identity; RC prefix defaults to it. **2-char default** (derived like
   the beads prefix), operator-overridable to longer — no hard length cap.
2. **Dash delimiter:** `hk-paul`.
3. **Existing projects:** implementation **asks the user** at migrate time (don't silently
   backfill); default suggestion = the project's current beads `issue_prefix`.

## Success criteria (concrete, verifiable)

1. With `remote_control_prefix: hk` (or slug `hk`) in `.harmonik/config.yaml`, a launched
   crew `paul` registers in Claude Code's picker as `hk-paul`; captain as `hk-captain`.
2. Two projects (`hk`, `mp`) run concurrently; the picker shows `hk-*` and `mp-*` with no
   name collisions.
3. With no slug configured, the launched label is exactly `paul` / `captain` (unchanged).
4. A keeper-driven clear→resume of crew `paul` comes back labeled `hk-paul` (resume parity),
   and crew wake / keeper rebind still function (identity unaffected).
5. `harmonik init` writes the slug, defaulted to the same value as `br init --prefix`.
6. `go test ./cmd/harmonik/` green (embed-sync intact).
