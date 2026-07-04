package main

// agent_check_hk9cheh_test.go — unit tests for `harmonik agent check` arg parsing.
// Bead: hk-9cheh (T5 schema-check verb).

import "testing"

func TestResolveAgentCheckArgs_TypePositional(t *testing.T) {
	t.Parallel()
	typ, proj, help, errMsg := resolveAgentCheckArgs([]string{"crew"})
	if typ != "crew" {
		t.Errorf("typeName = %q, want %q", typ, "crew")
	}
	if proj != "" || help || errMsg != "" {
		t.Errorf("unexpected: proj=%q help=%v err=%q", proj, help, errMsg)
	}
}

func TestResolveAgentCheckArgs_ProjectFlag(t *testing.T) {
	t.Parallel()
	typ, proj, help, errMsg := resolveAgentCheckArgs([]string{"crew", "--project", "/tmp/proj"})
	if typ != "crew" {
		t.Errorf("typeName = %q, want %q", typ, "crew")
	}
	if proj != "/tmp/proj" {
		t.Errorf("projectFlag = %q, want %q", proj, "/tmp/proj")
	}
	if help || errMsg != "" {
		t.Errorf("unexpected: help=%v err=%q", help, errMsg)
	}
}

func TestResolveAgentCheckArgs_ProjectFlagEquals(t *testing.T) {
	t.Parallel()
	_, proj, _, errMsg := resolveAgentCheckArgs([]string{"crew", "--project=/some/dir"})
	if proj != "/some/dir" {
		t.Errorf("projectFlag = %q, want %q", proj, "/some/dir")
	}
	if errMsg != "" {
		t.Errorf("unexpected error: %q", errMsg)
	}
}

func TestResolveAgentCheckArgs_Help(t *testing.T) {
	t.Parallel()
	_, _, help, _ := resolveAgentCheckArgs([]string{"--help"})
	if !help {
		t.Error("expected showHelp=true")
	}
}

func TestResolveAgentCheckArgs_HelpShort(t *testing.T) {
	t.Parallel()
	_, _, help, _ := resolveAgentCheckArgs([]string{"-h"})
	if !help {
		t.Error("expected showHelp=true for -h")
	}
}

func TestResolveAgentCheckArgs_EmptyArgs(t *testing.T) {
	t.Parallel()
	typ, proj, help, errMsg := resolveAgentCheckArgs(nil)
	if typ != "" || proj != "" || help || errMsg != "" {
		t.Errorf("empty args: got typ=%q proj=%q help=%v err=%q", typ, proj, help, errMsg)
	}
}

func TestResolveAgentCheckArgs_DuplicateType(t *testing.T) {
	t.Parallel()
	_, _, _, errMsg := resolveAgentCheckArgs([]string{"crew", "captain"})
	if errMsg == "" {
		t.Error("expected usage error for duplicate type arg, got none")
	}
}

func TestResolveAgentCheckArgs_UnknownFlag(t *testing.T) {
	t.Parallel()
	_, _, _, errMsg := resolveAgentCheckArgs([]string{"crew", "--unknown"})
	if errMsg == "" {
		t.Error("expected usage error for unknown flag, got none")
	}
}
