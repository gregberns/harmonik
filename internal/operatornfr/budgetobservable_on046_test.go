package operatornfr_test

// budgetObservableON046 — spec-level harness for hk-sx9r.65.
//
// Covers: ON-046 (budget events are operator-observable — summarized view via
// `harmonik status` and attach UI, no raw JSONL parsing required).
//
// These are spec-artifact existence and structural-constraint tests. The §10.2
// sensor obligation for ON-046 is: (a) `harmonik status` includes a budget-
// summary section for `budget_warning`, `budget_exhausted`, and `budget_accrual`
// without requiring JSONL parsing; (b) the summary names event type, run_id,
// and threshold/remaining-fraction; (c) the attach UI's T_attach_status snapshot
// includes the budget summary; (d) no raw JSONL bytes are exposed.
//
// Spec ref: specs/operator-nfr.md §4.11 ON-046, §10.2.

import (
	"strings"
	"testing"
)

// budgetObservableON046EventType models one budget event type that ON-046
// requires to be operator-observable via `harmonik status` and the attach UI.
//
// Spec ref: operator-nfr.md §4.11 ON-046 — "budget_warning, budget_exhausted,
// budget_accrual per [event-model.md §8.4] and [control-points.md §4.5]."
type budgetObservableON046EventType struct {
	Name    string // event type name
	EV      string // event-model.md §8.4 sub-section
	SpecRef string
}

// budgetObservableON046EventTypes is the authoritative fixture encoding the
// three budget-threshold event types that ON-046 requires to be observable.
var budgetObservableON046EventTypes = []budgetObservableON046EventType{
	{"budget_warning", "event-model.md §8.4.1", "ON-046 — warning threshold crossed"},
	{"budget_exhausted", "event-model.md §8.4.3", "ON-046 — budget exhausted"},
	{"budget_accrual", "event-model.md §8.4.2", "ON-046 — per-chunk accrual"},
}

// budgetObservableON046ObservationSurface models one observation surface
// named by ON-046 where budget events must be visible.
//
// Spec ref: operator-nfr.md §4.11 ON-046 — "MUST be operator-observable via
// `harmonik status` and the attach UI per [process-lifecycle.md §4.10]."
type budgetObservableON046ObservationSurface struct {
	Name    string // surface name
	Keyword string // keyword that must appear in the spec under ON-046
	SpecRef string
}

// budgetObservableON046ObservationSurfaces is the authoritative fixture
// encoding the two observation surfaces declared by ON-046.
var budgetObservableON046ObservationSurfaces = []budgetObservableON046ObservationSurface{
	{
		Name:    "harmonik-status",
		Keyword: "harmonik status",
		SpecRef: "ON-046 — harmonik status command",
	},
	{
		Name:    "attach-ui",
		Keyword: "attach UI",
		SpecRef: "ON-046 — attach UI per process-lifecycle.md §4.10",
	},
}

// on046Section extracts the ON-046 section of the spec, bounded by the
// "#### ON-046" heading and the immediately following "#### ON-047" heading.
// This avoids false matches from cross-reference mentions of the IDs elsewhere
// in the document.
func on046Section(t *testing.T, content string) string {
	t.Helper()

	start := strings.Index(content, "#### ON-046")
	if start < 0 {
		t.Fatal("ON-046: '#### ON-046' heading not found in specs/operator-nfr.md")
	}
	// Find the next section heading relative to start so we don't rely on the
	// first occurrence of "#### ON-047" in the whole document.
	rel := strings.Index(content[start:], "#### ON-047")
	if rel < 0 {
		t.Fatal("ON-046: '#### ON-047' heading not found after ON-046; needed to bound section")
	}
	return content[start : start+rel]
}

// --- spec-existence tests ---

// TestON046_SectionFourElevenExists verifies that §4.11 (resource budgets
// section) exists and that ON-046 is subordinate to it.
//
// Spec ref: operator-nfr.md §4.11 Resource budgets.
func TestON046_SectionFourElevenExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "### 4.11 Resource budgets") {
		t.Error("ON-046: specs/operator-nfr.md missing '### 4.11 Resource budgets' section header")
	}

	sec411Idx := strings.Index(content, "### 4.11 Resource budgets")
	on046Idx := strings.Index(content, "#### ON-046")
	if sec411Idx < 0 || on046Idx < 0 {
		t.Fatal("ON-046: cannot verify section ordering — one of the expected markers is missing")
	}
	if on046Idx < sec411Idx {
		t.Error("ON-046: '#### ON-046' appears before '### 4.11 Resource budgets'; it must be subordinate to §4.11")
	}
}

// TestON046_AxesLinePresent verifies that ON-046 carries an Axes: line
// classifying its boundary axes.
//
// Spec ref: operator-nfr.md §4.11 ON-046 — Axes classification per
// [architecture.md §4.1]; observability queries are idempotent reads.
func TestON046_AxesLinePresent(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	section := on046Section(t, string(data))

	if !strings.Contains(section, "Axes:") {
		t.Error("ON-046: Axes: line missing from ON-046 section; every mechanism requirement with I/O or state mutation must carry Axes")
	}
	if !strings.Contains(section, "llm-freedom=none") {
		t.Error("ON-046: Axes line missing llm-freedom=none; the observability surface has no LLM-driven behavior")
	}
	if !strings.Contains(section, "io-determinism=deterministic") {
		t.Error("ON-046: Axes line missing io-determinism=deterministic; same budget state produces same status output")
	}
	if !strings.Contains(section, "idempotency=idempotent") {
		t.Error("ON-046: Axes line missing idempotency=idempotent; reading the budget-summary is an idempotent operation")
	}
}

// TestON046_ThreeEventTypesNamed verifies that the spec names all three
// budget-threshold event types within the ON-046 section.
//
// Spec ref: operator-nfr.md §4.11 ON-046 — "budget_warning, budget_exhausted,
// budget_accrual per [event-model.md §8.4]."
func TestON046_ThreeEventTypesNamed(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	section := on046Section(t, string(data))

	for _, ev := range budgetObservableON046EventTypes {
		ev := ev
		t.Run(ev.Name, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(section, ev.Name) {
				t.Errorf("ON-046: budget event type %q not found in ON-046 section; all three event types must be named", ev.Name)
			}
		})
	}
}

// TestON046_EventTypeFixtureHasThreeTypes verifies the fixture encodes exactly
// three budget-threshold event types.
//
// Spec ref: operator-nfr.md §4.11 ON-046 — three event types.
func TestON046_EventTypeFixtureHasThreeTypes(t *testing.T) {
	t.Parallel()

	const wantTypes = 3
	if len(budgetObservableON046EventTypes) != wantTypes {
		t.Errorf("ON-046: event-type fixture has %d entries, want %d (warning/exhausted/accrual)",
			len(budgetObservableON046EventTypes), wantTypes)
	}
}

// TestON046_EventTypeFixtureFieldsNonEmpty verifies every event-type fixture
// entry has non-empty Name, EV, and SpecRef.
//
// Spec ref: operator-nfr.md §4.11 ON-046.
func TestON046_EventTypeFixtureFieldsNonEmpty(t *testing.T) {
	t.Parallel()

	for _, ev := range budgetObservableON046EventTypes {
		ev := ev
		t.Run(ev.Name, func(t *testing.T) {
			t.Parallel()

			if ev.Name == "" {
				t.Error("ON-046: event-type fixture entry has empty Name")
			}
			if ev.EV == "" {
				t.Errorf("ON-046: event type %q has empty EV (event-model citation)", ev.Name)
			}
			if ev.SpecRef == "" {
				t.Errorf("ON-046: event type %q has empty SpecRef", ev.Name)
			}
		})
	}
}

// TestON046_TwoObservationSurfacesNamed verifies that the spec names both
// required observation surfaces (`harmonik status` and the attach UI) within
// ON-046.
//
// Spec ref: operator-nfr.md §4.11 ON-046 — "MUST be operator-observable via
// `harmonik status` and the attach UI."
func TestON046_TwoObservationSurfacesNamed(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	section := on046Section(t, string(data))

	for _, surface := range budgetObservableON046ObservationSurfaces {
		surface := surface
		t.Run(surface.Name, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(section, surface.Keyword) {
				t.Errorf("ON-046: observation surface %q — keyword %q not found in ON-046 section; both surfaces must be named", surface.Name, surface.Keyword)
			}
		})
	}
}

// TestON046_ObservationSurfaceFixtureHasTwoEntries verifies the fixture encodes
// exactly two observation surfaces.
//
// Spec ref: operator-nfr.md §4.11 ON-046 — harmonik status + attach UI.
func TestON046_ObservationSurfaceFixtureHasTwoEntries(t *testing.T) {
	t.Parallel()

	const wantSurfaces = 2
	if len(budgetObservableON046ObservationSurfaces) != wantSurfaces {
		t.Errorf("ON-046: observation-surface fixture has %d entries, want %d (harmonik-status + attach-ui)",
			len(budgetObservableON046ObservationSurfaces), wantSurfaces)
	}
}

// TestON046_SummarizedViewRequirement verifies that the spec explicitly states
// a summarized view is adequate and that raw JSONL parsing is not required.
//
// Spec ref: operator-nfr.md §4.11 ON-046 — "Operator-observable MUST NOT
// require parsing the raw JSONL; a summarized view is adequate."
func TestON046_SummarizedViewRequirement(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	section := on046Section(t, string(data))

	if !strings.Contains(section, "summarized view") {
		t.Error("ON-046: 'summarized view' not found in ON-046 section; the spec must state a summarized view is adequate")
	}
	if !strings.Contains(section, "JSONL") {
		t.Error("ON-046: 'JSONL' not found in ON-046 section; the spec must explicitly state raw JSONL parsing is not required")
	}
}

// TestON046_SummaryMinimumFieldsNamed verifies that the spec names the minimum
// fields required in the observable summary (event type, run_id, and a
// threshold or remaining-fraction indicator).
//
// Spec ref: operator-nfr.md §4.11 ON-046 — "at minimum: event type name,
// run_id, and a threshold or remaining-fraction indicator."
func TestON046_SummaryMinimumFieldsNamed(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	section := on046Section(t, string(data))

	if !strings.Contains(section, "event type") {
		t.Error("ON-046: 'event type' not found in ON-046 section; the spec must name event type as a minimum summary field")
	}
	if !strings.Contains(section, "run_id") {
		t.Error("ON-046: 'run_id' not found in ON-046 section; the spec must name run_id as a minimum summary field")
	}
	if !strings.Contains(section, "threshold") {
		t.Error("ON-046: 'threshold' not found in ON-046 section; the spec must name a threshold/remaining-fraction indicator as a minimum summary field")
	}
}

// TestON046_AttachUISnapshotNamed verifies that the spec explicitly names the
// attach UI's T_attach_status periodic snapshot as a delivery mechanism.
//
// Spec ref: operator-nfr.md §4.11 ON-046 — "The attach UI's T_attach_status
// periodic snapshot per ON-050(c) MUST include the same budget-summary block."
func TestON046_AttachUISnapshotNamed(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	section := on046Section(t, string(data))

	if !strings.Contains(section, "T_attach_status") {
		t.Error("ON-046: 'T_attach_status' not found in ON-046 section; the spec must name the T_attach_status periodic snapshot as a delivery mechanism")
	}
	if !strings.Contains(section, "ON-050") {
		t.Error("ON-046: 'ON-050' cross-reference not found in ON-046 section; T_attach_status is defined in ON-050(c)")
	}
}

// TestON046_NoRawJSONLBytesExposed verifies that the spec explicitly prohibits
// exposing raw JSONL bytes in the budget-summary section.
//
// Spec ref: operator-nfr.md §4.11 ON-046 — "The budget-summary section MUST
// NOT expose raw JSONL bytes."
func TestON046_NoRawJSONLBytesExposed(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	section := on046Section(t, string(data))

	if !strings.Contains(section, "MUST NOT expose raw JSONL bytes") {
		t.Error("ON-046: 'MUST NOT expose raw JSONL bytes' not found in ON-046 section; the spec must explicitly prohibit exposing raw JSONL in the budget-summary surface")
	}
}

// TestON046_ProcessLifecycleCitationPresent verifies the spec cites
// [process-lifecycle.md §4.10] within ON-046 — the cross-spec anchor for
// the attach UI and harmonik status command surface.
//
// Spec ref: operator-nfr.md §4.11 ON-046 — "[process-lifecycle.md §4.10]"
// is the command surface section.
func TestON046_ProcessLifecycleCitationPresent(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	section := on046Section(t, string(data))

	if !strings.Contains(section, "process-lifecycle.md §4.10") {
		t.Error("ON-046: [process-lifecycle.md §4.10] citation missing from ON-046; the command surface is defined there")
	}
}

// TestON046_Section10_2SensorPresent verifies that the §10.2 test-surface
// obligation for ON-041—ON-046 includes an ON-046 sensor annotation.
//
// Spec ref: operator-nfr.md §10.2 — ON-041–ON-046 bullet with ON-046
// budget-events-observable sensor.
func TestON046_Section10_2SensorPresent(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-046 budget-events-observable sensor") {
		t.Error("ON-046: §10.2 ON-041–ON-046 bullet missing 'ON-046 budget-events-observable sensor' annotation; §10.2 must name the four sub-assertions")
	}
}

// TestON046_Section10_2SensorFourSubAssertions verifies that the §10.2 sensor
// for ON-046 names all four required sub-assertions.
//
// Spec ref: operator-nfr.md §10.2 — ON-046 sensor: (a) harmonik status budget-
// summary; (b) summary names event type, run_id, threshold; (c) attach UI
// T_attach_status; (d) no raw JSONL bytes.
func TestON046_Section10_2SensorFourSubAssertions(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	// Bound the ON-046 sensor block within §10.2: start at the sensor label,
	// end at the next bullet's sensor or end of §10.2.
	sensorStart := strings.Index(content, "ON-046 budget-events-observable sensor")
	if sensorStart < 0 {
		t.Fatal("ON-046: §10.2 sensor annotation not found; cannot verify sub-assertions")
	}
	// Take a reasonable window (up to the ON-047 bullet) after the sensor label.
	sensorWindow := content[sensorStart:]
	if idx := strings.Index(sensorWindow, "ON-047 — ON-049"); idx > 0 {
		sensorWindow = sensorWindow[:idx]
	}

	subAssertions := []struct {
		marker  string
		missing string
	}{
		{"budget-summary", "sub-assertion (a): 'budget-summary' missing from ON-046 §10.2 sensor"},
		{"without requiring", "sub-assertion (a): 'without requiring' clause missing — must state JSONL parsing not needed"},
		{"event type", "sub-assertion (b): 'event type' missing from ON-046 §10.2 sensor summary-fields list"},
		{"T_attach_status", "sub-assertion (c): T_attach_status missing from ON-046 §10.2 sensor"},
		{"raw JSONL bytes", "sub-assertion (d): 'raw JSONL bytes' not named in ON-046 §10.2 sensor"},
	}

	for _, sa := range subAssertions {
		sa := sa
		t.Run(sa.marker, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(sensorWindow, sa.marker) {
				t.Errorf("ON-046: %s", sa.missing)
			}
		})
	}
}
