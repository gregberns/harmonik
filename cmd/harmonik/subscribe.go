package main

import (
	"bufio"
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

const (
	subscribeFollowReconnectInitialBackoff = time.Second
	subscribeFollowReconnectMaxBackoff     = 10 * time.Second
)

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
		case arg == "--list-types":
			for _, t := range knownSubscribeTypes() {
				fmt.Println(t)
			}
			return 0
		case arg == "--json":
		default:
			fmt.Fprintf(os.Stderr, "harmonik subscribe: unknown argument %q\n", arg)
			return 1
		}
	}

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

	var types []string
	if typesFlag != "" {
		for _, t := range strings.Split(typesFlag, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				types = append(types, t)
			}
		}
	}

	if err := validateSubscribeTypes(types); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik subscribe: %v\n", err)
		return 1
	}

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

	if sinceFlag != "" {
		reqBodyBase["since_event_id"] = sinceFlag
	}
	reqBytes, err := json.Marshal(reqBodyBase)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik subscribe: marshal request: %v\n", err)
		return 1
	}

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelDial()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	if err != nil {
		if commsIsSocketAbsent(err) || commsIsConnRefused(err) {
			fmt.Fprintf(os.Stderr, "harmonik subscribe: daemon not running (socket %s missing or refused)\n", sockPath)
			return 17
		}
		fmt.Fprintf(os.Stderr, "harmonik subscribe: dial %s: %v\n", sockPath, err)
		return 1
	}
	defer func() { closeSubscribeConn(conn) }()

	if _, err := conn.Write(reqBytes); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik subscribe: write request: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		closeSubscribeConn(conn)
	}()

	buffered := bufio.NewReader(conn)
	first, readErr := buffered.ReadBytes('\n')
	if reason, refused := subscribeRefusalReason(first); refused {
		fmt.Fprintf(os.Stderr, "harmonik subscribe: daemon refused the subscription: %s\n", reason)
		return 1
	}
	if len(first) > 0 {
		if _, err := os.Stdout.Write(first); err != nil {
			fmt.Fprintf(os.Stderr, "harmonik subscribe: write to stdout: %v\n", err)
			return 1
		}
	}
	if readErr != nil {
		if !errors.Is(readErr, io.EOF) && !strings.Contains(readErr.Error(), "use of closed") {
			fmt.Fprintf(os.Stderr, "harmonik subscribe: stream read: %v\n", readErr)
			return 1
		}
		return 0
	}

	if _, err := io.Copy(os.Stdout, buffered); err != nil {
		if !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "use of closed") {
			fmt.Fprintf(os.Stderr, "harmonik subscribe: stream copy: %v\n", err)
			return 1
		}
	}
	return 0
}

func runSubscribeFollowIO(ctx context.Context, reqBodyBase map[string]any, sockPath, sinceEventID string, w io.Writer, heartbeatFilePath string) int {
	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	lastSeen := sinceEventID
	backoff := subscribeFollowReconnectInitialBackoff
	firstDial := true

	for {
		if sigCtx.Err() != nil {
			return 0
		}

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

		backoff = subscribeFollowReconnectInitialBackoff
		firstDial = false

		if _, writeErr := conn.Write(reqBytes); writeErr != nil {
			closeSubscribeConn(conn)
			fmt.Fprintf(os.Stderr, "harmonik subscribe --follow: write request: %v\n", writeErr)
			return 1
		}

		connCloseOnce := make(chan struct{})
		go func() {
			select {
			case <-sigCtx.Done():
				closeSubscribeConn(conn)
			case <-connCloseOnce:
			}
		}()

		reconnect := false
		dec := json.NewDecoder(conn)
		for {
			var rawMsg json.RawMessage
			if decErr := dec.Decode(&rawMsg); decErr != nil {
				close(connCloseOnce)
				closeSubscribeConn(conn)
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

			touchSubscribeHeartbeatFile(heartbeatFilePath)

			var env struct {
				Type        string `json:"type"`
				EventID     string `json:"event_id"`
				LastEventID string `json:"last_event_id"` // heartbeat payload; EV-037a
				// hk-62r8w: SocketResponse fields for server error detection.
				Ok    *bool  `json:"ok"`
				Error string `json:"error"`
			}
			_ = json.Unmarshal(rawMsg, &env) //nolint:errcheck // envelope extraction is best-effort — a line that does not fit the envelope is still forwarded verbatim

			if subscribeRefused(env.Ok) {
				close(connCloseOnce)
				closeSubscribeConn(conn)
				fmt.Fprintf(os.Stderr, "harmonik subscribe --follow: server error: %s\n", env.Error)
				return 1
			}

			if env.Type == "heartbeat" && env.LastEventID != "" {
				if lastSeen == "" || env.LastEventID > lastSeen {
					lastSeen = env.LastEventID
				}
			} else if env.EventID != "" && (lastSeen == "" || env.EventID > lastSeen) {
				lastSeen = env.EventID
			}

			if _, writeErr := fmt.Fprintln(w, string(rawMsg)); writeErr != nil {
				close(connCloseOnce)
				closeSubscribeConn(conn)
				return 0
			}
		}

		if !reconnect {
			return 0
		}
	}
}

func touchSubscribeHeartbeatFile(path string) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //dirmode:allow parent of an operator-supplied --heartbeat-file path, not .harmonik state
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
	if err := f.Close(); err != nil {
		return
	}
	if err := os.Chtimes(path, now, now); err != nil {
		return
	}
}

func closeSubscribeConn(conn net.Conn) {
	if err := conn.Close(); err != nil && !strings.Contains(err.Error(), "use of closed") {
		fmt.Fprintf(os.Stderr, "harmonik subscribe: close connection: %v\n", err)
	}
}

func subscribeUsage() {
	fmt.Print(`harmonik subscribe — stream daemon events on the Unix socket

USAGE
  harmonik subscribe [flags]

FLAGS
  --types t1,t2,...      Comma-separated event-type filter (default: all).
                         An unknown type is refused, not silently accepted.
  --list-types           Print every accepted --types value and exit
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
  1   Argument error (this includes an unknown --types value) or stream write failure
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
