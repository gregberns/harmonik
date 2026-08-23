// Command harmonik-twin-claude is the canonical twin binary for the Claude
// Code handler. The daemon's handler subsystem subprocess-launches this
// binary in scenario tests and CI in place of a real Claude Code session.
//
// # Scope (hk-ahvq.48.1, hk-ahvq.48.3, hk-ahvq.48.4, hk-w5vra.2, hk-e66ht)
//
// This scaffold covers:
//   - Flag parsing: --socket-path (LaunchSpec.SocketPath per §6.1),
//     --launch-spec (file-path form of LaunchSpec delivery per HC-005),
//     --script-path (YAML script file for scenario-mode per §4.6.HC-026a /
//     §4.8.HC-036; schema documented in scriptdriver.go),
//     --scenario (canned scenario name per CHB-021 §10; emits to stdout when
//     --socket-path is absent),
//     --worktree-path (path to the worktree; twin reads .claude/settings.json
//     from this directory at startup per hk-e66ht / audit items 1+2), and
//     --version (prints the build-time commit-hash stamp per HC-043).
//   - Unix-domain-socket dial-back to the daemon per §4.10.HC-044 /
//     §4.10.HC-045.
//   - Stdout fallback: when --scenario is set without --socket-path the twin
//     writes NDJSON to os.Stdout, matching the handler.Launch stdout-watcher
//     topology (CHB-022).
//   - Settings.json reader: when --worktree-path is set the twin reads
//     <worktree-path>/.claude/settings.json and emits twin_settings_loaded
//     with permissions_present and stop_hook_present fields (hk-e66ht).
//   - Stop hook caller: YAML script step "call_stop_hook" executes the loaded
//     Stop hook command and emits twin_hook_called (hk-e66ht).
//   - Clean exit (exit code 1) when preconditions fail.
//   - Build-time commit-hash stamp via -ldflags (hk-ahvq.48.4); the
//     commitHash variable is declared in version.go.
//   - Script-driver loop: reads --script-path YAML and emits the declared
//     message stream on the wire-protocol path (hk-ahvq.48.3).
//
// # Scenario mode (CHB-021)
//
// When --scenario is supplied the twin selects a canned ScriptFile from
// scenarios.go and drives the script-driver loop.  If --socket-path is also
// provided the output goes over the UDS connection; otherwise it goes to
// os.Stdout, which is what handler.Launch reads via the stdout-watcher
// topology (CHB-022 twin-blind routing).
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
// §4.10.HC-043, §4.10.HC-044, §4.10.HC-045, §6.1;
// specs/claude-hook-bridge.md §4.8.CHB-021, §4.8.CHB-022, §10;
// docs/twin-parity-audit-2026-05-14.md §4 items 1+2 (hk-e66ht).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	os.Exit(run())
}

func run() int {
	fs := flag.NewFlagSet("harmonik-twin-claude", flag.ContinueOnError)

	showVersion := fs.Bool("version", false, "print the build-time commit hash and exit (HC-043)")

	socketPath := fs.String("socket-path", "", "Unix-domain socket path the daemon is listening on (HC-044; required unless --scenario is set)")

	// --launch-spec: file-path form of LaunchSpec delivery per HC-005.
	// When the LaunchSpec payload exceeds 1 MiB the daemon writes it to a
	// temp file and passes its path here instead of encoding it on stdin.
	// TODO(hk-ahvq.48.2): parse the LaunchSpec file in the wire-protocol bead.
	launchSpecPath := fs.String("launch-spec", "", "path to a JSON file containing the LaunchSpec (HC-005 file-path form; optional)")

	scriptPath := fs.String("script-path", "", "path to the YAML script file for scenario-mode (HC-026a; optional)")

	scenarioName := fs.String("scenario", "", "canned scenario name (CHB-021 §10; optional; implies stdout when --socket-path is absent)")

	worktreePath := fs.String("worktree-path", "", "absolute path to the project worktree; twin reads .claude/settings.json from here (hk-e66ht; optional)")

	replayPath := fs.String("replay-path", "", "path to a Claude-A wire.ndjson capture to replay verbatim (M6 WS3-Claude-B; takes precedence over --scenario/--script-path)")

	preserveTiming := fs.Bool("preserve-timing", false, "replay with the capture's inter-event delays (default: fast-drain)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return 1
	}

	if *showVersion {
		if err := writeVersion(os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "harmonik-twin-claude: write version: %v\n", err)
			return 1
		}
		return 0
	}

	if *launchSpecPath != "" {
		if _, err := os.Stat(*launchSpecPath); err != nil {
			fmt.Fprintf(os.Stderr, "harmonik-twin-claude: --launch-spec file not found: %v\n", err)
			return 1
		}
	}

	var scriptFile *ScriptFile
	switch {
	case *replayPath != "":
	case *scenarioName != "":
		var err error
		scriptFile, err = cannedScenario(*scenarioName)
		if err != nil {
			fmt.Fprintln(os.Stderr, "harmonik-twin-claude:", err)
			return 1
		}
	case *scriptPath != "":
		var err error
		scriptFile, err = loadScriptFile(*scriptPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik-twin-claude: --script-path: %v\n", err)
			return 1
		}
	}

	var loadedSettings *cloneSettings
	if *worktreePath != "" {
		var settingsErr error
		loadedSettings, settingsErr = loadCloneSettings(*worktreePath)
		if settingsErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik-twin-claude: settings.json: %v\n", settingsErr)
			return 1
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	var out io.Writer
	switch {
	case *socketPath != "":
		conn, err := dialSocket(ctx, *socketPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik-twin-claude: dial %s: %v\n", *socketPath, err)
			return 1
		}
		defer func() {
			if closeErr := conn.Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "harmonik-twin-claude: close conn: %v\n", closeErr)
			}
		}()
		out = conn
	case scriptFile != nil || *replayPath != "":
		out = os.Stdout
	default:
		fmt.Fprintln(os.Stderr, "harmonik-twin-claude: --socket-path is required")
		return 1
	}

	if *replayPath != "" {
		if err := runReplay(ctx, out, *replayPath, *preserveTiming); err != nil {
			fmt.Fprintf(os.Stderr, "harmonik-twin-claude: replay: %v\n", err)
			return 1
		}
		return 0
	}

	if scriptFile != nil {
		emitter := newWireEmitter(out)

		if scriptFile.StartupDelayMs > 0 {
			delay := time.Duration(scriptFile.StartupDelayMs) * time.Millisecond
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				fmt.Fprintf(os.Stderr, "harmonik-twin-claude: startup delay cancelled: %v\n", ctx.Err())
				return 1
			}
		}

		if *worktreePath != "" && loadedSettings != nil {
			if err := emitter.emitTwinSettingsLoaded(
				loadedSettings.permissionsPresent,
				loadedSettings.stopHookPresent,
				loadedSettings.stopHookCommand,
			); err != nil {
				fmt.Fprintf(os.Stderr, "harmonik-twin-claude: emit twin_settings_loaded: %v\n", err)
				return 1
			}
		}

		cfg := scriptRunConfig{
			settings:     loadedSettings,
			worktreePath: *worktreePath,
		}
		if err := runScript(ctx, emitter, scriptFile, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "harmonik-twin-claude: script-driver: %v\n", err)
			return 1
		}
	}

	return 0
}

func dialSocket(ctx context.Context, socketPath string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
}
