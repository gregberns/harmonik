package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// nonAgenticAgentTypes is the set of agent_type values that are classified as
// NON-agentic per workspace-model.md §4.6.WM-022 and handler-contract.md §6.1.
// mechanical / generator / merge-node classes are non-agentic; everything else
// is agentic.
var nonAgenticAgentTypes = map[core.AgentType]struct{}{
	"non-agentic": {},
	"generator":   {},
	"merge-node":  {},
}

// agentTypeIsAgentic reports whether agentType belongs to the set of agentic
// handler classes per workspace-model.md §4.6.WM-022 / handler-contract.md §6.1.
//
// Non-agentic classes: "non-agentic", "generator", "merge-node".
// All other valid agent_type values are agentic.
func agentTypeIsAgentic(at core.AgentType) bool {
	_, nonAgentic := nonAgenticAgentTypes[at]
	return !nonAgentic
}

// sidecarEntry is the minimal shape parsed from a harmonik.meta.json file for
// the purposes of the WM-022 implementer identification walk.
type sidecarEntry struct {
	AgentType  core.AgentType `json:"agent_type"`
	LaunchedAt string         `json:"launched_at"`
}

// FindImplementerHandlerRef walks the session sidecars under
// ${workspacePath}/.harmonik/sessions/*/harmonik.meta.json (per WM-026) and
// returns the *core.HandlerRef for the most-recent agentic session, or nil
// when no agentic session is found.
//
// Identification mechanism per workspace-model.md §4.6.WM-022:
//  1. Enumerate all harmonik.meta.json sidecars under the sessions root.
//  2. Parse the launched_at (RFC 3339) field from each sidecar.
//  3. Order sidecars in REVERSE chronological order (newest first).
//  4. Inspect agent_type for each, in that order; the FIRST sidecar whose
//     agent_type is agentic (not "non-agentic", "generator", or "merge-node")
//     supplies the implementer_handler_ref.
//
// Returns (nil, nil) when no agentic session sidecar is found; the workspace
// manager MUST then set implementer_handler_ref to null per WM-022a.
//
// Returns a non-nil error only on I/O or JSON parse failures; a missing
// sessions directory (no sessions yet) is NOT an error — it yields (nil, nil).
func FindImplementerHandlerRef(workspacePath string) (*core.HandlerRef, error) {
	sessionsDir := filepath.Join(workspacePath, ".harmonik", "sessions")

	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No sessions directory at all — zero sidecars, null fallback per WM-022a.
			return nil, nil //nolint:nilnil // caller interprets nil as "no agentic session" per WM-022
		}
		return nil, fmt.Errorf("workspace: FindImplementerHandlerRef: ReadDir %q: %w", sessionsDir, err)
	}

	type parsedSidecar struct {
		launchedAt time.Time
		agentType  core.AgentType
	}

	var sidecars []parsedSidecar
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sidecarPath := filepath.Join(sessionsDir, entry.Name(), "harmonik.meta.json")
		//nolint:gosec // G304: path constructed from workspace_path + known relative segments; not user input
		data, err := os.ReadFile(sidecarPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue // session dir exists but sidecar not yet written or was interrupted
			}
			return nil, fmt.Errorf("workspace: FindImplementerHandlerRef: ReadFile %q: %w", sidecarPath, err)
		}

		var s sidecarEntry
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("workspace: FindImplementerHandlerRef: Unmarshal %q: %w", sidecarPath, err)
		}

		launchedAt, err := time.Parse(time.RFC3339, s.LaunchedAt)
		if err != nil {
			return nil, fmt.Errorf("workspace: FindImplementerHandlerRef: parse launched_at in %q: %w", sidecarPath, err)
		}

		sidecars = append(sidecars, parsedSidecar{
			launchedAt: launchedAt,
			agentType:  s.AgentType,
		})
	}

	// Sort by launched_at descending (newest first) per WM-022.
	sort.Slice(sidecars, func(i, j int) bool {
		return sidecars[i].launchedAt.After(sidecars[j].launchedAt)
	})

	// Select the FIRST sidecar whose agent_type is agentic per WM-022.
	for _, s := range sidecars {
		if agentTypeIsAgentic(s.agentType) {
			ref := core.HandlerRef(s.agentType)
			return &ref, nil
		}
	}

	// No agentic session found — null fallback per WM-022a.
	return nil, nil //nolint:nilnil // caller interprets nil as "no agentic session" per WM-022
}

// SetImplementerHandlerRefAtMergePending resolves the implementer_handler_ref for ws
// using the sidecar walk (WM-022) and stores the result on ws.ImplementerHandlerRef.
//
// This MUST be called by the workspace manager immediately before calling
// Transition(ws, core.WorkspaceStateMergePending) per workspace-model.md §7.1 / WM-022.
// ws.Path MUST be the absolute filesystem path to the worktree.
//
// On success the field is set (possibly to nil for the all-mechanical path per WM-022a)
// and nil is returned. A non-nil error means an I/O or parse failure occurred; the
// transition MUST NOT proceed.
func SetImplementerHandlerRefAtMergePending(ws *Workspace) error {
	if ws == nil {
		return fmt.Errorf("workspace: SetImplementerHandlerRefAtMergePending: ws must not be nil")
	}
	if ws.Path == "" {
		return fmt.Errorf("workspace: SetImplementerHandlerRefAtMergePending: ws.Path must not be empty")
	}

	ref, err := FindImplementerHandlerRef(ws.Path)
	if err != nil {
		return fmt.Errorf("workspace: SetImplementerHandlerRefAtMergePending: sidecar walk: %w", err)
	}

	ws.ImplementerHandlerRef = ref
	return nil
}
