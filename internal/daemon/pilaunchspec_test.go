package daemon_test

// pilaunchspec_test.go — unit tests for buildPiLaunchSpec (hk-1c16h PI-015/020/021).
//
// Key invariants tested:
//
//   - PI-020: initial turn argv = pi --mode json --provider <p> --model <m> <seed>
//   - PI-020: resume turn argv = pi --mode json --session <id> <seed>
//   - PI-015: no --sandbox flag in any argv
//   - PI-015: seed prompt contains bead ID and "Refs" instruction
//   - PI-020: WorkDir set to workspacePath; StdinDevNull = true
//   - PI-021: buildPiEnv strips ALL *_API_KEY vars (except selected) + maintained table
//   - PI-021: buildPiEnv injects ONLY the selected provider's key
//   - PI-021: allowlist strip works with process env forwarded as baseEnv (T10 analog)
//   - PI-021: resolvePiAPIKeyValue reads the correct operator env var
//   - Error cases: empty workspacePath, beadID, apiKeyEnv, emptySessionID

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// TestBuildPiLaunchSpec_InitialTurn (PI-020)
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildPiLaunchSpec_InitialTurn verifies PI-020: initial launch argv shape.
func TestBuildPiLaunchSpec_InitialTurn(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedPiRunCtx{
		WorkspacePath:    "/tmp/wt-test-pi-initial",
		BeadID:           "hk-test001",
		Provider:         "openrouter",
		Model:            "openrouter/qwen/qwen3-coder",
		APIKeyEnv:        "OPENROUTER_API_KEY",
		BaseEnv:          []string{"PATH=/usr/bin"},
		SkipBillingGuard: true,
	}

	spec, err := daemon.ExportedBuildPiLaunchSpec(rc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Binary default.
	if spec.Binary != "pi" {
		t.Errorf("Binary = %q; want %q", spec.Binary, "pi")
	}
	// WorkDir set to workspacePath.
	if spec.WorkDir != rc.WorkspacePath {
		t.Errorf("WorkDir = %q; want %q", spec.WorkDir, rc.WorkspacePath)
	}
	// StdinDevNull = true (PI-020 / #4303).
	if !spec.StdinDevNull {
		t.Error("StdinDevNull must be true (PI-020)")
	}

	// PI-020: argv shape — --mode json --provider <p> --model <m> <seed>
	piLaunchSpecAssertFlag(t, spec.Args, "--mode")
	piLaunchSpecAssertFlagValue(t, spec.Args, "--mode", "json")
	piLaunchSpecAssertFlag(t, spec.Args, "--provider")
	piLaunchSpecAssertFlagValue(t, spec.Args, "--provider", rc.Provider)
	piLaunchSpecAssertFlag(t, spec.Args, "--model")
	piLaunchSpecAssertFlagValue(t, spec.Args, "--model", rc.Model)

	// PI-020: no --session on initial turn.
	for _, arg := range spec.Args {
		if arg == "--session" {
			t.Error("initial turn must not contain --session in argv")
		}
	}

	// PI-015: no --sandbox flag.
	for _, arg := range spec.Args {
		if arg == "--sandbox" {
			t.Error("PI-015: --sandbox must not be present (Pi is unsandboxed)")
		}
	}

	// PI-015: seed prompt contains bead ID.
	piLaunchSpecAssertSeedPrompt(t, spec.Args, rc.BeadID)
}

// TestBuildPiLaunchSpec_CustomBinary verifies that a custom piBinary is used.
func TestBuildPiLaunchSpec_CustomBinary(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedPiRunCtx{
		PiBinary:         "/usr/local/bin/pi",
		WorkspacePath:    "/tmp/wt-test-pi-bin",
		BeadID:           "hk-test002",
		Provider:         "openrouter",
		Model:            "openrouter/qwen/qwen3-coder",
		APIKeyEnv:        "OPENROUTER_API_KEY",
		SkipBillingGuard: true,
	}

	spec, err := daemon.ExportedBuildPiLaunchSpec(rc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spec.Binary != rc.PiBinary {
		t.Errorf("Binary = %q; want %q", spec.Binary, rc.PiBinary)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestBuildPiLaunchSpec_ResumeTurn (PI-020)
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildPiLaunchSpec_ResumeTurn verifies PI-020: resume turn uses
// --session <id> and does not include --provider / --model.
func TestBuildPiLaunchSpec_ResumeTurn(t *testing.T) {
	t.Parallel()

	sessionID := "pi-session-abc-123"
	rc := daemon.ExportedPiRunCtx{
		WorkspacePath:    "/tmp/wt-test-pi-resume",
		BeadID:           "hk-test003",
		Provider:         "openrouter",
		Model:            "openrouter/qwen/qwen3-coder",
		APIKeyEnv:        "OPENROUTER_API_KEY",
		PriorSessionID:   &sessionID,
		BaseEnv:          []string{"PATH=/usr/bin"},
		SkipBillingGuard: true,
	}

	spec, err := daemon.ExportedBuildPiLaunchSpec(rc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// PI-020: argv shape — --mode json --session <id> <seed>
	piLaunchSpecAssertFlag(t, spec.Args, "--mode")
	piLaunchSpecAssertFlagValue(t, spec.Args, "--mode", "json")
	piLaunchSpecAssertFlag(t, spec.Args, "--session")
	piLaunchSpecAssertFlagValue(t, spec.Args, "--session", sessionID)

	// Resume must not include --provider or --model (session carries state).
	for _, arg := range spec.Args {
		if arg == "--provider" {
			t.Error("resume turn must not contain --provider in argv")
		}
		if arg == "--model" {
			t.Error("resume turn must not contain --model in argv")
		}
	}

	// PI-015: no --sandbox.
	for _, arg := range spec.Args {
		if arg == "--sandbox" {
			t.Error("PI-015: --sandbox must not be present (Pi is unsandboxed)")
		}
	}

	// Seed prompt still references bead ID.
	piLaunchSpecAssertSeedPrompt(t, spec.Args, rc.BeadID)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestBuildPiEnv_AllowlistStrip (PI-021)
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildPiEnv_AllowlistStrip verifies PI-021: maintained-table keys and
// *_API_KEY keys are stripped (emitted as KEY=); only the selected key is
// injected with its value.
func TestBuildPiEnv_AllowlistStrip(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process env, which forbids parallel tests.
	selectedKey := "OPENROUTER_API_KEY"
	selectedVal := "or-key-sentinel"
	t.Setenv(selectedKey, selectedVal)

	baseEnv := []string{
		"PATH=/usr/bin",
		"OPENROUTER_API_KEY=" + selectedVal,
		"ANTHROPIC_API_KEY=ant-must-be-stripped",
		"OPENAI_API_KEY=oai-must-be-stripped",
		"MISTRAL_API_KEY=mis-must-be-stripped",      // *_API_KEY pattern
		"HYPOTHETICAL_API_KEY=hyp-must-be-stripped", // unknown provider, *_API_KEY catch-all
		"HOME=/root",
	}

	env := daemon.ExportedBuildPiEnv(baseEnv, "", selectedKey)

	// Non-credential entries must pass through.
	assertEnvContains(t, env, "PATH=/usr/bin")
	assertEnvContains(t, env, "HOME=/root")

	// PI-021: the selected key must be injected with its value.
	assertEnvContains(t, env, selectedKey+"="+selectedVal)

	// PI-021: all other *_API_KEY / maintained-table keys must be empty-overridden.
	strippedKeys := []string{
		"ANTHROPIC_API_KEY",
		"OPENAI_API_KEY",
		"MISTRAL_API_KEY",
		"HYPOTHETICAL_API_KEY",
	}
	for _, k := range strippedKeys {
		// No live value must leak.
		for _, kv := range env {
			prefix := k + "="
			if strings.HasPrefix(kv, prefix) && len(kv) > len(prefix) {
				t.Errorf("PI-021: env carries live value for %q; must be empty override", k)
			}
		}
		// Empty override must be present.
		assertEnvContains(t, env, k+"=")
	}
}

// TestBuildPiEnv_MaintainedTableStrippedEvenIfAbsentFromBaseEnv verifies that
// the maintained piProviderCredentialKeys table entries are empty-overridden even
// when they are NOT in baseEnv (guarding against the tmux additive -e mechanism).
func TestBuildPiEnv_MaintainedTableStrippedEvenIfAbsentFromBaseEnv(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process env, which forbids parallel tests.
	selectedKey := "OPENROUTER_API_KEY"
	t.Setenv(selectedKey, "or-key-val")

	// baseEnv contains ONLY PATH; none of the maintained-table keys are present.
	baseEnv := []string{"PATH=/usr/bin"}

	env := daemon.ExportedBuildPiEnv(baseEnv, "", selectedKey)

	// ANTHROPIC_API_KEY is in the maintained table but not in baseEnv.
	// It must still be emitted as an empty override (tmux additive -e guard).
	assertEnvContains(t, env, "ANTHROPIC_API_KEY=")

	// No live value must appear for ANTHROPIC_API_KEY.
	for _, kv := range env {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") && len(kv) > len("ANTHROPIC_API_KEY=") {
			t.Errorf("PI-021: env carries live value for ANTHROPIC_API_KEY not in baseEnv; must be empty override")
		}
	}
}

// TestBuildPiEnv_GuaranteesPathWhenBaseEnvHasNone verifies the hk-6atjk /
// codename:pi-model-leak fix: when baseEnv carries NO PATH (the daemon's
// unpopulated HandlerEnv case), buildPiEnv appends a non-empty PATH from the
// daemon process env so the pi CLI's `env node` shebang can resolve node.
func TestBuildPiEnv_GuaranteesPathWhenBaseEnvHasNone(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process env, which forbids parallel tests.
	selectedKey := "OPENROUTER_API_KEY"
	t.Setenv(selectedKey, "or-key-val")
	t.Setenv("PATH", "/opt/homebrew/bin:/usr/bin:/bin")

	// baseEnv has NO PATH entry — the leak scenario.
	baseEnv := []string{"HK_PROVENANCE=daemon"}

	env := daemon.ExportedBuildPiEnv(baseEnv, "", selectedKey)

	// A PATH entry must be present and non-empty (falls back to process PATH).
	var pathVal string
	found := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			found = true
			pathVal = kv[len("PATH="):]
		}
	}
	if !found {
		t.Fatalf("hk-6atjk: expected buildPiEnv to append a PATH when baseEnv has none; env=%v", env)
	}
	if pathVal == "" {
		t.Errorf("hk-6atjk: PATH must be non-empty; got empty override")
	}
	if pathVal != "/opt/homebrew/bin:/usr/bin:/bin" {
		t.Errorf("hk-6atjk: expected process PATH fallback, got %q", pathVal)
	}
}

// TestBuildPiEnv_PreservesBaseEnvPath verifies the fix does NOT override an
// existing PATH: when baseEnv already carries a PATH, that exact value is
// preserved (not replaced by the daemon process PATH).
func TestBuildPiEnv_PreservesBaseEnvPath(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process env, which forbids parallel tests.
	selectedKey := "OPENROUTER_API_KEY"
	t.Setenv(selectedKey, "or-key-val")
	// Process PATH differs from baseEnv PATH to prove baseEnv wins.
	t.Setenv("PATH", "/opt/homebrew/bin:/should/not/appear")

	baseEnv := []string{"PATH=/usr/bin"}

	env := daemon.ExportedBuildPiEnv(baseEnv, "", selectedKey)

	// The baseEnv PATH must be preserved verbatim, and appear exactly once.
	assertEnvContains(t, env, "PATH=/usr/bin")
	count := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			count++
			if kv != "PATH=/usr/bin" {
				t.Errorf("hk-6atjk: baseEnv PATH must not be overridden; got %q", kv)
			}
		}
	}
	if count != 1 {
		t.Errorf("hk-6atjk: expected exactly one PATH entry, got %d", count)
	}
}

// TestBuildPiEnv_AllowlistStrip_ProcessEnvForwarded is the T10-analog regression
// lock for Pi: the operator's process environment is forwarded unfiltered as
// baseEnv (simulating a daemon that passes os.Environ() directly), and the
// allowlist-strip must prevent live credential values from reaching Pi.
func TestBuildPiEnv_AllowlistStrip_ProcessEnvForwarded(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process env, which forbids parallel tests.

	selectedKey := "OPENROUTER_API_KEY"
	t.Setenv(selectedKey, "or-live-sentinel")
	t.Setenv("ANTHROPIC_API_KEY", "ant-T10-sentinel-must-not-reach-pi")
	t.Setenv("GEMINI_API_KEY", "gem-T10-sentinel-must-not-reach-pi")
	t.Setenv("MYSTERY_API_KEY", "mys-T10-sentinel-must-not-reach-pi")

	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Fatal("test setup: expected ANTHROPIC_API_KEY to be set")
	}

	env := daemon.ExportedBuildPiEnv(os.Environ(), "", selectedKey)

	// No live value must leak for non-selected credential keys.
	strippedKeys := []string{"ANTHROPIC_API_KEY", "GEMINI_API_KEY", "MYSTERY_API_KEY"}
	for _, k := range strippedKeys {
		prefix := k + "="
		for _, kv := range env {
			if strings.HasPrefix(kv, prefix) && len(kv) > len(prefix) {
				t.Errorf("PI-021 T10: child env carries live value for %q from process env; must be empty override", k)
			}
		}
		assertEnvContains(t, env, k+"=")
	}

	// The selected key must be present with its value.
	assertEnvContains(t, env, selectedKey+"=or-live-sentinel")
}

// ─────────────────────────────────────────────────────────────────────────────
// TestBuildPiEnv_InjectsOnlySelectedKey (PI-021)
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildPiEnv_InjectsOnlySelectedKey verifies that ONLY the selected
// provider's key is injected (as KEY=<value>), and no other *_API_KEY carries
// a live value.
func TestBuildPiEnv_InjectsOnlySelectedKey(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process env, which forbids parallel tests.
	selectedKey := "OPENAI_API_KEY"
	selectedVal := "oai-selected-value"
	t.Setenv(selectedKey, selectedVal)

	baseEnv := []string{
		"OPENAI_API_KEY=" + selectedVal,
		"ANTHROPIC_API_KEY=ant-should-be-stripped",
	}

	env := daemon.ExportedBuildPiEnv(baseEnv, "", selectedKey)

	// Selected key must have its value.
	assertEnvContains(t, env, selectedKey+"="+selectedVal)

	// ANTHROPIC_API_KEY must be an empty override.
	assertEnvContains(t, env, "ANTHROPIC_API_KEY=")
	for _, kv := range env {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") && len(kv) > len("ANTHROPIC_API_KEY=") {
			t.Error("ANTHROPIC_API_KEY must be empty-overridden, not carry live value")
		}
	}

	// No other *_API_KEY must carry a non-empty value.
	for _, kv := range env {
		eqIdx := strings.IndexByte(kv, '=')
		if eqIdx < 0 {
			continue
		}
		k := kv[:eqIdx]
		v := kv[eqIdx+1:]
		if k == selectedKey {
			continue
		}
		if strings.HasSuffix(k, "_API_KEY") && v != "" {
			t.Errorf("PI-021: env carries live value for non-selected credential key %q: %q", k, v)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestResolvePiAPIKeyValue (PI-021 shared helper)
// ─────────────────────────────────────────────────────────────────────────────

// TestResolvePiAPIKeyValue verifies the shared key-resolution helper reads the
// correct env var from the operator environment.
func TestResolvePiAPIKeyValue(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process env.
	const testKey = "OPENROUTER_API_KEY"
	const testVal = "or-resolver-sentinel"
	t.Setenv(testKey, testVal)

	got := daemon.ExportedResolvePiAPIKeyValue("", testKey)
	if got != testVal {
		t.Errorf("resolvePiAPIKeyValue(%q) = %q; want %q", testKey, got, testVal)
	}
}

// TestResolvePiAPIKeyValue_AbsentKey verifies that an unset key returns empty string.
func TestResolvePiAPIKeyValue_AbsentKey(t *testing.T) {
	t.Parallel()
	// Use a key that should never be set in the test environment.
	got := daemon.ExportedResolvePiAPIKeyValue("", "HARMONIK_TEST_NEVER_SET_XYZ_API_KEY")
	if got != "" {
		t.Errorf("absent key: expected empty string; got %q", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestBuildPiLaunchSpec_ErrorCases
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildPiLaunchSpec_EmptyWorkspacePath(t *testing.T) {
	t.Parallel()
	rc := daemon.ExportedPiRunCtx{
		BeadID:    "hk-err01",
		Provider:  "openrouter",
		Model:     "openrouter/qwen/qwen3-coder",
		APIKeyEnv: "OPENROUTER_API_KEY",
	}
	_, err := daemon.ExportedBuildPiLaunchSpec(rc)
	if err == nil {
		t.Error("expected error for empty workspacePath; got nil")
	}
}

func TestBuildPiLaunchSpec_EmptyBeadID(t *testing.T) {
	t.Parallel()
	rc := daemon.ExportedPiRunCtx{
		WorkspacePath: "/tmp/wt-test",
		Provider:      "openrouter",
		Model:         "openrouter/qwen/qwen3-coder",
		APIKeyEnv:     "OPENROUTER_API_KEY",
	}
	_, err := daemon.ExportedBuildPiLaunchSpec(rc)
	if err == nil {
		t.Error("expected error for empty beadID; got nil")
	}
}

func TestBuildPiLaunchSpec_EmptyAPIKeyEnv(t *testing.T) {
	t.Parallel()
	rc := daemon.ExportedPiRunCtx{
		WorkspacePath: "/tmp/wt-test",
		BeadID:        "hk-err02",
		Provider:      "openrouter",
		Model:         "openrouter/qwen/qwen3-coder",
		APIKeyEnv:     "", // empty
	}
	_, err := daemon.ExportedBuildPiLaunchSpec(rc)
	if err == nil {
		t.Error("expected error for empty apiKeyEnv; got nil")
	}
}

func TestBuildPiLaunchSpec_EmptyPriorSessionID(t *testing.T) {
	t.Parallel()
	empty := ""
	rc := daemon.ExportedPiRunCtx{
		WorkspacePath:  "/tmp/wt-test",
		BeadID:         "hk-err03",
		APIKeyEnv:      "OPENROUTER_API_KEY",
		PriorSessionID: &empty, // pointer to empty string: invalid resume
	}
	_, err := daemon.ExportedBuildPiLaunchSpec(rc)
	if err == nil {
		t.Error("expected error for empty priorSessionID string; got nil")
	}
}

func TestBuildPiLaunchSpec_InitialTurn_EmptyProvider(t *testing.T) {
	t.Parallel()
	rc := daemon.ExportedPiRunCtx{
		WorkspacePath:    "/tmp/wt-test",
		BeadID:           "hk-err04",
		Provider:         "", // missing on initial turn
		Model:            "openrouter/qwen/qwen3-coder",
		APIKeyEnv:        "OPENROUTER_API_KEY",
		SkipBillingGuard: true,
	}
	_, err := daemon.ExportedBuildPiLaunchSpec(rc)
	if err == nil {
		t.Error("expected error for empty provider on initial turn; got nil")
	}
}

func TestBuildPiLaunchSpec_InitialTurn_EmptyModel(t *testing.T) {
	t.Parallel()
	rc := daemon.ExportedPiRunCtx{
		WorkspacePath:    "/tmp/wt-test",
		BeadID:           "hk-err05",
		Provider:         "openrouter",
		Model:            "", // missing on initial turn
		APIKeyEnv:        "OPENROUTER_API_KEY",
		SkipBillingGuard: true,
	}
	_, err := daemon.ExportedBuildPiLaunchSpec(rc)
	if err == nil {
		t.Error("expected error for empty model on initial turn; got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// assertion helpers
// ─────────────────────────────────────────────────────────────────────────────

// piLaunchSpecAssertFlag verifies that flag is present in args.
func piLaunchSpecAssertFlag(t *testing.T, args []string, flag string) {
	t.Helper()
	for _, a := range args {
		if a == flag {
			return
		}
	}
	t.Errorf("flag %q not found in args %v", flag, args)
}

// piLaunchSpecAssertFlagValue verifies that flag is immediately followed by
// value in args.
func piLaunchSpecAssertFlagValue(t *testing.T, args []string, flag, value string) {
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

// piLaunchSpecAssertSeedPrompt verifies that the last arg is the seed prompt
// and that it contains the beadID (for the Refs: instruction).
func piLaunchSpecAssertSeedPrompt(t *testing.T, args []string, beadID string) {
	t.Helper()
	if len(args) == 0 {
		t.Error("piLaunchSpecAssertSeedPrompt: args is empty")
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

// assertEnvContains verifies that want is present in env.
func assertEnvContains(t *testing.T, env []string, want string) {
	t.Helper()
	for _, kv := range env {
		if kv == want {
			return
		}
	}
	t.Errorf("env missing %q; have %v", want, env)
}

// ─────────────────────────────────────────────────────────────────────────────
// api_key_file tests (PI-050, hk-xmfoi)
// ─────────────────────────────────────────────────────────────────────────────

// TestResolvePiAPIKeyValue_FromFile_UsesFileValueOverEnv verifies that when
// apiKeyFile is set and readable, resolvePiAPIKeyValue returns the file value
// and ignores the ambient env — file-first precedence (PI-050).
func TestResolvePiAPIKeyValue_FromFile_UsesFileValueOverEnv(t *testing.T) {
	// Not parallel: t.Setenv mutates process env.
	const envKey = "TEST_PI_FILE_PREFERS_FILE_OVER_ENV"
	t.Setenv(envKey, "env-value-must-not-be-used")

	dir := t.TempDir()
	keyFile := dir + "/openrouter.key"
	if err := os.WriteFile(keyFile, []byte("file-value-wins\n"), 0o600); err != nil {
		t.Fatalf("setup: write key file: %v", err)
	}

	got := daemon.ExportedResolvePiAPIKeyValue(keyFile, envKey)
	if got != "file-value-wins" {
		t.Errorf("resolvePiAPIKeyValue(file, env) = %q; want %q (file-first precedence)", got, "file-value-wins")
	}
}

// TestResolvePiAPIKeyValue_FileAbsent_FallsBackToEnv verifies that when
// apiKeyFile is set but the file does not exist, the helper falls back to the
// ambient env value.
func TestResolvePiAPIKeyValue_FileAbsent_FallsBackToEnv(t *testing.T) {
	// Not parallel: t.Setenv mutates process env.
	const envKey = "TEST_PI_FILE_FALLBACK_TO_ENV"
	t.Setenv(envKey, "env-fallback-value")

	got := daemon.ExportedResolvePiAPIKeyValue("/nonexistent-harmonik-test-xmfoi/key", envKey)
	if got != "env-fallback-value" {
		t.Errorf("resolvePiAPIKeyValue(missing file, env) = %q; want env fallback %q", got, "env-fallback-value")
	}
}

// TestBuildPiEnv_APIKeyFile_InjectsFileValueOverEnv verifies that buildPiEnv
// injects the key value from the file (not the ambient env) when apiKeyFile is
// set — child env carries the file value, daemon env stays clean (PI-050).
func TestBuildPiEnv_APIKeyFile_InjectsFileValueOverEnv(t *testing.T) {
	// Not parallel: t.Setenv mutates process env.
	const selectedKey = "OPENROUTER_API_KEY"
	t.Setenv(selectedKey, "env-value-must-not-be-used")

	dir := t.TempDir()
	keyFile := dir + "/openrouter.key"
	if err := os.WriteFile(keyFile, []byte("file-key-sentinel"), 0o600); err != nil {
		t.Fatalf("setup: write key file: %v", err)
	}

	baseEnv := []string{"PATH=/usr/bin"}
	env := daemon.ExportedBuildPiEnv(baseEnv, keyFile, selectedKey)

	// PI-050: the file value must appear in the child env.
	assertEnvContains(t, env, selectedKey+"=file-key-sentinel")

	// The env-value must NOT appear (file takes precedence).
	for _, kv := range env {
		if kv == selectedKey+"=env-value-must-not-be-used" {
			t.Error("PI-050: child env carries env value instead of file value; file-first precedence violated")
		}
	}
}

// TestBuildPiEnv_APIKeyFile_Unset_UsesEnvValue verifies that when apiKeyFile is
// empty, buildPiEnv falls back to the ambient env value (existing behavior).
func TestBuildPiEnv_APIKeyFile_Unset_UsesEnvValue(t *testing.T) {
	// Not parallel: t.Setenv mutates process env.
	const selectedKey = "OPENROUTER_API_KEY"
	const envVal = "sk-or-env-only-sentinel"
	t.Setenv(selectedKey, envVal)

	baseEnv := []string{"PATH=/usr/bin", selectedKey + "=" + envVal}
	env := daemon.ExportedBuildPiEnv(baseEnv, "", selectedKey)

	// When apiKeyFile is unset, the ambient env value is injected.
	assertEnvContains(t, env, selectedKey+"="+envVal)
}

// TestBuildPiLaunchSpec_APIKeyFile_DaemonEnvClean verifies end-to-end: when
// apiKeyFile is set, buildPiLaunchSpec injects the file value into the child env
// keyed by apiKeyEnv, and the daemon ambient env (which would be present via
// baseEnv) is NOT used. The daemon must not carry the secret itself (PI-050).
func TestBuildPiLaunchSpec_APIKeyFile_DaemonEnvClean(t *testing.T) {
	// Not parallel: t.Setenv mutates process env.
	const envKey = "OPENROUTER_API_KEY"
	t.Setenv(envKey, "daemon-env-value-must-not-appear")

	dir := t.TempDir()
	keyFile := dir + "/openrouter.key"
	if err := os.WriteFile(keyFile, []byte("file-key-child-only"), 0o600); err != nil {
		t.Fatalf("setup: write key file: %v", err)
	}

	rc := daemon.ExportedPiRunCtx{
		WorkspacePath:    "/tmp/wt-test-pi-apikeyfile",
		BeadID:           "hk-xmfoi-test",
		Provider:         "openrouter",
		Model:            "openrouter/minimax/minimax-m3",
		APIKeyEnv:        envKey,
		APIKeyFile:       keyFile,
		BaseEnv:          []string{"PATH=/usr/bin"},
		SkipBillingGuard: true,
	}

	spec, err := daemon.ExportedBuildPiLaunchSpec(rc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Child env must carry the file value (not the daemon env value).
	assertEnvContains(t, spec.Env, envKey+"=file-key-child-only")

	// The daemon env value must not appear in the child env.
	for _, kv := range spec.Env {
		if kv == envKey+"=daemon-env-value-must-not-appear" {
			t.Error("PI-050: child env carries daemon-env value; file-first precedence violated")
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// base_url passthrough tests — production call chain (hk-z13jz)
// ─────────────────────────────────────────────────────────────────────────────

// TestPiHarness_BaseURL_ProductionPath_Present verifies the production call chain
// (NewPiHarness → LaunchSpec) writes models.json and injects PI_CODING_AGENT_DIR
// when baseURL is set and this is the initial turn. Must NOT use
// ExportedBuildPiLaunchSpec — exercises the full production path.
func TestPiHarness_BaseURL_ProductionPath_Present(t *testing.T) {
	// Not parallel: t.Setenv mutates process env.
	const apiKeyEnv = "TEST_PI_BASE_URL_PROD_KEY"
	const apiKeyVal = "sk-or-testkey-sentinel"
	t.Setenv(apiKeyEnv, apiKeyVal)

	workDir := t.TempDir()

	harness := daemon.NewPiHarness(
		"pi",                       // piBinary
		"mylocal",                  // provider
		"mylocal/ornith",           // model ("ornith" after last "/")
		apiKeyEnv,                  // apiKeyEnv
		"",                         // apiKeyFile
		"http://dgx.local:8551/v1", // baseURL
		"",                         // api: empty → defaults to "openai"
	)

	rc := daemon.ExportedRunCtxForPi(workDir, "hk-z13jz-prod")
	spec, err := harness.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("LaunchSpec: unexpected error: %v", err)
	}

	// PI_CODING_AGENT_DIR must be in the child env.
	piAgentDir := findEnvValue(t, spec.Env, "PI_CODING_AGENT_DIR")
	if piAgentDir == "" {
		t.Fatal("PI_CODING_AGENT_DIR not injected into child env; want non-empty")
	}

	// models.json must exist under the pi-agent dir.
	modelsPath := piAgentDir + "/models.json"
	modelsBytes, readErr := os.ReadFile(modelsPath)
	if readErr != nil {
		t.Fatalf("models.json not found at %q: %v", modelsPath, readErr)
	}

	// Deserialize and verify content.
	var parsed struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			API     string `json:"api"`
			APIKey  string `json:"apiKey"`
			Models  []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(modelsBytes, &parsed); err != nil {
		t.Fatalf("models.json JSON parse failed: %v\ncontent: %s", err, modelsBytes)
	}

	prov, ok := parsed.Providers["mylocal"]
	if !ok {
		t.Fatalf("models.json has no 'mylocal' provider; got providers: %v", parsed.Providers)
	}
	if prov.BaseURL != "http://dgx.local:8551/v1" {
		t.Errorf("baseUrl = %q; want %q", prov.BaseURL, "http://dgx.local:8551/v1")
	}
	if prov.API != "openai" {
		t.Errorf("api = %q; want %q (default when empty)", prov.API, "openai")
	}
	if prov.APIKey != apiKeyVal {
		t.Errorf("apiKey = %q; want %q", prov.APIKey, apiKeyVal)
	}
	if len(prov.Models) != 1 || prov.Models[0].ID != "ornith" {
		t.Errorf("models = %v; want [{id:ornith}]", prov.Models)
	}
}

// TestPiHarness_BaseURL_ProductionPath_Absent verifies the production call chain
// emits NO PI_CODING_AGENT_DIR and writes NO models.json when baseURL is absent.
// Today's cloud-provider behavior must be byte-for-byte unchanged.
func TestPiHarness_BaseURL_ProductionPath_Absent(t *testing.T) {
	// Not parallel: t.Setenv mutates process env.
	const apiKeyEnv = "TEST_PI_BASE_URL_ABSENT_KEY"
	t.Setenv(apiKeyEnv, "sk-or-absent-test")

	workDir := t.TempDir()

	harness := daemon.NewPiHarness(
		"pi",                          // piBinary
		"openrouter",                  // provider
		"openrouter/qwen/qwen3-coder", // model
		apiKeyEnv,                     // apiKeyEnv
		"",                            // apiKeyFile
		"",                            // baseURL: absent
		"",                            // api: absent
	)

	rc := daemon.ExportedRunCtxForPi(workDir, "hk-z13jz-absent")
	spec, err := harness.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("LaunchSpec: unexpected error: %v", err)
	}

	// PI_CODING_AGENT_DIR must NOT be present.
	for _, kv := range spec.Env {
		if strings.HasPrefix(kv, "PI_CODING_AGENT_DIR=") {
			t.Errorf("PI_CODING_AGENT_DIR injected when baseURL is absent: %q", kv)
		}
	}

	// models.json must NOT have been created.
	piAgentDir := workDir + "/.harmonik/pi-agent"
	if _, statErr := os.Stat(piAgentDir + "/models.json"); statErr == nil {
		t.Errorf("models.json written when baseURL is absent; pi-agent dir should not exist")
	}
}

// TestPiHarness_BaseURL_APIOverride verifies the api field flows into models.json
// correctly when explicitly set.
func TestPiHarness_BaseURL_APIOverride(t *testing.T) {
	// Not parallel: t.Setenv mutates process env.
	const apiKeyEnv = "TEST_PI_BASE_URL_API_OVERRIDE_KEY"
	t.Setenv(apiKeyEnv, "sk-override-test")

	workDir := t.TempDir()

	harness := daemon.NewPiHarness(
		"pi",
		"localprov",
		"localprov/mymodel",
		apiKeyEnv,
		"",
		"http://localhost:11434/v1",
		"openai", // explicit api override
	)

	rc := daemon.ExportedRunCtxForPi(workDir, "hk-z13jz-api-override")
	spec, err := harness.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("LaunchSpec: unexpected error: %v", err)
	}

	piAgentDir := findEnvValue(t, spec.Env, "PI_CODING_AGENT_DIR")
	if piAgentDir == "" {
		t.Fatal("PI_CODING_AGENT_DIR not injected into child env")
	}

	modelsBytes, readErr := os.ReadFile(piAgentDir + "/models.json")
	if readErr != nil {
		t.Fatalf("models.json not found: %v", readErr)
	}

	var parsed struct {
		Providers map[string]struct {
			API string `json:"api"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(modelsBytes, &parsed); err != nil {
		t.Fatalf("models.json parse: %v", err)
	}
	prov, ok := parsed.Providers["localprov"]
	if !ok {
		t.Fatal("models.json has no 'localprov' provider")
	}
	if prov.API != "openai" {
		t.Errorf("api = %q; want %q", prov.API, "openai")
	}
}

// TestBuildPiModelsJSON_ModelIDExtraction verifies that buildPiModelsJSON extracts
// the model-id correctly from both "provider/id" and bare "id" forms.
func TestBuildPiModelsJSON_ModelIDExtraction(t *testing.T) {
	t.Parallel()
	cases := []struct {
		model  string
		wantID string
	}{
		{"mylocal/ornith", "ornith"},
		{"openrouter/qwen/qwen3-coder", "qwen3-coder"},
		{"ornith", "ornith"}, // no slash → whole string
		{"", ""},             // empty → empty
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.model, func(t *testing.T) {
			t.Parallel()
			raw, err := daemon.ExportedBuildPiModelsJSON("prov", "http://host/v1", "openai", "", "", tc.model)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var parsed struct {
				Providers map[string]struct {
					Models []struct {
						ID string `json:"id"`
					} `json:"models"`
				} `json:"providers"`
			}
			if jsonErr := json.Unmarshal(raw, &parsed); jsonErr != nil {
				t.Fatalf("parse: %v", jsonErr)
			}
			prov := parsed.Providers["prov"]
			if len(prov.Models) != 1 || prov.Models[0].ID != tc.wantID {
				t.Errorf("model id = %v; want %q", prov.Models, tc.wantID)
			}
		})
	}
}

// TestBuildPiLaunchSpec_BaseURL_NoInjectionOnResumeTurn verifies that when this is
// a resume turn (priorSessionID != nil), NO models.json is written and no
// PI_CODING_AGENT_DIR is injected — even if baseURL is set.
func TestBuildPiLaunchSpec_BaseURL_NoInjectionOnResumeTurn(t *testing.T) {
	// Not parallel: t.Setenv mutates process env.
	const apiKeyEnv = "TEST_PI_BASE_URL_RESUME_KEY"
	t.Setenv(apiKeyEnv, "sk-resume-test")

	workDir := t.TempDir()
	sessionID := "pi-resume-session-id"

	rc := daemon.ExportedPiRunCtx{
		WorkspacePath:    workDir,
		BeadID:           "hk-z13jz-resume",
		Provider:         "mylocal",
		Model:            "mylocal/ornith",
		APIKeyEnv:        apiKeyEnv,
		BaseURL:          "http://dgx.local:8551/v1",
		PriorSessionID:   &sessionID, // resume turn
		BaseEnv:          []string{"PATH=/usr/bin"},
		SkipBillingGuard: true,
	}
	spec, err := daemon.ExportedBuildPiLaunchSpec(rc)
	if err != nil {
		t.Fatalf("ExportedBuildPiLaunchSpec: unexpected error: %v", err)
	}

	// On a resume turn: no PI_CODING_AGENT_DIR injected.
	for _, kv := range spec.Env {
		if strings.HasPrefix(kv, "PI_CODING_AGENT_DIR=") {
			t.Errorf("PI_CODING_AGENT_DIR injected on resume turn: %q", kv)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// rc.Model override tests — per-run model override (hk-oqlgw)
// ─────────────────────────────────────────────────────────────────────────────

// TestPiHarness_LaunchSpec_RcModelOverridesHarnessModel verifies that a non-empty
// rc.Model takes precedence over the harness-level model in argv (hk-oqlgw).
// Uses the full production call chain: NewPiHarness → LaunchSpec.
func TestPiHarness_LaunchSpec_RcModelOverridesHarnessModel(t *testing.T) {
	// Not parallel: t.Setenv mutates process env.
	const apiKeyEnv = "TEST_PI_RC_MODEL_OVERRIDE_KEY"
	t.Setenv(apiKeyEnv, "sk-rc-model-override-sentinel")

	workDir := t.TempDir()
	const harnessModel = "openrouter/qwen/qwen3-coder" // harness-level default
	const rcModel = "openrouter/deepseek/deepseek-r1"  // per-run override

	harness := daemon.NewPiHarness(
		"pi",
		"openrouter",
		harnessModel,
		apiKeyEnv,
		"",
		"",
		"",
	)

	rc := handlercontract.RunCtx{
		WorkspacePath: workDir,
		BeadID:        "hk-oqlgw-override",
		BaseEnv:       []string{"PATH=/usr/bin"},
		Model:         rcModel,
	}

	spec, err := harness.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("LaunchSpec: unexpected error: %v", err)
	}

	piLaunchSpecAssertFlagValue(t, spec.Args, "--model", rcModel)

	for i, arg := range spec.Args {
		if arg == "--model" && i+1 < len(spec.Args) && spec.Args[i+1] == harnessModel {
			t.Error("harness-level model appeared in argv; rc.Model override must take precedence")
		}
	}
}

// TestPiHarness_LaunchSpec_EmptyRcModelFallsBackToHarnessModel verifies that when
// rc.Model is empty, LaunchSpec uses h.model (the harness-level default). hk-oqlgw.
func TestPiHarness_LaunchSpec_EmptyRcModelFallsBackToHarnessModel(t *testing.T) {
	// Not parallel: t.Setenv mutates process env.
	const apiKeyEnv = "TEST_PI_RC_MODEL_FALLBACK_KEY"
	t.Setenv(apiKeyEnv, "sk-rc-model-fallback-sentinel")

	workDir := t.TempDir()
	const harnessModel = "openrouter/qwen/qwen3-coder"

	harness := daemon.NewPiHarness(
		"pi",
		"openrouter",
		harnessModel,
		apiKeyEnv,
		"",
		"",
		"",
	)

	rc := handlercontract.RunCtx{
		WorkspacePath: workDir,
		BeadID:        "hk-oqlgw-fallback",
		BaseEnv:       []string{"PATH=/usr/bin"},
		// Model empty → must fall back to h.model
	}

	spec, err := harness.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("LaunchSpec: unexpected error: %v", err)
	}

	piLaunchSpecAssertFlagValue(t, spec.Args, "--model", harnessModel)
}

// ─────────────────────────────────────────────────────────────────────────────
// C4 rc.* tuple override tests (pi-provider-switch, hk-m6uu2.3)
// ─────────────────────────────────────────────────────────────────────────────

// TestPiHarness_LaunchSpec_RCTupleOverridesGlobal verifies that non-empty
// rc.Provider / rc.APIKeyEnv / rc.BaseURL / rc.API each override the
// harness-level values (h.*) in the produced LaunchSpec argv and models.json.
// Uses the full production call chain: NewPiHarness → LaunchSpec.
func TestPiHarness_LaunchSpec_RCTupleOverridesGlobal(t *testing.T) {
	// Not parallel: t.Setenv mutates process env.

	// Harness-level (global) credentials — must NOT appear in the child env.
	const harnessAPIKeyEnv = "OPENROUTER_API_KEY"
	t.Setenv(harnessAPIKeyEnv, "harness-global-key-must-not-appear")

	// rc-level (override) credentials — must appear.
	const rcAPIKeyEnv = "PI_ORNITH_KEY"
	const rcAPIKeyVal = "sk-ornith-rc-override-sentinel"
	t.Setenv(rcAPIKeyEnv, rcAPIKeyVal)

	workDir := t.TempDir()

	// Harness carries the "old" global openrouter configuration.
	harness := daemon.NewPiHarness(
		"pi",
		"openrouter",
		"openrouter/deepseek/deepseek-v4-flash",
		harnessAPIKeyEnv,
		"",
		"",
		"",
	)

	// rc carries the per-bead ornith-dgx profile override.
	rc := handlercontract.RunCtx{
		WorkspacePath: workDir,
		BeadID:        "hk-m6uu2-rc-override",
		BaseEnv:       []string{"PATH=/usr/bin"},
		Provider:      "ornith",
		Model:         "ornith/deepseek-r1",
		APIKeyEnv:     rcAPIKeyEnv,
		BaseURL:       "http://127.0.0.1:8551/v1",
		API:           "openai-completions",
	}

	spec, err := harness.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("LaunchSpec: unexpected error: %v", err)
	}

	// rc.Provider must override h.provider in argv.
	piLaunchSpecAssertFlagValue(t, spec.Args, "--provider", "ornith")

	// rc.Model must override h.model in argv.
	piLaunchSpecAssertFlagValue(t, spec.Args, "--model", "ornith/deepseek-r1")

	// rc.BaseURL must produce a models.json with the override endpoint.
	piAgentDir := findEnvValue(t, spec.Env, "PI_CODING_AGENT_DIR")
	if piAgentDir == "" {
		t.Fatal("PI_CODING_AGENT_DIR not injected; rc.BaseURL must trigger models.json generation")
	}
	modelsBytes, readErr := os.ReadFile(piAgentDir + "/models.json")
	if readErr != nil {
		t.Fatalf("models.json not found: %v", readErr)
	}
	var parsed struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			API     string `json:"api"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(modelsBytes, &parsed); err != nil {
		t.Fatalf("models.json JSON parse failed: %v\ncontent: %s", err, modelsBytes)
	}
	prov, ok := parsed.Providers["ornith"]
	if !ok {
		t.Fatalf("models.json has no 'ornith' provider; got: %v", parsed.Providers)
	}
	if prov.BaseURL != "http://127.0.0.1:8551/v1" {
		t.Errorf("baseUrl = %q; want %q", prov.BaseURL, "http://127.0.0.1:8551/v1")
	}
	if prov.API != "openai-completions" {
		t.Errorf("api = %q; want %q", prov.API, "openai-completions")
	}

	// rc.APIKeyEnv key must be injected; harness key must be stripped.
	injected := findEnvValue(t, spec.Env, rcAPIKeyEnv)
	if injected != rcAPIKeyVal {
		t.Errorf("rc.APIKeyEnv value = %q; want %q", injected, rcAPIKeyVal)
	}
	for _, kv := range spec.Env {
		if kv == harnessAPIKeyEnv+"=harness-global-key-must-not-appear" {
			t.Error("harness-level key value appeared in child env; rc.APIKeyEnv override must take precedence")
		}
	}
}

// TestPiHarness_LaunchSpec_EmptyRCFallsBackToGlobal verifies that when all five
// rc tuple fields are empty, LaunchSpec falls back to h.* values. Shared with C6.
func TestPiHarness_LaunchSpec_EmptyRCFallsBackToGlobal(t *testing.T) {
	// Not parallel: t.Setenv mutates process env.
	const apiKeyEnv = "TEST_PI_C4_FALLBACK_KEY"
	t.Setenv(apiKeyEnv, "sk-fallback-sentinel")

	workDir := t.TempDir()

	harness := daemon.NewPiHarness(
		"pi",
		"openrouter",
		"openrouter/deepseek/deepseek-v4-flash",
		apiKeyEnv,
		"",
		"",
		"",
	)

	// All five tuple fields empty — must use h.* entirely.
	rc := handlercontract.RunCtx{
		WorkspacePath: workDir,
		BeadID:        "hk-m6uu2-empty-rc",
		BaseEnv:       []string{"PATH=/usr/bin"},
		// Provider, APIKeyEnv, APIKeyFile, BaseURL, API all zero
	}

	spec, err := harness.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("LaunchSpec: unexpected error: %v", err)
	}

	// h.provider must appear in argv.
	piLaunchSpecAssertFlagValue(t, spec.Args, "--provider", "openrouter")
	// h.model must appear in argv.
	piLaunchSpecAssertFlagValue(t, spec.Args, "--model", "openrouter/deepseek/deepseek-v4-flash")

	// No models.json when h.baseURL is also empty.
	piAgentDir := workDir + "/.harmonik/pi-agent"
	if _, statErr := os.Stat(piAgentDir + "/models.json"); statErr == nil {
		t.Error("models.json written when neither rc.BaseURL nor h.baseURL is set; must not exist")
	}

	// h.apiKeyEnv key must be injected in the child env.
	injected := findEnvValue(t, spec.Env, apiKeyEnv)
	if injected != "sk-fallback-sentinel" {
		t.Errorf("h.apiKeyEnv value = %q; want %q", injected, "sk-fallback-sentinel")
	}
}

// TestPiHarness_LaunchSpec_OverriddenAPIKeyEnv_StripsSiblings verifies that when
// rc.APIKeyEnv overrides the harness-level key, buildPiEnv re-runs the fail-closed
// strip keyed on the NEW env var: only the overridden key is injected; the harness-
// level key (and other siblings) appear as KEY= empty-overrides.
func TestPiHarness_LaunchSpec_OverriddenAPIKeyEnv_StripsSiblings(t *testing.T) {
	// Not parallel: t.Setenv mutates process env.

	// harness-level key — must appear STRIPPED (KEY=) in the child env.
	const harnessKeyEnv = "OPENROUTER_API_KEY"
	t.Setenv(harnessKeyEnv, "harness-key-must-be-stripped")

	// rc-level override key — must be INJECTED with its live value.
	const rcKeyEnv = "PI_ORNITH_KEY"
	const rcKeyVal = "sk-rc-ornith-sibling-strip-test"
	t.Setenv(rcKeyEnv, rcKeyVal)

	// Another sibling that should also be stripped.
	const siblingKey = "ANTHROPIC_API_KEY"
	t.Setenv(siblingKey, "sibling-must-be-stripped")

	workDir := t.TempDir()

	harness := daemon.NewPiHarness(
		"pi",
		"openrouter",
		"openrouter/deepseek/deepseek-v4-flash",
		harnessKeyEnv,
		"",
		"",
		"",
	)

	rc := handlercontract.RunCtx{
		WorkspacePath: workDir,
		BeadID:        "hk-m6uu2-sibling-strip",
		BaseEnv: []string{
			"PATH=/usr/bin",
			harnessKeyEnv + "=harness-key-must-be-stripped",
			rcKeyEnv + "=" + rcKeyVal,
			siblingKey + "=sibling-must-be-stripped",
		},
		Provider:  "ornith",
		APIKeyEnv: rcKeyEnv,
	}

	spec, err := harness.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("LaunchSpec: unexpected error: %v", err)
	}

	// rc.APIKeyEnv key must be injected with its value.
	injected := findEnvValue(t, spec.Env, rcKeyEnv)
	if injected != rcKeyVal {
		t.Errorf("rc.APIKeyEnv value = %q; want %q", injected, rcKeyVal)
	}

	// Harness key and sibling keys must be stripped (appear as KEY= with empty value).
	for _, strippedKey := range []string{harnessKeyEnv, siblingKey} {
		for _, kv := range spec.Env {
			if strings.HasPrefix(kv, strippedKey+"=") {
				val := kv[len(strippedKey)+1:]
				if val != "" {
					t.Errorf("PI-021: child env carries live value for %s=%q; must be empty-overridden", strippedKey, val)
				}
			}
		}
	}
}

// TestPiHarness_LaunchSpec_CoupledTriple_TravelTogether verifies the coupling
// invariant: provider, baseURL, and api from the rc tuple are all applied together
// (or all fall back to h.*), proving C4 introduces no per-field split.
func TestPiHarness_LaunchSpec_CoupledTriple_TravelTogether(t *testing.T) {
	// Not parallel: t.Setenv mutates process env.
	const harnessKeyEnv = "OPENROUTER_API_KEY"
	t.Setenv(harnessKeyEnv, "coupled-triple-harness-key")

	const rcKeyEnv = "PI_ORNITH_COUPLED"
	t.Setenv(rcKeyEnv, "coupled-triple-rc-key")

	workDir := t.TempDir()

	// Harness carries openrouter (cloud, no baseURL).
	harness := daemon.NewPiHarness(
		"pi",
		"openrouter",
		"openrouter/deepseek/deepseek-v4-flash",
		harnessKeyEnv,
		"",
		"",
		"",
	)

	// rc carries the full ornith triple {provider, baseURL, api}.
	rc := handlercontract.RunCtx{
		WorkspacePath: workDir,
		BeadID:        "hk-m6uu2-coupled-triple",
		BaseEnv:       []string{"PATH=/usr/bin"},
		Provider:      "ornith",
		Model:         "ornith/deepseek-r1",
		APIKeyEnv:     rcKeyEnv,
		BaseURL:       "http://127.0.0.1:8551/v1",
		API:           "openai-completions",
	}

	spec, err := harness.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("LaunchSpec: unexpected error: %v", err)
	}

	// The full triple must appear in models.json — not a mix of rc+h.
	piAgentDir := findEnvValue(t, spec.Env, "PI_CODING_AGENT_DIR")
	if piAgentDir == "" {
		t.Fatal("PI_CODING_AGENT_DIR not injected; rc.BaseURL must produce models.json")
	}
	modelsBytes, readErr := os.ReadFile(piAgentDir + "/models.json")
	if readErr != nil {
		t.Fatalf("models.json not found: %v", readErr)
	}
	var parsed struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			API     string `json:"api"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(modelsBytes, &parsed); err != nil {
		t.Fatalf("models.json JSON parse failed: %v\ncontent: %s", err, modelsBytes)
	}
	// "ornith" provider key — from rc.Provider, not from h.provider ("openrouter").
	prov, ok := parsed.Providers["ornith"]
	if !ok {
		t.Fatalf("models.json has no 'ornith' provider (expected rc.Provider); got: %v", parsed.Providers)
	}
	if _, openrouterPresent := parsed.Providers["openrouter"]; openrouterPresent {
		t.Error("h.provider 'openrouter' appeared in models.json; rc.Provider must override it")
	}
	// rc.BaseURL must appear, not h.baseURL (empty).
	if prov.BaseURL != "http://127.0.0.1:8551/v1" {
		t.Errorf("baseUrl = %q; want rc.BaseURL %q", prov.BaseURL, "http://127.0.0.1:8551/v1")
	}
	// rc.API must appear, not h.api (empty → would default to "openai").
	if prov.API != "openai-completions" {
		t.Errorf("api = %q; want rc.API %q", prov.API, "openai-completions")
	}

	// argv must use rc.Provider, not h.provider.
	piLaunchSpecAssertFlagValue(t, spec.Args, "--provider", "ornith")
}

// TestPiHarness_DefaultPath_ByteIdentical is the C6 regression guard for the
// pi-provider-switch initiative (hk-m6uu2): it proves that an UNLABELED bead
// (rc.Provider/Model/APIKeyEnv/APIKeyFile/BaseURL/API all zero — the default,
// pre-C1-C4 path every existing bead still takes) produces a SpawnSpec that is
// byte-for-byte identical to what buildPiLaunchSpec would produce driven
// exclusively by harness-level (h.*) config, with no rc-tuple coalescing in the
// path at all. If C4's LaunchSpec override plumbing ever regresses the default
// path (e.g. an accidental non-empty override, a field swap, a models.json
// written when it should not be), this test fails on the Args/Env/WorkDir diff.
func TestPiHarness_DefaultPath_ByteIdentical(t *testing.T) {
	// Not parallel: t.Setenv mutates process env.
	const apiKeyEnv = "TEST_PI_C6_GOLDEN_KEY"
	t.Setenv(apiKeyEnv, "sk-golden-sentinel")

	workDir := t.TempDir()

	const (
		piBinary = "pi"
		provider = "openrouter"
		model    = "openrouter/deepseek/deepseek-v4-flash"
		beadID   = "hk-m6uu2-golden-default"
	)
	baseEnv := []string{"PATH=/usr/bin"}

	harness := daemon.NewPiHarness(piBinary, provider, model, apiKeyEnv, "", "", "")

	// Unlabeled bead: every rc-tuple field is zero.
	rc := handlercontract.RunCtx{
		WorkspacePath: workDir,
		BeadID:        beadID,
		BaseEnv:       baseEnv,
	}

	got, err := harness.LaunchSpec(rc)
	if err != nil {
		t.Fatalf("LaunchSpec: unexpected error: %v", err)
	}

	// Golden: the pre-C4 shape — buildPiLaunchSpec driven ONLY by h.* config,
	// with no rc-tuple involved anywhere in its construction.
	wantSpec, err := daemon.ExportedBuildPiLaunchSpec(daemon.ExportedPiRunCtx{
		PiBinary:      piBinary,
		WorkspacePath: workDir,
		BeadID:        beadID,
		Provider:      provider,
		Model:         model,
		APIKeyEnv:     apiKeyEnv,
		BaseEnv:       baseEnv,
	})
	if err != nil {
		t.Fatalf("golden ExportedBuildPiLaunchSpec: unexpected error: %v", err)
	}
	// Binary/Args/WorkDir are byte-identical (argv order is a hard contract —
	// PI-020). Env is compared as a SET: buildPiEnv strips the credential
	// allowlist by ranging a map (pilaunchspec.go strippedSet), so slice order
	// is non-deterministic independent of C4 — a pre-existing property, not
	// something this test should regress-guard against.
	if got.Binary != wantSpec.Binary {
		t.Errorf("Binary = %q; want %q", got.Binary, wantSpec.Binary)
	}
	if got.WorkDir != wantSpec.WorkDir {
		t.Errorf("WorkDir = %q; want %q", got.WorkDir, wantSpec.WorkDir)
	}
	if !reflect.DeepEqual(got.Args, wantSpec.Args) {
		t.Errorf("Args diverged from the pre-C4 golden shape:\ngot:  %#v\nwant: %#v", got.Args, wantSpec.Args)
	}
	if !sameEnvSet(got.Env, wantSpec.Env) {
		t.Errorf("Env diverged from the pre-C4 golden shape (as a set):\ngot:  %#v\nwant: %#v", got.Env, wantSpec.Env)
	}
}

// sameEnvSet reports whether a and b contain the same "KEY=VALUE" entries,
// ignoring order (buildPiEnv's credential-strip pass ranges a map).
func sameEnvSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, kv := range a {
		counts[kv]++
	}
	for _, kv := range b {
		counts[kv]--
	}
	for _, c := range counts {
		if c != 0 {
			return false
		}
	}
	return true
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers for base_url tests
// ─────────────────────────────────────────────────────────────────────────────

// findEnvValue scans env for key= and returns the value portion.
// Returns "" when not found; fails the test when found with empty value.
func findEnvValue(t *testing.T, env []string, key string) string {
	t.Helper()
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return kv[len(prefix):]
		}
	}
	return ""
}
