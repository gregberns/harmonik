package daemon

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

const qpostOverlayMarker = "# ---8<--- everything below this marker"

const qpostOverlayPath = "../../scripts/scratch-config-overlay.yaml"

const qpostScriptPath = "../../scripts/scratch-daemon.sh"

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
	projectconfig.SubsystemSupervisorWatchdog,
}

var qpostSubsystemsOn = []projectconfig.SubsystemName{
	projectconfig.SubsystemSocketListener,
	projectconfig.SubsystemSubscribeHub,
}

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
	_, body, _ = strings.Cut(body, "\n")
	return body
}

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

var qpostKnownListPattern = regexp.MustCompile(`\(known: ([^)]+)\)`)

func qpostKnownSubsystems(t *testing.T) []string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".harmonik"), 0o750); err != nil {
		t.Fatalf("qpostKnownSubsystems: MkdirAll: %v", err)
	}
	bogus := "schema_version: 1\nsubsystems:\n  qpost_not_a_subsystem:\n    enabled: false\n"
	if err := os.WriteFile(filepath.Join(root, ".harmonik", "config.yaml"), []byte(bogus), 0o600); err != nil {
		t.Fatalf("qpostKnownSubsystems: WriteFile: %v", err)
	}
	_, err := projectconfig.LoadProjectConfig(root)
	if err == nil {
		t.Fatal("qpostKnownSubsystems: an unknown subsystem name loaded without error; unknown names must be a hard error")
	}
	m := qpostKnownListPattern.FindStringSubmatch(err.Error())
	if m == nil {
		t.Fatalf("qpostKnownSubsystems: no %q list in the unknown-subsystem error; error = %v", "known:", err)
	}
	names := strings.Split(m[1], ", ")
	for i := range names {
		names[i] = strings.TrimSpace(names[i])
	}
	return names
}

// The posture has an opinion about EVERY switchable subsystem. A subsystem added
// to the schema after this posture was written defaults to ON, so it would join
// the assessor's "queue only" pass without anyone deciding that it should. This
// test is what forces that decision, by failing until the new name is placed in
// one of the two lists above.
func TestQueueOnlyPosture_CoversEverySwitchableSubsystem(t *testing.T) {
	t.Parallel()

	decided := make(map[string]struct{})
	for _, name := range qpostSubsystemsOff {
		decided[string(name)] = struct{}{}
	}
	for _, name := range qpostSubsystemsOn {
		decided[string(name)] = struct{}{}
	}

	for _, name := range qpostKnownSubsystems(t) {
		if _, ok := decided[name]; !ok {
			t.Errorf("subsystem %q has no place in the queue-only posture; it therefore defaults to ON. Decide: add it to scripts/scratch-config-overlay.yaml and qpostSubsystemsOff, or to qpostSubsystemsOn with the reason it stays", name)
		}
	}
}

var qpostStripListPattern = regexp.MustCompile(`\^\(([a-z_|]+)\):\[\[:space:\]\]\*\$`)

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
