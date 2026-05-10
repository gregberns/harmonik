package operatornfr_test

// healthCheckFixture — spec-level harness for hk-sx9r.52.
//
// Covers: ON-036 (every subsystem exposes a health-check interface), with
// specific focus on the orchestrator aggregation obligation ("The orchestrator
// MUST aggregate subsystem health into a harmonik-wide health status exposed
// via `harmonik status`").
//
// The per-status-value and fixture-completeness tests for ON-036 live in
// observability_sx9r82_test.go (the §10.2 parent harness). This file adds the
// aggregation-obligation and reason-string tests that are scoped to hk-sx9r.52.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-036; process-lifecycle.md §4.10.

import (
	"strings"
	"testing"
)

// healthCheckFixtureAggregationRule models one rule about how subsystem-level
// health-check results MUST be aggregated into daemon-wide health.
//
// Spec ref: operator-nfr.md §4.9 ON-036 — "The orchestrator MUST aggregate
// subsystem health into a harmonik-wide health status exposed via
// `harmonik status` per [process-lifecycle.md §4.10]."
type healthCheckFixtureAggregationRule struct {
	Rule    string
	SpecRef string
}

// healthCheckFixtureAggregationRules is the authoritative fixture encoding of
// the ON-036 aggregation obligations.
var healthCheckFixtureAggregationRules = []healthCheckFixtureAggregationRule{
	{
		"orchestrator aggregates subsystem health into harmonik-wide health status",
		"operator-nfr.md §4.9 ON-036",
	},
	{
		"harmonik-wide health status is exposed via harmonik status",
		"operator-nfr.md §4.9 ON-036 + process-lifecycle.md §4.10",
	},
}

// TestON036_OrchestratorAggregationObligationInSpec verifies that the spec
// text explicitly requires the orchestrator to aggregate subsystem health into
// a harmonik-wide health status.
//
// Spec ref: operator-nfr.md §4.9 ON-036 — "The orchestrator MUST aggregate
// subsystem health into a harmonik-wide health status exposed via
// `harmonik status` per [process-lifecycle.md §4.10]."
func TestON036_OrchestratorAggregationObligationInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "aggregate subsystem health") {
		t.Error("ON-036: specs/operator-nfr.md missing 'aggregate subsystem health' aggregation obligation")
	}
	if !strings.Contains(content, "harmonik-wide health status") {
		t.Error("ON-036: specs/operator-nfr.md missing 'harmonik-wide health status' in ON-036 aggregation clause")
	}
}

// TestON036_AggregationExposedViaHarmonikStatus verifies that the spec
// requires the aggregated health to be exposed via `harmonik status`.
//
// Spec ref: operator-nfr.md §4.9 ON-036 — "… exposed via `harmonik status`
// per [process-lifecycle.md §4.10]."
func TestON036_AggregationExposedViaHarmonikStatus(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "harmonik status") {
		t.Error("ON-036: specs/operator-nfr.md missing 'harmonik status' as the aggregation exposure surface in ON-036")
	}
	if !strings.Contains(content, "process-lifecycle.md") {
		t.Error("ON-036: specs/operator-nfr.md missing cross-reference to process-lifecycle.md in ON-036 aggregation clause")
	}
}

// TestON036_HealthCheckReasonStringDeclared verifies that the spec explicitly
// declares the optional reason string as part of the health-check interface.
//
// Spec ref: operator-nfr.md §4.9 ON-036 — "returning `health_status ∈ {OK,
// degraded, failed}` with an optional reason string."
func TestON036_HealthCheckReasonStringDeclared(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "optional reason string") {
		t.Error("ON-036: specs/operator-nfr.md missing 'optional reason string' in the health-check interface definition")
	}
}

// TestON036_AggregationFixtureRulesComplete verifies the aggregation-rule
// fixture is non-empty and every rule has SpecRef populated.
//
// Spec ref: operator-nfr.md §4.9 ON-036.
func TestON036_AggregationFixtureRulesComplete(t *testing.T) {
	t.Parallel()

	if len(healthCheckFixtureAggregationRules) == 0 {
		t.Fatal("ON-036: aggregation-rule fixture is empty; must encode at least the orchestrator-aggregation obligation")
	}

	for _, r := range healthCheckFixtureAggregationRules {
		r := r
		t.Run(r.Rule, func(t *testing.T) {
			t.Parallel()

			if r.Rule == "" {
				t.Error("ON-036: aggregation-rule fixture entry has empty Rule")
			}
			if r.SpecRef == "" {
				t.Errorf("ON-036: aggregation-rule %q has empty SpecRef", r.Rule)
			}
		})
	}
}

// TestON036_SubsystemDegradedDisambiguationInSpec verifies that the spec
// explicitly disambiguates subsystem-level `degraded` (ON-036/ON-037 input)
// from daemon-level `degraded` (§6.1 DaemonStatus aggregation).
//
// Spec ref: operator-nfr.md §3 glossary — "subsystem-level `degraded` … vs
// daemon-level `degraded`."
func TestON036_SubsystemDegradedDisambiguationInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "subsystem-level") {
		t.Error("ON-036: specs/operator-nfr.md missing 'subsystem-level' disambiguation for 'degraded' in §3 glossary")
	}
	if !strings.Contains(content, "daemon-level") {
		t.Error("ON-036: specs/operator-nfr.md missing 'daemon-level' disambiguation for 'degraded' in §3 glossary")
	}
}
