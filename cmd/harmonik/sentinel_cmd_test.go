package main

// sentinel_cmd_test.go — CLI-layer tests for the `harmonik sentinel` verb block
// (sentinel_cmd.go). Covers the pure-logic surface: verb routing exit codes,
// per-verb flag/arg parsing (unknown-arg → 1, --help → 0, --reason required),
// and the emit → clear / emit → record-halt round trips against a temp project
// dir (the sentinel package writes ack files under .harmonik/decision_acks/, so
// no process spawn or daemon is involved). The governor-trip judgment logic
// itself lives in internal/sentinel and is tested there.
//
// SKIPPED (not reachable as pure logic in this layer): none — every branch of
// the three run* helpers is either an arg-parse decision or a filesystem write
// against a project dir, all deterministic. No t.Parallel(): the capture helpers
// mutate process globals.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sentinelFixtureDir returns a temp project dir with the .harmonik/ layout, so
// the sentinel filesystem writes have somewhere to land.
func sentinelFixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	return dir
}

// TestRunSentinelSubcommand_HelpAndEmptyReturnZero verifies the help/empty verb
// inputs print usage and return 0.
func TestRunSentinelSubcommand_HelpAndEmptyReturnZero(t *testing.T) {
	for _, args := range [][]string{{}, {"--help"}, {"-h"}} {
		var code int
		out := captureStdoutDuring(t, func() { code = runSentinelSubcommand(args) })
		if code != 0 {
			t.Fatalf("args=%v: exit = %d, want 0", args, code)
		}
		if !strings.Contains(out, "harmonik sentinel — flywheel sentinel surface") {
			t.Fatalf("args=%v: usage banner missing:\n%s", args, out)
		}
	}
}

// TestRunSentinelSubcommand_UnknownVerbReturnsTwo verifies an unknown verb
// returns exit 2.
func TestRunSentinelSubcommand_UnknownVerbReturnsTwo(t *testing.T) {
	vgSilenceStd(t)
	if code := runSentinelSubcommand([]string{"bogus"}); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

// --- emit-trip -------------------------------------------------------------

// TestRunSentinelEmitTrip_HelpReturnsZero verifies --help short-circuits to 0.
func TestRunSentinelEmitTrip_HelpReturnsZero(t *testing.T) {
	var code int
	out := captureStdoutDuring(t, func() { code = runSentinelEmitTrip([]string{"--help"}) })
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "emit-trip") {
		t.Fatalf("emit-trip usage missing:\n%s", out)
	}
}

// TestRunSentinelEmitTrip_UnknownArgReturnsOne verifies an unrecognised
// argument returns exit 1.
func TestRunSentinelEmitTrip_UnknownArgReturnsOne(t *testing.T) {
	vgSilenceStd(t)
	if code := runSentinelEmitTrip([]string{"--nope"}); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

// TestRunSentinelEmitTrip_WritesTokenAndIsIdempotent drives the full CLI path:
// the first emit writes an ack file and prints a non-empty token; a second emit
// with the same project returns the SAME token (idempotent) without printing a
// new one. Exercises --bead comma-splitting and --undeployed-tail parsing too.
func TestRunSentinelEmitTrip_WritesTokenAndIsIdempotent(t *testing.T) {
	dir := sentinelFixtureDir(t)

	var code int
	first := captureStdoutDuring(t, func() {
		code = runSentinelEmitTrip([]string{"--project", dir, "--bead", "hk-a,hk-b", "--undeployed-tail"})
	})
	if code != 0 {
		t.Fatalf("first emit exit = %d, want 0", code)
	}
	tok := strings.TrimSpace(first)
	if tok == "" {
		t.Fatal("first emit printed no ack token")
	}
	// The ack file must exist under decision_acks/.
	ackPath := filepath.Join(dir, ".harmonik", "decision_acks", tok)
	if _, err := os.Stat(ackPath); err != nil {
		t.Fatalf("ack file %s not written: %v", ackPath, err)
	}

	second := captureStdoutDuring(t, func() {
		code = runSentinelEmitTrip([]string{"--project=" + dir})
	})
	if code != 0 {
		t.Fatalf("second emit exit = %d, want 0", code)
	}
	if got := strings.TrimSpace(second); got != tok {
		t.Fatalf("idempotency broken: second token = %q, want %q", got, tok)
	}
}

// --- clear-trip ------------------------------------------------------------

// TestRunSentinelClearTrip_HelpReturnsZero verifies --help returns 0.
func TestRunSentinelClearTrip_HelpReturnsZero(t *testing.T) {
	var code int
	captureStdoutDuring(t, func() { code = runSentinelClearTrip([]string{"-h"}) })
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}

// TestRunSentinelClearTrip_UnknownArgReturnsOne verifies bad args return 1.
func TestRunSentinelClearTrip_UnknownArgReturnsOne(t *testing.T) {
	vgSilenceStd(t)
	if code := runSentinelClearTrip([]string{"--bogus"}); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

// TestRunSentinelClearTrip_NoPendingIsNoOp verifies clearing with no pending
// trip returns 0 and prints nothing (idempotent).
func TestRunSentinelClearTrip_NoPendingIsNoOp(t *testing.T) {
	dir := sentinelFixtureDir(t)
	var code int
	out := captureStdoutDuring(t, func() { code = runSentinelClearTrip([]string{"--project", dir}) })
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("expected no output for no-pending clear, got %q", out)
	}
}

// TestRunSentinelClearTrip_ClearsPendingTrip verifies emit → clear prints the
// same token, and a second clear is a silent no-op.
func TestRunSentinelClearTrip_ClearsPendingTrip(t *testing.T) {
	dir := sentinelFixtureDir(t)

	var code int
	emit := captureStdoutDuring(t, func() { code = runSentinelEmitTrip([]string{"--project", dir}) })
	if code != 0 {
		t.Fatalf("emit exit = %d, want 0", code)
	}
	tok := strings.TrimSpace(emit)

	cleared := captureStdoutDuring(t, func() { code = runSentinelClearTrip([]string{"--project", dir}) })
	if code != 0 {
		t.Fatalf("clear exit = %d, want 0", code)
	}
	if got := strings.TrimSpace(cleared); got != tok {
		t.Fatalf("cleared token = %q, want %q", got, tok)
	}

	again := captureStdoutDuring(t, func() { code = runSentinelClearTrip([]string{"--project", dir}) })
	if code != 0 || strings.TrimSpace(again) != "" {
		t.Fatalf("second clear: exit=%d out=%q, want 0 and empty", code, again)
	}
}

// --- record-halt -----------------------------------------------------------

// TestRunSentinelRecordHalt_HelpReturnsZero verifies --help returns 0.
func TestRunSentinelRecordHalt_HelpReturnsZero(t *testing.T) {
	var code int
	captureStdoutDuring(t, func() { code = runSentinelRecordHalt([]string{"--help"}) })
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}

// TestRunSentinelRecordHalt_UnknownArgReturnsOne verifies bad args return 1.
func TestRunSentinelRecordHalt_UnknownArgReturnsOne(t *testing.T) {
	vgSilenceStd(t)
	if code := runSentinelRecordHalt([]string{"--wat"}); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

// TestRunSentinelRecordHalt_EmptyReasonReturnsOne verifies the required
// non-empty --reason guard: a missing reason and a whitespace-only reason both
// return exit 1 before touching the filesystem.
func TestRunSentinelRecordHalt_EmptyReasonReturnsOne(t *testing.T) {
	vgSilenceStd(t)
	dir := sentinelFixtureDir(t)
	cases := [][]string{
		{"--project", dir},                    // no --reason at all
		{"--project", dir, "--reason", "   "}, // whitespace-only
		{"--project", dir, "--reason="},       // empty via =form
	}
	for _, args := range cases {
		if code := runSentinelRecordHalt(args); code != 1 {
			t.Fatalf("args=%v: exit = %d, want 1", args, code)
		}
	}
}

// TestRunSentinelRecordHalt_RecordsHaltAndClears verifies emit → record-halt
// with a valid reason clears the pending trip and prints its token.
func TestRunSentinelRecordHalt_RecordsHaltAndClears(t *testing.T) {
	dir := sentinelFixtureDir(t)

	var code int
	emit := captureStdoutDuring(t, func() { code = runSentinelEmitTrip([]string{"--project", dir}) })
	if code != 0 {
		t.Fatalf("emit exit = %d, want 0", code)
	}
	tok := strings.TrimSpace(emit)

	halted := captureStdoutDuring(t, func() {
		code = runSentinelRecordHalt([]string{"--project", dir, "--reason", "infra: box unreachable"})
	})
	if code != 0 {
		t.Fatalf("record-halt exit = %d, want 0", code)
	}
	if got := strings.TrimSpace(halted); got != tok {
		t.Fatalf("record-halt token = %q, want %q", got, tok)
	}
}
