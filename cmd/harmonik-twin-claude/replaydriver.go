// Replay driver for the canonical twin binary (M6 WS3-Claude-B).
//
// The replay driver reads a previously-captured Claude-A wire.ndjson progress
// stream (see testdata/twin-parity/claude/<scn>/wire.ndjson) and re-emits it,
// line for line, on the same output writer the daemon's watcher reads. The
// captured stream is written VERBATIM: replay does NOT re-emit through the
// wireEmitter and does NOT restamp any field. Verbatim replay is what makes the
// round-trip an identity — a re-marshaled or re-timestamped line would diverge
// from the committed capture and break twinparity round-trip equivalence.
//
// # Modes
//
//   - Default: every captured line is drained as fast as the writer accepts it.
//   - --preserve-timing: the inter-line delay from the capture's own envelope
//     timestamps is reproduced by a context-aware sleep between lines, mirroring
//     the startup_delay_ms select pattern in main.go. A capture with no
//     timestamp fields (e.g. the wire tap, which carries none) collapses to the
//     default fast-drain behaviour.
//
// # Error policy (exit 1)
//
//   - A line that is not a JSON object → error (caller exits 1).
//   - A first line that is not a valid handshake message
//     (handler_capabilities / handshake) → error (caller exits 1).
//   - An empty capture (no messages) → error.
//
// Cite: plans/2026-07-13-code-revamp/M6-PLAN.md §WS3 Claude-B.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// runReplay reads the NDJSON capture at replayPath and writes each line to out
// unchanged. When preserveTiming is true the capture's inter-line envelope
// delays are reproduced by a context-aware sleep. It returns an error on a
// malformed line, an invalid handshake first line, or a write/scan failure —
// the caller maps a non-nil error to exit code 1.
func runReplay(ctx context.Context, out io.Writer, replayPath string, preserveTiming bool) error {
	//nolint:gosec // G304: replayPath is operator-supplied via --replay-path; provenance is a captured fixture.
	f, err := os.Open(replayPath)
	if err != nil {
		return fmt.Errorf("open replay capture: %w", err)
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // read-only capture handle; close error is irrelevant.

	scanner := bufio.NewScanner(f)
	// Match the watcher's 1 MiB max-line cap (HC-007a) so replay never accepts a
	// line the daemon-side reader would reject.
	const maxLineBytes = 1 << 20
	scanner.Buffer(make([]byte, 4096), maxLineBytes)

	pacer := replayPacer{enabled: preserveTiming}
	var (
		first  = true
		lineNo int
	)
	for scanner.Scan() {
		lineNo++
		raw := scanner.Bytes()
		if strings.TrimSpace(string(raw)) == "" {
			continue // tolerate stray blank lines; they carry no message.
		}

		// Validate the line is a JSON object. Malformed → exit-1 path.
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return fmt.Errorf("replay line %d not a JSON object: %w", lineNo, err)
		}

		if first {
			if !isHandshakeMessage(obj) {
				return fmt.Errorf("replay line %d is not a valid handshake (handler_capabilities/handshake) first message", lineNo)
			}
			first = false
		}

		// --preserve-timing: sleep the inter-line delta before emitting this line.
		if err := pacer.wait(ctx, obj); err != nil {
			return err
		}

		if err := writeVerbatim(out, raw); err != nil {
			return fmt.Errorf("replay write line %d: %w", lineNo, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("replay scan: %w", err)
	}
	if first {
		// No non-blank lines at all: an empty capture is not a valid stream.
		return fmt.Errorf("replay capture %s contained no messages", replayPath)
	}
	return nil
}

// writeVerbatim writes the captured line to out unchanged (raw bytes + one
// 0x0A), no restamp. A fresh copy is written because scanner.Bytes() may be
// overwritten on the next Scan; appending the newline forces a copy anyway.
func writeVerbatim(out io.Writer, raw []byte) error {
	lineOut := make([]byte, 0, len(raw)+1)
	lineOut = append(lineOut, raw...)
	lineOut = append(lineOut, '\n')
	_, err := out.Write(lineOut)
	return err
}

// replayPacer reproduces a capture's inter-line envelope delays when enabled.
// When disabled (or a line carries no timestamp) wait is a no-op.
type replayPacer struct {
	enabled  bool
	havePrev bool
	prevTS   time.Time
}

// wait sleeps the delta between this line's timestamp and the previous line's,
// updating the pacer's running previous-timestamp. It returns ctx.Err() if the
// context is cancelled mid-sleep.
func (p *replayPacer) wait(ctx context.Context, obj map[string]json.RawMessage) error {
	if !p.enabled {
		return nil
	}
	ts, ok := replayTimestamp(obj)
	if !ok {
		return nil
	}
	if p.havePrev {
		if delay := ts.Sub(p.prevTS); delay > 0 {
			if err := sleepCtx(ctx, delay); err != nil {
				return err
			}
		}
	}
	p.prevTS = ts
	p.havePrev = true
	return nil
}

// isHandshakeMessage reports whether the decoded line is a valid first message
// on a progress stream: handler_capabilities (HC-009) or a handshake envelope.
func isHandshakeMessage(obj map[string]json.RawMessage) bool {
	kind := messageType(obj)
	return kind == "handler_capabilities" || kind == "handshake"
}

// messageType extracts the wire message type, preferring the envelope
// "event_type" and falling back to the raw-payload "type" (the dual-field rule
// used across the parity surface).
func messageType(obj map[string]json.RawMessage) string {
	for _, field := range []string{"event_type", "type"} {
		if raw, ok := obj[field]; ok {
			var s string
			if err := json.Unmarshal(raw, &s); err == nil && s != "" {
				return s
			}
		}
	}
	return ""
}

// replayTimestamp extracts a line's envelope timestamp for --preserve-timing,
// trying the common timestamp-bearing fields in priority order. ok=false when
// no parseable timestamp is present (e.g. wire-tap captures carry none).
func replayTimestamp(obj map[string]json.RawMessage) (time.Time, bool) {
	for _, field := range []string{"timestamp", "emitted_at", "transitioned_at"} {
		raw, ok := obj[field]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t, true
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// sleepCtx sleeps for d or returns ctx.Err() if the context is cancelled first,
// mirroring the startup_delay_ms select pattern in main.go.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("replay cancelled during preserve-timing delay: %w", ctx.Err())
	}
}
