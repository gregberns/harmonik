// Tests for apptap.Tap (codex-app-server T1, hk-893ct).
//
// Gate (T1 acceptance criteria):
//   "real client through tap for one happy turn → child works E2E AND
//    concatenated captured raw bytes diff-match an untapped control run
//    (transparent + lossless)"
//
// The test child is `cat` (reads stdin, echoes verbatim to stdout, exits on
// EOF).  cat is the minimal bidirectional stdio process: any JSONL payload sent
// through it exercises the full client→child→client roundtrip without requiring
// the real `codex app-server` binary.  Protocol specifics are not tested here
// (T1's scope is the splice itself, not the RPC layer).
//
// Helper-prefix: tapFixture (per-bead prefix convention from implementer-protocol.md).
package apptap

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// tapFixtureRunDirect runs binary with args, feeds input to its stdin, and
// returns its combined stdout.  Fails the test on any subprocess error.
func tapFixtureRunDirect(t *testing.T, binary string, args []string, input string) string {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("tapFixtureRunDirect(%q %v): %v", binary, args, err)
	}
	return string(out)
}

// tapFixtureRunTap runs the Tap with binary/args and the given input, returning
// the stdout seen by the caller and the captured bytes from each direction.
func tapFixtureRunTap(t *testing.T, binary string, args []string, input string) (stdout string, inCap string, outCap string) {
	t.Helper()
	var stdoutBuf, inBuf, outBuf bytes.Buffer
	tap := Tap{
		Binary:     binary,
		Args:       args,
		Stdin:      strings.NewReader(input),
		Stdout:     &stdoutBuf,
		Stderr:     io.Discard,
		InCapture:  &inBuf,
		OutCapture: &outBuf,
	}
	if err := tap.Run(); err != nil {
		t.Fatalf("tapFixtureRunTap(%q %v): %v", binary, args, err)
	}
	return stdoutBuf.String(), inBuf.String(), outBuf.String()
}

// TestTapTransparent verifies that the tap does not alter bytes flowing from the
// child to the caller: stdout received through the tap equals the direct-run
// stdout (transparency invariant).
func TestTapTransparent(t *testing.T) {
	const input = "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n" +
		"{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"ping\"}\n"

	direct := tapFixtureRunDirect(t, "cat", nil, input)
	through, _, _ := tapFixtureRunTap(t, "cat", nil, input)

	if through != direct {
		t.Errorf("tap stdout differs from direct stdout\ngot:  %q\nwant: %q", through, direct)
	}
}

// TestTapInCaptureMatchesInput verifies that InCapture receives exactly the
// bytes that were forwarded to the child's stdin (lossless input capture).
func TestTapInCaptureMatchesInput(t *testing.T) {
	const input = "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n"

	_, inCap, _ := tapFixtureRunTap(t, "cat", nil, input)

	if inCap != input {
		t.Errorf("InCapture mismatch\ngot:  %q\nwant: %q", inCap, input)
	}
}

// TestTapOutCaptureMatchesChildOutput verifies that OutCapture receives exactly
// the bytes that the child wrote to stdout (lossless output capture).
func TestTapOutCaptureMatchesChildOutput(t *testing.T) {
	const input = "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}\n"

	direct := tapFixtureRunDirect(t, "cat", nil, input)
	_, _, outCap := tapFixtureRunTap(t, "cat", nil, input)

	if outCap != direct {
		t.Errorf("OutCapture mismatch\ngot:  %q\nwant: %q", outCap, direct)
	}
}

// TestTapGateLosslessRoundtrip is the T1 gate test.
//
// Gate: real client through tap for one happy turn → child works E2E AND
// concatenated captured raw bytes diff-match an untapped control run
// (transparent + lossless).
//
// Procedure:
//  1. Run `cat` directly with a multi-line JSONL payload; record stdout.
//  2. Run the same payload through Tap with capture writers.
//  3. Assert tap stdout == direct stdout (transparent).
//  4. Assert InCapture == input bytes (lossless input).
//  5. Assert OutCapture == direct stdout bytes (lossless output).
func TestTapGateLosslessRoundtrip(t *testing.T) {
	// Multi-line JSONL payload simulating two JSON-RPC requests.
	const input = "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{}}\n" +
		"{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"executeCode\",\"params\":{\"code\":\"echo hello\"}}\n"

	// Step 1: control run without the tap.
	directOut := tapFixtureRunDirect(t, "cat", nil, input)

	// Step 2: tap run with both capture directions wired.
	tapOut, inCap, outCap := tapFixtureRunTap(t, "cat", nil, input)

	// Gate assertion 1: transparent — tap output matches direct output.
	if tapOut != directOut {
		t.Errorf("GATE FAIL: tap stdout != direct stdout\ntap:    %q\ndirect: %q", tapOut, directOut)
	}

	// Gate assertion 2: lossless input — InCapture matches what was sent.
	if inCap != input {
		t.Errorf("GATE FAIL: InCapture != input\ngot:  %q\nwant: %q", inCap, input)
	}

	// Gate assertion 3: lossless output — OutCapture matches what child produced.
	if outCap != directOut {
		t.Errorf("GATE FAIL: OutCapture != direct stdout\ngot:  %q\nwant: %q", outCap, directOut)
	}
}

// TestTapNilCaptures verifies that Tap works correctly when InCapture and
// OutCapture are both nil (capture-disabled mode).
func TestTapNilCaptures(t *testing.T) {
	const input = "{\"type\":\"ping\"}\n"

	var stdoutBuf bytes.Buffer
	tap := Tap{
		Binary: "cat",
		Stdin:  strings.NewReader(input),
		Stdout: &stdoutBuf,
		Stderr: io.Discard,
		// InCapture and OutCapture intentionally nil.
	}
	if err := tap.Run(); err != nil {
		t.Fatalf("Tap.Run with nil captures: %v", err)
	}
	if got := stdoutBuf.String(); got != input {
		t.Errorf("nil-capture tap: stdout = %q, want %q", got, input)
	}
}

// TestTapEmptyInput verifies that Tap handles an empty stdin without hanging or
// erroring — the child receives EOF immediately and exits cleanly.
func TestTapEmptyInput(t *testing.T) {
	var stdoutBuf, inBuf, outBuf bytes.Buffer
	tap := Tap{
		Binary:     "cat",
		Stdin:      strings.NewReader(""),
		Stdout:     &stdoutBuf,
		Stderr:     io.Discard,
		InCapture:  &inBuf,
		OutCapture: &outBuf,
	}
	if err := tap.Run(); err != nil {
		t.Fatalf("Tap.Run with empty input: %v", err)
	}
	if got := stdoutBuf.String(); got != "" {
		t.Errorf("empty-input tap: stdout = %q, want empty", got)
	}
	if got := inBuf.String(); got != "" {
		t.Errorf("empty-input tap: InCapture = %q, want empty", got)
	}
	if got := outBuf.String(); got != "" {
		t.Errorf("empty-input tap: OutCapture = %q, want empty", got)
	}
}

// TestTapDefaultsToOSStdio verifies that Tap.Run does not panic when Stdin,
// Stdout, and Stderr are nil — it must default to the process stdio without
// actually touching os.Stdin/Stdout in the test (we use file descriptors that
// are guaranteed to be /dev/null in CI).  We exercise this by running cat with
// /dev/null as the process stdin (the child exits immediately on EOF).
func TestTapDefaultsToOSStdio(t *testing.T) {
	// Redirect the process stdin to /dev/null so the default os.Stdin yields
	// EOF immediately, allowing cat to exit without blocking.
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Skipf("cannot open /dev/null: %v", err)
	}
	t.Cleanup(func() { devNull.Close() })

	origStdin := os.Stdin
	origStdout := os.Stdout
	os.Stdin = devNull
	// Redirect stdout to discard so the test doesn't pollute test output.
	devNullOut, err := os.Open(os.DevNull)
	if err != nil {
		os.Stdin = origStdin
		t.Skipf("cannot open /dev/null for stdout: %v", err)
	}
	os.Stdout = devNullOut
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
		devNullOut.Close()
	}()

	tap := Tap{
		Binary: "cat",
		Stderr: io.Discard,
		// Stdin, Stdout nil → should default to os.Stdin/Stdout.
	}
	if err := tap.Run(); err != nil {
		t.Errorf("Tap.Run with nil Stdin/Stdout: %v", err)
	}
}

// TestTapMultilineJSONL verifies that the tap correctly handles a stream of
// many JSONL lines without dropping or reordering any.
func TestTapMultilineJSONL(t *testing.T) {
	// Build a payload with N JSONL lines.
	const n = 50
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString("{\"seq\":")
		sb.WriteString(string(rune('0' + i%10)))
		sb.WriteString("}\n")
	}
	input := sb.String()

	direct := tapFixtureRunDirect(t, "cat", nil, input)
	tapOut, inCap, outCap := tapFixtureRunTap(t, "cat", nil, input)

	if tapOut != direct {
		t.Errorf("multiline: tap stdout != direct stdout (len tap=%d direct=%d)", len(tapOut), len(direct))
	}
	if inCap != input {
		t.Errorf("multiline: InCapture != input (len cap=%d input=%d)", len(inCap), len(input))
	}
	if outCap != direct {
		t.Errorf("multiline: OutCapture != direct stdout (len cap=%d direct=%d)", len(outCap), len(direct))
	}
}
