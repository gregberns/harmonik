// Command harmonik is the production daemon binary for harmonik.
//
// # Composition root
//
// This file is the composition root: it constructs every production dependency
// and wires them together before handing control to the daemon's run loop.
// Production bindings declared here:
//
//   - PolicyEngine: [core.NoOpPolicyEngine] — permits every evaluation with no
//     constraints. This is the first-class production binding for MVH; it is
//     NOT a nil sentinel and NOT a test double. The orchestrator dispatcher
//     calls PolicyEngine.Evaluate on every gate and guard without branching on
//     the concrete type, satisfying [specs/scenario-harness.md §4.3.SH-018].
//
// When the control-points subsystem (hk-a8bg) lands post-MVH, the composition
// root substitutes the real PolicyEngine evaluator. No dispatcher changes are
// required.
//
// Spec ref: docs/foundation/phase-1-readiness-gap-analysis.md §A5 (policy-engine
// bypass-ability must be explicit); specs/scenario-harness.md §4.3.SH-018
// (no test-mode branches in production); bootstrap-subset.md §1 (CP fully
// deferred).
package main

import (
	"os"

	"github.com/gregberns/harmonik/internal/core"
)

func main() {
	os.Exit(run())
}

// run is the testable entry-point. It constructs the composition root and
// starts the daemon. It returns an exit code.
//
// The composition root pattern keeps dependency construction separate from
// daemon logic so that the wiring can be inspected and replaced at this single
// site.
func run() int {
	// PolicyEngine binding for MVH.
	//
	// NoOpPolicyEngine is the production interface — not a nil check, not a
	// test double. The dispatcher always calls policyEngine.Evaluate; the
	// no-op always returns {Permitted: true, Constraints: nil}.
	//
	// Spec ref: docs/foundation/phase-1-readiness-gap-analysis.md §A5;
	// specs/scenario-harness.md §4.3.SH-018; bootstrap-subset.md §1.
	var policyEngine core.PolicyEngine = core.NoOpPolicyEngine{} //nolint:ineffassign // composition-root binding; dispatcher wiring is pending (hk-b3f.*)
	_ = policyEngine                                             // consumed by dispatcher once cluster-A EM beads land

	// TODO(hk-b3f): pass policyEngine to the EM dispatcher once the
	// dispatcher wiring beads (hk-b3f cluster-A) land. The binding site is
	// here; the consumer site is internal/orchestrator (not yet shipped).
	return 0
}
