package daemon_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

type dropFixtureLedger struct {
	status map[core.BeadID]queue.BeadStatus
}

func (l dropFixtureLedger) LookupStatus(_ context.Context, id core.BeadID) (queue.BeadStatus, error) {
	if got, ok := l.status[id]; ok {
		return got, nil
	}
	return queue.BeadStatusNotFound, nil
}

// TestSocketRouting_QueueDrop_IsRegistered pins the registration itself, the
// same way TestSocketRouting_QueueRecover_IsRegistered pins queue-recover: a
// dropped Register call reads back as the unknown-op envelope, not this one.
func TestSocketRouting_QueueDrop_IsRegistered(t *testing.T) {
	t.Parallel()
	sockPath := recoverFixtureServe(t, daemon.SocketHandlers{})

	resp := recoverFixtureSend(t, sockPath, map[string]string{"op": "queue-drop", "queue": "main"})
	if resp.Ok {
		t.Fatal("queue-drop with no handler must not report success")
	}
	if resp.Error == `daemon: unknown op "queue-drop"` {
		t.Fatal("queue-drop is NOT registered on the socket router — the CLI verb dead-ends")
	}
	if resp.Error != "daemon: QueueRecoveryHandler not registered" {
		t.Fatalf("error = %q, want the QueueRecoveryHandler-not-registered envelope", resp.Error)
	}
}

// TestSocketRouting_QueueDrop_ArchivesAndClearsWhenBeadIsClosed is the
// disposal path this bead asks for: the failed item's bead already finished
// on another queue (so it reads as not_found here — BRQueueLedger.LookupStatus
// collapses closed beads to not_found by design), and drop must archive the
// queue file and reap the in-memory slot WITHOUT re-arming anything.
func TestSocketRouting_QueueDrop_ArchivesAndClearsWhenBeadIsClosed(t *testing.T) {
	t.Parallel()
	store, projectDir := recoverFixtureStore(t)
	ledger := dropFixtureLedger{status: map[core.BeadID]queue.BeadStatus{
		"hk-canary": queue.BeadStatusNotFound,
	}}
	sockPath := recoverFixtureServe(t, daemon.SocketHandlers{
		Recovery: daemon.NewQueueRecoveryController(store, projectDir, ledger),
	})

	resp := recoverFixtureSend(t, sockPath, map[string]string{"op": "queue-drop", "queue": "canary"})
	if !resp.Ok {
		t.Fatalf("queue-drop: %q (code %d)", resp.Error, resp.ErrorCode)
	}

	var result daemon.QueueDropResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.DroppedCount != 1 || len(result.Dropped) != 1 || result.Dropped[0] != "hk-canary" {
		t.Fatalf("dropped = %v (count %d), want [hk-canary]", result.Dropped, result.DroppedCount)
	}
	if result.ArchivePath == "" {
		t.Fatal("archive_path is empty; the queue file was not archived")
	}
	if _, err := os.Stat(result.ArchivePath); err != nil {
		t.Fatalf("archived file does not exist: %v", err)
	}

	if got := store.QueueByName("canary"); got != nil {
		t.Fatalf("queue is still loaded after drop: %+v", got)
	}
	livePath := filepath.Join(projectDir, ".harmonik", "queues", "canary.json")
	if _, err := os.Stat(livePath); !os.IsNotExist(err) {
		t.Fatalf("live queue.json still present at %s: err=%v", livePath, err)
	}
}

// TestSocketRouting_QueueDrop_RefusesAnOpenBead defends the one guard drop
// must never bypass: a failure whose bead is still open or in_progress is a
// live failure, not stale residue, and dropping it would hide that failure
// rather than dispose of it.
func TestSocketRouting_QueueDrop_RefusesAnOpenBead(t *testing.T) {
	t.Parallel()
	store, projectDir := recoverFixtureStore(t)
	ledger := dropFixtureLedger{status: map[core.BeadID]queue.BeadStatus{
		"hk-canary": queue.BeadStatusOpen,
	}}
	sockPath := recoverFixtureServe(t, daemon.SocketHandlers{
		Recovery: daemon.NewQueueRecoveryController(store, projectDir, ledger),
	})

	resp := recoverFixtureSend(t, sockPath, map[string]string{"op": "queue-drop", "queue": "canary"})
	if resp.Ok {
		t.Fatal("queue-drop against an open bead reported success; drop must refuse to hide a live failure")
	}
	if resp.ErrorCode != queue.ErrorCodeDropBeadNotClosed {
		t.Fatalf("error_code = %d, want %d (error %q)", resp.ErrorCode, queue.ErrorCodeDropBeadNotClosed, resp.Error)
	}
	if got := store.QueueByName("canary"); got == nil || got.Status != queue.QueueStatusPausedByFailure {
		t.Fatalf("queue must be left untouched on refusal, got %+v", got)
	}
}

// TestSocketRouting_QueueDrop_RefusesWithNoLedgerWired pins that drop fails
// closed rather than silently skip its one safety check when no bead ledger
// is wired — unlike recover, which is allowed to skip its preflight in that
// case because re-arming is reversible; archiving is not.
func TestSocketRouting_QueueDrop_RefusesWithNoLedgerWired(t *testing.T) {
	t.Parallel()
	store, projectDir := recoverFixtureStore(t)
	sockPath := recoverFixtureServe(t, daemon.SocketHandlers{
		Recovery: daemon.NewQueueRecoveryController(store, projectDir, nil),
	})

	resp := recoverFixtureSend(t, sockPath, map[string]string{"op": "queue-drop", "queue": "canary"})
	if resp.Ok {
		t.Fatal("queue-drop with no ledger wired reported success")
	}
	if resp.ErrorCode != queue.ErrorCodeDropLedgerUnavailable {
		t.Fatalf("error_code = %d, want %d (error %q)", resp.ErrorCode, queue.ErrorCodeDropLedgerUnavailable, resp.Error)
	}
}

// TestSocketRouting_QueueDrop_RefusesTrailingGroups pins the scope limit: a
// queue whose failure sits ahead of a still-pending group must not be
// dropped — the pending group is real work, and archiving the whole queue
// file would discard it silently.
func TestSocketRouting_QueueDrop_RefusesTrailingGroups(t *testing.T) {
	t.Parallel()
	store, projectDir := recoverFixtureStore(t)
	q := store.QueueByName("canary")
	q.Groups = append(q.Groups, queue.Group{
		GroupIndex: 1,
		Kind:       queue.GroupKindWave,
		Status:     queue.GroupStatusPending,
		Items: []queue.Item{{
			BeadID: "hk-canary-2",
			Status: queue.ItemStatusPending,
		}},
	})
	store.SetQueueByName("canary", q)
	ledger := dropFixtureLedger{status: map[core.BeadID]queue.BeadStatus{
		"hk-canary": queue.BeadStatusNotFound,
	}}
	sockPath := recoverFixtureServe(t, daemon.SocketHandlers{
		Recovery: daemon.NewQueueRecoveryController(store, projectDir, ledger),
	})

	resp := recoverFixtureSend(t, sockPath, map[string]string{"op": "queue-drop", "queue": "canary"})
	if resp.Ok {
		t.Fatal("queue-drop with a pending group behind the failure reported success")
	}
	if resp.ErrorCode != queue.ErrorCodeDropTrailingGroups {
		t.Fatalf("error_code = %d, want %d (error %q)", resp.ErrorCode, queue.ErrorCodeDropTrailingGroups, resp.Error)
	}
	if got := store.QueueByName("canary"); got == nil || len(got.Groups) != 2 {
		t.Fatalf("queue must be left untouched on refusal, got %+v", got)
	}
}
