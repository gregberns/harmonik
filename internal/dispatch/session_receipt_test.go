package dispatch

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestSessionStartReceiptStrictRoundTrip(t *testing.T) {
	want, err := NewSessionStartReceipt(testIntent(PhaseHandoffDurable))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON := `{"schema_version":3,"binding":{"queue_id":"` + testQueueID +
		`","queue_name":"main","group_index":2,"item_index":3,"bead_id":"hk-dispatch","run_id":"` + testRunID +
		`","claim_transition_id":"` + testTransitionID + `","parent_commit":"` + testParentCommit + `","repository_path":"/srv/harmonik/project` +
		`"},"session_name":"harmonik-run-0197d100","window_name":"run-0197d100"}`
	if string(data) != wantJSON {
		t.Fatalf("receipt bytes = %s, want %s", data, wantJSON)
	}
	var got SessionStartReceipt
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}

	valid := string(data)
	for name, raw := range map[string]string{
		"pre-activation schema": strings.Replace(valid, `"schema_version":3`, `"schema_version":2`, 1),
		"unknown field":         strings.Replace(valid, `"schema_version":3`, `"schema_version":3,"extra":true`, 1),
		"missing window":        strings.Replace(valid, `,"window_name":"run-0197d100"`, "", 1),
		"uppercase run":         strings.Replace(valid, testRunID, strings.ToUpper(testRunID), 1),
		"trailing value":        valid + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(raw), &got); err == nil {
				t.Fatal("Unmarshal() = nil")
			}
		})
	}
}

func TestNewSessionStartReceiptRequiresExactHandoff(t *testing.T) {
	for _, phase := range []Phase{PhasePrepared, PhaseClaimDurable, PhaseClaimRefused, PhaseRunDurable} {
		if _, err := NewSessionStartReceipt(testIntent(phase)); err == nil {
			t.Fatalf("NewSessionStartReceipt(%q) = nil error", phase)
		}
	}
	want := testIntent(PhaseHandoffDurable)
	receipt, err := NewSessionStartReceipt(want)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Binding != want.Binding || receipt.SessionName != want.Handoff.SessionName ||
		receipt.WindowName != want.Handoff.WindowName {
		t.Fatalf("receipt = %+v, intent = %+v", receipt, want)
	}
	badTarget := want
	badTarget.Handoff = &HandoffBinding{
		SessionName:        "bad\nsession",
		WindowName:         want.Handoff.WindowName,
		WorktreeLeaseRunID: want.Handoff.WorktreeLeaseRunID,
	}
	if _, err := NewSessionStartReceipt(badTarget); err == nil {
		t.Fatal("NewSessionStartReceipt() accepted invalid target name")
	}
}

func TestSessionStartReceiptRejectsInvalidValues(t *testing.T) {
	base, err := NewSessionStartReceipt(testIntent(PhaseHandoffDurable))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*SessionStartReceipt)
	}{
		{name: "schema", mutate: func(r *SessionStartReceipt) { r.SchemaVersion++ }},
		{name: "binding", mutate: func(r *SessionStartReceipt) { r.Binding.BeadID = "" }},
		{name: "session", mutate: func(r *SessionStartReceipt) { r.SessionName = "" }},
		{name: "window", mutate: func(r *SessionStartReceipt) { r.WindowName = "bad\nwindow" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := base
			tc.mutate(&value)
			if err := value.Validate(); err == nil {
				t.Fatal("Validate() = nil")
			}
			if _, err := json.Marshal(value); err == nil {
				t.Fatal("Marshal() = nil error")
			}
		})
	}
}
