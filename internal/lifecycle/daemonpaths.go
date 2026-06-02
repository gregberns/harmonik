package lifecycle

import (
	"fmt"
	"path/filepath"
)

// daemonpaths.go — per-project file surface constants for the daemon.
//
// PL-004 enumerates every file or directory under .harmonik/ that the daemon
// reads or writes. All path helpers in the lifecycle package MUST be derived
// from the constants in this file. The daemon MUST NOT access harmonik-owned
// state outside this surface.
//
// Spec ref: process-lifecycle.md §4.1 PL-004 — "Daemon owns per-project files
// under .harmonik/".

// harmonikSubdir is the fixed name of the per-project state directory.
const harmonikSubdir = ".harmonik"

// Per-project file paths (relative to the .harmonik/ directory).
const (
	// pidfileRelPath is the relative path of the per-project daemon PID file
	// within the .harmonik directory.
	//
	// Spec ref: process-lifecycle.md §4.1 PL-002, PL-002a, PL-002b.
	pidfileRelPath = "daemon.pid"

	// socketRelPath is the relative path of the Unix-domain socket.
	//
	// Spec ref: process-lifecycle.md §4.1 PL-003, PL-003a, PL-003b.
	socketRelPath = "daemon.sock"

	// instanceIDRelPath is the relative path of the per-process daemon_instance_id
	// (UUIDv7) written at PL-005 step 0.
	//
	// Spec ref: process-lifecycle.md §4.2 PL-005 step 0.
	instanceIDRelPath = "daemon.instance-id"

	// upgradingRelPath is the relative path of the durable upgrade-intent
	// marker written by PL-027(iv) before execve.
	//
	// Spec ref: process-lifecycle.md §4.9 PL-027(iv);
	//           operator-nfr.md §4.6 ON-020a.
	upgradingRelPath = "daemon.upgrading"

	// stateRelPath is the relative path of the pause-state durable marker.
	// Content is owned by ON-030a; PL reads this at PL-005 step 8a.
	//
	// Spec ref: operator-nfr.md §4.7 ON-030a; process-lifecycle.md §4.2 PL-005
	//           step 8a.
	stateRelPath = "daemon.state"

	// eventIDHWMRelPath is the relative path of the event-ID high-water-mark
	// file within the .harmonik directory.
	//
	// Spec ref: event-model.md §4.1.
	eventIDHWMRelPath = "event_id_hwm"

	// eventsSubdir is the subdirectory under .harmonik/ holding the JSONL event
	// log, dead-letter log, and per-consumer spill files.
	//
	// Spec ref: event-model.md §6.2.
	eventsSubdir = "events"

	// beadsIntentsSubdir is the subdirectory under .harmonik/ holding the
	// per-operation intent files written by the Beads CLI adapter.
	//
	// Spec ref: beads-integration.md §4.10 BI-030; beads-integration.md §6.2.
	beadsIntentsSubdir = "beads-intents"

	// reconciliationLocksSubdir is the subdirectory under .harmonik/ holding
	// per-target-run reconciliation lock files. Written by the reconciliation
	// manager (RC-002a); swept by the orphan sweep (PL-006).
	//
	// Spec ref: reconciliation/spec.md §4.1 RC-002a.
	reconciliationLocksSubdir = "reconciliation-locks"

	// reconciliationSubdir is the subdirectory under .harmonik/ holding
	// per-investigator evidence directories. Each investigator run gets its own
	// subdirectory at .harmonik/reconciliation/<investigator_run_id>/.
	//
	// Spec ref: reconciliation/spec.md §4.4 RC-019; reconciliation/spec.md §4.5 RC-022.
	reconciliationSubdir = "reconciliation"

	// reconciliationAttemptsSubdir is the subdirectory under .harmonik/ holding
	// per-target-run Cat 3b retry attempt counter files. Each target run's
	// counter is stored at .harmonik/reconciliation-attempts/<target_run_id>.json,
	// written atomically per WM-026 by the lifecycle I/O layer.
	//
	// Spec ref: reconciliation/spec.md §4.5 RC-026a.
	reconciliationAttemptsSubdir = "reconciliation-attempts"

	// beadsOwnedSubdir is the subdirectory under .harmonik/ holding per-bead
	// ownership sentinel files. A file at .harmonik/beads-owned/<bead-id>
	// records that THIS project's daemon has ever successfully claimed that bead.
	// The sentinel outlives the BI-030 claim intent file (which is deleted on
	// claim success) and provides an independent provenance signal for the PL-006
	// sixth-bullet orphan sweep. Sentinel files are created on successful
	// ClaimBead and deleted on successful CloseBead, ReopenBead, or ResetBead.
	//
	// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet (provenance OR clause);
	// §4.4 PL-006a (project_hash discipline).
	// Bead ref: hk-11xkn (audit-log actor=project_hash provenance followup).
	beadsOwnedSubdir = "beads-owned"

	// wipCaptureSubdir is the leaf subdirectory name under an investigator's
	// evidence directory for WIP capture files.
	//
	// Spec ref: reconciliation/spec.md §4.4 RC-019.
	wipCaptureSubdir = "wip-capture"
)

// HarmonikDir returns the absolute .harmonik/ directory path for a project.
//
// Spec ref: process-lifecycle.md §4.1 PL-004.
func HarmonikDir(projectDir string) string {
	return filepath.Join(projectDir, harmonikSubdir)
}

// PidfilePath returns the absolute path of the daemon PID file for a project.
//
// Spec ref: process-lifecycle.md §4.1 PL-002.
func PidfilePath(projectDir string) string {
	return filepath.Join(HarmonikDir(projectDir), pidfileRelPath)
}

// SocketPath returns the absolute path of the daemon Unix socket for a project.
//
// Spec ref: process-lifecycle.md §4.1 PL-003.
func SocketPath(projectDir string) string {
	return filepath.Join(HarmonikDir(projectDir), socketRelPath)
}

// InstanceIDPath returns the absolute path of the daemon_instance_id file for
// a project.
//
// Spec ref: process-lifecycle.md §4.2 PL-005 step 0.
func InstanceIDPath(projectDir string) string {
	return filepath.Join(HarmonikDir(projectDir), instanceIDRelPath)
}

// UpgradingMarkerPath returns the absolute path of the daemon.upgrading marker
// for a project.
//
// Spec ref: process-lifecycle.md §4.9 PL-027(iv); operator-nfr.md §4.6 ON-020a.
func UpgradingMarkerPath(projectDir string) string {
	return filepath.Join(HarmonikDir(projectDir), upgradingRelPath)
}

// StateMarkerPath returns the absolute path of the daemon.state marker for a
// project.
//
// Spec ref: operator-nfr.md §4.7 ON-030a; process-lifecycle.md §4.2 PL-005
// step 8a.
func StateMarkerPath(projectDir string) string {
	return filepath.Join(HarmonikDir(projectDir), stateRelPath)
}

// EventIDHWMPath returns the absolute path of the event-ID high-water-mark
// file for a project.
//
// Spec ref: event-model.md §4.1.
func EventIDHWMPath(projectDir string) string {
	return filepath.Join(HarmonikDir(projectDir), eventIDHWMRelPath)
}

// EventsDir returns the absolute path of the events/ subdirectory for a project.
//
// Spec ref: event-model.md §6.2.
func EventsDir(projectDir string) string {
	return filepath.Join(HarmonikDir(projectDir), eventsSubdir)
}

// BeadsIntentsDir returns the absolute path of the beads-intents/ subdirectory
// for a project.
//
// Spec ref: beads-integration.md §4.10 BI-030.
func BeadsIntentsDir(projectDir string) string {
	return filepath.Join(HarmonikDir(projectDir), beadsIntentsSubdir)
}

// BeadsOwnedDir returns the absolute path of the beads-owned/ subdirectory for
// a project. Each file under this directory is a zero-byte ownership sentinel
// named by bead ID; its presence asserts that THIS project's daemon has
// successfully claimed the bead at least once.
//
// The sentinel outlives the BI-030 claim intent file (which is deleted after
// successful claim) and provides an independent provenance signal for the
// PL-006 sixth-bullet orphan sweep when all intent files have been cleared.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet; §4.4 PL-006a.
// Bead ref: hk-11xkn.
func BeadsOwnedDir(projectDir string) string {
	return filepath.Join(HarmonikDir(projectDir), beadsOwnedSubdir)
}

// ReconciliationLocksDir returns the absolute path of the reconciliation-locks/
// subdirectory for a project.
//
// Spec ref: reconciliation/spec.md §4.1 RC-002a.
func ReconciliationLocksDir(projectDir string) string {
	return filepath.Join(HarmonikDir(projectDir), reconciliationLocksSubdir)
}

// ReconciliationLockPath returns the absolute path of the per-run reconciliation
// lock file for a target run ID within the project.
//
// Spec ref: reconciliation/spec.md §4.1 RC-002a.
func ReconciliationLockPath(projectDir, runID string) string {
	return filepath.Join(ReconciliationLocksDir(projectDir), runID+".lock")
}

// SpillFilePath returns the absolute path of the per-consumer spill JSONL file
// for a project and consumer name.
//
// Spec ref: event-model.md §6.2; event-model.md §4.4 (EV-011a).
func SpillFilePath(projectDir, consumerName string) string {
	return filepath.Join(EventsDir(projectDir), fmt.Sprintf("spill-%s.jsonl", consumerName))
}

// ReconciliationDir returns the absolute path of the reconciliation/ evidence
// root under .harmonik/. Each investigator run writes its evidence under a
// per-run subdirectory of this root.
//
// Spec ref: reconciliation/spec.md §4.4 RC-019; §4.5 RC-022.
func ReconciliationDir(projectDir string) string {
	return filepath.Join(HarmonikDir(projectDir), reconciliationSubdir)
}

// InvestigatorEvidenceDir returns the absolute path of the per-investigator
// evidence directory: .harmonik/reconciliation/<investigatorRunID>/
//
// The verdict commit (RC-022) and any evidence files (including WIP capture)
// are committed from this directory.
//
// Spec ref: reconciliation/spec.md §4.4 RC-019; §4.5 RC-022.
func InvestigatorEvidenceDir(projectDir, investigatorRunID string) string {
	return filepath.Join(ReconciliationDir(projectDir), investigatorRunID)
}

// WIPCaptureDir returns the absolute path of the WIP capture subdirectory for
// a given investigator run: .harmonik/reconciliation/<investigatorRunID>/wip-capture/
//
// Before emitting a reopen-bead verdict the investigator MUST write the outer
// run's WIP (git status, diff, untracked file listing) into this directory so
// that the daemon's verdict commit preserves recoverable work.
//
// Spec ref: reconciliation/spec.md §4.4 RC-019.
func WIPCaptureDir(projectDir, investigatorRunID string) string {
	return filepath.Join(InvestigatorEvidenceDir(projectDir, investigatorRunID), wipCaptureSubdir)
}

// ReconciliationAttemptsDir returns the absolute path of the
// reconciliation-attempts/ subdirectory for a project.
//
// Each target run's Cat 3b retry counter file is stored under this directory
// at <targetRunID>.json. The files are written atomically per WM-026 by
// the lifecycle I/O layer (lifecycle/verdictretrycap_rc026a.go).
//
// Spec ref: reconciliation/spec.md §4.5 RC-026a.
func ReconciliationAttemptsDir(projectDir string) string {
	return filepath.Join(HarmonikDir(projectDir), reconciliationAttemptsSubdir)
}

// ReconciliationAttemptPath returns the absolute path of the per-run Cat 3b
// retry attempt counter file for a target run:
//
//	.harmonik/reconciliation-attempts/<targetRunID>.json
//
// The file is written atomically per WM-026 by WriteVerdictAttemptAtomic and
// read by ReadVerdictAttempt.
//
// Spec ref: reconciliation/spec.md §4.5 RC-026a.
func ReconciliationAttemptPath(projectDir, targetRunID string) string {
	return filepath.Join(ReconciliationAttemptsDir(projectDir), targetRunID+".json")
}
