package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
	"github.com/gregberns/harmonik/kernel/host"
	"github.com/gregberns/harmonik/kernel/transport"
)

// idleBackoff is how long the dispatch loop waits before checking again
// whether a delivery target has come back, while a plugin process is not in
// StateRunning. It does not pop a subscription's queue during that wait, so
// a publish that lands while the process is stopped stays kernel-held
// (transport.Subscription outlives the process) instead of being lost.
const idleBackoff = 20 * time.Millisecond

// dispatcher pumps one kernel-held subscription into whatever process its
// pluginManager currently holds. held is an envelope already popped from
// the subscription but not yet delivered — set whenever a process was not
// live for it, or Deliver against it failed — so Pending, and the loop
// itself, both treat "sitting in held" the same as "still queued in the
// subscription": either way it has not reached the plugin yet.
type dispatcher struct {
	sub *transport.Subscription

	mu   sync.Mutex
	held *kernelv1.Envelope
}

// pending reports how many envelopes this dispatcher has not yet delivered:
// whatever the subscription itself still buffers, plus one more if an
// envelope is currently held pending a live process.
func (d *dispatcher) pending() int {
	d.mu.Lock()
	held := d.held != nil
	d.mu.Unlock()
	n := d.sub.Pending()
	if held {
		n++
	}
	return n
}

// pump feeds sub into whatever process pm currently holds, until ctx is
// done. It never calls sub.Recv while it already holds an undelivered
// envelope, so a process dying between the readiness check and Deliver (or
// between the readiness check and Recv unblocking) retries the same
// envelope instead of losing it. That retry can duplicate a delivery whose
// call failed after the plugin had already acted on it; closing that window
// for good is K8's drain gate (plans/2026-09-07-harmonik-bus/
// 09-kernel-vc12-task-plan.md K8), out of scope here — K7 only needs
// "stopped, then delivered exactly when a process comes back" to hold.
func (d *dispatcher) pump(ctx context.Context, pm *pluginManager) {
	for {
		env := d.takeHeld()
		if env == nil {
			var err error
			env, err = d.sub.Recv(ctx)
			if err != nil {
				return
			}
		}

		h := pm.liveHost()
		if h == nil {
			d.setHeld(env)
			select {
			case <-ctx.Done():
				return
			case <-time.After(idleBackoff):
			}
			continue
		}

		if _, err := h.Deliver(ctx, env); err != nil {
			d.setHeld(env)
			// A dead process is handled by the liveHost check above on the
			// next iteration. A live process that keeps refusing this same
			// envelope (a bad payload, a wedged handler) would otherwise
			// spin this loop as fast as it can call Deliver; back off so
			// that failure mode costs a sleep, not a CPU.
			select {
			case <-ctx.Done():
				return
			case <-time.After(idleBackoff):
			}
			continue
		}
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
	transport   *transport.Transport
	manifest    *kernelv1.PluginManifest
	dispatchers []*dispatcher

	mu      sync.Mutex
	current *host.Host
	spec    host.LaunchSpec

	stop context.CancelFunc
}

// launchPlugin runs spec through the full launch pipeline, declares its
// manifest's channels, subscribes to its channel interests, and starts one
// dispatcher per subscription. It returns once the plugin has reached
// RUNNING; the dispatchers keep going until the returned manager's close is
// called.
func launchPlugin(ctx context.Context, spec host.LaunchSpec, t *transport.Transport) (*pluginManager, error) {
	h, err := host.Launch(ctx, spec)
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
	pm := &pluginManager{transport: t, manifest: manifest, current: h, spec: spec, stop: cancel}

	for _, interest := range manifest.GetInterests() {
		chInterest := interest.GetChannel()
		if chInterest == nil {
			continue // roster interest: no delivery queue to pump in this slice
		}
		sub, err := t.Subscribe(manifest.GetNamespace(), chInterest.GetPattern())
		if err != nil {
			cancel()
			h.Kill()
			return nil, fmt.Errorf("harmonikd: subscribe %q: %w", chInterest.GetPattern(), err)
		}
		d := &dispatcher{sub: sub}
		pm.dispatchers = append(pm.dispatchers, d)
		go d.pump(dispatchCtx, pm) //nolint:contextcheck // dispatchCtx deliberately outlives ctx; see the comment above its construction
	}

	return pm, nil
}

func (pm *pluginManager) liveHost() *host.Host {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.current == nil || pm.current.State() != host.StateRunning {
		return nil
	}
	return pm.current
}

// reload kills the current process, if any, and launches a fresh one from
// spec (the same launch spec as before, unless the caller passes a new
// one). It carries no drain guarantee — an in-flight Deliver at the moment
// of reload is not waited for; K8 owns that. Existing subscriptions are
// untouched, so whatever the old process never received stays queued and
// reaches the new one once a dispatcher finds it RUNNING again.
func (pm *pluginManager) reload(ctx context.Context, spec host.LaunchSpec) error {
	h, err := host.Launch(ctx, spec)
	if err != nil {
		return fmt.Errorf("harmonikd: reload plugin: %w", err)
	}

	pm.mu.Lock()
	old := pm.current
	pm.current = h
	pm.spec = spec
	pm.mu.Unlock()

	if old != nil {
		old.Kill()
	}
	return nil
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

// namespace reports the registered plugin's namespace.
func (pm *pluginManager) namespace() string {
	return pm.manifest.GetNamespace()
}

// pendingCount sums every dispatcher's pending count — the observable
// evidence that a publish landed while no process was RUNNING to take it.
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
