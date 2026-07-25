package reviewcycle

import (
	"reflect"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

func TestDecisionTable(t *testing.T) {
	t.Parallel()

	initial := mustState(t)
	awaitReviewer := mustAwaitReviewer(t, initial, "head-1", "diff-1")
	resume := mustResume(t, awaitReviewer, rawVerdict(
		core.ReviewerVerdictRequestChanges, []string{"correctness"}, "fix it",
	))
	atCap := resume
	atCap.Iteration = atCap.Cap
	atCap.Phase = PhaseAwaitReviewer
	atCap.PriorWorkProductHeadSHA = "head-cap"

	historical := initial
	historical.Phase = PhaseTerminal
	historical.Terminal = &TerminalResult{
		Success:          false,
		CompletionReason: core.ReviewLoopCompletionReasonNoProgress,
		NeedsAttention:   true,
		Summary:          "historical no-progress",
	}

	tests := []struct {
		name           string
		state          State
		observation    Observation
		wantPhase      Phase
		wantIteration  int
		wantIntents    []IntentKind
		wantReason     core.ReviewLoopCompletionReason
		wantSuccess    bool
		wantAttention  bool
		wantRawVerdict core.ReviewerVerdict
		wantNoTerminal bool
	}{
		{
			name:          "phase failure",
			state:         initial,
			observation:   ObserveFailure("launch", "launch failed"),
			wantPhase:     PhaseTerminal,
			wantIteration: 1,
			wantIntents:   []IntentKind{IntentPublishCycleComplete},
			wantReason:    core.ReviewLoopCompletionReasonError,
			wantAttention: true,
		},
		{
			name:          "cancellation is normalized error",
			state:         awaitReviewer,
			observation:   ObserveCancellation("operator cancelled"),
			wantPhase:     PhaseTerminal,
			wantIteration: 1,
			wantIntents:   []IntentKind{IntentPublishCycleComplete},
			wantReason:    core.ReviewLoopCompletionReasonError,
			wantAttention: true,
		},
		{
			name:          "initial no work product",
			state:         initial,
			observation:   ObserveImplementer("baseline", "diff-empty", false),
			wantPhase:     PhaseTerminal,
			wantIteration: 1,
			wantIntents:   []IntentKind{IntentPublishCycleComplete},
			wantReason:    core.ReviewLoopCompletionReasonError,
			wantAttention: true,
		},
		{
			name:          "checkpoint-only initial HEAD is not work product",
			state:         initial,
			observation:   ObserveImplementer("baseline", "diff-empty", true),
			wantPhase:     PhaseTerminal,
			wantIteration: 1,
			wantIntents:   []IntentKind{IntentPublishCycleComplete},
			wantReason:    core.ReviewLoopCompletionReasonError,
			wantAttention: true,
		},
		{
			name:          "resumed unchanged HEAD is fixup stalled",
			state:         resume,
			observation:   ObserveImplementer("head-1", "diff-1", true),
			wantPhase:     PhaseTerminal,
			wantIteration: 2,
			wantIntents: []IntentKind{
				IntentPublishFixupStalled,
				IntentPublishCycleComplete,
			},
			wantReason:    core.ReviewLoopCompletionReasonFixupStalled,
			wantAttention: true,
		},
		{
			name:          "advanced HEAD proceeds despite equal diff hash",
			state:         resume,
			observation:   ObserveImplementer("head-2", "diff-1", false),
			wantPhase:     PhaseAwaitReviewer,
			wantIteration: 2,
			wantIntents: []IntentKind{
				IntentPersistIterationFacts,
				IntentDispatchFreshReviewer,
			},
			wantNoTerminal: true,
		},
		{
			name:          "approve",
			state:         awaitReviewer,
			observation:   ObserveReviewer(rawVerdict(core.ReviewerVerdictApprove, nil, "looks good")),
			wantPhase:     PhaseTerminal,
			wantIteration: 1,
			wantIntents: []IntentKind{
				IntentPersistRawLastVerdict,
				IntentPublishRawVerdict,
				IntentPublishCycleComplete,
			},
			wantReason:     core.ReviewLoopCompletionReasonApproved,
			wantSuccess:    true,
			wantRawVerdict: core.ReviewerVerdictApprove,
		},
		{
			name:  "flagless request changes preserves raw and normalizes approval",
			state: awaitReviewer,
			observation: ObserveReviewer(rawVerdict(
				core.ReviewerVerdictRequestChanges, []string{}, "nothing actionable",
			)),
			wantPhase:     PhaseTerminal,
			wantIteration: 1,
			wantIntents: []IntentKind{
				IntentPersistRawLastVerdict,
				IntentPublishRawVerdict,
				IntentPublishCycleComplete,
			},
			wantReason:     core.ReviewLoopCompletionReasonApproved,
			wantSuccess:    true,
			wantRawVerdict: core.ReviewerVerdictRequestChanges,
		},
		{
			name:  "actionable request changes advances and resumes",
			state: awaitReviewer,
			observation: ObserveReviewer(rawVerdict(
				core.ReviewerVerdictRequestChanges, []string{"correctness"}, "fix it",
			)),
			wantPhase:     PhaseAwaitImplementer,
			wantIteration: 2,
			wantIntents: []IntentKind{
				IntentPersistRawLastVerdict,
				IntentPublishRawVerdict,
				IntentAdvanceIteration,
				IntentDispatchImplementer,
			},
			wantNoTerminal: true,
		},
		{
			name:  "actionable request changes at cap",
			state: atCap,
			observation: ObserveReviewer(rawVerdict(
				core.ReviewerVerdictRequestChanges, []string{"correctness"}, "still broken",
			)),
			wantPhase:     PhaseTerminal,
			wantIteration: 3,
			wantIntents: []IntentKind{
				IntentPersistRawLastVerdict,
				IntentPublishRawVerdict,
				IntentPublishIterationCapHit,
				IntentPublishCycleComplete,
			},
			wantReason:     core.ReviewLoopCompletionReasonCapHit,
			wantAttention:  true,
			wantRawVerdict: core.ReviewerVerdictRequestChanges,
		},
		{
			name:          "block",
			state:         awaitReviewer,
			observation:   ObserveReviewer(rawVerdict(core.ReviewerVerdictBlock, []string{"security"}, "unsafe")),
			wantPhase:     PhaseTerminal,
			wantIteration: 1,
			wantIntents: []IntentKind{
				IntentPersistRawLastVerdict,
				IntentPublishRawVerdict,
				IntentPublishCycleComplete,
			},
			wantReason:     core.ReviewLoopCompletionReasonBlocked,
			wantAttention:  true,
			wantRawVerdict: core.ReviewerVerdictBlock,
		},
		{
			name:          "terminal absorbs later observation",
			state:         historical,
			observation:   Observation{},
			wantPhase:     PhaseTerminal,
			wantIteration: 1,
			wantIntents:   nil,
			wantReason:    core.ReviewLoopCompletionReasonNoProgress,
			wantAttention: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Decide(tt.state, tt.observation)
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if got.NextState.Phase != tt.wantPhase {
				t.Errorf("phase = %q, want %q", got.NextState.Phase, tt.wantPhase)
			}
			if got.NextState.Iteration != tt.wantIteration {
				t.Errorf("iteration = %d, want %d", got.NextState.Iteration, tt.wantIteration)
			}
			if kinds := intentKinds(got.Intents); !reflect.DeepEqual(kinds, tt.wantIntents) {
				t.Errorf("intent kinds = %v, want %v", kinds, tt.wantIntents)
			}
			assertVerdictIntentFlagsNonNil(t, got.Intents)
			if tt.wantNoTerminal {
				if got.Terminal != nil {
					t.Fatalf("terminal = %+v, want nil", got.Terminal)
				}
				return
			}
			if got.Terminal == nil {
				t.Fatal("terminal = nil")
			}
			if got.Terminal.CompletionReason != tt.wantReason {
				t.Errorf("reason = %q, want %q", got.Terminal.CompletionReason, tt.wantReason)
			}
			if got.Terminal.Success != tt.wantSuccess {
				t.Errorf("success = %v, want %v", got.Terminal.Success, tt.wantSuccess)
			}
			if got.Terminal.NeedsAttention != tt.wantAttention {
				t.Errorf("needs attention = %v, want %v", got.Terminal.NeedsAttention, tt.wantAttention)
			}
			if tt.wantRawVerdict != "" {
				if got.Terminal.RawVerdict == nil {
					t.Fatal("raw verdict = nil")
				}
				if got.Terminal.RawVerdict.Verdict != tt.wantRawVerdict {
					t.Errorf("raw verdict = %q, want %q", got.Terminal.RawVerdict.Verdict, tt.wantRawVerdict)
				}
			}
		})
	}
}

func TestPropertyIterationMonotoneBoundedAndIdentitySelection(t *testing.T) {
	t.Parallel()

	for cap := 1; cap <= 12; cap++ {
		cap := cap
		t.Run("cap-"+itoa(cap), func(t *testing.T) {
			t.Parallel()

			state, err := NewState(cap, "stable-implementer-id", "baseline")
			if err != nil {
				t.Fatal(err)
			}
			reviewerDispatches := make(map[int]int)
			cycleCompletions := 0
			previousIteration := state.Iteration

			for {
				impl, decideErr := Decide(
					state,
					ObserveImplementer("head-"+itoa(state.Iteration), "same-diff", true),
				)
				if decideErr != nil {
					t.Fatal(decideErr)
				}
				assertMonotoneIteration(t, previousIteration, impl.NextState.Iteration, cap)
				previousIteration = impl.NextState.Iteration
				for _, intent := range impl.Intents {
					switch intent.Kind {
					case IntentDispatchFreshReviewer:
						reviewerDispatches[intent.Iteration]++
						if intent.Selection != SelectionFreshReviewer {
							t.Errorf("reviewer selection = %q", intent.Selection)
						}
						if intent.ContinuityIdentity != "" {
							t.Errorf("fresh reviewer inherited identity %q", intent.ContinuityIdentity)
						}
					case IntentPublishCycleComplete:
						cycleCompletions++
					}
				}
				if impl.Terminal != nil {
					state = impl.NextState
					break
				}

				review, reviewErr := Decide(
					impl.NextState,
					ObserveReviewer(rawVerdict(
						core.ReviewerVerdictRequestChanges,
						[]string{"correctness"},
						"keep fixing",
					)),
				)
				if reviewErr != nil {
					t.Fatal(reviewErr)
				}
				assertMonotoneIteration(t, previousIteration, review.NextState.Iteration, cap)
				previousIteration = review.NextState.Iteration
				for _, intent := range review.Intents {
					switch intent.Kind {
					case IntentDispatchImplementer:
						if intent.Selection != SelectionStableImplementer {
							t.Errorf("implementer selection = %q", intent.Selection)
						}
						if intent.ContinuityIdentity != "stable-implementer-id" {
							t.Errorf("implementer identity = %q", intent.ContinuityIdentity)
						}
					case IntentPublishCycleComplete:
						cycleCompletions++
					}
				}
				state = review.NextState
				if review.Terminal != nil {
					break
				}
			}

			if state.Iteration != cap {
				t.Errorf("terminal iteration = %d, want cap %d", state.Iteration, cap)
			}
			if cycleCompletions != 1 {
				t.Errorf("cycle completions = %d, want 1", cycleCompletions)
			}
			for iteration, count := range reviewerDispatches {
				if count != 1 {
					t.Errorf("reviewer dispatches at iteration %d = %d, want 1", iteration, count)
				}
			}
		})
	}
}

func TestPropertyTerminalAbsorption(t *testing.T) {
	t.Parallel()

	state := mustAwaitReviewer(t, mustState(t), "head-1", "diff-1")
	decision, err := Decide(
		state,
		ObserveReviewer(rawVerdict(core.ReviewerVerdictBlock, []string{"security"}, "blocked")),
	)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Terminal == nil {
		t.Fatal("first decision is not terminal")
	}

	observations := []Observation{
		ObserveFailure("late", "late failure"),
		ObserveCancellation("late cancellation"),
		ObserveImplementer("late-head", "late-diff", true),
		ObserveReviewer(rawVerdict(core.ReviewerVerdictApprove, nil, "late approval")),
		{},
	}
	for _, observation := range observations {
		absorbed, absorbErr := Decide(decision.NextState, observation)
		if absorbErr != nil {
			t.Fatalf("terminal Decide() error = %v", absorbErr)
		}
		if len(absorbed.Intents) != 0 {
			t.Errorf("terminal absorption emitted intents %v", intentKinds(absorbed.Intents))
		}
		if !reflect.DeepEqual(absorbed.Terminal, decision.Terminal) {
			t.Errorf("absorbed terminal = %+v, want %+v", absorbed.Terminal, decision.Terminal)
		}
		if !reflect.DeepEqual(absorbed.NextState, decision.NextState) {
			t.Errorf("absorbed state changed")
		}
	}
}

func TestPropertyDefensiveSliceHandling(t *testing.T) {
	t.Parallel()

	flags := []string{"correctness"}
	verdict := rawVerdict(core.ReviewerVerdictRequestChanges, flags, "fix it")
	observation := ObserveReviewer(verdict)
	flags[0] = "caller-mutated"
	verdict.Flags[0] = "verdict-mutated"

	state := mustAwaitReviewer(t, mustState(t), "head-1", "diff-1")
	decision, err := Decide(state, observation)
	if err != nil {
		t.Fatal(err)
	}
	if got := decision.NextState.PriorVerdict.Flags[0]; got != "correctness" {
		t.Fatalf("state flags = %q, want defensive copy", got)
	}

	rawIntent := findIntent(t, decision.Intents, IntentPublishRawVerdict)
	persistIntent := findIntent(t, decision.Intents, IntentPersistRawLastVerdict)
	resumeIntent := findIntent(t, decision.Intents, IntentDispatchImplementer)
	rawIntent.Verdict.Flags[0] = "intent-mutated"
	persistIntent.Verdict.Flags[0] = "persist-mutated"
	resumeIntent.Verdict.Flags[0] = "resume-mutated"
	if got := decision.NextState.PriorVerdict.Flags[0]; got != "correctness" {
		t.Errorf("intent mutation changed state flags to %q", got)
	}

	nextInput := decision.NextState
	next, nextErr := Decide(
		nextInput,
		ObserveImplementer("head-2", "diff-2", true),
	)
	if nextErr != nil {
		t.Fatal(nextErr)
	}
	nextInput.PriorVerdict.Flags[0] = "input-mutated"
	if got := next.Intents[1].Verdict; got != nil {
		t.Errorf("fresh reviewer intent verdict = %+v, want nil", got)
	}
	if got := next.NextState.PriorVerdict.Flags[0]; got != "correctness" {
		t.Errorf("caller mutation changed derived state flags to %q", got)
	}
}

func TestReviewerIntentOrderAndFlagNormalization(t *testing.T) {
	t.Parallel()

	awaitReviewer := mustAwaitReviewer(t, mustState(t), "head-1", "diff-1")
	tests := []struct {
		name         string
		state        State
		verdict      RawVerdict
		afterPublish IntentKind
		direct       bool
	}{
		{
			name:         "approve",
			state:        awaitReviewer,
			verdict:      rawVerdict(core.ReviewerVerdictApprove, nil, "approved"),
			afterPublish: IntentPublishCycleComplete,
			direct:       true,
		},
		{
			name:         "flagless request changes",
			state:        awaitReviewer,
			verdict:      rawVerdict(core.ReviewerVerdictRequestChanges, nil, "non-actionable"),
			afterPublish: IntentPublishCycleComplete,
		},
		{
			name:  "actionable request changes",
			state: awaitReviewer,
			verdict: rawVerdict(
				core.ReviewerVerdictRequestChanges, []string{"correctness"}, "fix",
			),
			afterPublish: IntentAdvanceIteration,
		},
		{
			name:         "block",
			state:        awaitReviewer,
			verdict:      rawVerdict(core.ReviewerVerdictBlock, nil, "blocked"),
			afterPublish: IntentPublishCycleComplete,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var observation Observation
			if tt.direct {
				verdict := tt.verdict
				observation = Observation{
					Kind:    ObservationReviewerComplete,
					Verdict: &verdict,
				}
			} else {
				observation = ObserveReviewer(tt.verdict)
				if observation.Verdict.Flags == nil {
					t.Fatal("ObserveReviewer left nil flags unnormalized")
				}
			}
			decision, err := Decide(tt.state, observation)
			if err != nil {
				t.Fatal(err)
			}
			kinds := intentKinds(decision.Intents)
			if len(kinds) < 3 {
				t.Fatalf("intent order too short: %v", kinds)
			}
			wantPrefix := []IntentKind{
				IntentPersistRawLastVerdict,
				IntentPublishRawVerdict,
				tt.afterPublish,
			}
			if !reflect.DeepEqual(kinds[:3], wantPrefix) {
				t.Errorf("intent prefix = %v, want %v", kinds[:3], wantPrefix)
			}
			assertVerdictIntentFlagsNonNil(t, decision.Intents)
		})
	}
}

func TestFixupIntentCarriesNonNilPriorFlags(t *testing.T) {
	t.Parallel()

	awaitReviewer := mustAwaitReviewer(t, mustState(t), "head-1", "diff-1")
	resume := mustResume(t, awaitReviewer, rawVerdict(
		core.ReviewerVerdictRequestChanges, []string{"correctness"}, "fix",
	))
	decision, err := Decide(resume, ObserveImplementer("head-1", "diff-1", true))
	if err != nil {
		t.Fatal(err)
	}
	fixup := findIntent(t, decision.Intents, IntentPublishFixupStalled)
	if fixup.Verdict == nil {
		t.Fatal("fixup intent verdict = nil")
	}
	if fixup.Verdict.Flags == nil {
		t.Fatal("fixup intent flags = nil")
	}
}

func TestPropertyBuiltInNeverProducesNoProgress(t *testing.T) {
	t.Parallel()

	observations := []Observation{
		ObserveFailure("failure", "failed"),
		ObserveCancellation("cancelled"),
		ObserveImplementer("baseline", "diff", false),
	}
	for _, observation := range observations {
		assertNoProducedNoProgress(t, mustState(t), observation)
	}

	awaitReviewer := mustAwaitReviewer(t, mustState(t), "head-1", "diff-1")
	for _, verdict := range []RawVerdict{
		rawVerdict(core.ReviewerVerdictApprove, nil, "approved"),
		rawVerdict(core.ReviewerVerdictRequestChanges, nil, "flagless"),
		rawVerdict(core.ReviewerVerdictRequestChanges, []string{"correctness"}, "actionable"),
		rawVerdict(core.ReviewerVerdictBlock, []string{"security"}, "blocked"),
	} {
		assertNoProducedNoProgress(t, awaitReviewer, ObserveReviewer(verdict))
	}

	resume := mustResume(t, awaitReviewer, rawVerdict(
		core.ReviewerVerdictRequestChanges, []string{"correctness"}, "fix",
	))
	assertNoProducedNoProgress(t, resume, ObserveImplementer("head-1", "diff-1", true))
}

func TestInvalidStateAndObservationDoNotReturnDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		state       State
		observation Observation
	}{
		{
			name:        "zero state",
			state:       State{},
			observation: ObserveFailure("failure", "failed"),
		},
		{
			name:        "wrong phase",
			state:       mustState(t),
			observation: ObserveReviewer(rawVerdict(core.ReviewerVerdictApprove, nil, "approved")),
		},
		{
			name:        "empty tagged union",
			state:       mustState(t),
			observation: Observation{},
		},
		{
			name:  "multiple payloads",
			state: mustState(t),
			observation: Observation{
				Kind:        ObservationImplementerComplete,
				Implementer: &ImplementerObservation{HeadSHA: "head", DiffHash: "diff"},
				Failure:     &FailureObservation{Code: "failure", Summary: "failed"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Decide(tt.state, tt.observation)
			if err == nil {
				t.Fatalf("Decide() = %+v, want error", got)
			}
			if !reflect.DeepEqual(got, Decision{}) {
				t.Errorf("decision on error = %+v, want zero", got)
			}
		})
	}
}

func mustState(t *testing.T) State {
	t.Helper()
	state, err := NewState(3, "stable-implementer-id", "baseline")
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func mustAwaitReviewer(t *testing.T, state State, head, diff string) State {
	t.Helper()
	decision, err := Decide(state, ObserveImplementer(head, diff, true))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Terminal != nil {
		t.Fatalf("implementer decision unexpectedly terminal: %+v", decision.Terminal)
	}
	return decision.NextState
}

func mustResume(t *testing.T, state State, verdict RawVerdict) State {
	t.Helper()
	decision, err := Decide(state, ObserveReviewer(verdict))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Terminal != nil {
		t.Fatalf("reviewer decision unexpectedly terminal: %+v", decision.Terminal)
	}
	return decision.NextState
}

func rawVerdict(verdict core.ReviewerVerdict, flags []string, notes string) RawVerdict {
	return RawVerdict{Verdict: verdict, Flags: flags, Notes: notes}
}

func intentKinds(intents []Intent) []IntentKind {
	if intents == nil {
		return nil
	}
	kinds := make([]IntentKind, len(intents))
	for i := range intents {
		kinds[i] = intents[i].Kind
	}
	return kinds
}

func findIntent(t *testing.T, intents []Intent, kind IntentKind) Intent {
	t.Helper()
	for _, intent := range intents {
		if intent.Kind == kind {
			return intent
		}
	}
	t.Fatalf("intent %q not found in %v", kind, intentKinds(intents))
	return Intent{}
}

func assertVerdictIntentFlagsNonNil(t *testing.T, intents []Intent) {
	t.Helper()
	for _, intent := range intents {
		switch intent.Kind {
		case IntentPersistRawLastVerdict, IntentPublishRawVerdict, IntentPublishFixupStalled:
			if intent.Verdict == nil {
				t.Errorf("%s verdict = nil", intent.Kind)
			} else if intent.Verdict.Flags == nil {
				t.Errorf("%s flags = nil", intent.Kind)
			}
		}
	}
}

func assertMonotoneIteration(t *testing.T, before, after, cap int) {
	t.Helper()
	if after < before {
		t.Errorf("iteration decreased: %d -> %d", before, after)
	}
	if after < 1 || after > cap {
		t.Errorf("iteration %d outside 1..%d", after, cap)
	}
}

func assertNoProducedNoProgress(t *testing.T, state State, observation Observation) {
	t.Helper()
	decision, err := Decide(state, observation)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Terminal != nil &&
		decision.Terminal.CompletionReason == core.ReviewLoopCompletionReasonNoProgress {
		t.Errorf("built-in decision produced historical no_progress")
	}
	for _, intent := range decision.Intents {
		if intent.CompletionReason == core.ReviewLoopCompletionReasonNoProgress {
			t.Errorf("built-in intent produced historical no_progress")
		}
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
