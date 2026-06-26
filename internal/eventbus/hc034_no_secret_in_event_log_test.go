package eventbus_test

// hc034_no_secret_in_event_log_test.go — end-to-end redaction sensor for HC-034.
//
// Spec ref: specs/handler-contract.md §4.7.HC-034; bead hk-8i31.41.
//
// HC-034 states: no secret value MAY appear in any persisted event record,
// audit record, or session log as stored to disk.  Operator debugging of
// secret-related failures MUST use redacted forms; full-secret access requires
// filesystem-level privileges outside harmonik's control surface.
//
// # Scope
//
// This file covers the event-log (JSONL) persistence surface.  The session-log
// surface is handler-specific and is tracked separately.  The JSONL file is the
// only persisted output owned by the eventbus package; it is the primary
// at-rest artifact in scope for HC-034 at MVH.
//
// # What this file provides
//
//  1. TestHC034_SecretNamedFieldAbsentFromJSONL — HC-031 path: a payload field
//     whose NAME matches the common-prefix regex MUST appear as "<redacted>" in
//     the decoded JSONL payload, not as the original value.
//
//  2. TestHC034_SecretValuePatternAbsentFromJSONL — HC-032 path: a payload
//     field whose VALUE matches a registered per-handler pattern MUST appear as
//     "<redacted>" in the decoded JSONL payload, not as the original value.
//
//  3. TestHC034_BothHC031AndHC032SecretAbsentFromJSONL — composed path: a
//     payload carrying both a secret-named field (HC-031) and a secret-valued
//     field (HC-032) MUST have both values redacted in the JSONL output, with
//     safe fields preserved unchanged.
//
//  4. TestHC034_SafeFieldsPreservedInJSONL — no over-redaction: a payload with
//     only safe field names and values reaches the JSONL file verbatim.
//
//  5. TestHC034_EmitWithRunID_SecretAbsentFromJSONL — HC-031 path via
//     EmitWithRunID: the run-stamped envelope path also redacts secret-named
//     fields before JSONL append.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hc034Fixture prefix per
// implementer-protocol.md §Helper-prefix discipline.  The helper prefix is
// derived from the bead ID (hk-8i31.41); "hc034" is the requirement tag.
//
// # Assertion strategy
//
// The JSONL payload is a JSON-encoded value nested inside the EV-001 envelope.
// Go's json.Marshal HTML-escapes < and > by default, so the literal string
// "<redacted>" round-trips as "<redacted>" in the raw bytes.
// Tests decode the envelope and payload to compare values as Go strings so
// that encoding artefacts do not produce false failures.  Raw-byte checks for
// the original secret values are performed in addition to decoded checks.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test fixtures
// ─────────────────────────────────────────────────────────────────────────────

// hc034FixtureEventType is the synthetic event type used across HC-034 tests.
const hc034FixtureEventType core.EventType = "test.hc034.v1"

// hc034FixtureRunEventType is an F-class event used in EmitWithRunID tests.
// "run_started" is F-class (§8.1) and carries a run_id stamp.
const hc034FixtureRunEventType core.EventType = "run_started"

// hc034FixtureAnthropicPattern is the compiled Anthropic API key shape.
// Mirrors the pattern declared in redactionregistry_test.go (registryFixtureAnthropicPattern)
// without cross-file import — test helpers are not importable across files.
var hc034FixtureAnthropicPattern = regexp.MustCompile(`^sk-ant-[A-Za-z0-9_\-]{10,}$`)

// hc034FixtureAnthropicKeyStub is a structural key stub matching the Anthropic
// pattern shape.  Uses 'x' padding only per HC-034 compliance: the body MUST
// NOT contain real credential material.
//
// Spec ref: specs/handler-contract.md §4.7.HC-034 — no real secret value in test fixtures.
const hc034FixtureAnthropicKeyStub = "sk-ant-" +
	"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

// hc034FixtureSecretFieldValue is a clearly-fake secret value used in HC-031
// path tests (field-name redaction).  The value does NOT match any pattern
// shape so that the test exercises HC-031 in isolation.
const hc034FixtureSecretFieldValue = "hc034-fake-secret-field-value"

// hc034FixtureJSONLPath returns a temporary JSONL log path for one test.
func hc034FixtureJSONLPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "events.jsonl")
}

// hc034FixtureReadJSONL reads the JSONL file at path and returns all non-empty
// lines.
func hc034FixtureReadJSONL(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("hc034FixtureReadJSONL: ReadFile %s: %v", path, err)
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// hc034FixtureDecodePayload decodes the JSONL line as an EV-001 envelope and
// returns the nested payload as a string-keyed map.
//
// The JSONL line is a JSON object with an EV-001 envelope; the "payload" field
// contains the redacted event payload as a nested JSON object.
func hc034FixtureDecodePayload(t *testing.T, line string) map[string]any {
	t.Helper()
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		t.Fatalf("hc034FixtureDecodePayload: unmarshal envelope: %v\n  line: %s", err, line)
	}
	var payload map[string]any
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatalf("hc034FixtureDecodePayload: unmarshal payload: %v\n  payload bytes: %s", err, envelope.Payload)
	}
	return payload
}

// hc034FixtureWildcardPattern returns a wildcard EventPattern matching all event types.
func hc034FixtureWildcardPattern() core.EventPattern {
	return core.EventPattern{Wildcard: true}
}

// hc034FixtureNewRunID generates a UUIDv7-based RunID for EmitWithRunID tests.
func hc034FixtureNewRunID(t *testing.T) core.RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hc034FixtureNewRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(id)
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-034 test 1: HC-031 path — secret-named field absent from JSONL
// ─────────────────────────────────────────────────────────────────────────────

// TestHC034_SecretNamedFieldAbsentFromJSONL is the HC-031 path of the HC-034
// end-to-end redaction sensor (hk-8i31.41).
//
// Contract under test:
// When an event payload contains a field whose NAME matches the HC-031
// common-prefix regex (e.g. "token"), the PERSISTED JSONL file MUST NOT
// contain the original field value.  The decoded payload MUST contain
// "<redacted>" instead.
//
// This exercises the full pipeline: Emit → HC-031 redaction → JSON re-encode →
// JSONL append.  It is the "at-rest" complement to the in-flight dispatch tests
// in busimpl_test.go.
//
// Spec refs: specs/handler-contract.md §4.7.HC-034, §4.7.HC-031.
// Bead ref: hk-8i31.41.
func TestHC034_SecretNamedFieldAbsentFromJSONL(t *testing.T) {
	t.Parallel()

	logPath := hc034FixtureJSONLPath(t)
	writer, err := eventbus.OpenJSONLWriter(logPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer func() { _ = writer.Close() }()

	// Construct a bus with no HC-032 patterns — only HC-031 fires here.
	bus := eventbus.NewBusImplWithWriter(nil, writer)
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

	// Payload: "token" matches HC-031; "node_id" is safe.
	payload, marshalErr := json.Marshal(map[string]any{
		"token":   hc034FixtureSecretFieldValue,
		"node_id": "hc034-node-hc031",
	})
	if marshalErr != nil {
		t.Fatalf("json.Marshal: %v", marshalErr)
	}

	if emitErr := bus.Emit(context.Background(), hc034FixtureEventType, payload); emitErr != nil {
		t.Fatalf("Emit: %v", emitErr)
	}

	lines := hc034FixtureReadJSONL(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("JSONL file has %d lines, want 1", len(lines))
	}

	line := lines[0]

	// HC-034: the raw secret value MUST NOT appear in the raw JSONL bytes.
	if strings.Contains(line, hc034FixtureSecretFieldValue) {
		t.Errorf(
			"HC-034 VIOLATED (HC-031 path): persisted JSONL raw bytes contain the secret value %q;\n"+
				"  want: value replaced by %q before JSONL append\n"+
				"  spec: specs/handler-contract.md §4.7.HC-034 — no secret value MAY appear in "+
				"any persisted event record",
			hc034FixtureSecretFieldValue, core.RedactedSentinel,
		)
	}

	// Decode and check the stored value in the payload.
	got := hc034FixtureDecodePayload(t, line)

	tokenVal, ok := got["token"]
	if !ok {
		t.Error("HC-034: decoded payload missing 'token' key; redaction MUST preserve the key, only replacing the value")
	} else if tokenVal != core.RedactedSentinel {
		t.Errorf(
			"HC-034 VIOLATED (HC-031 path): decoded payload[\"token\"] = %q, want %q;\n"+
				"  the redacted sentinel MUST be the stored value, not the original secret\n"+
				"  spec: specs/handler-contract.md §4.7.HC-034, §4.7.HC-031",
			tokenVal, core.RedactedSentinel,
		)
	}

	// Safe field MUST be present and unchanged.
	if nodeVal, nodeOK := got["node_id"]; !nodeOK {
		t.Error("HC-034: decoded payload missing safe field 'node_id'")
	} else if nodeVal != "hc034-node-hc031" {
		t.Errorf("HC-034: decoded payload[\"node_id\"] = %v, want %q; safe fields MUST NOT be redacted", nodeVal, "hc034-node-hc031")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-034 test 2: HC-032 path — secret-valued field absent from JSONL
// ─────────────────────────────────────────────────────────────────────────────

// TestHC034_SecretValuePatternAbsentFromJSONL is the HC-032 path of the HC-034
// end-to-end redaction sensor (hk-8i31.41).
//
// Contract under test:
// When an event payload contains a field whose VALUE matches a registered
// per-handler pattern (HC-032), the PERSISTED JSONL file MUST NOT contain the
// original value.  The field name itself is benign (not a HC-031 match), so
// only the HC-032 value-pattern redaction is in scope here.
//
// Spec refs: specs/handler-contract.md §4.7.HC-034, §4.7.HC-032.
// Bead ref: hk-8i31.41.
func TestHC034_SecretValuePatternAbsentFromJSONL(t *testing.T) {
	t.Parallel()

	logPath := hc034FixtureJSONLPath(t)
	writer, err := eventbus.OpenJSONLWriter(logPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer func() { _ = writer.Close() }()

	// Register the Anthropic key-shape pattern for one subsystem.
	registry := core.NewRedactionRegistry()
	registry.RegisterPattern("hc034_test_subsystem", []*regexp.Regexp{hc034FixtureAnthropicPattern})

	bus := eventbus.NewBusImplWithWriter(registry, writer)
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

	// Payload: "provider_key" is a benign field name but carries a secret-shaped value.
	// HC-031 MUST NOT redact it (name is safe); HC-032 MUST redact it (value matches).
	payload, marshalErr := json.Marshal(map[string]any{
		"provider_key": hc034FixtureAnthropicKeyStub,
		"node_id":      "hc034-node-hc032",
	})
	if marshalErr != nil {
		t.Fatalf("json.Marshal: %v", marshalErr)
	}

	if emitErr := bus.Emit(context.Background(), hc034FixtureEventType, payload); emitErr != nil {
		t.Fatalf("Emit: %v", emitErr)
	}

	lines := hc034FixtureReadJSONL(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("JSONL file has %d lines, want 1", len(lines))
	}

	line := lines[0]

	// HC-034: the raw Anthropic key stub MUST NOT appear in the raw JSONL bytes.
	if strings.Contains(line, hc034FixtureAnthropicKeyStub) {
		t.Errorf(
			"HC-034 VIOLATED (HC-032 path): persisted JSONL raw bytes contain the secret value %q;\n"+
				"  want: value replaced by %q before JSONL append\n"+
				"  spec: specs/handler-contract.md §4.7.HC-034 — no secret value MAY appear in "+
				"any persisted event record",
			hc034FixtureAnthropicKeyStub, core.RedactedSentinel,
		)
	}

	// Decode and check the stored value in the payload.
	got := hc034FixtureDecodePayload(t, line)

	providerVal, providerOK := got["provider_key"]
	if !providerOK {
		t.Error("HC-034: decoded payload missing 'provider_key' key; redaction MUST preserve the key")
	} else if providerVal != core.RedactedSentinel {
		t.Errorf(
			"HC-034 VIOLATED (HC-032 path): decoded payload[\"provider_key\"] = %q, want %q;\n"+
				"  HC-032 value-pattern redaction MUST fire before JSONL append\n"+
				"  spec: specs/handler-contract.md §4.7.HC-034, §4.7.HC-032",
			providerVal, core.RedactedSentinel,
		)
	}

	// Safe field MUST pass through unchanged.
	if nodeVal, nodeOK := got["node_id"]; !nodeOK {
		t.Error("HC-034: decoded payload missing safe field 'node_id'")
	} else if nodeVal != "hc034-node-hc032" {
		t.Errorf("HC-034: decoded payload[\"node_id\"] = %v, want %q; safe fields MUST NOT be redacted", nodeVal, "hc034-node-hc032")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-034 test 3: composed HC-031 + HC-032 path
// ─────────────────────────────────────────────────────────────────────────────

// TestHC034_BothHC031AndHC032SecretAbsentFromJSONL is the composed redaction
// path of the HC-034 end-to-end sensor (hk-8i31.41).
//
// Contract under test:
// A payload carrying BOTH a secret-named field (HC-031) and a benign-named
// field with a secret-shaped value (HC-032) MUST have BOTH values redacted in
// the persisted JSONL.  Safe fields MUST pass through unchanged.
//
// Spec refs: specs/handler-contract.md §4.7.HC-034, §4.7.HC-031, §4.7.HC-032.
// Bead ref: hk-8i31.41.
func TestHC034_BothHC031AndHC032SecretAbsentFromJSONL(t *testing.T) {
	t.Parallel()

	logPath := hc034FixtureJSONLPath(t)
	writer, err := eventbus.OpenJSONLWriter(logPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer func() { _ = writer.Close() }()

	// Registry with Anthropic key pattern (HC-032 path).
	registry := core.NewRedactionRegistry()
	registry.RegisterPattern("hc034_composed_subsystem", []*regexp.Regexp{hc034FixtureAnthropicPattern})

	bus := eventbus.NewBusImplWithWriter(registry, writer)
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

	// Payload carries:
	//   - "token": name matches HC-031 (independent of value)
	//   - "provider_key": benign name, value matches HC-032 Anthropic pattern
	//   - "node_id": safe field (MUST NOT be redacted)
	payload, marshalErr := json.Marshal(map[string]any{
		"token":        hc034FixtureSecretFieldValue,
		"provider_key": hc034FixtureAnthropicKeyStub,
		"node_id":      "hc034-node-composed",
	})
	if marshalErr != nil {
		t.Fatalf("json.Marshal: %v", marshalErr)
	}

	if emitErr := bus.Emit(context.Background(), hc034FixtureEventType, payload); emitErr != nil {
		t.Fatalf("Emit: %v", emitErr)
	}

	lines := hc034FixtureReadJSONL(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("JSONL file has %d lines, want 1", len(lines))
	}

	line := lines[0]

	// HC-034 / HC-031: raw value of "token" field MUST NOT appear in JSONL bytes.
	if strings.Contains(line, hc034FixtureSecretFieldValue) {
		t.Errorf(
			"HC-034 VIOLATED (HC-031 path, composed): raw JSONL bytes contain secret %q;\n"+
				"  want: value replaced by %q\n"+
				"  spec: specs/handler-contract.md §4.7.HC-034",
			hc034FixtureSecretFieldValue, core.RedactedSentinel,
		)
	}

	// HC-034 / HC-032: raw Anthropic key stub MUST NOT appear in JSONL bytes.
	if strings.Contains(line, hc034FixtureAnthropicKeyStub) {
		t.Errorf(
			"HC-034 VIOLATED (HC-032 path, composed): raw JSONL bytes contain secret %q;\n"+
				"  want: value replaced by %q\n"+
				"  spec: specs/handler-contract.md §4.7.HC-034",
			hc034FixtureAnthropicKeyStub, core.RedactedSentinel,
		)
	}

	// Decode and check decoded payload values.
	got := hc034FixtureDecodePayload(t, line)

	// "token" (HC-031): MUST be sentinel.
	if tokenVal, tokenOK := got["token"]; !tokenOK {
		t.Error("HC-034 composed: decoded payload missing 'token' key")
	} else if tokenVal != core.RedactedSentinel {
		t.Errorf(
			"HC-034 VIOLATED (HC-031, composed): decoded payload[\"token\"] = %q, want %q",
			tokenVal, core.RedactedSentinel,
		)
	}

	// "provider_key" (HC-032): MUST be sentinel.
	if providerVal, providerOK := got["provider_key"]; !providerOK {
		t.Error("HC-034 composed: decoded payload missing 'provider_key' key")
	} else if providerVal != core.RedactedSentinel {
		t.Errorf(
			"HC-034 VIOLATED (HC-032, composed): decoded payload[\"provider_key\"] = %q, want %q",
			providerVal, core.RedactedSentinel,
		)
	}

	// Safe field MUST pass through unchanged.
	if nodeVal, nodeOK := got["node_id"]; !nodeOK {
		t.Error("HC-034 composed: decoded payload missing safe field 'node_id'")
	} else if nodeVal != "hc034-node-composed" {
		t.Errorf("HC-034 composed: decoded payload[\"node_id\"] = %v, want %q", nodeVal, "hc034-node-composed")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-034 test 4: no over-redaction — safe fields preserved in JSONL
// ─────────────────────────────────────────────────────────────────────────────

// TestHC034_SafeFieldsPreservedInJSONL verifies that the redaction pipeline
// does NOT suppress or alter fields whose names and values are safe.
//
// This is the negative (control) case for HC-034: if over-redaction were
// present, safe operational fields (node_id, run_id, status) would be silently
// lost from the event log, making operator debugging impossible.
//
// Spec ref: specs/handler-contract.md §4.7.HC-031 (no over-redaction), §4.7.HC-034.
// Bead ref: hk-8i31.41.
func TestHC034_SafeFieldsPreservedInJSONL(t *testing.T) {
	t.Parallel()

	logPath := hc034FixtureJSONLPath(t)
	writer, err := eventbus.OpenJSONLWriter(logPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer func() { _ = writer.Close() }()

	// Registry with Anthropic pattern — neither field name nor value matches here.
	registry := core.NewRedactionRegistry()
	registry.RegisterPattern("hc034_safe_subsystem", []*regexp.Regexp{hc034FixtureAnthropicPattern})

	bus := eventbus.NewBusImplWithWriter(registry, writer)
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

	safePayload := map[string]any{
		"node_id":    "node-hc034-safe-001",
		"run_id":     "run-hc034-safe-456",
		"status":     "RUNNING",
		"exit_code":  "0",
		"agent_type": "claude",
	}
	payload, marshalErr := json.Marshal(safePayload)
	if marshalErr != nil {
		t.Fatalf("json.Marshal: %v", marshalErr)
	}

	if emitErr := bus.Emit(context.Background(), hc034FixtureEventType, payload); emitErr != nil {
		t.Fatalf("Emit: %v", emitErr)
	}

	lines := hc034FixtureReadJSONL(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("JSONL file has %d lines, want 1", len(lines))
	}

	line := lines[0]
	got := hc034FixtureDecodePayload(t, line)

	// Every safe field MUST be preserved verbatim.
	for k, want := range safePayload {
		k, want := k, want
		t.Run(k, func(t *testing.T) {
			t.Parallel()
			v, exists := got[k]
			if !exists {
				t.Errorf("HC-034 over-redaction: JSONL payload missing safe field %q;\n"+
					"  safe fields MUST NOT be redacted (HC-031 no over-redaction)", k)
				return
			}
			// JSON numbers round-trip as float64; compare as fmt.Sprint for robustness.
			if fmt.Sprint(v) != fmt.Sprint(want) {
				t.Errorf("HC-034 over-redaction: JSONL payload[%q] = %v, want %v;\n"+
					"  safe field value MUST be preserved verbatim", k, v, want)
			}
		})
	}

	// The redaction sentinel MUST NOT appear in the decoded payload values.
	for k, v := range got {
		if fmt.Sprint(v) == core.RedactedSentinel {
			t.Errorf(
				"HC-034 over-redaction: decoded payload[%q] = %q for a safe-only payload;\n"+
					"  the redaction pipeline MUST NOT redact fields whose names and values are safe",
				k, core.RedactedSentinel,
			)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-034 test 5: EmitWithRunID path — secret absent from run-stamped JSONL
// ─────────────────────────────────────────────────────────────────────────────

// TestHC034_EmitWithRunID_SecretAbsentFromJSONL verifies that the EmitWithRunID
// code path also redacts secret-named payload fields before JSONL append.
//
// EmitWithRunID follows a parallel code path to Emit (it stamps run_id on the
// EV-001 envelope before JSONL append).  HC-034 applies equally: the payload
// MUST be redacted before it reaches disk, regardless of whether a run_id is
// present.
//
// Spec refs: specs/handler-contract.md §4.7.HC-034, §4.7.HC-031;
// specs/event-model.md §6.1 EV-001.
// Bead ref: hk-8i31.41.
func TestHC034_EmitWithRunID_SecretAbsentFromJSONL(t *testing.T) {
	t.Parallel()

	logPath := hc034FixtureJSONLPath(t)
	writer, err := eventbus.OpenJSONLWriter(logPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	defer func() { _ = writer.Close() }()

	// No HC-032 patterns — HC-031 field-name redaction only.
	bus := eventbus.NewBusImplWithWriter(nil, writer)
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

	runID := hc034FixtureNewRunID(t)

	// Payload with a secret-named field ("password") and a safe field.
	payload, marshalErr := json.Marshal(map[string]any{
		"password": hc034FixtureSecretFieldValue,
		"node_id":  "hc034-node-runid",
	})
	if marshalErr != nil {
		t.Fatalf("json.Marshal: %v", marshalErr)
	}

	if emitErr := bus.EmitWithRunID(context.Background(), runID, hc034FixtureRunEventType, payload); emitErr != nil {
		t.Fatalf("EmitWithRunID: %v", emitErr)
	}

	lines := hc034FixtureReadJSONL(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("JSONL file has %d lines, want 1", len(lines))
	}

	line := lines[0]

	// HC-034: raw secret MUST NOT appear in the run-stamped JSONL raw bytes.
	if strings.Contains(line, hc034FixtureSecretFieldValue) {
		t.Errorf(
			"HC-034 VIOLATED (EmitWithRunID path): raw JSONL bytes contain secret %q;\n"+
				"  EmitWithRunID MUST apply the same HC-031 redaction pipeline as Emit\n"+
				"  spec: specs/handler-contract.md §4.7.HC-034",
			hc034FixtureSecretFieldValue,
		)
	}

	// Decode and check payload values.
	got := hc034FixtureDecodePayload(t, line)

	// "password" (HC-031): MUST be sentinel.
	if pwdVal, pwdOK := got["password"]; !pwdOK {
		t.Error("HC-034 EmitWithRunID: decoded payload missing 'password' key; redaction MUST preserve the key")
	} else if pwdVal != core.RedactedSentinel {
		t.Errorf(
			"HC-034 VIOLATED (EmitWithRunID path): decoded payload[\"password\"] = %q, want %q;\n"+
				"  HC-031 MUST redact the 'password' field via EmitWithRunID\n"+
				"  spec: specs/handler-contract.md §4.7.HC-034",
			pwdVal, core.RedactedSentinel,
		)
	}

	// The run_id MUST be present in the envelope (EmitWithRunID contract).
	if !strings.Contains(line, runID.String()) {
		t.Errorf(
			"HC-034 (EmitWithRunID path): JSONL envelope missing run_id %q;\n"+
				"  EmitWithRunID MUST stamp run_id on the EV-001 envelope (EV-001 / EM-013)",
			runID.String(),
		)
	}

	// Safe field MUST pass through unchanged.
	if nodeVal, nodeOK := got["node_id"]; !nodeOK {
		t.Error("HC-034 EmitWithRunID: decoded payload missing safe field 'node_id'")
	} else if nodeVal != "hc034-node-runid" {
		t.Errorf("HC-034 EmitWithRunID: decoded payload[\"node_id\"] = %v, want %q", nodeVal, "hc034-node-runid")
	}
}
