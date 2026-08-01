package daemon_test

// dot_node_piprofile_test.go — a bead's Pi provider profile must reach the
// agent, whichever workflow mode dispatches it.
//
// The profile is a five-field tuple — provider, api-key env var, api-key file,
// base URL, wire API — resolved from the bead's `profile:<name>` label at claim
// time. The single-mode launch context set all five. The graph node's launch
// context set none of them, and nothing downstream supplied them, so a graph
// run silently fell back to the harness-global default provider. The graph is
// the production default, so in practice the profile never applied.
//
// Bead: hk-yo9g6.

import (
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/harness/claude"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

// dotFixturePiProfile is the tuple under test. Every field is distinct so a
// mis-wired assignment cannot pass by accident.
var dotFixturePiProfile = projectconfig.PiProfileConfig{
	Provider:   "fixture-provider",
	Model:      "fixture-provider/fixture-model",
	APIKeyEnv:  "HARMONIK_FIXTURE_PI_KEY",
	APIKeyFile: "/fixture/pi/key",
	BaseURL:    "https://fixture.invalid/v1",
	API:        "openai-completions",
}

const dotFixturePiProfileName = "fixture"

// dotFixtureLaunchCtxRecorder is the run's launch-spec port with a tap on it.
// It records the launch context each node is built from and then delegates to
// the production claude builder, so the run proceeds exactly as it otherwise
// would.
type dotFixtureLaunchCtxRecorder struct {
	seen chan shared.LaunchCtx
}

func newDotFixtureLaunchCtxRecorder() *dotFixtureLaunchCtxRecorder {
	return &dotFixtureLaunchCtxRecorder{seen: make(chan shared.LaunchCtx, 8)}
}

func (r *dotFixtureLaunchCtxRecorder) build(ctx context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	select {
	case r.seen <- rc:
	default:
	}
	return claude.BuildLaunchSpec(ctx, rc)
}

// first returns the first recorded launch context.
func (r *dotFixtureLaunchCtxRecorder) first(t *testing.T) shared.LaunchCtx {
	t.Helper()
	select {
	case rc := <-r.seen:
		return rc
	default:
		t.Fatal("no launch context was recorded, so this test asserts nothing")
		return shared.LaunchCtx{}
	}
}

// dotFixturePiProjectCfg is a project config carrying the profile above.
func dotFixturePiProjectCfg() projectconfig.ProjectConfig {
	return projectconfig.ProjectConfig{
		Harnesses: projectconfig.HarnessesConfig{
			Pi: projectconfig.PiHarnessConfig{
				Profiles: map[string]projectconfig.PiProfileConfig{
					dotFixturePiProfileName: dotFixturePiProfile,
				},
			},
		},
	}
}

// dotFixtureAssertPiProfile checks all five provider fields against the profile.
func dotFixtureAssertPiProfile(t *testing.T, mode string, rc shared.LaunchCtx) {
	t.Helper()
	for _, f := range []struct {
		name string
		got  string
		want string
	}{
		{"Provider", rc.Provider, dotFixturePiProfile.Provider},
		{"APIKeyEnv", rc.APIKeyEnv, dotFixturePiProfile.APIKeyEnv},
		{"APIKeyFile", rc.APIKeyFile, dotFixturePiProfile.APIKeyFile},
		{"BaseURL", rc.BaseURL, dotFixturePiProfile.BaseURL},
		{"API", rc.API, dotFixturePiProfile.API},
		// The profile's model rides the same hop. Without it the tuple could be
		// carried while the run still asked the new provider for the old model.
		{"Model", rc.Model, dotFixturePiProfile.Model},
	} {
		if f.got != f.want {
			t.Errorf("%s launch context %s = %q; want %q.\n"+
				"The bead's provider profile did not reach the agent, so the run silently used the harness-global default provider.",
				mode, f.name, f.got, f.want)
		}
	}
}

// dotFixturePiOpts are the shared inputs of the two runs below. The ONLY thing
// that differs between them is the workflow mode.
func dotFixturePiOpts(t *testing.T, beadID core.BeadID, rec *dotFixtureLaunchCtxRecorder) dotFixtureOpts {
	t.Helper()
	return dotFixtureOpts{
		HandlerScript:     dotFixtureCommittingHandler(t, beadID),
		BeadLabels:        []string{"profile:" + dotFixturePiProfileName},
		ProjectCfg:        dotFixturePiProjectCfg(),
		DefaultHarness:    core.AgentTypePi,
		LaunchSpecBuilder: rec.build,
	}
}

// TestDotNode_PiProviderProfileReachesTheGraphNode is the claim.
func TestDotNode_PiProviderProfileReachesTheGraphNode(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-yo9g6-dot-profile")
	rec := newDotFixtureLaunchCtxRecorder()
	runDotFixtureBead(t, beadID, dotFixturePiOpts(t, beadID, rec))

	dotFixtureAssertPiProfile(t, "dot", rec.first(t))
}

// TestSingleRun_PiProviderProfileReachesTheAgent is the reference the claim is
// measured against, and it is what makes the test above more than an assertion
// about a constant.
//
// It is the SAME bead, the SAME project config and the SAME handler; only the
// per-item workflow mode differs. Single mode has always carried the tuple. If
// this one ever goes red, the resolution itself broke and the graph test's
// verdict means nothing.
func TestSingleRun_PiProviderProfileReachesTheAgent(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-yo9g6-single-profile")
	rec := newDotFixtureLaunchCtxRecorder()
	opts := dotFixturePiOpts(t, beadID, rec)
	opts.WorkflowMode = core.WorkflowModeSingle
	runDotFixtureBead(t, beadID, opts)

	dotFixtureAssertPiProfile(t, "single", rec.first(t))
}
