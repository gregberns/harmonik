package queue

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const (
	completionQueueID       = "0197c451-0000-7000-8000-000000000001"
	completionTransactionID = "0197c451-0000-7000-8000-000000000002"
	completionReceiptID     = "0197c451-0000-7000-8000-000000000003"
)

func completionFixtureQueue() Queue {
	completedAt := completionFixtureTime()
	return Queue{
		SchemaVersion: 1,
		QueueID:       completionQueueID,
		Name:          QueueNameMain,
		Status:        QueueStatusActive,
		Groups: []Group{{
			GroupIndex:  4,
			Kind:        GroupKindStream,
			Status:      GroupStatusCompleteSuccess,
			CompletedAt: &completedAt,
			Items: []Item{{
				BeadID:         "hk-completion-one",
				Status:         ItemStatusCompleted,
				TemplateParams: map[string]string{"mode": "fixed"},
			}},
		}},
	}
}

func completionFixtureTime() time.Time {
	return time.Date(2026, 8, 10, 18, 12, 13, 456789000, time.UTC)
}

func TestPrepareCompletionProducesExactStableDetachedValues(t *testing.T) {
	prior := completionFixtureQueue()
	completedAt := completionFixtureTime()

	first, err := PrepareCompletion(prior, completionTransactionID, completionReceiptID, completedAt)
	if err != nil {
		t.Fatalf("PrepareCompletion: %v", err)
	}
	second, err := PrepareCompletion(prior, completionTransactionID, completionReceiptID, completedAt)
	if err != nil {
		t.Fatalf("PrepareCompletion repeated: %v", err)
	}
	firstBytes, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatalf("fixed input changed output:\n%s\n%s", firstBytes, secondBytes)
	}
	if first.TransactionID != completionTransactionID || first.Receipt.ReceiptID != completionReceiptID {
		t.Fatalf("prepared identities = %q/%q", first.TransactionID, first.Receipt.ReceiptID)
	}
	if first.Receipt.CompletedAt != "2026-08-10T18:12:13.456Z" {
		t.Fatalf("completed_at = %q", first.Receipt.CompletedAt)
	}
	if first.Receipt.FinalGroupIndex != 4 || first.Receipt.SuccessCount != 1 || first.Receipt.FailCount != 0 {
		t.Fatalf("final facts = %+v", first.Receipt)
	}
	if first.Candidate.Status != QueueStatusCompleted {
		t.Fatalf("candidate status = %q", first.Candidate.Status)
	}
	if len(first.CandidateBytes) == 0 || len(first.ReceiptBytes) == 0 {
		t.Fatal("completion plan did not retain exact candidate and receipt bytes")
	}
	if !strings.Contains(string(first.CandidateBytes), `"completed_at":"2026-08-10T18:12:13.456Z"`) ||
		strings.Contains(string(first.CandidateBytes), ".456789") {
		t.Fatalf("candidate bytes do not use the one canonical completion time: %s", first.CandidateBytes)
	}
	if first.MarkerInputs.ReceiptSHA256 != first.Binding.SHA256 ||
		first.MarkerInputs.CompletedQueueSHA256 != first.Receipt.CompletedQueueSHA256 {
		t.Fatalf("marker inputs = %+v", first.MarkerInputs)
	}

	prior.Groups[0].Items[0].TemplateParams["mode"] = "mutated"
	if got := first.Candidate.Groups[0].Items[0].TemplateParams["mode"]; got != "fixed" {
		t.Fatalf("candidate shares input map: %q", got)
	}
	receiptBytes, err := base64.StdEncoding.DecodeString(first.Binding.CanonicalBytesBase64)
	if err != nil {
		t.Fatal(err)
	}
	if digestHex(receiptBytes) != first.Binding.SHA256 {
		t.Fatal("binding digest does not match exact receipt bytes")
	}
}

func TestPrepareCompletionRejectsInvalidAdmittedValues(t *testing.T) {
	tests := []struct {
		name          string
		queue         Queue
		transactionID string
		receiptID     string
		want          string
	}{
		{name: "transaction ID", queue: completionFixtureQueue(), transactionID: "bad", receiptID: completionReceiptID, want: "transaction id"},
		{name: "receipt ID", queue: completionFixtureQueue(), transactionID: completionTransactionID, receiptID: "bad", want: "receipt id"},
		{name: "no groups", queue: Queue{SchemaVersion: 1, QueueID: completionQueueID, Status: QueueStatusActive}, transactionID: completionTransactionID, receiptID: completionReceiptID, want: "at least one group"},
		{name: "failed group", queue: func() Queue {
			q := completionFixtureQueue()
			q.Groups[0].Status = GroupStatusCompleteWithFailures
			return q
		}(), transactionID: completionTransactionID, receiptID: completionReceiptID, want: "complete queue"},
		{name: "missing completed time", queue: func() Queue { q := completionFixtureQueue(); q.Groups[0].CompletedAt = nil; return q }(), transactionID: completionTransactionID, receiptID: completionReceiptID, want: "completion time"},
		{name: "mismatched completed time", queue: completionFixtureQueue(), transactionID: completionTransactionID, receiptID: completionReceiptID, want: "completion time"},
		{name: "non-completed item", queue: func() Queue { q := completionFixtureQueue(); q.Groups[0].Items[0].Status = ItemStatusFailed; return q }(), transactionID: completionTransactionID, receiptID: completionReceiptID, want: "non-completed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stamp := completionFixtureTime()
			if tc.name == "mismatched completed time" {
				stamp = stamp.Add(time.Second)
			}
			_, err := PrepareCompletion(tc.queue, tc.transactionID, tc.receiptID, stamp)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want text %q", err, tc.want)
			}
		})
	}
}

func TestCompletionReceiptBindingCouplesOnlyToCompletion(t *testing.T) {
	prior := completionFixtureQueue()
	prepared, err := PrepareCompletion(
		prior,
		completionTransactionID,
		completionReceiptID,
		completionFixtureTime(),
	)
	if err != nil {
		t.Fatal(err)
	}
	priorBytes, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	candidateBytes := prepared.CandidateBytes
	base := ReplacementPlan{
		OperationKind:            OperationCompletion,
		NormalizedName:           QueueNameMain,
		QueueID:                  completionQueueID,
		PriorBytes:               priorBytes,
		CandidateBytes:           candidateBytes,
		CompletionReceiptBinding: prepared.Binding,
	}
	if err := validateCompletionReceiptBinding(prepared.Binding, completionQueueID, completionTransactionID, QueueNameMain, digestHex(candidateBytes)); err != nil {
		t.Fatalf("prepared binding self-check: %v; receipt=%+v candidate_digest=%s", err, prepared.Receipt, digestHex(candidateBytes))
	}
	intent, intentBytes, err := prepareReplacementWithIDs(base, completionTransactionID, "")
	if err != nil {
		t.Fatalf("prepare completion replacement: %v", err)
	}
	if intent.CompletionReceiptBinding == nil {
		t.Fatal("completion intent has no receipt binding")
	}
	decoded, err := decodeReplaceIntent(intentBytes)
	if err != nil || decoded.CompletionReceiptBinding == nil {
		t.Fatalf("decode completion intent: binding=%+v err=%v", decoded.CompletionReceiptBinding, err)
	}
	for _, kind := range []OperationKind{OperationAppend, OperationFailedRecovery, OperationCancellation} {
		conflict := intent
		conflict.OperationKind = kind
		if err := validateReplaceIntent(conflict); err == nil {
			t.Fatalf("%s intent accepted completion receipt binding", kind)
		}
		conflictBytes, err := json.Marshal(conflict)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeReplaceIntent(conflictBytes); err == nil {
			t.Fatalf("startup decode accepted %s intent with completion binding", kind)
		}
	}

	missing := base
	missing.CompletionReceiptBinding = nil
	if _, _, err := prepareReplacementWithIDs(missing, completionTransactionID, ""); err == nil {
		t.Fatal("completion without receipt binding succeeded")
	}
	for _, kind := range []OperationKind{
		OperationCreate, OperationSubmit, OperationAppend, OperationReservation,
		OperationRunIDPatch, OperationActivation, OperationAdvance, OperationPause,
		OperationResume, OperationEagerRefill, OperationBudgetCharge,
		OperationReviewCharge, OperationMaintenance, OperationReconciliation,
		OperationStartup, OperationInline, OperationBootstrap, OperationCrewPlaceholder,
	} {
		t.Run(string(kind), func(t *testing.T) {
			wrong := base
			wrong.OperationKind = kind
			if _, _, err := prepareReplacementWithIDs(wrong, completionTransactionID, ""); err == nil {
				t.Fatal("non-completion operation accepted completion binding")
			}
		})
	}
}

func TestCompletionReceiptBindingRejectsEveryFieldAndCandidateConflict(t *testing.T) {
	prepared, err := PrepareCompletion(
		completionFixtureQueue(),
		completionTransactionID,
		completionReceiptID,
		completionFixtureTime(),
	)
	if err != nil {
		t.Fatal(err)
	}
	valid := *prepared.Binding
	tests := []struct {
		name   string
		mutate func(*CompletionReceiptBinding)
	}{
		{name: "receipt id", mutate: func(v *CompletionReceiptBinding) { v.ReceiptID = "bad" }},
		{name: "basename", mutate: func(v *CompletionReceiptBinding) { v.Basename = "wrong.json" }},
		{name: "schema", mutate: func(v *CompletionReceiptBinding) { v.SchemaVersion = 2 }},
		{name: "bytes", mutate: func(v *CompletionReceiptBinding) { v.CanonicalBytesBase64 = "%%%" }},
		{name: "digest", mutate: func(v *CompletionReceiptBinding) { v.SHA256 = strings.Repeat("0", 64) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			binding := valid
			tc.mutate(&binding)
			if err := validateCompletionReceiptBinding(
				&binding,
				completionQueueID,
				completionTransactionID,
				QueueNameMain,
				prepared.Receipt.CompletedQueueSHA256,
			); err == nil {
				t.Fatal("invalid binding validated")
			}
		})
	}

	for _, tc := range []struct {
		name   string
		mutate func(*CompletionReceipt)
	}{
		{name: "final group", mutate: func(v *CompletionReceipt) { v.FinalGroupIndex++ }},
		{name: "success count", mutate: func(v *CompletionReceipt) { v.SuccessCount++ }},
		{name: "completed time", mutate: func(v *CompletionReceipt) { v.CompletedAt = "2026-08-10T18:12:14.456Z" }},
	} {
		t.Run("candidate conflict "+tc.name, func(t *testing.T) {
			receipt := prepared.Receipt
			tc.mutate(&receipt)
			receiptBytes, err := json.Marshal(receipt)
			if err != nil {
				t.Fatal(err)
			}
			binding := valid
			binding.CanonicalBytesBase64 = base64.StdEncoding.EncodeToString(receiptBytes)
			binding.SHA256 = digestHex(receiptBytes)
			plan := ReplacementPlan{
				TransactionID:            completionTransactionID,
				OperationKind:            OperationCompletion,
				NormalizedName:           QueueNameMain,
				QueueID:                  completionQueueID,
				CandidateBytes:           prepared.CandidateBytes,
				CompletionReceiptBinding: &binding,
			}
			if err := validateCompletionPlan(plan, prepared.Candidate, prepared.Receipt.CompletedQueueSHA256); err == nil {
				t.Fatal("receipt facts that conflict with candidate validated")
			}
		})
	}
}

func TestCompletionReleaseMarkerCanonicalValidation(t *testing.T) {
	released := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	marker := CompletionReleaseMarker{
		RecordType:           "completion-release",
		SchemaVersion:        1,
		QueueID:              completionQueueID,
		ReceiptID:            completionReceiptID,
		TransactionID:        completionTransactionID,
		ReceiptSHA256:        strings.Repeat("a", 64),
		CompletedQueueSHA256: strings.Repeat("b", 64),
		ReleasedAt:           released.Format("2006-01-02T15:04:05.000Z"),
		GCNotBefore:          released.Add(720 * time.Hour).Format("2006-01-02T15:04:05.000Z"),
	}
	if err := validateCompletionReleaseMarker(marker); err != nil {
		t.Fatalf("valid marker: %v", err)
	}
	marker.GCNotBefore = released.Add(time.Hour).Format("2006-01-02T15:04:05.000Z")
	if err := validateCompletionReleaseMarker(marker); err == nil {
		t.Fatal("marker accepted wrong retention interval")
	}
}

func TestCompletionReceiptStrictRoundTripRejectsEachInvalidField(t *testing.T) {
	prepared, err := PrepareCompletion(
		completionFixtureQueue(),
		completionTransactionID,
		completionReceiptID,
		completionFixtureTime(),
	)
	if err != nil {
		t.Fatal(err)
	}
	validBytes, err := json.Marshal(prepared.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := DecodeCompletionReceipt(validBytes); err != nil || got != prepared.Receipt {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	tests := []struct {
		name   string
		mutate func(*CompletionReceipt)
	}{
		{name: "schema", mutate: func(v *CompletionReceipt) { v.SchemaVersion = 2 }},
		{name: "queue id", mutate: func(v *CompletionReceipt) { v.QueueID = "bad" }},
		{name: "receipt id", mutate: func(v *CompletionReceipt) { v.ReceiptID = "bad" }},
		{name: "transaction id", mutate: func(v *CompletionReceipt) { v.TransactionID = "bad" }},
		{name: "name", mutate: func(v *CompletionReceipt) { v.NormalizedName = "Bad Name" }},
		{name: "group index", mutate: func(v *CompletionReceipt) { v.FinalGroupIndex = -1 }},
		{name: "status", mutate: func(v *CompletionReceipt) { v.FinalStatus = "complete-with-failures" }},
		{name: "success count", mutate: func(v *CompletionReceipt) { v.SuccessCount = -1 }},
		{name: "fail count", mutate: func(v *CompletionReceipt) { v.FailCount = 1 }},
		{name: "time", mutate: func(v *CompletionReceipt) { v.CompletedAt = "2026-08-10T12:00:00Z" }},
		{name: "digest", mutate: func(v *CompletionReceipt) { v.CompletedQueueSHA256 = "bad" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			value := prepared.Receipt
			tc.mutate(&value)
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeCompletionReceipt(data); err == nil {
				t.Fatal("invalid receipt decoded")
			}
		})
	}
	unknown := append(append([]byte(nil), validBytes[:len(validBytes)-1]...), []byte(`,"unknown":true}`)...)
	if _, err := DecodeCompletionReceipt(unknown); err == nil {
		t.Fatal("receipt with unknown field decoded")
	}
}

func TestCompletionReleaseMarkerStrictRoundTripAndBasenames(t *testing.T) {
	released := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	marker := CompletionReleaseMarker{
		RecordType:           "completion-release",
		SchemaVersion:        1,
		QueueID:              completionQueueID,
		ReceiptID:            completionReceiptID,
		TransactionID:        completionTransactionID,
		ReceiptSHA256:        strings.Repeat("a", 64),
		CompletedQueueSHA256: strings.Repeat("b", 64),
		ReleasedAt:           released.Format("2006-01-02T15:04:05.000Z"),
		GCNotBefore:          released.Add(720 * time.Hour).Format("2006-01-02T15:04:05.000Z"),
	}
	data, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := DecodeCompletionReleaseMarker(data); err != nil || got != marker {
		t.Fatalf("marker round trip = %+v, %v", got, err)
	}
	receiptBase, err := CompletionReceiptBasename(completionQueueID, completionReceiptID)
	if err != nil || receiptBase != completionQueueID+"--"+completionReceiptID+".json" {
		t.Fatalf("receipt basename = %q, %v", receiptBase, err)
	}
	markerBase, err := CompletionReleaseMarkerBasename(completionQueueID, completionReceiptID)
	if err != nil || markerBase != completionQueueID+"--"+completionReceiptID+".release-v1.json" {
		t.Fatalf("marker basename = %q, %v", markerBase, err)
	}
	for _, mutate := range []func(*CompletionReleaseMarker){
		func(v *CompletionReleaseMarker) { v.RecordType = "wrong" },
		func(v *CompletionReleaseMarker) { v.SchemaVersion = 2 },
		func(v *CompletionReleaseMarker) { v.QueueID = "bad" },
		func(v *CompletionReleaseMarker) { v.ReceiptID = "bad" },
		func(v *CompletionReleaseMarker) { v.TransactionID = "bad" },
		func(v *CompletionReleaseMarker) { v.ReceiptSHA256 = "bad" },
		func(v *CompletionReleaseMarker) { v.CompletedQueueSHA256 = "bad" },
		func(v *CompletionReleaseMarker) { v.ReleasedAt = "bad" },
		func(v *CompletionReleaseMarker) { v.GCNotBefore = "bad" },
	} {
		invalid := marker
		mutate(&invalid)
		invalidBytes, err := json.Marshal(invalid)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeCompletionReleaseMarker(invalidBytes); err == nil {
			t.Fatalf("invalid marker decoded: %+v", invalid)
		}
	}
}
