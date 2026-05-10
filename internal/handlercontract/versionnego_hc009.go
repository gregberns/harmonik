package handlercontract

import "time"

// versionNego — per-bead helper prefix for test helpers in
// versionnego_hc009_test.go (implementer-protocol.md §Helper-prefix
// discipline; bead hk-8i31.10).

// HandlerCapabilitiesMsg is the on-wire NDJSON message the handler subprocess
// MUST emit as the FIRST progress-stream message on connection.
//
// The daemon reads this message to discover the handler's supported wire-protocol
// versions, selects the highest mutually supported version, and sends a
// version_selected control message back to the handler.  If handler_capabilities
// is absent within HandlerCapabilitiesTimeout, or no mutually supported version
// exists, the daemon terminates the session and returns ErrProtocolMismatch.
//
// # Wire fields
//
//   - type               — always "handler_capabilities" (ProgressMsgTypeHandlerCapabilities);
//     not stored after dispatch.
//   - supported_versions — ordered list of wire-protocol version integers the
//     handler supports.  The daemon selects the highest version present in both
//     lists.  A nil or empty slice means the handler supports NO version; the
//     daemon MUST treat this as a version-negotiation failure and return
//     ErrProtocolMismatch.
//
// Spec: specs/handler-contract.md §4.2.HC-009, §7.2.
type HandlerCapabilitiesMsg struct {
	// Type is always ProgressMsgTypeHandlerCapabilities; retained for round-trip
	// fidelity.  Watcher dispatches on this field before decoding.
	Type string `json:"type"`

	// SupportedVersions is the list of wire-protocol version integers the
	// handler subprocess supports (specs/handler-contract.md §4.2.HC-009).
	//
	// The daemon selects max(intersection(daemon.supported, handler.supported)).
	// When the intersection is empty, the daemon returns ErrProtocolMismatch.
	//
	// TODO(hk-8i31.10): The version type is currently an int per the
	// pseudo-code at §7.2; OQ-HC-009 tracks whether semver should be used
	// instead.  This field uses int until that open question is resolved.
	SupportedVersions []int `json:"supported_versions"`
}

// HandlerCapabilitiesTimeout is the maximum duration the watcher waits for the
// handler subprocess to emit the handler_capabilities message after subprocess
// spawn.
//
// If no handler_capabilities message is received within this window, the watcher
// MUST abort the session with ErrProtocolMismatch per §4.2.HC-009, §7.2, §8.7.
//
// Normative value: 5 seconds (specs/handler-contract.md §7.2 pseudocode,
// §8.7 detection rule).
const HandlerCapabilitiesTimeout = 5 * time.Second

// VersionSelectedControlMsgType is the "type" field value of the daemon-to-handler
// control message sent after successful version negotiation per §7.2.
//
// After reading handler_capabilities and selecting the highest mutually supported
// version, the daemon sends a NDJSON control message of this type carrying
// {selected_version: <int>} to the handler subprocess on the same socket.
//
// Spec: specs/handler-contract.md §4.11 (control message catalog), §7.2.
const VersionSelectedControlMsgType = "version_selected"
