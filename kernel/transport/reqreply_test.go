package transport

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// serveAsync runs Serve in a goroutine and, on test cleanup, cancels it and
// checks it returned cleanly. A canceled context is the normal way Serve stops,
// so that is not a failure; anything else is.
func serveAsync(t *testing.T, tr *Transport, pattern string, deliver func(*kernelv1.Envelope, string) error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tr.Serve(ctx, pattern, deliver) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Serve(%q) returned %v", pattern, err)
		}
	})
}

// waitServing blocks until a live server is attached to channel, so a test can
// publish a Request without racing the Serve goroutine's registration. It fails
// the test rather than hang forever.
func waitServing(t *testing.T, tr *Transport, channel string) {
	t.Helper()
	for i := 0; i < 2000; i++ {
		if tr.ServesChannel(channel) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no server ever registered for %q", channel)
}

// A question gets its one correlated answer, payload opaque in both directions.
func TestRequestRoundTripsACorrelatedReply(t *testing.T) {
	tr := New("box-1")
	if err := tr.Declare("rpc.mirror", kernelv1.ChannelType_CHANNEL_TYPE_REQUEST_REPLY); err != nil {
		t.Fatalf("Declare: %v", err)
	}

	serveAsync(t, tr, "rpc.mirror", func(env *kernelv1.Envelope, requestID string) error {
		return tr.Respond(requestID, append([]byte("re:"), env.GetPayload()...), map[string]string{"answered": "1"}, "")
	})
	waitServing(t, tr, "rpc.mirror")

	question := []byte{0x00, 0xff, 0x10}
	resp, err := tr.Request(withDeadline(t), &kernelv1.PublishRequest{Channel: "rpc.mirror", Payload: question}, "producer-1")
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if resp.GetInterest() != kernelv1.Interest_INTEREST_PRESENT {
		t.Fatalf("Interest = %v, want INTEREST_PRESENT", resp.GetInterest())
	}
	want := append([]byte("re:"), question...)
	if !bytes.Equal(resp.GetEnvelope().GetPayload(), want) {
		t.Fatalf("answer payload = %x, want %x", resp.GetEnvelope().GetPayload(), want)
	}
	if resp.GetEnvelope().GetHeaders()["answered"] != "1" {
		t.Fatalf("answer headers = %v", resp.GetEnvelope().GetHeaders())
	}
}

// With a server attached but never answering, the caller's deadline ends the
// wait — a real timeout, not a silent hang and not an immediate INTEREST_NONE.
func TestRequestTimesOutWhenTheServerNeverAnswers(t *testing.T) {
	tr := New("box-1")
	if err := tr.Declare("rpc.slow", kernelv1.ChannelType_CHANNEL_TYPE_REQUEST_REPLY); err != nil {
		t.Fatalf("Declare: %v", err)
	}

	// Take each question but never Respond — the caller must time out.
	serveAsync(t, tr, "rpc.slow", func(_ *kernelv1.Envelope, _ string) error { return nil })
	waitServing(t, tr, "rpc.slow")

	ctx, timeoutCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer timeoutCancel()
	_, err := tr.Request(ctx, &kernelv1.PublishRequest{Channel: "rpc.slow", Payload: []byte("x")}, "producer-1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Request err = %v, want context.DeadlineExceeded", err)
	}
	// The timed-out request must leave nothing behind.
	tr.rr.mu.Lock()
	open := len(tr.rr.open)
	queued := len(tr.rr.byChannel["rpc.slow"].queue)
	tr.rr.mu.Unlock()
	if open != 0 {
		t.Fatalf("open correlations after timeout = %d, want 0 (leak)", open)
	}
	if queued != 0 {
		t.Fatalf("queued questions after timeout = %d, want 0", queued)
	}
}

// No server attached: INTEREST_NONE comes back at once, not after a timeout.
func TestRequestReturnsInterestNoneWithNoServer(t *testing.T) {
	tr := New("box-1")
	if err := tr.Declare("rpc.nobody", kernelv1.ChannelType_CHANNEL_TYPE_REQUEST_REPLY); err != nil {
		t.Fatalf("Declare: %v", err)
	}

	// A long deadline: a correct INTEREST_NONE returns well before it.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	resp, err := tr.Request(ctx, &kernelv1.PublishRequest{Channel: "rpc.nobody", Payload: []byte("x")}, "producer-1")
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if resp.GetInterest() != kernelv1.Interest_INTEREST_NONE {
		t.Fatalf("Interest = %v, want INTEREST_NONE", resp.GetInterest())
	}
	if resp.GetEnvelope() != nil {
		t.Fatalf("INTEREST_NONE carried an envelope: %v", resp.GetEnvelope())
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("INTEREST_NONE took %v — it waited instead of failing fast", elapsed)
	}
}

// A Respond for a request_id the transport holds no open correlation for is a
// typed refusal, not a silent no-op.
func TestRespondToAnUnknownRequestIsATypedRefusal(t *testing.T) {
	tr := New("box-1")
	err := tr.Respond("never-minted", []byte("x"), nil, "")
	if !errors.Is(err, ErrUnknownRequest) {
		t.Fatalf("Respond to unknown id: err = %v, want ErrUnknownRequest", err)
	}
}

// A responder that sets a non-empty error fails the requester's call with that
// message, rather than returning an answer.
func TestRespondWithAnErrorFailsTheRequest(t *testing.T) {
	tr := New("box-1")
	if err := tr.Declare("rpc.fail", kernelv1.ChannelType_CHANNEL_TYPE_REQUEST_REPLY); err != nil {
		t.Fatalf("Declare: %v", err)
	}

	serveAsync(t, tr, "rpc.fail", func(_ *kernelv1.Envelope, requestID string) error {
		return tr.Respond(requestID, nil, nil, "handler refused")
	})
	waitServing(t, tr, "rpc.fail")

	_, err := tr.Request(withDeadline(t), &kernelv1.PublishRequest{Channel: "rpc.fail", Payload: []byte("x")}, "producer-1")
	if !errors.Is(err, ErrNoResponder) {
		t.Fatalf("Request err = %v, want it to wrap ErrNoResponder", err)
	}
	if !bytes.Contains([]byte(err.Error()), []byte("handler refused")) {
		t.Fatalf("Request err = %q, want it to carry the responder's message", err.Error())
	}
}

// Request and Serve refuse a channel that is not REQUEST_REPLY, and an
// undeclared one, rather than silently doing nothing.
func TestRequestReplyRefusesTheWrongChannelType(t *testing.T) {
	tr := New("box-1")
	if err := tr.Declare("topic.x", kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB); err != nil {
		t.Fatalf("Declare: %v", err)
	}

	if _, err := tr.Request(withDeadline(t), &kernelv1.PublishRequest{Channel: "topic.x"}, "p"); !errors.Is(err, ErrChannelTypeNotImplemented) {
		t.Fatalf("Request on a PUBSUB channel: err = %v, want ErrChannelTypeNotImplemented", err)
	}
	if err := tr.Serve(withDeadline(t), "topic.x", func(*kernelv1.Envelope, string) error { return nil }); !errors.Is(err, ErrChannelTypeNotImplemented) {
		t.Fatalf("Serve on a PUBSUB channel: err = %v, want ErrChannelTypeNotImplemented", err)
	}
	if _, err := tr.Request(withDeadline(t), &kernelv1.PublishRequest{Channel: "nope"}, "p"); !errors.Is(err, ErrChannelNotDeclared) {
		t.Fatalf("Request on an undeclared channel: err = %v, want ErrChannelNotDeclared", err)
	}
}
