package handlercontract_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// adapterreadydetect_hc041_test.go — sensor tests for HC-041
// (Adapter.DetectReady is agent_ready-gated) per bead hk-8i31.48.
//
// Spec refs: specs/handler-contract.md §4.9.HC-041, §6.1.
// Bead: hk-8i31.48.
//
// Verifies:
//   (a) DetectReady is part of the Adapter interface signature.
//   (b) A spec-compliant DetectReady implementation returns true ONLY for
//       agent_ready events and false for all other event types.
//   (c) Spec-corpus sensor: handler-contract.md contains HC-041 and the
//       "MUST NOT synthesize" clause.
//
// Helper prefix: readyDetectFixture (per implementer-protocol.md).

// readyDetectFixtureModuleRoot returns the module root by walking upward.
func readyDetectFixtureModuleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod in any parent directory")
		}
		dir = parent
	}
}

// readyDetectFixtureMakeEvent builds an EventEnvelope with the given type
// string and an empty JSON object payload. Sufficient for routing tests where
// the payload is not decoded.
func readyDetectFixtureMakeEvent(t *testing.T, eventType string) core.EventEnvelope {
	t.Helper()
	return core.EventEnvelope{
		EventID: core.EventID(uuid.MustParse("0196f500-0000-7000-8000-000000000001")),
		Type:    eventType,
		Payload: json.RawMessage(`{}`),
	}
}

// readyDetectFixtureAdapter is a minimal spec-compliant Adapter stub that
// implements DetectReady correctly per HC-041: returns true ONLY when the
// event type is "agent_ready".
//
// A real adapter would also scope to a specific session ID. This sensor
// omits session scoping to focus on event-type gating.
type readyDetectFixtureAdapter struct{}

func (readyDetectFixtureAdapter) DetectReady(event core.EventEnvelope) bool {
	// Per HC-041: return true ONLY for agent_ready events.
	// Adapters MUST NOT synthesize ready-state from other signals.
	return event.Type == handlercontract.ProgressMsgTypeAgentReady
}

func (readyDetectFixtureAdapter) DetectRateLimit(_ core.EventEnvelope) (bool, time.Duration) {
	return false, 0
}

func (readyDetectFixtureAdapter) CleanExitSequence(_ context.Context, _ handlercontract.Session) error {
	return nil
}

func (readyDetectFixtureAdapter) RotateAccount(_ context.Context) error {
	return nil
}

func (readyDetectFixtureAdapter) Diagnose(_ context.Context) (handlercontract.DiagnosticReport, error) {
	return handlercontract.DiagnosticReport{}, handlercontract.ErrDeterministic
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-041: DetectReady signature
// ─────────────────────────────────────────────────────────────────────────────

// TestReadyDetect_AdapterImplementsInterface verifies that a spec-compliant
// DetectReady implementation satisfies the Adapter interface (compile-time).
func TestReadyDetect_AdapterImplementsInterface(t *testing.T) {
	t.Parallel()

	var _ handlercontract.Adapter = readyDetectFixtureAdapter{}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-041: DetectReady returns true ONLY for agent_ready
// ─────────────────────────────────────────────────────────────────────────────

// TestReadyDetect_ReturnsTrueForAgentReady verifies that a HC-041-compliant
// adapter returns true for an agent_ready event.
func TestReadyDetect_ReturnsTrueForAgentReady(t *testing.T) {
	t.Parallel()

	adapter := readyDetectFixtureAdapter{}
	ev := readyDetectFixtureMakeEvent(t, handlercontract.ProgressMsgTypeAgentReady)

	if !adapter.DetectReady(ev) {
		t.Error("HC-041: DetectReady returned false for agent_ready event; want true")
	}
}

// TestReadyDetect_ReturnsFalseForNonAgentReadyEvents verifies that a
// HC-041-compliant adapter returns false for all other event types.
// Adapters MUST NOT synthesize ready-state from other signals per HC-041.
func TestReadyDetect_ReturnsFalseForNonAgentReadyEvents(t *testing.T) {
	t.Parallel()

	nonReadyTypes := []string{
		handlercontract.ProgressMsgTypeAgentStarted,
		handlercontract.ProgressMsgTypeAgentOutputChunk,
		handlercontract.ProgressMsgTypeAgentFailed,
		handlercontract.ProgressMsgTypeAgentCompleted,
		handlercontract.ProgressMsgTypeHandlerCapabilities,
		handlercontract.ProgressMsgTypeSkillsProvisioned,
		handlercontract.ProgressMsgTypeSessionLogLocation,
		handlercontract.ProgressMsgTypeAgentHeartbeat,
		handlercontract.ProgressMsgTypeOutcomeEmitted,
		// HC-041 hard rule: launch_initiated MUST NOT satisfy ready-state (hk-p63bz).
		handlercontract.ProgressMsgTypeLaunchInitiated,
	}

	adapter := readyDetectFixtureAdapter{}
	for _, evType := range nonReadyTypes {
		t.Run(evType, func(t *testing.T) {
			t.Parallel()
			ev := readyDetectFixtureMakeEvent(t, evType)
			if adapter.DetectReady(ev) {
				t.Errorf("HC-041: DetectReady returned true for %q; want false (MUST NOT synthesize ready-state from other signals)", evType)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-041: Spec-corpus sensor
// ─────────────────────────────────────────────────────────────────────────────

// TestReadyDetect_SpecCorpusHC041Clause verifies that handler-contract.md
// contains HC-041 and the "MUST NOT synthesize" ready-state constraint.
func TestReadyDetect_SpecCorpusHC041Clause(t *testing.T) {
	t.Parallel()

	root := readyDetectFixtureModuleRoot(t)
	specPath := filepath.Join(root, "specs", "handler-contract.md")

	content, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading handler-contract.md: %v", err)
	}
	specText := string(content)

	if !strings.Contains(specText, "HC-041") {
		t.Error("handler-contract.md missing HC-041 clause")
	}
	if !strings.Contains(specText, "MUST NOT synthesize") {
		t.Error("handler-contract.md missing 'MUST NOT synthesize' ready-state constraint; HC-041 may have drifted")
	}
}
