package daemon

import (
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/dispatch"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

func TestMapDispatchTargetProbeClosedStatuses(t *testing.T) {
	for _, tc := range []struct {
		input ltmux.TargetProbeStatus
		want  dispatch.SessionTargetProbeStatus
	}{
		{input: ltmux.TargetProbeSessionAbsent, want: dispatch.SessionTargetSessionAbsent},
		{input: ltmux.TargetProbeWindowAbsent, want: dispatch.SessionTargetWindowAbsent},
		{input: ltmux.TargetProbePaneAbsent, want: dispatch.SessionTargetPaneAbsent},
		{input: ltmux.TargetProbeExact, want: dispatch.SessionTargetExact},
		{input: ltmux.TargetProbeDuplicate, want: dispatch.SessionTargetDuplicate},
		{input: ltmux.TargetProbeSessionUnreadable, want: dispatch.SessionTargetSessionUnreadable},
		{input: ltmux.TargetProbeWindowUnreadable, want: dispatch.SessionTargetWindowUnreadable},
		{input: ltmux.TargetProbePaneUnreadable, want: dispatch.SessionTargetPaneUnreadable},
		{input: "unknown", want: ""},
	} {
		t.Run(string(tc.input), func(t *testing.T) {
			probe := ltmux.TargetProbe{
				Status: tc.input, RunID: "run", ClaimTransitionID: "claim",
				SessionName: "session", WindowName: "window", PanePID: "1", PaneDead: "0",
			}
			got := mapDispatchTargetProbe(probe)
			if got.Status != tc.want || got.RunID != probe.RunID || got.ClaimTransitionID != probe.ClaimTransitionID ||
				got.SessionName != probe.SessionName || got.WindowName != probe.WindowName ||
				got.PanePID != probe.PanePID || got.PaneDead != probe.PaneDead {
				t.Fatalf("mapped probe = %+v", got)
			}
		})
	}
}

func TestReadDispatchTargetFactUsesExactIntentTarget(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	adapter := &dispatchTargetProbeAdapter{probe: ltmux.TargetProbe{
		Status: ltmux.TargetProbeExact, RunID: intent.Binding.RunID.String(),
		ClaimTransitionID: intent.Binding.ClaimTransitionID.String(),
		SessionName:       intent.Handoff.SessionName, WindowName: intent.Handoff.WindowName,
		PanePID: "1234", PaneDead: "0",
	}}
	if got := readDispatchTargetFact(t.Context(), adapter, intent); got != dispatch.SessionLive {
		t.Fatalf("fact = %q, want live", got)
	}
	if adapter.session != intent.Handoff.SessionName || adapter.window != intent.Handoff.WindowName {
		t.Fatalf("probe target = %q:%q", adapter.session, adapter.window)
	}
}

func TestReadDispatchTargetFactFailsClosedWithoutProbePort(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	if got := readDispatchTargetFact(t.Context(), noDispatchTargetProbeAdapter{}, intent); got != dispatch.SessionConflict {
		t.Fatalf("fact = %q, want conflict", got)
	}
	prober := &dispatchTargetProbeAdapter{}
	if got := readDispatchTargetFact(t.Context(), prober, replayOwnershipIntent(t, dispatch.PhaseRunDurable)); got != dispatch.SessionConflict {
		t.Fatalf("pre-handoff fact = %q, want conflict", got)
	}
	if prober.calls != 0 {
		t.Fatalf("invalid handoff made %d probe calls", prober.calls)
	}
}

func TestOSAdapterFormsImplementDispatchTargetProbePort(t *testing.T) {
	var value ltmux.Adapter = ltmux.OSAdapter{}
	var pointer ltmux.Adapter = &ltmux.OSAdapter{}
	var remoteValue ltmux.Adapter = ltmux.OSAdapter{}.WithRunner(ltmux.SSHRunner{Host: "worker.example"})
	for name, adapter := range map[string]ltmux.Adapter{
		"value": value, "pointer": pointer, "remote value": remoteValue,
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := adapter.(dispatchTargetProber); !ok {
				t.Fatalf("%T does not implement dispatchTargetProber", adapter)
			}
		})
	}
}

type dispatchTargetProbeAdapter struct {
	noDispatchTargetProbeAdapter
	probe           ltmux.TargetProbe
	session, window string
	calls           int
}

func (a *dispatchTargetProbeAdapter) ProbeDispatchTarget(_ context.Context, session, window string) ltmux.TargetProbe {
	a.calls++
	a.session, a.window = session, window
	return a.probe
}

type noDispatchTargetProbeAdapter struct{ ltmux.Adapter }
