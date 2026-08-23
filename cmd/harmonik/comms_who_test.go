package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type whoLine struct {
	Agent    string `json:"agent"`
	LastSeen string `json:"last_seen"`
	Status   string `json:"status"`
}

func captureCommsWho(t *testing.T, projectDir string, jsonOut bool) (stdout string, exitCode int) {
	t.Helper()
	args := []string{"--project", projectDir}
	if jsonOut {
		args = append(args, "--json")
	}
	stdout, _ = captureStd(t, func() { exitCode = runCommsWhoSubcommand(args) })
	return stdout, exitCode
}

func decodeWhoJSON(t *testing.T, out string) (entries []whoLine, rawKeys []map[string]any) {
	t.Helper()
	split := strings.Split(strings.TrimSpace(out), "\n")
	typed := make([]whoLine, 0, len(split))
	raw := make([]map[string]any, 0, len(split))
	for _, line := range split {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var one whoLine
		if err := json.Unmarshal([]byte(line), &one); err != nil {
			t.Fatalf("comms who --json emitted a line that is not JSON: %v — %q", err, line)
		}
		var keys map[string]any
		if err := json.Unmarshal([]byte(line), &keys); err != nil {
			t.Fatalf("comms who --json: decode keys of %q: %v", line, err)
		}
		typed = append(typed, one)
		raw = append(raw, keys)
	}
	return typed, raw
}

// TestCommsWhoJSON_FieldNamesAndStaleAnnotation pins the NDJSON contract the
// fleet health probe parses: keys agent / last_seen / status, a status of
// exactly "online" or "stale", lines sorted by agent name, and departed agents
// left out entirely.
func TestCommsWhoJSON_FieldNamesAndStaleAnnotation(t *testing.T) {
	now := time.Now()
	fresh := now.Add(-10 * time.Second).UTC().Format(time.RFC3339)
	aging := now.Add(-5 * time.Minute).UTC().Format(time.RFC3339)

	projDir := commsWriteEvents(t,
		commsPresenceLine(t, "01965b00-0000-7000-8000-000000000001", fresh, "zoe", "online", "join"),
		commsPresenceLine(t, "01965b00-0000-7000-8000-000000000002", fresh, "mallory", "online", "join"),
		commsPresenceLine(t, "01965b00-0000-7000-8000-000000000003", aging, "charlie", "online", "join"),
		commsPresenceLine(t, "01965b00-0000-7000-8000-000000000004", fresh, "bob", "online", "join"),
		commsPresenceLine(t, "01965b00-0000-7000-8000-000000000005", fresh, "alice", "online", "join"),
		commsPresenceLine(t, "01965b00-0000-7000-8000-000000000006", fresh, "yara", "online", "join"),
		commsPresenceLine(t, "01965b00-0000-7000-8000-000000000007", fresh, "dave", "online", "join"),
		commsPresenceLine(t, "01965b00-0000-7000-8000-000000000008", fresh, "dave", "offline", "leave"),
	)

	out, code := captureCommsWho(t, projDir, true)
	if code != 0 {
		t.Fatalf("comms who --json: exit = %d, want 0", code)
	}

	entries, rawKeys := decodeWhoJSON(t, out)

	wantOrder := []string{"alice", "bob", "charlie", "mallory", "yara", "zoe"}
	gotOrder := make([]string, 0, len(entries))
	for _, e := range entries {
		gotOrder = append(gotOrder, e.Agent)
	}
	if strings.Join(gotOrder, ",") != strings.Join(wantOrder, ",") {
		t.Errorf("comms who --json agents = %v, want %v sorted by name — an unsorted list makes every consumer diff noisily", gotOrder, wantOrder)
	}

	for i, keys := range rawKeys {
		wantKeys := map[string]bool{"agent": true, "last_seen": true, "status": true}
		for k := range keys {
			if !wantKeys[k] {
				t.Errorf("comms who --json line %d has unexpected key %q", i, k)
			}
			delete(wantKeys, k)
		}
		for k := range wantKeys {
			t.Errorf("comms who --json line %d is missing key %q — the fleet health probe binds to this exact spelling", i, k)
		}
	}

	byAgent := map[string]whoLine{}
	for _, e := range entries {
		byAgent[e.Agent] = e
	}
	if got := byAgent["alice"].Status; got != "online" {
		t.Errorf("comms who --json: alice status = %q, want %q", got, "online")
	}
	if got := byAgent["charlie"].Status; got != "stale" {
		t.Errorf("comms who --json: charlie beat 5m ago, status = %q, want %q", got, "stale")
	}
	if _, present := byAgent["dave"]; present {
		t.Errorf("comms who --json: dave sent a leave beat and must be omitted; got %v", gotOrder)
	}

	for _, e := range entries {
		if _, err := time.Parse(time.RFC3339, e.LastSeen); err != nil {
			t.Errorf("comms who --json: %s last_seen = %q is not RFC3339: %v", e.Agent, e.LastSeen, err)
		}
	}
}

// TestCommsWhoJSON_EmptyRegistryPrintsNothing verifies that an empty registry
// gives empty stdout and exit 0. A "no agents online" note on stdout would be
// parsed as a record and corrupt the probe; it belongs on stderr.
func TestCommsWhoJSON_EmptyRegistryPrintsNothing(t *testing.T) {
	projDir := commsWriteEvents(t)

	out, code := captureCommsWho(t, projDir, true)
	if code != 0 {
		t.Fatalf("comms who --json on an empty registry: exit = %d, want 0", code)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("comms who --json on an empty registry wrote %q to stdout; want nothing", out)
	}
}
