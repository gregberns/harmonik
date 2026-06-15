package main

// init_agents_sidecar_test.go — tests for the existing-AGENTS.md path of
// renderAgentsMD.
//
// When a project already has its own AGENTS.md, `harmonik init` must NOT overwrite
// it (without --force), but it also must not leave the project with zero harmonik
// operating instructions. Instead it renders the harmonik AGENTS template to the
// reviewable sidecar .harmonik/AGENTS.harmonik.md for the operator to merge in.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderAgentsMD_ExistingAgentsMD_WritesSidecar verifies that when AGENTS.md
// already exists and force is false, the real AGENTS.md is left untouched and the
// harmonik instructions are written to the .harmonik/AGENTS.harmonik.md sidecar
// with substituted content.
func TestRenderAgentsMD_ExistingAgentsMD_WritesSidecar(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik"), 0o755); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}

	// Pre-existing, project-owned AGENTS.md whose content we must preserve verbatim.
	const existing = "# My Project AGENTS\n\nDo not clobber me.\n"
	agentsPath := filepath.Join(projectDir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte(existing), 0o644); err != nil {
		t.Fatalf("seed AGENTS.md: %v", err)
	}

	const targetBranch = "integration"
	var stdout, stderr bytes.Buffer
	code := renderAgentsMD(projectDir, targetBranch, false, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("renderAgentsMD returned %d (want 0); stderr=%q", code, stderr.String())
	}

	// The real AGENTS.md must be byte-for-byte untouched.
	got, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if string(got) != existing {
		t.Errorf("AGENTS.md was modified.\n got: %q\nwant: %q", string(got), existing)
	}

	// The sidecar must exist with substituted harmonik content.
	sidecarPath := filepath.Join(projectDir, ".harmonik", "AGENTS.harmonik.md")
	sidecar, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("sidecar .harmonik/AGENTS.harmonik.md not written: %v", err)
	}
	sidecarStr := string(sidecar)

	if strings.Contains(sidecarStr, "$PROJECT_DIR") {
		t.Errorf("sidecar contains unreplaced $PROJECT_DIR placeholder")
	}
	if strings.Contains(sidecarStr, "$TARGET_BRANCH") {
		t.Errorf("sidecar contains unreplaced $TARGET_BRANCH placeholder")
	}
	if !strings.Contains(sidecarStr, projectDir) {
		t.Errorf("sidecar does not contain substituted project dir %q", projectDir)
	}
	if !strings.Contains(sidecarStr, targetBranch) {
		t.Errorf("sidecar does not contain substituted target branch %q", targetBranch)
	}

	// The operator-facing message must point at the sidecar.
	if !strings.Contains(stdout.String(), "AGENTS.harmonik.md") {
		t.Errorf("stdout does not mention the sidecar path; got: %q", stdout.String())
	}
}

// TestRenderAgentsMD_ExistingAgentsMD_SidecarIdempotent verifies that the
// existing-AGENTS.md sidecar write is idempotent: a second run does not overwrite
// an already-present sidecar unless force is set, and force refreshes it.
func TestRenderAgentsMD_ExistingAgentsMD_SidecarIdempotent(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik"), 0o755); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "AGENTS.md"), []byte("existing\n"), 0o644); err != nil {
		t.Fatalf("seed AGENTS.md: %v", err)
	}

	// First run writes the sidecar (AGENTS.md exists, no force).
	var stdout, stderr bytes.Buffer
	if code := renderAgentsMD(projectDir, "main", false, &stdout, &stderr); code != 0 {
		t.Fatalf("renderAgentsMD (run 1) returned %d (want 0); stderr=%q", code, stderr.String())
	}
	sidecarPath := filepath.Join(projectDir, ".harmonik", "AGENTS.harmonik.md")
	first, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("sidecar not written on run 1: %v", err)
	}

	// Mutate the sidecar to a sentinel, then re-run without force: must be skipped.
	const sentinel = "SENTINEL — do not overwrite\n"
	if err := os.WriteFile(sidecarPath, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("mutate sidecar: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := renderAgentsMD(projectDir, "main", false, &stdout, &stderr); code != 0 {
		t.Fatalf("renderAgentsMD (run 2, no force) returned %d (want 0); stderr=%q", code, stderr.String())
	}
	got, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("read sidecar after run 2: %v", err)
	}
	if string(got) != sentinel {
		t.Errorf("sidecar was overwritten without --force (not idempotent).\n got: %q\nwant: %q", string(got), sentinel)
	}

	// Re-run the sidecar write with force via the helper directly: must refresh.
	stdout.Reset()
	stderr.Reset()
	if code := writeAgentsHarmonikSidecar(projectDir, string(first), true, &stdout, &stderr); code != 0 {
		t.Fatalf("writeAgentsHarmonikSidecar(force) returned %d (want 0); stderr=%q", code, stderr.String())
	}
	refreshed, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("read sidecar after force refresh: %v", err)
	}
	if string(refreshed) == sentinel {
		t.Errorf("sidecar was not refreshed with --force (still the sentinel)")
	}
	if string(refreshed) != string(first) {
		t.Errorf("force-refreshed sidecar content does not match the rendered template")
	}
}
