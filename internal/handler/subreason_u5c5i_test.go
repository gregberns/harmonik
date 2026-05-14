package handler

import (
	"testing"
)

// subReasonFixtureAll returns every SubReason constant declared in
// subreason_u5c5i.go so table-driven tests can validate the full set.
func subReasonFixtureAll() []struct {
	name   string
	value  SubReason
	strVal string
} {
	return []struct {
		name   string
		value  SubReason
		strVal string
	}{
		// bridge_* constants
		{"BridgeDialFailed", SubReasonBridgeDialFailed, "bridge_dial_failed"},
		{"BridgeDaemonStartupWindowExceeded", SubReasonBridgeDaemonStartupWindowExceeded, "bridge_daemon_startup_window_exceeded"},
		{"BridgeMalformedHookPayload", SubReasonBridgeMalformedHookPayload, "bridge_malformed_hook_payload"},
		{"BridgeSessionIDMismatch", SubReasonBridgeSessionIDMismatch, "bridge_session_id_mismatch"},
		{"BridgeEventKindMismatch", SubReasonBridgeEventKindMismatch, "bridge_event_kind_mismatch"},
		{"BridgePartialWrite", SubReasonBridgePartialWrite, "bridge_partial_write"},
		{"BridgeSettingsShadowed", SubReasonBridgeSettingsShadowed, "bridge_settings_shadowed"},
		// daemon-side constants
		{"TrustSeedFailed", SubReasonTrustSeedFailed, "trust_seed_failed"},
		{"TaskFileEmpty", SubReasonTaskFileEmpty, "task_file_empty"},
		// claude_* constants
		{"ClaudeExitWithoutOutcome", SubReasonClaudeExitWithoutOutcome, "claude_exit_without_outcome"},
		{"ClaudeCrashed", SubReasonClaudeCrashed, "claude_crashed"},
		{"ClaudeAuthenticationFailed", SubReasonClaudeAuthenticationFailed, "claude_authentication_failed"},
		{"ClaudeOAuthOrgNotAllowed", SubReasonClaudeOAuthOrgNotAllowed, "claude_oauth_org_not_allowed"},
		{"ClaudeBillingError", SubReasonClaudeBillingError, "claude_billing_error"},
		{"ClaudeInvalidRequest", SubReasonClaudeInvalidRequest, "claude_invalid_request"},
		{"ClaudeMaxOutputTokens", SubReasonClaudeMaxOutputTokens, "claude_max_output_tokens"},
		{"ClaudeUnknown", SubReasonClaudeUnknown, "claude_unknown"},
		{"ClaudeServerError", SubReasonClaudeServerError, "claude_server_error"},
	}
}

// TestSubReasonStringValues verifies that every SubReason constant has exactly
// the wire-format string value required by the spec (§8 table and CHB-013).
// Any change to a constant value is a breaking wire-protocol change.
func TestSubReasonStringValues(t *testing.T) {
	t.Parallel()

	for _, tc := range subReasonFixtureAll() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := string(tc.value); got != tc.strVal {
				t.Errorf("SubReason%s = %q; want %q", tc.name, got, tc.strVal)
			}
		})
	}
}

// TestSubReasonUniqueness verifies that no two SubReason constants share the
// same string value; duplicate wire values would be a registry collision.
func TestSubReasonUniqueness(t *testing.T) {
	t.Parallel()

	seen := make(map[string]string) // wire value → constant name
	for _, tc := range subReasonFixtureAll() {
		if prior, ok := seen[tc.strVal]; ok {
			t.Errorf("duplicate SubReason value %q: constants %q and %q both use it",
				tc.strVal, prior, tc.name)
		}
		seen[tc.strVal] = tc.name
	}
}

// TestSubReasonNonEmpty verifies that no SubReason constant is the empty
// string; an empty sub_reason would be indistinguishable from an unset field.
func TestSubReasonNonEmpty(t *testing.T) {
	t.Parallel()

	for _, tc := range subReasonFixtureAll() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.value == "" {
				t.Errorf("SubReason%s is empty string; sub_reason constants must be non-empty", tc.name)
			}
		})
	}
}

// TestSubReasonBridgePrefixConvention checks that all bridge-side constants
// carry the "bridge_" prefix and all Claude-side constants carry the "claude_"
// prefix, matching the naming convention in §8.
func TestSubReasonBridgePrefixConvention(t *testing.T) {
	t.Parallel()

	bridgeConstants := []SubReason{
		SubReasonBridgeDialFailed,
		SubReasonBridgeDaemonStartupWindowExceeded,
		SubReasonBridgeMalformedHookPayload,
		SubReasonBridgeSessionIDMismatch,
		SubReasonBridgeEventKindMismatch,
		SubReasonBridgePartialWrite,
		SubReasonBridgeSettingsShadowed,
	}
	for _, sr := range bridgeConstants {
		s := string(sr)
		if len(s) < 7 || s[:7] != "bridge_" {
			t.Errorf("bridge-side SubReason %q does not start with %q", s, "bridge_")
		}
	}

	claudeConstants := []SubReason{
		SubReasonClaudeExitWithoutOutcome,
		SubReasonClaudeCrashed,
		SubReasonClaudeAuthenticationFailed,
		SubReasonClaudeOAuthOrgNotAllowed,
		SubReasonClaudeBillingError,
		SubReasonClaudeInvalidRequest,
		SubReasonClaudeMaxOutputTokens,
		SubReasonClaudeUnknown,
		SubReasonClaudeServerError,
	}
	for _, sr := range claudeConstants {
		s := string(sr)
		if len(s) < 7 || s[:7] != "claude_" {
			t.Errorf("claude-side SubReason %q does not start with %q", s, "claude_")
		}
	}
}

// TestSubReasonCHB013StopFailureMapping verifies the seven concrete StopFailure
// error_type values defined in CHB-013 each have a corresponding SubReason
// constant with the correct "claude_" + error_type wire value.
//
// Spec: specs/claude-hook-bridge.md §4.5.CHB-013.
func TestSubReasonCHB013StopFailureMapping(t *testing.T) {
	t.Parallel()

	// The CHB-013 table maps StopFailure.error_type to sub_reason by
	// prepending "claude_".  These are the seven concrete error_types.
	tests := []struct {
		errorType string
		want      SubReason
	}{
		{"authentication_failed", SubReasonClaudeAuthenticationFailed},
		{"oauth_org_not_allowed", SubReasonClaudeOAuthOrgNotAllowed},
		{"billing_error", SubReasonClaudeBillingError},
		{"invalid_request", SubReasonClaudeInvalidRequest},
		{"max_output_tokens", SubReasonClaudeMaxOutputTokens},
		{"unknown", SubReasonClaudeUnknown},
		{"server_error", SubReasonClaudeServerError},
	}

	for _, tc := range tests {
		t.Run(tc.errorType, func(t *testing.T) {
			t.Parallel()
			want := SubReason("claude_" + tc.errorType)
			if tc.want != want {
				t.Errorf("SubReason for error_type %q = %q; want %q", tc.errorType, tc.want, want)
			}
		})
	}
}
