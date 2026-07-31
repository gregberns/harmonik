package runlease

import "sync"

// A Lease is one held resource together with the single call that gives it
// back. The call runs at most once no matter how many goroutines ask, so a
// resource cannot be given back twice, and it cannot be given back at all once
// a [Scope] has decided to leave it standing.
//
// Build one with [Hold], or with [Scope.Hold] to have a scope hold it too.
type Lease struct {
	resource Resource

	mu sync.Mutex
	// release is the caller's give-back call, and it is nil once the lease is
	// spent. Nil is therefore both "already given back" and "there was never
	// anything to call", which are the same thing to every reader.
	release func() error
}

// Hold records that the caller holds r and that release gives it back.
//
// A nil release means the resource needs no call to give it back. The lease is
// then born spent: it still names its resource, and it can never fire.
func Hold(r Resource, release func() error) *Lease {
	return &Lease{resource: r, release: release}
}

// Resource returns what this lease holds.
func (l *Lease) Resource() Resource { return l.resource }

// spent reports whether the lease has been given back, disarmed, or was born
// with nothing to call. A spent lease will never call anything.
//
// It is deliberately not exported. A release site that asks whether the release
// already happened is the shape this package exists to remove (RSM-036); the
// answer is always "call Release and let the lease decide". The tests in this
// package are the only readers.
func (l *Lease) spent() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.release == nil
}

// Release gives the resource back, and does so exactly once across every caller
// and every repeat. A second call is not an error: a resource already given
// back is in the state the caller wanted.
//
// A release that fails still spends the lease. The caller does not retry it —
// a give-back call that cannot succeed does not succeed on a second try, and
// retrying is how one stuck resource becomes a loop.
func (l *Lease) Release() error {
	_, err := l.take()
	return err
}

// take spends the lease, makes its give-back call, and reports whether this
// call is the one that spent it.
//
// The lock is released before the call, because the call reaches the world —
// it kills a process or removes a directory — and a lock held across that is
// held for an unbounded time.
func (l *Lease) take() (took bool, err error) {
	release := l.spend()
	if release == nil {
		return false, nil
	}
	return true, release()
}

// disarm spends the lease and drops its give-back call without making it. It
// reports whether this call is the one that spent it. Disarming reaches
// nothing, so unlike [Lease.take] it cannot fail.
func (l *Lease) disarm() bool { return l.spend() != nil }

// spend takes the give-back call out of the lease, leaving it spent, and
// returns it. A second caller gets nil, which is what makes every path through
// this type run the call at most once.
func (l *Lease) spend() func() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	release := l.release
	l.release = nil
	return release
}

// give implements held. A lease the disposition keeps is disarmed rather than
// left armed, so no later caller can give back what the run decided to leave
// standing.
func (l *Lease) give(d Disposition) Report {
	if !d.Releases(l.resource) {
		if l.disarm() {
			return Report{Kept: []Resource{l.resource}}
		}
		return Report{}
	}
	took, err := l.take()
	switch {
	case !took:
		return Report{}
	case err != nil:
		return Report{Failures: []Failure{{Resource: l.resource, Err: err}}}
	default:
		return Report{Released: []Resource{l.resource}}
	}
}
