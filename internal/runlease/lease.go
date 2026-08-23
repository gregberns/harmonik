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

func (l *Lease) take() (took bool, err error) {
	release := l.spend()
	if release == nil {
		return false, nil
	}
	return true, release()
}

func (l *Lease) disarm() bool { return l.spend() != nil }

func (l *Lease) spend() func() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	release := l.release
	l.release = nil
	return release
}

// Give hands the resource back if disposition d releases it, and disarms the
// lease if d keeps it. It is what a release site calls when the resource comes
// back EARLY, before the scope that holds it closes — the hook session is given
// back at the end of a launch, and the launch may still be part of a run that
// keeps it.
//
// [Lease.Release] is the other call, and the difference is the whole point:
// Release gives the resource back unconditionally, so a site that used it here
// would defeat the survive disposition. Reach for Give wherever the run's answer
// applies, and for Release only where the resource is this caller's alone —
// the cold-start token, which the readiness edge always gives back.
//
// Like Release, Give acts at most once across every caller and every repeat: a
// lease the scope already closed, or an earlier Give already answered for, does
// nothing and reports nothing.
func (l *Lease) Give(d Disposition) Report { return l.give(d) }

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
