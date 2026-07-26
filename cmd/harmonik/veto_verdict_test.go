package main

// veto_verdict_test.go — behavior tests for `harmonik veto-verdict` argument
// parsing/validation and its success-message dispatch (RC-027, hk-63oh.39).

import (
	"encoding/json"
	"testing"
)

func TestRunVetoVerdict_MissingRunID(t *testing.T) {
	vgSilenceStd(t)
	if code := runVetoVerdictSubcommand(nil); code != 1 {
		t.Fatalf("exit code = %d, want 1 for missing run_id", code)
	}
}

func TestRunVetoVerdict_ExtraArg(t *testing.T) {
	vgSilenceStd(t)
	if code := runVetoVerdictSubcommand([]string{"run-a", "run-b"}); code != 1 {
		t.Fatalf("exit code = %d, want 1 for a second positional", code)
	}
}

func TestRunVetoVerdict_UnknownFlag(t *testing.T) {
	vgSilenceStd(t)
	if code := runVetoVerdictSubcommand([]string{"--bogus"}); code != 1 {
		t.Fatalf("exit code = %d, want 1 for unknown flag", code)
	}
}

func TestRunVetoVerdict_InvalidPromoteTo(t *testing.T) {
	vgSilenceStd(t)
	code := runVetoVerdictSubcommand([]string{"run-a", "--promote-to", "somewhere-else"})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for invalid --promote-to value", code)
	}
}

func TestRunVetoVerdict_Help(t *testing.T) {
	vgSilenceStd(t)
	for _, a := range []string{"--help", "-h"} {
		if code := runVetoVerdictSubcommand([]string{a}); code != 0 {
			t.Errorf("%s: exit code = %d, want 0", a, code)
		}
	}
}

func TestRunVetoVerdict_NonexistentProject(t *testing.T) {
	vgSilenceStd(t)
	code := runVetoVerdictSubcommand([]string{"run-a", "--project", "/no/such/dir/hkv-veto-missing"})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for missing project dir", code)
	}
}

// TestRunVetoVerdict_ReachesDaemonDial confirms a valid invocation passes all
// local validation, dials, and (with no daemon) surfaces exit 17.
func TestRunVetoVerdict_ReachesDaemonDial(t *testing.T) {
	vgSilenceStd(t)
	dir := t.TempDir()
	if code := runVetoVerdictSubcommand([]string{"run-a", "--project", dir}); code != 17 {
		t.Fatalf("exit code = %d, want 17 (validated, dialed, no daemon)", code)
	}
}

// TestSendVetoVerdictRequest_PlainVetoSuccess drives sendVetoVerdictRequest
// through a fake daemon that accepts the veto (ok:true) and confirms the plain
// (no-promote) path returns 0.
func TestSendVetoVerdictRequest_PlainVetoSuccess(t *testing.T) {
	vgSilenceStd(t)
	dir, gotReq := startFakeVerdictDaemon(t, map[string]any{"ok": true})

	if code := sendVetoVerdictRequest(dir, "run-a", ""); code != 0 {
		t.Fatalf("exit code = %d, want 0 for accepted veto", code)
	}
	var req struct {
		Op        string `json:"op"`
		PromoteTo string `json:"promote_to"`
	}
	if err := json.Unmarshal(<-gotReq, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if req.Op != "veto_verdict" {
		t.Errorf("op = %q, want veto_verdict", req.Op)
	}
	if req.PromoteTo != "" {
		t.Errorf("promote_to = %q, want empty for plain veto", req.PromoteTo)
	}
}

// TestSendVetoVerdictRequest_PromoteSuccess drives the promote-to-escalate path.
func TestSendVetoVerdictRequest_PromoteSuccess(t *testing.T) {
	vgSilenceStd(t)
	dir, gotReq := startFakeVerdictDaemon(t, map[string]any{"ok": true})

	if code := sendVetoVerdictRequest(dir, "run-a", "escalate-to-human"); code != 0 {
		t.Fatalf("exit code = %d, want 0 for accepted promoted veto", code)
	}
	var req struct {
		PromoteTo string `json:"promote_to"`
	}
	if err := json.Unmarshal(<-gotReq, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if req.PromoteTo != "escalate-to-human" {
		t.Errorf("promote_to = %q, want escalate-to-human", req.PromoteTo)
	}
}
