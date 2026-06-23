package scenario

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// TestSH030_SyntheticMatrixName verifies the synthetic name format
// <baseName>[k1=v1,k2=v2,...] with byte-lexicographic key order per SH-030.
func TestSH030_SyntheticMatrixName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		baseName string
		cell     map[string]string
		want     string
	}{
		{
			name:     "single key",
			baseName: "my-scenario",
			cell:     map[string]string{"env": "staging"},
			want:     "my-scenario[env=staging]",
		},
		{
			name:     "two keys byte-lex ordered",
			baseName: "scenario",
			cell:     map[string]string{"version": "v2", "env": "prod"},
			want:     "scenario[env=prod,version=v2]",
		},
		{
			name:     "three keys byte-lex ordered",
			baseName: "s",
			cell:     map[string]string{"z": "last", "a": "first", "m": "mid"},
			want:     "s[a=first,m=mid,z=last]",
		},
		{
			name:     "empty cell returns base name unchanged",
			baseName: "base",
			cell:     map[string]string{},
			want:     "base",
		},
		{
			name:     "value containing equals sign",
			baseName: "t",
			cell:     map[string]string{"k": "a=b"},
			want:     "t[k=a=b]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := SyntheticMatrixName(tc.baseName, tc.cell)
			if got != tc.want {
				t.Errorf("SyntheticMatrixName(%q, %v) = %q; want %q", tc.baseName, tc.cell, got, tc.want)
			}
		})
	}
}

// TestSH030_MatrixCartesianProduct verifies the cartesian product generation.
func TestSH030_MatrixCartesianProduct(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		matrix    map[string][]string
		wantCount int
		wantKeys  []string // all cells should contain exactly these keys
	}{
		{
			name:      "single key two values",
			matrix:    map[string][]string{"env": {"staging", "prod"}},
			wantCount: 2,
			wantKeys:  []string{"env"},
		},
		{
			name: "two keys two values each",
			matrix: map[string][]string{
				"env":     {"staging", "prod"},
				"version": {"v1", "v2"},
			},
			wantCount: 4,
			wantKeys:  []string{"env", "version"},
		},
		{
			name: "three keys",
			matrix: map[string][]string{
				"a": {"1", "2"},
				"b": {"x"},
				"c": {"p", "q"},
			},
			wantCount: 4, // 2 * 1 * 2
			wantKeys:  []string{"a", "b", "c"},
		},
		{
			name:      "empty matrix",
			matrix:    map[string][]string{},
			wantCount: 1, // degenerate: one empty cell
			wantKeys:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cells := matrixCartesianProduct(tc.matrix)
			if len(cells) != tc.wantCount {
				t.Fatalf("matrixCartesianProduct(%v) produced %d cells; want %d", tc.matrix, len(cells), tc.wantCount)
			}
			for _, cell := range cells {
				for _, k := range tc.wantKeys {
					if _, ok := cell[k]; !ok {
						t.Errorf("cell %v missing key %q", cell, k)
					}
				}
				if len(cell) != len(tc.wantKeys) {
					t.Errorf("cell %v has %d keys; want %d", cell, len(cell), len(tc.wantKeys))
				}
			}
		})
	}
}

// TestSH030_CartesianProductCoverage verifies all value combinations appear exactly once.
func TestSH030_CartesianProductCoverage(t *testing.T) {
	t.Parallel()

	matrix := map[string][]string{
		"env":     {"staging", "prod"},
		"version": {"v1", "v2"},
	}
	cells := matrixCartesianProduct(matrix)

	type pair struct{ env, version string }
	seen := make(map[pair]int)
	for _, cell := range cells {
		seen[pair{cell["env"], cell["version"]}]++
	}

	want := []pair{
		{"staging", "v1"},
		{"staging", "v2"},
		{"prod", "v1"},
		{"prod", "v2"},
	}
	for _, p := range want {
		if seen[p] != 1 {
			t.Errorf("pair %+v appeared %d times; want exactly 1", p, seen[p])
		}
	}
}

// TestSH030_SubstituteString verifies template substitution and error paths.
func TestSH030_SubstituteString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		s         string
		params    map[string]string
		want      string
		wantError bool
	}{
		{
			name:   "no template markers: fast path",
			s:      "hello world",
			params: map[string]string{"env": "prod"},
			want:   "hello world",
		},
		{
			name:   "simple substitution",
			s:      "deploy to {{.env}}",
			params: map[string]string{"env": "staging"},
			want:   "deploy to staging",
		},
		{
			name:   "multiple substitutions",
			s:      "{{.env}}-{{.version}}",
			params: map[string]string{"env": "prod", "version": "v2"},
			want:   "prod-v2",
		},
		{
			name:   "empty string",
			s:      "",
			params: map[string]string{},
			want:   "",
		},
		{
			name:      "unknown parameter key",
			s:         "{{.missing}}",
			params:    map[string]string{"env": "staging"},
			wantError: true,
		},
		{
			name:      "template parse error",
			s:         "{{.unclosed",
			params:    map[string]string{},
			wantError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := substituteString(tc.s, tc.params)
			if tc.wantError {
				if err == nil {
					t.Errorf("substituteString(%q, %v) returned nil error; want error", tc.s, tc.params)
				}
				return
			}
			if err != nil {
				t.Fatalf("substituteString(%q, %v) returned unexpected error: %v", tc.s, tc.params, err)
			}
			if got != tc.want {
				t.Errorf("substituteString(%q, %v) = %q; want %q", tc.s, tc.params, got, tc.want)
			}
		})
	}
}

// TestSH030_ExpandMatrix_NilMatrix verifies that a nil matrix returns the
// scenario unchanged in a single-element slice.
func TestSH030_ExpandMatrix_NilMatrix(t *testing.T) {
	t.Parallel()

	wp := "workflows/basic.dot"
	sf := ScenarioFile{
		Name:         "no-matrix",
		Description:  "no matrix declared",
		WorkflowPath: &wp,
		TimeoutSecs:  30,
		CadenceTag:   CadenceTagSmoke,
		Matrix:       nil,
	}

	cells, err := sf.ExpandMatrix()
	if err != nil {
		t.Fatalf("ExpandMatrix() error = %v; want nil", err)
	}
	if len(cells) != 1 {
		t.Fatalf("ExpandMatrix() returned %d cells; want 1", len(cells))
	}
	if cells[0].Name != "no-matrix" {
		t.Errorf("cell[0].Name = %q; want %q", cells[0].Name, "no-matrix")
	}
	if cells[0].Matrix != nil {
		t.Errorf("cell[0].Matrix should be nil; got %v", cells[0].Matrix)
	}
}

// TestSH030_ExpandMatrix_SingleKey verifies expansion of a single-key matrix
// into one cell per value, sorted by synthetic name.
func TestSH030_ExpandMatrix_SingleKey(t *testing.T) {
	t.Parallel()

	wp := "workflows/basic.dot"
	sf := ScenarioFile{
		Name:         "my-scenario",
		Description:  "running on {{.env}}",
		WorkflowPath: &wp,
		TimeoutSecs:  30,
		CadenceTag:   CadenceTagSmoke,
		Matrix: map[string][]string{
			"env": {"prod", "staging"}, // prod < staging lexicographically
		},
	}

	cells, err := sf.ExpandMatrix()
	if err != nil {
		t.Fatalf("ExpandMatrix() error = %v; want nil", err)
	}
	if len(cells) != 2 {
		t.Fatalf("ExpandMatrix() returned %d cells; want 2", len(cells))
	}

	// Sorted by synthetic name: my-scenario[env=prod] < my-scenario[env=staging]
	if cells[0].Name != "my-scenario[env=prod]" {
		t.Errorf("cells[0].Name = %q; want %q", cells[0].Name, "my-scenario[env=prod]")
	}
	if cells[1].Name != "my-scenario[env=staging]" {
		t.Errorf("cells[1].Name = %q; want %q", cells[1].Name, "my-scenario[env=staging]")
	}

	// Template substitution applied to description.
	if cells[0].Description != "running on prod" {
		t.Errorf("cells[0].Description = %q; want %q", cells[0].Description, "running on prod")
	}
	if cells[1].Description != "running on staging" {
		t.Errorf("cells[1].Description = %q; want %q", cells[1].Description, "running on staging")
	}

	// Matrix field cleared on each cell.
	for i, c := range cells {
		if c.Matrix != nil {
			t.Errorf("cells[%d].Matrix should be nil after expansion; got %v", i, c.Matrix)
		}
	}
}

// TestSH030_ExpandMatrix_TwoKeys verifies 2×2 cartesian product and name ordering.
func TestSH030_ExpandMatrix_TwoKeys(t *testing.T) {
	t.Parallel()

	wp := "w.dot"
	sf := ScenarioFile{
		Name:         "t",
		Description:  "{{.env}}/{{.ver}}",
		WorkflowPath: &wp,
		TimeoutSecs:  10,
		CadenceTag:   CadenceTagSmoke,
		Matrix: map[string][]string{
			"env": {"a", "b"},
			"ver": {"1", "2"},
		},
	}

	cells, err := sf.ExpandMatrix()
	if err != nil {
		t.Fatalf("ExpandMatrix() error = %v; want nil", err)
	}
	if len(cells) != 4 {
		t.Fatalf("ExpandMatrix() returned %d cells; want 4", len(cells))
	}

	// Verify sorted order: t[env=a,ver=1] < t[env=a,ver=2] < t[env=b,ver=1] < t[env=b,ver=2]
	wantNames := []string{
		"t[env=a,ver=1]",
		"t[env=a,ver=2]",
		"t[env=b,ver=1]",
		"t[env=b,ver=2]",
	}
	for i, want := range wantNames {
		if cells[i].Name != want {
			t.Errorf("cells[%d].Name = %q; want %q", i, cells[i].Name, want)
		}
	}

	// Verify description substitution for first and last cells.
	if cells[0].Description != "a/1" {
		t.Errorf("cells[0].Description = %q; want %q", cells[0].Description, "a/1")
	}
	if cells[3].Description != "b/2" {
		t.Errorf("cells[3].Description = %q; want %q", cells[3].Description, "b/2")
	}
}

// TestSH030_ExpandMatrix_NameCollision verifies that duplicate matrix values
// producing identical synthetic names fail with scenario-load-failure.
func TestSH030_ExpandMatrix_NameCollision(t *testing.T) {
	t.Parallel()

	wp := "w.dot"
	sf := ScenarioFile{
		Name:         "dup",
		Description:  "d",
		WorkflowPath: &wp,
		TimeoutSecs:  10,
		CadenceTag:   CadenceTagSmoke,
		Matrix: map[string][]string{
			"env": {"prod", "prod"}, // duplicate value → same synthetic name
		},
	}

	_, err := sf.ExpandMatrix()
	if err == nil {
		t.Fatal("ExpandMatrix() returned nil error; want scenario-load-failure for duplicate name")
	}
	if !strings.Contains(err.Error(), "scenario-load-failure") {
		t.Errorf("error %q should contain 'scenario-load-failure'", err.Error())
	}
}

// TestSH030_ExpandMatrix_UnknownParameter verifies that a template referencing
// a parameter not declared in the matrix fails at scenario-load time per SH-030.
func TestSH030_ExpandMatrix_UnknownParameter(t *testing.T) {
	t.Parallel()

	wp := "w.dot"
	sf := ScenarioFile{
		Name:         "t",
		Description:  "{{.missing_key}}",
		WorkflowPath: &wp,
		TimeoutSecs:  10,
		CadenceTag:   CadenceTagSmoke,
		Matrix: map[string][]string{
			"env": {"staging"},
		},
	}

	_, err := sf.ExpandMatrix()
	if err == nil {
		t.Fatal("ExpandMatrix() returned nil error; want error for unknown parameter")
	}
	if !strings.Contains(err.Error(), "scenario-load-failure") {
		t.Errorf("error %q should contain 'scenario-load-failure'", err.Error())
	}
}

// TestSH030_ExpandMatrix_WorkflowPathSubstitution verifies template substitution
// in the WorkflowPath field.
func TestSH030_ExpandMatrix_WorkflowPathSubstitution(t *testing.T) {
	t.Parallel()

	path := "workflows/{{.env}}.dot"
	sf := ScenarioFile{
		Name:         "path-test",
		Description:  "d",
		WorkflowPath: &path,
		TimeoutSecs:  10,
		CadenceTag:   CadenceTagSmoke,
		Matrix: map[string][]string{
			"env": {"staging"},
		},
	}

	cells, err := sf.ExpandMatrix()
	if err != nil {
		t.Fatalf("ExpandMatrix() error = %v; want nil", err)
	}
	if len(cells) != 1 {
		t.Fatalf("want 1 cell; got %d", len(cells))
	}
	if cells[0].WorkflowPath == nil || *cells[0].WorkflowPath != "workflows/staging.dot" {
		t.Errorf("WorkflowPath = %v; want %q", cells[0].WorkflowPath, "workflows/staging.dot")
	}
}

// TestSH030_ExpandMatrix_AgentOverridesSubstitution verifies template substitution
// in AgentOverride.Binary and Args fields.
func TestSH030_ExpandMatrix_AgentOverridesSubstitution(t *testing.T) {
	t.Parallel()

	wp := "w.dot"
	sf := ScenarioFile{
		Name:         "agent-test",
		Description:  "d",
		WorkflowPath: &wp,
		AgentOverrides: map[string]AgentOverride{
			"worker": {
				Binary: "twins/{{.env}}-twin",
				Args:   []string{"--mode={{.env}}"},
			},
		},
		TimeoutSecs: 10,
		CadenceTag:  CadenceTagSmoke,
		Matrix: map[string][]string{
			"env": {"staging"},
		},
	}

	cells, err := sf.ExpandMatrix()
	if err != nil {
		t.Fatalf("ExpandMatrix() error = %v; want nil", err)
	}
	if len(cells) != 1 {
		t.Fatalf("want 1 cell; got %d", len(cells))
	}

	ao := cells[0].AgentOverrides["worker"]
	if ao.Binary != "twins/staging-twin" {
		t.Errorf("Binary = %q; want %q", ao.Binary, "twins/staging-twin")
	}
	if len(ao.Args) != 1 || ao.Args[0] != "--mode=staging" {
		t.Errorf("Args = %v; want [\"--mode=staging\"]", ao.Args)
	}
}

// TestSH030_ExpandMatrix_FixtureSetupSubstitution verifies template substitution
// in FixtureSetup fields.
func TestSH030_ExpandMatrix_FixtureSetupSubstitution(t *testing.T) {
	t.Parallel()

	wp := "w.dot"
	sf := ScenarioFile{
		Name:         "fs-test",
		Description:  "d",
		WorkflowPath: &wp,
		FixtureSetup: FixtureSetup{
			Files: map[string]FileSeed{
				"config-{{.env}}.yaml": {Contents: "env: {{.env}}", Encoding: ""},
			},
			SkillSearchPaths: []string{"/opt/{{.env}}/skills"},
		},
		TimeoutSecs: 10,
		CadenceTag:  CadenceTagSmoke,
		Matrix: map[string][]string{
			"env": {"prod"},
		},
	}

	cells, err := sf.ExpandMatrix()
	if err != nil {
		t.Fatalf("ExpandMatrix() error = %v; want nil", err)
	}
	if len(cells) != 1 {
		t.Fatalf("want 1 cell; got %d", len(cells))
	}

	c := cells[0]
	seed, ok := c.FixtureSetup.Files["config-prod.yaml"]
	if !ok {
		t.Errorf("expected file key 'config-prod.yaml' not found; got %v", c.FixtureSetup.Files)
	} else if seed.Contents != "env: prod" {
		t.Errorf("file contents = %q; want %q", seed.Contents, "env: prod")
	}

	if len(c.FixtureSetup.SkillSearchPaths) != 1 || c.FixtureSetup.SkillSearchPaths[0] != "/opt/prod/skills" {
		t.Errorf("SkillSearchPaths = %v; want [/opt/prod/skills]", c.FixtureSetup.SkillSearchPaths)
	}
}

// TestSH030_ExpandMatrix_ExpectedEventsSubstitution verifies template substitution
// in ExpectedEvents description and string payload_match values.
func TestSH030_ExpandMatrix_ExpectedEventsSubstitution(t *testing.T) {
	t.Parallel()

	wp := "w.dot"
	sf := ScenarioFile{
		Name:         "ev-test",
		Description:  "d",
		WorkflowPath: &wp,
		ExpectedEvents: []EventExpectation{
			{
				Kind:        EventExpectationKindPresent,
				Type:        core.EventType("agent_ready"),
				Description: "agent ready in {{.env}}",
				PayloadMatch: map[string]any{
					"env":   "{{.env}}",
					"count": 42, // non-string: should not be substituted
				},
			},
		},
		TimeoutSecs: 10,
		CadenceTag:  CadenceTagSmoke,
		Matrix: map[string][]string{
			"env": {"staging"},
		},
	}

	cells, err := sf.ExpandMatrix()
	if err != nil {
		t.Fatalf("ExpandMatrix() error = %v; want nil", err)
	}
	if len(cells) != 1 {
		t.Fatalf("want 1 cell; got %d", len(cells))
	}

	ee := cells[0].ExpectedEvents[0]
	if ee.Description != "agent ready in staging" {
		t.Errorf("Description = %q; want %q", ee.Description, "agent ready in staging")
	}
	if v, ok := ee.PayloadMatch["env"]; !ok || v != "staging" {
		t.Errorf("payload_match[env] = %v; want %q", v, "staging")
	}
	if v, ok := ee.PayloadMatch["count"]; !ok || v != 42 {
		t.Errorf("payload_match[count] = %v; want 42 (non-string preserved)", v)
	}
}

// TestSH030_ExpandMatrix_ExpectedWorkspaceSubstitution verifies template substitution
// in ExpectedWorkspace path, expected, and description fields.
func TestSH030_ExpandMatrix_ExpectedWorkspaceSubstitution(t *testing.T) {
	t.Parallel()

	expected := "content for {{.env}}"
	wp := "w.dot"
	sf := ScenarioFile{
		Name:         "ws-test",
		Description:  "d",
		WorkflowPath: &wp,
		ExpectedWorkspace: []WorkspacePredicate{
			{
				Kind:        WorkspacePredicateKindFileContentsEqual,
				Path:        "{{.env}}/config.yaml",
				Expected:    &expected,
				Description: "config for {{.env}}",
			},
		},
		TimeoutSecs: 10,
		CadenceTag:  CadenceTagSmoke,
		Matrix: map[string][]string{
			"env": {"dev"},
		},
	}

	cells, err := sf.ExpandMatrix()
	if err != nil {
		t.Fatalf("ExpandMatrix() error = %v; want nil", err)
	}
	if len(cells) != 1 {
		t.Fatalf("want 1 cell; got %d", len(cells))
	}

	wp2 := cells[0].ExpectedWorkspace[0]
	if wp2.Path != "dev/config.yaml" {
		t.Errorf("Path = %q; want %q", wp2.Path, "dev/config.yaml")
	}
	if wp2.Expected == nil || *wp2.Expected != "content for dev" {
		t.Errorf("Expected = %v; want %q", wp2.Expected, "content for dev")
	}
	if wp2.Description != "config for dev" {
		t.Errorf("Description = %q; want %q", wp2.Description, "config for dev")
	}
}

// TestSH030_ExpandMatrix_OutcomeDescriptionSubstitution verifies template
// substitution in ExpectedOutcome.Description.
func TestSH030_ExpandMatrix_OutcomeDescriptionSubstitution(t *testing.T) {
	t.Parallel()

	wp := "w.dot"
	outcome := OutcomeExpectation{
		OutcomeStatus: core.OutcomeStatusSuccess,
		Description:   "scenario {{.env}} must succeed",
	}
	sf := ScenarioFile{
		Name:            "oc-test",
		Description:     "d",
		WorkflowPath:    &wp,
		ExpectedOutcome: &outcome,
		TimeoutSecs:     10,
		CadenceTag:      CadenceTagSmoke,
		Matrix: map[string][]string{
			"env": {"prod"},
		},
	}

	cells, err := sf.ExpandMatrix()
	if err != nil {
		t.Fatalf("ExpandMatrix() error = %v; want nil", err)
	}
	if len(cells) != 1 {
		t.Fatalf("want 1 cell; got %d", len(cells))
	}

	got := cells[0].ExpectedOutcome.Description
	if got != "scenario prod must succeed" {
		t.Errorf("OutcomeExpectation.Description = %q; want %q", got, "scenario prod must succeed")
	}
}

// TestSH030_ExpandMatrix_Base64ContentsNotSubstituted verifies that base64-encoded
// FileSeed contents are NOT passed through template substitution (binary data).
func TestSH030_ExpandMatrix_Base64ContentsNotSubstituted(t *testing.T) {
	t.Parallel()

	const base64Value = "dGVzdCBkYXRh" // "test data" base64-encoded; happens to contain no {{
	wp := "w.dot"
	sf := ScenarioFile{
		Name:         "b64-test",
		Description:  "d",
		WorkflowPath: &wp,
		FixtureSetup: FixtureSetup{
			Files: map[string]FileSeed{
				"data.bin": {
					Encoding: FileSeedEncodingBase64,
					Contents: base64Value,
				},
			},
		},
		TimeoutSecs: 10,
		CadenceTag:  CadenceTagSmoke,
		Matrix: map[string][]string{
			"env": {"prod"},
		},
	}

	cells, err := sf.ExpandMatrix()
	if err != nil {
		t.Fatalf("ExpandMatrix() error = %v; want nil", err)
	}
	if len(cells) != 1 {
		t.Fatalf("want 1 cell; got %d", len(cells))
	}

	seed := cells[0].FixtureSetup.Files["data.bin"]
	if seed.Contents != base64Value {
		t.Errorf("base64 Contents modified; got %q; want %q", seed.Contents, base64Value)
	}
}
