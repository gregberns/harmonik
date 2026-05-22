package scenariotest

// causality.go — AssertEventCausality helper for scenario tests (hk-xegej).
//
// AssertEventCausality asserts the "successor" invariant: every occurrence of a
// predicate event type must be followed by at least one successor event within a
// deadline measured against timestamp_wall.
//
// Spec ref: specs/scenario-harness.md §4 (assertion vocabulary).
// Bead: hk-xegej.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// timedEvent is an internal snapshot used only by the causality checker.
type timedEvent struct {
	Type          string
	RunID         string
	TimestampWall time.Time
	Raw           string
}

// AssertEventCausality reads all events from the JSONL at jsonlPath and
// asserts that for every occurrence of predType, at least one event whose type
// is in successorTypes appears within within of that occurrence's
// timestamp_wall.
//
// When predType never appears in the log the assertion passes vacuously — the
// invariant is only violated when the predicate fires but no qualifying
// successor follows.
//
// On failure the helper dumps the full event sequence (type + wall timestamp)
// so the reviewer can trace the exact ordering.
//
// Recommended default invariants for every daemon scenario test:
//
//	AssertEventCausality(t, jsonlPath, "run_started",
//	    []string{"run_completed", "run_failed", "run_cancelled"}, 60*time.Second)
//	AssertEventCausality(t, jsonlPath, "implementer_commit",
//	    []string{"reviewer_launched", "run_completed"}, 30*time.Second)
//
// Bead: hk-xegej.
func AssertEventCausality(
	t *testing.T,
	jsonlPath string,
	predType string,
	successorTypes []string,
	within time.Duration,
) {
	t.Helper()

	events := readTimedEvents(t, jsonlPath)

	succSet := make(map[string]struct{}, len(successorTypes))
	for _, s := range successorTypes {
		succSet[s] = struct{}{}
	}

	for i, ev := range events {
		if ev.Type != predType {
			continue
		}
		deadline := ev.TimestampWall.Add(within)
		found := false
		for j := i + 1; j < len(events); j++ {
			if _, ok := succSet[events[j].Type]; ok {
				if !events[j].TimestampWall.After(deadline) {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf(
				"AssertEventCausality: %q at %v has no successor in %v within %s\nfull event sequence:\n%s",
				predType, ev.TimestampWall.Format(time.RFC3339Nano),
				successorTypes, within,
				formatTimedEvents(events),
			)
		}
	}
}

// readTimedEvents decodes every non-empty JSONL line from jsonlPath into
// timedEvent values, preserving order.
func readTimedEvents(t *testing.T, jsonlPath string) []timedEvent {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("AssertEventCausality: open %s: %v", jsonlPath, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Logf("readTimedEvents: close: %v", closeErr)
		}
	}()

	var out []timedEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env struct {
			Type          string    `json:"type"`
			RunID         string    `json:"run_id"`
			TimestampWall time.Time `json:"timestamp_wall"`
		}
		if decErr := json.Unmarshal([]byte(line), &env); decErr == nil {
			out = append(out, timedEvent{
				Type:          env.Type,
				RunID:         env.RunID,
				TimestampWall: env.TimestampWall,
				Raw:           line,
			})
		}
	}
	return out
}

// formatTimedEvents returns a multi-line string listing each event's index,
// type, and wall timestamp — used in failure messages.
func formatTimedEvents(events []timedEvent) string {
	var sb strings.Builder
	for i, ev := range events {
		runPart := ""
		if ev.RunID != "" {
			runPart = fmt.Sprintf(" run_id=%s", ev.RunID)
		}
		fmt.Fprintf(&sb, "  [%3d] %-45s %s%s\n",
			i, ev.Type, ev.TimestampWall.Format(time.RFC3339Nano), runPart)
	}
	return sb.String()
}
