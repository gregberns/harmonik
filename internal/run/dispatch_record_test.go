package run

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

const (
	dispatchTestQueueID      = "0197d200-0000-7000-8000-000000000001"
	dispatchTestRunID        = "0197d200-0000-7000-8000-000000000002"
	dispatchTestTransitionID = "0197d200-0000-7000-8000-000000000003"
)

func testDispatchRecord() DispatchRecord {
	return DispatchRecord{
		SchemaVersion:     2,
		RunID:             core.RunID(uuid.MustParse(dispatchTestRunID)),
		BeadID:            "hk-run-record",
		QueueName:         "main",
		QueueID:           dispatchTestQueueID,
		GroupIndex:        0,
		ItemIndex:         0,
		ClaimTransitionID: core.TransitionID(uuid.MustParse(dispatchTestTransitionID)),
		StartedAt:         time.Date(2026, 8, 11, 12, 13, 14, 567000000, time.UTC),
	}
}

func TestDispatchRecordValidLocalRemoteAndHandoffShapes(t *testing.T) {
	base := testDispatchRecord()
	localIndependent, err := base.BindLocation(ExecutionLocation{Kind: ExecutionLocalIndependent})
	if err != nil {
		t.Fatal(err)
	}
	localShared, err := base.BindLocation(ExecutionLocation{Kind: ExecutionLocalShared})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := base.BindLocation(ExecutionLocation{Kind: ExecutionRemote, WorkerName: "worker-a"})
	if err != nil {
		t.Fatal(err)
	}
	handoff := remote
	handoff.SessionName = "harmonik-run-0197d200"
	for _, record := range []DispatchRecord{base, localIndependent, localShared, remote, handoff} {
		if err := record.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDispatchRecordRejectsInvalidShapes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*DispatchRecord)
	}{
		{name: "schema", mutate: func(r *DispatchRecord) { r.SchemaVersion = 1 }},
		{name: "run", mutate: func(r *DispatchRecord) { r.RunID = core.RunID{} }},
		{name: "bead", mutate: func(r *DispatchRecord) { r.BeadID = "" }},
		{name: "queue name", mutate: func(r *DispatchRecord) { r.QueueName = "Bad Name" }},
		{name: "queue ID", mutate: func(r *DispatchRecord) { r.QueueID = "bad" }},
		{name: "group", mutate: func(r *DispatchRecord) { r.GroupIndex = -1 }},
		{name: "item", mutate: func(r *DispatchRecord) { r.ItemIndex = -1 }},
		{name: "claim", mutate: func(r *DispatchRecord) { r.ClaimTransitionID = core.TransitionID{} }},
		{name: "location", mutate: func(r *DispatchRecord) { r.Location = &ExecutionLocation{Kind: "other"} }},
		{name: "local worker", mutate: func(r *DispatchRecord) {
			r.Location = &ExecutionLocation{Kind: ExecutionLocalIndependent, WorkerName: "worker"}
		}},
		{name: "remote worker", mutate: func(r *DispatchRecord) { r.Location = &ExecutionLocation{Kind: ExecutionRemote} }},
		{name: "session before location", mutate: func(r *DispatchRecord) { r.SessionName = "session" }},
		{name: "zero time", mutate: func(r *DispatchRecord) { r.StartedAt = time.Time{} }},
		{name: "offset time", mutate: func(r *DispatchRecord) { r.StartedAt = r.StartedAt.In(time.FixedZone("offset", 3600)) }},
		{name: "submillisecond time", mutate: func(r *DispatchRecord) { r.StartedAt = r.StartedAt.Add(time.Nanosecond) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			record := testDispatchRecord()
			tc.mutate(&record)
			if err := record.Validate(); err == nil {
				t.Fatal("Validate() = nil")
			}
		})
	}
}

func TestDispatchRecordStrictCanonicalJSON(t *testing.T) {
	want := testDispatchRecord()
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got DispatchRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	valid := string(data)
	bad := map[string]string{
		"unknown":       strings.Replace(valid, `"schema_version":2`, `"schema_version":2,"extra":true`, 1),
		"missing group": strings.Replace(valid, `"group_index":0,`, "", 1),
		"missing item":  strings.Replace(valid, `"item_index":0,`, "", 1),
		"uppercase":     strings.Replace(valid, dispatchTestRunID, strings.ToUpper(dispatchTestRunID), 1),
		"two values":    valid + `{}`,
		"time offset":   strings.Replace(valid, `2026-08-11T12:13:14.567Z`, `2026-08-11T13:13:14.567+01:00`, 1),
	}
	for name, input := range bad {
		t.Run(name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(input), &got); err == nil {
				t.Fatal("Unmarshal() = nil")
			}
		})
	}
}

func TestDispatchRecordHandoffAddsOnlySessionName(t *testing.T) {
	base := testDispatchRecord()
	located, err := base.BindLocation(ExecutionLocation{Kind: ExecutionLocalIndependent})
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := located.BindSession("harmonik-run-0197d200")
	if err != nil {
		t.Fatal(err)
	}
	want := located
	want.SessionName = "harmonik-run-0197d200"
	if !reflect.DeepEqual(handoff, want) {
		t.Fatalf("handoff = %+v, want %+v", handoff, want)
	}
	handoff.Location.Kind = ExecutionRemote
	if located.Location.Kind != ExecutionLocalIndependent {
		t.Fatal("BindSession candidate aliases the prior location")
	}
	handoff = want
	replayed, err := handoff.BindSession(want.SessionName)
	if err != nil || !reflect.DeepEqual(replayed, want) {
		t.Fatalf("same-session replay = (%+v, %v)", replayed, err)
	}
	if _, err := handoff.BindSession("other"); err == nil {
		t.Fatal("changed session BindSession() = nil")
	}
}

func TestDispatchRecordLocationBindingIsMonotonic(t *testing.T) {
	base := testDispatchRecord()
	location := ExecutionLocation{Kind: ExecutionLocalShared}
	bound, err := base.BindLocation(location)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Location == nil || *bound.Location != location {
		t.Fatalf("BindLocation() = %+v", bound.Location)
	}
	replayed, err := bound.BindLocation(location)
	if err != nil || replayed.Location == nil || *replayed.Location != location {
		t.Fatalf("same-location replay = (%+v, %v)", replayed, err)
	}
	replayed.Location.Kind = ExecutionRemote
	if bound.Location.Kind != ExecutionLocalShared {
		t.Fatal("BindLocation replay aliases the prior location")
	}
	if _, err := bound.BindLocation(ExecutionLocation{Kind: ExecutionLocalIndependent}); err == nil {
		t.Fatal("changed location BindLocation() = nil")
	}
}
