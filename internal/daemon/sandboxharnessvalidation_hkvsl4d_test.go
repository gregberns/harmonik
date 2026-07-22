// hk-vsl4d: sandbox.harnesses must be validated against the real agent-type set
// at config load.
//
// THE DEFECT THIS GUARDS. SandboxConfig.Harnesses was taken verbatim from YAML —
// only sandbox.backend was ever checked. The list is consumed by HasHarness via
// an EXACT string compare against a run's agent type (sandboxgate.go
// sandboxSpawnForRun), so an entry naming no real agent type does not error and
// does not warn: it matches nothing, sandboxSpawnForRun returns nil, and the run
// executes UNSANDBOXED. The failure is silent and it is fail-OPEN.
//
// WHY IT IS A LIKELY MISTAKE RATHER THAN A THEORETICAL ONE. The real identifier
// is "claude-code". The natural thing to write in a config file is "claude". A
// config saying `harnesses: [claude]` reads to a human as "sandbox the claude
// runs" and sandboxes nothing at all, while presenting as a working sandbox
// configuration. That is false confidence in a security control, which is worse
// than no sandbox config at all — an absent block at least looks absent.
//
// WHY A SHAPE CHECK WOULD NOT HAVE CAUGHT IT, since that is the obvious fix and
// it does not work: core.AgentType.Valid() enforces the AR-025 regex only, and
// "claude" satisfies it. Valid() documents that it deliberately does not check
// membership. So the check has to be membership in core.ReservedAgentTypes().
package daemon_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// hkvsl4dWriteConfig writes a .harmonik/config.yaml carrying the given
// sandbox.harnesses list and returns the project dir.
func hkvsl4dWriteConfig(t *testing.T, harnessesYAML string) string {
	t.Helper()

	projectDir := t.TempDir()
	cfgDir := filepath.Join(projectDir, ".harmonik")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("hk-vsl4d: MkdirAll: %v", err)
	}
	body := "schema_version: 1\nsandbox:\n  backend: srt\n  harnesses:\n" + harnessesYAML
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("hk-vsl4d: WriteFile: %v", err)
	}
	return projectDir
}

// TestSandboxHarnesses_UnknownNameIsRejected_hkvsl4d is the regression guard for
// the defect itself: the plausible-but-wrong name must not load.
func TestSandboxHarnesses_UnknownNameIsRejected_hkvsl4d(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"claude", "Claude-Code", "codex-cli", "pi2"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			projectDir := hkvsl4dWriteConfig(t, "    - "+name+"\n")

			_, err := daemon.LoadProjectConfig(projectDir)
			if err == nil {
				t.Fatalf("hk-vsl4d: sandbox.harnesses[%q] LOADED WITHOUT ERROR. That is the "+
					"fail-open defect: HasHarness compares this against a run's agent type by "+
					"exact string match, so an unknown name silently sandboxes NOTHING while the "+
					"config reads as though it sandboxes something.", name)
			}
			if !strings.Contains(err.Error(), "sandbox.harnesses") {
				t.Errorf("hk-vsl4d: error does not name the offending field, so an operator "+
					"cannot tell what to fix. Got: %v", err)
			}
		})
	}
}

// TestSandboxHarnesses_ClaudeSuggestsClaudeCode_hkvsl4d pins the near-miss hint.
// The whole shape of this defect is a name that looks right, so the error has to
// say what to write instead — otherwise the operator re-reads their config,
// sees a sensible-looking word, and assumes the validator is wrong.
func TestSandboxHarnesses_ClaudeSuggestsClaudeCode_hkvsl4d(t *testing.T) {
	t.Parallel()

	// "claude-c" is an unambiguous prefix of claude-code only. Bare "claude" is a
	// prefix of BOTH claude-code and claude-twin, and the helper deliberately
	// stays silent when ambiguous rather than guessing — that case is covered by
	// the full-set listing asserted below.
	projectDir := hkvsl4dWriteConfig(t, "    - claude-c\n")

	_, err := daemon.LoadProjectConfig(projectDir)
	if err == nil {
		t.Fatal("hk-vsl4d: expected an error for \"claude-c\"")
	}
	if !strings.Contains(err.Error(), "claude-code") {
		t.Errorf("hk-vsl4d: error should name %q as the intended agent type. Got: %v",
			"claude-code", err)
	}
}

// TestSandboxHarnesses_AmbiguousPrefixListsFullSet_hkvsl4d covers the case the
// hint deliberately declines to guess. "claude" matches two reserved types, so
// the error must fall back to listing the set rather than picking one.
func TestSandboxHarnesses_AmbiguousPrefixListsFullSet_hkvsl4d(t *testing.T) {
	t.Parallel()

	projectDir := hkvsl4dWriteConfig(t, "    - claude\n")

	_, err := daemon.LoadProjectConfig(projectDir)
	if err == nil {
		t.Fatal("hk-vsl4d: expected an error for \"claude\"")
	}
	for _, want := range []string{"claude-code", "claude-twin", "pi", "codex"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("hk-vsl4d: ambiguous name must list the full valid set; %q missing. Got: %v",
				want, err)
		}
	}
}

// TestSandboxHarnesses_EveryReservedTypeIsAccepted_hkvsl4d is the other half of
// the guard, and it is the one that stops this fix from breaking real configs.
// A validator that rejects a name production actually uses is worse than no
// validator: it fails the daemon closed on a correct config. Driven off
// core.ReservedAgentTypes() so a newly-added agent type is covered automatically
// rather than silently unlisted.
func TestSandboxHarnesses_EveryReservedTypeIsAccepted_hkvsl4d(t *testing.T) {
	t.Parallel()

	for _, agentType := range core.ReservedAgentTypes() {
		t.Run(string(agentType), func(t *testing.T) {
			t.Parallel()

			projectDir := hkvsl4dWriteConfig(t, "    - "+string(agentType)+"\n")

			cfg, err := daemon.LoadProjectConfig(projectDir)
			if err != nil {
				t.Fatalf("hk-vsl4d: reserved agent type %q was REJECTED. This validation must "+
					"never refuse a name production uses — that turns a security fix into an "+
					"outage. Error: %v", agentType, err)
			}
			if !cfg.Sandbox.HasHarness(string(agentType)) {
				t.Errorf("hk-vsl4d: %q loaded but HasHarness is false — the entry was dropped "+
					"somewhere between parse and config, which would silently disable the "+
					"sandbox for it. Harnesses: %v", agentType, cfg.Sandbox.Harnesses)
			}
		})
	}
}

// TestSandboxHarnesses_RealProjectConfigStillLoads_hkvsl4d guards the actual
// deployed shape. This repo's own .harmonik/config.yaml uses `harnesses: [pi]`;
// if that ever failed to load, the daemon would not start.
func TestSandboxHarnesses_RealProjectConfigStillLoads_hkvsl4d(t *testing.T) {
	t.Parallel()

	projectDir := hkvsl4dWriteConfig(t, "    - pi\n")

	cfg, err := daemon.LoadProjectConfig(projectDir)
	if err != nil {
		t.Fatalf("hk-vsl4d: the production config shape (harnesses: [pi]) failed to load: %v", err)
	}
	if !cfg.Sandbox.HasHarness("pi") {
		t.Fatalf("hk-vsl4d: HasHarness(\"pi\") false after loading the production shape; got %v",
			cfg.Sandbox.Harnesses)
	}
	// The defect in one assertion: the wrong-but-plausible name must not be
	// treated as sandboxing anything.
	if cfg.Sandbox.HasHarness("claude") {
		t.Error("hk-vsl4d: HasHarness(\"claude\") true — the exact-match assumption this " +
			"validation rests on no longer holds, and the validation may now be pointless.")
	}
}

// TestReservedAgentTypes_MatchesDeclaredConstants_hkvsl4d pins the source of
// truth itself. If a constant is added to core without being added to
// ReservedAgentTypes, every consumer doing a membership check — including this
// bead's validation — would reject a legitimate agent type at config load.
func TestReservedAgentTypes_MatchesDeclaredConstants_hkvsl4d(t *testing.T) {
	t.Parallel()

	declared := []core.AgentType{
		core.AgentTypeClaudeCode,
		core.AgentTypePi,
		core.AgentTypeClaudeTwin,
		core.AgentTypePiTwin,
		core.AgentTypeCodex,
	}
	got := core.ReservedAgentTypes()

	if len(got) != len(declared) {
		t.Fatalf("hk-vsl4d: ReservedAgentTypes() has %d entries, %d constants are declared. "+
			"A missing entry makes config load reject a real agent type. got=%v", len(got), len(declared), got)
	}
	for _, want := range declared {
		if !want.Reserved() {
			t.Errorf("hk-vsl4d: declared constant %q is not in ReservedAgentTypes()", want)
		}
	}
	// And the negative case, so Reserved() cannot be trivially true.
	if core.AgentType("claude").Reserved() {
		t.Error("hk-vsl4d: AgentType(\"claude\").Reserved() is true — Reserved() is not " +
			"discriminating and every assertion in this file is vacuous.")
	}
}
