package digest

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
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

// TestSentinelConfig_Phase2Classes verifies that Phase2Classes returns only
// class names whose done_definition is not "merged" (flywheel-motion.md §5.3).
func TestSentinelConfig_Phase2Classes(t *testing.T) {
	t.Parallel()

	yaml := []byte(`
sentinel:
  done_definition:
    default: merged
    deploy-class: make deploy && make smoke
    verify-class: ./scripts/verify.sh
`)
	cfg, err := parseSentinelConfig(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	classes := cfg.Phase2Classes()
	if len(classes) != 2 {
		t.Fatalf("Phase2Classes: got %d classes, want 2; classes=%v", len(classes), classes)
	}
	classSet := make(map[string]struct{}, len(classes))
	for _, c := range classes {
		classSet[c] = struct{}{}
	}
	for _, want := range []string{"deploy-class", "verify-class"} {
		if _, ok := classSet[want]; !ok {
			t.Errorf("Phase2Classes missing %q; got %v", want, classes)
		}
	}
	if _, ok := classSet["default"]; ok {
		t.Errorf("Phase2Classes must not include 'default' (value is 'merged')")
	}
}

// TestSentinelConfig_Phase2Classes_Empty verifies that Phase2Classes returns
// nil when all done_definitions are "merged" or not configured.
func TestSentinelConfig_Phase2Classes_Empty(t *testing.T) {
	t.Parallel()

	// No done_definition configured.
	cfg, err := parseSentinelConfig([]byte(`sentinel: {}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.Phase2Classes(); len(got) != 0 {
		t.Errorf("Phase2Classes with no config: got %v, want empty", got)
	}

	// All classes are "merged".
	cfg2, err := parseSentinelConfig([]byte(`
sentinel:
  done_definition:
    default: merged
    another: merged
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg2.Phase2Classes(); len(got) != 0 {
		t.Errorf("Phase2Classes all-merged: got %v, want empty", got)
	}
}

// TestSentinelConfig_GovernorConfig verifies that GovernorConfig bridges the
// map[string]int → map[core.EventType]int type-mismatch and copies all
// governor fields into the returned sentinel.Config (FW1, hk-y9fn).
func TestSentinelConfig_GovernorConfig(t *testing.T) {
	t.Parallel()

	yaml := []byte(`
sentinel:
  window: 15m
  warmup_window: 20m
  sustained_windows: 3
  movement_weights:
    bead_closed: 5
    run_completed: 7
  liveness_no_progress_n: 4
`)
	cfg, err := parseSentinelConfig(yaml)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	gcfg := cfg.GovernorConfig()

	if gcfg.Window != 15*time.Minute {
		t.Errorf("Window: got %v, want 15m", gcfg.Window)
	}
	if gcfg.WarmupWindow != 20*time.Minute {
		t.Errorf("WarmupWindow: got %v, want 20m", gcfg.WarmupWindow)
	}
	if gcfg.SustainedWindows != 3 {
		t.Errorf("SustainedWindows: got %d, want 3", gcfg.SustainedWindows)
	}
	if gcfg.LivenessNoProgressN != 4 {
		t.Errorf("LivenessNoProgressN: got %d, want 4", gcfg.LivenessNoProgressN)
	}

	if gcfg.Weights == nil {
		t.Fatal("Weights: got nil, want non-nil map")
	}
	if got := gcfg.Weights[core.EventTypeBeadClosed]; got != 5 {
		t.Errorf("Weights[bead_closed]: got %d, want 5", got)
	}
	if got := gcfg.Weights[core.EventTypeRunCompleted]; got != 7 {
		t.Errorf("Weights[run_completed]: got %d, want 7", got)
	}
}

// TestSentinelConfig_GovernorConfig_NilWeights verifies that a zero
// movement_weights in the YAML produces a nil Weights field so sentinel.Evaluate
// falls back to sentinel.DefaultWeights.
func TestSentinelConfig_GovernorConfig_NilWeights(t *testing.T) {
	t.Parallel()

	cfg, err := parseSentinelConfig([]byte(`sentinel: {}`))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	gcfg := cfg.GovernorConfig()
	if gcfg.Weights != nil {
		t.Errorf("Weights: expected nil for empty config, got %v", gcfg.Weights)
	}
}
