package daemon

import (
	"context"

	"github.com/gregberns/harmonik/internal/dispatch"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

type dispatchTargetProber interface {
	ProbeDispatchTarget(ctx context.Context, session, window string) ltmux.TargetProbe
}

func readDispatchTargetFact(
	ctx context.Context,
	adapter ltmux.Adapter,
	intent dispatch.Intent,
) dispatch.SessionFact {
	if intent.Validate() != nil || intent.Phase != dispatch.PhaseHandoffDurable {
		return dispatch.SessionConflict
	}
	prober, ok := adapter.(dispatchTargetProber)
	if !ok {
		return dispatch.SessionConflict
	}
	probe := prober.ProbeDispatchTarget(ctx, intent.Handoff.SessionName, intent.Handoff.WindowName)
	return dispatch.ClassifySessionTarget(intent, mapDispatchTargetProbe(probe))
}

func mapDispatchTargetProbe(probe ltmux.TargetProbe) dispatch.SessionTargetObservation {
	status := map[ltmux.TargetProbeStatus]dispatch.SessionTargetProbeStatus{
		ltmux.TargetProbeSessionAbsent:     dispatch.SessionTargetSessionAbsent,
		ltmux.TargetProbeWindowAbsent:      dispatch.SessionTargetWindowAbsent,
		ltmux.TargetProbePaneAbsent:        dispatch.SessionTargetPaneAbsent,
		ltmux.TargetProbeExact:             dispatch.SessionTargetExact,
		ltmux.TargetProbeDuplicate:         dispatch.SessionTargetDuplicate,
		ltmux.TargetProbeSessionUnreadable: dispatch.SessionTargetSessionUnreadable,
		ltmux.TargetProbeWindowUnreadable:  dispatch.SessionTargetWindowUnreadable,
		ltmux.TargetProbePaneUnreadable:    dispatch.SessionTargetPaneUnreadable,
	}[probe.Status]
	return dispatch.SessionTargetObservation{
		Status: status, RunID: probe.RunID, ClaimTransitionID: probe.ClaimTransitionID,
		SessionName: probe.SessionName, WindowName: probe.WindowName,
		PanePID: probe.PanePID, PaneDead: probe.PaneDead,
	}
}
