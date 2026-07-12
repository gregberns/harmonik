// Package codexwire implements the wire-contract serializer for the codex
// app-server JSON-RPC 2.0 stdio protocol (codex-app-server T2, hk-tg5mo).
//
// The design has two layers:
//
//  1. Trusted envelope: strict JSON-RPC 2.0 outer frame. Four discriminated
//     frame kinds are inferred from the combination of id/method/result/error
//     fields present on each line:
//       - ClientRequest      (jsonrpc + id + method + params)
//       - ClientNotification (jsonrpc + method; no id)
//       - ServerResponse     (id + result/error; no jsonrpc/method on wire)
//       - ServerNotification (method + params; no id/jsonrpc on wire)
//
//  2. Untrusted payload: per-method params/result structs. Every struct
//     carries an Extra field (map[string]json.RawMessage) that captures and
//     counts any fields not yet modeled here. Unknown methods yield
//     FrameKindRaw rather than an error — the bytes are preserved verbatim.
//
// Method strings live in a single registry table (methodRegistry). Adding a
// new method requires one entry there plus a Params/Result type pair.
//
// Round-trip guarantee (T2 gate): Parse(raw) → Marshal(frame) produces JSON
// that is semantically equal to raw — same key-value pairs at every level,
// Extra fields preserved. Verified by TestCorpusRoundTrip in codexwire_test.go.
//
// Bead: hk-tg5mo [codex-app-server T2]
package codexwire

import (
	"encoding/json"
	"fmt"
)

// ─── Frame discrimination ────────────────────────────────────────────────────

// FrameKind classifies a parsed line.
type FrameKind int

const (
	FrameKindClientRequest      FrameKind = iota // client→server: jsonrpc + id + method
	FrameKindClientNotification                  // client→server: jsonrpc + method, no id
	FrameKindServerResponse                      // server→client: id + result/error, no method
	FrameKindServerNotification                  // server→client: method + params, no id/jsonrpc
	FrameKindRaw                                 // unknown method; raw bytes preserved
)

// Frame is a fully-parsed line from the codex app-server stdio stream.
// Exactly one of the typed payload fields is non-nil, selected by Kind.
type Frame struct {
	Kind FrameKind

	// Envelope fields (all may be zero for their type when not present on wire).
	JSONRPC string
	ID      int64  // 0 when absent
	Method  string // "" for server responses

	// Typed payload — only one is non-nil.
	Params any             // client request/notification or server notification params
	Result any             // server response result
	Error  json.RawMessage // server response error (raw; JSON-RPC error object)

	// RawParams / RawResult carry the unmodified JSON for the payload so the
	// typed structs can be marshaled back faithfully alongside Extra fields.
	RawParams json.RawMessage
	RawResult json.RawMessage

	// Raw is only set when Kind == FrameKindRaw (unknown method).
	Raw []byte
}

// ─── Method registry ─────────────────────────────────────────────────────────

// Direction distinguishes who originates a method.
type Direction int

const (
	DirClient Direction = iota // client sends this method
	DirServer                  // server sends this method
)

type methodEntry struct {
	Dir        Direction
	MakeParams func() any // nil for methods with no params (e.g. "initialized")
	MakeResult func() any // non-nil only for client requests
}

// methodRegistry is the single source of truth for every method string that
// appears in corpus-covered traffic. One entry per method.
//
// To add a new method: add an entry here, then add the Params/Result types
// below. Unknown methods are handled gracefully (FrameKindRaw).
var methodRegistry = map[string]methodEntry{
	// ── Client requests ──────────────────────────────────────────────────────
	"initialize": {
		Dir:        DirClient,
		MakeParams: func() any { return &InitializeParams{} },
		MakeResult: func() any { return &InitializeResult{} },
	},
	"thread/start": {
		Dir:        DirClient,
		MakeParams: func() any { return &ThreadStartParams{} },
		MakeResult: func() any { return &ThreadStartResult{} },
	},
	"turn/start": {
		Dir:        DirClient,
		MakeParams: func() any { return &TurnStartParams{} },
		MakeResult: func() any { return &TurnStartResult{} },
	},
	// ── Client notifications ─────────────────────────────────────────────────
	"initialized": {
		Dir:        DirClient,
		MakeParams: nil, // no params; only {jsonrpc, method} on wire
	},
	// ── Server notifications ─────────────────────────────────────────────────
	"configWarning": {
		Dir:        DirServer,
		MakeParams: func() any { return &ConfigWarningParams{} },
	},
	"remoteControl/status/changed": {
		Dir:        DirServer,
		MakeParams: func() any { return &RemoteControlStatusChangedParams{} },
	},
	"thread/started": {
		Dir:        DirServer,
		MakeParams: func() any { return &ThreadStartedParams{} },
	},
	"mcpServer/startupStatus/updated": {
		Dir:        DirServer,
		MakeParams: func() any { return &MCPServerStartupStatusUpdatedParams{} },
	},
	"thread/status/changed": {
		Dir:        DirServer,
		MakeParams: func() any { return &ThreadStatusChangedParams{} },
	},
	"turn/started": {
		Dir:        DirServer,
		MakeParams: func() any { return &TurnStartedParams{} },
	},
	"item/started": {
		Dir:        DirServer,
		MakeParams: func() any { return &ItemStartedParams{} },
	},
	"item/completed": {
		Dir:        DirServer,
		MakeParams: func() any { return &ItemCompletedParams{} },
	},
	"item/agentMessage/delta": {
		Dir:        DirServer,
		MakeParams: func() any { return &ItemAgentMessageDeltaParams{} },
	},
	"thread/tokenUsage/updated": {
		Dir:        DirServer,
		MakeParams: func() any { return &ThreadTokenUsageUpdatedParams{} },
	},
	"account/rateLimits/updated": {
		Dir:        DirServer,
		MakeParams: func() any { return &AccountRateLimitsUpdatedParams{} },
	},
	"turn/completed": {
		Dir:        DirServer,
		MakeParams: func() any { return &TurnCompletedParams{} },
	},
}

// ─── Parse ───────────────────────────────────────────────────────────────────

// rawLine is the superset decode target for one JSON-RPC 2.0 line.
// Every field is optional at the envelope level; absent fields are zero.
type rawLine struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"` // pointer to distinguish absent (nil) from 0
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

// Parse decodes one newline-delimited JSON line into a Frame.
//
// The envelope layer (jsonrpc, id, method) is parsed strictly; an
// unrecognised JSON structure is an error. An unknown method is NOT an error:
// it yields a Frame with Kind == FrameKindRaw and the original bytes in Raw.
func Parse(line []byte) (Frame, error) {
	if len(line) == 0 {
		return Frame{}, fmt.Errorf("codexwire: empty line")
	}

	var env rawLine
	if err := json.Unmarshal(line, &env); err != nil {
		return Frame{}, fmt.Errorf("codexwire: decode envelope: %w", err)
	}

	hasID := env.ID != nil
	hasMethod := env.Method != ""
	hasResult := len(env.Result) > 0 && string(env.Result) != "null"
	hasError := len(env.Error) > 0 && string(env.Error) != "null"
	hasParams := len(env.Params) > 0 && string(env.Params) != "null"

	f := Frame{
		JSONRPC:   env.JSONRPC,
		Method:    env.Method,
		RawParams: env.Params,
		RawResult: env.Result,
		Error:     env.Error,
	}
	if hasID {
		f.ID = *env.ID
	}

	switch {
	case hasID && hasMethod:
		// Client request.
		f.Kind = FrameKindClientRequest
		if err := parseParams(&f, hasParams); err != nil {
			return Frame{}, err
		}

	case !hasID && hasMethod && !hasResult && !hasError:
		// Notification (client or server determined by registry).
		entry, ok := methodRegistry[env.Method]
		if !ok {
			f.Kind = FrameKindRaw
			raw := make([]byte, len(line))
			copy(raw, line)
			f.Raw = raw
			return f, nil
		}
		if entry.Dir == DirClient {
			f.Kind = FrameKindClientNotification
		} else {
			f.Kind = FrameKindServerNotification
		}
		if err := parseParams(&f, hasParams); err != nil {
			return Frame{}, err
		}

	case hasID && !hasMethod:
		// Server response.
		f.Kind = FrameKindServerResponse
		if err := parseResult(&f); err != nil {
			return Frame{}, err
		}

	default:
		f.Kind = FrameKindRaw
		raw := make([]byte, len(line))
		copy(raw, line)
		f.Raw = raw
	}

	return f, nil
}

// parseParams populates f.Params from f.RawParams using the method registry.
func parseParams(f *Frame, hasParams bool) error {
	entry, ok := methodRegistry[f.Method]
	if !ok || entry.MakeParams == nil || !hasParams {
		return nil
	}
	p := entry.MakeParams()
	if err := json.Unmarshal(f.RawParams, p); err != nil {
		return fmt.Errorf("codexwire: parse params for %q: %w", f.Method, err)
	}
	f.Params = p
	return nil
}

// parseResult populates f.Result from f.RawResult for a server response.
// The method is looked up via a reverse lookup from the request registry;
// since we parse both directions, we find the entry by matching requests.
// For corpus correctness the result is stored in f.Params (see Marshal).
//
// We can't resolve method→result without knowing the request id correlation.
// Instead we store the raw result and resolve via ResponseResult if needed.
func parseResult(f *Frame) error {
	// Result parsing is done lazily via ResolveResponseResult when the caller
	// correlates the response id to the originating request method. For the
	// round-trip gate, we only need to preserve and re-emit RawResult, which
	// is already stored in f.RawResult.
	return nil
}

// ResolveResponseResult parses the raw result of a server response into the
// typed result struct for the given request method. Returns nil error when the
// method is unknown or has no result type (result remains in frame.RawResult).
func ResolveResponseResult(f *Frame, requestMethod string) error {
	if f.Kind != FrameKindServerResponse {
		return fmt.Errorf("codexwire: ResolveResponseResult called on non-response frame")
	}
	entry, ok := methodRegistry[requestMethod]
	if !ok || entry.MakeResult == nil || len(f.RawResult) == 0 {
		return nil
	}
	r := entry.MakeResult()
	if err := json.Unmarshal(f.RawResult, r); err != nil {
		return fmt.Errorf("codexwire: parse result for %q: %w", requestMethod, err)
	}
	f.Result = r
	return nil
}

// ─── Marshal ─────────────────────────────────────────────────────────────────

// Marshal re-serializes a Frame back to JSON-RPC 2.0 wire format.
//
// The output is semantically equal to the original line: all envelope fields
// are preserved and Extra fields on payload structs are merged back. This is
// the round-trip guarantee.
func Marshal(f Frame) ([]byte, error) {
	switch f.Kind {
	case FrameKindClientRequest:
		return marshalClientRequest(f)
	case FrameKindClientNotification:
		return marshalClientNotification(f)
	case FrameKindServerResponse:
		return marshalServerResponse(f)
	case FrameKindServerNotification:
		return marshalServerNotification(f)
	case FrameKindRaw:
		if len(f.Raw) > 0 {
			out := make([]byte, len(f.Raw))
			copy(out, f.Raw)
			return out, nil
		}
		return nil, fmt.Errorf("codexwire: marshal FrameKindRaw with no raw bytes")
	default:
		return nil, fmt.Errorf("codexwire: marshal unknown kind %d", f.Kind)
	}
}

func marshalClientRequest(f Frame) ([]byte, error) {
	params, err := marshalPayload(f.Params, f.RawParams)
	if err != nil {
		return nil, fmt.Errorf("codexwire: marshal client request params: %w", err)
	}
	m := map[string]json.RawMessage{}
	if f.JSONRPC != "" {
		b, _ := json.Marshal(f.JSONRPC)
		m["jsonrpc"] = b
	}
	idB, _ := json.Marshal(f.ID)
	m["id"] = idB
	methodB, _ := json.Marshal(f.Method)
	m["method"] = methodB
	if len(params) > 0 {
		m["params"] = params
	}
	return json.Marshal(m)
}

func marshalClientNotification(f Frame) ([]byte, error) {
	params, err := marshalPayload(f.Params, f.RawParams)
	if err != nil {
		return nil, fmt.Errorf("codexwire: marshal client notification params: %w", err)
	}
	m := map[string]json.RawMessage{}
	if f.JSONRPC != "" {
		b, _ := json.Marshal(f.JSONRPC)
		m["jsonrpc"] = b
	}
	methodB, _ := json.Marshal(f.Method)
	m["method"] = methodB
	if len(params) > 0 {
		m["params"] = params
	}
	return json.Marshal(m)
}

func marshalServerResponse(f Frame) ([]byte, error) {
	m := map[string]json.RawMessage{}
	idB, _ := json.Marshal(f.ID)
	m["id"] = idB
	if f.Result != nil {
		rb, err := json.Marshal(f.Result)
		if err != nil {
			return nil, fmt.Errorf("codexwire: marshal server response result: %w", err)
		}
		m["result"] = rb
	} else if len(f.RawResult) > 0 {
		m["result"] = f.RawResult
	}
	if len(f.Error) > 0 {
		m["error"] = f.Error
	}
	return json.Marshal(m)
}

func marshalServerNotification(f Frame) ([]byte, error) {
	params, err := marshalPayload(f.Params, f.RawParams)
	if err != nil {
		return nil, fmt.Errorf("codexwire: marshal server notification params: %w", err)
	}
	m := map[string]json.RawMessage{}
	methodB, _ := json.Marshal(f.Method)
	m["method"] = methodB
	if len(params) > 0 {
		m["params"] = params
	}
	return json.Marshal(m)
}

// marshalPayload serializes a typed params struct back to JSON, merging Extra.
// Falls back to raw when typed is nil.
func marshalPayload(typed any, raw json.RawMessage) (json.RawMessage, error) {
	if typed == nil {
		return raw, nil
	}
	return json.Marshal(typed)
}

// ─── Extra field helpers ──────────────────────────────────────────────────────

// parseExtra reads data (a JSON object) into target (via the standard
// json.Unmarshal using an alias), then captures any keys not in known into
// extra. Call this from each struct's UnmarshalJSON.
//
// Usage pattern:
//
//	func (p *FooParams) UnmarshalJSON(data []byte) error {
//	    type alias FooParams
//	    if err := json.Unmarshal(data, (*alias)(p)); err != nil {
//	        return err
//	    }
//	    return parseExtra(data, fooParamsKnown, &p.Extra)
//	}
func parseExtra(data []byte, known map[string]bool, extra *map[string]json.RawMessage) error {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	for k, v := range all {
		if !known[k] {
			if *extra == nil {
				*extra = make(map[string]json.RawMessage)
			}
			(*extra)[k] = v
		}
	}
	return nil
}

// mergeExtra merges extra fields into a marshaled JSON object byte slice.
// Returns base unchanged when extra is empty.
func mergeExtra(base []byte, extra map[string]json.RawMessage) ([]byte, error) {
	if len(extra) == 0 {
		return base, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(base, &m); err != nil {
		return nil, err
	}
	for k, v := range extra {
		m[k] = v
	}
	return json.Marshal(m)
}

// ExtraCount returns the number of unmodeled fields captured in extra.
// A non-zero count means the struct has fields the package does not yet model.
func ExtraCount(extra map[string]json.RawMessage) int {
	return len(extra)
}

// ─── Client request / notification params ─────────────────────────────────────

// InitializeParams: client→server "initialize" request params.
//
// Corpus evidence:
//   {"clientInfo": {"name": ..., "title": ..., "version": ...}, "capabilities": null}
type InitializeParams struct {
	ClientInfo   ClientInfo      `json:"clientInfo"`
	Capabilities json.RawMessage `json:"capabilities"` // null in corpus; preserved verbatim
	Extra        map[string]json.RawMessage `json:"-"`
}

var initializeParamsKnown = map[string]bool{"clientInfo": true, "capabilities": true}

func (p *InitializeParams) UnmarshalJSON(data []byte) error {
	type alias InitializeParams
	if err := json.Unmarshal(data, (*alias)(p)); err != nil {
		return err
	}
	return parseExtra(data, initializeParamsKnown, &p.Extra)
}

func (p InitializeParams) MarshalJSON() ([]byte, error) {
	type alias InitializeParams
	b, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, p.Extra)
}

// ClientInfo is the client identity block sent in "initialize".
type ClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
	Extra   map[string]json.RawMessage `json:"-"`
}

var clientInfoKnown = map[string]bool{"name": true, "title": true, "version": true}

func (c *ClientInfo) UnmarshalJSON(data []byte) error {
	type alias ClientInfo
	if err := json.Unmarshal(data, (*alias)(c)); err != nil {
		return err
	}
	return parseExtra(data, clientInfoKnown, &c.Extra)
}

func (c ClientInfo) MarshalJSON() ([]byte, error) {
	type alias ClientInfo
	b, err := json.Marshal(alias(c))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, c.Extra)
}

// ThreadStartParams: client→server "thread/start" request params.
//
// Corpus evidence: {"cwd": "/Users/gb/github/harmonik"}
// All fields optional per T0 findings ("all fields optional").
type ThreadStartParams struct {
	CWD   string `json:"cwd,omitempty"`
	Extra map[string]json.RawMessage `json:"-"`
}

var threadStartParamsKnown = map[string]bool{"cwd": true}

func (p *ThreadStartParams) UnmarshalJSON(data []byte) error {
	type alias ThreadStartParams
	if err := json.Unmarshal(data, (*alias)(p)); err != nil {
		return err
	}
	return parseExtra(data, threadStartParamsKnown, &p.Extra)
}

func (p ThreadStartParams) MarshalJSON() ([]byte, error) {
	type alias ThreadStartParams
	b, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, p.Extra)
}

// TurnStartParams: client→server "turn/start" request params.
//
// Corpus evidence: {"threadId": "...", "input": [{"type":"text","text":"...","text_elements":[]}]}
// NOTE: text_elements:[] is REQUIRED in the text UserInput variant (T0 finding).
type TurnStartParams struct {
	ThreadID string      `json:"threadId"`
	Input    []InputItem `json:"input"`
	Extra    map[string]json.RawMessage `json:"-"`
}

var turnStartParamsKnown = map[string]bool{"threadId": true, "input": true}

func (p *TurnStartParams) UnmarshalJSON(data []byte) error {
	type alias TurnStartParams
	if err := json.Unmarshal(data, (*alias)(p)); err != nil {
		return err
	}
	return parseExtra(data, turnStartParamsKnown, &p.Extra)
}

func (p TurnStartParams) MarshalJSON() ([]byte, error) {
	type alias TurnStartParams
	b, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, p.Extra)
}

// InputItem is one element of the turn/start input array.
// Corpus shows only the "text" variant; other variants preserved via Extra.
type InputItem struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	TextElements json.RawMessage `json:"text_elements,omitempty"` // [] in corpus; raw to preserve
	Extra        map[string]json.RawMessage `json:"-"`
}

var inputItemKnown = map[string]bool{"type": true, "text": true, "text_elements": true}

func (i *InputItem) UnmarshalJSON(data []byte) error {
	type alias InputItem
	if err := json.Unmarshal(data, (*alias)(i)); err != nil {
		return err
	}
	return parseExtra(data, inputItemKnown, &i.Extra)
}

func (i InputItem) MarshalJSON() ([]byte, error) {
	type alias InputItem
	b, err := json.Marshal(alias(i))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, i.Extra)
}

// ─── Client request results ──────────────────────────────────────────────────

// InitializeResult: server→client "initialize" response result.
//
// Corpus evidence: {"userAgent":"...","codexHome":"...","platformFamily":"...","platformOs":"..."}
type InitializeResult struct {
	UserAgent      string `json:"userAgent"`
	CodexHome      string `json:"codexHome"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOs     string `json:"platformOs"`
	Extra          map[string]json.RawMessage `json:"-"`
}

var initializeResultKnown = map[string]bool{
	"userAgent": true, "codexHome": true, "platformFamily": true, "platformOs": true,
}

func (r *InitializeResult) UnmarshalJSON(data []byte) error {
	type alias InitializeResult
	if err := json.Unmarshal(data, (*alias)(r)); err != nil {
		return err
	}
	return parseExtra(data, initializeResultKnown, &r.Extra)
}

func (r InitializeResult) MarshalJSON() ([]byte, error) {
	type alias InitializeResult
	b, err := json.Marshal(alias(r))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, r.Extra)
}

// ThreadStartResult: server→client "thread/start" response result.
//
// Corpus evidence: large object with thread, model, sandbox, etc.
// thread and most nested fields are stored as RawMessage to preserve all
// fields verbatim (they are not used programmatically by this package).
type ThreadStartResult struct {
	Thread                  json.RawMessage `json:"thread"`
	Model                   string          `json:"model,omitempty"`
	ModelProvider           string          `json:"modelProvider,omitempty"`
	ServiceTier             json.RawMessage `json:"serviceTier"`      // null in corpus
	CWD                     string          `json:"cwd,omitempty"`
	RuntimeWorkspaceRoots   []string        `json:"runtimeWorkspaceRoots,omitempty"`
	InstructionSources      []string        `json:"instructionSources,omitempty"`
	ApprovalPolicy          string          `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer       string          `json:"approvalsReviewer,omitempty"`
	Sandbox                 json.RawMessage `json:"sandbox"`                 // {type,networkAccess}
	ActivePermissionProfile json.RawMessage `json:"activePermissionProfile"` // {id,extends}
	ReasoningEffort         json.RawMessage `json:"reasoningEffort"`         // null in corpus
	MultiAgentMode          string          `json:"multiAgentMode,omitempty"`
	Extra                   map[string]json.RawMessage `json:"-"`
}

var threadStartResultKnown = map[string]bool{
	"thread": true, "model": true, "modelProvider": true, "serviceTier": true,
	"cwd": true, "runtimeWorkspaceRoots": true, "instructionSources": true,
	"approvalPolicy": true, "approvalsReviewer": true, "sandbox": true,
	"activePermissionProfile": true, "reasoningEffort": true, "multiAgentMode": true,
}

func (r *ThreadStartResult) UnmarshalJSON(data []byte) error {
	type alias ThreadStartResult
	if err := json.Unmarshal(data, (*alias)(r)); err != nil {
		return err
	}
	return parseExtra(data, threadStartResultKnown, &r.Extra)
}

func (r ThreadStartResult) MarshalJSON() ([]byte, error) {
	type alias ThreadStartResult
	b, err := json.Marshal(alias(r))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, r.Extra)
}

// TurnStartResult: server→client "turn/start" response result.
//
// Corpus evidence: {"turn":{"id":"...","items":[],"itemsView":"notLoaded","status":"inProgress",...}}
type TurnStartResult struct {
	Turn  Turn `json:"turn"`
	Extra map[string]json.RawMessage `json:"-"`
}

var turnStartResultKnown = map[string]bool{"turn": true}

func (r *TurnStartResult) UnmarshalJSON(data []byte) error {
	type alias TurnStartResult
	if err := json.Unmarshal(data, (*alias)(r)); err != nil {
		return err
	}
	return parseExtra(data, turnStartResultKnown, &r.Extra)
}

func (r TurnStartResult) MarshalJSON() ([]byte, error) {
	type alias TurnStartResult
	b, err := json.Marshal(alias(r))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, r.Extra)
}

// ─── Shared model types ──────────────────────────────────────────────────────

// Turn is the codex turn object appearing in multiple notifications.
//
// Corpus evidence: id, items (empty array), itemsView, status, error (null),
// startedAt (null or int), completedAt (null or int), durationMs (null or int).
type Turn struct {
	ID          string          `json:"id"`
	Items       json.RawMessage `json:"items"`       // [] in corpus; array of item objects
	ItemsView   string          `json:"itemsView,omitempty"`
	Status      string          `json:"status,omitempty"`
	Error       json.RawMessage `json:"error"`       // null in corpus
	StartedAt   json.RawMessage `json:"startedAt"`   // null or int
	CompletedAt json.RawMessage `json:"completedAt"` // null or int
	DurationMs  json.RawMessage `json:"durationMs"`  // null or int
	Extra       map[string]json.RawMessage `json:"-"`
}

var turnKnown = map[string]bool{
	"id": true, "items": true, "itemsView": true, "status": true,
	"error": true, "startedAt": true, "completedAt": true, "durationMs": true,
}

func (t *Turn) UnmarshalJSON(data []byte) error {
	type alias Turn
	if err := json.Unmarshal(data, (*alias)(t)); err != nil {
		return err
	}
	return parseExtra(data, turnKnown, &t.Extra)
}

func (t Turn) MarshalJSON() ([]byte, error) {
	type alias Turn
	b, err := json.Marshal(alias(t))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, t.Extra)
}

// ThreadStatus represents the status discriminated union on a thread.
//
// Corpus evidence:
//   {"type":"idle"}
//   {"type":"active","activeFlags":[]}
type ThreadStatus struct {
	Type        string          `json:"type"`
	ActiveFlags json.RawMessage `json:"activeFlags,omitempty"` // [] when active; absent when idle
	Extra       map[string]json.RawMessage `json:"-"`
}

var threadStatusKnown = map[string]bool{"type": true, "activeFlags": true}

func (s *ThreadStatus) UnmarshalJSON(data []byte) error {
	type alias ThreadStatus
	if err := json.Unmarshal(data, (*alias)(s)); err != nil {
		return err
	}
	return parseExtra(data, threadStatusKnown, &s.Extra)
}

func (s ThreadStatus) MarshalJSON() ([]byte, error) {
	type alias ThreadStatus
	b, err := json.Marshal(alias(s))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, s.Extra)
}

// ─── Server notification params ──────────────────────────────────────────────

// ConfigWarningParams: server→client "configWarning" notification params.
//
// Corpus evidence: {"summary":"Project-local config...","details":null}
type ConfigWarningParams struct {
	Summary string          `json:"summary"`
	Details json.RawMessage `json:"details"` // null in corpus; may be object
	Extra   map[string]json.RawMessage `json:"-"`
}

var configWarningParamsKnown = map[string]bool{"summary": true, "details": true}

func (p *ConfigWarningParams) UnmarshalJSON(data []byte) error {
	type alias ConfigWarningParams
	if err := json.Unmarshal(data, (*alias)(p)); err != nil {
		return err
	}
	return parseExtra(data, configWarningParamsKnown, &p.Extra)
}

func (p ConfigWarningParams) MarshalJSON() ([]byte, error) {
	type alias ConfigWarningParams
	b, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, p.Extra)
}

// RemoteControlStatusChangedParams: server→client "remoteControl/status/changed".
//
// Corpus evidence: {"status":"disabled","serverName":"...","installationId":"...","environmentId":null}
type RemoteControlStatusChangedParams struct {
	Status         string          `json:"status"`
	ServerName     string          `json:"serverName"`
	InstallationID string          `json:"installationId"`
	EnvironmentID  json.RawMessage `json:"environmentId"` // null in corpus
	Extra          map[string]json.RawMessage `json:"-"`
}

var remoteControlStatusChangedParamsKnown = map[string]bool{
	"status": true, "serverName": true, "installationId": true, "environmentId": true,
}

func (p *RemoteControlStatusChangedParams) UnmarshalJSON(data []byte) error {
	type alias RemoteControlStatusChangedParams
	if err := json.Unmarshal(data, (*alias)(p)); err != nil {
		return err
	}
	return parseExtra(data, remoteControlStatusChangedParamsKnown, &p.Extra)
}

func (p RemoteControlStatusChangedParams) MarshalJSON() ([]byte, error) {
	type alias RemoteControlStatusChangedParams
	b, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, p.Extra)
}

// ThreadStartedParams: server→client "thread/started" notification params.
//
// Corpus evidence: {"thread":{...}}
type ThreadStartedParams struct {
	Thread json.RawMessage `json:"thread"` // full Thread object; raw for round-trip
	Extra  map[string]json.RawMessage `json:"-"`
}

var threadStartedParamsKnown = map[string]bool{"thread": true}

func (p *ThreadStartedParams) UnmarshalJSON(data []byte) error {
	type alias ThreadStartedParams
	if err := json.Unmarshal(data, (*alias)(p)); err != nil {
		return err
	}
	return parseExtra(data, threadStartedParamsKnown, &p.Extra)
}

func (p ThreadStartedParams) MarshalJSON() ([]byte, error) {
	type alias ThreadStartedParams
	b, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, p.Extra)
}

// MCPServerStartupStatusUpdatedParams: server→client "mcpServer/startupStatus/updated".
//
// Corpus evidence (2 frames):
//   {"threadId":"...","name":"codex_apps","status":"starting","error":null}
//   {"threadId":"...","name":"codex_apps","status":"ready","error":null}
type MCPServerStartupStatusUpdatedParams struct {
	ThreadID string          `json:"threadId"`
	Name     string          `json:"name"`
	Status   string          `json:"status"`
	Error    json.RawMessage `json:"error"` // null in corpus
	Extra    map[string]json.RawMessage `json:"-"`
}

var mcpServerStartupStatusUpdatedParamsKnown = map[string]bool{
	"threadId": true, "name": true, "status": true, "error": true,
}

func (p *MCPServerStartupStatusUpdatedParams) UnmarshalJSON(data []byte) error {
	type alias MCPServerStartupStatusUpdatedParams
	if err := json.Unmarshal(data, (*alias)(p)); err != nil {
		return err
	}
	return parseExtra(data, mcpServerStartupStatusUpdatedParamsKnown, &p.Extra)
}

func (p MCPServerStartupStatusUpdatedParams) MarshalJSON() ([]byte, error) {
	type alias MCPServerStartupStatusUpdatedParams
	b, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, p.Extra)
}

// ThreadStatusChangedParams: server→client "thread/status/changed".
//
// Corpus evidence:
//   {"threadId":"...","status":{"type":"active","activeFlags":[]}}
//   {"threadId":"...","status":{"type":"idle"}}
type ThreadStatusChangedParams struct {
	ThreadID string       `json:"threadId"`
	Status   ThreadStatus `json:"status"`
	Extra    map[string]json.RawMessage `json:"-"`
}

var threadStatusChangedParamsKnown = map[string]bool{"threadId": true, "status": true}

func (p *ThreadStatusChangedParams) UnmarshalJSON(data []byte) error {
	type alias ThreadStatusChangedParams
	if err := json.Unmarshal(data, (*alias)(p)); err != nil {
		return err
	}
	return parseExtra(data, threadStatusChangedParamsKnown, &p.Extra)
}

func (p ThreadStatusChangedParams) MarshalJSON() ([]byte, error) {
	type alias ThreadStatusChangedParams
	b, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, p.Extra)
}

// TurnStartedParams: server→client "turn/started".
//
// Corpus evidence: {"threadId":"...","turn":{...}}
type TurnStartedParams struct {
	ThreadID string `json:"threadId"`
	Turn     Turn   `json:"turn"`
	Extra    map[string]json.RawMessage `json:"-"`
}

var turnStartedParamsKnown = map[string]bool{"threadId": true, "turn": true}

func (p *TurnStartedParams) UnmarshalJSON(data []byte) error {
	type alias TurnStartedParams
	if err := json.Unmarshal(data, (*alias)(p)); err != nil {
		return err
	}
	return parseExtra(data, turnStartedParamsKnown, &p.Extra)
}

func (p TurnStartedParams) MarshalJSON() ([]byte, error) {
	type alias TurnStartedParams
	b, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, p.Extra)
}

// ItemStartedParams: server→client "item/started".
//
// Corpus evidence (2 variants — userMessage and agentMessage items):
//   {"item":{"type":"userMessage",...},"threadId":"...","turnId":"...","startedAtMs":...}
//   {"item":{"type":"agentMessage",...},"threadId":"...","turnId":"...","startedAtMs":...}
//
// item is stored as RawItem (discriminated by Type, raw content preserved).
type ItemStartedParams struct {
	Item      RawItem `json:"item"`
	ThreadID  string  `json:"threadId"`
	TurnID    string  `json:"turnId"`
	StartedAt int64   `json:"startedAtMs"`
	Extra     map[string]json.RawMessage `json:"-"`
}

var itemStartedParamsKnown = map[string]bool{
	"item": true, "threadId": true, "turnId": true, "startedAtMs": true,
}

func (p *ItemStartedParams) UnmarshalJSON(data []byte) error {
	type alias ItemStartedParams
	if err := json.Unmarshal(data, (*alias)(p)); err != nil {
		return err
	}
	return parseExtra(data, itemStartedParamsKnown, &p.Extra)
}

func (p ItemStartedParams) MarshalJSON() ([]byte, error) {
	type alias ItemStartedParams
	b, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, p.Extra)
}

// ItemCompletedParams: server→client "item/completed".
//
// Same structure as ItemStartedParams but with completedAtMs instead of startedAtMs.
type ItemCompletedParams struct {
	Item        RawItem `json:"item"`
	ThreadID    string  `json:"threadId"`
	TurnID      string  `json:"turnId"`
	CompletedAt int64   `json:"completedAtMs"`
	Extra       map[string]json.RawMessage `json:"-"`
}

var itemCompletedParamsKnown = map[string]bool{
	"item": true, "threadId": true, "turnId": true, "completedAtMs": true,
}

func (p *ItemCompletedParams) UnmarshalJSON(data []byte) error {
	type alias ItemCompletedParams
	if err := json.Unmarshal(data, (*alias)(p)); err != nil {
		return err
	}
	return parseExtra(data, itemCompletedParamsKnown, &p.Extra)
}

func (p ItemCompletedParams) MarshalJSON() ([]byte, error) {
	type alias ItemCompletedParams
	b, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, p.Extra)
}

// RawItem is an item discriminated by Type with all other fields preserved.
// The raw JSON is preserved verbatim so round-trip works regardless of item type.
//
// Corpus item types: "userMessage", "agentMessage".
type RawItem struct {
	Type string // the "type" discriminator
	Raw  json.RawMessage
}

func (r *RawItem) UnmarshalJSON(data []byte) error {
	// Extract the type discriminator.
	var disc struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &disc); err != nil {
		return err
	}
	r.Type = disc.Type
	r.Raw = make(json.RawMessage, len(data))
	copy(r.Raw, data)
	return nil
}

func (r RawItem) MarshalJSON() ([]byte, error) {
	if len(r.Raw) == 0 {
		return []byte("null"), nil
	}
	out := make([]byte, len(r.Raw))
	copy(out, r.Raw)
	return out, nil
}

// ItemAgentMessageDeltaParams: server→client "item/agentMessage/delta".
//
// Corpus evidence: {"threadId":"...","turnId":"...","itemId":"msg_...","delta":"ok"}
type ItemAgentMessageDeltaParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
	Extra    map[string]json.RawMessage `json:"-"`
}

var itemAgentMessageDeltaParamsKnown = map[string]bool{
	"threadId": true, "turnId": true, "itemId": true, "delta": true,
}

func (p *ItemAgentMessageDeltaParams) UnmarshalJSON(data []byte) error {
	type alias ItemAgentMessageDeltaParams
	if err := json.Unmarshal(data, (*alias)(p)); err != nil {
		return err
	}
	return parseExtra(data, itemAgentMessageDeltaParamsKnown, &p.Extra)
}

func (p ItemAgentMessageDeltaParams) MarshalJSON() ([]byte, error) {
	type alias ItemAgentMessageDeltaParams
	b, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, p.Extra)
}

// ThreadTokenUsageUpdatedParams: server→client "thread/tokenUsage/updated".
//
// Corpus evidence:
//   {"threadId":"...","turnId":"...","tokenUsage":{"total":{...},"last":{...},"modelContextWindow":258400}}
type ThreadTokenUsageUpdatedParams struct {
	ThreadID   string     `json:"threadId"`
	TurnID     string     `json:"turnId"`
	TokenUsage TokenUsage `json:"tokenUsage"`
	Extra      map[string]json.RawMessage `json:"-"`
}

var threadTokenUsageUpdatedParamsKnown = map[string]bool{
	"threadId": true, "turnId": true, "tokenUsage": true,
}

func (p *ThreadTokenUsageUpdatedParams) UnmarshalJSON(data []byte) error {
	type alias ThreadTokenUsageUpdatedParams
	if err := json.Unmarshal(data, (*alias)(p)); err != nil {
		return err
	}
	return parseExtra(data, threadTokenUsageUpdatedParamsKnown, &p.Extra)
}

func (p ThreadTokenUsageUpdatedParams) MarshalJSON() ([]byte, error) {
	type alias ThreadTokenUsageUpdatedParams
	b, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, p.Extra)
}

// TokenUsage holds cumulative and per-turn token counts.
type TokenUsage struct {
	Total              TokenCounts `json:"total"`
	Last               TokenCounts `json:"last"`
	ModelContextWindow int64       `json:"modelContextWindow"`
	Extra              map[string]json.RawMessage `json:"-"`
}

var tokenUsageKnown = map[string]bool{"total": true, "last": true, "modelContextWindow": true}

func (u *TokenUsage) UnmarshalJSON(data []byte) error {
	type alias TokenUsage
	if err := json.Unmarshal(data, (*alias)(u)); err != nil {
		return err
	}
	return parseExtra(data, tokenUsageKnown, &u.Extra)
}

func (u TokenUsage) MarshalJSON() ([]byte, error) {
	type alias TokenUsage
	b, err := json.Marshal(alias(u))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, u.Extra)
}

// TokenCounts holds per-event token count fields.
//
// Corpus: {totalTokens, inputTokens, cachedInputTokens, outputTokens, reasoningOutputTokens}
type TokenCounts struct {
	TotalTokens           int64 `json:"totalTokens"`
	InputTokens           int64 `json:"inputTokens"`
	CachedInputTokens     int64 `json:"cachedInputTokens"`
	OutputTokens          int64 `json:"outputTokens"`
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
	Extra                 map[string]json.RawMessage `json:"-"`
}

var tokenCountsKnown = map[string]bool{
	"totalTokens": true, "inputTokens": true, "cachedInputTokens": true,
	"outputTokens": true, "reasoningOutputTokens": true,
}

func (c *TokenCounts) UnmarshalJSON(data []byte) error {
	type alias TokenCounts
	if err := json.Unmarshal(data, (*alias)(c)); err != nil {
		return err
	}
	return parseExtra(data, tokenCountsKnown, &c.Extra)
}

func (c TokenCounts) MarshalJSON() ([]byte, error) {
	type alias TokenCounts
	b, err := json.Marshal(alias(c))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, c.Extra)
}

// AccountRateLimitsUpdatedParams: server→client "account/rateLimits/updated".
type AccountRateLimitsUpdatedParams struct {
	RateLimits RateLimits `json:"rateLimits"`
	Extra      map[string]json.RawMessage `json:"-"`
}

var accountRateLimitsUpdatedParamsKnown = map[string]bool{"rateLimits": true}

func (p *AccountRateLimitsUpdatedParams) UnmarshalJSON(data []byte) error {
	type alias AccountRateLimitsUpdatedParams
	if err := json.Unmarshal(data, (*alias)(p)); err != nil {
		return err
	}
	return parseExtra(data, accountRateLimitsUpdatedParamsKnown, &p.Extra)
}

func (p AccountRateLimitsUpdatedParams) MarshalJSON() ([]byte, error) {
	type alias AccountRateLimitsUpdatedParams
	b, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, p.Extra)
}

// RateLimits holds the rate-limit block from "account/rateLimits/updated".
//
// Corpus:
//   {"limitId":"codex","limitName":null,"primary":{...},"secondary":{...},
//    "credits":null,"individualLimit":null,"planType":"prolite","rateLimitReachedType":null}
type RateLimits struct {
	LimitID             string           `json:"limitId"`
	LimitName           json.RawMessage  `json:"limitName"`           // null in corpus
	Primary             RateLimitWindow  `json:"primary"`
	Secondary           RateLimitWindow  `json:"secondary"`
	Credits             json.RawMessage  `json:"credits"`             // null in corpus
	IndividualLimit     json.RawMessage  `json:"individualLimit"`     // null in corpus
	PlanType            string           `json:"planType"`
	RateLimitReachedType json.RawMessage `json:"rateLimitReachedType"` // null in corpus
	Extra               map[string]json.RawMessage `json:"-"`
}

var rateLimitsKnown = map[string]bool{
	"limitId": true, "limitName": true, "primary": true, "secondary": true,
	"credits": true, "individualLimit": true, "planType": true, "rateLimitReachedType": true,
}

func (r *RateLimits) UnmarshalJSON(data []byte) error {
	type alias RateLimits
	if err := json.Unmarshal(data, (*alias)(r)); err != nil {
		return err
	}
	return parseExtra(data, rateLimitsKnown, &r.Extra)
}

func (r RateLimits) MarshalJSON() ([]byte, error) {
	type alias RateLimits
	b, err := json.Marshal(alias(r))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, r.Extra)
}

// RateLimitWindow is a primary or secondary rate-limit window.
//
// Corpus: {"usedPercent":0,"windowDurationMins":300,"resetsAt":1783847730}
type RateLimitWindow struct {
	UsedPercent       float64 `json:"usedPercent"`
	WindowDurationMins int64  `json:"windowDurationMins"`
	ResetsAt          int64   `json:"resetsAt"`
	Extra             map[string]json.RawMessage `json:"-"`
}

var rateLimitWindowKnown = map[string]bool{
	"usedPercent": true, "windowDurationMins": true, "resetsAt": true,
}

func (w *RateLimitWindow) UnmarshalJSON(data []byte) error {
	type alias RateLimitWindow
	if err := json.Unmarshal(data, (*alias)(w)); err != nil {
		return err
	}
	return parseExtra(data, rateLimitWindowKnown, &w.Extra)
}

func (w RateLimitWindow) MarshalJSON() ([]byte, error) {
	type alias RateLimitWindow
	b, err := json.Marshal(alias(w))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, w.Extra)
}

// RegisteredMethods returns the sorted list of method strings in the registry.
// Used by tests and diagnostics.
func RegisteredMethods() []string {
	methods := make([]string, 0, len(methodRegistry))
	for m := range methodRegistry {
		methods = append(methods, m)
	}
	// Stable sort for deterministic output.
	sortStrings(methods)
	return methods
}

// sortStrings sorts ss in-place (insertion sort; small slice, no import needed).
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

// TurnCompletedParams: server→client "turn/completed".
//
// Corpus evidence: {"threadId":"...","turn":{...}}
type TurnCompletedParams struct {
	ThreadID string `json:"threadId"`
	Turn     Turn   `json:"turn"`
	Extra    map[string]json.RawMessage `json:"-"`
}

var turnCompletedParamsKnown = map[string]bool{"threadId": true, "turn": true}

func (p *TurnCompletedParams) UnmarshalJSON(data []byte) error {
	type alias TurnCompletedParams
	if err := json.Unmarshal(data, (*alias)(p)); err != nil {
		return err
	}
	return parseExtra(data, turnCompletedParamsKnown, &p.Extra)
}

func (p TurnCompletedParams) MarshalJSON() ([]byte, error) {
	type alias TurnCompletedParams
	b, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, p.Extra)
}
