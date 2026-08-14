package dispatch

import "testing"

func TestDecideReplayProcessDeathCuts(t *testing.T) {
	tests := []struct {
		name  string
		facts ReplayFacts
		want  ReplayAction
	}{
		{name: "D1 prepared before reservation", facts: replayFacts(PhasePrepared, QueueOfferable, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent), want: ReplayReservation},
		{name: "D2 reserved before claim", facts: replayFacts(PhasePrepared, QueueReserved, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent), want: ReplayClaim},
		{name: "D4 claim landed before phase", facts: withClaim(replayFacts(PhasePrepared, QueueReserved, BeadInProgress, RunRecordAbsent, WorktreeAbsent, SessionAbsent), ClaimMatching), want: AdvanceClaim},
		{name: "D5 claimed before record", facts: replayFacts(PhaseClaimDurable, QueueReserved, BeadInProgress, RunRecordAbsent, WorktreeAbsent, SessionAbsent), want: WriteRunRecord},
		{name: "D6 record before phase", facts: replayFacts(PhaseClaimDurable, QueueReserved, BeadInProgress, RunRecordBase, WorktreeAbsent, SessionAbsent), want: AdvanceRunPhase},
		{name: "D6a run before placement", facts: replayFacts(PhaseRunDurable, QueueReserved, BeadInProgress, RunRecordBase, WorktreeAbsent, SessionAbsent), want: ResumeProvision},
		{name: "D7 worktree before handoff", facts: replayFacts(PhaseRunDurable, QueueReserved, BeadInProgress, RunRecordLocated, WorktreePrepared, SessionAbsent), want: PrepareHandoff},
		{name: "D8 identity before phase", facts: replayFacts(PhaseRunDurable, QueueReserved, BeadInProgress, RunRecordSession, WorktreePrepared, SessionAbsent), want: AdvanceHandoffPhase},
		{name: "D8a phase before spawn", facts: replayFacts(PhaseHandoffDurable, QueueReserved, BeadInProgress, RunRecordSession, WorktreePrepared, SessionAbsent), want: ReplaySessionStart},
		{name: "D8b lease before spawn", facts: replayFacts(PhaseHandoffDurable, QueueReserved, BeadInProgress, RunRecordSession, WorktreeLeased, SessionAbsent), want: ReplaySessionStart},
		{name: "D9 live session", facts: withReceipt(replayFacts(PhaseHandoffDurable, QueueReserved, BeadInProgress, RunRecordSession, WorktreeLeased, SessionLive)), want: AdoptLive},
		{name: "dead before receipt", facts: replayFacts(PhaseHandoffDurable, QueueReserved, BeadInProgress, RunRecordSession, WorktreeLeased, SessionDead), want: RemoveDeadUnstartedTarget},
		{name: "dead without outcome", facts: withReceipt(replayFacts(PhaseHandoffDurable, QueueReserved, BeadInProgress, RunRecordSession, WorktreeLeased, SessionDead)), want: ResumeDead},
		{name: "dead with outcome", facts: withOutcome(withReceipt(replayFacts(PhaseHandoffDurable, QueueReserved, BeadInProgress, RunRecordSession, WorktreeLeased, SessionDead))), want: AdvanceRunOutcome},
		{name: "closed bead", facts: withMatchingGit(replayFacts(PhasePrepared, QueueReserved, BeadClosed, RunRecordAbsent, WorktreeAbsent, SessionAbsent)), want: AdvanceClose},
		{name: "terminal success", facts: withMatchingGit(replayFacts(PhaseHandoffDurable, QueueTerminalSuccess, BeadClosed, RunRecordSession, WorktreeLeased, SessionDead)), want: ReplayCleanupOnly},
		{name: "terminal retryable", facts: replayFacts(PhaseHandoffDurable, QueueTerminalRetryable, BeadOpen, RunRecordSession, WorktreeLeased, SessionDead), want: ReplayCleanupOnly},
		{name: "terminal unreopened", facts: replayFacts(PhaseHandoffDurable, QueueTerminalUnreopened, BeadInProgress, RunRecordSession, WorktreeLeased, SessionDead), want: ReplayRepairRequired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecideReplay(tc.facts)
			if err != nil || got != tc.want {
				t.Fatalf("DecideReplay() = (%q, %v), want %q", got, err, tc.want)
			}
		})
	}
}

func TestDecideReplayRejectsWorktreePhaseContradictions(t *testing.T) {
	for _, facts := range []ReplayFacts{
		replayFacts(PhaseRunDurable, QueueReserved, BeadInProgress, RunRecordBase, WorktreePrepared, SessionAbsent),
		replayFacts(PhaseRunDurable, QueueReserved, BeadInProgress, RunRecordLocated, WorktreeLeased, SessionAbsent),
	} {
		got, err := DecideReplay(facts)
		if err != nil || got != ReplayRepairRequired {
			t.Fatalf("DecideReplay() = (%q, %v)", got, err)
		}
	}
}

func TestDecideReplayConflictsFailClosed(t *testing.T) {
	base := replayFacts(PhaseHandoffDurable, QueueReserved, BeadInProgress, RunRecordSession, WorktreeLeased, SessionLive)
	mutations := []func(*ReplayFacts){
		func(f *ReplayFacts) { f.Queue = QueueConflict },
		func(f *ReplayFacts) { f.Bead = BeadConflict },
		func(f *ReplayFacts) { f.RunRecord = RunRecordConflict },
		func(f *ReplayFacts) { f.Worktree = WorktreeConflict },
		func(f *ReplayFacts) { f.Session = SessionConflict },
		func(f *ReplayFacts) { f.SessionReceipt = SessionReceiptConflict },
	}
	for index, mutate := range mutations {
		facts := base
		mutate(&facts)
		got, err := DecideReplay(facts)
		if err != nil || got != ReplayRepairRequired {
			t.Fatalf("conflict %d = (%q, %v)", index, got, err)
		}
	}
}

func TestDecideReplaySessionReceiptTargetMatrix(t *testing.T) {
	base := replayFacts(PhaseHandoffDurable, QueueReserved, BeadInProgress, RunRecordSession, WorktreeLeased, SessionAbsent)
	for _, tc := range []struct {
		name    string
		receipt SessionReceiptFact
		target  SessionFact
		want    ReplayAction
	}{
		{name: "absent receipt absent target", receipt: SessionReceiptAbsent, target: SessionAbsent, want: ReplaySessionStart},
		{name: "exact receipt live target", receipt: SessionReceiptExact, target: SessionLive, want: AdoptLive},
		{name: "exact receipt absent target", receipt: SessionReceiptExact, target: SessionAbsent, want: ResumeDead},
		{name: "exact receipt dead target", receipt: SessionReceiptExact, target: SessionDead, want: ResumeDead},
		{name: "absent receipt live target", receipt: SessionReceiptAbsent, target: SessionLive, want: ReplayRepairRequired},
		{name: "absent receipt dead target", receipt: SessionReceiptAbsent, target: SessionDead, want: RemoveDeadUnstartedTarget},
		{name: "conflicting receipt", receipt: SessionReceiptConflict, target: SessionAbsent, want: ReplayRepairRequired},
		{name: "conflicting target", receipt: SessionReceiptAbsent, target: SessionConflict, want: ReplayRepairRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := base
			facts.SessionReceipt, facts.Session = tc.receipt, tc.target
			if tc.receipt == SessionReceiptAbsent && tc.target == SessionAbsent {
				facts.Worktree = WorktreePrepared
			}
			got, err := DecideReplay(facts)
			if err != nil || got != tc.want {
				t.Fatalf("DecideReplay() = (%q, %v), want %q", got, err, tc.want)
			}
		})
	}
}

func TestDecideReplaySessionReceiptTargetOutcomeMatrix(t *testing.T) {
	base := withOutcome(replayFacts(
		PhaseHandoffDurable, QueueReserved, BeadInProgress, RunRecordSession, WorktreeLeased, SessionAbsent,
	))
	for _, tc := range []struct {
		name    string
		receipt SessionReceiptFact
		target  SessionFact
		want    ReplayAction
	}{
		{name: "absent receipt absent target", receipt: SessionReceiptAbsent, target: SessionAbsent, want: ReplayRepairRequired},
		{name: "absent receipt live target", receipt: SessionReceiptAbsent, target: SessionLive, want: ReplayRepairRequired},
		{name: "absent receipt dead target", receipt: SessionReceiptAbsent, target: SessionDead, want: ReplayRepairRequired},
		{name: "exact receipt absent target", receipt: SessionReceiptExact, target: SessionAbsent, want: AdvanceRunOutcome},
		{name: "exact receipt live target", receipt: SessionReceiptExact, target: SessionLive, want: ReplayRepairRequired},
		{name: "exact receipt dead target", receipt: SessionReceiptExact, target: SessionDead, want: AdvanceRunOutcome},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := base
			facts.SessionReceipt, facts.Session = tc.receipt, tc.target
			got, err := DecideReplay(facts)
			if err != nil || got != tc.want {
				t.Fatalf("DecideReplay() = (%q, %v), want %q", got, err, tc.want)
			}
		})
	}
}

func TestDecideReplayRejectsReceiptBeforeHandoffOnEarlyBranches(t *testing.T) {
	for _, facts := range []ReplayFacts{
		withReceipt(replayFacts(PhasePrepared, QueueReserved, BeadClosed, RunRecordAbsent, WorktreeAbsent, SessionAbsent)),
		withReceipt(withRefusal(replayFacts(PhaseClaimRefused, QueueReserved, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent), ClaimRefusalDependency)),
		withReceipt(replayFacts(PhaseClaimDurable, QueueReserved, BeadInProgress, RunRecordBase, WorktreeAbsent, SessionAbsent)),
		withReceipt(replayFacts(PhaseRunDurable, QueueReserved, BeadInProgress, RunRecordSession, WorktreeLeased, SessionAbsent)),
	} {
		got, err := DecideReplay(facts)
		if err != nil || got != ReplayRepairRequired {
			t.Fatalf("DecideReplay() = (%q, %v), want repair", got, err)
		}
	}
}

func TestDecideReplayRejectsImpossiblePhaseCoupling(t *testing.T) {
	tests := []ReplayFacts{
		replayFacts(PhasePrepared, QueueOfferable, BeadOpen, RunRecordBase, WorktreeAbsent, SessionAbsent),
		replayFacts(PhaseClaimDurable, QueueOfferable, BeadInProgress, RunRecordAbsent, WorktreeAbsent, SessionAbsent),
		replayFacts(PhaseClaimDurable, QueueReserved, BeadInProgress, RunRecordLocated, WorktreeAbsent, SessionAbsent),
		replayFacts(PhaseClaimDurable, QueueReserved, BeadInProgress, RunRecordSession, WorktreeLeased, SessionAbsent),
		replayFacts(PhaseRunDurable, QueueReserved, BeadInProgress, RunRecordSession, WorktreeAbsent, SessionAbsent),
		replayFacts(PhaseHandoffDurable, QueueReserved, BeadInProgress, RunRecordLocated, WorktreeLeased, SessionLive),
	}
	for index, facts := range tests {
		got, err := DecideReplay(facts)
		if err != nil || got != ReplayRepairRequired {
			t.Fatalf("impossible %d = (%q, %v)", index, got, err)
		}
	}
}

func TestDecideReplayTypedClaimRefusals(t *testing.T) {
	for _, tc := range []struct {
		bead  BeadFact
		claim ClaimFact
		want  ReplayAction
	}{
		{bead: BeadOpen, claim: ClaimDependencyRefusal, want: AdvanceClaimRefusal},
		{bead: BeadOpen, claim: ClaimAlreadyAssigned, want: ReplayRepairRequired},
		{bead: BeadOther, claim: ClaimExternalRefusal, want: AdvanceClaimRefusal},
		{bead: BeadOpen, claim: ClaimConflict, want: ReplayRepairRequired},
	} {
		facts := replayFacts(PhasePrepared, QueueReserved, tc.bead, RunRecordAbsent, WorktreeAbsent, SessionAbsent)
		got, err := DecideReplay(withClaim(facts, tc.claim))
		if err != nil || got != tc.want {
			t.Fatalf("claim %q = (%q, %v), want %q", tc.claim, got, err, tc.want)
		}
	}
}

func TestDecideReplayPreclaimCompensationStages(t *testing.T) {
	tests := []struct {
		name     string
		facts    ReplayFacts
		refusal  ClaimRefusalCause
		preclaim PreclaimFact
		want     ReplayAction
	}{
		{name: "dependency refusal before item failure", facts: replayFacts(PhaseClaimRefused, QueueReserved, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent), refusal: ClaimRefusalDependency, preclaim: PreclaimAbsent, want: FailQueueItem},
		{name: "dependency item needs group", facts: replayFacts(PhaseClaimRefused, QueueTerminalUnreopened, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent), refusal: ClaimRefusalDependency, preclaim: PreclaimDependencyItemTerminal, want: FinalizePreclaimGroup},
		{name: "dependency group permits removal", facts: replayFacts(PhaseClaimRefused, QueueTerminalUnreopened, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent), refusal: ClaimRefusalDependency, preclaim: PreclaimDependencyGroupDurable, want: RemoveDispatchIntent},
		{name: "max attempts item needs group", facts: replayFacts(PhasePrepared, QueueTerminalUnreopened, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent), preclaim: PreclaimMaxAttemptsItemTerminal, want: FinalizePreclaimGroup},
		{name: "max attempts group permits removal", facts: replayFacts(PhasePrepared, QueueTerminalUnreopened, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent), preclaim: PreclaimMaxAttemptsGroupDurable, want: RemoveDispatchIntent},
		{name: "cross queue item needs group", facts: replayFacts(PhasePrepared, QueueTerminalUnreopened, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent), preclaim: PreclaimCrossQueueItemTerminal, want: FinalizePreclaimGroup},
		{name: "cross queue group permits removal", facts: replayFacts(PhasePrepared, QueueTerminalUnreopened, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent), preclaim: PreclaimCrossQueueGroupDurable, want: RemoveDispatchIntent},
		{name: "supported non-open needs release", facts: replayFacts(PhaseClaimRefused, QueueReserved, BeadOther, RunRecordAbsent, WorktreeAbsent, SessionAbsent), refusal: ClaimRefusalSupportedNonOpen, preclaim: PreclaimAbsent, want: ReleaseReservation},
		{name: "durable release permits removal", facts: replayFacts(PhaseClaimRefused, QueueOfferable, BeadOther, RunRecordAbsent, WorktreeAbsent, SessionAbsent), refusal: ClaimRefusalSupportedNonOpen, preclaim: PreclaimReleased, want: RemoveDispatchIntent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			facts := tc.facts
			facts.RefusalCause = tc.refusal
			facts.Preclaim = tc.preclaim
			got, err := DecideReplay(facts)
			if err != nil || got != tc.want {
				t.Fatalf("DecideReplay() = (%q, %v), want %q", got, err, tc.want)
			}
		})
	}
}

func TestDecideReplayPreclaimCrossCauseAndStageMismatchRepairs(t *testing.T) {
	tests := []ReplayFacts{
		withPreclaim(withRefusal(replayFacts(PhaseClaimRefused, QueueTerminalUnreopened, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent), ClaimRefusalDependency), PreclaimCrossQueueItemTerminal),
		withPreclaim(replayFacts(PhasePrepared, QueueTerminalUnreopened, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent), PreclaimDependencyItemTerminal),
		withPreclaim(withRefusal(replayFacts(PhaseClaimRefused, QueueReserved, BeadOther, RunRecordAbsent, WorktreeAbsent, SessionAbsent), ClaimRefusalSupportedNonOpen), PreclaimReleased),
		withPreclaim(withRefusal(replayFacts(PhaseClaimRefused, QueueOfferable, BeadOther, RunRecordAbsent, WorktreeAbsent, SessionAbsent), ClaimRefusalDependency), PreclaimReleased),
		withPreclaim(replayFacts(PhasePrepared, QueueTerminalUnreopened, BeadClosed, RunRecordAbsent, WorktreeAbsent, SessionAbsent), PreclaimMaxAttemptsGroupDurable),
	}
	for index, facts := range tests {
		assertRepair(t, index, facts)
	}
}

func TestDecideReplayPreclaimRejectsQueueStageAndLaterArtifacts(t *testing.T) {
	base := withPreclaim(replayFacts(PhasePrepared, QueueTerminalUnreopened, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent), PreclaimMaxAttemptsItemTerminal)
	mutations := []func(*ReplayFacts){
		func(f *ReplayFacts) { f.Queue = QueueOfferable },
		func(f *ReplayFacts) { f.Queue = QueueReserved },
		func(f *ReplayFacts) { f.RunRecord = RunRecordBase },
		func(f *ReplayFacts) { f.Worktree = WorktreeLeased },
		func(f *ReplayFacts) { f.Session = SessionDead },
		func(f *ReplayFacts) { f.Git = GitMatching },
		func(f *ReplayFacts) { f.RunOutcomeDurable = true },
		func(f *ReplayFacts) { f.Claim = ClaimMatching },
	}
	for index, mutate := range mutations {
		facts := base
		mutate(&facts)
		assertRepair(t, index, facts)
	}

	refused := withRefusal(replayFacts(PhaseClaimRefused, QueueTerminalUnreopened, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent), ClaimRefusalDependency)
	for index, stage := range []PreclaimFact{PreclaimDependencyItemTerminal, PreclaimDependencyGroupDurable} {
		facts := withPreclaim(refused, stage)
		facts.Queue = QueueOfferable
		assertRepair(t, index+20, facts)
		facts.Queue = QueueReserved
		assertRepair(t, index+30, facts)
	}
}

func TestDecideReplayAcceptsPartialTerminalResidue(t *testing.T) {
	tests := []struct {
		name  string
		facts ReplayFacts
		want  ReplayAction
	}{
		{
			name:  "closed before close phase with no resources",
			facts: withMatchingGit(replayFacts(PhaseHandoffDurable, QueueReserved, BeadClosed, RunRecordAbsent, WorktreeAbsent, SessionAbsent)),
			want:  AdvanceClose,
		},
		{
			name:  "successful queue outcome after all resources removed",
			facts: withMatchingGit(replayFacts(PhaseHandoffDurable, QueueTerminalSuccess, BeadClosed, RunRecordAbsent, WorktreeAbsent, SessionAbsent)),
			want:  ReplayCleanupOnly,
		},
		{
			name:  "successful queue outcome with dead session record",
			facts: withMatchingGit(replayFacts(PhaseHandoffDurable, QueueTerminalSuccess, BeadClosed, RunRecordSession, WorktreeAbsent, SessionDead)),
			want:  ReplayCleanupOnly,
		},
		{
			name:  "retryable queue outcome after all resources removed",
			facts: replayFacts(PhaseHandoffDurable, QueueTerminalRetryable, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent),
			want:  ReplayCleanupOnly,
		},
		{
			name:  "live session prevents terminal cleanup",
			facts: withMatchingGit(replayFacts(PhaseHandoffDurable, QueueTerminalSuccess, BeadClosed, RunRecordSession, WorktreeLeased, SessionLive)),
			want:  ReplayRepairRequired,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecideReplay(tc.facts)
			if err != nil || got != tc.want {
				t.Fatalf("DecideReplay() = (%q, %v), want %q", got, err, tc.want)
			}
		})
	}
}

func TestDecideReplayTerminalCleanupAcceptsReceiptResidue(t *testing.T) {
	for _, tc := range []struct {
		name  string
		facts ReplayFacts
	}{
		{
			name: "success receipt absent",
			facts: withMatchingGit(replayFacts(
				PhaseHandoffDurable, QueueTerminalSuccess, BeadClosed, RunRecordSession, WorktreeAbsent, SessionDead,
			)),
		},
		{
			name: "success receipt exact",
			facts: withReceipt(withMatchingGit(replayFacts(
				PhaseHandoffDurable, QueueTerminalSuccess, BeadClosed, RunRecordSession, WorktreeAbsent, SessionDead,
			))),
		},
		{
			name: "retryable receipt absent",
			facts: replayFacts(
				PhaseHandoffDurable, QueueTerminalRetryable, BeadOpen, RunRecordSession, WorktreeAbsent, SessionDead,
			),
		},
		{
			name: "retryable receipt exact",
			facts: withReceipt(replayFacts(
				PhaseHandoffDurable, QueueTerminalRetryable, BeadOpen, RunRecordSession, WorktreeAbsent, SessionDead,
			)),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecideReplay(tc.facts)
			if err != nil || got != ReplayCleanupOnly {
				t.Fatalf("DecideReplay() = (%q, %v), want cleanup", got, err)
			}
		})
	}
}

func TestDecideReplayRequiresGitEvidenceForClosedAndTerminalSuccess(t *testing.T) {
	for _, facts := range []ReplayFacts{
		replayFacts(PhasePrepared, QueueReserved, BeadClosed, RunRecordAbsent, WorktreeAbsent, SessionAbsent),
		replayFacts(PhaseHandoffDurable, QueueTerminalSuccess, BeadClosed, RunRecordSession, WorktreeLeased, SessionDead),
	} {
		got, err := DecideReplay(facts)
		if err != nil || got != ReplayRepairRequired {
			t.Fatalf("DecideReplay() = (%q, %v)", got, err)
		}
	}
}

func TestDecideReplayRejectsCrossPhaseFacts(t *testing.T) {
	prepared := replayFacts(PhasePrepared, QueueOfferable, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent)
	preparedMutations := []func(*ReplayFacts){
		func(f *ReplayFacts) { f.RunRecord = RunRecordBase },
		func(f *ReplayFacts) { f.Worktree = WorktreeLeased },
		func(f *ReplayFacts) { f.Session = SessionLive },
		func(f *ReplayFacts) { f.SessionReceipt = SessionReceiptExact },
		func(f *ReplayFacts) { f.Git = GitMatching },
		func(f *ReplayFacts) { f.RunOutcomeDurable = true },
	}
	for index, mutate := range preparedMutations {
		facts := prepared
		mutate(&facts)
		assertRepair(t, index, facts)
	}

	claimDurable := replayFacts(PhaseClaimDurable, QueueReserved, BeadInProgress, RunRecordAbsent, WorktreeAbsent, SessionAbsent)
	claimMutations := []func(*ReplayFacts){
		func(f *ReplayFacts) { f.Claim = ClaimDependencyRefusal },
		func(f *ReplayFacts) { f.Worktree = WorktreeLeased },
		func(f *ReplayFacts) { f.Session = SessionLive },
		func(f *ReplayFacts) { f.SessionReceipt = SessionReceiptExact },
		func(f *ReplayFacts) { f.RunOutcomeDurable = true },
	}
	for index, mutate := range claimMutations {
		facts := claimDurable
		mutate(&facts)
		assertRepair(t, index+len(preparedMutations), facts)
	}

	for index, facts := range []ReplayFacts{
		withClaim(replayFacts(PhasePrepared, QueueReserved, BeadInProgress, RunRecordAbsent, WorktreeAbsent, SessionAbsent), ClaimDependencyRefusal),
		withClaim(replayFacts(PhasePrepared, QueueReserved, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent), ClaimExternalRefusal),
		withClaim(replayFacts(PhasePrepared, QueueReserved, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent), ClaimMatching),
		replayFacts(PhasePrepared, QueueTerminalSuccess, BeadClosed, RunRecordAbsent, WorktreeAbsent, SessionAbsent),
		replayFacts(PhaseClaimDurable, QueueTerminalRetryable, BeadOpen, RunRecordBase, WorktreeAbsent, SessionAbsent),
	} {
		assertRepair(t, index+20, facts)
	}
}

func assertRepair(t *testing.T, index int, facts ReplayFacts) {
	t.Helper()
	got, err := DecideReplay(facts)
	if err != nil || got != ReplayRepairRequired {
		t.Fatalf("cross-phase %d = (%q, %v): %+v", index, got, err, facts)
	}
}

func TestDecideReplayRejectsUnknownFacts(t *testing.T) {
	for _, mutate := range []func(*ReplayFacts){
		func(f *ReplayFacts) { f.Session = "unknown" },
		func(f *ReplayFacts) { f.SessionReceipt = "unknown" },
	} {
		facts := replayFacts(PhasePrepared, QueueOfferable, BeadOpen, RunRecordAbsent, WorktreeAbsent, SessionAbsent)
		mutate(&facts)
		if _, err := DecideReplay(facts); err == nil {
			t.Fatal("DecideReplay() = nil error")
		}
	}
}

func replayFacts(phase Phase, queue QueueFact, bead BeadFact, record RunRecordFact, worktree WorktreeFact, session SessionFact) ReplayFacts {
	return ReplayFacts{
		IntentPhase: phase, Queue: queue, Bead: bead, RunRecord: record,
		Worktree: worktree, Session: session, SessionReceipt: SessionReceiptAbsent,
		Git: GitAbsent, Claim: ClaimNone, Preclaim: PreclaimAbsent,
	}
}

func withReceipt(facts ReplayFacts) ReplayFacts {
	facts.SessionReceipt = SessionReceiptExact
	return facts
}

func withMatchingGit(facts ReplayFacts) ReplayFacts {
	facts.Git = GitMatching
	return facts
}

func withClaim(facts ReplayFacts, claim ClaimFact) ReplayFacts {
	facts.Claim = claim
	return facts
}

func withOutcome(facts ReplayFacts) ReplayFacts {
	facts.RunOutcomeDurable = true
	return facts
}

func withPreclaim(facts ReplayFacts, preclaim PreclaimFact) ReplayFacts {
	facts.Preclaim = preclaim
	return facts
}

func withRefusal(facts ReplayFacts, cause ClaimRefusalCause) ReplayFacts {
	facts.RefusalCause = cause
	return facts
}
