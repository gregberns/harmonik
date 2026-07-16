// Pi NDJSON emitter for harmonik-twin-pi (M6 WS3-pi).
//
// This file implements the stdout NDJSON emitter that the pi twin uses to
// simulate `pi --mode json` output. The format mirrors the real pi surface the
// daemon parser consumes (internal/daemon/pijsonlparser.go):
//
//	session → message_start → message_end → agent_end
//
// Each write serialises one compact JSON object followed by a single newline,
// matching pi's NDJSON framing. Field names and nesting are exactly what
// parsePiNDJSONEvent decodes: usage under "message.usage" for message_start,
// top-level "usage" for message_end, and a "messages" array carrying per-message
// "usage" for agent_end.
package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// piEmitter writes pi NDJSON events to an io.Writer (typically os.Stdout).
// NOT goroutine-safe.
type piEmitter struct {
	w io.Writer
}

// newPiEmitter wraps w in a piEmitter.
func newPiEmitter(w io.Writer) *piEmitter {
	return &piEmitter{w: w}
}

// emitRaw marshals v as compact JSON followed by a newline.
func (e *piEmitter) emitRaw(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("piEmitter.emitRaw: marshal: %w", err)
	}
	b = append(b, '\n')
	_, err = e.w.Write(b)
	return err
}

// emitSession emits {"type":"session","version":3,"id":"<id>","cwd":"<cwd>"}.
//
// This is the first line pi emits on `--mode json` launch. The daemon captures
// the session id from it (PI-012; SessionIDPolicy==SessionIDCaptured) for the
// --session resume argv on the next turn.
func (e *piEmitter) emitSession(id, cwd string) error {
	return e.emitRaw(map[string]any{
		"type":    "session",
		"version": 3,
		"id":      id,
		"cwd":     cwd,
	})
}

// emitMessageStart emits {"type":"message_start","message":{"usage":{"input_tokens":N,...}}}.
//
// input_tokens carries the prompt cost for the turn (capturePiUsage folds only
// InputTokens from message_start).
func (e *piEmitter) emitMessageStart(inputTokens int64) error {
	return e.emitRaw(map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"usage": map[string]any{
				"input_tokens":  inputTokens,
				"output_tokens": 0,
			},
		},
	})
}

// emitMessageEnd emits {"type":"message_end","usage":{"output_tokens":N}}.
//
// output_tokens carries the completion cost for the turn (capturePiUsage folds
// only OutputTokens from message_end).
func (e *piEmitter) emitMessageEnd(outputTokens int64) error {
	return e.emitRaw(map[string]any{
		"type": "message_end",
		"usage": map[string]any{
			"input_tokens":  0,
			"output_tokens": outputTokens,
		},
	})
}

// emitAgentEnd emits the terminal
// {"type":"agent_end","messages":[{"role":"assistant","usage":{...}}]}.
//
// The messages array carries the run-level usage that parsePiNDJSONEvent sums
// across all messages as a cross-check of the per-turn message_start/end totals.
// The daemon's agent_end watcher (PI-014) observes this event to invoke Teardown
// because pi's process exit is unreliable.
func (e *piEmitter) emitAgentEnd(inputTokens, outputTokens int64) error {
	return e.emitRaw(map[string]any{
		"type": "agent_end",
		"messages": []map[string]any{
			{
				"role": "assistant",
				"usage": map[string]any{
					"input_tokens":  inputTokens,
					"output_tokens": outputTokens,
				},
			},
		},
	})
}
