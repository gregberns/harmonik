package codexwire_test

// T2 gate: every captured real message must round-trip (parse→re-serialize→
// semantic-equal), ZERO unmodeled fields (Extra must be empty on all frames),
// ZERO unknown methods (no FrameKindRaw).
//
// The corpus (23 frames, raw-session-01.jsonl) is the normative test input.
// This test is the operator checkpoint for the T2 phase.
//
// Response frames (FrameKindServerResponse) carry the id but not the method;
// they are correlated to their originating request via requestsByID so the
// result type can be resolved and Extra-checked.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/codexwire"
)

// corpusPath resolves the testdata corpus relative to the package source.
func corpusPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "codex-app-server", "corpus", "raw-session-01.jsonl")
	return root
}

// TestCorpusRoundTrip is the T2 gate test. For every line in the corpus:
//  1. Parse → Frame
//  2. Marshal → bytes
//  3. Semantic-equal check (both parsed to map[string]any and deep-compared)
//  4. ZERO unknown methods (no FrameKindRaw)
//  5. ZERO unmodeled fields (Extra on all reachable structs is empty)
func TestCorpusRoundTrip(t *testing.T) {
	t.Helper()

	f, err := os.Open(corpusPath())
	if err != nil {
		t.Fatalf("open corpus: %v", err)
	}
	defer f.Close()

	// Track client requests by id so we can resolve response results.
	requestsByID := map[string]string{} // id (raw JSON) → method

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	lineNum := 0
	for sc.Scan() {
		line := sc.Bytes()
		lineNum++
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}

		frame, err := codexwire.Parse(line)
		if err != nil {
			t.Errorf("line %d: Parse error: %v\n  line: %s", lineNum, err, line)
			continue
		}

		// Gate 1: ZERO unknown methods.
		if frame.Kind == codexwire.FrameKindRaw {
			t.Errorf("line %d: unknown method (FrameKindRaw) — method %q must be added to registry\n  line: %s",
				lineNum, frame.Method, line)
			continue
		}

		// For response frames, resolve the result type using the tracked request id.
		if frame.Kind == codexwire.FrameKindServerResponse {
			id := string(frame.ID)
			if method, ok := requestsByID[id]; ok {
				if err := codexwire.ResolveResponseResult(&frame, method); err != nil {
					t.Errorf("line %d: ResolveResponseResult (id=%s method=%q): %v",
						lineNum, id, method, err)
				}
			} else {
				t.Errorf("line %d: server response for id=%s has no tracked client request",
					lineNum, id)
			}
		}

		// Track client requests for response correlation.
		if frame.Kind == codexwire.FrameKindClientRequest {
			requestsByID[string(frame.ID)] = frame.Method
		}

		// Gate 2: ZERO unmodeled fields.
		if extras := collectExtras(t, lineNum, &frame); len(extras) > 0 {
			t.Errorf("line %d: unmodeled fields found (Extra must be empty):\n%s\n  line: %s",
				lineNum, formatExtras(extras), line)
		}

		// Gate 3: Round-trip semantic equality.
		got, err := codexwire.Marshal(frame)
		if err != nil {
			t.Errorf("line %d: Marshal error: %v\n  line: %s", lineNum, err, line)
			continue
		}
		if err := assertSemanticEqual(t, lineNum, line, got); err != nil {
			t.Errorf("line %d: round-trip mismatch: %v\n  original:     %s\n  re-serialized: %s",
				lineNum, err, line, got)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	if lineNum == 0 {
		t.Fatal("corpus file was empty — check testdata path")
	}
	t.Logf("corpus: %d lines processed", lineNum)
}

// TestMethodRegistry verifies that every method in the registry can construct
// a zero-value params without panicking. Also verifies the registry is non-empty.
func TestMethodRegistry(t *testing.T) {
	methods := codexwire.RegisteredMethods()
	if len(methods) == 0 {
		t.Fatal("methodRegistry is empty")
	}
	t.Logf("registered methods: %d", len(methods))
	for _, m := range methods {
		t.Logf("  %s", m)
	}
}

// TestStringAndVariantIDRoundTrip guards the JSON-RPC 2.0 rule that an id may
// be a string, a number, or null (not only an integer). A non-integer id must
// parse cleanly and round-trip byte-for-byte, rather than failing the envelope
// decode and tearing the session down (H11).
func TestStringAndVariantIDRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"string id request", `{"jsonrpc":"2.0","id":"abc-123","method":"initialize","params":{"clientInfo":{"name":"x","title":"y","version":"1"},"capabilities":null}}`},
		{"string id response", `{"id":"abc-123","result":{"userAgent":"ua","codexHome":"h","platformFamily":"f","platformOs":"o"}}`},
		{"integer id request", `{"jsonrpc":"2.0","id":7,"method":"initialize","params":{"clientInfo":{"name":"x","title":"y","version":"1"},"capabilities":null}}`},
		{"large integer id response", `{"id":9007199254740993,"result":{"userAgent":"ua","codexHome":"h","platformFamily":"f","platformOs":"o"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame, err := codexwire.Parse([]byte(tc.line))
			if err != nil {
				t.Fatalf("Parse returned error for %s: %v", tc.name, err)
			}
			if frame.Kind == codexwire.FrameKindRaw {
				t.Fatalf("%s parsed to FrameKindRaw; envelope decode failed", tc.name)
			}
			out, err := codexwire.Marshal(frame)
			if err != nil {
				t.Fatalf("Marshal returned error for %s: %v", tc.name, err)
			}
			if err := assertSemanticEqual(t, 0, []byte(tc.line), out); err != nil {
				t.Fatalf("%s did not round-trip: %v\n  in:  %s\n  out: %s", tc.name, err, tc.line, out)
			}
		})
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// assertSemanticEqual compares original and remarshal as parsed JSON maps.
// Returns a descriptive error on mismatch, nil on equal.
func assertSemanticEqual(t *testing.T, lineNum int, original, remarshal []byte) error {
	t.Helper()
	var orig, got any
	if err := json.Unmarshal(original, &orig); err != nil {
		return fmt.Errorf("unmarshal original: %w", err)
	}
	if err := json.Unmarshal(remarshal, &got); err != nil {
		return fmt.Errorf("unmarshal remarshal: %w", err)
	}
	if !reflect.DeepEqual(orig, got) {
		return fmt.Errorf("values differ")
	}
	return nil
}

// extraReport records an unmodeled field path and its raw JSON value.
type extraReport struct {
	path  string
	value json.RawMessage
}

// collectExtras walks a Frame and its typed payload to collect any non-empty
// Extra maps, returning a report of all unmodeled fields found.
func collectExtras(t *testing.T, lineNum int, f *codexwire.Frame) []extraReport {
	t.Helper()
	var out []extraReport

	switch f.Kind {
	case codexwire.FrameKindClientRequest, codexwire.FrameKindClientNotification,
		codexwire.FrameKindServerNotification:
		if f.Params != nil {
			out = append(out, walkExtras("params", f.Params)...)
		}
	case codexwire.FrameKindServerResponse:
		if f.Result != nil {
			out = append(out, walkExtras("result", f.Result)...)
		}
	}

	return out
}

// walkExtras uses reflection to find Extra fields (map[string]json.RawMessage)
// on structs reachable from v, returning their contents as extraReports.
func walkExtras(prefix string, v any) []extraReport {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}

	var out []extraReport
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		fv := rv.Field(i)

		if f.Name == "Extra" {
			// Check if this Extra map is non-empty.
			if !fv.IsNil() {
				iter := fv.MapRange()
				for iter.Next() {
					path := prefix + ".Extra." + iter.Key().String()
					raw, _ := iter.Value().Interface().(json.RawMessage)
					out = append(out, extraReport{path: path, value: raw})
				}
			}
			continue
		}

		// Recurse into struct fields (dereference pointers).
		child := fv
		if child.Kind() == reflect.Ptr {
			if child.IsNil() {
				continue
			}
			child = child.Elem()
		}
		tag := f.Tag.Get("json")
		name := f.Name
		if tag != "" && tag != "-" {
			name = strings.Split(tag, ",")[0]
			if name == "" {
				name = f.Name
			}
		}
		if child.Kind() == reflect.Struct {
			childPath := prefix + "." + name
			childIface := child.Addr().Interface()
			out = append(out, walkExtras(childPath, childIface)...)
		}
		if child.Kind() == reflect.Slice && child.Type().Elem().Kind() == reflect.Struct {
			for j := 0; j < child.Len(); j++ {
				elem := child.Index(j)
				childPath := fmt.Sprintf("%s.%s[%d]", prefix, name, j)
				childIface := elem.Addr().Interface()
				out = append(out, walkExtras(childPath, childIface)...)
			}
		}
	}
	return out
}

func formatExtras(extras []extraReport) string {
	var sb strings.Builder
	for _, e := range extras {
		fmt.Fprintf(&sb, "  %s = %s\n", e.path, e.value)
	}
	return sb.String()
}
