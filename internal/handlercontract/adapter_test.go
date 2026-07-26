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
func (adapterFixtureStub) Diagnose(_ context.Context) (handlercontract.DiagnosticReport, error) {
	return handlercontract.DiagnosticReport{}, handlercontract.ErrDeterministic
}

// Compile-time assertion that adapterFixtureStub satisfies the Adapter
// interface. The blank identifier is deliberate: the declaration exists purely
// so the build fails when the interface and the stub drift apart.
var _ handlercontract.Adapter = adapterFixtureStub{}

// adapterFixtureAssertType fails to compile unless got is assignable to T. It
// replaces the `var _ T = expr` idiom used by the return-type conformance tests
// below, which staticcheck (QF1011) reads as a redundant type annotation rather
// than the deliberate assertion it is.
func adapterFixtureAssertType[T any](_ T) {}

// TestAdapter_MethodSetConformance verifies that the Adapter interface is
// declared with the expected 5-method surface
// (specs/handler-contract.md §6.1, §4.3.HC-013, §4.3a.HC-014a, bead hk-8i31.73).
//
// The test compiles only if adapterFixtureStub satisfies the interface, which
// means the interface shape is exactly the 5-method set below.
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

	// Diagnose(ctx) -> (DiagnosticReport, error)
	report, err := a.Diagnose(context.Background())
	_ = report
	_ = err
}

// TestAdapter_DetectReadyReturnType verifies that DetectReady returns bool
// (specs/handler-contract.md §6.1 Adapter).
func TestAdapter_DetectReadyReturnType(t *testing.T) {
	var a handlercontract.Adapter = adapterFixtureStub{}
	adapterFixtureAssertType[bool](a.DetectReady(core.EventEnvelope{}))
}

// TestAdapter_DetectRateLimitReturnTypes verifies that DetectRateLimit returns
// (bool, time.Duration) (specs/handler-contract.md §6.1 Adapter).
func TestAdapter_DetectRateLimitReturnTypes(t *testing.T) {
	var a handlercontract.Adapter = adapterFixtureStub{}
	limited, retryAfter := a.DetectRateLimit(core.EventEnvelope{})
	adapterFixtureAssertType[bool](limited)
	adapterFixtureAssertType[time.Duration](retryAfter)
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
	// Compile-time parameter-type check; the stub's nil return is also asserted
	// so a signature change to a non-error result cannot pass silently.
	if err := a.CleanExitSequence(context.Background(), s); err != nil {
		t.Fatalf("CleanExitSequence: unexpected error: %v", err)
	}
}
