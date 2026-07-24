package main

// agent_cov_covf_test.go — coverage-drain chunk F: the `harmonik agent` command
// routing + the runAgentBrief / runAgentCheck validation surface that the
// existing agent_brief_/agent_check_ arg-parser tests do not reach.
//
// Covered (exit-code truth table for the pure validation legs):
//   - runAgentSubcommand   verb routing (help / unknown / brief / check)
//   - runAgentBrief        help(0), usage-error(1), missing-name(1),
//                          --agent vs $HARMONIK_AGENT conflict(1), bad-format(1)
//   - runAgentCheck        help(0), usage-error(1), missing-type(1), Check-defects(1)
//
// SKIPPED: the render/success path of runAgentBrief (needs a real
// .harmonik/agents/ manifest tree + crew.ResolveType + agentmanifest.BuildBootDoc)
// and the well-formed "ok" path of runAgentCheck — both are agentmanifest
// integration concerns, not this cluster's CLI logic.
//
// Uses t.Setenv + os.Stderr redirection → no t.Parallel.

import (
	"testing"
)

// TestRunAgentSubcommand_Routing covers the verb switch: help verbs exit 0, an
// unknown verb exits 2, and the brief/check verbs route to their handlers (here
// via deliberately-erroring args so no filesystem manifest is required).
func TestRunAgentSubcommand_Routing(t *testing.T) {
	silenceStderr(t)

	if code := runAgentSubcommand(nil); code != 0 {
		t.Fatalf("empty verb: exit = %d, want 0", code)
	}
	if code := runAgentSubcommand([]string{"--help"}); code != 0 {
		t.Fatalf("--help: exit = %d, want 0", code)
	}
	if code := runAgentSubcommand([]string{"bogus"}); code != 2 {
		t.Fatalf("unknown verb: exit = %d, want 2", code)
	}
	// brief routing: an unknown flag makes runAgentBrief exit 1 without touching
	// any manifest tree.
	if code := runAgentSubcommand([]string{"brief", "--nope"}); code != 1 {
		t.Fatalf("brief routing (bad flag): exit = %d, want 1", code)
	}
	// check routing: a missing type makes runAgentCheck exit 1.
	if code := runAgentSubcommand([]string{"check"}); code != 1 {
		t.Fatalf("check routing (missing type): exit = %d, want 1", code)
	}
}

// TestRunAgentBrief_Validation walks the pure validation legs of runAgentBrief.
func TestRunAgentBrief_Validation(t *testing.T) {
	silenceStderr(t)

	// Help short-circuits to exit 0.
	if code := runAgentBrief([]string{"--help"}); code != 0 {
		t.Fatalf("--help: exit = %d, want 0", code)
	}

	// A parse-level usage error exits 1.
	if code := runAgentBrief([]string{"--nope"}); code != 1 {
		t.Fatalf("unknown flag: exit = %d, want 1", code)
	}

	// Missing name (neither --agent nor $HARMONIK_AGENT) exits 1.
	t.Setenv("HARMONIK_AGENT", "")
	if code := runAgentBrief(nil); code != 1 {
		t.Fatalf("missing name: exit = %d, want 1", code)
	}

	// --agent conflicts with a differing $HARMONIK_AGENT (no --override) → exit 1.
	t.Setenv("HARMONIK_AGENT", "leto")
	if code := runAgentBrief([]string{"--agent", "paul"}); code != 1 {
		t.Fatalf("agent/env conflict: exit = %d, want 1", code)
	}

	// A bad --format is rejected before any manifest resolution → exit 1.
	t.Setenv("HARMONIK_AGENT", "")
	if code := runAgentBrief([]string{"--agent", "paul", "--format", "bogus"}); code != 1 {
		t.Fatalf("bad format: exit = %d, want 1", code)
	}
}

// TestRunAgentCheck_Validation covers runAgentCheck's arg legs plus the
// defect-listing exit (a nonexistent agents dir yields defects → exit 1).
func TestRunAgentCheck_Validation(t *testing.T) {
	silenceStderr(t)

	if code := runAgentCheck([]string{"--help"}); code != 0 {
		t.Fatalf("--help: exit = %d, want 0", code)
	}
	if code := runAgentCheck([]string{"--nope"}); code != 1 {
		t.Fatalf("unknown flag: exit = %d, want 1", code)
	}
	if code := runAgentCheck(nil); code != 1 {
		t.Fatalf("missing type: exit = %d, want 1", code)
	}
	// A type against a project with no .harmonik/agents/ tree yields validation
	// defects (not "ok") → exit 1.
	if code := runAgentCheck([]string{"crew", "--project", t.TempDir()}); code != 1 {
		t.Fatalf("check against empty project: exit = %d, want 1", code)
	}
}
