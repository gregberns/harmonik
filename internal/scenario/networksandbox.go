package scenario

import "errors"

// ErrNetworkSandboxNotApplied is returned by DriveOrchestration when
// OrchestrationConfig.EnableNetworkSandbox is true but the network sandbox is
// not currently active on this process (IsNetworkSandboxActive returns false).
//
// The CLI harness (G-01 / SH-032) is responsible for activating the sandbox
// via ApplyNetworkSandbox before calling DriveOrchestration.
//
// Spec ref: specs/scenario-harness.md §4.8 SH-028.
var ErrNetworkSandboxNotApplied = errors.New(
	"network sandbox: EnableNetworkSandbox=true but no active sandbox detected; " +
		"call ApplyNetworkSandbox() at process startup before DriveOrchestration",
)

// ErrNetworkSandboxUnsupported is returned by ApplyNetworkSandbox on platforms
// where no sandbox mechanism is implemented.
var ErrNetworkSandboxUnsupported = errors.New(
	"network sandbox: no sandbox mechanism available on this platform",
)

// NetworkSandboxHandle holds the cleanup hook for an active sandbox.
// Release MUST be called on every non-error return from ApplyNetworkSandbox.
type NetworkSandboxHandle struct {
	release func() error
}

// Release tears down the sandbox resources installed by ApplyNetworkSandbox.
// Safe to call on a zero-value or nil-pointer handle (no-op).
//
// On Linux: unlocks the OS thread that was locked for the unshare(CLONE_NEWNET)
// call. On macOS: flushes the pf anchor rules installed by ApplyNetworkSandbox.
func (h *NetworkSandboxHandle) Release() error {
	if h == nil || h.release == nil {
		return nil
	}
	return h.release()
}

// ApplyNetworkSandbox activates the platform-specific network sandbox on the
// calling goroutine / process per specs/scenario-harness.md §4.8 SH-028.
//
// On Linux, calls unshare(CLONE_NEWNET) with runtime.LockOSThread to create a
// new network namespace containing only the loopback interface.
//
// On macOS, installs a pf packet-filter anchor that drops all non-loopback
// egress. Requires root or admin privileges (sudo); returns a wrapped
// ErrNetworkSandboxUnsupported if pfctl is unavailable or permission is denied.
//
// On other platforms, returns ErrNetworkSandboxUnsupported.
//
// The returned handle's Release method MUST be called (typically via defer) to
// free sandbox resources. Release is a no-op on an error return.
//
// Spec ref: specs/scenario-harness.md §4.8 SH-028.
func ApplyNetworkSandbox() (*NetworkSandboxHandle, error) {
	return applyNetworkSandbox()
}

// IsNetworkSandboxActive reports whether the calling process is currently
// executing inside an active network sandbox per SH-028.
//
// On Linux, this is verified by reading /proc/net/dev and confirming that
// no non-loopback interface entries are present (indicating a network namespace
// that contains only the loopback interface).
//
// On macOS, this is verified by querying the pf anchor installed by
// ApplyNetworkSandbox; returns true only if the anchor exists and contains a
// block rule.
//
// On other platforms, always returns false.
//
// Spec ref: specs/scenario-harness.md §4.8 SH-028; §10.2 conformance lane
// obligation to verify "no non-loopback interface is reachable from the harness
// process tree".
func IsNetworkSandboxActive() bool {
	return isNetworkSandboxActive()
}
