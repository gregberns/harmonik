package keeper

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// restartnow_test.go — tests for the DEAD-SIMPLE restart-now / ping path
// (hk-5da7). Replaces cycle_restart_now_test.go, which tested the removed
// marker → RunOnDemand state machine.

// recordingInjector captures every (target,text) injection in order so a test
// can assert the exact ack→/clear→/session-resume sequence.
type recordingInjector struct {
	mu    sync.Mutex
	calls [][2]string
	err   error // when non-nil, every Inject returns it (failure simulation)
}

func (r *recordingInjector) inject(_ context.Context, target, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, [2]string{target, text})
	return r.err
}

func (r *recordingInjector) texts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	for i, c := range r.calls {
		out[i] = c[1]
	}
	return out
}

// writeSid writes a primary (UUIDv4) .sid + a matching .ctx so ReadCtxFile
// returns a verified session id.
func writeSidAndCtx(t *testing.T, dir, agent, sid string) {
	t.Helper()
	kdir := filepath.Join(dir, ".harmonik", "keeper")
	if err := os.MkdirAll(kdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kdir, agent+".sid"), []byte(sid+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := `{"pct":50,"session_id":"` + sid + `","ts":"2026-06-19T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(kdir, agent+".ctx"), []byte(ctx), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeFreshHandoff(t *testing.T, dir, agent string, mtime time.Time) string {
	t.Helper()
	p := filepath.Join(dir, "HANDOFF-"+agent+".md")
	if err := os.WriteFile(p, []byte("# handoff\n"), 0o644); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	if !mtime.IsZero() {
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

const goodSID = "11111111-1111-4111-8111-111111111111"

// TestRestartNow_HappyPath asserts the full ack→/clear→/session-resume sequence
// is injected, in order, when sid is verified and the handoff is fresh.
func TestRestartNow_HappyPath(t *testing.T) {
	dir := t.TempDir()
	agent := "captain"
	writeSidAndCtx(t, dir, agent, goodSID)
	requested := time.Now()
	handoffPath := writeFreshHandoff(t, dir, agent, requested.Add(time.Second))

	rec := &recordingInjector{}
	err := RestartNow(context.Background(), RestartNowConfig{
		ProjectDir:  dir,
		AgentName:   agent,
		TmuxTarget:  "sess:0",
		Inject:      rec.inject,
		RequestedAt: requested,
	}, "nonceXYZ")
	if err != nil {
		t.Fatalf("RestartNow: unexpected error: %v", err)
	}
	got := rec.texts()
	want := []string{
		AckLine("nonceXYZ", "restart"),
		"/clear",
		"/session-resume " + handoffPath,
	}
	if len(got) != len(want) {
		t.Fatalf("injected %d lines %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("inject[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRestartNow_NoTmuxTarget_FailsLoudly(t *testing.T) {
	dir := t.TempDir()
	writeSidAndCtx(t, dir, "captain", goodSID)
	writeFreshHandoff(t, dir, "captain", time.Time{})
	err := RestartNow(context.Background(), RestartNowConfig{
		ProjectDir: dir, AgentName: "captain", TmuxTarget: "",
	}, "n")
	if err == nil {
		t.Fatal("want error when no tmux target, got nil")
	}
}

func TestRestartNow_UnverifiedSID_Refuses(t *testing.T) {
	dir := t.TempDir()
	agent := "captain"
	// UUIDv7-shaped (rejected by IsPrimarySID) — daemon-spawned, not interactive.
	writeSidAndCtx(t, dir, agent, "01890000-0000-7000-8000-000000000000")
	writeFreshHandoff(t, dir, agent, time.Time{})
	rec := &recordingInjector{}
	err := RestartNow(context.Background(), RestartNowConfig{
		ProjectDir: dir, AgentName: agent, TmuxTarget: "sess:0", Inject: rec.inject,
	}, "n")
	if err == nil {
		t.Fatal("want error for non-primary sid, got nil")
	}
	if len(rec.calls) != 0 {
		t.Errorf("must NOT inject when sid unverified; got %v", rec.texts())
	}
}

func TestRestartNow_MissingHandoff_Refuses(t *testing.T) {
	dir := t.TempDir()
	agent := "captain"
	writeSidAndCtx(t, dir, agent, goodSID)
	// no handoff written
	rec := &recordingInjector{}
	err := RestartNow(context.Background(), RestartNowConfig{
		ProjectDir: dir, AgentName: agent, TmuxTarget: "sess:0", Inject: rec.inject,
	}, "n")
	if err == nil {
		t.Fatal("want error for missing handoff, got nil")
	}
	if len(rec.calls) != 0 {
		t.Errorf("must NOT /clear with no handoff; got %v", rec.texts())
	}
}

func TestRestartNow_StaleHandoff_Refuses(t *testing.T) {
	dir := t.TempDir()
	agent := "captain"
	writeSidAndCtx(t, dir, agent, goodSID)
	requested := time.Now()
	// handoff mtime BEFORE the request → stale.
	writeFreshHandoff(t, dir, agent, requested.Add(-time.Hour))
	rec := &recordingInjector{}
	err := RestartNow(context.Background(), RestartNowConfig{
		ProjectDir: dir, AgentName: agent, TmuxTarget: "sess:0",
		Inject: rec.inject, RequestedAt: requested,
	}, "n")
	if err == nil {
		t.Fatal("want error for stale handoff, got nil")
	}
	if len(rec.calls) != 0 {
		t.Errorf("must NOT /clear with stale handoff; got %v", rec.texts())
	}
}

// TestPing_InjectsAckOnly asserts ping injects exactly one ack line, no clear/resume.
func TestPing_InjectsAckOnly(t *testing.T) {
	rec := &recordingInjector{}
	err := Ping(context.Background(), RestartNowConfig{
		ProjectDir: t.TempDir(), AgentName: "captain", TmuxTarget: "sess:0", Inject: rec.inject,
	}, "pingnonce")
	if err != nil {
		t.Fatalf("Ping: unexpected error: %v", err)
	}
	got := rec.texts()
	if len(got) != 1 || got[0] != AckLine("pingnonce", "ping") {
		t.Fatalf("ping injected %v, want exactly [%q]", got, AckLine("pingnonce", "ping"))
	}
}

// TestPing_NoTmuxTarget_FailsLoudly guards the no-pane loud failure.
func TestPing_NoTmuxTarget_FailsLoudly(t *testing.T) {
	err := Ping(context.Background(), RestartNowConfig{
		ProjectDir: t.TempDir(), AgentName: "captain", TmuxTarget: "",
	}, "pingnonce")
	if err == nil {
		t.Fatal("want error when no tmux target for ping, got nil")
	}
}

// TestAckLine pins the exact ack wire format the agent-side protocol matches on.
func TestAckLine(t *testing.T) {
	if got := AckLine("abc123", "restart"); got != "[KEEPER ACK abc123] received restart" {
		t.Errorf("AckLine restart = %q", got)
	}
	if got := AckLine("abc123", "ping"); got != "[KEEPER ACK abc123] received ping" {
		t.Errorf("AckLine ping = %q", got)
	}
}
