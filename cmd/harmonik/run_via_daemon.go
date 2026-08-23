package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

func isDaemonUp(projectDir string) bool {
	sockPath := lifecycle.SocketPath(projectDir)
	dialCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	if err != nil {
		return false
	}
	if closeErr := conn.Close(); closeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: close daemon probe connection: %v\n", closeErr)
	}
	return true
}

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

	items := make([]queue.Item, len(beadIDs))
	for i, id := range beadIDs {
		items[i] = queue.NewPendingItem(queue.Item{
			BeadID:         id,
			Context:        extraContext,
			WorkflowMode:   workflowMode,
			WorkflowRef:    workflowRef,
			TemplateParams: templateParams,
		})
	}

	signalCtx, stopSignal := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignal()

	subDialCtx, cancelSubDial := context.WithTimeout(signalCtx, 5*time.Second)
	subConn, err := (&net.Dialer{}).DialContext(subDialCtx, "unix", sockPath)
	cancelSubDial()
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot connect to daemon socket for subscribe: %v\n", err)
		return 1
	}
	defer func() {
		if closeErr := subConn.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik run: close subscribe connection: %v\n", closeErr)
		}
	}()

	subReqBytes, marshalErr := json.Marshal(map[string]any{
		"op": "subscribe",
		"types": []string{
			"queue_group_completed", "queue_paused", "heartbeat",
			"run_started", "run_completed", "run_failed",
		},
		"heartbeat_seconds": 60,
	})
	if marshalErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot build subscribe request: %v\n", marshalErr)
		return 1
	}
	if _, writeErr := subConn.Write(subReqBytes); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot send subscribe request: %v\n", writeErr)
		return 1
	}

	watchQueueID, watchGroupIndex, appended, submitCode := viaSubmitOrAppend(signalCtx, harmonikDir, items, groupKind)
	if submitCode != 0 {
		return submitCode
	}

	var watchBeads []core.BeadID
	if appended {
		watchBeads = beadIDs
	}

	beadIDStrs := make([]string, len(beadIDs))
	for i, id := range beadIDs {
		beadIDStrs[i] = string(id)
	}
	fmt.Fprintf(os.Stderr, "harmonik run: submitted to daemon (queue_id=%s, group=%d, beads=[%s]); waiting for completion...\n",
		watchQueueID, watchGroupIndex, strings.Join(beadIDStrs, ", "))

	go func() {
		<-signalCtx.Done()
		if closeErr := subConn.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik run: close subscribe connection on signal: %v\n", closeErr)
		}
	}()

	return viaWatchGroupCompletion(subConn, watchQueueID, watchGroupIndex, watchBeads, notifyWriter)
}

func viaSubmitOrAppend(
	ctx context.Context,
	harmonikDir string,
	items []queue.Item,
	groupKind queue.GroupKind,
) (queueID string, groupIndex int, appended bool, exitCode int) {
	now := time.Now().UTC()

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

	submitGroup := queue.NewPendingGroup(queue.Group{
		GroupIndex: 0,
		Kind:       groupKind,
		Items:      items,
		CreatedAt:  now,
	})
	submitBody := submitEnvelope{
		Op:            "queue-submit",
		SchemaVersion: 1,
		Groups: []wireGroup{
			{
				GroupIndex: submitGroup.GroupIndex,
				Kind:       submitGroup.Kind,
				Status:     submitGroup.Status,
				Items:      submitGroup.Items,
				CreatedAt:  submitGroup.CreatedAt,
			},
		},
	}
	submitPayload, marshalErr := json.Marshal(submitBody)
	if marshalErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot marshal submit request: %v\n", marshalErr)
		return "", 0, false, 1
	}

	submitResp, earlyExit := viaSendRequest(ctx, harmonikDir, submitPayload)
	if earlyExit == exitViaDaemonDown {
		fmt.Fprintf(os.Stderr, "harmonik run: daemon went down between probe and submit\n")
		return "", 0, false, 1
	}
	if earlyExit != 0 {
		fmt.Fprintf(os.Stderr, "harmonik run: transport error sending submit request\n")
		return "", 0, false, 1
	}

	if submitResp.Ok {
		var sr struct {
			QueueID string `json:"queue_id"`
		}
		if unmarshalErr := json.Unmarshal(submitResp.Result, &sr); unmarshalErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik run: cannot parse submit response: %v\n", unmarshalErr)
			return "", 0, false, 1
		}
		return sr.QueueID, 0, false, 0
	}

	if submitResp.ErrorCode != queue.ErrorCodeQueueAlreadyActive {
		fmt.Fprintf(os.Stderr, "harmonik run: queue-submit rejected: %s (code %d)\n",
			submitResp.Error, submitResp.ErrorCode)
		return "", 0, false, 1
	}

	return viaAppendToActiveQueue(ctx, harmonikDir, items)
}

func viaAppendToActiveQueue(
	ctx context.Context,
	harmonikDir string,
	items []queue.Item,
) (queueID string, groupIndex int, appended bool, exitCode int) {
	statusPayload, marshalErr := json.Marshal(map[string]string{"op": "queue-status"})
	if marshalErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot build queue-status request: %v\n", marshalErr)
		return "", 0, false, 1
	}
	statusResp, earlyExit := viaSendRequest(ctx, harmonikDir, statusPayload)
	if earlyExit != 0 {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot query daemon queue status for append fallback\n")
		return "", 0, false, 1
	}
	if !statusResp.Ok {
		fmt.Fprintf(os.Stderr, "harmonik run: queue-status error: %s\n", statusResp.Error)
		return "", 0, false, 1
	}

	var statusBody struct {
		Queue *struct {
			QueueID string `json:"queue_id"`
		} `json:"queue"`
	}
	if unmarshalErr := json.Unmarshal(statusResp.Result, &statusBody); unmarshalErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik run: cannot parse queue-status response: %v\n", unmarshalErr)
		return "", 0, false, 1
	}
	if statusBody.Queue == nil {
		fmt.Fprintf(os.Stderr, "harmonik run: active queue disappeared; retry harmonik run\n")
		return "", 0, false, 1
	}
	activeQueueID := statusBody.Queue.QueueID

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
		return "", 0, false, 1
	}

	appendResp, earlyExitA := viaSendRequest(ctx, harmonikDir, appendPayload)
	if earlyExitA != 0 {
		fmt.Fprintf(os.Stderr, "harmonik run: transport error sending append request\n")
		return "", 0, false, 1
	}
	if !appendResp.Ok {
		fmt.Fprintf(os.Stderr, "harmonik run: queue-append rejected: %s (code %d)\n",
			appendResp.Error, appendResp.ErrorCode)
		fmt.Fprintf(os.Stderr, "  (the active queue may not accept appends; check 'harmonik queue status')\n")
		return "", 0, false, 1
	}

	fmt.Fprintf(os.Stderr, "harmonik run: appended to existing queue (queue_id=%s, group=0)\n", activeQueueID)
	return activeQueueID, 0, true, 0
}

func viaWatchGroupCompletion(
	subConn net.Conn,
	queueID string,
	groupIndex int,
	watchBeads []core.BeadID,
	notifyWriter io.Writer,
) int {
	scanner := bufio.NewScanner(subConn)
	setLargeScanBuffer(scanner)

	pendingBeads := make(map[string]struct{}, len(watchBeads))
	for _, id := range watchBeads {
		pendingBeads[string(id)] = struct{}{}
	}
	runToBead := make(map[string]string) // run_id → bead_id (our beads only)
	anyBeadFailed := false

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		if reason, refused := subscribeRefusalReason(line); refused {
			fmt.Fprintf(os.Stderr, "harmonik run: daemon refused the subscription: %s\n", reason)
			return 1
		}

		var envelope struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
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
				if _, writeErr := fmt.Fprintf(notifyWriter, "group_completed queue_id=%s group=%d status=%s\n",
					payload.QueueID, payload.GroupIndex, payload.FinalStatus); writeErr != nil {
					return 1
				}
			}
			if len(watchBeads) > 0 {
				if anyBeadFailed {
					return 1
				}
				if len(pendingBeads) == 0 {
					return 0
				}
				fmt.Fprintf(os.Stderr, "harmonik run: %d of our bead(s) had no observed run outcome; falling back to group status %s\n",
					len(pendingBeads), payload.FinalStatus)
				if payload.FinalStatus == "complete-success" {
					return 0
				}
				return 1
			}
			if payload.FinalStatus == "complete-success" {
				return 0
			}
			return 1

		case "run_started":
			if len(watchBeads) == 0 {
				continue
			}
			var payload struct {
				RunID  string  `json:"run_id"`
				BeadID *string `json:"bead_id"`
			}
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil || payload.BeadID == nil {
				continue
			}
			if _, ours := pendingBeads[*payload.BeadID]; ours {
				runToBead[payload.RunID] = *payload.BeadID
			}

		case "run_completed", "run_failed":
			if len(watchBeads) == 0 {
				continue
			}
			var payload struct {
				RunID string `json:"run_id"`
			}
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
				continue
			}
			beadID, ours := runToBead[payload.RunID]
			if !ours {
				continue
			}
			delete(runToBead, payload.RunID)
			delete(pendingBeads, beadID)
			if envelope.Type == "run_failed" {
				anyBeadFailed = true
				fmt.Fprintf(os.Stderr, "harmonik run: bead %s failed (run_id=%s)\n", beadID, payload.RunID)
			}
			if len(pendingBeads) == 0 {
				fmt.Fprintf(os.Stderr, "harmonik run: all submitted bead(s) reached a terminal run state (failed=%v)\n", anyBeadFailed)
				if anyBeadFailed {
					return 1
				}
				return 0
			}

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
			fmt.Fprintf(os.Stderr, "harmonik run: waiting for group %d completion (queue_id=%s)...\n",
				groupIndex, queueID)
		}
	}

	if scanErr := scanner.Err(); scanErr != nil && !isConnectionClosed(scanErr) {
		fmt.Fprintf(os.Stderr, "harmonik run: subscribe stream error: %v\n", scanErr)
	}
	return 1
}

const exitViaDaemonDown = 17

type viaSocketResponse struct {
	Ok        bool            `json:"ok"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	ErrorCode int             `json:"error_code,omitempty"`
}

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
		if closeErr := uw.CloseWrite(); !isBenignCloseWrite(closeErr) {
			return viaSocketResponse{}, 1
		}
	}

	var resp viaSocketResponse
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		return viaSocketResponse{}, 1
	}
	return resp, 0
}

func isViaSocketAbsent(err error) bool {
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		return false
	}
	var pathErr *os.PathError
	if errors.As(opErr.Err, &pathErr) {
		return errors.Is(pathErr.Err, fs.ErrNotExist)
	}
	return errors.Is(opErr.Err, fs.ErrNotExist)
}

func isViaConnRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}

func isConnectionClosed(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "use of closed") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe")
}
