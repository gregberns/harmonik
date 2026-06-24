package daemon

// pasteinject_hk92ih3_test.go — red→green test for the remote reviewer
// verdict-detection fix (hk-92ih3).
//
// Scenario: remote reviewer writes APPROVE to <remote-worktree>/.harmonik/review.json
// on the worker, but box A has no local copy.  Before the fix, os.Stat(verdictPath)
// on box A always fails → watcher never sees the verdict → reviewer never gets /quit
// → ~30 min hang.  After the fix, statTaskFileVia routes the check through the run's
// CommandRunner; the SSHRunner's exit 0 is treated as "verdict present" and /quit is sent.
//
// This file is in package daemon (not daemon_test) so it can satisfy the unexported
// commandRunnerProvider interface via a local stub — identical to the hkhh5e template.

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// Stubs
// ─────────────────────────────────────────────────────────────────────────────

// hk92ih3Runner records Command() calls.  By default every command exits 0
// (simulating a remote worker where the verdict file IS present).
type hk92ih3Runner struct {
	mu    sync.Mutex
	calls []tmux.RecordingCall
}

func (r *hk92ih3Runner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cp := make([]string, len(args))
	copy(cp, args)
	r.mu.Lock()
	r.calls = append(r.calls, tmux.RecordingCall{Name: name, Args: cp})
	r.mu.Unlock()
	return exec.CommandContext(ctx, "true")
}

func (r *hk92ih3Runner) recorded() []tmux.RecordingCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]tmux.RecordingCall, len(r.calls))
	copy(out, r.calls)
	return out
}

// hk92ih3QuitSender records SendQuitToLastPane calls AND implements
// commandRunnerProvider so that pasteInjectQuitOnReviewFile sees the non-local
// runner and routes verdict-stat through it.
type hk92ih3QuitSender struct {
	runner tmux.CommandRunner
	quits  atomic.Int64
}

func (q *hk92ih3QuitSender) SendQuitToLastPane(_ context.Context) error {
	q.quits.Add(1)
	return nil
}

// commandRunner implements commandRunnerProvider.
func (q *hk92ih3QuitSender) commandRunner() tmux.CommandRunner { return q.runner }

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestPasteInjectQuitOnReviewFile_RemoteRunner_DetectsVerdictViaRunner verifies
// that when the quitSender carries a non-local CommandRunner, the verdict-
// detection stat is routed through the runner rather than os.Stat.
//
// Setup:
//   - box-A wtPath has NO review.json (so pre-fix os.Stat would fail forever).
//   - hk92ih3Runner returns exit 0 for every command, simulating a remote worker
//     whose review.json IS present.
//
// Assertions:
//  1. The runner receives at least one "stat …/.harmonik/review.json" call.
//  2. SendQuitToLastPane is called (verdict was detected via the runner).
//
// FAILS before the fix (os.Stat on box A never finds the file → spins to budget kill
// without calling SendQuitToLastPane promptly); PASSES after (statTaskFileVia routes
// through the runner → exit 0 → verdict detected → /quit sent).
func TestPasteInjectQuitOnReviewFile_RemoteRunner_DetectsVerdictViaRunner(t *testing.T) {
	// Shrink poll interval so the test resolves in milliseconds.
	origPoll := reviewFilePollInterval
	reviewFilePollInterval = 5 * time.Millisecond
	t.Cleanup(func() { reviewFilePollInterval = origPoll })

	// Shrink postQuitKillGrace so the function returns promptly after detecting
	// the verdict and calling SendQuitToLastPane.
	origGrace := postQuitKillGrace
	postQuitKillGrace = 1 * time.Millisecond
	t.Cleanup(func() { postQuitKillGrace = origGrace })

	// Shrink the review budget to avoid the test hanging if the fix is absent —
	// reviewFileTimeout gates how long we wait before the hard-kill path.
	origTimeout := reviewFileTimeout
	reviewFileTimeout = 200 * time.Millisecond
	t.Cleanup(func() { reviewFileTimeout = origTimeout })

	// wtPath has NO review.json — box-A os.Stat will always fail.
	wtPath := t.TempDir()

	runner := &hk92ih3Runner{} // exits 0 for every command = remote verdict present
	qs := &hk92ih3QuitSender{runner: runner}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		pasteInjectQuitOnReviewFile(
			ctx,
			qs,
			nil, // killer — nil is safe; function handles nil
			nil, // inj — nil disables re-seed (irrelevant here)
			"",  // claudeSessID
			wtPath,
			nil, // briefDelivered
			nil, // eventCh
			0,   // overrideCeiling
		)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pasteInjectQuitOnReviewFile did not return after remote verdict detected")
	}

	// Assert runner received a stat call for review.json.
	var foundStat bool
	for _, c := range runner.recorded() {
		if c.Name == "stat" && len(c.Args) > 0 && strings.HasSuffix(c.Args[0], "review.json") {
			foundStat = true
			break
		}
	}
	if !foundStat {
		t.Errorf("runner calls = %v; expected a 'stat …/review.json' call routed through the runner", runner.recorded())
	}

	// Assert /quit was sent — verdict was detected via the runner.
	if got := qs.quits.Load(); got < 1 {
		t.Errorf("SendQuitToLastPane calls: want ≥1 (verdict detected via runner), got %d", got)
	}
}
