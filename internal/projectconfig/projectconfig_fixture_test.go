package projectconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func projCfgFixtureDir(t *testing.T, yamlContent string) string {
	t.Helper()
	root := t.TempDir()
	harmonikDir := filepath.Join(root, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o750); err != nil {
		t.Fatalf("projCfgFixtureDir: MkdirAll: %v", err)
	}
	if yamlContent != "" {
		cfgPath := filepath.Join(harmonikDir, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o600); err != nil {
			t.Fatalf("projCfgFixtureDir: WriteFile: %v", err)
		}
	}
	return root
}
