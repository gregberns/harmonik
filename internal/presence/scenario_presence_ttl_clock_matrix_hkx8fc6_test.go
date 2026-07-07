package presence_test

// scenario_presence_ttl_clock_matrix_hkx8fc6_test.go — L0 presence TTL +
// EffectiveLastSeen clock matrix (design doc scenario 4, B2/G2, bead hk-x8fc6).
//
// Seam: presence.ComputeRegistry (JSONL fixture) + GetStateAt (injected now).
// No daemon, no socket, no concurrency — pure projection + deterministic clock.
//
// Matrix (t0 = fixed anchor; now injected via GetStateAt):
//   B1: join beat only,             now=t0+119s → Online  (119s < TTL=120s)
//   B2: join beat only,             now=t0+121s → Stale   (121s > TTL < StaleCutoff=10m)
//   G2: join at t0, send at t0+90s, now=t0+121s → Online
//       EffectiveLastSeen=max(t0,t0+90s)=t0+90s; age@t0+121s=31s < TTL
//
// The G2 sub-case also asserts EffectiveLastSeen is exactly max(beat, send).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/presence"
)

// t0 is the fixed anchor for all clock arithmetic in this matrix.
var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// matrixJoinLine builds a raw JSONL event line for an agent_presence join beat
// at the given ts.
func matrixJoinLine(eventID string, ts time.Time, agent string) string {
	tsStr := ts.UTC().Format(time.RFC3339)
	payload, _ := json.Marshal(map[string]any{
		"agent":     agent,
		"status":    "online",
		"last_seen": tsStr,
		"reason":    "join",
	})
	ev, _ := json.Marshal(map[string]any{
		"event_id":         eventID,
		"schema_version":   1,
		"type":             "agent_presence",
		"timestamp_wall":   ts.UTC(),
		"source_subsystem": "test",
		"payload":          json.RawMessage(payload),
	})
	return string(ev)
}

// matrixSendLine builds a raw JSONL event line for an agent_message send at ts.
// The envelope timestamp_wall is ts — ComputeRegistry uses this for lastActivity.
func matrixSendLine(eventID string, ts time.Time, from, to string) string {
	payload, _ := json.Marshal(map[string]any{
		"from": from,
		"to":   to,
		"body": "ping",
	})
	ev, _ := json.Marshal(map[string]any{
		"event_id":         eventID,
		"schema_version":   1,
		"type":             "agent_message",
		"timestamp_wall":   ts.UTC(),
		"source_subsystem": "test",
		"payload":          json.RawMessage(payload),
	})
	return string(ev)
}

// writeMatrixFixture writes lines to a temp file and returns its path.
func writeMatrixFixture(t *testing.T, lines []string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "events-*.jsonl")
	if err != nil {
		t.Fatalf("writeMatrixFixture: %v", err)
	}
	defer func() { _ = f.Close() }()
	for _, l := range lines {
		if _, writeErr := fmt.Fprintln(f, l); writeErr != nil {
			t.Fatalf("writeMatrixFixture: write: %v", writeErr)
		}
	}
	return f.Name()
}

// buildSingleAgentRegistry returns the presence.Record for agent from the fixture.
func buildSingleAgentRegistry(t *testing.T, path, agent string) presence.Record {
	t.Helper()
	reg := presence.ComputeRegistry(path)
	rec, ok := reg[agent]
	if !ok {
		t.Fatalf("agent %q not found in presence registry", agent)
	}
	return rec
}

// ---------------------------------------------------------------------------
// B1 — join beat only, t0+119s → Online
// ---------------------------------------------------------------------------

// TestPresenceTTL_JoinOnlyOnlineBeforeTTL verifies that a join beat at t0,
// observed at t0+119s, computes to StateOnline (119s < TTL=120s).
func TestPresenceTTL_JoinOnlyOnlineBeforeTTL(t *testing.T) {
	path := writeMatrixFixture(t, []string{
		matrixJoinLine("01965b00-0000-7000-8000-000000000001", t0, "alice"),
	})
	rec := buildSingleAgentRegistry(t, path, "alice")

	now := t0.Add(119 * time.Second)
	got := presence.GetStateAt(rec, now)
	if got != presence.StateOnline {
		t.Errorf("B1: GetStateAt at t0+119s = %v, want StateOnline", got)
	}
}

// ---------------------------------------------------------------------------
// B2 — join beat only, t0+121s → Stale
// ---------------------------------------------------------------------------

// TestPresenceTTL_JoinOnlyStaleAfterTTL verifies that a join beat at t0 with
// no further activity, observed at t0+121s, computes to StateStale
// (121s > TTL=120s; still within StaleCutoff=10m).
func TestPresenceTTL_JoinOnlyStaleAfterTTL(t *testing.T) {
	path := writeMatrixFixture(t, []string{
		matrixJoinLine("01965b00-0000-7000-8000-000000000001", t0, "alice"),
	})
	rec := buildSingleAgentRegistry(t, path, "alice")

	now := t0.Add(121 * time.Second)
	got := presence.GetStateAt(rec, now)
	if got != presence.StateStale {
		t.Errorf("B2: GetStateAt at t0+121s = %v, want StateStale", got)
	}
}

// ---------------------------------------------------------------------------
// G2 — join at t0, send at t0+90s; EffectiveLastSeen=max(beat,send), t0+121s → Online
// ---------------------------------------------------------------------------

// TestPresenceTTL_SendExtendsEffectiveLastSeen verifies the EffectiveLastSeen
// = max(beat, send) invariant (hk-6vwi3 fix #1): a join beat at t0 plus a
// send at t0+90s produces EffectiveLastSeen = t0+90s, so the agent is still
// Online at t0+121s (age = 31s < TTL=120s).
func TestPresenceTTL_SendExtendsEffectiveLastSeen(t *testing.T) {
	sendTS := t0.Add(90 * time.Second)
	path := writeMatrixFixture(t, []string{
		matrixJoinLine("01965b00-0000-7000-8000-000000000001", t0, "alice"),
		matrixSendLine("01965b00-0000-7000-8000-000000000002", sendTS, "alice", "bob"),
	})
	rec := buildSingleAgentRegistry(t, path, "alice")

	// Assert EffectiveLastSeen = max(t0, t0+90s) = t0+90s.
	wantELS := sendTS
	if !rec.EffectiveLastSeen.Equal(wantELS) {
		t.Errorf("G2: EffectiveLastSeen = %v, want %v (max of beat and send)", rec.EffectiveLastSeen, wantELS)
	}

	// At t0+121s: age relative to EffectiveLastSeen = t0+90s is 31s < TTL=120s → Online.
	now := t0.Add(121 * time.Second)
	got := presence.GetStateAt(rec, now)
	if got != presence.StateOnline {
		t.Errorf("G2: GetStateAt at t0+121s = %v, want StateOnline (send at t0+90s keeps agent alive)", got)
	}
}

// ---------------------------------------------------------------------------
// Boundary: EffectiveLastSeen = beat when beat is more recent than send
// ---------------------------------------------------------------------------

// TestPresenceTTL_BeatWinsOverOlderSend verifies that when the join beat is
// more recent than the send, EffectiveLastSeen = beat (the max invariant holds
// in both directions).
func TestPresenceTTL_BeatWinsOverOlderSend(t *testing.T) {
	sendTS := t0.Add(30 * time.Second) // send at t0+30s
	beatTS := t0.Add(60 * time.Second) // join at t0+60s (more recent)

	path := writeMatrixFixture(t, []string{
		matrixSendLine("01965b00-0000-7000-8000-000000000001", sendTS, "alice", "bob"),
		matrixJoinLine("01965b00-0000-7000-8000-000000000002", beatTS, "alice"),
	})
	rec := buildSingleAgentRegistry(t, path, "alice")

	wantELS := beatTS
	if !rec.EffectiveLastSeen.Equal(wantELS) {
		t.Errorf("beat-wins: EffectiveLastSeen = %v, want %v (max = beat when beat > send)", rec.EffectiveLastSeen, wantELS)
	}
}

// ---------------------------------------------------------------------------
// Ensure the scenario test file is registered via TestMain-style sentinel
// ---------------------------------------------------------------------------

// TestPresenceTTLClockMatrix_Scenario is a table-driven summary of the four
// matrix cases — a single named test the acceptance corpus can reference.
func TestPresenceTTLClockMatrix_Scenario(t *testing.T) {
	cases := []struct {
		name      string
		lines     []string
		agent     string
		nowOffset time.Duration
		wantState presence.State
		wantELS   *time.Time // optional: assert EffectiveLastSeen exactly
	}{
		{
			name: "B1: join only, t0+119s → Online",
			lines: []string{
				matrixJoinLine("01965b00-0000-7000-8000-000000000001", t0, "agent"),
			},
			agent:     "agent",
			nowOffset: 119 * time.Second,
			wantState: presence.StateOnline,
		},
		{
			name: "B2: join only, t0+121s → Stale",
			lines: []string{
				matrixJoinLine("01965b00-0000-7000-8000-000000000001", t0, "agent"),
			},
			agent:     "agent",
			nowOffset: 121 * time.Second,
			wantState: presence.StateStale,
		},
		{
			name: "G2: join t0 + send t0+90s, t0+121s → Online (ELS=t0+90s, age=31s)",
			lines: []string{
				matrixJoinLine("01965b00-0000-7000-8000-000000000001", t0, "agent"),
				matrixSendLine("01965b00-0000-7000-8000-000000000002", t0.Add(90*time.Second), "agent", "*"),
			},
			agent:     "agent",
			nowOffset: 121 * time.Second,
			wantState: presence.StateOnline,
			wantELS:   func() *time.Time { v := t0.Add(90 * time.Second); return &v }(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.jsonl")
			f, err := os.Create(path)
			if err != nil {
				t.Fatalf("create fixture: %v", err)
			}
			for _, l := range tc.lines {
				fmt.Fprintln(f, l)
			}
			_ = f.Close()

			reg := presence.ComputeRegistry(path)
			rec, ok := reg[tc.agent]
			if !ok {
				t.Fatalf("agent %q not in registry", tc.agent)
			}

			if tc.wantELS != nil && !rec.EffectiveLastSeen.Equal(*tc.wantELS) {
				t.Errorf("EffectiveLastSeen = %v, want %v", rec.EffectiveLastSeen, *tc.wantELS)
			}

			got := presence.GetStateAt(rec, t0.Add(tc.nowOffset))
			if got != tc.wantState {
				t.Errorf("GetStateAt at t0+%v = %v, want %v", tc.nowOffset, got, tc.wantState)
			}
		})
	}
}
