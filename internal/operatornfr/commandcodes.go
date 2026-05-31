package operatornfr

// CommandName is one of the operator-invoked harmonik sub-commands declared in
// ON-001.  Every name here corresponds to a CLI sub-command that MUST return a
// structured exit code from the §8 taxonomy on every exit path.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001 — "every operator-invoked
// harmonik command (daemon, attach, enqueue, status, pause, stop, upgrade, and
// all multi-daemon commands per §4.10) MUST return a structured exit code."
type CommandName string

const (
	// CommandDaemon — `harmonik daemon` starts the per-project daemon process.
	// Spec ref: process-lifecycle.md §4.10 PL-028.
	CommandDaemon CommandName = "daemon"

	// CommandAttach — `harmonik attach` connects an operator terminal to a
	// running daemon.
	// Spec ref: operator-nfr.md §4.10 ON-050.
	CommandAttach CommandName = "attach"

	// CommandEnqueue — `harmonik enqueue` submits a bead to the daemon queue.
	// Spec ref: process-lifecycle.md §4.10 PL-028.
	CommandEnqueue CommandName = "enqueue"

	// CommandStatus — `harmonik status` reports the daemon's current state and
	// pause-reason discriminator.
	// Spec ref: operator-nfr.md §4.10 ON-054.
	CommandStatus CommandName = "status"

	// CommandPause — `harmonik pause` requests the daemon to enter the paused
	// state after completing in-flight runs.
	// Spec ref: operator-nfr.md §4.3 ON-007 – ON-010.
	CommandPause CommandName = "pause"

	// CommandResume — `harmonik resume` clears the paused state and resumes
	// bead dispatch.
	// Spec ref: operator-nfr.md §4.3 ON-007 – ON-010.
	CommandResume CommandName = "resume"

	// CommandStop — `harmonik stop` initiates graceful or immediate daemon
	// shutdown.
	// Spec ref: operator-nfr.md §4.7 ON-027.
	CommandStop CommandName = "stop"

	// CommandUpgrade — `harmonik upgrade` performs a commit-hash-verified
	// in-place binary replacement.
	// Spec ref: operator-nfr.md §4.6 ON-020.
	CommandUpgrade CommandName = "upgrade"

	// CommandList — `harmonik list` enumerates running daemons machine-wide.
	// Spec ref: operator-nfr.md §4.10 ON-041.
	CommandList CommandName = "list"

	// CommandRunner — `harmonik runner` starts the runner (orchestrator-agent)
	// process.
	// Spec ref: process-lifecycle.md §4.10 PL-028.
	CommandRunner CommandName = "runner"
)

// CommandExitCodeSet declares the exit codes that a specific harmonik command
// MUST be capable of returning.  Every code in the set MUST resolve to a §8
// taxonomy entry via [LookupExitCode].
//
// This is the production contract artifact: cmd-scaffold implementations wiring
// each command's failure paths MUST cover every code in the corresponding set.
// Additional codes (beyond this set) MUST NOT be returned — they would violate
// ON-001's one-to-one mapping requirement.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001 — "The mapping MUST be stable:
// a given code MUST refer to the same category across releases within the N-1
// compatibility window."
type CommandExitCodeSet struct {
	// Command is the CLI sub-command this set applies to.
	Command CommandName

	// Codes lists every §8 exit code the command MAY return.  Code 0 (success)
	// is always implied and is omitted from this list.
	//
	// Every code here MUST appear in the §8 taxonomy (verified by
	// [VerifyCommandExitCodeSets]).
	Codes []int
}

// CommandExitCodeSets is the authoritative declaration of exit codes per
// operator-invoked command.  Every entry aligns to the §8 taxonomy.
//
// Design note: codes are declared per-command, not per-failure-path, because
// ON-001 requires a stable code-to-category mapping across releases.  The
// cmd-scaffold implementations enumerate the same codes on their failure paths;
// these sets are the contract they are tested against.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001; §8 exit-code taxonomy;
// §4.10 ON-041 (multi-daemon commands); process-lifecycle.md §4.10 PL-028.
var CommandExitCodeSets = []CommandExitCodeSet{
	{
		Command: CommandDaemon,
		// daemon starts the per-project daemon; its failure paths span all
		// startup prerequisite checks (codes 2–10) and runtime-start failures
		// (codes 19, 22, 23).
		Codes: []int{
			1,  // generic-failure (uncategorised fallback)
			2,  // queue-format-unsupported
			3,  // checkpoint-schema-unsupported
			4,  // event-schema-unsupported
			5,  // pidfile-locked
			6,  // socket-bind-failed
			7,  // git-bad-state
			8,  // beads-unavailable
			9,  // filesystem-unwritable
			10, // disk-full
			19, // runtime-panic (panic barrier)
			22, // ntm-unavailable
			23, // orchestrator-agent-unavailable
		},
	},
	{
		Command: CommandRunner,
		// runner is the orchestrator-agent sub-command; it inherits daemon
		// startup failures and adds code 23 when Claude Code is not found.
		Codes: []int{
			1,  // generic-failure
			8,  // beads-unavailable
			19, // runtime-panic
			22, // ntm-unavailable
			23, // orchestrator-agent-unavailable
		},
	},
	{
		Command: CommandAttach,
		// attach connects an operator terminal to a running daemon; it fails
		// with code 17 when the daemon target cannot be resolved.
		Codes: []int{
			1,  // generic-failure
			16, // operator-control-invalid-state (e.g., daemon not in attachable state)
			17, // multi-daemon-target-missing
		},
	},
	{
		Command: CommandEnqueue,
		// enqueue submits a bead to the daemon queue; it fails when the target
		// daemon is unresolvable or the machine ceiling is exhausted.
		Codes: []int{
			1,  // generic-failure
			17, // multi-daemon-target-missing
			18, // machine-ceiling-exhausted
		},
	},
	{
		Command: CommandStatus,
		// status queries the daemon's current state; it fails when the target
		// daemon is unresolvable.
		Codes: []int{
			1,  // generic-failure
			17, // multi-daemon-target-missing
		},
	},
	{
		Command: CommandPause,
		// pause issues the pause operator control; it fails when the daemon is
		// in an incompatible state or the target is unresolvable.
		Codes: []int{
			1,  // generic-failure
			16, // operator-control-invalid-state
			17, // multi-daemon-target-missing
		},
	},
	{
		Command: CommandResume,
		// resume clears the paused state; it fails when the target is
		// unresolvable.
		Codes: []int{
			1,  // generic-failure
			17, // multi-daemon-target-missing
		},
	},
	{
		Command: CommandStop,
		// stop initiates graceful or immediate shutdown; it can encounter drain
		// failures (11, 21) and target-resolution failures (17).
		Codes: []int{
			1,  // generic-failure
			11, // drain-timeout-escalated
			17, // multi-daemon-target-missing
			21, // drain-step-errored
		},
	},
	{
		Command: CommandUpgrade,
		// upgrade performs a verified in-place binary replacement; it fails
		// when the daemon is not paused (13), the hash mismatches (14), or the
		// schema window is violated (15).
		Codes: []int{
			1,  // generic-failure
			13, // upgrade-requires-paused
			14, // upgrade-hash-mismatch
			15, // upgrade-schema-incompatible
			17, // multi-daemon-target-missing
		},
	},
	{
		Command: CommandList,
		// list enumerates running daemons machine-wide; it fails with code 17
		// when no daemons are resolvable (e.g., $HOME unreadable).
		Codes: []int{
			1,  // generic-failure
			17, // multi-daemon-target-missing
		},
	},
}

// VerifyCommandExitCodeSets checks that every code declared in
// [CommandExitCodeSets] resolves to a §8 taxonomy entry via [LookupExitCode].
// It returns a slice of violation strings (empty on success).
//
// This function is called by the table-driven tests in commandcodes_test.go; it
// may also be called by cmd-scaffold integration tests.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001 — "Non-zero codes MUST map
// one-to-one to a failure category declared in the exit-code taxonomy of §8."
func VerifyCommandExitCodeSets() []string {
	var violations []string
	for _, set := range CommandExitCodeSets {
		for _, code := range set.Codes {
			_, ok := LookupExitCode(code)
			if !ok {
				violations = append(violations,
					string(set.Command)+": exit code "+itoa(code)+" not in §8 taxonomy",
				)
			}
		}
	}
	return violations
}

// CommandLookup returns the [CommandExitCodeSet] for the given command name and
// a boolean indicating whether the command is declared.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001.
func CommandLookup(name CommandName) (CommandExitCodeSet, bool) {
	for i := range CommandExitCodeSets {
		if CommandExitCodeSets[i].Command == name {
			return CommandExitCodeSets[i], true
		}
	}
	return CommandExitCodeSet{}, false
}

// itoa converts an int to its decimal string representation without importing
// strconv in the production package (this avoids a dependency for a minor
// helper). Used only within [VerifyCommandExitCodeSets].
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
