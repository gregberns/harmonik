package daemon_test

// reviewer_never_inherits_captured_hkpkxju_test.go — a reviewer never INHERITS a
// SessionIDCaptured harness (hk-pkxju).
//
// # The defect
//
// Under HARMONIK_SUBSTRATE=codexdriver, codex implemented and committed fine but the
// REVIEW node always died with "reviewer agent_ready_timeout" and the bead never
// closed. Cause: the reviewer INHERITED the implementer's harness, and no
// SessionIDCaptured harness (codex, pi) can review today —
// codexlaunchspec.go builds only an implementer seed prompt (there is no
// reviewer-phase branch), so a codex "reviewer" runs IMPLEMENT instructions and never
// writes .harmonik/review.json; and codex never emits agent_ready while the reviewer
// dispatch blocks on it.
//
// Two sites inherited:
//   - reviewloop.go — reviewer tier-3 nodeDefault pinned to implArtifacts.resolvedAgentType.
//   - dot_cascade.go — reviewer node with no reviewer_harness= override and no own
//     harness= attr fell through to deps.launchSpecBuilder (tier-1/tier-4 → codex).
//
// # What this file proves
//
//  1. review-loop mode: a codex implementer ⇒ the reviewer's tier-3 default becomes
//     claude-code, and the downstream resolveHarness walk therefore selects claude.
//  2. review-loop mode: a claude implementer ⇒ claude, unchanged (no regression).
//  3. DOT mode: reviewer node with NO explicit harness under a codex default ⇒ the
//     inherited-harness correction fires and yields claude-code.
//  4. DOT mode: an explicit reviewer_harness= override (or the reviewer node's own
//     harness= attr) is NOT clobbered — even when it names a SessionIDCaptured
//     harness. An operator pin stays an operator pin, which is the seam a future
//     "teach codex to review" fast-follow uses.
//
// Helper prefix: hkpkxju (per implementer-protocol.md helper-prefix discipline).
//
// Tags: mechanism
//
// Bead: hk-pkxju. Builds on hk-iv748 [C5/T14] and hk-2jxqg.

import (
	"os"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// hkpkxjuBead is a bead with no tier-1 harness: label unless labels are supplied.
func hkpkxjuBead(id string, labels ...string) core.BeadRecord {
	return core.BeadRecord{
		BeadID:   core.BeadID(id),
		BeadType: "task",
		Status:   core.CoarseStatusOpen,
		Labels:   labels,
	}
}

// TestReviewerNeverInheritsCapturedHarness_ReviewLoop_hkpkxju covers cases 1 and 2:
// the review-loop reviewer's tier-3 nodeDefault.
//
// This binds to reviewloop.go, where implArtifacts.resolvedAgentType is passed
// through reviewerDefaultHarness before being handed to routedLaunchSpecBuilder as
// the reviewer's tier-3 nodeDefault.
func TestReviewerNeverInheritsCapturedHarness_ReviewLoop_hkpkxju(t *testing.T) {
	t.Parallel()

	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	tests := []struct {
		name        string
		implementer core.AgentType
		want        core.AgentType
	}{
		{
			name:        "codex implementer falls back to claude",
			implementer: core.AgentTypeCodex,
			want:        core.AgentTypeClaudeCode,
		},
		{
			name:        "pi implementer falls back to claude",
			implementer: core.AgentTypePi,
			want:        core.AgentTypeClaudeCode,
		},
		{
			name:        "claude implementer unchanged (no regression)",
			implementer: core.AgentTypeClaudeCode,
			want:        core.AgentTypeClaudeCode,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := daemon.ExportedReviewerDefaultHarness(reg, tc.implementer, "hkpkxju-reviewloop")
			if got != tc.want {
				t.Fatalf("reviewer tier-3 default for implementer %q = %q; want %q",
					tc.implementer, got, tc.want)
			}

			// End-to-end through the walk reviewloop.go actually performs: tier-1 is an
			// empty BeadRecord (bead labels already folded into resolvedAgentType) and
			// the global default is empty, so tier-3 decides.
			bus := &hkiv748Bus{}
			resolved := daemon.ExportedResolveHarness(
				t.Context(),
				core.BeadRecord{},
				core.AgentType(""), // queue default
				got,                // tier-3: the hk-pkxju-corrected reviewer default
				core.AgentType(""), // global default → built-in claude-code
				bus,
			)
			if resolved != tc.want {
				t.Fatalf("resolveHarness for reviewer = %q; want %q", resolved, tc.want)
			}
		})
	}
}

// TestReviewerNeverInheritsCapturedHarness_ClaudeImplementerByteIdentical_hkpkxju
// pins case 2 harder: for a claude (SessionIDMinted) implementer the helper must
// return the implementer agent type UNCHANGED — not merely something that happens to
// equal claude-code. That identity is what makes the all-claude path byte-identical
// to pre-hk-pkxju behaviour.
func TestReviewerNeverInheritsCapturedHarness_ClaudeImplementerByteIdentical_hkpkxju(t *testing.T) {
	t.Parallel()

	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	// Unknown / unregistered agent types and an absent registry also pass through
	// unchanged (fail open — the caller's existing error handling still applies).
	cases := []struct {
		name        string
		implementer core.AgentType
	}{
		{"claude-code", core.AgentTypeClaudeCode},
		{"empty (no resolved implementer harness)", core.AgentType("")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := daemon.ExportedReviewerDefaultHarness(reg, tc.implementer, "hkpkxju-passthrough"); got != tc.implementer {
				t.Fatalf("implementer %q was rewritten to %q; want pass-through", tc.implementer, got)
			}
		})
	}

	// nil registry: pass through unchanged rather than silently rewriting.
	if got := daemon.ExportedReviewerDefaultHarness(nil, core.AgentTypeCodex, "hkpkxju-nilreg"); got != core.AgentTypeCodex {
		t.Fatalf("nil registry: implementer codex rewritten to %q; want pass-through", got)
	}
}

// TestReviewerNeverInheritsCapturedHarness_Dot_hkpkxju covers cases 3 and 4: the DOT
// cascade's reviewer-harness precedence.
//
// Non-empty return ⇒ dispatchDotAgenticNode pins the reviewer to that harness via
// pinnedHarnessLaunchSpecBuilder. Empty return ⇒ the existing precedence stands
// untouched (explicit pin, or deps.launchSpecBuilder for an already-fine default).
func TestReviewerNeverInheritsCapturedHarness_Dot_hkpkxju(t *testing.T) {
	t.Parallel()

	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	tests := []struct {
		name             string
		isReviewer       bool
		reviewerOverride core.AgentType
		nodeHarness      core.AgentType
		bead             core.BeadRecord
		globalDefault    core.AgentType
		want             core.AgentType
	}{
		{
			// Case 3: the defect. Reviewer node with no explicit harness, codexdriver
			// global default ⇒ would have inherited codex ⇒ corrected to claude.
			name:          "default reviewer under codex global default falls back to claude",
			isReviewer:    true,
			bead:          hkpkxjuBead("hkpkxju-dot-default"),
			globalDefault: core.AgentTypeCodex,
			want:          core.AgentTypeClaudeCode,
		},
		{
			// Same defect reached via a tier-1 bead label instead of the global default:
			// harness:codex selects the IMPLEMENTER's harness, and inheriting it into the
			// reviewer is exactly what hk-pkxju forbids.
			name:          "default reviewer under harness:codex bead label falls back to claude",
			isReviewer:    true,
			bead:          hkpkxjuBead("hkpkxju-dot-label", "harness:codex"),
			globalDefault: core.AgentType(""),
			want:          core.AgentTypeClaudeCode,
		},
		{
			// Case 4a: explicit reviewer_harness= override, even to a captured harness.
			name:             "reviewer_harness override to codex is honored",
			isReviewer:       true,
			reviewerOverride: core.AgentTypeCodex,
			bead:             hkpkxjuBead("hkpkxju-dot-override"),
			globalDefault:    core.AgentTypeCodex,
			want:             core.AgentType(""), // no correction: the pin stands
		},
		{
			// Case 4b: reviewer node's own harness= attr, even to a captured harness.
			name:          "reviewer node harness= attr to codex is honored",
			isReviewer:    true,
			nodeHarness:   core.AgentTypeCodex,
			bead:          hkpkxjuBead("hkpkxju-dot-nodeattr"),
			globalDefault: core.AgentTypeCodex,
			want:          core.AgentType(""), // no correction: the pin stands
		},
		{
			// reviewer_harness override to claude: already claude, nothing to correct;
			// the existing pinnedHarnessLaunchSpecBuilder path handles it.
			name:             "reviewer_harness override to claude needs no correction",
			isReviewer:       true,
			reviewerOverride: core.AgentTypeClaudeCode,
			bead:             hkpkxjuBead("hkpkxju-dot-override-claude"),
			globalDefault:    core.AgentTypeCodex,
			want:             core.AgentType(""),
		},
		{
			// All-claude DOT run: no correction, deps.launchSpecBuilder unchanged.
			name:          "default reviewer under claude default is untouched",
			isReviewer:    true,
			bead:          hkpkxjuBead("hkpkxju-dot-allclaude"),
			globalDefault: core.AgentTypeClaudeCode,
			want:          core.AgentType(""),
		},
		{
			// The IMPLEMENTER node must keep inheriting codex — this rule is
			// reviewer-only. Without this the fix would break codex implementation.
			name:          "implementer node under codex default is untouched",
			isReviewer:    false,
			bead:          hkpkxjuBead("hkpkxju-dot-impl"),
			globalDefault: core.AgentTypeCodex,
			want:          core.AgentType(""),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := daemon.ExportedDotReviewerInheritedHarnessOverride(
				reg,
				tc.isReviewer,
				tc.reviewerOverride,
				tc.nodeHarness,
				tc.bead,
				tc.globalDefault,
				string(tc.bead.BeadID),
			)
			if got != tc.want {
				t.Fatalf("dotReviewerInheritedHarnessOverride = %q; want %q", got, tc.want)
			}
		})
	}
}

// TestReviewerNeverInheritsCapturedHarness_DispatchWiring_hkpkxju is the
// regression guard for the two production call sites. The helpers above are pure
// functions; without this, deleting a call site would leave every behaviour test
// green while the defect returned. Same idiom as
// TestReviewerSubstrateDispatchWiring_hkqxvc2 (hk-qxvc2).
func TestReviewerNeverInheritsCapturedHarness_DispatchWiring_hkpkxju(t *testing.T) {
	t.Parallel()

	tests := []struct {
		file string
		want []string
	}{
		{
			file: "reviewloop.go",
			want: []string{
				"revNodeDefault := reviewerDefaultHarness(",
				"deps.harnessRegistry, implArtifacts.resolvedAgentType, string(beadID))",
				"revNodeDefault,     // tier-3",
			},
		},
		{
			file: "dot_cascade.go",
			want: []string{
				"reviewerInheritedHarness := dotReviewerInheritedHarnessOverride(",
				"effectiveNodeHarness = reviewerInheritedHarness",
				"nodeModelHarness = reviewerInheritedHarness",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			body, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatalf("read %s: %v", tc.file, err)
			}
			src := string(body)
			for _, want := range tc.want {
				if !strings.Contains(src, want) {
					t.Fatalf("hk-pkxju regression: %s no longer contains %q", tc.file, want)
				}
			}
		})
	}
}
