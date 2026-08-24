package keeper

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

const restartTestSID = "11111111-1111-4111-8111-111111111111"

func writeRestartFixture(t *testing.T, dir, agent, sid, handoff string) {
	t.Helper()
	keeperDir := filepath.Join(dir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := `{"pct":50,"session_id":"` + sid + `","ts":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	if err := os.WriteFile(filepath.Join(keeperDir, agent+".ctx"), []byte(ctx+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keeperDir, agent+".sid"), []byte(sid+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if handoff != "" {
		if err := os.WriteFile(filepath.Join(dir, "HANDOFF-"+agent+".md"), []byte(handoff), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidateRestartNow_AcceptsNonEmptyHandoffWithoutAgeLimit(t *testing.T) {
	dir := t.TempDir()
	writeRestartFixture(t, dir, "captain", restartTestSID, "# durable handoff\n")
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "HANDOFF-captain.md"), old, old); err != nil {
		t.Fatal(err)
	}
	sid, err := ValidateRestartNow(t.Context(), RestartNowConfig{
		ProjectDir: dir, AgentName: "captain", TmuxTarget: "pane",
		LiveKeeperPresentFn: func(string, string) bool { return true },
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if sid != restartTestSID {
		t.Fatalf("session id = %q, want %q", sid, restartTestSID)
	}
}

func TestValidateRestartNow_RejectsUnsafeInputsBeforeEffects(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		sid     string
		handoff string
		want    string
	}{
		{name: "no target", sid: restartTestSID, handoff: "handoff", want: "no tmux target"},
		{name: "untrusted session", target: "pane", sid: "not-a-primary-session", handoff: "handoff", want: "not a trusted primary"},
		{name: "missing handoff", target: "pane", sid: restartTestSID, want: "missing"},
		{name: "empty handoff", target: "pane", sid: restartTestSID, handoff: " \n", want: "empty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeRestartFixture(t, dir, "captain", tc.sid, tc.handoff)
			_, err := ValidateRestartNow(t.Context(), RestartNowConfig{
				ProjectDir: dir, AgentName: "captain", TmuxTarget: tc.target,
				LiveKeeperPresentFn: func(string, string) bool { return true },
			}, slog.Default())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want text %q", err, tc.want)
			}
		})
	}
}

func TestValidateRestartNow_DispatchGateCanBeExplicitlyForced(t *testing.T) {
	dir := t.TempDir()
	writeRestartFixture(t, dir, "captain", restartTestSID, "handoff")
	cfg := RestartNowConfig{
		ProjectDir: dir, AgentName: "captain", TmuxTarget: "pane",
		HoldingDispatchFn:   func(string, string) bool { return true },
		LiveKeeperPresentFn: func(string, string) bool { return true },
	}
	if _, err := ValidateRestartNow(t.Context(), cfg, slog.Default()); err == nil {
		t.Fatal("dispatch gate accepted without force")
	}
	cfg.Force = true
	if _, err := ValidateRestartNow(t.Context(), cfg, slog.Default()); err != nil {
		t.Fatalf("explicit force rejected: %v", err)
	}
}

func TestValidateRestartNow_RequiresLiveKeeperOwner(t *testing.T) {
	dir := t.TempDir()
	writeRestartFixture(t, dir, "captain", restartTestSID, "handoff")
	_, err := ValidateRestartNow(t.Context(), RestartNowConfig{
		ProjectDir: dir, AgentName: "captain", TmuxTarget: "pane",
		LiveKeeperPresentFn: func(string, string) bool { return false },
	}, slog.Default())
	if err == nil || !strings.Contains(err.Error(), "no live keeper") {
		t.Fatalf("error = %v, want no live keeper", err)
	}
}

func TestEmitRestartNowAccepted_RecordsValidatedSessionAndNonce(t *testing.T) {
	emitter := &RecordingEmitter{}
	if err := EmitRestartNowAccepted(t.Context(), emitter, "captain", restartTestSID, "nonce-1"); err != nil {
		t.Fatal(err)
	}
	events := emitter.EventsOfType(core.EventTypeSessionKeeperRestartNow)
	if len(events) != 1 {
		t.Fatalf("accepted events = %d, want 1", len(events))
	}
	var payload core.SessionKeeperRestartNowPayload
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.AgentName != "captain" || payload.SessionID != restartTestSID || payload.Nonce != "nonce-1" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestDriveRestartAfterReturn_ClearsOnceResetsInputAndBriefsAfterNewSession(t *testing.T) {
	dir := t.TempDir()
	writeRestartFixture(t, dir, "captain", restartTestSID, "handoff")
	const newSID = "22222222-2222-4222-8222-222222222222"
	var effects []string
	inject := func(_ context.Context, _, text string) error {
		effects = append(effects, "inject:"+text)
		if text == "/clear" {
			return os.WriteFile(filepath.Join(dir, ".harmonik", "keeper", "captain.sid"), []byte(newSID+"\n"), 0o600)
		}
		return nil
	}
	err := DriveRestartAfterReturn(t.Context(), RestartDriveConfig{
		RestartNowConfig: RestartNowConfig{
			ProjectDir: dir, AgentName: "captain", TmuxTarget: "pane", Inject: inject,
		},
		PreviousSessionID: restartTestSID,
		Grace:             time.Nanosecond,
		Timeout:           time.Second,
		Poll:              time.Millisecond,
		ResetPendingInput: func(context.Context, string) error {
			effects = append(effects, "reset")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(effects) != 3 || effects[0] != "inject:/clear" || effects[1] != "reset" || !strings.Contains(effects[2], "agent brief") {
		t.Fatalf("effects = %v, want clear, reset, brief", effects)
	}
}

func TestDriveRestartAfterReturn_NoTurnoverNeverRetriesClearOrBriefs(t *testing.T) {
	dir := t.TempDir()
	writeRestartFixture(t, dir, "captain", restartTestSID, "handoff")
	var calls []string
	err := DriveRestartAfterReturn(t.Context(), RestartDriveConfig{
		RestartNowConfig: RestartNowConfig{
			ProjectDir: dir, AgentName: "captain", TmuxTarget: "pane",
			Inject: func(_ context.Context, _, text string) error { calls = append(calls, text); return nil },
		},
		PreviousSessionID: restartTestSID,
		Grace:             time.Nanosecond,
		Timeout:           5 * time.Millisecond,
		Poll:              time.Millisecond,
	})
	if err == nil {
		t.Fatal("missing session turnover was reported as success")
	}
	if len(calls) != 1 || calls[0] != "/clear" {
		t.Fatalf("calls = %v, want exactly one clear and no brief", calls)
	}
}

func TestDriveRestartAfterReturn_TurnoverDuringGraceSkipsClearAndReset(t *testing.T) {
	dir := t.TempDir()
	writeRestartFixture(t, dir, "captain", restartTestSID, "handoff")
	const newSID = "22222222-2222-4222-8222-222222222222"
	go func() {
		time.Sleep(5 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(dir, ".harmonik", "keeper", "captain.sid"), []byte(newSID+"\n"), 0o600)
	}()
	var effects []string
	err := DriveRestartAfterReturn(t.Context(), RestartDriveConfig{
		RestartNowConfig: RestartNowConfig{
			ProjectDir: dir, AgentName: "captain", TmuxTarget: "pane",
			Inject: func(_ context.Context, _, text string) error { effects = append(effects, "inject:"+text); return nil },
		},
		PreviousSessionID: restartTestSID,
		Grace:             30 * time.Millisecond,
		Timeout:           time.Second,
		Poll:              time.Millisecond,
		ResetPendingInput: func(context.Context, string) error { effects = append(effects, "reset"); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(effects) != 1 || !strings.Contains(effects[0], "agent brief") {
		t.Fatalf("effects = %v, want only the brief after external turnover", effects)
	}
}

func TestPing_InjectsAckOnly(t *testing.T) {
	var calls []string
	err := Ping(t.Context(), RestartNowConfig{
		AgentName: "captain", TmuxTarget: "pane",
		Inject: func(_ context.Context, _, text string) error { calls = append(calls, text); return nil },
	}, "nonce")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != AckLine("nonce", "ping") {
		t.Fatalf("calls = %v", calls)
	}
}

func TestAckLine(t *testing.T) {
	if got := AckLine("abc", "restart"); !strings.Contains(got, "abc") || !strings.Contains(got, "restart") {
		t.Fatalf("ack = %q", got)
	}
}
