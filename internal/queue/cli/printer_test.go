package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// errAfterN is an io.Writer that succeeds for the first n writes and then fails
// every subsequent one — the shape of a pager that exits mid-report or a pipe
// that fills up.
type errAfterN struct {
	n     int
	sink  strings.Builder
	fail  error
	calls int
}

func (w *errAfterN) Write(p []byte) (int, error) {
	w.calls++
	if w.calls > w.n {
		return 0, w.fail
	}
	return w.sink.Write(p)
}

func TestPrinterRetainsFirstErrorAndStopsWriting(t *testing.T) {
	boom := errors.New("boom")
	w := &errAfterN{n: 1, fail: boom}
	p := newPrinter(w)

	p.printf("first\n")
	if p.failed() {
		t.Fatalf("printer failed after a successful write: %v", p.err)
	}

	p.printf("second\n")
	if !p.failed() {
		t.Fatal("printer did not retain the write error")
	}
	if !errors.Is(p.err, boom) {
		t.Fatalf("retained error = %v, want %v", p.err, boom)
	}

	// Every later write must be a no-op: the retained error is not overwritten
	// and the underlying writer is not called again.
	callsAtFailure := w.calls
	p.println("third")
	p.printf("fourth\n")
	if w.calls != callsAtFailure {
		t.Fatalf("printer kept writing after failure: %d calls, want %d", w.calls, callsAtFailure)
	}
	if !errors.Is(p.err, boom) {
		t.Fatalf("retained error was overwritten: %v", p.err)
	}
	if got := w.sink.String(); got != "first\n" {
		t.Fatalf("sink = %q, want %q", got, "first\n")
	}
}

// A renderer whose stdout write fails must NOT report exitSuccess: the caller
// received a truncated report and has no other way to tell.
func TestRenderersReportTruncatedStdoutAsTransportError(t *testing.T) {
	result := json.RawMessage(`{"queue":{"queue_id":"q1","status":"active","groups":[]}}`)

	if got := renderQueueStatusText(result, &strings.Builder{}); got != exitSuccess {
		t.Fatalf("healthy stdout: exit = %d, want %d", got, exitSuccess)
	}

	w := &errAfterN{n: 0, fail: errors.New("EPIPE")}
	if got := renderQueueStatusText(result, w); got != exitTransportError {
		t.Fatalf("failing stdout: exit = %d, want %d", got, exitTransportError)
	}
}

// handleResponse writes the daemon's error body to stdout per PL-028c; a failed
// write there must surface as a transport error rather than the response's own
// validation exit code, which the caller never actually got to read.
func TestHandleResponseReportsFailedStdoutWrite(t *testing.T) {
	resp := socketResponse{Ok: false, Error: "queue_already_completed", ErrorCode: validationErrorCodeMin}

	if got := handleResponse(resp, &strings.Builder{}, false, renderQueueStatusText); got != exitValidationError {
		t.Fatalf("healthy stdout: exit = %d, want %d", got, exitValidationError)
	}

	w := &errAfterN{n: 0, fail: errors.New("EPIPE")}
	if got := handleResponse(resp, w, false, renderQueueStatusText); got != exitTransportError {
		t.Fatalf("failing stdout: exit = %d, want %d", got, exitTransportError)
	}
}
