package main

import (
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
)

func commsRespond(conn net.Conn, resp map[string]any) {
	out, err := json.Marshal(resp)
	if err != nil {
		return
	}
	if _, writeErr := conn.Write(out); writeErr != nil {
		return
	}
}

func commsOpOf(req []byte) string {
	var parsed map[string]any
	if err := json.Unmarshal(req, &parsed); err != nil {
		return ""
	}
	op, ok := parsed["op"].(string)
	if !ok {
		return ""
	}
	return op
}

func commsStringField(m map[string]any, key string) string {
	v, ok := m[key].(string)
	if !ok {
		return ""
	}
	return v
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestCommsRecvJSON_CarriesEventIDField pins the NDJSON object `comms recv
// --json` emits per message. The agent-comms contract tells every agent to
// dedupe on `event_id`, so the spelling of that key is a fleet-wide API. The
// test asserts the full key set, because scripts/hk-wake.sh reads `from` and
// `event_id` off these same lines.
func TestCommsRecvJSON_CarriesEventIDField(t *testing.T) {
	out, _ := captureStd(t, func() {
		printCommsRecvMsg(
			true, // jsonOut
			"01965b00-0000-7000-8000-0000000000aa",
			"captain", "alice", "status", "the body",
			"01965b00-0000-7000-8000-0000000000bb",
			"2026-06-01T10:00:00Z",
		)
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("comms recv --json emitted a line that is not JSON: %v — %q", err, out)
	}

	want := map[string]string{
		"event_id":    "01965b00-0000-7000-8000-0000000000aa",
		"from":        "captain",
		"to":          "alice",
		"topic":       "status",
		"body":        "the body",
		"in_reply_to": "01965b00-0000-7000-8000-0000000000bb",
		"ts":          "2026-06-01T10:00:00Z",
	}
	for key, wantVal := range want {
		gotVal, ok := got[key]
		if !ok {
			t.Errorf("comms recv --json: key %q is missing — agents dedupe on event_id and parse the rest; got keys %v", key, mapKeys(got))
			continue
		}
		if gotVal != wantVal {
			t.Errorf("comms recv --json: key %q = %v, want %q", key, gotVal, wantVal)
		}
	}
	if len(got) != len(want) {
		t.Errorf("comms recv --json: got %d keys %v, want exactly %d %v", len(got), mapKeys(got), len(want), mapKeys(want))
	}
}

// TestCommsRecvJSON_OmitsEmptyOptionalKeys verifies that topic and in_reply_to
// drop out when unset, so a consumer can test for their presence.
func TestCommsRecvJSON_OmitsEmptyOptionalKeys(t *testing.T) {
	out, _ := captureStd(t, func() {
		printCommsRecvMsg(true, "01965b00-0000-7000-8000-0000000000aa",
			"captain", "alice", "", "the body", "", "2026-06-01T10:00:00Z")
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	for _, key := range []string{"topic", "in_reply_to"} {
		if _, present := got[key]; present {
			t.Errorf("comms recv --json: key %q must be omitted when empty; got keys %v", key, mapKeys(got))
		}
	}
	for _, key := range []string{"event_id", "from", "to", "body", "ts"} {
		if _, present := got[key]; !present {
			t.Errorf("comms recv --json: required key %q is missing; got keys %v", key, mapKeys(got))
		}
	}
}

// TestCommsRecvHuman_OmitsEventIDByDesign pins the split between the two output
// modes. Human output is for a person reading a pane; it deliberately drops the
// event id so that an agent which needs to dedupe has to reach for --json,
// where the key is stable. If the id leaks into the human line, agents start
// scraping it out of a format string that nothing holds still.
func TestCommsRecvHuman_OmitsEventIDByDesign(t *testing.T) {
	const eventID = "01965b00-0000-7000-8000-0000000000aa"
	const replyTo = "01965b00-0000-7000-8000-0000000000bb"

	for _, tc := range []struct {
		name  string
		topic string
	}{
		{"with topic", "status"},
		{"without topic", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _ := captureStd(t, func() {
				printCommsRecvMsg(false, eventID, "captain", "alice", tc.topic, "the body", replyTo, "2026-06-01T10:00:00Z")
			})
			if strings.Contains(out, eventID) {
				t.Errorf("human comms recv output must not carry the event id; got: %q", out)
			}
			if strings.Contains(out, replyTo) {
				t.Errorf("human comms recv output must not carry the in-reply-to id; got: %q", out)
			}
			for _, want := range []string{"captain", "alice", "the body"} {
				if !strings.Contains(out, want) {
					t.Errorf("human comms recv output is missing %q; got: %q", want, out)
				}
			}
		})
	}
}

// TestCommsRecvFollow_AnchorsOnScanAnchorWhenCursorEmpty guards a regression
// that already shipped once (GH #8 / hk-7xvf). When the catch-up drain matches
// no messages the daemon returns an empty cursor_after, but it still reports
// scan_anchor: the last event it looked at. The follow stream must anchor
// there. With an empty anchor the daemon skips replay altogether, so every
// message that lands between the drain and the subscriber registering on the
// hub is dropped for good — a silent loss, never an error.
func TestCommsRecvFollow_AnchorsOnScanAnchorWhenCursorEmpty(t *testing.T) {
	const scanAnchor = "01965b00-0000-7000-8000-00000000c0de"

	d := startFakeDaemon(t, func(conn net.Conn, req []byte) {
		if commsOpOf(req) == "comms-recv" {
			commsRespond(conn, map[string]any{
				"ok": true,
				"result": map[string]any{
					"messages":    []any{},
					"scan_anchor": scanAnchor,
				},
			})
			return
		}
		commsRespond(conn, map[string]any{"ok": false, "error": "test-stop"})
	})

	captureStd(t, func() {
		runCommsRecvSubcommand([]string{
			"--agent", "alice",
			"--socket", d.SockPath,
			"--follow",
			"--json",
		})
	})

	sub := commsAwaitRequest(t, d, "subscribe")
	got := commsStringField(sub, "since_event_id")
	if got != scanAnchor {
		t.Errorf("subscribe since_event_id = %q, want the drain's scan_anchor %q — an empty anchor makes the daemon skip replay and drop every message that arrived in the gap", got, scanAnchor)
	}
}

// TestCommsRecvFollow_PrefersCursorAfterOverScanAnchor is the other half of the
// same branch: once the agent has a real cursor, that cursor wins.
func TestCommsRecvFollow_PrefersCursorAfterOverScanAnchor(t *testing.T) {
	const cursorAfter = "01965b00-0000-7000-8000-00000000face"
	const scanAnchor = "01965b00-0000-7000-8000-00000000c0de"

	d := startFakeDaemon(t, func(conn net.Conn, req []byte) {
		if commsOpOf(req) == "comms-recv" {
			commsRespond(conn, map[string]any{
				"ok": true,
				"result": map[string]any{
					"messages":     []any{},
					"cursor_after": cursorAfter,
					"scan_anchor":  scanAnchor,
				},
			})
			return
		}
		commsRespond(conn, map[string]any{"ok": false, "error": "test-stop"})
	})

	captureStd(t, func() {
		runCommsRecvSubcommand([]string{
			"--agent", "alice",
			"--socket", d.SockPath,
			"--follow",
			"--json",
		})
	})

	sub := commsAwaitRequest(t, d, "subscribe")
	got := commsStringField(sub, "since_event_id")
	if got != cursorAfter {
		t.Errorf("subscribe since_event_id = %q, want the durable cursor_after %q", got, cursorAfter)
	}
}

// TestCommsRecvLiveFlag_SetOnlyForFollowAndWait pins amendment B1 (hk-8xspi):
// a --follow or --wait catch-up drain reads the LIVE cursor it shares with the
// subscribe session that follows it, while a plain one-shot recv reads the POLL
// cursor. Drop the flag and a follow session steals read position from an
// independent poller; add it to the plain path and the poller steals it back.
func TestCommsRecvLiveFlag_SetOnlyForFollowAndWait(t *testing.T) {
	for _, tc := range []struct {
		name     string
		extra    []string
		wantLive bool
	}{
		{"plain recv reads the poll cursor", nil, false},
		{"follow reads the live cursor", []string{"--follow"}, true},
		{"wait reads the live cursor", []string{"--wait"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := startFakeDaemon(t, func(conn net.Conn, req []byte) {
				if commsOpOf(req) == "comms-recv" {
					commsRespond(conn, map[string]any{
						"ok": true,
						"result": map[string]any{
							"messages": []any{map[string]any{
								"event_id": "01965b00-0000-7000-8000-0000000000aa",
								"from":     "captain",
								"to":       "alice",
								"body":     "hi",
								"ts":       "2026-06-01T10:00:00Z",
							}},
							"cursor_after": "01965b00-0000-7000-8000-0000000000aa",
						},
					})
					return
				}
				commsRespond(conn, map[string]any{"ok": false, "error": "test-stop"})
			})

			args := append([]string{"--agent", "alice", "--socket", d.SockPath, "--json"}, tc.extra...)
			captureStd(t, func() { runCommsRecvSubcommand(args) })

			drain := commsAwaitRequest(t, d, "comms-recv")
			live, present := commsRequestPayload(drain)["live"]
			if tc.wantLive {
				if !present || live != true {
					t.Errorf("comms-recv payload live = %v (present=%v), want true — a live drain must share the cursor with its subscribe session (B1)", live, present)
				}
			} else if present {
				t.Errorf("comms-recv payload carries live=%v; a plain one-shot recv must read the poll cursor (B1)", live)
			}
		})
	}
}

// TestCommsRecvDaemonDown_ExitsSeventeen verifies that recv reports the shared
// daemon-down code. Exit 1 means "your arguments were wrong", and a caller that
// reads 1 for a stopped daemon retries nothing and blames the operator.
func TestCommsRecvDaemonDown_ExitsSeventeen(t *testing.T) {
	absent := filepath.Join(newProjectFixture(t), ".harmonik", "daemon.sock")
	var code int
	captureStd(t, func() {
		code = runCommsRecvSubcommand([]string{"--agent", "alice", "--socket", absent})
	})
	if code != 17 {
		t.Errorf("comms recv with no daemon: exit = %d, want 17 (daemon down, not an argument error)", code)
	}
}
