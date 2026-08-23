package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

const labelPrefixProfile = "profile:"

// PiProfileUnknownError is returned by resolvePiProfile when a bead names a
// profile that is absent from the C2-validated harnesses.pi.profiles map.
// Fail-loud: the caller MUST reopen the bead rather than launch on an
// empty/wrong tuple (C3-spec.md §"Claim-time wiring").
type PiProfileUnknownError struct {
	BeadID  string
	Profile string
}

func (e *PiProfileUnknownError) Error() string {
	return fmt.Sprintf("daemon: bead %s: unknown Pi profile %q (not present in harnesses.pi.profiles)", e.BeadID, e.Profile)
}

func resolvePiProfile(
	ctx context.Context,
	beadLabels []string,
	agentType core.AgentType,
	piCfg projectconfig.PiHarnessConfig,
	bus handlercontract.EventEmitter,
	beadID string,
) (projectconfig.PiProfileConfig, error) {
	if agentType != core.AgentTypePi {
		return projectconfig.PiProfileConfig{}, nil
	}

	var profileLabels []string
	for _, lbl := range beadLabels {
		if strings.HasPrefix(lbl, labelPrefixProfile) {
			profileLabels = append(profileLabels, lbl)
		}
	}

	switch len(profileLabels) {
	case 0:
		return projectconfig.PiProfileConfig{}, nil
	case 1:
		name := strings.TrimPrefix(profileLabels[0], labelPrefixProfile)
		profile, ok := piCfg.Profiles[name]
		if !ok {
			return projectconfig.PiProfileConfig{}, &PiProfileUnknownError{BeadID: beadID, Profile: name}
		}
		return profile, nil
	default:
		emitBeadLabelConflict(ctx, bus,
			core.BeadRecord{BeadID: core.BeadID(beadID), Labels: beadLabels},
			profileLabels,
			"tier-1 profile absent: multiple profile:<name> labels; zero tuple (C4 h.* fallback)")
		return projectconfig.PiProfileConfig{}, nil
	}
}

func emitProviderSelected(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	provider string,
) {
	pl := core.ProviderSelectedPayload{
		RunID:    runID.String(),
		Provider: provider,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	if emitErr := bus.Emit(ctx, core.EventTypeProviderSelected, b); emitErr != nil {
		slog.WarnContext(ctx, "daemon: emit provider_selected failed", "err", emitErr, "run_id", runID.String())
	}
}

func hasSingleModelLabel(beadLabels []string) bool {
	count := 0
	for _, lbl := range beadLabels {
		if strings.HasPrefix(lbl, labelPrefixModel) {
			count++
		}
	}
	return count == 1
}
