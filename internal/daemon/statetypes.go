package daemon

// statetypes.go — normative Go types for the harmonik system-state snapshot.
//
// All types here are NORMATIVE per specs/system-state.md §4.1 (hk-gv04 /
// codename:fleet-state). The JSON tags are stable; no field may be removed or
// renamed once emitted in a released binary.
//
// Bead ref: hk-gv04 (P2-a: harmonik state aggregator command).
// Spec ref: specs/system-state.md §4 (SS-001..SS-001b).

import "time"

// ActivityLabel is the fleet-level activity roll-up.  Four values, priority
// order: PROCESSING > DRAINING > WAITING > INACTIVE.  Computed by RollUpLabel.
// MUST NOT be used as a directive (SS-INV-001).
type ActivityLabel string

const (
	ActivityProcessing ActivityLabel = "PROCESSING"
	ActivityWaiting    ActivityLabel = "WAITING"
	ActivityDraining   ActivityLabel = "DRAINING"
	ActivityInactive   ActivityLabel = "INACTIVE"
)

// StateSnapshot is the top-level output of `harmonik state [--json]`.
// Spec: SS-001.
type StateSnapshot struct {
	SchemaVersion int           `json:"schema_version"` // always 1
	CapturedAt    string        `json:"captured_at"`    // RFC-3339
	Daemon        StateDaemon   `json:"daemon"`
	ActivityLabel ActivityLabel `json:"activity_label"`
	Runs          []StateRun    `json:"runs"`
	Queues        []StateQueue  `json:"queues"`
	Sessions      []StateSession `json:"sessions"`
	WorkAxes      *FleetFacts   `json:"work_axes"`
	ReadQuality   ReadQuality   `json:"read_quality"`
}

// StateDaemon is the daemon presence block. Spec: SS-001b.
type StateDaemon struct {
	Up     bool   `json:"up"`
	Pid    int    `json:"pid,omitempty"`    // present when Up==true
	Socket string `json:"socket,omitempty"` // present when Up==true
}

// ReadQuality carries read-quality caveats for the whole snapshot.
// Mirrors FleetFacts.Unsure at the snapshot level (SS-010).
type ReadQuality struct {
	Ok      bool     `json:"ok"`
	Unsure  bool     `json:"unsure"`
	Reasons []string `json:"reasons,omitempty"`
}

// StateRun is one in-flight run projected from a *RunHandle.  Spec: SS-001.
type StateRun struct {
	RunID          string `json:"run_id"`
	BeadID         string `json:"bead_id"`
	QueueName      string `json:"queue_name,omitempty"`
	WorktreePath   string `json:"worktree_path,omitempty"`
	StartedAt      string `json:"started_at"` // RFC-3339
	OwningEpicID   string `json:"owning_epic_id,omitempty"`
	OwningAssignee string `json:"owning_epic_assignee,omitempty"`
	// LifecycleState is the per-RUN FSM state (see handlercontract/lifecycle).
	// Not the same as the activity label — a run stays registered through the
	// whole merge/build/push/cleanup tail (PROCESSING by registry membership).
	LifecycleState string `json:"lifecycle_state"`
	Source         string `json:"source"` // "live" | "disk"
}

// StateQueue is one entry in queues[].  Spec: SS-001b / SS-001a.
type StateQueue struct {
	Name               string `json:"name"`
	Status             string `json:"status"`              // QueueStatus string
	Source             string `json:"source"`              // "live" | "disk"
	ItemCount          int    `json:"item_count"`
	ActiveCount        int    `json:"active_count"`
	EffectiveWorkerCap int    `json:"effective_worker_cap"`
	EligibleNow        bool   `json:"eligible_now"`
	PauseReason        string `json:"pause_reason,omitempty"`
}

// StateSession is one entry in sessions[].  Spec: SS-001b.
type StateSession struct {
	Agent          string           `json:"agent"`
	SessionType    string           `json:"session_type"`    // "captain" | "crew"
	Alive          bool             `json:"alive"`
	SleepMarker    bool             `json:"sleep_marker"`
	AtRest         bool             `json:"at_rest"`
	PresenceSource string           `json:"presence_source"` // "registry" | "tmux" | "both"
	Cognition      *SessionCognition `json:"cognition"`       // null when Alive==false
}

// SessionCognition is the typed shape of sessions[i].cognition.  Spec: SS-011.
type SessionCognition struct {
	Agent             string           `json:"agent"`
	SessionID         string           `json:"session_id"`           // live (.sid)
	SessionIDDeclared string           `json:"session_id_declared"`  // from crew registry
	SIDDesync         bool             `json:"sid_desync"`
	Context           SessionContext   `json:"context"`
	Signals           CognitionSignals `json:"signals"`
	Subagents         interface{}      `json:"subagents"` // null in v1.0 (SS-014slot)
}

// SessionContext carries raw token count and fill fraction for one session.
// Spec: SS-011.
type SessionContext struct {
	Tokens     int64   `json:"tokens"`
	WindowSize int64   `json:"window_size"` // 0 ⇒ FallbackWindowSize was used for fill_frac
	FillFrac   float64 `json:"fill_frac"`   // tokens / effective_window
	// Source: "gauge" | "heartbeat_derive" | "capture_pane" | "absent"
	// A consumer MUST treat "absent" as UNKNOWN, not as zero.
	Source     string `json:"source"`
	GaugeTS    string `json:"gauge_ts,omitempty"` // RFC-3339; set when source=="gauge"
	ReadTS     string `json:"read_ts"`            // RFC-3339
	AgeSeconds int    `json:"age_seconds"`        // ReadTS − GaugeTS in seconds
}

// CognitionSignals bundles the three observable signals per SS-012..SS-013.
type CognitionSignals struct {
	TooBig        TooBigSignal        `json:"too_big"`
	ContextStatic ContextStaticSignal `json:"context_static"`
	LoopDetected  *LoopDetectedSignal `json:"loop_detected"` // null in v1.0 (SS-013 DEFERRED)
}

// TooBigSignal is the "context over band" signal. Spec: SS-012.
type TooBigSignal struct {
	Tripped      bool   `json:"tripped"`
	Band         string `json:"band,omitempty"` // "warn"|"act"|"force_act"|"hard_ceiling"
	ThresholdRef string `json:"threshold_ref"`
	Threshold    *int64 `json:"threshold"` // null if config knob unset
	Value        int64  `json:"value"`
}

// ContextStaticSignal is the "token-not-changing" raw-facts signal. Spec: SS-012.
// Reports FACTS about gauge readings; MUST NOT be read as a "stuck" verdict.
type ContextStaticSignal struct {
	GaugeAgeSeconds          int    `json:"gauge_age_seconds"`
	StalenessRef             string `json:"staleness_ref"`
	StalenessS               *int   `json:"staleness_s"`               // null if config knob unset
	TokensUnchangedIntervals int    `json:"tokens_unchanged_intervals"` // 0 without gauge history
	StuckMinIntervalsRef     string `json:"stuck_min_intervals_ref"`
	StuckMinIntervals        *int   `json:"stuck_min_intervals"`        // null if config knob unset
	Flat                     *bool  `json:"flat"`                       // null when StuckMinIntervals unset
}

// LoopDetectedSignal is the repeating-pattern signal.  Always null in v1.0
// (producer DEFERRED per SS-013).  Shape reserved for a later slice.
type LoopDetectedSignal struct {
	Tripped    bool    `json:"tripped"`
	Source     string  `json:"source"`     // always "haiku" when populated
	CheckedTS  string  `json:"checked_ts"` // RFC-3339
	Confidence float64 `json:"confidence"`
	Note       string  `json:"note"`
}

// onDiskSleepMarkerPrefix is the prefix for sleep marker files under .harmonik/.
const onDiskSleepMarkerPrefix = ".sleeping."

// formatRFC3339 formats t as RFC-3339, or "" for zero.
func formatRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}
