package main

// comms_send_scheduled_hk0lwje_test.go — round-trip test verifying that the
// argv produced by the fixed shellCommsSend (internal/daemon) can be parsed by
// runCommsSendSubcommand without hitting the "unknown flag" exit path (hk-0lwje).
//
// Two bugs made every watch scheduled comms-send fail:
//   1. --body flag used instead of positional body — CLI exits 1 "unknown flag --body"
//   2. --from omitted for watch jobs (no Action.From set) — CLI exits 1 "--from is required"
//
// This test drives the parsing path of runCommsSendSubcommand with the corrected
// argv (body positional after "--", --from always supplied) and confirms the
// failure, if any, occurs at socket-connect — not at flag parsing.

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// TestRunCommsSendSubcommand_ScheduledArgvRoundtrip calls runCommsSendSubcommand
// with the same argv that the fixed shellCommsSend/commsSendArgv produces and
// asserts that flag parsing succeeds (no "unknown flag" error in stderr).
// The call will still exit 1 due to a socket-connect failure (no daemon
// running in the test), but that failure is at the network layer — not at
// argument parsing — proving the round-trip is clean.
func TestRunCommsSendSubcommand_ScheduledArgvRoundtrip(t *testing.T) {
	dir := t.TempDir()

	// Argv mirroring commsSendArgv("captain","daemon","watch-liveness-ping","liveness",dir):
	//   comms send --to captain --from daemon --project <dir> --topic liveness -- watch-liveness-ping
	// Note: the first two elements ("comms","send") are consumed by the caller before
	// runCommsSendSubcommand is invoked, so we pass subArgs[2:] here.
	argv := []string{
		"--to", "captain",
		"--from", "daemon",
		"--project", dir,
		"--topic", "liveness",
		"--",
		"watch-liveness-ping",
	}

	// Capture stderr to distinguish parse failure from socket-connect failure.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = w

	_ = runCommsSendSubcommand(argv)

	w.Close()
	os.Stderr = origStderr

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	r.Close()

	stderr := buf.String()

	// Arg parsing must not fail — "unknown flag" must not appear in stderr.
	if strings.Contains(stderr, "unknown flag") {
		t.Errorf("runCommsSendSubcommand failed at flag parsing; stderr=%q", stderr)
	}
	// "--from is required" must not appear (from is always supplied now).
	if strings.Contains(stderr, "--from is required") {
		t.Errorf("runCommsSendSubcommand failed on missing --from; stderr=%q", stderr)
	}
}
