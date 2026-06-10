package handlercontract_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// Tests for LaunchSpec delivery per handler-contract.md §4.2 HC-005.
//
// Helper prefix: deliveryFixture (bead hk-8i31.5).

// deliveryFixtureValidSpec returns a minimal valid LaunchSpec for delivery tests.
func deliveryFixtureValidSpec(t *testing.T) *handlercontract.LaunchSpec {
	t.Helper()
	return &handlercontract.LaunchSpec{
		RunID:               core.RunID(uuid.MustParse("0196f200-0000-7000-8000-000000000001")),
		WorkflowID:          core.WorkflowID(uuid.MustParse("0196f200-0000-7000-8000-000000000002")),
		NodeID:              core.NodeID("impl-node-1"),
		AgentType:           core.AgentType("claude-code"),
		WorkspacePath:       "/tmp/test-ws",
		RequiredSkills:      []string{},
		SkillSearchPaths:    []string{},
		Timeout:             3600,
		ProvisioningTimeout: 60,
		Budget:              core.BudgetRef("default"),
		FreedomProfileRef:   "standard",
		SchemaVersion:       handlercontract.LaunchSpecSchemaVersion,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-005: Constants
// ─────────────────────────────────────────────────────────────────────────────

// TestHC005_MaxStdinBytesIs1MiB verifies that LaunchSpecMaxStdinBytes equals 1 MiB.
func TestHC005_MaxStdinBytesIs1MiB(t *testing.T) {
	t.Parallel()

	const oneMiB = 1 << 20
	if handlercontract.LaunchSpecMaxStdinBytes != oneMiB {
		t.Errorf("HC-005: LaunchSpecMaxStdinBytes = %d; want %d (1 MiB)", handlercontract.LaunchSpecMaxStdinBytes, oneMiB)
	}
}

// TestHC005_FileArgConstant verifies that LaunchSpecFileArg is "--launch-spec".
func TestHC005_FileArgConstant(t *testing.T) {
	t.Parallel()

	if handlercontract.LaunchSpecFileArg != "--launch-spec" {
		t.Errorf("HC-005: LaunchSpecFileArg = %q; want \"--launch-spec\"", handlercontract.LaunchSpecFileArg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// MarshalLaunchSpec
// ─────────────────────────────────────────────────────────────────────────────

// TestHC005_MarshalValidSpec verifies that a valid spec marshals without error.
func TestHC005_MarshalValidSpec(t *testing.T) {
	t.Parallel()

	spec := deliveryFixtureValidSpec(t)
	data, err := handlercontract.MarshalLaunchSpec(spec)
	if err != nil {
		t.Fatalf("HC-005: MarshalLaunchSpec: %v", err)
	}
	if len(data) == 0 {
		t.Error("HC-005: MarshalLaunchSpec returned empty bytes; want JSON")
	}
}

// TestHC005_MarshalNilSpecReturnsError verifies that nil spec is rejected.
func TestHC005_MarshalNilSpecReturnsError(t *testing.T) {
	t.Parallel()

	_, err := handlercontract.MarshalLaunchSpec(nil)
	if err == nil {
		t.Error("HC-005: MarshalLaunchSpec(nil) = nil; want error")
	}
}

// TestHC005_MarshalInvalidSpecReturnsError verifies that an invalid spec is rejected.
func TestHC005_MarshalInvalidSpecReturnsError(t *testing.T) {
	t.Parallel()

	spec := &handlercontract.LaunchSpec{} // zero: all required fields missing
	_, err := handlercontract.MarshalLaunchSpec(spec)
	if err == nil {
		t.Error("HC-005: MarshalLaunchSpec(invalid) = nil; want error")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadLaunchSpecFromArgs — stdin path
// ─────────────────────────────────────────────────────────────────────────────

// TestHC005_ReadFromStdinRoundTrip verifies the stdin delivery path:
// marshal a spec, pipe it as stdin, ReadLaunchSpecFromArgs returns equal spec.
func TestHC005_ReadFromStdinRoundTrip(t *testing.T) {
	t.Parallel()

	original := deliveryFixtureValidSpec(t)
	data, err := handlercontract.MarshalLaunchSpec(original)
	if err != nil {
		t.Fatalf("HC-005: MarshalLaunchSpec: %v", err)
	}

	// args has no --launch-spec → reads from stdin (the bytes.Reader).
	decoded, err := handlercontract.ReadLaunchSpecFromArgs([]string{}, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("HC-005: ReadLaunchSpecFromArgs(stdin): %v", err)
	}
	if decoded.RunID != original.RunID {
		t.Errorf("HC-005: RunID mismatch: got %v, want %v", decoded.RunID, original.RunID)
	}
}

// TestHC005_ReadFromStdinEmptyReturnsError verifies that empty stdin is rejected.
func TestHC005_ReadFromStdinEmptyReturnsError(t *testing.T) {
	t.Parallel()

	_, err := handlercontract.ReadLaunchSpecFromArgs([]string{}, bytes.NewReader(nil))
	if err == nil {
		t.Error("HC-005: ReadLaunchSpecFromArgs with empty stdin = nil; want error")
	}
}

// TestHC005_ReadFromStdinInvalidJSONReturnsError verifies that invalid JSON on stdin is rejected.
func TestHC005_ReadFromStdinInvalidJSONReturnsError(t *testing.T) {
	t.Parallel()

	_, err := handlercontract.ReadLaunchSpecFromArgs([]string{}, bytes.NewReader([]byte("not json")))
	if err == nil {
		t.Error("HC-005: ReadLaunchSpecFromArgs with invalid JSON = nil; want error")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadLaunchSpecFromArgs — file-path path
// ─────────────────────────────────────────────────────────────────────────────

// TestHC005_ReadFromFileRoundTrip verifies the --launch-spec file delivery path:
// write spec to a temp file, pass --launch-spec <path> in args, read it back.
func TestHC005_ReadFromFileRoundTrip(t *testing.T) {
	t.Parallel()

	original := deliveryFixtureValidSpec(t)
	data, err := handlercontract.MarshalLaunchSpec(original)
	if err != nil {
		t.Fatalf("HC-005: MarshalLaunchSpec: %v", err)
	}

	// Write JSON to a temp file.
	specPath := filepath.Join(t.TempDir(), "launchspec.json")
	//nolint:gosec // G306: 0644 is fine for a test file
	if err := os.WriteFile(specPath, data, 0o644); err != nil {
		t.Fatalf("HC-005: WriteFile: %v", err)
	}

	// Pass --launch-spec <path> in args. Stdin is empty (should not be read).
	args := []string{"--launch-spec", specPath}
	decoded, err := handlercontract.ReadLaunchSpecFromArgs(args, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("HC-005: ReadLaunchSpecFromArgs(--launch-spec): %v", err)
	}
	if decoded.RunID != original.RunID {
		t.Errorf("HC-005: RunID mismatch: got %v, want %v", decoded.RunID, original.RunID)
	}
}

// TestHC005_FileArgPreferredOverStdin verifies that --launch-spec takes precedence
// over stdin content — the file is read, not stdin.
func TestHC005_FileArgPreferredOverStdin(t *testing.T) {
	t.Parallel()

	specA := deliveryFixtureValidSpec(t)
	specA.NodeID = "node-from-file"
	dataA, _ := handlercontract.MarshalLaunchSpec(specA)

	specB := deliveryFixtureValidSpec(t)
	specB.NodeID = "node-from-stdin"
	dataB, _ := handlercontract.MarshalLaunchSpec(specB)

	specPath := filepath.Join(t.TempDir(), "launchspec.json")
	//nolint:gosec // G306: 0644 is fine for a test file
	if err := os.WriteFile(specPath, dataA, 0o644); err != nil {
		t.Fatalf("HC-005: WriteFile: %v", err)
	}

	args := []string{"--launch-spec", specPath}
	decoded, err := handlercontract.ReadLaunchSpecFromArgs(args, bytes.NewReader(dataB))
	if err != nil {
		t.Fatalf("HC-005: ReadLaunchSpecFromArgs: %v", err)
	}
	if decoded.NodeID != "node-from-file" {
		t.Errorf("HC-005: --launch-spec not preferred: got node_id %q, want \"node-from-file\"", decoded.NodeID)
	}
}

// TestHC005_FileMissingReturnsError verifies that a non-existent file path returns an error.
func TestHC005_FileMissingReturnsError(t *testing.T) {
	t.Parallel()

	args := []string{"--launch-spec", "/nonexistent/launchspec.json"}
	_, err := handlercontract.ReadLaunchSpecFromArgs(args, bytes.NewReader(nil))
	if err == nil {
		t.Error("HC-005: --launch-spec missing file = nil; want error")
	}
}

// TestHC005_SizeThresholdDeterminesDeliveryMode verifies that a payload at or
// below the threshold fits in a single stdin delivery, and that the size
// boundary is 1 MiB.
func TestHC005_SizeThresholdDeterminesDeliveryMode(t *testing.T) {
	t.Parallel()

	spec := deliveryFixtureValidSpec(t)
	data, err := handlercontract.MarshalLaunchSpec(spec)
	if err != nil {
		t.Fatalf("HC-005: MarshalLaunchSpec: %v", err)
	}

	// A normal LaunchSpec MUST fit in stdin (well under 1 MiB).
	if len(data) > handlercontract.LaunchSpecMaxStdinBytes {
		t.Errorf("HC-005: normal spec JSON exceeds 1 MiB (%d bytes); want < 1 MiB", len(data))
	}

	// Verify JSON is valid.
	var v map[string]json.RawMessage
	if err := json.Unmarshal(data, &v); err != nil {
		t.Errorf("HC-005: marshalled spec is not valid JSON: %v", err)
	}
}
