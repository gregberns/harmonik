// Package reviewcycle implements the deterministic review-loop policy kernel.
//
// The package consumes already-observed facts and returns ordered semantic
// intents. It deliberately owns no effectful mechanism: callers remain
// responsible for git, persistence, event publication, process lifecycle,
// artifact transfer, and Beads mutations.
package reviewcycle

import (
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// Phase is the next phase whose observation the cycle accepts.
type Phase string

const (
	PhaseAwaitImplementer Phase = "await-implementer"
	PhaseAwaitReviewer    Phase = "await-reviewer"
	PhaseTerminal         Phase = "terminal"
)

// Valid reports whether p is a declared cycle phase.
func (p Phase) Valid() bool {
	switch p {
	case PhaseAwaitImplementer, PhaseAwaitReviewer, PhaseTerminal:
		return true
	default:
		return false
	}
}

// RawVerdict preserves the validated agent-reviewer verdict without applying
// routing normalization. In particular, a flagless REQUEST_CHANGES remains a
// raw REQUEST_CHANGES even though the kernel routes it as approved.
type RawVerdict struct {
	Verdict core.ReviewerVerdict
	Flags   []string
	Notes   string
}

// Valid reports whether v is a validated agent-reviewer schema-v1 value.
func (v RawVerdict) Valid() bool {
	return v.Verdict.Valid() && v.Notes != ""
}

// State is the complete value state of one review cycle.
//
// PriorWorkProductHeadSHA is the HEAD recorded immediately before the prior
// reviewer launch. CurrentDiffHash and PriorDiffHash are evidence only; routing
// uses HEAD advancement.
type State struct {
	Phase                         Phase
	Iteration                     int
	Cap                           int
	ImplementerContinuityIdentity string
	InitialBaselineSHA            string
	PriorWorkProductHeadSHA       string
	CurrentDiffHash               string
	PriorDiffHash                 string
	PriorVerdict                  *RawVerdict
	Terminal                      *TerminalResult
}

// NewState constructs the state before the initial implementer observation.
func NewState(cap int, continuityIdentity, initialBaselineSHA string) (State, error) {
	state := State{
		Phase:                         PhaseAwaitImplementer,
		Iteration:                     1,
		Cap:                           cap,
		ImplementerContinuityIdentity: continuityIdentity,
		InitialBaselineSHA:            initialBaselineSHA,
	}
	if err := state.valid(); err != nil {
		return State{}, err
	}
	return state, nil
}

// ObservationKind discriminates the fact supplied to Decide.
type ObservationKind string

const (
	ObservationImplementerComplete ObservationKind = "implementer-complete"
	ObservationReviewerComplete    ObservationKind = "reviewer-complete"
	ObservationFailed              ObservationKind = "failed"
	ObservationCancelled           ObservationKind = "cancelled"
)

// ImplementerObservation contains facts resolved by the implementer shell.
//
// WorkProductAdvanced distinguishes a real initial work-product change from a
// control-checkpoint-only or harness-churn commit. From iteration two onward,
// the normative routing predicate is HEAD advancement from the prior reviewer
// baseline, irrespective of cumulative diff-hash equality.
type ImplementerObservation struct {
	HeadSHA             string
	DiffHash            string
	WorkProductAdvanced bool
}

// FailureObservation is a typed shell failure or cancellation.
type FailureObservation struct {
	Code    string
	Summary string
}

// Observation is a tagged union. Exactly one payload matching Kind must be
// present. Use the Observe* constructors to build a valid value.
type Observation struct {
	Kind        ObservationKind
	Implementer *ImplementerObservation
	Verdict     *RawVerdict
	Failure     *FailureObservation
}

// ObserveImplementer constructs a completed-implementer observation.
func ObserveImplementer(headSHA, diffHash string, workProductAdvanced bool) Observation {
	return Observation{
		Kind: ObservationImplementerComplete,
		Implementer: &ImplementerObservation{
			HeadSHA:             headSHA,
			DiffHash:            diffHash,
			WorkProductAdvanced: workProductAdvanced,
		},
	}
}

// ObserveReviewer constructs a validated reviewer-verdict observation.
func ObserveReviewer(verdict RawVerdict) Observation {
	copy := cloneVerdict(&verdict)
	return Observation{Kind: ObservationReviewerComplete, Verdict: copy}
}

// ObserveFailure constructs a typed phase-failure observation.
func ObserveFailure(code, summary string) Observation {
	return Observation{
		Kind:    ObservationFailed,
		Failure: &FailureObservation{Code: code, Summary: summary},
	}
}

// ObserveCancellation constructs a typed cancellation observation.
func ObserveCancellation(summary string) Observation {
	return Observation{
		Kind:    ObservationCancelled,
		Failure: &FailureObservation{Code: "cancelled", Summary: summary},
	}
}

// SelectionPolicy describes the continuity selection attached to a dispatch
// intent. Implementer resumes reuse the stable authoritative identity;
// reviewers always request a fresh identity.
type SelectionPolicy string

const (
	SelectionStableImplementer SelectionPolicy = "stable-implementer"
	SelectionFreshReviewer     SelectionPolicy = "fresh-reviewer"
)

// IntentKind identifies one ordered semantic intent returned by the kernel.
type IntentKind string

const (
	IntentPersistIterationFacts  IntentKind = "persist-iteration-facts"
	IntentDispatchFreshReviewer  IntentKind = "dispatch-fresh-reviewer"
	IntentPersistRawLastVerdict  IntentKind = "persist-raw-last-verdict"
	IntentPublishRawVerdict      IntentKind = "publish-raw-reviewer-verdict"
	IntentAdvanceIteration       IntentKind = "advance-iteration"
	IntentDispatchImplementer    IntentKind = "dispatch-implementer-resume"
	IntentPublishFixupStalled    IntentKind = "publish-fixup-stalled"
	IntentPublishIterationCapHit IntentKind = "publish-iteration-cap-hit"
	IntentPublishCycleComplete   IntentKind = "publish-cycle-complete"
)

// Intent is one semantic instruction for the imperative shells/projector.
// Fields not applicable to Kind remain zero-valued.
type Intent struct {
	Kind               IntentKind
	Iteration          int
	Selection          SelectionPolicy
	ContinuityIdentity string
	HeadSHA            string
	CurrentDiffHash    string
	PriorDiffHash      string
	Verdict            *RawVerdict
	CompletionReason   core.ReviewLoopCompletionReason
}

// TerminalResult is the normalized terminal cycle outcome.
type TerminalResult struct {
	Success          bool
	CompletionReason core.ReviewLoopCompletionReason
	NeedsAttention   bool
	Summary          string
	RawVerdict       *RawVerdict
}

// Decision is the pure result of applying one observation to one state.
type Decision struct {
	NextState State
	Intents   []Intent
	Terminal  *TerminalResult
}

// Decide applies one observed fact to the cycle state.
//
// A terminal state absorbs every later observation: it returns the identical
// terminal value with no new intent, even when the later observation itself is
// malformed. This makes terminal completion exactly-once at the kernel edge.
func Decide(input State, observation Observation) (Decision, error) {
	state := cloneState(input)
	if err := state.valid(); err != nil {
		return Decision{}, fmt.Errorf("reviewcycle: invalid state: %w", err)
	}
	if state.Phase == PhaseTerminal {
		return Decision{
			NextState: cloneState(state),
			Terminal:  cloneTerminal(state.Terminal),
		}, nil
	}
	if err := observation.valid(); err != nil {
		return Decision{}, fmt.Errorf("reviewcycle: invalid observation: %w", err)
	}

	switch observation.Kind {
	case ObservationFailed, ObservationCancelled:
		return terminalDecision(
			state,
			nil,
			TerminalResult{
				Success:          false,
				CompletionReason: core.ReviewLoopCompletionReasonError,
				NeedsAttention:   true,
				Summary:          observation.Failure.Summary,
			},
		), nil

	case ObservationImplementerComplete:
		if state.Phase != PhaseAwaitImplementer {
			return Decision{}, fmt.Errorf(
				"reviewcycle: observation %q is invalid in phase %q",
				observation.Kind, state.Phase,
			)
		}
		return decideImplementer(state, *observation.Implementer), nil

	case ObservationReviewerComplete:
		if state.Phase != PhaseAwaitReviewer {
			return Decision{}, fmt.Errorf(
				"reviewcycle: observation %q is invalid in phase %q",
				observation.Kind, state.Phase,
			)
		}
		return decideReviewer(state, *observation.Verdict)

	default:
		return Decision{}, fmt.Errorf("reviewcycle: unsupported observation kind %q", observation.Kind)
	}
}

func decideImplementer(state State, observed ImplementerObservation) Decision {
	priorDiffHash := state.CurrentDiffHash
	state.PriorDiffHash = priorDiffHash
	state.CurrentDiffHash = observed.DiffHash

	if state.Iteration == 1 {
		advanced := observed.WorkProductAdvanced && observed.HeadSHA != state.InitialBaselineSHA
		if !advanced {
			return terminalDecision(
				state,
				nil,
				TerminalResult{
					Success:          false,
					CompletionReason: core.ReviewLoopCompletionReasonError,
					NeedsAttention:   true,
					Summary:          "initial implementer produced no work-product commit",
				},
			)
		}
	} else if observed.HeadSHA == state.PriorWorkProductHeadSHA {
		fixup := Intent{
			Kind:            IntentPublishFixupStalled,
			Iteration:       state.Iteration,
			CurrentDiffHash: observed.DiffHash,
			PriorDiffHash:   priorDiffHash,
			Verdict:         cloneVerdict(state.PriorVerdict),
		}
		return terminalDecision(
			state,
			[]Intent{fixup},
			TerminalResult{
				Success:          false,
				CompletionReason: core.ReviewLoopCompletionReasonFixupStalled,
				NeedsAttention:   true,
				Summary: fmt.Sprintf(
					"review fix-up stalled at iteration %d: HEAD unchanged after REQUEST_CHANGES",
					state.Iteration,
				),
			},
		)
	}

	state.PriorWorkProductHeadSHA = observed.HeadSHA
	state.Phase = PhaseAwaitReviewer
	return Decision{
		NextState: cloneState(state),
		Intents: []Intent{
			{
				Kind:            IntentPersistIterationFacts,
				Iteration:       state.Iteration,
				HeadSHA:         observed.HeadSHA,
				CurrentDiffHash: observed.DiffHash,
				PriorDiffHash:   priorDiffHash,
			},
			{
				Kind:      IntentDispatchFreshReviewer,
				Iteration: state.Iteration,
				Selection: SelectionFreshReviewer,
			},
		},
	}
}

func decideReviewer(state State, observed RawVerdict) (Decision, error) {
	verdict := cloneVerdict(&observed)
	state.PriorVerdict = cloneVerdict(verdict)

	intents := []Intent{
		{
			Kind:      IntentPersistRawLastVerdict,
			Iteration: state.Iteration,
			Verdict:   cloneVerdict(verdict),
		},
		{
			Kind:      IntentPublishRawVerdict,
			Iteration: state.Iteration,
			Verdict:   cloneVerdict(verdict),
		},
	}

	switch verdict.Verdict {
	case core.ReviewerVerdictApprove:
		return terminalDecision(
			state,
			intents,
			TerminalResult{
				Success:          true,
				CompletionReason: core.ReviewLoopCompletionReasonApproved,
				NeedsAttention:   false,
				Summary:          fmt.Sprintf("APPROVE at iteration %d", state.Iteration),
				RawVerdict:       cloneVerdict(verdict),
			},
		), nil

	case core.ReviewerVerdictBlock:
		return terminalDecision(
			state,
			intents,
			TerminalResult{
				Success:          false,
				CompletionReason: core.ReviewLoopCompletionReasonBlocked,
				NeedsAttention:   true,
				Summary:          fmt.Sprintf("BLOCK at iteration %d", state.Iteration),
				RawVerdict:       cloneVerdict(verdict),
			},
		), nil

	case core.ReviewerVerdictRequestChanges:
		if len(verdict.Flags) == 0 {
			return terminalDecision(
				state,
				intents,
				TerminalResult{
					Success:          true,
					CompletionReason: core.ReviewLoopCompletionReasonApproved,
					NeedsAttention:   false,
					Summary: fmt.Sprintf(
						"REQUEST_CHANGES with no flags at iteration %d treated as APPROVE",
						state.Iteration,
					),
					RawVerdict: cloneVerdict(verdict),
				},
			), nil
		}
		if state.Iteration >= state.Cap {
			intents = append(intents, Intent{
				Kind:      IntentPublishIterationCapHit,
				Iteration: state.Iteration,
				Verdict:   cloneVerdict(verdict),
			})
			return terminalDecision(
				state,
				intents,
				TerminalResult{
					Success:          false,
					CompletionReason: core.ReviewLoopCompletionReasonCapHit,
					NeedsAttention:   true,
					Summary: fmt.Sprintf(
						"REQUEST_CHANGES at iteration %d (cap=%d)",
						state.Iteration, state.Cap,
					),
					RawVerdict: cloneVerdict(verdict),
				},
			), nil
		}

		state.Iteration++
		state.Phase = PhaseAwaitImplementer
		intents = append(
			intents,
			Intent{
				Kind:      IntentAdvanceIteration,
				Iteration: state.Iteration,
			},
			Intent{
				Kind:               IntentDispatchImplementer,
				Iteration:          state.Iteration,
				Selection:          SelectionStableImplementer,
				ContinuityIdentity: state.ImplementerContinuityIdentity,
				Verdict:            cloneVerdict(verdict),
				CurrentDiffHash:    state.CurrentDiffHash,
				PriorDiffHash:      state.PriorDiffHash,
			},
		)
		return Decision{
			NextState: cloneState(state),
			Intents:   cloneIntents(intents),
		}, nil
	}

	return Decision{}, fmt.Errorf(
		"reviewcycle: validated verdict %q reached an unsupported routing branch",
		verdict.Verdict,
	)
}

func terminalDecision(state State, prefix []Intent, terminal TerminalResult) Decision {
	terminalCopy := cloneTerminal(&terminal)
	state.Phase = PhaseTerminal
	state.Terminal = cloneTerminal(terminalCopy)

	intents := cloneIntents(prefix)
	intents = append(intents, Intent{
		Kind:             IntentPublishCycleComplete,
		Iteration:        state.Iteration,
		CompletionReason: terminal.CompletionReason,
		Verdict:          cloneVerdict(terminal.RawVerdict),
	})
	return Decision{
		NextState: cloneState(state),
		Intents:   intents,
		Terminal:  cloneTerminal(terminalCopy),
	}
}

func (s State) valid() error {
	if !s.Phase.Valid() {
		return fmt.Errorf("phase %q is not valid", s.Phase)
	}
	if s.Cap < 1 {
		return fmt.Errorf("cap must be positive, got %d", s.Cap)
	}
	if s.Iteration < 1 || s.Iteration > s.Cap {
		return fmt.Errorf("iteration %d is outside 1..%d", s.Iteration, s.Cap)
	}
	if s.ImplementerContinuityIdentity == "" {
		return fmt.Errorf("implementer continuity identity must not be empty")
	}
	if s.InitialBaselineSHA == "" {
		return fmt.Errorf("initial baseline SHA must not be empty")
	}
	if s.PriorVerdict != nil && !s.PriorVerdict.Valid() {
		return fmt.Errorf("prior verdict is not valid")
	}
	if s.Iteration > 1 {
		if s.PriorWorkProductHeadSHA == "" {
			return fmt.Errorf("iteration %d requires a prior work-product HEAD", s.Iteration)
		}
		if s.PriorVerdict == nil ||
			s.PriorVerdict.Verdict != core.ReviewerVerdictRequestChanges ||
			len(s.PriorVerdict.Flags) == 0 {
			return fmt.Errorf("iteration %d requires a prior actionable REQUEST_CHANGES verdict", s.Iteration)
		}
	}
	if s.Phase == PhaseAwaitReviewer && s.PriorWorkProductHeadSHA == "" {
		return fmt.Errorf("reviewer phase requires a work-product HEAD")
	}
	if s.Phase == PhaseTerminal {
		if s.Terminal == nil {
			return fmt.Errorf("terminal phase requires a terminal result")
		}
		if err := s.Terminal.valid(); err != nil {
			return fmt.Errorf("terminal result: %w", err)
		}
	} else if s.Terminal != nil {
		return fmt.Errorf("non-terminal phase must not carry a terminal result")
	}
	return nil
}

func (o Observation) valid() error {
	switch o.Kind {
	case ObservationImplementerComplete:
		if o.Implementer == nil || o.Verdict != nil || o.Failure != nil {
			return fmt.Errorf("implementer-complete requires only an implementer payload")
		}
		if o.Implementer.HeadSHA == "" {
			return fmt.Errorf("implementer HEAD SHA must not be empty")
		}
		if o.Implementer.DiffHash == "" {
			return fmt.Errorf("implementer diff hash must not be empty")
		}
	case ObservationReviewerComplete:
		if o.Verdict == nil || o.Implementer != nil || o.Failure != nil {
			return fmt.Errorf("reviewer-complete requires only a verdict payload")
		}
		if !o.Verdict.Valid() {
			return fmt.Errorf("reviewer verdict is not valid")
		}
	case ObservationFailed, ObservationCancelled:
		if o.Failure == nil || o.Implementer != nil || o.Verdict != nil {
			return fmt.Errorf("%s requires only a failure payload", o.Kind)
		}
		if o.Failure.Code == "" {
			return fmt.Errorf("%s failure code must not be empty", o.Kind)
		}
		if o.Failure.Summary == "" {
			return fmt.Errorf("%s failure summary must not be empty", o.Kind)
		}
	default:
		return fmt.Errorf("observation kind %q is not valid", o.Kind)
	}
	return nil
}

func (t TerminalResult) valid() error {
	if !t.CompletionReason.Valid() {
		return fmt.Errorf("completion reason %q is not valid", t.CompletionReason)
	}
	if t.Summary == "" {
		return fmt.Errorf("summary must not be empty")
	}
	if t.RawVerdict != nil && !t.RawVerdict.Valid() {
		return fmt.Errorf("raw verdict is not valid")
	}
	switch t.CompletionReason {
	case core.ReviewLoopCompletionReasonApproved:
		if !t.Success || t.NeedsAttention {
			return fmt.Errorf("approved must be successful without needs-attention")
		}
	case core.ReviewLoopCompletionReasonCapHit,
		core.ReviewLoopCompletionReasonBlocked,
		core.ReviewLoopCompletionReasonNoProgress,
		core.ReviewLoopCompletionReasonError,
		core.ReviewLoopCompletionReasonFixupStalled:
		if t.Success || !t.NeedsAttention {
			return fmt.Errorf(
				"completion %q must fail with needs-attention",
				t.CompletionReason,
			)
		}
	}
	return nil
}

func cloneVerdict(verdict *RawVerdict) *RawVerdict {
	if verdict == nil {
		return nil
	}
	copy := *verdict
	// The workspace parser normalizes JSON null to an empty list. Preserve that
	// contract at the kernel boundary as well so every persisted/published raw
	// verdict and every later fix-up intent carries a non-nil flags slice.
	copy.Flags = append([]string{}, verdict.Flags...)
	return &copy
}

func cloneTerminal(terminal *TerminalResult) *TerminalResult {
	if terminal == nil {
		return nil
	}
	copy := *terminal
	copy.RawVerdict = cloneVerdict(terminal.RawVerdict)
	return &copy
}

func cloneState(state State) State {
	copy := state
	copy.PriorVerdict = cloneVerdict(state.PriorVerdict)
	copy.Terminal = cloneTerminal(state.Terminal)
	return copy
}

func cloneIntents(intents []Intent) []Intent {
	if intents == nil {
		return nil
	}
	copies := make([]Intent, len(intents))
	for i := range intents {
		copies[i] = intents[i]
		copies[i].Verdict = cloneVerdict(intents[i].Verdict)
	}
	return copies
}
