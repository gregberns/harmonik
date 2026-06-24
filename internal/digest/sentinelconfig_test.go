package digest

import (
	"errors"
	"strings"
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

	gcfg, gerr := cfg.GovernorConfig()
	if gerr != nil {
		t.Fatalf("unexpected GovernorConfig error: %v", gerr)
	}
	if got := gcfg.LivenessNoProgressN; got != 7 {
		t.Errorf("LivenessNoProgressN: got %d, want 7", got)
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
	// liveness_no_progress_n has NO compiled default under FIX-B (hk-drygf):
	// an absent key must fail loud at GovernorConfig(), not fall back to a literal.
	if _, gerr := cfg.GovernorConfig(); gerr == nil {
		t.Error("GovernorConfig with no liveness_no_progress_n: expected fail-loud error, got nil")
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

	gcfg, gerr := cfg.GovernorConfig()
	if gerr != nil {
		t.Fatalf("unexpected GovernorConfig error: %v", gerr)
	}

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

	// liveness_no_progress_n must be present (any explicit value) for GovernorConfig
	// to succeed under FIX-B; an explicit 0 is the minimal valid form here.
	cfg, err := parseSentinelConfig([]byte("sentinel:\n  liveness_no_progress_n: 0\n"))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	gcfg, gerr := cfg.GovernorConfig()
	if gerr != nil {
		t.Fatalf("unexpected GovernorConfig error: %v", gerr)
	}
	if gcfg.Weights != nil {
		t.Errorf("Weights: expected nil for empty config, got %v", gcfg.Weights)
	}
}

// TestSentinelConfig_GovernorConfig_MissingLivenessN_FailsLoud verifies the
// FIX-B contract (hk-drygf): when liveness_no_progress_n is UNSET on the
// production parse path (as the live config.yaml had it commented out), the
// governor-config resolution fails loud with an error that NAMES the missing
// key, so the daemon cannot silently run with the G-liveness gate disabled.
func TestSentinelConfig_GovernorConfig_MissingLivenessN_FailsLoud(t *testing.T) {
	t.Parallel()

	// A sentinel block WITHOUT liveness_no_progress_n (mirrors the live config:
	// the key commented out, other keys like mode present).
	cfg, err := parseSentinelConfig([]byte("sentinel:\n  mode: observe\n"))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	_, gerr := cfg.GovernorConfig()
	if gerr == nil {
		t.Fatal("expected fail-loud error for missing liveness_no_progress_n, got nil")
	}
	var target *ErrMissingLivenessNoProgressN
	if !errors.As(gerr, &target) {
		t.Errorf("expected *ErrMissingLivenessNoProgressN, got %T: %v", gerr, gerr)
	}
	if !strings.Contains(gerr.Error(), "liveness_no_progress_n") {
		t.Errorf("error message must name the key liveness_no_progress_n, got: %q", gerr.Error())
	}
}

// TestSentinelConfig_GovernorConfig_ExplicitZeroHonored verifies that an
// explicit liveness_no_progress_n: 0 is honored as a valid operator choice
// (the gate is explicitly disabled) — NOT treated as "absent". GovernorConfig
// returns no error and carries the 0 through verbatim. Guards against
// over-failing on the explicit-disable case.
func TestSentinelConfig_GovernorConfig_ExplicitZeroHonored(t *testing.T) {
	t.Parallel()

	cfg, err := parseSentinelConfig([]byte("sentinel:\n  liveness_no_progress_n: 0\n"))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	gcfg, gerr := cfg.GovernorConfig()
	if gerr != nil {
		t.Fatalf("explicit liveness_no_progress_n: 0 must be honored, got error: %v", gerr)
	}
	if gcfg.LivenessNoProgressN != 0 {
		t.Errorf("LivenessNoProgressN: got %d, want 0 (explicit disable)", gcfg.LivenessNoProgressN)
	}
}

// TestParseSentinelConfig_TrivialVerifyCommand verifies that Phase-2 classes
// with trivially-always-exit-0 verify commands are rejected at config-load time
// (flywheel-motion.md §5.3, bead hk-kgwv).
func TestParseSentinelConfig_TrivialVerifyCommand(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "true",
			yaml: "sentinel:\n  done_definition:\n    myclass: \"true\"\n",
		},
		{
			name: "colon",
			yaml: "sentinel:\n  done_definition:\n    myclass: \":\"\n",
		},
		{
			name: "whitespace-only",
			yaml: "sentinel:\n  done_definition:\n    myclass: \"   \"\n",
		},
		{
			name: "empty",
			yaml: "sentinel:\n  done_definition:\n    myclass: \"\"\n",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseSentinelConfig([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("expected ErrTrivialVerifyCommand for %q, got nil", tc.name)
			}
			var target *ErrTrivialVerifyCommand
			if !errors.As(err, &target) {
				t.Errorf("expected *ErrTrivialVerifyCommand, got %T: %v", err, err)
			}
		})
	}
}

// TestParseSentinelConfig_ValidVerifyCommand verifies that a real observable
// post-condition command is accepted and "merged" (Phase-1) is not affected.
func TestParseSentinelConfig_ValidVerifyCommand(t *testing.T) {
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
		t.Fatalf("unexpected error for valid verify commands: %v", err)
	}
	if got := cfg.DoneDefinitionFor("deploy-class"); got != "make deploy && make smoke" {
		t.Errorf("DoneDefinitionFor(deploy-class): got %q", got)
	}
}

// TestParseSentinelConfig_KeeperSiblingNonInterference verifies that a
// config.yaml containing both sentinel: and keeper: blocks loads the sentinel
// block correctly without error (the sentinel parser ignores unknown top-level
// keys; the keeper: block must not pollute or break the sentinel config path).
func TestParseSentinelConfig_KeeperSiblingNonInterference(t *testing.T) {
	t.Parallel()
	yamlData := []byte(`
keeper:
  context_thresholds:
    warn_abs_tokens: 200000
    act_abs_tokens: 250000
sentinel:
  window: 20m
  liveness_no_progress_n: 5
`)
	cfg, err := parseSentinelConfig(yamlData)
	if err != nil {
		t.Fatalf("expected no error when keeper: block is present alongside sentinel:, got: %v", err)
	}
	if got := cfg.GovernorWindow(); got != 20*time.Minute {
		t.Errorf("GovernorWindow: got %v, want 20m", got)
	}
	gcfg, gerr := cfg.GovernorConfig()
	if gerr != nil {
		t.Fatalf("unexpected GovernorConfig error: %v", gerr)
	}
	if got := gcfg.LivenessNoProgressN; got != 5 {
		t.Errorf("LivenessNoProgressN: got %d, want 5", got)
	}
}

// TestParseSentinelConfig_MalformedPhaseFlagExpiry verifies that a valid
// phase_flag accompanied by a non-RFC3339 phase_flag_expiry returns an error.
func TestParseSentinelConfig_MalformedPhaseFlagExpiry(t *testing.T) {
	t.Parallel()
	_, err := parseSentinelConfig([]byte(`
sentinel:
  phase_flag: freeze
  phase_flag_expiry: "not-a-timestamp"
`))
	if err == nil {
		t.Fatal("expected error for malformed phase_flag_expiry, got nil")
	}
}

// TestSentinelConfig_SuppressionTTLDefault verifies that suppressionTTL and
// attachedInactiveTimeout fall back to their compiled defaults when not configured.
func TestSentinelConfig_SuppressionTTLDefault(t *testing.T) {
	t.Parallel()
	cfg, err := parseSentinelConfig([]byte(`sentinel: {}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.suppressionTTL(); got != DefaultSuppressionTTL {
		t.Errorf("suppressionTTL default: got %v, want %v", got, DefaultSuppressionTTL)
	}
	if got := cfg.attachedInactiveTimeout(); got != DefaultAttachedInactiveTimeout {
		t.Errorf("attachedInactiveTimeout default: got %v, want %v", got, DefaultAttachedInactiveTimeout)
	}
}
