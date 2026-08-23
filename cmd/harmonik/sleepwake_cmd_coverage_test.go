package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func captureSleepWakeIO(t *testing.T, fn func() int) (out string, code int) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	code = fn()
	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	bOut, _ := io.ReadAll(rOut)
	bErr, _ := io.ReadAll(rErr)
	_ = rOut.Close()
	_ = rErr.Close()
	return string(bOut) + string(bErr), code
}

func TestResolveSleepWakeSock(t *testing.T) {
	got, code := resolveSleepWakeSock("/proj/x", "sleep")
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if want := filepath.Join("/proj/x", ".harmonik", "daemon.sock"); got != want {
		t.Errorf("sock = %q, want %q", got, want)
	}

	got, code = resolveSleepWakeSock(".", "wake")
	if code != 0 {
		t.Fatalf("relative dir code = %d, want 0", code)
	}
	if !filepath.IsAbs(got) || !strings.HasSuffix(got, filepath.Join(".harmonik", "daemon.sock")) {
		t.Errorf("relative resolution = %q, want an absolute path ending in .harmonik/daemon.sock", got)
	}
}

func TestIsSleepWakeSocketAbsent(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"OpError wrapping PathError wrapping ENOENT", &net.OpError{Err: &os.PathError{Op: "stat", Err: syscall.ENOENT}}, true},
		{"OpError wrapping ENOENT directly", &net.OpError{Err: syscall.ENOENT}, true},
		{"bare ENOENT", syscall.ENOENT, true},
		{"ECONNREFUSED is not absence", syscall.ECONNREFUSED, false},
		{"OpError wrapping ECONNREFUSED is not absence", &net.OpError{Err: syscall.ECONNREFUSED}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSleepWakeSocketAbsent(tc.err); got != tc.want {
				t.Errorf("isSleepWakeSocketAbsent = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsSleepWakeConnRefused(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"OpError wrapping SyscallError wrapping ECONNREFUSED", &net.OpError{Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}, true},
		{"OpError wrapping ECONNREFUSED directly", &net.OpError{Err: syscall.ECONNREFUSED}, true},
		{"bare ECONNREFUSED", syscall.ECONNREFUSED, true},
		{"ENOENT is not refusal", syscall.ENOENT, false},
		{"OpError wrapping SyscallError wrapping ENOENT is not refusal", &net.OpError{Err: &os.SyscallError{Syscall: "connect", Err: syscall.ENOENT}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSleepWakeConnRefused(tc.err); got != tc.want {
				t.Errorf("isSleepWakeConnRefused = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRunSleepGateSubcommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, code := captureSleepWakeIO(t, func() int { return runSleepGateSubcommand([]string{"--project", dir}) }); code != 1 {
		t.Errorf("awake gate code = %d, want 1", code)
	}

	marker := filepath.Join(dir, ".harmonik", ".fleet-sleeping")
	if err := os.WriteFile(marker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	if _, code := captureSleepWakeIO(t, func() int { return runSleepGateSubcommand([]string{"--project", dir}) }); code != 0 {
		t.Errorf("sleeping gate code = %d, want 0", code)
	}

	if _, code := captureSleepWakeIO(t, func() int { return runSleepGateSubcommand([]string{"--project=" + dir}) }); code != 0 {
		t.Errorf("--project= form code = %d, want 0", code)
	}

	if _, code := captureSleepWakeIO(t, func() int { return runSleepGateSubcommand([]string{"--bogus"}) }); code != 2 {
		t.Errorf("bad-arg gate code = %d, want 2", code)
	}

	out, code := captureSleepWakeIO(t, func() int { return runSleepGateSubcommand([]string{"--help"}) })
	if code != 0 || !strings.Contains(out, "sleep-gate") {
		t.Errorf("help gate: code=%d out=%q", code, out)
	}
}

func shortProjectDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hksw")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if len(filepath.Join(dir, ".harmonik", "daemon.sock")) >= 104 {
		t.Skipf("socket path under %q too long for this platform", dir)
	}
	return dir
}

func TestRunSleepSubcommand_ArgAndSocket(t *testing.T) {
	ctx := context.Background()
	dir := shortProjectDir(t) // no daemon.sock present → dial yields ENOENT → exit 17

	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantSub  string
	}{
		{"unrecognized argument", []string{"--nope"}, 1, "unrecognized argument"},
		{"--help", []string{"--help"}, 0, "harmonik sleep"},
		{"no daemon socket present", []string{"--project", dir}, 17, "daemon not running"},
		{"--force still needs the socket", []string{"--force", "--project", dir}, 17, "daemon not running"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, code := captureSleepWakeIO(t, func() int { return runSleepSubcommand(ctx, tc.args) })
			if code != tc.wantCode {
				t.Fatalf("code = %d, want %d (out=%q)", code, tc.wantCode, out)
			}
			if !strings.Contains(out, tc.wantSub) {
				t.Errorf("output %q must contain %q", out, tc.wantSub)
			}
		})
	}
}

func TestRunWakeSubcommand_ArgAndSocket(t *testing.T) {
	ctx := context.Background()
	dir := shortProjectDir(t)

	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantSub  string
	}{
		{"neither --agent nor --all", nil, 1, "provide --agent <name> or --all"},
		{"--agent and --all are mutually exclusive", []string{"--all", "--agent", "captain"}, 1, "mutually exclusive"},
		{"unrecognized argument", []string{"--nope"}, 1, "unrecognized argument"},
		{"--help", []string{"--help"}, 0, "harmonik wake"},
		{"--all with no daemon socket", []string{"--all", "--project", dir}, 17, "daemon not running"},
		{"--agent with no daemon socket", []string{"--agent", "captain", "--project", dir}, 17, "daemon not running"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, code := captureSleepWakeIO(t, func() int { return runWakeSubcommand(ctx, tc.args) })
			if code != tc.wantCode {
				t.Fatalf("code = %d, want %d (out=%q)", code, tc.wantCode, out)
			}
			if !strings.Contains(out, tc.wantSub) {
				t.Errorf("output %q must contain %q", out, tc.wantSub)
			}
		})
	}
}

// TestSendSleepWakeRequest drives the request round-trip against a real
// in-process unix socket, covering the happy path (ok=true), the daemon-error
// path (ok=false surfaced to the caller), and the socket-absent (exit 17) path.
func TestSendSleepWakeRequest(t *testing.T) {
	ctx := context.Background()

	absent := filepath.Join(t.TempDir(), "missing.sock")
	if _, code := sendSleepWakeRequest(ctx, absent, []byte(`{}`), "sleep"); code != 17 {
		t.Errorf("absent socket code = %d, want 17", code)
	}

	base, err := os.MkdirTemp("", "sw")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	sockPath := filepath.Join(base, "d.sock")
	if len(sockPath) >= 104 {
		t.Skipf("socket path %q too long for this platform", sockPath)
	}

	serve := func(t *testing.T, reply sleepWakeSocketResponse) string {
		t.Helper()
		ln, err := net.Listen("unix", sockPath)
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		defer func() { _ = ln.Close() }()
		done := make(chan []byte, 1)
		go func() {
			c, aerr := ln.Accept()
			if aerr != nil {
				done <- nil
				return
			}
			defer func() { _ = c.Close() }()
			got, _ := io.ReadAll(c)
			body, _ := json.Marshal(reply)
			_, _ = c.Write(body)
			done <- got
		}()
		resp, code := sendSleepWakeRequest(ctx, sockPath, []byte(`{"op":"daemon-sleep"}`), "sleep")
		if code != 0 {
			t.Fatalf("round-trip code = %d, want 0", code)
		}
		if resp.Ok != reply.Ok || resp.Error != reply.Error {
			t.Errorf("resp = %+v, want %+v", resp, reply)
		}
		return string(<-done)
	}

	if req := serve(t, sleepWakeSocketResponse{Ok: true}); !strings.Contains(req, "daemon-sleep") {
		t.Errorf("server received %q, want it to contain the op payload", req)
	}
	_ = os.Remove(sockPath)
	serve(t, sleepWakeSocketResponse{Ok: false, Error: "fleet still draining"})
}
