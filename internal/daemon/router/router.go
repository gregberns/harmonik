package socketrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// Result is the neutral outcome of dispatching one op. The daemon maps it to its
// wire SocketResponse (result-or-error, prefixed-error, or error_code shape). No
// wire vocabulary lives here.
type Result struct {
	// OK is the ok field of the eventual SocketResponse.
	OK bool
	// Payload is the op-specific result on success. A nil Payload MUST render as
	// an omitted result (never "result":null).
	Payload json.RawMessage
	// Err is the human-readable error message on failure.
	Err string
	// ErrorCode is the JSON-RPC code; 0 = plain error (queue ops only).
	ErrorCode int
	// Unknown is set when the op is not registered. The daemon builds the exact
	// "daemon: unknown op %q" wire string from it (no daemon: vocabulary here).
	Unknown bool
}

// Kind discriminates a raw socket envelope: an op-request vs a hook-relay message.
type Kind int

const (
	// KindOp is an op-based SocketRequest.
	KindOp Kind = iota
	// KindHookRelay is a hook-relay envelope (non-empty "type" field).
	KindHookRelay
)

// Classify mirrors handleSocketConn's pre-switch discrimination exactly
// (socket.go:403): a "type" key present AND len(raw["type"]) > 2 → hook-relay;
// otherwise an op-request. The len>2 test is on the raw JSON bytes of the value,
// so {"type":null} (len 4) and {"type":"x"} (len 3) classify as HookRelay while
// {"type":""} (len 2) and an absent "type" classify as Op. Preserve the literal
// expression — a future "cleanup" would silently change routing.
func Classify(raw map[string]json.RawMessage) Kind {
	if typeRaw, hasType := raw["type"]; hasType && len(typeRaw) > 2 {
		return KindHookRelay
	}
	return KindOp
}

// HandlerFunc handles one op. raw is the re-encoded request bytes; the adapter
// re-decodes the daemon's SocketRequest from raw locally when it needs scalar
// fields. Conn-free by design (the edge sheds net).
type HandlerFunc func(ctx context.Context, raw json.RawMessage) Result

// Router is the table-driven op→handler dispatch engine.
type Router struct {
	routes map[string]HandlerFunc
}

// New returns an empty Router.
func New() *Router {
	return &Router{routes: make(map[string]HandlerFunc)}
}

// Register binds fn to op. It panics on a duplicate op (init-time wiring bug).
func (r *Router) Register(op string, fn HandlerFunc) {
	if _, dup := r.routes[op]; dup {
		panic(fmt.Sprintf("socketrouter: duplicate op registered: %q", op))
	}
	r.routes[op] = fn
}

// Ops returns the registered op names, sorted — for the wiring-completeness guard.
func (r *Router) Ops() []string {
	ops := make([]string, 0, len(r.routes))
	for op := range r.routes {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	return ops
}

// Dispatch looks up op and invokes its handler. An unregistered op returns a
// neutral Result{Unknown: true} — the daemon builds the wire string.
func (r *Router) Dispatch(ctx context.Context, op string, raw json.RawMessage) Result {
	fn, ok := r.routes[op]
	if !ok {
		return Result{Unknown: true}
	}
	return fn(ctx, raw)
}
