package daemon

// bootsubsystems_test.go — subsystem partitioning of the background subsystems
// the daemon builds at boot: the crew idle reaper and the branch reaper.
//
// Both states are driven through the REAL config edge: a .harmonik/config.yaml
// written to disk and read by projectconfig.LoadProjectConfig. Nothing here
// hand-builds a SubsystemsConfig, because the thing under test is the whole path
// from operator YAML to construction seam.
//
// "Off" is asserted as ABSENT AT RUNTIME, not as a boolean. Where a subsystem is
// a goroutine, the assertion is that no goroutine carrying its loop frame
// exists — read out of a live runtime.Stack dump, not inferred. A
// constructed-but-inert watcher still parks a goroutine on a ticker and would
// fail these tests; an absent one has nothing to park.
//
// Counting is done as a DELTA against a baseline sampled immediately before the
// gate call, because the dump covers every goroutine in the test binary and other
// tests in this package boot daemons of their own.
//
// The frame-counting tests deliberately do NOT call t.Parallel(), and that
// omission is load-bearing rather than an oversight: the DefaultConstructsAndRuns
// case spawns exactly the goroutine the DisabledIsAbsent case waits to NOT see,
// so running the pair concurrently would produce a false failure. Adding
// t.Parallel() "for consistency" with the object-absence tests above them is the
// way to make this file flaky.
//
// The crew idle reaper is the exception, and the reason for its switch: its sweep
// body is already an operator-directed no-op (crewrun/idlereap.go StartWatcher
// launches nothing), so there is no goroutine to look for in EITHER state. Object
// absence is therefore the whole of the runtime difference — which is exactly the
// constructed-and-inert state the partition rule rejects.
//
// Helper prefix: bootpart — derived from what these helpers do, not from a bead
// or ticket id.

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/projectconfig"
)

// bootpartBranchReaperFrame is the goroutine frame the branch reaper's sweep
// parks on. Matching the frame rather than counting goroutines is what makes the
// assertion specific: it says "this subsystem's loop is/is not running", not
// "the goroutine count moved".
const bootpartBranchReaperFrame = "internal/daemon.(*BranchReapWatcher).loop"

// bootpartNoWaitForAbsence is how long an "absent" assertion waits before
// concluding a goroutine will never appear. It only has to outlast the scheduler
// getting round to a `go` statement that was already executed, so it is short.
const bootpartNoWaitForAbsence = 300 * time.Millisecond

// bootpartCountFrame reports how many live goroutines carry frame in their stack.
// A frame appears once per goroutine stack, so the substring count is the
// goroutine count.
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

// bootpartFrameAppeared polls for a goroutine carrying frame beyond baseline,
// returning true as soon as one shows up and false if none does within timeout.
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

// bootpartBootState builds a bootState whose ProjectCfg comes from yamlContent
// written to a real .harmonik/config.yaml, with a log writer the tests read back.
func bootpartBootState(t *testing.T, yamlContent string) (*bootState, *sockpartSyncBuffer) {
	t.Helper()
	pc, root := subpartLoadConfig(t, yamlContent)
	logBuf := &sockpartSyncBuffer{}
	return &bootState{cfg: Config{ProjectDir: root, ProjectCfg: pc, LogWriter: logBuf}}, logBuf
}

// bootpartDisabledYAML is the one-line partition an operator writes for name.
func bootpartDisabledYAML(name projectconfig.SubsystemName) string {
	return "schema_version: 1\nsubsystems:\n  " + string(name) + ":\n    enabled: false\n"
}

// --- crew idle reaper (SD-3) -------------------------------------------------

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

// --- branch reaper -----------------------------------------------------------

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

// --- both reapers at once, through a real daemon boot ------------------------

// bootpartReapersDisabledYAML switches off every subsystem this file covers, as
// an operator would write it in one block.
const bootpartReapersDisabledYAML = sockpartBaseConfigYAML + `
subsystems:
  crew_idle_reap:
    enabled: false
  branch_reaper:
    enabled: false
`

// The switches compose, and the daemon still reaches its work loop with both
// reapers off. This is the assertion that the nil bootState fields they leave
// behind are actually guarded: startBackgroundLoops calls StartWatcher on both,
// and an unguarded nil branch reaper panics inside a goroutine — taking the whole
// test binary down rather than failing one test.
func TestSubsystemPartition_Reapers_DisabledDaemonStillReachesWorkLoop(t *testing.T) {
	t.Setenv("HARMONIK_DEBUG_WIRING", "1")

	projectDir, jsonlPath := sockpartProjectDir(t, bootpartReapersDisabledYAML)
	logs := sockpartRunDaemon(t, projectDir, jsonlPath, 1500*time.Millisecond)

	for _, want := range []string{
		"crew idle reaper not constructed",
		"branch reaper not constructed",
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("boot log never reported %q; a silent partition is indistinguishable from a config that did not take effect", want)
		}
	}
	if !strings.Contains(logs, "composition-root wiring audit") {
		t.Error("daemon did not reach startBackgroundLoops with both reapers switched off; the core must run without them")
	}
	// The socket listener stays ON here, so its subtree was built and only these
	// two were carved out of it — the partitions are independent, not a re-run of
	// the socket-listener switch.
	if strings.Contains(logs, "socket listener and its handler subtree not constructed") {
		t.Error("the socket-listener partition fired; this fixture only switches off the two reapers")
	}
}
