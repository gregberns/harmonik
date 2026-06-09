package daemon_test

// crewlaunchspec_test.go — unit tests for buildCrewLaunchSpec (C2 AC-5).
//
// Acceptance criterion AC-5: buildCrewLaunchSpec produces
//
//	argv = [<claude> --remote-control "<name>" --session-id <uuid>]
//
// with the caller-supplied UUID, HARMONIK_AGENT/HARMONIK_PROJECT in env,
// and NO worktree / NO --dangerously-skip-permissions.
//
// Run: go test ./internal/daemon/ -run CrewLaunchSpec
// Bead: hk-kbqto.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

func TestBuildCrewLaunchSpec_Argv(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedCrewLaunchCtx{
		ClaudeBinary: "claude",
		Name:         "alpha",
		SessionID:    "01930000-0000-7000-8000-000000000001",
		ProjectDir:   "/tmp/test-project",
	}

	spec, err := daemon.ExportedBuildCrewLaunchSpec(rc)
	if err != nil {
		t.Fatalf("buildCrewLaunchSpec: unexpected error: %v", err)
	}

	if spec.Binary != "claude" {
		t.Errorf("Binary = %q; want %q", spec.Binary, "claude")
	}

	// argv must be exactly [--remote-control <name> --session-id <uuid>]
	if len(spec.Args) != 4 {
		t.Fatalf("len(Args) = %d; want 4: got %v", len(spec.Args), spec.Args)
	}
	if spec.Args[0] != "--remote-control" {
		t.Errorf("Args[0] = %q; want --remote-control", spec.Args[0])
	}
	if spec.Args[1] != "alpha" {
		t.Errorf("Args[1] = %q; want %q (crew name)", spec.Args[1], "alpha")
	}
	if spec.Args[2] != "--session-id" {
		t.Errorf("Args[2] = %q; want --session-id", spec.Args[2])
	}
	if spec.Args[3] != rc.SessionID {
		t.Errorf("Args[3] = %q; want caller-supplied UUID %q", spec.Args[3], rc.SessionID)
	}
}

func TestBuildCrewLaunchSpec_Env(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedCrewLaunchCtx{
		Name:       "beta",
		SessionID:  "01930000-0000-7000-8000-000000000002",
		ProjectDir: "/home/user/harmonik",
	}

	spec, err := daemon.ExportedBuildCrewLaunchSpec(rc)
	if err != nil {
		t.Fatalf("buildCrewLaunchSpec: unexpected error: %v", err)
	}

	wantAgent := "HARMONIK_AGENT=beta"
	wantProject := "HARMONIK_PROJECT=/home/user/harmonik"

	hasAgent, hasProject := false, false
	for _, e := range spec.Env {
		if e == wantAgent {
			hasAgent = true
		}
		if e == wantProject {
			hasProject = true
		}
	}
	if !hasAgent {
		t.Errorf("env missing %q; got %v", wantAgent, spec.Env)
	}
	if !hasProject {
		t.Errorf("env missing %q; got %v", wantProject, spec.Env)
	}
}

func TestBuildCrewLaunchSpec_NoWorktreeNoSkipPermissions(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedCrewLaunchCtx{
		Name:       "gamma",
		SessionID:  "01930000-0000-7000-8000-000000000003",
		ProjectDir: "/tmp/harmonik-proj",
	}

	spec, err := daemon.ExportedBuildCrewLaunchSpec(rc)
	if err != nil {
		t.Fatalf("buildCrewLaunchSpec: unexpected error: %v", err)
	}

	for _, arg := range spec.Args {
		if strings.Contains(arg, "dangerously-skip-permissions") {
			t.Errorf("argv must not contain --dangerously-skip-permissions; got %v", spec.Args)
		}
		if strings.Contains(arg, "worktree") {
			t.Errorf("argv must not contain a worktree flag; got %v", spec.Args)
		}
	}
}

func TestBuildCrewLaunchSpec_DefaultBinary(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedCrewLaunchCtx{
		ClaudeBinary: "",
		Name:         "delta",
		SessionID:    "01930000-0000-7000-8000-000000000004",
		ProjectDir:   "/tmp/harmonik",
	}

	spec, err := daemon.ExportedBuildCrewLaunchSpec(rc)
	if err != nil {
		t.Fatalf("buildCrewLaunchSpec: unexpected error: %v", err)
	}
	if spec.Binary != "claude" {
		t.Errorf("Binary = %q; want %q (default)", spec.Binary, "claude")
	}
}

func TestBuildCrewLaunchSpec_CustomBinary(t *testing.T) {
	t.Parallel()

	rc := daemon.ExportedCrewLaunchCtx{
		ClaudeBinary: "/usr/local/bin/harmonik-twin-claude",
		Name:         "epsilon",
		SessionID:    "01930000-0000-7000-8000-000000000005",
		ProjectDir:   "/tmp/harmonik",
	}

	spec, err := daemon.ExportedBuildCrewLaunchSpec(rc)
	if err != nil {
		t.Fatalf("buildCrewLaunchSpec: unexpected error: %v", err)
	}
	if spec.Binary != rc.ClaudeBinary {
		t.Errorf("Binary = %q; want %q", spec.Binary, rc.ClaudeBinary)
	}
}

func TestBuildCrewLaunchSpec_ValidationErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		label string
		rc    daemon.ExportedCrewLaunchCtx
	}{
		{
			label: "empty_name",
			rc: daemon.ExportedCrewLaunchCtx{
				Name:       "",
				SessionID:  "01930000-0000-7000-8000-000000000006",
				ProjectDir: "/tmp/harmonik",
			},
		},
		{
			label: "empty_session_id",
			rc: daemon.ExportedCrewLaunchCtx{
				Name:       "zeta",
				SessionID:  "",
				ProjectDir: "/tmp/harmonik",
			},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.label, func(t *testing.T) {
			t.Parallel()
			_, err := daemon.ExportedBuildCrewLaunchSpec(c.rc)
			if err == nil {
				t.Errorf("buildCrewLaunchSpec(%s): expected error, got nil", c.label)
			}
		})
	}
}

// TestBuildCrewLaunchSpec_ResumePath verifies that Resume=true produces
// --resume <uuid> instead of --session-id <uuid> (c2-spec.md §7 stale re-launch).
//
// AC-5 extension: resume argv must be [--remote-control <name> --resume <uuid>].
// Bead ref: hk-4z0gp.
func TestBuildCrewLaunchSpec_ResumePath(t *testing.T) {
	t.Parallel()

	const uuid = "01930000-0000-7000-8000-000000000099"
	rc := daemon.ExportedCrewLaunchCtx{
		Name:      "resume-crew",
		SessionID: uuid,
		ProjectDir: "/tmp/harmonik",
		Resume:    true,
	}

	spec, err := daemon.ExportedBuildCrewLaunchSpec(rc)
	if err != nil {
		t.Fatalf("buildCrewLaunchSpec(resume): unexpected error: %v", err)
	}

	if len(spec.Args) != 4 {
		t.Fatalf("len(Args) = %d; want 4: got %v", len(spec.Args), spec.Args)
	}
	if spec.Args[2] != "--resume" {
		t.Errorf("Args[2] = %q; want --resume (resume path)", spec.Args[2])
	}
	if spec.Args[3] != uuid {
		t.Errorf("Args[3] = %q; want %q (session UUID)", spec.Args[3], uuid)
	}
}

// TestBuildCrewLaunchSpec_WorkDir verifies WorkDir is set to projectDir so the
// crew session starts at the project root.
//
// Bead ref: hk-4z0gp.
func TestBuildCrewLaunchSpec_WorkDir(t *testing.T) {
	t.Parallel()

	const projDir = "/home/user/my-project"
	rc := daemon.ExportedCrewLaunchCtx{
		Name:       "eta",
		SessionID:  "01930000-0000-7000-8000-000000000007",
		ProjectDir: projDir,
	}

	spec, err := daemon.ExportedBuildCrewLaunchSpec(rc)
	if err != nil {
		t.Fatalf("buildCrewLaunchSpec: unexpected error: %v", err)
	}
	if spec.WorkDir != projDir {
		t.Errorf("WorkDir = %q; want %q (project root)", spec.WorkDir, projDir)
	}
	if spec.Role != "crew" {
		t.Errorf("Role = %q; want %q", spec.Role, "crew")
	}
}
