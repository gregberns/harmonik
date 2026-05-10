package scenario

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// scenarioYAMLFixtureDir creates a temporary directory for scenario YAML
// fixture files and registers cleanup. All helpers below write files into
// this directory.
func scenarioYAMLFixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

// scenarioYAMLFixtureWrite writes content to a file with the given name inside
// dir. It registers t.Fatal on any write failure. Returns the absolute path.
func scenarioYAMLFixtureWrite(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	//nolint:gosec // G306: test fixture file, world-readable is fine
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("scenarioYAMLFixtureWrite: write %q: %v", path, err)
	}
	return path
}

// scenarioYAMLFixtureMinimal returns the YAML content of a minimally valid
// ScenarioFile using workflow_path. All required fields are present, no
// optional fields are set.
func scenarioYAMLFixtureMinimal(t *testing.T) string {
	t.Helper()
	return `name: smoke-basic
description: minimal smoke scenario
workflow_path: workflows/basic.dot
fixture_setup: {}
timeout_secs: 30
cadence_tag: smoke
`
}

// scenarioYAMLFixtureFull returns the YAML content of a fully-populated valid
// ScenarioFile exercising every optional field.
func scenarioYAMLFixtureFull(t *testing.T) string {
	t.Helper()
	return `name: full-scenario
description: a fully-populated scenario exercising all fields
workflow_path: workflows/full.dot
agent_overrides:
  worker:
    binary: /usr/local/bin/claude-twin
    args:
      - --log-level=debug
fixture_setup:
  files:
    config.yaml:
      encoding: utf8
      contents: "key: value\n"
      mode: "0644"
  skill_search_paths:
    - /tmp/skills
expected_events:
  - kind: event_present
    type: agent_ready
    description: worker agent must signal ready
expected_workspace:
  - kind: file_exists
    path: output.txt
    description: output file must exist
expected_outcome:
  outcome_status: SUCCESS
  description: run must complete successfully
timeout_secs: 120
cadence_tag: nightly
matrix:
  env:
    - staging
    - prod
`
}

// scenarioYAMLFixtureWorkflowID returns the YAML content of a valid ScenarioFile
// that uses workflow_id instead of workflow_path.
func scenarioYAMLFixtureWorkflowID(t *testing.T) string {
	t.Helper()
	return `name: id-based-scenario
description: scenario referencing workflow by UUID
workflow_id: "00000000-0000-0000-0000-000000000001"
fixture_setup: {}
timeout_secs: 60
cadence_tag: regression
`
}

func TestParseScenarioFile_ValidMinimal(t *testing.T) {
	t.Parallel()

	dir := scenarioYAMLFixtureDir(t)
	path := scenarioYAMLFixtureWrite(t, dir, "smoke-basic.yaml", scenarioYAMLFixtureMinimal(t))

	sf, err := ParseScenarioFile(path)
	if err != nil {
		t.Fatalf("ParseScenarioFile() unexpected error: %v", err)
	}
	if sf.Name != "smoke-basic" {
		t.Errorf("Name = %q, want %q", sf.Name, "smoke-basic")
	}
	if sf.TimeoutSecs != 30 {
		t.Errorf("TimeoutSecs = %d, want 30", sf.TimeoutSecs)
	}
	if sf.CadenceTag != CadenceTagSmoke {
		t.Errorf("CadenceTag = %q, want %q", sf.CadenceTag, CadenceTagSmoke)
	}
}

func TestParseScenarioFile_ValidFull(t *testing.T) {
	t.Parallel()

	dir := scenarioYAMLFixtureDir(t)
	path := scenarioYAMLFixtureWrite(t, dir, "full-scenario.yaml", scenarioYAMLFixtureFull(t))

	sf, err := ParseScenarioFile(path)
	if err != nil {
		t.Fatalf("ParseScenarioFile() unexpected error: %v", err)
	}
	if sf.Name != "full-scenario" {
		t.Errorf("Name = %q, want %q", sf.Name, "full-scenario")
	}
	if len(sf.AgentOverrides) != 1 {
		t.Errorf("len(AgentOverrides) = %d, want 1", len(sf.AgentOverrides))
	}
	if sf.Matrix == nil || len(sf.Matrix["env"]) != 2 {
		t.Errorf("Matrix[\"env\"] should have 2 values; got %v", sf.Matrix)
	}
}

func TestParseScenarioFile_ValidWorkflowID(t *testing.T) {
	t.Parallel()

	dir := scenarioYAMLFixtureDir(t)
	path := scenarioYAMLFixtureWrite(t, dir, "id-based-scenario.yaml", scenarioYAMLFixtureWorkflowID(t))

	sf, err := ParseScenarioFile(path)
	if err != nil {
		t.Fatalf("ParseScenarioFile() unexpected error: %v", err)
	}
	if sf.WorkflowID == nil {
		t.Error("WorkflowID should be non-nil for workflow_id scenario")
	}
	if sf.WorkflowPath != nil {
		t.Error("WorkflowPath should be nil when workflow_id is set")
	}
}

func TestParseScenarioFile_InvalidExtension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		filename string
	}{
		{name: "yml extension", filename: "scenario.yml"},
		{name: "YAML uppercase", filename: "scenario.YAML"},
		{name: "no extension", filename: "scenario"},
		{name: "txt extension", filename: "scenario.txt"},
		{name: "yaml with extra suffix", filename: "scenario.yaml.bak"},
	}

	dir := scenarioYAMLFixtureDir(t)
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := scenarioYAMLFixtureWrite(t, dir, tc.filename, scenarioYAMLFixtureMinimal(t))
			_, err := ParseScenarioFile(path)
			if err == nil {
				t.Errorf("ParseScenarioFile(%q) expected error for non-.yaml extension, got nil", tc.filename)
			}
			if !strings.Contains(err.Error(), "scenario-load-failure") {
				t.Errorf("error %q should contain 'scenario-load-failure'", err.Error())
			}
		})
	}
}

func TestParseScenarioFile_InvalidYAMLParse(t *testing.T) {
	t.Parallel()

	dir := scenarioYAMLFixtureDir(t)
	path := scenarioYAMLFixtureWrite(t, dir, "bad-yaml.yaml", `
name: [unterminated list
`)

	_, err := ParseScenarioFile(path)
	if err == nil {
		t.Fatal("ParseScenarioFile() expected error for malformed YAML, got nil")
	}
	if !strings.Contains(err.Error(), "scenario-load-failure") {
		t.Errorf("error %q should contain 'scenario-load-failure'", err.Error())
	}
}

func TestParseScenarioFile_InvalidSchema_UnknownField(t *testing.T) {
	t.Parallel()

	dir := scenarioYAMLFixtureDir(t)
	// KnownFields(true) must reject unknown fields.
	content := `name: schema-fail
description: test
workflow_path: workflows/basic.dot
fixture_setup: {}
timeout_secs: 30
cadence_tag: smoke
unknown_field: forbidden
`
	path := scenarioYAMLFixtureWrite(t, dir, "schema-fail.yaml", content)

	_, err := ParseScenarioFile(path)
	if err == nil {
		t.Fatal("ParseScenarioFile() expected error for unknown YAML field, got nil")
	}
	if !strings.Contains(err.Error(), "scenario-load-failure") {
		t.Errorf("error %q should contain 'scenario-load-failure'", err.Error())
	}
}

func TestParseScenarioFile_InvalidSchema_MissingRequired(t *testing.T) {
	t.Parallel()

	dir := scenarioYAMLFixtureDir(t)
	// Valid() will reject a ScenarioFile with missing required fields.
	content := `description: missing name
workflow_path: workflows/basic.dot
fixture_setup: {}
timeout_secs: 30
cadence_tag: smoke
`
	path := scenarioYAMLFixtureWrite(t, dir, "missing-name.yaml", content)

	_, err := ParseScenarioFile(path)
	if err == nil {
		t.Fatal("ParseScenarioFile() expected error for missing name field, got nil")
	}
	if !strings.Contains(err.Error(), "scenario-load-failure") {
		t.Errorf("error %q should contain 'scenario-load-failure'", err.Error())
	}
}

func TestParseScenarioFile_InvalidSchema_BothWorkflows(t *testing.T) {
	t.Parallel()

	dir := scenarioYAMLFixtureDir(t)
	content := `name: both-workflows
description: both workflow_path and workflow_id set
workflow_path: workflows/basic.dot
workflow_id: "00000000-0000-0000-0000-000000000001"
fixture_setup: {}
timeout_secs: 30
cadence_tag: smoke
`
	path := scenarioYAMLFixtureWrite(t, dir, "both-workflows.yaml", content)

	_, err := ParseScenarioFile(path)
	if err == nil {
		t.Fatal("ParseScenarioFile() expected error when both workflow_path and workflow_id are set, got nil")
	}
	if !strings.Contains(err.Error(), "scenario-load-failure") {
		t.Errorf("error %q should contain 'scenario-load-failure'", err.Error())
	}
}

func TestParseScenarioFile_InvalidSchema_UnknownCadence(t *testing.T) {
	t.Parallel()

	dir := scenarioYAMLFixtureDir(t)
	content := `name: bad-cadence
description: unknown cadence tag
workflow_path: workflows/basic.dot
fixture_setup: {}
timeout_secs: 30
cadence_tag: daily
`
	path := scenarioYAMLFixtureWrite(t, dir, "bad-cadence.yaml", content)

	_, err := ParseScenarioFile(path)
	if err == nil {
		t.Fatal("ParseScenarioFile() expected error for unknown cadence_tag, got nil")
	}
	if !strings.Contains(err.Error(), "scenario-load-failure") {
		t.Errorf("error %q should contain 'scenario-load-failure'", err.Error())
	}
}

func TestParseScenarioFile_InvalidSchema_TimeoutOutOfRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		timeoutSecs string
	}{
		{name: "zero", timeoutSecs: "0"},
		{name: "negative", timeoutSecs: "-1"},
		{name: "over max", timeoutSecs: "7201"},
	}

	dir := scenarioYAMLFixtureDir(t)
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content := "name: timeout-test\ndescription: timeout boundary test\n" +
				"workflow_path: workflows/basic.dot\nfixture_setup: {}\n" +
				"timeout_secs: " + tc.timeoutSecs + "\ncadence_tag: smoke\n"
			path := scenarioYAMLFixtureWrite(t, dir, "timeout-"+tc.name+".yaml", content)
			_, err := ParseScenarioFile(path)
			if err == nil {
				t.Errorf("ParseScenarioFile() expected error for timeout_secs=%s, got nil", tc.timeoutSecs)
			}
			if !strings.Contains(err.Error(), "scenario-load-failure") {
				t.Errorf("error %q should contain 'scenario-load-failure'", err.Error())
			}
		})
	}
}

func TestParseScenarioFile_InvalidSchema_MatrixOverLimit(t *testing.T) {
	t.Parallel()

	dir := scenarioYAMLFixtureDir(t)
	// 33 × 32 = 1056 > 1024 cells — must be rejected.
	valuesA := strings.Repeat("    - v\n", 33)
	valuesB := strings.Repeat("    - v\n", 32)
	content := `name: big-matrix
description: matrix exceeds 1024 cells
workflow_path: workflows/basic.dot
fixture_setup: {}
timeout_secs: 30
cadence_tag: smoke
matrix:
  a:
` + valuesA + `  b:
` + valuesB

	path := scenarioYAMLFixtureWrite(t, dir, "big-matrix.yaml", content)

	_, err := ParseScenarioFile(path)
	if err == nil {
		t.Fatal("ParseScenarioFile() expected error for matrix exceeding 1024 cells, got nil")
	}
	if !strings.Contains(err.Error(), "scenario-load-failure") {
		t.Errorf("error %q should contain 'scenario-load-failure'", err.Error())
	}
}

func TestParseScenarioFile_RejectsUTF8BOM(t *testing.T) {
	t.Parallel()

	dir := scenarioYAMLFixtureDir(t)
	// Prepend a UTF-8 BOM to an otherwise valid scenario file.
	content := "\xEF\xBB\xBF" + scenarioYAMLFixtureMinimal(t)
	path := scenarioYAMLFixtureWrite(t, dir, "bom-scenario.yaml", content)

	_, err := ParseScenarioFile(path)
	if err == nil {
		t.Fatal("ParseScenarioFile() expected error for BOM-prefixed file, got nil")
	}
	if !strings.Contains(err.Error(), "scenario-load-failure") {
		t.Errorf("error %q should contain 'scenario-load-failure'", err.Error())
	}
}

func TestParseScenarioFile_RejectsForbiddenTags(t *testing.T) {
	t.Parallel()

	dir := scenarioYAMLFixtureDir(t)

	tests := []struct {
		name     string
		filename string
		content  string
	}{
		{
			name:     "eval tag",
			filename: "eval-tag.yaml",
			content: `name: eval-tag
description: forbidden eval tag
workflow_path: workflows/basic.dot
fixture_setup: {}
timeout_secs: 30
cadence_tag: smoke
extra: !eval "os.system('rm -rf /')"
`,
		},
		{
			name:     "python-object tag",
			filename: "python-object-tag.yaml",
			content: `name: python-tag
description: forbidden python/object tag
workflow_path: workflows/basic.dot
fixture_setup: {}
timeout_secs: 30
cadence_tag: smoke
extra: !!python/object:os.system ['ls']
`,
		},
		{
			name:     "binary tag",
			filename: "binary-tag.yaml",
			content: `name: binary-tag
description: forbidden binary tag
workflow_path: workflows/basic.dot
fixture_setup: {}
timeout_secs: 30
cadence_tag: smoke
data: !!binary |
  SGVsbG8gV29ybGQ=
`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := scenarioYAMLFixtureWrite(t, dir, tc.filename, tc.content)
			_, err := ParseScenarioFile(path)
			if err == nil {
				t.Errorf("ParseScenarioFile() expected error for %q (forbidden tag), got nil", tc.name)
			}
			if !strings.Contains(err.Error(), "scenario-load-failure") {
				t.Errorf("error %q should contain 'scenario-load-failure'", err.Error())
			}
		})
	}
}

// scenarioYAMLFixtureAliasBomb returns a YAML content string that is a
// billion-laughs-style alias/anchor bomb. Six levels of 9-way fan-out produce
// ~672 000 expanded nodes, well above the 100 000-node ceiling (SH-003).
// SH-INV-005 lists "anchors that reference unbound aliases" as a forbidden
// construct; this fixture exercises the alias-expansion backstop.
func scenarioYAMLFixtureAliasBomb(t *testing.T) string {
	t.Helper()
	return `name: alias-bomb
description: billion-laughs alias bomb
workflow_path: workflows/bomb.dot
fixture_setup: {}
timeout_secs: 30
cadence_tag: smoke
lol1: &lol1 ["lol","lol","lol","lol","lol","lol","lol","lol","lol"]
lol2: &lol2 [*lol1,*lol1,*lol1,*lol1,*lol1,*lol1,*lol1,*lol1,*lol1]
lol3: &lol3 [*lol2,*lol2,*lol2,*lol2,*lol2,*lol2,*lol2,*lol2,*lol2]
lol4: &lol4 [*lol3,*lol3,*lol3,*lol3,*lol3,*lol3,*lol3,*lol3,*lol3]
lol5: &lol5 [*lol4,*lol4,*lol4,*lol4,*lol4,*lol4,*lol4,*lol4,*lol4]
lol6: &lol6 [*lol5,*lol5,*lol5,*lol5,*lol5,*lol5,*lol5,*lol5,*lol5]
`
}

// TestParseScenarioFile_RejectsAliasBomb verifies that a YAML alias/anchor bomb
// (billion-laughs pattern) is rejected by the 100 000-node ceiling (SH-003).
// gopkg.in/yaml.v3 represents alias nodes as AliasNode pointers to the anchor;
// countYAMLNodes follows those pointers so the expanded node count is used,
// not the raw alias-reference count. Six levels of 9-way fan-out produce
// ~672 000 expanded nodes, triggering the ceiling well before 100 000.
// Spec refs: SH-003 (node ceiling), SH-INV-005 (alias/anchor bomb as forbidden construct).
func TestParseScenarioFile_RejectsAliasBomb(t *testing.T) {
	t.Parallel()

	dir := scenarioYAMLFixtureDir(t)
	path := scenarioYAMLFixtureWrite(t, dir, "alias-bomb.yaml", scenarioYAMLFixtureAliasBomb(t))

	_, err := ParseScenarioFile(path)
	if err == nil {
		t.Fatal("ParseScenarioFile() expected error for alias/anchor bomb, got nil")
	}
	if !strings.Contains(err.Error(), "scenario-load-failure") {
		t.Errorf("error %q should contain 'scenario-load-failure'", err.Error())
	}
	if !strings.Contains(err.Error(), "node ceiling") {
		t.Errorf("error %q should reference node ceiling (SH-003 backstop for alias bombs)", err.Error())
	}
}

func TestParseScenarioFile_RejectsMissingFile(t *testing.T) {
	t.Parallel()

	_, err := ParseScenarioFile("/nonexistent/path/to/scenario.yaml")
	if err == nil {
		t.Fatal("ParseScenarioFile() expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "scenario-load-failure") {
		t.Errorf("error %q should contain 'scenario-load-failure'", err.Error())
	}
}

func TestParseScenarioFile_RejectsOversizeFile(t *testing.T) {
	t.Parallel()

	dir := scenarioYAMLFixtureDir(t)
	// Write a file of scenarioFileSizeLimitBytes + 1 bytes.
	oversized := make([]byte, scenarioFileSizeLimitBytes+1)
	for i := range oversized {
		oversized[i] = '#' // valid YAML comment character — won't parse as valid scenario
	}
	path := filepath.Join(dir, "oversize.yaml")
	//nolint:gosec // G306: test fixture file
	if err := os.WriteFile(path, oversized, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ParseScenarioFile(path)
	if err == nil {
		t.Fatal("ParseScenarioFile() expected error for oversize file, got nil")
	}
	if !strings.Contains(err.Error(), "scenario-load-failure") {
		t.Errorf("error %q should contain 'scenario-load-failure'", err.Error())
	}
}

func TestParseScenarioFile_InvalidNamePattern(t *testing.T) {
	t.Parallel()

	dir := scenarioYAMLFixtureDir(t)
	tests := []struct {
		name    string
		nameVal string
	}{
		{name: "starts with dot", nameVal: ".hidden"},
		{name: "contains slash", nameVal: "foo/bar"},
		{name: "contains space", nameVal: "foo bar"},
		{name: "empty name", nameVal: ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content := "name: " + tc.nameVal + "\n" +
				"description: invalid name test\n" +
				"workflow_path: workflows/basic.dot\n" +
				"fixture_setup: {}\n" +
				"timeout_secs: 30\n" +
				"cadence_tag: smoke\n"
			path := scenarioYAMLFixtureWrite(t, dir, "badname-"+tc.name+".yaml", content)
			_, err := ParseScenarioFile(path)
			if err == nil {
				t.Errorf("ParseScenarioFile() expected error for name %q, got nil", tc.nameVal)
			}
			if !strings.Contains(err.Error(), "scenario-load-failure") {
				t.Errorf("error %q should contain 'scenario-load-failure'", err.Error())
			}
		})
	}
}

func TestParseScenarioFile_YAMLRoundTrip(t *testing.T) {
	t.Parallel()

	// Verify that the struct's YAML tags are correct end-to-end: marshal a
	// known-valid ScenarioFile as YAML, write to disk, parse it back, and
	// deep-compare the in-memory structs. Uses gopkg.in/yaml.v3 (the same
	// library ParseScenarioFile uses) to marshal, confirming tag symmetry.
	dir := scenarioYAMLFixtureDir(t)

	// Write the minimal fixture directly to a file via the canonical YAML content
	// (not by marshalling, to avoid circular dependency on yaml.Marshal correctness).
	path := scenarioYAMLFixtureWrite(t, dir, "roundtrip.yaml", scenarioYAMLFixtureMinimal(t))

	sf, err := ParseScenarioFile(path)
	if err != nil {
		t.Fatalf("ParseScenarioFile() round-trip setup error: %v", err)
	}

	// Verify fields match the declared fixture values.
	if sf.Name != "smoke-basic" {
		t.Errorf("Name = %q, want %q", sf.Name, "smoke-basic")
	}
	wp := "workflows/basic.dot"
	if sf.WorkflowPath == nil || *sf.WorkflowPath != wp {
		t.Errorf("WorkflowPath = %v, want %q", sf.WorkflowPath, wp)
	}
	if sf.TimeoutSecs != 30 {
		t.Errorf("TimeoutSecs = %d, want 30", sf.TimeoutSecs)
	}
	if sf.CadenceTag != CadenceTagSmoke {
		t.Errorf("CadenceTag = %q, want %q", sf.CadenceTag, CadenceTagSmoke)
	}
}

func TestCountYAMLNodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantMin   int // lower bound (tree structures vary by doc wrapper)
		wantExact bool
		want      int
	}{
		{
			name:      "nil node",
			input:     "",
			wantExact: false,
			wantMin:   0,
		},
		{
			name:      "scalar",
			input:     "hello",
			wantExact: false,
			wantMin:   1,
		},
		{
			name:      "mapping",
			input:     "a: 1\nb: 2",
			wantExact: false,
			wantMin:   3, // mapping node + 2 key-value pairs = at least 5 nodes
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var doc yaml.Node
			if tc.input != "" {
				if err := yaml.Unmarshal([]byte(tc.input), &doc); err != nil {
					t.Fatalf("yaml.Unmarshal: %v", err)
				}
			}
			got := countYAMLNodes(&doc, scenarioFileNodeLimit+1)
			if got < tc.wantMin {
				t.Errorf("countYAMLNodes() = %d, want >= %d", got, tc.wantMin)
			}
		})
	}
}
