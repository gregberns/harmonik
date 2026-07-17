// Package bootconfig holds the pure config-resolution seam extracted from the
// daemon composition root (startWithHooks). It resolves the boot-time branching
// and workflow-mode configuration — the flag > YAML > built-in precedence merge,
// the "" → "main" target-branch default, and the hk-sul12 fail-closed
// branch-protection checks — as pure functions over string / []string /
// core.WorkflowMode primitives.
//
// The package is a daemon-internal sub-package (giant-retirement boot-config,
// slice B1). It imports ONLY the Go standard library and internal/core; it must
// never import internal/daemon back (a depguard row on
// **/internal/daemon/bootconfig/** enforces the one-way edge). All file/OS I/O
// (branching.Load, LoadProjectConfig, pidfile acquire) stays daemon-side; this
// package receives already-loaded values.
//
// Ordering contract (seam-2, load-bearing): the daemon calls ValidateWorkflowMode
// BEFORE branching.Load so the common empty-mode misconfig (hk-81n9r)
// short-circuits before any I/O, byte-identical to the pre-extraction inline
// block. Resolve re-validates the mode idempotently so it remains a correct
// standalone composition.
package bootconfig
