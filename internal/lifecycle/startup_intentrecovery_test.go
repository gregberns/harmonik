package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

func TestLoadQueueAtStartupInstallsMarkerForReceiptOnlyCrashState(t *testing.T) {
	projectDir := t.TempDir()
	completedAt := time.Date(2026, 8, 10, 18, 12, 13, 456000000, time.UTC)
	q := queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0197c453-0000-7000-8000-000000000001",
		Name:          queue.QueueNameMain,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex:  0,
			Kind:        queue.GroupKindStream,
			Status:      queue.GroupStatusCompleteSuccess,
			CompletedAt: &completedAt,
			Items: []queue.Item{{
				BeadID: core.BeadID("hk-marker-restart"),
				Status: queue.ItemStatusCompleted,
			}},
		}},
	}
	if err := queue.Persist(t.Context(), projectDir, &q); err != nil {
		t.Fatal(err)
	}
	prepared, err := queue.PrepareCompletion(
		q,
		"0197c453-0000-7000-8000-000000000002",
		"0197c453-0000-7000-8000-000000000003",
		completedAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	priorBytes, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	commit := queue.WriteReplacement(t.Context(), queue.ReplacementPlan{
		ProjectDir:               projectDir,
		TransactionID:            prepared.TransactionID,
		OperationKind:            queue.OperationCompletion,
		NormalizedName:           q.Name,
		QueueID:                  q.QueueID,
		PriorBytes:               priorBytes,
		CandidateBytes:           prepared.CandidateBytes,
		CompletionReceiptBinding: prepared.Binding,
	})
	if !commit.Committed() {
		t.Fatalf("completion commit = %+v", commit)
	}
	if _, err := queue.RecoverReplaceIntents(projectDir); err != nil {
		t.Fatal(err)
	}
	markerBase, err := queue.CompletionReleaseMarkerBasename(q.QueueID, prepared.Receipt.ReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(projectDir, ".harmonik", "queues", ".completion-receipts", markerBase)
	if _, err := os.Stat(markerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture already has marker: %v", err)
	}

	loaded, err := LoadQueueAtStartup(
		t.Context(), projectDir, emptyBeadLedger{}, nil, slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 {
		t.Fatalf("completed queue loaded after cleanup: %+v", loaded)
	}
	data, err := os.ReadFile(markerPath) //nolint:gosec // path is under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	marker, err := queue.DecodeCompletionReleaseMarker(data)
	if err != nil || marker.ReceiptID != prepared.Receipt.ReceiptID {
		t.Fatalf("marker = %+v, err=%v", marker, err)
	}
}

func TestCompletionFaultStateConvergesThroughPublicStartup(t *testing.T) {
	projectDir := t.TempDir()
	completedAt := time.Date(2026, 8, 11, 9, 0, 0, 123000000, time.UTC)
	q := queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0197c453-0000-7000-8000-000000000011",
		Name:          queue.QueueNameMain,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindStream,
			Status:     queue.GroupStatusActive,
			Items:      []queue.Item{{BeadID: core.BeadID("hk-startup-completion"), Status: queue.ItemStatusDispatched}},
		}},
	}
	if err := queue.Persist(t.Context(), projectDir, &q); err != nil {
		t.Fatal(err)
	}
	input := queue.GroupCompletionInput{
		ExpectedQueueID:     q.QueueID,
		Location:            queue.GroupCompletionLocation{GroupIndex: 0, ItemIndex: 0},
		Outcome:             queue.GroupCompletionOutcomeCompleted,
		CompletedAt:         completedAt,
		CompletionReceiptID: "0197c453-0000-7000-8000-000000000013",
	}
	decision, err := queue.DecideGroupCompletion(q, input)
	if err != nil || decision.Disposition != queue.GroupCompletionDispositionQueueCompleted {
		t.Fatalf("completion decision = %+v, err=%v", decision, err)
	}
	prepared, err := queue.PrepareCompletion(
		*decision.NextQueue,
		"0197c453-0000-7000-8000-000000000012",
		input.CompletionReceiptID,
		completedAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	priorBytes, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	commit := queue.WriteReplacement(t.Context(), queue.ReplacementPlan{
		ProjectDir:               projectDir,
		TransactionID:            prepared.TransactionID,
		OperationKind:            queue.OperationCompletion,
		NormalizedName:           q.Name,
		QueueID:                  q.QueueID,
		PriorBytes:               priorBytes,
		CandidateBytes:           prepared.CandidateBytes,
		CompletionReceiptBinding: prepared.Binding,
	})
	if !commit.Committed() || commit.Phase != queue.CompletionPhaseReceiptDurable {
		t.Fatalf("completion fault state = %+v", commit)
	}
	intentPath := filepath.Join(projectDir, ".harmonik", "queues", "main.replace-intent")
	if _, err := os.Stat(intentPath); err != nil {
		t.Fatalf("fault state has no intent: %v", err)
	}
	emitter := &bootRevertEmitter{projectDir: projectDir}
	loaded, err := LoadQueueAtStartup(t.Context(), projectDir, emptyBeadLedger{}, emitter, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 {
		t.Fatalf("completed queue loaded after convergence: %+v", loaded)
	}
	if len(emitter.types) != 0 {
		t.Fatalf("startup replayed completion events: %v", emitter.types)
	}
	for _, path := range []string{filepath.Join(projectDir, ".harmonik", "queues", "main.json"), intentPath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("startup retained resolved path %s: %v", path, err)
		}
	}
	receiptPath := filepath.Join(projectDir, ".harmonik", "queues", ".completion-receipts", prepared.Binding.Basename)
	receiptBytes, err := os.ReadFile(receiptPath) //nolint:gosec // path is under t.TempDir and uses a validated basename.
	if err != nil || !bytes.Equal(receiptBytes, prepared.ReceiptBytes) {
		t.Fatalf("receipt changed: err=%v", err)
	}
	markerBase, err := queue.CompletionReleaseMarkerBasename(q.QueueID, input.CompletionReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	markerBytes, err := os.ReadFile(filepath.Join(projectDir, ".harmonik", "queues", ".completion-receipts", markerBase)) //nolint:gosec // path is under t.TempDir and uses validated IDs.
	if err != nil {
		t.Fatal(err)
	}
	marker, err := queue.DecodeCompletionReleaseMarker(markerBytes)
	if err != nil || marker.ReceiptID != input.CompletionReceiptID || marker.CompletedQueueSHA256 != prepared.Receipt.CompletedQueueSHA256 {
		t.Fatalf("marker = %+v, err=%v", marker, err)
	}
}

func TestLoadQueueAtStartupRefusesInvalidCompletionReceiptRoot(t *testing.T) {
	projectDir := t.TempDir()
	receiptRoot := filepath.Join(projectDir, ".harmonik", "queues", ".completion-receipts")
	if err := os.MkdirAll(filepath.Dir(receiptRoot), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptRoot, []byte("wrong type"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQueueAtStartup(
		t.Context(), projectDir, emptyBeadLedger{}, nil, slog.New(slog.DiscardHandler),
	); err == nil {
		t.Fatal("startup accepted an invalid completion receipt root")
	}
}

type emptyBeadLedger struct{}

func (emptyBeadLedger) ShowBead(context.Context, core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{}, errors.New("no ledger in this test")
}

func (emptyBeadLedger) ListInFlightBeads(context.Context) ([]core.BeadRecord, error) {
	return nil, nil
}

func crashedReplaceFixture(t *testing.T) (projectDir, intentPath string) {
	t.Helper()
	projectDir = t.TempDir()

	priorQueue := queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0001",
		Name:          queue.QueueNameMain,
		Status:        queue.QueueStatusActive,
		Groups:        []queue.Group{},
	}
	priorBytes, err := json.Marshal(priorQueue)
	if err != nil {
		t.Fatal(err)
	}
	candidateQueue := priorQueue
	candidateQueue.Status = queue.QueueStatusPausedByDrain
	candidateBytes, err := json.Marshal(candidateQueue)
	if err != nil {
		t.Fatal(err)
	}

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.MkdirAll(queuesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(queuesDir, "main.json"), priorBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	commit := queue.WriteReplacement(context.Background(), queue.ReplacementPlan{
		ProjectDir:     projectDir,
		TransactionID:  "0190b3c4-9001-7000-8000-0000000000d4",
		OperationKind:  queue.OperationPause,
		NormalizedName: queue.QueueNameMain,
		QueueID:        priorQueue.QueueID,
		PriorBytes:     priorBytes,
		CandidateBytes: candidateBytes,
	})
	if !commit.Committed() {
		t.Fatalf("fixture replacement did not commit: %v", commit.Err)
	}

	intentPath = filepath.Join(queuesDir, "main.replace-intent")
	if _, err := os.Stat(intentPath); err != nil {
		t.Fatalf("fixture did not leave a replace intent: %v", err)
	}
	return projectDir, intentPath
}

// TestLoadQueueAtStartup_RollsForwardBeforeItLoads is the test that pins WHERE
// the sweep is called, not just that it is called. In the retry-rename state the
// canonical file still holds the prior bytes, so a sweep that ran after the load
// loop would hand back the prior queue and roll the file forward behind it. The
// two tests below cannot tell those orders apart; this one can.
func TestLoadQueueAtStartup_RollsForwardBeforeItLoads(t *testing.T) {
	t.Parallel()

	projectDir, intentPath := crashedReplaceFixture(t)
	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	canonicalPath := filepath.Join(queuesDir, "main.json")

	committed, err := os.ReadFile(canonicalPath) //nolint:gosec // G304: path is t.TempDir-derived
	if err != nil {
		t.Fatal(err)
	}
	var q queue.Queue
	if err := json.Unmarshal(committed, &q); err != nil {
		t.Fatal(err)
	}
	q.Status = queue.QueueStatusActive
	prior, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonicalPath, prior, 0o600); err != nil {
		t.Fatal(err)
	}
	candidatePath := filepath.Join(queuesDir, "main.candidate-0190b3c4-9001-7000-8000-0000000000d4")
	if err := os.WriteFile(candidatePath, committed, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadQueueAtStartup(
		context.Background(),
		projectDir,
		emptyBeadLedger{},
		nil,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatalf("LoadQueueAtStartup: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("want one loaded queue, got %d", len(loaded))
	}
	if loaded[0].Status != queue.QueueStatusPausedByDrain {
		t.Fatalf("startup loaded the pre-recovery queue (%q); the sweep ran too late",
			loaded[0].Status)
	}
	if _, statErr := os.Stat(candidatePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the candidate was not consumed: %v", statErr)
	}
	if _, statErr := os.Stat(intentPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the intent was not cleared: %v", statErr)
	}
}

func TestPrepareQueueNamespaceAtStartupSettlesTransactionsBeforeFactReads(t *testing.T) {
	t.Parallel()

	projectDir, intentPath := crashedReplaceFixture(t)
	if err := PrepareQueueNamespaceAtStartup(
		context.Background(),
		projectDir,
		slog.New(slog.DiscardHandler),
	); err != nil {
		t.Fatalf("PrepareQueueNamespaceAtStartup() = %v", err)
	}

	loaded, err := queue.Load(context.Background(), projectDir, "main")
	if err != nil {
		t.Fatalf("queue.Load() after namespace preparation = %v", err)
	}
	if loaded.Status != queue.QueueStatusPausedByDrain {
		t.Fatalf("queue fact after namespace preparation = %q", loaded.Status)
	}
	if _, err := os.Stat(intentPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replace intent remains after namespace preparation: %v", err)
	}
}

func TestLoadQueueAtStartup_ResolvesAReplaceIntentLeftByACrash(t *testing.T) {
	t.Parallel()

	projectDir, intentPath := crashedReplaceFixture(t)

	loaded, err := LoadQueueAtStartup(
		context.Background(),
		projectDir,
		emptyBeadLedger{},
		nil,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatalf("LoadQueueAtStartup: %v", err)
	}

	if _, statErr := os.Stat(intentPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("startup left the replace intent on disk: %v", statErr)
	}

	if len(loaded) != 1 {
		t.Fatalf("want one loaded queue, got %d", len(loaded))
	}
	if loaded[0].Status != queue.QueueStatusPausedByDrain {
		t.Fatalf("want the committed candidate state, got %q", loaded[0].Status)
	}
}

func TestLoadQueueAtStartup_UnwedgesTheQueueForLaterWrites(t *testing.T) {
	t.Parallel()

	projectDir, _ := crashedReplaceFixture(t)

	nextPlan := func(transactionID string) queue.ReplacementPlan {
		prior, err := os.ReadFile(filepath.Join(projectDir, ".harmonik", "queues", "main.json")) //nolint:gosec // G304: path is t.TempDir-derived
		if err != nil {
			t.Fatal(err)
		}
		var q queue.Queue
		if err := json.Unmarshal(prior, &q); err != nil {
			t.Fatal(err)
		}
		q.Status = queue.QueueStatusActive
		candidate, err := json.Marshal(q)
		if err != nil {
			t.Fatal(err)
		}
		return queue.ReplacementPlan{
			ProjectDir:     projectDir,
			TransactionID:  transactionID,
			OperationKind:  queue.OperationResume,
			NormalizedName: queue.QueueNameMain,
			QueueID:        q.QueueID,
			PriorBytes:     prior,
			CandidateBytes: candidate,
		}
	}

	blocked := queue.WriteReplacement(context.Background(), nextPlan("0190b3c4-9001-7000-8000-0000000000e5"))
	if blocked.Committed() {
		t.Fatal("the leftover intent did not block a later write; " +
			"this test can no longer detect the wedge it exists to defend against")
	}

	if _, err := LoadQueueAtStartup(
		context.Background(),
		projectDir,
		emptyBeadLedger{},
		nil,
		slog.New(slog.DiscardHandler),
	); err != nil {
		t.Fatalf("LoadQueueAtStartup: %v", err)
	}

	unblocked := queue.WriteReplacement(context.Background(), nextPlan("0190b3c4-9001-7000-8000-0000000000f6"))
	if !unblocked.Committed() {
		t.Fatalf("the queue is still wedged after startup: %v", unblocked.Err)
	}
}

func TestLoadQueueAtStartupRefusesUnresolvedReplaceIntent(t *testing.T) {
	projectDir := t.TempDir()
	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.MkdirAll(queuesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	intentPath := filepath.Join(queuesDir, "main.replace-intent")
	corrupt := []byte(`{"operation_kind":"completion"}`)
	if err := os.WriteFile(intentPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadQueueAtStartup(
		context.Background(),
		projectDir,
		emptyBeadLedger{},
		nil,
		slog.New(slog.DiscardHandler),
	)
	if err == nil || loaded != nil {
		t.Fatalf("startup result = (%+v, %v), want fail closed", loaded, err)
	}
	got, readErr := os.ReadFile(intentPath) //nolint:gosec // path is under t.TempDir
	if readErr != nil || !bytes.Equal(got, corrupt) {
		t.Fatalf("startup changed refused intent = %q, err=%v", got, readErr)
	}
}
