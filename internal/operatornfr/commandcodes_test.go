package operatornfr_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/operatornfr"
)

// exitCodeWiringFixtureAllCommands lists every ON-001 command name that MUST
// appear in CommandExitCodeSets.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001 — "every operator-invoked
// harmonik command (daemon, attach, enqueue, status, pause, stop, upgrade, and
// all multi-daemon commands per §4.10)".
var exitCodeWiringFixtureAllCommands = []operatornfr.CommandName{
	operatornfr.CommandDaemon,
	operatornfr.CommandAttach,
	operatornfr.CommandEnqueue,
	operatornfr.CommandStatus,
	operatornfr.CommandPause,
	operatornfr.CommandStop,
	operatornfr.CommandUpgrade,
	operatornfr.CommandList,   // multi-daemon per §4.10 ON-041
	operatornfr.CommandRunner, // process-lifecycle.md §4.10 PL-028
}

// TestON001_CommandExitCodeSets_AllCommandsPresent verifies that every ON-001
// command has a declared exit-code set in [CommandExitCodeSets].
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001.
func TestON001_CommandExitCodeSets_AllCommandsPresent(t *testing.T) {
	t.Parallel()

	for _, cmd := range exitCodeWiringFixtureAllCommands {
		cmd := cmd
		t.Run(string(cmd), func(t *testing.T) {
			t.Parallel()

			_, ok := operatornfr.CommandLookup(cmd)
			if !ok {
				t.Errorf("ON-001: command %q has no entry in CommandExitCodeSets; every operator command MUST declare its exit-code set", cmd)
			}
		})
	}
}

// TestON001_CommandExitCodeSets_NamesAreDistinct verifies that no two entries
// in [CommandExitCodeSets] declare the same command name.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001 — one-to-one mapping requirement
// extends to the command surface.
func TestON001_CommandExitCodeSets_NamesAreDistinct(t *testing.T) {
	t.Parallel()

	seen := make(map[operatornfr.CommandName]bool)
	for _, set := range operatornfr.CommandExitCodeSets {
		set := set
		t.Run(string(set.Command), func(t *testing.T) {
			t.Parallel()

			if seen[set.Command] {
				t.Errorf("ON-001: command %q appears more than once in CommandExitCodeSets; command names must be distinct", set.Command)
			}
			seen[set.Command] = true
		})
	}
}

// TestON001_CommandExitCodeSets_AllCodesResolveTo8 verifies that every code
// declared in each command's exit-code set resolves to a §8 taxonomy entry via
// [LookupExitCode].
//
// This is the table-driven one-to-one §8 mapping test required by the bead
// (hk-sx9r.2): "table-driven tests proving one-to-one §8 mapping."
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001 — "Non-zero codes MUST map
// one-to-one to a failure category declared in the exit-code taxonomy of §8."
func TestON001_CommandExitCodeSets_AllCodesResolveTo8(t *testing.T) {
	t.Parallel()

	for _, set := range operatornfr.CommandExitCodeSets {
		set := set
		t.Run(string(set.Command), func(t *testing.T) {
			t.Parallel()

			for _, code := range set.Codes {
				code := code
				t.Run("", func(t *testing.T) {
					t.Parallel()

					e, ok := operatornfr.LookupExitCode(code)
					if !ok {
						t.Errorf("ON-001: command %q declares exit code %d but that code is not in the §8 taxonomy; every declared code MUST resolve to a §8 entry", set.Command, code)
						return
					}
					if e.Code != code {
						t.Errorf("ON-001: command %q: LookupExitCode(%d).Code = %d, want %d", set.Command, code, e.Code, code)
					}
					if e.Category == "" {
						t.Errorf("ON-001: command %q: exit code %d has empty Category in the §8 taxonomy", set.Command, code)
					}
				})
			}
		})
	}
}

// TestON001_CommandExitCodeSets_NoSuccessCodeDeclared verifies that no
// command's exit-code set declares code 0 (success).  Code 0 is always implied
// and MUST NOT appear in the failure set.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001 — "Zero MUST mean success."
func TestON001_CommandExitCodeSets_NoSuccessCodeDeclared(t *testing.T) {
	t.Parallel()

	for _, set := range operatornfr.CommandExitCodeSets {
		set := set
		t.Run(string(set.Command), func(t *testing.T) {
			t.Parallel()

			for _, code := range set.Codes {
				if code == 0 {
					t.Errorf("ON-001: command %q declares exit code 0 in its failure set; code 0 (success) is implied and MUST NOT be listed explicitly", set.Command)
				}
			}
		})
	}
}

// TestON001_CommandExitCodeSets_CodesAreDistinctPerCommand verifies that no
// single command's exit-code set declares the same code twice.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001 — one-to-one mapping.
func TestON001_CommandExitCodeSets_CodesAreDistinctPerCommand(t *testing.T) {
	t.Parallel()

	for _, set := range operatornfr.CommandExitCodeSets {
		set := set
		t.Run(string(set.Command), func(t *testing.T) {
			t.Parallel()

			seen := make(map[int]bool, len(set.Codes))
			for _, code := range set.Codes {
				if seen[code] {
					t.Errorf("ON-001: command %q declares exit code %d more than once; codes within a command set must be distinct", set.Command, code)
				}
				seen[code] = true
			}
		})
	}
}

// TestON001_CommandExitCodeSets_NonZeroCodesAreNonSuccess verifies that every
// code in every command's failure set is non-zero and maps to a non-success
// category.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001 — "Zero MUST mean success.
// Non-zero codes MUST map one-to-one to a failure category."
func TestON001_CommandExitCodeSets_NonZeroCodesAreNonSuccess(t *testing.T) {
	t.Parallel()

	for _, set := range operatornfr.CommandExitCodeSets {
		set := set
		t.Run(string(set.Command), func(t *testing.T) {
			t.Parallel()

			for _, code := range set.Codes {
				code := code
				t.Run("", func(t *testing.T) {
					t.Parallel()

					if code == 0 {
						// Already caught by TestON001_CommandExitCodeSets_NoSuccessCodeDeclared.
						return
					}
					e, ok := operatornfr.LookupExitCode(code)
					if !ok {
						// Already caught by TestON001_CommandExitCodeSets_AllCodesResolveTo8.
						return
					}
					if e.Category == "success" {
						t.Errorf("ON-001: command %q exit code %d maps to category %q; non-zero codes MUST NOT carry the 'success' category", set.Command, code, e.Category)
					}
				})
			}
		})
	}
}

// TestON001_VerifyCommandExitCodeSets_ReturnsNoViolations exercises the
// production [VerifyCommandExitCodeSets] function and asserts that it reports
// zero violations.  This is the production-function counterpart to the
// table-driven tests above.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001.
func TestON001_VerifyCommandExitCodeSets_ReturnsNoViolations(t *testing.T) {
	t.Parallel()

	violations := operatornfr.VerifyCommandExitCodeSets()
	if len(violations) != 0 {
		for _, v := range violations {
			t.Errorf("ON-001 VerifyCommandExitCodeSets: %s", v)
		}
	}
}

// TestON001_CommandLookup_KnownCommandsFound verifies that [CommandLookup]
// returns found=true for every ON-001 declared command name.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001.
func TestON001_CommandLookup_KnownCommandsFound(t *testing.T) {
	t.Parallel()

	for _, cmd := range exitCodeWiringFixtureAllCommands {
		cmd := cmd
		t.Run(string(cmd), func(t *testing.T) {
			t.Parallel()

			set, ok := operatornfr.CommandLookup(cmd)
			if !ok {
				t.Fatalf("ON-001: CommandLookup(%q) returned not-found; every declared command MUST be resolvable", cmd)
			}
			if set.Command != cmd {
				t.Errorf("ON-001: CommandLookup(%q) returned set with Command = %q, want %q", cmd, set.Command, cmd)
			}
		})
	}
}

// TestON001_CommandLookup_UnknownCommandNotFound verifies that [CommandLookup]
// returns found=false for an undeclared command name.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001 — negative-path check.
func TestON001_CommandLookup_UnknownCommandNotFound(t *testing.T) {
	t.Parallel()

	_, ok := operatornfr.CommandLookup("undeclared-command")
	if ok {
		t.Error("ON-001: CommandLookup(\"undeclared-command\") returned found=true; undeclared commands must not be resolvable")
	}
}

// TestON001_CommandExitCodeSets_DaemonCoversAllStartupCodes verifies that the
// daemon command's exit-code set covers the full startup-prerequisite-failure
// catalog (codes 2–10, 19, 22, 23) per §4.1.ON-003 and process-lifecycle.md
// §4.2 PL-008a.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-003; §8 startup-catalog codes.
func TestON001_CommandExitCodeSets_DaemonCoversAllStartupCodes(t *testing.T) {
	t.Parallel()

	// Startup codes per §8 taxonomy (IsStartup == true in exitCodeFixtureTable).
	startupCodes := []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 19, 22, 23}

	set, ok := operatornfr.CommandLookup(operatornfr.CommandDaemon)
	if !ok {
		t.Fatal("ON-001: CommandLookup(CommandDaemon) returned not-found")
	}

	codeSet := make(map[int]bool, len(set.Codes))
	for _, code := range set.Codes {
		codeSet[code] = true
	}

	for _, code := range startupCodes {
		code := code
		t.Run("", func(t *testing.T) {
			t.Parallel()

			if !codeSet[code] {
				e, _ := operatornfr.LookupExitCode(code)
				t.Errorf("ON-001: daemon command exit-code set is missing startup code %d (%s); the daemon MUST cover all startup prerequisite failure codes per ON-003", code, e.Category)
			}
		})
	}
}

// TestON001_CommandExitCodeSets_UpgradeCoversRequiredPaused verifies that the
// upgrade command's exit-code set includes code 13 (upgrade-requires-paused).
//
// Spec ref: specs/operator-nfr.md §4.6 ON-020 — upgrade is only valid when
// the daemon is paused; violating this yields exit code 13.
func TestON001_CommandExitCodeSets_UpgradeCoversRequiredPaused(t *testing.T) {
	t.Parallel()

	set, ok := operatornfr.CommandLookup(operatornfr.CommandUpgrade)
	if !ok {
		t.Fatal("ON-001: CommandLookup(CommandUpgrade) returned not-found")
	}

	found := false
	for _, code := range set.Codes {
		if code == 13 {
			found = true
			break
		}
	}
	if !found {
		t.Error("ON-001: upgrade command exit-code set is missing code 13 (upgrade-requires-paused); upgrade MUST return this code when invoked without prior pause")
	}
}

// TestON001_CommandExitCodeSets_MultiDaemonCommandsCoverCode17 verifies that
// every daemon-communicating command (attach, status, pause, stop, upgrade, list)
// includes code 17 (multi-daemon-target-missing) in its exit-code set.
//
// Spec ref: specs/operator-nfr.md §4.10 ON-041 — daemon-communicating commands
// MUST accept the identification flags and MUST return code 17 when the target
// cannot be resolved.
func TestON001_CommandExitCodeSets_MultiDaemonCommandsCoverCode17(t *testing.T) {
	t.Parallel()

	// All daemon-communicating commands per ON-041 and process-lifecycle.md
	// §4.10 PL-028.  "daemon" and "runner" start processes (not daemon-comm) so
	// they are excluded.
	daemonCommands := []operatornfr.CommandName{
		operatornfr.CommandAttach,
		operatornfr.CommandEnqueue,
		operatornfr.CommandStatus,
		operatornfr.CommandPause,
		operatornfr.CommandStop,
		operatornfr.CommandUpgrade,
		operatornfr.CommandList,
	}

	for _, cmd := range daemonCommands {
		cmd := cmd
		t.Run(string(cmd), func(t *testing.T) {
			t.Parallel()

			set, ok := operatornfr.CommandLookup(cmd)
			if !ok {
				t.Fatalf("ON-001: CommandLookup(%q) returned not-found", cmd)
			}

			found := false
			for _, code := range set.Codes {
				if code == 17 {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("ON-001: daemon-communicating command %q is missing code 17 (multi-daemon-target-missing); per ON-041 all daemon-communicating commands MUST return this code when the target cannot be resolved", cmd)
			}
		})
	}
}
