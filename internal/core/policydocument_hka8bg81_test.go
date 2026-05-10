package core

import (
	"errors"
	"testing"

	"github.com/expr-lang/expr"
)

// ---------------------------------------------------------------------------
// Fixtures — hk-a8bg.81 helper prefix: policyDocFixture
// ---------------------------------------------------------------------------

// policyDocFixtureValidYAML returns a valid minimal policy YAML document
// containing all seven required sections (CP-035) with one entry per section
// and a mechanism-tagged expression in the gate evaluator.
func policyDocFixtureValidYAML(t *testing.T) []byte {
	t.Helper()
	return []byte(`
metadata:
  name: harness-policy
  version: "0.1.0"
  author: test-harness
  schema_version: 2

roles:
  - name: orchestrator
    permission_schema:
      allowed_tools: [bash]
      writable_paths: ["**"]
      readable_paths: ["**"]
      default_skills: [beads-cli]
      allowed_hooks: [on-agent-started-hook]
      invocable_by: []
    status: mvh-required

freedom_profiles:
  - name: default-profile
    tool_whitelist: [bash]
    writable_paths: ["**"]
    max_iterations: 10

gates:
  - name: review-gate
    subtype: goal-gate
    attach_point: node-pre-entry
    evaluator:
      mode: mechanism
      expression: 'run.state == "ready"'

hooks:
  - name: on-agent-started-hook
    trigger_event: on_agent_started
    side_effect_kind: emit-event
    halt_on_failure: false
    subsystem_priority: 10
    evaluator:
      mode: mechanism
      expression: 'event != nil'

guards:
  - name: edge-reorder-guard
    evaluator:
      mode: mechanism
      expression: 'edges'

budgets:
  - name: token-budget
    resource: tokens
    scope: per_run
    limit: 50000
    warning_threshold: 0.8
    scope_target: "*"
`)
}

// policyDocFixtureMissingSectionYAML returns a YAML document missing the
// specified section key, for CP-035 missing-section rejection tests.
func policyDocFixtureMissingSectionYAML(t *testing.T, missSection string) []byte {
	t.Helper()
	full := string(policyDocFixtureValidYAML(t))

	// Remove the target section by stripping the key and its indented block.
	// We use a simple line-filter approach suitable for fixtures.
	import_ := missSection + ":"
	var lines []string
	skip := false
	for _, line := range splitLines(full) {
		if len(line) > 0 && !isIndented(line) && line != import_ {
			// Top-level key that is not our target — stop skipping.
			skip = false
		}
		if line == import_ {
			skip = true
			continue
		}
		if !skip {
			lines = append(lines, line)
		}
	}
	return []byte(joinLines(lines))
}

// splitLines splits s on newlines.
func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// joinLines joins lines with newlines.
func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return result
}

// isIndented reports whether line starts with whitespace (is a nested YAML value).
func isIndented(line string) bool {
	return len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
}

// ---------------------------------------------------------------------------
// CP-035: Required-section validation
// ---------------------------------------------------------------------------

// TestPolicyDocumentValidateSections_AllPresent verifies that a document with
// all seven required sections passes ValidateSections (CP-035).
func TestPolicyDocumentValidateSections_AllPresent(t *testing.T) {
	t.Parallel()

	data := policyDocFixtureValidYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if err := doc.ValidateSections(); err != nil {
		t.Errorf("ValidateSections() = %v, want nil (all sections present)", err)
	}
}

// TestPolicyDocumentValidateSections_MissingSection verifies that absence of
// each required section triggers a ErrMissingPolicySection error (CP-035).
func TestPolicyDocumentValidateSections_MissingSection(t *testing.T) {
	t.Parallel()

	for _, section := range requiredSections {
		section := section
		t.Run("missing_"+section, func(t *testing.T) {
			t.Parallel()

			data := policyDocFixtureMissingSectionYAML(t, section)
			doc, err := ParsePolicyDocument(data)
			if err != nil {
				t.Fatalf("ParsePolicyDocument: %v", err)
			}
			err = doc.ValidateSections()
			if err == nil {
				t.Errorf("ValidateSections() = nil, want ErrMissingPolicySection for missing %q", section)
				return
			}
			if !errors.Is(err, ErrMissingPolicySection) {
				t.Errorf("ValidateSections() error = %v, want errors.Is(ErrMissingPolicySection)", err)
			}
		})
	}
}

// TestPolicyDocumentValidateSections_EmptySectionsValid verifies that a section
// present but with an empty list is NOT a missing-section error per CP-035
// (missing = absent key, not empty list).
func TestPolicyDocumentValidateSections_EmptySectionsValid(t *testing.T) {
	t.Parallel()

	data := []byte(`
metadata:
  name: minimal
  version: "0.1.0"
  author: test
  schema_version: 1
roles: []
freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if err := doc.ValidateSections(); err != nil {
		t.Errorf("ValidateSections() = %v, want nil (empty sections still present)", err)
	}
}

// ---------------------------------------------------------------------------
// CP-037: Config-loading precedence (4-layer deep-merge)
// ---------------------------------------------------------------------------

// TestMergeConfigs_PrecedenceOrder verifies that the four-layer merge applies
// precedence correctly (runtime > operator-policy > workflow-def > default)
// per §4.7.CP-037.
func TestMergeConfigs_PrecedenceOrder(t *testing.T) {
	t.Parallel()

	defaultCfg := PolicyConfig{SchemaVersion: 1, ExtraFields: map[string]string{
		"timeout": "60",
		"layer":   "default",
	}}
	workflowDef := PolicyConfig{ExtraFields: map[string]string{
		"timeout": "120",
		"layer":   "workflow",
	}}
	operatorPolicy := PolicyConfig{SchemaVersion: 2, ExtraFields: map[string]string{
		"layer": "operator",
	}}
	runtimeOverride := PolicyConfig{ExtraFields: map[string]string{
		"layer": "runtime",
	}}

	got := MergeConfigs(runtimeOverride, operatorPolicy, workflowDef, defaultCfg)

	// runtime wins on "layer"
	if got.ExtraFields["layer"] != "runtime" {
		t.Errorf("layer = %q, want %q (runtime wins)", got.ExtraFields["layer"], "runtime")
	}

	// operator-policy wins on schema_version (runtime did not set it)
	if got.SchemaVersion != 2 {
		t.Errorf("SchemaVersion = %d, want 2 (operator-policy wins)", got.SchemaVersion)
	}

	// workflow-def wins on "timeout" (no higher layer set it)
	if got.ExtraFields["timeout"] != "120" {
		t.Errorf("timeout = %q, want %q (workflow-def wins over default)", got.ExtraFields["timeout"], "120")
	}
}

// TestMergeConfigs_DefaultFallthrough verifies that fields absent from every
// higher-precedence layer fall through to the default (CP-037).
func TestMergeConfigs_DefaultFallthrough(t *testing.T) {
	t.Parallel()

	defaultCfg := PolicyConfig{SchemaVersion: 1, ExtraFields: map[string]string{
		"only-in-default": "yes",
	}}
	got := MergeConfigs(PolicyConfig{}, PolicyConfig{}, PolicyConfig{}, defaultCfg)

	if got.ExtraFields["only-in-default"] != "yes" {
		t.Errorf("only-in-default = %q, want %q (default fallthrough)", got.ExtraFields["only-in-default"], "yes")
	}
	if got.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1 (default fallthrough)", got.SchemaVersion)
	}
}

// TestMergeConfigs_AllLayersEmpty verifies that merging four empty configs
// produces an empty result (no panic, no spurious fields).
func TestMergeConfigs_AllLayersEmpty(t *testing.T) {
	t.Parallel()

	got := MergeConfigs(PolicyConfig{}, PolicyConfig{}, PolicyConfig{}, PolicyConfig{})
	if got.SchemaVersion != 0 {
		t.Errorf("SchemaVersion = %d, want 0 for all-empty merge", got.SchemaVersion)
	}
	if len(got.ExtraFields) != 0 {
		t.Errorf("ExtraFields = %v, want empty for all-empty merge", got.ExtraFields)
	}
}

// TestMergeConfigs_RuntimeAlwaysWins verifies that a non-zero runtime field
// always overrides every other layer (CP-037: runtime override > all).
func TestMergeConfigs_RuntimeAlwaysWins(t *testing.T) {
	t.Parallel()

	runtime := PolicyConfig{SchemaVersion: 99, ExtraFields: map[string]string{"key": "runtime"}}
	operator := PolicyConfig{SchemaVersion: 5, ExtraFields: map[string]string{"key": "operator"}}
	workflow := PolicyConfig{SchemaVersion: 3, ExtraFields: map[string]string{"key": "workflow"}}
	def := PolicyConfig{SchemaVersion: 1, ExtraFields: map[string]string{"key": "default"}}

	got := MergeConfigs(runtime, operator, workflow, def)
	if got.SchemaVersion != 99 {
		t.Errorf("SchemaVersion = %d, want 99 (runtime wins)", got.SchemaVersion)
	}
	if got.ExtraFields["key"] != "runtime" {
		t.Errorf("key = %q, want %q (runtime wins)", got.ExtraFields["key"], "runtime")
	}
}

// ---------------------------------------------------------------------------
// CP-038: N-1 schema version readability
// ---------------------------------------------------------------------------

// TestPolicyDocumentNMinusOneReadability verifies that a reader accepts both
// the current schema version and the immediately prior version per CP-038.
func TestPolicyDocumentNMinusOneReadability(t *testing.T) {
	t.Parallel()

	// currentSchemaVersion is the MVH baseline schema version.
	const currentSchemaVersion = 2
	const priorSchemaVersion = currentSchemaVersion - 1

	cases := []struct {
		name          string
		schemaVersion int
		wantAccepted  bool
	}{
		{"current_N", currentSchemaVersion, true},
		{"prior_N_minus_1", priorSchemaVersion, true},
		{"too_old_N_minus_2", currentSchemaVersion - 2, false},
		{"future_N_plus_1", currentSchemaVersion + 1, true}, // future versions: reader accepts per additive-only rule
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ok := acceptsSchemaVersion(tc.schemaVersion, currentSchemaVersion)
			if ok != tc.wantAccepted {
				t.Errorf("acceptsSchemaVersion(%d, %d) = %v, want %v",
					tc.schemaVersion, currentSchemaVersion, ok, tc.wantAccepted)
			}
		})
	}
}

// acceptsSchemaVersion reports whether a reader at currentVersion can accept a
// document at docVersion per §4.7.CP-038 N-1 readability contract.
//
// A reader MUST accept:
//   - docVersion == currentVersion (current)
//   - docVersion == currentVersion-1 (N-1)
//   - docVersion > currentVersion (additive-only; future versions accepted per OQ-CP-001 default)
//
// A reader MUST reject docVersion < currentVersion-1 (breaking change; migration release required).
func acceptsSchemaVersion(docVersion, currentVersion int) bool {
	return docVersion >= currentVersion-1
}

// TestPolicyDocumentMetaSchemaVersion verifies that ParsePolicyDocument
// correctly populates Metadata.SchemaVersion from the YAML source, enabling
// readers to apply the N-1 acceptance rule (CP-038).
func TestPolicyDocumentMetaSchemaVersion(t *testing.T) {
	t.Parallel()

	data := policyDocFixtureValidYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if doc.Metadata.SchemaVersion != 2 {
		t.Errorf("Metadata.SchemaVersion = %d, want 2", doc.Metadata.SchemaVersion)
	}
}

// ---------------------------------------------------------------------------
// CP-034: expr-lang/expr round-trip on section expressions
// ---------------------------------------------------------------------------

// TestPolicyExprRoundTrip_GateExpression verifies that a mechanism-tagged gate
// expression from the policy YAML parses and evaluates correctly with
// expr-lang/expr (CP-034: expr-lang/expr grammar is adopted).
func TestPolicyExprRoundTrip_GateExpression(t *testing.T) {
	t.Parallel()

	data := policyDocFixtureValidYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if len(doc.Gates) == 0 {
		t.Fatal("fixture has no gates")
	}

	gate := doc.Gates[0]
	if gate.Evaluator.Mode != "mechanism" {
		t.Fatalf("gate evaluator mode = %q, want mechanism", gate.Evaluator.Mode)
	}
	expression := gate.Evaluator.Expression

	// Define a minimal environment with the fields used by the expression.
	env := map[string]any{
		"run": map[string]any{
			"state": "ready",
		},
	}

	// Compile and evaluate using expr-lang/expr.
	program, err := expr.Compile(expression, expr.Env(env))
	if err != nil {
		t.Fatalf("expr.Compile(%q): %v", expression, err)
	}
	output, err := expr.Run(program, env)
	if err != nil {
		t.Fatalf("expr.Run(%q): %v", expression, err)
	}

	got, ok := output.(bool)
	if !ok {
		t.Fatalf("gate expression returned %T (%v), want bool", output, output)
	}
	if !got {
		t.Errorf("gate expression %q evaluated to false, want true (state == ready)", expression)
	}
}

// TestPolicyExprRoundTrip_HookSubscriptionFilter verifies that a hook's
// subscription_filter expression parses correctly with expr-lang/expr (CP-034).
func TestPolicyExprRoundTrip_HookSubscriptionFilter(t *testing.T) {
	t.Parallel()

	// Use an expression with an event binding (Hook context).
	expression := `event != nil && event.type == "agent_started"`
	env := map[string]any{
		"event": map[string]any{
			"type": "agent_started",
		},
	}

	program, err := expr.Compile(expression, expr.Env(env))
	if err != nil {
		t.Fatalf("expr.Compile(%q): %v", expression, err)
	}
	output, err := expr.Run(program, env)
	if err != nil {
		t.Fatalf("expr.Run(%q): %v", expression, err)
	}

	got, ok := output.(bool)
	if !ok {
		t.Fatalf("hook filter expression returned %T (%v), want bool", output, output)
	}
	if !got {
		t.Errorf("hook filter %q = false, want true", expression)
	}
}

// TestPolicyExprRoundTrip_GuardExpression verifies that a guard expression
// (returns List<Edge> per §6.4.2) compiles with expr-lang/expr (CP-034).
func TestPolicyExprRoundTrip_GuardExpression(t *testing.T) {
	t.Parallel()

	// Guard expressions return a reordered edge list. Use a simple identity
	// expression (return edges unchanged) as the round-trip fixture.
	expression := `edges`
	edgeList := []any{"edge-a", "edge-b"}
	env := map[string]any{
		"edges": edgeList,
	}

	program, err := expr.Compile(expression, expr.Env(env))
	if err != nil {
		t.Fatalf("expr.Compile(%q): %v", expression, err)
	}
	output, err := expr.Run(program, env)
	if err != nil {
		t.Fatalf("expr.Run(%q): %v", expression, err)
	}

	got, ok := output.([]any)
	if !ok {
		t.Fatalf("guard expression returned %T (%v), want []any", output, output)
	}
	if len(got) != len(edgeList) {
		t.Errorf("guard expression returned %d edges, want %d", len(got), len(edgeList))
	}
}

// TestPolicyExprRoundTrip_PolicyMetaBinding verifies that policy_meta is
// accessible from expressions (CP-034 environment binding includes policy_meta).
func TestPolicyExprRoundTrip_PolicyMetaBinding(t *testing.T) {
	t.Parallel()

	expression := `policy_meta["author"] == "test-harness"`
	env := map[string]any{
		"policy_meta": map[string]any{
			"author": "test-harness",
		},
	}

	program, err := expr.Compile(expression, expr.Env(env))
	if err != nil {
		t.Fatalf("expr.Compile(%q): %v", expression, err)
	}
	output, err := expr.Run(program, env)
	if err != nil {
		t.Fatalf("expr.Run(%q): %v", expression, err)
	}

	got, ok := output.(bool)
	if !ok {
		t.Fatalf("policy_meta expression returned %T (%v), want bool", output, output)
	}
	if !got {
		t.Errorf("policy_meta expression %q = false, want true", expression)
	}
}

// ---------------------------------------------------------------------------
// CP-036: DOT attributes reference policy YAML by name (no inline bodies)
// ---------------------------------------------------------------------------

// TestPolicyDocumentDOTNameResolution verifies that policy elements in the
// document have non-empty names, confirming they are registered-name references
// per CP-036 (DOT attributes carry only the registered name, not inline bodies).
func TestPolicyDocumentDOTNameResolution(t *testing.T) {
	t.Parallel()

	data := policyDocFixtureValidYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}

	// Every gate must have a non-empty name (DOT gate_ref carries the name).
	for i, g := range doc.Gates {
		if g.Name == "" {
			t.Errorf("gates[%d].name is empty; DOT gate_ref must carry a non-empty registered name (CP-036)", i)
		}
	}

	// Every hook must have a non-empty name (DOT hook references by name).
	for i, h := range doc.Hooks {
		if h.Name == "" {
			t.Errorf("hooks[%d].name is empty; hook must carry a non-empty registered name (CP-036)", i)
		}
	}

	// Every guard must have a non-empty name.
	for i, g := range doc.Guards {
		if g.Name == "" {
			t.Errorf("guards[%d].name is empty; guard must carry a non-empty registered name (CP-036)", i)
		}
	}

	// Every budget must have a non-empty name (DOT budget_ref carries the name).
	for i, b := range doc.Budgets {
		if b.Name == "" {
			t.Errorf("budgets[%d].name is empty; DOT budget_ref must carry a non-empty registered name (CP-036)", i)
		}
	}
}

// TestPolicyDocumentDOTNameResolution_FreedomProfile verifies that freedom
// profiles carry non-empty names referenceable from DOT freedom_profile_ref
// (CP-036).
func TestPolicyDocumentDOTNameResolution_FreedomProfile(t *testing.T) {
	t.Parallel()

	data := policyDocFixtureValidYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}

	for i, fp := range doc.FreedomProfiles {
		if fp.Name == "" {
			t.Errorf("freedom_profiles[%d].name is empty; DOT freedom_profile_ref must carry a non-empty registered name (CP-036)", i)
		}
	}
}

// TestPolicyDocumentParsed_SectionContents verifies that ParsePolicyDocument
// correctly populates all required sections from the fixture YAML.
func TestPolicyDocumentParsed_SectionContents(t *testing.T) {
	t.Parallel()

	data := policyDocFixtureValidYAML(t)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}

	if doc.Metadata.Name == "" {
		t.Error("Metadata.Name is empty")
	}
	if doc.Metadata.Author == "" {
		t.Error("Metadata.Author is empty")
	}
	if len(doc.Roles) == 0 {
		t.Error("Roles is empty")
	}
	if len(doc.FreedomProfiles) == 0 {
		t.Error("FreedomProfiles is empty")
	}
	if len(doc.Gates) == 0 {
		t.Error("Gates is empty")
	}
	if len(doc.Hooks) == 0 {
		t.Error("Hooks is empty")
	}
	if len(doc.Guards) == 0 {
		t.Error("Guards is empty")
	}
	if len(doc.Budgets) == 0 {
		t.Error("Budgets is empty")
	}
}
