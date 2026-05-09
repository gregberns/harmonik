package brcli

// TODO(hk-872.28): When BrError enum lands, classify Run's exit codes via that
// taxonomy; ErrBrDepListFailed will either be subsumed or aliased.
// TODO(hk-872.30): When read-timeout discipline lands, the 5s read timeout will
// wrap ctx automatically; no explicit timeout needed here.
// TODO(hk-872.55): When EdgeKind extends to tolerate Beads's broader dep-type
// surface (related, etc.), unknown-kind errors in ListDependencies will reduce.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
)

// ErrBrDepListFailed is returned by ListDependencies when br exits non-zero for
// any reason other than ISSUE_NOT_FOUND. Callers can use errors.Is to
// distinguish dep-list failures from show failures (ErrBrShowFailed).
//
// TODO(hk-872.28): Full BrError integration will absorb this sentinel.
var ErrBrDepListFailed = errors.New("brcli: br dep list failed")

// brDepListItem is the per-element JSON shape returned by
// `br dep list <id> --direction both --format json`.
// The top-level response is a flat JSON array; each element has this structure.
// Fields title, status, and priority are parsed but not mapped to DependencyEdge.
type brDepListItem struct {
	IssueID     string `json:"issue_id"`
	DependsOnID string `json:"depends_on_id"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Priority    int    `json:"priority"`
}

// brDepListErrorEnvelope is the JSON shape returned on non-zero exit by br dep list.
// It matches the same error envelope shape used by br show (handled by show.go).
type brDepListErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// ListDependencies invokes `br dep list <id> --direction both --format json` and
// returns the full typed-edge set for the given bead ID, covering both outgoing
// (issue_id == id) and incoming (depends_on_id == id) edges.
//
// Spec ref: specs/beads-integration.md §4.5 BI-014.
//
// Field mapping:
//   - issue_id      → FromBeadID
//   - depends_on_id → ToBeadID
//   - type          → EdgeKind (via EdgeKind.UnmarshalText; rejects unknown values)
//   - title/status/priority → ignored
//
// Every returned edge satisfies edge.Valid(). An empty slice is returned when
// the bead exists but has no dependency edges.
func (a *Adapter) ListDependencies(ctx context.Context, id core.BeadID) ([]core.DependencyEdge, error) {
	result, err := a.runFormatJSON(ctx, "dep", "list", string(id), "--direction", "both")
	if err != nil {
		return nil, fmt.Errorf("brcli.ListDependencies: exec failed: %w", err)
	}

	if result.ExitCode != 0 {
		// Attempt to parse as an error envelope to detect ISSUE_NOT_FOUND.
		// ErrBeadNotFound is reused from show.go — not redeclared here.
		var envelope brDepListErrorEnvelope
		if jsonErr := json.Unmarshal(result.Stdout, &envelope); jsonErr == nil && envelope.Error.Code == "ISSUE_NOT_FOUND" {
			return nil, ErrBeadNotFound
		}

		// Determine a human-readable error detail for the wrapped error.
		errDetail := envelope.Error.Message
		if errDetail == "" {
			// Fall back to truncated stdout if envelope parse failed or message
			// was empty.
			truncated := result.Stdout
			if len(truncated) > 200 {
				truncated = truncated[:200]
			}
			errDetail = strings.TrimSpace(string(truncated))
		}

		return nil, fmt.Errorf(
			"brcli.ListDependencies: br exit %d: %s: %w",
			result.ExitCode,
			errDetail,
			ErrBrDepListFailed,
		)
	}

	// Success path: parse the flat JSON array.
	// Per BI-025b: parse failures of structured output MUST classify as BrSchemaMismatch.
	var items []brDepListItem
	if jsonErr := json.Unmarshal(result.Stdout, &items); jsonErr != nil {
		return nil, fmt.Errorf("brcli.ListDependencies: malformed br dep list output: %w; %w", jsonErr, BrSchemaMismatch)
	}

	edges := make([]core.DependencyEdge, 0, len(items))
	for _, item := range items {
		// TODO(hk-872.55): "related" and other Beads dep-types not in the spec
		// will be rejected here until EdgeKind is extended.
		var kind core.EdgeKind
		if kindErr := kind.UnmarshalText([]byte(item.Type)); kindErr != nil {
			return nil, fmt.Errorf("brcli.ListDependencies: edge %q→%q: %w", item.IssueID, item.DependsOnID, kindErr)
		}
		edge := core.DependencyEdge{
			FromBeadID: core.BeadID(item.IssueID),
			ToBeadID:   core.BeadID(item.DependsOnID),
			EdgeKind:   kind,
		}
		if !edge.Valid() {
			return nil, fmt.Errorf(
				"brcli.ListDependencies: invalid edge from br dep list output: from=%q to=%q kind=%q",
				item.IssueID, item.DependsOnID, item.Type,
			)
		}
		edges = append(edges, edge)
	}

	return edges, nil
}
