package main

import (
	"context"
	"testing"
)

func TestBuildKeeperCycleDepsOwnsCommandRuntimeCapabilities(t *testing.T) {
	cfg, _ := buildKeeperConfigs(ResolvedKeeperConfig{}, keeperBuildParams{})
	called := false
	deps := buildKeeperCycleDeps(cfg, nil, func(context.Context, string) error {
		called = true
		return nil
	})
	if _, ok := deps.Pane.(keeperPaneWithEscape); !ok {
		t.Fatalf("pane = %T, want command escape decorator", deps.Pane)
	}
	if deps.Respawn == nil {
		t.Fatal("force-restart capability was not wired")
	}
	if err := deps.Respawn.ForceRestart(context.Background(), "agent"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("force-restart dependency did not reach command function")
	}
}
