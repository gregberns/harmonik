package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The harness plugin's delivery-timing knobs. These mirror the env var names
// harnesstestplugin reads; that binary is a separate main package, so the
// names are repeated here rather than imported.
const (
	deliverDelayEnv = "HARMONIK_HARNESSTESTPLUGIN_DELIVER_DELAY_MS"
	deliverHangEnv  = "HARMONIK_HARNESSTESTPLUGIN_DELIVER_HANG"
)

// lockedBuffer is a concurrency-safe sink for a slog handler: the pump
// goroutine writes to it while the test goroutine reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startDaemonWithLogger is startDaemon plus a caller-supplied logger, so a
// drain test can read back the Canceled / Unavailable it classified.
func startDaemonWithLogger(t *testing.T, path, digest string, logger *slog.Logger) *Daemon {
	t.Helper()

	dir := t.TempDir()
	d, err := Start(withDeadline(t), Config{
		Node:         "test-node",
		DBPath:       filepath.Join(dir, "state.db"),
		ListenAddr:   "127.0.0.1:0",
		AdminAddr:    "127.0.0.1:0",
		PluginPath:   path,
		PluginSHA256: digest,
		Logger:       logger,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := d.Close(context.Background()); closeErr != nil {
			t.Errorf("Close: %v", closeErr)
		}
	})
	return d
}

// waitForInFlight blocks until the manager reports at least one in-flight
// delivery, so a reload fires at a deterministic moment instead of racing a
// fixed sleep.
func waitForInFlight(t *testing.T, ctx context.Context, d *Daemon) {
	t.Helper()
	for {
		if d.plugin.inFlightCount() >= 1 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("no delivery ever went in flight")
		case <-time.After(time.Millisecond):
		}
	}
}

// TestReloadWaitsForSlowInFlightDeliver is design/22 TEST-4: a deliberately
// slow, but finishing, Deliver that is in flight when a reload starts must
// complete — and be journaled — before the process is killed. The drain gate
// proves it by waiting: the reload call itself cannot return faster than the
// slow handler it waited for.
func TestReloadWaitsForSlowInFlightDeliver(t *testing.T) {
	t.Setenv(deliverDelayEnv, "500")
	dir := t.TempDir()
	path, digest := buildHarnessPlugin(t, dir)
	d := startDaemon(t, path, digest)
	ctx := withDeadline(t)

	channel := d.PluginManifest().GetChannels()[0].GetName()
	payload := []byte("slow-in-flight")
	if _, err := clientPublish(ctx, d.KernelAddr(), channel, payload); err != nil {
		t.Fatalf("clientPublish: %v", err)
	}

	waitForInFlight(t, ctx, d)

	start := time.Now()
	if err := d.plugin.reload(ctx, d.plugin.currentSpec()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	elapsed := time.Since(start)

	// The slow handler had ~500ms left when the reload started; if the gate
	// had killed the process instead of draining, the reload would have
	// returned near-instantly.
	if elapsed < 300*time.Millisecond {
		t.Fatalf("reload returned in %v; it did not wait for the in-flight deliver", elapsed)
	}
	// And the payload the slow handler was carrying is on the journal — it
	// drained to completion before the kill.
	pollJournal(t, ctx, d.KernelAddr(), hex.EncodeToString(payload))
}

// TestReloadLosesNoMessagesUnderLoad is the exact-count half of VC-12 in
// miniature: a burst of distinguishable payloads with a reload fired
// mid-stream must all land on the journal, each exactly once — no loss, no
// duplicate.
func TestReloadLosesNoMessagesUnderLoad(t *testing.T) {
	dir := t.TempDir()
	path, digest := buildHarnessPlugin(t, dir)
	d := startDaemon(t, path, digest)
	ctx := withDeadline(t)

	channel := d.PluginManifest().GetChannels()[0].GetName()

	const n = 60
	want := make([]string, n)
	for i := range want {
		want[i] = hex.EncodeToString([]byte(fmt.Sprintf("load-%04d", i)))
	}

	var pubErr error
	published := make(chan struct{})
	go func() {
		defer close(published)
		for i := 0; i < n; i++ {
			payload := []byte(fmt.Sprintf("load-%04d", i))
			if _, err := clientPublish(ctx, d.KernelAddr(), channel, payload); err != nil {
				pubErr = fmt.Errorf("publish %d: %w", i, err)
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()

	// Fire the reload mid-stream, while publishes are still arriving.
	time.Sleep(10 * time.Millisecond)
	if err := d.plugin.reload(ctx, d.plugin.currentSpec()); err != nil {
		t.Fatalf("reload: %v", err)
	}

	<-published
	if pubErr != nil {
		t.Fatalf("%v", pubErr)
	}

	// Wait until every payload is present, then give a late duplicate a
	// chance to show up before the final count assertion.
	for _, w := range want {
		pollJournal(t, ctx, d.KernelAddr(), w)
	}
	time.Sleep(200 * time.Millisecond)

	lines, err := clientJournalRead(ctx, d.KernelAddr(), "records")
	if err != nil {
		t.Fatalf("clientJournalRead: %v", err)
	}
	seen := make(map[string]int, len(lines))
	for _, line := range lines {
		seen[line]++
	}
	for _, w := range want {
		switch seen[w] {
		case 0:
			t.Errorf("payload %q was lost across the reload", w)
		case 1:
		default:
			t.Errorf("payload %q was delivered %d times across the reload", w, seen[w])
		}
	}
	if len(lines) != n {
		t.Fatalf("journal holds %d records, want exactly %d (loss or duplication)", len(lines), n)
	}
}

// TestReloadKillsHungPluginAtDeadline is the fall-through case: a Deliver that
// never returns must not stall a reload forever. The gate waits to its
// deadline, cancels the hung call (logged as a deliberate Canceled), kills the
// process, and relaunches — so the reload returns and the plugin process is a
// new one.
func TestReloadKillsHungPluginAtDeadline(t *testing.T) {
	t.Setenv(deliverHangEnv, "1")
	dir := t.TempDir()
	path, digest := buildHarnessPlugin(t, dir)

	logs := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	d := startDaemonWithLogger(t, path, digest, logger)
	ctx := withDeadline(t)

	channel := d.PluginManifest().GetChannels()[0].GetName()
	if _, err := clientPublish(ctx, d.KernelAddr(), channel, []byte("hang")); err != nil {
		t.Fatalf("clientPublish: %v", err)
	}
	waitForInFlight(t, ctx, d)

	oldPID := d.plugin.liveHost().PID()
	d.plugin.drainDeadline = 200 * time.Millisecond

	start := time.Now()
	if err := d.plugin.reload(ctx, d.plugin.currentSpec()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 150*time.Millisecond {
		t.Fatalf("reload returned in %v; it did not wait out the drain deadline", elapsed)
	}
	newPID := d.plugin.liveHost().PID()
	if newPID == 0 || newPID == oldPID {
		t.Fatalf("plugin was not relaunched: old pid %d, new pid %d", oldPID, newPID)
	}
	// The hung call was stopped deliberately, so it is logged as Canceled.
	waitForLog(t, logs, "reason=canceled")
}

// TestDeadChildLogsUnavailable is the other side of the Canceled/Unavailable
// split: when the plugin process dies on its own (here, a kill signal), the
// next delivery fails with Unavailable, not Canceled.
func TestDeadChildLogsUnavailable(t *testing.T) {
	dir := t.TempDir()
	path, digest := buildHarnessPlugin(t, dir)

	logs := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	d := startDaemonWithLogger(t, path, digest, logger)
	ctx := withDeadline(t)

	channel := d.PluginManifest().GetChannels()[0].GetName()

	// Kill the plugin process outright — a crash, not a drain.
	pid := d.plugin.liveHost().PID()
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill plugin pid %d: %v", pid, err)
	}

	// A publish now has nowhere healthy to go; the dispatcher's first attempt
	// hits the dead connection and classifies it Unavailable.
	if _, err := clientPublish(ctx, d.KernelAddr(), channel, []byte("after-kill")); err != nil {
		t.Fatalf("clientPublish: %v", err)
	}
	waitForLog(t, logs, "reason=unavailable")
}

// waitForLog polls the log sink until want appears, or the test's deadline.
func waitForLog(t *testing.T, logs *lockedBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if strings.Contains(logs.String(), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("log never contained %q; log so far:\n%s", want, logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
