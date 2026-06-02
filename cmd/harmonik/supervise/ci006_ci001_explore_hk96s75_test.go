package supervisecmd

// Exploratory test for specs/credential-isolation.md acceptance scenario 2:
//
//   A fresh supervise start with no operator export but a gitignored credential
//   file authenticates the holder process from that file (CI-006); the daemon
//   process env, inspected at the same boot, contains no deny-list key
//   (CI-001/CI-INV-001).
//
// Observable:
//   - resolveAPIKey sources ANTHROPIC_API_KEY from .env when absent from env.
//   - The value survives the WriteConfigAtomic → ReadConfig round-trip.
//   - buildPiEnv injects the scoped key into Pi's env while stripping all
//     ambient credential deny-list keys (CI-005, CI-INV-001).
//   - ClaudeEnvVars (daemon child-env path) emits only empty overrides for all
//     credential deny-list keys regardless of what the daemon's base env holds
//     (CI-001, CI-INV-001).
//
// Spec: specs/credential-isolation.md CI-001, CI-006, CI-INV-001.
// Bead: hk-96s75

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
)

// TestCI006_CI001_Explore probes acceptance scenario 2 of credential-isolation.md
// end-to-end across the supervise start path (CI-006, CI-005) and the daemon
// child-env path (CI-001, CI-INV-001).
func TestCI006_CI001_Explore(t *testing.T) {
	// Phase 1: .env → resolveAPIKey → config.json → buildPiEnv → Pi env carries
	// the scoped credential and strips all other ambient deny-list keys.
	t.Run("DotEnvToConfigToPiEnv", func(t *testing.T) {
		// Precondition: ANTHROPIC_API_KEY absent from the operator env (no export).
		unsetenvWithRestore(t, "ANTHROPIC_API_KEY")

		dir := t.TempDir()
		const sentinel = "sk-ant-dotenv-ci006-probe"
		dotenvContent := "# gitignored credential file\nANTHROPIC_API_KEY=" + sentinel + "\n"
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(dotenvContent), 0o600); err != nil {
			t.Fatalf("write .env: %v", err)
		}

		// CI-006: resolveAPIKey must fall back to the .env file when the env var is absent.
		resolved, err := resolveAPIKey(dir, false)
		if err != nil {
			t.Fatalf("CI-006: resolveAPIKey from .env: unexpected error: %v", err)
		}
		if resolved != sentinel {
			t.Fatalf("CI-006: resolveAPIKey from .env: got %q, want sentinel", resolved)
		}

		// Persist the resolved key into config.json — the shim reads this at exec time
		// to inject the credential into Pi's env (CI-005).
		cfg := Config{
			SchemaVersion: configSchemaVersion,
			Command:       []string{"claude", "--pi"},
			APIKey:        resolved,
		}
		if err := WriteConfigAtomic(dir, cfg); err != nil {
			t.Fatalf("WriteConfigAtomic: %v", err)
		}
		gotCfg, err := ReadConfig(dir)
		if err != nil {
			t.Fatalf("ReadConfig: %v", err)
		}
		if gotCfg.APIKey != sentinel {
			t.Fatalf("config round-trip APIKey: got %q, want sentinel", gotCfg.APIKey)
		}

		// Inject ambient deny-list keys to verify CI-005/CI-INV-001 stripping.
		t.Setenv("ANTHROPIC_AUTH_TOKEN", "ambient-auth-must-be-stripped")
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "ambient-oauth-must-be-stripped")

		piEnv := buildPiEnv(gotCfg.APIKey)
		piMap := toEnvMap(piEnv)

		// Pi must receive the scoped key from config (CI-006 / CI-005).
		if got, ok := piMap["ANTHROPIC_API_KEY"]; !ok || got != sentinel {
			t.Errorf("Pi env ANTHROPIC_API_KEY: got %q ok=%v; want sentinel injected from .env", got, ok)
		}

		// Ambient credential keys must be absent or carry an empty value (CI-INV-001).
		for _, k := range []string{"ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"} {
			if v, present := piMap[k]; present && v != "" {
				// Report key name only — never emit credential values (CI-007).
				t.Errorf("CI-INV-001: ambient credential key %q leaked into Pi env (non-empty value); want absent or empty", k)
			}
		}
	})

	// Phase 2: even when the daemon's base env (inherited os.Environ()) carries a
	// live credential, ClaudeEnvVars emits only empty overrides for all credential
	// deny-list keys — the daemon process and its claude children carry no live
	// credential at the same boot (CI-001 / CI-INV-001).
	t.Run("DaemonChildEnvNoLiveCredential", func(t *testing.T) {
		childEnv := handler.ClaudeEnvVars(handler.ClaudeEnvConfig{
			RunID:            "run-ci001-explore",
			DaemonSocket:     "/tmp/ci001-explore.sock",
			WorkspacePath:    "/ws/ci001-explore",
			HandlerSessionID: "h-ci001",
			ClaudeSessionID:  "c-ci001",
			WorkflowID:       "wf-ci001",
			NodeID:           "n-ci001",
			// Simulate the daemon inheriting live credentials from the operator shell.
			BaseEnv: []string{
				"ANTHROPIC_API_KEY=sk-ant-ambient-must-not-reach-child",
				"ANTHROPIC_AUTH_TOKEN=bearer-ambient-must-not-reach-child",
				"CLAUDE_CODE_OAUTH_TOKEN=oauth-ambient-must-not-reach-child",
				// Non-credential key that must survive the scrub.
				"HOME=/home/operator",
			},
		})

		// All credential deny-list keys must be present as empty overrides; none
		// may carry a non-empty value (CI-001 / CI-INV-001).
		for _, kv := range childEnv {
			idx := strings.IndexByte(kv, '=')
			if idx < 0 {
				continue
			}
			key, val := kv[:idx], kv[idx+1:]
			if handler.IsCredentialDenyListKey(key) && val != "" {
				// Report key name only per CI-007 — never emit matched credential values.
				t.Errorf("CI-001: child env key %q has non-empty value; credential must not reach daemon-spawned claude", key)
			}
		}

		// Non-credential baseline var must pass through unmodified.
		childMap := toEnvMap(childEnv)
		if got, ok := childMap["HOME"]; !ok || got != "/home/operator" {
			t.Errorf("HOME = %q ok=%v; want /home/operator", got, ok)
		}
	})
}
