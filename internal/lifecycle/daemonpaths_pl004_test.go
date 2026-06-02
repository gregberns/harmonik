package lifecycle

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestPL004_HarmonikDir verifies that HarmonikDir returns
// <projectDir>/.harmonik.
//
// Spec ref: process-lifecycle.md §4.1 PL-004 — daemon file surface is
// rooted at .harmonik/.
func TestPL004_HarmonikDir(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	got := HarmonikDir(projectDir)
	want := filepath.Join(projectDir, ".harmonik")
	if got != want {
		t.Errorf("HarmonikDir: got %q, want %q", got, want)
	}
}

// TestPL004_PidfilePath verifies the canonical daemon.pid path.
//
// Spec ref: process-lifecycle.md §4.1 PL-002, PL-002a, PL-002b.
func TestPL004_PidfilePath(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	got := PidfilePath(projectDir)
	want := filepath.Join(projectDir, ".harmonik", "daemon.pid")
	if got != want {
		t.Errorf("PidfilePath: got %q, want %q", got, want)
	}
}

// TestPL004_SocketPath verifies the canonical daemon.sock path.
//
// Spec ref: process-lifecycle.md §4.1 PL-003, PL-003a, PL-003b.
func TestPL004_SocketPath(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	got := SocketPath(projectDir)
	want := filepath.Join(projectDir, ".harmonik", "daemon.sock")
	if got != want {
		t.Errorf("SocketPath: got %q, want %q", got, want)
	}
}

// TestPL004_InstanceIDPath verifies the canonical daemon.instance-id path.
//
// Spec ref: process-lifecycle.md §4.2 PL-005 step 0.
func TestPL004_InstanceIDPath(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	got := InstanceIDPath(projectDir)
	want := filepath.Join(projectDir, ".harmonik", "daemon.instance-id")
	if got != want {
		t.Errorf("InstanceIDPath: got %q, want %q", got, want)
	}
}

// TestPL004_UpgradingMarkerPath verifies the canonical daemon.upgrading path.
//
// Spec ref: process-lifecycle.md §4.9 PL-027(iv); operator-nfr.md §4.6 ON-020a.
func TestPL004_UpgradingMarkerPath(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	got := UpgradingMarkerPath(projectDir)
	want := filepath.Join(projectDir, ".harmonik", "daemon.upgrading")
	if got != want {
		t.Errorf("UpgradingMarkerPath: got %q, want %q", got, want)
	}
}

// TestPL004_StateMarkerPath verifies the canonical daemon.state path.
//
// Spec ref: operator-nfr.md §4.7 ON-030a; process-lifecycle.md §4.2 PL-005
// step 8a.
func TestPL004_StateMarkerPath(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	got := StateMarkerPath(projectDir)
	want := filepath.Join(projectDir, ".harmonik", "daemon.state")
	if got != want {
		t.Errorf("StateMarkerPath: got %q, want %q", got, want)
	}
}

// TestPL004_EventIDHWMPath verifies the canonical event_id_hwm path.
//
// Spec ref: event-model.md §4.1.
func TestPL004_EventIDHWMPath(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	got := EventIDHWMPath(projectDir)
	want := filepath.Join(projectDir, ".harmonik", "event_id_hwm")
	if got != want {
		t.Errorf("EventIDHWMPath: got %q, want %q", got, want)
	}
}

// TestPL004_EventsDir verifies the canonical events/ directory path.
//
// Spec ref: event-model.md §6.2.
func TestPL004_EventsDir(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	got := EventsDir(projectDir)
	want := filepath.Join(projectDir, ".harmonik", "events")
	if got != want {
		t.Errorf("EventsDir: got %q, want %q", got, want)
	}
}

// TestPL004_BeadsIntentsDir verifies the canonical beads-intents/ directory.
//
// Spec ref: beads-integration.md §4.10 BI-030.
func TestPL004_BeadsIntentsDir(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	got := BeadsIntentsDir(projectDir)
	want := filepath.Join(projectDir, ".harmonik", "beads-intents")
	if got != want {
		t.Errorf("BeadsIntentsDir: got %q, want %q", got, want)
	}
}

// TestPL004_ReconciliationLocksDir verifies the canonical reconciliation-locks/
// directory path.
//
// Spec ref: reconciliation/spec.md §4.1 RC-002a.
func TestPL004_ReconciliationLocksDir(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	got := ReconciliationLocksDir(projectDir)
	want := filepath.Join(projectDir, ".harmonik", "reconciliation-locks")
	if got != want {
		t.Errorf("ReconciliationLocksDir: got %q, want %q", got, want)
	}
}

// TestPL004_ReconciliationLockPath verifies the per-run lock file path.
//
// Spec ref: reconciliation/spec.md §4.1 RC-002a.
func TestPL004_ReconciliationLockPath(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	const runID = "run-abc123"
	got := ReconciliationLockPath(projectDir, runID)
	want := filepath.Join(projectDir, ".harmonik", "reconciliation-locks", runID+".lock")
	if got != want {
		t.Errorf("ReconciliationLockPath: got %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, ".lock") {
		t.Errorf("ReconciliationLockPath: got %q, want .lock suffix", got)
	}
}

// TestPL004_SpillFilePath verifies the per-consumer spill file path.
//
// Spec ref: event-model.md §6.2; event-model.md §4.4 (EV-011a).
func TestPL004_SpillFilePath(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	const consumer = "beads-adapter"
	got := SpillFilePath(projectDir, consumer)
	want := filepath.Join(projectDir, ".harmonik", "events", "spill-beads-adapter.jsonl")
	if got != want {
		t.Errorf("SpillFilePath: got %q, want %q", got, want)
	}
}

// TestPL004_AllPathsRootedUnderHarmonik verifies that every helper in the
// PL-004 path surface returns a path whose prefix is HarmonikDir. This
// ensures no path escapes the per-project .harmonik/ boundary.
//
// Spec ref: process-lifecycle.md §4.1 PL-004 — "The daemon MUST NOT read or
// write harmonik-owned state outside this surface."
func TestPL004_AllPathsRootedUnderHarmonik(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	harmonikDir := HarmonikDir(projectDir)

	paths := []struct {
		name string
		path string
	}{
		{"PidfilePath", PidfilePath(projectDir)},
		{"SocketPath", SocketPath(projectDir)},
		{"InstanceIDPath", InstanceIDPath(projectDir)},
		{"UpgradingMarkerPath", UpgradingMarkerPath(projectDir)},
		{"StateMarkerPath", StateMarkerPath(projectDir)},
		{"EventIDHWMPath", EventIDHWMPath(projectDir)},
		{"EventsDir", EventsDir(projectDir)},
		{"BeadsIntentsDir", BeadsIntentsDir(projectDir)},
		{"BeadsOwnedDir", BeadsOwnedDir(projectDir)},
		{"ReconciliationLocksDir", ReconciliationLocksDir(projectDir)},
		{"ReconciliationLockPath", ReconciliationLockPath(projectDir, "some-run")},
		{"SpillFilePath", SpillFilePath(projectDir, "consumer")},
		{"ReconciliationDir", ReconciliationDir(projectDir)},
		{"InvestigatorEvidenceDir", InvestigatorEvidenceDir(projectDir, "inv-run-id")},
		{"WIPCaptureDir", WIPCaptureDir(projectDir, "inv-run-id")},
	}

	for _, tc := range paths {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !strings.HasPrefix(tc.path, harmonikDir) {
				t.Errorf("PL-004 %s: path %q is not rooted under HarmonikDir %q",
					tc.name, tc.path, harmonikDir)
			}
		})
	}
}

// TestRC019_ReconciliationDir verifies the canonical reconciliation/ evidence
// root path under .harmonik/.
//
// Spec ref: reconciliation/spec.md §4.4 RC-019; §4.5 RC-022.
func TestRC019_ReconciliationDir(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	got := ReconciliationDir(projectDir)
	want := filepath.Join(projectDir, ".harmonik", "reconciliation")
	if got != want {
		t.Errorf("ReconciliationDir: got %q, want %q", got, want)
	}
}

// TestRC019_InvestigatorEvidenceDir verifies the per-investigator evidence
// directory path under .harmonik/reconciliation/<investigatorRunID>/.
//
// Spec ref: reconciliation/spec.md §4.4 RC-019; §4.5 RC-022.
func TestRC019_InvestigatorEvidenceDir(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	const invRunID = "01900000-0000-7000-0000-000000000042"
	got := InvestigatorEvidenceDir(projectDir, invRunID)
	want := filepath.Join(projectDir, ".harmonik", "reconciliation", invRunID)
	if got != want {
		t.Errorf("InvestigatorEvidenceDir: got %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, ReconciliationDir(projectDir)) {
		t.Errorf("InvestigatorEvidenceDir: path %q not under ReconciliationDir", got)
	}
}

// TestRC019_WIPCaptureDir verifies the canonical WIP capture path:
// .harmonik/reconciliation/<investigatorRunID>/wip-capture/
//
// Spec ref: reconciliation/spec.md §4.4 RC-019.
func TestRC019_WIPCaptureDir(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	const invRunID = "01900000-0000-7000-0000-000000000042"
	got := WIPCaptureDir(projectDir, invRunID)
	want := filepath.Join(projectDir, ".harmonik", "reconciliation", invRunID, "wip-capture")
	if got != want {
		t.Errorf("WIPCaptureDir: got %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, "wip-capture") {
		t.Errorf("WIPCaptureDir: path %q must end with 'wip-capture'", got)
	}
	if !strings.HasPrefix(got, InvestigatorEvidenceDir(projectDir, invRunID)) {
		t.Errorf("WIPCaptureDir: path %q not under InvestigatorEvidenceDir", got)
	}
}
