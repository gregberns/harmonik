//go:build integration

package keeper_test

// cycle_twin_gauge_liveness_integration_test.go — bead hk-baf4.
//
// Exercises the twin's two gauge-liveness test-harness flags through the REAL
// scripts/keeper-statusline.sh → <project>/.harmonik/keeper/<agent>.ctx pipeline
// in a real tmux pane. These flags exist in ONE place (the twin binary +
// twTwinSpec/twStartTwin, both single-definition) so the downstream
// keeper-redesign beads (gauge-liveness, live-pane, force-restart) reuse them
// without each re-adding the same harness knob (helper-redeclare collision).
//
//   - --emit-na: every statusLine carries a non-numeric used_percentage ("NA"),
//     so the real script's numeric guard SKIPS the .ctx write — the gauge file
//     never appears even though the session is alive and ticking.
//   - --suppress-statusline-after <dur>: statusLine emits stop after <dur>, so
//     the .ctx goes STALE while the session stays alive (the idle hook keeps
//     firing). This is the input the gauge-liveness / force-restart paths need.
//
// NO production behavior change: both flags are test-only knobs on the twin.
//
// Helper prefix: tw (twin); shares the safety/teardown discipline documented in
// cycle_twin_e2e_integration_test.go (uniquely-named throwaway sessions only).

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

// twIdlePath returns the absolute path to the twin's .idle marker, touched by
// the real stop hook on every emit while the session is alive.
func twIdlePath(project, agent string) string {
	return filepath.Join(project, ".harmonik", "keeper", agent+".idle")
}

// twWaitForIdle polls for the .idle marker and returns its modTime once present,
// failing after timeout. The marker appearing proves the twin BOOTED and its
// emitter ticked at least once — i.e. the twin accepted its flags (a binary that
// rejected an unknown flag exits before ever firing the idle hook).
func twWaitForIdle(t *testing.T, project, agent string, timeout time.Duration) time.Time {
	t.Helper()
	deadline := time.Now().Add(timeout)
	idle := twIdlePath(project, agent)
	for time.Now().Before(deadline) {
		if fi, err := os.Stat(idle); err == nil {
			return fi.ModTime()
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("tw: .idle marker never appeared for agent %q within %s (twin failed to boot/tick — did it reject a flag?)", agent, timeout)
	return time.Time{}
}

// TestIntegration_TwinEmitNA_SkipsCtxWrite proves --emit-na makes the twin emit
// a non-numeric used_percentage so the REAL keeper-statusline.sh skips the .ctx
// write entirely, while the session stays alive (the .idle marker advances).
//
// RED on main: the twin binary on main does not define --emit-na, so it exits
// before emitting and the .idle marker never appears (twWaitForIdle fails).
func TestIntegration_TwinEmitNA_SkipsCtxWrite(t *testing.T) {
	twRequireTmux(t)

	project := t.TempDir()
	agent := fmt.Sprintf("twna%d", rand.Int64()) //nolint:gosec // G404: test-local agent-name uniqueness
	twin := twBuildTwin(t, project)
	statusline, idleHook := twScripts(t)

	twStartTwin(t, twTwinSpec{
		project:     project,
		agent:       agent,
		twin:        twin,
		statusline:  statusline,
		idleHook:    idleHook,
		model:       "claude-opus-4-8 [1m]",
		window:      1_000_000,
		growth:      50_000,
		startTokens: 50_000,
		emitEvery:   150 * time.Millisecond,
		emitNA:      true,
	})

	// The twin is alive and ticking: the idle hook fired (flags accepted).
	_ = twWaitForIdle(t, project, agent, 5*time.Second)

	// Give several emit intervals' worth of NA statusLines a chance to (not)
	// write the gauge file.
	time.Sleep(1 * time.Second)

	// The .ctx must NOT exist: every emit was NA, so the script's numeric guard
	// skipped the write on every tick.
	if _, _, err := keeper.ReadCtxFile(project, agent); !os.IsNotExist(err) {
		cf, _, _ := keeper.ReadCtxFile(project, agent)
		t.Fatalf("tw: --emit-na wrote a .ctx (err=%v, file=%+v); the NA statusLine must skip the write", err, cf)
	}
}

// TestIntegration_TwinSuppressStatusline_GoesStale proves
// --suppress-statusline-after stops the statusLine emits after the deadline so
// the gauge .ctx FREEZES, while the session stays alive (the .idle marker keeps
// advancing because the idle hook is not suppressed).
//
// RED on main: the twin binary on main does not define
// --suppress-statusline-after, so it exits before emitting and the .ctx never
// appears (twWaitForCtxTokens fails before the staleness assertion is reached).
func TestIntegration_TwinSuppressStatusline_GoesStale(t *testing.T) {
	twRequireTmux(t)

	project := t.TempDir()
	agent := fmt.Sprintf("twsup%d", rand.Int64()) //nolint:gosec // G404: test-local agent-name uniqueness
	twin := twBuildTwin(t, project)
	statusline, idleHook := twScripts(t)

	const emitEvery = 150 * time.Millisecond
	twStartTwin(t, twTwinSpec{
		project:       project,
		agent:         agent,
		twin:          twin,
		statusline:    statusline,
		idleHook:      idleHook,
		model:         "claude-opus-4-8 [1m]",
		window:        1_000_000,
		growth:        50_000,
		startTokens:   50_000,
		emitEvery:     emitEvery,
		suppressAfter: 1 * time.Second,
	})

	// Before suppression: the .ctx is written normally. Wait for it to appear.
	if cf := twWaitForCtxTokens(t, project, agent, 50_000, 5*time.Second); cf == nil {
		t.Fatal("tw: .ctx never appeared before the suppression deadline")
	}

	// Wait well past the suppression deadline + several emit intervals so the
	// last pre-suppression write has definitely landed and emits have stopped.
	time.Sleep(1*time.Second + 6*emitEvery)

	// Sample 1 (post-suppression): record the frozen .ctx modTime and the live
	// .idle modTime.
	_, ctxMod1, err := keeper.ReadCtxFile(project, agent)
	if err != nil {
		t.Fatalf("tw: read .ctx (sample 1): %v", err)
	}
	idleFi1, err := os.Stat(twIdlePath(project, agent))
	if err != nil {
		t.Fatalf("tw: stat .idle (sample 1): %v", err)
	}

	// Let several more emit ticks elapse.
	time.Sleep(6 * emitEvery)

	// Sample 2: the .ctx must be FROZEN (statusLine suppressed) while the .idle
	// must have ADVANCED (idle hook still firing → session alive, only the
	// statusLine emit is suppressed).
	_, ctxMod2, err := keeper.ReadCtxFile(project, agent)
	if err != nil {
		t.Fatalf("tw: read .ctx (sample 2): %v", err)
	}
	idleFi2, err := os.Stat(twIdlePath(project, agent))
	if err != nil {
		t.Fatalf("tw: stat .idle (sample 2): %v", err)
	}

	if !ctxMod2.Equal(ctxMod1) {
		t.Errorf("tw: .ctx advanced after suppression (%s → %s); statusLine emits should have stopped", ctxMod1, ctxMod2)
	}
	if !idleFi2.ModTime().After(idleFi1.ModTime()) {
		t.Errorf("tw: .idle did not advance (%s → %s); the session should still be alive (only statusLine suppressed)", idleFi1.ModTime(), idleFi2.ModTime())
	}
}
