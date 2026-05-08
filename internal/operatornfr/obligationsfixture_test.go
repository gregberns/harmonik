package operatornfr_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// obligationsFixturePrerequisiteFailure models one entry in the ON-003
// startup failure-mode catalog. Fields match §4.1.ON-003's required per-entry
// columns: detection rule keyword, §8 exit code, and the emitted event type.
//
// Spec ref: operator-nfr.md §4.1 ON-003 — "For every daemon-startup
// prerequisite failure … the catalog MUST specify: detection rule, exit code
// per §4.1.ON-001, operator remediation procedure, emitted event type …"
type obligationsFixturePrerequisiteFailure struct {
	Name         string // human-readable name for t.Run labelling
	ExitCode     int
	EmittedEvent string
}

// obligationsFixtureStartupCatalog is the fixture representation of the
// ON-003 startup failure-mode catalog. It enumerates every daemon-startup
// prerequisite failure listed in §4.1.ON-003 with its §8 exit code.
//
// The catalog is co-owned with process-lifecycle.md §4.2; additions to the
// process-lifecycle startup sequence MUST add a corresponding entry here and
// in the §8 taxonomy.
//
// Spec ref: operator-nfr.md §4.1 ON-003.
var obligationsFixtureStartupCatalog = []obligationsFixturePrerequisiteFailure{
	{"git-bad-state", 7, "daemon_startup_failed"},
	{"beads-unavailable", 8, "daemon_startup_failed"},
	{"queue-format-unsupported", 2, "daemon_startup_failed"},
	{"checkpoint-schema-unsupported", 3, "daemon_startup_failed"},
	{"pidfile-locked", 5, "daemon_startup_failed"},
	{"filesystem-unwritable", 9, "daemon_startup_failed"},
	{"disk-full", 10, "daemon_startup_failed"},
	{"socket-bind-failed", 6, "daemon_startup_failed"},
	{"event-schema-unsupported", 4, "daemon_startup_failed"},
	{"runtime-panic", 19, "daemon_startup_failed"},
	{"ntm-unavailable", 22, "daemon_startup_failed"},
	{"orchestrator-agent-unavailable", 23, "daemon_startup_failed"},
}

// obligationsFixtureConfigKnob models one entry in the ON-004 config
// inventory. Fields match §4.1.ON-004's required per-entry columns.
//
// Spec ref: operator-nfr.md §4.1 ON-004 — "For each knob, the inventory MUST
// specify: the precedence layer, the default value, the allowed range or
// enumeration, and the change-takes-effect semantics."
type obligationsFixtureConfigKnob struct {
	Name            string // unique knob identifier
	PrecedenceLayer string // runtime-override / operator-policy / workflow / default
	ChangeEffective string // next-daemon-start / next-operator-pause / immediate
	SpecRef         string // normative spec section owning the knob
}

// obligationsFixtureConfigInventory is the fixture representation of the
// ON-004 config inventory. It enumerates the minimum set of knobs listed in
// §4.1.ON-004.
//
// Spec ref: operator-nfr.md §4.1 ON-004 — "At minimum the inventory covers
// the timer-flush cadence, budget warning threshold, drain timeout, RTO
// thresholds, queue-empty re-query cadence, Cat 0 pre-check retry cadence,
// and per-Cat reconciliation budgets."
var obligationsFixtureConfigInventory = []obligationsFixtureConfigKnob{
	{
		Name:            "timer_flush_cadence",
		PrecedenceLayer: "operator-policy",
		ChangeEffective: "next-daemon-start",
		SpecRef:         "event-model.md §4.4",
	},
	{
		Name:            "budget_warning_threshold",
		PrecedenceLayer: "operator-policy",
		ChangeEffective: "immediate",
		SpecRef:         "control-points.md §4.5",
	},
	{
		Name:            "drain_timeout_total",
		PrecedenceLayer: "operator-policy",
		ChangeEffective: "next-daemon-start",
		SpecRef:         "operator-nfr.md §4.7 ON-029",
	},
	{
		Name:            "rto_nominal_seconds",
		PrecedenceLayer: "operator-policy",
		ChangeEffective: "next-daemon-start",
		SpecRef:         "operator-nfr.md §4.8 ON-031",
	},
	{
		Name:            "rto_hard_ceiling_seconds",
		PrecedenceLayer: "operator-policy",
		ChangeEffective: "next-daemon-start",
		SpecRef:         "operator-nfr.md §4.8 ON-032",
	},
	{
		Name:            "queue_empty_requery_cadence",
		PrecedenceLayer: "operator-policy",
		ChangeEffective: "next-daemon-start",
		SpecRef:         "process-lifecycle.md §4.4",
	},
	{
		Name:            "cat0_precheck_retry_cadence",
		PrecedenceLayer: "operator-policy",
		ChangeEffective: "next-daemon-start",
		SpecRef:         "reconciliation/spec.md §4.3",
	},
	{
		Name:            "per_cat_reconciliation_budget",
		PrecedenceLayer: "operator-policy",
		ChangeEffective: "next-daemon-start",
		SpecRef:         "reconciliation/spec.md §4.4",
	},
}

// TestON003_StartupCatalogCoverageAgainstTaxonomy verifies that every entry in
// the startup failure-mode catalog resolves to a §8 taxonomy entry. This is
// the static-check obligation from §10.2: "verify startup failure-mode catalog
// co-owned with PL §4.2 covers all enumerated prerequisite failures."
//
// Spec ref: operator-nfr.md §10.2 — "verify startup failure-mode catalog
// co-owned with PL §4.2 covers all enumerated prerequisite failures."
func TestON003_StartupCatalogCoverageAgainstTaxonomy(t *testing.T) {
	t.Parallel()

	for _, f := range obligationsFixtureStartupCatalog {
		f := f
		t.Run(f.Name, func(t *testing.T) {
			t.Parallel()

			// Each catalog entry MUST resolve to a §8 taxonomy entry.
			e, ok := exitCodeFixtureLookup(f.ExitCode)
			if !ok {
				t.Errorf("ON-003: startup catalog entry %q has exit code %d which is not in the §8 taxonomy; every catalog exit code MUST resolve to a §8 entry", f.Name, f.ExitCode)
				return
			}

			// Each catalog entry MUST emit daemon_startup_failed (per ON-003).
			if f.EmittedEvent != "daemon_startup_failed" {
				t.Errorf("ON-003: startup catalog entry %q emitted_event = %q, want %q", f.Name, f.EmittedEvent, "daemon_startup_failed")
			}

			// The §8 taxonomy entry for a startup code MUST also name
			// daemon_startup_failed as its emitted event.
			if e.EmittedEvent != "daemon_startup_failed" {
				t.Errorf("ON-003: §8 taxonomy entry for code %d (category %q) emitted_event = %q, want %q — startup prerequisite failures MUST emit daemon_startup_failed", f.ExitCode, e.Category, e.EmittedEvent, "daemon_startup_failed")
			}

			// The §8 entry MUST be flagged as a startup code.
			if !e.IsStartup {
				t.Errorf("ON-003: §8 taxonomy entry for code %d (category %q) has IsStartup=false; it appears in the startup catalog so it MUST be a startup code", f.ExitCode, e.Category)
			}
		})
	}
}

// TestON003_TaxonomyStartupCodesAreInCatalog is the reverse of the previous
// test: every §8 taxonomy entry marked IsStartup MUST appear in the startup
// catalog. This prevents silent drift where the taxonomy gains a startup code
// that the catalog doesn't enumerate.
//
// Spec ref: operator-nfr.md §4.1 ON-003 — co-owned startup failure-mode
// catalog; §8 taxonomy is authoritative input.
func TestON003_TaxonomyStartupCodesAreInCatalog(t *testing.T) {
	t.Parallel()

	// Build a set of exit codes in the startup catalog.
	catalogCodes := make(map[int]bool)
	for _, f := range obligationsFixtureStartupCatalog {
		catalogCodes[f.ExitCode] = true
	}

	// Every IsStartup entry in the §8 taxonomy MUST appear in the catalog.
	for _, e := range exitCodeFixtureTable {
		e := e
		if !e.IsStartup {
			continue
		}
		t.Run(e.Category, func(t *testing.T) {
			t.Parallel()
			if !catalogCodes[e.Code] {
				t.Errorf("ON-003: §8 taxonomy marks code %d (category %q) as IsStartup=true but it is missing from the startup catalog; catalog must cover all enumerated startup failures", e.Code, e.Category)
			}
		})
	}
}

// TestON003_StartupCatalogIsNonEmpty verifies that the startup catalog has at
// least the eight prerequisite failures enumerated by name in §4.1.ON-003.
//
// Spec ref: operator-nfr.md §4.1 ON-003 — lists eight prerequisite failure
// categories by name: "git bad state, Beads SQLite unavailable, Beads schema
// version unsupported, checkpoint schema version unsupported, stale-pidfile
// race, filesystem unwritable, disk-full during checkpoint commit, socket bind
// failure."
func TestON003_StartupCatalogIsNonEmpty(t *testing.T) {
	t.Parallel()

	const minRequired = 8 // eight named in §4.1.ON-003
	if len(obligationsFixtureStartupCatalog) < minRequired {
		t.Errorf("ON-003: startup catalog has %d entries, want at least %d (the eight named in §4.1.ON-003)", len(obligationsFixtureStartupCatalog), minRequired)
	}

	// Verify each of the eight named failures is present by checking that a
	// catalog entry maps to the expected exit code.
	required := map[string]int{
		"git-bad-state":                 7,
		"beads-unavailable":             8,
		"queue-format-unsupported":      2,
		"checkpoint-schema-unsupported": 3,
		"pidfile-locked":                5,
		"filesystem-unwritable":         9,
		"disk-full":                     10,
		"socket-bind-failed":            6,
	}

	catalogByCode := make(map[int]string)
	for _, f := range obligationsFixtureStartupCatalog {
		catalogByCode[f.ExitCode] = f.Name
	}

	for name, code := range required {
		if catalogByCode[code] == "" {
			t.Errorf("ON-003: required startup failure %q (exit code %d) is missing from the catalog", name, code)
		}
	}
}

// TestON004_ConfigInventoryIsNonEmpty verifies that the config inventory
// fixture covers the minimum set of knobs named in §4.1.ON-004.
//
// Spec ref: operator-nfr.md §4.1 ON-004 — "At minimum the inventory covers
// the timer-flush cadence, budget warning threshold, drain timeout, RTO
// thresholds, queue-empty re-query cadence, Cat 0 pre-check retry cadence,
// and per-Cat reconciliation budgets."
func TestON004_ConfigInventoryIsNonEmpty(t *testing.T) {
	t.Parallel()

	const minRequired = 8 // seven named + one for per-Cat budgets
	if len(obligationsFixtureConfigInventory) < minRequired {
		t.Errorf("ON-004: config inventory has %d entries, want at least %d (the minimum set named in §4.1.ON-004)", len(obligationsFixtureConfigInventory), minRequired)
	}
}

// TestON004_ConfigInventoryKnobsHaveRequiredFields verifies that every entry
// in the config inventory has all required fields populated.
//
// Spec ref: operator-nfr.md §4.1 ON-004 — "For each knob, the inventory MUST
// specify: the precedence layer … default value … allowed range … and the
// change-takes-effect semantics."
func TestON004_ConfigInventoryKnobsHaveRequiredFields(t *testing.T) {
	t.Parallel()

	for _, k := range obligationsFixtureConfigInventory {
		k := k
		t.Run(k.Name, func(t *testing.T) {
			t.Parallel()

			if k.Name == "" {
				t.Error("ON-004: config knob has empty Name field")
			}
			if k.PrecedenceLayer == "" {
				t.Errorf("ON-004: config knob %q has empty PrecedenceLayer; must be one of: runtime-override, operator-policy, workflow, default", k.Name)
			}
			if k.ChangeEffective == "" {
				t.Errorf("ON-004: config knob %q has empty ChangeEffective; must be one of: next-daemon-start, next-operator-pause, immediate", k.Name)
			}
			if k.SpecRef == "" {
				t.Errorf("ON-004: config knob %q has empty SpecRef; must cite the owning spec section", k.Name)
			}
		})
	}
}

// TestON004_ConfigInventoryPrecedenceLayersAreValid verifies that every
// PrecedenceLayer value is one of the four layers defined in §4.1.ON-004 and
// control-points.md §4.7 CP-037.
//
// Spec ref: operator-nfr.md §4.1 ON-004 — "the precedence layer (runtime
// override / operator-policy file / workflow definition / default, per
// [control-points.md §4.7] CP-037)."
func TestON004_ConfigInventoryPrecedenceLayersAreValid(t *testing.T) {
	t.Parallel()

	valid := map[string]bool{
		"runtime-override": true,
		"operator-policy":  true,
		"workflow":         true,
		"default":          true,
	}

	for _, k := range obligationsFixtureConfigInventory {
		k := k
		t.Run(k.Name, func(t *testing.T) {
			t.Parallel()
			if !valid[k.PrecedenceLayer] {
				t.Errorf("ON-004: config knob %q has invalid PrecedenceLayer %q; valid values are: runtime-override, operator-policy, workflow, default", k.Name, k.PrecedenceLayer)
			}
		})
	}
}

// TestON004_ConfigInventoryChangeEffectiveSemanticsAreValid verifies that
// every ChangeEffective value is one of the allowed semantics named in ON-004.
//
// Spec ref: operator-nfr.md §4.1 ON-004 — "change-takes-effect semantics
// (next operator pause, immediate, next daemon start, etc.)."
func TestON004_ConfigInventoryChangeEffectiveSemanticsAreValid(t *testing.T) {
	t.Parallel()

	valid := map[string]bool{
		"next-daemon-start":   true,
		"next-operator-pause": true,
		"immediate":           true,
	}

	for _, k := range obligationsFixtureConfigInventory {
		k := k
		t.Run(k.Name, func(t *testing.T) {
			t.Parallel()
			if !valid[k.ChangeEffective] {
				t.Errorf("ON-004: config knob %q has invalid ChangeEffective %q; valid values are: next-daemon-start, next-operator-pause, immediate", k.Name, k.ChangeEffective)
			}
		})
	}
}

// obligationsFixtureRepoRoot returns the absolute path to the repository root,
// calculated relative to this test file's location at
// internal/operatornfr/obligationsfixture_test.go.
func obligationsFixtureRepoRoot(t *testing.T) string {
	t.Helper()

	// runtime.Caller(0) gives the path of this source file at compile time.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("obligationsFixtureRepoRoot: runtime.Caller(0) failed")
	}

	// thisFile is …/internal/operatornfr/obligationsfixture_test.go; walk up
	// two directories to reach the repo root.
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return root
}

// TestON002_SpecSectionEightExists verifies that the §8 taxonomy section
// exists in specs/operator-nfr.md. This is the artifact-existence check for
// ON-002: "The taxonomy lives in §8 of this spec."
//
// Spec ref: operator-nfr.md §4.1 ON-002 — "The taxonomy lives in §8 of this
// spec."
func TestON002_SpecSectionEightExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	specPath := filepath.Join(root, "specs", "operator-nfr.md")

	//nolint:gosec // G304: specPath derived from runtime.Caller source path, not user input
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("ON-002: cannot read specs/operator-nfr.md: %v", err)
	}

	content := string(data)

	// The §8 section header must be present.
	if !strings.Contains(content, "## 8. Error and failure taxonomy") {
		t.Error("ON-002: specs/operator-nfr.md does not contain '## 8. Error and failure taxonomy'; the §8 taxonomy section must exist")
	}

	// The taxonomy table must contain at least the MVH surface header row.
	if !strings.Contains(content, "Exit-code taxonomy") {
		t.Error("ON-002: specs/operator-nfr.md §8 does not contain 'Exit-code taxonomy'; the table preamble must be present")
	}
}

// TestON003_SpecSectionFourOneON003Exists verifies that §4.1 ON-003 (the
// startup failure-mode catalog obligation) is present in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.1 ON-003.
func TestON003_SpecSectionFourOneON003Exists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	specPath := filepath.Join(root, "specs", "operator-nfr.md")

	//nolint:gosec // G304: specPath derived from runtime.Caller source path, not user input
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("ON-003: cannot read specs/operator-nfr.md: %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "ON-003") {
		t.Error("ON-003: specs/operator-nfr.md does not contain 'ON-003'; the startup failure-mode catalog obligation section must exist")
	}

	if !strings.Contains(content, "Startup failure-mode catalog obligation") {
		t.Error("ON-003: specs/operator-nfr.md does not contain the 'Startup failure-mode catalog obligation' heading")
	}
}

// TestON004_SpecSectionFourOneON004Exists verifies that §4.1 ON-004 (the
// config inventory obligation) is present in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.1 ON-004.
func TestON004_SpecSectionFourOneON004Exists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	specPath := filepath.Join(root, "specs", "operator-nfr.md")

	//nolint:gosec // G304: specPath derived from runtime.Caller source path, not user input
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("ON-004: cannot read specs/operator-nfr.md: %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "ON-004") {
		t.Error("ON-004: specs/operator-nfr.md does not contain 'ON-004'; the config inventory obligation section must exist")
	}

	if !strings.Contains(content, "Config inventory obligation") {
		t.Error("ON-004: specs/operator-nfr.md does not contain the 'Config inventory obligation' heading")
	}
}

// TestON002_StaticCheck_SectionFourOneCrossRefsResolveTo8 verifies that
// §4.1-level requirements that declare cross-references to the exit-code
// taxonomy ("per §4.1.ON-001" or "per §8") are resolvable: the referenced
// codes all exist in the fixture taxonomy table.
//
// The static check is: every code referenced in the spec's §8 table row MUST
// have an entry in exitCodeFixtureTable. Since exitCodeFixtureTable IS the
// fixture mirror of §8, any mismatch is a discrepancy in the fixture itself.
//
// Spec ref: operator-nfr.md §10.2 — "static-check test verifying that every
// requirement with a cross-reference to §4.1 resolves to a §8 entry."
func TestON002_StaticCheck_SectionFourOneCrossRefsResolveTo8(t *testing.T) {
	t.Parallel()

	// The ON-003 startup catalog items and the config inventory knobs in
	// ON-004 both cross-reference §4.1 (specifically ON-001) via exit codes.
	// Verify that every exit code cited by §4.1 requirements (codes 2–10,
	// 14, 19, 22, 23 from ON-003 plus codes 11, 12 from ON-032/ON-031) has
	// a §8 entry.
	//
	// This is expressed as the set of cross-reference codes from §4.1 that
	// MUST resolve: every code cited within §4.1 requirements.
	section41CrossRefCodes := []struct {
		code    int
		context string // which §4.1 requirement cites this code
	}{
		{2, "ON-016 (queue-format-unsupported)"},
		{3, "ON-018 (checkpoint-schema-unsupported)"},
		{5, "ON-003 (pidfile-locked)"},
		{6, "ON-003 (socket-bind-failed)"},
		{7, "ON-003 (git-bad-state)"},
		{8, "ON-003 (beads-unavailable)"},
		{9, "ON-003 (filesystem-unwritable)"},
		{10, "ON-003 (disk-full)"},
		{11, "ON-027 (drain-timeout-escalated)"},
		{12, "ON-032 (rto-hard-ceiling-exceeded)"},
		{13, "ON-020 (upgrade-requires-paused)"},
		{14, "ON-005a (upgrade-hash-mismatch)"},
		{15, "ON-019 (upgrade-schema-incompatible)"},
		{16, "ON-011 (operator-control-invalid-state)"},
		{17, "ON-041 (multi-daemon-target-missing)"},
		{18, "ON-041 (machine-ceiling-exhausted)"},
		{19, "PL-018a (runtime-panic)"},
		{22, "PL-021a (ntm-unavailable)"},
		{23, "PL-028 (orchestrator-agent-unavailable)"},
	}

	for _, ref := range section41CrossRefCodes {
		ref := ref
		t.Run(ref.context, func(t *testing.T) {
			t.Parallel()
			_, ok := exitCodeFixtureLookup(ref.code)
			if !ok {
				t.Errorf("ON-002 static-check: §4.1 requirement %q references exit code %d but that code is not in the §8 taxonomy fixture; cross-reference MUST resolve", ref.context, ref.code)
			}
		})
	}
}

// TestON001_HarmonikStatusResolvesEveryNonZeroCode verifies the static
// obligation that `harmonik status` must be able to resolve every non-zero
// §8 exit code to a category. This is exercised at the fixture level: the
// lookup helper (which models the status command's taxonomy lookup) MUST
// return a non-empty category for every code 1..23.
//
// Spec ref: operator-nfr.md §10.2 — "verify `harmonik status` resolves §8
// entries on every non-zero exit observed"; operator-nfr.md §4.1 ON-002 —
// "cross-references from other specs … MUST resolve to §8 entries."
func TestON001_HarmonikStatusResolvesEveryNonZeroCode(t *testing.T) {
	t.Parallel()

	const maxMVHCode = 23

	for code := 1; code <= maxMVHCode; code++ {
		code := code
		t.Run("", func(t *testing.T) {
			t.Parallel()
			e, ok := exitCodeFixtureLookup(code)
			if !ok {
				t.Errorf("harmonik-status resolve: exit code %d has no §8 taxonomy entry; `harmonik status` cannot resolve it", code)
				return
			}
			if e.Category == "" {
				t.Errorf("harmonik-status resolve: exit code %d taxonomy entry has empty category; `harmonik status` cannot produce a meaningful status for it", code)
			}
		})
	}
}
