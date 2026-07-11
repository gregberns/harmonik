package daemon_test

// codexlaunchspec_test.go — unit tests for buildCodexLaunchSpec (hk-rgxwd C2/T7).
//
// Key invariants tested:
//
//   - AC2.1: initial run argv = codex exec --json --sandbox workspace-write -C <wt> <seed>
//   - AC2.2: resume run argv includes "resume <thread_id>" prefix
//   - AC3.1: OPENAI_API_KEY and CODEX_API_KEY stripped from env (empty overrides present),
//     including when inherited from the real process env via os.Environ() (C3/T10, hk-jxgnp)
//   - AC3.4: CODEX_HOME set to a non-empty path in env
//   - Error cases: empty workspacePath, empty beadID, empty priorThreadID string

import (
	"os"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// TestBuildCodexLaunchSpec_InitialTurn
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildCodexLaunchSpec_InitialTurn verifies AC2.1: initial launch argv shape.
func TestBuildCodexLaunchSpec_InitialTurn(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath:    "/tmp/wt-test-codex-initial",
		BeadID:           "hk-test001",
		Model:            "o4-mini",
		BaseEnv:          []string{"PATH=/usr/bin"},
		SkipBillingGuard: true, // argv/env-shape test only; T11 guard covered separately
	}

	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Fatalf("TestBuildCodexLaunchSpec_InitialTurn: unexpected error: %v", err)
	}

	// Binary default.
	if spec.Binary != "codex" {
		t.Errorf("Binary = %q; want %q", spec.Binary, "codex")
	}
	// WorkDir set to worktreePath.
	if spec.WorkDir != rc.WorkspacePath {
		t.Errorf("WorkDir = %q; want %q", spec.WorkDir, rc.WorkspacePath)
	}

	// argv: codex exec --json --sandbox workspace-write --model <model> -C <wt> <seed>
	// Note: -a/--ask-for-approval was removed in codex 0.139.0.
	codexLaunchSpecAssertArgv(t, spec.Args, false, "")
	codexLaunchSpecAssertArgContains(t, spec.Args, "--json")
	codexLaunchSpecAssertArgContains(t, spec.Args, "--sandbox")
	codexLaunchSpecAssertArgContainsValue(t, spec.Args, "--sandbox", "workspace-write")
	codexLaunchSpecAssertArgContains(t, spec.Args, "--model")
	codexLaunchSpecAssertArgContainsValue(t, spec.Args, "--model", rc.Model)
	codexLaunchSpecAssertArgContains(t, spec.Args, "-C")
	codexLaunchSpecAssertArgContainsValue(t, spec.Args, "-C", rc.WorkspacePath)

	// Seed prompt present and references bead ID.
	codexLaunchSpecAssertSeedPrompt(t, spec.Args, rc.BeadID)
}

// TestBuildCodexLaunchSpec_CustomBinary verifies that a custom codexBinary is
// passed through as spec.Binary.
func TestBuildCodexLaunchSpec_CustomBinary(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedCodexRunCtx{
		CodexBinary:      "/usr/local/bin/codex",
		WorkspacePath:    "/tmp/wt-test-codex-bin",
		BeadID:           "hk-test002",
		Model:            "o4-mini",
		SkipBillingGuard: true, // argv/env-shape test only; T11 guard covered separately
	}

	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Fatalf("TestBuildCodexLaunchSpec_CustomBinary: unexpected error: %v", err)
	}

	if spec.Binary != rc.CodexBinary {
		t.Errorf("Binary = %q; want %q", spec.Binary, rc.CodexBinary)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestBuildCodexLaunchSpec_ResumeTurn
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildCodexLaunchSpec_ResumeTurn verifies AC2.2: resume turn includes
// "exec resume <thread_id>" in argv.
func TestBuildCodexLaunchSpec_ResumeTurn(t *testing.T) {
	t.Parallel()

	threadID := "thread-abc-123"
	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath:    "/tmp/wt-test-codex-resume",
		BeadID:           "hk-test003",
		PriorThreadID:    &threadID,
		BaseEnv:          []string{"PATH=/usr/bin"},
		SkipBillingGuard: true, // argv/env-shape test only; T11 guard covered separately
	}

	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Fatalf("TestBuildCodexLaunchSpec_ResumeTurn: unexpected error: %v", err)
	}

	// AC2.2: argv[0]="exec" argv[1]="resume" argv[2]=<thread_id>
	codexLaunchSpecAssertArgv(t, spec.Args, true, threadID)
	codexLaunchSpecAssertArgContains(t, spec.Args, "--json")

	// hk-mzgh: codex exec resume rejects -C (exit 2: "unexpected argument -C found").
	// The resume subcommand must NOT pass -C; WorkDir in the LaunchSpec sets CWD.
	for _, arg := range spec.Args {
		if arg == "-C" {
			t.Errorf("resume argv must not contain -C; codex exec resume rejects it: %v", spec.Args)
			break
		}
	}

	codexLaunchSpecAssertSeedPrompt(t, spec.Args, rc.BeadID)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestBuildCodexLaunchSpec_CredentialStrip (AC3.1)
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildCodexLaunchSpec_CredentialStrip verifies AC3.1: OPENAI_API_KEY and
// CODEX_API_KEY are stripped from the child env and re-emitted as empty overrides.
func TestBuildCodexLaunchSpec_CredentialStrip(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath: "/tmp/wt-test-codex-cred",
		BeadID:        "hk-test004",
		Model:         "o4-mini",
		BaseEnv: []string{
			"PATH=/usr/bin",
			"OPENAI_API_KEY=sk-test-must-not-leak",
			"CODEX_API_KEY=ck-test-must-not-leak",
		},
		SkipBillingGuard: true, // argv/env-shape test only; T11 guard covered separately
	}

	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Fatalf("TestBuildCodexLaunchSpec_CredentialStrip: unexpected error: %v", err)
	}

	// AC3.1: no live value for either key.
	denyKeys := []string{"OPENAI_API_KEY", "CODEX_API_KEY"}
	for _, kv := range spec.Env {
		for _, dk := range denyKeys {
			prefix := dk + "="
			if strings.HasPrefix(kv, prefix) && len(kv) > len(prefix) {
				t.Errorf("AC3.1: spec.Env carries live value for %q; must be empty override", dk)
			}
		}
	}

	// Empty overrides must be present.
	for _, dk := range denyKeys {
		want := dk + "="
		found := false
		for _, kv := range spec.Env {
			if kv == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AC3.1: spec.Env missing empty override %q", want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestBuildCodexLaunchSpec_CredentialKeysAbsentFromProcessEnv (C3/T10, hk-jxgnp)
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildCodexLaunchSpec_CredentialKeysAbsentFromProcessEnv is the T10
// regression lock mirroring the claude-side
// TestBuildClaudeLaunchSpec_CredentialKeysAbsentFromEnv (claudelaunchspec_test.go).
//
// It verifies the billing-leak guard end-to-end through the *real process
// environment*: OPENAI_API_KEY and CODEX_API_KEY are set on the parent process
// via t.Setenv, then os.Environ() (which now carries those live values) is
// passed straight through as BaseEnv — simulating a caller that forwards the
// daemon's inherited environment without pre-filtering. The resulting child env
// MUST carry an explicit empty override ("KEY=") for each, never a live value.
//
// The tmux server's -e mechanism is additive: merely omitting a key leaves the
// server env value intact; only an explicit KEY= zeros it in the spawned window.
// Without the strip, a live OPENAI_API_KEY / CODEX_API_KEY would let `codex exec`
// silently bill the API credit pool instead of the ChatGPT subscription.
//
// This is the codex analogue of the 2026-05-30 ANTHROPIC_API_KEY burn guard
// (hk-f2nm1); see codexCredentialDenyKeys / buildCodexEnv in codexlaunchspec.go.
//
// Spec: C3-auth-billing-spec.md AC3.1; specs/harness-contract.md §2 N1.
func TestBuildCodexLaunchSpec_CredentialKeysAbsentFromProcessEnv(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process env, which forbids parallel tests.

	// Set live credential values on the parent process. Test-only sentinels; no
	// real credentials are used. t.Setenv restores the prior values on cleanup.
	t.Setenv("OPENAI_API_KEY", "sk-t10-sentinel-must-not-reach-codex-child")
	t.Setenv("CODEX_API_KEY", "ck-t10-sentinel-must-not-reach-codex-child")

	// Sanity: confirm the sentinels really are in the process environment, so a
	// passing assertion below cannot be a false negative from an unset var.
	if os.Getenv("OPENAI_API_KEY") == "" || os.Getenv("CODEX_API_KEY") == "" {
		t.Fatalf("test setup: expected OPENAI_API_KEY and CODEX_API_KEY to be set in process env")
	}

	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath: "/tmp/wt-test-codex-procenv",
		BeadID:        "hk-test-t10",
		Model:         "o4-mini",
		// Forward the real process environment unfiltered, exactly as a caller
		// passing os.Environ() would. This is what makes the leak observable.
		BaseEnv:          os.Environ(),
		SkipBillingGuard: true, // argv/env-shape test only; T11 guard covered separately
	}

	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Fatalf("TestBuildCodexLaunchSpec_CredentialKeysAbsentFromProcessEnv: unexpected error: %v", err)
	}

	denyKeys := []string{"OPENAI_API_KEY", "CODEX_API_KEY"}

	// AC3.1: no live value for either credential key leaks into the child env.
	// Error messages redact the value (print only the key).
	for _, kv := range spec.Env {
		for _, dk := range denyKeys {
			prefix := dk + "="
			if strings.HasPrefix(kv, prefix) && len(kv) > len(prefix) {
				t.Errorf("AC3.1 (T10): child env carries live value for %q inherited from process env; must be empty override %q", dk, prefix)
			}
		}
	}

	// AC3.1: an explicit empty override ("KEY=") must be present for each key.
	// Without it, the tmux server env value would survive into the child window.
	for _, dk := range denyKeys {
		want := dk + "="
		found := false
		for _, kv := range spec.Env {
			if kv == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AC3.1 (T10): child env missing empty override %q; required to zero an inherited process-env credential", want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestBuildCodexLaunchSpec_PATH (hk-07jrb)
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildCodexLaunchSpec_PATH_FallsBackToProcessPATH_WhenBaseEnvHasNone
// verifies the hk-07jrb fix: when BaseEnv carries no PATH entry,
// buildCodexEnv falls back to the daemon process's own PATH rather than
// emitting an env slice with no PATH at all. SubstrateSpawn fully replaces
// the spawned pane's environment, so a missing PATH previously resolved
// against the libc default (/usr/bin:/bin) and died with exit 127 ("go:
// command not found") before the codex turn ever started.
func TestBuildCodexLaunchSpec_PATH_FallsBackToProcessPATH_WhenBaseEnvHasNone(t *testing.T) {
	// No t.Parallel: asserts against the live process PATH.
	procPath := os.Getenv("PATH")
	if procPath == "" {
		t.Skip("test process has no PATH set; cannot assert fallback value")
	}

	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath:    "/tmp/wt-test-codex-nopath",
		BeadID:           "hk-test-07jrb",
		Model:            "o4-mini",
		BaseEnv:          []string{"HOME=/home/op"}, // no PATH
		SkipBillingGuard: true,
	}

	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got string
	found := false
	for _, kv := range spec.Env {
		if strings.HasPrefix(kv, "PATH=") {
			got = strings.TrimPrefix(kv, "PATH=")
			found = true
		}
	}
	if !found {
		t.Fatal("PATH missing from codex child env; empty-PATH exit=127 hazard (hk-07jrb) reintroduced")
	}
	if got != procPath {
		t.Errorf("PATH = %q; want daemon process PATH %q", got, procPath)
	}
}

// TestBuildCodexLaunchSpec_PATH_PreservedWhenBaseEnvHasOne verifies that an
// existing PATH in BaseEnv is passed through unchanged rather than being
// overwritten by the process-PATH fallback.
func TestBuildCodexLaunchSpec_PATH_PreservedWhenBaseEnvHasOne(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath:    "/tmp/wt-test-codex-haspath",
		BeadID:           "hk-test-07jrb-2",
		Model:            "o4-mini",
		BaseEnv:          []string{"PATH=/custom/bin:/usr/bin"},
		SkipBillingGuard: true,
	}

	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got string
	for _, kv := range spec.Env {
		if strings.HasPrefix(kv, "PATH=") {
			got = strings.TrimPrefix(kv, "PATH=")
		}
	}
	if got != "/custom/bin:/usr/bin" {
		t.Errorf("PATH = %q; want BaseEnv's PATH preserved unchanged", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestBuildCodexLaunchSpec_CodexHomeSet (AC3.4)
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildCodexLaunchSpec_CodexHomeSet verifies AC3.4: CODEX_HOME is set to a
// non-empty value in the env, either the caller-supplied value or the default.
func TestBuildCodexLaunchSpec_CodexHomeSet(t *testing.T) {
	t.Parallel()

	// Case 1: explicit CodexHome.
	explicitHome := "/tmp/test-codex-home"
	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath:    "/tmp/wt-test-codex-home",
		BeadID:           "hk-test005",
		Model:            "o4-mini",
		CodexHome:        explicitHome,
		SkipBillingGuard: true, // argv/env-shape test only; T11 guard covered separately
	}

	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Fatalf("TestBuildCodexLaunchSpec_CodexHomeSet explicit: unexpected error: %v", err)
	}

	wantKV := "CODEX_HOME=" + explicitHome
	found := false
	for _, kv := range spec.Env {
		if kv == wantKV {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("AC3.4: explicit CODEX_HOME=%q not found in env; have %v", explicitHome, spec.Env)
	}

	// Case 2: default (empty CodexHome → non-empty path derived from $HOME).
	rc2 := daemon.ExportedCodexRunCtx{
		WorkspacePath:    "/tmp/wt-test-codex-home2",
		BeadID:           "hk-test005b",
		Model:            "o4-mini",
		SkipBillingGuard: true, // argv/env-shape test only; T11 guard covered separately
	}
	spec2, err := daemon.ExportedBuildCodexLaunchSpec(rc2)
	if err != nil {
		t.Fatalf("TestBuildCodexLaunchSpec_CodexHomeSet default: unexpected error: %v", err)
	}

	prefix := "CODEX_HOME="
	found2 := false
	for _, kv := range spec2.Env {
		if strings.HasPrefix(kv, prefix) && len(kv) > len(prefix) {
			found2 = true
			break
		}
	}
	if !found2 {
		t.Errorf("AC3.4: default CODEX_HOME not set to non-empty value; have %v", spec2.Env)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestBuildCodexLaunchSpec_ErrorCases
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildCodexLaunchSpec_EmptyWorkspacePath verifies a structural error when
// workspacePath is empty.
func TestBuildCodexLaunchSpec_EmptyWorkspacePath(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedCodexRunCtx{
		BeadID: "hk-test006",
	}
	_, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err == nil {
		t.Error("expected error for empty workspacePath; got nil")
	}
}

// TestBuildCodexLaunchSpec_EmptyBeadID verifies a structural error when beadID
// is empty.
func TestBuildCodexLaunchSpec_EmptyBeadID(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath: "/tmp/wt-test",
	}
	_, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err == nil {
		t.Error("expected error for empty beadID; got nil")
	}
}

// TestBuildCodexLaunchSpec_EmptyPriorThreadID verifies a structural error when
// priorThreadID is set to a pointer to an empty string (invalid resume).
func TestBuildCodexLaunchSpec_EmptyPriorThreadID(t *testing.T) {
	t.Parallel()

	empty := ""
	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath: "/tmp/wt-test",
		BeadID:        "hk-test007",
		PriorThreadID: &empty,
	}
	_, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err == nil {
		t.Error("expected error for empty priorThreadID string; got nil")
	}
}

// TestBuildCodexLaunchSpec_EmptyModelInitialTurn verifies that an empty model on an
// initial (non-resume) turn returns an error instead of hanging for ~30 min (hk-heh3t).
func TestBuildCodexLaunchSpec_EmptyModelInitialTurn(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath: "/tmp/wt-test-codex-nomodel",
		BeadID:        "hk-test-empty-model",
		// Model deliberately omitted: empty model on initial turn must fail loud.
		SkipBillingGuard: true,
	}
	_, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err == nil {
		t.Error("expected error for empty model on initial turn; got nil")
	}
}

// TestBuildCodexLaunchSpec_EmptyModelResumeTurnOK verifies that an empty model is
// allowed on a resume turn: the thread context already encodes the model.
func TestBuildCodexLaunchSpec_EmptyModelResumeTurnOK(t *testing.T) {
	t.Parallel()

	threadID := "th_resume_nomodel"
	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath: "/tmp/wt-test-codex-resume-nomodel",
		BeadID:        "hk-test-resume-nomodel",
		// Model omitted: resume turns do not require it.
		PriorThreadID:    &threadID,
		SkipBillingGuard: true,
	}
	_, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Errorf("empty model on resume turn must not error; got: %v", err)
	}
}

// TestBuildCodexLaunchSpec_ModelInArgv verifies that the model is passed as
// --model <model> in the initial-turn argv (hk-heh3t).
func TestBuildCodexLaunchSpec_ModelInArgv(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath:    "/tmp/wt-test-codex-model-argv",
		BeadID:           "hk-test-model-argv",
		Model:            "o4-mini",
		SkipBillingGuard: true,
	}
	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	codexLaunchSpecAssertArgContains(t, spec.Args, "--model")
	codexLaunchSpecAssertArgContainsValue(t, spec.Args, "--model", "o4-mini")
}

// TestBuildCodexLaunchSpec_ModelNotInResumeArgv verifies that --model does NOT
// appear in resume-turn argv (resume thread context already encodes the model).
func TestBuildCodexLaunchSpec_ModelNotInResumeArgv(t *testing.T) {
	t.Parallel()

	threadID := "th_resume_model_check"
	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath:    "/tmp/wt-test-codex-resume-model",
		BeadID:           "hk-test-resume-model",
		Model:            "o4-mini",
		PriorThreadID:    &threadID,
		SkipBillingGuard: true,
	}
	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, arg := range spec.Args {
		if arg == "--model" {
			t.Errorf("resume argv must not contain --model; got %v", spec.Args)
			return
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// assertion helpers
// ─────────────────────────────────────────────────────────────────────────────

// codexLaunchSpecAssertArgv verifies the high-level argv shape.
// wantResume=true checks for "exec resume <threadID>" prefix.
// wantResume=false checks for "exec" (no "resume") as first token.
func codexLaunchSpecAssertArgv(t *testing.T, args []string, wantResume bool, threadID string) {
	t.Helper()
	if len(args) == 0 {
		t.Error("args is empty; expected at least 'exec'")
		return
	}
	if args[0] != "exec" {
		t.Errorf("args[0] = %q; want %q (AC2.1/2.2)", args[0], "exec")
	}
	if wantResume {
		if len(args) < 3 {
			t.Errorf("resume turn requires at least 3 args (exec resume <id>); got %d: %v", len(args), args)
			return
		}
		if args[1] != "resume" {
			t.Errorf("AC2.2: args[1] = %q; want %q for resume turn", args[1], "resume")
		}
		if args[2] != threadID {
			t.Errorf("AC2.2: args[2] = %q; want thread_id %q", args[2], threadID)
		}
		// Must not contain an additional "resume" token past the expected position.
		for i, a := range args[3:] {
			if a == "resume" {
				t.Errorf("unexpected extra 'resume' token at args[%d]", i+3)
			}
		}
	} else {
		if len(args) > 1 && args[1] == "resume" {
			t.Errorf("AC2.1: initial turn must not have 'resume' in argv; got %v", args)
		}
	}
}

// codexLaunchSpecAssertArgContains verifies that flag is present in args.
func codexLaunchSpecAssertArgContains(t *testing.T, args []string, flag string) {
	t.Helper()
	for _, a := range args {
		if a == flag {
			return
		}
	}
	t.Errorf("flag %q not found in args %v", flag, args)
}

// codexLaunchSpecAssertArgContainsValue verifies that flag is immediately followed
// by value in args.
func codexLaunchSpecAssertArgContainsValue(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i, a := range args {
		if a == flag {
			if i+1 >= len(args) {
				t.Errorf("flag %q found at args[%d] but no following value; args=%v", flag, i, args)
				return
			}
			if args[i+1] != value {
				t.Errorf("flag %q value = %q; want %q", flag, args[i+1], value)
			}
			return
		}
	}
	t.Errorf("flag %q not found in args %v (expected value %q)", flag, args, value)
}

// codexLaunchSpecAssertSeedPrompt verifies that the last arg is the seed prompt
// and that it contains the beadID (for the Refs: instruction).
func codexLaunchSpecAssertSeedPrompt(t *testing.T, args []string, beadID string) {
	t.Helper()
	if len(args) == 0 {
		t.Error("codexLaunchSpecAssertSeedPrompt: args is empty")
		return
	}
	last := args[len(args)-1]
	if !strings.Contains(last, beadID) {
		t.Errorf("seed prompt (last arg) does not reference beadID %q; got %q", beadID, last)
	}
	if !strings.Contains(strings.ToLower(last), "refs") {
		t.Errorf("seed prompt does not contain 'Refs' instruction; got %q", last)
	}
}
