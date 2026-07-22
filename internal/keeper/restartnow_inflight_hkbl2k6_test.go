package keeper

import (
	"context"
	"strings"
	"testing"
	"time"
)

// restartnow_inflight_hkbl2k6_test.go — restart-now must refuse to /clear over
// in-flight queue work unless explicitly forced.
//
// Covers:
//   - holding dispatch + no --force  → error, and ZERO injections (the agent's
//     context is never touched by a refusal)
//   - holding dispatch + --force     → the full ack → /clear → brief sequence
//   - not holding                    → unaffected (no regression on the happy path)
//   - fail-closed                    → a real unreadable marker path reads as
//     holding, matching HoldingDispatch's own contract
//   - gate ordering                  → a STALE handoff still loses to the
//     freshness check even while dispatch is held, so the refusal reason the
//     operator sees is the most actionable one
//
// Why this matters: the auto cycle has always deferred around in-flight work
// (Gate 5, stepIdleGaugeTick), but restart-now — the operator/captain-driven
// path — consulted NO gate ladder at all. Restarting mid-run cancels the crew's
// in-flight tool work, which is the first link in the hk-bl2k6 orphan chain:
// the killed run's descendants are not in a killable process group, so they are
// reparented to init and survive.
//
// Helper prefix: bl2k6.
//
// Bead ref: hk-bl2k6.

// bl2k6Holding returns a HoldingDispatchFn that always reports the given value
// and records that it was consulted at all — a gate that is never called is a
// gate that does not exist.
func bl2k6Holding(held bool, calls *int) func(string, string) bool {
	return func(_, _ string) bool {
		*calls++
		return held
	}
}

func TestRestartNow_HoldingDispatch_RefusesAndInjectsNothing_hkbl2k6(t *testing.T) {
	dir := t.TempDir()
	agent := "kilo"
	writeSidAndCtx(t, dir, agent, goodSID)
	requested := time.Now()
	writeFreshHandoff(t, dir, agent, requested.Add(time.Second))

	rec := &recordingInjector{}
	calls := 0
	err := RestartNow(context.Background(), RestartNowConfig{
		ProjectDir:        dir,
		AgentName:         agent,
		TmuxTarget:        "sess:0",
		Inject:            rec.inject,
		RequestedAt:       requested,
		HoldingDispatchFn: bl2k6Holding(true, &calls),
	}, "nonce-held")

	if err == nil {
		t.Fatalf("RestartNow: want an error when dispatch is held, got nil (regression: restart-now /clears straight over a live run, cancelling in-flight tool work — the first link in the hk-bl2k6 orphan chain)")
	}
	if !strings.Contains(err.Error(), "in-flight queue work") {
		t.Errorf("error = %q, want it to name the in-flight queue work so the operator knows WHY it refused", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error = %q, want it to name --force so the operator knows the override exists", err)
	}
	if calls != 1 {
		t.Errorf("HoldingDispatchFn consulted %d times, want exactly 1 (regression: the gate is not wired in)", calls)
	}
	if got := rec.texts(); len(got) != 0 {
		t.Errorf("injected %d lines %v, want 0 — a REFUSAL must not touch the pane at all; injecting an ack and then bailing would leave the agent believing a restart is under way", len(got), got)
	}
}

func TestRestartNow_HoldingDispatch_ForceOverrides_hkbl2k6(t *testing.T) {
	dir := t.TempDir()
	agent := "kilo"
	writeSidAndCtx(t, dir, agent, goodSID)
	requested := time.Now()
	writeFreshHandoff(t, dir, agent, requested.Add(time.Second))

	rec := &recordingInjector{}
	calls := 0
	err := RestartNow(context.Background(), RestartNowConfig{
		ProjectDir:        dir,
		AgentName:         agent,
		TmuxTarget:        "sess:0",
		Inject:            rec.inject,
		RequestedAt:       requested,
		Force:             true,
		HoldingDispatchFn: bl2k6Holding(true, &calls),
	}, "nonce-forced")
	if err != nil {
		t.Fatalf("RestartNow with Force: unexpected error: %v", err)
	}
	if calls != 0 {
		t.Errorf("HoldingDispatchFn consulted %d times under Force, want 0 — Force must SKIP the gate, not evaluate and ignore it", calls)
	}
	got := rec.texts()
	if len(got) != 3 {
		t.Fatalf("injected %d lines %v, want 3 (ack + /clear + agent brief) — Force must drive the full sequence", len(got), got)
	}
	if got[1] != "/clear" {
		t.Errorf("inject[1] = %q, want /clear", got[1])
	}
}

func TestRestartNow_NotHolding_ProceedsNormally_hkbl2k6(t *testing.T) {
	dir := t.TempDir()
	agent := "kilo"
	writeSidAndCtx(t, dir, agent, goodSID)
	requested := time.Now()
	writeFreshHandoff(t, dir, agent, requested.Add(time.Second))

	rec := &recordingInjector{}
	calls := 0
	err := RestartNow(context.Background(), RestartNowConfig{
		ProjectDir:        dir,
		AgentName:         agent,
		TmuxTarget:        "sess:0",
		Inject:            rec.inject,
		RequestedAt:       requested,
		HoldingDispatchFn: bl2k6Holding(false, &calls),
	}, "nonce-free")
	if err != nil {
		t.Fatalf("RestartNow: unexpected error with no in-flight work: %v", err)
	}
	if calls != 1 {
		t.Errorf("HoldingDispatchFn consulted %d times, want exactly 1", calls)
	}
	if got := rec.texts(); len(got) != 3 {
		t.Errorf("injected %d lines %v, want 3 — the gate must not disturb the happy path", len(got), got)
	}
}

// The production default must be the REAL HoldingDispatch, not a permissive
// stub: a nil seam that silently defaulted to "not holding" would leave the
// gate looking present while being inert — the failure shape this whole lane
// exists to remove. A clean temp dir has no marker, so the real function
// reports false and the restart proceeds.
func TestRestartNow_NilSeam_UsesRealMarker_hkbl2k6(t *testing.T) {
	dir := t.TempDir()
	agent := "kilo"
	writeSidAndCtx(t, dir, agent, goodSID)
	requested := time.Now()
	writeFreshHandoff(t, dir, agent, requested.Add(time.Second))

	rec := &recordingInjector{}
	if err := RestartNow(context.Background(), RestartNowConfig{
		ProjectDir:  dir,
		AgentName:   agent,
		TmuxTarget:  "sess:0",
		Inject:      rec.inject,
		RequestedAt: requested,
	}, "nonce-nil-seam"); err != nil {
		t.Fatalf("RestartNow with nil seam and no marker: unexpected error: %v", err)
	}
	if got := rec.texts(); len(got) != 3 {
		t.Fatalf("injected %d lines, want 3", len(got))
	}

	// Now set the marker through the production writer and assert the same
	// nil-seam call refuses. This pins that the default really is the marker.
	if err := SetDispatching(dir, agent); err != nil {
		t.Fatalf("SetDispatching: %v", err)
	}
	rec2 := &recordingInjector{}
	err := RestartNow(context.Background(), RestartNowConfig{
		ProjectDir:  dir,
		AgentName:   agent,
		TmuxTarget:  "sess:0",
		Inject:      rec2.inject,
		RequestedAt: requested,
	}, "nonce-nil-seam-held")
	if err == nil {
		t.Fatalf("RestartNow: want refusal with a real .dispatching marker present and a nil seam, got nil (regression: the nil default is not wired to HoldingDispatch)")
	}
	if got := rec2.texts(); len(got) != 0 {
		t.Errorf("injected %d lines %v, want 0 on refusal", len(got), got)
	}
}

// Gate ordering: the freshness check runs BEFORE the dispatch gate, so a stale
// handoff is reported as stale even when dispatch is also held. Both are
// refusals, but the operator should be told the one they must act on first —
// writing a fresh handoff — rather than being sent to wait on a queue.
func TestRestartNow_StaleHandoffBeatsDispatchGate_hkbl2k6(t *testing.T) {
	dir := t.TempDir()
	agent := "kilo"
	writeSidAndCtx(t, dir, agent, goodSID)
	requested := time.Now()
	// A handoff written well outside the freshness window.
	writeFreshHandoff(t, dir, agent, requested.Add(-2*HandoffFreshnessWindow))

	rec := &recordingInjector{}
	calls := 0
	err := RestartNow(context.Background(), RestartNowConfig{
		ProjectDir:        dir,
		AgentName:         agent,
		TmuxTarget:        "sess:0",
		Inject:            rec.inject,
		RequestedAt:       requested,
		HoldingDispatchFn: bl2k6Holding(true, &calls),
	}, "nonce-stale-and-held")
	if err == nil {
		t.Fatalf("RestartNow: want an error for a stale handoff, got nil")
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Errorf("error = %q, want the STALE-handoff reason to win — it is the one the operator must act on", err)
	}
	if calls != 0 {
		t.Errorf("HoldingDispatchFn consulted %d times, want 0 — the freshness check must short-circuit first", calls)
	}
}
