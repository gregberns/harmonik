// Canned scenarios for harmonik-twin-claude (hk-w5vra.2).
//
// Each scenario is a fixed ScriptFile embedded in the binary, covering the
// §10 Conformance scenario set from specs/claude-hook-bridge.md CHB-021.
//
// # Scenario names
//
//   - single-happy-path       — full happy-path lifecycle (handler_capabilities →
//     session_log_location → skills_provisioned → agent_ready → optional
//     heartbeats → outcome_emitted{WORK_COMPLETE} → agent_completed).
//   - review-loop-3iter       — 3-iteration review-loop: implementer-initial,
//     reviewer-1, implementer-resume, reviewer-2, implementer-resume,
//     reviewer-3 with claude_session_id stability across implementer-resume
//     launches and freshness across reviewer launches (CHB-008/009).
//   - rate-limit              — agent_rate_limited (non-terminal) emitted
//     mid-run via StopFailure mapping; then agent_rate_limit_cleared and
//     resume (CHB-013 rate_limit mapping).
//   - dial-failed             — twin emits agent_failed immediately, simulating
//     the handler-side terminal-event emission when the relay can't dial (CHB
//     §8 bridge_dial_failed sub-reason).
//   - daemon-not-ready-retry  — twin emits a brief delay then the full happy
//     path, simulating the relay-side retry path for daemon_not_ready
//     (CHB-016).
//   - partial-pre-exec        — emits handler_capabilities + agent_started only,
//     omitting agent_ready.  Watcher times out waiting for agent_ready; daemon
//     HC-024 closes the bead (SC-5 / hk-35mpj).
//
// # Mechanism tagging
//
// All scenarios are mechanism-tagged (HC-037): no cognition, deterministic
// per-scenario output per CHB-021.
//
// Cite: specs/claude-hook-bridge.md §4.8.CHB-021, §10;
// specs/handler-contract.md §4.8.HC-036, §4.8.HC-037.
package main

import (
	"fmt"
	"time"
)

// cannedScenario returns the ScriptFile for the named scenario per CHB-021 §10.
//
// Returns an error if name is not a recognised scenario name.
func cannedScenario(name string) (*ScriptFile, error) {
	switch name {
	case "single-happy-path":
		return scenarioSingleHappyPath(), nil
	case "review-loop-3iter":
		return scenarioReviewLoop3Iter(), nil
	case "rate-limit":
		return scenarioRateLimit(), nil
	case "dial-failed":
		return scenarioDialFailed(), nil
	case "daemon-not-ready-retry":
		return scenarioDaemonNotReadyRetry(), nil
	case "commit-on-cue-startup-delay":
		return scenarioCommitOnCueStartupDelay(), nil
	case "budget-exhausted":
		return scenarioBudgetExhausted(), nil
	case "handler-fatal":
		return scenarioHandlerFatal(), nil
	case "silent-hang":
		return scenarioSilentHang(), nil
	case "partial-pre-exec":
		return scenarioPartialPreExec(), nil
	default:
		return nil, fmt.Errorf("unknown scenario %q: must be one of single-happy-path, review-loop-3iter, rate-limit, dial-failed, daemon-not-ready-retry, commit-on-cue-startup-delay, budget-exhausted, handler-fatal, silent-hang, partial-pre-exec", name)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: single-happy-path (CHB-021 §10 class 1)
// ─────────────────────────────────────────────────────────────────────────────

// scenarioSingleHappyPath returns the full happy-path claude-code session
// lifecycle per CHB-018 pre-exec ordering + CHB-020 terminal-event emission:
//
//  1. handler_capabilities (CHB-018 step 1, HC-009)
//  2. session_log_location  (CHB-018 step 2, HC-010)
//  3. skills_provisioned    (CHB-018 step 3, HC-049)
//  4. agent_ready           (CHB-018 step 4, HC-039)
//  5. agent_started         (§6.4)
//  6. agent_heartbeat × 2   (CHB-019 timer-driven heartbeat, HC-026a)
//  7. agent_output_chunk    (HC-007)
//  8. outcome_emitted       (CHB-013 Stop→WORK_COMPLETE, HC-008)
//  9. agent_completed       (CHB-020 Wait-return, HC-024)
//
// Cite: specs/claude-hook-bridge.md §4.8.CHB-021, §10 "single workflow-mode run".
func scenarioSingleHappyPath() *ScriptFile {
	now := time.Now().UTC()
	const (
		runID     = "run-chb021-shp-001"
		sessID    = "sess-chb021-shp-001"
		nodeID    = "node-chb021-shp-001"
		agentType = "claude-twin-claude"
	)
	logPath := "/tmp/harmonik/sessions/" + sessID + ".jsonl"

	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			// 1. handler_capabilities — first message on stream (HC-009).
			{
				Type: "handler_capabilities",
				Payload: map[string]any{
					"run_id":                      runID,
					"session_id":                  sessID,
					"protocol_versions_supported": []any{1},
					"claude_session_id":           "claude-sess-shp-001",
				},
			},
			// 2. session_log_location (HC-010).
			{
				Type: "session_log_location",
				Payload: map[string]any{
					"run_id":     runID,
					"session_id": sessID,
					"node_id":    nodeID,
					"agent_type": agentType,
					"log_path":   logPath,
					"log_format": "ndjson",
				},
			},
			// 3. skills_provisioned (HC-049).
			{
				Type: "skills_provisioned",
				Payload: map[string]any{
					"run_id":     runID,
					"session_id": sessID,
					"skills":     []any{},
				},
			},
			// 4. agent_ready (HC-039).
			{
				Type: "agent_ready",
				Payload: map[string]any{
					"run_id":       runID,
					"session_id":   sessID,
					"capabilities": []any{"scripted", "heartbeat"},
				},
			},
			// 5. agent_started (§6.4).
			{
				Type: "agent_started",
				Payload: map[string]any{
					"run_id":     runID,
					"session_id": sessID,
					"node_id":    nodeID,
					"agent_type": agentType,
					"started_at": now.Format(time.RFC3339Nano),
				},
			},
			// 6a. agent_heartbeat × 1 (CHB-019 timer-driven, HC-026a).
			{
				Type: "agent_heartbeat",
				Payload: map[string]any{
					"session_id": sessID,
					"phase":      "reasoning",
				},
				RelativeTimestampMs: 10,
			},
			// 6b. agent_heartbeat × 2.
			{
				Type: "agent_heartbeat",
				Payload: map[string]any{
					"session_id": sessID,
					"phase":      "tool_call",
				},
				RelativeTimestampMs: 10,
			},
			// 7. agent_output_chunk (HC-007, event-model §8.3.3).
			{
				Type: "agent_output_chunk",
				Payload: map[string]any{
					"run_id":        runID,
					"session_id":    sessID,
					"chunk_index":   0,
					"bytes_emitted": 512,
				},
				RelativeTimestampMs: 5,
			},
			// 8. outcome_emitted — Stop→WORK_COMPLETE (CHB-013, HC-008).
			{
				Type: "outcome_emitted",
				Payload: map[string]any{
					"run_id":         runID,
					"session_id":     sessID,
					"node_id":        nodeID,
					"outcome_status": "WORK_COMPLETE",
				},
			},
			// 9. agent_completed — Wait-return (CHB-020, HC-024).
			{
				Type: "agent_completed",
				Payload: map[string]any{
					"run_id":      runID,
					"session_id":  sessID,
					"ended_at":    now.Add(50 * time.Millisecond).Format(time.RFC3339Nano),
					"exit_code":   0,
					"outcome_ref": runID + "/outcome",
				},
			},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: review-loop-3iter (CHB-021 §10 class 2)
// ─────────────────────────────────────────────────────────────────────────────

// scenarioReviewLoop3Iter returns the 3-iteration review-loop sequence per
// CHB-021 §10 "3-iteration review-loop run". The full sequence models:
//
//   - iteration 1: implementer-initial + reviewer-1
//   - iteration 2: implementer-resume (stable claude_session_id) + reviewer-2 (fresh)
//   - iteration 3: implementer-resume (stable claude_session_id) + reviewer-3 (fresh)
//
// claude_session_id is stable across implementer-resume launches (CHB-008) and
// fresh across reviewer launches (CHB-009).
//
// Cite: specs/claude-hook-bridge.md §4.3.CHB-008, §4.3.CHB-009, §10.
func scenarioReviewLoop3Iter() *ScriptFile {
	now := time.Now().UTC()
	const (
		runID     = "run-chb021-rl3-001"
		nodeID    = "node-chb021-rl3-001"
		agentType = "claude-twin-claude"

		// Implementer claude_session_id is stable across resume phases (CHB-008).
		implClaudeSessionID = "claude-impl-rl3-001"

		// Reviewer claude_session_ids are minted fresh per iteration (CHB-009).
		rev1ClaudeSessionID = "claude-rev-rl3-001"
		rev2ClaudeSessionID = "claude-rev-rl3-002"
		rev3ClaudeSessionID = "claude-rev-rl3-003"
	)

	msgs := []ScriptMessage{}

	// Helper: emit a minimal happy-path session block for one phase.
	addPhase := func(sessID, claudeSessID, phase, outcomeStatus string, iteration int) {
		_ = phase     // informational only; not emitted to stream
		_ = iteration // informational only; not emitted to stream
		msgs = append(msgs,
			ScriptMessage{
				Type: "handler_capabilities",
				Payload: map[string]any{
					"run_id":                      runID,
					"session_id":                  sessID,
					"protocol_versions_supported": []any{1},
					"claude_session_id":           claudeSessID,
				},
			},
			ScriptMessage{
				Type: "agent_ready",
				Payload: map[string]any{
					"run_id":       runID,
					"session_id":   sessID,
					"capabilities": []any{"scripted"},
				},
			},
			ScriptMessage{
				Type: "agent_started",
				Payload: map[string]any{
					"run_id":     runID,
					"session_id": sessID,
					"node_id":    nodeID,
					"agent_type": agentType,
					"started_at": now.Format(time.RFC3339Nano),
				},
			},
			ScriptMessage{
				Type: "agent_heartbeat",
				Payload: map[string]any{
					"session_id": sessID,
					"phase":      "reasoning",
				},
				RelativeTimestampMs: 5,
			},
			ScriptMessage{
				Type: "outcome_emitted",
				Payload: map[string]any{
					"run_id":         runID,
					"session_id":     sessID,
					"node_id":        nodeID,
					"outcome_status": outcomeStatus,
				},
			},
			ScriptMessage{
				Type: "agent_completed",
				Payload: map[string]any{
					"run_id":      runID,
					"session_id":  sessID,
					"ended_at":    now.Add(20 * time.Millisecond).Format(time.RFC3339Nano),
					"exit_code":   0,
					"outcome_ref": runID + "/" + sessID + "/outcome",
				},
			},
		)
	}

	// Iteration 1: implementer-initial + reviewer-1.
	addPhase("sess-impl-rl3-initial", implClaudeSessionID, "implementer-initial", "WORK_COMPLETE", 1)
	addPhase("sess-rev-rl3-001", rev1ClaudeSessionID, "reviewer", "REVIEWER_VERDICT", 1)

	// Iteration 2: implementer-resume (same claude_session_id) + reviewer-2 (fresh).
	addPhase("sess-impl-rl3-resume2", implClaudeSessionID, "implementer-resume", "WORK_COMPLETE", 2)
	addPhase("sess-rev-rl3-002", rev2ClaudeSessionID, "reviewer", "REVIEWER_VERDICT", 2)

	// Iteration 3: implementer-resume (same claude_session_id) + reviewer-3 (fresh).
	addPhase("sess-impl-rl3-resume3", implClaudeSessionID, "implementer-resume", "WORK_COMPLETE", 3)
	addPhase("sess-rev-rl3-003", rev3ClaudeSessionID, "reviewer", "REVIEWER_VERDICT", 3)

	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages:      msgs,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: rate-limit (CHB-021 §10 class 3)
// ─────────────────────────────────────────────────────────────────────────────

// scenarioRateLimit returns a script that emits the rate-limit lifecycle per
// CHB-013: StopFailure{error_type=rate_limit} → agent_rate_limited (non-terminal)
// → heartbeats(waiting_input) → agent_rate_limit_cleared → resume → outcome.
//
// Cite: specs/claude-hook-bridge.md §4.5.CHB-013 (rate_limit mapping),
// specs/handler-contract.md §4.6.HC-025.
func scenarioRateLimit() *ScriptFile {
	now := time.Now().UTC()
	const (
		runID      = "run-chb021-rl-001"
		sessID     = "sess-chb021-rl-001"
		nodeID     = "node-chb021-rl-001"
		retryAfter = 60
	)

	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			// Preamble: handler_capabilities + agent_ready + agent_started.
			{
				Type: "handler_capabilities",
				Payload: map[string]any{
					"run_id":                      runID,
					"session_id":                  sessID,
					"protocol_versions_supported": []any{1},
					"claude_session_id":           "claude-sess-rl-001",
				},
			},
			{
				Type: "agent_ready",
				Payload: map[string]any{
					"run_id":       runID,
					"session_id":   sessID,
					"capabilities": []any{"scripted", "heartbeat"},
				},
			},
			{
				Type: "agent_started",
				Payload: map[string]any{
					"run_id":     runID,
					"session_id": sessID,
					"node_id":    nodeID,
					"agent_type": "claude-twin-claude",
					"started_at": now.Format(time.RFC3339Nano),
				},
			},
			// Initial reasoning heartbeat before rate-limit.
			{
				Type: "agent_heartbeat",
				Payload: map[string]any{
					"session_id": sessID,
					"phase":      "reasoning",
				},
				RelativeTimestampMs: 5,
			},
			// Rate-limit onset (CHB-013: StopFailure{rate_limit} → agent_rate_limited).
			// Non-terminal per event-model §8.3.
			{
				Type: "agent_rate_limited",
				Payload: map[string]any{
					"run_id":              runID,
					"session_id":          sessID,
					"rate_limit_source":   "anthropic",
					"retry_after_seconds": retryAfter,
					"changed_at":          now.Add(100 * time.Millisecond).Format(time.RFC3339Nano),
				},
				RelativeTimestampMs: 5,
			},
			// Heartbeats during rate-limited window (HC-026a: waiting_input phase).
			{
				Type: "agent_heartbeat",
				Payload: map[string]any{
					"session_id": sessID,
					"phase":      "waiting_input",
				},
				RelativeTimestampMs: 10,
			},
			{
				Type: "agent_heartbeat",
				Payload: map[string]any{
					"session_id": sessID,
					"phase":      "waiting_input",
				},
				RelativeTimestampMs: 10,
			},
			// Rate-limit cleared.
			{
				Type: "agent_rate_limit_cleared",
				Payload: map[string]any{
					"run_id":     runID,
					"session_id": sessID,
					"changed_at": now.Add(200 * time.Millisecond).Format(time.RFC3339Nano),
				},
				RelativeTimestampMs: 5,
			},
			// Resume: reasoning heartbeat then outcome.
			{
				Type: "agent_heartbeat",
				Payload: map[string]any{
					"session_id": sessID,
					"phase":      "reasoning",
				},
				RelativeTimestampMs: 5,
			},
			// outcome_emitted after rate-limit window.
			{
				Type: "outcome_emitted",
				Payload: map[string]any{
					"run_id":         runID,
					"session_id":     sessID,
					"node_id":        nodeID,
					"outcome_status": "WORK_COMPLETE",
				},
			},
			// agent_completed.
			{
				Type: "agent_completed",
				Payload: map[string]any{
					"run_id":      runID,
					"session_id":  sessID,
					"ended_at":    now.Add(250 * time.Millisecond).Format(time.RFC3339Nano),
					"exit_code":   0,
					"outcome_ref": runID + "/outcome",
				},
			},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: dial-failed (CHB-021 §10 class 4)
// ─────────────────────────────────────────────────────────────────────────────

// scenarioDialFailed returns a script that emits agent_failed immediately,
// simulating handler-side terminal-event emission when the relay can't dial
// the daemon socket (CHB §8 bridge_dial_failed sub-reason).
//
// In a real bridge failure the handler-process emits agent_failed when it
// detects the relay exited non-zero. This twin synthesizes that emission
// directly.
//
// Cite: specs/claude-hook-bridge.md §8 (bridge_dial_failed), §10.
func scenarioDialFailed() *ScriptFile {
	now := time.Now().UTC()
	const (
		runID  = "run-chb021-df-001"
		sessID = "sess-chb021-df-001"
		nodeID = "node-chb021-df-001"
	)
	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			// Preamble through agent_ready (handler still connected).
			{
				Type: "handler_capabilities",
				Payload: map[string]any{
					"run_id":                      runID,
					"session_id":                  sessID,
					"protocol_versions_supported": []any{1},
					"claude_session_id":           "claude-sess-df-001",
				},
			},
			{
				Type: "agent_ready",
				Payload: map[string]any{
					"run_id":       runID,
					"session_id":   sessID,
					"capabilities": []any{"scripted"},
				},
			},
			{
				Type: "agent_started",
				Payload: map[string]any{
					"run_id":     runID,
					"session_id": sessID,
					"node_id":    nodeID,
					"agent_type": "claude-twin-claude",
					"started_at": now.Format(time.RFC3339Nano),
				},
			},
			// Handler detects relay exited 1 (bridge_dial_failed) and emits
			// agent_failed as the terminal event (CHB-020 + CHB §8).
			{
				Type: "agent_failed",
				Payload: map[string]any{
					"run_id":         runID,
					"session_id":     sessID,
					"ended_at":       now.Add(10 * time.Millisecond).Format(time.RFC3339Nano),
					"error_category": "transient",
					"reason":         "bridge_dial_failed",
				},
			},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: daemon-not-ready-retry (CHB-021 §10 class 5)
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: commit-on-cue-startup-delay (hk-8ys88, audit items 3+6)
// ─────────────────────────────────────────────────────────────────────────────

// scenarioCommitOnCueStartupDelay exercises both twin extensions from hk-8ys88:
//   - startup_delay_ms: twin sleeps 100ms before emitting handler_capabilities,
//     modelling the splash-dismiss window for timeout-sensitivity scenarios
//     (docs/twin-parity-audit-2026-05-14.md §4 item 6).
//   - commit_on_cue: step writes a sentinel file + git commit in the worktree
//     so pasteInjectQuitOnCommit can detect the HEAD change (§4 item 3).
//
// Expected event ordering (with --worktree-path supplied):
//
//  1. handler_capabilities   (emitted AFTER startup_delay_ms sleep)
//  2. agent_ready
//  3. agent_output_chunk     (represents work output)
//  4. commit_on_cue step     → emits twin_committed
//  5. outcome_emitted
//  6. agent_completed
//
// Cite: docs/twin-parity-audit-2026-05-14.md §4 items 3+6; hk-8ys88.
func scenarioCommitOnCueStartupDelay() *ScriptFile {
	now := time.Now().UTC()
	const (
		runID     = "run-hk8ys88-coc-001"
		sessID    = "sess-hk8ys88-coc-001"
		nodeID    = "node-hk8ys88-coc-001"
		agentType = "claude-twin-claude"
	)

	return &ScriptFile{
		HeartbeatMode:  heartbeatModeScripted,
		StartupDelayMs: 100, // models 750ms splash-dismiss window at small scale
		Messages: []ScriptMessage{
			// 1. handler_capabilities (emitted after startup_delay_ms).
			{
				Type: "handler_capabilities",
				Payload: map[string]any{
					"run_id":                      runID,
					"session_id":                  sessID,
					"protocol_versions_supported": []any{1},
					"claude_session_id":           "claude-sess-hk8ys88-001",
				},
			},
			// 2. agent_ready.
			{
				Type: "agent_ready",
				Payload: map[string]any{
					"run_id":       runID,
					"session_id":   sessID,
					"capabilities": []any{"scripted", "commit_on_cue"},
				},
			},
			// 3. agent_output_chunk (represents work output before commit).
			{
				Type: "agent_output_chunk",
				Payload: map[string]any{
					"run_id":        runID,
					"session_id":    sessID,
					"chunk_index":   0,
					"bytes_emitted": 256,
				},
			},
			// 4. commit_on_cue: writes sentinel + git commit → emits twin_committed.
			//    Requires --worktree-path to be set; without it twin emits twin_error.
			{
				Type: commitOnCueStep,
			},
			// 5. outcome_emitted.
			{
				Type: "outcome_emitted",
				Payload: map[string]any{
					"run_id":         runID,
					"session_id":     sessID,
					"node_id":        nodeID,
					"outcome_status": "WORK_COMPLETE",
				},
			},
			// 6. agent_completed.
			{
				Type: "agent_completed",
				Payload: map[string]any{
					"run_id":      runID,
					"session_id":  sessID,
					"ended_at":    now.Add(200 * time.Millisecond).Format(time.RFC3339Nano),
					"exit_code":   0,
					"outcome_ref": runID + "/outcome",
				},
			},
		},
	}
}

// scenarioDaemonNotReadyRetry returns a script that models the relay-side retry
// path for daemon_not_ready (CHB-016): the relay pauses (via heartbeats in the
// twin's scripted mode) and then proceeds to the full happy path once the
// daemon becomes ready.
//
// In a real bridge the relay retries the socket dial; the twin simulates this
// by emitting the preamble messages only after a scripted delay.
//
// Cite: specs/claude-hook-bridge.md §4.6.CHB-016, §10.
func scenarioDaemonNotReadyRetry() *ScriptFile {
	now := time.Now().UTC()
	const (
		runID     = "run-chb021-dnr-001"
		sessID    = "sess-chb021-dnr-001"
		nodeID    = "node-chb021-dnr-001"
		agentType = "claude-twin-claude"
	)
	logPath := "/tmp/harmonik/sessions/" + sessID + ".jsonl"

	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			// Simulated retry window: the twin waits 50 ms before emitting
			// handler_capabilities (the relay would have been retrying the daemon
			// socket dial during this window per CHB-016).
			{
				Type: "handler_capabilities",
				Payload: map[string]any{
					"run_id":                      runID,
					"session_id":                  sessID,
					"protocol_versions_supported": []any{1},
					"claude_session_id":           "claude-sess-dnr-001",
				},
				RelativeTimestampMs: 50, // simulated retry window
			},
			{
				Type: "session_log_location",
				Payload: map[string]any{
					"run_id":     runID,
					"session_id": sessID,
					"node_id":    nodeID,
					"agent_type": agentType,
					"log_path":   logPath,
					"log_format": "ndjson",
				},
			},
			{
				Type: "skills_provisioned",
				Payload: map[string]any{
					"run_id":     runID,
					"session_id": sessID,
					"skills":     []any{},
				},
			},
			{
				Type: "agent_ready",
				Payload: map[string]any{
					"run_id":       runID,
					"session_id":   sessID,
					"capabilities": []any{"scripted"},
				},
			},
			{
				Type: "agent_started",
				Payload: map[string]any{
					"run_id":     runID,
					"session_id": sessID,
					"node_id":    nodeID,
					"agent_type": agentType,
					"started_at": now.Format(time.RFC3339Nano),
				},
			},
			{
				Type: "agent_heartbeat",
				Payload: map[string]any{
					"session_id": sessID,
					"phase":      "reasoning",
				},
				RelativeTimestampMs: 5,
			},
			{
				Type: "outcome_emitted",
				Payload: map[string]any{
					"run_id":         runID,
					"session_id":     sessID,
					"node_id":        nodeID,
					"outcome_status": "WORK_COMPLETE",
				},
			},
			{
				Type: "agent_completed",
				Payload: map[string]any{
					"run_id":      runID,
					"session_id":  sessID,
					"ended_at":    now.Add(100 * time.Millisecond).Format(time.RFC3339Nano),
					"exit_code":   0,
					"outcome_ref": runID + "/outcome",
				},
			},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: budget-exhausted (hk-6f1uj)
// ─────────────────────────────────────────────────────────────────────────────

// scenarioBudgetExhausted returns a script that emits a budget_exhausted event
// after agent_ready, modelling the handler-account budget exhaustion case that
// trips HandlerPauseController.Pause via the policy goroutine (HP-012).
//
// Expected event ordering:
//
//  1. handler_capabilities
//  2. agent_ready
//  3. budget_exhausted  — trips handler-pause policy for AgentTypeClaudeCode
//  4. agent_failed      — terminal event; run closes with failure
//
// The budget_exhausted event uses budget_ref="handler-account" to represent the
// handler-account scope per specs/handler-pause.md §5.2 HP-012.
//
// Cite: specs/handler-pause.md §5.2 HP-012; hk-6f1uj.
func scenarioBudgetExhausted() *ScriptFile {
	now := time.Now().UTC()
	const (
		runID  = "run-hk6f1uj-be-001"
		sessID = "sess-hk6f1uj-be-001"
	)

	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			// 1. handler_capabilities — first message on stream (HC-009).
			{
				Type: "handler_capabilities",
				Payload: map[string]any{
					"run_id":                      runID,
					"session_id":                  sessID,
					"protocol_versions_supported": []any{1},
					"claude_session_id":           "claude-sess-hk6f1uj-001",
				},
			},
			// 2. agent_ready (HC-039) — signals handler is live before exhaustion.
			{
				Type: "agent_ready",
				Payload: map[string]any{
					"run_id":       runID,
					"session_id":   sessID,
					"capabilities": []any{"scripted"},
				},
			},
			// 3. budget_exhausted — handler-account scope; trips HP-012 single-hit pause.
			// budget_ref="handler-account" signals the per-handler-account budget cap.
			{
				Type: "budget_exhausted",
				Payload: map[string]any{
					"run_id":                  runID,
					"budget_ref":              "handler-account",
					"attempted_dispatch_cost": 0.01,
				},
				RelativeTimestampMs: 5,
			},
			// 4. agent_failed — terminal event after budget exhaustion.
			{
				Type: "agent_failed",
				Payload: map[string]any{
					"run_id":         runID,
					"session_id":     sessID,
					"ended_at":       now.Add(20 * time.Millisecond).Format(time.RFC3339Nano),
					"error_category": "budget_exhausted",
					"reason":         "handler_account_budget_exhausted",
				},
			},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: handler-fatal (hk-qxtbq)
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: silent-hang (hk-0r1ti — SC-3 / hk-xfhva)
// ─────────────────────────────────────────────────────────────────────────────

// scenarioSilentHang returns a script that emits the preamble
// (handler_capabilities → agent_ready → agent_started) and then returns
// without emitting any heartbeats or outcome events.
//
// This models SC-3 (hk-xfhva): the agent process starts but then silently
// hangs — no heartbeats arrive, no outcome_emitted is ever sent.  The daemon's
// HC-056 silent-hang detection watcher MUST fire when presented with this event
// stream.
//
// Expected event ordering (truncated — intentionally no terminal events):
//
//  1. handler_capabilities (HC-009)
//  2. agent_ready          (HC-039)
//  3. agent_started        (§6.4)
//
// After agent_started the script ends.  The twin process exits 0; the daemon
// watcher observes the subprocess exit without outcome_emitted and without
// heartbeats, putting it in the regime where HC-056 silent-hang detection must
// have already fired (or fires on the subprocess-exit event).
//
// Cite: specs/handler-contract.md §4.6.HC-056, §7.1; hk-0r1ti; hk-xfhva.
func scenarioSilentHang() *ScriptFile {
	now := time.Now().UTC()
	const (
		runID     = "run-hk0r1ti-sh-001"
		sessID    = "sess-hk0r1ti-sh-001"
		nodeID    = "node-hk0r1ti-sh-001"
		agentType = "claude-twin-claude"
	)

	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			// 1. handler_capabilities — first message on stream (HC-009).
			{
				Type: "handler_capabilities",
				Payload: map[string]any{
					"run_id":                      runID,
					"session_id":                  sessID,
					"protocol_versions_supported": []any{1},
					"claude_session_id":           "claude-sess-hk0r1ti-001",
				},
			},
			// 2. agent_ready (HC-039).
			{
				Type: "agent_ready",
				Payload: map[string]any{
					"run_id":       runID,
					"session_id":   sessID,
					"capabilities": []any{"scripted"},
				},
			},
			// 3. agent_started (§6.4).
			{
				Type: "agent_started",
				Payload: map[string]any{
					"run_id":     runID,
					"session_id": sessID,
					"node_id":    nodeID,
					"agent_type": agentType,
					"started_at": now.Format(time.RFC3339Nano),
				},
			},
			// Deliberate: no heartbeats follow, no outcome_emitted.
			// The subprocess exits here.  The daemon watcher must detect
			// silent-hang (HC-056 / §7.1 FSM) and emit agent_failed.
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario: partial-pre-exec (hk-7amcv — SC-5 / hk-35mpj)
// ─────────────────────────────────────────────────────────────────────────────

// scenarioPartialPreExec returns a script that emits the handler-side pre-exec
// preamble only partially: handler_capabilities + agent_started are sent but
// agent_ready is never emitted.
//
// This models SC-5 (hk-35mpj): the twin process starts and emits
// handler_capabilities (announcing protocol support) and agent_started
// (confirming the agent process launched), but then exits without sending
// agent_ready.  The watcher times out waiting for agent_ready per the
// §7.2 handshake window; daemon HC-024 closes the bead with a terminal
// agent_failed event.
//
// Expected event ordering (truncated — agent_ready intentionally absent):
//
//  1. handler_capabilities (HC-009)
//  2. agent_started        (§6.4)
//
// After agent_started the script ends.  The twin process exits 0; the daemon
// watcher observes the subprocess exit without agent_ready having arrived,
// triggering the §7.2 handshake-timeout path and HC-024 terminal closure.
//
// Cite: specs/handler-contract.md §4.6.HC-024, §7.2; hk-7amcv; hk-35mpj.
func scenarioPartialPreExec() *ScriptFile {
	now := time.Now().UTC()
	const (
		runID     = "run-hk7amcv-ppe-001"
		sessID    = "sess-hk7amcv-ppe-001"
		nodeID    = "node-hk7amcv-ppe-001"
		agentType = "claude-twin-claude"
	)

	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			// 1. handler_capabilities — first message on stream (HC-009).
			{
				Type: "handler_capabilities",
				Payload: map[string]any{
					"run_id":                      runID,
					"session_id":                  sessID,
					"protocol_versions_supported": []any{1},
					"claude_session_id":           "claude-sess-hk7amcv-001",
				},
			},
			// 2. agent_started (§6.4) — process has launched but pre-exec handshake
			// stalls: agent_ready is never emitted.
			{
				Type: "agent_started",
				Payload: map[string]any{
					"run_id":     runID,
					"session_id": sessID,
					"node_id":    nodeID,
					"agent_type": agentType,
					"started_at": now.Format(time.RFC3339Nano),
				},
			},
			// Deliberate: no agent_ready follows.  The subprocess exits here.
			// The watcher's §7.2 handshake-timeout fires; HC-024 closes the bead.
		},
	}
}

// scenarioHandlerFatal returns a script that emits agent_ready followed by
// agent_failed, simulating a handler-fatal failure outcome.
//
// Used by TestScenario_WorkLoop_HandlerFatalTripsGate to exercise the
// dispatcher gate: after bead-1 reaches agent_failed, the test directly trips
// HandlerPausePolicyGoroutine so that bead-2 is held rather than dispatched.
//
// Cite: hk-qxtbq.
func scenarioHandlerFatal() *ScriptFile {
	now := time.Now().UTC()
	const (
		runID  = "run-qxtbq-hf-001"
		sessID = "sess-qxtbq-hf-001"
		nodeID = "node-qxtbq-hf-001"
	)
	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		// ExitWithError causes the twin process to exit 1 after emitting the
		// messages.  The work loop's CHB-020 exit=0 fallback would otherwise
		// close the bead rather than reopen it; a non-zero exit triggers the
		// default ReopenBead branch.
		ExitWithError: true,
		Messages: []ScriptMessage{
			{
				Type: "handler_capabilities",
				Payload: map[string]any{
					"run_id":                      runID,
					"session_id":                  sessID,
					"protocol_versions_supported": []any{1},
					"claude_session_id":           "claude-sess-qxtbq-hf-001",
				},
			},
			{
				Type: "agent_ready",
				Payload: map[string]any{
					"run_id":       runID,
					"session_id":   sessID,
					"capabilities": []any{"scripted"},
				},
			},
			{
				Type: "agent_started",
				Payload: map[string]any{
					"run_id":     runID,
					"session_id": sessID,
					"node_id":    nodeID,
					"agent_type": "claude-twin-claude",
					"started_at": now.Format(time.RFC3339Nano),
				},
			},
			// Terminal failure event — work loop reopens the bead on agent_failed.
			{
				Type: "agent_failed",
				Payload: map[string]any{
					"run_id":         runID,
					"session_id":     sessID,
					"ended_at":       now.Add(20 * time.Millisecond).Format(time.RFC3339Nano),
					"error_category": "transient",
					"reason":         "handler_fatal_test",
				},
			},
		},
	}
}
