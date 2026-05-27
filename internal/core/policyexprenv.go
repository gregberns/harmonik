package core

import "encoding/json"

// PolicyExprEnv is the typed environment for policy-expression evaluation per
// specs/control-points.md §6.4 (CP-034).
//
// # Expression bindings
//
// Every policy expression is compiled and evaluated against this environment
// via [expr.Env] + [expr.Compile] + [expr.Run]. The struct's `expr` struct
// tags establish the lowercase binding names the expression grammar exposes to
// policy authors:
//
//   - run         — the current [Run] record (key paths in §6.4.1)
//   - outcome     — the handler-produced [Outcome] record (key paths in §6.4.1)
//   - event       — the matched [Event] record (Hook context only); nil (None)
//     when the expression is evaluated outside a Hook
//   - context     — the run's shared key/value map (alias of run.context)
//   - policy_meta — the policy document's metadata block (string→string)
//   - edges       — the candidate edge list (Guard context only); nil (None)
//     when the expression is evaluated outside a Guard
//
// # Context binding is an alias of run.context
//
// The `context` top-level binding is the same map as [PolicyExprRunView].Context
// (i.e., [Run].Context after EM-041a context updates have been applied).
// Expression authors MAY use either form; both refer to the same underlying map.
//
// # Kind-specific bindings
//
// event and edges are available only in specific ControlPoint Kinds:
//
//   - Hook evaluators / subscription_filter: event is non-nil; edges is nil.
//   - Guard evaluators: edges is non-nil; event is nil.
//   - Gate evaluators: both event and edges are nil.
//
// Dereferencing event outside Hook context or edges outside Guard context is a
// type-check error at workflow-ingest per CP-035 (not a runtime panic).
//
// # Constructors
//
// Use the kind-specific constructors to build a PolicyExprEnv for the correct
// evaluation context:
//
//   - [NewPolicyExprGateEnv] — Gate expressions (event=nil, edges=nil)
//   - [NewPolicyExprHookEnv] — Hook expressions (event=non-nil, edges=nil)
//   - [NewPolicyExprGuardEnv] — Guard expressions (event=nil, edges=non-nil)
type PolicyExprEnv struct {
	// Run is the current run view (spec binding: run).
	Run PolicyExprRunView `expr:"run"`

	// Outcome is the handler-produced outcome view (spec binding: outcome).
	Outcome PolicyExprOutcomeView `expr:"outcome"`

	// Event is the matched event (Hook context only); nil outside Hook context
	// (spec binding: event; type Event | None per §6.4).
	Event *PolicyExprEventView `expr:"event"`

	// Context is the run's shared key/value map after EM-041a context updates
	// have been applied (spec binding: context; alias of run.context per §6.4).
	Context map[string]any `expr:"context"`

	// PolicyMeta is the policy document's metadata block (spec binding:
	// policy_meta; type Map<String, String> per §6.4).
	PolicyMeta map[string]string `expr:"policy_meta"`

	// Edges is the candidate edge list in Guard context; nil outside Guard
	// context (spec binding: edges; type List<Edge> | None per §6.4).
	Edges []PolicyExprEdgeView `expr:"edges"`
}

// PolicyExprRunView is the run-shaped view exposed to policy expressions per
// specs/control-points.md §6.4.1. Field tags establish the lowercase
// snake_case names policy authors use in expressions.
//
// The §6.4.1 "key paths" table lists the 20 most-used stable paths. Authors
// MAY dereference any exported field the full Run shape documents; the paths
// listed in this struct are guaranteed stable at the MVH schema version.
type PolicyExprRunView struct {
	// ID is the unique run identifier (spec path: run.id).
	ID string `expr:"id"`

	// State is the current node identifier string (spec path: run.state).
	// Populated from Run.State converted to its canonical string form.
	State string `expr:"state"`

	// BeadID is the linked Beads record id; nil when the run has no bead
	// (spec path: run.bead_id; type String | None).
	BeadID *string `expr:"bead_id"`

	// WorkflowVersion is the pinned workflow version string at dispatch time
	// (spec path: run.workflow_version).
	WorkflowVersion string `expr:"workflow_version"`

	// Paused reports whether the daemon is in operator pause per
	// operator-nfr.md §4.3 (spec path: run.paused).
	// The caller MUST inject the current daemon pause state; it is not
	// derivable from the Run record alone.
	Paused bool `expr:"paused"`

	// NextNode is the candidate target node available when the Gate's
	// attach_point = node-pre-entry; nil otherwise (spec path: run.next_node).
	NextNode *PolicyExprNodeHandleView `expr:"next_node"`

	// Context is the run's shared key/value map (spec path: run.context).
	// This field is the same map as the top-level PolicyExprEnv.Context binding.
	Context map[string]any `expr:"context"`
}

// PolicyExprOutcomeView is the outcome-shaped view exposed to policy expressions
// per specs/control-points.md §6.4.1.
type PolicyExprOutcomeView struct {
	// Status is the outcome status string (spec path: outcome.status).
	// One of {SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS} per execution-model.md §8.
	Status string `expr:"status"`

	// FailureClass is the failure classification hint; nil when Status is not
	// FAIL (spec path: outcome.failure_class; type String | None).
	FailureClass *string `expr:"failure_class"`

	// ArtifactRefs is the list of artifact identifiers produced by the handler
	// (spec path: outcome.artifact_refs; type List<String>).
	// This field is reserved per §6.4.1; the Outcome record does not yet carry
	// artifact references at MVH — callers MUST pass nil or empty slice until
	// the field is added to the Outcome record.
	ArtifactRefs []string `expr:"artifact_refs"`
}

// PolicyExprEventView is the event-shaped view exposed to policy expressions in
// Hook context per specs/control-points.md §6.4.1.
type PolicyExprEventView struct {
	// Type is the canonical event type name (spec path: event.type).
	Type string `expr:"type"`

	// Payload is the decoded event payload as a freeform map; fields are
	// event-type-specific per event-model.md §6.3 (spec path: event.payload;
	// type Map<String, Any>). Callers MUST decode Event.Payload (json.RawMessage)
	// to map[string]any before constructing the view.
	Payload map[string]any `expr:"payload"`
}

// PolicyExprEdgeView is the edge-shaped view exposed to policy expressions in
// Guard context per specs/control-points.md §6.4.1.
type PolicyExprEdgeView struct {
	// Target is the destination node handle (spec path: edge.target).
	Target PolicyExprNodeHandleView `expr:"target"`

	// Weight is the numeric tie-breaker for this edge; nil when not set
	// (spec path: edge.weight; type Integer | None).
	Weight *int `expr:"weight"`

	// PreferredLabel is the informational routing label hint; nil when not set
	// (spec path: edge.preferred_label; type String | None).
	PreferredLabel *string `expr:"preferred_label"`
}

// PolicyExprNodeHandleView is the node handle view used by run.next_node (Gate)
// and edge.target (Guard) per specs/control-points.md §6.4.1.
type PolicyExprNodeHandleView struct {
	// ID is the node identifier string (spec path: {handle}.id).
	ID string `expr:"id"`

	// AgentType is the handler agent type string per handler-contract.md §6.1;
	// e.g., "claude-code", "ntm" (spec path: {handle}.agent_type).
	AgentType string `expr:"agent_type"`
}

// NewPolicyExprGateEnv constructs a [PolicyExprEnv] for Gate expression
// evaluation (event=nil, edges=nil) per specs/control-points.md §6.4.
//
// Parameters:
//   - run: the current Run record. Run.Context is the shared context map.
//   - outcome: the handler-produced Outcome record.
//   - daemonPaused: current operator pause state from operator-nfr.md §4.3.
//   - policyMeta: the policy document's metadata block (map[string]string).
//   - nextNode: the candidate Gate target when attach_point=node-pre-entry; nil otherwise.
//
// The returned PolicyExprEnv.Event is nil and PolicyExprEnv.Edges is nil.
// Expressions that dereference event or edges will fail type-checking at
// workflow-ingest per CP-035.
func NewPolicyExprGateEnv(
	run Run,
	outcome Outcome,
	daemonPaused bool,
	policyMeta map[string]string,
	nextNode *PolicyExprNodeHandleView,
) PolicyExprEnv {
	rv := newRunView(run, daemonPaused, nextNode)
	ov := newOutcomeView(outcome)
	return PolicyExprEnv{
		Run:        rv,
		Outcome:    ov,
		Event:      nil,
		Context:    run.Context,
		PolicyMeta: policyMeta,
		Edges:      nil,
	}
}

// NewPolicyExprHookEnv constructs a [PolicyExprEnv] for Hook expression and
// subscription_filter evaluation (event=non-nil, edges=nil) per
// specs/control-points.md §6.4.
//
// Parameters:
//   - run: the current Run record.
//   - outcome: the handler-produced Outcome record.
//   - event: the matched event record. MUST be non-nil for Hook context.
//     Event.Payload (json.RawMessage) is decoded to map[string]any; a decode
//     error produces an empty map (not a fatal error — expression evaluation
//     then simply sees an empty payload).
//   - daemonPaused: current operator pause state.
//   - policyMeta: the policy document's metadata block.
//
// The returned PolicyExprEnv.Edges is nil. Expressions that dereference edges
// will fail type-checking at workflow-ingest per CP-035.
func NewPolicyExprHookEnv(
	run Run,
	outcome Outcome,
	event Event,
	daemonPaused bool,
	policyMeta map[string]string,
) PolicyExprEnv {
	rv := newRunView(run, daemonPaused, nil)
	ov := newOutcomeView(outcome)
	ev := newEventView(event)
	return PolicyExprEnv{
		Run:        rv,
		Outcome:    ov,
		Event:      &ev,
		Context:    run.Context,
		PolicyMeta: policyMeta,
		Edges:      nil,
	}
}

// NewPolicyExprGuardEnv constructs a [PolicyExprEnv] for Guard expression
// evaluation (edges=non-nil, event=nil) per specs/control-points.md §6.4.
//
// Parameters:
//   - run: the current Run record.
//   - outcome: the handler-produced Outcome record.
//   - edges: the candidate edges for this guard evaluation.
//   - daemonPaused: current operator pause state.
//   - policyMeta: the policy document's metadata block.
//
// The returned PolicyExprEnv.Event is nil. Expressions that dereference event
// will fail type-checking at workflow-ingest per CP-035.
func NewPolicyExprGuardEnv(
	run Run,
	outcome Outcome,
	edges []Edge,
	daemonPaused bool,
	policyMeta map[string]string,
) PolicyExprEnv {
	rv := newRunView(run, daemonPaused, nil)
	ov := newOutcomeView(outcome)
	evs := newEdgeViews(edges)
	return PolicyExprEnv{
		Run:        rv,
		Outcome:    ov,
		Event:      nil,
		Context:    run.Context,
		PolicyMeta: policyMeta,
		Edges:      evs,
	}
}

// newRunView constructs a PolicyExprRunView from a Run record.
func newRunView(r Run, daemonPaused bool, nextNode *PolicyExprNodeHandleView) PolicyExprRunView {
	runID := r.RunID.String()
	stateID := r.State.String()

	var beadID *string
	if r.BeadID != nil {
		s := string(*r.BeadID)
		beadID = &s
	}

	return PolicyExprRunView{
		ID:              runID,
		State:           stateID,
		BeadID:          beadID,
		WorkflowVersion: string(r.WorkflowVersion),
		Paused:          daemonPaused,
		NextNode:        nextNode,
		Context:         r.Context,
	}
}

// newOutcomeView constructs a PolicyExprOutcomeView from an Outcome record.
func newOutcomeView(o Outcome) PolicyExprOutcomeView {
	var failureClass *string
	if o.FailureClass != nil {
		s := string(*o.FailureClass)
		failureClass = &s
	}
	return PolicyExprOutcomeView{
		Status:       string(o.Status),
		FailureClass: failureClass,
		ArtifactRefs: nil, // forward-deferred: Outcome does not carry artifact_refs at MVH
	}
}

// newEventView constructs a PolicyExprEventView from an Event record.
// Event.Payload (json.RawMessage) is decoded to map[string]any; a JSON decode
// error results in an empty map so expressions still compile but see no payload
// fields.
func newEventView(e Event) PolicyExprEventView {
	payload := make(map[string]any)
	if len(e.Payload) > 0 {
		_ = json.Unmarshal(e.Payload, &payload) // error → empty map; non-fatal
	}
	return PolicyExprEventView{
		Type:    e.Type,
		Payload: payload,
	}
}

// newEdgeViews constructs []PolicyExprEdgeView from a slice of Edge records.
func newEdgeViews(edges []Edge) []PolicyExprEdgeView {
	if len(edges) == 0 {
		return nil
	}
	views := make([]PolicyExprEdgeView, len(edges))
	for i, e := range edges {
		var weight *int
		// Weight zero is a valid value (Edge.Weight defaults to 0), so we
		// always include it as a non-nil pointer; policy authors can test
		// edge.weight == 0 vs. nil via the edge.preferred_label sentinel.
		w := e.Weight
		weight = &w

		views[i] = PolicyExprEdgeView{
			Target: PolicyExprNodeHandleView{
				ID:        string(e.ToNode),
				AgentType: "", // agent_type is not on Edge; injected by the daemon at evaluation time
			},
			Weight:         weight,
			PreferredLabel: e.PreferredLabel,
		}
	}
	return views
}
