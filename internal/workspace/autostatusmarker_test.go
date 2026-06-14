package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// Tests for ReadAutoStatusMarker per handler-contract.md §4.2a HC-068.
// Refs: hk-cq1.

// autoStatusFixtureWrite writes JSON data to an auto_status.json file inside
// a fresh temp workspace and returns the workspace path.
func autoStatusFixtureWrite(t *testing.T, data []byte) string {
	t.Helper()
	workspacePath := t.TempDir()
	harmonikDir := filepath.Join(workspacePath, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("autoStatusFixtureWrite: MkdirAll: %v", err)
	}
	target := AutoStatusMarkerPath(workspacePath)
	//nolint:gosec // G306: test fixture; 0644 is appropriate
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatalf("autoStatusFixtureWrite: WriteFile: %v", err)
	}
	return workspacePath
}

// autoStatusValidJSON returns a minimal valid auto_status.json payload for the
// given failure_class.
func autoStatusValidJSON(t *testing.T, failureClass string) []byte {
	t.Helper()
	payload := map[string]interface{}{
		"schema_version": 1,
		"status":         "FAIL",
		"failure_class":  failureClass,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("autoStatusValidJSON: json.Marshal: %v", err)
	}
	return data
}

// ─────────────────────────────────────────────────────────────────────────────
// AutoStatusMarkerPath — path helper
// ─────────────────────────────────────────────────────────────────────────────

// TestHC068_AutoStatusMarkerPathShape verifies that AutoStatusMarkerPath
// returns ${workspace_path}/.harmonik/auto_status.json per HC-068.
func TestHC068_AutoStatusMarkerPathShape(t *testing.T) {
	t.Parallel()

	workspacePath := "/abs/path/to/worktree"
	got := AutoStatusMarkerPath(workspacePath)
	want := filepath.Join(workspacePath, ".harmonik", "auto_status.json")

	if got != want {
		t.Errorf("HC-068: AutoStatusMarkerPath = %q, want %q", got, want)
	}
}

// TestHC068_AutoStatusMarkerPathFilename verifies the filename is auto_status.json.
func TestHC068_AutoStatusMarkerPathFilename(t *testing.T) {
	t.Parallel()

	got := AutoStatusMarkerPath("/any/path")
	if filepath.Base(got) != "auto_status.json" {
		t.Errorf("HC-068: auto_status filename = %q, want auto_status.json", filepath.Base(got))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadAutoStatusMarker — absent file
// ─────────────────────────────────────────────────────────────────────────────

// TestHC068_ReadAutoStatusMarkerAbsentReturnsNilNil verifies that (nil, nil)
// is returned when auto_status.json does not exist per HC-068 Optionality.
func TestHC068_ReadAutoStatusMarkerAbsentReturnsNilNil(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	m, err := ReadAutoStatusMarker(workspacePath)
	if err != nil {
		t.Errorf("HC-068: ReadAutoStatusMarker(absent) error = %v; want nil", err)
	}
	if m != nil {
		t.Errorf("HC-068: ReadAutoStatusMarker(absent) returned non-nil marker; want nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadAutoStatusMarker — malformed JSON → treat as absent
// ─────────────────────────────────────────────────────────────────────────────

// TestHC068_ReadAutoStatusMarkerMalformedJSONTreatedAbsent verifies that a
// non-JSON file is treated as absent per HC-068 Validation.
func TestHC068_ReadAutoStatusMarkerMalformedJSONTreatedAbsent(t *testing.T) {
	t.Parallel()

	workspacePath := autoStatusFixtureWrite(t, []byte("not valid json"))
	m, err := ReadAutoStatusMarker(workspacePath)
	if err != nil {
		t.Errorf("HC-068: ReadAutoStatusMarker(malformed JSON) error = %v; want nil", err)
	}
	if m != nil {
		t.Errorf("HC-068: ReadAutoStatusMarker(malformed JSON) returned non-nil; want nil (treat-as-absent)")
	}
}

// TestHC068_ReadAutoStatusMarkerEmptyFileTreatedAbsent verifies that an empty
// file is treated as absent.
func TestHC068_ReadAutoStatusMarkerEmptyFileTreatedAbsent(t *testing.T) {
	t.Parallel()

	workspacePath := autoStatusFixtureWrite(t, []byte{})
	m, err := ReadAutoStatusMarker(workspacePath)
	if err != nil {
		t.Errorf("HC-068: ReadAutoStatusMarker(empty) error = %v; want nil", err)
	}
	if m != nil {
		t.Errorf("HC-068: ReadAutoStatusMarker(empty) returned non-nil; want nil (treat-as-absent)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadAutoStatusMarker — non-FAIL status → treat as absent (HC-068 D1)
// ─────────────────────────────────────────────────────────────────────────────

// TestHC068_ReadAutoStatusMarkerNonFailStatusTreatedAbsent verifies that
// markers with status != "FAIL" are treated as absent per HC-068 D1.
func TestHC068_ReadAutoStatusMarkerNonFailStatusTreatedAbsent(t *testing.T) {
	t.Parallel()

	nonFailStatuses := []string{
		"SUCCESS",
		"APPROVE",
		"BLOCK",
		"REQUEST_CHANGES",
		"",
		"fail", // wrong case
		"UNKNOWN",
	}
	for _, status := range nonFailStatuses {
		status := status
		t.Run("status="+status, func(t *testing.T) {
			t.Parallel()

			payload := map[string]interface{}{
				"schema_version": 1,
				"status":         status,
				"failure_class":  "structural",
			}
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			workspacePath := autoStatusFixtureWrite(t, data)

			m, err := ReadAutoStatusMarker(workspacePath)
			if err != nil {
				t.Errorf("HC-068: ReadAutoStatusMarker(status=%q) error = %v; want nil", status, err)
			}
			if m != nil {
				t.Errorf("HC-068: ReadAutoStatusMarker(status=%q) returned non-nil; want nil (treat-as-absent)", status)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadAutoStatusMarker — FAIL + each of six failure classes
// ─────────────────────────────────────────────────────────────────────────────

// TestHC068_ReadAutoStatusMarkerAcceptsFAILWithEachClass verifies that a
// valid FAIL marker with each of the six failure_class values is accepted.
func TestHC068_ReadAutoStatusMarkerAcceptsFAILWithEachClass(t *testing.T) {
	t.Parallel()

	classes := []core.FailureClass{
		core.FailureClassTransient,
		core.FailureClassStructural,
		core.FailureClassDeterministic,
		core.FailureClassCanceled,
		core.FailureClassBudgetExhausted,
		core.FailureClassCompilationLoop, // overridden to structural
	}
	for _, fc := range classes {
		fc := fc
		t.Run("class="+string(fc), func(t *testing.T) {
			t.Parallel()

			workspacePath := autoStatusFixtureWrite(t, autoStatusValidJSON(t, string(fc)))
			m, err := ReadAutoStatusMarker(workspacePath)
			if err != nil {
				t.Fatalf("HC-068: ReadAutoStatusMarker(class=%q) error = %v; want nil", fc, err)
			}
			if m == nil {
				t.Fatalf("HC-068: ReadAutoStatusMarker(class=%q) returned nil; want non-nil", fc)
			}
			if m.Status != "FAIL" {
				t.Errorf("HC-068: Status = %q; want FAIL", m.Status)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadAutoStatusMarker — compilation_loop → structural override (HC-059)
// ─────────────────────────────────────────────────────────────────────────────

// TestHC068_ReadAutoStatusMarkerCompilationLoopOverriddenToStructural verifies
// that failure_class=compilation_loop is overridden to structural per HC-059.
func TestHC068_ReadAutoStatusMarkerCompilationLoopOverriddenToStructural(t *testing.T) {
	t.Parallel()

	workspacePath := autoStatusFixtureWrite(t, autoStatusValidJSON(t, string(core.FailureClassCompilationLoop)))
	m, err := ReadAutoStatusMarker(workspacePath)
	if err != nil {
		t.Fatalf("HC-068: ReadAutoStatusMarker(compilation_loop) error = %v", err)
	}
	if m == nil {
		t.Fatal("HC-068: ReadAutoStatusMarker(compilation_loop) returned nil; want non-nil")
	}
	if m.FailureClass != string(core.FailureClassStructural) {
		t.Errorf("HC-068: FailureClass = %q; want %q (HC-059 override)", m.FailureClass, core.FailureClassStructural)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadAutoStatusMarker — out-of-set failure_class → hint dropped
// ─────────────────────────────────────────────────────────────────────────────

// TestHC068_ReadAutoStatusMarkerOutOfSetClassHintDropped verifies that a
// failure_class value outside the six valid values causes the hint to be
// dropped (FailureClass = "") while the marker is still returned.
func TestHC068_ReadAutoStatusMarkerOutOfSetClassHintDropped(t *testing.T) {
	t.Parallel()

	badClasses := []string{"unknown", "STRUCTURAL", "FAIL", ""}
	for _, bad := range badClasses {
		bad := bad
		t.Run("class="+bad, func(t *testing.T) {
			t.Parallel()

			payload := map[string]interface{}{
				"schema_version": 1,
				"status":         "FAIL",
				"failure_class":  bad,
			}
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			workspacePath := autoStatusFixtureWrite(t, data)

			m, err := ReadAutoStatusMarker(workspacePath)
			if err != nil {
				t.Errorf("HC-068: ReadAutoStatusMarker(bad_class=%q) error = %v; want nil", bad, err)
			}
			if m == nil {
				t.Fatalf("HC-068: ReadAutoStatusMarker(bad_class=%q) returned nil; want non-nil marker with hint dropped", bad)
			}
			if m.FailureClass != "" {
				t.Errorf("HC-068: FailureClass = %q; want \"\" (hint dropped)", m.FailureClass)
			}
		})
	}
}

// TestHC068_ReadAutoStatusMarkerMissingClassHintDropped verifies that a marker
// with no failure_class key has the hint dropped (FailureClass = "").
func TestHC068_ReadAutoStatusMarkerMissingClassHintDropped(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"schema_version":1,"status":"FAIL"}`)
	workspacePath := autoStatusFixtureWrite(t, raw)

	m, err := ReadAutoStatusMarker(workspacePath)
	if err != nil {
		t.Errorf("HC-068: ReadAutoStatusMarker(missing failure_class) error = %v; want nil", err)
	}
	if m == nil {
		t.Fatal("HC-068: ReadAutoStatusMarker(missing failure_class) returned nil; want non-nil")
	}
	if m.FailureClass != "" {
		t.Errorf("HC-068: FailureClass = %q; want \"\" (hint dropped)", m.FailureClass)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadAutoStatusMarker — optional notes and signals
// ─────────────────────────────────────────────────────────────────────────────

// TestHC068_ReadAutoStatusMarkerNotesOptional verifies that notes is optional:
// both present and absent are accepted.
func TestHC068_ReadAutoStatusMarkerNotesOptional(t *testing.T) {
	t.Parallel()

	t.Run("notes_present", func(t *testing.T) {
		t.Parallel()

		payload := map[string]interface{}{
			"schema_version": 1,
			"status":         "FAIL",
			"failure_class":  "structural",
			"notes":          "Implementation failed to compile.",
		}
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		workspacePath := autoStatusFixtureWrite(t, data)

		m, err := ReadAutoStatusMarker(workspacePath)
		if err != nil {
			t.Fatalf("HC-068: ReadAutoStatusMarker(notes=present) error = %v", err)
		}
		if m == nil {
			t.Fatal("HC-068: ReadAutoStatusMarker(notes=present) returned nil")
		}
		if m.Notes != "Implementation failed to compile." {
			t.Errorf("HC-068: Notes = %q; want non-empty", m.Notes)
		}
	})

	t.Run("notes_absent", func(t *testing.T) {
		t.Parallel()

		workspacePath := autoStatusFixtureWrite(t, autoStatusValidJSON(t, "structural"))
		m, err := ReadAutoStatusMarker(workspacePath)
		if err != nil {
			t.Fatalf("HC-068: ReadAutoStatusMarker(notes=absent) error = %v", err)
		}
		if m == nil {
			t.Fatal("HC-068: ReadAutoStatusMarker(notes=absent) returned nil")
		}
		if m.Notes != "" {
			t.Errorf("HC-068: Notes = %q; want empty string when absent", m.Notes)
		}
	})
}

// TestHC068_ReadAutoStatusMarkerSignalsOptional verifies that signals is
// optional: both present (with values) and absent are accepted.
func TestHC068_ReadAutoStatusMarkerSignalsOptional(t *testing.T) {
	t.Parallel()

	t.Run("signals_present", func(t *testing.T) {
		t.Parallel()

		payload := map[string]interface{}{
			"schema_version": 1,
			"status":         "FAIL",
			"failure_class":  "deterministic",
			"signals": map[string]interface{}{
				"exit_code":    1,
				"stderr_lines": 42,
			},
		}
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		workspacePath := autoStatusFixtureWrite(t, data)

		m, err := ReadAutoStatusMarker(workspacePath)
		if err != nil {
			t.Fatalf("HC-068: ReadAutoStatusMarker(signals=present) error = %v", err)
		}
		if m == nil {
			t.Fatal("HC-068: ReadAutoStatusMarker(signals=present) returned nil")
		}
		if m.Signals == nil {
			t.Error("HC-068: Signals is nil; want non-nil map")
		}
	})

	t.Run("signals_absent", func(t *testing.T) {
		t.Parallel()

		workspacePath := autoStatusFixtureWrite(t, autoStatusValidJSON(t, "transient"))
		m, err := ReadAutoStatusMarker(workspacePath)
		if err != nil {
			t.Fatalf("HC-068: ReadAutoStatusMarker(signals=absent) error = %v", err)
		}
		if m == nil {
			t.Fatal("HC-068: ReadAutoStatusMarker(signals=absent) returned nil")
		}
		// Signals may be nil when not present; that's fine.
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadAutoStatusMarker — happy path with all valid failure classes
// ─────────────────────────────────────────────────────────────────────────────

// TestHC068_ReadAutoStatusMarkerHappyPathAllClasses is a table-driven test
// covering all six failure classes with expected post-read FailureClass values.
func TestHC068_ReadAutoStatusMarkerHappyPathAllClasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		inputClass string
		wantClass  string
	}{
		{string(core.FailureClassTransient), string(core.FailureClassTransient)},
		{string(core.FailureClassStructural), string(core.FailureClassStructural)},
		{string(core.FailureClassDeterministic), string(core.FailureClassDeterministic)},
		{string(core.FailureClassCanceled), string(core.FailureClassCanceled)},
		{string(core.FailureClassBudgetExhausted), string(core.FailureClassBudgetExhausted)},
		// compilation_loop → overridden to structural per HC-059
		{string(core.FailureClassCompilationLoop), string(core.FailureClassStructural)},
	}

	for _, tc := range tests {
		tc := tc
		t.Run("class="+tc.inputClass, func(t *testing.T) {
			t.Parallel()

			workspacePath := autoStatusFixtureWrite(t, autoStatusValidJSON(t, tc.inputClass))
			m, err := ReadAutoStatusMarker(workspacePath)
			if err != nil {
				t.Fatalf("ReadAutoStatusMarker: %v", err)
			}
			if m == nil {
				t.Fatal("ReadAutoStatusMarker returned nil; want non-nil")
			}
			if m.FailureClass != tc.wantClass {
				t.Errorf("FailureClass = %q; want %q", m.FailureClass, tc.wantClass)
			}
			if m.Status != "FAIL" {
				t.Errorf("Status = %q; want FAIL", m.Status)
			}
			if m.SchemaVersion != AutoStatusMarkerSchemaVersion {
				t.Errorf("SchemaVersion = %d; want %d", m.SchemaVersion, AutoStatusMarkerSchemaVersion)
			}
		})
	}
}
