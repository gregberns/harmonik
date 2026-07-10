package daemon_test

// reconciliation_bl3_unit_hkg1o02_test.go — unit tests for RunCatBL3StartupSweep
// verifying that both bead_ledger_conflict_audit and operator_escalation_required
// are emitted when merge-conflicts.log is non-empty (§8.BL3 steps 3–4).
//
// Spec ref: specs/reconciliation/spec.md §8.BL3.
// Bead ref: hk-g1o02.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// bl3StubEmitter records emitted event types for assertion.
type bl3StubEmitter struct {
	mu     sync.Mutex
	events []core.EventType
}

func (e *bl3StubEmitter) Emit(_ context.Context, t core.EventType, _ []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, t)
	return nil
}

func (e *bl3StubEmitter) has(t core.EventType) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ev := range e.events {
		if ev == t {
			return true
		}
	}
	return false
}

// TestRunCatBL3StartupSweep_EmitsBothEventsOnConflictLog verifies that
// RunCatBL3StartupSweep emits bead_ledger_conflict_audit AND
// operator_escalation_required when merge-conflicts.log contains valid entries.
//
// Spec ref: specs/reconciliation/spec.md §8.BL3 steps 3–4.
// Bead ref: hk-g1o02.
func TestRunCatBL3StartupSweep_EmitsBothEventsOnConflictLog(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	beadsDir := filepath.Join(projectDir, ".beads")
	//nolint:gosec // G301: test-only temp directory
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}

	conflictLine := "2026-06-24T12:00:00Z CONFLICT bead=hk-abc123 field=status a=open b=closed resolution=b\n"
	conflictLogPath := filepath.Join(beadsDir, "merge-conflicts.log")
	//nolint:gosec // G306: test-only file
	if err := os.WriteFile(conflictLogPath, []byte(conflictLine), 0o644); err != nil {
		t.Fatalf("write merge-conflicts.log: %v", err)
	}

	emitter := &bl3StubEmitter{}
	cfg := daemon.CatBL3StartupSweepConfig{
		ProjectDir: projectDir,
		RunID:      "test-run-id",
		Emitter:    emitter,
	}

	if err := daemon.RunCatBL3StartupSweep(context.Background(), cfg); err != nil {
		t.Fatalf("RunCatBL3StartupSweep returned error: %v", err)
	}

	if !emitter.has(core.EventTypeBeadLedgerConflictAudit) {
		t.Errorf("expected %q event to be emitted, but it was not", core.EventTypeBeadLedgerConflictAudit)
	}
	if !emitter.has(core.EventTypeOperatorEscalationRequired) {
		t.Errorf("expected %q event to be emitted, but it was not", core.EventTypeOperatorEscalationRequired)
	}
	// hk-u4dv4: the escalation must also land in the operator-mailbox
	// projection via a decision_needed event on the reserved topic.
	if !emitter.has(core.EventTypeDecisionNeeded) {
		t.Errorf("expected %q event (operator-mailbox routing) to be emitted, but it was not", core.EventTypeDecisionNeeded)
	}
}
