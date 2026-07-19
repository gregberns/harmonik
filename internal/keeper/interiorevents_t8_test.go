package keeper_test

// interiorevents_t8_test.go — T8 acceptance at the SHELL level: the four §8.20
// durable interior events (session_keeper_handoff_written / model_done /
// clear_sent / new_session_up) are emitted at their named transitions with the
// in-flight cycle_id and the pinned payload shapes (SK-012, 00b R1/R2), and
// the model-done signal works end-to-end through the reactive harness:
// .idle-marker primary, transcript backstop, and the 60s-class fail-open
// model_done_timeout (SK-014, here shrunk for a fast deterministic run).

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// newModelDoneCycler builds a reactive-harness Cycler with explicit control
// over the T8 model-done detection knobs (idle-marker read, transcript
// backstop, fail-open bound). Mirrors newReactiveCyclerWithBackstop.
func newModelDoneCycler(
	agent, projectDir, cycleID string,
	rs *reactiveSession,
	em keeper.Emitter,
	jc *journalCapture,
	managedSet *string,
	idleMarker func(string, string) (time.Time, bool),
	transcriptTurn func(string, string, string) (time.Time, bool),
	modelDoneTimeout time.Duration,
) *keeper.Cycler {
	var mu sync.Mutex
	cfg := keeper.CyclerConfig{
		AgentName:            agent,
		ProjectDir:           projectDir,
		TmuxTarget:           "fake-pane",
		ActPct:               90.0,
		WarnPct:              80.0,
		HandoffTimeout:       500 * time.Millisecond,
		ClearSettle:          300 * time.Millisecond,
		PollInterval:         5 * time.Millisecond,
		ClearConfirmBackstop: 900 * time.Millisecond,
		ClearConfirmRetries:  5,
		ModelDoneTimeout:     modelDoneTimeout,
		CycleIDGen:           func() string { return cycleID },
		IsManagedFn:          func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:            rs.readHandoff,
		HandoffModTimeFn:       rs.handoffModTime,
		TruncateHandoffFn:      rs.truncate,
		InjectFn:               rs.inject,
		ReadGaugeFn:            rs.readGauge,
		CrispIdleFn:            func(_, _ string) bool { return true },
		HoldingDispatchFn:      func(_, _ string) bool { return false },
		WriteJournalFn:         jc.write,
		SetTmuxEnvFn:           func(_ context.Context, _, _, _ string) error { return nil },
		IdleMarkerModTimeFn:    idleMarker,
		RecentTranscriptTurnFn: transcriptTurn,
		SetManagedSessionFn: func(_, _, sid string) error {
			mu.Lock()
			defer mu.Unlock()
			*managedSet = sid
			return nil
		},
	}
	return keeper.NewCycler(cfg, em)
}

// noIdleMarker models an agent whose Stop hook is not wired.
func noIdleMarker(_, _ string) (time.Time, bool) { return time.Time{}, false }

// noTranscriptTurn models an empty/absent session transcript.
func noTranscriptTurn(_, _, _ string) (time.Time, bool) { return time.Time{}, false }

// firstIndexOfType returns the global emission index of the first recorded
// event of the given type, or -1.
func firstIndexOfType(em *keeper.RecordingEmitter, typ core.EventType) int {
	for i, e := range em.Events {
		if e.Type == typ {
			return i
		}
	}
	return -1
}

// runReactiveModelDoneCycle drives one full cycle and returns the recorder.
func runReactiveModelDoneCycle(
	t *testing.T,
	agent, cycleID string,
	idleMarker func(string, string) (time.Time, bool),
	transcriptTurn func(string, string, string) (time.Time, bool),
	modelDoneTimeout time.Duration,
) (*keeper.RecordingEmitter, *reactiveSession, string) {
	t.Helper()
	s1, s2 := reactiveSIDs()
	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	var managedBinding string
	rs := newReactiveSession(s1, s2, true, true)
	cycler := newModelDoneCycler(
		agent, t.TempDir(), cycleID, rs, em, jc, &managedBinding,
		idleMarker, transcriptTurn, modelDoneTimeout,
	)
	cf := &keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s1}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}
	return em, rs, s1
}

// assertInteriorOrdering asserts SR3/SR4/SR6 over the emitted stream: the four
// interior events are present exactly once, share the in-flight cycle_id, and
// appear in handoff_written < model_done < clear_sent < new_session_up <
// cycle_complete order.
func assertInteriorOrdering(t *testing.T, em *keeper.RecordingEmitter, cycleID string) {
	t.Helper()
	order := []core.EventType{
		core.EventTypeSessionKeeperHandoffWritten,
		core.EventTypeSessionKeeperModelDone,
		core.EventTypeSessionKeeperClearSent,
		core.EventTypeSessionKeeperNewSessionUp,
		core.EventTypeSessionKeeperCycleComplete,
	}
	prev := -1
	for _, typ := range order {
		evts := em.EventsOfType(typ)
		if len(evts) != 1 {
			t.Fatalf("want exactly 1 %s; got %d", typ, len(evts))
		}
		var scope struct {
			CycleID string `json:"cycle_id"`
		}
		if err := json.Unmarshal(evts[0].Payload, &scope); err != nil {
			t.Fatalf("unmarshal %s: %v", typ, err)
		}
		if scope.CycleID != cycleID {
			t.Errorf("%s cycle_id = %q; want %q", typ, scope.CycleID, cycleID)
		}
		idx := firstIndexOfType(em, typ)
		if idx <= prev {
			t.Fatalf("%s emitted at %d, not after previous interior event at %d", typ, idx, prev)
		}
		prev = idx
	}
}

// TestCycler_InteriorEvents_IdleMarkerPath is the clean end-to-end T8 path:
// the Stop-hook .idle marker signals model-done (source "idle_marker", not
// degraded), and the four interior events land at their transitions in
// SR3/SR4/SR6 order, all carrying the cycle_id.
func TestCycler_InteriorEvents_IdleMarkerPath(t *testing.T) {
	t.Parallel()
	const cycleID = "cyc-t8-idle-001"
	em, rs, s1 := runReactiveModelDoneCycle(t, "t8-idle-agent", cycleID,
		func(_, _ string) (time.Time, bool) { return time.Now(), true }, // Stop hook freshly fired
		noTranscriptTurn,
		time.Minute, // bound never reached
	)

	assertInteriorOrdering(t, em, cycleID)

	var hw core.SessionKeeperHandoffWrittenPayload
	mustUnmarshalPayload(t, em, core.EventTypeSessionKeeperHandoffWritten, &hw)
	if hw.Nonce == "" || hw.Recovered || hw.SessionID != s1 || !hw.Valid() {
		t.Errorf("handoff_written = %+v; want nonce audit + session_id=%s, not recovered", hw, s1)
	}

	var md core.SessionKeeperModelDonePayload
	mustUnmarshalPayload(t, em, core.EventTypeSessionKeeperModelDone, &md)
	if md.Source != "idle_marker" || md.Degraded || !md.Valid() {
		t.Errorf("model_done = %+v; want source=idle_marker, not degraded", md)
	}

	var cs core.SessionKeeperClearSentPayload
	mustUnmarshalPayload(t, em, core.EventTypeSessionKeeperClearSent, &cs)
	if cs.Attempt != 1 || !cs.Valid() {
		t.Errorf("clear_sent = %+v; want attempt:1", cs)
	}

	var nsu core.SessionKeeperNewSessionUpPayload
	mustUnmarshalPayload(t, em, core.EventTypeSessionKeeperNewSessionUp, &nsu)
	_, s2 := reactiveSIDs()
	if nsu.PrevSessionID != s1 || nsu.NewSessionID != s2 || !nsu.Valid() {
		t.Errorf("new_session_up = %+v; want prev=%s new=%s", nsu, s1, s2)
	}

	if !rs.sawClear() {
		t.Error("reactive session never saw /clear")
	}
}

// TestCycler_InteriorEvents_TranscriptBackstop: no .idle marker (Stop hook
// unwired) but a fresh assistant transcript turn at/after t_nonce → model-done
// via the backstop (source "transcript_turn", not degraded).
func TestCycler_InteriorEvents_TranscriptBackstop(t *testing.T) {
	t.Parallel()
	const cycleID = "cyc-t8-transcript-001"
	em, _, _ := runReactiveModelDoneCycle(t, "t8-transcript-agent", cycleID,
		noIdleMarker,
		func(_, _, role string) (time.Time, bool) {
			if role != "assistant" {
				return time.Time{}, false
			}
			return time.Now().Add(time.Second), true // turn ts ≥ t_nonce
		},
		time.Minute, // bound never reached
	)

	assertInteriorOrdering(t, em, cycleID)
	var md core.SessionKeeperModelDonePayload
	mustUnmarshalPayload(t, em, core.EventTypeSessionKeeperModelDone, &md)
	if md.Source != "transcript_turn" || md.Degraded {
		t.Errorf("model_done = %+v; want source=transcript_turn, not degraded", md)
	}
}

// TestCycler_InteriorEvents_ModelDoneTimeout_FailOpen: neither source ever
// signals; the model_done_timeout fires and the cycle PROCEEDS to /clear +
// brief (SR9 — never silence), emitting model_done{source:"timeout",
// degraded:true} strictly before clear_sent (SR4 holds on the degraded path
// too).
func TestCycler_InteriorEvents_ModelDoneTimeout_FailOpen(t *testing.T) {
	t.Parallel()
	const cycleID = "cyc-t8-timeout-001"
	em, rs, _ := runReactiveModelDoneCycle(t, "t8-timeout-agent", cycleID,
		noIdleMarker,
		noTranscriptTurn,
		40*time.Millisecond, // shrunk fail-open bound
	)

	assertInteriorOrdering(t, em, cycleID)
	var md core.SessionKeeperModelDonePayload
	mustUnmarshalPayload(t, em, core.EventTypeSessionKeeperModelDone, &md)
	if md.Source != "timeout" || !md.Degraded {
		t.Errorf("model_done = %+v; want source=timeout degraded=true", md)
	}
	if !rs.sawClear() {
		t.Error("fail-open path never injected /clear (SR9: must proceed)")
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)); n != 0 {
		t.Errorf("want 0 cycle_aborted on the fail-open path; got %d", n)
	}
}

// mustUnmarshalPayload decodes the single recorded event of the given type.
func mustUnmarshalPayload(t *testing.T, em *keeper.RecordingEmitter, typ core.EventType, dst any) {
	t.Helper()
	evts := em.EventsOfType(typ)
	if len(evts) != 1 {
		t.Fatalf("want exactly 1 %s; got %d", typ, len(evts))
	}
	if err := json.Unmarshal(evts[0].Payload, dst); err != nil {
		t.Fatalf("unmarshal %s: %v", typ, err)
	}
}
