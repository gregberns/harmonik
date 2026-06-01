package core

// detectorsnotinlibrary_63oh9_test.go — spec-level harness for hk-63oh.9.
//
// Covers: RC-005 (detectors and verdict-execution mechanics are NOT in the
// workflow library), with focus on two sub-obligations:
//
//   §4.3 — detectors MUST live in the daemon's Go code (mechanism-tagged
//           functions), NOT in the S01 workflow library.
//   §4.5 — verdict-execution mechanics (action dispatch, verdict-executed
//           commit emission, idempotency-key adapter calls) MUST also live
//           in the daemon's Go code.
//   Corollary — the S01 library owns investigator REASONING only.
//
// Tests split into three groups:
//   A. Spec-text anchors (spec.md must contain the RC-005 requirement prose)
//   B. DOT-file structural probes (S01 library workflows must not carry
//      detector or verdict-execution node logic)
//   C. Node-role fixtures (the only allowed node roles in S01 reconciliation
//      workflows are investigator-reasoning roles per RC-015a)
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-005.
// Bead: hk-63oh.9.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// rc63oh9FixtureReadSpec reads specs/reconciliation/spec.md and returns its
// content. Fails the test if the file cannot be read.
func rc63oh9FixtureReadSpec(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("rc63oh9FixtureReadSpec: runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "reconciliation", "spec.md")
	//nolint:gosec // G304: path constructed from runtime.Caller + known relative segments, not user input
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("rc63oh9FixtureReadSpec: cannot read %s: %v", specPath, err)
	}
	return string(raw)
}

// rc63oh9FixtureReadDOT reads a DOT file from specs/s01/reconciliation/workflows/
// by filename (e.g. "cat-2.dot"). Fails the test if the file cannot be read.
func rc63oh9FixtureReadDOT(t *testing.T, filename string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("rc63oh9FixtureReadDOT: runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "s01", "reconciliation", "workflows", filename)
	//nolint:gosec // G304: path constructed from runtime.Caller + known relative segments, not user input
	raw, err := os.ReadFile(dotPath)
	if err != nil {
		t.Fatalf("rc63oh9FixtureReadDOT: cannot read %s: %v", dotPath, err)
	}
	return string(raw)
}

// rc63oh9FixtureS01DOTFiles returns the three canonical DOT filenames in the
// S01 reconciliation workflow library.
var rc63oh9FixtureS01DOTFiles = []string{
	"cat-2.dot",
	"cat-3.dot",
	"cat-6a.dot",
}

// rc63oh9FixtureDetectorRoleMarkers are string patterns whose presence in a
// DOT workflow node would indicate that detector logic has leaked into the
// S01 library (violating RC-005 §4.3).
var rc63oh9FixtureDetectorRoleMarkers = []string{
	`role="detector"`,
	`type="detector"`,
	`handler_ref="detector"`,
	`role="reconciliation-detector"`,
	`classify_run`,
	`category_assignment`,
}

// rc63oh9FixtureVerdictExecutionMarkers are string patterns whose presence in a
// DOT workflow node would indicate that verdict-execution mechanics have leaked
// into the S01 library (violating RC-005 §4.5).
var rc63oh9FixtureVerdictExecutionMarkers = []string{
	`role="verdict-executor"`,
	`role="verdict_executor"`,
	`type="verdict-executor"`,
	`handler_ref="verdict-executor"`,
	`handler_ref="verdict-execution"`,
	`verdict_execute`,
	`verdict-executed-commit`,
}

// ── A. Spec-text anchors ──────────────────────────────────────────────────────

// TestRC005_SpecSectionExists verifies that RC-005 is present in
// specs/reconciliation/spec.md with the expected heading.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-005.
func TestRC005_SpecSectionExists(t *testing.T) {
	t.Parallel()

	content := rc63oh9FixtureReadSpec(t)

	if !strings.Contains(content, "RC-005") {
		t.Error("RC-005: specs/reconciliation/spec.md does not contain 'RC-005'")
	}
	if !strings.Contains(content, "Detectors and verdict-execution mechanics are NOT in the workflow library") {
		t.Error("RC-005: specs/reconciliation/spec.md missing the RC-005 heading prose")
	}
}

// TestRC005_SpecNamesDetectorsInDaemonGoCode verifies that the spec text
// explicitly places §4.3 detectors in the daemon's Go code.
//
// RC-005: "The §4.3 detectors MUST live in the daemon's Go code (mechanism-
// tagged functions), NOT in the S01 workflow library."
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-005.
func TestRC005_SpecNamesDetectorsInDaemonGoCode(t *testing.T) {
	t.Parallel()

	content := rc63oh9FixtureReadSpec(t)

	if !strings.Contains(content, "daemon's Go code") {
		t.Error("RC-005: spec.md missing 'daemon's Go code' — detector placement is ambiguous")
	}
	if !strings.Contains(content, "mechanism-tagged functions") {
		t.Error("RC-005: spec.md missing 'mechanism-tagged functions' — detector tagging convention is undeclared")
	}
	if !strings.Contains(content, "NOT in the S01 workflow library") {
		t.Error("RC-005: spec.md missing 'NOT in the S01 workflow library' prohibition for detectors")
	}
}

// TestRC005_SpecNamesVerdictExecutionInDaemonGoCode verifies that the spec text
// explicitly places §4.5 verdict-execution mechanics in the daemon's Go code.
//
// RC-005: "The §4.5 verdict-execution mechanics (action dispatch, verdict-
// executed commit emission, idempotency-key adapter calls) MUST also live in
// the daemon's Go code."
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-005.
func TestRC005_SpecNamesVerdictExecutionInDaemonGoCode(t *testing.T) {
	t.Parallel()

	content := rc63oh9FixtureReadSpec(t)

	if !strings.Contains(content, "verdict-execution mechanics") {
		t.Error("RC-005: spec.md missing 'verdict-execution mechanics' phrase in RC-005")
	}
	if !strings.Contains(content, "action dispatch") {
		t.Error("RC-005: spec.md missing 'action dispatch' in the verdict-execution mechanics enumeration")
	}
	if !strings.Contains(content, "verdict-executed commit emission") {
		t.Error("RC-005: spec.md missing 'verdict-executed commit emission' in the verdict-execution mechanics enumeration")
	}
	if !strings.Contains(content, "idempotency-key adapter calls") {
		t.Error("RC-005: spec.md missing 'idempotency-key adapter calls' in the verdict-execution mechanics enumeration")
	}
}

// TestRC005_SpecAssignsInvestigatorReasoningToS01 verifies that the spec text
// explicitly assigns investigator reasoning (and only reasoning) to the S01
// library.
//
// RC-005: "The S01 library owns investigator reasoning only."
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-005.
func TestRC005_SpecAssignsInvestigatorReasoningToS01(t *testing.T) {
	t.Parallel()

	content := rc63oh9FixtureReadSpec(t)

	if !strings.Contains(content, "S01 library owns investigator reasoning only") {
		t.Error("RC-005: spec.md missing 'S01 library owns investigator reasoning only' ownership clause")
	}
}

// ── B. DOT-file structural probes ─────────────────────────────────────────────

// TestRC005_S01DOTFilesExist verifies that all three expected S01 reconciliation
// DOT workflow files are present on disk.
//
// RC-004 (via RC-005): the S01 library must ship a DOT per investigator-
// required category; RC-005 bounds what those DOTs may contain.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-004, RC-005.
func TestRC005_S01DOTFilesExist(t *testing.T) {
	t.Parallel()

	for _, filename := range rc63oh9FixtureS01DOTFiles {
		filename := filename
		t.Run(filename, func(t *testing.T) {
			t.Parallel()
			// rc63oh9FixtureReadDOT fails the test if the file is missing.
			content := rc63oh9FixtureReadDOT(t, filename)
			if content == "" {
				t.Errorf("RC-005: %s is empty; S01 library DOT file must be non-empty", filename)
			}
		})
	}
}

// TestRC005_S01DOTFilesCarryNoDetectorNodes verifies that none of the three S01
// DOT workflow files contain node definitions or attributes that would indicate
// detector logic (violating RC-005 §4.3).
//
// Detectors are mechanism-tagged Go functions in the daemon; they MUST NOT
// appear as DOT nodes in the S01 library.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-005.
func TestRC005_S01DOTFilesCarryNoDetectorNodes(t *testing.T) {
	t.Parallel()

	for _, filename := range rc63oh9FixtureS01DOTFiles {
		filename := filename
		t.Run(filename, func(t *testing.T) {
			t.Parallel()
			content := rc63oh9FixtureReadDOT(t, filename)
			for _, marker := range rc63oh9FixtureDetectorRoleMarkers {
				if strings.Contains(content, marker) {
					t.Errorf("RC-005 §4.3 violation: %s contains detector marker %q; "+
						"detectors MUST live in daemon Go code, NOT in the S01 workflow library",
						filename, marker)
				}
			}
		})
	}
}

// TestRC005_S01DOTFilesCarryNoVerdictExecutionNodes verifies that none of the
// three S01 DOT workflow files contain node definitions or attributes that
// would indicate verdict-execution mechanics (violating RC-005 §4.5).
//
// Verdict-execution (action dispatch, verdict-executed commit emission,
// idempotency-key adapter calls) MUST live in daemon Go code; the DOT node
// that represents this work is the daemon-side verdict-executor per RC-025a,
// which is NOT modeled as a workflow node.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-005.
func TestRC005_S01DOTFilesCarryNoVerdictExecutionNodes(t *testing.T) {
	t.Parallel()

	for _, filename := range rc63oh9FixtureS01DOTFiles {
		filename := filename
		t.Run(filename, func(t *testing.T) {
			t.Parallel()
			content := rc63oh9FixtureReadDOT(t, filename)
			for _, marker := range rc63oh9FixtureVerdictExecutionMarkers {
				if strings.Contains(content, marker) {
					t.Errorf("RC-005 §4.5 violation: %s contains verdict-execution marker %q; "+
						"verdict-execution mechanics MUST live in daemon Go code, NOT in the S01 workflow library",
						filename, marker)
				}
			}
		})
	}
}

// ── C. Node-role fixtures ─────────────────────────────────────────────────────

// rc63oh9FixturePermittedNodeRoles enumerates the node roles the S01 reconciliation
// library DOT files are allowed to carry, per RC-005's "S01 library owns
// investigator reasoning only" principle.
//
// The three allowed roles correspond to the three structural nodes that every
// S01 reconciliation DOT contains:
//   - "reconciliation workflow entry point" (the start node)
//   - "investigator"                        (the reasoning-only cognition node)
//   - "reconciliation workflow terminal"    (the close node)
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-005, RC-015a.
var rc63oh9FixturePermittedNodeRoles = []string{
	"reconciliation workflow entry point",
	"investigator",
	"reconciliation workflow terminal",
}

// TestRC005_PermittedNodeRolesAreInvestigatorReasoningOnly verifies that the
// fixture encoding of allowed node roles is exactly three entries — one for
// each structural node in an S01 reconciliation DOT — and that none of them
// names a detector or verdict-execution role.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-005.
func TestRC005_PermittedNodeRolesAreInvestigatorReasoningOnly(t *testing.T) {
	t.Parallel()

	const wantCount = 3
	if len(rc63oh9FixturePermittedNodeRoles) != wantCount {
		t.Errorf("RC-005: permitted-node-roles fixture has %d entries, want %d "+
			"(entry-point, investigator, terminal)", len(rc63oh9FixturePermittedNodeRoles), wantCount)
	}

	for _, role := range rc63oh9FixturePermittedNodeRoles {
		role := role
		t.Run(role, func(t *testing.T) {
			t.Parallel()

			if role == "" {
				t.Error("RC-005: permitted role is empty string; fixture error")
			}
			// No permitted role should name a detector or verdict-executor.
			for _, forbidden := range []string{"detector", "verdict-execut", "action-dispatch"} {
				if strings.Contains(strings.ToLower(role), forbidden) {
					t.Errorf("RC-005: permitted role %q contains forbidden term %q; "+
						"detector/verdict-execution roles are not allowed in S01 library", role, forbidden)
				}
			}
		})
	}
}

// TestRC005_S01DOTFilesContainOnlyPermittedRoles verifies that every
// `role="..."` attribute in each S01 reconciliation DOT workflow matches one
// of the three permitted node roles.
//
// This test provides the tightest structural guarantee: it checks that no new
// role has crept into a DOT file that is not in the investigator-reasoning-only
// set.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-005.
func TestRC005_S01DOTFilesContainOnlyPermittedRoles(t *testing.T) {
	t.Parallel()

	for _, filename := range rc63oh9FixtureS01DOTFiles {
		filename := filename
		t.Run(filename, func(t *testing.T) {
			t.Parallel()
			content := rc63oh9FixtureReadDOT(t, filename)

			// Extract every role="..." value from the DOT content.
			// A role attribute appears as:   role="<value>"
			// We scan for role=" tokens and collect the quoted value.
			extractedRoles := rc63oh9ExtractRoleAttributes(content)

			for _, extracted := range extractedRoles {
				found := false
				for _, permitted := range rc63oh9FixturePermittedNodeRoles {
					if extracted == permitted {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("RC-005: %s contains role=%q which is not in the investigator-reasoning-only "+
						"set %v; S01 library MUST own investigator reasoning only (RC-005)",
						filename, extracted, rc63oh9FixturePermittedNodeRoles)
				}
			}
		})
	}
}

// rc63oh9ExtractRoleAttributes extracts all values from `role="..."` occurrences
// in a DOT file's text content. It handles the typical DOT attribute format.
func rc63oh9ExtractRoleAttributes(content string) []string {
	const prefix = `role="`
	var roles []string
	remaining := content
	for {
		idx := strings.Index(remaining, prefix)
		if idx < 0 {
			break
		}
		after := remaining[idx+len(prefix):]
		end := strings.Index(after, `"`)
		if end < 0 {
			break
		}
		roles = append(roles, after[:end])
		remaining = after[end+1:]
	}
	return roles
}

// TestRC005_WorkflowClassTagIsTheSoleReconciliationDiscriminatorInDOT verifies
// that the `workflow_class="reconciliation"` graph attribute is present in
// each S01 DOT file and is the only reconciliation-discriminating attribute
// at the graph level. Detector logic (which reads this tag) lives in daemon
// Go code, not in the DOT itself.
//
// RC-005 corollary with RC-002: "This is an explicit exception to EM-023 and
// is keyed on the workflow-library metadata tag workflow_class = reconciliation."
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-005, RC-002;
// specs/reconciliation/schemas.md §6.5.
func TestRC005_WorkflowClassTagIsTheSoleReconciliationDiscriminatorInDOT(t *testing.T) {
	t.Parallel()

	for _, filename := range rc63oh9FixtureS01DOTFiles {
		filename := filename
		t.Run(filename, func(t *testing.T) {
			t.Parallel()
			content := rc63oh9FixtureReadDOT(t, filename)

			// The DOT must carry workflow_class="reconciliation".
			if !strings.Contains(content, `workflow_class="reconciliation"`) {
				t.Errorf("RC-005: %s missing workflow_class=\"reconciliation\" graph attribute; "+
					"this is the sole reconciliation discriminator per RC-002/RC-005", filename)
			}

			// The DOT must NOT carry a category-assignment attribute — that is
			// a detector output, not a library-level annotation.
			if strings.Contains(content, `reconciliation_category`) {
				t.Errorf("RC-005: %s contains 'reconciliation_category' attribute; "+
					"category assignment is a detector output (daemon Go code), NOT a DOT annotation",
					filename)
			}
		})
	}
}
