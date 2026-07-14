// Package keepertest hosts the keeper vertical's L0–L3 replay test taxonomy
// (T10; RS-017/018/019, SK-020/SK-R8; measurement-design §3). It mirrors
// internal/codextest 1:1 and contains NO production code — every test lives in
// the external package keepertest_test.
//
// The four tiers (all zero-token except L3):
//
//   - L0 unit (l0_step_test.go): pure keeper.Step transition tables (the gate
//     ladder, the clean/abort/recovery/degraded paths) driven through
//     substrate.SyntheticSource + substrate.FakeEffector, plus seeded property
//     tests asserting SR3/SR4/SR6/SR7 as pure postconditions over the emitted
//     action order, plus the stimulus-codec golden round-trip.
//
//   - L1 contract (l1_contract_test.go): the 507-cycle recorded corpus under
//     testdata/keeper-cycles/baseline-2026-07-13. The recorded OUTPUT
//     envelopes are decoded via the internal/replay typed-decode harness
//     (strict mode + the SR checkers, D6); the synthesized INPUT schedules are
//     replayed FLAT (pre-scheduled TimerFired lines) through keepertwin.Twin →
//     the pure reactor → substrate.FakeEffector and compared against the
//     golden summary.json outcome + degraded flag per cycle. Flat replay is
//     sufficient here because the L1 goldens are BOUNDARY goldens (terminal
//     outcome, clear_unconfirmed flag, interior first-occurrence ORDER via the
//     SR checkers) — they never assert per-cycle interior attempt counts,
//     which a flat schedule cannot reproduce (measurement-design §2.2 note).
//
//   - L2 integration (l2_integration_test.go): the §2.2 DISCRETE-EVENT
//     harness — pre-scheduled TimerFired lines are STRIPPED, the harness arms
//     real virtual-time timers in response to the reactor's ArmTimer actions,
//     and fires them shell-faithfully (backstop consulted only at settle-window
//     ends). Effects land in a KeeperBridgeSink (a test-local recording fake of
//     the five ports). L2 asserts interior counts a live shell would produce
//     (e.g. the degraded path's defensive /clear re-injects), plus one fault
//     case per substrate fault mode asserting terminal-never-silence (RS-017;
//     the exhaustive matrix is T12).
//
//   - L3 live (l3_live_test.go): gated on KEEPER_LIVE=1 (RS-019). One real
//     tmux pane, one scripted handoff→clear→resume cycle, wire-canary
//     assertions only. SKIPPED by default.
//
// canary_test.go is the ungated corpus drift canary: manifest.json must equal
// the frozen 2026-07-13 anchors (507/427/79/347/1) before any replay runs
// (measurement-design §1.4), every corpus envelope must decode to a registered
// type, and every summary must classify into a known stratum.
//
// Makefile gate pair: `make test-keeper-l012` (L0+L1+L2 + canary, zero-token)
// and `make test-keeper-live` (L3, KEEPER_LIVE=1).
package keepertest
