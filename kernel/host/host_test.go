package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// Mirrors the environment variable names hoststub/main.go reads. Kept as
// literal constants here, not imported, because hoststub is package main.
// exec.Command inherits the test process's own environment when Cmd.Env is
// left nil, which is what Launch does, so t.Setenv before Launch is enough
// to steer the launched hoststub without any extra plumbing in LaunchSpec.
const (
	stubChannelEnv        = "HARMONIK_KERNEL_HOSTSTUB_CHANNEL"
	stubDeliverDelayMsEnv = "HARMONIK_KERNEL_HOSTSTUB_DELIVER_DELAY_MS"
)

func withDeadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// buildStub compiles hoststub once into dir and returns its path and sha256.
func buildStub(t *testing.T, dir string) (path, sha256Hex string) {
	t.Helper()

	path = filepath.Join(dir, "hoststub")
	buildCmd := exec.CommandContext(context.Background(), "go", "build", "-o", path,
		"github.com/gregberns/harmonik/kernel/host/hoststub")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build hoststub: %v\n%s", err, out)
	}

	f, err := os.Open(path) //nolint:gosec // path is this test's own t.TempDir() build output, not external input
	if err != nil {
		t.Fatalf("open built hoststub: %v", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Fatalf("close built hoststub: %v", closeErr)
		}
	}()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("sha256 built hoststub: %v", err)
	}
	return path, hex.EncodeToString(h.Sum(nil))
}

func TestLaunchReachesRunningWithADistinctPID(t *testing.T) {
	dir := t.TempDir()
	path, digest := buildStub(t, dir)

	h, err := Launch(withDeadline(t), LaunchSpec{
		Path:           path,
		SHA256:         digest,
		Node:           "box-1",
		KernelEndpoint: "unix:///tmp/does-not-matter.sock",
		CallerID:       "test",
		APIVersion:     1,
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	t.Cleanup(h.Kill)

	if h.State() != StateRunning {
		t.Fatalf("State() = %v, want %v", h.State(), StateRunning)
	}
	if h.PID() == 0 || h.PID() == os.Getpid() {
		t.Fatalf("PID() = %d, want a distinct nonzero pid", h.PID())
	}
	if got := h.Manifest().GetNamespace(); got != "hoststub" {
		t.Fatalf("Manifest().Namespace = %q, want %q", got, "hoststub")
	}

	resp, err := h.Deliver(withDeadline(t), &kernelv1.Envelope{
		Channel: "hoststub.in",
		Payload: []byte("hello"),
	})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if resp == nil {
		t.Fatal("Deliver returned a nil response with a nil error")
	}
}

func TestLaunchRefusesOnChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	path, _ := buildStub(t, dir)

	_, err := Launch(withDeadline(t), LaunchSpec{
		Path:   path,
		SHA256: strings.Repeat("0", 64),
	})
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Launch err = %v, want ErrChecksumMismatch", err)
	}
}

func TestLaunchRefusesOutOfNamespaceChannel(t *testing.T) {
	dir := t.TempDir()
	path, digest := buildStub(t, dir)
	t.Setenv(stubChannelEnv, "other-namespace.in")

	_, err := Launch(withDeadline(t), LaunchSpec{
		Path:   path,
		SHA256: digest,
		Node:   "box-1",
	})
	if !errors.Is(err, ErrNamespaceViolation) {
		t.Fatalf("Launch err = %v, want ErrNamespaceViolation", err)
	}
}

func TestDeliverRefusesUndeclaredChannel(t *testing.T) {
	dir := t.TempDir()
	path, digest := buildStub(t, dir)

	h, err := Launch(withDeadline(t), LaunchSpec{Path: path, SHA256: digest, Node: "box-1"})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	t.Cleanup(h.Kill)

	_, err = h.Deliver(withDeadline(t), &kernelv1.Envelope{Channel: "hoststub.never-declared", Payload: []byte("x")})
	if !errors.Is(err, ErrUndeclaredChannel) {
		t.Fatalf("Deliver err = %v, want ErrUndeclaredChannel", err)
	}
}

func TestKillMidDeliverYieldsUnavailable(t *testing.T) {
	dir := t.TempDir()
	path, digest := buildStub(t, dir)
	t.Setenv(stubDeliverDelayMsEnv, "2000")

	h, err := Launch(withDeadline(t), LaunchSpec{Path: path, SHA256: digest, Node: "box-1"})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	deliverErr := make(chan error, 1)
	go func() {
		_, deliverCallErr := h.Deliver(withDeadline(t), &kernelv1.Envelope{Channel: "hoststub.in", Payload: []byte("x")})
		deliverErr <- deliverCallErr
	}()

	time.Sleep(200 * time.Millisecond) // give Deliver time to be in flight
	h.Kill()

	select {
	case deliverCallErr := <-deliverErr:
		if deliverCallErr == nil {
			t.Fatal("Deliver returned nil error after Kill mid-call")
		}
		if !errors.Is(deliverCallErr, ErrUnavailable) && status.Code(deliverCallErr) != codes.Unavailable {
			t.Fatalf("Deliver err = %v, want ErrUnavailable/codes.Unavailable", deliverCallErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Deliver never returned after Kill mid-call")
	}

	if h.State() != StateUnavailable {
		t.Fatalf("State() = %v, want %v", h.State(), StateUnavailable)
	}
}

// TestPrewarmMeasuresTheDarwinSignatureCost is K5's acceptance measurement:
// this box's OS pays a one-time cost on a binary's first exec (macOS's code-
// signature check), and PREWARMED exists to pay that cost off any reload
// path. It logs both durations for the acceptance record rather than
// asserting a specific number, because the absolute cost is host-hardware-
// dependent — what the acceptance needs is the shape (first exec far slower
// than a second exec of the same, now-cached, binary).
func TestPrewarmMeasuresTheDarwinSignatureCost(t *testing.T) {
	dir := t.TempDir()
	path, _ := buildStub(t, dir)

	first, err := prewarm(withDeadline(t), path)
	if err != nil {
		t.Fatalf("first prewarm: %v", err)
	}
	second, err := prewarm(withDeadline(t), path)
	if err != nil {
		t.Fatalf("second prewarm: %v", err)
	}

	t.Logf("prewarm exec durations on this box: first=%s second=%s", first, second)
}
