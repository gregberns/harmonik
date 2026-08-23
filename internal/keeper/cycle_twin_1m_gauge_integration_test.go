//go:build integration

package keeper_test

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

const twWindowSizeMillion = int64(500_000)

func twWriteBrokenStatusline(t *testing.T, dir string) string {
	t.Helper()
	src, err := os.ReadFile(twScriptStatusline(t)) //nolint:gosec // G304: repo script path
	if err != nil {
		t.Fatalf("tw: read real statusline: %v", err)
	}
	const marker = `WINDOW_SIZE="$(awk "BEGIN {printf \"%d\n\", 1000000 * ${_1M_FRACTION}}")"`
	if !strings.Contains(string(src), marker) {
		t.Fatalf("tw: real statusline no longer contains %q — the [1m] inference moved; update this guard", marker)
	}
	broken := strings.Replace(string(src), marker, "WINDOW_SIZE=0  # [1m] inference DISABLED (regression-guard probe)", 1)
	path := filepath.Join(dir, "keeper-statusline-broken.sh")
	//nolint:gosec // G306: test-local executable script
	if err := os.WriteFile(path, []byte(broken), 0o700); err != nil {
		t.Fatalf("tw: write broken statusline: %v", err)
	}
	return path
}

func twScriptStatusline(t *testing.T) string {
	t.Helper()
	statusline, _ := twScripts(t)
	return statusline
}

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
// computes the gauge correctly: window_size == 500000 (floor(1M*0.5), NOT 0/garbage),
// and the absolute token count passes through intact (hk-d8dj0).
//
// This is the positive regression assertion. The companion
// ..._RegressionGuard_CatchesBug test proves it is non-vacuous.
func TestIntegration_Twin1mGauge_RealScriptInfersWindow(t *testing.T) {
	twRequireTmux(t)

	project := t.TempDir()
	agent := fmt.Sprintf("tw1m%d", rand.Int64()) //nolint:gosec // G404: test-local agent-name uniqueness
	twin := twBuildTwin(t, project)
	statusline, idleHook := twScripts(t)

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

	if cf.WindowSize != twWindowSizeMillion {
		t.Fatalf("tw: [1m] gauge window_size = %d; want %d (the script's [1m] model-id inference must fire when context_window_size is omitted)",
			cf.WindowSize, twWindowSizeMillion)
	}
	if cf.Tokens < 260_000 {
		t.Errorf("tw: [1m] gauge tokens = %d; want >= 260000 (absolute token count must pass through)", cf.Tokens)
	}
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

	if cf.WindowSize != 0 {
		t.Fatalf("tw: broken-script gauge window_size = %d; want 0 — the broken copy should NOT infer the [1m] window. "+
			"If this fails the guard is mis-wired and the positive test would be vacuous.", cf.WindowSize)
	}
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

	seed := twWaitForCtxTokens(t, project, agent, 320_000, 10*time.Second)
	if seed.WindowSize != twWindowSizeMillion {
		t.Fatalf("tw: pre-cycle .ctx window_size = %d; want %d (inference must have fired for the gate to trip)",
			seed.WindowSize, twWindowSizeMillion)
	}
	if seed.Pct <= 0 {
		t.Logf("tw: note: pct=%v — expected > 0 (script should have recomputed pct = tokens/500k)", seed.Pct)
	}

	em := &keeper.RecordingEmitter{}
	cfgOverrides := testCycleOverrides{Inject:

	// REAL, UNMODIFIED keeper.InjectText (no flatten): the twin parses the
	// production MULTI-LINE /session-handoff directive natively (hk-fan).
	keeper.InjectText}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     project,
		TmuxTarget:     sess,
		HandoffTimeout: 10 * time.Second,
		ClearSettle:    5 * time.Second,
		PollInterval:   150 * time.Millisecond,
	}
	cycler := mustNewCyclerWithOverrides(cfg, em, cfgOverrides)

	if err := cycler.MaybeRun(context.Background(), seed); err != nil {
		t.Fatalf("tw: MaybeRun: %v", err)
	}

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
