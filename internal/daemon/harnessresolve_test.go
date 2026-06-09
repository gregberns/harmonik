package daemon_test

// harnessresolve_test.go — table-driven tests for the four-tier harness-selection
// precedence resolver (resolveHarness via ExportedResolveHarness).
//
// Acceptance criteria (codex-harness C4/T4, hk-y01k6):
//   - Table-driven test covers all four precedence tiers.
//   - Resolved value matches expected for each combination.
//   - bead_label_conflict event emitted for multi-label and malformed-value tier-1 cases.
//   - Conflict payload satisfies BeadLabelConflictPayload.Valid().
//   - Returned value is always a valid core.AgentType (AR-025).
//
// Helper prefix: harnessResolveFixture (per implementer-protocol.md §Helper-prefix discipline).
//
// Bead: hk-y01k6 [C4/T4]

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// harnessResolveFixtureBead builds a minimal BeadRecord with the given label set.
func harnessResolveFixtureBead(t *testing.T, labels []string) core.BeadRecord {
	t.Helper()
	return core.BeadRecord{
		BeadID:        core.BeadID("test-harness-bead-001"),
		Title:         "fixture harness bead",
		BeadType:      "task",
		Status:        core.CoarseStatusOpen,
		Labels:        labels,
		AuditTrailRef: "test-harness-bead-001",
	}
}

// harnessResolveFixtureBus is a minimal in-process event collector.
type harnessResolveFixtureBus struct {
	mu     sync.Mutex
	events []harnessResolveFixtureEvent
}

type harnessResolveFixtureEvent struct {
	EventType core.EventType
	Payload   []byte
}

func (b *harnessResolveFixtureBus) Emit(_ context.Context, et core.EventType, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, harnessResolveFixtureEvent{EventType: et, Payload: payload})
	return nil
}

func (b *harnessResolveFixtureBus) EmitWithRunID(_ context.Context, _ core.RunID, et core.EventType, payload []byte) error {
	return b.Emit(context.Background(), et, payload)
}

var _ handlercontract.EventEmitter = (*harnessResolveFixtureBus)(nil)

func harnessResolveFixtureBusEvents(t *testing.T, bus *harnessResolveFixtureBus) []harnessResolveFixtureEvent {
	t.Helper()
	bus.mu.Lock()
	defer bus.mu.Unlock()
	out := make([]harnessResolveFixtureEvent, len(bus.events))
	copy(out, bus.events)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// TestResolveHarnessPrecedence — primary table-driven test
// ─────────────────────────────────────────────────────────────────────────────

// TestResolveHarnessPrecedence covers all four tiers and conflict paths.
func TestResolveHarnessPrecedence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		beadLabels    []string
		queueDefault  core.AgentType
		nodeDefault   core.AgentType
		globalDefault core.AgentType
		wantHarness   core.AgentType
		// wantConflictEvent: when true, exactly one bead_label_conflict event must be emitted.
		wantConflictEvent bool
	}{
		// ── Tier 1: per-bead harness label wins ──────────────────────────────
		{
			name:          "tier1 harness:codex overrides global default",
			beadLabels:    []string{"area:daemon", "harness:codex"},
			globalDefault: core.AgentTypeClaudeCode,
			wantHarness:   core.AgentTypeCodex,
		},
		{
			name:          "tier1 harness:claude-code overrides global codex default",
			beadLabels:    []string{"harness:claude-code"},
			globalDefault: core.AgentTypeCodex,
			wantHarness:   core.AgentTypeClaudeCode,
		},
		{
			name:          "tier1 harness label overrides all lower tiers",
			beadLabels:    []string{"harness:codex"},
			queueDefault:  core.AgentTypeClaudeCode,
			nodeDefault:   core.AgentTypeClaudeCode,
			globalDefault: core.AgentTypeClaudeCode,
			wantHarness:   core.AgentTypeCodex,
		},

		// ── Tier 1 conflict: multiple harness labels → emit event, fall through
		{
			name:              "tier1 conflict multiple harness labels falls to tier2",
			beadLabels:        []string{"harness:codex", "harness:claude-code"},
			queueDefault:      core.AgentTypeClaudeCode,
			wantHarness:       core.AgentTypeClaudeCode,
			wantConflictEvent: true,
		},
		{
			name:              "tier1 conflict multiple harness labels falls to tier4 built-in when all absent",
			beadLabels:        []string{"harness:codex", "harness:claude-code"},
			wantHarness:       core.AgentTypeClaudeCode, // built-in fallback
			wantConflictEvent: true,
		},

		// ── Tier 1 conflict: invalid agent-type value → emit event, fall through
		{
			name:              "tier1 invalid agent-type value emits conflict and falls through",
			beadLabels:        []string{"harness:INVALID_UPPER"},
			globalDefault:     core.AgentTypeCodex,
			wantHarness:       core.AgentTypeCodex,
			wantConflictEvent: true,
		},
		{
			name:              "tier1 empty agent-type value emits conflict and falls to built-in",
			beadLabels:        []string{"harness:"},
			wantHarness:       core.AgentTypeClaudeCode, // built-in fallback
			wantConflictEvent: true,
		},

		// ── Tier 2: per-queue default ─────────────────────────────────────────
		{
			name:         "tier2 queue default wins when no bead label",
			beadLabels:   nil,
			queueDefault: core.AgentTypeCodex,
			wantHarness:  core.AgentTypeCodex,
		},
		{
			name:          "tier2 queue default overrides node and global",
			beadLabels:    []string{"area:core"},
			queueDefault:  core.AgentTypeCodex,
			nodeDefault:   core.AgentTypeClaudeCode,
			globalDefault: core.AgentTypeClaudeCode,
			wantHarness:   core.AgentTypeCodex,
		},

		// ── Tier 3: DOT node attribute ────────────────────────────────────────
		{
			name:          "tier3 node default wins when bead and queue absent",
			beadLabels:    nil,
			queueDefault:  core.AgentType(""),
			nodeDefault:   core.AgentTypeCodex,
			globalDefault: core.AgentTypeClaudeCode,
			wantHarness:   core.AgentTypeCodex,
		},
		{
			name:          "tier3 node default overrides global",
			beadLabels:    []string{"size:S"},
			queueDefault:  core.AgentType(""),
			nodeDefault:   core.AgentTypeCodex,
			globalDefault: core.AgentTypeClaudeCode,
			wantHarness:   core.AgentTypeCodex,
		},

		// ── Tier 4: global Config.DefaultHarness ─────────────────────────────
		{
			name:          "tier4 global default wins when bead/queue/node absent",
			beadLabels:    nil,
			queueDefault:  core.AgentType(""),
			nodeDefault:   core.AgentType(""),
			globalDefault: core.AgentTypeCodex,
			wantHarness:   core.AgentTypeCodex,
		},
		{
			name:          "tier4 global claude-code default when explicitly set",
			beadLabels:    nil,
			queueDefault:  core.AgentType(""),
			nodeDefault:   core.AgentType(""),
			globalDefault: core.AgentTypeClaudeCode,
			wantHarness:   core.AgentTypeClaudeCode,
		},

		// ── Tier 4 built-in fallback: all absent → claude-code ───────────────
		{
			name:          "built-in fallback to claude-code when all tiers absent",
			beadLabels:    nil,
			queueDefault:  core.AgentType(""),
			nodeDefault:   core.AgentType(""),
			globalDefault: core.AgentType(""),
			wantHarness:   core.AgentTypeClaudeCode,
		},
		{
			name:          "built-in fallback when bead has only unrelated labels",
			beadLabels:    []string{"size:S", "area:core", "codename:foo"},
			queueDefault:  core.AgentType(""),
			nodeDefault:   core.AgentType(""),
			globalDefault: core.AgentType(""),
			wantHarness:   core.AgentTypeClaudeCode,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bus := &harnessResolveFixtureBus{}
			bead := harnessResolveFixtureBead(t, tc.beadLabels)

			got := daemon.ExportedResolveHarness(
				t.Context(),
				bead,
				tc.queueDefault,
				tc.nodeDefault,
				tc.globalDefault,
				bus,
			)

			if got != tc.wantHarness {
				t.Errorf("resolveHarness: got %q; want %q", got, tc.wantHarness)
			}

			events := harnessResolveFixtureBusEvents(t, bus)
			conflictEvents := 0
			for _, e := range events {
				if e.EventType == core.EventTypeBeadLabelConflict {
					conflictEvents++
				}
			}
			if tc.wantConflictEvent && conflictEvents == 0 {
				t.Error("resolveHarness: expected bead_label_conflict event; none emitted")
			}
			if !tc.wantConflictEvent && conflictEvents > 0 {
				t.Errorf("resolveHarness: unexpected bead_label_conflict event(s): %d emitted", conflictEvents)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestResolveHarnessConflictPayloadShape
// ─────────────────────────────────────────────────────────────────────────────

// TestResolveHarnessConflictPayloadShape verifies the bead_label_conflict payload
// satisfies BeadLabelConflictPayload.Valid() and carries the expected bead_id.
func TestResolveHarnessConflictPayloadShape(t *testing.T) {
	t.Parallel()

	bus := &harnessResolveFixtureBus{}
	bead := harnessResolveFixtureBead(t, []string{"harness:codex", "harness:claude-code"})

	daemon.ExportedResolveHarness(t.Context(), bead, core.AgentType(""), core.AgentType(""), core.AgentTypeClaudeCode, bus)

	events := harnessResolveFixtureBusEvents(t, bus)
	var conflictEvent *harnessResolveFixtureEvent
	for i := range events {
		if events[i].EventType == core.EventTypeBeadLabelConflict {
			conflictEvent = &events[i]
			break
		}
	}
	if conflictEvent == nil {
		t.Fatal("resolveHarness: no bead_label_conflict event emitted for multi-label conflict")
	}

	var pl core.BeadLabelConflictPayload
	if err := json.Unmarshal(conflictEvent.Payload, &pl); err != nil {
		t.Fatalf("resolveHarness: bead_label_conflict payload unmarshal: %v", err)
	}
	if !pl.Valid() {
		t.Errorf("resolveHarness: BeadLabelConflictPayload.Valid() = false; payload = %+v", pl)
	}
	if pl.BeadID != string(bead.BeadID) {
		t.Errorf("resolveHarness: bead_label_conflict bead_id = %q; want %q", pl.BeadID, bead.BeadID)
	}
	if len(pl.ConflictingLabels) == 0 {
		t.Error("resolveHarness: bead_label_conflict conflicting_labels is empty")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestResolveHarnessResultIsValidAgentType
// ─────────────────────────────────────────────────────────────────────────────

// TestResolveHarnessResultIsValidAgentType verifies the resolved value is always
// a valid core.AgentType (AR-025) regardless of inputs.
func TestResolveHarnessResultIsValidAgentType(t *testing.T) {
	t.Parallel()

	inputs := []struct {
		labels        []string
		queueDefault  core.AgentType
		nodeDefault   core.AgentType
		globalDefault core.AgentType
	}{
		{nil, core.AgentType(""), core.AgentType(""), core.AgentType("")},
		{nil, core.AgentType(""), core.AgentType(""), core.AgentTypeClaudeCode},
		{nil, core.AgentType(""), core.AgentType(""), core.AgentTypeCodex},
		{[]string{"harness:codex"}, core.AgentType(""), core.AgentType(""), core.AgentType("")},
		{[]string{"harness:claude-code"}, core.AgentTypeCodex, core.AgentType(""), core.AgentType("")},
		{[]string{"harness:INVALID"}, core.AgentTypeCodex, core.AgentType(""), core.AgentType("")},
		{[]string{"harness:codex", "harness:claude-code"}, core.AgentType(""), core.AgentType(""), core.AgentTypeClaudeCode},
		{[]string{"area:core"}, core.AgentType(""), core.AgentType(""), core.AgentType("")},
		{[]string{"harness:"}, core.AgentType(""), core.AgentType(""), core.AgentType("")},
		{nil, core.AgentTypeCodex, core.AgentType(""), core.AgentType("")},
		{nil, core.AgentType(""), core.AgentTypeCodex, core.AgentType("")},
	}

	for _, in := range inputs {
		bus := &harnessResolveFixtureBus{}
		bead := harnessResolveFixtureBead(t, in.labels)
		got := daemon.ExportedResolveHarness(
			t.Context(), bead,
			in.queueDefault, in.nodeDefault, in.globalDefault,
			bus,
		)
		if !got.Valid() {
			t.Errorf("resolveHarness: result %q is not a valid AgentType (AR-025); labels=%v queueDefault=%q nodeDefault=%q globalDefault=%q",
				got, in.labels, in.queueDefault, in.nodeDefault, in.globalDefault)
		}
	}
}
