package daemon

// routed_preexec_identity_hksll_test.go — regression tests for
// hk-sll-claude-leak-z8fs0 and hk-sll-empty-logpath-7dxdw.
//
// # The two defects
//
// buildCodexRoutedLaunchSpec serves EVERY non-claude harness, and it built the
// CHB-018 pre-exec messages through the claude handler's PreExecMessages with
// two constants baked in:
//
//   - a hard-coded agent_type of "claude-code", so every pi and every codex run
//     announced session_log_location{agent_type:"claude-code"} — one event after
//     harness_selected{agent_type:"pi"} said otherwise (hk-sll-claude-leak-z8fs0).
//   - an empty log_path, which core.SessionLogLocationPayload.Valid() rejects
//     outright (event-model.md §8.3.7).
//
// Observed live on 2026-08-09 against a daemon built from 6920f9cf3, driving the
// pi seed bead as-p4i:
//
//	harness_selected      {"agent_type":"pi","bead_id":"as-p4i","tier":1}
//	session_log_location  {"agent_type":"claude-code","log_path":"","node_id":"bead/as-p4i"}
//
// # What these tests pin
//
//  1. The routed builder REPORTS the resolved harness — "codex" for a codex run,
//     "pi" for a pi run.
//  2. It announces a non-empty session-log path.
//  3. The whole payload satisfies core.SessionLogLocationPayload.Valid(), so what
//     the daemon publishes is something its own contract accepts.
//
// The guard half of hk-sll-empty-logpath-7dxdw — the emit path refusing an
// invalid payload — is pinned next to that code, in
// internal/runlaunch/sessionlogloc_guard_hksll_test.go.
//
// Helper prefix: hksll (per implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/codex"
	"github.com/gregberns/harmonik/internal/harness/pi"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/workspace"
)

// hksllRunID returns a fresh non-nil run id.
func hksllRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// hksllRawSessionLogLocation returns the raw session_log_location pre-exec
// message.
func hksllRawSessionLogLocation(t *testing.T, arts shared.LaunchArtifacts) json.RawMessage {
	t.Helper()
	for _, raw := range arts.PreExecMsgs {
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatalf("pre-exec message is not JSON: %v (%s)", err, string(raw))
		}
		if envelope.Type == handlercontract.ProgressMsgTypeSessionLogLocation {
			return raw
		}
	}
	t.Fatal("no session_log_location message among the pre-exec messages")
	return nil
}

// hksllSessionLogLocation decodes the on-wire message shape.
func hksllSessionLogLocation(t *testing.T, arts shared.LaunchArtifacts) handlercontract.SessionLogLocationMsg {
	t.Helper()
	var msg handlercontract.SessionLogLocationMsg
	if err := json.Unmarshal(hksllRawSessionLogLocation(t, arts), &msg); err != nil {
		t.Fatalf("decode session_log_location: %v", err)
	}
	return msg
}

// hksllWorktree returns a temp worktree path with a .harmonik dir, the shape the
// routed builder expects.
func hksllWorktree(t *testing.T) string {
	t.Helper()
	wt := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wt, ".harmonik"), 0o750); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	return wt
}

// TestRoutedPreExec_ReportsResolvedHarness_hksllclaudeleak is the
// hk-sll-claude-leak-z8fs0 regression: session_log_location.agent_type MUST name
// the handler subprocess that actually runs, for every non-claude harness.
func TestRoutedPreExec_ReportsResolvedHarness_hksllclaudeleak(t *testing.T) {
	ctx := context.Background()

	t.Run("codex", func(t *testing.T) {
		wt := hksllWorktree(t)
		rc := shared.LaunchCtx{
			RunID:           hksllRunID(t),
			BeadID:          "hksll-codex",
			WorkspacePath:   wt,
			Phase:           "implementer-initial",
			IterationCount:  1,
			BeadTitle:       "codex identity",
			BeadDescription: "body",
		}
		_, arts, err := buildCodexRoutedLaunchSpec(ctx, rc, codex.NewHarness("", ""), core.AgentTypeCodex)
		if err != nil {
			t.Fatalf("buildCodexRoutedLaunchSpec (codex): %v", err)
		}
		got := hksllSessionLogLocation(t, arts).AgentType
		if got != string(core.AgentTypeCodex) {
			t.Errorf("session_log_location.agent_type = %q; want %q — a codex run must not announce a claude identity (hk-sll-claude-leak-z8fs0)",
				got, core.AgentTypeCodex)
		}
	})

	t.Run("pi", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "sk-test-hksll")
		wt := hksllWorktree(t)
		rc := shared.LaunchCtx{
			RunID:           hksllRunID(t),
			BeadID:          "hksll-pi",
			WorkspacePath:   wt,
			Phase:           "implementer-initial",
			IterationCount:  1,
			BeadTitle:       "pi identity",
			BeadDescription: "body",
			HandlerBinary:   "pi",
			Provider:        "openrouter",
			Model:           "openrouter/qwen/qwen3-coder",
			APIKeyEnv:       "OPENROUTER_API_KEY",
			BaseURL:         "http://dgx.local:8080/v1",
			API:             "openai",
		}
		h := pi.NewHarness("pi", "openrouter", "openrouter/qwen/qwen3-coder", "OPENROUTER_API_KEY", "", "", "")
		_, arts, err := buildCodexRoutedLaunchSpec(ctx, rc, h, core.AgentTypePi)
		if err != nil {
			t.Fatalf("buildCodexRoutedLaunchSpec (pi): %v", err)
		}
		got := hksllSessionLogLocation(t, arts).AgentType
		if got != string(core.AgentTypePi) {
			t.Errorf("session_log_location.agent_type = %q; want %q — a pi run must not announce a claude identity (hk-sll-claude-leak-z8fs0)",
				got, core.AgentTypePi)
		}
	})
}

// TestRoutedPreExec_LogPathIsAnnounced_hksllemptylogpath is the
// hk-sll-empty-logpath-7dxdw regression, value half: the routed builder must
// announce a real session-log path, and the resulting payload must satisfy the
// validator the daemon itself documents.
func TestRoutedPreExec_LogPathIsAnnounced_hksllemptylogpath(t *testing.T) {
	ctx := context.Background()
	wt := hksllWorktree(t)
	rc := shared.LaunchCtx{
		RunID:           hksllRunID(t),
		BeadID:          "hksll-logpath",
		WorkspacePath:   wt,
		Phase:           "implementer-initial",
		IterationCount:  1,
		BeadTitle:       "log path",
		BeadDescription: "body",
	}
	_, arts, err := buildCodexRoutedLaunchSpec(ctx, rc, codex.NewHarness("", ""), core.AgentTypeCodex)
	if err != nil {
		t.Fatalf("buildCodexRoutedLaunchSpec: %v", err)
	}

	msg := hksllSessionLogLocation(t, arts)
	if msg.LogPath == "" {
		t.Error("session_log_location.log_path is empty; core.SessionLogLocationPayload.Valid() rejects that (hk-sll-empty-logpath-7dxdw)")
	}
	// The announced path is the canonical per-session log directory of
	// workspace-model.md §4.7 WM-025, keyed on the handler session id.
	wantPath := workspace.SessionLogDirPath(wt, msg.SessionID)
	if msg.LogPath != wantPath {
		t.Errorf("session_log_location.log_path = %q; want the canonical session-log directory %q", msg.LogPath, wantPath)
	}

	var pl core.SessionLogLocationPayload
	if err := json.Unmarshal(hksllRawSessionLogLocation(t, arts), &pl); err != nil {
		t.Fatalf("decode session_log_location as core.SessionLogLocationPayload: %v", err)
	}
	if !pl.Valid() {
		t.Errorf("core.SessionLogLocationPayload.Valid() = false for the payload the daemon publishes: %+v", pl)
	}
}

// TestRoutedPreExec_PiLogPathNamesWhatPiWrites_hkium95 is the hk-ium95
// regression: pi is a SessionIDCaptured harness, so the handler session id
// minted for this pre-exec message never exists on pi's side, and pi never
// writes to workspace.SessionLogDirPath(handlerSessionID) — that directory is
// never created. pi writes under its own PI_CODING_AGENT_DIR
// (<workspace>/.harmonik/pi-agent/) instead. The announced log_path must name
// that directory, not the fictional per-session one.
func TestRoutedPreExec_PiLogPathNamesWhatPiWrites_hkium95(t *testing.T) {
	ctx := context.Background()
	t.Setenv("OPENROUTER_API_KEY", "sk-test-hkium95")
	wt := hksllWorktree(t)
	rc := shared.LaunchCtx{
		RunID:           hksllRunID(t),
		BeadID:          "hkium95-pi",
		WorkspacePath:   wt,
		Phase:           "implementer-initial",
		IterationCount:  1,
		BeadTitle:       "pi log path",
		BeadDescription: "body",
		HandlerBinary:   "pi",
		Provider:        "openrouter",
		Model:           "openrouter/qwen/qwen3-coder",
		APIKeyEnv:       "OPENROUTER_API_KEY",
		BaseURL:         "http://dgx.local:8080/v1",
		API:             "openai",
	}
	h := pi.NewHarness("pi", "openrouter", "openrouter/qwen/qwen3-coder", "OPENROUTER_API_KEY", "", "", "")
	_, arts, err := buildCodexRoutedLaunchSpec(ctx, rc, h, core.AgentTypePi)
	if err != nil {
		t.Fatalf("buildCodexRoutedLaunchSpec (pi): %v", err)
	}

	msg := hksllSessionLogLocation(t, arts)
	wantPath := filepath.Join(wt, ".harmonik", "pi-agent")
	if msg.LogPath != wantPath {
		t.Errorf("session_log_location.log_path = %q; want the directory pi actually writes to %q (hk-ium95)", msg.LogPath, wantPath)
	}
	notWantPath := workspace.SessionLogDirPath(wt, msg.SessionID)
	if msg.LogPath == notWantPath {
		t.Errorf("session_log_location.log_path = %q; this is the canonical per-session directory pi never writes to, because pi is SessionIDCaptured (hk-ium95)", msg.LogPath)
	}
	if _, statErr := os.Stat(wantPath); statErr != nil {
		t.Errorf("announced pi-agent dir %q does not exist on disk: %v", wantPath, statErr)
	}
}
