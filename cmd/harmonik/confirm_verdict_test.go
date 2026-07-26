package main

// confirm_verdict_test.go — behavior tests for `harmonik confirm-verdict`,
// the shared sendVerdictOverrideRequest socket client, and the two dial-error
// predicate helpers (RC-027, hk-63oh.39).

import (
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// startFakeVerdictDaemon listens on ${projectDir}/.harmonik/daemon.sock and
// answers exactly one connection with respBody (JSON-marshalled). The bytes the
// client sent are delivered on the returned channel so the test can assert the
// request shape. Uses a short /tmp-rooted dir to stay under the ~104-byte unix
// socket path limit on macOS.
func startFakeVerdictDaemon(t *testing.T, respBody map[string]any) (projectDir string, gotReq <-chan []byte) {
	t.Helper()
	base, err := os.MkdirTemp("/tmp", "hkv")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	sockDir := filepath.Join(base, ".harmonik")
	if err := os.MkdirAll(sockDir, 0o755); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	sockPath := filepath.Join(sockDir, "daemon.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix %q: %v", sockPath, err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	reqCh := make(chan []byte, 1)
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			reqCh <- nil
			return
		}
		defer func() { _ = conn.Close() }()
		// Client writes the payload then half-closes; read to EOF.
		reqBytes, _ := io.ReadAll(conn)
		reqCh <- reqBytes
		out, _ := json.Marshal(respBody)
		_, _ = conn.Write(out)
	}()
	return base, reqCh
}

func TestSendVerdictOverride_ConfirmOK(t *testing.T) {
	vgSilenceStd(t)
	dir, gotReq := startFakeVerdictDaemon(t, map[string]any{"ok": true})

	code := sendVerdictOverrideRequest(dir, "run-abc", "confirm_verdict", "")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 on ok response", code)
	}

	// Assert the request the client marshalled.
	raw := <-gotReq
	var req struct {
		Op        string `json:"op"`
		RunID     string `json:"run_id"`
		PromoteTo string `json:"promote_to"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("decode request: %v (raw=%q)", err, raw)
	}
	if req.Op != "confirm_verdict" || req.RunID != "run-abc" {
		t.Errorf("request = %+v, want op=confirm_verdict run_id=run-abc", req)
	}
	if req.PromoteTo != "" {
		t.Errorf("promote_to = %q, want empty (omitempty)", req.PromoteTo)
	}
}

func TestSendVerdictOverride_VetoPromotePropagated(t *testing.T) {
	vgSilenceStd(t)
	dir, gotReq := startFakeVerdictDaemon(t, map[string]any{"ok": true})

	code := sendVerdictOverrideRequest(dir, "run-xyz", "veto_verdict", "escalate-to-human")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	var req struct {
		Op        string `json:"op"`
		PromoteTo string `json:"promote_to"`
	}
	if err := json.Unmarshal(<-gotReq, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if req.Op != "veto_verdict" || req.PromoteTo != "escalate-to-human" {
		t.Errorf("request = %+v, want op=veto_verdict promote_to=escalate-to-human", req)
	}
}

func TestSendVerdictOverride_InvalidStateCode16(t *testing.T) {
	vgSilenceStd(t)
	dir, _ := startFakeVerdictDaemon(t, map[string]any{"ok": false, "error_code": 16, "error": "no pending"})

	code := sendVerdictOverrideRequest(dir, "run-abc", "confirm_verdict", "")
	if code != 16 {
		t.Fatalf("exit code = %d, want 16 (operator-control-invalid-state)", code)
	}
}

func TestSendVerdictOverride_OtherDaemonErrorCode1(t *testing.T) {
	vgSilenceStd(t)
	dir, _ := startFakeVerdictDaemon(t, map[string]any{"ok": false, "error_code": 42, "error": "boom"})

	code := sendVerdictOverrideRequest(dir, "run-abc", "confirm_verdict", "")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for non-16 daemon rejection", code)
	}
}

func TestSendVerdictOverride_DaemonNotRunning(t *testing.T) {
	vgSilenceStd(t)
	// A real project dir but no socket => dial fails with socket-absent => 17.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755); err != nil {
		t.Fatal(err)
	}
	code := sendVerdictOverrideRequest(dir, "run-abc", "confirm_verdict", "")
	if code != 17 {
		t.Fatalf("exit code = %d, want 17 (daemon not running)", code)
	}
}

func TestRunConfirmVerdict_MissingRunID(t *testing.T) {
	vgSilenceStd(t)
	if code := runConfirmVerdictSubcommand(nil); code != 1 {
		t.Fatalf("exit code = %d, want 1 for missing run_id", code)
	}
}

func TestRunConfirmVerdict_ExtraArg(t *testing.T) {
	vgSilenceStd(t)
	if code := runConfirmVerdictSubcommand([]string{"run-a", "run-b"}); code != 1 {
		t.Fatalf("exit code = %d, want 1 for a second positional", code)
	}
}

func TestRunConfirmVerdict_UnknownFlag(t *testing.T) {
	vgSilenceStd(t)
	if code := runConfirmVerdictSubcommand([]string{"--bogus"}); code != 1 {
		t.Fatalf("exit code = %d, want 1 for unknown flag", code)
	}
}

func TestRunConfirmVerdict_Help(t *testing.T) {
	vgSilenceStd(t)
	for _, a := range []string{"--help", "-h"} {
		if code := runConfirmVerdictSubcommand([]string{a}); code != 0 {
			t.Errorf("%s: exit code = %d, want 0", a, code)
		}
	}
}

func TestRunConfirmVerdict_NonexistentProject(t *testing.T) {
	vgSilenceStd(t)
	code := runConfirmVerdictSubcommand([]string{"run-a", "--project", "/no/such/dir/hkv-does-not-exist"})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for missing project dir", code)
	}
}

func TestRunConfirmVerdict_ReachesDaemonDial(t *testing.T) {
	vgSilenceStd(t)
	// Existing project dir, no daemon => passes validation, dials, returns 17.
	dir := t.TempDir()
	code := runConfirmVerdictSubcommand([]string{"run-a", "--project", dir})
	if code != 17 {
		t.Fatalf("exit code = %d, want 17 (validated args, dialed, no daemon)", code)
	}
}

func TestVerdictSocketAbsent(t *testing.T) {
	if !isVerdictSocketAbsent(fs.ErrNotExist) {
		t.Error("fs.ErrNotExist should be treated as socket-absent")
	}
	if !isVerdictSocketAbsent(syscall.EINVAL) {
		t.Error("syscall.EINVAL should be treated as socket-absent (macOS)")
	}
	if isVerdictSocketAbsent(syscall.ECONNREFUSED) {
		t.Error("ECONNREFUSED must NOT be socket-absent")
	}
	if isVerdictSocketAbsent(nil) {
		t.Error("nil must NOT be socket-absent")
	}
}

func TestVerdictConnectionRefused(t *testing.T) {
	if !isVerdictConnectionRefused(syscall.ECONNREFUSED) {
		t.Error("ECONNREFUSED should be connection-refused")
	}
	if isVerdictConnectionRefused(fs.ErrNotExist) {
		t.Error("fs.ErrNotExist must NOT be connection-refused")
	}
	if isVerdictConnectionRefused(nil) {
		t.Error("nil must NOT be connection-refused")
	}
}
