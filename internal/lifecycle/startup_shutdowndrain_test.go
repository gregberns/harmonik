package lifecycle

import (
	"log/slog"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

func TestLoadQueueAtStartupResumesCleanShutdownBeforeClassD(t *testing.T) {
	projectDir := t.TempDir()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0197c453-0000-7000-8000-000000000101",
		Name:          queue.QueueNameMain,
		SubmittedAt:   time.Now().UTC(),
		Status:        queue.QueueStatusPausedByDrain,
		ResumeOnStart: true,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindStream,
			Status:     queue.GroupStatusActive,
			Items: []queue.Item{{
				BeadID: core.BeadID("hk-shutdown-resume"),
				Status: queue.ItemStatusPending,
			}},
			CreatedAt: time.Now().UTC(),
		}},
	}
	if err := queue.Persist(t.Context(), projectDir, q); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadQueueAtStartup(
		t.Context(), projectDir, emptyBeadLedger{}, nil, slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded queues = %d; want 1", len(loaded))
	}
	got := loaded[0]
	if got.Status != queue.QueueStatusActive || got.ResumeOnStart {
		t.Fatalf("queue after startup = status %q resume_on_start=%v; want active/false", got.Status, got.ResumeOnStart)
	}
	if got.Groups[0].Items[0].Status != queue.ItemStatusPending {
		t.Fatalf("pending item after startup = %q; want pending", got.Groups[0].Items[0].Status)
	}

	durable, err := queue.Load(t.Context(), projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatal(err)
	}
	if durable == nil || durable.Status != queue.QueueStatusActive || durable.ResumeOnStart {
		t.Fatalf("durable queue after startup = %+v; want active with cleared restart intent", durable)
	}
}

func TestLoadQueueAtStartupDoesNotResumeExplicitOperatorPause(t *testing.T) {
	projectDir := t.TempDir()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0197c453-0000-7000-8000-000000000102",
		Name:          queue.QueueNameMain,
		SubmittedAt:   time.Now().UTC(),
		Status:        queue.QueueStatusPausedByDrain,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindStream,
			Status:     queue.GroupStatusActive,
			Items: []queue.Item{{
				BeadID: core.BeadID("hk-operator-pause"),
				Status: queue.ItemStatusPending,
			}},
			CreatedAt: time.Now().UTC(),
		}},
	}
	if err := queue.Persist(t.Context(), projectDir, q); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadQueueAtStartup(
		t.Context(), projectDir, emptyBeadLedger{}, nil, slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Status != queue.QueueStatusPausedByDrain {
		t.Fatalf("explicit pause after startup = %+v; want paused-by-drain", loaded)
	}
	if loaded[0].Groups[0].Items[0].Status != queue.ItemStatusPending {
		t.Fatalf("explicit-pause item = %q; want pending for later operator resume", loaded[0].Groups[0].Items[0].Status)
	}
}
