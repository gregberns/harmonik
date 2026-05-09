package brcli

// TODO(hk-872.28): When BrError enum lands, classify Run's exit codes via that
// taxonomy; ErrBeadNotFound and ErrBrShowFailed will either be subsumed or aliased.
// TODO(hk-872.30): When read-timeout discipline lands, the 5s read timeout will
// wrap ctx automatically; no explicit timeout needed here.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// ErrBeadNotFound is returned by ShowBead when br reports that the requested
// bead ID does not exist (br exit 3, error.code == "ISSUE_NOT_FOUND").
var ErrBeadNotFound = errors.New("brcli: bead not found")

// ErrBrShowFailed is returned by ShowBead when br exits non-zero for any reason
// other than ISSUE_NOT_FOUND.
//
// TODO(hk-872.28): Full BrError integration will absorb this sentinel.
var ErrBrShowFailed = errors.New("brcli: br show failed")

// brShowItem is the per-element JSON shape returned by `br show <id> --format json`.
// The top-level response is a JSON array; each element has this structure.
type brShowItem struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	Status       string       `json:"status"`
	IssueType    string       `json:"issue_type"`
	Dependencies []brShowEdge `json:"dependencies"`
	Dependents   []brShowEdge `json:"dependents"`
	// Parent field is intentionally not used for edge construction — its
	// parent-child entry is already present in Dependencies. Parsing it here
	// allows us to unmarshal the full JSON without unknown-field errors.
	Parent string `json:"parent"`
}

// brShowEdge represents a single entry in either the dependencies or
// dependents array of the br show JSON response.
type brShowEdge struct {
	ID             string `json:"id"`
	DependencyType string `json:"dependency_type"`
}

// brShowErrorEnvelope is the JSON shape returned on non-zero exit by br show.
type brShowErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// ShowBead invokes `br show <id> --format json` and returns the parsed
// BeadRecord for the given bead ID.
//
// Spec ref: specs/beads-integration.md §4.5 BI-015.
//
// Field mapping:
//   - id             → BeadID
//   - title          → Title
//   - description    → Description
//   - issue_type     → BeadType
//   - status         → Status (via CoarseStatus.UnmarshalText)
//   - dependencies[] → outgoing edges (FromBeadID = this bead)
//   - dependents[]   → incoming edges (ToBeadID = this bead)
//   - parent         → IGNORED (already present in dependencies)
//   - AuditTrailRef  → string(id), per BI-031 step 3
func (a *Adapter) ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error) {
	result, err := a.runFormatJSON(ctx, "show", string(id))
	if err != nil {
		return core.BeadRecord{}, fmt.Errorf("brcli.ShowBead: exec failed: %w", err)
	}

	if result.ExitCode != 0 {
		// Attempt to parse as an error envelope to detect ISSUE_NOT_FOUND.
		var envelope brShowErrorEnvelope
		if jsonErr := json.Unmarshal(result.Stdout, &envelope); jsonErr == nil && envelope.Error.Code == "ISSUE_NOT_FOUND" {
			return core.BeadRecord{}, ErrBeadNotFound
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
			errDetail = string(truncated)
		}

		return core.BeadRecord{}, fmt.Errorf(
			"brcli.ShowBead: br exit %d: %s: %w",
			result.ExitCode,
			errDetail,
			ErrBrShowFailed,
		)
	}

	// Success path: parse JSON array.
	// Per BI-025b: parse failures of structured output MUST classify as BrSchemaMismatch.
	var items []brShowItem
	if jsonErr := json.Unmarshal(result.Stdout, &items); jsonErr != nil {
		return core.BeadRecord{}, fmt.Errorf("brcli.ShowBead: malformed br show output: %w; %w", jsonErr, BrSchemaMismatch)
	}

	if len(items) != 1 {
		return core.BeadRecord{}, fmt.Errorf(
			"brcli.ShowBead: malformed br show output: expected exactly 1 element in array, got %d: %w",
			len(items),
			BrSchemaMismatch,
		)
	}

	item := items[0]

	// issue_type is required for a valid BeadRecord.
	if item.IssueType == "" {
		return core.BeadRecord{}, fmt.Errorf(
			"brcli.ShowBead: malformed br show output: missing issue_type field for bead %q: %w",
			item.ID,
			BrSchemaMismatch,
		)
	}

	// Parse CoarseStatus — UnmarshalText rejects unknown values per its contract.
	var status core.CoarseStatus
	if unmarshalErr := status.UnmarshalText([]byte(item.Status)); unmarshalErr != nil {
		return core.BeadRecord{}, fmt.Errorf("brcli.ShowBead: %w", unmarshalErr)
	}

	// Build edges from dependencies (outgoing: this bead → dep) and
	// dependents (incoming: dep → this bead).
	// The `parent` field is redundant with the parent-child entry already in
	// dependencies and is NOT used for edge construction.
	edges := make([]core.DependencyEdge, 0, len(item.Dependencies)+len(item.Dependents))

	for _, dep := range item.Dependencies {
		var kind core.EdgeKind
		if kindErr := kind.UnmarshalText([]byte(dep.DependencyType)); kindErr != nil {
			return core.BeadRecord{}, fmt.Errorf("brcli.ShowBead: dependency edge %q: %w", dep.ID, kindErr)
		}
		edges = append(edges, core.DependencyEdge{
			FromBeadID: id,
			ToBeadID:   core.BeadID(dep.ID),
			EdgeKind:   kind,
		})
	}

	for _, dep := range item.Dependents {
		var kind core.EdgeKind
		if kindErr := kind.UnmarshalText([]byte(dep.DependencyType)); kindErr != nil {
			return core.BeadRecord{}, fmt.Errorf("brcli.ShowBead: dependent edge %q: %w", dep.ID, kindErr)
		}
		edges = append(edges, core.DependencyEdge{
			FromBeadID: core.BeadID(dep.ID),
			ToBeadID:   id,
			EdgeKind:   kind,
		})
	}

	record := core.BeadRecord{
		BeadID:        core.BeadID(item.ID),
		Title:         item.Title,
		Description:   item.Description,
		BeadType:      item.IssueType,
		Status:        status,
		Edges:         edges,
		AuditTrailRef: string(id),
	}

	return record, nil
}
