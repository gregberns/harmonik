package daemon

// runplan_precedence_test.go — the ten run-plan decisions, written as tables.
//
// Each decision in workloop_runplan.go is a precedence walk, and a precedence
// walk has a fixed set of shapes: the tier is absent, the tier holds exactly one
// valid input, the tier holds exactly one INVALID input, or the tier holds more
// than one input. The last two are where the walks disagree with each other —
// some report a conflict and continue, one refuses the bead, and one is
// deliberately silent because a second resolver reports the same conflict later.
// A table per decision is the only way to see that a walk covers all four
// shapes, and to see which ones emit.
//
// Every case asserts the EXACT event sequence, not just the resolved value.
// Adding an event to a refusal path is as much a behaviour change as changing
// what the path decides: operator tooling reads the stream.
//
// These are IN-PACKAGE (white-box) tests. resolveRunPlan is unexported and the
// point of the seam is that it stays that way — a test seam that exported the
// plan would let a caller resolve it twice.
//
// Helper prefix: runplan (implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/runloop"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// runplanBus captures every emitted event so a case can assert the exact
// sequence. resolveRunPlan runs on one goroutine, but the mutex costs nothing
// and keeps the fake safe if that ever stops being true.
type runplanBus struct {
	mu      sync.Mutex
	types   []core.EventType
	payload [][]byte
}

func (b *runplanBus) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.types = append(b.types, eventType)
	b.payload = append(b.payload, payload)
	return nil
}

func (b *runplanBus) EmitWithRunID(ctx context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	return b.Emit(ctx, eventType, payload)
}

// seen returns the emitted event types in order.
func (b *runplanBus) seen() []core.EventType {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]core.EventType, len(b.types))
	copy(out, b.types)
	return out
}

// firstPayload returns the payload of the first event of type want, or nil.
func (b *runplanBus) firstPayload(want core.EventType) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, typ := range b.types {
		if typ == want {
			return b.payload[i]
		}
	}
	return nil
}

// runplanHandle is a RunHandlePort that records the one write the plan makes.
type runplanHandle struct {
	provider    string
	providerSet bool
}

func (h *runplanHandle) SetOwningEpic(string, string) {}
func (h *runplanHandle) SetResolvedProvider(provider string) {
	h.provider, h.providerSet = provider, true
}
func (h *runplanHandle) SetRemote(bool)                  {}
func (h *runplanHandle) SetAgentType(core.AgentType)     {}
func (h *runplanHandle) SetMachine(*hclifecycle.Machine) {}
func (h *runplanHandle) Aborted() bool                   { return false }
func (h *runplanHandle) SetCapturedAgentOutput()         {}
func (h *runplanHandle) CapturedAgentOutput() bool       { return false }

// runplanRegistry hands out the one handle.
type runplanRegistry struct{ h *runplanHandle }

func (r runplanRegistry) Get(core.RunID) (runloop.RunHandlePort, bool) { return r.h, true }

// runplanRepo initialises a git repository with one commit on main and returns
// its path plus the commit SHA. Decisions 8 and 9 fork `git rev-parse`, so
// every case needs a real repository — there is no fake for a branch tip.
func runplanRepo(t *testing.T) (dir, headSHA string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("runplanRepo: git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("run-plan table fixture\n"), 0o600); err != nil {
		t.Fatalf("runplanRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	return dir, run("rev-parse", "HEAD")
}

// runplanSideBranch creates the branch named runplanSideBranchName at HEAD and
// returns its SHA. The cases that need a second branch all need the same one.
func runplanSideBranch(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "branch", runplanSideBranchName)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runplanSideBranch: git branch: %v\n%s", err, out)
	}
	shaCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "refs/heads/"+runplanSideBranchName)
	shaCmd.Dir = dir
	out, err := shaCmd.Output()
	if err != nil {
		t.Fatalf("runplanSideBranch: git rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// runplanSideBranchName is the non-default branch the branching cases resolve
// against.
const runplanSideBranchName = "side"

// runplanWriteFile writes rel under dir, creating parent directories.
func runplanWriteFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("runplanWriteFile: MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("runplanWriteFile: WriteFile: %v", err)
	}
}

// runplanBeadBody wraps yaml in the ## Branching section shape BI-009b defines.
func runplanBeadBody(yaml string) string {
	return "Some description.\n\n## Branching\n\n```yaml\n" + yaml + "\n```\n"
}

// runplanEnv builds the minimum RunEnv a plan needs: a project dir that is a
// git repository, a target branch, a run id, and the bead.
func runplanEnv(projectDir string, bead core.BeadRecord) runloop.RunEnv {
	return runloop.RunEnv{
		ProjectDir:   projectDir,
		TargetBranch: "main",
		RunID:        core.RunID(uuid.New()),
		BeadRecord:   bead,
	}
}

// runplanResolve resolves the plan and returns it with the bus that watched it.
func runplanResolve(t *testing.T, env runloop.RunEnv) (runPlan, *runplanBus, *runplanHandle) {
	t.Helper()
	bus := &runplanBus{}
	handle := &runplanHandle{}
	plan := resolveRunPlan(t.Context(), runPlanRequest{
		Env:     env,
		Emit:    bus,
		Handles: runloop.SharedHandles{RunRegistry: runplanRegistry{h: handle}},
	})
	return plan, bus, handle
}

// runplanWantEvents fails when got is not exactly want, in order.
func runplanWantEvents(t *testing.T, got, want []core.EventType) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("event sequence = %v; want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("event sequence = %v; want %v", got, want)
		}
	}
}

// runplanBead builds a bead record with labels and an optional body.
func runplanBead(labels []string, body string) core.BeadRecord {
	return core.BeadRecord{
		BeadID:      core.BeadID("hk-runplan-probe"),
		Title:       "run-plan table probe",
		BeadType:    "task",
		Status:      core.CoarseStatusOpen,
		Labels:      labels,
		Description: body,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Decision 1 — workflow mode (EM-012a)
// ─────────────────────────────────────────────────────────────────────────────

func TestRunPlan_WorkflowModePrecedence(t *testing.T) {
	t.Parallel()
	repo, _ := runplanRepo(t)

	for _, tc := range []struct {
		name          string
		labels        []string
		daemonDefault core.WorkflowMode
		itemOverride  string
		wantMode      core.WorkflowMode
		wantEvents    []core.EventType
	}{
		{
			name:       "tier-1 absent, tier-3 absent: hard fallback is dot",
			wantMode:   core.WorkflowModeDot,
			wantEvents: nil,
		},
		{
			name:          "tier-1 absent: daemon default wins",
			daemonDefault: core.WorkflowModeSingle,
			wantMode:      core.WorkflowModeSingle,
			wantEvents:    nil,
		},
		{
			name:          "tier-1 exactly one valid label beats the daemon default",
			labels:        []string{"workflow:dot"},
			daemonDefault: core.WorkflowModeSingle,
			wantMode:      core.WorkflowModeDot,
			wantEvents:    nil,
		},
		{
			name:       "tier-1 workflow:single is audited",
			labels:     []string{"workflow:single"},
			wantMode:   core.WorkflowModeSingle,
			wantEvents: []core.EventType{core.EventTypeReviewBypassed},
		},
		{
			name:          "tier-1 exactly one INVALID label: conflict, walk continues",
			labels:        []string{"workflow:review-loop"},
			daemonDefault: core.WorkflowModeSingle,
			wantMode:      core.WorkflowModeSingle,
			wantEvents:    []core.EventType{core.EventTypeBeadLabelConflict},
		},
		{
			name:          "tier-1 more than one label: conflict, walk continues",
			labels:        []string{"workflow:dot", "workflow:single"},
			daemonDefault: core.WorkflowModeSingle,
			wantMode:      core.WorkflowModeSingle,
			wantEvents:    []core.EventType{core.EventTypeBeadLabelConflict},
		},
		{
			name:         "tier-0 per-item override beats the whole walk",
			labels:       []string{"workflow:single"},
			itemOverride: "dot",
			wantMode:     core.WorkflowModeDot,
			// The walk still runs and still audits the single label. Only the
			// answer changes.
			wantEvents: []core.EventType{core.EventTypeReviewBypassed},
		},
		{
			name:         "tier-0 per-item override that is not a mode is ignored",
			labels:       []string{"workflow:single"},
			itemOverride: "review-loop",
			wantMode:     core.WorkflowModeSingle,
			wantEvents:   []core.EventType{core.EventTypeReviewBypassed},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := runplanEnv(repo, runplanBead(tc.labels, ""))
			env.WorkflowModeDefault = tc.daemonDefault
			env.ItemWorkflowMode = tc.itemOverride

			plan, bus, _ := runplanResolve(t, env)

			if plan.Verdict != runPlanReady {
				t.Fatalf("Verdict = %q; want %q (refusal %+v)", plan.Verdict, runPlanReady, plan.Refusal)
			}
			if plan.WorkflowMode != tc.wantMode {
				t.Errorf("WorkflowMode = %q; want %q", plan.WorkflowMode, tc.wantMode)
			}
			runplanWantEvents(t, bus.seen(), tc.wantEvents)
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Decision 2 — workflow ref (EM-012a)
// ─────────────────────────────────────────────────────────────────────────────

func TestRunPlan_WorkflowRefPrecedence(t *testing.T) {
	t.Parallel()
	repo, _ := runplanRepo(t)

	for _, tc := range []struct {
		name         string
		labels       []string
		itemOverride string
		wantRef      string
	}{
		{name: "no dot label: absent, caller falls through", wantRef: ""},
		{name: "exactly one dot label, bare name gains the suffix", labels: []string{"dot:kerf"}, wantRef: "kerf.dot"},
		{name: "exactly one dot label, suffix already present", labels: []string{"dot:kerf.dot"}, wantRef: "kerf.dot"},
		{name: "exactly one dot label with an empty value: absent", labels: []string{"dot:"}, wantRef: ""},
		{name: "more than one dot label: absent, and SILENT", labels: []string{"dot:a", "dot:b"}, wantRef: ""},
		{name: "codename:eval routes to the lightweight eval graph", labels: []string{"codename:eval"}, wantRef: "eval-bead.dot"},
		{name: "a dot label beats codename:eval", labels: []string{"dot:a", "codename:eval"}, wantRef: "a.dot"},
		{name: "tier-0 per-item ref beats the label", labels: []string{"dot:kerf"}, itemOverride: "explicit.dot", wantRef: "explicit.dot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := runplanEnv(repo, runplanBead(tc.labels, ""))
			env.ItemWorkflowRef = tc.itemOverride

			plan, bus, _ := runplanResolve(t, env)

			if plan.Verdict != runPlanReady {
				t.Fatalf("Verdict = %q; want %q", plan.Verdict, runPlanReady)
			}
			if plan.WorkflowRef != tc.wantRef {
				t.Errorf("WorkflowRef = %q; want %q", plan.WorkflowRef, tc.wantRef)
			}
			// The ref walk reports NOTHING, in every shape — including the
			// more-than-one case, where every other walk emits a conflict. That
			// silence is the behaviour, so assert it.
			runplanWantEvents(t, bus.seen(), nil)
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Decision 3 — harness agent type
// ─────────────────────────────────────────────────────────────────────────────

func TestRunPlan_HarnessAgentTypePrecedence(t *testing.T) {
	t.Parallel()
	repo, _ := runplanRepo(t)

	for _, tc := range []struct {
		name          string
		labels        []string
		queueDefault  core.AgentType
		globalDefault core.AgentType
		wantType      core.AgentType
	}{
		{name: "every tier absent: built-in claude-code", wantType: core.AgentTypeClaudeCode},
		{name: "tier-1 exactly one valid label", labels: []string{"harness:codex"}, wantType: core.AgentTypeCodex},
		{
			name:     "tier-1 exactly one label whose value fails the AR-025 shape: absent",
			labels:   []string{"harness:X"},
			wantType: core.AgentTypeClaudeCode,
		},
		{
			name:         "tier-1 more than one label: absent, queue default wins",
			labels:       []string{"harness:codex", "harness:pi"},
			queueDefault: core.AgentTypePi,
			wantType:     core.AgentTypePi,
		},
		{name: "tier-2 queue default", queueDefault: core.AgentTypeCodex, wantType: core.AgentTypeCodex},
		{name: "tier-4 global default", globalDefault: core.AgentTypePi, wantType: core.AgentTypePi},
		{
			name:          "tier-2 beats tier-4",
			queueDefault:  core.AgentTypeCodex,
			globalDefault: core.AgentTypePi,
			wantType:      core.AgentTypeCodex,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := runplanEnv(repo, runplanBead(tc.labels, ""))
			env.QueueDefaultHarness = tc.queueDefault
			env.DefaultHarness = tc.globalDefault

			plan, bus, _ := runplanResolve(t, env)

			if plan.AgentType != tc.wantType {
				t.Errorf("AgentType = %q; want %q", plan.AgentType, tc.wantType)
			}
			// The harness walk is QUIET on purpose: the launch path resolves the
			// same tuple and emits harness_selected and any conflict there.
			// Emitting here would double every one of them.
			for _, got := range bus.seen() {
				if got == core.EventTypeHarnessSelected {
					t.Errorf("plan emitted %s; the quiet walk must emit nothing — the launch path owns that event", got)
				}
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Decision 4 — model and effort (EM-012b)
// ─────────────────────────────────────────────────────────────────────────────

func TestRunPlan_ModelAndEffortPrecedence(t *testing.T) {
	// NOT parallel: the tier-2.5 cases set process environment variables.
	repo, _ := runplanRepo(t)

	for _, tc := range []struct {
		name       string
		labels     []string
		envModel   string
		envEffort  string
		wantModel  string
		wantEffort string
		wantEvents []core.EventType
	}{
		{
			name:       "every label tier absent: compiled claude-code defaults",
			wantModel:  "sonnet",
			wantEffort: "medium",
		},
		{
			name:       "tier-1 exactly one model label",
			labels:     []string{"model:opus"},
			wantModel:  "opus",
			wantEffort: "medium",
		},
		{
			name:       "tier-1 exactly one effort label",
			labels:     []string{"effort:high"},
			wantModel:  "sonnet",
			wantEffort: "high",
		},
		{
			name:       "tier-1 exactly one INVALID effort label: conflict, walk continues",
			labels:     []string{"effort:extreme"},
			wantModel:  "sonnet",
			wantEffort: "medium",
			wantEvents: []core.EventType{core.EventTypeBeadLabelConflict},
		},
		{
			name:       "tier-1 more than one model label: conflict, walk continues",
			labels:     []string{"model:opus", "model:sonnet"},
			wantModel:  "sonnet",
			wantEffort: "medium",
			wantEvents: []core.EventType{core.EventTypeBeadLabelConflict},
		},
		{
			name:       "tier-1 more than one effort label: conflict, walk continues",
			labels:     []string{"effort:high", "effort:low"},
			wantModel:  "sonnet",
			wantEffort: "medium",
			wantEvents: []core.EventType{core.EventTypeBeadLabelConflict},
		},
		{
			name:       "model and effort resolve independently, and both report",
			labels:     []string{"model:a", "model:b", "effort:x", "effort:y"},
			wantModel:  "sonnet",
			wantEffort: "medium",
			wantEvents: []core.EventType{core.EventTypeBeadLabelConflict, core.EventTypeBeadLabelConflict},
		},
		{
			name:       "tier-2.5 operator env vars are read at plan time",
			envModel:   "opus",
			envEffort:  "high",
			wantModel:  "opus",
			wantEffort: "high",
		},
		{
			name:       "tier-2.5 invalid env values are skipped, not fatal",
			envModel:   "NOT A MODEL",
			envEffort:  "extreme",
			wantModel:  "sonnet",
			wantEffort: "medium",
		},
		{
			name:       "tier-1 beats tier-2.5",
			labels:     []string{"model:haiku", "effort:low"},
			envModel:   "opus",
			envEffort:  "high",
			wantModel:  "haiku",
			wantEffort: "low",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvModelKey, tc.envModel)
			t.Setenv(EnvEffortKey, tc.envEffort)

			env := runplanEnv(repo, runplanBead(tc.labels, ""))
			plan, bus, _ := runplanResolve(t, env)

			if plan.Model != tc.wantModel {
				t.Errorf("Model = %q; want %q", plan.Model, tc.wantModel)
			}
			if plan.Effort != tc.wantEffort {
				t.Errorf("Effort = %q; want %q", plan.Effort, tc.wantEffort)
			}
			runplanWantEvents(t, bus.seen(), tc.wantEvents)
		})
	}
}

// TestRunPlan_ModelDefaultFollowsTheResolvedHarness pins the ordering rule that
// decision 3 must precede decision 4: the compiled model default belongs to the
// harness that will actually run. A pi bead must not be handed the claude
// default (hk-pkugu).
func TestRunPlan_ModelDefaultFollowsTheResolvedHarness(t *testing.T) {
	t.Parallel()
	repo, _ := runplanRepo(t)

	env := runplanEnv(repo, runplanBead([]string{"harness:pi"}, ""))
	plan, _, _ := runplanResolve(t, env)

	if plan.AgentType != core.AgentTypePi {
		t.Fatalf("AgentType = %q; want %q", plan.AgentType, core.AgentTypePi)
	}
	if plan.Model == "sonnet" {
		t.Fatalf("Model = %q: the claude compiled default leaked onto a pi run (hk-pkugu). "+
			"The harness must resolve BEFORE the model walk", plan.Model)
	}
	if plan.Model != "" {
		t.Errorf("Model = %q; want \"\" (pi has no compiled default; the harness applies its own)", plan.Model)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Decision 5 — Pi provider profile
// ─────────────────────────────────────────────────────────────────────────────

// runplanPiConfig is a project config with one named Pi profile and a
// harness-global provider default.
func runplanPiConfig() projectconfig.ProjectConfig {
	return projectconfig.ProjectConfig{
		Harnesses: projectconfig.HarnessesConfig{
			Pi: projectconfig.PiHarnessConfig{
				Provider: "global-provider",
				Profiles: map[string]projectconfig.PiProfileConfig{
					"named": {
						Provider:  "named-provider",
						Model:     "named-provider/some-id",
						APIKeyEnv: "RUNPLAN_PI_KEY",
					},
				},
			},
		},
	}
}

func TestRunPlan_PiProfilePrecedence(t *testing.T) {
	t.Parallel()
	repo, _ := runplanRepo(t)

	for _, tc := range []struct {
		name          string
		labels        []string
		globalDefault core.AgentType
		wantVerdict   runPlanVerdict
		wantProfile   string // resolved profile provider, "" for the zero tuple
		wantModel     string
		wantProvider  string // provider_selected + handle, "" when neither happens
		wantEvents    []core.EventType
	}{
		{
			name:          "not a pi run: a profile label is ignored, quietly",
			labels:        []string{"profile:named", "harness:codex"},
			globalDefault: core.AgentTypePi,
			wantVerdict:   runPlanReady,
			wantModel:     "",
		},
		{
			name:          "pi run, no profile label: zero tuple, harness-global provider",
			globalDefault: core.AgentTypePi,
			wantVerdict:   runPlanReady,
			wantProvider:  "global-provider",
			wantEvents:    []core.EventType{core.EventTypeProviderSelected},
		},
		{
			name:          "pi run, exactly one known profile: the profile wins",
			labels:        []string{"profile:named"},
			globalDefault: core.AgentTypePi,
			wantVerdict:   runPlanReady,
			wantProfile:   "named-provider",
			wantModel:     "named-provider/some-id",
			wantProvider:  "named-provider",
			wantEvents:    []core.EventType{core.EventTypeProviderSelected},
		},
		{
			name:          "pi run, exactly one UNKNOWN profile: the bead is refused",
			labels:        []string{"profile:absent"},
			globalDefault: core.AgentTypePi,
			wantVerdict:   runPlanRefusedPiProfile,
		},
		{
			name:          "pi run, more than one profile label: conflict, zero tuple",
			labels:        []string{"profile:named", "profile:other"},
			globalDefault: core.AgentTypePi,
			wantVerdict:   runPlanReady,
			wantProvider:  "global-provider",
			wantEvents:    []core.EventType{core.EventTypeBeadLabelConflict, core.EventTypeProviderSelected},
		},
		{
			name:          "a profile and exactly one model label: the label keeps the model",
			labels:        []string{"profile:named", "model:override"},
			globalDefault: core.AgentTypePi,
			wantVerdict:   runPlanReady,
			wantProfile:   "named-provider",
			wantModel:     "override",
			wantProvider:  "named-provider",
			wantEvents:    []core.EventType{core.EventTypeProviderSelected},
		},
		{
			name:          "a profile and MORE THAN ONE model label: the profile model wins",
			labels:        []string{"profile:named", "model:a", "model:b"},
			globalDefault: core.AgentTypePi,
			wantVerdict:   runPlanReady,
			wantProfile:   "named-provider",
			wantModel:     "named-provider/some-id",
			wantProvider:  "named-provider",
			wantEvents: []core.EventType{
				core.EventTypeBeadLabelConflict, // the two model labels
				core.EventTypeProviderSelected,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := runplanEnv(repo, runplanBead(tc.labels, ""))
			env.DefaultHarness = tc.globalDefault
			env.ProjectCfg = runplanPiConfig()

			plan, bus, handle := runplanResolve(t, env)

			if plan.Verdict != tc.wantVerdict {
				t.Fatalf("Verdict = %q; want %q (refusal %+v)", plan.Verdict, tc.wantVerdict, plan.Refusal)
			}
			if tc.wantVerdict != runPlanReady {
				// A refusal names the profile and carries the typed error.
				var unknown *PiProfileUnknownError
				if !errors.As(plan.Refusal.Err, &unknown) {
					t.Fatalf("Refusal.Err = %v; want a *PiProfileUnknownError", plan.Refusal.Err)
				}
				if !strings.Contains(plan.Refusal.ReopenReason, "absent") {
					t.Errorf("ReopenReason = %q; want it to name the unknown profile", plan.Refusal.ReopenReason)
				}
				// A refusal must not stamp a provider on the run handle.
				if handle.providerSet {
					t.Errorf("SetResolvedProvider was called on a refused run")
				}
				return
			}
			if plan.PiProfile.Provider != tc.wantProfile {
				t.Errorf("PiProfile.Provider = %q; want %q", plan.PiProfile.Provider, tc.wantProfile)
			}
			if plan.Model != tc.wantModel {
				t.Errorf("Model = %q; want %q", plan.Model, tc.wantModel)
			}
			runplanWantEvents(t, bus.seen(), tc.wantEvents)

			if tc.wantProvider == "" {
				if handle.providerSet {
					t.Errorf("SetResolvedProvider(%q) on a non-pi run; want no call at all — "+
						"the handle distinguishes \"not resolved\" from \"resolved to empty\"", handle.provider)
				}
				return
			}
			if !handle.providerSet || handle.provider != tc.wantProvider {
				t.Errorf("handle provider = %q (set=%v); want %q", handle.provider, handle.providerSet, tc.wantProvider)
			}
			var payload core.ProviderSelectedPayload
			if err := json.Unmarshal(bus.firstPayload(core.EventTypeProviderSelected), &payload); err != nil {
				t.Fatalf("provider_selected payload: %v", err)
			}
			if payload.Provider != tc.wantProvider {
				t.Errorf("provider_selected provider = %q; want %q", payload.Provider, tc.wantProvider)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Decisions 6 and 7 — active repo and effective protect-branches
// ─────────────────────────────────────────────────────────────────────────────

func TestRunPlan_ActiveRepoAndProtectBranches(t *testing.T) {
	t.Parallel()

	t.Run("no target_repo: the project dir is the active repo", func(t *testing.T) {
		t.Parallel()
		repo, _ := runplanRepo(t)
		env := runplanEnv(repo, runplanBead(nil, ""))
		env.ProtectBranches = []string{"release"}

		plan, _, _ := runplanResolve(t, env)

		if plan.Verdict != runPlanReady {
			t.Fatalf("Verdict = %q; want ready (%+v)", plan.Verdict, plan.Refusal)
		}
		if plan.ActiveRepo != repo {
			t.Errorf("ActiveRepo = %q; want %q", plan.ActiveRepo, repo)
		}
		if len(plan.MergeProtectBranches) != 1 || plan.MergeProtectBranches[0] != "release" {
			t.Errorf("MergeProtectBranches = %v; want [release]", plan.MergeProtectBranches)
		}
	})

	t.Run("target_repo equal to the project dir is not a cross-repo run", func(t *testing.T) {
		t.Parallel()
		repo, _ := runplanRepo(t)
		env := runplanEnv(repo, runplanBead(nil, runplanBeadBody("target_repo: "+repo)))

		plan, _, _ := runplanResolve(t, env)

		if plan.Verdict != runPlanReady {
			t.Fatalf("Verdict = %q; want ready (%+v)", plan.Verdict, plan.Refusal)
		}
		if plan.ActiveRepo != repo {
			t.Errorf("ActiveRepo = %q; want %q", plan.ActiveRepo, repo)
		}
	})

	t.Run("target_repo outside the safelist refuses the bead", func(t *testing.T) {
		t.Parallel()
		repo, _ := runplanRepo(t)
		other, _ := runplanRepo(t)
		env := runplanEnv(repo, runplanBead(nil, runplanBeadBody("target_repo: "+other)))
		// AllowedRepos deliberately left empty: an empty safelist allows nothing.

		plan, bus, _ := runplanResolve(t, env)

		if plan.Verdict != runPlanRefusedCrossRepoUnsafe {
			t.Fatalf("Verdict = %q; want %q", plan.Verdict, runPlanRefusedCrossRepoUnsafe)
		}
		var unsafe *CrossRepoUnsafeError
		if !errors.As(plan.Refusal.Err, &unsafe) {
			t.Fatalf("Refusal.Err = %v; want a *CrossRepoUnsafeError", plan.Refusal.Err)
		}
		if !strings.Contains(plan.Refusal.ReopenReason, other) {
			t.Errorf("ReopenReason = %q; want it to name the refused repo %q", plan.Refusal.ReopenReason, other)
		}
		// A refusal adds nothing to the stream.
		runplanWantEvents(t, bus.seen(), nil)
	})

	t.Run("target_repo in the safelist becomes the active repo and nils the protect list", func(t *testing.T) {
		t.Parallel()
		repo, _ := runplanRepo(t)
		other, _ := runplanRepo(t)
		env := runplanEnv(repo, runplanBead(nil, runplanBeadBody("target_repo: "+other)))
		env.AllowedRepos = []string{other}
		env.ProtectBranches = []string{"main"}

		plan, _, _ := runplanResolve(t, env)

		if plan.Verdict != runPlanReady {
			t.Fatalf("Verdict = %q; want ready (%+v)", plan.Verdict, plan.Refusal)
		}
		if plan.ActiveRepo != other {
			t.Errorf("ActiveRepo = %q; want %q", plan.ActiveRepo, other)
		}
		// The daemon's protect list names harmonik's branches and says nothing
		// about the target repo's.
		if plan.MergeProtectBranches != nil {
			t.Errorf("MergeProtectBranches = %v; want nil on a cross-repo run", plan.MergeProtectBranches)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Decisions 8, 9 and 10 — parent commit, lands_on, merge target
// ─────────────────────────────────────────────────────────────────────────────

func TestRunPlan_BranchingPrecedence(t *testing.T) {
	t.Parallel()

	t.Run("no ## Branching section: spec defaults off the daemon target", func(t *testing.T) {
		t.Parallel()
		repo, head := runplanRepo(t)
		plan, _, _ := runplanResolve(t, runplanEnv(repo, runplanBead(nil, "")))

		if plan.Verdict != runPlanReady {
			t.Fatalf("Verdict = %q; want ready (%+v)", plan.Verdict, plan.Refusal)
		}
		if plan.ParentSHA != head {
			t.Errorf("ParentSHA = %q; want the main tip %q", plan.ParentSHA, head)
		}
		if plan.BaseBranch != "main" {
			t.Errorf("BaseBranch = %q; want \"main\"", plan.BaseBranch)
		}
		if plan.MergeTarget != "main" {
			t.Errorf("MergeTarget = %q; want \"main\"", plan.MergeTarget)
		}
		if plan.Branching.LandingStrategy != "squash" {
			t.Errorf("LandingStrategy = %q; want \"squash\"", plan.Branching.LandingStrategy)
		}
	})

	t.Run("tier-1 start_from names a branch that exists", func(t *testing.T) {
		t.Parallel()
		repo, _ := runplanRepo(t)
		sideSHA := runplanSideBranch(t, repo)
		env := runplanEnv(repo, runplanBead(nil, runplanBeadBody("start_from: side")))

		plan, _, _ := runplanResolve(t, env)

		if plan.Verdict != runPlanReady {
			t.Fatalf("Verdict = %q; want ready (%+v)", plan.Verdict, plan.Refusal)
		}
		if plan.ParentSHA != sideSHA {
			t.Errorf("ParentSHA = %q; want the side tip %q", plan.ParentSHA, sideSHA)
		}
	})

	t.Run("tier-1 start_from names a ref that does not resolve: refuse", func(t *testing.T) {
		t.Parallel()
		repo, _ := runplanRepo(t)
		env := runplanEnv(repo, runplanBead(nil, runplanBeadBody("start_from: no-such-ref")))

		plan, bus, _ := runplanResolve(t, env)

		if plan.Verdict != runPlanRefusedStartFrom {
			t.Fatalf("Verdict = %q; want %q", plan.Verdict, runPlanRefusedStartFrom)
		}
		var refErr *StartFromRefError
		if !errors.As(plan.Refusal.Err, &refErr) {
			t.Fatalf("Refusal.Err = %v; want a *StartFromRefError", plan.Refusal.Err)
		}
		if !strings.HasPrefix(plan.Refusal.ReopenReason, "resolve start_from failed: ") {
			t.Errorf("ReopenReason = %q; want the pre-plan wording", plan.Refusal.ReopenReason)
		}
		runplanWantEvents(t, bus.seen(), nil)
	})

	t.Run("a malformed project branching.yaml refuses rather than falling back", func(t *testing.T) {
		t.Parallel()
		repo, _ := runplanRepo(t)
		runplanWriteFile(t, repo, ".harmonik/branching.yaml", "start_from: [not, a, scalar\n")
		env := runplanEnv(repo, runplanBead(nil, ""))

		plan, _, _ := runplanResolve(t, env)

		if plan.Verdict != runPlanRefusedStartFrom {
			t.Fatalf("Verdict = %q; want %q — a malformed project config is operator-detectable "+
				"and must not silently fall through to spec defaults", plan.Verdict, runPlanRefusedStartFrom)
		}
		var cfgErr *ErrProjectBranchingConfig
		if !errors.As(plan.Refusal.Err, &cfgErr) {
			t.Fatalf("Refusal.Err = %v; want an *ErrProjectBranchingConfig", plan.Refusal.Err)
		}
	})

	t.Run("a malformed ## Branching section is treated as absent, not fatal", func(t *testing.T) {
		t.Parallel()
		repo, head := runplanRepo(t)
		// Section present, fenced block absent — a BI-009b parse error.
		body := "Some description.\n\n## Branching\n\nstart_from: side\n"
		plan, _, _ := runplanResolve(t, runplanEnv(repo, runplanBead(nil, body)))

		if plan.Verdict != runPlanReady {
			t.Fatalf("Verdict = %q; want ready (%+v)", plan.Verdict, plan.Refusal)
		}
		if plan.ParentSHA != head {
			t.Errorf("ParentSHA = %q; want the main tip %q (bead fields treated as absent)", plan.ParentSHA, head)
		}
	})

	t.Run("tier-2 project defaults fill an unset bead field", func(t *testing.T) {
		t.Parallel()
		repo, _ := runplanRepo(t)
		sideSHA := runplanSideBranch(t, repo)
		runplanWriteFile(t, repo, ".harmonik/branching.yaml",
			"version: 1\ndefaults:\n  start_from: side\n  lands_on: side\n")
		env := runplanEnv(repo, runplanBead(nil, ""))

		plan, _, _ := runplanResolve(t, env)

		if plan.Verdict != runPlanReady {
			t.Fatalf("Verdict = %q; want ready (%+v)", plan.Verdict, plan.Refusal)
		}
		if plan.ParentSHA != sideSHA {
			t.Errorf("ParentSHA = %q; want the side tip %q", plan.ParentSHA, sideSHA)
		}
		if plan.BaseBranch != "side" || plan.MergeTarget != "side" {
			t.Errorf("BaseBranch/MergeTarget = %q/%q; want side/side", plan.BaseBranch, plan.MergeTarget)
		}
	})

	t.Run("tier-1 bead body beats tier-2 project defaults", func(t *testing.T) {
		t.Parallel()
		repo, head := runplanRepo(t)
		runplanSideBranch(t, repo)
		runplanWriteFile(t, repo, ".harmonik/branching.yaml",
			"version: 1\ndefaults:\n  start_from: side\n  lands_on: side\n")
		env := runplanEnv(repo, runplanBead(nil, runplanBeadBody("start_from: main\ntarget_branch: main")))

		plan, _, _ := runplanResolve(t, env)

		if plan.Verdict != runPlanReady {
			t.Fatalf("Verdict = %q; want ready (%+v)", plan.Verdict, plan.Refusal)
		}
		if plan.ParentSHA != head {
			t.Errorf("ParentSHA = %q; want the main tip %q", plan.ParentSHA, head)
		}
		if plan.BaseBranch != "main" {
			t.Errorf("BaseBranch = %q; want \"main\"", plan.BaseBranch)
		}
	})

	t.Run("lands_on on a protected branch refuses the bead", func(t *testing.T) {
		t.Parallel()
		repo, _ := runplanRepo(t)
		env := runplanEnv(repo, runplanBead(nil, runplanBeadBody("target_branch: main")))
		env.ProtectBranches = []string{"main"}

		plan, bus, _ := runplanResolve(t, env)

		if plan.Verdict != runPlanRefusedLandsOnProtected {
			t.Fatalf("Verdict = %q; want %q", plan.Verdict, runPlanRefusedLandsOnProtected)
		}
		var protErr *LandsOnProtectedError
		if !errors.As(plan.Refusal.Err, &protErr) {
			t.Fatalf("Refusal.Err = %v; want a *LandsOnProtectedError", plan.Refusal.Err)
		}
		if protErr.LandsOn != "main" {
			t.Errorf("LandsOnProtectedError.LandsOn = %q; want \"main\"", protErr.LandsOn)
		}
		runplanWantEvents(t, bus.seen(), nil)
	})

	t.Run("the protect check is skipped on a cross-repo run", func(t *testing.T) {
		t.Parallel()
		repo, _ := runplanRepo(t)
		other, otherHead := runplanRepo(t)
		env := runplanEnv(repo, runplanBead(nil, runplanBeadBody("target_repo: "+other+"\ntarget_branch: main")))
		env.AllowedRepos = []string{other}
		env.ProtectBranches = []string{"main"}

		plan, _, _ := runplanResolve(t, env)

		if plan.Verdict != runPlanReady {
			t.Fatalf("Verdict = %q; want ready — the daemon's protect list governs harmonik's "+
				"branches, not the target repo's (%+v)", plan.Verdict, plan.Refusal)
		}
		if plan.ParentSHA != otherHead {
			t.Errorf("ParentSHA = %q; want the TARGET repo's tip %q", plan.ParentSHA, otherHead)
		}
		if plan.BaseBranch != "main" {
			t.Errorf("BaseBranch = %q; want \"main\"", plan.BaseBranch)
		}
	})

	// The two branching answers now come from ONE resolution. Before the plan
	// they came from two, and the second one's failure was survivable: lands_on
	// stayed empty and the protect check was skipped. With one resolution there
	// is no window in which the two can disagree, so a bead whose lands_on is
	// protected can no longer slip past the gate by racing a config write.
	t.Run("one resolution answers both questions from the same config", func(t *testing.T) {
		t.Parallel()
		repo, _ := runplanRepo(t)
		sideSHA := runplanSideBranch(t, repo)
		env := runplanEnv(repo, runplanBead(nil, runplanBeadBody("start_from: side\ntarget_branch: side")))

		plan, _, _ := runplanResolve(t, env)

		if plan.Verdict != runPlanReady {
			t.Fatalf("Verdict = %q; want ready (%+v)", plan.Verdict, plan.Refusal)
		}
		if plan.ParentSHA != sideSHA {
			t.Errorf("ParentSHA = %q; want %q", plan.ParentSHA, sideSHA)
		}
		if plan.Branching.StartFrom != "side" || plan.Branching.LandsOn != "side" {
			t.Errorf("Branching = %+v; want start_from and lands_on both \"side\" from one read", plan.Branching)
		}
		if plan.MergeTarget != plan.BaseBranch {
			t.Errorf("MergeTarget = %q; want it to equal BaseBranch %q", plan.MergeTarget, plan.BaseBranch)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Placement intent
// ─────────────────────────────────────────────────────────────────────────────

// TestRunPlan_CarriesPlacementIntentWithoutPlacing pins the boundary: the plan
// reports where the operator asked the run to go and does not act on it.
// Selecting and reserving a worker happen together inside one mutex-held
// critical section, and the plan must not split that.
func TestRunPlan_CarriesPlacementIntentWithoutPlacing(t *testing.T) {
	t.Parallel()
	repo, _ := runplanRepo(t)

	env := runplanEnv(repo, runplanBead(nil, ""))
	env.ItemLocalOnly = true
	env.ItemWorkerTarget = "box-b"

	// Handles carries NO worker registry. If the plan tried to select a worker
	// it would have to reach through a nil one.
	plan, _, _ := runplanResolve(t, env)

	if plan.Verdict != runPlanReady {
		t.Fatalf("Verdict = %q; want ready (%+v)", plan.Verdict, plan.Refusal)
	}
	if !plan.LocalOnly {
		t.Error("LocalOnly = false; want the per-item intent carried through")
	}
	if plan.WorkerTarget != "box-b" {
		t.Errorf("WorkerTarget = %q; want \"box-b\"", plan.WorkerTarget)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Refusal reports
// ─────────────────────────────────────────────────────────────────────────────

// TestRunPlan_RefusalReportsAreExact pins the two strings every refusal owes an
// operator: the stderr line and the reopen reason. The wording is what the
// daemon said before the plan existed, and an operator's grep does not care
// that the code moved. Each case builds the expected text from the SAME typed
// error the production path builds, so what is under test is the wrapper the
// plan owns, not the error's own wording.
func TestRunPlan_RefusalReportsAreExact(t *testing.T) {
	t.Parallel()

	const beadID = "hk-runplan-probe"

	t.Run("unknown pi profile", func(t *testing.T) {
		t.Parallel()
		repo, _ := runplanRepo(t)
		env := runplanEnv(repo, runplanBead([]string{"profile:absent"}, ""))
		env.DefaultHarness = core.AgentTypePi
		env.ProjectCfg = runplanPiConfig()

		plan, _, _ := runplanResolve(t, env)

		wantErr := (&PiProfileUnknownError{BeadID: beadID, Profile: "absent"}).Error()
		wantLog := "daemon: workloop: bead " + beadID + " refused: " + wantErr + " (reopening)\n"
		if plan.Refusal.LogLine != wantLog {
			t.Errorf("LogLine  = %q\nwant       %q", plan.Refusal.LogLine, wantLog)
		}
		if plan.Refusal.ReopenReason != wantErr {
			t.Errorf("ReopenReason = %q\nwant           %q", plan.Refusal.ReopenReason, wantErr)
		}
	})

	t.Run("target repo outside the safelist", func(t *testing.T) {
		t.Parallel()
		repo, _ := runplanRepo(t)
		const target = "/nonexistent/not-on-the-safelist"
		env := runplanEnv(repo, runplanBead(nil, runplanBeadBody("target_repo: "+target)))

		plan, _, _ := runplanResolve(t, env)

		wantErr := (&CrossRepoUnsafeError{TargetRepo: target, ProjectDir: repo}).Error()
		wantLog := "daemon: workloop: bead " + beadID + " refused: " + wantErr + " (reopening)\n"
		if plan.Refusal.LogLine != wantLog {
			t.Errorf("LogLine  = %q\nwant       %q", plan.Refusal.LogLine, wantLog)
		}
		if plan.Refusal.ReopenReason != wantErr {
			t.Errorf("ReopenReason = %q\nwant           %q", plan.Refusal.ReopenReason, wantErr)
		}
	})

	t.Run("start_from names a ref that does not resolve", func(t *testing.T) {
		t.Parallel()
		repo, _ := runplanRepo(t)
		env := runplanEnv(repo, runplanBead(nil, runplanBeadBody("start_from: no-such-ref")))

		plan, _, _ := runplanResolve(t, env)

		// This refusal keeps the wrapper that names resolveParentCommit. The
		// operator's reopen reason has carried that word since before the plan,
		// so the collapse must not quietly rename it.
		gotErr := plan.Refusal.Err.Error()
		wantPrefix := "daemon: resolveParentCommit for bead " + beadID + ": daemon: start_from ref \"no-such-ref\" not found"
		if !strings.HasPrefix(gotErr, wantPrefix) {
			t.Errorf("Err = %q\nwant it to start with %q", gotErr, wantPrefix)
		}
		wantLog := "daemon: workloop: resolveParentCommit for bead " + beadID + ": " + gotErr + " (reopening)\n"
		if plan.Refusal.LogLine != wantLog {
			t.Errorf("LogLine  = %q\nwant       %q", plan.Refusal.LogLine, wantLog)
		}
		wantReason := "resolve start_from failed: " + gotErr
		if plan.Refusal.ReopenReason != wantReason {
			t.Errorf("ReopenReason = %q\nwant           %q", plan.Refusal.ReopenReason, wantReason)
		}
	})

	t.Run("lands_on is a protected branch", func(t *testing.T) {
		t.Parallel()
		repo, _ := runplanRepo(t)
		env := runplanEnv(repo, runplanBead(nil, runplanBeadBody("target_branch: main")))
		env.ProtectBranches = []string{"main"}

		plan, _, _ := runplanResolve(t, env)

		wantErr := (&LandsOnProtectedError{LandsOn: "main"}).Error()
		wantLog := "daemon: workloop: bead " + beadID + " refused: " + wantErr + " (reopening)\n"
		if plan.Refusal.LogLine != wantLog {
			t.Errorf("LogLine  = %q\nwant       %q", plan.Refusal.LogLine, wantLog)
		}
		if plan.Refusal.ReopenReason != wantErr {
			t.Errorf("ReopenReason = %q\nwant           %q", plan.Refusal.ReopenReason, wantErr)
		}
	})
}
