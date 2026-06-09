package daemon

// codexjsonlparser.go — codex `exec --json` JSONL event parser (codex-harness C2/T8, hk-m57va).
//
// `codex exec --json` is a one-shot run-to-exit invocation that streams a
// newline-delimited JSON (JSONL) event log to stdout. Unlike claude, codex has
// no TUI/paste/`/quit` path: it self-terminates on turn completion. So the
// codex run-state has no minted session ID — the thread identifier is CAPTURED
// from the first `thread.started` event and recorded so the next turn can be
// launched with `codex exec resume <thread_id>` (buildCodexLaunchSpec resume
// path, hk-rgxwd C2/T7).
//
// This file owns two units, both standalone (T12 / hk-xhawy wires them into the
// dispatch cascade):
//
//  1. parseCodexJSONLEvent — decodes a single JSONL line into a codexEvent.
//  2. codexRunArtifacts + captureCodexThreadID — the run-state holder for the
//     captured thread_id and the helper that updates it from a parsed event
//     stream (first thread.started wins; subsequent ones are ignored).
//
// Event-shape reference (codex exec --json, observed): each line is a JSON
// object with a top-level "type" discriminator. The events this parser models:
//
//   {"type":"thread.started","thread_id":"th_abc123"}
//   {"type":"turn.started","turn_id":"tr_1"}
//   {"type":"turn.completed","turn_id":"tr_1","usage":{...}}
//   {"type":"turn.failed","turn_id":"tr_1","error":{"message":"..."}}
//
// The parser is intentionally permissive about unknown event types and unknown
// fields: codex emits many item.* / token-count events the harness does not act
// on, so an unrecognised "type" yields a codexEvent with Kind ==
// CodexEventKindOther rather than an error. A genuinely malformed line (not a
// JSON object) is the only parse error.
//
// Spec refs:
//   - .kerf/works/codex-harness/05-specs/C2-codex-adapter-spec.md (adapter shape)
//   - specs/harness-contract.md §2 N3 (SessionIDCaptured policy)
// Bead: hk-m57va [C2/T8]

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// codexEventKind classifies a parsed codex JSONL event into the small set of
// kinds the harness acts on. Every codex event whose "type" the harness does
// not model maps to CodexEventKindOther; the raw type string is preserved in
// codexEvent.RawType for diagnostics.
type codexEventKind int

const (
	// CodexEventKindOther is any codex event the harness does not specifically
	// model (item.*, token counts, reasoning deltas, etc.). RawType carries the
	// original discriminator.
	CodexEventKindOther codexEventKind = iota

	// CodexEventKindThreadStarted is the "thread.started" event. ThreadID carries
	// the captured codex thread identifier used for `codex exec resume`.
	CodexEventKindThreadStarted

	// CodexEventKindTurnStarted is the "turn.started" event. TurnID carries the
	// codex turn identifier when present.
	CodexEventKindTurnStarted

	// CodexEventKindTurnCompleted is the "turn.completed" event signalling a clean
	// turn boundary. For codex this is the harness's analog of agent_completed:
	// the process exits shortly after.
	CodexEventKindTurnCompleted

	// CodexEventKindTurnFailed is the "turn.failed" event. ErrorMessage carries the
	// codex-reported failure reason when present.
	CodexEventKindTurnFailed
)

// String renders a codexEventKind for diagnostics and test messages.
func (k codexEventKind) String() string {
	switch k {
	case CodexEventKindThreadStarted:
		return "thread.started"
	case CodexEventKindTurnStarted:
		return "turn.started"
	case CodexEventKindTurnCompleted:
		return "turn.completed"
	case CodexEventKindTurnFailed:
		return "turn.failed"
	case CodexEventKindOther:
		return "other"
	default:
		return fmt.Sprintf("codexEventKind(%d)", int(k))
	}
}

// codexEvent is the parsed form of a single codex JSONL line.
//
// Only the fields relevant to a given Kind are populated; the rest are zero. The
// RawType field always carries the original "type" discriminator so callers can
// log or branch on event types the harness does not yet model.
type codexEvent struct {
	// Kind is the classified event kind. CodexEventKindOther for unmodelled types.
	Kind codexEventKind

	// RawType is the verbatim "type" discriminator from the JSONL line. Empty
	// only if the line carried no "type" field.
	RawType string

	// ThreadID is the codex thread identifier. Populated for
	// CodexEventKindThreadStarted; empty otherwise.
	ThreadID string

	// TurnID is the codex turn identifier. Populated for turn.* events when the
	// line carries one; may be empty if codex omits it.
	TurnID string

	// ErrorMessage is the codex-reported failure reason. Populated for
	// CodexEventKindTurnFailed when the line carries an error object; empty
	// otherwise.
	ErrorMessage string
}

// codexJSONLLine is the on-the-wire shape of a codex exec --json line. It is a
// superset decode target: every field the parser reads is optional, so a line
// missing any of them decodes cleanly (json leaves absent fields zero).
//
// codex nests the thread id directly on the thread.started line and the turn id
// on turn.* lines. The error object on turn.failed carries a "message" string.
type codexJSONLLine struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// parseCodexJSONLEvent decodes one codex JSONL line into a codexEvent.
//
// It returns an error only when line is not a JSON object (a genuinely malformed
// line). An unrecognised "type" discriminator is NOT an error: it yields a
// codexEvent with Kind == CodexEventKindOther and RawType set to the original
// value, so the caller can skip events the harness does not model without
// aborting the stream.
//
// Leading/trailing whitespace is tolerated. A blank line (after trimming) is
// treated as malformed: callers should skip blank lines before calling this.
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
		ev.Kind = CodexEventKindThreadStarted
		ev.ThreadID = raw.ThreadID
	case "turn.started":
		ev.Kind = CodexEventKindTurnStarted
		ev.TurnID = raw.TurnID
	case "turn.completed":
		ev.Kind = CodexEventKindTurnCompleted
		ev.TurnID = raw.TurnID
	case "turn.failed":
		ev.Kind = CodexEventKindTurnFailed
		ev.TurnID = raw.TurnID
		if raw.Error != nil {
			ev.ErrorMessage = raw.Error.Message
		}
	default:
		ev.Kind = CodexEventKindOther
	}

	return ev, nil
}

// codexRunArtifacts is the codex analog of claudeRunArtifacts: it holds the
// per-run state the workloop records from a codex turn so the next turn can be
// dispatched.
//
// Where claudeRunArtifacts.claudeSessionID is MINTED before launch, codex's
// thread identifier is CAPTURED from the first thread.started event in the
// JSONL stream (SessionIDCaptured policy, specs/harness-contract.md §2 N3). The
// caller stores capturedThreadID and passes it as priorThreadID on the next
// `codex exec resume <thread_id>` launch.
//
// T12 (hk-xhawy) threads this struct through the dispatch cascade; in T8 it is a
// standalone unit populated by captureCodexThreadID.
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
}

// captureCodexThreadID folds a parsed codexEvent into the run artifacts.
//
// The first thread.started event populates capturedThreadID; subsequent
// thread.started events are ignored (first wins) so a resumed turn that re-emits
// thread.started does not clobber the original thread id. turn.completed and
// turn.failed set the corresponding flags.
//
// Returns true iff the event mutated arts (i.e. it carried newly-captured
// thread/turn state). Callers may use the return value to detect the
// thread-id-capture boundary.
func captureCodexThreadID(arts *codexRunArtifacts, ev codexEvent) bool {
	switch ev.Kind {
	case CodexEventKindThreadStarted:
		// First thread.started wins; ignore later ones (and empty ids).
		if arts.capturedThreadID == "" && ev.ThreadID != "" {
			arts.capturedThreadID = ev.ThreadID
			return true
		}
		return false
	case CodexEventKindTurnCompleted:
		if !arts.turnCompleted {
			arts.turnCompleted = true
			return true
		}
		return false
	case CodexEventKindTurnFailed:
		if !arts.turnFailed {
			arts.turnFailed = true
			arts.turnFailureMessage = ev.ErrorMessage
			return true
		}
		return false
	default:
		return false
	}
}
