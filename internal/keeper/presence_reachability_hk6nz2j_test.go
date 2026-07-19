package keeper

// presence_reachability_hk6nz2j_test.go — T1 (hk-keeper-delivery-agent-input-6nz2j)
// AIS-020 acceptance: the keeper is a NEW in-process consumer of the presence-Online
// reachability read. This test lives in package `keeper` and imports
// `internal/presence` directly — so it fails the build (depguard lint / compile) if
// that import edge is ever removed from .golangci.yml's keeper allow-list. It reads
// an Online state with age < presence.TTL (120s) via the exact surface the keeper's
// delivery decision (T7) will use: presence.ComputeRegistry(eventsPath) →
// presence.GetState(record), mirroring watcher.go's blockedOnOpenDecision.
//
// This is substrate-only (reachability), NOT the delivery decision itself (T7). No
// threshold change, no bus redesign — a new caller on an existing surface.
//
// Spec ref: agent-input.md §4.10 AIS-020 (presence-Online reachability, necessary-
// but-not-sufficient; draft 05-spec-drafts/agent-input-amendment.md).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/presence"
)

// keeperPresenceBeatLine builds a raw agent_presence JSONL event line — the same
// envelope shape presence.ComputeRegistry folds (see the presence TTL matrix tests).
func keeperPresenceBeatLine(eventID, agent string, ts time.Time) string {
	payload, _ := json.Marshal(map[string]any{ //nolint:errcheck,errchkjson // marshal of a fixed test map cannot fail
		"agent":     agent,
		"status":    "online",
		"last_seen": ts.UTC().Format(time.RFC3339),
		"reason":    "join",
	})
	ev, _ := json.Marshal(map[string]any{ //nolint:errcheck,errchkjson // marshal of a fixed test map cannot fail
		"event_id":         eventID,
		"schema_version":   1,
		"type":             "agent_presence",
		"timestamp_wall":   ts.UTC(),
		"source_subsystem": "test",
		"payload":          json.RawMessage(payload),
	})
	return string(ev)
}

func keeperWriteEvents(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	f, err := os.Create(path) //nolint:gosec // G304: path from t.TempDir(), not user input
	if err != nil {
		t.Fatalf("keeperWriteEvents: create: %v", err)
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // best-effort close of test fixture file
	for _, l := range lines {
		if _, wErr := fmt.Fprintln(f, l); wErr != nil {
			t.Fatalf("keeperWriteEvents: write: %v", wErr)
		}
	}
	return path
}

// TestKeeperReadsPresenceOnlineReachability_hk6nz2j asserts the keeper package can
// read an Online reachability state (age < TTL) for a target agent, and that the
// TTL boundary is honored (age ≥ TTL is no longer Online).
func TestKeeperReadsPresenceOnlineReachability_hk6nz2j(t *testing.T) {
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	path := keeperWriteEvents(t,
		keeperPresenceBeatLine("01986000-0000-7000-8000-000000000001", "target-agent", base),
	)

	reg := presence.ComputeRegistry(path)
	rec, known := reg["target-agent"]
	if !known {
		t.Fatalf("target-agent not present in the keeper-read presence registry")
	}

	// Age < TTL (30s < 120s): the reachability read the keeper relies on = Online.
	// GetStateAt is the clock-injection seam so the assertion is deterministic.
	if got := presence.GetStateAt(rec, base.Add(30*time.Second)); got != presence.StateOnline {
		t.Errorf("age 30s (< TTL %s): GetStateAt = %v, want StateOnline", presence.TTL, got)
	}

	// Age ≥ TTL (130s ≥ 120s): no longer Online — the necessary-but-not-sufficient
	// boundary (AIS-020) the keeper delivery decision must respect.
	if got := presence.GetStateAt(rec, base.Add(130*time.Second)); got == presence.StateOnline {
		t.Errorf("age 130s (≥ TTL %s): GetStateAt = StateOnline, want a non-Online state", presence.TTL)
	}
}
