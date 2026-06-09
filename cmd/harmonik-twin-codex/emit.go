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

// codexEmitter writes codex JSONL events to an io.Writer (typically os.Stdout).
//
// Each write method serialises one JSON object and appends a single newline,
// matching the codex --json JSONL framing.  NOT goroutine-safe.
type codexEmitter struct {
	w io.Writer
}

// newCodexEmitter wraps w in a codexEmitter.
func newCodexEmitter(w io.Writer) *codexEmitter {
	return &codexEmitter{w: w}
}

// emitRaw marshals v as compact JSON followed by a newline.
func (e *codexEmitter) emitRaw(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("codexEmitter.emitRaw: marshal: %w", err)
	}
	b = append(b, '\n')
	_, err = e.w.Write(b)
	return err
}

// emitThreadStarted emits {"type":"thread.started","thread_id":"<id>"}.
//
// This is the first event codex emits on stdout under --json. The harmonik
// codex adapter captures the thread_id from this event for session tracking
// (SessionIDPolicy==SessionIDCaptured; C2-codex-adapter-spec.md §AC2.1).
func (e *codexEmitter) emitThreadStarted(threadID string) error {
	return e.emitRaw(map[string]any{
		"type":      "thread.started",
		"thread_id": threadID,
	})
}

// emitTurnCompleted emits {"type":"turn.completed","usage":{...}}.
//
// This is the terminal success event. The harmonik codex adapter watches for
// process exit + this event to close the run (C2 AC2.3).
func (e *codexEmitter) emitTurnCompleted() error {
	return e.emitRaw(map[string]any{
		"type": "turn.completed",
		"usage": map[string]any{
			"input_tokens":  0,
			"output_tokens": 0,
		},
	})
}

// emitTurnFailed emits {"type":"turn.failed","error":{"message":"<msg>"}}.
//
// This is the terminal failure event. The harmonik codex adapter maps this to
// run_failed (C2 AC2.5; C6 variant: turn.failed).
func (e *codexEmitter) emitTurnFailed(message string) error {
	return e.emitRaw(map[string]any{
		"type": "turn.failed",
		"error": map[string]any{
			"message": message,
		},
	})
}
