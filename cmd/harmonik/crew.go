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
//	                    May also be supplied via --name <name>.
//	--queue <q>         Named queue the crew is bound to. Default: "<name>-q".
//	--mission <path>    Path to the mission handoff file. OPTIONAL. On a FRESH
//	                    start this is the only source of the mission; the on-disk
//	                    default (.harmonik/crew/missions/<name>.md) is never
//	                    auto-read here (D3, hk-sn4n). Keeper-restart re-reads disk.
//	--harness <type>    Crew orchestrator harness override (e.g. "codex"). OPTIONAL.
//	                    Highest-precedence tier of the crew-scoped harness resolver
//	                    (hk-l63b9); "claude" (today's only supported substrate) is
//	                    the default when this and the mission harness: front-matter
//	                    field are both absent.
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
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
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

// crewStartArgs holds the resolved, post-defaulting inputs to a crew-start op.
//
// It is the output of resolveCrewStartArgs — the pure arg/defaulting layer that
// ES4 (hk-sn4n) made unit-testable. The RPC wiring in runCrewStartSubcommand
// consumes these fields; the helper itself touches no daemon, no network, and no
// disk, so the defaulting + mission-split logic can be table-tested daemon-down.
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

// crewReapPriorWatchers is the hk-6629b launch-path reap hook (see
// watcherreap.go). A package var, not a runCrewStartCoreWith parameter, so
// the many existing call sites/tests of that function are unaffected; tests
// that care override this var directly and restore it via t.Cleanup.
var crewReapPriorWatchers reapPriorAgentWatchersFn = reapPriorAgentWatchers

// resolveCrewStartArgs parses `crew start` / `start crew` argv, applies the ES4
// defaults, and enforces the mission-split rule. It returns the resolved args,
// a help-requested flag, and a usage-error message ("" on success).
//
// Defaults (hk-sn4n / PLAN §4):
//   - --queue defaults to "<name>-q" when not supplied (one named queue per crew).
//   - --mission is OPTIONAL on a fresh start.
//
// Mission-split rule (D3, review outcome D — the load-bearing invariant):
//
//	A FRESH `crew start` reads ONLY the --mission flag. When --mission is given,
//	that file is the mission. When it is absent, MissionPath stays "" and the crew
//	starts WITHOUT a mission (to be commissioned later over comms). The on-disk
//	default mission .harmonik/crew/missions/<name>.md is NEVER consulted here.
//
//	This makes stale-reuse impossible BY CONSTRUCTION: this function never reads
//	disk and never synthesises the default path, so a prior agent's leftover
//	mission file simply cannot become the value sent over the RPC. The daemon
//	(HandleCrewStart) only ever pastes the path it is handed — it does not read
//	the on-disk default either — so an empty MissionPath yields a crew that boots
//	with no auto-loaded mission rather than the stale one.
//
//	The KEEPER-RESTART re-hydration path is a DIFFERENT code path and is
//	deliberately untouched: a keeper cycles a crew via `/clear` + `/session-resume`
//	on the SAME session_id (internal/keeper), NOT via this `crew start` RPC. On
//	that resume the crew re-runs its own boot sequence and re-reads its OWN
//	just-written .harmonik/crew/missions/<name>.md (crew-launch § Self-restart).
//	Because restart never flows through resolveCrewStartArgs, the "fresh start
//	ignores disk" rule cannot regress restart's "re-read disk" behaviour.
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

	// Default --queue to "<name>-q" (one named queue per crew). hk-sn4n.
	if args.Queue == "" {
		args.Queue = args.Name + "-q"
	}

	// NOTE (D3): no --mission default. MissionPath stays "" when the flag is
	// absent — we deliberately do NOT fall back to the on-disk default mission
	// path, which is what prevents booting on a prior agent's stale mission.

	return args, false, ""
}

// crewBriefSeedFn is the injectable seam for pasting the agent-brief boot seed
// to the crew's agent pane after a successful crew-start RPC (T10/hk-ncg9m).
// Production passes pasteCrewBriefSeedViaTmux; tests inject a capturing stub.
type crewBriefSeedFn func(project, name, sessionID string)

// crewBriefSeedDelay mirrors captainSplashDismissDelay in captain.go: the
// settle period between the splash-dismiss Enter and the boot-seed paste, and
// between the paste and the submit Enter (T10/hk-ncg9m).
const crewBriefSeedDelay = 750 * time.Millisecond

// pasteCrewBriefSeedViaTmux is the production crewBriefSeedFn. It derives the
// crew's tmux session name (harmonik-<project-hash>-crew-<name>) and pastes
// "Please run `harmonik agent brief` and begin your operating loop." to the
// crew's agent pane, mirroring captain.go PasteSeedToAgentPane (T10/hk-ncg9m).
// Best-effort: all errors are logged to stderr but never returned.
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
	bufName := fmt.Sprintf("harmonik-%s-crew-boot", sessionID)
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

// runCrewStartSubcommand implements `harmonik crew start <name> [--queue <q>] [--mission <path>]`.
//
// Defaults --queue to "<name>-q" and treats --mission as optional, never reusing
// the on-disk default mission — see resolveCrewStartArgs for the mission-split
// rule (hk-sn4n / D3). subArgs is os.Args[3:].
func runCrewStartSubcommand(subArgs []string) int {
	return runCrewStartCore(subArgs, runKeeperEnable)
}

// runCrewStartCore is the testable inner function for crew start. enableKeeper
// is injected so tests can verify keeper wiring without touching the real
// ~/.claude/settings.json. Production callers pass runKeeperEnable.
// Bead ref: hk-xxcv9.
func runCrewStartCore(subArgs []string, enableKeeper keeperEnableFn) int {
	return runCrewStartCoreWith(subArgs, enableKeeper, pasteCrewBriefSeedViaTmux)
}

// runCrewStartCoreWith is runCrewStartCore with an injectable brief-seed seam
// (T10/hk-ncg9m). briefSeed is called after a successful crew-start RPC to paste
// the agent-brief boot seed to the crew's agent pane. nil skips the paste (tests
// that do not want tmux side-effects pass nil or a capturing stub).
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

	// Resolve project dir before the RPC so boot assets can be provisioned first.
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

	// hk-6629b: reap any prior `comms recv --agent <name> --follow` /
	// `subscribe --to <name> --follow` watcher process for this crew name,
	// REGARDLESS of liveness — a crew relaunched (e.g. after a keeper restart
	// or a re-`crew start` for the same name) must never leave its
	// predecessor's watcher holding a daemon subscribe slot. See
	// captainReapPriorWatchers in captain.go for the mirrored captain-side call.
	// Scoped to absProject: crew names repeat across projects sharing a box, so
	// an unscoped reap would kill a peer project's same-named watcher.
	crewReapPriorWatchers(name, absProject)

	// Provision boot assets (skills, scaffolds, context tiers, AGENTS.md router)
	// before the daemon spawns the crew so a foreign project (never run harmonik
	// init) has the files the crew agent reads at boot. (hk-2nmbq)
	ensureBootAssets(absProject, os.Stdout, os.Stderr)

	// Wire keeper hooks BEFORE sending the RPC so the new crew session reads the
	// statusLine + Stop + PreCompact + SessionStart stanzas at session start.
	// Mirrors the captain path (runCaptainLaunchWithOps). Non-fatal: a failure
	// WARNS but does not block the crew start. Bead: hk-xxcv9.
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

	// Seed the .sid file so the keeper can find the session immediately (before
	// the first statusLine repaint writes the hook-generated .sid). Non-fatal:
	// the SessionStart hook will overwrite it on first repaint anyway.
	// Refs: hk-yfcc, hk-8prq.
	if result.SessionID != "" {
		seedSID(absProject, name, result.SessionID)
	}

	// Paste the agent-brief boot seed to the crew's agent pane so the crew runs
	// `harmonik agent brief` as its first action (T10/hk-ncg9m — symmetric seed
	// paste mirroring captain.go PasteSeedToAgentPane). Best-effort: never blocks.
	if briefSeed != nil && result.SessionID != "" {
		briefSeed(absProject, name, result.SessionID)
	}

	// The crew's keeper is launched by the DAEMON as a sibling `keeper` window
	// inside the crew session (HandleCrewStart → SpawnCrewSession, hk-rmy1),
	// targeting `agent` window. Full force-cut band (D4, hk-lcga). The CLI does
	// not spawn a separate keeper session. Refs: hk-rmy1 / hk-lcga / hk-tt9q.

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

// buildCrewKeeperConfig assembles the enableConfig used to wire keeper hooks
// for a freshly-started crew member. Mirrors buildCaptainKeeperConfig in
// captain.go. No --yes-destructive gate is needed: the daemon creates the
// .managed marker in HandleCrewStart (createCrewManagedMarker). Bead: hk-xxcv9.
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

	// The daemon's HandleCrewStop tears down the whole crew session
	// (StopCrewSession → KillSession), which kills BOTH the `agent` and `keeper`
	// windows — so the keeper process dies with the session. No separate
	// hk-keeper-<name> teardown is needed any more. Refs: hk-rmy1 / hk-yfcc.

	fmt.Printf("crew %s stopped\n", name)
	return 0
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
  --harness <type>  Crew orchestrator harness override (e.g. "codex"). OPTIONAL.
                    Highest-precedence tier of the crew-scoped harness resolver
                    (flag > mission harness: front-matter > per-crew config >
                    default "claude"). A harness whose substrate isn't wired
                    yet is rejected with an explicit error — no silent
                    fallback to claude.
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
  harmonik crew start gamma --harness codex                  # crew harness override
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
