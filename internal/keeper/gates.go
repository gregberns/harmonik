package keeper

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// idleMarkerPath returns the path to <projectDir>/.harmonik/keeper/<agent>.idle.
func idleMarkerPath(projectDir, agent string) string {
	return filepath.Join(projectDir, ".harmonik", "keeper", agent+".idle")
}

// dispatchingMarkerPath returns the path to <projectDir>/.harmonik/keeper/<agent>.dispatching.
func dispatchingMarkerPath(projectDir, agent string) string {
	return filepath.Join(projectDir, ".harmonik", "keeper", agent+".dispatching")
}

// CrispIdle reports whether the agent is at a crisp await-input boundary: the
// .idle marker exists AND its mtime is strictly newer than the .ctx gauge file's
// mtime. The Stop hook writes .idle only at await-input boundaries, so .idle
// newer than .ctx means the agent stopped AFTER its last context activity.
//
// Returns false when the .idle marker is absent, when the .ctx gauge file
// cannot be stat'd, or when .idle does not postdate .ctx.
func CrispIdle(projectDir, agent string) bool {
	idleStat, err := os.Stat(idleMarkerPath(projectDir, agent))
	if err != nil {
		return false // absent or unreadable
	}
	ctxStat, err := os.Stat(ctxFilePath(projectDir, agent))
	if err != nil {
		return false // no ctx yet — can't confirm ordering
	}
	return idleStat.ModTime().After(ctxStat.ModTime())
}

// HoldingDispatch reports whether the agent has in-flight queue work that the
// session-keeper cycle must defer around. It checks for the presence of the
// .dispatching marker file.
//
// FAIL-CLOSED: any stat error other than ErrNotExist (e.g. permission denied,
// I/O error) is treated as HoldingDispatch = true so the cycle never clobbers
// an uncertain state.
func HoldingDispatch(projectDir, agent string) bool {
	_, err := os.Stat(dispatchingMarkerPath(projectDir, agent))
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return true // fail-closed on unexpected error
}

// SetDispatching writes the .dispatching marker for the given agent, recording
// the current timestamp as its content. The orchestrator calls this before
// submitting a batch to the queue so the session-keeper cycle defers.
func SetDispatching(projectDir, agent string) error {
	if err := validateAgent(agent); err != nil {
		return err
	}
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		return fmt.Errorf("keeper: create keeper dir: %w", err)
	}
	path := dispatchingMarkerPath(projectDir, agent)
	content := time.Now().UTC().Format(time.RFC3339) + "\n"
	//nolint:gosec // G306: 0600 — keeper-owned file, no world-read needed
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("keeper: write dispatching marker %q: %w", path, err)
	}
	return nil
}

// ClearDispatching removes the .dispatching marker for the given agent.
// The orchestrator calls this when all in-flight queue work has completed.
// It is idempotent: an already-absent marker is not an error.
func ClearDispatching(projectDir, agent string) error {
	if err := validateAgent(agent); err != nil {
		return err
	}
	if err := os.Remove(dispatchingMarkerPath(projectDir, agent)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("keeper: remove dispatching marker: %w", err)
	}
	return nil
}
