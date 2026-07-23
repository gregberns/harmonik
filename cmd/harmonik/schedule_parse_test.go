package main

// schedule_parse_test.go — table-driven coverage for the `harmonik schedule`
// argument parsers and renderers.
//
// parseScheduleSpec is the validation boundary for `schedule add --schedule`:
// its whole reason to call NextFire at parse time is so a bad spec fails when
// the operator types it rather than silently never firing. Nothing covered
// that, so a spec form that quietly stopped validating would look identical to
// one that worked until the job failed to run.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/schedule"
)

func TestParseScheduleSpec_Accepted(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want schedule.Schedule
	}{
		{
			name: "daily with implicit local timezone",
			spec: "daily@09:30",
			want: schedule.Schedule{Kind: schedule.ScheduleKindDaily, At: "09:30", TZ: schedule.TZLocal},
		},
		{
			name: "daily with an explicit IANA zone",
			spec: "daily@09:30 America/New_York",
			want: schedule.Schedule{Kind: schedule.ScheduleKindDaily, At: "09:30", TZ: "America/New_York"},
		},
		{
			name: "daily at midnight",
			spec: "daily@00:00",
			want: schedule.Schedule{Kind: schedule.ScheduleKindDaily, At: "00:00", TZ: schedule.TZLocal},
		},
		{
			name: "surrounding whitespace is trimmed",
			spec: "   daily@23:59   ",
			want: schedule.Schedule{Kind: schedule.ScheduleKindDaily, At: "23:59", TZ: schedule.TZLocal},
		},
		{
			name: "interval in minutes",
			spec: "every@5m",
			want: schedule.Schedule{Kind: schedule.ScheduleKindEvery, Interval: "5m"},
		},
		{
			name: "interval in seconds",
			spec: "every@30s",
			want: schedule.Schedule{Kind: schedule.ScheduleKindEvery, Interval: "30s"},
		},
		{
			name: "compound interval",
			spec: "every@1h30m",
			want: schedule.Schedule{Kind: schedule.ScheduleKindEvery, Interval: "1h30m"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseScheduleSpec(tc.spec)
			if err != nil {
				t.Fatalf("parseScheduleSpec(%q) returned error: %v", tc.spec, err)
			}
			if got != tc.want {
				t.Errorf("parseScheduleSpec(%q) = %+v, want %+v", tc.spec, got, tc.want)
			}
		})
	}
}

func TestParseScheduleSpec_Rejected(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		wantSub string // substring the error must name, so the operator can act on it
	}{
		{"empty", "", "empty --schedule"},
		{"whitespace only", "   ", "empty --schedule"},
		{"missing the @ separator", "daily 09:30", "invalid --schedule"},
		{"bare kind with no value", "daily", "invalid --schedule"},
		{"unknown kind", "weekly@monday", "unsupported schedule kind"},
		{"unknown kind names the supported ones", "hourly@1", "daily"},
		{"daily with a malformed time", "daily@25:00", ""},
		{"daily with a non-time value", "daily@soon", ""},
		{"daily with an unknown timezone", "daily@09:30 Mars/Olympus_Mons", ""},
		{"every with a non-duration", "every@soon", ""},
		{"every with an empty duration", "every@", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseScheduleSpec(tc.spec)
			if err == nil {
				t.Fatalf("parseScheduleSpec(%q) = %+v, want an error", tc.spec, got)
			}
			if got != (schedule.Schedule{}) {
				t.Errorf("parseScheduleSpec(%q) returned %+v alongside its error; want the zero value", tc.spec, got)
			}
			if tc.wantSub != "" && !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q must name %q", err.Error(), tc.wantSub)
			}
		})
	}
}

// TestParseScheduleSpec_ExtraFieldsAfterTZ pins that only the first two fields
// are consumed: a trailing token must not silently become part of the zone.
func TestParseScheduleSpec_ExtraFieldsAfterTZ(t *testing.T) {
	got, err := parseScheduleSpec("daily@09:30 UTC and-then-some")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TZ != "UTC" {
		t.Errorf("TZ = %q, want %q (trailing tokens must be ignored, not concatenated)", got.TZ, "UTC")
	}
}

func TestScheduleActionSummary(t *testing.T) {
	tests := []struct {
		name   string
		action schedule.Action
		want   string
	}{
		{
			name:   "command with arguments",
			action: schedule.Action{Kind: schedule.ActionKindCommand, Argv: []string{"harmonik", "digest", "--full"}},
			want:   "command: harmonik digest --full",
		},
		{
			name:   "command with no arguments",
			action: schedule.Action{Kind: schedule.ActionKindCommand},
			want:   "command: ",
		},
		{
			name:   "spawn-crew",
			action: schedule.Action{Kind: schedule.ActionKindSpawnCrew, Crew: "paul", Queue: "quality"},
			want:   "spawn-crew: crew=paul queue=quality",
		},
		{
			name:   "spawn-crew ignores the mission field in the summary",
			action: schedule.Action{Kind: schedule.ActionKindSpawnCrew, Crew: "paul", Queue: "q", Mission: "m.md"},
			want:   "spawn-crew: crew=paul queue=q",
		},
		{
			name:   "an unrecognised kind renders as itself",
			action: schedule.Action{Kind: "comms-send", To: "operator"},
			want:   "comms-send",
		},
		{
			name:   "the zero action renders empty rather than panicking",
			action: schedule.Action{},
			want:   "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := scheduleActionSummary(tc.action); got != tc.want {
				t.Errorf("scheduleActionSummary = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestScheduleSingleIDArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantID      string
		wantProject string
		wantCode    int
	}{
		{"id only", []string{"job-1"}, "job-1", "", 0},
		{"id then --project VALUE", []string{"job-1", "--project", "/p"}, "job-1", "/p", 0},
		{"--project VALUE then id", []string{"--project", "/p", "job-1"}, "job-1", "/p", 0},
		{"--project=VALUE form", []string{"job-1", "--project=/p"}, "job-1", "/p", 0},
		{"missing id", []string{}, "", "", 1},
		{"only a flag, no id", []string{"--project", "/p"}, "", "", 1},
		{"unknown flag", []string{"job-1", "--bogus"}, "", "", 1},
		{"two positional ids", []string{"job-1", "job-2"}, "", "", 1},
		{"--project with no value is treated as an unknown flag", []string{"job-1", "--project"}, "", "", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, project, code := scheduleSingleIDArgs("remove", tc.args)
			if code != tc.wantCode {
				t.Fatalf("code = %d, want %d (id=%q project=%q)", code, tc.wantCode, id, project)
			}
			if id != tc.wantID {
				t.Errorf("id = %q, want %q", id, tc.wantID)
			}
			if project != tc.wantProject {
				t.Errorf("project = %q, want %q", project, tc.wantProject)
			}
		})
	}
}
