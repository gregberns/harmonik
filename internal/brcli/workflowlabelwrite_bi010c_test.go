package brcli_test

// workflowlabelwrite_bi010c_test.go — BI-010c workflow-mode label write
// discipline tests.
//
// Spec ref: specs/beads-integration.md §4.3 BI-010c.
//
// Coverage:
//   - Agent-context write with workflow:<mode> label returns
//     ErrWorkflowLabelWriteForbidden.
//   - Daemon-context write with workflow:<mode> label succeeds (nil error).
//   - Agent-context write without a workflow:<mode> label succeeds (nil error).
//   - All three declared workflow modes (single, review-loop, dot) are caught.
//   - CallerKindAgent is the zero value (safe default).
//   - Spec-content sensor: BI-010c anchor present in beads-integration.md.
//
// Bead: hk-7om2q.13.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
)

// ─── Fixtures ────────────────────────────────────────────────────────────────

// wlwFixtureAgentArgs returns a br argv slice that carries the given label as
// an argument (simulating `br update <beadID> --label <label>`).
func wlwFixtureAgentArgs(label string) []string {
	return []string{"update", "hk-7om2q.13", "--label", label}
}

// wlwFixtureDaemonArgs returns the same argv slice as wlwFixtureAgentArgs.
// The caller kind, not the argv, determines the permission.
func wlwFixtureDaemonArgs(label string) []string {
	return wlwFixtureAgentArgs(label)
}

// wlwFixtureBenignArgs returns a br argv slice that does NOT carry any
// workflow:<mode> label.
func wlwFixtureBenignArgs() []string {
	return []string{"update", "hk-7om2q.13", "--notes", "some note"}
}

// wlwFixtureSpecContent reads specs/beads-integration.md and returns the
// paragraph starting at the BI-010c anchor. The test fails if the spec is
// unreadable or the anchor is absent.
func wlwFixtureSpecContent(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("wlwFixtureSpecContent: runtime.Caller failed — cannot locate repo root")
	}
	// Walk up: internal/brcli/<file> → internal → repo root
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "beads-integration.md")

	//nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("wlwFixtureSpecContent: cannot read %s: %v", specPath, err)
	}
	content := string(raw)

	const anchor = "BI-010c"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf(
			"wlwFixtureSpecContent: spec %s does not contain %q; BI-010c may have been removed or renamed",
			specPath, anchor,
		)
	}

	// Return the paragraph from the anchor to the next section boundary.
	para := content[idx:]
	if end := strings.Index(para, "\n####"); end > 0 {
		para = para[:end]
	}
	return para
}

// ─── Agent-context rejection tests ───────────────────────────────────────────

// TestBI010c_AgentContextSingleLabelRejected verifies that an agent-context
// caller attempting to write the workflow:single label receives
// ErrWorkflowLabelWriteForbidden.
//
// This is the primary acceptance criterion from the bead body:
// "agent-context write with workflow:single label returns typed error".
//
// Spec ref: specs/beads-integration.md §4.3 BI-010c.
func TestBI010c_AgentContextSingleLabelRejected(t *testing.T) {
	t.Parallel()

	err := brcli.CheckWorkflowLabelWrite(brcli.CallerKindAgent, wlwFixtureAgentArgs("workflow:single"))
	if err == nil {
		t.Fatal("BI-010c: expected ErrWorkflowLabelWriteForbidden for agent-context workflow:single write, got nil")
	}
	if !errors.Is(err, brcli.ErrWorkflowLabelWriteForbidden) {
		t.Errorf(
			"BI-010c: errors.Is(err, ErrWorkflowLabelWriteForbidden) = false; got %v",
			err,
		)
	}
}

// TestBI010c_AgentContextReviewLoopLabelRejected verifies that an agent-context
// caller attempting to write the workflow:review-loop label receives
// ErrWorkflowLabelWriteForbidden.
//
// Spec ref: specs/beads-integration.md §4.3 BI-010c.
func TestBI010c_AgentContextReviewLoopLabelRejected(t *testing.T) {
	t.Parallel()

	err := brcli.CheckWorkflowLabelWrite(brcli.CallerKindAgent, wlwFixtureAgentArgs("workflow:review-loop"))
	if err == nil {
		t.Fatal("BI-010c: expected ErrWorkflowLabelWriteForbidden for agent-context workflow:review-loop write, got nil")
	}
	if !errors.Is(err, brcli.ErrWorkflowLabelWriteForbidden) {
		t.Errorf("BI-010c: got %v; want ErrWorkflowLabelWriteForbidden", err)
	}
}

// TestBI010c_AgentContextDotLabelRejected verifies that an agent-context caller
// attempting to write the workflow:dot label receives
// ErrWorkflowLabelWriteForbidden.
//
// Spec ref: specs/beads-integration.md §4.3 BI-010c.
func TestBI010c_AgentContextDotLabelRejected(t *testing.T) {
	t.Parallel()

	err := brcli.CheckWorkflowLabelWrite(brcli.CallerKindAgent, wlwFixtureAgentArgs("workflow:dot"))
	if err == nil {
		t.Fatal("BI-010c: expected ErrWorkflowLabelWriteForbidden for agent-context workflow:dot write, got nil")
	}
	if !errors.Is(err, brcli.ErrWorkflowLabelWriteForbidden) {
		t.Errorf("BI-010c: got %v; want ErrWorkflowLabelWriteForbidden", err)
	}
}

// ─── Daemon-context bypass tests ──────────────────────────────────────────────

// TestBI010c_DaemonContextSingleLabelPermitted verifies that a daemon-context
// caller writing the workflow:single label receives nil — daemon-side writes
// are permitted per BI-010c.
//
// This is the second acceptance criterion from the bead body:
// "daemon-context write succeeds".
//
// Spec ref: specs/beads-integration.md §4.3 BI-010c.
func TestBI010c_DaemonContextSingleLabelPermitted(t *testing.T) {
	t.Parallel()

	err := brcli.CheckWorkflowLabelWrite(brcli.CallerKindDaemon, wlwFixtureDaemonArgs("workflow:single"))
	if err != nil {
		t.Errorf("BI-010c: expected nil for daemon-context workflow:single write, got %v", err)
	}
}

// TestBI010c_DaemonContextReviewLoopLabelPermitted verifies that a daemon-context
// caller writing the workflow:review-loop label receives nil.
//
// Spec ref: specs/beads-integration.md §4.3 BI-010c.
func TestBI010c_DaemonContextReviewLoopLabelPermitted(t *testing.T) {
	t.Parallel()

	err := brcli.CheckWorkflowLabelWrite(brcli.CallerKindDaemon, wlwFixtureDaemonArgs("workflow:review-loop"))
	if err != nil {
		t.Errorf("BI-010c: expected nil for daemon-context workflow:review-loop write, got %v", err)
	}
}

// ─── Benign-args tests ────────────────────────────────────────────────────────

// TestBI010c_AgentContextBenignArgsPermitted verifies that an agent-context
// caller issuing a br update without any workflow:<mode> label receives nil —
// the guard only fires on label mutations in the workflow: namespace.
//
// Spec ref: specs/beads-integration.md §4.3 BI-010c.
func TestBI010c_AgentContextBenignArgsPermitted(t *testing.T) {
	t.Parallel()

	err := brcli.CheckWorkflowLabelWrite(brcli.CallerKindAgent, wlwFixtureBenignArgs())
	if err != nil {
		t.Errorf("BI-010c: expected nil for agent-context write without workflow label, got %v", err)
	}
}

// TestBI010c_AgentContextEmptyArgsPermitted verifies that an empty argv slice
// does not trigger the guard.
//
// Spec ref: specs/beads-integration.md §4.3 BI-010c.
func TestBI010c_AgentContextEmptyArgsPermitted(t *testing.T) {
	t.Parallel()

	err := brcli.CheckWorkflowLabelWrite(brcli.CallerKindAgent, []string{})
	if err != nil {
		t.Errorf("BI-010c: expected nil for empty argv, got %v", err)
	}
}

// ─── Zero-value safety test ───────────────────────────────────────────────────

// TestBI010c_ZeroValueCallerKindIsAgent verifies that the zero value of
// CallerKind is CallerKindAgent — the more restrictive context. A caller that
// forgets to set the field is subject to the write prohibition rather than
// silently bypassing it.
//
// Spec ref: specs/beads-integration.md §4.3 BI-010c.
func TestBI010c_ZeroValueCallerKindIsAgent(t *testing.T) {
	t.Parallel()

	var kind brcli.CallerKind // zero value

	// A workflow-label write on the zero-value kind MUST be rejected.
	err := brcli.CheckWorkflowLabelWrite(kind, wlwFixtureAgentArgs("workflow:single"))
	if err == nil {
		t.Fatal("BI-010c: zero-value CallerKind did not reject workflow-label write; CallerKindAgent must be the zero value")
	}
	if !errors.Is(err, brcli.ErrWorkflowLabelWriteForbidden) {
		t.Errorf("BI-010c: got %v; want ErrWorkflowLabelWriteForbidden", err)
	}
}

// ─── Spec-content sensor ──────────────────────────────────────────────────────

// TestBI010c_SpecContainsWorkflowLabelDiscipline verifies that the BI-010c
// section of specs/beads-integration.md encodes the write-discipline invariant
// with required canonical phrases. Protects against spec drift that would
// silently un-anchor the enforcement.
//
// Phrases required:
//   - "BI-010c" — the requirement ID anchor
//   - "workflow:<" — the label namespace prefix (any mode value)
//   - "MUST NOT" — the normative prohibition
//
// Spec ref: specs/beads-integration.md §4.3 BI-010c.
func TestBI010c_SpecContainsWorkflowLabelDiscipline(t *testing.T) {
	t.Parallel()

	para := wlwFixtureSpecContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "workflow:<",
			hint:   "BI-010c must reference the workflow:<mode> label namespace",
		},
		{
			phrase: "MUST NOT",
			hint:   "BI-010c must use normative MUST NOT to prohibit agent-side label mutations",
		},
		{
			phrase: "br update",
			hint:   "BI-010c must name br update as the forbidden label-mutation surface",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(para, tc.phrase) {
			t.Errorf(
				"BI-010c spec paragraph does not contain %q — %s\nParagraph:\n%s",
				tc.phrase, tc.hint, para,
			)
		}
	}
}
