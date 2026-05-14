// Package handler — sub_reason enum for agent_failed wire messages (hk-u5c5i).
//
// Spec: specs/claude-hook-bridge.md §8 (error taxonomy table).
//
// SubReason is the typed string carried in agent_failed{sub_reason} and in
// outcome_emitted{kind=FAILURE_SIGNAL, sub_reason} messages.  All values are
// defined here so callers reference a constant rather than a bare string
// literal; string matching on sub_reason is still forbidden per HC-020 (use
// errors.Is / errors.As on the sentinel class instead).
//
// Naming convention:
//   - bridge_*  — failure detected by the harmonik side (handler-process or
//     relay), before or during Claude execution.
//   - claude_*  — failure reported by Claude via a StopFailure hook event
//     (§4.5 CHB-013) or inferred from Claude's exit behaviour (CHB-020).
//
// Tags: req:CHB-013, req:CHB-015, req:CHB-016, req:CHB-018, req:CHB-020.
package handler

// SubReason is the string value carried in the sub_reason field of
// agent_failed and outcome_emitted{kind=FAILURE_SIGNAL} wire messages.
//
// The underlying type is string so SubReason values marshal to JSON
// transparently; cast to string when constructing wire payloads.
//
// Spec: specs/claude-hook-bridge.md §8.
type SubReason string

// Bridge-side sub_reasons: detected by the handler-process or hook-relay,
// not by Claude itself.  These arise before or during Claude execution and
// are always produced by harmonik-owned code.
const (
	// SubReasonBridgeDialFailed is emitted when the hook-relay subprocess
	// cannot connect to the daemon socket within the 5-second dial window.
	// Class: ErrTransient.
	// Spec: specs/claude-hook-bridge.md §8, §4.6.CHB-015.
	SubReasonBridgeDialFailed SubReason = "bridge_dial_failed"

	// SubReasonBridgeDaemonStartupWindowExceeded is emitted when the relay's
	// daemon_not_ready retry budget is exhausted (25-second wall-clock cap).
	// Class: ErrTransient.
	// Spec: specs/claude-hook-bridge.md §8, §4.6.CHB-016.
	SubReasonBridgeDaemonStartupWindowExceeded SubReason = "bridge_daemon_startup_window_exceeded"

	// SubReasonBridgeMalformedHookPayload is emitted when the hook-relay
	// receives stdin JSON that is malformed or is missing a required field.
	// Class: ErrStructural.
	// Spec: specs/claude-hook-bridge.md §8.
	SubReasonBridgeMalformedHookPayload SubReason = "bridge_malformed_hook_payload"

	// SubReasonBridgeSessionIDMismatch is emitted when the hook stdin's
	// session_id does not match HARMONIK_CLAUDE_SESSION_ID.
	// Class: ErrStructural.
	// Spec: specs/claude-hook-bridge.md §8, §4.4 (exit 1 path).
	SubReasonBridgeSessionIDMismatch SubReason = "bridge_session_id_mismatch"

	// SubReasonBridgeEventKindMismatch is emitted when the hook stdin's
	// hook_event_name does not match the relay's argv <event-kind>.
	// Class: ErrStructural.
	// Spec: specs/claude-hook-bridge.md §8, §4.4 (exit 1 path).
	SubReasonBridgeEventKindMismatch SubReason = "bridge_event_kind_mismatch"

	// SubReasonBridgePartialWrite is emitted by the handler-process on
	// Wait-return when no outcome_emitted was observed, attributable to a
	// relay that terminated after opening the daemon socket but before
	// completing the NDJSON envelope (daemon received unidentifiable EOF).
	// Recovery: the CHB-020 "no outcome_emitted" branch fires and the
	// terminal event is emitted correctly.
	// Class: ErrTransient.
	// Spec: specs/claude-hook-bridge.md §8, §4.6.CHB-027.
	SubReasonBridgePartialWrite SubReason = "bridge_partial_write"

	// SubReasonBridgeSettingsShadowed is emitted during the pre-exec
	// settings-precedence verification (§4.9 CHB-024) when
	// settings.local.json would shadow the bridge-required hook entries.
	// Class: ErrStructural.
	// Spec: specs/claude-hook-bridge.md §8, §4.9.CHB-024.
	SubReasonBridgeSettingsShadowed SubReason = "bridge_settings_shadowed"
)

// Daemon-side sub_reasons: detected by the handler-process before exec'ing
// Claude, not reported by Claude itself.
const (
	// SubReasonTrustSeedFailed is emitted when the daemon cannot write the
	// pre-exec worktree auto-trust entry to ~/.claude.json before launching
	// Claude.  Without this, the interactive trust dialog would block the
	// session indefinitely (HC-056).
	// Class: ErrStructural.
	// Spec: specs/claude-hook-bridge.md §4.12 CHB-029.
	SubReasonTrustSeedFailed SubReason = "trust_seed_failed"

	// SubReasonTaskFileEmpty is emitted when agent-task.md is absent or
	// empty after the atomic write in §4.11 CHB-028.
	// Class: ErrStructural.
	// Spec: specs/claude-hook-bridge.md §8, §4.11.CHB-028.
	SubReasonTaskFileEmpty SubReason = "task_file_empty"
)

// Claude-side sub_reasons: derived from Claude's StopFailure hook
// (§4.5 CHB-013) or from Claude's exit behaviour (§4.7 CHB-020).
const (
	// SubReasonClaudeExitWithoutOutcome is emitted by the handler-process on
	// Wait-return when no outcome_emitted was observed and Claude exited
	// cleanly (exit code 0).  The agent shut down without producing a verdict,
	// which is a structural defect in the agent's plan.
	// Class: ErrStructural.
	// Spec: specs/claude-hook-bridge.md §8, §4.7.CHB-020.
	SubReasonClaudeExitWithoutOutcome SubReason = "claude_exit_without_outcome"

	// SubReasonClaudeCrashed is emitted by the handler-process on Wait-return
	// when no outcome_emitted was observed and Claude exited non-zero.
	// Class: ErrStructural.
	// Spec: specs/claude-hook-bridge.md §8, §4.7.CHB-020.
	SubReasonClaudeCrashed SubReason = "claude_crashed"

	// SubReasonClaudeAuthenticationFailed maps StopFailure{error_type:
	// authentication_failed} per CHB-013.
	// Class: ErrStructural.
	// Spec: specs/claude-hook-bridge.md §4.5.CHB-013.
	SubReasonClaudeAuthenticationFailed SubReason = "claude_authentication_failed"

	// SubReasonClaudeOAuthOrgNotAllowed maps StopFailure{error_type:
	// oauth_org_not_allowed} per CHB-013.
	// Class: ErrStructural.
	// Spec: specs/claude-hook-bridge.md §4.5.CHB-013.
	SubReasonClaudeOAuthOrgNotAllowed SubReason = "claude_oauth_org_not_allowed"

	// SubReasonClaudeBillingError maps StopFailure{error_type: billing_error}
	// per CHB-013.
	// Class: ErrStructural.
	// Spec: specs/claude-hook-bridge.md §4.5.CHB-013.
	SubReasonClaudeBillingError SubReason = "claude_billing_error"

	// SubReasonClaudeInvalidRequest maps StopFailure{error_type:
	// invalid_request} per CHB-013.
	// Class: ErrStructural.
	// Spec: specs/claude-hook-bridge.md §4.5.CHB-013.
	SubReasonClaudeInvalidRequest SubReason = "claude_invalid_request"

	// SubReasonClaudeMaxOutputTokens maps StopFailure{error_type:
	// max_output_tokens} per CHB-013.
	// Class: ErrStructural.
	// Spec: specs/claude-hook-bridge.md §4.5.CHB-013.
	SubReasonClaudeMaxOutputTokens SubReason = "claude_max_output_tokens"

	// SubReasonClaudeUnknown maps StopFailure{error_type: unknown} per
	// CHB-013.
	// Class: ErrStructural.
	// Spec: specs/claude-hook-bridge.md §4.5.CHB-013.
	SubReasonClaudeUnknown SubReason = "claude_unknown"

	// SubReasonClaudeServerError maps StopFailure{error_type: server_error}
	// per CHB-013.  This is the only claude_* sub_reason that maps to
	// ErrTransient rather than ErrStructural.
	// Class: ErrTransient.
	// Spec: specs/claude-hook-bridge.md §4.5.CHB-013.
	SubReasonClaudeServerError SubReason = "claude_server_error"
)
