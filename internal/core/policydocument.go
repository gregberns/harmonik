package core

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// PolicyDocument is the in-memory representation of a policy YAML document
// per specs/control-points.md §6.3.
//
// Every normative field declared in §6.3 appears here. The seven required
// sections (metadata, roles, freedom_profiles, gates, hooks, guards, budgets)
// MUST all be present; absence of any required section is a validation error
// per §4.7.CP-035.
//
// Tags: mechanism
type PolicyDocument struct {
	// Metadata is the document header block (§6.3: required).
	Metadata PolicyDocumentMeta `yaml:"metadata"`

	// Roles is the list of role permission-schema declarations (§6.3: required).
	// A nil or absent roles list is a validation error per CP-035.
	Roles []PolicyRole `yaml:"roles"`

	// FreedomProfiles is the list of freedom-profile declarations (§6.3: required).
	FreedomProfiles []PolicyFreedomProfile `yaml:"freedom_profiles"`

	// Gates is the list of Gate ControlPoint declarations (§6.3: required).
	Gates []PolicyGate `yaml:"gates"`

	// Hooks is the list of Hook ControlPoint declarations (§6.3: required).
	Hooks []PolicyHook `yaml:"hooks"`

	// Guards is the list of Guard ControlPoint declarations (§6.3: required).
	Guards []PolicyGuard `yaml:"guards"`

	// Budgets is the list of Budget ControlPoint declarations (§6.3: required).
	Budgets []PolicyBudget `yaml:"budgets"`

	// SkillSets is the optional list of named skill-set blocks referenceable
	// from DOT policy_ref (§4.11.CP-049).
	SkillSets []PolicySkillSet `yaml:"skill_sets,omitempty"`

	// sectionPresence tracks which top-level keys were explicitly present in the
	// YAML source (non-nil vs. absent). Required for CP-035 missing-section detection.
	sectionPresence policyDocumentSections
}

// policyDocumentSections tracks whether each required YAML section was
// explicitly present (even if empty) in the parsed document.
type policyDocumentSections struct {
	metadata        bool
	roles           bool
	freedomProfiles bool
	gates           bool
	hooks           bool
	guards          bool
	budgets         bool
}

// PolicyDocumentMeta is the metadata block of a policy YAML document (§6.3).
type PolicyDocumentMeta struct {
	// Name is the policy document name. Required.
	Name string `yaml:"name"`

	// Version is the human-readable policy version (semver-ish). Required.
	Version string `yaml:"version"`

	// Author is the policy author identifier. Required.
	Author string `yaml:"author"`

	// SchemaVersion is the policy schema version integer, N-1 readable per
	// §4.7.CP-038. Required and must be positive.
	SchemaVersion int `yaml:"schema_version"`
}

// PolicyRole is a role declaration from the policy YAML roles[] section (§6.3).
//
// PermissionSchema is a pointer so that a nil value signals that the
// permission_schema key was absent from the YAML source, enabling CP-028
// detection in ValidateRoles. A non-nil pointer (even with all zero fields)
// means the key was explicitly declared.
type PolicyRole struct {
	// Name is the role name per [architecture.md §4.8]. Required.
	Name string `yaml:"name"`

	// PermissionSchema carries the tool and path allowances for this role (§6.2).
	// Nil means the permission_schema key was absent from the YAML source,
	// which is a CP-028 violation detected by ValidateRoles.
	PermissionSchema *PolicyPermissionSchema `yaml:"permission_schema"`

	// Status is "mvh-required" or "declared-but-deferred" (§4.6.CP-028).
	Status string `yaml:"status"`
}

// PolicyPermissionSchema is the permission_schema block of a role (§6.2).
type PolicyPermissionSchema struct {
	AllowedTools  []string `yaml:"allowed_tools"`
	WritablePaths []string `yaml:"writable_paths"`
	ReadablePaths []string `yaml:"readable_paths,omitempty"`
	ModelTier     string   `yaml:"model_tier,omitempty"`
	DefaultSkills []string `yaml:"default_skills"`
	AllowedHooks  []string `yaml:"allowed_hooks"`
	InvocableBy   []string `yaml:"invocable_by"`
}

// PolicyFreedomProfile is a freedom_profile declaration (§6.2 RECORD FreedomProfile, §6.3).
type PolicyFreedomProfile struct {
	Name               string   `yaml:"name"`
	ToolWhitelist      []string `yaml:"tool_whitelist"`
	WritablePaths      []string `yaml:"writable_paths"`
	ModelTier          string   `yaml:"model_tier,omitempty"`
	TokenBudgetRef     string   `yaml:"token_budget_ref,omitempty"`
	WallClockBudgetRef string   `yaml:"wall_clock_budget_ref,omitempty"`
	MaxIterations      int      `yaml:"max_iterations"`
}

// PolicyEvaluatorBlock is the evaluator: sub-block in a gate/hook/guard YAML entry.
type PolicyEvaluatorBlock struct {
	Mode           string                     `yaml:"mode"`
	Expression     string                     `yaml:"expression,omitempty"`
	DelegationPath *PolicyDelegationPathBlock `yaml:"delegation_path,omitempty"`
}

// PolicyDelegationPathBlock is the delegation_path: sub-block in a cognition evaluator.
type PolicyDelegationPathBlock struct {
	Role              string `yaml:"role"`
	ModelClass        string `yaml:"model_class"`
	InputSchemaRef    string `yaml:"input_schema_ref"`
	ResponseSchemaRef string `yaml:"response_schema_ref"`
	PromptTemplateRef string `yaml:"prompt_template_ref"`
}

// PolicyGate is a gate[] entry in the policy YAML (§6.3 gates:).
type PolicyGate struct {
	Name            string               `yaml:"name"`
	Subtype         string               `yaml:"subtype"`
	AttachPoint     string               `yaml:"attach_point"`
	NamedApprover   string               `yaml:"named_approver,omitempty"`
	VerificationRef string               `yaml:"verification_ref,omitempty"`
	Evaluator       PolicyEvaluatorBlock `yaml:"evaluator"`
}

// PolicyHook is a hook[] entry in the policy YAML (§6.3 hooks:).
type PolicyHook struct {
	Name               string               `yaml:"name"`
	TriggerEvent       string               `yaml:"trigger_event"`
	SubscriptionFilter string               `yaml:"subscription_filter,omitempty"`
	SideEffectKind     string               `yaml:"side_effect_kind"`
	HaltOnFailure      bool                 `yaml:"halt_on_failure"`
	SubsystemPriority  int                  `yaml:"subsystem_priority"`
	Evaluator          PolicyEvaluatorBlock `yaml:"evaluator"`
}

// PolicyGuard is a guard[] entry in the policy YAML (§6.3 guards:).
type PolicyGuard struct {
	Name          string               `yaml:"name"`
	AppliesToNode string               `yaml:"applies_to_node,omitempty"`
	Evaluator     PolicyEvaluatorBlock `yaml:"evaluator"`
}

// PolicyBudget is a budget[] entry in the policy YAML (§6.3 budgets:).
type PolicyBudget struct {
	Name             string  `yaml:"name"`
	Resource         string  `yaml:"resource"`
	Scope            string  `yaml:"scope"`
	Limit            int64   `yaml:"limit"`
	WarningThreshold float64 `yaml:"warning_threshold"`
	ScopeTarget      string  `yaml:"scope_target"`
}

// PolicySkillSet is a skill_sets[] entry in the policy YAML (§6.3 skill_sets:).
type PolicySkillSet struct {
	Name   string   `yaml:"name"`
	Skills []string `yaml:"skills"`
}

// ErrMissingPolicySection is returned by ValidateSections when a required
// top-level YAML section is absent from the document (§4.7.CP-035).
var ErrMissingPolicySection = errors.New("policy document missing required section")

// ErrMissingPermissionSchema is returned by ValidateRoles when a role
// declaration is missing its permission_schema block (§4.6.CP-028).
var ErrMissingPermissionSchema = errors.New("role missing required permission_schema")

// ErrNonEmptyDeferredRoleShell is returned by ValidateDeferredRoleShells when
// a declared-but-deferred role carries a non-empty allowed_tools, writable_paths,
// or default_skills field (§4.6.CP-030).
var ErrNonEmptyDeferredRoleShell = errors.New("declared-but-deferred role must carry empty shell: allowed_tools, writable_paths, and default_skills must all be empty")

// requiredSections lists the seven required top-level keys per CP-035.
var requiredSections = []string{
	"metadata",
	"roles",
	"freedom_profiles",
	"gates",
	"hooks",
	"guards",
	"budgets",
}

// ParsePolicyDocument parses raw YAML bytes into a PolicyDocument and
// records which top-level sections were present.
//
// Parsing does not validate the document against CP-035; call ValidateSections
// after parsing. This separation allows test fixtures to exercise missing-section
// detection explicitly.
//
//nolint:gosec // G304: path provenance is test-fixture YAML bytes, not user input
func ParsePolicyDocument(data []byte) (PolicyDocument, error) {
	// First, extract the set of top-level keys that were present in the YAML.
	var rawMap map[string]yaml.Node
	if err := yaml.Unmarshal(data, &rawMap); err != nil {
		return PolicyDocument{}, fmt.Errorf("policy document: yaml parse: %w", err)
	}

	presence := policyDocumentSections{
		metadata:        rawMap["metadata"].Kind != 0,
		roles:           rawMap["roles"].Kind != 0,
		freedomProfiles: rawMap["freedom_profiles"].Kind != 0,
		gates:           rawMap["gates"].Kind != 0,
		hooks:           rawMap["hooks"].Kind != 0,
		guards:          rawMap["guards"].Kind != 0,
		budgets:         rawMap["budgets"].Kind != 0,
	}

	var doc PolicyDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return PolicyDocument{}, fmt.Errorf("policy document: yaml unmarshal: %w", err)
	}
	doc.sectionPresence = presence
	return doc, nil
}

// ValidateSections reports the first missing required section per CP-035, or
// nil when all seven required sections are present.
//
// A section is "present" when its top-level key appeared in the YAML source,
// even if the value is an empty list. Absence means the key was not written.
func (d *PolicyDocument) ValidateSections() error {
	missing := d.missingSections()
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrMissingPolicySection, strings.Join(missing, ", "))
}

// ValidateRoles reports the first CP-028 violation found in d.Roles, or nil
// when every role carries a permission_schema block.
//
// CP-028 requires every role declared in a policy document to carry a
// permission_schema (specs/control-points.md §4.6.CP-028). A nil
// PermissionSchema pointer means the key was absent from the YAML source.
func (d *PolicyDocument) ValidateRoles() error {
	for i, r := range d.Roles {
		if r.PermissionSchema == nil {
			name := r.Name
			if name == "" {
				name = fmt.Sprintf("roles[%d]", i)
			}
			return fmt.Errorf("%w: role %q", ErrMissingPermissionSchema, name)
		}
	}
	return nil
}

// ValidateDeferredRoleShells reports the first CP-030 violation found in
// d.Roles, or nil when all declared-but-deferred roles carry empty permission
// shells.
//
// CP-030 requires that every role with status "declared-but-deferred" carries
// a permission shell where allowed_tools, writable_paths, and default_skills
// are all empty. Activation of a deferred role requires a foundation amendment
// per §4.6; shell fields are filled at activation time, not declaration time.
func (d *PolicyDocument) ValidateDeferredRoleShells() error {
	for i, r := range d.Roles {
		if r.Status != "declared-but-deferred" {
			continue
		}
		name := r.Name
		if name == "" {
			name = fmt.Sprintf("roles[%d]", i)
		}
		ps := r.PermissionSchema
		if ps == nil {
			// CP-028 catches this; skip here to avoid double-reporting.
			continue
		}
		if len(ps.AllowedTools) > 0 {
			return fmt.Errorf("%w: role %q has non-empty allowed_tools", ErrNonEmptyDeferredRoleShell, name)
		}
		if len(ps.WritablePaths) > 0 {
			return fmt.Errorf("%w: role %q has non-empty writable_paths", ErrNonEmptyDeferredRoleShell, name)
		}
		if len(ps.DefaultSkills) > 0 {
			return fmt.Errorf("%w: role %q has non-empty default_skills", ErrNonEmptyDeferredRoleShell, name)
		}
	}
	return nil
}

// missingSections returns the names of required sections that were absent.
func (d *PolicyDocument) missingSections() []string {
	type check struct {
		name    string
		present bool
	}
	checks := []check{
		{"metadata", d.sectionPresence.metadata},
		{"roles", d.sectionPresence.roles},
		{"freedom_profiles", d.sectionPresence.freedomProfiles},
		{"gates", d.sectionPresence.gates},
		{"hooks", d.sectionPresence.hooks},
		{"guards", d.sectionPresence.guards},
		{"budgets", d.sectionPresence.budgets},
	}
	var missing []string
	for _, c := range checks {
		if !c.present {
			missing = append(missing, c.name)
		}
	}
	return missing
}

// PolicyConfig is the flattened configuration derived from the four-layer
// precedence merge defined in §4.7.CP-037.
//
// The four layers (highest precedence first):
//  1. RuntimeOverride  — runtime operator CLI flags
//  2. OperatorPolicy   — operator-policy YAML file
//  3. WorkflowDef      — per-workflow policy overrides
//  4. DefaultConfig    — harmonik built-in defaults
//
// MergeConfigs performs a deep merge with higher-precedence values replacing
// lower-precedence values on every field.
type PolicyConfig struct {
	// SchemaVersion is the effective schema version after merge.
	SchemaVersion int

	// ExtraFields carries any additional config keys not yet typed in this
	// struct. Deep-merged by higher-precedence layer winning.
	ExtraFields map[string]string
}

// MergeConfigs deep-merges four PolicyConfig layers in precedence order
// (highest first: runtime override, operator policy, workflow def, default).
//
// Merge rule per §4.7.CP-037: higher-precedence values replace lower-precedence
// values on every field. For map fields, individual keys from the higher-
// precedence layer overwrite matching keys from lower-precedence layers.
// Missing keys in a higher-precedence layer are filled from lower layers.
func MergeConfigs(runtime, operatorPolicy, workflowDef, defaultConfig PolicyConfig) PolicyConfig {
	// Build result bottom-up: start with lowest precedence, apply upward.
	result := defaultConfig

	applyLayer := func(higher PolicyConfig) {
		if higher.SchemaVersion != 0 {
			result.SchemaVersion = higher.SchemaVersion
		}
		for k, v := range higher.ExtraFields {
			if result.ExtraFields == nil {
				result.ExtraFields = make(map[string]string)
			}
			result.ExtraFields[k] = v
		}
	}

	applyLayer(workflowDef)
	applyLayer(operatorPolicy)
	applyLayer(runtime)

	return result
}
