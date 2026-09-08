// Package herdrfake is an in-process, scriptable fake of herdr's unix-socket
// JSON API, for herdrwire's tests. It listens on a temp unix socket and
// hands each accepted connection to a caller-supplied handler, so a test
// can script exactly one behavior per connection: a normal reply, a
// malformed line, an early close, a deliberate delay past a deadline, or a
// wrong protocol number on ping.
//
// herdrfake is deliberately low-level (raw net.Conn in, nothing else) so
// tests can construct exactly the byte sequence a fault case needs, rather
// than being routed through another layer of typed request/response
// modeling that would hide the fault under test.
package herdrfake

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
)

// Handler processes one accepted connection. It owns the connection's
// lifecycle: closing it (or not) is part of the scripted behavior.
type Handler func(conn net.Conn)

// Server is a running fake herdr socket server.
type Server struct {
	ln   net.Listener
	Path string
	dir  string
}

// Start listens on a fresh temp unix socket and serves accepted
// connections with handler, one goroutine per connection, until Close.
func Start(handler Handler) (*Server, error) {
	dir, err := os.MkdirTemp("", "herdrfake-")
	if err != nil {
		return nil, err
	}
	sockPath := filepath.Join(dir, "herdr.sock")
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "unix", sockPath)
	if err != nil {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			slog.WarnContext(context.Background(), "herdrfake: remove temp dir after listen failure", "err", rmErr, "dir", dir)
		}
		return nil, err
	}
	s := &Server{ln: ln, Path: sockPath, dir: dir}
	go s.serve(handler)
	return s, nil
}

func (s *Server) serve(handler Handler) {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go handler(conn)
	}
}

// Close stops accepting new connections and removes the socket directory.
func (s *Server) Close() error {
	return errors.Join(s.ln.Close(), os.RemoveAll(s.dir))
}

// Request is one decoded {"id","method","params"} line, for handlers that
// want to read a request without hand-rolling the envelope decode.
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// ReadRequest reads and decodes one request line from conn. Handlers that
// need to inspect the method before deciding how to respond use this;
// handlers testing a malformed-input fault write raw bytes instead and
// never call it.
func ReadRequest(conn net.Conn) (Request, []byte, error) {
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return Request{}, line, err
	}
	var req Request
	if jsonErr := json.Unmarshal(line, &req); jsonErr != nil {
		return Request{}, line, jsonErr
	}
	return req, line, nil
}

// WriteResult writes one {"id","result":{...}} success line for id.
func WriteResult(conn net.Conn, id string, result any) error {
	resultRaw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	line, err := json.Marshal(struct {
		ID     string          `json:"id"`
		Result json.RawMessage `json:"result"`
	}{ID: id, Result: resultRaw})
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = conn.Write(line)
	return err
}

// WriteError writes one {"id","error":{"code","message"}} line for id.
func WriteError(conn net.Conn, id, code, message string) error {
	line, err := json.Marshal(struct {
		ID    string `json:"id"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{ID: id, Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message}})
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = conn.Write(line)
	return err
}

// WriteEvent writes one subscription-event line {"event","data":{...}}.
func WriteEvent(conn net.Conn, event string, data any) error {
	dataRaw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	line, err := json.Marshal(struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}{Event: event, Data: dataRaw})
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = conn.Write(line)
	return err
}

// MethodHandler answers one decoded request on its own connection.
type MethodHandler func(req Request, conn net.Conn)

// NewMethodServer starts a fake server that auto-answers "ping" with
// protocol, then dispatches every other method to the matching entry in
// methods (a "method_not_found" WireError for anything else). This is the
// shape nearly every test wants: one line to script the RPC under test,
// with the mandatory protocol-check ping handled for free.
func NewMethodServer(protocol uint32, methods map[string]MethodHandler) (*Server, error) {
	return Start(func(conn net.Conn) {
		defer func() {
			if closeErr := conn.Close(); closeErr != nil {
				slog.WarnContext(context.Background(), "herdrfake: close conn", "err", closeErr)
			}
		}()
		req, _, err := ReadRequest(conn)
		if err != nil {
			return
		}
		if req.Method == "ping" {
			if writeErr := WriteResult(conn, req.ID, PongResult(protocol)); writeErr != nil {
				slog.WarnContext(context.Background(), "herdrfake: write pong", "err", writeErr)
			}
			return
		}
		h, ok := methods[req.Method]
		if !ok {
			if writeErr := WriteError(conn, req.ID, "method_not_found", "fake: no handler for "+req.Method); writeErr != nil {
				slog.WarnContext(context.Background(), "herdrfake: write method_not_found", "err", writeErr)
			}
			return
		}
		h(req, conn)
	})
}

// PongResult builds the {"type":"pong","version","protocol"} result for a
// ping response — a shared shape used by nearly every fake-server test to
// answer the automatic protocol-check ping before its scripted behavior.
func PongResult(protocol uint32) any {
	return struct {
		Type     string `json:"type"`
		Version  string `json:"version"`
		Protocol uint32 `json:"protocol"`
	}{Type: "pong", Version: "0.9.0-fake", Protocol: protocol}
}
