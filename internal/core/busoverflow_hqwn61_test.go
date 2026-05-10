// Package core — bus-overflow scenario harness for non-blocking back-pressure.
//
// This file implements the scenario harness described in bead hk-hqwn.61.
// It covers three test surfaces:
//
//  1. Spec-content sensors — assert that EV-011a (non-blocking back-pressure)
//     and the three shed-policy enum values are present in specs/event-model.md.
//     These sensors run unconditionally and guard against silent spec erosion.
//
//  2. Fixture helpers — busOverflowFixture* helpers that define the scenario
//     inputs used by the scenario tests below. The helpers are pre-created so
//     implementers can drop in concrete bus assertions once the bus lands.
//
//  3. Forward-doc scenario tests — the six scenario contracts from §10.2 /
//     EV-009–EV-014b, documented as skipping tests. Each test records exactly
//     which invariants the future bus implementation must satisfy. When the bus
//     lands, the implementer SHOULD replace t.SkipNow() with a concrete harness
//     invocation on a real or in-process bus.
//
// Requirement-traceable bead: hk-hqwn.61.
// Spec ref: event-model.md §4.7 EV-011a, §8.8.4 bus_overflow, §10.2.
package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Fixture helpers — busOverflowFixture* prefix per bead brief.
// ---------------------------------------------------------------------------

// busOverflowFixtureShedPolicy is a local mirror of the three shed-policy enum
// values declared in event-model.md §8.8.4. The bus implementation MUST emit
// exactly one of these strings in the bus_overflow payload's shed_policy field.
//
// "fsync-spilled" — F-class event could not queue; redirected to spill file
// per EV-011a.
// "ordinary-dropped" — O-class event could not queue; shed (loss accepted per
// EV-017 / EV-INV-002).
// "lossy-dropped" — L-class event could not queue; shed (loss accepted per
// EV-017 / EV-INV-002).
type busOverflowFixtureShedPolicy string

const (
	busOverflowFixtureShedPolicyFsyncSpilled    busOverflowFixtureShedPolicy = "fsync-spilled"
	busOverflowFixtureShedPolicyOrdinaryDropped busOverflowFixtureShedPolicy = "ordinary-dropped"
	busOverflowFixtureShedPolicyLossyDropped    busOverflowFixtureShedPolicy = "lossy-dropped"
)

// busOverflowFixtureConsumerDesc describes a single consumer in a scenario.
// It carries the consumer name, class, queue depth cap, and expected shed policy
// when the queue is saturated.
type busOverflowFixtureConsumerDesc struct {
	// Name is the consumer_name value that appears in bus_overflow payloads.
	Name string
	// Class is the consumer class (synchronous / asynchronous / observer).
	Class ConsumerClass
	// QueueDepth is the bounded queue depth cap (default 1024 per EV-011).
	QueueDepth int
	// ExpectedShedPolicy is the shed_policy the bus MUST emit in bus_overflow
	// when this consumer's queue is full and an event arrives.
	ExpectedShedPolicy busOverflowFixtureShedPolicy
}

// busOverflowFixtureScenario groups the consumer descriptions and event inputs
// for one scenario run. Implementers construct a real bus from the consumers,
// fill the queues, and then inject the overflow event, asserting that:
//   - bus_overflow is emitted with the correct consumer_name and shed_policy.
//   - F-class events appear in the spill file; O and L events are dropped.
type busOverflowFixtureScenario struct {
	// Name is the human-readable scenario label.
	Name string
	// Consumers are the subscriptions that must be registered before sealing.
	Consumers []busOverflowFixtureConsumerDesc
	// EventType is the §8 event type string of the overflow-triggering event.
	EventType string
	// EventDurabilityClass is "F", "O", or "L" — the durability class of the
	// event that triggers overflow (determines shed_policy).
	EventDurabilityClass string
}

// busOverflowFixtureBuildEvent returns a minimal valid Event with the given
// type, durability class annotation, and a fresh UUIDv7 event ID. The payload
// carries a single "scenario" key for test identification.
//
// Implementers: the durability class annotation here is advisory for test
// scaffolding; the actual bus reads durability class from the §8 registry at
// emit time. Align the registered event type's class with EventDurabilityClass
// when wiring up the real bus.
func busOverflowFixtureBuildEvent(t *testing.T, eventType, durabilityClass string) Event {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("busOverflowFixtureBuildEvent: uuid.NewV7: %v", err)
	}
	return Event{
		EventID:         EventID(id),
		SchemaVersion:   1,
		Type:            eventType,
		TimestampWall:   time.Now(),
		SourceSubsystem: "core.test.busoverflow",
		Payload:         []byte(`{"scenario":"` + durabilityClass + `"}`),
	}
}

// busOverflowFixtureSpillPath returns the expected spill-file path for a
// consumer under .harmonik/events/ per EV-011a.
//
// Spec: "spill-<consumer>.jsonl" at .harmonik/events/spill-<consumer>.jsonl.
func busOverflowFixtureSpillPath(harmonikDir, consumerName string) string {
	return filepath.Join(harmonikDir, "events", "spill-"+consumerName+".jsonl")
}

// busOverflowFixtureSaturatedAsyncConsumer returns the consumer descriptor for
// the standard asynchronous-consumer queue-saturation scenario (O-class event,
// ordinary-dropped shed policy).
func busOverflowFixtureSaturatedAsyncConsumer(t *testing.T) busOverflowFixtureConsumerDesc {
	t.Helper()
	return busOverflowFixtureConsumerDesc{
		Name:               "consumer-async-o",
		Class:              ConsumerClassAsynchronous,
		QueueDepth:         1024,
		ExpectedShedPolicy: busOverflowFixtureShedPolicyOrdinaryDropped,
	}
}

// busOverflowFixtureSaturatedObserverConsumer returns the consumer descriptor
// for the standard observer-consumer queue-saturation scenario (L-class event,
// lossy-dropped shed policy, per EV-014c).
func busOverflowFixtureSaturatedObserverConsumer(t *testing.T) busOverflowFixtureConsumerDesc {
	t.Helper()
	return busOverflowFixtureConsumerDesc{
		Name:               "consumer-observer-l",
		Class:              ConsumerClassObserver,
		QueueDepth:         1024,
		ExpectedShedPolicy: busOverflowFixtureShedPolicyLossyDropped,
	}
}

// busOverflowFixtureSaturatedFsyncConsumer returns the consumer descriptor for
// the fsync-boundary queue-saturation scenario (F-class event, fsync-spilled
// shed policy, per EV-011a spill-file path).
func busOverflowFixtureSaturatedFsyncConsumer(t *testing.T) busOverflowFixtureConsumerDesc {
	t.Helper()
	return busOverflowFixtureConsumerDesc{
		Name:               "consumer-async-f",
		Class:              ConsumerClassAsynchronous,
		QueueDepth:         1024,
		ExpectedShedPolicy: busOverflowFixtureShedPolicyFsyncSpilled,
	}
}

// busOverflowFixtureReservationDesc holds the parameters that describe the
// observer-queue reservation-slot behaviour (EV-011a §capacity-1 reservation).
//
// The bus MUST reserve one slot in every observer queue for bus_overflow itself
// so that it can always enqueue at least one overflow signal without a recursive
// fill check. When even that reservation is exhausted (queue full AND reservation
// consumed), the bus MUST fall back to direct JSONL append.
type busOverflowFixtureReservationDesc struct {
	// ObserverName is the consumer_name of the observer that holds the reservation.
	ObserverName string
	// QueueDepth is the full logical depth of the observer queue.
	QueueDepth int
	// ReservedSlots is the number of slots reserved for bus_overflow (spec: 1).
	ReservedSlots int
	// DirectAppendExpected indicates whether the scenario is expected to trigger
	// the direct JSONL append fallback (reservation exhausted path).
	DirectAppendExpected bool
}

// busOverflowFixtureReservationScenario returns the standard reservation-slot
// scenario with QueueDepth=8 (small, to make exhaustion reachable in tests).
// Reserve 1 slot per EV-011a; set DirectAppendExpected=true for the exhaustion
// sub-scenario.
func busOverflowFixtureReservationScenario(t *testing.T, exhausted bool) busOverflowFixtureReservationDesc {
	t.Helper()
	return busOverflowFixtureReservationDesc{
		ObserverName:         "consumer-observer-reservation",
		QueueDepth:           8,
		ReservedSlots:        1,
		DirectAppendExpected: exhausted,
	}
}

// ---------------------------------------------------------------------------
// Spec-content sensors — run unconditionally.
// ---------------------------------------------------------------------------

// TestBusOverflow_SpecContainsEV011a verifies that event-model.md §4.7 EV-011a
// is present and carries the three required policy terms.
//
// This sensor guards against silent removal or softening of the non-blocking
// back-pressure spec text. Any edit that removes EV-011a or its shed policies
// will fail this test, forcing a deliberate spec-amendment review.
//
// Spec ref: event-model.md §4.7 EV-011a (hk-hqwn.61).
func TestBusOverflow_SpecContainsEV011a(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate event-model.md")
	}
	// internal/core/busoverflow_hqwn61_test.go → ../../.. → repo root → specs/
	specPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "specs", "event-model.md")

	raw, err := os.ReadFile(specPath) //nolint:gosec // G304: specPath constructed from runtime.Caller + known relative segments; not user input
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v\nSpec must be present at repo root specs/event-model.md", specPath, err)
	}
	content := string(raw)

	// EV-011a section must be present.
	if !strings.Contains(content, "EV-011a") {
		t.Error("event-model.md does not contain \"EV-011a\"; " +
			"the non-blocking back-pressure section is missing from the spec")
	}

	// "Non-blocking producer back-pressure" is the normative heading of EV-011a.
	if !strings.Contains(content, "Non-blocking producer back-pressure") {
		t.Error("event-model.md does not contain the EV-011a heading " +
			"\"Non-blocking producer back-pressure\"; the section may have been renamed or removed")
	}

	// The three shed-policy values must appear in the spec.
	for _, policy := range []string{"fsync-spilled", "ordinary-dropped", "lossy-dropped"} {
		if !strings.Contains(content, policy) {
			t.Errorf("event-model.md does not contain shed_policy value %q; "+
				"spec §8.8.4 or EV-011a shed-policy enum may have been edited", policy)
		}
	}

	// "spill-<consumer>.jsonl" is the spill-file naming convention per EV-011a.
	if !strings.Contains(content, "spill-") {
		t.Error("event-model.md does not contain spill-file naming pattern " +
			"\"spill-<consumer>.jsonl\"; the EV-011a spill-file requirement may have been removed")
	}

	// The capacity-1 reservation guarantee must be stated.
	if !strings.Contains(content, "capacity-1 reservation") {
		t.Error("event-model.md does not contain \"capacity-1 reservation\"; " +
			"the EV-011a observer-queue reservation requirement may have been removed")
	}

	// The direct JSONL append fallback must be stated.
	if !strings.Contains(content, "direct JSONL append") {
		t.Error("event-model.md does not contain \"direct JSONL append\"; " +
			"the EV-011a reservation-exhausted fallback requirement may have been removed")
	}
}

// TestBusOverflow_SpecContainsBusOverflowPayload verifies that event-model.md
// §8.8.4 declares the bus_overflow payload with all six required fields.
//
// Spec ref: event-model.md §8.8.4 bus_overflow (hk-hqwn.61).
func TestBusOverflow_SpecContainsBusOverflowPayload(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate event-model.md")
	}
	specPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "specs", "event-model.md")

	raw, err := os.ReadFile(specPath) //nolint:gosec // G304: specPath constructed from runtime.Caller + known relative segments; not user input
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", specPath, err)
	}
	content := string(raw)

	// §8.8.4 must be present.
	if !strings.Contains(content, "bus_overflow") {
		t.Error("event-model.md does not contain event type \"bus_overflow\"; §8.8.4 is missing")
	}

	// Six required payload fields per §8.8.4.
	requiredFields := []string{
		"consumer_name",
		"event_type",
		"event_id",
		"queue_depth",
		"shed_at",
		"shed_policy",
	}
	for _, field := range requiredFields {
		if !strings.Contains(content, field) {
			t.Errorf("event-model.md §8.8.4 bus_overflow payload missing field %q", field)
		}
	}
}

// ---------------------------------------------------------------------------
// Forward-doc scenario tests — skip until bus implementation lands.
// ---------------------------------------------------------------------------

// TestBusOverflow_FClassEventSpillsOnQueueSaturation documents the EV-011a
// invariant for fsync-boundary (F) class events:
//
//	When a per-consumer queue is full AND the incoming event's durability class
//	is fsync-boundary, the bus MUST NOT shed the event. It MUST redirect it to
//	the consumer's spill file at .harmonik/events/spill-<consumer>.jsonl and
//	MUST emit bus_overflow{shed_policy: "fsync-spilled"}.
//
// Spec ref: event-model.md §4.7 EV-011a, §8.8.4 (hk-hqwn.61).
func TestBusOverflow_FClassEventSpillsOnQueueSaturation(t *testing.T) {
	// Fixture wired, pending bus implementation.
	desc := busOverflowFixtureSaturatedFsyncConsumer(t)

	t.Logf("EV-011a (hk-hqwn.61): F-class overflow scenario.")
	t.Logf("  Consumer: %q (class=%s, depth=%d)", desc.Name, desc.Class, desc.QueueDepth)
	t.Logf("  Event: durability class F (fsync-boundary)")
	t.Logf("  Expected shed_policy: %q", desc.ExpectedShedPolicy)
	t.Logf("  Expected action: event MUST be written to spill-%s.jsonl", desc.Name)
	t.Logf("  Expected bus_overflow emission: consumer_name=%q, shed_policy=%q",
		desc.Name, desc.ExpectedShedPolicy)
	t.Logf("")
	t.Logf("When bus implementation lands (hk-hqwn parent), the implementer SHOULD:")
	t.Logf("  1. Register a F-class event type in the test registry.")
	t.Logf("  2. Create a bus with consumer %q (depth=%d).", desc.Name, desc.QueueDepth)
	t.Logf("  3. Fill the consumer queue to capacity with stub events.")
	t.Logf("  4. Emit one more F-class event; call bus.Emit() from the fixture.")
	t.Logf("  5. Assert spill file exists at busOverflowFixtureSpillPath(dir, %q).", desc.Name)
	t.Logf("  6. Assert bus_overflow was emitted with shed_policy=%q.", desc.ExpectedShedPolicy)
	t.Logf("  7. Assert the producer was NOT blocked beyond one fsync write.")
	t.SkipNow()
}

// TestBusOverflow_OClassEventDroppedOnQueueSaturation documents the EV-011a
// invariant for ordinary (O) class events:
//
//	When a per-consumer queue is full AND the incoming event's durability class
//	is ordinary, the bus MUST shed (drop) the event for that consumer and MUST
//	emit bus_overflow{shed_policy: "ordinary-dropped"}.
//
// Spec ref: event-model.md §4.7 EV-011a, §8.8.4 (hk-hqwn.61).
func TestBusOverflow_OClassEventDroppedOnQueueSaturation(t *testing.T) {
	desc := busOverflowFixtureSaturatedAsyncConsumer(t)

	t.Logf("EV-011a (hk-hqwn.61): O-class overflow scenario.")
	t.Logf("  Consumer: %q (class=%s, depth=%d)", desc.Name, desc.Class, desc.QueueDepth)
	t.Logf("  Event: durability class O (ordinary)")
	t.Logf("  Expected shed_policy: %q", desc.ExpectedShedPolicy)
	t.Logf("  Expected action: event is dropped; no spill file written.")
	t.Logf("  Expected bus_overflow emission: consumer_name=%q, shed_policy=%q",
		desc.Name, desc.ExpectedShedPolicy)
	t.Logf("")
	t.Logf("When bus implementation lands, the implementer SHOULD:")
	t.Logf("  1. Register an O-class event type in the test registry.")
	t.Logf("  2. Create a bus with consumer %q (depth=%d).", desc.Name, desc.QueueDepth)
	t.Logf("  3. Fill queue to capacity.")
	t.Logf("  4. Emit one O-class event; assert it is NOT delivered to the consumer.")
	t.Logf("  5. Assert no spill file was written.")
	t.Logf("  6. Assert bus_overflow was emitted with shed_policy=%q.", desc.ExpectedShedPolicy)
	t.SkipNow()
}

// TestBusOverflow_LClassEventDroppedOnQueueSaturation documents the EV-011a /
// EV-014c invariant for lossy-tail-ok (L) class events:
//
//	When a per-observer queue is full AND the incoming event's durability class
//	is lossy-tail-ok, the bus MUST shed (drop) the event for that observer and
//	MUST emit bus_overflow{shed_policy: "lossy-dropped"}.
//
// Spec ref: event-model.md §4.7 EV-011a, §4.8 EV-014c, §8.8.4 (hk-hqwn.61).
func TestBusOverflow_LClassEventDroppedOnQueueSaturation(t *testing.T) {
	desc := busOverflowFixtureSaturatedObserverConsumer(t)

	t.Logf("EV-011a / EV-014c (hk-hqwn.61): L-class overflow scenario.")
	t.Logf("  Consumer: %q (class=%s, depth=%d)", desc.Name, desc.Class, desc.QueueDepth)
	t.Logf("  Event: durability class L (lossy-tail-ok)")
	t.Logf("  Expected shed_policy: %q", desc.ExpectedShedPolicy)
	t.Logf("  Expected action: event is dropped; no spill file written.")
	t.Logf("  Expected bus_overflow emission: consumer_name=%q, shed_policy=%q",
		desc.Name, desc.ExpectedShedPolicy)
	t.Logf("")
	t.Logf("When bus implementation lands, the implementer SHOULD:")
	t.Logf("  1. Register an L-class event type in the test registry.")
	t.Logf("  2. Create a bus with observer %q (depth=%d).", desc.Name, desc.QueueDepth)
	t.Logf("  3. Fill queue to capacity.")
	t.Logf("  4. Emit one L-class event; assert it is NOT delivered to the observer.")
	t.Logf("  5. Assert no spill file was written for L-class (dropped, not spilled).")
	t.Logf("  6. Assert bus_overflow was emitted with shed_policy=%q.", desc.ExpectedShedPolicy)
	t.SkipNow()
}

// TestBusOverflow_ReservationSlotConsumedThenExhausted documents the EV-011a
// capacity-1 reservation invariant and the direct-JSONL-append fallback:
//
//	The bus MUST reserve one dedicated slot in every observer queue for
//	bus_overflow itself (the capacity-1 reservation). This guarantees at least
//	one overflow signal can be enqueued without a recursive fill check.
//
//	When even the reservation slot is consumed (queue full AND reservation
//	consumed), the bus MUST fall back to a direct JSONL append with
//	fsync-boundary semantics for that single bus_overflow write (promoted from
//	O to F at write time). The promotion MUST be recorded in the structured-log
//	channel. This fallback blocks the producer for one write+fsync; this is the
//	accepted floor-price of signalling queue-space exhaustion to the operator.
//
// Spec ref: event-model.md §4.7 EV-011a reservation and fallback paragraphs
// (hk-hqwn.61).
func TestBusOverflow_ReservationSlotConsumedThenExhausted(t *testing.T) {
	normalDesc := busOverflowFixtureReservationScenario(t, false /* not exhausted */)
	exhaustedDesc := busOverflowFixtureReservationScenario(t, true /* exhausted */)

	t.Logf("EV-011a (hk-hqwn.61): reservation-slot consumption + exhaustion scenario.")
	t.Logf("")
	t.Logf("Sub-scenario A — reservation slot consumed (queue fill, one overflow):")
	t.Logf("  Observer: %q (depth=%d, reserved_slots=%d)",
		normalDesc.ObserverName, normalDesc.QueueDepth, normalDesc.ReservedSlots)
	t.Logf("  Fill queue to (depth - reserved_slots) = %d events.",
		normalDesc.QueueDepth-normalDesc.ReservedSlots)
	t.Logf("  Emit one overflow event; bus MUST use the reservation slot.")
	t.Logf("  Assert bus_overflow is enqueued (not direct-append) on the reservation slot.")
	t.Logf("  Assert direct_append_expected=%v.", normalDesc.DirectAppendExpected)
	t.Logf("")
	t.Logf("Sub-scenario B — reservation exhausted (both queue and reservation full):")
	t.Logf("  Observer: %q (depth=%d, reserved_slots=%d)",
		exhaustedDesc.ObserverName, exhaustedDesc.QueueDepth, exhaustedDesc.ReservedSlots)
	t.Logf("  Fill queue to full depth (all slots including reservation slot).")
	t.Logf("  Emit one overflow event; bus MUST fall back to direct JSONL append.")
	t.Logf("  Assert JSONL append file is written with fsync-boundary durability.")
	t.Logf("  Assert structured-log channel records the O→F promotion.")
	t.Logf("  Assert direct_append_expected=%v.", exhaustedDesc.DirectAppendExpected)
	t.Logf("")
	t.Logf("When bus implementation lands, the implementer SHOULD:")
	t.Logf("  1. Inject a small-queue observer (depth=8) using busOverflowFixtureReservationScenario.")
	t.Logf("  2. Drive sub-scenario A with a mock bus that can report slot usage.")
	t.Logf("  3. Drive sub-scenario B by filling all 8 slots before triggering overflow.")
	t.Logf("  4. Assert the direct-append writes a valid bus_overflow JSONL line to events.jsonl.")
	t.Logf("  5. Assert the structured-log promotion record is emitted.")
	t.SkipNow()
}

// TestBusOverflow_SpillFileMaterialization documents the EV-011a and EV-009
// pre-creation invariant for per-consumer spill files:
//
//	Spill files MUST be pre-created at subscription-registration time (EV-009)
//	with O_CREAT|O_APPEND|O_DSYNC semantics. Failure to create the spill file
//	MUST fail daemon startup with a typed error.
//
//	File path: .harmonik/events/spill-<consumer>.jsonl
//
// This test also documents the expected JSONL line format appended during
// F-class spill: each line is a complete Event envelope in JSON, one per line,
// with the shed_at timestamp and consumer_name annotation in the bus_overflow
// emission that accompanies the spill.
//
// Spec ref: event-model.md §4.7 EV-011a spill-file pre-creation, EV-009
// (hk-hqwn.61).
func TestBusOverflow_SpillFileMaterialization(t *testing.T) {
	consumerName := "consumer-async-f-spill"

	// busOverflowFixtureSpillPath documents the expected path for test
	// implementers wiring this scenario to a real .harmonik directory.
	exampleHarmonikDir := "/tmp/harmonik-example-run"
	expectedPath := busOverflowFixtureSpillPath(exampleHarmonikDir, consumerName)

	t.Logf("EV-011a / EV-009 (hk-hqwn.61): spill-file pre-creation scenario.")
	t.Logf("  Consumer: %q", consumerName)
	t.Logf("  Expected spill path: %q", expectedPath)
	t.Logf("  Open flags: O_CREAT | O_APPEND | O_DSYNC (per EV-011a).")
	t.Logf("  Pre-creation MUST happen at Subscribe() time, not at first spill.")
	t.Logf("  Failure to create MUST return a typed error that fails daemon startup.")
	t.Logf("")
	t.Logf("Concrete invariants the bus implementation MUST satisfy:")
	t.Logf("  1. After Subscribe(sub) for an asynchronous consumer returns (nil, sub),")
	t.Logf("     the file spill-%s.jsonl MUST exist under .harmonik/events/.", consumerName)
	t.Logf("  2. If the directory .harmonik/events/ is not writable, Subscribe MUST")
	t.Logf("     return a typed error (e.g., *SpillFileCreateError) and the daemon MUST")
	t.Logf("     refuse to accept the first Emit call (bus is sealed only on success).")
	t.Logf("  3. Each spilled F-class event MUST be appended as a complete JSON envelope")
	t.Logf("     line to the spill file, in arrival order.")
	t.Logf("  4. The corresponding bus_overflow emission MUST carry shed_policy=%q.",
		busOverflowFixtureShedPolicyFsyncSpilled)
	t.Logf("")
	t.Logf("When bus implementation lands, the implementer SHOULD:")
	t.Logf("  1. Create a temp dir via t.TempDir(); place .harmonik/events/ under it.")
	t.Logf("  2. Register a bus with the consumer, call Subscribe, and assert the file exists.")
	t.Logf("  3. Fill queue; emit an F-class event; assert the event appears in the spill file.")
	t.Logf("  4. Verify the spill file is append-only (no prior content is overwritten).")
	t.SkipNow()
}

// TestBusOverflow_BusOverflowEventIsObserverClass documents the EV-011a
// invariant that bus_overflow is itself an observer-class (O durability) event
// that uses the reserved observer queue slot — and therefore MUST NOT recursively
// trigger another bus_overflow when its own queue is full:
//
//	The bus MUST reserve a single dedicated slot (capacity-1 reservation) in
//	the observer queue that consumes bus_overflow. The reservation guarantees
//	the bus can enqueue at least one overflow signal per actual shed without a
//	recursive fill check.
//
// This test documents the anti-recursion invariant: bus_overflow MUST NOT cause
// bus_overflow via the normal fanout path.
//
// Spec ref: event-model.md §4.7 EV-011a reservation paragraph, §8.8.4
// (hk-hqwn.61).
func TestBusOverflow_BusOverflowEventIsObserverClass(t *testing.T) {
	t.Logf("EV-011a (hk-hqwn.61): bus_overflow anti-recursion invariant.")
	t.Logf("")
	t.Logf("Invariant: bus_overflow MUST NOT trigger a second bus_overflow via")
	t.Logf("the normal observer fanout path. The capacity-1 reservation slot bypasses")
	t.Logf("the recursive fill check, guaranteeing exactly one overflow signal per shed.")
	t.Logf("")
	t.Logf("From §8.8.4: bus_overflow has durability class O and is emitted as an")
	t.Logf("observability event. It MUST NOT be re-emitted as a consequence of its")
	t.Logf("own enqueue attempt failing (that path goes directly to JSONL append per")
	t.Logf("the reservation-exhausted fallback).")
	t.Logf("")
	t.Logf("When bus implementation lands, the implementer SHOULD:")
	t.Logf("  1. Install an overflow-counting observer that receives bus_overflow events.")
	t.Logf("  2. Trigger one overflow (fill queue, emit one ordinary event).")
	t.Logf("  3. Assert the overflow counter is exactly 1 (one bus_overflow emitted).")
	t.Logf("  4. Assert no second bus_overflow was emitted as a consequence of the first.")
	t.Logf("  5. Assert the observer's reservation slot is correctly restored after delivery.")
	t.SkipNow()
}
