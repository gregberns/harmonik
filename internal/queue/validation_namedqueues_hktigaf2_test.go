package queue_test

// validation_namedqueues_hktigaf2_test.go — queue-naming rule tests (hk-tigaf.2).
//
// Covers:
//   - NormaliseQueueName defaults empty → "main"
//   - ValidateQueueName passes/fails per charset and length rules
//   - Validate rejects invalid QueueName with ReasonQueueNameInvalid
//   - QM-027 per-name: submit to existing non-completed name rejected,
//     submit to same-name completed queue passes
//   - Submit to a different name passes when ActiveQueue is nil for that name
//
// Bead ref: hk-tigaf.2.

import (
	"context"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
)

// maxQueueNameLenTest mirrors the validation package's maxQueueNameLen (64).
const maxQueueNameLenTest = 64

// ---------------------------------------------------------------------------
// NormaliseQueueName
// ---------------------------------------------------------------------------

func TestNormaliseQueueName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"", queue.QueueNameMain},
		{"main", "main"},
		{"work", "work"},
		{"my-queue", "my-queue"},
	}
	for _, c := range cases {
		got := queue.NormaliseQueueName(c.in)
		if got != c.want {
			t.Errorf("NormaliseQueueName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ValidateQueueName
// ---------------------------------------------------------------------------

func TestValidateQueueName(t *testing.T) {
	t.Parallel()

	passing := []string{
		"main",
		"a",
		"my-queue",
		"work-123",
		"abc",
		strings.Repeat("a", maxQueueNameLenTest),
	}
	for _, name := range passing {
		ok, detail := queue.ValidateQueueName(name)
		if !ok {
			t.Errorf("ValidateQueueName(%q): expected pass, got fail: %s", name, detail)
		}
	}

	failing := []struct {
		name   string
		reason string
	}{
		{"", "empty"},
		{strings.Repeat("a", maxQueueNameLenTest+1), "too long"},
		{"UPPER", "uppercase"},
		{"has space", "space"},
		{"under_score", "underscore"},
		{"dot.name", "dot"},
		{"has/slash", "slash"},
	}
	for _, c := range failing {
		ok, _ := queue.ValidateQueueName(c.name)
		if ok {
			t.Errorf("ValidateQueueName(%q) [%s]: expected fail, got pass", c.name, c.reason)
		}
	}
}

// ---------------------------------------------------------------------------
// Validate — QM-002/2.1 name-invalid pre-check
// ---------------------------------------------------------------------------

func TestValidateQueueNameInvalid(t *testing.T) {
	t.Parallel()

	ledger := &validFixtureFakeLedger{}

	badNames := []string{
		"UPPER",
		"has space",
		strings.Repeat("x", maxQueueNameLenTest+1),
		"under_score",
	}
	for _, name := range badNames {
		vreq := queue.ValidationRequest{
			Groups:      []queue.Group{{GroupIndex: 0, Kind: queue.GroupKindStream, Status: queue.GroupStatusPending, Items: []queue.Item{}}},
			ActiveQueue: nil,
			QueueName:   name,
			IsAppend:    false,
		}
		verrs, _, err := queue.Validate(context.Background(), vreq, ledger)
		if err != nil {
			t.Fatalf("name=%q: Validate error: %v", name, err)
		}
		if len(verrs) == 0 {
			t.Fatalf("name=%q: expected ValidationError, got none", name)
		}
		if verrs[0].Reason != queue.ReasonQueueNameInvalid {
			t.Errorf("name=%q: got reason %q, want %q", name, verrs[0].Reason, queue.ReasonQueueNameInvalid)
		}
	}
}

// TestValidateQueueNameEmptySkipsCheck asserts that an empty QueueName in
// ValidationRequest does not trigger ReasonQueueNameInvalid (implicit "main").
func TestValidateQueueNameEmptySkipsCheck(t *testing.T) {
	t.Parallel()

	ledger := &validFixtureFakeLedger{}
	vreq := queue.ValidationRequest{
		Groups:      []queue.Group{{GroupIndex: 0, Kind: queue.GroupKindStream, Status: queue.GroupStatusPending, Items: []queue.Item{}}},
		ActiveQueue: nil,
		QueueName:   "", // empty → skip check
		IsAppend:    false,
	}
	verrs, _, err := queue.Validate(context.Background(), vreq, ledger)
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	for _, ve := range verrs {
		if ve.Reason == queue.ReasonQueueNameInvalid {
			t.Error("empty QueueName should not produce ReasonQueueNameInvalid")
		}
	}
}

// ---------------------------------------------------------------------------
// Validate — QM-027 per-name single-active guard
// ---------------------------------------------------------------------------

// TestValidateQM027PerName_PassDifferentName asserts that submitting to a name
// with no active queue passes (ActiveQueue=nil for that name slot).
func TestValidateQM027PerName_PassDifferentName(t *testing.T) {
	t.Parallel()

	ledger := &validFixtureFakeLedger{}
	vreq := queue.ValidationRequest{
		Groups:      []queue.Group{{GroupIndex: 0, Kind: queue.GroupKindStream, Status: queue.GroupStatusPending, Items: []queue.Item{}}},
		ActiveQueue: nil, // no queue for "work" yet
		QueueName:   "work",
		IsAppend:    false,
	}
	verrs, _, err := queue.Validate(context.Background(), vreq, ledger)
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if len(verrs) > 0 {
		t.Fatalf("expected pass, got errors: %v", verrs)
	}
}

// TestValidateQM027PerName_RejectSameNameActive asserts that submitting to a
// name that already has an active (non-completed) queue is rejected.
func TestValidateQM027PerName_RejectSameNameActive(t *testing.T) {
	t.Parallel()

	ledger := &validFixtureFakeLedger{}
	existing := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "existing-queue-id",
		Name:          "work",
		Status:        queue.QueueStatusActive,
	}
	vreq := queue.ValidationRequest{
		Groups:      []queue.Group{{GroupIndex: 0, Kind: queue.GroupKindStream, Status: queue.GroupStatusPending, Items: []queue.Item{}}},
		ActiveQueue: existing,
		QueueName:   "work",
		IsAppend:    false,
	}
	verrs, _, err := queue.Validate(context.Background(), vreq, ledger)
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if len(verrs) == 0 {
		t.Fatal("expected ReasonQueueAlreadyActive, got no errors")
	}
	if verrs[0].Reason != queue.ReasonQueueAlreadyActive {
		t.Errorf("got reason %q, want %q", verrs[0].Reason, queue.ReasonQueueAlreadyActive)
	}
}

// TestValidateQM027PerName_PassCompletedSameName asserts that submitting to a
// name whose previous queue is completed is allowed (completed is terminal).
func TestValidateQM027PerName_PassCompletedSameName(t *testing.T) {
	t.Parallel()

	ledger := &validFixtureFakeLedger{}
	completed := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "old-queue-id",
		Name:          "work",
		Status:        queue.QueueStatusCompleted,
	}
	vreq := queue.ValidationRequest{
		Groups:      []queue.Group{{GroupIndex: 0, Kind: queue.GroupKindStream, Status: queue.GroupStatusPending, Items: []queue.Item{}}},
		ActiveQueue: completed,
		QueueName:   "work",
		IsAppend:    false,
	}
	verrs, _, err := queue.Validate(context.Background(), vreq, ledger)
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if len(verrs) > 0 {
		t.Fatalf("expected pass for completed queue at same name, got errors: %v", verrs)
	}
}
