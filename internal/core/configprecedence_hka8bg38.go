package core

// configprecedence_hka8bg38.go — CP-037: Policy-source precedence and
// no-mid-run-reload invariant.
//
// Implements the config-loading chain declared in specs/control-points.md
// §4.7.CP-037:
//
//	Policy-source precedence is (highest first):
//	(1) runtime override set by operator before launch
//	(2) operator-policy file (persistent operator preferences)
//	(3) workflow definition (per-workflow overrides)
//	(4) default configuration shipped with harmonik
//
//	Resolution is deep-merge with higher-precedence values replacing
//	lower-precedence. A change to a higher-precedence layer takes effect on
//	the next operator pause per [operator-nfr.md §4.3] — there are NO
//	mid-run policy reloads.
//
// The "no mid-run policy reloads" constraint is enforced structurally:
// ResolvePolicyConfig is called ONCE at daemon startup and produces a frozen
// PolicyConfig. The daemon passes that resolved snapshot to every run without
// re-calling ResolvePolicyConfig. Any mid-run config read sees the same frozen
// snapshot; the between-task invariant of [operator-nfr.md §4.3] governs when
// a new call to ResolvePolicyConfig is permitted (after all in-flight runs
// complete, at the next operator pause boundary).
//
// Tags: mechanism
//
// Refs: hk-a8bg.38

// PolicyConfigLayers groups the four policy-source layers defined in
// specs/control-points.md §4.7.CP-037. The layers are ordered by precedence
// (highest first); ResolvePolicyConfig performs the deep-merge.
//
// Callers populate each field from its canonical source:
//   - RuntimeOverride: operator CLI flags / env overrides set before launch
//   - OperatorPolicy:  operator-policy YAML file (persistent operator prefs)
//   - WorkflowDef:     per-workflow policy overrides from the workflow definition
//   - DefaultConfig:   harmonik's built-in defaults (lowest precedence)
//
// A zero-value layer is treated as "no values set" and is outweighed by any
// non-zero value from a higher-precedence layer. All four fields may be
// zero-value simultaneously; ResolvePolicyConfig handles that gracefully.
type PolicyConfigLayers struct {
	// RuntimeOverride is the highest-precedence layer. Set by the operator via
	// CLI flags or environment variables immediately before daemon launch.
	// Values here always win over every other layer.
	RuntimeOverride PolicyConfig

	// OperatorPolicy is the operator-policy file layer. Loaded from the
	// operator-policy YAML at daemon startup; supersedes WorkflowDef and
	// DefaultConfig but is overridden by RuntimeOverride.
	OperatorPolicy PolicyConfig

	// WorkflowDef is the per-workflow override layer. Loaded from the workflow
	// definition at registration time; supersedes DefaultConfig but is
	// overridden by OperatorPolicy and RuntimeOverride.
	WorkflowDef PolicyConfig

	// DefaultConfig is the lowest-precedence layer. The built-in defaults
	// shipped with harmonik; active when no higher-precedence layer sets a
	// given knob.
	DefaultConfig PolicyConfig
}

// ResolvePolicyConfig deep-merges the four policy-source layers into a single
// frozen PolicyConfig per specs/control-points.md §4.7.CP-037.
//
// Resolution uses MergeConfigs with higher-precedence values replacing
// lower-precedence values on every field. The merge is deterministic and
// idempotent: given the same four layers, ResolvePolicyConfig always produces
// the same result.
//
// # No-mid-run-reload invariant (CP-037)
//
// The returned PolicyConfig MUST be treated as immutable for the lifetime of
// every in-flight run. Callers MUST invoke ResolvePolicyConfig exactly once at
// daemon startup — before the daemon's main dispatch loop begins — and pass the
// resulting snapshot to all runs.
//
// Mid-run calls to ResolvePolicyConfig violate the no-mid-run-reload invariant
// of CP-037 and the between-task invariant of [operator-nfr.md §4.3]. A
// config-layer change takes effect only at the NEXT operator pause boundary
// (after all in-flight runs have reached a terminal or pause-checkpoint state).
//
// Spec ref: specs/control-points.md §4.7.CP-037; operator-nfr.md §4.3.
func ResolvePolicyConfig(layers PolicyConfigLayers) PolicyConfig {
	return MergeConfigs(
		layers.RuntimeOverride,
		layers.OperatorPolicy,
		layers.WorkflowDef,
		layers.DefaultConfig,
	)
}
