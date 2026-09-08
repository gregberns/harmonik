// Package herdrwire is a pure-$gostd client for herdr's unix-socket JSON API
// (protocol 22, herdr 0.9.0). It is the wire layer only: dial-per-call
// request/response, typed params and results for the P0 method set the
// keeper needs, and a subscribe path for the two events the keeper watches.
// It has no keeper wiring and no PaneHost dependency — see
// plans/2026-09-07-keeper-herdr-substrate/README.md parcel KH-2.
//
// # Wire shape
//
// Every call opens a fresh unix-socket connection, writes one newline-
// delimited JSON request line, reads one newline-delimited JSON response
// line, and closes. herdr answers and closes its end too — this package
// never reuses a connection across calls. The one exception is
// events.subscribe, which holds the connection open and streams event
// envelopes until Close: see Subscribe.
//
// Request:  {"id":"<n>","method":"<name>","params":{...}}
// Success:  {"id":"<n>","result":{...}}
// Error:    {"id":"<n>","error":{"code":"...","message":"..."}}
//
// # Protocol pin
//
// The client pins protocol 22 (the version this package was written
// against) and checks it on the first call a Client makes, via an implicit
// "ping". A mismatch fails closed with *ProtocolMismatchError rather than
// attempting the call anyway — see CheckProtocol.
//
// # Agent-name limit (resolves README §10 open question 3)
//
// Verified against the live herdr 0.9.0 server on this box (protocol 22):
// agent.start rejects a name that is not 1-32 characters of
// [a-z][a-z0-9_-]* with error code "invalid_agent_name" and message "agent
// name must start with a lowercase letter and contain only lowercase
// letters, digits, '-' or '_' (1-32 characters)". The rejection fires before
// any other validation (an invalid name is refused even with a nonexistent
// agent kind), and a 32-character name of that shape is accepted.
//
// This is tighter than the harmonik-<hash12>-[crew-]<name> convention:
// "harmonik-" (9) + a 12-hex hash (12) already spends 21 of the 32
// characters before any crew name, and "-crew-" (6) leaves only 5 for a
// crew name if a hash is kept. herdrhost (KH-3) must derive a
// name that fits 32 characters and the charset above — it cannot reuse the
// tmux HarmonikSessionName/HarmonikCrewSessionName convention verbatim. That
// derivation is KH-3's job; this package only documents the limit it must
// respect.
package herdrwire

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ProtocolVersion is the herdr wire protocol this package was written
// against and pins at dial time.
const ProtocolVersion = 22

// WireError is the {"code","message"} error body herdr returns in place of
// a result. It is the typed shape for every application-level failure the
// server reports (bad params, unknown pane/agent, unsupported kind, ...).
type WireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *WireError) Error() string {
	return fmt.Sprintf("herdrwire: %s: %s", e.Code, e.Message)
}

// ProtocolMismatchError reports that the connected herdr server speaks a
// different wire protocol than this package pins. Every call fails closed
// on this error rather than proceeding against an unverified wire shape.
type ProtocolMismatchError struct {
	Got  uint32
	Want uint32
}

func (e *ProtocolMismatchError) Error() string {
	return fmt.Sprintf("herdrwire: protocol mismatch: server speaks %d, client pins %d", e.Got, e.Want)
}

// DialError wraps a failure to connect to the herdr socket (e.g. connection
// refused because no server is listening, or the socket path is stale).
type DialError struct {
	SockPath string
	Err      error
}

func (e *DialError) Error() string {
	return fmt.Sprintf("herdrwire: dial %s: %v", e.SockPath, e.Err)
}

func (e *DialError) Unwrap() error { return e.Err }

// FrameError reports that a line read from the socket could not be decoded
// as a herdr response envelope (malformed JSON, or valid JSON that is
// neither a success nor an error response).
type FrameError struct {
	Line []byte
	Err  error
}

func (e *FrameError) Error() string {
	return fmt.Sprintf("herdrwire: decode response %q: %v", truncateForError(e.Line), e.Err)
}

func (e *FrameError) Unwrap() error { return e.Err }

func truncateForError(b []byte) string {
	const maxLen = 200
	if len(b) <= maxLen {
		return string(b)
	}
	return string(b[:maxLen]) + "...(truncated)"
}

// Client is a herdrwire client bound to one herdr unix socket path. It holds
// no persistent connection — every call dials fresh — and is safe for
// concurrent use.
type Client struct {
	sockPath string
	timeout  time.Duration // per-call round-trip budget when ctx carries no deadline

	nextID uint64

	protocolOnce sync.Mutex
	protocolOK   bool
}

// DefaultTimeout bounds a call when the caller's context has no deadline.
// herdr answers over a local unix socket, so this is generous headroom
// against a wedged server, not a normal-path budget.
const DefaultTimeout = 10 * time.Second

// NewClient returns a Client bound to sockPath. It performs no I/O; the
// first call checks the protocol (see CheckProtocol) before issuing itself.
func NewClient(sockPath string) *Client {
	return &Client{sockPath: sockPath, timeout: DefaultTimeout}
}

type wireRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type wireResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *WireError      `json:"error,omitempty"`
}

func (c *Client) nextRequestID() string {
	n := atomic.AddUint64(&c.nextID, 1)
	return fmt.Sprintf("hkw-%d", n)
}

// deadline resolves the effective absolute deadline for one call: the
// context's deadline if it has one, else now+c.timeout.
func (c *Client) deadline(ctx context.Context) time.Time {
	if dl, ok := ctx.Deadline(); ok {
		return dl
	}
	return time.Now().Add(c.timeout)
}

// dial opens one fresh connection to the herdr socket, fail-closed on
// refusal (no server, stale socket path, permission denied, ...).
func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	dl := c.deadline(ctx)
	dialCtx, cancel := context.WithDeadline(ctx, dl)
	defer cancel()
	conn, err := d.DialContext(dialCtx, "unix", c.sockPath)
	if err != nil {
		return nil, &DialError{SockPath: c.sockPath, Err: err}
	}
	if err := conn.SetDeadline(dl); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			slog.WarnContext(ctx, "herdrwire: close conn after SetDeadline failure", "err", closeErr)
		}
		return nil, &DialError{SockPath: c.sockPath, Err: err}
	}
	return conn, nil
}

// call performs one dial-per-call request/response round trip: dial, write
// the request line, read exactly one response line, close. result is
// json.Unmarshal'd from the "result" field on success; pass nil when the
// method's result carries no data the caller needs.
func (c *Client) call(ctx context.Context, method string, params, result any) error {
	if method != "ping" {
		if err := c.checkProtocolOnce(ctx); err != nil {
			return err
		}
	}
	return c.rawCall(ctx, method, params, result)
}

func (c *Client) rawCall(ctx context.Context, method string, params, result any) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			slog.WarnContext(ctx, "herdrwire: close conn after call", "method", method, "err", closeErr)
		}
	}()

	paramsRaw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("herdrwire: marshal params for %s: %w", method, err)
	}
	req := wireRequest{ID: c.nextRequestID(), Method: method, Params: paramsRaw}
	line, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("herdrwire: marshal request for %s: %w", method, err)
	}
	line = append(line, '\n')
	if _, err := conn.Write(line); err != nil {
		return &DialError{SockPath: c.sockPath, Err: fmt.Errorf("write %s: %w", method, err)}
	}

	reader := bufio.NewReader(conn)
	respLine, err := reader.ReadBytes('\n')
	if err != nil && len(respLine) == 0 {
		return &DialError{SockPath: c.sockPath, Err: fmt.Errorf("read response for %s: %w", method, err)}
	}

	var resp wireResponse
	if jsonErr := json.Unmarshal(respLine, &resp); jsonErr != nil {
		return &FrameError{Line: respLine, Err: jsonErr}
	}
	if resp.Error != nil {
		return resp.Error
	}
	if result == nil {
		return nil
	}
	if len(resp.Result) == 0 {
		return &FrameError{Line: respLine, Err: fmt.Errorf("response for %s carries no result", method)}
	}
	if err := json.Unmarshal(resp.Result, result); err != nil {
		return &FrameError{Line: respLine, Err: fmt.Errorf("decode result for %s: %w", method, err)}
	}
	return nil
}

// checkProtocolOnce pings once per Client lifetime and caches success. A
// mismatch is never cached, so a later call retries the check — a herdr
// server can be upgraded to the pinned protocol without recreating the
// Client. Concurrent callers before the first success each pay one ping;
// that is bounded and cheap on a local socket, and simpler than a
// singleflight for a fail-safe check.
func (c *Client) checkProtocolOnce(ctx context.Context) error {
	c.protocolOnce.Lock()
	ok := c.protocolOK
	c.protocolOnce.Unlock()
	if ok {
		return nil
	}
	return c.CheckProtocol(ctx)
}

// CheckProtocol pings the server and fails closed with *ProtocolMismatchError
// when it does not speak ProtocolVersion. Safe to call directly (e.g. from
// `keeper doctor`); ordinary Client methods call it lazily on first use.
func (c *Client) CheckProtocol(ctx context.Context) error {
	var pong PingResult
	if err := c.rawCall(ctx, "ping", struct{}{}, &pong); err != nil {
		return err
	}
	if pong.Protocol != ProtocolVersion {
		return &ProtocolMismatchError{Got: pong.Protocol, Want: ProtocolVersion}
	}
	c.protocolOnce.Lock()
	c.protocolOK = true
	c.protocolOnce.Unlock()
	return nil
}
