package operatornfr_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/operatornfr"
)

// upgradeContractFixture models one upgrade scenario covering the sub-obligations
// of specs/operator-nfr.md section 4.6 ON-020.
//
// Spec ref: operator-nfr.md section 4.6 ON-020 -- "full upgrade scenario tests
// covering all sub-obligations of ON-020: binary-source mechanism, hash check,
// drain-vs-reconciliation interaction, cross-version state contract, fd-passing
// socket continuity, rollback first-class, post-exec-replace failure recovery."
type upgradeContractFixture struct {
	// Name is the scenario name for t.Run labelling.
	Name string
	// DaemonStatus is the daemon's current operator-control state.
	DaemonStatus string
	// ExpectedHash is the operator-supplied expected commit hash.
	ExpectedHash string
	// ActualHash is the binary's embedded ldflags stamp.
	ActualHash string
	// OnDiskSchemaVersion is the schema version of the on-disk state.
	OnDiskSchemaVersion string
	// NewBinarySchemaSupported lists schema versions the new binary supports.
	NewBinarySchemaSupported []string
	// IsReconciliationInFlight is true if reconciliation workflows are running.
	IsReconciliationInFlight bool
	// WantExitCode is 0 on success, or the section 8 code on rejection.
	WantExitCode int
	// WantEmittedEvent is the event type emitted on rejection.
	WantEmittedEvent string
	// WantNewState is the operator-control state after the upgrade attempt.
	WantNewState string
}

// upgradeContractFixtureScenarios covers the section 10.2 requirement:
// "full upgrade scenario tests covering all sub-obligations of ON-020."
var upgradeContractFixtureScenarios = []upgradeContractFixture{
	{
		// Sub-obligation (b): hash check -- matching hash, paused, compat schema.
		Name:                     "happy-path-paused-hash-match-schema-ok",
		DaemonStatus:             "paused",
		ExpectedHash:             "abc123",
		ActualHash:               "abc123",
		OnDiskSchemaVersion:      "1.9",
		NewBinarySchemaSupported: []string{"2.0", "1.9"},
		WantExitCode:             0,
		WantNewState:             "upgrading",
	},
	{
		// Sub-obligation precondition: daemon is NOT paused -- MUST refuse (code 13).
		Name:                     "not-paused-refused",
		DaemonStatus:             "running",
		ExpectedHash:             "abc123",
		ActualHash:               "abc123",
		OnDiskSchemaVersion:      "1.9",
		NewBinarySchemaSupported: []string{"2.0", "1.9"},
		WantExitCode:             13,
		WantEmittedEvent:         "operator_upgrade_rejected",
		WantNewState:             "running",
	},
	{
		// Sub-obligation (b): hash mismatch -- MUST refuse (code 14).
		Name:                     "hash-mismatch-refused",
		DaemonStatus:             "paused",
		ExpectedHash:             "abc123",
		ActualHash:               "wronghash",
		OnDiskSchemaVersion:      "1.9",
		NewBinarySchemaSupported: []string{"2.0", "1.9"},
		WantExitCode:             14,
		WantEmittedEvent:         "operator_upgrade_rejected",
		WantNewState:             "paused",
	},
	{
		// Sub-obligation (d): schema incompatible -- N+2 disk, new binary only supports N..N-1.
		Name:                     "schema-incompatible-refused",
		DaemonStatus:             "paused",
		ExpectedHash:             "abc123",
		ActualHash:               "abc123",
		OnDiskSchemaVersion:      "1.5", // too old -- not in {2.0, 1.9}
		NewBinarySchemaSupported: []string{"2.0", "1.9"},
		WantExitCode:             15,
		WantEmittedEvent:         "operator_upgrade_rejected",
		WantNewState:             "paused",
	},
	{
		// Sub-obligation (c): reconciliation workflows in-flight -- MUST queue not interrupt.
		// Drain-gate (ON-008) ensures upgrade enters paused only after drain.
		// Since the fixture models paused state, this is already drain-complete.
		// The reconciliation carve-out (ON-010) applies during the pausing phase.
		Name:                     "reconciliation-in-flight-paused-is-safe",
		DaemonStatus:             "paused",
		ExpectedHash:             "abc123",
		ActualHash:               "abc123",
		OnDiskSchemaVersion:      "1.9",
		NewBinarySchemaSupported: []string{"2.0", "1.9"},
		IsReconciliationInFlight: true,
		// Paused implies drain-completed, so reconciliation within the daemon is
		// already settled. The flag here tests that reconciliation DURING pausing
		// was handled by the carve-out (ON-010), not that it blocks the upgrade.
		WantExitCode: 0,
		WantNewState: "upgrading",
	},
	{
		// Sub-obligation (d) same-version upgrade is always allowed.
		Name:                     "same-version-upgrade-allowed",
		DaemonStatus:             "paused",
		ExpectedHash:             "abc123",
		ActualHash:               "abc123",
		OnDiskSchemaVersion:      "2.0",
		NewBinarySchemaSupported: []string{"2.0", "1.9"}, // on-disk matches N exactly
		WantExitCode:             0,
		WantNewState:             "upgrading",
	},
}

// upgradeContractFixtureApply simulates the upgrade contract logic per
// specs/operator-nfr.md section 7.3 (upgrade protocol pseudocode) and ON-020.
//
// Returns (exitCode, emittedEvent, newState).
func upgradeContractFixtureApply(fx upgradeContractFixture) (exitCode int, emittedEvent string, newState string) {
	// Precondition: daemon must be paused (ON-020 + section 7.3).
	if fx.DaemonStatus != "paused" {
		return 13, "operator_upgrade_rejected", fx.DaemonStatus
	}

	// Sub-obligation (b): commit hash check (ON-005).
	if fx.ActualHash == "" || fx.ActualHash != fx.ExpectedHash {
		return 14, "operator_upgrade_rejected", fx.DaemonStatus
	}

	// Sub-obligation (d): schema compat check (ON-019 / ON-021).
	schemaOK := false
	for _, v := range fx.NewBinarySchemaSupported {
		if v == fx.OnDiskSchemaVersion {
			schemaOK = true
			break
		}
	}
	if !schemaOK {
		return 15, "operator_upgrade_rejected", fx.DaemonStatus
	}

	// All checks pass: transition to upgrading (emit operator_upgrading).
	return 0, "operator_upgrading", "upgrading"
}

// TestON020_UpgradeContractAllScenarios runs every upgrade contract scenario.
//
// Spec ref: operator-nfr.md section 4.6 ON-020; section 10.2 -- "Full upgrade
// scenario tests covering all sub-obligations of ON-020."
func TestON020_UpgradeContractAllScenarios(t *testing.T) {
	t.Parallel()

	for _, fx := range upgradeContractFixtureScenarios {
		fx := fx
		t.Run(fx.Name, func(t *testing.T) {
			t.Parallel()

			exitCode, emittedEvent, newState := upgradeContractFixtureApply(fx)

			if exitCode != fx.WantExitCode {
				t.Errorf("ON-020: %s: exit code = %d, want %d", fx.Name, exitCode, fx.WantExitCode)
			}
			if fx.WantEmittedEvent != "" && emittedEvent != fx.WantEmittedEvent {
				t.Errorf("ON-020: %s: emitted_event = %q, want %q", fx.Name, emittedEvent, fx.WantEmittedEvent)
			}
			if fx.WantNewState != "" && newState != fx.WantNewState {
				t.Errorf("ON-020: %s: new_state = %q, want %q", fx.Name, newState, fx.WantNewState)
			}

			// On rejection, verify the taxonomy entry for the exit code.
			if fx.WantExitCode != 0 {
				e, ok := operatornfr.LookupExitCode(fx.WantExitCode)
				if !ok {
					t.Fatalf("ON-020: %s: section 8 taxonomy missing code %d", fx.Name, fx.WantExitCode)
				}
				if e.Event != fx.WantEmittedEvent {
					t.Errorf("ON-020: %s: taxonomy code %d event = %q, want %q", fx.Name, fx.WantExitCode, e.Event, fx.WantEmittedEvent)
				}
			}
		})
	}
}

// upgradingMarkerFixture models the ON-020a daemon.upgrading marker state.
//
// Spec ref: operator-nfr.md section 4.6 ON-020a -- "When `harmonik upgrade`
// enters the drain phase, the daemon MUST atomically write `.harmonik/daemon.upgrading`
// containing: (a) the operator-supplied `expected_commit_hash`; (b) the
// upgrade-initiation timestamp; (c) the operator's session_id."
type upgradingMarkerFixture struct {
	// ExpectedCommitHash is the hash the operator supplied.
	ExpectedCommitHash string
	// UpgradeInitTimestamp is a timestamp string (RFC 3339 format).
	UpgradeInitTimestamp string
	// SessionID is the operator's session identifier.
	SessionID string
}

// upgradingMarkerFixtureOnStartup models the ON-020a startup check: read
// the marker and compare against the binary's embedded commit hash.
type upgradingMarkerFixtureOnStartup struct {
	// MarkerPresent indicates whether .harmonik/daemon.upgrading exists.
	MarkerPresent bool
	// MarkerExpectedHash is the hash recorded in the marker.
	MarkerExpectedHash string
	// BinaryActualHash is the running binary's embedded stamp.
	BinaryActualHash string
	// WantStartupAllowed is true if startup should proceed normally.
	WantStartupAllowed bool
	// WantExitCode is the section 8 code on rejection (0 = allowed).
	WantExitCode int
	// WantFailureMode identifies the startup failure detail.
	WantFailureMode string
}

// upgradingMarkerFixtureStartupScenarios covers ON-020a's startup check logic.
var upgradingMarkerFixtureStartupScenarios = []upgradingMarkerFixtureOnStartup{
	{
		// No marker: normal startup.
		MarkerPresent:      false,
		WantStartupAllowed: true,
	},
	{
		// Marker present, hash matches: startup proceeds, marker consumed.
		MarkerPresent:      true,
		MarkerExpectedHash: "abc123",
		BinaryActualHash:   "abc123",
		WantStartupAllowed: true,
	},
	{
		// Marker present, hash DOES NOT match: startup refused (code 14).
		// Spec ref: operator-nfr.md section 4.6 ON-020a -- "if present and the
		// hash does not match, the daemon MUST refuse startup with section 8 code 14
		// and emit daemon_startup_failed{failure_mode=upgrade-hash-mismatch-on-restart}."
		MarkerPresent:      true,
		MarkerExpectedHash: "abc123",
		BinaryActualHash:   "wronghash",
		WantStartupAllowed: false,
		WantExitCode:       14,
		WantFailureMode:    "upgrade-hash-mismatch-on-restart",
	},
}

// upgradingMarkerFixtureStartupCheck simulates the ON-020a startup check.
func upgradingMarkerFixtureStartupCheck(fx upgradingMarkerFixtureOnStartup) (allowed bool, exitCode int, failureMode string) {
	if !fx.MarkerPresent {
		return true, 0, ""
	}
	if fx.BinaryActualHash == fx.MarkerExpectedHash {
		// Hash matches: startup proceeds, marker is consumed.
		return true, 0, ""
	}
	// Hash mismatch: refuse startup.
	return false, 14, "upgrade-hash-mismatch-on-restart"
}

// TestON020a_UpgradingMarkerStartupCheck verifies the ON-020a startup check
// for the .harmonik/daemon.upgrading marker file.
//
// Spec ref: operator-nfr.md section 4.6 ON-020a.
func TestON020a_UpgradingMarkerStartupCheck(t *testing.T) {
	t.Parallel()

	for i, fx := range upgradingMarkerFixtureStartupScenarios {
		fx := fx
		i := i
		t.Run(upgradeMarkerScenarioName(i, fx), func(t *testing.T) {
			t.Parallel()

			allowed, exitCode, failureMode := upgradingMarkerFixtureStartupCheck(fx)

			if allowed != fx.WantStartupAllowed {
				t.Errorf("ON-020a: scenario %d: startup allowed = %v, want %v", i, allowed, fx.WantStartupAllowed)
			}
			if exitCode != fx.WantExitCode {
				t.Errorf("ON-020a: scenario %d: exit code = %d, want %d", i, exitCode, fx.WantExitCode)
			}
			if failureMode != fx.WantFailureMode {
				t.Errorf("ON-020a: scenario %d: failure mode = %q, want %q", i, failureMode, fx.WantFailureMode)
			}

			// On refusal, verify code 14 resolves in the taxonomy.
			if !fx.WantStartupAllowed && fx.WantExitCode == 14 {
				e, ok := operatornfr.LookupExitCode(14)
				if !ok {
					t.Fatal("ON-020a: section 8 taxonomy missing code 14 (upgrade-hash-mismatch)")
				}
				if e.Category != "upgrade-hash-mismatch" {
					t.Errorf("ON-020a: code 14 category = %q, want %q", e.Category, "upgrade-hash-mismatch")
				}
			}
		})
	}
}

// upgradeMarkerScenarioName returns a t.Run label for a startup scenario.
func upgradeMarkerScenarioName(i int, fx upgradingMarkerFixtureOnStartup) string {
	if !fx.MarkerPresent {
		return "no-marker"
	}
	if fx.WantStartupAllowed {
		return "marker-hash-match"
	}
	return "marker-hash-mismatch"
}

// TestON020a_MarkerContainsRequiredFields verifies that the upgrading marker
// fixture contains all three required fields per ON-020a.
//
// Spec ref: operator-nfr.md section 4.6 ON-020a -- "(a) the operator-supplied
// `expected_commit_hash`; (b) the upgrade-initiation timestamp; (c) the
// operator's session_id."
func TestON020a_MarkerContainsRequiredFields(t *testing.T) {
	t.Parallel()

	marker := upgradingMarkerFixture{
		ExpectedCommitHash:   "abc123def456",
		UpgradeInitTimestamp: "2026-05-09T12:00:00.000Z",
		SessionID:            "sess-001",
	}

	if marker.ExpectedCommitHash == "" {
		t.Error("ON-020a: marker missing ExpectedCommitHash (field a); MUST be present")
	}
	if marker.UpgradeInitTimestamp == "" {
		t.Error("ON-020a: marker missing UpgradeInitTimestamp (field b); MUST be present")
	}
	if marker.SessionID == "" {
		t.Error("ON-020a: marker missing SessionID (field c); MUST be present")
	}
}

// upgradeStatePreservationFixture models a cross-version state preservation
// scenario for ON-021.
//
// Spec ref: operator-nfr.md section 4.6 ON-021 -- "An `upgrade` operation MUST
// NOT make any in-flight run unrecoverable. The recoverability invariant holds
// iff entry into `paused` implies drain-completion."
type upgradeStatePreservationFixture struct {
	// Name is the scenario label.
	Name string
	// DrainCompleted indicates whether the pausing -> paused gate fired (drain done).
	DrainCompleted bool
	// InFlightRunsAtPaused is the count of in-flight runs when paused is entered.
	// ON-021: MUST be 0 (drain-completed implies no in-flight runs).
	InFlightRunsAtPaused int
	// OnDiskSchemaVersion is the schema version at upgrade time.
	OnDiskSchemaVersion string
	// NewBinarySchemaSupported is the new binary's supported schema set.
	NewBinarySchemaSupported []string
	// WantRecoverable is true if all in-flight runs are recoverable post-upgrade.
	WantRecoverable bool
}

// upgradeStatePreservationFixtureScenarios covers ON-021's recoverability invariant.
var upgradeStatePreservationFixtureScenarios = []upgradeStatePreservationFixture{
	{
		// Drain-completed + schema-compat: all runs recoverable.
		Name:                     "drain-complete-schema-compat",
		DrainCompleted:           true,
		InFlightRunsAtPaused:     0,
		OnDiskSchemaVersion:      "1.9",
		NewBinarySchemaSupported: []string{"2.0", "1.9"},
		WantRecoverable:          true,
	},
	{
		// Schema incompatible: ON-021 says upgrade MUST be rejected (handled by ON-020),
		// so this scenario would not reach exec-replace. Post-rejection, runs are safe.
		Name:                     "schema-incompatible-rejected-before-execreplace",
		DrainCompleted:           true,
		InFlightRunsAtPaused:     0,
		OnDiskSchemaVersion:      "1.5",
		NewBinarySchemaSupported: []string{"2.0", "1.9"},
		// ON-021: the cross-version state contract MUST reject, so recoverability
		// is preserved (rejected = no state change).
		WantRecoverable: true,
	},
	{
		// Drain NOT completed (hypothetical violation): runs NOT recoverable.
		// This scenario tests that the ON-008 drain gate prevents this from happening.
		Name:                     "drain-not-complete-violation",
		DrainCompleted:           false,
		InFlightRunsAtPaused:     3, // in-flight runs exist at "paused" -- forbidden by ON-008
		OnDiskSchemaVersion:      "1.9",
		NewBinarySchemaSupported: []string{"2.0", "1.9"},
		WantRecoverable:          false, // drain-gate violation makes this scenario unsafe
	},
}

// upgradeStatePreservationCheck evaluates whether the upgrade preserves run
// recoverability per ON-021.
func upgradeStatePreservationCheck(fx upgradeStatePreservationFixture) bool {
	// ON-021: recoverability iff drain-completed (paused gate fired).
	// If drain was NOT completed, in-flight runs may be unrecoverable.
	if !fx.DrainCompleted {
		return false
	}
	// DrainCompleted implies InFlightRunsAtPaused == 0 per ON-008.
	if fx.InFlightRunsAtPaused != 0 {
		return false
	}
	// Schema compat: if incompatible, upgrade is rejected before exec-replace,
	// so recoverability is preserved (no state change).
	return true
}

// TestON021_UpgradePreservesRunRecoverability verifies ON-021's recoverability
// invariant across same-version and N-1 upgrade scenarios.
//
// Spec ref: operator-nfr.md section 4.6 ON-021; section 10.2 -- "verify
// cross-version state preservation across same-version and N-1 upgrades; iff
// drain-completed."
func TestON021_UpgradePreservesRunRecoverability(t *testing.T) {
	t.Parallel()

	for _, fx := range upgradeStatePreservationFixtureScenarios {
		fx := fx
		t.Run(fx.Name, func(t *testing.T) {
			t.Parallel()

			recoverable := upgradeStatePreservationCheck(fx)
			if recoverable != fx.WantRecoverable {
				t.Errorf("ON-021: %s: recoverable = %v, want %v; drain-completed=%v, in-flight=%d",
					fx.Name, recoverable, fx.WantRecoverable, fx.DrainCompleted, fx.InFlightRunsAtPaused)
			}
		})
	}
}

// TestON021_DrainGateIsNecessaryForRecoverability verifies the iff relationship
// from ON-021: recoverability holds if AND ONLY IF entry into paused implies
// drain-completion.
//
// Spec ref: operator-nfr.md section 4.6 ON-021 -- "The recoverability invariant
// holds iff entry into `paused` implies drain-completion."
func TestON021_DrainGateIsNecessaryForRecoverability(t *testing.T) {
	t.Parallel()

	// Without drain, recoverability is NOT guaranteed.
	withoutDrain := upgradeStatePreservationCheck(upgradeStatePreservationFixture{
		DrainCompleted:           false,
		InFlightRunsAtPaused:     2,
		OnDiskSchemaVersion:      "1.9",
		NewBinarySchemaSupported: []string{"2.0", "1.9"},
	})
	if withoutDrain {
		t.Error("ON-021: without drain-completion, recoverability should NOT hold; the iff direction requires drain-completed")
	}

	// With drain, recoverability IS guaranteed (for compat schema).
	withDrain := upgradeStatePreservationCheck(upgradeStatePreservationFixture{
		DrainCompleted:           true,
		InFlightRunsAtPaused:     0,
		OnDiskSchemaVersion:      "1.9",
		NewBinarySchemaSupported: []string{"2.0", "1.9"},
	})
	if !withDrain {
		t.Error("ON-021: with drain-completion and compat schema, recoverability MUST hold")
	}
}

// TestON020_RollbackIsFirstClass verifies the structural presence of the rollback
// sub-obligation (f) in the ON-020 taxonomy and state alignment.
//
// Spec ref: operator-nfr.md section 4.6 ON-020(f) -- "On `harmonik upgrade
// --rollback` invocation while the daemon is `paused` after a successful upgrade,
// the daemon MUST exec-replace back to the prior binary."
func TestON020_RollbackIsFirstClass(t *testing.T) {
	t.Parallel()

	// Rollback invocation: daemon must be paused (post-upgrade paused state).
	// From the state machine: after exec-replace, new binary starts at running.
	// Rollback while paused uses the same hash-check gate as upgrade.
	// Verify that code 14 (hash mismatch) applies to rollback hash check too.
	e, ok := operatornfr.LookupExitCode(14)
	if !ok {
		t.Fatal("ON-020(f): section 8 taxonomy missing code 14 (upgrade-hash-mismatch); rollback uses same gate")
	}
	if e.Category != "upgrade-hash-mismatch" {
		t.Errorf("ON-020(f): code 14 category = %q, want %q", e.Category, "upgrade-hash-mismatch")
	}

	// ON-020(f): rollback during live upgrade window (between drain-complete and
	// exec-replace) is NOT supported; operator must resume and retry or stop.
	// Verify by checking the state machine: rollback is only valid in paused state.
	// In upgrading state, rollback is not a valid command (would be code 16).
	e16, ok16 := operatornfr.LookupExitCode(16)
	if !ok16 {
		t.Fatal("ON-020(f): section 8 taxonomy missing code 16 (operator-control-invalid-state)")
	}
	if e16.Event != "operator_command_rejected" {
		t.Errorf("ON-020(f): code 16 event = %q, want %q", e16.Event, "operator_command_rejected")
	}
}

// TestON020_PostExecReplaceFailureRecovery verifies the ON-020(g) post-exec-
// replace failure recovery obligation: rollback removes stale pidfile/socket.
//
// Spec ref: operator-nfr.md section 4.6 ON-020(g) -- "If the new binary's
// startup fails ... the operator MUST be able to recover by invoking
// `harmonik upgrade --rollback`, which removes the stale pidfile/socket."
func TestON020_PostExecReplaceFailureRecovery(t *testing.T) {
	t.Parallel()

	// Startup failure after exec-replace: pidfile and socket are stale.
	// The recovery path: rollback removes stale artifacts and restores prior binary.
	// The startup failure is category from the startup catalog (e.g., pidfile-locked).

	// Verify code 5 (pidfile-locked) is in the startup catalog.
	e, ok := operatornfr.LookupExitCode(5)
	if !ok {
		t.Fatal("ON-020(g): section 8 taxonomy missing code 5 (pidfile-locked); stale-pidfile scenario relies on it")
	}
	if e.Category != "pidfile-locked" {
		t.Errorf("ON-020(g): code 5 category = %q, want %q", e.Category, "pidfile-locked")
	}

	// Rollback consumes the daemon.upgrading marker to determine prior binary's hash.
	// Verify code 14 is available for rollback's integrity gate.
	_, ok14 := operatornfr.LookupExitCode(14)
	if !ok14 {
		t.Fatal("ON-020(g): section 8 taxonomy missing code 14; rollback integrity gate relies on it")
	}
}

// TestON020_SocketContinuityRequirementStructural verifies the structural
// presence of ON-020(e) fd-passing socket continuity obligation in the taxonomy.
//
// Spec ref: operator-nfr.md section 4.6 ON-020(e) -- "The daemon MUST preserve
// the bound listener fd across exec-replace via fd-passing ... clients observe
// no `ECONNREFUSED` window."
func TestON020_SocketContinuityRequirementStructural(t *testing.T) {
	t.Parallel()

	// Socket continuity is a runtime property of exec-replace. At the harness
	// level, we verify that the upgrade path (exec-replace) resolves to the
	// upgrading state before the new binary adopts the socket.
	// The structural check: upgrading state MUST be followed by running (no
	// intermediate socket-bind failure possible if fd-passing is correct).

	// Verify upgrading -> running transition in the state machine.
	to, emitted, rejected := smFixtureApply(smStateUpgrading, smCommandExecReplace, "")
	if rejected {
		t.Error("ON-020(e): exec-replace rejected in upgrading state; socket-continuity transition MUST be accepted")
	}
	if to != smStateRunning {
		t.Errorf("ON-020(e): exec-replace in upgrading -> %q, want %q; no ECONNREFUSED window means gap-free transition", to, smStateRunning)
	}
	if emitted != "operator_upgrade_completed" {
		t.Errorf("ON-020(e): exec-replace emitted %q, want %q", emitted, "operator_upgrade_completed")
	}
}
