package core

import "fmt"

// OnPanic is the policy controlling what the bus does when a consumer's
// goroutine panics (event-model.md §6.1 ENUM on_panic, OQ-EV-007).
//
// The default policy is OnPanicRecoverAndLog per OQ-EV-007.
// The enum is closed at MVH; future variants extend via the amendment
// protocol per [architecture.md §4.6].
//
// Note: quarantine_consumer and fail_daemon are declared in the Subscription
// record but their enforcement semantics are deferred post-testing.md per
// OQ-EV-007. MVH implements recover_and_log behaviour.
type OnPanic string

// OnPanic values per event-model.md §6.1 OQ-EV-007.
const (
	// OnPanicRecoverAndLog recovers the panic, emits consumer_failed with
	// error_category=panic, and continues dispatching to other consumers.
	// This is the default policy per OQ-EV-007.
	OnPanicRecoverAndLog OnPanic = "recover_and_log"

	// OnPanicQuarantineConsumer additionally suspends dispatch to the panicking
	// consumer for the rest of the daemon cycle (declared; enforcement deferred
	// per OQ-EV-007).
	OnPanicQuarantineConsumer OnPanic = "quarantine_consumer"

	// OnPanicFailDaemon escalates to daemon_startup_failed. Inappropriate for
	// MVH default (declared; enforcement deferred per OQ-EV-007).
	OnPanicFailDaemon OnPanic = "fail_daemon"
)

// Valid reports whether p is one of the three declared OnPanic constants
// at MVH.
func (p OnPanic) Valid() bool {
	switch p {
	case OnPanicRecoverAndLog, OnPanicQuarantineConsumer, OnPanicFailDaemon:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so OnPanic serialises
// correctly in JSON and YAML.
// It rejects any value that is not one of the three declared constants at MVH.
func (p OnPanic) MarshalText() ([]byte, error) {
	if !p.Valid() {
		return nil, fmt.Errorf("onpanic: unknown value %q", string(p))
	}
	return []byte(p), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the three declared constants at MVH.
func (p *OnPanic) UnmarshalText(text []byte) error {
	v := OnPanic(text)
	if !v.Valid() {
		return fmt.Errorf(
			"onpanic: unknown value %q; must be one of recover_and_log, quarantine_consumer, fail_daemon",
			string(text),
		)
	}
	*p = v
	return nil
}
