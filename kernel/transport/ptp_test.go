package transport

import (
	"errors"
	"testing"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// ptpChan is the one POINT_TO_POINT channel the helpers below declare and
// drive. A neutral substrate name: no domain noun.
const ptpChan = "d.work"

// ptpTransport declares ptpChan as POINT_TO_POINT and returns the transport.
func ptpTransport(t *testing.T) *Transport {
	t.Helper()
	tr := New("box-1")
	if err := tr.Declare(ptpChan, kernelv1.ChannelType_CHANNEL_TYPE_POINT_TO_POINT); err != nil {
		t.Fatalf("Declare: %v", err)
	}
	return tr
}

func ptpSubscribe(t *testing.T, tr *Transport, id, group string) *Subscription {
	t.Helper()
	sub, err := tr.Subscribe(id, ptpChan, group)
	if err != nil {
		t.Fatalf("Subscribe(%q, %q, %q): %v", id, ptpChan, group, err)
	}
	return sub
}

func ptpPublish(t *testing.T, tr *Transport, payload byte) {
	t.Helper()
	if _, err := tr.Publish(&kernelv1.PublishRequest{Channel: ptpChan, Payload: []byte{payload}}, "producer-1"); err != nil {
		t.Fatalf("Publish(%d): %v", payload, err)
	}
}

// recvByte receives one envelope and returns its single-byte payload.
func recvByte(t *testing.T, sub *Subscription) byte {
	t.Helper()
	env := recvEnv(t, sub)
	if len(env.GetPayload()) != 1 {
		t.Fatalf("payload length = %d, want 1", len(env.GetPayload()))
	}
	return env.GetPayload()[0]
}

// recvEnv receives one envelope and returns it whole.
func recvEnv(t *testing.T, sub *Subscription) *kernelv1.Envelope {
	t.Helper()
	env, err := sub.Recv(withDeadline(t))
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	return env
}

// TestPtpTenEnvelopesAcrossTwoMembersLandFiveFiveInOrder is G1 at the unit
// level: round-robin assignment is deterministic, so the two members split the
// ten in a fixed interleave and each keeps its slice in order.
func TestPtpTenEnvelopesAcrossTwoMembersLandFiveFiveInOrder(t *testing.T) {
	tr := ptpTransport(t)
	m0 := ptpSubscribe(t, tr, "m0", "workers")
	m1 := ptpSubscribe(t, tr, "m1", "workers")

	for i := byte(0); i < 10; i++ {
		ptpPublish(t, tr, i)
	}

	if got := m0.Pending(); got != 5 {
		t.Fatalf("m0.Pending() = %d, want 5", got)
	}
	if got := m1.Pending(); got != 5 {
		t.Fatalf("m1.Pending() = %d, want 5", got)
	}

	// Round-robin from an empty cursor: m0 takes the even indices, m1 the odd.
	for _, want := range []byte{0, 2, 4, 6, 8} {
		if got := recvByte(t, m0); got != want {
			t.Fatalf("m0 receive order: got %d, want %d", got, want)
		}
	}
	for _, want := range []byte{1, 3, 5, 7, 9} {
		if got := recvByte(t, m1); got != want {
			t.Fatalf("m1 receive order: got %d, want %d", got, want)
		}
	}
}

// TestPtpNackReassignsToTheOtherMemberExactlyOnce is the C5 minimum at unit
// scale: a failed member's message moves to a survivor and the failed member
// keeps no copy of it.
func TestPtpNackReassignsToTheOtherMemberExactlyOnce(t *testing.T) {
	tr := ptpTransport(t)
	m0 := ptpSubscribe(t, tr, "m0", "workers")
	m1 := ptpSubscribe(t, tr, "m1", "workers")

	// One message: round-robin hands it to m0.
	ptpPublish(t, tr, 42)
	env := recvEnv(t, m0)
	if env.GetPayload()[0] != 42 {
		t.Fatalf("m0 first receive = %d, want 42", env.GetPayload()[0])
	}

	// m0 fails it. It must reassign to m1, not back to m0.
	if err := tr.Nack("m0", env.GetMessageId()); err != nil {
		t.Fatalf("Nack: %v", err)
	}

	if got := m0.Pending(); got != 0 {
		t.Fatalf("m0.Pending() after nack = %d, want 0 (the failed member keeps no copy)", got)
	}
	if n := tr.memberLeasedCount(t, "m0"); n != 0 {
		t.Fatalf("m0 leased after nack = %d, want 0", n)
	}
	if got := recvByte(t, m1); got != 42 {
		t.Fatalf("m1 receive after reassign = %d, want 42", got)
	}
	// Exactly once: nothing left anywhere.
	if got := m1.Pending(); got != 0 {
		t.Fatalf("m1.Pending() after receiving the reassigned message = %d, want 0", got)
	}
}

// TestPtpDetachReassignsLeasedAndQueued covers the detach verb: a member that
// goes away nacks BOTH its in-flight leased work and its still-queued work back
// to the group, and every one reaches the survivor.
func TestPtpDetachReassignsLeasedAndQueued(t *testing.T) {
	tr := ptpTransport(t)

	// Attach only m0, publish 5 — all five land on m0 (the sole member).
	m0 := ptpSubscribe(t, tr, "m0", "workers")
	for i := byte(0); i < 5; i++ {
		ptpPublish(t, tr, i)
	}

	// m0 receives 3 (now leased) and leaves 2 queued.
	for i := 0; i < 3; i++ {
		recvByte(t, m0)
	}
	if got := m0.Pending(); got != 2 {
		t.Fatalf("m0.Pending() = %d, want 2 queued", got)
	}
	if n := tr.memberLeasedCount(t, "m0"); n != 3 {
		t.Fatalf("m0 leased = %d, want 3", n)
	}

	// A second member joins, then m0 detaches (a reload or a crash).
	m1 := ptpSubscribe(t, tr, "m1", "workers")
	if err := tr.Detach("m0"); err != nil {
		t.Fatalf("Detach: %v", err)
	}

	// All five — 3 leased + 2 queued — reach m1.
	got := map[byte]bool{}
	for i := 0; i < 5; i++ {
		got[recvByte(t, m1)] = true
	}
	if len(got) != 5 {
		t.Fatalf("m1 received %d distinct payloads, want 5", len(got))
	}
	for b := byte(0); b < 5; b++ {
		if !got[b] {
			t.Fatalf("m1 never received payload %d; the detach stranded it", b)
		}
	}
	if m1.Pending() != 0 {
		t.Fatalf("m1.Pending() after draining = %d, want 0", m1.Pending())
	}
}

// TestPtpSingleMemberDrainsTheWholeQueue is the degenerate 1:1 case the channel
// comment names: with one member the group is a plain queue.
func TestPtpSingleMemberDrainsTheWholeQueue(t *testing.T) {
	tr := ptpTransport(t)
	m0 := ptpSubscribe(t, tr, "m0", "workers")

	for i := byte(0); i < 4; i++ {
		ptpPublish(t, tr, i)
	}
	if got := m0.Pending(); got != 4 {
		t.Fatalf("m0.Pending() = %d, want 4", got)
	}
	for want := byte(0); want < 4; want++ {
		if got := recvByte(t, m0); got != want {
			t.Fatalf("single-member receive order: got %d, want %d", got, want)
		}
	}
}

// TestPtpAckForgetsTheLease confirms a completed message is dropped, not
// requeued.
func TestPtpAckForgetsTheLease(t *testing.T) {
	tr := ptpTransport(t)
	m0 := ptpSubscribe(t, tr, "m0", "workers")

	ptpPublish(t, tr, 7)
	env := recvEnv(t, m0)
	if err := tr.Ack("m0", env.GetMessageId()); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if n := tr.memberLeasedCount(t, "m0"); n != 0 {
		t.Fatalf("leased after ack = %d, want 0", n)
	}
	if got := m0.Pending(); got != 0 {
		t.Fatalf("Pending after ack = %d, want 0 (an ack forgets, never requeues)", got)
	}
}

func TestPtpSubscribeRefusals(t *testing.T) {
	const pubsub = "d.status"
	tr := ptpTransport(t)
	if err := tr.Declare(pubsub, kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB); err != nil {
		t.Fatalf("Declare pubsub: %v", err)
	}

	if _, err := tr.Subscribe("m0", ptpChan); !errors.Is(err, ErrGroupRequired) {
		t.Fatalf("PTP subscribe with no group: got %v, want ErrGroupRequired", err)
	}
	if _, err := tr.Subscribe("m0", pubsub, "workers"); !errors.Is(err, ErrGroupOnNonPointToPoint) {
		t.Fatalf("PUBSUB subscribe with a group: got %v, want ErrGroupOnNonPointToPoint", err)
	}
	if _, err := tr.Subscribe("m0", ptpChan, "a", "b"); !errors.Is(err, ErrTooManyGroups) {
		t.Fatalf("subscribe with two groups: got %v, want ErrTooManyGroups", err)
	}
}

func TestPtpPublishGroupKeyIsNotImplemented(t *testing.T) {
	tr := ptpTransport(t)
	ptpSubscribe(t, tr, "m0", "workers")

	_, err := tr.Publish(&kernelv1.PublishRequest{Channel: ptpChan, Payload: []byte("x"), GroupKey: "k1"}, "producer-1")
	if !errors.Is(err, ErrGroupKeyNotImplemented) {
		t.Fatalf("Publish with group_key: got %v, want ErrGroupKeyNotImplemented", err)
	}
}

func TestPtpAckNackRefusals(t *testing.T) {
	const pubsub = "d.status"
	tr := ptpTransport(t)
	if err := tr.Declare(pubsub, kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB); err != nil {
		t.Fatalf("Declare pubsub: %v", err)
	}
	ptpSubscribe(t, tr, "m0", "workers")
	if _, err := tr.Subscribe("s0", pubsub); err != nil {
		t.Fatalf("pubsub subscribe: %v", err)
	}

	if err := tr.Ack("nobody", "mid"); !errors.Is(err, ErrUnknownSubscriber) {
		t.Fatalf("Ack unknown subscriber: got %v, want ErrUnknownSubscriber", err)
	}
	if err := tr.Nack("s0", "mid"); !errors.Is(err, ErrNotPointToPoint) {
		t.Fatalf("Nack on a pubsub subscriber: got %v, want ErrNotPointToPoint", err)
	}
	if err := tr.Ack("m0", "never-leased"); !errors.Is(err, ErrNotLeased) {
		t.Fatalf("Ack of a message never leased: got %v, want ErrNotLeased", err)
	}
	if err := tr.Detach("nobody"); !errors.Is(err, ErrUnknownSubscriber) {
		t.Fatalf("Detach unknown subscriber: got %v, want ErrUnknownSubscriber", err)
	}
}

// TestPtpEachGroupGetsACopy confirms competing-consumer scope: one message
// reaches one member of EACH group, so two groups each see it once.
func TestPtpEachGroupGetsACopy(t *testing.T) {
	tr := ptpTransport(t)
	a := ptpSubscribe(t, tr, "a0", "group-a")
	b := ptpSubscribe(t, tr, "b0", "group-b")

	ptpPublish(t, tr, 9)

	if got := recvByte(t, a); got != 9 {
		t.Fatalf("group-a member: got %d, want 9", got)
	}
	if got := recvByte(t, b); got != 9 {
		t.Fatalf("group-b member: got %d, want 9", got)
	}
}

// TestPtpPublishWithNoMembersReportsInterestNone mirrors the PUBSUB contract:
// no live consumer => INTEREST_NONE.
func TestPtpPublishWithNoMembersReportsInterestNone(t *testing.T) {
	tr := ptpTransport(t)
	// No subscribe has happened, so the channel has no group at all. The
	// publish reports INTEREST_NONE, the same as a PUBSUB publish with no
	// subscriber.
	resp, err := tr.Publish(&kernelv1.PublishRequest{Channel: ptpChan, Payload: []byte("x")}, "producer-1")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if resp.GetInterest() != kernelv1.Interest_INTEREST_NONE {
		t.Fatalf("Interest = %v, want INTEREST_NONE", resp.GetInterest())
	}
}

// memberLeasedCount reads a member's in-flight leased count. It reaches into
// transport internals on purpose: a lease is held state with no public getter,
// and the tests here are the only thing that needs to see it directly.
func (t *Transport) memberLeasedCount(tb *testing.T, subscriberID string) int {
	tb.Helper()
	t.mu.Lock()
	sub, ok := t.subs[subscriberID]
	t.mu.Unlock()
	if !ok || sub.ptp == nil {
		tb.Fatalf("memberLeasedCount: %q is not a PTP member", subscriberID)
	}
	g := sub.ptp.grp
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(sub.ptp.leased)
}
