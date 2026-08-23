package scenario

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
)

func applyNetworkSandbox() (*NetworkSandboxHandle, error) {
	runtime.LockOSThread()

	if err := syscall.Unshare(syscall.CLONE_NEWNET); err != nil {
		runtime.UnlockOSThread()
		return nil, fmt.Errorf(
			"network sandbox: unshare(CLONE_NEWNET): %w "+
				"(requires CAP_SYS_ADMIN or CONFIG_USER_NS user namespace support)",
			err,
		)
	}

	if out, err := exec.Command("ip", "link", "set", "lo", "up").CombinedOutput(); err != nil {
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("network sandbox: ip link set lo up: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}

	return &NetworkSandboxHandle{
		release: func() error {
			runtime.UnlockOSThread()
			return nil
		},
	}, nil
}

func isNetworkSandboxActive() bool {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines[2:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		if iface != "" && iface != "lo" {
			return false // non-loopback interface present → not sandboxed
		}
	}
	return true
}
