package core

// daemonevents_coverage_hkj3hrn_test.go — targeted coverage uplift for the
// Valid() methods on §8.7 daemon-event payload types.
//
// Bead: hk-j3hrn (core coverage uplift EPIC, step a).
// Spec: specs/event-model.md §8.7; §6.3.
//
// Each sub-table covers: a valid (all fields set) case, a zero-value (all-empty)
// case, and one case per required field that triggers return false.

import (
	"testing"

	"github.com/google/uuid"
)

// ─── ShutdownMode.Valid ───────────────────────────────────────────────────────

func TestShutdownMode_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		mode  ShutdownMode
		valid bool
	}{
		{ShutdownModeGraceful, true},
		{ShutdownModeImmediate, true},
		{"", false},
		{"unknown", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.mode), func(t *testing.T) {
			t.Parallel()
			if got := tc.mode.Valid(); got != tc.valid {
				t.Errorf("ShutdownMode(%q).Valid() = %v, want %v", tc.mode, got, tc.valid)
			}
		})
	}
}

// ─── OperatorPauseStatusValue.Valid ──────────────────────────────────────────

func TestOperatorPauseStatusValue_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		v     OperatorPauseStatusValue
		valid bool
	}{
		{OperatorPauseStatusValuePausing, true},
		{OperatorPauseStatusValuePaused, true},
		{"", false},
		{"running", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.v), func(t *testing.T) {
			t.Parallel()
			if got := tc.v.Valid(); got != tc.valid {
				t.Errorf("OperatorPauseStatusValue(%q).Valid() = %v, want %v", tc.v, got, tc.valid)
			}
		})
	}
}

// ─── DaemonStartedPayload.Valid ───────────────────────────────────────────────

func TestDaemonStartedPayload_Valid(t *testing.T) {
	t.Parallel()

	validPID := 1234
	validHash := "abc123def456"
	validTs := "2026-05-09T12:00:00Z"

	cases := []struct {
		name  string
		p     DaemonStartedPayload
		valid bool
	}{
		{"valid", DaemonStartedPayload{StartedAt: validTs, PID: validPID, BinaryCommitHash: validHash}, true},
		{"zero-value", DaemonStartedPayload{}, false},
		{"missing-started-at", DaemonStartedPayload{PID: validPID, BinaryCommitHash: validHash}, false},
		{"pid-zero", DaemonStartedPayload{StartedAt: validTs, PID: 0, BinaryCommitHash: validHash}, false},
		{"pid-negative", DaemonStartedPayload{StartedAt: validTs, PID: -1, BinaryCommitHash: validHash}, false},
		{"missing-commit-hash", DaemonStartedPayload{StartedAt: validTs, PID: validPID}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("DaemonStartedPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── DaemonReadyPayload.Valid ─────────────────────────────────────────────────

func TestDaemonReadyPayload_Valid(t *testing.T) {
	t.Parallel()

	validID := RunID(uuid.MustParse("0196a1b2-c3d4-7000-8a1b-000000000003"))
	nilID := RunID(uuid.Nil)

	cases := []struct {
		name  string
		p     DaemonReadyPayload
		valid bool
	}{
		{"valid-no-investigators", DaemonReadyPayload{ReadyAt: "2026-05-09T12:00:00Z", ReadyAtNsSinceBoot: 100}, true},
		{"valid-with-investigators", DaemonReadyPayload{ReadyAt: "2026-05-09T12:00:00Z", ReadyAtNsSinceBoot: 100, InvestigatorRunIDs: []RunID{validID}}, true},
		{"zero-value", DaemonReadyPayload{}, false},
		{"missing-ready-at", DaemonReadyPayload{ReadyAtNsSinceBoot: 100}, false},
		{"zero-ns", DaemonReadyPayload{ReadyAt: "2026-05-09T12:00:00Z", ReadyAtNsSinceBoot: 0}, false},
		{"nil-run-id-in-investigators", DaemonReadyPayload{ReadyAt: "2026-05-09T12:00:00Z", ReadyAtNsSinceBoot: 100, InvestigatorRunIDs: []RunID{nilID}}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("DaemonReadyPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── DaemonShutdownPayload.Valid ──────────────────────────────────────────────

func TestDaemonShutdownPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     DaemonShutdownPayload
		valid bool
	}{
		{"valid-graceful", DaemonShutdownPayload{ShutdownAt: "2026-05-09T12:00:00Z", ShutdownAtNsSinceBoot: 200, Mode: ShutdownModeGraceful}, true},
		{"valid-immediate", DaemonShutdownPayload{ShutdownAt: "2026-05-09T12:00:00Z", ShutdownAtNsSinceBoot: 200, Mode: ShutdownModeImmediate}, true},
		{"zero-value", DaemonShutdownPayload{}, false},
		{"missing-shutdown-at", DaemonShutdownPayload{ShutdownAtNsSinceBoot: 200, Mode: ShutdownModeGraceful}, false},
		{"zero-ns", DaemonShutdownPayload{ShutdownAt: "2026-05-09T12:00:00Z", Mode: ShutdownModeGraceful}, false},
		{"invalid-mode", DaemonShutdownPayload{ShutdownAt: "2026-05-09T12:00:00Z", ShutdownAtNsSinceBoot: 200, Mode: "unknown"}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("DaemonShutdownPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── DaemonStartupFailedPayload.Valid ────────────────────────────────────────

func TestDaemonStartupFailedPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     DaemonStartupFailedPayload
		valid bool
	}{
		{"valid", DaemonStartupFailedPayload{FailedAt: "2026-05-09T12:00:00Z", FailureMode: "queue-format-unsupported"}, true},
		{"zero-value", DaemonStartupFailedPayload{}, false},
		{"missing-failed-at", DaemonStartupFailedPayload{FailureMode: "queue-format-unsupported"}, false},
		{"missing-failure-mode", DaemonStartupFailedPayload{FailedAt: "2026-05-09T12:00:00Z"}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("DaemonStartupFailedPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── DaemonDegradedPayload.Valid ──────────────────────────────────────────────

func TestDaemonDegradedPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     DaemonDegradedPayload
		valid bool
	}{
		{"valid", DaemonDegradedPayload{DetectedAt: "2026-05-09T12:00:00Z", Reason: DaemonDegradedReasonRTOBreach}, true},
		{"zero-value", DaemonDegradedPayload{}, false},
		{"missing-detected-at", DaemonDegradedPayload{Reason: DaemonDegradedReasonRTOBreach}, false},
		{"invalid-reason", DaemonDegradedPayload{DetectedAt: "2026-05-09T12:00:00Z", Reason: "unknown_reason"}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("DaemonDegradedPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── OperatorPauseStatusPayload.Valid ────────────────────────────────────────

func TestOperatorPauseStatusPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     OperatorPauseStatusPayload
		valid bool
	}{
		{"valid-pausing", OperatorPauseStatusPayload{Status: OperatorPauseStatusValuePausing, ChangedAt: "2026-05-09T12:00:00.000Z"}, true},
		{"valid-paused", OperatorPauseStatusPayload{Status: OperatorPauseStatusValuePaused, ChangedAt: "2026-05-09T12:00:00.000Z"}, true},
		{"zero-value", OperatorPauseStatusPayload{}, false},
		{"invalid-status", OperatorPauseStatusPayload{Status: "running", ChangedAt: "2026-05-09T12:00:00.000Z"}, false},
		{"missing-changed-at", OperatorPauseStatusPayload{Status: OperatorPauseStatusValuePaused}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("OperatorPauseStatusPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── OperatorResumingPayload.Valid ────────────────────────────────────────────

func TestOperatorResumingPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     OperatorResumingPayload
		valid bool
	}{
		{"valid", OperatorResumingPayload{ResumedAt: "2026-05-09T12:00:00Z"}, true},
		{"zero-value", OperatorResumingPayload{}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("OperatorResumingPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── OperatorStoppedPayload.Valid ─────────────────────────────────────────────

func TestOperatorStoppedPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     OperatorStoppedPayload
		valid bool
	}{
		{"valid", OperatorStoppedPayload{StoppedAt: "2026-05-09T12:00:00Z", Mode: ShutdownModeGraceful}, true},
		{"zero-value", OperatorStoppedPayload{}, false},
		{"missing-stopped-at", OperatorStoppedPayload{Mode: ShutdownModeGraceful}, false},
		{"invalid-mode", OperatorStoppedPayload{StoppedAt: "2026-05-09T12:00:00Z", Mode: "unknown"}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("OperatorStoppedPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── OperatorUpgradingPayload.Valid ───────────────────────────────────────────

func TestOperatorUpgradingPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     OperatorUpgradingPayload
		valid bool
	}{
		{"valid", OperatorUpgradingPayload{UpgradeVersion: "v1.2.3", StartedAt: "2026-05-09T12:00:00Z"}, true},
		{"zero-value", OperatorUpgradingPayload{}, false},
		{"missing-version", OperatorUpgradingPayload{StartedAt: "2026-05-09T12:00:00Z"}, false},
		{"missing-started-at", OperatorUpgradingPayload{UpgradeVersion: "v1.2.3"}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("OperatorUpgradingPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── OperatorUpgradeCompletedPayload.Valid ────────────────────────────────────

func TestOperatorUpgradeCompletedPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     OperatorUpgradeCompletedPayload
		valid bool
	}{
		{"valid", OperatorUpgradeCompletedPayload{UpgradeVersion: "v1.2.3", CompletedAt: "2026-05-09T12:00:00Z", BinaryCommitHash: "abc123"}, true},
		{"zero-value", OperatorUpgradeCompletedPayload{}, false},
		{"missing-version", OperatorUpgradeCompletedPayload{CompletedAt: "2026-05-09T12:00:00Z", BinaryCommitHash: "abc123"}, false},
		{"missing-completed-at", OperatorUpgradeCompletedPayload{UpgradeVersion: "v1.2.3", BinaryCommitHash: "abc123"}, false},
		{"missing-commit-hash", OperatorUpgradeCompletedPayload{UpgradeVersion: "v1.2.3", CompletedAt: "2026-05-09T12:00:00Z"}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("OperatorUpgradeCompletedPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── OperatorUpgradeRejectedPayload.Valid ────────────────────────────────────

func TestOperatorUpgradeRejectedPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     OperatorUpgradeRejectedPayload
		valid bool
	}{
		{"valid", OperatorUpgradeRejectedPayload{RejectedAt: "2026-05-09T12:00:00Z", Reason: OperatorUpgradeRejectedReasonHashMismatch}, true},
		{"zero-value", OperatorUpgradeRejectedPayload{}, false},
		{"missing-rejected-at", OperatorUpgradeRejectedPayload{Reason: OperatorUpgradeRejectedReasonHashMismatch}, false},
		{"invalid-reason", OperatorUpgradeRejectedPayload{RejectedAt: "2026-05-09T12:00:00Z", Reason: "unknown_reason"}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("OperatorUpgradeRejectedPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── OperatorCommandRejectedPayload.Valid ────────────────────────────────────

func TestOperatorCommandRejectedPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     OperatorCommandRejectedPayload
		valid bool
	}{
		{"valid", OperatorCommandRejectedPayload{Command: OperatorCommandPause, CurrentState: DaemonStatusReady, RejectedAt: "2026-05-09T12:00:00Z"}, true},
		{"zero-value", OperatorCommandRejectedPayload{}, false},
		{"invalid-command", OperatorCommandRejectedPayload{Command: "unknown-cmd", CurrentState: DaemonStatusReady, RejectedAt: "2026-05-09T12:00:00Z"}, false},
		{"invalid-state", OperatorCommandRejectedPayload{Command: OperatorCommandPause, CurrentState: "unknown-state", RejectedAt: "2026-05-09T12:00:00Z"}, false},
		{"missing-rejected-at", OperatorCommandRejectedPayload{Command: OperatorCommandPause, CurrentState: DaemonStatusReady}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("OperatorCommandRejectedPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── DispatchDeferredPayload.Valid ────────────────────────────────────────────

func TestDispatchDeferredPayload_Valid(t *testing.T) {
	t.Parallel()

	validRunID := RunID(uuid.MustParse("0196a1b2-c3d4-7000-8a1b-000000000003"))
	nilRunID := RunID(uuid.Nil)
	validNodeID := NodeID("node-1")
	emptyNodeID := NodeID("")

	cases := []struct {
		name  string
		p     DispatchDeferredPayload
		valid bool
	}{
		{"valid-no-optionals", DispatchDeferredPayload{Reason: DispatchDeferredReasonMachineCeilingExhausted, DeferredAt: "2026-05-09T12:00:00Z"}, true},
		{"valid-with-run-and-node", DispatchDeferredPayload{RunID: &validRunID, NodeID: &validNodeID, Reason: DispatchDeferredReasonMachineCeilingExhausted, DeferredAt: "2026-05-09T12:00:00Z"}, true},
		{"zero-value", DispatchDeferredPayload{}, false},
		{"nil-run-id", DispatchDeferredPayload{RunID: &nilRunID, Reason: DispatchDeferredReasonMachineCeilingExhausted, DeferredAt: "2026-05-09T12:00:00Z"}, false},
		{"empty-node-id", DispatchDeferredPayload{NodeID: &emptyNodeID, Reason: DispatchDeferredReasonMachineCeilingExhausted, DeferredAt: "2026-05-09T12:00:00Z"}, false},
		{"missing-reason", DispatchDeferredPayload{DeferredAt: "2026-05-09T12:00:00Z"}, false},
		{"missing-deferred-at", DispatchDeferredPayload{Reason: DispatchDeferredReasonMachineCeilingExhausted}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("DispatchDeferredPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── DaemonOrphanSweepCompletedPayload.Valid ──────────────────────────────────

func TestDaemonOrphanSweepCompletedPayload_Valid(t *testing.T) {
	t.Parallel()

	validP := DaemonOrphanSweepCompletedPayload{
		TmuxSessionsKilled:         0,
		TmuxWindowsKilled:          0,
		LocksCleared:               0,
		SubprocessesKilled:         0,
		BrSubprocessesKilled:       0,
		ReconciliationLocksRemoved: 0,
		StaleIntentsObserved:       0,
		BeadInProgressReset:        0,
		BeadCat3cClosed:            0,
		SweptAt:                    "2026-05-09T12:00:00Z",
	}

	cases := []struct {
		name  string
		p     DaemonOrphanSweepCompletedPayload
		valid bool
	}{
		{"valid-all-zeros", validP, true},
		{"zero-value-struct", DaemonOrphanSweepCompletedPayload{}, false}, // SweptAt empty → false
		{"negative-tmux-sessions", func() DaemonOrphanSweepCompletedPayload {
			p := validP
			p.TmuxSessionsKilled = -1
			return p
		}(), false},
		{"negative-tmux-windows", func() DaemonOrphanSweepCompletedPayload {
			p := validP
			p.TmuxWindowsKilled = -1
			return p
		}(), false},
		{"negative-locks-cleared", func() DaemonOrphanSweepCompletedPayload {
			p := validP
			p.LocksCleared = -1
			return p
		}(), false},
		{"negative-subprocesses", func() DaemonOrphanSweepCompletedPayload {
			p := validP
			p.SubprocessesKilled = -1
			return p
		}(), false},
		{"negative-br-subprocesses", func() DaemonOrphanSweepCompletedPayload {
			p := validP
			p.BrSubprocessesKilled = -1
			return p
		}(), false},
		{"negative-recon-locks", func() DaemonOrphanSweepCompletedPayload {
			p := validP
			p.ReconciliationLocksRemoved = -1
			return p
		}(), false},
		{"negative-stale-intents", func() DaemonOrphanSweepCompletedPayload {
			p := validP
			p.StaleIntentsObserved = -1
			return p
		}(), false},
		{"missing-swept-at", func() DaemonOrphanSweepCompletedPayload {
			p := validP
			p.SweptAt = ""
			return p
		}(), false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("DaemonOrphanSweepCompletedPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── InfrastructureUnavailablePayload.Valid ───────────────────────────────────

func TestInfrastructureUnavailablePayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     InfrastructureUnavailablePayload
		valid bool
	}{
		{"valid", InfrastructureUnavailablePayload{FailedPrerequisite: InfrastructurePrerequisiteBrMissing, DetailString: "br not found", RetryCount: 0}, true},
		{"zero-value", InfrastructureUnavailablePayload{}, false},
		{"invalid-prerequisite", InfrastructureUnavailablePayload{FailedPrerequisite: "unknown_prereq", DetailString: "detail", RetryCount: 0}, false},
		{"missing-detail", InfrastructureUnavailablePayload{FailedPrerequisite: InfrastructurePrerequisiteBrMissing, RetryCount: 0}, false},
		{"negative-retry-count", InfrastructureUnavailablePayload{FailedPrerequisite: InfrastructurePrerequisiteBrMissing, DetailString: "detail", RetryCount: -1}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("InfrastructureUnavailablePayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── OperatorCommandFailedPayload.Valid ──────────────────────────────────────

func TestOperatorCommandFailedPayload_Valid(t *testing.T) {
	t.Parallel()

	validRunID := RunID(uuid.MustParse("0196a1b2-c3d4-7000-8a1b-000000000003"))
	nilRunID := RunID(uuid.Nil)

	cases := []struct {
		name  string
		p     OperatorCommandFailedPayload
		valid bool
	}{
		{"valid-no-run-id", OperatorCommandFailedPayload{Command: OperatorCommandStop, FailureClass: FailureClassTransient, FailedAt: "2026-05-09T12:00:00Z"}, true},
		{"valid-with-run-id", OperatorCommandFailedPayload{Command: OperatorCommandStop, FailureClass: FailureClassTransient, RunID: &validRunID, FailedAt: "2026-05-09T12:00:00Z"}, true},
		{"zero-value", OperatorCommandFailedPayload{}, false},
		{"invalid-command", OperatorCommandFailedPayload{Command: "unknown-cmd", FailureClass: FailureClassTransient, FailedAt: "2026-05-09T12:00:00Z"}, false},
		{"invalid-failure-class", OperatorCommandFailedPayload{Command: OperatorCommandStop, FailureClass: "unknown", FailedAt: "2026-05-09T12:00:00Z"}, false},
		{"nil-run-id", OperatorCommandFailedPayload{Command: OperatorCommandStop, FailureClass: FailureClassTransient, RunID: &nilRunID, FailedAt: "2026-05-09T12:00:00Z"}, false},
		{"missing-failed-at", OperatorCommandFailedPayload{Command: OperatorCommandStop, FailureClass: FailureClassTransient}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("OperatorCommandFailedPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── DaemonConfigPayload.Valid ────────────────────────────────────────────────

func TestDaemonConfigPayload_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		p     DaemonConfigPayload
		valid bool
	}{
		{"valid-minimal", DaemonConfigPayload{TargetBranch: "main"}, true},
		{"valid-with-protect", DaemonConfigPayload{TargetBranch: "release", ProtectBranches: []string{"main"}, ForbidUnprotectedDefault: true}, true},
		{"zero-value", DaemonConfigPayload{}, false},
		{"empty-target-branch", DaemonConfigPayload{TargetBranch: ""}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("DaemonConfigPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}

// ─── OperatorEscalationClearedPayload.Valid ──────────────────────────────────

func TestOperatorEscalationClearedPayload_Valid(t *testing.T) {
	t.Parallel()

	validRunID := RunID(uuid.MustParse("0196a1b2-c3d4-7000-8a1b-000000000003"))
	nilRunID := RunID(uuid.Nil)

	cases := []struct {
		name  string
		p     OperatorEscalationClearedPayload
		valid bool
	}{
		{"valid-no-run-id", OperatorEscalationClearedPayload{ClearedAt: "2026-05-09T12:00:00Z", ClearanceReason: ClearanceReasonVerdictExecuted}, true},
		{"valid-with-run-id", OperatorEscalationClearedPayload{TargetRunID: &validRunID, ClearedAt: "2026-05-09T12:00:00Z", ClearanceReason: ClearanceReasonManualClear}, true},
		{"zero-value", OperatorEscalationClearedPayload{}, false},
		{"nil-run-id", OperatorEscalationClearedPayload{TargetRunID: &nilRunID, ClearedAt: "2026-05-09T12:00:00Z", ClearanceReason: ClearanceReasonManualClear}, false},
		{"missing-cleared-at", OperatorEscalationClearedPayload{ClearanceReason: ClearanceReasonManualClear}, false},
		{"invalid-clearance-reason", OperatorEscalationClearedPayload{ClearedAt: "2026-05-09T12:00:00Z", ClearanceReason: "unknown_reason"}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("OperatorEscalationClearedPayload.Valid() = %v, want %v (case %s)", got, tc.valid, tc.name)
			}
		})
	}
}
