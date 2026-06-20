package keeper

import "testing"

// TestLiveKeeperPresent_NoLockfile: an agent that was never started (no lockfile)
// has no live keeper.
func TestLiveKeeperPresent_NoLockfile(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	if LiveKeeperPresent(projectDir, "never-started") {
		t.Error("LiveKeeperPresent: want false when no lockfile exists")
	}
}

// TestLiveKeeperPresent_LockHeld: while a keeper holds the exclusive flock, the
// probe reports present=true; after Release it reports false (lockfile lingers but
// is unlocked).
func TestLiveKeeperPresent_LockHeld(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	agent := "held-agent"

	lock, err := AcquireLock(projectDir, agent)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if !LiveKeeperPresent(projectDir, agent) {
		t.Error("LiveKeeperPresent: want true while the exclusive lock is held")
	}

	if relErr := lock.Release(); relErr != nil {
		t.Fatalf("Release: %v", relErr)
	}
	// Lockfile still exists but is no longer flocked → no live keeper.
	if LiveKeeperPresent(projectDir, agent) {
		t.Error("LiveKeeperPresent: want false after the lock is released")
	}
}

// TestLiveKeeperPresent_InvalidAgent: a traversal agent name reports false (no
// valid lock can exist for it).
func TestLiveKeeperPresent_InvalidAgent(t *testing.T) {
	t.Parallel()
	if LiveKeeperPresent(t.TempDir(), "../evil") {
		t.Error("LiveKeeperPresent: want false for a traversal agent name")
	}
}

// TestEffectiveBandTokens_DefaultsAndTightenOnly pins the W7 honest-band helper:
// unset (0) inputs fall back to the compiled abs defaults, and an explicit pct
// ceil is tighten-only (a high pct on a large window cannot push the threshold
// LATER than the abs band; a low pct moves it EARLIER).
func TestEffectiveBandTokens_DefaultsAndTightenOnly(t *testing.T) {
	t.Parallel()

	// windowSize 0 → abs defaults verbatim (pct applied at runtime, not yet).
	warn, act, force := EffectiveBandTokens(0, 0, 0, 0, 0, 0)
	if warn != DefaultWarnAbsTokens || act != DefaultActAbsTokens {
		t.Errorf("defaults: want warn=%d act=%d, got warn=%d act=%d", DefaultWarnAbsTokens, DefaultActAbsTokens, warn, act)
	}
	if force <= act {
		t.Errorf("force band should exceed act band; got force=%d act=%d", force, act)
	}

	// High act-pct on a 1M window: tighten-only means it CANNOT exceed the abs cap.
	_, actHigh, _ := EffectiveBandTokens(0, 0, 0, 0, 0.99, 1_000_000)
	if actHigh > DefaultActAbsTokens {
		t.Errorf("tighten-only: act-pct 99%% on a 1M window must not exceed abs cap %d; got %d", DefaultActAbsTokens, actHigh)
	}

	// Low act-pct on a 1M window: moves the act threshold EARLIER than the abs band.
	_, actLow, _ := EffectiveBandTokens(0, 0, 0, 0, 0.10, 1_000_000)
	if actLow >= DefaultActAbsTokens {
		t.Errorf("low act-pct should fire EARLIER than abs band %d; got %d", DefaultActAbsTokens, actLow)
	}
}
