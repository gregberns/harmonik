package main

import (
	"path/filepath"
	"testing"
)

// hk-snjr — KEEPER parser-parity SPLIT C, graph.go sibling sweep. `harmonik graph
// validate` previously let an unrecognized leading-dash token fall through and be
// treated as the <path>, surfacing only as an obscure exit-1 read error. These
// tests pin the loud exit-2 reject while proving recognized flags (--json) and
// genuine paths are preserved.

func TestGraphValidateRejectsUnrecognizedFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"leading-dash-bogus", []string{"--bogus"}},
		{"leading-dash-bogus-before-path", []string{"--bogus", "wf.dot"}},
		{"trailing-dash-bogus-after-path", []string{"wf.dot", "--bogus"}},
		{"single-dash-unknown", []string{"-x", "wf.dot"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code := runGraphValidate(tc.args); code != 2 {
				t.Fatalf("runGraphValidate(%v): exit = %d, want 2", tc.args, code)
			}
		})
	}
}

// TestGraphValidateKeepsRecognizedFlagAndPath proves the reject does NOT swallow
// the --json flag or a genuine path: a valid workflow validates (exit 0) and an
// unreadable path still surfaces the read error (exit 1), not the flag reject.
func TestGraphValidateKeepsRecognizedFlagAndPath(t *testing.T) {
	// validDOT is the package-level valid-workflow fixture (graph_validate_explore_hkw3eip_test.go).
	good := graphValidateFixtureWriteFile(t, "wf.dot", validDOT)

	if code := runGraphValidate([]string{"--json", good}); code != 0 {
		t.Fatalf("runGraphValidate([--json %s]) on a valid workflow: exit = %d, want 0", good, code)
	}

	missing := filepath.Join(t.TempDir(), "nope.dot")
	if code := runGraphValidate([]string{"--json", missing}); code != 1 {
		t.Fatalf("runGraphValidate([--json %s]) on a missing path: exit = %d, want 1 (read error, not flag reject)", missing, code)
	}
}
