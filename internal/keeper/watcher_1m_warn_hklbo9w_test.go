package keeper_test

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// TestWatcher_LargeWindow_NoWarnBelowWarnPct is the RED test for hk-lbo9w.
//
// A 1M-window session at tokens=200001/pct=20 must emit ZERO session_keeper_warn.
// The abs-token gate (defaultWarnAbsTokens=200k) resolves to 200k on a 1M window
// (min(200k,700k)=200k). With the BUGGY gate:
//
//	200001 < 200000 → false → belowWarnThreshold=false → warn fires at pct=20.
//
// With the FIX (pct<WarnPct is a necessary condition):
//
//	20 < 80 → true → belowWarnThreshold=true → no warn.
func TestWatcher_LargeWindow_NoWarnBelowWarnPct(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "1m-no-warn-agent"

	writeCtxFileTokens(t, projectDir, agent, 20.0, 200_001, 1_000_000, "sess-1m-no-warn")

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 5 * time.Millisecond,
		WarnPct:      80.0,
		// WarnAbsTokens zero → applyDefaults fills defaultWarnAbsTokens (200k).
		// WarnPctCeil zero → applyDefaults fills defaultWarnPctCeil (0.70).
		IdleQuiesce: 1 * time.Millisecond,
		Staleness:   120 * time.Second, // generous — gauge stays fresh
		TmuxTarget:  "",
	}

	em := &keeper.RecordingEmitter{}
	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) != 0 {
		t.Errorf("hk-lbo9w: want 0 session_keeper_warn at pct=20 < warn_pct=80 on 1M window (tokens=200001); got %d — abs gate fired without pct guard", len(warns))
	}
}

// TestWatcher_LargeWindow_WarnFiresAtWarnPct verifies the positive case: on a
// 1M-window session at pct=80/tokens=800000, exactly ONE session_keeper_warn
// must fire. This is the upward crossing; subsequent ticks must be silent.
func TestWatcher_LargeWindow_WarnFiresAtWarnPct(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "1m-at-warn-agent"

	writeCtxFileTokens(t, projectDir, agent, 80.0, 800_000, 1_000_000, "sess-1m-at-warn")

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		PollInterval: 5 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "",
		// WarnCooldown=0 disables the dip-rise cooldown gate so the watcher fires
		// on the first upward crossing without waiting for a cooldown period.
		WarnCooldown: 0,
	}

	em := &keeper.RecordingEmitter{}
	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) != 1 {
		t.Errorf("hk-lbo9w: want exactly 1 session_keeper_warn at pct=80 == warn_pct=80 on 1M window (tokens=800000); got %d", len(warns))
	}
}
