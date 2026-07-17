package daemon

// T5 — golden wire-byte table (MANDATORY, package daemon, over net.Pipe, no
// daemon boot). Drives handleSocketConn and asserts the EXACT JSON bytes and the
// presence/absence of the trailing '\n' for every distinct envelope shape. This
// is the sole byte-identity proof (the round-trip suites json.Decode into a
// struct and are order-/newline-/null-blind — wire-F1).
//
// The last three rows (success/no-result/error_code-absent) exercise the only
// real drift surface — resultToResponse + adapter Result population (wire-F2).
// Rows 5/6 pin both newline directions (wire-F3).

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	socketrouter "github.com/gregberns/harmonik/internal/daemon/router"
	"github.com/gregberns/harmonik/internal/queue"
)

// --- stub handlers -----------------------------------------------------------

type t5RequestHandler struct {
	result json.RawMessage
}

func (s *t5RequestHandler) EmitOutcome(_ context.Context, _ OutcomeRequest) (json.RawMessage, error) {
	return s.result, nil
}
func (s *t5RequestHandler) ClaimNext(_ context.Context, _ string) (json.RawMessage, error) {
	return s.result, nil
}

type t5OperatorHandler struct{}

func (t5OperatorHandler) HandleOperatorPause(_ context.Context, _ string) error  { return nil }
func (t5OperatorHandler) HandleOperatorResume(_ context.Context, _ string) error { return nil }

type t5QueueHandler struct {
	result json.RawMessage
}

func (s *t5QueueHandler) HandleQueueSubmit(_ context.Context, _ json.RawMessage) (json.RawMessage, *queue.RPCError) {
	return s.result, nil
}
func (s *t5QueueHandler) HandleQueueAppend(_ context.Context, _ json.RawMessage) (json.RawMessage, *queue.RPCError) {
	return s.result, nil
}
func (s *t5QueueHandler) HandleQueueStatus(_ context.Context, _ json.RawMessage) (json.RawMessage, *queue.RPCError) {
	return s.result, nil
}
func (s *t5QueueHandler) HandleQueueDryRun(_ context.Context, _ json.RawMessage) (json.RawMessage, *queue.RPCError) {
	return s.result, nil
}
func (s *t5QueueHandler) HandleQueueList(_ context.Context) (json.RawMessage, *queue.RPCError) {
	return s.result, nil
}
func (s *t5QueueHandler) HandleQueueSetConcurrency(_ context.Context, _ json.RawMessage) (json.RawMessage, *queue.RPCError) {
	return s.result, nil
}
func (s *t5QueueHandler) HandleWorkerSetEnabled(_ context.Context, _ json.RawMessage) (json.RawMessage, *queue.RPCError) {
	return s.result, nil
}
func (s *t5QueueHandler) HandleQueueCancel(_ context.Context, _ json.RawMessage) (json.RawMessage, *queue.RPCError) {
	return s.result, nil
}

// driveConn runs handleSocketConn over a net.Pipe with the given router, writes
// reqBytes, and returns the exact raw response bytes.
func driveConn(t *testing.T, router *socketrouter.Router, hr HookRelayHandler, sub SubscribeHandler, reqBytes []byte) []byte {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	go handleSocketConn(context.Background(), serverConn, hr, sub, router)

	go func() {
		_, _ = clientConn.Write(reqBytes)
	}()

	_ = clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	got, err := io.ReadAll(clientConn)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return got
}

func TestHandleSocketConn_WireBytes(t *testing.T) {
	// Precompute the two dynamic-error strings so the assertions are byte-exact
	// yet robust to encoding/json's exact wording.
	var junkReq SocketRequest
	decodeReqErr := json.Unmarshal([]byte(`{"op":123}`), &junkReq)
	if decodeReqErr == nil {
		t.Fatal("expected {\"op\":123} to fail SocketRequest decode")
	}
	var junkMap map[string]json.RawMessage
	rawDecodeErr := json.Unmarshal([]byte(`not json`), &junkMap)
	if rawDecodeErr == nil {
		t.Fatal("expected `not json` to fail raw-map decode")
	}

	cases := []struct {
		name     string
		dispatch *socketDispatch
		sub      SubscribeHandler
		req      string
		want     string
	}{
		{
			name:     "error: nil comms-presence",
			dispatch: &socketDispatch{},
			req:      `{"op":"comms-presence"}`,
			want:     `{"ok":false,"error":"daemon: CommsPresenceHandler not registered"}`,
		},
		{
			name:     "error: nil queue-submit",
			dispatch: &socketDispatch{},
			req:      `{"op":"queue-submit"}`,
			want:     `{"ok":false,"error":"daemon: QueueHandler not registered","error_code":-32099}`,
		},
		{
			name:     "error: unknown op",
			dispatch: &socketDispatch{},
			req:      `{"op":"bogus"}`,
			want:     `{"ok":false,"error":"daemon: unknown op \"bogus\""}`,
		},
		{
			name:     "error: decode-request fail",
			dispatch: &socketDispatch{},
			req:      `{"op":123}`,
			want:     `{"ok":false,"error":"daemon: decode request: ` + jsonEscape(decodeReqErr.Error()) + `"}`,
		},
		{
			name:     "bad_envelope: raw-decode fail (trailing newline)",
			dispatch: &socketDispatch{},
			req:      `not json`,
			want:     `{"status":"bad_envelope","reason":"decode: ` + jsonEscape(rawDecodeErr.Error()) + `"}` + "\n",
		},
		{
			name:     "success: operator-pause (no result)",
			dispatch: &socketDispatch{oh: t5OperatorHandler{}},
			req:      `{"op":"operator-pause"}`,
			want:     `{"ok":true}`,
		},
		{
			name:     "success: emit-outcome (with result)",
			dispatch: &socketDispatch{h: &t5RequestHandler{result: json.RawMessage(`{"x":1}`)}},
			req:      `{"op":"emit-outcome"}`,
			want:     `{"ok":true,"result":{"x":1}}`,
		},
		{
			name:     "success: queue-op (error_code absent)",
			dispatch: &socketDispatch{qh: &t5QueueHandler{result: json.RawMessage(`{"q":true}`)}},
			req:      `{"op":"queue-status"}`,
			want:     `{"ok":true,"result":{"q":true}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := buildSocketRouter(tc.dispatch)
			got := driveConn(t, router, nil, tc.sub, []byte(tc.req))
			if string(got) != tc.want {
				t.Fatalf("wire bytes mismatch:\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// jsonEscape returns s as it would appear inside a JSON string literal (minus the
// surrounding quotes), matching encoding/json's escaping used by writeSocketResponse.
func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}
