package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queue/cli"
)

func cancelFixtureWriteQueue(t *testing.T, projectDir, name string) string {
	t.Helper()

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.MkdirAll(queuesDir, 0o700); err != nil {
		t.Fatalf("cancelFixtureWriteQueue: MkdirAll %q: %v", queuesDir, err)
	}

	normName := queue.NormaliseQueueName(name)
	queueFile := filepath.Join(queuesDir, normName+".json")

	content := `{
  "schema_version": 1,
  "queue_id": "aaaaaaaa-0000-7000-8000-` + normName + `000000",
  "name": "` + normName + `",
  "status": "active",
  "groups": []
}`
	if err := os.WriteFile(queueFile, []byte(content), 0o644); err != nil { //nolint:gosec // G306: test-only
		t.Fatalf("cancelFixtureWriteQueue: WriteFile %q: %v", queueFile, err)
	}
	return queueFile
}

// TestRunQueueCancel_NoArg_ArchivesMain verifies that running cancel without a
// queue-name argument archives the default "main" queue.
func TestRunQueueCancel_NoArg_ArchivesMain(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	mainPath := cancelFixtureWriteQueue(t, projectDir, "main")

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir}, &out, &errOut)

	if got != 0 {
		t.Fatalf("RunQueueCancel no-arg: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if _, err := os.Stat(mainPath); !os.IsNotExist(err) {
		t.Errorf("RunQueueCancel no-arg: main queue file still exists at %q; expected it to be archived", mainPath)
	}
	if !strings.Contains(out.String(), "archived") {
		t.Errorf("RunQueueCancel no-arg: stdout %q does not mention 'archived'", out.String())
	}

	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath) //nolint:gosec // G304: eventsPath is rooted in t.TempDir
	if err != nil {
		t.Fatalf("RunQueueCancel no-arg: read event journal: %v", err)
	}
	var event core.Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(eventsData))), &event); err != nil {
		t.Fatalf("RunQueueCancel no-arg: decode event envelope: %v", err)
	}
	if !event.Valid() {
		t.Errorf("RunQueueCancel no-arg: event envelope is invalid: %+v", event)
	}
	if event.Type != "queue_cancelled_operator" {
		t.Errorf("RunQueueCancel no-arg: event type = %q, want queue_cancelled_operator", event.Type)
	}
	if event.SourceSubsystem != "github.com/gregberns/harmonik/internal/queue" {
		t.Errorf("RunQueueCancel no-arg: event source_subsystem = %q, want queue subsystem", event.SourceSubsystem)
	}
}

// TestRunQueueCancel_NameArg_ArchivesNamedQueue verifies that supplying a
// queue name as a positional argument archives THAT queue and leaves the
// "main" queue untouched. This is the regression test for hk-4kuvj: the old
// code always archived "main" regardless of the name argument.
func TestRunQueueCancel_NameArg_ArchivesNamedQueue(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	mainPath := cancelFixtureWriteQueue(t, projectDir, "main")
	investigatePath := cancelFixtureWriteQueue(t, projectDir, "investigate")

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir, "investigate"}, &out, &errOut)

	if got != 0 {
		t.Fatalf("RunQueueCancel named: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if _, err := os.Stat(investigatePath); !os.IsNotExist(err) {
		t.Errorf("RunQueueCancel named: investigate queue file still exists at %q", investigatePath)
	}
	if _, err := os.Stat(mainPath); err != nil {
		t.Errorf("RunQueueCancel named: main queue file unexpectedly gone: %v", err)
	}
	if !strings.Contains(out.String(), "archived") {
		t.Errorf("RunQueueCancel named: stdout %q does not mention 'archived'", out.String())
	}
}

// TestRunQueueCancel_AbsentQueue_ExitsZero verifies that a cancel that names
// NO queue and finds no default queue exits 0 (nothing to cancel). The caller
// named nothing, so there is no name to be wrong about. A cancel that DOES name
// a queue that does not exist is refused instead — see
// TestRunQueueCancel_UnknownNamedQueue_IsRefused.
func TestRunQueueCancel_AbsentQueue_ExitsZero(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueCancel absent: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if !strings.Contains(out.String(), "no active queue") {
		t.Errorf("RunQueueCancel absent: stdout %q does not mention 'no active queue'", out.String())
	}
}

// TestRunQueueCancel_CompletedQueue_RefusesWithoutForce verifies that
// cancelling an already-completed queue exits non-zero without --force.
func TestRunQueueCancel_CompletedQueue_RefusesWithoutForce(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.MkdirAll(queuesDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	completedContent := `{
  "schema_version": 1,
  "queue_id": "bbbbbbbb-0000-7000-8000-000000000000",
  "name": "main",
  "status": "completed",
  "groups": []
}`
	queueFile := filepath.Join(queuesDir, "main.json")
	if err := os.WriteFile(queueFile, []byte(completedContent), 0o644); err != nil { //nolint:gosec // G306: test-only
		t.Fatalf("WriteFile: %v", err)
	}

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir}, &out, &errOut)

	if got == 0 {
		t.Errorf("RunQueueCancel completed (no --force): exit = 0, want non-zero")
	}
}

// TestRunQueueCancel_CompletedQueue_ForceArchives verifies that --force
// archives a completed queue.
func TestRunQueueCancel_CompletedQueue_ForceArchives(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.MkdirAll(queuesDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	completedContent := `{
  "schema_version": 1,
  "queue_id": "cccccccc-0000-7000-8000-000000000000",
  "name": "main",
  "status": "completed",
  "groups": []
}`
	queueFile := filepath.Join(queuesDir, "main.json")
	if err := os.WriteFile(queueFile, []byte(completedContent), 0o644); err != nil { //nolint:gosec // G306: test-only
		t.Fatalf("WriteFile: %v", err)
	}

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir, "--force"}, &out, &errOut)

	if got != 0 {
		t.Fatalf("RunQueueCancel completed --force: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if _, err := os.Stat(queueFile); !os.IsNotExist(err) {
		t.Errorf("RunQueueCancel completed --force: queue file still present at %q", queueFile)
	}
}

// TestRunQueueCancel_QueueFlag_ArchivesNamedQueue verifies that --queue <name>
// archives the named queue, freeing the name for a fresh submit (hk-fkpb7
// problem (3): cancel must accept the flag form used by the other queue verbs).
func TestRunQueueCancel_QueueFlag_ArchivesNamedQueue(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	mainPath := cancelFixtureWriteQueue(t, projectDir, "main")
	fwkPath := cancelFixtureWriteQueue(t, projectDir, "fwkeeper")

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir, "--queue", "fwkeeper"}, &out, &errOut)

	if got != 0 {
		t.Fatalf("RunQueueCancel --queue fwkeeper: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if _, err := os.Stat(fwkPath); !os.IsNotExist(err) {
		t.Errorf("RunQueueCancel --queue fwkeeper: fwkeeper queue file still exists at %q", fwkPath)
	}
	if _, err := os.Stat(mainPath); err != nil {
		t.Errorf("RunQueueCancel --queue fwkeeper: main queue file unexpectedly gone: %v", err)
	}
	if !strings.Contains(out.String(), "archived") {
		t.Errorf("RunQueueCancel --queue fwkeeper: stdout %q does not mention 'archived'", out.String())
	}
}

// TestRunQueueCancel_QueueFlagEquals_ArchivesNamedQueue verifies the --queue=<name>
// equals form.
func TestRunQueueCancel_QueueFlagEquals_ArchivesNamedQueue(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	fwkPath := cancelFixtureWriteQueue(t, projectDir, "fwkeeper")

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir, "--queue=fwkeeper"}, &out, &errOut)

	if got != 0 {
		t.Fatalf("RunQueueCancel --queue=fwkeeper: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if _, err := os.Stat(fwkPath); !os.IsNotExist(err) {
		t.Errorf("RunQueueCancel --queue=fwkeeper: fwkeeper queue file still exists at %q", fwkPath)
	}
}

// TestRunQueueCancel_QueueIDFlag_ArchivesByUUID verifies that --queue-id <uuid>
// locates the queue across all per-name files and archives the matching one,
// leaving other queues untouched (hk-fkpb7).
func TestRunQueueCancel_QueueIDFlag_ArchivesByUUID(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	mainPath := cancelFixtureWriteQueue(t, projectDir, "main")

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.MkdirAll(queuesDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	fwkQueueID := "aaaabbbb-0000-7000-8000-fwkeeper00001"
	fwkContent := `{
  "schema_version": 1,
  "queue_id": "` + fwkQueueID + `",
  "name": "fwkeeper",
  "status": "paused-by-failure",
  "groups": []
}`
	fwkPath := filepath.Join(queuesDir, "fwkeeper.json")
	if err := os.WriteFile(fwkPath, []byte(fwkContent), 0o644); err != nil { //nolint:gosec // G306: test-only
		t.Fatalf("WriteFile fwkeeper: %v", err)
	}

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir, "--queue-id", fwkQueueID}, &out, &errOut)

	if got != 0 {
		t.Fatalf("RunQueueCancel --queue-id: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if _, err := os.Stat(fwkPath); !os.IsNotExist(err) {
		t.Errorf("RunQueueCancel --queue-id: fwkeeper queue file still exists at %q", fwkPath)
	}
	if _, err := os.Stat(mainPath); err != nil {
		t.Errorf("RunQueueCancel --queue-id: main queue file unexpectedly gone: %v", err)
	}
	if !strings.Contains(out.String(), "archived") {
		t.Errorf("RunQueueCancel --queue-id: stdout %q does not mention 'archived'", out.String())
	}
	if !strings.Contains(out.String(), fwkQueueID) {
		t.Errorf("RunQueueCancel --queue-id: stdout %q does not mention queue_id %s", out.String(), fwkQueueID)
	}
}

// TestRunQueueCancel_CorruptStub_ArchivesByName verifies that a corrupt/zero-value
// stub file (e.g. schema_version:0 left by a half-completed prior session) is
// archived by queue name even when the file cannot be parsed (hk-9ztth).
func TestRunQueueCancel_CorruptStub_ArchivesByName(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.MkdirAll(queuesDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	stubContent := `{"queue_id":"","status":"","groups":null,"workers":1}`
	stubPath := filepath.Join(queuesDir, "chani-q.json")
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o644); err != nil { //nolint:gosec // G306: test-only
		t.Fatalf("WriteFile stub: %v", err)
	}

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir, "--queue", "chani-q"}, &out, &errOut)

	if got != 0 {
		t.Fatalf("RunQueueCancel corrupt stub: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if _, err := os.Stat(stubPath); !os.IsNotExist(err) {
		t.Errorf("RunQueueCancel corrupt stub: stub file still exists at %q", stubPath)
	}
	if !strings.Contains(out.String(), "archived") {
		t.Errorf("RunQueueCancel corrupt stub: stdout %q does not mention 'archived'", out.String())
	}
}

// TestRunQueueCancel_QueueIDFlag_NotFound_IsRefused verifies that --queue-id
// with a UUID that matches no file is REFUSED.
//
// This test asserted exit 0 and "no active queue found" until hk-wka5o. That
// was the defect written down: the caller named one specific queue by its id,
// was told nothing was wrong, and no queue was cancelled. Reversed deliberately
// — see TestRunQueueCancel_UnknownQueueID_IsRefused for the wording.
func TestRunQueueCancel_QueueIDFlag_NotFound_IsRefused(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	cancelFixtureWriteQueue(t, projectDir, "main")

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir, "--queue-id", "00000000-dead-7000-beef-000000000000"}, &out, &errOut)

	if got != 2 {
		t.Errorf("RunQueueCancel --queue-id not-found: exit = %d, want 2; stdout=%q stderr=%q", got, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "no queue with id") {
		t.Errorf("RunQueueCancel --queue-id not-found: stderr %q does not refuse the id", errOut.String())
	}
}

// TestRunQueueCancel_LiveDaemon_RoutesThroughSocket verifies that when a
// daemon is reachable, RunQueueCancel sends a "queue-cancel" op (not a
// disk-only archive) and reports the daemon's response — this is the path
// that lets a live daemon reap its in-memory QueueStore slot (hk-0mmy4).
func TestRunQueueCancel_LiveDaemon_RoutesThroughSocket(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	cancelFixtureWriteQueue(t, projectDir, "alpha")

	var capturedOp string
	var capturedQueue string
	var capturedForce bool
	queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
		msg := queueCliFixtureDecodeRequest(t, raw)
		queueCliFixtureCapture(t, msg, "op", &capturedOp)
		queueCliFixtureCapture(t, msg, "queue", &capturedQueue)
		queueCliFixtureCapture(t, msg, "force", &capturedForce)
		return queueCliFixtureSuccessResponse(t, map[string]any{
			"queue_id":     "qid-alpha-daemon-routed",
			"prior_status": "active",
		})
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir, "--queue", "alpha"}, &out, &errOut)

	if got != 0 {
		t.Fatalf("RunQueueCancel live-daemon: exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if capturedOp != "queue-cancel" {
		t.Errorf("RunQueueCancel live-daemon: op = %q, want %q", capturedOp, "queue-cancel")
	}
	if capturedQueue != "alpha" {
		t.Errorf("RunQueueCancel live-daemon: queue = %q, want %q", capturedQueue, "alpha")
	}
	if capturedForce {
		t.Errorf("RunQueueCancel live-daemon: force = true, want false (no --force passed)")
	}
	if !strings.Contains(out.String(), "qid-alpha-daemon-routed") {
		t.Errorf("RunQueueCancel live-daemon: stdout %q does not echo the daemon-reported queue_id", out.String())
	}
	if !strings.Contains(out.String(), "daemon-reaped") {
		t.Errorf("RunQueueCancel live-daemon: stdout %q does not indicate the daemon-routed path was used", out.String())
	}
}

// TestRunQueueCancel_LiveDaemon_SurfacesDaemonError verifies that a daemon
// rejection (e.g. queue_already_completed without --force) is surfaced to
// the operator with a non-zero exit rather than silently falling back to the
// disk-only archive (which would bypass the daemon's authoritative refusal).
func TestRunQueueCancel_LiveDaemon_SurfacesDaemonError(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	cancelFixtureWriteQueue(t, projectDir, "main")

	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		resp := map[string]json.RawMessage{
			"ok":         json.RawMessage(`false`),
			"error":      mustJSONMarshal(t, "queue qid-x is already completed; use --force to archive anyway"),
			"error_code": json.RawMessage(`-32099`),
		}
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("marshal error response: %v", err)
		}
		return data
	})

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir}, &out, &errOut)

	if got == 0 {
		t.Fatalf("RunQueueCancel live-daemon error: exit = 0, want non-zero; stdout=%q", out.String())
	}
	if !strings.Contains(errOut.String(), "already completed") {
		t.Errorf("RunQueueCancel live-daemon error: stderr %q does not mention the daemon's rejection reason", errOut.String())
	}
}

func mustJSONMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustJSONMarshal: %v", err)
	}
	return data
}

// TestRunQueueCancel_UnknownNamedQueue_IsRefused covers both spellings of a
// caller-supplied name: the --queue flag and the backward-compatible
// positional. Neither may report success.
func TestRunQueueCancel_UnknownNamedQueue_IsRefused(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"queue-flag", []string{"--queue", "mian"}},
		{"positional", []string{"mian"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			projectDir := queueCliFixtureTempDir(t)
			cancelFixtureWriteQueue(t, projectDir, "main")

			var out strings.Builder
			var errOut strings.Builder

			args := append([]string{"--project", projectDir}, tc.args...)
			got := cli.RunQueueCancel(context.Background(), args, &out, &errOut)

			if got != 2 {
				t.Errorf("RunQueueCancel %v: exit = %d, want 2 — a queue name that matches nothing is refused, the same as `queue pause`; stdout=%q stderr=%q",
					tc.args, got, out.String(), errOut.String())
			}
			if strings.Contains(out.String(), "archived") {
				t.Errorf("RunQueueCancel %v: stdout claims an archive happened: %q", tc.args, out.String())
			}
			for _, want := range []string{
				`no queue named "mian"`,
				"changed nothing and no queue was cancelled",
				"queues that exist: main",
			} {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("RunQueueCancel %v: stderr %q does not contain %q", tc.args, errOut.String(), want)
				}
			}
		})
	}
}

// TestRunQueueCancel_UnknownNamedQueue_NoQueuesAtAll pins the empty-project
// wording. "none are loaded" is the sibling's phrasing for a project that holds
// no queues, and it must not read as an empty list.
func TestRunQueueCancel_UnknownNamedQueue_NoQueuesAtAll(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir, "--queue", "mian"}, &out, &errOut)

	if got != 2 {
		t.Errorf("RunQueueCancel unknown queue in empty project: exit = %d, want 2; stderr=%q", got, errOut.String())
	}
	if !strings.Contains(errOut.String(), "queues that exist: none are loaded") {
		t.Errorf("RunQueueCancel unknown queue in empty project: stderr %q does not say no queues are loaded", errOut.String())
	}
}

// TestRunQueueCancel_RefusalComesFromTheSharedError checks that cancel's
// refusal text keeps TRACKING queue.UnknownQueueError's current output. It
// builds the expected text by calling the renderer, so it cannot tell a
// rendered string from a byte-identical hand-rolled copy — today they pass it
// alike. What it catches is the divergence: reword the renderer and this test
// rewords with it, so any copy left behind in cancel.go fails here.
//
// That is the half the literal-text assertions in
// TestRunQueueCancel_UnknownNamedQueue_IsRefused cannot cover. Those pin the
// wording as it stands now, which means a reword makes them fail and someone
// edits them — against whatever cancel prints, copy or not.
//
// It compares cancel to cancel on purpose: it is a coupling check, not a
// cross-verb one. Whether pause and cancel agree as SENTENCES is a property of
// the renderer, and it is held there —
// internal/queue TestUnknownQueueErrorVerbsDifferOnlyWhereIntended.
func TestRunQueueCancel_RefusalComesFromTheSharedError(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	cancelFixtureWriteQueue(t, projectDir, "canary")

	var out strings.Builder
	var errOut strings.Builder
	cli.RunQueueCancel(context.Background(), []string{"--project", projectDir, "--queue", "canry"}, &out, &errOut)

	rendered := (&queue.UnknownQueueError{
		Verb:           "cancel",
		PastTense:      "cancelled",
		NormalizedName: "canry",
		KnownNames:     []string{"canary"},
	}).Error()
	if !strings.Contains(errOut.String(), rendered) {
		t.Errorf("RunQueueCancel refusal text drifted from queue.UnknownQueueError:\n got: %q\nwant substring: %q", errOut.String(), rendered)
	}
}

// TestRunQueueCancel_UnknownQueueID_IsRefused pins the refusal and its wording:
// a uuid is reported as an ID, not as a name.
func TestRunQueueCancel_UnknownQueueID_IsRefused(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	cancelFixtureWriteQueue(t, projectDir, "canary")

	const missing = "00000000-0000-7000-8000-000000000000"

	var out strings.Builder
	var errOut strings.Builder
	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir, "--queue-id", missing}, &out, &errOut)

	if got != 2 {
		t.Errorf("RunQueueCancel --queue-id <no match>: exit = %d, want 2 — a uuid that matches nothing is the most explicit miss there is; stdout=%q stderr=%q",
			got, out.String(), errOut.String())
	}
	if strings.Contains(out.String(), "archived") {
		t.Errorf("RunQueueCancel --queue-id <no match>: stdout claims an archive happened: %q", out.String())
	}
	for _, want := range []string{
		`no queue with id "` + missing + `"`,
		"changed nothing and no queue was cancelled",
		"queues that exist: canary",
	} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("RunQueueCancel --queue-id <no match>: stderr %q does not contain %q", errOut.String(), want)
		}
	}
	if strings.Contains(errOut.String(), "no queue named") {
		t.Errorf("RunQueueCancel --queue-id <no match>: a uuid was reported as a queue NAME:\n%s", errOut.String())
	}
}

// TestRunQueueCancel_QueueIDThatMatches_StillCancels is the control. The
// refusal must fire only on a miss.
func TestRunQueueCancel_QueueIDThatMatches_StillCancels(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	queueFile := cancelFixtureWriteQueue(t, projectDir, "canary")

	const present = "aaaaaaaa-0000-7000-8000-canary000000"

	var out strings.Builder
	var errOut strings.Builder
	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir, "--queue-id", present}, &out, &errOut)

	if got != 0 {
		t.Fatalf("RunQueueCancel --queue-id <match>: exit = %d, want 0; stdout=%q stderr=%q", got, out.String(), errOut.String())
	}
	if _, err := os.Stat(queueFile); !os.IsNotExist(err) {
		t.Errorf("RunQueueCancel --queue-id <match>: queue file still present at %q", queueFile)
	}
	if !strings.Contains(out.String(), "archived") {
		t.Errorf("RunQueueCancel --queue-id <match>: stdout %q does not mention 'archived'", out.String())
	}
}

// TestRunQueueCancel_LiveDaemon_EmptyQueueIDWithPriorStatus covers the archive
// the CLI used to deny. The queue file carried no queue_id; the daemon archived
// it anyway and said so with prior_status.
func TestRunQueueCancel_LiveDaemon_EmptyQueueIDWithPriorStatus(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	cancelFixtureWriteQueue(t, projectDir, "alpha")

	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureSuccessResponse(t, map[string]any{
			"queue_id":     "",
			"prior_status": "active",
		})
	})

	var out strings.Builder
	var errOut strings.Builder
	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir, "--queue", "alpha"}, &out, &errOut)

	if got != 0 {
		t.Fatalf("RunQueueCancel daemon-archived-without-id: exit = %d, want 0; stdout=%q stderr=%q", got, out.String(), errOut.String())
	}
	if strings.Contains(out.String(), "no active queue found") {
		t.Errorf("RunQueueCancel daemon-archived-without-id: the daemon archived the queue and the CLI reported the opposite:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "archived") {
		t.Errorf("RunQueueCancel daemon-archived-without-id: stdout %q does not report the archive that happened", out.String())
	}
	if !strings.Contains(out.String(), "status=active") {
		t.Errorf("RunQueueCancel daemon-archived-without-id: stdout %q drops the prior status the daemon reported", out.String())
	}
}

// TestRunQueueCancel_LiveDaemon_WhollyEmptyResponse covers the other cause: the
// daemon's own load found nothing, so the queue this command had already loaded
// was gone by the time the daemon looked. Nothing was archived. Exit 0 is right
// — the caller asked for the queue not to be running and it is not — but the
// message has to name the queue and say nothing was archived, because "queue
// file absent" describes the wrong moment in time.
func TestRunQueueCancel_LiveDaemon_WhollyEmptyResponse(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	cancelFixtureWriteQueue(t, projectDir, "alpha")

	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureSuccessResponse(t, map[string]any{})
	})

	var out strings.Builder
	var errOut strings.Builder
	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir, "--queue", "alpha"}, &out, &errOut)

	if got != 0 {
		t.Fatalf("RunQueueCancel daemon-found-nothing: exit = %d, want 0; stdout=%q stderr=%q", got, out.String(), errOut.String())
	}
	if strings.Contains(out.String(), "daemon-reaped") {
		t.Errorf("RunQueueCancel daemon-found-nothing: stdout claims an archive that did not happen: %q", out.String())
	}
	if !strings.Contains(out.String(), "alpha") {
		t.Errorf("RunQueueCancel daemon-found-nothing: stdout %q never names the queue the caller asked about", out.String())
	}
}

// TestRunQueueCancel_EmptySelectorValue_IsRefused covers all five spellings of
// an empty selector value. Each must leave every queue file on disk, refuse
// with exit 2, print nothing that reads as an archive, and name on stderr WHICH
// selector arrived empty.
func TestRunQueueCancel_EmptySelectorValue_IsRefused(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		// wantStderr is the whole phrase, not just the flag spelling:
		// "--queue-id was given an empty value" contains "--queue", so a
		// bare-flag match would let the two selectors' messages pass for
		// each other.
		wantStderr string
	}{
		{"queue-id-equals", []string{"--queue-id="}, `--queue-id was given an empty value`},
		{"queue-id-separate", []string{"--queue-id", ""}, `--queue-id was given an empty value`},
		{"queue-equals", []string{"--queue="}, `--queue was given an empty value`},
		{"queue-separate", []string{"--queue", ""}, `--queue was given an empty value`},
		{"positional-empty", []string{""}, `the queue name argument was empty`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			projectDir := queueCliFixtureTempDir(t)
			mainPath := cancelFixtureWriteQueue(t, projectDir, "main")
			betaPath := cancelFixtureWriteQueue(t, projectDir, "beta")

			var out strings.Builder
			var errOut strings.Builder

			args := append([]string{"--project", projectDir}, tc.args...)
			got := cli.RunQueueCancel(context.Background(), args, &out, &errOut)

			if _, err := os.Stat(mainPath); err != nil {
				t.Errorf("RunQueueCancel %v: the default queue was archived by a selector the caller never filled in: %v", tc.args, err)
			}
			if _, err := os.Stat(betaPath); err != nil {
				t.Errorf("RunQueueCancel %v: queue %q is gone: %v", tc.args, betaPath, err)
			}
			if got != 2 {
				t.Errorf("RunQueueCancel %v: exit = %d, want 2 — an empty selector value is an argument error, not a request for the default queue; stdout=%q stderr=%q",
					tc.args, got, out.String(), errOut.String())
			}
			if strings.Contains(out.String(), "archived") {
				t.Errorf("RunQueueCancel %v: stdout claims an archive happened: %q", tc.args, out.String())
			}
			if !strings.Contains(errOut.String(), tc.wantStderr) {
				t.Errorf("RunQueueCancel %v: stderr %q does not name which selector was empty (want %q)", tc.args, errOut.String(), tc.wantStderr)
			}
		})
	}
}

// TestRunQueueCancel_NoSelectorAtAll_StillExitsZero is the control for the test
// above, and it pins the boundary the fix must not cross. Giving NO selector is
// a different act from giving an empty one: a bare `queue cancel` asserts an end
// state — "main is not running" — and an absent main satisfies it, the way
// `rm -f` is satisfied by an absent file. Scripts end with a best-effort cancel,
// so refusing this would break them.
//
// TestRunQueueCancel_NoArg_ArchivesMain and TestRunQueueCancel_AbsentQueue_ExitsZero
// hold the same line from the archive side. This one holds it from the
// empty-selector side: it also asserts that the refusal wording above never
// reaches a caller who typed no selector.
func TestRunQueueCancel_NoSelectorAtAll_StillExitsZero(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir}, &out, &errOut)

	if got != 0 {
		t.Errorf("RunQueueCancel bare: exit = %d, want 0 — a bare cancel asserts an end state and an absent main satisfies it; stdout=%q stderr=%q",
			got, out.String(), errOut.String())
	}
	if strings.Contains(errOut.String(), "empty value") || strings.Contains(errOut.String(), "was empty") {
		t.Errorf("RunQueueCancel bare: stderr %q refuses a caller who gave no selector at all", errOut.String())
	}
}

// TestRunQueueCancel_QueueIDFlag_ArchivesTheFileItFound covers both spellings
// of a `name` field that does not describe the file it sits in: absent (which
// normalises to "main") and present but pointing at another file. In both, the
// caller selected one queue by uuid, so that queue's FILE is the only one
// allowed to move, and main must still be there afterwards.
func TestRunQueueCancel_QueueIDFlag_ArchivesTheFileItFound(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		testName string
		// file is the queue file's basename (without .json) — the name the
		// caller's uuid actually selects, and the only file that may move.
		file string
		// declaredName is the file's own `name` field: the value the resolution
		// used to trust.
		declaredName string
	}{
		{"empty-name-field", "beta", ""},
		{"name-disagrees-with-filename", "gamma", "main"},
	} {
		t.Run(tc.testName, func(t *testing.T) {
			t.Parallel()

			projectDir := queueCliFixtureTempDir(t)
			mainPath := cancelFixtureWriteQueue(t, projectDir, "main")

			queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
			targetID := "bbbbbbbb-0000-7000-8000-" + tc.file + "00000000"
			targetPath := filepath.Join(queuesDir, tc.file+".json")
			content := `{
  "schema_version": 1,
  "queue_id": "` + targetID + `",
  "name": "` + tc.declaredName + `",
  "status": "active",
  "groups": []
}`
			if err := os.WriteFile(targetPath, []byte(content), 0o644); err != nil { //nolint:gosec // G306: test-only
				t.Fatalf("WriteFile %q: %v", targetPath, err)
			}

			var out strings.Builder
			var errOut strings.Builder

			got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir, "--queue-id", targetID}, &out, &errOut)

			if _, err := os.Stat(mainPath); err != nil {
				t.Errorf("RunQueueCancel --queue-id %s: the default queue was archived by a cancel aimed at %q, whose file declares name %q: %v",
					targetID, tc.file, tc.declaredName, err)
			}
			if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
				t.Errorf("RunQueueCancel --queue-id %s: %q was selected but its file is still at %q", targetID, tc.file, targetPath)
			}
			archives, globErr := filepath.Glob(targetPath + queue.FailedArchiveInfix + "*")
			if globErr != nil {
				t.Fatalf("Glob archives: %v", globErr)
			}
			if len(archives) != 1 {
				t.Errorf("RunQueueCancel --queue-id %s: want exactly one archive of %q, got %v", targetID, tc.file, archives)
			}
			if got != 0 {
				t.Errorf("RunQueueCancel --queue-id %s: exit = %d, want 0; stdout=%q stderr=%q", targetID, got, out.String(), errOut.String())
			}
			if !strings.Contains(out.String(), tc.file+".json"+queue.FailedArchiveInfix) {
				t.Errorf("RunQueueCancel --queue-id %s: stdout %q does not report an archive of %q", targetID, out.String(), tc.file)
			}
			if strings.Contains(out.String(), "main.json"+queue.FailedArchiveInfix) {
				t.Errorf("RunQueueCancel --queue-id %s: stdout reports archiving the default queue: %q", targetID, out.String())
			}
		})
	}
}
