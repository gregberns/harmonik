package daemon

// workloop_gate_n5md3_test.go — drives the per-item REMOTE-ROUTING GATE inside
// beadRunOne (workloop.go ~2980), closing the L5 test-altitude gap flagged by
// hk-f10xl / hk-n5md3 (codename:remote-test-pyramid).
//
// THE L5 GAP THIS CLOSES
// ----------------------
// The worker-selector property tests (internal/workers/routing_prop_test.go)
// exercise Registry.SelectWorker / SelectWorkerByName in ISOLATION. Nothing
// drove the ACTUAL gate that decides, per dispatched item, whether to call a
// selector at all and WHICH selector to call:
//
//	if !itemLocalOnly && deps.workerRegistry != nil {
//	    if itemWorkerTarget != "" { w = SelectWorkerByName(itemWorkerTarget) }
//	    else                      { w = SelectWorker() }
//	    if w != nil { rbc = …; defer ReleaseSlot(); <remote tunnel/launch> }
//	}
//
// This test drives THAT gate for the four (itemLocalOnly, itemWorkerTarget)
// shapes the gate must satisfy and asserts the branch the gate actually chose.
//
// SEAM CHOSEN (and why)
// ---------------------
// The brief's "strongly preferred recording fake of the workerRegistry
// INTERFACE" is NOT achievable: workLoopDeps.workerRegistry is a CONCRETE
// *workers.Registry (workloop.go:633), not an interface, so it cannot be
// replaced by a counting fake without editing production. Rather than refactor
// production to introduce an interface, this test drives the gate with a REAL
// *workers.Registry and observes the branch the gate chose through TWO
// production-observable seams — exactly the seams the brief calls out:
//
//   - REMOTE branch taken (selector returned non-nil → rbc != nil): the run
//     enters the reverse-tunnel setup and the readiness gate
//     (waitWorkerSocketLive) fails, which emits a `worker_tunnel_failed` event
//     carrying the SELECTED worker's name. Its presence proves the gate did NOT
//     skip and a slot was reserved; its worker_name proves WHICH worker (hence
//     which selector matched). The deferred ReleaseSlot then runs, so
//     Registry.InFlight() returns to 0 — proving the remote-only ReleaseSlot
//     fired.
//   - LOCAL branch taken (gate skipped, or selector returned nil → rbc stays
//     nil): NO worker_tunnel_failed is ever emitted (it lives only inside the
//     rbc != nil block) and the registry's slot count is never perturbed
//     (InFlight() stays 0 — no ReleaseSlot leak).
//
// To keep the REMOTE-taken cases deterministic and FAST without a real worker,
// two things are arranged: (1) the package-level `reverseTunnelRunner` seam is
// swapped for a no-op so no real `ssh -N -R` is spawned, and (2) the worker
// Host is an unresolvable `.invalid` name and the run ctx is short-bounded, so
// the readiness gate's connect-probe fails fast and the gate decision (the
// thing under test) is observed WITHOUT completing a heavy remote run — the
// approach the brief endorses for bounding the heavy remote path. The
// LOCAL-taken cases use a worktree factory that errors immediately after the
// gate, so they terminate instantly with no real launch.
//
// beadRunOne is called DIRECTLY (this is an in-package `daemon` test) with each
// (itemLocalOnly, itemWorkerTarget) combo: the smallest seam that reaches the
// gate, bypassing the whole work loop. No production code is modified.
//
// Bead: hk-n5md3 (codename:remote-test-pyramid). Refs the gate: hk-f10xl [L5].

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workers"
)

// errN5md3StopAfterGate is returned by the worktree factory so a run that took
// the LOCAL branch terminates immediately AFTER the gate decision (the factory
// is reached only on the local path, well past the gate at workloop.go ~3184).
// REMOTE-branch runs never reach the factory — they reopen at the readiness
// gate first — so this error is observed only by local-decision cases.
var errN5md3StopAfterGate = errors.New("n5md3: stop run immediately after the routing-gate decision")

// n5md3Collector is a minimal handlercontract.EventEmitter that records every
// emitted (eventType, payload) pair. Both Emit and EmitWithRunID funnel here so
// run_started / run_completed (EmitWithRunID) and worker_tunnel_failed (Emit)
// are all captured.
type n5md3Collector struct {
	mu     sync.Mutex
	events []n5md3Event
}

type n5md3Event struct {
	typ     core.EventType
	payload []byte
}

func (c *n5md3Collector) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	c.record(eventType, payload)
	return nil
}

func (c *n5md3Collector) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	c.record(eventType, payload)
	return nil
}

func (c *n5md3Collector) record(eventType core.EventType, payload []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]byte, len(payload))
	copy(cp, payload)
	c.events = append(c.events, n5md3Event{typ: eventType, payload: cp})
}

// tunnelFailedWorkerName scans for a worker_tunnel_failed event and returns its
// worker_name (ok=false when none was emitted — i.e. the gate chose LOCAL).
func (c *n5md3Collector) tunnelFailedWorkerName(t *testing.T) (name string, ok bool) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.events {
		if e.typ != core.EventTypeWorkerTunnelFailed {
			continue
		}
		var pl workers.WorkerTunnelFailedPayload
		if err := json.Unmarshal(e.payload, &pl); err != nil {
			t.Fatalf("n5md3: decode worker_tunnel_failed payload: %v\nraw: %s", err, e.payload)
		}
		return pl.WorkerName, true
	}
	return "", false
}

// n5md3Ledger is a no-op beadLedger: beadRunOne calls only ReopenBead (on the
// fast-fail paths these cases drive) and resolveOwningEpicFromRecord (which
// returns early for an edge-less record), so the other methods are inert.
type n5md3Ledger struct{}

func (n5md3Ledger) Ready(context.Context) ([]core.BeadRecord, error) { return nil, nil }
func (n5md3Ledger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id}, nil
}
func (n5md3Ledger) ClaimBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID) error {
	return nil
}
func (n5md3Ledger) CloseBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID, bool) error {
	return nil
}
func (n5md3Ledger) ReopenBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID, string) error {
	return nil
}

// n5md3Git runs a git command in dir, failing the test on error.
func n5md3Git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("n5md3Git: git %v (dir=%s): %v\n%s", args, dir, err, out)
	}
}

// n5md3RepoWithCommit creates a throwaway git repo on `main` with one commit so
// resolveParentCommit (which runs BEFORE the gate and resolves start_from→HEAD)
// succeeds and the run actually reaches the gate under test.
func n5md3RepoWithCommit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	n5md3Git(t, dir, "init", "--initial-branch=main")
	n5md3Git(t, dir, "config", "user.email", "daemon@harmonik.local")
	n5md3Git(t, dir, "config", "user.name", "Harmonik Test")
	//nolint:gosec // G306: test fixture file in a throwaway repo
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("init\n"), 0o644); err != nil {
		t.Fatalf("n5md3: write README: %v", err)
	}
	n5md3Git(t, dir, "add", "README")
	n5md3Git(t, dir, "commit", "-m", "init")
	return dir
}

// n5md3SealedAdapterRegistry mirrors NewSealedAdapterRegistryForTest (which
// lives in the external daemon_test package and is therefore unavailable here):
// register the real ClaudeCode adapter, then seal via ForAgent. beadRunOne's
// hk-d8u1y precondition requires a non-nil sealed registry.
func n5md3SealedAdapterRegistry(t *testing.T) *handlercontract.AdapterRegistry {
	t.Helper()
	reg := handlercontract.NewAdapterRegistry()
	if err := handler.Register(reg); err != nil {
		t.Fatalf("n5md3: register claude adapter: %v", err)
	}
	_, _ = reg.ForAgent(core.AgentTypeClaudeCode) // seal
	return reg
}

const n5md3WorkerName = "alpha"

// n5md3Registry builds a real single-worker registry whose worker is selectable
// (Enabled, a free slot) and named n5md3WorkerName. The Host is an unresolvable
// `.invalid` name so the remote readiness probe fails fast; combined with the
// no-op reverseTunnelRunner seam and a short ctx, the REMOTE branch is observed
// without a real worker or a slow network timeout.
func n5md3Registry(t *testing.T) *workers.Registry {
	t.Helper()
	return workers.NewRegistry(workers.Config{
		Version: 1,
		Workers: []workers.Worker{{
			Name:      n5md3WorkerName,
			Transport: "ssh",
			Host:      "worker-n5md3.invalid",
			OS:        "darwin",
			RepoPath:  t.TempDir(),
			MaxSlots:  1,
			Enabled:   true,
		}},
	})
}

// TestBeadRunOne_RoutingGate_N5md3 drives the remote-routing gate in beadRunOne
// for the four (itemLocalOnly, itemWorkerTarget) shapes and asserts the branch
// the gate chose via the worker_tunnel_failed seam (REMOTE) / its absence
// (LOCAL) plus the registry slot accounting (ReleaseSlot is remote-only).
//
// NOT parallel: it swaps the package-level reverseTunnelRunner seam.
func TestBeadRunOne_RoutingGate_N5md3(t *testing.T) {
	// Swap the reverse-tunnel constructor for a no-op so REMOTE-branch cases
	// never spawn a real `ssh -N -R`. The readiness gate still fails (nothing is
	// listening), which is exactly the observable we want.
	origRunner := reverseTunnelRunner
	t.Cleanup(func() { reverseTunnelRunner = origRunner })
	reverseTunnelRunner = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		// A trivially-startable, ctx-bounded process standing in for the tunnel.
		return exec.CommandContext(ctx, "sleep", "30")
	}

	// LOCAL-branch runs reach the worktree factory (after the gate) and stop
	// there immediately; REMOTE-branch runs reopen at the readiness gate first.
	stopFactory := func(context.Context, string, string, string) (string, func(), error) {
		return "", nil, errN5md3StopAfterGate
	}

	// expectRemote=true  → gate took the REMOTE branch with worker wantWorker.
	// expectRemote=false → gate took the LOCAL branch (skip or nil-fallback).
	cases := []struct {
		name          string
		hasRegistry   bool
		itemLocalOnly bool
		workerTarget  string
		expectRemote  bool
		wantWorker    string // worker_name expected on worker_tunnel_failed (remote only)
		why           string
	}{
		{
			name:          "LocalOnly_true_skips_gate",
			hasRegistry:   true, // a selectable worker IS present…
			itemLocalOnly: true, // …but the gate must be skipped entirely.
			workerTarget:  "",
			expectRemote:  false,
			why:           "itemLocalOnly=true must skip selection even when a worker is available (no SelectWorker/ByName, no ReleaseSlot)",
		},
		{
			name:         "Zero_value_calls_SelectWorker_remote",
			hasRegistry:  true,
			workerTarget: "", // empty target + !localOnly → SelectWorker()
			expectRemote: true,
			wantWorker:   n5md3WorkerName,
			why:          "zero-value (localOnly=false, target=\"\") must call SelectWorker() and route remote; a buggy SelectWorkerByName(\"\") would return nil → local",
		},
		{
			name:         "Target_match_calls_SelectWorkerByName_remote",
			hasRegistry:  true,
			workerTarget: n5md3WorkerName, // resolves to the worker → remote
			expectRemote: true,
			wantWorker:   n5md3WorkerName,
			why:          "non-empty target that matches must route via SelectWorkerByName(target) → remote",
		},
		{
			name:         "Target_miss_local_fallback",
			hasRegistry:  true,
			workerTarget: "ghost", // no worker named ghost → SelectWorkerByName returns nil
			expectRemote: false,
			why:          "non-empty target with no match must fall back LOCAL (no remote, no ReleaseSlot leak); a buggy SelectWorker() would pick alpha → remote",
		},
		{
			name:         "No_registry_local",
			hasRegistry:  false, // deps.workerRegistry == nil → gate condition false
			workerTarget: "",
			expectRemote: false,
			why:          "nil registry must short-circuit the gate to LOCAL (baseline)",
		},
	}

	projectDir := n5md3RepoWithCommit(t)
	adapterReg := n5md3SealedAdapterRegistry(t)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			collector := &n5md3Collector{}

			var reg *workers.Registry
			if tc.hasRegistry {
				reg = n5md3Registry(t)
			}

			deps := ExportedWorkLoopDeps(WorkLoopDepsParams{
				BrAdapter:        n5md3Ledger{},
				Bus:              collector,
				ProjectDir:       projectDir,
				HandlerBinary:    "/bin/sh",
				HandlerArgs:      []string{"-c", "exit 0"},
				IntentLogDir:     t.TempDir(),
				MaxConcurrent:    1,
				AdapterRegistry2: adapterReg,
				WorktreeFactory:  stopFactory,
				WorkerRegistry:   reg,
			})

			// Short ctx: ample headroom for the pre-gate resolveParentCommit git
			// fork to reach the gate even on a loaded -race CI box, and bounds the
			// REMOTE readiness probe so the gate decision is observed without a slow
			// timeout. 5s (not 2s) buys margin for the git fork under -race load;
			// remote cases consume the full ctx in the readiness loop, local cases
			// finish far sooner (the worktree factory errors immediately).
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			runID := core.RunID(uuid.New())
			beadRecord := core.BeadRecord{
				BeadID:   core.BeadID("hk-n5md3-gate"),
				Title:    "routing-gate probe",
				BeadType: "task",
				Status:   core.CoarseStatusOpen,
			}

			// Drive the gate directly with this case's (localOnly, target).
			beadRunOne(ctx, deps, runID, beadRecord,
				"", nil, nil, 0, nil, "", "", "", nil,
				tc.itemLocalOnly, tc.workerTarget)

			gotWorker, sawTunnelFailed := collector.tunnelFailedWorkerName(t)

			if tc.expectRemote {
				if !sawTunnelFailed {
					t.Fatalf("%s: expected a worker_tunnel_failed event (REMOTE branch taken), got none — gate did not route remote.\nwhy: %s", tc.name, tc.why)
				}
				if gotWorker != tc.wantWorker {
					t.Errorf("%s: worker_tunnel_failed.worker_name = %q, want %q — wrong worker selected.\nwhy: %s", tc.name, gotWorker, tc.wantWorker, tc.why)
				}
				// Remote branch reserved a slot then ran its deferred ReleaseSlot
				// on return → back to 0. Proves the remote-only ReleaseSlot fired
				// (no slot leak).
				if got := reg.InFlight(); got != 0 {
					t.Errorf("%s: registry InFlight()=%d after run, want 0 — deferred ReleaseSlot did not run", tc.name, got)
				}
			} else {
				if sawTunnelFailed {
					t.Fatalf("%s: unexpected worker_tunnel_failed (worker_name=%q) — gate entered the REMOTE branch but should have stayed LOCAL.\nwhy: %s", tc.name, gotWorker, tc.why)
				}
				if reg != nil {
					// No selection happened → no reservation → no ReleaseSlot
					// needed; the slot count must be pristine.
					if got := reg.InFlight(); got != 0 {
						t.Errorf("%s: registry InFlight()=%d, want 0 — local path must not reserve a slot", tc.name, got)
					}
				}
			}
		})
	}
}
