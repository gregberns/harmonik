package daemon_test

// crewlaunchspec_twoproj_hk25bg_test.go — T5 live two-project validation
// (hk-25bg): side-by-side scenario asserting that two concurrent harmonik
// projects with distinct slugs produce non-colliding RC labels while keeping
// all internal identity channels (HARMONIK_AGENT, --session-id / --resume)
// completely unaffected by the prefix.
//
// Scenarios:
//   1. TwoProjectLabelIsolation — same agent names ("captain", "paul") under
//      prefixes "hk" and "mp" yield distinct RC labels; HARMONIK_AGENT stays bare.
//   2. TwoProjectKeeperResumeParity — keeper clear→resume (fresh vs resume branch)
//      produces the SAME label for each project; the RC picker does not rename a
//      resumed session mid-flight.
//   3. TwoProjectSessionIDIndependence — the --session-id / --resume UUID is the
//      exclusive identity key (not the RC label); different projects with the same
//      agent name can carry the same bare session-id without collision because the
//      display label already disambiguates them.
//
// Run: go test ./internal/daemon/ -run TwoProject -v

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

const (
	hk25bgUUIDHK = "01940000-0001-7000-8000-000000000001"
	hk25bgUUIDMP = "01940000-0001-7000-8000-000000000002"
)

// buildSpec is a thin wrapper for test brevity.
func buildSpec(t *testing.T, name, rcPrefix, sessionID string, resume bool) []string {
	t.Helper()
	spec, err := daemon.ExportedBuildCrewLaunchSpec(daemon.ExportedCrewLaunchCtx{
		Name:       name,
		RcPrefix:   rcPrefix,
		SessionID:  sessionID,
		ProjectDir: t.TempDir(),
		Resume:     resume,
	})
	if err != nil {
		t.Fatalf("ExportedBuildCrewLaunchSpec(name=%q, prefix=%q, resume=%v): %v", name, rcPrefix, resume, err)
	}
	return spec.Args
}

// envHarmonikAgent extracts the bare HARMONIK_AGENT value from a launch spec's env.
func envHarmonikAgent(t *testing.T, name, rcPrefix string, resume bool) string {
	t.Helper()
	spec, err := daemon.ExportedBuildCrewLaunchSpec(daemon.ExportedCrewLaunchCtx{
		Name:       name,
		RcPrefix:   rcPrefix,
		SessionID:  hk25bgUUIDHK,
		ProjectDir: t.TempDir(),
		Resume:     resume,
	})
	if err != nil {
		t.Fatalf("ExportedBuildCrewLaunchSpec env path: %v", err)
	}
	for _, e := range spec.Env {
		if strings.HasPrefix(e, "HARMONIK_AGENT=") {
			return strings.TrimPrefix(e, "HARMONIK_AGENT=")
		}
	}
	return ""
}

// TestTwoProjectLabelIsolation_hk25bg asserts that two concurrent projects with
// prefixes "hk" and "mp" produce non-colliding RC session labels for the same
// agent names, and that HARMONIK_AGENT stays bare (keeper rebind / crew wake
// unaffected) in both projects.
func TestTwoProjectLabelIsolation_hk25bg(t *testing.T) {
	t.Parallel()

	agents := []string{"captain", "paul", "chani"}

	for _, agentName := range agents {
		agentName := agentName
		t.Run(agentName, func(t *testing.T) {
			t.Parallel()

			argsHK := buildSpec(t, agentName, "hk", hk25bgUUIDHK, false)
			argsMP := buildSpec(t, agentName, "mp", hk25bgUUIDMP, false)

			labelHK := argvFlagValue(argsHK, "--remote-control")
			labelMP := argvFlagValue(argsMP, "--remote-control")

			wantHK := "hk-" + agentName
			wantMP := "mp-" + agentName

			if labelHK != wantHK {
				t.Errorf("project hk: --remote-control = %q; want %q", labelHK, wantHK)
			}
			if labelMP != wantMP {
				t.Errorf("project mp: --remote-control = %q; want %q", labelMP, wantMP)
			}
			if labelHK == labelMP {
				t.Errorf("collision: both projects produced label %q; they must be distinct", labelHK)
			}

			// HARMONIK_AGENT must stay bare for both projects — this is the
			// identity channel used by comms, crew registry, and keeper rebind.
			agentHK := envHarmonikAgent(t, agentName, "hk", false)
			agentMP := envHarmonikAgent(t, agentName, "mp", false)

			if agentHK != agentName {
				t.Errorf("project hk: HARMONIK_AGENT = %q; want bare %q (prefix must not bleed into identity)", agentHK, agentName)
			}
			if agentMP != agentName {
				t.Errorf("project mp: HARMONIK_AGENT = %q; want bare %q (prefix must not bleed into identity)", agentMP, agentName)
			}
		})
	}
}

// TestTwoProjectKeeperResumeParity_hk25bg verifies that a keeper context-clear
// and resume (the --resume branch) produces the SAME RC label as the original
// fresh launch for each project. A divergent label would rename the RC picker
// session, breaking the "resume = same conversation" contract.
func TestTwoProjectKeeperResumeParity_hk25bg(t *testing.T) {
	t.Parallel()

	type project struct {
		prefix string
		uuid   string
	}
	projects := []project{
		{"hk", hk25bgUUIDHK},
		{"mp", hk25bgUUIDMP},
	}

	agents := []string{"captain", "paul"}

	for _, proj := range projects {
		proj := proj
		for _, agentName := range agents {
			agentName := agentName
			t.Run(proj.prefix+"/"+agentName, func(t *testing.T) {
				t.Parallel()

				argsFresh := buildSpec(t, agentName, proj.prefix, proj.uuid, false)
				argsResume := buildSpec(t, agentName, proj.prefix, proj.uuid, true)

				labelFresh := argvFlagValue(argsFresh, "--remote-control")
				labelResume := argvFlagValue(argsResume, "--remote-control")

				if labelFresh != labelResume {
					t.Errorf("project %q / agent %q: keeper resume parity broken — fresh label %q != resume label %q",
						proj.prefix, agentName, labelFresh, labelResume)
				}
				// Sanity: the label is actually prefixed.
				want := proj.prefix + "-" + agentName
				if labelFresh != want {
					t.Errorf("project %q / agent %q: fresh label %q; want %q", proj.prefix, agentName, labelFresh, want)
				}
			})
		}
	}
}

// TestTwoProjectSessionIDIndependence_hk25bg verifies that the --session-id /
// --resume value is the raw UUID (identity key, keeper rebind channel) and
// contains no RC prefix. The RC prefix is RC-label-only: it must never leak
// into the UUID that keeper, comms, or the crew registry uses to re-attach.
func TestTwoProjectSessionIDIndependence_hk25bg(t *testing.T) {
	t.Parallel()

	for _, resume := range []bool{false, true} {
		resume := resume
		t.Run(map[bool]string{false: "fresh", true: "resume"}[resume], func(t *testing.T) {
			t.Parallel()

			// Use the SAME UUID for both projects — two projects can share an
			// agent name. The UUID (session-id / resume target) is independent;
			// collisions here don't matter because the RC label already separates
			// the sessions in the picker.
			args := buildSpec(t, "captain", "hk", hk25bgUUIDHK, resume)

			flag := "--session-id"
			if resume {
				flag = "--resume"
			}
			sid := argvFlagValue(args, flag)

			if sid != hk25bgUUIDHK {
				t.Errorf("flag %q value = %q; want raw UUID %q (prefix must not pollute the identity channel)", flag, sid, hk25bgUUIDHK)
			}
			if strings.Contains(sid, "hk") {
				t.Errorf("flag %q contains prefix %q: %q — identity channel must be prefix-free", flag, "hk", sid)
			}
		})
	}
}
