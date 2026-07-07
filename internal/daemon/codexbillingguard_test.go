package daemon_test

// codexbillingguard_test.go — unit tests for the positive codex billing guard
// (codex-harness C3/T11, hk-tu48u).
//
// Coverage:
//   - materializeForcedLoginMethod writes forced_login_method="chatgpt" into a
//     fresh CODEX_HOME/config.toml; is idempotent; rewrites a wrong value;
//     preserves pre-existing unrelated config content.
//   - assertChatGPTPlan FAILS CLOSED (table-driven): missing config, wrong
//     value, populated OPENAI_API_KEY in auth.json; PASSES when forced + clean.
//   - runCodexBillingGuard emits codex_billing_guard events (materialized +
//     allowed on success; denied on failure) via a payload-capturing emitter.
//   - buildCodexLaunchSpec refuses to return a spec when the guard fails closed
//     (the end-to-end fail-closed wiring).
//
// All filesystem state uses t.TempDir() as a fake CODEX_HOME; no real codex home
// is touched.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// payload-capturing emitter
// ─────────────────────────────────────────────────────────────────────────────

// capturingBillingEmitter records the (eventType, decoded payload) of every
// emitted event so tests can assert the codex_billing_guard outcome sequence.
// Safe for concurrent use.
type capturingBillingEmitter struct {
	mu      sync.Mutex
	types   []core.EventType
	guards  []core.CodexBillingGuardPayload
	rawErrs []error
}

func (e *capturingBillingEmitter) record(eventType core.EventType, payload []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.types = append(e.types, eventType)
	if eventType == core.EventTypeCodexBillingGuard {
		var pl core.CodexBillingGuardPayload
		if err := json.Unmarshal(payload, &pl); err != nil {
			e.rawErrs = append(e.rawErrs, err)
			return nil
		}
		e.guards = append(e.guards, pl)
	}
	return nil
}

func (e *capturingBillingEmitter) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	return e.record(eventType, payload)
}

func (e *capturingBillingEmitter) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	return e.record(eventType, payload)
}

func (e *capturingBillingEmitter) guardOutcomes() []core.CodexBillingGuardOutcome {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]core.CodexBillingGuardOutcome, 0, len(e.guards))
	for _, g := range e.guards {
		out = append(out, g.Outcome)
	}
	return out
}

// writeForcedConfig writes a config.toml into codexHome with the forced
// chatgpt login method (the materialized-state fixture).
func writeForcedConfig(t *testing.T, codexHome string) {
	t.Helper()
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatalf("writeForcedConfig: mkdir: %v", err)
	}
	line := "forced_login_method = \"" + daemon.ExportedForcedLoginMethodValue + "\"\n"
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(line), 0o600); err != nil {
		t.Fatalf("writeForcedConfig: write: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// materializeForcedLoginMethod
// ─────────────────────────────────────────────────────────────────────────────

// TestMaterializeForcedLoginMethod_FreshHome verifies forced_login_method is
// written into a fresh CODEX_HOME/config.toml.
func TestMaterializeForcedLoginMethod_FreshHome(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := daemon.ExportedMaterializeForcedLoginMethod(home); err != nil {
		t.Fatalf("materialize: unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	want := "forced_login_method = \"chatgpt\""
	if !strings.Contains(string(data), want) {
		t.Errorf("config.toml does not contain %q; got:\n%s", want, string(data))
	}
}

// TestMaterializeForcedLoginMethod_Idempotent verifies a second call leaves the
// already-forced config unchanged (byte-identical).
func TestMaterializeForcedLoginMethod_Idempotent(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := daemon.ExportedMaterializeForcedLoginMethod(home); err != nil {
		t.Fatalf("materialize #1: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatalf("read #1: %v", err)
	}
	if err := daemon.ExportedMaterializeForcedLoginMethod(home); err != nil {
		t.Fatalf("materialize #2: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatalf("read #2: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("idempotency violated:\nfirst:\n%s\nsecond:\n%s", string(first), string(second))
	}
}

// TestMaterializeForcedLoginMethod_PreservesExisting verifies pre-existing
// unrelated config content is preserved and the forced key is appended.
func TestMaterializeForcedLoginMethod_PreservesExisting(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	pre := "model = \"o3\"\n[mcp_servers.foo]\nurl = \"https://example\"\n"
	cfg := filepath.Join(home, "config.toml")
	if err := os.WriteFile(cfg, []byte(pre), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	if err := daemon.ExportedMaterializeForcedLoginMethod(home); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "model = \"o3\"") {
		t.Errorf("pre-existing model line lost; got:\n%s", got)
	}
	if !strings.Contains(got, "[mcp_servers.foo]") {
		t.Errorf("pre-existing table header lost; got:\n%s", got)
	}
	if !strings.Contains(got, "forced_login_method = \"chatgpt\"") {
		t.Errorf("forced_login_method not appended; got:\n%s", got)
	}
}

// TestMaterializeForcedLoginMethod_RewritesWrongValue verifies a pre-existing
// forced_login_method with a DIFFERENT value is rewritten to chatgpt (the guard
// owns this key), and that there is exactly one such line afterwards.
func TestMaterializeForcedLoginMethod_RewritesWrongValue(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := filepath.Join(home, "config.toml")
	if err := os.WriteFile(cfg, []byte("forced_login_method = \"apikey\"\nmodel = \"o3\"\n"), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	if err := daemon.ExportedMaterializeForcedLoginMethod(home); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(data)
	if strings.Contains(got, "apikey") {
		t.Errorf("wrong value \"apikey\" not rewritten; got:\n%s", got)
	}
	if n := strings.Count(got, "forced_login_method"); n != 1 {
		t.Errorf("expected exactly one forced_login_method line, got %d; content:\n%s", n, got)
	}
	if !strings.Contains(got, "model = \"o3\"") {
		t.Errorf("unrelated model line lost during rewrite; got:\n%s", got)
	}
}

// TestMaterializeForcedLoginMethod_EmptyHomeErrors verifies an empty codexHome
// is rejected.
func TestMaterializeForcedLoginMethod_EmptyHomeErrors(t *testing.T) {
	t.Parallel()
	if err := daemon.ExportedMaterializeForcedLoginMethod(""); err == nil {
		t.Error("expected error for empty codexHome; got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// assertChatGPTPlan — fail-closed table test
// ─────────────────────────────────────────────────────────────────────────────

// TestAssertChatGPTPlan_FailClosed is the core fail-closed table test: it
// constructs a CODEX_HOME for each case and asserts whether the pre-flight assert
// permits (nil) or refuses (non-nil) the launch.
func TestAssertChatGPTPlan_FailClosed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		setup   func(t *testing.T, home string)
		wantErr bool
	}{
		{
			name:    "no config at all -> deny",
			setup:   func(t *testing.T, home string) {}, // empty home, no config.toml
			wantErr: true,
		},
		{
			name: "config without forced_login_method -> deny",
			setup: func(t *testing.T, home string) {
				mustWrite(t, filepath.Join(home, "config.toml"), "model = \"o3\"\n")
			},
			wantErr: true,
		},
		{
			name: "forced_login_method=apikey -> deny",
			setup: func(t *testing.T, home string) {
				mustWrite(t, filepath.Join(home, "config.toml"), "forced_login_method = \"apikey\"\n")
			},
			wantErr: true,
		},
		{
			name: "forced chatgpt + no auth.json -> allow",
			setup: func(t *testing.T, home string) {
				writeForcedConfig(t, home)
			},
			wantErr: false,
		},
		{
			name: "forced chatgpt + auth.json without api key -> allow",
			setup: func(t *testing.T, home string) {
				writeForcedConfig(t, home)
				mustWrite(t, filepath.Join(home, "auth.json"),
					`{"OPENAI_API_KEY":"","tokens":{"access_token":"oauth-tok"}}`)
			},
			wantErr: false,
		},
		{
			name: "forced chatgpt BUT auth.json carries populated api key -> deny",
			setup: func(t *testing.T, home string) {
				writeForcedConfig(t, home)
				mustWrite(t, filepath.Join(home, "auth.json"),
					`{"OPENAI_API_KEY":"sk-live-pool-billing-must-deny"}`)
			},
			wantErr: true,
		},
		{
			name: "forced chatgpt BUT auth.json is malformed -> deny (fail closed)",
			setup: func(t *testing.T, home string) {
				writeForcedConfig(t, home)
				mustWrite(t, filepath.Join(home, "auth.json"), "{not-json")
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			tc.setup(t, home)

			err := daemon.ExportedAssertChatGPTPlan(home)
			if tc.wantErr && err == nil {
				t.Errorf("%s: expected fail-closed error, got nil", tc.name)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("%s: expected nil (launch permitted), got error: %v", tc.name, err)
			}
		})
	}
}

// TestAssertChatGPTPlan_EmptyHomeErrors verifies an empty codexHome is rejected.
func TestAssertChatGPTPlan_EmptyHomeErrors(t *testing.T) {
	t.Parallel()
	if err := daemon.ExportedAssertChatGPTPlan(""); err == nil {
		t.Error("expected error for empty codexHome; got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runCodexBillingGuard — event emission
// ─────────────────────────────────────────────────────────────────────────────

// TestRunCodexBillingGuard_Success_EmitsMaterializedThenAllowed verifies the
// happy path: a fresh CODEX_HOME is materialized and the assert permits the
// launch, emitting materialized then allowed and returning nil.
func TestRunCodexBillingGuard_Success_EmitsMaterializedThenAllowed(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	em := &capturingBillingEmitter{}

	if err := daemon.ExportedRunCodexBillingGuard(em, "hk-guard-ok", home); err != nil {
		t.Fatalf("guard returned error on a clean home: %v", err)
	}

	got := em.guardOutcomes()
	want := []core.CodexBillingGuardOutcome{
		core.CodexBillingGuardMaterialized,
		core.CodexBillingGuardAllowed,
	}
	if len(got) != len(want) {
		t.Fatalf("outcome sequence = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("outcome[%d] = %q; want %q", i, got[i], want[i])
		}
	}
}

// TestRunCodexBillingGuard_ApiKeyLogin_EmitsDenied verifies that when auth.json
// carries a populated OPENAI_API_KEY, the guard fails closed: it returns an
// error and emits a denied event (after the materialized event).
func TestRunCodexBillingGuard_ApiKeyLogin_EmitsDenied(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	// Pre-seed an API-key auth.json. materializeForcedLoginMethod will still
	// force the config, but the assert must deny on the populated key.
	mustWrite(t, filepath.Join(home, "auth.json"),
		`{"OPENAI_API_KEY":"sk-pool-billing"}`)

	em := &capturingBillingEmitter{}
	err := daemon.ExportedRunCodexBillingGuard(em, "hk-guard-deny", home)
	if err == nil {
		t.Fatal("guard returned nil; expected fail-closed error on populated OPENAI_API_KEY")
	}

	got := em.guardOutcomes()
	// Must include a denied outcome; the last outcome must be denied.
	if len(got) == 0 || got[len(got)-1] != core.CodexBillingGuardDenied {
		t.Errorf("expected a trailing denied outcome; got sequence %v", got)
	}
}

// TestRunCodexBillingGuard_NilEmitter verifies the guard runs (and fails closed)
// with a nil emitter without panicking — emission is best-effort, enforcement is
// the return value.
func TestRunCodexBillingGuard_NilEmitter(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "auth.json"), `{"OPENAI_API_KEY":"sk-pool"}`)

	if err := daemon.ExportedRunCodexBillingGuard(nil, "hk-guard-nil", home); err == nil {
		t.Error("expected fail-closed error with nil emitter; got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// buildCodexLaunchSpec end-to-end fail-closed wiring
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildCodexLaunchSpec_GuardFailClosed_NoSpec verifies that with the guard
// ENABLED (SkipBillingGuard=false) and a CODEX_HOME that carries an API-key
// auth.json, buildCodexLaunchSpec refuses to return a launchable spec.
func TestBuildCodexLaunchSpec_GuardFailClosed_NoSpec(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "auth.json"), `{"OPENAI_API_KEY":"sk-pool-billing"}`)

	em := &capturingBillingEmitter{}
	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath:  "/tmp/wt-test-codex-guard",
		BeadID:         "hk-guard-e2e",
		Model:          "o4-mini", // required; model guard runs before billing guard
		CodexHome:      home,
		BillingEmitter: em,
		// SkipBillingGuard intentionally false: the guard MUST run.
	}

	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err == nil {
		t.Fatalf("expected fail-closed error; got spec with binary %q and %d args", spec.Binary, len(spec.Args))
	}
	if spec.Binary != "" || len(spec.Args) != 0 {
		t.Errorf("fail-closed must return a zero LaunchSpec; got binary=%q args=%v", spec.Binary, spec.Args)
	}
	got := em.guardOutcomes()
	if len(got) == 0 || got[len(got)-1] != core.CodexBillingGuardDenied {
		t.Errorf("expected a trailing denied event; got %v", got)
	}
}

// TestBuildCodexLaunchSpec_GuardAllows_ReturnsSpec verifies that with the guard
// ENABLED and a clean CODEX_HOME (the guard materializes the forced config and
// the assert passes), buildCodexLaunchSpec returns a valid spec and the env
// carries the materialized CODEX_HOME.
func TestBuildCodexLaunchSpec_GuardAllows_ReturnsSpec(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	em := &capturingBillingEmitter{}
	rc := daemon.ExportedCodexRunCtx{
		WorkspacePath:  "/tmp/wt-test-codex-guard-ok",
		BeadID:         "hk-guard-e2e-ok",
		Model:          "o4-mini", // required; model guard runs before billing guard
		CodexHome:      home,
		BaseEnv:        []string{"PATH=/usr/bin"},
		BillingEmitter: em,
	}

	spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
	if err != nil {
		t.Fatalf("guard should allow a clean home; got error: %v", err)
	}
	if spec.Binary != "codex" {
		t.Errorf("Binary = %q; want codex", spec.Binary)
	}

	// The forced config must have been materialized into the home as a side effect.
	data, rerr := os.ReadFile(filepath.Join(home, "config.toml"))
	if rerr != nil {
		t.Fatalf("read materialized config.toml: %v", rerr)
	}
	if !strings.Contains(string(data), "forced_login_method = \"chatgpt\"") {
		t.Errorf("guard did not materialize forced config; got:\n%s", string(data))
	}

	// The CODEX_HOME env override must point at the same home.
	wantKV := "CODEX_HOME=" + home
	found := false
	for _, kv := range spec.Env {
		if kv == wantKV {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("env missing %q; have %v", wantKV, spec.Env)
	}

	// Outcomes: materialized then allowed.
	got := em.guardOutcomes()
	if len(got) != 2 || got[0] != core.CodexBillingGuardMaterialized || got[1] != core.CodexBillingGuardAllowed {
		t.Errorf("outcome sequence = %v; want [materialized allowed]", got)
	}
}

// mustWrite writes content to path (creating parent dirs) or fails the test.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mustWrite mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("mustWrite %s: %v", path, err)
	}
}
