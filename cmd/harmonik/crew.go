package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

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

type crewStartArgs struct {
	// Name is the crew member identifier (sole positional).
	Name string
	// Queue is the named queue, defaulted to "<name>-q" when --queue was absent.
	Queue string
	// MissionPath is the FRESH-START mission source. It is EXACTLY the --mission
	// value the invoker supplied, or "" when none was given. It is NEVER defaulted
	// to the on-disk default mission (.harmonik/crew/missions/<name>.md) — see the
	// mission-split rule below (D3).
	MissionPath string
	// Harness is the --harness override value, or "" when the flag was absent.
	// Highest-precedence tier of the crew-scoped harness resolver (hk-l63b9).
	Harness string
	// SocketFlag / ProjectFlag are passed through to socket/project resolution.
	SocketFlag  string
	ProjectFlag string
}

var crewReapPriorWatchers reapPriorAgentWatchersFn = reapPriorAgentWatchers

func resolveCrewStartArgs(subArgs []string) (args crewStartArgs, help bool, usageErr string) {
	var positional []string

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			return crewStartArgs{}, true, ""
		case arg == "--name" && i+1 < len(subArgs):
			i++
			positional = append(positional, subArgs[i])
		case strings.HasPrefix(arg, "--name="):
			positional = append(positional, strings.TrimPrefix(arg, "--name="))
		case arg == "--queue" && i+1 < len(subArgs):
			i++
			args.Queue = subArgs[i]
		case strings.HasPrefix(arg, "--queue="):
			args.Queue = strings.TrimPrefix(arg, "--queue=")
		case arg == "--mission" && i+1 < len(subArgs):
			i++
			args.MissionPath = subArgs[i]
		case strings.HasPrefix(arg, "--mission="):
			args.MissionPath = strings.TrimPrefix(arg, "--mission=")
		case arg == "--harness" && i+1 < len(subArgs):
			i++
			args.Harness = subArgs[i]
		case strings.HasPrefix(arg, "--harness="):
			args.Harness = strings.TrimPrefix(arg, "--harness=")
		case arg == "--socket" && i+1 < len(subArgs):
			i++
			args.SocketFlag = subArgs[i]
		case strings.HasPrefix(arg, "--socket="):
			args.SocketFlag = strings.TrimPrefix(arg, "--socket=")
		case arg == "--project" && i+1 < len(subArgs):
			i++
			args.ProjectFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			args.ProjectFlag = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "-"):
			return crewStartArgs{}, false, fmt.Sprintf("harmonik crew start: unknown flag %q", arg)
		default:
			positional = append(positional, arg)
		}
	}

	if len(positional) != 1 {
		return crewStartArgs{}, false, "harmonik crew start: exactly one crew name is required (positional <name> or --name <name>)"
	}
	args.Name = positional[0]

	if args.Queue == "" {
		args.Queue = args.Name + "-q"
	}

	return args, false, ""
}

type crewBriefSeedFn func(project, name, sessionID string)

const crewBriefSeedDelay = 750 * time.Millisecond

func crewBootBufferName(sessionID string) string {
	return ltmux.BufferName(sessionID, "crew-boot")
}

func pasteCrewBriefSeedViaTmux(project, name, sessionID string) {
	realDir, err := filepath.EvalSymlinks(project)
	if err != nil {
		realDir = project
	}
	hash := lifecycle.ComputeProjectHash(realDir)
	sessName := lifecycle.TmuxSessionName(hash, "crew-"+name)
	paneTarget := sessName + ":" + ltmux.WindowAgent
	adapter := ltmux.OSAdapter{}
	ctx := context.Background()
	if err := adapter.SendKeysEnter(ctx, paneTarget); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew start: boot-seed splash dismiss: %v\n", err)
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(crewBriefSeedDelay):
	}
	bufName := crewBootBufferName(sessionID)
	const bootSeedMsg = "Please run `harmonik agent brief` and begin your operating loop.\n"
	if err := adapter.WriteToPane(ctx, bufName, paneTarget, []byte(bootSeedMsg)); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew start: boot-seed paste: %v\n", err)
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(crewBriefSeedDelay):
	}
	if err := adapter.SendKeysEnter(ctx, paneTarget); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew start: boot-seed submit: %v\n", err)
	}
}

func runCrewStartSubcommand(subArgs []string) int {
	return runCrewStartCore(subArgs, runKeeperEnable)
}

func runCrewStartCore(subArgs []string, enableKeeper keeperEnableFn) int {
	return runCrewStartCoreWith(subArgs, enableKeeper, pasteCrewBriefSeedViaTmux)
}

func runCrewStartCoreWith(subArgs []string, enableKeeper keeperEnableFn, briefSeed crewBriefSeedFn) int {
	args, help, usageErr := resolveCrewStartArgs(subArgs)
	if help {
		crewStartUsage()
		return 0
	}
	if usageErr != "" {
		fmt.Fprintln(os.Stderr, usageErr)
		return 1
	}

	name := args.Name

	sockPath := crewResolveSockPath(args.SocketFlag, args.ProjectFlag)
	if sockPath == "" {
		return 1
	}

	absProject := args.ProjectFlag
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

	crewReapPriorWatchers(name)

	if err := ensureBootAssets(absProject, os.Stdout, os.Stderr); err != nil {
		return 1
	}

	if keeperCfg, cerr := buildCrewKeeperConfig(name, absProject); cerr != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew start: build keeper config: %v\n", cerr)
	} else if rc := enableKeeper(keeperCfg, os.Stdout, os.Stderr); rc != 0 {
		fmt.Fprintf(os.Stderr, "harmonik crew start: keeper enable returned %d — continuing; "+
			"run `harmonik keeper enable --agent %s` manually to wire keeper hooks\n", rc, name)
	}

	payload := map[string]any{
		"name":         name,
		"queue":        args.Queue,
		"mission_path": args.MissionPath,
		"harness":      args.Harness,
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

	if result.SessionID != "" {
		seedSID(absProject, name, result.SessionID)
	}

	if briefSeed != nil && result.SessionID != "" {
		briefSeed(absProject, name, result.SessionID)
	}

	fmt.Println(result.SessionID)
	return 0
}

func seedSID(projectDir, name, sessionID string) {
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if mkErr := os.MkdirAll(keeperDir, core.HarmonikDirMode); mkErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew start: seed .sid: mkdir %q: %v\n", keeperDir, mkErr)
		return
	}
	sidPath := filepath.Join(keeperDir, name+".sid")
	if writeErr := os.WriteFile(sidPath, []byte(sessionID+"\n"), 0o600); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik crew start: seed .sid: write %q: %v\n", sidPath, writeErr)
	}
}

func buildCrewKeeperConfig(name, projectDir string) (enableConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return enableConfig{}, fmt.Errorf("cannot determine home directory: %w", err)
	}
	return enableConfig{
		agentName:    name,
		projectDir:   projectDir,
		scriptsDir:   autoDetectScriptsDir(projectDir),
		settingsPath: filepath.Join(home, ".claude", "settings.json"),
	}, nil
}

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

	fmt.Printf("crew %s stopped\n", name)
	return 0
}

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

type crewSocketResponse struct {
	Ok     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

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
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik %s: close connection: %v\n", verb, closeErr)
		}
	}()

	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik %s: write request: %v\n", verb, writeErr)
		return crewSocketResponse{}, 1
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		if closeWriteErr := uw.CloseWrite(); !isBenignCloseWrite(closeWriteErr) {
			if _, err := fmt.Fprintf(os.Stderr, "harmonik %s: close request write side: %v\n", verb, closeWriteErr); err != nil {
				return crewSocketResponse{}, 1
			}
			return crewSocketResponse{}, 1
		}
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
  harmonik crew start <name> [--queue <q>] [--mission <handoff-path>] [--harness <type>] [--socket PATH] [--project DIR]

Sends a crew-start op to the daemon. The daemon mints a session_id, writes the
crew registry record at .harmonik/crew/<name>.json, ensures the named queue exists,
launches an interactive claude --remote-control session, pastes the mission seed,
and sets up keeper-attach inputs. The minted session_id is printed to stdout.

ARGS
  <name>            Crew member name (charset [a-z0-9-], 1–64 chars). Required.
                    May also be supplied as --name <name>.

FLAGS
  --queue <q>       Named queue the crew is bound to.
                    Default: "<name>-q" (one named queue per crew).
  --mission <path>  Path to the mission handoff file. OPTIONAL.
                    FRESH-START rule (D3): this flag is the ONLY source of the
                    mission. The on-disk default mission
                    (.harmonik/crew/missions/<name>.md) is NEVER auto-read on a
                    fresh start — that prevents a crew booting on a prior agent's
                    stale mission. With no --mission the crew starts WITHOUT a
                    mission (commission it later over comms). A keeper RESTART is
                    a separate path and DOES re-read the on-disk mission.
  --harness <type>  Crew orchestrator harness override. OPTIONAL, and today the
                    only value that starts a crew is "claude". Leave it unset.
                    Any other value is a harness whose crew-orchestrator
                    substrate is not wired yet: the daemon refuses it with "not
                    yet supported" and this command exits non-zero — there is no
                    silent fallback to claude. Codex and Pi still implement at
                    the BEAD level, which is a different mechanism: label the
                    bead harness:codex ("br label add <id> -l harness:codex").
                    Highest-precedence tier of the crew-scoped harness resolver
                    (flag > mission harness: front-matter > per-crew config >
                    default "claude") for the day that substrate lands.
  --socket PATH     Override socket path (default: <project>/.harmonik/daemon.sock).
  --project DIR     Project directory (default: cwd).

EXIT CODES
  0   Success (session_id printed to stdout)
  1   Argument error or daemon rejected the op
  17  Daemon not running (socket missing or ECONNREFUSED)

EXAMPLES
  harmonik crew start alpha                                  # queue defaults to alpha-q, no mission
  harmonik crew start alpha --mission /tmp/alpha-handoff.md  # queue defaults to alpha-q
  harmonik crew start beta  --queue beta-q  --mission /tmp/beta-handoff.md
  br label add hk-abc12 -l harness:codex                     # put a BEAD on codex; crews stay on claude
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
