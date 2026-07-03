# session-keeper — Integration

How session-keeper plugs into the existing system, what's net-new, and the rollout order.

## Dependencies (existing, reused)
- **tmux send-keys delivery** — the mechanics behind `pasteinject.go` `WriteLastPane`/`SendEnterToLastPane`. The keeper injects externally; if shared code is extracted, coordinate with named-queues (pasteinject is their lane).
- **Event bus** — `EmitWithRunID` (eventbus.go:152); new constants in `internal/core/eventtype.go`.
- **tmux naming** — `harmonik-<project_hash>-<agent>` (`internal/lifecycle/provenance.go:106-116`) for target resolution.
- **Per-agent handoff files** — `HANDOFF-<agent>.md` already exist as a project convention.
- **comms presence** — `harmonik comms who` for liveness (no tmux target; keeper derives target from naming convention).
- **supervisor patterns** — `internal/supervise/supervisor.go` probe/backoff/crash-window logic, reused (not modified) by the standalone keeper.

## Net-new surface
- `cmd/harmonik/keeper/` — the `harmonik keeper` subcommand.
- `internal/keeper/` — watcher loop, threshold/idle/dispatch gating, handoff-cycle orchestration, anti-loop marker.
- `scripts/keeper-statusline.sh`, `scripts/keeper-stop-hook.sh`, `scripts/keeper-precompact-hook.sh`.
- New event constants (`session_keeper_*`).
- Keeper state dir `.harmonik/keeper/` (gitignored — escape-detector safe).

## Subsystem boundaries (depguard)
`internal/keeper/` is a new top-level subsystem → add a component-matrix entry in `.golangci.yml` and scaffold per `go-subsystem-add`. It may import `core`, `eventbus`, and a thin tmux/send-keys helper; it MUST NOT import `daemon`/`workloop` internals (orchestrator-keeping is independent of bead-dispatch).

## Rollout (Phase-1-first)
1. **Phase 1 — warn-only, dogfooded on flywheel.** Ship C1 (statusLine+gauge), C2 warn mode, C3 inject, C6 events. Operator adds the statusLine to `~/.claude/settings.json` and runs `harmonik keeper --agent flywheel`. Validate S1+S2 with zero destructive action. This is the safe, independently-shippable slice.
2. **Phase 2 — full cycle.** Add C4 (handoff-cycle + anti-loop), C5 (PreCompact backstop), Phase-2 idle-marker. Gate behind the empirical must-verify items (does in-place `/clear` preserve the injected `/session-resume`? does `/clear` mint a new session_id the statusLine sees?) before enabling reset mode by default.

## Operator setup (documented at finalize)
- Add keeper statusLine to each orchestrator's `~/.claude/settings.json`.
- (Phase 2) install the Stop + PreCompact hooks.
- Start one `harmonik keeper --agent <name>` per orchestrator. Open question (→ tasks): keepers started manually by the operator vs. supervised by a small keeper-supervisor (or the existing daemon-supervisor). Default for Phase 1: manual start, since it's non-destructive.

## Interaction with sibling works
- **hk-ymav1 (auto-tune --max-concurrent)** — sibling "supervise long-running agents" work; MAY later feed `warn_pct`/`act_pct`. No hard coupling now.
- **agent-comms (hk-uxm0j, landed)** — the keeper emits to the same bus; peers/operator observe keeper activity via `harmonik subscribe`.
- **flywheel kerf work (parent)** — session-keeper realizes the "managed context, no compaction" capability that work scoped.

## SPEC.md
Single-component work → the consolidated normative spec is `05-specs/session-keeper-spec.md`; `SPEC.md` is generated from it at finalize (after the adversarial review's fixes land + operator spec-text check-in).
