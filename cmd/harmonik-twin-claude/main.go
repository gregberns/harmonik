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

// run is the testable entry-point; it returns an exit code.
//
// Exit codes:
//
//	0 — completed successfully (socket path or stdout path).
//	1 — precondition failure (unknown scenario, dial error, missing socket when required, etc.).
func run() int {
	fs := flag.NewFlagSet("harmonik-twin-claude", flag.ContinueOnError)

	// --version: print the build-time commit-hash stamp and exit 0.
	// The stamp is injected via -ldflags "-X main.commitHash=<sha>" at build
	// time (see version.go). Per HC-043: in-repo handler binaries MUST embed
	// a commit hash; the daemon's VerifyCommitHash gate checks this before
	// launch. --version provides the human-readable complement.
	// Cite: specs/handler-contract.md §4.10.HC-043.
	showVersion := fs.Bool("version", false, "print the build-time commit hash and exit (HC-043)")

	// --socket-path: the Unix-domain socket path supplied by the daemon at
	// launch time. Per §4.10.HC-044/HC-007 the production daemon listens at
	// .harmonik/daemon.sock; this flag is the launch-time delivery vehicle
	// (the path is NOT a LaunchSpec field per §6.1).
	// Optional when --scenario is set: in that case output goes to stdout
	// (CHB-022 stdout-watcher topology).
	socketPath := fs.String("socket-path", "", "Unix-domain socket path the daemon is listening on (HC-044; required unless --scenario is set)")

	// --launch-spec: file-path form of LaunchSpec delivery per HC-005.
	// When the LaunchSpec payload exceeds 1 MiB the daemon writes it to a
	// temp file and passes its path here instead of encoding it on stdin.
	// TODO(hk-ahvq.48.2): parse the LaunchSpec file in the wire-protocol bead.
	launchSpecPath := fs.String("launch-spec", "", "path to a JSON file containing the LaunchSpec (HC-005 file-path form; optional)")

	// --script-path: YAML script file for scenario-mode (hk-ahvq.48.3).
	// When supplied, the twin reads the script and emits the declared message
	// stream on the wire-protocol path, implementing the HC-026a scripted-mode
	// carve-out and HC-036 twin-parity requirement.
	// Schema: <fixture-root>/<scenario>/twin-scripts/<role>.yaml.
	// Normative spec section: follow-up bead hk-ahvq.48.11.
	// Cite: specs/handler-contract.md §4.6.HC-026a, §4.8.HC-036.
	scriptPath := fs.String("script-path", "", "path to the YAML script file for scenario-mode (HC-026a; optional)")

	// --scenario: canned scenario name per CHB-021 §10.
	// When set the twin loads the named canned ScriptFile from scenarios.go
	// and drives the script-driver loop.  Output goes to the UDS connection
	// when --socket-path is also supplied; otherwise to os.Stdout (stdout-
	// watcher topology per CHB-022 twin-blind routing).
	// Cite: specs/claude-hook-bridge.md §4.8.CHB-021, §10.
	scenarioName := fs.String("scenario", "", "canned scenario name (CHB-021 §10; optional; implies stdout when --socket-path is absent)")

	// --worktree-path: absolute path to the project worktree that this twin
	// session is running against (hk-e66ht / audit items 1+2).
	// When set, the twin reads <worktree-path>/.claude/settings.json at startup
	// and emits twin_settings_loaded. The path is also used as cwd for hook
	// subprocesses so that relative-path hook commands resolve correctly.
	// Optional: when absent, twin_settings_loaded is NOT emitted and the
	// call_stop_hook script step will error if used.
	worktreePath := fs.String("worktree-path", "", "absolute path to the project worktree; twin reads .claude/settings.json from here (hk-e66ht; optional)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		// flag.ContinueOnError: parse errors are already printed to stderr by
		// the flag package; just exit with a non-zero code.
		return 1
	}

	// Handle --version before any further validation.
	if *showVersion {
		writeVersion(os.Stdout)
		return 0
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

	// Resolve the ScriptFile to drive.  Priority: --scenario > --script-path.
	// If neither is set, scriptFile is nil (no-op script path).
	var scriptFile *ScriptFile
	switch {
	case *scenarioName != "":
		// Canned scenario mode (CHB-021): load the named scenario from the
		// embedded scenario table in scenarios.go.
		var err error
		scriptFile, err = cannedScenario(*scenarioName)
		if err != nil {
			fmt.Fprintln(os.Stderr, "harmonik-twin-claude:", err)
			return 1
		}
	case *scriptPath != "":
		// YAML script file mode (hk-ahvq.48.3).
		var err error
		scriptFile, err = loadScriptFile(*scriptPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik-twin-claude: --script-path: %v\n", err)
			return 1
		}
	}

	// Load settings.json when --worktree-path is supplied (hk-e66ht).
	// The settings are loaded before the socket connection is opened so that
	// any malformed-JSON error can be reported before wire-protocol negotiation.
	// twin_settings_loaded is emitted on the wire AFTER the output writer is
	// established (below), so we stash the result in a variable here.
	var loadedSettings *cloneSettings
	if *worktreePath != "" {
		var settingsErr error
		loadedSettings, settingsErr = loadCloneSettings(*worktreePath)
		if settingsErr != nil {
			// Malformed JSON → error + exit 1 per bead error policy.
			fmt.Fprintf(os.Stderr, "harmonik-twin-claude: settings.json: %v\n", settingsErr)
			return 1
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Determine the output writer.
	//
	// Scenario mode without --socket-path: write NDJSON to os.Stdout.
	// This matches the handler.Launch stdout-watcher topology: the daemon's
	// Watcher reads subprocess stdout, so writing to stdout is functionally
	// identical to writing over a UDS from the daemon's perspective (CHB-022).
	//
	// Socket path present: dial the UDS and write there (production path /
	// --script-path mode / scenario mode with socket override).
	//
	// Neither scenario nor socket path: --socket-path is required.
	var out io.Writer
	if *socketPath != "" {
		conn, err := dialSocket(ctx, *socketPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik-twin-claude: dial %s: %v\n", *socketPath, err)
			return 1
		}
		defer func() { _ = conn.Close() }()
		out = conn
	} else if scriptFile != nil {
		// Scenario or script mode without socket path: stdout fallback (CHB-022).
		out = os.Stdout
	} else {
		// No scenario/script and no socket path: socket-path is required.
		fmt.Fprintln(os.Stderr, "harmonik-twin-claude: --socket-path is required")
		return 1
	}

	// Script-driver loop (hk-ahvq.48.3 / CHB-021): when a script file is
	// loaded, run the declared message stream on the wire-protocol path.
	// This satisfies HC-036 (subprocess script drives output instead of an LLM)
	// and the HC-026a scripted-mode carve-out for scenario-reproducible
	// heartbeats.
	if scriptFile != nil {
		emitter := newWireEmitter(out)

		// startup_delay_ms: sleep AFTER flag-parse but BEFORE emitting
		// handler_capabilities (audit item 6, hk-8ys88). Models the
		// splash-dismiss window for daemon-side timeout-sensitivity scenarios.
		// Does NOT exercise the tmux pane-delivery path (real-claude-only).
		// Sleep is context-aware: cancelled mid-sleep → clean exit.
		if scriptFile.StartupDelayMs > 0 {
			delay := time.Duration(scriptFile.StartupDelayMs) * time.Millisecond
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
				// Delay elapsed normally.
			case <-ctx.Done():
				timer.Stop()
				fmt.Fprintf(os.Stderr, "harmonik-twin-claude: startup delay cancelled: %v\n", ctx.Err())
				return 1
			}
		}

		// Emit twin_settings_loaded when --worktree-path was supplied (hk-e66ht).
		// loadedSettings is non-nil iff --worktree-path was set and settings.json
		// was valid (or absent — absent produces a zero-value cloneSettings).
		// When --worktree-path was NOT set, we do not emit this message at all:
		// the feature is opt-in, and existing scenarios without the flag continue
		// to work unchanged.
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

// dialSocket opens a Unix-domain socket connection to the daemon.
//
// Uses (&net.Dialer{}).DialContext per the project lint rule that forbids the
// plain net.Dial helper (implementer-protocol.md §Lint compliance).
func dialSocket(ctx context.Context, socketPath string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
}
