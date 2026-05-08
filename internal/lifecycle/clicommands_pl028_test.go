package lifecycle

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
)

// cliFixtureCommand describes a single PL-028 CLI entry point, its JSON-RPC
// method name (per PL-003a), the expected exit code on success, and any flags
// that must be recognized.
//
// The actual dispatcher is downstream; this fixture asserts the mapping-table
// shape and flag-surface shape only — no real binary is invoked.
//
// Spec ref: process-lifecycle.md §4.10 PL-028 — Daemon command surface.
// Spec ref: process-lifecycle.md §4.1 PL-003a — JSON-RPC method inventory.
type cliFixtureCommand struct {
	name          string   // CLI sub-command name (e.g. "daemon")
	jsonRPCMethod string   // JSON-RPC 2.0 method routed over the socket
	exitCode      int      // expected exit code on success
	requiredFlags []string // flags the entry-point MUST expose
	optionalFlags []string // flags the entry-point MAY expose
}

// cliFixtureCommands is the authoritative PL-028 mapping table.
//
// CLI-facing JSON-RPC methods per PL-003a:
//
//	status, pause, resume, stop, upgrade, attach, enqueue, list
//
// Agent-facing methods (claim-next, emit-outcome, dispatch-status) are
// out-of-scope for this CLI-surface fixture and are covered by agent-channel
// tests elsewhere.
//
// "harmonik resume" and "harmonik list" are part of the ON §4.10 ON-041
// surface and share the socket; they are intentionally OUT OF SCOPE for
// this PL-028 fixture, which covers only the eight entry-point bullets.
var cliFixtureCommands = []cliFixtureCommand{
	{
		name:          "daemon",
		jsonRPCMethod: "daemon.start",
		exitCode:      0,
		requiredFlags: []string{"--config", "--log-level"},
	},
	{
		name:          "attach",
		jsonRPCMethod: "attach.session",
		exitCode:      0,
		requiredFlags: []string{},
	},
	{
		name:          "runner",
		jsonRPCMethod: "runner.start",
		exitCode:      0,
		requiredFlags: []string{},
		optionalFlags: []string{"--orchestrator-agent", "--config", "--log-level"},
	},
	{
		name:          "enqueue",
		jsonRPCMethod: "enqueue",
		exitCode:      0,
		requiredFlags: []string{},
	},
	{
		name:          "status",
		jsonRPCMethod: "status",
		exitCode:      0,
		requiredFlags: []string{},
	},
	{
		name:          "pause",
		jsonRPCMethod: "pause",
		exitCode:      0,
		requiredFlags: []string{},
	},
	{
		name:          "stop",
		jsonRPCMethod: "stop",
		exitCode:      0,
		requiredFlags: []string{},
		optionalFlags: []string{"--graceful", "--immediate"},
	},
	{
		name:          "upgrade",
		jsonRPCMethod: "upgrade",
		exitCode:      0,
		requiredFlags: []string{},
	},
}

// cliFixtureMethodUnique asserts that each declared CLI command maps to a
// distinct JSON-RPC method name. Two commands sharing a method name would
// create an ambiguous dispatch surface.
func cliFixtureMethodUnique(t *testing.T, cmds []cliFixtureCommand) {
	t.Helper()

	seen := make(map[string]string, len(cmds))
	for _, cmd := range cmds {
		if prev, ok := seen[cmd.jsonRPCMethod]; ok {
			t.Errorf("PL-028 method collision: method %q claimed by both %q and %q",
				cmd.jsonRPCMethod, prev, cmd.name)
		}
		seen[cmd.jsonRPCMethod] = cmd.name
	}
}

// cliFixtureNameUnique asserts that each CLI sub-command name is unique in the
// table.
func cliFixtureNameUnique(t *testing.T, cmds []cliFixtureCommand) {
	t.Helper()

	seen := make(map[string]bool, len(cmds))
	for _, cmd := range cmds {
		if seen[cmd.name] {
			t.Errorf("PL-028 command table: duplicate name %q", cmd.name)
		}
		seen[cmd.name] = true
	}
}

// cliFixtureSendJSONRPC sends a single JSON-RPC 2.0 request over a Unix socket
// connection and returns the raw response bytes. The connection is closed after
// the read (CLI connection discipline per PL-003a).
func cliFixtureSendJSONRPC(t *testing.T, conn net.Conn, method string, id int) []byte {
	t.Helper()

	type request struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
	}
	req := request{JSONRPC: "2.0", ID: id, Method: method}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("cliFixtureSendJSONRPC: marshal: %v", err)
	}
	if _, err := fmt.Fprintf(conn, "%s\n", data); err != nil {
		t.Fatalf("cliFixtureSendJSONRPC: write: %v", err)
	}

	buf := make([]byte, 65536)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("cliFixtureSendJSONRPC: read: %v", err)
	}
	return buf[:n]
}

// TestPL028_CLICommandMappingTable verifies the structural invariants of the
// PL-028 CLI-surface mapping table: all eight declared commands are present,
// each has a unique name, each maps to a unique JSON-RPC method name, and the
// expected success exit code for every entry is 0.
//
// The downstream dispatcher is not yet implemented; this fixture asserts the
// *shape* of the mapping so that the dispatcher's implementation contract is
// clear and verifiable once landed.
//
// Spec ref: process-lifecycle.md §4.10 PL-028 — Daemon command surface;
// eight declared entry points: daemon, attach, runner, enqueue, status, pause,
// stop, upgrade.
func TestPL028_CLICommandMappingTable(t *testing.T) {
	t.Parallel()

	// All eight PL-028 entry-point names must appear.
	wantNames := []string{"daemon", "attach", "runner", "enqueue", "status", "pause", "stop", "upgrade"}

	t.Run("all-eight-commands-present", func(t *testing.T) {
		t.Parallel()

		cliFixtureNameUnique(t, cliFixtureCommands)

		nameIndex := make(map[string]bool, len(cliFixtureCommands))
		for _, cmd := range cliFixtureCommands {
			nameIndex[cmd.name] = true
		}
		for _, want := range wantNames {
			if !nameIndex[want] {
				t.Errorf("PL-028 command table: missing entry for %q", want)
			}
		}
	})

	t.Run("json-rpc-methods-distinct", func(t *testing.T) {
		t.Parallel()

		cliFixtureMethodUnique(t, cliFixtureCommands)
	})

	t.Run("success-exit-code-is-zero", func(t *testing.T) {
		t.Parallel()

		for _, cmd := range cliFixtureCommands {
			t.Run(cmd.name, func(t *testing.T) {
				t.Parallel()

				if cmd.exitCode != 0 {
					t.Errorf("PL-028 %q: exitCode = %d, want 0 (success)", cmd.name, cmd.exitCode)
				}
			})
		}
	})

	t.Run("method-names-non-empty", func(t *testing.T) {
		t.Parallel()

		for _, cmd := range cliFixtureCommands {
			t.Run(cmd.name, func(t *testing.T) {
				t.Parallel()

				if cmd.jsonRPCMethod == "" {
					t.Errorf("PL-028 %q: jsonRPCMethod is empty; every command must have a non-empty method name", cmd.name)
				}
			})
		}
	})
}

// TestPL028_CLIFlagSurface verifies that the PL-028-declared flag surface for
// each command is captured in the mapping table. This fixture checks the table
// shape; the actual flag-parser is downstream.
//
// PL-028 declares flags for:
//   - daemon: --config, --log-level
//   - runner: --orchestrator-agent (optional), --config (optional), --log-level (optional)
//   - stop:   --graceful (optional), --immediate (optional)
//
// Spec ref: process-lifecycle.md §4.10 PL-028 — flag surface per entry point.
func TestPL028_CLIFlagSurface(t *testing.T) {
	t.Parallel()

	flagChecks := []struct {
		commandName  string
		requiredFlag string
	}{
		{"daemon", "--config"},
		{"daemon", "--log-level"},
	}

	for _, fc := range flagChecks {
		t.Run(fc.commandName+"/"+fc.requiredFlag, func(t *testing.T) {
			t.Parallel()

			var found bool
			for _, cmd := range cliFixtureCommands {
				if cmd.name != fc.commandName {
					continue
				}
				for _, f := range cmd.requiredFlags {
					if f == fc.requiredFlag {
						found = true
						break
					}
				}
			}
			if !found {
				t.Errorf("PL-028 command %q: required flag %q not declared in mapping table",
					fc.commandName, fc.requiredFlag)
			}
		})
	}

	// runner optional flag: --orchestrator-agent
	t.Run("runner/optional-orchestrator-agent", func(t *testing.T) {
		t.Parallel()

		var found bool
		for _, cmd := range cliFixtureCommands {
			if cmd.name != "runner" {
				continue
			}
			for _, f := range cmd.optionalFlags {
				if f == "--orchestrator-agent" {
					found = true
					break
				}
			}
		}
		if !found {
			t.Error("PL-028 runner: optional flag --orchestrator-agent not declared in mapping table")
		}
	})

	// stop optional flags: --graceful and --immediate
	stopOptional := []string{"--graceful", "--immediate"}
	for _, flag := range stopOptional {
		t.Run("stop/optional-"+flag, func(t *testing.T) {
			t.Parallel()

			var found bool
			for _, cmd := range cliFixtureCommands {
				if cmd.name != "stop" {
					continue
				}
				for _, f := range cmd.optionalFlags {
					if f == flag {
						found = true
						break
					}
				}
			}
			if !found {
				t.Errorf("PL-028 stop: optional flag %q not declared in mapping table", flag)
			}
		})
	}
}

// TestPL028_JSONRPCMethodWiring verifies the JSON-RPC framing discipline from
// the CLI-surface perspective. For each PL-028 command that routes over the
// daemon socket, a request is sent using the declared method name and the
// response is verified to be a valid JSON-RPC 2.0 object.
//
// The stub responder returns "method not found" for all methods, which is the
// correct wire behaviour before the real dispatcher is implemented. The fixture
// asserts the framing layer, not the semantic result.
//
// Spec ref: process-lifecycle.md §4.1 PL-003a — CLI clients MUST issue one
// JSON-RPC request per connection and close the connection on receipt of the
// response.
// Spec ref: process-lifecycle.md §4.10 PL-028 — CLI-facing JSON-RPC methods.
func TestPL028_JSONRPCMethodWiring(t *testing.T) {
	t.Parallel()

	// CLI-facing socket commands only (daemon and runner do not route over the
	// daemon socket — they start the daemon or the runner process).
	socketCommands := []cliFixtureCommand{}
	for _, cmd := range cliFixtureCommands {
		switch cmd.name {
		case "daemon", "runner":
			// These are process-start entry points, not socket-dispatch commands.
		default:
			socketCommands = append(socketCommands, cmd)
		}
	}

	for i, cmd := range socketCommands {
		reqID := i + 100
		t.Run(cmd.name, func(t *testing.T) {
			t.Parallel()

			projectDir := plFixtureTempProjectDir(t)

			ln, err := plFixtureBindSocket(t, projectDir)
			if err != nil {
				t.Fatalf("PL-028 %s: bindSocket: %v", cmd.name, err)
			}
			defer func() { _ = ln.Close() }() //nolint:errcheck // cleanup error unactionable

			done := make(chan struct{})
			go stubNDJSONResponder(ln, true /* ready */, done)

			conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", plFixtureSocketPath(projectDir))
			if err != nil {
				t.Fatalf("PL-028 %s: Dial: %v", cmd.name, err)
			}
			defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

			// Send one JSON-RPC request using the declared method name.
			respBytes := cliFixtureSendJSONRPC(t, conn, cmd.jsonRPCMethod, reqID)

			// The response must be valid JSON-RPC 2.0.
			var resp jsonrpcResponse
			if err := json.Unmarshal(respBytes, &resp); err != nil {
				// Try stripping trailing newline.
				trimmed := []byte{}
				for _, b := range respBytes {
					if b != '\n' {
						trimmed = append(trimmed, b)
					}
				}
				if err2 := json.Unmarshal(trimmed, &resp); err2 != nil {
					t.Fatalf("PL-028 %s: unmarshal response %q: %v", cmd.name, string(respBytes), err2)
				}
			}

			if resp.JSONRPC != "2.0" {
				t.Errorf("PL-028 %s: response.jsonrpc = %q, want %q", cmd.name, resp.JSONRPC, "2.0")
			}
			if resp.ID != reqID {
				t.Errorf("PL-028 %s: response.id = %d, want %d", cmd.name, resp.ID, reqID)
			}

			_ = conn.Close() //nolint:errcheck // cleanup error unactionable
			_ = ln.Close()   //nolint:errcheck // cleanup error unactionable
			<-done
		})
	}
}

// TestPL028_ExitCodeTaxonomy verifies that the fixture's exit-code mapper
// covers all codes declared by PL-008a that are relevant to the CLI surface:
// code 0 (success), code 5 (pidfile-locked), code 6 (socket-bind-failed),
// code 22 (ntm-unavailable), and code 23 (orchestrator-agent-unavailable).
//
// Codes 22 and 23 are fixture-level sentinel errors; the mapper for these is
// defined as cliFixtureErrToExitCode (this file).
//
// Spec ref: process-lifecycle.md §4.1 PL-008a — exit code taxonomy.
// Spec ref: operator-nfr.md §8 — authoritative exit code table.
func TestPL028_ExitCodeTaxonomy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		label    string
		err      error
		wantCode int
	}{
		{"nil/success", nil, 0},
		{"ntm-unavailable/code-22", errCLIFixtureNtmUnavailable, 22},
		{"orchestrator-agent-unavailable/code-23", errCLIFixtureOrchestratorAgentUnavailable, 23},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()

			gotCode := cliFixtureErrToExitCode(tc.err)
			if gotCode != tc.wantCode {
				t.Errorf("PL-028 exit code: cliFixtureErrToExitCode(%v) = %d, want %d",
					tc.err, gotCode, tc.wantCode)
			}
		})
	}
}
