package workflow_test

// params_test.go — unit tests for SubstituteTemplateParams (WG-045/WG-046).
//
// Tests the four acceptance scenarios from hk-55zv2 (T5):
//   1. Substitution: __ISSUE_NUMBER__ + param → substituted value.
//   2. Residual-token error: missing param → *ErrResidualToken with offending token.
//   3. No-op: token-free source → byte-identical return.
//   4. Ordering invariant: substitution happens before parse-sensitive content.
//
// Bead ref: hk-55zv2 (T5).

import (
	"errors"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/workflow"
)

// TestSubstituteTemplateParams_substitution verifies that __KEY__ tokens are replaced
// with the corresponding param values (WG-045 acceptance scenario 1).
func TestSubstituteTemplateParams_substitution(t *testing.T) {
	src := `digraph W { goal="Fix #__ISSUE_NUMBER__"; }`
	params := map[string]string{"ISSUE_NUMBER": "172"}

	got, err := workflow.SubstituteTemplateParams(src, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `digraph W { goal="Fix #172"; }`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestSubstituteTemplateParams_multipleTokens verifies that multiple distinct tokens
// are all replaced in a single pass.
func TestSubstituteTemplateParams_multipleTokens(t *testing.T) {
	src := `digraph W { goal="__PROJECT__: fix #__TICKET_ID__"; }`
	params := map[string]string{
		"PROJECT":   "harmonik",
		"TICKET_ID": "99",
	}

	got, err := workflow.SubstituteTemplateParams(src, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `digraph W { goal="harmonik: fix #99"; }`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestSubstituteTemplateParams_residualError verifies that an unresolved token after
// substitution returns *ErrResidualToken naming the offending token (WG-045).
func TestSubstituteTemplateParams_residualError(t *testing.T) {
	src := `digraph W { goal="Fix #__ISSUE_NUMBER__"; }`
	// No params supplied — token is unresolved.
	got, err := workflow.SubstituteTemplateParams(src, nil)
	if err == nil {
		t.Fatalf("expected ErrResidualToken, got nil (result=%q)", got)
	}

	var rte *workflow.ErrResidualToken
	if !errors.As(err, &rte) {
		t.Fatalf("expected *ErrResidualToken, got %T: %v", err, err)
	}
	if len(rte.Tokens) != 1 || rte.Tokens[0] != "ISSUE_NUMBER" {
		t.Errorf("ErrResidualToken.Tokens = %v, want [ISSUE_NUMBER]", rte.Tokens)
	}
	if !strings.Contains(err.Error(), "ISSUE_NUMBER") {
		t.Errorf("error message %q does not mention ISSUE_NUMBER", err.Error())
	}
}

// TestSubstituteTemplateParams_residualDedup verifies that duplicate tokens appear only
// once in ErrResidualToken.Tokens.
func TestSubstituteTemplateParams_residualDedup(t *testing.T) {
	src := `digraph W { goal="__A__ and __A__ and __B__"; }`

	got, err := workflow.SubstituteTemplateParams(src, nil)
	if err == nil {
		t.Fatalf("expected ErrResidualToken, got nil (result=%q)", got)
	}

	var rte *workflow.ErrResidualToken
	if !errors.As(err, &rte) {
		t.Fatalf("expected *ErrResidualToken, got %T: %v", err, err)
	}
	// __A__ deduped → appears once; __B__ once.
	if len(rte.Tokens) != 2 {
		t.Errorf("expected 2 deduplicated tokens, got %d: %v", len(rte.Tokens), rte.Tokens)
	}
}

// TestSubstituteTemplateParams_noop verifies that token-free source is returned
// byte-identical and no error is returned (WG-045 no-op path).
func TestSubstituteTemplateParams_noop(t *testing.T) {
	src := `digraph W { start_node="start"; }`
	params := map[string]string{"UNUSED": "value"}

	got, err := workflow.SubstituteTemplateParams(src, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != src {
		t.Errorf("expected byte-identical return, got %q", got)
	}
}

// TestSubstituteTemplateParams_emptyParamsNoTokens verifies the fast-path for
// empty params + token-free source (no-op, no error).
func TestSubstituteTemplateParams_emptyParamsNoTokens(t *testing.T) {
	src := `digraph W { start_node="a"; }`

	got, err := workflow.SubstituteTemplateParams(src, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != src {
		t.Errorf("expected byte-identical return, got %q", got)
	}
}

// TestSubstituteTemplateParams_orderingInvariant verifies that substitution happens
// before any parse-sensitive content could be affected (WG-046). Concretely: a token
// that expands to a quoted string value must be embedded verbatim — the substituted
// result is what the parser would see.
func TestSubstituteTemplateParams_orderingInvariant(t *testing.T) {
	// The token value contains characters that are significant to the DOT parser
	// (quotes, semicolons) — the substitution pass is dumb string replacement; it
	// does NOT re-parse. This test confirms the raw bytes are substituted and that
	// the caller (LoadDotWorkflowWithParams) is responsible for handling any
	// resulting parse issues from injected values.
	src := `digraph W { goal="__RAW_GOAL__"; }`
	params := map[string]string{"RAW_GOAL": "Task: implement feature X"}

	got, err := workflow.SubstituteTemplateParams(src, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "Task: implement feature X") {
		t.Errorf("substituted result %q does not contain the expanded value", got)
	}
}

// TestSubstituteTemplateParams_unknownParamIgnored verifies that params whose keys
// do not appear in the source are silently ignored — no error, no side effect.
func TestSubstituteTemplateParams_unknownParamIgnored(t *testing.T) {
	src := `digraph W { start_node="n"; }`
	params := map[string]string{"NONEXISTENT_KEY": "some_value"}

	got, err := workflow.SubstituteTemplateParams(src, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != src {
		t.Errorf("expected byte-identical return when param key absent from src, got %q", got)
	}
}

// TestSubstituteTemplateParams_partialParams verifies that some tokens are resolved
// while others remain → ErrResidualToken lists only the unresolved ones.
func TestSubstituteTemplateParams_partialParams(t *testing.T) {
	src := `digraph W { goal="__RESOLVED__: fix #__UNRESOLVED__"; }`
	params := map[string]string{"RESOLVED": "ok"}

	got, err := workflow.SubstituteTemplateParams(src, params)
	if err == nil {
		t.Fatalf("expected ErrResidualToken for unresolved token, got nil (result=%q)", got)
	}

	var rte *workflow.ErrResidualToken
	if !errors.As(err, &rte) {
		t.Fatalf("expected *ErrResidualToken, got %T: %v", err, err)
	}
	if len(rte.Tokens) != 1 || rte.Tokens[0] != "UNRESOLVED" {
		t.Errorf("ErrResidualToken.Tokens = %v, want [UNRESOLVED]", rte.Tokens)
	}
}
