package run

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
)

func TestNewDispatchRecordBindsPreparedIntent(t *testing.T) {
	base := testDispatchRecord()
	binding := dispatch.Binding{
		QueueID:           base.QueueID,
		QueueName:         base.QueueName,
		GroupIndex:        base.GroupIndex,
		ItemIndex:         base.ItemIndex,
		BeadID:            base.BeadID,
		RunID:             base.RunID,
		ClaimTransitionID: base.ClaimTransitionID,
		ParentCommit:      base.ParentCommit,
		RepositoryPath:    base.RepositoryPath,
	}
	startedAt := base.StartedAt.Add(456 * time.Microsecond).In(time.FixedZone("offset", 3600))
	record, err := NewDispatchRecord(binding, startedAt)
	if err != nil {
		t.Fatalf("NewDispatchRecord: %v", err)
	}
	if record.SchemaVersion != dispatchRecordSchemaVersion ||
		record.RunID != binding.RunID ||
		record.BeadID != binding.BeadID ||
		record.QueueName != binding.QueueName ||
		record.QueueID != binding.QueueID ||
		record.GroupIndex != binding.GroupIndex ||
		record.ItemIndex != binding.ItemIndex ||
		record.ClaimTransitionID != binding.ClaimTransitionID {
		t.Fatalf("record identity = %+v", record)
	}
	wantTime := startedAt.UTC().Truncate(time.Millisecond)
	if !record.StartedAt.Equal(wantTime) || record.StartedAt.Location() != time.UTC {
		t.Fatalf("StartedAt = %v, want %v", record.StartedAt, wantTime)
	}
}

const (
	dispatchTestQueueID      = "0197d200-0000-7000-8000-000000000001"
	dispatchTestRunID        = "0197d200-0000-7000-8000-000000000002"
	dispatchTestTransitionID = "0197d200-0000-7000-8000-000000000003"
	dispatchTestParentCommit = "0123456789abcdef0123456789abcdef01234567"
)

func testDispatchRecord() DispatchRecord {
	return DispatchRecord{
		SchemaVersion:     4,
		RunID:             core.RunID(uuid.MustParse(dispatchTestRunID)),
		BeadID:            "hk-run-record",
		QueueName:         "main",
		QueueID:           dispatchTestQueueID,
		GroupIndex:        0,
		ItemIndex:         0,
		ClaimTransitionID: core.TransitionID(uuid.MustParse(dispatchTestTransitionID)),
		ParentCommit:      dispatchTestParentCommit,
		RepositoryPath:    "/srv/harmonik/project",
		StartedAt:         time.Date(2026, 8, 11, 12, 13, 14, 567000000, time.UTC),
	}
}

func testLocalLocation() ExecutionLocation {
	return ExecutionLocation{Kind: ExecutionLocalIndependent, RepositoryPath: "/srv/harmonik/project"}
}

func testRemoteLocation() ExecutionLocation {
	return ExecutionLocation{
		Kind: ExecutionRemote, WorkerName: "worker-a", Transport: "ssh",
		Host: "worker.example", RepositoryPath: "/srv/worker/project",
	}
}

func TestDispatchRecordValidLocalRemoteAndHandoffShapes(t *testing.T) {
	base := testDispatchRecord()
	localIndependent, err := base.BindLocation(testLocalLocation())
	if err != nil {
		t.Fatal(err)
	}
	localShared, err := base.BindLocation(ExecutionLocation{Kind: ExecutionLocalShared, RepositoryPath: base.RepositoryPath})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := base.BindLocation(testRemoteLocation())
	if err != nil {
		t.Fatal(err)
	}
	handoff := remote
	handoff.SessionName = "harmonik-run-0197d200"
	handoff.WindowName = "run-0197d200"
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
		{name: "window before location", mutate: func(r *DispatchRecord) { r.WindowName = "window" }},
		{name: "session without window", mutate: func(r *DispatchRecord) {
			r.Location = &ExecutionLocation{Kind: ExecutionLocalIndependent}
			r.SessionName = "session"
		}},
		{name: "window without session", mutate: func(r *DispatchRecord) {
			r.Location = &ExecutionLocation{Kind: ExecutionLocalIndependent}
			r.WindowName = "window"
		}},
		{name: "zero time", mutate: func(r *DispatchRecord) { r.StartedAt = time.Time{} }},
		{name: "offset time", mutate: func(r *DispatchRecord) { r.StartedAt = r.StartedAt.In(time.FixedZone("offset", 3600)) }},
		{name: "submillisecond time", mutate: func(r *DispatchRecord) { r.StartedAt = r.StartedAt.Add(time.Nanosecond) }},
		{name: "parent commit", mutate: func(r *DispatchRecord) { r.ParentCommit = strings.ToUpper(r.ParentCommit) }},
		{name: "missing repository", mutate: func(r *DispatchRecord) { r.RepositoryPath = "" }},
		{name: "relative repository", mutate: func(r *DispatchRecord) { r.RepositoryPath = "project" }},
		{name: "unclean repository", mutate: func(r *DispatchRecord) { r.RepositoryPath = "/srv/project/../other" }},
		{name: "local transport", mutate: func(r *DispatchRecord) {
			location := testLocalLocation()
			location.Transport = "ssh"
			r.Location = &location
		}},
		{name: "local host", mutate: func(r *DispatchRecord) {
			location := testLocalLocation()
			location.Host = "worker.example"
			r.Location = &location
		}},
		{name: "local other repository", mutate: func(r *DispatchRecord) {
			location := testLocalLocation()
			location.RepositoryPath = "/srv/harmonik/other"
			r.Location = &location
		}},
		{name: "remote missing name", mutate: func(r *DispatchRecord) {
			location := testRemoteLocation()
			location.WorkerName = ""
			r.Location = &location
		}},
		{name: "remote missing transport", mutate: func(r *DispatchRecord) {
			location := testRemoteLocation()
			location.Transport = ""
			r.Location = &location
		}},
		{name: "remote missing host", mutate: func(r *DispatchRecord) {
			location := testRemoteLocation()
			location.Host = ""
			r.Location = &location
		}},
		{name: "remote missing repository", mutate: func(r *DispatchRecord) {
			location := testRemoteLocation()
			location.RepositoryPath = ""
			r.Location = &location
		}},
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
		"pre-activation schema": strings.Replace(valid, `"schema_version":4`, `"schema_version":3`, 1),
		"unknown":               strings.Replace(valid, `"schema_version":4`, `"schema_version":4,"extra":true`, 1),
		"missing group":         strings.Replace(valid, `"group_index":0,`, "", 1),
		"missing item":          strings.Replace(valid, `"item_index":0,`, "", 1),
		"uppercase":             strings.Replace(valid, dispatchTestRunID, strings.ToUpper(dispatchTestRunID), 1),
		"two values":            valid + `{}`,
		"time offset":           strings.Replace(valid, `2026-08-11T12:13:14.567Z`, `2026-08-11T13:13:14.567+01:00`, 1),
	}
	for name, input := range bad {
		t.Run(name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(input), &got); err == nil {
				t.Fatal("Unmarshal() = nil")
			}
		})
	}
}

func TestDispatchRecordHandoffAddsExactTarget(t *testing.T) {
	base := testDispatchRecord()
	located, err := base.BindLocation(testLocalLocation())
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := located.BindSession("harmonik-run-0197d200", "run-0197d200")
	if err != nil {
		t.Fatal(err)
	}
	want := located
	want.SessionName = "harmonik-run-0197d200"
	want.WindowName = "run-0197d200"
	if !reflect.DeepEqual(handoff, want) {
		t.Fatalf("handoff = %+v, want %+v", handoff, want)
	}
	handoff.Location.Kind = ExecutionRemote
	if located.Location.Kind != ExecutionLocalIndependent {
		t.Fatal("BindSession candidate aliases the prior location")
	}
	handoff = want
	replayed, err := handoff.BindSession(want.SessionName, want.WindowName)
	if err != nil || !reflect.DeepEqual(replayed, want) {
		t.Fatalf("same-session replay = (%+v, %v)", replayed, err)
	}
	if _, err := handoff.BindSession("other", want.WindowName); err == nil {
		t.Fatal("changed session BindSession() = nil")
	}
	if _, err := handoff.BindSession(want.SessionName, "other"); err == nil {
		t.Fatal("changed window BindSession() = nil")
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"session_name":"harmonik-run-0197d200","window_name":"run-0197d200"`) {
		t.Fatalf("handoff bytes = %s", data)
	}
	withoutWindow := strings.Replace(string(data), `,"window_name":"run-0197d200"`, "", 1)
	var decoded DispatchRecord
	if err := json.Unmarshal([]byte(withoutWindow), &decoded); err == nil {
		t.Fatal("handoff without window_name decode = nil")
	}
}

func TestDispatchRecordLocationBindingIsMonotonic(t *testing.T) {
	base := testDispatchRecord()
	location := ExecutionLocation{Kind: ExecutionLocalShared, RepositoryPath: base.RepositoryPath}
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
	if _, err := bound.BindLocation(testLocalLocation()); err == nil {
		t.Fatal("changed location BindLocation() = nil")
	}
}

func TestDispatchRecordBindLocationRejectsDifferentLocalRepository(t *testing.T) {
	base := testDispatchRecord()
	location := testLocalLocation()
	location.RepositoryPath = "/srv/harmonik/other"
	if _, err := base.BindLocation(location); err == nil {
		t.Fatal("BindLocation() with a different local repository = nil")
	}
}
