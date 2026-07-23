package main

// graph_diagnostic_mapping_test.go — coverage for the error→diagnostic mapping
// behind `harmonik graph validate`.
//
// validateDot is what decides whether a workflow is reported as startable. Its
// parse-error branch classifies the error by TYPE, and the classification was
// converted from a bare type switch to errors.As so a wrapped parse error is
// still recognised. A regression there degrades a precise per-line
// em038_not_parseable list into one opaque blob, or — worse for the operator —
// reports a specific wrapped failure as if it were an unknown one.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/workflow/dot"
)

func TestDiagnosticDetail_PrefixesTheSourceLine(t *testing.T) {
	tests := []struct {
		name string
		diag dot.Diagnostic
		want string
	}{
		{
			name: "known line is prefixed",
			diag: dot.Diagnostic{Line: 12, Message: "node has no outgoing edge"},
			want: "dot:12: node has no outgoing edge",
		},
		{
			name: "line 1 is still prefixed",
			diag: dot.Diagnostic{Line: 1, Message: "bad header"},
			want: "dot:1: bad header",
		},
		{
			name: "unknown line (0) is left bare",
			diag: dot.Diagnostic{Line: 0, Message: "graph has no start node"},
			want: "graph has no start node",
		},
		{
			name: "a negative line is treated as unknown",
			diag: dot.Diagnostic{Line: -1, Message: "somewhere"},
			want: "somewhere",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := diagnosticDetail(tc.diag); got != tc.want {
				t.Errorf("diagnosticDetail = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestValidateDot_ParseErrorsBecomeOneDiagnosticEach pins the contract that a
// multi-error parse produces one em038_not_parseable entry PER error, not a
// single joined blob. That is the difference between the operator seeing three
// line numbers and seeing one run-on string.
func TestValidateDot_ParseErrorsBecomeOneDiagnosticEach(t *testing.T) {
	// Three distinct strict violations on three different lines.
	src := "digraph W {\n  a -> ;\n  -> b;\n  c - d;\n}\n"

	diags := validateDot(src)
	if len(diags) == 0 {
		t.Fatalf("expected at least one diagnostic for %q", src)
	}
	for _, d := range diags {
		if d.Code != "em038_not_parseable" {
			t.Errorf("parse failure produced code %q, want em038_not_parseable", d.Code)
		}
		if d.Detail == "" {
			t.Error("diagnostic has an empty detail")
		}
	}
	// A joined multi-error would surface as ONE diagnostic containing "; ",
	// which is exactly the shape the per-error fan-out exists to avoid.
	if len(diags) == 1 && strings.Contains(diags[0].Detail, "; dot:") {
		t.Errorf("multi-error was joined into one diagnostic instead of fanned out: %q", diags[0].Detail)
	}
}

func TestValidateDot_WellFormedGraphHasNoParseDiagnostic(t *testing.T) {
	src := "digraph W {\n  start -> done;\n}\n"
	for _, d := range validateDot(src) {
		if d.Code == "em038_not_parseable" {
			t.Errorf("well-formed graph reported as not-parseable: %q", d.Detail)
		}
	}
}

// TestValidateDot_EmptySourceIsReportedNotPanicked guards the degenerate input
// the CLI can be handed by an empty or truncated file.
func TestValidateDot_EmptySourceIsReportedNotPanicked(t *testing.T) {
	for _, src := range []string{"", "   ", "\n\n", "not dot at all"} {
		t.Run(fmt.Sprintf("%q", src), func(t *testing.T) {
			diags := validateDot(src)
			if len(diags) == 0 {
				t.Errorf("validateDot(%q) returned no diagnostics; malformed input must be reported", src)
			}
		})
	}
}

// TestDotParseErrorIdentity_SurvivesWrapping is the property validateDot's
// errors.As classification depends on. If dot.ParseError / dot.ParseErrors ever
// stop being reachable through errors.As, validateDot silently falls back to
// its default branch and collapses a per-line list into one entry — with no
// other test noticing.
func TestDotParseErrorIdentity_SurvivesWrapping(t *testing.T) {
	single := &dot.ParseError{Line: 4, Message: "unexpected token"}
	multi := dot.ParseErrors{single, {Line: 7, Message: "unterminated attribute"}}

	t.Run("single, wrapped", func(t *testing.T) {
		var got *dot.ParseError
		if !errors.As(fmt.Errorf("load workflow: %w", single), &got) {
			t.Fatal("errors.As failed to see a wrapped *dot.ParseError")
		}
		if got.Line != 4 {
			t.Errorf("Line = %d, want 4", got.Line)
		}
	})

	t.Run("multi, wrapped", func(t *testing.T) {
		var got dot.ParseErrors
		if !errors.As(fmt.Errorf("load workflow: %w", multi), &got) {
			t.Fatal("errors.As failed to see a wrapped dot.ParseErrors")
		}
		if len(got) != 2 {
			t.Errorf("len = %d, want 2", len(got))
		}
	})

	t.Run("ParseErrors is not mistaken for a single ParseError", func(t *testing.T) {
		var single *dot.ParseError
		if errors.As(error(multi), &single) {
			t.Error("dot.ParseErrors matched *dot.ParseError; validateDot's case order would then " +
				"collapse a multi-error into a single diagnostic")
		}
	})
}

func TestHarnessMatrixCellCount(t *testing.T) {
	tests := []struct {
		name   string
		matrix map[string][]string
		want   int
	}{
		{"nil matrix is one cell (the scenario itself)", nil, 1},
		{"empty matrix is one cell", map[string][]string{}, 1},
		{"single axis", map[string][]string{"model": {"a", "b", "c"}}, 3},
		{"cartesian product of two axes", map[string][]string{"model": {"a", "b"}, "harness": {"x", "y", "z"}}, 6},
		{"a single-valued axis does not change the count", map[string][]string{"model": {"a", "b"}, "only": {"x"}}, 2},
		{"a zero-length axis collapses the product to zero", map[string][]string{"model": {"a", "b"}, "empty": {}}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := harnessMatrixCellCount(tc.matrix); got != tc.want {
				t.Errorf("harnessMatrixCellCount(%v) = %d, want %d", tc.matrix, got, tc.want)
			}
		})
	}
}
