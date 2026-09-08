package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func withDeadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// buildHarnessPlugin compiles kernel/cmd/harmonikd/harnesstestplugin once
// into dir and returns its path and sha256 — the same shape the real
// composition root's config takes for any plugin.
func buildHarnessPlugin(t *testing.T, dir string) (path, sha256Hex string) {
	t.Helper()

	path = filepath.Join(dir, "harnesstestplugin")
	buildCmd := exec.CommandContext(context.Background(), "go", "build", "-o", path,
		"github.com/gregberns/harmonik/kernel/cmd/harmonikd/harnesstestplugin")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build harness plugin: %v\n%s", err, out)
	}

	f, err := os.Open(path) //nolint:gosec // path is this test's own t.TempDir() build output, not external input
	if err != nil {
		t.Fatalf("open built harness plugin: %v", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Fatalf("close built harness plugin: %v", closeErr)
		}
	}()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("sha256 built harness plugin: %v", err)
	}
	return path, hex.EncodeToString(h.Sum(nil))
}

func startDaemon(t *testing.T, path, digest string) *Daemon {
	t.Helper()

	dir := t.TempDir()
	d, err := Start(withDeadline(t), Config{
		Node:         "test-node",
		DBPath:       filepath.Join(dir, "state.db"),
		ListenAddr:   "127.0.0.1:0",
		AdminAddr:    "127.0.0.1:0",
		PluginPath:   path,
		PluginSHA256: digest,
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

// harnessJournal is the only journal the harness plugin ever writes to (its
// default), so every test reads back from this one name.
const harnessJournal = "records"

// pollJournal retries clientJournalRead until want is present or ctx times
// out; the plugin's own JournalAppend call happens asynchronously to
// Publish returning, so a single read can legitimately race it.
func pollJournal(t *testing.T, ctx context.Context, addr, want string) []string {
	t.Helper()
	for {
		lines, err := clientJournalRead(ctx, addr, harnessJournal)
		if err != nil {
			t.Fatalf("clientJournalRead: %v", err)
		}
		for _, line := range lines {
			if line == want {
				return lines
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("journal %q never contained %q; last read: %v", harnessJournal, want, lines)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// TestSplitDemoRoundTrips is K7's acceptance in code: publish a payload to
// the registered plugin's declared channel through the real KernelService
// gRPC surface (the same call the "publish" client verb makes), and read it
// back byte-for-byte through the same journal name the plugin's own Deliver
// wrote to (the same call "journal read" makes).
func TestSplitDemoRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path, digest := buildHarnessPlugin(t, dir)
	d := startDaemon(t, path, digest)
	ctx := withDeadline(t)

	channel := d.PluginManifest().GetChannels()[0].GetName()
	payload := []byte{0xca, 0xfe, 0x00, 0x42}

	if _, err := clientPublish(ctx, d.KernelAddr(), channel, payload); err != nil {
		t.Fatalf("clientPublish: %v", err)
	}

	want := hex.EncodeToString(payload)
	pollJournal(t, ctx, d.KernelAddr(), want)
}

// TestPublishWhileStoppedStaysQueuedThenDelivers is the other half of K7's
// acceptance: a publish that lands while the registered plugin process is
// down must not be lost. It must sit in the kernel's own subscription queue
// (transport, K4) until a plugin reload (also a K7 client verb) brings a
// live process back, at which point the dispatch loop this file adds
// delivers it — never re-published, never dropped.
func TestPublishWhileStoppedStaysQueuedThenDelivers(t *testing.T) {
	dir := t.TempDir()
	path, digest := buildHarnessPlugin(t, dir)
	d := startDaemon(t, path, digest)
	ctx := withDeadline(t)

	channel := d.PluginManifest().GetChannels()[0].GetName()
	firstPayload := []byte("first")
	secondPayload := []byte("second")

	if _, err := clientPublish(ctx, d.KernelAddr(), channel, firstPayload); err != nil {
		t.Fatalf("clientPublish (first): %v", err)
	}
	pollJournal(t, ctx, d.KernelAddr(), hex.EncodeToString(firstPayload))

	// Simulate the plugin process going down without a reload, the way an
	// unexpected exit would leave things: killed, and no process installed
	// in its place until reload runs.
	d.plugin.forceUnavailable()

	if _, err := clientPublish(ctx, d.KernelAddr(), channel, secondPayload); err != nil {
		t.Fatalf("clientPublish (second): %v", err)
	}

	// Give the dispatch loop several idle cycles to prove it is NOT
	// delivering: the second payload must stay off the journal while no
	// process is registered.
	time.Sleep(5 * idleBackoff)
	lines, err := clientJournalRead(ctx, d.KernelAddr(), "records")
	if err != nil {
		t.Fatalf("clientJournalRead: %v", err)
	}
	for _, line := range lines {
		if line == hex.EncodeToString(secondPayload) {
			t.Fatalf("second payload was delivered while the plugin was stopped; journal: %v", lines)
		}
	}
	if pending := d.plugin.pendingCount(); pending == 0 {
		t.Fatal("kernel-held subscription reports no pending envelopes while the plugin is stopped")
	}

	if err := requestReload(ctx, d.AdminAddr(), reloadRequest{Namespace: d.PluginManifest().GetNamespace()}); err != nil {
		t.Fatalf("requestReload: %v", err)
	}

	pollJournal(t, ctx, d.KernelAddr(), hex.EncodeToString(secondPayload))
}
