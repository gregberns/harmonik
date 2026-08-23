package eventbus_test

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

const hc034FixtureEventType core.EventType = "test.hc034.v1"

const hc034FixtureRunEventType core.EventType = "run_started"

var hc034FixtureAnthropicPattern = regexp.MustCompile(`^sk-ant-[A-Za-z0-9_\-]{10,}$`)

const hc034FixtureAnthropicKeyStub = "sk-ant-" +
	"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

const hc034FixtureRedactedFieldValue = "hc034-redacted-field-value"

func hc034FixtureJSONLPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "events.jsonl")
}

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

func hc034FixtureNewRunID(t *testing.T) core.RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hc034FixtureNewRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(id)
}

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
	defer eventbusFixtureClose(t, writer)

	bus := eventbus.NewBusImplWithWriter(nil, writer)
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

	payload, marshalErr := json.Marshal(map[string]any{
		"token":   hc034FixtureRedactedFieldValue,
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

	if strings.Contains(line, hc034FixtureRedactedFieldValue) {
		t.Errorf(
			"HC-034 VIOLATED (HC-031 path): persisted JSONL raw bytes contain the secret value %q;\n"+
				"  want: value replaced by %q before JSONL append\n"+
				"  spec: specs/handler-contract.md §4.7.HC-034 — no secret value MAY appear in "+
				"any persisted event record",
			hc034FixtureRedactedFieldValue, core.RedactedSentinel,
		)
	}

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

	if nodeVal, nodeOK := got["node_id"]; !nodeOK {
		t.Error("HC-034: decoded payload missing safe field 'node_id'")
	} else if nodeVal != "hc034-node-hc031" {
		t.Errorf("HC-034: decoded payload[\"node_id\"] = %v, want %q; safe fields MUST NOT be redacted", nodeVal, "hc034-node-hc031")
	}
}

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
	defer eventbusFixtureClose(t, writer)

	registry := core.NewRedactionRegistry()
	registry.RegisterPattern("hc034_test_subsystem", []*regexp.Regexp{hc034FixtureAnthropicPattern})

	bus := eventbus.NewBusImplWithWriter(registry, writer)
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

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

	if strings.Contains(line, hc034FixtureAnthropicKeyStub) {
		t.Errorf(
			"HC-034 VIOLATED (HC-032 path): persisted JSONL raw bytes contain the secret value %q;\n"+
				"  want: value replaced by %q before JSONL append\n"+
				"  spec: specs/handler-contract.md §4.7.HC-034 — no secret value MAY appear in "+
				"any persisted event record",
			hc034FixtureAnthropicKeyStub, core.RedactedSentinel,
		)
	}

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

	if nodeVal, nodeOK := got["node_id"]; !nodeOK {
		t.Error("HC-034: decoded payload missing safe field 'node_id'")
	} else if nodeVal != "hc034-node-hc032" {
		t.Errorf("HC-034: decoded payload[\"node_id\"] = %v, want %q; safe fields MUST NOT be redacted", nodeVal, "hc034-node-hc032")
	}
}

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
	defer eventbusFixtureClose(t, writer)

	registry := core.NewRedactionRegistry()
	registry.RegisterPattern("hc034_composed_subsystem", []*regexp.Regexp{hc034FixtureAnthropicPattern})

	bus := eventbus.NewBusImplWithWriter(registry, writer)
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

	payload, marshalErr := json.Marshal(map[string]any{
		"token":        hc034FixtureRedactedFieldValue,
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

	if strings.Contains(line, hc034FixtureRedactedFieldValue) {
		t.Errorf(
			"HC-034 VIOLATED (HC-031 path, composed): raw JSONL bytes contain secret %q;\n"+
				"  want: value replaced by %q\n"+
				"  spec: specs/handler-contract.md §4.7.HC-034",
			hc034FixtureRedactedFieldValue, core.RedactedSentinel,
		)
	}

	if strings.Contains(line, hc034FixtureAnthropicKeyStub) {
		t.Errorf(
			"HC-034 VIOLATED (HC-032 path, composed): raw JSONL bytes contain secret %q;\n"+
				"  want: value replaced by %q\n"+
				"  spec: specs/handler-contract.md §4.7.HC-034",
			hc034FixtureAnthropicKeyStub, core.RedactedSentinel,
		)
	}

	got := hc034FixtureDecodePayload(t, line)

	if tokenVal, tokenOK := got["token"]; !tokenOK {
		t.Error("HC-034 composed: decoded payload missing 'token' key")
	} else if tokenVal != core.RedactedSentinel {
		t.Errorf(
			"HC-034 VIOLATED (HC-031, composed): decoded payload[\"token\"] = %q, want %q",
			tokenVal, core.RedactedSentinel,
		)
	}

	if providerVal, providerOK := got["provider_key"]; !providerOK {
		t.Error("HC-034 composed: decoded payload missing 'provider_key' key")
	} else if providerVal != core.RedactedSentinel {
		t.Errorf(
			"HC-034 VIOLATED (HC-032, composed): decoded payload[\"provider_key\"] = %q, want %q",
			providerVal, core.RedactedSentinel,
		)
	}

	if nodeVal, nodeOK := got["node_id"]; !nodeOK {
		t.Error("HC-034 composed: decoded payload missing safe field 'node_id'")
	} else if nodeVal != "hc034-node-composed" {
		t.Errorf("HC-034 composed: decoded payload[\"node_id\"] = %v, want %q", nodeVal, "hc034-node-composed")
	}
}

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
	defer eventbusFixtureClose(t, writer)

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

	for k, want := range safePayload {
		t.Run(k, func(t *testing.T) {
			t.Parallel()
			v, exists := got[k]
			if !exists {
				t.Errorf("HC-034 over-redaction: JSONL payload missing safe field %q;\n"+
					"  safe fields MUST NOT be redacted (HC-031 no over-redaction)", k)
				return
			}
			if fmt.Sprint(v) != fmt.Sprint(want) {
				t.Errorf("HC-034 over-redaction: JSONL payload[%q] = %v, want %v;\n"+
					"  safe field value MUST be preserved verbatim", k, v, want)
			}
		})
	}

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
	defer eventbusFixtureClose(t, writer)

	bus := eventbus.NewBusImplWithWriter(nil, writer)
	if sealErr := bus.Seal(); sealErr != nil {
		t.Fatalf("Seal: %v", sealErr)
	}

	runID := hc034FixtureNewRunID(t)

	payload, marshalErr := json.Marshal(map[string]any{
		"password": hc034FixtureRedactedFieldValue,
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

	if strings.Contains(line, hc034FixtureRedactedFieldValue) {
		t.Errorf(
			"HC-034 VIOLATED (EmitWithRunID path): raw JSONL bytes contain secret %q;\n"+
				"  EmitWithRunID MUST apply the same HC-031 redaction pipeline as Emit\n"+
				"  spec: specs/handler-contract.md §4.7.HC-034",
			hc034FixtureRedactedFieldValue,
		)
	}

	got := hc034FixtureDecodePayload(t, line)

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

	if !strings.Contains(line, runID.String()) {
		t.Errorf(
			"HC-034 (EmitWithRunID path): JSONL envelope missing run_id %q;\n"+
				"  EmitWithRunID MUST stamp run_id on the EV-001 envelope (EV-001 / EM-013)",
			runID.String(),
		)
	}

	if nodeVal, nodeOK := got["node_id"]; !nodeOK {
		t.Error("HC-034 EmitWithRunID: decoded payload missing safe field 'node_id'")
	} else if nodeVal != "hc034-node-runid" {
		t.Errorf("HC-034 EmitWithRunID: decoded payload[\"node_id\"] = %v, want %q", nodeVal, "hc034-node-runid")
	}
}
