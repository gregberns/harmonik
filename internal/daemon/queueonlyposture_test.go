package daemon

// queueonlyposture_test.go — the queue-only run posture.
//
// The posture is the `subsystems:` block in scripts/scratch-config-overlay.yaml.
// scratch-daemon.sh `init` appends that overlay onto the config `harmonik init`
// generates, so the overlay — not any string in this file — is what a scratch
// daemon actually boots with. Every test here therefore READS THE TRACKED OVERLAY
// off disk. Restating the block in Go would make these tests pass while the file
// the daemon reads said something else, which is the failure mode they exist to
// prevent.
//
// What each test defends:
//
//   - PostureParses: the overlay names only subsystems the schema knows. An
//     unknown name under `subsystems:` is ErrUnknownSubsystem and the daemon
//     REFUSES TO BOOT, so a name written ahead of its switch (supervisor_watchdog
//     is the live example) turns the assessment pass into a dead daemon.
//   - QueueSurfaceStaysOn: socket_listener and subscribe_hub are ON. The queue
//     handler adapter is built inside daemon.bindSocket, and `scratch-daemon.sh
//     batch` awaits terminal events over `harmonik subscribe`. Switching either
//     off does not give "queue only", it removes the queue or the instrument that
//     reads it.
//   - NonQueueSubsystemsAreAbsent: the switched-off subsystems are ABSENT at their
//     CONSTRUCTION SEAMS, not merely inert. This is the claim with teeth: it calls
//     the real constructors with the real overlay config and requires nil / false.
//   - OverlayKeysAreStrippable: every top-level key the overlay owns appears in the
//     strip list inside scripts/scratch-daemon.sh. A key added to the overlay but
//     missing there lands as a SECOND top-level key of the same name, which is a
//     duplicate-key YAML error and again a dead daemon.
//
// Helper prefix: qpost.

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/workers"
)

// qpostOverlayMarker separates the overlay's explanatory header from the YAML
// scratch-daemon.sh appends. Only the part after it reaches a generated config.
const qpostOverlayMarker = "# ---8<--- everything below this marker"

// qpostOverlayPath is the tracked overlay, relative to this package directory.
const qpostOverlayPath = "../../scripts/scratch-config-overlay.yaml"

// qpostScriptPath is the script that appends the overlay onto a generated config.
const qpostScriptPath = "../../scripts/scratch-daemon.sh"

// qpostSubsystemsOff is the set of subsystems the queue-only posture switches off.
// It is the EXPECTATION the overlay is checked against, so an edit to the overlay
// that changes the posture has to be a deliberate edit here too.
var qpostSubsystemsOff = []projectconfig.SubsystemName{
	projectconfig.SubsystemReconciliationScheduler,
	projectconfig.SubsystemDashboardGate,
	projectconfig.SubsystemMovementGovernor,
	projectconfig.SubsystemCrewIdleReap,
	projectconfig.SubsystemBandwidthTuner,
	projectconfig.SubsystemBranchReaper,
	projectconfig.SubsystemWorkerReportLoop,
	projectconfig.SubsystemHandlerPausePolicy,
	projectconfig.SubsystemDaemonSpendMeter,
	projectconfig.SubsystemReviewGateAnomaly,
	projectconfig.SubsystemLedgerImportRecovery,
}

// qpostSubsystemsOn is the set the posture leaves ON. Both halves are named so a
// posture that switched EVERYTHING off — which removes the queue — cannot pass.
var qpostSubsystemsOn = []projectconfig.SubsystemName{
	projectconfig.SubsystemSocketListener,
	projectconfig.SubsystemSubscribeHub,
}

// qpostOverlayBody returns the ACTIVE section of the tracked overlay — the exact
// text scratch-daemon.sh appends onto a generated .harmonik/config.yaml.
func qpostOverlayBody(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(qpostOverlayPath)
	if err != nil {
		t.Fatalf("qpostOverlayBody: read %s: %v", qpostOverlayPath, err)
	}
	_, body, found := strings.Cut(string(raw), qpostOverlayMarker)
	if !found {
		t.Fatalf("qpostOverlayBody: %s has no %q marker; scratch-daemon.sh appends nothing without it",
			qpostOverlayPath, qpostOverlayMarker)
	}
	// Cut leaves the remainder of the marker line; drop it.
	_, body, _ = strings.Cut(body, "\n")
	return body
}

// qpostLoadConfig writes the overlay's active section into a real
// .harmonik/config.yaml and loads it through projectconfig.LoadProjectConfig.
//
// The schema_version line mirrors what `harmonik init` writes above the appended
// section, so the file under test has the same shape as the generated one.
func qpostLoadConfig(t *testing.T) (projectconfig.ProjectConfig, string) {
	t.Helper()
	return subpartLoadConfig(t, "schema_version: 1\nversion: 1\n"+qpostOverlayBody(t))
}

// The overlay names only subsystems the schema knows, and the names it switches
// off are exactly the posture. A name outside knownSubsystems is a hard error at
// load, so this test is what stops an unbootable overlay reaching an assessor.
func TestQueueOnlyPosture_PostureParses(t *testing.T) {
	t.Parallel()

	pc, _ := qpostLoadConfig(t)

	for _, name := range qpostSubsystemsOff {
		if pc.Subsystems.Enabled(name) {
			t.Errorf("subsystem %q is ENABLED under the queue-only overlay; the posture switches it off", name)
		}
	}
	for _, name := range qpostSubsystemsOn {
		if !pc.Subsystems.Enabled(name) {
			t.Errorf("subsystem %q is DISABLED under the queue-only overlay; the posture keeps it on", name)
		}
	}

	// The two switches that are not `subsystems:` entries but are part of the same
	// posture. The ctx-watchdog has its own gate; the ops-monitor has none, and a
	// stretched interval is the whole mitigation.
	if pc.Watchdog.Enabled {
		t.Error("watchdog.enabled is true under the queue-only overlay; the ctx-watchdog schedule must not be registered")
	}
	if pc.Opsmonitor.Interval == "" || pc.Opsmonitor.Interval == "5m" {
		t.Errorf("opsmonitor.interval = %q; the posture stretches the 5m default because the schedule has no enable switch", pc.Opsmonitor.Interval)
	}
}

// The queue's own surface stays on. socket_listener gates daemon.buildQueueHandler,
// which builds the queue handler adapter every `harmonik queue` verb talks to;
// subscribe_hub gates the `subscribe` op that scratch-daemon.sh batch waits on.
//
// Both are asserted through the SAME expression the construction seams evaluate
// (bootState.socketListenerEnabled, and the Enabled read in
// wireSpendAndQueueConsumers). Calling bindSocket itself would bind a real Unix
// socket and build a dozen collaborators, which tests binding rather than the gate.
func TestQueueOnlyPosture_QueueSurfaceStaysOn(t *testing.T) {
	t.Parallel()

	pc, root := qpostLoadConfig(t)
	bs := &bootState{cfg: Config{ProjectDir: root, ProjectCfg: pc, LogWriter: &sockpartSyncBuffer{}}}

	if !bs.socketListenerEnabled() {
		t.Fatal("socketListenerEnabled = false under the queue-only overlay; the queue handler adapter is built inside bindSocket, so this removes the queue instead of isolating it")
	}
	if !pc.Subsystems.Enabled(projectconfig.SubsystemSubscribeHub) {
		t.Fatal("subscribe_hub is off under the queue-only overlay; scratch-daemon.sh batch awaits terminal events over `harmonik subscribe`, and a refused subscription reads as an empty pass")
	}
}

// The switched-off subsystems are ABSENT at their construction seams. Each check
// calls the real constructor with the real overlay config and requires nil or
// false — a constructed-but-inert object fails here, which is the point.
func TestQueueOnlyPosture_NonQueueSubsystemsAreAbsent(t *testing.T) {
	t.Parallel()

	pc, root := qpostLoadConfig(t)
	logBuf := &sockpartSyncBuffer{}
	cfg := Config{ProjectDir: root, ProjectCfg: pc, LogWriter: logBuf}
	bs := &bootState{cfg: cfg}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if got := bs.newCrewIdleReaperIfEnabled(); got != nil {
		t.Error("newCrewIdleReaperIfEnabled returned a reaper under the queue-only overlay; crew_idle_reap must be absent")
	}
	if got := bs.newBranchReapWatcherIfEnabled(); got != nil {
		t.Error("newBranchReapWatcherIfEnabled returned a watcher under the queue-only overlay; branch_reaper must be absent (it DELETES branches the assessor needs)")
	}
	// A worker is CONFIGURED first. RunReportLoop returns immediately with an empty
	// registry, so without this the switch and an empty registry look the same.
	bs.cfg.Workers = bootpartWorkerConfig()
	reg := workers.BuildRegistryWithRunner(ctx, bs.cfg.Workers, nil, nil)
	if reg == nil {
		t.Fatal("BuildRegistryWithRunner returned nil for a configured worker; the loop would self-disable and the switch would prove nothing")
	}
	if bs.startWorkerReportLoopIfEnabled(ctx, reg) {
		t.Error("startWorkerReportLoopIfEnabled = true under the queue-only overlay; worker_report_loop must be absent")
	}
	if bs.startBandwidthTunerIfEnabled(ctx) {
		t.Error("startBandwidthTunerIfEnabled = true under the queue-only overlay; bandwidth_tuner must be absent")
	}
	if got := newDashboardGateIfEnabled(pc, logBuf); got != nil {
		t.Error("newDashboardGateIfEnabled returned a gate under the queue-only overlay; dashboard_gate must be absent (it makes the dispatch loop read the captain's lanes.json)")
	}
	if startReconciliationSchedulerIfEnabled(ctx, pc, ReconciliationSchedulerConfig{
		ProjectDir: root,
		Interval:   time.Hour,
		LogWriter:  logBuf,
	}) {
		t.Error("startReconciliationSchedulerIfEnabled = true under the queue-only overlay; reconciliation_scheduler must be absent")
	}
	if _, governorEnabled, err := newGovernorPort(cfg, time.Now()); err != nil {
		t.Errorf("newGovernorPort returned an error under the queue-only overlay: %v", err)
	} else if governorEnabled {
		t.Error("newGovernorPort reported the governor enabled under the queue-only overlay; movement_governor must be absent")
	}

	// The seams that only exist inside bus wiring are asserted through the same
	// Enabled read wireSpendAndQueueConsumers and wireWatchersAndObservers use.
	for _, name := range []projectconfig.SubsystemName{
		projectconfig.SubsystemHandlerPausePolicy,
		projectconfig.SubsystemDaemonSpendMeter,
		projectconfig.SubsystemReviewGateAnomaly,
		projectconfig.SubsystemLedgerImportRecovery,
	} {
		if pc.Subsystems.Enabled(name) {
			t.Errorf("bus consumer %q is enabled under the queue-only overlay; the posture switches it off", name)
		}
	}

	// Each partition announces itself. A silent partition cannot be told apart from
	// a config that never took effect, which is how an operator ends up believing a
	// subsystem was switched off while it kept running.
	logged := logBuf.String()
	for _, name := range []projectconfig.SubsystemName{
		projectconfig.SubsystemCrewIdleReap,
		projectconfig.SubsystemBranchReaper,
		projectconfig.SubsystemWorkerReportLoop,
		projectconfig.SubsystemBandwidthTuner,
		projectconfig.SubsystemDashboardGate,
		projectconfig.SubsystemReconciliationScheduler,
	} {
		if !strings.Contains(logged, string(name)) {
			t.Errorf("subsystem %q was partitioned away without saying so on the daemon log; log = %q", name, logged)
		}
	}
}

// qpostStripListPattern finds the top-level keys scratch-daemon.sh strips before
// it appends the overlay. The script writes them as one alternation, e.g.
// /^(harnesses|codex|subsystems):[[:space:]]*$/.
var qpostStripListPattern = regexp.MustCompile(`\^\(([a-z_|]+)\):\[\[:space:\]\]\*\$`)

// qpostTopLevelKeyPattern finds a bare top-level YAML key at column zero.
var qpostTopLevelKeyPattern = regexp.MustCompile(`(?m)^([a-z_]+):`)

// Every top-level key the overlay owns is stripped from the generated config
// before the overlay is appended. Without that, `harmonik init` writing its own
// copy of the key leaves TWO top-level keys of the same name — a duplicate-key
// YAML error, and a daemon that will not boot. This has already happened once for
// `harnesses:`, so the guard is against a repeat, not a hypothetical.
func TestQueueOnlyPosture_OverlayKeysAreStrippable(t *testing.T) {
	t.Parallel()

	script, err := os.ReadFile(qpostScriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Clean(qpostScriptPath), err)
	}
	match := qpostStripListPattern.FindSubmatch(script)
	if match == nil {
		t.Fatalf("no top-level strip list found in %s; provision_matrix_config must strip the keys the overlay owns", qpostScriptPath)
	}
	stripList := string(match[1])
	stripped := make(map[string]struct{})
	for _, key := range strings.Split(stripList, "|") {
		stripped[key] = struct{}{}
	}

	for _, m := range qpostTopLevelKeyPattern.FindAllStringSubmatch(qpostOverlayBody(t), -1) {
		if _, ok := stripped[m[1]]; !ok {
			t.Errorf("overlay top-level key %q is not in scratch-daemon.sh's strip list %q; appending it beside an init-written copy is a duplicate-key YAML error", m[1], stripList)
		}
	}
}
