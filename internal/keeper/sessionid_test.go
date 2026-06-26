package keeper_test

// sessionid_test.go — unit tests for the single-writer session-id channel
// (hk-8prq). The keeper reads <agent>.sid as its PRIMARY identity source and
// falls back to the gauge's session_id when .sid is absent or malformed.
//
// These tests drive the behavior through the real read path keeper.ReadCtxFile —
// the same function the watcher loop and the cycler's waitForNewSessionID use —
// so they exercise the production override, not a test-only shim. They FAIL on
// main (ReadCtxFile ignores .sid there); the GREEN implementation makes them
// pass.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/keeper"
)

// writeGauge writes a minimal <agent>.ctx with the given session_id.
func writeGauge(t *testing.T, projectDir, agent, sid string) {
	t.Helper()
	dir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir keeper dir: %v", err)
	}
	body := `{"pct":42.0,"tokens":1000,"window_size":200000,"session_id":"` + sid + `","ts":"2026-06-16T00:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, agent+".ctx"), []byte(body), 0o644); err != nil {
		t.Fatalf("write ctx: %v", err)
	}
}

// writeSidFile writes <agent>.sid directly (modeling what the SessionStart hook
// produces). The path is hardcoded so this test compiles on main and FAILS only
// on the assertion — a clean RED.
func writeSidFile(t *testing.T, projectDir, agent, sid string) {
	t.Helper()
	dir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir keeper dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, agent+".sid"), []byte(sid+"\n"), 0o644); err != nil {
		t.Fatalf("write sid: %v", err)
	}
}

const (
	gaugeSID   = "11111111-1111-4111-8111-111111111111" // valid UUIDv4
	primarySID = "22222222-2222-4222-8222-222222222222" // valid UUIDv4
)

// TestReadCtxFile_SidChannelIsPrimary: a present, well-formed .sid OVERRIDES the
// gauge's session_id (the root-cause fix for the multi-writer .ctx ambiguity).
func TestReadCtxFile_SidChannelIsPrimary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGauge(t, dir, "captain", gaugeSID)
	writeSidFile(t, dir, "captain", primarySID)

	cf, _, err := keeper.ReadCtxFile(dir, "captain")
	if err != nil {
		t.Fatalf("ReadCtxFile: %v", err)
	}
	if cf.SessionID != primarySID {
		t.Errorf("SessionID: want primary %q (from .sid), got %q", primarySID, cf.SessionID)
	}
}

// TestReadCtxFile_FallsBackWhenSidAbsent: with no .sid the keeper uses the
// gauge's session_id as a fallback. The watcher no longer writes that fallback
// into .managed on first sighting.
func TestReadCtxFile_FallsBackWhenSidAbsent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGauge(t, dir, "captain", gaugeSID)

	cf, _, err := keeper.ReadCtxFile(dir, "captain")
	if err != nil {
		t.Fatalf("ReadCtxFile: %v", err)
	}
	if cf.SessionID != gaugeSID {
		t.Errorf("SessionID: want gauge fallback %q, got %q", gaugeSID, cf.SessionID)
	}
}

// TestReadCtxFile_FallsBackWhenSidMalformed: a non-UUID / UUIDv7 / empty .sid is
// NOT trusted as primary; the keeper falls back to the gauge id rather than
// binding a worse identity than the fallback would.
func TestReadCtxFile_FallsBackWhenSidMalformed(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"empty":      "",
		"not-a-uuid": "garbage",
		"uuidv7":     "33333333-3333-7333-8333-333333333333", // version 7 → daemon implementer
	}
	for name, badSID := range cases {
		badSID := badSID
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeGauge(t, dir, "captain", gaugeSID)
			writeSidFile(t, dir, "captain", badSID)

			cf, _, err := keeper.ReadCtxFile(dir, "captain")
			if err != nil {
				t.Fatalf("ReadCtxFile: %v", err)
			}
			if cf.SessionID != gaugeSID {
				t.Errorf("malformed .sid %q: want gauge fallback %q, got %q", badSID, gaugeSID, cf.SessionID)
			}
		})
	}
}

// TestReadSessionIDFile_LowercasesAndTrims: the exported reader normalises the
// channel value (lowercase, trimmed) so the watcher's identity comparisons are
// stable regardless of the on-disk byte form.
func TestReadSessionIDFile_LowercasesAndTrims(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keeperDir := filepath.Join(dir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Trailing newline + surrounding whitespace; the value itself is lowercase v4.
	if err := os.WriteFile(filepath.Join(keeperDir, "captain.sid"), []byte("  "+primarySID+"  \n"), 0o644); err != nil {
		t.Fatalf("write sid: %v", err)
	}
	got, _, err := keeper.ReadSessionIDFile(dir, "captain")
	if err != nil {
		t.Fatalf("ReadSessionIDFile: %v", err)
	}
	if got != primarySID {
		t.Errorf("ReadSessionIDFile: want %q, got %q", primarySID, got)
	}
}
