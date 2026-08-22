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

func TestOSAdapterBindDispatchTargetOptionsWritesAndVerifiesExactIdentity(t *testing.T) {
	runner := &RecordingRunner{CmdFunc: probeTargetCommand([]probeTargetResult{
		{},
		{},
		{},
		{},
		{output: "dispatch-session\n"},
		{output: "dispatch-window\n"},
		{output: "run-id\tclaim-id\tdispatch-session\tdispatch-window\t1234\t0\n"},
	})}
	adapter := OSAdapter{}.WithRunner(runner)
	if err := adapter.BindDispatchTargetOptions(
		t.Context(), "dispatch-session", "dispatch-window", "run-id", "claim-id",
	); err != nil {
		t.Fatal(err)
	}
	wantOptions := [][]string{
		{"set-option", "-w", "-o", "-t", "dispatch-session:dispatch-window", "@harmonik-run-id", "run-id"},
		{"set-option", "-w", "-o", "-t", "dispatch-session:dispatch-window", "@harmonik-claim-transition-id", "claim-id"},
		{"set-option", "-w", "-o", "-t", "dispatch-session:dispatch-window", "@harmonik-session-name", "dispatch-session"},
		{"set-option", "-w", "-o", "-t", "dispatch-session:dispatch-window", "@harmonik-window-name", "dispatch-window"},
	}
	for index, want := range wantOptions {
		if runner.Calls[index].Name != "tmux" || !reflect.DeepEqual(runner.Calls[index].Args, want) {
			t.Fatalf("call %d = %+v, want %v", index, runner.Calls[index], want)
		}
	}
	if len(runner.Calls) != 7 {
		t.Fatalf("call count = %d, want 7", len(runner.Calls))
	}
}

func TestOSAdapterBindDispatchTargetOptionsStopsOnWriteOrVerifyFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		results []probeTargetResult
	}{
		{name: "write", results: []probeTargetResult{{fail: true}, {fail: true}}},
		{name: "verify", results: []probeTargetResult{{}, {}, {}, {}, {output: "other\n"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &RecordingRunner{CmdFunc: probeTargetCommand(tc.results)}
			err := OSAdapter{}.WithRunner(runner).BindDispatchTargetOptions(
				t.Context(), "dispatch-session", "dispatch-window", "run-id", "claim-id",
			)
			if err == nil {
				t.Fatal("BindDispatchTargetOptions() = nil error")
			}
		})
	}
}

func TestOSAdapterBindDispatchTargetOptionsExactRetry(t *testing.T) {
	results := make([]probeTargetResult, 0, 11)
	for _, value := range []string{"run-id", "claim-id", "dispatch-session", "dispatch-window"} {
		results = append(results, probeTargetResult{fail: true}, probeTargetResult{output: value + "\n"})
	}
	results = append(results,
		probeTargetResult{output: "dispatch-session\n"},
		probeTargetResult{output: "dispatch-window\n"},
		probeTargetResult{output: "run-id\tclaim-id\tdispatch-session\tdispatch-window\t1234\t0\n"},
	)
	runner := &RecordingRunner{CmdFunc: probeTargetCommand(results)}
	if err := (OSAdapter{}).WithRunner(runner).BindDispatchTargetOptions(
		t.Context(), "dispatch-session", "dispatch-window", "run-id", "claim-id",
	); err != nil {
		t.Fatal(err)
	}
}

func TestOSAdapterBindDispatchTargetOptionsRejectsEveryExistingConflict(t *testing.T) {
	values := []string{"run-id", "claim-id", "dispatch-session", "dispatch-window"}
	for conflictAt := range values {
		t.Run(values[conflictAt], func(t *testing.T) {
			results := make([]probeTargetResult, 0, conflictAt*2+2)
			for index := 0; index <= conflictAt; index++ {
				results = append(results, probeTargetResult{fail: true})
				value := values[index]
				if index == conflictAt {
					value = "conflict"
				}
				results = append(results, probeTargetResult{output: value + "\n"})
			}
			runner := &RecordingRunner{CmdFunc: probeTargetCommand(results)}
			if err := (OSAdapter{}).WithRunner(runner).BindDispatchTargetOptions(
				t.Context(), "dispatch-session", "dispatch-window", "run-id", "claim-id",
			); err == nil {
				t.Fatal("BindDispatchTargetOptions() = nil error")
			}
		})
	}
}

func TestOSAdapterBindDispatchTargetOptionsRecoversEveryPartialPrefix(t *testing.T) {
	values := []string{"run-id", "claim-id", "dispatch-session", "dispatch-window"}
	for prefix := 1; prefix <= len(values); prefix++ {
		t.Run(values[prefix-1], func(t *testing.T) {
			results := make([]probeTargetResult, 0, prefix*2+(len(values)-prefix)+3)
			for index, value := range values {
				if index < prefix {
					results = append(results, probeTargetResult{fail: true}, probeTargetResult{output: value + "\n"})
				} else {
					results = append(results, probeTargetResult{})
				}
			}
			results = append(results,
				probeTargetResult{output: "dispatch-session\n"},
				probeTargetResult{output: "dispatch-window\n"},
				probeTargetResult{output: "run-id\tclaim-id\tdispatch-session\tdispatch-window\t1234\t0\n"},
			)
			runner := &RecordingRunner{CmdFunc: probeTargetCommand(results)}
			if err := (OSAdapter{}).WithRunner(runner).BindDispatchTargetOptions(
				t.Context(), "dispatch-session", "dispatch-window", "run-id", "claim-id",
			); err != nil {
				t.Fatal(err)
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
		//nolint:gosec // G204: printf and its arguments are literals from this file's result table.
		return exec.CommandContext(ctx, "printf", "%s", result.output)
	}
}
