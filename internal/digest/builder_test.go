package digest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// TestBuildMissingHarmonikDir verifies that Build returns ErrNoHarmonikDir
// when the project directory does not contain a .harmonik/ subdirectory.
func TestBuildMissingHarmonikDir(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	_, err := Build(context.Background(), BuildInput{
		ProjectDir: tmp,
		Limits:     DefaultLimits(),
	})
	if err != ErrNoHarmonikDir {
		t.Fatalf("expected ErrNoHarmonikDir; got %v", err)
	}
}

// TestBuildEmptyProject verifies that Build succeeds on a project with only an
// empty .harmonik/ directory and returns a schema-versioned DigestJSON with
// no queue, no commits (git may fail gracefully), no events.
func TestBuildEmptyProject(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)
	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if d.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version: got %d, want %d", d.SchemaVersion, SchemaVersion)
	}
	if d.Queue.Present {
		t.Errorf("expected Queue.Present=false for empty project")
	}
}

// TestBuildDefaultLimitsActiveRuns verifies that when more than 10 active runs
// are present in queue.json, the digest truncates to 10 and reports the omission
// count in TruncationReport.ActiveRunsOmitted (CL-032: ≤10 active runs cap).
func TestBuildDefaultLimitsActiveRuns(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)

	// Write a queue.json with 12 dispatched items.
	writeQueueJSON(t, dir, 12)

	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if !d.Queue.Present {
		t.Fatal("expected Queue.Present=true")
	}
	if len(d.Queue.ActiveRuns) != 10 {
		t.Errorf("expected 10 active runs (capped); got %d", len(d.Queue.ActiveRuns))
	}
	if d.Queue.ActiveRunCount != 12 {
		t.Errorf("ActiveRunCount: got %d, want 12", d.Queue.ActiveRunCount)
	}
	// DC-005: the omission count MUST flow into the top-level TruncationReport
	// so the operator can tell how many runs were hidden.
	if d.Truncated == nil {
		t.Fatal("expected Truncated to be set when active runs are capped")
	}
	if d.Truncated.ActiveRunsOmitted != 2 {
		t.Errorf("ActiveRunsOmitted: got %d, want 2", d.Truncated.ActiveRunsOmitted)
	}
}

// TestBuildActiveRunsTruncationInJSON verifies the active_runs_omitted count is
// serialized into the JSON output's `truncated` object (DC-005 end-to-end).
func TestBuildActiveRunsTruncationInJSON(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)
	writeQueueJSON(t, dir, 15)

	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var raw struct {
		Truncated *struct {
			ActiveRunsOmitted int `json:"active_runs_omitted"`
		} `json:"truncated"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if raw.Truncated == nil {
		t.Fatal("truncated object missing from JSON output")
	}
	if raw.Truncated.ActiveRunsOmitted != 5 {
		t.Errorf("active_runs_omitted in JSON: got %d, want 5", raw.Truncated.ActiveRunsOmitted)
	}
}

// TestBuildActiveRunsFullNoTruncation verifies --full disables the active-run cap
// and reports no omission count.
func TestBuildActiveRunsFullNoTruncation(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)
	writeQueueJSON(t, dir, 15)

	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     FullLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if d.Truncated != nil && d.Truncated.ActiveRunsOmitted != 0 {
		t.Errorf("--full: expected no active-run truncation; got %d", d.Truncated.ActiveRunsOmitted)
	}
}

// TestBuildSurfacesCollectionErrors verifies DC-007: a non-fatal source failure
// (here, an unresolvable br binary) is reported in Errors[] rather than silently
// discarded, while Build still succeeds.
func TestBuildSurfacesCollectionErrors(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)

	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        time.Unix(1700000000, 0),
		// Point br at a path that does not exist so runCmd fails; DC-007 says the
		// error must surface in Errors[], not be swallowed.
		BrPath:   filepath.Join(dir, "no-such-br"),
		KerfPath: filepath.Join(dir, "no-such-kerf"),
	})
	if err != nil {
		t.Fatalf("Build should not hard-fail on br/kerf errors: %v", err)
	}
	if len(d.Errors) == 0 {
		t.Fatal("expected non-fatal collection errors to be surfaced in Errors[]")
	}
	var sawBr, sawKerf bool
	for _, e := range d.Errors {
		if strings.HasPrefix(e, "br_ready:") || strings.HasPrefix(e, "br_list:") {
			sawBr = true
		}
		if strings.HasPrefix(e, "kerf_next:") {
			sawKerf = true
		}
	}
	if !sawBr {
		t.Errorf("expected a br error in Errors[]; got %v", d.Errors)
	}
	if !sawKerf {
		t.Errorf("expected a kerf error in Errors[]; got %v", d.Errors)
	}
}

// TestBuildFullLimitsActiveRuns verifies that --full mode disables the 10-run cap
// and all 12 active runs are included.
func TestBuildFullLimitsActiveRuns(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)
	writeQueueJSON(t, dir, 12)

	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     FullLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(d.Queue.ActiveRuns) != 12 {
		t.Errorf("--full: expected 12 active runs; got %d", len(d.Queue.ActiveRuns))
	}
}

// TestBuildOpenNotesDefaultCap verifies that more than 20 open notes are
// truncated to 20 with the remainder reported in TruncationReport.OpenNotesOmitted
// (CL-032: ≤20 open notes cap).
func TestBuildOpenNotesDefaultCap(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)
	writeNotesJSONL(t, dir, 25)

	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(d.OpenNotes) != 20 {
		t.Errorf("expected 20 open notes (capped); got %d", len(d.OpenNotes))
	}
	if d.Truncated == nil || d.Truncated.OpenNotesOmitted != 5 {
		omitted := 0
		if d.Truncated != nil {
			omitted = d.Truncated.OpenNotesOmitted
		}
		t.Errorf("OpenNotesOmitted: got %d, want 5", omitted)
	}
}

// TestBuildOpenNotesFullMode verifies that --full disables the notes cap.
func TestBuildOpenNotesFullMode(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)
	writeNotesJSONL(t, dir, 25)

	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     FullLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(d.OpenNotes) != 25 {
		t.Errorf("--full notes: expected 25; got %d", len(d.OpenNotes))
	}
}

// TestBuildResolvedNotesExcluded verifies that resolved notes (resolved_at non-null)
// are excluded from the digest.
func TestBuildResolvedNotesExcluded(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)

	notesDir := filepath.Join(dir, ".harmonik", "cognition")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	notesPath := filepath.Join(notesDir, "notes.jsonl")
	f, err := os.Create(notesPath)
	if err != nil {
		t.Fatal(err)
	}
	resolved := "2026-01-01T00:00:00Z"
	lines := []map[string]interface{}{
		{"schema_version": 1, "ts": "2026-01-01T00:00:00Z", "tool_call_id": "a", "session_id": "s", "kind": "decision", "refs": []string{}, "text": "open note"},
		{"schema_version": 1, "ts": "2026-01-01T00:00:00Z", "tool_call_id": "b", "session_id": "s", "kind": "decision", "refs": []string{}, "text": "resolved note", "resolved_at": resolved},
	}
	enc := json.NewEncoder(f)
	for _, line := range lines {
		if err := enc.Encode(line); err != nil {
			t.Fatal(err)
		}
	}
	_ = f.Close()

	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(d.OpenNotes) != 1 {
		t.Errorf("expected 1 open note; got %d", len(d.OpenNotes))
	}
	if d.OpenNotes[0].Text != "open note" {
		t.Errorf("unexpected note text: %q", d.OpenNotes[0].Text)
	}
}

// TestBuildSchemaVersion verifies that the JSON output always carries schema_version=1.
func TestBuildSchemaVersion(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)
	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	sv, ok := raw["schema_version"]
	if !ok {
		t.Fatal("schema_version missing from JSON output")
	}
	if sv.(float64) != 1 {
		t.Errorf("schema_version: got %v, want 1", sv)
	}
}

// TestApplyNoteTruncation_NoCap verifies that when limits are 0 (full mode),
// no truncation occurs.
func TestApplyNoteTruncation_NoCap(t *testing.T) {
	t.Parallel()
	notes := make([]noteEntry, 30)
	for i := range notes {
		notes[i] = noteEntry{Kind: "decision", Text: "note"}
	}
	summaries, tr := applyNoteTruncation(notes, FullLimits(), nil)
	if len(summaries) != 30 {
		t.Errorf("full mode: expected 30; got %d", len(summaries))
	}
	if tr != nil && tr.OpenNotesOmitted != 0 {
		t.Errorf("full mode: expected 0 omitted; got %d", tr.OpenNotesOmitted)
	}
}

// TestApplyNoteTruncation_Cap verifies the 20-note ordinary cap.
func TestApplyNoteTruncation_Cap(t *testing.T) {
	t.Parallel()
	notes := make([]noteEntry, 22)
	for i := range notes {
		notes[i] = noteEntry{Kind: "decision", Text: "note"}
	}
	summaries, tr := applyNoteTruncation(notes, DefaultLimits(), nil)
	if len(summaries) != 20 {
		t.Errorf("cap: expected 20; got %d", len(summaries))
	}
	if tr == nil || tr.OpenNotesOmitted != 2 {
		omitted := 0
		if tr != nil {
			omitted = tr.OpenNotesOmitted
		}
		t.Errorf("omitted: got %d, want 2", omitted)
	}
}

// TestBuildPendingDecisionsUnacknowledged verifies EV-044: an unacknowledged
// decision_required event MUST appear in PendingDecisions even when it is before
// the SinceEventID watermark (i.e. in a "quiet" period where no recent events exist).
func TestBuildPendingDecisionsUnacknowledged(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)

	// Write a decision_required event with an "old" event_id.
	decisionEventID := "01900000-0000-7000-8000-000000000001"
	watermarkEventID := "01900000-0000-7000-8000-000000000099"
	writeDecisionEvents(t, dir, []testDecisionEvent{
		{
			eventID:     decisionEventID,
			evType:      "decision_required",
			ackToken:    "tok-aaa",
			subjectKind: "bead",
			subjectID:   "hk-test1",
			reason:      "bead_double_failure",
		},
	})

	// Parse watermarkEventID as an EventID for SinceEventID.
	watermarkUUID, err := uuid.Parse(watermarkEventID)
	if err != nil {
		t.Fatalf("parse watermark uuid: %v", err)
	}
	sinceID := core.EventID(watermarkUUID)

	d, err := Build(context.Background(), BuildInput{
		ProjectDir:   dir,
		Limits:       DefaultLimits(),
		SinceEventID: sinceID,
		Now:          time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// RecentEvents should be empty (decision is before the watermark).
	if len(d.RecentEvents) != 0 {
		t.Errorf("expected no recent events (all before watermark); got %d", len(d.RecentEvents))
	}

	// PendingDecisions MUST surface the unacknowledged decision regardless.
	if len(d.PendingDecisions) != 1 {
		t.Fatalf("expected 1 pending decision (EV-044); got %d", len(d.PendingDecisions))
	}
	pd := d.PendingDecisions[0]
	if pd.AckToken != "tok-aaa" {
		t.Errorf("ack_token: got %q, want %q", pd.AckToken, "tok-aaa")
	}
	if pd.SubjectKind != "bead" {
		t.Errorf("subject_kind: got %q, want %q", pd.SubjectKind, "bead")
	}
	if pd.SubjectID != "hk-test1" {
		t.Errorf("subject_id: got %q, want %q", pd.SubjectID, "hk-test1")
	}
	if pd.Reason != "bead_double_failure" {
		t.Errorf("reason: got %q, want %q", pd.Reason, "bead_double_failure")
	}
}

// TestBuildPendingDecisionsAcknowledged verifies EV-044: a decision_required event
// that has a matching decision_acknowledged MUST NOT appear in PendingDecisions.
func TestBuildPendingDecisionsAcknowledged(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)

	writeDecisionEvents(t, dir, []testDecisionEvent{
		{
			eventID:     "01900000-0000-7000-8000-000000000001",
			evType:      "decision_required",
			ackToken:    "tok-bbb",
			subjectKind: "bead",
			subjectID:   "hk-test2",
			reason:      "bead_double_failure",
		},
		{
			eventID:  "01900000-0000-7000-8000-000000000002",
			evType:   "decision_acknowledged",
			ackToken: "tok-bbb",
		},
	})

	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if len(d.PendingDecisions) != 0 {
		t.Errorf("expected 0 pending decisions after acknowledgement; got %d", len(d.PendingDecisions))
	}
}

// TestBuildPendingDecisionsMixed verifies EV-044 with multiple decisions where
// some are acknowledged and some are not.
func TestBuildPendingDecisionsMixed(t *testing.T) {
	t.Parallel()
	dir := makeMinimalProject(t)

	writeDecisionEvents(t, dir, []testDecisionEvent{
		// Unacknowledged
		{
			eventID:     "01900000-0000-7000-8000-000000000001",
			evType:      "decision_required",
			ackToken:    "tok-pending",
			subjectKind: "bead",
			subjectID:   "hk-pend",
			reason:      "bead_double_failure",
		},
		// Acknowledged: decision_required followed by decision_acknowledged
		{
			eventID:     "01900000-0000-7000-8000-000000000002",
			evType:      "decision_required",
			ackToken:    "tok-acked",
			subjectKind: "queue",
			subjectID:   "q-1",
			reason:      "queue_group_failure",
		},
		{
			eventID:  "01900000-0000-7000-8000-000000000003",
			evType:   "decision_acknowledged",
			ackToken: "tok-acked",
		},
	})

	d, err := Build(context.Background(), BuildInput{
		ProjectDir: dir,
		Limits:     DefaultLimits(),
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if len(d.PendingDecisions) != 1 {
		t.Fatalf("expected 1 pending decision; got %d", len(d.PendingDecisions))
	}
	if d.PendingDecisions[0].AckToken != "tok-pending" {
		t.Errorf("expected tok-pending; got %q", d.PendingDecisions[0].AckToken)
	}
}

// --- helpers ---

func makeMinimalProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeQueueJSON writes a queue.json with n dispatched items into dir.
func writeQueueJSON(t *testing.T, dir string, n int) {
	t.Helper()
	harmonikDir := filepath.Join(dir, ".harmonik")

	type itemJSON struct {
		BeadID   string  `json:"bead_id"`
		Status   string  `json:"status"`
		RunID    *string `json:"run_id"`
		Attempts int     `json:"attempts"`
	}
	type groupJSON struct {
		GroupIndex int        `json:"group_index"`
		Kind       string     `json:"kind"`
		Status     string     `json:"status"`
		Items      []itemJSON `json:"items"`
		CreatedAt  string     `json:"created_at"`
	}
	type queueJSON struct {
		SchemaVersion int         `json:"schema_version"`
		QueueID       string      `json:"queue_id"`
		SubmittedAt   string      `json:"submitted_at"`
		Groups        []groupJSON `json:"groups"`
		Status        string      `json:"status"`
	}

	items := make([]itemJSON, n)
	runID := "00000000-0000-0000-0000-000000000001"
	for i := 0; i < n; i++ {
		items[i] = itemJSON{
			BeadID:   "hk-test" + string(rune('a'+i%26)),
			Status:   "dispatched",
			RunID:    &runID,
			Attempts: 1,
		}
	}
	q := queueJSON{
		SchemaVersion: 1,
		QueueID:       "00000000-0000-0000-0000-000000000099",
		SubmittedAt:   "2026-01-01T00:00:00Z",
		Groups: []groupJSON{{
			GroupIndex: 0,
			Kind:       "stream",
			Status:     "active",
			Items:      items,
			CreatedAt:  "2026-01-01T00:00:00Z",
		}},
		Status: "active",
	}
	data, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	queuesDir := filepath.Join(harmonikDir, "queues")
	if err := os.MkdirAll(queuesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(queuesDir, "main.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// testDecisionEvent describes one event line to write into events.jsonl.
// For decision_required: fill ackToken, subjectKind, subjectID, reason.
// For decision_acknowledged: fill ackToken only.
type testDecisionEvent struct {
	eventID     string
	evType      string
	ackToken    string
	subjectKind string
	subjectID   string
	reason      string
}

// writeDecisionEvents writes the given events to .harmonik/events/events.jsonl.
func writeDecisionEvents(t *testing.T, dir string, events []testDecisionEvent) {
	t.Helper()
	eventsDir := filepath.Join(dir, ".harmonik", "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(eventsDir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	for _, ev := range events {
		var payload map[string]interface{}
		switch ev.evType {
		case "decision_required":
			payload = map[string]interface{}{
				"subject":             map[string]interface{}{"kind": ev.subjectKind, "id": ev.subjectID},
				"reason":              ev.reason,
				"suggested_action":    "",
				"ack_required":        true,
				"ack_token":           ev.ackToken,
				"triggering_event_id": "00000000-0000-7000-8000-000000000000",
			}
		case "decision_acknowledged":
			payload = map[string]interface{}{
				"ack_token":  ev.ackToken,
				"subject":    map[string]interface{}{"kind": "bead", "id": ""},
				"ack_method": "operator",
				"acked_at":   "2026-01-01T00:00:00Z",
			}
		default:
			t.Fatalf("unsupported test event type %q", ev.evType)
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		line := map[string]interface{}{
			"event_id":         ev.eventID,
			"schema_version":   1,
			"type":             ev.evType,
			"timestamp_wall":   "2026-01-01T00:00:00Z",
			"source_subsystem": "test",
			"payload":          json.RawMessage(payloadBytes),
		}
		if err := enc.Encode(line); err != nil {
			t.Fatal(err)
		}
	}
}

// writeNotesJSONL writes n unresolved notes into .harmonik/cognition/notes.jsonl.
func writeNotesJSONL(t *testing.T, dir string, n int) {
	t.Helper()
	notesDir := filepath.Join(dir, ".harmonik", "cognition")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(notesDir, "notes.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	for i := 0; i < n; i++ {
		note := map[string]interface{}{
			"schema_version": 1,
			"ts":             "2026-01-01T00:00:00Z",
			"tool_call_id":   "tc" + string(rune('a'+i%26)),
			"session_id":     "s1",
			"kind":           "decision",
			"refs":           []string{},
			"text":           "note text",
		}
		if err := enc.Encode(note); err != nil {
			t.Fatal(err)
		}
	}
}
