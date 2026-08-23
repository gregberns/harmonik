package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func commsAwaitRequest(t *testing.T, d *fakeDaemon, op string) map[string]any {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case raw := <-d.Requests():
			if raw == nil {
				continue
			}
			var req map[string]any
			if err := json.Unmarshal(raw, &req); err != nil {
				continue
			}
			if commsStringField(req, "op") == op {
				return req
			}
		case <-deadline:
			t.Fatalf("no %q request reached the fake daemon within 5s", op)
			return nil
		}
	}
}

func commsPendingRequestCount(d *fakeDaemon) int {
	n := 0
	for {
		select {
		case <-d.Requests():
			n++
		default:
			return n
		}
	}
}

func commsRequestPayload(req map[string]any) map[string]any {
	p, ok := req["payload"].(map[string]any)
	if !ok {
		return nil
	}
	return p
}

func commsWriteEvents(t *testing.T, lines ...string) string {
	t.Helper()
	dir := t.TempDir()
	eventsDir := filepath.Join(dir, ".harmonik", "events")
	if err := os.MkdirAll(eventsDir, 0o750); err != nil {
		t.Fatalf("commsWriteEvents: mkdir: %v", err)
	}
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}
	if err := os.WriteFile(filepath.Join(eventsDir, "events.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatalf("commsWriteEvents: write: %v", err)
	}
	return dir
}

func commsEventLine(t *testing.T, eventID, ts, evType string, payload map[string]any) string {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("commsEventLine: marshal payload: %v", err)
	}
	line, err := json.Marshal(map[string]any{
		"event_id":         eventID,
		"schema_version":   1,
		"type":             evType,
		"timestamp_wall":   ts,
		"source_subsystem": "daemon.comms",
		"payload":          json.RawMessage(payloadBytes),
	})
	if err != nil {
		t.Fatalf("commsEventLine: marshal event: %v", err)
	}
	return string(line)
}

func commsMessageLine(t *testing.T, eventID, ts, from, to, topic, body string) string {
	t.Helper()
	payload := map[string]any{"from": from, "to": to, "body": body}
	if topic != "" {
		payload["topic"] = topic
	}
	return commsEventLine(t, eventID, ts, "agent_message", payload)
}

func commsPresenceLine(t *testing.T, eventID, ts, agent, status, reason string) string {
	t.Helper()
	return commsEventLine(t, eventID, ts, "agent_presence", map[string]any{
		"agent":     agent,
		"status":    status,
		"last_seen": ts,
		"reason":    reason,
	})
}

// TestCommsRouter_UnknownVerbExitsTwoAndNamesTheVerbs verifies that an
// unrecognised verb exits 2, which no verb handler returns, so a caller can
// tell a typo apart from a real failure inside a verb. The message must also
// list the verbs that exist — an agent reading only stderr should not have to
// go find the usage text.
func TestCommsRouter_UnknownVerbExitsTwoAndNamesTheVerbs(t *testing.T) {
	var code int
	_, errOut := captureStd(t, func() {
		code = runCommsSubcommand([]string{"recvv"})
	})

	if code != 2 {
		t.Errorf("comms with an unknown verb: exit = %d, want 2 (a typo, not a handler failure)", code)
	}
	if !strings.Contains(errOut, "recvv") {
		t.Errorf("the unknown-verb error must quote what was typed; got: %q", errOut)
	}
	for _, verb := range []string{"send", "log", "join", "leave", "who", "recv"} {
		if !strings.Contains(errOut, verb) {
			t.Errorf("the unknown-verb error must name the %q verb; got: %q", verb, errOut)
		}
	}
}

// TestCommsRouter_NoVerbPrintsUsageAndExitsZero verifies that a bare
// `harmonik comms` is help, not an error.
func TestCommsRouter_NoVerbPrintsUsageAndExitsZero(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"-h"}} {
		var code int
		out, _ := captureStd(t, func() { code = runCommsSubcommand(args) })
		if code != 0 {
			t.Errorf("comms %v: exit = %d, want 0", args, code)
		}
		if !strings.Contains(out, "recv") {
			t.Errorf("comms %v: usage must list the verbs; got: %q", args, out)
		}
	}
}
