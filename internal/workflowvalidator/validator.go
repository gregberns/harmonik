package workflowvalidator

// validator.go — PreRunValidator implementation per EM-038.
//
// Every check is mechanism-tagged (EM-039): no semantic judgment is delegated
// to cognition. Only structural and attribute constraints are enforced here;
// semantic questions (is this policy "good"?) belong in reviewer nodes.
//
// Spec ref: specs/execution-model.md §4.9.EM-038, §4.8.EM-034b, §4.10.EM-043.
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
)

// WorkflowResolver resolves a sub-workflow reference to its DOT source.
// Returns an error when the reference does not resolve to a registered workflow.
//
// The validator calls Resolve during the transitive sub-workflow resolution
// pass (EM-038, EM-034b). The returned DOT string is parsed and validated
// recursively; the caller is responsible for cycle detection.
type WorkflowResolver interface {
	// Resolve returns the DOT source for the named sub-workflow, or an error
	// if no workflow with that name is registered.
	Resolve(ref core.SubWorkflowRef) (string, error)
}

// ReferenceRegistry answers resolution queries for the optional node-level
// references: handler_ref, policy_ref, gate_ref, freedom_profile_ref,
// budget_ref, and required_skills entries.
//
// Any method that returns false means the named target is not registered;
// the validator treats that as a validation failure per EM-038.
type ReferenceRegistry interface {
	// HasHandler reports whether a handler with the given ref name is registered.
	HasHandler(ref string) bool
	// HasPolicy reports whether a policy with the given ref name is registered.
	HasPolicy(ref core.PolicyRef) bool
	// HasGate reports whether a gate with the given ref name is registered.
	HasGate(ref core.GateRef) bool
	// HasFreedomProfile reports whether a freedom-profile with the given ref is registered.
	HasFreedomProfile(ref core.FreedomProfileRef) bool
	// HasBudget reports whether a budget with the given ref is registered.
	HasBudget(ref core.BudgetRef) bool
	// HasSkill reports whether a skill with the given name is registered.
	HasSkill(name string) bool
}

// ValidationError is a structured failure from the pre-run validator.
// Multiple failures are joined via errors.Join and can be unwrapped individually.
type ValidationError struct {
	// Code is a stable machine-readable tag identifying the failure mode.
	Code string
	// Detail is a human-readable description of the specific failure.
	Detail string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation[%s]: %s", e.Code, e.Detail)
}

// validationErrorCode constants — each maps to one EM-038 failure mode.
// Code naming follows the pattern: em038_<short-label>.
const (
	codeNotParseable             = "em038_not_parseable"
	codeSubWorkflowUnresolved    = "em038_subworkflow_unresolved"
	codeSubWorkflowCycle         = "em038_subworkflow_cycle"
	codeHandlerRefUnresolved     = "em038_handler_ref_unresolved"
	codePolicyRefUnresolved      = "em038_policy_ref_unresolved"
	codeGateRefUnresolved        = "em038_gate_ref_unresolved"
	codeFreedomProfileUnresolved = "em038_freedom_profile_unresolved"
	codeBudgetRefUnresolved      = "em038_budget_ref_unresolved"
	codeSkillUnresolved          = "em038_skill_unresolved"
	codeSkillNameBadShape        = "cp049_skill_name_bad_shape"
	codeBadNodeType              = "em038_bad_node_type"
	codeMissingHandlerRef        = "em038_missing_handler_ref"
	codeForbiddenHandlerRef      = "em038_forbidden_handler_ref"
	codeMissingSubWorkflowRef    = "em038_missing_sub_workflow_ref"
	codeForbiddenSubWorkflowRef  = "em038_forbidden_sub_workflow_ref"
	codeBadIdempotencyClass      = "em038_bad_idempotency_class"
	codeBadLLMFreedom            = "em038_bad_llm_freedom"
	codeBadIODeterminism         = "em038_bad_io_determinism"
	codeBadReplaySafety          = "em038_bad_replay_safety"
	codeBadAxisIdempotency       = "em038_bad_axis_idempotency"
	codeBadModeTag               = "em038_bad_mode_tag"
	codeTimeoutNotPositive       = "em038_timeout_not_positive"
	codeMissingStartNodeID       = "em038_missing_start_node_id"
	codeMissingTerminalNodeIDs   = "em038_missing_terminal_node_ids"
	codeStartNodeNotDeclared     = "em038_start_node_not_declared"
	codeTerminalNodeNotDeclared  = "em038_terminal_node_not_declared"
	codeBadWorkflowClass         = "em038_bad_workflow_class"
	codeNodeNotReachable         = "em038_node_not_reachable"
	codeNodeCannotReachTerminal  = "em038_node_cannot_reach_terminal"
	codeCycleNoCap               = "em038_cycle_no_cap"
)

// vErr constructs a ValidationError with the given code and formatted detail.
func vErr(code, format string, args ...any) *ValidationError {
	return &ValidationError{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// PreRunValidator validates a workflow DOT document before any node executes,
// enforcing all EM-038 obligations:
//
//   - DOT parseability.
//   - Sub-workflow resolution (transitive) and acyclicity (EM-034b).
//   - Reference resolution.
//   - Attribute type checks.
//   - Reachability.
//   - Cycle-bound check (EM-043).
//
// Usage:
//
//	v := workflowvalidator.New(resolver, registry)
//	if err := v.Validate(dotSrc); err != nil {
//	    // workflow must not start
//	}
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
type PreRunValidator struct {
	resolver WorkflowResolver
	registry ReferenceRegistry
}

// New constructs a PreRunValidator.
//
// resolver is called to dereference sub-workflow nodes during transitive
// resolution (EM-034b). Pass nil when sub-workflow resolution is not
// applicable (e.g., in tests covering only attribute-level checks); the
// validator will return an error for any sub-workflow node it encounters.
//
// registry is called to verify that handler_ref, policy_ref, gate_ref,
// freedom_profile_ref, budget_ref, and required_skills entries resolve to
// registered targets. Pass nil to skip reference-resolution checks (e.g.,
// in unit tests that focus on structural failures). When nil, any node
// carrying these optional refs passes the resolution check silently.
func New(resolver WorkflowResolver, registry ReferenceRegistry) *PreRunValidator {
	return &PreRunValidator{resolver: resolver, registry: registry}
}

// Validate runs the full EM-038 pre-run validation suite against the given DOT
// source string. Returns nil on success; returns one or more ValidationErrors
// joined via errors.Join on failure.
//
// Any failure MUST prevent the workflow from starting per EM-038.
//
// Tags: mechanism
func (v *PreRunValidator) Validate(dotSrc string) error {
	// Pass 1 — DOT parseability.
	g, err := parseDOT(dotSrc)
	if err != nil {
		return vErr(codeNotParseable, "DOT parse failed: %v", err)
	}

	// Collect all validation failures; emit them all at once.
	var errs []error

	// Pass 2 — Attribute type checks (graph-level).
	startNodeID := strings.TrimSpace(g.graphAttrs["start_node_id"])
	if startNodeID == "" {
		errs = append(errs, vErr(codeMissingStartNodeID, "workflow must declare start_node_id"))
	}
	rawTerminals := strings.TrimSpace(g.graphAttrs["terminal_node_ids"])
	terminalNodeIDs := parseSpaceSeparatedIDs(rawTerminals)
	if len(terminalNodeIDs) == 0 {
		errs = append(errs, vErr(codeMissingTerminalNodeIDs, "workflow must declare a non-empty terminal_node_ids list"))
	}
	// workflow_class: if present, must be "reconciliation" (EM-038).
	if rawClass, ok := g.graphAttrs["workflow_class"]; ok {
		wc := core.WorkflowClass(strings.TrimSpace(rawClass))
		if !wc.Valid() {
			errs = append(errs, vErr(codeBadWorkflowClass,
				"workflow_class %q is invalid at MVH; only \"reconciliation\" is accepted (EM-038)", rawClass))
		}
	}

	// Pass 3 — Node-level attribute type checks.
	for _, nodeID := range g.nodeOrder {
		nodeErrs := v.validateNodeAttrs(nodeID, g.nodes[nodeID])
		errs = append(errs, nodeErrs...)
	}

	// Pass 4 — Start/terminal node cross-references.
	if startNodeID != "" {
		if _, found := g.nodes[startNodeID]; !found {
			errs = append(errs, vErr(codeStartNodeNotDeclared,
				"start_node_id %q is not declared as a node", startNodeID))
		}
	}
	for _, tid := range terminalNodeIDs {
		if _, found := g.nodes[tid]; !found {
			errs = append(errs, vErr(codeTerminalNodeNotDeclared,
				"terminal_node_id %q is not declared as a node", tid))
		}
	}

	// Pass 5 — Sub-workflow resolution and acyclicity (EM-034b).
	// Track the chain of workflow IDs visited during transitive resolution to
	// detect cycles (EM-034b). The root workflow is identified by its workflow_id
	// graph attribute; fall back to the DOT graph name if absent.
	rootID := strings.TrimSpace(g.graphAttrs["workflow_id"])
	if rootID == "" {
		rootID = "<root>"
	}
	subErrs := v.resolveSubWorkflows(g, rootID, []string{rootID})
	errs = append(errs, subErrs...)

	// Pass 6 — Reference resolution (handler_ref, policy_ref, etc.).
	if v.registry != nil {
		refErrs := v.validateRefs(g)
		errs = append(errs, refErrs...)
	}

	// Pass 7 — Reachability (from start; to terminal).
	if startNodeID != "" && len(terminalNodeIDs) > 0 {
		reachErrs := validateReachability(g, startNodeID, terminalNodeIDs)
		errs = append(errs, reachErrs...)
	}

	// Pass 8 — Cycle-bound check (EM-043): every cycle must have ≥1 edge with traversal_cap.
	cycleErrs := validateCycleBounds(g)
	errs = append(errs, cycleErrs...)

	return errors.Join(errs...)
}

// --- Pass 3: node attribute checks ---

// validateNodeAttrs checks all EM-038 attribute-type obligations for one node.
func (v *PreRunValidator) validateNodeAttrs(nodeID string, attrs map[string]string) []error {
	var errs []error

	// type (required; enum-valued per EM-006).
	rawType := strings.TrimSpace(attrs["type"])
	nodeType := core.NodeType(rawType)
	if !nodeType.Valid() {
		errs = append(errs, vErr(codeBadNodeType,
			"node %q: type %q is not one of {agentic, non-agentic, gate, control-point, sub-workflow}", nodeID, rawType))
		// Cannot validate conditional fields without a known type; continue checking other attrs.
	}

	// handler_ref: required iff type=agentic; forbidden otherwise (EM-007).
	handlerRef, hasHandlerRef := attrs["handler_ref"]
	handlerRef = strings.TrimSpace(handlerRef)
	if nodeType.Valid() {
		if nodeType == core.NodeTypeAgentic && (!hasHandlerRef || handlerRef == "") {
			errs = append(errs, vErr(codeMissingHandlerRef,
				"node %q: agentic node must declare handler_ref", nodeID))
		}
		if nodeType != core.NodeTypeAgentic && hasHandlerRef && handlerRef != "" {
			errs = append(errs, vErr(codeForbiddenHandlerRef,
				"node %q: non-agentic node must not declare handler_ref", nodeID))
		}
		// sub_workflow_ref: required iff type=sub-workflow; forbidden otherwise.
		subRef, hasSubRef := attrs["sub_workflow_ref"]
		subRef = strings.TrimSpace(subRef)
		if nodeType == core.NodeTypeSubWorkflow && (!hasSubRef || subRef == "") {
			errs = append(errs, vErr(codeMissingSubWorkflowRef,
				"node %q: sub-workflow node must declare sub_workflow_ref", nodeID))
		}
		if nodeType != core.NodeTypeSubWorkflow && hasSubRef && subRef != "" {
			errs = append(errs, vErr(codeForbiddenSubWorkflowRef,
				"node %q: non-sub-workflow node must not declare sub_workflow_ref", nodeID))
		}
	}

	// idempotency_class (required per EM-009).
	rawIC := strings.TrimSpace(attrs["idempotency_class"])
	if rawIC == "" {
		errs = append(errs, vErr(codeBadIdempotencyClass,
			"node %q: idempotency_class is required", nodeID))
	} else {
		ic := core.IdempotencyClass(rawIC)
		if !ic.Valid() {
			errs = append(errs, vErr(codeBadIdempotencyClass,
				"node %q: idempotency_class %q is not one of {idempotent, non-idempotent, recoverable-non-idempotent}", nodeID, rawIC))
		}
	}

	// llm-freedom axis.
	rawLLM := strings.TrimSpace(attrs["llm-freedom"])
	if rawLLM != "" {
		llmF := core.LLMFreedom(rawLLM)
		if !llmF.Valid() {
			errs = append(errs, vErr(codeBadLLMFreedom,
				"node %q: llm-freedom %q is not one of {none, bounded, unbounded}", nodeID, rawLLM))
		}
	}

	// io-determinism axis.
	rawIO := strings.TrimSpace(attrs["io-determinism"])
	if rawIO != "" {
		ioD := core.IODeterminism(rawIO)
		if !ioD.Valid() {
			errs = append(errs, vErr(codeBadIODeterminism,
				"node %q: io-determinism %q is not one of {deterministic, best-effort, nondeterministic}", nodeID, rawIO))
		}
	}

	// replay-safety axis.
	rawRS := strings.TrimSpace(attrs["replay-safety"])
	if rawRS != "" {
		rs := core.ReplaySafety(rawRS)
		if !rs.Valid() {
			errs = append(errs, vErr(codeBadReplaySafety,
				"node %q: replay-safety %q is not one of {safe, unsafe, n/a}", nodeID, rawRS))
		}
	}

	// idempotency axis (distinct from idempotency_class per AxisTags).
	rawAI := strings.TrimSpace(attrs["idempotency"])
	if rawAI != "" {
		ai := core.AxisIdempotency(rawAI)
		if !ai.Valid() {
			errs = append(errs, vErr(codeBadAxisIdempotency,
				"node %q: idempotency axis %q is not one of {idempotent, non-idempotent, recoverable-non-idempotent, n/a}", nodeID, rawAI))
		}
	}

	// mode (mechanism/cognition per EM-039 / AR-005).
	rawMode := strings.TrimSpace(attrs["mode"])
	if rawMode != "" {
		mt := core.ModeTag(rawMode)
		if !mt.Valid() {
			errs = append(errs, vErr(codeBadModeTag,
				"node %q: mode %q is not one of {mechanism, cognition}", nodeID, rawMode))
		}
	}

	// timeout: if present, must be a positive integer.
	if rawTO, ok := attrs["timeout"]; ok {
		rawTO = strings.TrimSpace(rawTO)
		if rawTO != "" {
			n, parseErr := strconv.Atoi(rawTO)
			if parseErr != nil || n <= 0 {
				errs = append(errs, vErr(codeTimeoutNotPositive,
					"node %q: timeout %q must be a positive integer", nodeID, rawTO))
			}
		}
	}

	return errs
}

// --- Pass 5: sub-workflow resolution and acyclicity ---

// resolveSubWorkflows walks every sub-workflow node in g and calls Resolve
// transitively, tracking the chain in visitChain for cycle detection (EM-034b).
func (v *PreRunValidator) resolveSubWorkflows(g *rawGraph, currentID string, visitChain []string) []error {
	var errs []error
	for _, nodeID := range g.nodeOrder {
		attrs := g.nodes[nodeID]
		rawType := strings.TrimSpace(attrs["type"])
		if rawType != string(core.NodeTypeSubWorkflow) {
			continue
		}
		subRef := core.SubWorkflowRef(strings.TrimSpace(attrs["sub_workflow_ref"]))
		if subRef == "" {
			// Missing sub_workflow_ref is caught by attribute checks; skip here.
			continue
		}
		if v.resolver == nil {
			errs = append(errs, vErr(codeSubWorkflowUnresolved,
				"node %q: sub_workflow_ref %q cannot be resolved (no resolver provided)", nodeID, subRef))
			continue
		}
		dotSrc, err := v.resolver.Resolve(subRef)
		if err != nil {
			errs = append(errs, vErr(codeSubWorkflowUnresolved,
				"node %q: sub_workflow_ref %q not registered: %v", nodeID, subRef, err))
			continue
		}
		// Parse resolved sub-workflow DOT.
		subG, err := parseDOT(dotSrc)
		if err != nil {
			errs = append(errs, vErr(codeNotParseable,
				"sub-workflow %q (ref %q): DOT parse failed: %v", nodeID, subRef, err))
			continue
		}
		// Determine the sub-workflow's own identifier for cycle detection.
		subID := strings.TrimSpace(subG.graphAttrs["workflow_id"])
		if subID == "" {
			subID = string(subRef) // fall back to the ref name
		}
		// Cycle detection (EM-034b).
		if chainContains(visitChain, subID) {
			errs = append(errs, vErr(codeSubWorkflowCycle,
				"sub-workflow reference cycle detected: %s → %s (chain: %s)",
				currentID, subID, strings.Join(visitChain, " → ")))
			continue
		}
		// Recurse into sub-workflow.
		subErrs := v.resolveSubWorkflows(subG, subID, append(visitChain, subID))
		errs = append(errs, subErrs...)
	}
	return errs
}

// chainContains reports whether id appears in the chain slice.
func chainContains(chain []string, id string) bool {
	for _, v := range chain {
		if v == id {
			return true
		}
	}
	return false
}

// --- Pass 6: reference resolution ---

// validateRefs verifies that optional node-level references resolve to registered targets.
func (v *PreRunValidator) validateRefs(g *rawGraph) []error {
	var errs []error
	for _, nodeID := range g.nodeOrder {
		attrs := g.nodes[nodeID]

		// handler_ref: checked only for agentic nodes.
		rawType := strings.TrimSpace(attrs["type"])
		if rawType == string(core.NodeTypeAgentic) {
			if hr, ok := attrs["handler_ref"]; ok && strings.TrimSpace(hr) != "" {
				if !v.registry.HasHandler(strings.TrimSpace(hr)) {
					errs = append(errs, vErr(codeHandlerRefUnresolved,
						"node %q: handler_ref %q not registered", nodeID, hr))
				}
			}
		}

		// policy_ref.
		if pr, ok := attrs["policy_ref"]; ok && strings.TrimSpace(pr) != "" {
			ref := core.PolicyRef(strings.TrimSpace(pr))
			if !v.registry.HasPolicy(ref) {
				errs = append(errs, vErr(codePolicyRefUnresolved,
					"node %q: policy_ref %q not registered", nodeID, pr))
			}
		}

		// gate_ref.
		if gr, ok := attrs["gate_ref"]; ok && strings.TrimSpace(gr) != "" {
			ref := core.GateRef(strings.TrimSpace(gr))
			if !v.registry.HasGate(ref) {
				errs = append(errs, vErr(codeGateRefUnresolved,
					"node %q: gate_ref %q not registered", nodeID, gr))
			}
		}

		// freedom_profile_ref.
		if fpr, ok := attrs["freedom_profile_ref"]; ok && strings.TrimSpace(fpr) != "" {
			ref := core.FreedomProfileRef(strings.TrimSpace(fpr))
			if !v.registry.HasFreedomProfile(ref) {
				errs = append(errs, vErr(codeFreedomProfileUnresolved,
					"node %q: freedom_profile_ref %q not registered", nodeID, fpr))
			}
		}

		// budget_ref.
		if br, ok := attrs["budget_ref"]; ok && strings.TrimSpace(br) != "" {
			ref := core.BudgetRef(strings.TrimSpace(br))
			if !v.registry.HasBudget(ref) {
				errs = append(errs, vErr(codeBudgetRefUnresolved,
					"node %q: budget_ref %q not registered", nodeID, br))
			}
		}

		// required_skills: space-separated list.
		// CP-049: check shape first (syntactic validity), then registry resolution.
		if rs, ok := attrs["required_skills"]; ok {
			for _, skill := range parseSpaceSeparatedIDs(rs) {
				// CP-049 ingest-time syntactic validity: skill name MUST match the
				// lowercase-hyphenated identifier shape, optionally with @<version>.
				if !core.SkillName(skill).ValidShape() {
					errs = append(errs, vErr(codeSkillNameBadShape,
						"node %q: required_skill %q does not match skill-name shape (lowercase-hyphenated, optional @version)", nodeID, skill))
					continue // do not attempt registry resolution for malformed names
				}
				if !v.registry.HasSkill(skill) {
					errs = append(errs, vErr(codeSkillUnresolved,
						"node %q: required_skill %q not registered", nodeID, skill))
				}
			}
		}
	}
	return errs
}

// --- Pass 7: reachability ---

// validateReachability checks:
//  1. Every node in g is reachable from startNodeID (forward BFS).
//  2. Every node can reach at least one node in terminalNodeIDs (reverse BFS from terminals).
func validateReachability(g *rawGraph, startNodeID string, terminalNodeIDs []string) []error {
	var errs []error

	// Build adjacency lists.
	forward := make(map[string][]string)  // node → successors
	backward := make(map[string][]string) // node → predecessors
	for _, e := range g.edges {
		forward[e.from] = append(forward[e.from], e.to)
		backward[e.to] = append(backward[e.to], e.from)
	}

	// Forward reachability: BFS from startNodeID.
	reachableFromStart := bfsReach(startNodeID, forward)

	// Reverse reachability: BFS from each terminal node using the reverse graph.
	canReachTerminal := make(map[string]bool)
	for _, t := range terminalNodeIDs {
		for n := range bfsReach(t, backward) {
			canReachTerminal[n] = true
		}
		// The terminal node itself can reach terminal.
		canReachTerminal[t] = true
	}
	// Also the start node can reach terminal if it is one.
	for _, t := range terminalNodeIDs {
		if t == startNodeID {
			canReachTerminal[startNodeID] = true
		}
	}

	for _, nodeID := range g.nodeOrder {
		if !reachableFromStart[nodeID] {
			errs = append(errs, vErr(codeNodeNotReachable,
				"node %q is not reachable from start_node_id %q", nodeID, startNodeID))
		}
		if !canReachTerminal[nodeID] {
			errs = append(errs, vErr(codeNodeCannotReachTerminal,
				"node %q cannot reach any terminal_node_id", nodeID))
		}
	}
	return errs
}

// bfsReach returns the set of nodes reachable from start in graph adj.
func bfsReach(start string, adj map[string][]string) map[string]bool {
	visited := make(map[string]bool)
	queue := []string{start}
	visited[start] = true
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adj[cur] {
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	return visited
}

// --- Pass 8: cycle-bound check (EM-043) ---

// validateCycleBounds detects every cycle in the workflow graph and verifies
// that at least one edge in each cycle carries a traversal_cap (EM-043).
//
// Algorithm: DFS-based cycle detection (Tarjan-style SCC); for each strongly
// connected component with more than one node (or a self-loop), verify that at
// least one edge within the SCC carries a traversal_cap.
func validateCycleBounds(g *rawGraph) []error {
	if len(g.edges) == 0 {
		return nil
	}

	// Find all strongly connected components (SCCs) via Tarjan's algorithm.
	sccs := tarjanSCC(g)

	// Build a set of all nodes in the graph.
	allNodes := make(map[string]bool, len(g.nodeOrder))
	for _, id := range g.nodeOrder {
		allNodes[id] = true
	}

	var errs []error
	for _, scc := range sccs {
		if len(scc) < 2 && !hasSelfLoop(g, scc[0]) {
			continue // trivial SCC: no cycle
		}
		// SCC has a cycle; verify at least one edge within the SCC carries a cap.
		sccSet := make(map[string]bool, len(scc))
		for _, n := range scc {
			sccSet[n] = true
		}
		hasCap := false
		for _, e := range g.edges {
			if !sccSet[e.from] || !sccSet[e.to] {
				continue // edge not inside this SCC
			}
			if rawCap, ok := e.attrs["traversal_cap"]; ok && strings.TrimSpace(rawCap) != "" {
				n, parseErr := strconv.Atoi(strings.TrimSpace(rawCap))
				if parseErr == nil && n > 0 {
					hasCap = true
					break
				}
			}
		}
		if !hasCap {
			errs = append(errs, vErr(codeCycleNoCap,
				"cycle involving nodes [%s] has no edge with a positive traversal_cap (EM-043)",
				strings.Join(scc, ", ")))
		}
	}
	return errs
}

// hasSelfLoop reports whether a node has an edge to itself.
func hasSelfLoop(g *rawGraph, nodeID string) bool {
	for _, e := range g.edges {
		if e.from == nodeID && e.to == nodeID {
			return true
		}
	}
	return false
}

// --- Tarjan SCC ---

type tarjanState struct {
	index   map[string]int
	lowlink map[string]int
	onStack map[string]bool
	stack   []string
	counter int
	sccs    [][]string
}

// tarjanSCC returns all strongly connected components of the graph, each as a
// slice of node IDs. Only nodes with ≥1 edge (or self-loops) form non-trivial SCCs.
func tarjanSCC(g *rawGraph) [][]string {
	adj := make(map[string][]string)
	for _, e := range g.edges {
		adj[e.from] = append(adj[e.from], e.to)
	}

	st := &tarjanState{
		index:   make(map[string]int),
		lowlink: make(map[string]int),
		onStack: make(map[string]bool),
	}
	for _, n := range g.nodeOrder {
		if _, visited := st.index[n]; !visited {
			st.strongConnect(n, adj)
		}
	}
	return st.sccs
}

func (st *tarjanState) strongConnect(v string, adj map[string][]string) {
	st.index[v] = st.counter
	st.lowlink[v] = st.counter
	st.counter++
	st.stack = append(st.stack, v)
	st.onStack[v] = true

	for _, w := range adj[v] {
		if _, visited := st.index[w]; !visited {
			st.strongConnect(w, adj)
			if st.lowlink[w] < st.lowlink[v] {
				st.lowlink[v] = st.lowlink[w]
			}
		} else if st.onStack[w] {
			if st.index[w] < st.lowlink[v] {
				st.lowlink[v] = st.index[w]
			}
		}
	}

	// Pop SCC.
	if st.lowlink[v] == st.index[v] {
		var scc []string
		for {
			w := st.stack[len(st.stack)-1]
			st.stack = st.stack[:len(st.stack)-1]
			st.onStack[w] = false
			scc = append(scc, w)
			if w == v {
				break
			}
		}
		st.sccs = append(st.sccs, scc)
	}
}

// --- Helpers ---

// parseSpaceSeparatedIDs splits a space-and-comma-separated string of node IDs.
// Empty entries are omitted.
func parseSpaceSeparatedIDs(s string) []string {
	s = strings.ReplaceAll(s, ",", " ")
	parts := strings.Fields(s)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
