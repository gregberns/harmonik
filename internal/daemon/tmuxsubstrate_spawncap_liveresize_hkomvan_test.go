package daemon_test

// tmuxsubstrate_spawncap_liveresize_hkomvan_test.go — regression tests for the
// live-resizable spawn cap (hk-omvan, follow-up to hk-vfeeo).
//
// # Background
//
// Before this bead, WithSpawnCap(n) built a fixed-size buffered channel once
// at construction; raising the cap required a daemon restart with a higher
// --max-concurrent. hk-omvan replaces the channel with a resizableSemaphore
// (mu + capacity + inUse + sync.Cond) and adds SetSpawnCap(n), so the cap can
// grow (or shrink) live.
//
// # What is tested
//
//   - TestSetSpawnCap_RaisesCeiling: SetSpawnCap(n) raises SpawnCapSize() to n
//     with no restart.
//   - TestSetSpawnCap_UnblocksWaitingSpawn: a non-terminal spawn blocked on a
//     saturated cap succeeds as soon as SetSpawnCap raises the ceiling — it
//     does not have to wait for an unrelated slot to free up first.
//   - TestSetSpawnCap_NoopWhenNoCapConfigured: SetSpawnCap on a substrate built
//     without WithSpawnCap is a no-op (SpawnCapSize stays 0).
//   - TestSetSpawnCap_NoopForNonPositive: SetSpawnCap(0) / SetSpawnCap(-1)
//     leave the existing cap unchanged.
//
// # Bead
//
//   - hk-omvan (follow-up to hk-vfeeo)

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// TestSetSpawnCap_RaisesCeiling verifies that SetSpawnCap(n) raises
// SpawnCapSize() to n immediately, with no daemon restart.
func TestSetSpawnCap_RaisesCeiling(t *testing.T) {
	t.Parallel()

	adapter := &terminalReserveFixtureAdapter{}
	sub := daemon.NewTmuxSubstrate(adapter, "liveresize-session", daemon.WithSpawnCap(2))

	if got := daemon.ExportedSpawnCapSize(sub); got != 2 {
		t.Fatalf("initial SpawnCapSize() = %d, want 2", got)
	}

	daemon.ExportedSetSpawnCap(sub, 5)

	if got := daemon.ExportedSpawnCapSize(sub); got != 5 {
		t.Fatalf("SpawnCapSize() after SetSpawnCap(5) = %d, want 5", got)
	}
}

// TestSetSpawnCap_UnblocksWaitingSpawn verifies that a non-terminal spawn
// blocked on a saturated cap succeeds as soon as SetSpawnCap raises the
// ceiling, without waiting for an unrelated slot to free up (SetCapacity
// broadcasts to every blocked Acquire).
func TestSetSpawnCap_UnblocksWaitingSpawn(t *testing.T) {
	t.Parallel()

	adapter := &terminalReserveFixtureAdapter{}
	const capN = 1
	sub := daemon.NewTmuxSubstrate(adapter, "liveresize-session",
		daemon.WithSpawnCap(capN),
		daemon.WithSpawnAcquireTimeout(10*time.Second)) // generous: SetSpawnCap should win, not the timeout

	ctx := context.Background()

	// Saturate the single non-terminal slot (held, not released).
	if _, err := terminalReserveFixtureSpawnNonTerminal(ctx, sub); err != nil {
		t.Fatalf("saturating spawn failed: %v", err)
	}
	if got := daemon.ExportedSpawnSlotsInUse(sub); got != capN {
		t.Fatalf("pool not saturated: SpawnSlotsInUse()=%d want %d", got, capN)
	}

	// A second non-terminal spawn now blocks against the saturated cap.
	done := make(chan error, 1)
	go func() {
		_, err := terminalReserveFixtureSpawnNonTerminal(ctx, sub)
		done <- err
	}()

	// Give the blocked spawn a moment to actually reach the semaphore wait.
	time.Sleep(50 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("second spawn returned before the cap was raised (err=%v) — test is not exercising the blocked path", err)
	default:
	}

	// Raise the cap: the blocked spawn must succeed promptly, not after the
	// full 10s acquire timeout.
	daemon.ExportedSetSpawnCap(sub, capN+1)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second spawn failed after SetSpawnCap raised the ceiling: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second spawn did not unblock promptly after SetSpawnCap raised the ceiling")
	}
}

// TestSetSpawnCap_NoopWhenNoCapConfigured verifies that SetSpawnCap is a no-op
// on a substrate constructed without WithSpawnCap.
func TestSetSpawnCap_NoopWhenNoCapConfigured(t *testing.T) {
	t.Parallel()

	adapter := &terminalReserveFixtureAdapter{}
	sub := daemon.NewTmuxSubstrate(adapter, "liveresize-session") // no WithSpawnCap

	daemon.ExportedSetSpawnCap(sub, 5)

	if got := daemon.ExportedSpawnCapSize(sub); got != 0 {
		t.Fatalf("SpawnCapSize() after SetSpawnCap on an uncapped substrate = %d, want 0", got)
	}
}

// TestSetSpawnCap_NoopForNonPositive verifies that SetSpawnCap(0) and
// SetSpawnCap(-1) leave the existing cap unchanged.
func TestSetSpawnCap_NoopForNonPositive(t *testing.T) {
	t.Parallel()

	adapter := &terminalReserveFixtureAdapter{}
	sub := daemon.NewTmuxSubstrate(adapter, "liveresize-session", daemon.WithSpawnCap(3))

	daemon.ExportedSetSpawnCap(sub, 0)
	if got := daemon.ExportedSpawnCapSize(sub); got != 3 {
		t.Fatalf("SpawnCapSize() after SetSpawnCap(0) = %d, want unchanged 3", got)
	}

	daemon.ExportedSetSpawnCap(sub, -1)
	if got := daemon.ExportedSpawnCapSize(sub); got != 3 {
		t.Fatalf("SpawnCapSize() after SetSpawnCap(-1) = %d, want unchanged 3", got)
	}
}
