// Package digest — deterministic suppression resolver (flywheel-motion.md §3).
//
// ResolveSuppressionState is LLM-free and reads only durable file surfaces
// already owned by the digest builder. It returns a SuppressionState that
// the cognition loop reads each turn to decide whether to dispatch work
// (EXECUTE-BACKLOG, the default) or hold (suppressed).
//
// Suppression sources (all decaying — spec §3.2):
//
//  1. operator_attached   — most recent session_keeper_operator_attached event in
//     events.jsonl; decays after min(SuppressionTTL, AttachedInactiveTimeout).
//
//  2. operator_dialogue   — most recent agent_message event from "operator" in
//     events.jsonl; decays after SuppressionTTL.
//
//  3. phase_flag          — sentinel.phase_flag in .harmonik/config.yaml;
//     active when non-empty and phase_flag_expiry is in the future.
//     A phase_flag without expiry is invalid config (fail-open: treated inactive).
//
// Issue-clearing is NOT a mode (spec §3.3): progressing issue-clears emit
// bead_closed/HEAD-advances that the movement governor credits, keeping it
// dormant without any suppression. A stalled clear correctly trips.
//
// Bead: hk-1f8f. Epic: hk-0oca (codename:flywheel).
package digest

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// ResolveSuppressionState evaluates all three suppression sources and returns
// the combined SuppressionState. The eventsPath argument is the full path to
// events.jsonl; cfg is the sentinel config read from .harmonik/config.yaml.
//
// Errors from individual sources are recorded in SuppressionState.ConfigError
// (for config errors) or treated as "not seen" (for event-scan failures, which
// are non-fatal per DC-007 discipline).
func ResolveSuppressionState(eventsPath string, now time.Time, cfg SentinelConfig) SuppressionState {
	attachedLast, dialogueLast := scanSuppressionEvents(eventsPath)

	var sources []SuppressionSourceState
	var configErr string

	attachedSrc := resolveAttachedSource(attachedLast, now, cfg)
	sources = append(sources, attachedSrc)

	dialogueSrc := resolveDialogueSource(dialogueLast, now, cfg)
	sources = append(sources, dialogueSrc)

	phaseSrc, phaseCfgErr := resolvePhaseFlagSource(now, cfg)
	sources = append(sources, phaseSrc)
	if phaseCfgErr != "" {
		configErr = phaseCfgErr
	}

	suppressed := attachedSrc.Active || dialogueSrc.Active || phaseSrc.Active
	return SuppressionState{
		Suppressed:  suppressed,
		Sources:     sources,
		ConfigError: configErr,
	}
}

func resolveAttachedSource(lastSeen, now time.Time, cfg SentinelConfig) SuppressionSourceState {
	src := SuppressionSourceState{Name: "operator_attached"}
	if lastSeen.IsZero() {
		src.Reason = "no session_keeper_operator_attached events found"
		return src
	}
	effectiveTTL := min2(cfg.suppressionTTL(), cfg.attachedInactiveTimeout())
	expiresAt := lastSeen.Add(effectiveTTL)
	src.LastSeen = lastSeen
	src.ExpiresAt = expiresAt
	if now.Before(expiresAt) {
		src.Active = true
		src.Reason = fmt.Sprintf("session_keeper_operator_attached within %s (expires %s)",
			effectiveTTL, expiresAt.UTC().Format(time.RFC3339))
	} else {
		src.Reason = fmt.Sprintf("session_keeper_operator_attached expired %s ago (ttl=%s)",
			now.Sub(expiresAt).Round(time.Second), effectiveTTL)
	}
	return src
}

func resolveDialogueSource(lastSeen, now time.Time, cfg SentinelConfig) SuppressionSourceState {
	src := SuppressionSourceState{Name: "operator_dialogue"}
	if lastSeen.IsZero() {
		src.Reason = "no agent_message events from operator found"
		return src
	}
	ttl := cfg.suppressionTTL()
	expiresAt := lastSeen.Add(ttl)
	src.LastSeen = lastSeen
	src.ExpiresAt = expiresAt
	if now.Before(expiresAt) {
		src.Active = true
		src.Reason = fmt.Sprintf("operator dialogue within suppression_ttl=%s (expires %s)",
			ttl, expiresAt.UTC().Format(time.RFC3339))
	} else {
		src.Reason = fmt.Sprintf("operator dialogue expired %s ago (ttl=%s)",
			now.Sub(expiresAt).Round(time.Second), ttl)
	}
	return src
}

func resolvePhaseFlagSource(now time.Time, cfg SentinelConfig) (source SuppressionSourceState, configError string) {
	src := SuppressionSourceState{Name: "phase_flag"}
	if cfg.PhaseFlag == "" {
		src.Reason = "not set"
		return src, ""
	}
	if cfg.PhaseFlagExpiry.IsZero() {
		err := fmt.Sprintf("phase_flag %q set without phase_flag_expiry (invalid config; treated inactive)", cfg.PhaseFlag)
		src.Reason = err
		return src, err
	}
	src.ExpiresAt = cfg.PhaseFlagExpiry
	if now.Before(cfg.PhaseFlagExpiry) {
		src.Active = true
		src.Reason = fmt.Sprintf("phase_flag=%q active until %s",
			cfg.PhaseFlag, cfg.PhaseFlagExpiry.UTC().Format(time.RFC3339))
	} else {
		src.Reason = fmt.Sprintf("phase_flag=%q expired at %s",
			cfg.PhaseFlag, cfg.PhaseFlagExpiry.UTC().Format(time.RFC3339))
	}
	return src, ""
}

type agentMessagePayloadFrom struct {
	From string `json:"from"`
}

const eventTypeAgentMessage = "agent_message"

func scanSuppressionEvents(eventsPath string) (attachedLast, dialogueLast time.Time) {
	for ev := range eventbus.ScanAfter(eventsPath, ZeroEventID) {
		switch ev.Type {
		case core.EventTypeSessionKeeperOperatorAttached:
			if ev.TimestampWall.After(attachedLast) {
				attachedLast = ev.TimestampWall
			}
		case eventTypeAgentMessage:
			var p agentMessagePayloadFrom
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.From == "operator" && ev.TimestampWall.After(dialogueLast) {
				dialogueLast = ev.TimestampWall
			}
		default:
		}
	}
	return attachedLast, dialogueLast
}

func min2(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
