package apptap

// Capture helpers for drivers that own their child's pipes directly.
//
// Tap.Run owns the whole child (exec + Wait), which does not fit a driver that
// spawns and reaps its own process (AIS-009). These two helpers expose the same
// transparent/lossless/verbatim tee invariant as Tap for callers that hold the
// raw pipe endpoints: pure io.TeeReader / io.MultiWriter wrappers, no parsing,
// no reframing, no buffering for meaning.

import "io"

// CaptureReader tees every byte read from r into capture, verbatim. When
// capture is nil it returns r unchanged. A write error on capture surfaces as
// a read error (io.TeeReader semantics) — callers wanting best-effort capture
// wrap capture accordingly.
func CaptureReader(r io.Reader, capture io.Writer) io.Reader {
	if capture == nil {
		return r
	}
	return io.TeeReader(r, capture)
}

// CaptureWriter tees every byte written to w into capture, verbatim. When
// capture is nil it returns w unchanged.
func CaptureWriter(w, capture io.Writer) io.Writer {
	if capture == nil {
		return w
	}
	return io.MultiWriter(w, capture)
}

// BestEffortCaptureWriter returns a writer that forwards every byte to dst and
// tees a byte-identical copy to capture, but a capture-write error MUST NOT
// abort or back-pressure the write to dst (AIS-013 / AIS-INV-002). This is an
// explicit reversal of CaptureWriter's fail-closed io.MultiWriter semantics:
// the primary stream (dst) is the live agent wire and its liveness must be
// independent of the capture side-channel.
//
// On the first capture-write error the tee degrades to uncaptured — capture is
// dropped for the remainder of the stream and onErr (when non-nil) is called
// exactly once with that error. Subsequent writes flow to dst only.
//
// The returned writer is single-goroutine-owned (no internal locking); the
// driver's stdin path has one writer goroutine.
//
// When capture is nil it returns dst unchanged.
func BestEffortCaptureWriter(dst, capture io.Writer, onErr func(error)) io.Writer {
	if capture == nil {
		return dst
	}
	// io.MultiWriter aborts on the first sub-writer error; wrapping capture in
	// a degrading writer (which never returns an error) keeps dst flowing.
	return io.MultiWriter(dst, &degradingWriter{w: capture, onErr: onErr})
}

// BestEffortCaptureReader returns a reader that tees every byte read from src
// into capture, but a capture-write error MUST NOT surface as a read error or
// abort the primary stream (AIS-013 / AIS-INV-002) — an explicit reversal of
// CaptureReader's io.TeeReader fail-closed semantics.
//
// On the first capture-write error the tee degrades to uncaptured and onErr
// (when non-nil) is called exactly once. Read errors from src pass through
// unchanged. When capture is nil it returns src unchanged.
//
// The returned reader is single-goroutine-owned; the driver's stdout path has
// one reader goroutine (the scanner).
func BestEffortCaptureReader(src io.Reader, capture io.Writer, onErr func(error)) io.Reader {
	if capture == nil {
		return src
	}
	return io.TeeReader(src, &degradingWriter{w: capture, onErr: onErr})
}

// degradingWriter wraps a capture writer so a write error degrades to
// uncaptured instead of propagating: it always reports a full, error-free
// write, and after the first underlying error it stops writing and fires onErr
// once. This is the load-bearing best-effort primitive behind AIS-INV-002.
//
// Not safe for concurrent use; each capture direction owns its own instance on
// a single goroutine.
type degradingWriter struct {
	w       io.Writer
	onErr   func(error)
	dropped bool
}

func (d *degradingWriter) Write(p []byte) (int, error) {
	if !d.dropped {
		if _, err := d.w.Write(p); err != nil {
			d.dropped = true
			if d.onErr != nil {
				d.onErr(err)
			}
		}
	}
	// Always claim a full, error-free write so io.MultiWriter / io.TeeReader
	// never abort the primary stream on a capture fault.
	return len(p), nil
}
