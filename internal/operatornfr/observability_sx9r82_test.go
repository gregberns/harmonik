package operatornfr_test

// observabilityFixture — spec-level harness for hk-sx9r.82.
//
// Covers: ON-034 (every subsystem emits typed events), ON-035 (every subsystem
// emits structured logs), ON-036 (health-check interface), ON-037 (liveness
// heartbeats), ON-038 (audit records are a subset of traces), ON-039 (all
// observability operations are mechanism-tagged), ON-040 (silent-hang
// detection obligation + drain-forced synthesis).
//
// These are spec-artifact existence and structural-constraint tests. Runtime
// conformance (per-subsystem emission verification) is the implementation-level
// integration test surface; this file is the §10.2 sensor layer.
//
// Spec ref: specs/operator-nfr.md §4.9, §10.2.

import (
	"strings"
	"testing"
)

// observabilityFixtureHealthStatus enumerates the three valid health-check
// status values per ON-036.
//
// Spec ref: operator-nfr.md §4.9 ON-036 — "health_status ∈ {OK, degraded,
// failed}."
type observabilityFixtureHealthStatus struct {
	Value   string
	SpecRef string
}

// observabilityFixtureHealthStatuses is the authoritative fixture encoding of
// the three health-status values per §4.9.ON-036.
var observabilityFixtureHealthStatuses = []observabilityFixtureHealthStatus{
	{"OK", "operator-nfr.md §4.9 ON-036"},
	{"degraded", "operator-nfr.md §4.9 ON-036"},
	{"failed", "operator-nfr.md §4.9 ON-036"},
}

// observabilityFixtureLogLevel enumerates the four valid structured-log level
// values per ON-035.
//
// Spec ref: operator-nfr.md §4.9 ON-035 — "`level` ∈ `{debug, info, warn,
// error}`."
type observabilityFixtureLogLevel struct {
	Value   string
	SpecRef string
}

// observabilityFixtureLogLevels is the authoritative fixture encoding of the
// four log-level values per §4.9.ON-035.
var observabilityFixtureLogLevels = []observabilityFixtureLogLevel{
	{"debug", "operator-nfr.md §4.9 ON-035"},
	{"info", "operator-nfr.md §4.9 ON-035"},
	{"warn", "operator-nfr.md §4.9 ON-035"},
	{"error", "operator-nfr.md §4.9 ON-035"},
}

// observabilityFixtureStructuredLogField models one required field in the
// ON-035 structured-log wire format.
//
// Spec ref: operator-nfr.md §4.9 ON-035 — "minimum structured-log shape …
// carrying the fields: ts, log_schema_version, level, subsystem,
// source_subsystem, run_id?, node_id?, event_id?, msg, fields."
type observabilityFixtureStructuredLogField struct {
	Name     string // JSON key name
	Optional bool   // true for fields marked "?" in the spec
	SpecRef  string
}

// observabilityFixtureStructuredLogFields is the authoritative fixture
// encoding of the ON-035 structured-log minimum shape.
var observabilityFixtureStructuredLogFields = []observabilityFixtureStructuredLogField{
	{"ts", false, "ON-035 — RFC 3339 with ms"},
	{"log_schema_version", false, "ON-035 — current '1.0'"},
	{"level", false, "ON-035 — {debug,info,warn,error}"},
	{"subsystem", false, "ON-035 — owning subsystem name"},
	{"source_subsystem", false, "ON-035 — per event-model.md §4.9 EV-034a"},
	{"run_id", true, "ON-035 — optional run correlation"},
	{"node_id", true, "ON-035 — optional node correlation"},
	{"event_id", true, "ON-035 — UUIDv7 when log emits a tracked event"},
	{"msg", false, "ON-035 — short human-readable"},
	{"fields", false, "ON-035 — map of typed values"},
}

// observabilityFixtureSilentHangReason enumerates the silent-hang reason
// values that ON-040 declares.
//
// Spec ref: operator-nfr.md §4.9 ON-040 — "agent_warning_silent_hang{
// reason=drain_forced, run_id, node_id}."
type observabilityFixtureSilentHangReason struct {
	Value   string
	SpecRef string
}

// observabilityFixtureSilentHangReasons lists the declared silent-hang reason
// values. The "drain_forced" reason is the one introduced by ON-040's
// drain-timeout synthesis clause.
var observabilityFixtureSilentHangReasons = []observabilityFixtureSilentHangReason{
	{"drain_forced", "operator-nfr.md §4.9 ON-040 drain-timeout SIGKILL synthesis"},
}

// TestON034_SpecSectionExists verifies that ON-034 (every subsystem emits
// typed events) exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.9 ON-034.
func TestON034_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-034") {
		t.Error("ON-034: specs/operator-nfr.md does not contain 'ON-034'")
	}
	if !strings.Contains(content, "Every subsystem emits typed events") {
		t.Error("ON-034: specs/operator-nfr.md missing 'Every subsystem emits typed events' heading")
	}
}

// TestON035_SpecSectionExists verifies that ON-035 (every subsystem emits
// structured logs) exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.9 ON-035.
func TestON035_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-035") {
		t.Error("ON-035: specs/operator-nfr.md does not contain 'ON-035'")
	}
	if !strings.Contains(content, "Every subsystem emits structured logs") {
		t.Error("ON-035: specs/operator-nfr.md missing 'Every subsystem emits structured logs' heading")
	}
}

// TestON035_StructuredLogSchemaVersion verifies that ON-035 names the current
// log_schema_version value "1.0".
//
// Spec ref: operator-nfr.md §4.9 ON-035 — "`log_schema_version` (string,
// current `\"1.0\"`)."
func TestON035_StructuredLogSchemaVersion(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, `"1.0"`) {
		t.Error("ON-035: specs/operator-nfr.md missing log_schema_version value '1.0'")
	}
}

// TestON035_StructuredLogRotationPolicy verifies that ON-035 documents the
// log rotation policy (100 MiB or 24 hours).
//
// Spec ref: operator-nfr.md §4.9 ON-035 — "Log files MUST rotate at 100 MiB
// or 24 hours (whichever comes first)."
func TestON035_StructuredLogRotationPolicy(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "100 MiB") {
		t.Error("ON-035: specs/operator-nfr.md missing '100 MiB' rotation policy")
	}
	if !strings.Contains(content, "24 hours") {
		t.Error("ON-035: specs/operator-nfr.md missing '24 hours' rotation policy")
	}
}

// TestON035_LogLevelSetIsComplete verifies the fixture log-level set has
// exactly four values as declared in ON-035.
//
// Spec ref: operator-nfr.md §4.9 ON-035 — "`level` ∈ `{debug, info, warn,
// error}`."
func TestON035_LogLevelSetIsComplete(t *testing.T) {
	t.Parallel()

	const wantLevels = 4
	if len(observabilityFixtureLogLevels) != wantLevels {
		t.Errorf("ON-035: log-level fixture has %d entries, want %d", len(observabilityFixtureLogLevels), wantLevels)
	}

	required := map[string]bool{
		"debug": false, "info": false, "warn": false, "error": false,
	}
	for _, l := range observabilityFixtureLogLevels {
		required[l.Value] = true
	}
	for val, found := range required {
		if !found {
			t.Errorf("ON-035: log level %q is missing from the fixture", val)
		}
	}
}

// TestON035_StructuredLogFieldsAreComplete verifies the fixture structured-log
// fields cover all ten fields declared in ON-035 (7 required + 3 optional).
//
// Spec ref: operator-nfr.md §4.9 ON-035.
func TestON035_StructuredLogFieldsAreComplete(t *testing.T) {
	t.Parallel()

	const wantFields = 10
	if len(observabilityFixtureStructuredLogFields) < wantFields {
		t.Errorf("ON-035: structured-log field fixture has %d entries, want at least %d",
			len(observabilityFixtureStructuredLogFields), wantFields)
	}

	for _, f := range observabilityFixtureStructuredLogFields {
		f := f
		t.Run(f.Name, func(t *testing.T) {
			t.Parallel()

			if f.Name == "" {
				t.Error("ON-035: structured-log field has empty Name")
			}
			if f.SpecRef == "" {
				t.Errorf("ON-035: structured-log field %q has empty SpecRef", f.Name)
			}
		})
	}
}

// TestON035_RequiredFieldsAreMarkedNonOptional verifies that the required
// fields (ts, log_schema_version, level, subsystem, source_subsystem, msg,
// fields) are NOT marked optional in the fixture.
//
// Spec ref: operator-nfr.md §4.9 ON-035 — required fields have no "?" marker.
func TestON035_RequiredFieldsAreMarkedNonOptional(t *testing.T) {
	t.Parallel()

	required := map[string]bool{
		"ts":                 true,
		"log_schema_version": true,
		"level":              true,
		"subsystem":          true,
		"source_subsystem":   true,
		"msg":                true,
		"fields":             true,
	}

	for _, f := range observabilityFixtureStructuredLogFields {
		f := f
		if !required[f.Name] {
			continue
		}
		t.Run(f.Name, func(t *testing.T) {
			t.Parallel()

			if f.Optional {
				t.Errorf("ON-035: structured-log field %q is marked Optional=true but should be required per the spec", f.Name)
			}
		})
	}
}

// TestON035_OptionalFieldsAreMarkedOptional verifies that run_id, node_id,
// and event_id are marked optional (they carry "?" in the spec).
//
// Spec ref: operator-nfr.md §4.9 ON-035 — "`run_id?`, `node_id?`, `event_id?`."
func TestON035_OptionalFieldsAreMarkedOptional(t *testing.T) {
	t.Parallel()

	optionalFields := map[string]bool{"run_id": true, "node_id": true, "event_id": true}

	for _, f := range observabilityFixtureStructuredLogFields {
		f := f
		if !optionalFields[f.Name] {
			continue
		}
		t.Run(f.Name, func(t *testing.T) {
			t.Parallel()

			if !f.Optional {
				t.Errorf("ON-035: structured-log field %q is marked Optional=false but the spec declares it optional (with '?')", f.Name)
			}
		})
	}
}

// TestON036_SpecSectionExists verifies that ON-036 (health-check interface)
// exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.9 ON-036.
func TestON036_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-036") {
		t.Error("ON-036: specs/operator-nfr.md does not contain 'ON-036'")
	}
	if !strings.Contains(content, "health-check interface") {
		t.Error("ON-036: specs/operator-nfr.md missing 'health-check interface' in ON-036")
	}
}

// TestON036_HealthStatusSetIsComplete verifies the fixture health-status set
// has exactly three values (OK, degraded, failed) as declared in ON-036.
//
// Spec ref: operator-nfr.md §4.9 ON-036 — "`health_status` ∈ {OK, degraded,
// failed}`."
func TestON036_HealthStatusSetIsComplete(t *testing.T) {
	t.Parallel()

	const wantStatuses = 3
	if len(observabilityFixtureHealthStatuses) != wantStatuses {
		t.Errorf("ON-036: health-status fixture has %d entries, want %d (OK, degraded, failed)",
			len(observabilityFixtureHealthStatuses), wantStatuses)
	}

	required := map[string]bool{"OK": false, "degraded": false, "failed": false}
	for _, s := range observabilityFixtureHealthStatuses {
		required[s.Value] = true
	}
	for val, found := range required {
		if !found {
			t.Errorf("ON-036: health-status %q is missing from the fixture", val)
		}
	}
}

// TestON036_HealthStatusValuesHaveSpecRefs verifies that every health-status
// value in the fixture has a non-empty SpecRef.
//
// Spec ref: operator-nfr.md §4.9 ON-036.
func TestON036_HealthStatusValuesHaveSpecRefs(t *testing.T) {
	t.Parallel()

	for _, s := range observabilityFixtureHealthStatuses {
		s := s
		t.Run(s.Value, func(t *testing.T) {
			t.Parallel()

			if s.SpecRef == "" {
				t.Errorf("ON-036: health-status %q has empty SpecRef", s.Value)
			}
		})
	}
}

// TestON037_SpecSectionExists verifies that ON-037 (liveness heartbeats)
// exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.9 ON-037.
func TestON037_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-037") {
		t.Error("ON-037: specs/operator-nfr.md does not contain 'ON-037'")
	}
	if !strings.Contains(content, "liveness heartbeat") {
		t.Error("ON-037: specs/operator-nfr.md missing 'liveness heartbeat' in ON-037")
	}
}

// TestON037_MissedHeartbeatTriggersDegraded verifies that the spec requires
// missed heartbeats to trigger a `degraded` classification.
//
// Spec ref: operator-nfr.md §4.9 ON-037 — "Missing heartbeats beyond tolerance
// MUST trigger a `degraded` classification."
func TestON037_MissedHeartbeatTriggersDegraded(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "Missing heartbeats") {
		t.Error("ON-037: specs/operator-nfr.md missing 'Missing heartbeats' consequence in ON-037")
	}
	if !strings.Contains(content, "degraded") {
		t.Error("ON-037: specs/operator-nfr.md missing 'degraded' classification consequence in ON-037")
	}
}

// TestON038_SpecSectionExists verifies that ON-038 (audit records are a subset
// of traces) exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.9 ON-038.
func TestON038_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-038") {
		t.Error("ON-038: specs/operator-nfr.md does not contain 'ON-038'")
	}
	if !strings.Contains(content, "Audit records are a subset") {
		t.Error("ON-038: specs/operator-nfr.md missing 'Audit records are a subset' heading text")
	}
}

// TestON038_AuditIsSubsetOfTransitionRecords verifies that the spec defines
// audit as a query over transition records (no separate audit-log store).
//
// Spec ref: operator-nfr.md §4.9 ON-038 — "No separate audit-log store is
// introduced; audit is a query over the transition-record sibling files."
func TestON038_AuditIsSubsetOfTransitionRecords(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "No separate audit-log store") {
		t.Error("ON-038: specs/operator-nfr.md missing 'No separate audit-log store' in ON-038")
	}
}

// TestON039_SpecSectionExists verifies that ON-039 (all observability
// operations are mechanism-tagged) exists.
//
// Spec ref: operator-nfr.md §4.9 ON-039.
func TestON039_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-039") {
		t.Error("ON-039: specs/operator-nfr.md does not contain 'ON-039'")
	}
	if !strings.Contains(content, "mechanism-tagged") {
		t.Error("ON-039: specs/operator-nfr.md missing 'mechanism-tagged' in ON-039 context")
	}
}

// TestON040_SpecSectionExists verifies that ON-040 (silent-hang detection
// obligation) exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.9 ON-040.
func TestON040_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-040") {
		t.Error("ON-040: specs/operator-nfr.md does not contain 'ON-040'")
	}
	if !strings.Contains(content, "Silent-hang detection obligation") {
		t.Error("ON-040: specs/operator-nfr.md missing 'Silent-hang detection obligation' heading")
	}
}

// TestON040_SilentHangEventNameIsCanonical verifies that the spec names the
// exact canonical event name `agent_warning_silent_hang`.
//
// Spec ref: operator-nfr.md §4.9 ON-040 — "the `agent_warning_silent_hang`
// event per [event-model.md §8.3]."
func TestON040_SilentHangEventNameIsCanonical(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "agent_warning_silent_hang") {
		t.Error("ON-040: specs/operator-nfr.md missing canonical event name 'agent_warning_silent_hang'")
	}
}

// TestON040_DrainForcedReasonExists verifies that the fixture encodes the
// `drain_forced` reason value declared by ON-040.
//
// Spec ref: operator-nfr.md §4.9 ON-040 — "agent_warning_silent_hang{
// reason=drain_forced, run_id, node_id}."
func TestON040_DrainForcedReasonExists(t *testing.T) {
	t.Parallel()

	found := false
	for _, r := range observabilityFixtureSilentHangReasons {
		if r.Value == "drain_forced" {
			found = true
			break
		}
	}
	if !found {
		t.Error("ON-040: silent-hang reason 'drain_forced' is missing from the fixture; ON-040 drain-timeout synthesis requires this value")
	}
}

// TestON040_DrainForcedSynthesisPriorToSIGKILL verifies that the spec
// explicitly requires the silent-hang synthesis to occur PRIOR to the SIGKILL.
//
// Spec ref: operator-nfr.md §4.9 ON-040 — "the daemon MUST synthesize an
// `agent_warning_silent_hang` event prior to the SIGKILL emission."
func TestON040_DrainForcedSynthesisPriorToSIGKILL(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "prior to the SIGKILL emission") {
		t.Error("ON-040: specs/operator-nfr.md missing 'prior to the SIGKILL emission' ordering constraint in ON-040")
	}
}

// TestON040_SilentHangSynthesisWithinDrainStep4Window verifies that the spec
// requires the synthesis to occur within drain step 4's wait window.
//
// Spec ref: operator-nfr.md §4.9 ON-040 — "The synthesis MUST occur within
// drain step 4's wait window per ON-027."
func TestON040_SilentHangSynthesisWithinDrainStep4Window(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "drain step 4") {
		t.Error("ON-040: specs/operator-nfr.md missing 'drain step 4' window constraint in ON-040")
	}
}
