package lifecycle

import (
	"fmt"
	"runtime"
)

// socketpathlimit.go — platform sun_path length guard for the daemon's
// Unix-domain socket (SocketPath / daemonpaths.go PL-003).
//
// struct sockaddr_un.sun_path is a fixed-size char array: 104 bytes on
// Darwin/BSD, 108 bytes on Linux (both figures include the NUL terminator).
// bind(2)/connect(2) on a path at or beyond that size fail with EINVAL/
// ENAMETOOLONG. A deeply-nested real project directory (e.g. a repo checked
// out several levels under a synced-storage mount) can push
// "<projectDir>/.harmonik/daemon.sock" past this limit.
//
// Two call sites need this check to FAIL LOUD instead of silently degrading:
//   - daemon.go binds the socket at startup; PL-003 treats a bind failure as
//     non-fatal so the daemon still comes up — but a too-long path never
//     self-heals (unlike a transient stale-socket race), leaving the daemon
//     permanently socket-less with no clear signal why.
//   - the remote reverse-tunnel (daemon/reversetunnel.go) forwards a worker
//     TCP port back to this same socket path via `ssh -R <port>:<path>`. ssh
//     does not validate that local forward destination at tunnel start —
//     only when a connection actually needs forwarding — so the tunnel
//     readiness gate (a `nc -z` probe against the WORKER's TCP listener)
//     reports ready even though the forward's destination can never connect.
//     The failure otherwise surfaces only as an unexplained agent_ready
//     timeout deep into the run.
//
// Bead ref: hk-ta6dg.

// darwinSunPathMax is sizeof(sockaddr_un.sun_path) on Darwin/BSD, including
// the NUL terminator.
const darwinSunPathMax = 104

// linuxSunPathMax is sizeof(sockaddr_un.sun_path) on Linux (and used as the
// fallback for any other platform), including the NUL terminator.
const linuxSunPathMax = 108

// sunPathMax returns the platform's sockaddr_un.sun_path array size,
// including the NUL terminator.
func sunPathMax() int {
	if runtime.GOOS == "darwin" {
		return darwinSunPathMax
	}
	return linuxSunPathMax
}

// ValidateSocketPathLength returns an error describing why sockPath cannot
// be bound/connected as a Unix-domain socket when it is at or beyond the
// platform's sun_path capacity (leaving no room for the NUL terminator the
// kernel appends). Returns nil when sockPath fits.
//
// Bead ref: hk-ta6dg.
func ValidateSocketPathLength(sockPath string) error {
	max := sunPathMax()
	// One byte of the array is reserved for the NUL terminator the kernel
	// writes, so the usable path length is max-1.
	if len(sockPath) < max {
		return nil
	}
	return fmt.Errorf(
		"socket path %q is %d bytes, at or beyond the platform sun_path limit of %d bytes "+
			"(%d usable, incl. NUL terminator) — bind/connect will fail with EINVAL/ENAMETOOLONG; "+
			"move the project to a shorter path (e.g. a shallower directory or a shorter symlink) "+
			"so <projectDir>/.harmonik/daemon.sock fits",
		sockPath, len(sockPath), max, max-1,
	)
}
