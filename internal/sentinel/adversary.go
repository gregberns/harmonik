package sentinel

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
)

// AdversaryCrewName is the fixed name of the sentinel adversary crew member.
// Unique within a project; the overlap check uses this as a presence key.
const AdversaryCrewName = "sentinel-adversary"

// AdversaryQueueName is the named queue the adversary crew member binds to.
// Kept separate from the main/captain queue so the adversary's private inbox
// never pollutes the captain's queue view.
const AdversaryQueueName = "sentinel"

// DefaultAdversaryMissionRelPath is the path of the adversary's mission file
// relative to the project root. Overridable via AdversaryInput.MissionPath.
const DefaultAdversaryMissionRelPath = ".flywheel/skills/sentinel-adversary.md"

// AdversaryInput holds the context the caller supplies when invoking SpawnAdversary.
type AdversaryInput struct {
	// ProjectDir is the harmonik project root (parent of .harmonik/).
	ProjectDir string
	// MissionPath is the path to the adversary's mission/handoff file. When
	// empty, DefaultAdversaryMissionRelPath relative to ProjectDir is used.
	MissionPath string
}

type adversaryCrewStartRequest struct {
	Name        string `json:"name"`
	Queue       string `json:"queue"`
	MissionPath string `json:"mission_path"`
}

// AdversaryCrewStarter is the narrow interface SpawnAdversary needs: it must be
// able to start a crew session by name, queue, and mission path. The concrete
// implementation in production is daemon.crewHandlerImpl (via daemon.NewCrewHandler);
// tests inject a lightweight double.
//
// The interface mirrors the daemon.crewStarter interface already used by the
// schedule tick (internal/daemon/scheduletick.go) so no new contract is
// introduced.
type AdversaryCrewStarter interface {
	HandleCrewStart(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}

func (in AdversaryInput) resolvedMissionPath() string {
	if in.MissionPath != "" {
		return in.MissionPath
	}
	return filepath.Join(in.ProjectDir, DefaultAdversaryMissionRelPath)
}

// SpawnAdversary spawns an independent fresh-context adversary crew session
// that reviews the captain's recent comms/commits and writes the
// decision_required exception if it confirms the governor trip.
//
// onlineAgents is a map of presence-online agent names (keyed by name).
// Callers SHOULD obtain this from `harmonik comms who --json` before invoking.
// When AdversaryCrewName is already in onlineAgents, SpawnAdversary returns
// (false, nil) — the prior adversary session is still running; no duplicate
// is spawned.
//
// On a successful spawn, returns (true, nil). On error, returns (false, err);
// the caller SHOULD log and continue — a failed adversary spawn does not block
// the governor or the daemon.
//
// Spec ref: flywheel-motion.md §2.3, §2.4.
func SpawnAdversary(
	ctx context.Context,
	in AdversaryInput,
	starter AdversaryCrewStarter,
	onlineAgents map[string]struct{},
) (spawned bool, err error) {
	if _, online := onlineAgents[AdversaryCrewName]; online {
		return false, nil
	}

	req := adversaryCrewStartRequest{
		Name:        AdversaryCrewName,
		Queue:       AdversaryQueueName,
		MissionPath: in.resolvedMissionPath(),
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return false, fmt.Errorf("sentinel.SpawnAdversary: marshal request: %w", err)
	}
	if _, err := starter.HandleCrewStart(ctx, payload); err != nil {
		return false, fmt.Errorf("sentinel.SpawnAdversary: HandleCrewStart: %w", err)
	}
	return true, nil
}
