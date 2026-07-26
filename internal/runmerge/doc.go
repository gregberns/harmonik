// Package runmerge holds the run-branch merge path: the §4.12 EM-052/EM-053
// ordered merge-to-main sequence, the worktree hygiene around it, the
// gofumpt/gci format gate, the review-trailer amend, the run-context strip, and
// the merge path's own event emissions.
//
// # Why this package exists
//
// P2 unit E5 RT13 carved this concern out of internal/daemon/workloop.go. It is
// param-in / value-out git plumbing: callers pass ctx, paths, an
// [handlercontract.EventEmitter] and a [Submit] closure, and get an [Outcome]
// back. It holds no daemon state and reads no workLoopDeps field.
//
// The direction of the seam is daemon -> runmerge and never back: the daemon
// threads its mergeq exclusion-domain Submit in, and the .golangci.yml `runmerge`
// depguard block denies importing internal/daemon. Two functions that DID read
// workLoopDeps — emitBeadClosedAndMaybeEpic and maybeEmitEpicCompleted — stayed
// behind in internal/daemon rather than being dragged across behind a callback.
//
// Origin: internal/daemon/workloop.go (the merge region), stripruncontext_hk4je.go
// and reviewtrailers_hkdyim.go.
// Plan: plans/2026-07-21-p2-extraction/E5-dot-runloop.md §4 "RT13".
package runmerge
