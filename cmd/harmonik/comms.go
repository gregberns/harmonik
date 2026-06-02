package main

// comms.go — `harmonik comms` CLI subcommand block (agent-comms spec §2.1 C2, bead hk-cnjhx T3).
//
// Routes `harmonik comms <verb>` to the appropriate handler. Currently
// implements `send` (C2 CLI half); further verbs (recv, log, who, join, leave)
// land in subsequent tasks (T5, T8–T11).
//
// Flag reference for `harmonik comms send`:
//
//	(--to NAME | --broadcast)  Directed recipient OR broadcast sentinel "*". Exactly one required.
//	--from NAME                Sender identity (default: $HARMONIK_AGENT env var).
//	--topic T                  Optional free-text filter key.
//	--reply-to ID              Optional event_id of the message being replied to.
//	--socket PATH              Override socket path (default: <project>/.harmonik/daemon.sock).
//	--project DIR              Project directory (default: cwd).
//	--                         End of flags; remaining args are the message body.
//	<body> | -                 Message body as trailing args joined by space, or "-" to read stdin.
//
// Exit codes:
//
//	0   Success (event_id printed to stdout)
//	1   Argument error or op rejected by daemon
//	17  Daemon not running (socket missing or ECONNREFUSED)
//
// Spec ref: ~/.kerf/projects/gregberns-harmonik/agent-comms/05-spec-draft.md §2.1.
// Bead ref: hk-cnjhx (T3).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
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
	default:
		fmt.Fprintf(os.Stderr, "harmonik comms: unrecognised verb %q; verbs are: send\n", verb)
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

	// Resolve socket path.
	sockPath := socketFlag
	if sockPath == "" {
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
		sockPath = filepath.Join(absProject, ".harmonik", "daemon.sock")
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
	return 0
}

// commsIsSocketAbsent reports whether err indicates a missing socket file.
func commsIsSocketAbsent(err error) bool {
	var sysErr *os.PathError
	if errors.As(err, &sysErr) {
		return errors.Is(sysErr.Err, syscall.ENOENT)
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

EXIT CODES
  0   Success
  1   Argument error or op rejected
  2   Unrecognised verb
  17  Daemon not running

EXAMPLES
  harmonik comms send --to other-agent -- Hello
  harmonik comms send --broadcast --from myagent -- Status update
`)
}

func commsSendUsage() {
	fmt.Print(`harmonik comms send — send an agent_message via the daemon

USAGE
  harmonik comms send (--to NAME | --broadcast) [--from NAME] [--topic T] [--reply-to ID] [flags] [--] <body>

FLAGS
  --to NAME       Directed recipient agent name. Mutually exclusive with --broadcast.
  --broadcast     Broadcast to all agents (sets to:"*"). Mutually exclusive with --to.
  --from NAME     Sender identity (default: $HARMONIK_AGENT env var). Required.
  --topic T       Optional free-text filter key.
  --reply-to ID   Optional event_id of the message being replied to (threading hint).
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
  harmonik comms send --broadcast --from orchestrator -- Batch complete
  echo "body text" | harmonik comms send --to alice --from me -
`)
}
