package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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
