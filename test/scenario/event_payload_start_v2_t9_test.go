//go:build scenario

package scenario

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

// eventPayloadT9BuildHarmonik builds the production CLI for the isolated
// scenario. The test invokes this binary through its public run command.
func eventPayloadT9BuildHarmonik(t *testing.T) string {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatal("go is required for the event payload scenario")
	}
	goModCmd := exec.CommandContext(t.Context(), goTool, "env", "GOMOD") //nolint:gosec // goTool comes from LookPath
	goModOut, err := goModCmd.Output()
	if err != nil {
		t.Fatalf("find module root: %v", err)
	}
	moduleRoot := filepath.Dir(strings.TrimSpace(string(goModOut)))
	binPath := filepath.Join(t.TempDir(), "harmonik")
	buildCmd := exec.CommandContext(t.Context(), goTool, "build", "-o", binPath, "./cmd/harmonik") //nolint:gosec // goTool comes from LookPath
	buildCmd.Dir = moduleRoot
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build harmonik CLI: %v\n%s", err, out)
	}
	return binPath
}

// eventPayloadT9RunStarted reads the one expected start event and decodes it
// through the strict current-version payload contract.
func eventPayloadT9RunStarted(t *testing.T, jsonlPath string) (core.Event, core.RunStartedPayload) {
	t.Helper()
	var starts []core.Event
	for _, line := range scenarioFixtureReadJSONLLines(t, jsonlPath) {
		var event core.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode event log line: %v\n%s", err, line)
		}
		if event.Type == string(core.EventTypeRunStarted) {
			starts = append(starts, event)
		}
	}
	if len(starts) != 1 {
		t.Fatalf("run_started count = %d, want 1", len(starts))
	}
	if starts[0].SchemaVersion != 2 {
		t.Fatalf("run_started schema_version = %d, want 2", starts[0].SchemaVersion)
	}
	var payload core.RunStartedPayload
	if err := json.Unmarshal(starts[0].Payload, &payload); err != nil {
		t.Fatalf("decode strict version-2 run_started payload: %v\npayload: %s", err, starts[0].Payload)
	}
	return starts[0], payload
}

// TestScenario_EventPayloadStartV2_DOTLifecycle proves the public `harmonik
// run` path against one isolated daemon. It uses a named DOT graph and twin
// handler, then checks the durable start record and its terminal event.
func TestScenario_EventPayloadStartV2_DOTLifecycle(t *testing.T) {
	if codexTwinBinaryPath == "" {
		t.Skip("harmonik-twin-codex binary not built")
	}

	realBrPath := codexLifecycleFixtureBrPath(t)
	project := scenarioFixtureProjectDir(t)
	projectDir, err := filepath.EvalSymlinks(project.projectDir)
	if err != nil {
		t.Fatalf("resolve scenario project path: %v", err)
	}
	jsonlPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	codexLifecycleFixtureGitRepo(t, projectDir)
	codexLifecycleFixtureWorkflowDot(t, projectDir)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := codexLifecycleFixtureBrWrapperScript(t, realBrPath, dbPath)
	handlerScript := codexLifecycleFixtureHandlerScript(t, codexTwinBinaryPath)
	beadID := codexLifecycleFixtureInitBr(t, realBrPath, projectDir, brWrapper)

	// Keep workspace trust state inside the temporary scenario project.
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", filepath.Join(t.TempDir(), ".claude.json"))
	queueStore := queuewiring.NewQueueStore()
	cancel, daemonDone := scenarioFixtureStartDaemon(t, daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              brWrapper,
		HandlerBinary:       handlerScript,
		HandlerEnv:          os.Environ(),
		NoAutoPull:          true,
		QueueStore:          queueStore,
		WorkflowModeDefault: core.WorkflowModeDot,
	})
	defer func() {
		cancel()
		scenarioFixtureWaitDaemon(t, daemonDone, 10*time.Second)
	}()

	sockPath := filepath.Join(projectDir, ".harmonik", "daemon.sock")
	if !scenarioFixturePollSocket(sockPath, 10*time.Second) {
		t.Fatalf("isolated daemon socket did not start: %s", sockPath)
	}

	workflowPath := filepath.Join(projectDir, "workflow.dot")
	runCtx, cancelRun := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancelRun()
	runCmd := exec.CommandContext(runCtx, eventPayloadT9BuildHarmonik(t),
		"run", "--beads", beadID, "--max-concurrent", "1",
		"--project", projectDir, "--workflow-mode", "dot", "--workflow-ref", workflowPath)
	runCmd.Dir = projectDir
	runCmd.Env = append(os.Environ(), "TMUX=/tmp/harmonik-t9,1,0")
	if out, err := runCmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated harmonik run failed: %v\n%s", err, out)
	}

	startEvent, start := eventPayloadT9RunStarted(t, jsonlPath)
	if startEvent.RunID == nil || *startEvent.RunID != start.RunID {
		t.Fatalf("run_started envelope run ID = %v, payload run ID = %s", startEvent.RunID, start.RunID)
	}
	if start.Descriptor() != (core.WorkflowDescriptor{
		WorkflowID:      core.WorkflowID("review-loop-example"),
		WorkflowVersion: core.WorkflowVersion("1.0"),
	}) {
		t.Fatalf("start descriptor = %#v, want review-loop-example@1.0", start.Descriptor())
	}
	if start.WorkflowMode != core.WorkflowModeDot || start.ReviewPolicy != core.ReviewPolicyReviewed ||
		start.WorkflowSelectionSource != core.WorkflowSelectionExplicitRef {
		t.Fatalf("start graph decision = mode %q policy %q source %q", start.WorkflowMode, start.ReviewPolicy, start.WorkflowSelectionSource)
	}
	if start.BeadID == nil || *start.BeadID != core.BeadID(beadID) ||
		start.WorkspacePath == "" || start.InputRef != "bead:"+beadID || start.StartedAt.IsZero() {
		t.Fatalf("start run context is incomplete: %#v", start)
	}
	if start.WorkerName != nil || start.WorkerOS != nil {
		t.Fatalf("local start worker fields = %v, %v; want explicit null", start.WorkerName, start.WorkerOS)
	}
	if start.QueueID == nil || *start.QueueID == "" || start.QueueGroupIndex == nil || *start.QueueGroupIndex != 0 {
		t.Fatalf("start queue attribution = %v, %v; want queue ID and group 0", start.QueueID, start.QueueGroupIndex)
	}

	var sawDotDispatch, sawCompleted bool
	for _, line := range scenarioFixtureReadJSONLLines(t, jsonlPath) {
		var event core.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode lifecycle event: %v", err)
		}
		if event.RunID == nil || *event.RunID != start.RunID {
			continue
		}
		switch core.EventType(event.Type) {
		case core.EventTypeNodeDispatchRequested:
			sawDotDispatch = true
		case core.EventTypeRunCompleted:
			sawCompleted = true
		case core.EventTypeRunFailed:
			t.Fatalf("started run reached unexpected failure: %s", event.Payload)
		}
	}
	if !sawDotDispatch {
		t.Fatal("no DOT node dispatch event for the started run")
	}
	if !sawCompleted {
		t.Fatal("no run_completed event for the started run")
	}
}
