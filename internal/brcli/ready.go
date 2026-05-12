package brcli

// TODO(hk-872.28): When BrError enum lands, classify Run's exit codes via that
// taxonomy; ErrBrReadyFailed will either be subsumed or aliased.
// TODO(hk-872.30): When read-timeout discipline lands, the 5s read timeout will
// wrap ctx automatically; no explicit timeout needed here.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// ErrBrReadyFailed is returned by Ready when br exits non-zero for any reason.
// Unlike ShowBead / ListDependencies, `br ready` does not have an
// ISSUE_NOT_FOUND semantic — it always succeeds with an empty array when there
// are no ready beads.
//
// TODO(hk-872.28): Full BrError integration will absorb this sentinel.
var ErrBrReadyFailed = errors.New("brcli: br ready failed")

// brReadyItem is the per-element JSON shape returned by
// `br ready --format json`. The id field is required; labels surfaces the
// workflow:<mode> label per BI-009a / BI-013.
type brReadyItem struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Priority  int      `json:"priority"`
	IssueType string   `json:"issue_type"`
	Status    string   `json:"status"`
	Labels    []string `json:"labels"`
}

// Ready invokes `br ready --format json` and returns a BeadRecord slice for
// every bead whose dependencies are satisfied and whose status is `open`.
// Each record carries the bead's labels array — including any workflow:<mode>
// label per BI-009a — so the daemon's claim path can apply per-task
// workflow-mode overrides at dispatch time (BI-013).
//
// Spec ref: specs/beads-integration.md §4.5 BI-013.
//
// The ready-work query is the input to the daemon dispatch loop. `br ready`
// natively excludes `draft`-status beads (the harmonik-side readiness
// mechanism for loaded-but-not-yet-dispatchable beads) and beads in
// `deferred` or `tombstone` status, so no post-filtering is required.
//
// An empty slice is a valid result (no ready beads) and is NOT an error.
//
// Note: `br ready` does not return full edge lists; the Edges field of each
// returned BeadRecord is always nil. Callers needing full edges MUST call
// ShowBead separately.
//
// Error semantics:
//   - Non-zero br exit (any reason) → wrapped ErrBrReadyFailed
//   - Exec failure                  → wrapped error (no sentinel)
//   - JSON parse failure            → wrapped BrSchemaMismatch (per BI-025b)
//   - Missing id field per element  → wrapped BrSchemaMismatch (per BI-025b)
func (a *Adapter) Ready(ctx context.Context) ([]core.BeadRecord, error) {
	result, err := a.runFormatJSON(ctx, "ready")
	if err != nil {
		return nil, fmt.Errorf("brcli.Ready: exec failed: %w", err)
	}

	if result.ExitCode != 0 {
		truncated := result.Stdout
		if len(truncated) > 200 {
			truncated = truncated[:200]
		}
		return nil, fmt.Errorf(
			"brcli.Ready: br exit %d: %s: %w",
			result.ExitCode,
			string(truncated),
			ErrBrReadyFailed,
		)
	}

	// Success path: parse flat JSON array.
	// Per BI-025b: parse failures of structured output MUST classify as BrSchemaMismatch.
	var items []brReadyItem
	if jsonErr := json.Unmarshal(result.Stdout, &items); jsonErr != nil {
		return nil, fmt.Errorf("brcli.Ready: malformed br ready output: %w; %w", jsonErr, BrSchemaMismatch)
	}

	// Return empty slice (not nil) when the array is empty, so callers can
	// distinguish "no ready beads" from "not queried".
	if len(items) == 0 {
		return []core.BeadRecord{}, nil
	}

	records := make([]core.BeadRecord, 0, len(items))
	for _, item := range items {
		// id is required — a missing id cannot produce a valid BeadID.
		// Per BI-025b: missing required field is a schema-level invariant
		// violation; classify as BrSchemaMismatch.
		if item.ID == "" {
			return nil, fmt.Errorf(
				"brcli.Ready: malformed br ready output: missing id field in element: %w",
				BrSchemaMismatch,
			)
		}
		// br ready does not return full edges; Edges is nil per the Edges carve-out
		// analogous to br list (callers needing full edges must call ShowBead).
		// Labels are populated from the raw br ready JSON payload (BI-013 / BI-009a).
		// Status is passed through as-is; `br ready` only surfaces open beads so
		// the value is always "open" in practice, but we parse it directly to stay
		// robust to any future ready-work semantics change.
		var cs core.CoarseStatus
		if item.Status != "" {
			if unmarshalErr := cs.UnmarshalText([]byte(item.Status)); unmarshalErr != nil {
				return nil, fmt.Errorf("brcli.Ready: bead %q: %w", item.ID, unmarshalErr)
			}
		}
		records = append(records, core.BeadRecord{
			BeadID:        core.BeadID(item.ID),
			Title:         item.Title,
			BeadType:      item.IssueType,
			Status:        cs,
			Labels:        item.Labels,
			Edges:         nil,
			AuditTrailRef: item.ID,
		})
	}

	return records, nil
}
