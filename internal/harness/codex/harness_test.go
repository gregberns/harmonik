package codex_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/codex"
)

// TestCodexHarness_AgentType verifies AgentType returns AgentTypeCodex.
func TestCodexHarness_AgentType(t *testing.T) {
	t.Parallel()

	h := codex.ExportedNewCodexHarness("", "")
	if got := h.AgentType(); got != core.AgentTypeCodex {
		t.Errorf("AgentType = %q; want %q", got, core.AgentTypeCodex)
	}
}

// TestCodexHarness_SessionIDPolicy verifies SessionIDPolicy returns
// SessionIDCaptured (codex captures the thread_id from the JSONL stream).
func TestCodexHarness_SessionIDPolicy(t *testing.T) {
	t.Parallel()

	h := codex.ExportedNewCodexHarness("", "")
	if got := h.SessionIDPolicy(); got != handlercontract.SessionIDCaptured {
		t.Errorf("SessionIDPolicy = %v; want SessionIDCaptured", got)
	}
}

// TestCodexHarness_Completion verifies Completion returns CompletionProcessExit
// (codex exec self-terminates on turn completion).
func TestCodexHarness_Completion(t *testing.T) {
	t.Parallel()

	h := codex.ExportedNewCodexHarness("", "")
	if got := h.Completion(); got != handlercontract.CompletionProcessExit {
		t.Errorf("Completion = %v; want CompletionProcessExit", got)
	}
}

// TestCodexHarness_DetectReady_AgentReady verifies DetectReady returns true for
// an agent_ready event.
func TestCodexHarness_DetectReady_AgentReady(t *testing.T) {
	t.Parallel()

	h := codex.ExportedNewCodexHarness("", "")
	ev := handlercontract.EventEnvelope{Type: core.EventTypeAgentReady}
	if !h.DetectReady(ev) {
		t.Error("DetectReady(agent_ready) = false; want true")
	}
}

// TestCodexHarness_DetectReady_LaunchInitiated verifies DetectReady returns false
// for launch_initiated (HC-041 hard rule).
func TestCodexHarness_DetectReady_LaunchInitiated(t *testing.T) {
	t.Parallel()

	h := codex.ExportedNewCodexHarness("", "")
	ev := handlercontract.EventEnvelope{Type: core.EventTypeLaunchInitiated}
	if h.DetectReady(ev) {
		t.Error("DetectReady(launch_initiated) = true; want false (HC-041)")
	}
}

// TestCodexHarness_DetectReady_OtherEvent verifies DetectReady returns false for
// an unrelated event type.
func TestCodexHarness_DetectReady_OtherEvent(t *testing.T) {
	t.Parallel()

	h := codex.ExportedNewCodexHarness("", "")
	ev := handlercontract.EventEnvelope{Type: "run_started"}
	if h.DetectReady(ev) {
		t.Error("DetectReady(run_started) = true; want false")
	}
}

// TestCodexHarness_LaunchSpec_InitialDelegates verifies the harness LaunchSpec
// produces the same initial-turn argv as buildCodexLaunchSpec: codex exec --json
// -c sandbox_mode="danger-full-access" -C <wt> <seed>, with no "resume".
func TestCodexHarness_LaunchSpec_InitialDelegates(t *testing.T) {
	t.Parallel()

	rc := handlercontract.RunCtx{
		WorkspacePath: "/tmp/wt-codex-harness-initial",
		BeadID:        "hk-m57va-test-initial",
		Model:         "o4-mini",
		BaseEnv:       []string{"PATH=/usr/bin"},
	}

	h := codex.ExportedNewCodexHarness("", t.TempDir())
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
	codexHarnessAssertArgContainsSeq(t, spawn.Args, "-c", `sandbox_mode="danger-full-access"`)
	if codexHarnessArgsContain(spawn.Args, "--sandbox") {
		t.Errorf("initial-turn argv must not contain the \"--sandbox\" flag (sandbox set via -c): %v", spawn.Args)
	}
	codexHarnessAssertArgValue(t, spawn.Args, "-C", rc.WorkspacePath)
	if codexHarnessArgsContain(spawn.Args, "resume") {
		t.Errorf("initial-turn argv must not contain \"resume\": %v", spawn.Args)
	}
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

	h := codex.ExportedNewCodexHarness("", t.TempDir())
	spawn, err := h.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("CodexHarness.LaunchSpec: %v", err)
	}

	codexHarnessAssertArgContainsSeq(t, spawn.Args, "exec", "resume", threadID)

	if codexHarnessArgsContain(spawn.Args, "-C") {
		t.Errorf("resume argv must not contain -C (codex exec resume rejects it): %v", spawn.Args)
	}
}

// TestCodexHarness_LaunchSpec_CustomBinary verifies the constructor's codexBinary
// flows through to SpawnSpec.Binary.
func TestCodexHarness_LaunchSpec_CustomBinary(t *testing.T) {
	t.Parallel()

	rc := handlercontract.RunCtx{
		WorkspacePath: "/tmp/wt-codex-harness-bin",
		BeadID:        "hk-m57va-test-bin",
		Model:         "o4-mini",
	}

	h := codex.ExportedNewCodexHarness("/usr/local/bin/codex", t.TempDir())
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
		Model:         "o4-mini",
		BaseEnv: []string{
			"PATH=/usr/bin",
			"OPENAI_API_KEY=sentinel-must-not-reach-child",
			"CODEX_API_KEY=sentinel-must-not-reach-child",
		},
	}

	h := codex.ExportedNewCodexHarness("", t.TempDir())
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
		Model:         "o4-mini",
	}

	codexHome := t.TempDir()
	h := codex.ExportedNewCodexHarness("", codexHome)
	spawn, err := h.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("CodexHarness.LaunchSpec: %v", err)
	}
	if !codexHarnessArgsContain(spawn.Env, "CODEX_HOME="+codexHome) {
		t.Errorf("SpawnSpec.Env missing CODEX_HOME=%s: %v", codexHome, spawn.Env)
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

	h := codex.ExportedNewCodexHarness("", t.TempDir())
	_, err := h.LaunchSpec(rc)
	if err == nil {
		t.Fatal("LaunchSpec with empty WorkspacePath: want error, got nil")
	}
	if !strings.Contains(err.Error(), "workspacePath must be non-empty") {
		t.Errorf("LaunchSpec with empty WorkspacePath returned the wrong error: %v\n"+
			"The test is named for workspace validation. An error from the billing guard or "+
			"the stale-WAL sweep would satisfy a bare non-nil check and measure nothing.", err)
	}
}

// TestCodexHarness_LaunchSpec_EmptyModelAccountDefault verifies that an empty Model
// on an initial turn launches WITHOUT --model through the harness adapter, so codex
// uses its config-default (account) model. Inverts the retired hk-heh3t fail-loud
// guard (the ~30-min omitted-model hang no longer reproduces, and a named model 400s
// on the HN-022 ChatGPT path).
//
// This comment said "Uses SkipBillingGuard" until 2026-08-07 and that was FALSE.
// It uses an isolated CODEX_HOME. SkipBillingGuard is a field on the package's
// own codex.RunCtx, not on handlercontract.RunCtx, and Harness.LaunchSpec builds
// the internal struct without it — so no test reaching the harness through the
// handlercontract seam can skip the guard, whatever it says. The pi harness
// records the same fact in its RunCtx doc comment.
func TestCodexHarness_LaunchSpec_EmptyModelAccountDefault(t *testing.T) {
	t.Parallel()

	rc := handlercontract.RunCtx{
		WorkspacePath: "/tmp/wt-codex-harness-nomodel",
		BeadID:        "hk-m57va-test-nomodel",
	}

	h := codex.ExportedNewCodexHarness("", t.TempDir())
	spec, err := h.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("LaunchSpec with empty Model on initial turn: want account-default launch, got error: %v", err)
	}
	for _, arg := range spec.Args {
		if arg == "--model" {
			t.Errorf("empty-model harness argv must omit --model; got %v", spec.Args)
			return
		}
	}
}

// TestCodexHarness_LaunchSpec_ModelFlagInInitialArgv verifies that RunCtx.Model
// flows through to --model <model> in the initial-turn argv (hk-heh3t).
func TestCodexHarness_LaunchSpec_ModelFlagInInitialArgv(t *testing.T) {
	t.Parallel()

	rc := handlercontract.RunCtx{
		WorkspacePath: "/tmp/wt-codex-harness-model-flag",
		BeadID:        "hk-m57va-test-model-flag",
		Model:         "o4-mini",
	}

	h := codex.ExportedNewCodexHarness("", t.TempDir())
	spawn, err := h.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("LaunchSpec: %v", err)
	}
	if !codexHarnessArgsContain(spawn.Args, "--model") {
		t.Errorf("initial-turn argv missing --model; got %v", spawn.Args)
	}
	codexHarnessAssertArgValue(t, spawn.Args, "--model", "o4-mini")
}

// TestCodexHarness_Seed_NoOp verifies Seed returns nil (codex delivers the task
// via argv; there is nothing to paste).
func TestCodexHarness_Seed_NoOp(t *testing.T) {
	t.Parallel()

	h := codex.ExportedNewCodexHarness("", "")
	if err := h.Seed(nil, handlercontract.RunCtx{}); err != nil {
		t.Errorf("Seed = %v; want nil", err)
	}
}

// TestCodexHarness_Retask_NoOp verifies Retask returns nil (codex re-task is the
// resume argv, not a REPL write).
func TestCodexHarness_Retask_NoOp(t *testing.T) {
	t.Parallel()

	h := codex.ExportedNewCodexHarness("", "")
	if err := h.Retask(nil, "some feedback", handlercontract.RunCtx{}); err != nil {
		t.Errorf("Retask = %v; want nil", err)
	}
}

// TestCodexHarness_Teardown_NilSession verifies Teardown is nil-safe with a nil
// session (codex self-terminates, so a nil handle is the common case).
func TestCodexHarness_Teardown_NilSession(t *testing.T) {
	t.Parallel()

	h := codex.ExportedNewCodexHarness("", "")
	if err := h.Teardown(nil); err != nil {
		t.Errorf("Teardown(nil) = %v; want nil", err)
	}
}

// TestCodexHarness_Teardown_LiveSessionKilled verifies Teardown calls Kill on a
// live session handle (defensive teardown after a timeout).
func TestCodexHarness_Teardown_LiveSessionKilled(t *testing.T) {
	t.Parallel()

	sess := &codexHarnessFakeSession{}
	h := codex.ExportedNewCodexHarness("", "")
	if err := h.Teardown(sess); err != nil {
		t.Fatalf("Teardown(live) = %v; want nil", err)
	}
	if !sess.killed {
		t.Error("Teardown(live) did not call Kill on the session")
	}
}

func codexHarnessArgsContain(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

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

func codexHarnessSeedReferencesBead(args []string, beadID string) bool {
	for _, a := range args {
		if strings.Contains(a, beadID) {
			return true
		}
	}
	return false
}

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
