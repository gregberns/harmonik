// Command harmonik-twin-claude is the canonical twin binary for the Claude
// Code handler. The daemon's handler subsystem subprocess-launches this
// binary in scenario tests and CI in place of a real Claude Code session.
//
// # Scope (hk-ahvq.48.1)
//
// This scaffold covers:
//   - Flag parsing: --socket-path (LaunchSpec.SocketPath per §6.1) and
//     --launch-spec (file-path form of LaunchSpec delivery per HC-005).
//   - Unix-domain-socket dial-back to the daemon per §4.10.HC-044 /
//     §4.10.HC-045.
//   - Clean exit (exit code 1) when the socket path is missing or the dial
//     fails — precondition for all downstream binary-behaviour beads.
//
// # Out of scope (deferred to sibling beads)
//
// The wire-protocol parity loop (hk-ahvq.48.2), the script-driver loop
// (hk-ahvq.48.3), the commit-hash stamp via ldflags (hk-ahvq.48.4), and the
// Makefile target (hk-ahvq.48.5) are all handled by separate beads.
//
// Cite: specs/handler-contract.md §4.10.HC-044, §4.10.HC-045, §6.1.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	os.Exit(run())
}

// run is the testable entry-point; it returns an exit code.
//
// Exit codes:
//
//	0 — connected successfully and closed cleanly.
//	1 — precondition failure (missing socket path, dial error, etc.).
func run() int {
	fs := flag.NewFlagSet("harmonik-twin-claude", flag.ContinueOnError)

	// --socket-path: the Unix-domain socket path supplied by the daemon.
	// Corresponds to LaunchSpec.SocketPath per specs/handler-contract.md §6.1
	// and the dial-back obligation of §4.10.HC-044.
	socketPath := fs.String("socket-path", "", "Unix-domain socket path the daemon is listening on (LaunchSpec.SocketPath; required)")

	// --launch-spec: file-path form of LaunchSpec delivery per HC-005.
	// When the LaunchSpec payload exceeds 1 MiB the daemon writes it to a
	// temp file and passes its path here instead of encoding it on stdin.
	// TODO(hk-ahvq.48.2): parse the LaunchSpec file in the wire-protocol bead.
	launchSpecPath := fs.String("launch-spec", "", "path to a JSON file containing the LaunchSpec (HC-005 file-path form; optional)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		// flag.ContinueOnError: parse errors are already printed to stderr by
		// the flag package; just exit with a non-zero code.
		return 1
	}

	// Validate precondition: socket-path is required. Exit cleanly without a
	// stack trace so downstream beads can assert on this behaviour.
	if *socketPath == "" {
		fmt.Fprintln(os.Stderr, "harmonik-twin-claude: --socket-path is required")
		return 1
	}

	// Validate precondition: if --launch-spec is provided, the file must exist.
	// The actual LaunchSpec parsing is deferred to hk-ahvq.48.2.
	if *launchSpecPath != "" {
		//nolint:gosec // G304: path is operator-supplied via --launch-spec flag; provenance is the daemon
		if _, err := os.Stat(*launchSpecPath); err != nil {
			fmt.Fprintf(os.Stderr, "harmonik-twin-claude: --launch-spec file not found: %v\n", err)
			return 1
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	conn, err := dialSocket(ctx, *socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik-twin-claude: dial %s: %v\n", *socketPath, err)
		return 1
	}
	defer func() { _ = conn.Close() }()

	// Scaffold only: the parity loop (hk-ahvq.48.2) and script-driver loop
	// (hk-ahvq.48.3) will extend this point. For now, close immediately so
	// that the binary exits cleanly once the connection is established.
	return 0
}

// dialSocket opens a Unix-domain socket connection to the daemon.
//
// Uses (&net.Dialer{}).DialContext per the project lint rule that forbids the
// plain net.Dial helper (implementer-protocol.md §Lint compliance).
func dialSocket(ctx context.Context, socketPath string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
}
