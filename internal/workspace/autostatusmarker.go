package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gregberns/harmonik/internal/core"
)

// AutoStatusMarker is the typed struct returned by ReadAutoStatusMarker.
// Fields map verbatim to the auto_status.json marker schema v1 per
// handler-contract.md §4.2a HC-068.
//
// Schema v1 fields:
//   - SchemaVersion: MUST equal AutoStatusMarkerSchemaVersion (1).
//   - Status:        MUST be "FAIL" (only FAIL is accepted; any other value
//     causes the marker to be treated as absent per HC-068 D1).
//   - FailureClass:  One of the six core.FailureClass* values. Out-of-set
//     values cause the hint to be dropped (FailureClass == ""); daemon
//     back-fills per HC-059. compilation_loop is overridden to structural.
//   - Notes:         Optional freeform rationale; engine MUST NOT parse for routing.
//   - Signals:       Optional agent-supplied evidence map; retained for audit only.
type AutoStatusMarker struct {
	SchemaVersion int            `json:"schema_version"`
	Status        string         `json:"status"`
	FailureClass  string         `json:"failure_class"`
	Notes         string         `json:"notes"`
	Signals       map[string]any `json:"signals"`
}

// AutoStatusMarkerSchemaVersion is the current auto_status.json schema version.
const AutoStatusMarkerSchemaVersion = 1

// AutoStatusMarkerPath returns the canonical path for the auto_status marker
// file per handler-contract.md §4.2a HC-068:
//
//	${workspace_path}/.harmonik/auto_status.json
//
// The caller MUST pass the absolute worktree path.
func AutoStatusMarkerPath(workspacePath string) string {
	return filepath.Join(workspacePath, ".harmonik", "auto_status.json")
}

// ReadAutoStatusMarker reads and validates the deny-side outcome-derivation
// marker at ${workspace_path}/.harmonik/auto_status.json per HC-068.
//
// Validation follows the TREAT-AS-ABSENT discipline: invalid or non-conforming
// markers are silently ignored (returned as nil, nil) rather than error.
//
// Validation rules:
//   - JSON parse failure: treated as absent → (nil, nil).
//   - status != "FAIL": treated as absent → (nil, nil).
//     Includes SUCCESS, APPROVE, BLOCK, REQUEST_CHANGES (HC-068 D1).
//   - failure_class out-of-set or absent: hint dropped (FailureClass = "");
//     daemon back-fills from its own classification per HC-059.
//   - failure_class == "compilation_loop": overridden to "structural" per HC-059
//     (compilation_loop is daemon-only).
//
// Returns:
//   - (*AutoStatusMarker, nil) when the file is present and valid.
//   - (nil, nil) when the file is absent — caller treats absence as C1-only gate.
//   - (nil, nil) when the file is present but fails validation (treat-as-absent).
//   - (nil, <wrapped I/O error>) for I/O failures other than not-exist.
func ReadAutoStatusMarker(workspacePath string) (*AutoStatusMarker, error) {
	target := AutoStatusMarkerPath(workspacePath)

	//nolint:gosec // G304: path constructed from workspace_path + known relative segments; not user input
	data, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil //nolint:nilnil // absent marker = C1-only gate per HC-068 Optionality
		}
		return nil, fmt.Errorf("workspace: ReadAutoStatusMarker: ReadFile %q: %w", target, err)
	}

	return ParseAutoStatusMarker(data), nil
}

// ParseAutoStatusMarker validates raw auto_status.json bytes and applies the
// TREAT-AS-ABSENT discipline per HC-068, returning the typed marker or nil when
// the bytes do not denote an active FAIL marker. It is the byte-level core of
// ReadAutoStatusMarker, factored out so a caller that obtains the marker bytes by
// some other transport (e.g. a remote-substrate worker over an SSHRunner, where
// the file is not on box A's filesystem) can apply identical validation.
//
// Returns nil for: empty input, JSON parse failure, or status != "FAIL".
func ParseAutoStatusMarker(data []byte) *AutoStatusMarker {
	if len(data) == 0 {
		return nil
	}

	// JSON parse failure → treat as absent per HC-068 Validation.
	var m AutoStatusMarker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil // treat-as-absent per HC-068
	}

	// status MUST be "FAIL" per HC-068 D1; any other value → treat as absent.
	if m.Status != "FAIL" {
		return nil // non-FAIL status is deny-side-only; treat as absent per HC-068
	}

	// failure_class hint processing per HC-059 / HC-068:
	//   - out-of-set or missing → drop hint (FailureClass = ""); daemon back-fills.
	//   - compilation_loop → override to structural (daemon-only class per HC-059).
	fc := core.FailureClass(m.FailureClass)
	if !fc.Valid() {
		m.FailureClass = ""
	} else if fc == core.FailureClassCompilationLoop {
		m.FailureClass = string(core.FailureClassStructural)
	}

	return &m
}
