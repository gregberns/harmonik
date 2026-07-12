package evalvol

import "testing"

func TestIsBalanced(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", true},
		{"simple parens", "()", true},
		{"simple brackets", "[]", true},
		{"simple braces", "{}", true},
		{"nested mixed", "([{}])", true},
		{"sequential", "()[]{}", true},
		{"nested with text", "foo(bar[baz]{qux})", true},
		{"mismatched types", "(]", false},
		{"mismatched order", "([)]", false},
		{"unclosed", "(", false},
		{"unopened", ")", false},
		{"unopened after valid", "()]", false},
		{"deeply nested", "((([[[{{{}}}]]])))", true},
		{"deeply nested unbalanced", "((([[[{{{}}]]])))", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsBalanced(tc.in); got != tc.want {
				t.Errorf("IsBalanced(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
