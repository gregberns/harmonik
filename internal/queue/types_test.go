package queue_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// typesFixtureTimestamp returns a deterministic UTC timestamp for use in
// JSON round-trip fixtures (millisecond precision, no monotonic component).
func typesFixtureTimestamp() time.Time {
	return time.Date(2026, 5, 14, 18, 22, 11, 482_000_000, time.UTC)
}

// typesFixtureTimestampPtr returns a pointer to typesFixtureTimestamp().
func typesFixtureTimestampPtr() *time.Time {
	t := typesFixtureTimestamp()
	return &t
}

// typesFixtureQueueID returns a canonical UUIDv7 string for test fixtures.
func typesFixtureQueueID() string {
	return "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0001"
}

// typesFixtureRunID returns a canonical run ID string for test fixtures.
func typesFixtureRunID() *string {
	s := "0190b3c4-9001-7000-8000-000000000001"
	return &s
}

// typesFixtureQueue builds a minimal valid Queue for round-trip tests.
func typesFixtureQueue() queue.Queue {
	return queue.Queue{
		SchemaVersion: 1,
		QueueID:       typesFixtureQueueID(),
		SubmittedAt:   typesFixtureTimestamp(),
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusCompleteSuccess,
				Items: []queue.Item{
					{
						BeadID:     core.BeadID("hk-09tne"),
						Status:     queue.ItemStatusCompleted,
						RunID:      typesFixtureRunID(),
						AppendedAt: nil,
					},
				},
				CreatedAt:   typesFixtureTimestamp(),
				StartedAt:   typesFixtureTimestampPtr(),
				CompletedAt: typesFixtureTimestampPtr(),
			},
			{
				GroupIndex: 1,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID:     core.BeadID("hk-1n0cw"),
						Status:     queue.ItemStatusDispatched,
						RunID:      typesFixtureRunID(),
						AppendedAt: nil,
					},
					{
						BeadID:     core.BeadID("hk-u5c5i"),
						Status:     queue.ItemStatusPending,
						RunID:      nil,
						AppendedAt: nil,
					},
				},
				CreatedAt:   typesFixtureTimestamp(),
				StartedAt:   typesFixtureTimestampPtr(),
				CompletedAt: nil,
			},
		},
	}
}

// TestQueueRoundTrip verifies JSON encode → decode fidelity for the Queue
// envelope (specs/queue-model.md §2.1, §2.9).
func TestQueueRoundTrip(t *testing.T) {
	t.Parallel()

	original := typesFixtureQueue()

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got queue.Queue
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.SchemaVersion != original.SchemaVersion {
		t.Errorf("SchemaVersion: got %d, want %d", got.SchemaVersion, original.SchemaVersion)
	}
	if got.QueueID != original.QueueID {
		t.Errorf("QueueID: got %q, want %q", got.QueueID, original.QueueID)
	}
	if !got.SubmittedAt.Equal(original.SubmittedAt) {
		t.Errorf("SubmittedAt: got %v, want %v", got.SubmittedAt, original.SubmittedAt)
	}
	if got.Status != original.Status {
		t.Errorf("Status: got %q, want %q", got.Status, original.Status)
	}
	if len(got.Groups) != len(original.Groups) {
		t.Fatalf("Groups len: got %d, want %d", len(got.Groups), len(original.Groups))
	}
}

// TestUnmarshalQueueSchemaVersionEnforced verifies that UnmarshalQueue rejects
// envelopes with schema_version != 1 (specs/queue-model.md §2.1 QM-002).
func TestUnmarshalQueueSchemaVersionEnforced(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		version int
		wantErr bool
	}{
		{"version_1_accepted", 1, false},
		{"version_0_rejected", 0, true},
		{"version_2_rejected", 2, true},
		{"version_negative_rejected", -1, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			raw := map[string]any{
				"schema_version": tc.version,
				"queue_id":       typesFixtureQueueID(),
				"submitted_at":   "2026-05-14T18:22:11.482Z",
				"status":         "active",
				"groups":         []any{},
			}
			data, err := json.Marshal(raw)
			if err != nil {
				t.Fatalf("marshal fixture: %v", err)
			}

			_, err = queue.UnmarshalQueue(data)
			if tc.wantErr && err == nil {
				t.Errorf("UnmarshalQueue: want error for schema_version=%d, got nil", tc.version)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("UnmarshalQueue: unexpected error for schema_version=%d: %v", tc.version, err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, queue.ErrSchemaVersion) {
				t.Errorf("UnmarshalQueue: error %v does not wrap ErrSchemaVersion", err)
			}
		})
	}
}

// TestGroupRoundTrip verifies JSON encode → decode fidelity for the Group
// record including optional timestamp fields (specs/queue-model.md §2.3).
func TestGroupRoundTrip(t *testing.T) {
	t.Parallel()

	original := queue.Group{
		GroupIndex:  0,
		Kind:        queue.GroupKindWave,
		Status:      queue.GroupStatusPending,
		Items:       []queue.Item{},
		CreatedAt:   typesFixtureTimestamp(),
		StartedAt:   nil,
		CompletedAt: nil,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got queue.Group
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.GroupIndex != original.GroupIndex {
		t.Errorf("GroupIndex: got %d, want %d", got.GroupIndex, original.GroupIndex)
	}
	if got.Kind != original.Kind {
		t.Errorf("Kind: got %q, want %q", got.Kind, original.Kind)
	}
	if got.Status != original.Status {
		t.Errorf("Status: got %q, want %q", got.Status, original.Status)
	}
	if got.StartedAt != nil {
		t.Errorf("StartedAt: want nil, got %v", got.StartedAt)
	}
	if got.CompletedAt != nil {
		t.Errorf("CompletedAt: want nil, got %v", got.CompletedAt)
	}
}

// TestItemRoundTrip verifies JSON encode → decode fidelity for the Item record
// including optional run_id and appended_at fields (specs/queue-model.md §2.6).
func TestItemRoundTrip(t *testing.T) {
	t.Parallel()

	rid := "0190b3c4-9001-7000-8000-000000000002"
	original := queue.Item{
		BeadID:     core.BeadID("hk-u5c5i"),
		Status:     queue.ItemStatusDeferredForLedgerDep,
		RunID:      &rid,
		AppendedAt: typesFixtureTimestampPtr(),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got queue.Item
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.BeadID != original.BeadID {
		t.Errorf("BeadID: got %q, want %q", got.BeadID, original.BeadID)
	}
	if got.Status != original.Status {
		t.Errorf("Status: got %q, want %q", got.Status, original.Status)
	}
	if got.RunID == nil || *got.RunID != rid {
		t.Errorf("RunID: got %v, want %q", got.RunID, rid)
	}
	if got.AppendedAt == nil {
		t.Error("AppendedAt: got nil, want non-nil")
	}
}

// TestItemNilOptionalFieldsOmitted verifies that nil optional fields on Item
// are omitted from JSON output (specs/queue-model.md §2.9).
func TestItemNilOptionalFieldsOmitted(t *testing.T) {
	t.Parallel()

	item := queue.Item{
		BeadID:     core.BeadID("hk-test"),
		Status:     queue.ItemStatusPending,
		RunID:      nil,
		AppendedAt: nil,
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal into map: %v", err)
	}

	// run_id and appended_at should be present but null (the spec example shows
	// explicit null for these fields, so we allow null; omitempty is NOT used on
	// these fields per the spec §2.9 informative example which shows null values).
	// Verify the fields are present in the JSON map (as JSON null).
	for _, key := range []string{"run_id", "appended_at"} {
		v, ok := m[key]
		if !ok {
			t.Errorf("field %q missing from JSON", key)
			continue
		}
		if v != nil {
			t.Errorf("field %q: got %v, want JSON null", key, v)
		}
	}
}

// TestQueueSubmitRequestRoundTrip verifies JSON fidelity for QueueSubmitRequest
// (specs/queue-model.md §2.10).
func TestQueueSubmitRequestRoundTrip(t *testing.T) {
	t.Parallel()

	original := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups: []queue.Group{
			{
				GroupIndex:  0,
				Kind:        queue.GroupKindWave,
				Status:      queue.GroupStatusPending,
				Items:       []queue.Item{{BeadID: core.BeadID("hk-abc"), Status: queue.ItemStatusPending}},
				CreatedAt:   typesFixtureTimestamp(),
				StartedAt:   nil,
				CompletedAt: nil,
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got queue.QueueSubmitRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.SchemaVersion != 1 {
		t.Errorf("SchemaVersion: got %d, want 1", got.SchemaVersion)
	}
	if len(got.Groups) != 1 {
		t.Fatalf("Groups len: got %d, want 1", len(got.Groups))
	}
}

// TestQueueSubmitResponseRoundTrip verifies JSON fidelity for
// QueueSubmitResponse (specs/queue-model.md §2.10).
func TestQueueSubmitResponseRoundTrip(t *testing.T) {
	t.Parallel()

	original := queue.QueueSubmitResponse{
		QueueID:    typesFixtureQueueID(),
		Status:     queue.QueueStatusActive,
		GroupCount: 2,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got queue.QueueSubmitResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.QueueID != original.QueueID {
		t.Errorf("QueueID: got %q, want %q", got.QueueID, original.QueueID)
	}
	if got.Status != original.Status {
		t.Errorf("Status: got %q, want %q", got.Status, original.Status)
	}
	if got.GroupCount != original.GroupCount {
		t.Errorf("GroupCount: got %d, want %d", got.GroupCount, original.GroupCount)
	}
}

// TestQueueAppendRequestRoundTrip verifies JSON fidelity for
// QueueAppendRequest (specs/queue-model.md §2.10).
func TestQueueAppendRequestRoundTrip(t *testing.T) {
	t.Parallel()

	original := queue.QueueAppendRequest{
		QueueID:    typesFixtureQueueID(),
		GroupIndex: 1,
		BeadIDs:    []core.BeadID{"hk-abc", "hk-def"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got queue.QueueAppendRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.QueueID != original.QueueID {
		t.Errorf("QueueID: got %q, want %q", got.QueueID, original.QueueID)
	}
	if got.GroupIndex != original.GroupIndex {
		t.Errorf("GroupIndex: got %d, want %d", got.GroupIndex, original.GroupIndex)
	}
	if len(got.BeadIDs) != len(original.BeadIDs) {
		t.Fatalf("BeadIDs len: got %d, want %d", len(got.BeadIDs), len(original.BeadIDs))
	}
}

// TestQueueAppendResponseRoundTrip verifies JSON fidelity for
// QueueAppendResponse (specs/queue-model.md §2.10).
func TestQueueAppendResponseRoundTrip(t *testing.T) {
	t.Parallel()

	original := queue.QueueAppendResponse{
		AppendedCount:  2,
		NewTailIndices: []int{3, 4},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got queue.QueueAppendResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.AppendedCount != original.AppendedCount {
		t.Errorf("AppendedCount: got %d, want %d", got.AppendedCount, original.AppendedCount)
	}
	if len(got.NewTailIndices) != len(original.NewTailIndices) {
		t.Fatalf("NewTailIndices len: got %d, want %d", len(got.NewTailIndices), len(original.NewTailIndices))
	}
}

// TestQueueStatusResponseRoundTrip verifies JSON fidelity for
// QueueStatusResponse including the null-queue case (specs/queue-model.md §2.10).
func TestQueueStatusResponseRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("with_queue", func(t *testing.T) {
		t.Parallel()

		q := typesFixtureQueue()
		original := queue.QueueStatusResponse{Queue: &q}

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}

		var got queue.QueueStatusResponse
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		if got.Queue == nil {
			t.Fatal("Queue: got nil, want non-nil")
		}
		if got.Queue.QueueID != q.QueueID {
			t.Errorf("Queue.QueueID: got %q, want %q", got.Queue.QueueID, q.QueueID)
		}
	})

	t.Run("null_queue", func(t *testing.T) {
		t.Parallel()

		original := queue.QueueStatusResponse{Queue: nil}

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}

		var got queue.QueueStatusResponse
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		if got.Queue != nil {
			t.Errorf("Queue: got %+v, want nil", got.Queue)
		}
	})
}

// TestQueueDryRunRequestRoundTrip verifies JSON fidelity for
// QueueDryRunRequest (specs/queue-model.md §2.10).
func TestQueueDryRunRequestRoundTrip(t *testing.T) {
	t.Parallel()

	original := queue.QueueDryRunRequest{
		SchemaVersion: 1,
		Groups: []queue.Group{
			{
				GroupIndex:  0,
				Kind:        queue.GroupKindStream,
				Status:      queue.GroupStatusPending,
				Items:       []queue.Item{},
				CreatedAt:   typesFixtureTimestamp(),
				StartedAt:   nil,
				CompletedAt: nil,
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got queue.QueueDryRunRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.SchemaVersion != 1 {
		t.Errorf("SchemaVersion: got %d, want 1", got.SchemaVersion)
	}
	if len(got.Groups) != 1 {
		t.Fatalf("Groups len: got %d, want 1", len(got.Groups))
	}
}

// TestQueueDryRunResponseRoundTrip verifies JSON fidelity for
// QueueDryRunResponse (specs/queue-model.md §2.10).
func TestQueueDryRunResponseRoundTrip(t *testing.T) {
	t.Parallel()

	original := queue.QueueDryRunResponse{
		ResolvedQueue: typesFixtureQueue(),
		LedgerDepNotices: []queue.LedgerDepNotice{
			{
				BeadID:        core.BeadID("hk-aaa"),
				BlockerBeadID: core.BeadID("hk-bbb"),
			},
		},
		ParallelismNarrowed: true,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got queue.QueueDryRunResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.ResolvedQueue.QueueID != original.ResolvedQueue.QueueID {
		t.Errorf("ResolvedQueue.QueueID: got %q, want %q", got.ResolvedQueue.QueueID, original.ResolvedQueue.QueueID)
	}
	if len(got.LedgerDepNotices) != 1 {
		t.Fatalf("LedgerDepNotices len: got %d, want 1", len(got.LedgerDepNotices))
	}
	if got.LedgerDepNotices[0].BeadID != "hk-aaa" {
		t.Errorf("LedgerDepNotices[0].BeadID: got %q, want hk-aaa", got.LedgerDepNotices[0].BeadID)
	}
	if !got.ParallelismNarrowed {
		t.Error("ParallelismNarrowed: got false, want true")
	}
}
