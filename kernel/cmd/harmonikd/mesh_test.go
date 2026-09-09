package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// buildBinary compiles a Go main package into dir/name and returns the path and
// its sha256 — the same shape any plugin's mesh config carries.
func buildBinary(t *testing.T, dir, name, pkg string) (path, sha256Hex string) {
	t.Helper()

	path = filepath.Join(dir, name)
	buildCmd := exec.CommandContext(context.Background(), "go", "build", "-o", path, pkg)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, out)
	}

	f, err := os.Open(path) //nolint:gosec // path is this test's own t.TempDir() build output
	if err != nil {
		t.Fatalf("open built binary: %v", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Fatalf("close built binary: %v", closeErr)
		}
	}()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("sha256 built binary: %v", err)
	}
	return path, hex.EncodeToString(h.Sum(nil))
}

// twoNodeDispatchConfig builds the dispatch binary and returns a 2-node mesh
// config: box-1 runs the primary (it declares the channels), box-2 runs a
// worker. The primary is listed first, as StartMesh requires of a declarer.
func twoNodeDispatchConfig(t *testing.T) meshConfig {
	t.Helper()
	dir := t.TempDir()
	path, digest := buildBinary(t, dir, "dispatch", "github.com/gregberns/harmonik/tools/dispatch/cmd/dispatch")
	return meshConfig{
		Nodes: []meshNodeConfig{
			{
				Name:   "box-1",
				DBPath: filepath.Join(dir, "box-1.db"),
				Plugin: meshPluginConfig{Path: path, SHA256: digest, Args: []string{"--role", "primary"}},
			},
			{
				Name:   "box-2",
				DBPath: filepath.Join(dir, "box-2.db"),
				Plugin: meshPluginConfig{Path: path, SHA256: digest, Args: []string{"--role", "worker"}},
			},
		},
	}
}

func startMesh(t *testing.T, cfg meshConfig) *Mesh {
	t.Helper()
	m, err := StartMesh(withDeadline(t), cfg, nil)
	if err != nil {
		t.Fatalf("StartMesh: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := m.Close(context.Background()); closeErr != nil {
			t.Errorf("mesh Close: %v", closeErr)
		}
	})
	return m
}

// pidOf returns the live plugin process id for a node, failing if no process is
// RUNNING.
func pidOf(t *testing.T, d *Daemon) int {
	t.Helper()
	h := d.plugin.liveHost()
	if h == nil {
		t.Fatalf("node %q has no RUNNING plugin", d.kernel.node)
	}
	return h.PID()
}

// TestMeshTwoNodeBootsDistinct is B7's first acceptance case: a 2-node mesh
// boots both plugins to RUNNING with distinct PIDs and distinct kernel/admin
// addresses.
func TestMeshTwoNodeBootsDistinct(t *testing.T) {
	m := startMesh(t, twoNodeDispatchConfig(t))

	a := m.Node("box-1")
	b := m.Node("box-2")
	if a == nil || b == nil {
		t.Fatalf("mesh missing a node: box-1=%v box-2=%v", a, b)
	}

	if ha, hb := a.plugin.liveHost(), b.plugin.liveHost(); ha == nil || hb == nil {
		t.Fatalf("both plugins must be RUNNING: box-1=%v box-2=%v", ha, hb)
	}
	if pidA, pidB := pidOf(t, a), pidOf(t, b); pidA == pidB {
		t.Errorf("plugin PIDs must differ: both %d", pidA)
	}
	if a.KernelAddr() == b.KernelAddr() {
		t.Errorf("kernel addresses must differ: both %q", a.KernelAddr())
	}
	if a.AdminAddr() == b.AdminAddr() {
		t.Errorf("admin addresses must differ: both %q", a.AdminAddr())
	}
}

// TestMeshRosterShowsBothNodes is B7's second acceptance case: RosterList on
// either node shows both boxes and self.
func TestMeshRosterShowsBothNodes(t *testing.T) {
	m := startMesh(t, twoNodeDispatchConfig(t))
	ctx := withDeadline(t)

	for _, name := range []string{"box-1", "box-2"} {
		d := m.Node(name)
		resp, err := d.kernel.RosterList(ctx, &kernelv1.RosterListRequest{})
		if err != nil {
			t.Fatalf("RosterList on %q: %v", name, err)
		}
		if resp.GetSelf() != name {
			t.Errorf("RosterList on %q: Self = %q, want %q", name, resp.GetSelf(), name)
		}
		seen := map[string]kernelv1.Liveness_State{}
		for _, ns := range resp.GetNodes() {
			seen[ns.GetNode().GetName()] = ns.GetLiveness().GetState()
		}
		if len(seen) != 2 {
			t.Fatalf("RosterList on %q: %d nodes, want 2 (self + peer): %v", name, len(seen), seen)
		}
		for _, want := range []string{"box-1", "box-2"} {
			if seen[want] != kernelv1.Liveness_STATE_ALIVE {
				t.Errorf("RosterList on %q: node %q state = %v, want ALIVE", name, want, seen[want])
			}
		}
	}
}

// TestMeshCrossNodeSubmitReachesPrimaryJournal is B7's third acceptance case: a
// publish to dispatch.submit on the WORKER node's kernel (box-2) is carried
// across the mesh to the primary (box-1) and lands in the primary's accepted
// journal. This exercises cross-node PUBSUB fan-out end to end: the worker node
// never holds the submit interest, so the job reaches the primary only through
// the memmesh link.
func TestMeshCrossNodeSubmitReachesPrimaryJournal(t *testing.T) {
	m := startMesh(t, twoNodeDispatchConfig(t))
	ctx := withDeadline(t)

	primary := m.Node("box-1")
	worker := m.Node("box-2")

	payload := []byte("job-one")
	if _, err := clientPublish(ctx, worker.KernelAddr(), "dispatch.submit", payload); err != nil {
		t.Fatalf("clientPublish dispatch.submit on worker node: %v", err)
	}

	// Poll the primary's accepted journal: the primary stamps a job id and
	// appends the accepted record asynchronously to the publish returning.
	for {
		lines, err := clientJournalRead(ctx, primary.KernelAddr(), "accepted")
		if err != nil {
			t.Fatalf("read accepted journal: %v", err)
		}
		if body, ok := acceptedBody(t, lines); ok {
			if !bytes.Equal(body, payload) {
				t.Fatalf("accepted job body = %q, want %q", body, payload)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("primary's accepted journal never recorded the submitted job; read: %v", lines)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// acceptedBody decodes the first accepted-journal record (hex-encoded on the
// wire, JSON {id, body} underneath) and returns its body, or ok=false when the
// journal is still empty.
func acceptedBody(t *testing.T, hexLines []string) (body []byte, ok bool) {
	t.Helper()
	if len(hexLines) == 0 {
		return nil, false
	}
	raw, err := hex.DecodeString(hexLines[0])
	if err != nil {
		t.Fatalf("decode accepted record hex: %v", err)
	}
	var job struct {
		ID   string `json:"id"`
		Body []byte `json:"body"`
	}
	if err := json.Unmarshal(raw, &job); err != nil {
		t.Fatalf("decode accepted record json: %v", err)
	}
	return job.Body, true
}

// TestMeshReloadIsNodeLocal is B7's fourth acceptance case: a reload against
// node B's admin restarts only node B's plugin; node A's process is untouched.
func TestMeshReloadIsNodeLocal(t *testing.T) {
	m := startMesh(t, twoNodeDispatchConfig(t))
	ctx := withDeadline(t)

	a := m.Node("box-1")
	b := m.Node("box-2")

	pidABefore := pidOf(t, a)
	pidBBefore := pidOf(t, b)

	if err := requestReload(ctx, b.AdminAddr(), reloadRequest{Namespace: "dispatch"}); err != nil {
		t.Fatalf("reload node B: %v", err)
	}

	pidBAfter := pidOf(t, b)
	if pidBAfter == pidBBefore {
		t.Errorf("node B plugin PID unchanged after reload: %d", pidBAfter)
	}
	if pidAAfter := pidOf(t, a); pidAAfter != pidABefore {
		t.Errorf("node A plugin PID changed by node B's reload: before %d, after %d", pidABefore, pidAAfter)
	}
}

// TestMeshReloadKeepsRole proves the launch args persist across a reload: the
// reloaded node B process is still a worker, so its manifest still holds only
// the work-channel interest and declares no channels (the primary's shape would
// declare three).
func TestMeshReloadKeepsRole(t *testing.T) {
	m := startMesh(t, twoNodeDispatchConfig(t))
	ctx := withDeadline(t)

	b := m.Node("box-2")
	if got := len(b.PluginManifest().GetChannels()); got != 0 {
		t.Fatalf("worker declares %d channels before reload, want 0", got)
	}

	if err := requestReload(ctx, b.AdminAddr(), reloadRequest{Namespace: "dispatch"}); err != nil {
		t.Fatalf("reload node B: %v", err)
	}

	man := b.PluginManifest()
	if got := len(man.GetChannels()); got != 0 {
		t.Errorf("reloaded worker declares %d channels, want 0 — the --role arg did not persist", got)
	}
	if got := len(man.GetInterests()); got != 1 {
		t.Errorf("reloaded worker holds %d interests, want 1 (dispatch.work)", got)
	}
}

// TestLoadMeshConfig covers the file path: a JSON config round-trips through
// loadMeshConfig, and the validation refusals fire.
func TestLoadMeshConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := meshConfig{Nodes: []meshNodeConfig{
		{Name: "box-1", DBPath: "a.db", Plugin: meshPluginConfig{Path: "/bin/x", SHA256: "abc", Args: []string{"--role", "primary"}}},
		{Name: "box-2", DBPath: "b.db", Plugin: meshPluginConfig{Path: "/bin/x", SHA256: "abc", Args: []string{"--role", "worker"}}},
	}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	path := filepath.Join(dir, "mesh.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got, err := loadMeshConfig(path)
	if err != nil {
		t.Fatalf("loadMeshConfig: %v", err)
	}
	if len(got.Nodes) != 2 || got.Nodes[1].Plugin.Args[1] != "worker" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	// A config with two nodes sharing one state file is refused.
	bad := meshConfig{Nodes: []meshNodeConfig{
		{Name: "box-1", DBPath: "same.db", Plugin: meshPluginConfig{Path: "/bin/x", SHA256: "abc"}},
		{Name: "box-2", DBPath: "same.db", Plugin: meshPluginConfig{Path: "/bin/x", SHA256: "abc"}},
	}}
	if err := bad.validate(); err == nil {
		t.Error("shared db_path must be refused")
	}
}
