package cli_test

// cancelexit_hk9z2yl_test.go — coverage for cancelExitOK and journalCancel
// (de9aac48, follow-up bead hk-9z2yl).
//
// de9aac48's commit body singles out one behaviour change: `queue cancel` now
// maps a failed stdout write to exit 1. Every pre-existing cancel test writes
// into a strings.Builder, which never fails, so that path had no coverage at
// all — cancelExitOK could be reduced to `return 0` and the suite would stay
// green.
//
// What these tests pin:
//   - a cancel whose confirmation line never reached stdout exits 1, even
//     though the archive itself succeeded (TestRunQueueCancel_StdoutWriteFails_*);
//   - the "nothing to cancel" replies do the same — they are the reply, so a
//     caller that did not receive them learned nothing (…AbsentQueue…);
//   - a failed cancel-event journal warns on stderr and leaves the exit code
//     alone (…JournalFailure…), which is journalCancel's whole contract now that
//     emitQueueCancelEvent returns its error instead of swallowing it.
//
// Bead ref: hk-9z2yl.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/queue/cli"
)

// cancelFailingWriter is an io.Writer that fails every write — a closed pager,
// a full pipe, or a redirect to a full filesystem.
type cancelFailingWriter struct{ writes int }

func (w *cancelFailingWriter) Write(p []byte) (int, error) {
	w.writes++
	return 0, errors.New("EPIPE")
}

// TestRunQueueCancel_StdoutWriteFails_ExitsOne verifies that a cancel which
// archived the queue but could not deliver its confirmation line exits 1. The
// archive still happens: the exit code reports the truncated report, it does
// not undo the cancel.
func TestRunQueueCancel_StdoutWriteFails_ExitsOne(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	mainPath := cancelFixtureWriteQueue(t, projectDir, "main")

	out := &cancelFailingWriter{}
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir}, out, &errOut)

	if got != 1 {
		t.Fatalf("RunQueueCancel with a failing stdout: exit = %d, want 1; stderr=%q", got, errOut.String())
	}
	if out.writes == 0 {
		t.Fatal("RunQueueCancel never attempted a stdout write; the test is not exercising the reporting path")
	}
	// The cancel itself must still have happened — exit 1 reports the truncated
	// report, not a refused cancel.
	if _, err := os.Stat(mainPath); !os.IsNotExist(err) {
		t.Errorf("RunQueueCancel with a failing stdout: queue file still present at %q; the cancel was skipped", mainPath)
	}
}

// TestRunQueueCancel_AbsentQueue_StdoutWriteFails_ExitsOne verifies the
// nothing-to-cancel reply is held to the same standard. "no active queue found"
// IS the answer, so a caller that never received it must not read exit 0.
func TestRunQueueCancel_AbsentQueue_StdoutWriteFails_ExitsOne(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	// Deliberately no queue file.

	out := &cancelFailingWriter{}
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir}, out, &errOut)

	if got != 1 {
		t.Fatalf("RunQueueCancel absent-queue with a failing stdout: exit = %d, want 1; stderr=%q", got, errOut.String())
	}
	if out.writes == 0 {
		t.Fatal("RunQueueCancel never attempted a stdout write for the absent-queue reply")
	}
}

// TestRunQueueCancel_JournalFailure_WarnsWithoutFailing verifies journalCancel's
// contract: emitQueueCancelEvent now RETURNS its error, and the caller surfaces
// it on stderr but does not let it change the exit code, because the archive it
// records has already landed on disk.
//
// The journal is poisoned by planting a regular file where the events directory
// belongs, so emitQueueCancelEvent's MkdirAll fails.
func TestRunQueueCancel_JournalFailure_WarnsWithoutFailing(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	mainPath := cancelFixtureWriteQueue(t, projectDir, "main")

	eventsPath := filepath.Join(projectDir, ".harmonik", "events")
	if err := os.WriteFile(eventsPath, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatalf("plant a file at the events dir path %q: %v", eventsPath, err)
	}

	var out strings.Builder
	var errOut strings.Builder

	got := cli.RunQueueCancel(context.Background(), []string{"--project", projectDir}, &out, &errOut)

	if got != 0 {
		t.Fatalf("RunQueueCancel with an unwritable journal: exit = %d, want 0 (journalling is best-effort); stderr=%q", got, errOut.String())
	}
	if !strings.Contains(errOut.String(), "could not journal cancel event") {
		t.Errorf("RunQueueCancel with an unwritable journal: stderr %q does not warn about the dropped cancel event", errOut.String())
	}
	if _, err := os.Stat(mainPath); !os.IsNotExist(err) {
		t.Errorf("RunQueueCancel with an unwritable journal: queue file still present at %q", mainPath)
	}
	if !strings.Contains(out.String(), "archived") {
		t.Errorf("RunQueueCancel with an unwritable journal: stdout %q does not confirm the archive", out.String())
	}
}
