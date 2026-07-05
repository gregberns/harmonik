package daemon_test

// hk_ytzj2_default_harness_wiring_test.go — verifies that Config.DefaultHarness
// is correctly threaded into the harness-selection precedence walk (hk-ytzj2).
//
// # What this file proves
//
//  1. WorkLoopDepsParams.DefaultHarness is carried through ExportedWorkLoopDeps
//     into workLoopDeps.defaultHarness so the dispatch path passes it as
//     resolveHarness's tier-4 global default.
//
//  2. The embedded standard-bead.dot REVIEW node carries harness="claude-code"
//     (tier-3 pin) so the reviewer stays on Claude even when the global default
//     (tier 4) is pi — dodging the Pi-reviewer pane seed-paste bug (hk-z4nif).
//
// Helper prefix: hkytzj2 (per implementer-protocol.md §Helper-prefix discipline).
//
// Bead: hk-ytzj2.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// TestDefaultHarnessWiring_FieldCarriedThroughDeps verifies that
// WorkLoopDepsParams.DefaultHarness is stored in workLoopDeps.defaultHarness
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
				Bus:            eventbus.NewBusImpl(),
				DefaultHarness: tc.harness,
			})

			got := daemon.ExportedWorkLoopDefaultHarness(deps)
			if got != tc.harness {
				t.Errorf("defaultHarness = %q; want %q (not wired from WorkLoopDepsParams.DefaultHarness)",
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
