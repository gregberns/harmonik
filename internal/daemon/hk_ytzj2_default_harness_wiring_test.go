package daemon_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// TestDefaultHarnessWiring_FieldCarriedThroughDeps verifies that
// TestRuntimeParams.DefaultHarness is stored in testRuntime.defaultHarness
// so the dispatch path forwards it to resolveHarness as the tier-4 global
// default (hk-ytzj2 fix part 1).
func TestDefaultHarnessWiring_FieldCarriedThroughDeps(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		harness core.AgentType
	}{
		{"pi", core.AgentTypePi},
		{"codex", core.AgentTypeCodex},
		{"claude-code", core.AgentTypeClaudeCode},
		{"empty (built-in fallback)", core.AgentType("")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
				Bus:            eventbus.NewBusImpl(),
				DefaultHarness: tc.harness,
			})

			got := daemon.ExportedWorkLoopDefaultHarness(deps)
			if got != tc.harness {
				t.Errorf("defaultHarness = %q; want %q (not wired from TestRuntimeParams.DefaultHarness)",
					got, tc.harness)
			}
		})
	}
}

// TestStandardBeadDotReviewNodePinnedToClaudeCode verifies the embedded
// standard-bead.dot REVIEW node carries harness="claude-code" (tier-3 pin),
// ensuring the reviewer stays on Claude when the global default is pi
// (hk-ytzj2 fix part 2 / hk-z4nif dodge).
func TestStandardBeadDotReviewNodePinnedToClaudeCode(t *testing.T) {
	t.Parallel()

	g, err := daemon.ExportedLoadStandardGraph(nil)
	if err != nil {
		t.Fatalf("ExportedLoadStandardGraph: %v", err)
	}

	for _, n := range g.Nodes {
		if n.ID == "review" {
			if n.Harness != string(core.AgentTypeClaudeCode) {
				t.Errorf("standard-bead.dot review node Harness = %q; want %q (tier-3 pin for hk-ytzj2)",
					n.Harness, core.AgentTypeClaudeCode)
			}
			return
		}
	}
	t.Fatal("standard-bead.dot: review node not found")
}
