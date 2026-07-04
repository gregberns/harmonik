package main

// agent_brief_hkj784q_test.go — unit tests for `harmonik agent brief` arg parsing.
// Bead: hk-j784q (T3 — brief command + boot-document ORDER).

import "testing"

func TestResolveAgentBriefArgs_AgentFlag(t *testing.T) {
	t.Parallel()
	got := resolveAgentBriefArgs([]string{"--agent", "leto"})
	if got.agentName != "leto" {
		t.Errorf("agentName = %q, want %q", got.agentName, "leto")
	}
	if got.usageErr != "" || got.showHelp {
		t.Errorf("unexpected: err=%q help=%v", got.usageErr, got.showHelp)
	}
}

func TestResolveAgentBriefArgs_AgentFlagEquals(t *testing.T) {
	t.Parallel()
	got := resolveAgentBriefArgs([]string{"--agent=leto"})
	if got.agentName != "leto" {
		t.Errorf("agentName = %q, want %q", got.agentName, "leto")
	}
}

func TestResolveAgentBriefArgs_WakeFlag(t *testing.T) {
	t.Parallel()
	got := resolveAgentBriefArgs([]string{"--wake", "keeper-restart"})
	if got.wake != "keeper-restart" {
		t.Errorf("wake = %q, want %q", got.wake, "keeper-restart")
	}
}

func TestResolveAgentBriefArgs_WakeFlagEquals(t *testing.T) {
	t.Parallel()
	got := resolveAgentBriefArgs([]string{"--wake=trigger:priorities-report"})
	if got.wake != "trigger:priorities-report" {
		t.Errorf("wake = %q, want %q", got.wake, "trigger:priorities-report")
	}
}

func TestResolveAgentBriefArgs_FormatFlag(t *testing.T) {
	t.Parallel()
	got := resolveAgentBriefArgs([]string{"--format", "json"})
	if got.format != "json" {
		t.Errorf("format = %q, want %q", got.format, "json")
	}
}

func TestResolveAgentBriefArgs_FormatFlagEquals(t *testing.T) {
	t.Parallel()
	got := resolveAgentBriefArgs([]string{"--format=yaml"})
	if got.format != "yaml" {
		t.Errorf("format = %q, want %q", got.format, "yaml")
	}
}

func TestResolveAgentBriefArgs_ProjectFlag(t *testing.T) {
	t.Parallel()
	got := resolveAgentBriefArgs([]string{"--project", "/some/dir"})
	if got.projectFlag != "/some/dir" {
		t.Errorf("projectFlag = %q, want %q", got.projectFlag, "/some/dir")
	}
}

func TestResolveAgentBriefArgs_ProjectFlagEquals(t *testing.T) {
	t.Parallel()
	got := resolveAgentBriefArgs([]string{"--project=/other/dir"})
	if got.projectFlag != "/other/dir" {
		t.Errorf("projectFlag = %q, want %q", got.projectFlag, "/other/dir")
	}
}

func TestResolveAgentBriefArgs_Override(t *testing.T) {
	t.Parallel()
	got := resolveAgentBriefArgs([]string{"--override"})
	if !got.override {
		t.Error("expected override=true")
	}
}

func TestResolveAgentBriefArgs_Help(t *testing.T) {
	t.Parallel()
	got := resolveAgentBriefArgs([]string{"--help"})
	if !got.showHelp {
		t.Error("expected showHelp=true for --help")
	}
}

func TestResolveAgentBriefArgs_HelpShort(t *testing.T) {
	t.Parallel()
	got := resolveAgentBriefArgs([]string{"-h"})
	if !got.showHelp {
		t.Error("expected showHelp=true for -h")
	}
}

func TestResolveAgentBriefArgs_EmptyArgs(t *testing.T) {
	t.Parallel()
	got := resolveAgentBriefArgs(nil)
	if got.agentName != "" || got.wake != "" || got.format != "" || got.showHelp || got.usageErr != "" {
		t.Errorf("empty args: got %+v", got)
	}
}

func TestResolveAgentBriefArgs_UnknownFlag(t *testing.T) {
	t.Parallel()
	got := resolveAgentBriefArgs([]string{"--unknown"})
	if got.usageErr == "" {
		t.Error("expected usageErr for unknown flag")
	}
}

func TestResolveAgentBriefArgs_PositionalRejected(t *testing.T) {
	t.Parallel()
	got := resolveAgentBriefArgs([]string{"leto"})
	if got.usageErr == "" {
		t.Error("expected usageErr for unexpected positional argument")
	}
}

func TestResolveAgentBriefArgs_AllFlags(t *testing.T) {
	t.Parallel()
	got := resolveAgentBriefArgs([]string{
		"--agent", "leto",
		"--wake", "fresh",
		"--format", "toon",
		"--project", "/proj",
		"--override",
	})
	if got.agentName != "leto" {
		t.Errorf("agentName = %q, want %q", got.agentName, "leto")
	}
	if got.wake != "fresh" {
		t.Errorf("wake = %q, want %q", got.wake, "fresh")
	}
	if got.format != "toon" {
		t.Errorf("format = %q, want %q", got.format, "toon")
	}
	if got.projectFlag != "/proj" {
		t.Errorf("projectFlag = %q, want %q", got.projectFlag, "/proj")
	}
	if !got.override {
		t.Error("expected override=true")
	}
	if got.usageErr != "" || got.showHelp {
		t.Errorf("unexpected: err=%q help=%v", got.usageErr, got.showHelp)
	}
}
