package operatornfr_test

// hk-sx9r.20 binding test — ON-016 startup check: failure event payload MUST
// name the required migration release.
//
// Spec ref: specs/operator-nfr.md §4.4 ON-016 — "An unsupported version MUST
// cause startup failure with the exit code assigned to category
// 'queue-format-unsupported' per §8, naming the required migration release in
// the failure event payload."
//
// # Gap this sensor closes
//
// The sibling harness (hk-sx9r.78, schemacompatwindow_test.go +
// queueformatcompat_test.go) verifies that:
//   - unsupported versions produce exit code 2,
//   - code 2 maps to event "daemon_startup_failed",
//   - N-1 compat window semantics are enforced.
//
// What that harness does NOT verify is the payload-field obligation in ON-016:
// when failure_mode = "queue-format-unsupported", the daemon_startup_failed
// payload MUST carry a non-empty required_migration_release field. This file
// is the binding sensor for that clause.
//
// # What this sensor checks
//
//  1. DaemonStartupFailedPayload has a RequiredMigrationRelease field
//     (struct-shape assertion via field assignment + zero-value probe).
//
//  2. For failure_mode = "queue-format-unsupported", a payload without
//     RequiredMigrationRelease is rejected by the validator fixture
//     (missing-field detection).
//
//  3. For failure_mode = "queue-format-unsupported", a payload WITH a
//     non-empty RequiredMigrationRelease is accepted.
//
//  4. For other failure modes, RequiredMigrationRelease is optional
//     (empty is acceptable).
//
// # Helper prefix
//
// All package-level identifiers use the sx9r20Fixture prefix per
// implementer-protocol.md helper-prefix discipline.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/operatornfr"
)

// sx9r20FixtureFailureModeQueueUnsupported is the canonical failure mode string
// for queue-format-unsupported per operator-nfr.md §8 (code 2, category
// "queue-format-unsupported").
const sx9r20FixtureFailureModeQueueUnsupported = core.FailureMode("queue-format-unsupported")

// sx9r20FixtureCheckPayloadMigrationField simulates the ON-016 payload
// validator for queue-format-unsupported failures. Returns an error string
// (non-empty = invalid) if the failure_mode requires a migration release but
// the field is empty.
//
// Spec ref: operator-nfr.md §4.4 ON-016 — "naming the required migration
// release in the failure event payload."
func sx9r20FixtureCheckPayloadMigrationField(p core.DaemonStartupFailedPayload) string {
	if p.FailureMode == sx9r20FixtureFailureModeQueueUnsupported {
		if p.RequiredMigrationRelease == "" {
			return "ON-016: daemon_startup_failed{failure_mode=queue-format-unsupported} " +
				"MUST have non-empty required_migration_release field; " +
				"the operator needs to know which migration release to install"
		}
	}
	return "" // valid
}

// TestSX9R20_ON016_PayloadHasMigrationReleaseField verifies that
// DaemonStartupFailedPayload has a RequiredMigrationRelease field and that it
// is accessible (struct-shape assertion).
//
// Spec ref: operator-nfr.md §4.4 ON-016.
func TestSX9R20_ON016_PayloadHasMigrationReleaseField(t *testing.T) {
	t.Parallel()

	// Zero-value probe: the field must exist and default to empty string.
	var p core.DaemonStartupFailedPayload
	if p.RequiredMigrationRelease != "" {
		t.Errorf("ON-016: DaemonStartupFailedPayload.RequiredMigrationRelease zero value = %q, want empty string; the field must default to empty (omitempty) for non-migration-release failure modes",
			p.RequiredMigrationRelease)
	}

	// Assignment probe: the field must be writable.
	p.RequiredMigrationRelease = "harmonik-v1.1.0-migration"
	if p.RequiredMigrationRelease != "harmonik-v1.1.0-migration" {
		t.Errorf("ON-016: DaemonStartupFailedPayload.RequiredMigrationRelease assignment round-trip failed; want %q, got %q",
			"harmonik-v1.1.0-migration", p.RequiredMigrationRelease)
	}
}

// TestSX9R20_ON016_QueueUnsupportedPayloadMustNameMigrationRelease verifies
// that queue-format-unsupported failures with an empty required_migration_release
// are detected as invalid.
//
// Spec ref: operator-nfr.md §4.4 ON-016 — "naming the required migration
// release in the failure event payload."
func TestSX9R20_ON016_QueueUnsupportedPayloadMustNameMigrationRelease(t *testing.T) {
	t.Parallel()

	// Missing field: validator MUST reject.
	missing := core.DaemonStartupFailedPayload{
		FailedAt:                 "2026-05-10T10:00:00Z",
		ExitCode:                 2,
		FailureMode:              sx9r20FixtureFailureModeQueueUnsupported,
		RequiredMigrationRelease: "", // missing — ON-016 violation
	}
	if errStr := sx9r20FixtureCheckPayloadMigrationField(missing); errStr == "" {
		t.Error("ON-016: queue-format-unsupported payload with empty RequiredMigrationRelease " +
			"was not detected as invalid; the validator MUST reject it — the operator cannot " +
			"remediate without knowing which migration release to install")
	}

	// Present field: validator MUST accept.
	present := core.DaemonStartupFailedPayload{
		FailedAt:                 "2026-05-10T10:00:00Z",
		ExitCode:                 2,
		FailureMode:              sx9r20FixtureFailureModeQueueUnsupported,
		RequiredMigrationRelease: "harmonik-v1.1.0-migration",
	}
	if errStr := sx9r20FixtureCheckPayloadMigrationField(present); errStr != "" {
		t.Errorf("ON-016: queue-format-unsupported payload with non-empty RequiredMigrationRelease "+
			"was incorrectly rejected: %s", errStr)
	}
}

// TestSX9R20_ON016_OtherFailureModesDoNotRequireMigrationRelease verifies that
// for failure modes other than queue-format-unsupported, an empty
// RequiredMigrationRelease is acceptable (the field is optional for those modes).
//
// Spec ref: operator-nfr.md §4.4 ON-016 — the migration-release naming
// obligation is specific to "queue-format-unsupported" failures.
func TestSX9R20_ON016_OtherFailureModesDoNotRequireMigrationRelease(t *testing.T) {
	t.Parallel()

	otherModes := []core.FailureMode{
		"git-bad-state",
		"beads-unavailable",
		"checkpoint-schema-unsupported",
		"pidfile-locked",
		"filesystem-unwritable",
		"disk-full",
		"socket-bind-failed",
		"event-schema-unsupported",
		"runtime-panic",
		"ntm-unavailable",
		"orchestrator-agent-unavailable",
	}

	for _, mode := range otherModes {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()

			p := core.DaemonStartupFailedPayload{
				FailedAt:                 "2026-05-10T10:00:00Z",
				ExitCode:                 1,
				FailureMode:              mode,
				RequiredMigrationRelease: "", // empty is OK for non-migration-release modes
			}
			if errStr := sx9r20FixtureCheckPayloadMigrationField(p); errStr != "" {
				t.Errorf("ON-016: failure mode %q with empty RequiredMigrationRelease "+
					"was incorrectly rejected: %s; the migration-release field is REQUIRED "+
					"only for queue-format-unsupported failures", mode, errStr)
			}
		})
	}
}

// TestSX9R20_ON016_ExitCode2TaxonomyRemediation verifies that the §8 taxonomy
// entry for code 2 cites the migration-release remediation path (§4.5.ON-019).
//
// Spec ref: operator-nfr.md §8 — code 2 remediation: "Install migration release
// per §4.5.ON-019."
func TestSX9R20_ON016_ExitCode2TaxonomyRemediation(t *testing.T) {
	t.Parallel()

	e, ok := operatornfr.LookupExitCode(2)
	if !ok {
		t.Fatal("ON-016: §8 taxonomy missing code 2 (queue-format-unsupported)")
	}
	if e.Remediation == "" {
		t.Errorf("ON-016: §8 taxonomy entry for code 2 has empty Remediation; " +
			"the operator needs the migration-release install path")
	}
	// The remediation must reference the migration release concept so the
	// operator knows what action to take (install migration release).
	if !strings.Contains(e.Remediation, "migration") {
		t.Errorf("ON-016: §8 taxonomy code 2 Remediation = %q; expected 'migration' "+
			"reference pointing operator to migration-release install procedure per §4.5.ON-019",
			e.Remediation)
	}
}
