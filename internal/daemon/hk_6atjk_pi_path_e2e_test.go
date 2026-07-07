package daemon_test

// hk_6atjk_pi_path_e2e_test.go — ISOLATED end-to-end regression for the
// Pi-empty-PATH bug (hk-6atjk, codename:pi-model-leak), driving the REAL daemon
// launch path (routedLaunchSpecBuilder → PiHarness.LaunchSpec → buildPiLaunchSpec
// → buildPiEnv).
//
// # The bug it reproduces
//
// The pi harness launches via the EXEC substrate; handler.go does
// `cmd.Env = spec.Env`, FULLY replacing the child environment. buildPiEnv only
// passes through the daemon's handlerEnv (baseEnv), but cfg.HandlerEnv is never
// populated at the composition root, so baseEnv arrives with NO PATH. Without a
// PATH the pi CLI's `#!/usr/bin/env node` shebang resolves against the libc
// default PATH (/usr/bin:/bin), which excludes /opt/homebrew/bin where node
// lives → `env: node: No such file or directory` (exit 127), HEAD never advances.
//
// # What "real launch path" means here
//
// The RunCtx.BaseEnv carries NO PATH (mirroring production, where handlerEnv has
// none), and the assertion reads spec.Env — the exact env handler.go will hand to
// cmd.Env. Nothing is stubbed except temp dirs and the dummy provider key.
//
// Helper prefix: hk6atjkE2E (per implementer-protocol.md §Helper-prefix discipline).
// Reuses hkpkuguE2EKeyFile from hk_pkugu_pi_launch_e2e_test.go (same package).
//
// Bead: hk-6atjk.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// hk6atjkE2EEnvValue returns the value of the first KEY=VALUE entry in env whose
// key == want, plus whether it was present.
func hk6atjkE2EEnvValue(env []string, want string) (string, bool) {
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 && kv[:i] == want {
			return kv[i+1:], true
		}
	}
	return "", false
}

// hk6atjkE2ERunCtx builds a pi single-mode initial-turn RunCtx with a caller-
// controlled BaseEnv (so the test can drive the PATH-present and PATH-absent
// cases through the real launch path).
func hk6atjkE2ERunCtx(t *testing.T, ws string, baseEnv []string) daemon.ExportedClaudeRunCtx {
	t.Helper()
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("NewV7: %v", err)
	}
	return daemon.ExportedClaudeRunCtx{
		RunID:         core.RunID(runUID),
		BeadID:        "hk-6atjk-e2e-bead",
		WorkspacePath: ws,
		DaemonSocket:  "/tmp/harmonik-hk-6atjk-e2e.sock",
		WorkflowMode:  core.WorkflowModeSingle,
		HandlerBinary: "claude",
		BaseEnv:       baseEnv,
	}
}

// TestHk6atjkPiLaunchPath_ChildGetsWorkingPath is the ISOLATED e2e regression:
// a pi-resolved run whose BaseEnv carries NO PATH must still yield a spec.Env
// with a non-empty PATH (falling back to the daemon process PATH), so the pi
// CLI's node shebang resolves. And when BaseEnv DOES carry a PATH, that exact
// value is preserved (never overridden by the process PATH).
func TestHk6atjkPiLaunchPath_ChildGetsWorkingPath(t *testing.T) {
	// Not t.Parallel: t.Setenv (process PATH sentinel + hermetic HOME for the
	// PI-042 on-disk credential check).
	t.Setenv("HOME", t.TempDir())

	const sentinelPath = "/hk6atjk/sentinel/bin:/usr/bin:/bin"
	t.Setenv("PATH", sentinelPath)

	ctx := context.Background()
	bus := eventbus.NewBusImpl()
	bead := core.BeadRecord{
		BeadID: "hk-6atjk-e2e-bead",
		Title:  "pi empty-PATH e2e bead",
		Labels: nil,
	}

	piCfg := daemon.PiHarnessConfig{
		Provider:   "ornith",
		Model:      "ornith",
		APIKeyEnv:  "HK_6ATJK_PI_KEY",
		APIKeyFile: hkpkuguE2EKeyFile(t),
		BaseURL:    "http://127.0.0.1:8551/v1",
		API:        "openai-completions",
	}
	reg, err := daemon.ExportedNewHarnessRegistryWithPi(piCfg)
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistryWithPi: %v", err)
	}
	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""), core.AgentType(""), core.AgentTypePi,
		bus,
	)

	// ── Case 1: BaseEnv has NO PATH → child must get the process PATH fallback ──
	specNoPath, _, err := build(ctx, hk6atjkE2ERunCtx(t, t.TempDir(),
		[]string{"HARMONIK_PROJECT_HASH=deadbeef123456"}))
	if err != nil {
		t.Fatalf("routed launch spec build (no-PATH baseEnv): %v", err)
	}
	got, ok := hk6atjkE2EEnvValue(specNoPath.Env, "PATH")
	if !ok || got == "" {
		t.Fatalf("pi child spec.Env has no non-empty PATH — the bug is back; env=%v", specNoPath.Env)
	}
	if got != sentinelPath {
		t.Errorf("pi child PATH = %q; want the daemon process PATH fallback %q", got, sentinelPath)
	}

	// ── Case 2: BaseEnv HAS a PATH → it is preserved verbatim, not overridden ──
	const customPath = "/custom/only/bin"
	specWithPath, _, err := build(ctx, hk6atjkE2ERunCtx(t, t.TempDir(),
		[]string{"HARMONIK_PROJECT_HASH=deadbeef123456", "PATH=" + customPath}))
	if err != nil {
		t.Fatalf("routed launch spec build (with-PATH baseEnv): %v", err)
	}
	got2, ok2 := hk6atjkE2EEnvValue(specWithPath.Env, "PATH")
	if !ok2 {
		t.Fatalf("pi child spec.Env dropped the baseEnv PATH; env=%v", specWithPath.Env)
	}
	if got2 != customPath {
		t.Errorf("pi child PATH = %q; want the preserved baseEnv PATH %q (must not be overridden by process PATH)", got2, customPath)
	}
	// Exactly one PATH entry (no duplicate append).
	n := 0
	for _, kv := range specWithPath.Env {
		if strings.HasPrefix(kv, "PATH=") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("pi child spec.Env has %d PATH entries; want exactly 1; env=%v", n, specWithPath.Env)
	}
}
