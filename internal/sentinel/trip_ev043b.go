package sentinel

// trip_ev043b.go — sentinel trip emission and clearing (hk-dcvj).
//
// When the movement governor trips (ActivationActive), the caller MUST call
// EmitTrip to write ONE decision_required exception. The exception is surfaced
// in every harmonik digest via EV-044 (internal/digest/builder.go buildPendingDecisions),
// structurally blocking the all-clear until real movement resumes.
//
// Clearing fires only on real movement: the caller MUST call ClearTrip when
// the governor returns ActivationDormant (score >= high threshold). Bare
// self-ack is prevented because the governor can only return dormant when
// terminal-progress events (bead_closed, run_completed, HEAD-advance) appear
// in the window — not on the captain's say-so alone.
//
// Idempotency: EmitTrip scans .harmonik/decision_acks/ for an existing
// pending sentinel exception and returns it without writing again.
//
// Ack-state files use the same format as EV-043a
// (internal/daemon/decision_block_ev043a.go) so LoadDecisionAckState restores
// the sentinel block on daemon restart.
//
// Spec ref: docs/flywheel-self-reinforcing-design.md §2, §5.
// Bead ref: hk-dcvj. Epic: hk-0oca (codename:flywheel).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// sentinelSubjectKind is the ack subject_kind for sentinel exceptions.
// Uses "queue" (one of the two kinds in decisionAckSubjectKind) because the
// sentinel watches the whole system, not a specific bead.
const sentinelSubjectKind = "queue"

// sentinelSubjectID is the reserved subject_id for sentinel exceptions.
// Must not collide with operator-assigned queue names.
const sentinelSubjectID = "sentinel"

// sentinelAckSchemaVersion matches decisionAckSchemaVersion in
// internal/daemon/decision_block_ev043a.go (currently 1).
const sentinelAckSchemaVersion = 1

// sentinelSourceSubsystem is the source_subsystem field written into events.jsonl.
const sentinelSourceSubsystem = "sentinel"

// sentinelAckRecord is the on-disk shape for a sentinel ack-state file.
// It mirrors daemon.decisionAckRecord to keep the format compatible with
// LoadDecisionAckState without creating a cross-package import.
type sentinelAckRecord struct {
	SchemaVersion int    `json:"schema_version"`
	AckToken      string `json:"ack_token"`
	Status        string `json:"status"` // "pending" | "acknowledged"
	SubjectKind   string `json:"subject_kind"`
	SubjectID     string `json:"subject_id"`
	Reason        string `json:"reason,omitempty"`
	EmittedAt     string `json:"emitted_at,omitempty"`
	// AckMethod and HaltReason are set when the record is acknowledged.
	// AckMethod: "governor_movement" | "operator" | "legitimate_halt"
	// HaltReason is only present for ack_method="legitimate_halt".
	AckMethod  string `json:"ack_method,omitempty"`
	HaltReason string `json:"halt_reason,omitempty"`
}

// TripInput holds the contextual data the caller supplies when the governor trips.
type TripInput struct {
	// ProjectDir is the root of the harmonik project (parent of .harmonik/).
	ProjectDir string
	// ReadyBeadIDs lists the unblocked open beads at trip time.
	// Used to build the human-readable reason in the exception.
	ReadyBeadIDs []string
	// HasUndeployedTail is true when merged-but-undeployed work exists.
	HasUndeployedTail bool
	// Now is the current wall-clock time.
	Now time.Time
}

// EmitTrip writes ONE decision_required exception for a sentinel trip.
//
// The exception is written to:
//   - .harmonik/decision_acks/<ack_token>   (EV-043a durability anchor)
//   - .harmonik/events/events.jsonl          (EV-044 observational record)
//
// Idempotent: if a pending sentinel exception already exists in decision_acks/,
// EmitTrip returns its ack_token without writing a new one.
//
// Returns the ack_token of the (existing or newly written) exception, or ("", error).
// Called by the workloop when GovernorSignal.Level == ActivationActive.
func EmitTrip(_ context.Context, in TripInput) (string, error) {
	acksDir := decisionAcksDirPath(in.ProjectDir)

	// Idempotency: return the existing ack_token if one is already pending.
	if existing, err := findPendingSentinelAck(acksDir); err != nil {
		return "", fmt.Errorf("sentinel.EmitTrip: check existing: %w", err)
	} else if existing != "" {
		return existing, nil
	}

	ackToken := uuid.New().String()
	reason := buildTripReason(in.ReadyBeadIDs, in.HasUndeployedTail)
	emittedAt := in.Now.UTC().Format(time.RFC3339)

	// Write ack-state file FIRST — it is the durability anchor per EV-043a.
	rec := sentinelAckRecord{
		SchemaVersion: sentinelAckSchemaVersion,
		AckToken:      ackToken,
		Status:        "pending",
		SubjectKind:   sentinelSubjectKind,
		SubjectID:     sentinelSubjectID,
		Reason:        reason,
		EmittedAt:     emittedAt,
	}
	if err := writeSentinelAckFile(acksDir, ackToken, rec); err != nil {
		return "", fmt.Errorf("sentinel.EmitTrip: write ack file: %w", err)
	}

	// Append decision_required event to events.jsonl (EV-044 surface).
	// Non-fatal on failure: the ack file is the durability anchor; the JSONL
	// event is the observational record (decision_block_ev043a.go comment).
	eventsPath := eventsJSONLPath(in.ProjectDir)
	if err := appendDecisionRequired(eventsPath, ackToken, reason, in.Now); err != nil {
		fmt.Fprintf(os.Stderr, "sentinel: EmitTrip: append event (non-fatal): %v\n", err)
	}

	return ackToken, nil
}

// ClearTrip writes a decision_acknowledged event for ackToken and marks the
// ack-state file as acknowledged. Returns nil if the ack file is absent (no-op).
//
// Called by the workloop when GovernorSignal.Level == ActivationDormant AND
// a prior sentinel ack_token is in flight. The governor can only return Dormant
// when real terminal-progress events appear in the window — this is what prevents
// bare self-ack from clearing the exception.
func ClearTrip(_ context.Context, projectDir, ackToken string, now time.Time) error {
	acksDir := decisionAcksDirPath(projectDir)
	ackPath := filepath.Join(acksDir, ackToken)

	data, err := os.ReadFile(ackPath) //nolint:gosec // G304: ackToken is daemon-generated UUID
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to clear
		}
		return fmt.Errorf("sentinel.ClearTrip: read ack file: %w", err)
	}
	var rec sentinelAckRecord
	if jsonErr := json.Unmarshal(data, &rec); jsonErr != nil {
		return fmt.Errorf("sentinel.ClearTrip: parse ack file: %w", jsonErr)
	}

	rec.Status = "acknowledged"
	if writeErr := writeSentinelAckFile(acksDir, ackToken, rec); writeErr != nil {
		return fmt.Errorf("sentinel.ClearTrip: update ack file: %w", writeErr)
	}

	eventsPath := eventsJSONLPath(projectDir)
	if err := appendDecisionAcknowledged(eventsPath, ackToken, "governor_movement", now); err != nil {
		fmt.Fprintf(os.Stderr, "sentinel: ClearTrip: append event (non-fatal): %v\n", err)
	}

	return nil
}

// ClearPendingTrip is the operator escape hatch: it scans .harmonik/decision_acks/
// for the current pending sentinel exception and marks it acknowledged.
//
// Returns (ackToken, nil) when a pending trip was cleared, ("", nil) when no
// pending trip exists, or ("", error) on I/O failure.
//
// Unlike ClearTrip (called by the workloop on governor movement), this records
// ack_method="operator" so observers can distinguish manual operator clears from
// auto-clears on real movement.
//
// Called by `harmonik sentinel clear-trip` (A10 operator escape hatch,
// flywheel-motion.md §2.2, bead hk-kgwv).
func ClearPendingTrip(_ context.Context, projectDir string, now time.Time) (string, error) {
	acksDir := decisionAcksDirPath(projectDir)

	ackToken, err := findPendingSentinelAck(acksDir)
	if err != nil {
		return "", fmt.Errorf("sentinel.ClearPendingTrip: scan acks: %w", err)
	}
	if ackToken == "" {
		return "", nil // no pending trip
	}

	ackPath := filepath.Join(acksDir, ackToken)
	data, readErr := os.ReadFile(ackPath) //nolint:gosec // G304: ackToken is daemon-generated UUID
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return "", nil
		}
		return "", fmt.Errorf("sentinel.ClearPendingTrip: read ack file: %w", readErr)
	}
	var rec sentinelAckRecord
	if jsonErr := json.Unmarshal(data, &rec); jsonErr != nil {
		return "", fmt.Errorf("sentinel.ClearPendingTrip: parse ack file: %w", jsonErr)
	}

	rec.Status = "acknowledged"
	if writeErr := writeSentinelAckFile(acksDir, ackToken, rec); writeErr != nil {
		return "", fmt.Errorf("sentinel.ClearPendingTrip: update ack file: %w", writeErr)
	}

	eventsPath := eventsJSONLPath(projectDir)
	if appendErr := appendDecisionAcknowledged(eventsPath, ackToken, "operator", now); appendErr != nil {
		fmt.Fprintf(os.Stderr, "sentinel: ClearPendingTrip: append event (non-fatal): %v\n", appendErr)
	}

	return ackToken, nil
}

// LegitimateHaltInput holds the data the captain supplies when declaring a
// legitimate halt (spec §2.2 clause 2).
type LegitimateHaltInput struct {
	// ProjectDir is the root of the harmonik project (parent of .harmonik/).
	ProjectDir string
	// AckToken is the ack_token of the pending sentinel exception to clear.
	// If empty, RecordLegitimateHalt scans for the current pending exception.
	AckToken string
	// HaltReason is the human-readable reason for the halt (e.g. "ENOSPC: disk
	// full on /dev/sda1" or "infra: gb-mbp SSH unreachable"). Must be non-empty:
	// an empty reason is indistinguishable from a bare self-ack, which is forbidden.
	HaltReason string
	// Now is the current wall-clock time.
	Now time.Time
}

// RecordLegitimateHalt acknowledges a pending sentinel exception on behalf of a
// captain-declared legitimate halt (spec §2.2 clause 2).
//
// The exception is marked acknowledged with ack_method="legitimate_halt" and the
// halt reason persisted in the ack file and appended to events.jsonl. This is NOT
// a permanent suppression: the governor continues evaluating on the next tick and
// will EmitTrip again if movement remains low (next-pass re-adjudication). The
// recorded halt reason is visible to the adversary session for re-adjudication.
//
// Returns (ackToken, nil) when a pending trip was cleared, ("", nil) when no
// pending trip exists, or ("", error) on I/O failure or empty HaltReason.
//
// Called by `harmonik sentinel record-halt` (AC4 legitimate-halt clear path,
// flywheel-motion.md §2.2 clause 2, bead hk-jvul).
func RecordLegitimateHalt(_ context.Context, in LegitimateHaltInput) (string, error) {
	if strings.TrimSpace(in.HaltReason) == "" {
		return "", fmt.Errorf("sentinel.RecordLegitimateHalt: halt_reason must be non-empty (empty reason is a bare self-ack)")
	}

	acksDir := decisionAcksDirPath(in.ProjectDir)

	ackToken := in.AckToken
	if ackToken == "" {
		var err error
		ackToken, err = findPendingSentinelAck(acksDir)
		if err != nil {
			return "", fmt.Errorf("sentinel.RecordLegitimateHalt: scan acks: %w", err)
		}
		if ackToken == "" {
			return "", nil // no pending trip
		}
	}

	ackPath := filepath.Join(acksDir, ackToken)
	data, readErr := os.ReadFile(ackPath) //nolint:gosec // G304: ackToken is daemon-generated UUID
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return "", nil // ack file absent — no-op
		}
		return "", fmt.Errorf("sentinel.RecordLegitimateHalt: read ack file: %w", readErr)
	}
	var rec sentinelAckRecord
	if jsonErr := json.Unmarshal(data, &rec); jsonErr != nil {
		return "", fmt.Errorf("sentinel.RecordLegitimateHalt: parse ack file: %w", jsonErr)
	}

	rec.Status = "acknowledged"
	rec.AckMethod = "legitimate_halt"
	rec.HaltReason = strings.TrimSpace(in.HaltReason)
	if writeErr := writeSentinelAckFile(acksDir, ackToken, rec); writeErr != nil {
		return "", fmt.Errorf("sentinel.RecordLegitimateHalt: update ack file: %w", writeErr)
	}

	eventsPath := eventsJSONLPath(in.ProjectDir)
	if appendErr := appendLegitimateHaltAck(eventsPath, ackToken, rec.HaltReason, in.Now); appendErr != nil {
		fmt.Fprintf(os.Stderr, "sentinel: RecordLegitimateHalt: append event (non-fatal): %v\n", appendErr)
	}

	return ackToken, nil
}

// IsTripAcknowledged reports whether the ack-state file for ackToken has
// status="acknowledged". Returns (false, nil) when the file is absent or pending,
// and (false, err) on I/O or parse failure.
//
// Used by the workloop to detect external clears (e.g. RecordLegitimateHalt via
// CLI) between ticks, so it can reset sentinelPendingAckToken and re-emit a fresh
// trip for next-pass re-adjudication.
func IsTripAcknowledged(projectDir, ackToken string) (bool, error) {
	acksDir := decisionAcksDirPath(projectDir)
	ackPath := filepath.Join(acksDir, ackToken)
	data, err := os.ReadFile(ackPath) //nolint:gosec // G304: ackToken is daemon-generated UUID
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("sentinel.IsTripAcknowledged: read ack file: %w", err)
	}
	var rec sentinelAckRecord
	if jsonErr := json.Unmarshal(data, &rec); jsonErr != nil {
		return false, fmt.Errorf("sentinel.IsTripAcknowledged: parse ack file: %w", jsonErr)
	}
	return rec.Status == "acknowledged", nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// decisionAcksDirPath returns .harmonik/decision_acks/ for a project dir.
func decisionAcksDirPath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "decision_acks")
}

// eventsJSONLPath returns .harmonik/events/events.jsonl for a project dir.
func eventsJSONLPath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
}

// findPendingSentinelAck scans acksDir for a file with subject_kind=sentinelSubjectKind,
// subject_id=sentinelSubjectID, and status=pending. Returns the ack_token, or "".
func findPendingSentinelAck(acksDir string) (string, error) {
	entries, err := os.ReadDir(acksDir) //nolint:gosec // G304: daemon-controlled dir
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(acksDir, entry.Name())
		data, readErr := os.ReadFile(path) //nolint:gosec // G304: daemon-controlled dir
		if readErr != nil {
			continue // non-fatal: skip unreadable files
		}
		var rec sentinelAckRecord
		if jsonErr := json.Unmarshal(data, &rec); jsonErr != nil {
			continue // non-fatal: skip unparseable files
		}
		if rec.SubjectKind == sentinelSubjectKind &&
			rec.SubjectID == sentinelSubjectID &&
			rec.Status == "pending" {
			return rec.AckToken, nil
		}
	}
	return "", nil
}

// writeSentinelAckFile atomically writes the ack-state file at acksDir/<ackToken>.
func writeSentinelAckFile(acksDir, ackToken string, rec sentinelAckRecord) error {
	if err := os.MkdirAll(acksDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", acksDir, err)
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	path := filepath.Join(acksDir, ackToken)
	return os.WriteFile(path, data, 0o600) //nolint:gosec // G306: ack files are private
}

// appendDecisionRequired appends a decision_required event line to eventsPath.
func appendDecisionRequired(eventsPath, ackToken, reason string, now time.Time) error {
	payload := map[string]interface{}{
		"subject":          map[string]interface{}{"kind": sentinelSubjectKind, "id": sentinelSubjectID},
		"reason":           reason,
		"suggested_action": "Dispatch ready beads or deploy undeployed tail. Clears automatically on real movement (bead_closed / run_completed / HEAD-advance); never self-ack.",
		"ack_required":     true,
		"ack_token":        ackToken,
	}
	return appendEventLine(eventsPath, "decision_required", now, payload)
}

// appendDecisionAcknowledged appends a decision_acknowledged event line to eventsPath.
// ackMethod distinguishes auto-clears ("governor_movement") from operator clears ("operator").
func appendDecisionAcknowledged(eventsPath, ackToken, ackMethod string, now time.Time) error {
	payload := map[string]interface{}{
		"ack_token":  ackToken,
		"subject":    map[string]interface{}{"kind": sentinelSubjectKind, "id": sentinelSubjectID},
		"ack_method": ackMethod,
		"acked_at":   now.UTC().Format(time.RFC3339),
	}
	return appendEventLine(eventsPath, "decision_acknowledged", now, payload)
}

// appendLegitimateHaltAck appends a decision_acknowledged event for a captain
// legitimate-halt clear. Includes halt_reason and readjudicate=true so the
// adversary can re-adjudicate on the next pass (spec §2.2 clause 2).
func appendLegitimateHaltAck(eventsPath, ackToken, haltReason string, now time.Time) error {
	payload := map[string]interface{}{
		"ack_token":    ackToken,
		"subject":      map[string]interface{}{"kind": sentinelSubjectKind, "id": sentinelSubjectID},
		"ack_method":   "legitimate_halt",
		"halt_reason":  haltReason,
		"acked_at":     now.UTC().Format(time.RFC3339),
		"readjudicate": true,
	}
	return appendEventLine(eventsPath, "decision_acknowledged", now, payload)
}

// appendEventLine marshals one core.Event and appends it to eventsPath.
func appendEventLine(eventsPath, evType string, now time.Time, payload interface{}) error {
	eventUUID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("new event id: %w", err)
	}
	payloadBytes, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return fmt.Errorf("marshal payload: %w", marshalErr)
	}
	ev := core.Event{
		EventID:         core.EventID(eventUUID),
		SchemaVersion:   1,
		Type:            evType,
		TimestampWall:   now.UTC(),
		SourceSubsystem: sentinelSourceSubsystem,
		Payload:         json.RawMessage(payloadBytes),
	}
	lineBytes, marshalErr := json.Marshal(ev)
	if marshalErr != nil {
		return fmt.Errorf("marshal event: %w", marshalErr)
	}
	if mkErr := os.MkdirAll(filepath.Dir(eventsPath), 0o755); mkErr != nil {
		return fmt.Errorf("mkdir events dir: %w", mkErr)
	}
	f, openErr := os.OpenFile(eventsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644) //nolint:gosec // G304: daemon-controlled path
	if openErr != nil {
		return fmt.Errorf("open events.jsonl: %w", openErr)
	}
	defer func() { _ = f.Close() }()
	lineBytes = append(lineBytes, '\n')
	_, writeErr := f.Write(lineBytes)
	return writeErr
}

// buildTripReason constructs the human-readable reason string for a sentinel trip.
func buildTripReason(readyBeadIDs []string, hasUndeployedTail bool) string {
	parts := []string{"sentinel: sustained low movement detected"}
	if len(readyBeadIDs) > 0 {
		parts = append(parts, fmt.Sprintf("ready beads: [%s]", strings.Join(readyBeadIDs, ", ")))
	}
	if hasUndeployedTail {
		parts = append(parts, "undeployed tail exists")
	}
	return strings.Join(parts, "; ")
}
