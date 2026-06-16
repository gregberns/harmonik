package digest

import (
	"testing"
	"time"
)

// TestParseSentinelConfig_GovernorFields verifies that the governor tunables
// (window, warmup_window, sustained_windows, movement_weights,
// liveness_no_progress_n, done_definition) are parsed from YAML and accessible
// via the SentinelConfig accessor methods (flywheel-motion.md §7, bead hk-w0rm).
func TestParseSentinelConfig_GovernorFields(t *testing.T) {
	t.Parallel()

	yaml := []byte(`
sentinel:
  window: 15m
  warmup_window: 20m
  sustained_windows: 3
  movement_weights:
    bead_closed: 5
    run_completed: 5
    reviewer_verdict: 5
  liveness_no_progress_n: 7
  done_definition:
    default: merged
    deploy-class: make deploy && make smoke
`)

	cfg, err := parseSentinelConfig(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := cfg.GovernorWindow(); got != 15*time.Minute {
		t.Errorf("GovernorWindow: got %v, want 15m", got)
	}
	if got := cfg.GovernorWarmupWindow(); got != 20*time.Minute {
		t.Errorf("GovernorWarmupWindow: got %v, want 20m", got)
	}
	if got := cfg.GovernorSustainedWindows(); got != 3 {
		t.Errorf("GovernorSustainedWindows: got %d, want 3", got)
	}

	weights := cfg.GovernorMovementWeights()
	if weights == nil {
		t.Fatal("GovernorMovementWeights: got nil, want non-nil")
	}
	for _, k := range []string{"bead_closed", "run_completed", "reviewer_verdict"} {
		if weights[k] != 5 {
			t.Errorf("MovementWeights[%q]: got %d, want 5", k, weights[k])
		}
	}

	if got := cfg.GovernorLivenessNoProgressN(); got != 7 {
		t.Errorf("GovernorLivenessNoProgressN: got %d, want 7", got)
	}

	if got := cfg.DoneDefinitionFor("default"); got != "merged" {
		t.Errorf("DoneDefinitionFor(default): got %q, want merged", got)
	}
	if got := cfg.DoneDefinitionFor("deploy-class"); got != "make deploy && make smoke" {
		t.Errorf("DoneDefinitionFor(deploy-class): got %q, want command", got)
	}
}

// TestParseSentinelConfig_GovernorDefaults verifies that zero/missing governor
// fields fall back to the compiled defaults.
func TestParseSentinelConfig_GovernorDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := parseSentinelConfig([]byte(`sentinel: {}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := cfg.GovernorWindow(); got != DefaultGovernorWindow {
		t.Errorf("GovernorWindow default: got %v, want %v", got, DefaultGovernorWindow)
	}
	if got := cfg.GovernorWarmupWindow(); got != DefaultGovernorWarmupWindow {
		t.Errorf("GovernorWarmupWindow default: got %v, want %v", got, DefaultGovernorWarmupWindow)
	}
	if got := cfg.GovernorSustainedWindows(); got != DefaultGovernorSustainedWindows {
		t.Errorf("GovernorSustainedWindows default: got %d, want %d", got, DefaultGovernorSustainedWindows)
	}
	if weights := cfg.GovernorMovementWeights(); weights != nil {
		t.Errorf("GovernorMovementWeights default: expected nil (use compiled defaults), got %v", weights)
	}
	if got := cfg.GovernorLivenessNoProgressN(); got != DefaultLivenessNoProgressN {
		t.Errorf("GovernorLivenessNoProgressN default: got %d, want %d", got, DefaultLivenessNoProgressN)
	}
	if got := cfg.DoneDefinitionFor("any-class"); got != DefaultDoneDefinition {
		t.Errorf("DoneDefinitionFor default: got %q, want %q", got, DefaultDoneDefinition)
	}
}

// TestParseSentinelConfig_InvalidWindow verifies that a malformed window
// duration returns a descriptive error.
func TestParseSentinelConfig_InvalidWindow(t *testing.T) {
	t.Parallel()

	_, err := parseSentinelConfig([]byte("sentinel:\n  window: not-a-duration\n"))
	if err == nil {
		t.Error("expected error for invalid window duration, got nil")
	}
}
