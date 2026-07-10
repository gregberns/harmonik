package dashboard

// unlock.go — the operator override for the staleness forcing gate
// (plans/2026-07-03-operator-dashboard/DESIGN.md §4 guardrail: "MUST have an
// operator override (`harmonik dashboard --unlock` / a config kill-switch)").
//
// Persisted at .harmonik/context/dashboard-unlock.json. The expiry is
// mandatory (mirrors the sentinel PhaseFlag+Expiry convention in
// internal/digest/sentinelconfig.go): an operator who forgets to re-lock
// cannot leave the gate permanently disabled by accident.
//
// Bead ref: hk-xg6rw.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// UnlockState is the on-disk shape of dashboard-unlock.json.
type UnlockState struct {
	// UnlockedUntil is the mandatory expiry — the gate resumes normal
	// staleness evaluation once now >= UnlockedUntil.
	UnlockedUntil time.Time `json:"unlocked_until"`
	// By records who applied the override (operator identity or "operator").
	By string `json:"by,omitempty"`
}

// UnlockPath returns the canonical path to dashboard-unlock.json for the given
// project directory.
func UnlockPath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "context", "dashboard-unlock.json")
}

// ReadUnlock reads the unlock override. Returns (nil, nil) when the file is
// absent (the common case — no override in effect).
func ReadUnlock(projectDir string) (*UnlockState, error) {
	data, err := os.ReadFile(UnlockPath(projectDir)) //nolint:gosec // G304: operator-controlled projectDir
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var u UnlockState
	if err := json.Unmarshal(data, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// WriteUnlock atomically writes the unlock override with the given expiry and
// operator identity.
func WriteUnlock(projectDir string, until time.Time, by string) error {
	u := UnlockState{UnlockedUntil: until, By: by}
	data, err := json.MarshalIndent(u, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Join(projectDir, ".harmonik", "context")
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	dst := UnlockPath(projectDir)
	tmp := dst + ".tmp"
	//nolint:gosec // G306: 0644 is appropriate for a readable context file
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// ClearUnlock removes the unlock override file, re-arming the gate
// immediately (early re-lock). A no-op, not an error, when no override file
// exists.
func ClearUnlock(projectDir string) error {
	err := os.Remove(UnlockPath(projectDir))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Active reports whether the unlock override is currently in effect. A nil
// receiver (no override on disk) is never active.
func (u *UnlockState) Active(now time.Time) bool {
	return u != nil && now.Before(u.UnlockedUntil)
}
