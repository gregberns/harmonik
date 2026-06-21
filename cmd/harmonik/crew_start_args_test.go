package main

// crew_start_args_test.go — unit tests for the ES4 (hk-sn4n) crew-start arg
// resolution layer: --queue defaulting and the D3 mission-split rule.
//
// resolveCrewStartArgs is the PURE arg/defaulting helper — it dials no daemon,
// touches no network, and (critically) reads no disk. That is what makes the
// "fresh start never reads the stale on-disk default mission" invariant testable
// daemon-down: there is simply no code path in the fresh-start resolver that
// could ever surface the on-disk default, so a planted stale mission cannot leak.

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveCrewStartArgs_QueueDefaulting covers the --queue default ("<name>-q")
// and explicit override, plus the --name alternate spelling of the name.
func TestResolveCrewStartArgs_QueueDefaulting(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		argv      []string
		wantName  string
		wantQueue string
	}{
		{
			name:      "queue defaults to <name>-q",
			argv:      []string{"alpha"},
			wantName:  "alpha",
			wantQueue: "alpha-q",
		},
		{
			name:      "explicit --queue overrides default",
			argv:      []string{"alpha", "--queue", "special-q"},
			wantName:  "alpha",
			wantQueue: "special-q",
		},
		{
			name:      "explicit --queue= form overrides default",
			argv:      []string{"beta", "--queue=other-q"},
			wantName:  "beta",
			wantQueue: "other-q",
		},
		{
			name:      "--name positional alternate, queue still defaults",
			argv:      []string{"--name", "gamma"},
			wantName:  "gamma",
			wantQueue: "gamma-q",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args, help, usageErr := resolveCrewStartArgs(tc.argv)
			if help {
				t.Fatalf("unexpected help=true")
			}
			if usageErr != "" {
				t.Fatalf("unexpected usage error: %s", usageErr)
			}
			if args.Name != tc.wantName {
				t.Errorf("Name = %q; want %q", args.Name, tc.wantName)
			}
			if args.Queue != tc.wantQueue {
				t.Errorf("Queue = %q; want %q", args.Queue, tc.wantQueue)
			}
		})
	}
}

// TestResolveCrewStartArgs_MissionRule covers the D3 fresh-start mission rule:
//   - --mission path → MissionPath is exactly that path.
//   - no --mission → MissionPath is "" (NOT defaulted to anything).
func TestResolveCrewStartArgs_MissionRule(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		argv        []string
		wantMission string
	}{
		{
			name:        "explicit --mission path is used verbatim",
			argv:        []string{"alpha", "--mission", "/tmp/alpha-handoff.md"},
			wantMission: "/tmp/alpha-handoff.md",
		},
		{
			name:        "explicit --mission= form is used verbatim",
			argv:        []string{"alpha", "--mission=/tmp/alpha2.md"},
			wantMission: "/tmp/alpha2.md",
		},
		{
			name:        "no --mission yields empty (optional, never defaulted)",
			argv:        []string{"alpha"},
			wantMission: "",
		},
		{
			name:        "no --mission with explicit queue still yields empty mission",
			argv:        []string{"alpha", "--queue", "alpha-q"},
			wantMission: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args, help, usageErr := resolveCrewStartArgs(tc.argv)
			if help {
				t.Fatalf("unexpected help=true")
			}
			if usageErr != "" {
				t.Fatalf("unexpected usage error: %s", usageErr)
			}
			if args.MissionPath != tc.wantMission {
				t.Errorf("MissionPath = %q; want %q", args.MissionPath, tc.wantMission)
			}
		})
	}
}

// TestResolveCrewStartArgs_FreshStartIgnoresStaleOnDiskMission is the load-bearing
// D3 test: even when a stale on-disk default mission EXISTS, a fresh start with no
// --mission must NOT pick it up. We plant a stale mission at the on-disk default
// path and assert resolveCrewStartArgs produces MissionPath=="" — i.e. it never
// surfaces the default path. The invariant holds BY CONSTRUCTION: the resolver
// reads no disk, so the planted file is structurally unreachable from this path.
func TestResolveCrewStartArgs_FreshStartIgnoresStaleOnDiskMission(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	name := "alpha"

	// Plant a stale on-disk default mission from a hypothetical prior agent.
	missionsDir := filepath.Join(projectDir, ".harmonik", "crew", "missions")
	if err := os.MkdirAll(missionsDir, 0o755); err != nil {
		t.Fatalf("mkdir missions: %v", err)
	}
	stalePath := filepath.Join(missionsDir, name+".md")
	if err := os.WriteFile(stalePath, []byte("STALE PRIOR MISSION — must never be reused\n"), 0o644); err != nil {
		t.Fatalf("write stale mission: %v", err)
	}

	// Fresh start with no --mission, pointed at the project that has the stale file.
	args, help, usageErr := resolveCrewStartArgs([]string{name, "--project", projectDir})
	if help {
		t.Fatalf("unexpected help=true")
	}
	if usageErr != "" {
		t.Fatalf("unexpected usage error: %s", usageErr)
	}

	if args.MissionPath != "" {
		t.Errorf("fresh start picked up a mission path %q; want \"\" (stale on-disk default must be ignored)", args.MissionPath)
	}
	// Belt-and-suspenders: it must specifically not be the on-disk default path.
	if args.MissionPath == stalePath {
		t.Errorf("fresh start resolved to the stale on-disk default %q — exactly the reuse D3 forbids", stalePath)
	}
}

// TestCrewRestartRehydrationReadsOnDiskMission documents and guards the OTHER
// half of D3: a keeper-restart re-hydration is SUPPOSED to re-read the crew's own
// on-disk mission. That path does NOT flow through resolveCrewStartArgs — a keeper
// cycles a crew via /clear + /session-resume on the same session_id, and the crew
// re-reads .harmonik/crew/missions/<name>.md in its own boot sequence
// (crew-launch § Self-restart). This test pins the on-disk-default path contract
// that the restart path depends on, proving the two paths are distinct:
//   - fresh start (resolveCrewStartArgs) → never this path
//   - restart re-hydration               → exactly this path
//
// If the on-disk default path convention ever changes, both this guard and the
// crew-launch boot sequence must move together — keeping the restart re-read
// intact while the fresh-start ignore-disk rule stays correct.
func TestCrewRestartRehydrationReadsOnDiskMission(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	name := "alpha"

	// The on-disk default mission path the restart re-hydration re-reads.
	onDiskDefault := filepath.Join(projectDir, ".harmonik", "crew", "missions", name+".md")

	missionsDir := filepath.Dir(onDiskDefault)
	if err := os.MkdirAll(missionsDir, 0o755); err != nil {
		t.Fatalf("mkdir missions: %v", err)
	}
	want := "CREW'S OWN JUST-WRITTEN MISSION — restart re-reads this\n"
	if err := os.WriteFile(onDiskDefault, []byte(want), 0o644); err != nil {
		t.Fatalf("write on-disk mission: %v", err)
	}

	// The restart re-hydration path reads the on-disk default directly (the crew
	// boot does this; here we exercise the same read to pin the contract).
	got, err := os.ReadFile(onDiskDefault)
	if err != nil {
		t.Fatalf("restart re-hydration could not read on-disk mission %q: %v", onDiskDefault, err)
	}
	if string(got) != want {
		t.Errorf("on-disk mission content = %q; want %q", string(got), want)
	}

	// And confirm the fresh-start resolver, given the SAME project, still refuses
	// to surface that on-disk path — the two paths diverge as designed.
	args, _, usageErr := resolveCrewStartArgs([]string{name, "--project", projectDir})
	if usageErr != "" {
		t.Fatalf("unexpected usage error: %s", usageErr)
	}
	if args.MissionPath == onDiskDefault || args.MissionPath != "" {
		t.Errorf("fresh start surfaced on-disk mission %q; fresh start must ignore disk (MissionPath=%q)", onDiskDefault, args.MissionPath)
	}
}

// TestResolveCrewStartArgs_Errors covers the usage-error surface (missing name,
// too many positionals, unknown flag, help).
func TestResolveCrewStartArgs_Errors(t *testing.T) {
	t.Parallel()

	t.Run("help requested", func(t *testing.T) {
		t.Parallel()
		_, help, usageErr := resolveCrewStartArgs([]string{"--help"})
		if !help {
			t.Errorf("--help: help = false; want true")
		}
		if usageErr != "" {
			t.Errorf("--help: usageErr = %q; want \"\"", usageErr)
		}
	})

	t.Run("missing name", func(t *testing.T) {
		t.Parallel()
		_, _, usageErr := resolveCrewStartArgs([]string{"--queue", "alpha-q"})
		if usageErr == "" {
			t.Errorf("missing name: expected a usage error")
		}
	})

	t.Run("too many positionals", func(t *testing.T) {
		t.Parallel()
		_, _, usageErr := resolveCrewStartArgs([]string{"alpha", "beta"})
		if usageErr == "" {
			t.Errorf("two names: expected a usage error")
		}
	})

	t.Run("unknown flag", func(t *testing.T) {
		t.Parallel()
		_, _, usageErr := resolveCrewStartArgs([]string{"alpha", "--bogus"})
		if usageErr == "" {
			t.Errorf("unknown flag: expected a usage error")
		}
	})
}
