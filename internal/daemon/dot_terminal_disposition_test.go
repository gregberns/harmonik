package daemon

// dot_terminal_disposition_test.go — terminal-node disposition classification.
//
// Guards the rule that decides whether a DOT run's work is MERGED and its bead
// closed green. The classifier must read the graph, never a hardcoded terminal
// name: a graph whose failure terminal is not spelled "close-needs-attention"
// (eval-bead.dot declares terminal_node_ids="close-pass,close-fail") was
// previously classified as a success, so failing model-evaluation runs merged
// and closed green.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// evalShapedGraph is the eval-bead.dot terminal shape: two author-defined
// terminals, neither of which is one of the WG-022 reserved IDs.
const evalShapedGraph = `digraph "eval-bead" {
    schema_version="1";
    version="1.0";
    start_node="start";
    terminal_node_ids="close-pass,close-fail";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    grade [type="non-agentic", handler_ref="shell", idempotency_class="idempotent", tool_command="true"];
    "close-pass" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent", terminal_disposition="success"];
    "close-fail" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent", terminal_disposition="needs_attention"];

    start -> grade;
    grade -> "close-pass" [condition="outcome.status == 'SUCCESS'"];
    grade -> "close-fail";
}`

// standardShapedGraph is the WG-022 reserved terminal pair, declared with no
// terminal_disposition attribute at all — exactly as every shipped standard
// graph declares it today.
const standardShapedGraph = `digraph "standard-bead" {
    schema_version="1";
    version="1.0";
    start_node="start";
    terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    review [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> review;
    review -> close [condition="outcome.preferred_label == 'APPROVE'"];
    review -> "close-needs-attention";
}`

// undeclaredTerminalGraph has an author-defined terminal that declares no
// disposition — the graph cannot say whether reaching it is a success.
const undeclaredTerminalGraph = `digraph "gate-check" {
    schema_version="1";
    version="1.0";
    start_node="start";
    terminal_node_ids="tests,gate_fail";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    tests [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    gate_fail [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> tests [condition="outcome.status == 'SUCCESS'"];
    start -> gate_fail;
}`

func parseGraph(t *testing.T, src string) *dot.Graph {
	t.Helper()
	g, err := dot.Parse(src, "disposition_test")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if g == nil {
		t.Fatal("Parse returned nil graph")
	}
	return g
}

// TestTerminalDisposition_EvalShapedGraph is the regression that motivated the
// change: eval-bead.dot's failure terminal is "close-fail", and classifying it
// by the single-literal denylist `id != "close-needs-attention"` returned
// success, merging failed eval work and closing the bead green.
func TestTerminalDisposition_EvalShapedGraph(t *testing.T) {
	g := parseGraph(t, evalShapedGraph)

	if got, why := dotTerminalNodeIsSuccess(g, "close-fail"); got {
		t.Errorf("close-fail classified as SUCCESS (why=%q); a failed grade must not merge", why)
	}
	if got, why := dotTerminalNodeIsSuccess(g, "close-pass"); !got {
		t.Errorf("close-pass classified as needs-attention (why=%q); a passing grade must merge", why)
	}
}

// TestTerminalDisposition_StandardGraphUnchanged pins the WG-022 reserved pair
// in both directions. These graphs carry no terminal_disposition attribute, so
// the reserved IDs alone must keep producing today's answers.
func TestTerminalDisposition_StandardGraphUnchanged(t *testing.T) {
	g := parseGraph(t, standardShapedGraph)

	if got, why := dotTerminalNodeIsSuccess(g, "close"); !got {
		t.Errorf("close classified as needs-attention (why=%q); WG-022 reserves it as normal completion", why)
	}
	if got, why := dotTerminalNodeIsSuccess(g, "close-needs-attention"); got {
		t.Errorf("close-needs-attention classified as SUCCESS (why=%q); WG-022 reserves it as the attention close", why)
	}
}

// TestTerminalDisposition_ReservedIDsWinOverAttribute: WG-022 reserves the two
// IDs normatively, so a graph cannot redefine them out from under the daemon.
func TestTerminalDisposition_ReservedIDsWinOverAttribute(t *testing.T) {
	src := `digraph "contradictory" {
    schema_version="1";
    version="1.0";
    start_node="start";
    terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent", terminal_disposition="needs_attention"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent", terminal_disposition="success"];

    start -> close [condition="outcome.status == 'SUCCESS'"];
    start -> "close-needs-attention";
}`
	g := parseGraph(t, src)

	if got, _ := dotTerminalNodeIsSuccess(g, "close"); !got {
		t.Error("close: reserved WG-022 meaning must outrank a contradictory terminal_disposition")
	}
	if got, _ := dotTerminalNodeIsSuccess(g, "close-needs-attention"); got {
		t.Error("close-needs-attention: reserved WG-022 meaning must outrank a contradictory terminal_disposition")
	}
}

// TestTerminalDisposition_UndeclaredIsNotSuccess: when the graph does not say,
// the daemon must not guess "success" from the node's name. It reports that it
// cannot classify and routes to needs-attention, which is recoverable; merging
// unclassifiable work is not.
func TestTerminalDisposition_UndeclaredIsNotSuccess(t *testing.T) {
	g := parseGraph(t, undeclaredTerminalGraph)

	for _, id := range []string{"tests", "gate_fail"} {
		got, why := dotTerminalNodeIsSuccess(g, id)
		if got {
			t.Errorf("terminal %q with no declared disposition classified as SUCCESS; must not merge on a guess", id)
		}
		if why == "" {
			t.Errorf("terminal %q: expected a reason explaining the unclassifiable terminal, got empty", id)
		}
	}
}

// TestTerminalDisposition_InvalidValueIsNotSuccess: a typo'd disposition is an
// authoring error, not a licence to merge.
func TestTerminalDisposition_InvalidValueIsNotSuccess(t *testing.T) {
	src := `digraph "typo" {
    schema_version="1";
    version="1.0";
    start_node="start";
    terminal_node_ids="done";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    done [type="non-agentic", handler_ref="noop", idempotency_class="idempotent", terminal_disposition="ok"];

    start -> done;
}`
	g := parseGraph(t, src)

	got, why := dotTerminalNodeIsSuccess(g, "done")
	if got {
		t.Error("terminal with an invalid terminal_disposition classified as SUCCESS")
	}
	if why == "" {
		t.Error("expected a reason naming the invalid terminal_disposition value")
	}
}

// TestTerminalDisposition_ShippedStandardGraph runs the assertion against the
// graph the daemon actually embeds and falls back to, not a hand-written
// approximation of it.
func TestTerminalDisposition_ShippedStandardGraph(t *testing.T) {
	g := parseGraph(t, string(standardBeadDotSrc))

	if got, why := dotTerminalNodeIsSuccess(g, "close"); !got {
		t.Errorf("embedded standard-bead.dot: close success=false (why=%q)", why)
	}
	if got, why := dotTerminalNodeIsSuccess(g, "close-needs-attention"); got {
		t.Errorf("embedded standard-bead.dot: close-needs-attention success=true (why=%q)", why)
	}
}

// TestTerminalDisposition_ShippedEvalGraph runs the assertion against the
// repo-root eval-bead.dot itself — the graph whose failing runs were being
// merged. It reads the file rather than a copy so an un-annotated eval-bead.dot
// fails this test instead of silently regressing.
func TestTerminalDisposition_ShippedEvalGraph(t *testing.T) {
	g := parseGraph(t, readRepoFile(t, "eval-bead.dot"))

	if got, why := dotTerminalNodeIsSuccess(g, "close-fail"); got {
		t.Errorf("eval-bead.dot: close-fail success=true (why=%q); a failed eval must not merge", why)
	}
	if got, why := dotTerminalNodeIsSuccess(g, "close-pass"); !got {
		t.Errorf("eval-bead.dot: close-pass success=false (why=%q); a passing eval must merge", why)
	}
}

// TestTerminalDisposition_ExecutableGraphsAllClassify walks every graph this
// repo actually RUNS — the repo-root workflow graphs and the scenario-harness
// workflows — and requires each declared terminal to be classifiable. A new
// executable graph that declares a terminal outside the WG-022 reserved pair
// and forgets terminal_disposition fails here, at authoring time, instead of at
// the end of a real run.
//
// specs/examples/*.dot are deliberately out of scope: they are normative spec
// artifacts, and annotating them is a workflow-graph.md amendment, not a code
// change. Four of them (plan-review-loop, plan-review-finalize,
// sentry-triage-faithful, sub-workflow-commit-gate) declare non-reserved
// terminals and are unclassifiable until that amendment lands.
func TestTerminalDisposition_ExecutableGraphsAllClassify(t *testing.T) {
	root := repoRootForConformance()
	patterns := []string{
		filepath.Join(root, "*.dot"),
		filepath.Join(root, "scenarios", "_workflows", "*.dot"),
	}

	var paths []string
	for _, p := range patterns {
		matches, err := filepath.Glob(p)
		if err != nil {
			t.Fatalf("glob %s: %v", p, err)
		}
		paths = append(paths, matches...)
	}
	if len(paths) == 0 {
		t.Fatal("no executable .dot graphs found; the globs are wrong")
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path) //nolint:gosec // G304: repo-relative path, test-only
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			// Deliberately Fatal, not Skip. A graph that later stops parsing
			// would silently drop out of this sweep's coverage while the test
			// stayed green — the same shape as a -run filter that matches zero
			// tests and exits successfully.
			g, err := dot.Parse(string(src), path)
			if err != nil {
				t.Fatalf("graph does not parse: %v", err)
			}
			if len(g.TerminalNodeIDs) == 0 {
				t.Fatal("graph declares no terminal_node_ids; nothing was asserted")
			}
			for _, id := range g.TerminalNodeIDs {
				if _, why := dotTerminalNodeIsSuccess(g, id); why != "" {
					t.Errorf("terminal %q is unclassifiable: %s", id, why)
				}
			}
		})
	}
}
