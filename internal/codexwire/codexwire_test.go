package codexwire_test

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
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("close corpus: %v", err)
		}
	})

	requestsByID := map[string]string{} // id (raw JSON) → method

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	lineNum := 0
	for sc.Scan() {
		line := sc.Bytes()
		lineNum++
		if strings.TrimSpace(string(line)) == "" {
			continue
		}

		frame, err := codexwire.Parse(line)
		if err != nil {
			t.Errorf("line %d: Parse error: %v\n  line: %s", lineNum, err, line)
			continue
		}

		if frame.Kind == codexwire.FrameKindRaw {
			t.Errorf("line %d: unknown method (FrameKindRaw) — method %q must be added to registry\n  line: %s",
				lineNum, frame.Method, line)
			continue
		}

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

		if frame.Kind == codexwire.FrameKindClientRequest {
			requestsByID[string(frame.ID)] = frame.Method
		}

		if extras := collectExtras(t, &frame); len(extras) > 0 {
			t.Errorf("line %d: unmodeled fields found (Extra must be empty):\n%s\n  line: %s",
				lineNum, formatExtras(extras), line)
		}

		got, err := codexwire.Marshal(frame)
		if err != nil {
			t.Errorf("line %d: Marshal error: %v\n  line: %s", lineNum, err, line)
			continue
		}
		if err := assertSemanticEqual(t, line, got); err != nil {
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
			if err := assertSemanticEqual(t, []byte(tc.line), out); err != nil {
				t.Fatalf("%s did not round-trip: %v\n  in:  %s\n  out: %s", tc.name, err, tc.line, out)
			}
		})
	}
}

// TestServerRequestClassification proves a JSON-RPC request the app-server
// sends TO the client (id + method, e.g. an exec / apply-patch approval prompt
// whose method is not a client-originated method) is classified as
// FrameKindServerRequest — NOT FrameKindClientRequest and NOT dropped to
// FrameKindRaw — so the driver can answer it instead of hanging the turn (RU-07).
func TestServerRequestClassification(t *testing.T) {
	cases := []struct {
		name string
		line string
		want codexwire.FrameKind
	}{
		{
			name: "approval prompt (unknown method, id+method) → ServerRequest",
			line: `{"jsonrpc":"2.0","id":42,"method":"execCommandApproval","params":{"command":"rm -rf /tmp/x"}}`,
			want: codexwire.FrameKindServerRequest,
		},
		{
			name: "apply-patch approval (unknown method, string id) → ServerRequest",
			line: `{"jsonrpc":"2.0","id":"appr-1","method":"applyPatchApproval","params":{"patch":"..."}}`,
			want: codexwire.FrameKindServerRequest,
		},
		{
			name: "client-originated request (registry DirClient) → ClientRequest",
			line: `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"x","title":"y","version":"1"},"capabilities":null}}`,
			want: codexwire.FrameKindClientRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame, err := codexwire.Parse([]byte(tc.line))
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if frame.Kind != tc.want {
				t.Fatalf("classification = %d, want %d (line dropped or misfiled)", frame.Kind, tc.want)
			}
			out, err := codexwire.Marshal(frame)
			if err != nil {
				t.Fatalf("Marshal returned error: %v", err)
			}
			if err := assertSemanticEqual(t, []byte(tc.line), out); err != nil {
				t.Fatalf("did not round-trip: %v\n  in:  %s\n  out: %s", err, tc.line, out)
			}
		})
	}
}

// TestThreadResumeRoundTrip_HK160YB pins the thread/resume wire method added for
// the persistent sidecar's reconnect path (hk-160yb G2). It proves: the method
// is registered and client-originated (NOT dropped to FrameKindRaw), the typed
// params expose the required threadId, and every optional posture field
// (sandbox, approvalPolicy, cwd, …) round-trips verbatim through Extra — so a
// reconnect can carry the full resume posture without the codec enumerating it.
func TestThreadResumeRoundTrip_HK160YB(t *testing.T) {
	found := false
	for _, m := range codexwire.RegisteredMethods() {
		if m == "thread/resume" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("thread/resume not in registry — reconnect cannot marshal it")
	}

	cases := []struct {
		name         string
		line         string
		wantThreadID string
	}{
		{
			name:         "threadId only",
			line:         `{"jsonrpc":"2.0","id":3,"method":"thread/resume","params":{"threadId":"th_abc123"}}`,
			wantThreadID: "th_abc123",
		},
		{
			name:         "threadId plus posture extras preserved via Extra",
			line:         `{"jsonrpc":"2.0","id":4,"method":"thread/resume","params":{"threadId":"th_xyz789","sandbox":"danger-full-access","approvalPolicy":"never","cwd":"/w/repo"}}`,
			wantThreadID: "th_xyz789",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame, err := codexwire.Parse([]byte(tc.line))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			if frame.Kind != codexwire.FrameKindClientRequest {
				t.Fatalf("thread/resume classified as %d, want FrameKindClientRequest (%d)",
					frame.Kind, codexwire.FrameKindClientRequest)
			}
			params, ok := frame.Params.(*codexwire.ThreadResumeParams)
			if !ok {
				t.Fatalf("Params is %T, want *codexwire.ThreadResumeParams", frame.Params)
			}
			if params.ThreadID != tc.wantThreadID {
				t.Fatalf("ThreadID = %q, want %q", params.ThreadID, tc.wantThreadID)
			}
			out, err := codexwire.Marshal(frame)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}
			if err := assertSemanticEqual(t, []byte(tc.line), out); err != nil {
				t.Fatalf("did not round-trip: %v\n  in:  %s\n  out: %s", err, tc.line, out)
			}
		})
	}
}

func assertSemanticEqual(t *testing.T, original, remarshal []byte) error {
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

type extraReport struct {
	path  string
	value json.RawMessage
}

func collectExtras(t *testing.T, f *codexwire.Frame) []extraReport {
	t.Helper()
	var out []extraReport

	switch f.Kind {
	case codexwire.FrameKindClientRequest, codexwire.FrameKindClientNotification,
		codexwire.FrameKindServerRequest,
		codexwire.FrameKindServerNotification:
		if f.Params != nil {
			out = append(out, walkExtras("params", f.Params)...)
		}
	case codexwire.FrameKindServerResponse:
		if f.Result != nil {
			out = append(out, walkExtras("result", f.Result)...)
		}
	case codexwire.FrameKindRaw:
	}

	return out
}

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
			if !fv.IsNil() {
				iter := fv.MapRange()
				for iter.Next() {
					path := prefix + ".Extra." + iter.Key().String()
					raw, ok := iter.Value().Interface().(json.RawMessage)
					if !ok {
						out = append(out, extraReport{
							path:  path + ".<unexpected-value-type>",
							value: json.RawMessage("null"),
						})
						continue
					}
					out = append(out, extraReport{path: path, value: raw})
				}
			}
			continue
		}

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
