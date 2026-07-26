package handlercontract_test

import (
	"context"
	"io"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// sessionFixtureStub is a minimal Session implementation used only to verify
// the interface method-set. It is NOT a usable Session; all methods panic.
//
// Helper prefix: sessionFixture (per implementer-protocol.md §Helper-prefix discipline).
type sessionFixtureStub struct{}

func (sessionFixtureStub) ID() core.SessionID { return "" }
func (sessionFixtureStub) SendInput(_ context.Context, _ string) error {
	return nil
}

func (sessionFixtureStub) Attach(_ context.Context) (io.Reader, error) {
	return nil, nil
}
func (sessionFixtureStub) Kill(_ context.Context) error { return nil }
func (sessionFixtureStub) Wait(_ context.Context) (core.Outcome, error) {
	return core.Outcome{}, nil
}
func (sessionFixtureStub) LogLocation() string { return "" }

// Compile-time assertion that sessionFixtureStub satisfies the Session
// interface. The blank identifier is deliberate: the declaration exists purely
// so the build fails when the interface and the stub drift apart.
var _ handlercontract.Session = sessionFixtureStub{}

// sessionFixtureAssertType fails to compile unless got is assignable to T. It
// replaces the `var _ T = expr` idiom used by the return-type conformance tests
// below, which staticcheck (QF1011) reads as a redundant type annotation rather
// than the deliberate assertion it is.
func sessionFixtureAssertType[T any](_ T) {}

// TestSession_MethodSetConformance verifies that the Session interface
// is declared with the expected 6-method surface
// (specs/handler-contract.md §6.1, HC-002, bead hk-8i31.72).
//
// The test compiles only if sessionFixtureStub satisfies the interface,
// which means the interface shape is exactly the 6-method set below.
func TestSession_MethodSetConformance(t *testing.T) {
	var s handlercontract.Session = sessionFixtureStub{}

	// ID() -> core.SessionID
	id := s.ID()
	_ = id

	// SendInput(ctx, input) -> error
	err := s.SendInput(context.Background(), "")
	_ = err

	// Attach(ctx) -> (io.Reader, error)
	r, err := s.Attach(context.Background())
	_ = r
	_ = err

	// Kill(ctx) -> error
	err = s.Kill(context.Background())
	_ = err

	// Wait(ctx) -> (core.Outcome, error)
	outcome, err := s.Wait(context.Background())
	_ = outcome
	_ = err

	// LogLocation() -> string
	loc := s.LogLocation()
	_ = loc
}

// TestSession_IDReturnType verifies that ID() returns core.SessionID (not a
// raw string), enforcing the typed-alias discipline from hk-8i31.75.
func TestSession_IDReturnType(t *testing.T) {
	var s handlercontract.Session = sessionFixtureStub{}
	sessionFixtureAssertType[core.SessionID](s.ID())
}

// TestSession_WaitReturnType verifies that Wait() returns core.Outcome (not a
// raw struct), enforcing the typed record from hk-b3f.79.
func TestSession_WaitReturnType(t *testing.T) {
	var s handlercontract.Session = sessionFixtureStub{}
	outcome, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: unexpected error: %v", err)
	}
	sessionFixtureAssertType[core.Outcome](outcome)
}
