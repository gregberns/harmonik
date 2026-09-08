package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper/panehost"
	"github.com/gregberns/harmonik/internal/keeper/panehost/tmuxhost"
	"github.com/gregberns/harmonik/internal/substrate"
)

// DefaultAwaitAckTimeout / DefaultAwaitAckPoll are the operator-confirmed
// defaults for the await-ack primitive (design §0/decision 2). The CLI uses
// these as flag defaults; AwaitAck fills them when the config leaves them zero.
const (
	DefaultAwaitAckTimeout = 15 * time.Second
	DefaultAwaitAckPoll    = 1 * time.Second
)

const captureErrorBudget = 5

// PaneCapturer captures the current contents of a tmux pane. Production wires
// CaptureTmuxPane (a real `tmux capture-pane`); tests substitute a fake that
// returns/withholds the ACK line so AwaitAck is unit-testable WITHOUT tmux —
// the whole point of the CLI-over-prose choice (design §1/§2). The returned
// string is the pane text; an error means the capture itself failed.
type PaneCapturer func(ctx context.Context, tmuxTarget string) (string, error)

// AwaitAckConfig carries everything AwaitAck needs. TmuxTarget is the
// already-resolved pane (the CLI resolves it via ResolveTmuxTarget). Capture
// defaults to PaneHost.Capture when nil (PaneHost itself defaulting to a
// tmux Host — KH-1, the ONE owner of the production tmux default per
// plans/2026-09-07-keeper-herdr-substrate/README.md); Clock defaults to
// substrate.SystemClock (overridable in tests via a substrate.FakeClock for a
// deterministic clock); Timeout/Poll default to the package constants when
// zero.
type AwaitAckConfig struct {
	AgentName  string
	TmuxTarget string
	Nonce      string
	Kind       string // "restart" | "ping" (echoed into the event; not part of the match token)
	Timeout    time.Duration
	Poll       time.Duration
	Capture    PaneCapturer
	Clock      substrate.ClockPort
	// PaneHost supplies the default Capture when Capture is nil. Nil selects
	// a tmux Host (tmuxhost.New()) — the production default.
	PaneHost panehost.PaneHost
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
	clock := cfg.Clock
	if clock == nil {
		clock = substrate.SystemClock{}
	}
	capture := cfg.Capture
	if capture == nil {
		ph := cfg.PaneHost
		if ph == nil {
			ph = tmuxhost.New()
		}
		capture = func(ctx context.Context, target string) (string, error) {
			return ph.Capture(ctx, panehost.Target(target))
		}
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

	if cfg.TmuxTarget == "" {
		log.WarnContext(ctx, "keeper: await-ack: aborted", "reason", "no_tmux_target")
		emitAckTimeout(ctx, emitter, cfg, kind, timeout, "no_tmux_target")
		return fmt.Errorf("%w: no tmux target resolved for agent %q", ErrAckTimeout, cfg.AgentName)
	}

	token := AckMatchToken(cfg.Nonce)
	deadline := clock.Now().Add(timeout)
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

		if !clock.Now().Before(deadline) {
			log.WarnContext(ctx, "keeper: await-ack: timeout; ack not observed", "timeout", timeout)
			emitAckTimeout(ctx, emitter, cfg, kind, timeout, "ack_not_observed")
			return fmt.Errorf("%w: no %q within %s for agent %q — keeper may be dead, wrong pane, or unverifiable sid; investigate",
				ErrAckTimeout, token, timeout, cfg.AgentName)
		}

		wait := poll
		if rem := deadline.Sub(clock.Now()); rem > 0 && rem < wait {
			wait = rem
		}
		if !clock.Sleep(ctx, wait) {
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

// CaptureTmuxPane is a back-compat wrapper over tmuxhost.CaptureTmuxPane
// (KH-1: the tmux capture-pane call moved to panehost/tmuxhost). See
// tmuxhost.CaptureTmuxPane for the full doc.
func CaptureTmuxPane(ctx context.Context, tmuxTarget string) (string, error) {
	return tmuxhost.CaptureTmuxPane(ctx, tmuxTarget)
}
