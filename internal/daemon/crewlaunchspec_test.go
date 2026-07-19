package daemon_test

// crewlaunchspec_test.go — unit tests for buildCrewLaunchSpec (C2 AC-5).
//
// Acceptance criterion AC-5: buildCrewLaunchSpec produces
//
//	argv = [<claude> --dangerously-skip-permissions --remote-control "<name>" --session-id <uuid>]
//
// with the caller-supplied UUID, HARMONIK_AGENT/HARMONIK_PROJECT in env,
// --dangerously-skip-permissions present (hk-672di), and NO worktree.
//
// Run: go test ./internal/daemon/ -run CrewLaunchSpec
// Bead: hk-kbqto.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// argvHasFlagValue reports whether args contains the pair [flag, value] adjacent
// (flag immediately followed by value).
func argvHasFlagValue(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

// argvHasFlag reports whether args contains flag anywhere.
func argvHasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// TestBuildCrewLaunchSpec_ModelInjection verifies the optional per-crew model:
// front-matter field (hk-9j3z): a non-empty Model appends [--model <alias>] to
// argv on BOTH the fresh-session and the --resume branch, and an empty Model
// appends no --model flag (the crew inherits the compiled default).
func TestBuildCrewLaunchSpec_ModelInjection(t *testing.T) {
	t.Parallel()

	const uuid = "01930000-0000-7000-8000-0000000000a1"

	cases := []struct {
		label  string
		model  string
		resume bool
		want   bool // expect --model present
	}{
		{label: "fresh_opus", model: "opus", resume: false, want: true},
		{label: "fresh_sonnet", model: "sonnet", resume: false, want: true},
		{label: "resume_opus", model: "opus", resume: true, want: true},
		{label: "fresh_empty", model: "", resume: false, want: false},
		{label: "resume_empty", model: "", resume: true, want: false},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Parallel()
			rc := daemon.ExportedCrewLaunchCtx{
				Name:       "modeled-crew",
				SessionID:  uuid,
				ProjectDir: "/tmp/harmonik",
				Resume:     c.resume,
				Model:      c.model,
			}
			spec, err := daemon.ExportedBuildCrewLaunchSpec(rc)
			if err != nil {
				t.Fatalf("buildCrewLaunchSpec(%s): unexpected error: %v", c.label, err)
			}

			hasFlag := argvHasFlag(spec.Args, "--model")
			if hasFlag != c.want {
				t.Fatalf("%s: --model present=%v; want %v (argv=%v)", c.label, hasFlag, c.want, spec.Args)
			}
			if c.want && !argvHasFlagValue(spec.Args, "--model", c.model) {
				t.Errorf("%s: argv missing [--model %s]; got %v", c.label, c.model, spec.Args)
			}
			// The model flag must follow the session/resume flag (the base argv
			// is unchanged), so the first five elements are untouched.
			if spec.Args[1] != "--remote-control" {
				t.Errorf("%s: base argv mutated; Args[1]=%q want --remote-control", c.label, spec.Args[1])
			}
		})
	}
}

// TestReadMissionModel verifies the mission front-matter model: parse (hk-9j3z):
// a present model: field is returned; absence, a missing file, an empty path, or
// no front-matter block all degrade to "".
func TestReadMissionModel(t *testing.T) {
	t.Parallel()

	const withModel = `---
schema_version: 1
crew_name: alpha
queue: alpha-q
epic_id: hk-tigaf
goal: "Ship named-queues"
captain_name: captain
model: opus
---

# Mission: Ship named-queues
body text here
`
	const withoutModel = `---
schema_version: 1
crew_name: beta
queue: beta-q
epic_id: hk-xyz
goal: "drain a lane"
captain_name: captain
---

# Mission
`
	const noFrontMatter = "# Mission\n\njust prose, no front-matter\n"

	cases := []struct {
		label   string
		content string
		want    string
	}{
		{label: "present", content: withModel, want: "opus"},
		{label: "absent", content: withoutModel, want: ""},
		{label: "no_frontmatter", content: noFrontMatter, want: ""},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			p := filepath.Join(dir, "mission.md")
			if err := os.WriteFile(p, []byte(c.content), 0o600); err != nil {
				t.Fatalf("write mission: %v", err)
			}
			if got := daemon.ExportedReadMissionModel(p); got != c.want {
				t.Errorf("readMissionModel(%s) = %q; want %q", c.label, got, c.want)
			}
		})
	}

	// Empty path and missing file both return "".
	if got := daemon.ExportedReadMissionModel(""); got != "" {
		t.Errorf("readMissionModel(\"\") = %q; want \"\"", got)
	}
	if got := daemon.ExportedReadMissionModel(filepath.Join(t.TempDir(), "nope.md")); got != "" {
		t.Errorf("readMissionModel(missing) = %q; want \"\"", got)
	}
}

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

	// argv must be exactly [--dangerously-skip-permissions --remote-control <name> --session-id <uuid>]
	if len(spec.Args) != 5 {
		t.Fatalf("len(Args) = %d; want 5: got %v", len(spec.Args), spec.Args)
	}
	if spec.Args[0] != "--dangerously-skip-permissions" {
		t.Errorf("Args[0] = %q; want --dangerously-skip-permissions", spec.Args[0])
	}
	if spec.Args[1] != "--remote-control" {
		t.Errorf("Args[1] = %q; want --remote-control", spec.Args[1])
	}
	if spec.Args[2] != "alpha" {
		t.Errorf("Args[2] = %q; want %q (crew name)", spec.Args[2], "alpha")
	}
	if spec.Args[3] != "--session-id" {
		t.Errorf("Args[3] = %q; want --session-id", spec.Args[3])
	}
	if spec.Args[4] != rc.SessionID {
		t.Errorf("Args[4] = %q; want caller-supplied UUID %q", spec.Args[4], rc.SessionID)
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

// TestBuildCrewLaunchSpec_SkipPermissionsNoWorktree verifies that
// --dangerously-skip-permissions IS present (hk-672di: crew sessions must not
// wedge on mid-loop permission prompts) and no worktree flag appears.
func TestBuildCrewLaunchSpec_SkipPermissionsNoWorktree(t *testing.T) {
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

	hasSkipPerms := false
	for _, arg := range spec.Args {
		if strings.Contains(arg, "dangerously-skip-permissions") {
			hasSkipPerms = true
		}
		if strings.Contains(arg, "worktree") {
			t.Errorf("argv must not contain a worktree flag; got %v", spec.Args)
		}
	}
	if !hasSkipPerms {
		t.Errorf("argv must contain --dangerously-skip-permissions; got %v", spec.Args)
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
		Name:       "resume-crew",
		SessionID:  uuid,
		ProjectDir: "/tmp/harmonik",
		Resume:     true,
	}

	spec, err := daemon.ExportedBuildCrewLaunchSpec(rc)
	if err != nil {
		t.Fatalf("buildCrewLaunchSpec(resume): unexpected error: %v", err)
	}

	if len(spec.Args) != 5 {
		t.Fatalf("len(Args) = %d; want 5: got %v", len(spec.Args), spec.Args)
	}
	if spec.Args[0] != "--dangerously-skip-permissions" {
		t.Errorf("Args[0] = %q; want --dangerously-skip-permissions", spec.Args[0])
	}
	if spec.Args[3] != "--resume" {
		t.Errorf("Args[3] = %q; want --resume (resume path)", spec.Args[3])
	}
	if spec.Args[4] != uuid {
		t.Errorf("Args[4] = %q; want %q (session UUID)", spec.Args[4], uuid)
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

// argvFlagValue returns the value immediately following flag in args, or "" if
// the flag is absent or has no following element.
func argvFlagValue(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

// TestJoinRemoteControlName covers the single source of the --remote-control
// label format (hk-igpg): an empty prefix yields the BARE name (backward
// compatible), a non-empty prefix yields "<prefix>-<name>".
func TestJoinRemoteControlName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		label  string
		prefix string
		name   string
		want   string
	}{
		{label: "empty_prefix_bare", prefix: "", name: "paul", want: "paul"},
		{label: "empty_prefix_captain", prefix: "", name: "captain", want: "captain"},
		{label: "prefixed", prefix: "hk", name: "paul", want: "hk-paul"},
		{label: "prefixed_captain", prefix: "hk", name: "captain", want: "hk-captain"},
		{label: "longer_prefix", prefix: "mproj", name: "chani", want: "mproj-chani"},
		{label: "empty_name", prefix: "hk", name: "", want: "hk-"},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Parallel()
			if got := daemon.JoinRemoteControlName(c.prefix, c.name); got != c.want {
				t.Errorf("JoinRemoteControlName(%q, %q) = %q; want %q", c.prefix, c.name, got, c.want)
			}
		})
	}
}

// TestBuildCrewLaunchSpec_RcPrefix verifies the per-project --remote-control
// label prefix (hk-igpg):
//   - empty RcPrefix emits the UNCHANGED bare-name arg (backward compatible);
//   - a non-empty RcPrefix emits "<prefix>-<name>" as the --remote-control value;
//   - the --resume and --session-id branches emit IDENTICAL labels for a given
//     prefix (resume parity — a keeper clear→resume must not rename the picker
//     session). HARMONIK_AGENT stays BARE in every case.
func TestBuildCrewLaunchSpec_RcPrefix(t *testing.T) {
	t.Parallel()

	const uuid = "01930000-0000-7000-8000-0000000000b2"

	t.Run("empty_prefix_unchanged", func(t *testing.T) {
		t.Parallel()
		for _, resume := range []bool{false, true} {
			rc := daemon.ExportedCrewLaunchCtx{
				Name:       "paul",
				RcPrefix:   "",
				SessionID:  uuid,
				ProjectDir: "/tmp/harmonik",
				Resume:     resume,
			}
			spec, err := daemon.ExportedBuildCrewLaunchSpec(rc)
			if err != nil {
				t.Fatalf("resume=%v: unexpected error: %v", resume, err)
			}
			if got := argvFlagValue(spec.Args, "--remote-control"); got != "paul" {
				t.Errorf("resume=%v: --remote-control = %q; want bare %q", resume, got, "paul")
			}
		}
	})

	t.Run("prefixed_resume_parity", func(t *testing.T) {
		t.Parallel()
		base := daemon.ExportedCrewLaunchCtx{
			Name:       "paul",
			RcPrefix:   "hk",
			SessionID:  uuid,
			ProjectDir: "/tmp/harmonik",
		}

		fresh := base
		fresh.Resume = false
		resumed := base
		resumed.Resume = true

		specFresh, err := daemon.ExportedBuildCrewLaunchSpec(fresh)
		if err != nil {
			t.Fatalf("fresh: unexpected error: %v", err)
		}
		specResume, err := daemon.ExportedBuildCrewLaunchSpec(resumed)
		if err != nil {
			t.Fatalf("resume: unexpected error: %v", err)
		}

		labelFresh := argvFlagValue(specFresh.Args, "--remote-control")
		labelResume := argvFlagValue(specResume.Args, "--remote-control")

		if labelFresh != "hk-paul" {
			t.Errorf("fresh --remote-control = %q; want %q", labelFresh, "hk-paul")
		}
		if labelFresh != labelResume {
			t.Errorf("resume parity broken: fresh label %q != resume label %q", labelFresh, labelResume)
		}

		// HARMONIK_AGENT must remain BARE (no prefix) on both branches.
		for _, spec := range []struct {
			label string
			env   []string
		}{{"fresh", specFresh.Env}, {"resume", specResume.Env}} {
			bare := false
			for _, e := range spec.env {
				if e == "HARMONIK_AGENT=paul" {
					bare = true
				}
				if e == "HARMONIK_AGENT=hk-paul" {
					t.Errorf("%s: HARMONIK_AGENT was prefixed (=hk-paul); it must stay bare", spec.label)
				}
			}
			if !bare {
				t.Errorf("%s: env missing bare HARMONIK_AGENT=paul; got %v", spec.label, spec.env)
			}
		}
	})
}

// TestResolveCrewHarness_Precedence covers the crew-scoped harness resolver
// precedence walk (hk-l63b9): flag > mission front-matter > per-crew config >
// default "claude". This is a SEPARATE resolver from the worker per-bead
// resolveHarness (harnessresolve.go) — a crew has no bead.
func TestResolveCrewHarness_Precedence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		label   string
		flag    string
		mission string
		config  string
		want    string
	}{
		{label: "all_absent_defaults_claude", flag: "", mission: "", config: "", want: "claude"},
		{label: "config_only", flag: "", mission: "", config: "codex", want: "codex"},
		{label: "mission_beats_config", flag: "", mission: "pi", config: "codex", want: "pi"},
		{label: "flag_beats_mission_and_config", flag: "codex", mission: "pi", config: "pi", want: "codex"},
		{label: "flag_only", flag: "codex", mission: "", config: "", want: "codex"},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Parallel()
			got := daemon.ExportedResolveCrewHarness(c.flag, c.mission, c.config)
			if got != c.want {
				t.Errorf("resolveCrewHarness(%q, %q, %q) = %q; want %q",
					c.flag, c.mission, c.config, got, c.want)
			}
		})
	}
}

// TestReadMissionHarness verifies the mission front-matter harness: parse
// (hk-l63b9): a present harness: field is returned; absence, a missing file,
// an empty path, or no front-matter block all degrade to "".
func TestReadMissionHarness(t *testing.T) {
	t.Parallel()

	const withHarness = `---
schema_version: 1
crew_name: alpha
queue: alpha-q
epic_id: hk-tigaf
goal: "Ship named-queues"
captain_name: captain
harness: codex
---

# Mission: Ship named-queues
body text here
`
	const withoutHarness = `---
schema_version: 1
crew_name: beta
queue: beta-q
epic_id: hk-xyz
goal: "drain a lane"
captain_name: captain
---

# Mission
`
	const noFrontMatter = "# Mission\n\njust prose, no front-matter\n"

	cases := []struct {
		label   string
		content string
		want    string
	}{
		{label: "present", content: withHarness, want: "codex"},
		{label: "absent", content: withoutHarness, want: ""},
		{label: "no_frontmatter", content: noFrontMatter, want: ""},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			p := filepath.Join(dir, "mission.md")
			if err := os.WriteFile(p, []byte(c.content), 0o600); err != nil {
				t.Fatalf("write mission: %v", err)
			}
			if got := daemon.ExportedReadMissionHarness(p); got != c.want {
				t.Errorf("readMissionHarness(%s) = %q; want %q", c.label, got, c.want)
			}
		})
	}

	if got := daemon.ExportedReadMissionHarness(""); got != "" {
		t.Errorf("readMissionHarness(\"\") = %q; want \"\"", got)
	}
	if got := daemon.ExportedReadMissionHarness(filepath.Join(t.TempDir(), "nope.md")); got != "" {
		t.Errorf("readMissionHarness(missing) = %q; want \"\"", got)
	}
}

// TestBuildCrewLaunchSpec_HarnessBranch covers the hk-l63b9 spec-builder branch:
// "" and "claude" both build today's Claude spec unchanged (regression
// coverage on the existing Claude path); any other harness is rejected with an
// explicit "not yet supported" error, never a silent Claude fallback.
func TestBuildCrewLaunchSpec_HarnessBranch(t *testing.T) {
	t.Parallel()

	baseRC := daemon.ExportedCrewLaunchCtx{
		ClaudeBinary: "claude",
		Name:         "alpha",
		SessionID:    "01930000-0000-7000-8000-000000000099",
		ProjectDir:   "/tmp/test-project",
	}

	t.Run("empty_harness_builds_claude_spec", func(t *testing.T) {
		t.Parallel()
		rc := baseRC
		rc.Harness = ""
		spec, err := daemon.ExportedBuildCrewLaunchSpec(rc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if spec.Binary != "claude" {
			t.Errorf("Binary = %q; want claude", spec.Binary)
		}
	})

	t.Run("explicit_claude_builds_claude_spec", func(t *testing.T) {
		t.Parallel()
		rc := baseRC
		rc.Harness = "claude"
		spec, err := daemon.ExportedBuildCrewLaunchSpec(rc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if spec.Binary != "claude" {
			t.Errorf("Binary = %q; want claude", spec.Binary)
		}
	})

	for _, unsupported := range []string{"codex", "pi", "bogus"} {
		t.Run("unsupported_"+unsupported, func(t *testing.T) {
			t.Parallel()
			rc := baseRC
			rc.Harness = unsupported
			_, err := daemon.ExportedBuildCrewLaunchSpec(rc)
			if err == nil {
				t.Fatalf("expected error for harness %q, got nil", unsupported)
			}
			if !strings.Contains(err.Error(), unsupported) {
				t.Errorf("error %q does not name the unsupported harness %q", err.Error(), unsupported)
			}
			if !strings.Contains(err.Error(), "not yet supported") {
				t.Errorf("error %q does not read as an explicit not-yet-supported rejection", err.Error())
			}
		})
	}
}
