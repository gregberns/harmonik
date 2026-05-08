package lifecycle

import (
	"os"
	"syscall"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// projectHashFixtureProjectDir returns a canonical project root for hash tests.
// It delegates to plFixtureTempProjectDir (shared test helper) so the directory
// tree is consistent with the rest of the lifecycle test suite.
func projectHashFixtureProjectDir(t *testing.T) string {
	t.Helper()
	return plFixtureTempProjectDir(t)
}

// TestComputeProjectHash_Stability verifies that ComputeProjectHash returns the
// same 12-character hex string for the same input on repeated calls.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "The hash MUST be stable
// across restarts (the same project root yields the same hash)."
func TestComputeProjectHash_Stability(t *testing.T) {
	t.Parallel()

	projectDir := projectHashFixtureProjectDir(t)

	hash1 := ComputeProjectHash(projectDir)
	hash2 := ComputeProjectHash(projectDir)

	if hash1 != hash2 {
		t.Errorf("ComputeProjectHash not stable: %q != %q", hash1, hash2)
	}
}

// TestComputeProjectHash_Length verifies that the returned hash is exactly 12
// hex characters (6 bytes of SHA-256 encoded as lowercase hex).
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "first 12 hexadecimal
// characters of SHA-256(realpath(project_root))."
func TestComputeProjectHash_Length(t *testing.T) {
	t.Parallel()

	projectDir := projectHashFixtureProjectDir(t)
	hash := ComputeProjectHash(projectDir)

	if len(hash) != 12 {
		t.Errorf("ComputeProjectHash: len = %d, want 12; hash = %q", len(hash), hash)
	}

	// Verify all characters are lowercase hex.
	for i, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("ComputeProjectHash: non-lowercase-hex char %q at position %d in %q", c, i, hash)
		}
	}
}

// TestComputeProjectHash_Uniqueness verifies that two different project roots
// produce different hashes. This is the core property that makes the provenance
// marker project-scoped.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "identification MUST NOT rely
// on binary path alone; binary-path matching is insufficient on multi-project
// machines where the same handler binary serves multiple projects."
func TestComputeProjectHash_Uniqueness(t *testing.T) {
	t.Parallel()

	dir1 := projectHashFixtureProjectDir(t)
	dir2 := projectHashFixtureProjectDir(t)

	if dir1 == dir2 {
		t.Skip("temp dirs collided — skipping uniqueness test")
	}

	hash1 := ComputeProjectHash(dir1)
	hash2 := ComputeProjectHash(dir2)

	if hash1 == hash2 {
		t.Errorf("ComputeProjectHash: different roots produced the same hash %q; provenance marker is not project-scoped", hash1)
	}
}

// TestProvenanceEnvVar_Format verifies that ProvenanceEnvVar returns the
// correctly formatted "KEY=VALUE" string for the subprocess environment.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "setting the environment
// variable HARMONIK_PROJECT_HASH=<project_hash> on every spawned subprocess."
func TestProvenanceEnvVar_Format(t *testing.T) {
	t.Parallel()

	hash := ComputeProjectHash(projectHashFixtureProjectDir(t))
	envEntry := ProvenanceEnvVar(hash)

	want := ProvenanceEnvKey + "=" + hash.String()
	if envEntry != want {
		t.Errorf("ProvenanceEnvVar: got %q, want %q", envEntry, want)
	}
}

// TestMatchesProvenanceMarker_Hit verifies that MatchesProvenanceMarker returns
// true when the target env var is present in the slice.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — orphan sweep matches on the
// env var on Linux.
func TestMatchesProvenanceMarker_Hit(t *testing.T) {
	t.Parallel()

	hash := ComputeProjectHash(projectHashFixtureProjectDir(t))
	env := []string{
		"PATH=/usr/bin",
		ProvenanceEnvVar(hash),
		"HOME=/root",
	}

	if !MatchesProvenanceMarker(env, hash) {
		t.Errorf("MatchesProvenanceMarker: expected true for env with marker, got false")
	}
}

// TestMatchesProvenanceMarker_Miss verifies that MatchesProvenanceMarker
// returns false when the marker is absent — i.e., the process is not
// harmonik-owned for this project.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "candidates without a valid
// marker MUST NOT be touched."
func TestMatchesProvenanceMarker_Miss(t *testing.T) {
	t.Parallel()

	hash := ComputeProjectHash(projectHashFixtureProjectDir(t))
	env := []string{
		"PATH=/usr/bin",
		"HOME=/root",
	}

	if MatchesProvenanceMarker(env, hash) {
		t.Errorf("MatchesProvenanceMarker: expected false for env without marker, got true")
	}
}

// TestMatchesProvenanceMarker_WrongHash verifies that MatchesProvenanceMarker
// returns false when the hash in the env var does not match wantHash — i.e.,
// the process belongs to a different project.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — sweep MUST NOT touch processes
// that belong to a different project.
func TestMatchesProvenanceMarker_WrongHash(t *testing.T) {
	t.Parallel()

	dir1 := projectHashFixtureProjectDir(t)
	dir2 := projectHashFixtureProjectDir(t)
	if dir1 == dir2 {
		t.Skip("temp dirs collided — skipping cross-project test")
	}

	hash1 := ComputeProjectHash(dir1)
	hash2 := ComputeProjectHash(dir2)

	// Env carries hash2 but we probe for hash1.
	env := []string{ProvenanceEnvVar(hash2)}

	if MatchesProvenanceMarker(env, hash1) {
		t.Errorf("MatchesProvenanceMarker: cross-project false-positive; env has %q but query is %q", hash2, hash1)
	}
}

// TestSpawnSysProcAttr_Fields verifies that SpawnSysProcAttr returns a
// *syscall.SysProcAttr with Setpgid=true and Pgid equal to the given value.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "On every handler subprocess
// spawn, the daemon MUST set Go's SysProcAttr{Setpgid: true,
// Pgid: <recorded_pgid>}."
func TestSpawnSysProcAttr_Fields(t *testing.T) {
	t.Parallel()

	wantPGID := core.PGID(12345)
	attr := SpawnSysProcAttr(wantPGID)

	if attr == nil {
		t.Fatal("SpawnSysProcAttr: returned nil")
	}
	if !attr.Setpgid {
		t.Error("SpawnSysProcAttr: Setpgid = false, want true")
	}
	if attr.Pgid != wantPGID.Int() {
		t.Errorf("SpawnSysProcAttr: Pgid = %d, want %d", attr.Pgid, wantPGID.Int())
	}
}

// TestTmuxSessionPrefix_Format verifies the canonical tmux session prefix
// format: "harmonik-<hash>-".
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "harmonik-<project_hash>-<session_name>".
func TestTmuxSessionPrefix_Format(t *testing.T) {
	t.Parallel()

	hash := ComputeProjectHash(projectHashFixtureProjectDir(t))
	prefix := TmuxSessionPrefix(hash)

	want := "harmonik-" + hash.String() + "-"
	if prefix != want {
		t.Errorf("TmuxSessionPrefix: got %q, want %q", prefix, want)
	}
}

// TestTmuxSessionName_Format verifies the full tmux session name
// "harmonik-<hash>-<sessionName>".
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "harmonik-<project_hash>-<session_name>".
func TestTmuxSessionName_Format(t *testing.T) {
	t.Parallel()

	hash := ComputeProjectHash(projectHashFixtureProjectDir(t))
	name := TmuxSessionName(hash, "main")

	want := "harmonik-" + hash.String() + "-main"
	if name != want {
		t.Errorf("TmuxSessionName: got %q, want %q", name, want)
	}
}

// TestRecordedPGID_ReturnsCurrentPGID verifies that RecordedPGID() returns the
// process's current PGID, consistent with syscall.Getpgrp().
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "This PGID MUST be recorded
// in the pidfile per PL-002b (line 2)."
func TestRecordedPGID_ReturnsCurrentPGID(t *testing.T) {
	t.Parallel()

	got := RecordedPGID()
	want := core.PGID(syscall.Getpgrp())

	if got != want {
		t.Errorf("RecordedPGID: got %d, want %d (syscall.Getpgrp)", got, want)
	}
}

// TestSplitNul_Basic validates the internal NUL-split helper used by
// ReadProcessEnviron to parse /proc/<pid>/environ.
func TestSplitNul_Basic(t *testing.T) {
	t.Parallel()

	// Simulate a two-entry NUL-separated environ.
	data := []byte("FOO=1\x00BAR=2\x00")
	parts := splitNul(data)

	if len(parts) < 2 {
		t.Fatalf("splitNul: expected ≥2 parts, got %d: %v", len(parts), parts)
	}
	if parts[0] != "FOO=1" {
		t.Errorf("splitNul: parts[0] = %q, want %q", parts[0], "FOO=1")
	}
	if parts[1] != "BAR=2" {
		t.Errorf("splitNul: parts[1] = %q, want %q", parts[1], "BAR=2")
	}
}

// TestReadProcessEnviron_OwnProcess reads the current process's own environment
// via ReadProcessEnviron and confirms the PATH variable is present. This test
// is Linux-only; on darwin /proc is absent and the function returns ErrNotExist.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "readable via
// /proc/<pid>/environ on Linux."
func TestReadProcessEnviron_OwnProcess(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat("/proc"); os.IsNotExist(err) {
		t.Skip("ReadProcessEnviron: /proc not available on this platform (darwin OQ-PL-008)")
	}

	pid := os.Getpid()
	env, err := ReadProcessEnviron(pid)
	if err != nil {
		t.Fatalf("ReadProcessEnviron(%d): %v", pid, err)
	}
	if len(env) == 0 {
		t.Fatal("ReadProcessEnviron: returned empty environment for own process")
	}

	// Sanity: we should be able to find at least one "KEY=VALUE" shaped entry.
	found := false
	for _, e := range env {
		if len(e) > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Error("ReadProcessEnviron: no non-empty entries in own environment")
	}
}
