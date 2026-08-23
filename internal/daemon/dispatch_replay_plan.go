package daemon

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
)

type dispatchReplayFactReader interface {
	ReadDispatchReplayFacts(context.Context, dispatch.Intent) (dispatch.ReplayFacts, error)
}

type dispatchReplayStep struct {
	Intent dispatch.Intent
	Action dispatch.ReplayAction
}

func planDispatchReplay(
	ctx context.Context,
	intents []dispatch.Intent,
	reader dispatchReplayFactReader,
) ([]dispatchReplayStep, error) {
	if len(intents) == 0 {
		return nil, nil
	}
	if reader == nil {
		return nil, fmt.Errorf("daemon: dispatch replay fact reader is not configured")
	}

	ordered := append([]dispatch.Intent(nil), intents...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Binding.RunID.String() < ordered[j].Binding.RunID.String()
	})
	steps := make([]dispatchReplayStep, 0, len(ordered))
	seen := make(map[core.RunID]struct{}, len(ordered))
	var planErrors []error
	for _, intent := range ordered {
		if err := intent.Validate(); err != nil {
			planErrors = append(planErrors, fmt.Errorf("daemon: validate dispatch replay intent: %w", err))
			continue
		}
		if _, exists := seen[intent.Binding.RunID]; exists {
			planErrors = append(planErrors, fmt.Errorf("daemon: duplicate dispatch replay run %s", intent.Binding.RunID))
			continue
		}
		seen[intent.Binding.RunID] = struct{}{}

		readerIntent := cloneDispatchIntent(intent)
		facts, err := reader.ReadDispatchReplayFacts(ctx, readerIntent)
		if err != nil {
			planErrors = append(planErrors, fmt.Errorf("daemon: read dispatch replay facts for run %s: %w", intent.Binding.RunID, err))
			continue
		}
		if facts.IntentPhase != intent.Phase || facts.RefusalCause != intentRefusalCause(intent) {
			planErrors = append(planErrors, fmt.Errorf("daemon: dispatch replay facts do not match intent for run %s", intent.Binding.RunID))
			continue
		}
		action, err := dispatch.DecideReplay(facts)
		if err != nil {
			planErrors = append(planErrors, fmt.Errorf("daemon: decide dispatch replay for run %s: %w", intent.Binding.RunID, err))
			continue
		}
		if action == dispatch.ReplayRepairRequired {
			planErrors = append(planErrors, fmt.Errorf("daemon: dispatch replay for run %s requires repair", intent.Binding.RunID))
			continue
		}
		steps = append(steps, dispatchReplayStep{Intent: cloneDispatchIntent(intent), Action: action})
	}
	if len(planErrors) > 0 {
		return nil, errors.Join(planErrors...)
	}
	return steps, nil
}

func intentRefusalCause(intent dispatch.Intent) dispatch.ClaimRefusalCause {
	if intent.Refusal == nil {
		return ""
	}
	return intent.Refusal.Cause
}

func cloneDispatchIntent(intent dispatch.Intent) dispatch.Intent {
	clone := intent
	if intent.Refusal != nil {
		value := *intent.Refusal
		clone.Refusal = &value
	}
	if intent.Run != nil {
		value := *intent.Run
		clone.Run = &value
	}
	if intent.Handoff != nil {
		value := *intent.Handoff
		clone.Handoff = &value
	}
	return clone
}
