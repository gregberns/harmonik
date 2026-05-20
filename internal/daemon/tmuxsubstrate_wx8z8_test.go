// Package daemon — regression test for hk-wx8z8 (parallel pane allocation).
//
// Root cause: tmuxSubstrate held a single substrate-wide pair of
// lastHandle/lastPaneID, mutated by every SpawnWindow. Concurrent SpawnWindow
// calls from `harmonik run --beads ... --max-concurrent 2` raced to overwrite
// these fields, so both sessions' subsequent WriteLastPane / SendQuitToLastPane
// targeted the same pane — the one whose SpawnWindow happened to win the
// store race. The pasted text from the two claude sessions collided in a
// single pane and neither bead ever committed.
//
// Fix: tmuxSubstrateSession now carries its own paneID, captured atomically
// at SpawnWindow time (via outcome.PaneID from `tmux new-window -P -F
// "#{pane_id}"`), and exposes WritePane / SendEnter / SendQuit methods that
// read only the per-session fields. The workloop and reviewloop now route
// paste-inject and quit-on-commit through the per-session path
// (extractPaneSession + pasteInjectOnLaunchSession +
// pasteInjectQuitOnCommitSession).
//
// This test exercises N concurrent SpawnWindow + WritePane sequences against
// a single substrate and asserts that each session writes to its own pane.
//
// Bead: hk-wx8z8.
package daemon

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// wx8z8FixtureAdapter is a concurrent-safe fake tmux.Adapter that hands out
// distinct pane IDs to each NewWindowIn caller and records every WriteToPane
// call as a (paneTarget, payload) pair.
type wx8z8FixtureAdapter struct {
	mu sync.Mutex

	// nextPaneIdx is the next pane index to allocate; incremented per call.
	nextPaneIdx int

	// writeCalls records every WriteToPane invocation. Indexed by call order;
	// the test asserts on the set of distinct paneTargets, not the order.
	writeCalls []wx8z8WriteCall
}

type wx8z8WriteCall struct {
	paneTarget string
	payload    string
}

// NewWindowIn returns successive pane IDs "%100", "%101", ... The Handle is a
// slash-bearing path to mirror production (worktree paths include slashes).
// Holds the mutex briefly so concurrent callers observe distinct pane IDs.
func (f *wx8z8FixtureAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	f.mu.Lock()
	idx := f.nextPaneIdx
	f.nextPaneIdx++
	f.mu.Unlock()
	paneID := fmt.Sprintf("%%%d", 100+idx)
	handle := tmux.WindowHandle(fmt.Sprintf("test-session:/wt/run-%d", idx))
	return tmux.Outcome{Handle: handle, PaneID: paneID}
}

// WriteToPane records each call. paneTarget MUST equal the per-session paneID
// captured at SpawnWindow time, not the substrate-shared lastPaneID.
func (f *wx8z8FixtureAdapter) WriteToPane(_ context.Context, _, paneTarget string, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeCalls = append(f.writeCalls, wx8z8WriteCall{paneTarget: paneTarget, payload: string(payload)})
	return nil
}

// SendKeysEnter is a no-op stub; the test only asserts WritePane routing.
func (f *wx8z8FixtureAdapter) SendKeysEnter(_ context.Context, _ string) error { return nil }

// SendKeysQuit records a synthetic write so quit routing can be asserted by
// the test's secondary sub-test if it chooses; for the core test it is unused.
func (f *wx8z8FixtureAdapter) SendKeysQuit(_ context.Context, paneTarget string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeCalls = append(f.writeCalls, wx8z8WriteCall{paneTarget: paneTarget, payload: "<quit>"})
	return nil
}

// Stub implementations for unused Adapter methods.
func (f *wx8z8FixtureAdapter) ProbeTmux(_ context.Context) error                { return nil }
func (f *wx8z8FixtureAdapter) ListSessions(_ context.Context) ([]string, error) { return nil, nil }
func (f *wx8z8FixtureAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (f *wx8z8FixtureAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error { return nil }
func (f *wx8z8FixtureAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return 0, nil
}
func (f *wx8z8FixtureAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "", nil
}
func (f *wx8z8FixtureAdapter) KillSession(_ context.Context, _ string) error { return nil }
func (f *wx8z8FixtureAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (f *wx8z8FixtureAdapter) PasteBuffer(_ context.Context, _, _ string) error { return nil }
func (f *wx8z8FixtureAdapter) SendKeysLiteral(_ context.Context, _, _ string) error {
	return nil
}

var _ tmux.Adapter = (*wx8z8FixtureAdapter)(nil)

// TestParallelPaneAllocation_Distinct is the core regression test for hk-wx8z8.
//
// It spawns N=4 sessions concurrently against a single tmuxSubstrate, then has
// each session perform a WritePane with a session-distinct payload, and asserts:
//
//  1. The substrate handed each session a distinct paneID at SpawnWindow time.
//  2. Each session's WritePane targeted exactly that session's paneID — i.e. no
//     two sessions' writes shared a pane target.
//
// Before the fix (substrate-level lastPaneID): the last SpawnWindow to write
// would overwrite the shared field and all subsequent (or even prior, depending
// on timing) WriteLastPane calls would target a single pane. With per-session
// paneID, each call routes correctly.
func TestParallelPaneAllocation_Distinct(t *testing.T) {
	t.Parallel()

	const numSessions = 4
	fake := &wx8z8FixtureAdapter{}
	substrate := NewTmuxSubstrate(fake, "test-session")

	// Spawn N sessions concurrently. Each goroutine: SpawnWindow → WritePane.
	// Using a tight start-gate (start chan) so all goroutines race the spawn.
	start := make(chan struct{})
	var wg sync.WaitGroup
	sessions := make([]handler.SubstrateSession, numSessions)
	spawnErrs := make([]error, numSessions)

	for i := 0; i < numSessions; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			spawn := handler.SubstrateSpawn{
				WindowName: fmt.Sprintf("/wt/run-%d", i),
				Cwd:        t.TempDir(),
				Argv:       []string{"claude"},
			}
			sess, err := substrate.SpawnWindow(t.Context(), spawn)
			if err != nil {
				spawnErrs[i] = err
				return
			}
			sessions[i] = sess
			// Each session writes a session-distinct payload to its own pane.
			ps, ok := sess.(paneSession)
			if !ok {
				spawnErrs[i] = fmt.Errorf("session %d does not implement paneSession", i)
				return
			}
			bufName := fmt.Sprintf("harmonik-sess-%d-task", i)
			payload := fmt.Sprintf("payload-from-session-%d", i)
			if werr := ps.WritePane(t.Context(), bufName, []byte(payload)); werr != nil {
				spawnErrs[i] = fmt.Errorf("session %d WritePane: %w", i, werr)
				return
			}
		}()
	}
	close(start)
	wg.Wait()

	// Surface any per-goroutine errors.
	for i, err := range spawnErrs {
		if err != nil {
			t.Fatalf("session %d: %v", i, err)
		}
	}

	// Collect each session's paneID (the per-session field captured at spawn).
	gotPaneIDs := make([]string, numSessions)
	for i, sess := range sessions {
		tss, ok := sess.(*tmuxSubstrateSession)
		if !ok {
			t.Fatalf("session %d: not a *tmuxSubstrateSession", i)
		}
		gotPaneIDs[i] = tss.paneID
	}

	// Invariant 1: every session has a distinct paneID.
	seen := make(map[string]int)
	for i, pid := range gotPaneIDs {
		if pid == "" {
			t.Errorf("session %d: paneID is empty (expected non-empty for atomic capture)", i)
			continue
		}
		if prev, dup := seen[pid]; dup {
			t.Errorf("pane collision: session %d and session %d both have paneID %q (parallel allocator must hand out distinct panes)",
				prev, i, pid)
		}
		seen[pid] = i
	}

	// Invariant 2: each session's WritePane targeted that session's paneID.
	// Build a map from payload → paneTarget so we can verify routing without
	// depending on goroutine completion order.
	if len(fake.writeCalls) != numSessions {
		t.Fatalf("WriteToPane call count = %d; want %d", len(fake.writeCalls), numSessions)
	}
	payloadToTarget := make(map[string]string, numSessions)
	for _, c := range fake.writeCalls {
		payloadToTarget[c.payload] = c.paneTarget
	}
	for i, paneID := range gotPaneIDs {
		expectedPayload := fmt.Sprintf("payload-from-session-%d", i)
		gotTarget, ok := payloadToTarget[expectedPayload]
		if !ok {
			t.Errorf("session %d: no WriteToPane call observed for payload %q", i, expectedPayload)
			continue
		}
		if gotTarget != paneID {
			t.Errorf("session %d: WritePane targeted pane %q; want %q (per-session paneID — concurrent sessions must NOT share a pane target)",
				i, gotTarget, paneID)
		}
	}
}

// TestParallelPaneAllocation_PerSessionImmutable asserts that a second
// SpawnWindow on the same substrate does NOT mutate the first session's
// captured paneID. This is the structural invariant that makes the parallel
// fix work: the per-session field is immutable after SpawnWindow returns.
func TestParallelPaneAllocation_PerSessionImmutable(t *testing.T) {
	t.Parallel()

	fake := &wx8z8FixtureAdapter{}
	substrate := NewTmuxSubstrate(fake, "test-session")

	// Spawn session A.
	sessA, err := substrate.SpawnWindow(t.Context(), handler.SubstrateSpawn{
		WindowName: "/wt/runA", Cwd: t.TempDir(), Argv: []string{"claude"},
	})
	if err != nil {
		t.Fatalf("spawn A: %v", err)
	}
	tssA := sessA.(*tmuxSubstrateSession)
	paneA := tssA.paneID
	if paneA == "" {
		t.Fatal("session A: paneID empty after spawn")
	}

	// Spawn session B — should get a different paneID.
	sessB, err := substrate.SpawnWindow(t.Context(), handler.SubstrateSpawn{
		WindowName: "/wt/runB", Cwd: t.TempDir(), Argv: []string{"claude"},
	})
	if err != nil {
		t.Fatalf("spawn B: %v", err)
	}
	tssB := sessB.(*tmuxSubstrateSession)
	paneB := tssB.paneID
	if paneB == "" {
		t.Fatal("session B: paneID empty after spawn")
	}
	if paneA == paneB {
		t.Fatalf("paneA = paneB = %q (must be distinct)", paneA)
	}

	// Critically: session A's paneID is unchanged by session B's spawn.
	if tssA.paneID != paneA {
		t.Errorf("session A paneID mutated by session B spawn: was %q, now %q", paneA, tssA.paneID)
	}

	// And session A's WritePane still routes to its own pane.
	psA := sessA.(paneSession)
	if werr := psA.WritePane(t.Context(), "harmonik-A-task", []byte("hello-A")); werr != nil {
		t.Fatalf("session A WritePane: %v", werr)
	}
	last := fake.writeCalls[len(fake.writeCalls)-1]
	if last.paneTarget != paneA {
		t.Errorf("session A WritePane after B spawn: target = %q; want %q (session pane is immutable)",
			last.paneTarget, paneA)
	}
}
