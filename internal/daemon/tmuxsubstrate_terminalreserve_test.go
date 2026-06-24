package daemon_test

// tmuxsubstrate_terminalreserve_test.go — regression tests for the terminal-node
// reserved-slot fix (hk-x882o).
//
// # The bug
//
// The spawn semaphore was a single, node-type-BLIND, GLOBAL pool. When cap=2 and
// two non-terminal sessions held both slots, the terminal (consolidate) node of a
// completed+reviewed run timed out after 2 min and the entire reviewed run was
// discarded. The self-diagnosis "possible slot leak" in the error string was
// misleading — there was no leak, only saturation.
//
// # Fix
//
// WithSpawnCap(n) now sizes the semaphore at n+1 and adds a second
// nonTerminalSem of size n. Non-terminal spawns must acquire BOTH semaphores
// (nonTerminalSem first, then spawnSem). Terminal spawns acquire only spawnSem,
// drawing from the reserved +1 slot that non-terminal sessions cannot occupy.
//
// # What is tested
//
//   - TestSpawnCap_TerminalNodeNotStarvedBySaturation: with cap non-terminal
//     sessions holding all cap slots, a terminal-marked spawn (in.Terminal=true)
//     acquires the reserved +1 slot and returns a live session within timeout.
//     This is the RED test: before the fix, SpawnWindow times out with
//     ErrSpawnCapTimeout. After the fix, it returns nil.
//
//   - TestSpawnCap_NonTerminalStillBlocksAtCap: a non-terminal spawn with all
//     cap slots held still times out with ErrSpawnCapTimeout. The reserved slot
//     must NOT widen the cap for ordinary nodes.
//
//   - TestSpawnCap_LeakInvariantUnchanged: adapter for the existing round-trip
//     guard — a sequence of acquire/Kill round-trips (non-terminal sessions)
//     returns SpawnSlotsInUse()==0 after each. The reserved slot must not leak.
//
// # Helper prefix
//
// Helpers use the prefix "terminalReserveFixture" per implementer-protocol.md.
//
// # Bead
//
//   - hk-x882o (terminal-node spawn-cap starvation)

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// terminalReserveFixtureAdapter is a concurrency-safe fake tmux adapter for the
// terminal-reserve tests. NewWindowIn always succeeds with a unique handle.
type terminalReserveFixtureAdapter struct {
	mu          sync.Mutex
	windowCount int
}

func (a *terminalReserveFixtureAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *terminalReserveFixtureAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}
func (a *terminalReserveFixtureAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (a *terminalReserveFixtureAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.windowCount++
	return tmux.Outcome{Handle: tmux.WindowHandle("termres-session:win" + string(rune('a'+a.windowCount%26)))}
}
func (a *terminalReserveFixtureAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error {
	return nil
}
func (a *terminalReserveFixtureAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return 0, nil
}
func (a *terminalReserveFixtureAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *terminalReserveFixtureAdapter) KillSession(_ context.Context, _ string) error { return nil }
func (a *terminalReserveFixtureAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (a *terminalReserveFixtureAdapter) PasteBuffer(_ context.Context, _, _ string) error {
	return nil
}
func (a *terminalReserveFixtureAdapter) SendKeysLiteral(_ context.Context, _, _ string) error {
	return nil
}
func (a *terminalReserveFixtureAdapter) SendKeysEnter(_ context.Context, _ string) error { return nil }
func (a *terminalReserveFixtureAdapter) SendKeysQuit(_ context.Context, _ string) error  { return nil }
func (a *terminalReserveFixtureAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ tmux.Adapter = (*terminalReserveFixtureAdapter)(nil)

func terminalReserveFixtureSpawnNonTerminal(ctx context.Context, sub handler.Substrate) (handler.SubstrateSession, error) {
	return sub.SpawnWindow(ctx, handler.SubstrateSpawn{
		Argv:       []string{"claude"},
		WindowName: "hk-termres-nonterminal",
		Terminal:   false,
	})
}

func terminalReserveFixtureSpawnTerminal(ctx context.Context, sub handler.Substrate) (handler.SubstrateSession, error) {
	return sub.SpawnWindow(ctx, handler.SubstrateSpawn{
		Argv:       []string{"claude"},
		WindowName: "hk-termres-terminal",
		Terminal:   true,
	})
}

// TestSpawnCap_TerminalNodeNotStarvedBySaturation reproduces the hk-x882o
// incident: cap=2, two non-terminal sessions hold both slots. Before the fix,
// a terminal spawn times out with ErrSpawnCapTimeout. After the fix, it draws
// from the reserved +1 slot and returns a live session within timeout.
func TestSpawnCap_TerminalNodeNotStarvedBySaturation(t *testing.T) {
	t.Parallel()

	adapter := &terminalReserveFixtureAdapter{}
	const capN = 2
	// Short acquire timeout so the test is fast; production default is minutes.
	sub := daemon.NewTmuxSubstrate(adapter, "termres-session",
		daemon.WithSpawnCap(capN),
		daemon.WithSpawnAcquireTimeout(200*time.Millisecond))

	ctx := context.Background()

	// Saturate the pool by acquiring all cap non-terminal slots and HOLDING them
	// (not killing — mimics two concurrent DOT runs at their implementer nodes).
	for i := 0; i < capN; i++ {
		if _, err := terminalReserveFixtureSpawnNonTerminal(ctx, sub); err != nil {
			t.Fatalf("saturating non-terminal spawn %d failed: %v", i, err)
		}
	}

	// Both non-terminal slots are in use.
	if got := daemon.ExportedSpawnSlotsInUse(sub); got != capN {
		t.Fatalf("pool not saturated: SpawnSlotsInUse()=%d want %d", got, capN)
	}

	// A terminal spawn MUST succeed within the acquire timeout: the reserved +1
	// slot is available exclusively for terminal nodes.
	done := make(chan error, 1)
	go func() {
		_, err := terminalReserveFixtureSpawnTerminal(ctx, sub)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			// Pre-fix: ErrSpawnCapTimeout. Post-fix: nil (reserved slot acquired).
			t.Fatalf("terminal spawn failed when cap was saturated by non-terminal sessions: %v\n"+
				"(want nil — reserved +1 slot should be available for terminal/consolidate nodes)",
				err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("terminal SpawnWindow blocked indefinitely — terminal-reserve fix NOT applied (hk-x882o)")
	}
}

// TestSpawnCap_NonTerminalStillBlocksAtCap verifies that the reserved slot does
// NOT widen the effective cap for ordinary non-terminal spawns. With all cap
// slots held by non-terminal sessions, a new non-terminal spawn must block and
// eventually time out with ErrSpawnCapTimeout.
func TestSpawnCap_NonTerminalStillBlocksAtCap(t *testing.T) {
	t.Parallel()

	adapter := &terminalReserveFixtureAdapter{}
	const capN = 2
	sub := daemon.NewTmuxSubstrate(adapter, "termres-session",
		daemon.WithSpawnCap(capN),
		daemon.WithSpawnAcquireTimeout(200*time.Millisecond))

	ctx := context.Background()

	// Saturate the pool with cap non-terminal sessions (no Kill).
	for i := 0; i < capN; i++ {
		if _, err := terminalReserveFixtureSpawnNonTerminal(ctx, sub); err != nil {
			t.Fatalf("saturating spawn %d failed: %v", i, err)
		}
	}

	// A new non-terminal spawn must time out — the reserved slot is terminal-only.
	done := make(chan error, 1)
	go func() {
		_, err := terminalReserveFixtureSpawnNonTerminal(ctx, sub)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("non-terminal spawn succeeded when cap was full — reserved slot leaked to non-terminal")
		}
		if !errors.Is(err, daemon.ErrSpawnCapTimeout) {
			t.Errorf("expected ErrSpawnCapTimeout, got: %v", err)
		}
		if !errors.Is(err, handler.ErrStructural) {
			t.Errorf("expected error to wrap ErrStructural, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("non-terminal SpawnWindow did not time out — ErrSpawnCapTimeout never fired")
	}
}

// TestSpawnCap_LeakInvariantUnchanged asserts that the two-semaphore design does
// not introduce a slot leak: SpawnSlotsInUse() returns 0 after every acquire/Kill
// round-trip, regardless of whether the session was terminal or non-terminal.
func TestSpawnCap_LeakInvariantUnchanged(t *testing.T) {
	t.Parallel()

	adapter := &terminalReserveFixtureAdapter{}
	const capN = 2
	sub := daemon.NewTmuxSubstrate(adapter, "termres-session",
		daemon.WithSpawnCap(capN),
		daemon.WithSpawnAcquireTimeout(2*time.Second))

	ctx := context.Background()

	for round := 0; round < 10; round++ {
		// Non-terminal round-trip.
		sess, err := terminalReserveFixtureSpawnNonTerminal(ctx, sub)
		if err != nil {
			t.Fatalf("round %d: non-terminal spawn failed: %v", round, err)
		}
		if err := sess.Kill(ctx); err != nil {
			t.Fatalf("round %d: non-terminal Kill: %v", round, err)
		}
		if got := daemon.ExportedSpawnSlotsInUse(sub); got != 0 {
			t.Fatalf("round %d: after non-terminal Kill, SpawnSlotsInUse()=%d want 0 — slot leaked", round, got)
		}

		// Terminal round-trip.
		tSess, err := terminalReserveFixtureSpawnTerminal(ctx, sub)
		if err != nil {
			t.Fatalf("round %d: terminal spawn failed: %v", round, err)
		}
		if err := tSess.Kill(ctx); err != nil {
			t.Fatalf("round %d: terminal Kill: %v", round, err)
		}
		if got := daemon.ExportedSpawnSlotsInUse(sub); got != 0 {
			t.Fatalf("round %d: after terminal Kill, SpawnSlotsInUse()=%d want 0 — slot leaked", round, got)
		}
	}
}
