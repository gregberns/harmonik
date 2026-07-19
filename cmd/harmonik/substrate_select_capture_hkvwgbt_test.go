package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCodexSubstrateOptions_CaptureNonInert is the regression that pins the
// hk-vwgbt fix: before wiring, codexdriver.Options.InCapture/OutCapture were
// left nil at the composition root, so the M2-4 capture tee was INERT in the
// live binary. This proves that when capture is opted in, the composition root
// routes I/O through the sessioncapture corpus — writing to the wired writers
// lands bytes in wire-in.jsonl / wire-out.jsonl (AIS-013/AIS-014).
func TestCodexSubstrateOptions_CaptureNonInert(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(captureDirEnv, dir)

	opts, sess := codexSubstrateOptions("codex", &codexWorkerRoutingRunner{})
	if sess == nil {
		t.Fatal("capture session not opened despite HARMONIK_CAPTURE_DIR set")
	}
	if opts.InCapture == nil || opts.OutCapture == nil {
		t.Fatal("capture writers not wired into Options (tee is INERT — the hk-vwgbt gap)")
	}

	// Drive bytes through the WIRED writers and confirm they reach the corpus,
	// not a discarded sink. Newline-terminated so the OUTPUT line-scrubber emits.
	if _, err := io.WriteString(opts.InCapture, `{"dir":"in","payload":"teed-input"}`+"\n"); err != nil {
		t.Fatalf("write InCapture: %v", err)
	}
	if _, err := io.WriteString(opts.OutCapture, `{"dir":"out","frame":"teed-output"}`+"\n"); err != nil {
		t.Fatalf("write OutCapture: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	inBytes, err := os.ReadFile(filepath.Join(sess.Dir(), "wire-in.jsonl"))
	if err != nil {
		t.Fatalf("read wire-in.jsonl: %v", err)
	}
	if !strings.Contains(string(inBytes), "teed-input") {
		t.Fatalf("wire-in.jsonl missing input routed through Options.InCapture; got %q", inBytes)
	}
	outBytes, err := os.ReadFile(filepath.Join(sess.Dir(), "wire-out.jsonl"))
	if err != nil {
		t.Fatalf("read wire-out.jsonl: %v", err)
	}
	if !strings.Contains(string(outBytes), "teed-output") {
		t.Fatalf("wire-out.jsonl missing output routed through Options.OutCapture; got %q", outBytes)
	}
}

// TestCodexSubstrateOptions_CaptureOptInOffByDefault pins the opt-in contract:
// with no HARMONIK_CAPTURE_DIR, no session opens and the writers stay nil (the
// tmux-safe default; capture is never on unless explicitly asked for).
func TestCodexSubstrateOptions_CaptureOptInOffByDefault(t *testing.T) {
	t.Setenv(captureDirEnv, "") // force-unset even if the ambient env had it

	opts, sess := codexSubstrateOptions("codex", &codexWorkerRoutingRunner{})
	if sess != nil {
		t.Fatal("capture session opened with no HARMONIK_CAPTURE_DIR (must be opt-in)")
	}
	if opts.InCapture != nil || opts.OutCapture != nil {
		t.Fatal("capture writers wired with capture disabled")
	}
}
