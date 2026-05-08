package brcli

import "io"

// stderrCapMaxBytes is the BI-025d maximum capture size for br stderr: 1 MiB.
// Any bytes written beyond this limit are silently dropped; the Truncated field
// of stderrCapWriter is set to true and TruncationSuffix is appended when
// StderrResult is retrieved.
const stderrCapMaxBytes = 1 << 20 // 1 MiB

// StderrTruncationSuffix is the explicit suffix appended to the captured stderr
// bytes when truncation has occurred, per BI-025d.
//
// Spec ref: specs/beads-integration.md §4.8a BI-025d.
const StderrTruncationSuffix = "[...stderr truncated at 1 MiB]"

// StderrResult holds the captured stderr from a br subprocess invocation.
//
// When Truncated is true, the Bytes slice ends with StderrTruncationSuffix.
// The adapter MUST NOT parse Bytes for state (BI-025d: stderr is informational).
type StderrResult struct {
	// Bytes is the captured stderr, bounded to stderrCapMaxBytes before
	// appending the truncation suffix. If the subprocess wrote less than the
	// cap, Bytes is the verbatim output.
	Bytes []byte

	// Truncated is true when the subprocess wrote more than stderrCapMaxBytes
	// to stderr and the capture was truncated.
	Truncated bool
}

// stderrCapWriter is an io.Writer that buffers up to stderrCapMaxBytes of
// stderr output. Bytes written beyond the cap are silently discarded; Truncated
// is latched to true on the first overflow write.
type stderrCapWriter struct {
	buf       []byte
	truncated bool
}

// newStderrCapWriter allocates a stderrCapWriter with a pre-allocated buffer
// sized to the common case (4 KiB) to avoid re-allocation for short stderr
// payloads.
func newStderrCapWriter() *stderrCapWriter {
	return &stderrCapWriter{buf: make([]byte, 0, 4096)}
}

// Write implements io.Writer. Bytes that would push buf past stderrCapMaxBytes
// are silently dropped; Truncated is latched true on first overflow.
func (w *stderrCapWriter) Write(p []byte) (int, error) {
	if w.truncated {
		// Already truncating — discard all further bytes; report success to the
		// caller (the subprocess writer must not receive EPIPE from overflow).
		return len(p), nil
	}

	remaining := stderrCapMaxBytes - len(w.buf)
	if remaining <= 0 {
		// Buf is already at the cap from a prior write; latch and discard.
		w.truncated = true
		return len(p), nil
	}

	if len(p) <= remaining {
		// Fits entirely — append verbatim.
		w.buf = append(w.buf, p...)
		return len(p), nil
	}

	// Partial fit: append what fits, then latch truncated.
	w.buf = append(w.buf, p[:remaining]...)
	w.truncated = true
	return len(p), nil
}

// Result returns the captured StderrResult. When Truncated is true, the
// truncation suffix is appended to Bytes so callers always see the marker
// without a separate flag check.
//
// Result MUST be called only after the subprocess has exited and cmd.Wait()
// has returned (or the equivalent goroutine-driven Wait in RunWithTimeout),
// to guarantee that all subprocess stderr writes have been flushed to w.
func (w *stderrCapWriter) Result() StderrResult {
	if !w.truncated {
		return StderrResult{Bytes: w.buf, Truncated: false}
	}

	// Append the explicit truncation suffix per BI-025d.
	out := make([]byte, len(w.buf), len(w.buf)+len(StderrTruncationSuffix)+1)
	copy(out, w.buf)
	out = append(out, '\n')
	out = append(out, StderrTruncationSuffix...)
	return StderrResult{Bytes: out, Truncated: true}
}

// Verify that stderrCapWriter satisfies io.Writer at compile time.
var _ io.Writer = (*stderrCapWriter)(nil)
