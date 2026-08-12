package tmux

import (
	"context"
	"os/exec"
	"reflect"
	"testing"
)

func TestOSAdapterProbeDispatchTargetExact(t *testing.T) {
	runner := &RecordingRunner{CmdFunc: probeTargetCommand([]probeTargetResult{
		{output: "dispatch-session\n"},
		{output: "dispatch-window\n"},
		{output: "run-id\tclaim-id\tdispatch-session\tdispatch-window\t1234\t0\n"},
	})}
	got := OSAdapter{}.WithRunner(runner).ProbeDispatchTarget(t.Context(), "dispatch-session", "dispatch-window")
	want := TargetProbe{
		Status: TargetProbeExact, RunID: "run-id", ClaimTransitionID: "claim-id",
		SessionName: "dispatch-session", WindowName: "dispatch-window", PanePID: "1234", PaneDead: "0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("probe = %+v, want %+v", got, want)
	}
	wantCalls := []RecordingCall{
		{Name: "tmux", Args: []string{"list-sessions", "-F", "#{session_name}"}},
		{Name: "tmux", Args: []string{"list-windows", "-t", "dispatch-session", "-F", "#{window_name}"}},
		{Name: "tmux", Args: []string{
			"list-panes", "-t", "dispatch-session:dispatch-window", "-F",
			"#{@harmonik-run-id}\t#{@harmonik-claim-transition-id}\t" +
				"#{@harmonik-session-name}\t#{@harmonik-window-name}\t#{pane_pid}\t#{pane_dead}",
		}},
	}
	if !reflect.DeepEqual(runner.Calls, wantCalls) {
		t.Fatalf("calls = %+v", runner.Calls)
	}
}

func TestOSAdapterProbeDispatchTargetClosedOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		results []probeTargetResult
		want    TargetProbeStatus
	}{
		{name: "session absent", results: []probeTargetResult{{output: "other\n"}}, want: TargetProbeSessionAbsent},
		{name: "duplicate session", results: []probeTargetResult{{output: "dispatch-session\ndispatch-session\n"}}, want: TargetProbeDuplicate},
		{name: "session unreadable", results: []probeTargetResult{{fail: true}}, want: TargetProbeSessionUnreadable},
		{name: "window absent", results: []probeTargetResult{{output: "dispatch-session\n"}, {output: "other\n"}}, want: TargetProbeWindowAbsent},
		{name: "duplicate window", results: []probeTargetResult{{output: "dispatch-session\n"}, {output: "dispatch-window\ndispatch-window\n"}}, want: TargetProbeDuplicate},
		{name: "window unreadable", results: []probeTargetResult{{output: "dispatch-session\n"}, {fail: true}}, want: TargetProbeWindowUnreadable},
		{name: "pane absent", results: []probeTargetResult{{output: "dispatch-session\n"}, {output: "dispatch-window\n"}, {}}, want: TargetProbePaneAbsent},
		{name: "duplicate pane", results: []probeTargetResult{{output: "dispatch-session\n"}, {output: "dispatch-window\n"}, {output: "a\nb\n"}}, want: TargetProbeDuplicate},
		{name: "pane unreadable", results: []probeTargetResult{{output: "dispatch-session\n"}, {output: "dispatch-window\n"}, {fail: true}}, want: TargetProbePaneUnreadable},
		{name: "pane malformed", results: []probeTargetResult{{output: "dispatch-session\n"}, {output: "dispatch-window\n"}, {output: "too\tfew\n"}}, want: TargetProbePaneUnreadable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &RecordingRunner{CmdFunc: probeTargetCommand(tc.results)}
			got := OSAdapter{}.WithRunner(runner).ProbeDispatchTarget(t.Context(), "dispatch-session", "dispatch-window")
			if got.Status != tc.want {
				t.Fatalf("status = %q, want %q", got.Status, tc.want)
			}
			if got.Status != TargetProbeExact && got != (TargetProbe{Status: tc.want}) {
				t.Fatalf("failure probe carries values: %+v", got)
			}
		})
	}
}

type probeTargetResult struct {
	output string
	fail   bool
}

func probeTargetCommand(results []probeTargetResult) func(context.Context, string, ...string) *exec.Cmd {
	index := 0
	return func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		result := results[index]
		index++
		if result.fail {
			return exec.CommandContext(ctx, "sh", "-c", "exit 1")
		}
		return exec.CommandContext(ctx, "printf", "%s", result.output)
	}
}
