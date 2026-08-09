package main

// comms_log_test.go — contract for `harmonik comms log`, the read-only operator
// view. It scans .harmonik/events/events.jsonl directly, so it needs a temp dir
// and no daemon at all.
//
// Two things are load-bearing here. First, --json emits the FULL event envelope
// per line (event_id, type, payload), not a flattened payload —
// scripts/crew-boot-digest.sh parses these lines. Second, --to must match the
// broadcast sentinel "*" as well as the named recipient: drop that clause and
// every broadcast quietly vanishes from every operator's log.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// captureCommsLog runs the log subcommand and returns its stdout and exit code.
func captureCommsLog(t *testing.T, args []string) (stdout string, exitCode int) {
	t.Helper()
	stdout, _ = captureStd(t, func() { exitCode = runCommsLogSubcommand(args) })
	return stdout, exitCode
}

// nonEmptyLines splits output into its non-blank lines.
func nonEmptyLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// ---------------------------------------------------------------------------
// --json emits the full event envelope
// ---------------------------------------------------------------------------

// TestCommsLogJSON_EmitsFullEventEnvelopePerLine verifies that each --json line
// is the whole event envelope. A flattened payload would drop event_id and
// type, and event_id is the only durable handle an operator or a boot digest
// has on a message.
func TestCommsLogJSON_EmitsFullEventEnvelopePerLine(t *testing.T) {
	const ts = "2026-06-01T10:00:00Z"
	const firstID = "01965b00-0000-7000-8000-000000000001"
	const secondID = "01965b00-0000-7000-8000-000000000002"

	dir := commsWriteEvents(t,
		commsMessageLine(t, firstID, ts, "alice", "bob", "status", "hello"),
		commsMessageLine(t, secondID, ts, "charlie", "dave", "", "world"),
	)

	out, code := captureCommsLog(t, []string{"--project", dir, "--json"})
	if code != 0 {
		t.Fatalf("comms log --json: exit = %d, want 0", code)
	}

	lines := nonEmptyLines(out)
	if len(lines) != 2 {
		t.Fatalf("comms log --json: got %d lines, want 2: %q", len(lines), out)
	}

	wantIDs := []string{firstID, secondID}
	for i, line := range lines {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("comms log --json line %d is not JSON: %v — %q", i, err, line)
		}
		// The envelope keys. A flattened payload has none of these.
		if got := commsStringField(ev, "event_id"); got != wantIDs[i] {
			t.Errorf("comms log --json line %d: event_id = %q, want %q — a flattened payload loses the only durable handle on a message", i, got, wantIDs[i])
		}
		if got := commsStringField(ev, "type"); got != "agent_message" {
			t.Errorf("comms log --json line %d: type = %q, want %q", i, got, "agent_message")
		}
		payload, ok := ev["payload"].(map[string]any)
		if !ok {
			t.Fatalf("comms log --json line %d: payload is missing or not an object; got keys %v", i, mapKeys(ev))
		}
		// The addressing fields live INSIDE payload, not at the top level.
		if _, present := ev["from"]; present {
			t.Errorf("comms log --json line %d: %q must live inside payload, not at the envelope top level", i, "from")
		}
		if got := commsStringField(payload, "from"); got == "" {
			t.Errorf("comms log --json line %d: payload.from is empty; got payload keys %v", i, mapKeys(payload))
		}
	}
}

// ---------------------------------------------------------------------------
// Filters and the two --since forms
// ---------------------------------------------------------------------------

// TestCommsLog_FiltersAndSinceForms covers the addressing filters and both
// --since spellings in one table. The --to case is the one that matters most:
// a broadcast is addressed to "*", so filtering for a named agent must still
// show it, or every fleet-wide announcement disappears from the operator's log.
func TestCommsLog_FiltersAndSinceForms(t *testing.T) {
	const ts = "2026-06-01T10:00:00Z"
	directed := commsMessageLine(t, "01965b00-0000-7000-8000-000000000001", ts, "alice", "bob", "status", "to bob")
	broadcast := commsMessageLine(t, "01965b00-0000-7000-8000-000000000002", ts, "alice", "*", "", "all hands")
	other := commsMessageLine(t, "01965b00-0000-7000-8000-000000000003", ts, "charlie", "dave", "work", "to dave")
	// A non-agent_message event. The human renderer never prints an event type,
	// so its absence can only be detected by counting lines.
	notAMessage := commsEventLine(t, "01965b00-0000-7000-8000-000000000004", ts, "run_started", map[string]any{})

	dir := commsWriteEvents(t, directed, broadcast, other, notAMessage)

	cases := []struct {
		name    string
		args    []string
		want    []string
		exclude []string
	}{
		{
			name: "no filter shows every agent_message and nothing else",
			args: nil,
			want: []string{"to bob", "all hands", "to dave"},
		},
		{
			name:    "from filters by sender",
			args:    []string{"--from", "alice"},
			want:    []string{"to bob", "all hands"},
			exclude: []string{"to dave"},
		},
		{
			name:    "to matches the named recipient AND the broadcast sentinel",
			args:    []string{"--to", "bob"},
			want:    []string{"to bob", "all hands"},
			exclude: []string{"to dave"},
		},
		{
			name:    "topic filters by topic",
			args:    []string{"--topic", "status"},
			want:    []string{"to bob"},
			exclude: []string{"all hands", "to dave"},
		},
		{
			name:    "since accepts an event id and skips at the boundary",
			args:    []string{"--since", "01965b00-0000-7000-8000-000000000001"},
			want:    []string{"all hands", "to dave"},
			exclude: []string{"to bob"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, code := captureCommsLog(t, append([]string{"--project", dir}, tc.args...))
			if code != 0 {
				t.Fatalf("comms log %v: exit = %d, want 0", tc.args, code)
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("comms log %v: missing %q; got: %q", tc.args, want, out)
				}
			}
			for _, unwanted := range tc.exclude {
				if strings.Contains(out, unwanted) {
					t.Errorf("comms log %v: %q must be filtered out; got: %q", tc.args, unwanted, out)
				}
			}
			// Exactly one line per matched message, and no line for anything
			// else. Counting is the only way to catch a non-agent_message event
			// leaking through, because the human line never names the type.
			if got := len(nonEmptyLines(out)); got != len(tc.want) {
				t.Errorf("comms log %v: got %d output lines, want exactly %d (one per matched message); got: %q", tc.args, got, len(tc.want), out)
			}
		})
	}
}

// TestCommsLog_SinceAcceptsFriendlyDurations covers the duration form of
// --since end to end, including the d and w suffixes Go's parser rejects. A
// week that silently means a day quietly hides six days of history.
func TestCommsLog_SinceAcceptsFriendlyDurations(t *testing.T) {
	now := time.Now()
	tenDaysAgo := now.Add(-10 * 24 * time.Hour).UTC().Format(time.RFC3339)
	twoDaysAgo := now.Add(-2 * 24 * time.Hour).UTC().Format(time.RFC3339)
	justNow := now.Add(-5 * time.Minute).UTC().Format(time.RFC3339)

	dir := commsWriteEvents(t,
		commsMessageLine(t, "01965b00-0000-7000-8000-000000000001", tenDaysAgo, "alice", "bob", "", "ten days old"),
		commsMessageLine(t, "01965b00-0000-7000-8000-000000000002", twoDaysAgo, "alice", "bob", "", "two days old"),
		commsMessageLine(t, "01965b00-0000-7000-8000-000000000003", justNow, "alice", "bob", "", "five minutes old"),
	)

	cases := []struct {
		since   string
		want    []string
		exclude []string
	}{
		{"30m", []string{"five minutes old"}, []string{"two days old", "ten days old"}},
		{"8d", []string{"five minutes old", "two days old"}, []string{"ten days old"}},
		// 2 weeks reaches all three. If the week branch decayed to hours or
		// days, the ten-day-old message would drop out.
		{"2w", []string{"five minutes old", "two days old", "ten days old"}, nil},
	}

	for _, tc := range cases {
		t.Run("since "+tc.since, func(t *testing.T) {
			out, code := captureCommsLog(t, []string{"--project", dir, "--since", tc.since})
			if code != 0 {
				t.Fatalf("comms log --since %s: exit = %d, want 0", tc.since, code)
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("comms log --since %s: missing %q; got: %q", tc.since, want, out)
				}
			}
			for _, unwanted := range tc.exclude {
				if strings.Contains(out, unwanted) {
					t.Errorf("comms log --since %s: %q is outside the window; got: %q", tc.since, unwanted, out)
				}
			}
		})
	}
}

// TestCommsLog_UnparseableSinceExitsOneAndNamesBothForms verifies the error
// path tells the operator what --since actually accepts. A bare "invalid value"
// leaves them guessing between an id and a duration.
func TestCommsLog_UnparseableSinceExitsOneAndNamesBothForms(t *testing.T) {
	dir := commsWriteEvents(t)

	var code int
	_, errOut := captureStd(t, func() {
		code = runCommsLogSubcommand([]string{"--project", dir, "--since", "last-tuesday"})
	})

	if code != 1 {
		t.Errorf("comms log --since last-tuesday: exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "last-tuesday") {
		t.Errorf("the --since error must quote the offending value; got: %q", errOut)
	}
	for _, form := range []string{"event_id", "duration"} {
		if !strings.Contains(errOut, form) {
			t.Errorf("the --since error must name the %q form it accepts; got: %q", form, errOut)
		}
	}
}

// TestParseFriendlyDuration covers the d and w suffixes directly, including the
// rejections that must fall through to the standard parser.
func TestParseFriendlyDuration(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{in: "30m", want: 30 * time.Minute},
		{in: "1h", want: time.Hour},
		{in: "1d", want: 24 * time.Hour},
		{in: "8d", want: 8 * 24 * time.Hour},
		{in: "1w", want: 7 * 24 * time.Hour},
		{in: "2w", want: 2 * 7 * 24 * time.Hour},
		{in: "1h30m", want: 90 * time.Minute},
		{in: "0d", wantErr: true},
		{in: "d", wantErr: true},
		{in: "w", wantErr: true},
		{in: "1x", wantErr: true},
		{in: "last-tuesday", wantErr: true},
	}
	for _, tc := range cases {
		got, err := parseFriendlyDuration(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseFriendlyDuration(%q) = %v, want an error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseFriendlyDuration(%q): unexpected error: %v", tc.in, err)
		} else if got != tc.want {
			t.Errorf("parseFriendlyDuration(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
