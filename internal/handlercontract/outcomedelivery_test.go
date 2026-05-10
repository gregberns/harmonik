package handlercontract_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// outcomeDelivery — per-bead helper prefix for test helpers in this file.
// crashClassFixture — per-bead helper prefix for ClassifyCrash test helpers (hk-8i31.28).

// outcomeDeliveryRoundTrip is a helper that encodes msg to JSON and decodes it
// back into a new OutcomeEmittedMsg, returning the round-tripped value or
// calling t.Fatalf on any error.
func outcomeDeliveryRoundTrip(t *testing.T, msg handlercontract.OutcomeEmittedMsg) handlercontract.OutcomeEmittedMsg {
	t.Helper()
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("outcomeDeliveryRoundTrip: marshal: %v", err)
	}
	var out handlercontract.OutcomeEmittedMsg
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("outcomeDeliveryRoundTrip: unmarshal: %v", err)
	}
	return out
}

// outcomeDeliveryPtr returns a pointer to s (helper to build *string fields).
func outcomeDeliveryPtr(s string) *string { return &s }

// ─────────────────────────────────────────────────────────────────────────────
// OutcomeDeliveryState constants
// ─────────────────────────────────────────────────────────────────────────────

func TestOutcomeDeliveryStateValues(t *testing.T) {
	t.Parallel()

	t.Run("not_yet_delivered_is_zero", func(t *testing.T) {
		t.Parallel()
		var s handlercontract.OutcomeDeliveryState
		if s != handlercontract.OutcomeNotYetDelivered {
			t.Error("zero value of OutcomeDeliveryState must equal OutcomeNotYetDelivered")
		}
	})

	t.Run("delivered_is_non_zero", func(t *testing.T) {
		t.Parallel()
		s := handlercontract.OutcomeDelivered
		if s == handlercontract.OutcomeNotYetDelivered {
			t.Error("OutcomeDelivered must not equal OutcomeNotYetDelivered")
		}
	})

	t.Run("transition_from_not_delivered_to_delivered", func(t *testing.T) {
		t.Parallel()
		s := handlercontract.OutcomeNotYetDelivered
		s = handlercontract.OutcomeDelivered
		if s != handlercontract.OutcomeDelivered {
			t.Error("OutcomeDeliveryState transition failed")
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// ClassifyExit
// ─────────────────────────────────────────────────────────────────────────────

func TestClassifyExit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		exitCode  int
		state     handlercontract.OutcomeDeliveryState
		wantNil   bool
		wantClass string // non-empty when wantNil==false
	}{
		{
			name:     "clean_exit_after_outcome_delivered",
			exitCode: 0,
			state:    handlercontract.OutcomeDelivered,
			wantNil:  true,
		},
		{
			name:     "dirty_exit_after_outcome_delivered_is_nil",
			exitCode: 1,
			state:    handlercontract.OutcomeDelivered,
			wantNil:  true,
		},
		{
			name:     "large_nonzero_exit_after_outcome_delivered_is_nil",
			exitCode: 137,
			state:    handlercontract.OutcomeDelivered,
			wantNil:  true,
		},
		{
			name:      "clean_exit_without_outcome_is_structural",
			exitCode:  0,
			state:     handlercontract.OutcomeNotYetDelivered,
			wantNil:   false,
			wantClass: "structural",
		},
		{
			name:      "nonzero_exit_without_outcome_is_structural",
			exitCode:  1,
			state:     handlercontract.OutcomeNotYetDelivered,
			wantNil:   false,
			wantClass: "structural",
		},
		{
			name:      "sigkill_exit_without_outcome_is_structural",
			exitCode:  137,
			state:     handlercontract.OutcomeNotYetDelivered,
			wantNil:   false,
			wantClass: "structural",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := handlercontract.ClassifyExit(tc.exitCode, tc.state)
			if tc.wantNil {
				if err != nil {
					t.Errorf("ClassifyExit(%d, %v) = %v, want nil", tc.exitCode, tc.state, err)
				}
				return
			}
			if err == nil {
				t.Errorf("ClassifyExit(%d, %v) = nil, want non-nil error", tc.exitCode, tc.state)
				return
			}
			got := handlercontract.Class(err)
			if got != tc.wantClass {
				t.Errorf("Class(ClassifyExit(%d, %v)) = %q, want %q", tc.exitCode, tc.state, got, tc.wantClass)
			}
		})
	}
}

func TestClassifyExitReturnsErrStructural(t *testing.T) {
	t.Parallel()

	// The returned error must wrap ErrStructural directly for narrowest-first
	// dispatch (HC-020).
	err := handlercontract.ClassifyExit(1, handlercontract.OutcomeNotYetDelivered)
	if !errors.Is(err, handlercontract.ErrStructural) {
		t.Errorf("ClassifyExit crash: errors.Is(err, ErrStructural) = false, want true")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// OutcomeEmittedMsg JSON round-trip
// ─────────────────────────────────────────────────────────────────────────────

func TestOutcomeEmittedMsgRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("minimal_fields", func(t *testing.T) {
		t.Parallel()
		orig := handlercontract.OutcomeEmittedMsg{
			Type:          "outcome_emitted",
			RunID:         "run-1",
			SessionID:     "sess-1",
			NodeID:        "node-1",
			OutcomeStatus: "SUCCESS",
		}
		got := outcomeDeliveryRoundTrip(t, orig)
		if got.Type != orig.Type {
			t.Errorf("Type: got %q, want %q", got.Type, orig.Type)
		}
		if got.RunID != orig.RunID {
			t.Errorf("RunID: got %q, want %q", got.RunID, orig.RunID)
		}
		if got.SessionID != orig.SessionID {
			t.Errorf("SessionID: got %q, want %q", got.SessionID, orig.SessionID)
		}
		if got.NodeID != orig.NodeID {
			t.Errorf("NodeID: got %q, want %q", got.NodeID, orig.NodeID)
		}
		if got.OutcomeStatus != orig.OutcomeStatus {
			t.Errorf("OutcomeStatus: got %q, want %q", got.OutcomeStatus, orig.OutcomeStatus)
		}
		if got.OutcomeKind != "" {
			t.Errorf("OutcomeKind: got %q, want empty (omitempty)", got.OutcomeKind)
		}
		if got.PreferredLabel != nil {
			t.Errorf("PreferredLabel: got %v, want nil", got.PreferredLabel)
		}
		if got.SuggestedNextIDs != nil {
			t.Errorf("SuggestedNextIDs: got %v, want nil", got.SuggestedNextIDs)
		}
	})

	t.Run("all_fields", func(t *testing.T) {
		t.Parallel()
		label := "retry-path"
		orig := handlercontract.OutcomeEmittedMsg{
			Type:             "outcome_emitted",
			RunID:            "run-abc",
			SessionID:        "sess-abc",
			NodeID:           "node-abc",
			OutcomeStatus:    "FAIL",
			OutcomeKind:      "reconciliation_verdict",
			PreferredLabel:   outcomeDeliveryPtr(label),
			SuggestedNextIDs: []string{"node-x", "node-y"},
		}
		got := outcomeDeliveryRoundTrip(t, orig)
		if got.OutcomeStatus != orig.OutcomeStatus {
			t.Errorf("OutcomeStatus: got %q, want %q", got.OutcomeStatus, orig.OutcomeStatus)
		}
		if got.OutcomeKind != orig.OutcomeKind {
			t.Errorf("OutcomeKind: got %q, want %q", got.OutcomeKind, orig.OutcomeKind)
		}
		if got.PreferredLabel == nil || *got.PreferredLabel != label {
			t.Errorf("PreferredLabel: got %v, want %q", got.PreferredLabel, label)
		}
		if len(got.SuggestedNextIDs) != 2 || got.SuggestedNextIDs[0] != "node-x" || got.SuggestedNextIDs[1] != "node-y" {
			t.Errorf("SuggestedNextIDs: got %v, want [node-x node-y]", got.SuggestedNextIDs)
		}
	})

	t.Run("outcome_kind_omitted_when_empty", func(t *testing.T) {
		t.Parallel()
		msg := handlercontract.OutcomeEmittedMsg{
			Type:          "outcome_emitted",
			RunID:         "r",
			SessionID:     "s",
			NodeID:        "n",
			OutcomeStatus: "SUCCESS",
			// OutcomeKind intentionally left empty
		}
		b, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var raw map[string]any
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Fatalf("unmarshal raw: %v", err)
		}
		if _, ok := raw["outcome_kind"]; ok {
			t.Error("outcome_kind should be omitted from JSON when empty (omitempty)")
		}
	})

	t.Run("preferred_label_omitted_when_nil", func(t *testing.T) {
		t.Parallel()
		msg := handlercontract.OutcomeEmittedMsg{
			Type:          "outcome_emitted",
			RunID:         "r",
			SessionID:     "s",
			NodeID:        "n",
			OutcomeStatus: "SUCCESS",
		}
		b, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var raw map[string]any
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Fatalf("unmarshal raw: %v", err)
		}
		if _, ok := raw["preferred_label"]; ok {
			t.Error("preferred_label should be omitted from JSON when nil (omitempty)")
		}
	})

	t.Run("suggested_next_ids_omitted_when_nil", func(t *testing.T) {
		t.Parallel()
		msg := handlercontract.OutcomeEmittedMsg{
			Type:          "outcome_emitted",
			RunID:         "r",
			SessionID:     "s",
			NodeID:        "n",
			OutcomeStatus: "SUCCESS",
		}
		b, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var raw map[string]any
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Fatalf("unmarshal raw: %v", err)
		}
		if _, ok := raw["suggested_next_ids"]; ok {
			t.Error("suggested_next_ids should be omitted from JSON when nil (omitempty)")
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// JSON field names match event-model.md §8.1.8 wire names
// ─────────────────────────────────────────────────────────────────────────────

func TestOutcomeEmittedMsgWireFieldNames(t *testing.T) {
	t.Parallel()

	label := "hint"
	msg := handlercontract.OutcomeEmittedMsg{
		Type:             "outcome_emitted",
		RunID:            "r",
		SessionID:        "s",
		NodeID:           "n",
		OutcomeStatus:    "RETRY",
		OutcomeKind:      "default",
		PreferredLabel:   outcomeDeliveryPtr(label),
		SuggestedNextIDs: []string{"nx"},
	}

	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	wantKeys := []string{
		"type", "run_id", "session_id", "node_id",
		"outcome_status", "outcome_kind", "preferred_label", "suggested_next_ids",
	}
	for _, k := range wantKeys {
		if _, ok := raw[k]; !ok {
			t.Errorf("expected JSON key %q to be present in marshalled OutcomeEmittedMsg", k)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ClassifyCrash — HC-024 typed-class mapping (hk-8i31.28)
// ─────────────────────────────────────────────────────────────────────────────

// TestCrashClassFixture_CrashWithoutOutcomeSubReasonValue asserts that the
// pinned constant equals the literal string expected by the spec and by the
// wire emitter test suite in crashRecov_hc024_test.go (hk-8i31.80).
//
// Spec: §4.6.HC-024 — "agent_failed bus event whose payload includes the
// mapped sentinel class per §4.5"; §8.2 sub_reason is implementation-defined
// for the crash-without-outcome case; the literal is pinned here.
//
// Cite: specs/handler-contract.md §4.6.HC-024, §8.2.
func TestCrashClassFixture_CrashWithoutOutcomeSubReasonValue(t *testing.T) {
	t.Parallel()

	const want = "crash_without_outcome"
	if handlercontract.CrashWithoutOutcomeSubReason != want {
		t.Errorf("CrashWithoutOutcomeSubReason = %q, want %q", handlercontract.CrashWithoutOutcomeSubReason, want)
	}
}

// TestCrashClassFixture_ClassifyCrashPayloadTable verifies ClassifyCrash returns
// the correct CrashAgentFailedPayload for every exit scenario.
//
// HC-024 says: "when a handler subprocess crashes (exits non-zero without a
// preceding outcome_emitted message), the watcher MUST emit an agent_failed bus
// event whose payload includes the mapped sentinel class per §4.5."  This test
// asserts the mapping from (exitCode, OutcomeDeliveryState) →
// (ErrorCategory, Reason, SubReason) is deterministic and spec-correct.
//
// Cite: specs/handler-contract.md §4.6.HC-024, §4.5.HC-020, §8.2.
func TestCrashClassFixture_ClassifyCrashPayloadTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		exitCode      int
		state         handlercontract.OutcomeDeliveryState
		wantNonZero   bool   // true iff a non-zero payload (agent_failed needed)
		wantCategory  string // expected ErrorCategory when wantNonZero
		wantReason    string // expected Reason when wantNonZero
		wantSubReason string // expected SubReason when wantNonZero
	}{
		// Clean shutdown after outcome — no agent_failed needed.
		{
			name:        "clean_exit_after_outcome_delivered_is_zero",
			exitCode:    0,
			state:       handlercontract.OutcomeDelivered,
			wantNonZero: false,
		},
		// Dirty exit inside shutdown window (HC-008a) — no agent_failed.
		{
			name:        "dirty_exit_after_outcome_delivered_is_zero",
			exitCode:    1,
			state:       handlercontract.OutcomeDelivered,
			wantNonZero: false,
		},
		{
			name:        "sigkill_exit_after_outcome_delivered_is_zero",
			exitCode:    137,
			state:       handlercontract.OutcomeDelivered,
			wantNonZero: false,
		},
		// Clean exit WITHOUT outcome (handler bug) — agent_failed needed.
		{
			name:          "clean_exit_without_outcome_is_structural",
			exitCode:      0,
			state:         handlercontract.OutcomeNotYetDelivered,
			wantNonZero:   true,
			wantCategory:  "structural",
			wantReason:    handlercontract.CrashWithoutOutcomeSubReason,
			wantSubReason: handlercontract.CrashWithoutOutcomeSubReason,
		},
		// Non-zero exit WITHOUT outcome (crash) — agent_failed needed.
		{
			name:          "nonzero_exit_without_outcome_is_structural",
			exitCode:      1,
			state:         handlercontract.OutcomeNotYetDelivered,
			wantNonZero:   true,
			wantCategory:  "structural",
			wantReason:    handlercontract.CrashWithoutOutcomeSubReason,
			wantSubReason: handlercontract.CrashWithoutOutcomeSubReason,
		},
		// SIGKILL exit WITHOUT outcome — agent_failed needed.
		{
			name:          "sigkill_exit_without_outcome_is_structural",
			exitCode:      137,
			state:         handlercontract.OutcomeNotYetDelivered,
			wantNonZero:   true,
			wantCategory:  "structural",
			wantReason:    handlercontract.CrashWithoutOutcomeSubReason,
			wantSubReason: handlercontract.CrashWithoutOutcomeSubReason,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := handlercontract.ClassifyCrash(tc.exitCode, tc.state)

			if !tc.wantNonZero {
				// Expect zero payload — no agent_failed emission.
				if got.ErrorCategory != "" {
					t.Errorf("ClassifyCrash(%d, %v).ErrorCategory = %q, want empty (no agent_failed needed)",
						tc.exitCode, tc.state, got.ErrorCategory)
				}
				if got.Reason != "" {
					t.Errorf("ClassifyCrash(%d, %v).Reason = %q, want empty", tc.exitCode, tc.state, got.Reason)
				}
				if got.SubReason != "" {
					t.Errorf("ClassifyCrash(%d, %v).SubReason = %q, want empty", tc.exitCode, tc.state, got.SubReason)
				}
				return
			}

			// Expect non-zero payload — agent_failed must be emitted.
			if got.ErrorCategory != tc.wantCategory {
				t.Errorf("ClassifyCrash(%d, %v).ErrorCategory = %q, want %q",
					tc.exitCode, tc.state, got.ErrorCategory, tc.wantCategory)
			}
			if got.Reason != tc.wantReason {
				t.Errorf("ClassifyCrash(%d, %v).Reason = %q, want %q",
					tc.exitCode, tc.state, got.Reason, tc.wantReason)
			}
			if got.SubReason != tc.wantSubReason {
				t.Errorf("ClassifyCrash(%d, %v).SubReason = %q, want %q",
					tc.exitCode, tc.state, got.SubReason, tc.wantSubReason)
			}
		})
	}
}

// TestCrashClassFixture_ClassifyCrashCategoryMatchesClassOfClassifyExit verifies
// that ClassifyCrash's ErrorCategory field exactly matches Class(ClassifyExit(...))
// for every crash scenario.
//
// This asserts the invariant from HC-024 + §4.5: the "mapped sentinel class"
// in the agent_failed payload MUST be derived from the sentinel returned by
// ClassifyExit via the Class() function.  ClassifyCrash is the combined
// one-call API; this test proves it is internally consistent.
//
// Cite: specs/handler-contract.md §4.5.HC-020, §4.6.HC-024.
func TestCrashClassFixture_ClassifyCrashCategoryMatchesClassOfClassifyExit(t *testing.T) {
	t.Parallel()

	scenarios := []struct {
		exitCode int
		state    handlercontract.OutcomeDeliveryState
	}{
		{0, handlercontract.OutcomeDelivered},
		{1, handlercontract.OutcomeDelivered},
		{137, handlercontract.OutcomeDelivered},
		{0, handlercontract.OutcomeNotYetDelivered},
		{1, handlercontract.OutcomeNotYetDelivered},
		{137, handlercontract.OutcomeNotYetDelivered},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run("", func(t *testing.T) {
			t.Parallel()
			classifyErr := handlercontract.ClassifyExit(sc.exitCode, sc.state)
			crashPayload := handlercontract.ClassifyCrash(sc.exitCode, sc.state)

			wantCategory := handlercontract.Class(classifyErr)
			// When ClassifyExit returns nil, Class returns ""; ClassifyCrash
			// returns zero payload with ErrorCategory == "".  Both agree.
			if crashPayload.ErrorCategory != wantCategory {
				t.Errorf(
					"ClassifyCrash(%d, %v).ErrorCategory = %q; Class(ClassifyExit(%d, %v)) = %q; they must agree (HC-024 + §4.5)",
					sc.exitCode, sc.state, crashPayload.ErrorCategory,
					sc.exitCode, sc.state, wantCategory,
				)
			}
		})
	}
}

// TestCrashClassFixture_ClassifyCrashWrapsErrStructural verifies that when
// ClassifyCrash produces a non-zero payload, ClassifyExit for the same inputs
// returns an error that wraps ErrStructural per HC-020 narrowest-first dispatch.
//
// This is the sentinel-identity test for HC-024: the mapped class is not just a
// string "structural" — it MUST originate from an error that wraps ErrStructural
// so that consumers using errors.Is dispatch correctly.
//
// Cite: specs/handler-contract.md §4.5.HC-020, §4.6.HC-024.
func TestCrashClassFixture_ClassifyCrashWrapsErrStructural(t *testing.T) {
	t.Parallel()

	crashScenarios := []struct {
		name     string
		exitCode int
		state    handlercontract.OutcomeDeliveryState
	}{
		{"crash_nonzero", 1, handlercontract.OutcomeNotYetDelivered},
		{"crash_sigkill", 137, handlercontract.OutcomeNotYetDelivered},
		{"clean_exit_handler_bug", 0, handlercontract.OutcomeNotYetDelivered},
	}

	for _, sc := range crashScenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			err := handlercontract.ClassifyExit(sc.exitCode, sc.state)
			if err == nil {
				t.Fatalf("ClassifyExit(%d, %v) = nil, want non-nil for crash scenario", sc.exitCode, sc.state)
			}
			if !errors.Is(err, handlercontract.ErrStructural) {
				t.Errorf("ClassifyExit(%d, %v): errors.Is(err, ErrStructural) = false, want true; "+
					"crash without outcome MUST map to ErrStructural per HC-024 + §8.2",
					sc.exitCode, sc.state)
			}
			// Confirm ClassifyCrash agrees on the class string.
			payload := handlercontract.ClassifyCrash(sc.exitCode, sc.state)
			if payload.ErrorCategory != "structural" {
				t.Errorf("ClassifyCrash(%d, %v).ErrorCategory = %q, want structural", sc.exitCode, sc.state, payload.ErrorCategory)
			}
		})
	}
}
