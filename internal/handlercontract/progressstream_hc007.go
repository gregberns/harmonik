package handlercontract

// progressStream — per-bead helper prefix for test helpers in
// progressstream_hc007_test.go (implementer-protocol.md §Helper-prefix
// discipline; bead hk-8i31.7).

// ProgressMsgType is the string discriminator field ("type") carried in every
// NDJSON-framed progress-stream message emitted by a handler subprocess.
//
// The 12 required message types are declared as untyped string constants below
// (ProgressMsgTypeHandlerCapabilities … ProgressMsgTypeOutcomeEmitted).  A
// watcher dispatches on this field before decoding the remainder of the JSON
// object; unknown type values MUST be ignored (not treated as errors) to allow
// additive evolution.
//
// Spec: specs/handler-contract.md §4.2.HC-007.
type ProgressMsgType = string

// Progress-stream message type constants — the complete required set per
// specs/handler-contract.md §4.2.HC-007.
//
// Each constant is the literal string value the handler subprocess places in
// the "type" field of the corresponding NDJSON-framed progress-stream message.
// The watcher dispatches on these values to select the decoding path and the
// bus-event name.
const (
	// ProgressMsgTypeHandlerCapabilities is emitted as the FIRST message on
	// the progress stream per §4.2.HC-009.  Carries the handler's supported
	// wire-protocol versions; the daemon selects the highest mutually supported
	// version or returns ErrProtocolMismatch (§8.7).
	ProgressMsgTypeHandlerCapabilities ProgressMsgType = "handler_capabilities"

	// ProgressMsgTypeAgentReady is emitted when the handler subprocess has
	// completed startup and is ready to accept work per §4.9.HC-039.  The
	// watcher translates this into the agent_ready bus event.
	ProgressMsgTypeAgentReady ProgressMsgType = "agent_ready"

	// ProgressMsgTypeAgentStarted is emitted when the handler subprocess has
	// started processing a work item.  The watcher translates this into the
	// agent_started bus event.
	ProgressMsgTypeAgentStarted ProgressMsgType = "agent_started"

	// ProgressMsgTypeAgentOutputChunk is emitted per output chunk produced by
	// the agent subprocess.  Best-effort stream: replay on retry is NOT
	// guaranteed to reproduce identical chunks per §4.2.HC-007b.
	ProgressMsgTypeAgentOutputChunk ProgressMsgType = "agent_output_chunk"

	// ProgressMsgTypeAgentCompleted is emitted on clean exit after
	// outcome_emitted, or on dirty exit inside the post-outcome shutdown window
	// per §4.2.HC-008a.  Exactly one terminal event per session per HC-INV-006.
	ProgressMsgTypeAgentCompleted ProgressMsgType = "agent_completed"

	// ProgressMsgTypeAgentFailed is emitted on any terminal failure condition
	// (crash, silent-hang, socket break, protocol mismatch, etc.) per §4.6.
	// Carries class and sub_reason fields from the error taxonomy (§8).
	ProgressMsgTypeAgentFailed ProgressMsgType = "agent_failed"

	// ProgressMsgTypeAgentRateLimited is emitted when the adapter detects a
	// rate-limit condition per §4.6.HC-025.  Carries retry_after; the session
	// is NOT a failure.
	ProgressMsgTypeAgentRateLimited ProgressMsgType = "agent_rate_limited"

	// ProgressMsgTypeAgentRateLimitCleared is emitted when the rate-limit
	// condition observed in the prior agent_rate_limited message has cleared per
	// §4.6.HC-025.
	ProgressMsgTypeAgentRateLimitCleared ProgressMsgType = "agent_rate_limit_cleared"

	// ProgressMsgTypeAgentHeartbeat is emitted at least every T/2 seconds while
	// the subprocess is alive and has not yet emitted outcome_emitted per
	// §4.2.HC-026a.  Its presence resets the silent-hang timer (§7.1).
	ProgressMsgTypeAgentHeartbeat ProgressMsgType = "agent_heartbeat"

	// ProgressMsgTypeSessionLogLocation is emitted early in the session (after
	// handler_capabilities and before agent_ready) per §4.2.HC-010.  Carries
	// {session_id, run_id, node_id, agent_type, log_path, log_format, bead_id?}.
	ProgressMsgTypeSessionLogLocation ProgressMsgType = "session_log_location"

	// ProgressMsgTypeSkillsProvisioned is emitted after all required skills
	// have been provisioned per §4.11.HC-049.
	ProgressMsgTypeSkillsProvisioned ProgressMsgType = "skills_provisioned"

	// ProgressMsgTypeOutcomeEmitted is the final progress-stream message,
	// carrying the session's Outcome per §4.2.HC-008.  The watcher translates
	// it into the outcome_emitted bus event.  After this message the subprocess
	// MUST exit within T_shutdown (§4.2.HC-008a).
	ProgressMsgTypeOutcomeEmitted ProgressMsgType = "outcome_emitted"

	// ProgressMsgTypeLaunchInitiated is the pre-exec precursor emitted by the
	// handler-process BEFORE exec'ing Claude per CHB-018 step 4 (interactive
	// substrate path). It signals that the handler is about to exec Claude but
	// does NOT indicate that Claude is ready to accept work — that signal is
	// the relay-synthesized agent_ready on first SessionStart hook receipt per
	// CHB-013 / HC-039.
	//
	// Adapters MUST NOT return true from DetectReady on receipt of this message
	// per HC-041.
	ProgressMsgTypeLaunchInitiated ProgressMsgType = "launch_initiated"
)

// NDJSONMaxLineLenBytes is the maximum byte length of a single NDJSON-framed
// progress-stream line (byte sequence up to and including the terminating \n).
//
// A line exceeding this cap MUST cause the watcher to abort the session with
// ErrProtocolMismatch and emit agent_failed with sub_reason =
// NDJSONLineTooLongSubReason.
//
// The cap is a DoS guard at the framing layer; it is NOT a payload-size limit
// (payloads > 1 MiB take the LaunchSpec file-path path per §4.2.HC-005).
// Outbound handler messages larger than this cap are a protocol defect.
//
// Normative value: 1 MiB (specs/handler-contract.md §4.2.HC-007a).
const NDJSONMaxLineLenBytes = 1 << 20 // 1 MiB

// Sub-reason string constants for progress-stream framing failures (§4.2.HC-007b,
// §8.7).  These are the literal sub_reason field values placed in the
// agent_failed payload when the watcher detects a framing violation.

// NDJSONLineTooLongSubReason is the sub_reason value the watcher MUST use when
// a progress-stream line exceeds NDJSONMaxLineLenBytes.
//
// The corresponding error class is ErrProtocolMismatch (which wraps ErrStructural)
// per §8.7.
//
// Spec: specs/handler-contract.md §4.2.HC-007a, §8.7.
const NDJSONLineTooLongSubReason = "ndjson_line_too_long"

// ProtocolMismatchSubReason is the sub_reason value the watcher MUST use when
// version negotiation fails — the handler advertises no mutually supported wire
// version, or never emits handler_capabilities within the caps timeout — and the
// failure surfaces through the progress-stream scanner as an error wrapping
// ErrProtocolMismatch.
//
// The corresponding error class is ErrProtocolMismatch (which wraps ErrStructural)
// per §8.7. This is the general protocol-mismatch case; the line-cap subcase uses
// NDJSONLineTooLongSubReason instead.
//
// Spec: specs/handler-contract.md §8.7.
const ProtocolMismatchSubReason = "protocol_mismatch"

// PartialMessageSubReason is the sub_reason value the watcher MUST use when
// the progress stream closes (EOF) with bytes buffered before the terminating
// \n — i.e., a message that was started but never terminated.
//
// The corresponding error class is ErrStructural per §8.2 (most partial-message
// cases; see also ErrTransient note in §8.1 for recoverable framing causes).
//
// Spec: specs/handler-contract.md §4.2.HC-007b.
const PartialMessageSubReason = "partial-message"

// MalformedProgressMessageSubReason is the sub_reason value the watcher MUST
// use when a syntactically invalid JSON object is received on a live socket.
// On detection the watcher MUST close the session (no reconnect per §4.10.HC-044
// and §4.6.HC-024a).
//
// The corresponding error class is ErrStructural per §8.2.
//
// Spec: specs/handler-contract.md §4.2.HC-007b.
const MalformedProgressMessageSubReason = "malformed_progress_message"
