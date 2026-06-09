package daemon_test

// codexharness_test.go — CodexHarness unit tests (codex-harness C2/T8, hk-m57va).
//
// Coverage:
//   - constant-method enums: AgentType=codex, SessionIDPolicy=Captured,
//     Completion=ProcessExit.
//   - DetectReady: true for agent_ready, false for launch_initiated (HC-041) and
//     unrelated events.
//   - LaunchSpec delegates to buildCodexLaunchSpec: initial argv on nil
//     PriorSessionID, resume argv on non-nil PriorSessionID (captured thread_id),
//     credential strip parity (OPENAI_API_KEY/CODEX_API_KEY → empty overrides),
//     CODEX_HOME present, WorkDir = workspace.
//   - Seed/Retask/Teardown are no-op / nil-safe.

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Constant-method tests
// ─────────────────────────────────────────────────────────────────────────────

// TestCodexHarness_AgentType verifies AgentType returns AgentTypeCodex.
func TestCodexHarness_AgentType(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewCodexHarness("", "")
	if got := h.AgentType(); got != core.AgentTypeCodex {
		t.Errorf("AgentType = %q; want %q", got, core.AgentTypeCodex)
	}
}

// TestCodexHarness_SessionIDPolicy verifies SessionIDPolicy returns
// SessionIDCaptured (codex captures the thread_id from the JSONL stream).
func TestCodexHarness_SessionIDPolicy(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewCodexHarness("", "")
	if got := h.SessionIDPolicy(); got != handlercontract.SessionIDCaptured {
		t.Errorf("SessionIDPolicy = %v; want SessionIDCaptured", got)
	}
}

// TestCodexHarness_Completion verifies Completion returns CompletionProcessExit
// (codex exec self-terminates on turn completion).
func TestCodexHarness_Completion(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewCodexHarness("", "")
	if got := h.Completion(); got != handlercontract.CompletionProcessExit {
		t.Errorf("Completion = %v; want CompletionProcessExit", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DetectReady tests
// ─────────────────────────────────────────────────────────────────────────────

// TestCodexHarness_DetectReady_AgentReady verifies DetectReady returns true for
// an agent_ready event.
func TestCodexHarness_DetectReady_AgentReady(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewCodexHarness("", "")
	ev := handlercontract.EventEnvelope{Type: string(core.EventTypeAgentReady)}
	if !h.DetectReady(ev) {
		t.Error("DetectReady(agent_ready) = false; want true")
	}
}

// TestCodexHarness_DetectReady_LaunchInitiated verifies DetectReady returns false
// for launch_initiated (HC-041 hard rule).
func TestCodexHarness_DetectReady_LaunchInitiated(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewCodexHarness("", "")
	ev := handlercontract.EventEnvelope{Type: string(core.EventTypeLaunchInitiated)}
	if h.DetectReady(ev) {
		t.Error("DetectReady(launch_initiated) = true; want false (HC-041)")
	}
}

// TestCodexHarness_DetectReady_OtherEvent verifies DetectReady returns false for
// an unrelated event type.
func TestCodexHarness_DetectReady_OtherEvent(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewCodexHarness("", "")
	ev := handlercontract.EventEnvelope{Type: "run_started"}
	if h.DetectReady(ev) {
		t.Error("DetectReady(run_started) = true; want false")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// LaunchSpec delegation tests
// ─────────────────────────────────────────────────────────────────────────────

// TestCodexHarness_LaunchSpec_InitialDelegates verifies the harness LaunchSpec
// produces the same initial-turn argv as buildCodexLaunchSpec: codex exec --json
// --sandbox workspace-write -a never -C <wt> <seed>, with no "resume".
func TestCodexHarness_LaunchSpec_InitialDelegates(t *testing.T) {
	t.Parallel()

	rc := handlercontract.RunCtx{
		WorkspacePath: "/tmp/wt-codex-harness-initial",
		BeadID:        "hk-m57va-test-initial",
		BaseEnv:       []string{"PATH=/usr/bin"},
	}

	h := daemon.ExportedNewCodexHarness("", "")
	spawn, err := h.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("CodexHarness.LaunchSpec: %v", err)
	}

	if spawn.Binary != "codex" {
		t.Errorf("Binary = %q; want %q", spawn.Binary, "codex")
	}
	if spawn.WorkDir != rc.WorkspacePath {
		t.Errorf("WorkDir = %q; want %q", spawn.WorkDir, rc.WorkspacePath)
	}
	codexHarnessAssertArgContainsSeq(t, spawn.Args, "exec", "--json")
	codexHarnessAssertArgValue(t, spawn.Args, "--sandbox", "workspace-write")
	codexHarnessAssertArgValue(t, spawn.Args, "-a", "never")
	codexHarnessAssertArgValue(t, spawn.Args, "-C", rc.WorkspacePath)
	if codexHarnessArgsContain(spawn.Args, "resume") {
		t.Errorf("initial-turn argv must not contain \"resume\": %v", spawn.Args)
	}
	// Seed prompt references the bead ID.
	if !codexHarnessSeedReferencesBead(spawn.Args, rc.BeadID) {
		t.Errorf("seed prompt does not reference bead ID %q in args %v", rc.BeadID, spawn.Args)
	}
}

// TestCodexHarness_LaunchSpec_ResumeDelegates verifies the harness LaunchSpec
// emits the resume argv when PriorSessionID (captured thread_id) is set:
// codex exec resume <thread_id> ...
func TestCodexHarness_LaunchSpec_ResumeDelegates(t *testing.T) {
	t.Parallel()

	threadID := "th_captured_abc123"
	rc := handlercontract.RunCtx{
		WorkspacePath:  "/tmp/wt-codex-harness-resume",
		BeadID:         "hk-m57va-test-resume",
		BaseEnv:        []string{"PATH=/usr/bin"},
		PriorSessionID: &threadID,
	}

	h := daemon.ExportedNewCodexHarness("", "")
	spawn, err := h.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("CodexHarness.LaunchSpec: %v", err)
	}

	// argv: exec resume <thread_id> ...
	codexHarnessAssertArgContainsSeq(t, spawn.Args, "exec", "resume", threadID)
}

// TestCodexHarness_LaunchSpec_CustomBinary verifies the constructor's codexBinary
// flows through to SpawnSpec.Binary.
func TestCodexHarness_LaunchSpec_CustomBinary(t *testing.T) {
	t.Parallel()

	rc := handlercontract.RunCtx{
		WorkspacePath: "/tmp/wt-codex-harness-bin",
		BeadID:        "hk-m57va-test-bin",
	}

	h := daemon.ExportedNewCodexHarness("/usr/local/bin/codex", "")
	spawn, err := h.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("CodexHarness.LaunchSpec: %v", err)
	}
	if spawn.Binary != "/usr/local/bin/codex" {
		t.Errorf("Binary = %q; want %q", spawn.Binary, "/usr/local/bin/codex")
	}
}

// TestCodexHarness_LaunchSpec_CredentialKeysStripped verifies OPENAI_API_KEY and
// CODEX_API_KEY are stripped to empty overrides (C3 credential-strip parity).
func TestCodexHarness_LaunchSpec_CredentialKeysStripped(t *testing.T) {
	t.Parallel()

	rc := handlercontract.RunCtx{
		WorkspacePath: "/tmp/wt-codex-harness-creds",
		BeadID:        "hk-m57va-test-creds",
		BaseEnv: []string{
			"PATH=/usr/bin",
			"OPENAI_API_KEY=sentinel-must-not-reach-child",
			"CODEX_API_KEY=sentinel-must-not-reach-child",
		},
	}

	h := daemon.ExportedNewCodexHarness("", "")
	spawn, err := h.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("CodexHarness.LaunchSpec: %v", err)
	}

	denyKeys := []string{"OPENAI_API_KEY", "CODEX_API_KEY"}
	for _, kv := range spawn.Env {
		for _, dk := range denyKeys {
			prefix := dk + "="
			if strings.HasPrefix(kv, prefix) && len(kv) > len(prefix) {
				t.Errorf("credential strip: SpawnSpec carries live value for %q; want empty override", dk)
			}
		}
	}
	// Empty override must be present.
	for _, dk := range denyKeys {
		want := dk + "="
		if !codexHarnessArgsContain(spawn.Env, want) {
			t.Errorf("credential strip: SpawnSpec.Env missing empty override %q", dk)
		}
	}
}

// TestCodexHarness_LaunchSpec_CodexHomePresent verifies CODEX_HOME is set in the
// child env (AC3.4).
func TestCodexHarness_LaunchSpec_CodexHomePresent(t *testing.T) {
	t.Parallel()

	rc := handlercontract.RunCtx{
		WorkspacePath: "/tmp/wt-codex-harness-home",
		BeadID:        "hk-m57va-test-home",
	}

	h := daemon.ExportedNewCodexHarness("", "/custom/codex/home")
	spawn, err := h.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("CodexHarness.LaunchSpec: %v", err)
	}
	if !codexHarnessArgsContain(spawn.Env, "CODEX_HOME=/custom/codex/home") {
		t.Errorf("SpawnSpec.Env missing CODEX_HOME=/custom/codex/home: %v", spawn.Env)
	}
}

// TestCodexHarness_LaunchSpec_EmptyWorkspaceErrors verifies buildCodexLaunchSpec
// validation propagates through the harness (empty workspacePath → error).
func TestCodexHarness_LaunchSpec_EmptyWorkspaceErrors(t *testing.T) {
	t.Parallel()

	rc := handlercontract.RunCtx{
		WorkspacePath: "",
		BeadID:        "hk-m57va-test-err",
	}

	h := daemon.ExportedNewCodexHarness("", "")
	if _, err := h.LaunchSpec(rc); err == nil {
		t.Error("LaunchSpec with empty WorkspacePath: want error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Seed / Retask / Teardown no-op tests
// ─────────────────────────────────────────────────────────────────────────────

// TestCodexHarness_Seed_NoOp verifies Seed returns nil (codex delivers the task
// via argv; there is nothing to paste).
func TestCodexHarness_Seed_NoOp(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewCodexHarness("", "")
	if err := h.Seed(nil, handlercontract.RunCtx{}); err != nil {
		t.Errorf("Seed = %v; want nil", err)
	}
}

// TestCodexHarness_Retask_NoOp verifies Retask returns nil (codex re-task is the
// resume argv, not a REPL write).
func TestCodexHarness_Retask_NoOp(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewCodexHarness("", "")
	if err := h.Retask(nil, "some feedback", handlercontract.RunCtx{}); err != nil {
		t.Errorf("Retask = %v; want nil", err)
	}
}

// TestCodexHarness_Teardown_NilSession verifies Teardown is nil-safe with a nil
// session (codex self-terminates, so a nil handle is the common case).
func TestCodexHarness_Teardown_NilSession(t *testing.T) {
	t.Parallel()

	h := daemon.ExportedNewCodexHarness("", "")
	if err := h.Teardown(nil); err != nil {
		t.Errorf("Teardown(nil) = %v; want nil", err)
	}
}

// TestCodexHarness_Teardown_LiveSessionKilled verifies Teardown calls Kill on a
// live session handle (defensive teardown after a timeout).
func TestCodexHarness_Teardown_LiveSessionKilled(t *testing.T) {
	t.Parallel()

	sess := &codexHarnessFakeSession{}
	h := daemon.ExportedNewCodexHarness("", "")
	if err := h.Teardown(sess); err != nil {
		t.Fatalf("Teardown(live) = %v; want nil", err)
	}
	if !sess.killed {
		t.Error("Teardown(live) did not call Kill on the session")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers + fake session
// ─────────────────────────────────────────────────────────────────────────────

// codexHarnessArgsContain reports whether args contains the exact element want.
func codexHarnessArgsContain(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// codexHarnessAssertArgContainsSeq asserts that seq appears as a contiguous
// subsequence within args (in order).
func codexHarnessAssertArgContainsSeq(t *testing.T, args []string, seq ...string) {
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

// codexHarnessAssertArgValue asserts that flag is immediately followed by
// wantValue in args.
func codexHarnessAssertArgValue(t *testing.T, args []string, flag, wantValue string) {
	t.Helper()
	for i, a := range args {
		if a == flag {
			if i+1 >= len(args) {
				t.Errorf("%s present but has no following value", flag)
				return
			}
			if args[i+1] != wantValue {
				t.Errorf("arg after %s = %q; want %q", flag, args[i+1], wantValue)
			}
			return
		}
	}
	t.Errorf("%s not found in args %v", flag, args)
}

// codexHarnessSeedReferencesBead reports whether any arg contains the bead ID
// (the seed prompt instructs codex to commit with a Refs:<bead> trailer).
func codexHarnessSeedReferencesBead(args []string, beadID string) bool {
	for _, a := range args {
		if strings.Contains(a, beadID) {
			return true
		}
	}
	return false
}

// codexHarnessFakeSession is a minimal handlercontract.Session stub that records
// whether Kill was called. Only Kill is exercised by the Teardown tests; the
// other methods return zero values.
type codexHarnessFakeSession struct {
	killed bool
}

func (s *codexHarnessFakeSession) ID() core.SessionID { return "" }
func (s *codexHarnessFakeSession) SendInput(ctx context.Context, input string) error {
	return nil
}
func (s *codexHarnessFakeSession) Attach(ctx context.Context) (io.Reader, error) { return nil, nil }
func (s *codexHarnessFakeSession) Kill(ctx context.Context) error {
	s.killed = true
	return nil
}
func (s *codexHarnessFakeSession) Wait(ctx context.Context) (core.Outcome, error) {
	return core.Outcome{}, nil
}
func (s *codexHarnessFakeSession) LogLocation() string { return "" }
