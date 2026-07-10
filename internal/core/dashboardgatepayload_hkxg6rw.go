package core

// dashboardgatepayload_hkxg6rw.go — payload types for the dashboard_stale /
// dashboard_refreshed event types (plans/2026-07-03-operator-dashboard/DESIGN.md
// §4, hk-xg6rw).
//
// Refs: hk-xg6rw.

// DashboardStalePayload is the event-bus payload for the dashboard_stale event
// type. Emitted when .harmonik/context/dashboard.json's `updated` timestamp is
// older than the configured dashboard.max_staleness. While active, the daemon
// staffs no new work on the listed captain-curated queues.
type DashboardStalePayload struct {
	// MaxStalenessSecs is the configured dashboard.max_staleness, in seconds.
	// Always positive.
	MaxStalenessSecs int64 `json:"max_staleness_secs"`

	// StaleSecs is how far past the staleness window dashboard.json's updated
	// timestamp is, in seconds, at detection time. Non-negative.
	StaleSecs int64 `json:"stale_secs"`

	// UpdatedAt is the RFC 3339 `updated` timestamp read from dashboard.json.
	// Empty when dashboard.json has never been written (treated as maximally stale).
	UpdatedAt string `json:"updated_at,omitempty"`

	// BlockedQueues lists the captain-curated queue names gated by this trip.
	BlockedQueues []string `json:"blocked_queues,omitempty"`

	// DetectedAt is the RFC 3339 timestamp of the gate evaluation.
	// Required (non-empty).
	DetectedAt string `json:"detected_at"`
}

// Valid reports whether p is a well-formed DashboardStalePayload.
func (p DashboardStalePayload) Valid() bool {
	return p.MaxStalenessSecs > 0 && p.DetectedAt != ""
}

// DashboardRefreshedPayload is the event-bus payload for the
// dashboard_refreshed event type. Emitted on the transition out of the
// dashboard_stale gate.
type DashboardRefreshedPayload struct {
	// Reason is one of "refreshed" (captain updated dashboard.json within the
	// window) or "unlocked" (operator applied the kill-switch/--unlock
	// override). Required (non-empty).
	Reason string `json:"reason"`

	// UpdatedAt is the RFC 3339 `updated` timestamp read from dashboard.json
	// at the time of the transition. Empty when Reason is "unlocked" and
	// dashboard.json is still stale/absent.
	UpdatedAt string `json:"updated_at,omitempty"`

	// DetectedAt is the RFC 3339 timestamp of the gate evaluation.
	// Required (non-empty).
	DetectedAt string `json:"detected_at"`
}

// Valid reports whether p is a well-formed DashboardRefreshedPayload.
func (p DashboardRefreshedPayload) Valid() bool {
	return p.Reason != "" && p.DetectedAt != ""
}
