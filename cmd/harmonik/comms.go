package main

// comms.go — `harmonik comms` CLI subcommand block (agent-comms spec §2.1 C2/C3/C6).
//
// Routes `harmonik comms <verb>` to the appropriate handler. Currently implements:
//   - send  (C2 CLI half; bead hk-cnjhx T3)
//   - log   (C3 read-only operator view; bead hk-onn1x T5)
//   - join  (C6 presence join; bead hk-7t27s T10)
//   - leave (C6 presence leave; bead hk-7t27s T10)
//   - who   (C3/C6 presence view; bead hk-ofxd0 T11)
//   - recv  (C2/C5 durable recv; bead hk-nnwaa T8)
//
// Flag reference for `harmonik comms send`:
//
//	(--to NAME | --broadcast)  Directed recipient OR broadcast sentinel "*". Exactly one required.
//	--from NAME                Sender identity (default: $HARMONIK_AGENT env var).
//	--topic T                  Optional free-text filter key.
//	--reply-to ID              Optional event_id of the message being replied to.
//	--wake                     After sending, nudge the recipient's tmux pane to wake an idle crew.
//	--socket PATH              Override socket path (default: <project>/.harmonik/daemon.sock).
//	--project DIR              Project directory (default: cwd).
//	--                         End of flags; remaining args are the message body.
//	<body> | -                 Message body as trailing args joined by space, or "-" to read stdin.
//
// Flag reference for `harmonik comms log`:
//
//	--since EVENT_ID|DURATION  Scan after the given event_id, or within the last DURATION (e.g. 30m).
//	--to NAME                  Filter: only messages directed to NAME (or broadcast).
//	--from NAME                Filter: only messages from NAME.
//	--topic T                  Filter: only messages with topic T.
//	--json                     Emit one JSON object per matched event (NDJSON).
//	--project DIR              Project directory (default: cwd).
//
// Flag reference for `harmonik comms join` and `harmonik comms leave`:
//
//	--name NAME                Agent identity (default: $HARMONIK_AGENT env var).
//	--socket PATH              Override socket path (default: <project>/.harmonik/daemon.sock).
//	--project DIR              Project directory (default: cwd).
//
// Flag reference for `harmonik comms who`:
//
//	--json                     Emit one JSON object per online agent (NDJSON).
//	--project DIR              Project directory (default: cwd).
//
// Flag reference for `harmonik comms recv`:
//
//	--agent NAME               Agent identity (default: $HARMONIK_AGENT env var).
//	--from NAME                Filter: only messages from NAME.
//	--topic T                  Filter: only messages with topic T.
//	--follow                   After draining the backlog, tail live messages via subscribe (streams until signal).
//	--wait                     Block until exactly one matching message, deliver it, advance the cursor, exit (hk-tafd4).
//	--timeout DUR              With --wait: exit 3 if no message arrives within DUR (e.g. 30s).
//	--json                     Emit one JSON object per message (NDJSON) instead of human-readable.
//	--socket PATH              Override socket path (default: <project>/.harmonik/daemon.sock).
//	--project DIR              Project directory (default: cwd).
//
// Exit codes:
//
//	0   Success
//	1   Argument error or read failure
//	17  Daemon not running (send/join/leave/recv — socket missing or ECONNREFUSED)
//
// Spec ref: ~/.kerf/projects/gregberns-harmonik/agent-comms/05-spec-draft.md §2.1, §2.3, §2.4, §2.5, §4.
// Bead ref: hk-cnjhx (T3), hk-onn1x (T5), hk-7t27s (T10), hk-ofxd0 (T11).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/presence"
)

// runCommsSubcommand routes `harmonik comms <verb> [args]`.
// subArgs is os.Args[2:].
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

// runCommsSendSubcommand implements `harmonik comms send`.
// subArgs is os.Args[3:].
func runCommsSendSubcommand(subArgs []string) int {
	toFlag := ""
	broadcastFlag := false
	fromFlag := ""
	topicFlag := ""
	replyToFlag := ""
	wakeFlag := false
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

	// Validate: exactly one of --to / --broadcast.
	if toFlag != "" && broadcastFlag {
		fmt.Fprintf(os.Stderr, "harmonik comms send: --to and --broadcast are mutually exclusive\n")
		return 1
	}
	if toFlag == "" && !broadcastFlag {
		fmt.Fprintf(os.Stderr, "harmonik comms send: one of --to NAME or --broadcast is required\n")
		return 1
	}
	// --wake requires a directed recipient (not broadcast).
	if wakeFlag && broadcastFlag {
		fmt.Fprintf(os.Stderr, "harmonik comms send: --wake requires --to (cannot wake a broadcast)\n")
		return 1
	}

	// Resolve recipient: broadcast maps to "*".
	to := toFlag
	if broadcastFlag {
		to = "*"
	}

	// Resolve sender identity: --from > $HARMONIK_AGENT.
	from := fromFlag
	if from == "" {
		from = os.Getenv("HARMONIK_AGENT")
	}
	if from == "" {
		fmt.Fprintf(os.Stderr, "harmonik comms send: --from is required (or set $HARMONIK_AGENT)\n")
		return 1
	}

	// Resolve body.
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

	// Resolve project directory and socket path.
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

	// Two-captains conflict detection (hk-z0f02): warn when sending as a name that
	// is already online under a different session.
	sessionID := resolveSessionID()
	if sessionID != "" {
		eventsPath := filepath.Join(absProject, ".harmonik", "events", "events.jsonl")
		if warn := checkCommsNameConflict(eventsPath, from, sessionID); warn != "" {
			fmt.Fprintf(os.Stderr, "harmonik comms send: WARNING: %s\n", warn)
		}
	}

	// Build the CommsSendRequest payload.
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

	// Wrap in a SocketRequest envelope.
	reqBytes, err := json.Marshal(map[string]any{
		"op":      "comms-send",
		"payload": json.RawMessage(payloadBytes),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms send: marshal request: %v\n", err)
		return 1
	}

	// Dial, send, read response.
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
	defer func() { _ = conn.Close() }()

	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms send: write request: %v\n", writeErr)
		return 1
	}
	// Signal end of write so the daemon's decoder sees EOF on its read side.
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite()
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

	// Extract event_id from the CommsSendResult.
	var result struct {
		EventID string `json:"event_id"`
	}
	if unmarshalErr := json.Unmarshal(resp.Result, &result); unmarshalErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms send: decode result: %v\n", unmarshalErr)
		return 1
	}

	fmt.Println(result.EventID)

	// --wake: nudge the recipient's tmux pane after the message is delivered.
	// Best-effort: a wake failure does not affect the exit code.
	if wakeFlag && to != "*" {
		if wakeErr := commsWakePaneForAgent(context.Background(), absProject, to); wakeErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik comms send: --wake: %v\n", wakeErr)
		}
	}

	return 0
}

// commsWakePaneForAgent nudges the tmux pane for the named crew agent so that
// an idle Claude session wakes and processes the newly-delivered message.
//
// Resolution order for the pane target:
//  1. crew registry: loads .harmonik/crew/<agentName>.json and uses Handle+".0"
//     (independent crew session pane, format "<session>:<window>.0").
//  2. convention fallback: "harmonik-<projectHash>-crew-<agentName>" (session name,
//     targets first pane). Fleet-portability T2: project-qualified crew session name.
//
// Best-effort: errors are returned but the caller treats them as non-fatal so that
// message delivery is not affected by wake failures (e.g. no tmux running).
//
// Bead ref: hk-37ra4.
func commsWakePaneForAgent(ctx context.Context, projectDir, agentName string) error {
	var paneTarget string
	rec, loadErr := crew.Load(projectDir, agentName)
	if loadErr == nil && rec.Handle != "" {
		// handle format: "<session>:<window>" → pane = handle + ".0"
		paneTarget = rec.Handle + ".0"
	} else {
		// Fall back to the deterministic project-qualified crew session name (T2).
		paneTarget = lifecycle.TmuxSessionName(lifecycle.ComputeProjectHash(projectDir), "crew-"+agentName)
	}
	nudgeMsg := "You have a new comms message. Please check your inbox."
	return commsInjectTmuxPane(ctx, paneTarget, nudgeMsg)
}

// commsInjectTmuxPane delivers text into a tmux pane via the bracketed-paste
// mechanism (tmux load-buffer → paste-buffer → send-keys Enter), the same
// approach used by keeper.InjectText. The named buffer "hk-comms-wake" is
// overwritten on each call; it is not shared with the daemon's own paste-inject
// buffers (which use "hk-<run_id>" names).
//
// Returns an error if any tmux invocation fails (e.g. pane does not exist,
// tmux not running). Callers treat this error as non-fatal.
func commsInjectTmuxPane(ctx context.Context, paneTarget, text string) error {
	const buf = "hk-comms-wake"

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

// commsIsSocketAbsent reports whether err indicates a missing socket file.
// On Linux connect(2) to a missing unix socket returns ENOENT.
// On macOS connect(2) to a missing unix socket returns EINVAL
// (the kernel rejects the path because no socket file exists there).
// Both are handled via errors.Is traversal over the full error chain.
func commsIsSocketAbsent(err error) bool {
	if errors.Is(err, syscall.ENOENT) {
		return true
	}
	// EINVAL on a unix-domain connect means the path does not exist as a
	// socket on macOS (connect(2) returns EINVAL when the socket file is absent).
	if errors.Is(err, syscall.EINVAL) {
		return true
	}
	return strings.Contains(err.Error(), "no such file or directory")
}

// commsIsConnRefused reports whether err indicates ECONNREFUSED.
func commsIsConnRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) ||
		strings.Contains(err.Error(), "connection refused")
}

func commsUsage() {
	fmt.Print(`harmonik comms — agent-to-agent messaging surface

USAGE
  harmonik comms <verb> [flags]

VERBS
  send    Send an agent_message to a named agent or broadcast to all
  recv    Receive unread agent_messages from the durable cursor (daemon required)
  log     Read-only operator view of recent agent_message events (no daemon needed)
  join    Emit an agent_presence{online, reason:"join"} beat (presence registry)
  leave   Emit an agent_presence{offline, reason:"leave"} beat (presence registry)
  who     List currently-online agents from the presence registry (no daemon needed)

EXIT CODES
  0   Success
  1   Argument error or op rejected
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
  harmonik comms leave --name myagent
  harmonik comms who
  harmonik comms who --json
`)
}

func commsSendUsage() {
	fmt.Print(`harmonik comms send — send an agent_message via the daemon

USAGE
  harmonik comms send (--to NAME | --broadcast) [--from NAME] [--topic T] [--reply-to ID] [--wake] [flags] [--] <body>

FLAGS
  --to NAME       Directed recipient agent name. Mutually exclusive with --broadcast.
  --broadcast     Broadcast to all agents (sets to:"*"). Mutually exclusive with --to.
  --from NAME     Sender identity (default: $HARMONIK_AGENT env var). Required.
  --topic T       Optional free-text filter key.
  --reply-to ID   Optional event_id of the message being replied to (threading hint).
  --wake          After sending, nudge the recipient's tmux pane to wake an idle crew
                  member. Requires --to (not --broadcast). The pane target is resolved
                  from the crew registry handle, falling back to "harmonik-<hash>-crew-<name>".
                  Best-effort: wake failures are reported to stderr but do not affect
                  the exit code.
  --socket PATH   Override socket path (default: <project>/.harmonik/daemon.sock).
  --project DIR   Project directory (default: cwd).
  --              End of flags; remaining args form the body.
  <body> | -      Message body as trailing args (joined by space) or "-" to read stdin.

EXIT CODES
  0   Success (event_id printed to stdout)
  1   Argument error or daemon rejected the op
  17  Daemon not running (socket missing or ECONNREFUSED)

EXAMPLES
  harmonik comms send --to alice -- Hello from bob
  harmonik comms send --to alice --from bob --topic status -- ready
  harmonik comms send --to alice --wake -- You have work to do
  harmonik comms send --broadcast --from orchestrator -- Batch complete
  echo "body text" | harmonik comms send --to alice --from me -
`)
}

// runCommsLogSubcommand implements `harmonik comms log` (agent-comms spec §2.4, bead hk-onn1x T5).
//
// Scans events.jsonl for ALL agent_message events ordered by event_id (file order).
// Does NOT advance any agent cursor — pure read-only operator view.
// No daemon connection required.
//
// --since accepts either:
//   - a UUIDv7 event_id string (scan after that event)
//   - a Go duration string (e.g. "30m", "1h") — delivers events whose TimestampWall
//     is at or after now minus the given duration
//
// subArgs is os.Args[3:].
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

	// Resolve project directory and events.jsonl path.
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

	// Parse --since: try as event_id UUID first, then as a duration.
	var sinceID core.EventID // zero value = scan from beginning
	var wallCutoff time.Time // zero = no wall-time filter
	if sinceFlag != "" {
		if err := sinceID.UnmarshalText([]byte(sinceFlag)); err == nil {
			// Parsed as event_id — sinceID is set; ScanAfter will skip events ≤ sinceID.
		} else {
			// Try as a duration.
			dur, durErr := time.ParseDuration(sinceFlag)
			if durErr != nil {
				fmt.Fprintf(os.Stderr, "harmonik comms log: --since %q is not a valid event_id or duration: %v\n", sinceFlag, durErr)
				return 1
			}
			wallCutoff = time.Now().Add(-dur)
		}
	}

	// Scan events.jsonl, filter for agent_message, apply addressing filters.
	count := 0
	for ev := range eventbus.ScanAfter(eventsPath, sinceID) {
		if ev.Type != "agent_message" {
			continue
		}

		// Apply wall-time cutoff for duration-based --since.
		if !wallCutoff.IsZero() && ev.TimestampWall.Before(wallCutoff) {
			continue
		}

		// Decode payload to apply addressing filters.
		var p core.AgentMessagePayload
		if decErr := json.Unmarshal(ev.Payload, &p); decErr != nil {
			// Malformed payload — skip with warning.
			fmt.Fprintf(os.Stderr, "harmonik comms log: malformed agent_message payload (event_id=%s): %v\n", ev.EventID, decErr)
			continue
		}

		// Apply --from filter.
		if fromFlag != "" && p.From != fromFlag {
			continue
		}
		// Apply --to filter: match directed-to-name or broadcast "*".
		if toFlag != "" && p.To != toFlag && p.To != "*" {
			continue
		}
		// Apply --topic filter.
		if topicFlag != "" && p.Topic != topicFlag {
			continue
		}

		count++
		if jsonFlag {
			// Emit the full event envelope as NDJSON.
			line, marshalErr := json.Marshal(ev)
			if marshalErr != nil {
				fmt.Fprintf(os.Stderr, "harmonik comms log: marshal event: %v\n", marshalErr)
				return 1
			}
			fmt.Println(string(line))
		} else {
			// Human-readable: timestamp  from → to  [topic]  body
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

FLAGS
  --since EVENT_ID|DURATION
                  Start from: an event_id (scan after that event) OR a duration (e.g. 30m, 1h,
                  12h) meaning "events in the last <duration>". Without --since, scans all events.
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
  harmonik comms log --since 1h --to alice    # last hour, directed to alice or broadcast
  harmonik comms log --from orchestrator      # all messages from orchestrator
  harmonik comms log --json                   # machine-readable NDJSON
`)
}

// Presence projection — CANONICAL HOME MOVED to internal/presence (hitl-decisions
// K5 lift, bead hk-061). The agent-presence registry projection (T10) used to live
// here in package main; it was lifted into the leaf package internal/presence so
// the session-keeper orphan reaper (which MUST NOT import internal/daemon and could
// not import package main at all) can compute the same Offline predicate. The
// symbols below are thin aliases keeping the package-main call sites (comms who,
// two-captains conflict detection, decisions_k4.go orphaned-pending flag) and their
// tests unchanged — the behaviour is byte-for-byte identical (same logic, same
// constants), it just lives in one shared place now.
//
// Spec ref: agent-comms spec §4 (presence registry projection); hitl-decisions
// SPEC §5 / N9 (the K5 reaper reuses this same Offline determination).
// Bead refs: hk-7t27s (T10, original), hk-6vwi3 (fix #1), hk-061 (this lift).

// presenceTTL / presenceStaleCutoff alias the canonical windows in
// internal/presence (TTL=120s, StaleCutoff=10m).
const (
	presenceTTL         = presence.TTL
	presenceStaleCutoff = presence.StaleCutoff
)

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

// resolveSessionID returns the per-session opaque token for two-captains conflict
// detection (hk-z0f02).
//
// Resolution order:
//  1. $HARMONIK_SESSION_ID — explicit operator-set token (interactive captain sessions).
//  2. $HARMONIK_RUN_ID    — injected by the daemon per dispatched run
//     (claudehandler_chb006_024.go:259); unique per run, covers daemon-dispatched agents
//     such as crew members that might mis-claim a name.
//
// Returns empty string when neither variable is set, which disables conflict
// detection gracefully (no false positives for sessions without a token).
func resolveSessionID() string {
	if id := os.Getenv("HARMONIK_SESSION_ID"); id != "" {
		return id
	}
	return os.Getenv("HARMONIK_RUN_ID")
}

// checkCommsNameConflict returns a non-empty warning string when eventsPath
// records that name is currently online with a different session_id than sessionID.
// Returns empty string (no warning) when:
//   - sessionID is empty (conflict detection requires a session token)
//   - name has no presence entry
//   - name is offline or stale
//   - name's last known session_id is empty or matches sessionID
//
// Reads events.jsonl directly; no daemon connection required.
// Bead ref: hk-z0f02.
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

// runCommsPresenceSubcommand implements `harmonik comms join` and `harmonik comms leave`.
// verb is "join" or "leave". subArgs is os.Args[3:].
func runCommsPresenceSubcommand(subArgs []string, verb string) int {
	nameFlag := ""
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

	// Resolve agent name: --name > $HARMONIK_AGENT.
	name := nameFlag
	if name == "" {
		name = os.Getenv("HARMONIK_AGENT")
	}
	if name == "" {
		fmt.Fprintf(os.Stderr, "harmonik comms %s: --name is required (or set $HARMONIK_AGENT)\n", verb)
		return 1
	}

	// Map verb → (status, reason).
	status := "online"
	reason := "join"
	if verb == "leave" {
		status = "offline"
		reason = "leave"
	}

	// Resolve socket path and project directory.
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

	// Two-captains conflict detection (hk-z0f02): warn when joining as a name that
	// is already online under a different session. Only fires for "join" (not leave).
	sessionID := resolveSessionID()
	if verb == "join" {
		eventsPath := filepath.Join(absProject, ".harmonik", "events", "events.jsonl")
		if warn := checkCommsNameConflict(eventsPath, name, sessionID); warn != "" {
			fmt.Fprintf(os.Stderr, "harmonik comms join: WARNING: %s\n", warn)
		}
	}

	// Build the comms-presence request payload.
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

	// Wrap in a SocketRequest envelope.
	reqBytes, err := json.Marshal(map[string]any{
		"op":      "comms-presence",
		"payload": json.RawMessage(payloadBytes),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms %s: marshal request: %v\n", verb, err)
		return 1
	}

	// Dial, send, read response.
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
	defer func() { _ = conn.Close() }()

	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms %s: write request: %v\n", verb, writeErr)
		return 1
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite()
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

	// Extract event_id from the CommsPresenceResult.
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

// runCommsWhoSubcommand implements `harmonik comms who` (agent-comms spec §2.3, bead hk-ofxd0 T11).
//
// Reads the presence projection (T10's ComputePresenceRegistry) and prints agents
// that are online within the 120s staleness window (presenceTTL). Read-only; emits
// nothing, advances no cursor. No daemon connection required.
//
// subArgs is os.Args[3:].
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

	// Resolve project directory and events.jsonl path.
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

	registry := ComputePresenceRegistry(eventsPath)

	// Collect online and stale agents in deterministic order (sorted by name).
	// Stale agents (presenceTTL..presenceStaleCutoff) are included with a
	// degraded annotation; offline agents (>presenceStaleCutoff or leave beat) are omitted.
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
	// Sort by agent name for stable output.
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
	fmt.Printf(`harmonik comms %s — emit an agent_presence beat (presence registry)

USAGE
  harmonik comms %s [--name NAME] [--socket PATH] [--project DIR]

Sends a comms-presence op to the daemon, which emits an agent_presence event
with status="%s" and reason="%s". The minted event_id is printed to stdout.

FLAGS
  --name NAME     Agent identity (default: $HARMONIK_AGENT env var). Required.
  --socket PATH   Override socket path (default: <project>/.harmonik/daemon.sock).
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
	}(), verb, verb, verb)
}

// runCommsRecvSubcommand implements `harmonik comms recv [--follow]` (agent-comms
// spec §2.2 C2/C5/C3, beads hk-nnwaa T8 and hk-oqmrs T9).
//
// Without --follow: sends one comms-recv op to the daemon, drains the backlog
// for the agent's durable cursor, prints matched messages, and exits.
//
// With --follow: drains the backlog via comms-recv (same as above), then
// opens a subscribe connection anchored at cursor_after (the position returned
// by the comms-recv op) and streams live agent_message events until a signal.
// The subscribe server registers for live events BEFORE replaying the gap
// (subscribe.go:304), so no messages are dropped between the drain and the
// live tail.
//
// subArgs is os.Args[3:].
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

	// Resolve agent name: --agent > $HARMONIK_AGENT.
	agent := agentFlag
	if agent == "" {
		agent = os.Getenv("HARMONIK_AGENT")
	}
	if agent == "" {
		fmt.Fprintf(os.Stderr, "harmonik comms recv: --agent is required (or set $HARMONIK_AGENT)\n")
		return 1
	}

	// --follow and --wait are mutually exclusive: --follow streams indefinitely,
	// --wait blocks for exactly one message then exits (hk-tafd4).
	if followFlag && waitFlag {
		fmt.Fprintf(os.Stderr, "harmonik comms recv: --follow and --wait are mutually exclusive\n")
		return 1
	}
	if timeoutFlag != 0 && !waitFlag {
		fmt.Fprintf(os.Stderr, "harmonik comms recv: --timeout requires --wait\n")
		return 1
	}

	// Resolve socket path.
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

	// Build the CommsRecvRequest payload.
	recvPayload := map[string]any{
		"agent": agent,
	}
	if fromFlag != "" {
		recvPayload["from"] = fromFlag
	}
	if topicFlag != "" {
		recvPayload["topic"] = topicFlag
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

	// Dial, send, read response.
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
	defer func() { _ = conn.Close() }()

	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms recv: write request: %v\n", writeErr)
		return 1
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite()
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

	// Decode CommsRecvResult.
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
	}
	if unmarshalErr := json.Unmarshal(resp.Result, &result); unmarshalErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms recv: decode result: %v\n", unmarshalErr)
		return 1
	}

	// --wait: block until exactly ONE matching message, then exit. If the drain
	// already produced messages, the first one IS that message (cursor already
	// advanced by the one-shot comms-recv op) — print it and exit 0 (hk-tafd4).
	if waitFlag {
		if len(result.Messages) > 0 {
			m := result.Messages[0]
			printCommsRecvMsg(jsonFlag, m.EventID, m.From, m.To, m.Topic, m.Body, m.InReplyTo, m.Ts)
			return 0
		}
		// Backlog empty — block on a subscribe stream for exactly one message.
		return runCommsRecvWait(sockPath, agent, fromFlag, topicFlag, result.CursorAfter, jsonFlag, timeoutFlag)
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

	// --follow: open a subscribe connection anchored at cursor_after to tail
	// live agent_message events with no gap.
	//
	// The subscribe server registers for live events BEFORE replaying the gap
	// (subscribe.go:304), so any agent_message events that arrived between the
	// comms-recv call and this subscribe dial are covered by the replay path.
	return runCommsRecvFollow(sockPath, agent, fromFlag, topicFlag, result.CursorAfter, jsonFlag)
}

// commsFollowReconnectInitialBackoff is the starting reconnect delay after a
// dropped subscribe connection (daemon restart or transient disconnect).
// Doubles on each attempt up to commsFollowReconnectMaxBackoff.
const commsFollowReconnectInitialBackoff = time.Second

// commsFollowReconnectMaxBackoff caps the reconnect delay so an agent is never
// more than ~10 s away from picking up the live stream after a daemon revive.
// This is the primary lever against the F12 false-STALLED / stale-ceiling class
// of misreads (logmine finding F12, bead hk-5xuvc).
const commsFollowReconnectMaxBackoff = 10 * time.Second

// runCommsRecvFollow opens a subscribe connection for live agent_message events
// anchored at sinceEventID (the cursor_after from the preceding comms-recv drain).
// Streams until signal or connection close.
//
// Reconnect behaviour (F12 fix, hk-5xuvc): when the subscribe connection drops
// (daemon restart or transient disconnect), the function waits a short backoff
// (1 s → 2 s → … → 10 s) and re-dials, anchoring the new subscribe at the last
// seen event_id so no messages are missed or duplicated. The loop exits only on
// SIGINT/SIGTERM. This eliminates the ~10-30 s comms dead-window that caused
// false STALLED reads and stale concurrency-ceiling beliefs.
func runCommsRecvFollow(sockPath, agent, fromFilter, topicFilter, sinceEventID string, jsonOut bool) int {
	// Register signal handler once for the lifetime of the --follow loop.
	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// lastSeen tracks the highest event_id delivered so far; it advances as
	// messages arrive and becomes the since_event_id anchor on reconnect.
	lastSeen := sinceEventID
	backoff := commsFollowReconnectInitialBackoff
	firstDial := true

	for {
		// Exit cleanly when a signal arrived while we were sleeping.
		if sigCtx.Err() != nil {
			return 0
		}

		// Build subscribe request anchored at lastSeen.
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

		// Dial — use sigCtx so the dial itself is cancelled on signal.
		dialCtx, cancelDial := context.WithTimeout(sigCtx, 5*time.Second)
		conn, dialErr := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
		cancelDial()

		if sigCtx.Err() != nil {
			return 0 // signal fired during dial
		}

		if dialErr != nil {
			if commsIsSocketAbsent(dialErr) || commsIsConnRefused(dialErr) {
				if firstDial {
					// On the very first attempt, report that the daemon is not
					// running and return exit 17 so callers that require the
					// daemon to already be up get a clear signal.
					fmt.Fprintf(os.Stderr, "harmonik comms recv --follow: daemon not running (socket %s missing or refused)\n", sockPath)
					return 17
				}
				// Subsequent attempts: daemon is restarting — wait and retry.
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

		// Reset backoff on successful connect.
		backoff = commsFollowReconnectInitialBackoff
		firstDial = false

		if _, writeErr := conn.Write(reqBytes); writeErr != nil {
			_ = conn.Close()
			fmt.Fprintf(os.Stderr, "harmonik comms recv --follow: write subscribe request: %v\n", writeErr)
			return 1
		}

		// Close conn on signal so the decode loop below exits cleanly.
		connCloseOnce := make(chan struct{})
		go func() {
			select {
			case <-sigCtx.Done():
				_ = conn.Close()
			case <-connCloseOnce:
			}
		}()

		// Stream events; update lastSeen so reconnects pick up without gaps.
		reconnect := false
		dec := json.NewDecoder(conn)
		for {
			var env struct {
				Type          string          `json:"type"`
				EventID       string          `json:"event_id"`
				LastEventID   string          `json:"last_event_id"` // heartbeat payload field; EV-037a
				TimestampWall string          `json:"timestamp_wall"`
				Payload       json.RawMessage `json:"payload"`
			}
			if decErr := dec.Decode(&env); decErr != nil {
				close(connCloseOnce) // stop the signal-closer goroutine
				_ = conn.Close()
				if sigCtx.Err() != nil {
					return 0 // clean signal exit
				}
				if errors.Is(decErr, io.EOF) || strings.Contains(decErr.Error(), "use of closed") {
					// Connection dropped — reconnect after a short backoff.
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

			// EV-037a: advance lastSeen from heartbeat.last_event_id even when no
			// actionable message was processed — prevents watermark regression across
			// reconnects in quiet periods. max() invariant: only advance forward.
			if env.Type == "heartbeat" && env.LastEventID != "" {
				if lastSeen == "" || env.LastEventID > lastSeen {
					lastSeen = env.LastEventID
				}
			}

			// Skip non-message events.
			if env.Type != "agent_message" {
				continue
			}

			// Advance lastSeen so reconnects anchor past this message.
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
					_ = conn.Close()
					fmt.Fprintf(os.Stderr, "harmonik comms recv --follow: marshal message: %v\n", marshalErr)
					return 1
				}
				fmt.Println(string(line))
			} else {
				ts := env.TimestampWall
				direction := fmt.Sprintf("%s → %s", p.From, p.To)
				if p.Topic != "" {
					fmt.Printf("%s  %-30s  [%s]  %s\n", ts, direction, p.Topic, p.Body)
				} else {
					fmt.Printf("%s  %-30s  %s\n", ts, direction, p.Body)
				}
			}
		}

		if !reconnect {
			// Non-reconnect exit path (write error already returned above).
			return 0
		}
	}
}

// printCommsRecvMsg renders one received message to stdout in either NDJSON
// (jsonOut) or human-readable form, matching the comms-recv / --follow format.
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

// runCommsRecvWait blocks until exactly ONE matching agent_message arrives via a
// subscribe stream anchored at sinceEventID (the cursor_after from the preceding
// empty drain), prints it, and exits 0. The daemon advances the agent's durable
// cursor as it delivers the event (hk-tafd4), so the message is consumed exactly
// as a one-shot `comms recv` would consume it.
//
// With timeout > 0, the call returns commsRecvWaitTimeoutExit if no matching
// message arrives in time. timeout == 0 blocks indefinitely (until signal).
//
// Bead ref: hk-tafd4.
func runCommsRecvWait(sockPath, agent, fromFilter, topicFilter, sinceEventID string, jsonOut bool, timeout time.Duration) int {
	// Build the subscribe request (agent_message events directed to this agent).
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
	defer func() { _ = conn.Close() }()

	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik comms recv --wait: write subscribe request: %v\n", writeErr)
		return 1
	}

	// Cancel on signal or (if set) timeout — closing conn unblocks the decoder.
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
		_ = conn.Close()
	}()

	dec := json.NewDecoder(conn)
	for {
		var env struct {
			Type          string          `json:"type"`
			EventID       string          `json:"event_id"`
			TimestampWall string          `json:"timestamp_wall"`
			Payload       json.RawMessage `json:"payload"`
		}
		if decErr := dec.Decode(&env); decErr != nil {
			// Timeout / signal close manifests as an EOF or "use of closed".
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

		// Skip non-message events (heartbeats, subscription_gap, etc.).
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
		// Got our one message — close the connection so the daemon flushes the
		// cursor (defer flushCursor in HandleSubscribe), then exit 0.
		return 0
	}
}

// commsRecvWaitTimeoutExit is the exit code `comms recv --wait --timeout` returns
// when the timeout elapses with no matching message. Distinct from 1 (arg/IO
// error) and 17 (daemon down) so scripts can branch on "no message yet".
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
