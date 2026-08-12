//go:build scenario

package daemon_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	"github.com/gregberns/harmonik/internal/workspace"
)

const queueCrashGraphHelperEnv = "HARMONIK_QUEUE_CRASH_GRAPH_HELPER"

func TestScenario_QueueCrashGraphHelper(t *testing.T) {
	if os.Getenv(queueCrashGraphHelperEnv) != "1" {
		return
	}
	maxConcurrent, err := strconv.Atoi(os.Getenv("HARMONIK_QUEUE_CRASH_MAX_CONCURRENT"))
	require.NoError(t, err)
	err = daemon.Start(context.Background(), daemon.Config{
		ProjectDir:            os.Getenv("HARMONIK_QUEUE_CRASH_PROJECT"),
		JSONLLogPath:          os.Getenv("HARMONIK_QUEUE_CRASH_JSONL"),
		BrPath:                os.Getenv("HARMONIK_QUEUE_CRASH_BR"),
		HandlerBinary:         os.Getenv("HARMONIK_QUEUE_CRASH_HANDLER"),
		HandlerEnv:            os.Environ(),
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		SkipRestartBackoff:    true,
		AgentReadyTimeout:     15 * time.Second,
		MaxConcurrent:         maxConcurrent,
		NoAutoPull:            true,
		LogWriter:             os.Stderr,
		WorkflowModeDefault:   core.WorkflowModeDot,
		TargetBranch:          "integration",
		ProtectBranches:       []string{"main"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestScenario_QueueSubmit_AbruptCrashResumesFanGraph(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}

	realBrPath := queueSubmitDispatchBrPath(t)
	projectDir, jsonlPath := queueSubmitDispatchProjectDir(t)
	queueSubmitDispatchGitRepo(t, projectDir)
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := queueSubmitDispatchBrWrapper(t, realBrPath, dbPath)
	epicID, ids := queueSubmitDispatchInitBrFanGraph(t, realBrPath, projectDir, brWrapper)
	twinWrapper := queueSubmitDispatchTwinWrapper(t, twinPath)
	scenariotest.WriteStandardWorkflowDot(t, projectDir)

	helper := exec.Command(os.Args[0], "-test.run=^TestScenario_QueueCrashGraphHelper$", "-test.v") //nolint:gosec,noctx // test binary and fixed arguments
	helper.Env = append(os.Environ(),
		queueCrashGraphHelperEnv+"=1",
		"HARMONIK_QUEUE_CRASH_PROJECT="+projectDir,
		"HARMONIK_QUEUE_CRASH_JSONL="+jsonlPath,
		"HARMONIK_QUEUE_CRASH_BR="+brWrapper,
		"HARMONIK_QUEUE_CRASH_HANDLER="+twinWrapper,
		"HARMONIK_QUEUE_CRASH_MAX_CONCURRENT=3",
	)
	helper.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, helper.Start(), "start child daemon")
	helperDone := make(chan error, 1)
	go func() { helperDone <- helper.Wait() }()
	t.Cleanup(func() {
		if helper.Process != nil {
			_ = syscall.Kill(-helper.Process.Pid, syscall.SIGKILL)
		}
		select {
		case <-helperDone:
		default:
		}
	})

	queueSubmitDispatchWaitSocket(t, projectDir)
	_ = queueSubmitDispatchSubmitCLI(t, projectDir, ids)
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 60*time.Second, func() {
		for queueSubmitDispatchCountRunStarted(t, jsonlPath) < 3 ||
			queueSubmitDispatchCountRunTerminal(t, jsonlPath) < 1 {
			time.Sleep(25 * time.Millisecond)
		}
	})

	require.NoError(t, syscall.Kill(-helper.Process.Pid, syscall.SIGKILL), "kill daemon process group")
	select {
	case <-helperDone:
	case <-time.After(10 * time.Second):
		t.Fatal("child daemon process group did not exit after SIGKILL")
	}
	firstTerminalCount := queueSubmitDispatchCountRunTerminal(t, jsonlPath)
	require.GreaterOrEqual(t, firstTerminalCount, 1, "root must finish before branch crash")

	loopCtx, loopCancel := context.WithCancel(context.Background())
	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.Start(loopCtx, daemon.Config{
			ProjectDir:            projectDir,
			JSONLLogPath:          jsonlPath,
			BrPath:                brWrapper,
			HandlerBinary:         twinWrapper,
			HandlerEnv:            os.Environ(),
			SkipWALCheckpoint:     true,
			SkipBrHistoryRotation: true,
			SkipRestartBackoff:    true,
			AgentReadyTimeout:     15 * time.Second,
			MaxConcurrent:         3,
			NoAutoPull:            true,
			LogWriter:             testLogWriter{t: t},
			WorkflowModeDefault:   core.WorkflowModeDot,
			TargetBranch:          "integration",
			ProtectBranches:       []string{"main"},
		})
	}()

	queueSubmitDispatchWaitSocket(t, projectDir)
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 90*time.Second, func() {
		for !queueCrashGraphAllClosed(t, brWrapper, ids) || queueCrashGraphCompletedCount(jsonlPath) < len(ids) {
			time.Sleep(50 * time.Millisecond)
		}
	})
	loopCancel()
	require.NoError(t, <-startDone, "restart daemon")

	for _, id := range ids {
		scenariotest.AssertBeadStatus(t, brWrapper, string(id), "closed")
	}
	validationData, validationErr := os.ReadFile(filepath.Join(projectDir, ".harmonik", "validation-runs"))
	require.NoError(t, validationErr, "read durable validation evidence after restart")
	require.Len(t, strings.Fields(string(validationData)), len(ids),
		"restart must not bypass or duplicate a child's commit gate")
	scenariotest.AssertBeadStatus(t, brWrapper, string(epicID), "open")
	epicEvents := queueSubmitDispatchEpicCompleted(t, jsonlPath, epicID)
	require.Len(t, epicEvents, 1, "restart must retain one epic completion decision point")
	derivedBranch, err := workspace.IntegrationBranchName(t.Context(), string(epicID))
	require.NoError(t, err)
	queueSubmitDispatchAssertLanded(t, projectDir, derivedBranch, ids)
	graphEvents := queueSubmitDispatchGraphEvents(t, jsonlPath, ids)
	require.Equal(t, ids[0], graphEvents.started[0], "root must start first")
	require.Equal(t, 1, queueCrashGraphEventCount(jsonlPath, core.EventTypeRunStarted, ids[0]),
		"the successful root must not run again after restart")
	require.Equal(t, 1, queueCrashGraphEventCount(jsonlPath, core.EventTypeRunCompleted, ids[0]),
		"the successful root must have one completion")
	require.Equal(t, 1, queueCrashGraphEventCount(jsonlPath, core.EventTypeRunStarted, ids[4]),
		"the join must start once after every branch completes")
	require.Greater(t, graphEvents.startedAt[ids[4]], graphEvents.completedAt[ids[1]], "join waits for B")
	require.Greater(t, graphEvents.startedAt[ids[4]], graphEvents.completedAt[ids[2]], "join waits for C")
	require.Greater(t, graphEvents.startedAt[ids[4]], graphEvents.completedAt[ids[3]], "join waits for D")
}

func queueCrashGraphEventCount(jsonlPath string, eventType core.EventType, beadID core.BeadID) int {
	data, err := os.ReadFile(jsonlPath) //nolint:gosec // scenario temp path
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"type":"`+string(eventType)+`"`) && strings.Contains(line, string(beadID)) {
			n++
		}
	}
	return n
}

func queueCrashGraphCompletedCount(jsonlPath string) int {
	data, err := os.ReadFile(jsonlPath) //nolint:gosec // scenario temp path
	if err != nil {
		return 0
	}
	return strings.Count(string(data), `"type":"run_completed"`)
}

func queueCrashGraphAllClosed(t *testing.T, brWrapper string, ids []core.BeadID) bool {
	t.Helper()
	for _, id := range ids {
		cmd := exec.CommandContext(t.Context(), brWrapper, "show", string(id), "--json")
		out, err := cmd.Output()
		if err != nil {
			return false
		}
		var rows []struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(out, &rows) != nil || len(rows) != 1 || rows[0].Status != "closed" {
			return false
		}
	}
	return true
}
