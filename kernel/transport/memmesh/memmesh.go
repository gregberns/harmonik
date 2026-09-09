// Package memmesh links N in-process transport.Transport instances into one
// logical mesh, so a multi-kernel topology can be stood up and fault-tested
// inside a single OS process.
//
// THIS IS NOT THE Q-1 CROSS-MACHINE TRANSPORT — it is in-process
// test/composition plumbing only. Nodes talk to each other by direct function
// calls into each other's transport under that transport's own locks; there is
// no socket between kernels, no goroutine per hop, and no pluggable network
// seam. Whether a real cross-machine transport exists, and what shape it takes,
// is the open Q-1 decision of a later slice; this double must not be mistaken
// for, or grown into, that verdict. The cross-node seam it uses is deliberately
// tiny: it stamps an envelope once at its origin node (transport.Stamp) and
// hands that one envelope to peers (transport.Inject), it forwards channel
// declarations, and it routes a competing-consumer membership to the kernel
// that holds the group queue. Nothing more belongs here.
//
// Three cross-kernel behaviours live in this package:
//
//   - Declaration forwarding. The first (name, type) declared anywhere in the
//     mesh wins mesh-wide; a conflicting type from another node is refused. The
//     winning declaration is forwarded to every node's transport so each can
//     subscribe and deliver against it.
//   - PUBSUB fan-out. A publish on one node is stamped once there (origin_node =
//     that node, origin_seq monotonic per (that node, channel)) and the one
//     stamped envelope is delivered to every node's matching subscribers.
//   - POINT_TO_POINT membership against the declaring kernel. The kernel that
//     first declared a channel holds its group queue; a member subscribing on
//     any node is attached to that declaring kernel's transport, so workers on
//     different nodes compete in the one queue that lives on the authority.
package memmesh

import (
	"context"
	"errors"
	"fmt"
	"sync"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
	"github.com/gregberns/harmonik/kernel/transport"
)

// idSep joins a node name to a subscriber id to form the mesh-unique key a
// subscription is attached under. Two nodes may each use the same local id
// (say, both a node's "worker"), and a POINT_TO_POINT member from each is
// attached to the one declaring kernel's transport, so the key they attach
// under must differ. A NUL byte never appears in a caller's id.
const idSep = "\x00"

// ErrChannelTypeConflict is returned when a node declares a channel name that
// another node already declared mesh-wide with a different type. The first
// (name, type) declared anywhere wins.
var ErrChannelTypeConflict = errors.New("memmesh: channel already declared mesh-wide with a different type")

// ErrChannelNotDeclared is returned when a publish or subscribe names a channel
// no node has declared in the mesh.
var ErrChannelNotDeclared = errors.New("memmesh: channel not declared in the mesh")

// ErrUnknownMember is returned by Ack, Nack, or Detach for a (node, subscriber)
// pair that never joined a POINT_TO_POINT group through this mesh.
var ErrUnknownMember = errors.New("memmesh: no such point_to_point member on this node")

// Mesh holds the set of linked nodes and the mesh-wide channel registry. The
// zero value is not usable; construct one with New.
type Mesh struct {
	mu    sync.Mutex
	nodes []*Node
	// decls is the mesh-wide declaration registry: channel name -> its type and
	// its authority (the node that first declared it). The authority is the
	// kernel that holds the channel's POINT_TO_POINT group queue.
	decls map[string]decl
	// ptpMembers records where each routed POINT_TO_POINT membership was
	// attached — the declaring node and the effective id — so Ack, Nack, and
	// Detach reach the same lease the Subscribe created. Keyed by the same
	// mesh-unique key the member attached under.
	ptpMembers map[string]*memberRef
}

// decl is one channel's mesh-wide declaration.
type decl struct {
	typ      kernelv1.ChannelType
	declarer *Node
}

// memberRef remembers the declaring node and the effective id a routed
// POINT_TO_POINT membership was attached under.
type memberRef struct {
	declarer    *Node
	effectiveID string
}

// Node is one kernel's handle in the mesh: its own transport plus a back
// reference to the mesh that routes its cross-node traffic. A test or a
// composition root acts "as that kernel" through it.
type Node struct {
	mesh *Mesh
	name string
	t    *transport.Transport
}

// New builds an empty mesh. Add nodes with AddNode.
func New() *Mesh {
	return &Mesh{
		decls:      make(map[string]decl),
		ptpMembers: make(map[string]*memberRef),
	}
}

// AddNode creates a kernel named name, with its own transport, and links it
// into the mesh. A node that joins after channels are declared is seeded with
// every existing declaration, so a later subscribe or delivery on it finds the
// channel. The name must be unique in the mesh and non-empty.
func (m *Mesh) AddNode(name string) (*Node, error) {
	if name == "" {
		return nil, errors.New("memmesh: a node needs a non-empty name")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.nodes {
		if existing.name == name {
			return nil, fmt.Errorf("memmesh: node %q is already in the mesh", name)
		}
	}

	n := &Node{mesh: m, name: name, t: transport.New(name)}
	for chName, d := range m.decls {
		if err := n.t.Declare(chName, d.typ); err != nil {
			return nil, fmt.Errorf("memmesh: seeding node %q with channel %q: %w", name, chName, err)
		}
	}
	m.nodes = append(m.nodes, n)
	return n, nil
}

// Name reports the node's kernel name, the value it stamps as origin_node.
func (n *Node) Name() string { return n.name }

// Declare registers a channel mesh-wide from this node. The first (name, type)
// declared anywhere in the mesh wins and makes this node the channel's
// authority; a later declaration of the same name with a different type from
// any node is refused with ErrChannelTypeConflict. A repeat of the same
// (name, type) is idempotent. On success the declaration is forwarded to every
// node's transport.
func (n *Node) Declare(channel string, typ kernelv1.ChannelType) error {
	m := n.mesh
	m.mu.Lock()
	defer m.mu.Unlock()

	if d, ok := m.decls[channel]; ok {
		if d.typ != typ {
			return fmt.Errorf("%w: %q is %s, not %s", ErrChannelTypeConflict, channel, d.typ, typ)
		}
		return nil // already declared mesh-wide and forwarded to every node
	}

	// Forward to every node (this node included) before recording the winner,
	// so a forward failure leaves no half-registered channel behind.
	for _, peer := range m.nodes {
		if err := peer.t.Declare(channel, typ); err != nil {
			return fmt.Errorf("memmesh: forwarding %q to node %q: %w", channel, peer.name, err)
		}
	}
	m.decls[channel] = decl{typ: typ, declarer: n}
	return nil
}

// Publish stamps req at this node and fans it across the mesh. On a PUBSUB
// channel the one stamped envelope — origin_node = this node, origin_seq
// monotonic per (this node, channel) — reaches every node's matching
// subscribers, this node's own included. On a POINT_TO_POINT channel the
// stamped envelope goes to the declaring node's group queue, where one live
// member of each group takes it, wherever that member's node is. Interest is
// INTEREST_PRESENT when any node had a taker.
func (n *Node) Publish(req *kernelv1.PublishRequest, producer string) (*kernelv1.PublishResponse, error) {
	d, ok := n.mesh.declaration(req.GetChannel())
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrChannelNotDeclared, req.GetChannel())
	}

	// Stamp once, at the origin node, so origin_node and origin_seq are this
	// node's and stay fixed as the one envelope travels to peers.
	env, err := n.t.Stamp(req, producer)
	if err != nil {
		return nil, err
	}

	var targets []*Node
	if d.typ == kernelv1.ChannelType_CHANNEL_TYPE_POINT_TO_POINT {
		targets = []*Node{d.declarer} // the one kernel that holds the group queue
	} else {
		targets = n.mesh.peers() // fan out to every node
	}

	interest := kernelv1.Interest_INTEREST_NONE
	for _, peer := range targets {
		got, err := peer.t.Inject(env)
		if err != nil {
			return nil, fmt.Errorf("memmesh: delivering %q to node %q: %w", req.GetChannel(), peer.name, err)
		}
		if got == kernelv1.Interest_INTEREST_PRESENT {
			interest = kernelv1.Interest_INTEREST_PRESENT
		}
	}
	return &kernelv1.PublishResponse{MessageId: env.GetMessageId(), Interest: interest, OriginSeq: env.GetOriginSeq()}, nil
}

// Subscribe attaches a subscriber on this node. A PUBSUB subscription lives on
// this node's own transport. A POINT_TO_POINT member is attached instead to the
// DECLARING node's transport — the kernel that holds the group queue — so a
// worker on any node competes in the one queue that lives on the authority. The
// returned Subscription is driven with Recv exactly as a single-box one is; for
// a routed member, Recv reads the declaring node's group under its lock, and
// Ack/Nack/Detach on this node route back to it.
func (n *Node) Subscribe(subscriberID, channel string, group ...string) (*transport.Subscription, error) {
	d, ok := n.mesh.declaration(channel)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrChannelNotDeclared, channel)
	}

	effectiveID := n.name + idSep + subscriberID
	holder := n
	if d.typ == kernelv1.ChannelType_CHANNEL_TYPE_POINT_TO_POINT {
		holder = d.declarer
	}

	sub, err := holder.t.Subscribe(effectiveID, channel, group...)
	if err != nil {
		return nil, err
	}
	if d.typ == kernelv1.ChannelType_CHANNEL_TYPE_POINT_TO_POINT {
		n.mesh.rememberMember(effectiveID, &memberRef{declarer: d.declarer, effectiveID: effectiveID})
	}
	return sub, nil
}

// Ack routes a lease acknowledgement for a POINT_TO_POINT member on this node
// to the declaring kernel that holds the lease. The member must have joined a
// group through Subscribe.
func (n *Node) Ack(subscriberID, messageID string) error {
	ref, err := n.mesh.lookupMember(n, subscriberID)
	if err != nil {
		return err
	}
	return ref.declarer.t.Ack(ref.effectiveID, messageID)
}

// Nack routes a lease failure for a POINT_TO_POINT member on this node to the
// declaring kernel, which reassigns the message to another live member —
// possibly on another node. Same precondition as Ack.
func (n *Node) Nack(subscriberID, messageID string) error {
	ref, err := n.mesh.lookupMember(n, subscriberID)
	if err != nil {
		return err
	}
	return ref.declarer.t.Nack(ref.effectiveID, messageID)
}

// Detach removes a POINT_TO_POINT member on this node from its group on the
// declaring kernel, nacking everything it held back to the group queue for the
// survivors, and forgets the membership. Same precondition as Ack.
func (n *Node) Detach(subscriberID string) error {
	ref, err := n.mesh.lookupMember(n, subscriberID)
	if err != nil {
		return err
	}
	if err := ref.declarer.t.Detach(ref.effectiveID); err != nil {
		return err
	}
	n.mesh.forgetMember(n, subscriberID)
	return nil
}

// Serve registers this node as a server of a REQUEST_REPLY channel and streams
// each incoming question to deliver as (envelope, request_id), until ctx is
// done or deliver errors. A REQUEST_REPLY server stays on its OWN node's
// transport — unlike a POINT_TO_POINT member, which attaches to the declaring
// kernel — because a question is routed to a node that already holds a server
// (see Request) and answered on that same node (see Respond), so all of one
// exchange's correlation state lives on one node with no cross-node request_id
// bookkeeping. The channel must be declared mesh-wide.
func (n *Node) Serve(ctx context.Context, pattern string, deliver func(env *kernelv1.Envelope, requestID string) error) error {
	if _, ok := n.mesh.declaration(pattern); !ok {
		return fmt.Errorf("%w: %q", ErrChannelNotDeclared, pattern)
	}
	return n.t.Serve(ctx, pattern, deliver)
}

// Request asks one question on a REQUEST_REPLY channel and waits for one
// answer, reaching a server wherever it lives in the mesh. It finds a node with
// a live server for the channel — this node included — and routes the blocking
// Request into that node's transport, where the question is minted, answered,
// and correlated end to end. With no server anywhere it returns INTEREST_NONE
// at once, never a silent wait. The channel must be declared mesh-wide.
func (n *Node) Request(ctx context.Context, req *kernelv1.PublishRequest, producer string) (*kernelv1.RequestResponse, error) {
	if _, ok := n.mesh.declaration(req.GetChannel()); !ok {
		return nil, fmt.Errorf("%w: %q", ErrChannelNotDeclared, req.GetChannel())
	}
	for _, peer := range n.mesh.peers() {
		if peer.t.ServesChannel(req.GetChannel()) {
			return peer.t.Request(ctx, req, producer)
		}
	}
	return &kernelv1.RequestResponse{Interest: kernelv1.Interest_INTEREST_NONE}, nil
}

// Respond delivers an answer to an open request by its request_id. A
// REQUEST_REPLY server answers on the same node it received the question on,
// and Request routes a question to the node that holds the server, so the open
// correlation for that request_id lives on this node's transport — Respond is a
// local call with no mesh routing. A non-empty errMsg fails the requester's
// call.
func (n *Node) Respond(requestID string, payload []byte, headers map[string]string, errMsg string) error {
	return n.t.Respond(requestID, payload, headers, errMsg)
}

// --- extension seam for later slices (B4 LOOKUP) ---
//
// B3 (a request on one kernel reaching a server on another, just above) reads
// declaration (to confirm the channel and refuse an undeclared one) and peers
// (to find a node that serves it); B4 (a lookup read merged across kernels)
// will read the same two. Both land in this package, so they read declaration
// and peers directly — no new public surface, and no general "network
// transport" interface. That minimalism is the guard the task names: if a later
// slice needs the mesh to grow a pluggable transport seam, that is the Q-1
// decision (see the package doc), not this double.

// declaration returns a snapshot of channel's mesh-wide declaration: its type
// and its authority node. It is the per-channel read Publish and Subscribe use,
// and the read B3 and B4 plug into.
func (m *Mesh) declaration(channel string) (decl, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.decls[channel]
	return d, ok
}

// peers returns a snapshot of every node in the mesh. PUBSUB fan-out delivers
// to each; B4's lookup merge will read each peer's local map, and B3's request
// routing will find a server among them.
func (m *Mesh) peers() []*Node {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Node, len(m.nodes))
	copy(out, m.nodes)
	return out
}

func (m *Mesh) rememberMember(key string, ref *memberRef) {
	m.mu.Lock()
	m.ptpMembers[key] = ref
	m.mu.Unlock()
}

func (m *Mesh) lookupMember(n *Node, subscriberID string) (*memberRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ref, ok := m.ptpMembers[n.name+idSep+subscriberID]
	if !ok {
		return nil, fmt.Errorf("%w: node %q id %q", ErrUnknownMember, n.name, subscriberID)
	}
	return ref, nil
}

func (m *Mesh) forgetMember(n *Node, subscriberID string) {
	m.mu.Lock()
	delete(m.ptpMembers, n.name+idSep+subscriberID)
	m.mu.Unlock()
}
