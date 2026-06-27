package workflow

// loader.go — DOT workflow loader for workflow_mode=dot.
//
// Provides LoadDotWorkflow: reads a .dot file, parses via dot.Parse, validates
// via dot.Validate, and returns the validated graph or a typed error the daemon
// can map to failure_class=workflow_load.
//
// Provides LoadDotWorkflowWithPolicy: extends LoadDotWorkflow with skills_ref
// resolution per control-points.md §4.13 CP-057. Returns one SkillsResolvedPayload
// per node whose skills_ref resolved successfully.
//
// Spec refs:
//   - specs/workflow-graph.md §10 WG-031/032 — parse policy.
//   - specs/workflow-graph.md §9 WG-024..028 — validation obligations.
//   - specs/execution-model.md §4.3 EM-012   — WorkflowModeDot dispatch.
//   - specs/control-points.md §4.12 CP-056   — policy_ref deprecated.
//   - specs/control-points.md §4.13 CP-057   — skills_ref semantics.
//
// Bead ref: hk-waj4b (T-IMPL-004), hk-zqr6f (CP-056/CP-057 surface).
// Tags: mechanism

import (
	"fmt"
	"os"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// ErrWorkflowLoad is the typed error returned by LoadDotWorkflow when the .dot
// artifact cannot be read, parsed, or validated. The daemon maps this to
// failure_class=workflow_load when emitting run_failed events.
type ErrWorkflowLoad struct {
	// Path is the filesystem path that was attempted.
	Path string
	// Reason describes the failure (read error, parse error, validation error).
	Reason string
}

func (e *ErrWorkflowLoad) Error() string {
	return fmt.Sprintf("workflow_load: %s: %s", e.Path, e.Reason)
}

// ErrPolicyRefRejected is the typed error returned when a DOT workflow uses the
// deprecated "policy_ref" attribute (CP-056). It wraps handlercontract.ErrDeterministic
// so that errors.Is(err, handlercontract.ErrDeterministic) returns true — the spec
// mandates ErrDeterministic (not ErrWorkflowLoad) for this specific rejection.
type ErrPolicyRefRejected struct {
	// Path is the filesystem path of the workflow file.
	Path string
	// Reason contains the rejection message, naming the typed replacement attributes.
	Reason string
}

func (e *ErrPolicyRefRejected) Error() string {
	return fmt.Sprintf("workflow_load: %s: %s", e.Path, e.Reason)
}

// Unwrap returns handlercontract.ErrDeterministic so that errors.Is checks propagate.
func (e *ErrPolicyRefRejected) Unwrap() error { return handlercontract.ErrDeterministic }

// LoadDotWorkflow reads a .dot file at dotPath, parses it via dot.Parse,
// validates via dot.Validate, and returns the validated graph.
//
// On any failure (file read, parse, validation with SeverityError diagnostics),
// returns nil and an *ErrWorkflowLoad that the daemon can map to
// failure_class=workflow_load.
//
// When a policy_ref attribute is detected (deprecated per CP-056), a deprecation
// warning is printed to stderr naming the typed replacements (gate_ref,
// skills_ref, or freedom_profile_ref per CP-055) before returning the load error.
func LoadDotWorkflow(dotPath string) (*dot.Graph, error) {
	src, err := os.ReadFile(dotPath)
	if err != nil {
		return nil, &ErrWorkflowLoad{
			Path:   dotPath,
			Reason: fmt.Sprintf("read failed: %v", err),
		}
	}

	graph, parseErr := dot.Parse(string(src), dotPath)
	if parseErr != nil {
		// CP-056: policy_ref is a deterministic rejection — return ErrPolicyRefRejected
		// (which wraps handlercontract.ErrDeterministic) and print a deprecation warning
		// to stderr naming the typed replacement attributes per the spec mandate.
		if strings.Contains(parseErr.Error(), "CP-056") {
			fmt.Fprintf(os.Stderr,
				"DEPRECATION WARNING [CP-056]: workflow %q uses the deprecated \"policy_ref\" attribute. "+
					"Replace it with the typed successor: gate_ref, skills_ref, or freedom_profile_ref (CP-055).\n",
				dotPath)
			return nil, &ErrPolicyRefRejected{
				Path:   dotPath,
				Reason: fmt.Sprintf("policy_ref rejected (CP-056): use gate_ref, skills_ref, or freedom_profile_ref instead (CP-055); parse error: %v", parseErr),
			}
		}
		return nil, &ErrWorkflowLoad{
			Path:   dotPath,
			Reason: fmt.Sprintf("parse failed: %v", parseErr),
		}
	}

	diags := dot.Validate(graph)
	var errs []string
	for _, d := range diags {
		if d.Severity == dot.SeverityError {
			errs = append(errs, d.String())
		}
	}
	if len(errs) > 0 {
		return nil, &ErrWorkflowLoad{
			Path:   dotPath,
			Reason: fmt.Sprintf("validation failed: %s", strings.Join(errs, "; ")),
		}
	}

	return graph, nil
}

// LoadDotWorkflowWithParams reads a .dot file at dotPath, applies template-param
// substitution via SubstituteTemplateParams (WG-045/WG-046) before parsing, then
// parses and validates the graph.
//
// Ordering invariant (WG-046): read → substitute → parse → validate → dispatch.
//
// When params is nil or empty the call is equivalent to LoadDotWorkflow (the
// substitution pass is a byte-identical no-op when src contains no __TOKEN__ patterns).
//
// Returns *ErrWorkflowLoad on file-read, substitution, parse, or validation failure.
// The residual-token error (unresolved __TOKEN__ in src) maps to failure_class=workflow_load.
//
// Bead ref: hk-55zv2 (T5 — WG-045/WG-046).
func LoadDotWorkflowWithParams(dotPath string, params map[string]string) (*dot.Graph, error) {
	src, err := os.ReadFile(dotPath)
	if err != nil {
		return nil, &ErrWorkflowLoad{
			Path:   dotPath,
			Reason: fmt.Sprintf("read failed: %v", err),
		}
	}

	// WG-046 (security): parse the TEMPLATE with __TOKEN__ placeholders intact,
	// then substitute per-attribute AFTER parse (substituteGraphParams below). The
	// DOT lexer preserves __TOKEN__ verbatim inside quoted attribute values, so a
	// param value can never alter graph shape (DOT-structure injection is closed by
	// construction) and tool_command values can be context-aware shell-quoted.
	graph, parseErr := dot.Parse(string(src), dotPath)
	if parseErr != nil {
		if strings.Contains(parseErr.Error(), "CP-056") {
			fmt.Fprintf(os.Stderr,
				"DEPRECATION WARNING [CP-056]: workflow %q uses the deprecated \"policy_ref\" attribute. "+
					"Replace it with the typed successor: gate_ref, skills_ref, or freedom_profile_ref (CP-055).\n",
				dotPath)
			return nil, &ErrPolicyRefRejected{
				Path:   dotPath,
				Reason: fmt.Sprintf("policy_ref rejected (CP-056): use gate_ref, skills_ref, or freedom_profile_ref instead (CP-055); parse error: %v", parseErr),
			}
		}
		return nil, &ErrWorkflowLoad{
			Path:   dotPath,
			Reason: fmt.Sprintf("parse failed: %v", parseErr),
		}
	}

	// WG-045 (security): substitute params into the parsed graph per-attribute —
	// tool_command values shell-quoted, all others verbatim — applying ingestion
	// hygiene + the residual-token launch check. Runs BEFORE Validate so typed-field
	// checks see substituted values.
	if subErr := substituteGraphParams(graph, params); subErr != nil {
		return nil, &ErrWorkflowLoad{
			Path:   dotPath,
			Reason: fmt.Sprintf("template substitution failed: %v", subErr),
		}
	}

	diags := dot.Validate(graph)
	var errs []string
	for _, d := range diags {
		if d.Severity == dot.SeverityError {
			errs = append(errs, d.String())
		}
	}
	if len(errs) > 0 {
		return nil, &ErrWorkflowLoad{
			Path:   dotPath,
			Reason: fmt.Sprintf("validation failed: %s", strings.Join(errs, "; ")),
		}
	}

	return graph, nil
}

// LoadDotWorkflowFromBytes parses and validates a .dot workflow from an in-memory
// byte slice. sourceName is used only for error messages (e.g. "embedded:standard-bead.dot").
// When params is nil or empty, template substitution is a no-op.
//
// This is the load path for the embedded standard-bead.dot fallback
// (hk-30vlb): same pipeline as LoadDotWorkflowWithParams but without the
// os.ReadFile call.
func LoadDotWorkflowFromBytes(src []byte, sourceName string, params map[string]string) (*dot.Graph, error) {
	// WG-046 (security): parse the TEMPLATE with __TOKEN__ placeholders intact,
	// then substitute per-attribute AFTER parse (substituteGraphParams below).
	graph, parseErr := dot.Parse(string(src), sourceName)
	if parseErr != nil {
		if strings.Contains(parseErr.Error(), "CP-056") {
			fmt.Fprintf(os.Stderr,
				"DEPRECATION WARNING [CP-056]: workflow %q uses the deprecated \"policy_ref\" attribute. "+
					"Replace it with the typed successor: gate_ref, skills_ref, or freedom_profile_ref (CP-055).\n",
				sourceName)
			return nil, &ErrPolicyRefRejected{
				Path:   sourceName,
				Reason: fmt.Sprintf("policy_ref rejected (CP-056): use gate_ref, skills_ref, or freedom_profile_ref instead (CP-055); parse error: %v", parseErr),
			}
		}
		return nil, &ErrWorkflowLoad{
			Path:   sourceName,
			Reason: fmt.Sprintf("parse failed: %v", parseErr),
		}
	}

	// WG-045 (security): substitute params into the parsed graph per-attribute —
	// tool_command values shell-quoted, all others verbatim — applying ingestion
	// hygiene + the residual-token launch check. Runs BEFORE Validate.
	if subErr := substituteGraphParams(graph, params); subErr != nil {
		return nil, &ErrWorkflowLoad{
			Path:   sourceName,
			Reason: fmt.Sprintf("template substitution failed: %v", subErr),
		}
	}

	diags := dot.Validate(graph)
	var errs []string
	for _, d := range diags {
		if d.Severity == dot.SeverityError {
			errs = append(errs, d.String())
		}
	}
	if len(errs) > 0 {
		return nil, &ErrWorkflowLoad{
			Path:   sourceName,
			Reason: fmt.Sprintf("validation failed: %s", strings.Join(errs, "; ")),
		}
	}

	return graph, nil
}

// LoadDotWorkflowWithPolicy extends LoadDotWorkflow with skills_ref resolution
// per control-points.md §4.13 CP-057.
//
// After successfully loading and validating the graph, it iterates every node
// that declares a skills_ref attribute and resolves the name against the
// policy's skill_sets[] block. Successful resolution yields one
// core.SkillsResolvedPayload per node; an unresolved skills_ref is returned as
// an *ErrWorkflowLoad (structural failure — the same class as a missing *_ref).
//
// skills_ref is OPTIONAL on every node type per CP-057; nodes without skills_ref
// are silently accepted and produce no payload entry.
func LoadDotWorkflowWithPolicy(dotPath string, policy *core.PolicyDocument) (*dot.Graph, []core.SkillsResolvedPayload, error) {
	graph, err := LoadDotWorkflow(dotPath)
	if err != nil {
		return nil, nil, err
	}

	// Build a name→skills index from the policy's skill_sets block (CP-057 §6.3).
	skillSetIndex := make(map[string][]string, len(policy.SkillSets))
	for _, ss := range policy.SkillSets {
		skillSetIndex[ss.Name] = ss.Skills
	}

	var resolved []core.SkillsResolvedPayload
	for _, n := range graph.Nodes {
		ref := strings.TrimSpace(n.SkillsRef)
		if ref == "" {
			continue
		}
		skills, ok := skillSetIndex[ref]
		if !ok {
			return nil, nil, &ErrWorkflowLoad{
				Path: dotPath,
				Reason: fmt.Sprintf(
					"node %q: skills_ref %q does not resolve to any skill_sets[] entry in the policy (CP-057 / WG-026)",
					n.ID, ref),
			}
		}
		resolved = append(resolved, core.SkillsResolvedPayload{
			NodeID:    n.ID,
			SkillsRef: ref,
			Skills:    skills,
		})
	}

	return graph, resolved, nil
}
