package core

// DelegationPath is the cognition-tagged evaluator routing record that names
// the delegation target for a Gate or Hook whose evaluator is cognition-tagged
// (specs/control-points.md §6.1.5 RECORD DelegationPath).
//
// A cognition-tagged evaluator MUST name its delegation path explicitly on the
// ControlPoint record per CP-039 (§4.8). A ControlPoint whose evaluator.mode =
// cognition but whose evaluator.delegation_path is nil fails registration.
//
//	RECORD DelegationPath:
//	    role                 : String  -- role name per [architecture.md §4.8]
//	    model_class          : String  -- e.g., "reviewer-tier-1"
//	    input_schema_ref     : String  -- registered input schema name
//	    response_schema_ref  : String  -- registered response schema name
//	    prompt_template_ref  : String  -- registered prompt template (provisioned via skill per §4.11)
type DelegationPath struct {
	// Role is the role name (architecture.md §4.8) that receives the delegation.
	// Must be non-empty.
	Role string `json:"role"`

	// ModelClass identifies the model tier used for this delegation
	// (e.g., "reviewer-tier-1"). Must be non-empty.
	ModelClass string `json:"model_class"`

	// InputSchemaRef names the registered input schema the delegation path
	// declares. Checked at registration per CP-039. Must be non-empty.
	InputSchemaRef string `json:"input_schema_ref"`

	// ResponseSchemaRef names the registered response schema. Must be non-empty.
	ResponseSchemaRef string `json:"response_schema_ref"`

	// PromptTemplateRef names the registered prompt template provisioned via
	// skill per handler-contract.md §4.11. Must be non-empty.
	PromptTemplateRef string `json:"prompt_template_ref"`
}

// Valid reports whether d is a structurally well-formed DelegationPath.
// All five fields must be non-empty; runtime resolution of the registered
// names is deferred to daemon-init registration per CP-039.
func (d DelegationPath) Valid() bool {
	return d.Role != "" &&
		d.ModelClass != "" &&
		d.InputSchemaRef != "" &&
		d.ResponseSchemaRef != "" &&
		d.PromptTemplateRef != ""
}
