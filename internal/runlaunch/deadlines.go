package runlaunch

// deadlines.go — the HC-056 agent-readiness deadlines, their local/remote
// resolver, the typed timeout sentinel, and the kill-reap bound.
//
// Carved out of internal/daemon/agentready.go and internal/daemon/workloop.go by
// P2 unit E5 RT19b (pure move). The doc comments below travel VERBATIM: they
// carry the hk-do7te / hk-4hso5 / hk-5z1f0 / hk-96d7w tuning history and the
// HC-056 spec refs, and that history is the reason the values are what they are.
//
// Spec ref: specs/handler-contract.md §4.9 HC-056.

import (
	"errors"
	"time"
)

// KillReapTimeout bounds two operations in the HC-056
// agent_ready_timeout path:
//
//  1. Watcher-reap: maximum time to wait for watcher.Done() after Kill().
//     Kill() itself sends SIGTERM then SIGKILL (3 s grace); this 10 s covers
//     watcher teardown after SIGKILL lands. If the watcher does not exit, the
//     bead is still reopened — the stuck goroutine eventually unblocks when ctx
//     is cancelled.
//
//  2. Session-reap (hk-4hso5): bounds sess.Wait in the ErrAgentReadyTimeout
//     branch. For remote sessions where the pane stays alive after Kill,
//     runWait polls WindowPanePID until ctx is cancelled (up to 30 min). This
//     timeout caps that wait so ReopenBead is reached promptly regardless of
//     pane liveness.
//
// Declared as var so tests can override without waiting real wall time. It stays
// a mutable exported var here because internal/daemon/export_test.go overrides
// and restores it; that is exactly as safe (and as ugly) as it was in-package.
// Turning it into a config field is a signature change and belongs to
// RT15/RT17, where runexec.DispatchConfig.ReadyKillReap already exists as its
// home.
//
// Spec ref: specs/handler-contract.md §4.9 HC-056.
// Bead ref: hk-do7te, hk-4hso5.
// Origin: internal/daemon/workloop.go agentReadyKillReapTimeout.
var KillReapTimeout = 10 * time.Second

// DefaultAgentReadyTimeout is the HC-056 default: 150 seconds.
// Informed by claude cold-start latency (≤5s typical, 10–15s cold disk cache)
// plus margin for skill provisioning, one-time .claude/ filesystem warm-up,
// and concurrent-burst CPU/disk contention under --max-concurrent ≥ 4.
//
// The prior 30s default was tuned for single-instance cold-start; under
// concurrent dispatch bursts with high disk utilisation (≥90%) multiple
// agents compete for I/O and CPU during cold-start, pushing the longest-
// waiting agent past 30s. 90s provided headroom for a 4-wide burst under
// moderate disk pressure while remaining far below the 30-min implementer
// commit budget (hk-hzj). Operators may adjust per-environment via
// --agent-ready-timeout.
//
// hk-5z1f0: raised 90s→150s. Under the 10-concurrent ramp a remote worker
// hosts a 2nd (REVIEW-stage) cold-start claude spawn that must additionally
// clear reverse-SSH-tunnel readiness while competing with up to 6 concurrent
// agents; 90s was too tight for that second spawn and recurrently tripped
// agent_ready_timeout only on the remote worker. 150s covers the reviewer
// cold-start over the tunnel; a companion per-worker cold-start spawn
// semaphore (workLoopDeps.agentSpawnSem) bounds how many such spawns overlap.
//
// Spec ref: specs/handler-contract.md §4.9 HC-056.
// Origin: internal/daemon/agentready.go defaultAgentReadyTimeout.
const DefaultAgentReadyTimeout = 150 * time.Second

// DefaultRemoteAgentReadyTimeout is the HC-056 default applied to a REMOTE
// (SSH worker) agent spawn: 210 seconds.
//
// hk-96d7w (LOCAL slice of hk-5z1f0): a remote spawn clears reverse-SSH-tunnel
// readiness in addition to the claude cold-start itself, and — for the
// reviewer node specifically — competes with a resident implementer agent for
// CPU/disk on the same worker (up to agentSpawnSem's cap-3 concurrent
// cold-starts). 60s of headroom over the local default (150s) covers that
// additional tunnel + contention latency without masking a genuinely hung
// spawn. Operators may override via Config.RemoteAgentReadyTimeout /
// --remote-agent-ready-timeout.
//
// Spec ref: specs/handler-contract.md §4.9 HC-056.
// Bead ref: hk-96d7w. Sibling: hk-5z1f0 (remote canary, parked for live verify).
// Origin: internal/daemon/agentready.go defaultRemoteAgentReadyTimeout.
const DefaultRemoteAgentReadyTimeout = 210 * time.Second

// EffectiveAgentReadyTimeout resolves the agent_ready wait window for a
// single dispatch, given the configured local and remote overrides
// (Config.AgentReadyTimeout / Config.RemoteAgentReadyTimeout, threaded through
// workLoopDeps as agentReadyTimeout / remoteAgentReadyTimeout) and whether
// this particular run targets a remote worker.
//
// A non-positive override falls back to the matching compiled-in default
// (DefaultAgentReadyTimeout for local, DefaultRemoteAgentReadyTimeout for
// remote) — mirroring the zero-value-safe fallback waitAgentReady already
// applies for the local-only case.
//
// Bead ref: hk-96d7w.
// Origin: internal/daemon/agentready.go effectiveAgentReadyTimeout.
func EffectiveAgentReadyTimeout(local, remote time.Duration, isRemote bool) time.Duration {
	if isRemote {
		if remote > 0 {
			return remote
		}
		return DefaultRemoteAgentReadyTimeout
	}
	if local > 0 {
		return local
	}
	return DefaultAgentReadyTimeout
}

// ErrAgentReadyTimeout is the typed sentinel returned when no agent_ready event
// arrives within the configured timeout window.
//
// Callers (workloop integration hk-gql20.14/.15) MUST match this sentinel with
// errors.Is and respond by cancelling the session context, reaping the subprocess,
// emitting agent_failed{class=structural, sub_reason=agent_ready_timeout}, and
// reopening the bead per HC-056 steps 1–4.
//
// Spec ref: specs/handler-contract.md §4.9 HC-056.
// Origin: internal/daemon/agentready.go ErrAgentReadyTimeout.
var ErrAgentReadyTimeout = errors.New("agent_ready timeout: no agent_ready event within deadline (HC-056)")
