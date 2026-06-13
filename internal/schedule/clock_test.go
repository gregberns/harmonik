package schedule

import (
	"testing"
	"time"
)

// mustLoad loads an IANA location or fails the test.
func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

func TestNextFire(t *testing.T) {
	ny := mustLoad(t, "America/New_York")

	tests := []struct {
		name string
		sch  Schedule
		ref  time.Time
		want time.Time
	}{
		{
			name: "later today (UTC)",
			sch:  Schedule{Kind: ScheduleKindDaily, At: "12:00", TZ: "UTC"},
			ref:  time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC),
			want: time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC),
		},
		{
			name: "past-time-today rolls to tomorrow (UTC)",
			sch:  Schedule{Kind: ScheduleKindDaily, At: "08:00", TZ: "UTC"},
			ref:  time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC),
			want: time.Date(2026, 6, 13, 8, 0, 0, 0, time.UTC),
		},
		{
			name: "exactly now rolls to tomorrow (exclusive of ref)",
			sch:  Schedule{Kind: ScheduleKindDaily, At: "09:00", TZ: "UTC"},
			ref:  time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC),
			want: time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC),
		},
		{
			name: "midnight rollover",
			sch:  Schedule{Kind: ScheduleKindDaily, At: "00:00", TZ: "UTC"},
			ref:  time.Date(2026, 6, 12, 23, 59, 0, 0, time.UTC),
			want: time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "explicit IANA zone: 09:30 New York in winter (EST = UTC-5)",
			sch:  Schedule{Kind: ScheduleKindDaily, At: "09:30", TZ: "America/New_York"},
			ref:  time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			// 09:30 EST on 2026-01-15 == 14:30 UTC.
			want: time.Date(2026, 1, 15, 14, 30, 0, 0, time.UTC),
		},
		{
			name: "explicit IANA zone: 09:30 New York in summer (EDT = UTC-4)",
			sch:  Schedule{Kind: ScheduleKindDaily, At: "09:30", TZ: "America/New_York"},
			ref:  time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
			// 09:30 EDT on 2026-07-15 == 13:30 UTC.
			want: time.Date(2026, 7, 15, 13, 30, 0, 0, time.UTC),
		},
		{
			name: "DST-transition day: fire at 09:00 NY on US spring-forward (2026-03-08)",
			// On 2026-03-08 the US springs forward at 02:00 → 03:00 EST→EDT.
			// 09:00 wall-clock is unambiguous (after the gap) and maps to 13:00 UTC (EDT).
			sch:  Schedule{Kind: ScheduleKindDaily, At: "09:00", TZ: "America/New_York"},
			ref:  time.Date(2026, 3, 8, 0, 0, 0, 0, ny), // local midnight on the DST day
			want: time.Date(2026, 3, 8, 13, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NextFire(tc.sch, tc.ref)
			if err != nil {
				t.Fatalf("NextFire: unexpected error: %v", err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("NextFire = %s, want %s", got.Format(time.RFC3339), tc.want.Format(time.RFC3339))
			}
		})
	}
}

func TestNextFire_LocalTZ(t *testing.T) {
	// tz="local" must resolve to time.Local. We assert the returned instant equals
	// the wall-clock HH:MM formed in time.Local, independent of the host's offset.
	ref := time.Date(2026, 6, 12, 0, 0, 0, 0, time.Local)
	got, err := NextFire(Schedule{Kind: ScheduleKindDaily, At: "10:15", TZ: "local"}, ref)
	if err != nil {
		t.Fatalf("NextFire: %v", err)
	}
	want := time.Date(2026, 6, 12, 10, 15, 0, 0, time.Local).UTC()
	if !got.Equal(want) {
		t.Fatalf("NextFire(local) = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestNextFire_Errors(t *testing.T) {
	tests := []struct {
		name string
		sch  Schedule
	}{
		{"unsupported kind", Schedule{Kind: "cron", At: "09:00", TZ: "UTC"}},
		{"bad hour", Schedule{Kind: ScheduleKindDaily, At: "25:00", TZ: "UTC"}},
		{"bad minute", Schedule{Kind: ScheduleKindDaily, At: "09:99", TZ: "UTC"}},
		{"missing colon", Schedule{Kind: ScheduleKindDaily, At: "0900", TZ: "UTC"}},
		{"bad tz", Schedule{Kind: ScheduleKindDaily, At: "09:00", TZ: "Not/AZone"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NextFire(tc.sch, time.Now()); err == nil {
				t.Fatalf("NextFire(%+v): expected error, got nil", tc.sch)
			}
		})
	}
}

func TestPrevFire(t *testing.T) {
	tests := []struct {
		name string
		sch  Schedule
		ref  time.Time
		want time.Time
	}{
		{
			name: "earlier today → today's fire",
			sch:  Schedule{Kind: ScheduleKindDaily, At: "08:00", TZ: "UTC"},
			ref:  time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC),
			want: time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC),
		},
		{
			name: "before today's fire → yesterday's fire",
			sch:  Schedule{Kind: ScheduleKindDaily, At: "12:00", TZ: "UTC"},
			ref:  time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC),
			want: time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC),
		},
		{
			name: "exactly now → counts as previous (inclusive)",
			sch:  Schedule{Kind: ScheduleKindDaily, At: "09:00", TZ: "UTC"},
			ref:  time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC),
			want: time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PrevFire(tc.sch, tc.ref)
			if err != nil {
				t.Fatalf("PrevFire: %v", err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("PrevFire = %s, want %s", got.Format(time.RFC3339), tc.want.Format(time.RFC3339))
			}
		})
	}
}

func TestDecide(t *testing.T) {
	utc := func(y int, mo time.Month, d, h, mi int) time.Time {
		return time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
	}
	daily := func(at string) Schedule { return Schedule{Kind: ScheduleKindDaily, At: at, TZ: "UTC"} }

	tests := []struct {
		name           string
		job            ScheduledJob
		now            time.Time
		wantFire       bool
		wantCatchup    bool
		wantMissedSkip bool
	}{
		{
			name:     "never fired, now past today's instant → fire",
			job:      ScheduledJob{ID: "a", Schedule: daily("08:00"), Catchup: CatchupCoalesceWithinWindow},
			now:      utc(2026, 6, 12, 9, 0),
			wantFire: true,
		},
		{
			name:     "never fired, today's instant still in future → no fire yet (waits for boundary)",
			job:      ScheduledJob{ID: "a", Schedule: daily("12:00"), Catchup: CatchupCoalesceWithinWindow, LastFire: utc(2026, 6, 11, 12, 0).Format(time.RFC3339)},
			now:      utc(2026, 6, 12, 9, 0),
			wantFire: false,
		},
		{
			name:     "fired today already → nothing due",
			job:      ScheduledJob{ID: "a", Schedule: daily("08:00"), Catchup: CatchupCoalesceWithinWindow, LastFire: utc(2026, 6, 12, 8, 0).Format(time.RFC3339)},
			now:      utc(2026, 6, 12, 9, 0),
			wantFire: false,
		},
		{
			name:        "missed within window → coalesced catch-up fire",
			job:         ScheduledJob{ID: "a", Schedule: daily("08:00"), Catchup: CatchupCoalesceWithinWindow, LastFire: utc(2026, 6, 11, 8, 0).Format(time.RFC3339)},
			now:         utc(2026, 6, 12, 9, 0), // prev fire = 06-12 08:00, 1h ago, within 24h
			wantFire:    true,
			wantCatchup: true,
		},
		{
			name:           "missed outside window → skipped",
			job:            ScheduledJob{ID: "a", Schedule: daily("08:00"), Catchup: CatchupCoalesceWithinWindow, CatchupWindow: "30m", LastFire: utc(2026, 6, 10, 8, 0).Format(time.RFC3339)},
			now:            utc(2026, 6, 12, 9, 0), // prev fire = 06-12 08:00, 1h ago, > 30m window
			wantFire:       false,
			wantMissedSkip: true,
		},
		{
			name:     "catchup off, missed → normal forward fire (no catch-up flag)",
			job:      ScheduledJob{ID: "a", Schedule: daily("08:00"), Catchup: CatchupOff, LastFire: utc(2026, 6, 10, 8, 0).Format(time.RFC3339)},
			now:      utc(2026, 6, 12, 9, 0),
			wantFire: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.job.NormaliseDefaults()
			got, err := Decide(tc.job, tc.now)
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if got.Fire != tc.wantFire {
				t.Errorf("Fire = %v, want %v", got.Fire, tc.wantFire)
			}
			if got.Catchup != tc.wantCatchup {
				t.Errorf("Catchup = %v, want %v", got.Catchup, tc.wantCatchup)
			}
			if got.MissedSkipped != tc.wantMissedSkip {
				t.Errorf("MissedSkipped = %v, want %v", got.MissedSkipped, tc.wantMissedSkip)
			}
		})
	}
}

// TestNextFire_DSTSpringForward pins behaviour on a daily schedule whose wall
// time falls in the "missing" hour of a DST spring-forward day. On 2026-03-08 in
// America/New_York clocks jump 02:00 EST → 03:00 EDT, so a local 02:30 does not
// exist. The contract under test: NextFire (a) does NOT error, (b) returns the
// SAME instant Go's stdlib normalisation produces for that nonexistent wall time
// (NextFire must not invent its own drift), and (c) stays on the same NY calendar
// day — it does NOT skip a full day.
//
// Direction note: Go's time.Date resolves the nonexistent 02:30 with the
// pre-transition offset (EST), yielding the instant whose local label is 01:30
// EST == 06:30 UTC. The test asserts against that computed normalisation rather
// than a hardcoded wall-clock, so it stays correct regardless of which way the
// stdlib normalises.
func TestNextFire_DSTSpringForward(t *testing.T) {
	ny := mustLoad(t, "America/New_York")
	sch := Schedule{Kind: ScheduleKindDaily, At: "02:30", TZ: "America/New_York"}

	// ref: just after midnight local on the spring-forward day, before the missing
	// hour, so "today at 02:30 NY" is the next fire.
	ref := time.Date(2026, 3, 8, 1, 0, 0, 0, ny)
	got, err := NextFire(sch, ref)
	if err != nil {
		t.Fatalf("NextFire on spring-forward day: unexpected error: %v", err)
	}

	// The reference normalised instant: time.Date of the nonexistent 02:30 in NY,
	// computed the same way NextFire does. NextFire must match it exactly.
	wantInstant := time.Date(2026, 3, 8, 2, 30, 0, 0, ny).UTC()
	if !got.Equal(wantInstant) {
		t.Fatalf("NextFire = %s, want stdlib-normalised %s",
			got.Format(time.RFC3339), wantInstant.Format(time.RFC3339))
	}

	// Sanity: strictly after ref and within the same NY calendar day (no full-day
	// drift to 03-09).
	if !got.After(ref) {
		t.Fatalf("NextFire %s is not after ref %s", got.Format(time.RFC3339), ref.Format(time.RFC3339))
	}
	gotNY := got.In(ny)
	if gotNY.Year() != 2026 || gotNY.Month() != time.March || gotNY.Day() != 8 {
		t.Fatalf("NextFire drifted off the spring-forward day: got %s (NY)", gotNY.Format(time.RFC3339))
	}
}
