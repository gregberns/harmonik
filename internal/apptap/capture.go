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
