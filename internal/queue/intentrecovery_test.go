package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// intentRecoveryCrashAfterIntent runs a replacement that installs its durable
// intent and then dies, leaving whatever the cut left on disk. It returns the
// plan so a test can address the same queue again.
func intentRecoveryCrashAfterIntent(t *testing.T, cut func(*namespaceOps)) ReplacementPlan {
	t.Helper()
	plan := transactionFixturePlan(t, t.TempDir())
	plan.TransactionID = "0190b3c4-9001-7000-8000-0000000000a1"
	transactionSeedPrior(t, plan)

	ops := osNamespaceOps()
	if cut != nil {
		cut(&ops)
	}
	writeReplacement(context.Background(), plan, ops)
	return plan
}

func intentRecoveryIntentPresent(t *testing.T, plan ReplacementPlan) bool {
	t.Helper()
	_, err := os.Stat(replaceIntentPath(plan.ProjectDir, plan.NormalizedName))
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	t.Fatal(err)
	return false
}

func intentRecoveryOnly(t *testing.T, plan ReplacementPlan) ReplaceIntentRecovery {
	t.Helper()
	recoveries, err := RecoverReplaceIntents(plan.ProjectDir)
	if err != nil {
		t.Fatalf("RecoverReplaceIntents: %v", err)
	}
	if len(recoveries) != 1 {
		t.Fatalf("want exactly one recovery, got %d: %+v", len(recoveries), recoveries)
	}
	return recoveries[0]
}

// cutRename fails the candidate-to-canonical rename, which is the only rename
// writeReplacement performs. The candidate and the intent both survive.
func cutRename(ops *namespaceOps) {
	ops.rename = func(string, string) error { return errors.New("cut candidate rename") }
}

func TestRecoverReplaceIntents_CleanBootFindsNothing(t *testing.T) {
	t.Parallel()

	plan := transactionFixturePlan(t, t.TempDir())
	transactionSeedPrior(t, plan)

	recoveries, err := RecoverReplaceIntents(plan.ProjectDir)
	if err != nil {
		t.Fatalf("RecoverReplaceIntents: %v", err)
	}
	if len(recoveries) != 0 {
		t.Fatalf("a clean boot reported recoveries: %+v", recoveries)
	}
}

func TestRecoverReplaceIntents_MissingQueuesDirIsNotAnError(t *testing.T) {
	t.Parallel()

	recoveries, err := RecoverReplaceIntents(t.TempDir())
	if err != nil {
		t.Fatalf("RecoverReplaceIntents on a project with no queues dir: %v", err)
	}
	if len(recoveries) != 0 {
		t.Fatalf("want no recoveries, got %+v", recoveries)
	}
}

func TestRecoverReplaceIntents_DropsIntentWhenRenameAlreadyLanded(t *testing.T) {
	t.Parallel()

	// A committed replacement leaves the intent for the QueueStore to remove.
	// Crashing before that removal is the promote-canonical state.
	plan := intentRecoveryCrashAfterIntent(t, nil)
	if !intentRecoveryIntentPresent(t, plan) {
		t.Fatal("setup did not leave a replace intent on disk")
	}
	if got := transactionReadCanonical(t, plan); !bytes.Equal(got, plan.CandidateBytes) {
		t.Fatal("setup did not land the candidate as canonical")
	}

	got := intentRecoveryOnly(t, plan)
	if got.Err != nil || got.Action != ReplacePromoteCanonical {
		t.Fatalf("want promote_canonical with no error, got action=%q err=%v", got.Action, got.Err)
	}
	if intentRecoveryIntentPresent(t, plan) {
		t.Fatal("recovery left the intent on disk")
	}
	if canonical := transactionReadCanonical(t, plan); !bytes.Equal(canonical, plan.CandidateBytes) {
		t.Fatal("recovery changed the canonical queue file")
	}
}

func TestRecoverReplaceIntents_RetriesRenameWhenCandidateSurvivedTheCrash(t *testing.T) {
	t.Parallel()

	plan := intentRecoveryCrashAfterIntent(t, cutRename)
	candidatePath := filepath.Join(queuesDir(plan.ProjectDir), plan.NormalizedName+".candidate-"+plan.TransactionID)
	if _, err := os.Stat(candidatePath); err != nil {
		t.Fatalf("setup did not leave a candidate at the basename the intent pins: %v", err)
	}
	if got := transactionReadCanonical(t, plan); !bytes.Equal(got, plan.PriorBytes) {
		t.Fatal("setup should have left the prior bytes canonical")
	}

	got := intentRecoveryOnly(t, plan)
	if got.Err != nil || got.Action != ReplaceRetryRename {
		t.Fatalf("want retry_candidate_rename with no error, got action=%q err=%v", got.Action, got.Err)
	}
	if canonical := transactionReadCanonical(t, plan); !bytes.Equal(canonical, plan.CandidateBytes) {
		t.Fatal("recovery did not finish the rename")
	}
	if _, err := os.Stat(candidatePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery left the candidate behind: %v", err)
	}
	if intentRecoveryIntentPresent(t, plan) {
		t.Fatal("recovery left the intent on disk")
	}
}

func TestRecoverReplaceIntents_RollsBackWhenCandidateNeverLanded(t *testing.T) {
	t.Parallel()

	plan := intentRecoveryCrashAfterIntent(t, cutRename)
	// Remove the candidate to reach the state where the write never reached the
	// queues directory at all.
	entries, err := os.ReadDir(queuesDir(plan.ProjectDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" && e.Name() != plan.NormalizedName+".replace-intent" {
			if rmErr := os.Remove(filepath.Join(queuesDir(plan.ProjectDir), e.Name())); rmErr != nil {
				t.Fatal(rmErr)
			}
		}
	}

	got := intentRecoveryOnly(t, plan)
	if got.Err != nil || got.Action != ReplaceNotCommitted {
		t.Fatalf("want not_committed_cleanup with no error, got action=%q err=%v", got.Action, got.Err)
	}
	if canonical := transactionReadCanonical(t, plan); !bytes.Equal(canonical, plan.PriorBytes) {
		t.Fatal("rollback did not leave the prior bytes canonical")
	}
	if intentRecoveryIntentPresent(t, plan) {
		t.Fatal("rollback left the intent on disk")
	}
}

func TestRecoverReplaceIntents_ReportsAReaddirFailureRatherThanClaimingACleanBoot(t *testing.T) {
	t.Parallel()

	plan := transactionFixturePlan(t, t.TempDir())
	transactionSeedPrior(t, plan)
	ops := osNamespaceOps()
	ops.readDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("cut readdir") }

	if _, err := recoverReplaceIntents(plan.ProjectDir, ops); err == nil {
		t.Fatal("a readdir failure was reported as a clean boot")
	}
}

func TestRecoverReplaceIntents_ACorruptIntentDoesNotStopTheOthers(t *testing.T) {
	t.Parallel()

	// One queue crashed mid-replace and is recoverable. A second queue's intent
	// is unreadable. The recoverable one must still be resolved.
	good := intentRecoveryCrashAfterIntent(t, nil)
	badIntentPath := filepath.Join(queuesDir(good.ProjectDir), "other.replace-intent")
	if err := os.WriteFile(badIntentPath, []byte(`{"schema_version":1,`), 0o600); err != nil {
		t.Fatal(err)
	}

	recoveries, err := RecoverReplaceIntents(good.ProjectDir)
	if err != nil {
		t.Fatalf("RecoverReplaceIntents: %v", err)
	}
	if len(recoveries) != 2 {
		t.Fatalf("want a report for both intents, got %d: %+v", len(recoveries), recoveries)
	}

	byName := make(map[string]ReplaceIntentRecovery, len(recoveries))
	for _, r := range recoveries {
		byName[r.NormalizedName] = r
	}
	if got := byName[good.NormalizedName]; !got.Resolved() {
		t.Fatalf("the recoverable queue was not resolved: action=%q err=%v", got.Action, got.Err)
	}
	if got := byName["other"]; got.Err == nil {
		t.Fatal("the corrupt intent was reported as resolved")
	}
	if intentRecoveryIntentPresent(t, good) {
		t.Fatal("the recoverable queue's intent is still on disk")
	}
	if _, statErr := os.Stat(badIntentPath); statErr != nil {
		t.Fatalf("the corrupt intent was removed: %v", statErr)
	}
}

func TestRecoverReplaceIntents_RefusesAnIntentFiledUnderTheWrongQueueName(t *testing.T) {
	t.Parallel()

	crashed := intentRecoveryCrashAfterIntent(t, nil)
	intentBytes, err := os.ReadFile(replaceIntentPath(crashed.ProjectDir, crashed.NormalizedName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(replaceIntentPath(crashed.ProjectDir, crashed.NormalizedName)); err != nil {
		t.Fatal(err)
	}
	misfiled := filepath.Join(queuesDir(crashed.ProjectDir), "somewhereelse.replace-intent")
	if err := os.WriteFile(misfiled, intentBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	recoveries, err := RecoverReplaceIntents(crashed.ProjectDir)
	if err != nil {
		t.Fatalf("RecoverReplaceIntents: %v", err)
	}
	if len(recoveries) != 1 || recoveries[0].Err == nil {
		t.Fatalf("a misfiled intent was accepted: %+v", recoveries)
	}
	if _, statErr := os.Stat(misfiled); statErr != nil {
		t.Fatalf("the misfiled intent was removed: %v", statErr)
	}
}

func TestRecoverReplaceIntents_LeavesACorruptIntentExactlyWhereItWas(t *testing.T) {
	t.Parallel()

	plan := transactionFixturePlan(t, t.TempDir())
	transactionSeedPrior(t, plan)
	intentPath := replaceIntentPath(plan.ProjectDir, plan.NormalizedName)
	corrupt := []byte(`{"schema_version":1,`)
	if err := os.WriteFile(intentPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	got := intentRecoveryOnly(t, plan)
	if got.Err == nil {
		t.Fatal("a corrupt intent was reported as resolved")
	}
	if got.Resolved() {
		t.Fatal("Resolved() is true for a refusal")
	}
	if got.NormalizedName != plan.NormalizedName {
		t.Fatalf("refusal did not name the queue: %q", got.NormalizedName)
	}
	after, err := os.ReadFile(intentPath) //nolint:gosec // G304: path is t.TempDir-derived
	if err != nil {
		t.Fatalf("refusal removed the intent: %v", err)
	}
	if !bytes.Equal(after, corrupt) {
		t.Fatal("refusal rewrote the intent")
	}
	if canonical := transactionReadCanonical(t, plan); !bytes.Equal(canonical, plan.PriorBytes) {
		t.Fatal("refusal touched the canonical queue file")
	}
}

func TestRecoverReplaceIntents_RefusesAnArchiveHandoffIntent(t *testing.T) {
	t.Parallel()

	plan := transactionFixturePlan(t, t.TempDir())
	plan.TransactionID = "0190b3c4-9001-7000-8000-0000000000c3"
	plan.OperationKind = OperationCancellation
	var candidate Queue
	if err := json.Unmarshal(plan.CandidateBytes, &candidate); err != nil {
		t.Fatal(err)
	}
	candidate.Status = QueueStatusCancelled
	candidateBytes, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	plan.CandidateBytes = candidateBytes
	plan.ArchiveHandoff = &ArchiveHandoffPlan{
		ArchiveOrigin:       "operator-cancel",
		ArchiveKind:         "cancelled",
		SourceIdentity:      plan.QueueID,
		DestinationBasename: "main.json.cancelled-fixed",
	}
	transactionSeedPrior(t, plan)

	commit := WriteReplacement(context.Background(), plan)
	if !commit.Committed() {
		t.Fatalf("setup replacement did not commit: %v", commit.Err)
	}
	if !intentRecoveryIntentPresent(t, plan) {
		t.Fatal("a cancellation is supposed to leave its intent for the linked continuation")
	}

	got := intentRecoveryOnly(t, plan)
	if !errors.Is(got.Err, ErrArchiveHandoffIntentNotRecoverable) {
		t.Fatalf("want ErrArchiveHandoffIntentNotRecoverable, got %v", got.Err)
	}
	if !intentRecoveryIntentPresent(t, plan) {
		t.Fatal("the sweep removed an archive-handoff intent the continuation still needs")
	}
}

// TestRecoverReplaceIntents_UnwedgesTheQueueAfterACrash is the claim this whole
// file defends. It proves the wedge is real before it proves the sweep clears
// it: without recovery the next replacement is refused and the queue is stuck,
// and after recovery the very same replacement commits.
func TestRecoverReplaceIntents_UnwedgesTheQueueAfterACrash(t *testing.T) {
	t.Parallel()

	crashed := intentRecoveryCrashAfterIntent(t, nil)
	if !intentRecoveryIntentPresent(t, crashed) {
		t.Fatal("setup did not leave a replace intent on disk")
	}

	// The next replacement carries a different transaction, so its intent bytes
	// differ from the leftover one. Each attempt gets its own transaction ID,
	// the way PrepareFailedRecovery allocates one per call in production.
	attempt := func(transactionID string) ReplacementPlan {
		p := transactionFixturePlan(t, crashed.ProjectDir)
		p.TransactionID = transactionID
		p.PriorBytes = crashed.CandidateBytes
		return p
	}

	blocked := WriteReplacement(context.Background(), attempt("0190b3c4-9001-7000-8000-0000000000b2"))
	if blocked.Committed() {
		t.Fatal("the leftover intent did not block the next replacement; " +
			"this test can no longer detect the wedge it exists to defend against")
	}
	if blocked.Outcome != OutcomeCommitIndeterminate {
		t.Fatalf("want the wedge to surface as an indeterminate commit, got %q", blocked.Outcome)
	}

	got := intentRecoveryOnly(t, crashed)
	if !got.Resolved() {
		t.Fatalf("recovery refused: action=%q err=%v", got.Action, got.Err)
	}

	retry := attempt("0190b3c4-9001-7000-8000-0000000000b3")
	unblocked := WriteReplacement(context.Background(), retry)
	if !unblocked.Committed() {
		t.Fatalf("the replacement is still refused after recovery: %v", unblocked.Err)
	}
	if canonical := transactionReadCanonical(t, retry); !bytes.Equal(canonical, retry.CandidateBytes) {
		t.Fatal("the unblocked replacement did not land its candidate")
	}
}
