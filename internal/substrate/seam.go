package substrate

import "context"

// EventSource is the generic producer half of the record→replay seam. Any
// live source, a synthetic slice, or a replay Twin all satisfy it (RS-001).
type EventSource[E any] interface {
	Events(ctx context.Context) <-chan E
}

// Effector is the generic consumer half of the seam. A real side-effect sink,
// an in-memory recorder, or a bridge sink all satisfy it (RS-001).
type Effector[A any] interface {
	Execute(ctx context.Context, a A) error
}

// Run is the normative driver loop of the seam. It is a FREE FUNCTION, not a
// method: Go forbids generic methods, and the vertical's step carries vertical
// semantics, not substrate semantics (RS-002).
//
// Run ranges over src.Events(ctx), applies step to each event, and calls
// eff.Execute for each returned action in order. It returns nil on source
// exhaustion (channel close) and the first effector error otherwise, without
// executing further actions. Run never closes the source's channel — the
// source owns closure. Cancellation is delivered through ctx into src.Events,
// whose producing goroutine stops and closes its channel.
func Run[E, A any](ctx context.Context, src EventSource[E], step func(E) []A, eff Effector[A]) error {
	for ev := range src.Events(ctx) {
		for _, a := range step(ev) {
			if err := eff.Execute(ctx, a); err != nil {
				return err
			}
		}
	}
	return nil
}
