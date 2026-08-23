package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

type codexEventKind int

const (
	// EventKindOther is any codex event the harness does not specifically
	// model (item.*, token counts, reasoning deltas, etc.). RawType carries the
	// original discriminator.
	EventKindOther codexEventKind = iota

	// EventKindThreadStarted is the "thread.started" event. ThreadID carries
	// the captured codex thread identifier used for `codex exec resume`.
	EventKindThreadStarted

	// EventKindTurnStarted is the "turn.started" event. TurnID carries the
	// codex turn identifier when present.
	EventKindTurnStarted

	// EventKindTurnCompleted is the "turn.completed" event signalling a clean
	// turn boundary. For codex this is the harness's analog of agent_completed:
	// the process exits shortly after.
	EventKindTurnCompleted

	// EventKindTurnFailed is the "turn.failed" event. ErrorMessage carries the
	// codex-reported failure reason when present.
	EventKindTurnFailed
)

// String renders a codexEventKind for diagnostics and test messages.
func (k codexEventKind) String() string {
	switch k {
	case EventKindThreadStarted:
		return "thread.started"
	case EventKindTurnStarted:
		return "turn.started"
	case EventKindTurnCompleted:
		return "turn.completed"
	case EventKindTurnFailed:
		return "turn.failed"
	case EventKindOther:
		return "other"
	default:
		return fmt.Sprintf("codexEventKind(%d)", int(k))
	}
}

type codexEvent struct {
	// Kind is the classified event kind. EventKindOther for unmodelled types.
	Kind codexEventKind

	// RawType is the verbatim "type" discriminator from the JSONL line. Empty
	// only if the line carried no "type" field.
	RawType string

	// ThreadID is the codex thread identifier. Populated for
	// EventKindThreadStarted; empty otherwise.
	ThreadID string

	// TurnID is the codex turn identifier. Populated for turn.* events when the
	// line carries one; may be empty if codex omits it.
	TurnID string

	// ErrorMessage is the codex-reported failure reason. Populated for
	// EventKindTurnFailed when the line carries an error object; empty
	// otherwise.
	ErrorMessage string

	// Usage holds the token counts from a turn.completed event. Zero for all
	// other event kinds and when codex omits the usage object.
	Usage codexTokenUsage
}

type codexTokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type codexJSONLLine struct {
	Type     string           `json:"type"`
	ThreadID string           `json:"thread_id"`
	TurnID   string           `json:"turn_id"`
	Usage    *codexTokenUsage `json:"usage"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func parseCodexJSONLEvent(line []byte) (codexEvent, error) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return codexEvent{}, fmt.Errorf("parseCodexJSONLEvent: empty line")
	}

	var raw codexJSONLLine
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return codexEvent{}, fmt.Errorf("parseCodexJSONLEvent: decode line %q: %w", string(trimmed), err)
	}

	ev := codexEvent{
		RawType: raw.Type,
	}

	switch raw.Type {
	case "thread.started":
		ev.Kind = EventKindThreadStarted
		ev.ThreadID = raw.ThreadID
	case "turn.started":
		ev.Kind = EventKindTurnStarted
		ev.TurnID = raw.TurnID
	case "turn.completed":
		ev.Kind = EventKindTurnCompleted
		ev.TurnID = raw.TurnID
		if raw.Usage != nil {
			ev.Usage = *raw.Usage
		}
	case "turn.failed":
		ev.Kind = EventKindTurnFailed
		ev.TurnID = raw.TurnID
		if raw.Error != nil {
			ev.ErrorMessage = raw.Error.Message
		}
	default:
		ev.Kind = EventKindOther
	}

	return ev, nil
}

type codexRunArtifacts struct {
	// capturedThreadID is the codex thread identifier captured from the first
	// thread.started event of this run's JSONL stream. Empty until that event is
	// observed.
	capturedThreadID string

	// turnCompleted records whether a turn.completed event was observed for this
	// run. The codex completion mode is ProcessExit; turnCompleted is the
	// harness-side signal that the turn ended cleanly (analogous to
	// agent_completed) rather than via a crash before any boundary.
	turnCompleted bool

	// turnFailed records whether a turn.failed event was observed. When true,
	// turnFailureMessage carries the codex-reported reason.
	turnFailed bool

	// turnFailureMessage is the codex-reported failure reason from a turn.failed
	// event. Empty unless turnFailed is true (and even then may be empty if codex
	// omitted the error message).
	turnFailureMessage string

	// inputTokens is the input token count from the turn.completed usage object.
	// Zero when no usage was reported or the turn did not complete cleanly.
	inputTokens int

	// outputTokens is the output token count from the turn.completed usage object.
	// Zero when no usage was reported or the turn did not complete cleanly.
	outputTokens int
}

type codexThreadIDInterceptor struct {
	mu          sync.Mutex
	inner       io.Reader
	buf         bytes.Buffer
	cbFiredOnce bool // guards thread_id callback; does NOT stop scanning
	arts        codexRunArtifacts
	cb          func(string)
}

func newCodexThreadIDInterceptor(inner io.Reader, cb func(string)) *codexThreadIDInterceptor {
	return &codexThreadIDInterceptor{inner: inner, cb: cb}
}

// Read implements io.Reader. Bytes are passed through unchanged; each complete
// JSONL line is also parsed for the thread_id side-effect.
func (c *codexThreadIDInterceptor) Read(p []byte) (int, error) {
	n, err := c.inner.Read(p)
	if n > 0 {
		c.mu.Lock()
		c.buf.Write(p[:n])
		c.checkBuffer()
		c.mu.Unlock()
	}
	return n, err
}

func (c *codexThreadIDInterceptor) checkBuffer() {
	for {
		b := c.buf.Bytes()
		idx := bytes.IndexByte(b, '\n')
		if idx < 0 {
			break
		}
		line := make([]byte, idx)
		copy(line, b[:idx])
		c.buf.Next(idx + 1)

		if len(line) == 0 {
			continue
		}

		ev, err := parseCodexJSONLEvent(line)
		if err != nil {
			continue
		}
		captureCodexThreadID(&c.arts, ev)
		if !c.cbFiredOnce && c.arts.capturedThreadID != "" {
			c.cbFiredOnce = true
			c.cb(c.arts.capturedThreadID)
		}
	}
}

// TokenUsage returns the input and output token counts captured from the
// turn.completed usage object in the JSONL stream. Both values are zero until
// a turn.completed event with a usage field is observed. Safe to call
// concurrently or after the stream is exhausted.
func (c *codexThreadIDInterceptor) TokenUsage() (inputTokens, outputTokens int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.arts.inputTokens, c.arts.outputTokens
}

func captureCodexThreadID(arts *codexRunArtifacts, ev codexEvent) {
	switch ev.Kind {
	case EventKindThreadStarted:
		if arts.capturedThreadID == "" && ev.ThreadID != "" {
			arts.capturedThreadID = ev.ThreadID
		}
	case EventKindTurnCompleted:
		if !arts.turnCompleted {
			arts.turnCompleted = true
			arts.inputTokens = ev.Usage.InputTokens
			arts.outputTokens = ev.Usage.OutputTokens
		}
	case EventKindTurnFailed:
		if !arts.turnFailed {
			arts.turnFailed = true
			arts.turnFailureMessage = ev.ErrorMessage
		}
	default:
	}
}
