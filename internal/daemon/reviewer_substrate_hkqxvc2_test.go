package daemon

import (
	"os"
	"strings"
	"testing"
)

// TestReviewerSubstrateDispatchWiring_hkqxvc2 is the daemon-tier regression
// guard for the three production reviewer/evaluator dispatch paths. The
// composition-root behavior is exercised in cmd/harmonik's
// TestSelectSubstrate_RequireIsolationBoundary_HK5H759; this test pins the
// load-bearing handoff from workLoopDeps.reviewerSubstrate into each per-run
// substrate without adding a production-only selection helper.
func TestReviewerSubstrateDispatchWiring_hkqxvc2(t *testing.T) {
	tests := []struct {
		file       string
		claudeGate string
		base       string
	}{
		{"dot_cascade.go", "reviewerHarnessIsClaude && deps.reviewerSubstrate != nil", "newPerRunSubstrate(baseSubstrate"},
		{"reviewloop.go", "revReviewerHarnessIsClaude && deps.reviewerSubstrate != nil", "newPerRunSubstrate(revBaseSubstrate"},
		{"dot_gate.go", "gateHarnessIsClaude && deps.reviewerSubstrate != nil", "newPerRunSubstrate(gateBaseSubstrate"},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			body, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatalf("read %s: %v", tc.file, err)
			}
			src := string(body)
			for _, want := range []string{
				"SessionIDPolicy() == handlercontract.SessionIDMinted",
				tc.claudeGate,
				"= deps.reviewerSubstrate",
				tc.base,
			} {
				if !strings.Contains(src, want) {
					t.Fatalf("hk-qxvc2 regression: %s no longer contains %q", tc.file, want)
				}
			}
		})
	}
}
