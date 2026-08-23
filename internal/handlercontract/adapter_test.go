package handlercontract_test

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

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

var _ handlercontract.Adapter = adapterFixtureStub{}

func adapterFixtureAssertType[T any](_ T) {}

// TestAdapter_MethodSetConformance verifies that the Adapter interface is
// declared with the expected 5-method surface
// (specs/handler-contract.md §6.1, §4.3.HC-013, §4.3a.HC-014a, bead hk-8i31.73).
//
// The test compiles only if adapterFixtureStub satisfies the interface, which
// means the interface shape is exactly the 5-method set below.
func TestAdapter_MethodSetConformance(t *testing.T) {
	var a handlercontract.Adapter = adapterFixtureStub{}

	ready := a.DetectReady(core.EventEnvelope{})
	_ = ready

	limited, retryAfter := a.DetectRateLimit(core.EventEnvelope{})
	_ = limited
	_ = retryAfter

	err := a.CleanExitSequence(context.Background(), sessionFixtureStub{})
	_ = err

	err = a.RotateAccount(context.Background())
	_ = err

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
	if err := a.CleanExitSequence(context.Background(), s); err != nil {
		t.Fatalf("CleanExitSequence: unexpected error: %v", err)
	}
}
