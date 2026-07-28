package projectconfig

// subsystems_test.go — the subsystems: block, driven through the real
// LoadProjectConfig edge (a .harmonik/config.yaml on disk), never through a
// hand-built SubsystemsConfig.
//
// Covers the three states an operator can land in:
//   - absent block / absent entry / absent enabled: → ENABLED (unchanged behaviour)
//   - explicit enabled: false                      → DISABLED (subsystem is absent)
//   - unknown subsystem name                       → FAIL LOUD (*ErrUnknownSubsystem)
//
// Helper prefix: subsys (implementer-protocol.md §Helper-prefix discipline).

import (
	"errors"
	"strings"
	"testing"
)

// Every "no explicit off" spelling must leave the subsystem ENABLED, so adding
// the subsystems: block cannot change an existing deployment's behaviour.
func TestSubsystems_DefaultsToEnabled(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"no config file at all": "",
		"no subsystems block":   "schema_version: 1\n",
		"entry with no enabled key": `
schema_version: 1
subsystems:
  reconciliation_scheduler: {}
`,
		"entry explicitly enabled": `
schema_version: 1
subsystems:
  reconciliation_scheduler:
    enabled: true
`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := projCfgFixtureDir(t, content)
			cfg, err := LoadProjectConfig(root)
			if err != nil {
				t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
			}
			if !cfg.Subsystems.Enabled(SubsystemReconciliationScheduler) {
				t.Errorf("Enabled(%s) = false; want true — %s must leave the subsystem on",
					SubsystemReconciliationScheduler, name)
			}
		})
	}
}

// An explicit enabled: false must switch the subsystem off.
func TestSubsystems_ExplicitFalseDisables(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
subsystems:
  reconciliation_scheduler:
    enabled: false
`)
	cfg, err := LoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Subsystems.Enabled(SubsystemReconciliationScheduler) {
		t.Errorf("Enabled(%s) = true; want false — subsystems.reconciliation_scheduler.enabled: false was not honoured",
			SubsystemReconciliationScheduler)
	}
}

// A typo'd subsystem name must FAIL LOUD. Silently ignoring it would leave the
// operator believing a subsystem was partitioned away while it kept running.
func TestSubsystems_UnknownNameFailsLoud(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
subsystems:
  reconcilliation_scheduler:
    enabled: false
`)
	_, err := LoadProjectConfig(root)
	if err == nil {
		t.Fatal("LoadProjectConfig: a misspelled subsystem name was silently accepted; want *ErrUnknownSubsystem")
	}
	var uerr *ErrUnknownSubsystem
	if !errors.As(err, &uerr) {
		t.Fatalf("error type = %T (%v); want *ErrUnknownSubsystem", err, err)
	}
	if uerr.Name != "reconcilliation_scheduler" {
		t.Errorf("ErrUnknownSubsystem.Name = %q; want %q", uerr.Name, "reconcilliation_scheduler")
	}
	// The message must name the accepted set, or the operator cannot fix the typo.
	if !strings.Contains(uerr.Error(), string(SubsystemReconciliationScheduler)) {
		t.Errorf("error message %q does not list the known subsystem names", uerr.Error())
	}
}

// A typo'd key INSIDE an entry must also fail loud. Silently ignoring it leaves
// the subsystem RUNNING while the operator believes it was switched off — the
// same failure mode as a typo'd name, reached from the other direction.
func TestSubsystems_UnknownEntryKeyFailsLoud(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, `
schema_version: 1
subsystems:
  reconciliation_scheduler:
    enbaled: false
`)
	_, err := LoadProjectConfig(root)
	if err == nil {
		t.Fatal("LoadProjectConfig: a misspelled 'enabled' key was silently ignored, leaving the subsystem on; want *ErrUnknownConfigKey")
	}
	var kerr *ErrUnknownConfigKey
	if !errors.As(err, &kerr) {
		t.Fatalf("error type = %T (%v); want *ErrUnknownConfigKey", err, err)
	}
	if want := "subsystems.reconciliation_scheduler.enbaled"; kerr.KeyPath != want {
		t.Errorf("ErrUnknownConfigKey.KeyPath = %q; want %q", kerr.KeyPath, want)
	}
	// The message must name the subsystems: block. ErrUnknownConfigKey was
	// originally keeper-only and hardcoded "keeper:", which would send the
	// operator to the wrong block.
	if msg := kerr.Error(); !strings.Contains(msg, "under subsystems:") || strings.Contains(msg, "keeper") {
		t.Errorf("error message = %q; want it to name the subsystems: block and not mention keeper", msg)
	}
}

// A `subsystems: {}` block carries no partitioning intent, so it must not defeat
// the RU-06 empty-file sentinel — matching `agents: {}` and `crews: {}`.
func TestSubsystems_EmptyBlockReadsAsAbsent(t *testing.T) {
	t.Parallel()

	root := projCfgFixtureDir(t, "subsystems: {}\n")
	cfg, err := LoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if !cfg.Subsystems.Enabled(SubsystemReconciliationScheduler) {
		t.Errorf("Enabled(%s) = false; want true for an empty subsystems: block", SubsystemReconciliationScheduler)
	}
}

// The zero value (no config was ever loaded) must enable everything — callers
// that leave ProjectCfg unset keep the pre-partitioning behaviour.
func TestSubsystems_ZeroValueEnablesEverything(t *testing.T) {
	t.Parallel()

	var zero SubsystemsConfig
	if !zero.Enabled(SubsystemReconciliationScheduler) {
		t.Errorf("zero SubsystemsConfig.Enabled(%s) = false; want true", SubsystemReconciliationScheduler)
	}
}
