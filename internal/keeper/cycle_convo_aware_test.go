package keeper_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

func writeTranscriptLine(t *testing.T, transcriptDir, sessionID, role, ts string, isReal bool) {
	t.Helper()
	if err := os.MkdirAll(transcriptDir, 0o700); err != nil {
		t.Fatalf("mkdir transcriptDir: %v", err)
	}
	path := filepath.Join(transcriptDir, sessionID+".jsonl")

	var contentItems []map[string]string
	switch {
	case role == "user" && isReal:
		contentItems = []map[string]string{{"type": "text", "text": "hello"}}
	case role == "user" && !isReal:
		contentItems = []map[string]string{{"type": "tool_result", "tool_use_id": "x"}}
	case role == "assistant" && isReal:
		contentItems = []map[string]string{{"type": "text", "text": "here is my answer"}}
	case role == "assistant" && !isReal:
		contentItems = []map[string]string{{"type": "tool_use", "id": "y"}}
	}
	content, err := json.Marshal(contentItems)
	if err != nil {
		t.Fatalf("marshal transcript content: %v", err)
	}
	line := fmt.Sprintf(`{"type":%q,"timestamp":%q,"message":{"content":%s}}`,
		role, ts, content)
	//nolint:gosec // G304: path is constructed under this test's t.TempDir fixture.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open transcript: %v", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Errorf("close transcript: %v", closeErr)
		}
	}()
	if _, err := fmt.Fprintln(f, line); err != nil {
		t.Fatalf("write transcript line: %v", err)
	}
}

func recentTS(age time.Duration) string {
	return time.Now().UTC().Add(-age).Format(time.RFC3339Nano)
}

func newConvoAwareCycler(
	t *testing.T,
	agent, projectDir, transcriptDir, cycleID string,
	em keeper.Emitter,
	spy *cycleSpyInjector,
	jc *journalCapture,
	operatorTurnLookback, postAnswerGrace time.Duration,
) *keeper.Cycler {
	t.Helper()
	nonce := "<!-- KEEPER:" + cycleID + " -->"
	cfgOverrides := testCycleOverrides{CycleIDs: func() string { return cycleID }, HandoffPath: func(_, a string) string {
		return filepath.Join(projectDir, "HANDOFF-"+a+".md")
	}, HandoffRead: func(_ string) (string, error) { return "# Handoff\n\n" + nonce + "\n", nil }, HandoffScrub: func(_ string) error { return nil }, Inject: spy.inject, Gauge: func(_, _ string) (*keeper.CtxFile, time.Time, error) { return nil, time.Time{}, os.ErrNotExist }, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     projectDir,
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 200 * time.Millisecond,
		ClearSettle:    50 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,

		TranscriptDir:        transcriptDir,
		OperatorTurnLookback: operatorTurnLookback,
		PostAnswerGrace:      postAnswerGrace,
	}
	return mustNewCyclerWithOverrides(cfg, em, cfgOverrides)
}

// TestCycler_RecentOperatorTurn_SuppressesACT drives the INCIDENT: an agent at
// CrispIdle + above-act band with a real user turn in the transcript within the
// lookback window → the cycle MUST NOT fire ACT (today it does). This is the
// RED-first proof that Gate 5d is missing before the fix.
func TestCycler_RecentOperatorTurn_SuppressesACT(t *testing.T) {
	t.Parallel()

	const (
		agent   = "convo-aware-user-agent"
		cycleID = "cyc-convo-user"
		sid     = "11111111-1111-4111-8111-111111111111" // primarySID
	)

	projectDir := t.TempDir()
	transcriptDir := t.TempDir()
	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	writeTranscriptLine(t, transcriptDir, sid, "user", recentTS(30*time.Second), true)

	cycler := newConvoAwareCycler(t, agent, projectDir, transcriptDir, cycleID,
		em, spy, jc, 5*time.Minute, 0)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: sid}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if n := len(spy.texts()); n != 0 {
		t.Errorf("want 0 inject calls (Gate 5d should suppress ACT for recent operator turn); got %d: %v",
			n, spy.texts())
	}
}

// TestCycler_StaleOperatorTurn_DoesNotSuppress verifies that an OLD user turn
// (outside the lookback window) does NOT suppress the cycle.
func TestCycler_StaleOperatorTurn_DoesNotSuppress(t *testing.T) {
	t.Parallel()

	const (
		agent   = "convo-aware-stale-agent"
		cycleID = "cyc-convo-stale"
		prevSID = "11111111-1111-4111-8111-111111111111"
		newSID  = "22222222-2222-4222-8222-222222222222"
	)

	projectDir := t.TempDir()
	transcriptDir := t.TempDir()
	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	writeTranscriptLine(t, transcriptDir, prevSID, "user", recentTS(10*time.Minute), true)

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(1, nonce)
	readGaugeFn := gaugeReturnsNewSIDAfter(1, prevSID, newSID)
	cfgOverrides := testCycleOverrides{CycleIDs: func() string { return cycleID }, HandoffPath: func(_, a string) string {
		return filepath.Join(projectDir, "HANDOFF-"+a+".md")
	}, HandoffRead: readHandoff, HandoffScrub: func(_ string) error { return nil }, Inject: spy.inject, Gauge: readGaugeFn, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     projectDir,
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 200 * time.Millisecond,
		ClearSettle:    50 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,

		TranscriptDir:        transcriptDir,
		OperatorTurnLookback: 5 * time.Minute, // lookback shorter than 10m stale turn
		PostAnswerGrace:      0,
	}
	cycler := mustNewCyclerWithOverrides(cfg, em, cfgOverrides)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if n := len(spy.texts()); n < 2 {
		t.Errorf("want >=2 inject calls (stale turn should not suppress); got %d: %v",
			n, spy.texts())
	}
}

// TestCycler_ToolResultUserTurn_DoesNotSuppress verifies that a user turn
// consisting ONLY of tool_results (not a real operator message) does NOT
// trigger auto-hold.
func TestCycler_ToolResultUserTurn_DoesNotSuppress(t *testing.T) {
	t.Parallel()

	const (
		agent   = "convo-aware-tool-agent"
		cycleID = "cyc-convo-tool"
		prevSID = "11111111-1111-4111-8111-111111111111"
		newSID  = "22222222-2222-4222-8222-222222222222"
	)

	projectDir := t.TempDir()
	transcriptDir := t.TempDir()
	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	writeTranscriptLine(t, transcriptDir, prevSID, "user", recentTS(10*time.Second), false)

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(1, nonce)
	readGaugeFn := gaugeReturnsNewSIDAfter(1, prevSID, newSID)
	cfgOverrides := testCycleOverrides{CycleIDs: func() string { return cycleID }, HandoffPath: func(_, a string) string {
		return filepath.Join(projectDir, "HANDOFF-"+a+".md")
	}, HandoffRead: readHandoff, HandoffScrub: func(_ string) error { return nil }, Inject: spy.inject, Gauge: readGaugeFn, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     projectDir,
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 200 * time.Millisecond,
		ClearSettle:    50 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,

		TranscriptDir:        transcriptDir,
		OperatorTurnLookback: 5 * time.Minute,
		PostAnswerGrace:      0,
	}
	cycler := mustNewCyclerWithOverrides(cfg, em, cfgOverrides)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if n := len(spy.texts()); n < 2 {
		t.Errorf("want >=2 inject calls (tool_result turn must not suppress); got %d: %v",
			n, spy.texts())
	}
}

// TestCycler_OperatorTurnLookbackZero_DisablesGate5d verifies that setting
// OperatorTurnLookback to 0 disables Gate 5d entirely — even a very recent
// real user turn must not suppress ACT.
func TestCycler_OperatorTurnLookbackZero_DisablesGate5d(t *testing.T) {
	t.Parallel()

	const (
		agent   = "convo-lookback-zero-agent"
		cycleID = "cyc-convo-lookback-zero"
		prevSID = "33333333-3333-4333-8333-333333333333"
		newSID  = "44444444-4444-4444-8444-444444444444"
	)

	projectDir := t.TempDir()
	transcriptDir := t.TempDir()
	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	writeTranscriptLine(t, transcriptDir, prevSID, "user", recentTS(1*time.Second), true)

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(1, nonce)
	readGaugeFn := gaugeReturnsNewSIDAfter(1, prevSID, newSID)
	cfgOverrides := testCycleOverrides{CycleIDs: func() string { return cycleID }, HandoffPath: func(_, a string) string {
		return filepath.Join(projectDir, "HANDOFF-"+a+".md")
	}, HandoffRead: readHandoff, HandoffScrub: func(_ string) error { return nil }, Inject: spy.inject, Gauge: readGaugeFn, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     projectDir,
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 200 * time.Millisecond,
		ClearSettle:    50 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,

		TranscriptDir:        transcriptDir,
		OperatorTurnLookback: 0, // DISABLED — gate must not fire
		PostAnswerGrace:      0,
	}
	cycler := mustNewCyclerWithOverrides(cfg, em, cfgOverrides)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if n := len(spy.texts()); n < 2 {
		t.Errorf("want >=2 inject calls (lookback=0 must not suppress); got %d: %v",
			n, spy.texts())
	}
}

// TestCycler_Gate5d_WritesHoldMarker verifies that Gate 5d calls SetHold and
// creates the <agent>.hold.<sid> marker file when a recent operator turn
// is detected.
func TestCycler_Gate5d_DoesNotWriteHoldMarker(t *testing.T) {
	t.Parallel()

	const (
		agent   = "convo-hold-marker-agent"
		cycleID = "cyc-convo-hold-marker"
		sid     = "55555555-5555-4555-8555-555555555555"
	)

	projectDir := t.TempDir()
	transcriptDir := t.TempDir()
	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatalf("mkdir keeper dir: %v", err)
	}
	sidPath := filepath.Join(keeperDir, agent+".sid")
	if err := os.WriteFile(sidPath, []byte(sid+"\n"), 0o600); err != nil {
		t.Fatalf("write .sid: %v", err)
	}

	writeTranscriptLine(t, transcriptDir, sid, "user", recentTS(30*time.Second), true)

	cycler := newConvoAwareCycler(t, agent, projectDir, transcriptDir, cycleID,
		em, spy, jc, 5*time.Minute, 0)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: sid}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if n := len(spy.texts()); n != 0 {
		t.Errorf("want 0 inject calls (Gate 5d suppress); got %d", n)
	}

	holdPath := filepath.Join(keeperDir, agent+".hold."+sid)
	if _, err := os.Stat(holdPath); !os.IsNotExist(err) {
		t.Errorf("Gate 5d wrote persistent hold marker %q: %v", holdPath, err)
	}
}

// TestCycler_PostAnswerGrace_SuppressesACT verifies that a real assistant text
// turn within PostAnswerGrace suppresses the cycle.
func TestCycler_PostAnswerGrace_SuppressesACT(t *testing.T) {
	t.Parallel()

	const (
		agent   = "convo-grace-agent"
		cycleID = "cyc-convo-grace"
		sid     = "11111111-1111-4111-8111-111111111111"
	)

	projectDir := t.TempDir()
	transcriptDir := t.TempDir()
	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	writeTranscriptLine(t, transcriptDir, sid, "assistant", recentTS(5*time.Second), true)

	cycler := newConvoAwareCycler(t, agent, projectDir, transcriptDir, cycleID,
		em, spy, jc, 0, 30*time.Second)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: sid}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if n := len(spy.texts()); n != 0 {
		t.Errorf("want 0 inject calls (Gate 5e should suppress ACT within post-answer grace); got %d: %v",
			n, spy.texts())
	}
}

// TestCycler_PostAnswerGrace_Expired_DoesNotSuppress verifies that an OLD
// assistant turn (outside the grace window) does not suppress.
func TestCycler_PostAnswerGrace_Expired_DoesNotSuppress(t *testing.T) {
	t.Parallel()

	const (
		agent   = "convo-grace-expire-agent"
		cycleID = "cyc-convo-grace-expire"
		prevSID = "11111111-1111-4111-8111-111111111111"
		newSID  = "22222222-2222-4222-8222-222222222222"
	)

	projectDir := t.TempDir()
	transcriptDir := t.TempDir()
	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	writeTranscriptLine(t, transcriptDir, prevSID, "assistant", recentTS(2*time.Minute), true)

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(1, nonce)
	readGaugeFn := gaugeReturnsNewSIDAfter(1, prevSID, newSID)
	cfgOverrides := testCycleOverrides{CycleIDs: func() string { return cycleID }, HandoffPath: func(_, a string) string {
		return filepath.Join(projectDir, "HANDOFF-"+a+".md")
	}, HandoffRead: readHandoff, HandoffScrub: func(_ string) error { return nil }, Inject: spy.inject, Gauge: readGaugeFn, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     projectDir,
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 200 * time.Millisecond,
		ClearSettle:    50 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,

		TranscriptDir:        transcriptDir,
		OperatorTurnLookback: 0,
		PostAnswerGrace:      30 * time.Second,
	}
	cycler := mustNewCyclerWithOverrides(cfg, em, cfgOverrides)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if n := len(spy.texts()); n < 2 {
		t.Errorf("want >=2 inject calls (expired grace should not suppress); got %d: %v",
			n, spy.texts())
	}
}

// TestCycler_AssistantToolUseTurn_DoesNotTriggerGrace verifies that a pure
// tool_use assistant turn does NOT trigger the grace delay.
func TestCycler_AssistantToolUseTurn_DoesNotTriggerGrace(t *testing.T) {
	t.Parallel()

	const (
		agent   = "convo-grace-tooluse-agent"
		cycleID = "cyc-convo-grace-tooluse"
		prevSID = "11111111-1111-4111-8111-111111111111"
		newSID  = "22222222-2222-4222-8222-222222222222"
	)

	projectDir := t.TempDir()
	transcriptDir := t.TempDir()
	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	writeTranscriptLine(t, transcriptDir, prevSID, "assistant", recentTS(5*time.Second), false)

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	readHandoff := handoffReturnsNonceAfter(1, nonce)
	readGaugeFn := gaugeReturnsNewSIDAfter(1, prevSID, newSID)
	cfgOverrides := testCycleOverrides{CycleIDs: func() string { return cycleID }, HandoffPath: func(_, a string) string {
		return filepath.Join(projectDir, "HANDOFF-"+a+".md")
	}, HandoffRead: readHandoff, HandoffScrub: func(_ string) error { return nil }, Inject: spy.inject, Gauge: readGaugeFn, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     projectDir,
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 200 * time.Millisecond,
		ClearSettle:    50 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,

		TranscriptDir:        transcriptDir,
		OperatorTurnLookback: 0,
		PostAnswerGrace:      30 * time.Second,
	}
	cycler := mustNewCyclerWithOverrides(cfg, em, cfgOverrides)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: prevSID}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if n := len(spy.texts()); n < 2 {
		t.Errorf("want >=2 inject calls (tool_use assistant turn must not trigger grace); got %d: %v",
			n, spy.texts())
	}
}
