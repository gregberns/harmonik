package main

// NDJSON wire-protocol fixture tests (hk-8i31.78).
//
// Six scenarios per §10.2 (HC-005..HC-010 + HC-007a + HC-007b):
//
//  1. Well-formed message sequence — emitter produces valid NDJSON.
//  2. Embedded-newline-rejection — emitter never embeds unescaped newlines.
//  3. 1-MiB line-cap rejection — wireReader rejects lines exceeding 1 MiB.
//  4. Partial-message-on-EOF discard — wireReader discards incomplete lines.
//  5. Malformed-JSON close-with-ErrStructural — wireReader surfaces unmarshal error.
//  6. LaunchSpec round-trip — JSON encoding survives both stdin and file-path delivery.
//
// Cite: specs/handler-contract.md §10.2, §4.2.HC-005, §4.2.HC-007a, §4.2.HC-007b.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wireFixtureLaunchSpec is the minimal JSON-serialisable LaunchSpec payload used
// across round-trip tests.  Fields match the RECORD LaunchSpec shape declared in
// specs/handler-contract.md §6.1.  A concrete Go type does not yet exist
// (deferred); map[string]any is used here so the fixture exercises the JSON
// serialisation boundary without coupling to an unimplemented struct.
//
// TODO: replace map[string]any with *handlercontract.LaunchSpec once
// hk-8i31.6 (LaunchSpec record) lands.
func wireFixtureLaunchSpec() map[string]any {
	return map[string]any{
		"run_id":               "run-fixture-001",
		"workflow_id":          "wf-fixture-001",
		"node_id":              "node-fixture-001",
		"agent_type":           "claude-twin",
		"workspace_path":       "/workspace/fixture",
		"required_skills":      []string{},
		"skill_search_paths":   []string{"/skills"},
		"timeout":              60,
		"provisioning_timeout": 30,
		"budget":               "default",
		"freedom_profile_ref":  "standard",
		"schema_version":       1,
	}
}

// wireFixtureEncodeJSON encodes v as compact JSON and calls t.Fatalf on error.
func wireFixtureEncodeJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("wireFixtureEncodeJSON: marshal: %v", err)
	}
	return b
}

// wireFixtureDecodeJSON decodes b into a map[string]any and calls t.Fatalf on error.
func wireFixtureDecodeJSON(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("wireFixtureDecodeJSON: unmarshal: %v", err)
	}
	return m
}

// wireFixtureWriteTempFile writes content to a new file in t.TempDir() and
// returns its absolute path.
//
//nolint:gosec // G306: 0o600 is the correct mode for LaunchSpec files (daemon-written, operator-readable)
func wireFixtureWriteTempFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatalf("wireFixtureWriteTempFile: %v", err)
	}
	return p
}

// ────────────────────────────────────────────────────────────────────────────
// Scenario 1 — Well-formed message sequence (HC-007a positive case)
// ────────────────────────────────────────────────────────────────────────────

// TestWireFixtureWellFormedSequence verifies that a canonical handler_capabilities →
// session_log_location → outcome_emitted sequence produces valid NDJSON: each
// message is a complete JSON object terminated by exactly one 0x0A byte, with
// no embedded unescaped newlines (HC-007a).
func TestWireFixtureWellFormedSequence(t *testing.T) {
	var buf bytes.Buffer
	e := newWireEmitter(&buf)

	if err := e.emitHandlerCapabilities("run-001", "sess-001", []int{1}); err != nil {
		t.Fatalf("emitHandlerCapabilities: %v", err)
	}
	if err := e.emitSessionLogLocation(
		"run-001", "sess-001", "node-001", "claude-twin",
		"/tmp/session.log", "ndjson", nil,
	); err != nil {
		t.Fatalf("emitSessionLogLocation: %v", err)
	}
	if err := e.emitOutcomeEmitted("run-001", "sess-001", "node-001", "success"); err != nil {
		t.Fatalf("emitOutcomeEmitted: %v", err)
	}

	raw := buf.Bytes()

	// The buffer must end with exactly one 0x0A.
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		t.Fatalf("buffer does not end with 0x0A; last byte = %#x", raw[len(raw)-1])
	}

	// Split on newline; last element is "" due to trailing \n.
	lines := bytes.Split(bytes.TrimRight(raw, "\n"), []byte("\n"))
	if len(lines) != 3 {
		t.Fatalf("expected 3 NDJSON lines, got %d", len(lines))
	}

	wantTypes := []string{"handler_capabilities", "session_log_location", "outcome_emitted"}
	for i, line := range lines {
		// Each line must be a valid JSON object.
		var obj map[string]any
		if err := json.Unmarshal(line, &obj); err != nil {
			t.Errorf("line %d is not valid JSON: %v — %q", i, err, string(line))
			continue
		}
		// No embedded unescaped newline (HC-007a).
		if bytes.Contains(line, []byte{'\n'}) {
			t.Errorf("line %d contains embedded newline (HC-007a violation)", i)
		}
		// Type field must match expected order.
		gotType, ok := obj["type"].(string)
		if !ok {
			t.Errorf("line %d: missing or non-string 'type' field", i)
			continue
		}
		if gotType != wantTypes[i] {
			t.Errorf("line %d: type = %q, want %q", i, gotType, wantTypes[i])
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Scenario 2 — Embedded-newline-rejection (HC-007a)
// ────────────────────────────────────────────────────────────────────────────

// TestWireFixtureEmbeddedNewlineRejection verifies that the wireEmitter never
// embeds a raw 0x0A byte inside a JSON object, even when the input string
// contains a newline character (HC-007a: "no embedded unescaped newlines
// appear inside a JSON object").
//
// json.Marshal escapes \n as the two-character sequence \n (0x5C 0x6E), so the
// emitter is conformant by construction; this test makes that invariant visible
// and detectable.
func TestWireFixtureEmbeddedNewlineRejection(t *testing.T) {
	var buf bytes.Buffer
	e := newWireEmitter(&buf)

	// Emit a chunk with a newline character embedded in a string field (e.g. a
	// log excerpt) to exercise the JSON escaping path.
	if err := e.emitAgentOutputChunk("run-1", "sess-1\nmalicious", 0, 64, nil); err != nil {
		t.Fatalf("emitAgentOutputChunk: %v", err)
	}

	raw := buf.Bytes()

	// The full buffer must contain exactly ONE newline (the NDJSON terminator).
	newlines := bytes.Count(raw, []byte{'\n'})
	if newlines != 1 {
		t.Errorf("emitted bytes contain %d newlines, want exactly 1 (HC-007a); raw=%q", newlines, raw)
	}

	// The single newline must be the last byte.
	if raw[len(raw)-1] != '\n' {
		t.Errorf("trailing byte = %#x, want 0x0A (HC-007a)", raw[len(raw)-1])
	}

	// The JSON body (everything before the trailing \n) must be parseable.
	body := raw[:len(raw)-1]
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Errorf("JSON body is not valid: %v — %q", err, string(body))
	}

	// session_id must be present and contain the escaped newline as a Go string
	// (i.e. json.Unmarshal turns \n back into the newline character in-memory).
	sidRaw, ok := obj["session_id"].(string)
	if !ok {
		t.Fatalf("session_id missing or not string: %v", obj["session_id"])
	}
	if !strings.Contains(sidRaw, "\n") {
		t.Errorf("session_id in decoded map should contain newline char; got %q", sidRaw)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Scenario 3 — 1-MiB line-cap rejection (HC-007a)
// ────────────────────────────────────────────────────────────────────────────

// TestWireFixtureLineCap1MiBRejection verifies that wireReader rejects a line
// that exceeds the 1-MiB cap declared in HC-007a.
//
// Mechanism: bufio.Scanner returns bufio.ErrTooLong when the token exceeds
// the configured max buffer size; wireReader surfaces this as a scan error
// (not io.EOF).  The daemon-side watcher maps this error to ErrProtocolMismatch
// with sub_reason "ndjson_line_too_long" per §8.7.
func TestWireFixtureLineCap1MiBRejection(t *testing.T) {
	const oneMiB = 1 << 20

	// Build a line that just exceeds 1 MiB when including the terminating \n.
	// Content is a JSON-like payload padded with 'x' characters; it need not
	// be valid JSON — the scanner cap fires before unmarshal.
	oversize := make([]byte, oneMiB+1)
	oversize[0] = '{'
	for i := 1; i < len(oversize)-2; i++ {
		oversize[i] = 'x'
	}
	oversize[len(oversize)-2] = '}'
	oversize[len(oversize)-1] = '\n'

	r := newWireReader(bytes.NewReader(oversize))
	_, err := r.readControlMsg()

	if err == nil {
		t.Fatal("readControlMsg with >1 MiB line returned nil error; want scan error (HC-007a)")
	}
	if errors.Is(err, io.EOF) {
		t.Errorf("readControlMsg returned io.EOF for oversized line; want scan error, not EOF (HC-007a)")
	}
	// The error must wrap bufio.ErrTooLong, confirming the line-cap path.
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Errorf("readControlMsg error = %v; want to wrap bufio.ErrTooLong (HC-007a cap)", err)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Scenario 4 — Partial-message-on-EOF discard (HC-007b)
// ────────────────────────────────────────────────────────────────────────────

// TestWireFixturePartialMessageOnEOFDiscard documents the HC-007b partial-message
// boundary at stream close for the handler-to-daemon (progress-stream) direction.
//
// HC-007b normative rule: "partial messages at stream close — socket EOF with
// bytes buffered before the terminating \n — MUST be discarded; the watcher
// MUST emit agent_failed with class ErrStructural, sub_reason partial-message".
//
// This rule is enforced by the daemon-side watcher (not the twin binary's
// wireReader, which reads the reverse daemon-to-handler direction).  This
// fixture tests the contractual surface from the emitter side: the wireEmitter
// always appends a terminating \n, guaranteeing that complete messages are
// delivered; a reader that reads the full emitter output never sees a partial
// frame.
//
// The fixture also tests that when a stream presents EOF immediately after a
// complete NDJSON line (i.e., one message followed by EOF with no further
// data), the wireReader returns the message cleanly and then io.EOF on the
// next call — confirming the "nothing to discard" case.
func TestWireFixturePartialMessageOnEOFDiscard(t *testing.T) {
	// Complete NDJSON line with terminating \n followed immediately by EOF.
	// The wireEmitter always produces this shape; the test confirms that no
	// ghost discard occurs when the stream is correctly framed.
	complete := []byte(`{"type":"version_selected","selected_version":1}` + "\n")

	r := newWireReader(bytes.NewReader(complete))

	// First call: must decode the complete message.
	msg, err := r.readControlMsg()
	if err != nil {
		t.Fatalf("readControlMsg complete line: unexpected error %v", err)
	}
	if msg == nil {
		t.Fatal("readControlMsg complete line: got nil message, want decoded msg")
	}
	if msg.Type != "version_selected" {
		t.Errorf("message type = %q, want version_selected", msg.Type)
	}

	// Second call: stream is exhausted; must return io.EOF (nothing to discard).
	_, err = r.readControlMsg()
	if !errors.Is(err, io.EOF) {
		t.Errorf("readControlMsg after last message: got %v, want io.EOF", err)
	}

	// Emitter-side guarantee: wireEmitter always appends \n, so a partial
	// frame (HC-007b discard scenario) can only arise from a bug in the
	// emitter or an abnormal socket close.  Verify the emitter guarantee:
	var buf bytes.Buffer
	e := newWireEmitter(&buf)
	if err2 := e.emitAgentHeartbeat("sess-partial-test", heartbeatPhaseStarting); err2 != nil {
		t.Fatalf("emitAgentHeartbeat: %v", err2)
	}
	raw := buf.Bytes()
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		t.Errorf("wireEmitter output does not end with 0x0A; HC-007b partial-discard scenario is prevented by the emitter guarantee")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Scenario 5 — Malformed-JSON close-with-ErrStructural (HC-007b)
// ────────────────────────────────────────────────────────────────────────────

// TestWireFixtureMalformedJSONClosesWithError verifies that wireReader surfaces
// a non-nil, non-EOF error when a complete NDJSON line contains syntactically
// invalid JSON (HC-007b: "a decoder error on a malformed JSON object MUST be
// discarded; the watcher MUST emit agent_failed with class ErrStructural,
// sub_reason malformed_progress_message").
//
// wireReader returns the unmarshal error directly; the daemon-side watcher is
// responsible for mapping it to ErrStructural and emitting agent_failed.  This
// test exercises the wireReader's detection side of that contract.
func TestWireFixtureMalformedJSONClosesWithError(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{
			name: "truncated_object",
			line: `{"type":"cancel"` + "\n", // missing closing brace
		},
		{
			name: "bare_string",
			line: `"not_an_object"` + "\n",
		},
		{
			name: "invalid_unicode_escape",
			line: `{"type":"test","val":"\uXXXX"}` + "\n",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r := newWireReader(strings.NewReader(tc.line))
			msg, err := r.readControlMsg()

			if msg != nil {
				t.Errorf("readControlMsg(%q): returned non-nil message %+v, want nil on parse error", tc.name, msg)
			}
			if err == nil {
				t.Fatalf("readControlMsg(%q): expected non-nil error for malformed JSON, got nil", tc.name)
			}
			if errors.Is(err, io.EOF) {
				t.Errorf("readControlMsg(%q): got io.EOF, want unmarshal error (HC-007b)", tc.name)
			}
		})
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Scenario 6 — LaunchSpec round-trip: stdin and file-path delivery modes (HC-005)
// ────────────────────────────────────────────────────────────────────────────

// TestWireFixtureLaunchSpecStdinRoundTrip verifies that a LaunchSpec can be
// serialised to JSON, delivered via a simulated stdin byte stream (io.Reader),
// and decoded back with all required fields intact (HC-005 stdin delivery mode,
// HC-006 field conformance).
//
// A concrete LaunchSpec Go type does not yet exist (pending hk-8i31.6); this
// fixture exercises the JSON boundary using map[string]any so the serialisation
// contract is validated without coupling to an unimplemented struct.
func TestWireFixtureLaunchSpecStdinRoundTrip(t *testing.T) {
	spec := wireFixtureLaunchSpec()
	encoded := wireFixtureEncodeJSON(t, spec)

	// Simulate stdin: wrap encoded JSON in a bytes.Reader (io.Reader).
	r := bytes.NewReader(encoded)

	// Read back: json.NewDecoder simulates how the handler reads from stdin.
	var decoded map[string]any
	if err := json.NewDecoder(r).Decode(&decoded); err != nil {
		t.Fatalf("stdin round-trip decode: %v", err)
	}

	wireFixtureAssertLaunchSpecFields(t, decoded)
}

// TestWireFixtureLaunchSpecFilePathRoundTrip verifies that a LaunchSpec can be
// serialised to JSON, written to a temp file, read back via the file-path
// delivery mechanism (HC-005 file-path form: --launch-spec <path>), and
// decoded with all required fields intact.
func TestWireFixtureLaunchSpecFilePathRoundTrip(t *testing.T) {
	spec := wireFixtureLaunchSpec()
	encoded := wireFixtureEncodeJSON(t, spec)

	// Simulate file-path delivery: write to temp file.
	p := wireFixtureWriteTempFile(t, "launch-spec.json", encoded)

	// Read back: simulate what the handler binary does with --launch-spec.
	//nolint:gosec // G304: path is test-controlled via t.TempDir(); provenance is this test
	content, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("file-path round-trip read: %v", err)
	}

	decoded := wireFixtureDecodeJSON(t, content)
	wireFixtureAssertLaunchSpecFields(t, decoded)
}

// wireFixtureAssertLaunchSpecFields validates that the required LaunchSpec
// fields per HC-006 and specs/handler-contract.md §6.1 are present and
// non-empty in decoded.
func wireFixtureAssertLaunchSpecFields(t *testing.T, decoded map[string]any) {
	t.Helper()

	requiredStrings := []string{
		"run_id", "workflow_id", "node_id", "agent_type",
		"workspace_path", "budget", "freedom_profile_ref",
	}
	for _, k := range requiredStrings {
		v, ok := decoded[k]
		if !ok {
			t.Errorf("LaunchSpec missing required field %q", k)
			continue
		}
		if s, ok := v.(string); !ok || s == "" {
			t.Errorf("LaunchSpec field %q = %v, want non-empty string", k, v)
		}
	}

	requiredArrays := []string{"required_skills", "skill_search_paths"}
	for _, k := range requiredArrays {
		if _, ok := decoded[k]; !ok {
			t.Errorf("LaunchSpec missing required array field %q", k)
		}
	}

	requiredNumerics := []string{"timeout", "provisioning_timeout", "schema_version"}
	for _, k := range requiredNumerics {
		v, ok := decoded[k]
		if !ok {
			t.Errorf("LaunchSpec missing required numeric field %q", k)
			continue
		}
		if _, ok := v.(float64); !ok {
			t.Errorf("LaunchSpec field %q = %v (%T), want number", k, v, v)
		}
	}
}
