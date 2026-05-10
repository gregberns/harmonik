package core

// upgradeDisciplineFixture — spec-level harness for hk-63oh.10.
//
// Covers: RC-006 (upgrade discipline — daemon code and library ship together),
// with specific focus on the split-release prohibition and the co-ship
// obligation (detector + action-map entry + workflow-library addition in S01
// in the same harmonik release).
//
// The WorkflowClass enum-fence test (TestRC006_WorkflowClassIsOnlyReconciliationAtMVH)
// lives in reconciliationworkflow_rc001_test.go. This file adds spec-artifact
// and co-ship-obligation tests scoped to hk-63oh.10.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-006.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// upgradeDisciplineFixtureCoShipObligation models one component that RC-006
// requires to ship together in the same harmonik release.
//
// Spec ref: reconciliation/spec.md §4.1 RC-006 — "MUST ship a daemon-code
// change (detector + action-map entry per §8 taxonomy) AND a workflow-library
// addition in S01 … in the same harmonik release."
type upgradeDisciplineFixtureCoShipObligation struct {
	Component string
	SpecRef   string
}

// upgradeDisciplineFixtureCoShipObligations is the authoritative fixture
// encoding of the three co-ship components declared by RC-006.
var upgradeDisciplineFixtureCoShipObligations = []upgradeDisciplineFixtureCoShipObligation{
	{"detector (daemon-code change)", "RC-006 — §8 taxonomy detector"},
	{"action-map entry (daemon-code change)", "RC-006 — §8 action-map entry"},
	{"workflow-library addition in S01 (for investigator-required categories)", "RC-006 — S01 library"},
}

// upgradeDisciplineFixtureReadSpec reads specs/reconciliation/spec.md and
// returns its content. Fails the test if the file cannot be read.
func upgradeDisciplineFixtureReadSpec(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("upgradeDisciplineFixtureReadSpec: runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "reconciliation", "spec.md")

	//nolint:gosec // G304: path constructed from runtime.Caller + known relative segments, not user input
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("upgradeDisciplineFixtureReadSpec: cannot read %s: %v", specPath, err)
	}
	return string(raw)
}

// TestRC006_SpecSectionExists verifies that RC-006 (upgrade discipline — daemon
// code and library ship together) exists in specs/reconciliation/spec.md.
//
// Spec ref: reconciliation/spec.md §4.1 RC-006.
func TestRC006_SpecSectionExists(t *testing.T) {
	t.Parallel()

	content := upgradeDisciplineFixtureReadSpec(t)

	if !strings.Contains(content, "RC-006") {
		t.Error("RC-006: specs/reconciliation/spec.md does not contain 'RC-006'")
	}
	if !strings.Contains(content, "Upgrade discipline") {
		t.Error("RC-006: specs/reconciliation/spec.md missing 'Upgrade discipline' heading for RC-006")
	}
}

// TestRC006_SplitReleasesAreForbidden verifies that the spec explicitly
// prohibits split releases.
//
// Spec ref: reconciliation/spec.md §4.1 RC-006 — "Split releases are
// forbidden."
func TestRC006_SplitReleasesAreForbidden(t *testing.T) {
	t.Parallel()

	content := upgradeDisciplineFixtureReadSpec(t)

	if !strings.Contains(content, "Split releases are forbidden") {
		t.Error("RC-006: specs/reconciliation/spec.md missing 'Split releases are forbidden' in RC-006")
	}
}

// TestRC006_CoShipObligationsAreThree verifies the fixture encodes exactly
// three co-ship components as declared by RC-006.
//
// Spec ref: reconciliation/spec.md §4.1 RC-006.
func TestRC006_CoShipObligationsAreThree(t *testing.T) {
	t.Parallel()

	const wantComponents = 3
	if len(upgradeDisciplineFixtureCoShipObligations) != wantComponents {
		t.Errorf("RC-006: co-ship-obligation fixture has %d entries, want %d (detector, action-map entry, S01 library)",
			len(upgradeDisciplineFixtureCoShipObligations), wantComponents)
	}
}

// TestRC006_CoShipObligationsHaveSpecRefs verifies every co-ship component has
// a non-empty SpecRef.
//
// Spec ref: reconciliation/spec.md §4.1 RC-006.
func TestRC006_CoShipObligationsHaveSpecRefs(t *testing.T) {
	t.Parallel()

	for _, ob := range upgradeDisciplineFixtureCoShipObligations {
		ob := ob
		t.Run(ob.Component, func(t *testing.T) {
			t.Parallel()

			if ob.SpecRef == "" {
				t.Errorf("RC-006: co-ship-obligation component %q has empty SpecRef", ob.Component)
			}
		})
	}
}

// TestRC006_DaemonCodeChangeDeclaresDetectorAndActionMap verifies that the
// spec names both "detector" and "action-map entry" as the two daemon-code
// change components.
//
// Spec ref: reconciliation/spec.md §4.1 RC-006 — "daemon-code change (detector
// + action-map entry per §8 taxonomy)."
func TestRC006_DaemonCodeChangeDeclaresDetectorAndActionMap(t *testing.T) {
	t.Parallel()

	content := upgradeDisciplineFixtureReadSpec(t)

	if !strings.Contains(content, "detector") {
		t.Error("RC-006: specs/reconciliation/spec.md missing 'detector' as part of the daemon-code change in RC-006")
	}
	if !strings.Contains(content, "action-map") {
		t.Error("RC-006: specs/reconciliation/spec.md missing 'action-map' as part of the daemon-code change in RC-006")
	}
}

// TestRC006_S01WorkflowLibraryAdditionRequired verifies that the spec names
// the S01 workflow-library addition as required for investigator-required
// categories.
//
// Spec ref: reconciliation/spec.md §4.1 RC-006 — "AND a workflow-library
// addition in S01 (for investigator-required categories)."
func TestRC006_S01WorkflowLibraryAdditionRequired(t *testing.T) {
	t.Parallel()

	content := upgradeDisciplineFixtureReadSpec(t)

	if !strings.Contains(content, "S01") {
		t.Error("RC-006: specs/reconciliation/spec.md missing 'S01' workflow-library addition clause in RC-006")
	}
	if !strings.Contains(content, "workflow-library addition") {
		t.Error("RC-006: specs/reconciliation/spec.md missing 'workflow-library addition' in RC-006")
	}
}

// TestRC006_AmendmentProtocolRequired verifies that the spec requires new
// categories to use the amendment protocol per architecture.md §4.6.
//
// Spec ref: reconciliation/spec.md §4.1 RC-006 — "A new reconciliation
// category (added via the amendment protocol per [architecture.md §4.6])."
func TestRC006_AmendmentProtocolRequired(t *testing.T) {
	t.Parallel()

	content := upgradeDisciplineFixtureReadSpec(t)

	if !strings.Contains(content, "amendment protocol") {
		t.Error("RC-006: specs/reconciliation/spec.md missing 'amendment protocol' requirement in RC-006")
	}
	if !strings.Contains(content, "architecture.md") {
		t.Error("RC-006: specs/reconciliation/spec.md missing 'architecture.md' cross-reference in RC-006 amendment clause")
	}
}

// TestRC006_SameHarmonikReleaseConstraint verifies that the spec explicitly
// names "same harmonik release" as the co-ship constraint.
//
// Spec ref: reconciliation/spec.md §4.1 RC-006 — "… in the same harmonik
// release."
func TestRC006_SameHarmonikReleaseConstraint(t *testing.T) {
	t.Parallel()

	content := upgradeDisciplineFixtureReadSpec(t)

	if !strings.Contains(content, "same harmonik release") {
		t.Error("RC-006: specs/reconciliation/spec.md missing 'same harmonik release' constraint in RC-006")
	}
}
