# Stall-Sentinel — Assembled Spec

> Formalized from the authoritative brief `plans/2026-07-02-stall-sentinel/DESIGN.md`.
> The brief is authoritative; this SPEC is the kerf-record assembly of it. Do not re-derive or
> re-open locked decisions.

## Objective (§0)

A **deterministic (no-LLM) sentinel** that periodically snapshots harmonik agent/run liveness,
computes "nothing is happening" per a small set of signatures, and **escalates on tiered thresholds**
— crew after X min, captain after Y min, operator after Z min. Detection + first-nudge are pure
Go/shell; a coding agent is only ever the last-resort responder, never the detector. Concrete
mechanism for the standing "fleet must not stall" mandate.

## Signal contract (§1) — load-bearing

Authoritative liveness = the typed event stream `.harmonik/events/events.jsonl` (run lifecycle,
`agent_heartbeat`/`agent_message`, `implementer_phase_complete`/`reviewer_verdict`,
`agent_presence`/`governor_signal`, `bead_closed` + `main` commits). tmux `capture-pane` /
worktree mtime are UNRELIABLE and used ONLY as a secondary confirm on an already-suspect run.

## Detection (§2) — two layers

**Layer A (per-run):** heartbeat-gap (> `run_silence_stall`), review-stall (`reviewer_verdict` fired,
no terminal within `review_finalize_stall`), run-age (> `run_max_age` backstop). Emits
`stall_detected`.

**Layer B (global lane no-forward-progress):** expectation-of-progress (non-empty queue OR
open-assigned bead OR mid-flight run) AND live crew, but zero forward-progress for
> `lane_noprogress_stall`. **False-positive guard is make-or-break:** a correctly-idle crew must NOT
trip it.

## Escalation (§3) — tiered, deterministic

Tier 1 (X min) → crew (comms send; pane keystroke via keeper `send-keys` if wedged). Tier 2 (Y min)
→ captain. Tier 3 (Z min) → operator-mailbox. Each firing emits a typed `stall_detected` event.

## Architecture / home (§4)

Daemon-integrated deterministic Go goroutine — the daemon holds the run registry, owns the event bus,
already emits `agent_ready_stall_detected`, and is always-on. watch/ops-monitor CONSUME
`stall_detected`; they do not detect. **Open (design-pass note):** daemon-integrated vs. standalone
Go sidecar — lean daemon-integrated.

## Config (§3/§5)

X/Y/Z + `run_silence_stall` / `review_finalize_stall` / `run_max_age` / `lane_noprogress_stall`,
all config-driven and fail-loud on missing required keys (mirror keeper/watch `Resolve*Config`).

## Acceptance (§5)

Replay the 3 real stall classes (deadlock → Layer B; silent hang → Layer A heartbeat-gap;
review-wedge → Layer A review-stall) + a NEGATIVE idle test (correctly-idle crew does NOT trip
Layer B).

## Relationship to existing (§6)

Generalizes `agent_ready_stall_detected`; reuses keeper `send-keys`; feeds the LLM watch tier;
`hk-thbbv` is the class-3 FIX (this is the DETECTION).
