package daemon_test

// pi_twin_parser_drive_test.go — M6 WS3-pi (pi-B load-bearing proof).
//
// The pi twin (cmd/harmonik-twin-pi) is only useful as an oracle if its NDJSON
// output drives the REAL pi parser (internal/daemon/pijsonlparser.go) to exactly
// the same normalized result a live pi session would: a captured session id, a
// fired agent_end watcher, and accumulated token usage. This test proves that by
// running the twin binary and feeding its stdout through the real
// piSessionIDInterceptor + capturePiUsage path (via the exported test seams).
//
// It also pins the committed reference fixture
// (testdata/twin-parity/pi/happy-path-sample/ndjson) as byte-identical to the
// twin's live output, so the twin-parity gate's corpus can never silently drift
// from the twin.
//
// Bead: M6 WS3-pi.

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// runPiTwin executes `go run ./cmd/harmonik-twin-pi <args...>` from the module
// root and returns its stdout. Using `go run` keeps the proof honest — it
// exercises the actual twin binary, not an in-test re-implementation.
func runPiTwin(t *testing.T, args ...string) []byte {
	t.Helper()
	full := append([]string{"run", "github.com/gregberns/harmonik/cmd/harmonik-twin-pi"}, args...)
	cmd := exec.Command("go", full...) //nolint:gosec // G204: fixed module-internal package path + test-controlled args.
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run pi twin %v: %v\nstderr: %s", args, err, stderr.String())
	}
	return stdout.Bytes()
}

// TestPiTwinDrivesRealParser is the pi-B correctness proof: the twin's
// happy-path NDJSON drives the real pi parser to the expected session id,
// agent_end firing, and accumulated usage.
func TestPiTwinDrivesRealParser(t *testing.T) {
	out := runPiTwin(t, "--scenario", "happy-path")

	// (1) Drive the real interceptor: session-id capture (PI-012) + agent_end
	// watcher firing (PI-014). The interceptor passes bytes through unchanged, so
	// we read it to EOF exactly as the SpawnWatcher does.
	var gotSessionID string
	var sessionFires, agentEndFires int
	interceptor := daemon.ExportedNewPiSessionIDInterceptor(
		bytes.NewReader(out),
		func(id string) { gotSessionID = id; sessionFires++ },
		func() { agentEndFires++ },
	)
	drained, err := io.ReadAll(interceptor)
	if err != nil {
		t.Fatalf("drain interceptor: %v", err)
	}
	if !bytes.Equal(drained, out) {
		t.Errorf("interceptor mutated the byte stream; pass-through is required")
	}

	const wantSessionID = "00000000-0000-4000-8000-0000000000a1"
	if gotSessionID != wantSessionID {
		t.Errorf("captured session id = %q, want %q", gotSessionID, wantSessionID)
	}
	if sessionFires != 1 {
		t.Errorf("sessionIDCb fired %d times, want exactly 1", sessionFires)
	}
	if agentEndFires != 1 {
		t.Errorf("agentEndCb fired %d times, want exactly 1 (PI-014 terminal)", agentEndFires)
	}

	// (2) Drive the real usage accumulator line-by-line, exactly as the
	// session-data collector does. The twin's happy-path emits input=42 via
	// message_start and output=17 via message_end.
	var arts daemon.ExportedPiRunArtifacts
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		daemon.ExportedCapturePiUsage(&arts, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan twin output: %v", err)
	}
	if arts.TotalUsage.InputTokens != 42 {
		t.Errorf("accumulated InputTokens = %d, want 42", arts.TotalUsage.InputTokens)
	}
	if arts.TotalUsage.OutputTokens != 17 {
		t.Errorf("accumulated OutputTokens = %d, want 17", arts.TotalUsage.OutputTokens)
	}

	// (3) The committed reference fixture must be byte-identical to the twin's
	// live output — the parity gate's corpus is the twin, verbatim.
	fixture := filepath.Join("..", "..", "testdata", "twin-parity", "pi", "happy-path-sample", "ndjson")
	refBytes, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read committed reference ndjson: %v", err)
	}
	if !bytes.Equal(refBytes, out) {
		t.Errorf("committed reference ndjson has drifted from the twin's output;\n"+
			"regenerate: go run ./cmd/harmonik-twin-pi --scenario happy-path > %s", fixture)
	}
}
