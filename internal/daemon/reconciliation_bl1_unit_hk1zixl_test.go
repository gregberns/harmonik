package daemon_test

// reconciliation_bl1_unit_hk1zixl_test.go — unit tests for RunCatBL1StartupSweep,
// the Cat-BL1 child-bead orphan startup sweep (§8.BL1).
//
// Behavior under test (reconciliation.go):
//   - Enumerate open + in_progress beads carrying a parent:hk-* label.
//   - For each, check git for the parent's merge commit on the target branch.
//   - When no parent merge commit exists (orphan):
//       * OPEN orphan        → close it via `br close` (auto-close).
//       * IN_PROGRESS orphan → emit operator_escalation_required, do NOT close.
//
// Seams used (no real br / git binary required for the bead ledger):
//   - BrPath points at a dispatching mock `br` shell script that returns the
//     two orphan fixtures for `list --status open|in_progress` and records every
//     `close <id>` invocation to a marker file (the brcliFixtureMockBinary +
//     beadSyncCallWriteMockBr idiom). This lets the test prove which beads were
//     closed without a real ledger.
//   - The git merge-commit check runs against ProjectDir, which is a bare
//     t.TempDir() (NOT a git repo). `git log` exits non-zero there, which
//     hasParentMergeCommit treats as "no merge commit" → every candidate is an
//     orphan. This drives both the close path and the escalate path without a
//     real repo, exactly as the production fallback intends.
//   - Emitter is a recording fake capturing emitted event types + payloads.
//
// Each test fails if the corresponding behavior were removed: the OPEN-orphan
// assertion checks the bead's ID appears in the close marker; the IN_PROGRESS
// assertion checks operator_escalation_required was emitted AND the bead's ID
// did NOT appear in the close marker.
//
// Spec ref: specs/reconciliation/spec.md §8.BL1.
// Bead ref: hk-1zixl.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

const (
	bl1OpenOrphanID       = "hk-openorphan1"
	bl1InProgressOrphanID = "hk-inprogorphan1"
	bl1ParentOpen         = "hk-parentopen"
	bl1ParentInProgress   = "hk-parentinprog"
)

// bl1RecordingEmitter records emitted (eventType, payload) pairs for assertion.
type bl1RecordingEmitter struct {
	mu     sync.Mutex
	types  []core.EventType
	bodies [][]byte
}

func (e *bl1RecordingEmitter) Emit(_ context.Context, t core.EventType, payload []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.types = append(e.types, t)
	cp := make([]byte, len(payload))
	copy(cp, payload)
	e.bodies = append(e.bodies, cp)
	return nil
}

func (e *bl1RecordingEmitter) has(t core.EventType) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ev := range e.types {
		if ev == t {
			return true
		}
	}
	return false
}

// orphanBeadIDs returns the bead IDs from every emitted orphaned_child_bead event.
func (e *bl1RecordingEmitter) orphanBeadIDs(t *testing.T) []string {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	var ids []string
	for i, ev := range e.types {
		if ev != core.EventTypeOrphanedChildBead {
			continue
		}
		var pl core.OrphanedChildBeadPayload
		if err := json.Unmarshal(e.bodies[i], &pl); err != nil {
			t.Fatalf("unmarshal orphaned_child_bead: %v", err)
		}
		ids = append(ids, pl.BeadID)
	}
	return ids
}

// bl1ListJSON builds a `br list` envelope with one bead in the given status
// carrying a parent:hk-* label for parentID.
func bl1ListJSON(beadID, status, parentID string) string {
	return fmt.Sprintf(
		`{"issues":[{"id":%q,"title":"orphan child","description":"","status":%q,"priority":2,"issue_type":"task","labels":["parent:%s"],"dependency_count":0,"dependent_count":0}]}`,
		beadID, status, parentID,
	)
}

// bl1WriteDispatchingMockBr writes a mock `br` that:
//   - `list --status open`        → prints the open-orphan envelope (exit 0)
//   - `list --status in_progress` → prints the in_progress-orphan envelope (exit 0)
//   - `close <id> ...`            → records "<id>" to closeMarkerPath (exit 0)
//   - anything else               → empty issues envelope (exit 0)
//
// Mirrors the brcliFixtureMockBinary + beadSyncCallWriteMockBr shell-script
// idiom established in this package.
func bl1WriteDispatchingMockBr(t *testing.T, closeMarkerPath string) string {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "br")

	// The label is "parent:hk-..."; bl1ListJSON prefixes the "parent:" itself,
	// and the bl1Parent* constants already start with "hk-".
	openJSON := bl1ListJSON(bl1OpenOrphanID, "open", bl1ParentOpen)
	inProgJSON := bl1ListJSON(bl1InProgressOrphanID, "in_progress", bl1ParentInProgress)

	// The script dispatches on the subcommand ($1) and, for list, the status flag.
	script := fmt.Sprintf(`#!/bin/sh
sub="$1"
if [ "$sub" = "list" ]; then
  # find the value after --status
  status=""
  prev=""
  for a in "$@"; do
    if [ "$prev" = "--status" ]; then status="$a"; fi
    prev="$a"
  done
  if [ "$status" = "open" ]; then
    printf '%%s' %q
    exit 0
  fi
  if [ "$status" = "in_progress" ]; then
    printf '%%s' %q
    exit 0
  fi
  printf '%%s' '{"issues":[]}'
  exit 0
fi
if [ "$sub" = "close" ]; then
  printf '%%s\n' "$2" >> %q
  exit 0
fi
exit 0
`, openJSON, inProgJSON, closeMarkerPath)

	//nolint:gosec // G306: 0755 required for executable mock-br fixture
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("bl1WriteDispatchingMockBr: WriteFile: %v", err)
	}
	return scriptPath
}

// bl1ReadCloseMarker returns the bead IDs that the mock-br recorded as closed.
func bl1ReadCloseMarker(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: test-controlled temp path
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("bl1ReadCloseMarker: read %s: %v", path, err)
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ids = append(ids, line)
		}
	}
	return ids
}

func bl1Contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestRunCatBL1StartupSweep_OpenOrphanClosed_InProgressEscalated drives the
// Cat-BL1 sweep with one OPEN orphan and one IN_PROGRESS orphan (both with no
// parent merge commit) and asserts:
//   - both beads emit orphaned_child_bead,
//   - the OPEN orphan is closed (its ID is recorded by the mock `br close`),
//   - the IN_PROGRESS orphan emits operator_escalation_required and is NOT
//     closed (its ID is absent from the close marker).
//
// Spec ref: specs/reconciliation/spec.md §8.BL1.
// Bead ref: hk-1zixl.
func TestRunCatBL1StartupSweep_OpenOrphanClosed_InProgressEscalated(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir() // NOT a git repo → git log fails → every candidate is an orphan
	closeMarker := filepath.Join(t.TempDir(), "br-close-invocations.txt")
	brPath := bl1WriteDispatchingMockBr(t, closeMarker)

	emitter := &bl1RecordingEmitter{}
	cfg := daemon.CatBL1StartupSweepConfig{
		ProjectDir:   projectDir,
		BrPath:       brPath,
		TargetBranch: "main",
		Emitter:      emitter,
		LogWriter:    os.Stderr,
	}

	if err := daemon.RunCatBL1StartupSweep(context.Background(), cfg); err != nil {
		t.Fatalf("RunCatBL1StartupSweep returned error: %v", err)
	}

	// Both orphans should have produced an orphaned_child_bead event.
	orphanIDs := emitter.orphanBeadIDs(t)
	if !bl1Contains(orphanIDs, bl1OpenOrphanID) {
		t.Errorf("no orphaned_child_bead for OPEN orphan %s (got %v)", bl1OpenOrphanID, orphanIDs)
	}
	if !bl1Contains(orphanIDs, bl1InProgressOrphanID) {
		t.Errorf("no orphaned_child_bead for IN_PROGRESS orphan %s (got %v)", bl1InProgressOrphanID, orphanIDs)
	}

	// OPEN orphan → closed.
	closed := bl1ReadCloseMarker(t, closeMarker)
	if !bl1Contains(closed, bl1OpenOrphanID) {
		t.Errorf("OPEN orphan %s was NOT closed; close-marker contents: %v", bl1OpenOrphanID, closed)
	}

	// IN_PROGRESS orphan → escalated, NOT closed.
	if !emitter.has(core.EventTypeOperatorEscalationRequired) {
		t.Errorf("expected operator_escalation_required for the IN_PROGRESS orphan, but none was emitted (events: %v)", emitter.types)
	}
	if bl1Contains(closed, bl1InProgressOrphanID) {
		t.Errorf("IN_PROGRESS orphan %s was closed; it must be escalated, not auto-closed; close-marker: %v",
			bl1InProgressOrphanID, closed)
	}
}
