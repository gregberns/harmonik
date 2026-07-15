package apptap

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// besteffortErrWriter fails every Write after the first `okWrites` successful
// ones, recording what it did receive. It models a capture sink that goes bad
// mid-stream (full disk / slow-then-broken sink) for AIS-INV-002.
type besteffortErrWriter struct {
	okWrites int
	got      bytes.Buffer
	writes   int
}

var errCaptureFull = errors.New("capture: no space left on device")

func (w *besteffortErrWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes > w.okWrites {
		return 0, errCaptureFull
	}
	return w.got.Write(p)
}

// TestBestEffortCaptureWriterDegradeDoesNotAbort asserts a capture-write fault
// never aborts or short-writes the primary stream (AIS-013 / AIS-INV-002), and
// that onErr fires exactly once on degrade.
func TestBestEffortCaptureWriterDegradeDoesNotAbort(t *testing.T) {
	var dst bytes.Buffer
	capW := &besteffortErrWriter{okWrites: 1} // first tee ok, then broken.
	var onErrCalls int
	var lastErr error
	w := BestEffortCaptureWriter(&dst, capW, func(err error) {
		onErrCalls++
		lastErr = err
	})

	chunks := []string{"first\n", "second\n", "third\n"}
	for _, c := range chunks {
		n, err := w.Write([]byte(c))
		if err != nil {
			t.Fatalf("primary write %q returned error: %v (capture fault must not surface)", c, err)
		}
		if n != len(c) {
			t.Fatalf("primary write %q short: n=%d want=%d", c, n, len(c))
		}
	}

	if got, want := dst.String(), strings.Join(chunks, ""); got != want {
		t.Fatalf("primary stream corrupted by capture fault: got %q want %q", got, want)
	}
	// Capture got the first chunk verbatim, then degraded.
	if got := capW.got.String(); got != "first\n" {
		t.Fatalf("capture before fault not verbatim: got %q want %q", got, "first\n")
	}
	if onErrCalls != 1 {
		t.Fatalf("onErr fired %d times, want exactly 1 (degrade-once)", onErrCalls)
	}
	if !errors.Is(lastErr, errCaptureFull) {
		t.Fatalf("onErr got %v, want errCaptureFull", lastErr)
	}
}

// TestBestEffortCaptureWriterVerbatim asserts a healthy capture is byte-verbatim.
func TestBestEffortCaptureWriterVerbatim(t *testing.T) {
	var dst, capBuf bytes.Buffer
	w := BestEffortCaptureWriter(&dst, &capBuf, nil)
	payload := []byte(`{"jsonrpc":"2.0","method":"turn/start"}` + "\n")
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !bytes.Equal(capBuf.Bytes(), payload) {
		t.Fatalf("capture not verbatim: got %q want %q", capBuf.Bytes(), payload)
	}
	if !bytes.Equal(dst.Bytes(), payload) {
		t.Fatalf("primary not verbatim: got %q want %q", dst.Bytes(), payload)
	}
}

// TestBestEffortCaptureReaderDegradeDoesNotAbort asserts a capture fault on the
// read path never surfaces as a read error and never drops primary bytes.
func TestBestEffortCaptureReaderDegradeDoesNotAbort(t *testing.T) {
	src := strings.NewReader("alpha\nbeta\ngamma\n")
	capW := &besteffortErrWriter{okWrites: 0} // broken from the very first tee.
	var onErrCalls int
	r := BestEffortCaptureReader(src, capW, func(error) { onErrCalls++ })

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll surfaced a capture fault as a read error: %v", err)
	}
	if want := "alpha\nbeta\ngamma\n"; string(got) != want {
		t.Fatalf("primary read corrupted: got %q want %q", got, want)
	}
	if onErrCalls != 1 {
		t.Fatalf("onErr fired %d times, want exactly 1", onErrCalls)
	}
}

// TestBestEffortCaptureNilPassthrough asserts a nil capture returns the
// underlying stream unchanged (no wrapping cost, no behavior change).
func TestBestEffortCaptureNilPassthrough(t *testing.T) {
	var dst bytes.Buffer
	if w := BestEffortCaptureWriter(&dst, nil, nil); w != io.Writer(&dst) {
		t.Fatalf("nil capture writer should return dst unchanged")
	}
	src := strings.NewReader("x")
	if r := BestEffortCaptureReader(src, nil, nil); r != io.Reader(src) {
		t.Fatalf("nil capture reader should return src unchanged")
	}
}
