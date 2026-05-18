package main

// handler_test.go — unit tests for `harmonik handler status` (hk-39ryh).
//
// Helper prefix: handlerFixture (per implementer-protocol.md §Helper-prefix discipline).
//
// Tests cover:
//   - absent handler-state.json → all-live output
//   - paused handler → correct text and JSON output shape
//   - --type filter → single handler scoped
//   - --format json → machine-parseable JSON with held_count
//   - forward-incompatible schema_version → exit 2
//   - unknown flag / missing verb → exit 1
//
// All tests are parallel-safe (no flag.CommandLine or os.Args mutation).
//
// Acceptance: hk-39ryh.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// handlerFixtureTempDir creates a temporary directory with the .harmonik/
// subdirectory pre-created and returns the project root path.
func handlerFixtureTempDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	harmDir := filepath.Join(root, ".harmonik")
	if err := os.MkdirAll(harmDir, 0o755); err != nil {
		t.Fatalf("handlerFixtureTempDir: mkdir %s: %v", harmDir, err)
	}
	return root
}

// handlerFixtureWriteStateFile writes content to .harmonik/handler-state.json
// inside projectDir.
func handlerFixtureWriteStateFile(t *testing.T, projectDir, content string) {
	t.Helper()
	p := filepath.Join(projectDir, ".harmonik", "handler-state.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("handlerFixtureWriteStateFile: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestHandlerStatus_FileAbsent verifies that when handler-state.json does not
// exist the command exits 0 and reports "no handler-pause records".
//
// Acceptance: hk-39ryh — "file-absent → all handlers live".
func TestHandlerStatus_FileAbsent(t *testing.T) {
	t.Parallel()

	projectDir := handlerFixtureTempDir(t)
	var out, errOut bytes.Buffer

	code := runHandlerSubcommandIO(
		[]string{"status", "--project", projectDir},
		&out, &errOut,
	)

	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "no handler-pause records") {
		t.Errorf("stdout %q missing 'no handler-pause records'", out.String())
	}
}

// TestHandlerStatus_FileAbsent_JSON verifies that the JSON output when the
// file is absent is a valid JSON object with an empty handlers map.
//
// Acceptance: hk-39ryh — "JSON output mirrors handler-state.json + held_count".
func TestHandlerStatus_FileAbsent_JSON(t *testing.T) {
	t.Parallel()

	projectDir := handlerFixtureTempDir(t)
	var out, errOut bytes.Buffer

	code := runHandlerSubcommandIO(
		[]string{"status", "--format", "json", "--project", projectDir},
		&out, &errOut,
	)

	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr: %s", code, errOut.String())
	}

	var got handlerStatusJSONOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("JSON parse error: %v\nraw: %s", err, out.String())
	}
	if got.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", got.SchemaVersion)
	}
	if len(got.Handlers) != 0 {
		t.Errorf("handlers map is non-empty: %v", got.Handlers)
	}
}

// TestHandlerStatus_PausedHandler_Text verifies text output for a paused handler.
//
// Acceptance: hk-39ryh — "text output includes status, cause, in-flight-at-pause".
func TestHandlerStatus_PausedHandler_Text(t *testing.T) {
	t.Parallel()

	projectDir := handlerFixtureTempDir(t)
	handlerFixtureWriteStateFile(t, projectDir, `{
		"schema_version": 1,
		"handlers": {
			"claude-code": {
				"status": "paused",
				"cause": {
					"failure_class": "transient",
					"sub_reason": "rate_limit",
					"source_run_id": "01900000-0000-7000-8000-000000000001",
					"source_bead_id": "hk-test01",
					"tripped_at": "2026-05-18T14:22:11Z"
				},
				"in_flight_at_pause": [
					{
						"run_id": "01900000-0000-7000-8000-000000000042",
						"bead_id": "hk-ajchp",
						"dispatched_at": "2026-05-18T14:20:01Z"
					}
				],
				"paused_epoch": 1
			}
		}
	}`)

	var out, errOut bytes.Buffer
	code := runHandlerSubcommandIO(
		[]string{"status", "--project", projectDir},
		&out, &errOut,
	)

	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr: %s", code, errOut.String())
	}
	stdout := out.String()

	for _, want := range []string{
		"claude-code",
		"paused",
		"rate_limit",
		"hk-test01",
		"hk-ajchp",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

// TestHandlerStatus_PausedHandler_JSON verifies JSON output shape for a paused handler.
//
// Acceptance: hk-39ryh — "JSON output mirrors handler-state.json + held_count".
func TestHandlerStatus_PausedHandler_JSON(t *testing.T) {
	t.Parallel()

	projectDir := handlerFixtureTempDir(t)
	handlerFixtureWriteStateFile(t, projectDir, `{
		"schema_version": 1,
		"handlers": {
			"claude-code": {
				"status": "paused",
				"cause": {
					"failure_class": "transient",
					"sub_reason": "rate_limit",
					"source_run_id": "01900000-0000-7000-8000-000000000001",
					"source_bead_id": "hk-test01",
					"tripped_at": "2026-05-18T14:22:11Z"
				},
				"in_flight_at_pause": [],
				"paused_epoch": 2
			}
		}
	}`)

	var out, errOut bytes.Buffer
	code := runHandlerSubcommandIO(
		[]string{"status", "--format", "json", "--project", projectDir},
		&out, &errOut,
	)

	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr: %s", code, errOut.String())
	}

	var got handlerStatusJSONOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("JSON parse error: %v\nraw: %s", err, out.String())
	}

	entry, ok := got.Handlers["claude-code"]
	if !ok {
		t.Fatalf("handlers missing 'claude-code'; got: %v", got.Handlers)
	}
	if entry.Status != "paused" {
		t.Errorf("status = %q, want paused", entry.Status)
	}
	if entry.Cause == nil {
		t.Fatal("cause is nil")
	}
	if entry.Cause.SubReason != "rate_limit" {
		t.Errorf("cause.sub_reason = %q, want rate_limit", entry.Cause.SubReason)
	}
	if entry.PausedEpoch != 2 {
		t.Errorf("paused_epoch = %d, want 2", entry.PausedEpoch)
	}
	// held_count always 0 at MVH CLI level.
	if entry.HeldCount != 0 {
		t.Errorf("held_count = %d, want 0", entry.HeldCount)
	}
}

// TestHandlerStatus_TypeFilter verifies that --type scopes to a single handler.
//
// Acceptance: hk-39ryh — "harmonik handler status --type T implemented".
func TestHandlerStatus_TypeFilter(t *testing.T) {
	t.Parallel()

	projectDir := handlerFixtureTempDir(t)
	handlerFixtureWriteStateFile(t, projectDir, `{
		"schema_version": 1,
		"handlers": {
			"claude-code": {
				"status": "paused",
				"cause": {
					"failure_class": "transient",
					"sub_reason": "rate_limit",
					"source_run_id": "run-1",
					"source_bead_id": "hk-a",
					"tripped_at": "2026-05-18T14:22:11Z"
				},
				"in_flight_at_pause": [],
				"paused_epoch": 1
			},
			"codex": {
				"status": "live",
				"cause": null,
				"in_flight_at_pause": [],
				"paused_epoch": 0
			}
		}
	}`)

	var out, errOut bytes.Buffer
	code := runHandlerSubcommandIO(
		[]string{"status", "--type", "codex", "--format", "json", "--project", projectDir},
		&out, &errOut,
	)

	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr: %s", code, errOut.String())
	}

	var got handlerStatusJSONOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("JSON parse error: %v\nraw: %s", err, out.String())
	}

	if len(got.Handlers) != 1 {
		t.Errorf("handlers len = %d, want 1 (only codex)", len(got.Handlers))
	}
	if _, ok := got.Handlers["codex"]; !ok {
		t.Errorf("handlers missing 'codex'; got: %v", got.Handlers)
	}
	if _, ok := got.Handlers["claude-code"]; ok {
		t.Errorf("handlers unexpectedly contains 'claude-code' after --type=codex filter")
	}
}

// TestHandlerStatus_TypeFilter_NotInFile verifies that --type for an unknown
// handler returns "live" (no-pause record).
//
// Acceptance: hk-39ryh — "file-absent or unknown type → live".
func TestHandlerStatus_TypeFilter_NotInFile(t *testing.T) {
	t.Parallel()

	projectDir := handlerFixtureTempDir(t)
	handlerFixtureWriteStateFile(t, projectDir, `{
		"schema_version": 1,
		"handlers": {}
	}`)

	var out, errOut bytes.Buffer
	code := runHandlerSubcommandIO(
		[]string{"status", "--type", "codex", "--format", "json", "--project", projectDir},
		&out, &errOut,
	)

	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr: %s", code, errOut.String())
	}

	var got handlerStatusJSONOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("JSON parse error: %v\nraw: %s", err, out.String())
	}
	entry, ok := got.Handlers["codex"]
	if !ok {
		t.Fatalf("handlers missing 'codex'; got: %v", got.Handlers)
	}
	if entry.Status != "live" {
		t.Errorf("status = %q, want live", entry.Status)
	}
}

// TestHandlerStatus_ForwardIncompatibleSchema verifies exit 2 on schema_version > 1.
//
// Acceptance: hk-39ryh — mirrors QM-002 forward-incompatible handling.
func TestHandlerStatus_ForwardIncompatibleSchema(t *testing.T) {
	t.Parallel()

	projectDir := handlerFixtureTempDir(t)
	handlerFixtureWriteStateFile(t, projectDir, `{"schema_version": 99, "handlers": {}}`)

	var out, errOut bytes.Buffer
	code := runHandlerSubcommandIO(
		[]string{"status", "--project", projectDir},
		&out, &errOut,
	)

	if code != 2 {
		t.Errorf("exit code = %d, want 2 (forward-incompatible schema); stderr: %s", code, errOut.String())
	}
}

// TestHandlerStatus_BadJSON verifies exit 1 on unparseable file.
func TestHandlerStatus_BadJSON(t *testing.T) {
	t.Parallel()

	projectDir := handlerFixtureTempDir(t)
	handlerFixtureWriteStateFile(t, projectDir, `not json at all`)

	var out, errOut bytes.Buffer
	code := runHandlerSubcommandIO(
		[]string{"status", "--project", projectDir},
		&out, &errOut,
	)

	if code != 1 {
		t.Errorf("exit code = %d, want 1 (parse error); stderr: %s", code, errOut.String())
	}
}

// TestHandlerSubcommand_MissingVerb verifies exit 1 when no verb is given.
func TestHandlerSubcommand_MissingVerb(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer
	code := runHandlerSubcommandIO([]string{}, &out, &errOut)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// TestHandlerSubcommand_UnknownVerb verifies exit 1 for an unknown verb.
func TestHandlerSubcommand_UnknownVerb(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer
	code := runHandlerSubcommandIO([]string{"frobnicate"}, &out, &errOut)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// TestHandlerStatus_UnknownFlag verifies exit 1 for an unknown flag.
func TestHandlerStatus_UnknownFlag(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer
	code := runHandlerSubcommandIO([]string{"status", "--unknown-flag"}, &out, &errOut)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// TestHandlerStatus_JSONAlias verifies that --json is equivalent to --format json.
func TestHandlerStatus_JSONAlias(t *testing.T) {
	t.Parallel()

	projectDir := handlerFixtureTempDir(t)
	var out, errOut bytes.Buffer

	code := runHandlerSubcommandIO(
		[]string{"status", "--json", "--project", projectDir},
		&out, &errOut,
	)

	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr: %s", code, errOut.String())
	}
	// Must be valid JSON.
	var got handlerStatusJSONOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\nraw: %s", err, out.String())
	}
}
