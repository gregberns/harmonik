package brcli

// TODO(hk-872.28): When BrError enum lands, classify Run's exit codes via that
// taxonomy; ErrBeadNotFound and ErrBrAuditLogFailed will either be subsumed or aliased.
// TODO(hk-872.30): When read-timeout discipline lands, the 5s read timeout will
// wrap ctx automatically; no explicit timeout needed here.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// ErrBrAuditLogFailed is returned by AuditLog when br exits non-zero for any
// reason other than ISSUE_NOT_FOUND.
//
// TODO(hk-872.28): Full BrError integration will absorb this sentinel.
var ErrBrAuditLogFailed = errors.New("brcli: br audit log failed")

// AuditEvent is a single entry in the audit log returned by
// `br --json audit log <id>`.
//
// EventType is a Beads-owned, extensible string; harmonik MUST NOT validate or
// enumerate its values (BI-007 read-surface tolerance). It is passed through
// as-is so callers can inspect it without requiring harmonik changes when Beads
// adds new event types.
//
// Comment, OldValue, and NewValue are optional: they are present only on
// certain event types and are omitted from JSON output when empty
// (omitempty).
//
// Spec ref: specs/beads-integration.md §4.5 BI-016; BI-031 §4.10
// (audit-log is the disambiguation surface for crash-recovery, step 3i).
type AuditEvent struct {
	// ID is the Beads-internal sequential event identifier.
	ID int64 `json:"id"`

	// EventType is the Beads-owned event classification string. The set of
	// valid values is OPEN and extensible by Beads; harmonik passes it through
	// without validation (BI-007).
	EventType string `json:"event_type"`

	// Actor is the user or system that triggered the event (e.g., "gb",
	// "harmonik-daemon").
	Actor string `json:"actor"`

	// Timestamp is the RFC3339 UTC instant the event was recorded.
	Timestamp time.Time `json:"timestamp"`

	// Comment is a free-form annotation supplied by the actor; empty when absent.
	Comment string `json:"comment,omitempty"`

	// OldValue is the prior value for state-transition events (e.g.,
	// status_changed); empty when absent.
	OldValue string `json:"old_value,omitempty"`

	// NewValue is the new value for state-transition events (e.g.,
	// status_changed); empty when absent.
	NewValue string `json:"new_value,omitempty"`
}

// brAuditLogEnvelope is the JSON response shape for `br --json audit log <id>`
// on exit 0.
type brAuditLogEnvelope struct {
	IssueID string       `json:"issue_id"`
	Events  []AuditEvent `json:"events"`
}

// AuditLog invokes `br --json audit log <id>` and returns the ordered list of
// audit events for the specified bead.
//
// The --json flag is global and MUST be the first argument; this differs from
// ShowBead's --format json placement (BI-025b carve-out: the pinned Beads
// version uses --json for this sub-command).
//
// EventType values in the returned events are passed through without
// validation; callers MUST tolerate unknown event types (BI-007).
//
// Spec ref: specs/beads-integration.md §4.5 BI-016; BI-031 §4.10
// (disambiguation surface for crash-recovery, step 3i).
//
// Error semantics:
//   - ISSUE_NOT_FOUND br envelope → ErrBeadNotFound (reuses ShowBead sentinel)
//   - Other non-zero br exit      → wrapped ErrBrAuditLogFailed
//   - Exec / JSON parse failure   → wrapped error (no sentinel)
func (a *Adapter) AuditLog(ctx context.Context, id core.BeadID) ([]AuditEvent, error) {
	result, err := a.Run(ctx, "--json", "audit", "log", string(id))
	if err != nil {
		return nil, fmt.Errorf("brcli.AuditLog: exec failed: %w", err)
	}

	if result.ExitCode != 0 {
		// Attempt to parse as an error envelope to detect ISSUE_NOT_FOUND.
		var envelope brShowErrorEnvelope
		if jsonErr := json.Unmarshal(result.Stdout, &envelope); jsonErr == nil && envelope.Error.Code == "ISSUE_NOT_FOUND" {
			return nil, ErrBeadNotFound
		}

		errDetail := envelope.Error.Message
		if errDetail == "" {
			truncated := result.Stdout
			if len(truncated) > 200 {
				truncated = truncated[:200]
			}
			errDetail = string(truncated)
		}

		return nil, fmt.Errorf(
			"brcli.AuditLog: br exit %d: %s: %w",
			result.ExitCode,
			errDetail,
			ErrBrAuditLogFailed,
		)
	}

	// Success path: parse {issue_id, events: [...]} envelope.
	var envelope brAuditLogEnvelope
	if jsonErr := json.Unmarshal(result.Stdout, &envelope); jsonErr != nil {
		return nil, fmt.Errorf("brcli.AuditLog: malformed br audit log output: %w", jsonErr)
	}

	// Return empty slice (not nil) when the events array is empty, so callers
	// can distinguish "no events" from "not queried".
	if len(envelope.Events) == 0 {
		return []AuditEvent{}, nil
	}

	return envelope.Events, nil
}
