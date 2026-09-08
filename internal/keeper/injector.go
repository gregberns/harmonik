package keeper

import (
	"context"
	"fmt"
	"strings"

	"github.com/gregberns/harmonik/internal/keeper/panehost/tmuxhost"
)

const wrapUpWarningText = "[KEEPER NOTICE] We use periodic session transitions to keep our work token-efficient. " +
	"As you continue, shape the work toward a state that a fresh session can resume without losing decisions or repeating work."

// AutomationEnvelopePrefix marks text inserted by Harmonik rather than typed by
// the operator. Transcript readers use this stable prefix to keep automation
// messages out of operator-activity gates. The rest of the envelope stays
// visible so an agent can identify the sender without hidden state.
const AutomationEnvelopePrefix = "[[harmonik-message:v1 "

// AutomationMessage wraps injected prose with its machine-readable origin.
// Slash commands are not prose and must not be wrapped because the leading
// command token is load-bearing.
func AutomationMessage(origin, text string) string {
	origin = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return -1
	}, strings.ToLower(origin))
	if origin == "" {
		origin = "unknown"
	}
	return AutomationEnvelopePrefix + "origin=" + origin + "]]\n" + text
}

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
		"[KEEPER NOTICE] We use periodic session transitions to keep our work token-efficient. "+
			"This session is at %dk tokens. We generally aim to continue in a fresh session before %dk. "+
			"As you continue, shape the work toward a state that a fresh session can resume without losing decisions or repeating work. "+
			"When ready, run /session-handoff, then run `%s` so we can continue in the fresh session. "+
			"The notice band is %dk.",
		tokens/1000, act/1000, cmd, warn/1000)
}

// SettleWarnText renders the stronger warning at the second checkpoint band.
// It still leaves the checkpoint with the agent. The hard band owns any
// automatic handoff request.
func SettleWarnText(agent string, tokens, hard int64) string {
	cmd := fmt.Sprintf(restartNowCmdToken, agent)
	return fmt.Sprintf(
		"[KEEPER WARN] This session is at %dk tokens. We want to continue in a fresh session before the %dk hard band. "+
			"As you continue, bring the current unit to a durable checkpoint. "+
			"When ready, run /session-handoff, then run `%s` so we can continue in the fresh session.",
		tokens/1000, hard/1000, cmd)
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
	return InjectText(ctx, tmuxTarget, AutomationMessage("keeper", text))
}

const (
	deferOperatorExchangeToken = "finish the operator exchange"
	// Slot 2 — defer condition B (SK-026.2): finish the in-flight unit of work first.
	deferInflightUnitToken = "finish the in-flight unit"
	// Slot 3 anchor — the good-stopping-point self-test (SK-026.3 / SK-027). The
	// verbatim four-part criterion is goodStoppingPointSelfTest; this short anchor
	// is what the override-completeness check keys on.
	goodStoppingPointToken = "good stopping point"
)

const restartNowNonceCmdToken = "harmonik keeper restart-now --agent %s --nonce %s"

const goodStoppingPointSelfTest = "A good stopping point is one where nothing needed to continue lives only in your context: " +
	"(i) you are between discrete units, not mid-edit / mid-plan / mid-tool-sequence; " +
	"(ii) in-flight work is committed or trivially re-derivable; " +
	"(iii) no unanswered operator question is held; and " +
	"(iv) the next session resumes from the handoff plus durable substrate with no redo and no lost decision."

const handoffWriteGuardHint = "The handoff file already EXISTS: Read it first, then Write it — " +
	"the Write tool refuses a file this session has not Read."

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

// InjectText is a back-compat wrapper over tmuxhost.InjectText (KH-1: the
// tmux inject mechanics moved to panehost/tmuxhost). See tmuxhost.InjectText
// for the full doc.
func InjectText(ctx context.Context, tmuxTarget, text string) error {
	return tmuxhost.InjectText(ctx, tmuxTarget, text)
}

// SendEscapeKey is a back-compat wrapper over tmuxhost.SendEscapeKey.
func SendEscapeKey(ctx context.Context, tmuxTarget string) error {
	return tmuxhost.SendEscapeKey(ctx, tmuxTarget)
}

// InjectWrapUpWarning delivers the wrap-up-warning prompt into the tmux pane
// identified by tmuxTarget using the bracketed-paste mechanism.
//
// The injector is side-effect-only. Errors are returned but the watcher
// treats injection failure as non-fatal (warn event is still emitted).
func InjectWrapUpWarning(ctx context.Context, tmuxTarget string) error {
	return InjectText(ctx, tmuxTarget, AutomationMessage("keeper", wrapUpWarningText))
}
