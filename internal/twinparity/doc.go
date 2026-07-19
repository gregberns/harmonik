// Package twinparity provides a normalized-event equivalence library used to
// assert that the codex digital-twin and a real-agent capture produce the same
// wire/event-layer behavior.
//
// # Scope: the wire/event layer only
//
// Parity here is scoped to the JSONL/event layer — the sequence of typed events
// and their stable payload fields. It is deliberately NOT a claim of
// bit-for-bit behavioral identity between the twin and real claude. Two
// physical-delivery stages are explicitly OUT of the equivalence domain, per
// docs/twin-parity-audit-2026-05-14.md §5 ("Carve-outs — real-claude-only"):
//
//   - Carve-out 1 — pane-targeting (Fix 5, hk-yngq2): whether
//     `tmux send-keys -t %NNNN` lands in the correct pane is a tmux-topology
//     question that no NDJSON message can observe. The twin has no terminal, so
//     pane-ID stability cannot be exercised at the event layer.
//
//   - Carve-out 2 — splash-dismiss (Fix 8, hk-rf4ux): whether
//     `SendEnterToLastPane` clears the Claude Code welcome splash is a
//     terminal-render question. The twin has no terminal, so Enter-delivery
//     correctness cannot be exercised at the event layer.
//
// Both carve-outs are physical-delivery stages tracked as real-claude-only
// conformance work (hk-7uasg); they are outside what this package asserts. A
// PASS from AssertStreamEquivalent means the two streams agree on the event
// spine and stable payload fields — nothing about pane targeting or splash
// dismissal.
//
// The canonicalization logic (dual-field kind extraction, volatile-field
// dropping) is the exported, re-homed equivalent of the package-private helpers
// in test/scenario/harness_test.go (scenarioEventSequence projection).
package twinparity
