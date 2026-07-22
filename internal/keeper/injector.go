package keeper

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/substrate"
)

// wrapUpWarningText is the prompt injected into the managed pane when the
// context-window percentage crosses the warn threshold. Advisory only — no exit
// instruction (agents without a supervised respawn path must not stop).
const wrapUpWarningText = "[KEEPER WARN] Context threshold crossed. " +
	"At a clean stop: commit + write HANDOFF-<name>.md (KEEPER nonce). Keep working."

// restartNowCmdToken is the EXACT, load-bearing command an actionable-warn agent
// must run to self-restart. It is templated INTO ActionableWarnText (never
// concatenated free-form) so a custom warn-text override CANNOT drop the required
// command token: the agent always receives the verbatim command. Refs: hk-vs4u.
const restartNowCmdToken = "harmonik keeper restart-now --agent %s"

// ActionableWarnText produces the R3 self-service restart handshake text injected
// at the warn crossing when the keeper warns AND the agent can act (captain, or a
// crew with self_service.crews_enabled). Unlike the lighter advisory, it names the
// EXACT two-step procedure so the agent self-restarts and the keeper's existing
// restart-now path completes the clear→resume cycle:
//
//	(a) run /session-handoff, then
//	(b) run `harmonik keeper restart-now --agent <name>`.
//
// The live token count and band (warn/act, in thousands) are interpolated so the
// agent sees its real position. A fall-through line tells it to act ONLY at a clean
// stop — if mid-task, finish first, because the keeper auto-restarts at the act
// ceiling regardless. The restart-now command is templated IN (restartNowCmdToken),
// so even a config override of this whole string cannot strip the required token;
// the watcher selection layer enforces that the override still contains it. Refs:
// hk-vs4u (R3 actionable warn → self-service restart handshake), hk-5da7 (ack).
func ActionableWarnText(agent string, tokens, warn, act int64) string {
	cmd := fmt.Sprintf(restartNowCmdToken, agent)
	return fmt.Sprintf(
		"[KEEPER WARN] Context at %dk tokens (warn %dk / act %dk). "+
			"Self-restart now: (a) run /session-handoff, then "+
			"(b) run `%s`. "+
			"Only at a clean stop; if mid-task, finish first — the keeper auto-restarts at %dk.",
		tokens/1000, warn/1000, act/1000, cmd, act/1000)
}

// InjectOnDemandRestartWarning delivers the on-demand-restart actionable warn text
// for the named agent into the tmux pane at tmuxTarget. Used when
// WatcherConfig.OnDemandRestart is true (e.g. the captain session). It is a thin
// wrapper over ActionableWarnText (hk-vs4u), preserving the historical signature
// for callers that only have the agent name. The token/band figures default to the
// compiled warn/act band when the live values are unknown at the call site; the
// watcher passes the live figures via the selection layer. Refs: hk-xjlq, ON-059,
// hk-vs4u.
func InjectOnDemandRestartWarning(ctx context.Context, tmuxTarget, agentName string) error {
	text := ActionableWarnText(agentName, defaultWarnAbsTokens, defaultWarnAbsTokens, defaultActAbsTokens)
	return InjectText(ctx, tmuxTarget, text)
}

// --- K2 leader defer template (SK-026 / SK-027 / SK-033) ------------------------
//
// The leader comms nudge body is a normative template with four fixed structural
// slots (SK-026); their PROSE is tunable but their PRESENCE is not (SK-033). Each
// slot has a load-bearing anchor token — templated the way restartNowCmdToken is —
// that the compiled default embeds and a valid operator override MUST retain, or
// the override falls back to the compiled default (leaderDeferHasAllSlots, watcher.go).

const (
	// Slot 1 — defer condition A (SK-026.1): finish the operator exchange first.
	deferOperatorExchangeToken = "finish the operator exchange"
	// Slot 2 — defer condition B (SK-026.2): finish the in-flight unit of work first.
	deferInflightUnitToken = "finish the in-flight unit"
	// Slot 3 anchor — the good-stopping-point self-test (SK-026.3 / SK-027). The
	// verbatim four-part criterion is goodStoppingPointSelfTest; this short anchor
	// is what the override-completeness check keys on.
	goodStoppingPointToken = "good stopping point"
)

// restartNowNonceCmdToken is the SK-030 restart-now command carrying the cycle
// nonce — slot 4 of the leader defer body (SK-026.4). Distinct from
// restartNowCmdToken (the actionable-warn path, --agent only) so that path stays
// unchanged; the verbatim "harmonik keeper restart-now" stem is shared, so
// containsRestartNowCmd validates slot 4 the same way. Refs: SK-029, SK-030.
const restartNowNonceCmdToken = "harmonik keeper restart-now --agent %s --nonce %s"

// goodStoppingPointSelfTest is the verbatim SK-027 four-part good-stopping-point
// criterion embedded as slot 3 of the leader defer body. Its presence is normative
// (SK-026.3 / SK-033); surrounding prose is tunable. The keeper nudges and bounds
// this self-assessment — it is agent-owned; the keeper cannot read the agent's
// context and does not claim to detect a task boundary (SK-027).
const goodStoppingPointSelfTest = "A good stopping point is one where nothing needed to continue lives only in your context: " +
	"(i) you are between discrete units, not mid-edit / mid-plan / mid-tool-sequence; " +
	"(ii) in-flight work is committed or trivially re-derivable; " +
	"(iii) no unanswered operator question is held; and " +
	"(iv) the next session resumes from the handoff plus durable substrate with no redo and no lost decision."

// handoffWriteGuardHint tells the agent to Read the handoff file before writing
// it. Claude Code's Write tool REFUSES to overwrite a file the current session
// has not Read, and after a /clear the rebooted session has read nothing — so a
// crew handed a pre-existing HANDOFF-<name>.md burns a round trip discovering
// that. The session-handoff skill is user-global (not in this repo), so the
// injected directive is the only place we control. Kept in one constant so the
// auto-cycle directive, the leader defer body, and the keeper SKILL.md all say
// the same thing. Refs: hk-4tjyj / hk-pgtt6.
const handoffWriteGuardHint = "The handoff file already EXISTS: Read it first, then Write it — " +
	"the Write tool refuses a file this session has not Read."

// handoffDirective renders the /session-handoff directive the keeper pastes into
// the agent's pane at the start of a cycle.
//
// It is ONE LINE on purpose (hk-pgtt6). The previous shape put "\n\n" between the
// path and the IMPORTANT clause; Claude Code collapses a pasted multi-line block
// into a single slash-command argument, so the newlines vanished entirely and the
// crew saw `HANDOFF-<name>.mdIMPORTANT: include exactly this line…` — the path and
// the instruction fused into one token. A VISIBLE separator (" — ") survives that
// collapse; whitespace does not.
func handoffDirective(path, nonce string) string {
	return fmt.Sprintf(
		"/session-handoff %s — IMPORTANT: include exactly this line verbatim in the handoff file: %s — %s",
		path, nonce, handoffWriteGuardHint,
	)
}

// LeaderDeferBody renders the compiled-default K2 leader defer nudge body: the
// normative four-slot template (SK-026) — defer-A, defer-B, the verbatim SK-027
// self-test, and the SK-030 restart-now command carrying the cycle nonce. This is
// the fallback body used whenever no valid operator override is configured
// (selectLeaderDeferText). It sits UNDER the unchanged FORCE-ACT backstop (SK-028):
// "take your time" is bounded, not open-ended. agent is the session name; nonce is
// the keeper cycle id, carried for audit (SK-030). Refs: T3.
func LeaderDeferBody(agent, nonce string) string {
	return fmt.Sprintf(
		"[KEEPER] Context threshold crossed — plan to restart soon, but at a %s, not mid-flow. "+
			"If you are mid-conversation with the operator, %s first. "+
			"If you are mid-task, %s first. "+
			"%s "+
			"Then self-restart: run /session-handoff — include the marker %s verbatim in your "+
			"HANDOFF-<name>.md — then run `%s`. %s",
		goodStoppingPointToken,
		deferOperatorExchangeToken,
		deferInflightUnitToken,
		goodStoppingPointSelfTest,
		nonceMarker(nonce),
		fmt.Sprintf(restartNowNonceCmdToken, agent, nonce),
		handoffWriteGuardHint,
	)
}

// AckLine formats the verifiability ACK line that the keeper injects into the
// agent's pane (via the restart-now / ping injection surface) on a restart-now
// or ping request. The agent arms a timer after firing the request and waits for
// this exact line (matched on the nonce) to confirm the keeper received it —
// instead of trusting a silent success. kind is "restart" or "ping".
// Refs: hk-5da7 (operator-specified ack handshake).
func AckLine(nonce, kind string) string {
	return fmt.Sprintf("[KEEPER ACK %s] received %s", nonce, kind)
}

// bufferName is the tmux buffer name used for keeper injections. Using a
// keeper-specific name avoids clobbering buffers owned by the daemon's own
// paste-inject step (which uses buffers like "hk-<run_id>").
const bufferName = "hk-keeper-warn"

// submitSettle is the grace period between the bracketed-paste write and the
// first submit Enter. Without it the post-paste Enter can land before the REPL
// input handler has finished accepting the pasted text, and the line sits
// unsubmitted in the pane buffer (hk-89g; same race as the implementer path's
// hk-jzpqo / hk-wcv). Mirrors internal/daemon/pasteinject.go's splash-settle.
var submitSettle = 750 * time.Millisecond

// submitRetries / submitRetryDelay re-send the submit Enter a bounded number of
// extra times to defend against a dropped first keypress. A redundant Enter at
// an already-submitted REPL is a harmless empty line. Mirrors the daemon's
// resumeSubmitRetries / resumeSubmitRetryDelay (hk-ip33d, hk-7rgqs).
const submitRetries = 2

// submitRetryDelay is a var (not a const) for the same reason submitSettle is:
// tests can zero it to drive the retry loop instantly without skipping it. Its
// designed value (400ms) is regression-guarded by TestInjectText_SettleConstants.
var submitRetryDelay = 400 * time.Millisecond

// tmuxRunFn is the seam through which the injector shells out to tmux. It runs
// the given tmux argv (with optional stdin) and returns the combined
// stdout+stderr plus any error — the same surface CombinedOutput() provides.
//
// It exists so the paste/settle/Enter/retry SEQUENCE (not just its timing
// constants) can be driven deterministically against a fake runner, with no
// real tmux. It is a package-level var defaulting to the real exec, mirroring
// the package's existing injectable-function style (CyclerConfig.InjectFn et
// al.). Tests in package keeper swap it out and restore it. Refs: hk-zole.
var tmuxRunFn = runTmuxCombined

// runTmuxCombined is the production tmuxRunFn: it execs `tmux <args...>` with the
// given stdin ("" for none) and returns CombinedOutput().
func runTmuxCombined(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}

// InjectText delivers arbitrary text into the tmux pane at tmuxTarget using
// the bracketed-paste mechanism (tmux load-buffer → paste-buffer → settle →
// send-keys Enter with bounded retry).
//
// tmuxTarget is a tmux pane address in any of tmux's accepted forms:
// "session:window.pane", "session:window", "%pane_id", or just the session name.
//
// The submit Enter is delivered after a short settle and then re-sent a bounded
// number of times. This mirrors the WORKING implementer paste path
// (internal/daemon/pasteinject.go) and fixes the bracketed-paste submit race
// where the injected line (e.g. /session-resume) sits in the pane buffer until
// a manual Enter (hk-89g).
//
// The cycle core uses this as its InjectFn default, so /session-handoff,
// /clear, and /session-resume all inherit the fix.
func InjectText(ctx context.Context, tmuxTarget, text string) error {
	return injectTextClocked(ctx, substrate.SystemClock{}, tmuxTarget, text)
}

// injectTextClocked is InjectText with the settle/retry sleeps read through the
// given ClockPort (SK-008/SK-R3). The cycle core's default InjectFn binds this
// to CyclerConfig.Clock (the T5 injectorClock package var, folded into the port
// wiring at T6), so a substrate.FakeClock drives the 750ms/400ms×2 sequence in
// virtual time; the free InjectText keeps the wall clock for watcher-side warn
// injection. The sequence ORDER and durations are unchanged
// (TestInjectText_SettleConstants guards them — parity risk #8).
func injectTextClocked(ctx context.Context, clock substrate.ClockPort, tmuxTarget, text string) error {
	if clock == nil {
		clock = substrate.SystemClock{}
	}
	if tmuxTarget == "" {
		return fmt.Errorf("keeper: inject: tmuxTarget is empty")
	}

	const buf = "hk-keeper-inject"

	if out, err := tmuxRunFn(ctx, text, "load-buffer", "-b", buf, "-"); err != nil {
		return fmt.Errorf("keeper: tmux load-buffer: %w (stderr: %s)", err, strings.TrimSpace(string(out)))
	}

	if out, err := tmuxRunFn(ctx, "", "paste-buffer", "-b", buf, "-t", tmuxTarget, "-d"); err != nil {
		return fmt.Errorf("keeper: tmux paste-buffer: %w (stderr: %s)", err, strings.TrimSpace(string(out)))
	}

	// Settle so the REPL finishes ingesting the pasted text before the submit
	// Enter; otherwise the first Enter races ahead and is dropped (hk-89g).
	if !clock.Sleep(ctx, submitSettle) {
		return ctx.Err()
	}

	// First submit Enter is load-bearing; a non-nil error here is surfaced.
	if err := sendEnter(ctx, tmuxTarget); err != nil {
		return fmt.Errorf("keeper: tmux send-keys Enter: %w", err)
	}

	// Bounded retries defend against a dropped first keypress. Failures here are
	// non-fatal — the line is already submitted by the first Enter on the happy
	// path, and a redundant Enter is a harmless empty line.
	for i := 0; i < submitRetries; i++ {
		if !clock.Sleep(ctx, submitRetryDelay) {
			break
		}
		_ = sendEnter(ctx, tmuxTarget) //nolint:errcheck // retry; best-effort
	}

	return nil
}

// sendEnter sends a bare Enter keypress to the pane as a real key event (NOT a
// bracketed paste), so the REPL's key-event handler submits the pending line.
func sendEnter(ctx context.Context, tmuxTarget string) error {
	if out, err := tmuxRunFn(ctx, "", "send-keys", "-t", tmuxTarget, "Enter"); err != nil {
		return fmt.Errorf("%w (stderr: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SendEscapeKey sends an Escape keypress to the tmux pane at tmuxTarget.
// The cycle core calls this before injecting /session-handoff to preempt any
// in-progress input on a busy pane (e.g. partial text, a tool-call response
// being typed). Escape is harmless at a clean prompt and clears partial input
// in most REPL implementations. Refs: hk-qoz (forced-clear busy-pane fix).
func SendEscapeKey(ctx context.Context, tmuxTarget string) error {
	if tmuxTarget == "" {
		return fmt.Errorf("keeper: send-escape: tmuxTarget is empty")
	}
	cmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxTarget, "Escape")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keeper: tmux send-keys Escape: %w (stderr: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// InjectWrapUpWarning delivers the wrap-up-warning prompt into the tmux pane
// identified by tmuxTarget using the bracketed-paste mechanism.
//
// The injector is side-effect-only. Errors are returned but the watcher
// treats injection failure as non-fatal (warn event is still emitted).
func InjectWrapUpWarning(ctx context.Context, tmuxTarget string) error {
	return InjectText(ctx, tmuxTarget, wrapUpWarningText)
}

// SetTmuxEnv sets an environment variable in the tmux session that owns
// tmuxTarget. The variable is inherited by any new process started in that
// session after this call — including a Claude Code session resumed after /clear.
//
// Uses `tmux setenv -t <target> <key> <value>` which writes to the session
// environment table. This is intentionally NOT `setenv -g` (global) to avoid
// leaking across unrelated sessions.
func SetTmuxEnv(ctx context.Context, tmuxTarget, key, value string) error {
	if tmuxTarget == "" {
		return fmt.Errorf("keeper: setenv: tmuxTarget is empty")
	}
	cmd := exec.CommandContext(ctx, "tmux", "setenv", "-t", tmuxTarget, key, value)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keeper: tmux setenv %s: %w (stderr: %s)", key, err, strings.TrimSpace(string(out)))
	}
	return nil
}
