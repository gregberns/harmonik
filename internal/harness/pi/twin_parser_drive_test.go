package pi_test

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/harness/pi"
)

func runPiTwin(t *testing.T, args ...string) []byte {
	t.Helper()
	full := append([]string{"run", "github.com/gregberns/harmonik/cmd/harmonik-twin-pi"}, args...)
	cmd := exec.CommandContext(t.Context(), "go", full...) //nolint:gosec // G204: fixed module-internal package path + test-controlled args.
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

	var gotSessionID string
	var sessionFires, agentEndFires int
	interceptor := pi.ExportedNewPiSessionIDInterceptor(
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

	var arts pi.ExportedPiRunArtifacts
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		pi.ExportedCapturePiUsage(&arts, line)
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

	fixture := filepath.Join("..", "..", "..", "testdata", "twin-parity", "pi", "happy-path-sample", "ndjson")
	refBytes, err := os.ReadFile(fixture) //nolint:gosec // G304: fixture is a fixed in-repo testdata path, not user input
	if err != nil {
		t.Fatalf("read committed reference ndjson: %v", err)
	}
	if !bytes.Equal(refBytes, out) {
		t.Errorf("committed reference ndjson has drifted from the twin's output;\n"+
			"regenerate: go run ./cmd/harmonik-twin-pi --scenario happy-path > %s", fixture)
	}
}
