package brcli

import (
	"errors"
	"strings"
)

// CallerKind distinguishes the two harmonik write-path contexts for purposes
// of the BI-010c workflow-label write discipline.
//
// The zero value is CallerKindAgent (the more restrictive context), so that
// callers which forget to set the field are subject to the write prohibition
// rather than silently bypassing it.
type CallerKind uint8

const (
	// CallerKindAgent is the context in which an agent subprocess issues a
	// br write. Agents MUST NOT mutate workflow:<mode> labels per BI-010c.
	// This is the zero value — the safe default.
	CallerKindAgent CallerKind = iota

	// CallerKindDaemon is the context in which the harmonik daemon (or a
	// reconciliation-side auto-resolver) issues a br write. Daemon-side
	// workflow:<mode> label writes are permitted per BI-010c where the
	// workflow's design intent explicitly requires it.
	CallerKindDaemon
)

// ErrWorkflowLabelWriteForbidden is returned by CheckWorkflowLabelWrite when
// an agent-context caller attempts to add, remove, or modify a workflow:<mode>
// label via br update or any equivalent label-mutation argv.
//
// Callers SHOULD use errors.Is(err, ErrWorkflowLabelWriteForbidden) to detect
// this specific violation. The error wraps a message naming the offending
// label value so that structured-log consumers can surface the bead ID and
// label without string parsing.
//
// Spec ref: specs/beads-integration.md §4.3 BI-010c; §4.9 BI-027.
var ErrWorkflowLabelWriteForbidden = errors.New("brcli: workflow label write forbidden for agent-context caller (BI-010c)")

const workflowLabelPrefix = "workflow:"

// CheckWorkflowLabelWrite inspects brArgs for any argument that begins with
// the "workflow:" prefix and, if the caller kind is CallerKindAgent, returns
// ErrWorkflowLabelWriteForbidden before the write is issued.
//
// When kind is CallerKindDaemon, CheckWorkflowLabelWrite always returns nil —
// daemon-side workflow-label writes are permitted by BI-010c.
//
// When kind is CallerKindAgent and no workflow:<mode> argument is present,
// CheckWorkflowLabelWrite returns nil — the write does not touch the
// label namespace and is not governed by BI-010c.
//
// Usage: callers that issue br update (or br label) argv MUST call
// CheckWorkflowLabelWrite before invoking Run, RunWithTimeout, or
// RunWithDBLockedRetry. Failure to do so is a BI-010c conformance violation
// (BI-INV-001).
//
// Spec ref: specs/beads-integration.md §4.3 BI-010c.
func CheckWorkflowLabelWrite(kind CallerKind, brArgs []string) error {
	if kind == CallerKindDaemon {
		return nil
	}

	for _, arg := range brArgs {
		if strings.HasPrefix(labelValue(arg), workflowLabelPrefix) {
			return ErrWorkflowLabelWriteForbidden
		}
	}
	return nil
}

func labelValue(arg string) string {
	if strings.HasPrefix(arg, "-") {
		if _, value, found := strings.Cut(arg, "="); found {
			return value
		}
	}
	return arg
}
