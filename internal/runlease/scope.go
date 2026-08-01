package runlease

import (
	"errors"
	"fmt"
	"sync"
)

// held is what a scope can hold: a [Lease], or a nested [Scope]. The method is
// unexported, so the set is closed to this package and no caller can add a
// third kind of thing to a scope.
type held interface {
	give(Disposition) Report
}

// A Scope is the ordered set of resources one run holds. [Scope.Close] gives
// them back in the reverse of the order they were taken — innermost first —
// and asks one [Disposition] about the whole set rather than carrying a
// predicate per resource.
//
// The zero value is an empty open scope and is ready to use. A Scope is safe
// for concurrent use.
type Scope struct {
	mu     sync.Mutex
	items  []held
	closed bool
	// final is the disposition Close used, and it is read only when closed. It
	// exists so that a resource taken after the scope closed is given back
	// under the same answer the rest of the run got.
	final Disposition
}

// Hold adds a lease on r to the scope and returns it. The caller may give the
// resource back early through the returned lease, and the scope then has nothing
// left to do for it. Two do: the cold-start token comes back on the readiness
// edge through [Lease.Release], because it is the run's own bookkeeping and no
// disposition keeps it, and the hook session comes back at the end of a launch
// through [Lease.Give], because the survive disposition does keep that one.
//
// A nil release means the resource needs no call to give it back; see [Hold].
func (s *Scope) Hold(r Resource, release func() error) *Lease {
	l := Hold(r, release)
	s.add(l)
	return l
}

// Nest returns a child scope that this scope holds.
//
// A run's per-launch resources have a shorter life than its per-run ones: a
// graph run takes and gives back one hook session and one agent session per
// node while it holds one worktree and one tunnel for the whole run. Each node
// gets a child, and closing the child at the end of the node is the normal
// path. Closing the parent closes any child still open, in the parent's reverse
// order and under the parent's disposition, so a node that returns without
// closing its child leaks nothing.
func (s *Scope) Nest() *Scope {
	c := &Scope{}
	s.add(c)
	return c
}

// Close gives back everything the scope still holds and reports what it did.
//
// Close is idempotent. A second call holds nothing and reports nothing, so a
// deferred Close and an explicit one on the success path do not fight.
//
// The first close is the one that answers. A second call with a different
// disposition does not overwrite the first: the run decides once, and a later
// caller who decided differently must not be able to change what a resource
// taken after the close is given back under.
func (s *Scope) Close(d Disposition) Report {
	s.mu.Lock()
	items := s.items
	s.items = nil
	if !s.closed {
		s.closed = true
		s.final = d
	}
	s.mu.Unlock()

	var rep Report
	for i := len(items) - 1; i >= 0; i-- {
		rep.merge(items[i].give(d))
	}
	return rep
}

// give implements held, so a scope can be nested in another scope.
func (s *Scope) give(d Disposition) Report { return s.Close(d) }

// add puts h at the top of the scope's stack.
//
// A scope that is already closed keeps nothing: h is given back at once, under
// the disposition the close used. Taking a resource after the scope that owns
// it has closed is a caller mistake, and holding it forever in a stack nobody
// will walk again is the worse answer to it.
func (s *Scope) add(h held) {
	s.mu.Lock()
	if s.closed {
		d := s.final
		s.mu.Unlock()
		h.give(d)
		return
	}
	s.items = append(s.items, h)
	s.mu.Unlock()
}

// Report is what one [Scope.Close] did.
//
// A resource appears in at most one of the three lists, in the order the close
// walked. Released is what the scope gave back. Kept is what the disposition
// left standing, and those leases are disarmed. Failures is what the scope
// tried to give back and could not; a failed release is spent and is not
// retried. A lease already given back before the close, and a lease that never
// had anything to call, appear in none of the three.
type Report struct {
	Released []Resource
	Kept     []Resource
	Failures []Failure
}

// Failure is one give-back call that did not succeed.
type Failure struct {
	Resource Resource
	Err      error
}

// Error makes a Failure readable on its own.
func (f Failure) Error() string { return fmt.Sprintf("release %s: %v", f.Resource, f.Err) }

// Unwrap exposes the underlying error to errors.Is and errors.As.
func (f Failure) Unwrap() error { return f.Err }

// Err joins every failure in the report, or returns nil when there were none.
// It is what a caller logs; nothing in the run path should fail on it, because
// the resources are already spent either way.
func (r Report) Err() error {
	if len(r.Failures) == 0 {
		return nil
	}
	errs := make([]error, len(r.Failures))
	for i, f := range r.Failures {
		errs[i] = f
	}
	return errors.Join(errs...)
}

// merge appends one lease's or one child scope's result onto this report.
func (r *Report) merge(other Report) {
	r.Released = append(r.Released, other.Released...)
	r.Kept = append(r.Kept, other.Kept...)
	r.Failures = append(r.Failures, other.Failures...)
}
