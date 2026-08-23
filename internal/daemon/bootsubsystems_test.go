package daemon

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/workers"
)

const (
	bootpartBranchReaperFrame = "internal/daemon.(*BranchReapWatcher).loop"
	bootpartTunerFrame        = "internal/daemon.(*BandwidthTuner).Run"
	bootpartReportLoopFrame   = "internal/workers.RunReportLoop"
)

const bootpartNoWaitForAbsence = 300 * time.Millisecond

func bootpartCountFrame(frame string) int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return strings.Count(string(buf[:n]), frame)
		}
		buf = make([]byte, 2*len(buf))
	}
}

func bootpartFrameAppeared(frame string, baseline int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if bootpartCountFrame(frame) > baseline {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func bootpartBootState(t *testing.T, yamlContent string) (*bootState, *sockpartSyncBuffer) {
	t.Helper()
	pc, root := subpartLoadConfig(t, yamlContent)
	logBuf := &sockpartSyncBuffer{}
	return &bootState{cfg: Config{ProjectDir: root, ProjectCfg: pc, LogWriter: logBuf}}, logBuf
}

func bootpartDisabledYAML(name projectconfig.SubsystemName) string {
	return "schema_version: 1\nsubsystems:\n  " + string(name) + ":\n    enabled: false\n"
}

// Default state: no subsystems: block → the reaper is constructed, exactly as
// before partitioning existed.
func TestSubsystemPartition_CrewIdleReap_DefaultConstructs(t *testing.T) {
	t.Parallel()

	bs, logBuf := bootpartBootState(t, "schema_version: 1\n")
	if got := bs.newCrewIdleReaperIfEnabled(); got == nil {
		t.Fatal("newCrewIdleReaperIfEnabled = nil with no subsystems: block; absent config must not disable anything")
	}
	if strings.Contains(logBuf.String(), string(projectconfig.SubsystemCrewIdleReap)) {
		t.Errorf("the crew-idle-reap partition was announced with no subsystems: block; log = %q", logBuf.String())
	}
}

// Disabled state: the reaper is ABSENT — no object at all, so startBackgroundLoops
// has nothing to start. Object absence IS the runtime difference here: the sweep
// body is already inert, which is precisely why leaving the object constructed
// buys nothing and costs a live field on the composition root.
func TestSubsystemPartition_CrewIdleReap_DisabledIsAbsent(t *testing.T) {
	t.Parallel()

	bs, logBuf := bootpartBootState(t, bootpartDisabledYAML(projectconfig.SubsystemCrewIdleReap))
	if got := bs.newCrewIdleReaperIfEnabled(); got != nil {
		t.Fatal("newCrewIdleReaperIfEnabled returned a reaper with subsystems.crew_idle_reap.enabled: false; off means NEVER CONSTRUCTED, not constructed-and-inert")
	}
	if !strings.Contains(logBuf.String(), "crew idle reaper not constructed") {
		t.Errorf("the daemon did not report the crew-idle-reap partition; a silent partition is indistinguishable from a config that did not take effect. log = %q", logBuf.String())
	}
}

// Default state: the watcher is constructed AND its loop goroutine is running.
// The goroutine half is what keeps the disabled test below from being vacuous.
func TestSubsystemPartition_BranchReaper_DefaultConstructsAndRuns(t *testing.T) {
	bs, logBuf := bootpartBootState(t, "schema_version: 1\n")

	baseline := bootpartCountFrame(bootpartBranchReaperFrame)
	w := bs.newBranchReapWatcherIfEnabled()
	if w == nil {
		t.Fatal("newBranchReapWatcherIfEnabled = nil with no subsystems: block; absent config must not disable anything")
	}
	if strings.Contains(logBuf.String(), string(projectconfig.SubsystemBranchReaper)) {
		t.Errorf("the branch-reaper partition was announced with no subsystems: block; log = %q", logBuf.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.StartWatcher(ctx)
	if !bootpartFrameAppeared(bootpartBranchReaperFrame, baseline, 2*time.Second) {
		t.Fatal("no goroutine running BranchReapWatcher.loop after StartWatcher; the absent-state test proves nothing unless the present state is observable the same way")
	}
}

// Disabled state: the watcher is ABSENT — no object, and consequently no loop
// goroutine can ever exist, however long the test waits.
func TestSubsystemPartition_BranchReaper_DisabledIsAbsent(t *testing.T) {
	bs, logBuf := bootpartBootState(t, bootpartDisabledYAML(projectconfig.SubsystemBranchReaper))

	baseline := bootpartCountFrame(bootpartBranchReaperFrame)
	if w := bs.newBranchReapWatcherIfEnabled(); w != nil {
		t.Fatal("newBranchReapWatcherIfEnabled returned a watcher with subsystems.branch_reaper.enabled: false; off means NEVER CONSTRUCTED")
	}
	if !strings.Contains(logBuf.String(), "branch reaper not constructed") {
		t.Errorf("the daemon did not report the branch-reaper partition; log = %q", logBuf.String())
	}
	if bootpartFrameAppeared(bootpartBranchReaperFrame, baseline, bootpartNoWaitForAbsence) {
		t.Error("a BranchReapWatcher.loop goroutine appeared with the subsystem switched off; the reaper DELETES BRANCHES, so inert-but-present is not an acceptable off state")
	}
}

func bootpartTunerBootState(t *testing.T, yamlContent string) (*bootState, *sockpartSyncBuffer) {
	t.Helper()
	bs, logBuf := bootpartBootState(t, yamlContent)
	bs.cfg.MaxConcurrent = 2
	bs.cfg.SubscriptionTokenCeiling = 1_000_000
	bs.concurrencyCtrl = NewConcurrencyController(bs.cfg.MaxConcurrent)
	bs.pollGate = &PollGate{}
	bs.pollGate.SetInactive(true)
	bs.tunerBackstop = &bandwidthTunerBackstop{}
	return bs, logBuf
}

// Default state: with a token ceiling configured the tuner is constructed and its
// Run goroutine is live, exactly as before partitioning existed.
func TestSubsystemPartition_BandwidthTuner_DefaultRuns(t *testing.T) {
	bs, logBuf := bootpartTunerBootState(t, "schema_version: 1\n")

	baseline := bootpartCountFrame(bootpartTunerFrame)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !bs.startBandwidthTunerIfEnabled(ctx) {
		t.Fatal("startBandwidthTunerIfEnabled = false with no subsystems: block and a positive token ceiling; absent config must not disable anything")
	}
	if strings.Contains(logBuf.String(), string(projectconfig.SubsystemBandwidthTuner)) {
		t.Errorf("the bandwidth-tuner partition was announced with no subsystems: block; log = %q", logBuf.String())
	}
	if !bootpartFrameAppeared(bootpartTunerFrame, baseline, 2*time.Second) {
		t.Fatal("no goroutine running BandwidthTuner.Run after the tuner was reported started")
	}
	if bs.tunerBackstop.tuner.Load() == nil {
		t.Error("the pre-Seal backstop was never armed with the tuner; rate-limit events would go nowhere")
	}
}

// Disabled state: neither half of the subsystem exists — no tuner object, no Run
// goroutine, and no backstop subscription on the event bus.
func TestSubsystemPartition_BandwidthTuner_DisabledIsAbsent(t *testing.T) {
	bs, logBuf := bootpartTunerBootState(t, bootpartDisabledYAML(projectconfig.SubsystemBandwidthTuner))

	baseline := bootpartCountFrame(bootpartTunerFrame)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if bs.startBandwidthTunerIfEnabled(ctx) {
		t.Fatal("startBandwidthTunerIfEnabled = true with subsystems.bandwidth_tuner.enabled: false; the tuner must be ABSENT")
	}
	if !strings.Contains(logBuf.String(), "bandwidth tuner and its rate-limit backstop not constructed") {
		t.Errorf("the daemon did not report the bandwidth-tuner partition; log = %q", logBuf.String())
	}
	if bootpartFrameAppeared(bootpartTunerFrame, baseline, bootpartNoWaitForAbsence) {
		t.Error("a BandwidthTuner.Run goroutine appeared with the subsystem switched off")
	}
	if bs.tunerBackstop.tuner.Load() != nil {
		t.Error("the backstop was armed with a tuner behind the disabled switch")
	}
	if bs.bandwidthTunerEnabled() {
		t.Error("bandwidthTunerEnabled = true with the subsystem switched off; the two construction sites would then disagree")
	}
}

// The gate short-circuits BEFORE touching anything: a bootState with every tuner
// dependency nil survives it. Any construction that leaked past the switch would
// dereference one of those nils — SetTuner on a nil backstop panics — and take
// the test binary down here rather than fail one assertion.
func TestSubsystemPartition_BandwidthTuner_DisabledGateTouchesNothing(t *testing.T) {
	t.Parallel()

	bs, _ := bootpartBootState(t, bootpartDisabledYAML(projectconfig.SubsystemBandwidthTuner))
	bs.cfg.SubscriptionTokenCeiling = 1_000_000 // the pre-existing gate would let it through

	if bs.startBandwidthTunerIfEnabled(context.Background()) {
		t.Fatal("startBandwidthTunerIfEnabled = true behind the disabled switch")
	}
}

func bootpartWorkerConfig() workers.Config {
	return workers.Config{
		Version: 1,
		Workers: []workers.Worker{{
			Name:      "bootpart-worker",
			Transport: "ssh",
			Host:      "bootpart.invalid",
			OS:        "darwin",
			RepoPath:  "/tmp/bootpart",
			MaxSlots:  1,
			Enabled:   true,
		}},
	}
}

// Default state: the poll goroutine is spawned and parks on its ticker.
func TestSubsystemPartition_WorkerReportLoop_DefaultRuns(t *testing.T) {
	bs, logBuf := bootpartBootState(t, "schema_version: 1\n")
	bs.cfg.Workers = bootpartWorkerConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reg := workers.BuildRegistryWithRunner(ctx, bs.cfg.Workers, nil, nil)
	if reg == nil {
		t.Fatal("BuildRegistryWithRunner returned nil for a configured worker; the loop would self-disable and prove nothing")
	}

	baseline := bootpartCountFrame(bootpartReportLoopFrame)
	if !bs.startWorkerReportLoopIfEnabled(ctx, reg) {
		t.Fatal("startWorkerReportLoopIfEnabled = false with no subsystems: block; absent config must not disable anything")
	}
	if strings.Contains(logBuf.String(), string(projectconfig.SubsystemWorkerReportLoop)) {
		t.Errorf("the worker-report partition was announced with no subsystems: block; log = %q", logBuf.String())
	}
	if !bootpartFrameAppeared(bootpartReportLoopFrame, baseline, 2*time.Second) {
		t.Fatal("no goroutine running workers.RunReportLoop after it was reported started")
	}
}

// Disabled state: the goroutine is never spawned. RunReportLoop would have
// returned immediately on its own with no worker enabled — so this test enables
// one, making the difference the SWITCH rather than the empty registry.
func TestSubsystemPartition_WorkerReportLoop_DisabledIsAbsent(t *testing.T) {
	bs, logBuf := bootpartBootState(t, bootpartDisabledYAML(projectconfig.SubsystemWorkerReportLoop))
	bs.cfg.Workers = bootpartWorkerConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reg := workers.BuildRegistryWithRunner(ctx, bs.cfg.Workers, nil, nil)

	baseline := bootpartCountFrame(bootpartReportLoopFrame)
	if bs.startWorkerReportLoopIfEnabled(ctx, reg) {
		t.Fatal("startWorkerReportLoopIfEnabled = true with subsystems.worker_report_loop.enabled: false; the loop must be ABSENT")
	}
	if !strings.Contains(logBuf.String(), "worker-report poll loop not constructed") {
		t.Errorf("the daemon did not report the worker-report partition; log = %q", logBuf.String())
	}
	if bootpartFrameAppeared(bootpartReportLoopFrame, baseline, bootpartNoWaitForAbsence) {
		t.Error("a workers.RunReportLoop goroutine appeared with the subsystem switched off")
	}
}

const bootpartAllDisabledYAML = sockpartBaseConfigYAML + `
subsystems:
  crew_idle_reap:
    enabled: false
  bandwidth_tuner:
    enabled: false
  branch_reaper:
    enabled: false
  worker_report_loop:
    enabled: false
`

// The switches compose, and the daemon still reaches its work loop with all of
// them off. This is the assertion that the nil bootState fields they leave behind
// are actually guarded: startBackgroundLoops calls StartWatcher on both reapers,
// and an unguarded nil branch reaper panics inside a goroutine — taking the whole
// test binary down rather than failing one test.
func TestSubsystemPartition_BootSubsystems_DisabledDaemonStillReachesWorkLoop(t *testing.T) {
	t.Setenv("HARMONIK_DEBUG_WIRING", "1")

	projectDir, jsonlPath := sockpartProjectDir(t, bootpartAllDisabledYAML)
	logs := sockpartRunDaemon(t, projectDir, jsonlPath)

	for _, want := range []string{
		"crew idle reaper not constructed",
		"branch reaper not constructed",
		"bandwidth tuner and its rate-limit backstop not constructed",
		"worker-report poll loop not constructed",
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("boot log never reported %q; a silent partition is indistinguishable from a config that did not take effect", want)
		}
	}
	if !strings.Contains(logs, "composition-root wiring audit") {
		t.Error("daemon did not reach startBackgroundLoops with these subsystems switched off; the core must run without them")
	}
	if strings.Contains(logs, "socket listener and its handler subtree not constructed") {
		t.Error("the socket-listener partition fired; this fixture only switches off the background subsystems above")
	}
}
