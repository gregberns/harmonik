//go:build chaos

// This file is the B9 four-type conformance harness (VC-13, full width). It is
// behind //go:build chaos so it never runs in the normal build or in `make
// segments`/`make fast`/`make full`; `make chaos` is the one command that runs
// it, alongside the companion slice gate in tools/.
//
// What it proves, against a real `harmonikd mesh` process built from the slice
// tip and driven over gRPC only:
//
//   - PUBSUB: a payload published on one node reaches a subscriber on every
//     node — copy-to-all across nodes.
//   - POINT_TO_POINT: a burst published on the declaring node is split across
//     competing members on two nodes, each payload delivered to exactly one —
//     exactly-one across nodes.
//   - REQUEST_REPLY: a question reaches a server on another node and comes back
//     with a correlated answer; and a question on a declared-but-unserved
//     channel fast-fails with INTEREST_NONE, never a silent timeout.
//   - LOOKUP: two nodes claiming one key both come back from a single get — the
//     all-claimants merge, with the kernel picking neither.
//
// Each case drives the harnesstestplugin under its neutral harnesstestplugin.*
// names (no domain noun, so the kernel-vocabulary gate stays green), selects the
// channel type with the plugin's MODE / GROUP env knobs, and asserts the
// ARTIFACT — journal rows, the correlated answer payload, the returned lookup
// entries — never a bare process exit code. VC-14 (the kernel vocabulary probe)
// runs alongside as its own case.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

const (
	// nodeListeningMarker is logged once per node by the mesh verb AFTER the
	// whole mesh is up and every plugin has declared and subscribed, so counting
	// it is a true all-nodes-ready signal — stronger than a per-node Info poll,
	// which answers before a plugin's subscription exists.
	nodeListeningMarker = "node listening"
	// conformanceChannel is the one channel every mode declares. It carries no
	// domain noun; the plugin's namespace owns it.
	conformanceChannel = "harnesstestplugin.in"
	unservedChannel    = "harnesstestplugin.unserved"
	conformanceJournal = "records"
)

// TestVC13PubsubCopyToAllAcrossNodes drives the PUBSUB conformance case: one
// payload published on node-a is journaled by a subscriber on BOTH nodes. The
// artifact is the per-node journal: exactly one matching record on each.
func TestVC13PubsubCopyToAllAcrossNodes(t *testing.T) {
	mp := startMeshProcess(t, twoNodeConfig(t), nil)
	ctx := chaosCtx(t)

	payload := []byte{0xca, 0xfe, 0x00, 0x42, 'p', 'u', 'b'}
	want := hex.EncodeToString(payload)
	if _, err := clientPublish(ctx, mp.kernelAddr("node-a"), conformanceChannel, payload); err != nil {
		t.Fatalf("publish on node-a: %v\n%s", err, mp.log())
	}

	for _, node := range []string{"node-a", "node-b"} {
		got := pollJournalHas(t, ctx, mp.kernelAddr(node), conformanceJournal, want, mp)
		count := 0
		for _, line := range got {
			if line == want {
				count++
			}
		}
		if count != 1 {
			t.Errorf("node %q journaled the payload %d times, want exactly one copy (copy-to-all): %v", node, count, got)
		}
	}
}

// TestVC13PointToPointExactlyOneAcrossNodes drives the POINT_TO_POINT case: ten
// distinct payloads published on the declaring node are split across two
// competing members on two nodes. The artifact is the UNION of both journals:
// every payload present exactly once (no loss, no duplication), and both nodes
// holding a share (the stream really crossed nodes).
func TestVC13PointToPointExactlyOneAcrossNodes(t *testing.T) {
	mp := startMeshProcess(t, twoNodeConfig(t), []string{"HARMONIK_HARNESSTESTPLUGIN_GROUP=workers"})
	ctx := chaosCtx(t)

	const n = 10
	want := make([]string, n)
	for i := range want {
		payload := []byte(fmt.Sprintf("ptp-%04d", i))
		want[i] = hex.EncodeToString(payload)
		if _, err := clientPublish(ctx, mp.kernelAddr("node-a"), conformanceChannel, payload); err != nil {
			t.Fatalf("publish %d on node-a: %v\n%s", i, err, mp.log())
		}
	}

	perNode := waitForUnion(t, ctx, mp, want)

	// Exactly-one: the union holds each payload once and nothing else.
	union := map[string]int{}
	for _, lines := range perNode {
		for _, line := range lines {
			union[line]++
		}
	}
	for _, w := range want {
		switch union[w] {
		case 1:
		case 0:
			t.Errorf("payload %q was lost", w)
		default:
			t.Errorf("payload %q was delivered %d times (duplicate) — POINT_TO_POINT must be exactly-one", w, union[w])
		}
	}
	if len(union) != n {
		t.Errorf("union journal holds %d distinct payloads, want exactly %d", len(union), n)
	}

	// Across nodes: both members took a share of the one stream.
	for node, lines := range perNode {
		if len(lines) == 0 {
			t.Errorf("node %q took no payloads; the stream did not compete across both nodes (shares: %s)", node, shares(perNode))
		}
	}
	t.Logf("POINT_TO_POINT shares across nodes: %s", shares(perNode))
}

// TestVC13RequestReplyCorrelatedAndInterestNone drives the REQUEST_REPLY case
// in two parts, and the first genuinely exercises the CROSS-NODE leg. Only
// node-a serves the channel (the per-node --rr-role arg: node-a=server,
// node-b=client), so a question issued on node-b has NO local answerer and must
// route across the mesh to node-a. The server writes its own node name into the
// answer, so the artifact — the correlated answer payload — proves BOTH that the
// answer carries this question's bytes back AND that node-a is the node that
// served it. Second part: a question on a declared-but-never-served channel
// returns INTEREST_NONE at once — the fast-fail, asserted to both carry
// INTEREST_NONE and return well inside the request timeout.
func TestVC13RequestReplyCorrelatedAndInterestNone(t *testing.T) {
	cfg := twoNodeConfig(t)
	setPluginArgs(cfg, "node-a", "--rr-role=server")
	setPluginArgs(cfg, "node-b", "--rr-role=client")
	mp := startMeshProcess(t, cfg, []string{"HARMONIK_HARNESSTESTPLUGIN_MODE=request_reply"})
	ctx := chaosCtx(t)

	// Correlated, cross-node answer. node-b holds no server, so this question
	// routes to node-a. node-a's Serve stream attaches just after boot, so an
	// early question can see INTEREST_NONE for a moment; retry until a server is
	// present, then assert the answer is node-a's correlated reply to THIS
	// question. The "node-a:" in the expected answer is what makes this a
	// cross-node assertion: had node-b answered locally, it would read "node-b:".
	question := []byte("question-7f3a")
	wantAnswer := append([]byte("reply:node-a:"), question...)
	var answer []byte
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := clientRequest(ctx, mp.kernelAddr("node-b"), conformanceChannel, question, 2000)
		if err != nil {
			t.Fatalf("request on node-b: %v\n%s", err, mp.log())
		}
		if resp.GetInterest() == kernelv1.Interest_INTEREST_PRESENT {
			answer = resp.GetEnvelope().GetPayload()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no REQUEST_REPLY server ever attached; last interest = %v\n%s", resp.GetInterest(), mp.log())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !bytes.Equal(answer, wantAnswer) {
		t.Errorf("answer = %q, want node-a's correlated cross-node reply %q", answer, wantAnswer)
	}
	t.Logf("REQUEST_REPLY cross-node: question issued on node-b, answered %q (served by node-a)", answer)

	// INTEREST_NONE fast-fail: nobody serves the unserved channel, so the kernel
	// answers at once with INTEREST_NONE rather than waiting out the timeout.
	start := time.Now()
	resp, err := clientRequest(ctx, mp.kernelAddr("node-a"), unservedChannel, []byte("anyone?"), 5000)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("request on the unserved channel: %v\n%s", err, mp.log())
	}
	if resp.GetInterest() != kernelv1.Interest_INTEREST_NONE {
		t.Errorf("unserved-channel interest = %v, want INTEREST_NONE", resp.GetInterest())
	}
	if resp.GetEnvelope().GetPayload() != nil {
		t.Errorf("unserved-channel request carried a payload %q, want none", resp.GetEnvelope().GetPayload())
	}
	if elapsed > time.Second {
		t.Errorf("INTEREST_NONE took %v, want a fast-fail well inside the 5s timeout (not a silent wait)", elapsed)
	}
	t.Logf("INTEREST_NONE returned in %v (timeout was 5s)", elapsed)
}

// TestVC13LookupAllClaimantsOnClash drives the LOOKUP case: both nodes claim one
// key, each with its own node name as the value, and a single get returns BOTH
// claimants. The artifact is the returned entry set — two entries, one per
// writer node, the kernel picking neither.
func TestVC13LookupAllClaimantsOnClash(t *testing.T) {
	mp := startMeshProcess(t, twoNodeConfig(t), []string{"HARMONIK_HARNESSTESTPLUGIN_MODE=lookup"})
	ctx := chaosCtx(t)

	// Each node's plugin claims the key asynchronously just after boot; wait for
	// both claims to land, read through one node (the merge reads every node's
	// map), and assert both claimants are returned.
	var entries []*kernelv1.LookupEntry
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := clientLookupGet(ctx, mp.kernelAddr("node-a"), conformanceChannel, "claimed-key")
		if err != nil {
			t.Fatalf("lookup get on node-a: %v\n%s", err, mp.log())
		}
		if len(got) >= 2 {
			entries = got
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the key never had two claimants; got %d\n%s", len(got), mp.log())
		}
		time.Sleep(20 * time.Millisecond)
	}

	if len(entries) != 2 {
		t.Fatalf("lookup returned %d entries, want exactly two claimants: %v", len(entries), entries)
	}
	byWriter := map[string]string{}
	for _, e := range entries {
		byWriter[e.GetWriterNode()] = string(e.GetValue())
	}
	for _, node := range []string{"node-a", "node-b"} {
		v, ok := byWriter[node]
		if !ok {
			t.Errorf("no claim from writer %q; the clash did not surface both nodes: %v", node, byWriter)
			continue
		}
		if v != node {
			t.Errorf("claim from %q carried value %q, want the node's own name", node, v)
		}
	}
	t.Logf("LOOKUP clash returned both claimants: %v", byWriter)
}

// TestVC14KernelVocabularyProbe is VC-14 as a probe driven alongside the four
// conformance cases: it drives the same grep the segment gate drives and
// asserts a clean exit, so a chaos pass also catches a domain word that leaked
// into kernel/ (this test file and the harnesstestplugin extension included).
func TestVC14KernelVocabularyProbe(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "segments", "kernel-vocabulary.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("kernel-vocabulary.sh not found at %s: %v", script, err)
	}
	cmd := exec.CommandContext(context.Background(), script)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("VC-14 kernel vocabulary check failed:\n%s", out)
	}
	t.Logf("VC-14: %s", bytes.TrimSpace(out))
}

// --- harness plumbing -------------------------------------------------------

// meshProcess is one launched `harmonikd mesh` subprocess and the addresses its
// nodes answer on. The ports are pinned in the config this harness writes, so
// the caller reaches each node directly rather than parsing them back out.
type meshProcess struct {
	cmd   *exec.Cmd
	addrs map[string]string // node name -> kernel gRPC address

	mu  sync.Mutex
	buf bytes.Buffer
}

func (mp *meshProcess) Write(p []byte) (int, error) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return mp.buf.Write(p)
}

func (mp *meshProcess) log() string {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return "--- harmonikd mesh log ---\n" + mp.buf.String()
}

func (mp *meshProcess) markerCount() int {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return strings.Count(mp.buf.String(), nodeListeningMarker)
}

func (mp *meshProcess) kernelAddr(node string) string { return mp.addrs[node] }

// twoNodeConfig builds the harnesstestplugin once and returns a two-node mesh
// config with pinned, free loopback ports. node-a is listed first, so it is the
// kernel that declares the channels and holds any group queue; node-b attaches
// across the mesh. Both nodes launch the one plugin binary; the env the caller
// passes to startMeshProcess picks the channel type each serves.
func twoNodeConfig(t *testing.T) meshConfig {
	t.Helper()
	dir := t.TempDir()
	path, digest := buildBinary(t, dir, "harnesstestplugin",
		"github.com/gregberns/harmonik/kernel/cmd/harmonikd/harnesstestplugin")
	cfg := meshConfig{}
	for _, name := range []string{"node-a", "node-b"} {
		cfg.Nodes = append(cfg.Nodes, meshNodeConfig{
			Name:        name,
			Listen:      freePort(t),
			AdminListen: freePort(t),
			DBPath:      filepath.Join(dir, name+".db"),
			Plugin:      meshPluginConfig{Path: path, SHA256: digest},
		})
	}
	return cfg
}

// setPluginArgs sets the launch args for the named node's plugin — the per-node
// differentiator that env cannot provide, since env is inherited process-wide.
// The args travel on the mesh config's plugin.args, the same path the dispatch
// plugin's --role takes.
func setPluginArgs(cfg meshConfig, node string, args ...string) {
	for i := range cfg.Nodes {
		if cfg.Nodes[i].Name == node {
			cfg.Nodes[i].Plugin.Args = args
			return
		}
	}
	panic("setPluginArgs: no node named " + node)
}

// startMeshProcess writes cfg to a file, launches `harmonikd mesh` as a real
// subprocess with extraEnv appended to this process's environment (the plugin
// children inherit it, so it is how each case picks its channel type), waits
// until every node has logged that it is listening, and registers a cleanup that
// stops it. It returns a handle whose addrs map is the pinned config ports.
func startMeshProcess(t *testing.T, cfg meshConfig, extraEnv []string) *meshProcess {
	t.Helper()

	dir := t.TempDir()
	harmonikd, _ := buildBinary(t, dir, "harmonikd", "github.com/gregberns/harmonik/kernel/cmd/harmonikd")

	configPath := filepath.Join(dir, "mesh.json")
	writeMeshConfig(t, configPath, cfg)

	mp := &meshProcess{addrs: map[string]string{}}
	for _, n := range cfg.Nodes {
		mp.addrs[n.Name] = n.Listen
	}

	cmd := exec.CommandContext(context.Background(), harmonikd, "mesh", "--config", configPath)
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Stdout = mp
	cmd.Stderr = mp
	if err := cmd.Start(); err != nil {
		t.Fatalf("start harmonikd mesh: %v", err)
	}
	mp.cmd = cmd

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

	// Ready when every node has logged that it is listening — the mesh verb logs
	// that once per node only after the whole mesh is up and subscribed.
	deadline := time.Now().Add(30 * time.Second)
	for mp.markerCount() < len(cfg.Nodes) {
		if time.Now().After(deadline) {
			t.Fatalf("harmonikd mesh never reported all %d nodes listening\n%s", len(cfg.Nodes), mp.log())
		}
		time.Sleep(20 * time.Millisecond)
	}
	return mp
}

// writeMeshConfig serialises cfg to path as the JSON the mesh verb reads. It
// round-trips through loadMeshConfig to fail loudly here if the harness ever
// writes a config the real loader would reject.
func writeMeshConfig(t *testing.T, path string, cfg meshConfig) {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal mesh config: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write mesh config: %v", err)
	}
	if _, err := loadMeshConfig(path); err != nil {
		t.Fatalf("harness wrote a mesh config the loader rejects: %v", err)
	}
}

// chaosCtx is a generous per-case deadline: building two binaries and booting a
// two-node mesh subprocess is slower than an in-process test, so this is wider
// than the in-process withDeadline.
func chaosCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// pollJournalHas retries a journal read until want appears or ctx ends, then
// returns every record the final read saw (so a duplicate is visible too).
func pollJournalHas(t *testing.T, ctx context.Context, addr, journal, want string, mp *meshProcess) []string {
	t.Helper()
	for {
		lines, err := clientJournalRead(ctx, addr, journal)
		if err != nil {
			t.Fatalf("journal read on %q: %v\n%s", addr, err, mp.log())
		}
		for _, line := range lines {
			if line == want {
				return lines
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("journal %q on %q never held %q; last read: %v\n%s", journal, addr, want, lines, mp.log())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// waitForUnion waits until every want payload has reached SOME node's journal,
// gives a late duplicate a moment to surface, then returns each node's records.
func waitForUnion(t *testing.T, ctx context.Context, mp *meshProcess, want []string) map[string][]string {
	t.Helper()
	nodes := make([]string, 0, len(mp.addrs))
	for name := range mp.addrs {
		nodes = append(nodes, name)
	}

	deadline := time.Now().Add(20 * time.Second)
	for {
		perNode := readAll(t, ctx, mp, nodes)
		if haveEvery(perNode, want) {
			time.Sleep(300 * time.Millisecond) // let an erroneous duplicate surface
			return readAll(t, ctx, mp, nodes)
		}
		if time.Now().After(deadline) {
			t.Fatalf("not every payload reached a journal; shares: %s\n%s", shares(perNode), mp.log())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func readAll(t *testing.T, ctx context.Context, mp *meshProcess, nodes []string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, name := range nodes {
		lines, err := clientJournalRead(ctx, mp.kernelAddr(name), conformanceJournal)
		if err != nil {
			t.Fatalf("journal read on %q: %v\n%s", name, err, mp.log())
		}
		out[name] = lines
	}
	return out
}

func haveEvery(perNode map[string][]string, want []string) bool {
	seen := map[string]bool{}
	for _, lines := range perNode {
		for _, line := range lines {
			seen[line] = true
		}
	}
	for _, w := range want {
		if !seen[w] {
			return false
		}
	}
	return true
}

func shares(perNode map[string][]string) string {
	parts := make([]string, 0, len(perNode))
	for name, lines := range perNode {
		parts = append(parts, fmt.Sprintf("%s=%d", name, len(lines)))
	}
	return strings.Join(parts, " ")
}

// clientRequest issues one REQUEST_REPLY question and returns the kernel's
// response, so a case can read both the correlated answer and the interest.
func clientRequest(ctx context.Context, kernelAddr, channel string, payload []byte, timeoutMs uint32) (*kernelv1.RequestResponse, error) {
	client, closeFn, err := dialKernel(kernelAddr)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			slog.ErrorContext(ctx, "harmonikd: close kernel connection", "error", closeErr)
		}
	}()
	resp, err := client.Request(ctx, &kernelv1.RequestRequest{
		Channel:   channel,
		Payload:   payload,
		TimeoutMs: timeoutMs,
	})
	if err != nil {
		// A deadline is the only "no answer" the harness expects to see as an
		// error; an INTEREST_NONE is a normal response, not an error.
		if status.Code(err) == codes.DeadlineExceeded {
			return nil, fmt.Errorf("request timed out with no response: %w", err)
		}
		return nil, fmt.Errorf("request: %w", err)
	}
	return resp, nil
}

// clientLookupGet returns every claimant of a key on a LOOKUP channel — the
// mesh merge, read through one node, is what surfaces a cross-node clash.
func clientLookupGet(ctx context.Context, kernelAddr, channel, key string) ([]*kernelv1.LookupEntry, error) {
	client, closeFn, err := dialKernel(kernelAddr)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			slog.ErrorContext(ctx, "harmonikd: close kernel connection", "error", closeErr)
		}
	}()
	resp, err := client.LookupGet(ctx, &kernelv1.LookupGetRequest{Channel: channel, Key: key})
	if err != nil {
		return nil, fmt.Errorf("lookup get: %w", err)
	}
	return resp.GetEntries(), nil
}

// freePort asks the OS for an unused loopback port and returns it as a
// host:port string. There is a small window between closing the probe listener
// and harmonikd binding it, acceptable for a gate that starts a handful of
// processes.
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

// repoRoot walks up from the test's working directory to the workspace go.work,
// so the VC-14 probe can find scripts/segments from any module.
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
