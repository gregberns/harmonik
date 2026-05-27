package core

// s02registrar_hka8bg45.go — S02 (Policy Engine) ownership of the ControlPoint registry.
//
// Implements specs/control-points.md §4.9.CP-043:
//
//	The ControlPoint registry MUST be a single in-process table (Go map keyed by
//	`name`) owned by S02 (Policy Engine). S02 constructs ControlPoint instances by
//	reading policy YAML per §4.7 and calling the registration surface defined in
//	§4.9.CP-044. The registry is the single source of truth for registered
//	ControlPoints within a daemon.
//
// # S02 ownership model
//
// S02Registrar is the S02-owned component that:
//  1. Holds the single MapRegistry (the in-process table per CP-043).
//  2. Constructs ControlPoint instances from PolicyDocument entries (per §7.1).
//  3. Registers each constructed ControlPoint via Registry.Register (per CP-044).
//
// The construction step is PURE per §7.1: no event emission, no durable writes,
// no dispatch, no Hook firing, no audit-record side-effect during construction.
// All side-effects (the paired control_points_registration_started /
// control_points_registered events) are emitted by the caller around the batch.
//
// # Registration sequence
//
// The §7.1 two-pass sequence drives registration from the outside:
//
//  1. Load and validate all PolicyDocument instances (Pass 1 — caller).
//  2. Call RegisterFromDocument on each document (Pass 2 — this type).
//
// RegisterFromDocument is re-registration-safe per CP-044: idempotent on
// identical body, error on divergent body.
//
// Tags: mechanism
//
// Refs: hk-a8bg.45

import (
	"errors"
	"fmt"
	"strings"
)

// ErrConstructControlPoint is the sentinel error for failures in the
// ControlPoint construction step. Errors returned by the construct* helpers
// wrap this sentinel so callers can distinguish construction errors (bad YAML
// semantics) from registration errors (duplicate name, divergent body).
var ErrConstructControlPoint = errors.New("s02: ControlPoint construction failed")

// S02Registrar is the S02 (Policy Engine) component that owns the ControlPoint
// registry and drives construction of ControlPoint instances from policy YAML.
//
// Callers must use NewS02Registrar to construct an S02Registrar. The zero
// value is invalid (nil registry map causes a panic on first access).
//
// Concurrency: the same concurrency guarantee as MapRegistry applies — safe
// for concurrent reads after startup population; not safe for concurrent
// writes. The daemon populates once at startup and treats the registry as
// read-only thereafter.
//
// Tags: mechanism
type S02Registrar struct {
	reg *MapRegistry
}

// NewS02Registrar returns a new S02Registrar with an empty MapRegistry.
//
// The S02Registrar is ready for calls to RegisterFromDocument. After all
// documents are registered, call Registry() to obtain the read-only Registry
// interface that S01 and S05 consult.
func NewS02Registrar() *S02Registrar {
	return &S02Registrar{reg: NewMapRegistry()}
}

// Registry returns the Registry interface backed by S02Registrar's MapRegistry.
//
// The returned Registry is suitable for passing to S01 (Gate/Guard invocation)
// and S05 (Hook dispatch loop); neither owner may register additional
// ControlPoints — registration is exclusively S02's responsibility per CP-047.
func (s *S02Registrar) Registry() Registry {
	return s.reg
}

// RegisterFromDocument constructs ControlPoint instances from every entry in
// doc (gates, hooks, guards, budgets) and registers each via Registry.Register.
//
// The document's schema version (doc.Metadata.SchemaVersion) is propagated to
// every constructed ControlPoint per §4.7.CP-038.
//
// Returns nil when all entries are registered successfully. Returns an error
// wrapping [ErrDivergentBody] when any entry conflicts with an already-
// registered ControlPoint under the same name (CP-044). Returns an error
// wrapping [ErrConstructControlPoint] when any entry cannot be constructed
// from the YAML semantics (invalid attach_point, unknown evaluator mode, etc.).
//
// Re-registration with an identical body succeeds silently per CP-044.
// Structural invalidity (cp.Valid() == false) returns [ErrInvalidControlPoint]
// from the underlying MapRegistry.Register call.
func (s *S02Registrar) RegisterFromDocument(doc PolicyDocument) error {
	sv := doc.Metadata.SchemaVersion

	for _, pg := range doc.Gates {
		cp, err := constructGate(pg, sv)
		if err != nil {
			return err
		}
		if err := s.reg.Register(cp); err != nil {
			return fmt.Errorf("s02: register gate %q: %w", pg.Name, err)
		}
	}

	for _, ph := range doc.Hooks {
		cp, err := constructHook(ph, sv)
		if err != nil {
			return err
		}
		if err := s.reg.Register(cp); err != nil {
			return fmt.Errorf("s02: register hook %q: %w", ph.Name, err)
		}
	}

	for _, pg := range doc.Guards {
		cp, err := constructGuard(pg, sv)
		if err != nil {
			return err
		}
		if err := s.reg.Register(cp); err != nil {
			return fmt.Errorf("s02: register guard %q: %w", pg.Name, err)
		}
	}

	for _, pb := range doc.Budgets {
		cp, err := constructBudget(pb, sv)
		if err != nil {
			return err
		}
		if err := s.reg.Register(cp); err != nil {
			return fmt.Errorf("s02: register budget %q: %w", pb.Name, err)
		}
	}

	return nil
}

// constructGate constructs a Gate ControlPoint from the policy YAML entry pg
// and the document's schemaVersion.
//
// Mapping per §7.1:
//   - Kind   = KindGate
//   - Trigger = Trigger{Name: pg.AttachPoint} (attach-point encodes the fire point)
//   - OutcomeAction = OutcomeActionAllow (primary action for a Gate; §4.2)
//   - Payload.Gate  = {Subtype, AttachPoint, NamedApprover, VerificationRef}
//
// construction is PURE: no I/O, no side effects.
func constructGate(pg PolicyGate, schemaVersion int) (ControlPoint, error) {
	eval, err := constructEvaluator(pg.Evaluator, fmt.Sprintf("gate %q", pg.Name))
	if err != nil {
		return ControlPoint{}, err
	}

	attachPoint := AttachPoint(pg.AttachPoint)
	if !attachPoint.Valid() {
		return ControlPoint{}, fmt.Errorf("%w: gate %q: invalid attach_point %q",
			ErrConstructControlPoint, pg.Name, pg.AttachPoint)
	}

	subtype := GateSubtype(pg.Subtype)
	if !subtype.Valid() {
		return ControlPoint{}, fmt.Errorf("%w: gate %q: invalid subtype %q",
			ErrConstructControlPoint, pg.Name, pg.Subtype)
	}

	gatePL := &GatePayload{
		Subtype:     subtype,
		AttachPoint: attachPoint,
	}
	if pg.NamedApprover != "" {
		a := pg.NamedApprover
		gatePL.NamedApprover = &a
	}
	if pg.VerificationRef != "" {
		v := pg.VerificationRef
		gatePL.VerificationRef = &v
	}

	return ControlPoint{
		Name:          pg.Name,
		Kind:          KindGate,
		Trigger:       Trigger{Name: pg.AttachPoint},
		Evaluator:     eval,
		OutcomeAction: OutcomeActionAllow,
		Payload:       KindPayload{Gate: gatePL},
		Axes:          BaselineAxisTags,
		ModeTag:       eval.Mode,
		SchemaVersion: schemaVersion,
	}, nil
}

// constructHook constructs a Hook ControlPoint from the policy YAML entry ph
// and the document's schemaVersion.
//
// Mapping per §7.1:
//   - Kind          = KindHook
//   - Trigger       = Trigger{Name: ph.TriggerEvent}
//   - OutcomeAction = OutcomeActionSideEffect (§4.3: Hooks always produce a side-effect)
//   - Payload.Hook  = {TriggerEvent, SubscriptionFilter, SideEffectKind, HaltOnFailure, SubsystemPriority}
//
// construction is PURE: no I/O, no side effects.
func constructHook(ph PolicyHook, schemaVersion int) (ControlPoint, error) {
	eval, err := constructEvaluator(ph.Evaluator, fmt.Sprintf("hook %q", ph.Name))
	if err != nil {
		return ControlPoint{}, err
	}

	sideEffectKind := SideEffectKind(ph.SideEffectKind)
	if !sideEffectKind.Valid() {
		return ControlPoint{}, fmt.Errorf("%w: hook %q: invalid side_effect_kind %q",
			ErrConstructControlPoint, ph.Name, ph.SideEffectKind)
	}

	hookPL := &HookPayload{
		TriggerEvent:      ph.TriggerEvent,
		SideEffectKind:    sideEffectKind,
		HaltOnFailure:     ph.HaltOnFailure,
		SubsystemPriority: ph.SubsystemPriority,
	}
	if ph.SubscriptionFilter != "" {
		sf := PolicyExpression(ph.SubscriptionFilter)
		hookPL.SubscriptionFilter = &sf
	}

	return ControlPoint{
		Name:          ph.Name,
		Kind:          KindHook,
		Trigger:       Trigger{Name: ph.TriggerEvent},
		Evaluator:     eval,
		OutcomeAction: OutcomeActionSideEffect,
		Payload:       KindPayload{Hook: hookPL},
		Axes:          BaselineAxisTags,
		ModeTag:       eval.Mode,
		SchemaVersion: schemaVersion,
	}, nil
}

// constructGuard constructs a Guard ControlPoint from the policy YAML entry pg
// and the document's schemaVersion.
//
// Mapping per §7.1:
//   - Kind          = KindGuard
//   - Trigger       = Trigger{Name: ""} (Guard trigger is implicit — §4.4)
//   - OutcomeAction = OutcomeActionReorder (§4.4: Guard may only reorder)
//   - Payload.Guard = {AppliesToNode}
//
// construction is PURE: no I/O, no side effects.
func constructGuard(pg PolicyGuard, schemaVersion int) (ControlPoint, error) {
	eval, err := constructEvaluator(pg.Evaluator, fmt.Sprintf("guard %q", pg.Name))
	if err != nil {
		return ControlPoint{}, err
	}

	guardPL := &GuardPayload{}
	if pg.AppliesToNode != "" {
		nid := NodeID(pg.AppliesToNode)
		guardPL.AppliesToNode = &nid
	}

	return ControlPoint{
		Name:          pg.Name,
		Kind:          KindGuard,
		Trigger:       Trigger{Name: ""},
		Evaluator:     eval,
		OutcomeAction: OutcomeActionReorder,
		Payload:       KindPayload{Guard: guardPL},
		Axes:          BaselineAxisTags,
		ModeTag:       eval.Mode,
		SchemaVersion: schemaVersion,
	}, nil
}

// constructBudget constructs a Budget ControlPoint from the policy YAML entry pb
// and the document's schemaVersion.
//
// Mapping per §7.1:
//   - Kind          = KindBudget
//   - Trigger       = Trigger{Name: "dispatch"} (Budget fires on dispatch + per-chunk accrual)
//   - OutcomeAction = OutcomeActionAdmit (§4.5: primary outcome is admission within limit)
//   - Payload.Budget = {Resource, Scope, Limit, WarningThreshold, ScopeTarget}
//
// Budget evaluators are mechanism-tagged (deterministic threshold check per
// §4.5.CP-022). The synthetic canonical expression is constructed from the
// budget's limit value; full expression customisation is a post-MVH concern.
//
// construction is PURE: no I/O, no side effects.
func constructBudget(pb PolicyBudget, schemaVersion int) (ControlPoint, error) {
	resource := BudgetResource(pb.Resource)
	if !resource.Valid() {
		return ControlPoint{}, fmt.Errorf("%w: budget %q: invalid resource %q",
			ErrConstructControlPoint, pb.Name, pb.Resource)
	}

	scope := BudgetScope(pb.Scope)
	if !scope.Valid() {
		return ControlPoint{}, fmt.Errorf("%w: budget %q: invalid scope %q",
			ErrConstructControlPoint, pb.Name, pb.Scope)
	}

	if pb.Limit < 1 {
		return ControlPoint{}, fmt.Errorf("%w: budget %q: limit must be positive, got %d",
			ErrConstructControlPoint, pb.Name, pb.Limit)
	}

	st, err := parseScopeTarget(pb.ScopeTarget)
	if err != nil {
		return ControlPoint{}, fmt.Errorf("%w: budget %q: %v",
			ErrConstructControlPoint, pb.Name, err)
	}

	warningThreshold := pb.WarningThreshold
	if warningThreshold == 0 {
		// Apply CP-022 default of 0.8 when not explicitly set in YAML.
		warningThreshold = 0.8
	}

	budgetPL := &BudgetPayload{
		Resource:         resource,
		Scope:            scope,
		Limit:            pb.Limit,
		WarningThreshold: warningThreshold,
		ScopeTarget:      st,
	}

	// Budget evaluators are always mechanism-tagged (deterministic threshold
	// check per §4.5.CP-022). The canonical expression encodes the budget limit
	// check; the evaluator uses this at dispatch time.
	//
	// TODO(hk-a8bg): replace with typed BudgetExpression once the full CP
	// mechanism-evaluator surface (§6.4) is implemented post-MVH.
	expr := PolicyExpression(fmt.Sprintf("accrual <= %d", pb.Limit))

	return ControlPoint{
		Name:          pb.Name,
		Kind:          KindBudget,
		Trigger:       Trigger{Name: "dispatch"},
		Evaluator:     Evaluator{Mode: ModeTagMechanism, Expression: &expr},
		OutcomeAction: OutcomeActionAdmit,
		Payload:       KindPayload{Budget: budgetPL},
		Axes:          BaselineAxisTags,
		ModeTag:       ModeTagMechanism,
		SchemaVersion: schemaVersion,
	}, nil
}

// constructEvaluator builds an Evaluator from a PolicyEvaluatorBlock.
//
// Supported modes:
//   - "mechanism": Expression must be non-empty; DelegationPath must be absent.
//   - "cognition":  DelegationPath must be fully populated; Expression must be absent.
//
// cpLabel is used only in error messages (e.g., "gate \"deploy-gate\"").
func constructEvaluator(block PolicyEvaluatorBlock, cpLabel string) (Evaluator, error) {
	switch ModeTag(block.Mode) {
	case ModeTagMechanism:
		if block.Expression == "" {
			return Evaluator{}, fmt.Errorf("%w: %s: mechanism evaluator requires non-empty expression",
				ErrConstructControlPoint, cpLabel)
		}
		if block.DelegationPath != nil {
			return Evaluator{}, fmt.Errorf("%w: %s: mechanism evaluator must not carry delegation_path",
				ErrConstructControlPoint, cpLabel)
		}
		expr := PolicyExpression(block.Expression)
		return Evaluator{Mode: ModeTagMechanism, Expression: &expr}, nil

	case ModeTagCognition:
		if block.DelegationPath == nil {
			return Evaluator{}, fmt.Errorf("%w: %s: cognition evaluator requires delegation_path",
				ErrConstructControlPoint, cpLabel)
		}
		dp := &DelegationPath{
			Role:              block.DelegationPath.Role,
			ModelClass:        block.DelegationPath.ModelClass,
			InputSchemaRef:    block.DelegationPath.InputSchemaRef,
			ResponseSchemaRef: block.DelegationPath.ResponseSchemaRef,
			PromptTemplateRef: block.DelegationPath.PromptTemplateRef,
		}
		if !dp.Valid() {
			return Evaluator{}, fmt.Errorf("%w: %s: cognition delegation_path has empty required field",
				ErrConstructControlPoint, cpLabel)
		}
		if block.Expression != "" {
			return Evaluator{}, fmt.Errorf("%w: %s: cognition evaluator must not carry expression",
				ErrConstructControlPoint, cpLabel)
		}
		return Evaluator{Mode: ModeTagCognition, DelegationPath: dp}, nil

	default:
		return Evaluator{}, fmt.Errorf("%w: %s: unknown evaluator mode %q (must be mechanism or cognition)",
			ErrConstructControlPoint, cpLabel, block.Mode)
	}
}

// parseScopeTarget converts a raw scope_target string from policy YAML into a
// typed ScopeTarget value.
//
// Accepted forms (per specs/control-points.md §6.1.4):
//   - "*"                → wildcard (ScopeTargetWildcard)
//   - "node_type:<type>" → predicate (ScopeTargetPredicate)
//   - "<single-id>"      → singleton (ScopeTargetSingleton)
//
// An empty string is treated as wildcard ("*") for leniency at construction
// time; callers may reject this via BudgetPayload.Valid() if required.
func parseScopeTarget(raw string) (ScopeTarget, error) {
	switch {
	case raw == "" || raw == "*":
		return ScopeTargetWildcard(), nil

	case strings.HasPrefix(raw, "node_type:"):
		nodeType := strings.TrimPrefix(raw, "node_type:")
		return ScopeTargetPredicate(nodeType)

	default:
		return ScopeTargetSingleton(raw)
	}
}
