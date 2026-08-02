package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runloop"
)

// TestResolveWorkflow_CompleteSelection proves that graph selection is complete
// before the run path can create resources. Each case also records the exact
// compatibility provenance and raw queue input that a later start event uses.
func TestResolveWorkflow_CompleteSelection(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		labels     []string
		input      runloop.QueueWorkflowInput
		projectDot bool
		namedDot   bool
		want       resolvedWorkflow
		wantEvents []core.EventType
	}{
		{
			name: "embedded standard graph is the reviewed fallback",
			want: resolvedWorkflow{
				Descriptor:      standardBeadDescriptor,
				Mode:            core.WorkflowModeDot,
				ReviewPolicy:    core.ReviewPolicyReviewed,
				SelectionSource: core.WorkflowSelectionEmbeddedDefault,
			},
		},
		{
			name:       "project workflow is a reviewed default",
			projectDot: true,
			want: resolvedWorkflow{
				Descriptor:      standardBeadDescriptor,
				Mode:            core.WorkflowModeDot,
				ReviewPolicy:    core.ReviewPolicyReviewed,
				SelectionSource: core.WorkflowSelectionProjectDefault,
			},
		},
		{
			name:     "named graph is an explicit reviewed selection",
			input:    runloop.QueueWorkflowInput{Ref: "named.dot"},
			namedDot: true,
			want: resolvedWorkflow{
				Descriptor:      standardBeadDescriptor,
				Mode:            core.WorkflowModeDot,
				ReviewPolicy:    core.ReviewPolicyReviewed,
				SelectionSource: core.WorkflowSelectionExplicitRef,
				WorkflowRef:     "named.dot",
				RawQueueRef:     "named.dot",
			},
		},
		{
			name:   "legacy label selects the registered no-review graph",
			labels: []string{"workflow:single"},
			want: resolvedWorkflow{
				Descriptor:      noReviewBeadDescriptor,
				Mode:            core.WorkflowModeDot,
				ReviewPolicy:    core.ReviewPolicyNoReview,
				SelectionSource: core.WorkflowSelectionLegacySingleLabel,
			},
			wantEvents: []core.EventType{core.EventTypeReviewBypassed},
		},
		{
			name:   "queue single wins before label or ref resolution",
			labels: []string{"workflow:dot"},
			input:  runloop.QueueWorkflowInput{Mode: "single", Ref: "named.dot"},
			want: resolvedWorkflow{
				Descriptor:      noReviewBeadDescriptor,
				Mode:            core.WorkflowModeDot,
				ReviewPolicy:    core.ReviewPolicyNoReview,
				SelectionSource: core.WorkflowSelectionQueueItemSingleMode,
				RawQueueMode:    "single",
				RawQueueRef:     "named.dot",
			},
		},
		{
			name:   "tier-zero dot suppresses a lower-priority bypass audit",
			labels: []string{"workflow:single"},
			input:  runloop.QueueWorkflowInput{Mode: "dot"},
			want: resolvedWorkflow{
				Descriptor:      standardBeadDescriptor,
				Mode:            core.WorkflowModeDot,
				ReviewPolicy:    core.ReviewPolicyReviewed,
				SelectionSource: core.WorkflowSelectionEmbeddedDefault,
				RawQueueMode:    "dot",
			},
		},
		{
			name:   "retired review-loop is not a workflow selection",
			labels: []string{"workflow:review-loop"},
			want: resolvedWorkflow{
				Descriptor:      standardBeadDescriptor,
				Mode:            core.WorkflowModeDot,
				ReviewPolicy:    core.ReviewPolicyReviewed,
				SelectionSource: core.WorkflowSelectionEmbeddedDefault,
			},
			wantEvents: []core.EventType{core.EventTypeBeadLabelConflict},
		},
		{
			name:   "duplicate workflow labels are rejected as an ambiguous tier",
			labels: []string{"workflow:dot", "workflow:single"},
			want: resolvedWorkflow{
				Descriptor:      standardBeadDescriptor,
				Mode:            core.WorkflowModeDot,
				ReviewPolicy:    core.ReviewPolicyReviewed,
				SelectionSource: core.WorkflowSelectionEmbeddedDefault,
			},
			wantEvents: []core.EventType{core.EventTypeBeadLabelConflict},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			projectDir := t.TempDir()
			if tc.projectDot {
				runplanWriteFile(t, projectDir, "workflow.dot", string(standardBeadDotSrc))
			}
			if tc.namedDot {
				runplanWriteFile(t, projectDir, "named.dot", string(standardBeadDotSrc))
			}
			bus := &runplanBus{}
			got, err := resolveWorkflow(context.Background(), runloop.RunEnv{
				ProjectDir:   projectDir,
				BeadRecord:   runplanBead(tc.labels, ""),
				ItemWorkflow: tc.input,
			}, bus)
			if err != nil {
				t.Fatalf("resolveWorkflow() error = %v", err)
			}
			if !got.Valid() {
				t.Fatalf("resolved workflow is invalid: %+v", got)
			}
			if got.Graph == nil || got.Descriptor != tc.want.Descriptor || got.Mode != tc.want.Mode ||
				got.ReviewPolicy != tc.want.ReviewPolicy || got.SelectionSource != tc.want.SelectionSource ||
				got.WorkflowRef != tc.want.WorkflowRef || got.RawQueueMode != tc.want.RawQueueMode || got.RawQueueRef != tc.want.RawQueueRef {
				t.Fatalf("resolved workflow = %+v; want descriptor=%+v mode=%q policy=%q source=%q ref=%q raw=(%q,%q)",
					got, tc.want.Descriptor, tc.want.Mode, tc.want.ReviewPolicy, tc.want.SelectionSource,
					tc.want.WorkflowRef, tc.want.RawQueueMode, tc.want.RawQueueRef)
			}
			runplanWantEvents(t, bus.seen(), tc.wantEvents)
		})
	}
}

func TestResolveWorkflow_RejectsMissingExplicitGraph(t *testing.T) {
	t.Parallel()
	_, err := resolveWorkflow(context.Background(), runloop.RunEnv{
		ProjectDir:   t.TempDir(),
		BeadRecord:   runplanBead(nil, ""),
		ItemWorkflow: runloop.QueueWorkflowInput{Ref: "missing.dot"},
	}, &runplanBus{})
	if err == nil || !strings.Contains(err.Error(), "missing.dot") {
		t.Fatalf("resolveWorkflow() error = %v; want missing explicit graph error", err)
	}
}

func TestResolveWorkflow_RejectsGraphAuthoredReviewPolicy(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	withPolicy := strings.Replace(string(standardBeadDotSrc), "workflow_id=\"standard-bead\";", "workflow_id=\"standard-bead\";\n    review_policy=\"no_review\";", 1)
	runplanWriteFile(t, projectDir, "policy.dot", withPolicy)
	_, err := resolveWorkflow(context.Background(), runloop.RunEnv{
		ProjectDir:   projectDir,
		BeadRecord:   runplanBead(nil, ""),
		ItemWorkflow: runloop.QueueWorkflowInput{Ref: "policy.dot"},
	}, &runplanBus{})
	if err == nil || !strings.Contains(err.Error(), "review_policy") {
		t.Fatalf("resolveWorkflow() error = %v; want graph policy rejection", err)
	}
}

func TestResolveWorkflow_ExplicitNoReviewGraphRemainsReviewed(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	runplanWriteFile(t, projectDir, "copied-no-review.dot", string(noReviewBeadDotSrc))
	got, err := resolveWorkflow(context.Background(), runloop.RunEnv{
		ProjectDir:   projectDir,
		BeadRecord:   runplanBead(nil, ""),
		ItemWorkflow: runloop.QueueWorkflowInput{Ref: "copied-no-review.dot"},
	}, &runplanBus{})
	if err != nil {
		t.Fatalf("resolveWorkflow() error = %v", err)
	}
	if got.Descriptor != noReviewBeadDescriptor || got.ReviewPolicy != core.ReviewPolicyReviewed || got.SelectionSource != core.WorkflowSelectionExplicitRef {
		t.Fatalf("explicit no-review graph = descriptor=%+v policy=%q source=%q; want reviewed explicit selection", got.Descriptor, got.ReviewPolicy, got.SelectionSource)
	}
}

func TestResolvedWorkflow_NoReviewRequiresExactRegisteredCompatibilitySelection(t *testing.T) {
	t.Parallel()
	graph, err := loadStandardGraph(nil)
	if err != nil {
		t.Fatalf("loadStandardGraph: %v", err)
	}
	for _, source := range []core.WorkflowSelectionSource{
		core.WorkflowSelectionEmbeddedDefault,
		core.WorkflowSelectionProjectDefault,
		core.WorkflowSelectionExplicitRef,
	} {
		candidate := resolvedWorkflow{
			Graph:           graph,
			Descriptor:      standardBeadDescriptor,
			Mode:            core.WorkflowModeDot,
			ReviewPolicy:    core.ReviewPolicyNoReview,
			SelectionSource: source,
		}
		if candidate.Valid() {
			t.Fatalf("arbitrary DOT selection source %q accepted no_review", source)
		}
	}
}

func TestRunEnvWithDispatch_RetainsRawQueueWorkflow(t *testing.T) {
	t.Parallel()
	want := runloop.QueueWorkflowInput{Mode: "single", Ref: "legacy.dot"}
	got := runEnvWithDispatch(runloop.RunEnv{}, core.RunID(uuid.New()), runplanBead(nil, ""), "queue", nil, nil, 0, want, nil, false, "", "")
	if got.ItemWorkflow != want {
		t.Fatalf("ItemWorkflow = %+v; want %+v", got.ItemWorkflow, want)
	}
}
