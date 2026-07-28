package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// tmuxhosting.go — boot-time resolution of the daemon's tmux hosting capability.
//
// Historically the composition root refused to boot at all when $TMUX was unset
// (specs/process-lifecycle.md §4.7 PL-021b item 3 / §4.10 PL-028b), on the
// rationale of locked decision #4 ("Inspectability via tmux is a requirement,
// not a preference"). Operator direction 2026-07-28 reopened that decision:
//
//	"If we get more flexibility from removing that, then do so. Seems like
//	 another 'crossed wires' issue where the underlying reasoning was lost.
//	 Maybe it was from before when not run as a daemon. Doesn't matter — but we
//	 should probably be able to run it any way."
//
// What was authorized is that the daemon MUST be able to boot without an
// ambient tmux client — NOT that tmux is removed. tmux stays the default host
// and the operator's inspection surface. So the refusal is replaced by a
// capability resolution with three honest outcomes:
//
//   - ambient session ($TMUX set)  — unchanged; spawn into the operator's session.
//   - no ambient session, tmux OK  — create and keep alive the deterministic
//     per-project session harmonik-<hash>-default and spawn there. The operator
//     inspects with `tmux attach -t <name>`. Inspectability is fully preserved;
//     only the "operator is already looking at it" convenience is lost.
//   - tmux unusable (absent / < 3.0 / cannot create a session) — no tmux hosting
//     at all. The caller decides: fatal when the tmux substrate is the one being
//     wired (every spawn would fail — see tmuxSubstrateSelected), a loud warning
//     when the structured Codex driver is selected (it owns child stdio and
//     never shells out to tmux).
//
// Note the deliberate asymmetry with the pre-existing code: `tmux display-message`
// is consulted ONLY when $TMUX is set. Without an ambient client that command
// answers with whatever session the tmux server most recently made current —
// which may be a crew or captain session — so trusting it would scatter
// implementer windows into sessions the daemon does not own. An absent ambient
// client means "resolve deterministically", which is exactly what
// tmux.ResolveDaemonSpawnSession does for an empty live session.
//
// SPEC DEBT — this code KNOWINGLY diverges from specs/, which is normative here.
// The amendment is NAMED, not made: it is a multi-clause spec pass that does not
// belong inside a behavior change. Whoever does it must cover the whole set, not
// just the two clauses the guard cited:
//
//   - specs/process-lifecycle.md PL-021a — the real locked-decision-#4 anchor:
//     "handler spawns MUST fail-fast ... rather than silently degrading to
//     non-tmux mode". Amending only PL-021b/PL-028b leaves this one contradicting.
//   - PL-021b item 3 — "If TMUX is unset, the daemon MUST NOT proceed to spawn
//     handler subprocesses ... a daemon that reaches the dispatch loop without
//     TMUX is a defect."
//   - PL-028b — daemon MUST refuse the ready state, MUST NOT silently create its
//     own session when $TMUX is unset.
//   - PL-028 refinement item 3 and the PL-028 `harmonik runner` step-3 bullet.
//   - specs/cognition-loop.md CL-081 — flywheel pane inherits the session from $TMUX.
//   - Re-scope (not repeal) to mode-conditional: WM-002a, PL-021c, HC-054.
//   - Exit-code taxonomy: code 22 on probe failure originates in PL-021a
//     (`ntm-unavailable`, per PL-008a) and is retitled `tmux-unavailable` by
//     PL-021b item 2; PL-028b mandates 24 on $TMUX-unset. This code returns 1.
//     That divergence is INHERITED (the deleted guard also returned 1), but the
//     two spec-distinguished conditions now collapse behind one code, so the
//     taxonomy pass belongs in the same amendment.
//     cmd/harmonik/supervise/start.go still reserves 24 citing PL-028b.
//
// Locked decision #4's canonical wording is NOT in STATUS.md (that section points
// at git history) — it is in docs/foundation/problem-space.md.

// bootNotef writes an operator-facing boot diagnostic. The write error is
// deliberately dropped: the destination is the daemon's stderr, and a daemon
// that cannot write to stderr has no better channel to report that on.
func bootNotef(out io.Writer, format string, a ...any) {
	fmt.Fprintf(out, format, a...) //nolint:errcheck // diagnostic write to stderr; error non-actionable
}

// tmuxHosting is the resolved boot-time tmux hosting capability.
type tmuxHosting struct {
	// Available reports that tmux is usable AND a spawn-target session is ready.
	// When false, SessionName is empty and nothing may be spawned via tmux.
	Available bool

	// SessionName is the resolved spawn-target session name.
	SessionName string

	// NeedKeepalive is true when the daemon created the session itself and is
	// therefore responsible for keeping it alive for its whole lifetime
	// (hk-9ptu). False when the daemon is spawning into a session it does not
	// own — the operator's ambient one.
	NeedKeepalive bool

	// Ambient reports that $TMUX was set at boot.
	Ambient bool

	// Err is the reason tmux hosting is unavailable. Non-nil iff !Available.
	Err error
}

// resolveTmuxHosting probes tmux, resolves the daemon's spawn-target session,
// and creates that session when the daemon must own it. Progress and failure
// notes are written to out (the daemon's stderr in production).
//
// It never returns an error: an unusable tmux is a resolved state
// (Available=false, Err set), not a caller-level failure. Whether that state is
// fatal depends on which substrate is being wired, which is the caller's call.
func resolveTmuxHosting(ctx context.Context, projectDir string, adapter tmux.OSAdapter, out io.Writer) tmuxHosting {
	h := tmuxHosting{Ambient: os.Getenv("TMUX") != ""}

	// Probe tmux version (≥ 3.0 required for -e env-injection per PL-021b item 2).
	if probeErr := adapter.ProbeTmux(ctx); probeErr != nil {
		h.Err = probeErr
		return h
	}

	// hk-9vp51 + hk-u9ji: when the daemon IS inside a tmux client, prefer the
	// live session it is running in — it provably exists right now, so
	// SpawnWindow can never hit "session does not exist". Exclude the system
	// sessions (supervisor / flywheel shim) that must not receive implementer
	// windows; ResolveDaemonSpawnSession owns that exclusion.
	//
	// This deliberately does NOT switch to a boot-time deterministic name for
	// the ambient case (an earlier sub-fix did, and the created session did not
	// persist to dispatch time → every spawn failed in 0.6 s, reverted fe94e0b1).
	liveSession := ""
	if h.Ambient {
		// exec.CommandContext, not the adapter, because no window handle exists yet.
		if outBytes, dmErr := exec.CommandContext(ctx, "tmux", "display-message", "-p", "#{session_name}").Output(); dmErr != nil {
			// Non-fatal: an empty live session forces the deterministic fallback.
			bootNotef(out, "harmonik: tmux display-message failed (%v); falling back to deterministic daemon session\n", dmErr)
		} else {
			liveSession = strings.TrimSpace(string(outBytes))
		}
	}

	sessionName, needEnsure := tmux.ResolveDaemonSpawnSession(projectDir, liveSession)

	if needEnsure {
		// A detached session with a live shell persists for the daemon's whole
		// lifetime, and the coordinator reaper only targets "-flywheel" sessions
		// (never this "-default" one), so it is present at dispatch time.
		if ensErr := adapter.EnsureSession(ctx, sessionName, projectDir); ensErr != nil {
			h.Err = fmt.Errorf("cannot ensure daemon tmux session %q: %w", sessionName, ensErr)
			return h
		}
		if h.Ambient {
			bootNotef(out, "harmonik: spawning implementer windows into daemon-owned session %q (ambient session was supervisor/flywheel/empty)\n", sessionName)
		} else {
			bootNotef(out, "harmonik: no ambient tmux client ($TMUX unset) — created daemon-owned session %q; inspect live agents with: tmux attach -t %s\n", sessionName, sessionName)
		}
	}

	h.Available = true
	h.SessionName = sessionName
	h.NeedKeepalive = needEnsure
	return h
}

// reportNoTmuxHosting writes the boot-time notice for a daemon that has no tmux
// hosting. It returns the process exit code to use: non-zero when the loss is
// fatal (the tmux substrate is the one being wired, so every agent spawn would
// fail), 0 when the daemon may continue in a degraded, non-inspectable mode.
//
// Degrading honestly is the whole point: the daemon says exactly which
// capabilities it has lost, at boot, rather than booting quietly and failing on
// the first dispatch.
func reportNoTmuxHosting(h tmuxHosting, tmuxSubstrate bool, out io.Writer) int {
	if tmuxSubstrate {
		bootNotef(out, "harmonik: FATAL — tmux hosting is unavailable (%v) and the tmux substrate is selected.\n"+
			"  Every agent spawn routes through `tmux new-window`, so the daemon would boot and then fail on the first dispatch.\n"+
			"  Fix one of:\n"+
			"    - install tmux >= 3.0 on PATH (an ambient $TMUX session is NOT required — the daemon creates its own)\n"+
			"    - select the structured Codex driver, which owns child stdio and needs no tmux: %s=codexdriver\n",
			h.Err, substrateSelectEnv)
		return 1
	}
	bootNotef(out, "harmonik: WARNING — running WITHOUT tmux hosting (%v).\n"+
		"  The structured Codex driver is selected, so agent dispatch works: it owns child stdio directly.\n"+
		"  DEGRADED, until tmux >= 3.0 is on PATH:\n"+
		"    - NO SUPERVISOR: `harmonik supervise start` builds its flywheel session with tmux, so this\n"+
		"      daemon has no auto-revive — if it dies, nothing restarts it.\n"+
		"    - `harmonik start crew` / `start captain` / keeper nudges cannot create their tmux sessions\n"+
		"    - no attachable agent panes; live inspection is `harmonik subscribe` only, plus the\n"+
		"      capture-tee IF you opt in by setting %s (it is off by default)\n",
		h.Err, captureDirEnv)
	return 0
}
