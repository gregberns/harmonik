// Package transport is the kernel's in-memory, single-box channel layer.
//
// It carries opaque byte payloads between declared channels and never parses
// them. A channel is a (name, type) pair; the first declaration for a name
// wins, and a later declaration with a different type for the same name is
// rejected before any byte moves. Two of the four declared types carry
// traffic here: CHANNEL_TYPE_PUBSUB (copy to every matching subscriber) and
// CHANNEL_TYPE_POINT_TO_POINT (competing consumers — one member of each named
// group takes each message, see ptp.go). The other two are accepted at
// declaration but every operation against them returns
// ErrChannelTypeNotImplemented, a typed refusal, never a silent no-op.
//
// A subscription and its pending queue are transport state keyed by the
// subscriber's own identity, not by any calling process. Once created, a
// subscription and whatever it has buffered outlive a detach: a later call
// with the same subscriber identity reattaches to the same queue and drains
// what built up while nobody was reading it. This is the guarantee a plugin
// reload depends on. POINT_TO_POINT group state — the group queue, each
// member's lease queue, and each member's in-flight leased set — is held the
// same way: in the transport, not in any plugin process, so a lease survives
// the process that held it.
package transport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"google.golang.org/protobuf/types/known/timestamppb"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// MaxPayloadBytes bounds a single envelope's opaque payload. A caller reads
// this through Info rather than hardcoding a guess.
const MaxPayloadBytes = 256 * 1024

// ErrChannelTypeConflict is returned when a channel name is declared a
// second time with a type that differs from its first declaration.
var ErrChannelTypeConflict = errors.New("transport: channel already declared with a different type")

// ErrChannelTypeNotImplemented is returned for every operation against a
// channel type this slice does not carry traffic for.
var ErrChannelTypeNotImplemented = errors.New("transport: channel type not implemented in this slice")

// ErrPayloadTooLarge is returned when a published payload exceeds MaxPayloadBytes.
var ErrPayloadTooLarge = errors.New("transport: payload exceeds max_payload_bytes")

// ErrChannelNotDeclared is returned when a caller publishes or subscribes to
// a name no ChannelDecl has registered.
var ErrChannelNotDeclared = errors.New("transport: channel not declared")

// ErrGroupRequired is returned when a POINT_TO_POINT subscribe names no group.
// A competing-consumer member has to say which group it competes in.
var ErrGroupRequired = errors.New("transport: point_to_point subscribe requires a group")

// ErrGroupOnNonPointToPoint is returned when a subscribe to a non-PTP channel
// carries a group. A group only has meaning for competing consumers.
var ErrGroupOnNonPointToPoint = errors.New("transport: a group is only valid on a point_to_point channel")

// ErrTooManyGroups is returned when a subscribe names more than one group. A
// subscription competes in at most one group.
var ErrTooManyGroups = errors.New("transport: subscribe names at most one group")

// ErrGroupKeyNotImplemented is returned when a Publish sets group_key.
// Per-key consumer affinity is deferred to a later slice; the field is a typed
// refusal here, never a silent no-op.
var ErrGroupKeyNotImplemented = errors.New("transport: group_key not implemented in this slice")

// ErrNotLeased is returned by Ack or Nack for a message the named member does
// not currently hold a lease on (already acked, already nacked, or never
// delivered to it).
var ErrNotLeased = errors.New("transport: message is not leased to this member")

// ErrUnknownSubscriber is returned by Ack, Nack, or Detach for a subscriber id
// that never subscribed.
var ErrUnknownSubscriber = errors.New("transport: unknown subscriber")

// ErrNotPointToPoint is returned by Ack, Nack, or Detach against a subscriber
// whose channel is not POINT_TO_POINT — there is no lease to act on.
var ErrNotPointToPoint = errors.New("transport: subscriber is not a point_to_point member")

// Transport is the in-memory PUBSUB layer plus its channel registry. The
// zero value is not usable; construct one with New.
type Transport struct {
	node string

	mu       sync.Mutex
	channels map[string]kernelv1.ChannelType
	subs     map[string]*Subscription // keyed by subscriber identity
	seq      map[string]uint64        // origin_seq, per channel (single node in this slice)

	// groups holds POINT_TO_POINT competing-consumer state: channel name ->
	// group name -> the group's queue, members, and leases. A group is created
	// on the first subscribe that names it. See ptp.go.
	groups map[string]map[string]*ptpGroup
}

// New builds a Transport that stamps every envelope it originates with node
// as origin_node.
func New(node string) *Transport {
	return &Transport{
		node:     node,
		channels: make(map[string]kernelv1.ChannelType),
		subs:     make(map[string]*Subscription),
		seq:      make(map[string]uint64),
		groups:   make(map[string]map[string]*ptpGroup),
	}
}

// Info reports the boundary limits a caller must read, never hardcode.
func (t *Transport) Info() *kernelv1.InfoResponse {
	return &kernelv1.InfoResponse{
		Node:            t.node,
		MaxPayloadBytes: MaxPayloadBytes,
	}
}

// Declare registers name as typ. A first declaration for name always
// succeeds. A later call for the same name with the same typ is a no-op; a
// later call with a different typ is rejected with ErrChannelTypeConflict.
func (t *Transport) Declare(name string, typ kernelv1.ChannelType) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	existing, declared := t.channels[name]
	if !declared {
		t.channels[name] = typ
		return nil
	}
	if existing != typ {
		return fmt.Errorf("%w: %q is %s, not %s", ErrChannelTypeConflict, name, existing, typ)
	}
	return nil
}

// Publish moves req onto req.Channel. On a PUBSUB channel every matching
// subscription gets a copy; on a POINT_TO_POINT channel one member of each
// named group gets it, chosen round-robin (see ptp.go). producer names the
// calling plugin's namespace and is stamped onto the resulting envelope; every
// other provenance field (origin_node, origin_time, message_id, origin_seq) is
// stamped by the transport itself. The transport counts payload bytes and
// never reads them. A non-empty group_key is refused with
// ErrGroupKeyNotImplemented — per-key affinity is a later slice.
//
// Publish is Stamp followed by Inject against this same Transport — the
// single-box path. Those two steps are separate and exported so the in-process
// mesh double (kernel/transport/memmesh) can stamp an envelope once at its
// origin node and Inject that one envelope — origin_node and origin_seq intact
// — into the subscribers on every other node. A caller that is not the mesh
// double wants Publish, not the two halves.
func (t *Transport) Publish(req *kernelv1.PublishRequest, producer string) (*kernelv1.PublishResponse, error) {
	env, err := t.Stamp(req, producer)
	if err != nil {
		return nil, err
	}
	interest, err := t.Inject(env)
	if err != nil {
		return nil, err
	}
	return &kernelv1.PublishResponse{MessageId: env.GetMessageId(), Interest: interest, OriginSeq: env.GetOriginSeq()}, nil
}

// Stamp builds the provenance-stamped envelope this node originates for req and
// advances this node's origin_seq for the channel. It delivers nothing. The
// channel must be declared and of a type this slice carries traffic for
// (PUBSUB or POINT_TO_POINT); a group_key or an oversized payload is refused
// before any counter moves, exactly as Publish refused them. See Publish for
// why Stamp is separate from Inject and why a non-mesh caller wants Publish.
func (t *Transport) Stamp(req *kernelv1.PublishRequest, producer string) (*kernelv1.Envelope, error) {
	if req.GetGroupKey() != "" {
		return nil, ErrGroupKeyNotImplemented
	}
	if len(req.GetPayload()) > MaxPayloadBytes {
		return nil, ErrPayloadTooLarge
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	typ, declared := t.channels[req.GetChannel()]
	if !declared {
		return nil, fmt.Errorf("%w: %q", ErrChannelNotDeclared, req.GetChannel())
	}
	switch typ {
	case kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB,
		kernelv1.ChannelType_CHANNEL_TYPE_POINT_TO_POINT:
		t.seq[req.GetChannel()]++
		return t.stampEnvelope(req, producer, t.seq[req.GetChannel()]), nil

	case kernelv1.ChannelType_CHANNEL_TYPE_UNSPECIFIED,
		kernelv1.ChannelType_CHANNEL_TYPE_REQUEST_REPLY,
		kernelv1.ChannelType_CHANNEL_TYPE_LOOKUP:
		return nil, ErrChannelTypeNotImplemented

	default:
		return nil, ErrChannelTypeNotImplemented
	}
}

// Inject delivers an already-stamped envelope to this node's local subscribers,
// by the channel's declared type, WITHOUT stamping it again — env's origin_node
// and origin_seq are preserved exactly. On a PUBSUB channel every matching
// local subscription gets a copy; on a POINT_TO_POINT channel the envelope goes
// onto the local group queues, where one live member of each group takes it. It
// reports INTEREST_PRESENT when at least one local subscriber or live group
// member exists to take the envelope, INTEREST_NONE otherwise. Because Inject
// never re-stamps, a mesh peer cannot overwrite the origin a message was
// stamped with. See Publish for the single-box pairing with Stamp.
func (t *Transport) Inject(env *kernelv1.Envelope) (kernelv1.Interest, error) {
	t.mu.Lock()
	typ, declared := t.channels[env.GetChannel()]
	if !declared {
		t.mu.Unlock()
		return kernelv1.Interest_INTEREST_NONE, fmt.Errorf("%w: %q", ErrChannelNotDeclared, env.GetChannel())
	}

	switch typ {
	case kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB:
		var targets []*Subscription
		for _, sub := range t.subs {
			if sub.pattern == env.GetChannel() {
				targets = append(targets, sub)
			}
		}
		t.mu.Unlock()

		interest := kernelv1.Interest_INTEREST_NONE
		for _, sub := range targets {
			sub.enqueue(env)
			interest = kernelv1.Interest_INTEREST_PRESENT
		}
		return interest, nil

	case kernelv1.ChannelType_CHANNEL_TYPE_POINT_TO_POINT:
		groups := make([]*ptpGroup, 0, len(t.groups[env.GetChannel()]))
		for _, g := range t.groups[env.GetChannel()] {
			groups = append(groups, g)
		}
		t.mu.Unlock()

		// One message goes to one member of EACH named group. A group with a
		// live member is interest; all parked with no member is INTEREST_NONE.
		interest := kernelv1.Interest_INTEREST_NONE
		for _, g := range groups {
			if g.enqueue(env) {
				interest = kernelv1.Interest_INTEREST_PRESENT
			}
		}
		return interest, nil

	case kernelv1.ChannelType_CHANNEL_TYPE_UNSPECIFIED,
		kernelv1.ChannelType_CHANNEL_TYPE_REQUEST_REPLY,
		kernelv1.ChannelType_CHANNEL_TYPE_LOOKUP:
		t.mu.Unlock()
		return kernelv1.Interest_INTEREST_NONE, ErrChannelTypeNotImplemented

	default:
		t.mu.Unlock()
		return kernelv1.Interest_INTEREST_NONE, ErrChannelTypeNotImplemented
	}
}

// stampEnvelope builds the envelope the transport originates. producer names
// the calling plugin's namespace; every provenance field (origin_node,
// origin_time, message_id, origin_seq) is the transport's, overwriting any the
// caller set. The payload is copied by reference and never read.
func (t *Transport) stampEnvelope(req *kernelv1.PublishRequest, producer string, seq uint64) *kernelv1.Envelope {
	return &kernelv1.Envelope{
		Channel:    req.GetChannel(),
		Payload:    req.GetPayload(),
		Headers:    req.GetHeaders(),
		OriginNode: t.node,
		OriginTime: timestamppb.Now(),
		MessageId:  newMessageID(),
		OriginSeq:  seq,
		Producer:   producer,
	}
}

// Subscribe attaches subscriberID to pattern. subscriberID names the
// subscribing plugin's own declared interest and is the key the transport
// holds the subscription under, so a second call with the same subscriberID
// reattaches to whatever the first call already buffered instead of starting
// empty.
//
// group is the POINT_TO_POINT competing-consumer group and is optional: a
// PUBSUB subscribe names none, a POINT_TO_POINT subscribe names exactly one.
// A PTP subscribe with no group (ErrGroupRequired), a non-PTP subscribe with a
// group (ErrGroupOnNonPointToPoint), and a subscribe naming more than one
// group (ErrTooManyGroups) are each a typed refusal. The variadic form keeps
// the PUBSUB two-argument call unchanged.
func (t *Transport) Subscribe(subscriberID, pattern string, group ...string) (*Subscription, error) {
	if subscriberID == "" {
		return nil, errors.New("transport: subscribe requires a non-empty subscriber id")
	}
	groupName, err := soleGroup(group)
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if existing, ok := t.subs[subscriberID]; ok {
		if existing.pattern != pattern {
			return nil, fmt.Errorf("transport: subscriber %q already holds pattern %q, not %q", subscriberID, existing.pattern, pattern)
		}
		if existing.groupName() != groupName {
			return nil, fmt.Errorf("transport: subscriber %q already holds group %q, not %q", subscriberID, existing.groupName(), groupName)
		}
		return existing, nil
	}

	typ, declared := t.channels[pattern]
	if !declared {
		return nil, fmt.Errorf("%w: %q", ErrChannelNotDeclared, pattern)
	}

	switch typ {
	case kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB:
		if groupName != "" {
			return nil, ErrGroupOnNonPointToPoint
		}
		sub := newSubscription(pattern)
		t.subs[subscriberID] = sub
		return sub, nil

	case kernelv1.ChannelType_CHANNEL_TYPE_POINT_TO_POINT:
		if groupName == "" {
			return nil, ErrGroupRequired
		}
		sub := newSubscription(pattern)
		g := t.groupLocked(pattern, groupName)
		sub.ptp = g.attach(subscriberID, sub)
		t.subs[subscriberID] = sub
		return sub, nil

	case kernelv1.ChannelType_CHANNEL_TYPE_UNSPECIFIED,
		kernelv1.ChannelType_CHANNEL_TYPE_REQUEST_REPLY,
		kernelv1.ChannelType_CHANNEL_TYPE_LOOKUP:
		return nil, ErrChannelTypeNotImplemented

	default:
		return nil, ErrChannelTypeNotImplemented
	}
}

// groupLocked finds, or creates, the group state for (channel, name). It must
// be called with t.mu held; the group itself carries its own lock for the
// queue/lease bookkeeping.
func (t *Transport) groupLocked(channel, name string) *ptpGroup {
	byName := t.groups[channel]
	if byName == nil {
		byName = make(map[string]*ptpGroup)
		t.groups[channel] = byName
	}
	g := byName[name]
	if g == nil {
		g = &ptpGroup{channel: channel, name: name}
		byName[name] = g
	}
	return g
}

// soleGroup collapses the variadic group argument to the single group a
// subscription may name: none ("") or one. More than one is a typed refusal.
func soleGroup(group []string) (string, error) {
	switch len(group) {
	case 0:
		return "", nil
	case 1:
		return group[0], nil
	default:
		return "", ErrTooManyGroups
	}
}

// Ack tells the transport that subscriberID finished the leased message and it
// can be forgotten. It is a typed refusal if the subscriber is unknown, is not
// a POINT_TO_POINT member, or holds no lease on messageID.
func (t *Transport) Ack(subscriberID, messageID string) error {
	m, err := t.memberOf(subscriberID)
	if err != nil {
		return err
	}
	return m.ack(messageID)
}

// Nack tells the transport that subscriberID failed the leased message. The
// message returns to the group queue and is reassigned to another live member
// (never back to the member that failed it while another exists). Same typed
// refusals as Ack.
func (t *Transport) Nack(subscriberID, messageID string) error {
	m, err := t.memberOf(subscriberID)
	if err != nil {
		return err
	}
	return m.nack(messageID)
}

// Detach removes subscriberID from its group and nacks everything it held —
// both assigned-but-not-yet-received and in-flight leased — back to the group
// queue for reassignment to the survivors. This is the verb a reload or a
// crash triggers: a dead member strands no message. A later Subscribe with the
// same id attaches a fresh member. Typed refusal if the subscriber is unknown
// or is not a POINT_TO_POINT member.
func (t *Transport) Detach(subscriberID string) error {
	t.mu.Lock()
	sub, ok := t.subs[subscriberID]
	if !ok {
		t.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrUnknownSubscriber, subscriberID)
	}
	if sub.ptp == nil {
		t.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrNotPointToPoint, subscriberID)
	}
	m := sub.ptp
	delete(t.subs, subscriberID)
	t.mu.Unlock()

	m.grp.detach(m)
	return nil
}

// memberOf resolves a subscriber id to its POINT_TO_POINT member, or a typed
// refusal.
func (t *Transport) memberOf(subscriberID string) (*groupMember, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	sub, ok := t.subs[subscriberID]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownSubscriber, subscriberID)
	}
	if sub.ptp == nil {
		return nil, fmt.Errorf("%w: %q", ErrNotPointToPoint, subscriberID)
	}
	return sub.ptp, nil
}

func newMessageID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read does not fail on a supported OS; this fallback
		// only keeps Publish from panicking if it somehow does.
		return "fallback-" + timestamppb.Now().AsTime().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(b[:])
}

// Subscription is a kernel-held, manifest-scoped queue of envelopes waiting
// for one subscriber. It is created once per subscriber identity and lives
// in the owning Transport for as long as that Transport does, independent of
// whether anything is currently calling Recv.
type Subscription struct {
	pattern string

	mu    sync.Mutex
	queue []*kernelv1.Envelope
	wake  chan struct{}

	// ptp is non-nil only for a POINT_TO_POINT group member. When set, Recv
	// and Pending read the member's lease queue held in the group (under the
	// group's lock) and the queue field above stays unused. wake is shared by
	// both paths: the group signals it when it assigns the member an envelope.
	ptp *groupMember
}

func newSubscription(pattern string) *Subscription {
	return &Subscription{pattern: pattern, wake: make(chan struct{}, 1)}
}

// signal wakes a blocked Recv. It is non-blocking: the buffered wake channel
// collapses any number of pending signals into one, and Recv re-checks the
// queue on every wake, so no notification is lost.
func (s *Subscription) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// groupName reports the POINT_TO_POINT group this subscription competes in, or
// "" for a PUBSUB subscription.
func (s *Subscription) groupName() string {
	if s.ptp == nil {
		return ""
	}
	return s.ptp.grp.name
}

func (s *Subscription) enqueue(env *kernelv1.Envelope) {
	s.mu.Lock()
	s.queue = append(s.queue, env)
	s.mu.Unlock()
	s.signal()
}

// Pending reports how many envelopes are waiting for this subscriber to
// receive right now. For a POINT_TO_POINT member this is its assigned but
// not-yet-received count; messages already received and still leased (awaiting
// Ack/Nack) are in flight, not pending.
func (s *Subscription) Pending() int {
	if s.ptp != nil {
		return s.ptp.pending()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

// Recv returns the next buffered envelope, blocking until one arrives or ctx
// is done. Envelopes buffered before Recv is ever called, or while nothing
// was calling it, are returned in the order they were published. For a
// POINT_TO_POINT member, receiving an envelope also leases it: it is held
// against the member until an Ack forgets it or a Nack requeues it.
func (s *Subscription) Recv(ctx context.Context) (*kernelv1.Envelope, error) {
	if s.ptp != nil {
		return s.ptp.recv(ctx, s.wake)
	}
	for {
		s.mu.Lock()
		if len(s.queue) > 0 {
			env := s.queue[0]
			s.queue = s.queue[1:]
			s.mu.Unlock()
			return env, nil
		}
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.wake:
		}
	}
}
