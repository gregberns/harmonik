package cli_test

// cancel_test.go — unit tests for RunQueueCancel.
//
// RunQueueCancel works without a live daemon — it manipulates queue files
// directly under .harmonik/queues/. Tests therefore do NOT start an echo
// server; they write queue JSON into the standard per-queue path and verify
// the correct file is archived (or left alone).
//
// Bead ref: hk-4kuvj.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queue/cli"
)

// cancelFixtureWriteQueue writes a minimal active queue JSON to the canonical
// per-queue path (.harmonik/queues/<name>.json) under projectDir and returns
// the path. The queue is given a synthetic queue_id so tests can verify
// archive contents.
func cancelFixtureWriteQueue(t *testing.T, projectDir, name string) string {
	t.Helper()

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.MkdirAll(queuesDir, 0o755); err != nil {
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

// ---------------------------------------------------------------------------
// RunQueueCancel tests
// ---------------------------------------------------------------------------

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
	// Original "main" queue file must be gone (renamed to archive).
	if _, err := os.Stat(mainPath); !os.IsNotExist(err) {
		t.Errorf("RunQueueCancel no-arg: main queue file still exists at %q; expected it to be archived", mainPath)
	}
	if !strings.Contains(out.String(), "archived") {
		t.Errorf("RunQueueCancel no-arg: stdout %q does not mention 'archived'", out.String())
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
	// "investigate" queue file must be gone.
	if _, err := os.Stat(investigatePath); !os.IsNotExist(err) {
		t.Errorf("RunQueueCancel named: investigate queue file still exists at %q", investigatePath)
	}
	// "main" queue file must be untouched.
	if _, err := os.Stat(mainPath); err != nil {
		t.Errorf("RunQueueCancel named: main queue file unexpectedly gone: %v", err)
	}
	if !strings.Contains(out.String(), "archived") {
		t.Errorf("RunQueueCancel named: stdout %q does not mention 'archived'", out.String())
	}
}

// TestRunQueueCancel_AbsentQueue_ExitsZero verifies that cancelling a
// queue that has no on-disk file exits 0 (nothing to cancel).
func TestRunQueueCancel_AbsentQueue_ExitsZero(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	// Do NOT write any queue file.

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

	// Write a completed queue.
	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.MkdirAll(queuesDir, 0o755); err != nil {
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
	if err := os.MkdirAll(queuesDir, 0o755); err != nil {
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
