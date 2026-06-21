package digest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// writeResolverEvent appends a minimal event envelope to eventsPath.
func writeResolverEvent(t *testing.T, eventsPath string, evType string, payload interface{}, ts time.Time) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("writeResolverEvent: marshal payload: %v", err)
	}
	uid, _ := uuid.NewV7()
	ev := map[string]interface{}{
		"event_id":         uid.String(),
		"schema_version":   1,
		"type":             evType,
		"timestamp_wall":   ts.UTC().Format(time.RFC3339Nano),
		"source_subsystem": "test",
		"payload":          json.RawMessage(raw),
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("writeResolverEvent: marshal event: %v", err)
	}
	f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("writeResolverEvent: open: %v", err)
	}
	defer f.Close()
	fmt.Fprintf(f, "%s\n", b)
}

// TestResolveSuppressionState_Default verifies that with no events and no config,
// the resolver returns Suppressed=false (EXECUTE-BACKLOG default, spec §3.1).
func TestResolveSuppressionState_Default(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")

	state := ResolveSuppressionState(eventsPath, time.Now(), SentinelConfig{})
	if state.Suppressed {
		t.Errorf("expected Suppressed=false with no events; got true")
	}
	if len(state.Sources) != 3 {
		t.Errorf("expected 3 sources; got %d", len(state.Sources))
	}
}

// TestResolveSuppressionState_OperatorAttachedActive verifies that a recent
// session_keeper_operator_attached event within the TTL activates suppression.
func TestResolveSuppressionState_OperatorAttachedActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	now := time.Now()

	// Write a session_keeper_operator_attached event 1 minute ago (within 5m default TTL).
	writeResolverEvent(t, eventsPath, string(core.EventTypeSessionKeeperOperatorAttached),
		core.SessionKeeperOperatorAttachedPayload{AgentName: "captain", Phase: "cycle"},
		now.Add(-1*time.Minute))

	cfg := SentinelConfig{
		SuppressionTTL:          10 * time.Minute,
		AttachedInactiveTimeout: 5 * time.Minute,
	}
	state := ResolveSuppressionState(eventsPath, now, cfg)
	if !state.Suppressed {
		t.Errorf("expected Suppressed=true; got false")
	}
	attachedSrc := findSource(state.Sources, "operator_attached")
	if attachedSrc == nil {
		t.Fatal("missing operator_attached source")
	}
	if !attachedSrc.Active {
		t.Errorf("expected operator_attached.Active=true; got false")
	}
}

// TestResolveSuppressionState_OperatorAttachedExpired verifies that a
// session_keeper_operator_attached event older than attached_inactive_timeout
// does NOT suppress (attached-but-inactive guard, spec §3.2).
func TestResolveSuppressionState_OperatorAttachedExpired(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	now := time.Now()

	// Write an event 10 minutes ago — outside the 5m attached_inactive_timeout.
	writeResolverEvent(t, eventsPath, string(core.EventTypeSessionKeeperOperatorAttached),
		core.SessionKeeperOperatorAttachedPayload{AgentName: "captain", Phase: "cycle"},
		now.Add(-10*time.Minute))

	cfg := SentinelConfig{
		SuppressionTTL:          20 * time.Minute, // outer TTL is wide
		AttachedInactiveTimeout: 5 * time.Minute,  // but inner timeout is 5m
	}
	state := ResolveSuppressionState(eventsPath, now, cfg)
	if state.Suppressed {
		t.Errorf("expected Suppressed=false (attached_inactive_timeout expired); got true")
	}
	attachedSrc := findSource(state.Sources, "operator_attached")
	if attachedSrc == nil {
		t.Fatal("missing operator_attached source")
	}
	if attachedSrc.Active {
		t.Errorf("expected operator_attached.Active=false (expired); got true")
	}
}

// TestResolveSuppressionState_OperatorDialogueActive verifies that a recent
// agent_message from "operator" suppresses within suppression_ttl.
func TestResolveSuppressionState_OperatorDialogueActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	now := time.Now()

	// Write an agent_message from "operator" 2 minutes ago (within 10m TTL).
	writeResolverEvent(t, eventsPath, eventTypeAgentMessage,
		map[string]string{"from": "operator", "to": "captain", "body": "hold"},
		now.Add(-2*time.Minute))

	cfg := SentinelConfig{SuppressionTTL: 10 * time.Minute}
	state := ResolveSuppressionState(eventsPath, now, cfg)
	if !state.Suppressed {
		t.Errorf("expected Suppressed=true; got false")
	}
	src := findSource(state.Sources, "operator_dialogue")
	if src == nil {
		t.Fatal("missing operator_dialogue source")
	}
	if !src.Active {
		t.Errorf("expected operator_dialogue.Active=true; got false")
	}
}

// TestResolveSuppressionState_OperatorDialogueNotOperator verifies that
// agent_message events from other agents don't suppress.
func TestResolveSuppressionState_OperatorDialogueNotOperator(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	now := time.Now()

	// Message from "captain", not "operator".
	writeResolverEvent(t, eventsPath, eventTypeAgentMessage,
		map[string]string{"from": "captain", "to": "crew1", "body": "status"},
		now.Add(-1*time.Minute))

	state := ResolveSuppressionState(eventsPath, now, SentinelConfig{})
	if state.Suppressed {
		t.Errorf("expected Suppressed=false (not from operator); got true")
	}
	src := findSource(state.Sources, "operator_dialogue")
	if src == nil {
		t.Fatal("missing operator_dialogue source")
	}
	if src.Active {
		t.Errorf("expected operator_dialogue.Active=false; got true")
	}
}

// TestResolveSuppressionState_PhaseFlagActive verifies that a phase_flag with a
// future expiry suppresses dispatch.
func TestResolveSuppressionState_PhaseFlagActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	now := time.Now()

	cfg := SentinelConfig{
		PhaseFlag:       "design",
		PhaseFlagExpiry: now.Add(2 * time.Hour),
	}
	state := ResolveSuppressionState(eventsPath, now, cfg)
	if !state.Suppressed {
		t.Errorf("expected Suppressed=true (phase_flag active); got false")
	}
	src := findSource(state.Sources, "phase_flag")
	if src == nil {
		t.Fatal("missing phase_flag source")
	}
	if !src.Active {
		t.Errorf("expected phase_flag.Active=true; got false")
	}
}

// TestResolveSuppressionState_PhaseFlagExpired verifies that an expired phase_flag
// does not suppress.
func TestResolveSuppressionState_PhaseFlagExpired(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	now := time.Now()

	cfg := SentinelConfig{
		PhaseFlag:       "design",
		PhaseFlagExpiry: now.Add(-1 * time.Hour), // already expired
	}
	state := ResolveSuppressionState(eventsPath, now, cfg)
	if state.Suppressed {
		t.Errorf("expected Suppressed=false (phase_flag expired); got true")
	}
}

// TestResolveSuppressionState_PhaseFlagMissingExpiry verifies that a phase_flag
// without an expiry is treated as inactive (fail-open) and sets ConfigError.
func TestResolveSuppressionState_PhaseFlagMissingExpiry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	now := time.Now()

	// PhaseFlag set but PhaseFlagExpiry is zero — invalid config.
	cfg := SentinelConfig{PhaseFlag: "design"}
	state := ResolveSuppressionState(eventsPath, now, cfg)
	if state.Suppressed {
		t.Errorf("expected Suppressed=false (invalid config, fail-open); got true")
	}
	if state.ConfigError == "" {
		t.Errorf("expected ConfigError for phase_flag without expiry; got empty")
	}
}

// TestLoadSentinelConfig_Absent verifies that a missing config.yaml returns
// zero-value SentinelConfig with no error.
func TestLoadSentinelConfig_Absent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755) //nolint:errcheck
	cfg, err := LoadSentinelConfig(dir)
	if err != nil {
		t.Fatalf("expected nil error; got %v", err)
	}
	if cfg.PhaseFlag != "" || cfg.SuppressionTTL != 0 {
		t.Errorf("expected zero-value SentinelConfig; got %+v", cfg)
	}
}

// TestLoadSentinelConfig_ValidBlock verifies that a valid sentinel: block is parsed correctly.
func TestLoadSentinelConfig_ValidBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755) //nolint:errcheck
	yaml := `schema_version: 1
sentinel:
  suppression_ttl: 15m
  attached_inactive_timeout: 8m
  phase_flag: design
  phase_flag_expiry: "2030-01-01T00:00:00Z"
`
	if err := os.WriteFile(filepath.Join(dir, ".harmonik", "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	cfg, err := LoadSentinelConfig(dir)
	if err != nil {
		t.Fatalf("LoadSentinelConfig: %v", err)
	}
	if cfg.SuppressionTTL != 15*time.Minute {
		t.Errorf("suppression_ttl: got %v, want 15m", cfg.SuppressionTTL)
	}
	if cfg.AttachedInactiveTimeout != 8*time.Minute {
		t.Errorf("attached_inactive_timeout: got %v, want 8m", cfg.AttachedInactiveTimeout)
	}
	if cfg.PhaseFlag != "design" {
		t.Errorf("phase_flag: got %q, want design", cfg.PhaseFlag)
	}
	if cfg.PhaseFlagExpiry.Year() != 2030 {
		t.Errorf("phase_flag_expiry: got %v", cfg.PhaseFlagExpiry)
	}
}

// TestLoadSentinelConfig_PhaseFlagMissingExpiry verifies that phase_flag without
// phase_flag_expiry returns ErrPhaseFlagMissingExpiry.
func TestLoadSentinelConfig_PhaseFlagMissingExpiry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755) //nolint:errcheck
	yaml := `schema_version: 1
sentinel:
  phase_flag: design
`
	if err := os.WriteFile(filepath.Join(dir, ".harmonik", "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	_, err := LoadSentinelConfig(dir)
	if err == nil {
		t.Fatal("expected error for phase_flag without expiry; got nil")
	}
	var pfe *ErrPhaseFlagMissingExpiry
	if !func() bool {
		e, ok := err.(*ErrPhaseFlagMissingExpiry)
		pfe = e
		return ok
	}() {
		t.Errorf("expected *ErrPhaseFlagMissingExpiry; got %T: %v", err, err)
	}
	_ = pfe
}

// findSource returns the SuppressionSourceState with the given name, or nil.
func findSource(sources []SuppressionSourceState, name string) *SuppressionSourceState {
	for i := range sources {
		if sources[i].Name == name {
			return &sources[i]
		}
	}
	return nil
}

// --- BT1 unit-gap tests (hk-tbg8) ---

// TestResolveSuppressionState_OperatorDialogueExpired verifies that an operator
// dialogue event (agent_message from "operator") older than suppression_ttl does
// NOT suppress dispatch. The suppression decays after the TTL elapses.
// Spec: B3 "dialogue-recency decay (expired)".
func TestResolveSuppressionState_OperatorDialogueExpired(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	now := time.Now()

	// Write a dialogue event 12 minutes ago — beyond the 10m suppression_ttl.
	writeResolverEvent(t, eventsPath, eventTypeAgentMessage,
		map[string]string{"from": "operator", "to": "captain", "body": "continue"},
		now.Add(-12*time.Minute))

	cfg := SentinelConfig{SuppressionTTL: 10 * time.Minute}
	state := ResolveSuppressionState(eventsPath, now, cfg)

	if state.Suppressed {
		t.Errorf("expected Suppressed=false (dialogue TTL expired); got true")
	}
	src := findSource(state.Sources, "operator_dialogue")
	if src == nil {
		t.Fatal("missing operator_dialogue source")
	}
	if src.Active {
		t.Errorf("expected operator_dialogue.Active=false (TTL=%v elapsed); got true", cfg.SuppressionTTL)
	}
	if src.LastSeen.IsZero() {
		t.Error("expected LastSeen to be non-zero (event was seen)")
	}
	if src.ExpiresAt.IsZero() {
		t.Error("expected ExpiresAt to be non-zero for a seen event")
	}
}

// TestResolveSuppressionState_IssueClearing_NotAMode verifies that bead_closed
// events — representing issue-clearing progress — do NOT activate any suppression
// source. Issue-clearing is NOT a suppression mode; it is regular terminal
// progress that keeps the movement governor dormant.
//
// Spec §3.3: "Issue-clearing is NOT a mode: progressing issue-clears emit
// bead_closed/HEAD-advances that the movement governor credits, keeping it
// dormant without any suppression. A stalled clear correctly trips."
// Spec: B3 "issue-clearing-is-NOT-a-mode negative".
func TestResolveSuppressionState_IssueClearing_NotAMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	now := time.Now()

	// Write several bead_closed events (active issue-clearing progress).
	for i := 0; i < 5; i++ {
		writeResolverEvent(t, eventsPath, string(core.EventTypeBeadClosed),
			map[string]interface{}{"bead_id": fmt.Sprintf("hk-x%04d", i)},
			now.Add(-time.Duration(i)*time.Minute))
	}

	state := ResolveSuppressionState(eventsPath, now, SentinelConfig{})

	if state.Suppressed {
		t.Error("bead_closed events (issue-clearing) must NOT suppress the sentinel (spec §3.3)")
	}
	for _, src := range state.Sources {
		if src.Active {
			t.Errorf("source %q must not be active from bead_closed events; got Active=true reason=%q",
				src.Name, src.Reason)
		}
	}
}
