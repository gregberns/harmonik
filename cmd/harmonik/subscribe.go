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
//	--follow               Auto-reconnect on daemon-restart/EOF (hk-5hs5b); resumes from last cursor
//	--to <name>            Agent-message addressing filter: only deliver agent_message events addressed to <name> or "*"
//	--from <name>          Agent-message addressing filter: only deliver agent_message events sent by <name>
//	--topic <topic>        Agent-message addressing filter: only deliver agent_message events with matching topic
//	--socket <path>        Override socket path (default: <project>/.harmonik/daemon.sock)
//	--project <dir>        Project directory (default: cwd)
//	--heartbeat-file <path> With --follow: touch this file's mtime on every decoded
//	                        line (events AND idle heartbeats), so an external health
//	                        checker can tell the stream is alive without depending on
//	                        whether THIS process ever emits output of its own (hk-q6yrw).
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

// subscribeFollowReconnectInitialBackoff / Max mirror the comms-recv follow
// constants (hk-5hs5b): start at 1 s, double up to 10 s so an agent is never
// more than ~10 s away from picking up the live stream after a daemon revive.
const (
	subscribeFollowReconnectInitialBackoff = time.Second
	subscribeFollowReconnectMaxBackoff     = 10 * time.Second
)

// runSubscribeSubcommand implements `harmonik subscribe [flags]`.
// subArgs is os.Args[2:].
func runSubscribeSubcommand(subArgs []string) int {
	typesFlag := ""
	heartbeatFlag := 60 * time.Second
	sinceFlag := ""
	followFlag := false
	toFlag := ""
	fromFlag := ""
	topicFlag := ""
	socketFlag := ""
	projectFlag := ""
	heartbeatFileFlag := ""

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
		case arg == "--follow":
			followFlag = true
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
		case arg == "--heartbeat-file" && i+1 < len(subArgs):
			i++
			heartbeatFileFlag = subArgs[i]
		case strings.HasPrefix(arg, "--heartbeat-file="):
			heartbeatFileFlag = strings.TrimPrefix(arg, "--heartbeat-file=")
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

	// Build the base request body (without since_event_id, which may advance on reconnect).
	reqBodyBase := map[string]any{
		"op":                "subscribe",
		"heartbeat_seconds": int(heartbeatFlag.Seconds()),
	}
	if len(types) > 0 {
		reqBodyBase["types"] = types
	}
	if toFlag != "" {
		reqBodyBase["to"] = toFlag
	}
	if fromFlag != "" {
		reqBodyBase["from"] = fromFlag
	}
	if topicFlag != "" {
		reqBodyBase["topic"] = topicFlag
	}

	if followFlag {
		return runSubscribeFollowIO(context.Background(), reqBodyBase, sockPath, sinceFlag, os.Stdout, heartbeatFileFlag)
	}

	// Non-follow: single connection, stream to stdout until EOF or signal.
	if sinceFlag != "" {
		reqBodyBase["since_event_id"] = sinceFlag
	}
	reqBytes, err := json.Marshal(reqBodyBase)
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

// runSubscribeFollowIO is the testable core of `harmonik subscribe --follow`
// (hk-5hs5b). It streams events to w, auto-reconnecting on daemon-restart or
// EOF with exponential backoff (1 s → 2 s → … → 10 s). It resumes from the
// last seen event_id so no events are missed or duplicated across reconnects.
//
// Reconnect behaviour mirrors runCommsRecvFollowIO:
//   - First dial failure (socket absent / ECONNREFUSED) → return 17.
//   - Subsequent dial failures → wait backoff, retry (daemon is restarting).
//   - Connection drop (EOF / "use of closed") → wait backoff, reconnect.
//   - SIGINT / SIGTERM → exit 0.
//
// Watermark: lastSeen advances from event_id on each received event and from
// heartbeat.last_event_id (EV-037a) so reconnects in quiet periods do not
// replay events already delivered.
// heartbeatFilePath, when non-empty, is touched (mtime updated) on every
// decoded line — events AND idle heartbeats alike — so an external health
// checker (ops-monitor) can distinguish "stream is alive but nothing to
// escalate" from "stream is dead" without relying on this process's own
// output (hk-q6yrw: a healthy idle watch was mis-read as stalled because the
// prior liveness proxy only tracked messages THIS process chose to send).
func runSubscribeFollowIO(ctx context.Context, reqBodyBase map[string]any, sockPath, sinceEventID string, w io.Writer, heartbeatFilePath string) int {
	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// lastSeen is the watermark: highest event_id delivered so far. It is
	// forwarded as since_event_id on every reconnect so the daemon replays
	// only events strictly after the last delivered one.
	lastSeen := sinceEventID
	backoff := subscribeFollowReconnectInitialBackoff
	firstDial := true

	for {
		if sigCtx.Err() != nil {
			return 0
		}

		// Build subscribe request anchored at lastSeen.
		req := make(map[string]any, len(reqBodyBase)+1)
		for k, v := range reqBodyBase {
			req[k] = v
		}
		if lastSeen != "" {
			req["since_event_id"] = lastSeen
		}
		reqBytes, marshalErr := json.Marshal(req)
		if marshalErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik subscribe --follow: marshal request: %v\n", marshalErr)
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
					fmt.Fprintf(os.Stderr, "harmonik subscribe: daemon not running (socket %s missing or refused)\n", sockPath)
					return 17
				}
				fmt.Fprintf(os.Stderr, "harmonik subscribe --follow: daemon offline, reconnecting in %v...\n", backoff)
				select {
				case <-time.After(backoff):
				case <-sigCtx.Done():
					return 0
				}
				if backoff < subscribeFollowReconnectMaxBackoff {
					backoff *= 2
					if backoff > subscribeFollowReconnectMaxBackoff {
						backoff = subscribeFollowReconnectMaxBackoff
					}
				}
				continue
			}
			fmt.Fprintf(os.Stderr, "harmonik subscribe --follow: dial %s: %v\n", sockPath, dialErr)
			return 1
		}

		// Successful connection — reset backoff.
		backoff = subscribeFollowReconnectInitialBackoff
		firstDial = false

		if _, writeErr := conn.Write(reqBytes); writeErr != nil {
			_ = conn.Close()
			fmt.Fprintf(os.Stderr, "harmonik subscribe --follow: write request: %v\n", writeErr)
			return 1
		}

		// Close conn on signal so the decode loop exits cleanly.
		connCloseOnce := make(chan struct{})
		go func() {
			select {
			case <-sigCtx.Done():
				_ = conn.Close()
			case <-connCloseOnce:
			}
		}()

		// Stream events: decode each as a raw JSON message (preserving bytes
		// for forwarding), extract event_id / heartbeat watermark fields, then
		// write the raw JSON line to w.
		reconnect := false
		dec := json.NewDecoder(conn)
		for {
			var rawMsg json.RawMessage
			if decErr := dec.Decode(&rawMsg); decErr != nil {
				close(connCloseOnce)
				_ = conn.Close()
				if sigCtx.Err() != nil {
					return 0
				}
				if errors.Is(decErr, io.EOF) || strings.Contains(decErr.Error(), "use of closed") {
					fmt.Fprintf(os.Stderr, "harmonik subscribe --follow: connection dropped, reconnecting in %v...\n", backoff)
					select {
					case <-time.After(backoff):
					case <-sigCtx.Done():
						return 0
					}
					if backoff < subscribeFollowReconnectMaxBackoff {
						backoff *= 2
						if backoff > subscribeFollowReconnectMaxBackoff {
							backoff = subscribeFollowReconnectMaxBackoff
						}
					}
					reconnect = true
					break
				}
				fmt.Fprintf(os.Stderr, "harmonik subscribe --follow: decode event: %v\n", decErr)
				return 1
			}

			// hk-q6yrw: touch the heartbeat file for every decoded line,
			// including idle heartbeats — this is stream liveness, not
			// activity, so it must not depend on the line's contents.
			touchSubscribeHeartbeatFile(heartbeatFilePath)

			// Extract watermark fields without re-marshaling the full event.
			var env struct {
				Type        string `json:"type"`
				EventID     string `json:"event_id"`
				LastEventID string `json:"last_event_id"` // heartbeat payload; EV-037a
			}
			_ = json.Unmarshal(rawMsg, &env)

			// EV-037a: advance watermark from heartbeat.last_event_id so reconnects
			// in quiet periods do not re-replay already-delivered events.
			if env.Type == "heartbeat" && env.LastEventID != "" {
				if lastSeen == "" || env.LastEventID > lastSeen {
					lastSeen = env.LastEventID
				}
			} else if env.EventID != "" && (lastSeen == "" || env.EventID > lastSeen) {
				lastSeen = env.EventID
			}

			// Forward the raw event line to the caller's writer.
			if _, writeErr := fmt.Fprintln(w, string(rawMsg)); writeErr != nil {
				// Writer closed (e.g. pipe broken) — exit cleanly.
				close(connCloseOnce)
				_ = conn.Close()
				return 0
			}
		}

		if !reconnect {
			return 0
		}
	}
}

// touchSubscribeHeartbeatFile best-effort-updates path's mtime (creating it
// and any parent directory if needed). Errors are swallowed: the heartbeat
// file is an observability aid, never allowed to interrupt the stream.
func touchSubscribeHeartbeatFile(path string) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	now := time.Now()
	if err := os.Chtimes(path, now, now); err == nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // heartbeat sentinel path, not user input
	if err != nil {
		return
	}
	_ = f.Close()
	_ = os.Chtimes(path, now, now)
}

func subscribeUsage() {
	fmt.Print(`harmonik subscribe — stream daemon events on the Unix socket

USAGE
  harmonik subscribe [flags]

FLAGS
  --types t1,t2,...      Comma-separated event-type filter (default: all)
  --heartbeat DUR        Idle heartbeat cadence (default 60s; clamped 10s..600s)
  --since-event-id ID    Replay cursor: replay events strictly after this event_id before delivering live stream
  --follow               Auto-reconnect on daemon-restart or EOF; resumes from last cursor so no events are lost
  --to NAME              Agent-message filter: only agent_message events addressed to NAME or "*"
  --from NAME            Agent-message filter: only agent_message events sent by NAME
  --topic TOPIC          Agent-message filter: only agent_message events with matching topic
  --socket PATH          Override socket path (default: <project>/.harmonik/daemon.sock)
  --project DIR          Project directory (default: cwd)
  --heartbeat-file PATH  With --follow: touch PATH's mtime on every decoded line
                         (events and idle heartbeats), for external liveness checks
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
  harmonik subscribe --since-event-id <id> --follow
  harmonik subscribe --types run_completed,run_failed --follow
`)
}
