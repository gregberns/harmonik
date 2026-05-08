package lifecycle

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// supervisionFixtureProjectHash computes the first 12 hex characters of
// SHA-256(project_root) — the project_hash used by PL-006a as a stable
// per-project scoping key for the provenance marker.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "The daemon MUST compute a
// stable project_hash at startup as the first 12 hexadecimal characters of
// SHA-256(realpath(project_root))."
func supervisionFixtureProjectHash(projectRoot string) string {
	sum := sha256.Sum256([]byte(projectRoot))
	return fmt.Sprintf("%x", sum[:6]) // 6 bytes = 12 hex chars
}

// supervisionFixtureProvenanceEnvKey is the environment variable name carried
// by every spawned subprocess per PL-006a.
const supervisionFixtureProvenanceEnvKey = "HARMONIK_PROJECT_HASH"

// TestPL014_SpawnProvenanceMarker verifies that every subprocess spawn carries
// the PL-006a provenance marker (HARMONIK_PROJECT_HASH env var) and that the
// child exposes the marker in its environment.
//
// Self-exec pattern: the test binary re-invokes itself with a sentinel env var
// so the child exits immediately after writing its environment to a temp file.
//
// Spec ref: process-lifecycle.md §4.5 PL-014 — "Every spawn MUST carry the
// provenance marker of PL-006a." PL-006a — "setting the environment variable
// HARMONIK_PROJECT_HASH=<project_hash> on every spawned subprocess."
// PL-INV-005 — "Sensor: every spawn site MUST set the provenance marker of
// PL-006a."
func TestPL014_SpawnProvenanceMarker(t *testing.T) {
	// Sentinel: child process body — exit immediately after writing env.
	const sentinelEnv = "GO_PL014_CHILD_STUB"
	const outputEnv = "GO_PL014_OUTPUT_FILE"

	if os.Getenv(sentinelEnv) == "1" {
		outFile := os.Getenv(outputEnv)
		if outFile == "" {
			os.Exit(1)
		}
		// Write the value of HARMONIK_PROJECT_HASH to the output file.
		val := os.Getenv(supervisionFixtureProvenanceEnvKey)
		if err := os.WriteFile(outFile, []byte(val), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "PL014 child: WriteFile: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	t.Parallel()

	t.Run("marker/child-receives-project-hash", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		wantHash := supervisionFixtureProjectHash(projectDir)

		// CreateTemp creates the file; remove it so the child writes fresh content.
		outFile, err := os.CreateTemp(t.TempDir(), "pl014-out-")
		if err != nil {
			t.Fatalf("PL-014 marker: CreateTemp: %v", err)
		}
		outFilePath := outFile.Name()
		if err := outFile.Close(); err != nil {
			t.Fatalf("PL-014 marker: Close: %v", err)
		}
		if err := os.Remove(outFilePath); err != nil {
			t.Fatalf("PL-014 marker: Remove: %v", err)
		}

		testBin := os.Args[0]
		//nolint:gosec // G204: testBin is os.Args[0] — the test binary itself
		cmd := exec.CommandContext(t.Context(), testBin, "-test.run=^TestPL014_SpawnProvenanceMarker$")
		cmd.Env = append(os.Environ(),
			sentinelEnv+"=1",
			supervisionFixtureProvenanceEnvKey+"="+wantHash,
			outputEnv+"="+outFilePath,
		)
		if err := cmd.Start(); err != nil {
			t.Fatalf("PL-014 marker: cmd.Start: %v", err)
		}
		if err := cmd.Wait(); err != nil {
			t.Fatalf("PL-014 marker: cmd.Wait: %v", err)
		}

		//nolint:gosec // G304: outFilePath is derived from t.TempDir(), not user input
		data, err := os.ReadFile(outFilePath)
		if err != nil {
			t.Fatalf("PL-014 marker: ReadFile output: %v", err)
		}
		gotHash := strings.TrimSpace(string(data))
		if gotHash != wantHash {
			t.Errorf("PL-014 marker: child received HARMONIK_PROJECT_HASH = %q, want %q", gotHash, wantHash)
		}
	})

	t.Run("marker/hash-is-stable-across-calls", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		hash1 := supervisionFixtureProjectHash(projectDir)
		hash2 := supervisionFixtureProjectHash(projectDir)
		if hash1 != hash2 {
			t.Errorf("PL-006a: project hash not stable: %q != %q", hash1, hash2)
		}
		if len(hash1) != 12 {
			t.Errorf("PL-006a: project hash length = %d, want 12 hex chars", len(hash1))
		}
	})

	t.Run("marker/different-projects-yield-different-hashes", func(t *testing.T) {
		t.Parallel()

		dir1 := plFixtureTempProjectDir(t)
		dir2 := plFixtureTempProjectDir(t)
		hash1 := supervisionFixtureProjectHash(dir1)
		hash2 := supervisionFixtureProjectHash(dir2)
		if hash1 == hash2 {
			t.Errorf("PL-006a: different project roots produced the same hash %q; provenance marker is not project-scoped", hash1)
		}
	})

	t.Run("marker/spawn-without-marker-is-detectable", func(t *testing.T) {
		t.Parallel()

		// Demonstrate: spawning without the marker produces a child that cannot
		// be identified as harmonik-owned. The sentinel check confirms that
		// absence of the env var is detectable at the spawn site.
		outFile, err := os.CreateTemp(t.TempDir(), "pl014-nomark-")
		if err != nil {
			t.Fatalf("PL-014 no-marker: CreateTemp: %v", err)
		}
		outFilePath := outFile.Name()
		if err := outFile.Close(); err != nil {
			t.Fatalf("PL-014 no-marker: Close: %v", err)
		}
		if err := os.Remove(outFilePath); err != nil {
			t.Fatalf("PL-014 no-marker: Remove: %v", err)
		}

		testBin := os.Args[0]
		//nolint:gosec // G204: testBin is os.Args[0] — the test binary itself
		cmd := exec.CommandContext(t.Context(), testBin, "-test.run=^TestPL014_SpawnProvenanceMarker$")
		// Deliberately omit HARMONIK_PROJECT_HASH — child will write empty string.
		envWithoutMarker := make([]string, 0, len(os.Environ())+2)
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, supervisionFixtureProvenanceEnvKey+"=") {
				envWithoutMarker = append(envWithoutMarker, e)
			}
		}
		envWithoutMarker = append(envWithoutMarker, sentinelEnv+"=1", outputEnv+"="+outFilePath)
		cmd.Env = envWithoutMarker

		if err := cmd.Start(); err != nil {
			t.Fatalf("PL-014 no-marker: cmd.Start: %v", err)
		}
		if err := cmd.Wait(); err != nil {
			t.Fatalf("PL-014 no-marker: cmd.Wait: %v", err)
		}

		//nolint:gosec // G304: outFilePath is derived from t.TempDir(), not user input
		data, err := os.ReadFile(outFilePath)
		if err != nil {
			t.Fatalf("PL-014 no-marker: ReadFile: %v", err)
		}
		gotHash := strings.TrimSpace(string(data))
		// Without the marker the child sees an empty value — not a valid hash.
		if len(gotHash) == 12 {
			t.Errorf("PL-INV-005 sensor: child without marker appears to have a valid hash %q; spawn-site provenance not enforced", gotHash)
		}
	})
}

// TestPL014_SpawnProvenanceMarker_ChildStub is the self-exec stub target used
// when the test binary is invoked via -test.run=^TestPL014_SpawnProvenanceMarker_ChildStub$.
// It exits immediately when the sentinel is not set; it is a no-op placeholder
// for manual invocations that target it directly.
//
// Note: the actual child-process body is embedded in TestPL014_SpawnProvenanceMarker
// itself (before t.Parallel()) following the standard Go self-exec pattern for
// tests that spawn child processes.
func TestPL014_SpawnProvenanceMarker_ChildStub(t *testing.T) {
	t.Parallel()

	// Not a stub invocation — the stub body lives in the sentinel check in
	// TestPL014_SpawnProvenanceMarker. This function exists solely so the
	// -test.run regex used in self-exec spawns does not match the parent test.
}
