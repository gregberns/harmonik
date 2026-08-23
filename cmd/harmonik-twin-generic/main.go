// Command harmonik-twin-generic is the generic test handler twin binary.
// It emits harmonik-native NDJSON progress-stream messages directly,
// testing the back half of the pipeline without simulating Claude's lifecycle.
//
// # Scope
//
// This binary was renamed from harmonik-twin-claude per bead hk-w5vra.1 to
// make its narrow scope explicit. A separate harmonik-twin-claude binary (bead
// hk-w5vra.2) will mirror what the real Claude CLI lifecycle produces, per
// CHB-021 (specs/claude-hook-bridge.md §4.8).
//
// This scaffold covers:
//   - Flag parsing: --socket-path (LaunchSpec.SocketPath per §6.1),
//     --launch-spec (file-path form of LaunchSpec delivery per HC-005),
//     --script-path (YAML script file for scenario-mode per §4.6.HC-026a /
//     §4.8.HC-036; schema documented in scriptdriver.go), and --version
//     (prints the build-time commit-hash stamp per HC-043).
//   - Unix-domain-socket dial-back to the daemon per §4.10.HC-044 /
//     §4.10.HC-045.
//   - Clean exit (exit code 1) when the socket path is missing or the dial
//     fails — precondition for all downstream binary-behaviour beads.
//   - Build-time commit-hash stamp via -ldflags (hk-ahvq.48.4); the
//     commitHash variable is declared in version.go.
//   - Script-driver loop: reads --script-path YAML and emits the declared
//     message stream on the wire-protocol path (hk-ahvq.48.3).
//
// # Script-file schema (de-facto; hk-ahvq.48.3)
//
// When --script-path is supplied the twin reads a YAML file whose schema is
// documented in scriptdriver.go. The normative spec section is tracked in
// follow-up bead hk-ahvq.48.11.
//
// # Out of scope (deferred to sibling beads)
//
// The Makefile build target wiring up the ldflags stamp is tracked in
// hk-ahvq.48.5.
//
// Cite: specs/handler-contract.md §4.6.HC-026a, §4.8.HC-036,
// §4.10.HC-043, §4.10.HC-044, §4.10.HC-045, §6.1.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	os.Exit(run())
}

func run() int {
	fs := flag.NewFlagSet("harmonik-twin-generic", flag.ContinueOnError)

	showVersion := fs.Bool("version", false, "print the build-time commit hash and exit (HC-043)")

	socketPath := fs.String("socket-path", "", "Unix-domain socket path the daemon is listening on (HC-044; required)")

	// --launch-spec: file-path form of LaunchSpec delivery per HC-005.
	// When the LaunchSpec payload exceeds 1 MiB the daemon writes it to a
	// temp file and passes its path here instead of encoding it on stdin.
	// TODO(hk-ahvq.48.2): parse the LaunchSpec file in the wire-protocol bead.
	launchSpecPath := fs.String("launch-spec", "", "path to a JSON file containing the LaunchSpec (HC-005 file-path form; optional)")

	scriptPath := fs.String("script-path", "", "path to the YAML script file for scenario-mode (HC-026a; optional)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return 1
	}

	if *showVersion {
		if err := writeVersion(os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "harmonik-twin-generic: write version: %v\n", err)
			return 1
		}
		return 0
	}

	if *socketPath == "" {
		fmt.Fprintln(os.Stderr, "harmonik-twin-generic: --socket-path is required")
		return 1
	}

	if *launchSpecPath != "" {
		if _, err := os.Stat(*launchSpecPath); err != nil {
			fmt.Fprintf(os.Stderr, "harmonik-twin-generic: --launch-spec file not found: %v\n", err)
			return 1
		}
	}

	var scriptFile *ScriptFile
	if *scriptPath != "" {
		var err error
		scriptFile, err = loadScriptFile(*scriptPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik-twin-generic: --script-path: %v\n", err)
			return 1
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	conn, err := dialSocket(ctx, *socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik-twin-generic: dial %s: %v\n", *socketPath, err)
		return 1
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("harmonik-twin-generic: close socket: %v", closeErr)
		}
	}()

	if scriptFile != nil {
		emitter := newWireEmitter(conn)
		if err := runScript(ctx, emitter, scriptFile); err != nil {
			fmt.Fprintf(os.Stderr, "harmonik-twin-generic: script-driver: %v\n", err)
			return 1
		}
	}

	return 0
}

func dialSocket(ctx context.Context, socketPath string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
}
