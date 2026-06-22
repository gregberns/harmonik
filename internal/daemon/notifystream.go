package daemon

// notifystream.go — --notify-stream per-bead completion line emitter (hk-ibilr).
//
// NotifyStreamConsumer subscribes to run lifecycle events and writes one line
// per bead completion to an io.Writer (stdout or a FIFO/file path).
//
// Output format:
//
//	[hk-XXX] success (commit abcdef)
//	[hk-XXX] failed (reason: ...)
//
// Lines are emitted as each bead's run_completed/run_failed event lands, not
// at process exit.  This lets the orchestrator react to per-bead completion in
// real time without polling harmonik queue status.
//
// Subscribed events:
//
//   - run_started          — capture RunID → BeadID mapping
//   - workspace_merge_status (status=merged) — capture RunID → commit SHA
//   - run_completed        — write success line
//   - run_failed           — write failure line
//
// Consumer class: observer (EV-012) — side-effect only; must not block
// critical path or emit events back to the bus.
//
// Bead ref: hk-ibilr.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// notifyRunStarted is the minimal subset of the run_started payload we decode.
type notifyRunStarted struct {
	RunID  string `json:"run_id"`
	BeadID string `json:"bead_id"`
}

// notifyMergeStatus is the minimal subset of the workspace_merge_status payload.
type notifyMergeStatus struct {
	RunID           string  `json:"run_id"`
	Status          string  `json:"status"`
	MergeCommitHash *string `json:"merge_commit_hash,omitempty"`
}

// notifyRunCompleted is the minimal subset of run_completed / run_failed payloads.
type notifyRunCompleted struct {
	RunID   string `json:"run_id"`
	Success bool   `json:"success"`
	Summary string `json:"summary"`
}

// NotifyStreamConsumer writes one line per bead completion to w.
// Call Subscribe before bus.Seal (EV-009).
type NotifyStreamConsumer struct {
	w   io.Writer
	mu  sync.Mutex
	ids map[string]string // RunID → BeadID
	sha map[string]string // RunID → merge commit SHA
}

// NewNotifyStreamConsumer creates a consumer that writes completion lines to w.
func NewNotifyStreamConsumer(w io.Writer) *NotifyStreamConsumer {
	return &NotifyStreamConsumer{
		w:   w,
		ids: make(map[string]string),
		sha: make(map[string]string),
	}
}

// Subscribe registers all four observer subscriptions with the bus.
// Must be called before bus.Seal (EV-009).
func (n *NotifyStreamConsumer) Subscribe(bus eventbus.EventBus) error {
	subs := []core.Subscription{
		{
			ConsumerID:    "notify-stream-run-started",
			ConsumerClass: core.ConsumerClassObserver,
			EventPattern:  core.EventPattern{Types: map[core.EventType]struct{}{core.EventTypeRunStarted: {}}},
			OnPanic:       core.OnPanicRecoverAndLog,
			Handler:       n.handleRunStarted,
		},
		{
			ConsumerID:    "notify-stream-merge-status",
			ConsumerClass: core.ConsumerClassObserver,
			EventPattern:  core.EventPattern{Types: map[core.EventType]struct{}{core.EventTypeWorkspaceMergeStatus: {}}},
			OnPanic:       core.OnPanicRecoverAndLog,
			Handler:       n.handleMergeStatus,
		},
		{
			ConsumerID:    "notify-stream-run-completed",
			ConsumerClass: core.ConsumerClassObserver,
			EventPattern:  core.EventPattern{Types: map[core.EventType]struct{}{core.EventTypeRunCompleted: {}}},
			OnPanic:       core.OnPanicRecoverAndLog,
			Handler:       n.handleRunCompleted,
		},
		{
			ConsumerID:    "notify-stream-run-failed",
			ConsumerClass: core.ConsumerClassObserver,
			EventPattern:  core.EventPattern{Types: map[core.EventType]struct{}{core.EventTypeRunFailed: {}}},
			OnPanic:       core.OnPanicRecoverAndLog,
			Handler:       n.handleRunFailed,
		},
	}
	for _, sub := range subs {
		if _, err := bus.Subscribe(sub); err != nil {
			return fmt.Errorf("NotifyStreamConsumer.Subscribe %q: %w", sub.ConsumerID, err)
		}
	}
	return nil
}

func (n *NotifyStreamConsumer) handleRunStarted(_ context.Context, evt core.Event) error {
	var p notifyRunStarted
	if err := json.Unmarshal(evt.Payload, &p); err != nil || p.RunID == "" || p.BeadID == "" {
		return nil
	}
	n.mu.Lock()
	n.ids[p.RunID] = p.BeadID
	n.mu.Unlock()
	return nil
}

func (n *NotifyStreamConsumer) handleMergeStatus(_ context.Context, evt core.Event) error {
	var p notifyMergeStatus
	if err := json.Unmarshal(evt.Payload, &p); err != nil || p.RunID == "" {
		return nil
	}
	if p.Status == "merged" && p.MergeCommitHash != nil && *p.MergeCommitHash != "" {
		n.mu.Lock()
		n.sha[p.RunID] = *p.MergeCommitHash
		n.mu.Unlock()
	}
	return nil
}

func (n *NotifyStreamConsumer) handleRunCompleted(_ context.Context, evt core.Event) error {
	var p notifyRunCompleted
	if err := json.Unmarshal(evt.Payload, &p); err != nil || p.RunID == "" {
		return nil
	}
	n.mu.Lock()
	beadID := n.ids[p.RunID]
	sha := n.sha[p.RunID]
	delete(n.ids, p.RunID)
	delete(n.sha, p.RunID)
	n.mu.Unlock()

	if beadID == "" {
		beadID = p.RunID // fallback: use run_id when bead_id not yet captured
	}
	line := fmt.Sprintf("[%s] success", beadID)
	if sha != "" {
		short := sha
		if len(short) > 7 {
			short = short[:7]
		}
		line = fmt.Sprintf("[%s] success (commit %s)", beadID, short)
	}
	_, _ = fmt.Fprintln(n.w, line)
	return nil
}

func (n *NotifyStreamConsumer) handleRunFailed(_ context.Context, evt core.Event) error {
	var p notifyRunCompleted
	if err := json.Unmarshal(evt.Payload, &p); err != nil || p.RunID == "" {
		return nil
	}
	n.mu.Lock()
	beadID := n.ids[p.RunID]
	delete(n.ids, p.RunID)
	delete(n.sha, p.RunID)
	n.mu.Unlock()

	if beadID == "" {
		beadID = p.RunID
	}
	reason := p.Summary
	if reason == "" {
		reason = "unknown"
	}
	_, _ = fmt.Fprintf(n.w, "[%s] failed (reason: %s)\n", beadID, reason)
	return nil
}
