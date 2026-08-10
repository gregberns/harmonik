package core

import (
	"fmt"
	"regexp"
)

var (
	namedWorkflowIDPattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)
	legacyWorkflowIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// WorkflowID is the validated logical identifier of a selected workflow graph.
// New graph IDs use the named form. Lower-case UUID text remains valid only so
// stored legacy records remain readable.
type WorkflowID string

// NewWorkflowID validates raw as a logical graph ID.
func NewWorkflowID(raw string) (WorkflowID, error) {
	if namedWorkflowIDPattern.MatchString(raw) || legacyWorkflowIDPattern.MatchString(raw) {
		return WorkflowID(raw), nil
	}
	return "", fmt.Errorf("invalid workflow ID %q", raw)
}

// Valid reports whether w is a logical graph ID or legacy UUID text.
func (w WorkflowID) Valid() bool {
	_, err := NewWorkflowID(string(w))
	return err == nil
}

// String returns the validated workflow ID text.
func (w WorkflowID) String() string { return string(w) }

// MarshalText implements encoding.TextMarshaler.
func (w WorkflowID) MarshalText() ([]byte, error) {
	if !w.Valid() {
		return nil, fmt.Errorf("invalid workflow ID %q", w)
	}
	return []byte(w), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (w *WorkflowID) UnmarshalText(data []byte) error {
	id, err := NewWorkflowID(string(data))
	if err != nil {
		return err
	}
	*w = id
	return nil
}

// WorkflowDescriptor is the immutable identity of the graph selected for a
// run. It contains identity only. Mode, policy, and source are resolver output.
type WorkflowDescriptor struct {
	WorkflowID      WorkflowID      `json:"workflow_id"`
	WorkflowVersion WorkflowVersion `json:"workflow_version"`
}

// Valid reports whether d identifies one selected graph.
func (d WorkflowDescriptor) Valid() bool {
	return d.WorkflowID.Valid() && d.WorkflowVersion != ""
}

// ReviewPolicy is the resolver-owned review policy for a selected graph.
type ReviewPolicy string

const (
	ReviewPolicyReviewed ReviewPolicy = "reviewed"
	ReviewPolicyNoReview ReviewPolicy = "no_review"
)

// Valid reports whether p is a declared review policy.
func (p ReviewPolicy) Valid() bool {
	return p == ReviewPolicyReviewed || p == ReviewPolicyNoReview
}

// WorkflowSelectionSource records how the resolver selected a graph.
type WorkflowSelectionSource string

const (
	WorkflowSelectionEmbeddedDefault     WorkflowSelectionSource = "embedded_default"
	WorkflowSelectionProjectDefault      WorkflowSelectionSource = "project_default"
	WorkflowSelectionExplicitRef         WorkflowSelectionSource = "explicit_ref"
	WorkflowSelectionLegacySingleLabel   WorkflowSelectionSource = "legacy_single_label"
	WorkflowSelectionQueueItemSingleMode WorkflowSelectionSource = "queue_item_single_mode"
)

// SelectsNoReview reports whether s is one of the two compatibility inputs that
// may select the no-review graph: a workflow:single bead label or a queue item
// that asks for single mode. Every other source is reviewed.
//
// This is the one owner of that set. The daemon resolver, the run_started
// payload validator and the core-loop fixture guard all ask here instead of
// listing the two sources again, because a rule with three copies reports the
// daemon as broken the day one copy changes.
func (s WorkflowSelectionSource) SelectsNoReview() bool {
	return s == WorkflowSelectionLegacySingleLabel || s == WorkflowSelectionQueueItemSingleMode
}

// Valid reports whether s is a declared selection source.
func (s WorkflowSelectionSource) Valid() bool {
	switch s {
	case WorkflowSelectionEmbeddedDefault, WorkflowSelectionProjectDefault,
		WorkflowSelectionExplicitRef, WorkflowSelectionLegacySingleLabel,
		WorkflowSelectionQueueItemSingleMode:
		return true
	default:
		return false
	}
}
