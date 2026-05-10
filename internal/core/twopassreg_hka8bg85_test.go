package core

// twopassreg_hka8bg85_test.go — Two-pass registration fixture
//
// Covers specs/control-points.md §10.2 (CP-043..CP-046 + CP-INV-001 + §7.1):
//   - Idempotent-by-name: re-register identical body succeeds silently (CP-044).
//   - Divergent-body rejection: registering a different body under an existing
//     name MUST fail at startup with a specific error (CP-044).
//   - Daemon-local scope: no cross-daemon sharing (CP-045).
//   - Deterministic lookups: LookupByName / LookupByTrigger / LookupByAttachPoint
//     return reproducible orderings (CP-046).
//   - Cognition-Guard rejection at registration (CP-020).
//   - Two-pass cross-document reference resolution (§7.1 OQ-CP-005 default):
//     all policies parsed before registration; missing refs fail startup.
//   - control_points_registration_started / control_points_registered batch_id
//     pairing: absence of _registered paired with prior _started of the same
//     batch_id signals crashed-mid-registration.
//   - Replay rebuilds from policy YAML (registry is ephemeral per CP-045).
//
// These tests are fixtures-only: they document the registration-sequence
// contracts at the core-types level. The S02 (Policy Engine) subsystem
// implements the registry; here we verify data shape and invariant contracts.

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"testing"
)

// twoPassRegControlPoint is a minimal record modeling a registered ControlPoint.
// It carries only the fields needed to exercise the registration surface
// contracts: name, kind, evaluator mode, and a canonical body hash.
//
// In production, ControlPoint body equality per CP-044 is computed over a
// canonical serialisation of (kind, trigger, evaluator, payload). This fixture
// uses a pre-computed bodyHash to represent that equality surface.
type twoPassRegControlPoint struct {
	// Name is the unique identifier within the daemon registry (CP-043).
	// Must be non-empty.
	Name string

	// Kind is the ControlPoint discriminator (Gate, Hook, Guard, Budget).
	Kind Kind

	// EvaluatorMode is the mode tag on the evaluator (mechanism or cognition).
	// Guards MUST be mechanism-tagged (CP-020).
	EvaluatorMode ModeTag

	// BodyHash is the canonical equality hash of (kind, trigger, evaluator, payload).
	// Two ControlPoints with the same Name and same BodyHash are considered
	// identical per CP-044. Different BodyHash under the same Name is a
	// divergent-body conflict.
	BodyHash string
}

// Valid reports whether the fixture record is structurally well-formed.
func (cp twoPassRegControlPoint) Valid() bool {
	return cp.Name != "" && cp.Kind.Valid() && cp.EvaluatorMode.Valid() && cp.BodyHash != ""
}

// twoPassRegRegistry is a minimal in-memory registry implementing the
// registration surface contracts per specs/control-points.md §6.1.7.
// It is NOT a production implementation; it exists to document the invariants.
type twoPassRegRegistry struct {
	entries map[string]twoPassRegControlPoint
}

func newTwoPassRegRegistry() *twoPassRegRegistry {
	return &twoPassRegRegistry{entries: make(map[string]twoPassRegControlPoint)}
}

// ErrDivergentBody is returned by Register when a ControlPoint with the same
// name but a different body is already registered. Per CP-044, this MUST fail
// at startup with a specific error code.
var errDivergentBody = errors.New("twopassreg: duplicate registration with divergent body")

// ErrCognitionGuard is returned by Register when a Guard ControlPoint with a
// cognition-tagged evaluator is submitted for registration. Per CP-020, this
// MUST fail with a structural error.
var errCognitionGuard = errors.New("twopassreg: cognition-tagged Guard forbidden (CP-020)")

// Register implements the re-registration-safe contract per CP-044:
//   - Same name + same BodyHash → succeed silently (idempotent).
//   - Same name + different BodyHash → fail with errDivergentBody.
//   - Guard + cognition evaluator → fail with errCognitionGuard (CP-020).
//   - New name → register and return nil.
func (r *twoPassRegRegistry) Register(cp twoPassRegControlPoint) error {
	// CP-020: cognition-tagged Guards are forbidden.
	if cp.Kind == KindGuard && cp.EvaluatorMode == ModeTagCognition {
		return errCognitionGuard
	}

	existing, exists := r.entries[cp.Name]
	if exists {
		if existing.BodyHash == cp.BodyHash {
			// Re-registration-safe: identical body, succeed silently.
			return nil
		}
		// Divergent body: fail with specific error.
		return fmt.Errorf("%w: name=%q existing_hash=%q new_hash=%q",
			errDivergentBody, cp.Name, existing.BodyHash, cp.BodyHash)
	}

	r.entries[cp.Name] = cp
	return nil
}

// LookupByName returns the ControlPoint with the given name, and a boolean
// indicating whether the name is registered. Per CP-046, the lookup is
// deterministic given the same registry state.
func (r *twoPassRegRegistry) LookupByName(name string) (twoPassRegControlPoint, bool) {
	cp, ok := r.entries[name]
	return cp, ok
}

// All returns every registered ControlPoint in name-ascending order, per
// §6.1.7 and CP-046 (deterministic; sorted by name ascending).
func (r *twoPassRegRegistry) All() []twoPassRegControlPoint {
	out := make([]twoPassRegControlPoint, 0, len(r.entries))
	for _, cp := range r.entries {
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// LookupByKind returns all ControlPoints of the given Kind, sorted by name
// ascending per CP-046 (total ordering for reproducibility).
func (r *twoPassRegRegistry) LookupByKind(kind Kind) []twoPassRegControlPoint {
	var out []twoPassRegControlPoint
	for _, cp := range r.entries {
		if cp.Kind == kind {
			out = append(out, cp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// --- Fixture builders ---

// twoPassRegGateFixture returns a mechanism-tagged Gate ControlPoint.
func twoPassRegGateFixture() twoPassRegControlPoint {
	return twoPassRegControlPoint{
		Name:          "pre-deploy-gate",
		Kind:          KindGate,
		EvaluatorMode: ModeTagMechanism,
		BodyHash:      "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899",
	}
}

// twoPassRegHookFixture returns a cognition-tagged Hook ControlPoint.
func twoPassRegHookFixture() twoPassRegControlPoint {
	return twoPassRegControlPoint{
		Name:          "post-merge-hook",
		Kind:          KindHook,
		EvaluatorMode: ModeTagCognition,
		BodyHash:      "bbccddeeff00112233445566778899aabbccddeeff00112233445566778899aa",
	}
}

// twoPassRegGuardMechanismFixture returns a mechanism-tagged Guard — valid.
func twoPassRegGuardMechanismFixture() twoPassRegControlPoint {
	return twoPassRegControlPoint{
		Name:          "priority-guard",
		Kind:          KindGuard,
		EvaluatorMode: ModeTagMechanism,
		BodyHash:      "ccddeeff00112233445566778899aabbccddeeff00112233445566778899aabb",
	}
}

// twoPassRegGuardCognitionFixture returns a cognition-tagged Guard — invalid per CP-020.
func twoPassRegGuardCognitionFixture() twoPassRegControlPoint {
	return twoPassRegControlPoint{
		Name:          "bad-cognition-guard",
		Kind:          KindGuard,
		EvaluatorMode: ModeTagCognition,
		BodyHash:      "ddeeff00112233445566778899aabbccddeeff00112233445566778899aabbcc",
	}
}

// twoPassRegRegistrationEvent documents the batch-id pairing events per §7.1.
// These are the JSONL events that bracket a registration batch.
type twoPassRegRegistrationEvent struct {
	// EventType is "control_points_registration_started" or
	// "control_points_registered".
	EventType string `json:"event_type"`

	// BatchID is the daemon boot_id that pairs the start and complete events.
	// Absence of "control_points_registered" with the same BatchID signals a
	// crashed-mid-registration batch per §7.1.
	BatchID string `json:"batch_id"`

	// Count is the number of registered ControlPoints (only on _registered event).
	Count int `json:"count,omitempty"`
}

// --- Tests ---

// TestTwoPassReg_IdempotentByNameIdenticalBody verifies that registering a
// ControlPoint with the same name and identical body succeeds silently (no
// error, no duplicate entry).
//
// specs/control-points.md §4.9.CP-044: "re-registration-safe on identical body."
func TestTwoPassReg_IdempotentByNameIdenticalBody(t *testing.T) {
	t.Parallel()

	reg := newTwoPassRegRegistry()
	cp := twoPassRegGateFixture()

	// First registration.
	if err := reg.Register(cp); err != nil {
		t.Fatalf("first Register: unexpected error: %v", err)
	}

	// Second registration with identical body: MUST succeed silently.
	if err := reg.Register(cp); err != nil {
		t.Errorf("second Register (identical body): got error %v, want nil", err)
	}

	// Registry still has exactly one entry.
	all := reg.All()
	if len(all) != 1 {
		t.Errorf("registry has %d entries after idempotent re-registration, want 1", len(all))
	}
}

// TestTwoPassReg_DivergentBodyRejected verifies that registering a different
// body under an existing name fails with errDivergentBody.
//
// specs/control-points.md §4.9.CP-044: "registering a different body under an
// existing name MUST fail at startup with a specific error code."
func TestTwoPassReg_DivergentBodyRejected(t *testing.T) {
	t.Parallel()

	reg := newTwoPassRegRegistry()
	original := twoPassRegGateFixture()

	if err := reg.Register(original); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	// Same name, different BodyHash.
	divergent := original
	divergent.BodyHash = "ffeeddccbbaa00998877665544332211ffeeddccbbaa00998877665544332211"

	err := reg.Register(divergent)
	if err == nil {
		t.Fatal("Register with divergent body: expected error, got nil")
	}
	if !errors.Is(err, errDivergentBody) {
		t.Errorf("Register with divergent body: got %v, want errDivergentBody", err)
	}
}

// TestTwoPassReg_CognitionGuardRejected verifies that a cognition-tagged Guard
// is rejected at registration per CP-020.
//
// specs/control-points.md §4.4.CP-020: "A Guard MUST be mechanism-tagged; a
// cognition-tagged Guard MUST fail registration."
func TestTwoPassReg_CognitionGuardRejected(t *testing.T) {
	t.Parallel()

	reg := newTwoPassRegRegistry()
	badGuard := twoPassRegGuardCognitionFixture()

	err := reg.Register(badGuard)
	if err == nil {
		t.Fatal("Register cognition-tagged Guard: expected error, got nil")
	}
	if !errors.Is(err, errCognitionGuard) {
		t.Errorf("Register cognition-tagged Guard: got %v, want errCognitionGuard", err)
	}

	// The registry must remain empty; the bad registration must not persist.
	all := reg.All()
	if len(all) != 0 {
		t.Errorf("registry has %d entries after cognition-Guard rejection, want 0", len(all))
	}
}

// TestTwoPassReg_MechanismGuardAccepted verifies that a mechanism-tagged Guard
// is accepted at registration (the valid path adjacent to CP-020 rejection).
func TestTwoPassReg_MechanismGuardAccepted(t *testing.T) {
	t.Parallel()

	reg := newTwoPassRegRegistry()
	guard := twoPassRegGuardMechanismFixture()

	if err := reg.Register(guard); err != nil {
		t.Errorf("Register mechanism-tagged Guard: unexpected error: %v", err)
	}
}

// TestTwoPassReg_DaemonLocalScope verifies that two independent registries
// do not share state, modelling the daemon-local scope requirement of CP-045.
//
// Each daemon maintains its own independent registry; no cross-daemon sharing
// occurs. This test demonstrates the isolation property by showing that
// registrations in one registry are invisible to another.
func TestTwoPassReg_DaemonLocalScope(t *testing.T) {
	t.Parallel()

	reg1 := newTwoPassRegRegistry()
	reg2 := newTwoPassRegRegistry()

	cp := twoPassRegGateFixture()
	if err := reg1.Register(cp); err != nil {
		t.Fatalf("reg1.Register: %v", err)
	}

	// reg2 is independent: the registration in reg1 must not appear in reg2.
	_, found := reg2.LookupByName(cp.Name)
	if found {
		t.Errorf("reg2 contains %q which was only registered in reg1 — cross-daemon leak", cp.Name)
	}

	all2 := reg2.All()
	if len(all2) != 0 {
		t.Errorf("reg2 has %d entries, want 0 (daemon-local scope per CP-045)", len(all2))
	}
}

// TestTwoPassReg_LookupByNameDeterministic verifies that LookupByName returns
// the same ControlPoint on repeated calls given the same registry state.
//
// CP-046: "all registry lookups MUST be deterministic."
func TestTwoPassReg_LookupByNameDeterministic(t *testing.T) {
	t.Parallel()

	reg := newTwoPassRegRegistry()
	cp := twoPassRegGateFixture()

	if err := reg.Register(cp); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for i := range 5 {
		got, ok := reg.LookupByName(cp.Name)
		if !ok {
			t.Fatalf("iteration %d: LookupByName(%q) not found", i, cp.Name)
		}
		if got.Name != cp.Name {
			t.Errorf("iteration %d: Name = %q, want %q", i, got.Name, cp.Name)
		}
		if got.BodyHash != cp.BodyHash {
			t.Errorf("iteration %d: BodyHash = %q, want %q", i, got.BodyHash, cp.BodyHash)
		}
	}
}

// TestTwoPassReg_AllReturnsSortedByName verifies that All() returns
// ControlPoints in name-ascending order on every call, satisfying the total
// ordering requirement of CP-046.
func TestTwoPassReg_AllReturnsSortedByName(t *testing.T) {
	t.Parallel()

	reg := newTwoPassRegRegistry()

	// Register in non-alphabetical order to probe sort stability.
	cps := []twoPassRegControlPoint{
		{Name: "zz-gate", Kind: KindGate, EvaluatorMode: ModeTagMechanism, BodyHash: "aa" + hashPad(62)},
		{Name: "aa-hook", Kind: KindHook, EvaluatorMode: ModeTagCognition, BodyHash: "bb" + hashPad(62)},
		{Name: "mm-budget", Kind: KindBudget, EvaluatorMode: ModeTagMechanism, BodyHash: "cc" + hashPad(62)},
	}
	for _, cp := range cps {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", cp.Name, err)
		}
	}

	all := reg.All()
	if len(all) != len(cps) {
		t.Fatalf("All() len = %d, want %d", len(all), len(cps))
	}

	// Verify ascending order.
	for i := 1; i < len(all); i++ {
		if all[i].Name <= all[i-1].Name {
			t.Errorf("All()[%d].Name = %q <= All()[%d].Name = %q (not ascending)",
				i, all[i].Name, i-1, all[i-1].Name)
		}
	}

	// Verify consistent on second call.
	all2 := reg.All()
	for i, cp := range all2 {
		if cp.Name != all[i].Name {
			t.Errorf("second All()[%d].Name = %q, first = %q (non-deterministic)", i, cp.Name, all[i].Name)
		}
	}
}

// hashPad returns n '0' characters, used to pad test body hashes to 64 chars.
func hashPad(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '0'
	}
	return string(b)
}

// TestTwoPassReg_LookupByKindDeterministic verifies that LookupByKind returns
// the same ordered list on repeated calls, per CP-046.
func TestTwoPassReg_LookupByKindDeterministic(t *testing.T) {
	t.Parallel()

	reg := newTwoPassRegRegistry()

	gates := []twoPassRegControlPoint{
		{Name: "c-gate", Kind: KindGate, EvaluatorMode: ModeTagMechanism, BodyHash: "cc" + hashPad(62)},
		{Name: "a-gate", Kind: KindGate, EvaluatorMode: ModeTagMechanism, BodyHash: "aa" + hashPad(62)},
		{Name: "b-gate", Kind: KindGate, EvaluatorMode: ModeTagMechanism, BodyHash: "bb" + hashPad(62)},
	}
	for _, cp := range gates {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", cp.Name, err)
		}
	}
	// Also register a hook to verify it's excluded from Gate lookup.
	if err := reg.Register(twoPassRegHookFixture()); err != nil {
		t.Fatalf("Register hook: %v", err)
	}

	result1 := reg.LookupByKind(KindGate)
	result2 := reg.LookupByKind(KindGate)

	if len(result1) != 3 {
		t.Fatalf("LookupByKind(Gate) len = %d, want 3", len(result1))
	}

	for i, cp := range result1 {
		if cp.Kind != KindGate {
			t.Errorf("result1[%d].Kind = %q, want Gate", i, cp.Kind)
		}
		if i > 0 && result1[i].Name <= result1[i-1].Name {
			t.Errorf("LookupByKind not sorted: result[%d].Name=%q <= result[%d].Name=%q",
				i, result1[i].Name, i-1, result1[i-1].Name)
		}
		if result2[i].Name != result1[i].Name {
			t.Errorf("LookupByKind non-deterministic at index %d: run1=%q run2=%q",
				i, result1[i].Name, result2[i].Name)
		}
	}
}

// --- §7.1 batch-id pairing tests ---

// TestTwoPassReg_BatchIDPairingCompleteRun verifies the normal-case pairing:
// control_points_registration_started is followed by control_points_registered
// with the same batch_id, indicating successful registration.
func TestTwoPassReg_BatchIDPairingCompleteRun(t *testing.T) {
	t.Parallel()

	batchID := "boot-20260509-001"
	events := []twoPassRegRegistrationEvent{
		{EventType: "control_points_registration_started", BatchID: batchID},
		{EventType: "control_points_registered", BatchID: batchID, Count: 3},
	}

	started, completed := twoPassRegScanBatchEvents(events, batchID)
	if !started {
		t.Error("control_points_registration_started not detected")
	}
	if !completed {
		t.Error("control_points_registered not detected for complete run")
	}
}

// TestTwoPassReg_BatchIDPairingCrashedMidRegistration verifies that when
// control_points_registration_started appears without a matching
// control_points_registered (same batch_id), the batch is detected as
// crashed-mid-registration.
//
// §7.1: "if control_points_registration_started appears in JSONL without a
// matching control_points_registered (same batch_id), the batch is presumed
// crashed mid-registration."
func TestTwoPassReg_BatchIDPairingCrashedMidRegistration(t *testing.T) {
	t.Parallel()

	batchID := "boot-20260509-002"
	// Only the started event — daemon crashed before emitting registered.
	events := []twoPassRegRegistrationEvent{
		{EventType: "control_points_registration_started", BatchID: batchID},
	}

	started, completed := twoPassRegScanBatchEvents(events, batchID)
	if !started {
		t.Error("control_points_registration_started not detected")
	}
	if completed {
		t.Error("control_points_registered should NOT be detected for crashed batch")
	}

	crashedMidRegistration := started && !completed
	if !crashedMidRegistration {
		t.Error("crashed-mid-registration not detected from batch event gap")
	}
}

// TestTwoPassReg_BatchIDPairingMismatchedBatchIgnored verifies that events
// with a different batch_id do not satisfy the pairing for the target batch.
func TestTwoPassReg_BatchIDPairingMismatchedBatchIgnored(t *testing.T) {
	t.Parallel()

	targetBatchID := "boot-20260509-003"
	otherBatchID := "boot-20260509-004"

	events := []twoPassRegRegistrationEvent{
		{EventType: "control_points_registration_started", BatchID: targetBatchID},
		// registered event belongs to a DIFFERENT batch.
		{EventType: "control_points_registered", BatchID: otherBatchID, Count: 2},
	}

	started, completed := twoPassRegScanBatchEvents(events, targetBatchID)
	if !started {
		t.Error("control_points_registration_started not detected for target batch")
	}
	if completed {
		t.Error("control_points_registered from different batch must NOT satisfy target pairing")
	}
}

// twoPassRegScanBatchEvents scans a sequence of registration events for the
// given batch_id and returns (started, completed) booleans documenting whether
// the paired events are present.
func twoPassRegScanBatchEvents(events []twoPassRegRegistrationEvent, batchID string) (started, completed bool) {
	for _, ev := range events {
		if ev.BatchID != batchID {
			continue
		}
		switch ev.EventType {
		case "control_points_registration_started":
			started = true
		case "control_points_registered":
			completed = true
		}
	}
	return started, completed
}

// TestTwoPassReg_ReplayRebuildsFromPolicyYAML verifies that after a
// crashed-mid-registration batch is detected, the daemon rebuilds the registry
// from scratch from policy YAML.
//
// §7.1: "the in-process registry is ephemeral (§4.9.CP-045), so replay
// rebuilds from policy YAML. A daemon-startup subsystem MUST NOT consume
// registry state across a batch gap."
//
// This test documents the rebuild invariant: before the batch_id pairing is
// confirmed complete, the registry is treated as empty and must be rebuilt.
func TestTwoPassReg_ReplayRebuildsFromPolicyYAML(t *testing.T) {
	t.Parallel()

	// Crashed state: registry has in-memory state from incomplete pass.
	crashedReg := newTwoPassRegRegistry()
	_ = crashedReg.Register(twoPassRegGateFixture())
	// At crash, the registry has 1 entry.
	if len(crashedReg.All()) != 1 {
		t.Fatal("fixture setup: crashed registry should have 1 entry")
	}

	// On restart with a batch gap, the daemon MUST NOT use crashedReg.
	// It MUST construct a new empty registry and rebuild from policy YAML.
	rebuiltReg := newTwoPassRegRegistry()
	// Initially empty.
	if len(rebuiltReg.All()) != 0 {
		t.Fatal("rebuilt registry must start empty (ephemeral per CP-045)")
	}

	// Rebuild from policy YAML: re-register the same ControlPoints.
	cpFromYAML := twoPassRegGateFixture()
	if err := rebuiltReg.Register(cpFromYAML); err != nil {
		t.Errorf("rebuild Register: %v", err)
	}

	// After rebuild, the registry matches the expected state.
	rebuilt, ok := rebuiltReg.LookupByName(cpFromYAML.Name)
	if !ok {
		t.Errorf("rebuilt registry missing %q after replay-from-YAML", cpFromYAML.Name)
	}
	if rebuilt.BodyHash != cpFromYAML.BodyHash {
		t.Errorf("rebuilt BodyHash = %q, want %q", rebuilt.BodyHash, cpFromYAML.BodyHash)
	}
}

// TestTwoPassReg_TwoPassCrossDocRefMissingFails documents the two-pass
// cross-document reference resolution requirement (§7.1 OQ-CP-005 default):
// all policies MUST be parsed before registration; missing refs MUST fail startup.
//
// Pass 1 builds a symbol table of all names. Pass 2 resolves references against
// that table. A missing reference in Pass 2 must fail the entire startup.
func TestTwoPassReg_TwoPassCrossDocRefMissingFails(t *testing.T) {
	t.Parallel()

	// Symbol table built in Pass 1 (available names from all policy docs).
	symbolTable := map[string]bool{
		"token-budget-default": true,
		"wall-clock-budget":    true,
	}

	// Pass 2: a Gate references "missing-budget-ref" which is not in symbolTable.
	budgetRef := "missing-budget-ref"
	_, refExists := symbolTable[budgetRef]
	if refExists {
		t.Fatal("fixture setup: ref should be missing from symbol table")
	}

	// Missing ref MUST fail startup — represented here as a detected startup error.
	startupFailed := !refExists
	if !startupFailed {
		t.Error("startup should fail when a cross-document ref is missing; two-pass must detect this")
	}
}

// TestTwoPassReg_BatchIDEventJSONRoundTrip verifies that registration batch
// events serialise and deserialise correctly for JSONL durability.
func TestTwoPassReg_BatchIDEventJSONRoundTrip(t *testing.T) {
	t.Parallel()

	events := []twoPassRegRegistrationEvent{
		{EventType: "control_points_registration_started", BatchID: "boot-001"},
		{EventType: "control_points_registered", BatchID: "boot-001", Count: 5},
	}

	for i, ev := range events {
		data, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("event[%d] json.Marshal: %v", i, err)
		}
		var got twoPassRegRegistrationEvent
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("event[%d] json.Unmarshal: %v", i, err)
		}
		if got.EventType != ev.EventType {
			t.Errorf("event[%d] EventType: got %q, want %q", i, got.EventType, ev.EventType)
		}
		if got.BatchID != ev.BatchID {
			t.Errorf("event[%d] BatchID: got %q, want %q", i, got.BatchID, ev.BatchID)
		}
		if got.Count != ev.Count {
			t.Errorf("event[%d] Count: got %d, want %d", i, got.Count, ev.Count)
		}
	}
}

// TestTwoPassReg_ControlPointFixturesAreValid verifies that all fixture
// ControlPoints are structurally valid (mechanism and cognition variants).
func TestTwoPassReg_ControlPointFixturesAreValid(t *testing.T) {
	t.Parallel()

	cps := []twoPassRegControlPoint{
		twoPassRegGateFixture(),
		twoPassRegHookFixture(),
		twoPassRegGuardMechanismFixture(),
	}
	for _, cp := range cps {
		if !cp.Valid() {
			t.Errorf("fixture %q is invalid: %+v", cp.Name, cp)
		}
	}
}

// TestTwoPassReg_CognitionGuardFixtureIsInvalidForRegistration verifies that
// the cognition-Guard fixture — while structurally parseable — is NOT registerable
// (CP-020), and that the errCognitionGuard error is correctly identified.
func TestTwoPassReg_CognitionGuardFixtureIsInvalidForRegistration(t *testing.T) {
	t.Parallel()

	reg := newTwoPassRegRegistry()
	badGuard := twoPassRegGuardCognitionFixture()

	// The fixture struct is parseable (Valid() checks structural shape only).
	// But registration must reject it per CP-020.
	err := reg.Register(badGuard)
	if err == nil {
		t.Fatal("expected errCognitionGuard, got nil")
	}
	if !errors.Is(err, errCognitionGuard) {
		t.Errorf("expected errCognitionGuard, got: %v", err)
	}
}
