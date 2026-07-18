package daemon

// hk-5h759 — FAIL-CLOSED codex isolation-boundary guard (white-box).
//
// A codex app-server crew runs with a permissive sandbox posture
// (danger-full-access) that is safe ONLY inside a real isolation boundary — an
// ENABLED, ssh-transport remote worker IS that boundary. The composition root
// sets Config.CodexRequireIsolationBoundary=true iff HARMONIK_SUBSTRATE=codexdriver.
// With it set, beadRunOne MUST refuse to launch UNLESS the shared registry's
// WorkerSnapshot() yields `w.Enabled && w.Transport == "ssh"` — the SAME question
// codexWorkerRoutingRunner.Command asks before routing to the worker. Any other
// state (no registry, no worker, disabled, or a non-ssh transport) means the
// runner would fall through to LocalRunner and run codex UNSANDBOXED on the
// daemon host, so the guard must refuse. rbc != nil is NOT a sufficient proxy:
// SelectWorker binds rbc without inspecting Transport, so an enabled non-ssh
// worker yields rbc != nil yet the runner still runs locally. Operator mandate:
// never a silent local fallback.
//
// This drives beadRunOne DIRECTLY (mirroring workloop_gate_n5md3_test.go /
// pi_unknown_profile_refuse_test.go): beadRunOne is unexported, so the file
// lives in package daemon. It reuses n5md3RepoWithCommit (pre-guard
// resolveParentCommit needs a real repo), n5md3SealedAdapterRegistry,
// n5md3Registry (a healthy ssh worker), and the hkppsNoLaunchLedger capturing
// brAdapter. The guard fires at workloop.go before worktree creation, so the
// refuse case needs no WorktreeFactory.
//
// Bead: hk-5h759.

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/workers"
)

// guardReason is the substring the guard names in its refuse reason.
const hk5h759GuardReasonSubstr = "isolation-boundary"

func TestCodexIsolationGuard_HK5H759(t *testing.T) {
	// Swap the reverse-tunnel constructor so the boundary-present case never
	// spawns a real `ssh -N -R`; the readiness gate still fails (nothing
	// listening), which is fine — we only assert the guard did NOT refuse.
	origRunner := reverseTunnelRunner
	t.Cleanup(func() { reverseTunnelRunner = origRunner })
	reverseTunnelRunner = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sleep", "30")
	}

	// A worktree factory that stops the run immediately AFTER the guard, so the
	// flag-off / baseline case reopens with a non-guard reason instead of doing
	// real worktree work.
	stopFactory := func(context.Context, string, string, string) (string, func(), error) {
		return "", nil, errN5md3StopAfterGate
	}

	projectDir := n5md3RepoWithCommit(t)
	adapterReg := n5md3SealedAdapterRegistry(t)

	// hk5h759DisabledSSHRegistry / hk5h759NonSSHRegistry build single-worker
	// registries whose WorkerSnapshot() fails the enabled+ssh predicate, so the
	// guard must refuse even though SelectWorker would still bind rbc for the
	// enabled non-ssh case.
	nonSSHReg := func(t *testing.T) *workers.Registry {
		return workers.NewRegistry(workers.Config{
			Version: 1,
			Workers: []workers.Worker{{
				Name:      "worker-hk5h759-local",
				Transport: "local", // enabled but NOT ssh → runner runs codex unsandboxed
				Host:      "",
				OS:        "darwin",
				RepoPath:  t.TempDir(),
				MaxSlots:  1,
				Enabled:   true,
			}},
		})
	}
	disabledReg := func(t *testing.T) *workers.Registry {
		return workers.NewRegistry(workers.Config{
			Version: 1,
			Workers: []workers.Worker{{
				Name:      "worker-hk5h759-disabled",
				Transport: "ssh",
				Host:      "worker-hk5h759.invalid",
				OS:        "darwin",
				RepoPath:  t.TempDir(),
				MaxSlots:  1,
				Enabled:   false, // ssh but disabled → not a live boundary
			}},
		})
	}

	cases := []struct {
		name         string
		requireBound bool
		regFn        func(t *testing.T) *workers.Registry // nil ⇒ no registry bound
		wantRefuse   bool                                 // guard fires → reopen reason names the isolation boundary
		why          string
	}{
		{
			name:         "codex_crew_no_registry_REFUSED",
			requireBound: true,
			regFn:        nil,
			wantRefuse:   true,
			why:          "codexdriver crew with no worker registry would run unsandboxed on the host → fail closed",
		},
		{
			name:         "codex_crew_nonssh_worker_REFUSED",
			requireBound: true,
			regFn:        nonSSHReg,
			wantRefuse:   true,
			why:          "an enabled non-ssh worker binds rbc but the runner runs codex LOCALLY (unsandboxed) → fail closed",
		},
		{
			name:         "codex_crew_disabled_worker_REFUSED",
			requireBound: true,
			regFn:        disabledReg,
			wantRefuse:   true,
			why:          "a disabled ssh worker is not a live boundary; WorkerSnapshot reports Enabled=false → fail closed",
		},
		{
			name:         "flag_off_no_guard_baseline",
			requireBound: false,
			regFn:        nil,
			wantRefuse:   false,
			why:          "the tmux/baseline path has no permissive posture; the guard must be byte-identical no-op",
		},
		{
			name:         "codex_crew_with_boundary_ALLOWED",
			requireBound: true,
			regFn:        n5md3Registry,
			wantRefuse:   false,
			why:          "an enabled ssh worker IS the isolation boundary; WorkerSnapshot reports Enabled+ssh → let the run proceed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ledger := &hkppsNoLaunchLedger{}

			var reg *workers.Registry
			if tc.regFn != nil {
				reg = tc.regFn(t)
			}

			deps := ExportedWorkLoopDeps(WorkLoopDepsParams{
				BrAdapter:                     ledger,
				Bus:                           eventbus.NewBusImpl(),
				ProjectDir:                    projectDir,
				HandlerBinary:                 "/bin/sh",
				HandlerArgs:                   []string{"-c", "exit 0"},
				IntentLogDir:                  t.TempDir(),
				MaxConcurrent:                 1,
				AdapterRegistry2:              adapterReg,
				WorktreeFactory:               stopFactory,
				WorkerRegistry:                reg,
				CodexRequireIsolationBoundary: tc.requireBound,
			})

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			runID := core.RunID(uuid.New())
			beadRecord := core.BeadRecord{
				BeadID:   core.BeadID("hk-5h759-guard-probe"),
				Title:    "codex isolation-boundary guard probe",
				BeadType: "task",
				Status:   core.CoarseStatusOpen,
			}

			beadRunOne(ctx, deps, runID, beadRecord,
				"", nil, nil, 0, "", "", "", nil,
				false, "", nil, false)

			calls := ledger.calls()
			// Every case reopens exactly once: the guard refuse, or (guard-passed)
			// the stopFactory / readiness-gate stop downstream.
			if len(calls) != 1 {
				t.Fatalf("%s: ReopenBead count = %d, want 1\ncalls=%+v\nwhy: %s", tc.name, len(calls), calls, tc.why)
			}
			gotGuardRefuse := strings.Contains(calls[0].reason, hk5h759GuardReasonSubstr)
			if gotGuardRefuse != tc.wantRefuse {
				t.Fatalf("%s: guard-refuse=%v, want %v\nreopen reason: %q\nwhy: %s",
					tc.name, gotGuardRefuse, tc.wantRefuse, calls[0].reason, tc.why)
			}
		})
	}
}
