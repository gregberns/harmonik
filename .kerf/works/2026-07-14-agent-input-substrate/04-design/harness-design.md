# 04-Design — harness (C5: replay + fault matrix + L0–L3 + measurement/bake)

> Elaborates D8/D9 within `00-decisions.md`. Facts: `03-research/harness/findings.md`.
> Two-layer pattern (P1-proven): wire-level replay via `substrate.Twin` + durable-event
> invariants via `internal/replay` Checkers.

## 1. Tiers (`internal/claudetest`, RS-017..020 conformance)

| Tier | File | Exercises | Gate |
|---|---|---|---|
| L0 | `l0_wire_test.go` | claudewire Parse/Marshal: golden, round-trip, malformed table, unknown→Raw | zero-token, no IO |
| L1 | `l1_contract_test.go` | whole-corpus decode (zero Raw on curated corpus; zero unmodeled Extra); golden action-sequence: Twin → claudereactor → FakeEffector → DeepEqual | zero-token |
| L2 | `l2_integration_test.go` | corpus → Twin → reactor → fake shell sink (parked-Submit resolution, timers via FakeClock+BlockUntil); THE FAULT MATRIX (§3) | zero-token |
| L3 | `l3_live_test.go` | `CLAUDE_LIVE=1`: real claude child, minimal turn, wire canary; doubles as `make capture-fixtures-claude` recorder (codex `Makefile:123-134` precedent) | env-gated, budget-capped |
| canary | `canary_test.go` | corpus: valid JSON, parseable, zero Raw, ≥10 frames (codextest 4-gate shape `canary_hkoe86p_test.go:34-90`) | zero-token |

Makefile pair: `test-claude-l012` / `test-claude-live`; single env gate `CLAUDE_LIVE`
(RS-019). Minimum-artifact list per RS-018 (tiers + canary + corpus + CAPTURE-LOG + pair).

## 2. Corpus
- Provenance: T0' spike capture (driver-design §8) curates the first
  `testdata/claude-agent/corpus/raw-session-01.jsonl` (+resume session 02, stale-probe 03)
  via the promotion pipeline (capture-tee §4). Production captures extend it deliberately.
- `claudeCodec` (`internal/claudetwin`): `DecodeLine` = parse → filter to output frames →
  frameToEvent; skip non-reactor lines (emit=false); parse failure FATAL (RS-009 discipline);
  `ErrorEvent` = transport-error terminal; `DisconnectEvent` = `ProcExited{-1}`-class event
  (the vertical's connection-lost, RS-009's carve-out).
- M2's corpus asymmetry (harness findings §4): direct corpus replay (codex-L1-like), NOT
  keeper-style stimulus synthesis — the corpus records the channel M2 owns. Only fault
  schedules are synthetic.

## 3. Fault matrix (L2) — pinned to the eight observed incident classes

Every cell asserts **a terminal signal within a virtual-time bound + wall-clock backstop,
never silence** (RS-INV-003 / IN-INV-001). Strata = {fresh-seed, resume-brief} × modes:

| Injected | Real incident class (evidence) | Required terminal |
|---|---|---|
| FaultTruncate @ ack anchor | paste discarded (`05-io-boundaries.md:161`; pasteinject.go:1762) | transport-error event → Submit resolves stale/err |
| FaultDropAfter @ N=1 (init only) | claude never starts / empty pane (hk-5cox8, hk-kunm4) | DisconnectEvent → driver_exited terminal |
| FaultDropAfter @ post-submit, pre-anchor | pasted-never-submitted / Enter lost (hk-3qjwl, hk-76n5g) | ack_timeout → input_stale |
| FaultStall @ handshake | env wedge / [Y/n] prompt (hk-5s6re); no-window race (hk-2hb2y) | handshake_timeout terminal |
| FaultStall @ mid-turn | TUI stall / taskless wedge (hk-1kdd7); splash race (hk-8ys88) | ctx/backstop terminal, never hang |
| FaultDup @ ack anchor | double submit / blind Enter×3 (hk-8cq23) | exactly-one ResolveSubmit (IN-INV-002) |
| FaultNone | baseline | clean golden |
| (assertion, all cells) | silent no-ack → false-close/hang (hk-4ie1z, hk-trjef) | zero-silence: some terminal ALWAYS |

## 4. Durable-event invariant checkers (`internal/replay` reuse)
New Checkers over the three `agent_input_*` events (driver-design §6), keyed on
`(run_id, msg_id)`:
- **IN-INV-001** ack-or-stale: every `agent_input_submitted` reaches `acked` XOR `stale`
  (Finalizer flags unterminated submits — the SR9 shape, `checkers.go:170-181` template).
- **IN-INV-002** no-double-delivery: ≤1 `acked` per msg_id.
- **IN-INV-003** ordering: `submitted < acked` (EventID order after sort — `replay.go:163-171`).

## 5. Measurement + oracle + bake (D9 elaboration)
- **Baseline proxies (mined once, frozen):** jq counts over the frozen events.jsonl of
  `pasteinject_failed`, heartbeat-timeout kills, noChange kills — recorded in
  `testdata/claude-agent/baseline-proxies.json` with the extraction commands (out-of-band,
  re-runnable). Honest framing: proxies, not P1-style rate anchors (the old path emits no
  acks to measure).
- **Offline acceptance oracle (Stage A gate):** (1) N=10 consecutive green
  `make test-claude-l012` + driver integration; (2) fault matrix 100%; (3) grep gates: zero
  `time.Sleep`/`time.After` in claudedriver/claudereactor, zero `capture-pane` on the
  bead-run path, depguard green; (4) IN-INV replay checkers green over a driver-produced
  events fixture; (5) out-of-band jq/stat verification (harness never grades itself).
- **Stage B (post-DOGFOOD, gates physical deletion):** 20 consecutive live bead runs, zero
  input-class incidents; abort criteria per D9 (ack-wedge / false-ack / resume-hang /
  crash-loop ≥3 → config back to `tmux`, halt deletion).
- Coverage floor on claudereactor/claudedriver: measured & recorded at implementation
  (P1 D13 discipline; value not invented here).
