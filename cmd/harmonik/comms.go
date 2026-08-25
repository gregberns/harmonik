package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/presence"
)

func runCommsSubcommand(subArgs []string) int {
	verb := ""
	if len(subArgs) > 0 {
		verb = subArgs[0]
	}

	switch verb {
	case "", "--help", "-h":
		commsUsage()
		return 0
	case "send":
		return runCommsSendSubcommand(subArgs[1:])
	case "log":
		return runCommsLogSubcommand(subArgs[1:])
	case "join":
		return runCommsPresenceSubcommand(subArgs[1:], "join")
	case "leave":
		return runCommsPresenceSubcommand(subArgs[1:], "leave")
	case "who":
		return runCommsWhoSubcommand(subArgs[1:])
	case "recv":
		return runCommsRecvSubcommand(subArgs[1:])
	default:
		fmt.Fprintf(os.Stderr, "harmonik comms: unrecognised verb %q; verbs are: send, log, join, leave, who, recv\n", verb)
		return 2
	}
}

func runCommsSendSubcommand(subArgs []string) int {
	toFlag := ""
	broadcastFlag := false
	fromFlag := ""
	topicFlag := ""
	replyToFlag := ""
	wakeFlag := false
	noWakeFlag := false
	socketFlag := ""
	projectFlag := ""
	var bodyParts []string
	pastDoubleDash := false

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		if pastDoubleDash {
			bodyParts = append(bodyParts, arg)
			continue
		}
		switch {
		case arg == "--":
			pastDoubleDash = true
		case arg == "--help" || arg == "-h":
			commsSendUsage()
			return 0
		case arg == "--to" && i+1 < len(subArgs):
			i++
			toFlag = subArgs[i]
		case strings.HasPrefix(arg, "--to="):
			toFlag = strings.TrimPrefix(arg, "--to=")
		case arg == "--broadcast":
			broadcastFlag = true
		case arg == "--from" && i+1 < len(subArgs):
			i++
			fromFlag = subArgs[i]
		case strings.HasPrefix(arg, "--from="):
			fromFlag = strings.TrimPrefix(arg, "--from=")
		case arg == "--topic" && i+1 < len(subArgs):
			i++
			topicFlag = subArgs[i]
		case strings.HasPrefix(arg, "--topic="):
			topicFlag = strings.TrimPrefix(arg, "--topic=")
		case arg == "--reply-to" && i+1 < len(subArgs):
			i++
			replyToFlag = subArgs[i]
		case strings.HasPrefix(arg, "--reply-to="):
			replyToFlag = strings.TrimPrefix(arg, "--reply-to=")
		case arg == "--wake":
			wakeFlag = true
		case arg == "--no-wake":
			noWakeFlag = true
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
			fmt.Fprintf(os.Stderr, "harmonik comms send: unknown flag %q\n", arg)
			return 1
		default:
			bodyParts = append(bodyParts, arg)
		}
	}

	if toFlag != "" && broadcastFlag {
		fmt.Fprintf(os.Stderr, "harmonik comms send: --to and --broadcast are mutually exclusive\n")
		return 1
	}
	if toFlag == "" && !broadcastFlag {
		fmt.Fprintf(os.Stderr, "harmonik comms send: one of --to NAME or --broadcast is required\n")
		return 1
	}
	if wakeFlag && noWakeFlag {
		fmt.Fprintf(os.Stderr, "harmonik comms send: --wake and --no-wake are mutually exclusive\n")
		return 1
	}
	if wakeFlag && broadcastFlag {
		fmt.Fprintf(os.Stderr, "harmonik comms send: --wake requires --to (cannot wake a broadcast)\n")
		return 1
	}

	to := toFlag
	if broadcastFlag {
		to = "*"
	}

	from := fromFlag
	if from == "" {
		from = os.Getenv("HARMONIK_AGENT")
	}
	if from == "" {
		fmt.Fprintf(os.Stderr, "harmonik comms send: --from is required (or set $HARMONIK_AGENT)\n")
		return 1
	}

	var body string
	if len(bodyParts) == 1 && bodyParts[0] == "-" {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik comms send: read stdin: %v\n", err)
			return 1
		}
		body = string(raw)
	} else if len(bodyParts) > 0 {
		body = strings.Join(bodyParts, " ")
	}
	if body == "" {
		fmt.Fprintf(os.Stderr, "harmonik comms send: body is required (pass as trailing args or use - for stdin)\n")
		return 1
	}

	projectDir := projectFlag
	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik comms send: cannot determine cwd: %v\n", err)
			return 1
		}
		projectDir = wd
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms send: cannot resolve project path: %v\n", err)
		return 1
	}
	sockPath := socketFlag
	if sockPath == "" {
		sockPath = filepath.Join(absProject, ".harmonik", "daemon.sock")
	}

	sessionID := resolveSessionID()
	if sessionID != "" {
		eventsPath := filepath.Join(absProject, ".harmonik", "events", "events.jsonl")
		if warn := checkCommsNameConflict(eventsPath, from, sessionID); warn != "" {
			fmt.Fprintf(os.Stderr, "harmonik comms send: WARNING: %s\n", warn)
		}
	}

	commsSendPayload := map[string]any{
		"from": from,
		"to":   to,
		"body": body,
	}
	if topicFlag != "" {
		commsSendPayload["topic"] = topicFlag
	}
	if replyToFlag != "" {
		commsSendPayload["in_reply_to"] = replyToFlag
	}
	if sessionID != "" {
		commsSendPayload["session_id"] = sessionID
	}

	payloadBytes, err := json.Marshal(commsSendPayload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms send: marshal payload: %v\n", err)
		return 1
	}

	reqBytes, err := json.Marshal(map[string]any{
		"op":      "comms-send",
		"payload": json.RawMessage(payloadBytes),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms send: marshal request: %v\n", err)
		return 1
	}

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 5*time.Second)
	conn, dialErr := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	cancelDial()
	if dialErr != nil {
		if commsIsSocketAbsent(dialErr) || commsIsConnRefused(dialErr) {
			fmt.Fprintf(os.Stderr, "harmonik comms send: daemon not running (socket %s missing or refused)\n", sockPath)
			return 17
		}
		fmt.Fprintf(os.Stderr, "harmonik comms send: dial %s: %v\n", sockPath, dialErr)
		return 1
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("harmonik comms send: close connection: %v", closeErr)
		}
	}()

	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms send: write request: %v\n", writeErr)
		return 1
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		if closeErr := uw.CloseWrite(); !isBenignCloseWrite(closeErr) {
			log.Printf("harmonik comms send: close write: %v", closeErr)
			return 1
		}
	}

	var resp struct {
		Ok     bool            `json:"ok"`
		Result json.RawMessage `json:"result,omitempty"`
		Error  string          `json:"error,omitempty"`
	}
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms send: decode response: %v\n", decErr)
		return 1
	}

	if !resp.Ok {
		fmt.Fprintf(os.Stderr, "harmonik comms send: %s\n", resp.Error)
		return 1
	}

	var result struct {
		EventID string `json:"event_id"`
	}
	if unmarshalErr := json.Unmarshal(resp.Result, &result); unmarshalErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms send: decode result: %v\n", unmarshalErr)
		return 1
	}

	fmt.Println(result.EventID)

	undelivered := to != "*" && !commsRecipientKnown(absProject, to)
	if undelivered {
		fmt.Fprintf(os.Stderr, "harmonik comms send: WARNING: no agent named %q is known in this project. The name is not in the presence registry, not in the crew registry and not in the agent manifests.\n", to)
		fmt.Fprintf(os.Stderr, "harmonik comms send: The message is recorded and nobody has received it. It waits until an agent runs `harmonik comms recv --agent %s`.\n", to)
		fmt.Fprintf(os.Stderr, "harmonik comms send: Exiting 1 because nobody received it. The event id above is real and the message is durable.\n")
	}

	if commsShouldWake(to != "*", noWakeFlag) {
		if wakeErr := commsWakePaneForAgent(context.Background(), absProject, to); wakeErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik comms send: wake: %v\n", wakeErr)
		}
	}

	if undelivered {
		return 1
	}
	return 0
}

func commsShouldWake(directed, noWake bool) bool {
	return directed && !noWake
}

var commsAlwaysAddressable = []string{"operator"}

func commsRecipientKnown(absProject, name string) bool {
	for _, builtin := range commsAlwaysAddressable {
		if name == builtin {
			return true
		}
	}
	if records, err := crew.List(absProject); err == nil {
		for _, r := range records {
			if r.Name == name {
				return true
			}
		}
	}
	entries, err := os.ReadDir(filepath.Join(absProject, ".harmonik", "agents"))
	if err == nil {
		for _, e := range entries {
			if e.IsDir() && e.Name() == name {
				return true
			}
		}
	}
	eventsPath := filepath.Join(absProject, ".harmonik", "events", "events.jsonl")
	_, online := ComputePresenceRegistry(eventsPath)[name]
	return online
}

func resolveProjectPath(projectDir string) string {
	resolved, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		return projectDir
	}
	return resolved
}

// commsWakePaneCandidates returns the tmux targets to try, in order, when a
// directed comms message must wake its recipient.
//
// Order, and why:
//
//  1. The crew registry handle VERBATIM. A handle is "session:window"
//     ("hk-alpha:1", "harmonik-<hash>-crew-charlie:agent") and tmux resolves
//     that form to the window's ACTIVE pane. This is the only candidate that
//     is correct regardless of the server's pane-base-index, and it is the
//     same rule internal/keeper/tmuxresolve.go ResolveTmuxTarget follows.
//  2. and 3. The two naming conventions, for an agent with no registry record.
//     Both are bare session names, so they also resolve to an active pane.
//
// No candidate names a pane index. Appending ".0" to the handle was candidate 1
// until hk-vigk8, and on a server with pane-base-index 1 it made every directed
// wake fail fleet-wide. Keeping it as a late fallback buys nothing either: the
// loop stops at the first success, so a ".0" target is reached only after the
// bare handle already failed, and a narrower target cannot resolve where the
// wider one did not.
//
// KNOWN LIMIT: if an agent's window holds more than one pane and the agent does
// not hold the active one, the wake lands on the wrong pane and reports success.
// A pane id is the real fix. Every fleet window is single-pane today.
func commsWakePaneCandidates(projectDir, agentName string) []string {
	hash := lifecycle.ComputeProjectHash(resolveProjectPath(projectDir))
	var handle string
	if rec, loadErr := crew.Load(projectDir, agentName); loadErr == nil && rec.Handle != "" {
		handle = rec.Handle
	}
	var candidates []string
	if handle != "" {
		candidates = append(candidates, handle)
	}
	candidates = append(candidates,
		lifecycle.TmuxSessionName(hash, "crew-"+agentName),
		lifecycle.TmuxSessionName(hash, agentName))
	return candidates
}

func commsWakePaneForAgent(ctx context.Context, projectDir, agentName string) error {
	const nudgeMsg = "[[harmonik-message:v1 origin=comms]]\nYou have a new comms message. Please check your inbox."
	candidates := commsWakePaneCandidates(projectDir, agentName)
	if len(candidates) == 0 {
		return fmt.Errorf("no tmux target could be derived for agent %q", agentName)
	}
	// Keep EVERY attempt, not just the last one. Reporting only the last error
	// named a naming-convention guess and hid the registry candidate that
	// really failed, so the fleet read a wrong-pane bug as "unknown agent" and
	// lost a work window chasing it (hk-vigk8).
	attempts := make([]error, 0, len(candidates))
	for _, paneTarget := range candidates {
		err := commsInjectTmuxPane(ctx, paneTarget, nudgeMsg)
		if err == nil {
			return nil
		}
		attempts = append(attempts, fmt.Errorf("candidate %d/%d %q: %w", len(attempts)+1, len(candidates), paneTarget, err))
	}
	return fmt.Errorf("no tmux target accepted the wake for agent %q; tried %d:\n%w",
		agentName, len(candidates), errors.Join(attempts...))
}

func commsInjectTmuxPane(ctx context.Context, paneTarget, text string) error {
	buf := ltmux.BufferName("comms", "wake")

	loadCmd := exec.CommandContext(ctx, "tmux", "load-buffer", "-b", buf, "-")
	loadCmd.Stdin = strings.NewReader(text)
	if out, err := loadCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux load-buffer: %w (stderr: %s)", err, strings.TrimSpace(string(out)))
	}

	pasteCmd := exec.CommandContext(ctx, "tmux", "paste-buffer", "-b", buf, "-t", paneTarget, "-d")
	if out, err := pasteCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux paste-buffer -t %s: %w (stderr: %s)", paneTarget, err, strings.TrimSpace(string(out)))
	}

	enterCmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", paneTarget, "Enter")
	if out, err := enterCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux send-keys Enter -t %s: %w (stderr: %s)", paneTarget, err, strings.TrimSpace(string(out)))
	}

	return nil
}

func commsDaemonDown(absProject string) (sockPath string, down bool) {
	sockPath = filepath.Join(absProject, ".harmonik", "daemon.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
	if err != nil {
		return sockPath, commsIsSocketAbsent(err) || commsIsConnRefused(err)
	}
	if closeErr := conn.Close(); closeErr != nil {
		log.Printf("harmonik comms: close daemon probe: %v", closeErr)
	}
	return sockPath, false
}

func commsIsSocketAbsent(err error) bool {
	if errors.Is(err, syscall.ENOENT) {
		return true
	}
	return errors.Is(err, syscall.EINVAL)
}

func commsIsConnRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}

func commsUsage() {
	fmt.Print(`harmonik comms — agent-to-agent messaging surface

USAGE
  harmonik comms <verb> [flags]

VERBS
  send    Send an agent_message to a named agent or broadcast to all
  recv    Receive unread agent_messages from the durable cursor (daemon required)
  log     Read-only operator view of recent agent_message events (no daemon needed)
  join    Emit an agent_presence{online, reason:"join"|"refresh"} beat (presence registry)
  leave   Emit an agent_presence{offline, reason:"leave"} beat (presence registry)
  who     List currently-online agents from the presence registry (no daemon needed)

EXIT CODES
  0   Success
  1   Argument error, op rejected, or -- on send -- the recipient is a name
      this project does not know. The message is still recorded; nobody has
      received it. See "harmonik comms send --help".
  2   Unrecognised verb
  17  Daemon not running (send/recv/join/leave)

EXAMPLES
  harmonik comms send --to other-agent -- Hello
  harmonik comms send --to crew-alpha --wake -- New task for you
  harmonik comms send --broadcast --from myagent -- Status update
  harmonik comms recv --agent myagent
  harmonik comms recv --agent myagent --follow
  harmonik comms recv --agent myagent --from orchestrator --json
  harmonik comms log --since 30m
  harmonik comms log --since 30m --to myagent --json
  harmonik comms join --name myagent
  harmonik comms join --name myagent --reason=refresh  # persisted TTL heartbeat
  harmonik comms leave --name myagent
  harmonik comms who
  harmonik comms who --json
`)
}

func commsSendUsage() {
	fmt.Print(`harmonik comms send — send an agent_message via the daemon

USAGE
  harmonik comms send (--to NAME | --broadcast) [--from NAME] [--topic T] [--reply-to ID] [--wake | --no-wake] [flags] [--] <body>

FLAGS
  --to NAME       Directed recipient agent name. Mutually exclusive with --broadcast.
  --broadcast     Broadcast to all agents (sets to:"*"). Mutually exclusive with --to.
  --from NAME     Sender identity (default: $HARMONIK_AGENT env var). Required.
  --topic T       Optional free-text filter key.
  --reply-to ID   Optional event_id of the message being replied to (threading hint).
  --wake          Explicitly request the default directed-send pane wake. Requires
                  --to (not --broadcast).
  --no-wake       Deliver a directed message without nudging the recipient's pane.
                  By default, every directed send wakes the recipient. The pane is resolved
                  from the crew registry handle, then the crew and bare-agent
                  tmux session naming conventions.
                  Best-effort: wake failures are reported to stderr but do not affect
                  the exit code.
  --socket PATH   Override socket path (default: <project>/.harmonik/daemon.sock).
  --project DIR   Project directory (default: cwd).
  --              End of flags; remaining args form the body.
  <body> | -      Message body as trailing args (joined by space) or "-" to read stdin.

UNKNOWN RECIPIENTS
  A --to name this project does not use is still ACCEPTED and the message is
  still durably recorded, because an agent that starts later reads the whole
  backlog on its first recv. The event id is printed on stdout as always. But
  nobody has received it yet, so the send warns on stderr AND exits 1. A caller
  that branches on the exit code must not read that send as delivered. A name
  counts as known through the crew registry, .harmonik/agents/, or the presence
  registry; "operator" is always addressable and --broadcast is never checked.

  "harmonik wake --agent <name>" exits 1 for a name that matches nothing too,
  but the two surfaces do NOT share one list and are not meant to. wake reaches
  a tmux pane and send reaches a mailbox, so "operator" is addressable here and
  is not a wake target, while "captain" and "watch" are wake builtins. The two
  agree only on a name NEITHER of them knows.

EXIT CODES
  0   Success: recorded AND the recipient is a name this project knows
      (event_id printed to stdout)
  1   Argument error, daemon rejected the op, or the message was recorded but
      the recipient is a name nobody uses (see UNKNOWN RECIPIENTS)
  17  Daemon not running (socket missing or ECONNREFUSED)

EXAMPLES
  harmonik comms send --to alice -- Hello from bob
  harmonik comms send --to alice --from bob --topic status -- ready
  harmonik comms send --to alice --wake -- You have work to do
  harmonik comms send --broadcast --from orchestrator -- Batch complete
  echo "body text" | harmonik comms send --to alice --from me -
`)
}

func runCommsLogSubcommand(subArgs []string) int {
	sinceFlag := ""
	toFlag := ""
	fromFlag := ""
	topicFlag := ""
	jsonFlag := false
	projectFlag := ""

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			commsLogUsage()
			return 0
		case arg == "--since" && i+1 < len(subArgs):
			i++
			sinceFlag = subArgs[i]
		case strings.HasPrefix(arg, "--since="):
			sinceFlag = strings.TrimPrefix(arg, "--since=")
		case arg == "--to" && i+1 < len(subArgs):
			i++
			toFlag = subArgs[i]
		case strings.HasPrefix(arg, "--to="):
			toFlag = strings.TrimPrefix(arg, "--to=")
		case arg == "--from" && i+1 < len(subArgs):
			i++
			fromFlag = subArgs[i]
		case strings.HasPrefix(arg, "--from="):
			fromFlag = strings.TrimPrefix(arg, "--from=")
		case arg == "--topic" && i+1 < len(subArgs):
			i++
			topicFlag = subArgs[i]
		case strings.HasPrefix(arg, "--topic="):
			topicFlag = strings.TrimPrefix(arg, "--topic=")
		case arg == "--json":
			jsonFlag = true
		case arg == "--project" && i+1 < len(subArgs):
			i++
			projectFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik comms log: unknown flag %q\n", arg)
			return 1
		default:
			fmt.Fprintf(os.Stderr, "harmonik comms log: unexpected argument %q\n", arg)
			return 1
		}
	}

	if projectFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik comms log: cannot determine cwd: %v\n", err)
			return 1
		}
		projectFlag = wd
	}
	absProject, err := filepath.Abs(projectFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms log: cannot resolve project path: %v\n", err)
		return 1
	}
	eventsPath := filepath.Join(absProject, ".harmonik", "events", "events.jsonl")

	if sockPath, down := commsDaemonDown(absProject); down {
		fmt.Fprintf(os.Stderr, "harmonik comms log: the daemon is not running (socket %s missing or refused).\n", sockPath)
		fmt.Fprintf(os.Stderr, "harmonik comms log: these lines come from the event log at %s. They are history, not live traffic.\n", eventsPath)
	}

	var sinceID core.EventID // zero value = scan from beginning
	var wallCutoff time.Time // zero = no wall-time filter
	if sinceFlag != "" {
		if err := sinceID.UnmarshalText([]byte(sinceFlag)); err == nil {
		} else {
			dur, durErr := parseFriendlyDuration(sinceFlag)
			if durErr != nil {
				fmt.Fprintf(os.Stderr, "harmonik comms log: --since %q is not a valid event_id or duration: %v\n", sinceFlag, durErr)
				return 1
			}
			wallCutoff = time.Now().Add(-dur)
		}
	}

	count := 0
	for ev := range eventbus.ScanAfter(eventsPath, sinceID) {
		if ev.Type != "agent_message" {
			continue
		}

		if !wallCutoff.IsZero() && ev.TimestampWall.Before(wallCutoff) {
			continue
		}

		var p core.AgentMessagePayload
		if decErr := json.Unmarshal(ev.Payload, &p); decErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik comms log: malformed agent_message payload (event_id=%s): %v\n", ev.EventID, decErr)
			continue
		}

		if fromFlag != "" && p.From != fromFlag {
			continue
		}
		if toFlag != "" && p.To != toFlag && p.To != "*" {
			continue
		}
		if topicFlag != "" && p.Topic != topicFlag {
			continue
		}

		count++
		if jsonFlag {
			line, marshalErr := json.Marshal(ev)
			if marshalErr != nil {
				fmt.Fprintf(os.Stderr, "harmonik comms log: marshal event: %v\n", marshalErr)
				return 1
			}
			fmt.Println(string(line))
		} else {
			ts := ev.TimestampWall.UTC().Format(time.RFC3339)
			direction := fmt.Sprintf("%s → %s", p.From, p.To)
			if p.Topic != "" {
				fmt.Printf("%s  %-30s  [%s]  %s\n", ts, direction, p.Topic, p.Body)
			} else {
				fmt.Printf("%s  %-30s  %s\n", ts, direction, p.Body)
			}
		}
	}

	if count == 0 && !jsonFlag {
		fmt.Fprintln(os.Stderr, "harmonik comms log: no agent_message events found")
	}
	return 0
}

func commsLogUsage() {
	fmt.Print(`harmonik comms log — read-only operator view of agent_message events

USAGE
  harmonik comms log [--since <event_id|duration>] [--to NAME] [--from NAME] [--topic T] [--json] [--project DIR]

Scans events.jsonl for all agent_message events ordered by event_id (file/chronological order).
Does NOT advance any agent cursor. No daemon connection required.
When the daemon is down, the output is labelled on stderr as history read from
the event log, because nothing is adding to it.

FLAGS
  --since EVENT_ID|DURATION
                  Start from: an event_id (scan after that event) OR a duration meaning "events in
                  the last <duration>". Accepts Go units (30m, 1h, 200h) plus d (days) and w
                  (weeks): 8d, 2w. Without --since, scans all events.
  --to NAME       Filter: only messages directed to NAME or broadcast ("*").
  --from NAME     Filter: only messages from NAME.
  --topic T       Filter: only messages with topic T.
  --json          Emit one JSON event envelope per line (NDJSON) instead of human-readable output.
  --project DIR   Project directory (default: cwd). Used to locate .harmonik/events/events.jsonl.

EXIT CODES
  0   Success
  1   Argument error or read failure

EXAMPLES
  harmonik comms log                          # all agent_message events
  harmonik comms log --since 30m              # last 30 minutes
  harmonik comms log --since 8d               # last 8 days
  harmonik comms log --since 1h --to alice    # last hour, directed to alice or broadcast
  harmonik comms log --from orchestrator      # all messages from orchestrator
  harmonik comms log --json                   # machine-readable NDJSON
`)
}

func parseFriendlyDuration(s string) (time.Duration, error) {
	if len(s) >= 2 {
		last := s[len(s)-1]
		if last == 'd' || last == 'w' {
			n, err := strconv.ParseInt(s[:len(s)-1], 10, 64)
			if err == nil && n > 0 {
				if last == 'w' {
					return time.Duration(n) * 7 * 24 * time.Hour, nil
				}
				return time.Duration(n) * 24 * time.Hour, nil
			}
		}
	}
	return time.ParseDuration(s)
}

// PresenceRecord aliases presence.Record (the registry projection entry).
type PresenceRecord = presence.Record

// PresenceState aliases presence.State (the computed liveness state).
type PresenceState = presence.State

// Presence-state constants alias presence.State{Online,Stale,Offline}.
const (
	PresenceStateOnline  = presence.StateOnline
	PresenceStateStale   = presence.StateStale
	PresenceStateOffline = presence.StateOffline
)

// GetPresenceState delegates to presence.GetState.
func GetPresenceState(r PresenceRecord) PresenceState { return presence.GetState(r) }

// IsOnline delegates to presence.IsOnline.
func IsOnline(r PresenceRecord) bool { return presence.IsOnline(r) }

// IsStale delegates to presence.IsStale.
func IsStale(r PresenceRecord) bool { return presence.IsStale(r) }

// ComputePresenceRegistry delegates to presence.ComputeRegistry — the canonical
// agent-presence projection over events.jsonl (logic unchanged from the former
// package-main implementation).
func ComputePresenceRegistry(eventsPath string) map[string]PresenceRecord {
	return presence.ComputeRegistry(eventsPath)
}

func resolveSessionID() string {
	if id := os.Getenv("HARMONIK_SESSION_ID"); id != "" {
		return id
	}
	return os.Getenv("HARMONIK_RUN_ID")
}

func checkCommsNameConflict(eventsPath, name, sessionID string) string {
	if sessionID == "" || name == "" {
		return ""
	}
	registry := ComputePresenceRegistry(eventsPath)
	rec, ok := registry[name]
	if !ok {
		return ""
	}
	if GetPresenceState(rec) == PresenceStateOffline {
		return ""
	}
	if rec.SessionID == "" || rec.SessionID == sessionID {
		return ""
	}
	return fmt.Sprintf(
		"identity conflict: %q is already online under session %s — two sessions claiming the same name may send conflicting orders",
		name, rec.SessionID,
	)
}

func runCommsPresenceSubcommand(subArgs []string, verb string) int {
	nameFlag := ""
	reasonFlag := ""
	socketFlag := ""
	projectFlag := ""

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			commsPresenceUsage(verb)
			return 0
		case arg == "--name" && i+1 < len(subArgs):
			i++
			nameFlag = subArgs[i]
		case strings.HasPrefix(arg, "--name="):
			nameFlag = strings.TrimPrefix(arg, "--name=")
		case arg == "--reason" && i+1 < len(subArgs):
			i++
			reasonFlag = subArgs[i]
		case strings.HasPrefix(arg, "--reason="):
			reasonFlag = strings.TrimPrefix(arg, "--reason=")
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
			fmt.Fprintf(os.Stderr, "harmonik comms %s: unknown flag %q\n", verb, arg)
			return 1
		default:
			fmt.Fprintf(os.Stderr, "harmonik comms %s: unexpected argument %q\n", verb, arg)
			return 1
		}
	}

	name := nameFlag
	if name == "" {
		name = os.Getenv("HARMONIK_AGENT")
	}
	if name == "" {
		fmt.Fprintf(os.Stderr, "harmonik comms %s: --name is required (or set $HARMONIK_AGENT)\n", verb)
		return 1
	}

	status := "online"
	reason := "join"
	if verb == "leave" {
		status = "offline"
		reason = "leave"
	}
	if reasonFlag != "" && verb == "join" {
		switch reasonFlag {
		case "join", "refresh":
			reason = reasonFlag
		default:
			fmt.Fprintf(os.Stderr, "harmonik comms join: --reason %q is invalid; must be \"join\" or \"refresh\"\n", reasonFlag)
			return 1
		}
	}

	projectDir := projectFlag
	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik comms %s: cannot determine cwd: %v\n", verb, err)
			return 1
		}
		projectDir = wd
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms %s: cannot resolve project path: %v\n", verb, err)
		return 1
	}

	sockPath := socketFlag
	if sockPath == "" {
		sockPath = filepath.Join(absProject, ".harmonik", "daemon.sock")
	}

	sessionID := resolveSessionID()
	if verb == "join" {
		eventsPath := filepath.Join(absProject, ".harmonik", "events", "events.jsonl")
		if warn := checkCommsNameConflict(eventsPath, name, sessionID); warn != "" {
			fmt.Fprintf(os.Stderr, "harmonik comms join: WARNING: %s\n", warn)
		}
	}

	presencePayload := map[string]any{
		"agent":  name,
		"status": status,
		"reason": reason,
	}
	if sessionID != "" {
		presencePayload["session_id"] = sessionID
	}

	payloadBytes, err := json.Marshal(presencePayload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms %s: marshal payload: %v\n", verb, err)
		return 1
	}

	reqBytes, err := json.Marshal(map[string]any{
		"op":      "comms-presence",
		"payload": json.RawMessage(payloadBytes),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms %s: marshal request: %v\n", verb, err)
		return 1
	}

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 5*time.Second)
	conn, dialErr := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	cancelDial()
	if dialErr != nil {
		if commsIsSocketAbsent(dialErr) || commsIsConnRefused(dialErr) {
			fmt.Fprintf(os.Stderr, "harmonik comms %s: daemon not running (socket %s missing or refused)\n", verb, sockPath)
			return 17
		}
		fmt.Fprintf(os.Stderr, "harmonik comms %s: dial %s: %v\n", verb, sockPath, dialErr)
		return 1
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("harmonik comms %s: close connection: %v", verb, closeErr)
		}
	}()

	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms %s: write request: %v\n", verb, writeErr)
		return 1
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		if closeErr := uw.CloseWrite(); !isBenignCloseWrite(closeErr) {
			log.Printf("harmonik comms %s: close write: %v", verb, closeErr)
			return 1
		}
	}

	var resp struct {
		Ok     bool            `json:"ok"`
		Result json.RawMessage `json:"result,omitempty"`
		Error  string          `json:"error,omitempty"`
	}
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms %s: decode response: %v\n", verb, decErr)
		return 1
	}

	if !resp.Ok {
		fmt.Fprintf(os.Stderr, "harmonik comms %s: %s\n", verb, resp.Error)
		return 1
	}

	var result struct {
		EventID string `json:"event_id"`
	}
	if unmarshalErr := json.Unmarshal(resp.Result, &result); unmarshalErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms %s: decode result: %v\n", verb, unmarshalErr)
		return 1
	}

	fmt.Println(result.EventID)
	return 0
}

func runCommsWhoSubcommand(subArgs []string) int {
	jsonFlag := false
	projectFlag := ""

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			commsWhoUsage()
			return 0
		case arg == "--json":
			jsonFlag = true
		case arg == "--project" && i+1 < len(subArgs):
			i++
			projectFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik comms who: unknown flag %q\n", arg)
			return 1
		default:
			fmt.Fprintf(os.Stderr, "harmonik comms who: unexpected argument %q\n", arg)
			return 1
		}
	}

	if projectFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik comms who: cannot determine cwd: %v\n", err)
			return 1
		}
		projectFlag = wd
	}
	absProject, err := filepath.Abs(projectFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms who: cannot resolve project path: %v\n", err)
		return 1
	}
	eventsPath := filepath.Join(absProject, ".harmonik", "events", "events.jsonl")

	if sockPath, down := commsDaemonDown(absProject); down {
		fmt.Fprintf(os.Stderr, "harmonik comms who: the daemon is not running (socket %s missing or refused). No agent can be online now.\n", sockPath)
		fmt.Fprintf(os.Stderr, "harmonik comms who: this roster comes from the event log at %s. It is history, not live presence.\n", eventsPath)
	}

	registry := ComputePresenceRegistry(eventsPath)

	type whoEntry struct {
		Agent    string    `json:"agent"`
		LastSeen time.Time `json:"last_seen"`
		Status   string    `json:"status"` // "online" or "stale"
	}
	var entries []whoEntry
	for _, rec := range registry {
		switch GetPresenceState(rec) {
		case PresenceStateOnline:
			entries = append(entries, whoEntry{Agent: rec.Agent, LastSeen: rec.EffectiveLastSeen, Status: "online"})
		case PresenceStateStale:
			entries = append(entries, whoEntry{Agent: rec.Agent, LastSeen: rec.EffectiveLastSeen, Status: "stale"})
		}
	}
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].Agent < entries[j-1].Agent; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}

	if len(entries) == 0 {
		if !jsonFlag {
			fmt.Fprintln(os.Stderr, "harmonik comms who: no agents currently online")
		}
		return 0
	}

	for _, e := range entries {
		if jsonFlag {
			line, marshalErr := json.Marshal(e)
			if marshalErr != nil {
				fmt.Fprintf(os.Stderr, "harmonik comms who: marshal entry: %v\n", marshalErr)
				return 1
			}
			fmt.Println(string(line))
		} else {
			if e.Status == "stale" {
				age := time.Since(e.LastSeen)
				fmt.Printf("%-30s  stale (last seen %dm ago)\n", e.Agent, int(age.Minutes()))
			} else {
				fmt.Printf("%-30s  last_seen %s\n", e.Agent, e.LastSeen.UTC().Format(time.RFC3339))
			}
		}
	}
	return 0
}

func commsWhoUsage() {
	fmt.Print(`harmonik comms who — list currently-online agents from the presence registry

USAGE
  harmonik comms who [--json] [--project DIR]

Reads the presence projection over events.jsonl and prints agents that are
online within the staleness window (~120s). An agent is online if its latest
agent_presence beat has status="online" and last_seen is within 120s of now.
Read-only: emits nothing, advances no cursor. No daemon connection required.
When the daemon is down, the roster is labelled on stderr as history, because
no agent can be online without a bus.

FLAGS
  --json          Emit one JSON object per online agent (NDJSON — one object per
                  line, not a JSON array). Fields: agent (string), last_seen (RFC3339).
                  Pipe to 'jq -s' to collect into an array, or process line-by-line:
                    harmonik comms who --json | jq -s '.'
                    harmonik comms who --json | while IFS= read -r line; do ...; done
  --project DIR   Project directory (default: cwd). Used to locate
                  .harmonik/events/events.jsonl.

EXIT CODES
  0   Success (zero or more agents listed)
  1   Argument error or read failure

EXAMPLES
  harmonik comms who                  # human-readable list of online agents
  harmonik comms who --json           # machine-readable NDJSON
  harmonik comms who --json | jq -s '.'  # collect into a JSON array
  harmonik comms who --project /path  # specify project directory
`)
}

func commsPresenceUsage(verb string) {
	joinReasonNote := ""
	if verb == "join" {
		joinReasonNote = `  --reason join|refresh  Presence reason (default: join). Use "refresh" for
                        periodic heartbeat calls — refresh beats are not persisted
                        to events.jsonl, keeping the log clean (hk-ru45u).
`
	}
	fmt.Printf(`harmonik comms %s — emit an agent_presence beat (presence registry)

USAGE
  harmonik comms %s [--name NAME] [--socket PATH] [--project DIR]

Sends a comms-presence op to the daemon, which emits an agent_presence event
with status="%s" and reason="%s". The minted event_id is printed to stdout.

FLAGS
  --name NAME     Agent identity (default: $HARMONIK_AGENT env var). Required.
%s  --socket PATH   Override socket path (default: <project>/.harmonik/daemon.sock).
  --project DIR   Project directory (default: cwd).

EXIT CODES
  0   Success (event_id printed to stdout)
  1   Argument error or daemon rejected the op
  17  Daemon not running (socket missing or ECONNREFUSED)

EXAMPLES
  harmonik comms %s --name myagent
  harmonik comms %s                    # uses $HARMONIK_AGENT
`, verb, verb, func() string {
		if verb == "join" {
			return "online"
		}
		return "offline"
	}(), verb, joinReasonNote, verb, verb)
}

func runCommsRecvSubcommand(subArgs []string) int {
	agentFlag := ""
	fromFlag := ""
	topicFlag := ""
	followFlag := false
	waitFlag := false
	timeoutFlag := time.Duration(0)
	jsonFlag := false
	socketFlag := ""
	projectFlag := ""

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			commsRecvUsage()
			return 0
		case arg == "--agent" && i+1 < len(subArgs):
			i++
			agentFlag = subArgs[i]
		case strings.HasPrefix(arg, "--agent="):
			agentFlag = strings.TrimPrefix(arg, "--agent=")
		case arg == "--from" && i+1 < len(subArgs):
			i++
			fromFlag = subArgs[i]
		case strings.HasPrefix(arg, "--from="):
			fromFlag = strings.TrimPrefix(arg, "--from=")
		case arg == "--topic" && i+1 < len(subArgs):
			i++
			topicFlag = subArgs[i]
		case strings.HasPrefix(arg, "--topic="):
			topicFlag = strings.TrimPrefix(arg, "--topic=")
		case arg == "--follow":
			followFlag = true
		case arg == "--wait":
			waitFlag = true
		case arg == "--timeout" && i+1 < len(subArgs):
			i++
			d, perr := time.ParseDuration(subArgs[i])
			if perr != nil {
				fmt.Fprintf(os.Stderr, "harmonik comms recv: --timeout %q is not a valid duration: %v\n", subArgs[i], perr)
				return 1
			}
			timeoutFlag = d
		case strings.HasPrefix(arg, "--timeout="):
			d, perr := time.ParseDuration(strings.TrimPrefix(arg, "--timeout="))
			if perr != nil {
				fmt.Fprintf(os.Stderr, "harmonik comms recv: --timeout is not a valid duration: %v\n", perr)
				return 1
			}
			timeoutFlag = d
		case arg == "--json":
			jsonFlag = true
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
			fmt.Fprintf(os.Stderr, "harmonik comms recv: unknown flag %q\n", arg)
			return 1
		default:
			fmt.Fprintf(os.Stderr, "harmonik comms recv: unexpected argument %q\n", arg)
			return 1
		}
	}

	agent := agentFlag
	if agent == "" {
		agent = os.Getenv("HARMONIK_AGENT")
	}
	if agent == "" {
		fmt.Fprintf(os.Stderr, "harmonik comms recv: --agent is required (or set $HARMONIK_AGENT)\n")
		return 1
	}

	if followFlag && waitFlag {
		fmt.Fprintf(os.Stderr, "harmonik comms recv: --follow and --wait are mutually exclusive\n")
		return 1
	}
	if timeoutFlag != 0 && !waitFlag {
		fmt.Fprintf(os.Stderr, "harmonik comms recv: --timeout requires --wait\n")
		return 1
	}

	sockPath := socketFlag
	if sockPath == "" {
		projectDir := projectFlag
		if projectDir == "" {
			wd, err := os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "harmonik comms recv: cannot determine cwd: %v\n", err)
				return 1
			}
			projectDir = wd
		}
		absProject, err := filepath.Abs(projectDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik comms recv: cannot resolve project path: %v\n", err)
			return 1
		}
		sockPath = filepath.Join(absProject, ".harmonik", "daemon.sock")
	}

	recvPayload := map[string]any{
		"agent": agent,
	}
	if fromFlag != "" {
		recvPayload["from"] = fromFlag
	}
	if topicFlag != "" {
		recvPayload["topic"] = topicFlag
	}
	if followFlag || waitFlag {
		recvPayload["live"] = true
	}

	payloadBytes, err := json.Marshal(recvPayload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms recv: marshal payload: %v\n", err)
		return 1
	}

	reqBytes, err := json.Marshal(map[string]any{
		"op":      "comms-recv",
		"payload": json.RawMessage(payloadBytes),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms recv: marshal request: %v\n", err)
		return 1
	}

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 5*time.Second)
	conn, dialErr := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	cancelDial()
	if dialErr != nil {
		if commsIsSocketAbsent(dialErr) || commsIsConnRefused(dialErr) {
			fmt.Fprintf(os.Stderr, "harmonik comms recv: daemon not running (socket %s missing or refused)\n", sockPath)
			return 17
		}
		fmt.Fprintf(os.Stderr, "harmonik comms recv: dial %s: %v\n", sockPath, dialErr)
		return 1
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("harmonik comms recv: close connection: %v", closeErr)
		}
	}()

	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms recv: write request: %v\n", writeErr)
		return 1
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		if closeErr := uw.CloseWrite(); !isBenignCloseWrite(closeErr) {
			log.Printf("harmonik comms recv: close write: %v", closeErr)
			return 1
		}
	}

	var resp struct {
		Ok     bool            `json:"ok"`
		Result json.RawMessage `json:"result,omitempty"`
		Error  string          `json:"error,omitempty"`
	}
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms recv: decode response: %v\n", decErr)
		return 1
	}

	if !resp.Ok {
		fmt.Fprintf(os.Stderr, "harmonik comms recv: %s\n", resp.Error)
		return 1
	}

	var result struct {
		Messages []struct {
			EventID   string `json:"event_id"`
			From      string `json:"from"`
			To        string `json:"to"`
			Topic     string `json:"topic,omitempty"`
			Body      string `json:"body"`
			InReplyTo string `json:"in_reply_to,omitempty"`
			Ts        string `json:"ts"`
		} `json:"messages"`
		CursorAfter string `json:"cursor_after,omitempty"`
		ScanAnchor  string `json:"scan_anchor,omitempty"`
	}
	if unmarshalErr := json.Unmarshal(resp.Result, &result); unmarshalErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms recv: decode result: %v\n", unmarshalErr)
		return 1
	}

	followAnchor := result.CursorAfter
	if followAnchor == "" {
		followAnchor = result.ScanAnchor
	}

	if waitFlag {
		if len(result.Messages) > 0 {
			m := result.Messages[0]
			printCommsRecvMsg(jsonFlag, m.EventID, m.From, m.To, m.Topic, m.Body, m.InReplyTo, m.Ts)
			return 0
		}
		return runCommsRecvWait(sockPath, agent, fromFlag, topicFlag, followAnchor, jsonFlag, timeoutFlag)
	}

	if len(result.Messages) == 0 && !jsonFlag && !followFlag {
		fmt.Fprintln(os.Stderr, "harmonik comms recv: no new messages")
		return 0
	}

	for _, msg := range result.Messages {
		printCommsRecvMsg(jsonFlag, msg.EventID, msg.From, msg.To, msg.Topic, msg.Body, msg.InReplyTo, msg.Ts)
	}

	if !followFlag {
		return 0
	}

	return runCommsRecvFollow(sockPath, agent, fromFlag, topicFlag, followAnchor, jsonFlag)
}

const commsFollowReconnectInitialBackoff = time.Second

const commsFollowReconnectMaxBackoff = 10 * time.Second

var commsFollowPresenceBeatInterval = 60 * time.Second

func sendPresenceRefreshBeat(ctx context.Context, sockPath, agent, sessionID string) error {
	payload := map[string]any{
		"agent":  agent,
		"status": "online",
		"reason": "refresh",
	}
	if sessionID != "" {
		payload["session_id"] = sessionID
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	reqBytes, err := json.Marshal(map[string]any{
		"op":      "comms-presence",
		"payload": json.RawMessage(payloadBytes),
	})
	if err != nil {
		return err
	}

	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	conn, dialErr := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	cancel()
	if dialErr != nil {
		return dialErr
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("harmonik comms presence-refresh: close connection: %v", closeErr)
		}
	}()

	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		return writeErr
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		if closeErr := uw.CloseWrite(); !isBenignCloseWrite(closeErr) {
			return closeErr
		}
	}

	var resp struct {
		Ok    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		return decErr
	}
	if !resp.Ok {
		return fmt.Errorf("comms-presence refresh: %s", resp.Error)
	}
	return nil
}

func sendPresenceLeaveBeat(ctx context.Context, sockPath, agent, sessionID string) error {
	payload := map[string]any{
		"agent":  agent,
		"status": "offline",
		"reason": "leave",
	}
	if sessionID != "" {
		payload["session_id"] = sessionID
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	reqBytes, err := json.Marshal(map[string]any{
		"op":      "comms-presence",
		"payload": json.RawMessage(payloadBytes),
	})
	if err != nil {
		return err
	}

	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	conn, dialErr := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	cancel()
	if dialErr != nil {
		return dialErr
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("harmonik comms presence-leave: close connection: %v", closeErr)
		}
	}()

	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		return writeErr
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		if closeErr := uw.CloseWrite(); !isBenignCloseWrite(closeErr) {
			return closeErr
		}
	}

	var resp struct {
		Ok    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		return decErr
	}
	if !resp.Ok {
		return fmt.Errorf("comms-presence leave: %s", resp.Error)
	}
	return nil
}

func runCommsRecvFollow(sockPath, agent, fromFilter, topicFilter, sinceEventID string, jsonOut bool) int {
	return runCommsRecvFollowIO(context.Background(), sockPath, agent, fromFilter, topicFilter, sinceEventID, jsonOut, os.Stdout)
}

func runCommsRecvFollowIO(ctx context.Context, sockPath, agent, fromFilter, topicFilter, sinceEventID string, jsonOut bool, w io.Writer) int {
	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	beatSessionID := resolveSessionID()
	defer func() {
		if agent != "" && sigCtx.Err() != nil {
			if leaveErr := sendPresenceLeaveBeat(context.WithoutCancel(sigCtx), sockPath, agent, beatSessionID); leaveErr != nil {
				log.Printf("harmonik comms recv --follow: presence leave: %v", leaveErr)
			}
		}
	}()

	beatTicker := time.NewTicker(commsFollowPresenceBeatInterval)
	defer beatTicker.Stop()
	go func() {
		for {
			select {
			case <-sigCtx.Done():
				return
			case <-beatTicker.C:
				if refreshErr := sendPresenceRefreshBeat(sigCtx, sockPath, agent, beatSessionID); refreshErr != nil {
					log.Printf("harmonik comms recv --follow: presence refresh: %v", refreshErr)
				}
			}
		}
	}()

	lastSeen := sinceEventID
	backoff := commsFollowReconnectInitialBackoff
	firstDial := true

	for {
		if sigCtx.Err() != nil {
			return 0
		}

		reqBody := map[string]any{
			"op":                "subscribe",
			"heartbeat_seconds": 60,
			"types":             []string{"agent_message"},
			"to":                agent,
		}
		if fromFilter != "" {
			reqBody["from"] = fromFilter
		}
		if topicFilter != "" {
			reqBody["topic"] = topicFilter
		}
		if lastSeen != "" {
			reqBody["since_event_id"] = lastSeen
		}
		reqBytes, err := json.Marshal(reqBody)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik comms recv --follow: marshal subscribe request: %v\n", err)
			return 1
		}

		dialCtx, cancelDial := context.WithTimeout(sigCtx, 5*time.Second)
		conn, dialErr := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
		cancelDial()

		if sigCtx.Err() != nil {
			return 0 // signal fired during dial
		}

		if dialErr != nil {
			if commsIsSocketAbsent(dialErr) || commsIsConnRefused(dialErr) {
				if firstDial {
					fmt.Fprintf(os.Stderr, "harmonik comms recv --follow: daemon not running (socket %s missing or refused)\n", sockPath)
					return 17
				}
				fmt.Fprintf(os.Stderr, "harmonik comms recv --follow: daemon offline, reconnecting in %v...\n", backoff)
				select {
				case <-time.After(backoff):
				case <-sigCtx.Done():
					return 0
				}
				if backoff < commsFollowReconnectMaxBackoff {
					backoff *= 2
					if backoff > commsFollowReconnectMaxBackoff {
						backoff = commsFollowReconnectMaxBackoff
					}
				}
				continue
			}
			fmt.Fprintf(os.Stderr, "harmonik comms recv --follow: dial %s: %v\n", sockPath, dialErr)
			return 1
		}

		backoff = commsFollowReconnectInitialBackoff
		firstDial = false

		if _, writeErr := conn.Write(reqBytes); writeErr != nil {
			if closeErr := conn.Close(); closeErr != nil {
				log.Printf("harmonik comms recv --follow: close connection after write failure: %v", closeErr)
			}
			fmt.Fprintf(os.Stderr, "harmonik comms recv --follow: write subscribe request: %v\n", writeErr)
			return 1
		}

		connCloseOnce := make(chan struct{})
		go func() {
			select {
			case <-sigCtx.Done():
				if closeErr := conn.Close(); closeErr != nil {
					log.Printf("harmonik comms recv --follow: close connection on signal: %v", closeErr)
				}
			case <-connCloseOnce:
			}
		}()

		reconnect := false
		dec := json.NewDecoder(conn)
		for {
			var env struct {
				Type          string          `json:"type"`
				EventID       string          `json:"event_id"`
				LastEventID   string          `json:"last_event_id"` // heartbeat payload field; EV-037a
				TimestampWall string          `json:"timestamp_wall"`
				Payload       json.RawMessage `json:"payload"`
				// hk-62r8w: SocketResponse fields — present when the server rejects the
				// subscribe request before streaming begins (e.g. subscribe_capacity_exceeded,
				// malformed request). Type is absent in SocketResponse so env.Type == "".
				Ok    *bool  `json:"ok"`
				Error string `json:"error"`
			}
			if decErr := dec.Decode(&env); decErr != nil {
				close(connCloseOnce) // stop the signal-closer goroutine
				if closeErr := conn.Close(); closeErr != nil {
					log.Printf("harmonik comms recv --follow: close connection after decode error: %v", closeErr)
				}
				if sigCtx.Err() != nil {
					return 0 // clean signal exit
				}
				if errors.Is(decErr, io.EOF) || strings.Contains(decErr.Error(), "use of closed") {
					fmt.Fprintf(os.Stderr, "harmonik comms recv --follow: connection dropped, reconnecting in %v...\n", backoff)
					select {
					case <-time.After(backoff):
					case <-sigCtx.Done():
						return 0
					}
					if backoff < commsFollowReconnectMaxBackoff {
						backoff *= 2
						if backoff > commsFollowReconnectMaxBackoff {
							backoff = commsFollowReconnectMaxBackoff
						}
					}
					reconnect = true
					break
				}
				fmt.Fprintf(os.Stderr, "harmonik comms recv --follow: decode event: %v\n", decErr)
				return 1
			}

			if subscribeRefused(env.Ok) {
				close(connCloseOnce)
				if closeErr := conn.Close(); closeErr != nil {
					log.Printf("harmonik comms recv --follow: close connection after server error: %v", closeErr)
				}
				fmt.Fprintf(os.Stderr, "harmonik comms recv --follow: server error: %s\n", env.Error)
				return 1
			}

			if env.Type == "heartbeat" && env.LastEventID != "" {
				if lastSeen == "" || env.LastEventID > lastSeen {
					lastSeen = env.LastEventID
				}
			}

			if env.Type != "agent_message" {
				continue
			}

			if env.EventID != "" {
				lastSeen = env.EventID
			}

			var p struct {
				From      string `json:"from"`
				To        string `json:"to"`
				Topic     string `json:"topic,omitempty"`
				Body      string `json:"body"`
				InReplyTo string `json:"in_reply_to,omitempty"`
			}
			if decErr := json.Unmarshal(env.Payload, &p); decErr != nil {
				fmt.Fprintf(os.Stderr, "harmonik comms recv --follow: decode agent_message payload: %v\n", decErr)
				continue
			}

			if jsonOut {
				msg := struct {
					EventID   string `json:"event_id"`
					From      string `json:"from"`
					To        string `json:"to"`
					Topic     string `json:"topic,omitempty"`
					Body      string `json:"body"`
					InReplyTo string `json:"in_reply_to,omitempty"`
					Ts        string `json:"ts"`
				}{
					EventID:   env.EventID,
					From:      p.From,
					To:        p.To,
					Topic:     p.Topic,
					Body:      p.Body,
					InReplyTo: p.InReplyTo,
					Ts:        env.TimestampWall,
				}
				line, marshalErr := json.Marshal(msg)
				if marshalErr != nil {
					close(connCloseOnce)
					if closeErr := conn.Close(); closeErr != nil {
						log.Printf("harmonik comms recv --follow: close connection after marshal failure: %v", closeErr)
					}
					fmt.Fprintf(os.Stderr, "harmonik comms recv --follow: marshal message: %v\n", marshalErr)
					return 1
				}
				if _, writeErr := fmt.Fprintln(w, string(line)); writeErr != nil {
					close(connCloseOnce)
					if closeErr := conn.Close(); closeErr != nil {
						log.Printf("harmonik comms recv --follow: close connection after write failure: %v", closeErr)
					}
					return 1
				}
			} else {
				ts := env.TimestampWall
				direction := fmt.Sprintf("%s → %s", p.From, p.To)
				if p.Topic != "" {
					if _, writeErr := fmt.Fprintf(w, "%s  %-30s  [%s]  %s\n", ts, direction, p.Topic, p.Body); writeErr != nil {
						close(connCloseOnce)
						if closeErr := conn.Close(); closeErr != nil {
							log.Printf("harmonik comms recv --follow: close connection after write failure: %v", closeErr)
						}
						return 1
					}
				} else {
					if _, writeErr := fmt.Fprintf(w, "%s  %-30s  %s\n", ts, direction, p.Body); writeErr != nil {
						close(connCloseOnce)
						if closeErr := conn.Close(); closeErr != nil {
							log.Printf("harmonik comms recv --follow: close connection after write failure: %v", closeErr)
						}
						return 1
					}
				}
			}

			if p.Topic == "park" && p.From == "daemon" {
				close(connCloseOnce)
				if closeErr := conn.Close(); closeErr != nil {
					log.Printf("harmonik comms recv --follow: close connection after park message: %v", closeErr)
				}
				return 0
			}
		}

		if !reconnect {
			return 0
		}
	}
}

func printCommsRecvMsg(jsonOut bool, eventID, from, to, topic, body, inReplyTo, ts string) {
	if jsonOut {
		msg := struct {
			EventID   string `json:"event_id"`
			From      string `json:"from"`
			To        string `json:"to"`
			Topic     string `json:"topic,omitempty"`
			Body      string `json:"body"`
			InReplyTo string `json:"in_reply_to,omitempty"`
			Ts        string `json:"ts"`
		}{
			EventID:   eventID,
			From:      from,
			To:        to,
			Topic:     topic,
			Body:      body,
			InReplyTo: inReplyTo,
			Ts:        ts,
		}
		line, marshalErr := json.Marshal(msg)
		if marshalErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik comms recv: marshal message: %v\n", marshalErr)
			return
		}
		fmt.Println(string(line))
		return
	}
	direction := fmt.Sprintf("%s → %s", from, to)
	if topic != "" {
		fmt.Printf("%s  %-30s  [%s]  %s\n", ts, direction, topic, body)
	} else {
		fmt.Printf("%s  %-30s  %s\n", ts, direction, body)
	}
}

func runCommsRecvWait(sockPath, agent, fromFilter, topicFilter, sinceEventID string, jsonOut bool, timeout time.Duration) int {
	reqBody := map[string]any{
		"op":                "subscribe",
		"heartbeat_seconds": 60,
		"types":             []string{"agent_message"},
		"to":                agent,
	}
	if fromFilter != "" {
		reqBody["from"] = fromFilter
	}
	if topicFilter != "" {
		reqBody["topic"] = topicFilter
	}
	if sinceEventID != "" {
		reqBody["since_event_id"] = sinceEventID
	}
	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms recv --wait: marshal subscribe request: %v\n", err)
		return 1
	}

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 5*time.Second)
	conn, dialErr := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	cancelDial()
	if dialErr != nil {
		if commsIsSocketAbsent(dialErr) || commsIsConnRefused(dialErr) {
			fmt.Fprintf(os.Stderr, "harmonik comms recv --wait: daemon not running (socket %s missing or refused)\n", sockPath)
			return 17
		}
		fmt.Fprintf(os.Stderr, "harmonik comms recv --wait: dial %s: %v\n", sockPath, dialErr)
		return 1
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("harmonik comms recv --wait: close connection: %v", closeErr)
		}
	}()

	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms recv --wait: write subscribe request: %v\n", writeErr)
		return 1
	}

	baseCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	waitCtx := baseCtx
	if timeout > 0 {
		var cancelTimeout context.CancelFunc
		waitCtx, cancelTimeout = context.WithTimeout(baseCtx, timeout)
		defer cancelTimeout()
	}
	go func() {
		<-waitCtx.Done()
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("harmonik comms recv --wait: close connection on signal: %v", closeErr)
		}
	}()

	dec := json.NewDecoder(conn)
	for {
		var env struct {
			Type          string          `json:"type"`
			EventID       string          `json:"event_id"`
			TimestampWall string          `json:"timestamp_wall"`
			Payload       json.RawMessage `json:"payload"`
			// hk-62r8w: SocketResponse fields for server error detection.
			Ok    *bool  `json:"ok"`
			Error string `json:"error"`
		}
		if decErr := dec.Decode(&env); decErr != nil {
			if errors.Is(decErr, io.EOF) || strings.Contains(decErr.Error(), "use of closed") {
				if timeout > 0 && waitCtx.Err() == context.DeadlineExceeded {
					fmt.Fprintf(os.Stderr, "harmonik comms recv --wait: timed out after %s with no message\n", timeout)
					return commsRecvWaitTimeoutExit
				}
				return 0
			}
			fmt.Fprintf(os.Stderr, "harmonik comms recv --wait: decode event: %v\n", decErr)
			return 1
		}

		if subscribeRefused(env.Ok) {
			fmt.Fprintf(os.Stderr, "harmonik comms recv --wait: server error: %s\n", env.Error)
			return 1
		}

		if env.Type != "agent_message" {
			continue
		}

		var p struct {
			From      string `json:"from"`
			To        string `json:"to"`
			Topic     string `json:"topic,omitempty"`
			Body      string `json:"body"`
			InReplyTo string `json:"in_reply_to,omitempty"`
		}
		if decErr := json.Unmarshal(env.Payload, &p); decErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik comms recv --wait: decode agent_message payload: %v\n", decErr)
			continue
		}

		printCommsRecvMsg(jsonOut, env.EventID, p.From, p.To, p.Topic, p.Body, p.InReplyTo, env.TimestampWall)
		return 0
	}
}

const commsRecvWaitTimeoutExit = 3

func commsRecvUsage() {
	fmt.Print(`harmonik comms recv — receive unread agent_messages from the durable cursor

USAGE
  harmonik comms recv [--agent NAME] [--from NAME] [--topic T] [--follow] [--json] [--socket PATH] [--project DIR]

Sends a comms-recv op to the daemon. The daemon reads unread agent_message events
from the agent's durable cursor, advances the cursor (at-least-once delivery: N3),
and returns the matched messages. Recipients should deduplicate on event_id.

Without --follow / --wait: drains the backlog once and exits.

With --follow: drains the backlog, then streams live agent_message events via the
subscribe transport. The subscribe connection is anchored at the cursor position
returned by the drain (cursor_after), so no messages are missed between the drain
and the live tail. The daemon advances the agent's durable cursor as it delivers
each live message, so a watcher restart does NOT replay already-delivered
messages. Streams until SIGINT/SIGTERM.

With --wait: blocks until exactly ONE matching message arrives, prints it, advances
the durable cursor, and exits 0. With --timeout <dur> (e.g. 30s) it exits 3 if no
matching message arrives in time. A clean block-for-one primitive (vs --follow).

FLAGS
  --agent NAME    Agent identity (default: $HARMONIK_AGENT env var). Required.
  --from NAME     Filter: only messages from NAME.
  --topic T       Filter: only messages with topic T.
  --follow        After draining the backlog, tail live messages (streams until signal).
  --wait          Block until exactly one matching message, deliver it, advance the
                  cursor, and exit. Mutually exclusive with --follow.
  --timeout DUR   With --wait: exit 3 if no message arrives within DUR (e.g. 30s).
  --json          Emit one JSON object per message (NDJSON) instead of human-readable output.
  --socket PATH   Override socket path (default: <project>/.harmonik/daemon.sock).
  --project DIR   Project directory (default: cwd).

EXIT CODES
  0   Success (zero or more messages printed; --follow exits on signal; --wait got one)
  1   Argument error or daemon rejected the op
  3   --wait --timeout elapsed with no matching message
  17  Daemon not running (socket missing or ECONNREFUSED)

EXAMPLES
  harmonik comms recv --agent myagent
  harmonik comms recv --agent myagent --follow
  harmonik comms recv --agent myagent --wait
  harmonik comms recv --agent myagent --wait --timeout 30s
  harmonik comms recv --agent myagent --from orchestrator
  harmonik comms recv --agent myagent --topic status --json
  harmonik comms recv --agent myagent --follow --json
  harmonik comms recv                    # uses $HARMONIK_AGENT
`)
}
