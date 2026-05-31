package daemon_test

// ci003_ci004a_hk24d72_test.go — scenario test for credential isolation
// (specs/credential-isolation.md CI-003/CI-004a).
//
// Under test: buildClaudeLaunchSpec (via ExportedBuildClaudeLaunchSpec), which
// assembles the child env for a daemon-spawned claude implementer or reviewer.
//
// Scenario: the daemon's BaseEnv carries live credential env deny-list keys
// (ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_OAUTH*), simulating the
// 2026-05-30 API-key burn incident (hk-f2nm1) where ANTHROPIC_API_KEY was present
// in the operator shell via repo .env.  The assembled spec.Env must contain zero
// deny-list keys with non-empty values.
//
// Observable terminal condition: spec.Env contains only empty overrides (KEY=) for
// every deny-list key; a non-deny-list var threaded through BaseEnv survives
// unmodified, proving the scrub is deny-list-keyed rather than a blanket strip
// (acceptance scenario 1 in specs/credential-isolation.md §6).
//
// Assertions are by-key only; credential values are never printed or emitted (CI-007).
//
// Locks: CI-INV-002 — every daemon-spawned claude implementer/reviewer child env is
// free of every credential env deny-list key.
//
// Spec: specs/credential-isolation.md CI-002, CI-003, CI-004a, CI-007, CI-INV-002.
// Bead: hk-24d72.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ci003WorkspaceDir creates a minimal workspace directory for the test
// (needs a .claude/ subdir so CheckSettingsLocalJSON does not error).
func ci003WorkspaceDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatalf("ci003WorkspaceDir: MkdirAll .claude/: %v", err)
	}
	return dir
}

// ci003EnvMap parses a "KEY=VALUE" env slice into a map (last entry wins).
// Errors on malformed entries. Credential values are never logged (CI-007).
func ci003EnvMap(t *testing.T, env []string) map[string]string {
	t.Helper()
	m := make(map[string]string, len(env))
	for _, kv := range env {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			t.Errorf("ci003EnvMap: malformed env entry (no '='): %q", kv)
			continue
		}
		m[kv[:idx]] = kv[idx+1:]
	}
	return m
}

// credentialBaseEnv returns a BaseEnv slice that contains all three deny-list
// key types (ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_OAUTH*) plus
// a non-deny-list key that must survive the scrub.
//
// Values are test-only sentinels; real credentials are never used (CI-007).
func credentialBaseEnv() []string {
	return []string{
		// Exact deny-list members (CI-002).
		"ANTHROPIC_API_KEY=ci003-sentinel-must-not-reach-child",
		"ANTHROPIC_AUTH_TOKEN=ci003-sentinel-must-not-reach-child",
		// CLAUDE_CODE_OAUTH* prefix (CI-002): well-known variant.
		"CLAUDE_CODE_OAUTH_TOKEN=ci003-sentinel-must-not-reach-child",
		// CLAUDE_CODE_OAUTH* prefix: non-well-known variant.
		"CLAUDE_CODE_OAUTH_CUSTOM_VARIANT=ci003-sentinel-must-not-reach-child",
		// Non-deny-list key — must survive unmodified (acceptance scenario 1).
		"HARMONIK_PROJECT_HASH=deadbeef123456",
	}
}

// assertCI003InvariantsOnEnv checks that spec.Env satisfies CI-003 and
// CI-INV-002: all deny-list keys must be present as explicit empty overrides
// (KEY=), and none may carry a non-empty value.
//
// tag is a short string used in error messages to identify the test case;
// it does NOT embed credential values.
func assertCI003InvariantsOnEnv(t *testing.T, tag string, env []string) {
	t.Helper()

	// Pass 1: no deny-list key carries a live value (CI-INV-002).
	// Report key name only; never emit the matched value (CI-007).
	for _, kv := range env {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		key, val := kv[:idx], kv[idx+1:]
		if handler.IsCredentialDenyListKey(key) && val != "" {
			t.Errorf("[%s] CI-INV-002: child env key %q has non-empty value; "+
				"credential must not reach daemon-spawned claude child (CI-003)", tag, key)
		}
	}

	// Pass 2: the fixed deny-list keys and the variants seen in BaseEnv must be
	// present as explicit empty overrides (CI-003).
	// tmux -e is additive over the server env; merely omitting a key leaves the
	// server env value intact — an explicit KEY= zeros it in the spawned window.
	requiredOverrides := []string{
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_AUTH_TOKEN",
		"CLAUDE_CODE_OAUTH_TOKEN",
		"CLAUDE_CODE_OAUTH_CUSTOM_VARIANT",
	}
	envMap := ci003EnvMap(t, env)
	for _, key := range requiredOverrides {
		if _, ok := envMap[key]; !ok {
			t.Errorf("[%s] CI-003: spec.Env missing explicit empty override for deny-list key %q; "+
				"required to zero tmux server env inheritance", tag, key)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCI003CI004a_ScrubbedFromBaseEnv
// ─────────────────────────────────────────────────────────────────────────────

// TestCI003CI004a_ScrubbedFromBaseEnv is the named regression test for
// specs/credential-isolation.md CI-004a (acceptance scenario 1).
//
// It exercises the implementer-initial phase (the primary scenario from the
// 2026-05-30 incident) and verifies:
//  1. Credential env deny-list keys are stripped and replaced with explicit empty
//     overrides in spec.Env (CI-003).
//  2. A non-deny-list BaseEnv var (HARMONIK_PROJECT_HASH) survives unmodified,
//     proving the scrub is deny-list-keyed, not a blanket strip (CI-004a §6).
//  3. Assertions are by-key only; no credential values are printed (CI-007).
//
// Spec: specs/credential-isolation.md CI-003, CI-004a, CI-007, CI-INV-002.
// Bead: hk-24d72.
func TestCI003CI004a_ScrubbedFromBaseEnv(t *testing.T) {
	t.Parallel()

	ws := ci003WorkspaceDir(t)
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("TestCI003CI004a_ScrubbedFromBaseEnv: mint runUID: %v", err)
	}

	rc := daemon.ExportedClaudeRunCtx{
		RunID:          core.RunID(runUID),
		BeadID:         "hk-24d72-ci003-primary",
		WorkspacePath:  ws,
		DaemonSocket:   "/tmp/harmonik-ci003-hk24d72-primary.sock",
		WorkflowMode:   core.WorkflowModeSingle,
		Phase:          handlercontract.ReviewLoopPhaseImplementerInitial,
		IterationCount: 1,
		HandlerBinary:  "claude",
		BaseEnv:        credentialBaseEnv(),
	}

	spec, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("TestCI003CI004a_ScrubbedFromBaseEnv: ExportedBuildClaudeLaunchSpec: %v", err)
	}

	// CI-003 / CI-INV-002: deny-list keys must be scrubbed + empty-overridden.
	assertCI003InvariantsOnEnv(t, "implementer-initial", spec.Env)

	// CI-004a acceptance scenario 1: non-deny-list var survives.
	envMap := ci003EnvMap(t, spec.Env)
	if got, ok := envMap["HARMONIK_PROJECT_HASH"]; !ok || got != "deadbeef123456" {
		t.Errorf("CI-004a: non-deny-list var HARMONIK_PROJECT_HASH = %q ok=%v; "+
			"want %q — scrub must be deny-list-keyed, not blanket", got, ok, "deadbeef123456")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCI003CI004a_AllPhases_CredentialsDenied
// ─────────────────────────────────────────────────────────────────────────────

// TestCI003CI004a_AllPhases_CredentialsDenied verifies CI-INV-002 across all
// four workflow phases (single, implementer-initial, implementer-resume, reviewer),
// ensuring the credential scrub is not phase-gated.
//
// Spec: specs/credential-isolation.md CI-003, CI-INV-002.
// Bead: hk-24d72.
func TestCI003CI004a_AllPhases_CredentialsDenied(t *testing.T) {
	t.Parallel()

	// Mint a prior session ID once; shared across implementer-resume sub-tests.
	priorUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("TestCI003CI004a_AllPhases_CredentialsDenied: mint priorUID: %v", err)
	}
	priorSessID := priorUID.String()

	type phaseCase struct {
		name              string
		phase             handlercontract.ReviewLoopPhase
		priorClaudeSessID *string
		iterationCount    int
	}
	cases := []phaseCase{
		{"single", "", nil, 0},
		{"implementer-initial", handlercontract.ReviewLoopPhaseImplementerInitial, nil, 1},
		{"implementer-resume", handlercontract.ReviewLoopPhaseImplementerResume, &priorSessID, 2},
		{"reviewer", handlercontract.ReviewLoopPhaseReviewer, nil, 1},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ws := ci003WorkspaceDir(t)
			runUID, err := uuid.NewV7()
			if err != nil {
				t.Fatalf("[%s] mint runUID: %v", tc.name, err)
			}

			rc := daemon.ExportedClaudeRunCtx{
				RunID:             core.RunID(runUID),
				BeadID:            "hk-24d72-phase-" + tc.name,
				WorkspacePath:     ws,
				DaemonSocket:      "/tmp/harmonik-ci003-hk24d72-" + tc.name + ".sock",
				WorkflowMode:      core.WorkflowModeSingle,
				Phase:             tc.phase,
				IterationCount:    tc.iterationCount,
				PriorClaudeSessID: tc.priorClaudeSessID,
				HandlerBinary:     "claude",
				// BaseEnv carries live credentials — must be stripped at CHB-006 boundary.
				BaseEnv: []string{
					"ANTHROPIC_API_KEY=ci003-sentinel-must-not-reach-child",
					"ANTHROPIC_AUTH_TOKEN=ci003-sentinel-must-not-reach-child",
					"CLAUDE_CODE_OAUTH_TOKEN=ci003-sentinel-must-not-reach-child",
				},
			}

			spec, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
			if err != nil {
				t.Fatalf("[%s] ExportedBuildClaudeLaunchSpec: %v", tc.name, err)
			}

			// CI-INV-002: no deny-list key carries a live value in any phase.
			for _, kv := range spec.Env {
				idx := strings.IndexByte(kv, '=')
				if idx < 0 {
					continue
				}
				key, val := kv[:idx], kv[idx+1:]
				if handler.IsCredentialDenyListKey(key) && val != "" {
					// Report key name only; never emit credential values (CI-007).
					t.Errorf("[%s] CI-INV-002: child env key %q has non-empty value; "+
						"credential must not reach daemon-spawned claude (CI-003)", tc.name, key)
				}
			}
		})
	}
}
