package daemon_test

// codexthreadid_mzgh_test.go — unit tests for codex thread_id capture and
// resume launchspec construction (hk-mzgh, G2 fix).
//
// Coverage:
//
//  1. codexThreadIDInterceptor (via ExportedNewCodexThreadIDInterceptor):
//     - fires callback once on first thread.started event in the JSONL stream.
//     - passes all bytes through unchanged.
//     - ignores subsequent thread.started events (first wins).
//     - does not fire when no thread.started is present.
//
//  2. buildCodexLaunchSpec resume argv (via ExportedBuildCodexLaunchSpec):
//     - resume command includes the thread_id in "exec resume <id>" position.
//     - resume command does NOT include "-C" (codex exec resume rejects -C).

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// codexThreadIDInterceptor tests
// ─────────────────────────────────────────────────────────────────────────────

// TestCodexThreadIDInterceptor_FiresOnThreadStarted_mzgh verifies that the
// interceptor fires the callback exactly once with the captured thread_id.
func TestCodexThreadIDInterceptor_FiresOnThreadStarted_mzgh(t *testing.T) {
	t.Parallel()

	jsonlStream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th_mzgh_abc"}`,
		`{"type":"turn.started","turn_id":"tr_1"}`,
		`{"type":"turn.completed","turn_id":"tr_1"}`,
	}, "\n") + "\n"

	var capturedID string
	fired := 0
	cb := func(id string) {
		fired++
		capturedID = id
	}

	interceptor := daemon.ExportedNewCodexThreadIDInterceptor(strings.NewReader(jsonlStream), cb)
	_, err := io.ReadAll(interceptor)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if fired != 1 {
		t.Errorf("callback fired %d times; want exactly 1", fired)
	}
	if capturedID != "th_mzgh_abc" {
		t.Errorf("capturedID = %q; want %q", capturedID, "th_mzgh_abc")
	}
}

// TestCodexThreadIDInterceptor_PassesThrough_mzgh verifies that all bytes from
// the underlying reader are passed through unchanged.
func TestCodexThreadIDInterceptor_PassesThrough_mzgh(t *testing.T) {
	t.Parallel()

	jsonlStream := `{"type":"thread.started","thread_id":"th_passthrough"}` + "\n" +
		`{"type":"turn.completed","turn_id":"tr_1"}` + "\n"

	interceptor := daemon.ExportedNewCodexThreadIDInterceptor(strings.NewReader(jsonlStream), func(string) {})
	got, err := io.ReadAll(interceptor)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, []byte(jsonlStream)) {
		t.Errorf("pass-through mismatch:\ngot  %q\nwant %q", got, jsonlStream)
	}
}

// TestCodexThreadIDInterceptor_FirstThreadStartedWins_mzgh verifies that a
// resumed stream re-emitting thread.started does not clobber the first capture.
func TestCodexThreadIDInterceptor_FirstThreadStartedWins_mzgh(t *testing.T) {
	t.Parallel()

	jsonlStream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th_first"}`,
		`{"type":"thread.started","thread_id":"th_second_must_be_ignored"}`,
		`{"type":"turn.completed","turn_id":"tr_1"}`,
	}, "\n") + "\n"

	var capturedID string
	fired := 0
	interceptor := daemon.ExportedNewCodexThreadIDInterceptor(strings.NewReader(jsonlStream), func(id string) {
		fired++
		capturedID = id
	})
	if _, err := io.ReadAll(interceptor); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if fired != 1 {
		t.Errorf("callback fired %d times; want exactly 1", fired)
	}
	if capturedID != "th_first" {
		t.Errorf("capturedID = %q; want %q (first thread.started must win)", capturedID, "th_first")
	}
}

// TestCodexThreadIDInterceptor_NoThreadStarted_mzgh verifies that the callback
// is never fired when the stream contains no thread.started event.
func TestCodexThreadIDInterceptor_NoThreadStarted_mzgh(t *testing.T) {
	t.Parallel()

	jsonlStream := strings.Join([]string{
		`{"type":"turn.started","turn_id":"tr_1"}`,
		`{"type":"turn.completed","turn_id":"tr_1"}`,
	}, "\n") + "\n"

	fired := 0
	interceptor := daemon.ExportedNewCodexThreadIDInterceptor(strings.NewReader(jsonlStream), func(string) {
		fired++
	})
	if _, err := io.ReadAll(interceptor); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if fired != 0 {
		t.Errorf("callback fired %d times; want 0 (no thread.started in stream)", fired)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// buildCodexLaunchSpec resume argv tests (hk-mzgh — -C removal)
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildCodexLaunchSpec_ResumeHasThreadID_mzgh asserts that the resume argv
// encodes the captured thread_id in the "exec resume <id>" position.
func TestBuildCodexLaunchSpec_ResumeHasThreadID_mzgh(t *testing.T) {
	t.Parallel()

	threadID := "th_captured_mzgh_resume"
	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath:    "/tmp/wt-mzgh-resume",
		BeadID:           "hk-mzgh-test-resume",
		PriorThreadID:    &threadID,
		SkipBillingGuard: true,
	}

	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Fatalf("ExportedBuildCodexLaunchSpec: %v", err)
	}

	// argv must contain the sequence: exec resume <thread_id>
	codexMzghAssertArgSeq(t, spec.Args, "exec", "resume", threadID)
}

// TestBuildCodexLaunchSpec_ResumeNoCFlag_mzgh asserts that the resume argv
// does NOT contain "-C". codex exec resume rejects -C with exit 2.
func TestBuildCodexLaunchSpec_ResumeNoCFlag_mzgh(t *testing.T) {
	t.Parallel()

	threadID := "th_captured_mzgh_nocflag"
	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath:    "/tmp/wt-mzgh-nocflag",
		BeadID:           "hk-mzgh-test-nocflag",
		PriorThreadID:    &threadID,
		SkipBillingGuard: true,
	}

	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Fatalf("ExportedBuildCodexLaunchSpec: %v", err)
	}

	for _, arg := range spec.Args {
		if arg == "-C" {
			t.Errorf("resume argv must not contain -C (codex exec resume rejects it); got %v", spec.Args)
			return
		}
	}
}

// TestBuildCodexLaunchSpec_InitialHasCFlag_mzgh asserts that the INITIAL (non-resume)
// argv still includes -C (it is only the resume subcommand that rejects it).
func TestBuildCodexLaunchSpec_InitialHasCFlag_mzgh(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath:    "/tmp/wt-mzgh-initial-cflag",
		BeadID:           "hk-mzgh-test-initial-cflag",
		SkipBillingGuard: true,
	}

	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Fatalf("ExportedBuildCodexLaunchSpec: %v", err)
	}

	foundC := false
	for i, arg := range spec.Args {
		if arg == "-C" {
			foundC = true
			if i+1 < len(spec.Args) && spec.Args[i+1] != rc.WorkspacePath {
				t.Errorf("-C value = %q; want %q", spec.Args[i+1], rc.WorkspacePath)
			}
			break
		}
	}
	if !foundC {
		t.Errorf("initial argv must contain -C; got %v", spec.Args)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// codexMzghAssertArgSeq asserts that seq appears as a contiguous subsequence
// within args (in order). Suffix _mzgh avoids same-package helper collisions.
func codexMzghAssertArgSeq(t *testing.T, args []string, seq ...string) {
	t.Helper()
	for start := 0; start+len(seq) <= len(args); start++ {
		match := true
		for i, s := range seq {
			if args[start+i] != s {
				match = false
				break
			}
		}
		if match {
			return
		}
	}
	t.Errorf("argv %v does not contain contiguous sequence %v", args, seq)
}
