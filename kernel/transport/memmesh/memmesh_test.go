package memmesh_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
	"github.com/gregberns/harmonik/kernel/transport"
	"github.com/gregberns/harmonik/kernel/transport/memmesh"
)

func deadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func addNode(t *testing.T, m *memmesh.Mesh, name string) *memmesh.Node {
	t.Helper()
	n, err := m.AddNode(name)
	if err != nil {
		t.Fatalf("AddNode(%q): %v", name, err)
	}
	return n
}

func declare(t *testing.T, n *memmesh.Node, channel string, typ kernelv1.ChannelType) {
	t.Helper()
	if err := n.Declare(channel, typ); err != nil {
		t.Fatalf("Declare(%q): %v", channel, err)
	}
}

func subscribe(t *testing.T, n *memmesh.Node, id, channel string, group ...string) *transport.Subscription {
	t.Helper()
	sub, err := n.Subscribe(id, channel, group...)
	if err != nil {
		t.Fatalf("Subscribe(%q, %q): %v", id, channel, err)
	}
	return sub
}

// serveAsync runs a node's Serve in a goroutine and, on cleanup, cancels it and
// checks it stopped cleanly. A canceled context is the normal stop, not a
// failure.
func serveAsync(t *testing.T, n *memmesh.Node, pattern string, deliver func(*kernelv1.Envelope, string) error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- n.Serve(ctx, pattern, deliver) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Serve(%q) returned %v", pattern, err)
		}
	})
}

func recv(t *testing.T, sub *transport.Subscription) *kernelv1.Envelope {
	t.Helper()
	env, err := sub.Recv(deadline(t))
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	return env
}

// A publish on node A reaches a subscriber on node B carrying node A's
// origin_node, and its origin_seq climbs monotonically per (A, channel). The
// origin node's own subscriber sees the same stamped envelope — fan-out
// includes the origin.
func TestPubSubFansOutAcrossNodesWithOriginProvenance(t *testing.T) {
	m := memmesh.New()
	a := addNode(t, m, "node-a")
	b := addNode(t, m, "node-b")

	declare(t, a, "topic.x", kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB)
	subA := subscribe(t, a, "here", "topic.x")
	subB := subscribe(t, b, "there", "topic.x")

	var wantSeq uint64
	for i := 0; i < 2; i++ {
		wantSeq++
		resp, err := a.Publish(&kernelv1.PublishRequest{Channel: "topic.x", Payload: []byte{byte(i)}}, "producer-a")
		if err != nil {
			t.Fatalf("Publish #%d: %v", i, err)
		}
		if resp.GetInterest() != kernelv1.Interest_INTEREST_PRESENT {
			t.Fatalf("Publish #%d interest = %v, want INTEREST_PRESENT", i, resp.GetInterest())
		}
		if resp.GetOriginSeq() != wantSeq {
			t.Fatalf("Publish #%d origin_seq = %d, want %d", i, resp.GetOriginSeq(), wantSeq)
		}
	}

	// Node B's subscriber sees A as the origin, in seq order 1 then 2.
	for i := uint64(1); i <= 2; i++ {
		env := recv(t, subB)
		if env.GetOriginNode() != "node-a" {
			t.Fatalf("B recv #%d origin_node = %q, want node-a", i, env.GetOriginNode())
		}
		if env.GetOriginSeq() != i {
			t.Fatalf("B recv #%d origin_seq = %d, want %d", i, env.GetOriginSeq(), i)
		}
	}
	// The origin's own subscriber got the fan-out too, same provenance.
	envA := recv(t, subA)
	if envA.GetOriginNode() != "node-a" || envA.GetOriginSeq() != 1 {
		t.Fatalf("A recv = (node %q, seq %d), want (node-a, 1)", envA.GetOriginNode(), envA.GetOriginSeq())
	}
}

// Ten POINT_TO_POINT messages published on the declaring node A, with one
// member each on nodes B and C, split five-and-five: competing consumers across
// nodes against the declaring kernel's single group queue.
func TestPointToPointCompetesFiveAndFiveAcrossNodes(t *testing.T) {
	m := memmesh.New()
	a := addNode(t, m, "node-a")
	b := addNode(t, m, "node-b")
	c := addNode(t, m, "node-c")

	declare(t, a, "queue.work", kernelv1.ChannelType_CHANNEL_TYPE_POINT_TO_POINT)
	// Both members use the same local id "worker"; they are different members
	// because they live on different nodes.
	subB := subscribe(t, b, "worker", "queue.work", "workers")
	subC := subscribe(t, c, "worker", "queue.work", "workers")

	const total = 10
	sent := make(map[string]bool, total)
	for i := 0; i < total; i++ {
		resp, err := a.Publish(&kernelv1.PublishRequest{Channel: "queue.work", Payload: []byte{byte(i)}}, "producer-a")
		if err != nil {
			t.Fatalf("Publish #%d: %v", i, err)
		}
		sent[resp.GetMessageId()] = true
	}

	// Round-robin assignment over two members is deterministic: each holds five.
	if got := subB.Pending(); got != total/2 {
		t.Fatalf("B pending = %d, want %d", got, total/2)
	}
	if got := subC.Pending(); got != total/2 {
		t.Fatalf("C pending = %d, want %d", got, total/2)
	}

	got := make(map[string]bool, total)
	for i := 0; i < total/2; i++ {
		got[recv(t, subB).GetMessageId()] = true
		got[recv(t, subC).GetMessageId()] = true
	}
	if len(got) != total {
		t.Fatalf("received %d distinct messages, want %d (a message was dropped or duplicated)", len(got), total)
	}
	for id := range sent {
		if !got[id] {
			t.Fatalf("published message %q was never delivered", id)
		}
	}
}

// The first (name, type) declared anywhere in the mesh wins; a conflicting type
// from a second node is a typed refusal, while the same type from it is fine.
func TestConflictingTypeFromAnotherNodeIsRejected(t *testing.T) {
	m := memmesh.New()
	a := addNode(t, m, "node-a")
	b := addNode(t, m, "node-b")

	declare(t, a, "chan.z", kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB)

	err := b.Declare("chan.z", kernelv1.ChannelType_CHANNEL_TYPE_POINT_TO_POINT)
	if !errors.Is(err, memmesh.ErrChannelTypeConflict) {
		t.Fatalf("conflicting declare from node B: err = %v, want ErrChannelTypeConflict", err)
	}
	if err := b.Declare("chan.z", kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB); err != nil {
		t.Fatalf("same-type re-declare from node B: %v", err)
	}
}

// A member nacks its leased message through the mesh; the declaring kernel
// reassigns it to the live member on the OTHER node, which then acks it. This
// exercises the Ack/Nack routing to the declaring kernel and cross-node
// reassignment.
func TestNackReassignsToAMemberOnAnotherNode(t *testing.T) {
	m := memmesh.New()
	a := addNode(t, m, "node-a")
	b := addNode(t, m, "node-b")
	c := addNode(t, m, "node-c")

	declare(t, a, "queue.work", kernelv1.ChannelType_CHANNEL_TYPE_POINT_TO_POINT)
	subB := subscribe(t, b, "worker", "queue.work", "workers")
	subC := subscribe(t, c, "worker", "queue.work", "workers")

	resp, err := a.Publish(&kernelv1.PublishRequest{Channel: "queue.work", Payload: []byte{0x01}}, "producer-a")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Attach order B then C, so the first message goes to B.
	first := recv(t, subB)
	if first.GetMessageId() != resp.GetMessageId() {
		t.Fatalf("B got %q, want published %q", first.GetMessageId(), resp.GetMessageId())
	}
	if err := b.Nack("worker", first.GetMessageId()); err != nil {
		t.Fatalf("B Nack: %v", err)
	}

	// The nack reassigned it away from B, to C.
	if got := subB.Pending(); got != 0 {
		t.Fatalf("B pending after nack = %d, want 0 (it must not keep what it failed)", got)
	}
	reassigned := recv(t, subC)
	if reassigned.GetMessageId() != resp.GetMessageId() {
		t.Fatalf("C got %q, want the reassigned %q", reassigned.GetMessageId(), resp.GetMessageId())
	}
	if err := c.Ack("worker", reassigned.GetMessageId()); err != nil {
		t.Fatalf("C Ack: %v", err)
	}
}

// A REQUEST_REPLY question asked on node A reaches a server on node B and its
// one answer comes back correlated, payload opaque both ways. The channel is
// declared on A; the server lives on B — the request is routed to the node that
// holds the server, answered there, and returned to A.
func TestRequestReplyReachesAServerOnAnotherNode(t *testing.T) {
	m := memmesh.New()
	a := addNode(t, m, "node-a")
	b := addNode(t, m, "node-b")

	declare(t, a, "rpc.mirror", kernelv1.ChannelType_CHANNEL_TYPE_REQUEST_REPLY)

	serveAsync(t, b, "rpc.mirror", func(env *kernelv1.Envelope, requestID string) error {
		return b.Respond(requestID, append([]byte("re:"), env.GetPayload()...), nil, "")
	})

	question := []byte{0x01, 0x02, 0x03}
	want := append([]byte("re:"), question...)

	// Retry until B's server has registered: Request returns INTEREST_NONE at
	// once while no node serves the channel, so a NONE here means "not ready
	// yet", and a PRESENT means the correlated answer is in hand.
	for i := 0; i < 2000; i++ {
		resp, err := a.Request(deadline(t), &kernelv1.PublishRequest{Channel: "rpc.mirror", Payload: question}, "producer-a")
		if err != nil {
			t.Fatalf("Request: %v", err)
		}
		if resp.GetInterest() == kernelv1.Interest_INTEREST_NONE {
			time.Sleep(time.Millisecond)
			continue
		}
		if !bytes.Equal(resp.GetEnvelope().GetPayload(), want) {
			t.Fatalf("answer payload = %x, want %x", resp.GetEnvelope().GetPayload(), want)
		}
		return
	}
	t.Fatal("node-b's server never became reachable from node-a")
}

// With no server anywhere in the mesh, a Request returns INTEREST_NONE at once,
// never a silent wait.
func TestRequestReturnsInterestNoneWhenNoNodeServes(t *testing.T) {
	m := memmesh.New()
	a := addNode(t, m, "node-a")
	addNode(t, m, "node-b")

	declare(t, a, "rpc.empty", kernelv1.ChannelType_CHANNEL_TYPE_REQUEST_REPLY)

	resp, err := a.Request(deadline(t), &kernelv1.PublishRequest{Channel: "rpc.empty", Payload: []byte("x")}, "producer-a")
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if resp.GetInterest() != kernelv1.Interest_INTEREST_NONE {
		t.Fatalf("Interest = %v, want INTEREST_NONE", resp.GetInterest())
	}
}

// A publish or subscribe on a channel no node declared is a typed refusal, not
// a panic or a silent drop.
func TestUndeclaredChannelIsRefused(t *testing.T) {
	m := memmesh.New()
	a := addNode(t, m, "node-a")

	if _, err := a.Publish(&kernelv1.PublishRequest{Channel: "nope"}, "producer-a"); !errors.Is(err, memmesh.ErrChannelNotDeclared) {
		t.Fatalf("Publish undeclared: err = %v, want ErrChannelNotDeclared", err)
	}
	if _, err := a.Subscribe("id", "nope"); !errors.Is(err, memmesh.ErrChannelNotDeclared) {
		t.Fatalf("Subscribe undeclared: err = %v, want ErrChannelNotDeclared", err)
	}
}
