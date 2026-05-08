package lifecycle

import (
	"runtime"
	"syscall"
	"testing"
)

// supervisionFixtureFDsPerHandler is the conservative per-handler file-descriptor
// budget used in the PL-014a ceiling formula.
//
// Spec ref: process-lifecycle.md §4.5 PL-014a — "FDS_PER_HANDLER = 8
// (conservative, accounting for stdin/stdout/stderr/socket-conn plus transient
// spikes)."
const supervisionFixtureFDsPerHandler = 8

// supervisionFixtureFallbackCap is the maximum ceiling when the rlimit-derived
// value would exceed it.
//
// Spec ref: process-lifecycle.md §4.5 PL-014a — "FALLBACK_CAP = 1024."
const supervisionFixtureFallbackCap = 1024

// supervisionFixtureMinNofile is the target soft RLIMIT_NOFILE value the daemon
// attempts to reach on startup if the current soft limit is below it.
//
// Spec ref: process-lifecycle.md §4.5 PL-014a — "if the soft limit is below
// MIN_NOFILE = 4096, the daemon MUST attempt setrlimit to raise the soft limit
// to min(4096, hard)."
const supervisionFixtureMinNofile = 4096

// supervisionFixtureComputeCeiling derives the per-daemon agent concurrency
// ceiling from a RLIMIT_NOFILE soft value. Returns min(soft/FDS_PER_HANDLER,
// FALLBACK_CAP).
//
// Spec ref: process-lifecycle.md §4.5 PL-014a — "The default ceiling, when no
// operator override is set, is min(RLIMIT_NOFILE_soft / FDS_PER_HANDLER,
// FALLBACK_CAP)."
func supervisionFixtureComputeCeiling(nofileSoft uint64) uint64 {
	derived := nofileSoft / supervisionFixtureFDsPerHandler
	if derived > supervisionFixtureFallbackCap {
		return supervisionFixtureFallbackCap
	}
	return derived
}

// supervisionFixtureTargetSoftNofile returns the target soft RLIMIT_NOFILE value
// the daemon sets at startup: min(MIN_NOFILE=4096, hard).
//
// Spec ref: process-lifecycle.md §4.5 PL-014a — "the daemon MUST attempt
// setrlimit to raise the soft limit to min(4096, hard)."
func supervisionFixtureTargetSoftNofile(hard uint64) uint64 {
	if hard < supervisionFixtureMinNofile {
		return hard
	}
	return supervisionFixtureMinNofile
}

// TestPL014a_RlimitCeiling verifies the rlimit-derived concurrency ceiling
// math and the setrlimit-raise discipline.
//
// Spec ref: process-lifecycle.md §4.5 PL-014a — "The daemon MUST enforce a
// configurable ceiling on simultaneously-running agent subprocesses. The
// default ceiling, when no operator override is set, is min(RLIMIT_NOFILE_soft /
// FDS_PER_HANDLER, FALLBACK_CAP) where FDS_PER_HANDLER = 8 and FALLBACK_CAP =
// 1024. The daemon MUST getrlimit(RLIMIT_NOFILE) at PL-005 step 0; if the soft
// limit is below MIN_NOFILE = 4096, the daemon MUST attempt setrlimit to raise
// the soft limit to min(4096, hard)."
func TestPL014a_RlimitCeiling(t *testing.T) {
	t.Parallel()

	t.Run("ceiling-math/low-soft-limit", func(t *testing.T) {
		t.Parallel()

		// macOS default is 256; ceiling should be 256/8 = 32.
		const softLimit uint64 = 256
		got := supervisionFixtureComputeCeiling(softLimit)
		want := uint64(32)
		if got != want {
			t.Errorf("PL-014a ceiling(soft=%d): got %d, want %d", softLimit, got, want)
		}
	})

	t.Run("ceiling-math/medium-soft-limit", func(t *testing.T) {
		t.Parallel()

		// soft = 4096 → derived = 512, below FALLBACK_CAP=1024.
		const softLimit uint64 = 4096
		got := supervisionFixtureComputeCeiling(softLimit)
		want := uint64(512)
		if got != want {
			t.Errorf("PL-014a ceiling(soft=%d): got %d, want %d", softLimit, got, want)
		}
	})

	t.Run("ceiling-math/high-soft-limit-capped-by-fallback", func(t *testing.T) {
		t.Parallel()

		// soft = 65536 → derived = 8192, exceeds FALLBACK_CAP → capped at 1024.
		const softLimit uint64 = 65536
		got := supervisionFixtureComputeCeiling(softLimit)
		if got != supervisionFixtureFallbackCap {
			t.Errorf("PL-014a ceiling(soft=%d): got %d, want FALLBACK_CAP=%d", softLimit, got, supervisionFixtureFallbackCap)
		}
	})

	t.Run("ceiling-math/boundary-exactly-at-fallback", func(t *testing.T) {
		t.Parallel()

		// soft = 8192 → derived = 1024 = FALLBACK_CAP exactly; must not exceed.
		const softLimit uint64 = 8192
		got := supervisionFixtureComputeCeiling(softLimit)
		if got != supervisionFixtureFallbackCap {
			t.Errorf("PL-014a ceiling(soft=%d): got %d, want FALLBACK_CAP=%d (boundary)", softLimit, got, supervisionFixtureFallbackCap)
		}
	})

	t.Run("ceiling-math/zero-soft-limit-yields-zero", func(t *testing.T) {
		t.Parallel()

		// Degenerate: if somehow soft=0, ceiling = 0 (no agents allowed).
		const softLimit uint64 = 0
		got := supervisionFixtureComputeCeiling(softLimit)
		if got != 0 {
			t.Errorf("PL-014a ceiling(soft=0): got %d, want 0", got)
		}
	})

	t.Run("ceiling-math/real-rlimit-nofile", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
			t.Skipf("RLIMIT_NOFILE test: skipping on %s (POSIX rlimit only)", runtime.GOOS)
		}

		var rl syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rl); err != nil {
			t.Fatalf("PL-014a: Getrlimit(RLIMIT_NOFILE): %v", err)
		}

		// Ceiling must be ≥ 1 for a sane system (soft > FDS_PER_HANDLER).
		ceiling := supervisionFixtureComputeCeiling(rl.Cur)
		t.Logf("PL-014a: RLIMIT_NOFILE soft=%d hard=%d → ceiling=%d", rl.Cur, rl.Max, ceiling)

		if rl.Cur > supervisionFixtureFDsPerHandler && ceiling == 0 {
			t.Errorf("PL-014a: RLIMIT_NOFILE soft=%d (>FDS_PER_HANDLER=%d) produced ceiling=0; expected ≥1",
				rl.Cur, supervisionFixtureFDsPerHandler)
		}

		// Ceiling must never exceed FALLBACK_CAP.
		if ceiling > supervisionFixtureFallbackCap {
			t.Errorf("PL-014a: ceiling %d exceeds FALLBACK_CAP=%d; invariant violated", ceiling, supervisionFixtureFallbackCap)
		}
	})

	t.Run("setrlimit/raise-soft-to-min-4096-hard", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
			t.Skipf("setrlimit test: skipping on %s (POSIX rlimit only)", runtime.GOOS)
		}

		var rl syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rl); err != nil {
			t.Fatalf("PL-014a setrlimit: Getrlimit: %v", err)
		}

		originalSoft := rl.Cur
		originalHard := rl.Max

		// PL-014a: target is min(4096, hard).
		target := supervisionFixtureTargetSoftNofile(originalHard)
		t.Logf("PL-014a setrlimit: soft=%d hard=%d target=%d", originalSoft, originalHard, target)

		if originalSoft >= supervisionFixtureMinNofile {
			// Soft is already at or above MIN_NOFILE — setrlimit raise is a no-op.
			t.Logf("PL-014a setrlimit: soft=%d already >= MIN_NOFILE=%d; skipping setrlimit raise",
				originalSoft, supervisionFixtureMinNofile)
			return
		}

		// Attempt to raise soft to target.
		newRL := syscall.Rlimit{Cur: target, Max: originalHard}
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &newRL); err != nil {
			// PL-014a says "MUST log a warning on failure" — the daemon continues.
			// In a test, we just log and skip the post-setrlimit assertion.
			t.Logf("PL-014a setrlimit: Setrlimit(%d) failed (non-fatal per spec): %v", target, err)
			return
		}
		t.Cleanup(func() {
			// Restore original rlimit.
			restore := syscall.Rlimit{Cur: originalSoft, Max: originalHard}
			_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &restore) //nolint:errcheck // cleanup error unactionable
		})

		// Verify the soft limit was raised.
		var after syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &after); err != nil {
			t.Fatalf("PL-014a setrlimit: Getrlimit after raise: %v", err)
		}
		if after.Cur < target {
			t.Errorf("PL-014a setrlimit: soft limit after raise = %d, want ≥ %d", after.Cur, target)
		}

		// The hard limit must not have decreased.
		if after.Max < originalHard {
			t.Errorf("PL-014a setrlimit: hard limit decreased %d → %d; MUST NOT change", originalHard, after.Max)
		}
	})

	t.Run("target-soft/target-is-min-of-4096-and-hard", func(t *testing.T) {
		t.Parallel()

		// When hard < 4096, target = hard.
		if got := supervisionFixtureTargetSoftNofile(256); got != 256 {
			t.Errorf("supervisionFixtureTargetSoftNofile(hard=256) = %d, want 256", got)
		}
		// When hard >= 4096, target = 4096.
		if got := supervisionFixtureTargetSoftNofile(65536); got != supervisionFixtureMinNofile {
			t.Errorf("supervisionFixtureTargetSoftNofile(hard=65536) = %d, want %d", got, supervisionFixtureMinNofile)
		}
		// When hard == 4096, target = 4096.
		if got := supervisionFixtureTargetSoftNofile(4096); got != 4096 {
			t.Errorf("supervisionFixtureTargetSoftNofile(hard=4096) = %d, want 4096", got)
		}
	})
}
