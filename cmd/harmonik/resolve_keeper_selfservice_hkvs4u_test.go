package main

import (
	"testing"
)

// resolve_keeper_selfservice_hkvs4u_test.go — hk-vs4u: ResolveKeeperConfig threads
// self_service end-to-end and resolves crews_enabled UNSET→TRUE (operator decision:
// crews self-restart) while honoring an explicit false. self_service is OPTIONAL
// (not in the required-value set), so these start from completeTestKeeperConfig()
// (the required values) and override only the self_service fields under test.

func boolPtr(b bool) *bool { return &b }

func TestResolveKeeperConfig_CrewsEnabled_UnsetResolvesTrue(t *testing.T) {
	// SelfServiceCrewsEnabled is nil when the key is absent.
	cfg := completeTestKeeperConfig()
	cfg.SelfServiceCrewsEnabled = nil
	got, err := ResolveKeeperConfig(KeeperFlags{}, cfg, t.TempDir())
	if err != nil {
		t.Fatalf("resolve: unexpected error: %v", err)
	}
	if !got.SelfServiceCrewsEnabled {
		t.Errorf("crews_enabled UNSET must resolve to TRUE (crews self-restart by default); got false")
	}
}

func TestResolveKeeperConfig_CrewsEnabled_ExplicitFalseStaysFalse(t *testing.T) {
	cfg := completeTestKeeperConfig()
	cfg.SelfServiceCrewsEnabled = boolPtr(false)
	got, err := ResolveKeeperConfig(KeeperFlags{}, cfg, t.TempDir())
	if err != nil {
		t.Fatalf("resolve: unexpected error: %v", err)
	}
	if got.SelfServiceCrewsEnabled {
		t.Errorf("explicit crews_enabled=false must resolve to FALSE; got true")
	}
}

func TestResolveKeeperConfig_CrewsEnabled_ExplicitTrueStaysTrue(t *testing.T) {
	cfg := completeTestKeeperConfig()
	cfg.SelfServiceCrewsEnabled = boolPtr(true)
	got, err := ResolveKeeperConfig(KeeperFlags{}, cfg, t.TempDir())
	if err != nil {
		t.Fatalf("resolve: unexpected error: %v", err)
	}
	if !got.SelfServiceCrewsEnabled {
		t.Errorf("explicit crews_enabled=true must resolve to TRUE; got false")
	}
}

func TestResolveKeeperConfig_SelfServiceThreadedThrough(t *testing.T) {
	cfg := completeTestKeeperConfig()
	cfg.SelfServiceEnabled = true
	cfg.SelfServiceGraceSeconds = 45
	cfg.SelfServiceInstructOnlyWhenIdle = true
	cfg.DefaultWarnText = "lighter"
	cfg.ActionableWarnText = "do this thing harmonik keeper restart-now --agent x"
	got, err := ResolveKeeperConfig(KeeperFlags{}, cfg, t.TempDir())
	if err != nil {
		t.Fatalf("resolve: unexpected error: %v", err)
	}
	if !got.SelfServiceEnabled {
		t.Error("SelfServiceEnabled not threaded")
	}
	if got.SelfServiceGraceSeconds != 45 {
		t.Errorf("SelfServiceGraceSeconds: want 45, got %d", got.SelfServiceGraceSeconds)
	}
	if !got.SelfServiceInstructOnlyWhenIdle {
		t.Error("SelfServiceInstructOnlyWhenIdle not threaded")
	}
	if got.DefaultWarnText != "lighter" {
		t.Errorf("DefaultWarnText not threaded, got %q", got.DefaultWarnText)
	}
	if got.ActionableWarnText == "" {
		t.Error("ActionableWarnText not threaded")
	}
}
