package daemon

// pijsonlparser.go — Pi `--mode json` NDJSON event parser (codename:pilot, PI-012/014).
//
// Pi `--mode json` streams newline-delimited JSON (NDJSON) to stdout. The first
// line is always a session header:
//
//	{"type":"session","version":3,"id":"<uuid>","cwd":"..."}
//
// The terminal event is agent_end:
//
//	{"type":"agent_end","messages":[...]}
//
// This file owns two units (mirroring codexjsonlparser.go for codex):
//
//  1. parsePiNDJSONEvent — decodes a single NDJSON line into a piEvent.
//  2. newPiSessionIDInterceptor — an io.Reader wrapper that:
//       - fires sessionIDCb once on the first {"type":"session",...} line (PI-012).
//       - fires agentEndCb once on {"type":"agent_end",...} (PI-014).
//     All bytes are passed through unchanged.
//
// The session id is the Pi analog of codex's thread_id. SessionIDPolicy() ==
// SessionIDCaptured; the captured id is passed as --session <id> on the next
// turn (resume argv, buildPiLaunchSpec). agentEndCb invokes Teardown→Kill because
// Pi's process exit is unreliable (#4303/#161/#4942); the 90m ceiling is backstop
// only (PI-014).
//
// Spec: specs/pi-harness.md §1 PI-012/PI-014.
// Design: ~/.kerf/projects/gregberns-harmonik/pilot/04-design/pi-harness-design.md §3.3/§3.4.
// Bead: hk-mkcwg (PI-014).

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

// piSessionIDInterceptor wraps an io.Reader (Pi NDJSON stdout) and fires two
// optional callbacks as Pi events arrive, passing all bytes through unchanged
// so the SpawnWatcher can process them normally.
//
//   - sessionIDCb fires at most once on the first {"type":"session",...} line
//     that carries a non-empty "id" field (PI-012). May be nil.
//   - agentEndCb fires at most once on the first {"type":"agent_end",...} line
//     (PI-014). Invokes Teardown→Kill so a hung Pi does not burn the 90m ceiling.
//     May be nil.
//
// This is the Pi analog of codexThreadIDInterceptor (which only captures a
// thread_id; codex self-exits reliably and has no agent_end event to watch).
// The interceptor continues scanning after sessionIDCb fires because agentEndCb
// may fire later; it stops only when both have fired (or the stream ends).
//
// Usage:
//
//	sessionIDCh := make(chan string, 1)
//	agentEndCb := func() { _ = harness.Teardown(sess) }
//	implSpec.StdoutWrapper = func(r io.Reader) io.Reader {
//	    return newPiSessionIDInterceptor(r, func(id string) { sessionIDCh <- id }, agentEndCb)
//	}
//
// Spec: PI-012 (session-id capture); PI-014 (agent_end watcher).
// Bead: hk-mkcwg.
type piSessionIDInterceptor struct {
	mu                 sync.Mutex
	inner              io.Reader
	buf                bytes.Buffer
	sessionIDFiredOnce bool
	agentEndFiredOnce  bool
	sessionIDCb        func(string)
	agentEndCb         func()
}

// newPiSessionIDInterceptor wraps inner and wires both callbacks.
// sessionIDCb fires on the first {"type":"session",...} line with a non-empty id.
// agentEndCb fires on the first {"type":"agent_end",...} line (PI-014).
// Either callback may be nil.
func newPiSessionIDInterceptor(inner io.Reader, sessionIDCb func(string), agentEndCb func()) *piSessionIDInterceptor {
	return &piSessionIDInterceptor{
		inner:       inner,
		sessionIDCb: sessionIDCb,
		agentEndCb:  agentEndCb,
	}
}

// Read implements io.Reader. Bytes are passed through unchanged; each complete
// NDJSON line is parsed for the session-id and agent_end side-effects (PI-012/014).
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

// checkBuffer scans p.buf for complete NDJSON lines and fires callbacks.
// Called with p.mu held.
//
// Scanning continues after sessionIDCb fires because agentEndCb fires later.
// Stops when both have fired or no more complete lines are available.
func (p *piSessionIDInterceptor) checkBuffer() {
	if p.sessionIDFiredOnce && p.agentEndFiredOnce {
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
		if !p.sessionIDFiredOnce && ev.Kind == piEventKindSession && ev.SessionID != "" {
			p.sessionIDFiredOnce = true
			if p.sessionIDCb != nil {
				p.sessionIDCb(ev.SessionID)
			}
		}
		if !p.agentEndFiredOnce && ev.Kind == piEventKindAgentEnd {
			p.agentEndFiredOnce = true
			if p.agentEndCb != nil {
				p.agentEndCb()
			}
		}
		if p.sessionIDFiredOnce && p.agentEndFiredOnce {
			return
		}
	}
}
