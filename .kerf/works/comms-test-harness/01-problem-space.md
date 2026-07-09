# 01 — Problem Space: Comms Bus Test-Validation Harness

## Summary
The `harmonik comms` inter-agent message bus (send/recv/log/join/leave/who, `--follow`/`--wake`) and the
`harmonik subscribe` run-event stream are both pure projections off the append-only event log
(`internal/eventbus`). Several real incidents (cursor-sharing drain-0, presence false-offline, `--wake`
pane-mismatch, subscribe-dies-on-restart, scheduled-send flag bug, no multi-consumer fan-out test) have hit
the fleet. This work hardens and closes gaps in the existing comms test corpus so these defect classes are
pinned by executable assertions instead of rediscovered live. Full technical analysis, source citations, and
the six-scenario acceptance corpus already exist in
`plans/2026-07-06-quality-system/12-comms-test-design.md` (authoritative; produced by admiral this session) —
this pass condenses it into kerf's problem-space shape rather than re-deriving it.

## Goals
- Close the 6 identified coverage gaps (G1 cursor/delivery, G2 presence TTL, G3 `--wake` pane resolution,
  G4 subscribe-lifecycle/restart, G5 scheduled-send — already fixed+tested, G6 multi-consumer fan-out).
- Pin two "correct-behavior-that-reads-as-breakage" semantics as executable specs, not code fixes:
  B1 (recv-drains-0 under an armed `--follow`, shared-cursor semantics) and B2 (idle `--follow` does not
  refresh presence, so a live-idle crew ages to Stale at 120s).
- Keep the 4 existing sentinel tests green as part of the epic's CI gate (no rebuild).

## Non-goals
- No Docker, scripted-twin, or digital-twin dependency — comms is pure eventbus-client behavior with zero
  environment dependency (confirmed against the Phase-2 shared-substrate plan).
- Not resolving B1/B2 by changing runtime behavior — the deliverable is spec + doc, operator-in-the-loop.
- Not touching the daemon-dispatch or keeper-test lanes (disjoint, parallel sibling lanes).

## Constraints
- Build in worktree isolation on branch `integration/comms-test` (already created for this epic); one
  human-gated PR to `main` at the epic boundary.
- Token/capacity: prefer codex harness for build work (Claude ~98% cap this session); avoid `pi` (blocked,
  hk-4ir08).
- T1/T2 (8 beads) are file-disjoint and must fan out concurrently; T3 (L2, socket/tmux) must run serial —
  process-kill and tmux tests race the same daemon socket.
- T4 (the two design questions) is operator-in-the-loop: surface, do not self-resolve.

## Success criteria
- All 6 acceptance-corpus scenarios (see design doc §3) pass as executable tests at their assigned layer
  (L0/L1/L2), each labeled EXISTS/EXTEND/NEW per the doc.
- B1 and B2 each have an executable spec assertion + an inline doc/skill note; both surfaced to the operator
  as design questions before any behavior change.
- The 4 pre-existing sentinel tests remain green in the epic's CI gate.
- Epic `hk-7m7o2` closes via one assessor-gated PR from `integration/comms-test` to `main`.

Confirmed against the design doc; no open questions for the operator at this pass (the design doc already
answers "what problem/who benefits/scope/constraints/success" in full — this is a condensation, not new
discovery).
