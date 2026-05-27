package hookrelay_test

// hookrelay_chb014_qo08q_test.go — targeted tests for CHB-014 reviewer verdict
// file read and validation in the hook-relay Stop handler.
//
// Bead: hk-qo08q.14 (CHB-014: Reviewer verdict file read + validate).
// Spec: specs/claude-hook-bridge.md §4.5 CHB-014.
//
// CHB-014 rules:
//   - For phase=reviewer Stop hook, relay MUST read ${HARMONIK_WORKSPACE_PATH}/.harmonik/review.json.
//   - File MUST conform to agent-reviewer JSON verdict schema v1:
//       schema_version = 1
//       verdict ∈ {APPROVE, REQUEST_CHANGES, BLOCK}
//       flags is a string array (null normalised to [])
//       notes is a string
//   - On validation success: package four fields into outcome_emitted{kind=REVIEWER_VERDICT}.verdict sub-field.
//   - On file-absent: outcome_emitted{error="missing_review_file"}.
//   - On malformed (bad JSON, schema_version≠1, invalid verdict value): outcome_emitted{error="malformed_review_file"}.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/hookrelay"
)

// chb014Dir creates a temp workspace with an optional .harmonik/review.json.
// Pass verdictJSON="" to skip writing the file (tests file-absent case).
func chb014Dir(t *testing.T, verdictJSON string) (workspaceDir string) {
	t.Helper()
	dir := t.TempDir()
	if verdictJSON == "" {
		return dir
	}
	harmonikDir := filepath.Join(dir, ".harmonik")
	//nolint:gosec // G301: test helper; 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("chb014Dir: mkdir .harmonik: %v", err)
	}
	//nolint:gosec // G306: test file with non-secret content
	if err := os.WriteFile(filepath.Join(harmonikDir, "review.json"), []byte(verdictJSON), 0o644); err != nil {
		t.Fatalf("chb014Dir: write review.json: %v", err)
	}
	return dir
}

// chb014RunStop runs hook-relay Stop for a reviewer-phase env against a socket
// that ACKs ok, and returns the decoded envelope payload map.
func chb014RunStop(t *testing.T, workspaceDir string) map[string]interface{} {
	t.Helper()
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e := hookRelayFixtureEnv(workspaceDir)
	e.Phase = "reviewer"
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("chb014RunStop: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	select {
	case msgBytes := <-received:
		var env map[string]json.RawMessage
		if err := json.Unmarshal(msgBytes, &env); err != nil {
			t.Fatalf("chb014RunStop: unmarshal envelope: %v", err)
		}
		var pl map[string]interface{}
		if err := json.Unmarshal(env["payload"], &pl); err != nil {
			t.Fatalf("chb014RunStop: unmarshal payload: %v", err)
		}
		return pl
	default:
		t.Fatal("chb014RunStop: no message received on socket")
		return nil
	}
}

// ─── Happy path ───────────────────────────────────────────────────────────────

// TestHookRelay_CHB014_ApproveVerdict verifies that a valid APPROVE verdict in
// review.json produces outcome_emitted{kind=REVIEWER_VERDICT} with all four
// schema fields forwarded into the verdict sub-object.
//
// Spec: claude-hook-bridge.md §4.5 CHB-014.
func TestHookRelay_CHB014_ApproveVerdict(t *testing.T) {
	t.Parallel()

	dir := chb014Dir(t, `{"schema_version":1,"verdict":"APPROVE","flags":["flag-a","flag-b"],"notes":"looks good"}`)
	pl := chb014RunStop(t, dir)

	if pl["kind"] != "REVIEWER_VERDICT" {
		t.Errorf("CHB-014 APPROVE: kind=%v, want REVIEWER_VERDICT", pl["kind"])
	}
	verdict, ok := pl["verdict"].(map[string]interface{})
	if !ok {
		t.Fatalf("CHB-014 APPROVE: verdict field missing or not an object; payload=%v", pl)
	}
	if verdict["verdict"] != "APPROVE" {
		t.Errorf("CHB-014 APPROVE: verdict.verdict=%v, want APPROVE", verdict["verdict"])
	}
	if verdict["schema_version"] != float64(1) {
		t.Errorf("CHB-014 APPROVE: verdict.schema_version=%v, want 1", verdict["schema_version"])
	}
	if verdict["notes"] != "looks good" {
		t.Errorf("CHB-014 APPROVE: verdict.notes=%v, want %q", verdict["notes"], "looks good")
	}
	flags, ok := verdict["flags"].([]interface{})
	if !ok {
		t.Fatalf("CHB-014 APPROVE: verdict.flags not a slice; got %T %v", verdict["flags"], verdict["flags"])
	}
	if len(flags) != 2 {
		t.Errorf("CHB-014 APPROVE: verdict.flags len=%d, want 2", len(flags))
	}
}

// TestHookRelay_CHB014_RequestChangesVerdict verifies REQUEST_CHANGES is a
// valid verdict value and is forwarded unchanged.
//
// Spec: claude-hook-bridge.md §4.5 CHB-014 — verdict ∈ {APPROVE, REQUEST_CHANGES, BLOCK}.
func TestHookRelay_CHB014_RequestChangesVerdict(t *testing.T) {
	t.Parallel()

	dir := chb014Dir(t, `{"schema_version":1,"verdict":"REQUEST_CHANGES","flags":[],"notes":"needs work"}`)
	pl := chb014RunStop(t, dir)

	if pl["kind"] != "REVIEWER_VERDICT" {
		t.Errorf("CHB-014 REQUEST_CHANGES: kind=%v, want REVIEWER_VERDICT", pl["kind"])
	}
	verdict, _ := pl["verdict"].(map[string]interface{})
	if verdict["verdict"] != "REQUEST_CHANGES" {
		t.Errorf("CHB-014 REQUEST_CHANGES: verdict.verdict=%v, want REQUEST_CHANGES", verdict["verdict"])
	}
}

// TestHookRelay_CHB014_BlockVerdict verifies BLOCK is a valid verdict value.
//
// Spec: claude-hook-bridge.md §4.5 CHB-014 — verdict ∈ {APPROVE, REQUEST_CHANGES, BLOCK}.
func TestHookRelay_CHB014_BlockVerdict(t *testing.T) {
	t.Parallel()

	dir := chb014Dir(t, `{"schema_version":1,"verdict":"BLOCK","flags":["security"],"notes":"do not merge"}`)
	pl := chb014RunStop(t, dir)

	if pl["kind"] != "REVIEWER_VERDICT" {
		t.Errorf("CHB-014 BLOCK: kind=%v, want REVIEWER_VERDICT", pl["kind"])
	}
	verdict, _ := pl["verdict"].(map[string]interface{})
	if verdict["verdict"] != "BLOCK" {
		t.Errorf("CHB-014 BLOCK: verdict.verdict=%v, want BLOCK", verdict["verdict"])
	}
}

// TestHookRelay_CHB014_NullFlagsNormalisedToEmpty verifies that a null flags
// field in review.json is normalised to an empty array in the emitted payload.
//
// Spec: claude-hook-bridge.md §4.5 CHB-014 — flags is a string array.
func TestHookRelay_CHB014_NullFlagsNormalisedToEmpty(t *testing.T) {
	t.Parallel()

	// null flags value — Go's json.Unmarshal sets []string to nil; relay normalises.
	dir := chb014Dir(t, `{"schema_version":1,"verdict":"APPROVE","flags":null,"notes":"null flags"}`)
	pl := chb014RunStop(t, dir)

	if pl["kind"] != "REVIEWER_VERDICT" {
		t.Errorf("CHB-014 null flags: kind=%v, want REVIEWER_VERDICT", pl["kind"])
	}
	verdict, _ := pl["verdict"].(map[string]interface{})
	flags, ok := verdict["flags"].([]interface{})
	if !ok {
		t.Fatalf("CHB-014 null flags: verdict.flags not a slice after normalisation; got %T %v", verdict["flags"], verdict["flags"])
	}
	if len(flags) != 0 {
		t.Errorf("CHB-014 null flags: verdict.flags len=%d, want 0", len(flags))
	}
}

// ─── Failure modes ────────────────────────────────────────────────────────────

// TestHookRelay_CHB014_FileAbsent verifies that when review.json is not present
// the relay emits outcome_emitted{error="missing_review_file"} and exits 0.
//
// Spec: claude-hook-bridge.md §4.5 CHB-014 — file-absent failure mode.
func TestHookRelay_CHB014_FileAbsent(t *testing.T) {
	t.Parallel()

	// Pass "" → dir created without .harmonik/review.json.
	dir := chb014Dir(t, "")
	pl := chb014RunStop(t, dir)

	if pl["error"] != "missing_review_file" {
		t.Errorf("CHB-014 file absent: error=%v, want missing_review_file", pl["error"])
	}
	// Must NOT emit REVIEWER_VERDICT kind when file is absent.
	if pl["kind"] == "REVIEWER_VERDICT" {
		t.Error("CHB-014 file absent: kind must not be REVIEWER_VERDICT when file is absent")
	}
}

// TestHookRelay_CHB014_MalformedJSON verifies that a review.json with invalid
// JSON content produces outcome_emitted{error="malformed_review_file"}.
//
// Spec: claude-hook-bridge.md §4.5 CHB-014 — malformed failure mode.
func TestHookRelay_CHB014_MalformedJSON(t *testing.T) {
	t.Parallel()

	dir := chb014Dir(t, `{this is not valid json}`)
	pl := chb014RunStop(t, dir)

	if pl["error"] != "malformed_review_file" {
		t.Errorf("CHB-014 malformed JSON: error=%v, want malformed_review_file", pl["error"])
	}
}

// TestHookRelay_CHB014_SchemaVersionNotOne verifies that schema_version≠1
// produces outcome_emitted{error="malformed_review_file"}.
//
// Spec: claude-hook-bridge.md §4.5 CHB-014 — schema_version=1 is required.
func TestHookRelay_CHB014_SchemaVersionNotOne(t *testing.T) {
	t.Parallel()

	for _, sv := range []int{0, 2, 99} {
		sv := sv
		t.Run("schema_version_"+string(rune('0'+sv%10)), func(t *testing.T) {
			t.Parallel()
			verdictJSON, _ := json.Marshal(map[string]interface{}{
				"schema_version": sv,
				"verdict":        "APPROVE",
				"flags":          []string{},
				"notes":          "bad version",
			})
			dir := chb014Dir(t, string(verdictJSON))
			pl := chb014RunStop(t, dir)
			if pl["error"] != "malformed_review_file" {
				t.Errorf("CHB-014 schema_version=%d: error=%v, want malformed_review_file", sv, pl["error"])
			}
		})
	}
}

// TestHookRelay_CHB014_InvalidVerdictValue verifies that a verdict string outside
// {APPROVE, REQUEST_CHANGES, BLOCK} produces malformed_review_file.
//
// Spec: claude-hook-bridge.md §4.5 CHB-014 — verdict ∈ {APPROVE, REQUEST_CHANGES, BLOCK}.
func TestHookRelay_CHB014_InvalidVerdictValue(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"MAYBE", "approve", "", "ACCEPT"} {
		bad := bad
		t.Run("verdict_"+bad, func(t *testing.T) {
			t.Parallel()
			verdictJSON, _ := json.Marshal(map[string]interface{}{
				"schema_version": 1,
				"verdict":        bad,
				"flags":          []string{},
				"notes":          "invalid",
			})
			dir := chb014Dir(t, string(verdictJSON))
			pl := chb014RunStop(t, dir)
			if pl["error"] != "malformed_review_file" {
				t.Errorf("CHB-014 invalid verdict %q: error=%v, want malformed_review_file", bad, pl["error"])
			}
		})
	}
}

// TestHookRelay_CHB014_PhaseNotReviewerUsesWorkComplete verifies that the CHB-014
// reviewer verdict path is NOT taken for non-reviewer phases (single, implementer-initial,
// implementer-resume).  The relay MUST fall through to the WORK_COMPLETE path.
//
// This is the guard against phase mis-routing.
// Spec: claude-hook-bridge.md §4.5 CHB-013 Stop row — kind=WORK_COMPLETE for
// implementer phases; kind=REVIEWER_VERDICT only for phase=reviewer.
func TestHookRelay_CHB014_PhaseNotReviewerUsesWorkComplete(t *testing.T) {
	t.Parallel()

	for _, phase := range []string{"single", "implementer-initial", "implementer-resume", ""} {
		phase := phase
		t.Run("phase_"+phase, func(t *testing.T) {
			t.Parallel()

			// Even if review.json is present, non-reviewer phases must NOT read it.
			dir := chb014Dir(t, `{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"should not be read"}`)
			sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
			e := hookRelayFixtureEnv(dir)
			e.Phase = phase
			e.DaemonSocket = sockPath

			stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Stop", nil)
			var stderr bytes.Buffer
			code := hookrelay.Run("Stop", stdin, &stderr, &e)
			if code != 0 {
				t.Fatalf("CHB-014 phase=%q: exit %d, want 0; stderr=%q", phase, code, stderr.String())
			}

			select {
			case msgBytes := <-received:
				var env map[string]json.RawMessage
				_ = json.Unmarshal(msgBytes, &env)
				var pl map[string]interface{}
				_ = json.Unmarshal(env["payload"], &pl)
				if pl["kind"] != "WORK_COMPLETE" {
					t.Errorf("CHB-014 phase=%q: kind=%v, want WORK_COMPLETE (not REVIEWER_VERDICT)", phase, pl["kind"])
				}
			default:
				t.Errorf("CHB-014 phase=%q: no message received on socket", phase)
			}
		})
	}
}
