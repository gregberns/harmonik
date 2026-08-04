package daemon_test

// socket_queuerecover_test.go — the `queue-recover` op over a live daemon socket.
//
// Claims defended:
//   - `queue-recover` is registered on the daemon socket router, so the CLI verb
//     does not dead-end
//   - a recover request against a paused-by-failure queue returns the queue to
//     active over the wire and re-arms the failed items
//   - each refusal carries its QM-052b wire code in error_code
//   - `operator-resume` aimed at a failure-parked queue is refused rather than
//     reported as a success that dispatches nothing
//
// Spec ref: specs/queue-model.md §8.3b QM-052b; specs/process-lifecycle.md
// §4.4 PL-003a.

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

// recoverFixtureStore builds a store holding one paused-by-failure queue named
// name, plus a project directory whose canonical file matches.
// recoverFixtureQueueName is the queue every fixture in this file parks.
const recoverFixtureQueueName = "canary"

func recoverFixtureStore(t *testing.T) (store *queuewiring.QueueStore, projectDir string) {
	t.Helper()
	name := recoverFixtureQueueName
	projectDir = t.TempDir()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0200",
		Name:          name,
		Status:        queue.QueueStatusPausedByFailure,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusCompleteWithFailures,
			CreatedAt:  time.Unix(0, 0).UTC(),
			Items: []queue.Item{{
				BeadID:            "hk-canary",
				Status:            queue.ItemStatusFailed,
				Attempts:          queue.MaxItemAttempts,
				LastFailureReason: "max_attempts_exceeded",
			}},
		}},
	}
	store = queuewiring.NewQueueStore()
	store.SetQueueByName(name, q)

	data, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projectDir, ".harmonik", "queues", name+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return store, projectDir
}

// recoverFixtureServe starts a live socket carrying the supplied handlers.
func recoverFixtureServe(t *testing.T, handlers daemon.SocketHandlers) string {
	t.Helper()
	sockPath := socketFixtureTempSockPath(t)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		_ = daemon.Serve(ctx, sockPath, handlers) //nolint:errcheck // asserted via the response
	}()
	t.Cleanup(cancel)
	socketFixtureWaitReady(t, sockPath)
	return sockPath
}

// recoverFixtureSend writes one request object to sockPath and decodes the reply.
func recoverFixtureSend(t *testing.T, sockPath string, req map[string]string) daemon.SocketResponse {
	t.Helper()
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("dial %q: %v", sockPath, err)
	}
	defer func() { _ = conn.Close() }()

	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, writeErr := conn.Write(payload); writeErr != nil {
		t.Fatalf("write: %v", writeErr)
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite() //nolint:errcheck // half-close signals end of request; a failure surfaces as a decode error
	}
	var resp daemon.SocketResponse
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		t.Fatalf("decode response: %v", decErr)
	}
	return resp
}

// TestSocketRouting_QueueRecover_IsRegistered pins the registration itself.
//
// With no handler injected the op must answer its "not registered" envelope. If
// the Register call is dropped the router returns the neutral unknown-op
// envelope instead, which is what this test catches. A CLI verb that reaches
// the unknown-op path is a dead end.
func TestSocketRouting_QueueRecover_IsRegistered(t *testing.T) {
	t.Parallel()
	sockPath := recoverFixtureServe(t, daemon.SocketHandlers{})

	resp := recoverFixtureSend(t, sockPath, map[string]string{"op": "queue-recover", "queue": "main"})
	if resp.Ok {
		t.Fatal("queue-recover with no handler must not report success")
	}
	if resp.Error == `daemon: unknown op "queue-recover"` {
		t.Fatal("queue-recover is NOT registered on the socket router — the CLI verb dead-ends")
	}
	if resp.Error != "daemon: QueueRecoveryHandler not registered" {
		t.Fatalf("error = %q, want the QueueRecoveryHandler-not-registered envelope", resp.Error)
	}
}

func TestSocketRouting_QueueRecover_ReturnsFailureParkedQueueToActive(t *testing.T) {
	t.Parallel()
	store, projectDir := recoverFixtureStore(t)
	sockPath := recoverFixtureServe(t, daemon.SocketHandlers{
		Recovery: daemon.NewQueueRecoveryController(store, projectDir, nil),
	})

	resp := recoverFixtureSend(t, sockPath, map[string]string{"op": "queue-recover", "queue": "canary"})
	if !resp.Ok {
		t.Fatalf("queue-recover: %q (code %d)", resp.Error, resp.ErrorCode)
	}

	var result daemon.QueueRecoverResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.RearmedCount != 1 || len(result.Rearmed) != 1 || result.Rearmed[0] != "hk-canary" {
		t.Fatalf("rearmed = %v (count %d), want [hk-canary]", result.Rearmed, result.RearmedCount)
	}

	got := store.QueueByName("canary")
	if got.Status != queue.QueueStatusActive {
		t.Fatalf("queue status = %q, want %q", got.Status, queue.QueueStatusActive)
	}
	if got.Groups[0].Items[0].Status != queue.ItemStatusPending {
		t.Fatalf("item status = %q, want %q", got.Groups[0].Items[0].Status, queue.ItemStatusPending)
	}
}

func TestSocketRouting_QueueRecover_CarriesTypedCodeOnRefusal(t *testing.T) {
	t.Parallel()
	store, projectDir := recoverFixtureStore(t)
	sockPath := recoverFixtureServe(t, daemon.SocketHandlers{
		Recovery: daemon.NewQueueRecoveryController(store, projectDir, nil),
	})

	cases := []struct {
		name     string
		queue    string
		prepare  func()
		wantCode int
	}{
		{
			name:     "absent queue is -32030",
			queue:    "no-such-queue",
			wantCode: -32030,
		},
		{
			name:  "drain-paused queue is -32031",
			queue: "canary",
			prepare: func() {
				q := store.QueueByName("canary")
				q.Status = queue.QueueStatusPausedByDrain
				store.SetQueueByName("canary", q)
			},
			wantCode: -32031,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.prepare != nil {
				tc.prepare()
			}
			resp := recoverFixtureSend(t, sockPath, map[string]string{"op": "queue-recover", "queue": tc.queue})
			if resp.Ok {
				t.Fatal("refusal reported as success")
			}
			if resp.ErrorCode != tc.wantCode {
				t.Fatalf("error_code = %d, want %d (error %q)", resp.ErrorCode, tc.wantCode, resp.Error)
			}
		})
	}
}

// TestSocketRouting_OperatorResume_RefusesFailureParkedQueue defends the fix for
// the silent wrong answer: before it, this request answered ok=true and nothing
// dispatched.
func TestSocketRouting_OperatorResume_RefusesFailureParkedQueue(t *testing.T) {
	t.Parallel()
	store, _ := recoverFixtureStore(t)

	col := &stubEventCollector{}
	ctrl := daemon.ExportedNewOperatorPauseController(col)
	ctrl.SetQueueStates(store)

	sockPath := recoverFixtureServe(t, daemon.SocketHandlers{Operator: ctrl})

	resp := recoverFixtureSend(t, sockPath, map[string]string{"op": "operator-resume", "queue": "canary"})
	if resp.Ok {
		t.Fatal("operator-resume against a paused-by-failure queue reported success; it releases a DRAIN pause only")
	}
	if len(collectEventsByType(col, "operator_resuming")) != 0 {
		t.Fatal("a refused resume must not emit operator_resuming")
	}
	if got := store.QueueByName("canary"); got.Status != queue.QueueStatusPausedByFailure {
		t.Fatalf("queue status = %q, want it left at %q", got.Status, queue.QueueStatusPausedByFailure)
	}
}

// TestSocketRouting_OperatorResume_StillReleasesADrainPause pins that the fix
// did not retire the drain-release meaning of the verb.
func TestSocketRouting_OperatorResume_StillReleasesADrainPause(t *testing.T) {
	t.Parallel()
	store, _ := recoverFixtureStore(t)
	drained := store.QueueByName("canary")
	drained.Status = queue.QueueStatusPausedByDrain
	store.SetQueueByName("canary", drained)

	col := &stubEventCollector{}
	ctrl := daemon.ExportedNewOperatorPauseController(col)
	ctrl.SetQueueStates(store)

	sockPath := recoverFixtureServe(t, daemon.SocketHandlers{Operator: ctrl})

	resp := recoverFixtureSend(t, sockPath, map[string]string{"op": "operator-resume", "queue": "canary"})
	if !resp.Ok {
		t.Fatalf("operator-resume against a paused-by-drain queue: %q", resp.Error)
	}
	if len(collectEventsByType(col, "operator_resuming")) != 1 {
		t.Fatal("a drain release must emit exactly one operator_resuming")
	}
}
