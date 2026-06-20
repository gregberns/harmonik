package main

// crew.go — `harmonik crew` CLI subcommand block (captain & crew spec C2 §3.1).
//
// Routes `harmonik crew <verb>` to the appropriate handler. Implements:
//   - start  (C2 crew-start daemon RPC; exit 17 when daemon down)
//   - stop   (C2 crew-stop daemon RPC; exit 17 when daemon down)
//   - list   (read-only local read of .harmonik/crew/*.json; works daemon-down)
//
// Flag reference for `harmonik crew start`:
//
//	<name>              Crew member name (charset [a-z0-9-], 1–64 chars). Required.
//	--queue <q>         Named queue the crew is bound to. Required.
//	--mission <path>    Path to the mission handoff file. Required.
//	--socket PATH       Override socket path (default: <project>/.harmonik/daemon.sock).
//	--project DIR       Project directory (default: cwd).
//
// Flag reference for `harmonik crew stop`:
//
//	<name>              Crew member name. Required.
//	--pause-queue       Halt dispatch on the crew's named queue after teardown.
//	--socket PATH       Override socket path (default: <project>/.harmonik/daemon.sock).
//	--project DIR       Project directory (default: cwd).
//
// Flag reference for `harmonik crew list`:
//
//	--json              Emit one JSON object per record (NDJSON).
//	--project DIR       Project directory (default: cwd).
//
// Exit codes:
//
//	0   Success
//	1   Argument error or op rejected
//	2   Unrecognised verb
//	17  Daemon not running (start/stop — socket missing or ECONNREFUSED)
//
// Spec ref: docs/plans/captain/05-specs/c2-spec.md §3.1.
// Bead ref: hk-yj2j6 (C2 CLI).

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/crew"
)

// runCrewSubcommand routes `harmonik crew <verb> [args]`.
// subArgs is os.Args[2:].
func runCrewSubcommand(subArgs []string) int {
	verb := ""
	if len(subArgs) > 0 {
		verb = subArgs[0]
	}

	switch verb {
	case "", "--help", "-h":
		crewUsage()
		return 0
	case "start":
		return runCrewStartSubcommand(subArgs[1:])
	case "stop":
		return runCrewStopSubcommand(subArgs[1:])
	case "list":
		return runCrewListSubcommand(subArgs[1:])
	default:
		fmt.Fprintf(os.Stderr, "harmonik crew: unrecognised verb %q; verbs are: start, stop, list\n", verb)
		return 2
	}
}

// runCrewStartSubcommand implements `harmonik crew start <name> --queue <q> --mission <path>`.
// subArgs is os.Args[3:].
func runCrewStartSubcommand(subArgs []string) int {
	queueFlag := ""
	missionFlag := ""
	socketFlag := ""
	projectFlag := ""
	var positional []string

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			crewStartUsage()
			return 0
		case arg == "--queue" && i+1 < len(subArgs):
			i++
			queueFlag = subArgs[i]
		case strings.HasPrefix(arg, "--queue="):
			queueFlag = strings.TrimPrefix(arg, "--queue=")
		case arg == "--mission" && i+1 < len(subArgs):
			i++
			missionFlag = subArgs[i]
		case strings.HasPrefix(arg, "--mission="):
			missionFlag = strings.TrimPrefix(arg, "--mission=")
		case arg == "--socket" && i+1 < len(subArgs):
			i++
			socketFlag = subArgs[i]
		case strings.HasPrefix(arg, "--socket="):
			socketFlag = strings.TrimPrefix(arg, "--socket=")
		case arg == "--project" && i+1 < len(subArgs):
			i++
			projectFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik crew start: unknown flag %q\n", arg)
			return 1
		default:
			positional = append(positional, arg)
		}
	}

	if len(positional) != 1 {
		fmt.Fprintf(os.Stderr, "harmonik crew start: exactly one positional argument <name> is required\n")
		return 1
	}
	name := positional[0]

	if queueFlag == "" {
		fmt.Fprintf(os.Stderr, "harmonik crew start: --queue is required\n")
		return 1
	}
	if missionFlag == "" {
		fmt.Fprintf(os.Stderr, "harmonik crew start: --mission is required\n")
		return 1
	}

	sockPath := crewResolveSockPath(socketFlag, projectFlag)
	if sockPath == "" {
		return 1
	}

	payload := map[string]any{
		"name":         name,
		"queue":        queueFlag,
		"mission_path": missionFlag,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew start: marshal payload: %v\n", err)
		return 1
	}

	reqBytes, err := json.Marshal(map[string]any{
		"op":      "crew-start",
		"payload": json.RawMessage(payloadBytes),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew start: marshal request: %v\n", err)
		return 1
	}

	resp, exitCode := crewDialAndSend(sockPath, "crew start", reqBytes)
	if exitCode != 0 {
		return exitCode
	}

	var result struct {
		SessionID string `json:"session_id"`
		Name      string `json:"name"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew start: decode result: %v\n", err)
		return 1
	}

	// Resolve the absolute project dir so keeper paths are stable.
	absProject := projectFlag
	if absProject == "" {
		wd, wdErr := os.Getwd()
		if wdErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik crew start: cannot determine cwd: %v\n", wdErr)
			return 1
		}
		absProject = wd
	}
	if ap, apErr := filepath.Abs(absProject); apErr == nil {
		absProject = ap
	}

	// Seed the .sid file so the keeper can find the session immediately (before
	// the first statusLine repaint writes the hook-generated .sid). Non-fatal:
	// the SessionStart hook will overwrite it on first repaint anyway.
	// Refs: hk-yfcc, hk-8prq.
	if result.SessionID != "" {
		seedSID(absProject, name, result.SessionID)
	}

	// Spawn a warn-only keeper for the crew pane in a background tmux session.
	// The keeper runs in "hk-keeper-<name>" so it can be found and killed on
	// crew stop. Non-fatal: a failed keeper spawn is logged but does not fail
	// crew start. Refs: hk-yfcc.
	spawnCrewKeeper(absProject, name)

	fmt.Println(result.SessionID)
	return 0
}

// seedSID writes the crew's session ID to .harmonik/keeper/<name>.sid so the
// keeper can find the session before the first statusLine hook repaint. The
// SessionStart hook overwrites this with the same value on first repaint.
// Non-fatal: errors are logged to stderr but do not propagate. Refs: hk-yfcc.
func seedSID(projectDir, name, sessionID string) {
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if mkErr := os.MkdirAll(keeperDir, 0o755); mkErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew start: seed .sid: mkdir %q: %v\n", keeperDir, mkErr)
		return
	}
	sidPath := filepath.Join(keeperDir, name+".sid")
	//nolint:gosec // G306: .sid is readable by the keeper process (same user)
	if writeErr := os.WriteFile(sidPath, []byte(sessionID+"\n"), 0o644); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew start: seed .sid: write %q: %v\n", sidPath, writeErr)
	}
}

// spawnCrewKeeper launches a warn-only keeper for the crew pane in a detached
// tmux session named "hk-keeper-<name>". The keeper runs
//
//	harmonik keeper --agent <name> --warn-only --project <projectDir>
//
// The session is detached and non-blocking: crew start returns immediately.
// If tmux is not available or the session already exists, the error is logged
// but crew start succeeds. The crew session's tmux target is auto-resolved by
// the keeper from the "harmonik-<hash>-<name>" convention. Refs: hk-yfcc.
func spawnCrewKeeper(projectDir, name string) {
	keeperSession := "hk-keeper-" + name
	// Resolve harmonik binary path: use the same binary that is running now.
	selfBin, selfErr := os.Executable()
	if selfErr != nil {
		selfBin = "harmonik" // fallback: rely on PATH
	}
	// Build the keeper command string for tmux new-session -d.
	keeperCmd := fmt.Sprintf("%s keeper --agent %q --warn-only --project %q",
		selfBin, name, projectDir)
	//nolint:gosec // G204: keeperSession, keeperCmd are internally constructed from validated inputs
	cmd := exec.Command("tmux", "new-session", "-d", "-s", keeperSession, keeperCmd)
	if runErr := cmd.Run(); runErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew start: keeper spawn: tmux new-session -d -s %q: %v (non-fatal)\n",
			keeperSession, runErr)
	}
}

// runCrewStopSubcommand implements `harmonik crew stop <name> [--pause-queue]`.
// subArgs is os.Args[3:].
func runCrewStopSubcommand(subArgs []string) int {
	pauseQueueFlag := false
	socketFlag := ""
	projectFlag := ""
	var positional []string

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			crewStopUsage()
			return 0
		case arg == "--pause-queue":
			pauseQueueFlag = true
		case arg == "--socket" && i+1 < len(subArgs):
			i++
			socketFlag = subArgs[i]
		case strings.HasPrefix(arg, "--socket="):
			socketFlag = strings.TrimPrefix(arg, "--socket=")
		case arg == "--project" && i+1 < len(subArgs):
			i++
			projectFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik crew stop: unknown flag %q\n", arg)
			return 1
		default:
			positional = append(positional, arg)
		}
	}

	if len(positional) != 1 {
		fmt.Fprintf(os.Stderr, "harmonik crew stop: exactly one positional argument <name> is required\n")
		return 1
	}
	name := positional[0]

	sockPath := crewResolveSockPath(socketFlag, projectFlag)
	if sockPath == "" {
		return 1
	}

	payload := map[string]any{
		"name":        name,
		"pause_queue": pauseQueueFlag,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew stop: marshal payload: %v\n", err)
		return 1
	}

	reqBytes, err := json.Marshal(map[string]any{
		"op":      "crew-stop",
		"payload": json.RawMessage(payloadBytes),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew stop: marshal request: %v\n", err)
		return 1
	}

	_, exitCode := crewDialAndSend(sockPath, "crew stop", reqBytes)
	if exitCode != 0 {
		return exitCode
	}

	// Kill the crew's keeper tmux session (best-effort, non-fatal).
	// The .managed marker was removed by the daemon's HandleCrewStop, so the
	// keeper would exit on its next poll anyway — this just speeds it up.
	// Refs: hk-yfcc.
	stopCrewKeeper(name)

	fmt.Printf("crew %s stopped\n", name)
	return 0
}

// stopCrewKeeper kills the detached tmux session "hk-keeper-<name>" that was
// created by spawnCrewKeeper on crew start. Best-effort: if the session does
// not exist or tmux is unavailable the error is logged but crew stop succeeds.
// Refs: hk-yfcc.
func stopCrewKeeper(name string) {
	keeperSession := "hk-keeper-" + name
	//nolint:gosec // G204: keeperSession is internally constructed from validated crew name
	cmd := exec.Command("tmux", "kill-session", "-t", keeperSession)
	if runErr := cmd.Run(); runErr != nil {
		// Exit 1 from tmux kill-session means the session did not exist — not an
		// error worth logging loudly. Any other error IS worth surfacing.
		fmt.Fprintf(os.Stderr, "harmonik crew stop: keeper teardown: tmux kill-session -t %q: %v (non-fatal)\n",
			keeperSession, runErr)
	}
}

// runCrewListSubcommand implements `harmonik crew list [--json] [--project DIR]`.
// Read-only; works with the daemon down.
// subArgs is os.Args[3:].
func runCrewListSubcommand(subArgs []string) int {
	jsonFlag := false
	projectFlag := ""

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			crewListUsage()
			return 0
		case arg == "--json":
			jsonFlag = true
		case arg == "--project" && i+1 < len(subArgs):
			i++
			projectFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik crew list: unknown flag %q\n", arg)
			return 1
		default:
			fmt.Fprintf(os.Stderr, "harmonik crew list: unexpected argument %q\n", arg)
			return 1
		}
	}

	if projectFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik crew list: cannot determine cwd: %v\n", err)
			return 1
		}
		projectFlag = wd
	}
	absProject, err := filepath.Abs(projectFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew list: cannot resolve project path: %v\n", err)
		return 1
	}

	records, err := crew.List(absProject)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew list: %v\n", err)
		return 1
	}

	if len(records) == 0 {
		if !jsonFlag {
			fmt.Fprintln(os.Stderr, "harmonik crew list: no crew members registered")
		}
		return 0
	}

	for _, r := range records {
		if jsonFlag {
			line, marshalErr := json.Marshal(r)
			if marshalErr != nil {
				fmt.Fprintf(os.Stderr, "harmonik crew list: marshal record: %v\n", marshalErr)
				return 1
			}
			fmt.Println(string(line))
		} else {
			ts := r.StartedAt.UTC().Format(time.RFC3339)
			handle := r.Handle
			if handle == "" {
				handle = "(no handle)"
			}
			fmt.Printf("%-20s  queue:%-20s  session:%s  started:%s  handle:%s\n",
				r.Name, r.Queue, r.SessionID, ts, handle)
		}
	}
	return 0
}

// crewResolveSockPath resolves the daemon socket path from flag overrides or cwd.
// Returns "" and prints an error on failure.
func crewResolveSockPath(socketFlag, projectFlag string) string {
	if socketFlag != "" {
		return socketFlag
	}
	projectDir := projectFlag
	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik crew: cannot determine cwd: %v\n", err)
			return ""
		}
		projectDir = wd
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew: cannot resolve project path: %v\n", err)
		return ""
	}
	return filepath.Join(absProject, ".harmonik", "daemon.sock")
}

// crewSocketResponse mirrors the daemon's SocketResponse envelope.
type crewSocketResponse struct {
	Ok     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// crewDialAndSend dials the daemon socket, writes reqBytes, and reads the response.
// verb is used in error messages. Returns the decoded response and exit code.
func crewDialAndSend(sockPath, verb string, reqBytes []byte) (crewSocketResponse, int) {
	dialCtx, cancelDial := context.WithTimeout(context.Background(), 5*time.Second)
	conn, dialErr := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	cancelDial()
	if dialErr != nil {
		if commsIsSocketAbsent(dialErr) || commsIsConnRefused(dialErr) {
			fmt.Fprintf(os.Stderr, "harmonik %s: daemon not running (socket %s missing or refused)\n", verb, sockPath)
			return crewSocketResponse{}, 17
		}
		fmt.Fprintf(os.Stderr, "harmonik %s: dial %s: %v\n", verb, sockPath, dialErr)
		return crewSocketResponse{}, 1
	}
	defer func() { _ = conn.Close() }()

	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik %s: write request: %v\n", verb, writeErr)
		return crewSocketResponse{}, 1
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite()
	}

	var resp crewSocketResponse
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik %s: decode response: %v\n", verb, decErr)
		return crewSocketResponse{}, 1
	}

	if !resp.Ok {
		fmt.Fprintf(os.Stderr, "harmonik %s: %s\n", verb, resp.Error)
		return crewSocketResponse{}, 1
	}

	return resp, 0
}

func crewUsage() {
	fmt.Print(`harmonik crew — captain & crew session management (C2)

USAGE
  harmonik crew <verb> [flags]

VERBS
  start   Launch a persistent crew session and bind it to a named queue (daemon required)
  stop    Stop a crew session and clean up its registry record (daemon required)
  list    List registered crew members (read-only; works daemon-down)

EXIT CODES
  0   Success
  1   Argument error or op rejected
  2   Unrecognised verb
  17  Daemon not running (start/stop)

EXAMPLES
  harmonik crew start alpha --queue alpha-q --mission /tmp/alpha-handoff.md
  harmonik crew stop alpha
  harmonik crew stop alpha --pause-queue
  harmonik crew list
  harmonik crew list --json
`)
}

func crewStartUsage() {
	fmt.Print(`harmonik crew start — launch a persistent crew session

USAGE
  harmonik crew start <name> --queue <q> --mission <handoff-path> [--socket PATH] [--project DIR]

Sends a crew-start op to the daemon. The daemon mints a session_id, writes the
crew registry record at .harmonik/crew/<name>.json, ensures the named queue exists,
launches an interactive claude --remote-control session, pastes the mission seed,
and sets up keeper-attach inputs. The minted session_id is printed to stdout.

ARGS
  <name>            Crew member name (charset [a-z0-9-], 1–64 chars). Required.

FLAGS
  --queue <q>       Named queue the crew is bound to. Required.
  --mission <path>  Path to the mission handoff file. Required.
  --socket PATH     Override socket path (default: <project>/.harmonik/daemon.sock).
  --project DIR     Project directory (default: cwd).

EXIT CODES
  0   Success (session_id printed to stdout)
  1   Argument error or daemon rejected the op
  17  Daemon not running (socket missing or ECONNREFUSED)

EXAMPLES
  harmonik crew start alpha --queue alpha-q --mission /tmp/alpha-handoff.md
  harmonik crew start beta  --queue beta-q  --mission /tmp/beta-handoff.md
`)
}

func crewStopUsage() {
	fmt.Print(`harmonik crew stop — stop a crew session

USAGE
  harmonik crew stop <name> [--pause-queue] [--socket PATH] [--project DIR]

Sends a crew-stop op to the daemon. The daemon stops the session pane, removes
the registry record, and removes the keeper .managed marker.

NOTE: teardown is synchronous for the registry record and tmux window, but the
underlying 'claude --remote-control' process may take ~10s to fully exit
(graceful shutdown). This is not a leak — the process will exit on its own.

ARGS
  <name>          Crew member name. Required.

FLAGS
  --pause-queue   Halt dispatch on the crew's named queue after teardown (sets
                  workers to 0). Default: leave the queue as-is.
  --socket PATH   Override socket path (default: <project>/.harmonik/daemon.sock).
  --project DIR   Project directory (default: cwd).

EXIT CODES
  0   Success
  1   Argument error or daemon rejected the op
  17  Daemon not running (socket missing or ECONNREFUSED)

EXAMPLES
  harmonik crew stop alpha
  harmonik crew stop alpha --pause-queue
`)
}

func crewListUsage() {
	fmt.Print(`harmonik crew list — list registered crew members

USAGE
  harmonik crew list [--json] [--project DIR]

Reads .harmonik/crew/*.json directly. No daemon connection required.
Records are sorted by name. An absent .harmonik/crew/ directory returns an empty list.

FLAGS
  --json          Emit one JSON object per record (NDJSON — one object per line,
                  not a JSON array). Includes all fields.
                  Pipe to 'jq -s' to collect into an array, or process line-by-line:
                    harmonik crew list --json | jq -s '.'
                    harmonik crew list --json | while IFS= read -r line; do ...; done
  --project DIR   Project directory (default: cwd).

EXIT CODES
  0   Success (zero or more records listed)
  1   Argument error or read failure

EXAMPLES
  harmonik crew list
  harmonik crew list --json
  harmonik crew list --json | jq -s '.'
  harmonik crew list --project /path/to/project
`)
}
