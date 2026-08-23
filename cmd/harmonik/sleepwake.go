package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/gregberns/harmonik/internal/crew"
)

type sleepWakeSocketResponse struct {
	Ok    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func runSleepSubcommand(ctx context.Context, subArgs []string) int {
	var force bool
	var projectDir string

	for i := 0; i < len(subArgs); {
		arg := subArgs[i]
		switch {
		case arg == "--force":
			force = true
			i++
		case arg == "--project" && i+1 < len(subArgs):
			projectDir = subArgs[i+1]
			i += 2
		case strings.HasPrefix(arg, "--project="):
			projectDir = strings.TrimPrefix(arg, "--project=")
			i++
		case arg == "--help" || arg == "-h":
			if err := sleepUsage(); err != nil {
				return 1
			}
			return 0
		default:
			fmt.Fprintf(os.Stderr, "harmonik sleep: unrecognized argument %q\n", arg)
			return 1
		}
	}

	sockPath, code := resolveSleepWakeSock(projectDir, "sleep")
	if code != 0 {
		return code
	}

	payload, marshalErr := json.Marshal(struct {
		Force bool `json:"force"`
	}{Force: force})
	if marshalErr != nil {
		return 2
	}
	reqBody, marshalErr := json.Marshal(struct {
		Op      string          `json:"op"`
		Payload json.RawMessage `json:"payload"`
	}{Op: "daemon-sleep", Payload: payload})
	if marshalErr != nil {
		return 2
	}

	resp, earlyExit := sendSleepWakeRequest(ctx, sockPath, reqBody, "sleep")
	if earlyExit != 0 {
		return earlyExit
	}

	if !resp.Ok {
		fmt.Fprintf(os.Stderr, "harmonik sleep: %s\n", resp.Error)
		return 2
	}
	if force {
		if _, err := fmt.Fprintln(os.Stdout, "sleep: fleet parked (forced)"); err != nil {
			return 1
		}
	} else {
		if _, err := fmt.Fprintln(os.Stdout, "sleep: fleet parked"); err != nil {
			return 1
		}
	}
	return 0
}

func runWakeSubcommand(ctx context.Context, subArgs []string) int {
	var agentName string
	var wakeAll bool
	var projectDir string

	for i := 0; i < len(subArgs); {
		arg := subArgs[i]
		switch {
		case arg == "--all":
			wakeAll = true
			i++
		case arg == "--agent" && i+1 < len(subArgs):
			agentName = subArgs[i+1]
			i += 2
		case strings.HasPrefix(arg, "--agent="):
			agentName = strings.TrimPrefix(arg, "--agent=")
			i++
		case arg == "--project" && i+1 < len(subArgs):
			projectDir = subArgs[i+1]
			i += 2
		case strings.HasPrefix(arg, "--project="):
			projectDir = strings.TrimPrefix(arg, "--project=")
			i++
		case arg == "--help" || arg == "-h":
			if err := wakeUsage(); err != nil {
				return 1
			}
			return 0
		default:
			fmt.Fprintf(os.Stderr, "harmonik wake: unrecognized argument %q\n", arg)
			return 1
		}
	}

	if !wakeAll && agentName == "" {
		if _, err := fmt.Fprintln(os.Stderr, "harmonik wake: provide --agent <name> or --all"); err != nil {
			return 1
		}
		if err := wakeUsage(); err != nil {
			return 1
		}
		return 1
	}
	if wakeAll && agentName != "" {
		fmt.Fprintln(os.Stderr, "harmonik wake: --agent and --all are mutually exclusive")
		return 1
	}

	absProject, code := resolveSleepWakeProject(projectDir, "wake")
	if code != 0 {
		return code
	}
	if agentName != "" {
		if code := checkWakeTarget(absProject, agentName); code != 0 {
			return code
		}
	}
	sockPath := filepath.Join(absProject, ".harmonik", "daemon.sock")

	payload, marshalErr := json.Marshal(struct {
		Agent string `json:"agent,omitempty"`
		All   bool   `json:"all"`
	}{Agent: agentName, All: wakeAll})
	if marshalErr != nil {
		return 2
	}
	reqBody, marshalErr := json.Marshal(struct {
		Op      string          `json:"op"`
		Payload json.RawMessage `json:"payload"`
	}{Op: "daemon-wake", Payload: payload})
	if marshalErr != nil {
		return 2
	}

	resp, earlyExit := sendSleepWakeRequest(ctx, sockPath, reqBody, "wake")
	if earlyExit != 0 {
		return earlyExit
	}

	if !resp.Ok {
		fmt.Fprintf(os.Stderr, "harmonik wake: %s\n", resp.Error)
		return 2
	}
	if wakeAll {
		if _, err := fmt.Fprintln(os.Stdout, "wake: the daemon accepted a wake request for every sleeping session"); err != nil {
			return 1
		}
	} else {
		if _, err := fmt.Fprintf(os.Stdout, "wake: the daemon accepted a wake request for %q. It nudges that session only if the session sleeps.\n", agentName); err != nil {
			return 1
		}
	}
	return 0
}

var wakeSessionNameRe = regexp.MustCompile(`^[a-z0-9-]+$`)

const wakeSessionNameMaxLen = 64

var wakeBuiltinTargets = []string{"captain", "watch"}

func checkWakeTarget(absProject, agentName string) int {
	if len(agentName) > wakeSessionNameMaxLen || !wakeSessionNameRe.MatchString(agentName) {
		fmt.Fprintf(os.Stderr, "harmonik wake: %q cannot name a session. A session name is 1 to %d characters of lowercase letters, digits and hyphens. Nothing was nudged.\n", agentName, wakeSessionNameMaxLen)
		return 1
	}
	known := knownWakeTargets(absProject)
	for _, name := range known {
		if name == agentName {
			return 0
		}
	}
	fmt.Fprintf(os.Stderr, "harmonik wake: no session named %q in %s. Nothing was nudged.\n", agentName, absProject)
	fmt.Fprintf(os.Stderr, "harmonik wake: this project knows these session names: %s.\n", strings.Join(known, ", "))
	fmt.Fprintf(os.Stderr, "harmonik wake: run `harmonik wake --all` to wake every sleeping session.\n")
	return 1
}

func knownWakeTargets(absProject string) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, name := range wakeBuiltinTargets {
		add(name)
	}
	records, err := crew.List(absProject)
	if err == nil {
		for _, r := range records {
			add(r.Name)
		}
	}
	for _, id := range sleepingSessionIDs(absProject) {
		add(id)
	}
	sort.Strings(out)
	return out
}

func sleepingSessionIDs(absProject string) []string {
	const markerPrefix = ".sleeping."
	entries, err := os.ReadDir(filepath.Join(absProject, ".harmonik"))
	if err != nil {
		return nil
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		id := strings.TrimPrefix(e.Name(), markerPrefix)
		if id != e.Name() && id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func resolveSleepWakeSock(projectDir, verb string) (sockPath string, exitCode int) {
	abs, code := resolveSleepWakeProject(projectDir, verb)
	if code != 0 {
		return "", code
	}
	return filepath.Join(abs, ".harmonik", "daemon.sock"), 0
}

func resolveSleepWakeProject(projectDir, verb string) (absProject string, exitCode int) {
	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik %s: cannot determine working directory: %v\n", verb, err)
			return "", 1
		}
		projectDir = wd
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik %s: cannot resolve project path %q: %v\n", verb, projectDir, err)
		return "", 1
	}
	return abs, 0
}

func sendSleepWakeRequest(ctx context.Context, sockPath string, payload []byte, verb string) (resp sleepWakeSocketResponse, exitCode int) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
	if err != nil {
		if isSleepWakeSocketAbsent(err) || isSleepWakeConnRefused(err) {
			fmt.Fprintf(os.Stderr, "harmonik %s: daemon not running (socket %s missing or refused)\n", verb, sockPath)
			return sleepWakeSocketResponse{}, 17
		}
		fmt.Fprintf(os.Stderr, "harmonik %s: dial %s: %v\n", verb, sockPath, err)
		return sleepWakeSocketResponse{}, 2
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck

	if _, writeErr := conn.Write(payload); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik %s: write: %v\n", verb, writeErr)
		return sleepWakeSocketResponse{}, 2
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		if closeErr := uw.CloseWrite(); !isBenignCloseWrite(closeErr) {
			fmt.Fprintf(os.Stderr, "harmonik %s: close write: %v\n", verb, closeErr)
			return sleepWakeSocketResponse{}, 2
		}
	}

	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik %s: decode response: %v\n", verb, decErr)
		return sleepWakeSocketResponse{}, 2
	}
	return resp, 0
}

func isSleepWakeSocketAbsent(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var pathErr *os.PathError
		if errors.As(opErr.Err, &pathErr) {
			return errors.Is(pathErr.Err, syscall.ENOENT)
		}
		return errors.Is(opErr.Err, syscall.ENOENT)
	}
	return errors.Is(err, syscall.ENOENT)
}

func isSleepWakeConnRefused(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var sysErr *os.SyscallError
		if errors.As(opErr.Err, &sysErr) {
			return errors.Is(sysErr.Err, syscall.ECONNREFUSED)
		}
		return errors.Is(opErr.Err, syscall.ECONNREFUSED)
	}
	return errors.Is(err, syscall.ECONNREFUSED)
}

func runSleepGateSubcommand(subArgs []string) int {
	var projectDir string
	for i := 0; i < len(subArgs); {
		arg := subArgs[i]
		switch {
		case arg == "--project" && i+1 < len(subArgs):
			projectDir = subArgs[i+1]
			i += 2
		case strings.HasPrefix(arg, "--project="):
			projectDir = strings.TrimPrefix(arg, "--project=")
			i++
		case arg == "--help" || arg == "-h":
			if err := sleepGateUsage(); err != nil {
				return 2
			}
			return 0
		default:
			fmt.Fprintf(os.Stderr, "harmonik sleep-gate: unrecognized argument %q\n", arg)
			return 2
		}
	}
	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik sleep-gate: cannot determine working directory: %v\n", err)
			return 2
		}
		projectDir = wd
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik sleep-gate: cannot resolve project path %q: %v\n", projectDir, err)
		return 2
	}
	markerPath := filepath.Join(abs, ".harmonik", ".fleet-sleeping")
	if _, statErr := os.Stat(markerPath); statErr == nil {
		return 0 // fleet is sleeping
	}
	return 1 // fleet is awake
}

func sleepGateUsage() error {
	_, err := os.Stdout.WriteString(`harmonik sleep-gate — check whether the fleet is sleeping (for harness cron gates)

USAGE
  harmonik sleep-gate [--project DIR]

FLAGS
  --project DIR  project directory (default: cwd)

EXIT CODES
  0   fleet is sleeping — cron or timer should suppress / exit early
  1   fleet is awake — proceed normally
  2   argument error

NOTES
  No daemon connection required: checks .harmonik/.fleet-sleeping on disk.
  Written by 'harmonik sleep', removed by 'harmonik wake --all'.

  Add this one-liner at the top of Claude Code harness cron prompts to prevent
  them from firing while the fleet is parked:

    harmonik sleep-gate --project $HARMONIK_PROJECT && exit 0

EXAMPLES
  harmonik sleep-gate
  harmonik sleep-gate --project /path/to/project
`)
	return err
}

func sleepUsage() error {
	_, err := os.Stdout.WriteString(`harmonik sleep — park all LLM sessions now (manual quiesce override)

USAGE
  harmonik sleep [--force] [--project DIR]

FLAGS
  --force       bypass the drain gate; park sessions even if work remains
  --project DIR project directory (default: cwd)

NOTES
  Without --force the daemon consults GenuineDrain first.  If the fleet
  has pending or in-progress work the request is rejected with an error.
  --force is the human escape hatch for operator-initiated maintenance.

EXIT CODES
  0   fleet parked
  1   argument error
  2   daemon rejected the request or protocol error
  17  daemon not running

EXAMPLES
  harmonik sleep
  harmonik sleep --force
  harmonik sleep --project /path/to/project
`)
	return err
}

func wakeUsage() error {
	_, err := os.Stdout.WriteString(`harmonik wake — wake sleeping LLM sessions (manual quiesce override)

USAGE
  harmonik wake (--agent <name> | --all) [--project DIR]

FLAGS
  --agent NAME  wake the named session (e.g. "captain", "crew-investigate")
  --all         wake every sleeping session
  --project DIR project directory (default: cwd)

NOTES
  Exactly one of --agent or --all is required.
  A --agent name this project has no session for is refused with exit 1,
  and the refusal lists the names it does know.
  A session that is not currently sleeping is skipped, so exit 0 means the
  daemon accepted the request, not that a pane moved.
  This is the fleet-stall human escape hatch: if the automatic wake
  triggers missed a session, use harmonik wake --all to recover.

EXIT CODES
  0   the daemon accepted the wake request
  1   argument error, or no session by that name
  2   daemon rejected the request or protocol error
  17  daemon not running

EXAMPLES
  harmonik wake --all
  harmonik wake --agent captain
  harmonik wake --agent crew-investigate --project /path/to/project
`)
	return err
}
