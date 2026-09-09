package transport

import (
	"context"
	"sync"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// ptpGroup is the kernel-held competing-consumer state for one (channel,
// group). One message published on the channel reaches exactly one member of
// this group. Everything here lives in the transport, not in any plugin
// process, so a lease survives the process that held it — which is what lets a
// crashed or reloaded member's in-flight work be reassigned rather than lost.
//
// All of its state is guarded by the one mu: the group queue (messages with no
// live member to take them yet), the ordered member set, and the round-robin
// cursor. Each member carries its own assigned-but-not-received queue and its
// in-flight leased set, also under this lock. There is no goroutine per member
// and no lock per member: one lock covers the whole group, the same shape the
// PUBSUB path keeps.
type ptpGroup struct {
	channel string
	name    string

	mu      sync.Mutex
	queue   []*kernelv1.Envelope // waiting for any live member
	members []*groupMember       // round-robin order = attach order
	rr      int                  // round-robin cursor; always taken modulo len(members)
}

// groupMember is one competing consumer in a group. queue holds messages the
// group has assigned to it but that it has not yet received; leased holds
// messages it received but has not yet Acked or Nacked, keyed by message_id.
// Both are guarded by the owning group's lock, never the member's own.
type groupMember struct {
	grp    *ptpGroup
	id     string
	sub    *Subscription
	queue  []*kernelv1.Envelope
	leased map[string]*kernelv1.Envelope
}

// attach registers a new member under sub and gives it any work already
// waiting in the group queue. It must be called with the transport lock held
// (it is reached only from Subscribe) and takes the group lock itself.
func (g *ptpGroup) attach(id string, sub *Subscription) *groupMember {
	m := &groupMember{grp: g, id: id, sub: sub, leased: make(map[string]*kernelv1.Envelope)}
	g.mu.Lock()
	g.members = append(g.members, m)
	g.drainLocked()
	g.mu.Unlock()
	return m
}

// enqueue puts env on the group queue and assigns what it can to live members.
// It reports whether the group has a live member — the caller turns that into
// INTEREST_PRESENT vs INTEREST_NONE.
func (g *ptpGroup) enqueue(env *kernelv1.Envelope) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.queue = append(g.queue, env)
	g.drainLocked()
	return len(g.members) > 0
}

// drainLocked assigns every waiting group-queue message to live members,
// round-robin, preserving order. It is a no-op when no member is attached.
// Caller holds g.mu.
func (g *ptpGroup) drainLocked() {
	for len(g.queue) > 0 && len(g.members) > 0 {
		env := g.queue[0]
		g.queue = g.queue[1:]
		g.handToNextLocked(env, nil)
	}
}

// handToNextLocked gives env to the next round-robin member, skipping avoid
// while another live member exists — so a Nacked message does not return to
// the member that just failed it. With no live member (or only avoid, on a
// degenerate one-member group) it behaves as the group demands: a sole member
// keeps its own requeued work; an empty group parks the message. Caller holds
// g.mu.
func (g *ptpGroup) handToNextLocked(env *kernelv1.Envelope, avoid *groupMember) {
	n := len(g.members)
	for i := 0; i < n; i++ {
		m := g.members[g.rr%n]
		g.rr++
		if m == avoid && n > 1 {
			continue
		}
		m.queue = append(m.queue, env)
		m.sub.signal()
		return
	}
	// No member at all: hold it for whoever attaches next.
	g.queue = append(g.queue, env)
}

// recv returns this member's next assigned message, leasing it, or blocks on
// wake until one is assigned or ctx is done.
func (m *groupMember) recv(ctx context.Context, wake chan struct{}) (*kernelv1.Envelope, error) {
	g := m.grp
	for {
		g.mu.Lock()
		if len(m.queue) > 0 {
			env := m.queue[0]
			m.queue = m.queue[1:]
			m.leased[env.GetMessageId()] = env
			g.mu.Unlock()
			return env, nil
		}
		g.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-wake:
		}
	}
}

// pending reports this member's assigned-but-not-yet-received count. Leased
// messages are in flight, not pending.
func (m *groupMember) pending() int {
	g := m.grp
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(m.queue)
}

// ack forgets a leased message: the member finished it. A message not leased
// to this member is a typed refusal.
func (m *groupMember) ack(messageID string) error {
	g := m.grp
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := m.leased[messageID]; !ok {
		return ErrNotLeased
	}
	delete(m.leased, messageID)
	return nil
}

// nack returns a leased message to the group and reassigns it to another live
// member (never back to this one while another exists). A message not leased
// to this member is a typed refusal.
func (m *groupMember) nack(messageID string) error {
	g := m.grp
	g.mu.Lock()
	defer g.mu.Unlock()
	env, ok := m.leased[messageID]
	if !ok {
		return ErrNotLeased
	}
	delete(m.leased, messageID)
	g.handToNextLocked(env, m)
	return nil
}

// detach removes m from the group and reassigns everything it held — its
// assigned-but-not-received queue (in order) and then its in-flight leased set
// — to the survivors, or parks it if none remain. Removing m from the member
// set first is what guarantees no reassignment lands back on the member that
// is leaving.
func (g *ptpGroup) detach(m *groupMember) {
	g.mu.Lock()
	defer g.mu.Unlock()

	idx := -1
	for i, mm := range g.members {
		if mm == m {
			idx = i
			break
		}
	}
	if idx < 0 {
		return // already detached
	}
	g.members = append(g.members[:idx], g.members[idx+1:]...)

	held := make([]*kernelv1.Envelope, 0, len(m.queue)+len(m.leased))
	held = append(held, m.queue...)
	for _, env := range m.leased {
		held = append(held, env)
	}
	m.queue = nil
	m.leased = make(map[string]*kernelv1.Envelope)

	for _, env := range held {
		g.handToNextLocked(env, nil)
	}
}
