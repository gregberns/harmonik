package keeper_test

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// TestWatcher_LargeWindow_WarnsAtAbsoluteBand proves that the configured token
// band remains meaningful on a 1M window. Percentage is a fallback when the
// gauge has no token count. It does not mute the absolute notice band.
func TestWatcher_LargeWindow_WarnsAtAbsoluteBand(t *testing.T) {
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
	if len(warns) != 1 {
		t.Errorf("want 1 session_keeper_warn at the absolute band on a 1M window; got %d", len(warns))
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
