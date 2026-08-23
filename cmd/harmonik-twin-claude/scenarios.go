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
	case "heartbeat-then-hold":
		return scenarioHeartbeatThenHold(), nil
	default:
		return nil, fmt.Errorf("unknown scenario %q: must be one of single-happy-path, review-loop-3iter, rate-limit, dial-failed, daemon-not-ready-retry, commit-on-cue-startup-delay, budget-exhausted, handler-fatal, silent-hang, partial-pre-exec, heartbeat-then-hold", name)
	}
}

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

func scenarioReviewLoop3Iter() *ScriptFile {
	now := time.Now().UTC()
	const (
		runID     = "run-chb021-rl3-001"
		nodeID    = "node-chb021-rl3-001"
		agentType = "claude-twin-claude"

		implClaudeSessionID = "claude-impl-rl3-001"

		rev1ClaudeSessionID = "claude-rev-rl3-001"
		rev2ClaudeSessionID = "claude-rev-rl3-002"
		rev3ClaudeSessionID = "claude-rev-rl3-003"
	)

	msgs := []ScriptMessage{}

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

	addPhase("sess-impl-rl3-initial", implClaudeSessionID, "implementer-initial", "WORK_COMPLETE", 1)
	addPhase("sess-rev-rl3-001", rev1ClaudeSessionID, "reviewer", "REVIEWER_VERDICT", 1)

	addPhase("sess-impl-rl3-resume2", implClaudeSessionID, "implementer-resume", "WORK_COMPLETE", 2)
	addPhase("sess-rev-rl3-002", rev2ClaudeSessionID, "reviewer", "REVIEWER_VERDICT", 2)

	addPhase("sess-impl-rl3-resume3", implClaudeSessionID, "implementer-resume", "WORK_COMPLETE", 3)
	addPhase("sess-rev-rl3-003", rev3ClaudeSessionID, "reviewer", "REVIEWER_VERDICT", 3)

	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages:      msgs,
	}
}

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

func scenarioBudgetExhausted() *ScriptFile {
	now := time.Now().UTC()
	const (
		runID  = "run-hk6f1uj-be-001"
		sessID = "sess-hk6f1uj-be-001"
	)

	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
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
		},
	}
}

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
		},
	}
}

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

func scenarioHeartbeatThenHold() *ScriptFile {
	now := time.Now().UTC()
	const (
		runID     = "run-vn2-hth-001"
		sessID    = "sess-vn2-hth-001"
		nodeID    = "node-vn2-hth-001"
		agentType = "claude-twin-claude"
	)

	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			{
				Type: "handler_capabilities",
				Payload: map[string]any{
					"run_id":                      runID,
					"session_id":                  sessID,
					"protocol_versions_supported": []any{1},
					"claude_session_id":           "claude-sess-vn2-hth-001",
				},
			},
			// 2. agent_ready (HC-039) — clears waitAgentReady so the run advances
			//    into the paste-inject + watchdog phase where the tap contention
			//    occurs.
			{
				Type: "agent_ready",
				Payload: map[string]any{
					"run_id":       runID,
					"session_id":   sessID,
					"capabilities": []any{"scripted", "heartbeat"},
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
			// 4. agent_heartbeat — the heartbeat the watchdog must observe
			//    (firstHeartbeatSeen) to advance. On the pre-fix shared tap this is
			//    stolen by waitAgentReady's drainer under concurrency.
			{
				Type: "agent_heartbeat",
				Payload: map[string]any{
					"session_id": sessID,
					"phase":      "reasoning",
				},
				RelativeTimestampMs: 10,
			},
			// 5. hold — keep the pane child alive (no commit, no terminal event)
			//    until the daemon cancels the run context. This is the
			//    watchdog-engaging block: the pane stays "active" so the
			//    launch-suppression branch remains live.
			{
				Type: holdStep,
				Payload: map[string]any{
					"hold_ms": 0, // block until ctx cancel
				},
			},
		},
	}
}
