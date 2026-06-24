package scenario

// sh_inv_003_twin_path_check_test.go — contract tests for the SH-INV-003
// pre-launch twin-binary path-prefix sensor (CheckTwinBinaryPath).
//
// Per specs/scenario-harness.md §5 SH-INV-003: the sensor MUST be called at
// handler-config resolution time (§4.3), before DriveOrchestration. A binary
// whose resolved path is not under any configured twin-search-path prefix MUST
// fail with failure_class=harness-internal-error.
//
// Conformance test per §5 SH-INV-003: a scenario whose agent_overrides
// references /usr/bin/claude (or any binary outside the search-path prefix)
// MUST fail at the pre-launch check, NOT at orchestration time.
//
// Test naming: shINV003TwinPath* (helper prefix per implementer-protocol).
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-003.
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// ErrTwinBinaryOutsideSearchPath sentinel
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV003_SentinelError_IsWrappable verifies that ErrTwinBinaryOutsideSearchPath
// is a non-nil sentinel that can be detected via errors.Is after wrapping.
func TestSHINV003_SentinelError_IsWrappable(t *testing.T) {
	t.Parallel()

	if ErrTwinBinaryOutsideSearchPath == nil {
		t.Fatal("ErrTwinBinaryOutsideSearchPath is nil; must be a non-nil sentinel")
	}

	wrapped := CheckTwinBinaryPath("/usr/bin/claude", []string{"/twins"})
	if wrapped == nil {
		t.Fatal("CheckTwinBinaryPath returned nil for /usr/bin/claude; want error")
	}
	if !errors.Is(wrapped, ErrTwinBinaryOutsideSearchPath) {
		t.Errorf("errors.Is(err, ErrTwinBinaryOutsideSearchPath) = false; want true\nerr: %v", wrapped)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CheckTwinBinaryPath — must-pass cases
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV003_CheckTwinBinaryPath_BinaryDirectlyUnderPrefix verifies that a
// binary file directly inside a search-path prefix is accepted.
func TestSHINV003_CheckTwinBinaryPath_BinaryDirectlyUnderPrefix(t *testing.T) {
	t.Parallel()

	err := CheckTwinBinaryPath("/twins/claude-twin", []string{"/twins"})
	if err != nil {
		t.Errorf("CheckTwinBinaryPath: unexpected error: %v", err)
	}
}

// TestSHINV003_CheckTwinBinaryPath_BinaryInSubdirUnderPrefix verifies that a
// binary nested one level below the search-path prefix is accepted.
func TestSHINV003_CheckTwinBinaryPath_BinaryInSubdirUnderPrefix(t *testing.T) {
	t.Parallel()

	err := CheckTwinBinaryPath("/twins/v1/claude-twin", []string{"/twins"})
	if err != nil {
		t.Errorf("CheckTwinBinaryPath: unexpected error: %v", err)
	}
}

// TestSHINV003_CheckTwinBinaryPath_SecondPrefixMatches verifies that a binary
// under the second search path in a multi-path list is accepted.
func TestSHINV003_CheckTwinBinaryPath_SecondPrefixMatches(t *testing.T) {
	t.Parallel()

	err := CheckTwinBinaryPath(
		"/opt/harmonik-twins/claude-twin",
		[]string{"/twins", "/opt/harmonik-twins"},
	)
	if err != nil {
		t.Errorf("CheckTwinBinaryPath: unexpected error for second prefix: %v", err)
	}
}

// TestSHINV003_CheckTwinBinaryPath_TrailingSlashOnPrefix verifies that a
// search path with a trailing slash is normalised correctly and still accepts
// a binary inside that directory.
func TestSHINV003_CheckTwinBinaryPath_TrailingSlashOnPrefix(t *testing.T) {
	t.Parallel()

	err := CheckTwinBinaryPath("/twins/claude-twin", []string{"/twins/"})
	if err != nil {
		t.Errorf("CheckTwinBinaryPath: trailing-slash prefix rejected valid binary: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CheckTwinBinaryPath — must-fail cases (SH-INV-003 conformance)
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV003_ConformanceMustFail_UsrBinClaude is the normative conformance
// test for SH-INV-003: a binary at /usr/bin/claude (outside the search-path
// prefix) MUST be rejected by the pre-launch sensor.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-003 ("a scenario whose
// agent_overrides references /usr/bin/claude … MUST fail at the pre-launch
// check, NOT at orchestration time").
func TestSHINV003_ConformanceMustFail_UsrBinClaude(t *testing.T) {
	t.Parallel()

	err := CheckTwinBinaryPath("/usr/bin/claude", []string{"/twins"})
	if err == nil {
		t.Fatal("CheckTwinBinaryPath returned nil for /usr/bin/claude; want error (SH-INV-003 pre-launch rejection)")
	}
	if !errors.Is(err, ErrTwinBinaryOutsideSearchPath) {
		t.Errorf("error does not wrap ErrTwinBinaryOutsideSearchPath\nerr: %v", err)
	}
}

// TestSHINV003_MustFail_EmptySearchPaths verifies that an empty twinSearchPaths
// slice always rejects any binary, since there is no prefix to be "under".
func TestSHINV003_MustFail_EmptySearchPaths(t *testing.T) {
	t.Parallel()

	err := CheckTwinBinaryPath("/twins/claude-twin", nil)
	if err == nil {
		t.Fatal("CheckTwinBinaryPath returned nil for empty search paths; want error")
	}
	if !errors.Is(err, ErrTwinBinaryOutsideSearchPath) {
		t.Errorf("error does not wrap ErrTwinBinaryOutsideSearchPath: %v", err)
	}
}

// TestSHINV003_MustFail_EmptyStringSearchPaths verifies that a slice containing
// only empty strings is treated as an empty search path (filepath.Clean("")
// returns ".") and a binary at an absolute path is not "under" it.
func TestSHINV003_MustFail_EmptyStringSearchPaths(t *testing.T) {
	t.Parallel()

	// filepath.Clean("") = ".", filepath.Rel(".", "/twins/claude-twin") returns
	// a path starting with "../..", so /twins/claude-twin is not under ".".
	err := CheckTwinBinaryPath("/twins/claude-twin", []string{""})
	if err == nil {
		t.Fatal("CheckTwinBinaryPath returned nil for empty-string search path against absolute binary; want error")
	}
	if !errors.Is(err, ErrTwinBinaryOutsideSearchPath) {
		t.Errorf("error does not wrap ErrTwinBinaryOutsideSearchPath: %v", err)
	}
}

// TestSHINV003_MustFail_SiblingDirectory verifies that a binary in a directory
// that is a sibling of a search-path prefix is rejected. E.g. /other/claude
// is not under /twins even though both share the same parent.
func TestSHINV003_MustFail_SiblingDirectory(t *testing.T) {
	t.Parallel()

	err := CheckTwinBinaryPath("/other/claude-twin", []string{"/twins"})
	if err == nil {
		t.Fatal("CheckTwinBinaryPath returned nil for sibling-directory binary; want error")
	}
	if !errors.Is(err, ErrTwinBinaryOutsideSearchPath) {
		t.Errorf("error does not wrap ErrTwinBinaryOutsideSearchPath: %v", err)
	}
}

// TestSHINV003_MustFail_TraversalEscape verifies that a binary path containing
// "/../" that would escape the search prefix is rejected. filepath.Clean
// normalises the path before the prefix check, so the traversal cannot bypass
// the sensor (SH-INV-003: predicate is path-prefix-based).
func TestSHINV003_MustFail_TraversalEscape(t *testing.T) {
	t.Parallel()

	// /twins/../usr/bin/claude normalises to /usr/bin/claude via filepath.Clean.
	err := CheckTwinBinaryPath("/twins/../usr/bin/claude", []string{"/twins"})
	if err == nil {
		t.Fatal("CheckTwinBinaryPath returned nil for traversal-escaped binary; want error")
	}
	if !errors.Is(err, ErrTwinBinaryOutsideSearchPath) {
		t.Errorf("error does not wrap ErrTwinBinaryOutsideSearchPath: %v", err)
	}
}

// TestSHINV003_MustFail_NoMatchInMultiplePaths verifies that a binary outside
// all configured search paths is rejected even when multiple paths are set.
func TestSHINV003_MustFail_NoMatchInMultiplePaths(t *testing.T) {
	t.Parallel()

	err := CheckTwinBinaryPath(
		"/usr/bin/claude",
		[]string{"/twins", "/opt/harmonik-twins", "/var/lib/twins"},
	)
	if err == nil {
		t.Fatal("CheckTwinBinaryPath returned nil for binary outside all paths; want error")
	}
	if !errors.Is(err, ErrTwinBinaryOutsideSearchPath) {
		t.Errorf("error does not wrap ErrTwinBinaryOutsideSearchPath: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Error message quality
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV003_ErrorMessageContainsBinaryPath verifies that the error message
// returned by CheckTwinBinaryPath includes the rejected binary path so that
// harness logs are actionable.
func TestSHINV003_ErrorMessageContainsBinaryPath(t *testing.T) {
	t.Parallel()

	binary := "/usr/bin/claude"
	err := CheckTwinBinaryPath(binary, []string{"/twins"})
	if err == nil {
		t.Fatal("CheckTwinBinaryPath returned nil; want error")
	}
	msg := err.Error()
	if !containsSubstring(msg, binary) {
		t.Errorf("error message does not contain binary path %q\nerr: %v", binary, err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Failure class routing
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV003_FailureClass_MapsToHarnessInternalError verifies that the
// FailureClass for a twin-path rejection is FailureClassHarnessInternalError
// per §8.5 closed-list detection item (ii).
//
// The check is performed at the caller layer (the harness must do the mapping);
// this test documents the required classification to protect against caller
// drift.
//
// Spec ref: specs/scenario-harness.md §8.5 closed-list item (ii).
func TestSHINV003_FailureClass_MapsToHarnessInternalError(t *testing.T) {
	t.Parallel()

	// The caller maps ErrTwinBinaryOutsideSearchPath → FailureClassHarnessInternalError.
	// Verify the required FailureClass constant value matches §8.5.
	if FailureClassHarnessInternalError != "harness-internal-error" {
		t.Errorf("FailureClassHarnessInternalError = %q; want %q (§8.5)",
			FailureClassHarnessInternalError, "harness-internal-error")
	}

	// Verify errors.Is detects the sentinel so the caller mapping can use it.
	err := CheckTwinBinaryPath("/usr/bin/claude", []string{"/twins"})
	if !errors.Is(err, ErrTwinBinaryOutsideSearchPath) {
		t.Errorf("errors.Is check failed; caller mapping to FailureClassHarnessInternalError requires this: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario corpus: inv003-outside-search-path.yaml
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV003_CorpusScenarioParsesAndDeclaresBinary verifies that the
// conformance scenario file scenarios/regression/inv003-outside-search-path.yaml
// (a) parses successfully, (b) has cadence_tag=regression, and (c) declares
// an agent_overrides.agent_node.binary that is outside a typical twin search path.
//
// This guards against the scenario file drifting in a way that would break the
// "must-fail at pre-launch" contract it documents.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-003 (conformance scenario obligation).
func TestSHINV003_CorpusScenarioParsesAndDeclaresBinary(t *testing.T) {
	t.Parallel()

	root := conformanceCorpusFixtureRepoRoot(t)
	scenarioPath := filepath.Join(root, "scenarios", "regression", "inv003-outside-search-path.yaml")

	if _, err := os.Stat(scenarioPath); err != nil {
		t.Fatalf("scenario file missing at %q: %v", scenarioPath, err)
	}

	sf, err := ParseScenarioFile(scenarioPath)
	if err != nil {
		t.Fatalf("ParseScenarioFile(%q): %v", scenarioPath, err)
	}

	if !sf.Valid() {
		t.Errorf("ScenarioFile.Valid() = false for %q", scenarioPath)
	}

	if sf.CadenceTag != CadenceTagRegression {
		t.Errorf("CadenceTag = %q, want %q", sf.CadenceTag, CadenceTagRegression)
	}

	ao, ok := sf.AgentOverrides["agent_node"]
	if !ok {
		t.Fatalf("agent_overrides[\"agent_node\"] absent from scenario; SH-INV-003 test requires it")
	}

	// The declared binary must NOT be under /twins (or any typical search path),
	// so that the pre-launch check would reject it at handler-config resolution.
	twinSearchPath := "/twins"
	if err := CheckTwinBinaryPath(ao.Binary, []string{twinSearchPath}); err == nil {
		t.Errorf("binary %q passed CheckTwinBinaryPath with prefix %q; the scenario must use a binary outside the search path to test SH-INV-003",
			ao.Binary, twinSearchPath)
	}
}

// TestSHINV003_SpecCorpus_SensorDeclaredInSpec verifies that the
// scenario-harness spec (v0.2.2) declares SH-INV-003 and the required
// path-prefix predicate tokens. Guards against spec-drift removing the sensor
// obligation.
func TestSHINV003_SpecCorpus_SensorDeclaredInSpec(t *testing.T) {
	t.Parallel()

	root := conformanceCorpusFixtureRepoRoot(t)
	specPath := filepath.Join(root, "specs", "scenario-harness.md")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading spec %q: %v", specPath, err)
	}
	spec := string(data)

	required := []string{
		"SH-INV-003",
		"search-path prefix",
		"/usr/bin/claude",
		"harness-internal-error",
		"pre-launch",
	}
	for _, token := range required {
		if !containsSubstring(spec, token) {
			t.Errorf("spec %q missing required token %q; spec may have drifted", specPath, token)
		}
	}
}
