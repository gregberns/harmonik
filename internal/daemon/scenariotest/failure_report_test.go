package scenariotest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ev builds a captured event the way readAllEvents would return it.
func ev(eventType, runID, payload string) CapturedEvent {
	raw := `{"type":"` + eventType + `","run_id":"` + runID + `","payload":` + payload + `}`
	return CapturedEvent{Type: eventType, RunID: runID, Raw: raw}
}

// The whole point of the report: the reason the daemon gave must reach the test
// log. This is the assertion that was missing while three merge gates went red
// and printed counts only.
func TestFormatRunFailures_SurfacesTheReason(t *testing.T) {
	const reason = "dot: no-progress detected at iteration 3: HEAD did not advance"
	events := []CapturedEvent{
		ev("run_started", "01a03203-5fe3-73c0-b91e-f5d38fe92af8", `{}`),
		ev("run_failed", "01a03203-5fe3-73c0-b91e-f5d38fe92af8",
			`{"success":false,"summary":"`+reason+`"}`),
	}

	got := formatRunFailures(events, "/tmp/events.jsonl")

	if !strings.Contains(got, reason) {
		t.Errorf("report does not carry the failure reason.\nwant substring: %s\ngot:\n%s", reason, got)
	}
	if !strings.Contains(got, "run_failed") {
		t.Errorf("report does not name the terminal event type.\ngot:\n%s", got)
	}
}

// The emitted payload and core.RunFailedPayload have drifted apart before: the
// daemon writes summary, the struct declares reason. A report that decoded into
// known field names would print an empty reason on one of these two shapes.
func TestFormatRunFailures_CarriesEitherPayloadShape(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"emitted shape", `{"success":false,"summary":"trust key did not persist"}`, "trust key did not persist"},
		{"declared shape", `{"failure_class":"structural","reason":"agent_ready timeout"}`, "agent_ready timeout"},
		{"unknown future field", `{"whatever_comes_next":"the cause"}`, "the cause"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatRunFailures([]CapturedEvent{ev("run_failed", "r1", tc.payload)}, "log")
			if !strings.Contains(got, tc.want) {
				t.Errorf("payload detail lost.\nwant substring: %s\ngot:\n%s", tc.want, got)
			}
		})
	}
}

// A malformed line must still reach the reader, and it must survive the WHOLE
// path — a daemon killed mid-write truncates its last line, and that is the
// line saying why the run died. This test goes through readAllEvents on a real
// file rather than hand-building a CapturedEvent, because the earlier version
// of it passed while the production path silently dropped the line.
func TestFormatRunFailures_KeepsUndecodableLineThroughTheRealReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	content := `{"type":"run_started","run_id":"r1","payload":{}}` + "\n" +
		`{"type":"run_failed","run_id":"r1","payload":{"summary":"THE REASON` // truncated mid-write
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write events log: %v", err)
	}

	got := formatRunFailures(readAllEvents(t, path), path)

	if !strings.Contains(got, "THE REASON") {
		t.Errorf("the truncated final line was dropped, taking the reason with it.\ngot:\n%s", got)
	}
}

// The happy-path test reached a terminal failure and printed nothing. When there
// is no failure event to quote, the report must say so and hand over the trail,
// because the trail is then the whole of the evidence.
func TestFormatRunFailures_NoTerminalEventStillReportsTheTrail(t *testing.T) {
	events := []CapturedEvent{
		ev("run_started", "r1", `{}`),
		ev("node_dispatch_requested", "r1", `{}`),
		ev("node_dispatch_decided", "r1", `{}`),
	}
	got := formatRunFailures(events, "log")
	if !strings.Contains(got, "no run_failed") {
		t.Errorf("report does not say the failure was not a run outcome.\ngot:\n%s", got)
	}
	for _, want := range []string{"run_started", "node_dispatch_requested", "node_dispatch_decided"} {
		if !strings.Contains(got, want) {
			t.Errorf("trail is missing %s.\ngot:\n%s", want, got)
		}
	}
}

// With three runs in flight, a report that cannot tell them apart sends the
// reader to the wrong run.
func TestFormatRunFailures_SeparatesConcurrentRuns(t *testing.T) {
	events := []CapturedEvent{
		ev("run_started", "aaaa1111-0000-0000-0000-000000000001", `{}`),
		ev("run_started", "bbbb2222-0000-0000-0000-000000000002", `{}`),
		ev("run_started", "cccc3333-0000-0000-0000-000000000003", `{}`),
		ev("run_completed", "aaaa1111-0000-0000-0000-000000000001", `{}`),
		ev("run_failed", "bbbb2222-0000-0000-0000-000000000002", `{"summary":"the one that broke"}`),
		ev("run_completed", "cccc3333-0000-0000-0000-000000000003", `{}`),
	}
	got := formatRunFailures(events, "log")

	if !strings.Contains(got, "run=bbbb2222") {
		t.Errorf("failing run is not identified.\ngot:\n%s", got)
	}
	if strings.Contains(got, "trail run=aaaa1111") || strings.Contains(got, "trail run=cccc3333") {
		t.Errorf("a completed run's trail was printed; only unfinished runs are worth the noise.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "trail run=bbbb2222") {
		t.Errorf("the unfinished run has no trail.\ngot:\n%s", got)
	}
}

// An empty log is a distinct diagnosis from a log with no failure in it: the
// daemon never wrote anything.
func TestFormatRunFailures_EmptyLogSaysSo(t *testing.T) {
	got := formatRunFailures(nil, "log")
	if !strings.Contains(got, "no events at all") {
		t.Errorf("empty log is not called out.\ngot:\n%s", got)
	}
}

// A test can fail while every run succeeded — a sequence assertion fails exactly
// because an event it wanted is absent. Then the full trail is the whole of the
// evidence, and printing nothing sends the reader back to the deleted temp dir.
func TestFormatRunFailures_AllRunsCompletedStillShowsTheTrail(t *testing.T) {
	events := []CapturedEvent{
		ev("run_started", "r1", `{}`),
		ev("launch_initiated", "r1", `{}`),
		ev("run_completed", "r1", `{}`),
	}
	got := formatRunFailures(events, "log")
	if !strings.Contains(got, "trail run=r1") {
		t.Errorf("no trail printed for a completed run on a failing test.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "launch_initiated") {
		t.Errorf("trail is missing the events that show what did and did not happen.\ngot:\n%s", got)
	}
}

// Distinguish "runs happened and none failed" from "nothing ever ran".
func TestFormatRunFailures_NoRunScopedEventsSaysSo(t *testing.T) {
	events := []CapturedEvent{ev("daemon_started", "", `{}`)}
	got := formatRunFailures(events, "log")
	if !strings.Contains(got, "no run ever started") {
		t.Errorf("a log with no run-scoped events is not called out.\ngot:\n%s", got)
	}
}

// Most of a scenario run's events carry no run_id — measured at 17 of 32 on a
// real N1 run, including outcome_emitted, agent_completed and agent_ready. A
// report keyed only on run_id drops exactly the evidence it was written to
// surface.
func TestFormatRunFailures_KeepsEventsWithNoRunID(t *testing.T) {
	events := []CapturedEvent{
		ev("run_started", "r1", `{}`),
		ev("agent_ready", "", `{}`),
		ev("outcome_emitted", "", `{"outcome_status":"failure","node_id":"implementer"}`),
		ev("agent_completed", "", `{}`),
		ev("run_failed", "r1", `{"summary":"the reason"}`),
	}
	got := formatRunFailures(events, "log")

	for _, want := range []string{"agent_ready", "agent_completed", "belonging to no run"} {
		if !strings.Contains(got, want) {
			t.Errorf("unattributed event %q dropped from the report.\ngot:\n%s", want, got)
		}
	}
	// outcome_emitted is a payload the bead asked for by name, not just a type
	// in a trail.
	if !strings.Contains(got, `"outcome_status":"failure"`) {
		t.Errorf("outcome_emitted payload not printed.\ngot:\n%s", got)
	}
}

// outcome_emitted leaves the envelope run_id empty and writes the same ID into
// its payload. With three runs in flight, a report that shows "-" for all of
// them cannot answer the only question being asked: which run broke.
func TestFormatRunFailures_ReadsRunIDFromPayloadWhenEnvelopeIsEmpty(t *testing.T) {
	e := CapturedEvent{
		Type:  "outcome_emitted",
		RunID: "",
		Raw:   `{"type":"outcome_emitted","payload":{"run_id":"bbbb2222-0000-0000-0000-000000000002","outcome_status":"failure"}}`,
	}
	got := formatRunFailures([]CapturedEvent{e}, "log")
	if !strings.Contains(got, "run=bbbb2222") {
		t.Errorf("run ID in the payload was not used when the envelope had none.\ngot:\n%s", got)
	}
}

// A stalled run carries no prose of its own: core.RunStalePayload holds counters
// only, and the cause is written into the lifecycle_transition that precedes it.
// Dropping that type leaves the second-most-likely gate failure with no reason —
// the defect this whole report was written to remove.
func TestFormatRunFailures_CarriesTheLifecycleReasonBehindAStall(t *testing.T) {
	events := []CapturedEvent{
		ev("run_started", "r1", `{}`),
		ev("lifecycle_transition", "r1",
			`{"from_state":"Terminating","to_state":"Failed","err_code":"exit_error","err_msg":"signal: segmentation fault"}`),
		ev("run_stale", "r1", `{"age_seconds":600,"no_progress_seconds":600}`),
	}
	got := formatRunFailures(events, "log")

	if !strings.Contains(got, "signal: segmentation fault") {
		t.Errorf("the lifecycle reason behind the stall is missing.\ngot:\n%s", got)
	}
	if strings.Contains(got, "no run_failed") {
		t.Errorf("run_stale was not counted as a terminal failure.\ngot:\n%s", got)
	}
}

// run_cancelled is named by the assertions in RunConcurrentMerge, so it has to
// count as a terminal failure too.
func TestFormatRunFailures_CountsEveryTerminalFailureType(t *testing.T) {
	for _, eventType := range []string{"run_failed", "run_stale", "run_cancelled"} {
		t.Run(eventType, func(t *testing.T) {
			got := formatRunFailures([]CapturedEvent{ev(eventType, "r1", `{"summary":"why"}`)}, "log")
			if strings.Contains(got, "no run_failed") {
				t.Errorf("%s did not count as a terminal failure.\ngot:\n%s", eventType, got)
			}
			if !strings.Contains(got, "why") {
				t.Errorf("%s payload not printed.\ngot:\n%s", eventType, got)
			}
		})
	}
}

// The detail line must be the payload, not the whole envelope. An envelope
// carries event_id, schema_version and timestamps that push the cause off the
// end of a terminal line, which is how a reason gets missed even when printed.
func TestFormatRunFailures_PrintsThePayloadNotTheWholeEnvelope(t *testing.T) {
	e := CapturedEvent{
		Type:  "run_failed",
		RunID: "r1",
		Raw: `{"event_id":"01a03203-0000-7000-8000-000000000001","schema_version":1,` +
			`"type":"run_failed","timestamp_wall":"2026-08-24T04:51:17.424068Z",` +
			`"run_id":"r1","source_subsystem":"eventbus","payload":{"summary":"the cause"}}`,
	}
	got := formatRunFailures([]CapturedEvent{e}, "log")

	if !strings.Contains(got, "the cause") {
		t.Fatalf("the cause is missing.\ngot:\n%s", got)
	}
	if strings.Contains(got, "source_subsystem") || strings.Contains(got, "schema_version") {
		t.Errorf("envelope bookkeeping was printed instead of the payload alone.\ngot:\n%s", got)
	}
}

// The report has to say which log it read. A scenario temp dir is named after
// the test and the run, and without the path a reader cannot tell two runs of
// the same test apart.
func TestFormatRunFailures_NamesTheLogItRead(t *testing.T) {
	const path = "/tmp/TestScenario_Whatever123/001/.harmonik/events/events.jsonl"
	got := formatRunFailures([]CapturedEvent{ev("run_failed", "r1", `{}`)}, path)
	if !strings.Contains(got, path) {
		t.Errorf("report does not name the log it read.\ngot:\n%s", got)
	}
}

// The read-through must be all-or-nothing. Applied only to the detail line, the
// report named a run and then, two lines down, listed the same event as
// belonging to no run.
func TestFormatRunFailures_DoesNotContradictItselfAboutAttribution(t *testing.T) {
	events := []CapturedEvent{
		ev("run_started", "bbbb2222-0000-0000-0000-000000000002", `{}`),
		{
			Type: "outcome_emitted", RunID: "",
			Raw: `{"type":"outcome_emitted","payload":{"run_id":"bbbb2222-0000-0000-0000-000000000002","outcome_status":"failure"}}`,
		},
		ev("run_failed", "bbbb2222-0000-0000-0000-000000000002", `{"summary":"boom"}`),
	}
	got := formatRunFailures(events, "log")

	trail := ""
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "trail run=bbbb2222") {
			trail = line
			break
		}
	}
	if trail == "" {
		t.Fatalf("the failing run has no trail.\ngot:\n%s", got)
	}
	if !strings.Contains(trail, "outcome_emitted") {
		t.Errorf("an event attributed on the detail line is absent from that run's trail.\ngot:\n%s", got)
	}
	if strings.Contains(got, "belonging to no run") {
		t.Errorf("an event the report attributed to a run is also called unattributed.\ngot:\n%s", got)
	}
}

// shortRun must not collapse an ID to its first segment. Not every run ID is a
// UUID: a handler forwards its own synthetic one, and cutting at the first dash
// rendered every "run-..." ID as the bare word "run".
//
// It does NOT promise to tell two synthetic IDs apart, and it cannot. The twin
// declares its run ID as a single const (cmd/harmonik-twin-claude/scenarios.go,
// scenarioCommitOnCueStartupDelay), so every concurrent run of that scenario
// emits the SAME id and no display can separate them. Grouping is unaffected —
// unfinishedRuns, allRuns and trailFor all key on the full ID.
func TestShortRun_DoesNotCollapseAnIDToItsFirstSegment(t *testing.T) {
	if got := shortRun("run-hk8ys88-coc-001"); got == "run" {
		t.Errorf("a synthetic run ID collapsed to %q — it identifies nothing", got)
	}
	if got := shortRun("01a032e6-6b39-7167-87ab-ece0ad1ecdaf"); got != "01a032e6" {
		t.Errorf("a UUID should shorten to its first block, got %q", got)
	}
	if got := shortRun(""); got != "-" {
		t.Errorf("an absent run ID should render as %q, got %q", "-", got)
	}
}
