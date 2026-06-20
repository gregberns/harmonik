package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// awaitack.go — the AGENT-SIDE half of the restart-now/ping ACK handshake
// (hk-uldg). The keeper->pane half (RestartNow/Ping in restartnow.go) injects
// `[KEEPER ACK <nonce>] received <kind>` into the agent's pane. This file is the
// OBSERVER: it polls the pane scrollback for that EXACT line and either confirms
// the keeper is alive (returns nil / CLI exit 0) or, on timeout, emits a durable
// session_keeper_ack_timeout event and returns a timeout error (CLI exit 3).
//
// Design (authoritative): plans/2026-06-20-keeper-architecture-critique/
// 18-design-agent-side-ack.md.
//
// Decisions baked in (operator-confirmed, do not re-litigate):
//   - The BINARY does NOT send comms. On timeout it emits the durable event and
//     returns a timeout error; the calling skill sends the comms alert (the
//     binary stays identity-free — no hardcoded --from <lane> footgun).
//   - Defaults: timeout 15s, poll 1s.
//   - Match on the EXACT bracket token `[KEEPER ACK <nonce>]`, so a stale ACK
//     from a previous cycle with a DIFFERENT nonce never matches.
//
// Non-collision with hk-vpnp: this file adds ZERO code to cycle.go / watcher.go /
// restartnow.go. It is a brand-new out-of-process observer.

// DefaultAwaitAckTimeout / DefaultAwaitAckPoll are the operator-confirmed
// defaults for the await-ack primitive (design §0/decision 2). The CLI uses
// these as flag defaults; AwaitAck fills them when the config leaves them zero.
const (
	DefaultAwaitAckTimeout = 15 * time.Second
	DefaultAwaitAckPoll    = 1 * time.Second
)

// awaitAckScrollback is how many lines of pane scrollback the real capturer
// requests (`tmux capture-pane -p -S -<N>`). A bounded tail catches an ACK that
// already scrolled off the visible pane between the inject and the first poll,
// without dragging the entire (potentially huge) history each tick.
const awaitAckScrollback = 200

// captureErrorBudget bounds consecutive capture-pane failures. A transient tmux
// error (e.g. pane momentarily busy) is retried each poll; only when the budget
// is exhausted does AwaitAck give up with the capture error rather than the
// generic timeout. This keeps a flaky capturer from masquerading as a clean
// "ack_not_observed" timeout.
const captureErrorBudget = 5

// PaneCapturer captures the current contents of a tmux pane. Production wires
// CaptureTmuxPane (a real `tmux capture-pane`); tests substitute a fake that
// returns/withholds the ACK line so AwaitAck is unit-testable WITHOUT tmux —
// the whole point of the CLI-over-prose choice (design §1/§2). The returned
// string is the pane text; an error means the capture itself failed.
type PaneCapturer func(ctx context.Context, tmuxTarget string) (string, error)

// AwaitAckConfig carries everything AwaitAck needs. TmuxTarget is the
// already-resolved pane (the CLI resolves it via ResolveTmuxTarget). Capture
// defaults to CaptureTmuxPane when nil; Now defaults to time.Now (overridable
// in tests for a deterministic clock); Timeout/Poll default to the package
// constants when zero.
type AwaitAckConfig struct {
	AgentName  string
	TmuxTarget string
	Nonce      string
	Kind       string // "restart" | "ping" (echoed into the event; not part of the match token)
	Timeout    time.Duration
	Poll       time.Duration
	Capture    PaneCapturer
	Now        func() time.Time
}

// ErrAckTimeout is returned by AwaitAck when the timeout elapses without
// observing the ACK line. Callers (the CLI) map it to exit code 3.
var ErrAckTimeout = errors.New("keeper: await-ack: timeout")

// AwaitAck blocks until it observes the exact `[KEEPER ACK <nonce>]` token in
// the agent's pane scrollback (returns nil — keeper proven alive) or the timeout
// elapses (emits session_keeper_ack_timeout and returns an error wrapping
// ErrAckTimeout). A missing tmux target fails fast with a no_tmux_target event
// — there is nothing to watch.
//
// The match is on the FULL bracket token AckMatchToken(nonce) (not just the bare
// nonce), so scrollback from older cycles with a different nonce is inert: there
// is no "first ACK wins" ambiguity (design §2).
func AwaitAck(ctx context.Context, cfg AwaitAckConfig, emitter Emitter) error {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	capture := cfg.Capture
	if capture == nil {
		capture = CaptureTmuxPane
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultAwaitAckTimeout
	}
	poll := cfg.Poll
	if poll <= 0 {
		poll = DefaultAwaitAckPoll
	}
	kind := cfg.Kind
	if kind == "" {
		kind = "ping"
	}

	log := slog.With("agent", cfg.AgentName, "op", "await-ack", "nonce", cfg.Nonce, "kind", kind)

	// A pane is mandatory — without it there is nothing to observe. Emit the
	// durable event with reason no_tmux_target and fail (the CLI maps to exit 3
	// like any await-ack failure, and the caller escalates).
	if cfg.TmuxTarget == "" {
		log.WarnContext(ctx, "keeper: await-ack: aborted", "reason", "no_tmux_target")
		emitAckTimeout(ctx, emitter, cfg, kind, timeout, "no_tmux_target")
		return fmt.Errorf("%w: no tmux target resolved for agent %q", ErrAckTimeout, cfg.AgentName)
	}

	token := AckMatchToken(cfg.Nonce)
	deadline := now().Add(timeout)
	log.InfoContext(ctx, "keeper: await-ack: watching pane for ack", "tmux_target", cfg.TmuxTarget, "token", token, "timeout", timeout, "poll", poll)

	var captureErrs int
	var lastCaptureErr error
	for {
		buf, capErr := capture(ctx, cfg.TmuxTarget)
		if capErr != nil {
			captureErrs++
			lastCaptureErr = capErr
			log.WarnContext(ctx, "keeper: await-ack: capture-pane failed", "err", capErr, "consecutive", captureErrs)
			if captureErrs >= captureErrorBudget {
				emitAckTimeout(ctx, emitter, cfg, kind, timeout, "ack_not_observed")
				return fmt.Errorf("%w: capture-pane failed %d times for agent %q: %w", ErrAckTimeout, captureErrs, cfg.AgentName, lastCaptureErr)
			}
		} else {
			captureErrs = 0
			if strings.Contains(buf, token) {
				log.InfoContext(ctx, "keeper: await-ack: ack observed; keeper alive")
				return nil
			}
		}

		// Timed out? Emit the durable escalation event and return.
		if !now().Before(deadline) {
			log.WarnContext(ctx, "keeper: await-ack: timeout; ack not observed", "timeout", timeout)
			emitAckTimeout(ctx, emitter, cfg, kind, timeout, "ack_not_observed")
			return fmt.Errorf("%w: no %q within %s for agent %q — keeper may be dead, wrong pane, or unverifiable sid; investigate",
				ErrAckTimeout, token, timeout, cfg.AgentName)
		}

		// Wait one poll interval (or until the deadline / ctx cancel, whichever
		// is first) before the next capture.
		wait := poll
		if rem := time.Until(deadline); rem > 0 && rem < wait {
			wait = rem
		}
		if !sleepCtx(ctx, wait) {
			// Context cancelled (e.g. SIGINT). Surface the cancellation; do NOT
			// emit a timeout event — the operator interrupted, the keeper is not
			// implicated.
			return ctx.Err()
		}
	}
}

// AckMatchToken returns the exact substring AwaitAck matches against the pane
// scrollback: `[KEEPER ACK <nonce>]`. This is the leading bracket portion of
// AckLine (injector.go) — matching the full bracket token (not the bare nonce)
// avoids any cross-cycle false positive and is independent of the "received
// <kind>" tail, so an await-ack for one kind still confirms the keeper if the
// nonce matches.
func AckMatchToken(nonce string) string {
	return fmt.Sprintf("[KEEPER ACK %s]", nonce)
}

// emitAckTimeout emits the durable session_keeper_ack_timeout event. Best-effort
// (an emit failure is logged inside FileEmitter); AwaitAck's return value is the
// authoritative signal to the caller.
func emitAckTimeout(ctx context.Context, emitter Emitter, cfg AwaitAckConfig, kind string, timeout time.Duration, reason string) {
	if emitter == nil {
		return
	}
	payload := core.SessionKeeperAckTimeoutPayload{
		AgentName:      cfg.AgentName,
		Nonce:          cfg.Nonce,
		Kind:           kind,
		TimeoutSeconds: timeout.Seconds(),
		TmuxTarget:     cfg.TmuxTarget,
		Reason:         reason,
	}
	raw, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		slog.WarnContext(ctx, "keeper: await-ack: marshal ack-timeout payload", "err", marshalErr)
		return
	}
	_ = emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperAckTimeout, raw) //nolint:errcheck // best-effort; return value is the authoritative signal
}

// CaptureTmuxPane is the production PaneCapturer: it runs
// `tmux capture-pane -p -t <target> -S -<awaitAckScrollback>` to grab the pane
// text plus a bounded scrollback tail. The -S tail catches a fast ACK that
// already scrolled off the visible region between the inject and the first poll.
func CaptureTmuxPane(ctx context.Context, tmuxTarget string) (string, error) {
	if tmuxTarget == "" {
		return "", fmt.Errorf("keeper: capture-pane: tmuxTarget is empty")
	}
	cmd := exec.CommandContext(ctx, "tmux", "capture-pane", "-p", "-t", tmuxTarget, "-S", fmt.Sprintf("-%d", awaitAckScrollback))
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("keeper: tmux capture-pane: %w (stderr: %s)", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("keeper: tmux capture-pane: %w", err)
	}
	return string(out), nil
}
