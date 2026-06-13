// Package schedule is the generic recurring-job primitive for the harmonik
// daemon. A scheduled job pairs a Schedule (when to fire) with an Action (what
// to do). The daemon's work loop fires due jobs in-loop; spawn-crew actions
// reuse the existing crew-start path so subscription-billing guards apply by
// construction.
//
// This package is deliberately GENERIC: it contains no project-codename literals
// (no "liet"/"logmine"). It holds pure types, the pure next-fire computation
// (clock.go), and the durable single-writer store (store.go). Action EXECUTION
// (exec.Command for command actions, HandleCrewStart for spawn-crew actions) and
// the work-loop tick live in internal/daemon, which imports this package.
//
// Time discipline: next-fire and last-fire instants are stored and compared as
// RFC3339 UTC strings (matching core.SnapshotToken.CapturedAtTimestamp and
// crew.Record.StartedAt conventions). No time.Time field is persisted; the
// store round-trips plain strings to avoid silent timezone normalisation. Local
// time enters only at the fire-boundary computation (NextFire) and CLI display.
//
// Scope (operator-locked, codename:schedule, hk-0es):
//   - D1: daily@HH:MM + tz only. The Schedule.Kind discriminator is retained so
//     "cron"/"interval" can be added later without breaking stored rows.
//   - D2: catch-up coalesces ONE missed fire within a 24h window by default;
//     catchup:"off" disables it.
package schedule

// Schedule.Kind values. v1 supports "daily" only; "cron"/"interval" are reserved
// so stored rows remain forward-compatible when those kinds are added.
const (
	// ScheduleKindDaily fires once per day at Schedule.At in Schedule.TZ.
	ScheduleKindDaily = "daily"
	// ScheduleKindCron is reserved for a future cron-expression schedule kind.
	ScheduleKindCron = "cron"
	// ScheduleKindInterval is reserved for a future fixed-interval schedule kind.
	ScheduleKindInterval = "interval"
)

// Action.Kind values.
const (
	// ActionKindCommand runs Action.Argv as a fresh detached process.
	ActionKindCommand = "command"
	// ActionKindSpawnCrew starts a crew via the daemon's crew-start path.
	ActionKindSpawnCrew = "spawn-crew"
)

// OverlapPolicy values. The default is "skip".
const (
	// OverlapPolicySkip skips a fire when a prior instance is still running
	// (command: LastPID alive; spawn-crew: crew presence-online). Default.
	OverlapPolicySkip = "skip"
	// OverlapPolicyAllow fires regardless of any in-flight prior instance.
	OverlapPolicyAllow = "allow"
)

// Catchup values. The default is "coalesce-within-window".
const (
	// CatchupCoalesceWithinWindow fires at most ONE coalesced catch-up for a
	// missed fire whose most-recent missed instant is within CatchupWindow.
	// Default.
	CatchupCoalesceWithinWindow = "coalesce-within-window"
	// CatchupOff disables catch-up entirely; the job resumes normal forward
	// scheduling without firing any missed instants.
	CatchupOff = "off"
)

// TZLocal is the Schedule.TZ sentinel that selects the daemon host's local
// timezone (time.Local). Any other value is resolved via time.LoadLocation as
// an IANA zone name.
const TZLocal = "local"

// Schedule describes WHEN a job fires.
//
// v1: Kind is always "daily"; At is "HH:MM" 24h wall-clock; TZ is "local" or an
// IANA zone name. The Kind discriminator is the forward-compatibility seam for
// cron/interval kinds (D1).
type Schedule struct {
	// Kind is the schedule discriminator. v1: "daily" only.
	Kind string `json:"kind"`
	// At is the daily fire time as "HH:MM" 24-hour wall-clock (e.g. "09:30").
	At string `json:"at"`
	// TZ is "local" (host time.Local) or an IANA zone name (e.g. "America/New_York").
	TZ string `json:"tz"`
}

// Action describes WHAT a job does when it fires.
//
// Kind="command" uses Argv (Argv[0] is the binary; Argv[1:] its arguments).
// Kind="spawn-crew" uses Crew/Queue/Mission to drive the daemon's crew-start
// path (the same entry point `harmonik crew start` uses).
type Action struct {
	// Kind is "command" or "spawn-crew".
	Kind string `json:"kind"`
	// Argv is the command and its arguments for Kind="command".
	Argv []string `json:"argv,omitempty"`
	// Crew is the crew-member name for Kind="spawn-crew".
	Crew string `json:"crew,omitempty"`
	// Queue is the named queue the crew binds to for Kind="spawn-crew".
	Queue string `json:"queue,omitempty"`
	// Mission is the handoff/mission path the crew seeds from for Kind="spawn-crew".
	Mission string `json:"mission,omitempty"`
}

// ScheduledJob is one durable recurring-job row.
//
// LastFire and the computed next-fire are RFC3339 UTC strings (see package doc).
// LastPID is recorded for command-action overlap checks (skip iff still alive).
type ScheduledJob struct {
	// ID is the operator-chosen unique job identifier (store key).
	ID string `json:"id"`
	// Schedule is when the job fires.
	Schedule Schedule `json:"schedule"`
	// Action is what the job does.
	Action Action `json:"action"`
	// Enabled gates dispatch: a disabled job is never fired by the work loop.
	Enabled bool `json:"enabled"`
	// OverlapPolicy is "skip" (default) or "allow".
	OverlapPolicy string `json:"overlap_policy"`
	// Catchup is "coalesce-within-window" (default) or "off".
	Catchup string `json:"catchup"`
	// CatchupWindow is a Go duration string bounding catch-up eligibility.
	// Empty → the schedule interval (24h for daily).
	CatchupWindow string `json:"catchup_window,omitempty"`
	// LastFire is the RFC3339 UTC instant the job last fired (empty if never).
	LastFire string `json:"last_fire,omitempty"`
	// LastPID is the PID of the most recent command-action process (0 if none /
	// not a command action). Used by the "skip" overlap policy.
	LastPID int `json:"last_pid,omitempty"`
	// ForceNext requests an immediate one-shot fire on the next work-loop tick
	// regardless of the schedule, still honouring the overlap policy. It is the
	// daemon-agnostic mechanism behind `schedule run-now`: the CLI sets it on disk
	// and the running daemon (which reloads the file on mtime change) consumes and
	// clears it on the next tick. When the daemon is down it stays set and fires on
	// next boot — matching the "fire now / ad-hoc" intent.
	ForceNext bool `json:"force_next,omitempty"`
}

// NormaliseDefaults fills zero-value policy fields with their documented
// defaults. It is idempotent and safe to call on every load/add so stored rows
// and freshly-parsed CLI rows behave identically.
func (j *ScheduledJob) NormaliseDefaults() {
	if j.OverlapPolicy == "" {
		j.OverlapPolicy = OverlapPolicySkip
	}
	if j.Catchup == "" {
		j.Catchup = CatchupCoalesceWithinWindow
	}
}
