package daemon

// dashboardtypes.go — types for the DashboardSnapshot (hk-2exz9).
//
// DashboardSnapshot is a read-time projection: it joins the live StateSnapshot
// with captain-curated planning files (dashboard.json, lanes.json), windowed
// session-data.jsonl throughput, open decisions, and active stall signals.
// No new persisted store — the durable substrate is the files that already exist.
//
// Spec ref: plans/2026-07-03-operator-dashboard/DESIGN.md §2.
// Bead ref: hk-2exz9.

// DashboardSnapshot is the top-level output of `harmonik dashboard [--json]`.
type DashboardSnapshot struct {
	SchemaVersion int              `json:"schema_version"` // always 1
	CapturedAt    string           `json:"captured_at"`    // RFC-3339
	State         StateSnapshot    `json:"state"`
	Config        *DashboardConfig `json:"config,omitempty"`
	Lanes         []DashLane       `json:"lanes"`
	OpenDecisions []DashDecision   `json:"open_decisions"`
	ActiveStalls  []DashStall      `json:"active_stalls"`
	Throughput    *DashThroughput  `json:"throughput,omitempty"`
}

// DashboardConfig is the captain-curated planning layer from .harmonik/context/dashboard.json.
type DashboardConfig struct {
	SchemaVersion      int                 `json:"schema_version"`
	Updated            string              `json:"updated,omitempty"`
	UpdatedBy          string              `json:"updated_by,omitempty"`
	PrioritiesCurrent  []DashPriority      `json:"priorities_current,omitempty"`
	PrioritiesFuture   []DashPriority      `json:"priorities_future,omitempty"`
	ThroughputExpected []DashThroughputExp `json:"throughput_expected,omitempty"`
	Notes              string              `json:"notes,omitempty"`
}

// DashPriority is one entry in priorities_current or priorities_future.
type DashPriority struct {
	Rank     int    `json:"rank,omitempty"`
	Lane     string `json:"lane"`
	EpicID   string `json:"epic_id,omitempty"`
	Crew     string `json:"crew,omitempty"`
	Headline string `json:"headline"`
	Expected string `json:"expected,omitempty"`
	Gate     string `json:"gate,omitempty"`
}

// DashThroughputExp is an expected-throughput entry set by the operator.
type DashThroughputExp struct {
	Lane          string `json:"lane"`
	BeadsExpected int    `json:"beads_expected"`
	By            string `json:"by,omitempty"`
}

// DashLane is one lane from .harmonik/context/lanes.json.
type DashLane struct {
	Lane     string        `json:"lane"`
	Label    string        `json:"label,omitempty"`
	EpicID   string        `json:"epic_id,omitempty"`
	Crew     string        `json:"crew,omitempty"`
	Queue    string        `json:"queue,omitempty"`
	Status   string        `json:"status"`
	Gate     *DashLaneGate `json:"gate,omitempty"`
	Note     string        `json:"note,omitempty"`
	PlanPath string        `json:"plan_path,omitempty"`
}

// DashLaneGate is the gate object in a DashLane.
type DashLaneGate struct {
	Reason  string `json:"reason,omitempty"`
	Owner   string `json:"owner,omitempty"`
	Expires string `json:"expires,omitempty"`
}

// DashDecision is one open decision from the hitl-decisions projection.
type DashDecision struct {
	DecisionID     string   `json:"decision_id"`
	Question       string   `json:"question"`
	Options        []string `json:"options"`
	BlockedAgent   string   `json:"blocked_agent,omitempty"`
	ContextLink    string   `json:"context_link,omitempty"`
	ValueRequested bool     `json:"value_requested,omitempty"`
}

// DashStall is one active stall_detected event for a currently-running run.
type DashStall struct {
	RunID     string `json:"run_id"`
	BeadID    string `json:"bead_id"`
	Signature string `json:"signature"`
	ElapsedMs int64  `json:"elapsed_ms"`
}

// DashThroughput is a windowed aggregate from session-data.jsonl.
// Available=false when the file does not exist (WS1 task not yet landed).
type DashThroughput struct {
	Available bool                 `json:"available"`
	WindowH   int                  `json:"window_h,omitempty"`
	ByLane    []DashLaneThroughput `json:"by_lane,omitempty"`
}

// DashLaneThroughput is throughput stats for one lane over the window.
type DashLaneThroughput struct {
	Lane         string `json:"lane"`
	BeadsClosed  int    `json:"beads_closed"`
	MeanWallSecs int64  `json:"mean_wall_secs,omitempty"`
}

// lanesFile is the on-disk shape of .harmonik/context/lanes.json.
type lanesFile struct {
	SchemaVersion int        `json:"schema_version"`
	Updated       string     `json:"updated,omitempty"`
	Lanes         []DashLane `json:"lanes"`
}
