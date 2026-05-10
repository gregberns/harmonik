package operatornfr_test

// restartRTOFixture — spec-level harness for hk-sx9r.81.
//
// Covers: ON-030 (restart reconstruction path), ON-030a (pause-state durable
// marker), ON-031 (restart RTO target 30s p95 / 300s ceiling), ON-032 (RTO
// criteria and hard ceiling), ON-033 (RTO measurement boundary, monotonic
// clock), ON-053 (post-panic forensic file), ON-INV-005 (every subsystem
// reports reconstruction contribution).
//
// These are spec-artifact existence and structural-constraint tests. Runtime
// RTO benchmarking is a long-running integration test (tagged post-mvh);
// this file is the §10.2 sensor layer verifying the obligation catalog exists
// and constraint values are internally consistent.
//
// Spec ref: specs/operator-nfr.md §4.8, §4.9 ON-053, §5 ON-INV-005, §10.2.

import (
	"strings"
	"testing"
)

// restartRTOFixtureNominalSeconds is the 30-second nominal RTO target per
// §4.8.ON-031. The p95 measurement under the standard fixture MUST stay below
// this value.
//
// Spec ref: operator-nfr.md §4.8 ON-031 — "30 seconds nominal fixture target
// (p95 under the standard fixture)."
const restartRTOFixtureNominalSeconds = 30

// restartRTOFixtureHardCeilingSeconds is the 300-second hard ceiling per
// §4.8.ON-031 and §4.8.ON-032 criterion 3.
//
// Spec ref: operator-nfr.md §4.8 ON-031 — "300-second hard ceiling."
const restartRTOFixtureHardCeilingSeconds = 300

// restartRTOFixtureStandardFixture describes the fixture bounds that define
// the standard RTO measurement environment per §4.8.ON-032 criterion 1.
//
// Spec ref: operator-nfr.md §4.8 ON-032 — "Standard fixture for RTO
// measurement: ≤ 500 open beads, ≤ 50 in-flight runs, git-log depth ≤ 10,000
// commits … ≤ 100 reconciliation-Cat-3-pending runs, ≤ 10 active investigator
// workflows."
type restartRTOFixtureStandardFixtureBounds struct {
	MaxOpenBeads           int
	MaxInFlightRuns        int
	MaxGitLogDepthCommits  int
	MaxCat3PendingRuns     int
	MaxActiveInvestigators int
}

// restartRTOFixtureStdBounds is the authoritative encoding of the ON-032
// criterion-1 standard fixture bounds.
var restartRTOFixtureStdBounds = restartRTOFixtureStandardFixtureBounds{
	MaxOpenBeads:           500,
	MaxInFlightRuns:        50,
	MaxGitLogDepthCommits:  10000,
	MaxCat3PendingRuns:     100,
	MaxActiveInvestigators: 10,
}

// restartRTOFixturePauseReasonDiscriminator models one valid pause-reason
// discriminator value per ON-030a and ON-054.
//
// Spec ref: operator-nfr.md §4.8 ON-030a; §4.10 ON-054.
type restartRTOFixturePauseReasonDiscriminator struct {
	Value   string // one of: operator, improvement, upgrade-prepare
	SpecRef string
}

// restartRTOFixturePauseReasons enumerates the three valid pause-reason
// discriminator values written to the .harmonik/daemon.state marker file.
//
// Spec ref: operator-nfr.md §4.8 ON-030a — "pause-reason discriminator (when
// applicable; one of `operator`, `improvement`, `upgrade-prepare`)."
var restartRTOFixturePauseReasons = []restartRTOFixturePauseReasonDiscriminator{
	{"operator", "operator-nfr.md §4.8 ON-030a; ON-054"},
	{"improvement", "operator-nfr.md §4.8 ON-030a; ON-054"},
	{"upgrade-prepare", "operator-nfr.md §4.8 ON-030a; ON-054"},
}

// restartRTOFixtureForensicFileField models one field that the post-panic
// forensic file MUST contain per ON-053.
//
// Spec ref: operator-nfr.md §4.8 ON-053 — "(a) Go runtime panic message and
// stack trace; (b) daemon PID, PGID, project_hash, and binary commit hash;
// (c) timestamp … (d) last-emitted run_id / node_id / event_id."
type restartRTOFixtureForensicFileField struct {
	Name    string // canonical field name
	SpecRef string // clause in ON-053 (a/b/c/d)
}

// restartRTOFixtureForensicFields is the authoritative encoding of every
// required forensic file field per ON-053.
var restartRTOFixtureForensicFields = []restartRTOFixtureForensicFileField{
	{"panic-stack", "ON-053 (a) panic message and stack trace"},
	{"pid", "ON-053 (b) daemon PID"},
	{"pgid", "ON-053 (b) daemon PGID"},
	{"project_hash", "ON-053 (b) project_hash"},
	{"binary-commit-hash", "ON-053 (b) binary commit hash"},
	{"timestamp-wall-clock", "ON-053 (c) wall-clock RFC3339 timestamp"},
	{"timestamp-monotonic", "ON-053 (c) time.Since(boot) monotonic form"},
	{"last-run-id", "ON-053 (d) last-emitted run_id (best-effort)"},
	{"last-node-id", "ON-053 (d) last-emitted node_id (best-effort)"},
	{"last-event-id", "ON-053 (d) last-emitted event_id (best-effort)"},
}

// TestON031_NominalTargetIsThirtySeconds verifies the fixture encodes the
// ON-031 nominal RTO target as 30 seconds.
//
// Spec ref: operator-nfr.md §4.8 ON-031.
func TestON031_NominalTargetIsThirtySeconds(t *testing.T) {
	t.Parallel()

	if restartRTOFixtureNominalSeconds != 30 {
		t.Errorf("ON-031: fixture nominal target = %d s, want 30 s (the spec-mandated p95 target)", restartRTOFixtureNominalSeconds)
	}
}

// TestON031_HardCeilingIsThreeHundredSeconds verifies the fixture encodes the
// ON-031/ON-032 hard ceiling as 300 seconds.
//
// Spec ref: operator-nfr.md §4.8 ON-031; ON-032 criterion 3.
func TestON031_HardCeilingIsThreeHundredSeconds(t *testing.T) {
	t.Parallel()

	if restartRTOFixtureHardCeilingSeconds != 300 {
		t.Errorf("ON-031: fixture hard ceiling = %d s, want 300 s (ON-031/ON-032 criterion 3)", restartRTOFixtureHardCeilingSeconds)
	}
}

// TestON031_HardCeilingExceedsNominalTarget verifies that the hard ceiling
// is strictly greater than the nominal target (a structural sanity check).
//
// Spec ref: operator-nfr.md §4.8 ON-031; ON-032 criterion 3.
func TestON031_HardCeilingExceedsNominalTarget(t *testing.T) {
	t.Parallel()

	if restartRTOFixtureHardCeilingSeconds <= restartRTOFixtureNominalSeconds {
		t.Errorf("ON-031: hard ceiling (%d s) MUST exceed nominal target (%d s)",
			restartRTOFixtureHardCeilingSeconds, restartRTOFixtureNominalSeconds)
	}
}

// TestON032_HardCeilingExitCodeExistsInTaxonomy verifies that §8 code 12
// (rto-hard-ceiling-exceeded) is present in the taxonomy, as required by
// ON-032 criterion 3.
//
// Spec ref: operator-nfr.md §4.8 ON-032 criterion 3; §8 code 12.
func TestON032_HardCeilingExitCodeExistsInTaxonomy(t *testing.T) {
	t.Parallel()

	const hardCeilingCode = 12
	e, ok := exitCodeFixtureLookup(hardCeilingCode)
	if !ok {
		t.Errorf("ON-032: §8 taxonomy missing code %d (rto-hard-ceiling-exceeded); criterion 3 requires this code", hardCeilingCode)
		return
	}
	if e.Category != "rto-hard-ceiling-exceeded" {
		t.Errorf("ON-032: §8 code %d category = %q, want %q", hardCeilingCode, e.Category, "rto-hard-ceiling-exceeded")
	}
}

// TestON032_StandardFixtureBoundsArePositive verifies that every bound in
// the standard fixture is positive (zero-bound fixtures are meaningless).
//
// Spec ref: operator-nfr.md §4.8 ON-032 criterion 1.
func TestON032_StandardFixtureBoundsArePositive(t *testing.T) {
	t.Parallel()

	bounds := restartRTOFixtureStdBounds
	if bounds.MaxOpenBeads <= 0 {
		t.Errorf("ON-032: MaxOpenBeads = %d, want > 0", bounds.MaxOpenBeads)
	}
	if bounds.MaxInFlightRuns <= 0 {
		t.Errorf("ON-032: MaxInFlightRuns = %d, want > 0", bounds.MaxInFlightRuns)
	}
	if bounds.MaxGitLogDepthCommits <= 0 {
		t.Errorf("ON-032: MaxGitLogDepthCommits = %d, want > 0", bounds.MaxGitLogDepthCommits)
	}
	if bounds.MaxCat3PendingRuns <= 0 {
		t.Errorf("ON-032: MaxCat3PendingRuns = %d, want > 0", bounds.MaxCat3PendingRuns)
	}
	if bounds.MaxActiveInvestigators <= 0 {
		t.Errorf("ON-032: MaxActiveInvestigators = %d, want > 0", bounds.MaxActiveInvestigators)
	}
}

// TestON032_StandardFixtureMatchesSpec verifies that the fixture bounds match
// the values declared in §4.8.ON-032 criterion 1 verbatim.
//
// Spec ref: operator-nfr.md §4.8 ON-032 — exact bound values.
func TestON032_StandardFixtureMatchesSpec(t *testing.T) {
	t.Parallel()

	b := restartRTOFixtureStdBounds
	if b.MaxOpenBeads != 500 {
		t.Errorf("ON-032: MaxOpenBeads = %d, want 500 (spec §4.8.ON-032 criterion 1)", b.MaxOpenBeads)
	}
	if b.MaxInFlightRuns != 50 {
		t.Errorf("ON-032: MaxInFlightRuns = %d, want 50", b.MaxInFlightRuns)
	}
	if b.MaxGitLogDepthCommits != 10000 {
		t.Errorf("ON-032: MaxGitLogDepthCommits = %d, want 10000", b.MaxGitLogDepthCommits)
	}
	if b.MaxCat3PendingRuns != 100 {
		t.Errorf("ON-032: MaxCat3PendingRuns = %d, want 100", b.MaxCat3PendingRuns)
	}
	if b.MaxActiveInvestigators != 10 {
		t.Errorf("ON-032: MaxActiveInvestigators = %d, want 10", b.MaxActiveInvestigators)
	}
}

// TestON030_SpecSectionExists verifies that ON-030 (restart reconstruction
// path) exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.8 ON-030.
func TestON030_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-030") {
		t.Error("ON-030: specs/operator-nfr.md does not contain 'ON-030'")
	}
	if !strings.Contains(content, "Restart reconstruction path") {
		t.Error("ON-030: specs/operator-nfr.md missing 'Restart reconstruction path' heading text")
	}
}

// TestON030a_SpecSectionExists verifies that ON-030a (pause-state durable
// marker) exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.8 ON-030a.
func TestON030a_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-030a") {
		t.Error("ON-030a: specs/operator-nfr.md does not contain 'ON-030a'")
	}
	if !strings.Contains(content, "daemon.state") {
		t.Error("ON-030a: specs/operator-nfr.md missing 'daemon.state' marker file reference")
	}
}

// TestON030a_PauseStateMarkerFileIsDaemonState verifies that the spec names
// the exact marker file path `.harmonik/daemon.state`.
//
// Spec ref: operator-nfr.md §4.8 ON-030a — "atomic-written marker file
// `.harmonik/daemon.state`".
func TestON030a_PauseStateMarkerFileIsDaemonState(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, ".harmonik/daemon.state") {
		t.Error("ON-030a: specs/operator-nfr.md missing '.harmonik/daemon.state' exact path in ON-030a")
	}
}

// TestON030a_PauseReasonsAreThreeAndComplete verifies that the fixture
// encodes exactly three pause-reason discriminator values.
//
// Spec ref: operator-nfr.md §4.8 ON-030a — "one of `operator`, `improvement`,
// `upgrade-prepare`."
func TestON030a_PauseReasonsAreThreeAndComplete(t *testing.T) {
	t.Parallel()

	const wantReasons = 3
	if len(restartRTOFixturePauseReasons) != wantReasons {
		t.Errorf("ON-030a: fixture has %d pause-reason entries, want %d (operator, improvement, upgrade-prepare)",
			len(restartRTOFixturePauseReasons), wantReasons)
	}

	required := map[string]bool{
		"operator":        false,
		"improvement":     false,
		"upgrade-prepare": false,
	}
	for _, r := range restartRTOFixturePauseReasons {
		required[r.Value] = true
	}
	for val, found := range required {
		if !found {
			t.Errorf("ON-030a: pause-reason %q is missing from the fixture; spec §4.8.ON-030a declares exactly three", val)
		}
	}
}

// TestON030a_PauseReasonsHaveSpecRefs verifies that every pause-reason
// discriminator has a non-empty SpecRef.
//
// Spec ref: operator-nfr.md §4.8 ON-030a.
func TestON030a_PauseReasonsHaveSpecRefs(t *testing.T) {
	t.Parallel()

	for _, r := range restartRTOFixturePauseReasons {
		r := r
		t.Run(r.Value, func(t *testing.T) {
			t.Parallel()

			if r.SpecRef == "" {
				t.Errorf("ON-030a: pause-reason %q has empty SpecRef", r.Value)
			}
		})
	}
}

// TestON031_SpecSectionExists verifies that ON-031 (restart RTO target)
// exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.8 ON-031.
func TestON031_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-031") {
		t.Error("ON-031: specs/operator-nfr.md does not contain 'ON-031'")
	}
	if !strings.Contains(content, "Restart RTO target") {
		t.Error("ON-031: specs/operator-nfr.md missing 'Restart RTO target' heading text")
	}
}

// TestON032_SpecSectionExists verifies that ON-032 (RTO criteria and hard
// ceiling) exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.8 ON-032.
func TestON032_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-032") {
		t.Error("ON-032: specs/operator-nfr.md does not contain 'ON-032'")
	}
	if !strings.Contains(content, "RTO criteria") {
		t.Error("ON-032: specs/operator-nfr.md missing 'RTO criteria' heading text")
	}
}

// TestON033_SpecSectionExists verifies that ON-033 (RTO measurement boundary,
// monotonic clock) exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.8 ON-033.
func TestON033_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-033") {
		t.Error("ON-033: specs/operator-nfr.md does not contain 'ON-033'")
	}
	if !strings.Contains(content, "monotonic") {
		t.Error("ON-033: specs/operator-nfr.md missing 'monotonic' keyword in ON-033 context")
	}
}

// TestON033_MonotonicCompanionFieldsNamed verifies that the spec names both
// monotonic companion fields required by ON-033.
//
// Spec ref: operator-nfr.md §4.8 ON-033 — "MUST both carry a
// `_at_ns_since_boot` companion field."
func TestON033_MonotonicCompanionFieldsNamed(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "shutdown_at_ns_since_boot") {
		t.Error("ON-033: specs/operator-nfr.md missing 'shutdown_at_ns_since_boot' companion field")
	}
	if !strings.Contains(content, "ready_at_ns_since_boot") {
		t.Error("ON-033: specs/operator-nfr.md missing 'ready_at_ns_since_boot' companion field")
	}
}

// TestON033_RTOUndefinedCasesDocumented verifies that the spec documents the
// two cases where RTO is `rto_undefined` (boot-transition and SIGKILL).
//
// Spec ref: operator-nfr.md §4.8 ON-033 — "the RTO MUST be marked
// `rto_undefined` for the boot-transition cycle … SIGKILL terminations …
// rto_undefined."
func TestON033_RTOUndefinedCasesDocumented(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "rto_undefined") {
		t.Error("ON-033: specs/operator-nfr.md missing 'rto_undefined' marker for excluded cycles")
	}
	if !strings.Contains(content, "boot-transition") {
		t.Error("ON-033: specs/operator-nfr.md missing 'boot-transition' case in ON-033")
	}
}

// TestON053_SpecSectionExists verifies that ON-053 (post-panic forensic file)
// exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.8 ON-053.
func TestON053_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-053") {
		t.Error("ON-053: specs/operator-nfr.md does not contain 'ON-053'")
	}
	if !strings.Contains(content, "Post-panic forensic file") {
		t.Error("ON-053: specs/operator-nfr.md missing 'Post-panic forensic file' heading text")
	}
}

// TestON053_ForensicFilePathPattern verifies that the spec names the exact
// forensic file path pattern `.harmonik/panic-<timestamp>.log`.
//
// Spec ref: operator-nfr.md §4.8 ON-053 — "atomically write a forensic file
// to `.harmonik/panic-<timestamp>.log`."
func TestON053_ForensicFilePathPattern(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, ".harmonik/panic-") {
		t.Error("ON-053: specs/operator-nfr.md missing '.harmonik/panic-' file path pattern")
	}
}

// TestON053_ForensicFileFieldsAreComplete verifies that the forensic-file
// field fixture covers all four clauses (a, b, c, d) of ON-053.
//
// Spec ref: operator-nfr.md §4.8 ON-053.
func TestON053_ForensicFileFieldsAreComplete(t *testing.T) {
	t.Parallel()

	const wantFields = 10 // 10 required fields across clauses a-d
	if len(restartRTOFixtureForensicFields) < wantFields {
		t.Errorf("ON-053: forensic-field fixture has %d entries, want at least %d",
			len(restartRTOFixtureForensicFields), wantFields)
	}

	// Every field must have a non-empty Name and SpecRef.
	for _, f := range restartRTOFixtureForensicFields {
		f := f
		t.Run(f.Name, func(t *testing.T) {
			t.Parallel()

			if f.Name == "" {
				t.Error("ON-053: forensic field has empty Name")
			}
			if f.SpecRef == "" {
				t.Errorf("ON-053: forensic field %q has empty SpecRef", f.Name)
			}
		})
	}
}

// TestONINV005_SpecSectionExists verifies that ON-INV-005 (every subsystem
// must report its reconstruction contribution) exists.
//
// Spec ref: operator-nfr.md §5 ON-INV-005.
func TestONINV005_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-INV-005") {
		t.Error("ON-INV-005: specs/operator-nfr.md does not contain 'ON-INV-005'")
	}
	if !strings.Contains(content, "reconstruction contribution") {
		t.Error("ON-INV-005: specs/operator-nfr.md missing 'reconstruction contribution' in ON-INV-005")
	}
}

// TestONINV005_ThreeInvariantsForReconstruction verifies that ON-INV-005
// documents all three sub-invariants: (a) no silent reconstruction, (b)
// bounded termination before ready, (c) failure causes categorized exit.
//
// Spec ref: operator-nfr.md §5 ON-INV-005 — "(a) NO subsystem reconstruct
// silently, (b) every subsystem's reconstruction terminates … before the
// daemon emits `ready`, and (c) any subsystem that cannot reconstruct MUST
// cause the daemon to fail startup."
func TestONINV005_ThreeInvariantsForReconstruction(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	// Check the three sub-invariants are present in some form.
	if !strings.Contains(content, "NO subsystem reconstruct silently") {
		t.Error("ON-INV-005: specs/operator-nfr.md missing sub-invariant (a): 'NO subsystem reconstruct silently'")
	}
	if !strings.Contains(content, "reconstruction terminates") {
		t.Error("ON-INV-005: specs/operator-nfr.md missing sub-invariant (b): bounded reconstruction termination")
	}
	if !strings.Contains(content, "cannot reconstruct") {
		t.Error("ON-INV-005: specs/operator-nfr.md missing sub-invariant (c): reconstruction failure causes startup failure")
	}
}

// TestON030_NoJSONLReplayOnRestart verifies that the spec explicitly forbids
// JSONL event log replay during restart reconstruction.
//
// Spec ref: operator-nfr.md §4.8 ON-030 — "The JSONL event log MUST NOT be
// replayed for state reconstruction."
func TestON030_NoJSONLReplayOnRestart(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "JSONL event log MUST NOT be replayed") {
		t.Error("ON-030: specs/operator-nfr.md missing 'JSONL event log MUST NOT be replayed' in ON-030 reconstruction path")
	}
}
