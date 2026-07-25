package hook

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func hookFixtureRegisterSession(
	t *testing.T,
	store *SessionStore,
	runID, claudeSessionID, handlerSessionID string,
) *SessionRegistration {
	t.Helper()
	registration, err := store.RegisterSession(runID, claudeSessionID, handlerSessionID)
	if err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	return registration
}

func TestSessionRegistration_ReadyDeliveredExactlyOnce(t *testing.T) {
	t.Parallel()
	const (
		runID     = "run-registration-ready-once"
		sessionID = "session-registration-ready-once"
	)

	store := NewSessionStore()
	registration := hookFixtureRegisterSession(t, store, runID, sessionID, "handler-sess-1")

	var calls atomic.Int32
	if err := registration.SetAgentReadyCallback(func() {
		calls.Add(1)
	}); err != nil {
		t.Fatalf("SetAgentReadyCallback: %v", err)
	}

	ready := hookFixtureMakeEnvelope(runID, sessionID, "agent_ready", nil)
	for i := 0; i < 20; i++ {
		if ack := store.Dispatch(ready); ack.Status != "ok" {
			t.Fatalf("Dispatch(agent_ready) %d: status=%q reason=%q", i, ack.Status, ack.Reason)
		}
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("ready callback calls = %d, want exactly 1", got)
	}

	if err := registration.SetAgentReadyCallback(func() {
		calls.Add(1)
	}); err != nil {
		t.Fatalf("replace callback before seal: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("replacement callback redelivered ready: calls=%d, want 1", got)
	}
}

func TestSessionRegistration_LateCallbackReplaysReadyExactlyOnce(t *testing.T) {
	t.Parallel()
	const (
		runID     = "run-registration-ready-late"
		sessionID = "session-registration-ready-late"
	)

	store := NewSessionStore()
	registration := hookFixtureRegisterSession(t, store, runID, sessionID, "handler-sess-1")
	ready := hookFixtureMakeEnvelope(runID, sessionID, "agent_ready", nil)
	for i := 0; i < 10; i++ {
		store.Dispatch(ready)
	}

	var calls atomic.Int32
	if err := registration.SetAgentReadyCallback(func() {
		calls.Add(1)
	}); err != nil {
		t.Fatalf("SetAgentReadyCallback: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("late callback calls = %d, want exactly 1", got)
	}
}

func TestSessionRegistration_SealRejectsNewAdmission(t *testing.T) {
	t.Parallel()
	const (
		runID     = "run-registration-seal"
		sessionID = "session-registration-seal"
	)

	store := NewSessionStore()
	registration := hookFixtureRegisterSession(t, store, runID, sessionID, "handler-sess-1")
	if err := registration.SealNonterminal(t.Context()); err != nil {
		t.Fatalf("SealNonterminal: %v", err)
	}

	var calls atomic.Int32
	err := registration.SetAgentReadyCallback(func() {
		calls.Add(1)
	})
	if !errors.Is(err, ErrSessionRegistrationSealed) {
		t.Fatalf("SetAgentReadyCallback after seal: got %v, want ErrSessionRegistrationSealed", err)
	}

	ready := hookFixtureMakeEnvelope(runID, sessionID, "agent_ready", nil)
	if ack := store.Dispatch(ready); ack.Status != "ok" {
		t.Fatalf("Dispatch(agent_ready) after seal: status=%q reason=%q", ack.Status, ack.Reason)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("post-seal callback calls = %d, want 0", got)
	}
}

func TestSessionRegistration_SealWaitsForAdmittedCallback(t *testing.T) {
	t.Parallel()
	const (
		runID     = "run-registration-seal-wait"
		sessionID = "session-registration-seal-wait"
	)

	store := NewSessionStore()
	registration := hookFixtureRegisterSession(t, store, runID, sessionID, "handler-sess-1")
	entered := make(chan struct{})
	release := make(chan struct{})
	if err := registration.SetAgentReadyCallback(func() {
		close(entered)
		<-release
	}); err != nil {
		t.Fatalf("SetAgentReadyCallback: %v", err)
	}

	dispatchDone := make(chan struct{})
	go func() {
		defer close(dispatchDone)
		store.Dispatch(hookFixtureMakeEnvelope(runID, sessionID, "agent_ready", nil))
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("ready callback was not admitted")
	}

	sealDone := make(chan error, 1)
	go func() {
		sealDone <- registration.SealNonterminal(t.Context())
	}()
	select {
	case err := <-sealDone:
		t.Fatalf("SealNonterminal returned before admitted callback completed: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-sealDone:
		if err != nil {
			t.Fatalf("SealNonterminal after callback completion: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SealNonterminal did not return after admitted callback completed")
	}
	select {
	case <-dispatchDone:
	case <-time.After(2 * time.Second):
		t.Fatal("agent_ready dispatch did not return after callback completed")
	}
}

func TestSessionRegistration_SealTimeoutLeavesTerminalStateOpen(t *testing.T) {
	t.Parallel()
	const (
		runID     = "run-registration-seal-timeout"
		sessionID = "session-registration-seal-timeout"
	)

	store := NewSessionStore()
	registration := hookFixtureRegisterSession(t, store, runID, sessionID, "handler-sess-1")
	entered := make(chan struct{})
	release := make(chan struct{})
	if err := registration.SetAgentReadyCallback(func() {
		close(entered)
		<-release
	}); err != nil {
		t.Fatalf("SetAgentReadyCallback: %v", err)
	}
	go store.Dispatch(hookFixtureMakeEnvelope(runID, sessionID, "agent_ready", nil))
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("ready callback was not admitted")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := registration.SealNonterminal(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SealNonterminal timeout: got %v, want context.DeadlineExceeded", err)
	}

	outcome := hookFixtureMakeEnvelope(
		runID,
		sessionID,
		"outcome_emitted",
		hookFixtureMakePayload(t, "terminal after seal timeout"),
	)
	if ack := store.Dispatch(outcome); ack.Status != "ok" {
		t.Fatalf("terminal outcome after seal timeout: status=%q reason=%q", ack.Status, ack.Reason)
	}
	if got := store.LatestOutcome(runID, sessionID); got == nil {
		t.Fatal("terminal outcome was lost while nonterminal registration was sealed")
	}

	close(release)
	if err := registration.SealNonterminal(t.Context()); err != nil {
		t.Fatalf("SealNonterminal retry: %v", err)
	}
}

func TestSessionRegistration_TerminalOutcomeSurvivesNonterminalSeal(t *testing.T) {
	t.Parallel()
	const (
		runID     = "run-registration-terminal"
		sessionID = "session-registration-terminal"
	)

	store := NewSessionStore()
	registration := hookFixtureRegisterSession(t, store, runID, sessionID, "handler-sess-1")
	if err := registration.SealNonterminal(t.Context()); err != nil {
		t.Fatalf("SealNonterminal: %v", err)
	}

	want := hookFixtureMakePayload(t, "terminal survives seal")
	if ack := store.Dispatch(hookFixtureMakeEnvelope(runID, sessionID, "outcome_emitted", want)); ack.Status != "ok" {
		t.Fatalf("Dispatch(outcome_emitted): status=%q reason=%q", ack.Status, ack.Reason)
	}
	got := store.LatestOutcome(runID, sessionID)
	if got == nil {
		t.Fatal("LatestOutcome after nonterminal seal = nil, want terminal payload")
	}
	if summary := hookFixtureUnmarshal(t, *got)["summary"]; summary != "terminal survives seal" {
		t.Fatalf("LatestOutcome summary=%q, want %q", summary, "terminal survives seal")
	}

	if err := registration.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := store.LatestOutcome(runID, sessionID); got != nil {
		t.Fatal("LatestOutcome after Close != nil, want removed session")
	}
}

func TestSessionRegistration_CloseConcurrentAndIdempotent(t *testing.T) {
	t.Parallel()
	const (
		runID     = "run-registration-close"
		sessionID = "session-registration-close"
		closers   = 16
	)

	store := NewSessionStore()
	registration := hookFixtureRegisterSession(t, store, runID, sessionID, "handler-sess-1")
	duplicateHandle := hookFixtureRegisterSession(t, store, runID, sessionID, "handler-sess-1")

	entered := make(chan struct{})
	release := make(chan struct{})
	if err := registration.SetAgentReadyCallback(func() {
		close(entered)
		<-release
	}); err != nil {
		t.Fatalf("SetAgentReadyCallback: %v", err)
	}
	go store.Dispatch(hookFixtureMakeEnvelope(runID, sessionID, "agent_ready", nil))
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("ready callback was not admitted")
	}

	start := make(chan struct{})
	results := make(chan error, closers)
	var wg sync.WaitGroup
	for i := 0; i < closers; i++ {
		wg.Add(1)
		handle := registration
		if i%2 == 1 {
			handle = duplicateHandle
		}
		go func() {
			defer wg.Done()
			<-start
			results <- handle.Close(t.Context())
		}()
	}
	close(start)

	select {
	case err := <-results:
		t.Fatalf("Close returned before admitted callback completed: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Errorf("concurrent Close: %v", err)
		}
	}

	if err := registration.Close(t.Context()); err != nil {
		t.Fatalf("repeated Close: %v", err)
	}
	if err := duplicateHandle.Close(t.Context()); err != nil {
		t.Fatalf("repeated Close through duplicate handle: %v", err)
	}

	stale := store.Dispatch(hookFixtureMakeEnvelope(
		runID,
		sessionID,
		"outcome_emitted",
		hookFixtureMakePayload(t, "late terminal"),
	))
	if stale.Status != "unknown_session" {
		t.Fatalf("post-close terminal status=%q, want unknown_session", stale.Status)
	}
}

func TestSessionRegistration_OldHandleCannotCloseNewGeneration(t *testing.T) {
	t.Parallel()
	const (
		runID     = "run-registration-generation"
		sessionID = "session-registration-generation"
	)

	store := NewSessionStore()
	old := hookFixtureRegisterSession(t, store, runID, sessionID, "handler-generation-old")
	if err := old.Close(t.Context()); err != nil {
		t.Fatalf("old Close: %v", err)
	}

	current := hookFixtureRegisterSession(t, store, runID, sessionID, "handler-generation-new")
	if err := old.Close(t.Context()); err != nil {
		t.Fatalf("repeated old Close: %v", err)
	}

	payload := hookFixtureMakePayload(t, "new generation remains open")
	currentEnvelope := hookFixtureMakeEnvelope(runID, sessionID, "outcome_emitted", payload)
	currentEnvelope.HandlerSessionID = "handler-generation-new"
	if ack := store.Dispatch(currentEnvelope); ack.Status != "ok" {
		t.Fatalf("new generation Dispatch: status=%q reason=%q", ack.Status, ack.Reason)
	}
	if got := store.LatestOutcome(runID, sessionID); got == nil {
		t.Fatal("old handle closed the new session generation")
	}
	if err := current.Close(t.Context()); err != nil {
		t.Fatalf("current Close: %v", err)
	}
}

func TestSessionRegistration_StaleReadyRejectedAfterReregister(t *testing.T) {
	t.Parallel()
	const (
		runID        = "run-registration-stale-ready"
		sessionID    = "session-registration-stale-ready"
		oldHandlerID = "handler-registration-stale-ready-old"
		newHandlerID = "handler-registration-stale-ready-new"
	)

	store := NewSessionStore()
	old := hookFixtureRegisterSession(t, store, runID, sessionID, oldHandlerID)
	if err := old.Close(t.Context()); err != nil {
		t.Fatalf("old Close: %v", err)
	}

	current := hookFixtureRegisterSession(t, store, runID, sessionID, newHandlerID)
	var calls atomic.Int32
	if err := current.SetAgentReadyCallback(func() {
		calls.Add(1)
	}); err != nil {
		t.Fatalf("current SetAgentReadyCallback: %v", err)
	}

	stale := hookFixtureMakeEnvelope(runID, sessionID, "agent_ready", nil)
	stale.HandlerSessionID = oldHandlerID
	if ack := store.Dispatch(stale); ack.Status != "unknown_session" {
		t.Fatalf("stale ready status=%q reason=%q, want unknown_session", ack.Status, ack.Reason)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("stale prior-generation ready admitted callback: calls=%d, want 0", got)
	}

	fresh := hookFixtureMakeEnvelope(runID, sessionID, "agent_ready", nil)
	fresh.HandlerSessionID = newHandlerID
	if ack := store.Dispatch(fresh); ack.Status != "ok" {
		t.Fatalf("fresh ready status=%q reason=%q, want ok", ack.Status, ack.Reason)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fresh ready callback calls=%d, want 1", got)
	}

	if err := old.SetAgentReadyCallback(func() {
		calls.Add(1)
	}); !errors.Is(err, ErrSessionRegistrationSealed) {
		t.Fatalf("stale handle SetAgentReadyCallback: got %v, want ErrSessionRegistrationSealed", err)
	}
}

func TestSessionRegistration_StaleOutcomeRejectedAfterReregister(t *testing.T) {
	t.Parallel()
	const (
		runID        = "run-registration-stale-outcome"
		sessionID    = "session-registration-stale-outcome"
		oldHandlerID = "handler-registration-stale-outcome-old"
		newHandlerID = "handler-registration-stale-outcome-new"
	)

	store := NewSessionStore()
	old := hookFixtureRegisterSession(t, store, runID, sessionID, oldHandlerID)
	if err := old.Close(t.Context()); err != nil {
		t.Fatalf("old Close: %v", err)
	}
	current := hookFixtureRegisterSession(t, store, runID, sessionID, newHandlerID)

	stale := hookFixtureMakeEnvelope(
		runID,
		sessionID,
		"outcome_emitted",
		hookFixtureMakePayload(t, "stale prior-generation outcome"),
	)
	stale.HandlerSessionID = oldHandlerID
	if ack := store.Dispatch(stale); ack.Status != "unknown_session" {
		t.Fatalf("stale outcome status=%q reason=%q, want unknown_session", ack.Status, ack.Reason)
	}
	if got := store.LatestOutcome(runID, sessionID); got != nil {
		t.Fatal("stale prior-generation outcome contaminated current generation")
	}

	fresh := hookFixtureMakeEnvelope(
		runID,
		sessionID,
		"outcome_emitted",
		hookFixtureMakePayload(t, "fresh current-generation outcome"),
	)
	fresh.HandlerSessionID = newHandlerID
	if ack := store.Dispatch(fresh); ack.Status != "ok" {
		t.Fatalf("fresh outcome status=%q reason=%q, want ok", ack.Status, ack.Reason)
	}
	got := store.LatestOutcome(runID, sessionID)
	if got == nil {
		t.Fatal("fresh current-generation outcome was not recorded")
	}
	if summary := hookFixtureUnmarshal(t, *got)["summary"]; summary != "fresh current-generation outcome" {
		t.Fatalf("fresh outcome summary=%q, want %q", summary, "fresh current-generation outcome")
	}
	if err := current.Close(t.Context()); err != nil {
		t.Fatalf("current Close: %v", err)
	}
}

func TestSessionRegistration_BindsLegacyOpenWindow(t *testing.T) {
	t.Parallel()
	const (
		runID     = "run-registration-legacy-bind"
		sessionID = "session-registration-legacy-bind"
		handlerID = "handler-registration-legacy-bind"
	)

	store := NewSessionStore()
	store.RegisterHookSession(runID, sessionID)
	registration := hookFixtureRegisterSession(t, store, runID, sessionID, handlerID)

	stale := hookFixtureMakeEnvelope(runID, sessionID, "outcome_emitted", hookFixtureMakePayload(t, "wrong generation"))
	stale.HandlerSessionID = "handler-registration-legacy-other"
	if ack := store.Dispatch(stale); ack.Status != "unknown_session" {
		t.Fatalf("post-bind stale outcome status=%q, want unknown_session", ack.Status)
	}

	fresh := hookFixtureMakeEnvelope(runID, sessionID, "outcome_emitted", hookFixtureMakePayload(t, "bound generation"))
	fresh.HandlerSessionID = handlerID
	if ack := store.Dispatch(fresh); ack.Status != "ok" {
		t.Fatalf("post-bind fresh outcome status=%q reason=%q, want ok", ack.Status, ack.Reason)
	}
	if err := registration.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSessionRegistration_RejectsInvalidAndConflictingIdentity(t *testing.T) {
	t.Parallel()

	store := NewSessionStore()
	if _, err := store.RegisterSession("run", "claude", ""); !errors.Is(err, ErrSessionIdentityInvalid) {
		t.Fatalf("empty handler session identity: got %v, want ErrSessionIdentityInvalid", err)
	}

	first := hookFixtureRegisterSession(t, store, "run", "claude", "handler-one")
	if _, err := store.RegisterSession("run", "claude", "handler-two"); !errors.Is(err, ErrSessionRegistrationConflict) {
		t.Fatalf("conflicting live generation: got %v, want ErrSessionRegistrationConflict", err)
	}
	if err := first.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	legacy := NewSessionStore()
	legacy.RegisterHookSession("legacy-run", "legacy-claude")
	legacy.Dispatch(hookFixtureMakeEnvelope(
		"legacy-run",
		"legacy-claude",
		"outcome_emitted",
		hookFixtureMakePayload(t, "unbound state"),
	))
	if _, err := legacy.RegisterSession(
		"legacy-run",
		"legacy-claude",
		"handler-after-unbound-state",
	); !errors.Is(err, ErrSessionRegistrationConflict) {
		t.Fatalf("binding non-pristine legacy window: got %v, want ErrSessionRegistrationConflict", err)
	}
}
