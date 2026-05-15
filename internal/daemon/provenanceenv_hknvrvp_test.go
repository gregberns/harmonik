package daemon_test

// provenanceenv_hknvrvp_test.go — unit tests for HARMONIK_PROJECT_HASH injection
// into handler subprocess environment (hk-nvrvp).
//
// Helper prefix: provenanceEnvFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-nvrvp).

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// provenanceEnvFixtureBus returns a CollectingEmitter sufficient for
// newWorkLoopDeps construction.  The emitter's events are discarded; the
// test only inspects the returned handlerEnv slice.
func provenanceEnvFixtureBus(t *testing.T) handlercontract.EventEmitter {
	t.Helper()
	return &handlercontract.CollectingEmitter{}
}

// TestProvenanceEnv_HandlerEnvContainsProjectHash verifies that newWorkLoopDeps
// (via ExportedNewWorkLoopDepsWithStore) prepends HARMONIK_PROJECT_HASH to the
// handler subprocess env, satisfying dogfood-smoke-trace.md §4 and
// process-lifecycle.md §4.2 PL-006a.
//
// The test skips when `br` is not on PATH (CI environments without br installed).
func TestProvenanceEnv_HandlerEnvContainsProjectHash(t *testing.T) {
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br not on PATH; skipping newWorkLoopDeps integration test")
	}

	projectDir, _ := workloopFixtureProjectDir(t)

	cfg := daemon.Config{
		ProjectDir: projectDir,
		BrPath:     brPath,
	}

	bus := provenanceEnvFixtureBus(t)
	registry := handlercontract.NewAdapterRegistry()
	store := daemon.ExportedNewHookSessionStore()

	deps, depErr := daemon.ExportedNewWorkLoopDepsWithStore(cfg, bus, core.WorkflowModeSingle, registry, store)
	if depErr != nil {
		t.Fatalf("ExportedNewWorkLoopDepsWithStore: %v", depErr)
	}

	env := daemon.HandlerEnvOf(deps)
	if len(env) == 0 {
		t.Fatal("handlerEnv is empty; expected at least HARMONIK_PROJECT_HASH entry")
	}

	// Verify HARMONIK_PROJECT_HASH=<value> is present.
	wantPrefix := lifecycle.ProvenanceEnvKey + "="
	var found string
	for _, kv := range env {
		if strings.HasPrefix(kv, wantPrefix) {
			found = kv
			break
		}
	}
	if found == "" {
		t.Fatalf("HARMONIK_PROJECT_HASH not found in handlerEnv; got: %v", env)
	}

	// Verify the hash value is non-empty.
	hashVal := strings.TrimPrefix(found, wantPrefix)
	if hashVal == "" {
		t.Fatalf("HARMONIK_PROJECT_HASH is set but has empty value in %q", found)
	}

	// Cross-check: the hash must match ComputeProjectHash(projectDir).
	wantHash := lifecycle.ComputeProjectHash(projectDir)
	wantKV := lifecycle.ProvenanceEnvVar(wantHash)
	if found != wantKV {
		t.Fatalf("HARMONIK_PROJECT_HASH mismatch: got %q want %q", found, wantKV)
	}
}

// TestProvenanceEnv_CallerHandlerEnvIsPreserved verifies that when
// Config.HandlerEnv carries additional entries, those entries are retained
// after the HARMONIK_PROJECT_HASH injection and the hash entry comes first.
func TestProvenanceEnv_CallerHandlerEnvIsPreserved(t *testing.T) {
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br not on PATH; skipping newWorkLoopDeps integration test")
	}

	projectDir, _ := workloopFixtureProjectDir(t)

	extraEnv := []string{"MY_CUSTOM_VAR=hello", "ANOTHER_VAR=world"}
	cfg := daemon.Config{
		ProjectDir: projectDir,
		BrPath:     brPath,
		HandlerEnv: extraEnv,
	}

	bus := provenanceEnvFixtureBus(t)
	registry := handlercontract.NewAdapterRegistry()
	store := daemon.ExportedNewHookSessionStore()

	deps, depErr := daemon.ExportedNewWorkLoopDepsWithStore(cfg, bus, core.WorkflowModeSingle, registry, store)
	if depErr != nil {
		t.Fatalf("ExportedNewWorkLoopDepsWithStore: %v", depErr)
	}

	env := daemon.HandlerEnvOf(deps)

	// First entry must be the project hash.
	wantPrefix := lifecycle.ProvenanceEnvKey + "="
	if len(env) == 0 || !strings.HasPrefix(env[0], wantPrefix) {
		t.Fatalf("expected HARMONIK_PROJECT_HASH as first env entry; got: %v", env)
	}

	// All caller-supplied entries must appear after the hash.
	envSet := make(map[string]bool, len(env))
	for _, kv := range env {
		envSet[kv] = true
	}
	for _, extra := range extraEnv {
		if !envSet[extra] {
			t.Errorf("caller HandlerEnv entry %q missing from handlerEnv; full env: %v", extra, env)
		}
	}
}
