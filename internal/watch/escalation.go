// Package watch — escalation.go implements the WE3 escalation engine.
//
// Spec: plans/2026-06-23-captain-wake-economy/design.md §4 (taxonomy) + §11 WE3.
//
// Contract:
//   - IMMEDIATE-via-watch → CommsSender.SendEscalation (one send, no timer).
//   - PULL-DIGEST          → accumulate into latest.json; never timed-send.
//   - LEDGER-ONLY          → no-op (cursor advance is owned by Ledger).
//   - IMMEDIATE-DIRECT-bypass (daemon/supervisor/paused) is ops-monitor's path
//     (WE7) — the watch never sees those events in a wake-triggering capacity.
//
// Bead: hk-we3-watch-escalation-xas1m.
package watch

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// EscalationClass is the triage decision for a ledger event (design.md §4).
type EscalationClass int

const (
	// EscalationImmediate triggers a comms send --to captain --wake --topic escalation.
	// Triggers: decision_required, run_failed, review_bypassed, agent_failed,
	// review_gate_anomaly.
	EscalationImmediate EscalationClass = iota

	// EscalationPullDigest accumulates a flag into .harmonik/watch/latest.json.
	// The captain reads the digest on its own idle — the watch NEVER timed-sends it.
	// Triggers: crew-staleness signals (run_stale), unknown types (safe default).
	EscalationPullDigest

	// EscalationLedgerOnly records the cursor advance only; no comms action.
	// Triggers: epic_completed (to avoid triple-wake), run_started, run_completed,
	// agent_output_chunk, metric, agent_heartbeat, session_keeper_warn,
	// session_keeper_cycle_complete.
	EscalationLedgerOnly
)

// Classify returns the EscalationClass for ev per the §4 taxonomy table.
//
// Unknown event types default to EscalationPullDigest — safe accumulate-and-batch
// behavior that never wakes the captain spuriously.
func Classify(ev core.Event) EscalationClass {
	switch core.EventType(ev.Type) {

	// IMMEDIATE — captain judgment needed now.
	case core.EventTypeDecisionRequired,
		core.EventTypeRunFailed,
		core.EventTypeReviewBypassed,
		core.EventTypeAgentFailed,
		core.EventTypeReviewGateAnomaly:
		return EscalationImmediate

	// LEDGER-ONLY — routine churn; cursor advance only.
	// epic_completed stays on the captain's own direct subscribe to avoid triple-wake
	// (daemon QuiesceArbiter + captain direct subscribe + watch would be triple).
	case core.EventTypeEpicCompleted,
		core.EventTypeRunStarted,
		core.EventTypeRunCompleted,
		core.EventTypeAgentOutputChunk,
		core.EventTypeMetric,
		core.EventTypeAgentHeartbeat,
		core.EventTypeSessionKeeperWarn,
		core.EventTypeSessionKeeperCycleComplete:
		return EscalationLedgerOnly

	// Default: PULL-DIGEST — accumulate; captain reads on own idle.
	// Covers crew-staleness (run_stale), backlog-ready, lull indicators, etc.
	default:
		return EscalationPullDigest
	}
}

// CommsSender abstracts the `harmonik comms send --from watch --to captain --wake
// --topic escalation` call so the EscalationEngine can be tested without a live daemon.
type CommsSender interface {
	SendEscalation(summary string) error
}

// EscalationEngine processes classified ledger events and dispatches the appropriate action.
type EscalationEngine struct {
	Ledger *Ledger
	Sender CommsSender
}

// Process classifies ev and executes the corresponding action:
//   - IMMEDIATE: calls Sender.SendEscalation(summary).
//   - PULL-DIGEST: appends summary to pending_flags in latest.json.
//   - LEDGER-ONLY: no-op.
//
// summary must be a plain-English actionable description in the captain's own terms
// (what happened / which lane / what decision is needed). Never pass a raw event
// dump or a bare tracking ID.
func (e *EscalationEngine) Process(ev core.Event, summary string) error {
	switch Classify(ev) {
	case EscalationImmediate:
		if err := e.recordImmediate(summary); err != nil {
			return err
		}
		return e.Sender.SendEscalation(summary)
	case EscalationPullDigest:
		return e.appendDigestFlag(summary)
	default: // EscalationLedgerOnly
		return nil
	}
}

// ProcessOpsMonitorReceipt mirrors ops-monitor [IMMEDIATE]/[DIGEST] messages
// into watch/latest.json at receipt time. It is intentionally event-driven:
// callers invoke it only for received agent_message events; it does not poll or
// advance the comms recv cursor.
func (e *EscalationEngine) ProcessOpsMonitorReceipt(ev core.Event) (bool, error) {
	receipt, ok, err := opsMonitorReceipt(ev)
	if err != nil || !ok {
		return ok, err
	}

	report, err := os.ReadFile(e.Ledger.opsMonitorDigestPath)
	if err != nil && !os.IsNotExist(err) {
		return true, fmt.Errorf("escalation: read ops-monitor digest: %w", err)
	}
	if len(report) > 0 {
		receipt.Report = json.RawMessage(report)
	}

	d, err := e.Ledger.ReadDigest()
	if err != nil {
		return true, fmt.Errorf("escalation: read digest: %w", err)
	}
	d.LastOpsMonitorReceipt = &receipt
	d.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	d.Cursor = e.Ledger.Cursor().String()
	return true, e.Ledger.WriteDigest(d)
}

// appendDigestFlag reads latest.json, appends flag to pending_flags, and writes it back.
func (e *EscalationEngine) appendDigestFlag(flag string) error {
	d, err := e.Ledger.ReadDigest()
	if err != nil {
		return fmt.Errorf("escalation: read digest: %w", err)
	}
	d.PendingFlags = append(d.PendingFlags, flag)
	d.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	d.Cursor = e.Ledger.Cursor().String()
	return e.Ledger.WriteDigest(d)
}

func (e *EscalationEngine) recordImmediate(summary string) error {
	d, err := e.Ledger.ReadDigest()
	if err != nil {
		return fmt.Errorf("escalation: read digest: %w", err)
	}
	d.ImmediateCountSinceLastCaptainWake = 0
	d.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	d.Cursor = e.Ledger.Cursor().String()
	return e.Ledger.WriteDigest(d)
}

func opsMonitorReceipt(ev core.Event) (OpsMonitorReceipt, bool, error) {
	if core.EventType(ev.Type) != core.EventType("agent_message") {
		return OpsMonitorReceipt{}, false, nil
	}
	var msg core.AgentMessagePayload
	if err := json.Unmarshal(ev.Payload, &msg); err != nil {
		return OpsMonitorReceipt{}, false, err
	}
	if msg.From != "ops-monitor" {
		return OpsMonitorReceipt{}, false, nil
	}
	kind, ok := opsMonitorReceiptKind(msg.Body)
	if !ok {
		return OpsMonitorReceipt{}, false, nil
	}
	return OpsMonitorReceipt{
		EventID: ev.EventID.String(),
		Kind:    kind,
		Body:    msg.Body,
	}, true, nil
}

func opsMonitorReceiptKind(body string) (string, bool) {
	switch {
	case strings.Contains(body, "[IMMEDIATE]"):
		return "IMMEDIATE", true
	case strings.Contains(body, "[DIGEST]"):
		return "DIGEST", true
	default:
		return "", false
	}
}
