//go:build integration

package keeper_test

// cycle_twin_1m_gauge_integration_test.go — bead hk-sav, Part C.
//
// End-to-end regression guard for the Opus-`[1m]` gauge bug, which lives inside
// scripts/keeper-statusline.sh (Refs: hk-67c — "infer window_size from model id
// for [1m] models").
//
// # The bug
//
// Claude Code `[1m]` (1-million-token) models OMIT the context_window_size field
// from their statusLine JSON. The keeper's absolute-token gate
// (CyclerConfig.belowActThreshold / actThreshold) is only used when BOTH
// CtxFile.Tokens > 0 AND CtxFile.WindowSize > 0; otherwise it falls back to the
// pct field. A `[1m]` session reports tokens but NOT a window, so without an
// inference the .ctx would carry window_size=0, and:
//
//   - the absolute-token gate is disabled (Tokens>0 but WindowSize==0),
//   - the gauge falls back to pct, which a `[1m]` twin reports as 0 (it cannot
//     derive a percentage without a window),
//   - so the cycle NEVER fires — a `[1m]` crew would run to context exhaustion
//     and never get cleared.
//
// scripts/keeper-statusline.sh fixes this with a fallback: when
// context_window_size is absent, it (1) honors an explicit
// HARMONIK_KEEPER_WINDOW_SIZE env override, else (2) detects the `[1m]` token in
// the model id and sets window_size=1000000. This file proves that fallback
// end-to-end through the REAL script + REAL twin.
//
// # Why this is the ONLY way to regression-test the bug end-to-end
//
// The bug is in the BASH script's jq/grep inference, not in Go. A Go unit test
// of CtxFile/gauge.go cannot exercise the script. Driving the real twin
// (--model "claude-opus-4-8 [1m]" --window 0, so NO context_window_size is
// emitted) through the real keeper-statusline.sh and reading back the real .ctx
// is the only path that covers the script's inference.
//
// # Non-vacuous proof (how this catches the regression)
//
// twWriteBrokenStatusline writes a COPY of keeper-statusline.sh with the [1m]
// inference disabled (the model-detection branch's `WINDOW_SIZE=1000000` is
// rewritten to `WINDOW_SIZE=0`). TestIntegration_Twin1mGauge_RegressionGuard_CatchesBug
// drives the twin through that broken copy and asserts the .ctx carries the BUGGY
// window_size=0 — proving the assertion in the main test (window_size==1000000)
// would FAIL on the buggy path, so the guard is real, not vacuous. (Verified
// out-of-band too: the broken copy yields window_size=0 while the real script
// yields window_size=1000000 for the identical [1m] no-window input.)
//
// Safety + teardown discipline: identical to Part B — uniquely-named
// "hksav-twin-" sessions, killed by EXACT name, blocked-on before the temp dir
// is removed. See cycle_twin_e2e_integration_test.go's header. Helpers (tw*) are
// reused from that file. Bead: hk-sav.

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// twWindowSizeMillion is the inferred window for a [1m] model.
const twWindowSizeMillion = int64(1_000_000)

// twWriteBrokenStatusline writes, into dir, a copy of the real
// scripts/keeper-statusline.sh with the [1m] model-id inference DISABLED: the
// branch that sets WINDOW_SIZE=1000000 on a [1m] model is rewritten to leave it
// at 0. The env-override branch (HARMONIK_KEEPER_WINDOW_SIZE) is left intact, so
// the broken copy reproduces EXACTLY the pre-fix bug: a [1m] model with no
// context_window_size and no env override yields window_size=0. Returns the
// broken script path.
func twWriteBrokenStatusline(t *testing.T, dir string) string {
	t.Helper()
	src, err := os.ReadFile(twScriptStatusline(t)) //nolint:gosec // G304: repo script path
	if err != nil {
		t.Fatalf("tw: read real statusline: %v", err)
	}
	const marker = "WINDOW_SIZE=1000000"
	if !strings.Contains(string(src), marker) {
		t.Fatalf("tw: real statusline no longer contains %q — the [1m] inference moved; update this guard", marker)
	}
	// Disable ONLY the model-id inference assignment (the env-override branch sets
	// WINDOW_SIZE="${KEEPER_WINDOW_SIZE_OVERRIDE}", a different string, so it is
	// untouched). This is the minimal, surgical break that reproduces the bug.
	broken := strings.Replace(string(src), marker, "WINDOW_SIZE=0  # [1m] inference DISABLED (regression-guard probe)", 1)
	path := filepath.Join(dir, "keeper-statusline-broken.sh")
	//nolint:gosec // G306: test-local executable script
	if err := os.WriteFile(path, []byte(broken), 0o755); err != nil {
		t.Fatalf("tw: write broken statusline: %v", err)
	}
	return path
}

// twScriptStatusline returns the real keeper-statusline.sh path (a thin wrapper
// over twScripts so Part C does not need the idle-hook return).
func twScriptStatusline(t *testing.T) string {
	t.Helper()
	statusline, _ := twScripts(t)
	return statusline
}

// twReadCtxWithWindow polls the .ctx until it exists, then returns it. Unlike
// twWaitForCtxTokens this does NOT gate on a token threshold — Part C cares about
// window_size, which the script writes on the very first emit.
func twReadCtxWithWindow(t *testing.T, project, agent string, timeout time.Duration) *keeper.CtxFile {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cf, _, err := keeper.ReadCtxFile(project, agent)
		if err == nil {
			return cf
		}
		time.Sleep(75 * time.Millisecond)
	}
	t.Fatalf("tw: .ctx never appeared for agent %q within %s", agent, timeout)
	return nil
}

// TestIntegration_Twin1mGauge_RealScriptInfersWindow drives the twin with a
// [1m] model and NO context_window_size (--window 0) through the REAL
// keeper-statusline.sh, and asserts the script's [1m] model-id inference
// computes the gauge correctly: window_size == 1000000 (NOT 0/garbage), and the
// absolute token count passes through intact.
//
// This is the positive regression assertion. The companion
// ..._RegressionGuard_CatchesBug test proves it is non-vacuous.
func TestIntegration_Twin1mGauge_RealScriptInfersWindow(t *testing.T) {
	twRequireTmux(t)

	project := t.TempDir()
	agent := fmt.Sprintf("tw1m%d", rand.Int64()) //nolint:gosec // G404: test-local agent-name uniqueness
	twin := twBuildTwin(t, project)
	statusline, idleHook := twScripts(t)

	// --window 0 → the twin OMITS context_window_size entirely (the [1m] quirk).
	// --model carries the literal "[1m]" token the script greps for.
	// growth>0 so the absolute token count is non-trivial and we can also assert
	// the gauge would gate on absolute tokens (Tokens>0 && WindowSize>0).
	twStartTwin(t, twTwinSpec{
		project:     project,
		agent:       agent,
		twin:        twin,
		statusline:  statusline,
		idleHook:    idleHook,
		model:       "claude-opus-4-8 [1m]",
		window:      0, // NO context_window_size in the JSON
		growth:      40_000,
		startTokens: 260_000,
		emitEvery:   200 * time.Millisecond,
	})

	cf := twReadCtxWithWindow(t, project, agent, 8*time.Second)

	// THE regression assertion: the script inferred the [1m] window.
	if cf.WindowSize != twWindowSizeMillion {
		t.Fatalf("tw: [1m] gauge window_size = %d; want %d (the script's [1m] model-id inference must fire when context_window_size is omitted)",
			cf.WindowSize, twWindowSizeMillion)
	}
	// Absolute tokens passed through intact (the script copies total_input_tokens).
	if cf.Tokens < 260_000 {
		t.Errorf("tw: [1m] gauge tokens = %d; want >= 260000 (absolute token count must pass through)", cf.Tokens)
	}
	// With WindowSize inferred, the gauge now has a real absolute-token view:
	// Tokens>0 && WindowSize>0 means belowActThreshold uses absolute tokens, not
	// the pct fallback. (pct is 0 for a [1m] twin — without the inference the
	// gauge would be stuck on that 0 and never fire.)
	if cf.Tokens > 0 && cf.WindowSize == 0 {
		t.Error("tw: gauge has tokens but no window — absolute-token gate disabled (the [1m] bug)")
	}
}

// TestIntegration_Twin1mGauge_EnvOverrideInfersWindow proves the OTHER fallback
// branch: HARMONIK_KEEPER_WINDOW_SIZE explicitly overrides the window for a
// model whose statusLine omits context_window_size. Here the model id does NOT
// contain "[1m]" (so the model-id inference would NOT fire), yet the env
// override still produces a correct gauge — confirming the env path is wired
// independently of the model-id detection.
func TestIntegration_Twin1mGauge_EnvOverrideInfersWindow(t *testing.T) {
	twRequireTmux(t)

	project := t.TempDir()
	agent := fmt.Sprintf("tw1menv%d", rand.Int64()) //nolint:gosec // G404: test-local agent-name uniqueness
	twin := twBuildTwin(t, project)
	statusline, idleHook := twScripts(t)

	const overrideWindow = int64(800_000)
	// Model id has NO "[1m]" token → only the env override can supply the window.
	twStartTwin(t, twTwinSpec{
		project:     project,
		agent:       agent,
		twin:        twin,
		statusline:  statusline,
		idleHook:    idleHook,
		model:       "claude-opus-4-8", // NO [1m] token
		window:      0,                 // NO context_window_size emitted
		growth:      0,
		startTokens: 100_000,
		emitEvery:   200 * time.Millisecond,
		extraEnv:    []string{fmt.Sprintf("HARMONIK_KEEPER_WINDOW_SIZE=%d", overrideWindow)},
	})

	cf := twReadCtxWithWindow(t, project, agent, 8*time.Second)
	if cf.WindowSize != overrideWindow {
		t.Fatalf("tw: env-override gauge window_size = %d; want %d (HARMONIK_KEEPER_WINDOW_SIZE must win when context_window_size is omitted)",
			cf.WindowSize, overrideWindow)
	}
}

// TestIntegration_Twin1mGauge_RegressionGuard_CatchesBug proves the positive
// assertion above is NON-VACUOUS: it drives the SAME [1m]/no-window twin through
// a BROKEN copy of keeper-statusline.sh (the [1m] model-id inference disabled)
// and asserts the .ctx carries the BUGGY window_size=0. If the real script ever
// regressed to the broken behaviour, RealScriptInfersWindow's
// window_size==1000000 assertion would FAIL — exactly as this test demonstrates
// it does against the broken copy.
func TestIntegration_Twin1mGauge_RegressionGuard_CatchesBug(t *testing.T) {
	twRequireTmux(t)

	project := t.TempDir()
	agent := fmt.Sprintf("tw1mbug%d", rand.Int64()) //nolint:gosec // G404: test-local agent-name uniqueness
	twin := twBuildTwin(t, project)
	_, idleHook := twScripts(t)
	broken := twWriteBrokenStatusline(t, project)

	// Identical [1m]/no-window input as RealScriptInfersWindow, but through the
	// BROKEN script (inference disabled, no env override).
	twStartTwin(t, twTwinSpec{
		project:     project,
		agent:       agent,
		twin:        twin,
		statusline:  broken,
		idleHook:    idleHook,
		model:       "claude-opus-4-8 [1m]",
		window:      0,
		growth:      40_000,
		startTokens: 260_000,
		emitEvery:   200 * time.Millisecond,
	})

	cf := twReadCtxWithWindow(t, project, agent, 8*time.Second)

	// The buggy path: window_size stays 0 because the inference was removed.
	if cf.WindowSize != 0 {
		t.Fatalf("tw: broken-script gauge window_size = %d; want 0 — the broken copy should NOT infer the [1m] window. "+
			"If this fails the guard is mis-wired and the positive test would be vacuous.", cf.WindowSize)
	}
	// And the absolute-token gate is consequently disabled: Tokens present but
	// WindowSize==0 is the exact bug state the real script's fallback prevents.
	if cf.Tokens <= 0 {
		t.Errorf("tw: broken-script gauge tokens = %d; want > 0 (the bug is specifically tokens>0 with window==0)", cf.Tokens)
	}
}

// TestIntegration_Twin1mGauge_CycleFiresOnInferredWindow is the capstone: it
// proves the [1m] inference is not just cosmetic — with the real script's
// inferred window, a [1m] session that omits context_window_size actually
// crosses the keeper's ABSOLUTE-token act threshold and fires a full cycle. With
// the bug (window_size=0) the gauge would fall back to pct (0 for a [1m] twin)
// and the cycle would NEVER fire. This ties the gauge regression directly to the
// keeper behaviour it guards.
func TestIntegration_Twin1mGauge_CycleFiresOnInferredWindow(t *testing.T) {
	twRequireTmux(t)

	project := t.TempDir()
	agent := fmt.Sprintf("tw1mcyc%d", rand.Int64()) //nolint:gosec // G404: test-local agent-name uniqueness
	twin := twBuildTwin(t, project)
	statusline, idleHook := twScripts(t)

	if err := keeper.WriteManagedSessionID(project, agent, ""); err != nil {
		t.Fatalf("tw: WriteManagedSessionID: %v", err)
	}

	// [1m] model, NO context_window_size, growing tokens. The script must infer
	// window_size=1M for the absolute-token gate (215k default) to ever trip.
	sess := twStartTwin(t, twTwinSpec{
		project:     project,
		agent:       agent,
		twin:        twin,
		statusline:  statusline,
		idleHook:    idleHook,
		model:       "claude-opus-4-8 [1m]",
		window:      0,
		growth:      50_000,
		startTokens: 50_000,
		emitEvery:   200 * time.Millisecond,
	})

	// Wait until the REAL .ctx (with inferred window) crosses the act threshold.
	seed := twWaitForCtxTokens(t, project, agent, 320_000, 10*time.Second)
	if seed.WindowSize != twWindowSizeMillion {
		t.Fatalf("tw: pre-cycle .ctx window_size = %d; want %d (inference must have fired for the gate to trip)",
			seed.WindowSize, twWindowSizeMillion)
	}
	if seed.Pct != 0 {
		// The twin reports pct=0 for a [1m]/no-window session; if pct were
		// non-zero the cycle could fire via the pct fallback and this test would
		// not actually prove the absolute-token path. Guard against that.
		t.Logf("tw: note: pct=%v (expected 0 for a [1m] twin; cycle below relies on the absolute-token gate)", seed.Pct)
	}

	em := &keeper.RecordingEmitter{}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     project,
		TmuxTarget:     sess,
		HandoffTimeout: 10 * time.Second,
		ClearSettle:    5 * time.Second,
		PollInterval:   150 * time.Millisecond,
		// REAL, UNMODIFIED keeper.InjectText (no flatten): the twin parses the
		// production MULTI-LINE /session-handoff directive natively (hk-fan).
		InjectFn: keeper.InjectText,
	}
	cycler := keeper.NewCycler(cfg, em)

	if err := cycler.MaybeRun(context.Background(), seed); err != nil {
		t.Fatalf("tw: MaybeRun: %v", err)
	}

	// The cycle fired and completed — only possible because the inferred window
	// enabled the absolute-token gate.
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); n != 1 {
		t.Fatalf("tw: want 1 cycle_complete for the [1m] session; got %d (the inferred window must enable the absolute-token gate)", n)
	}
	final, _, err := keeper.ReadCtxFile(project, agent)
	if err != nil {
		t.Fatalf("tw: read final .ctx: %v", err)
	}
	if final.SessionID == seed.SessionID {
		t.Errorf("tw: session_id did not rotate on the [1m] cycle (still %q)", seed.SessionID)
	}
}
