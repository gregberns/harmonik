package daemon_test

// sandboxgate_engagement_hk5wdon_test.go — production-path srt
// sandbox-engagement verification (hk-5wdon, follow-up to hk-tch4t).
//
// # Why this exists
//
// hk-tch4t hardened the TEST-side discriminator (hktch4tRetryUntilDenied, a
// _test.go helper used by the i0377 scenario suite) against a transient srt
// sandbox_init apply-failure under fork saturation: srt can exit 0 while the
// Seatbelt profile silently never engages, so the "wrapped" command actually
// runs unsandboxed. That discriminator only protects the TEST suite. The
// PRODUCTION spawn path (workloop.go single-mode, dot_cascade.go) had NO
// analogous check: sandboxWrapExecArgv/srtWrapArgv build the argv and trust
// srt's exit code implicitly — an apply-failure in production would let a
// run write outside its worktree with nothing catching it.
//
// These tests drive verifySandboxEngaged directly against a STUB srt binary
// (a tiny shell script standing in for /opt/homebrew/bin/srt) so the fault
// injection is deterministic and has zero dependency on macOS Seatbelt, real
// srt, or fork-saturation timing to reproduce. This is exactly the "stub-srt
// binary ... forcing sandbox_init failure" proof hk-5wdon's scope calls for.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// hk5wdonWriteStubSrt writes an executable shell script at binPath that,
// regardless of the --settings/-c arguments it receives, unconditionally runs
// the trailing -c script argument via `sh -c` and exits 0 — the exact shape of
// "srt ran the child WITHOUT the sandbox engaging" (hk-tch4t's diagnosed
// apply-failure mode: srt exits 0, but the write it was supposed to deny goes
// through).
func hk5wdonWriteApplyFailureStubSrt(t *testing.T, binPath string) {
	t.Helper()
	script := "#!/bin/bash\n" +
		`last="${@: -1}"` + "\n" +
		`sh -c "$last"` + "\n" +
		"exit 0\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil { //nolint:gosec // G306: test fixture, must be executable
		t.Fatalf("write apply-failure stub srt: %v", err)
	}
}

// hk5wdonWriteDenyingStubSrt writes an executable shell script that refuses to
// run the wrapped command at all and exits 1 — the shape of a genuinely
// engaged sandbox denying the write.
func hk5wdonWriteDenyingStubSrt(t *testing.T, binPath string) {
	t.Helper()
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil { //nolint:gosec // G306: test fixture, must be executable
		t.Fatalf("write denying stub srt: %v", err)
	}
}

func hk5wdonProfileInput(t *testing.T, runID string) daemon.SandboxProfileInput {
	t.Helper()
	wtDir := t.TempDir()
	return daemon.SandboxProfileInput{
		WorktreePath:   wtDir,
		GitDir:         filepath.Join(wtDir, ".git-fake"),
		RunID:          runID,
		DaemonSockPath: filepath.Join(t.TempDir(), "daemon.sock"),
	}
}

// TestVerifySandboxEngaged_HardFailsOnStubSrtApplyFailure is the RED-before
// proof: with the production verifier absent (pre-hk-5wdon), an srt that
// exits 0 without actually engaging the sandbox would be silently trusted —
// the write-to-main hole hk-5wdon exists to close. Post-fix, verifySandboxEngaged
// must return a non-nil error, and the canary path must be left CLEAN (no
// residual write survives verification) so a caller that ignores the error
// cannot stumble onto stale evidence of a "successful" write.
func TestVerifySandboxEngaged_HardFailsOnStubSrtApplyFailure(t *testing.T) {
	t.Parallel()

	binDir := t.TempDir()
	stubPath := filepath.Join(binDir, "srt")
	hk5wdonWriteApplyFailureStubSrt(t, stubPath)

	runID := "hk5wdon-applyfail"
	canaryPath := filepath.Join(t.TempDir(), "canary.txt")
	spawn := &daemon.ExportedSrtSpawnConfig{
		SrtBinary:    stubPath,
		ProfileInput: hk5wdonProfileInput(t, runID),
	}

	var logLines []string
	logf := func(format string, args ...any) {
		logLines = append(logLines, format)
		_ = args
	}

	err := daemon.ExportedVerifySandboxEngaged(context.Background(), spawn, canaryPath, logf)
	if err == nil {
		t.Fatal("verifySandboxEngaged returned nil for a consistently apply-failing stub srt; " +
			"a genuine sandbox_init apply-failure must never be masked as engaged (hk-tch4t hole)")
	}
	if !strings.Contains(err.Error(), "hk-tch4t") && !strings.Contains(err.Error(), "apply-failure") {
		t.Errorf("engagement error lacks diagnostic context: %v", err)
	}
	if _, statErr := os.Stat(canaryPath); !os.IsNotExist(statErr) {
		t.Errorf("canary file at %q survived a failed verification; want it cleaned up", canaryPath)
	}
	if len(logLines) == 0 {
		t.Error("verifySandboxEngaged logged nothing while retrying; a real failure must carry a diagnostic trail")
	}
}

// TestVerifySandboxEngaged_PassesOnStubSrtThatDenies proves the companion
// half: a stub srt that genuinely refuses the wrapped command (exit 1, no
// write) is accepted as engaged on the FIRST attempt — no unnecessary retries,
// no false failure on a healthy sandbox.
func TestVerifySandboxEngaged_PassesOnStubSrtThatDenies(t *testing.T) {
	t.Parallel()

	binDir := t.TempDir()
	stubPath := filepath.Join(binDir, "srt")
	hk5wdonWriteDenyingStubSrt(t, stubPath)

	runID := "hk5wdon-denies"
	canaryPath := filepath.Join(t.TempDir(), "canary.txt")
	spawn := &daemon.ExportedSrtSpawnConfig{
		SrtBinary:    stubPath,
		ProfileInput: hk5wdonProfileInput(t, runID),
	}

	if err := daemon.ExportedVerifySandboxEngaged(context.Background(), spawn, canaryPath, nil); err != nil {
		t.Fatalf("verifySandboxEngaged returned an error for a genuinely-denying stub srt: %v", err)
	}
}

// TestVerifySandboxEngaged_AbsorbsSingleTransientThenEngages mirrors
// hktch4tRetryUntilDenied's transient-absorption guarantee for the PRODUCTION
// verifier: a single early apply-failure followed by a real denial must still
// pass, or the retry budget added to absorb hk-tch4t's diagnosed transient
// would be pointless.
func TestVerifySandboxEngaged_AbsorbsSingleTransientThenEngages(t *testing.T) {
	t.Parallel()

	binDir := t.TempDir()
	stubPath := filepath.Join(binDir, "srt")
	counterPath := filepath.Join(binDir, "calls")

	// First invocation: apply-failure (writes + exits 0). Every later
	// invocation: genuine denial (exits 1, no write).
	script := "#!/bin/bash\n" +
		`n=$(cat "` + counterPath + `" 2>/dev/null || echo 0)` + "\n" +
		`n=$((n + 1))` + "\n" +
		`echo "$n" > "` + counterPath + `"` + "\n" +
		`if [ "$n" -eq 1 ]; then` + "\n" +
		`  last="${@: -1}"` + "\n" +
		`  sh -c "$last"` + "\n" +
		`  exit 0` + "\n" +
		`fi` + "\n" +
		`exit 1` + "\n"
	if err := os.WriteFile(stubPath, []byte(script), 0o755); err != nil { //nolint:gosec // G306: test fixture, must be executable
		t.Fatalf("write transient stub srt: %v", err)
	}

	runID := "hk5wdon-transient"
	canaryPath := filepath.Join(t.TempDir(), "canary.txt")
	spawn := &daemon.ExportedSrtSpawnConfig{
		SrtBinary:    stubPath,
		ProfileInput: hk5wdonProfileInput(t, runID),
	}

	if err := daemon.ExportedVerifySandboxEngaged(context.Background(), spawn, canaryPath, nil); err != nil {
		t.Fatalf("verifySandboxEngaged failed after a single transient apply-failure: %v "+
			"(a lone transient must be absorbed, not treated as fatal)", err)
	}

	calls, readErr := os.ReadFile(counterPath)
	if readErr != nil {
		t.Fatalf("read call counter: %v", readErr)
	}
	if strings.TrimSpace(string(calls)) != "2" {
		t.Errorf("stub srt invoked %s times; want exactly 2 (stop immediately once denial is observed)", strings.TrimSpace(string(calls)))
	}
}

// TestSrtEngagementCanaryPath_OutsideAllowWrite pins the canary-path
// convention: it must land directly under projectDir (never inside the run
// worktree, which IS in the profile's allowWrite set) so the probe actually
// exercises a denied location. See GenerateSandboxProfile (hk-p7smp): only
// WorktreePath and specific git-internal paths are ever writable.
func TestSrtEngagementCanaryPath_OutsideAllowWrite(t *testing.T) {
	t.Parallel()

	projectDir := "/repo/main"
	got := daemon.ExportedSrtEngagementCanaryPath(projectDir, "run-123")

	if filepath.Dir(got) != projectDir {
		t.Errorf("canary path %q not directly under projectDir %q", got, projectDir)
	}
	if !strings.Contains(got, "run-123") {
		t.Errorf("canary path %q missing RunID suffix; concurrent runs would collide", got)
	}
}
