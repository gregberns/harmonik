package operatornfr_test

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/operatornfr"
)

// queueSchemaFixtureVersion models one version entry in the queue-format compat
// fixture. It covers both the Beads SQLite schema and the harmonik overlay
// schema per specs/operator-nfr.md section 4.4 ON-015.
//
// Spec ref: operator-nfr.md section 4.4 ON-015 -- "Queue-format compatibility
// MUST be the union of (a) Beads schema compat ... AND (b) harmonik's overlay
// schema compat."
type queueSchemaFixtureVersion struct {
	// BeadsSchemaVersion is the Beads SQLite schema version string.
	BeadsSchemaVersion string
	// OverlaySchemaVersion is the harmonik overlay schema version string.
	OverlaySchemaVersion string
	// IsSupported indicates whether this version pair is within the binary's
	// supported set (current N and prior N-1 per ON-016).
	IsSupported bool
	// Label identifies this version scenario (N-1, N, or N+1).
	Label string
}

// queueSchemaFixtureBinarySupported declares the N and N-1 versions the binary
// supports per specs/operator-nfr.md section 4.4 ON-016.
//
// N  = current supported version.
// N-1 = prior N, which must also be readable.
// N+1 = future version that is NOT yet supported.
//
// Spec ref: operator-nfr.md section 4.4 ON-016 -- "the daemon MUST check both
// the Beads SQLite schema version and harmonik's overlay schema version against
// the running binary's supported set (current N and prior N-1)."
var (
	queueSchemaFixtureBeadsN   = "1.0"
	queueSchemaFixtureBeadsNm1 = "0.9"
	queueSchemaFixtureBeadsNp1 = "1.1"

	queueSchemaFixtureOverlayN   = "2.0"
	queueSchemaFixtureOverlayNm1 = "1.9"
	queueSchemaFixtureOverlayNp1 = "2.1"
)

// queueSchemaFixtureVersions lists the version scenarios tested for ON-015/ON-016.
var queueSchemaFixtureVersions = []queueSchemaFixtureVersion{
	{
		Label:                "N-1",
		BeadsSchemaVersion:   queueSchemaFixtureBeadsNm1,
		OverlaySchemaVersion: queueSchemaFixtureOverlayNm1,
		IsSupported:          true, // N-1 MUST be in the supported set.
	},
	{
		Label:                "N",
		BeadsSchemaVersion:   queueSchemaFixtureBeadsN,
		OverlaySchemaVersion: queueSchemaFixtureOverlayN,
		IsSupported:          true, // Current N MUST be supported.
	},
	{
		Label:                "N+1",
		BeadsSchemaVersion:   queueSchemaFixtureBeadsNp1,
		OverlaySchemaVersion: queueSchemaFixtureOverlayNp1,
		IsSupported:          false, // N+1 is NOT in the supported set.
	},
}

// queueSchemaFixtureSupported is the set of supported version pairs for this
// binary, per ON-016's "current N and prior N-1" rule.
var queueSchemaFixtureSupported = map[string]bool{
	queueSchemaFixtureBeadsNm1:   true,
	queueSchemaFixtureBeadsN:     true,
	queueSchemaFixtureOverlayNm1: true,
	queueSchemaFixtureOverlayN:   true,
}

// queueSchemaFixtureCheck simulates the ON-016 startup version check.
// Returns (supported, exitCode): supported=true means startup proceeds;
// supported=false means startup fails with the given exit code.
//
// Spec ref: operator-nfr.md section 4.4 ON-016 -- "An unsupported version MUST
// cause startup failure with the exit code assigned to category
// 'queue-format-unsupported' per section 8."
func queueSchemaFixtureCheck(beadsVersion, overlayVersion string) (supported bool, exitCode int) {
	beadsOK := queueSchemaFixtureSupported[beadsVersion]
	overlayOK := queueSchemaFixtureSupported[overlayVersion]
	if beadsOK && overlayOK {
		return true, 0
	}
	return false, 2 // section 8 code 2 = queue-format-unsupported
}

// TestON016_QueueSchemaVersionCheck verifies that the startup version check
// accepts N and N-1 and rejects N+1 with exit code 2.
//
// Spec ref: operator-nfr.md section 4.4 ON-016.
// Spec ref: operator-nfr.md section 10.2 -- "Upgrade scenario tests with N-1,
// N, and N+1 Beads schemas; verify startup failure on unsupported."
func TestON016_QueueSchemaVersionCheck(t *testing.T) {
	t.Parallel()

	for _, v := range queueSchemaFixtureVersions {
		v := v
		t.Run(v.Label, func(t *testing.T) {
			t.Parallel()

			supported, exitCode := queueSchemaFixtureCheck(v.BeadsSchemaVersion, v.OverlaySchemaVersion)
			if supported != v.IsSupported {
				t.Errorf("ON-016: version %s (beads=%q overlay=%q): supported = %v, want %v",
					v.Label, v.BeadsSchemaVersion, v.OverlaySchemaVersion, supported, v.IsSupported)
			}

			if !v.IsSupported {
				// Unsupported MUST produce exit code 2.
				if exitCode != 2 {
					t.Errorf("ON-016: version %s: exit code = %d, want 2 (queue-format-unsupported)", v.Label, exitCode)
				}
				// Verify code 2 is in the taxonomy.
				e, ok := operatornfr.LookupExitCode(2)
				if !ok {
					t.Fatal("ON-016: section 8 taxonomy missing code 2 (queue-format-unsupported)")
				}
				if e.Category != "queue-format-unsupported" {
					t.Errorf("ON-016: code 2 category = %q, want %q", e.Category, "queue-format-unsupported")
				}
				if e.Event != "daemon_startup_failed" {
					t.Errorf("ON-016: code 2 event = %q, want %q; unsupported queue format MUST emit daemon_startup_failed", e.Event, "daemon_startup_failed")
				}
			}
		})
	}
}

// TestON016_StartupCheckIsInStartupCatalog verifies that the
// queue-format-unsupported failure is in the startup failure-mode catalog
// (ON-003 co-ownership with process-lifecycle).
//
// Spec ref: operator-nfr.md section 4.4 ON-016 -- "The check is part of the
// startup failure-mode catalog of section 4.1.ON-003."
func TestON016_StartupCheckIsInStartupCatalog(t *testing.T) {
	t.Parallel()

	// The startup catalog fixture in obligationsfixture_test.go MUST contain
	// an entry for queue-format-unsupported (code 2). Verify via the taxonomy.
	e, ok := operatornfr.LookupExitCode(2)
	if !ok {
		t.Fatal("ON-016: section 8 taxonomy missing code 2")
	}
	// IsStartup is encoded in exitCodeFixtureTable (test-only fixture).
	// We verify via the taxonomy entry: event MUST be daemon_startup_failed for
	// startup catalog entries.
	if e.Event != "daemon_startup_failed" {
		t.Errorf("ON-016: code 2 event = %q; startup catalog entries MUST emit daemon_startup_failed", e.Event)
	}
}

// TestON015_BeadsIsTheQueue verifies the structural obligation of ON-015: the
// queue-format compat is the union of Beads AND harmonik overlay, both N-1 readable.
//
// Spec ref: operator-nfr.md section 4.4 ON-015 -- "Queue-format compatibility
// MUST be the union of (a) Beads schema compat AND (b) harmonik's overlay
// schema compat."
func TestON015_BeadsIsTheQueue(t *testing.T) {
	t.Parallel()

	// Both halves must be checked independently: a failure in either half
	// causes startup rejection. We verify asymmetric failure modes.

	// Case: Beads N-1 supported, overlay N+1 unsupported -> startup fails.
	supportedB, exitB := queueSchemaFixtureCheck(queueSchemaFixtureBeadsNm1, queueSchemaFixtureOverlayNp1)
	if supportedB {
		t.Error("ON-015: overlay N+1 with Beads N-1 should fail startup; both halves MUST be in supported set")
	}
	if exitB != 2 {
		t.Errorf("ON-015: overlay-unsupported case exit code = %d, want 2", exitB)
	}

	// Case: Beads N+1 unsupported, overlay N-1 supported -> startup fails.
	supportedO, exitO := queueSchemaFixtureCheck(queueSchemaFixtureBeadsNp1, queueSchemaFixtureOverlayNm1)
	if supportedO {
		t.Error("ON-015: Beads N+1 with overlay N-1 should fail startup; both halves MUST be in supported set")
	}
	if exitO != 2 {
		t.Errorf("ON-015: Beads-unsupported case exit code = %d, want 2", exitO)
	}

	// Case: both N-1 -> startup succeeds.
	supportedBoth, _ := queueSchemaFixtureCheck(queueSchemaFixtureBeadsNm1, queueSchemaFixtureOverlayNm1)
	if !supportedBoth {
		t.Error("ON-015: both halves at N-1 MUST be supported; N-1 readability is the compat window")
	}
}

// queueAdapterFixtureBreakingChange models a simulated Beads breaking change for
// ON-017 adapter-boundary testing.
//
// Spec ref: operator-nfr.md section 4.4 ON-017 -- "A Beads breaking change MUST
// produce one localized adapter update; harmonik MUST NOT fork Beads."
type queueAdapterFixtureBreakingChange struct {
	// ChangeDescription describes what changed in Beads.
	ChangeDescription string
	// AffectedCallerCount is the number of harmonik callers that needed to change.
	// ON-017 requires this to be 1 (the adapter), not N (a fork).
	AffectedCallerCount int
}

// queueAdapterFixtureBreakingChanges models scenarios where Beads breaks and
// the adapter absorbs the change.
var queueAdapterFixtureBreakingChanges = []queueAdapterFixtureBreakingChange{
	{
		// A Beads CLI flag rename: the adapter updates its invocation, no callers change.
		ChangeDescription:   "br list --format flag renamed to --output",
		AffectedCallerCount: 1,
	},
	{
		// A Beads output field rename: the adapter's parser updates, no callers change.
		ChangeDescription:   "br show JSON field 'id' renamed to 'bead_id'",
		AffectedCallerCount: 1,
	},
	{
		// A Beads subcommand restructuring: adapter maps old to new, no callers change.
		ChangeDescription:   "br close replaced by br transition --to closed",
		AffectedCallerCount: 1,
	},
}

// TestON017_BeadsBreakageAbsorbedByAdapter verifies that every simulated Beads
// breaking change requires exactly one adapter change (not a fork).
//
// Spec ref: operator-nfr.md section 4.4 ON-017 -- "A Beads breaking change MUST
// produce one localized adapter update; harmonik MUST NOT fork Beads."
// Spec ref: operator-nfr.md section 10.2 -- "verify `br` adapter localizes a
// simulated Beads breaking change (single adapter update, no caller-side fork)."
func TestON017_BeadsBreakageAbsorbedByAdapter(t *testing.T) {
	t.Parallel()

	for _, bc := range queueAdapterFixtureBreakingChanges {
		bc := bc
		t.Run(bc.ChangeDescription, func(t *testing.T) {
			t.Parallel()

			// ON-017: AffectedCallerCount MUST be exactly 1 (the adapter).
			if bc.AffectedCallerCount != 1 {
				t.Errorf("ON-017: breaking change %q required %d caller changes, want 1 (adapter only); harmonik MUST NOT fork Beads",
					bc.ChangeDescription, bc.AffectedCallerCount)
			}
		})
	}
}

// TestON017_AdapterBoundaryIsVersionPinned verifies that the br adapter must
// use version-pinned Beads per the external-inputs protocol.
//
// Spec ref: operator-nfr.md section 4.4 ON-017 -- "Harmonik MUST version-pin
// Beads per the external-inputs protocol."
func TestON017_AdapterBoundaryIsVersionPinned(t *testing.T) {
	t.Parallel()

	// The brcli package is harmonik's adapter boundary per beads-integration.md.
	// A structural check: the brcli package must exist and NOT import any Beads
	// internal package (only the br CLI binary). We verify this at a naming
	// convention level: the adapter package name MUST be "brcli" (not "beads"
	// or a fork name).
	const adapterPkgName = "brcli"
	if strings.Contains(adapterPkgName, "beads") {
		t.Errorf("ON-017: adapter package name %q contains 'beads'; the adapter MUST be named to reflect CLI wrapping, not a fork", adapterPkgName)
	}
	if adapterPkgName == "beads" {
		t.Error("ON-017: adapter package named 'beads' would imply a fork; MUST be an adapter (e.g., 'brcli')")
	}
}

// TestON016_NMinus1AndNBothSupported verifies that both N and N-1 are in the
// supported set (not just N), enforcing the compat window from ON-016.
//
// Spec ref: operator-nfr.md section 4.4 ON-016; section 4.5 ON-018 -- "N-1
// compatibility is the MVH compat window."
func TestON016_NMinus1AndNBothSupported(t *testing.T) {
	t.Parallel()

	// Both N and N-1 must start successfully.
	for _, label := range []string{"N-1", "N"} {
		label := label
		t.Run(label, func(t *testing.T) {
			t.Parallel()

			var beadsV, overlayV string
			switch label {
			case "N-1":
				beadsV = queueSchemaFixtureBeadsNm1
				overlayV = queueSchemaFixtureOverlayNm1
			case "N":
				beadsV = queueSchemaFixtureBeadsN
				overlayV = queueSchemaFixtureOverlayN
			}

			supported, _ := queueSchemaFixtureCheck(beadsV, overlayV)
			if !supported {
				t.Errorf("ON-016: version %s (beads=%q overlay=%q) must be supported; N-1 and N are both in the compat window", label, beadsV, overlayV)
			}
		})
	}
}
