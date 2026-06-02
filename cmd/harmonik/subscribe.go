package main

// subscribe.go — `harmonik subscribe` CLI subcommand (hk-6ynv4).
//
// Opens the daemon's Unix-domain socket, sends a single "subscribe" JSON
// request, and copies the NDJSON stream to stdout until EOF or signal.
// Replaces the brittle "tail .harmonik/events/events.jsonl" pattern with a
// first-class subscriber interface (operator-nfr.md §4.9 ON-055).
//
// Flag reference:
//
//	--types t1,t2,...      Comma-separated event-type filter (default: all)
//	--heartbeat <dur>      Idle heartbeat cadence (default 60s; clamped 10s..600s)
//	--since-event-id <id>  Resume cursor: replay events strictly after this event_id before delivering live stream
//	--to <name>            Agent-message addressing filter: only deliver agent_message events addressed to <name> or "*"
//	--from <name>          Agent-message addressing filter: only deliver agent_message events sent by <name>
//	--topic <topic>        Agent-message addressing filter: only deliver agent_message events with matching topic
//	--socket <path>        Override socket path (default: <project>/.harmonik/daemon.sock)
//	--project <dir>        Project directory (default: cwd)
//
// Exit codes:
//
//	0   Stream closed cleanly (EOF from daemon, signal)
//	1   Argument error or write to stdout failed
//	17  Daemon socket missing or ECONNREFUSED

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// runSubscribeSubcommand implements `harmonik subscribe [flags]`.
// subArgs is os.Args[2:].
func runSubscribeSubcommand(subArgs []string) int {
	typesFlag := ""
	heartbeatFlag := 60 * time.Second
	sinceFlag := ""
	toFlag := ""
	fromFlag := ""
	topicFlag := ""
	socketFlag := ""
	projectFlag := ""

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			subscribeUsage()
			return 0
		case arg == "--types" && i+1 < len(subArgs):
			i++
			typesFlag = subArgs[i]
		case strings.HasPrefix(arg, "--types="):
			typesFlag = strings.TrimPrefix(arg, "--types=")
		case arg == "--heartbeat" && i+1 < len(subArgs):
			i++
			d, err := time.ParseDuration(subArgs[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "harmonik subscribe: --heartbeat: %v\n", err)
				return 1
			}
			heartbeatFlag = d
		case strings.HasPrefix(arg, "--heartbeat="):
			d, err := time.ParseDuration(strings.TrimPrefix(arg, "--heartbeat="))
			if err != nil {
				fmt.Fprintf(os.Stderr, "harmonik subscribe: --heartbeat: %v\n", err)
				return 1
			}
			heartbeatFlag = d
		case arg == "--since-event-id" && i+1 < len(subArgs):
			i++
			sinceFlag = subArgs[i]
		case strings.HasPrefix(arg, "--since-event-id="):
			sinceFlag = strings.TrimPrefix(arg, "--since-event-id=")
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
		// Accept --json as a no-op alias (output is already NDJSON).
		case arg == "--json":
			// no-op
		default:
			fmt.Fprintf(os.Stderr, "harmonik subscribe: unknown argument %q\n", arg)
			return 1
		}
	}

	// Resolve socket path.
	sockPath := socketFlag
	if sockPath == "" {
		projectDir := projectFlag
		if projectDir == "" {
			wd, err := os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "harmonik subscribe: cannot determine cwd: %v\n", err)
				return 1
			}
			projectDir = wd
		}
		absProject, err := filepath.Abs(projectDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik subscribe: cannot resolve project path: %v\n", err)
			return 1
		}
		sockPath = filepath.Join(absProject, ".harmonik", "daemon.sock")
	}

	// Parse types list.
	var types []string
	if typesFlag != "" {
		for _, t := range strings.Split(typesFlag, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				types = append(types, t)
			}
		}
	}

	// Build request.
	reqBody := map[string]any{
		"op":                "subscribe",
		"heartbeat_seconds": int(heartbeatFlag.Seconds()),
	}
	if len(types) > 0 {
		reqBody["types"] = types
	}
	if sinceFlag != "" {
		reqBody["since_event_id"] = sinceFlag
	}
	if toFlag != "" {
		reqBody["to"] = toFlag
	}
	if fromFlag != "" {
		reqBody["from"] = fromFlag
	}
	if topicFlag != "" {
		reqBody["topic"] = topicFlag
	}
	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik subscribe: marshal request: %v\n", err)
		return 1
	}

	// Dial.
	dialCtx, cancelDial := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelDial()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	if err != nil {
		// Distinguish "socket missing" / ECONNREFUSED from other errors.
		var sysErr *os.PathError
		if errors.As(err, &sysErr) && errors.Is(sysErr.Err, syscall.ENOENT) {
			fmt.Fprintf(os.Stderr, "harmonik subscribe: daemon not running (socket %s missing)\n", sockPath)
			return 17
		}
		if errors.Is(err, syscall.ECONNREFUSED) {
			fmt.Fprintf(os.Stderr, "harmonik subscribe: daemon not running (ECONNREFUSED on %s)\n", sockPath)
			return 17
		}
		fmt.Fprintf(os.Stderr, "harmonik subscribe: dial %s: %v\n", sockPath, err)
		return 1
	}
	defer func() { _ = conn.Close() }()

	// Send the subscribe request as a single JSON object.
	if _, err := conn.Write(reqBytes); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik subscribe: write request: %v\n", err)
		return 1
	}

	// On signal, close conn so io.Copy returns and we exit cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	// Copy the NDJSON stream to stdout until EOF.
	if _, err := io.Copy(os.Stdout, conn); err != nil {
		// EOF and "use of closed connection" are clean-exit conditions.
		if !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "use of closed") {
			fmt.Fprintf(os.Stderr, "harmonik subscribe: stream copy: %v\n", err)
			return 1
		}
	}
	return 0
}

func subscribeUsage() {
	fmt.Print(`harmonik subscribe — stream daemon events on the Unix socket

USAGE
  harmonik subscribe [flags]

FLAGS
  --types t1,t2,...      Comma-separated event-type filter (default: all)
  --heartbeat DUR        Idle heartbeat cadence (default 60s; clamped 10s..600s)
  --since-event-id ID    Replay cursor: replay events strictly after this event_id before delivering live stream
  --to NAME              Agent-message filter: only agent_message events addressed to NAME or "*"
  --from NAME            Agent-message filter: only agent_message events sent by NAME
  --topic TOPIC          Agent-message filter: only agent_message events with matching topic
  --socket PATH          Override socket path (default: <project>/.harmonik/daemon.sock)
  --project DIR          Project directory (default: cwd)
  --json                 No-op alias; output is already NDJSON

EXIT CODES
  0   Stream closed cleanly
  1   Argument error or stream write failure
  17  Daemon not running (socket missing or ECONNREFUSED)

EXAMPLES
  harmonik subscribe
  harmonik subscribe --types run_completed,run_failed
  harmonik subscribe --heartbeat 30s --types heartbeat,run_completed
  harmonik subscribe --types agent_message --to alice
  harmonik subscribe --types agent_message --to alice --from bob --topic status
`)
}
