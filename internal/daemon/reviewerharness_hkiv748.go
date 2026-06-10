package daemon

// reviewerharness_hkiv748.go — reviewer harness resolution (codex-harness C5/T14, hk-iv748).
//
// Implements the reviewer harness precedence for the builtin review-loop and the
// DOT cascade:
//
//   DEFAULT: reviewer uses the SAME resolved harness as the implementer for that run.
//   OPTIONAL OVERRIDE: driven by the reviewer_harness DOT node attribute (parsed by
//     T5 hk-u67of into dot.Node.ReviewerHarness). When present, the reviewer uses that
//     harness even when the implementer used a different one (e.g. codex-implemented,
//     claude-reviewed).
//
// Precedence walk for the reviewer:
//  1. reviewer_harness DOT node attribute (from the IMPLEMENTER node) → if valid, use it.
//  2. Fallback: implementer's resolved agent type (same harness as implementer).
//
// For an all-claude run (no reviewer_harness attr, no codex selection) the behaviour is
// byte-identical to pre-T14: reviewer = claude.
//
// This file is a declaration anchor. The resolution logic is wired into:
//   - dot_cascade.go (driveDotWorkflow + dispatchDotAgenticNode) for DOT-mode runs.
//   - reviewloop.go (runReviewLoop) for review-loop-mode runs.
//
// Spec ref: codex-harness C5, T14.
// Bead: hk-iv748 [C5/T14]

// WIP: full implementation in progress.
