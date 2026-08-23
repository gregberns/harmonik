package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/workers"
)

type runplanacqReopen struct {
	beadID core.BeadID
	reason string
}

type runplanacqLedger struct {
	mu      sync.Mutex
	reopens []runplanacqReopen
}

func (l *runplanacqLedger) Ready(context.Context) ([]core.BeadRecord, error) { return nil, nil }

func (l *runplanacqLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id}, nil
}

func (l *runplanacqLedger) ClaimBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID) error {
	return nil
}

func (l *runplanacqLedger) CloseBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID, bool) error {
	return nil
}

func (l *runplanacqLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, reason string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reopens = append(l.reopens, runplanacqReopen{beadID: beadID, reason: reason})
	return nil
}

func (l *runplanacqLedger) calls() []runplanacqReopen {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]runplanacqReopen, len(l.reopens))
	copy(out, l.reopens)
	return out
}

func runplanacqSealedRegistry(t *testing.T) *handlercontract.AdapterRegistry {
	t.Helper()
	reg := handlercontract.NewAdapterRegistry()
	if err := handler.Register(reg); err != nil {
		t.Fatalf("runplanacq: register claude adapter: %v", err)
	}
	_, _ = reg.ForAgent(core.AgentTypeClaudeCode) //nolint:errcheck // called for its sealing effect; the returned adapter is not used
	return reg
}

func runplanacqRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("runplanacqRepo: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("acquisition-invariant fixture\n"), 0o600); err != nil {
		t.Fatalf("runplanacqRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	return dir
}

func runplanacqBody(yaml string) string {
	return "Probe bead.\n\n## Branching\n\n```yaml\n" + yaml + "\n```\n"
}

// TestRunPlan_EveryRefusalTakesNothing drives beadRunOne once per refusal
// verdict the plan can produce, with a worker slot already reserved, and
// asserts three things each time: the bead was reopened, the reserved slot came
// back, and nothing was acquired on the way out.
func TestRunPlan_EveryRefusalTakesNothing(t *testing.T) {
	piCfg := projectconfig.ProjectConfig{
		Harnesses: projectconfig.HarnessesConfig{
			Pi: projectconfig.PiHarnessConfig{
				Profiles: map[string]projectconfig.PiProfileConfig{
					"real-profile": {
						Provider:  "runplanacq-provider",
						Model:     "runplanacq-provider/some-id",
						APIKeyEnv: "RUNPLANACQ_PI_KEY",
					},
				},
			},
		},
	}

	for _, tc := range []struct {
		name string
		// verdict is the plan verdict this case is built to provoke. It is
		// named so a reader can see the table covers every refusal constant.
		verdict runPlanVerdict
		labels  []string
		// body builds the bead description from the project dir, because the
		// cross-repo case has to name a path.
		body func(projectDir string) string
		// harness is the tier-4 global default.
		harness         core.AgentType
		protectBranches []string
		// wantReason is a substring the reopen reason must carry.
		wantReason string
	}{
		{
			name:       "unknown pi profile",
			verdict:    runPlanRefusedPiProfile,
			labels:     []string{"profile:does-not-exist"},
			harness:    core.AgentTypePi,
			wantReason: "does-not-exist",
		},
		{
			name:    "target repo outside the safelist",
			verdict: runPlanRefusedCrossRepoUnsafe,
			body: func(string) string {
				return runplanacqBody("target_repo: /nonexistent/not-on-the-safelist")
			},
			wantReason: "allowed_repos",
		},
		{
			name:       "start_from names a ref that does not resolve",
			verdict:    runPlanRefusedStartFrom,
			body:       func(string) string { return runplanacqBody("start_from: no-such-ref") },
			wantReason: "resolve start_from failed",
		},
		{
			name:            "lands_on is a protected branch",
			verdict:         runPlanRefusedLandsOnProtected,
			body:            func(string) string { return runplanacqBody("target_branch: main") },
			protectBranches: []string{"main"},
			wantReason:      "protected branch",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := runplanacqRepo(t)

			reg := workers.NewRegistry(workers.Config{Workers: []workers.Worker{{
				Name:     "runplanacq-worker",
				Host:     "runplanacq.example.com",
				Enabled:  true,
				MaxSlots: 1,
			}}})
			preSelected := reg.SelectWorker()
			if preSelected == nil {
				t.Fatal("setup: SelectWorker returned nil; expected a reserved slot")
			}
			if got := reg.InFlight(); got != 1 {
				t.Fatalf("setup: InFlight = %d; want 1", got)
			}

			var worktreeCreated bool
			worktreeFactory := func(context.Context, string, string, string) (string, func(), error) {
				worktreeCreated = true
				t.Error("worktree factory ran on a refused bead: the refusal moved BELOW an acquisition")
				return "", func() {}, nil
			}

			ledger := &runplanacqLedger{}
			body := ""
			if tc.body != nil {
				body = tc.body(projectDir)
			}

			deps := ExportedTestRuntime(TestRuntimeParams{
				BrAdapter:        ledger,
				Bus:              eventbus.NewBusImpl(),
				ProjectDir:       projectDir,
				HandlerBinary:    "/bin/sh",
				HandlerArgs:      []string{"-c", "exit 0"},
				IntentLogDir:     t.TempDir(),
				MaxConcurrent:    1,
				AdapterRegistry2: runplanacqSealedRegistry(t),
				ProjectCfg:       piCfg,
				DefaultHarness:   tc.harness,
				WorkerRegistry:   reg,
				WorktreeFactory:  worktreeFactory,
				TargetBranch:     "main",
				ProtectBranches:  tc.protectBranches,
			})

			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			runID := core.RunID(uuid.New())
			bead := core.BeadRecord{
				BeadID:      core.BeadID("hk-runplanacq-probe"),
				Title:       "refuse-takes-nothing probe",
				BeadType:    "task",
				Status:      core.CoarseStatusOpen,
				Labels:      tc.labels,
				Description: body,
			}

			env := deps.runEnv(runID, bead, "", "", core.AgentType(""))
			succeeded := runBeadOneTest(ctx, deps, env, "", preSelected, false)

			calls := ledger.calls()
			if len(calls) != 1 {
				t.Fatalf("ReopenBead call count = %d; want exactly 1 (the %s refusal)\ncalls=%+v",
					len(calls), tc.verdict, calls)
			}
			if calls[0].beadID != bead.BeadID {
				t.Errorf("ReopenBead beadID = %q; want %q", calls[0].beadID, bead.BeadID)
			}
			if !strings.Contains(calls[0].reason, tc.wantReason) {
				t.Errorf("ReopenBead reason = %q; want it to contain %q", calls[0].reason, tc.wantReason)
			}
			if succeeded {
				t.Error("beadRunOne reported success for a refused bead")
			}

			if worktreeCreated {
				t.Error("a worktree was created for a refused bead")
			}
			if got := reg.InFlight(); got != 0 {
				t.Fatalf("InFlight after the %s refusal = %d; want 0. A refused bead must return its "+
					"pre-reserved worker slot — a non-zero count is the leak that wedged the remote path "+
					"once the registry ran out of slots", tc.verdict, got)
			}
			if !reg.HasFreeSlot() {
				t.Fatal("HasFreeSlot after the refusal = false; the slot was not returned to the pool")
			}
		})
	}
}

// TestRunPlan_ResolvingAPlanAcquiresNothing states the invariant at the seam
// itself rather than through beadRunOne: resolving a plan for a bead that will
// be dispatched to a worker leaves the worker registry untouched. The plan
// carries placement intent. The placement decision and its reservation stay
// together in the scheduler, in one critical section.
func TestRunPlan_ResolvingAPlanAcquiresNothing(t *testing.T) {
	t.Parallel()
	projectDir := runplanacqRepo(t)

	reg := workers.NewRegistry(workers.Config{Workers: []workers.Worker{{
		Name:     "runplanacq-untouched",
		Host:     "runplanacq.example.com",
		Enabled:  true,
		MaxSlots: 2,
	}}})
	before := reg.InFlight()

	env := runplanEnv(projectDir, runplanBead(nil, ""))
	env.ItemWorkerTarget = "runplanacq-untouched"

	plan, _, _ := runplanResolve(t, env)

	if plan.Verdict != runPlanReady {
		t.Fatalf("Verdict = %q; want ready (%+v)", plan.Verdict, plan.Refusal)
	}
	if plan.WorkerTarget != "runplanacq-untouched" {
		t.Errorf("WorkerTarget = %q; want the intent carried through", plan.WorkerTarget)
	}
	if got := reg.InFlight(); got != before {
		t.Fatalf("InFlight = %d; want %d. Resolving a plan must not reserve a worker — "+
			"selection and reservation belong together in the scheduler's critical section", got, before)
	}
}
