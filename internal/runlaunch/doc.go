// Package runlaunch holds the effects a bead run performs around an agent
// launch — the CHB-018 pre-exec announcement relay, the HC-056 readiness
// deadlines and their sentinel, the spawn-cap / tmux-window / agent-ready
// anomaly events, the implementer phase-complete report, and the force-teardown
// backstop.
//
// # Why this package exists
//
// P2 unit E5 RT19b drained these helpers out of internal/daemon/workloop.go and
// internal/daemon/agentready.go, where they were stranded outside both E5
// extraction regions. Every one of them already took its inputs as explicit
// parameters — none read workLoopDeps — so the move is a change of package
// clause and a qualifier rewrite, with no new interface, port or injected
// closure anywhere.
//
// It is the EFFECT vocabulary that pairs with internal/runexec's PURE Dispatch
// machine, which models exactly the same launch -> ready -> working segment:
// [DefaultAgentReadyTimeout] / [EffectiveAgentReadyTimeout] and
// [KillReapTimeout] are what populate runexec.DispatchConfig.ReadyTimeout and
// .ReadyKillReap at each dispatch site.
//
// # Why a leaf, and not a file inside the run loop
//
// internal/daemon/bootstate.go calls [EmitSpawnCapBlocked] and
// [EmitTmuxNewWindowTimeout] from the BOOT path (boot-time spawn-semaphore
// instrumentation, not the run path), and bootstate.go never moves. So these
// symbols must live somewhere BOTH the run path and the boot path can import.
// The direction of the seam is daemon -> runlaunch and never back; the
// .golangci.yml `runlaunch` depguard block denies importing internal/daemon.
//
// # Charter — do not widen it
//
// This package covers exactly the four families named above. internal/daemon
// legitimately retains ~25 other emit* helpers (emitRunStarted, emitRunCompleted,
// emitImplementerEscapedWorktree, emitPostAgentReadyHang, emitReviewerVerdict,
// emitWorkloopLifecycleTransition, …) which are inside beadRunOne or read
// workLoopDeps. They are the E5 lift's problem, not this package's. Do not let a
// "while I'm here" impulse drag them in.
//
// Origin: internal/daemon/workloop.go and internal/daemon/agentready.go.
// Plan: plans/2026-07-21-p2-extraction/RT19b-stranded-run-path-helpers.md.
package runlaunch
