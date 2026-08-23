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

type brReadyItem struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Priority  int      `json:"priority"`
	IssueType string   `json:"issue_type"`
	Status    string   `json:"status"`
	Labels    []string `json:"labels"`
}

const labelNeedsAttention = "needs-attention"

const labelNeedsGreenlight = "needs-greenlight"

const brReadySortPriority = "priority"

// Ready invokes `br ready --format json` and returns a BeadRecord slice for
// every bead whose dependencies are satisfied and whose status is `open`.
// Each record carries the bead's labels array — including any workflow:<mode>
// label per BI-009a — so the daemon's claim path can apply per-task
// workflow-mode overrides at dispatch time (BI-013).
//
// Spec refs: specs/beads-integration.md §4.5 BI-013, BI-013a, BI-013d.
//
// The ready-work query is the input to the daemon dispatch loop. `br ready`
// natively excludes `draft`-status beads (the harmonik-side readiness
// mechanism for loaded-but-not-yet-dispatchable beads) and beads in
// `deferred` or `tombstone` status. Beads carrying a `needs-attention` label
// are additionally excluded at adapter read time per BI-013a so the daemon's
// dispatch loop never observes them as ready.
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
	return a.readyWithArgs(ctx, "--sort", brReadySortPriority)
}

// ReadyAll invokes `br ready --limit 0 --sort priority --format json` and
// returns the FULL, un-paginated set of dispatchable beads.
//
// The plain Ready() path relies on br's default pagination, which can truncate
// the result set and report a falsely-short (or empty) ready list when more
// beads are actually ready than fit on one page. The genuine-drain oracle
// (internal/daemon/draindetect.go) MUST NOT trust a default-paginated empty
// list as evidence of "no work": a paginated empty is one of the documented
// false-negative sources for the fleet sleep/wake interlock (hk-95uf defense
// #1). ReadyAll pins `--limit 0` so br returns every ready bead in one call,
// making an empty result trustworthy.
//
// Error and parse semantics are identical to Ready (see the doc comment above):
// a non-zero br exit wraps ErrBrReadyFailed; a parse / missing-id failure wraps
// BrSchemaMismatch. An empty slice (not nil) is a valid "no ready beads"
// result and is NOT an error.
//
// Bead ref: hk-95uf (genuine-drain oracle, defense #1 — br-ready pagination).
func (a *Adapter) ReadyAll(ctx context.Context) ([]core.BeadRecord, error) {
	return a.readyWithArgs(ctx, "--limit", "0", "--sort", brReadySortPriority)
}

func (a *Adapter) readyWithArgs(ctx context.Context, extraArgs ...string) ([]core.BeadRecord, error) {
	args := make([]string, 0, len(extraArgs)+1)
	args = append(args, "ready")
	args = append(args, extraArgs...)
	result, err := a.runFormatJSON(ctx, args...)
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

	var items []brReadyItem
	if jsonErr := json.Unmarshal(result.Stdout, &items); jsonErr != nil {
		return nil, fmt.Errorf("brcli.Ready: malformed br ready output: %w; %w", jsonErr, BrSchemaMismatch)
	}

	if len(items) == 0 {
		return []core.BeadRecord{}, nil
	}

	records := make([]core.BeadRecord, 0, len(items))
	for _, item := range items {
		if item.ID == "" {
			return nil, fmt.Errorf(
				"brcli.Ready: malformed br ready output: missing id field in element: %w",
				BrSchemaMismatch,
			)
		}
		excluded := false
		for _, lbl := range item.Labels {
			if lbl == labelNeedsAttention || lbl == labelNeedsGreenlight {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
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
