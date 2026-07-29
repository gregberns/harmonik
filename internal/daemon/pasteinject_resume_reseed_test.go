package daemon_test

// pasteinject_resume_reseed_test.go — guards for the implementer-RESUME submit
// path and the one-shot reseed-Enter that rescues it.
//
// # Why this file exists
//
// These two functions — sendResumeSubmitEnter and the reseed-Enter branch of
// pasteInjectQuitOnCommit — were covered only by
// reviewloop_resume_reseed_hk8oy_test.go, which was deleted with the review-loop
// driver. That deletion was wrong on this point: both functions are LIVE, they
// are how the DOT cascade drives implementer-resume on a REQUEST_CHANGES
// back-edge, and nothing about the failure mode they defend is review-loop
// specific. This restores the coverage aimed at the code that actually runs.
//
// # The incident being guarded (2026-06-10, hk-8oy / hk-76n5g)
//
// After `claude --resume <id>` the daemon pastes the combined task+feedback
// brief, then sends the submit Enters (hk-ip33d: 1 + resumeSubmitRetries, over
// ~800 ms). In production the TUI was still absorbing the bracketed paste when
// ALL of those Enters arrived, so every one was swallowed. The brief sat
// typed-but-unsubmitted, the resumed implementer stayed idle and committed
// nothing, and the run burned to the 30-minute commitPollTimeout. Worse, the
// failure then MISREPORTED itself: HEAD was unchanged at the next iteration, so
// the run was classified as the implementer refusing to address reviewer
// feedback rather than never having seen it.
//
// The recovery is a one-shot reseed-Enter fired after implementerReseedGrace
// (75 s in production) when no commit has appeared. It submits the pending
// input and restores normal flow.
//
// The original test reproduced this end-to-end through the review-loop driver in
// ~580 lines. These are unit-level guards on the same two behaviours: cheaper,
// and they do not need a driver to exist.
//
// Helper prefix: prr (per implementer-protocol.md §Helper-prefix discipline).
//
// Beads: hk-8oy, hk-76n5g, hk-ip33d.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// prrRecorder is a quitSender + enterSender + sessionKiller stub that counts
// each call. It is the whole substrate these paths touch.
type prrRecorder struct {
	mu      sync.Mutex
	enters  int
	quits   int
	kills   int
	enterAt []time.Time
}

func (r *prrRecorder) SendEnterToLastPane(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enters++
	r.enterAt = append(r.enterAt, time.Now())
	return nil
}

func (r *prrRecorder) SendQuitToLastPane(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.quits++
	return nil
}

func (r *prrRecorder) Kill(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kills++
	return nil
}

func (r *prrRecorder) counts() (enters, quits, kills int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enters, r.quits, r.kills
}

// prrGitRepoWithCommit creates a throwaway git repo with one commit and returns
// its path and HEAD SHA. pasteInjectQuitOnCommit polls HEAD via git, so it needs
// a real repo; nothing ever commits again, which is the wedged case under test.
func prrGitRepoWithCommit(t *testing.T) (wtPath, headSHA string) {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	run("add", "f.txt")
	run("commit", "-q", "-m", "seed")
	sha := run("rev-parse", "HEAD")
	for len(sha) > 0 && (sha[len(sha)-1] == '\n' || sha[len(sha)-1] == '\r') {
		sha = sha[:len(sha)-1]
	}
	return dir, sha
}

// TestResumeSubmitEnter_SendsTheWholeRetryBurst pins that the implementer-resume
// submit is a BURST, not a single Enter: one immediately plus resumeSubmitRetries
// more.
//
// The single-Enter version is what hk-ip33d fixed. A refactor that collapses this
// back to one Enter reintroduces the dropped-submit wedge, and would do it
// silently, because a swallowed Enter has no error to observe — the only symptom
// is a run that goes quiet for thirty minutes.
func TestResumeSubmitEnter_SendsTheWholeRetryBurst(t *testing.T) {
	origDelay := daemon.ExportedResumeSubmitRetryDelay()
	daemon.ExportedSetResumeSubmitRetryDelay(time.Millisecond)
	t.Cleanup(func() { daemon.ExportedSetResumeSubmitRetryDelay(origDelay) })

	rec := &prrRecorder{}
	daemon.ExportedSendResumeSubmitEnter(context.Background(), rec)

	want := 1 + *daemon.ExportedResumeSubmitRetries
	if enters, _, _ := rec.counts(); enters != want {
		t.Errorf("resume submit sent %d Enters, want %d (1 initial + %d retries); "+
			"a single Enter is the hk-ip33d wedge",
			enters, want, *daemon.ExportedResumeSubmitRetries)
	}
}

// TestResumeSubmitEnter_StopsOnCancelledContext pins that the retry burst honours
// cancellation rather than sending its full spread into a torn-down pane.
func TestResumeSubmitEnter_StopsOnCancelledContext(t *testing.T) {
	origDelay := daemon.ExportedResumeSubmitRetryDelay()
	daemon.ExportedSetResumeSubmitRetryDelay(time.Hour) // never elapses
	t.Cleanup(func() { daemon.ExportedSetResumeSubmitRetryDelay(origDelay) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rec := &prrRecorder{}
	daemon.ExportedSendResumeSubmitEnter(ctx, rec)

	// The first Enter is unconditional; every retry must bail on ctx.Done.
	if enters, _, _ := rec.counts(); enters != 1 {
		t.Errorf("cancelled resume submit sent %d Enters, want exactly 1 (the unconditional first)", enters)
	}
}

// TestQuitOnCommit_ReseedEnterRescuesTheSwallowedSubmit is the 2026-06-10
// incident guard: with the brief delivered and no commit appearing, the watchdog
// MUST fire one reseed-Enter after implementerReseedGrace.
//
// Without it the only recovery was the 30-minute commitPollTimeout kill, and the
// run misreported itself as the implementer ignoring reviewer feedback.
func TestQuitOnCommit_ReseedEnterRescuesTheSwallowedSubmit(t *testing.T) {
	wtPath, headSHA := prrGitRepoWithCommit(t)

	origGrace := *daemon.ExportedImplementerReseedGrace
	*daemon.ExportedImplementerReseedGrace = 50 * time.Millisecond
	t.Cleanup(func() { *daemon.ExportedImplementerReseedGrace = origGrace })

	rec := &prrRecorder{}
	briefDelivered := make(chan struct{})
	close(briefDelivered) // brief is on the pane; the submit Enters were swallowed
	noChange := make(chan struct{}, 1)

	// Bounded: long enough for the grace to elapse and several ticks to run,
	// far below any kill deadline, so the reseed is the only thing observed.
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnCommit(
		ctx, rec, rec, wtPath, headSHA, noChange, briefDelivered, nil)

	enters, _, _ := rec.counts()
	if enters == 0 {
		t.Fatal("no reseed-Enter fired after the grace elapsed with no commit — " +
			"a brief left typed-but-unsubmitted now has no recovery short of the 30-minute kill (hk-76n5g)")
	}
	if enters != 1 {
		t.Errorf("reseed-Enter fired %d times, want exactly 1; it is one-shot by design — "+
			"repeated Enters inject blank lines into a live REPL", enters)
	}
}

// TestQuitOnCommit_NoReseedEnterOnceCommitLands pins the other half: an
// implementer that commits inside the grace window must NOT be sent a spurious
// Enter. That Enter would land at a clear prompt as a stray blank line.
func TestQuitOnCommit_NoReseedEnterOnceCommitLands(t *testing.T) {
	wtPath, headSHA := prrGitRepoWithCommit(t)

	origGrace := *daemon.ExportedImplementerReseedGrace
	// Long grace: the commit below is detected first, so the reseed never comes due.
	*daemon.ExportedImplementerReseedGrace = time.Hour
	t.Cleanup(func() { *daemon.ExportedImplementerReseedGrace = origGrace })

	// Land a second commit so HEAD != initialSHA on the first poll.
	cmd := exec.Command("git", "commit", "-q", "--allow-empty", "-m", "implementer work")
	cmd.Dir = wtPath
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	rec := &prrRecorder{}
	briefDelivered := make(chan struct{})
	close(briefDelivered)
	noChange := make(chan struct{}, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnCommit(
		ctx, rec, rec, wtPath, headSHA, noChange, briefDelivered, nil)

	enters, quits, _ := rec.counts()
	if enters != 0 {
		t.Errorf("sent %d reseed-Enter(s) to an implementer that had already committed; "+
			"that is a stray blank line at a clear prompt", enters)
	}
	if quits == 0 {
		t.Error("no /quit sent after the commit was detected — the Stop hook never fires and the session lingers")
	}
}
