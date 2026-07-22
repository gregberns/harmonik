package workspace

// claudeconfigdir_hk8juwz_test.go — unit tests for PrepareIsolatedClaudeConfigDir.
//
// NOTE: no production LOCAL path reaches this function any more — the claude:LOCAL
// isolation was reverted (hk-8juwz: it broke claude auth). These tests survive
// because the REMOTE path (PrepareIsolatedClaudeConfigDirVia, hk-qxvc2) keeps the
// same seeding contract, and they pin that mechanism. The local regression guard
// lives in internal/daemon/claudelaunchspec_configdir_hk8juwz_test.go.
//
// UNIT SCOPE: these prove the MECHANISM only — the isolated dir is created, seeded
// from the operator's real config (or the fallback), trusted for the worktree, and
// distinct from the shared global config. They do NOT prove the modal is dismissed
// on a live run; that requires live agent_ready verification (host-lull batch).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// withIsolatedConfigSource redirects the isolatedConfigSourcePath test seam for the
// duration of the test, restoring it afterward.
func withIsolatedConfigSource(t *testing.T, src string) {
	t.Helper()
	prev := isolatedConfigSourcePath
	isolatedConfigSourcePath = func() (string, error) { return src, nil }
	t.Cleanup(func() { isolatedConfigSourcePath = prev })
}

// TestPrepareIsolatedClaudeConfigDir_CopiesSourceAndTrusts: given a fake source
// config, the isolated dir is created under the worktree, the source content is
// copied (onboarding keys preserved), the worktree-trust entry is upserted, and
// the returned dir is the co-located .harmonik/claude-config path.
func TestPrepareIsolatedClaudeConfigDir_CopiesSourceAndTrusts(t *testing.T) {
	wt := t.TempDir()

	// Fake operator config with modal-dismissing onboarding state.
	srcDir := t.TempDir()
	srcCfg := filepath.Join(srcDir, ".claude.json")
	srcContent := map[string]interface{}{
		"firstStartTime":   "2026-01-02T03:04:05.678Z",
		"migrationVersion": 13,
		"tipsHistory":      map[string]interface{}{"tip": 1},
		"operatorKey":      "keepme",
	}
	writeClaudeCfg(t, srcCfg, srcContent)
	withIsolatedConfigSource(t, srcCfg)

	dir, err := PrepareIsolatedClaudeConfigDir(wt)
	if err != nil {
		t.Fatalf("PrepareIsolatedClaudeConfigDir: %v", err)
	}

	wantDir := filepath.Join(wt, ".harmonik", "claude-config")
	if dir != wantDir {
		t.Errorf("returned dir = %q, want %q", dir, wantDir)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("returned dir %q is not absolute", dir)
	}

	isoCfg := filepath.Join(dir, ".claude.json")
	got := readClaudeCfg(t, isoCfg)

	// Copied onboarding keys preserved.
	if got["firstStartTime"] != "2026-01-02T03:04:05.678Z" {
		t.Errorf("firstStartTime = %v, want copied value", got["firstStartTime"])
	}
	if got["operatorKey"] != "keepme" {
		t.Errorf("operatorKey dropped: %v", got["operatorKey"])
	}
	if _, ok := got["tipsHistory"]; !ok {
		t.Errorf("tipsHistory onboarding key dropped: %v", got)
	}

	// Worktree-trust entry upserted (keyed by realpath of the worktree).
	resolved := wt
	if r, rerr := filepath.EvalSymlinks(wt); rerr == nil {
		resolved = r
	}
	projects, ok := got["projects"].(map[string]interface{})
	if !ok {
		t.Fatalf("projects map absent from isolated config: %v", got)
	}
	entry, ok := projects[resolved].(map[string]interface{})
	if !ok {
		t.Fatalf("no trust entry for %q in isolated config: %v", resolved, projects)
	}
	if entry["hasTrustDialogAccepted"] != true {
		t.Errorf("hasTrustDialogAccepted = %v, want true", entry["hasTrustDialogAccepted"])
	}
}

// TestPrepareIsolatedClaudeConfigDir_IsolatedNotShared: the seed lands in the
// isolated per-worktree dir, NOT in the shared global config path.
func TestPrepareIsolatedClaudeConfigDir_IsolatedNotShared(t *testing.T) {
	wt := t.TempDir()

	// A shared-global config that must be left untouched by this function.
	sharedDir := t.TempDir()
	sharedCfg := filepath.Join(sharedDir, ".claude.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", sharedCfg) // claudeGlobalConfigPath -> sharedCfg

	srcDir := t.TempDir()
	srcCfg := filepath.Join(srcDir, ".claude.json")
	writeClaudeCfg(t, srcCfg, map[string]interface{}{"firstStartTime": "2026-01-01T00:00:00.000Z"})
	withIsolatedConfigSource(t, srcCfg)

	dir, err := PrepareIsolatedClaudeConfigDir(wt)
	if err != nil {
		t.Fatalf("PrepareIsolatedClaudeConfigDir: %v", err)
	}

	// The isolated config exists...
	if _, err := os.Stat(filepath.Join(dir, ".claude.json")); err != nil {
		t.Errorf("isolated config not written: %v", err)
	}
	// ...and the shared-global config was NOT created/written by this function.
	if _, err := os.Stat(sharedCfg); !os.IsNotExist(err) {
		t.Errorf("shared-global config was touched (err=%v), want it left absent", err)
	}
}

// TestPrepareIsolatedClaudeConfigDir_MissingSourceFallback: when the source config
// is absent, the isolated config falls back to a minimal onboarding-complete config
// (firstStartTime set) and is still trusted.
func TestPrepareIsolatedClaudeConfigDir_MissingSourceFallback(t *testing.T) {
	wt := t.TempDir()

	// Source path points at a non-existent file.
	missing := filepath.Join(t.TempDir(), "does-not-exist", ".claude.json")
	withIsolatedConfigSource(t, missing)

	dir, err := PrepareIsolatedClaudeConfigDir(wt)
	if err != nil {
		t.Fatalf("PrepareIsolatedClaudeConfigDir (missing source): %v", err)
	}

	got := readClaudeCfg(t, filepath.Join(dir, ".claude.json"))
	if got["firstStartTime"] != fallbackFirstStartTime {
		t.Errorf("fallback firstStartTime = %v, want %q", got["firstStartTime"], fallbackFirstStartTime)
	}
	// Trust entry still upserted onto the fallback.
	resolved := wt
	if r, rerr := filepath.EvalSymlinks(wt); rerr == nil {
		resolved = r
	}
	projects, ok := got["projects"].(map[string]interface{})
	if !ok {
		t.Fatalf("projects map absent from fallback config: %v", got)
	}
	entry, ok := projects[resolved].(map[string]interface{})
	if !ok || entry["hasTrustDialogAccepted"] != true {
		t.Errorf("fallback config not trusted for %q: %v", resolved, got["projects"])
	}
}

// TestPrepareIsolatedClaudeConfigDir_DirPermissions: the config dir is created 0700
// (it holds a copy of the operator's config with userID/oauth metadata).
func TestPrepareIsolatedClaudeConfigDir_DirPermissions(t *testing.T) {
	wt := t.TempDir()
	srcDir := t.TempDir()
	srcCfg := filepath.Join(srcDir, ".claude.json")
	writeClaudeCfg(t, srcCfg, map[string]interface{}{"firstStartTime": "2026-01-01T00:00:00.000Z"})
	withIsolatedConfigSource(t, srcCfg)

	dir, err := PrepareIsolatedClaudeConfigDir(wt)
	if err != nil {
		t.Fatalf("PrepareIsolatedClaudeConfigDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("config dir perm = %o, want 0700", perm)
	}

	// Sanity: the seeded config parses as JSON.
	data, err := os.ReadFile(filepath.Join(dir, ".claude.json")) //nolint:gosec // G304: test-controlled path under a temp worktree
	if err != nil {
		t.Fatalf("read isolated config: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Errorf("isolated config is not valid JSON: %v\n%s", err, data)
	}
}
