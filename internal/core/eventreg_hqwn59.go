package core

// eventreg_hqwn59.go — startup-time registration of §8.1.* and §8.2.* payload
// types into the global event registry per EV-032 / EV-034.
//
// Spec ref: specs/event-model.md §6.3 EV-032, §4.9 EV-034.
//
// Each helper function called from init() registers one section's worth of §8
// event types so the global registry is populated before any event is emitted
// (EV-034). The registry is sealed at bus-seal time per EV-009.
//
// Tags: mechanism
// Durability classes per §8 table: F = fsync-boundary, O = ordinary.
//
// §8.1 Run lifecycle event registrations are in registerRunLifecycle().
// §8.2 Control-point lifecycle event registrations are in registerControlPoints().
//
// Bead refs: hk-hqwn.59.1 through hk-hqwn.59.20.

func init() {
	registerRunLifecycle()
	registerControlPoints()
}

// registerRunLifecycle registers all §8.1 run-lifecycle event payload constructors.
//
// Durability classes per §8.1 table:
//   - run_started (§8.1.1):             F (fsync-boundary per beads-integration.md §4.3 BI-009)
//   - run_completed (§8.1.2):           F (terminal-state fsync per BI-010)
//   - run_failed (§8.1.3):              F (terminal-state fsync per BI-010)
//   - state_entered (§8.1.4):           O (ordinary — observability stream)
//   - state_exited (§8.1.5):            O (ordinary — observability stream)
//   - transition_event (§8.1.6):        F (fsync-boundary per checkpoint write)
//   - checkpoint_written (§8.1.7):      F (fsync-boundary per checkpoint write)
//   - outcome_emitted (§8.1.8):         O (ordinary — handler-to-daemon pipe)
//   - sub_workflow_entered (§8.1.9):    O (ordinary — observability stream)
//   - sub_workflow_exited (§8.1.10):    O (ordinary — observability stream)
//   - node_dispatch_requested (§8.1.11): O (ordinary — observability stream)
func registerRunLifecycle() {
	mustRegister("run_started", func() EventPayload { return &RunStartedPayload{} })
	mustRegister("run_completed", func() EventPayload { return &RunCompletedPayload{} })
	mustRegister("run_failed", func() EventPayload { return &RunFailedPayload{} })
	mustRegister("state_entered", func() EventPayload { return &StateEnteredPayload{} })
	mustRegister("state_exited", func() EventPayload { return &StateExitedPayload{} })
	mustRegister("transition_event", func() EventPayload { return &TransitionEventPayload{} })
	mustRegister("checkpoint_written", func() EventPayload { return &CheckpointWrittenPayload{} })
	mustRegister("outcome_emitted", func() EventPayload { return &OutcomeEmittedPayload{} })
	mustRegister("sub_workflow_entered", func() EventPayload { return &SubWorkflowEnteredPayload{} })
	mustRegister("sub_workflow_exited", func() EventPayload { return &SubWorkflowExitedPayload{} })
	mustRegister("node_dispatch_requested", func() EventPayload { return &NodeDispatchRequestedPayload{} })
}

// registerControlPoints registers all §8.2 control-point-lifecycle event payload constructors.
//
// Durability classes per §8.2 table:
//   - hook_fired (§8.2.1):                          O (ordinary — observability)
//   - hook_failed (§8.2.2):                         O (ordinary — observability)
//   - hook_verdict_persisted (§8.2.3):              O (ordinary — observability)
//   - gate_allowed (§8.2.4):                        O (ordinary — observability)
//   - gate_denied (§8.2.5):                         O (ordinary — observability)
//   - gate_escalated (§8.2.6):                      O (ordinary — observability)
//   - guard_reordered (§8.2.7):                     O (ordinary — observability)
//   - guard_failed (§8.2.8):                        O (ordinary — observability)
//   - control_points_registered (§8.2.9):           O (ordinary — observability)
//   - control_points_registration_started (§8.2.10): O (ordinary — observability)
//   - verdict_envelope_mismatch (§8.2.11):          O (ordinary — reconciliation input)
//   - policy_expression_exceeded_cost (§8.2.12):    F (fsync-boundary per CP-034b durability-pair)
func registerControlPoints() {
	mustRegister("hook_fired", func() EventPayload { return &HookFiredPayload{} })
	mustRegister("hook_failed", func() EventPayload { return &HookFailedPayload{} })
	mustRegister("hook_verdict_persisted", func() EventPayload { return &HookVerdictPersistedPayload{} })
	mustRegister("gate_allowed", func() EventPayload { return &GateAllowedPayload{} })
	mustRegister("gate_denied", func() EventPayload { return &GateDeniedPayload{} })
	mustRegister("gate_escalated", func() EventPayload { return &GateEscalatedPayload{} })
	mustRegister("guard_reordered", func() EventPayload { return &GuardReorderedPayload{} })
	mustRegister("guard_failed", func() EventPayload { return &GuardFailedPayload{} })
	mustRegister("control_points_registered", func() EventPayload { return &ControlPointsRegisteredPayload{} })
	mustRegister("control_points_registration_started", func() EventPayload { return &ControlPointsRegistrationStartedPayload{} })
	mustRegister("verdict_envelope_mismatch", func() EventPayload { return &VerdictEnvelopeMismatchPayload{} })
	mustRegister("policy_expression_exceeded_cost", func() EventPayload { return &PolicyExpressionExceededCostPayload{} })
}

// mustRegister calls RegisterEventType and panics on error.
//
// init() functions that call mustRegister run before the first event is emitted
// (EV-034); a registration error is a programming error (duplicate type name or
// nil constructor) that MUST be caught at startup, not silently ignored. Panic
// is acceptable here because init() runs before any request is served.
//
// This helper is intentionally unexported and limited to init() callers; it
// MUST NOT be called after startup completes.
func mustRegister(typeName string, ctor func() EventPayload) {
	if err := RegisterEventType(typeName, ctor); err != nil {
		panic("core: mustRegister: " + typeName + ": " + err.Error())
	}
}
