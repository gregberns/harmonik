package dot

// validator.go — Workflow-graph validator per specs/workflow-graph.md §9.
//
// The validator layer sits above the parser: it receives an already-parsed
// *Graph (all ParseErrors already caught) and applies the remaining structural
// and semantic checks that require cross-node context.
//
// Spec refs:
//   - specs/workflow-graph.md §4 WG-002        — per-node required/optional attribute catalog.
//   - specs/workflow-graph.md §4 WG-005        — gate node requires gate_ref AND handler_ref.
//   - specs/workflow-graph.md §4 WG-008        — idempotency_class required on agentic/non-agentic; forbidden on gate/sub-workflow.
//   - specs/workflow-graph.md §8 WG-023        — terminal nodes have no outgoing edges.
//   - specs/workflow-graph.md §9 WG-024        — reserved-attribute strictness (per-type required/forbidden).
//   - specs/workflow-graph.md §9 WG-027        — well-formedness: start_node, terminal_node_ids, reachability.
//   - specs/workflow-graph.md §9 WG-028        — cycle bounding via traversal_cap.
//   - specs/workflow-graph.md §11 WG-034       — schema_version N-1 readability.
//   - specs/workflow-graph.md §11 WG-035       — workflow version present.
//
// Tags: mechanism, normative

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
)

// currentSchemaVersion is the schema version this engine understands.
// Graphs at this version and (currentSchemaVersion - 1) are accepted per WG-034.
const currentSchemaVersion = 1

// DiagnosticSeverity classifies a validation finding.
type DiagnosticSeverity int

const (
	// SeverityError means the graph MUST NOT start.
	SeverityError DiagnosticSeverity = iota
	// SeverityWarning means the graph MAY start; actionable for tooling.
	SeverityWarning
)

func (s DiagnosticSeverity) String() string {
	if s == SeverityWarning {
		return "warning"
	}
	return "error"
}

// Diagnostic is one validation finding produced by Validate.
type Diagnostic struct {
	Severity DiagnosticSeverity
	// Line is the 1-based source line of the offending construct; 0 = unknown.
	Line    int
	Code    string // WG-NNN, CP-NNN, or EM-NNN stable tag
	Message string
}

func (d Diagnostic) String() string {
	if d.Line > 0 {
		return fmt.Sprintf("%s: dot:%d [%s]: %s", d.Severity, d.Line, d.Code, d.Message)
	}
	return fmt.Sprintf("%s: [%s]: %s", d.Severity, d.Code, d.Message)
}

// Validate validates an already-parsed *Graph against the §9 WG-024 through
// WG-028 obligations plus the §11 WG-034/WG-035 schema-version contracts.
//
// Returns a (possibly empty) slice of Diagnostics. Any diagnostic with
// SeverityError means the workflow MUST NOT start per WG-024.
//
// The caller must first call Parse; any ParseErrors are already strict errors
// and prevent Validate from seeing a malformed graph. Validate assumes the
// input *Graph was produced by a successful Parse call.
//
// Tags: mechanism
func Validate(g *Graph) []Diagnostic {
	var diags []Diagnostic

	// WG-034: schema_version N-1 readability.
	diags = append(diags, checkSchemaVersion(g)...)

	// WG-035: workflow version must be present.
	if strings.TrimSpace(g.Version) == "" {
		diags = append(diags, diagError(0, "WG-035",
			"workflow must declare a top-level \"version\" attribute (WG-035)"))
	}

	// WG-024/WG-002/WG-005/WG-008: per-node required/forbidden attribute checks.
	for _, n := range g.Nodes {
		diags = append(diags, checkNodeAttrs(n)...)
	}

	// Build node index for cross-node checks.
	nodeIndex := make(map[string]*Node, len(g.Nodes))
	for _, n := range g.Nodes {
		nodeIndex[n.ID] = n
	}

	// WG-027: well-formedness checks.
	diags = append(diags, checkWellFormedness(g, nodeIndex)...)

	// WG-028: cycle bounding.
	diags = append(diags, checkCycleBounds(g)...)

	return diags
}

// ── WG-034/035 schema version ─────────────────────────────────────────────────

func checkSchemaVersion(g *Graph) []Diagnostic {
	raw := strings.TrimSpace(g.SchemaVersion)
	if raw == "" {
		return []Diagnostic{diagError(0, "WG-033",
			"workflow must declare a graph-level \"schema_version\" attribute (WG-033)")}
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return []Diagnostic{diagError(0, "WG-033",
			fmt.Sprintf("schema_version %q must be a positive integer (WG-033)", raw))}
	}
	// WG-034: accept current and N-1.
	if v < currentSchemaVersion-1 || v > currentSchemaVersion {
		return []Diagnostic{diagError(0, "WG-034",
			fmt.Sprintf("schema_version %d is outside the accepted range [%d, %d] (WG-034 N-1 readability)",
				v, currentSchemaVersion-1, currentSchemaVersion))}
	}
	return nil
}

// ── WG-024/WG-002/WG-005/WG-008 per-node required/forbidden attrs ────────────

func checkNodeAttrs(n *Node) []Diagnostic {
	var diags []Diagnostic
	// CP-056: policy_ref is deprecated and MUST be rejected on any node type.
	// Checked here for defense-in-depth; the parser also rejects it via ParseError.
	if _, ok := n.UnknownAttrs["policy_ref"]; ok {
		diags = append(diags, diagError(n.Line, "CP-056",
			fmt.Sprintf("node %q: attribute \"policy_ref\" is deprecated and must not be used (CP-056); use gate_ref, skills_ref, or freedom_profile_ref instead (CP-055)", n.ID)))
	}
	// Parser already rejected unknown type values (WG-001) and policy_ref (CP-056).
	// We handle the remaining per-type required/forbidden contracts here.
	switch n.Type {
	case core.NodeTypeAgentic:
		diags = append(diags, checkAgentic(n)...)
	case core.NodeTypeNonAgentic:
		diags = append(diags, checkNonAgentic(n)...)
	case core.NodeTypeGate:
		diags = append(diags, checkGate(n)...)
	case core.NodeTypeSubWorkflow:
		diags = append(diags, checkSubWorkflow(n)...)
	default:
		// Unknown type: parser already raised a ParseError; nothing to add here.
	}
	return diags
}

// checkAgentic validates an agentic node per WG-002/WG-008/WG-024.
func checkAgentic(n *Node) []Diagnostic {
	var diags []Diagnostic
	// Required: agent_type (WG-002).
	if strings.TrimSpace(n.AgentType) == "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (agentic): \"agent_type\" is required on agentic nodes (WG-002/WG-024)", n.ID)))
	}
	// Required: handler_ref (WG-002).
	if strings.TrimSpace(n.HandlerRef) == "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (agentic): \"handler_ref\" is required on agentic nodes (WG-002/WG-024)", n.ID)))
	}
	// Required: idempotency_class (WG-008/EM-010).
	checkIdempotencyClass(n.ID, n.Line, n.IdempotencyClass, &diags)
	// Forbidden: gate_ref, sub_workflow_ref (WG-024).
	if strings.TrimSpace(n.GateRef) != "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (agentic): \"gate_ref\" is forbidden on agentic nodes (WG-024)", n.ID)))
	}
	if strings.TrimSpace(n.SubWorkflowRef) != "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (agentic): \"sub_workflow_ref\" is forbidden on agentic nodes (WG-024)", n.ID)))
	}
	return diags
}

// checkNonAgentic validates a non-agentic node per WG-002/WG-008/WG-024.
func checkNonAgentic(n *Node) []Diagnostic {
	var diags []Diagnostic
	// Required: handler_ref (WG-002/WG-024 EM-007 amendment).
	if strings.TrimSpace(n.HandlerRef) == "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (non-agentic): \"handler_ref\" is required on non-agentic nodes (WG-024 / EM-007 amendment)", n.ID)))
	}
	// Required: idempotency_class (WG-008/EM-010).
	checkIdempotencyClass(n.ID, n.Line, n.IdempotencyClass, &diags)
	// Forbidden: agent_type (WG-024).
	if strings.TrimSpace(n.AgentType) != "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (non-agentic): \"agent_type\" is forbidden on non-agentic nodes (WG-024)", n.ID)))
	}
	// Forbidden: gate_ref, sub_workflow_ref (WG-024).
	if strings.TrimSpace(n.GateRef) != "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (non-agentic): \"gate_ref\" is forbidden on non-agentic nodes (WG-024)", n.ID)))
	}
	if strings.TrimSpace(n.SubWorkflowRef) != "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (non-agentic): \"sub_workflow_ref\" is forbidden on non-agentic nodes (WG-024)", n.ID)))
	}
	return diags
}

// checkGate validates a gate node per WG-005/WG-008/WG-024.
func checkGate(n *Node) []Diagnostic {
	var diags []Diagnostic
	// Required: gate_ref (WG-005).
	if strings.TrimSpace(n.GateRef) == "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (gate): \"gate_ref\" is required on gate nodes (WG-005/WG-024)", n.ID)))
	}
	// Required: handler_ref (WG-005/WG-024 EM-007 amendment).
	if strings.TrimSpace(n.HandlerRef) == "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (gate): \"handler_ref\" is required on gate nodes (WG-005/WG-024 EM-007 amendment)", n.ID)))
	}
	// Forbidden: agent_type (WG-024).
	if strings.TrimSpace(n.AgentType) != "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (gate): \"agent_type\" is forbidden on gate nodes (WG-024)", n.ID)))
	}
	// Forbidden: idempotency_class (WG-008).
	if strings.TrimSpace(n.IdempotencyClass) != "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (gate): \"idempotency_class\" is forbidden on gate nodes (WG-008/WG-024)", n.ID)))
	}
	// Forbidden: sub_workflow_ref (WG-024).
	if strings.TrimSpace(n.SubWorkflowRef) != "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (gate): \"sub_workflow_ref\" is forbidden on gate nodes (WG-024)", n.ID)))
	}
	return diags
}

// checkSubWorkflow validates a sub-workflow node per WG-006/WG-008/WG-024.
func checkSubWorkflow(n *Node) []Diagnostic {
	var diags []Diagnostic
	// Required: sub_workflow_ref (WG-006).
	if strings.TrimSpace(n.SubWorkflowRef) == "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (sub-workflow): \"sub_workflow_ref\" is required (WG-006/WG-024)", n.ID)))
	}
	// Required: workflow_version (WG-006).
	if strings.TrimSpace(n.WorkflowVersion) == "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (sub-workflow): \"workflow_version\" is required (WG-006/WG-024)", n.ID)))
	}
	// Forbidden: agent_type (WG-024).
	if strings.TrimSpace(n.AgentType) != "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (sub-workflow): \"agent_type\" is forbidden on sub-workflow nodes (WG-024)", n.ID)))
	}
	// Forbidden: idempotency_class (WG-008).
	if strings.TrimSpace(n.IdempotencyClass) != "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (sub-workflow): \"idempotency_class\" is forbidden on sub-workflow nodes (WG-008/WG-024)", n.ID)))
	}
	// Forbidden: gate_ref.
	if strings.TrimSpace(n.GateRef) != "" {
		diags = append(diags, diagError(n.Line, "WG-024",
			fmt.Sprintf("node %q (sub-workflow): \"gate_ref\" is forbidden on sub-workflow nodes (WG-024)", n.ID)))
	}
	return diags
}

// checkIdempotencyClass validates idempotency_class per WG-008/EM-010.
// Appends an error to diags when invalid.
func checkIdempotencyClass(nodeID string, line int, raw string, diags *[]Diagnostic) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		*diags = append(*diags, diagError(line, "WG-024",
			fmt.Sprintf("node %q: \"idempotency_class\" is required on agentic and non-agentic nodes (WG-008/EM-010)", nodeID)))
		return
	}
	ic := core.IdempotencyClass(raw)
	if !ic.Valid() {
		*diags = append(*diags, diagError(line, "WG-024",
			fmt.Sprintf("node %q: idempotency_class %q is not one of {idempotent, non-idempotent, recoverable-non-idempotent} (WG-008)", nodeID, raw)))
	}
}

// ── WG-027 well-formedness ────────────────────────────────────────────────────

func checkWellFormedness(g *Graph, nodeIndex map[string]*Node) []Diagnostic {
	var diags []Diagnostic

	// start_node must be present and refer to a declared node.
	startID := strings.TrimSpace(g.StartNodeID)
	if startID == "" {
		diags = append(diags, diagError(0, "WG-027",
			"workflow must declare a \"start_node\" graph-level attribute (WG-027)"))
	} else if _, ok := nodeIndex[startID]; !ok {
		diags = append(diags, diagError(0, "WG-027",
			fmt.Sprintf("start_node %q is not declared as a node (WG-027)", startID)))
	}

	// terminal_node_ids must be non-empty and all refer to declared nodes.
	if len(g.TerminalNodeIDs) == 0 {
		diags = append(diags, diagError(0, "WG-027",
			"workflow must declare a non-empty \"terminal_node_ids\" list (WG-027)"))
	}
	for _, tid := range g.TerminalNodeIDs {
		if _, ok := nodeIndex[tid]; !ok {
			diags = append(diags, diagError(0, "WG-027",
				fmt.Sprintf("terminal_node_id %q is not declared as a node (WG-027)", tid)))
		}
	}

	// Build terminal set.
	terminalSet := make(map[string]bool, len(g.TerminalNodeIDs))
	for _, tid := range g.TerminalNodeIDs {
		terminalSet[tid] = true
	}

	// Every edge's from/to must refer to declared nodes.
	for _, e := range g.Edges {
		if _, ok := nodeIndex[e.FromNodeID]; !ok {
			diags = append(diags, diagError(e.Line, "WG-027",
				fmt.Sprintf("edge source node %q is not declared (WG-027)", e.FromNodeID)))
		}
		if _, ok := nodeIndex[e.ToNodeID]; !ok {
			diags = append(diags, diagError(e.Line, "WG-027",
				fmt.Sprintf("edge target node %q is not declared (WG-027)", e.ToNodeID)))
		}
	}

	// WG-023: terminal nodes must have no outgoing edges.
	outEdges := make(map[string]bool)
	for _, e := range g.Edges {
		outEdges[e.FromNodeID] = true
	}
	for _, tid := range g.TerminalNodeIDs {
		if outEdges[tid] {
			diags = append(diags, diagError(0, "WG-023",
				fmt.Sprintf("terminal node %q must not have outgoing edges (WG-023)", tid)))
		}
	}

	// Reachability checks require a valid start node.
	if startID == "" {
		return diags
	}

	// Build adjacency for forward and backward reachability.
	forward := make(map[string][]string)
	backward := make(map[string][]string)
	for _, e := range g.Edges {
		forward[e.FromNodeID] = append(forward[e.FromNodeID], e.ToNodeID)
		backward[e.ToNodeID] = append(backward[e.ToNodeID], e.FromNodeID)
	}

	// Forward BFS from start_node.
	reachableFromStart := bfsReach(startID, forward)

	// Reverse BFS from terminal nodes.
	canReachTerminal := make(map[string]bool)
	for _, t := range g.TerminalNodeIDs {
		for n := range bfsReach(t, backward) {
			canReachTerminal[n] = true
		}
		canReachTerminal[t] = true
	}

	for _, n := range g.Nodes {
		// Every non-terminal node must be reachable from start_node (WG-027).
		if !terminalSet[n.ID] && !reachableFromStart[n.ID] {
			diags = append(diags, diagError(n.Line, "WG-027",
				fmt.Sprintf("node %q is not reachable from start_node %q (WG-027)", n.ID, startID)))
		}
		// Every terminal node must be reachable from start_node (WG-027).
		if terminalSet[n.ID] && !reachableFromStart[n.ID] {
			diags = append(diags, diagError(n.Line, "WG-027",
				fmt.Sprintf("terminal node %q is not reachable from start_node %q (WG-027)", n.ID, startID)))
		}
		// Every node must be able to reach a terminal (WG-027).
		if !canReachTerminal[n.ID] && !terminalSet[n.ID] {
			diags = append(diags, diagError(n.Line, "WG-027",
				fmt.Sprintf("node %q cannot reach any terminal_node_id (WG-027)", n.ID)))
		}
	}

	return diags
}

// bfsReach returns the set of nodes reachable from start in adj.
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

// ── WG-028 cycle bounding ─────────────────────────────────────────────────────

// checkCycleBounds verifies that every cycle has at least one edge with
// traversal_cap > 0 per WG-028 / EM-043.
func checkCycleBounds(g *Graph) []Diagnostic {
	if len(g.Edges) == 0 {
		return nil
	}
	var diags []Diagnostic
	sccs := tarjanSCCFromGraph(g)
	for _, scc := range sccs {
		if len(scc) < 2 && !hasSelfLoopInGraph(g, scc[0]) {
			continue
		}
		sccSet := make(map[string]bool, len(scc))
		for _, n := range scc {
			sccSet[n] = true
		}
		hasCap := false
		for _, e := range g.Edges {
			if !sccSet[e.FromNodeID] || !sccSet[e.ToNodeID] {
				continue
			}
			if rawCap, ok := e.UnknownAttrs["traversal_cap"]; ok {
				rawCap = strings.TrimSpace(rawCap)
				if n, err := strconv.Atoi(rawCap); err == nil && n > 0 {
					hasCap = true
					break
				}
			}
		}
		if !hasCap {
			diags = append(diags, diagError(0, "WG-028",
				fmt.Sprintf("cycle involving nodes [%s] has no edge with a positive traversal_cap (WG-028 / EM-043)",
					strings.Join(scc, ", "))))
		}
	}
	return diags
}

func hasSelfLoopInGraph(g *Graph, nodeID string) bool {
	for _, e := range g.Edges {
		if e.FromNodeID == nodeID && e.ToNodeID == nodeID {
			return true
		}
	}
	return false
}

// ── Tarjan SCC ────────────────────────────────────────────────────────────────

type sccState struct {
	index   map[string]int
	lowlink map[string]int
	onStack map[string]bool
	stack   []string
	counter int
	sccs    [][]string
}

func tarjanSCCFromGraph(g *Graph) [][]string {
	adj := make(map[string][]string)
	for _, e := range g.Edges {
		adj[e.FromNodeID] = append(adj[e.FromNodeID], e.ToNodeID)
	}
	st := &sccState{
		index:   make(map[string]int),
		lowlink: make(map[string]int),
		onStack: make(map[string]bool),
	}
	for _, n := range g.Nodes {
		if _, visited := st.index[n.ID]; !visited {
			st.connect(n.ID, adj)
		}
	}
	return st.sccs
}

func (st *sccState) connect(v string, adj map[string][]string) {
	st.index[v] = st.counter
	st.lowlink[v] = st.counter
	st.counter++
	st.stack = append(st.stack, v)
	st.onStack[v] = true

	for _, w := range adj[v] {
		if _, visited := st.index[w]; !visited {
			st.connect(w, adj)
			if st.lowlink[w] < st.lowlink[v] {
				st.lowlink[v] = st.lowlink[w]
			}
		} else if st.onStack[w] {
			if st.index[w] < st.lowlink[v] {
				st.lowlink[v] = st.index[w]
			}
		}
	}

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

// ── helpers ───────────────────────────────────────────────────────────────────

func diagError(line int, code, message string) Diagnostic {
	return Diagnostic{Severity: SeverityError, Line: line, Code: code, Message: message}
}
