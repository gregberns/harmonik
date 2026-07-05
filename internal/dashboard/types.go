// Package dashboard manages the captain-curated planning store at
// .harmonik/context/dashboard.json (Tier-B, hk-d503m).
//
// dashboard.json is the narrative + expectations layer that live state cannot
// self-derive. It is a sibling to lanes.json and joins on the "lane" key.
// The captain/admiral owns writes; the daemon reads it for the dashboard op.
//
// Schema v1 fields:
//   - updated / updated_by: freshness stamp — the forcing-gate key (§4).
//   - priorities_current: ranked current work items, plain-English, lane-keyed.
//   - priorities_future: on-deck / next items, ranked.
//   - throughput_expected: the ONLY expected-vs-actual input a human sets, per lane.
//   - notes: free-text operator-facing status, ≤3 lines.
package dashboard

import (
	"path/filepath"
	"time"
)

// SchemaVersion is the current schema version for DashboardState.
const SchemaVersion = 1

// DashboardState is the captain-curated planning store persisted at
// .harmonik/context/dashboard.json.
type DashboardState struct {
	SchemaVersion      int                  `json:"schema_version"`
	Updated            time.Time            `json:"updated"`
	UpdatedBy          string               `json:"updated_by"`
	PrioritiesCurrent  []PriorityCurrent    `json:"priorities_current"`
	PrioritiesFuture   []PriorityFuture     `json:"priorities_future"`
	ThroughputExpected []ThroughputExpected `json:"throughput_expected"`
	Notes              string               `json:"notes"`
}

// PriorityCurrent is one ranked entry in priorities_current.
// Rank 1 = highest priority. lane is the join key to lanes.json.
type PriorityCurrent struct {
	Rank     int    `json:"rank"`
	Lane     string `json:"lane"`
	EpicID   string `json:"epic_id,omitempty"`
	Crew     string `json:"crew,omitempty"`
	Headline string `json:"headline"`
	Expected string `json:"expected,omitempty"`
}

// PriorityFuture is one on-deck entry in priorities_future.
// No rank field — order in the slice is the rank.
type PriorityFuture struct {
	Lane     string `json:"lane"`
	Headline string `json:"headline"`
	Gate     string `json:"gate,omitempty"`
}

// ThroughputExpected is the human-set expected-vs-actual input for one lane.
// The daemon pairs this against Tier-A actuals from session-data.jsonl.
type ThroughputExpected struct {
	Lane          string    `json:"lane"`
	BeadsExpected int       `json:"beads_expected"`
	By            time.Time `json:"by"`
}

// Path returns the canonical path to dashboard.json for the given project
// directory.
func Path(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "context", "dashboard.json")
}
