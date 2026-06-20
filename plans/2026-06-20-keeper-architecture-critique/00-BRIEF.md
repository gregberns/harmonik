# Keeper Architecture Critique — Shared Brief

**Date:** 2026-06-20
**Why this exists:** The operator has had *persistent, repeated* trouble with the harmonik **session-keeper**. The concern is that the keeper may be **architecturally flawed and/or too complicated** — not just buggy. We are deploying a fleet of independent critics/architects/QAs to evaluate this honestly and adversarially.

## The objective of the keeper (as understood)
The session-keeper is a per-orchestrator / per-crew **context-fill watcher**. It gauges a long-lived interactive Claude session's context usage and, when it fills, drives an intent-preserving **handoff → /clear → /session-resume** cycle BEFORE the pane overflows and stops accepting keystrokes. Two thresholds: **warn** and **act**. Standalone `harmonik keeper`. It manages captain, crew, flywheel, and orchestrator sessions.

## Code surface (durable anchors — read what's relevant to your angle)
- **Core package** `internal/keeper/` (non-test): `keeper.go`, `watcher.go`, `cycle.go`, `gauge.go`, `gates.go`, `heartbeat.go`, `injector.go`, `respawn.go`, `sessionid.go`, `thresholds.go`, `tmuxresolve.go`
- **Core tests** `internal/keeper/*_test.go` (~28 files — cycle, watcher, gauge, tmuxresolve, injector, statusline, heartbeat, precompact, gates, thresholds, sessionid, twin-e2e integration)
- **CLI layer** `cmd/harmonik/keeper_cmd.go` (569), `cmd/harmonik/keeper_enable_doctor_cmd.go` (**1163 — large, scrutinize**), `cmd/harmonik/goalkeeper_cmd.go` (221)
- **Events** `internal/core/keeperevents.go` (266)
- **Shell hooks** `scripts/hk-keeper.sh`, `scripts/keeper-precompact-hook.sh`, `scripts/keeper-sessionstart-hook.sh`, `scripts/keeper-stop-hook.sh`, `scripts/keeper-statusline.sh`
- **Specs / design** (TWO competing drafts — note the drift):
  - `.kerf/works/session-keeper/05-specs/session-keeper-spec.md` (original)
  - `.kerf/works/keeper-redesign/05-spec-drafts/keeper-identity-and-liveness.md` (redesign)
  - `docs/components/internal/keeper-precompact.md`, `docs/retro/2026-06-10/A6-session-keeper-enable.md`
- **Prior investigation** `plans/2026-06-20-keeper-investigation-recovery/README.md` (READ THIS — captures recent bugs)
- **Skill / operating contract** `.claude/skills/keeper/` (SKILL.md)

## Known failure signatures (from operator memory — treat as leads, verify against code/events)
- keeper `--session-id` goes DEAD after the 1st `/clear`; must re-resolve from gauge, not bind-and-delete.
- stale `.managed` (foreign_session) — auto-recovery after ~3 ticks + `keeper rebind`.
- gauge not wired for crews on the live deployment (`keeper doctor` shows drift).
- captain quits-and-stays-dead if launched without `--session-id`.
- keeper restart-now local-tmux-paste `/clear` works but is UNRELIABLE — TIMING, not executability.
- ad-hoc looping restart-now smoke leaked 1500+ tmux sessions (fork-bomb signature).
- operator HARD-NO on widening the warn/act band; the real fix is captain restart-now.

## What every agent must do
1. Read the brief + the artifacts relevant to YOUR angle (don't read everything — stay scoped).
2. Evaluate **critically and honestly** from your assigned lens. We want flaws found, not reassurance.
3. Write your report to `plans/2026-06-20-keeper-architecture-critique/<your-file>.md`.
4. Return a ≤200-word summary: top finding, severity, and your one-line verdict.

## The three core questions to keep answering
1. **Architectural issues** — is the design itself wrong/fragile?
2. **Complexity issues** — is it too complicated, and does that complexity cause the failures?
3. **Untestability → consistent failure** — is everything imperatively tied together such that it can't be tested, which is *why* it keeps breaking?
