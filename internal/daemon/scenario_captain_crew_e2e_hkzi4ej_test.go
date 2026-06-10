//go:build scenario

package daemon_test

// scenario_captain_crew_e2e_hkzi4ej_test.go — the DEFINITIVE end-to-end smoke for
// the Captain & Crew system (bead hk-zi4ej / "T14").
//
// This is the single scenario that exercises all four Captain & Crew components
// against each other on the live daemon composition root, implementing the
// 7-step §6 "definitive end-to-end smoke" from
// docs/plans/captain/06-integration.md:
//
//   1. Boot a daemon (via daemon.StartForTesting) on a temp project; assert the
//      crew-registry read surface (crew.List) and the queue surface respond.
//   2. Simulate the Captain's MECHANICAL actions (scripted — there is no real
//      LLM): create TWO distinct epics, each with ≥2 ready child beads; write two
//      C3 mission handoffs; perform two crew "starts" (registry record +
//      named-queue ensure + a comms-presence JOIN over the live socket — the C2
//      crew-start op's real claude --remote-control launch is OUT of a hermetic
//      test's reach, so the start's MECHANICAL effects are scripted, exactly as
//      the brief and the C2 unit tests treat the launch); mail each crew its epic
//      assignment via comms-send --topic assign.
//   3. Assert ≥2 crew come up on DISTINCT queues — two presence JOINs, both crew
//      online in the presence projection, two distinct crew.List records
//      (criteria #1, #6).
//   4. Assert each assignment lands — each crew (scripted) runs
//      `br update <epic> --assignee <crew>` (the Gap-1 durable mirror) and
//      dispatches its epic's ready children to its OWN named queue; assert NOTHING
//      lands in the `main` queue (criterion #2).
//   5. Assert progress feeds update — comms-send --topic status (crew→captain) AND
//      `br comments add <epic>` both get entries as beads run (criterion #3).
//   6. Assert `epic_completed` fires EXACTLY ONCE when one epic's last child
//      merges+closes (daemon-owned close → maybeEmitEpicCompleted); the Captain's
//      subscribe-equivalent (a JSONL scan filtered to epic_completed) receives it;
//      attribution via `br show <epic> --assignee` (the durable mirror, NOT the
//      crew registry Record.Epic); and the Captain SURFACES-AND-AWAITS — does NOT
//      auto-assign the next epic (criterion #4 + judgment-out boundary).
//   7. Assert restart-continuity (criterion #5) — simulate a keeper cycle via a
//      re-JOIN of one crew under the SAME name (the C2 `--resume <uuid>` path
//      re-runs the boot sequence; a hermetic test cannot relaunch a real claude
//      pane, so the restart's OBSERVABLE effect — the crew re-appearing online and
//      re-hydrating {queue, epic_id} from the handoff + `--assignee` mirror — is
//      driven directly, mirroring scenario_restart_recovery_ivzsl_test.go's
//      simulate-the-effect approach); the daemon kept draining; the Captain treats
//      the restart as a non-event (no failure-surface, no re-spawn).
//
// # Why the real C2 crew-start op is NOT invoked
//
// `harmonik crew start` (the crew-start socket op) launches an interactive
// `claude --remote-control` tmux pane and bracketed-pastes a mission seed —
// neither is reachable from a hermetic `go test` (no real claude, no live tmux,
// and the brief forbids touching the live crew sessions). The C2 unit tests
// (crewstart_hkjzpqo_test.go, crewlaunchspec_test.go) likewise assert the
// launch's CODE SEQUENCE with mock injecters rather than a live launch. So this
// E2E SCRIPTS the crew-start MECHANICAL effects — the registry Write, the
// named-queue Persist, and a comms-presence JOIN (which the crew's C3 boot
// sequence performs as `harmonik comms join`) — and drives every event-bearing
// captain/crew action (presence, assign, status) over the LIVE daemon socket so
// the daemon's real comms handlers emit into the shared events.jsonl. The twin
// (harmonik-twin-claude) stands in for the per-bead IMPLEMENTER claude
// (commit-on-cue), NOT for a crew or captain.
//
// # Helper prefix
//
// Every helper/type/fixture here is namespaced with the "cc14" prefix
// (captain-crew T14) so it can never redeclare a symbol that exists in another
// scenario test file in package daemon_test (parallel-helper-collision lesson).
//
// # Spec refs
//
//   - docs/plans/captain/06-integration.md §6 (the definitive end-to-end smoke).
//   - docs/plans/captain/06-integration.md §4 Gap 1 (assignee-mirror attribution).
//   - docs/plans/captain/05-specs/c1-spec.md (epic_completed at-most-once).
//   - docs/plans/captain/05-specs/c2-spec.md §3.1, §3.3 (crew start, registry).
//   - docs/plans/captain/05-specs/c3-spec.md §3.1–§3.3 (boot, assignee mirror, progress feed).
//   - docs/plans/captain/05-specs/c4-spec.md §3.1 (captain mechanical loop).
//   - specs/queue-model.md §9 (named queues, two-level cap).
//   - agent-comms spec §1.1/§1.2/§4 (agent_message, agent_presence, presence projection).
//
// Bead: hk-zi4ej.
// Refs: hk-tfxjp (C1 scenario), hk-umemp (multi-queue), hk-ivzsl (restart),
//       hk-7t27s (comms-presence), hk-nbrmf (comms-send).

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
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	"github.com/gregberns/harmonik/internal/queue"
)

// ─────────────────────────────────────────────────────────────────────────────
// cc14 fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// cc14EvalSymlinks resolves all symlinks in path so that br — which rejects
// paths containing symlinks outside the beads directory — receives a canonical
// path. On macOS, t.TempDir() returns /var/folders/... which is a symlink to
// /private/var/folders/..., triggering br's symlink guard.
func cc14EvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err, "cc14EvalSymlinks: EvalSymlinks %q", path)
	return resolved
}

// cc14ProjectDir creates the minimal project directory for the scenario and
// returns the project dir and the JSONL events log path.
//
// The project is rooted under a SHORT /tmp path (not t.TempDir()) so that the
// daemon's Unix socket at <projectDir>/.harmonik/daemon.sock fits inside the
// macOS 104-byte sun_path limit — t.TempDir() on macOS yields a ~90-char
// /private/var/folders/... path that overflows the limit and makes the daemon's
// (non-fatal) socket bind silently fail, leaving comms/crew RPC undialable. This
// mirrors socketFixtureTempSockPath's strategy.
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

// cc14GitRepo initialises a git repository with one commit in dir and wires a
// bare-repo "origin" so mergeRunBranchToMain's git-push step succeeds (avoiding
// push_failed run_failed events when the twin's run is merged).
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

// cc14BrPath returns the path to the real `br` binary, skipping the test when
// br is not on PATH.
func cc14BrPath(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("cc14: br required for scenario test (not on PATH)")
	}
	return brPath
}

// cc14BrWrapperScript writes a /bin/sh wrapper that invokes realBrPath with
// --db <dbPath> prepended to all args. Returns the wrapper path. The daemon and
// the scripted crew/captain br calls all go through this wrapper so they share
// one beads DB.
func cc14BrWrapperScript(t *testing.T, realBrPath, dbPath string) string {
	t.Helper()
	dir := cc14EvalSymlinks(t, t.TempDir())
	path := filepath.Join(dir, "br")
	content := "#!/bin/sh\nexec " + realBrPath + " --db " + dbPath + " \"$@\"\n"
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755), "cc14BrWrapperScript: WriteFile")
	return path
}

// cc14Br runs the br wrapper with args and fails the test on a non-zero exit.
// Returns trimmed stdout+stderr.
func cc14Br(t *testing.T, brWrapper string, args ...string) string {
	t.Helper()
	//nolint:gosec // G204: br args are test-internal literals; not user input
	cmd := exec.CommandContext(t.Context(), brWrapper, args...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "cc14Br: br %v\n%s", args, out)
	return strings.TrimSpace(string(out))
}

// cc14CreateBead creates an open bead and returns its minted id.
func cc14CreateBead(t *testing.T, brWrapper, title string) string {
	t.Helper()
	id := cc14Br(t, brWrapper, "create", title, "--status", "open", "--silent")
	require.NotEmpty(t, id, "cc14CreateBead: empty id for %q", title)
	return id
}

// cc14Epic holds one epic and its two child bead ids.
type cc14Epic struct {
	EpicID    string
	ChildA    string
	ChildB    string
	QueueName string
	QueueID   string
	CrewName  string
}

// cc14SeedEpic creates an epic bead plus two child beads, wires the two
// parent-child edges (child depends-on epic via `br dep add <child> <epic>
// --type parent-child` — the direction that yields, in the daemon's br ShowBead
// edge model, an OUTGOING parent-child edge on each child pointing at the epic,
// which maybeEmitEpicCompleted walks to find the parent). Returns the populated
// cc14Epic (QueueName/CrewName filled in by the caller).
func cc14SeedEpic(t *testing.T, brWrapper, label string) cc14Epic {
	t.Helper()
	epic := cc14Br(t, brWrapper, "create", "cc14 epic "+label, "--type", "epic", "--status", "open", "--silent")
	require.NotEmpty(t, epic, "cc14SeedEpic: empty epic id for %q", label)
	childA := cc14CreateBead(t, brWrapper, "cc14 child A of "+label)
	childB := cc14CreateBead(t, brWrapper, "cc14 child B of "+label)
	// child depends-on epic via parent-child (verified empirically: yields the
	// child's outgoing parent-child edge → parent in br show dependencies[]).
	cc14Br(t, brWrapper, "dep", "add", childA, epic, "--type", "parent-child")
	cc14Br(t, brWrapper, "dep", "add", childB, epic, "--type", "parent-child")
	return cc14Epic{EpicID: epic, ChildA: childA, ChildB: childB}
}

// cc14NewQueueID returns a fresh UUIDv7 string for a named queue's QueueID.
func cc14NewQueueID(t *testing.T) string {
	t.Helper()
	u, err := uuid.NewV7()
	require.NoError(t, err, "cc14NewQueueID: NewV7")
	return u.String()
}

// cc14WriteMissionHandoff writes a C3-schema mission handoff at the canonical
// path .harmonik/crew/missions/<crew_name>.md (Markdown + YAML frontmatter,
// schema_version 1), as the Captain (C4) authors it. The crew (C3) re-hydrates
// {queue, epic_id} from this file on boot and on keeper restart.
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

// cc14MissionFrontmatter is the subset of the C3 handoff frontmatter the crew
// re-hydrates from on boot/restart. Parsed by cc14ParseMissionHandoff.
type cc14MissionFrontmatter struct {
	CrewName string
	Queue    string
	EpicID   string
}

// cc14ParseMissionHandoff reads a mission handoff file and extracts the
// {crew_name, queue, epic_id} frontmatter — the exact re-hydration step the crew
// performs on (re)boot. Trivial line-based YAML reader (no external dep).
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

// cc14WriteCrewRecord scripts the registry-write half of C2's crew start: the
// daemon's crew-start op writes .harmonik/crew/<name>.json BEFORE launch. We do
// the same via crew.Write so `crew list` and the C4 attribution path observe the
// record. Note Record.Epic is the SPAWN-TIME epic and may go stale on a comms
// re-task — which is precisely WHY §4 Gap 1 keys attribution on the durable
// `br show --assignee` mirror, not Record.Epic.
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

// cc14EnsureNamedQueue scripts the queue-ensure half of C2's crew start (and the
// daemon's own "persist a minimal Queue{Name, Workers:1} if absent"): it writes
// an ACTIVE wave queue named e.QueueName holding the epic's two children as
// pending items. The daemon's StartForTesting enumerates and drains every named
// queue under .harmonik/queues/ (proven by hk-umemp), so the children dispatch on
// the crew's OWN queue, never main.
//
// Workers=1 per queue × two queues = MaxConcurrent=2 fills the global ceiling.
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

// cc14TwinWrapperScript writes the phase-aware twin wrapper (same shape as the
// hk-umemp multi-queue scenario): implementer phase runs the twin's
// single-happy-path (no git commit — the empty-commit worktree factory
// pre-commits); reviewer phase writes an APPROVE verdict so the review loop
// terminates with run_completed + bead close.
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
exec "` + twinPath + `" --scenario single-happy-path
`
	//nolint:gosec // G306: script is test-only; chmod 0755 required for execution
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755), "cc14TwinWrapperScript: WriteFile")
	return path
}

// ─────────────────────────────────────────────────────────────────────────────
// cc14 socket helpers — drive the LIVE daemon socket (bound by StartForTesting)
// ─────────────────────────────────────────────────────────────────────────────

// cc14SockPath returns the daemon socket path that daemon.StartForTesting binds:
// <projectDir>/.harmonik/daemon.sock.
func cc14SockPath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "daemon.sock")
}

// cc14WaitSocketReady polls until the daemon socket accepts connections.
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

// cc14SocketOp dials the live daemon socket, sends a SocketRequest{op,payload},
// and returns the decoded SocketResponse. Fails the test on dial/IO error.
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

// cc14CommsJoin drives a comms-presence JOIN for agentName over the live socket
// (the crew's C3 boot step `harmonik comms join`, AND the keeper-restart re-join).
func cc14CommsJoin(t *testing.T, projectDir, agentName string) {
	t.Helper()
	resp := cc14SocketOp(t, projectDir, "comms-presence", map[string]any{
		"agent":  agentName,
		"status": "online",
		"reason": "join",
	})
	require.True(t, resp.Ok, "cc14CommsJoin(%s): Ok=false: %s", agentName, resp.Error)
}

// cc14CommsSend drives a comms-send over the live socket (captain→crew assign,
// crew→captain status). Returns the minted event_id.
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

// ─────────────────────────────────────────────────────────────────────────────
// cc14 JSONL / presence / comms assertions (the Captain's read surfaces)
// ─────────────────────────────────────────────────────────────────────────────

// cc14ScanEvents decodes every non-empty JSONL line at jsonlPath into typed
// envelopes carrying the fields cc14 assertions need.
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

// cc14EventCount counts events of eventType in the JSONL log.
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

// cc14OnlineAgents returns the set of agents currently "online" in the presence
// projection over events.jsonl. This mirrors what `harmonik comms who` computes:
// an agent is online when its latest agent_presence beat has status="online".
// (The CLI's TTL is generous — 120s — so within a test run a joined agent is
// always online; we apply the same latest-beat-wins fold and require online.)
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

// cc14WaitOnline polls until all wantAgents are online in the presence
// projection, up to budget. Returns the final online set.
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

// cc14CommsMessages returns the agent_message payloads matching from/to/topic
// filters (empty filter = match any). This is the `comms log --from X --topic Y`
// read surface.
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

// cc14EpicCompletedPayloads returns the decoded epic_completed payloads.
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

// ─────────────────────────────────────────────────────────────────────────────
// cc14 br read surfaces (assignee mirror, comments feed, bead status)
// ─────────────────────────────────────────────────────────────────────────────

// cc14BeadAssignee returns the assignee field from `br show <id> --format json`.
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

// cc14BeadStatus returns the status field from `br show <id> --format json`.
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

// cc14CommentCount returns the number of comments on a bead via
// `br comments list <id> --json` (the progress-feed read surface).
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

// cc14PollBeadClosed polls `br show <id>` until the bead reaches "closed", up to
// budget.
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

// cc14QueueItemStatuses reads .harmonik/queues/<name>.json and returns the
// group-0 item statuses in order. Returns nil when the file is absent (the queue
// completed and was unlinked — acceptable).
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

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_CaptainCrewE2E_hkzi4ej — the definitive 7-step end-to-end smoke
// ─────────────────────────────────────────────────────────────────────────────

// Not parallel: uses os.Setenv(HARMONIK_CLAUDE_CONFIG_PATH) to isolate
// EnsureWorktreeTrust — same rationale as TestScenario_HappyPath_N1 and the
// hk-umemp multi-queue scenario.
//
// Bead: hk-zi4ej.
func TestScenario_CaptainCrewE2E_hkzi4ej(t *testing.T) {
	const captainName = "cc14-captain"

	// ── Preflight: locate twin + br ──────────────────────────────────────────
	twinPath, ok := scenariotest.TwinBinaryPath()
	if !ok {
		t.Skip("cc14: harmonik-twin-claude binary not found; set HARMONIK_TWIN_CLAUDE or build the binary")
	}
	realBrPath := cc14BrPath(t)

	// ── Project + git + br DB ────────────────────────────────────────────────
	projectDir, jsonlPath := cc14ProjectDir(t)
	cc14GitRepo(t, projectDir)
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper := cc14BrWrapperScript(t, realBrPath, dbPath)

	//nolint:gosec // G204: br args are test-internal literals; not user input
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "cc14")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	require.NoError(t, initErr, "cc14: br init: %s", initOut)

	// ── Step 2 (setup half): two distinct epics, each with 2 ready children ──
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

	// Pre-seed each crew's OWN named queue on disk BEFORE booting the daemon, so
	// daemon.StartForTesting's LoadQueueAtStartup (PL-005 step 8a) enumerates them
	// and the work loop dispatches their children. This is the mechanical effect
	// of the crew's `harmonik queue submit --queue <q>` step (criterion #2); it is
	// done pre-boot because the daemon does NOT re-enumerate .harmonik/queues/ when
	// already idle (the hk-24xn1 idle-wake gap), so a queue submitted after boot
	// would sit unread until the next tick. The hk-umemp multi-queue scenario
	// pre-seeds for the same reason. Nothing is seeded into the `main` queue —
	// criterion #2's "never main" is asserted in step 4.
	cc14EnsureNamedQueue(t, projectDir, epicA)
	cc14EnsureNamedQueue(t, projectDir, epicB)

	// Isolate EnsureWorktreeTrust from the live ~/.claude.json (and the running
	// daemon's lock) — same as the other scenario tests.
	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	prevClaudeCfg, hadClaudeCfg := os.LookupEnv("HARMONIK_CLAUDE_CONFIG_PATH")
	require.NoError(t, os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath), "cc14: Setenv HARMONIK_CLAUDE_CONFIG_PATH")
	// hk-1o0cc: restore prior value (TestMain package default) — see scenario_happypath_n1.
	t.Cleanup(func() {
		if hadClaudeCfg {
			_ = os.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", prevClaudeCfg)
		} else {
			_ = os.Unsetenv("HARMONIK_CLAUDE_CONFIG_PATH")
		}
	})

	twinWrapper := cc14TwinWrapperScript(t, twinPath)

	// ── Step 1: boot the daemon on the temp project ──────────────────────────
	//
	// daemon.StartForTesting binds the unix socket at
	// <projectDir>/.harmonik/daemon.sock and writes all events (run_*, bead_closed,
	// epic_completed, agent_message, agent_presence) to jsonlPath. The empty-commit
	// worktree factory satisfies the no-commit guard; the merge mutex serialises
	// concurrent merges to the shared bare-repo origin.
	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()

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
		WorkflowModeDefault:   core.WorkflowModeReviewLoop,
	}

	var mergeMu sync.Mutex
	startDone := make(chan error, 1)
	go func() {
		startDone <- daemon.StartForTesting(loopCtx, cfg,
			daemon.WithWorktreeFactory(emptyCommitWorktreeFactory),
			daemon.WithMergeMutex(&mergeMu),
		)
	}()

	// Step 1 assertion: the daemon's socket (crew/queue/comms RPC surface) responds.
	cc14WaitSocketReady(t, projectDir, 20*time.Second)
	// crew list responds (read-only, daemon-independent): empty before any start.
	preCrew, err := crew.List(projectDir)
	require.NoError(t, err, "cc14 step1: crew.List before start")
	require.Empty(t, preCrew, "cc14 step1: crew list must be empty before any crew start")
	t.Logf("cc14 step1: daemon socket ready; crew list responds (empty); queue surface ready")

	// ── Step 2 (captain mechanical actions, scripted): handoffs, starts, mail ──
	//
	// For each epic: write a C3 mission handoff, script the crew-start MECHANICAL
	// effects (registry record + comms-presence JOIN — the named-queue ensure ran
	// pre-boot above), then mail the crew its assignment via comms-send --topic
	// assign. All event-bearing actions (JOIN, assign) go over the LIVE daemon
	// socket so the daemon's real comms handlers emit into the shared events.jsonl.
	missionA := cc14WriteMissionHandoff(t, projectDir, epicA, captainName)
	missionB := cc14WriteMissionHandoff(t, projectDir, epicB, captainName)

	sessA := cc14NewQueueID(t) // a fresh UUID stands in for the C2-minted session_id
	sessB := cc14NewQueueID(t)
	cc14WriteCrewRecord(t, projectDir, epicA, sessA, "cc14-handle-alpha")
	cc14WriteCrewRecord(t, projectDir, epicB, sessB, "cc14-handle-beta")

	// Crew C3 boot step: comms join (now visible in `comms who`).
	cc14CommsJoin(t, projectDir, epicA.CrewName)
	cc14CommsJoin(t, projectDir, epicB.CrewName)

	// Captain (C4) mails each crew its epic assignment over the bus.
	cc14CommsSend(t, projectDir, captainName, epicA.CrewName, "assign", epicA.EpicID+" — drive alpha to completion")
	cc14CommsSend(t, projectDir, captainName, epicB.CrewName, "assign", epicB.EpicID+" — drive beta to completion")
	t.Logf("cc14 step2: 2 handoffs written (%s, %s); 2 crew records; 2 joins; 2 assigns mailed", missionA, missionB)

	// ── Step 3: ≥2 crew come up on DISTINCT queues (criteria #1, #6) ──────────
	online := cc14WaitOnline(t, jsonlPath, 15*time.Second, epicA.CrewName, epicB.CrewName)
	require.Truef(t, online[epicA.CrewName] && online[epicB.CrewName],
		"cc14 step3: both crew must be online in presence projection; got %v", online)

	crewRecords, err := crew.List(projectDir)
	require.NoError(t, err, "cc14 step3: crew.List")
	require.Len(t, crewRecords, 2, "cc14 step3: crew list must show exactly 2 records")
	require.NotEqual(t, crewRecords[0].Queue, crewRecords[1].Queue,
		"cc14 step3: the two crew must be on DISTINCT queues")
	t.Logf("cc14 step3: 2 crew online on distinct queues %q / %q", crewRecords[0].Queue, crewRecords[1].Queue)

	// ── Step 4: each assignment lands — assignee mirror + own-queue, never main ─
	//
	// The crew (scripted) mirrors the Gap-1 assignee on adopt. The epic's ready
	// children were already submitted to the crew's OWN queue in step 2
	// (cc14EnsureNamedQueue is the `queue submit --queue <q>` effect); we assert
	// no main queue exists.
	cc14Br(t, brWrapper, "update", epicA.EpicID, "--assignee", epicA.CrewName)
	cc14Br(t, brWrapper, "update", epicB.EpicID, "--assignee", epicB.CrewName)
	require.Equal(t, epicA.CrewName, cc14BeadAssignee(t, brWrapper, epicA.EpicID),
		"cc14 step4: epicA assignee mirror")
	require.Equal(t, epicB.CrewName, cc14BeadAssignee(t, brWrapper, epicB.EpicID),
		"cc14 step4: epicB assignee mirror")

	// Nothing in the main queue: .harmonik/queues/main.json must be absent.
	mainQueuePath := filepath.Join(projectDir, ".harmonik", "queues", "main.json")
	_, mainStatErr := os.Stat(mainQueuePath)
	require.Truef(t, os.IsNotExist(mainStatErr),
		"cc14 step4: main queue must NOT exist (crews dispatch to OWN queues only); statErr=%v", mainStatErr)
	// Both named queues exist on disk.
	require.NotEmpty(t, cc14QueueItemStatuses(t, projectDir, epicA.QueueName),
		"cc14 step4: alpha named queue must hold items (or have completed)")
	t.Logf("cc14 step4: both epics assignee-mirrored; children on OWN queues; main queue absent")

	// ── Step 6 (set up the subscription before completion): the captain runs
	// `subscribe --types epic_completed`. We assert the durable event after it
	// fires; the daemon owns the close + emit. ──

	// Wait for ALL four children to dispatch, complete, and close (daemon-owned).
	// Budget: AgentReadyTimeout(5s) × 4 runs + review + merge overhead + headroom.
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

	// All four children must be closed.
	for _, c := range []struct{ id, label string }{
		{epicA.ChildA, "alphaA"},
		{epicA.ChildB, "alphaB"},
		{epicB.ChildA, "betaA"},
		{epicB.ChildB, "betaB"},
	} {
		require.Truef(t, cc14PollBeadClosed(t, brWrapper, c.id, 5*time.Second),
			"cc14: child %s (%s) not closed after run completion", c.label, c.id)
	}

	// ── Step 5: progress feeds update (criterion #3) ─────────────────────────
	//
	// As beads close, each crew (scripted) emits BOTH surfaces: comms-send
	// --topic status (crew→captain) AND br comments add <epic>. We perform the
	// crew's progress-emit for each closed bead now (post-close, mirroring the
	// crew's "emit on observed bead close" rule), then assert both feeds carry
	// entries.
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
	// comms log --from <crew> --topic status carries entries for each crew.
	statusA := cc14CommsMessages(t, jsonlPath, epicA.CrewName, "", "status")
	statusB := cc14CommsMessages(t, jsonlPath, epicB.CrewName, "", "status")
	require.GreaterOrEqual(t, len(statusA), 1, "cc14 step5: alpha crew status feed (comms) must carry entries")
	require.GreaterOrEqual(t, len(statusB), 1, "cc14 step5: beta crew status feed (comms) must carry entries")
	// br comments list <epic> carries entries.
	require.GreaterOrEqual(t, cc14CommentCount(t, brWrapper, epicA.EpicID), 1,
		"cc14 step5: alpha epic br-comments feed must carry entries")
	require.GreaterOrEqual(t, cc14CommentCount(t, brWrapper, epicB.EpicID), 1,
		"cc14 step5: beta epic br-comments feed must carry entries")
	t.Logf("cc14 step5: progress feeds present — comms status (alpha=%d, beta=%d), br comments (alpha=%d, beta=%d)",
		len(statusA), len(statusB), cc14CommentCount(t, brWrapper, epicA.EpicID), cc14CommentCount(t, brWrapper, epicB.EpicID))

	// ── Step 6: epic_completed fires EXACTLY ONCE per epic; captain surfaces ──
	//
	// When the daemon closes the LAST child of an epic, the close site calls
	// emitBeadClosedAndMaybeEpic → maybeEmitEpicCompleted, which (all children
	// closed) emits epic_completed exactly once (at-most-once emittedEpics guard).
	// Both epics complete here (all 4 children closed), so we expect ≥1 emit per
	// epic and EXACTLY ONE per epic.
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
	// EXACTLY ONCE per epic (criterion #4 — at-most-once guard).
	require.Equalf(t, 1, perEpic[epicA.EpicID],
		"cc14 step6: epic_completed for epicA must fire EXACTLY ONCE; got %d (events=%+v)", perEpic[epicA.EpicID], epicEvents)
	require.Equalf(t, 1, perEpic[epicB.EpicID],
		"cc14 step6: epic_completed for epicB must fire EXACTLY ONCE; got %d (events=%+v)", perEpic[epicB.EpicID], epicEvents)

	// Captain attributes epic_completed via the DURABLE br --assignee mirror,
	// NOT the registry's spawn-time Record.Epic (§4 Gap 1).
	require.Equal(t, epicA.CrewName, cc14BeadAssignee(t, brWrapper, epicA.EpicID),
		"cc14 step6: epicA attribution via --assignee mirror")
	require.Equal(t, epicB.CrewName, cc14BeadAssignee(t, brWrapper, epicB.EpicID),
		"cc14 step6: epicB attribution via --assignee mirror")

	// Captain SURFACES-AND-AWAITS: it surfaces via comms send --to operator
	// --topic status, and does NOT auto-assign a next epic. We script the surface
	// and assert the judgment-out boundary: no NEW assign message beyond the two
	// original assignments exists.
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

	// ── Step 7: restart-continuity (criterion #5) ────────────────────────────
	//
	// Simulate a keeper cycle on crew-alpha via the C2 `--resume <uuid>` path:
	// the keeper resumes the SAME session_id, and the crew re-runs its boot
	// sequence — re-JOIN comms and re-derive {queue, epic_id} from the handoff
	// frontmatter AND the durable `br show <epic> --assignee` mirror. A hermetic
	// test cannot relaunch a real claude pane, so we drive the restart's
	// OBSERVABLE effects directly (mirroring scenario_restart_recovery_ivzsl):
	//   (a) the crew re-appears online under the SAME name,
	//   (b) it re-hydrates queue+epic from the handoff + assignee mirror (we
	//       assert the re-derivation yields the original assignment),
	//   (c) the daemon kept draining across the restart (no failure, no re-spawn).
	cc14CommsJoin(t, projectDir, epicA.CrewName) // keeper re-join (same name/session)
	onlineAfter := cc14WaitOnline(t, jsonlPath, 10*time.Second, epicA.CrewName)
	require.Truef(t, onlineAfter[epicA.CrewName],
		"cc14 step7: crew %s must re-appear online after keeper restart", epicA.CrewName)

	// Re-hydration: parse the handoff and the assignee mirror; they must agree
	// with the original {queue, epic_id} — the crew continues off durable state.
	fm := cc14ParseMissionHandoff(t, missionA)
	require.Equal(t, epicA.QueueName, fm.Queue, "cc14 step7: re-hydrated queue from handoff")
	require.Equal(t, epicA.EpicID, fm.EpicID, "cc14 step7: re-hydrated epic_id from handoff")
	require.Equal(t, epicA.CrewName, cc14BeadAssignee(t, brWrapper, epicA.EpicID),
		"cc14 step7: re-hydrated assignee mirror survives restart")

	// The crew registry record still exists under the same name (no re-spawn).
	rec, recErr := crew.Load(projectDir, epicA.CrewName)
	require.NoError(t, recErr, "cc14 step7: crew record must survive restart")
	require.Equal(t, sessA, rec.SessionID, "cc14 step7: same session_id (keeper --resume, not a re-spawn)")

	// The captain treats the restart as a non-event: no run_failed and no new
	// crew records beyond the original two (no re-spawn / failure-surface).
	require.Equal(t, 0, cc14EventCount(t, jsonlPath, string(core.EventTypeRunFailed)),
		"cc14 step7: restart must be a non-event — no run_failed")
	postRestartCrew, err := crew.List(projectDir)
	require.NoError(t, err, "cc14 step7: crew.List after restart")
	require.Len(t, postRestartCrew, 2, "cc14 step7: no re-spawn — still exactly 2 crew records")
	t.Logf("cc14 step7: crew %s re-joined + re-hydrated {queue=%s, epic=%s}; daemon kept draining; restart treated as non-event",
		epicA.CrewName, fm.Queue, fm.EpicID)

	// ── Teardown: cancel the daemon and wait for clean exit ──────────────────
	loopCancel()
	scenariotest.MustCompleteWithin(t, jsonlPath, "", nil, 15*time.Second, func() {
		if err := <-startDone; err != nil {
			t.Errorf("cc14: daemon.StartForTesting returned error after cancel: %v", err)
		}
	})

	// ── Causality invariants (hk-xegej) ──────────────────────────────────────
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
