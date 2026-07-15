package handler

import (
	"context"
	"errors"
	"io"
	"testing"
)

// fakeSubSession is a minimal SubstrateSession that does NOT satisfy InputPort.
type fakeSubSession struct{}

func (fakeSubSession) Kill(context.Context) error { return nil }
func (fakeSubSession) Wait(context.Context) error { return nil }
func (fakeSubSession) Outcome() Outcome           { return Outcome{} }
func (fakeSubSession) PID() int                   { return 0 }
func (fakeSubSession) Stdout() io.Reader          { return nil }

// fakeInputSession is a SubstrateSession that ALSO satisfies InputPort, recording
// the last SubmitInput/CloseInput call for assertions.
type fakeInputSession struct {
	fakeSubSession
	lastPayload []byte
	closed      bool
	ack         Ack
	submitErr   error
}

func (f *fakeInputSession) SubmitInput(_ context.Context, req InputRequest) (Ack, error) {
	f.lastPayload = req.Payload
	return f.ack, f.submitErr
}

func (f *fakeInputSession) CloseInput(context.Context) error {
	f.closed = true
	return nil
}

func TestAsInputPort_structuralAssertion(t *testing.T) {
	if _, ok := AsInputPort(fakeSubSession{}); ok {
		t.Fatal("AsInputPort: a session without SubmitInput/CloseInput must report ok=false")
	}
	if _, ok := AsInputPort(&fakeInputSession{}); !ok {
		t.Fatal("AsInputPort: a session satisfying InputPort must report ok=true")
	}
}

func TestSubstrateAdapter_SendInput_unsupported(t *testing.T) {
	a := &substrateSessionAdapter{inner: fakeSubSession{}}
	err := a.SendInput(context.Background(), "hello")
	if !errors.Is(err, ErrInputUnsupported) {
		t.Fatalf("SendInput on a non-InputPort session: got %v, want ErrInputUnsupported", err)
	}
	if !errors.Is(err, ErrDeterministic) {
		t.Fatalf("ErrInputUnsupported must wrap ErrDeterministic (HC-069); got %v", err)
	}
	// CloseStdin on a non-InputPort session is a legitimate nil (no write-end pipe).
	if err := a.CloseStdin(); err != nil {
		t.Fatalf("CloseStdin on a non-InputPort session: got %v, want nil", err)
	}
}

func TestSubstrateAdapter_routesToInputPort(t *testing.T) {
	inner := &fakeInputSession{ack: Ack{Outcome: Delivered}}
	a := &substrateSessionAdapter{inner: inner}

	if err := a.SendInput(context.Background(), "payload"); err != nil {
		t.Fatalf("SendInput routing to SubmitInput: unexpected err %v", err)
	}
	if string(inner.lastPayload) != "payload" {
		t.Fatalf("SendInput did not route payload to SubmitInput: got %q", inner.lastPayload)
	}
	if err := a.CloseStdin(); err != nil {
		t.Fatalf("CloseStdin routing to CloseInput: unexpected err %v", err)
	}
	if !inner.closed {
		t.Fatal("CloseStdin did not route to InputPort.CloseInput")
	}
}

func TestSubstrateAdapter_SendInput_propagatesSubmitError(t *testing.T) {
	sentinel := errors.New("submit failed")
	inner := &fakeInputSession{submitErr: sentinel}
	a := &substrateSessionAdapter{inner: inner}
	if err := a.SendInput(context.Background(), "x"); !errors.Is(err, sentinel) {
		t.Fatalf("SendInput must propagate SubmitInput error; got %v", err)
	}
}

func TestDeliveryOutcome_String(t *testing.T) {
	if Delivered.String() != "delivered" || Rejected.String() != "rejected" {
		t.Fatalf("DeliveryOutcome.String mismatch: %q / %q", Delivered.String(), Rejected.String())
	}
}
