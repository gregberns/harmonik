//go:build chaos

// Package dispatch_test's chaos harness is the Slice B slice gate (task B10):
// the standing fault-injection test that runs the whole competing-consumers
// slice as a live system and asserts it loses nothing and duplicates nothing
// under load, a worker death, and a stateful-plugin reload.
//
// It is behind //go:build chaos so it never runs in the normal build or in
// `make segments`; `make chaos` is the one command that runs it, and it picks
// this file up because tools/dispatch is a SEGMENT_MODULE. It lives in the
// tools/dispatch module, not kernel/, for the same two reasons the echo gate
// does: the kernel-vocabulary check bans the domain words this harness must say
// ("dispatch", "worker", "job"), and a tool may name itself; and tool-isolation
// is not offended because the harness reaches harmonikd only as a launched
// subprocess over gRPC and HTTP, importing nothing from the kernel module.
//
// What it proves, against a real `harmonikd mesh` process built from the slice
// tip, with the dispatch plugin running as primary + two workers:
//
//   - G1 (load-balance): 10 paced jobs submitted to dispatch.submit land 5 and
//     5 across the two workers' done journals, set-equal to the primary's
//     accepted set — each job done exactly once, both workers took a share.
//   - G2 (worker-kill requeue, the C5 path): a kill -9 of one worker mid-job
//     ends with every accepted job done exactly once across the survivors — the
//     dead worker's in-flight, leased-but-unjournaled job is requeued to a
//     surviving peer, not lost.
//   - G3 (VC-12 re-run against a STATEFUL reload): jobs flowing at >= 100 msg/s,
//     the primary reloaded mid-stream to a byte-distinct binary and, separately,
//     a worker reloaded mid-stream; the accepted set and the done-union set are
//     equal afterwards (zero loss, zero duplicates by job id), the mesh process
//     PID is unchanged across the reloads, and each reload completes within the
//     echo gate's recalibrated latency budget. The dispatch plugin holds state
//     (journals), so this proves reload-without-loss for a STATEFUL plugin.
//   - G4 (four-type conformance, VC-13 full width): a dispatch-native cross-node
//     PUBSUB check plus running B9's four-type conformance cases, so the slice
//     gate covers all four channel types.
//
// Any lost or duplicated job is a HARD STOP: the assertion reports the exact
// job ids.
package dispatch_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
	dispatchpkg "github.com/gregberns/harmonik/tools/dispatch"
)

// The G3 load-generator shape mirrors the echo gate: loadCount is well above
// the 100-msg/s floor once paced over the window, and each payload is unique so
// the final check is set-equality, not a bare count.
const (
	loadCount          = 500
	loadPace           = 4 * time.Millisecond  // ~2 s of load => well above the floor
	primaryReloadAt    = loadCount / 3          // fire the primary reload after this many are sent
	workerReloadAt     = 2 * loadCount / 3      // then the worker reload
	minRate            = 100.0                  // msg/s floor the gate names
	reloadBudget       = 150 * time.Millisecond // the echo gate's recalibrated ceiling, reused (task plan Part 4)
	g2WorkDelay        = 300 * time.Millisecond // worker Deliver pause, so a kill lands mid-job
	g2JobCount         = 20
	g2SubmitPace       = 25 * time.Millisecond
)

// jobPayload returns the distinguishable submit payload for message i.
func jobPayload(i int) []byte {
	return []byte(fmt.Sprintf("slice-b-chaos-%06d", i))
}

// ---------------------------------------------------------------------------
// G1 — load-balance: 10 jobs => 5 + 5, exactly-once, set-equal to accepted.
// ---------------------------------------------------------------------------

func TestG1LoadBalanceFiveFive(t *testing.T) {
	mp := startDispatchMesh(t, threeNodeConfig(t, nil), nil)
	ctx := chaosCtx(t, 90*time.Second)

	const n = 10
	for i := 0; i < n; i++ {
		if _, err := clientPublish(ctx, mp.kernelAddr(primaryNode), dispatchpkg.SubmitChannel, jobPayload(i)); err != nil {
			t.Fatalf("submit %d: %v\n%s", i, err, mp.log())
		}
		time.Sleep(15 * time.Millisecond)
	}

	// accepted on the primary is the authoritative "sent job id" set; the done
	// journals on the two workers are what actually completed.
	accepted := waitForAcceptedCount(t, ctx, mp, n)
	perWorker := waitForDoneUnion(t, ctx, mp, accepted)

	assertExactlyOnce(t, "G1", accepted, perWorker)

	// The load-balance assertion: each worker took exactly half. Assignment is
	// deterministic round-robin over the two live members (both attached before
	// the first job — startDispatchMesh gates on all nodes listening), so this is
	// scheduler-independent.
	for _, w := range workerNodes {
		if got := len(perWorker[w]); got != n/2 {
			t.Errorf("G1 load-balance: worker %q completed %d jobs, want exactly %d (5+5 split); shares: %s",
				w, got, n/2, shareString(perWorker))
		}
	}
	t.Logf("G1 shares across workers: %s (accepted=%d)", shareString(perWorker), len(accepted))
}

// ---------------------------------------------------------------------------
// G2 — worker-kill requeue (the C5 path): kill one worker mid-job; every
// accepted job still completes exactly once, on a surviving peer.
// ---------------------------------------------------------------------------

func TestG2WorkerKillRequeue(t *testing.T) {
	// The work-delay knob holds each worker's Deliver in flight long enough for
	// the kill to land while a job is leased-but-unjournaled — the only state in
	// which the requeue path is exercised.
	mp := startDispatchMesh(t, threeNodeConfig(t, nil),
		[]string{dispatchpkg.WorkDelayEnv + "=" + strconv.Itoa(int(g2WorkDelay/time.Millisecond))})
	ctx := chaosCtx(t, 120*time.Second)

	// Submit a steady stream so there is always a job in flight.
	go func() {
		for i := 0; i < g2JobCount; i++ {
			if _, err := clientPublish(ctx, mp.kernelAddr(primaryNode), dispatchpkg.SubmitChannel, jobPayload(i)); err != nil {
				return // ctx ended or the mesh is gone; the assertions below report it
			}
			time.Sleep(g2SubmitPace)
		}
	}()

	// Wait until the victim worker has genuinely begun working (its done journal
	// has at least one completion, and with the work delay there is another job
	// leased in flight), then kill -9 its process. Killing by the node's unique
	// binary path targets exactly that worker.
	victim := workerNodes[0]
	survivor := workerNodes[1]
	waitForDoneAtLeast(t, ctx, mp, victim, 1)
	victimPIDs := pgrepChildren(t, mp.pluginPath(victim))
	if len(victimPIDs) != 1 {
		t.Fatalf("G2: expected exactly one live process for worker %q (path %q), found %v\n%s",
			victim, mp.pluginPath(victim), victimPIDs, mp.log())
	}
	victimPID := victimPIDs[0]
	victimDoneBeforeKill := readDoneIDs(t, ctx, mp.kernelAddr(victim))
	t.Logf("G2: killing worker %q (pid %d); it had completed %d job(s) before the kill: %v",
		victim, victimPID, len(victimDoneBeforeKill), sortedKeys(victimDoneBeforeKill))
	if err := syscall.Kill(victimPID, syscall.SIGKILL); err != nil {
		t.Fatalf("G2: kill -9 worker %q (pid %d): %v", victim, victimPID, err)
	}

	// Everything accepted must complete exactly once across the workers that are
	// still alive. Wait for accepted to settle at the full count first.
	accepted := waitForAcceptedCount(t, ctx, mp, g2JobCount)
	perWorker := waitForDoneUnion(t, ctx, mp, accepted)
	assertExactlyOnce(t, "G2", accepted, perWorker)

	// The dead worker completed nothing more after the kill: its done set is
	// frozen at the pre-kill snapshot. Every job it did NOT finish — including
	// the one it held in flight — was completed by the survivor. Prove the
	// requeue concretely: the survivor's completions include at least one job the
	// victim never finished, and the victim's post-kill set did not grow.
	victimDoneFinal := perWorker[victim]
	if len(victimDoneFinal) != len(victimDoneBeforeKill) {
		t.Errorf("G2: dead worker %q completed %d jobs after the kill but %d before — a killed worker must complete nothing more",
			victim, len(victimDoneFinal), len(victimDoneBeforeKill))
	}
	movedToSurvivor := 0
	for id := range perWorker[survivor] {
		if !victimDoneBeforeKill[id] {
			movedToSurvivor++
		}
	}
	if movedToSurvivor == 0 {
		t.Errorf("G2: no job moved to the surviving worker %q; the requeue path did not run", survivor)
	}
	t.Logf("G2: worker %q killed after %d done; survivor %q completed %d (of which %d were not the victim's); total exactly-once = %d",
		victim, len(victimDoneBeforeKill), survivor, len(perWorker[survivor]), movedToSurvivor, len(accepted))
}

// ---------------------------------------------------------------------------
// G3 — VC-12 re-run against a STATEFUL reload: reload the primary and a worker
// mid-stream; zero loss, zero duplicates, kernel PID unchanged, within budget.
// ---------------------------------------------------------------------------

func TestG3ReloadUnderLoadStateful(t *testing.T) {
	root := repoRoot(t)
	build := context.Background()

	// A byte-distinct dispatch binary for the reloads: same behaviour, a
	// different linker build-id, so the host's VERIFIED (sha256) step re-checks a
	// genuinely different file — a real reload, not a no-op restart.
	binB, shaB := buildDispatch(t, build, root, "dispatch-b", "slice-b-chaos-variant-b")
	warmExec(t, binB) // pay darwin's first-exec code-signature cost off the timed path

	mp := startDispatchMesh(t, threeNodeConfig(t, nil), nil)
	if mp.sha(primaryNode) == shaB {
		t.Fatalf("the reload target hashes the same as the running binary (%s); the reload would not be a real one", shaB)
	}
	t.Logf("running dispatch sha256=%s\nreload-target dispatch sha256=%s", mp.sha(primaryNode), shaB)

	meshPID := mp.pid()
	ctx := chaosCtx(t, 120*time.Second)

	// Two reloads fire off separate goroutines at fixed fractions of the stream,
	// so each lands mid-load no matter how fast this box publishes.
	var sent atomic.Int64
	var primaryLatency, workerLatency time.Duration
	var primaryErr, workerErr error
	childPIDsBefore := pgrepChildren(t, dispatchChildMatch)

	reloads := make(chan struct{}, 2)
	go func() {
		defer func() { reloads <- struct{}{} }()
		waitUntilSent(ctx, &sent, primaryReloadAt)
		start := time.Now()
		primaryErr = requestReload(ctx, mp.adminAddr(primaryNode), dispatchpkg.Namespace, binB, shaB)
		primaryLatency = time.Since(start)
	}()
	go func() {
		defer func() { reloads <- struct{}{} }()
		waitUntilSent(ctx, &sent, workerReloadAt)
		start := time.Now()
		workerErr = requestReload(ctx, mp.adminAddr(workerNodes[0]), dispatchpkg.Namespace, binB, shaB)
		workerLatency = time.Since(start)
	}()

	loadStart := time.Now()
	for i := 0; i < loadCount; i++ {
		if _, err := clientPublish(ctx, mp.kernelAddr(primaryNode), dispatchpkg.SubmitChannel, jobPayload(i)); err != nil {
			t.Fatalf("submit %d: %v\n%s", i, err, mp.log())
		}
		sent.Add(1)
		time.Sleep(loadPace)
	}
	loadElapsed := time.Since(loadStart)
	<-reloads
	<-reloads

	if primaryErr != nil {
		t.Fatalf("G3 primary reload failed: %v\n%s", primaryErr, mp.log())
	}
	if workerErr != nil {
		t.Fatalf("G3 worker reload failed: %v\n%s", workerErr, mp.log())
	}

	rate := float64(loadCount) / loadElapsed.Seconds()
	t.Logf("G3 load: %d jobs in %v => %.0f msg/s", loadCount, loadElapsed.Round(time.Millisecond), rate)
	t.Logf("G3 primary reload latency (mid-stream, byte-distinct binary): %v", primaryLatency)
	t.Logf("G3 worker  reload latency (mid-stream, byte-distinct binary): %v", workerLatency)

	accepted := waitForAcceptedCount(t, ctx, mp, loadCount)
	perWorker := waitForDoneUnion(t, ctx, mp, accepted)

	// (1)(2)(3) zero loss, zero duplicates, set-equality — the load-bearing VC-12
	// property, the reason the slice exists. These are hard stops.
	assertExactlyOnce(t, "G3", accepted, perWorker)
	t.Run("set-equality", func(t *testing.T) {
		if len(accepted) != loadCount {
			t.Errorf("HARD STOP — accepted holds %d jobs, want exactly %d", len(accepted), loadCount)
		}
	})

	// (4) mesh (kernel) process unchanged: the reload swapped a plugin child,
	// never restarted the daemon that hosts every kernel.
	t.Run("kernel-pid-unchanged", func(t *testing.T) {
		if mp.pid() != meshPID {
			t.Errorf("mesh process PID changed across the reloads: before=%d after=%d", meshPID, mp.pid())
		}
		if err := syscall.Kill(meshPID, 0); err != nil {
			t.Errorf("mesh process (pid %d) is not alive after the reloads: %v", meshPID, err)
		}
		childPIDsAfter := pgrepChildren(t, dispatchChildMatch)
		if sameSet(childPIDsBefore, childPIDsAfter) {
			t.Errorf("no plugin child PID changed across the reloads (%v -> %v); the reload was a no-op, not a real swap",
				childPIDsBefore, childPIDsAfter)
		}
		t.Logf("mesh process PID %d alive across the reloads; plugin children before=%v after=%v",
			meshPID, childPIDsBefore, childPIDsAfter)
	})

	// (5) load rate at or above the floor.
	t.Run("load-rate", func(t *testing.T) {
		if rate < minRate {
			t.Errorf("load rate %.0f msg/s is below the %.0f msg/s floor", rate, minRate)
		}
	})

	// (6) reload latency within budget. Separate from the zero-loss subtests so
	// the two never blur: a latency regression is loud but is not a loss failure.
	// A stateful plugin's reload also replays journals on Start, so this is a
	// stricter timing than echo's stateless reload — if it exceeds the budget the
	// finding is "stateful reload is slower than the echo ceiling", recorded as
	// such, with zero-loss still the verdict.
	t.Run("reload-latency-within-budget", func(t *testing.T) {
		if primaryLatency >= reloadBudget {
			t.Errorf("primary reload latency %v exceeds the %v ceiling (latency regression, not a zero-loss failure)", primaryLatency, reloadBudget)
		}
		if workerLatency >= reloadBudget {
			t.Errorf("worker reload latency %v exceeds the %v ceiling (latency regression, not a zero-loss failure)", workerLatency, reloadBudget)
		}
	})

	if t.Failed() {
		t.Logf("%s", mp.log())
	}
}

// ---------------------------------------------------------------------------
// G4 — four-type conformance (VC-13, full width).
// ---------------------------------------------------------------------------

// TestG4FourTypeConformance covers VC-13 at full width two ways. First, a
// dispatch-native PUBSUB-across-nodes check the test can observe: a submit
// published on a worker node's kernel fans out across the memmesh to the
// primary's subscription and lands in the primary's accepted journal — PUBSUB
// copy-to-a-remote-subscriber, proven by the artifact. POINT_TO_POINT
// exactly-one across nodes is already proven by G1/G2/G3. Second, it runs B9's
// four-type conformance cases (PUBSUB, POINT_TO_POINT, REQUEST_REPLY with the
// INTEREST_NONE fast-fail, and LOOKUP all-claimants) so the slice gate itself
// asserts the two channel types the dispatch plugin does not use. Those cases
// live in the kernel module (they need the harnesstestplugin, which
// tool-isolation forbids importing here), so the gate runs them as a
// subprocess — the same thing `make chaos` does for the kernel module, made
// part of the dispatch slice gate so one command covers the full width.
func TestG4FourTypeConformance(t *testing.T) {
	t.Run("dispatch-pubsub-cross-node", func(t *testing.T) {
		mp := startDispatchMesh(t, threeNodeConfig(t, nil), nil)
		ctx := chaosCtx(t, 60*time.Second)

		// Publish the submit on a WORKER node's kernel, not the primary's. Only the
		// primary holds an interest in dispatch.submit, so the payload must cross
		// the mesh from the worker node to the primary to be accepted at all.
		payload := jobPayload(42)
		if _, err := clientPublish(ctx, mp.kernelAddr(workerNodes[0]), dispatchpkg.SubmitChannel, payload); err != nil {
			t.Fatalf("G4: cross-node submit on %q: %v\n%s", workerNodes[0], err, mp.log())
		}
		accepted := waitForAcceptedCount(t, ctx, mp, 1)
		if len(accepted) != 1 {
			t.Fatalf("G4: primary accepted %d jobs from a cross-node submit, want exactly one: %v", len(accepted), sortedKeys(accepted))
		}
		t.Logf("G4 PUBSUB cross-node: submit published on %q reached the primary's accepted journal", workerNodes[0])
	})

	t.Run("b9-four-type-conformance", func(t *testing.T) {
		root := repoRoot(t)
		// Run B9's four-type conformance cases plus the VC-14 vocabulary probe in
		// the kernel module, with the chaos tag, as a subprocess.
		cmd := exec.CommandContext(context.Background(),
			"go", "test", "-tags", "chaos", "-count=1",
			"-run", "TestVC13|TestVC14KernelVocabularyProbe",
			"./cmd/harmonikd/...")
		cmd.Dir = filepath.Join(root, "kernel")
		out, err := cmd.CombinedOutput()
		t.Logf("B9 four-type conformance (kernel module):\n%s", bytes.TrimSpace(out))
		if err != nil {
			t.Fatalf("G4: B9 four-type conformance cases failed: %v", err)
		}
	})
}

// ===========================================================================
// Harness plumbing
// ===========================================================================

const (
	primaryNode = "primary"
	// nodeListeningMarker is logged once per node by the mesh verb AFTER the whole
	// mesh is up and every plugin has declared and subscribed, so counting it is a
	// true all-nodes-ready signal — stronger than a per-node Info poll.
	nodeListeningMarker = "node listening"
)

var workerNodes = []string{"worker-1", "worker-2"}

// meshConfig mirrors the JSON the `harmonikd mesh` verb reads. It is duplicated
// here rather than imported because tool-isolation keeps this module from
// importing the kernel command package; the shape is small and stable.
type meshConfig struct {
	Nodes []meshNodeConfig `json:"nodes"`
}

type meshNodeConfig struct {
	Name        string           `json:"name"`
	Listen      string           `json:"listen"`
	AdminListen string           `json:"admin_listen"`
	DBPath      string           `json:"db_path"`
	Plugin      meshPluginConfig `json:"plugin"`
}

type meshPluginConfig struct {
	Path   string   `json:"path"`
	SHA256 string   `json:"sha256"`
	Args   []string `json:"args"`
}

// dispatchMesh is one launched `harmonikd mesh` subprocess and the addresses and
// binary paths its nodes answer on.
type dispatchMesh struct {
	cmd         *exec.Cmd
	kernelAddrs map[string]string
	adminAddrs  map[string]string
	pluginPaths map[string]string
	shas        map[string]string

	mu  sync.Mutex
	buf bytes.Buffer
}

func (m *dispatchMesh) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buf.Write(p)
}

func (m *dispatchMesh) log() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return "--- harmonikd mesh log ---\n" + m.buf.String()
}

func (m *dispatchMesh) markerCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return strings.Count(m.buf.String(), nodeListeningMarker)
}

func (m *dispatchMesh) kernelAddr(node string) string { return m.kernelAddrs[node] }
func (m *dispatchMesh) adminAddr(node string) string  { return m.adminAddrs[node] }
func (m *dispatchMesh) pluginPath(node string) string { return m.pluginPaths[node] }
func (m *dispatchMesh) sha(node string) string        { return m.shas[node] }
func (m *dispatchMesh) pid() int                      { return m.cmd.Process.Pid }

// dispatchChildMatch is the pgrep -f pattern that resolves to every live
// dispatch plugin child of this mesh: each is launched with a --role= arg and
// the harmonikd mesh parent is not, so the substring "role=" selects the plugin
// processes and nothing else, whatever binary path a reload swapped in.
const dispatchChildMatch = "role="

// threeNodeConfig builds the N=3 mesh config (E1): one primary that declares the
// channels and two workers that compete. Each node's plugin is a DISTINCT file
// path (copies of the one built dispatch binary), so a kill -9 can target one
// worker by path without adding a launch flag the dispatch main.go does not
// parse. extraArgs, if any, are appended to every node's plugin args.
func threeNodeConfig(t *testing.T, extraArgs []string) meshConfig {
	t.Helper()
	root := repoRoot(t)
	dir := t.TempDir()
	src, _ := buildDispatch(t, context.Background(), root, "dispatch", "slice-b-chaos")

	cfg := meshConfig{}
	nodes := []struct {
		name string
		role string
	}{
		{primaryNode, "primary"},
		{workerNodes[0], "worker"},
		{workerNodes[1], "worker"},
	}
	for _, n := range nodes {
		// A per-node copy at a unique path; identical bytes => identical sha.
		dst := filepath.Join(dir, "dispatch-"+n.name)
		copyFile(t, src, dst)
		args := append([]string{"--role=" + n.role}, extraArgs...)
		cfg.Nodes = append(cfg.Nodes, meshNodeConfig{
			Name:        n.name,
			Listen:      freePort(t),
			AdminListen: freePort(t),
			DBPath:      filepath.Join(dir, n.name+".db"),
			Plugin:      meshPluginConfig{Path: dst, SHA256: sha256File(t, dst), Args: args},
		})
	}
	return cfg
}

// startDispatchMesh writes cfg to a file, launches `harmonikd mesh` as a real
// subprocess, waits until every node has logged that it is listening, and
// registers a cleanup that stops it.
func startDispatchMesh(t *testing.T, cfg meshConfig, extraEnv []string) *dispatchMesh {
	t.Helper()
	root := repoRoot(t)
	dir := t.TempDir()
	harmonikd, _ := buildBinary(t, context.Background(), root, "harmonikd",
		"github.com/gregberns/harmonik/kernel/cmd/harmonikd")

	configPath := filepath.Join(dir, "mesh.json")
	writeMeshConfig(t, configPath, cfg)

	m := &dispatchMesh{
		kernelAddrs: map[string]string{},
		adminAddrs:  map[string]string{},
		pluginPaths: map[string]string{},
		shas:        map[string]string{},
	}
	for _, n := range cfg.Nodes {
		m.kernelAddrs[n.Name] = n.Listen
		m.adminAddrs[n.Name] = n.AdminListen
		m.pluginPaths[n.Name] = n.Plugin.Path
		m.shas[n.Name] = n.Plugin.SHA256
	}

	cmd := exec.CommandContext(context.Background(), harmonikd, "mesh", "--config", configPath)
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Stdout = m
	cmd.Stderr = m
	if err := cmd.Start(); err != nil {
		t.Fatalf("start harmonikd mesh: %v", err)
	}
	m.cmd = cmd

	t.Cleanup(func() {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Logf("SIGTERM harmonikd mesh (pid %d): %v", cmd.Process.Pid, err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})

	deadline := time.Now().Add(40 * time.Second)
	for m.markerCount() < len(cfg.Nodes) {
		if time.Now().After(deadline) {
			t.Fatalf("harmonikd mesh never reported all %d nodes listening\n%s", len(cfg.Nodes), m.log())
		}
		time.Sleep(20 * time.Millisecond)
	}
	return m
}

func writeMeshConfig(t *testing.T, path string, cfg meshConfig) {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal mesh config: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write mesh config: %v", err)
	}
}

// ---- assertions ------------------------------------------------------------

// assertExactlyOnce is the hard stop: every accepted job id appears exactly once
// across the union of the workers' done journals — nothing lost, nothing
// duplicated, nothing completed that was never accepted.
func assertExactlyOnce(t *testing.T, gate string, accepted map[string]bool, perWorker map[string]map[string]bool) {
	t.Helper()

	counts := map[string]int{}
	var strays []string
	for _, done := range perWorker {
		for id := range done {
			counts[id]++
			if !accepted[id] {
				strays = append(strays, id)
			}
		}
	}
	var missing, duplicated []string
	for id := range accepted {
		switch counts[id] {
		case 1:
		case 0:
			missing = append(missing, id)
		default:
			duplicated = append(duplicated, id)
		}
	}
	sort.Strings(missing)
	sort.Strings(duplicated)
	sort.Strings(strays)

	t.Run(gate+"/no-loss", func(t *testing.T) {
		if len(missing) > 0 {
			t.Errorf("HARD STOP — %d job(s) LOST: %v", len(missing), missing)
		}
	})
	t.Run(gate+"/no-duplication", func(t *testing.T) {
		if len(duplicated) > 0 {
			t.Errorf("HARD STOP — %d job(s) DUPLICATED: %v", len(duplicated), duplicated)
		}
	})
	t.Run(gate+"/no-strays", func(t *testing.T) {
		if len(strays) > 0 {
			t.Errorf("HARD STOP — %d completion(s) for a job never accepted (corruption or a stray): %v", len(strays), strays)
		}
	})
}

// ---- polling reads ---------------------------------------------------------

// waitForAcceptedCount polls the primary's accepted journal until it holds at
// least want records, then returns the id set read on a final pass (a late
// straggler gets a moment to surface).
func waitForAcceptedCount(t *testing.T, ctx context.Context, mp *dispatchMesh, want int) map[string]bool {
	t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	for {
		ids := readJobIDs(t, ctx, mp.kernelAddr(primaryNode), dispatchpkg.AcceptedJournal)
		if len(ids) >= want {
			time.Sleep(300 * time.Millisecond)
			return readDoneIDsFromJournal(t, ctx, mp.kernelAddr(primaryNode), dispatchpkg.AcceptedJournal)
		}
		if time.Now().After(deadline) {
			t.Fatalf("accepted journal reached %d, want >= %d\n%s", len(ids), want, mp.log())
		}
		select {
		case <-ctx.Done():
			t.Fatalf("accepted journal never reached %d: %v\n%s", want, ctx.Err(), mp.log())
		case <-time.After(40 * time.Millisecond):
		}
	}
}

// waitForDoneUnion polls every worker's done journal until their union covers
// the accepted set, then (after a settle) returns each worker's done id set.
func waitForDoneUnion(t *testing.T, ctx context.Context, mp *dispatchMesh, accepted map[string]bool) map[string]map[string]bool {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		perWorker := readAllDone(t, ctx, mp)
		if covers(union(perWorker), accepted) {
			time.Sleep(400 * time.Millisecond) // let an erroneous duplicate surface
			return readAllDone(t, ctx, mp)
		}
		if time.Now().After(deadline) {
			missing := difference(accepted, union(perWorker))
			sort.Strings(missing)
			t.Fatalf("HARD STOP — done union never covered accepted; %d job(s) still missing: %v; shares: %s\n%s",
				len(missing), missing, shareString(perWorker), mp.log())
		}
		select {
		case <-ctx.Done():
			t.Fatalf("done union never covered accepted: %v\n%s", ctx.Err(), mp.log())
		case <-time.After(40 * time.Millisecond):
		}
	}
}

func readAllDone(t *testing.T, ctx context.Context, mp *dispatchMesh) map[string]map[string]bool {
	t.Helper()
	out := map[string]map[string]bool{}
	for _, w := range workerNodes {
		out[w] = readDoneIDs(t, ctx, mp.kernelAddr(w))
	}
	return out
}

// waitForDoneAtLeast blocks until the named worker's done journal holds at least
// n completions.
func waitForDoneAtLeast(t *testing.T, ctx context.Context, mp *dispatchMesh, node string, n int) {
	t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	for {
		if len(readDoneIDs(t, ctx, mp.kernelAddr(node))) >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker %q never reached %d completions\n%s", node, n, mp.log())
		}
		select {
		case <-ctx.Done():
			t.Fatalf("worker %q never reached %d completions: %v\n%s", node, n, ctx.Err(), mp.log())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// readDoneIDs reads a worker's done journal and returns its job-id set. The done
// journal records JSON-encoded jobs; the id is what anchors dedupe and the set
// checks.
func readDoneIDs(t *testing.T, ctx context.Context, addr string) map[string]bool {
	t.Helper()
	return readDoneIDsFromJournal(t, ctx, addr, dispatchpkg.DoneJournal)
}

func readDoneIDsFromJournal(t *testing.T, ctx context.Context, addr, journal string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, id := range readJobIDs(t, ctx, addr, journal) {
		out[id] = true
	}
	return out
}

// readJobIDs reads every record a journal holds and decodes each into a job id.
func readJobIDs(t *testing.T, ctx context.Context, addr, journal string) []string {
	t.Helper()
	records := readJournalRaw(t, ctx, addr, journal)
	ids := make([]string, 0, len(records))
	for _, rec := range records {
		var job dispatchpkg.Job
		if err := json.Unmarshal(rec, &job); err != nil {
			t.Fatalf("decode job from journal %q on %q: %v (record: %q)", journal, addr, err, rec)
		}
		ids = append(ids, job.ID)
	}
	return ids
}

// readJournalRaw reads every record a journal currently holds.
func readJournalRaw(t *testing.T, ctx context.Context, addr, journal string) [][]byte {
	t.Helper()
	client, closeClient := dialKernel(t, addr)
	defer closeClient()
	stream, err := client.JournalRead(ctx, &kernelv1.JournalReadRequest{Journal: journal})
	if err != nil {
		t.Fatalf("journal read %q on %q: %v", journal, addr, err)
	}
	var out [][]byte
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("journal read %q recv on %q: %v", journal, addr, err)
		}
		for _, rec := range resp.GetRecords() {
			// Copy: the returned slice is owned by the stream frame.
			b := append([]byte(nil), rec.GetRecord()...)
			out = append(out, b)
		}
	}
	return out
}

// ---- set helpers -----------------------------------------------------------

func union(perWorker map[string]map[string]bool) map[string]bool {
	u := map[string]bool{}
	for _, done := range perWorker {
		for id := range done {
			u[id] = true
		}
	}
	return u
}

func covers(have, want map[string]bool) bool {
	for id := range want {
		if !have[id] {
			return false
		}
	}
	return true
}

func difference(a, b map[string]bool) []string {
	var out []string
	for id := range a {
		if !b[id] {
			out = append(out, id)
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func shareString(perWorker map[string]map[string]bool) string {
	parts := make([]string, 0, len(perWorker))
	for _, w := range workerNodes {
		parts = append(parts, fmt.Sprintf("%s=%d", w, len(perWorker[w])))
	}
	return strings.Join(parts, " ")
}

// ---- process / build helpers ----------------------------------------------

// pgrepChildren returns the PIDs of every live process whose command line
// contains match. For a per-node binary path it resolves to exactly one worker.
func pgrepChildren(t *testing.T, match string) []int {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), "pgrep", "-f", match).CombinedOutput()
	if err != nil {
		// pgrep exits 1 when nothing matches; that is an empty set, not an error.
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return nil
		}
		t.Fatalf("pgrep -f %q: %v\n%s", match, err, out)
	}
	var pids []int
	for _, line := range strings.Fields(string(out)) {
		if n, convErr := strconv.Atoi(line); convErr == nil {
			pids = append(pids, n)
		}
	}
	sort.Ints(pids)
	return pids
}

func sameSet(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[int]bool{}
	for _, x := range a {
		seen[x] = true
	}
	for _, x := range b {
		if !seen[x] {
			return false
		}
	}
	return true
}

func waitUntilSent(ctx context.Context, sent *atomic.Int64, n int64) {
	for sent.Load() < n {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Millisecond):
		}
	}
}

// chaosCtx is a generous per-case deadline: building binaries and booting a
// three-node mesh subprocess is slower than an in-process test.
func chaosCtx(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

// requestReload drives the plugin-reload admin verb: it points the named plugin
// at a byte-distinct binary (path + sha256), the way a live deploy of a new
// plugin build does. The launch args persist across the reload, so a reloaded
// worker stays a worker and a reloaded primary stays a primary.
func requestReload(ctx context.Context, adminAddr, namespace, path, sha string) error {
	body, err := json.Marshal(map[string]string{"namespace": namespace, "path": path, "sha256": sha})
	if err != nil {
		return fmt.Errorf("encode reload request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+adminAddr+"/plugin/reload", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build reload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("reload: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("reload: status %s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	return nil
}

func dialKernel(t *testing.T, addr string) (kernelv1.KernelServiceClient, func()) {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial kernel %q: %v", addr, err)
	}
	return kernelv1.NewKernelServiceClient(conn), func() { _ = conn.Close() }
}

func clientPublish(ctx context.Context, kernelAddr, channel string, payload []byte) (string, error) {
	conn, err := grpc.NewClient(kernelAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", fmt.Errorf("dial %q: %w", kernelAddr, err)
	}
	defer func() { _ = conn.Close() }()
	resp, err := kernelv1.NewKernelServiceClient(conn).Publish(ctx, &kernelv1.PublishRequest{Channel: channel, Payload: payload})
	if err != nil {
		return "", fmt.Errorf("publish: %w", err)
	}
	return resp.GetMessageId(), nil
}

// buildDispatch builds tools/dispatch/cmd/dispatch with a distinct linker
// build-id, so two calls with different ids produce byte-distinct binaries the
// host's VERIFIED step re-checks. Returns the path and hex sha256.
func buildDispatch(t *testing.T, ctx context.Context, root, name, buildID string) (path, shaHex string) {
	t.Helper()
	return buildBinary(t, ctx, root, name,
		"github.com/gregberns/harmonik/tools/dispatch/cmd/dispatch",
		"-ldflags", "-buildid="+buildID)
}

func buildBinary(t *testing.T, ctx context.Context, root, name, pkg string, extraArgs ...string) (path, shaHex string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), name)
	args := append([]string{"build", "-o", path}, extraArgs...)
	args = append(args, pkg)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, out)
	}
	return path, sha256File(t, path)
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src) //nolint:gosec // src is a binary this test just built
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil { //nolint:gosec // an executable the test launches
		t.Fatalf("write %s: %v", dst, err)
	}
}

func warmExec(t *testing.T, path string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path)
	if err := cmd.Start(); err != nil {
		t.Fatalf("warm-exec %s: %v", path, err)
	}
	_ = cmd.Wait() // a non-zero exit here is expected (no plugin handshake env)
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("sha256 %s: %v", path, err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a free port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close port probe: %v", err)
	}
	return addr
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.work")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.work found above %s", dir)
		}
		dir = parent
	}
}

