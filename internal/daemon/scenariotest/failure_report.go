package scenariotest

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// terminalFailureTypes are the event types that end a run without success.
// run_failed is the one that carries a written reason. run_stale explains itself
// through the lifecycle_transition that precedes it, which is why that type is a
// detail type below. run_cancelled has no emitter in the tree today and is here
// because the assertions in RunConcurrentMerge already name it.
var terminalFailureTypes = []string{"run_failed", "run_stale", "run_cancelled"}

// detailTypes are the events whose payload is printed in full.
//
// The terminal failures carry the reason. outcome_emitted carries the status of
// the node that produced it, which is the next question a reader asks. And
// lifecycle_transition carries reason, err_code and err_msg — for a stalled run
// it is the ONLY place the cause is written, because core.RunStalePayload holds
// counters and no prose. Reading one out of a preserved log by hand is how the
// fork/exec segfault behind an earlier gate red was identified.
var detailTypes = append([]string{"outcome_emitted", "lifecycle_transition", UndecodableEventType}, terminalFailureTypes...)

// ReportRunFailures makes a failing scenario test print WHY its runs failed.
//
// It registers a cleanup that does nothing while the test passes. When the test
// has failed, it reads the run log and writes out the full payload of every
// event that carries a cause, plus the event trail of each run that did not
// reach run_completed.
//
// Why this exists: a scenario test boots a daemon inside a t.TempDir() that Go
// deletes when the test ends. The reason a run failed is in that directory and
// nowhere else, so a red gate used to report counts only and every reproduction
// cost a manual dig into a directory that was already gone. Call this once,
// after the run log path is known and after the project dir is created, so the
// dump runs before Go removes the directory (cleanups run last-registered-first).
func ReportRunFailures(t *testing.T, jsonlPath string) {
	t.Helper()
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		t.Log(formatRunFailures(readAllEvents(t, jsonlPath), jsonlPath))
	})
}

// formatRunFailures renders the failure diagnosis for a captured event log. It
// is separate from the cleanup above so the whole report can be tested against
// synthetic logs without booting a daemon.
func formatRunFailures(events []CapturedEvent, jsonlPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "run failure detail from %s:\n", jsonlPath)
	if len(events) == 0 {
		b.WriteString("  the run log is empty or absent — the daemon wrote no events at all.\n")
		return b.String()
	}

	failures := 0
	for _, e := range events {
		if !inSet(detailTypes, e.Type) {
			continue
		}
		if isTerminalFailure(e.Type) {
			failures++
		}
		fmt.Fprintf(&b, "  %s run=%s payload=%s\n", e.Type, shortRun(runIDOf(e)), payloadJSON(e.Raw))
	}
	if failures == 0 {
		b.WriteString("  no run_failed, run_stale or run_cancelled event — " +
			"the failure is not a run outcome. Read the trail below.\n")
	}

	// Unfinished runs are the interesting ones. When every run completed, the
	// test still failed on something, and the full trail is then the only
	// evidence there is — a sequence assertion fails precisely because an event
	// it wanted is missing from this list.
	trails := unfinishedRuns(events)
	if len(trails) == 0 {
		trails = allRuns(events)
	}
	for _, runID := range trails {
		fmt.Fprintf(&b, "  trail run=%s: %s\n", shortRun(runID), strings.Join(trailFor(events, runID), " "))
	}
	if len(trails) == 0 {
		b.WriteString("  no run-scoped events — no run ever started.\n")
	}

	// What is left belongs to no run by either route. Without this the events
	// vanish from the report, and they are not junk: the daemon-level ones say
	// what the daemon was doing when the run died.
	if orphans := trailFor(events, ""); len(orphans) > 0 {
		fmt.Fprintf(&b, "  events belonging to no run: %s\n", strings.Join(orphans, " "))
	}
	return b.String()
}

func isTerminalFailure(eventType string) bool {
	return inSet(terminalFailureTypes, eventType)
}

func inSet(set []string, want string) bool {
	for _, s := range set {
		if s == want {
			return true
		}
	}
	return false
}

// runIDOf returns the run this event belongs to. The envelope field is the right
// place for it, and several emitters leave it empty while writing the same ID
// into the payload — every one of the 1326 outcome_emitted events in the live
// log does exactly that. Every part of this report goes through here, so an
// event is called unattributed only when neither place has an ID.
//
// This makes the report usable while the emitters are wrong. It does not repair
// them; that is tracked separately.
func runIDOf(e CapturedEvent) string {
	if e.RunID != "" {
		return e.RunID
	}
	var env struct {
		Payload struct {
			RunID string `json:"run_id"`
		} `json:"payload"`
	}
	if json.Unmarshal([]byte(e.Raw), &env) != nil {
		return ""
	}
	return env.Payload.RunID
}

// payloadJSON pulls the payload object back out of a raw envelope line, so the
// detail line carries the cause and not the envelope's bookkeeping. On any
// decode trouble it returns the whole line: too much detail beats none.
//
// The payload is printed as JSON rather than decoded into a payload struct. The
// emitted shape and core.RunFailedPayload have already drifted apart — the
// daemon writes summary, the struct declares reason — and a decoder that knows
// the wrong field names reports an empty reason on a run that explained itself
// perfectly well. Raw JSON cannot drift.
func payloadJSON(raw string) string {
	var env struct {
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal([]byte(raw), &env) != nil || len(env.Payload) == 0 {
		return raw
	}
	return string(env.Payload)
}

// unfinishedRuns lists the runs that never reached run_completed, in first-seen
// order. Those are the ones whose trail is worth printing.
func unfinishedRuns(events []CapturedEvent) []string {
	order := map[string]int{}
	completed := map[string]bool{}
	for i, e := range events {
		runID := runIDOf(e)
		if runID == "" {
			continue
		}
		if _, seen := order[runID]; !seen {
			order[runID] = i
		}
		if e.Type == "run_completed" {
			completed[runID] = true
		}
	}
	var out []string
	for runID := range order {
		if !completed[runID] {
			out = append(out, runID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return order[out[i]] < order[out[j]] })
	return out
}

// allRuns lists every run ID in first-seen order.
func allRuns(events []CapturedEvent) []string {
	order := map[string]bool{}
	out := make([]string, 0, len(events))
	for _, e := range events {
		runID := runIDOf(e)
		if runID == "" || order[runID] {
			continue
		}
		order[runID] = true
		out = append(out, runID)
	}
	return out
}

func trailFor(events []CapturedEvent, runID string) []string {
	var out []string
	for _, e := range events {
		if runIDOf(e) == runID {
			out = append(out, e.Type)
		}
	}
	return out
}

// shortRun keeps a run ID readable in a log line. Eight characters is the first
// block of a UUID, which differs often enough to tell three concurrent runs
// apart.
//
// It is a fixed width rather than "up to the first dash" because not every run
// ID is a UUID: a handler forwards its own synthetic ID, and cutting
// "run-hk8ys88-coc-001" at the first dash renders every such run as "run".
func shortRun(runID string) string {
	if runID == "" {
		return "-"
	}
	const width = 8
	if len(runID) > width {
		return runID[:width]
	}
	return runID
}
