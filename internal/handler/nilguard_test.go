package handler_test

// nilguard_test.go — nil-argument guard tests for handler.NewHandler.
//
// Bead: hk-gql20.16.
// Helper prefix: nilguardFixture (per implementer-protocol.md §Helper-prefix
// discipline).
//
// Verifies that NewHandler panics with the expected message when publisher,
// deadLetter, or registry is nil — all three are required daemon-configuration
// invariants with no recovery path.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// nilguardFixturePub returns a non-nil EventEmitter for use in nil-guard tests.
func nilguardFixturePub() handlercontract.EventEmitter {
	return &handlercontract.CollectingEmitter{}
}

// nilguardFixtureDL returns a non-nil WatcherDeadLetterSink for use in nil-guard tests.
func nilguardFixtureDL() handlercontract.WatcherDeadLetterSink {
	return handlercontract.NoopWatcherDeadLetter{}
}

// nilguardFixtureReg returns a non-nil AdapterRegistry for use in nil-guard tests.
func nilguardFixtureReg() *handlercontract.AdapterRegistry {
	return handlercontract.NewAdapterRegistry()
}

// nilguardFixturePanicMsg runs f and returns the recovered panic value as a
// string. Returns "" if f does not panic.
func nilguardFixturePanicMsg(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			if s, ok := r.(string); ok {
				msg = s
			}
		}
	}()
	f()
	return ""
}

// TestNewHandler_NilPublisher_Panics verifies that NewHandler panics with the
// expected message when publisher is nil.
func TestNewHandler_NilPublisher_Panics(t *testing.T) {
	t.Parallel()

	const want = "handler: NewHandler: publisher is nil — daemon defect"
	got := nilguardFixturePanicMsg(func() {
		handler.NewHandler(nil, nilguardFixtureDL(), nilguardFixtureReg())
	})
	if got == "" {
		t.Fatal("NewHandler(nil publisher): expected panic, did not panic")
	}
	if got != want {
		t.Errorf("NewHandler(nil publisher): panic message = %q, want %q", got, want)
	}
}

// TestNewHandler_NilDeadLetter_Panics verifies that NewHandler panics with the
// expected message when deadLetter is nil.
func TestNewHandler_NilDeadLetter_Panics(t *testing.T) {
	t.Parallel()

	const want = "handler: NewHandler: deadLetter is nil — daemon defect"
	got := nilguardFixturePanicMsg(func() {
		handler.NewHandler(nilguardFixturePub(), nil, nilguardFixtureReg())
	})
	if got == "" {
		t.Fatal("NewHandler(nil deadLetter): expected panic, did not panic")
	}
	if got != want {
		t.Errorf("NewHandler(nil deadLetter): panic message = %q, want %q", got, want)
	}
}

// TestNewHandler_NilRegistry_Panics verifies that NewHandler panics with the
// expected message when registry is nil (hk-gql20.16).
func TestNewHandler_NilRegistry_Panics(t *testing.T) {
	t.Parallel()

	const want = "handler: NewHandler: registry is nil — daemon defect"
	got := nilguardFixturePanicMsg(func() {
		handler.NewHandler(nilguardFixturePub(), nilguardFixtureDL(), nil)
	})
	if got == "" {
		t.Fatal("NewHandler(nil registry): expected panic, did not panic")
	}
	if got != want {
		t.Errorf("NewHandler(nil registry): panic message = %q, want %q", got, want)
	}
}
