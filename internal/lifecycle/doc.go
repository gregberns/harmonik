// Package lifecycle holds filesystem-scenario tests for the process-lifecycle
// primitive defined in [specs/process-lifecycle.md].
//
// The lifecycle primitive covers per-project daemon singularity (§4.1 PL-001),
// the pidfile at .harmonik/daemon.pid (PL-002, PL-002a, PL-002b), the Unix
// socket at .harmonik/daemon.sock (PL-003, PL-003a, PL-003b), stale-pidfile
// detection (PL-024), and invariants PL-INV-001 and PL-INV-004.
//
// Production API: [AcquirePidfile] and [ReadPidfile] implement the PL-002b
// pidfile write and read discipline; [Pidfile.Release] releases the flock fd.
//
// The daemon TYPE itself is owned by internal/daemon (not yet shipped).
// Helpers, types, and placeholder primitives used exclusively by tests are
// declared in *_test.go files in this package.
//
// See [specs/process-lifecycle.md] §10.2 for the full test-surface obligations.
package lifecycle
