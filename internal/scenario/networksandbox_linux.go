package scenario

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
)

// applyNetworkSandbox implements ApplyNetworkSandbox on Linux.
//
// Calls runtime.LockOSThread to pin the current goroutine to its OS thread,
// then unshare(CLONE_NEWNET) to enter a new network namespace, then
// "ip link set lo up" to bring up the loopback interface in the new namespace.
//
// Important: this approach isolates the current OS thread's network namespace.
// For full process-level isolation (all goroutines), the harness binary SHOULD
// be launched via `unshare --net <binary>` at process startup so the entire Go
// runtime starts in the isolated namespace. The per-goroutine approach used
// here satisfies the conformance-lane test obligation for scenarios that run
// the daemon synchronously on the locked goroutine (no concurrent network
// access from other goroutines expected during scenario execution).
//
// Spec ref: specs/scenario-harness.md §4.8 SH-028.
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

	// Bring up loopback in the new namespace.
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

// isNetworkSandboxActive implements IsNetworkSandboxActive on Linux.
//
// Reads /proc/net/dev and returns true iff only the loopback interface ("lo")
// is present, which is the observable signature of a network namespace created
// by unshare(CLONE_NEWNET) with only loopback brought up.
//
// Spec ref: specs/scenario-harness.md §10.2 — "on Linux the network-namespace
// mechanism is verified by inspecting that no non-loopback interface is
// reachable from the harness process tree."
func isNetworkSandboxActive() bool {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	// /proc/net/dev has two header lines (column headings); skip them.
	for _, line := range lines[2:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "iface: rxbytes rxpackets ..." — split on first colon.
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
