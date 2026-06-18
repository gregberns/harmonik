package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/keeper"
)

// TestKeeperInertWarningUsesRealConstants (hk-cu7g) asserts that the
// --warn-pct/--act-pct inert warning printed to stderr references the live
// keeper.DefaultWarnAbsTokens and keeper.DefaultActAbsTokens values, not
// stale hardcoded literals.
func TestKeeperInertWarningUsesRealConstants(t *testing.T) {
	// Capture os.Stderr by temporarily replacing it with a pipe.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = pw

	// --warn-pct triggers the inert-warning path; no --agent → exit 1, which is
	// fine — we only care about what was written to stderr before the exit.
	runKeeperSubcommand([]string{"--warn-pct", "85"})

	pw.Close()
	os.Stderr = origStderr

	buf := make([]byte, 4096)
	n, _ := pr.Read(buf)
	pr.Close()
	output := string(buf[:n])

	wantSubstr := fmt.Sprintf("warn=%d act=%d", keeper.DefaultWarnAbsTokens, keeper.DefaultActAbsTokens)
	if !strings.Contains(output, wantSubstr) {
		t.Fatalf("inert-warning does not contain %q;\ngot: %q", wantSubstr, output)
	}
}
