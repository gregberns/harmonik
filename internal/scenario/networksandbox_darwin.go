package scenario

import (
	"fmt"
	"os/exec"
	"strings"
)

// harmonikPFAnchor is the pf anchor name used by the harness sandbox.
// Using a named anchor lets ApplyNetworkSandbox install and Release flush rules
// without modifying the system-wide pf ruleset.
const harmonikPFAnchor = "harmonik/scenario-sandbox"

// harmonikPFRules is the pf anchor ruleset loaded by ApplyNetworkSandbox.
//
// Rules:
//   - Pass all traffic on the loopback interface (lo0) — permits daemon RPC,
//     SQLite-over-localhost, AF_UNIX sockets within the synthetic project root.
//   - Block (drop) all other outbound traffic — enforces the SH-028 prohibition
//     on non-loopback IPv4/IPv6 and outbound DNS to non-loopback resolvers.
//
// The "quick" keyword causes the first matching rule to win (pf's first-match
// semantics); loopback is permitted before the catch-all block fires.
//
// Spec ref: specs/scenario-harness.md §4.8 SH-028.
const harmonikPFRules = `pass quick on lo0 all
pass quick on lo1 all
block drop out all
`

// applyNetworkSandbox implements ApplyNetworkSandbox on macOS.
//
// Installs the harmonikPFAnchor pf anchor via pfctl, then enables pf if it is
// not already running. Requires root or sudo privileges; returns a wrapped
// ErrNetworkSandboxUnsupported if pfctl is not available or permission is denied.
//
// Release flushes the anchor rules via `pfctl -a <anchor> -F rules`.
//
// Note: pf on macOS is tracked by OQ-SH-013 as the "mechanism floor" for the
// macOS conformance lane. CI environments without root access should skip
// sandbox-dependent tests via t.Skip (see sh028_network_sandbox_test.go).
//
// Spec ref: specs/scenario-harness.md §4.8 SH-028, OQ-SH-013.
func applyNetworkSandbox() (*NetworkSandboxHandle, error) {
	// Load the anchor rules from stdin to avoid a temp file.
	loadCmd := exec.Command("pfctl", "-a", harmonikPFAnchor, "-f", "-")
	loadCmd.Stdin = strings.NewReader(harmonikPFRules)
	if out, err := loadCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf(
			"%w: pfctl load anchor %q: %v (output: %s; requires root/admin)",
			ErrNetworkSandboxUnsupported, harmonikPFAnchor, err, strings.TrimSpace(string(out)),
		)
	}

	// Enable pf if not already running; ignore "already enabled" non-zero exits.
	_ = exec.Command("pfctl", "-e").Run()

	return &NetworkSandboxHandle{
		release: func() error {
			out, err := exec.Command("pfctl", "-a", harmonikPFAnchor, "-F", "rules").CombinedOutput()
			if err != nil {
				return fmt.Errorf("network sandbox release: pfctl flush anchor %q: %w (output: %s)",
					harmonikPFAnchor, err, strings.TrimSpace(string(out)))
			}
			return nil
		},
	}, nil
}

// isNetworkSandboxActive implements IsNetworkSandboxActive on macOS.
//
// Queries the pf anchor installed by ApplyNetworkSandbox and returns true if
// the anchor exists and contains a block rule, indicating the sandbox is active.
func isNetworkSandboxActive() bool {
	out, err := exec.Command("pfctl", "-a", harmonikPFAnchor, "-s", "rules").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "block")
}
