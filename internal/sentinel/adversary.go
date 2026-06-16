package sentinel

// adversary.go — independent fresh-context adversary spawn (flywheel V4, hk-9mr2).
//
// When the movement governor trips (ActivationActive), the caller invokes
// SpawnAdversary to start a separate, fresh-context crew session that reviews
// the captain's recent comms and commits as foreign artifacts. If the adversary
// confirms the trip is legitimate, it writes the decision_required exception via
// `harmonik sentinel emit-trip`.
//
// Independence is the active ingredient (spec §0.3, §2.3): the adversary runs in
// its own fresh context, untainted by the captain's running conversation. A
// fresh-context review of the same content lifts correction materially versus
// self-critique inside the captain's context (which is biased toward approval).
//
// Trigger discipline (spec §2.4): the adversary is gated by the governor trip —
// it is NOT run on a hot clock. The cheap, LLM-free governor fires the expensive
// LLM adversary only on sustained-low-movement-with-actionable-work past warm-up.
//
// Overlap policy: if a sentinel-adversary crew is already online, SpawnAdversary
// is a no-op (skip). This prevents stacked adversary sessions when the governor
// trips on consecutive evaluation cycles while a prior adversary is still running.
//
// Spec ref: flywheel-motion.md §2.3, §2.4.
// Bead ref: hk-9mr2. Epic: hk-0oca (codename:flywheel).

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

// adversaryCrewStartRequest mirrors daemon.CrewStartRequest without importing
// the daemon package (which would create an import cycle). The JSON shape is
// identical; the daemon socket decodes it to daemon.CrewStartRequest.
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

// resolvedMissionPath returns the effective mission file path: the explicit
// override when set, otherwise DefaultAdversaryMissionRelPath relative to
// ProjectDir.
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
	// Overlap-skip: if the adversary crew is already online, do not spawn a
	// duplicate. The prior session is still reviewing; wait for it to finish.
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
