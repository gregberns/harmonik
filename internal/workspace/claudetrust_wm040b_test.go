package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// setTempClaudeConfig redirects defaultClaudeGlobalConfigPath to a
// temp-dir-scoped path for the duration of the test. This prevents any test
// from touching the real ~/.claude.json on the developer's machine.
func setTempClaudeConfig(t *testing.T) string {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), ".claude.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", cfgPath)
	return cfgPath
}

// TestWM040b_FreshConfig verifies that EnsureWorktreeTrust creates a new
// ~/.claude.json with a trusted project entry when the file does not exist.
func TestWM040b_FreshConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-abc")

	if err := ensureWorktreeTrustAt(worktreePath, cfgPath); err != nil {
		t.Fatalf("WM-040b: ensureWorktreeTrustAt (fresh): %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("WM-040b: ReadFile after fresh write: %v", err)
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("WM-040b: Unmarshal fresh config: %v", err)
	}

	projects, ok := cfg["projects"].(map[string]interface{})
	if !ok {
		t.Fatal("WM-040b: fresh config missing 'projects' map")
	}
	entry, ok := projects[worktreePath].(map[string]interface{})
	if !ok {
		t.Fatalf("WM-040b: fresh config missing entry for %s", worktreePath)
	}
	trusted, ok := entry["hasTrustDialogAccepted"].(bool)
	if !ok || !trusted {
		t.Fatalf("WM-040b: hasTrustDialogAccepted not true; got %v", entry["hasTrustDialogAccepted"])
	}
}

// TestWM040b_ExistingConfigPreserved verifies that EnsureWorktreeTrust does
// not disturb existing keys in ~/.claude.json (other projects, top-level keys).
func TestWM040b_ExistingConfigPreserved(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-xyz")

	// Write a pre-existing config with a different project and a top-level key.
	initial := map[string]interface{}{
		"theme": "dark",
		"projects": map[string]interface{}{
			"/some/other/project": map[string]interface{}{
				"hasTrustDialogAccepted": true,
				"lastCost":               1.5,
			},
		},
	}
	raw, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(cfgPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("WM-040b: WriteFile initial config: %v", err)
	}

	if err := ensureWorktreeTrustAt(worktreePath, cfgPath); err != nil {
		t.Fatalf("WM-040b: ensureWorktreeTrustAt (merge): %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("WM-040b: Unmarshal merged config: %v", err)
	}

	// Top-level "theme" key MUST be preserved.
	if cfg["theme"] != "dark" {
		t.Errorf("WM-040b: top-level 'theme' key lost; got %v", cfg["theme"])
	}

	projects, _ := cfg["projects"].(map[string]interface{})

	// Existing project entry MUST be preserved.
	other, ok := projects["/some/other/project"].(map[string]interface{})
	if !ok {
		t.Fatal("WM-040b: existing project entry lost")
	}
	if other["lastCost"] != 1.5 {
		t.Errorf("WM-040b: existing project 'lastCost' lost; got %v", other["lastCost"])
	}

	// New worktree entry MUST be trusted.
	entry, ok := projects[worktreePath].(map[string]interface{})
	if !ok {
		t.Fatalf("WM-040b: new worktree entry missing for %s", worktreePath)
	}
	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Errorf("WM-040b: hasTrustDialogAccepted not true for new worktree")
	}
}

// TestWM040b_Idempotent verifies that calling EnsureWorktreeTrust twice for
// the same path does not duplicate or corrupt the entry.
func TestWM040b_Idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-idem")

	if err := ensureWorktreeTrustAt(worktreePath, cfgPath); err != nil {
		t.Fatalf("WM-040b idempotent: first call: %v", err)
	}
	if err := ensureWorktreeTrustAt(worktreePath, cfgPath); err != nil {
		t.Fatalf("WM-040b idempotent: second call: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("WM-040b idempotent: Unmarshal: %v", err)
	}

	projects, _ := cfg["projects"].(map[string]interface{})
	entry, ok := projects[worktreePath].(map[string]interface{})
	if !ok {
		t.Fatal("WM-040b idempotent: entry missing after second call")
	}
	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Error("WM-040b idempotent: hasTrustDialogAccepted not true after second call")
	}
}

// TestWM040b_UntrustedEntryUpgraded verifies that an existing entry with
// hasTrustDialogAccepted = false is upgraded to true.
func TestWM040b_UntrustedEntryUpgraded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-upgrade")

	initial := map[string]interface{}{
		"projects": map[string]interface{}{
			worktreePath: map[string]interface{}{
				"hasTrustDialogAccepted": false,
			},
		},
	}
	raw, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(cfgPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("WM-040b upgrade: WriteFile: %v", err)
	}

	if err := ensureWorktreeTrustAt(worktreePath, cfgPath); err != nil {
		t.Fatalf("WM-040b upgrade: ensureWorktreeTrustAt: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	var cfg map[string]interface{}
	_ = json.Unmarshal(data, &cfg)
	projects, _ := cfg["projects"].(map[string]interface{})
	entry, _ := projects[worktreePath].(map[string]interface{})
	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Error("WM-040b upgrade: hasTrustDialogAccepted should have been upgraded to true")
	}
}

// TestWM040b_MalformedConfigFails verifies that a malformed ~/.claude.json
// returns an error rather than silently corrupting the file.
func TestWM040b_MalformedConfigFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-bad")

	if err := os.WriteFile(cfgPath, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("WM-040b malformed: WriteFile: %v", err)
	}

	err := ensureWorktreeTrustAt(worktreePath, cfgPath)
	if err == nil {
		t.Fatal("WM-040b malformed: expected error, got nil")
	}
}

// TestWM040b_EnvVarOverride verifies that HARMONIK_CLAUDE_CONFIG_PATH redirects
// EnsureWorktreeTrust away from the real ~/.claude.json.
// Not parallel: uses t.Setenv (env mutation requires serial execution).
func TestWM040b_EnvVarOverride(t *testing.T) {
	cfgPath := setTempClaudeConfig(t)
	worktreePath := filepath.Join(t.TempDir(), "worktrees", "run-envvar")

	if err := EnsureWorktreeTrust(worktreePath); err != nil {
		t.Fatalf("WM-040b env-override: EnsureWorktreeTrust: %v", err)
	}

	data, err := os.ReadFile(cfgPath) //nolint:gosec // G304: cfgPath is a temp path set by t.Setenv
	if err != nil {
		t.Fatalf("WM-040b env-override: ReadFile: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("WM-040b env-override: Unmarshal: %v", err)
	}
	projects, ok := cfg["projects"].(map[string]interface{})
	if !ok {
		t.Fatal("WM-040b env-override: projects map missing")
	}
	entry, ok := projects[worktreePath].(map[string]interface{})
	if !ok {
		t.Fatalf("WM-040b env-override: entry missing for %s", worktreePath)
	}
	trusted, ok := entry["hasTrustDialogAccepted"].(bool)
	if !ok || !trusted {
		t.Error("WM-040b env-override: hasTrustDialogAccepted not true")
	}
}

// TestEnsureWorktreeTrust_ConcurrentWrites verifies that N goroutines each
// calling EnsureWorktreeTrust with a distinct worktree path against the same
// config file all land their entries without corruption. The flock serializes
// RMW cycles; the final config MUST parse cleanly and contain all N entries.
// Not parallel: uses t.Setenv (env mutation requires serial execution).
func TestEnsureWorktreeTrust_ConcurrentWrites(t *testing.T) {
	const N = 16

	cfgPath := setTempClaudeConfig(t)
	baseDir := t.TempDir()

	worktreePaths := make([]string, N)
	for i := 0; i < N; i++ {
		worktreePaths[i] = filepath.Join(baseDir, fmt.Sprintf("run-%02d", i))
	}

	var wg sync.WaitGroup
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = EnsureWorktreeTrust(worktreePaths[idx])
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("WM-040b concurrent: goroutine %d: %v", i, err)
		}
	}

	data, err := os.ReadFile(cfgPath) //nolint:gosec // G304: cfgPath is a temp path set by t.Setenv
	if err != nil {
		t.Fatalf("WM-040b concurrent: ReadFile: %v", err)
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("WM-040b concurrent: Unmarshal (config corrupted?): %v", err)
	}

	projects, ok := cfg["projects"].(map[string]interface{})
	if !ok {
		t.Fatal("WM-040b concurrent: projects map missing")
	}

	for _, p := range worktreePaths {
		entry, ok := projects[p].(map[string]interface{})
		if !ok {
			t.Errorf("WM-040b concurrent: entry missing for %s", p)
			continue
		}
		trusted, ok := entry["hasTrustDialogAccepted"].(bool)
		if !ok || !trusted {
			t.Errorf("WM-040b concurrent: hasTrustDialogAccepted not true for %s", p)
		}
	}
}
