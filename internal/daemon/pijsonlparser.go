package daemon

// pijsonlparser.go — Pi `--mode json` NDJSON event parser (codename:pilot, PI-012).
//
// Pi `--mode json` streams newline-delimited JSON (NDJSON) to stdout. The first
// line is always a session header:
//
//	{"type":"session","version":3,"id":"<uuid>","cwd":"..."}
//
// This file owns two units (mirroring codexjsonlparser.go for codex):
//
//  1. parsePiNDJSONEvent — decodes a single NDJSON line into a piEvent.
//  2. newPiSessionIDInterceptor — an io.Reader wrapper that fires a callback
//     exactly once when it captures the session `id` from the first
//     `{"type":"session",...}` line. All bytes are passed through unchanged.
//
// The session id is the Pi analog of codex's thread_id. SessionIDPolicy() ==
// SessionIDCaptured; the captured id is passed as --session <id> on the next
// turn (resume argv, buildPiLaunchSpec). PI-014 (agent_end watcher) will
// extend this same interceptor type.
//
// Spec: specs/pi-harness.md §1 PI-012.
// Design: ~/.kerf/projects/gregberns-harmonik/pilot/04-design/pi-harness-design.md §3.3.
// Bead: hk-4rmj1 (PI-012).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// piEventKind classifies a parsed Pi NDJSON event into the small set of kinds
// the harness acts on. Events whose "type" the harness does not model map to
// piEventKindOther; the raw type string is preserved in piEvent.RawType.
type piEventKind int

const (
	// piEventKindOther is any Pi NDJSON event the harness does not specifically
	// model. RawType carries the original discriminator.
	piEventKindOther piEventKind = iota

	// piEventKindSession is the first NDJSON line Pi emits on `--mode json`
	// launch: `{"type":"session","version":3,"id":"<uuid>","cwd":"..."}`.
	// SessionID carries the captured Pi session identifier used for --session on
	// the resume turn.
	piEventKindSession

	// piEventKindAgentEnd is the terminal Pi NDJSON event:
	// `{"type":"agent_end","messages":[...]}`. PI-014 (agent_end watcher, a
	// later bead) observes this to invoke Teardown because Pi's process exit is
	// unreliable (#4303/#161/#4942).
	piEventKindAgentEnd
)

// piEvent is the parsed form of a single Pi NDJSON line.
//
// Only the fields relevant to a given Kind are populated; the rest are zero. The
// RawType field always carries the original "type" discriminator so callers can
// log or branch on event types the harness does not yet model.
type piEvent struct {
	// Kind is the classified event kind. piEventKindOther for unmodelled types.
	Kind piEventKind

	// RawType is the verbatim "type" discriminator from the NDJSON line.
	RawType string

	// SessionID is the Pi session identifier. Populated for piEventKindSession;
	// empty otherwise.
	SessionID string
}

// piNDJSONLine is the on-the-wire decode target for a Pi NDJSON line.
// All fields are optional (absent fields decode to zero).
type piNDJSONLine struct {
	Type      string `json:"type"`
	SessionID string `json:"id"`
}

// parsePiNDJSONEvent decodes one Pi NDJSON line into a piEvent.
//
// Returns an error only when the line is not a JSON object (genuinely malformed).
// An unrecognised "type" discriminator is NOT an error; it yields piEventKindOther.
// A blank line (after trimming) is treated as malformed; callers should skip them.
func parsePiNDJSONEvent(line []byte) (piEvent, error) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return piEvent{}, fmt.Errorf("parsePiNDJSONEvent: empty line")
	}

	var raw piNDJSONLine
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return piEvent{}, fmt.Errorf("parsePiNDJSONEvent: decode line %q: %w", string(trimmed), err)
	}

	ev := piEvent{RawType: raw.Type}
	switch raw.Type {
	case "session":
		ev.Kind = piEventKindSession
		ev.SessionID = raw.SessionID
	case "agent_end":
		ev.Kind = piEventKindAgentEnd
	default:
		ev.Kind = piEventKindOther
	}
	return ev, nil
}

// piSessionIDInterceptor wraps an io.Reader (Pi NDJSON stdout) and fires a
// callback exactly once when it captures a non-empty session id from the first
// `{"type":"session",...}` line in the stream. All bytes are passed through to
// callers unchanged so the SpawnWatcher can process them normally.
//
// This is the Pi analog of codexThreadIDInterceptor. It is line-buffered:
// bytes accumulate until a '\n' boundary is found, then the complete NDJSON
// line is parsed by parsePiNDJSONEvent. The callback fires at most once (first
// non-empty session id wins).
//
// PI-014 (agent_end watcher, a later bead) will extend this struct with an
// additional callback for the agent_end event, so it is NOT unexported via
// embedding — the type must remain extensible.
//
// Usage:
//
//	cb := func(sessionID string) { sessionIDCh <- sessionID }
//	implSpec.StdoutWrapper = func(r io.Reader) io.Reader {
//	    return newPiSessionIDInterceptor(r, cb)
//	}
//
// Spec: PI-012. Bead: hk-4rmj1.
type piSessionIDInterceptor struct {
	mu        sync.Mutex
	inner     io.Reader
	buf       bytes.Buffer
	firedOnce bool
	cb        func(string)
}

// newPiSessionIDInterceptor wraps inner and fires cb with the captured session
// id on the first `{"type":"session",...}` Pi NDJSON event.
func newPiSessionIDInterceptor(inner io.Reader, cb func(string)) *piSessionIDInterceptor {
	return &piSessionIDInterceptor{inner: inner, cb: cb}
}

// Read implements io.Reader. Bytes are passed through unchanged; each complete
// NDJSON line is also parsed for the session-id side-effect.
func (p *piSessionIDInterceptor) Read(b []byte) (int, error) {
	n, err := p.inner.Read(b)
	if n > 0 {
		p.mu.Lock()
		p.buf.Write(b[:n])
		p.checkBuffer()
		p.mu.Unlock()
	}
	return n, err
}

// checkBuffer scans p.buf for complete NDJSON lines and fires the callback on
// the first line that yields a non-empty session id. Called with p.mu held.
func (p *piSessionIDInterceptor) checkBuffer() {
	if p.firedOnce {
		return
	}
	for {
		b := p.buf.Bytes()
		idx := bytes.IndexByte(b, '\n')
		if idx < 0 {
			break
		}
		line := make([]byte, idx)
		copy(line, b[:idx])
		p.buf.Next(idx + 1)

		if len(line) == 0 {
			continue
		}

		ev, err := parsePiNDJSONEvent(line)
		if err != nil {
			continue
		}
		if ev.Kind == piEventKindSession && ev.SessionID != "" {
			p.firedOnce = true
			p.cb(ev.SessionID)
			return
		}
	}
}
