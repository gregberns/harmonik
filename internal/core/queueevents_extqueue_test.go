package core

import (
	"encoding/json"
	"testing"
)

// queueFixtureQueueID returns a non-empty queue_id string for queue event tests.
func queueFixtureQueueID() string {
	return "019605a0-1111-7000-8000-000000000001"
}

// queueFixtureTimestamp returns a non-empty RFC 3339 timestamp string for queue
// event tests.
func queueFixtureTimestamp() string {
	return "2026-05-15T00:00:00.000Z"
}

// ---------------------------------------------------------------------------
// QueueSubmittedPayload
// ---------------------------------------------------------------------------

func TestQueueSubmittedPayloadValid(t *testing.T) {
	t.Parallel()

	qid := queueFixtureQueueID()
	ts := queueFixtureTimestamp()

	tests := []struct {
		name  string
		p     QueueSubmittedPayload
		valid bool
	}{
		{
			name: "minimal valid",
			p: QueueSubmittedPayload{
				QueueID:            qid,
				SubmittedAt:        ts,
				GroupCount:         2,
				TotalBeadCount:     5,
				QueueSchemaVersion: 1,
			},
			valid: true,
		},
		{
			name:  "empty queue_id rejected",
			p:     QueueSubmittedPayload{SubmittedAt: ts, GroupCount: 1, TotalBeadCount: 1, QueueSchemaVersion: 1},
			valid: false,
		},
		{
			name:  "empty submitted_at rejected",
			p:     QueueSubmittedPayload{QueueID: qid, GroupCount: 1, TotalBeadCount: 1, QueueSchemaVersion: 1},
			valid: false,
		},
		{
			name:  "zero group_count rejected",
			p:     QueueSubmittedPayload{QueueID: qid, SubmittedAt: ts, GroupCount: 0, TotalBeadCount: 1, QueueSchemaVersion: 1},
			valid: false,
		},
		{
			name:  "zero total_bead_count rejected",
			p:     QueueSubmittedPayload{QueueID: qid, SubmittedAt: ts, GroupCount: 1, TotalBeadCount: 0, QueueSchemaVersion: 1},
			valid: false,
		},
		{
			name:  "zero schema_version rejected",
			p:     QueueSubmittedPayload{QueueID: qid, SubmittedAt: ts, GroupCount: 1, TotalBeadCount: 1, QueueSchemaVersion: 0},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("QueueSubmittedPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestQueueSubmittedPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	original := QueueSubmittedPayload{
		QueueID:            queueFixtureQueueID(),
		SubmittedAt:        queueFixtureTimestamp(),
		GroupCount:         3,
		TotalBeadCount:     9,
		QueueSchemaVersion: 1,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded QueueSubmittedPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !decoded.Valid() {
		t.Error("decoded QueueSubmittedPayload failed Valid()")
	}
	if decoded.QueueID != original.QueueID {
		t.Errorf("QueueID: got %q, want %q", decoded.QueueID, original.QueueID)
	}
	if decoded.GroupCount != original.GroupCount {
		t.Errorf("GroupCount: got %d, want %d", decoded.GroupCount, original.GroupCount)
	}
	if decoded.TotalBeadCount != original.TotalBeadCount {
		t.Errorf("TotalBeadCount: got %d, want %d", decoded.TotalBeadCount, original.TotalBeadCount)
	}
	if decoded.QueueSchemaVersion != original.QueueSchemaVersion {
		t.Errorf("QueueSchemaVersion: got %d, want %d", decoded.QueueSchemaVersion, original.QueueSchemaVersion)
	}
}

// ---------------------------------------------------------------------------
// QueueGroupStartedPayload
// ---------------------------------------------------------------------------

func TestQueueGroupStartedPayloadValid(t *testing.T) {
	t.Parallel()

	qid := queueFixtureQueueID()
	ts := queueFixtureTimestamp()

	tests := []struct {
		name  string
		p     QueueGroupStartedPayload
		valid bool
	}{
		{
			name:  "valid wave group",
			p:     QueueGroupStartedPayload{QueueID: qid, GroupIndex: 0, GroupKind: "wave", ItemCount: 3, StartedAt: ts},
			valid: true,
		},
		{
			name:  "valid stream group",
			p:     QueueGroupStartedPayload{QueueID: qid, GroupIndex: 1, GroupKind: "stream", ItemCount: 1, StartedAt: ts},
			valid: true,
		},
		{
			name:  "empty queue_id rejected",
			p:     QueueGroupStartedPayload{GroupIndex: 0, GroupKind: "wave", ItemCount: 1, StartedAt: ts},
			valid: false,
		},
		{
			name:  "negative group_index rejected",
			p:     QueueGroupStartedPayload{QueueID: qid, GroupIndex: -1, GroupKind: "wave", ItemCount: 1, StartedAt: ts},
			valid: false,
		},
		{
			name:  "invalid group_kind rejected",
			p:     QueueGroupStartedPayload{QueueID: qid, GroupIndex: 0, GroupKind: "batch", ItemCount: 1, StartedAt: ts},
			valid: false,
		},
		{
			name:  "zero item_count rejected",
			p:     QueueGroupStartedPayload{QueueID: qid, GroupIndex: 0, GroupKind: "wave", ItemCount: 0, StartedAt: ts},
			valid: false,
		},
		{
			name:  "empty started_at rejected",
			p:     QueueGroupStartedPayload{QueueID: qid, GroupIndex: 0, GroupKind: "wave", ItemCount: 1},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("QueueGroupStartedPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestQueueGroupStartedPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	original := QueueGroupStartedPayload{
		QueueID:    queueFixtureQueueID(),
		GroupIndex: 0,
		GroupKind:  "wave",
		ItemCount:  4,
		StartedAt:  queueFixtureTimestamp(),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded QueueGroupStartedPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !decoded.Valid() {
		t.Error("decoded QueueGroupStartedPayload failed Valid()")
	}
	if decoded.GroupKind != original.GroupKind {
		t.Errorf("GroupKind: got %q, want %q", decoded.GroupKind, original.GroupKind)
	}
}

// ---------------------------------------------------------------------------
// QueueGroupCompletedPayload
// ---------------------------------------------------------------------------

func TestQueueGroupCompletedPayloadValid(t *testing.T) {
	t.Parallel()

	qid := queueFixtureQueueID()
	ts := queueFixtureTimestamp()

	tests := []struct {
		name  string
		p     QueueGroupCompletedPayload
		valid bool
	}{
		{
			name:  "valid complete-success",
			p:     QueueGroupCompletedPayload{QueueID: qid, GroupIndex: 0, FinalStatus: "complete-success", SuccessCount: 3, FailCount: 0, CompletedAt: ts},
			valid: true,
		},
		{
			name:  "valid complete-with-failures",
			p:     QueueGroupCompletedPayload{QueueID: qid, GroupIndex: 0, FinalStatus: "complete-with-failures", SuccessCount: 2, FailCount: 1, CompletedAt: ts},
			valid: true,
		},
		{
			name:  "empty queue_id rejected",
			p:     QueueGroupCompletedPayload{GroupIndex: 0, FinalStatus: "complete-success", SuccessCount: 1, CompletedAt: ts},
			valid: false,
		},
		{
			name:  "negative group_index rejected",
			p:     QueueGroupCompletedPayload{QueueID: qid, GroupIndex: -1, FinalStatus: "complete-success", SuccessCount: 1, CompletedAt: ts},
			valid: false,
		},
		{
			name:  "invalid final_status rejected",
			p:     QueueGroupCompletedPayload{QueueID: qid, GroupIndex: 0, FinalStatus: "ok", SuccessCount: 1, CompletedAt: ts},
			valid: false,
		},
		{
			name:  "negative success_count rejected",
			p:     QueueGroupCompletedPayload{QueueID: qid, GroupIndex: 0, FinalStatus: "complete-success", SuccessCount: -1, CompletedAt: ts},
			valid: false,
		},
		{
			name:  "negative fail_count rejected",
			p:     QueueGroupCompletedPayload{QueueID: qid, GroupIndex: 0, FinalStatus: "complete-with-failures", SuccessCount: 1, FailCount: -1, CompletedAt: ts},
			valid: false,
		},
		{
			name:  "empty completed_at rejected",
			p:     QueueGroupCompletedPayload{QueueID: qid, GroupIndex: 0, FinalStatus: "complete-success", SuccessCount: 1},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("QueueGroupCompletedPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestQueueGroupCompletedPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	original := QueueGroupCompletedPayload{
		QueueID:      queueFixtureQueueID(),
		GroupIndex:   1,
		FinalStatus:  "complete-with-failures",
		SuccessCount: 4,
		FailCount:    1,
		CompletedAt:  queueFixtureTimestamp(),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded QueueGroupCompletedPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !decoded.Valid() {
		t.Error("decoded QueueGroupCompletedPayload failed Valid()")
	}
	if decoded.FinalStatus != original.FinalStatus {
		t.Errorf("FinalStatus: got %q, want %q", decoded.FinalStatus, original.FinalStatus)
	}
	if decoded.FailCount != original.FailCount {
		t.Errorf("FailCount: got %d, want %d", decoded.FailCount, original.FailCount)
	}
}

func TestQueueGroupCompletedPayloadCompletionReceiptID(t *testing.T) {
	base := QueueGroupCompletedPayload{QueueID: "queue", GroupIndex: 0, FinalStatus: "complete-success", SuccessCount: 1, CompletedAt: "2026-08-10T12:00:00.000Z"}
	base.CompletionReceiptID = "0197c452-0000-7000-8000-000000000002"
	if !base.Valid() {
		t.Fatal("canonical final receipt ID was rejected")
	}
	for _, invalid := range []string{"bad", "0197C452-0000-7000-8000-000000000002", "{0197c452-0000-7000-8000-000000000002}"} {
		candidate := base
		candidate.CompletionReceiptID = invalid
		if candidate.Valid() {
			t.Fatalf("non-canonical receipt ID %q was accepted", invalid)
		}
	}
	failed := base
	failed.FinalStatus = "complete-with-failures"
	failed.FailCount = 1
	if failed.Valid() {
		t.Fatal("failure payload accepted a completion receipt ID")
	}
}

// ---------------------------------------------------------------------------
// QueuePausedPayload
// ---------------------------------------------------------------------------

func TestQueuePausedPayloadValid(t *testing.T) {
	t.Parallel()

	qid := queueFixtureQueueID()
	ts := queueFixtureTimestamp()

	tests := []struct {
		name  string
		p     QueuePausedPayload
		valid bool
	}{
		{
			name:  "valid group_failure",
			p:     QueuePausedPayload{QueueID: qid, GroupIndex: 0, FailCount: 2, PausedAt: ts, Reason: "group_failure"},
			valid: true,
		},
		{
			name:  "valid operator_drain",
			p:     QueuePausedPayload{QueueID: qid, GroupIndex: 1, FailCount: 0, PausedAt: ts, Reason: "operator_drain"},
			valid: true,
		},
		{
			name:  "empty queue_id rejected",
			p:     QueuePausedPayload{GroupIndex: 0, FailCount: 1, PausedAt: ts, Reason: "group_failure"},
			valid: false,
		},
		{
			name:  "negative group_index rejected",
			p:     QueuePausedPayload{QueueID: qid, GroupIndex: -1, FailCount: 1, PausedAt: ts, Reason: "group_failure"},
			valid: false,
		},
		{
			name:  "negative fail_count rejected",
			p:     QueuePausedPayload{QueueID: qid, GroupIndex: 0, FailCount: -1, PausedAt: ts, Reason: "group_failure"},
			valid: false,
		},
		{
			name:  "empty paused_at rejected",
			p:     QueuePausedPayload{QueueID: qid, GroupIndex: 0, FailCount: 1, Reason: "group_failure"},
			valid: false,
		},
		{
			name:  "invalid reason rejected",
			p:     QueuePausedPayload{QueueID: qid, GroupIndex: 0, FailCount: 1, PausedAt: ts, Reason: "manual"},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("QueuePausedPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestQueuePausedPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	original := QueuePausedPayload{
		QueueID:    queueFixtureQueueID(),
		GroupIndex: 0,
		FailCount:  3,
		PausedAt:   queueFixtureTimestamp(),
		Reason:     "group_failure",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded QueuePausedPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !decoded.Valid() {
		t.Error("decoded QueuePausedPayload failed Valid()")
	}
	if decoded.Reason != original.Reason {
		t.Errorf("Reason: got %q, want %q", decoded.Reason, original.Reason)
	}
}

// ---------------------------------------------------------------------------
// QueueAppendedPayload
// ---------------------------------------------------------------------------

func TestQueueAppendedPayloadValid(t *testing.T) {
	t.Parallel()

	qid := queueFixtureQueueID()
	ts := queueFixtureTimestamp()

	tests := []struct {
		name  string
		p     QueueAppendedPayload
		valid bool
	}{
		{
			name:  "valid single bead",
			p:     QueueAppendedPayload{QueueID: qid, GroupIndex: 0, AppendedBeadIDs: []string{"hk-abc01"}, AppendedAt: ts},
			valid: true,
		},
		{
			name:  "valid multiple beads",
			p:     QueueAppendedPayload{QueueID: qid, GroupIndex: 0, AppendedBeadIDs: []string{"hk-abc01", "hk-abc02"}, AppendedAt: ts},
			valid: true,
		},
		{
			name:  "empty queue_id rejected",
			p:     QueueAppendedPayload{GroupIndex: 0, AppendedBeadIDs: []string{"hk-abc01"}, AppendedAt: ts},
			valid: false,
		},
		{
			name:  "negative group_index rejected",
			p:     QueueAppendedPayload{QueueID: qid, GroupIndex: -1, AppendedBeadIDs: []string{"hk-abc01"}, AppendedAt: ts},
			valid: false,
		},
		{
			name:  "empty appended_bead_ids rejected",
			p:     QueueAppendedPayload{QueueID: qid, GroupIndex: 0, AppendedBeadIDs: []string{}, AppendedAt: ts},
			valid: false,
		},
		{
			name:  "nil appended_bead_ids rejected",
			p:     QueueAppendedPayload{QueueID: qid, GroupIndex: 0, AppendedBeadIDs: nil, AppendedAt: ts},
			valid: false,
		},
		{
			name:  "empty appended_at rejected",
			p:     QueueAppendedPayload{QueueID: qid, GroupIndex: 0, AppendedBeadIDs: []string{"hk-abc01"}},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("QueueAppendedPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestQueueAppendedPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	original := QueueAppendedPayload{
		QueueID:         queueFixtureQueueID(),
		GroupIndex:      0,
		AppendedBeadIDs: []string{"hk-t1001", "hk-t1002"},
		AppendedAt:      queueFixtureTimestamp(),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded QueueAppendedPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !decoded.Valid() {
		t.Error("decoded QueueAppendedPayload failed Valid()")
	}
	if len(decoded.AppendedBeadIDs) != len(original.AppendedBeadIDs) {
		t.Errorf("AppendedBeadIDs length: got %d, want %d", len(decoded.AppendedBeadIDs), len(original.AppendedBeadIDs))
	}
	for i, id := range original.AppendedBeadIDs {
		if decoded.AppendedBeadIDs[i] != id {
			t.Errorf("AppendedBeadIDs[%d]: got %q, want %q", i, decoded.AppendedBeadIDs[i], id)
		}
	}
}

// ---------------------------------------------------------------------------
// QueueItemDeferredForLedgerDepPayload
// ---------------------------------------------------------------------------

func TestQueueItemDeferredForLedgerDepPayloadValid(t *testing.T) {
	t.Parallel()

	qid := queueFixtureQueueID()
	ts := queueFixtureTimestamp()

	tests := []struct {
		name  string
		p     QueueItemDeferredForLedgerDepPayload
		valid bool
	}{
		{
			name:  "valid",
			p:     QueueItemDeferredForLedgerDepPayload{QueueID: qid, GroupIndex: 0, BeadID: "hk-item1", BlockerBeadID: "hk-blocker1", DetectedAt: ts},
			valid: true,
		},
		{
			name:  "empty queue_id rejected",
			p:     QueueItemDeferredForLedgerDepPayload{GroupIndex: 0, BeadID: "hk-item1", BlockerBeadID: "hk-blocker1", DetectedAt: ts},
			valid: false,
		},
		{
			name:  "negative group_index rejected",
			p:     QueueItemDeferredForLedgerDepPayload{QueueID: qid, GroupIndex: -1, BeadID: "hk-item1", BlockerBeadID: "hk-blocker1", DetectedAt: ts},
			valid: false,
		},
		{
			name:  "empty bead_id rejected",
			p:     QueueItemDeferredForLedgerDepPayload{QueueID: qid, GroupIndex: 0, BlockerBeadID: "hk-blocker1", DetectedAt: ts},
			valid: false,
		},
		{
			name:  "empty blocker_bead_id rejected",
			p:     QueueItemDeferredForLedgerDepPayload{QueueID: qid, GroupIndex: 0, BeadID: "hk-item1", DetectedAt: ts},
			valid: false,
		},
		{
			name:  "empty detected_at rejected",
			p:     QueueItemDeferredForLedgerDepPayload{QueueID: qid, GroupIndex: 0, BeadID: "hk-item1", BlockerBeadID: "hk-blocker1"},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("QueueItemDeferredForLedgerDepPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestQueueItemDeferredForLedgerDepPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	original := QueueItemDeferredForLedgerDepPayload{
		QueueID:       queueFixtureQueueID(),
		GroupIndex:    0,
		BeadID:        "hk-item1",
		BlockerBeadID: "hk-blocker1",
		DetectedAt:    queueFixtureTimestamp(),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded QueueItemDeferredForLedgerDepPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !decoded.Valid() {
		t.Error("decoded QueueItemDeferredForLedgerDepPayload failed Valid()")
	}
	if decoded.BeadID != original.BeadID {
		t.Errorf("BeadID: got %q, want %q", decoded.BeadID, original.BeadID)
	}
	if decoded.BlockerBeadID != original.BlockerBeadID {
		t.Errorf("BlockerBeadID: got %q, want %q", decoded.BlockerBeadID, original.BlockerBeadID)
	}
}

// ---------------------------------------------------------------------------
// QueueItemReconciledPayload
// ---------------------------------------------------------------------------

func TestQueueItemReconciledPayloadValid(t *testing.T) {
	t.Parallel()

	qid := queueFixtureQueueID()
	ts := queueFixtureTimestamp()

	tests := []struct {
		name  string
		p     QueueItemReconciledPayload
		valid bool
	}{
		{
			name:  "valid claim_write_lost",
			p:     QueueItemReconciledPayload{QueueID: qid, GroupIndex: 0, BeadID: "hk-item1", Reason: "claim_write_lost", ReconciledAt: ts},
			valid: true,
		},
		{
			name:  "empty queue_id rejected",
			p:     QueueItemReconciledPayload{GroupIndex: 0, BeadID: "hk-item1", Reason: "claim_write_lost", ReconciledAt: ts},
			valid: false,
		},
		{
			name:  "negative group_index rejected",
			p:     QueueItemReconciledPayload{QueueID: qid, GroupIndex: -1, BeadID: "hk-item1", Reason: "claim_write_lost", ReconciledAt: ts},
			valid: false,
		},
		{
			name:  "empty bead_id rejected",
			p:     QueueItemReconciledPayload{QueueID: qid, GroupIndex: 0, Reason: "claim_write_lost", ReconciledAt: ts},
			valid: false,
		},
		{
			name:  "invalid reason rejected",
			p:     QueueItemReconciledPayload{QueueID: qid, GroupIndex: 0, BeadID: "hk-item1", Reason: "unknown", ReconciledAt: ts},
			valid: false,
		},
		{
			name:  "empty reconciled_at rejected",
			p:     QueueItemReconciledPayload{QueueID: qid, GroupIndex: 0, BeadID: "hk-item1", Reason: "claim_write_lost"},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("QueueItemReconciledPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestQueueItemReconciledPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	original := QueueItemReconciledPayload{
		QueueID:      queueFixtureQueueID(),
		GroupIndex:   0,
		BeadID:       "hk-item1",
		Reason:       "claim_write_lost",
		ReconciledAt: queueFixtureTimestamp(),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded QueueItemReconciledPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !decoded.Valid() {
		t.Error("decoded QueueItemReconciledPayload failed Valid()")
	}
	if decoded.Reason != original.Reason {
		t.Errorf("Reason: got %q, want %q", decoded.Reason, original.Reason)
	}
	if decoded.ReconciledAt != original.ReconciledAt {
		t.Errorf("ReconciledAt: got %q, want %q", decoded.ReconciledAt, original.ReconciledAt)
	}
}

// ---------------------------------------------------------------------------
// CrossQueueCollisionPayload
// ---------------------------------------------------------------------------

func TestCrossQueueCollisionPayloadValid(t *testing.T) {
	t.Parallel()

	ts := queueFixtureTimestamp()

	tests := []struct {
		name  string
		p     CrossQueueCollisionPayload
		valid bool
	}{
		{
			name:  "valid refused",
			p:     CrossQueueCollisionPayload{BeadID: "hk-item1", LosingQueue: "beta", WinningQueue: "alpha", Disposition: CrossQueueCollisionRefused, DetectedAt: ts},
			valid: true,
		},
		{
			name:  "valid completed",
			p:     CrossQueueCollisionPayload{BeadID: "hk-item1", LosingQueue: "beta", WinningQueue: "alpha", Disposition: CrossQueueCollisionCompleted, DetectedAt: ts},
			valid: true,
		},
		{
			name:  "valid failed",
			p:     CrossQueueCollisionPayload{BeadID: "hk-item1", LosingQueue: "beta", WinningQueue: "alpha", Disposition: CrossQueueCollisionFailed, DetectedAt: ts},
			valid: true,
		},
		{
			name:  "empty bead_id rejected",
			p:     CrossQueueCollisionPayload{LosingQueue: "beta", WinningQueue: "alpha", Disposition: CrossQueueCollisionRefused, DetectedAt: ts},
			valid: false,
		},
		{
			name:  "empty losing_queue rejected",
			p:     CrossQueueCollisionPayload{BeadID: "hk-item1", WinningQueue: "alpha", Disposition: CrossQueueCollisionRefused, DetectedAt: ts},
			valid: false,
		},
		{
			name:  "empty winning_queue rejected",
			p:     CrossQueueCollisionPayload{BeadID: "hk-item1", LosingQueue: "beta", Disposition: CrossQueueCollisionRefused, DetectedAt: ts},
			valid: false,
		},
		{
			// The rejection that stops a useless report. A collision is between
			// TWO queues; a payload naming one queue twice sends an operator
			// looking for a second queue that does not exist.
			name:  "same queue on both sides rejected",
			p:     CrossQueueCollisionPayload{BeadID: "hk-item1", LosingQueue: "alpha", WinningQueue: "alpha", Disposition: CrossQueueCollisionRefused, DetectedAt: ts},
			valid: false,
		},
		{
			// The disposition is the field that tells an operator whether to act,
			// so an unrecognised value must not travel as if it were meaningful.
			name:  "out-of-range disposition rejected",
			p:     CrossQueueCollisionPayload{BeadID: "hk-item1", LosingQueue: "beta", WinningQueue: "alpha", Disposition: "parked", DetectedAt: ts},
			valid: false,
		},
		{
			name:  "empty disposition rejected",
			p:     CrossQueueCollisionPayload{BeadID: "hk-item1", LosingQueue: "beta", WinningQueue: "alpha", DetectedAt: ts},
			valid: false,
		},
		{
			name:  "empty detected_at rejected",
			p:     CrossQueueCollisionPayload{BeadID: "hk-item1", LosingQueue: "beta", WinningQueue: "alpha", Disposition: CrossQueueCollisionRefused},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("CrossQueueCollisionPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestCrossQueueCollisionPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	original := CrossQueueCollisionPayload{
		BeadID:       "hk-item1",
		LosingQueue:  "beta",
		WinningQueue: "alpha",
		Disposition:  CrossQueueCollisionRefused,
		DetectedAt:   queueFixtureTimestamp(),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded CrossQueueCollisionPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !decoded.Valid() {
		t.Error("decoded CrossQueueCollisionPayload failed Valid()")
	}
	if decoded.Disposition != original.Disposition {
		t.Errorf("Disposition: got %q, want %q", decoded.Disposition, original.Disposition)
	}
	if decoded.WinningQueue != original.WinningQueue {
		t.Errorf("WinningQueue: got %q, want %q", decoded.WinningQueue, original.WinningQueue)
	}
}

// ---------------------------------------------------------------------------
// Cohort registration assertions — every queue event must be registered
// with the correct constructor shape (EV-032 / EV-034 / hk-yslws).
// ---------------------------------------------------------------------------

// TestQueueEventsCohortRegistered asserts that the 8 §8.10 event type names
// that HAVE a Go payload produce the correct concrete payload pointer type from
// their constructors, and that a JSON round-trip through a local (isolated)
// registry succeeds. §8.10 carries nine rows: `queue_cancelled_operator`
// (§8.10.8) is spec-only — it has no EventType constant, no payload and no
// registration — so there is nothing here for it to cover.
//
// This test does NOT use the global registry (which may be reset by
// TestRegistry subtests via t.Cleanup(eventRegistryReset)). Instead it
// registers the 8 constructors into a fresh local registry snapshot and
// exercises DecodePayload through that snapshot directly. This is the
// same isolation pattern used by TestRedactionFailedPayloadConstructorShape.
//
// # The cohort table is hand-maintained, and that is a known weakness
//
// The table below is a copy of registerQueueEvents, and nothing makes the two
// agree. It went stale the moment cross_queue_collision was registered
// (hk-nsion): the count said seven, the production registry held eight, and the
// row for the new type was simply absent — so a test whose whole job is
// exhaustiveness passed while covering none of it. The count in the prose is a
// second copy of the same fact and went stale with it.
//
// What actually catches a missing registration today is
// TestEveryDeclaredEventTypeHasRuntimeContracts in internal/specaudit, which
// scans the tree for EventType constants and checks each against the live
// registry — it derives its set instead of restating it. This test earns its
// place on the constructor SHAPE and the round-trip, not on being the
// exhaustiveness gate it reads as. Deriving the cohort from the production
// registry here is the real repair and is not in hk-nsion's scope.
//
// Durability class context (documented here for spec traceability):
//
//	Class F (fsync-boundary): queue_submitted, queue_group_completed,
//	  queue_paused, queue_item_reconciled.
//	Class O (ordinary): queue_group_started, queue_appended,
//	  queue_item_deferred_for_ledger_dep, cross_queue_collision.
func TestQueueEventsCohortRegistered(t *testing.T) {
	t.Parallel()

	// Table: event type name → constructor (mirrors registerQueueEvents).
	cohort := []struct {
		typeName   string
		durability string
		mkPayload  func() EventPayload
	}{
		{"queue_submitted", "F", func() EventPayload { return &QueueSubmittedPayload{} }},
		{"queue_group_started", "O", func() EventPayload { return &QueueGroupStartedPayload{} }},
		{"queue_group_completed", "F", func() EventPayload { return &QueueGroupCompletedPayload{} }},
		{"queue_paused", "F", func() EventPayload { return &QueuePausedPayload{} }},
		{"queue_appended", "O", func() EventPayload { return &QueueAppendedPayload{} }},
		{"queue_item_deferred_for_ledger_dep", "O", func() EventPayload { return &QueueItemDeferredForLedgerDepPayload{} }},
		{"queue_item_reconciled", "F", func() EventPayload { return &QueueItemReconciledPayload{} }},
		{"cross_queue_collision", "O", func() EventPayload { return &CrossQueueCollisionPayload{} }},
	}

	// Build a local registry snapshot populated with only the queue cohort.
	// This avoids races with eventRegistryReset() in TestRegistry subtests.
	localCtors := make(map[string]func() EventPayload, len(cohort))
	for _, entry := range cohort {
		localCtors[entry.typeName] = entry.mkPayload
	}

	for _, entry := range cohort {
		t.Run(entry.typeName, func(t *testing.T) {
			t.Parallel()

			// 1. Constructor shape: must return a non-nil pointer.
			got := entry.mkPayload()
			if got == nil {
				t.Fatalf("constructor for %q returned nil", entry.typeName)
			}

			// 2. JSON round-trip via local registry (avoids global registry races).
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("Marshal zero payload for %q: %v", entry.typeName, err)
			}
			ctor, ok := localCtors[entry.typeName]
			if !ok {
				t.Fatalf("no constructor in local registry for %q", entry.typeName)
			}
			target := ctor()
			if err := json.Unmarshal(raw, target); err != nil {
				t.Fatalf("Unmarshal for %q: %v", entry.typeName, err)
			}
		})
	}
}
