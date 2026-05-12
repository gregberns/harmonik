package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ReviewVerdict is the typed struct returned by ReadReviewVerdict.
// Fields map verbatim to the agent-reviewer JSON schema v1 per
// workspace-model.md §4.7.WM-027a and event-model.md §8.1a.3.
//
// Schema v1 fields:
//   - SchemaVersion: MUST equal ReviewVerdictSchemaVersion (1).
//   - Verdict:       MUST be one of "APPROVE", "REQUEST_CHANGES", "BLOCK".
//   - Flags:         String array; MAY be empty.
//   - Notes:         Free text; MUST be non-empty per agent-reviewer skill contract.
type ReviewVerdict struct {
	// SchemaVersion is the integer schema version of the agent-reviewer JSON
	// verdict schema. MUST equal ReviewVerdictSchemaVersion (1).
	SchemaVersion int `json:"schema_version"`

	// Verdict is the reviewer's decision. MUST be one of the values declared
	// by ReviewVerdictValue: APPROVE, REQUEST_CHANGES, BLOCK.
	Verdict string `json:"verdict"`

	// Flags is the list of issue tags from the agent-reviewer schema v1.
	// MAY be empty (nil and [] are both valid); a nil JSON value is treated
	// as an empty slice.
	Flags []string `json:"flags"`

	// Notes is the free-text reviewer rationale. MUST be non-empty per the
	// agent-reviewer skill contract (1–3 sentences per §8.1a.3).
	Notes string `json:"notes"`
}

// ReviewVerdictSchemaVersion is the current agent-reviewer JSON schema version.
// ReadReviewVerdict rejects any file whose schema_version field differs from this.
const ReviewVerdictSchemaVersion = 1

// Accepted verdict strings for ReviewVerdict.Verdict per schema v1.
const (
	ReviewVerdictApprove        = "APPROVE"
	ReviewVerdictRequestChanges = "REQUEST_CHANGES"
	ReviewVerdictBlock          = "BLOCK"
)

// ErrMalformed is returned by ReadReviewVerdict when the verdict file at
// ${workspace_path}/.harmonik/review.json is present but fails schema
// validation. Callers that need to distinguish malformed from absent files
// use errors.Is(err, ErrMalformed).
//
// Conditions that produce ErrMalformed (per WM-027a and event-model §8.1a.3):
//   - JSON parse failure.
//   - schema_version field absent, zero, or not equal to ReviewVerdictSchemaVersion.
//   - verdict field absent or not in {APPROVE, REQUEST_CHANGES, BLOCK}.
//   - flags field absent (null token maps to empty slice; missing key is rejected).
//   - notes field absent or empty.
var ErrMalformed = errors.New("workspace: review verdict ErrMalformed")

// ReviewVerdictPath returns the canonical path for the current reviewer
// verdict file per workspace-model.md §4.7.WM-027a:
//
//	${workspace_path}/.harmonik/review.json
//
// The caller MUST pass the absolute worktree path.
func ReviewVerdictPath(workspacePath string) string {
	return filepath.Join(workspacePath, ".harmonik", "review.json")
}

// ReadReviewVerdict reads and validates the reviewer verdict file at
// ${workspace_path}/.harmonik/review.json against the agent-reviewer JSON
// schema v1 (workspace-model.md §4.7.WM-027a; event-model.md §8.1a.3).
//
// Validation rules:
//   - schema_version MUST equal ReviewVerdictSchemaVersion (1).
//   - verdict MUST be one of "APPROVE", "REQUEST_CHANGES", "BLOCK".
//   - flags MUST be present (null is treated as empty slice; missing key is malformed).
//   - notes MUST be non-empty.
//
// Returns:
//   - (*ReviewVerdict, nil) when the file is present and valid.
//   - (nil, ErrMalformed) (wrapping ErrMalformed) for any schema violation.
//   - (nil, nil) when the file does not exist — the caller interprets absence
//     as the inconclusive condition per WM-027a §(e).
//   - (nil, <wrapped I/O error>) for I/O failures other than not-exist.
func ReadReviewVerdict(workspacePath string) (*ReviewVerdict, error) {
	target := ReviewVerdictPath(workspacePath)

	//nolint:gosec // G304: path constructed from workspace_path + known relative segments; not user input
	data, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil //nolint:nilnil // caller interprets nil as "absent" per WM-027a §(e)
		}
		return nil, fmt.Errorf("workspace: ReadReviewVerdict: ReadFile %q: %w", target, err)
	}

	// Unmarshal into a raw map first so we can detect missing keys vs. zero values.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%w: json parse error at %q: %v", ErrMalformed, target, err)
	}

	// Unmarshal into typed struct for field access.
	var v ReviewVerdict
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("%w: json unmarshal into ReviewVerdict at %q: %v", ErrMalformed, target, err)
	}

	// Validate schema_version: key must be present and equal ReviewVerdictSchemaVersion.
	if _, ok := raw["schema_version"]; !ok {
		return nil, fmt.Errorf("%w: schema_version field missing in %q", ErrMalformed, target)
	}
	if v.SchemaVersion != ReviewVerdictSchemaVersion {
		return nil, fmt.Errorf("%w: schema_version = %d; want %d in %q",
			ErrMalformed, v.SchemaVersion, ReviewVerdictSchemaVersion, target)
	}

	// Validate verdict: key must be present and a recognised value.
	if _, ok := raw["verdict"]; !ok {
		return nil, fmt.Errorf("%w: verdict field missing in %q", ErrMalformed, target)
	}
	switch v.Verdict {
	case ReviewVerdictApprove, ReviewVerdictRequestChanges, ReviewVerdictBlock:
		// valid
	default:
		return nil, fmt.Errorf("%w: verdict = %q; must be APPROVE, REQUEST_CHANGES, or BLOCK in %q",
			ErrMalformed, v.Verdict, target)
	}

	// Validate flags: key must be present (null → empty slice is acceptable).
	if _, ok := raw["flags"]; !ok {
		return nil, fmt.Errorf("%w: flags field missing in %q", ErrMalformed, target)
	}
	if v.Flags == nil {
		v.Flags = []string{}
	}

	// Validate notes: key must be present and non-empty.
	if _, ok := raw["notes"]; !ok {
		return nil, fmt.Errorf("%w: notes field missing in %q", ErrMalformed, target)
	}
	if v.Notes == "" {
		return nil, fmt.Errorf("%w: notes field is empty in %q", ErrMalformed, target)
	}

	return &v, nil
}
