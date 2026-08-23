package scenario

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const harmonikPFAnchor = "harmonik/scenario-sandbox"

const harmonikPFRules = `pass quick on lo0 all
pass quick on lo1 all
block drop out all
`

func applyNetworkSandbox() (*NetworkSandboxHandle, error) {
	loadCmd := exec.CommandContext(context.Background(), "pfctl", "-a", harmonikPFAnchor, "-f", "-")
	loadCmd.Stdin = strings.NewReader(harmonikPFRules)
	if out, err := loadCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf(
			"%w: pfctl load anchor %q: %w (output: %s; requires root/admin)",
			ErrNetworkSandboxUnsupported, harmonikPFAnchor, err, strings.TrimSpace(string(out)),
		)
	}

	if out, err := exec.CommandContext(context.Background(), "pfctl", "-e").CombinedOutput(); err != nil && !strings.Contains(string(out), "already enabled") {
		return nil, fmt.Errorf("%w: pfctl enable: %w (output: %s; requires root/admin)",
			ErrNetworkSandboxUnsupported, err, strings.TrimSpace(string(out)))
	}

	return &NetworkSandboxHandle{
		release: func() error {
			out, err := exec.CommandContext(context.Background(), "pfctl", "-a", harmonikPFAnchor, "-F", "rules").CombinedOutput()
			if err != nil {
				return fmt.Errorf("network sandbox release: pfctl flush anchor %q: %w (output: %s)",
					harmonikPFAnchor, err, strings.TrimSpace(string(out)))
			}
			return nil
		},
	}, nil
}

func isNetworkSandboxActive() bool {
	out, err := exec.CommandContext(context.Background(), "pfctl", "-a", harmonikPFAnchor, "-s", "rules").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "block")
}
