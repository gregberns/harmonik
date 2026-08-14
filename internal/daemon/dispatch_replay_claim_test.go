package daemon

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/dispatchstore"
)

type replayClaimLedger struct {
	err          error
	intentLogDir string
	runID        core.RunID
	transitionID core.TransitionID
	beadID       core.BeadID
	calls        int
}

func (l *replayClaimLedger) ClaimBead(
	_ context.Context,
	intentLogDir string,
	_ brcli.TimeoutConfig,
	runID core.RunID,
	transitionID core.TransitionID,
	beadID core.BeadID,
) error {
	l.calls++
	l.intentLogDir = intentLogDir
	l.runID = runID
	l.transitionID = transitionID
	l.beadID = beadID
	return l.err
}

func TestReplayClaimAdvancesExactSuccessfulClaim(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	writeReplayReaderIntent(t, projectDir, intent)
	ledger := &replayClaimLedger{}
	executor := dispatchReplayExecutor{projectDir: projectDir, intentLogDir: "intent-log", claimLedger: ledger}
	if err := executor.execute(t.Context(), dispatchReplayStep{Intent: intent, Action: dispatch.ReplayClaim}); err != nil {
		t.Fatal(err)
	}
	assertReplayClaimCall(t, ledger, intent)
	got, err := dispatchstore.New(projectDir).Load(intent.Binding.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != dispatch.PhaseClaimDurable || got.Refusal != nil {
		t.Fatalf("replayed intent = %+v", got)
	}
}

func TestReplayClaimMakesDependencyRefusalDurable(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	writeReplayReaderIntent(t, projectDir, intent)
	ledger := &replayClaimLedger{err: brcli.ErrClaimDependencyBlocked}
	executor := dispatchReplayExecutor{projectDir: projectDir, intentLogDir: "intent-log", claimLedger: ledger}
	if err := executor.execute(t.Context(), dispatchReplayStep{Intent: intent, Action: dispatch.ReplayClaim}); err != nil {
		t.Fatal(err)
	}
	got, err := dispatchstore.New(projectDir).Load(intent.Binding.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != dispatch.PhaseClaimRefused || got.Refusal == nil || got.Refusal.Cause != dispatch.ClaimRefusalDependency {
		t.Fatalf("refused intent = %+v", got)
	}
}

func TestReplayClaimPreservesPreparedIntentOnUnacceptedRefusal(t *testing.T) {
	unknown := errors.New("claim result is unknown")
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "unknown", err: unknown},
		{name: "already assigned", err: brcli.ErrClaimAlreadyAssigned},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
			writeReplayReaderIntent(t, projectDir, intent)
			ledger := &replayClaimLedger{err: tc.err}
			executor := dispatchReplayExecutor{projectDir: projectDir, intentLogDir: "intent-log", claimLedger: ledger}
			err := executor.execute(t.Context(), dispatchReplayStep{Intent: intent, Action: dispatch.ReplayClaim})
			if !errors.Is(err, tc.err) {
				t.Fatalf("replay claim error = %v", err)
			}
			got, loadErr := dispatchstore.New(projectDir).Load(intent.Binding.RunID)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if !reflect.DeepEqual(got, intent) {
				t.Fatalf("unaccepted claim changed intent: got %+v, want %+v", got, intent)
			}
		})
	}
}

func assertReplayClaimCall(t *testing.T, ledger *replayClaimLedger, intent dispatch.Intent) {
	t.Helper()
	if ledger.calls != 1 || ledger.intentLogDir != "intent-log" || ledger.runID != intent.Binding.RunID ||
		ledger.transitionID != intent.Binding.ClaimTransitionID || ledger.beadID != intent.Binding.BeadID {
		t.Fatalf("claim call = %+v", ledger)
	}
}
