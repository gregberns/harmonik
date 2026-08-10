//go:build scenario

package scenario

import (
	"bufio"
	"bytes"
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
		if event.Type == core.EventTypeRunStarted {
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
	// scenarioFixtureProjectDir already returns a symlink-resolved path, and it
	// measured THAT path against the sun_path limit. Re-resolving here is what
	// used to make the guard and the bound socket two different strings.
	projectDir := project.projectDir
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

// eventPayloadT10InitLegacySingleBead seeds one open bead with the legacy
// workflow:single label. The resolver must map this compatibility input to the
// registered no-review DOT graph before it emits run_started.
func eventPayloadT10InitLegacySingleBead(t *testing.T, brPath, projectDir, brWrapper string) string {
	t.Helper()
	initCmd := exec.CommandContext(t.Context(), brPath, "init", "--prefix", "t10") //nolint:gosec // fixed test command
	initCmd.Dir = projectDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("initialise isolated Beads store: %v\n%s", err, out)
	}
	createCmd := exec.CommandContext(t.Context(), brWrapper,
		"create", "legacy no-review DOT scenario bead", "--status", "open",
		"--labels", "workflow:single", "--silent") //nolint:gosec // fixed test command
	out, err := createCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("create legacy single bead: %v\n%s", err, out)
	}
	beadID := strings.TrimSpace(string(out))
	if beadID == "" {
		t.Fatal("create legacy single bead returned no ID")
	}
	return beadID
}

// eventPayloadT10StreamEvents decodes the public subscribe command's NDJSON.
func eventPayloadT10StreamEvents(t *testing.T, raw string) []core.Event {
	t.Helper()
	var events []core.Event
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Bytes()
		var event core.Event
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode subscribe stream line: %v\n%s", err, line)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read subscribe stream: %v", err)
	}
	return events
}

// eventPayloadT10SubscriberSawEvent reads completed NDJSON records from the
// public subscriber. A final partial record is still being written and is not
// yet evidence of delivery.
func eventPayloadT10SubscriberSawEvent(path string, want core.EventType) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			continue
		}
		var event core.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if core.EventType(event.Type) == want {
			return true
		}
	}
	return false
}

func eventPayloadT10WaitForSubscriberEvent(t *testing.T, path string, want core.EventType, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if eventPayloadT10SubscriberSawEvent(path, want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("public subscribe stream did not receive %s", want)
}

// TestScenario_EventPayloadQueueSubscribeLegacySingle proves the queue submit
// and subscribe command path. The legacy label must resolve to no-review-bead,
// but the emitted start record must still name DOT mode and the resolver facts.
func TestScenario_EventPayloadQueueSubscribeLegacySingle(t *testing.T) {
	if codexTwinBinaryPath == "" {
		t.Skip("harmonik-twin-codex binary not built")
	}

	realBrPath := codexLifecycleFixtureBrPath(t)
	project := scenarioFixtureProjectDir(t)
	// scenarioFixtureProjectDir already returns a symlink-resolved path, and it
	// measured THAT path against the sun_path limit. Re-resolving here is what
	// used to make the guard and the bound socket two different strings.
	projectDir := project.projectDir
	jsonlPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	codexLifecycleFixtureGitRepo(t, projectDir)

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := codexLifecycleFixtureBrWrapperScript(t, realBrPath, dbPath)
	beadID := eventPayloadT10InitLegacySingleBead(t, realBrPath, projectDir, brWrapper)
	handlerScript := codexLifecycleFixtureHandlerScript(t, codexTwinBinaryPath)

	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", filepath.Join(t.TempDir(), ".claude.json"))
	cancel, daemonDone := scenarioFixtureStartDaemon(t, daemon.Config{
		ProjectDir:          projectDir,
		JSONLLogPath:        jsonlPath,
		BrPath:              brWrapper,
		HandlerBinary:       handlerScript,
		HandlerEnv:          os.Environ(),
		NoAutoPull:          true,
		QueueStore:          queuewiring.NewQueueStore(),
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

	harmonikBin := eventPayloadT9BuildHarmonik(t)
	subscribePath := filepath.Join(t.TempDir(), "subscribe.ndjson")
	subscribeFile, err := os.Create(subscribePath)
	if err != nil {
		t.Fatalf("create subscribe output file: %v", err)
	}
	defer subscribeFile.Close()
	var subscribeErr bytes.Buffer
	subscribeCmd := exec.Command(harmonikBin,
		"subscribe", "--project", projectDir, "--json",
		"--types", "run_started,node_dispatch_requested,run_completed,run_failed") //nolint:gosec // temporary test binary
	subscribeCmd.Dir = projectDir
	subscribeCmd.Stdout = subscribeFile
	subscribeCmd.Stderr = &subscribeErr
	if err := subscribeCmd.Start(); err != nil {
		t.Fatalf("start public subscribe command: %v", err)
	}
	subscribeDone := make(chan error, 1)
	go func() { subscribeDone <- subscribeCmd.Wait() }()
	stoppedSubscribe := false
	defer func() {
		if !stoppedSubscribe {
			_ = subscribeCmd.Process.Signal(os.Interrupt)
			<-subscribeDone
		}
	}()

	// The public subscriber is live-only. Give its socket request time to arm
	// before the queue command emits the start record.
	time.Sleep(250 * time.Millisecond)
	submitCmd := exec.CommandContext(t.Context(), harmonikBin,
		"queue", "submit", "--project", projectDir, "--beads", beadID, "--json") //nolint:gosec // temporary test binary
	submitCmd.Dir = projectDir
	if out, err := submitCmd.CombinedOutput(); err != nil {
		t.Fatalf("public queue submit failed: %v\n%s", err, out)
	}

	if !scenarioFixturePollJSONLForEvent(t, jsonlPath, []string{string(core.EventTypeRunCompleted)}, 90*time.Second) {
		t.Fatalf("isolated queue run did not complete; log:\n%s", strings.Join(scenarioFixtureReadJSONLLines(t, jsonlPath), "\n"))
	}
	// JSONL persistence proves the daemon emitted completion. Wait for the public
	// subscriber's own output before its interrupt closes the socket.
	eventPayloadT10WaitForSubscriberEvent(t, subscribePath, core.EventTypeRunCompleted, 10*time.Second)
	if err := subscribeCmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("stop public subscribe command: %v", err)
	}
	if err := <-subscribeDone; err != nil {
		t.Fatalf("public subscribe command: %v\n%s", err, subscribeErr.String())
	}
	stoppedSubscribe = true

	subscribeRaw, err := os.ReadFile(subscribePath)
	if err != nil {
		t.Fatalf("read subscribe output: %v", err)
	}
	streamEvents := eventPayloadT10StreamEvents(t, string(subscribeRaw))
	var starts []core.Event
	for _, event := range streamEvents {
		switch core.EventType(event.Type) {
		case core.EventTypeRunStarted:
			starts = append(starts, event)
		}
	}
	if len(starts) != 1 {
		t.Fatalf("subscribe run_started count = %d, want 1\n%s", len(starts), subscribeRaw)
	}
	startEvent := starts[0]
	if startEvent.SchemaVersion != 2 || startEvent.RunID == nil {
		t.Fatalf("subscribe start envelope = %#v, want version 2 with a run ID", startEvent)
	}
	var start core.RunStartedPayload
	if err := json.Unmarshal(startEvent.Payload, &start); err != nil {
		t.Fatalf("decode subscribed strict version-2 start payload: %v\npayload: %s", err, startEvent.Payload)
	}
	if start.RunID != *startEvent.RunID || start.Descriptor() != (core.WorkflowDescriptor{
		WorkflowID:      core.WorkflowID("no-review-bead"),
		WorkflowVersion: core.WorkflowVersion("1.0"),
	}) {
		t.Fatalf("subscribe start identity = envelope %v payload %#v", startEvent.RunID, start)
	}
	if start.WorkflowMode != core.WorkflowModeDot || start.ReviewPolicy != core.ReviewPolicyNoReview ||
		start.WorkflowSelectionSource != core.WorkflowSelectionLegacySingleLabel {
		t.Fatalf("subscribe start workflow decision = mode %q policy %q source %q", start.WorkflowMode, start.ReviewPolicy, start.WorkflowSelectionSource)
	}
	if start.BeadID == nil || string(*start.BeadID) != beadID || start.WorkspacePath == "" ||
		start.InputRef != "bead:"+beadID || start.StartedAt.IsZero() ||
		start.WorkerName != nil || start.WorkerOS != nil || start.QueueID == nil || *start.QueueID == "" ||
		start.QueueGroupIndex == nil || *start.QueueGroupIndex != 0 {
		t.Fatalf("subscribe start context is incomplete: %#v", start)
	}
	var sawDotDispatch, sawCompleted bool
	for _, event := range streamEvents {
		if event.RunID == nil || *event.RunID != start.RunID {
			continue
		}
		switch core.EventType(event.Type) {
		case core.EventTypeNodeDispatchRequested:
			sawDotDispatch = true
		case core.EventTypeRunCompleted:
			sawCompleted = true
		case core.EventTypeRunFailed:
			t.Fatalf("legacy single run failed: %s", event.Payload)
		}
	}
	if !sawDotDispatch {
		t.Fatal("subscribe stream did not show DOT node dispatch")
	}
	if !sawCompleted {
		t.Fatal("subscribe stream did not show run_completed for the started run")
	}
}
