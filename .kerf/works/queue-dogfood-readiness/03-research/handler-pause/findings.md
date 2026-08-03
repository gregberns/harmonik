# Research — Handler pause

## Questions

1. Can queue recovery clear handler state or handler resume clear failed queue
   state?
2. What happens if a re-armed item still targets a paused handler?
3. What does durable recovery success mean when dispatch remains held?

## Findings

- `HP-020` makes handler pause separate from queue state. `HP-040` changes only
  handler state. `HP-043` says handler resume does not clear failed or drained
  queue state.
- `QM-052a` repeats this orthogonality. Submit and append check handler pause.
  The dispatcher later decides eligibility.
- Queue recovery must not call `HandlerPauseController`. It can re-arm an item
  to pending while the handler pause keeps dispatch held.

## Patterns to keep

- Recovery success means durable queue mutation. It does not mean that work
  launched.
- Keep the normal held-item event and its deduplication behavior.

## Risks and decisions

- Do not use `operator-resume` or handler resume for failed-item recovery.
- The best fit is to allow durable recovery while a handler pause remains. The
  later dispatch gate reports the hold. Rejecting recovery would make handler
  state prevent repair of separate queue state.
