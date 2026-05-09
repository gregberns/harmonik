package scenario

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// scenarioFileFixtureWorkflowPath returns a minimally valid ScenarioFile using
// WorkflowPath (DOT file) as the workflow selector.
func scenarioFileFixtureWorkflowPath(t *testing.T) ScenarioFile {
	t.Helper()
	wp := "workflows/basic.dot"
	return ScenarioFile{
		Name:              "basic-smoke",
		Description:       "smoke test for the basic workflow",
		WorkflowPath:      &wp,
		AgentOverrides:    nil,
		FixtureSetup:      FixtureSetup{},
		ExpectedEvents:    nil,
		ExpectedWorkspace: nil,
		ExpectedOutcome:   nil,
		TimeoutSecs:       30,
		CadenceTag:        CadenceTagSmoke,
		Matrix:            nil,
	}
}

// scenarioFileFixtureWorkflowID returns a minimally valid ScenarioFile using
// WorkflowID as the workflow selector.
func scenarioFileFixtureWorkflowID(t *testing.T) ScenarioFile {
	t.Helper()
	// Use a nil-pointer-free zero UUID for testing (not a valid UUID value, but
	// valid for structural tests — WorkflowID presence is what matters here).
	wid := core.WorkflowID{}
	return ScenarioFile{
		Name:              "id-based-scenario",
		Description:       "scenario referencing a workflow by UUID",
		WorkflowPath:      nil,
		WorkflowID:        &wid,
		AgentOverrides:    nil,
		FixtureSetup:      FixtureSetup{},
		ExpectedEvents:    nil,
		ExpectedWorkspace: nil,
		ExpectedOutcome:   nil,
		TimeoutSecs:       60,
		CadenceTag:        CadenceTagRegression,
		Matrix:            nil,
	}
}

// scenarioFileFixtureFull returns a fully-populated valid ScenarioFile covering
// every optional field.
func scenarioFileFixtureFull(t *testing.T) ScenarioFile {
	t.Helper()
	wp := "workflows/full.dot"
	outcome := outcomeExpectationFixtureSuccess(t)
	return ScenarioFile{
		Name:         "full-scenario",
		Description:  "a fully-populated scenario exercising all fields",
		WorkflowPath: &wp,
		AgentOverrides: map[string]AgentOverride{
			"worker": agentOverrideFixtureBasic(t),
		},
		FixtureSetup:      fixtureSetupFixtureEmpty(t),
		ExpectedEvents:    []EventExpectation{eventExpectationFixturePresent(t)},
		ExpectedWorkspace: []WorkspacePredicate{workspacePredicateFixtureFileExists(t)},
		ExpectedOutcome:   &outcome,
		TimeoutSecs:       120,
		CadenceTag:        CadenceTagNightly,
		Matrix: map[string][]string{
			"env": {"staging", "prod"},
		},
	}
}

func TestScenarioFileValid(t *testing.T) {
	t.Parallel()

	makeWP := func(s string) *string { return &s }
	makeWID := func() *core.WorkflowID { w := core.WorkflowID{}; return &w }
	makeOutcome := func() *OutcomeExpectation {
		o := OutcomeExpectation{
			OutcomeStatus: core.OutcomeStatusSuccess,
			Description:   "must succeed",
		}
		return &o
	}

	tests := []struct {
		name  string
		input ScenarioFile
		want  bool
	}{
		{
			name:  "valid: workflow_path selector",
			input: scenarioFileFixtureWorkflowPath(t),
			want:  true,
		},
		{
			name:  "valid: workflow_id selector",
			input: scenarioFileFixtureWorkflowID(t),
			want:  true,
		},
		{
			name:  "valid: fully populated",
			input: scenarioFileFixtureFull(t),
			want:  true,
		},
		{
			name: "valid: min timeout_secs=1",
			input: ScenarioFile{
				Name: "t1", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: 1, CadenceTag: CadenceTagSmoke,
			},
			want: true,
		},
		{
			name: "valid: max timeout_secs=7200",
			input: ScenarioFile{
				Name: "t2", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: 7200, CadenceTag: CadenceTagSmoke,
			},
			want: true,
		},
		{
			name: "valid: matrix exactly at 1024 cells (4x4x4x4=256 — well under cap)",
			input: ScenarioFile{
				Name: "t3", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: 30, CadenceTag: CadenceTagSmoke,
				Matrix: map[string][]string{
					"a": {"1", "2", "3", "4"},
					"b": {"1", "2", "3", "4"},
					"c": {"1", "2", "3", "4"},
					"d": {"1", "2", "3", "4"},
				},
			},
			want: true,
		},
		{
			name: "valid: matrix with nil (no matrix)",
			input: ScenarioFile{
				Name: "t4", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: 30, CadenceTag: CadenceTagSmoke,
				Matrix: nil,
			},
			want: true,
		},
		{
			name: "valid: expected_outcome non-nil",
			input: ScenarioFile{
				Name: "t5", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: 30, CadenceTag: CadenceTagSmoke,
				ExpectedOutcome: makeOutcome(),
			},
			want: true,
		},
		// --- invalid cases ---
		{
			name:  "invalid: zero value",
			input: ScenarioFile{},
			want:  false,
		},
		{
			name: "invalid: empty name",
			input: ScenarioFile{
				Name: "", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: 30, CadenceTag: CadenceTagSmoke,
			},
			want: false,
		},
		{
			name: "invalid: name with slash (path separator forbidden per SH-005)",
			input: ScenarioFile{
				Name: "foo/bar", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: 30, CadenceTag: CadenceTagSmoke,
			},
			want: false,
		},
		{
			name: "invalid: name with space (whitespace forbidden per SH-005)",
			input: ScenarioFile{
				Name: "foo bar", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: 30, CadenceTag: CadenceTagSmoke,
			},
			want: false,
		},
		{
			name: "invalid: name starting with dot",
			input: ScenarioFile{
				Name: ".hidden", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: 30, CadenceTag: CadenceTagSmoke,
			},
			want: false,
		},
		{
			name: "invalid: name too long (>128 chars total)",
			input: ScenarioFile{
				// 129 'a's: regex allows 1 + up to 127 more = 128 max total
				Name: func() string {
					b := make([]byte, 129)
					for i := range b {
						b[i] = 'a'
					}
					return string(b)
				}(),
				WorkflowPath: makeWP("w.dot"),
				TimeoutSecs:  30, CadenceTag: CadenceTagSmoke,
			},
			want: false,
		},
		{
			name: "invalid: both workflow_path and workflow_id set",
			input: ScenarioFile{
				Name:         "both-set",
				WorkflowPath: makeWP("w.dot"),
				WorkflowID:   makeWID(),
				TimeoutSecs:  30,
				CadenceTag:   CadenceTagSmoke,
			},
			want: false,
		},
		{
			name: "invalid: neither workflow_path nor workflow_id set",
			input: ScenarioFile{
				Name:         "neither-set",
				WorkflowPath: nil,
				WorkflowID:   nil,
				TimeoutSecs:  30,
				CadenceTag:   CadenceTagSmoke,
			},
			want: false,
		},
		{
			name: "invalid: timeout_secs=0",
			input: ScenarioFile{
				Name: "t6", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: 0, CadenceTag: CadenceTagSmoke,
			},
			want: false,
		},
		{
			name: "invalid: timeout_secs negative",
			input: ScenarioFile{
				Name: "t7", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: -1, CadenceTag: CadenceTagSmoke,
			},
			want: false,
		},
		{
			name: "invalid: timeout_secs=7201 (exceeds upper bound)",
			input: ScenarioFile{
				Name: "t8", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: 7201, CadenceTag: CadenceTagSmoke,
			},
			want: false,
		},
		{
			name: "invalid: unknown cadence_tag",
			input: ScenarioFile{
				Name: "t9", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: 30, CadenceTag: CadenceTag("daily"),
			},
			want: false,
		},
		{
			name: "invalid: empty cadence_tag",
			input: ScenarioFile{
				Name: "t10", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: 30, CadenceTag: CadenceTag(""),
			},
			want: false,
		},
		{
			name: "invalid: matrix exceeds 1024 cells",
			input: ScenarioFile{
				Name: "t11", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: 30, CadenceTag: CadenceTagSmoke,
				Matrix: map[string][]string{
					// 33x32=1056 > 1024
					"a": func() []string {
						s := make([]string, 33)
						for i := range s {
							s[i] = "v"
						}
						return s
					}(),
					"b": func() []string {
						s := make([]string, 32)
						for i := range s {
							s[i] = "v"
						}
						return s
					}(),
				},
			},
			want: false,
		},
		{
			name: "invalid: AgentOverride with empty binary",
			input: ScenarioFile{
				Name: "t12", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: 30, CadenceTag: CadenceTagSmoke,
				AgentOverrides: map[string]AgentOverride{
					"worker": {Binary: ""},
				},
			},
			want: false,
		},
		{
			name: "invalid: ExpectedOutcome with empty description",
			input: ScenarioFile{
				Name: "t13", WorkflowPath: makeWP("w.dot"),
				TimeoutSecs: 30, CadenceTag: CadenceTagSmoke,
				ExpectedOutcome: &OutcomeExpectation{
					OutcomeStatus: core.OutcomeStatusSuccess,
					Description:   "",
				},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("ScenarioFile.Valid() = %v, want %v (input: %+v)", got, tc.want, tc.input)
			}
		})
	}
}

func TestScenarioFileJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input ScenarioFile
	}{
		{
			name:  "workflow_path selector",
			input: scenarioFileFixtureWorkflowPath(t),
		},
		{
			name:  "workflow_id selector",
			input: scenarioFileFixtureWorkflowID(t),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tc.input)
			if err != nil {
				t.Fatalf("json.Marshal error: %v", err)
			}

			var got ScenarioFile
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal error: %v", err)
			}

			if !reflect.DeepEqual(tc.input, got) {
				t.Errorf("round-trip mismatch:\n  in:  %+v\n  out: %+v", tc.input, got)
			}
		})
	}
}

func TestScenarioFileJSONKeys(t *testing.T) {
	t.Parallel()

	// Verify required keys are always present; optional keys obey omitempty.
	sf := scenarioFileFixtureWorkflowPath(t)
	data, err := json.Marshal(sf)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal into map error: %v", err)
	}

	requiredKeys := []string{"name", "description", "workflow_path", "fixture_setup", "timeout_secs", "cadence_tag"}
	for _, k := range requiredKeys {
		if _, ok := raw[k]; !ok {
			t.Errorf("marshaled JSON missing required key %q; got %s", k, data)
		}
	}

	// workflow_id is nil — must be absent (omitempty).
	if _, ok := raw["workflow_id"]; ok {
		t.Errorf("marshaled JSON contains 'workflow_id' when WorkflowID is nil; got %s", data)
	}

	// expected_outcome is nil — must be absent (omitempty).
	if _, ok := raw["expected_outcome"]; ok {
		t.Errorf("marshaled JSON contains 'expected_outcome' when ExpectedOutcome is nil; got %s", data)
	}

	// matrix is nil — must be absent (omitempty).
	if _, ok := raw["matrix"]; ok {
		t.Errorf("marshaled JSON contains 'matrix' when Matrix is nil; got %s", data)
	}
}

func TestMatrixCellCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		matrix map[string][]string
		want   int
	}{
		{
			name:   "nil matrix",
			matrix: nil,
			want:   0,
		},
		{
			name:   "empty map",
			matrix: map[string][]string{},
			want:   0,
		},
		{
			name:   "single param two values",
			matrix: map[string][]string{"env": {"staging", "prod"}},
			want:   2,
		},
		{
			name: "two params 2x3=6",
			matrix: map[string][]string{
				"env":  {"staging", "prod"},
				"size": {"small", "medium", "large"},
			},
			want: 6,
		},
		{
			name: "zero-length value list → 0 cells",
			matrix: map[string][]string{
				"env":  {"staging", "prod"},
				"size": {},
			},
			want: 0,
		},
		{
			name: "exactly 1024 cells (32x32)",
			matrix: map[string][]string{
				"a": func() []string { s := make([]string, 32); return s }(),
				"b": func() []string { s := make([]string, 32); return s }(),
			},
			want: 1024,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := matrixCellCount(tc.matrix); got != tc.want {
				t.Errorf("matrixCellCount(%v) = %d, want %d", tc.matrix, got, tc.want)
			}
		})
	}
}

func TestScenarioFileWorkflowMutualExclusivity(t *testing.T) {
	t.Parallel()

	makeWP := func(s string) *string { return &s }
	makeWID := func() *core.WorkflowID { w := core.WorkflowID{}; return &w }

	base := ScenarioFile{
		Name:        "mutex-test",
		TimeoutSecs: 30,
		CadenceTag:  CadenceTagSmoke,
	}

	t.Run("neither set → invalid", func(t *testing.T) {
		t.Parallel()
		sf := base
		sf.WorkflowPath = nil
		sf.WorkflowID = nil
		if sf.Valid() {
			t.Error("expected Valid()=false when neither WorkflowPath nor WorkflowID is set")
		}
	})

	t.Run("workflow_path only → valid", func(t *testing.T) {
		t.Parallel()
		sf := base
		sf.WorkflowPath = makeWP("w.dot")
		sf.WorkflowID = nil
		if !sf.Valid() {
			t.Error("expected Valid()=true with only WorkflowPath set")
		}
	})

	t.Run("workflow_id only → valid", func(t *testing.T) {
		t.Parallel()
		sf := base
		sf.WorkflowPath = nil
		sf.WorkflowID = makeWID()
		if !sf.Valid() {
			t.Error("expected Valid()=true with only WorkflowID set")
		}
	})

	t.Run("both set → invalid", func(t *testing.T) {
		t.Parallel()
		sf := base
		sf.WorkflowPath = makeWP("w.dot")
		sf.WorkflowID = makeWID()
		if sf.Valid() {
			t.Error("expected Valid()=false when both WorkflowPath and WorkflowID are set")
		}
	})
}

func TestScenarioFileNameRegex(t *testing.T) {
	t.Parallel()

	makeWP := func(s string) *string { return &s }
	base := ScenarioFile{
		WorkflowPath: makeWP("w.dot"),
		TimeoutSecs:  30,
		CadenceTag:   CadenceTagSmoke,
	}

	// build128 returns a valid 128-char name (all alphanumeric).
	build128 := func() string {
		b := make([]byte, 128)
		for i := range b {
			b[i] = 'a'
		}
		return string(b)
	}

	valid := []string{
		"a",
		"A",
		"abc",
		"abc-123",
		"abc_123",
		"abc.123",
		"A1B2C3",
		// 128 chars total (max length per SH-005: 1 + up to 127 more)
		build128(),
	}

	for _, n := range valid {
		n := n
		t.Run("valid:"+n, func(t *testing.T) {
			t.Parallel()
			sf := base
			sf.Name = n
			if !sf.Valid() {
				t.Errorf("expected Valid()=true for name %q", n)
			}
		})
	}

	// build129 returns a 129-char name (exceeds 128-char max per SH-005).
	build129 := func() string {
		b := make([]byte, 129)
		for i := range b {
			b[i] = 'a'
		}
		return string(b)
	}

	invalid := []string{
		"",
		"-leading-dash",
		".leading-dot",
		"foo bar",
		"foo/bar",
		"foo\x00bar",
		// 129 chars total (exceeds 128-char max per SH-005)
		build129(),
	}

	for _, n := range invalid {
		n := n
		t.Run("invalid:"+n, func(t *testing.T) {
			t.Parallel()
			sf := base
			sf.Name = n
			if sf.Valid() {
				t.Errorf("expected Valid()=false for name %q", n)
			}
		})
	}
}
