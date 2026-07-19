package scenario_test

// runids_from_events_a3_test.go — tests for RunIDsFromEvents, the join that
// feeds CheckPostSuiteLeaks the ExecutedRunIDs set (SH-INV-002). Finding A3.
//
// The harness runner collects the distinct run_ids observed across each
// scenario's captured event log and passes them to the post-suite leak sensor
// so its process- and lease-checks can match residual resources by run_id.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-002.

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/scenario"
)

// TestRunIDsFromEvents_DecodesAndDedupes verifies that run_id is decoded from
// the JSONL envelope and that RunIDsFromEvents returns distinct ids in
// first-seen order, skipping non-run-scoped events.
func TestRunIDsFromEvents_DecodesAndDedupes(t *testing.T) {
	t.Parallel()

	a := core.RunID(uuid.Must(uuid.NewV7()))
	b := core.RunID(uuid.Must(uuid.NewV7()))

	line := func(runID *core.RunID) scenario.RawEvent {
		env := map[string]any{"type": "run_started", "payload": map[string]any{}}
		if runID != nil {
			env["run_id"] = runID.String()
		}
		raw, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var ev scenario.RawEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return ev
	}

	events := []scenario.RawEvent{
		line(&a),
		line(nil), // non-run-scoped: skipped
		line(&a),  // duplicate: deduped
		line(&b),
	}

	got := scenario.RunIDsFromEvents(events)
	if len(got) != 2 {
		t.Fatalf("want 2 distinct run_ids, got %d: %v", len(got), got)
	}
	if got[0].String() != a.String() || got[1].String() != b.String() {
		t.Errorf("first-seen order violated: got %v, want [%s %s]", got, a, b)
	}
}

// TestRunIDsFromEvents_EmptyOnNone verifies the empty case: no run-scoped
// events yields a nil/empty slice (a suite with nothing executed).
func TestRunIDsFromEvents_EmptyOnNone(t *testing.T) {
	t.Parallel()

	if got := scenario.RunIDsFromEvents(nil); len(got) != 0 {
		t.Errorf("want empty for nil events, got %v", got)
	}
}
