package brcli

// TODO(hk-b3f.40): EM-031a active-run discovery requires querying Beads for
// ALL non-terminal statuses, not just in_progress. ListBeadsByStatus adds
// a general-status query method on top of the same Run machinery used by
// ListInFlightBeads. When a future brcli consolidation pass lands, this
// method can be merged with or replace ListInFlightBeads.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// ErrBrListByStatusFailed is returned by ListBeadsByStatus when br exits
// non-zero. Unlike ShowBead, `br list` does not have an ISSUE_NOT_FOUND
// semantic — it always succeeds with an empty envelope when no beads match.
//
// Spec ref: beads-integration.md §4.5 BI-016 (read-surface query discipline).
var ErrBrListByStatusFailed = errors.New("brcli: br list --status failed")

// ListBeadsByStatus invokes `br list --status <status> --json` and returns a
// BeadRecord slice for every bead in that status.
//
// This method is the EM-031a generalisation of ListInFlightBeads: active-run
// discovery requires querying all non-terminal statuses, not only in_progress.
//
// Edges carve-out: `br list` does NOT return full dependency entries — only
// dependency_count and dependent_count are present. Edges is always nil in
// returned records; callers that need full edges MUST call ShowBead separately.
//
// Error semantics:
//   - Non-zero br exit (any reason) → wrapped ErrBrListByStatusFailed
//   - Exec / JSON parse failure     → wrapped error (no sentinel)
//   - Missing issue_type or title   → BrSchemaMismatch (per BI-025b)
//
// Spec ref: execution-model.md §4.7 EM-031a; beads-integration.md §4.5 BI-016.
func (a *Adapter) ListBeadsByStatus(ctx context.Context, status string) ([]core.BeadRecord, error) {
	if status == "" {
		return nil, fmt.Errorf("brcli.ListBeadsByStatus: status must be non-empty")
	}

	result, err := a.Run(ctx, "list", "--status", status, "--json")
	if err != nil {
		return nil, fmt.Errorf("brcli.ListBeadsByStatus: exec failed: %w", err)
	}

	if result.ExitCode != 0 {
		truncated := result.Stdout
		if len(truncated) > 200 {
			truncated = truncated[:200]
		}
		return nil, fmt.Errorf(
			"brcli.ListBeadsByStatus: br exit %d (status=%s): %s: %w",
			result.ExitCode,
			status,
			string(truncated),
			ErrBrListByStatusFailed,
		)
	}

	// Success path: parse {issues: [...]} envelope.
	// Per BI-025b: parse failures of structured output MUST classify as BrSchemaMismatch.
	var envelope brListEnvelope
	if jsonErr := json.Unmarshal(result.Stdout, &envelope); jsonErr != nil {
		return nil, fmt.Errorf("brcli.ListBeadsByStatus: malformed br list output (status=%s): %w; %w", status, jsonErr, BrSchemaMismatch)
	}

	if len(envelope.Issues) == 0 {
		return []core.BeadRecord{}, nil
	}

	records := make([]core.BeadRecord, 0, len(envelope.Issues))
	for _, item := range envelope.Issues {
		if item.IssueType == "" {
			return nil, fmt.Errorf(
				"brcli.ListBeadsByStatus: malformed br list output: missing issue_type for bead %q (status=%s): %w",
				item.ID,
				status,
				BrSchemaMismatch,
			)
		}
		if item.Title == "" {
			return nil, fmt.Errorf(
				"brcli.ListBeadsByStatus: malformed br list output: missing title for bead %q (status=%s): %w",
				item.ID,
				status,
				BrSchemaMismatch,
			)
		}

		var cs core.CoarseStatus
		if unmarshalErr := cs.UnmarshalText([]byte(item.Status)); unmarshalErr != nil {
			return nil, fmt.Errorf("brcli.ListBeadsByStatus: bead %q (status=%s): %w", item.ID, status, unmarshalErr)
		}

		record := core.BeadRecord{
			BeadID:        core.BeadID(item.ID),
			Title:         item.Title,
			Description:   item.Description,
			BeadType:      item.IssueType,
			Status:        cs,
			Labels:        item.Labels,
			Edges:         nil,
			AuditTrailRef: item.ID,
		}
		if !record.Valid() {
			return nil, fmt.Errorf(
				"brcli.ListBeadsByStatus: constructed BeadRecord for %q (status=%s) failed Valid()",
				item.ID,
				status,
			)
		}
		records = append(records, record)
	}
	return records, nil
}
