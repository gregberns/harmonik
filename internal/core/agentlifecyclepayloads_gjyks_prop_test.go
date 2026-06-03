package core

// Property tests for the eight Valid() methods in agentlifecyclepayloads_gjyks.go.
//
// Naming: TestProp_* per testing.md §Decisions #10.
// File:   *_prop_test.go per testing.md §Property layer.
//
// Each test uses rapid to draw valid values for all fields NOT under
// falsification, then verifies the expected Valid() result.
//
// Bead ref: hk-z02yj (part of hk-j3hrn core coverage uplift).

import (
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// AgentCompletedPayload
// ---------------------------------------------------------------------------

func TestProp_AgentCompletedPayload_Valid_AcceptsFullPayload(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentCompletedPayload{
			RunID:      RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:  SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			EndedAt:    rapid.StringN(1, 64, -1).Draw(rt, "ended_at"),
			OutcomeRef: rapid.StringN(1, 64, -1).Draw(rt, "outcome_ref"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for fully-populated AgentCompletedPayload, want true")
		}
	})
}

func TestProp_AgentCompletedPayload_Valid_RejectsNilRunID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentCompletedPayload{
			RunID:      RunID(uuid.Nil),
			SessionID:  SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			EndedAt:    rapid.StringN(1, 64, -1).Draw(rt, "ended_at"),
			OutcomeRef: rapid.StringN(1, 64, -1).Draw(rt, "outcome_ref"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with nil RunID, want false")
		}
	})
}

func TestProp_AgentCompletedPayload_Valid_RejectsEmptySessionID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentCompletedPayload{
			RunID:      RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:  "",
			EndedAt:    rapid.StringN(1, 64, -1).Draw(rt, "ended_at"),
			OutcomeRef: rapid.StringN(1, 64, -1).Draw(rt, "outcome_ref"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty SessionID, want false")
		}
	})
}

func TestProp_AgentCompletedPayload_Valid_RejectsEmptyEndedAt(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentCompletedPayload{
			RunID:      RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:  SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			EndedAt:    "",
			OutcomeRef: rapid.StringN(1, 64, -1).Draw(rt, "outcome_ref"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty EndedAt, want false")
		}
	})
}

func TestProp_AgentCompletedPayload_Valid_RejectsEmptyOutcomeRef(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentCompletedPayload{
			RunID:      RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:  SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			EndedAt:    rapid.StringN(1, 64, -1).Draw(rt, "ended_at"),
			OutcomeRef: "",
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty OutcomeRef, want false")
		}
	})
}

// ---------------------------------------------------------------------------
// AgentHeartbeatPayload
// ---------------------------------------------------------------------------

func TestProp_AgentHeartbeatPayload_Valid_AcceptsFullPayload(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentHeartbeatPayload{
			SessionID: SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			Phase:     rapid.StringN(1, 64, -1).Draw(rt, "phase"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for fully-populated AgentHeartbeatPayload, want true")
		}
	})
}

func TestProp_AgentHeartbeatPayload_Valid_RejectsEmptySessionID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentHeartbeatPayload{
			SessionID: "",
			Phase:     rapid.StringN(1, 64, -1).Draw(rt, "phase"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty SessionID, want false")
		}
	})
}

func TestProp_AgentHeartbeatPayload_Valid_RejectsEmptyPhase(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentHeartbeatPayload{
			SessionID: SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			Phase:     "",
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty Phase, want false")
		}
	})
}

// ---------------------------------------------------------------------------
// AgentWarningSilentHangPayload
// ---------------------------------------------------------------------------

func TestProp_AgentWarningSilentHangPayload_Valid_AcceptsFullPayload(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentWarningSilentHangPayload{
			RunID:               RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:           SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ThresholdSeconds:    rapid.IntRange(1, 3600).Draw(rt, "threshold"),
			LastProgressEventAt: rapid.StringN(1, 64, -1).Draw(rt, "last_progress"),
			FSMState:            rapid.StringN(1, 64, -1).Draw(rt, "fsm_state"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for fully-populated AgentWarningSilentHangPayload, want true")
		}
	})
}

func TestProp_AgentWarningSilentHangPayload_Valid_RejectsNilRunID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentWarningSilentHangPayload{
			RunID:               RunID(uuid.Nil),
			SessionID:           SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ThresholdSeconds:    rapid.IntRange(1, 3600).Draw(rt, "threshold"),
			LastProgressEventAt: rapid.StringN(1, 64, -1).Draw(rt, "last_progress"),
			FSMState:            rapid.StringN(1, 64, -1).Draw(rt, "fsm_state"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with nil RunID, want false")
		}
	})
}

func TestProp_AgentWarningSilentHangPayload_Valid_RejectsZeroThreshold(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentWarningSilentHangPayload{
			RunID:               RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:           SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ThresholdSeconds:    rapid.IntRange(-100, 0).Draw(rt, "threshold"),
			LastProgressEventAt: rapid.StringN(1, 64, -1).Draw(rt, "last_progress"),
			FSMState:            rapid.StringN(1, 64, -1).Draw(rt, "fsm_state"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with ThresholdSeconds <= 0, want false")
		}
	})
}

func TestProp_AgentWarningSilentHangPayload_Valid_RejectsEmptyLastProgressEventAt(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentWarningSilentHangPayload{
			RunID:               RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:           SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ThresholdSeconds:    rapid.IntRange(1, 3600).Draw(rt, "threshold"),
			LastProgressEventAt: "",
			FSMState:            rapid.StringN(1, 64, -1).Draw(rt, "fsm_state"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty LastProgressEventAt, want false")
		}
	})
}

func TestProp_AgentWarningSilentHangPayload_Valid_RejectsEmptyFSMState(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentWarningSilentHangPayload{
			RunID:               RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:           SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ThresholdSeconds:    rapid.IntRange(1, 3600).Draw(rt, "threshold"),
			LastProgressEventAt: rapid.StringN(1, 64, -1).Draw(rt, "last_progress"),
			FSMState:            "",
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty FSMState, want false")
		}
	})
}

// ---------------------------------------------------------------------------
// AgentResumedAfterWarningPayload
// ---------------------------------------------------------------------------

func TestProp_AgentResumedAfterWarningPayload_Valid_AcceptsFullPayload(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentResumedAfterWarningPayload{
			RunID:                  RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:              SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ResumedAt:              rapid.StringN(1, 64, -1).Draw(rt, "resumed_at"),
			WarningDurationSeconds: rapid.IntRange(0, 3600).Draw(rt, "warning_duration"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for fully-populated AgentResumedAfterWarningPayload, want true")
		}
	})
}

func TestProp_AgentResumedAfterWarningPayload_Valid_RejectsNilRunID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentResumedAfterWarningPayload{
			RunID:                  RunID(uuid.Nil),
			SessionID:              SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ResumedAt:              rapid.StringN(1, 64, -1).Draw(rt, "resumed_at"),
			WarningDurationSeconds: rapid.IntRange(0, 3600).Draw(rt, "warning_duration"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with nil RunID, want false")
		}
	})
}

func TestProp_AgentResumedAfterWarningPayload_Valid_RejectsEmptyResumedAt(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentResumedAfterWarningPayload{
			RunID:                  RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:              SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ResumedAt:              "",
			WarningDurationSeconds: rapid.IntRange(0, 3600).Draw(rt, "warning_duration"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty ResumedAt, want false")
		}
	})
}

func TestProp_AgentResumedAfterWarningPayload_Valid_RejectsNegativeWarningDuration(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentResumedAfterWarningPayload{
			RunID:                  RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:              SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ResumedAt:              rapid.StringN(1, 64, -1).Draw(rt, "resumed_at"),
			WarningDurationSeconds: rapid.IntRange(-1000, -1).Draw(rt, "warning_duration"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with negative WarningDurationSeconds, want false")
		}
	})
}

// ---------------------------------------------------------------------------
// AgentSoftTerminatingPayload
// ---------------------------------------------------------------------------

func TestProp_AgentSoftTerminatingPayload_Valid_AcceptsFullPayload(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentSoftTerminatingPayload{
			RunID:            RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:        SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ThresholdSeconds: rapid.IntRange(1, 3600).Draw(rt, "threshold"),
			StartedAt:        rapid.StringN(1, 64, -1).Draw(rt, "started_at"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for fully-populated AgentSoftTerminatingPayload, want true")
		}
	})
}

func TestProp_AgentSoftTerminatingPayload_Valid_RejectsNilRunID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentSoftTerminatingPayload{
			RunID:            RunID(uuid.Nil),
			SessionID:        SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ThresholdSeconds: rapid.IntRange(1, 3600).Draw(rt, "threshold"),
			StartedAt:        rapid.StringN(1, 64, -1).Draw(rt, "started_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with nil RunID, want false")
		}
	})
}

func TestProp_AgentSoftTerminatingPayload_Valid_RejectsZeroThreshold(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentSoftTerminatingPayload{
			RunID:            RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:        SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ThresholdSeconds: rapid.IntRange(-100, 0).Draw(rt, "threshold"),
			StartedAt:        rapid.StringN(1, 64, -1).Draw(rt, "started_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with ThresholdSeconds <= 0, want false")
		}
	})
}

func TestProp_AgentSoftTerminatingPayload_Valid_RejectsEmptyStartedAt(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentSoftTerminatingPayload{
			RunID:            RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:        SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ThresholdSeconds: rapid.IntRange(1, 3600).Draw(rt, "threshold"),
			StartedAt:        "",
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty StartedAt, want false")
		}
	})
}

// ---------------------------------------------------------------------------
// AgentHardTerminatingPayload
// ---------------------------------------------------------------------------

func TestProp_AgentHardTerminatingPayload_Valid_AcceptsFullPayload(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentHardTerminatingPayload{
			RunID:            RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:        SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ThresholdSeconds: rapid.IntRange(1, 3600).Draw(rt, "threshold"),
			StartedAt:        rapid.StringN(1, 64, -1).Draw(rt, "started_at"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for fully-populated AgentHardTerminatingPayload, want true")
		}
	})
}

func TestProp_AgentHardTerminatingPayload_Valid_RejectsNilRunID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentHardTerminatingPayload{
			RunID:            RunID(uuid.Nil),
			SessionID:        SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ThresholdSeconds: rapid.IntRange(1, 3600).Draw(rt, "threshold"),
			StartedAt:        rapid.StringN(1, 64, -1).Draw(rt, "started_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with nil RunID, want false")
		}
	})
}

func TestProp_AgentHardTerminatingPayload_Valid_RejectsZeroThreshold(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentHardTerminatingPayload{
			RunID:            RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:        SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ThresholdSeconds: rapid.IntRange(-100, 0).Draw(rt, "threshold"),
			StartedAt:        rapid.StringN(1, 64, -1).Draw(rt, "started_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with ThresholdSeconds <= 0, want false")
		}
	})
}

func TestProp_AgentHardTerminatingPayload_Valid_RejectsEmptyStartedAt(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := AgentHardTerminatingPayload{
			RunID:            RunID(drawNonNilUUID(rt, "run_id")),
			SessionID:        SessionID(rapid.StringN(1, 64, -1).Draw(rt, "session_id")),
			ThresholdSeconds: rapid.IntRange(1, 3600).Draw(rt, "threshold"),
			StartedAt:        "",
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty StartedAt, want false")
		}
	})
}

// ---------------------------------------------------------------------------
// BeadClosedPayload
// ---------------------------------------------------------------------------

func TestProp_BeadClosedPayload_Valid_AcceptsFullPayload(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := BeadClosedPayload{
			RunID:  RunID(drawNonNilUUID(rt, "run_id")),
			BeadID: BeadID(rapid.StringN(1, 64, -1).Draw(rt, "bead_id")),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for fully-populated BeadClosedPayload, want true")
		}
	})
}

func TestProp_BeadClosedPayload_Valid_RejectsNilRunID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := BeadClosedPayload{
			RunID:  RunID(uuid.Nil),
			BeadID: BeadID(rapid.StringN(1, 64, -1).Draw(rt, "bead_id")),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with nil RunID, want false")
		}
	})
}

func TestProp_BeadClosedPayload_Valid_RejectsEmptyBeadID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := BeadClosedPayload{
			RunID:  RunID(drawNonNilUUID(rt, "run_id")),
			BeadID: "",
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty BeadID, want false")
		}
	})
}

// ---------------------------------------------------------------------------
// WorkingTreeRefreshFailedPayload
// ---------------------------------------------------------------------------

func TestProp_WorkingTreeRefreshFailedPayload_Valid_AcceptsFullPayload(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkingTreeRefreshFailedPayload{
			RunID:  RunID(drawNonNilUUID(rt, "run_id")),
			BeadID: BeadID(rapid.StringN(1, 64, -1).Draw(rt, "bead_id")),
			Error:  rapid.StringN(1, 128, -1).Draw(rt, "error"),
		}
		if !p.Valid() {
			rt.Errorf("Valid() = false for fully-populated WorkingTreeRefreshFailedPayload, want true")
		}
	})
}

func TestProp_WorkingTreeRefreshFailedPayload_Valid_RejectsNilRunID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkingTreeRefreshFailedPayload{
			RunID:  RunID(uuid.Nil),
			BeadID: BeadID(rapid.StringN(1, 64, -1).Draw(rt, "bead_id")),
			Error:  rapid.StringN(1, 128, -1).Draw(rt, "error"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with nil RunID, want false")
		}
	})
}

func TestProp_WorkingTreeRefreshFailedPayload_Valid_RejectsEmptyBeadID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkingTreeRefreshFailedPayload{
			RunID:  RunID(drawNonNilUUID(rt, "run_id")),
			BeadID: "",
			Error:  rapid.StringN(1, 128, -1).Draw(rt, "error"),
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty BeadID, want false")
		}
	})
}

func TestProp_WorkingTreeRefreshFailedPayload_Valid_RejectsEmptyError(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := WorkingTreeRefreshFailedPayload{
			RunID:  RunID(drawNonNilUUID(rt, "run_id")),
			BeadID: BeadID(rapid.StringN(1, 64, -1).Draw(rt, "bead_id")),
			Error:  "",
		}
		if p.Valid() {
			rt.Errorf("Valid() = true with empty Error, want false")
		}
	})
}
