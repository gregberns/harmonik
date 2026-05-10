package specaudit_test

// hk-hqwn.57 type-shape sensor — EventBus 6-method interface per §6.1.
//
// Spec ref: specs/event-model.md §6.1 INTERFACE EventBus.
//
// The EventBus interface (specs/event-model.md §6.1) declares six methods:
//
//   - Emit(ctx, type, payload) -> error
//   - Subscribe(sub Subscription) -> (Subscription, error)
//   - Seal() -> error
//   - ReplayFrom(consumer_id, since event_id) -> error
//   - DeadLetterReplay(consumer_name, filter?) -> error
//   - Drain(ctx) -> error
//
// This sensor verifies the Go interface in internal/eventbus carries exactly
// these six methods with the correct parameter and return types so that
// spec and code cannot silently diverge.
//
// # Audit frame
//
// The test uses the reflect package to inspect the eventbus.EventBus interface
// type at compile time (via interface satisfaction) and at runtime (method-set
// walk). A private sentinel implementation forcibly satisfies the interface;
// the compiler refuses to build this file if the method set diverges from what
// the sentinel provides. The runtime walk then asserts that no extra or missing
// methods are present.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn57Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// hqwn57FixtureExpectedMethods is the exact set of method names the
// specs/event-model.md §6.1 INTERFACE EventBus requires.
var hqwn57FixtureExpectedMethods = []string{
	"DeadLetterReplay",
	"Drain",
	"Emit",
	"ReplayFrom",
	"Seal",
	"Subscribe",
}

// hqwn57FixtureSentinel is a compile-time check: if eventbus.EventBus gains or
// loses methods, or any method signature changes, this struct will fail to
// satisfy the interface and the package will not compile.
//
// The sentinel is never instantiated at runtime; its sole purpose is the
// var _ = assignment below.
type hqwn57FixtureSentinel struct{}

func (hqwn57FixtureSentinel) Emit(_ context.Context, _ core.EventType, _ []byte) error {
	return nil
}

func (hqwn57FixtureSentinel) Subscribe(_ core.Subscription) (core.Subscription, error) {
	return core.Subscription{}, nil
}

func (hqwn57FixtureSentinel) Seal() error { return nil }

func (hqwn57FixtureSentinel) ReplayFrom(_ string, _ core.EventID) error { return nil }

func (hqwn57FixtureSentinel) DeadLetterReplay(_ string, _ *core.EventPattern) error { return nil }

func (hqwn57FixtureSentinel) Drain(_ context.Context) error { return nil }

// hqwn57FixtureCompileTimeCheck asserts that hqwn57FixtureSentinel satisfies
// eventbus.EventBus at compile time. If the interface changes in a way that
// breaks this assignment, the package will not compile and the mismatch is
// surfaced immediately.
var _ eventbus.EventBus = hqwn57FixtureSentinel{}

// TestHQWN57EventBusInterfaceMethodSet is the type-shape sensor for hk-hqwn.57.
//
// It verifies at runtime that the eventbus.EventBus interface carries exactly
// the 6 methods declared in specs/event-model.md §6.1, no more and no fewer.
func TestHQWN57EventBusInterfaceMethodSet(t *testing.T) {
	t.Parallel()

	ifaceType := reflect.TypeOf((*eventbus.EventBus)(nil)).Elem()

	actual := make([]string, 0, ifaceType.NumMethod())
	for i := 0; i < ifaceType.NumMethod(); i++ {
		actual = append(actual, ifaceType.Method(i).Name)
	}
	sort.Strings(actual)

	expected := make([]string, len(hqwn57FixtureExpectedMethods))
	copy(expected, hqwn57FixtureExpectedMethods)
	sort.Strings(expected)

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf(
			"hk-hqwn.57 FAILED: EventBus method set mismatch\n"+
				"  spec:   specs/event-model.md §6.1 — 6 methods: %v\n"+
				"  actual: %v\n"+
				"  detail: the interface in internal/eventbus/eventbus.go must carry exactly "+
				"the 6 methods declared in specs/event-model.md §6.1: "+
				"Emit, Subscribe, Seal, ReplayFrom, DeadLetterReplay, Drain",
			expected, actual,
		)
	} else {
		t.Logf("hk-hqwn.57 sensor PASS — EventBus method set matches §6.1: %v", actual)
	}
}

// TestHQWN57EmitSignature verifies the Emit method has the expected signature:
// Emit(ctx context.Context, eventType core.EventType, payload []byte) error.
func TestHQWN57EmitSignature(t *testing.T) {
	t.Parallel()

	ifaceType := reflect.TypeOf((*eventbus.EventBus)(nil)).Elem()
	m, ok := ifaceType.MethodByName("Emit")
	if !ok {
		t.Fatal("hk-hqwn.57 Emit: method not found on EventBus interface")
	}

	mt := m.Type // func(context.Context, core.EventType, []byte) error

	// 3 params, 1 return
	if mt.NumIn() != 3 {
		t.Errorf("Emit: want 3 params, got %d", mt.NumIn())
	}
	if mt.NumOut() != 1 {
		t.Errorf("Emit: want 1 return, got %d", mt.NumOut())
	}
	if mt.NumIn() >= 1 && mt.In(0) != reflect.TypeOf((*context.Context)(nil)).Elem() {
		t.Errorf("Emit param[0]: want context.Context, got %v", mt.In(0))
	}
	if mt.NumIn() >= 2 && mt.In(1) != reflect.TypeOf(core.EventType("")) {
		t.Errorf("Emit param[1]: want core.EventType, got %v", mt.In(1))
	}
	if mt.NumIn() >= 3 && mt.In(2) != reflect.TypeOf([]byte(nil)) {
		t.Errorf("Emit param[2]: want []byte, got %v", mt.In(2))
	}
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if mt.NumOut() >= 1 && !mt.Out(0).Implements(errorType) {
		t.Errorf("Emit return[0]: want error, got %v", mt.Out(0))
	}
}

// TestHQWN57SubscribeSignature verifies Subscribe(sub core.Subscription) (core.Subscription, error).
func TestHQWN57SubscribeSignature(t *testing.T) {
	t.Parallel()

	ifaceType := reflect.TypeOf((*eventbus.EventBus)(nil)).Elem()
	m, ok := ifaceType.MethodByName("Subscribe")
	if !ok {
		t.Fatal("hk-hqwn.57 Subscribe: method not found on EventBus interface")
	}

	mt := m.Type

	if mt.NumIn() != 1 {
		t.Errorf("Subscribe: want 1 param, got %d", mt.NumIn())
	}
	if mt.NumOut() != 2 {
		t.Errorf("Subscribe: want 2 returns, got %d", mt.NumOut())
	}
	subType := reflect.TypeOf(core.Subscription{})
	if mt.NumIn() >= 1 && mt.In(0) != subType {
		t.Errorf("Subscribe param[0]: want core.Subscription, got %v", mt.In(0))
	}
	if mt.NumOut() >= 1 && mt.Out(0) != subType {
		t.Errorf("Subscribe return[0]: want core.Subscription, got %v", mt.Out(0))
	}
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if mt.NumOut() >= 2 && !mt.Out(1).Implements(errorType) {
		t.Errorf("Subscribe return[1]: want error, got %v", mt.Out(1))
	}
}

// TestHQWN57ReplayFromSignature verifies ReplayFrom(consumerID string, since core.EventID) error.
func TestHQWN57ReplayFromSignature(t *testing.T) {
	t.Parallel()

	ifaceType := reflect.TypeOf((*eventbus.EventBus)(nil)).Elem()
	m, ok := ifaceType.MethodByName("ReplayFrom")
	if !ok {
		t.Fatal("hk-hqwn.57 ReplayFrom: method not found on EventBus interface")
	}

	mt := m.Type

	if mt.NumIn() != 2 {
		t.Errorf("ReplayFrom: want 2 params, got %d", mt.NumIn())
	}
	if mt.NumOut() != 1 {
		t.Errorf("ReplayFrom: want 1 return, got %d", mt.NumOut())
	}
	if mt.NumIn() >= 1 && mt.In(0) != reflect.TypeOf("") {
		t.Errorf("ReplayFrom param[0]: want string, got %v", mt.In(0))
	}
	if mt.NumIn() >= 2 && mt.In(1) != reflect.TypeOf(core.EventID{}) {
		t.Errorf("ReplayFrom param[1]: want core.EventID, got %v", mt.In(1))
	}
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if mt.NumOut() >= 1 && !mt.Out(0).Implements(errorType) {
		t.Errorf("ReplayFrom return[0]: want error, got %v", mt.Out(0))
	}
}

// TestHQWN57DeadLetterReplaySignature verifies DeadLetterReplay(consumerName string, filter *core.EventPattern) error.
func TestHQWN57DeadLetterReplaySignature(t *testing.T) {
	t.Parallel()

	ifaceType := reflect.TypeOf((*eventbus.EventBus)(nil)).Elem()
	m, ok := ifaceType.MethodByName("DeadLetterReplay")
	if !ok {
		t.Fatal("hk-hqwn.57 DeadLetterReplay: method not found on EventBus interface")
	}

	mt := m.Type

	if mt.NumIn() != 2 {
		t.Errorf("DeadLetterReplay: want 2 params, got %d", mt.NumIn())
	}
	if mt.NumOut() != 1 {
		t.Errorf("DeadLetterReplay: want 1 return, got %d", mt.NumOut())
	}
	if mt.NumIn() >= 1 && mt.In(0) != reflect.TypeOf("") {
		t.Errorf("DeadLetterReplay param[0]: want string, got %v", mt.In(0))
	}
	epPtrType := reflect.TypeOf((*core.EventPattern)(nil))
	if mt.NumIn() >= 2 && mt.In(1) != epPtrType {
		t.Errorf("DeadLetterReplay param[1]: want *core.EventPattern, got %v", mt.In(1))
	}
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if mt.NumOut() >= 1 && !mt.Out(0).Implements(errorType) {
		t.Errorf("DeadLetterReplay return[0]: want error, got %v", mt.Out(0))
	}
}
