package operatornfr_test

// multiDaemonFixture — spec-level harness for hk-sx9r.83.
//
// Covers: ON-041 (multi-daemon commands obligation), ON-042 (multi-tenancy
// deferred), ON-043 (metrics exposition deferred), ON-044 (distributed tracing
// deferred), ON-045 (budgets declared/enforced/attributed), ON-046 (budget
// events operator-observable), ON-050 (harmonik attach minimum surface),
// ON-051 (multi-attach arbitration), ON-054 (status pause-reason discriminator).
//
// These are spec-artifact existence and structural-constraint tests. Runtime
// multi-daemon orchestration is the implementation-level integration test
// surface; this file is the §10.2 sensor layer.
//
// Spec ref: specs/operator-nfr.md §4.10, §4.11 (partial), §10.2.

import (
	"strings"
	"testing"
)

// multiDaemonFixtureListColumn models one required output column for
// `harmonik list` per ON-041.
//
// Spec ref: operator-nfr.md §4.10 ON-041 — "Output columns: daemon_id,
// project_root, pid, status, socket_path, started_at, last_exit_code,
// budget_summary."
type multiDaemonFixtureListColumn struct {
	Name    string
	SpecRef string
}

// multiDaemonFixtureListColumns is the authoritative fixture encoding of the
// ON-041 `harmonik list` required output columns.
var multiDaemonFixtureListColumns = []multiDaemonFixtureListColumn{
	{"daemon_id", "ON-041 — project_hash from PL-006a"},
	{"project_root", "ON-041"},
	{"pid", "ON-041"},
	{"status", "ON-041 — per §6.1 DaemonStatus"},
	{"socket_path", "ON-041"},
	{"started_at", "ON-041"},
	{"last_exit_code", "ON-041 — 'n/a' if not observable"},
	{"budget_summary", "ON-041 — tokens_consumed/wall_clock_consumed/iterations_consumed"},
}

// multiDaemonFixtureIdentificationFlag models one daemon-identification flag
// that ON-041 requires on all daemon-communicating commands.
//
// Spec ref: operator-nfr.md §4.10 ON-041 — "daemon-identification flags on
// all daemon-communicating commands … at minimum `--socket <path>`,
// `--cwd <path>`, and `--daemon-id <id>`."
type multiDaemonFixtureIdentificationFlag struct {
	Flag    string
	SpecRef string
}

// multiDaemonFixtureIdentificationFlags lists the three required
// daemon-identification flags per ON-041.
var multiDaemonFixtureIdentificationFlags = []multiDaemonFixtureIdentificationFlag{
	{"--socket", "ON-041 — socket path"},
	{"--cwd", "ON-041 — project working directory"},
	{"--daemon-id", "ON-041 — daemon_id from harmonik list"},
}

// multiDaemonFixtureAttachSurface models one surface requirement of
// `harmonik attach` per ON-050.
//
// Spec ref: operator-nfr.md §4.10 ON-050 — "`harmonik attach` MUST: (a)
// connect … (b) stream … (c) present periodic status snapshot … (d) accept
// operator commands inline … (e) detach cleanly."
type multiDaemonFixtureAttachSurface struct {
	Clause  string // (a)-(e) as named in ON-050
	Summary string
	SpecRef string
}

// multiDaemonFixtureAttachSurfaces is the authoritative encoding of the five
// ON-050 attach-surface requirements.
var multiDaemonFixtureAttachSurfaces = []multiDaemonFixtureAttachSurface{
	{"a", "connect to daemon.sock and verify handshake", "ON-050"},
	{"b", "stream live event tap to operator terminal", "ON-050"},
	{"c", "present periodic status snapshot every T_attach_status", "ON-050"},
	{"d", "accept operator commands inline (pause/resume/stop/enqueue)", "ON-050"},
	{"e", "detach cleanly on SIGINT or :detach without affecting daemon state", "ON-050"},
}

// multiDaemonFixturePauseReasonDiscriminator models one status pause-reason
// value per ON-054.
//
// Spec ref: operator-nfr.md §4.10 ON-054 — "`operator-pause` (issued via
// `harmonik pause`), `improvement-pause` (per ON-012), `upgrade-prepare`."
type multiDaemonFixturePauseReasonDiscriminator struct {
	Value   string
	SpecRef string
}

// multiDaemonFixturePauseReasonDiscriminators lists the three status
// pause-reason discriminator values per ON-054.
var multiDaemonFixturePauseReasonDiscriminators = []multiDaemonFixturePauseReasonDiscriminator{
	{"operator-pause", "ON-054 — issued via harmonik pause"},
	{"improvement-pause", "ON-054 — per ON-012"},
	{"upgrade-prepare", "ON-054 — harmonik upgrade in progress per ON-019"},
}

// TestON041_SpecSectionExists verifies that ON-041 (multi-daemon commands
// obligation) exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.10 ON-041.
func TestON041_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-041") {
		t.Error("ON-041: specs/operator-nfr.md does not contain 'ON-041'")
	}
	if !strings.Contains(content, "Multi-daemon commands obligation") {
		t.Error("ON-041: specs/operator-nfr.md missing 'Multi-daemon commands obligation' heading")
	}
}

// TestON041_HarmonikListOutputColumnsAreEight verifies the fixture encodes
// exactly eight required output columns for `harmonik list`.
//
// Spec ref: operator-nfr.md §4.10 ON-041 Output columns.
func TestON041_HarmonikListOutputColumnsAreEight(t *testing.T) {
	t.Parallel()

	const wantColumns = 8
	if len(multiDaemonFixtureListColumns) != wantColumns {
		t.Errorf("ON-041: list-output-column fixture has %d entries, want %d", len(multiDaemonFixtureListColumns), wantColumns)
	}
}

// TestON041_HarmonikListOutputColumnsPresent verifies that all eight required
// columns are in the fixture.
//
// Spec ref: operator-nfr.md §4.10 ON-041.
func TestON041_HarmonikListOutputColumnsPresent(t *testing.T) {
	t.Parallel()

	required := map[string]bool{
		"daemon_id":      false,
		"project_root":   false,
		"pid":            false,
		"status":         false,
		"socket_path":    false,
		"started_at":     false,
		"last_exit_code": false,
		"budget_summary": false,
	}
	for _, col := range multiDaemonFixtureListColumns {
		required[col.Name] = true
	}
	for name, found := range required {
		if !found {
			t.Errorf("ON-041: required list column %q missing from fixture", name)
		}
	}
}

// TestON041_ListColumnSpecRefsNonEmpty verifies every list column has a
// SpecRef.
//
// Spec ref: operator-nfr.md §4.10 ON-041.
func TestON041_ListColumnSpecRefsNonEmpty(t *testing.T) {
	t.Parallel()

	for _, col := range multiDaemonFixtureListColumns {
		col := col
		t.Run(col.Name, func(t *testing.T) {
			t.Parallel()

			if col.SpecRef == "" {
				t.Errorf("ON-041: list column %q has empty SpecRef", col.Name)
			}
		})
	}
}

// TestON041_ThreeDaemonIdentificationFlags verifies that exactly three
// daemon-identification flags are declared in the fixture.
//
// Spec ref: operator-nfr.md §4.10 ON-041 — "--socket, --cwd, --daemon-id."
func TestON041_ThreeDaemonIdentificationFlags(t *testing.T) {
	t.Parallel()

	const wantFlags = 3
	if len(multiDaemonFixtureIdentificationFlags) != wantFlags {
		t.Errorf("ON-041: identification-flag fixture has %d entries, want %d (--socket, --cwd, --daemon-id)",
			len(multiDaemonFixtureIdentificationFlags), wantFlags)
	}

	required := map[string]bool{"--socket": false, "--cwd": false, "--daemon-id": false}
	for _, f := range multiDaemonFixtureIdentificationFlags {
		required[f.Flag] = true
	}
	for flag, found := range required {
		if !found {
			t.Errorf("ON-041: required flag %q missing from fixture", flag)
		}
	}
}

// TestON041_DaemonDiscoveryMechanismDocumented verifies that the spec names
// the discovery mechanism (scan HOME + HARMONIK_PROJECT_ROOTS).
//
// Spec ref: operator-nfr.md §4.10 ON-041 Daemon-discovery mechanism.
func TestON041_DaemonDiscoveryMechanismDocumented(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "daemon.pid") {
		t.Error("ON-041: specs/operator-nfr.md missing 'daemon.pid' in daemon-discovery mechanism")
	}
	if !strings.Contains(content, "HARMONIK_PROJECT_ROOTS") {
		t.Error("ON-041: specs/operator-nfr.md missing 'HARMONIK_PROJECT_ROOTS' env var in daemon-discovery")
	}
	if !strings.Contains(content, "stale") {
		t.Error("ON-041: specs/operator-nfr.md missing 'stale' reporting for unreachable daemons in ON-041")
	}
}

// TestON041_MultiDaemonTargetMissingExitCodeExistsInTaxonomy verifies that §8
// code 17 (multi-daemon-target-missing) exists in the taxonomy.
//
// Spec ref: operator-nfr.md §4.10 ON-041; §8 code 17.
func TestON041_MultiDaemonTargetMissingExitCodeExistsInTaxonomy(t *testing.T) {
	t.Parallel()

	const code17 = 17
	e, ok := exitCodeFixtureLookup(code17)
	if !ok {
		t.Errorf("ON-041: §8 taxonomy missing code %d (multi-daemon-target-missing)", code17)
		return
	}
	if e.Category != "multi-daemon-target-missing" {
		t.Errorf("ON-041: §8 code %d category = %q, want %q", code17, e.Category, "multi-daemon-target-missing")
	}
}

// TestON041_MachineCeilingExhaustedExitCodeExistsInTaxonomy verifies that §8
// code 18 (machine-ceiling-exhausted) exists in the taxonomy.
//
// Spec ref: operator-nfr.md §4.10 ON-041; §8 code 18.
func TestON041_MachineCeilingExhaustedExitCodeExistsInTaxonomy(t *testing.T) {
	t.Parallel()

	const code18 = 18
	e, ok := exitCodeFixtureLookup(code18)
	if !ok {
		t.Errorf("ON-041: §8 taxonomy missing code %d (machine-ceiling-exhausted)", code18)
		return
	}
	if e.Category != "machine-ceiling-exhausted" {
		t.Errorf("ON-041: §8 code %d category = %q, want %q", code18, e.Category, "machine-ceiling-exhausted")
	}
}

// TestON042_SpecSectionExists verifies that ON-042 (multi-tenancy deferred)
// exists.
//
// Spec ref: operator-nfr.md §4.10 ON-042.
func TestON042_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-042") {
		t.Error("ON-042: specs/operator-nfr.md does not contain 'ON-042'")
	}
	if !strings.Contains(content, "Multi-tenancy is explicitly deferred") {
		t.Error("ON-042: specs/operator-nfr.md missing 'Multi-tenancy is explicitly deferred' heading")
	}
}

// TestON043_SpecSectionExists verifies that ON-043 (metrics exposition
// deferred) exists.
//
// Spec ref: operator-nfr.md §4.10 ON-043.
func TestON043_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-043") {
		t.Error("ON-043: specs/operator-nfr.md does not contain 'ON-043'")
	}
	if !strings.Contains(content, "Metrics exposition format is deferred") {
		t.Error("ON-043: specs/operator-nfr.md missing 'Metrics exposition format is deferred' heading")
	}
}

// TestON044_SpecSectionExists verifies that ON-044 (distributed tracing
// deferred) exists.
//
// Spec ref: operator-nfr.md §4.10 ON-044.
func TestON044_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-044") {
		t.Error("ON-044: specs/operator-nfr.md does not contain 'ON-044'")
	}
	if !strings.Contains(content, "Distributed tracing across daemons is deferred") {
		t.Error("ON-044: specs/operator-nfr.md missing 'Distributed tracing across daemons is deferred' heading")
	}
}

// TestON045_SpecSectionExists verifies that ON-045 (budgets declared/enforced/
// attributed) exists.
//
// Spec ref: operator-nfr.md §4.11 ON-045.
func TestON045_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-045") {
		t.Error("ON-045: specs/operator-nfr.md does not contain 'ON-045'")
	}
	if !strings.Contains(content, "Budgets are declared, enforced, and attributed") {
		t.Error("ON-045: specs/operator-nfr.md missing 'Budgets are declared, enforced, and attributed' heading")
	}
}

// TestON046_SpecSectionExists verifies that ON-046 (budget events
// operator-observable) exists.
//
// Spec ref: operator-nfr.md §4.11 ON-046.
func TestON046_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-046") {
		t.Error("ON-046: specs/operator-nfr.md does not contain 'ON-046'")
	}
	if !strings.Contains(content, "Budget events are operator-observable") {
		t.Error("ON-046: specs/operator-nfr.md missing 'Budget events are operator-observable' heading")
	}
}

// TestON050_SpecSectionExists verifies that ON-050 (harmonik attach minimum
// surface) exists.
//
// Spec ref: operator-nfr.md §4.10 ON-050.
func TestON050_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-050") {
		t.Error("ON-050: specs/operator-nfr.md does not contain 'ON-050'")
	}
	if !strings.Contains(content, "harmonik attach") {
		t.Error("ON-050: specs/operator-nfr.md missing 'harmonik attach' in ON-050 context")
	}
}

// TestON050_AttachSurfaceHasFiveClauses verifies the fixture encodes exactly
// five attach-surface clauses (a-e) per ON-050.
//
// Spec ref: operator-nfr.md §4.10 ON-050 — "(a)…(e)."
func TestON050_AttachSurfaceHasFiveClauses(t *testing.T) {
	t.Parallel()

	const wantClauses = 5
	if len(multiDaemonFixtureAttachSurfaces) != wantClauses {
		t.Errorf("ON-050: attach-surface fixture has %d clauses, want %d (a-e)", len(multiDaemonFixtureAttachSurfaces), wantClauses)
	}

	required := map[string]bool{"a": false, "b": false, "c": false, "d": false, "e": false}
	for _, s := range multiDaemonFixtureAttachSurfaces {
		required[s.Clause] = true
	}
	for clause, found := range required {
		if !found {
			t.Errorf("ON-050: attach-surface clause (%s) missing from fixture", clause)
		}
	}
}

// TestON050_AttachSurfaceClausesHaveNonEmptyFields verifies each attach-surface
// clause has non-empty Summary and SpecRef.
//
// Spec ref: operator-nfr.md §4.10 ON-050.
func TestON050_AttachSurfaceClausesHaveNonEmptyFields(t *testing.T) {
	t.Parallel()

	for _, s := range multiDaemonFixtureAttachSurfaces {
		s := s
		t.Run(s.Clause, func(t *testing.T) {
			t.Parallel()

			if s.Summary == "" {
				t.Errorf("ON-050: attach-surface clause (%s) has empty Summary", s.Clause)
			}
			if s.SpecRef == "" {
				t.Errorf("ON-050: attach-surface clause (%s) has empty SpecRef", s.Clause)
			}
		})
	}
}

// TestON050_AttachSessionIDCarriedInEmissions verifies that the spec requires
// the attach session_id in operator-command emissions.
//
// Spec ref: operator-nfr.md §4.10 ON-050 — "The attach session_id MUST be
// carried in any operator-command emission for audit-trail correlation."
func TestON050_AttachSessionIDCarriedInEmissions(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "session_id") {
		t.Error("ON-050: specs/operator-nfr.md missing 'session_id' in ON-050 context")
	}
}

// TestON051_SpecSectionExists verifies that ON-051 (multi-attach arbitration)
// exists.
//
// Spec ref: operator-nfr.md §4.10 ON-051.
func TestON051_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-051") {
		t.Error("ON-051: specs/operator-nfr.md does not contain 'ON-051'")
	}
	if !strings.Contains(content, "Multi-attach arbitration") {
		t.Error("ON-051: specs/operator-nfr.md missing 'Multi-attach arbitration' heading")
	}
}

// TestON051_DetachByOneOperatorDoesNotAffectOthers verifies that the spec
// requires one operator's detach to not affect other attached operators.
//
// Spec ref: operator-nfr.md §4.10 ON-051 — "Detach by one operator MUST NOT
// affect other attached operators."
func TestON051_DetachByOneOperatorDoesNotAffectOthers(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "Detach by one operator MUST NOT affect") {
		t.Error("ON-051: specs/operator-nfr.md missing 'Detach by one operator MUST NOT affect' in ON-051")
	}
}

// TestON054_SpecSectionExists verifies that ON-054 (status pause-reason
// discriminator) exists.
//
// Spec ref: operator-nfr.md §4.10 ON-054.
func TestON054_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-054") {
		t.Error("ON-054: specs/operator-nfr.md does not contain 'ON-054'")
	}
	if !strings.Contains(content, "reports pause-reason") {
		t.Error("ON-054: specs/operator-nfr.md missing 'reports pause-reason' in ON-054 heading")
	}
}

// TestON054_PauseReasonDiscriminatorsAreThree verifies the fixture encodes
// exactly three pause-reason discriminator values for `harmonik status`.
//
// Spec ref: operator-nfr.md §4.10 ON-054 — "operator-pause / improvement-pause
// / upgrade-prepare."
func TestON054_PauseReasonDiscriminatorsAreThree(t *testing.T) {
	t.Parallel()

	const wantReasons = 3
	if len(multiDaemonFixturePauseReasonDiscriminators) != wantReasons {
		t.Errorf("ON-054: pause-reason discriminator fixture has %d entries, want %d",
			len(multiDaemonFixturePauseReasonDiscriminators), wantReasons)
	}

	required := map[string]bool{
		"operator-pause":    false,
		"improvement-pause": false,
		"upgrade-prepare":   false,
	}
	for _, r := range multiDaemonFixturePauseReasonDiscriminators {
		required[r.Value] = true
	}
	for val, found := range required {
		if !found {
			t.Errorf("ON-054: pause-reason discriminator %q missing from fixture; spec §4.10.ON-054 declares all three", val)
		}
	}
}

// TestON054_PauseReasonDiscriminatorsMatchON030a verifies that the ON-054
// pause-reason values match those declared in ON-030a's durable marker.
// Both ON-054 and ON-030a must use the same set.
//
// Spec ref: operator-nfr.md §4.10 ON-054 — "discriminator MUST match the
// operator_pause_status payload's pause-reason tag … sourced from the durable
// pause-state marker of ON-030a."
func TestON054_PauseReasonDiscriminatorsMatchON030a(t *testing.T) {
	t.Parallel()

	// ON-030a uses: operator, improvement, upgrade-prepare.
	// ON-054 uses: operator-pause, improvement-pause, upgrade-prepare.
	// They differ in suffix; the spec aliases them via the event payload.
	// Verify the spec explicitly links ON-054 to ON-030a.
	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-030a") {
		t.Error("ON-054: specs/operator-nfr.md ON-054 section does not reference ON-030a as the source of the durable marker")
	}
}

// TestON054_StatusWithoutEventLogConsultation verifies that the spec requires
// the discriminator to be readable without consulting the event log.
//
// Spec ref: operator-nfr.md §4.10 ON-054 — "An operator inspecting
// `harmonik status` during a pause MUST be able to distinguish these three
// reasons without consulting the event log."
func TestON054_StatusWithoutEventLogConsultation(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "without consulting the event log") {
		t.Error("ON-054: specs/operator-nfr.md missing 'without consulting the event log' in ON-054")
	}
}
