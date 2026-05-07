package core

// WorkflowVersion is the pinned version of a workflow at dispatch time
// (execution-model.md §6.1 Run.workflow_version; string-backed, semver-ish).
//
// WorkflowVersion is a named type (not a Go alias) to prevent accidental mixing
// with other string-backed identifiers at compile time. The spec describes the
// value as "semver-ish" but imposes no regex constraint; validation is limited
// to non-emptiness.
type WorkflowVersion string
