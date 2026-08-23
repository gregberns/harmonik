package queue

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func recoverFailedReplaceIntentBytes(projectDir string, intentBytes []byte) (ReplaceRecoveryAction, error) {
	intent, err := decodeReplaceIntent(intentBytes)
	if err != nil {
		return ReplaceRefuse, err
	}
	if intent.FailedRecoveryReceiptBinding == nil {
		return ReplaceRefuse, errors.New("replace intent has no failed recovery receipt binding")
	}
	return recoverFailedReplaceIntent(projectDir, intent, intentBytes, osNamespaceOps())
}

func transactionRecoveryFixturePlan(t *testing.T) ReplacementPlan {
	t.Helper()
	plan := transactionFixturePlan(t, t.TempDir())
	plan.OperationKind = OperationFailedRecovery
	plan.TransactionID = "0190b3c4-9001-7000-8000-000000000011"

	var prior Queue
	if err := json.Unmarshal(plan.PriorBytes, &prior); err != nil {
		t.Fatal(err)
	}
	prior.Status = QueueStatusPausedByFailure
	priorBytes, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	plan.PriorBytes = priorBytes

	var candidate Queue
	if err := json.Unmarshal(plan.CandidateBytes, &candidate); err != nil {
		t.Fatal(err)
	}
	candidate.Status = QueueStatusActive
	receiptID := "0190b3c4-9001-7000-8000-000000000010"
	candidate.FailedRecoveryReceiptID = &receiptID
	candidateBytes, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	plan.CandidateBytes = candidateBytes

	receiptBytes := []byte(`{"schema_version":1,"record_type":"failed-recovery","queue_id":"` + candidate.QueueID + `","receipt_id":"0190b3c4-9001-7000-8000-000000000010","transaction_id":"` + plan.TransactionID + `","normalized_name":"` + plan.NormalizedName + `","prior_queue_sha256":"` + digestHex(plan.PriorBytes) + `","recovered_queue_sha256":"` + digestHex(plan.CandidateBytes) + `","recovered_items":[],"recovered_at":"2026-08-02T18:22:11.482Z"}`)
	digest := sha256.Sum256(receiptBytes)
	plan.FailedRecoveryReceiptBinding = &FailedRecoveryReceiptBinding{
		ReceiptID:            receiptID,
		TransactionID:        plan.TransactionID,
		Basename:             candidate.QueueID + "--" + receiptID + ".json",
		SchemaVersion:        1,
		CanonicalBytesBase64: base64.StdEncoding.EncodeToString(receiptBytes),
		SHA256:               hex.EncodeToString(digest[:]),
	}
	return plan
}

func TestPrepareReplacement_FailedRecoveryRequiresReceiptBinding(t *testing.T) {
	t.Parallel()

	plan := transactionRecoveryFixturePlan(t)
	plan.FailedRecoveryReceiptBinding = nil

	if _, _, err := prepareReplacementWithIDs(
		plan,
		plan.TransactionID,
		"",
	); err == nil {
		t.Fatal("prepareReplacementWithIDs accepted failed recovery without a receipt binding")
	}
}

func TestPrepareReplacement_FailedRecoveryBindsExactReceipt(t *testing.T) {
	t.Parallel()

	plan := transactionRecoveryFixturePlan(t)
	intent, _, err := prepareReplacementWithIDs(
		plan,
		plan.TransactionID,
		"",
	)
	if err != nil {
		t.Fatalf("prepareReplacementWithIDs: %v", err)
	}
	if intent.FailedRecoveryReceiptBinding == nil {
		t.Fatal("intent omitted failed recovery receipt binding")
	}
	if got := intent.FailedRecoveryReceiptBinding.SHA256; got != plan.FailedRecoveryReceiptBinding.SHA256 {
		t.Errorf("binding SHA256 = %q, want %q", got, plan.FailedRecoveryReceiptBinding.SHA256)
	}
}

func TestPrepareReplacement_FailedRecoveryRejectsReceiptWithWrongQueueDigest(t *testing.T) {
	t.Parallel()

	plan := transactionRecoveryFixturePlan(t)
	plan.FailedRecoveryReceiptBinding.CanonicalBytesBase64 = base64.StdEncoding.EncodeToString([]byte(
		`{"schema_version":1,"record_type":"failed-recovery","queue_id":"` + plan.QueueID + `","receipt_id":"` + plan.FailedRecoveryReceiptBinding.ReceiptID + `","transaction_id":"` + plan.TransactionID + `","normalized_name":"main","prior_queue_sha256":"` + digestHex(plan.PriorBytes) + `","recovered_queue_sha256":"` + strings.Repeat("0", 64) + `","recovered_items":[],"recovered_at":"2026-08-02T18:22:11.482Z"}`,
	))
	badBytes, err := base64.StdEncoding.DecodeString(plan.FailedRecoveryReceiptBinding.CanonicalBytesBase64)
	if err != nil {
		t.Fatal(err)
	}
	plan.FailedRecoveryReceiptBinding.SHA256 = digestHex(badBytes)

	if _, _, err := prepareReplacementWithIDs(plan, plan.TransactionID, ""); err == nil {
		t.Fatal("prepareReplacementWithIDs accepted a receipt with the wrong recovered queue digest")
	}
}

func TestWriteReplacement_FailedRecoveryWritesBoundReceipt(t *testing.T) {
	t.Parallel()

	plan := transactionRecoveryFixturePlan(t)
	transactionSeedPrior(t, plan)

	got := WriteReplacement(t.Context(), plan)
	if !got.Committed() {
		t.Fatalf("WriteReplacement outcome = %q err=%v, want committed", got.Outcome, got.Err)
	}
	receiptPath := filepath.Join(
		failedRecoveryReceiptsDir(plan.ProjectDir),
		plan.FailedRecoveryReceiptBinding.Basename,
	)
	data, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	want, err := base64.StdEncoding.DecodeString(plan.FailedRecoveryReceiptBinding.CanonicalBytesBase64)
	if err != nil {
		t.Fatalf("decode fixture receipt: %v", err)
	}
	if string(data) != string(want) {
		t.Errorf("receipt bytes = %q, want %q", data, want)
	}
}

func TestWriteReplacement_FailedRecoveryReceiptFailureRetainsRecoverableIntent(t *testing.T) {
	t.Parallel()

	plan := transactionRecoveryFixturePlan(t)
	transactionSeedPrior(t, plan)
	ops := osNamespaceOps()
	openFile := ops.openFile
	ops.openFile = func(path string, flags int, mode os.FileMode) (*os.File, error) {
		if strings.Contains(path, ".failed-recovery-receipts") {
			return nil, errors.New("cut failed recovery receipt write")
		}
		return openFile(path, flags, mode)
	}

	got := writeReplacement(t.Context(), plan, ops)
	if got.Outcome != OutcomeCommitIndeterminate {
		t.Fatalf("WriteReplacement outcome = %q, want commit indeterminate", got.Outcome)
	}
	if canonical := transactionReadCanonical(t, plan); !bytes.Equal(canonical, plan.CandidateBytes) {
		t.Fatal("receipt failure did not retain the recovered canonical queue")
	}
	intentBytes, err := os.ReadFile(replaceIntentPath(plan.ProjectDir, plan.NormalizedName))
	if err != nil {
		t.Fatalf("receipt failure removed the replace intent: %v", err)
	}
	if action, err := ClassifyReplaceIntent(plan.ProjectDir, intentBytes); action != ReplacePromoteCanonical || err != nil {
		t.Fatalf("restart classification = (%q, %v), want promote canonical", action, err)
	}
}

func TestRecoverFailedReplaceIntent_InstallsMissingReceiptThenCleansIntent(t *testing.T) {
	t.Parallel()

	plan := transactionRecoveryFixturePlan(t)
	intent, intentBytes, err := prepareReplacementWithIDs(plan, plan.TransactionID, "")
	if err != nil {
		t.Fatal(err)
	}
	qDir := queuesDir(plan.ProjectDir)
	if err := os.MkdirAll(qDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(queuePath(plan.ProjectDir, plan.NormalizedName), plan.CandidateBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replaceIntentPath(plan.ProjectDir, plan.NormalizedName), intentBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	action, err := recoverFailedReplaceIntentBytes(plan.ProjectDir, intentBytes)
	if err != nil || action != ReplacePromoteCanonical {
		t.Fatalf("RecoverFailedReplaceIntent = (%q, %v), want promote canonical", action, err)
	}
	receiptPath := filepath.Join(failedRecoveryReceiptsDir(plan.ProjectDir), intent.FailedRecoveryReceiptBinding.Basename)
	gotReceipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatalf("read installed receipt: %v", err)
	}
	wantReceipt, err := base64.StdEncoding.DecodeString(intent.FailedRecoveryReceiptBinding.CanonicalBytesBase64)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotReceipt, wantReceipt) {
		t.Error("installed receipt bytes differ from the intent binding")
	}
	if _, err := os.Stat(replaceIntentPath(plan.ProjectDir, plan.NormalizedName)); !os.IsNotExist(err) {
		t.Fatalf("replace intent still exists after recovery: %v", err)
	}
}

func TestRecoverFailedReplaceIntent_ExactReceiptCleansIntent(t *testing.T) {
	t.Parallel()

	plan := transactionRecoveryFixturePlan(t)
	intent, intentBytes, err := prepareReplacementWithIDs(plan, plan.TransactionID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(failedRecoveryReceiptsDir(plan.ProjectDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(queuePath(plan.ProjectDir, plan.NormalizedName), plan.CandidateBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replaceIntentPath(plan.ProjectDir, plan.NormalizedName), intentBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	receiptBytes, err := base64.StdEncoding.DecodeString(intent.FailedRecoveryReceiptBinding.CanonicalBytesBase64)
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(failedRecoveryReceiptsDir(plan.ProjectDir), intent.FailedRecoveryReceiptBinding.Basename)
	if err := os.WriteFile(receiptPath, receiptBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	action, err := recoverFailedReplaceIntentBytes(plan.ProjectDir, intentBytes)
	if err != nil || action != ReplacePromoteCanonical {
		t.Fatalf("RecoverFailedReplaceIntent = (%q, %v), want promote canonical", action, err)
	}
	if _, err := os.Stat(replaceIntentPath(plan.ProjectDir, plan.NormalizedName)); !os.IsNotExist(err) {
		t.Fatalf("replace intent still exists after exact-receipt recovery: %v", err)
	}
	if got, err := os.ReadFile(receiptPath); err != nil || !bytes.Equal(got, receiptBytes) {
		t.Fatalf("exact receipt changed after recovery: bytes=%q err=%v", got, err)
	}
}

func TestRecoverFailedReplaceIntent_ConflictingReceiptRefusesWithoutMutation(t *testing.T) {
	t.Parallel()

	plan := transactionRecoveryFixturePlan(t)
	intent, intentBytes, err := prepareReplacementWithIDs(plan, plan.TransactionID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(failedRecoveryReceiptsDir(plan.ProjectDir), 0o700); err != nil {
		t.Fatal(err)
	}
	canonicalPath := queuePath(plan.ProjectDir, plan.NormalizedName)
	if err := os.WriteFile(canonicalPath, plan.CandidateBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	intentPath := replaceIntentPath(plan.ProjectDir, plan.NormalizedName)
	if err := os.WriteFile(intentPath, intentBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(failedRecoveryReceiptsDir(plan.ProjectDir), intent.FailedRecoveryReceiptBinding.Basename)
	conflicting := []byte("conflicting receipt")
	if err := os.WriteFile(receiptPath, conflicting, 0o600); err != nil {
		t.Fatal(err)
	}

	action, err := recoverFailedReplaceIntentBytes(plan.ProjectDir, intentBytes)
	if action != ReplaceRefuse || err == nil {
		t.Fatalf("RecoverFailedReplaceIntent = (%q, %v), want refusal", action, err)
	}
	if got, readErr := os.ReadFile(canonicalPath); readErr != nil || !bytes.Equal(got, plan.CandidateBytes) {
		t.Fatalf("refusal changed canonical evidence: bytes=%q err=%v", got, readErr)
	}
	if got, readErr := os.ReadFile(intentPath); readErr != nil || !bytes.Equal(got, intentBytes) {
		t.Fatalf("refusal changed intent evidence: bytes=%q err=%v", got, readErr)
	}
	if got, readErr := os.ReadFile(receiptPath); readErr != nil || !bytes.Equal(got, conflicting) {
		t.Fatalf("refusal changed receipt evidence: bytes=%q err=%v", got, readErr)
	}
}

func TestRecoverFailedReplaceIntent_CleansUncommittedCandidateAndKeepsPrior(t *testing.T) {
	t.Parallel()

	plan := transactionRecoveryFixturePlan(t)
	intent, intentBytes, err := prepareReplacementWithIDs(plan, plan.TransactionID, "")
	if err != nil {
		t.Fatal(err)
	}
	qDir := queuesDir(plan.ProjectDir)
	if err := os.MkdirAll(qDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(queuePath(plan.ProjectDir, plan.NormalizedName), plan.PriorBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	candidatePath := filepath.Join(qDir, intent.CandidateTempBasename)
	if err := os.WriteFile(candidatePath, plan.CandidateBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replaceIntentPath(plan.ProjectDir, plan.NormalizedName), intentBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	action, err := recoverFailedReplaceIntentBytes(plan.ProjectDir, intentBytes)
	if err != nil || action != ReplaceNotCommitted {
		t.Fatalf("RecoverFailedReplaceIntent = (%q, %v), want not committed", action, err)
	}
	canonical, err := os.ReadFile(queuePath(plan.ProjectDir, plan.NormalizedName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, plan.PriorBytes) {
		t.Error("uncommitted recovery changed the prior queue")
	}
	if _, err := os.Stat(candidatePath); !os.IsNotExist(err) {
		t.Fatalf("candidate remains after not-committed recovery: %v", err)
	}
	if _, err := os.Stat(replaceIntentPath(plan.ProjectDir, plan.NormalizedName)); !os.IsNotExist(err) {
		t.Fatalf("replace intent remains after not-committed recovery: %v", err)
	}
}
