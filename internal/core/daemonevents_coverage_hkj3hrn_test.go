package core_test

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

	"github.com/gregberns/harmonik/internal/core"
)

// ─── ShutdownMode.Valid ───────────────────────────────────────────────────────

func TestShutdownMode_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		mode  core.ShutdownMode
		valid bool
	}{
		{core.ShutdownModeGraceful, true},
		{core.ShutdownModeImmediate, true},
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
		v     core.OperatorPauseStatusValue
		valid bool
	}{
		{core.OperatorPauseStatusValuePausing, true},
		{core.OperatorPauseStatusValuePaused, true},
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
		p     core.DaemonStartedPayload
		valid bool
	}{
		{"valid", core.DaemonStartedPayload{StartedAt: validTs, PID: validPID, BinaryCommitHash: validHash}, true},
		{"zero-value", core.DaemonStartedPayload{}, false},
		{"missing-started-at", core.DaemonStartedPayload{PID: validPID, BinaryCommitHash: validHash}, false},
		{"pid-zero", core.DaemonStartedPayload{StartedAt: validTs, PID: 0, BinaryCommitHash: validHash}, false},
		{"pid-negative", core.DaemonStartedPayload{StartedAt: validTs, PID: -1, BinaryCommitHash: validHash}, false},
		{"missing-commit-hash", core.DaemonStartedPayload{StartedAt: validTs, PID: validPID}, false},
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

	validID := core.RunID(uuid.MustParse("0196a1b2-c3d4-7000-8a1b-000000000003"))
	nilID := core.RunID(uuid.Nil)

	cases := []struct {
		name  string
		p     core.DaemonReadyPayload
		valid bool
	}{
		{"valid-no-investigators", core.DaemonReadyPayload{ReadyAt: "2026-05-09T12:00:00Z", ReadyAtNsSinceBoot: 100}, true},
		{"valid-with-investigators", core.DaemonReadyPayload{ReadyAt: "2026-05-09T12:00:00Z", ReadyAtNsSinceBoot: 100, InvestigatorRunIDs: []core.RunID{validID}}, true},
		{"zero-value", core.DaemonReadyPayload{}, false},
		{"missing-ready-at", core.DaemonReadyPayload{ReadyAtNsSinceBoot: 100}, false},
		{"zero-ns", core.DaemonReadyPayload{ReadyAt: "2026-05-09T12:00:00Z", ReadyAtNsSinceBoot: 0}, false},
		{"nil-run-id-in-investigators", core.DaemonReadyPayload{ReadyAt: "2026-05-09T12:00:00Z", ReadyAtNsSinceBoot: 100, InvestigatorRunIDs: []core.RunID{nilID}}, false},
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
		p     core.DaemonShutdownPayload
		valid bool
	}{
		{"valid-graceful", core.DaemonShutdownPayload{ShutdownAt: "2026-05-09T12:00:00Z", ShutdownAtNsSinceBoot: 200, Mode: core.ShutdownModeGraceful}, true},
		{"valid-immediate", core.DaemonShutdownPayload{ShutdownAt: "2026-05-09T12:00:00Z", ShutdownAtNsSinceBoot: 200, Mode: core.ShutdownModeImmediate}, true},
		{"zero-value", core.DaemonShutdownPayload{}, false},
		{"missing-shutdown-at", core.DaemonShutdownPayload{ShutdownAtNsSinceBoot: 200, Mode: core.ShutdownModeGraceful}, false},
		{"zero-ns", core.DaemonShutdownPayload{ShutdownAt: "2026-05-09T12:00:00Z", Mode: core.ShutdownModeGraceful}, false},
		{"invalid-mode", core.DaemonShutdownPayload{ShutdownAt: "2026-05-09T12:00:00Z", ShutdownAtNsSinceBoot: 200, Mode: "unknown"}, false},
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
		p     core.DaemonStartupFailedPayload
		valid bool
	}{
		{"valid", core.DaemonStartupFailedPayload{FailedAt: "2026-05-09T12:00:00Z", FailureMode: "queue-format-unsupported"}, true},
		{"zero-value", core.DaemonStartupFailedPayload{}, false},
		{"missing-failed-at", core.DaemonStartupFailedPayload{FailureMode: "queue-format-unsupported"}, false},
		{"missing-failure-mode", core.DaemonStartupFailedPayload{FailedAt: "2026-05-09T12:00:00Z"}, false},
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
		p     core.DaemonDegradedPayload
		valid bool
	}{
		{"valid", core.DaemonDegradedPayload{DetectedAt: "2026-05-09T12:00:00Z", Reason: core.DaemonDegradedReasonRTOBreach}, true},
		{"zero-value", core.DaemonDegradedPayload{}, false},
		{"missing-detected-at", core.DaemonDegradedPayload{Reason: core.DaemonDegradedReasonRTOBreach}, false},
		{"invalid-reason", core.DaemonDegradedPayload{DetectedAt: "2026-05-09T12:00:00Z", Reason: "unknown_reason"}, false},
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
		p     core.OperatorPauseStatusPayload
		valid bool
	}{
		{"valid-pausing", core.OperatorPauseStatusPayload{Status: core.OperatorPauseStatusValuePausing, ChangedAt: "2026-05-09T12:00:00.000Z"}, true},
		{"valid-paused", core.OperatorPauseStatusPayload{Status: core.OperatorPauseStatusValuePaused, ChangedAt: "2026-05-09T12:00:00.000Z"}, true},
		{"zero-value", core.OperatorPauseStatusPayload{}, false},
		{"invalid-status", core.OperatorPauseStatusPayload{Status: "running", ChangedAt: "2026-05-09T12:00:00.000Z"}, false},
		{"missing-changed-at", core.OperatorPauseStatusPayload{Status: core.OperatorPauseStatusValuePaused}, false},
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
		p     core.OperatorResumingPayload
		valid bool
	}{
		{"valid", core.OperatorResumingPayload{ResumedAt: "2026-05-09T12:00:00Z"}, true},
		{"zero-value", core.OperatorResumingPayload{}, false},
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
		p     core.OperatorStoppedPayload
		valid bool
	}{
		{"valid", core.OperatorStoppedPayload{StoppedAt: "2026-05-09T12:00:00Z", Mode: core.ShutdownModeGraceful}, true},
		{"zero-value", core.OperatorStoppedPayload{}, false},
		{"missing-stopped-at", core.OperatorStoppedPayload{Mode: core.ShutdownModeGraceful}, false},
		{"invalid-mode", core.OperatorStoppedPayload{StoppedAt: "2026-05-09T12:00:00Z", Mode: "unknown"}, false},
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
		p     core.OperatorUpgradingPayload
		valid bool
	}{
		{"valid", core.OperatorUpgradingPayload{UpgradeVersion: "v1.2.3", StartedAt: "2026-05-09T12:00:00Z"}, true},
		{"zero-value", core.OperatorUpgradingPayload{}, false},
		{"missing-version", core.OperatorUpgradingPayload{StartedAt: "2026-05-09T12:00:00Z"}, false},
		{"missing-started-at", core.OperatorUpgradingPayload{UpgradeVersion: "v1.2.3"}, false},
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
		p     core.OperatorUpgradeCompletedPayload
		valid bool
	}{
		{"valid", core.OperatorUpgradeCompletedPayload{UpgradeVersion: "v1.2.3", CompletedAt: "2026-05-09T12:00:00Z", BinaryCommitHash: "abc123"}, true},
		{"zero-value", core.OperatorUpgradeCompletedPayload{}, false},
		{"missing-version", core.OperatorUpgradeCompletedPayload{CompletedAt: "2026-05-09T12:00:00Z", BinaryCommitHash: "abc123"}, false},
		{"missing-completed-at", core.OperatorUpgradeCompletedPayload{UpgradeVersion: "v1.2.3", BinaryCommitHash: "abc123"}, false},
		{"missing-commit-hash", core.OperatorUpgradeCompletedPayload{UpgradeVersion: "v1.2.3", CompletedAt: "2026-05-09T12:00:00Z"}, false},
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
		p     core.OperatorUpgradeRejectedPayload
		valid bool
	}{
		{"valid", core.OperatorUpgradeRejectedPayload{RejectedAt: "2026-05-09T12:00:00Z", Reason: core.OperatorUpgradeRejectedReasonHashMismatch}, true},
		{"zero-value", core.OperatorUpgradeRejectedPayload{}, false},
		{"missing-rejected-at", core.OperatorUpgradeRejectedPayload{Reason: core.OperatorUpgradeRejectedReasonHashMismatch}, false},
		{"invalid-reason", core.OperatorUpgradeRejectedPayload{RejectedAt: "2026-05-09T12:00:00Z", Reason: "unknown_reason"}, false},
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
		p     core.OperatorCommandRejectedPayload
		valid bool
	}{
		{"valid", core.OperatorCommandRejectedPayload{Command: core.OperatorCommandPause, CurrentState: core.DaemonStatusReady, RejectedAt: "2026-05-09T12:00:00Z"}, true},
		{"zero-value", core.OperatorCommandRejectedPayload{}, false},
		{"invalid-command", core.OperatorCommandRejectedPayload{Command: "unknown-cmd", CurrentState: core.DaemonStatusReady, RejectedAt: "2026-05-09T12:00:00Z"}, false},
		{"invalid-state", core.OperatorCommandRejectedPayload{Command: core.OperatorCommandPause, CurrentState: "unknown-state", RejectedAt: "2026-05-09T12:00:00Z"}, false},
		{"missing-rejected-at", core.OperatorCommandRejectedPayload{Command: core.OperatorCommandPause, CurrentState: core.DaemonStatusReady}, false},
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

	validRunID := core.RunID(uuid.MustParse("0196a1b2-c3d4-7000-8a1b-000000000003"))
	nilRunID := core.RunID(uuid.Nil)
	validNodeID := core.NodeID("node-1")
	emptyNodeID := core.NodeID("")

	cases := []struct {
		name  string
		p     core.DispatchDeferredPayload
		valid bool
	}{
		{"valid-no-optionals", core.DispatchDeferredPayload{Reason: core.DispatchDeferredReasonMachineCeilingExhausted, DeferredAt: "2026-05-09T12:00:00Z"}, true},
		{"valid-with-run-and-node", core.DispatchDeferredPayload{RunID: &validRunID, NodeID: &validNodeID, Reason: core.DispatchDeferredReasonMachineCeilingExhausted, DeferredAt: "2026-05-09T12:00:00Z"}, true},
		{"zero-value", core.DispatchDeferredPayload{}, false},
		{"nil-run-id", core.DispatchDeferredPayload{RunID: &nilRunID, Reason: core.DispatchDeferredReasonMachineCeilingExhausted, DeferredAt: "2026-05-09T12:00:00Z"}, false},
		{"empty-node-id", core.DispatchDeferredPayload{NodeID: &emptyNodeID, Reason: core.DispatchDeferredReasonMachineCeilingExhausted, DeferredAt: "2026-05-09T12:00:00Z"}, false},
		{"missing-reason", core.DispatchDeferredPayload{DeferredAt: "2026-05-09T12:00:00Z"}, false},
		{"missing-deferred-at", core.DispatchDeferredPayload{Reason: core.DispatchDeferredReasonMachineCeilingExhausted}, false},
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

	validP := core.DaemonOrphanSweepCompletedPayload{
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
		p     core.DaemonOrphanSweepCompletedPayload
		valid bool
	}{
		{"valid-all-zeros", validP, true},
		{"zero-value-struct", core.DaemonOrphanSweepCompletedPayload{}, false}, // SweptAt empty → false
		{"negative-tmux-sessions", func() core.DaemonOrphanSweepCompletedPayload {
			p := validP
			p.TmuxSessionsKilled = -1
			return p
		}(), false},
		{"negative-tmux-windows", func() core.DaemonOrphanSweepCompletedPayload {
			p := validP
			p.TmuxWindowsKilled = -1
			return p
		}(), false},
		{"negative-locks-cleared", func() core.DaemonOrphanSweepCompletedPayload {
			p := validP
			p.LocksCleared = -1
			return p
		}(), false},
		{"negative-subprocesses", func() core.DaemonOrphanSweepCompletedPayload {
			p := validP
			p.SubprocessesKilled = -1
			return p
		}(), false},
		{"negative-br-subprocesses", func() core.DaemonOrphanSweepCompletedPayload {
			p := validP
			p.BrSubprocessesKilled = -1
			return p
		}(), false},
		{"negative-recon-locks", func() core.DaemonOrphanSweepCompletedPayload {
			p := validP
			p.ReconciliationLocksRemoved = -1
			return p
		}(), false},
		{"negative-stale-intents", func() core.DaemonOrphanSweepCompletedPayload {
			p := validP
			p.StaleIntentsObserved = -1
			return p
		}(), false},
		{"missing-swept-at", func() core.DaemonOrphanSweepCompletedPayload {
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
		p     core.InfrastructureUnavailablePayload
		valid bool
	}{
		{"valid", core.InfrastructureUnavailablePayload{FailedPrerequisite: core.InfrastructurePrerequisiteBrMissing, DetailString: "br not found", RetryCount: 0}, true},
		{"zero-value", core.InfrastructureUnavailablePayload{}, false},
		{"invalid-prerequisite", core.InfrastructureUnavailablePayload{FailedPrerequisite: "unknown_prereq", DetailString: "detail", RetryCount: 0}, false},
		{"missing-detail", core.InfrastructureUnavailablePayload{FailedPrerequisite: core.InfrastructurePrerequisiteBrMissing, RetryCount: 0}, false},
		{"negative-retry-count", core.InfrastructureUnavailablePayload{FailedPrerequisite: core.InfrastructurePrerequisiteBrMissing, DetailString: "detail", RetryCount: -1}, false},
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

	validRunID := core.RunID(uuid.MustParse("0196a1b2-c3d4-7000-8a1b-000000000003"))
	nilRunID := core.RunID(uuid.Nil)

	cases := []struct {
		name  string
		p     core.OperatorCommandFailedPayload
		valid bool
	}{
		{"valid-no-run-id", core.OperatorCommandFailedPayload{Command: core.OperatorCommandStop, FailureClass: core.FailureClassTransient, FailedAt: "2026-05-09T12:00:00Z"}, true},
		{"valid-with-run-id", core.OperatorCommandFailedPayload{Command: core.OperatorCommandStop, FailureClass: core.FailureClassTransient, RunID: &validRunID, FailedAt: "2026-05-09T12:00:00Z"}, true},
		{"zero-value", core.OperatorCommandFailedPayload{}, false},
		{"invalid-command", core.OperatorCommandFailedPayload{Command: "unknown-cmd", FailureClass: core.FailureClassTransient, FailedAt: "2026-05-09T12:00:00Z"}, false},
		{"invalid-failure-class", core.OperatorCommandFailedPayload{Command: core.OperatorCommandStop, FailureClass: "unknown", FailedAt: "2026-05-09T12:00:00Z"}, false},
		{"nil-run-id", core.OperatorCommandFailedPayload{Command: core.OperatorCommandStop, FailureClass: core.FailureClassTransient, RunID: &nilRunID, FailedAt: "2026-05-09T12:00:00Z"}, false},
		{"missing-failed-at", core.OperatorCommandFailedPayload{Command: core.OperatorCommandStop, FailureClass: core.FailureClassTransient}, false},
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
		p     core.DaemonConfigPayload
		valid bool
	}{
		{"valid-minimal", core.DaemonConfigPayload{TargetBranch: "main"}, true},
		{"valid-with-protect", core.DaemonConfigPayload{TargetBranch: "release", ProtectBranches: []string{"main"}, ForbidUnprotectedDefault: true}, true},
		{"zero-value", core.DaemonConfigPayload{}, false},
		{"empty-target-branch", core.DaemonConfigPayload{TargetBranch: ""}, false},
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

	validRunID := core.RunID(uuid.MustParse("0196a1b2-c3d4-7000-8a1b-000000000003"))
	nilRunID := core.RunID(uuid.Nil)

	cases := []struct {
		name  string
		p     core.OperatorEscalationClearedPayload
		valid bool
	}{
		{"valid-no-run-id", core.OperatorEscalationClearedPayload{ClearedAt: "2026-05-09T12:00:00Z", ClearanceReason: core.ClearanceReasonVerdictExecuted}, true},
		{"valid-with-run-id", core.OperatorEscalationClearedPayload{TargetRunID: &validRunID, ClearedAt: "2026-05-09T12:00:00Z", ClearanceReason: core.ClearanceReasonManualClear}, true},
		{"zero-value", core.OperatorEscalationClearedPayload{}, false},
		{"nil-run-id", core.OperatorEscalationClearedPayload{TargetRunID: &nilRunID, ClearedAt: "2026-05-09T12:00:00Z", ClearanceReason: core.ClearanceReasonManualClear}, false},
		{"missing-cleared-at", core.OperatorEscalationClearedPayload{ClearanceReason: core.ClearanceReasonManualClear}, false},
		{"invalid-clearance-reason", core.OperatorEscalationClearedPayload{ClearedAt: "2026-05-09T12:00:00Z", ClearanceReason: "unknown_reason"}, false},
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
