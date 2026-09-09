package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// groupEnv mirrors the harnesstestplugin env knob that switches it from a
// PUBSUB plugin into a POINT_TO_POINT competing-consumer member. The binary is
// a separate main package, so the name is repeated here rather than imported.
const groupEnv = "HARMONIK_HARNESSTESTPLUGIN_GROUP"

// twoNodePTPHarnessConfig builds the harness plugin once and returns a 2-node
// mesh config where both nodes launch it as POINT_TO_POINT workers competing in one
// group. node-1 is listed first, so it is the kernel that declares the channel
// and holds the group queue; node-2 attaches to it across the mesh. The PTP mode
// and the group name come from the env (inherited by each child), so both nodes
// share one plugin binary.
func twoNodePTPHarnessConfig(t *testing.T) meshConfig {
	t.Helper()
	dir := t.TempDir()
	path, digest := buildHarnessPlugin(t, dir)
	return meshConfig{
		Nodes: []meshNodeConfig{
			{Name: "box-1", DBPath: filepath.Join(dir, "box-1.db"), Plugin: meshPluginConfig{Path: path, SHA256: digest}},
			{Name: "box-2", DBPath: filepath.Join(dir, "box-2.db"), Plugin: meshPluginConfig{Path: path, SHA256: digest}},
		},
	}
}

// startMeshWithLogger stands a mesh up with a caller-supplied logger so a test
// can read back the Canceled / Unavailable the dispatcher classified.
func startMeshWithLogger(t *testing.T, cfg meshConfig, logger *slog.Logger) *Mesh {
	t.Helper()
	m, err := StartMesh(withDeadline(t), cfg, logger)
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

// unionJournalCounts reads every node's records journal and returns how many
// times each hex-encoded payload appears across all of them — the "exactly once
// across the two workers" evidence.
func unionJournalCounts(t *testing.T, ctx context.Context, nodes ...*Daemon) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, d := range nodes {
		lines, err := clientJournalRead(ctx, d.KernelAddr(), harnessJournal)
		if err != nil {
			t.Fatalf("clientJournalRead on %q: %v", d.kernel.node, err)
		}
		for _, line := range lines {
			counts[line]++
		}
	}
	return counts
}

// haveAll reports whether counts holds at least one of every want key.
func haveAll(counts map[string]int, want []string) bool {
	for _, w := range want {
		if counts[w] == 0 {
			return false
		}
	}
	return true
}

// assertEachJournaledOnce waits for every want payload to reach some node's
// journal, gives a late duplicate a moment to surface, then asserts the union
// holds each payload exactly once and nothing extra — no loss, no duplication.
func assertEachJournaledOnce(t *testing.T, ctx context.Context, want []string, nodes ...*Daemon) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !haveAll(unionJournalCounts(t, ctx, nodes...), want) {
		if time.Now().After(deadline) {
			t.Fatalf("not every payload reached a journal; counts: %v", unionJournalCounts(t, ctx, nodes...))
		}
		time.Sleep(20 * time.Millisecond)
	}

	time.Sleep(300 * time.Millisecond) // let an erroneous duplicate surface
	counts := unionJournalCounts(t, ctx, nodes...)
	for _, w := range want {
		switch counts[w] {
		case 1:
		case 0:
			t.Errorf("payload %q was lost", w)
		default:
			t.Errorf("payload %q landed %d times across the workers (duplicate)", w, counts[w])
		}
	}
	if len(counts) != len(want) {
		t.Fatalf("union journal holds %d distinct payloads, want exactly %d", len(counts), len(want))
	}
}

// TestPTPKillMidDeliverRequeuesToSurvivor is B8's correctness crux and the C5
// minimum: two workers compete in one group, one is kill -9'd mid-Deliver (the
// slow-handler knob holds it there), and every job must still land on a journal
// exactly once — the dead worker's in-flight and still-queued jobs nacked across
// to the survivor, none lost, none duplicated.
func TestPTPKillMidDeliverRequeuesToSurvivor(t *testing.T) {
	t.Setenv(groupEnv, "workers")
	t.Setenv(deliverDelayEnv, "300") // slow enough that a kill lands inside a Deliver

	logs := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	m := startMeshWithLogger(t, twoNodePTPHarnessConfig(t), logger)
	ctx := withDeadline(t)

	primary := m.Node("box-1")
	victim := m.Node("box-2")
	channel := primary.PluginManifest().GetChannels()[0].GetName()

	const n = 10
	want := make([]string, n)
	for i := range want {
		payload := []byte(fmt.Sprintf("job-%04d", i))
		want[i] = hex.EncodeToString(payload)
		if _, err := clientPublish(ctx, primary.KernelAddr(), channel, payload); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	// Wait until the victim actually has a delivery in flight, then kill its
	// plugin process outright while it is still inside the slow handler — before
	// that job was ever journaled, so the survivor owns it.
	waitForInFlight(t, ctx, victim)
	pid := pidOf(t, victim)
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill victim plugin pid %d: %v", pid, err)
	}

	// Every payload must reach a journal exactly once, summed across both nodes:
	// the dead worker's in-flight and still-queued jobs nacked across to the
	// survivor, none lost, none duplicated.
	assertEachJournaledOnce(t, ctx, want, primary, victim)

	// The dead child surfaced as Unavailable, not a deliberate Canceled.
	waitForLog(t, logs, "reason=unavailable")
}

// TestPTPReloadMidStreamLosesNothing is the reload half of B8 at unit scale: a
// PTP-subscribed worker is reloaded while a burst of distinguishable jobs flows,
// and the stream must come through with no loss and no duplicate — the detach
// nacks the reloading worker's queued and leased work to the survivor, and the
// re-attach brings it back to competing.
func TestPTPReloadMidStreamLosesNothing(t *testing.T) {
	t.Setenv(groupEnv, "workers")

	m := startMesh(t, twoNodePTPHarnessConfig(t))
	ctx := withDeadline(t)

	primary := m.Node("box-1")
	worker := m.Node("box-2")
	channel := primary.PluginManifest().GetChannels()[0].GetName()

	const n = 40
	want := make([]string, n)
	for i := range want {
		want[i] = hex.EncodeToString([]byte(fmt.Sprintf("stream-%04d", i)))
	}

	var pubErr error
	published := make(chan struct{})
	go func() {
		defer close(published)
		for i := 0; i < n; i++ {
			if _, err := clientPublish(ctx, primary.KernelAddr(), channel, []byte(fmt.Sprintf("stream-%04d", i))); err != nil {
				pubErr = fmt.Errorf("publish %d: %w", i, err)
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// Reload the worker mid-stream, while publishes are still arriving.
	time.Sleep(10 * time.Millisecond)
	if err := requestReload(ctx, worker.AdminAddr(), reloadRequest{Namespace: "harnesstestplugin"}); err != nil {
		t.Fatalf("reload worker: %v", err)
	}

	<-published
	if pubErr != nil {
		t.Fatalf("%v", pubErr)
	}

	assertEachJournaledOnce(t, ctx, want, primary, worker)
}

// TestPTPReloadCancelsHungDeliverDistinctly keeps the K8 Canceled/Unavailable
// split on the PTP path: a reload whose in-flight Deliver is hung is cancelled
// at the drain deadline and logged as a deliberate Canceled — never confused
// with the Unavailable a crashed worker surfaces.
func TestPTPReloadCancelsHungDeliverDistinctly(t *testing.T) {
	t.Setenv(groupEnv, "workers")
	t.Setenv(deliverHangEnv, "1")

	logs := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	m := startMeshWithLogger(t, twoNodePTPHarnessConfig(t), logger)
	ctx := withDeadline(t)

	primary := m.Node("box-1")
	worker := m.Node("box-2")
	channel := primary.PluginManifest().GetChannels()[0].GetName()

	// Publish enough jobs that the worker is handed one and hangs inside its
	// Deliver; round-robin puts every other job on the worker.
	for i := 0; i < 6; i++ {
		if _, err := clientPublish(ctx, primary.KernelAddr(), channel, []byte(fmt.Sprintf("hang-%d", i))); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	waitForInFlight(t, ctx, worker)
	worker.plugin.drainDeadline = 200 * time.Millisecond

	if err := requestReload(ctx, worker.AdminAddr(), reloadRequest{Namespace: "harnesstestplugin"}); err != nil {
		t.Fatalf("reload worker: %v", err)
	}

	// The hung delivery was cancelled deliberately by the drain gate.
	waitForLog(t, logs, "reason=canceled")
}
