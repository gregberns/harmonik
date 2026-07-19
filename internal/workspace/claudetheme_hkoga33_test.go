package workspace

// claudetheme_hkoga33_test.go — regression tests for the ~/.claude.json theme
// pre-seed that suppresses Claude Code's first-run theme-selection modal (hk-oga33).
// Without a seeded top-level "theme", a daemon-spawned claude pane parks on the
// "Choose the text style…" modal before SessionStart and agent_ready times out.

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func readClaudeCfg(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cfg: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse cfg: %v", err)
	}
	return cfg
}

func writeClaudeCfg(t *testing.T, path string, cfg map[string]interface{}) {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestEnsureClaudeTheme_SeedsWhenAbsentFile: no config file at all → creates it
// with theme=dark.
func TestEnsureClaudeTheme_SeedsWhenAbsentFile(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), ".claude.json")
	if err := ensureClaudeThemeAt(cfg); err != nil {
		t.Fatalf("ensureClaudeThemeAt: %v", err)
	}
	got := readClaudeCfg(t, cfg)
	if got["theme"] != claudeDefaultTheme {
		t.Errorf("theme = %v, want %q", got["theme"], claudeDefaultTheme)
	}
}

// TestEnsureClaudeTheme_SeedsWhenAbsentKey / Null / Empty: config exists but the
// theme key is missing, null, or "" → seeded to dark.
func TestEnsureClaudeTheme_SeedsWhenUnset(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"absent-key":  {"projects": map[string]interface{}{}},
		"null-theme":  {"theme": nil},
		"empty-theme": {"theme": ""},
	}
	for name, initial := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := filepath.Join(t.TempDir(), ".claude.json")
			writeClaudeCfg(t, cfg, initial)
			if err := ensureClaudeThemeAt(cfg); err != nil {
				t.Fatalf("ensureClaudeThemeAt: %v", err)
			}
			if got := readClaudeCfg(t, cfg); got["theme"] != claudeDefaultTheme {
				t.Errorf("theme = %v, want %q", got["theme"], claudeDefaultTheme)
			}
		})
	}
}

// TestEnsureClaudeTheme_PreservesOperatorChoice: an explicit non-empty theme is
// never clobbered.
func TestEnsureClaudeTheme_PreservesOperatorChoice(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), ".claude.json")
	writeClaudeCfg(t, cfg, map[string]interface{}{"theme": "light"})
	if err := ensureClaudeThemeAt(cfg); err != nil {
		t.Fatalf("ensureClaudeThemeAt: %v", err)
	}
	if got := readClaudeCfg(t, cfg); got["theme"] != "light" {
		t.Errorf("theme = %v, want it preserved as \"light\"", got["theme"])
	}
}

// TestEnsureClaudeTheme_PreservesOtherKeys: seeding theme must not drop the trust
// map or any other top-level key.
func TestEnsureClaudeTheme_PreservesOtherKeys(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), ".claude.json")
	writeClaudeCfg(t, cfg, map[string]interface{}{
		"projects": map[string]interface{}{
			"/some/worktree": map[string]interface{}{"hasTrustDialogAccepted": true},
		},
		"someOtherKey": "keepme",
	})
	if err := ensureClaudeThemeAt(cfg); err != nil {
		t.Fatalf("ensureClaudeThemeAt: %v", err)
	}
	got := readClaudeCfg(t, cfg)
	if got["theme"] != claudeDefaultTheme {
		t.Errorf("theme = %v, want %q", got["theme"], claudeDefaultTheme)
	}
	if got["someOtherKey"] != "keepme" {
		t.Errorf("someOtherKey dropped: %v", got["someOtherKey"])
	}
	projects, ok := got["projects"].(map[string]interface{})
	if !ok || projects["/some/worktree"] == nil {
		t.Errorf("projects trust map was dropped: %v", got["projects"])
	}
}

// TestEnsureClaudeTheme_Idempotent: a second call is a no-op (fast path) and leaves
// the theme intact.
func TestEnsureClaudeTheme_Idempotent(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), ".claude.json")
	if err := ensureClaudeThemeAt(cfg); err != nil {
		t.Fatalf("first ensureClaudeThemeAt: %v", err)
	}
	if err := ensureClaudeThemeAt(cfg); err != nil {
		t.Fatalf("second ensureClaudeThemeAt: %v", err)
	}
	if got := readClaudeCfg(t, cfg); got["theme"] != claudeDefaultTheme {
		t.Errorf("theme = %v, want %q", got["theme"], claudeDefaultTheme)
	}
}

// TestThemeSetAt_Probe: the lock-free probe correctly classifies set vs unset.
func TestThemeSetAt_Probe(t *testing.T) {
	dir := t.TempDir()
	absent := filepath.Join(dir, "absent.json")
	if set, err := themeSetAt(absent); err != nil || set {
		t.Errorf("absent file: set=%v err=%v, want false,nil", set, err)
	}
	setCfg := filepath.Join(dir, "set.json")
	writeClaudeCfg(t, setCfg, map[string]interface{}{"theme": "dark"})
	if set, err := themeSetAt(setCfg); err != nil || !set {
		t.Errorf("set theme: set=%v err=%v, want true,nil", set, err)
	}
	emptyCfg := filepath.Join(dir, "empty.json")
	writeClaudeCfg(t, emptyCfg, map[string]interface{}{"theme": ""})
	if set, err := themeSetAt(emptyCfg); err != nil || set {
		t.Errorf("empty theme: set=%v err=%v, want false,nil", set, err)
	}
}

// runThemeUpsert runs the REAL workerThemeUpsertProgram via python3 against a
// private HOME (mirrors runTrustUpsert in workertrust_race_test.go). Theme takes
// no argv (it is a global key). Skips when python3 is unavailable.
func runThemeUpsert(t *testing.T, home string) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available — skipping real-python theme test")
	}
	cmd := exec.Command("python3", "-")
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.Stdin = bytes.NewReader([]byte(workerThemeUpsertProgram))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("workerThemeUpsertProgram failed: %v\noutput: %s", err, out)
	}
}

// TestWorkerThemeUpsertProgram_RealPython_Seeds: the actual python program seeds
// theme=dark into a fresh worker ~/.claude.json. Guards the python literal against
// syntax/logic regressions that would only surface on a live REMOTE launch.
func TestWorkerThemeUpsertProgram_RealPython_Seeds(t *testing.T) {
	home := t.TempDir()
	runThemeUpsert(t, home)
	got := readClaudeCfg(t, filepath.Join(home, ".claude.json"))
	if got["theme"] != claudeDefaultTheme {
		t.Errorf("theme = %v, want %q", got["theme"], claudeDefaultTheme)
	}
	// Idempotent second run.
	runThemeUpsert(t, home)
	if got := readClaudeCfg(t, filepath.Join(home, ".claude.json")); got["theme"] != claudeDefaultTheme {
		t.Errorf("after re-run theme = %v, want %q", got["theme"], claudeDefaultTheme)
	}
}

// TestWorkerThemeUpsertProgram_RealPython_PreservesChoiceAndKeys: the python
// program never clobbers an operator's theme and preserves other keys.
func TestWorkerThemeUpsertProgram_RealPython_PreservesChoiceAndKeys(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	// Operator choice preserved.
	home1 := t.TempDir()
	writeClaudeCfg(t, filepath.Join(home1, ".claude.json"), map[string]interface{}{"theme": "light"})
	runThemeUpsert(t, home1)
	if got := readClaudeCfg(t, filepath.Join(home1, ".claude.json")); got["theme"] != "light" {
		t.Errorf("operator theme clobbered: %v", got["theme"])
	}
	// Other keys preserved while seeding.
	home2 := t.TempDir()
	writeClaudeCfg(t, filepath.Join(home2, ".claude.json"), map[string]interface{}{
		"projects":     map[string]interface{}{"/wt": map[string]interface{}{"hasTrustDialogAccepted": true}},
		"someOtherKey": "keepme",
	})
	runThemeUpsert(t, home2)
	got := readClaudeCfg(t, filepath.Join(home2, ".claude.json"))
	if got["theme"] != claudeDefaultTheme {
		t.Errorf("theme = %v, want %q", got["theme"], claudeDefaultTheme)
	}
	if got["someOtherKey"] != "keepme" {
		t.Errorf("someOtherKey dropped: %v", got["someOtherKey"])
	}
	if p, ok := got["projects"].(map[string]interface{}); !ok || p["/wt"] == nil {
		t.Errorf("projects dropped: %v", got["projects"])
	}
}

// TestWorkerThemeUpsertProgram_InSyncWithConst: the python literal theme value must
// stay in sync with the Go claudeDefaultTheme const (they are coupled only by a
// comment). Cheap drift guard.
func TestWorkerThemeUpsertProgram_InSyncWithConst(t *testing.T) {
	if !strings.Contains(workerThemeUpsertProgram, `"`+claudeDefaultTheme+`"`) {
		t.Errorf("workerThemeUpsertProgram does not seed claudeDefaultTheme=%q — Go/python theme drift", claudeDefaultTheme)
	}
}
