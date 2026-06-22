package lifecycle

// orphansweep_gcintents_redrive_test.go — unit tests for the BI-031 step-4
// re-drive path in GCRetiredIntentsWithRedrive.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4 (4a–4f).
// Bead ref: hk-aev8t (G3 — step-4 re-drive missing).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// fakeRedriveWriter is a deterministic IntentRedriveWriter fake for tests.
// When a bead ID is in the `succeed` set, ReissueTerminalTransition deletes
// the intent file (simulating 4a success) and increments calls. When the bead
// is in the `fail` set, it returns an error (simulating 4b–4f). Any other bead
// ID returns an unexpected-call error.
type fakeRedriveWriter struct {
	succeed map[core.BeadID]bool
	fail    map[core.BeadID]bool
	calls   []core.BeadID
}

func (f *fakeRedriveWriter) ReissueTerminalTransition(
	_ context.Context,
	intentLogDir string,
	_ brcli.TimeoutConfig,
	entry core.IntentLogEntry,
) error {
	f.calls = append(f.calls, entry.BeadID)
	if f.succeed[entry.BeadID] {
		// Simulate (4a): delete the intent file on success (step 6).
		_ = os.Remove(filepath.Join(intentLogDir, entry.IdempotencyKey+".json")) //nolint:errcheck
		return nil
	}
	if f.fail[entry.BeadID] {
		return errors.New("fakeRedriveWriter: simulated redrive failure (retain for Cat 3a)")
	}
	return errors.New("fakeRedriveWriter: unexpected call for bead " + string(entry.BeadID))
}

// TestGCRetiredIntentsWithRedrive_NilWriter verifies that when RedriveWriter is
// nil, GCRetiredIntentsWithRedrive behaves identically to GCRetiredIntents
// (landed files removed, non-landed files retained, no re-drive).
func TestGCRetiredIntentsWithRedrive_NilWriter(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")

	// claim (→in_progress): bead is now in_progress → op LANDED.
	landedID := core.BeadID("hk-rdnil-landed")
	gcIntentsFixtureWriteIntent(t, intentsDir, "proj_hk-rdnil-landed_claim_1", landedID,
		"claim", "in_progress")

	// close (in_progress→closed): bead is still in_progress → op NOT landed, no pre-state match.
	// Writer=nil → retained for Cat 3a (step 5 divergence: pre-state for close is in_progress,
	// but in_progress IS the pre-state, so it actually would redrive if writer != nil — test the
	// nil-writer retained path explicitly).
	//
	// Use reopen (closed→open) with bead at "open" but intendedPostState="open":
	// gcIntentOpLanded(reopen, open, open) = true → landed/removed. That's not what we want.
	//
	// For a genuinely non-landed, non-redriven entry with nil writer, use:
	// close (in_progress→closed), bead at in_progress.
	// gcIntentOpLanded(close, in_progress, closed) = false. Pre-state for close = in_progress.
	// in_progress == in_progress → WOULD redrive if writer != nil. With nil writer → retained. ✓
	pendingID := core.BeadID("hk-rdnil-pending")
	pendingPath := gcIntentsFixtureWriteIntent(t, intentsDir, "proj_hk-rdnil-pending_close_1", pendingID,
		"close", "closed")

	ledger := &fakeIntentGCLedger{
		records: map[core.BeadID]core.BeadRecord{
			landedID:  {BeadID: landedID, Status: core.CoarseStatusInProgress}, // claim: in_progress == intended post-state → landed
			pendingID: {BeadID: pendingID, Status: core.CoarseStatusInProgress}, // close: in_progress = pre-state, not yet landed
		},
	}

	result, err := GCRetiredIntentsWithRedrive(context.Background(), GCRetiredIntentsConfig{
		ProjectDir:      projectDir,
		DaemonStartTime: time.Now(),
		Ledger:          ledger,
		RedriveWriter:   nil,
	})
	if err != nil {
		t.Fatalf("GCRetiredIntentsWithRedrive nil writer: unexpected error: %v", err)
	}
	if result.Removed != 1 {
		t.Errorf("Removed = %d, want 1", result.Removed)
	}
	if result.Retained != 1 {
		t.Errorf("Retained = %d, want 1", result.Retained)
	}
	if result.RedriveCount != 0 {
		t.Errorf("RedriveCount = %d, want 0 (nil writer)", result.RedriveCount)
	}
	// Non-landed file must still be on disk.
	if _, statErr := os.Stat(pendingPath); os.IsNotExist(statErr) {
		t.Errorf("pending intent file was unexpectedly deleted")
	}
}

// TestGCRetiredIntentsWithRedrive_RedriveSuccess verifies that when a bead is
// still at the op's pre-state and RedriveWriter succeeds, the intent file is
// deleted and RedriveCount is incremented (BI-031 step 4 branch 4a).
func TestGCRetiredIntentsWithRedrive_RedriveSuccess(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")

	beadID := core.BeadID("hk-rd-prestate")
	// claim (open→in_progress): pre-state is "open".
	intentPath := gcIntentsFixtureWriteIntent(t, intentsDir, "proj_hk-rd-prestate_claim_1", beadID,
		"claim", "in_progress")

	// Bead is at "open" — the pre-state for claim.
	// gcIntentOpLanded(claim, open, in_progress) = false (NOT landed).
	// gcIntentOpPreState(claim) = open.  open == open → step 4 redrive triggered.
	ledger := &fakeIntentGCLedger{
		records: map[core.BeadID]core.BeadRecord{
			beadID: {BeadID: beadID, Status: core.CoarseStatusOpen},
		},
	}
	writer := &fakeRedriveWriter{
		succeed: map[core.BeadID]bool{beadID: true},
		fail:    map[core.BeadID]bool{},
	}

	result, err := GCRetiredIntentsWithRedrive(context.Background(), GCRetiredIntentsConfig{
		ProjectDir:      projectDir,
		DaemonStartTime: time.Now(),
		Ledger:          ledger,
		RedriveWriter:   writer,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RedriveCount != 1 {
		t.Errorf("RedriveCount = %d, want 1", result.RedriveCount)
	}
	if result.Removed != 0 {
		t.Errorf("Removed = %d, want 0 (redrive success, not gc-remove)", result.Removed)
	}
	if result.Retained != 0 {
		t.Errorf("Retained = %d, want 0", result.Retained)
	}
	// fakeRedriveWriter deletes the file on success (step 6).
	if _, statErr := os.Stat(intentPath); !os.IsNotExist(statErr) {
		t.Errorf("intent file should have been deleted by fakeRedriveWriter (step 6)")
	}
	if len(writer.calls) != 1 || writer.calls[0] != beadID {
		t.Errorf("writer.calls = %v, want [%s]", writer.calls, beadID)
	}
}

// TestGCRetiredIntentsWithRedrive_RedriveFailure verifies that when
// ReissueTerminalTransition returns an error (branches 4b–4f), the intent file
// is retained for Cat 3a (Retained incremented, RedriveCount unchanged).
func TestGCRetiredIntentsWithRedrive_RedriveFailure(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")

	beadID := core.BeadID("hk-rd-fail")
	// close (in_progress→closed): pre-state is "in_progress".
	intentPath := gcIntentsFixtureWriteIntent(t, intentsDir, "proj_hk-rd-fail_close_1", beadID,
		"close", "closed")

	// Bead is at "in_progress" — the pre-state for close.
	// gcIntentOpLanded(close, in_progress, closed) = false (NOT landed).
	// gcIntentOpPreState(close) = in_progress.  in_progress == in_progress → redrive triggered.
	// Writer returns error → intent retained for Cat 3a.
	ledger := &fakeIntentGCLedger{
		records: map[core.BeadID]core.BeadRecord{
			beadID: {BeadID: beadID, Status: core.CoarseStatusInProgress},
		},
	}
	writer := &fakeRedriveWriter{
		succeed: map[core.BeadID]bool{},
		fail:    map[core.BeadID]bool{beadID: true},
	}

	result, err := GCRetiredIntentsWithRedrive(context.Background(), GCRetiredIntentsConfig{
		ProjectDir:      projectDir,
		DaemonStartTime: time.Now(),
		Ledger:          ledger,
		RedriveWriter:   writer,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RedriveCount != 0 {
		t.Errorf("RedriveCount = %d, want 0 (redrive failed)", result.RedriveCount)
	}
	if result.Retained != 1 {
		t.Errorf("Retained = %d, want 1", result.Retained)
	}
	// Intent file must still exist for Cat 3a.
	if _, statErr := os.Stat(intentPath); os.IsNotExist(statErr) {
		t.Errorf("intent file was unexpectedly deleted after failed redrive")
	}
	if len(writer.calls) != 1 {
		t.Errorf("writer.calls = %v, want 1 call", writer.calls)
	}
}

// TestGCRetiredIntentsWithRedrive_DivergedStatus verifies that when the bead is
// neither at the pre-state nor the post-state (torn write / external mutation),
// the intent file is retained for Cat 3a and the writer is NOT called (step 5).
func TestGCRetiredIntentsWithRedrive_DivergedStatus(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")

	beadID := core.BeadID("hk-rd-diverged")
	// close (in_progress→closed):
	//   pre-state  = in_progress
	//   post-state = closed
	//   bead is at "open" → neither pre- nor post-state → divergence → step 5 retain.
	//
	// gcIntentOpLanded(close, open, closed) = false (NOT landed: open != closed and open != tombstone).
	// gcIntentOpPreState(close) = in_progress.  in_progress != open → divergence → retain. ✓
	intentPath := gcIntentsFixtureWriteIntent(t, intentsDir, "proj_hk-rd-diverged_close_1", beadID,
		"close", "closed")

	ledger := &fakeIntentGCLedger{
		records: map[core.BeadID]core.BeadRecord{
			beadID: {BeadID: beadID, Status: core.CoarseStatusOpen},
		},
	}
	writer := &fakeRedriveWriter{
		succeed: map[core.BeadID]bool{},
		fail:    map[core.BeadID]bool{},
	}

	result, err := GCRetiredIntentsWithRedrive(context.Background(), GCRetiredIntentsConfig{
		ProjectDir:      projectDir,
		DaemonStartTime: time.Now(),
		Ledger:          ledger,
		RedriveWriter:   writer,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Retained != 1 {
		t.Errorf("Retained = %d, want 1 (diverged)", result.Retained)
	}
	if result.RedriveCount != 0 {
		t.Errorf("RedriveCount = %d, want 0 (diverged → no redrive)", result.RedriveCount)
	}
	if len(writer.calls) != 0 {
		t.Errorf("writer.calls = %v, want empty (diverged status must not trigger redrive)", writer.calls)
	}
	if _, statErr := os.Stat(intentPath); os.IsNotExist(statErr) {
		t.Errorf("intent file was unexpectedly deleted on divergence path")
	}
}

// TestGCRetiredIntentsWithRedrive_PreStateOps verifies that claim, close, and
// reopen each trigger a re-drive when their bead is at the op's pre-state.
//
// Note: reset (in_progress→open) is excluded because gcIntentOpLanded treats
// "bead at in_progress" as "landed" (reset ran + bead reclaimed), so the
// pre-state for reset never reaches the step-4 redrive path — it is treated as
// step-3 removal. This is intentional per the conservative landed heuristic.
func TestGCRetiredIntentsWithRedrive_PreStateOps(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		op        string
		postState string
		preStatus core.CoarseStatus
	}{
		// claim (open→in_progress): pre-state=open
		// gcIntentOpLanded(claim,open,in_progress)=false; preState=open == open → redrive
		{"claim", "claim", "in_progress", core.CoarseStatusOpen},
		// close (in_progress→closed): pre-state=in_progress
		// gcIntentOpLanded(close,in_progress,closed)=false; preState=in_progress == in_progress → redrive
		{"close", "close", "closed", core.CoarseStatusInProgress},
		// reopen (closed→open): pre-state=closed
		// gcIntentOpLanded(reopen,closed,open)=false; preState=closed == closed → redrive
		{"reopen", "reopen", "open", core.CoarseStatusClosed},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			projectDir := t.TempDir()
			intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
			beadID := core.BeadID("hk-rd-op-" + tc.name)
			key := "proj_" + string(beadID) + "_" + tc.op + "_1"

			gcIntentsFixtureWriteIntent(t, intentsDir, key, beadID,
				tc.op, tc.postState)

			ledger := &fakeIntentGCLedger{
				records: map[core.BeadID]core.BeadRecord{
					beadID: {BeadID: beadID, Status: tc.preStatus},
				},
			}
			writer := &fakeRedriveWriter{
				succeed: map[core.BeadID]bool{beadID: true},
				fail:    map[core.BeadID]bool{},
			}

			result, err := GCRetiredIntentsWithRedrive(context.Background(), GCRetiredIntentsConfig{
				ProjectDir:      projectDir,
				DaemonStartTime: time.Now(),
				Ledger:          ledger,
				RedriveWriter:   writer,
			})
			if err != nil {
				t.Fatalf("op=%s pre-state: unexpected error: %v", tc.op, err)
			}
			if result.RedriveCount != 1 {
				t.Errorf("op=%s pre-state: RedriveCount = %d, want 1", tc.op, result.RedriveCount)
			}
			if len(writer.calls) != 1 {
				t.Errorf("op=%s pre-state: writer.calls = %v, want 1 call", tc.op, writer.calls)
			}
		})
	}
}

// TestGCRetiredIntentsWithRedrive_MixedBatch verifies correct counters when a
// mix of landed, redriven, and retained (nil-writer) entries are processed.
func TestGCRetiredIntentsWithRedrive_MixedBatch(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")

	// 1. reopen (closed→open): bead at open → LANDED → removed.
	landedID := core.BeadID("hk-rdmix-landed")
	gcIntentsFixtureWriteIntent(t, intentsDir, "proj_hk-rdmix-landed_reopen_1", landedID,
		"reopen", "open")

	// 2. claim (open→in_progress): bead at open (pre-state) → re-drive succeeds.
	redriveID := core.BeadID("hk-rdmix-redrive")
	gcIntentsFixtureWriteIntent(t, intentsDir, "proj_hk-rdmix-redrive_claim_1", redriveID,
		"claim", "in_progress")

	// 3. close (in_progress→closed): bead at open → divergence → retained (step 5).
	retainedID := core.BeadID("hk-rdmix-retained")
	gcIntentsFixtureWriteIntent(t, intentsDir, "proj_hk-rdmix-retained_close_1", retainedID,
		"close", "closed")

	ledger := &fakeIntentGCLedger{
		records: map[core.BeadID]core.BeadRecord{
			landedID:   {BeadID: landedID, Status: core.CoarseStatusOpen},    // reopen landed (open==post-state)
			redriveID:  {BeadID: redriveID, Status: core.CoarseStatusOpen},   // claim pre-state
			retainedID: {BeadID: retainedID, Status: core.CoarseStatusOpen},  // close diverged (open≠pre-state=in_progress)
		},
	}
	writer := &fakeRedriveWriter{
		succeed: map[core.BeadID]bool{redriveID: true},
		fail:    map[core.BeadID]bool{},
	}

	result, err := GCRetiredIntentsWithRedrive(context.Background(), GCRetiredIntentsConfig{
		ProjectDir:      projectDir,
		DaemonStartTime: time.Now(),
		Ledger:          ledger,
		RedriveWriter:   writer,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Removed != 1 {
		t.Errorf("Removed = %d, want 1 (landed reopen)", result.Removed)
	}
	if result.RedriveCount != 1 {
		t.Errorf("RedriveCount = %d, want 1 (pre-state claim)", result.RedriveCount)
	}
	if result.Retained != 1 {
		t.Errorf("Retained = %d, want 1 (diverged close)", result.Retained)
	}
}
