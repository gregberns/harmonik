# S01 Reconciliation Workflow Library

Owned by S01 (Orchestrator Core) per [reconciliation/spec.md §4.1 RC-004].

This directory contains the concrete DOT workflows, YAML policies, and
investigator-agent prompt templates for the three investigator-required
reconciliation categories: Cat 2, Cat 3 generic, and Cat 6a.

## Structure

```
workflows/
  cat-2.dot          — Cat 2 (non-idempotent in-flight), budget 600s
  cat-3.dot          — Cat 3 (store disagreement generic), budget 300s
  cat-6a.dot         — Cat 6a (integrity violation LLM-triageable), budget 900s

policies/
  cat-2.yaml         — CP-035 policy + RC-016 playbook for Cat 2
  cat-3.yaml         — CP-035 policy + RC-016 playbook for Cat 3
  cat-6a.yaml        — CP-035 policy + RC-016 playbook for Cat 6a

prompts/
  cat-2-investigator.md   — Investigator prompt template for Cat 2
  cat-3-investigator.md   — Investigator prompt template for Cat 3
  cat-6a-investigator.md  — Investigator prompt template for Cat 6a
```

## Key design decisions

- All three DOT files carry `workflow_class="reconciliation"` per [schemas.md §6.5].
- All three DOT files route unconditionally: start → investigator → close.
  Verdict routing is daemon-side (RC-025a); no DOT-level verdict branching.
- Investigator node: `agent_type="claude-code"`, `role="investigator"` per RC-015a.
- Minimum skills: `beads-cli` + `git-inspection` per RC-004. Cat 2 and Cat 6a
  also include `workspace-inspection` (required for WIP capture and workspace probes).
- Wall-clock budgets per RC-017: Cat 2=600s, Cat 3=300s, Cat 6a=900s.
- Cat 6a policy sets `confirm_required: true` per the RC-027 default recommendation
  (operator confirmation before mechanical repair; escalate-to-human is the default verdict).

## Updating this library

When adding a new investigator-required category (per RC-006 amendment protocol):
1. Add a `.dot` file in `workflows/`
2. Add a `.yaml` policy in `policies/` with the required budget and playbook
3. Add a prompt template in `prompts/`
4. Update this README
5. Ship daemon-code changes (detector + action-map entry) in the same release (RC-006)
