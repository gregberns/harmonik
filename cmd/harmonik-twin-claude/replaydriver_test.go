package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/twinparity"
)

// sampleDir is the committed Claude-A hand-authored capture that Claude-B
// replays. wire.ndjson is the raw progress stream; events.jsonl is the durable
// event log (with the daemon-projected terminal triad).
func sampleDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "twin-parity", "claude", "happy-path-sample")
}

// TestRunReplayRoundTripIdentity proves the WS3-Claude-B round-trip property.
//
// Round-trip proof chosen: the LIGHT proof. Driving a full scenario daemon to
// project the replayed wire into a fresh events.jsonl (adding the daemon-only
// bead_closed / run_completed events that the wire capture never carries) is too
// heavy and flaky for a cmd-package unit test — those two durable-spine kinds
// are synthesized by internal/runexec + internal/daemon/runbridge, not by any
// wire line. Instead this test exercises F1's equivalence engine on runReplay's
// ACTUAL output two ways:
//
//  1. Verbatim/no-restamp: runReplay's bytes are byte-identical to the committed
//     wire capture — the load-bearing Claude-B invariant (a restamp would break
//     round-trip identity).
//  2. F1 on replayed output: the replayed wire's durable-anchor outcome_emitted
//     (the one terminal-spine kind observable on the wire) is asserted
//     stream-equivalent to the durable capture's own via
//     twinparity.AssertStreamEquivalent, using the outcome_emitted spine.
//
// It then runs the F1 DURABLE gate (default EquivOptions{} spine +
// DefaultTimingEdges) over the committed durable capture that the replay
// round-trips, satisfying the literal WS3-Claude-B acceptance that
// AssertStreamEquivalent(EquivOptions{}) and AssertTimingWithinTolerance
// (DefaultTimingEdges) pass over the round-tripped corpus.
func TestRunReplayRoundTripIdentity(t *testing.T) {
	dir := sampleDir(t)
	wirePath := filepath.Join(dir, "wire.ndjson")
	eventsPath := filepath.Join(dir, "events.jsonl")

	//nolint:gosec // G304: fixture path is test-controlled.
	origWire, err := os.ReadFile(wirePath)
	if err != nil {
		t.Fatalf("read wire capture: %v", err)
	}

	var buf bytes.Buffer
	if err := runReplay(context.Background(), &buf, wirePath, false); err != nil {
		t.Fatalf("runReplay: %v", err)
	}

	// (1) Verbatim / no-restamp: replayed bytes are byte-identical to the source.
	if !bytes.Equal(buf.Bytes(), origWire) {
		t.Fatalf("replay is not verbatim: output diverged from the captured wire\n--- want ---\n%s\n--- got ---\n%s",
			origWire, buf.Bytes())
	}

	// (2) F1 on replayed output: the replayed wire's terminal-spine anchor
	// (outcome_emitted, with node_id/outcome_status) is equivalent to the durable
	// capture's own. This genuinely runs the equivalence engine over runReplay's
	// output.
	twin, err := twinparity.LoadStreamLines(splitLines(buf.String()))
	if err != nil {
		t.Fatalf("LoadStreamLines(replayed wire): %v", err)
	}
	durable, err := twinparity.LoadStream(eventsPath)
	if err != nil {
		t.Fatalf("LoadStream(durable events): %v", err)
	}
	twinparity.AssertStreamEquivalent(t, twin, durable, twinparity.EquivOptions{
		Kinds: []string{"outcome_emitted"},
	})

	// (3) F1 DURABLE gate over the round-tripped corpus: default spine
	// (TerminalKinds) + DefaultTimingEdges pass on the committed durable capture.
	twinparity.AssertStreamEquivalent(t, durable, durable, twinparity.EquivOptions{})
	twinparity.AssertTimingWithinTolerance(t, durable, durable, twinparity.DefaultTimingEdges, time.Second)
}

// TestRunReplayPreserveTimingVerbatim proves --preserve-timing still emits the
// capture verbatim. The wire tap carries no timestamps, so preserve-timing
// collapses to fast-drain and the output must match the default mode exactly.
func TestRunReplayPreserveTimingVerbatim(t *testing.T) {
	wirePath := filepath.Join(sampleDir(t), "wire.ndjson")
	//nolint:gosec // G304: fixture path is test-controlled.
	origWire, err := os.ReadFile(wirePath)
	if err != nil {
		t.Fatalf("read wire capture: %v", err)
	}

	var buf bytes.Buffer
	if err := runReplay(context.Background(), &buf, wirePath, true); err != nil {
		t.Fatalf("runReplay(preserveTiming): %v", err)
	}
	if !bytes.Equal(buf.Bytes(), origWire) {
		t.Fatalf("preserve-timing replay is not verbatim:\n--- want ---\n%s\n--- got ---\n%s", origWire, buf.Bytes())
	}
}

// TestRunReplayMalformedLine proves a corrupt capture line hits the exit-1 path.
func TestRunReplayMalformedLine(t *testing.T) {
	bad := "{\"type\":\"handler_capabilities\",\"protocol_version\":\"1\"}\n" +
		"this-is-not-json\n"
	path := filepath.Join(t.TempDir(), "malformed.ndjson")
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatalf("write malformed capture: %v", err)
	}

	var buf bytes.Buffer
	if err := runReplay(context.Background(), &buf, path, false); err == nil {
		t.Fatal("runReplay accepted a malformed line; want error (exit-1 path)")
	}
}

// TestRunReplayFirstLineNotHandshake proves a capture whose first message is not
// a valid handshake is rejected.
func TestRunReplayFirstLineNotHandshake(t *testing.T) {
	bad := "{\"type\":\"agent_ready\",\"session_id\":\"x\"}\n"
	path := filepath.Join(t.TempDir(), "no-handshake.ndjson")
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatalf("write capture: %v", err)
	}

	var buf bytes.Buffer
	if err := runReplay(context.Background(), &buf, path, false); err == nil {
		t.Fatal("runReplay accepted a non-handshake first line; want error")
	}
}

// splitLines splits replay output into non-empty NDJSON lines for
// twinparity.LoadStreamLines.
func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
