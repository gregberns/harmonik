package brcli

// TODO(hk-872.28): When BrError enum lands, classify Run's exit codes via that
// taxonomy; ErrBrListFailed will either be subsumed or aliased.
// TODO(hk-872.30): When read-timeout discipline lands, the 5s read timeout will
// wrap ctx automatically; no explicit timeout needed here.
// BI-025b note: br list uses --json (global flag) rather than --format json;
// this is the pinned Beads version's structured-output flag for this subcommand.
// Run is called directly (not runFormatJSON) per the BI-025b carve-out.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// ErrBrListFailed is returned by ListInFlightBeads when br exits non-zero for
// any reason. Unlike ShowBead / AuditLog, `br list` does not have a
// ISSUE_NOT_FOUND semantic — it always succeeds with an empty envelope when
// there are no matching beads.
//
// TODO(hk-872.28): Full BrError integration will absorb this sentinel.
var ErrBrListFailed = errors.New("brcli: br list failed")

// brListItem is the per-element JSON shape returned by
// `br list --status in_progress --json`. It is similar to but NOT identical to
// the brShowItem shape: dependency_count and dependent_count are present
// instead of full edge details.
type brListItem struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	Status          string   `json:"status"`
	IssueType       string   `json:"issue_type"`
	Labels          []string `json:"labels"`
	DependencyCount int      `json:"dependency_count"`
	DependentCount  int      `json:"dependent_count"`
}

// brListEnvelope is the JSON response shape for
// `br list --status in_progress --json` on exit 0.
type brListEnvelope struct {
	Issues []brListItem `json:"issues"`
}

// ListInFlightBeads invokes `br list --status in_progress --json` and returns
// a BeadRecord slice for every bead currently in the in_progress state.
//
// Edges carve-out: `br list` does NOT return full dependency entries — only
// dependency_count and dependent_count are present in the response. Therefore
// Edges is always set to nil in the returned records. Callers that need full
// edge details MUST call ShowBead or ListDependencies for each bead separately.
//
// Spec ref: specs/beads-integration.md §4.5 BI-016 (in-flight enumeration
// during daemon startup reconciliation, steps 3–4).
//
// Error semantics:
//   - Non-zero br exit (any reason) → wrapped ErrBrListFailed
//   - Exec / JSON parse failure     → wrapped error (no sentinel)
//   - Missing issue_type or title   → wrapped error (BeadRecord.Valid() would fail)
func (a *Adapter) ListInFlightBeads(ctx context.Context) ([]core.BeadRecord, error) {
	result, err := a.Run(ctx, "list", "--status", "in_progress", "--json")
	if err != nil {
		return nil, fmt.Errorf("brcli.ListInFlightBeads: exec failed: %w", err)
	}

	if result.ExitCode != 0 {
		truncated := result.Stdout
		if len(truncated) > 200 {
			truncated = truncated[:200]
		}
		return nil, fmt.Errorf(
			"brcli.ListInFlightBeads: br exit %d: %s: %w",
			result.ExitCode,
			string(truncated),
			ErrBrListFailed,
		)
	}

	// Success path: parse {issues: [...]} envelope.
	// Per BI-025b: parse failures of structured output MUST classify as BrSchemaMismatch.
	var envelope brListEnvelope
	if jsonErr := json.Unmarshal(result.Stdout, &envelope); jsonErr != nil {
		return nil, fmt.Errorf("brcli.ListInFlightBeads: malformed br list output: %w; %w", jsonErr, BrSchemaMismatch)
	}

	// Return empty slice (not nil) when the issues array is empty, so callers
	// can distinguish "no in-flight beads" from "not queried".
	if len(envelope.Issues) == 0 {
		return []core.BeadRecord{}, nil
	}

	records := make([]core.BeadRecord, 0, len(envelope.Issues))
	for _, item := range envelope.Issues {
		// issue_type is required for a valid BeadRecord.
		// Per BI-025b: missing required field is a schema-level invariant violation;
		// classify as BrSchemaMismatch (mirrors ShowBead lines 124–129).
		if item.IssueType == "" {
			return nil, fmt.Errorf(
				"brcli.ListInFlightBeads: malformed br list output: missing issue_type field for bead %q: %w",
				item.ID,
				BrSchemaMismatch,
			)
		}

		// title is required for a valid BeadRecord.
		// Per BI-025b: missing required field is a schema-level invariant violation;
		// classify as BrSchemaMismatch (mirrors ShowBead pattern).
		if item.Title == "" {
			return nil, fmt.Errorf(
				"brcli.ListInFlightBeads: malformed br list output: missing title field for bead %q: %w",
				item.ID,
				BrSchemaMismatch,
			)
		}

		// Parse CoarseStatus — UnmarshalText rejects unknown values per its contract.
		var status core.CoarseStatus
		if unmarshalErr := status.UnmarshalText([]byte(item.Status)); unmarshalErr != nil {
			return nil, fmt.Errorf("brcli.ListInFlightBeads: bead %q: %w", item.ID, unmarshalErr)
		}

		// Edges are NOT available from br list (only dependency_count /
		// dependent_count). Set to nil — callers needing full edges MUST call
		// ShowBead or ListDependencies for each bead.
		record := core.BeadRecord{
			BeadID:        core.BeadID(item.ID),
			Title:         item.Title,
			Description:   item.Description,
			BeadType:      item.IssueType,
			Status:        status,
			Edges:         nil,
			AuditTrailRef: item.ID,
		}

		if !record.Valid() {
			return nil, fmt.Errorf(
				"brcli.ListInFlightBeads: constructed BeadRecord for %q failed Valid() check",
				item.ID,
			)
		}

		records = append(records, record)
	}

	return records, nil
}
