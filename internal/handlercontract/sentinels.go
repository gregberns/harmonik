package handlercontract

import (
	"errors"
	"fmt"
)

// ErrTransient is emitted when a failure is transient and the operation may
// succeed on a subsequent attempt without any change to the plan.
//
// Detection (§8.1): network-transient conditions (DNS failure, connection
// reset, 5xx provisioning fetch), rate-limit-transient transport failures (not
// an agent_rate_limited event), progressless timeouts below the silent-hang
// threshold, and first-occurrence socket-level I/O errors per HC-024a.
//
// Emission: watcher publishes agent_failed{class="transient"}.
// Sub-reasons include socket_io_error and, when the framing is recoverable,
// partial-message.
var ErrTransient = errors.New("handlercontract: transient")

// ErrStructural is emitted when a failure indicates a structural problem that
// requires a plan change or operator intervention to resolve.
//
// Detection (§8.2): wrong-plan conditions (wrong tool, missing precondition
// only recoverable via a different plan), silent-hang per §7.1, post-outcome
// shutdown exceeding T_shutdown per HC-008a, partial or malformed
// progress-stream messages per HC-007b, watcher wedge or panic per HC-011a,
// sustained socket break after reconnect per HC-024a, and workspace held by a
// prior-generation orphan per HC-044a.
//
// Emission: watcher publishes agent_failed{class="structural", sub_reason=...}.
// Non-exhaustive sub-reasons: silent_hang, silent_hang_hard_kill,
// post_outcome_shutdown_timeout, partial-message, malformed_progress_message,
// progress_stream_broken, watcher_panic, watcher_wedged,
// workspace_held_by_orphan, skill_provisioning_failed (§8.6),
// protocol_mismatch and ndjson_line_too_long (§8.7).
//
// ErrSkillProvisioningFailed and ErrProtocolMismatch are sub-sentinels that
// wrap this error; errors.Is(subSentinel, ErrStructural) is true for both.
var ErrStructural = errors.New("handlercontract: structural")

// ErrDeterministic is emitted when a failure is confirmed to be a bug that
// will recur on every attempt with the same plan and inputs.
//
// Detection (§8.3): confirmed-bug evidence from structured fields — a specific
// deterministic exit code, a typed error payload, or an adapter-returned
// deterministic condition.
//
// Emission: watcher publishes agent_failed{class="deterministic"}.
var ErrDeterministic = errors.New("handlercontract: deterministic")

// ErrCanceled is emitted when the operation was explicitly cancelled by the
// operator or by policy.
//
// Detection (§8.4): operator-initiated or policy-initiated cancellation
// observed on the context. Context cancellation supersedes silent-hang
// escalation — if both conditions are present, ErrCanceled takes precedence.
//
// Emission: watcher publishes agent_failed{class="canceled"}.
var ErrCanceled = errors.New("handlercontract: canceled")

// ErrBudget is emitted when the dispatch budget would be exceeded before the
// agent is launched.
//
// Detection (§8.5): budget counter at dispatch time would exceed the remaining
// budget per control-points §4.5. Detection occurs BEFORE Launch — no
// subprocess is spawned.
//
// Emission: budget_exhausted (NOT agent_failed — no agent ran).
var ErrBudget = errors.New("handlercontract: budget exhausted")

// ErrSkillProvisioningFailed is a sub-sentinel that wraps ErrStructural per
// HC-021. It is emitted when a required skill cannot be provisioned for
// structural reasons.
//
// Detection (§8.6): skill name unresolvable per HC-048, OR resolved but
// failing due to a structural cause per HC-048a (package-integrity failure,
// unsupported manifest, or permission denied). Transient provisioning failures
// wrap ErrTransient directly and do NOT use this sub-sentinel.
//
// Emission: agent_failed{class="structural", sub_reason="skill_provisioning_failed", skill_name=...}.
//
// errors.Is(ErrSkillProvisioningFailed, ErrStructural) is true.
var ErrSkillProvisioningFailed = fmt.Errorf("handlercontract: skill provisioning failed: %w", ErrStructural)

// ErrProtocolMismatch is a sub-sentinel that wraps ErrStructural per HC-022.
// It is emitted when handler and agent cannot agree on a wire-protocol version.
//
// Detection (§8.7): no mutually supported wire-protocol version per HC-009,
// handler_capabilities absent within 5 seconds of connection, or an NDJSON
// line exceeding the cap defined in HC-007a.
//
// Emission: agent_failed{class="structural", sub_reason="protocol_mismatch"}
// or agent_failed{class="structural", sub_reason="ndjson_line_too_long"}.
//
// errors.Is(ErrProtocolMismatch, ErrStructural) is true.
var ErrProtocolMismatch = fmt.Errorf("handlercontract: protocol mismatch: %w", ErrStructural)

// Class returns the canonical error-class string for err as defined in §8 of
// specs/handler-contract.md.
//
// The return values are:
//   - ""              if err is nil or does not match any known class.
//   - "transient"     if errors.Is(err, ErrTransient).
//   - "structural"    if errors.Is(err, ErrStructural) — this includes both
//     sub-sentinels (ErrSkillProvisioningFailed, ErrProtocolMismatch) because
//     they wrap ErrStructural via their Unwrap chain. The canonical CLASS
//     string for sub-sentinels is "structural"; narrowest-first dispatch on
//     the specific sub-sentinel is the consumer's responsibility.
//   - "deterministic" if errors.Is(err, ErrDeterministic).
//   - "canceled"      if errors.Is(err, ErrCanceled).
//   - "budget"        if errors.Is(err, ErrBudget).
//
// ErrTransient is checked before ErrStructural so that an error wrapping both
// (a defensive edge case) is classified as "transient" rather than
// "structural". errors.Is walks the full Unwrap chain, so any error that
// wraps a class sentinel — at any depth — is correctly classified.
func Class(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrTransient):
		return "transient"
	case errors.Is(err, ErrStructural):
		return "structural" // also catches both sub-sentinels via Unwrap chain
	case errors.Is(err, ErrDeterministic):
		return "deterministic"
	case errors.Is(err, ErrCanceled):
		return "canceled"
	case errors.Is(err, ErrBudget):
		return "budget"
	default:
		return ""
	}
}
