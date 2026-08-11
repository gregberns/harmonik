package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/substrate"
)

func receiveKeeperTestValue[T any](t *testing.T, ch <-chan T, label string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
		var zero T
		return zero
	}
}

func TestConstructKeeperCyclerCompletesThroughCommandComposition(t *testing.T) {
	clock := substrate.NewFakeClock(time.Unix(1_700_000_000, 0))
	injected := make(chan string, 8)
	phases := make(chan string, 16)
	gauge := &keeper.CtxFile{Pct: 90, SessionID: "11111111-1111-4111-8111-111111111111"}
	handoff := ""
	idleMarker := time.Time{}
	var journal *keeper.CycleJournal

	cfg := keeper.CyclerConfig{
		AgentName: "command-scenario", ProjectDir: t.TempDir(), TmuxTarget: "scenario:0",
		Clock: clock, PollInterval: 100 * time.Millisecond, ClearSettle: time.Second,
		HandoffTimeout: 30 * time.Second, ModelDoneTimeout: 30 * time.Second,
		CycleIDGen:         func() string { return "cyc-command" },
		InjectFn:           func(_ context.Context, _, text string) error { injected <- text; return nil },
		IsManagedFn:        func(string, string) bool { return true },
		CrispIdleFn:        func(string, string) bool { return true },
		HoldingDispatchFn:  func(string, string) bool { return false },
		SleepingCheckFn:    func(string, string) bool { return false },
		HeldCheckFn:        func(string, string) bool { return false },
		OperatorAttachedFn: func(string) bool { return false },
		ReadGaugeFn: func(string, string) (*keeper.CtxFile, time.Time, error) {
			return gauge, clock.Now(), nil
		},
		HandoffFilePath: func(string, string) string { return "/tmp/HANDOFF-command-scenario.md" },
		ReadHandoff:     func(string) (string, error) { return handoff, nil },
		IdleMarkerModTimeFn: func(string, string) (time.Time, bool) {
			return idleMarker, !idleMarker.IsZero()
		},
		WriteJournalFn: func(_ string, value *keeper.CycleJournal) error {
			saved := *value
			journal = &saved
			phases <- value.Phase
			return nil
		},
		ReadJournalFn: func(string) (*keeper.CycleJournal, error) {
			return nil, errors.New("no recovery journal")
		},
	}
	emitter := &keeper.RecordingEmitter{}
	cycler, err := constructKeeperCycler(cfg, emitter)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- cycler.MaybeRun(context.Background(), gauge) }()
	command := receiveKeeperTestValue(t, injected, "handoff injection")
	if !strings.Contains(command, "KEEPER:cyc-command") {
		t.Fatalf("handoff command = %q", command)
	}

	handoff = "# handoff\n<!-- KEEPER:cyc-command -->\n"
	time.Sleep(10 * time.Millisecond)
	clock.Advance(cfg.PollInterval)
	for receiveKeeperTestValue(t, phases, "confirmed phase") != "confirmed" {
	}
	idleMarker = clock.Now()
	time.Sleep(10 * time.Millisecond)
	clock.Advance(cfg.PollInterval)
	if clear := receiveKeeperTestValue(t, injected, "clear injection"); clear != "/clear" {
		t.Fatalf("clear injection = %q", clear)
	}

	gauge = &keeper.CtxFile{Pct: 2, SessionID: "22222222-2222-4222-8222-222222222222"}
	time.Sleep(10 * time.Millisecond)
	clock.Advance(cfg.PollInterval)
	brief := receiveKeeperTestValue(t, injected, "brief injection")
	if !strings.Contains(brief, "harmonik agent brief --wake keeper-restart") {
		t.Fatalf("brief injection = %q", brief)
	}
	if err := receiveKeeperTestValue(t, done, "cycle completion"); err != nil {
		t.Fatal(err)
	}
	if journal == nil || journal.Phase != "complete" {
		t.Fatalf("journal = %+v, want complete", journal)
	}
	if got := len(emitter.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); got != 1 {
		t.Fatalf("cycle-complete events = %d, want 1", got)
	}
}
