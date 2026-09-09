package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
	"github.com/gregberns/harmonik/kernel/host"
	"github.com/gregberns/harmonik/kernel/transport"
	"github.com/gregberns/harmonik/kernel/transport/memmesh"
)

// idleBackoff is how long the dispatch loop waits before checking again
// whether a delivery target can take an envelope, while no process is ready
// for it — either none is in StateRunning, or the drain gate is holding
// dispatch back during a reload. It does not pop a subscription's queue
// during that wait, so a publish that lands while dispatch is paused stays
// kernel-held (transport.Subscription outlives the process) instead of being
// lost.
const idleBackoff = 20 * time.Millisecond

// defaultDrainDeadline bounds how long a reload waits for in-flight unary
// deliveries to finish before it cancels them and kills the process anyway. A
// unary Deliver is meant to be quick, so this is generous enough to let a
// genuinely slow handler finish yet short enough that a hung one cannot stall
// a reload for long. A test sets its own through the pluginManager field.
const defaultDrainDeadline = 2 * time.Second

// dispatcher pumps one kernel-held subscription into whatever process its
// pluginManager currently holds.
//
// For a PUBSUB interest, held is an envelope already popped from the
// subscription but not yet delivered — set whenever a process was not ready for
// it, or Deliver against it failed — so Pending, and the loop itself, both treat
// "sitting in held" the same as "still queued in the subscription": either way
// it has not reached the plugin yet.
//
// For a POINT_TO_POINT interest, held is never used: Recv leases the envelope in
// the transport, and that lease IS the hold. The lease is resolved by exactly
// one Ack (delivered) or Nack (returned to the group queue for reassignment to a
// live member); nothing rides on this dispatcher's memory, so a reload or a
// worker death strands no message. ptp selects which loop runs; subscriberID,
// pattern, and group are what a reload needs to detach and re-attach the member.
type dispatcher struct {
	subscriberID string
	pattern      string
	group        string // "" for PUBSUB; the competing-consumer group for POINT_TO_POINT
	ptp          bool

	mu         sync.Mutex
	sub        *transport.Subscription
	held       *kernelv1.Envelope // PUBSUB-only
	pumpCancel context.CancelFunc // cancels this dispatcher's pump goroutine; a reload restarts a PTP pump on a fresh subscription
	pumpDone   chan struct{}      // closed when the pump goroutine has exited
}

// pending reports how many envelopes this dispatcher has not yet delivered:
// whatever the subscription itself still buffers, plus one more if a PUBSUB
// envelope is currently held pending a ready process. A leased POINT_TO_POINT
// envelope is in flight, not pending, so the transport counts it, never held.
func (d *dispatcher) pending() int {
	d.mu.Lock()
	held := d.held != nil
	sub := d.sub
	d.mu.Unlock()
	n := sub.Pending()
	if held {
		n++
	}
	return n
}

// currentSub returns the subscription this dispatcher is pumping right now. A
// reload swaps it for a POINT_TO_POINT member — detach + re-attach yields a
// fresh Subscription — so the pump reads it through this rather than caching it.
func (d *dispatcher) currentSub() *transport.Subscription {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sub
}

func (d *dispatcher) setSub(sub *transport.Subscription) {
	d.mu.Lock()
	d.sub = sub
	d.mu.Unlock()
}

// pump routes to the delivery loop the interest's channel type needs. The
// PUBSUB loop is the one K8 shipped; the POINT_TO_POINT loop leases, delivers,
// then acks or nacks.
func (d *dispatcher) pump(ctx context.Context, pm *pluginManager) {
	if d.ptp {
		d.pumpLeased(ctx, pm)
		return
	}
	d.pumpHeld(ctx, pm)
}

// pumpHeld is the PUBSUB loop, unchanged from K8 except that it reads the
// subscription through currentSub. It never calls Recv while it already holds an
// undelivered envelope, so a process dying between enter and Deliver (or a
// reload pausing dispatch between the two) retries the same envelope instead of
// losing it. The drain gate (pluginManager.reload) is what closes the duplicate
// window a bare kill-and-relaunch would leave open.
func (d *dispatcher) pumpHeld(ctx context.Context, pm *pluginManager) {
	for {
		env := d.takeHeld()
		if env == nil {
			var err error
			env, err = d.currentSub().Recv(ctx)
			if err != nil {
				return
			}
		}

		h, ok := pm.enter()
		if !ok {
			// No ready process, or a reload is draining: park the envelope
			// kernel-held and wait. It has not been dispatched, so it is
			// still exactly-once eligible once a process is ready again.
			d.setHeld(env)
			if !sleepOrDone(ctx) {
				return
			}
			continue
		}

		// Each delivery gets its own child context so the drain gate can
		// cancel a still-in-flight call at its deadline — that cancel is what
		// turns a deliberate stop into a Canceled, distinct from the
		// Unavailable a crashed child surfaces.
		dctx, dcancel := context.WithCancel(ctx)
		id := pm.trackCancel(dcancel)
		_, err := h.Deliver(dctx, env)
		pm.leave(id)
		dcancel()
		if err != nil {
			d.setHeld(env)
			pm.logDeliverFailure(ctx, env, err)
			if !sleepOrDone(ctx) {
				return
			}
			continue
		}
	}
}

// pumpLeased is the POINT_TO_POINT loop. Recv leases the next envelope in the
// transport; from there exactly one of Ack or Nack resolves the lease. A
// delivery that cannot happen now — no ready process, or a reload draining —
// nacks the lease straight back, so the transport reassigns it to a live member
// (a survivor on another node, or this sole member again) and the dispatcher
// keeps no copy. A delivery that fails — a dead worker surfaces Unavailable —
// nacks too: that is the C5 requeue-to-survivors path. The Ack or Nack lands
// BEFORE leave, so waitInflight cannot see the in-flight count reach zero until
// the lease is resolved; otherwise a reload's detach could reassign an envelope
// a delivery had already completed and journaled.
func (d *dispatcher) pumpLeased(ctx context.Context, pm *pluginManager) {
	for {
		env, err := d.currentSub().Recv(ctx)
		if err != nil {
			return // ctx done: the manager is closing, or a reload is resetting this pump
		}

		h, ok := pm.enter()
		if !ok {
			d.releaseLease(ctx, pm, env)
			if !sleepOrDone(ctx) {
				return
			}
			continue
		}

		dctx, dcancel := context.WithCancel(ctx)
		id := pm.trackCancel(dcancel)
		_, err = h.Deliver(dctx, env)
		if err != nil {
			pm.logDeliverFailure(ctx, env, err)
			d.releaseLease(ctx, pm, env)
		} else {
			d.ackLease(ctx, pm, env)
		}
		pm.leave(id)
		dcancel()
		if err != nil {
			if !sleepOrDone(ctx) {
				return
			}
		}
	}
}

// ackLease tells the transport the leased envelope is done. A lease already
// reclaimed by a reload's detach (leaseGone) is not an error: the detach has
// taken ownership, so there is nothing left to ack.
func (d *dispatcher) ackLease(ctx context.Context, pm *pluginManager, env *kernelv1.Envelope) {
	if err := pm.transport.Ack(d.subscriberID, env.GetMessageId()); err != nil && !leaseGone(err) {
		pm.logger.WarnContext(ctx, "harmonikd: ack leased message",
			"channel", env.GetChannel(), "message_id", env.GetMessageId(), "error", err.Error())
	}
}

// releaseLease hands the leased envelope back to the transport for reassignment
// to a live member. Same leaseGone tolerance as ackLease.
func (d *dispatcher) releaseLease(ctx context.Context, pm *pluginManager, env *kernelv1.Envelope) {
	if err := pm.transport.Nack(d.subscriberID, env.GetMessageId()); err != nil && !leaseGone(err) {
		pm.logger.WarnContext(ctx, "harmonikd: nack leased message",
			"channel", env.GetChannel(), "message_id", env.GetMessageId(), "error", err.Error())
	}
}

// leaseGone reports whether err means the lease this dispatcher tried to resolve
// is already gone — reclaimed by a reload's detach, on either the single-box
// transport or a mesh node. It is the one Ack/Nack outcome the pump may ignore.
func leaseGone(err error) bool {
	return errors.Is(err, transport.ErrNotLeased) ||
		errors.Is(err, transport.ErrUnknownSubscriber) ||
		errors.Is(err, memmesh.ErrUnknownMember)
}

// sleepOrDone waits one idleBackoff, or returns false the moment ctx is done.
func sleepOrDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(idleBackoff):
		return true
	}
}

func (d *dispatcher) takeHeld() *kernelv1.Envelope {
	d.mu.Lock()
	defer d.mu.Unlock()
	env := d.held
	d.held = nil
	return env
}

func (d *dispatcher) setHeld(env *kernelv1.Envelope) {
	d.mu.Lock()
	d.held = env
	d.mu.Unlock()
}

// pluginManager owns the one registered plugin this slice wires up: its
// manifest (stable across a process going away and coming back), the
// process handle a reload swaps, and the dispatchers that pump the
// transport's kernel-held subscriptions into that process. The
// subscriptions belong to the transport and are untouched by a reload or by
// the process dying, so whatever built up while no process held them is
// delivered once one does again.
type pluginManager struct {
	transport   transportPort
	dispatchers []*dispatcher
	logger      *slog.Logger

	// spawnPump starts one dispatcher's pump under a context derived from the
	// manager's dispatch context. It is a closure rather than a stored context so
	// a reload can restart a POINT_TO_POINT pump on a fresh subscription without
	// the manager holding a context.Context field.
	spawnPump func(*dispatcher)

	mu            sync.Mutex
	manifest      *kernelv1.PluginManifest
	current       *host.Host
	spec          host.LaunchSpec
	draining      bool
	drainDeadline time.Duration

	// inflight counts unary Deliver calls in progress; the drain gate waits
	// on it. cancels holds their per-call cancel funcs, keyed by cancelSeq,
	// so the gate can cancel every still-in-flight call at its deadline. A
	// CancelFunc is a func, not a stored context, so this stays clear of the
	// contained-context rule.
	inflight  sync.WaitGroup
	cancels   map[uint64]context.CancelFunc
	cancelSeq uint64

	stop context.CancelFunc
}

// launchPlugin runs spec through the full launch pipeline, declares its
// manifest's channels, subscribes to its channel interests, and starts one
// dispatcher per subscription. It returns once the plugin has reached
// RUNNING; the dispatchers keep going until the returned manager's close is
// called.
//nolint:contextcheck // the dispatchers' pump context is derived from context.Background() on purpose — they outlive this launch ctx; see dispatchCtx below.
func launchPlugin(ctx context.Context, spec host.LaunchSpec, t transportPort, logger *slog.Logger, onManifest func(*kernelv1.PluginManifest) error) (*pluginManager, error) {
	var opts []host.LaunchOption
	if onManifest != nil {
		opts = append(opts, host.OnManifest(onManifest))
	}
	h, err := host.Launch(ctx, spec, opts...)
	if err != nil {
		return nil, fmt.Errorf("harmonikd: launch plugin: %w", err)
	}

	manifest := h.Manifest()
	for _, decl := range manifest.GetChannels() {
		if err := t.Declare(decl.GetName(), decl.GetType()); err != nil {
			h.Kill()
			return nil, fmt.Errorf("harmonikd: declare %q: %w", decl.GetName(), err)
		}
	}

	// The dispatchers must outlive this call's own ctx — they keep pumping
	// for as long as the plugin manager exists, not just for the duration
	// of one launch — so this is deliberately not ctx's child. close()
	// cancels it. Same reasoning host.Launch itself documents for its own
	// context.Background() use.
	dispatchCtx, cancel := context.WithCancel(context.Background())
	pm := &pluginManager{
		transport:     t,
		logger:        loggerOrDefault(logger),
		manifest:      manifest,
		current:       h,
		spec:          spec,
		drainDeadline: defaultDrainDeadline,
		cancels:       make(map[uint64]context.CancelFunc),
		stop:          cancel,
	}

	// spawnPump captures dispatchCtx rather than storing it on the manager: a
	// context belongs in a call, not a struct field. It tracks each pump's cancel
	// and a done channel so a reload can stop a POINT_TO_POINT pump, re-attach its
	// member, and start it again on the fresh subscription.
	pm.spawnPump = func(d *dispatcher) {
		pumpCtx, pumpCancel := context.WithCancel(dispatchCtx)
		done := make(chan struct{})
		d.mu.Lock()
		d.pumpCancel = pumpCancel
		d.pumpDone = done
		d.mu.Unlock()
		go func() {
			defer close(done)
			d.pump(pumpCtx, pm)
		}()
	}

	for _, interest := range manifest.GetInterests() {
		chInterest := interest.GetChannel()
		if chInterest == nil {
			continue // roster interest: no delivery queue to pump in this slice
		}
		group := chInterest.GetGroup()
		sub, err := t.Subscribe(manifest.GetNamespace(), chInterest.GetPattern(), group)
		if err != nil {
			cancel()
			h.Kill()
			return nil, fmt.Errorf("harmonikd: subscribe %q: %w", chInterest.GetPattern(), err)
		}
		d := &dispatcher{
			subscriberID: manifest.GetNamespace(),
			pattern:      chInterest.GetPattern(),
			group:        group,
			ptp:          group != "",
			sub:          sub,
		}
		pm.dispatchers = append(pm.dispatchers, d)
		pm.spawnPump(d)
	}

	return pm, nil
}

func loggerOrDefault(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}
	return slog.Default()
}

// enter is the gate every dispatch passes through before it calls Deliver. It
// refuses — returns ok false — while a reload is draining or while no process
// is in StateRunning, which is what keeps a reload's new dispatches kernel-
// held instead of delivered to a dying process. On success it counts one
// in-flight delivery; the caller must pair it with leave.
func (pm *pluginManager) enter() (*host.Host, bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.draining || pm.current == nil || pm.current.State() != host.StateRunning {
		return nil, false
	}
	pm.inflight.Add(1)
	return pm.current, true
}

// trackCancel records one in-flight delivery's cancel func and returns the id
// leave removes it by. The drain gate cancels whatever is still tracked when
// its deadline passes.
func (pm *pluginManager) trackCancel(cancel context.CancelFunc) uint64 {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	id := pm.cancelSeq
	pm.cancelSeq++
	pm.cancels[id] = cancel
	return id
}

// leave ends the in-flight delivery enter started: it forgets the cancel func
// and drops the in-flight count so a draining reload can see the call finish.
func (pm *pluginManager) leave(id uint64) {
	pm.mu.Lock()
	delete(pm.cancels, id)
	pm.mu.Unlock()
	pm.inflight.Done()
}

// inFlightCount reports how many unary deliveries are in progress right now —
// the count the drain gate waits down to zero. Used by this package's own
// tests to fire a reload at a deterministic moment (a delivery is live),
// instead of racing a fixed sleep.
func (pm *pluginManager) inFlightCount() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return len(pm.cancels)
}

func (pm *pluginManager) liveHost() *host.Host {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.current == nil || pm.current.State() != host.StateRunning {
		return nil
	}
	return pm.current
}

// reload is the drain gate: it hot-swaps the plugin process without losing a
// message. go-plugin's own shutdown hard-stops an in-flight call, so the
// kernel drains itself first.
//
// Invariant: an envelope is always in exactly one of three states, never
// between — this is what makes VC-12's exact-count assertion satisfiable:
//   - never-dispatched: still kernel-held, delivered after reload;
//   - drained to completion: journaled before Kill (and, for a POINT_TO_POINT
//     lease, Acked before the in-flight count falls to zero);
//   - leased-but-undelivered work is nacked back to the group queue; there is
//     still no state in which an envelope is both unfinished and unowned.
func (pm *pluginManager) reload(ctx context.Context, spec host.LaunchSpec) error {
	pm.mu.Lock()
	pm.draining = true // new dispatches are refused at enter and stay kernel-held
	old := pm.current
	oldManifest := pm.manifest
	deadline := pm.drainDeadline
	pm.mu.Unlock()

	// Long-lived streams would be cancelled here (never waited on — a
	// subscription never ends on its own). This slice has none: the only
	// long-lived consumer is the kernel-held dispatcher loop, which enter now
	// turns away, so each dispatcher parks (PUBSUB) or nacks its lease (PTP).

	// Wait for in-flight unary deliveries to finish, but only to a deadline;
	// then cancel whatever is still running. A call that finished in time was
	// journaled — and, for PTP, Acked — before this point; one cancelled here
	// returns Canceled and its envelope goes back to the queue to be
	// redelivered: parked kernel-held for PUBSUB, nacked to the group for PTP.
	pm.waitInflight(deadline)
	pm.cancelInflight()

	// A POINT_TO_POINT member's lease queue lives in the transport, not in the
	// dying process, so it must be handed back before the process goes away:
	// stop the pump (so nothing races the detach), then detach, which nacks
	// everything the member held — assigned-but-unreceived and in-flight leased
	// alike — to the surviving members on other nodes. A PUBSUB dispatcher keeps
	// its pump and its kernel-held queue untouched, exactly as K8 left it.
	for _, d := range pm.dispatchers {
		if !d.ptp {
			continue
		}
		pm.stopPump(d)
		if err := pm.transport.Detach(d.subscriberID); err != nil && !leaseGone(err) {
			pm.logger.WarnContext(ctx, "harmonikd: detach member for reload",
				"subscriber", d.subscriberID, "error", err.Error())
		}
	}

	// Kill, then relaunch. host.Launch checks VERIFIED (a fresh sha256)
	// before it starts the binary.
	if old != nil {
		old.Kill()
	}
	h, err := host.Launch(ctx, spec)
	if err != nil {
		pm.mu.Lock()
		pm.current = nil
		pm.draining = false
		pm.mu.Unlock()
		return fmt.Errorf("harmonikd: reload plugin: %w", err)
	}

	// Diff the new manifest against the old: a PUBSUB channel in both persists,
	// and its kernel-held queue and the dispatcher pumping it are left untouched —
	// reload is not re-subscribe for PUBSUB. In this single-plugin slice the
	// manifest is stable, so every channel persists and no PUBSUB subscription is
	// rebuilt.
	pm.logManifestDiff(ctx, oldManifest, h.Manifest())

	pm.mu.Lock()
	pm.current = h
	pm.spec = spec
	pm.manifest = h.Manifest()
	pm.draining = false // dispatch resumes; parked PUBSUB dispatchers flush their held envelope
	pm.mu.Unlock()

	// Re-attach each POINT_TO_POINT member on a fresh subscription and restart
	// its pump. This runs after the process is RUNNING and draining is off, so the
	// restarted pump delivers straight away instead of nacking against a
	// not-yet-ready process.
	for _, d := range pm.dispatchers {
		if !d.ptp {
			continue
		}
		sub, subErr := pm.transport.Subscribe(d.subscriberID, d.pattern, d.group)
		if subErr != nil {
			return fmt.Errorf("harmonikd: re-subscribe %q after reload: %w", d.pattern, subErr)
		}
		d.setSub(sub)
		pm.spawnPump(d)
	}
	return nil
}

// stopPump cancels one dispatcher's pump goroutine and waits for it to exit, so
// a reload can detach and re-attach a POINT_TO_POINT member with no pump
// concurrently reading or resolving a lease on it.
func (pm *pluginManager) stopPump(d *dispatcher) {
	d.mu.Lock()
	cancel := d.pumpCancel
	done := d.pumpDone
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// waitInflight blocks until every counted in-flight delivery has finished, or
// until deadline, whichever comes first.
func (pm *pluginManager) waitInflight(deadline time.Duration) {
	done := make(chan struct{})
	go func() {
		pm.inflight.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(deadline):
	}
}

// cancelInflight cancels every delivery still tracked. It is a no-op when the
// deadline was met with nothing left running.
func (pm *pluginManager) cancelInflight() {
	pm.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(pm.cancels))
	for _, cancel := range pm.cancels {
		cancels = append(cancels, cancel)
	}
	pm.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// logDeliverFailure classifies a failed delivery. A Canceled is a deliberate
// stop — the drain gate cancelled this call at its deadline. An Unavailable is
// a plugin that died on its own (a crash, a kill signal). Everything else is
// an unclassified error.
func (pm *pluginManager) logDeliverFailure(ctx context.Context, env *kernelv1.Envelope, err error) {
	reason := "error"
	switch {
	case status.Code(err) == codes.Canceled || errors.Is(err, context.Canceled):
		reason = "canceled"
	case errors.Is(err, host.ErrUnavailable) || status.Code(err) == codes.Unavailable:
		reason = "unavailable"
	}
	pm.logger.WarnContext(ctx, "harmonikd: delivery failed",
		"reason", reason,
		"channel", env.GetChannel(),
		"message_id", env.GetMessageId(),
		"error", err.Error(),
	)
}

// logManifestDiff reports which channels persist across a reload, and warns if
// the channel set changed — this slice pumps only the channels subscribed at
// first launch, so an added or removed channel is worth surfacing.
func (pm *pluginManager) logManifestDiff(ctx context.Context, oldM, newM *kernelv1.PluginManifest) {
	persisted, added, removed := diffNames(channelNames(oldM), channelNames(newM))
	pm.logger.InfoContext(ctx, "harmonikd: reload manifest diff",
		"persisted", persisted, "added", added, "removed", removed)
	if len(added) > 0 || len(removed) > 0 {
		pm.logger.WarnContext(ctx, "harmonikd: reload changed the plugin's channel set; this slice pumps only the channels subscribed at first launch",
			"added", added, "removed", removed)
	}
}

func channelNames(m *kernelv1.PluginManifest) []string {
	names := make([]string, 0, len(m.GetChannels()))
	for _, decl := range m.GetChannels() {
		names = append(names, decl.GetName())
	}
	return names
}

// diffNames splits newNames against oldNames into the names in both
// (persisted), only in new (added), and only in old (removed).
func diffNames(oldNames, newNames []string) (persisted, added, removed []string) {
	oldSet := make(map[string]bool, len(oldNames))
	for _, n := range oldNames {
		oldSet[n] = true
	}
	newSet := make(map[string]bool, len(newNames))
	for _, n := range newNames {
		newSet[n] = true
	}
	for _, n := range newNames {
		if oldSet[n] {
			persisted = append(persisted, n)
		} else {
			added = append(added, n)
		}
	}
	for _, n := range oldNames {
		if !newSet[n] {
			removed = append(removed, n)
		}
	}
	return persisted, added, removed
}

// forceUnavailable kills the current process, if any, and clears it without
// launching a replacement, so every dispatcher stops delivering until
// reload brings a new one up. Production reaches the same effect through
// Deliver's own Unavailable detection when a process dies unexpectedly;
// this gives a caller — today, only this package's own tests — a way to
// force that state deterministically instead of racing a real crash.
func (pm *pluginManager) forceUnavailable() {
	pm.mu.Lock()
	h := pm.current
	pm.current = nil
	pm.mu.Unlock()
	if h != nil {
		h.Kill()
	}
}

// currentSpec reports the launch spec the live process was started from.
func (pm *pluginManager) currentSpec() host.LaunchSpec {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.spec
}

// currentManifest reports the registered plugin's manifest, as of the last
// launch or reload.
func (pm *pluginManager) currentManifest() *kernelv1.PluginManifest {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.manifest
}

// namespace reports the registered plugin's namespace.
func (pm *pluginManager) namespace() string {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.manifest.GetNamespace()
}

// pendingCount sums every dispatcher's pending count — the observable
// evidence that a publish landed while no process was ready to take it.
func (pm *pluginManager) pendingCount() int {
	total := 0
	for _, d := range pm.dispatchers {
		total += d.pending()
	}
	return total
}

// close stops every dispatcher and kills the live process, if any.
func (pm *pluginManager) close() {
	pm.stop()
	pm.mu.Lock()
	h := pm.current
	pm.mu.Unlock()
	if h != nil {
		h.Kill()
	}
}
