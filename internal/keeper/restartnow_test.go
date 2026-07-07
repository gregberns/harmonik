package keeper

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// restartnow_test.go — tests for the DEAD-SIMPLE restart-now / ping path
// (hk-5da7). Replaces cycle_restart_now_test.go, which tested the removed
// marker → RunOnDemand state machine.

// recordingInjector captures every (target,text) injection in order so a test
// can assert the exact ack→/clear→agent-brief sequence.
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

// TestRestartNow_HappyPath asserts the full ack→/clear→agent-brief sequence
// is injected, in order, when sid is verified and the handoff is fresh.
func TestRestartNow_HappyPath(t *testing.T) {
	dir := t.TempDir()
	agent := "captain"
	writeSidAndCtx(t, dir, agent, goodSID)
	requested := time.Now()
	writeFreshHandoff(t, dir, agent, requested.Add(time.Second))

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
	if len(got) != 3 {
		t.Fatalf("injected %d lines %v, want 3 (ack+/clear+agent brief)", len(got), got)
	}
	if got[0] != AckLine("nonceXYZ", "restart") {
		t.Errorf("inject[0] = %q, want ack line", got[0])
	}
	if got[1] != "/clear" {
		t.Errorf("inject[1] = %q, want /clear", got[1])
	}
	if !strings.Contains(got[2], "agent brief") {
		t.Errorf("inject[2] = %q, want 'agent brief'", got[2])
	}
	if !strings.Contains(got[2], "keeper-restart") {
		t.Errorf("inject[2] = %q, want '--wake keeper-restart'", got[2])
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

// TestRestartNow_CrewAgent_AccCorpus1_B4 asserts acceptance corpus item #1:
// restart-now does NOT abort no_tmux_target for a crew agent (e.g. admiral)
// whose pane is resolved via the crew convention "harmonik-<hash>-crew-<name>:agent".
//
// Layer: L-fake-tmux (recording injector; no real tmux required).
// Bead: hk-pp1in / B4. Acceptance corpus #1 per 11-keeper-test-design.md §3.
func TestRestartNow_CrewAgent_AccCorpus1_B4(t *testing.T) {
	dir := t.TempDir()
	agent := "admiral"
	writeSidAndCtx(t, dir, agent, goodSID)
	requested := time.Now()
	writeFreshHandoff(t, dir, agent, requested.Add(time.Second))

	// Simulate the crew-session target that ResolveTmuxTarget now returns after
	// the B4 fix: "harmonik-<hash>-crew-admiral:agent".
	crewTarget := HarmonikCrewSessionName(dir, agent) + ":" + windowAgent

	rec := &recordingInjector{}
	err := RestartNow(context.Background(), RestartNowConfig{
		ProjectDir:  dir,
		AgentName:   agent,
		TmuxTarget:  crewTarget,
		Inject:      rec.inject,
		RequestedAt: requested,
	}, "corpus1nonce")
	if err != nil {
		t.Fatalf("AccCorpus1 B4: RestartNow aborted for crew agent: %v", err)
	}
	got := rec.texts()
	if len(got) != 3 {
		t.Fatalf("AccCorpus1 B4: injected %d items, want 3 (ack+/clear+brief): %v", len(got), got)
	}
	if got[0] != AckLine("corpus1nonce", "restart") {
		t.Errorf("AccCorpus1 B4: inject[0] = %q, want ack line", got[0])
	}
	if got[1] != "/clear" {
		t.Errorf("AccCorpus1 B4: inject[1] = %q, want /clear", got[1])
	}
	if !strings.Contains(got[2], "agent brief") {
		t.Errorf("AccCorpus1 B4: inject[2] = %q, want 'agent brief'", got[2])
	}
}

// TestRestartNow_CrewAgent_ResolveThenRun_B4 exercises the full B4 fix path
// end-to-end: ResolveTmuxTarget with a crew-only stub → target non-empty →
// RestartNow drives ACK→/clear→resume (acceptance corpus #1, L-fake-tmux layer).
func TestRestartNow_CrewAgent_ResolveThenRun_B4(t *testing.T) {
	dir := t.TempDir()
	agent := "admiral"
	writeSidAndCtx(t, dir, agent, goodSID)
	requested := time.Now()
	writeFreshHandoff(t, dir, agent, requested.Add(time.Second))

	crewSession := HarmonikCrewSessionName(dir, agent)

	// Stub: only the crew-prefixed session is live.
	sessionExistsFn := func(name string) bool { return name == crewSession }
	target := ResolveTmuxTarget(dir, agent, "", sessionExistsFn)
	if target == "" {
		t.Fatal("B4 resolve: ResolveTmuxTarget returned empty for live crew session (no_tmux_target would fire)")
	}
	wantTarget := crewSession + ":agent"
	if target != wantTarget {
		t.Fatalf("B4 resolve: got %q, want %q", target, wantTarget)
	}

	rec := &recordingInjector{}
	err := RestartNow(context.Background(), RestartNowConfig{
		ProjectDir:  dir,
		AgentName:   agent,
		TmuxTarget:  target,
		Inject:      rec.inject,
		RequestedAt: requested,
	}, "corpus1runnonce")
	if err != nil {
		t.Fatalf("B4 run: RestartNow aborted after crew resolution: %v", err)
	}
	if len(rec.texts()) != 3 {
		t.Fatalf("B4 run: want 3 injections (ack+/clear+brief), got %d: %v", len(rec.texts()), rec.texts())
	}
}
