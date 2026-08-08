package lifecycle

// startup_intentrecovery_test.go — LoadQueueAtStartup resolves the durable
// replace intents a crash left behind, before it reads any queue file.
//
// The unit-level behaviour of the sweep belongs to internal/queue. What this
// file defends is the wiring: that startup calls it at all, that it calls it
// before this pass reads a queue file, and that a queue wedged by a leftover
// intent accepts writes again once the daemon is up.
//
// "Before this pass" is the honest bound and not "before anything reads a queue
// file". The daemon's boot reconcile reads every queue file earlier still. See
// the note on queue.RecoverReplaceIntents.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// emptyBeadLedger answers every cross-check with "nothing here". The queues in
// this file hold no dispatched or in-flight items, so no answer it gives can
// change the outcome under test.
type emptyBeadLedger struct{}

func (emptyBeadLedger) ShowBead(context.Context, core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{}, errors.New("no ledger in this test")
}

func (emptyBeadLedger) ListInFlightBeads(context.Context) ([]core.BeadRecord, error) {
	return nil, nil
}

// crashedReplaceFixture seeds a project whose queue committed a replacement and
// then died before the intent was removed — the state the QueueStore normally
// cleans up after WriteReplacement returns.
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

	// Rewind the committed fixture to the state where the candidate was durable
	// but the rename had not happened: canonical back to prior, candidate temp
	// present, intent untouched.
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

	// Positive evidence that the sweep resolved rather than merely deleted: the
	// queue startup loaded is the committed candidate, not the prior state.
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

	// Prove the wedge first. A different transaction for the same queue is
	// refused while the leftover intent is on disk.
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
