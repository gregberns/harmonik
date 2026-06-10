package daemon

// reviewerharness_hkiv748.go — reviewer harness resolution (codex-harness C5/T14, hk-iv748).
//
// Implements the reviewer harness precedence for the builtin review-loop and the
// DOT cascade:
//
//   DEFAULT: reviewer uses the SAME resolved harness as the implementer for that run.
//   OPTIONAL OVERRIDE: driven by the reviewer_harness DOT node attribute (parsed by
//     T5 hk-u67of into dot.Node.ReviewerHarness). When present on the IMPLEMENTER node,
//     the reviewer uses that harness even when the implementer used a different one
//     (e.g. codex-implemented bead reviewed by claude).
//
// Precedence walk for the reviewer (DOT mode):
//  1. reviewerHarnessOverride — implementer node's reviewer_harness= attr, if valid.
//  2. node.Harness — reviewer node's own harness= attr, if valid.
//  3. deps.launchSpecBuilder — DEFAULT: same resolved harness as the implementer.
//
// Review-loop mode (runReviewLoop):
//   The reviewer specBuilder is built with nodeDefault = implArtifacts.resolvedAgentType
//   (the implementer's resolved harness). For all-claude runs this is byte-identical to
//   pre-T14 behaviour. No DOT reviewer_harness override is applicable in review-loop mode
//   (no DOT node exists); that override is handled in dispatchDotAgenticNode.
//
// Spec ref: codex-harness C5, T14.
// Bead: hk-iv748 [C5/T14]
