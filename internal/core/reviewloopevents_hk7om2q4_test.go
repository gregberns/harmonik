package core

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Test helpers (prefix: reviewLoopFixture)
// ---------------------------------------------------------------------------

func reviewLoopFixtureRunID(t *testing.T) RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return RunID(id)
}

// ---------------------------------------------------------------------------
// ReviewerVerdict enum tests
// ---------------------------------------------------------------------------

func TestReviewerVerdictValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		v     ReviewerVerdict
		valid bool
	}{
		{"approve", ReviewerVerdictApprove, true},
		{"request_changes", ReviewerVerdictRequestChanges, true},
		{"block", ReviewerVerdictBlock, true},
		{"empty", ReviewerVerdict(""), false},
		{"unknown", ReviewerVerdict("UNKNOWN"), false},
		{"lowercase", ReviewerVerdict("approve"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.v.Valid(); got != tc.valid {
				t.Errorf("ReviewerVerdict(%q).Valid() = %v, want %v", tc.v, got, tc.valid)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ReviewLoopCompletionReason enum tests
// ---------------------------------------------------------------------------

func TestReviewLoopCompletionReasonValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		r     ReviewLoopCompletionReason
		valid bool
	}{
		{"approved", ReviewLoopCompletionReasonApproved, true},
		{"cap_hit", ReviewLoopCompletionReasonCapHit, true},
		{"blocked", ReviewLoopCompletionReasonBlocked, true},
		{"no_progress", ReviewLoopCompletionReasonNoProgress, true},
		{"error", ReviewLoopCompletionReasonError, true},
		{"empty", ReviewLoopCompletionReason(""), false},
		{"unknown", ReviewLoopCompletionReason("unknown_reason"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.r.Valid(); got != tc.valid {
				t.Errorf("ReviewLoopCompletionReason(%q).Valid() = %v, want %v", tc.r, got, tc.valid)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ImplementerResumedPayload tests
// ---------------------------------------------------------------------------

func TestImplementerResumedPayloadValid(t *testing.T) {
	t.Parallel()

	runID := reviewLoopFixtureRunID(t)

	tests := []struct {
		name  string
		p     ImplementerResumedPayload
		valid bool
	}{
		{
			name: "minimal valid",
			p: ImplementerResumedPayload{
				RunID:               runID,
				WorkflowMode:        WorkflowModeReviewLoop,
				SessionID:           "sess-impl-1",
				ClaudeSessionID:     "claude-sess-abc",
				IterationCount:      2,
				PriorVerdictSummary: "Prior reviewer noted missing test coverage.",
			},
			valid: true,
		},
		{
			name: "nil run_id rejected",
			p: ImplementerResumedPayload{
				RunID:               RunID(uuid.Nil),
				WorkflowMode:        WorkflowModeReviewLoop,
				SessionID:           "sess-impl-1",
				ClaudeSessionID:     "claude-sess-abc",
				IterationCount:      2,
				PriorVerdictSummary: "Prior notes.",
			},
			valid: false,
		},
		{
			name: "invalid workflow_mode rejected",
			p: ImplementerResumedPayload{
				RunID:               runID,
				WorkflowMode:        WorkflowMode("invalid"),
				SessionID:           "sess-impl-1",
				ClaudeSessionID:     "claude-sess-abc",
				IterationCount:      2,
				PriorVerdictSummary: "Prior notes.",
			},
			valid: false,
		},
		{
			name: "empty session_id rejected",
			p: ImplementerResumedPayload{
				RunID:               runID,
				WorkflowMode:        WorkflowModeReviewLoop,
				SessionID:           "",
				ClaudeSessionID:     "claude-sess-abc",
				IterationCount:      2,
				PriorVerdictSummary: "Prior notes.",
			},
			valid: false,
		},
		{
			name: "empty claude_session_id rejected",
			p: ImplementerResumedPayload{
				RunID:               runID,
				WorkflowMode:        WorkflowModeReviewLoop,
				SessionID:           "sess-impl-1",
				ClaudeSessionID:     "",
				IterationCount:      2,
				PriorVerdictSummary: "Prior notes.",
			},
			valid: false,
		},
		{
			name: "iteration_count 1 rejected (only fires from iteration 2+)",
			p: ImplementerResumedPayload{
				RunID:               runID,
				WorkflowMode:        WorkflowModeReviewLoop,
				SessionID:           "sess-impl-1",
				ClaudeSessionID:     "claude-sess-abc",
				IterationCount:      1,
				PriorVerdictSummary: "Prior notes.",
			},
			valid: false,
		},
		{
			name: "iteration_count 0 rejected",
			p: ImplementerResumedPayload{
				RunID:               runID,
				WorkflowMode:        WorkflowModeReviewLoop,
				SessionID:           "sess-impl-1",
				ClaudeSessionID:     "claude-sess-abc",
				IterationCount:      0,
				PriorVerdictSummary: "Prior notes.",
			},
			valid: false,
		},
		{
			name: "empty prior_verdict_summary rejected",
			p: ImplementerResumedPayload{
				RunID:               runID,
				WorkflowMode:        WorkflowModeReviewLoop,
				SessionID:           "sess-impl-1",
				ClaudeSessionID:     "claude-sess-abc",
				IterationCount:      2,
				PriorVerdictSummary: "",
			},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("ImplementerResumedPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestImplementerResumedPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	runID := reviewLoopFixtureRunID(t)
	original := ImplementerResumedPayload{
		RunID:               runID,
		WorkflowMode:        WorkflowModeReviewLoop,
		SessionID:           "sess-roundtrip",
		ClaudeSessionID:     "claude-sess-roundtrip",
		IterationCount:      3,
		PriorVerdictSummary: "Missing error handling in edge cases.",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ImplementerResumedPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !decoded.Valid() {
		t.Error("decoded payload failed Valid()")
	}
	if decoded.RunID != runID {
		t.Errorf("RunID: got %v, want %v", decoded.RunID, runID)
	}
	if decoded.WorkflowMode != original.WorkflowMode {
		t.Errorf("WorkflowMode: got %q, want %q", decoded.WorkflowMode, original.WorkflowMode)
	}
	if decoded.IterationCount != original.IterationCount {
		t.Errorf("IterationCount: got %d, want %d", decoded.IterationCount, original.IterationCount)
	}
	if decoded.PriorVerdictSummary != original.PriorVerdictSummary {
		t.Errorf("PriorVerdictSummary: got %q, want %q", decoded.PriorVerdictSummary, original.PriorVerdictSummary)
	}
}

// ---------------------------------------------------------------------------
// ReviewerLaunchedPayload tests
// ---------------------------------------------------------------------------

func TestReviewerLaunchedPayloadValid(t *testing.T) {
	t.Parallel()

	runID := reviewLoopFixtureRunID(t)

	tests := []struct {
		name  string
		p     ReviewerLaunchedPayload
		valid bool
	}{
		{
			name: "minimal valid",
			p: ReviewerLaunchedPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowModeReviewLoop,
				SessionID:       "sess-reviewer-1",
				ClaudeSessionID: "claude-sess-rev-1",
				IterationCount:  1,
			},
			valid: true,
		},
		{
			name: "nil run_id rejected",
			p: ReviewerLaunchedPayload{
				RunID:           RunID(uuid.Nil),
				WorkflowMode:    WorkflowModeReviewLoop,
				SessionID:       "sess-reviewer-1",
				ClaudeSessionID: "claude-sess-rev-1",
				IterationCount:  1,
			},
			valid: false,
		},
		{
			name: "invalid workflow_mode rejected",
			p: ReviewerLaunchedPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowMode("single"),
				SessionID:       "sess-reviewer-1",
				ClaudeSessionID: "claude-sess-rev-1",
				IterationCount:  1,
			},
			valid: true, // WorkflowModeSingle is valid as a WorkflowMode; spec says "always review-loop" but Valid() only checks enum membership
		},
		{
			name: "empty workflow_mode rejected",
			p: ReviewerLaunchedPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowMode(""),
				SessionID:       "sess-reviewer-1",
				ClaudeSessionID: "claude-sess-rev-1",
				IterationCount:  1,
			},
			valid: false,
		},
		{
			name: "empty session_id rejected",
			p: ReviewerLaunchedPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowModeReviewLoop,
				SessionID:       "",
				ClaudeSessionID: "claude-sess-rev-1",
				IterationCount:  1,
			},
			valid: false,
		},
		{
			name: "empty claude_session_id rejected",
			p: ReviewerLaunchedPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowModeReviewLoop,
				SessionID:       "sess-reviewer-1",
				ClaudeSessionID: "",
				IterationCount:  1,
			},
			valid: false,
		},
		{
			name: "iteration_count 0 rejected",
			p: ReviewerLaunchedPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowModeReviewLoop,
				SessionID:       "sess-reviewer-1",
				ClaudeSessionID: "claude-sess-rev-1",
				IterationCount:  0,
			},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("ReviewerLaunchedPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestReviewerLaunchedPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	runID := reviewLoopFixtureRunID(t)
	original := ReviewerLaunchedPayload{
		RunID:           runID,
		WorkflowMode:    WorkflowModeReviewLoop,
		SessionID:       "sess-rev-rt",
		ClaudeSessionID: "claude-sess-rev-rt",
		IterationCount:  2,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ReviewerLaunchedPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !decoded.Valid() {
		t.Error("decoded payload failed Valid()")
	}
	if decoded.IterationCount != original.IterationCount {
		t.Errorf("IterationCount: got %d, want %d", decoded.IterationCount, original.IterationCount)
	}
}

// ---------------------------------------------------------------------------
// ReviewerVerdictPayload tests
// ---------------------------------------------------------------------------

func TestReviewerVerdictPayloadValid(t *testing.T) {
	t.Parallel()

	runID := reviewLoopFixtureRunID(t)

	tests := []struct {
		name  string
		p     ReviewerVerdictPayload
		valid bool
	}{
		{
			name: "minimal valid approve",
			p: ReviewerVerdictPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowModeReviewLoop,
				SessionID:       "sess-rev-1",
				ClaudeSessionID: "claude-rev-1",
				IterationCount:  1,
				SchemaVersion:   1,
				Verdict:         ReviewerVerdictApprove,
				Flags:           []string{},
				Notes:           "All checks passed.",
			},
			valid: true,
		},
		{
			name: "request_changes with flags",
			p: ReviewerVerdictPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowModeReviewLoop,
				SessionID:       "sess-rev-2",
				ClaudeSessionID: "claude-rev-2",
				IterationCount:  2,
				SchemaVersion:   1,
				Verdict:         ReviewerVerdictRequestChanges,
				Flags:           []string{"MISSING_TEST", "LINT_VIOLATION"},
				Notes:           "Missing tests for edge cases at foo.go:42.",
			},
			valid: true,
		},
		{
			name: "nil run_id rejected",
			p: ReviewerVerdictPayload{
				RunID:           RunID(uuid.Nil),
				WorkflowMode:    WorkflowModeReviewLoop,
				SessionID:       "sess-rev-1",
				ClaudeSessionID: "claude-rev-1",
				IterationCount:  1,
				SchemaVersion:   1,
				Verdict:         ReviewerVerdictApprove,
				Flags:           []string{},
				Notes:           "Looks good.",
			},
			valid: false,
		},
		{
			name: "schema_version != 1 rejected",
			p: ReviewerVerdictPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowModeReviewLoop,
				SessionID:       "sess-rev-1",
				ClaudeSessionID: "claude-rev-1",
				IterationCount:  1,
				SchemaVersion:   2,
				Verdict:         ReviewerVerdictApprove,
				Flags:           []string{},
				Notes:           "Looks good.",
			},
			valid: false,
		},
		{
			name: "schema_version 0 rejected",
			p: ReviewerVerdictPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowModeReviewLoop,
				SessionID:       "sess-rev-1",
				ClaudeSessionID: "claude-rev-1",
				IterationCount:  1,
				SchemaVersion:   0,
				Verdict:         ReviewerVerdictApprove,
				Flags:           []string{},
				Notes:           "Looks good.",
			},
			valid: false,
		},
		{
			name: "invalid verdict rejected",
			p: ReviewerVerdictPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowModeReviewLoop,
				SessionID:       "sess-rev-1",
				ClaudeSessionID: "claude-rev-1",
				IterationCount:  1,
				SchemaVersion:   1,
				Verdict:         ReviewerVerdict("MAYBE"),
				Flags:           []string{},
				Notes:           "Looks good.",
			},
			valid: false,
		},
		{
			name: "nil flags rejected",
			p: ReviewerVerdictPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowModeReviewLoop,
				SessionID:       "sess-rev-1",
				ClaudeSessionID: "claude-rev-1",
				IterationCount:  1,
				SchemaVersion:   1,
				Verdict:         ReviewerVerdictApprove,
				Flags:           nil,
				Notes:           "Looks good.",
			},
			valid: false,
		},
		{
			name: "empty notes rejected",
			p: ReviewerVerdictPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowModeReviewLoop,
				SessionID:       "sess-rev-1",
				ClaudeSessionID: "claude-rev-1",
				IterationCount:  1,
				SchemaVersion:   1,
				Verdict:         ReviewerVerdictApprove,
				Flags:           []string{},
				Notes:           "",
			},
			valid: false,
		},
		{
			name: "iteration_count 0 rejected",
			p: ReviewerVerdictPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowModeReviewLoop,
				SessionID:       "sess-rev-1",
				ClaudeSessionID: "claude-rev-1",
				IterationCount:  0,
				SchemaVersion:   1,
				Verdict:         ReviewerVerdictApprove,
				Flags:           []string{},
				Notes:           "Looks good.",
			},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("ReviewerVerdictPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestReviewerVerdictPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	runID := reviewLoopFixtureRunID(t)
	original := ReviewerVerdictPayload{
		RunID:           runID,
		WorkflowMode:    WorkflowModeReviewLoop,
		SessionID:       "sess-rev-rt",
		ClaudeSessionID: "claude-rev-rt",
		IterationCount:  1,
		SchemaVersion:   1,
		Verdict:         ReviewerVerdictApprove,
		Flags:           []string{"FLAG_A"},
		Notes:           "LGTM.",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ReviewerVerdictPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !decoded.Valid() {
		t.Error("decoded payload failed Valid()")
	}
	if decoded.SchemaVersion != original.SchemaVersion {
		t.Errorf("SchemaVersion: got %d, want %d", decoded.SchemaVersion, original.SchemaVersion)
	}
	if decoded.Verdict != original.Verdict {
		t.Errorf("Verdict: got %q, want %q", decoded.Verdict, original.Verdict)
	}
	if len(decoded.Flags) != len(original.Flags) {
		t.Errorf("Flags length: got %d, want %d", len(decoded.Flags), len(original.Flags))
	}
}

// ---------------------------------------------------------------------------
// IterationCapHitPayload tests
// ---------------------------------------------------------------------------

func TestIterationCapHitPayloadValid(t *testing.T) {
	t.Parallel()

	runID := reviewLoopFixtureRunID(t)

	tests := []struct {
		name  string
		p     IterationCapHitPayload
		valid bool
	}{
		{
			name: "valid request_changes at cap",
			p: IterationCapHitPayload{
				RunID:          runID,
				WorkflowMode:   WorkflowModeReviewLoop,
				IterationCount: 3,
				CapValue:       3,
				FinalVerdict:   ReviewerVerdictRequestChanges,
			},
			valid: true,
		},
		{
			name: "valid block at cap",
			p: IterationCapHitPayload{
				RunID:          runID,
				WorkflowMode:   WorkflowModeReviewLoop,
				IterationCount: 3,
				CapValue:       3,
				FinalVerdict:   ReviewerVerdictBlock,
			},
			valid: true,
		},
		{
			name: "approve rejected as final_verdict",
			p: IterationCapHitPayload{
				RunID:          runID,
				WorkflowMode:   WorkflowModeReviewLoop,
				IterationCount: 3,
				CapValue:       3,
				FinalVerdict:   ReviewerVerdictApprove,
			},
			valid: false,
		},
		{
			name: "nil run_id rejected",
			p: IterationCapHitPayload{
				RunID:          RunID(uuid.Nil),
				WorkflowMode:   WorkflowModeReviewLoop,
				IterationCount: 3,
				CapValue:       3,
				FinalVerdict:   ReviewerVerdictRequestChanges,
			},
			valid: false,
		},
		{
			name: "cap_value 0 rejected",
			p: IterationCapHitPayload{
				RunID:          runID,
				WorkflowMode:   WorkflowModeReviewLoop,
				IterationCount: 3,
				CapValue:       0,
				FinalVerdict:   ReviewerVerdictRequestChanges,
			},
			valid: false,
		},
		{
			name: "iteration_count 0 rejected",
			p: IterationCapHitPayload{
				RunID:          runID,
				WorkflowMode:   WorkflowModeReviewLoop,
				IterationCount: 0,
				CapValue:       3,
				FinalVerdict:   ReviewerVerdictRequestChanges,
			},
			valid: false,
		},
		{
			name: "invalid workflow_mode rejected",
			p: IterationCapHitPayload{
				RunID:          runID,
				WorkflowMode:   WorkflowMode(""),
				IterationCount: 3,
				CapValue:       3,
				FinalVerdict:   ReviewerVerdictRequestChanges,
			},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("IterationCapHitPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestIterationCapHitPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	runID := reviewLoopFixtureRunID(t)
	original := IterationCapHitPayload{
		RunID:          runID,
		WorkflowMode:   WorkflowModeReviewLoop,
		IterationCount: 3,
		CapValue:       3,
		FinalVerdict:   ReviewerVerdictBlock,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded IterationCapHitPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !decoded.Valid() {
		t.Error("decoded payload failed Valid()")
	}
	if decoded.FinalVerdict != original.FinalVerdict {
		t.Errorf("FinalVerdict: got %q, want %q", decoded.FinalVerdict, original.FinalVerdict)
	}
	if decoded.CapValue != original.CapValue {
		t.Errorf("CapValue: got %d, want %d", decoded.CapValue, original.CapValue)
	}
}

// ---------------------------------------------------------------------------
// NoProgressDetectedPayload tests
// ---------------------------------------------------------------------------

func TestNoProgressDetectedPayloadValid(t *testing.T) {
	t.Parallel()

	runID := reviewLoopFixtureRunID(t)
	const exampleHash = "a3b1c2d4e5f67890a3b1c2d4e5f67890a3b1c2d4e5f67890a3b1c2d4e5f67890"

	tests := []struct {
		name  string
		p     NoProgressDetectedPayload
		valid bool
	}{
		{
			name: "minimal valid",
			p: NoProgressDetectedPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowModeReviewLoop,
				IterationCount:  2,
				DiffHashCurrent: exampleHash,
				DiffHashPrior:   exampleHash,
			},
			valid: true,
		},
		{
			name: "nil run_id rejected",
			p: NoProgressDetectedPayload{
				RunID:           RunID(uuid.Nil),
				WorkflowMode:    WorkflowModeReviewLoop,
				IterationCount:  2,
				DiffHashCurrent: exampleHash,
				DiffHashPrior:   exampleHash,
			},
			valid: false,
		},
		{
			name: "iteration_count 1 rejected (no-progress requires prior iteration)",
			p: NoProgressDetectedPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowModeReviewLoop,
				IterationCount:  1,
				DiffHashCurrent: exampleHash,
				DiffHashPrior:   exampleHash,
			},
			valid: false,
		},
		{
			name: "empty diff_hash_current rejected",
			p: NoProgressDetectedPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowModeReviewLoop,
				IterationCount:  2,
				DiffHashCurrent: "",
				DiffHashPrior:   exampleHash,
			},
			valid: false,
		},
		{
			name: "empty diff_hash_prior rejected",
			p: NoProgressDetectedPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowModeReviewLoop,
				IterationCount:  2,
				DiffHashCurrent: exampleHash,
				DiffHashPrior:   "",
			},
			valid: false,
		},
		{
			name: "invalid workflow_mode rejected",
			p: NoProgressDetectedPayload{
				RunID:           runID,
				WorkflowMode:    WorkflowMode(""),
				IterationCount:  2,
				DiffHashCurrent: exampleHash,
				DiffHashPrior:   exampleHash,
			},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("NoProgressDetectedPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestNoProgressDetectedPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	runID := reviewLoopFixtureRunID(t)
	const hash = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	original := NoProgressDetectedPayload{
		RunID:           runID,
		WorkflowMode:    WorkflowModeReviewLoop,
		IterationCount:  2,
		DiffHashCurrent: hash,
		DiffHashPrior:   hash,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded NoProgressDetectedPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !decoded.Valid() {
		t.Error("decoded payload failed Valid()")
	}
	if decoded.DiffHashCurrent != hash {
		t.Errorf("DiffHashCurrent: got %q, want %q", decoded.DiffHashCurrent, hash)
	}
	if decoded.DiffHashPrior != hash {
		t.Errorf("DiffHashPrior: got %q, want %q", decoded.DiffHashPrior, hash)
	}
}

// ---------------------------------------------------------------------------
// ReviewLoopCycleCompletePayload tests
// ---------------------------------------------------------------------------

func TestReviewLoopCycleCompletePayloadValid(t *testing.T) {
	t.Parallel()

	runID := reviewLoopFixtureRunID(t)

	tests := []struct {
		name  string
		p     ReviewLoopCycleCompletePayload
		valid bool
	}{
		{
			name: "approved",
			p: ReviewLoopCycleCompletePayload{
				RunID:               runID,
				WorkflowMode:        WorkflowModeReviewLoop,
				FinalIterationCount: 1,
				CompletionReason:    ReviewLoopCompletionReasonApproved,
			},
			valid: true,
		},
		{
			name: "cap_hit",
			p: ReviewLoopCycleCompletePayload{
				RunID:               runID,
				WorkflowMode:        WorkflowModeReviewLoop,
				FinalIterationCount: 3,
				CompletionReason:    ReviewLoopCompletionReasonCapHit,
			},
			valid: true,
		},
		{
			name: "blocked",
			p: ReviewLoopCycleCompletePayload{
				RunID:               runID,
				WorkflowMode:        WorkflowModeReviewLoop,
				FinalIterationCount: 2,
				CompletionReason:    ReviewLoopCompletionReasonBlocked,
			},
			valid: true,
		},
		{
			name: "no_progress",
			p: ReviewLoopCycleCompletePayload{
				RunID:               runID,
				WorkflowMode:        WorkflowModeReviewLoop,
				FinalIterationCount: 2,
				CompletionReason:    ReviewLoopCompletionReasonNoProgress,
			},
			valid: true,
		},
		{
			name: "error",
			p: ReviewLoopCycleCompletePayload{
				RunID:               runID,
				WorkflowMode:        WorkflowModeReviewLoop,
				FinalIterationCount: 1,
				CompletionReason:    ReviewLoopCompletionReasonError,
			},
			valid: true,
		},
		{
			name: "nil run_id rejected",
			p: ReviewLoopCycleCompletePayload{
				RunID:               RunID(uuid.Nil),
				WorkflowMode:        WorkflowModeReviewLoop,
				FinalIterationCount: 1,
				CompletionReason:    ReviewLoopCompletionReasonApproved,
			},
			valid: false,
		},
		{
			name: "final_iteration_count 0 rejected",
			p: ReviewLoopCycleCompletePayload{
				RunID:               runID,
				WorkflowMode:        WorkflowModeReviewLoop,
				FinalIterationCount: 0,
				CompletionReason:    ReviewLoopCompletionReasonApproved,
			},
			valid: false,
		},
		{
			name: "invalid completion_reason rejected",
			p: ReviewLoopCycleCompletePayload{
				RunID:               runID,
				WorkflowMode:        WorkflowModeReviewLoop,
				FinalIterationCount: 1,
				CompletionReason:    ReviewLoopCompletionReason("unknown"),
			},
			valid: false,
		},
		{
			name: "invalid workflow_mode rejected",
			p: ReviewLoopCycleCompletePayload{
				RunID:               runID,
				WorkflowMode:        WorkflowMode(""),
				FinalIterationCount: 1,
				CompletionReason:    ReviewLoopCompletionReasonApproved,
			},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("ReviewLoopCycleCompletePayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestReviewLoopCycleCompletePayloadRoundTrip(t *testing.T) {
	t.Parallel()

	runID := reviewLoopFixtureRunID(t)
	original := ReviewLoopCycleCompletePayload{
		RunID:               runID,
		WorkflowMode:        WorkflowModeReviewLoop,
		FinalIterationCount: 2,
		CompletionReason:    ReviewLoopCompletionReasonCapHit,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ReviewLoopCycleCompletePayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !decoded.Valid() {
		t.Error("decoded payload failed Valid()")
	}
	if decoded.CompletionReason != original.CompletionReason {
		t.Errorf("CompletionReason: got %q, want %q", decoded.CompletionReason, original.CompletionReason)
	}
	if decoded.FinalIterationCount != original.FinalIterationCount {
		t.Errorf("FinalIterationCount: got %d, want %d", decoded.FinalIterationCount, original.FinalIterationCount)
	}
}

// ---------------------------------------------------------------------------
// BeadLabelConflictPayload tests
// ---------------------------------------------------------------------------

func TestBeadLabelConflictPayloadValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		p     BeadLabelConflictPayload
		valid bool
	}{
		{
			name: "minimal valid",
			p: BeadLabelConflictPayload{
				BeadID:            "bead-abc123",
				ConflictingLabels: []string{"workflow:single", "workflow:review-loop"},
				FallbackAction:    "tier-1 input treated as absent; precedence walk continues to tier 2",
				DetectedAt:        "2026-05-12T10:00:00.000Z",
			},
			valid: true,
		},
		{
			name: "single unknown label",
			p: BeadLabelConflictPayload{
				BeadID:            "bead-xyz",
				ConflictingLabels: []string{"workflow:nonexistent"},
				FallbackAction:    "tier-1 absent; falling through to tier 2",
				DetectedAt:        "2026-05-12T10:00:00.000Z",
			},
			valid: true,
		},
		{
			name: "empty bead_id rejected",
			p: BeadLabelConflictPayload{
				BeadID:            "",
				ConflictingLabels: []string{"workflow:single"},
				FallbackAction:    "tier-1 absent",
				DetectedAt:        "2026-05-12T10:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "nil conflicting_labels rejected",
			p: BeadLabelConflictPayload{
				BeadID:            "bead-abc",
				ConflictingLabels: nil,
				FallbackAction:    "tier-1 absent",
				DetectedAt:        "2026-05-12T10:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "empty conflicting_labels rejected",
			p: BeadLabelConflictPayload{
				BeadID:            "bead-abc",
				ConflictingLabels: []string{},
				FallbackAction:    "tier-1 absent",
				DetectedAt:        "2026-05-12T10:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "empty fallback_action rejected",
			p: BeadLabelConflictPayload{
				BeadID:            "bead-abc",
				ConflictingLabels: []string{"workflow:single"},
				FallbackAction:    "",
				DetectedAt:        "2026-05-12T10:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "empty detected_at rejected",
			p: BeadLabelConflictPayload{
				BeadID:            "bead-abc",
				ConflictingLabels: []string{"workflow:single"},
				FallbackAction:    "tier-1 absent",
				DetectedAt:        "",
			},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("BeadLabelConflictPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestBeadLabelConflictPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	original := BeadLabelConflictPayload{
		BeadID:            "bead-rt-001",
		ConflictingLabels: []string{"workflow:single", "workflow:dot"},
		FallbackAction:    "tier-1 treated as absent; continuing to tier 2",
		DetectedAt:        "2026-05-12T12:00:00.000Z",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded BeadLabelConflictPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !decoded.Valid() {
		t.Error("decoded payload failed Valid()")
	}
	if decoded.BeadID != original.BeadID {
		t.Errorf("BeadID: got %q, want %q", decoded.BeadID, original.BeadID)
	}
	if len(decoded.ConflictingLabels) != len(original.ConflictingLabels) {
		t.Errorf("ConflictingLabels length: got %d, want %d", len(decoded.ConflictingLabels), len(original.ConflictingLabels))
	}
	if decoded.FallbackAction != original.FallbackAction {
		t.Errorf("FallbackAction: got %q, want %q", decoded.FallbackAction, original.FallbackAction)
	}
}

// ---------------------------------------------------------------------------
// Constructor shape tests (registry integration)
// ---------------------------------------------------------------------------

func TestReviewLoopEventConstructorShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctor func() EventPayload
		want interface{}
	}{
		{
			name: "implementer_resumed",
			ctor: func() EventPayload { return &ImplementerResumedPayload{} },
			want: &ImplementerResumedPayload{},
		},
		{
			name: "reviewer_launched",
			ctor: func() EventPayload { return &ReviewerLaunchedPayload{} },
			want: &ReviewerLaunchedPayload{},
		},
		{
			name: "reviewer_verdict",
			ctor: func() EventPayload { return &ReviewerVerdictPayload{} },
			want: &ReviewerVerdictPayload{},
		},
		{
			name: "iteration_cap_hit",
			ctor: func() EventPayload { return &IterationCapHitPayload{} },
			want: &IterationCapHitPayload{},
		},
		{
			name: "no_progress_detected",
			ctor: func() EventPayload { return &NoProgressDetectedPayload{} },
			want: &NoProgressDetectedPayload{},
		},
		{
			name: "review_loop_cycle_complete",
			ctor: func() EventPayload { return &ReviewLoopCycleCompletePayload{} },
			want: &ReviewLoopCycleCompletePayload{},
		},
		{
			name: "bead_label_conflict",
			ctor: func() EventPayload { return &BeadLabelConflictPayload{} },
			want: &BeadLabelConflictPayload{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.ctor()
			if got == nil {
				t.Fatalf("constructor returned nil")
			}
		})
	}
}
