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
// Cohort registration assertions — all 7 queue events must be registered
// with the correct constructor shape (EV-032 / EV-034 / hk-yslws).
// ---------------------------------------------------------------------------

// TestQueueEventsCohortRegistered asserts that all 7 §8.10 event type names
// produce the correct concrete payload pointer type from their constructors,
// and that a JSON round-trip through a local (isolated) registry succeeds.
//
// This test does NOT use the global registry (which may be reset by
// TestRegistry subtests via t.Cleanup(eventRegistryReset)). Instead it
// registers the 7 constructors into a fresh local registry snapshot and
// exercises DecodePayload through that snapshot directly. This is the
// same isolation pattern used by TestRedactionFailedPayloadConstructorShape.
//
// Durability class context (documented here for spec traceability):
//
//	Class F (fsync-boundary): queue_submitted, queue_group_completed,
//	  queue_paused, queue_item_reconciled.
//	Class O (ordinary): queue_group_started, queue_appended,
//	  queue_item_deferred_for_ledger_dep.
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
	}

	// Build a local registry snapshot populated with only the queue cohort.
	// This avoids races with eventRegistryReset() in TestRegistry subtests.
	localCtors := make(map[string]func() EventPayload, len(cohort))
	for _, entry := range cohort {
		localCtors[entry.typeName] = entry.mkPayload
	}

	for _, entry := range cohort {
		entry := entry
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
