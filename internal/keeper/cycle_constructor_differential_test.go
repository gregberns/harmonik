package keeper

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/substrate"
)

type constructorDifferentialResult struct {
	Injections []string
	Phases     []string
	Final      CycleJournal
	Events     []EmittedEvent
	Err        string
}

func newDifferentialCycler(t *testing.T, cfg CyclerConfig, emitter Emitter, legacy bool) *Cycler {
	t.Helper()
	if legacy {
		return NewCycler(cfg, emitter)
	}
	cycler, err := NewCyclerWithDeps(
		CyclePolicyFromConfig(cfg), CycleEnvFromConfig(cfg), CycleDepsFromConfig(cfg, emitter),
	)
	if err != nil {
		t.Fatal(err)
	}
	return cycler
}

func runConstructorTimeoutFixture(t *testing.T, legacy bool) constructorDifferentialResult {
	t.Helper()
	clock := substrate.NewFakeClock(time.Unix(1_700_000_000, 0))
	injected := make(chan string, 4)
	var mu sync.Mutex
	result := constructorDifferentialResult{}
	cfg := CyclerConfig{
		AgentName: "differential", ProjectDir: "/tmp/keeper-differential", TmuxTarget: "differential:0",
		Clock: clock, PollInterval: time.Second, HandoffTimeout: 3 * time.Second,
		CycleIDGen: func() string { return "cyc-differential" },
		HandoffFilePath: func(string, string) string {
			return "/tmp/HANDOFF-differential.md"
		},
		InjectFn: func(_ context.Context, _, text string) error {
			mu.Lock()
			result.Injections = append(result.Injections, text)
			mu.Unlock()
			injected <- text
			return nil
		},
		IsManagedFn:        func(string, string) bool { return true },
		CrispIdleFn:        func(string, string) bool { return true },
		HoldingDispatchFn:  func(string, string) bool { return false },
		SleepingCheckFn:    func(string, string) bool { return false },
		HeldCheckFn:        func(string, string) bool { return false },
		OperatorAttachedFn: func(string) bool { return false },
		ReadHandoff:        func(string) (string, error) { return "", nil },
		WriteJournalFn: func(_ string, journal *CycleJournal) error {
			mu.Lock()
			result.Phases = append(result.Phases, journal.Phase)
			result.Final = *journal
			mu.Unlock()
			return nil
		},
	}
	emitter := &RecordingEmitter{}
	cycler := newDifferentialCycler(t, cfg, emitter, legacy)
	done := make(chan error, 1)
	go func() {
		done <- cycler.MaybeRun(context.Background(), &CtxFile{
			Pct: 90, SessionID: "11111111-1111-4111-8111-111111111111",
		})
	}()
	select {
	case <-injected:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handoff injection")
	}
	time.Sleep(10 * time.Millisecond)
	clock.Advance(cfg.HandoffTimeout)
	if err := <-done; err != nil {
		result.Err = err.Error()
	}
	result.Events = append(result.Events, emitter.Events...)
	return result
}

func runConstructorRecoveryFixture(t *testing.T, legacy bool) constructorDifferentialResult {
	t.Helper()
	clock := substrate.NewFakeClock(time.Unix(1_700_000_000, 0))
	var mu sync.Mutex
	result := constructorDifferentialResult{}
	journal := &CycleJournal{
		CycleID: "cyc-differential", Phase: "cleared",
		OpenedAt: clock.Now().Add(-5 * time.Minute), UpdatedAt: clock.Now().Add(-5 * time.Minute),
	}
	cfg := CyclerConfig{
		AgentName: "differential", ProjectDir: "/tmp/keeper-differential", TmuxTarget: "differential:0",
		Clock: clock, PollInterval: time.Second,
		CycleIDGen: func() string { return "cyc-unused" },
		InjectFn: func(_ context.Context, _, text string) error {
			mu.Lock()
			result.Injections = append(result.Injections, text)
			mu.Unlock()
			return nil
		},
		IsManagedFn:         func(string, string) bool { return true },
		CrispIdleFn:         func(string, string) bool { return true },
		HoldingDispatchFn:   func(string, string) bool { return false },
		SleepingCheckFn:     func(string, string) bool { return false },
		HeldCheckFn:         func(string, string) bool { return false },
		OperatorAttachedFn:  func(string) bool { return false },
		IdleMarkerModTimeFn: func(string, string) (time.Time, bool) { return clock.Now(), true },
		ReadJournalFn:       func(string) (*CycleJournal, error) { copy := *journal; return &copy, nil },
		WriteJournalFn: func(_ string, written *CycleJournal) error {
			mu.Lock()
			result.Phases = append(result.Phases, written.Phase)
			result.Final = *written
			mu.Unlock()
			return nil
		},
	}
	emitter := &RecordingEmitter{}
	cycler := newDifferentialCycler(t, cfg, emitter, legacy)
	if err := cycler.RecoverFromCrash(context.Background()); err != nil {
		result.Err = err.Error()
	}
	result.Events = append(result.Events, emitter.Events...)
	return result
}

func TestLegacyAndNarrowConstructorsMatchHandoffTimeout(t *testing.T) {
	legacy := runConstructorTimeoutFixture(t, true)
	narrow := runConstructorTimeoutFixture(t, false)
	if !reflect.DeepEqual(narrow, legacy) {
		t.Fatalf("constructor behavior changed\nlegacy: %#v\nnarrow: %#v", legacy, narrow)
	}
	if narrow.Final.Phase != "aborted" || narrow.Final.Reason != "handoff_timeout" {
		t.Fatalf("timeout fixture ended as %+v", narrow.Final)
	}
	for _, event := range narrow.Events {
		if event.Type == core.EventTypeSessionKeeperCycleComplete {
			t.Fatalf("timeout emitted cycle complete: %+v", narrow.Events)
		}
	}
}

func TestLegacyAndNarrowConstructorsMatchClearedCrashRecovery(t *testing.T) {
	legacy := runConstructorRecoveryFixture(t, true)
	narrow := runConstructorRecoveryFixture(t, false)
	if !reflect.DeepEqual(narrow, legacy) {
		t.Fatalf("constructor behavior changed\nlegacy: %#v\nnarrow: %#v", legacy, narrow)
	}
	if narrow.Final.Phase != "complete" {
		t.Fatalf("recovery fixture ended as %+v", narrow.Final)
	}
	if len(narrow.Injections) != 1 || len(narrow.Events) != 1 ||
		narrow.Events[0].Type != core.EventTypeSessionKeeperCycleRecovered {
		t.Fatalf("recovery effects = injections %d, events %+v", len(narrow.Injections), narrow.Events)
	}
}
