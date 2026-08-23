// Pi NDJSON emitter for harmonik-twin-pi (M6 WS3-pi).
//
// This file implements the stdout NDJSON emitter that the pi twin uses to
// simulate `pi --mode json` output. The format mirrors the real pi surface the
// daemon parser consumes (internal/harness/pi/ndjsonparser.go):
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

type piEmitter struct {
	w io.Writer
}

func newPiEmitter(w io.Writer) *piEmitter {
	return &piEmitter{w: w}
}

func (e *piEmitter) emitRaw(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("piEmitter.emitRaw: marshal: %w", err)
	}
	b = append(b, '\n')
	_, err = e.w.Write(b)
	return err
}

func (e *piEmitter) emitSession(id, cwd string) error {
	return e.emitRaw(map[string]any{
		"type":    "session",
		"version": 3,
		"id":      id,
		"cwd":     cwd,
	})
}

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

func (e *piEmitter) emitMessageEnd(outputTokens int64) error {
	return e.emitRaw(map[string]any{
		"type": "message_end",
		"usage": map[string]any{
			"input_tokens":  0,
			"output_tokens": outputTokens,
		},
	})
}

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
