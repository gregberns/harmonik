// Codex JSONL emitter for harmonik-twin-codex (codex-harness C6/T15, hk-of3h4).
//
// This file implements the stdout JSONL emitter that the codex twin uses to
// simulate codex exec output. The format mirrors the real codex --json surface:
// thread.started → optional item.* events → turn.completed or turn.failed.
//
// Normative format reference: codex-harness 04-research/codex-cli/findings.md §2.
//
// Cite: codex-harness C2 spec (C2-codex-adapter-spec.md §Approach);
// codex-harness C6 spec (C6-migration-test-spec.md §Approach).
package main

import (
	"encoding/json"
	"fmt"
	"io"
)

type codexEmitter struct {
	w io.Writer
}

func newCodexEmitter(w io.Writer) *codexEmitter {
	return &codexEmitter{w: w}
}

func (e *codexEmitter) emitRaw(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("codexEmitter.emitRaw: marshal: %w", err)
	}
	b = append(b, '\n')
	_, err = e.w.Write(b)
	return err
}

func (e *codexEmitter) emitThreadStarted(threadID string) error {
	return e.emitRaw(map[string]any{
		"type":      "thread.started",
		"thread_id": threadID,
	})
}

func (e *codexEmitter) emitTurnCompleted() error {
	return e.emitRaw(map[string]any{
		"type": "turn.completed",
		"usage": map[string]any{
			"input_tokens":  0,
			"output_tokens": 0,
		},
	})
}

func (e *codexEmitter) emitTurnFailed(message string) error {
	return e.emitRaw(map[string]any{
		"type": "turn.failed",
		"error": map[string]any{
			"message": message,
		},
	})
}
