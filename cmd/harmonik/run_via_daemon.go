package main

// run_via_daemon.go — submit-to-existing-daemon path for `harmonik run`.
//
// When a daemon is already running (detected via the Unix socket), `harmonik run`
// submits its beads as a stream group via the queue-submit socket RPC and blocks
// until the group reaches a terminal state (queue_group_completed / queue_paused)
// by tailing the daemon's subscribe stream.
//
// This lets N concurrent `harmonik run` invocations share one persistent daemon
// transparently instead of colliding on the pidfile lock and exiting 5.
//
// Exit-code contract (mirrors the inline-daemon path):
//
//	0  — group reached complete-success (all beads succeeded)
//	1  — group reached complete-with-failures, queue_paused, or any transport error
//
// Bead ref: hk-b3wqd.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/queue"
)

// isDaemonUp probes the daemon socket for projectDir and returns true if a
// daemon is currently accepting connections. The probe is cheap — it dials
// and immediately closes the connection without sending any data.
func isDaemonUp(projectDir string) bool {
	sockPath := lifecycle.SocketPath(projectDir)
	dialCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// runBeadSubcommandViaDaemon handles the submit-to-existing-daemon path for
// `harmonik run`. It is called when isDaemonUp returns true.
//
// It submits beadIDs as a stream group via the queue-submit socket RPC, then
// subscribes to the daemon's event stream and blocks until the group reaches
// a terminal state. If queue-submit is rejected with queue_already_active, it
// falls back to appending the beads to group 0 of the active queue.
func runBeadSubcommandViaDaemon(
	projectDir string,
	beadIDs []core.BeadID,
	workflowMode string,
	workflowRef string,
	extraContext string,
	templateParams map[string]string,
	groupKind queue.GroupKind,
	notifyWriter io.Writer,
) int {
	sockPath := lifecycle.SocketPath(projectDir)
	harmonikDir := filepath.Join(projectDir, ".harmonik")

	// Build the Item slice from beadIDs and per-run settings.
	// templateParams is already sealed by the caller (nil when empty).
	items := make([]queue.Item, len(beadIDs))
	for i, id := range beadIDs {
		items[i] = queue.Item{
			BeadID:         id,
			Status:         queue.ItemStatusPending,
			Context:        extraContext,
			WorkflowMode:   workflowMode,
			WorkflowRef:    workflowRef,
			TemplateParams: templateParams,
		}
	}

	// Open the subscribe connection BEFORE submitting so we cannot miss the
	// queue_group_completed event for our own group.
	signalCtx, stopSignal := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignal()

	subDialCtx, cancelSubDial := context.WithTimeout(signalCtx, 5*time.Second)
	subConn, err := (&net.Dialer{}).DialContext(subDialCtx, "unix", sockPath)
	cancelSubDial()
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot connect to daemon socket for subscribe: %v\n", err)
		return 1
	}
	defer func() { _ = subConn.Close() }()

	// Subscribe to the minimal set of events needed to detect group completion.
	subReqBytes, _ := json.Marshal(map[string]any{ //nolint:errcheck // constant map; cannot fail
		"op":                "subscribe",
		"types":             []string{"queue_group_completed", "queue_paused", "heartbeat"},
		"heartbeat_seconds": 60,
	})
	if _, writeErr := subConn.Write(subReqBytes); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot send subscribe request: %v\n", writeErr)
		return 1
	}

	// Submit the beads to the daemon as a stream group.
	watchQueueID, watchGroupIndex, submitCode := viaSubmitOrAppend(signalCtx, harmonikDir, items, groupKind)
	if submitCode != 0 {
		return submitCode
	}

	beadIDStrs := make([]string, len(beadIDs))
	for i, id := range beadIDs {
		beadIDStrs[i] = string(id)
	}
	fmt.Fprintf(os.Stderr, "harmonik run: submitted to daemon (queue_id=%s, group=%d, beads=[%s]); waiting for completion...\n",
		watchQueueID, watchGroupIndex, strings.Join(beadIDStrs, ", "))

	// Close the subscribe connection when the signal context fires so the
	// scanner loop below exits cleanly.
	go func() {
		<-signalCtx.Done()
		_ = subConn.Close()
	}()

	return viaWatchGroupCompletion(subConn, watchQueueID, watchGroupIndex, notifyWriter)
}

// viaSubmitOrAppend tries to submit the items as a new stream group. If the
// daemon already has an active queue (queue_already_active), it falls back to
// appending the items to group 0 of the active queue.
//
// Returns (queueID, groupIndex, exitCode). exitCode 0 = accepted; non-zero = error.
func viaSubmitOrAppend(
	ctx context.Context,
	harmonikDir string,
	items []queue.Item,
	groupKind queue.GroupKind,
) (queueID string, groupIndex int, exitCode int) {
	now := time.Now().UTC()

	// Build the queue-submit envelope. The daemon's HandlerAdapter unmarshals
	// the entire SocketRequest JSON as a QueueSubmitRequest, so the op, schema_version,
	// and groups fields must be at the top level.
	type wireGroup struct {
		GroupIndex int               `json:"group_index"`
		Kind       queue.GroupKind   `json:"kind"`
		Status     queue.GroupStatus `json:"status"`
		Items      []queue.Item      `json:"items"`
		CreatedAt  time.Time         `json:"created_at"`
	}
	type submitEnvelope struct {
		Op            string      `json:"op"`
		SchemaVersion int         `json:"schema_version"`
		Groups        []wireGroup `json:"groups"`
	}

	submitBody := submitEnvelope{
		Op:            "queue-submit",
		SchemaVersion: 1,
		Groups: []wireGroup{
			{
				GroupIndex: 0,
				Kind:       groupKind,
				Status:     queue.GroupStatusPending,
				Items:      items,
				CreatedAt:  now,
			},
		},
	}
	submitPayload, marshalErr := json.Marshal(submitBody)
	if marshalErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot marshal submit request: %v\n", marshalErr)
		return "", 0, 1
	}

	submitResp, earlyExit := viaSendRequest(ctx, harmonikDir, submitPayload)
	if earlyExit == exitViaDaemonDown {
		fmt.Fprintf(os.Stderr, "harmonik run: daemon went down between probe and submit\n")
		return "", 0, 1
	}
	if earlyExit != 0 {
		fmt.Fprintf(os.Stderr, "harmonik run: transport error sending submit request\n")
		return "", 0, 1
	}

	if submitResp.Ok {
		// Submit succeeded: extract queue_id from the response.
		var sr struct {
			QueueID string `json:"queue_id"`
		}
		if unmarshalErr := json.Unmarshal(submitResp.Result, &sr); unmarshalErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik run: cannot parse submit response: %v\n", unmarshalErr)
			return "", 0, 1
		}
		return sr.QueueID, 0, 0
	}

	// Submit failed. If it's queue_already_active (QM-027 / ErrorCodeQueueAlreadyActive),
	// fall back to appending to the active queue's group 0.
	if submitResp.ErrorCode != queue.ErrorCodeQueueAlreadyActive {
		fmt.Fprintf(os.Stderr, "harmonik run: queue-submit rejected: %s (code %d)\n",
			submitResp.Error, submitResp.ErrorCode)
		return "", 0, 1
	}

	return viaAppendToActiveQueue(ctx, harmonikDir, items)
}

// viaAppendToActiveQueue queries the active queue_id via queue-status and
// appends items to group 0. Returns (queueID, groupIndex, exitCode).
func viaAppendToActiveQueue(
	ctx context.Context,
	harmonikDir string,
	items []queue.Item,
) (queueID string, groupIndex int, exitCode int) {
	// Query the active queue to get its queue_id.
	statusPayload, _ := json.Marshal(map[string]string{"op": "queue-status"}) //nolint:errcheck
	statusResp, earlyExit := viaSendRequest(ctx, harmonikDir, statusPayload)
	if earlyExit != 0 {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot query daemon queue status for append fallback\n")
		return "", 0, 1
	}
	if !statusResp.Ok {
		fmt.Fprintf(os.Stderr, "harmonik run: queue-status error: %s\n", statusResp.Error)
		return "", 0, 1
	}

	// Parse queue_id from status response.
	var statusBody struct {
		Queue *struct {
			QueueID string `json:"queue_id"`
		} `json:"queue"`
	}
	if unmarshalErr := json.Unmarshal(statusResp.Result, &statusBody); unmarshalErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot parse queue-status response: %v\n", unmarshalErr)
		return "", 0, 1
	}
	if statusBody.Queue == nil {
		// Queue disappeared between submit rejection and status query; safe to
		// retry submit, but for simplicity just surface an error.
		fmt.Fprintf(os.Stderr, "harmonik run: active queue disappeared; retry harmonik run\n")
		return "", 0, 1
	}
	activeQueueID := statusBody.Queue.QueueID

	// Build the queue-append envelope.
	beadIDStrs := make([]string, len(items))
	for i, it := range items {
		beadIDStrs[i] = string(it.BeadID)
	}
	type appendEnvelope struct {
		Op         string   `json:"op"`
		QueueID    string   `json:"queue_id"`
		GroupIndex int      `json:"group_index"`
		BeadIDs    []string `json:"bead_ids"`
	}
	appendPayload, marshalErr := json.Marshal(appendEnvelope{
		Op:         "queue-append",
		QueueID:    activeQueueID,
		GroupIndex: 0,
		BeadIDs:    beadIDStrs,
	})
	if marshalErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot marshal append request: %v\n", marshalErr)
		return "", 0, 1
	}

	appendResp, earlyExitA := viaSendRequest(ctx, harmonikDir, appendPayload)
	if earlyExitA != 0 {
		fmt.Fprintf(os.Stderr, "harmonik run: transport error sending append request\n")
		return "", 0, 1
	}
	if !appendResp.Ok {
		fmt.Fprintf(os.Stderr, "harmonik run: queue-append rejected: %s (code %d)\n",
			appendResp.Error, appendResp.ErrorCode)
		fmt.Fprintf(os.Stderr, "  (the active queue may not accept appends; check 'harmonik queue status')\n")
		return "", 0, 1
	}

	fmt.Fprintf(os.Stderr, "harmonik run: appended to existing queue (queue_id=%s, group=0)\n", activeQueueID)
	return activeQueueID, 0, 0
}

// viaWatchGroupCompletion reads NDJSON events from the subscribe connection
// until it receives a queue_group_completed or queue_paused event for
// queueID/groupIndex. Returns 0 on complete-success, 1 otherwise.
func viaWatchGroupCompletion(
	subConn net.Conn,
	queueID string,
	groupIndex int,
	notifyWriter io.Writer,
) int {
	scanner := bufio.NewScanner(subConn)
	// Increase scanner buffer for large event payloads.
	scanner.Buffer(make([]byte, 64*1024), 512*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// All subscribe events have at least a "type" field.
		var envelope struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			// Malformed line: skip and continue.
			continue
		}

		switch envelope.Type {
		case "queue_group_completed":
			var payload struct {
				QueueID      string `json:"queue_id"`
				GroupIndex   int    `json:"group_index"`
				FinalStatus  string `json:"final_status"`
				SuccessCount int    `json:"success_count"`
				FailCount    int    `json:"fail_count"`
			}
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
				continue
			}
			if payload.QueueID != queueID || payload.GroupIndex != groupIndex {
				continue // belongs to a different queue/group
			}
			fmt.Fprintf(os.Stderr, "harmonik run: group completed (queue_id=%s group=%d status=%s success=%d fail=%d)\n",
				payload.QueueID, payload.GroupIndex, payload.FinalStatus,
				payload.SuccessCount, payload.FailCount)
			if notifyWriter != nil {
				_, _ = fmt.Fprintf(notifyWriter, "group_completed queue_id=%s group=%d status=%s\n",
					payload.QueueID, payload.GroupIndex, payload.FinalStatus)
			}
			if payload.FinalStatus == "complete-success" {
				return 0
			}
			return 1

		case "queue_paused":
			var payload struct {
				QueueID string `json:"queue_id"`
			}
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
				continue
			}
			if payload.QueueID != queueID {
				continue
			}
			fmt.Fprintf(os.Stderr, "harmonik run: queue paused by failure (queue_id=%s)\n", payload.QueueID)
			return 1

		case "heartbeat":
			// Heartbeat: still alive, keep waiting.
			fmt.Fprintf(os.Stderr, "harmonik run: waiting for group %d completion (queue_id=%s)...\n",
				groupIndex, queueID)
		}
	}

	// Scanner ended: either the subscribe connection was closed (signal),
	// the daemon shut down, or a read error occurred.
	if scanErr := scanner.Err(); scanErr != nil && !isConnectionClosed(scanErr) {
		fmt.Fprintf(os.Stderr, "harmonik run: subscribe stream error: %v\n", scanErr)
	}
	return 1
}

// ---------------------------------------------------------------------------
// Socket helpers (local to run_via_daemon.go; mirrors internal/queue/cli/client.go)
// ---------------------------------------------------------------------------

// exitViaDaemonDown is the local sentinel for "daemon socket absent or ECONNREFUSED".
const exitViaDaemonDown = 17

// viaSocketResponse is the wire envelope returned by the daemon for one-shot ops.
type viaSocketResponse struct {
	Ok        bool            `json:"ok"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	ErrorCode int             `json:"error_code,omitempty"`
}

// viaSendRequest dials the daemon socket, sends payload, reads one JSON
// response, and returns (resp, 0). Returns (zero, exitViaDaemonDown) when the
// socket is absent/refused, (zero, 1) on other errors.
func viaSendRequest(ctx context.Context, harmonikDir string, payload []byte) (viaSocketResponse, int) {
	sockPath := filepath.Join(harmonikDir, "daemon.sock")

	dialCtx, cancelDial := context.WithTimeout(ctx, 5*time.Second)
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	cancelDial()
	if err != nil {
		if isViaSocketAbsent(err) || isViaConnRefused(err) {
			return viaSocketResponse{}, exitViaDaemonDown
		}
		return viaSocketResponse{}, 1
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck

	if _, writeErr := conn.Write(payload); writeErr != nil {
		return viaSocketResponse{}, 1
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite() //nolint:errcheck
	}

	var resp viaSocketResponse
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		return viaSocketResponse{}, 1
	}
	return resp, 0
}

// isViaSocketAbsent reports whether err indicates a missing socket file.
func isViaSocketAbsent(err error) bool {
	var opErr *net.OpError
	if netErr, ok := err.(*net.OpError); ok {
		opErr = netErr
	} else {
		return false
	}
	if sysErr, ok := opErr.Err.(*os.PathError); ok {
		return sysErr.Err.Error() == "no such file or directory"
	}
	return strings.Contains(err.Error(), "no such file or directory")
}

// isViaConnRefused reports whether err indicates ECONNREFUSED.
func isViaConnRefused(err error) bool {
	return strings.Contains(err.Error(), "connection refused")
}

// isConnectionClosed reports whether err is a benign "connection closed" error
// from the subscribe scanner (e.g., on signal or daemon shutdown).
func isConnectionClosed(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "use of closed") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe")
}
