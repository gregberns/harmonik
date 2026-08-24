package keeper

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/substrate"
)

// RestartNowInjector is the minimal injection surface RestartNow/Ping need.
// Production wires keeper.InjectText. Tests substitute a spy. Each call submits
// the text (via bracketed paste + Enter) into the tmux pane.
type RestartNowInjector func(ctx context.Context, tmuxTarget, text string) error

// RestartNowConfig carries everything RestartNow needs. ProjectDir + AgentName
// identify the session; TmuxTarget is the already-resolved pane; Inject is the
// injection surface (defaults to InjectText when nil); Clock defaults to
// substrate.SystemClock (overridable in tests via a substrate.FakeClock);
// RequestedAt is retained for audit compatibility. Handoff age does not limit
// an explicit restart-now request.
type RestartNowConfig struct {
	ProjectDir  string
	AgentName   string
	TmuxTarget  string
	Inject      RestartNowInjector
	Clock       substrate.ClockPort
	RequestedAt time.Time

	// Emitter, when non-nil, receives a durable session_keeper_restart_now event
	// carrying the nonce on a SUCCESSFUL restart (SK-030, carry-for-audit) so the
	// self-restart is joinable to its originating cycle in events.jsonl. Nil → no
	// event is emitted (Ping never emits). The CLI wires a keeper.FileEmitter.
	Emitter Emitter

	// Force skips the in-flight dispatch gate (Step 3b). The auto cycle has
	// always deferred around in-flight work via Gate 5, but restart-now — the
	// operator/captain-driven path — consulted NO gate at all, so it would
	// /clear straight over a live run. Restarting mid-run cancels the crew's
	// in-flight tool work, which is the first link in the hk-bl2k6 orphan
	// chain. Force exists for the case where the operator KNOWS the marker is
	// stale and wants the restart anyway; it is deliberately explicit.
	Force bool

	// HoldingDispatchFn reports whether the agent has in-flight queue work.
	// Nil → HoldingDispatch (the .dispatching marker). Injectable so tests can
	// drive the gate without a marker file.
	HoldingDispatchFn func(projectDir, agent string) bool

	// LiveKeeperPresentFn proves that the agent lock has a live owner. The
	// explicit command refuses to start a detached driver without that owner.
	// Nil uses LiveKeeperPresent.
	LiveKeeperPresentFn func(projectDir, agent string) bool
}

type RestartDriveConfig struct {
	RestartNowConfig
	PreviousSessionID string
	Grace             time.Duration
	Timeout           time.Duration
	Poll              time.Duration
	ResetPendingInput func(context.Context, string) error
}

func ValidateRestartNow(ctx context.Context, cfg RestartNowConfig, log *slog.Logger) (string, error) {
	if cfg.TmuxTarget == "" {
		return "", fmt.Errorf("keeper: restart-now: no tmux target resolved for agent %q", cfg.AgentName)
	}
	liveKeeper := cfg.LiveKeeperPresentFn
	if liveKeeper == nil {
		liveKeeper = LiveKeeperPresent
	}
	if !liveKeeper(cfg.ProjectDir, cfg.AgentName) {
		return "", fmt.Errorf("keeper: restart-now: no live keeper owns agent %q", cfg.AgentName)
	}
	cf, _, err := ReadCtxFile(cfg.ProjectDir, cfg.AgentName)
	if err != nil {
		return "", fmt.Errorf("keeper: restart-now: read gauge for agent %q: %w", cfg.AgentName, err)
	}
	if !IsPrimarySID(cf.SessionID) {
		return "", fmt.Errorf("keeper: restart-now: session id %q for agent %q is not a trusted primary id (need lowercase UUIDv4)", cf.SessionID, cfg.AgentName)
	}
	if err := checkHandoffExists(ctx, cfg, log); err != nil {
		return "", err
	}
	if err := checkInFlightDispatch(ctx, cfg, log); err != nil {
		return "", err
	}
	return cf.SessionID, nil
}

func EmitRestartNowAccepted(ctx context.Context, emitter Emitter, agent, sessionID, nonce string) error {
	if emitter == nil {
		return nil
	}
	payload, err := json.Marshal(core.SessionKeeperRestartNowPayload{
		AgentName: agent,
		SessionID: sessionID,
		Nonce:     nonce,
	})
	if err != nil {
		return fmt.Errorf("keeper: restart-now: marshal accepted event: %w", err)
	}
	if err := emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperRestartNow, payload); err != nil {
		return fmt.Errorf("keeper: restart-now: emit accepted event: %w", err)
	}
	return nil
}

func DriveRestartAfterReturn(ctx context.Context, cfg RestartDriveConfig) error {
	clock := cfg.Clock
	if clock == nil {
		clock = substrate.SystemClock{}
	}
	inject := cfg.Inject
	if inject == nil {
		inject = InjectText
	}
	if cfg.Grace <= 0 {
		cfg.Grace = 2 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Minute
	}
	if cfg.Poll <= 0 {
		cfg.Poll = 250 * time.Millisecond
	}
	if !clock.Sleep(ctx, cfg.Grace) {
		return ctx.Err()
	}
	sentClear := false
	currentSID, _, readErr := ReadSessionIDFile(cfg.ProjectDir, cfg.AgentName)
	if readErr != nil || !IsPrimarySID(currentSID) || currentSID == cfg.PreviousSessionID {
		if err := inject(ctx, cfg.TmuxTarget, "/clear"); err != nil {
			return fmt.Errorf("keeper: restart driver: inject clear: %w", err)
		}
		sentClear = true
	}
	deadline := clock.Now().Add(cfg.Timeout)
	for clock.Now().Before(deadline) {
		sid, _, err := ReadSessionIDFile(cfg.ProjectDir, cfg.AgentName)
		if err == nil && IsPrimarySID(sid) && sid != cfg.PreviousSessionID {
			if sentClear {
				reset := cfg.ResetPendingInput
				if reset == nil {
					reset = ResetTmuxInput
				}
				if err := reset(ctx, cfg.TmuxTarget); err != nil {
					return fmt.Errorf("keeper: restart driver: reset pending input: %w", err)
				}
			}
			if err := inject(ctx, cfg.TmuxTarget, briefRestartCmd(cfg.AgentName, cfg.ProjectDir)); err != nil {
				return fmt.Errorf("keeper: restart driver: inject brief: %w", err)
			}
			return nil
		}
		if !clock.Sleep(ctx, cfg.Poll) {
			return ctx.Err()
		}
	}
	return fmt.Errorf("keeper: restart driver: session did not change within %s after one clear", cfg.Timeout)
}

func checkHandoffExists(ctx context.Context, cfg RestartNowConfig, log *slog.Logger) error {
	handoffPath := handoffFilePathForAgent(cfg.ProjectDir, cfg.AgentName)
	raw, readErr := os.ReadFile(handoffPath)
	if readErr != nil {
		log.WarnContext(ctx, "keeper: restart-now: aborted", "reason", "handoff_missing", "path", handoffPath, "err", readErr)
		return fmt.Errorf("keeper: restart-now: handoff %q missing for agent %q (write /session-handoff first): %w", handoffPath, cfg.AgentName, readErr)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		log.WarnContext(ctx, "keeper: restart-now: aborted", "reason", "handoff_empty", "path", handoffPath)
		return fmt.Errorf("keeper: restart-now: handoff %q is empty for agent %q (write /session-handoff first)", handoffPath, cfg.AgentName)
	}
	log.InfoContext(ctx, "keeper: restart-now: non-empty handoff verified", "path", handoffPath)
	return nil
}

func checkInFlightDispatch(ctx context.Context, cfg RestartNowConfig, log *slog.Logger) error {
	if cfg.Force {
		log.WarnContext(ctx, "keeper: restart-now: in-flight dispatch gate FORCED past", "agent", cfg.AgentName)
		return nil
	}
	holding := cfg.HoldingDispatchFn
	if holding == nil {
		holding = HoldingDispatch
	}
	if holding(cfg.ProjectDir, cfg.AgentName) {
		log.WarnContext(ctx, "keeper: restart-now: aborted", "reason", "holding_dispatch")
		return fmt.Errorf("keeper: restart-now: agent %q has in-flight queue work (.dispatching marker present); "+
			"wait for it to drain, or pass --force if you know the marker is stale", cfg.AgentName)
	}
	log.InfoContext(ctx, "keeper: restart-now: no in-flight dispatch")
	return nil
}

// Ping injects ONLY the ACK line (no /clear, no resume) so the agent can verify
// the keeper is alive and reachable. It is the minimal liveness handshake.
// Returns an error (logged at WARN) if there is no pane or the inject fails.
func Ping(ctx context.Context, cfg RestartNowConfig, nonce string) error {
	inject := cfg.Inject
	if inject == nil {
		inject = InjectText
	}
	log := slog.With("agent", cfg.AgentName, "op", "ping", "nonce", nonce)
	log.InfoContext(ctx, "keeper: ping: request received")
	if cfg.TmuxTarget == "" {
		log.WarnContext(ctx, "keeper: ping: aborted", "reason", "no_tmux_target")
		return fmt.Errorf("keeper: ping: no tmux target resolved for agent %q", cfg.AgentName)
	}
	if err := inject(ctx, cfg.TmuxTarget, AckLine(nonce, "ping")); err != nil {
		log.WarnContext(ctx, "keeper: ping: aborted", "reason", "ack_inject_failed", "err", err)
		return fmt.Errorf("keeper: ping: inject ack: %w", err)
	}
	log.InfoContext(ctx, "keeper: ping: ack injected; done")
	return nil
}

func handoffFilePathForAgent(projectDir, agent string) string {
	return fmt.Sprintf("%s/HANDOFF-%s.md", projectDir, agent)
}
