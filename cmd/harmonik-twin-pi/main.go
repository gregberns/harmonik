// Command harmonik-twin-pi is the pi test twin binary (M6 WS3-pi).
//
// It simulates the `pi --mode json` CLI surface for use in harmonik scenario
// tests and CI runs in place of a real pi session. The binary mimics pi's
// non-interactive NDJSON lifecycle deterministically so it can be dropped in as
// the HandlerBinary the pi adapter (internal/handler/adapter_pi.go) resolves.
//
// # Why a dedicated binary (not a pi-mode of a generic twin)
//
// LOCKED decision (M6-PLAN §WS3): pi's NDJSON dialect differs enough from
// codex/claude that a shared twin would be a leaky abstraction — pi emits its
// own event vocabulary (`session` → `message_start`/`message_end` → `agent_end`)
// that no other harness uses (internal/daemon/pijsonlparser.go). A dedicated
// binary mirrors the codex precedent (cmd/harmonik-twin-codex).
//
// # Interface compatibility
//
// The twin accepts the subset of the pi flag surface the adapter emits
// (internal/daemon/pilaunchspec.go §buildPiLaunchSpec):
//
//	Initial: harmonik-twin-pi --mode json --no-extensions --provider <p> --model <m> "<prompt>"
//	Resume:  harmonik-twin-pi --mode json --no-extensions --session <id> "<prompt>"
//
// All of --mode/--no-extensions/--provider/--model/--session and any positional
// prompt text are accepted and (except --session, echoed as the session id when
// present) silently ignored. --scenario selects a canned lifecycle.
//
// # NDJSON output format (pi --mode json surface)
//
//   - {"type":"session","version":3,"id":"<uuid>","cwd":"<dir>"} — always first
//   - {"type":"message_start","message":{"usage":{"input_tokens":N,...}}}
//   - {"type":"message_end","usage":{"output_tokens":N}}
//   - {"type":"agent_end","messages":[{"role":"assistant","usage":{...}}]} — terminal
//
// Normative dialect reference: internal/daemon/pijsonlparser.go (the real parser
// this twin must drive identically to a live pi).
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	os.Exit(run())
}

// run is the testable entry-point; it returns an exit code.
//
// Exit codes:
//
//	0 — scenario completed successfully.
//	1 — precondition failure (unknown scenario, missing required flag, etc.).
func run() int {
	fs := flag.NewFlagSet("harmonik-twin-pi", flag.ContinueOnError)

	showVersion := fs.Bool("version", false, "print the build-time commit hash and exit (HC-043)")
	scenarioName := fs.String("scenario", "", "canned scenario name (one of: happy-path, empty-turn)")

	// pi --mode json interface flags — accepted for CLI compatibility.
	_ = fs.String("mode", "", "pi output mode (accepted; twin always emits NDJSON)")
	_ = fs.Bool("no-extensions", false, "pi --no-extensions (accepted; ignored by twin)")
	_ = fs.String("provider", "", "pi provider (accepted; ignored by twin)")
	_ = fs.String("model", "", "pi model (accepted; ignored by twin)")
	sessionFlag := fs.String("session", "", "pi resume session id (accepted; echoed as the session id when set)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return 1
	}

	if *showVersion {
		writeVersion(os.Stdout)
		return 0
	}

	if *scenarioName == "" {
		fmt.Fprintln(os.Stderr, "harmonik-twin-pi: --scenario is required")
		return 1
	}

	cfg := scenarioConfig{sessionOverride: strings.TrimSpace(*sessionFlag)}
	if err := runScenario(os.Stdout, *scenarioName, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "harmonik-twin-pi:", err)
		return 1
	}
	return 0
}
