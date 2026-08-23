package handler_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

func nilguardFixturePub() handlercontract.EventEmitter {
	return &handlercontract.CollectingEmitter{}
}

func nilguardFixtureDL() handlercontract.WatcherDeadLetterSink {
	return handlercontract.NoopWatcherDeadLetter{}
}

func nilguardFixtureReg() *handlercontract.AdapterRegistry {
	return handlercontract.NewAdapterRegistry()
}

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
