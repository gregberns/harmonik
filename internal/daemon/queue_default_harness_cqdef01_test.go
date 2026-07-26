package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
	"github.com/gregberns/harmonik/internal/runloop"
)

type cqDef01Emitter struct{}

func (cqDef01Emitter) Emit(context.Context, core.EventType, []byte) error { return nil }
func (cqDef01Emitter) EmitWithRunID(context.Context, core.RunID, core.EventType, []byte) error {
	return nil
}

func cqDef01Queue(defaultHarness core.AgentType, labels []string) (*queuewiring.QueueStore, core.BeadRecord) {
	bead := core.BeadRecord{
		BeadID: core.BeadID("cq-def-01"),
		Labels: labels,
	}
	qs := queuewiring.NewQueueStore()
	qs.SetQueueByName("main", &queue.Queue{
		SchemaVersion:  1,
		QueueID:        "cq-def-01-qid",
		Name:           "main",
		Status:         queue.QueueStatusActive,
		DefaultHarness: defaultHarness,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusActive,
			Items: []queue.Item{{
				BeadID: bead.BeadID,
				Status: queue.ItemStatusPending,
			}},
		}},
	})
	return qs, bead
}

func cqDef01PiRegistry(t *testing.T) *handlercontract.HarnessRegistry {
	t.Helper()
	keyFile := filepath.Join(t.TempDir(), "pi-key")
	if err := os.WriteFile(keyFile, []byte("test-key\n"), 0o600); err != nil {
		t.Fatalf("write Pi key fixture: %v", err)
	}
	reg, err := newHarnessRegistry(projectconfig.PiHarnessConfig{
		Provider:   "cq-def-01-provider",
		Model:      "cq-def-01-model",
		APIKeyEnv:  "CQ_DEF_01_PI_KEY",
		APIKeyFile: keyFile,
		BaseURL:    "http://127.0.0.1:1/v1",
		API:        "openai-completions",
	})
	if err != nil {
		t.Fatalf("newHarnessRegistry: %v", err)
	}
	return reg
}

// TestSelectQueueDefaultHarness covers harness precedence after the pure
// selector has preserved the selected queue's tier-2 value.
func TestSelectQueueDefaultHarness(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		labels        []string
		queueDefault  core.AgentType
		globalDefault core.AgentType
		want          core.AgentType
		compareLegacy bool
	}{
		{
			name:          "valid tier-1 label wins",
			labels:        []string{"harness:codex"},
			queueDefault:  core.AgentTypePi,
			globalDefault: core.AgentTypeClaudeCode,
			want:          core.AgentTypeCodex,
		},
		{
			name:          "empty queue default preserves golden fallback",
			queueDefault:  core.AgentType(""),
			globalDefault: core.AgentTypeCodex,
			want:          core.AgentTypeCodex,
			compareLegacy: true,
		},
		{
			name:          "valid non-pi queue default is not hard-coded away",
			queueDefault:  core.AgentTypeCodex,
			globalDefault: core.AgentTypeClaudeCode,
			want:          core.AgentTypeCodex,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			qs, bead := cqDef01Queue(tc.queueDefault, tc.labels)
			lq := qs.LockForMutation()
			sel, ok := selectNextQueue(lq, NewRunRegistry(), 1, 0, nil)
			lq.Done()
			if !ok {
				t.Fatal("selectNextQueue returned no selection")
			}
			got := resolveHarnessAgentTypeQuiet(
				bead, sel.queueDefaultHarness, core.AgentType(""), tc.globalDefault,
			)
			if got != tc.want {
				t.Fatalf("resolved harness = %q; want %q", got, tc.want)
			}
			if tc.compareLegacy {
				legacy := resolveHarnessAgentTypeQuiet(
					bead, core.AgentType(""), core.AgentType(""), tc.globalDefault,
				)
				if got != legacy {
					t.Fatalf("empty queue default changed legacy routing: got %q, legacy %q", got, legacy)
				}
			}
		})
	}
}

// TestQueueDefaultHarnessProductionPath starts at persisted queue state and
// follows the actual selection → RunEnv → buildRunBundles production spine.
func TestQueueDefaultHarnessProductionPath(t *testing.T) {
	t.Parallel()

	qs, bead := cqDef01Queue(core.AgentTypePi, nil)
	lq := qs.LockForMutation()
	sel, ok := selectNextQueue(lq, NewRunRegistry(), 1, 0, nil)
	lq.Done()
	if !ok {
		t.Fatal("selectNextQueue returned no selection")
	}

	deps := workLoopDeps{
		defaultHarness:  core.AgentTypeCodex,
		harnessRegistry: cqDef01PiRegistry(t),
		bus:             cqDef01Emitter{},
	}
	env := deps.runEnv(
		core.RunID{}, bead, sel.queueName, &sel.queueID, &sel.groupIndex, sel.itemIdx,
		sel.itemWFMode, sel.itemWFRef, sel.itemTemplateMap,
		sel.queueLocalOnly, sel.queueWorkerTarget, sel.queueDefaultHarness,
	)
	if env.QueueDefaultHarness != core.AgentTypePi {
		t.Fatalf("RunEnv.QueueDefaultHarness = %q; want pi", env.QueueDefaultHarness)
	}
	if env.DefaultHarness != core.AgentTypeCodex {
		t.Fatalf("RunEnv.DefaultHarness = %q; want global codex unchanged", env.DefaultHarness)
	}
	if got := resolveHarnessAgentTypeQuiet(
		bead, env.QueueDefaultHarness, core.AgentType(""), env.DefaultHarness,
	); got != core.AgentTypePi {
		t.Fatalf("quiet harness resolution = %q; want pi", got)
	}

	ports, _ := deps.buildRunBundles(env)
	if ports.LaunchBuilder == nil {
		t.Fatal("buildRunBundles left LaunchBuilder nil")
	}
	_, artifacts, err := ports.LaunchBuilder(context.Background(), shared.LaunchCtx{
		RunID:         core.RunID{},
		BeadID:        string(bead.BeadID),
		WorkspacePath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("production launch builder: %v", err)
	}
	if artifacts.ResolvedAgentType != core.AgentTypePi {
		t.Fatalf("launch builder resolved harness = %q; want pi", artifacts.ResolvedAgentType)
	}
}

// TestQueueDefaultHarnessDoesNotOverrideGlobalReviewerDefault proves the queue
// tier remains separate from the global tier and that DOT reviewer-class paths
// correct an inherited Pi implementer harness to the supported Claude harness.
func TestQueueDefaultHarnessDoesNotOverrideGlobalReviewerDefault(t *testing.T) {
	t.Parallel()

	reg := cqDef01PiRegistry(t)
	bead := core.BeadRecord{BeadID: core.BeadID("cq-def-01-reviewer")}
	got := runloop.DotReviewerInheritedHarnessOverride(
		reg,
		resolveHarnessAgentTypeQuiet,
		true,
		core.AgentType(""),
		core.AgentType(""),
		bead,
		core.AgentTypePi,
		core.AgentTypeCodex,
		string(bead.BeadID),
	)
	if got != core.AgentTypeClaudeCode {
		t.Fatalf("DOT inherited reviewer harness = %q; want claude-code", got)
	}

	gateBody, err := os.ReadFile("dot_gate.go")
	if err != nil {
		t.Fatalf("read dot_gate.go: %v", err)
	}
	cascadeBody, err := os.ReadFile("dot_cascade_core.go")
	if err != nil {
		t.Fatalf("read dot_cascade_core.go: %v", err)
	}
	for path, src := range map[string]string{
		"dot_gate.go":         string(gateBody),
		"dot_cascade_core.go": string(cascadeBody),
	} {
		for _, want := range []string{"env.QueueDefaultHarness,", "env.DefaultHarness,"} {
			if !strings.Contains(src, want) {
				t.Errorf("%s production call site missing %q", path, want)
			}
		}
	}
}
