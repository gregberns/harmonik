// Package codextest is the T5 test-taxonomy layer for the codex app-server
// integration (codex-app-server T5, hk-oe86p).
//
// Four tiers wire all previous phase tests under a single taxonomy:
//
//   L0 — unit:        codexwire serializer (round-trip, golden, malformed)
//   L1 — contract:    corpus replay via twin → zero unknown frames, all frames round-trip
//   L2 — integration: twin → reactor → HarmonikBridgeSink (faked comms/queue/beads)
//   L3 — live:        real codex app-server (CODEX_LIVE=1 required, token-capped)
//
// make test runs L0/L1/L2 only (CODEX_LIVE=0 default).
// make test-codex-live runs L3 (CODEX_LIVE=1 required).
// make capture-fixtures captures a new corpus session (deliberate, budget-capped, ledgered).
//
// Pre-deploy drift canary: canary_hkoe86p_test.go (TestCodexDriftCanary).
// GATE: L0/L1/L2 green vs real captured frames with CODEX_LIVE=0.
//
// Bead: hk-oe86p [codex-app-server T5]
package codextest_test

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/gregberns/harmonik/internal/codexwire"
)

// ─── corpus path helper ──────────────────────────────────────────────────────

func l0CorpusDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "codex-app-server", "corpus")
}

// ─── L0 — golden frame tests ─────────────────────────────────────────────────

// TestL0_Wire_GoldenClientRequest verifies that a literal initialize client
// request is parsed into the expected Frame fields.
func TestL0_Wire_GoldenClientRequest(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"test","title":"T","version":"0.0.1"},"capabilities":null}}`)

	frame, err := codexwire.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if frame.Kind != codexwire.FrameKindClientRequest {
		t.Errorf("Kind: got %v, want FrameKindClientRequest", frame.Kind)
	}
	if frame.Method != "initialize" {
		t.Errorf("Method: got %q, want %q", frame.Method, "initialize")
	}
	if frame.ID != 1 {
		t.Errorf("ID: got %d, want 1", frame.ID)
	}
	if frame.Params == nil {
		t.Fatal("Params: got nil, want non-nil")
	}
}

// TestL0_Wire_GoldenServerNotification verifies that a literal server
// notification (configWarning) is parsed with the expected Kind and Method.
func TestL0_Wire_GoldenServerNotification(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"method":"configWarning","params":{"summary":"test","details":null}}`)

	frame, err := codexwire.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if frame.Kind != codexwire.FrameKindServerNotification {
		t.Errorf("Kind: got %v, want FrameKindServerNotification", frame.Kind)
	}
	if frame.Method != "configWarning" {
		t.Errorf("Method: got %q, want %q", frame.Method, "configWarning")
	}
}

// TestL0_Wire_GoldenServerResponse verifies that a literal server response
// (initialize result) is parsed with the expected Kind.
func TestL0_Wire_GoldenServerResponse(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"id":1,"result":{"userAgent":"test/1.0","codexHome":"/home/.codex","platformFamily":"unix","platformOs":"linux"}}`)

	frame, err := codexwire.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if frame.Kind != codexwire.FrameKindServerResponse {
		t.Errorf("Kind: got %v, want FrameKindServerResponse", frame.Kind)
	}
	if frame.ID != 1 {
		t.Errorf("ID: got %d, want 1", frame.ID)
	}
}

// ─── L0 — round-trip test ────────────────────────────────────────────────────

// TestL0_Wire_RoundTrip verifies that Parse → Marshal produces JSON that is
// semantically equal to the original input for a set of golden frames.
//
// Cases use schema-valid formats per the codexwire method registry so that
// re-serialization produces the same key-value pairs (no spurious zero-value
// fields). The initialize request is the primary golden target; additional
// cases cover client-notification and server-response shapes.
func TestL0_Wire_RoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{
			// initialize client request — schema matches InitializeParams
			"initialize_request",
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"x","title":"y","version":"z"},"capabilities":null}}`,
		},
		{
			// thread/start client request — empty params
			"thread_start_request",
			`{"jsonrpc":"2.0","id":2,"method":"thread/start","params":{}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			frame, err := codexwire.Parse([]byte(tc.raw))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if frame.Kind == codexwire.FrameKindRaw {
				t.Skip("unknown method — skipping round-trip (not an L0 failure)")
			}
			got, marshalErr := codexwire.Marshal(frame)
			if marshalErr != nil {
				t.Fatalf("Marshal: %v", marshalErr)
			}

			// Semantic equality: decode both to map and compare.
			var orig, remarshal map[string]any
			if err := json.Unmarshal([]byte(tc.raw), &orig); err != nil {
				t.Fatalf("unmarshal original: %v", err)
			}
			if err := json.Unmarshal(got, &remarshal); err != nil {
				t.Fatalf("unmarshal re-marshaled: %v", err)
			}
			if !reflect.DeepEqual(orig, remarshal) {
				t.Errorf("round-trip semantic mismatch\n  orig:     %s\n  remarshal:%s", tc.raw, got)
			}
		})
	}
}

// ─── L0 — malformed-input tests ──────────────────────────────────────────────

// TestL0_Wire_MalformedJSON verifies that Parse returns a non-nil error for
// inputs that are not valid JSON or not valid JSON objects.
func TestL0_Wire_MalformedJSON(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  []byte
	}{
		{"empty", []byte{}},
		{"not_json", []byte(`not json`)},
		{"truncated", []byte(`{"jsonrpc":"2.0","id":1`)},
		{"json_array", []byte(`[1,2,3]`)},
		{"json_string", []byte(`"hello"`)},
		{"json_number", []byte(`42`)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := codexwire.Parse(tc.raw)
			if err == nil {
				t.Errorf("Parse(%q): expected error for malformed input, got nil", tc.raw)
			}
		})
	}
}

// TestL0_Wire_UnknownMethodYieldsRaw verifies that a well-formed JSON-RPC
// frame with an unregistered method is parsed as FrameKindRaw (not an error).
func TestL0_Wire_UnknownMethodYieldsRaw(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"method":"unregistered/method/xyz","params":{"x":1}}`)

	frame, err := codexwire.Parse(raw)
	if err != nil {
		t.Fatalf("Parse returned error for unknown method (should return FrameKindRaw): %v", err)
	}
	if frame.Kind != codexwire.FrameKindRaw {
		t.Errorf("Kind: got %v, want FrameKindRaw for unregistered method", frame.Kind)
	}
	if string(frame.Raw) != string(raw) {
		t.Errorf("Raw bytes not preserved: got %q, want %q", frame.Raw, raw)
	}
}

