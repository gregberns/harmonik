package brcli_test

// beadsownedsentinel_hk11xkn_test.go — integration tests for the beads-owned
// sentinel file lifecycle: ClaimBead writes sentinel, CloseBead/ReopenBead/
// ResetBead delete it.
//
// Tests use the fake-br-binary pattern (a shell script that exits 0) to avoid
// a real Beads installation. They construct an Adapter via NewForProject so
// that a.projectDir is populated and the sentinel write/delete paths execute.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet; §4.4 PL-006a.
// Bead ref: hk-11xkn.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// hk11xknTempProject creates a temp projectDir with a .harmonik/ subdirectory
// and returns the adapter configured with a br stub that always exits 0.
func hk11xknTempProject(t *testing.T) (adapter *brcli.Adapter, projectDir string, intentLogDir string) {
	t.Helper()
	projectDir = t.TempDir()

	// Create .harmonik/ subdirectory structure needed by the sentinel helpers.
	harmonikDir := filepath.Join(projectDir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil { //nolint:gosec
		t.Fatalf("MkdirAll harmonikDir: %v", err)
	}
	intentLogDir = filepath.Join(harmonikDir, "beads-intents")
	if err := os.MkdirAll(intentLogDir, 0o755); err != nil { //nolint:gosec
		t.Fatalf("MkdirAll intentLogDir: %v", err)
	}

	// Build a fake br script that exits 0.
	brScript := filepath.Join(t.TempDir(), "br")
	if err := os.WriteFile(brScript, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil { //nolint:gosec
		t.Fatalf("write br stub: %v", err)
	}

	var err error
	adapter, err = brcli.NewForProject(brScript, projectDir)
	if err != nil {
		t.Fatalf("NewForProject: %v", err)
	}
	return adapter, projectDir, intentLogDir
}

// sentinelExists reports whether the sentinel file for beadID exists under the
// beads-owned/ directory of projectDir.
func sentinelExists(t *testing.T, projectDir string, beadID core.BeadID) bool {
	t.Helper()
	path := filepath.Join(projectDir, ".harmonik", "beads-owned", string(beadID))
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat sentinel %q: %v", path, err)
	return false
}

// hk11xknFakeRunID returns a fresh UUIDv7 RunID for tests.
func hk11xknFakeRunID(t *testing.T) core.RunID {
	t.Helper()
	return core.RunID(uuid.Must(uuid.NewV7()))
}

// hk11xknFakeTransitionID returns a fresh UUIDv7 TransitionID for tests.
func hk11xknFakeTransitionID(t *testing.T) core.TransitionID {
	t.Helper()
	return core.TransitionID(uuid.Must(uuid.NewV7()))
}

// TestHK11xkn_ClaimBead_WritesSentinel verifies that a successful ClaimBead
// creates the .harmonik/beads-owned/<bead-id> sentinel file.
func TestHK11xkn_ClaimBead_WritesSentinel(t *testing.T) {
	t.Parallel()

	adapter, projectDir, intentLogDir := hk11xknTempProject(t)
	const beadID = core.BeadID("hk-sentinel-claim-001")

	if err := adapter.ClaimBead(
		context.Background(),
		intentLogDir,
		brcli.TimeoutConfig{},
		hk11xknFakeRunID(t),
		hk11xknFakeTransitionID(t),
		beadID,
	); err != nil {
		t.Fatalf("ClaimBead: unexpected error: %v", err)
	}

	if !sentinelExists(t, projectDir, beadID) {
		t.Error("ClaimBead: sentinel file not created in .harmonik/beads-owned/")
	}
}

// TestHK11xkn_CloseBead_DeletesSentinel verifies that a successful CloseBead
// removes the .harmonik/beads-owned/<bead-id> sentinel file.
func TestHK11xkn_CloseBead_DeletesSentinel(t *testing.T) {
	t.Parallel()

	adapter, projectDir, intentLogDir := hk11xknTempProject(t)
	const beadID = core.BeadID("hk-sentinel-close-001")

	// Pre-create the sentinel file as if ClaimBead had written it.
	ownedDir := filepath.Join(projectDir, ".harmonik", "beads-owned")
	if err := os.MkdirAll(ownedDir, 0o755); err != nil { //nolint:gosec
		t.Fatalf("MkdirAll beads-owned: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ownedDir, string(beadID)), nil, 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	if err := adapter.CloseBead(
		context.Background(),
		intentLogDir,
		brcli.TimeoutConfig{},
		hk11xknFakeRunID(t),
		hk11xknFakeTransitionID(t),
		beadID,
		false, // needsAttention
	); err != nil {
		t.Fatalf("CloseBead: unexpected error: %v", err)
	}

	if sentinelExists(t, projectDir, beadID) {
		t.Error("CloseBead: sentinel file still present after successful close")
	}
}

// TestHK11xkn_ReopenBead_DeletesSentinel verifies that a successful ReopenBead
// removes the .harmonik/beads-owned/<bead-id> sentinel file.
func TestHK11xkn_ReopenBead_DeletesSentinel(t *testing.T) {
	t.Parallel()

	adapter, projectDir, intentLogDir := hk11xknTempProject(t)
	const beadID = core.BeadID("hk-sentinel-reopen-001")

	// Pre-create the sentinel file.
	ownedDir := filepath.Join(projectDir, ".harmonik", "beads-owned")
	if err := os.MkdirAll(ownedDir, 0o755); err != nil { //nolint:gosec
		t.Fatalf("MkdirAll beads-owned: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ownedDir, string(beadID)), nil, 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	if err := adapter.ReopenBead(
		context.Background(),
		intentLogDir,
		brcli.TimeoutConfig{},
		hk11xknFakeRunID(t),
		hk11xknFakeTransitionID(t),
		beadID,
		"test reason",
	); err != nil {
		t.Fatalf("ReopenBead: unexpected error: %v", err)
	}

	if sentinelExists(t, projectDir, beadID) {
		t.Error("ReopenBead: sentinel file still present after successful reopen")
	}
}

// TestHK11xkn_ResetBead_DeletesSentinel verifies that a successful ResetBead
// removes the .harmonik/beads-owned/<bead-id> sentinel file.
func TestHK11xkn_ResetBead_DeletesSentinel(t *testing.T) {
	t.Parallel()

	adapter, projectDir, intentLogDir := hk11xknTempProject(t)
	const beadID = core.BeadID("hk-sentinel-reset-001")

	// Pre-create the sentinel file.
	ownedDir := filepath.Join(projectDir, ".harmonik", "beads-owned")
	if err := os.MkdirAll(ownedDir, 0o755); err != nil { //nolint:gosec
		t.Fatalf("MkdirAll beads-owned: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ownedDir, string(beadID)), nil, 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	if err := adapter.ResetBead(
		context.Background(),
		intentLogDir,
		brcli.TimeoutConfig{},
		beadID,
		core.ProjectHash("abc123def456"),
		time.Now().UnixNano(),
	); err != nil {
		t.Fatalf("ResetBead: unexpected error: %v", err)
	}

	if sentinelExists(t, projectDir, beadID) {
		t.Error("ResetBead: sentinel file still present after successful reset")
	}
}

// TestHK11xkn_CloseBead_MissingSentinel_NotError verifies that CloseBead is
// idempotent when the sentinel file does not exist (e.g. partial prior write).
func TestHK11xkn_CloseBead_MissingSentinel_NotError(t *testing.T) {
	t.Parallel()

	adapter, _, intentLogDir := hk11xknTempProject(t)
	const beadID = core.BeadID("hk-sentinel-close-nomissing-001")

	// CloseBead with no pre-existing sentinel should not error.
	if err := adapter.CloseBead(
		context.Background(),
		intentLogDir,
		brcli.TimeoutConfig{},
		hk11xknFakeRunID(t),
		hk11xknFakeTransitionID(t),
		beadID,
		false,
	); err != nil {
		t.Fatalf("CloseBead on missing sentinel: unexpected error: %v", err)
	}
}
