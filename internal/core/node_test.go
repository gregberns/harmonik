package core

import (
	"encoding/json"
	"testing"
)

// b3f73NodeValid returns a fully-populated Node with all required fields set to
// valid values. Tests mutate individual fields to probe Valid().
// Bead prefix: b3f73 (per implementer-protocol.md helper-prefix discipline).
func b3f73NodeValid(t *testing.T) Node {
	t.Helper()
	handlerRef := "handlers/my-handler"
	return Node{
		NodeID:            NodeID("node-001"),
		Type:              NodeTypeAgentic,
		HandlerRef:        &handlerRef,
		Timeout:           nil,
		RequiredSkills:    []string{},
		PolicyRef:         nil,
		GateRef:           nil,
		FreedomProfileRef: nil,
		BudgetRef:         nil,
		IdempotencyClass:  IdempotencyClassIdempotent,
		Axes:              "llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent",
		ModeTag:           ModeTagMechanism,
		SubWorkflowRef:    nil,
	}
}

// b3f73NodeNonAgentic returns a valid non-agentic Node with no HandlerRef.
func b3f73NodeNonAgentic(t *testing.T) Node {
	t.Helper()
	n := b3f73NodeValid(t)
	n.Type = NodeTypeNonAgentic
	n.HandlerRef = nil
	return n
}

// b3f73NodeSubWorkflow returns a valid sub-workflow Node with SubWorkflowRef set.
func b3f73NodeSubWorkflow(t *testing.T) Node {
	t.Helper()
	ref := "workflows/sub-wf-001"
	n := b3f73NodeValid(t)
	n.Type = NodeTypeSubWorkflow
	n.HandlerRef = nil
	n.SubWorkflowRef = &ref
	return n
}

func TestNodeValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	n := b3f73NodeValid(t)
	if !n.Valid() {
		t.Error("Valid() = false for fully-populated agentic Node, want true")
	}
}

func TestNodeValid_NonAgenticNode(t *testing.T) {
	t.Parallel()

	n := b3f73NodeNonAgentic(t)
	if !n.Valid() {
		t.Error("Valid() = false for fully-populated non-agentic Node, want true")
	}
}

func TestNodeValid_SubWorkflowNode(t *testing.T) {
	t.Parallel()

	n := b3f73NodeSubWorkflow(t)
	if !n.Valid() {
		t.Error("Valid() = false for fully-populated sub-workflow Node, want true")
	}
}

func TestNodeValid_AllNodeTypes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		nodeType NodeType
		wantRef  bool // true = need HandlerRef; false = need SubWorkflowRef or neither
	}{
		{NodeTypeAgentic, true},
		{NodeTypeNonAgentic, false},
		{NodeTypeGate, false},
		{NodeTypeControlPoint, false},
		{NodeTypeSubWorkflow, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.nodeType), func(t *testing.T) {
			t.Parallel()
			var n Node
			if tc.wantRef {
				n = b3f73NodeValid(t)
			} else if tc.nodeType == NodeTypeSubWorkflow {
				n = b3f73NodeSubWorkflow(t)
			} else {
				n = b3f73NodeNonAgentic(t)
				n.Type = tc.nodeType
			}
			if !n.Valid() {
				t.Errorf("Valid() = false for type=%q, want true", tc.nodeType)
			}
		})
	}
}

func TestNodeValid_EmptyNodeID(t *testing.T) {
	t.Parallel()

	n := b3f73NodeValid(t)
	n.NodeID = ""
	if n.Valid() {
		t.Error("Valid() = true with empty NodeID, want false")
	}
}

func TestNodeValid_InvalidType(t *testing.T) {
	t.Parallel()

	n := b3f73NodeValid(t)
	n.Type = NodeType("unknown-type")
	if n.Valid() {
		t.Error("Valid() = true with invalid NodeType, want false")
	}
}

func TestNodeValid_AgenticMissingHandlerRef(t *testing.T) {
	t.Parallel()

	n := b3f73NodeValid(t)
	n.HandlerRef = nil
	if n.Valid() {
		t.Error("Valid() = true for agentic Node with nil HandlerRef, want false")
	}
}

func TestNodeValid_NonAgenticWithHandlerRef(t *testing.T) {
	t.Parallel()

	ref := "handlers/foo"
	n := b3f73NodeNonAgentic(t)
	n.HandlerRef = &ref
	if n.Valid() {
		t.Error("Valid() = true for non-agentic Node with non-nil HandlerRef, want false")
	}
}

func TestNodeValid_GateWithHandlerRef(t *testing.T) {
	t.Parallel()

	ref := "handlers/foo"
	n := b3f73NodeNonAgentic(t)
	n.Type = NodeTypeGate
	n.HandlerRef = &ref
	if n.Valid() {
		t.Error("Valid() = true for gate Node with non-nil HandlerRef, want false")
	}
}

func TestNodeValid_ControlPointWithHandlerRef(t *testing.T) {
	t.Parallel()

	ref := "handlers/foo"
	n := b3f73NodeNonAgentic(t)
	n.Type = NodeTypeControlPoint
	n.HandlerRef = &ref
	if n.Valid() {
		t.Error("Valid() = true for control-point Node with non-nil HandlerRef, want false")
	}
}

func TestNodeValid_NegativeTimeout(t *testing.T) {
	t.Parallel()

	secs := -5
	n := b3f73NodeValid(t)
	n.Timeout = &secs
	if n.Valid() {
		t.Error("Valid() = true with negative Timeout, want false")
	}
}

func TestNodeValid_ZeroTimeout(t *testing.T) {
	t.Parallel()

	secs := 0
	n := b3f73NodeValid(t)
	n.Timeout = &secs
	if n.Valid() {
		t.Error("Valid() = true with zero Timeout, want false")
	}
}

func TestNodeValid_PositiveTimeout(t *testing.T) {
	t.Parallel()

	secs := 30
	n := b3f73NodeValid(t)
	n.Timeout = &secs
	if !n.Valid() {
		t.Error("Valid() = false with positive Timeout, want true")
	}
}

func TestNodeValid_InvalidIdempotencyClass(t *testing.T) {
	t.Parallel()

	n := b3f73NodeValid(t)
	n.IdempotencyClass = IdempotencyClass("unknown")
	if n.Valid() {
		t.Error("Valid() = true with invalid IdempotencyClass, want false")
	}
}

func TestNodeValid_AllIdempotencyClasses(t *testing.T) {
	t.Parallel()

	classes := []IdempotencyClass{
		IdempotencyClassIdempotent,
		IdempotencyClassNonIdempotent,
		IdempotencyClassRecoverableNonIdempotent,
	}
	for _, c := range classes {
		c := c
		t.Run(string(c), func(t *testing.T) {
			t.Parallel()
			n := b3f73NodeValid(t)
			n.IdempotencyClass = c
			if !n.Valid() {
				t.Errorf("Valid() = false for idempotency_class=%q, want true", c)
			}
		})
	}
}

func TestNodeValid_EmptyAxes(t *testing.T) {
	t.Parallel()

	n := b3f73NodeValid(t)
	n.Axes = ""
	if n.Valid() {
		t.Error("Valid() = true with empty Axes, want false")
	}
}

func TestNodeValid_EmptyModeTag(t *testing.T) {
	t.Parallel()

	n := b3f73NodeValid(t)
	n.ModeTag = ModeTag("")
	if n.Valid() {
		t.Error("Valid() = true with empty ModeTag, want false")
	}
}

func TestNodeValid_SubWorkflowMissingSubWorkflowRef(t *testing.T) {
	t.Parallel()

	n := b3f73NodeSubWorkflow(t)
	n.SubWorkflowRef = nil
	if n.Valid() {
		t.Error("Valid() = true for sub-workflow Node with nil SubWorkflowRef, want false")
	}
}

func TestNodeValid_NonSubWorkflowWithSubWorkflowRef(t *testing.T) {
	t.Parallel()

	ref := "workflows/wf-001"
	n := b3f73NodeValid(t) // agentic
	n.SubWorkflowRef = &ref
	if n.Valid() {
		t.Error("Valid() = true for agentic Node with non-nil SubWorkflowRef, want false")
	}
}

func TestNodeValid_GateWithSubWorkflowRef(t *testing.T) {
	t.Parallel()

	ref := "workflows/wf-001"
	n := b3f73NodeNonAgentic(t)
	n.Type = NodeTypeGate
	n.SubWorkflowRef = &ref
	if n.Valid() {
		t.Error("Valid() = true for gate Node with non-nil SubWorkflowRef, want false")
	}
}

func TestNodeValid_RequiredSkillsNil(t *testing.T) {
	t.Parallel()

	// nil RequiredSkills is valid — spec says List<String>, empty list allowed.
	n := b3f73NodeValid(t)
	n.RequiredSkills = nil
	if !n.Valid() {
		t.Error("Valid() = false with nil RequiredSkills, want true (empty list is valid)")
	}
}

func TestNodeValid_RequiredSkillsNonEmpty(t *testing.T) {
	t.Parallel()

	n := b3f73NodeValid(t)
	n.RequiredSkills = []string{"go", "git"}
	if !n.Valid() {
		t.Error("Valid() = false with populated RequiredSkills, want true")
	}
}

func TestNodeValid_OptionalRefsNil(t *testing.T) {
	t.Parallel()

	// All optional refs nil — fully valid for non-agentic node.
	n := b3f73NodeNonAgentic(t)
	n.PolicyRef = nil
	n.GateRef = nil
	n.FreedomProfileRef = nil
	n.BudgetRef = nil
	if !n.Valid() {
		t.Error("Valid() = false with all optional refs nil, want true")
	}
}

func TestNodeValid_OptionalRefsSet(t *testing.T) {
	t.Parallel()

	pRef := "policies/p001"
	gRef := "gates/g001"
	fRef := "freedom-profiles/fp001"
	bRef := "budgets/b001"

	n := b3f73NodeNonAgentic(t)
	n.PolicyRef = &pRef
	n.GateRef = &gRef
	n.FreedomProfileRef = &fRef
	n.BudgetRef = &bRef
	if !n.Valid() {
		t.Error("Valid() = false with all optional refs set, want true")
	}
}

// TestNodeValid_SubWorkflowWithHandlerRef verifies EM-007: HandlerRef is
// forbidden when Type == NodeTypeSubWorkflow.
func TestNodeValid_SubWorkflowWithHandlerRef(t *testing.T) {
	t.Parallel()

	ref := "handlers/foo"
	n := b3f73NodeSubWorkflow(t)
	n.HandlerRef = &ref
	if n.Valid() {
		t.Error("Valid() = true for sub-workflow Node with non-nil HandlerRef, want false (EM-007)")
	}
}

// TestNodeValid_ControlPointWithSubWorkflowRef verifies that SubWorkflowRef is
// forbidden when Type == NodeTypeControlPoint (EM-007 / §6.1 invariant).
func TestNodeValid_ControlPointWithSubWorkflowRef(t *testing.T) {
	t.Parallel()

	ref := "workflows/wf-001"
	n := b3f73NodeNonAgentic(t)
	n.Type = NodeTypeControlPoint
	n.SubWorkflowRef = &ref
	if n.Valid() {
		t.Error("Valid() = true for control-point Node with non-nil SubWorkflowRef, want false")
	}
}

// TestNodeTimeoutJSONRoundTrip verifies that Timeout serialises as an integer
// number of seconds (not nanoseconds) per execution-model.md §6.1
// ("Integer | None — positive seconds").
func TestNodeTimeoutJSONRoundTrip(t *testing.T) {
	t.Parallel()

	secs := 120
	n := b3f73NodeValid(t)
	n.Timeout = &secs

	data, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// Decode into a generic map so we can inspect the raw Timeout value.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal into map: %v", err)
	}

	rawTimeout, ok := raw["Timeout"]
	if !ok {
		t.Fatal("Timeout field absent from JSON output")
	}
	// json.Number / float64 — both are numeric. json.Unmarshal into any gives float64.
	got, ok := rawTimeout.(float64)
	if !ok {
		t.Fatalf("Timeout JSON value type = %T, want float64 (integer seconds)", rawTimeout)
	}
	if got != float64(secs) {
		t.Errorf("Timeout JSON value = %v, want %v (integer seconds per §6.1)", got, secs)
	}

	// Round-trip: unmarshal back and verify the value is preserved.
	var n2 Node
	if err := json.Unmarshal(data, &n2); err != nil {
		t.Fatalf("json.Unmarshal back to Node: %v", err)
	}
	if n2.Timeout == nil {
		t.Fatal("Timeout is nil after round-trip, want non-nil")
	}
	if *n2.Timeout != secs {
		t.Errorf("Timeout after round-trip = %d, want %d", *n2.Timeout, secs)
	}
}

// TestNodeTimeoutNilJSON verifies that a nil Timeout serialises as JSON null.
func TestNodeTimeoutNilJSON(t *testing.T) {
	t.Parallel()

	n := b3f73NodeValid(t)
	n.Timeout = nil

	data, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal into map: %v", err)
	}

	rawTimeout, ok := raw["Timeout"]
	if !ok {
		t.Fatal("Timeout field absent from JSON output")
	}
	if rawTimeout != nil {
		t.Errorf("Timeout JSON value = %v, want null for nil Timeout", rawTimeout)
	}
}
