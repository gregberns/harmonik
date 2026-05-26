package daemon_test

// failure_class_hkex9c4_test.go — unit tests for BackfillFailureClass.
//
// Covers all acceptance criteria for hk-ex9c4 (T-IMPL-006):
//   - Handlers emitting FAIL without failure_class get a back-filled value.
//   - Handlers emitting FAIL with failure_class are honoured.
//   - Non-FAIL outcomes have failure_class cleared.
//   - Structured-log line emitted on back-fill.
//
// Spec refs: specs/handler-contract.md §4.2a HC-058, HC-059;
//            specs/execution-model.md §4.1 EM-005c, §8;
//            specs/workflow-graph.md §7 WG-018.
// Bead: hk-ex9c4.

import (
	"context"
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ptr returns a pointer to a FailureClass value; convenience for table tests.
func fcPtr(fc core.FailureClass) *core.FailureClass { return &fc }

func TestBackfillFailureClass(t *testing.T) {
	ctx := context.Background()
	runID := core.RunID{}
	nodeID := core.NodeID("test-node")

	tests := []struct {
		name           string
		status         core.OutcomeStatus
		handlerFC      *core.FailureClass
		sessionErr     error
		wantFC         *core.FailureClass
		wantNilOnNonFail bool
	}{
		// ── Non-FAIL: failure_class is always cleared ──────────────────────────

		{
			name:   "SUCCESS clears failure_class",
			status: core.OutcomeStatusSuccess,
			// Handler accidentally set a failure_class on a SUCCESS outcome.
			handlerFC: fcPtr(core.FailureClassTransient),
			wantFC:    nil,
		},
		{
			name:   "PARTIAL_SUCCESS clears failure_class",
			status: core.OutcomeStatusPartialSuccess,
			handlerFC: fcPtr(core.FailureClassStructural),
			wantFC:    nil,
		},
		{
			name:   "RETRY clears failure_class",
			status: core.OutcomeStatusRetry,
			handlerFC: fcPtr(core.FailureClassDeterministic),
			wantFC:    nil,
		},
		{
			name:   "SUCCESS with nil failure_class stays nil",
			status: core.OutcomeStatusSuccess,
			handlerFC: nil,
			wantFC:    nil,
		},

		// ── FAIL + handler omitted failure_class ───────────────────────────────

		{
			name:       "FAIL + no handler class + ErrTransient → back-fill transient",
			status:     core.OutcomeStatusFail,
			handlerFC:  nil,
			sessionErr: handlercontract.ErrTransient,
			wantFC:     fcPtr(core.FailureClassTransient),
		},
		{
			name:       "FAIL + no handler class + ErrStructural → back-fill structural",
			status:     core.OutcomeStatusFail,
			handlerFC:  nil,
			sessionErr: handlercontract.ErrStructural,
			wantFC:     fcPtr(core.FailureClassStructural),
		},
		{
			name:       "FAIL + no handler class + ErrDeterministic → back-fill deterministic",
			status:     core.OutcomeStatusFail,
			handlerFC:  nil,
			sessionErr: handlercontract.ErrDeterministic,
			wantFC:     fcPtr(core.FailureClassDeterministic),
		},
		{
			name:       "FAIL + no handler class + ErrCanceled → back-fill canceled",
			status:     core.OutcomeStatusFail,
			handlerFC:  nil,
			sessionErr: handlercontract.ErrCanceled,
			wantFC:     fcPtr(core.FailureClassCanceled),
		},
		{
			name:       "FAIL + no handler class + ErrBudget → back-fill budget_exhausted",
			status:     core.OutcomeStatusFail,
			handlerFC:  nil,
			sessionErr: handlercontract.ErrBudget,
			wantFC:     fcPtr(core.FailureClassBudgetExhausted),
		},
		{
			name:       "FAIL + no handler class + nil sessionErr → back-fill structural (default)",
			status:     core.OutcomeStatusFail,
			handlerFC:  nil,
			sessionErr: nil,
			wantFC:     fcPtr(core.FailureClassStructural),
		},
		{
			name:       "FAIL + no handler class + wrapped ErrTransient → back-fill transient",
			status:     core.OutcomeStatusFail,
			handlerFC:  nil,
			sessionErr: errors.Join(errors.New("outer"), handlercontract.ErrTransient),
			wantFC:     fcPtr(core.FailureClassTransient),
		},
		{
			name:       "FAIL + no handler class + ErrProtocolMismatch (wraps ErrStructural) → structural",
			status:     core.OutcomeStatusFail,
			handlerFC:  nil,
			sessionErr: handlercontract.ErrProtocolMismatch,
			wantFC:     fcPtr(core.FailureClassStructural),
		},

		// ── FAIL + handler emitted failure_class ──────────────────────────────

		{
			name:       "FAIL + handler class=compilation_loop → override to structural (daemon-only)",
			status:     core.OutcomeStatusFail,
			handlerFC:  fcPtr(core.FailureClassCompilationLoop),
			sessionErr: nil,
			wantFC:     fcPtr(core.FailureClassStructural),
		},
		{
			name:       "FAIL + handler class=compilation_loop + ErrStructural → override to structural",
			status:     core.OutcomeStatusFail,
			handlerFC:  fcPtr(core.FailureClassCompilationLoop),
			sessionErr: handlercontract.ErrStructural,
			wantFC:     fcPtr(core.FailureClassStructural),
		},
		{
			name:       "FAIL + handler class=transient + ErrStructural → daemon wins (structural)",
			status:     core.OutcomeStatusFail,
			handlerFC:  fcPtr(core.FailureClassTransient),
			sessionErr: handlercontract.ErrStructural,
			wantFC:     fcPtr(core.FailureClassStructural),
		},
		{
			name:       "FAIL + handler class=structural + ErrStructural → agree, honour structural",
			status:     core.OutcomeStatusFail,
			handlerFC:  fcPtr(core.FailureClassStructural),
			sessionErr: handlercontract.ErrStructural,
			wantFC:     fcPtr(core.FailureClassStructural),
		},
		{
			name:       "FAIL + handler class=transient + ErrTransient → agree, honour transient",
			status:     core.OutcomeStatusFail,
			handlerFC:  fcPtr(core.FailureClassTransient),
			sessionErr: handlercontract.ErrTransient,
			wantFC:     fcPtr(core.FailureClassTransient),
		},
		{
			name:       "FAIL + handler class=deterministic + ErrTransient → daemon wins (transient)",
			status:     core.OutcomeStatusFail,
			handlerFC:  fcPtr(core.FailureClassDeterministic),
			sessionErr: handlercontract.ErrTransient,
			wantFC:     fcPtr(core.FailureClassTransient),
		},
		{
			name:       "FAIL + handler class=budget_exhausted + nil sessionErr → honour handler (no daemon class)",
			status:     core.OutcomeStatusFail,
			handlerFC:  fcPtr(core.FailureClassBudgetExhausted),
			sessionErr: nil,
			wantFC:     fcPtr(core.FailureClassBudgetExhausted),
		},
		{
			name:       "FAIL + handler class=canceled + nil sessionErr → honour handler",
			status:     core.OutcomeStatusFail,
			handlerFC:  fcPtr(core.FailureClassCanceled),
			sessionErr: nil,
			wantFC:     fcPtr(core.FailureClassCanceled),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := core.Outcome{
				Status:       tt.status,
				FailureClass: tt.handlerFC,
			}
			got := daemon.BackfillFailureClass(ctx, o, tt.sessionErr, runID, nodeID)

			if tt.wantFC == nil {
				if got.FailureClass != nil {
					t.Errorf("FailureClass: want nil, got %q", string(*got.FailureClass))
				}
			} else {
				if got.FailureClass == nil {
					t.Errorf("FailureClass: want %q, got nil", string(*tt.wantFC))
				} else if *got.FailureClass != *tt.wantFC {
					t.Errorf("FailureClass: want %q, got %q", string(*tt.wantFC), string(*got.FailureClass))
				}
			}

			// Invariant: on FAIL the post-classifier Outcome must carry failure_class.
			if got.Status == core.OutcomeStatusFail && got.FailureClass == nil {
				t.Error("post-classifier invariant violated: FAIL outcome must carry failure_class")
			}

			// Invariant: non-FAIL outcomes must never carry failure_class.
			if got.Status != core.OutcomeStatusFail && got.FailureClass != nil {
				t.Errorf("non-FAIL outcome must not carry failure_class, got %q", string(*got.FailureClass))
			}
		})
	}
}

// TestBackfillFailureClass_StatusPreserved ensures BackfillFailureClass never
// mutates the Outcome.Status, Notes, or other fields.
func TestBackfillFailureClass_StatusPreserved(t *testing.T) {
	ctx := context.Background()
	runID := core.RunID{}
	nodeID := core.NodeID("preserve-test")

	note := "original notes"
	label := "myLabel"
	o := core.Outcome{
		Status:         core.OutcomeStatusFail,
		FailureClass:   nil,
		Notes:          note,
		PreferredLabel: &label,
	}
	got := daemon.BackfillFailureClass(ctx, o, handlercontract.ErrTransient, runID, nodeID)

	if got.Status != core.OutcomeStatusFail {
		t.Errorf("Status mutated: want FAIL, got %v", got.Status)
	}
	if got.Notes != note {
		t.Errorf("Notes mutated: want %q, got %q", note, got.Notes)
	}
	if got.PreferredLabel == nil || *got.PreferredLabel != label {
		t.Errorf("PreferredLabel mutated")
	}
}
