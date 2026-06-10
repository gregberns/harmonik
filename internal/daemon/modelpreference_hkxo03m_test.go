package daemon_test

// modelpreference_hkxo03m_test.go — unit tests for --model/--effort argv emission
// and ModelPreference validation (hk-xo03m).
//
// Covers:
//   - Argv emits both flags when model and effort are populated.
//   - Argv emits only --model when effort is empty.
//   - Argv emits neither flag when both are empty.
//   - Invalid model (contains ";") returns *ModelPreferenceError.
//   - Invalid effort ("lowwww") returns *ModelPreferenceError.
//   - CheckForbiddenFlags does NOT reject --model or --effort (regression per HC-055a).
//
// Spec refs:
//   - specs/handler-contract.md §4.10 HC-055a — ModelPreference descriptor invariants.
//   - specs/handler-contract.md §4.10 HC-055 — allow-list includes --model / --effort.
//   - specs/execution-model.md §4.3 EM-012b — model/effort resolution chain.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
)

// ─────────────────────────────────────────────────────────────────────────────
// helper — build a claudeRunCtx fixture with model/effort fields
// ─────────────────────────────────────────────────────────────────────────────

func modelPrefFixtureRunCtx(t *testing.T, ws, model, effort string) daemon.ExportedClaudeRunCtx {
	t.Helper()
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("modelPrefFixtureRunCtx: NewV7: %v", err)
	}
	return daemon.ExportedClaudeRunCtx{
		RunID:         core.RunID(runUID),
		BeadID:        "test-bead-xo03m",
		WorkspacePath: ws,
		DaemonSocket:  "/tmp/harmonik-test-xo03m.sock",
		WorkflowMode:  core.WorkflowModeSingle,
		HandlerBinary: "claude",
		BaseEnv:       []string{"HARMONIK_PROJECT_HASH=deadbeef123456"},
		Model:         model,
		Effort:        effort,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestModelEffort_BothPopulated — both flags emitted, correct ordering
// ─────────────────────────────────────────────────────────────────────────────

func TestModelEffort_BothPopulated(t *testing.T) {
	t.Parallel()

	ws := claudeLaunchSpecFixtureWorkspace(t)
	rc := modelPrefFixtureRunCtx(t, ws, "claude-opus-4-5", "high")

	spec, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("TestModelEffort_BothPopulated: unexpected error: %v", err)
	}

	// Verify --session-id first, then --model, then --effort.
	args := spec.Args
	modelPrefAssertFlag(t, args, "--model", "claude-opus-4-5")
	modelPrefAssertFlag(t, args, "--effort", "high")
	modelPrefAssertOrdering(t, args, "--session-id", "--model", "--effort")
}

// ─────────────────────────────────────────────────────────────────────────────
// TestModelEffort_ModelOnly — --model emitted, no --effort
// ─────────────────────────────────────────────────────────────────────────────

func TestModelEffort_ModelOnly(t *testing.T) {
	t.Parallel()

	ws := claudeLaunchSpecFixtureWorkspace(t)
	rc := modelPrefFixtureRunCtx(t, ws, "claude-sonnet-4-6", "")

	spec, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("TestModelEffort_ModelOnly: unexpected error: %v", err)
	}

	args := spec.Args
	modelPrefAssertFlag(t, args, "--model", "claude-sonnet-4-6")
	modelPrefAssertAbsent(t, args, "--effort")
}

// ─────────────────────────────────────────────────────────────────────────────
// TestModelEffort_NeitherPopulated — neither flag emitted
// ─────────────────────────────────────────────────────────────────────────────

func TestModelEffort_NeitherPopulated(t *testing.T) {
	t.Parallel()

	ws := claudeLaunchSpecFixtureWorkspace(t)
	rc := modelPrefFixtureRunCtx(t, ws, "", "")

	spec, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err != nil {
		t.Fatalf("TestModelEffort_NeitherPopulated: unexpected error: %v", err)
	}

	args := spec.Args
	modelPrefAssertAbsent(t, args, "--model")
	modelPrefAssertAbsent(t, args, "--effort")
}

// ─────────────────────────────────────────────────────────────────────────────
// TestModelEffort_InvalidModel — bad model returns *ModelPreferenceError
// ─────────────────────────────────────────────────────────────────────────────

func TestModelEffort_InvalidModel(t *testing.T) {
	t.Parallel()

	ws := claudeLaunchSpecFixtureWorkspace(t)
	// Semicolon is a shell metacharacter and fails the regex.
	rc := modelPrefFixtureRunCtx(t, ws, "claude;bad", "")

	_, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err == nil {
		t.Fatal("TestModelEffort_InvalidModel: expected error for model containing ';'; got nil")
	}
	var mpe *daemon.ExportedModelPreferenceError
	if !errors.As(err, &mpe) {
		t.Errorf("TestModelEffort_InvalidModel: error type = %T (%v); want *ModelPreferenceError", err, err)
		return
	}
	if mpe.Field != "model" {
		t.Errorf("ModelPreferenceError.Field = %q; want %q", mpe.Field, "model")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestModelEffort_InvalidEffort — bad effort returns *ModelPreferenceError
// ─────────────────────────────────────────────────────────────────────────────

func TestModelEffort_InvalidEffort(t *testing.T) {
	t.Parallel()

	ws := claudeLaunchSpecFixtureWorkspace(t)
	rc := modelPrefFixtureRunCtx(t, ws, "", "lowwww")

	_, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err == nil {
		t.Fatal("TestModelEffort_InvalidEffort: expected error for effort 'lowwww'; got nil")
	}
	var mpe *daemon.ExportedModelPreferenceError
	if !errors.As(err, &mpe) {
		t.Errorf("TestModelEffort_InvalidEffort: error type = %T (%v); want *ModelPreferenceError", err, err)
		return
	}
	if mpe.Field != "effort" {
		t.Errorf("ModelPreferenceError.Field = %q; want %q", mpe.Field, "effort")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestModelEffort_ModelTooLong — model > 128 chars returns *ModelPreferenceError
// ─────────────────────────────────────────────────────────────────────────────

func TestModelEffort_ModelTooLong(t *testing.T) {
	t.Parallel()

	ws := claudeLaunchSpecFixtureWorkspace(t)
	// Build a 129-char string of valid characters.
	longModel := ""
	for i := 0; i < 129; i++ {
		longModel += "a"
	}
	rc := modelPrefFixtureRunCtx(t, ws, longModel, "")

	_, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rc)
	if err == nil {
		t.Fatal("TestModelEffort_ModelTooLong: expected error for model > 128 chars; got nil")
	}
	var mpe *daemon.ExportedModelPreferenceError
	if !errors.As(err, &mpe) {
		t.Errorf("TestModelEffort_ModelTooLong: error type = %T (%v); want *ModelPreferenceError", err, err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestModelEffort_CheckForbiddenFlags_AllowsModelAndEffort — regression per HC-055
// ─────────────────────────────────────────────────────────────────────────────

// TestModelEffort_CheckForbiddenFlags_AllowsModelAndEffort verifies that
// CheckForbiddenFlags does NOT reject --model or --effort, confirming HC-055
// allow-list extension lands correctly (HC-055a).
func TestModelEffort_CheckForbiddenFlags_AllowsModelAndEffort(t *testing.T) {
	t.Parallel()

	argv := []string{"--session-id", "some-uuid", "--model", "claude-opus-4-5", "--effort", "high"}
	if err := handler.CheckForbiddenFlags(argv, nil); err != nil {
		t.Errorf("CheckForbiddenFlags incorrectly rejected --model/--effort: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// assertion helpers
// ─────────────────────────────────────────────────────────────────────────────

// modelPrefAssertFlag verifies that args contains flagName followed by wantValue.
func modelPrefAssertFlag(t *testing.T, args []string, flagName, wantValue string) {
	t.Helper()
	for i, a := range args {
		if a == flagName {
			if i+1 >= len(args) {
				t.Errorf("%s present but value is missing in args %v", flagName, args)
				return
			}
			if args[i+1] != wantValue {
				t.Errorf("%s value = %q; want %q", flagName, args[i+1], wantValue)
			}
			return
		}
	}
	t.Errorf("%s not found in args %v", flagName, args)
}

// modelPrefAssertAbsent verifies that flagName does not appear in args.
func modelPrefAssertAbsent(t *testing.T, args []string, flagName string) {
	t.Helper()
	for _, a := range args {
		if a == flagName {
			t.Errorf("%s must not be present in args %v", flagName, args)
			return
		}
	}
}

// modelPrefAssertOrdering verifies that the flags appear in the given order
// within args (not necessarily adjacent).
func modelPrefAssertOrdering(t *testing.T, args []string, flags ...string) {
	t.Helper()
	positions := make([]int, len(flags))
	for fi, flag := range flags {
		found := false
		for ai, a := range args {
			if a == flag {
				positions[fi] = ai
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ordering check: flag %q not found in args %v", flag, args)
			return
		}
	}
	for i := 1; i < len(positions); i++ {
		if positions[i] <= positions[i-1] {
			t.Errorf("ordering check: %q (pos %d) must come after %q (pos %d) in args %v",
				flags[i], positions[i], flags[i-1], positions[i-1], args)
		}
	}
}
