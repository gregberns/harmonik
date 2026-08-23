package pi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

type piEventKind int

const (
	piEventKindOther piEventKind = iota

	piEventKindSession

	piEventKindAgentEnd

	piEventKindMessageStart

	piEventKindMessageEnd
)

type piTokenUsage struct {
	InputTokens  int64
	OutputTokens int64
}

type piEvent struct {
	// Kind is the classified event kind. piEventKindOther for unmodelled types.
	Kind piEventKind

	// RawType is the verbatim "type" discriminator from the NDJSON line.
	RawType string

	// SessionID is the Pi session identifier. Populated for piEventKindSession;
	// empty otherwise.
	SessionID string

	// Usage carries the token counts extracted from this event. Populated for
	// piEventKindMessageStart (input_tokens), piEventKindMessageEnd (output_tokens),
	// and piEventKindAgentEnd (totals summed across all messages). Zero otherwise.
	Usage piTokenUsage

	// WillRetry reports that Pi announced this agent_end only to start its own
	// retry, so the event is not terminal. Populated for piEventKindAgentEnd;
	// false for every other kind and for an agent_end that ends the run.
	WillRetry bool
}

type piUsageField struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type piMessageStartBody struct {
	Usage *piUsageField `json:"usage"`
}

type piAgentMessage struct {
	Role  string        `json:"role"`
	Usage *piUsageField `json:"usage"`
}

type piNDJSONLine struct {
	Type      string `json:"type"`
	SessionID string `json:"id"`
	// message_start: usage is nested under "message"
	Message *piMessageStartBody `json:"message"`
	// message_end: usage is at the top level
	Usage *piUsageField `json:"usage"`
	// agent_end: array of all conversation messages, each may carry usage
	Messages []piAgentMessage `json:"messages"`
	// agent_end: true when pi is about to retry, so this line is not terminal
	WillRetry bool `json:"willRetry"`
}

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
		ev.WillRetry = raw.WillRetry
		for _, msg := range raw.Messages {
			if msg.Usage != nil {
				ev.Usage.InputTokens += msg.Usage.InputTokens
				ev.Usage.OutputTokens += msg.Usage.OutputTokens
			}
		}
	case "message_start":
		ev.Kind = piEventKindMessageStart
		if raw.Message != nil && raw.Message.Usage != nil {
			ev.Usage.InputTokens = raw.Message.Usage.InputTokens
			ev.Usage.OutputTokens = raw.Message.Usage.OutputTokens
		}
	case "message_end":
		ev.Kind = piEventKindMessageEnd
		if raw.Usage != nil {
			ev.Usage.InputTokens = raw.Usage.InputTokens
			ev.Usage.OutputTokens = raw.Usage.OutputTokens
		}
	default:
		ev.Kind = piEventKindOther
	}
	return ev, nil
}

type piRunArtifacts struct {
	// TotalUsage is the accumulated token consumption across all turns.
	// InputTokens is summed from message_start events; OutputTokens from
	// message_end events.
	TotalUsage piTokenUsage
}

func capturePiUsage(arts *piRunArtifacts, ev piEvent) bool {
	switch ev.Kind {
	case piEventKindMessageStart:
		if ev.Usage.InputTokens == 0 {
			return false
		}
		arts.TotalUsage.InputTokens += ev.Usage.InputTokens
		return true
	case piEventKindMessageEnd:
		if ev.Usage.OutputTokens == 0 {
			return false
		}
		arts.TotalUsage.OutputTokens += ev.Usage.OutputTokens
		return true
	default:
		return false
	}
}

type piSessionIDInterceptor struct {
	mu                 sync.Mutex
	inner              io.Reader
	buf                bytes.Buffer
	sessionIDFiredOnce bool
	agentEndFiredOnce  bool
	sessionIDCb        func(string)
	agentEndCb         func()
}

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
		if !p.agentEndFiredOnce && ev.Kind == piEventKindAgentEnd && !ev.WillRetry {
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
