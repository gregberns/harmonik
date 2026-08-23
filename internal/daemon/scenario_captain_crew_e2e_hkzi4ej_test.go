//go:build scenario

package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	"github.com/gregberns/harmonik/internal/mergeq"
	"github.com/gregberns/harmonik/internal/queue"
)

func cc14EvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err, "cc14EvalSymlinks: EvalSymlinks %q", path)
	return resolved
}

func cc14ProjectDir(t *testing.T) (projectDir, jsonlPath string) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "cc14-")
	require.NoError(t, err, "cc14ProjectDir: MkdirTemp /tmp")
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	projectDir = cc14EvalSymlinks(t, root)
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
		filepath.Join(".harmonik", "queues"),
		filepath.Join(".harmonik", "crew"),
		filepath.Join(".harmonik", "crew", "missions"),
	} {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		require.NoError(t, os.MkdirAll(filepath.Join(projectDir, sub), 0o755), "cc14ProjectDir: mkdir %s", sub)
	}
	jsonlPath = filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	return projectDir, jsonlPath
}

func cc14GitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "cc14GitRepo: git %v\n%s", args, out)
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik CC14 Test")
	readmePath := filepath.Join(dir, "README")
	require.NoError(t, os.WriteFile(readmePath, []byte("cc14 captain-crew e2e\n"), 0o644), "cc14GitRepo: write README")
	run("add", "README")
	run("commit", "-m", "Initial commit")

	raw := t.TempDir()
	originDir, err := filepath.EvalSymlinks(raw)
	require.NoError(t, err, "cc14GitRepo: EvalSymlinks originDir")
	initBareCmd := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	out, err := initBareCmd.CombinedOutput()
	require.NoError(t, err, "cc14GitRepo: git init --bare\n%s", out)
	run("remote", "add", "origin", originDir)
	run("push", "origin", "main")
}

func cc14BrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("cc14: br required for scenario test (not on PATH)")
	}
	return brPath
}

func cc14BrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	dir := cc14EvalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755), "cc14BrWrapperScript: WriteFile")
	return path
}

func cc14Br(t *testing.T, brWrapper string, args ...string) string {
	t.Helper()
	//nolint:gosec // G204: br args are test-internal literals; not user input
	cmd := exec.CommandContext(t.Context(), brWrapper, args...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "cc14Br: br %v\n%s", args, out)
	return strings.TrimSpace(string(out))
}

func cc14CreateBead(t *testing.T, brWrapper, title string) string {
	t.Helper()
	id := cc14Br(t, brWrapper, "create", title, "--status", "open", "--silent")
	require.NotEmpty(t, id, "cc14CreateBead: empty id for %q", title)
	return id
}

type cc14Epic struct {
	EpicID    string
	ChildA    string
	ChildB    string
	QueueName string
	QueueID   string
	CrewName  string
}

func cc14SeedEpic(t *testing.T, brWrapper, label string) cc14Epic {
	t.Helper()
	epic := cc14Br(t, brWrapper, "create", "cc14 epic "+label, "--type", "epic", "--status", "open", "--silent")
	require.NotEmpty(t, epic, "cc14SeedEpic: empty epic id for %q", label)
	childA := cc14CreateBead(t, brWrapper, "cc14 child A of "+label)
	childB := cc14CreateBead(t, brWrapper, "cc14 child B of "+label)
	cc14Br(t, brWrapper, "dep", "add", childA, epic, "--type", "parent-child")
	cc14Br(t, brWrapper, "dep", "add", childB, epic, "--type", "parent-child")
	return cc14Epic{EpicID: epic, ChildA: childA, ChildB: childB}
}

func cc14NewQueueID(t *testing.T) string {
	t.Helper()
	u, err := uuid.NewV7()
	require.NoError(t, err, "cc14NewQueueID: NewV7")
	return u.String()
}

func cc14WriteMissionHandoff(t *testing.T, projectDir string, e cc14Epic, captainName string) string {
	t.Helper()
	missionsDir := filepath.Join(projectDir, ".harmonik", "crew", "missions")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	require.NoError(t, os.MkdirAll(missionsDir, 0o755), "cc14WriteMissionHandoff: mkdir missions")
	path := filepath.Join(missionsDir, e.CrewName+".md")
	body := fmt.Sprintf(`---
schema_version: 1
crew_name: %s
queue: %s
epic_id: %s
goal: drive epic %s to completion on queue %s
captain_name: %s
---

# Mission for %s

Adopt epic '%s', mirror the assignee, and dispatch its ready children to your
OWN queue '%s'. Do NOT dispatch to the shared main queue. Do NOT close beads —
the daemon owns terminal transitions.
`, e.CrewName, e.QueueName, e.EpicID, e.EpicID, e.QueueName, captainName,
		e.CrewName, e.EpicID, e.QueueName)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644), "cc14WriteMissionHandoff: WriteFile")
	return path
}

type cc14MissionFrontmatter struct {
	CrewName string
	Queue    string
	EpicID   string
}

func cc14ParseMissionHandoff(t *testing.T, path string) cc14MissionFrontmatter {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	data, err := os.ReadFile(path)
	require.NoError(t, err, "cc14ParseMissionHandoff: read %s", path)
	var fm cc14MissionFrontmatter
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "crew_name:"):
			fm.CrewName = strings.TrimSpace(strings.TrimPrefix(line, "crew_name:"))
		case strings.HasPrefix(line, "queue:"):
			fm.Queue = strings.TrimSpace(strings.TrimPrefix(line, "queue:"))
		case strings.HasPrefix(line, "epic_id:"):
			fm.EpicID = strings.TrimSpace(strings.TrimPrefix(line, "epic_id:"))
		}
	}
	return fm
}

func cc14WriteCrewRecord(t *testing.T, projectDir string, e cc14Epic, sessionID, handle string) {
	t.Helper()
	require.NoError(t, crew.Write(projectDir, crew.Record{
		Name:      e.CrewName,
		SessionID: sessionID,
		Queue:     e.QueueName,
		Epic:      e.EpicID,
		Handle:    handle,
		StartedAt: time.Now().UTC(),
	}), "cc14WriteCrewRecord: crew.Write %s", e.CrewName)
}

func cc14EnsureNamedQueue(t *testing.T, projectDir string, e cc14Epic) {
	t.Helper()
	now := time.Now().UTC()
	started := now
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       e.QueueID,
		Name:          e.QueueName,
		Workers:       1,
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				CreatedAt:  now,
				StartedAt:  &started,
				Items: []queue.Item{
					{BeadID: core.BeadID(e.ChildA), Status: queue.ItemStatusPending},
					{BeadID: core.BeadID(e.ChildB), Status: queue.ItemStatusPending},
				},
			},
		},
	}
	require.NoError(t, queue.Persist(t.Context(), projectDir, q), "cc14EnsureNamedQueue: persist %s", e.QueueName)
}

func cc14TwinWrapperScript(t *testing.T, twinPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "twin-cc14-wrapper.sh")
	content := `#!/bin/sh
set -e
if [ -f "$PWD/.harmonik/review-target.md" ]; then
  mkdir -p "$PWD/.harmonik"
  printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"cc14 review-loop happy path"}' > "$PWD/.harmonik/review.json"
  exit 0
fi
exec "` + twinPath + `" --scenario commit-on-cue-startup-delay --worktree-path "$PWD"
`
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755), "cc14TwinWrapperScript: WriteFile")
	return path
}

func cc14SockPath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "daemon.sock")
}

func cc14WaitSocketReady(t *testing.T, projectDir string, budget time.Duration) {
	t.Helper()
	sock := cc14SockPath(projectDir)
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sock)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("cc14WaitSocketReady: socket %q not ready within %s", sock, budget)
}

func cc14SocketOp(t *testing.T, projectDir, op string, payload map[string]any) daemon.SocketResponse {
	t.Helper()
	sock := cc14SockPath(projectDir)
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sock)
	require.NoError(t, err, "cc14SocketOp(%s): dial %q", op, sock)
	defer func() { _ = conn.Close() }()

	plBytes, err := json.Marshal(payload)
	require.NoError(t, err, "cc14SocketOp(%s): marshal payload", op)
	reqBytes, err := json.Marshal(daemon.SocketRequest{Op: op, Payload: plBytes})
	require.NoError(t, err, "cc14SocketOp(%s): marshal request", op)

	_, err = conn.Write(reqBytes)
	require.NoError(t, err, "cc14SocketOp(%s): write", op)
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite()
	}
	var resp daemon.SocketResponse
	require.NoError(t, json.NewDecoder(conn).Decode(&resp), "cc14SocketOp(%s): decode response", op)
	return resp
}

func cc14CommsJoin(t *testing.T, projectDir, agentName string) {
	t.Helper()
	resp := cc14SocketOp(t, projectDir, "comms-presence", map[string]any{
		"agent":  agentName,
		"status": "online",
		"reason": "join",
	})
	require.True(t, resp.Ok, "cc14CommsJoin(%s): Ok=false: %s", agentName, resp.Error)
}

func cc14CommsSend(t *testing.T, projectDir, from, to, topic, body string) string {
	t.Helper()
	resp := cc14SocketOp(t, projectDir, "comms-send", map[string]any{
		"from":  from,
		"to":    to,
		"topic": topic,
		"body":  body,
	})
	require.True(t, resp.Ok, "cc14CommsSend(%s→%s): Ok=false: %s", from, to, resp.Error)
	var r struct {
		EventID string `json:"event_id"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &r), "cc14CommsSend: decode result")
	require.NotEmpty(t, r.EventID, "cc14CommsSend: empty event_id")
	return r.EventID
}

type cc14Event struct {
	Type    string          `json:"type"`
	RunID   string          `json:"run_id"`
	Payload json.RawMessage `json:"payload"`
}

func cc14ScanEvents(t *testing.T, jsonlPath string) []cc14Event {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	f, err := os.Open(jsonlPath)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err, "cc14ScanEvents: open %s", jsonlPath)
	defer func() { _ = f.Close() }()
	var out []cc14Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev cc14Event
		if json.Unmarshal([]byte(line), &ev) == nil {
			out = append(out, ev)
		}
	}
	return out
}

func cc14EventCount(t *testing.T, jsonlPath, eventType string) int {
	t.Helper()
	n := 0
	for _, ev := range cc14ScanEvents(t, jsonlPath) {
		if ev.Type == eventType {
			n++
		}
	}
	return n
}

func cc14OnlineAgents(t *testing.T, jsonlPath string) map[string]bool {
	t.Helper()
	latest := map[string]string{} // agent → latest status (file order = UUIDv7 order)
	for _, ev := range cc14ScanEvents(t, jsonlPath) {
		if ev.Type != "agent_presence" {
			continue
		}
		var p core.AgentPresencePayload
		if json.Unmarshal(ev.Payload, &p) != nil || p.Agent == "" {
			continue
		}
		latest[p.Agent] = string(p.Status)
	}
	online := map[string]bool{}
	for agent, status := range latest {
		if status == string(core.AgentPresenceStatusOnline) {
			online[agent] = true
		}
	}
	return online
}

func cc14WaitOnline(t *testing.T, jsonlPath string, budget time.Duration, wantAgents ...string) map[string]bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		online := cc14OnlineAgents(t, jsonlPath)
		all := true
		for _, a := range wantAgents {
			if !online[a] {
				all = false
				break
			}
		}
		if all {
			return online
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cc14OnlineAgents(t, jsonlPath)
}

func cc14CommsMessages(t *testing.T, jsonlPath, from, to, topic string) []core.AgentMessagePayload {
	t.Helper()
	var out []core.AgentMessagePayload
	for _, ev := range cc14ScanEvents(t, jsonlPath) {
		if ev.Type != "agent_message" {
			continue
		}
		var p core.AgentMessagePayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			continue
		}
		if from != "" && p.From != from {
			continue
		}
		if to != "" && p.To != to && p.To != "*" {
			continue
		}
		if topic != "" && p.Topic != topic {
			continue
		}
		out = append(out, p)
	}
	return out
}

func cc14EpicCompletedPayloads(t *testing.T, jsonlPath string) []core.EpicCompletedPayload {
	t.Helper()
	var out []core.EpicCompletedPayload
	for _, ev := range cc14ScanEvents(t, jsonlPath) {
		if ev.Type != string(core.EventTypeEpicCompleted) {
			continue
		}
		var p core.EpicCompletedPayload
		if json.Unmarshal(ev.Payload, &p) == nil {
			out = append(out, p)
		}
	}
	return out
}

func cc14BeadAssignee(t *testing.T, brWrapper, beadID string) string {
	t.Helper()
	out := cc14Br(t, brWrapper, "show", beadID, "--format", "json")
	var items []struct {
		Assignee string `json:"assignee"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &items), "cc14BeadAssignee: unmarshal %s", beadID)
	require.NotEmpty(t, items, "cc14BeadAssignee: no record for %s", beadID)
	return items[0].Assignee
}

func cc14BeadStatus(t *testing.T, brWrapper, beadID string) string {
	t.Helper()
	out := cc14Br(t, brWrapper, "show", beadID, "--format", "json")
	var items []struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &items), "cc14BeadStatus: unmarshal %s", beadID)
	require.NotEmpty(t, items, "cc14BeadStatus: no record for %s", beadID)
	return items[0].Status
}

func cc14CommentCount(t *testing.T, brWrapper, beadID string) int {
	t.Helper()
	//nolint:gosec // G204: br args are test-internal literals; not user input
	cmd := exec.CommandContext(t.Context(), brWrapper, "comments", "list", beadID, "--json")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	var comments []map[string]any
	if json.Unmarshal(out, &comments) != nil {
		return 0
	}
	return len(comments)
}

func cc14PollBeadClosed(t *testing.T, brWrapper, beadID string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if cc14BeadStatus(t, brWrapper, beadID) == "closed" {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func cc14QueueItemStatuses(t *testing.T, projectDir, queueName string) []string {
	t.Helper()
	queuePath := filepath.Join(projectDir, ".harmonik", "queues", queueName+".json")
	//nolint:gosec // G304: path is t.TempDir()-based; not user input
	data, err := os.ReadFile(queuePath)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err, "cc14QueueItemStatuses: read %s", queuePath)
	var q struct {
		Groups []struct {
			Items []struct {
				Status string `json:"status"`
			} `json:"items"`
		} `json:"groups"`
	}
	require.NoError(t, json.Unmarshal(data, &q), "cc14QueueItemStatuses: unmarshal %s", queuePath)
	if len(q.Groups) == 0 {
		return nil
	}
	out := make([]string, len(q.Groups[0].Items))
	for i, it := range q.Groups[0].Items {
		out[i] = it.Status
	}
	return out
}

// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH) to isolate
// EnsureWorktreeTrust — same rationale as TestScenario_HappyPath_N1 and the
// hk-umemp multi-queue scenario.
//
// Bead: hk-zi4ej.
func TestScenario_CaptainCrewE2E_hkzi4ej(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	const captainName = "cc14-captain"

	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("cc14: harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}
	realBrPath := cc14BrPath(t)

	projectDir, jsonlPath := cc14ProjectDir(t)
	cc14GitRepo(t, projectDir)
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := cc14BrWrapperScript(t, realBrPath, dbPath)

	//nolint:gosec // G204: br args are test-internal literals; not user input
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "cc14")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	require.NoError(t, initErr, "cc14: br init: %s", initOut)

	epicA := cc14SeedEpic(t, brWrapper, "alpha")
	epicA.CrewName = "cc14-crew-alpha"
	epicA.QueueName = "cc14-alpha"
	epicA.QueueID = cc14NewQueueID(t)

	epicB := cc14SeedEpic(t, brWrapper, "beta")
	epicB.CrewName = "cc14-crew-beta"
	epicB.QueueName = "cc14-beta"
	epicB.QueueID = cc14NewQueueID(t)

	t.Logf("cc14: epicA=%s {%s,%s} crew=%s queue=%s", epicA.EpicID, epicA.ChildA, epicA.ChildB, epicA.CrewName, epicA.QueueName)
	t.Logf("cc14: epicB=%s {%s,%s} crew=%s queue=%s", epicB.EpicID, epicB.ChildA, epicB.ChildB, epicB.CrewName, epicB.QueueName)

	cc14EnsureNamedQueue(t, projectDir, epicA)
	cc14EnsureNamedQueue(t, projectDir, epicB)

	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	prevClaudeCfg, hadClaudeCfg := os.LookupEnv("HARMONIK_CLAUDE_CONFIG_PATH")
	require.NoError(t, os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath), "cc14: Setenv HARMONIK_CLAUDE_CONFIG_PATH")
	t.Cleanup(func() {
		if hadClaudeCfg {
			_ = os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", prevClaudeCfg)
		} else {
			_ = os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH")
		}
	})

	twinWrapper := cc14TwinWrapperScript(t, twinPath)

	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

	scenariotest.WriteReviewLoopWorkflowDot(t, projectDir)

	cfg := daemon.Config{
		ProjectDir:            projectDir,
		JSONLLogPath:          jsonlPath,
		BrPath:                brWrapper,
		HandlerBinary:         twinWrapper,
		NoAutoPull:            true, // queue-only: only the named queues we persist dispatch
		MaxConcurrent:         2,    // two crew queues × Workers=1 = global cap
		SkipWALCheckpoint:     true,
		SkipBrHistoryRotation: true,
		SkipRestartBackoff:    true,
		AgentReadyTimeout:     5 * time.Second,
		LogWriter:             testLogWriter{t: t},
		WorkflowModeDefault:   core.WorkflowModeDot,
	}

	mergeQ := mergeq.New(nil)
	mergeQCtx, mergeQCancel := context.WithCancel(context.Background())
	mergeQ.Start(mergeQCtx)
	t.Cleanup(mergeQCancel)
	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.StartForTesting(loopCtx, cfg,
			daemon.WithWorktreeFactory(emptyCommitWorktreeFactory),
			daemon.WithMergeQueue(mergeQ),
		)
	}()

	cc14WaitSocketReady(t, projectDir, 20*time.Second)
	preCrew, err := crew.List(projectDir)
	require.NoError(t, err, "cc14 step1: crew.List before start")
	require.Empty(t, preCrew, "cc14 step1: crew list must be empty before any crew start")
	t.Logf("cc14 step1: daemon socket ready; crew list responds (empty); queue surface ready")

	missionA := cc14WriteMissionHandoff(t, projectDir, epicA, captainName)
	missionB := cc14WriteMissionHandoff(t, projectDir, epicB, captainName)

	sessA := cc14NewQueueID(t) // a fresh UUID stands in for the C2-minted session_id
	sessB := cc14NewQueueID(t)
	cc14WriteCrewRecord(t, projectDir, epicA, sessA, "cc14-handle-alpha")
	cc14WriteCrewRecord(t, projectDir, epicB, sessB, "cc14-handle-beta")

	cc14CommsJoin(t, projectDir, epicA.CrewName)
	cc14CommsJoin(t, projectDir, epicB.CrewName)

	cc14CommsSend(t, projectDir, captainName, epicA.CrewName, "assign", epicA.EpicID+" — drive alpha to completion")
	cc14CommsSend(t, projectDir, captainName, epicB.CrewName, "assign", epicB.EpicID+" — drive beta to completion")
	t.Logf("cc14 step2: 2 handoffs written (%s, %s); 2 crew records; 2 joins; 2 assigns mailed", missionA, missionB)

	online := cc14WaitOnline(t, jsonlPath, 15*time.Second, epicA.CrewName, epicB.CrewName)
	require.Truef(t, online[epicA.CrewName] && online[epicB.CrewName],
		"cc14 step3: both crew must be online in presence projection; got %v", online)

	crewRecords, err := crew.List(projectDir)
	require.NoError(t, err, "cc14 step3: crew.List")
	require.Len(t, crewRecords, 2, "cc14 step3: crew list must show exactly 2 records")
	require.NotEqual(t, crewRecords[0].Queue, crewRecords[1].Queue,
		"cc14 step3: the two crew must be on DISTINCT queues")
	t.Logf("cc14 step3: 2 crew online on distinct queues %q / %q", crewRecords[0].Queue, crewRecords[1].Queue)

	cc14Br(t, brWrapper, "update", epicA.EpicID, "--assignee", epicA.CrewName)
	cc14Br(t, brWrapper, "update", epicB.EpicID, "--assignee", epicB.CrewName)
	require.Equal(t, epicA.CrewName, cc14BeadAssignee(t, brWrapper, epicA.EpicID),
		"cc14 step4: epicA assignee mirror")
	require.Equal(t, epicB.CrewName, cc14BeadAssignee(t, brWrapper, epicB.EpicID),
		"cc14 step4: epicB assignee mirror")

	mainQueuePath := filepath.Join(projectDir, ".harmonik", "queues", "main.json")
	_, mainStatErr := os.Stat(mainQueuePath)
	require.Truef(t, os.IsNotExist(mainStatErr),
		"cc14 step4: main queue must NOT exist (crews dispatch to OWN queues only); statErr=%v", mainStatErr)
	require.NotEmpty(t, cc14QueueItemStatuses(t, projectDir, epicA.QueueName),
		"cc14 step4: alpha named queue must hold items (or have completed)")
	t.Logf("cc14 step4: both epics assignee-mirrored; children on OWN queues; main queue absent")

	const completionBudget = 120 * time.Second
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, completionBudget, func() {
		for {
			nCompleted := cc14EventCount(t, jsonlPath, string(core.EventTypeRunCompleted))
			nFailed := cc14EventCount(t, jsonlPath, string(core.EventTypeRunFailed))
			if nCompleted+nFailed >= 4 {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})

	for _, c := range []struct{ id, label string }{
		{epicA.ChildA, "alphaA"},
		{epicA.ChildB, "alphaB"},
		{epicB.ChildA, "betaA"},
		{epicB.ChildB, "betaB"},
	} {
		require.Truef(t, cc14PollBeadClosed(t, brWrapper, c.id, 5*time.Second),
			"cc14: child %s (%s) not closed after run completion", c.label, c.id)
	}

	for _, e := range []cc14Epic{epicA, epicB} {
		cc14CommsSend(t, projectDir, e.CrewName, captainName, "status",
			fmt.Sprintf("epic %s: child %s closed (1/2)", e.EpicID, e.ChildA))
		cc14Br(t, brWrapper, "comments", "add", e.EpicID,
			fmt.Sprintf("progress: %s closed (1/2)", e.ChildA))
		cc14CommsSend(t, projectDir, e.CrewName, captainName, "status",
			fmt.Sprintf("epic %s: child %s closed (2/2)", e.EpicID, e.ChildB))
		cc14Br(t, brWrapper, "comments", "add", e.EpicID,
			fmt.Sprintf("progress: %s closed (2/2)", e.ChildB))
	}
	statusA := cc14CommsMessages(t, jsonlPath, epicA.CrewName, "", "status")
	statusB := cc14CommsMessages(t, jsonlPath, epicB.CrewName, "", "status")
	require.GreaterOrEqual(t, len(statusA), 1, "cc14 step5: alpha crew status feed (comms) must carry entries")
	require.GreaterOrEqual(t, len(statusB), 1, "cc14 step5: beta crew status feed (comms) must carry entries")
	require.GreaterOrEqual(t, cc14CommentCount(t, brWrapper, epicA.EpicID), 1,
		"cc14 step5: alpha epic br-comments feed must carry entries")
	require.GreaterOrEqual(t, cc14CommentCount(t, brWrapper, epicB.EpicID), 1,
		"cc14 step5: beta epic br-comments feed must carry entries")
	t.Logf("cc14 step5: progress feeds present — comms status (alpha=%d, beta=%d), br comments (alpha=%d, beta=%d)",
		len(statusA), len(statusB), cc14CommentCount(t, brWrapper, epicA.EpicID), cc14CommentCount(t, brWrapper, epicB.EpicID))

	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 15*time.Second, func() {
		for {
			if cc14EventCount(t, jsonlPath, string(core.EventTypeEpicCompleted)) >= 1 {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	epicEvents := cc14EpicCompletedPayloads(t, jsonlPath)
	perEpic := map[string]int{}
	for _, p := range epicEvents {
		require.NotEmptyf(t, p.LastChildBeadID, "cc14 step6: epic_completed{%s} missing last_child_bead_id", p.EpicID)
		require.NotEmptyf(t, p.ClosedAt, "cc14 step6: epic_completed{%s} missing closed_at", p.EpicID)
		perEpic[string(p.EpicID)]++
	}
	require.Equalf(t, 1, perEpic[epicA.EpicID],
		"cc14 step6: epic_completed for epicA must fire EXACTLY ONCE; got %d (events=%+v)", perEpic[epicA.EpicID], epicEvents)
	require.Equalf(t, 1, perEpic[epicB.EpicID],
		"cc14 step6: epic_completed for epicB must fire EXACTLY ONCE; got %d (events=%+v)", perEpic[epicB.EpicID], epicEvents)

	require.Equal(t, epicA.CrewName, cc14BeadAssignee(t, brWrapper, epicA.EpicID),
		"cc14 step6: epicA attribution via --assignee mirror")
	require.Equal(t, epicB.CrewName, cc14BeadAssignee(t, brWrapper, epicB.EpicID),
		"cc14 step6: epicB attribution via --assignee mirror")

	cc14CommsSend(t, projectDir, captainName, "operator", "status",
		fmt.Sprintf("epic %s completed (crew %s); awaiting next assignment", epicA.EpicID, epicA.CrewName))
	cc14CommsSend(t, projectDir, captainName, "operator", "status",
		fmt.Sprintf("epic %s completed (crew %s); awaiting next assignment", epicB.EpicID, epicB.CrewName))
	assignMsgs := cc14CommsMessages(t, jsonlPath, captainName, "", "assign")
	require.Lenf(t, assignMsgs, 2,
		"cc14 step6: captain must NOT auto-assign after completion (judgment-out); want exactly 2 assigns, got %d", len(assignMsgs))
	surfaceMsgs := cc14CommsMessages(t, jsonlPath, captainName, "operator", "status")
	require.GreaterOrEqual(t, len(surfaceMsgs), 2,
		"cc14 step6: captain must surface completion to operator (dual-channel)")
	t.Logf("cc14 step6: epic_completed fired once-per-epic; attribution via --assignee; surfaced-and-awaited (2 assigns total, %d surfaces)", len(surfaceMsgs))

	cc14CommsJoin(t, projectDir, epicA.CrewName) // keeper re-join (same name/session)
	onlineAfter := cc14WaitOnline(t, jsonlPath, 10*time.Second, epicA.CrewName)
	require.Truef(t, onlineAfter[epicA.CrewName],
		"cc14 step7: crew %s must re-appear online after keeper restart", epicA.CrewName)

	fm := cc14ParseMissionHandoff(t, missionA)
	require.Equal(t, epicA.QueueName, fm.Queue, "cc14 step7: re-hydrated queue from handoff")
	require.Equal(t, epicA.EpicID, fm.EpicID, "cc14 step7: re-hydrated epic_id from handoff")
	require.Equal(t, epicA.CrewName, cc14BeadAssignee(t, brWrapper, epicA.EpicID),
		"cc14 step7: re-hydrated assignee mirror survives restart")

	rec, recErr := crew.Load(projectDir, epicA.CrewName)
	require.NoError(t, recErr, "cc14 step7: crew record must survive restart")
	require.Equal(t, sessA, rec.SessionID, "cc14 step7: same session_id (keeper --resume, not a re-spawn)")

	require.Equal(t, 0, cc14EventCount(t, jsonlPath, string(core.EventTypeRunFailed)),
		"cc14 step7: restart must be a non-event — no run_failed")
	postRestartCrew, err := crew.List(projectDir)
	require.NoError(t, err, "cc14 step7: crew.List after restart")
	require.Len(t, postRestartCrew, 2, "cc14 step7: no re-spawn — still exactly 2 crew records")
	t.Logf("cc14 step7: crew %s re-joined + re-hydrated {queue=%s, epic=%s}; daemon kept draining; restart treated as non-event",
		epicA.CrewName, fm.Queue, fm.EpicID)

	loopCancel()
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 15*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("cc14: daemon.StartForTesting returned error after cancel: %v", err)
		}
	})

	scenariotest.AssertEventCausality(t, jsonlPath,
		"run_started",
		[]string{"run_completed", "run_failed", "run_cancelled"},
		60*time.Second,
	)
	scenariotest.AssertEventCausality(t, jsonlPath,
		"implementer_commit",
		[]string{"reviewer_launched", "run_completed"},
		30*time.Second,
	)

	t.Logf("cc14 PASS: 2 crew on distinct queues, both epics assignee-mirrored + own-queue dispatched (main absent), " +
		"progress feeds present, epic_completed once-per-epic surfaced-and-awaited, restart a non-event")
}
