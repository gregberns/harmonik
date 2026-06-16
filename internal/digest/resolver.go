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
	// Scan events.jsonl once to collect the two event-driven sources.
	attachedLast, dialogueLast := scanSuppressionEvents(eventsPath)

	var sources []SuppressionSourceState
	var configErr string

	// --- source 1: operator_attached ---
	// Suppress when the most recent session_keeper_operator_attached event falls
	// within min(SuppressionTTL, AttachedInactiveTimeout) of now.
	// The inner AttachedInactiveTimeout guards the operatorAttached-pins-forever bug:
	// if the keeper goes quiet (no new events), suppression expires quickly.
	attachedSrc := resolveAttachedSource(attachedLast, now, cfg)
	sources = append(sources, attachedSrc)

	// --- source 2: operator_dialogue ---
	// Suppress when the most recent agent_message from "operator" falls within
	// SuppressionTTL of now.
	dialogueSrc := resolveDialogueSource(dialogueLast, now, cfg)
	sources = append(sources, dialogueSrc)

	// --- source 3: phase_flag ---
	// Suppress when sentinel.phase_flag is set and phase_flag_expiry is in the future.
	// Invalid config (missing expiry) fails-open: treated as inactive.
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

// resolveAttachedSource evaluates the operator_attached suppression source.
func resolveAttachedSource(lastSeen time.Time, now time.Time, cfg SentinelConfig) SuppressionSourceState {
	src := SuppressionSourceState{Name: "operator_attached"}
	if lastSeen.IsZero() {
		src.Reason = "no session_keeper_operator_attached events found"
		return src
	}
	// Effective TTL is the SHORTER of SuppressionTTL and AttachedInactiveTimeout.
	// The inner timeout (AttachedInactiveTimeout) is the primary guard against
	// the pins-forever bug; the outer SuppressionTTL is the maximum.
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

// resolveDialogueSource evaluates the operator_dialogue suppression source.
func resolveDialogueSource(lastSeen time.Time, now time.Time, cfg SentinelConfig) SuppressionSourceState {
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

// resolvePhaseFlagSource evaluates the phase_flag suppression source.
// Returns (source, configError). configError is non-empty when the config is
// invalid; in that case the source is treated as inactive (fail-open).
func resolvePhaseFlagSource(now time.Time, cfg SentinelConfig) (SuppressionSourceState, string) {
	src := SuppressionSourceState{Name: "phase_flag"}
	if cfg.PhaseFlag == "" {
		src.Reason = "not set"
		return src, ""
	}
	if cfg.PhaseFlagExpiry.IsZero() {
		// This should have been caught by LoadSentinelConfig, but guard defensively.
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

// agentMessagePayloadFrom is a minimal unmarshal target to extract the From field.
type agentMessagePayloadFrom struct {
	From string `json:"from"`
}

// eventTypeAgentMessage is the string identifier for the agent_message event.
// No typed constant exists yet; the comms subsystem also compares by string.
const eventTypeAgentMessage = "agent_message"

// scanSuppressionEvents scans events.jsonl once from the beginning and returns:
//   - attachedLast: wall-clock time of the most recent session_keeper_operator_attached event
//   - dialogueLast: wall-clock time of the most recent agent_message from "operator"
//
// A missing or unreadable events.jsonl returns zero times (no suppression).
func scanSuppressionEvents(eventsPath string) (attachedLast time.Time, dialogueLast time.Time) {
	for ev := range eventbus.ScanAfter(eventsPath, ZeroEventID) {
		switch {
		case core.EventType(ev.Type) == core.EventTypeSessionKeeperOperatorAttached:
			if ev.TimestampWall.After(attachedLast) {
				attachedLast = ev.TimestampWall
			}
		case ev.Type == eventTypeAgentMessage:
			var p agentMessagePayloadFrom
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.From == "operator" && ev.TimestampWall.After(dialogueLast) {
				dialogueLast = ev.TimestampWall
			}
		}
	}
	return attachedLast, dialogueLast
}

// min2 returns the smaller of two durations.
func min2(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
