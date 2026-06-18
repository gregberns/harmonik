package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// dailyInterval is the fixed period of a "daily" schedule kind. It is also the
// default CatchupWindow for daily jobs (D2).
const dailyInterval = 24 * time.Hour

// parseInterval parses Schedule.Interval for a ScheduleKindEvery schedule.
func parseInterval(s Schedule) (time.Duration, error) {
	if s.Interval == "" {
		return 0, fmt.Errorf("schedule: every kind requires a non-empty interval")
	}
	d, err := time.ParseDuration(s.Interval)
	if err != nil {
		return 0, fmt.Errorf("schedule: invalid interval %q: %w", s.Interval, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("schedule: interval must be positive, got %q", s.Interval)
	}
	return d, nil
}

// ResolveLocation resolves a Schedule.TZ value to a *time.Location.
//
// "local" (TZLocal) → time.Local. Any other value is loaded as an IANA zone
// name via time.LoadLocation; an unknown name returns an error.
func ResolveLocation(tz string) (*time.Location, error) {
	if tz == "" || tz == TZLocal {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("schedule: resolve timezone %q: %w", tz, err)
	}
	return loc, nil
}

// parseHHMM parses an "HH:MM" 24-hour wall-clock string into hour and minute.
func parseHHMM(at string) (hour, minute int, err error) {
	parts := strings.Split(at, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("schedule: invalid time %q: want HH:MM", at)
	}
	hour, err = strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("schedule: invalid hour in %q: want 00..23", at)
	}
	minute, err = strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("schedule: invalid minute in %q: want 00..59", at)
	}
	return hour, minute, nil
}

// NextFire returns the next fire instant at or after ref (exclusive of ref
// itself — a fire exactly at ref rolls to the next day) for the given daily
// schedule.
//
// ref is a PARAMETER, never time.Now() — so the computation is deterministic
// and testable. The returned instant is in UTC (callers compare it against
// other UTC instants and persist it as an RFC3339 UTC string).
//
// Algorithm (D1, daily kind):
//  1. Resolve the schedule's timezone.
//  2. Form "ref's local date at HH:MM" in that timezone.
//  3. If that instant is <= ref, add 24h (roll to tomorrow).
//
// DST note: forming the instant via time.Date in the schedule's location lets
// Go's standard library handle wall-clock arithmetic across DST transitions —
// the wall-clock HH:MM is honoured on each calendar day. Adding a 24h
// time.Duration when rolling forward is intentional and matches the daily-cadence
// contract; on a DST boundary the next fire's wall-clock can shift by an hour,
// which is the expected behaviour for a fixed-period daily timer.
func NextFire(s Schedule, ref time.Time) (time.Time, error) {
	if s.Kind == ScheduleKindEvery {
		// For display purposes: next fire is ref + interval (conservative estimate
		// assuming just fired). For accurate next-fire accounting for LastFire, use
		// JobNextFire.
		d, err := parseInterval(s)
		if err != nil {
			return time.Time{}, fmt.Errorf("schedule: NextFire: %w", err)
		}
		return ref.Add(d).UTC(), nil
	}
	if s.Kind != ScheduleKindDaily {
		return time.Time{}, fmt.Errorf("schedule: NextFire: unsupported kind %q (supported: %q, %q)", s.Kind, ScheduleKindDaily, ScheduleKindEvery)
	}
	loc, err := ResolveLocation(s.TZ)
	if err != nil {
		return time.Time{}, err
	}
	hour, minute, err := parseHHMM(s.At)
	if err != nil {
		return time.Time{}, err
	}

	// Work in the schedule's local timezone so HH:MM is wall-clock on each day.
	refLocal := ref.In(loc)
	candidate := time.Date(refLocal.Year(), refLocal.Month(), refLocal.Day(), hour, minute, 0, 0, loc)

	// If today's fire instant has already passed (or is exactly now), roll to
	// tomorrow. Compare in UTC to avoid wall-clock ambiguity.
	if !candidate.After(ref) {
		candidate = time.Date(refLocal.Year(), refLocal.Month(), refLocal.Day()+1, hour, minute, 0, 0, loc)
	}
	return candidate.UTC(), nil
}

// PrevFire returns the most-recent fire instant at or before ref for the given
// daily schedule. It is the dual of NextFire and underpins catch-up detection
// (D2): a job missed a fire iff PrevFire(ref) is strictly after LastFire.
//
// A fire exactly at ref counts as the previous fire (inclusive of ref).
func PrevFire(s Schedule, ref time.Time) (time.Time, error) {
	if s.Kind == ScheduleKindEvery {
		// For display purposes: previous fire is ref - interval.
		d, err := parseInterval(s)
		if err != nil {
			return time.Time{}, fmt.Errorf("schedule: PrevFire: %w", err)
		}
		return ref.Add(-d).UTC(), nil
	}
	if s.Kind != ScheduleKindDaily {
		return time.Time{}, fmt.Errorf("schedule: PrevFire: unsupported kind %q (supported: %q, %q)", s.Kind, ScheduleKindDaily, ScheduleKindEvery)
	}
	loc, err := ResolveLocation(s.TZ)
	if err != nil {
		return time.Time{}, err
	}
	hour, minute, err := parseHHMM(s.At)
	if err != nil {
		return time.Time{}, err
	}

	refLocal := ref.In(loc)
	candidate := time.Date(refLocal.Year(), refLocal.Month(), refLocal.Day(), hour, minute, 0, 0, loc)

	// If today's fire instant is still in the future, the previous fire was
	// yesterday's.
	if candidate.After(ref) {
		candidate = time.Date(refLocal.Year(), refLocal.Month(), refLocal.Day()-1, hour, minute, 0, 0, loc)
	}
	return candidate.UTC(), nil
}

// catchupWindow returns the effective catch-up window for a job: the parsed
// CatchupWindow duration when set, else the schedule's interval (24h for daily).
func catchupWindow(j ScheduledJob) (time.Duration, error) {
	if j.CatchupWindow == "" {
		return dailyInterval, nil
	}
	d, err := time.ParseDuration(j.CatchupWindow)
	if err != nil {
		return 0, fmt.Errorf("schedule: job %q: invalid catchup_window %q: %w", j.ID, j.CatchupWindow, err)
	}
	return d, nil
}

// JobNextFire returns the accurate next-fire instant for job j at ref, using
// j.LastFire when available. This is the display companion to Decide: it gives
// the "when will this job fire next" answer a human or `schedule list` can show.
//
//   - daily kind: delegates to NextFire(j.Schedule, ref).
//   - every kind: lastFire + interval if j has fired before; ref (immediate) if not.
func JobNextFire(j ScheduledJob, ref time.Time) (time.Time, error) {
	if j.Schedule.Kind == ScheduleKindEvery {
		d, err := parseInterval(j.Schedule)
		if err != nil {
			return time.Time{}, fmt.Errorf("schedule: JobNextFire: %w", err)
		}
		lastFire, hadLast, err := parseLastFire(j)
		if err != nil {
			return time.Time{}, err
		}
		if !hadLast {
			return ref.UTC(), nil // never fired → fires immediately on next tick
		}
		return lastFire.Add(d).UTC(), nil
	}
	return NextFire(j.Schedule, ref)
}

// decideInterval computes the fire decision for a ScheduleKindEvery job.
// Interval jobs fire as soon as the interval has elapsed since LastFire. There
// is no "missed boundary" concept: if the daemon was down for multiple intervals,
// exactly one fire is triggered on restart and the timer resets from that point.
func decideInterval(j ScheduledJob, nowUTC time.Time) (FireDecision, error) {
	d, err := parseInterval(j.Schedule)
	if err != nil {
		return FireDecision{}, fmt.Errorf("schedule: job %q: %w", j.ID, err)
	}
	lastFire, hadLast, err := parseLastFire(j)
	if err != nil {
		return FireDecision{}, err
	}
	if !hadLast {
		return FireDecision{Fire: true, FireInstant: nowUTC}, nil
	}
	if nowUTC.Before(lastFire.Add(d)) {
		return FireDecision{Fire: false}, nil
	}
	return FireDecision{Fire: true, FireInstant: nowUTC}, nil
}

// FireDecision is the pure verdict for whether a job is due at a reference time.
type FireDecision struct {
	// Fire is true when the job should fire at nowUTC.
	Fire bool
	// Catchup is true when the fire is a coalesced catch-up of a missed instant
	// (as opposed to a normal on-time fire). Advisory: the action is identical;
	// callers may log the distinction.
	Catchup bool
	// MissedSkipped is true when a missed fire fell OUTSIDE the catch-up window
	// (or catchup is "off") and was deliberately skipped. Fire is false in this
	// case; callers should advance LastFire to the last missed instant and log.
	MissedSkipped bool
	// FireInstant is the fire instant being honoured (UTC). Set when Fire is true
	// or when MissedSkipped is true (the skipped missed instant).
	FireInstant time.Time
}

// parseLastFire parses a job's LastFire RFC3339 UTC string. Empty → zero time
// (job has never fired).
func parseLastFire(j ScheduledJob) (time.Time, bool, error) {
	if j.LastFire == "" {
		return time.Time{}, false, nil
	}
	t, err := time.Parse(time.RFC3339, j.LastFire)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("schedule: job %q: invalid last_fire %q: %w", j.ID, j.LastFire, err)
	}
	return t.UTC(), true, nil
}

// Decide computes whether job j is due at nowUTC. It is a pure function of the
// job row and the reference time — it never reads the wall clock and never
// mutates j (callers persist LastFire after acting on the decision).
//
// Semantics (D2 catch-up):
//   - Never fired (LastFire empty): fire iff nowUTC has reached or passed the
//     most-recent scheduled instant at/before now (PrevFire). This makes a
//     freshly-added job fire on its first due boundary without firing instantly
//     at add-time when the next boundary is still in the future.
//   - Previously fired: the most-recent scheduled instant at/before now is
//     PrevFire(now). If it is at/before LastFire, nothing is due. If it is after
//     LastFire, a fire is due:
//   - catchup "off": fire it as a normal forward fire (no missed-window
//     bookkeeping; we simply resume from now's boundary).
//   - catchup "coalesce-within-window": fire ONE coalesced catch-up iff the
//     missed instant is within CatchupWindow of now; otherwise skip it
//     (MissedSkipped) and let the caller advance LastFire past it.
//
// Returns an error only for malformed job fields (bad At/TZ/last_fire/window).
func Decide(j ScheduledJob, nowUTC time.Time) (FireDecision, error) {
	nowUTC = nowUTC.UTC()

	if j.Schedule.Kind == ScheduleKindEvery {
		return decideInterval(j, nowUTC)
	}

	prev, err := PrevFire(j.Schedule, nowUTC)
	if err != nil {
		return FireDecision{}, err
	}

	lastFire, hadLast, err := parseLastFire(j)
	if err != nil {
		return FireDecision{}, err
	}

	// Nothing is due until now has reached the most-recent scheduled instant.
	// (prev is always <= now by construction, so "due" means prev is newer than
	// the last fire we already serviced.)
	if hadLast && !prev.After(lastFire) {
		return FireDecision{Fire: false}, nil
	}
	if !hadLast {
		// Never fired: the first due boundary is prev (the most-recent scheduled
		// instant at/before now). Fire on it. There is no "missed window" concept
		// for a job that has never run — it simply starts firing at its first
		// boundary.
		return FireDecision{Fire: true, FireInstant: prev}, nil
	}

	// A scheduled instant (prev) is newer than LastFire → a fire is due.
	if j.Catchup == CatchupOff {
		// Resume normal forward scheduling: fire now's boundary, no catch-up
		// bookkeeping.
		return FireDecision{Fire: true, FireInstant: prev}, nil
	}

	// Coalesce-within-window: fire one catch-up iff the missed instant is recent
	// enough; otherwise skip it.
	window, err := catchupWindow(j)
	if err != nil {
		return FireDecision{}, err
	}
	if nowUTC.Sub(prev) <= window {
		return FireDecision{Fire: true, Catchup: true, FireInstant: prev}, nil
	}
	return FireDecision{Fire: false, MissedSkipped: true, FireInstant: prev}, nil
}
