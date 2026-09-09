package transport

import (
	"bytes"
	"context"
	"testing"
	"time"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

func withDeadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestPublishRoundTripsOpaquePayloadAndStampsProvenance(t *testing.T) {
	tr := New("box-1")
	if err := tr.Declare("a.one", kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB); err != nil {
		t.Fatalf("Declare: %v", err)
	}

	sub, err := tr.Subscribe("subscriber-1", "a.one")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	payload := []byte{0x00, 0xff, 0x10, 0xde, 0xad}
	resp, err := tr.Publish(&kernelv1.PublishRequest{
		Channel: "a.one",
		Payload: payload,
		Headers: map[string]string{"k": "v"},
	}, "producer-1")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if resp.GetMessageId() == "" {
		t.Fatal("Publish response carries no message_id")
	}
	if resp.GetInterest() != kernelv1.Interest_INTEREST_PRESENT {
		t.Fatalf("Interest = %v, want INTEREST_PRESENT", resp.GetInterest())
	}

	env, err := sub.Recv(withDeadline(t))
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}

	if !bytes.Equal(env.GetPayload(), payload) {
		t.Fatalf("payload round-trip: got %x, want %x", env.GetPayload(), payload)
	}
	if env.GetHeaders()["k"] != "v" {
		t.Fatalf("headers round-trip: got %v", env.GetHeaders())
	}
	if env.GetOriginNode() != "box-1" {
		t.Fatalf("OriginNode = %q, want box-1", env.GetOriginNode())
	}
	if env.GetProducer() != "producer-1" {
		t.Fatalf("Producer = %q, want producer-1", env.GetProducer())
	}
	if env.GetMessageId() != resp.GetMessageId() {
		t.Fatalf("envelope message_id %q != response message_id %q", env.GetMessageId(), resp.GetMessageId())
	}
	if env.GetOriginSeq() != 1 {
		t.Fatalf("OriginSeq = %d, want 1", env.GetOriginSeq())
	}
	if env.GetOriginTime() == nil {
		t.Fatal("OriginTime is unset")
	}
}

func TestOriginSeqMonotonicPerChannel(t *testing.T) {
	tr := New("box-1")
	if err := tr.Declare("a.one", kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB); err != nil {
		t.Fatalf("Declare: %v", err)
	}
	sub, err := tr.Subscribe("subscriber-1", "a.one")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	for i := 1; i <= 3; i++ {
		if _, err := tr.Publish(&kernelv1.PublishRequest{Channel: "a.one", Payload: []byte{byte(i)}}, "producer-1"); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}

	for want := uint64(1); want <= 3; want++ {
		env, err := sub.Recv(withDeadline(t))
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		if env.GetOriginSeq() != want {
			t.Fatalf("OriginSeq = %d, want %d", env.GetOriginSeq(), want)
		}
	}
}

func TestDeclareRejectsAConflictingTypeButAcceptsARepeat(t *testing.T) {
	tr := New("box-1")
	if err := tr.Declare("a.one", kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB); err != nil {
		t.Fatalf("first Declare: %v", err)
	}
	if err := tr.Declare("a.one", kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB); err != nil {
		t.Fatalf("repeat Declare of the same type: %v", err)
	}
	err := tr.Declare("a.one", kernelv1.ChannelType_CHANNEL_TYPE_POINT_TO_POINT)
	if err == nil {
		t.Fatal("Declare with a conflicting type: want an error, got nil")
	}
}

func TestNonPubsubChannelTypesAreRefusedNotSilentlyDropped(t *testing.T) {
	// POINT_TO_POINT used to be in this list; B1 implements it, so its
	// behavior is now covered by the PTP tests in ptp_test.go. The two types
	// this slice still does not carry stay a typed refusal, never a silent no-op.
	for _, typ := range []kernelv1.ChannelType{
		kernelv1.ChannelType_CHANNEL_TYPE_REQUEST_REPLY,
		kernelv1.ChannelType_CHANNEL_TYPE_LOOKUP,
	} {
		tr := New("box-1")
		if err := tr.Declare("a.one", typ); err != nil {
			t.Fatalf("Declare(%v): %v", typ, err)
		}

		if _, err := tr.Publish(&kernelv1.PublishRequest{Channel: "a.one", Payload: []byte("x")}, "producer-1"); err == nil {
			t.Fatalf("Publish on %v channel: want an error, got nil", typ)
		}
		if _, err := tr.Subscribe("subscriber-1", "a.one"); err == nil {
			t.Fatalf("Subscribe on %v channel: want an error, got nil", typ)
		}
	}
}

func TestQueueOutlivesDetachAndReattach(t *testing.T) {
	tr := New("box-1")
	if err := tr.Declare("a.one", kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB); err != nil {
		t.Fatalf("Declare: %v", err)
	}

	if _, err := tr.Subscribe("subscriber-1", "a.one"); err != nil {
		t.Fatalf("first Subscribe: %v", err)
	}
	// The subscriber above is discarded here without ever calling Recv,
	// simulating a plugin process that has gone away. The transport keeps
	// the subscription and its queue regardless.

	const n = 3
	for i := 0; i < n; i++ {
		if _, err := tr.Publish(&kernelv1.PublishRequest{Channel: "a.one", Payload: []byte{byte(i)}}, "producer-1"); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}

	// Reattach: a new call with the same subscriber id, standing in for a
	// reloaded plugin process picking its subscription back up.
	sub, err := tr.Subscribe("subscriber-1", "a.one")
	if err != nil {
		t.Fatalf("reattach Subscribe: %v", err)
	}
	if got := sub.Pending(); got != n {
		t.Fatalf("Pending() after reattach = %d, want %d", got, n)
	}

	for i := 0; i < n; i++ {
		env, err := sub.Recv(withDeadline(t))
		if err != nil {
			t.Fatalf("Recv %d: %v", i, err)
		}
		if len(env.GetPayload()) != 1 || env.GetPayload()[0] != byte(i) {
			t.Fatalf("Recv %d: got payload %x, want %v", i, env.GetPayload(), []byte{byte(i)})
		}
	}
}

func TestPublishRejectsAPayloadOverTheMax(t *testing.T) {
	tr := New("box-1")
	if err := tr.Declare("a.one", kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB); err != nil {
		t.Fatalf("Declare: %v", err)
	}

	oversized := make([]byte, MaxPayloadBytes+1)
	if _, err := tr.Publish(&kernelv1.PublishRequest{Channel: "a.one", Payload: oversized}, "producer-1"); err == nil {
		t.Fatal("Publish over MaxPayloadBytes: want an error, got nil")
	}
}

func TestInfoReportsMaxPayloadBytes(t *testing.T) {
	tr := New("box-1")
	info := tr.Info()
	if info.GetMaxPayloadBytes() != MaxPayloadBytes {
		t.Fatalf("Info().MaxPayloadBytes = %d, want %d", info.GetMaxPayloadBytes(), MaxPayloadBytes)
	}
	if info.GetNode() != "box-1" {
		t.Fatalf("Info().Node = %q, want box-1", info.GetNode())
	}
}
