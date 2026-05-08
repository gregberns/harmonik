package operatornfr

// ExitCodeEntry is one row of the §8 exit-code taxonomy declared in
// specs/operator-nfr.md §8.  The taxonomy is AUTHORITATIVE for the corpus:
// every process-lifecycle, reconciliation, and upgrade subsystem that emits a
// non-zero exit code MUST reference an entry here (per ON-002 obligation).
// Code-to-category mappings are stable across releases within the N-1 window
// per §4.1.ON-001; additions are permitted as long as existing mappings remain
// stable.
//
// Fields:
//
//   - Code        — the numeric exit code (0 = success; 1–23 are the MVH surface).
//   - Symbol      — the machine-readable identifier used in log and event fields.
//   - Category    — the human-readable failure category (stable across N-1 window).
//   - Detection   — how the daemon detects this condition at runtime.
//   - Event       — the typed event emitted on this exit path (empty where §8 says "—").
//   - Remediation — operator remediation pointer.
//
// Spec ref: specs/operator-nfr.md §8 — "Exit-code taxonomy. Every non-zero
// code maps to one category."
type ExitCodeEntry struct {
	Code        int
	Symbol      string
	Category    string
	Detection   string
	Event       string
	Remediation string
}

// ExitCodes is the authoritative registry of all 24 exit codes (0–23) defined
// in the §8 taxonomy.  The slice is ordered by code value; callers MUST NOT
// modify it at runtime.
//
// Spec ref: specs/operator-nfr.md §8.
var ExitCodes = [...]ExitCodeEntry{
	{
		Code:        0,
		Symbol:      "success",
		Category:    "success",
		Detection:   "Normal completion of all requested work.",
		Event:       "",
		Remediation: "—",
	},
	{
		Code:        1,
		Symbol:      "generic-failure",
		Category:    "generic-failure",
		Detection:   "Fallback for uncategorized failure; MUST be rare; presence in a release indicates missing taxonomy entry.",
		Event:       "run_failed",
		Remediation: "Operator files incident; foundation amends taxonomy.",
	},
	{
		Code:        2,
		Symbol:      "queue-format-unsupported",
		Category:    "queue-format-unsupported",
		Detection:   "Beads schema version or harmonik overlay version not in supported set per §4.4.ON-016.",
		Event:       "daemon_startup_failed",
		Remediation: "Install migration release per §4.5.ON-019.",
	},
	{
		Code:        3,
		Symbol:      "checkpoint-schema-unsupported",
		Category:    "checkpoint-schema-unsupported",
		Detection:   "Checkpoint trailer or sibling-file schema version not in supported set per §4.5.ON-018.",
		Event:       "daemon_startup_failed",
		Remediation: "Install migration release.",
	},
	{
		Code:        4,
		Symbol:      "event-schema-unsupported",
		Category:    "event-schema-unsupported",
		Detection:   "Event envelope or payload schema version not in supported set per event-model.md §4.7.",
		Event:       "daemon_startup_failed",
		Remediation: "Install migration release.",
	},
	{
		Code:        5,
		Symbol:      "pidfile-locked",
		Category:    "pidfile-locked",
		Detection:   "Another daemon holds the pidfile lock for this project per process-lifecycle.md §4.1.",
		Event:       "daemon_startup_failed",
		Remediation: "Identify running daemon via `harmonik list`; stop or target with --daemon-id.",
	},
	{
		Code:        6,
		Symbol:      "socket-bind-failed",
		Category:    "socket-bind-failed",
		Detection:   "Socket path cannot be bound (permission, stale socket).",
		Event:       "daemon_startup_failed",
		Remediation: "Per startup failure-mode catalog per §4.1.ON-003.",
	},
	{
		Code:        7,
		Symbol:      "git-bad-state",
		Category:    "git-bad-state",
		Detection:   "Git log walk fails (corrupt repo, missing refs, unreadable objects).",
		Event:       "daemon_startup_failed",
		Remediation: "Per startup failure-mode catalog.",
	},
	{
		Code:        8,
		Symbol:      "beads-unavailable",
		Category:    "beads-unavailable",
		Detection:   "`br` CLI invocation fails or Beads SQLite is unreadable.",
		Event:       "daemon_startup_failed",
		Remediation: "Per startup failure-mode catalog.",
	},
	{
		Code:        9,
		Symbol:      "filesystem-unwritable",
		Category:    "filesystem-unwritable",
		Detection:   "Workspace root or .harmonik/ directory is not writable.",
		Event:       "daemon_startup_failed",
		Remediation: "Per startup failure-mode catalog.",
	},
	{
		Code:        10,
		Symbol:      "disk-full",
		Category:    "disk-full",
		Detection:   "Filesystem full during checkpoint commit attempt.",
		Event:       "daemon_startup_failed",
		Remediation: "Per startup failure-mode catalog.",
	},
	{
		Code:        11,
		Symbol:      "drain-timeout-escalated",
		Category:    "drain-timeout-escalated",
		Detection:   "Any step of §4.7.ON-027 exceeded its bound during graceful shutdown.",
		Event:       "operator_stopped",
		Remediation: "Increase drain timeout per §4.7.ON-029; investigate stuck handler.",
	},
	{
		Code:        12,
		Symbol:      "rto-hard-ceiling-exceeded",
		Category:    "rto-hard-ceiling-exceeded",
		Detection:   "Restart exceeded 300-second ceiling per §4.8.ON-032.",
		Event:       "daemon_degraded",
		Remediation: "Operator intervention per §4.8.ON-032.",
	},
	{
		Code:        13,
		Symbol:      "upgrade-requires-paused",
		Category:    "upgrade-requires-paused",
		Detection:   "`upgrade` invoked while daemon is not `paused`.",
		Event:       "operator_upgrade_rejected",
		Remediation: "Issue `pause`, then retry `upgrade`.",
	},
	{
		Code:        14,
		Symbol:      "upgrade-hash-mismatch",
		Category:    "upgrade-hash-mismatch",
		Detection:   "§4.2.ON-005 commit-hash check failed.",
		Event:       "operator_upgrade_rejected",
		Remediation: "Re-verify binary source; supply correct hash.",
	},
	{
		Code:        15,
		Symbol:      "upgrade-schema-incompatible",
		Category:    "upgrade-schema-incompatible",
		Detection:   "New binary's schema version is outside the N-1 window vs on-disk state per §4.5.ON-019.",
		Event:       "operator_upgrade_rejected",
		Remediation: "Install migration release.",
	},
	{
		Code:        16,
		Symbol:      "operator-control-invalid-state",
		Category:    "operator-control-invalid-state",
		Detection:   "Operator issued a command incompatible with the current state-machine state (e.g., `resume` while `running`).",
		Event:       "operator_command_rejected",
		Remediation: "Inspect `harmonik status`; issue valid command.",
	},
	{
		Code:        17,
		Symbol:      "multi-daemon-target-missing",
		Category:    "multi-daemon-target-missing",
		Detection:   "A daemon-communicating command's --socket / --cwd / --daemon-id target cannot be resolved per §4.10.ON-041.",
		Event:       "",
		Remediation: "Use `harmonik list` to identify running daemons.",
	},
	{
		Code:        18,
		Symbol:      "machine-ceiling-exhausted",
		Category:    "machine-ceiling-exhausted",
		Detection:   "Machine-level agent-subprocess ceiling per §4.10.ON-041 blocks a dispatch.",
		Event:       "dispatch_deferred",
		Remediation: "Reduce concurrent workload or raise ceiling.",
	},
	{
		Code:        19,
		Symbol:      "runtime-panic",
		Category:    "runtime-panic",
		Detection:   "The daemon's top-level panic barrier per process-lifecycle.md §4.6 PL-018a intercepted an uncaught Go runtime panic.",
		Event:       "daemon_startup_failed",
		Remediation: "Inspect structured-log records around the panic timestamp (per §4.9.ON-035); file incident with the panic stack.",
	},
	{
		Code:        20,
		Symbol:      "signal-terminated",
		Category:    "signal-terminated",
		Detection:   "Daemon received a non-graceful termination signal (e.g., SIGKILL via external operator, OOM-killer, SIGBUS, SIGSEGV not intercepted by the panic barrier).",
		Event:       "",
		Remediation: "Next-restart reconciliation per reconciliation/spec.md §4.2 classifies surviving runs; operator inspects OS-level logs for the signal source.",
	},
	{
		Code:        21,
		Symbol:      "drain-step-errored",
		Category:    "drain-step-errored",
		Detection:   "A specific drain step of §4.7.ON-027 (distinct from timeout escalation at code 11) encountered a non-recoverable error, e.g., fsync failure at step 4, workspace lock-release failure at step 6.",
		Event:       "daemon_shutdown",
		Remediation: "Inspect the step-specific error category; apply the remediation for that subsystem's owning failure taxonomy.",
	},
	{
		Code:        22,
		Symbol:      "ntm-unavailable",
		Category:    "ntm-unavailable",
		Detection:   "`ntm` not on PATH, incompatible version, or tmux missing per process-lifecycle.md §4.7 PL-021a.",
		Event:       "daemon_startup_failed",
		Remediation: "Install/upgrade `ntm`; verify tmux available.",
	},
	{
		Code:        23,
		Symbol:      "orchestrator-agent-unavailable",
		Category:    "orchestrator-agent-unavailable",
		Detection:   "`harmonik runner --orchestrator-agent` cannot locate Claude Code per process-lifecycle.md §4.10 PL-028.",
		Event:       "daemon_startup_failed",
		Remediation: "Install Claude Code or run without --orchestrator-agent.",
	},
}

// LookupExitCode returns the ExitCodeEntry for the given numeric code and a
// boolean indicating whether the code is declared in the §8 taxonomy.
// Code 0 (success) is included.  Codes outside 0..23 return false.
//
// Spec ref: specs/operator-nfr.md §8 — "Codes 1–23 are the MVH surface."
func LookupExitCode(code int) (ExitCodeEntry, bool) {
	for i := range ExitCodes {
		if ExitCodes[i].Code == code {
			return ExitCodes[i], true
		}
	}
	return ExitCodeEntry{}, false
}
