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
	"os"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
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
