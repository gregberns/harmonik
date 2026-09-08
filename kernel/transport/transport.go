// Package transport is the kernel's in-memory, single-box channel layer.
//
// It carries opaque byte payloads between declared channels and never parses
// them. A channel is a (name, type) pair; the first declaration for a name
// wins, and a later declaration with a different type for the same name is
// rejected before any byte moves. Only CHANNEL_TYPE_PUBSUB carries traffic
// in this slice — the other three declared types are accepted but every
// operation against them returns ErrChannelTypeNotImplemented, a typed
// refusal, never a silent no-op.
//
// A subscription and its pending queue are transport state keyed by the
// subscriber's own identity, not by any calling process. Once created, a
// subscription and whatever it has buffered outlive a detach: a later call
// with the same subscriber identity reattaches to the same queue and drains
// what built up while nobody was reading it. This is the guarantee a plugin
// reload depends on.
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

// Transport is the in-memory PUBSUB layer plus its channel registry. The
// zero value is not usable; construct one with New.
type Transport struct {
	node string

	mu       sync.Mutex
	channels map[string]kernelv1.ChannelType
	subs     map[string]*Subscription // keyed by subscriber identity
	seq      map[string]uint64        // origin_seq, per channel (single node in this slice)
}

// New builds a Transport that stamps every envelope it originates with node
// as origin_node.
func New(node string) *Transport {
	return &Transport{
		node:     node,
		channels: make(map[string]kernelv1.ChannelType),
		subs:     make(map[string]*Subscription),
		seq:      make(map[string]uint64),
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

// Publish delivers req to every subscription declared against req.Channel.
// producer names the calling plugin's namespace and is stamped onto the
// resulting envelope; every other provenance field (origin_node, origin_time,
// message_id, origin_seq) is stamped by the transport itself. The transport
// counts payload bytes and never reads them.
func (t *Transport) Publish(req *kernelv1.PublishRequest, producer string) (*kernelv1.PublishResponse, error) {
	if len(req.GetPayload()) > MaxPayloadBytes {
		return nil, ErrPayloadTooLarge
	}

	t.mu.Lock()
	typ, declared := t.channels[req.GetChannel()]
	if !declared {
		t.mu.Unlock()
		return nil, fmt.Errorf("%w: %q", ErrChannelNotDeclared, req.GetChannel())
	}
	if typ != kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB {
		t.mu.Unlock()
		return nil, ErrChannelTypeNotImplemented
	}

	t.seq[req.GetChannel()]++
	seq := t.seq[req.GetChannel()]

	var targets []*Subscription
	for _, sub := range t.subs {
		if sub.pattern == req.GetChannel() {
			targets = append(targets, sub)
		}
	}
	t.mu.Unlock()

	env := &kernelv1.Envelope{
		Channel:    req.GetChannel(),
		Payload:    req.GetPayload(),
		Headers:    req.GetHeaders(),
		OriginNode: t.node,
		OriginTime: timestamppb.Now(),
		MessageId:  newMessageID(),
		OriginSeq:  seq,
		Producer:   producer,
	}

	interest := kernelv1.Interest_INTEREST_NONE
	for _, sub := range targets {
		sub.enqueue(env)
		interest = kernelv1.Interest_INTEREST_PRESENT
	}

	return &kernelv1.PublishResponse{
		MessageId: env.GetMessageId(),
		Interest:  interest,
		OriginSeq: seq,
	}, nil
}

// Subscribe attaches subscriberID to pattern. subscriberID names the
// subscribing plugin's own declared interest and is the key the transport
// holds the subscription's queue under, so a second call with the same
// subscriberID reattaches to whatever the first call's subscription already
// buffered instead of starting empty.
func (t *Transport) Subscribe(subscriberID, pattern string) (*Subscription, error) {
	if subscriberID == "" {
		return nil, errors.New("transport: subscribe requires a non-empty subscriber id")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if existing, ok := t.subs[subscriberID]; ok {
		if existing.pattern != pattern {
			return nil, fmt.Errorf("transport: subscriber %q already holds pattern %q, not %q", subscriberID, existing.pattern, pattern)
		}
		return existing, nil
	}

	typ, declared := t.channels[pattern]
	if !declared {
		return nil, fmt.Errorf("%w: %q", ErrChannelNotDeclared, pattern)
	}
	if typ != kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB {
		return nil, ErrChannelTypeNotImplemented
	}

	sub := newSubscription(pattern)
	t.subs[subscriberID] = sub
	return sub, nil
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
}

func newSubscription(pattern string) *Subscription {
	return &Subscription{pattern: pattern, wake: make(chan struct{}, 1)}
}

func (s *Subscription) enqueue(env *kernelv1.Envelope) {
	s.mu.Lock()
	s.queue = append(s.queue, env)
	s.mu.Unlock()

	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Pending reports how many envelopes are buffered right now.
func (s *Subscription) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

// Recv returns the next buffered envelope, blocking until one arrives or ctx
// is done. Envelopes buffered before Recv is ever called, or while nothing
// was calling it, are returned in the order they were published.
func (s *Subscription) Recv(ctx context.Context) (*kernelv1.Envelope, error) {
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
