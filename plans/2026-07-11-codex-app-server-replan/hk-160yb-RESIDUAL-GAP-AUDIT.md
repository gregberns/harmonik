# hk-160yb — Persistent Supervised Sidecar: Residual-Gap Audit (yankee, 2026-07-18)

Read-only audit of the codex app-server client stack against the C2 requirements in
`PHASE-2-BUILD-PLAN.md §C2`. Purpose (mission Work item 2): confirm which of
resident-client / reconnect / backpressure / watchdog / supervision already exist vs.
need building, so captain can rank the real remaining work. **No closed beads reopened.**

## Two codex paths (load-bearing distinction)
- **App-server / JSON-RPC path** — `internal/codexdriver` (+ `codexinput`, `codexwire`). Resident
  client over `codex app-server`. **Opt-in only**, wired iff `HARMONIK_SUBSTRATE=codexdriver`
  (`cmd/harmonik/substrate_select.go:71,76`).
- **Single-turn `codex exec` path** — `internal/daemon/codexlaunchspec.go`. **Current production**
  path: `buildCodexLaunchSpec` builds `codex exec --json` — fresh process per turn
  (`codexlaunchspec.go:152,200-210`), `codex exec resume <thread_id>` for iter≥2 (`:186-199`).
- **Gotcha:** `internal/codexreactor` (holds the named "I1" invariant) is the OUTPUT reactor and is
  **NOT wired into the driver** — the driver's reactor is `internal/codexinput` (`session.go:460`).
  Plan text that says "mirror codexreactor I1" / "reuse codexwalguard" points at primitives that are
  not currently on the driver path.

## Verdict table
| # | Capability | Verdict | Core evidence |
|---|-----------|---------|---------------|
| 1 | Resident multi-turn client | **PARTIAL** | driver mechanism exists (`session.go:201-206,756-806`, held `threadID` `:131`); per-crew across-wakes orchestration + production wiring MISSING (prod is one-shot `codexlaunchspec.go:152`) |
| 2 | Reconnect / thread-resume | **MISSING** | no `thread/resume` in wire registry (`codexwire.go:100-141`); driver winds down terminally on wire close (`session.go:458-506`); no cursor/replay |
| 3 | Backpressure | **PARTIAL** | one-turn-in-flight EXISTS (`submitMu` `session.go:106-107,255`; `awaitReady` `:333-360`); bounded input queue MISSING |
| 4 | Watchdog / liveness | **PARTIAL** | output-or-stale liveness EXISTS (`codexinput/reactor.go:29-33,87-90`); `codexwalguard.go` exists but wired only to legacy exec path (`:49-73`); no sidecar watchdog |
| 5 | Supervision | **MISSING** | driver terminates on wire close (`session.go:473-475`); daemon-level `supervisorrevival_hkrnkuy.go` is observability-only, not the sidecar |

## Proposed build items (residual only — reusing Phase-1 machinery)
- **G1 (cap 1+5): per-crew resident-session owner.** A supervised owner holds ONE driver session open
  across wakes, drives repeated `SubmitInput`, revives on child death. Bulk of the work.
- **G2 (cap 2): `thread/resume` + replay-since-cursor.** Add `thread/resume` wire method
  (`codexwire.go`), driver respawn→initialize→resume branch, comms/queue last-seen cursor.
- **G3 (cap 3): bounded input queue** in front of the resident client (FIFO + cap while a turn is
  in flight). One-turn-in-flight gate already exists; this is the buffer only.
- **G4 (cap 4): sidecar watchdog + WAL-guard wiring.** Output-or-stale watchdog owning the session;
  wire `codexwalguard` (or equivalent) into the app-server launch path for ungraceful-kill recovery.
- **Prod cutover:** flip the codex crew path off the one-shot `codex exec` onto the resident sidecar.
  Gated behind hk-g0ror.4 E2E; likely its own task.

## Open questions for captain
1. Bead granularity: one hk-160yb umbrella task, or split G1–G4 as children? (G1 is the bulk; G2/G3/G4
   are separable.)
2. Reactor reuse: adopt `codexreactor` (I1/I2 + reconnect-state-reset shape, `reactor.go:194-214`) as
   the resident client's core, or extend `codexinput`? Affects G2/G3 heavily.
3. Prod cutover is behind the hk-g0ror.4 E2E (host-headroom gated). Sequence G1–G4 as design/impl now,
   cutover last?
