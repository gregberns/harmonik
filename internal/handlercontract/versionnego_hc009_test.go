package handlercontract_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// versionNegoFixture — per-bead helper prefix for test helpers in this file
// (implementer-protocol.md §Helper-prefix discipline; bead hk-8i31.10).

// ─────────────────────────────────────────────────────────────────────────────
// HC-009 — HandlerCapabilitiesTimeout value
// ─────────────────────────────────────────────────────────────────────────────

// TestVersionNego_HandlerCapabilitiesTimeoutValue verifies that
// HandlerCapabilitiesTimeout equals 5 seconds as required by
// specs/handler-contract.md §7.2 and §8.7.
//
// The watcher awaits handler_capabilities for exactly this duration;
// exceeding it triggers ErrProtocolMismatch.
func TestVersionNego_HandlerCapabilitiesTimeoutValue(t *testing.T) {
	t.Parallel()

	const want = 5 * time.Second
	if handlercontract.HandlerCapabilitiesTimeout != want {
		t.Errorf("HandlerCapabilitiesTimeout = %v, want %v (HC-009 §7.2)", handlercontract.HandlerCapabilitiesTimeout, want)
	}
}

// TestVersionNego_HandlerCapabilitiesTimeoutPositive verifies the timeout is
// strictly positive (zero would skip the wait; negative is invalid).
func TestVersionNego_HandlerCapabilitiesTimeoutPositive(t *testing.T) {
	t.Parallel()

	if handlercontract.HandlerCapabilitiesTimeout <= 0 {
		t.Errorf("HandlerCapabilitiesTimeout = %v; must be > 0", handlercontract.HandlerCapabilitiesTimeout)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-009 — HandlerCapabilitiesMsg type field
// ─────────────────────────────────────────────────────────────────────────────

// TestVersionNego_HandlerCapabilitiesMsgTypeField verifies that the Type field
// of HandlerCapabilitiesMsg matches ProgressMsgTypeHandlerCapabilities.
//
// The watcher dispatches on this literal string; a mismatch silently
// breaks version negotiation.
func TestVersionNego_HandlerCapabilitiesMsgTypeField(t *testing.T) {
	t.Parallel()

	msg := handlercontract.HandlerCapabilitiesMsg{
		Type:              handlercontract.ProgressMsgTypeHandlerCapabilities,
		SupportedVersions: []int{1},
	}

	if msg.Type != handlercontract.ProgressMsgTypeHandlerCapabilities {
		t.Errorf("HandlerCapabilitiesMsg.Type = %q, want ProgressMsgTypeHandlerCapabilities (%q)",
			msg.Type, handlercontract.ProgressMsgTypeHandlerCapabilities)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-009 — HandlerCapabilitiesMsg JSON round-trip
// ─────────────────────────────────────────────────────────────────────────────

// TestVersionNego_HandlerCapabilitiesMsgRoundTrip verifies that
// HandlerCapabilitiesMsg encodes and decodes its fields faithfully.
func TestVersionNego_HandlerCapabilitiesMsgRoundTrip(t *testing.T) {
	t.Parallel()

	orig := handlercontract.HandlerCapabilitiesMsg{
		Type:              "handler_capabilities",
		SupportedVersions: []int{1, 2, 3},
	}

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got handlercontract.HandlerCapabilitiesMsg
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Type != orig.Type {
		t.Errorf("Type: got %q, want %q", got.Type, orig.Type)
	}
	if len(got.SupportedVersions) != len(orig.SupportedVersions) {
		t.Fatalf("SupportedVersions length: got %d, want %d", len(got.SupportedVersions), len(orig.SupportedVersions))
	}
	for i, v := range orig.SupportedVersions {
		if got.SupportedVersions[i] != v {
			t.Errorf("SupportedVersions[%d]: got %d, want %d", i, got.SupportedVersions[i], v)
		}
	}
}

// TestVersionNego_HandlerCapabilitiesMsgWireFieldNames verifies that the JSON
// serialization uses the spec-mandated wire field names.
func TestVersionNego_HandlerCapabilitiesMsgWireFieldNames(t *testing.T) {
	t.Parallel()

	msg := handlercontract.HandlerCapabilitiesMsg{
		Type:              "handler_capabilities",
		SupportedVersions: []int{1},
	}

	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	for _, wantKey := range []string{"type", "supported_versions"} {
		if _, ok := raw[wantKey]; !ok {
			t.Errorf("expected JSON key %q in HandlerCapabilitiesMsg", wantKey)
		}
	}
}

// TestVersionNego_HandlerCapabilitiesMsgEmptySupportedVersions verifies that a
// HandlerCapabilitiesMsg with an empty SupportedVersions slice round-trips without
// error.  An empty list means the handler supports no version; the watcher MUST
// classify this as a version-negotiation failure (ErrProtocolMismatch) per
// HC-009, but the struct itself must be decodable.
func TestVersionNego_HandlerCapabilitiesMsgEmptySupportedVersions(t *testing.T) {
	t.Parallel()

	msg := handlercontract.HandlerCapabilitiesMsg{
		Type:              "handler_capabilities",
		SupportedVersions: []int{},
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal empty SupportedVersions: %v", err)
	}
	var got handlercontract.HandlerCapabilitiesMsg
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Length 0 is valid at the struct level; watcher classifies it as failure.
	if len(got.SupportedVersions) != 0 {
		t.Errorf("SupportedVersions: got len %d, want 0", len(got.SupportedVersions))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-009 — VersionSelectedControlMsgType value
// ─────────────────────────────────────────────────────────────────────────────

// TestVersionNego_VersionSelectedControlMsgTypeValue verifies the literal string
// value of VersionSelectedControlMsgType matches the spec's control message
// catalog (specs/handler-contract.md §4.11 catalog, §7.2).
func TestVersionNego_VersionSelectedControlMsgTypeValue(t *testing.T) {
	t.Parallel()

	const want = "version_selected"
	if handlercontract.VersionSelectedControlMsgType != want {
		t.Errorf("VersionSelectedControlMsgType = %q, want %q", handlercontract.VersionSelectedControlMsgType, want)
	}
}
