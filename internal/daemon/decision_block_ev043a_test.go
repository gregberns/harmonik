package daemon

// decision_block_ev043a_test.go — unit tests for EV-043a: startup restoration
// of decision_required blocking state from persisted ack-state files.
//
// Spec ref: specs/event-model.md §4.12 EV-043, EV-043a.
// Bead ref: hk-pbmsq.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// writeAckFile writes a decisionAckRecord to acksDir/<ackToken>.
func writeAckFile(t *testing.T, acksDir string, rec decisionAckRecord) {
	t.Helper()
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("writeAckFile: marshal: %v", err)
	}
	if err := os.MkdirAll(acksDir, 0o755); err != nil {
		t.Fatalf("writeAckFile: mkdir: %v", err)
	}
	path := filepath.Join(acksDir, rec.AckToken)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writeAckFile: write %s: %v", path, err)
	}
}

// TestDecisionBlocker_BasicOps tests AddBeadBlock, IsBeadBlocked, and Acknowledge.
func TestDecisionBlocker_BasicOps(t *testing.T) {
	b := NewDecisionBlocker()
	bead := core.BeadID("hk-abc")

	if b.IsBeadBlocked(bead) {
		t.Fatal("expected bead to be unblocked initially")
	}

	b.AddBeadBlock(bead, "token1")
	if !b.IsBeadBlocked(bead) {
		t.Fatal("expected bead to be blocked after AddBeadBlock")
	}

	// Second token for the same bead — still blocked.
	b.AddBeadBlock(bead, "token2")
	if !b.IsBeadBlocked(bead) {
		t.Fatal("expected bead to remain blocked with two tokens")
	}

	// Acknowledge first token — still one pending.
	b.Acknowledge(decisionAckSubjectKindBead, string(bead), "token1")
	if !b.IsBeadBlocked(bead) {
		t.Fatal("expected bead to remain blocked after partial ack")
	}

	// Acknowledge second token — now fully unblocked.
	b.Acknowledge(decisionAckSubjectKindBead, string(bead), "token2")
	if b.IsBeadBlocked(bead) {
		t.Fatal("expected bead to be unblocked after all tokens acked")
	}
}

// TestDecisionBlocker_QueueBlock tests AddQueueBlock, IsQueueBlocked, and Acknowledge.
func TestDecisionBlocker_QueueBlock(t *testing.T) {
	b := NewDecisionBlocker()
	qid := "main"

	if b.IsQueueBlocked(qid) {
		t.Fatal("expected queue to be unblocked initially")
	}

	b.AddQueueBlock(qid, "qtok1")
	if !b.IsQueueBlocked(qid) {
		t.Fatal("expected queue to be blocked after AddQueueBlock")
	}

	b.Acknowledge(decisionAckSubjectKindQueue, qid, "qtok1")
	if b.IsQueueBlocked(qid) {
		t.Fatal("expected queue to be unblocked after ack")
	}
}

// TestDecisionBlocker_AcknowledgeUnknownToken is a no-op safety test.
func TestDecisionBlocker_AcknowledgeUnknownToken(t *testing.T) {
	b := NewDecisionBlocker()
	// Calling Acknowledge for a token that was never added must not panic.
	b.Acknowledge(decisionAckSubjectKindBead, "hk-xyz", "nonexistent")
}

// TestDecisionBlocker_IdempotentAdd checks that adding the same token twice
// does not inflate the pending set.
func TestDecisionBlocker_IdempotentAdd(t *testing.T) {
	b := NewDecisionBlocker()
	bead := core.BeadID("hk-idem")
	b.AddBeadBlock(bead, "tok")
	b.AddBeadBlock(bead, "tok")
	b.Acknowledge(decisionAckSubjectKindBead, string(bead), "tok")
	if b.IsBeadBlocked(bead) {
		t.Fatal("expected bead unblocked after ack of idempotently-added token")
	}
}

// TestLoadDecisionAckState_DirAbsent verifies that a missing decision_acks
// directory is treated as no pending decisions (no-op).
func TestLoadDecisionAckState_DirAbsent(t *testing.T) {
	tmp := t.TempDir()
	b := NewDecisionBlocker()
	// .harmonik/decision_acks does not exist.
	if err := LoadDecisionAckState(context.Background(), tmp, b); err != nil {
		t.Fatalf("unexpected error for absent dir: %v", err)
	}
}

// TestLoadDecisionAckState_PendingBead verifies that a pending bead ack file
// results in the bead being blocked after startup load.
func TestLoadDecisionAckState_PendingBead(t *testing.T) {
	tmp := t.TempDir()
	acksDir := filepath.Join(tmp, ".harmonik", "decision_acks")

	writeAckFile(t, acksDir, decisionAckRecord{
		SchemaVersion: 1,
		AckToken:      "tok-bead-001",
		Status:        decisionAckStatusPending,
		SubjectKind:   decisionAckSubjectKindBead,
		SubjectID:     "hk-abc",
	})

	b := NewDecisionBlocker()
	if err := LoadDecisionAckState(context.Background(), tmp, b); err != nil {
		t.Fatalf("LoadDecisionAckState: %v", err)
	}

	if !b.IsBeadBlocked("hk-abc") {
		t.Fatal("expected bead hk-abc to be blocked after loading pending ack")
	}
}

// TestLoadDecisionAckState_AcknowledgedBeadNotBlocked verifies that ack files
// with status=acknowledged do NOT restore blocking state.
func TestLoadDecisionAckState_AcknowledgedBeadNotBlocked(t *testing.T) {
	tmp := t.TempDir()
	acksDir := filepath.Join(tmp, ".harmonik", "decision_acks")

	writeAckFile(t, acksDir, decisionAckRecord{
		SchemaVersion: 1,
		AckToken:      "tok-acked",
		Status:        decisionAckStatusAcknowledged,
		SubjectKind:   decisionAckSubjectKindBead,
		SubjectID:     "hk-done",
	})

	b := NewDecisionBlocker()
	if err := LoadDecisionAckState(context.Background(), tmp, b); err != nil {
		t.Fatalf("LoadDecisionAckState: %v", err)
	}

	if b.IsBeadBlocked("hk-done") {
		t.Fatal("acknowledged bead must not be blocked")
	}
}

// TestLoadDecisionAckState_PendingQueue verifies that a pending queue ack file
// results in the queue being blocked after startup load.
func TestLoadDecisionAckState_PendingQueue(t *testing.T) {
	tmp := t.TempDir()
	acksDir := filepath.Join(tmp, ".harmonik", "decision_acks")

	writeAckFile(t, acksDir, decisionAckRecord{
		SchemaVersion: 1,
		AckToken:      "tok-queue-001",
		Status:        decisionAckStatusPending,
		SubjectKind:   decisionAckSubjectKindQueue,
		SubjectID:     "main",
	})

	b := NewDecisionBlocker()
	if err := LoadDecisionAckState(context.Background(), tmp, b); err != nil {
		t.Fatalf("LoadDecisionAckState: %v", err)
	}

	if !b.IsQueueBlocked("main") {
		t.Fatal("expected queue 'main' to be blocked after loading pending ack")
	}
}

// TestLoadDecisionAckState_MixedFiles verifies correct handling when some
// files are pending and others are acknowledged.
func TestLoadDecisionAckState_MixedFiles(t *testing.T) {
	tmp := t.TempDir()
	acksDir := filepath.Join(tmp, ".harmonik", "decision_acks")

	writeAckFile(t, acksDir, decisionAckRecord{
		SchemaVersion: 1,
		AckToken:      "tok-pend",
		Status:        decisionAckStatusPending,
		SubjectKind:   decisionAckSubjectKindBead,
		SubjectID:     "hk-blocked",
	})
	writeAckFile(t, acksDir, decisionAckRecord{
		SchemaVersion: 1,
		AckToken:      "tok-ack",
		Status:        decisionAckStatusAcknowledged,
		SubjectKind:   decisionAckSubjectKindBead,
		SubjectID:     "hk-free",
	})

	b := NewDecisionBlocker()
	if err := LoadDecisionAckState(context.Background(), tmp, b); err != nil {
		t.Fatalf("LoadDecisionAckState: %v", err)
	}

	if !b.IsBeadBlocked("hk-blocked") {
		t.Fatal("expected hk-blocked to be blocked")
	}
	if b.IsBeadBlocked("hk-free") {
		t.Fatal("expected hk-free to be unblocked")
	}
}

// TestLoadDecisionAckState_CorruptFileSkipped verifies that a file that fails
// JSON parse is skipped (non-fatal) and other files are still loaded.
func TestLoadDecisionAckState_CorruptFileSkipped(t *testing.T) {
	tmp := t.TempDir()
	acksDir := filepath.Join(tmp, ".harmonik", "decision_acks")

	if err := os.MkdirAll(acksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write an invalid JSON file.
	if err := os.WriteFile(filepath.Join(acksDir, "bad-tok"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Write a valid pending file.
	writeAckFile(t, acksDir, decisionAckRecord{
		SchemaVersion: 1,
		AckToken:      "good-tok",
		Status:        decisionAckStatusPending,
		SubjectKind:   decisionAckSubjectKindBead,
		SubjectID:     "hk-ok",
	})

	b := NewDecisionBlocker()
	// Should not return an error even though one file is corrupt.
	if err := LoadDecisionAckState(context.Background(), tmp, b); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The valid file's bead should still be blocked.
	if !b.IsBeadBlocked("hk-ok") {
		t.Fatal("expected hk-ok to be blocked despite corrupt sibling file")
	}
}

// TestLoadDecisionAckState_NewerSchemaSkipped verifies that a file with a
// schema_version newer than this binary supports is skipped non-fatally.
func TestLoadDecisionAckState_NewerSchemaSkipped(t *testing.T) {
	tmp := t.TempDir()
	acksDir := filepath.Join(tmp, ".harmonik", "decision_acks")

	writeAckFile(t, acksDir, decisionAckRecord{
		SchemaVersion: decisionAckSchemaVersion + 1, // future version
		AckToken:      "future-tok",
		Status:        decisionAckStatusPending,
		SubjectKind:   decisionAckSubjectKindBead,
		SubjectID:     "hk-future",
	})

	b := NewDecisionBlocker()
	if err := LoadDecisionAckState(context.Background(), tmp, b); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The bead must NOT be blocked — the file was skipped.
	if b.IsBeadBlocked("hk-future") {
		t.Fatal("newer-schema file should be skipped; bead must not be blocked")
	}
}

// TestLoadDecisionAckState_MultiplePendingTokensSameBead verifies that two
// pending ack files for the same bead both need to be acknowledged before the
// bead is unblocked.
func TestLoadDecisionAckState_MultiplePendingTokensSameBead(t *testing.T) {
	tmp := t.TempDir()
	acksDir := filepath.Join(tmp, ".harmonik", "decision_acks")

	for _, tok := range []string{"tok-a", "tok-b"} {
		writeAckFile(t, acksDir, decisionAckRecord{
			SchemaVersion: 1,
			AckToken:      tok,
			Status:        decisionAckStatusPending,
			SubjectKind:   decisionAckSubjectKindBead,
			SubjectID:     "hk-multi",
		})
	}

	b := NewDecisionBlocker()
	if err := LoadDecisionAckState(context.Background(), tmp, b); err != nil {
		t.Fatalf("LoadDecisionAckState: %v", err)
	}

	if !b.IsBeadBlocked("hk-multi") {
		t.Fatal("expected hk-multi to be blocked by two pending tokens")
	}

	b.Acknowledge(decisionAckSubjectKindBead, "hk-multi", "tok-a")
	if !b.IsBeadBlocked("hk-multi") {
		t.Fatal("expected hk-multi to remain blocked after one ack")
	}

	b.Acknowledge(decisionAckSubjectKindBead, "hk-multi", "tok-b")
	if b.IsBeadBlocked("hk-multi") {
		t.Fatal("expected hk-multi to be unblocked after both tokens acked")
	}
}
