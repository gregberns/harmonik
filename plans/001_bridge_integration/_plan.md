# Plan 001: bridge-integration

## Objective
Wire the claude-hook-bridge spec corpus (CHB-001..027) into the harmonik daemon work loop, with a tmux substrate hosting real `claude` CLI sessions, completion observed via the Stop hook through the daemon Unix socket, so dogfood smoke runs GREEN end-to-end against real claude.

## Status
mostly-done

The bridge-integration kerf work (jig=plan, status=ready in `spec.yaml`) has effectively landed: single-mode and review-loop smokes both reached GREEN in May 2026, the bridge-followup umbrella epic closed, and all four normative spec amendments (PL / WM / HC / CHB) are present in `specs/`. A handful of post-landing follow-ups remain open as standalone beads; they are not large enough to warrant a separate plan folder and are tracked here.

## What's done

- **Streams A (specs):** PL-021b/c + PL-028/028b, WM-002a, HC-054..057 all merged into `specs/process-lifecycle.md`, `specs/workspace-model.md`, `specs/handler-contract.md`. CHB-028 deliberately not filed (twin parity stays wire-level — Stream A4 decision record).
- **Stream B (tmux substrate):** `internal/lifecycle/tmux` package, `OSAdapter`, deterministic `WindowName`, window-level orphan sweep, `hk tmux-start` subcommand, `Substrate` seam in `internal/handler` — all landed. Substrate wired into daemon composition root via `hk-kqdpf.4`.
- **Stream C (daemon wiring):** `buildClaudeLaunchSpec`, single-mode + review-loop both use it, PreExecMessages emitted, heartbeat goroutine, `waitAgentReady` (HC-056), AdapterRegistry forwarded through `handler.NewHandler`. Dual-path `hookRelayEnabled` gate collapsed (`hk-kqdpf.1`).
- **Stream D (completion path):** `hookSessionStore.WaitForOutcome`, wired into `RunSocketListener`, `waitWithSocketGrace` with 3s post-Wait grace.
- **Stream E (smoke gates):**
  - SHA `f24ff5f` — `smoke(bridge-substrate): hk-kqdpf.5 — GREEN` (single-mode + substrate wired)
  - SHA `a8b6568` — `smoke(review-loop): hk-gql20 — GREEN take 2, bridge-integration epic closed`
  - SHA `f2e0350` — closed CHB-001..005 SUBSUMED
  - SHA `7c54c76` — CHB-022 daemon-twin-blind sensor
  - SHA `be91ba6` — CHB-024 settings-precedence sensor
  - SHA `8956ebc` — CHB-INV-002 sensor
  - SHA `79e7f19` — CHB-INV-001 sensor
- **Closed epic:** `hk-kqdpf` (bridge-followup umbrella) closed 2026-05-15 with all 10 children resolved or carved out.

## What's remaining

Five post-landing follow-ups, all standalone beads (not blocking the bridge thesis):

- `hk-44w19` (P2 bug) — SIGTERM to harmonik daemon doesn't propagate to child claude/tmux windows.
- `hk-pcgms` (P2) — Relay-failure scenario: daemon socket missing → `bridge_dial_failed` test.
- `hk-cw56j` (P2) — Implementer `--resume` correctness across daemon restart (CHB-023).
- `hk-s2vpx` (P2) — Twin emits identical wire-format sequence (CHB-021 verification).
- `hk-q7atz` (P2) — Daemon socket acceptor + CHB-023 durable-checkpoint write.

Carved-out children of the bridge-followup epic (separately tracked, not in this plan's scope):

- `hk-do7te` — `watcher.Done()` deadline (fix landed at `e19de6a`; standalone verification pending).
- `hk-4goy3` — Daemon merge-to-main leaves working tree out of sync with HEAD (benign warning in the GREEN smoke).

## References

- specs: `specs/claude-hook-bridge.md`, `specs/handler-contract.md`, `specs/process-lifecycle.md`, `specs/workspace-model.md`
- code: `internal/lifecycle/tmux/`, `internal/handler/`, `internal/daemon/` (`workloop.go`, `reviewloop.go`, `claudelaunchspec.go`, `socket.go`), `internal/hookrelay/`, `internal/workspace/claudesettings_wm040a.go`, `cmd/harmonik-twin-claude/`
- beads: epic `hk-kqdpf` (closed); follow-ups `hk-44w19`, `hk-pcgms`, `hk-cw56j`, `hk-s2vpx`, `hk-q7atz`; carve-outs `hk-do7te`, `hk-4goy3`
- key SHAs: `f24ff5f` (substrate smoke GREEN), `a8b6568` (review-loop smoke GREEN), `f2e0350` / `7c54c76` / `be91ba6` / `8956ebc` / `79e7f19` (CHB sensors)
- smoke artifacts: `docs/dogfood-smoke-run-2026-05-15-bridge-substrate.md`
- kerf source: `plans/001_bridge_integration/source/` (01-problem-space through 07-tasks plus spec.yaml)
- chat-context: bridge-integration was the first major end-to-end wiring after the May 8 MVH-foreground decision. It locked in the tmux-substrate seam, the substrate-aware handler factory, and the Stop-hook completion path that the rest of harmonik now depends on. Plan migrated 2026-05-18 from kerf bench to plans/.

## Next steps

- Triage the five P2 follow-ups (`hk-44w19`, `hk-pcgms`, `hk-cw56j`, `hk-s2vpx`, `hk-q7atz`) into the normal bead queue; none are bridge-thesis blockers.
- Verify `hk-do7te` fix at `e19de6a` with a dedicated test if not already covered.
- Decide whether `hk-4goy3` (merge-to-main working-tree drift) deserves a Phase-2 escalation; today it surfaces only as a benign warning.

## Open questions

None at the bridge-thesis level. The thesis is proven: daemon spawns real claude inside a deterministically-named tmux window, operator can attach, Stop hook drives completion, bead closes only on real work. Remaining open questions live inside the individual follow-up beads.
