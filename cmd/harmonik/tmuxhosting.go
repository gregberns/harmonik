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
// SPEC ALIGNMENT — the debt this file used to carry is PAID (specs amended
// 2026-07-28). This code is now the conforming implementation of:
//
//   - specs/process-lifecycle.md PL-021b item 3 — the three-outcome session
//     resolution below, including the rule that `tmux display-message` is NOT
//     consulted when $TMUX is unset, and that owns_session means "the daemon
//     created it" rather than "$TMUX was unset".
//   - PL-028b (retitled; the $TMUX-unset refusal is withdrawn) — resolve tmux
//     hosting rather than demand it, and announce a lost capability at boot
//     naming the CAPABILITIES lost, not merely the missing binary.
//   - PL-021a — the fail-fast is scoped to the substrate that hosts agents in
//     tmux; silent degradation stays forbidden.
//
// Two residuals are DECLARED in the specs rather than fixed here, so neither is
// mistaken for conformance:
//
//   - hk-0cjb8 — reportNoTmuxHosting returns exit code 1; PL-021b item 2 and
//     ON §8 code 22 say 22. Inherited (the deleted guard also returned 1); the
//     fix belongs to the ON §8 taxonomy pass, which also owns the ON-vs-PL
//     two-name split on 22 and the orphaning of code 24.
//   - hk-p01zm — on a no-tmux boot the reviewer substrate is left nil and the
//     work loop treats nil as "fall back to the general substrate", silently
//     un-pinning the claude reviewer from tmux (hk-qxvc2). The WARNING below
//     does not name the review loop among the lost capabilities. Declared at
//     specs/execution-model.md EM-015d-RIA step 2.
//
// SCOPE — this file frees the DAEMON boot path only. The CLI entry points were
// not touched and are not described by the amended clauses: run.go still
// self-exec-replaces itself with `tmux new-session` when $TMUX is unset, ahead
// of its own daemon-up check, so a thin-socket-client invocation against a
// running daemon still refuses to run outside tmux (hk-o3aj5, declared at
// PL-021a §SCOPE). It is the only such pre-daemon gate in cmd/.
//
// Locked decision #4's narrowing is recorded in STATUS.md §"Decisions in force"
// and in plans/2026-07-27-delete-and-rewrite/CHARTER.md §3. (An earlier version
// of this block pointed at docs/foundation/problem-space.md for the decision's
// canonical wording; it is not there.)

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
