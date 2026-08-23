package policy

import (
	"encoding/json"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// GateVerdictSchemaVersion is the schema version expected in gate-verdict.json
// files written by cognition gate evaluators. Frozen wire (schema_version:1).
const GateVerdictSchemaVersion = 1

type gateVerdictJSON struct {
	SchemaVersion int    `json:"schema_version"`
	Decision      string `json:"decision"`
	Reason        string `json:"reason,omitempty"`
}

// ParseGateVerdict parses raw gate-verdict.json bytes into a core.GateAction.
// Both the local and remote (Via) daemon read paths share this byte-identical
// validation (NFR7): schema_version must equal GateVerdictSchemaVersion and the
// decision must be a valid core.GateAction (allow, deny, or escalate-to-human).
func ParseGateVerdict(data []byte) (core.GateAction, error) {
	var v gateVerdictJSON
	if err := json.Unmarshal(data, &v); err != nil {
		return "", fmt.Errorf("unmarshal gate-verdict.json: %w", err)
	}
	if v.SchemaVersion != GateVerdictSchemaVersion {
		return "", fmt.Errorf("gate-verdict.json: schema_version=%d, want %d", v.SchemaVersion, GateVerdictSchemaVersion)
	}
	action := core.GateAction(v.Decision)
	if !action.Valid() {
		return "", fmt.Errorf("gate-verdict.json: decision=%q is not a valid GateAction (must be allow, deny, or escalate-to-human)", v.Decision)
	}
	return action, nil
}

// MechanismDecision maps a mechanism-tagged Gate expression's Bool result to a
// core.GateAction, per specs/control-points.md §6.4:
//
//   - true  → core.GateActionAllow
//   - false → core.GateActionDeny
//
// The daemon shell compiles and evaluates the PolicyExpression (core.PolicyExprEvaluator)
// against the gate environment and threads the resulting bool in here.
func MechanismDecision(allow bool) core.GateAction {
	if allow {
		return core.GateActionAllow
	}
	return core.GateActionDeny
}

// GateEvalFailureOutcome returns a FAIL Outcome with failure_class=structural and
// the given reason in Notes. Used by the daemon for pre-eval failures (no registry,
// gate_ref not found, wrong kind, unknown evaluator mode) that are not the result
// of the evaluator itself.
func GateEvalFailureOutcome(reason string) core.Outcome {
	fc := core.FailureClassStructural
	return core.Outcome{
		Status:       core.OutcomeStatusFail,
		Kind:         core.OutcomeKindDefault,
		FailureClass: &fc,
		Notes:        "gate dispatch: " + reason,
	}
}

// GateExprEnv is the typed evaluation environment for mechanism-tagged Gate
// policy expressions, per specs/control-points.md §6.4.
//
// The spec declares: run, outcome, event, context, policy_meta. For gate
// pre-entry dispatch, outcome and event are nil (no outcome has been produced
// yet; gate nodes are not event-triggered). policy_meta is nil (no
// policy-document metadata is threaded to the daemon in the current
// implementation). The daemon shell populates this value and feeds it to the
// core.PolicyExprEvaluator Compile/Evaluate calls (which stay in the daemon).
type GateExprEnv struct {
	Run        *core.Run      `expr:"run"`
	Outcome    *core.Outcome  `expr:"outcome"`
	Event      interface{}    `expr:"event"`
	Context    map[string]any `expr:"context"`
	PolicyMeta map[string]any `expr:"policy_meta"`
}
