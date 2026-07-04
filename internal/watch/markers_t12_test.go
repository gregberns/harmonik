package watch_test

// markers_t12_test.go — RED→GREEN tests for T12 watch marker-check.
//
// Required assertions (task spec):
//   (a) A declared never_emits marker matched against the event stream produces a friendly reminder.
//   (b) An event from an agent whose type has no matching marker produces no reminder.
//   (c) A qualified marker (e.g. "queue_submit:main") matches only when the qualifier also matches.
//   (d) An event with no resolvable emitter type is skipped (no false-positive).
//
// Done-check: these tests must be GREEN; event-stream authoritative (no transcript grepping).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/watch"
)

// markerFixtureAgentsDir builds a minimal agents directory tree with manifests.
//
//	admiral: never_emits: [queue_submit:main]
//	watch:   never_emits: [queue_submit:main]
//	crew:    never_emits: []
func markerFixtureAgentsDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	agentsDir := filepath.Join(root, "agents")

	write := func(path string, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("markerFixtureAgentsDir: mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("markerFixtureAgentsDir: write %s: %v", path, err)
		}
	}

	soul := "**I am** %s — test agent.\n\n**I do**\n- Things.\n\n**I do NOT**\n- Other things.\n\n**I escalate to** captain.\n"
	operating := "## On wake\n1. Do stuff.\n\n## Loop\n1. Wait.\n"

	admiralManifest := `type: admiral
cardinality: { min: 0, max: 1 }
harness: claude
identity:
  soul: soul.md
  parent_intent: operator
context:
  - { ref: operating.md, as: instruction, presence: injected }
triggers: []
handoff:
  channel: private
keeper:
  thresholds: default
lifecycle:
  self_restart: true
tools_dir: null
markers:
  never_emits:
    - queue_submit:main
`
	watchManifest := `type: watch
cardinality: { min: 0, max: 1 }
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
context:
  - { ref: operating.md, as: instruction, presence: injected }
triggers: []
handoff:
  channel: private
keeper:
  thresholds: default
lifecycle:
  self_restart: true
tools_dir: null
markers:
  never_emits:
    - queue_submit:main
`
	crewManifest := `type: crew
cardinality: { min: 0, max: n }
harness: claude
identity:
  soul: soul.md
  parent_intent: captain
context:
  - { ref: operating.md, as: instruction, presence: injected }
triggers:
  - { id: queue, source: queue, enabled: true }
handoff:
  channel: private
keeper:
  thresholds: default
lifecycle:
  self_restart: true
tools_dir: null
markers:
  never_emits: []
`
	captainManifest := `type: captain
cardinality: { min: 0, max: 1 }
harness: claude
identity:
  soul: soul.md
  parent_intent: admiral
context:
  - { ref: operating.md, as: instruction, presence: injected }
triggers: []
handoff:
  channel: private
keeper:
  thresholds: default
lifecycle:
  self_restart: true
tools_dir: null
markers:
  never_emits: []
`

	for _, pair := range []struct {
		typeName string
		manifest string
	}{
		{"admiral", admiralManifest},
		{"watch", watchManifest},
		{"crew", crewManifest},
		{"captain", captainManifest},
	} {
		write(filepath.Join(agentsDir, pair.typeName, "manifest.yaml"), pair.manifest)
		write(filepath.Join(agentsDir, pair.typeName, "soul.md"), strings.ReplaceAll(soul, "%s", pair.typeName))
		write(filepath.Join(agentsDir, pair.typeName, "operating.md"), operating)
	}

	return agentsDir
}

// markerEvent builds a core.Event with the given type and payload map.
func markerEvent(t *testing.T, evType string, payload map[string]any) core.Event {
	t.Helper()
	ev := ledgerFixtureEvent(t, evType)
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("markerEvent: marshal payload: %v", err)
		}
		ev.Payload = raw
	}
	return ev
}

// Test (a): A qualified never_emits marker fires a friendly reminder when the
// event type and qualifier both match.
func TestMarkerChecker_QualifiedViolationProducesReminder(t *testing.T) {
	t.Parallel()

	agentsDir := markerFixtureAgentsDir(t)
	mc, err := watch.NewMarkerChecker(agentsDir)
	if err != nil {
		t.Fatalf("NewMarkerChecker: %v", err)
	}

	// admiral emits queue_submit with queue=main → violation
	ev := markerEvent(t, "queue_submit", map[string]any{
		"from":  "admiral",
		"queue": "main",
	})

	reminder := mc.Check(ev)
	if reminder == "" {
		t.Fatal("Check: expected a friendly reminder for admiral queue_submit:main, got empty string")
	}
	if !strings.Contains(reminder, "admiral") {
		t.Errorf("reminder must name the emitter type, got: %q", reminder)
	}
	if !strings.Contains(reminder, "queue_submit") {
		t.Errorf("reminder must name the event type, got: %q", reminder)
	}
}

// Test (a-2): watch type emitting queue_submit:main also fires a reminder.
func TestMarkerChecker_WatchTypeViolation(t *testing.T) {
	t.Parallel()

	agentsDir := markerFixtureAgentsDir(t)
	mc, err := watch.NewMarkerChecker(agentsDir)
	if err != nil {
		t.Fatalf("NewMarkerChecker: %v", err)
	}

	ev := markerEvent(t, "queue_submit", map[string]any{
		"from":  "watch",
		"queue": "main",
	})

	reminder := mc.Check(ev)
	if reminder == "" {
		t.Fatal("Check: expected a friendly reminder for watch queue_submit:main, got empty string")
	}
	if !strings.Contains(reminder, "watch") {
		t.Errorf("reminder must name the emitter type, got: %q", reminder)
	}
}

// Test (b): An event from a crew instance (type has empty never_emits) produces no reminder.
func TestMarkerChecker_CrewTypeNoViolation(t *testing.T) {
	t.Parallel()

	agentsDir := markerFixtureAgentsDir(t)
	mc, err := watch.NewMarkerChecker(agentsDir)
	if err != nil {
		t.Fatalf("NewMarkerChecker: %v", err)
	}

	// "paul" is a crew instance (unknown to type names → resolves to crew; crew has no never_emits)
	ev := markerEvent(t, "queue_submit", map[string]any{
		"from":  "paul",
		"queue": "main",
	})

	if reminder := mc.Check(ev); reminder != "" {
		t.Errorf("Check: crew instance must not trigger violation, got: %q", reminder)
	}
}

// Test (b-2): captain type has empty never_emits — no reminder regardless of event.
func TestMarkerChecker_CaptainTypeNoViolation(t *testing.T) {
	t.Parallel()

	agentsDir := markerFixtureAgentsDir(t)
	mc, err := watch.NewMarkerChecker(agentsDir)
	if err != nil {
		t.Fatalf("NewMarkerChecker: %v", err)
	}

	ev := markerEvent(t, "queue_submit", map[string]any{
		"from":  "captain",
		"queue": "main",
	})

	if reminder := mc.Check(ev); reminder != "" {
		t.Errorf("Check: captain must not trigger violation, got: %q", reminder)
	}
}

// Test (c): Qualified marker only fires when the qualifier matches.
// queue_submit:main does NOT fire when the queue is something other than "main".
func TestMarkerChecker_QualifiedMarkerQueueMismatch(t *testing.T) {
	t.Parallel()

	agentsDir := markerFixtureAgentsDir(t)
	mc, err := watch.NewMarkerChecker(agentsDir)
	if err != nil {
		t.Fatalf("NewMarkerChecker: %v", err)
	}

	// admiral emits queue_submit but to a different queue — not "main" → no violation
	ev := markerEvent(t, "queue_submit", map[string]any{
		"from":  "admiral",
		"queue": "feature-branch",
	})

	if reminder := mc.Check(ev); reminder != "" {
		t.Errorf("Check: qualified marker must not fire when qualifier mismatch, got: %q", reminder)
	}
}

// Test (c-2): A different event type (not queue_submit) from admiral produces no reminder.
func TestMarkerChecker_UnrelatedEventNoViolation(t *testing.T) {
	t.Parallel()

	agentsDir := markerFixtureAgentsDir(t)
	mc, err := watch.NewMarkerChecker(agentsDir)
	if err != nil {
		t.Fatalf("NewMarkerChecker: %v", err)
	}

	ev := markerEvent(t, "epic_completed", map[string]any{
		"from":  "admiral",
		"epic":  "hk-abc",
	})

	if reminder := mc.Check(ev); reminder != "" {
		t.Errorf("Check: unrelated event type must not trigger violation, got: %q", reminder)
	}
}

// Test (d): An event with no 'from' field cannot be attributed → no reminder.
func TestMarkerChecker_NoEmitterSkipped(t *testing.T) {
	t.Parallel()

	agentsDir := markerFixtureAgentsDir(t)
	mc, err := watch.NewMarkerChecker(agentsDir)
	if err != nil {
		t.Fatalf("NewMarkerChecker: %v", err)
	}

	// No 'from' field in payload
	ev := markerEvent(t, "queue_submit", map[string]any{
		"queue": "main",
	})

	if reminder := mc.Check(ev); reminder != "" {
		t.Errorf("Check: no-emitter event must be skipped, got: %q", reminder)
	}
}

// Test: NewMarkerChecker handles a missing agents dir gracefully (no error, empty rules).
func TestMarkerChecker_MissingAgentsDirEmpty(t *testing.T) {
	t.Parallel()

	mc, err := watch.NewMarkerChecker("/nonexistent/path/agents")
	if err != nil {
		t.Fatalf("NewMarkerChecker with missing dir: %v", err)
	}

	ev := markerEvent(t, "queue_submit", map[string]any{
		"from":  "admiral",
		"queue": "main",
	})

	if reminder := mc.Check(ev); reminder != "" {
		t.Errorf("empty checker must produce no reminders, got: %q", reminder)
	}
}
