package brcli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gregberns/harmonik/internal/core"
)

const beadsOwnedSubdir = "beads-owned"

func beadsOwnedDir(projectDir string) string {
	if projectDir == "" {
		return ""
	}
	return filepath.Join(projectDir, ".harmonik", beadsOwnedSubdir)
}

func beadsOwnedSentinelPath(ownedDir, beadID string) string {
	return filepath.Join(ownedDir, beadID)
}

func writeBeadsOwnedSentinel(projectDir, beadID string) error {
	dir := beadsOwnedDir(projectDir)
	if dir == "" {
		return nil // test caller with no projectDir — skip
	}
	if err := os.MkdirAll(dir, core.HarmonikDirMode); err != nil {
		return fmt.Errorf("brcli.writeBeadsOwnedSentinel: MkdirAll %q: %w", dir, err)
	}
	sentinelPath := beadsOwnedSentinelPath(dir, beadID)
	//nolint:gosec // G304: path is constructed from operator-controlled projectDir + bead ID (opaque, no separators)
	f, err := os.OpenFile(sentinelPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("brcli.writeBeadsOwnedSentinel: create %q: %w", sentinelPath, err)
	}
	return f.Close()
}

func beadsOwnedSentinelExists(projectDir, beadID string) bool {
	dir := beadsOwnedDir(projectDir)
	if dir == "" {
		return false
	}
	_, err := os.Stat(beadsOwnedSentinelPath(dir, beadID))
	return err == nil
}

func deleteBeadsOwnedSentinel(projectDir, beadID string) error {
	dir := beadsOwnedDir(projectDir)
	if dir == "" {
		return nil // test caller with no projectDir — skip
	}
	sentinelPath := beadsOwnedSentinelPath(dir, beadID)
	if err := os.Remove(sentinelPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("brcli.deleteBeadsOwnedSentinel: Remove %q: %w", sentinelPath, err)
	}
	return nil
}
