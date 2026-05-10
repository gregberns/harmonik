package handlercontract_test

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// adapterFixtureStub is a minimal Adapter implementation used only to verify
// the interface method-set. It is NOT a usable Adapter; all methods return
// zero values.
//
// Helper prefix: adapterFixture (per implementer-protocol.md §Helper-prefix discipline).
type adapterFixtureStub struct{}

func (adapterFixtureStub) DetectReady(_ core.EventEnvelope) bool { return false }
func (adapterFixtureStub) DetectRateLimit(_ core.EventEnvelope) (bool, time.Duration) {
	return false, 0
}
func (adapterFixtureStub) CleanExitSequence(_ context.Context, _ handlercontract.Session) error {
	return nil
}
func (adapterFixtureStub) RotateAccount(_ context.Context) error { return nil }

// adapterFixtureAssertImplements is a compile-time assertion that
// adapterFixtureStub satisfies the Adapter interface.
var adapterFixtureAssertImplements handlercontract.Adapter = adapterFixtureStub{}

// TestAdapter_MethodSetConformance verifies that the Adapter interface is
// declared with the expected 4-method surface
// (specs/handler-contract.md §6.1, §4.3.HC-013, bead hk-8i31.73).
//
// The test compiles only if adapterFixtureStub satisfies the interface, which
// means the interface shape is exactly the 4-method set below.
func TestAdapter_MethodSetConformance(t *testing.T) {
	var a handlercontract.Adapter = adapterFixtureStub{}

	// DetectReady(event) -> bool
	ready := a.DetectReady(core.EventEnvelope{})
	_ = ready

	// DetectRateLimit(event) -> (bool, time.Duration)
	limited, retryAfter := a.DetectRateLimit(core.EventEnvelope{})
	_ = limited
	_ = retryAfter

	// CleanExitSequence(ctx, session) -> error
	err := a.CleanExitSequence(context.Background(), sessionFixtureStub{})
	_ = err

	// RotateAccount(ctx) -> error
	err = a.RotateAccount(context.Background())
	_ = err
}

// TestAdapter_DetectReadyReturnType verifies that DetectReady returns bool
// (specs/handler-contract.md §6.1 Adapter).
func TestAdapter_DetectReadyReturnType(t *testing.T) {
	var a handlercontract.Adapter = adapterFixtureStub{}
	var _ bool = a.DetectReady(core.EventEnvelope{}) // compile-time type check
}

// TestAdapter_DetectRateLimitReturnTypes verifies that DetectRateLimit returns
// (bool, time.Duration) (specs/handler-contract.md §6.1 Adapter).
func TestAdapter_DetectRateLimitReturnTypes(t *testing.T) {
	var a handlercontract.Adapter = adapterFixtureStub{}
	limited, retryAfter := a.DetectRateLimit(core.EventEnvelope{})
	var _ bool = limited             // compile-time type check
	var _ time.Duration = retryAfter // compile-time type check
}

// TestAdapter_DetectReadyEventParam verifies that DetectReady accepts a
// core.EventEnvelope (specs/handler-contract.md §6.1 Adapter; event-model.md §4.1).
func TestAdapter_DetectReadyEventParam(t *testing.T) {
	var a handlercontract.Adapter = adapterFixtureStub{}
	var ev core.EventEnvelope
	_ = a.DetectReady(ev) // compile-time parameter-type check
}

// TestAdapter_DetectRateLimitEventParam verifies that DetectRateLimit accepts a
// core.EventEnvelope (specs/handler-contract.md §6.1 Adapter; event-model.md §4.1).
func TestAdapter_DetectRateLimitEventParam(t *testing.T) {
	var a handlercontract.Adapter = adapterFixtureStub{}
	var ev core.EventEnvelope
	_, _ = a.DetectRateLimit(ev) // compile-time parameter-type check
}

// TestAdapter_CleanExitSequenceSessionParam verifies that CleanExitSequence
// accepts a handlercontract.Session (specs/handler-contract.md §6.1 Adapter).
func TestAdapter_CleanExitSequenceSessionParam(t *testing.T) {
	var a handlercontract.Adapter = adapterFixtureStub{}
	var s handlercontract.Session = sessionFixtureStub{}
	_ = a.CleanExitSequence(context.Background(), s) // compile-time parameter-type check
}
