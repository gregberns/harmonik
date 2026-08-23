//go:build integration

package keeper

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const (
	hookOldSID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	hookNewSID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

func runSessionStartHook(t *testing.T, scriptPath, project, agent, jsonPayload string) {
	t.Helper()
	cmd := exec.Command("bash", scriptPath)
	cmd.Stdin = bytes.NewReader([]byte(jsonPayload))
	cmd.Env = append(os.Environ(),
		"HARMONIK_PROJECT="+project,
		"HARMONIK_AGENT="+agent,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("sessionstart hook failed: %v\nstderr: %s", err, stderr.String())
	}
}

func TestSessionStartHook_KeeperPicksUpNewIDAcrossClear(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available; skipping SessionStart hook integration test")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available; skipping SessionStart hook integration test")
	}

	wd, err := os.Getwd() // .../internal/keeper
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	scriptPath := filepath.Join(wd, "..", "..", "scripts", "keeper-sessionstart-hook.sh")
	if _, statErr := os.Stat(scriptPath); statErr != nil {
		t.Fatalf("hook script not found at %s: %v", scriptPath, statErr)
	}

	project := t.TempDir()
	const agent = "captain"

	keeperDir := filepath.Join(project, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	gauge := `{"pct":50.0,"tokens":1000,"window_size":200000,"session_id":"` + hookOldSID + `","ts":"2026-06-16T00:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(keeperDir, agent+".ctx"), []byte(gauge), 0o600); err != nil {
		t.Fatalf("write gauge: %v", err)
	}

	runSessionStartHook(t, scriptPath, project, agent,
		`{"session_id":"`+hookOldSID+`","source":"startup","hook_event_name":"SessionStart"}`)
	if cf, _, err := ReadCtxFile(project, agent); err != nil {
		t.Fatalf("ReadCtxFile after startup: %v", err)
	} else if cf.SessionID != hookOldSID {
		t.Fatalf("after startup: want %q, got %q", hookOldSID, cf.SessionID)
	}

	runSessionStartHook(t, scriptPath, project, agent,
		`{"session_id":"`+hookNewSID+`","source":"clear","hook_event_name":"SessionStart"}`)
	cf, _, err := ReadCtxFile(project, agent)
	if err != nil {
		t.Fatalf("ReadCtxFile after clear: %v", err)
	}
	if cf.SessionID != hookNewSID {
		t.Errorf("after clear: keeper did not pick up new id from .sid; want %q, got %q (gauge still %q)",
			hookNewSID, cf.SessionID, hookOldSID)
	}

	if err := os.Remove(filepath.Join(keeperDir, agent+".sid")); err != nil {
		t.Fatalf("remove sid: %v", err)
	}
	cf, _, err = ReadCtxFile(project, agent)
	if err != nil {
		t.Fatalf("ReadCtxFile after sid removal: %v", err)
	}
	if cf.SessionID != hookOldSID {
		t.Errorf("after .sid removal: want gauge fallback %q, got %q", hookOldSID, cf.SessionID)
	}
}
