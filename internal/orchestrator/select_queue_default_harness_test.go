package orchestrator

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// TestSelectQueueDefaultHarness proves the pure queue selector preserves the
// queue-owned tier-2 harness value without interpreting or narrowing it.
func TestSelectQueueDefaultHarness(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		queueDefault core.AgentType
	}{
		{name: "empty legacy queue preserves fallback input", queueDefault: core.AgentType("")},
		{name: "pi queue default propagates", queueDefault: core.AgentTypePi},
		{name: "valid non-pi queue default is not hard-coded away", queueDefault: core.AgentTypeCodex},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sel, ok := SelectNextQueue(FleetSnapshot{
				Queues: []QueueSnapshot{{
					Name:           "main",
					QueueID:        "qid-main",
					Active:         true,
					WorkerCap:      1,
					DefaultHarness: tc.queueDefault,
					ActiveGroup: &GroupSnapshot{
						GroupIndex: 0,
						Eligible: []ItemSnapshot{{
							ItemIdx: 0,
							BeadID:  core.BeadID("cq-def-01"),
						}},
					},
				}},
			})
			if !ok {
				t.Fatal("SelectNextQueue returned no selection")
			}
			if sel.DefaultHarness != tc.queueDefault {
				t.Fatalf("Selection.DefaultHarness = %q; want %q", sel.DefaultHarness, tc.queueDefault)
			}
		})
	}
}
