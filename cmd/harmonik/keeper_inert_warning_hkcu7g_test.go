package main

import (
	"os"
	"strings"
	"testing"
)

// keeper_inert_warning_hkcu7g_test.go — hk-cu7g superseded by hk-5da7.
//
// Previously --warn-pct/--act-pct printed an "inert on 1M-window models" warning.
// hk-5da7 made those flags HONORED (fed in as pct-of-window ceils), so the inert
// warning is GONE and is replaced by a "honoring --warn-pct N as warn ceil"
// confirmation. This test pins that the explicit pct flag is acknowledged (not
// silently ignored) on the stderr path.
func TestKeeperPctFlagsAreHonoredNotInert(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = pw

	// --warn-pct + --act-pct with no --agent → exit 1, but the honoring lines are
	// emitted before the agent check.
	runKeeperSubcommand([]string{"--warn-pct", "30", "--act-pct", "35"})

	pw.Close()
	os.Stderr = origStderr

	buf := make([]byte, 8192)
	n, _ := pr.Read(buf)
	pr.Close()
	output := string(buf[:n])

	// The NEW contract: explicit pct flags are honored, not declared inert.
	if strings.Contains(output, "inert") {
		t.Fatalf("pct flags should be HONORED, but stderr still says 'inert':\n%s", output)
	}
	if !strings.Contains(output, "honoring --warn-pct 30") {
		t.Errorf("expected 'honoring --warn-pct 30' confirmation; got:\n%s", output)
	}
	if !strings.Contains(output, "honoring --act-pct 35") {
		t.Errorf("expected 'honoring --act-pct 35' confirmation; got:\n%s", output)
	}
}
