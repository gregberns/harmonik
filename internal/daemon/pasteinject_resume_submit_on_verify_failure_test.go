package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// failVerifyPaster accepts every WriteLastPane call but its CaptureLastPane
// never shows the marker - the observed failure mode on the implementer-resume
// path, where the first line of the resume message (carrying "agent-task.md")
// scrolls out of the pane while the second paragraph stays visible.
type failVerifyPaster struct {
	prrRecorder
	writes int
}

func (p *failVerifyPaster) WriteLastPane(context.Context, string, []byte) error {
	p.mu.Lock()
	p.writes++
	p.mu.Unlock()
	return nil
}

func (p *failVerifyPaster) CaptureLastPane(context.Context, int) (string, error) {
	return "Before continuing, also read .harmonik/reviewer-feedback.iter-1.md", nil
}

// TestImplementerResumeSubmitsOnVerifyFailure is the hk-uk6cd regression guard:
// a failed seed verification is not evidence the paste failed, and must not
// skip the Enter that submits the prompt. Before this fix, pasteInjectImplementerResume
// returned early on a failed injectAndVerifySeed, so sendResumeSubmitEnter never
// ran and a complete, correctly delivered prompt sat unsubmitted - turning a
// cosmetic verification miss into a hung run holding a worker slot indefinitely.
func TestImplementerResumeSubmitsOnVerifyFailure(t *testing.T) {
	origAttempts := *daemon.ExportedPasteVerifyAttempts
	*daemon.ExportedPasteVerifyAttempts = 1
	t.Cleanup(func() { *daemon.ExportedPasteVerifyAttempts = origAttempts })

	origBackoff := daemon.ExportedPasteVerifyBackoff()
	daemon.ExportedSetPasteVerifyBackoff(time.Millisecond)
	t.Cleanup(func() { daemon.ExportedSetPasteVerifyBackoff(origBackoff) })

	origDelay := daemon.ExportedResumeSubmitRetryDelay()
	daemon.ExportedSetResumeSubmitRetryDelay(time.Millisecond)
	t.Cleanup(func() { daemon.ExportedSetResumeSubmitRetryDelay(origDelay) })

	origSplash := daemon.ExportedSplashDismissDelay()
	daemon.ExportedSetSplashDismissDelay(time.Millisecond)
	t.Cleanup(func() { daemon.ExportedSetSplashDismissDelay(origSplash) })

	wtPath := t.TempDir()
	harmonikDir := filepath.Join(wtPath, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o750); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	if err := os.WriteFile(filepath.Join(harmonikDir, "agent-task.md"),
		[]byte("# task\n"), 0o600); err != nil {
		t.Fatalf("write agent-task.md: %v", err)
	}

	rec := &failVerifyPaster{}
	reason := daemon.ExportedPasteInjectImplementerResume(
		context.Background(), rec, "sess-1", 2, wtPath)

	if reason == "" {
		t.Fatal("expected a non-empty failure reason from a verification that never sees the marker")
	}

	if rec.writes == 0 {
		t.Fatal("the resume brief was never pasted")
	}

	wantEnters := 1 + (1 + *daemon.ExportedResumeSubmitRetries)
	enters, _, _ := rec.counts()
	if enters != wantEnters {
		t.Errorf("implementer-resume sent %d Enters after a failed verification, want %d "+
			"(1 splash-dismiss + 1 submit + %d submit-retries); a failed verification must not "+
			"skip the submit Enter - that is what converts a cosmetic verify miss into a hung run",
			enters, wantEnters, *daemon.ExportedResumeSubmitRetries)
	}
}
