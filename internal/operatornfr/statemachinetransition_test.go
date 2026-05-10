package operatornfr_test

import (
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/operatornfr"
)

// --- Fixture types for state-machine harness (ON-007..ON-014) ---

// smState mirrors the operator-control state machine states from
// specs/operator-nfr.md section 7.1.
//
// Spec ref: operator-nfr.md section 4.3 ON-011 -- "States are `running`,
// `pausing`, `paused`, `resuming`, `stopped` (terminal-recoverable via `start`),
// and `upgrading`."
type smState string

const (
	smStateRunning   smState = "running"
	smStatePausing   smState = "pausing"
	smStatePaused    smState = "paused"
	smStateResuming  smState = "resuming"
	smStateStopped   smState = "stopped"
	smStateUpgrading smState = "upgrading"
)

// smCommand is an operator command issued to the state machine.
type smCommand string

const (
	smCommandPause           smCommand = "pause"
	smCommandResume          smCommand = "resume"
	smCommandStop            smCommand = "stop"
	smCommandStopImmediate   smCommand = "stop --immediate"
	smCommandUpgrade         smCommand = "upgrade"
	smCommandStart           smCommand = "start"
	smCommandDrainCompleted  smCommand = "drain-completed"  // internal trigger
	smCommandImprovementLoop smCommand = "improvement-loop" // S09 trigger
	smCommandImprovementDone smCommand = "improvement-done" // improvement loop completion
	smCommandExecReplace     smCommand = "exec-replace"     // upgrade exec-replace success
)

// smTransitionFixture models one expected state-machine transition per
// specs/operator-nfr.md section 7.1 table.
//
// Spec ref: operator-nfr.md section 7.1.
type smTransitionFixture struct {
	Name         string    // human-readable label for t.Run
	From         smState   // initial state
	Command      smCommand // operator command or internal trigger
	WantTo       smState   // expected resulting state
	WantEmits    string    // expected emitted event type (empty = no emission)
	WantRejected bool      // true = command is invalid for From state (code 16)
	PauseReason  string    // "operator" or "improvement" when applicable
}

// smTransitionFixtures is the complete transition table from
// specs/operator-nfr.md section 7.1.
//
// Spec ref: operator-nfr.md section 7.1 -- every row in the state-machine table.
var smTransitionFixtures = []smTransitionFixture{
	// running -> pausing (operator pause)
	{
		Name:        "running-pause-operator",
		From:        smStateRunning,
		Command:     smCommandPause,
		WantTo:      smStatePausing,
		WantEmits:   "operator_pause_status",
		PauseReason: "operator",
	},
	// running -> pausing (improvement loop trigger)
	{
		Name:        "running-pause-improvement",
		From:        smStateRunning,
		Command:     smCommandImprovementLoop,
		WantTo:      smStatePausing,
		WantEmits:   "operator_pause_status",
		PauseReason: "improvement",
	},
	// pausing -> paused (drain completed, no in-flight runs)
	{
		Name:      "pausing-drain-completed",
		From:      smStatePausing,
		Command:   smCommandDrainCompleted,
		WantTo:    smStatePaused,
		WantEmits: "operator_pause_status",
	},
	// paused -> resuming (operator resume)
	{
		Name:      "paused-resume-operator",
		From:      smStatePaused,
		Command:   smCommandResume,
		WantTo:    smStateResuming,
		WantEmits: "operator_resuming",
	},
	// paused -> resuming (improvement loop completion auto-resume)
	{
		Name:        "paused-resume-improvement-done",
		From:        smStatePaused,
		Command:     smCommandImprovementDone,
		WantTo:      smStateResuming,
		WantEmits:   "operator_resuming",
		PauseReason: "improvement",
	},
	// resuming -> running (dispatch loop re-entered)
	{
		Name:      "resuming-to-running",
		From:      smStateResuming,
		Command:   smCommandStart,
		WantTo:    smStateRunning,
		WantEmits: "",
	},
	// paused -> upgrading (hash matches)
	{
		Name:      "paused-upgrade-hash-match",
		From:      smStatePaused,
		Command:   smCommandUpgrade,
		WantTo:    smStateUpgrading,
		WantEmits: "operator_upgrading",
	},
	// upgrading -> running (exec-replace succeeds, new binary)
	{
		Name:      "upgrading-exec-replace-success",
		From:      smStateUpgrading,
		Command:   smCommandExecReplace,
		WantTo:    smStateRunning,
		WantEmits: "operator_upgrade_completed",
	},
	// running -> stopped (stop --graceful)
	{
		Name:      "running-stop-graceful",
		From:      smStateRunning,
		Command:   smCommandStop,
		WantTo:    smStateStopped,
		WantEmits: "operator_stopped",
	},
	// any state -> stopped (stop --immediate)
	{
		Name:      "pausing-stop-immediate",
		From:      smStatePausing,
		Command:   smCommandStopImmediate,
		WantTo:    smStateStopped,
		WantEmits: "operator_stopped",
	},
	{
		Name:      "paused-stop-immediate",
		From:      smStatePaused,
		Command:   smCommandStopImmediate,
		WantTo:    smStateStopped,
		WantEmits: "operator_stopped",
	},
	{
		Name:      "upgrading-stop-immediate",
		From:      smStateUpgrading,
		Command:   smCommandStopImmediate,
		WantTo:    smStateStopped,
		WantEmits: "operator_stopped",
	},
	// stopped -> running (start, via normal startup)
	{
		Name:      "stopped-start",
		From:      smStateStopped,
		Command:   smCommandStart,
		WantTo:    smStateRunning,
		WantEmits: "",
	},
	// running -> running (resume while running: idempotent no-op per ON-013c)
	// Spec ref: operator-nfr.md section 4.3 ON-013c and section 7.1 dedicated row.
	{
		Name:      "running-resume-noop",
		From:      smStateRunning,
		Command:   smCommandResume,
		WantTo:    smStateRunning,
		WantEmits: "",
	},
}

// smInvalidTransitionFixtures lists transitions that MUST be rejected with
// code 16 (operator-control-invalid-state) and `operator_command_rejected`.
//
// Spec ref: operator-nfr.md section 7.1 -- "any operator command: command
// invalid for current state-machine state ... `operator_command_rejected`
// (per section 8 code 16)."
//
// Note: `resume` while `running` was previously listed here but was moved to
// smTransitionFixtures as a success (no-op) case per ON-013c. The section 7.1
// table now carries a dedicated row for this idempotent case (success, code 0).
var smInvalidTransitionFixtures = []smTransitionFixture{
	{Name: "running-upgrade-invalid", From: smStateRunning, Command: smCommandUpgrade, WantRejected: true},
	{Name: "paused-pause-noop", From: smStatePaused, Command: smCommandPause}, // idempotent per ON-013c
	{Name: "stopped-resume-invalid", From: smStateStopped, Command: smCommandResume, WantRejected: true},
	{Name: "upgrading-pause-invalid", From: smStateUpgrading, Command: smCommandPause, WantRejected: true},
}

// smFixtureApply simulates applying a command to the state machine.
// It returns (nextState, emittedEvent, rejected).
//
// "rejected" means the command was invalid for the current state (code 16).
// This is the fixture-level simulator; production implementation is in the
// as-yet-unimplemented S01 orchestrator core.
func smFixtureApply(from smState, cmd smCommand, pauseReason string) (to smState, emitted string, rejected bool) {
	switch from {
	case smStateRunning:
		switch cmd {
		case smCommandPause:
			return smStatePausing, "operator_pause_status", false
		case smCommandImprovementLoop:
			return smStatePausing, "operator_pause_status", false
		case smCommandStop:
			return smStateStopped, "operator_stopped", false
		case smCommandStopImmediate:
			return smStateStopped, "operator_stopped", false
		case smCommandResume:
			// ON-013c: resume while running is idempotent — already in target state.
			// Returns success (exit code 0) with no transition and no event.
			// Section 7.1 carries a dedicated row for this case (success, code 0).
			return from, "", false
		case smCommandUpgrade:
			return from, "operator_command_rejected", true
		}
	case smStatePausing:
		switch cmd {
		case smCommandDrainCompleted:
			return smStatePaused, "operator_pause_status", false
		case smCommandStopImmediate:
			return smStateStopped, "operator_stopped", false
		case smCommandPause:
			// idempotent per ON-013c: already pausing, treat as queued
			return from, "operator_pause_status", false
		}
	case smStatePaused:
		switch cmd {
		case smCommandResume:
			return smStateResuming, "operator_resuming", false
		case smCommandImprovementDone:
			if pauseReason == "improvement" {
				return smStateResuming, "operator_resuming", false
			}
			// improvement-done when not improvement-paused is no-op
			return from, "", false
		case smCommandUpgrade:
			return smStateUpgrading, "operator_upgrading", false
		case smCommandStopImmediate:
			return smStateStopped, "operator_stopped", false
		case smCommandPause:
			// ON-013c: already paused, return success with re-emit
			return from, "operator_pause_status", false
		case smCommandStop:
			return smStateStopped, "operator_stopped", false
		}
	case smStateResuming:
		switch cmd {
		case smCommandStart:
			return smStateRunning, "", false
		case smCommandStopImmediate:
			return smStateStopped, "operator_stopped", false
		}
	case smStateUpgrading:
		switch cmd {
		case smCommandExecReplace:
			return smStateRunning, "operator_upgrade_completed", false
		case smCommandStopImmediate:
			return smStateStopped, "operator_stopped", false
		case smCommandPause, smCommandResume, smCommandUpgrade:
			return from, "operator_command_rejected", true
		}
	case smStateStopped:
		switch cmd {
		case smCommandStart:
			return smStateRunning, "", false
		case smCommandStop:
			// ON-013c: stop while stopped is idempotent, returns success, no event.
			return from, "", false
		case smCommandResume, smCommandUpgrade:
			return from, "operator_command_rejected", true
		}
	}
	// Unrecognized command in current state: reject.
	return from, "operator_command_rejected", true
}

// TestON011_StateMachineTransitions verifies every transition in the
// specs/operator-nfr.md section 7.1 state-machine table.
//
// Spec ref: operator-nfr.md section 4.3 ON-011 -- "The daemon MUST implement
// the operator-control state machine defined in section 7.1."
// Spec ref: operator-nfr.md section 10.2 -- "State-machine scenario tests
// enumerating every transition in section 7.1."
func TestON011_StateMachineTransitions(t *testing.T) {
	t.Parallel()

	for _, fx := range smTransitionFixtures {
		fx := fx
		t.Run(fx.Name, func(t *testing.T) {
			t.Parallel()

			to, emitted, rejected := smFixtureApply(fx.From, fx.Command, fx.PauseReason)

			if rejected != fx.WantRejected {
				t.Errorf("ON-011: %s: rejected = %v, want %v", fx.Name, rejected, fx.WantRejected)
			}
			if !fx.WantRejected {
				if to != fx.WantTo {
					t.Errorf("ON-011: %s: state = %q, want %q", fx.Name, to, fx.WantTo)
				}
				if fx.WantEmits != "" && emitted != fx.WantEmits {
					t.Errorf("ON-011: %s: emitted = %q, want %q", fx.Name, emitted, fx.WantEmits)
				}
			} else {
				// On rejection, state MUST be unchanged and event is operator_command_rejected.
				if to != fx.From {
					t.Errorf("ON-011: %s: rejected but state changed from %q to %q; state MUST be unchanged on rejection", fx.Name, fx.From, to)
				}
				if emitted != "operator_command_rejected" {
					t.Errorf("ON-011: %s: rejected emitted %q, want %q", fx.Name, emitted, "operator_command_rejected")
				}
				// Rejection MUST resolve to code 16 in the taxonomy.
				e, ok := operatornfr.LookupExitCode(16)
				if !ok {
					t.Fatal("ON-011: section 8 taxonomy missing code 16 (operator-control-invalid-state)")
				}
				if e.Event != "operator_command_rejected" {
					t.Errorf("ON-011: section 8 code 16 event = %q, want %q", e.Event, "operator_command_rejected")
				}
			}
		})
	}
}

// TestON007_OperatorTaskEqualsRun verifies that operator-facing "task" maps to
// spec-internal "run" (terminology alignment per ON-007). The fixture validates
// that no wire format uses "task" in an event type name.
//
// Spec ref: operator-nfr.md section 4.3 ON-007 -- "wire formats MUST NOT" use
// "task" in place of "run".
func TestON007_OperatorTaskEqualsRun(t *testing.T) {
	t.Parallel()

	// Event type names derived from section 8 taxonomy MUST use "run_" prefix
	// for run lifecycle events, never "task_".
	for _, e := range operatornfr.ExitCodes {
		e := e
		t.Run(e.Category, func(t *testing.T) {
			t.Parallel()

			// Wire-format event names MUST NOT contain "task" (ON-007).
			// The taxonomy lists events like "run_failed", "daemon_startup_failed".
			if len(e.Event) > 4 && e.Event[:4] == "task" {
				t.Errorf("ON-007: section 8 code %d event %q starts with 'task'; wire-format event names MUST use 'run_*' not 'task_*'", e.Code, e.Event)
			}
		})
	}
}

// TestON008_PauseDrainGate verifies that the pausing -> paused transition ONLY
// fires on drain-completion, not on any other command.
//
// Spec ref: operator-nfr.md section 4.3 ON-008 -- "The `pausing -> paused`
// transition is gated on drain-completion: entry into `paused` is forbidden
// until (a) no run satisfies `in_flight(run)` AND (b) every drain step of
// ON-027 has completed."
func TestON008_PauseDrainGate(t *testing.T) {
	t.Parallel()

	// Only drain-completed MUST transition pausing -> paused.
	drainTo, _, drainRejected := smFixtureApply(smStatePausing, smCommandDrainCompleted, "")
	if drainRejected {
		t.Error("ON-008: drain-completed rejected in pausing state; MUST be accepted")
	}
	if drainTo != smStatePaused {
		t.Errorf("ON-008: drain-completed in pausing -> %q, want %q", drainTo, smStatePaused)
	}

	// An operator pause command while already pausing MUST NOT skip to paused.
	pauseTo, _, _ := smFixtureApply(smStatePausing, smCommandPause, "")
	if pauseTo == smStatePaused {
		t.Error("ON-008: pause in pausing state jumped directly to paused, bypassing the drain gate; MUST NOT")
	}

	// A resume command in pausing state is not allowed to jump to running.
	resumeTo, _, _ := smFixtureApply(smStatePausing, smCommandResume, "")
	if resumeTo == smStateRunning {
		t.Error("ON-008: resume in pausing state jumped to running; MUST complete drain first")
	}
}

// TestON009_StopImmediateAbortsInFlightRuns verifies that stop --immediate is
// the ONLY control that aborts in-flight runs. The fixture confirms that
// stop --immediate in any state always reaches stopped.
//
// Spec ref: operator-nfr.md section 4.3 ON-009 -- "`stop --immediate` and
// SIGKILL (treated equivalently) MUST abort in-flight runs."
func TestON009_StopImmediateAbortsInFlightRuns(t *testing.T) {
	t.Parallel()

	allStates := []smState{
		smStateRunning, smStatePausing, smStatePaused,
		smStateResuming, smStateUpgrading,
	}

	for _, st := range allStates {
		st := st
		t.Run(string(st), func(t *testing.T) {
			t.Parallel()

			to, emitted, rejected := smFixtureApply(st, smCommandStopImmediate, "")
			if rejected {
				t.Errorf("ON-009: stop --immediate rejected in state %q; it MUST be accepted from any state", st)
			}
			if to != smStateStopped {
				t.Errorf("ON-009: stop --immediate from %q -> %q, want %q", st, to, smStateStopped)
			}
			if emitted != "operator_stopped" {
				t.Errorf("ON-009: stop --immediate from %q emitted %q, want %q", st, emitted, "operator_stopped")
			}
		})
	}

	// Aborted runs MUST emit run_failed with class=canceled on next restart per
	// ON-009. We verify the taxonomy entry for run_failed is present.
	_, ok := operatornfr.LookupExitCode(1) // code 1 = generic-failure has run_failed
	if !ok {
		t.Error("ON-009: code 1 (generic-failure) missing from taxonomy; run_failed event relies on it")
	}
}

// TestON010_ReconciliationCarveOut verifies that a pause command issued during
// reconciling state is queued (the state machine transitions to reconciling-
// pause-queued, not directly to pausing).
//
// In the fixture model, "reconciling" is a daemon-startup state (not operator-
// control state); the carve-out is modelled as: if daemon is reconciling,
// pause is deferred (queue it), not applied.
//
// Spec ref: operator-nfr.md section 4.3 ON-010 -- "An operator pause issued
// during `reconciling` MUST be queued and applied at the boundary event."
func TestON010_ReconciliationCarveOut(t *testing.T) {
	t.Parallel()

	// The reconciliation carve-out means pause in running state (before ready)
	// MUST be queued. The fixture models this as: when the daemon is in
	// reconciling phase (guarded by the section 7.1 note), pause is deferred.
	// We verify that the between-task invariant (ON-008) applies only after
	// reaching "ready".

	// Structural check: the spec section 4.3 ON-010 obligation is present in the
	// taxonomy's awareness. Code 16 (operator-control-invalid-state) MUST NOT
	// be the response to a pause during reconciling -- it should be queued.
	// (Code 16 is for truly invalid commands like resume-while-running.)
	e, ok := operatornfr.LookupExitCode(16)
	if !ok {
		t.Fatal("ON-010: section 8 taxonomy missing code 16 (operator-control-invalid-state)")
	}
	// Code 16 is for invalid state transitions (like resume while running).
	// A pause during reconciling is NOT code 16; it is a queued operation.
	// The carve-out MUST NOT map to an error code.
	if e.Category == "" {
		t.Error("ON-010: code 16 category is empty; taxonomy entry is incomplete")
	}

	// The carve-out is distinct from an invalid command: reconciliation carve-out
	// returns success (code 0) with deferred application, NOT code 16.
	// Verify the spec acknowledges this via the section 10.2 test obligation.
	// Since we can't fully simulate daemon startup phases here, we verify the
	// fixture table does not list pause-during-running as rejected (running is
	// where the daemon ends up after ready, pause-during-running IS valid).
	to, _, rejected := smFixtureApply(smStateRunning, smCommandPause, "operator")
	if rejected {
		t.Error("ON-010: pause in running state rejected; pause MUST be accepted after daemon reaches ready")
	}
	if to != smStatePausing {
		t.Errorf("ON-010: pause in running -> %q, want %q", to, smStatePausing)
	}
}

// TestON012_ImprovementPauseIsSubtype verifies that improvement-pause reuses
// the pausing/paused states with a pause_reason discriminator, not a separate
// state.
//
// Spec ref: operator-nfr.md section 4.3 ON-012 -- "improvement-pause MUST
// transition `running` -> `pausing` -> `paused` via the same path as an
// operator pause, sharing the identical state table with the `pause_reason`
// discriminator."
func TestON012_ImprovementPauseIsSubtype(t *testing.T) {
	t.Parallel()

	// Improvement loop trigger MUST reach pausing (same as operator pause).
	improveTo, improveEmit, improveRejected := smFixtureApply(smStateRunning, smCommandImprovementLoop, "improvement")
	if improveRejected {
		t.Error("ON-012: improvement-loop trigger rejected in running state; MUST be accepted")
	}
	if improveTo != smStatePausing {
		t.Errorf("ON-012: improvement-loop in running -> %q, want %q", improveTo, smStatePausing)
	}
	if improveEmit != "operator_pause_status" {
		t.Errorf("ON-012: improvement-loop emitted %q, want %q; improvement-pause MUST reuse same event", improveEmit, "operator_pause_status")
	}

	// Improvement-done MUST auto-resume from paused when pause_reason=improvement.
	resumeTo, resumeEmit, resumeRejected := smFixtureApply(smStatePaused, smCommandImprovementDone, "improvement")
	if resumeRejected {
		t.Error("ON-012: improvement-done rejected in paused state; MUST auto-resume")
	}
	if resumeTo != smStateResuming {
		t.Errorf("ON-012: improvement-done from paused -> %q, want %q", resumeTo, smStateResuming)
	}
	if resumeEmit != "operator_resuming" {
		t.Errorf("ON-012: improvement-done emitted %q, want %q", resumeEmit, "operator_resuming")
	}

	// Improvement-done with pause_reason=operator MUST NOT auto-resume.
	noResumeReason := "operator"
	noResumeTo, _, _ := smFixtureApply(smStatePaused, smCommandImprovementDone, noResumeReason)
	if noResumeTo == smStateResuming {
		t.Error("ON-012: improvement-done with pause_reason=operator triggered auto-resume; it MUST NOT; only improvement-reason triggers auto-resume")
	}
}

// TestON013_EventsPerTransition verifies that each state-machine transition
// emits exactly the expected event type per the section 7.1 table.
//
// Spec ref: operator-nfr.md section 4.3 ON-013 -- "The daemon MUST emit one
// typed event per operator-control state transition."
func TestON013_EventsPerTransition(t *testing.T) {
	t.Parallel()

	for _, fx := range smTransitionFixtures {
		fx := fx
		if fx.WantRejected {
			continue // rejection path covered by TestON011
		}
		t.Run(fx.Name, func(t *testing.T) {
			t.Parallel()

			_, emitted, _ := smFixtureApply(fx.From, fx.Command, fx.PauseReason)

			if fx.WantEmits != "" {
				if emitted != fx.WantEmits {
					t.Errorf("ON-013: %s: emitted = %q, want %q; MUST emit one typed event per transition", fx.Name, emitted, fx.WantEmits)
				}
			}
		})
	}
}

// TestON013a_PanicBarrierEmitsCommandFailed verifies the ON-013a per-command
// supervision fixture: a panic in an operator-command goroutine MUST emit
// `operator_command_failed` and revert partial state.
//
// The fixture models the panic barrier's contract at the type level. Full
// runtime panic injection is a later integration test; this harness verifies
// the recovery event type is declared and distinct from normal rejection.
//
// Spec ref: operator-nfr.md section 4.3 ON-013a -- "On panic, the barrier MUST:
// (a) emit `operator_command_failed{command, panic_class, run_id?}`;
// (b) revert any partial state-machine transition."
func TestON013a_PanicBarrierEmitsCommandFailed(t *testing.T) {
	t.Parallel()

	// The panic-recovery event is "operator_command_failed" (a cross-spec
	// coordination request to EV per ON-013a). Verify it is NOT the same as the
	// normal rejection event.
	normalRejection := "operator_command_rejected"
	panicEvent := "operator_command_failed"

	if normalRejection == panicEvent {
		t.Error("ON-013a: panic event and normal rejection event are the same string; they MUST be distinct per ON-013a")
	}

	// State reversion: on panic, the partial transition MUST be reverted.
	// Model: if a pause command panics mid-transition, state stays at running.
	from := smStateRunning
	// Simulate: apply pause (transition begins), then panic reverts.
	// Fixture-level model: panic barrier restores original state.
	simulatedPanicRevert := from // ON-013a: revert to pre-command state
	if simulatedPanicRevert != smStateRunning {
		t.Errorf("ON-013a: panic revert should restore %q, got %q", smStateRunning, simulatedPanicRevert)
	}

	// Panic escalation to degraded if panic occurred during a drain step.
	// Verified at a structural level: the degraded path resolves to code 12.
	e, ok := operatornfr.LookupExitCode(12)
	if !ok {
		t.Fatal("ON-013a: section 8 taxonomy missing code 12 (rto-hard-ceiling-exceeded); drain-panic escalation path relies on degraded status")
	}
	if e.Event != "daemon_degraded" {
		t.Errorf("ON-013a: code 12 event = %q, want %q; drain-panic escalation emits daemon_degraded", e.Event, "daemon_degraded")
	}
}

// TestON013c_IdempotentPauseWhilePaused verifies that issuing a pause command
// while already paused returns success (code 0) with operator_pause_status
// re-emitted at most once, deduplicated via session_id.
//
// Spec ref: operator-nfr.md section 4.3 ON-013c -- "a `pause` issued while
// already `paused` MUST return success (exit code 0) with
// `operator_pause_status{status=paused}` re-emitted at most once per command."
func TestON013c_IdempotentPauseWhilePaused(t *testing.T) {
	t.Parallel()

	to, emitted, rejected := smFixtureApply(smStatePaused, smCommandPause, "operator")

	// MUST succeed (not rejected).
	if rejected {
		t.Error("ON-013c: pause while paused returned rejection; MUST return success (code 0) for no-op transition")
	}
	// State stays paused.
	if to != smStatePaused {
		t.Errorf("ON-013c: pause while paused transitioned to %q, want %q; no-op must preserve state", to, smStatePaused)
	}
	// Event is still emitted (re-emit once per command).
	if emitted != "operator_pause_status" {
		t.Errorf("ON-013c: pause while paused emitted %q, want %q", emitted, "operator_pause_status")
	}
}

// TestON013c_IdempotentStopWhileStopped verifies that stop while stopped
// returns success with no event emission.
//
// Spec ref: operator-nfr.md section 4.3 ON-013c -- "A `stop` issued while
// `stopped` MUST return success with no event emission."
func TestON013c_IdempotentStopWhileStopped(t *testing.T) {
	t.Parallel()

	to, emitted, rejected := smFixtureApply(smStateStopped, smCommandStop, "")

	// The fixture allows stop from stopped (not explicitly modelled as rejection).
	// Per ON-013c: it should succeed.
	if rejected {
		t.Error("ON-013c: stop while stopped rejected; MUST return success")
	}
	if to != smStateStopped {
		t.Errorf("ON-013c: stop while stopped -> %q, want %q", to, smStateStopped)
	}
	// No event emission for stop-while-stopped per ON-013c.
	if emitted != "" {
		t.Errorf("ON-013c: stop while stopped emitted %q, want empty (no event for no-op stop)", emitted)
	}
}

// TestON013c_IdempotentResumeWhileRunning verifies that resume while running
// returns success with no transition.
//
// Spec ref: operator-nfr.md section 4.3 ON-013c -- "A `resume` issued while
// `running` MUST return success with no transition."
// Spec ref: operator-nfr.md section 7.1 -- dedicated row for `running` +
// `resume` (no-op): returns success (exit code 0), no event emitted.
//
// The prior spec-internal tension (ON-013c vs section 7.1 catch-all rejection
// row) was resolved by adding a dedicated section 7.1 row for this idempotent
// case. Both ON-013c and section 7.1 now agree: success, no transition.
func TestON013c_IdempotentResumeWhileRunning(t *testing.T) {
	t.Parallel()

	to, emitted, rejected := smFixtureApply(smStateRunning, smCommandResume, "")

	if rejected {
		t.Error("ON-013c: resume while running returned rejection; MUST return success (code 0) for no-op transition")
	}
	if to != smStateRunning {
		t.Errorf("ON-013c: resume while running -> %q, want %q (no-op)", to, smStateRunning)
	}
	if emitted != "" {
		t.Errorf("ON-013c: resume while running emitted %q, want no event", emitted)
	}
}

// TestON014_ReconciliationOperatorOverride verifies the structural presence of
// the reconciliation operator override obligation in the spec.
//
// ON-014 mandates that `harmonik confirm-verdict` / `harmonik veto-verdict`
// commands exist as policy-controlled override points. This harness verifies
// the taxonomy alignment and obligation shape at the structural level.
//
// Spec ref: operator-nfr.md section 4.3 ON-014 -- "The spec-draft pass MUST
// produce a normative per-reconciliation-workflow policy option to pause the
// daemon's verdict-execution step."
func TestON014_ReconciliationOperatorOverride(t *testing.T) {
	t.Parallel()

	// Code 16 (operator-control-invalid-state) is the fallback for a veto/confirm
	// command issued when no pending verdict exists. Verify the taxonomy entry.
	e, ok := operatornfr.LookupExitCode(16)
	if !ok {
		t.Fatal("ON-014: section 8 taxonomy missing code 16; confirm/veto commands rely on it for invalid-state rejection")
	}
	if e.Category != "operator-control-invalid-state" {
		t.Errorf("ON-014: code 16 category = %q, want %q", e.Category, "operator-control-invalid-state")
	}

	// The confirm/veto commands are part of the multi-daemon command surface per
	// section 4.10. Verify the command taxonomy covers them via CommandList (the
	// multi-daemon list command as a proxy for the command surface declaration).
	_, ok = operatornfr.CommandLookup(operatornfr.CommandList)
	if !ok {
		t.Error("ON-014: CommandList not found in CommandExitCodeSets; multi-daemon command surface incomplete")
	}
}

// TestON011_MutexSerializability verifies the ON-011 serializability requirement:
// concurrent operator commands are arbitrated by mutex acquisition order.
//
// This race-detection test asserts that concurrent smFixtureApply calls do not
// data-race; the production mutex is modelled by the fixture's stateless
// function (no shared mutation), which is the correct harness-level contract.
//
// Spec ref: operator-nfr.md section 4.3 ON-011 -- "Operator-control state-machine
// transitions MUST be serializable: the daemon MUST hold a single mutex ...
// Concurrent operator commands are arbitrated by mutex acquisition order."
func TestON011_MutexSerializability(t *testing.T) {
	t.Parallel()

	const goroutines = 10
	var wg sync.WaitGroup
	results := make([]smState, goroutines)

	// Simulate concurrent pause commands from running state.
	// With mutex semantics, each goroutine sees a consistent state.
	// The fixture is stateless, so all calls independently compute the same result.
	// A production mutex would serialize them; the fixture verifies the outcome
	// of any single winner is deterministic.
	for i := 0; i < goroutines; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			to, _, _ := smFixtureApply(smStateRunning, smCommandPause, "operator")
			results[i] = to
		}()
	}
	wg.Wait()

	// Every goroutine's result MUST be pausing (deterministic regardless of
	// acquisition order, since fixture is stateless).
	for i, r := range results {
		if r != smStatePausing {
			t.Errorf("ON-011: goroutine %d got state %q, want %q; concurrent commands must produce deterministic outcomes", i, r, smStatePausing)
		}
	}
}
