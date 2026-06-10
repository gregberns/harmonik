package supervisecmd

// loopstatus.go — CognitionLoopStatus type and loop-status.json file surface.
//
// The cognition loop writes its current LoopStatus to
// .harmonik/cognition/loop-status.json. `harmonik supervise status` reads this
// file and surfaces the status to the operator, satisfying ON-008a's
// `budget-paused` / `circuit-tripped` surfacing obligation.
//
// Spec ref: specs/operator-nfr.md §4.3 ON-008a;
//           specs/cognition-loop.md §6 LoopStatus type.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CognitionLoopStatus is the cognition loop's current operational state.
// Values are the LoopStatus enum declared in specs/cognition-loop.md §6.
//
// Spec ref: specs/cognition-loop.md §6
//
//	LoopStatus: {starting, ready, paused, budget-paused, circuit-tripped, draining, stopped}
type CognitionLoopStatus string

const (
	// CognitionLoopStatusStarting — loop is initialising; not yet ready to react.
	CognitionLoopStatusStarting CognitionLoopStatus = "starting"

	// CognitionLoopStatusReady — loop is running normally.
	CognitionLoopStatusReady CognitionLoopStatus = "ready"

	// CognitionLoopStatusPaused — operator-issued pause (via `harmonik supervise pause`).
	CognitionLoopStatusPaused CognitionLoopStatus = "paused"

	// CognitionLoopStatusBudgetPaused — per-day spend cap exhausted (ON-008a / CL-090).
	// Entered when the unified per-day spend meter exhausts and the budget-exhaustion
	// handler-pause policy fires (handler-pause.md HP-012). Operator clears via
	// `harmonik supervise resume`; reset is not automatic.
	//
	// Spec ref: specs/operator-nfr.md §4.3 ON-008a;
	//           specs/cognition-loop.md §4.11 CL-090.
	CognitionLoopStatusBudgetPaused CognitionLoopStatus = "budget-paused"

	// CognitionLoopStatusCircuitTripped — reaction-rate circuit breaker fired (CL-091).
	// Entered when sustained reaction rate exceeds the operator-set threshold.
	// Operator clears via `harmonik supervise resume`.
	//
	// Spec ref: specs/cognition-loop.md §4.11 CL-091.
	CognitionLoopStatusCircuitTripped CognitionLoopStatus = "circuit-tripped"

	// CognitionLoopStatusDraining — loop is draining; shutdown in progress.
	CognitionLoopStatusDraining CognitionLoopStatus = "draining"

	// CognitionLoopStatusStopped — loop has stopped.
	CognitionLoopStatusStopped CognitionLoopStatus = "stopped"
)

// IsKnown reports whether s is one of the declared LoopStatus values from
// cognition-loop.md §6.
func (s CognitionLoopStatus) IsKnown() bool {
	switch s {
	case CognitionLoopStatusStarting,
		CognitionLoopStatusReady,
		CognitionLoopStatusPaused,
		CognitionLoopStatusBudgetPaused,
		CognitionLoopStatusCircuitTripped,
		CognitionLoopStatusDraining,
		CognitionLoopStatusStopped:
		return true
	}
	return false
}

// LoopStatusRecord is the schema for .harmonik/cognition/loop-status.json.
// Written atomically by the cognition loop; read by `harmonik supervise status`.
//
// schema_version=1 is the initial version; N-1 readers MUST tolerate unknown fields.
type LoopStatusRecord struct {
	SchemaVersion int                 `json:"schema_version"`
	Status        CognitionLoopStatus `json:"status"`
	UpdatedAt     string              `json:"updated_at,omitempty"` // RFC3339
	// PauseReason carries additional detail when Status is "paused",
	// "budget-paused", or "circuit-tripped", matching ON-054's pause-reason
	// discriminator surface. The value is one of "operator-pause",
	// "budget-paused", or "circuit-tripped".
	PauseReason string `json:"pause_reason,omitempty"`
}

// LoopStatusPath returns the path to the loop-status.json file.
func LoopStatusPath(projectDir string) string {
	return filepath.Join(CognitionDir(projectDir), "loop-status.json")
}

// ReadLoopStatus reads .harmonik/cognition/loop-status.json. Returns nil when
// the file does not exist (cognition loop has not written status yet).
func ReadLoopStatus(projectDir string) (*LoopStatusRecord, error) {
	//nolint:gosec // G304: path derived from operator-controlled projectDir
	data, err := os.ReadFile(LoopStatusPath(projectDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("supervisecmd: ReadLoopStatus: %w", err)
	}
	var rec LoopStatusRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("supervisecmd: ReadLoopStatus: unmarshal: %w", err)
	}
	return &rec, nil
}

// WriteLoopStatusAtomic writes rec to .harmonik/cognition/loop-status.json
// atomically via temp+rename+fsync per WM-026. Called by the cognition loop
// on every LoopStatus transition.
func WriteLoopStatusAtomic(projectDir string, rec LoopStatusRecord) error {
	dir := CognitionDir(projectDir)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("supervisecmd: WriteLoopStatusAtomic: mkdir: %w", err)
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("supervisecmd: WriteLoopStatusAtomic: marshal: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, "loop-status-*.json.tmp")
	if err != nil {
		return fmt.Errorf("supervisecmd: WriteLoopStatusAtomic: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	success := false
	defer func() {
		_ = tmp.Close()
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("supervisecmd: WriteLoopStatusAtomic: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("supervisecmd: WriteLoopStatusAtomic: fsync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("supervisecmd: WriteLoopStatusAtomic: close: %w", err)
	}
	destPath := LoopStatusPath(projectDir)
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("supervisecmd: WriteLoopStatusAtomic: rename: %w", err)
	}
	success = true

	// fsync parent directory to make rename durable (WM-026).
	//nolint:gosec // G304
	if dirFd, err := os.Open(dir); err == nil {
		_ = dirFd.Sync()
		_ = dirFd.Close()
	}
	return nil
}
