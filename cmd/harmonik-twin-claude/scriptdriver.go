// Script-driver loop for the canonical twin binary.
//
// This file implements the script-file reader and message-stream emitter that
// drive the twin subprocess in scenario-mode, satisfying the carve-out declared
// in specs/handler-contract.md §4.6.HC-026a (scripted heartbeat mode) and the
// twin-parity surface of §4.8.HC-036 (subprocess script drives output instead
// of an LLM).
//
// # De-facto script-file schema (hk-ahvq.48.3)
//
// Until the normative spec section lands (follow-up bead hk-ahvq.48.11 authors
// the HC §4.8 / SH §4.3 section), the schema described here is the de-facto
// contract. Any consumer of twin script files MUST treat this godoc as the
// authoritative definition until hk-ahvq.48.11 is closed.
//
// File location:
//
//	<fixture-root>/<scenario>/twin-scripts/<role>.yaml
//
// Top-level YAML fields:
//
//	heartbeat_mode   string   "wall_clock" | "scripted" (default: "wall_clock")
//	                          Per HC-026a scripted-mode carve-out: "scripted"
//	                          allows heartbeats at explicit relative timestamps,
//	                          bypassing the T/2 wall-clock timer so that scenario
//	                          tests produce byte-reproducible event streams.
//	                          MUST be declared on the script when using scripted
//	                          heartbeats; absence means "wall_clock".
//	messages         list     Ordered list of ScriptMessage records (see below).
//
// ScriptMessage record fields:
//
//	type                  string   Required. One of the progress-stream message
//	                               types declared in handler-contract.md §4.2
//	                               (e.g., "agent_heartbeat", "agent_output_chunk",
//	                               "outcome_emitted"). The script-driver emits
//	                               this type verbatim; the watcher validates it.
//	payload               map      Optional. Key-value pairs merged into the
//	                               emitted JSON object alongside "type". Callers
//	                               MUST include all fields required by the wire
//	                               schema for the declared type (HC-007, §6.4,
//	                               event-model §8.3.*); the driver does not
//	                               synthesise missing fields.
//	relative_timestamp_ms int      Optional. Milliseconds from the previous
//	                               message (or script start for the first
//	                               message) to wait before emitting this message.
//	                               Ignored when heartbeat_mode is "wall_clock".
//	                               MUST be >= 0. A value of 0 means "emit
//	                               immediately after the previous message."
//
// # Scripted heartbeat carve-out (HC-026a)
//
// When heartbeat_mode is "scripted", the driver emits "agent_heartbeat"
// messages at the relative_timestamp_ms offsets declared in the script,
// bypassing the T/2 wall-clock timer. The driver enforces the carve-out
// condition: heartbeat_mode MUST be "scripted" on the script (not just
// inferred). This allows scenario tests to produce byte-reproducible event
// streams without depending on system clock jitter.
//
// Cite: specs/handler-contract.md §4.6.HC-026a, §4.8.HC-036.
// Follow-up spec-edit bead: hk-ahvq.48.11 (normative HC/SH section).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// twinScriptFixture — per-bead helper prefix for test helpers in this file.
// (Actual test helpers are in scriptdriver_test.go; the prefix is declared here
// as a godoc anchor per implementer-protocol.md §Helper-prefix discipline.)

// ────────────────────────────────────────────────────────────────────────────
// Schema types (de-facto contract; see package godoc for normative reference)
// ────────────────────────────────────────────────────────────────────────────

// heartbeatMode is the enum controlling how heartbeats are driven.
//
// Values: wall_clock (real-time T/2 timer) or scripted (relative timestamps
// from the script).  The default is wall_clock per the de-facto schema.
//
// Cite: specs/handler-contract.md §4.6.HC-026a scripted-mode carve-out.
// Follow-up spec-edit: hk-ahvq.48.11.
type heartbeatMode string

const (
	// heartbeatModeWallClock uses the real-time T/2 wall-clock timer for
	// heartbeat emission (HC-026a default; used by real handlers and resilience
	// tests per §10.2 HC-026 obligations).
	heartbeatModeWallClock heartbeatMode = "wall_clock"

	// heartbeatModeScripted drives heartbeats from relative_timestamp_ms values
	// declared in the script.  MUST be declared on the script (HC-026a
	// scripted-mode carve-out).  Limited to the canonical twin binary.
	heartbeatModeScripted heartbeatMode = "scripted"
)

// Valid reports whether hm is a declared heartbeatMode constant.
func (hm heartbeatMode) Valid() bool {
	switch hm {
	case heartbeatModeWallClock, heartbeatModeScripted:
		return true
	default:
		return false
	}
}

// ScriptMessage is one entry in the script's messages list.
//
// All fields map directly to the de-facto schema; see package godoc.
// Follow-up spec-edit: hk-ahvq.48.11.
type ScriptMessage struct {
	// Type is the progress-stream message type (e.g., "agent_heartbeat").
	// Required; non-empty. The driver emits this value verbatim as the "type"
	// field per HC-007 NDJSON framing.
	Type string `yaml:"type"`

	// Payload holds additional key-value pairs merged into the emitted JSON
	// object.  nil means no extra fields beyond "type".  Callers MUST include
	// all required wire-schema fields for the declared Type.
	Payload map[string]any `yaml:"payload,omitempty"`

	// RelativeTimestampMs is the milliseconds to wait before emitting this
	// message, measured from the previous message (or script start for the
	// first message).  Only honoured when heartbeat_mode is "scripted".
	// MUST be >= 0; negative values are treated as 0 (immediate).
	RelativeTimestampMs int `yaml:"relative_timestamp_ms,omitempty"`
}

// ScriptFile is the top-level type parsed from a twin script YAML file.
//
// File location: <fixture-root>/<scenario>/twin-scripts/<role>.yaml.
// Follow-up spec-edit: hk-ahvq.48.11.
type ScriptFile struct {
	// HeartbeatMode controls how heartbeats are timed (see heartbeatMode).
	// Defaults to "wall_clock" when absent or empty.
	HeartbeatMode heartbeatMode `yaml:"heartbeat_mode"`

	// Messages is the ordered list of progress-stream messages to emit.
	// nil or empty means no messages are emitted (the driver exits immediately).
	Messages []ScriptMessage `yaml:"messages"`
}

// loadScriptFile reads and parses the YAML script file at path.
//
// Returns an error if the file cannot be read, the YAML is malformed, or
// heartbeat_mode is an unrecognised value.
func loadScriptFile(path string) (*ScriptFile, error) {
	//nolint:gosec // G304: path is operator-supplied via --script-path flag; provenance is the scenario harness
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("loadScriptFile: read %q: %w", path, err)
	}
	var sf ScriptFile
	if err := yaml.Unmarshal(raw, &sf); err != nil {
		return nil, fmt.Errorf("loadScriptFile: parse %q: %w", path, err)
	}
	// Apply default: absent or empty heartbeat_mode means wall_clock.
	if sf.HeartbeatMode == "" {
		sf.HeartbeatMode = heartbeatModeWallClock
	}
	if !sf.HeartbeatMode.Valid() {
		return nil, fmt.Errorf("loadScriptFile: %q: unknown heartbeat_mode %q (want %q or %q)",
			path, sf.HeartbeatMode, heartbeatModeWallClock, heartbeatModeScripted)
	}
	return &sf, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Script-driver loop
// ────────────────────────────────────────────────────────────────────────────

// runScript drives the wireEmitter through the ordered message list in sf.
//
// For each ScriptMessage:
//   - If sf.HeartbeatMode is "scripted" and RelativeTimestampMs > 0, the
//     driver waits that many milliseconds (or until ctx is cancelled) before
//     emitting.  This implements the HC-026a scripted-mode carve-out.
//   - If sf.HeartbeatMode is "wall_clock", relative timestamps are ignored
//     and messages are emitted immediately in declaration order.
//
// runScript returns the first emit error encountered, or ctx.Err() if the
// context is cancelled before the stream completes.
func runScript(ctx context.Context, e *wireEmitter, sf *ScriptFile) error {
	scripted := sf.HeartbeatMode == heartbeatModeScripted

	for i, msg := range sf.Messages {
		// Respect relative delay in scripted mode only.
		if scripted && msg.RelativeTimestampMs > 0 {
			delay := time.Duration(msg.RelativeTimestampMs) * time.Millisecond
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		} else {
			// In wall_clock mode (or zero delay) still honour cancellation.
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		if err := emitScriptMessage(e, msg); err != nil {
			return fmt.Errorf("runScript: message %d (type=%q): %w", i, msg.Type, err)
		}
	}
	return nil
}

// emitScriptMessage serialises one ScriptMessage as a NDJSON-framed JSON object.
//
// The emitted object always contains "type"; all fields from Payload are merged
// in alongside it.  The "type" key in Payload is silently overwritten by msg.Type
// to prevent scripts from spoofing the type field.
func emitScriptMessage(e *wireEmitter, msg ScriptMessage) error {
	// Build the output map: start with the declared payload, then set "type"
	// last so that a script cannot override it from the payload map.
	out := make(map[string]any, len(msg.Payload)+1)
	for k, v := range msg.Payload {
		out[k] = v
	}
	out["type"] = msg.Type

	raw, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("emitScriptMessage: marshal: %w", err)
	}
	raw = append(raw, '\n')
	_, err = e.w.Write(raw)
	return err
}
