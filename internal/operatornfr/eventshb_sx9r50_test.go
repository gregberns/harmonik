package operatornfr_test

// eventshbFixture — spec-level harness for hk-sx9r.50, hk-sx9r.53,
// hk-sx9r.55, and hk-sx9r.59.
//
// Covers:
//   - ON-034 (every subsystem emits typed events) — four-axis + mechanism tag
//     obligation, event-model §6.3 and §4.6 registration cross-reference.
//   - ON-037 (every subsystem emits liveness heartbeats) — cadence/tolerance
//     operator-configurability via ON-004, harmonik-wide health aggregation.
//   - ON-039 (all observability operations are mechanism-tagged) — cognition-
//     separation obligation: cognition MUST be a separate verification node,
//     not folded into the observability protocol.
//   - ON-043 (metrics exposition format is deferred post-MVH) — Prom/OTel
//     deferred; implementation MAY expose but MUST NOT require for MVH.
//
// The sibling observability_sx9r82_test.go (hk-sx9r.82) carries the
// spec-section-existence tests for ON-034, ON-037, and ON-039. This file
// adds the deeper structural-constraint tests that the bead descriptions
// require, plus the full ON-043 coverage.
//
// These are spec-artifact and structural-constraint tests. Runtime per-subsystem
// emission conformance is the implementation-level integration test surface.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-034, ON-037, ON-039; §4.10 ON-043.

import (
	"strings"
	"testing"
)

// ── ON-034 fixtures ────────────────────────────────────────────────────────

// eventshbFixtureEventTagConstraint models one structural constraint that
// ON-034 places on every event emitted by a subsystem: the event MUST carry
// both the four-axis classification and the mechanism/cognition tag.
//
// Spec ref: operator-nfr.md §4.9 ON-034 — "Every event MUST carry the
// four-axis and mechanism/cognition tags per [architecture.md §4.1]."
type eventshbFixtureEventTagConstraint struct {
	Axis    string // axis name (llm-freedom, io-determinism, replay-safety, idempotency)
	SpecRef string // normative spec section
}

// eventshbFixtureFourAxisConstraints enumerates the four axes that every
// emitted event must carry per ON-034 + architecture.md §4.1.
var eventshbFixtureFourAxisConstraints = []eventshbFixtureEventTagConstraint{
	{"llm-freedom", "architecture.md §4.1 — axis 1: llm-freedom"},
	{"io-determinism", "architecture.md §4.1 — axis 2: io-determinism"},
	{"replay-safety", "architecture.md §4.1 — axis 3: replay-safety"},
	{"idempotency", "architecture.md §4.1 — axis 4: idempotency"},
}

// ── ON-037 fixtures ────────────────────────────────────────────────────────

// eventshbFixtureHeartbeatCadenceKnob models one liveness-heartbeat config
// knob declared by ON-037 as operator-configurable via ON-004.
//
// Spec ref: operator-nfr.md §4.9 ON-037 — "The cadence and tolerance are
// operator-configurable per §4.1.ON-004."
type eventshbFixtureHeartbeatCadenceKnob struct {
	Name    string // knob identifier (must appear in ON-004 config inventory)
	SpecRef string
}

// eventshbFixtureHeartbeatKnobs enumerates the two liveness-heartbeat
// operator-configurable knobs that ON-037 requires.
var eventshbFixtureHeartbeatKnobs = []eventshbFixtureHeartbeatCadenceKnob{
	{"heartbeat_cadence", "operator-nfr.md §4.9 ON-037 — emission cadence"},
	{"heartbeat_miss_tolerance", "operator-nfr.md §4.9 ON-037 — miss tolerance before degraded"},
}

// ── ON-039 fixtures ────────────────────────────────────────────────────────

// eventshbFixtureObservabilityOperation models one observability operation
// that ON-039 declares must be mechanism-tagged.
//
// Spec ref: operator-nfr.md §4.9 ON-039 — "Every observability operation
// (health-check evaluation, heartbeat emission, metric emission, log emission,
// audit-record derivation) MUST be mechanism-tagged."
type eventshbFixtureObservabilityOperation struct {
	Name    string // canonical operation name per ON-039
	SpecRef string
}

// eventshbFixtureObservabilityOperations enumerates the five observability
// operations that ON-039 names explicitly.
var eventshbFixtureObservabilityOperations = []eventshbFixtureObservabilityOperation{
	{"health-check-evaluation", "operator-nfr.md §4.9 ON-039"},
	{"heartbeat-emission", "operator-nfr.md §4.9 ON-039"},
	{"metric-emission", "operator-nfr.md §4.9 ON-039"},
	{"log-emission", "operator-nfr.md §4.9 ON-039"},
	{"audit-record-derivation", "operator-nfr.md §4.9 ON-039"},
}

// ── ON-034 tests ───────────────────────────────────────────────────────────

// TestON034_FourAxisConstraintSetIsComplete verifies that the fixture
// enumerates all four axes declared in architecture.md §4.1.
//
// Spec ref: operator-nfr.md §4.9 ON-034 — "Every event MUST carry the
// four-axis and mechanism/cognition tags per [architecture.md §4.1]."
func TestON034_FourAxisConstraintSetIsComplete(t *testing.T) {
	t.Parallel()

	const wantAxes = 4
	if len(eventshbFixtureFourAxisConstraints) != wantAxes {
		t.Errorf("ON-034: four-axis fixture has %d entries, want %d",
			len(eventshbFixtureFourAxisConstraints), wantAxes)
	}

	required := map[string]bool{
		"llm-freedom":    false,
		"io-determinism": false,
		"replay-safety":  false,
		"idempotency":    false,
	}
	for _, c := range eventshbFixtureFourAxisConstraints {
		required[c.Axis] = true
	}
	for axis, found := range required {
		if !found {
			t.Errorf("ON-034: axis %q is missing from the four-axis fixture", axis)
		}
	}
}

// TestON034_FourAxisConstraintsHaveSpecRefs verifies that every four-axis
// fixture entry has a non-empty SpecRef.
//
// Spec ref: operator-nfr.md §4.9 ON-034.
func TestON034_FourAxisConstraintsHaveSpecRefs(t *testing.T) {
	t.Parallel()

	for _, c := range eventshbFixtureFourAxisConstraints {
		c := c
		t.Run(c.Axis, func(t *testing.T) {
			t.Parallel()

			if c.SpecRef == "" {
				t.Errorf("ON-034: four-axis fixture entry %q has empty SpecRef", c.Axis)
			}
		})
	}
}

// TestON034_SpecRefersToEventModelSection63 verifies that specs/operator-nfr.md
// ON-034 contains a cross-reference to event-model.md §6.3, which owns the
// event-emission protocol that subsystems MUST use.
//
// Spec ref: operator-nfr.md §4.9 ON-034 — "Every subsystem MUST emit events
// per [event-model.md §6.3]."
func TestON034_SpecRefersToEventModelSection63(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "event-model.md §6.3") {
		t.Error("ON-034: specs/operator-nfr.md missing cross-reference to 'event-model.md §6.3'; ON-034 requires it")
	}
}

// TestON034_SpecRefersToEventModelSection46 verifies that specs/operator-nfr.md
// ON-034 contains a cross-reference to event-model.md §4.6, the event taxonomy
// registration section.
//
// Spec ref: operator-nfr.md §4.9 ON-034 — "Event taxonomy additions … MUST be
// … registered per [event-model.md §4.6]."
func TestON034_SpecRefersToEventModelSection46(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "event-model.md §4.6") {
		t.Error("ON-034: specs/operator-nfr.md missing cross-reference to 'event-model.md §4.6'; ON-034 requires taxonomy additions to be registered there")
	}
}

// TestON034_SpecRefersToSubsystemEnvelopeAR013 verifies that ON-034 references
// the subsystem envelope obligation (AR-013) for declaring event taxonomy
// additions.
//
// Spec ref: operator-nfr.md §4.9 ON-034 — "Event taxonomy additions introduced
// by a subsystem MUST be declared via the subsystem envelope (per
// [architecture.md §4.4] AR-013)."
func TestON034_SpecRefersToSubsystemEnvelopeAR013(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "AR-013") {
		t.Error("ON-034: specs/operator-nfr.md missing 'AR-013' subsystem envelope reference; ON-034 requires it for taxonomy addition declarations")
	}
}

// TestON034_SpecTagsLinePresentForON034 verifies that ON-034's normative
// heading is followed by a Tags: mechanism line within 30 lines, as required
// by AR-005 for normative headings.
//
// Spec ref: architecture.md AR-005 — every normative heading addition must
// carry Tags: mechanism|cognition within 30 lines.
func TestON034_SpecTagsLinePresentForON034(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	lines := strings.Split(string(data), "\n")

	// Find the ON-034 heading line.
	on034Line := -1
	for i, l := range lines {
		if strings.Contains(l, "ON-034") && strings.Contains(l, "Every subsystem emits typed events") {
			on034Line = i
			break
		}
	}
	if on034Line < 0 {
		t.Fatal("ON-034: cannot locate ON-034 heading in specs/operator-nfr.md; prior tests should have caught this")
	}

	// Within 30 lines of the heading, a Tags: line must appear.
	end := on034Line + 30
	if end > len(lines) {
		end = len(lines)
	}
	foundTags := false
	for _, l := range lines[on034Line:end] {
		if strings.HasPrefix(l, "Tags:") && (strings.Contains(l, "mechanism") || strings.Contains(l, "cognition")) {
			foundTags = true
			break
		}
	}
	if !foundTags {
		t.Errorf("ON-034: no 'Tags: mechanism|cognition' line found within 30 lines of the ON-034 heading (AR-005 obligation)")
	}
}

// ── ON-037 tests ───────────────────────────────────────────────────────────

// TestON037_HeartbeatKnobsAreInConfigInventory verifies that the liveness-
// heartbeat cadence and tolerance knobs named by ON-037 are represented in the
// obligationsFixtureConfigInventory (the ON-004 config inventory fixture).
//
// Spec ref: operator-nfr.md §4.9 ON-037 — "The cadence and tolerance are
// operator-configurable per §4.1.ON-004."
func TestON037_HeartbeatKnobsAreInConfigInventory(t *testing.T) {
	t.Parallel()

	knobNames := make(map[string]bool)
	for _, k := range obligationsFixtureConfigInventory {
		knobNames[k.Name] = true
	}

	for _, hk := range eventshbFixtureHeartbeatKnobs {
		hk := hk
		t.Run(hk.Name, func(t *testing.T) {
			t.Parallel()

			if !knobNames[hk.Name] {
				t.Errorf("ON-037: liveness-heartbeat knob %q is not in the ON-004 config inventory fixture; ON-037 declares it operator-configurable per §4.1.ON-004", hk.Name)
			}
		})
	}
}

// TestON037_HeartbeatKnobFixtureIsNonEmpty verifies that the heartbeat-knob
// fixture contains at least the two knobs (cadence, tolerance) declared in
// ON-037.
//
// Spec ref: operator-nfr.md §4.9 ON-037.
func TestON037_HeartbeatKnobFixtureIsNonEmpty(t *testing.T) {
	t.Parallel()

	const minKnobs = 2
	if len(eventshbFixtureHeartbeatKnobs) < minKnobs {
		t.Errorf("ON-037: heartbeat-knob fixture has %d entries, want at least %d (cadence + tolerance)", len(eventshbFixtureHeartbeatKnobs), minKnobs)
	}
}

// TestON037_HeartbeatKnobsHaveSpecRefs verifies that every heartbeat-knob
// fixture entry has a non-empty SpecRef.
//
// Spec ref: operator-nfr.md §4.9 ON-037.
func TestON037_HeartbeatKnobsHaveSpecRefs(t *testing.T) {
	t.Parallel()

	for _, hk := range eventshbFixtureHeartbeatKnobs {
		hk := hk
		t.Run(hk.Name, func(t *testing.T) {
			t.Parallel()

			if hk.SpecRef == "" {
				t.Errorf("ON-037: heartbeat-knob fixture entry %q has empty SpecRef", hk.Name)
			}
		})
	}
}

// TestON037_SpecRequiresHarmonikWideAggregation verifies that ON-037 names the
// harmonik-wide health aggregation obligation when a subsystem is degraded due
// to missed heartbeats.
//
// Spec ref: operator-nfr.md §4.9 ON-037 — "Missing heartbeats beyond tolerance
// MUST trigger a `degraded` classification … and raise the aggregated
// harmonik-wide health accordingly."
func TestON037_SpecRequiresHarmonikWideAggregation(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "harmonik-wide health") {
		t.Error("ON-037: specs/operator-nfr.md missing 'harmonik-wide health' aggregation requirement in ON-037")
	}
}

// TestON037_SpecTagsLinePresentForON037 verifies that ON-037's normative
// heading is followed by a Tags: mechanism line within 30 lines (AR-005).
//
// Spec ref: architecture.md AR-005.
func TestON037_SpecTagsLinePresentForON037(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	lines := strings.Split(string(data), "\n")

	on037Line := -1
	for i, l := range lines {
		if strings.Contains(l, "ON-037") && strings.Contains(l, "liveness heartbeat") {
			on037Line = i
			break
		}
	}
	if on037Line < 0 {
		t.Fatal("ON-037: cannot locate ON-037 heading in specs/operator-nfr.md; prior tests should have caught this")
	}

	end := on037Line + 30
	if end > len(lines) {
		end = len(lines)
	}
	foundTags := false
	for _, l := range lines[on037Line:end] {
		if strings.HasPrefix(l, "Tags:") && (strings.Contains(l, "mechanism") || strings.Contains(l, "cognition")) {
			foundTags = true
			break
		}
	}
	if !foundTags {
		t.Errorf("ON-037: no 'Tags: mechanism|cognition' line found within 30 lines of the ON-037 heading (AR-005 obligation)")
	}
}

// TestON037_SpecAxesLinePresentForON037 verifies that ON-037 carries an Axes:
// line per the requirement label (spec labels it idempotent).
//
// Spec ref: operator-nfr.md §4.9 ON-037 — label "axis:idempotency-idempotent"
// on the bead; the spec MUST carry an Axes: line within 30 lines of ON-037.
func TestON037_SpecAxesLinePresentForON037(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	lines := strings.Split(string(data), "\n")

	on037Line := -1
	for i, l := range lines {
		if strings.Contains(l, "ON-037") && strings.Contains(l, "liveness heartbeat") {
			on037Line = i
			break
		}
	}
	if on037Line < 0 {
		t.Fatal("ON-037: cannot locate ON-037 heading in specs/operator-nfr.md; prior tests should have caught this")
	}

	end := on037Line + 30
	if end > len(lines) {
		end = len(lines)
	}
	foundAxes := false
	for _, l := range lines[on037Line:end] {
		if strings.HasPrefix(l, "Axes:") {
			foundAxes = true
			break
		}
	}
	if !foundAxes {
		t.Errorf("ON-037: no 'Axes:' line found within 30 lines of the ON-037 heading; bead label axis:idempotency-idempotent requires it")
	}
}

// ── ON-039 tests ───────────────────────────────────────────────────────────

// TestON039_ObservabilityOperationsFixtureIsComplete verifies that the fixture
// enumerates all five observability operations declared in ON-039.
//
// Spec ref: operator-nfr.md §4.9 ON-039 — "Every observability operation
// (health-check evaluation, heartbeat emission, metric emission, log emission,
// audit-record derivation) MUST be mechanism-tagged."
func TestON039_ObservabilityOperationsFixtureIsComplete(t *testing.T) {
	t.Parallel()

	const wantOps = 5
	if len(eventshbFixtureObservabilityOperations) != wantOps {
		t.Errorf("ON-039: observability-operation fixture has %d entries, want %d", len(eventshbFixtureObservabilityOperations), wantOps)
	}

	required := map[string]bool{
		"health-check-evaluation": false,
		"heartbeat-emission":      false,
		"metric-emission":         false,
		"log-emission":            false,
		"audit-record-derivation": false,
	}
	for _, op := range eventshbFixtureObservabilityOperations {
		required[op.Name] = true
	}
	for name, found := range required {
		if !found {
			t.Errorf("ON-039: observability operation %q is missing from the fixture", name)
		}
	}
}

// TestON039_ObservabilityOperationsHaveSpecRefs verifies that every
// observability-operation fixture entry has a non-empty SpecRef.
//
// Spec ref: operator-nfr.md §4.9 ON-039.
func TestON039_ObservabilityOperationsHaveSpecRefs(t *testing.T) {
	t.Parallel()

	for _, op := range eventshbFixtureObservabilityOperations {
		op := op
		t.Run(op.Name, func(t *testing.T) {
			t.Parallel()

			if op.SpecRef == "" {
				t.Errorf("ON-039: observability-operation fixture entry %q has empty SpecRef", op.Name)
			}
		})
	}
}

// TestON039_SpecCognitionSeparationObligation verifies that ON-039 explicitly
// requires cognition-producing operations to be represented as separate
// verification nodes, NOT folded into the observability protocol.
//
// Spec ref: operator-nfr.md §4.9 ON-039 — "Any operation that requires
// cognition to produce the observability signal MUST be represented as a
// separate verification node per [architecture.md §4.3], NOT folded into the
// observability protocol."
func TestON039_SpecCognitionSeparationObligation(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "separate verification node") {
		t.Error("ON-039: specs/operator-nfr.md missing 'separate verification node' cognition-separation obligation in ON-039")
	}
	if !strings.Contains(content, "NOT folded into the observability protocol") {
		t.Error("ON-039: specs/operator-nfr.md missing 'NOT folded into the observability protocol' prohibition in ON-039")
	}
}

// TestON039_SpecRefersToArchitectureSection43 verifies that ON-039 cross-
// references architecture.md §4.3, the verification-node definition.
//
// Spec ref: operator-nfr.md §4.9 ON-039 — "… a separate verification node
// per [architecture.md §4.3]."
func TestON039_SpecRefersToArchitectureSection43(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "architecture.md §4.3") {
		t.Error("ON-039: specs/operator-nfr.md missing cross-reference to 'architecture.md §4.3' in ON-039 context; cognition-separation obligation requires it")
	}
}

// TestON039_SpecTagsLinePresentForON039 verifies that ON-039's normative
// heading is followed by a Tags: mechanism line within 30 lines (AR-005).
//
// Spec ref: architecture.md AR-005.
func TestON039_SpecTagsLinePresentForON039(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	lines := strings.Split(string(data), "\n")

	on039Line := -1
	for i, l := range lines {
		if strings.Contains(l, "ON-039") && strings.Contains(l, "mechanism-tagged") {
			on039Line = i
			break
		}
	}
	if on039Line < 0 {
		t.Fatal("ON-039: cannot locate ON-039 heading in specs/operator-nfr.md; prior tests should have caught this")
	}

	end := on039Line + 30
	if end > len(lines) {
		end = len(lines)
	}
	foundTags := false
	for _, l := range lines[on039Line:end] {
		if strings.HasPrefix(l, "Tags:") && (strings.Contains(l, "mechanism") || strings.Contains(l, "cognition")) {
			foundTags = true
			break
		}
	}
	if !foundTags {
		t.Errorf("ON-039: no 'Tags: mechanism|cognition' line found within 30 lines of the ON-039 heading (AR-005 obligation)")
	}
}

// ── ON-043 tests ───────────────────────────────────────────────────────────
//
// Note: TestON043_SpecSectionExists is in multidaemon_sx9r83_test.go (hk-sx9r.83
// sibling). Tests below add the structural-constraint coverage from hk-sx9r.59.

// TestON043_PrometheusDeferredStatement verifies that ON-043 explicitly names
// Prometheus as a deferred wire format.
//
// Spec ref: operator-nfr.md §4.10 ON-043 — "Prometheus and OpenTelemetry wire
// formats for metric exposition are deferred post-MVH."
func TestON043_PrometheusDeferredStatement(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "Prometheus") {
		t.Error("ON-043: specs/operator-nfr.md missing 'Prometheus' deferred-format statement in ON-043")
	}
}

// TestON043_OpenTelemetryDeferredStatement verifies that ON-043 explicitly
// names OpenTelemetry as a deferred wire format.
//
// Spec ref: operator-nfr.md §4.10 ON-043 — "Prometheus and OpenTelemetry wire
// formats for metric exposition are deferred post-MVH."
func TestON043_OpenTelemetryDeferredStatement(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "OpenTelemetry") {
		t.Error("ON-043: specs/operator-nfr.md missing 'OpenTelemetry' deferred-format statement in ON-043")
	}
}

// TestON043_MayNotMustNotConstraint verifies that ON-043 carries the MAY-but-
// MUST-NOT-require conformance posture: an implementation MAY expose Prom/OTel
// but MUST NOT require them for MVH conformance.
//
// Spec ref: operator-nfr.md §4.10 ON-043 — "An implementation MAY additionally
// expose Prom/OTel endpoints but MUST NOT require them for MVH conformance."
func TestON043_MayNotMustNotConstraint(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "MUST NOT require them for MVH conformance") {
		t.Error("ON-043: specs/operator-nfr.md missing 'MUST NOT require them for MVH conformance' in ON-043; the MAY-but-MUST-NOT posture is normative")
	}
}

// TestON043_MVHSubstrateStatement verifies that ON-043 names structured logs
// and typed events as the MVH observability substrate (the positive
// counterpart to the Prom/OTel deferral).
//
// Spec ref: operator-nfr.md §4.10 ON-043 — "MVH observability is structured
// logs (§4.9.ON-035) plus typed events (§4.9.ON-034)."
func TestON043_MVHSubstrateStatement(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "MVH observability is structured logs") {
		t.Error("ON-043: specs/operator-nfr.md missing 'MVH observability is structured logs' substrate statement in ON-043")
	}
}

// TestON043_SpecTagsLinePresentForON043 verifies that ON-043's normative
// heading is followed by a Tags: mechanism line within 30 lines (AR-005).
//
// Spec ref: architecture.md AR-005.
func TestON043_SpecTagsLinePresentForON043(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	lines := strings.Split(string(data), "\n")

	on043Line := -1
	for i, l := range lines {
		if strings.Contains(l, "ON-043") && strings.Contains(l, "Metrics exposition format") {
			on043Line = i
			break
		}
	}
	if on043Line < 0 {
		t.Fatal("ON-043: cannot locate ON-043 heading in specs/operator-nfr.md; prior tests should have caught this")
	}

	end := on043Line + 30
	if end > len(lines) {
		end = len(lines)
	}
	foundTags := false
	for _, l := range lines[on043Line:end] {
		if strings.HasPrefix(l, "Tags:") && (strings.Contains(l, "mechanism") || strings.Contains(l, "cognition")) {
			foundTags = true
			break
		}
	}
	if !foundTags {
		t.Errorf("ON-043: no 'Tags: mechanism|cognition' line found within 30 lines of the ON-043 heading (AR-005 obligation)")
	}
}
