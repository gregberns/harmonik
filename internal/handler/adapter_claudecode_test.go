package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// adapter_claudecode_test.go — tests for ClaudeCodeAdapter (hk-prug5).
//
// Helper prefix: claudeCodeFixture (per implementer-protocol.md §Helper-prefix
// discipline).

// claudeCodeFixtureMakeEvent builds a minimal valid EventEnvelope with the
// given event type and payload bytes.
func claudeCodeFixtureMakeEvent(t *testing.T, eventType string, payload json.RawMessage) handlercontract.EventEnvelope {
	t.Helper()
	if payload == nil {
		payload = json.RawMessage(`{}`)
	}
	return handlercontract.EventEnvelope{
		EventID:         handlercontract.EventID(uuid.MustParse("0196f500-0000-7000-8000-000000000099")),
		SchemaVersion:   1,
		Type:            eventType,
		TimestampWall:   time.Now(),
		SourceSubsystem: "handler",
		Payload:         payload,
	}
}

// claudeCodeFixtureRetryPayload builds an agent_rate_limited payload with the
// given retry_after_seconds value.  Pass nil to omit the field.
func claudeCodeFixtureRetryPayload(t *testing.T, retryAfterSeconds *int) json.RawMessage {
	t.Helper()
	type rateLimitedMsg struct {
		RetryAfterSeconds *int `json:"retry_after_seconds,omitempty"`
	}
	raw, err := json.Marshal(rateLimitedMsg{RetryAfterSeconds: retryAfterSeconds})
	if err != nil {
		t.Fatalf("claudeCodeFixtureRetryPayload: marshal: %v", err)
	}
	return raw
}

// ptr is a generic helper that returns a pointer to a value.
func ptr[T any](v T) *T { return &v }

// ─────────────────────────────────────────────────────────────────────────────
// Interface satisfaction (compile-time)
// ─────────────────────────────────────────────────────────────────────────────

// TestClaudeCodeAdapter_ImplementsAdapterInterface verifies that
// ClaudeCodeAdapter satisfies handlercontract.Adapter at compile time.
func TestClaudeCodeAdapter_ImplementsAdapterInterface(t *testing.T) {
	t.Parallel()
	var _ handlercontract.Adapter = handler.ClaudeCodeAdapter{}
	var _ handlercontract.Adapter = handler.NewClaudeCodeAdapter()
}

// ─────────────────────────────────────────────────────────────────────────────
// DetectReady
// ─────────────────────────────────────────────────────────────────────────────

// TestClaudeCodeAdapter_DetectReady_TrueForAgentReady verifies that
// DetectReady returns true for an agent_ready event per HC-041.
func TestClaudeCodeAdapter_DetectReady_TrueForAgentReady(t *testing.T) {
	t.Parallel()

	a := handler.NewClaudeCodeAdapter()
	ev := claudeCodeFixtureMakeEvent(t, handlercontract.ProgressMsgTypeAgentReady, nil)
	if !a.DetectReady(ev) {
		t.Error("DetectReady returned false for agent_ready; want true")
	}
}

// TestClaudeCodeAdapter_DetectReady_FalseForNonAgentReady verifies that
// DetectReady returns false for all event types other than agent_ready per
// HC-041 ("MUST NOT synthesize ready-state from other signals").
func TestClaudeCodeAdapter_DetectReady_FalseForNonAgentReady(t *testing.T) {
	t.Parallel()

	nonReadyTypes := []string{
		handlercontract.ProgressMsgTypeAgentStarted,
		handlercontract.ProgressMsgTypeAgentOutputChunk,
		handlercontract.ProgressMsgTypeAgentFailed,
		handlercontract.ProgressMsgTypeAgentCompleted,
		handlercontract.ProgressMsgTypeHandlerCapabilities,
		handlercontract.ProgressMsgTypeAgentRateLimited,
		handlercontract.ProgressMsgTypeAgentRateLimitCleared,
		handlercontract.ProgressMsgTypeAgentHeartbeat,
		handlercontract.ProgressMsgTypeSessionLogLocation,
		handlercontract.ProgressMsgTypeSkillsProvisioned,
		handlercontract.ProgressMsgTypeOutcomeEmitted,
		"unknown_type",
		"",
	}

	a := handler.NewClaudeCodeAdapter()
	for _, evType := range nonReadyTypes {
		evType := evType
		t.Run(evType, func(t *testing.T) {
			t.Parallel()
			ev := claudeCodeFixtureMakeEvent(t, evType, nil)
			if a.DetectReady(ev) {
				t.Errorf("DetectReady returned true for %q; want false (HC-041: MUST NOT synthesize ready-state)", evType)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DetectRateLimit
// ─────────────────────────────────────────────────────────────────────────────

// TestClaudeCodeAdapter_DetectRateLimit_TrueForAgentRateLimited verifies
// (limited=true) for an agent_rate_limited event.
func TestClaudeCodeAdapter_DetectRateLimit_TrueForAgentRateLimited(t *testing.T) {
	t.Parallel()

	a := handler.NewClaudeCodeAdapter()
	ev := claudeCodeFixtureMakeEvent(t, handlercontract.ProgressMsgTypeAgentRateLimited, nil)
	limited, _ := a.DetectRateLimit(ev)
	if !limited {
		t.Error("DetectRateLimit returned limited=false for agent_rate_limited; want true")
	}
}

// TestClaudeCodeAdapter_DetectRateLimit_RetryAfterParsed verifies that
// retry_after_seconds in the payload is converted to a time.Duration.
func TestClaudeCodeAdapter_DetectRateLimit_RetryAfterParsed(t *testing.T) {
	t.Parallel()

	a := handler.NewClaudeCodeAdapter()
	pl := claudeCodeFixtureRetryPayload(t, ptr(30))
	ev := claudeCodeFixtureMakeEvent(t, handlercontract.ProgressMsgTypeAgentRateLimited, pl)

	limited, retryAfter := a.DetectRateLimit(ev)
	if !limited {
		t.Error("DetectRateLimit returned limited=false; want true")
	}
	want := 30 * time.Second
	if retryAfter != want {
		t.Errorf("retryAfter = %v; want %v", retryAfter, want)
	}
}

// TestClaudeCodeAdapter_DetectRateLimit_NoRetryAfterReturnsZero verifies that
// a missing retry_after_seconds yields (true, 0).
func TestClaudeCodeAdapter_DetectRateLimit_NoRetryAfterReturnsZero(t *testing.T) {
	t.Parallel()

	a := handler.NewClaudeCodeAdapter()
	pl := claudeCodeFixtureRetryPayload(t, nil)
	ev := claudeCodeFixtureMakeEvent(t, handlercontract.ProgressMsgTypeAgentRateLimited, pl)

	limited, retryAfter := a.DetectRateLimit(ev)
	if !limited {
		t.Error("DetectRateLimit returned limited=false; want true")
	}
	if retryAfter != 0 {
		t.Errorf("retryAfter = %v; want 0", retryAfter)
	}
}

// TestClaudeCodeAdapter_DetectRateLimit_FalseForNonRateLimited verifies that
// DetectRateLimit returns (false, 0) for all event types other than
// agent_rate_limited.
func TestClaudeCodeAdapter_DetectRateLimit_FalseForNonRateLimited(t *testing.T) {
	t.Parallel()

	nonRateLimitedTypes := []string{
		handlercontract.ProgressMsgTypeAgentReady,
		handlercontract.ProgressMsgTypeAgentStarted,
		handlercontract.ProgressMsgTypeAgentOutputChunk,
		handlercontract.ProgressMsgTypeAgentCompleted,
		handlercontract.ProgressMsgTypeAgentFailed,
		handlercontract.ProgressMsgTypeAgentRateLimitCleared,
		handlercontract.ProgressMsgTypeAgentHeartbeat,
		"unknown_event",
	}

	a := handler.NewClaudeCodeAdapter()
	for _, evType := range nonRateLimitedTypes {
		evType := evType
		t.Run(evType, func(t *testing.T) {
			t.Parallel()
			ev := claudeCodeFixtureMakeEvent(t, evType, nil)
			limited, retryAfter := a.DetectRateLimit(ev)
			if limited {
				t.Errorf("DetectRateLimit returned limited=true for %q; want false", evType)
			}
			if retryAfter != 0 {
				t.Errorf("DetectRateLimit returned retryAfter=%v for %q; want 0", retryAfter, evType)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CleanExitSequence
// ─────────────────────────────────────────────────────────────────────────────

// stubSession is a minimal handlercontract.Session stub that records SendInput calls.
type stubSession struct {
	inputs  []string
	sendErr error
}

func (s *stubSession) ID() handlercontract.SessionID { return "test-session" }
func (s *stubSession) Attach(_ context.Context) (io.Reader, error) {
	return nil, nil
}
func (s *stubSession) Kill(_ context.Context) error { return nil }
func (s *stubSession) Wait(_ context.Context) (handlercontract.Outcome, error) {
	return handlercontract.Outcome{}, nil
}
func (s *stubSession) LogLocation() string { return "" }
func (s *stubSession) SendInput(_ context.Context, input string) error {
	s.inputs = append(s.inputs, input)
	return s.sendErr
}

// TestClaudeCodeAdapter_CleanExitSequence_SendsSlashExit verifies that
// CleanExitSequence sends "/exit" via SendInput.
func TestClaudeCodeAdapter_CleanExitSequence_SendsSlashExit(t *testing.T) {
	t.Parallel()

	a := handler.NewClaudeCodeAdapter()
	sess := &stubSession{}
	if err := a.CleanExitSequence(context.Background(), sess); err != nil {
		t.Fatalf("CleanExitSequence returned error: %v", err)
	}
	if len(sess.inputs) != 1 || sess.inputs[0] != "/exit" {
		t.Errorf("CleanExitSequence sent %v; want [\"/exit\"]", sess.inputs)
	}
}

// TestClaudeCodeAdapter_CleanExitSequence_PropagatesSendError verifies that
// an error from SendInput is propagated (wrapped) by CleanExitSequence.
func TestClaudeCodeAdapter_CleanExitSequence_PropagatesSendError(t *testing.T) {
	t.Parallel()

	a := handler.NewClaudeCodeAdapter()
	wantErr := handlercontract.ErrStructural
	sess := &stubSession{sendErr: wantErr}
	err := a.CleanExitSequence(context.Background(), sess)
	if err == nil {
		t.Fatal("CleanExitSequence returned nil; want error")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error chain does not contain %v; got %v", wantErr, err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RotateAccount
// ─────────────────────────────────────────────────────────────────────────────

// TestClaudeCodeAdapter_RotateAccount_ReturnsErrSingleAccountOnly verifies
// that RotateAccount returns the ErrSingleAccountOnly sentinel and that it
// wraps ErrDeterministic per spec (§4.3.HC-013a).
func TestClaudeCodeAdapter_RotateAccount_ReturnsErrSingleAccountOnly(t *testing.T) {
	t.Parallel()

	a := handler.NewClaudeCodeAdapter()
	err := a.RotateAccount(context.Background())
	if err == nil {
		t.Fatal("RotateAccount returned nil; want ErrSingleAccountOnly")
	}
	if !errors.Is(err, handler.ErrSingleAccountOnly) {
		t.Errorf("RotateAccount error %v does not wrap ErrSingleAccountOnly", err)
	}
	if !errors.Is(err, handler.ErrDeterministic) {
		t.Errorf("RotateAccount error %v does not wrap ErrDeterministic (HC-013a requires deterministic sentinel)", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Register helper
// ─────────────────────────────────────────────────────────────────────────────

// TestClaudeCodeAdapter_Register_AddsToRegistry verifies that Register adds
// a ClaudeCodeAdapter under handlercontract.AgentTypeClaudeCode in a fresh registry.
func TestClaudeCodeAdapter_Register_AddsToRegistry(t *testing.T) {
	t.Parallel()

	reg := handlercontract.NewAdapterRegistry()
	if err := handler.Register(reg); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	types := reg.RegisteredTypes()
	found := false
	for _, at := range types {
		if at == handlercontract.AgentTypeClaudeCode {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("RegisteredTypes does not contain %q after Register", handlercontract.AgentTypeClaudeCode)
	}
}

// TestClaudeCodeAdapter_Register_DuplicateReturnsError verifies that calling
// Register twice on the same registry returns an error (HC-012 duplicate rule).
func TestClaudeCodeAdapter_Register_DuplicateReturnsError(t *testing.T) {
	t.Parallel()

	reg := handlercontract.NewAdapterRegistry()
	if err := handler.Register(reg); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}
	if err := handler.Register(reg); err == nil {
		t.Error("second Register returned nil; want error for duplicate registration")
	}
}

// TestClaudeCodeAdapter_Diagnose_ReturnsMVHMinimalReport verifies that
// ClaudeCodeAdapter.Diagnose returns a non-error DiagnosticReport with a
// non-empty Message and Healthy=false on a rate-limit pause at MVH.
//
// Acceptance criterion: "Test: claude-code adapter Diagnose run on rate-limit
// pause returns minimal report" (specs/handler-contract.md §4.3a HC-014a,
// bead hk-tvsl7).
func TestClaudeCodeAdapter_Diagnose_ReturnsMVHMinimalReport(t *testing.T) {
	t.Parallel()

	adapter := handler.NewClaudeCodeAdapter()
	report, err := adapter.Diagnose(context.Background())

	if err != nil {
		t.Fatalf("Diagnose returned unexpected error: %v (want nil)", err)
	}
	if report.Message == "" {
		t.Error("Diagnose returned empty Message; want non-empty diagnostic string")
	}
	if report.Healthy {
		t.Error("Diagnose returned Healthy=true; want false at MVH (no real-time probe)")
	}
}

// TestClaudeCodeAdapter_Diagnose_DoesNotReturnErrDeterministic verifies that
// ClaudeCodeAdapter.Diagnose does NOT return ErrDeterministic — the claude-code
// adapter supports the diagnostic seam (HC-014a).  Adapters that don't support
// it must return ErrDeterministic; claude-code must not.
func TestClaudeCodeAdapter_Diagnose_DoesNotReturnErrDeterministic(t *testing.T) {
	t.Parallel()

	adapter := handler.NewClaudeCodeAdapter()
	_, err := adapter.Diagnose(context.Background())

	if errors.Is(err, handlercontract.ErrDeterministic) {
		t.Error("ClaudeCodeAdapter.Diagnose returned ErrDeterministic; " +
			"the claude-code adapter supports diagnostics and must not return ErrDeterministic")
	}
}

// TestClaudeCodeAdapter_Register_ForAgentReturnsAdapter verifies that
// ForAgent returns the adapter after Register.
func TestClaudeCodeAdapter_Register_ForAgentReturnsAdapter(t *testing.T) {
	t.Parallel()

	reg := handlercontract.NewAdapterRegistry()
	if err := handler.Register(reg); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := reg.ForAgent(handlercontract.AgentTypeClaudeCode)
	if err != nil {
		t.Fatalf("ForAgent returned error: %v", err)
	}
	if got == nil {
		t.Error("ForAgent returned nil adapter")
	}
}
