package core

// hooktrigger_hka8bg12.go — Hook lifecycle event type registry (CP-013).
//
// Implements specs/control-points.md §4.3.CP-013:
//
//	A Hook's trigger MUST match one of the declared lifecycle event types.
//	Hook-trigger names form a separate Hook-namespace (the "on_" prefix
//	distinguishes them from raw event-type names). The MVH-baseline trigger
//	set is registered at daemon init; subsystems MAY declare additional
//	triggers via the subsystem envelope (architecture.md §4.4). An
//	unrecognized trigger fails registration.
//
// # Design
//
// HookTriggerSet is the in-daemon registry of declared trigger names. It is
// populated at daemon init via NewBaselineHookTriggerSet (baseline 8) followed
// by zero or more AddTrigger calls from subsystem envelopes. S02Registrar holds
// a HookTriggerSet and validates each Hook's TriggerEvent against it during
// construction (constructHook).
//
// Refs: hk-a8bg.12

import "fmt"

// HookTrigger is the canonical name for a Hook subscription trigger.
// All trigger names carry the "on_" prefix that distinguishes Hook-subscription
// names from raw event-type names per CP-013.
type HookTrigger string

// MVH-baseline Hook trigger set per specs/control-points.md §4.3.CP-013.
// These 8 triggers are always registered at daemon init.
const (
	HookTriggerOnAgentStarted        HookTrigger = "on_agent_started"
	HookTriggerOnAgentOutput         HookTrigger = "on_agent_output"
	HookTriggerOnAgentCompleted      HookTrigger = "on_agent_completed"
	HookTriggerOnTimeout             HookTrigger = "on_timeout"
	HookTriggerOnReviewRequired      HookTrigger = "on_review_required"
	HookTriggerOnTransitionAttempted HookTrigger = "on_transition_attempted"
	HookTriggerOnCheckpointWritten   HookTrigger = "on_checkpoint_written"
	HookTriggerOnCheckpointFailed    HookTrigger = "on_checkpoint_failed"
)

// baselineHookTriggers is the ordered list of MVH-baseline trigger names.
var baselineHookTriggers = [...]HookTrigger{
	HookTriggerOnAgentStarted,
	HookTriggerOnAgentOutput,
	HookTriggerOnAgentCompleted,
	HookTriggerOnTimeout,
	HookTriggerOnReviewRequired,
	HookTriggerOnTransitionAttempted,
	HookTriggerOnCheckpointWritten,
	HookTriggerOnCheckpointFailed,
}

// HookTriggerSet is the in-daemon registry of declared Hook trigger names.
//
// Populated at daemon init with the 8 MVH-baseline triggers via
// NewBaselineHookTriggerSet; subsystem envelopes may call AddTrigger before
// the daemon's main loop starts to register subsystem-specific triggers (per
// architecture.md §4.4 and CP-013).
//
// Not safe for concurrent use after init; treat as read-only once the daemon
// loop begins.
//
// Tags: mechanism
type HookTriggerSet struct {
	triggers map[HookTrigger]struct{}
}

// NewBaselineHookTriggerSet returns a HookTriggerSet pre-populated with the 8
// MVH-baseline Hook trigger names declared in CP-013.
//
// Callers that need to extend the set with subsystem-specific triggers MUST
// call AddTrigger before any PolicyDocument is registered via S02Registrar.
func NewBaselineHookTriggerSet() *HookTriggerSet {
	ts := &HookTriggerSet{
		triggers: make(map[HookTrigger]struct{}, len(baselineHookTriggers)),
	}
	for _, t := range baselineHookTriggers {
		ts.triggers[t] = struct{}{}
	}
	return ts
}

// Contains reports whether trigger is a declared Hook trigger name in this set.
func (ts *HookTriggerSet) Contains(trigger string) bool {
	_, ok := ts.triggers[HookTrigger(trigger)]
	return ok
}

// AddTrigger registers an additional Hook trigger name in the set.
//
// Idempotent: registering an already-declared trigger name is a no-op.
// Returns an error when trigger is empty.
//
// Subsystems MUST call AddTrigger during daemon init (before any PolicyDocument
// is registered) so that Hook ControlPoints that reference the subsystem trigger
// can pass construction validation per CP-013.
func (ts *HookTriggerSet) AddTrigger(trigger string) error {
	if trigger == "" {
		return fmt.Errorf("hooktrigger: trigger name must be non-empty")
	}
	ts.triggers[HookTrigger(trigger)] = struct{}{}
	return nil
}

// All returns a sorted slice of every declared trigger name in this set.
//
// The returned slice is a copy; modifications do not affect the set.
func (ts *HookTriggerSet) All() []string {
	out := make([]string, 0, len(ts.triggers))
	for t := range ts.triggers {
		out = append(out, string(t))
	}
	// Sort for determinism (CP-046 spirit: observable output is stable).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
