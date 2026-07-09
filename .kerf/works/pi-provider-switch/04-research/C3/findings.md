# C3 — Claim-time per-bead profile resolver — Research findings

Resolve the bead's profile label → tuple at claim time in `workloop.go`, inheriting
the hk-pkugu harness-aware discipline, carry it in `claudeRunCtx`, and project it
at the `RunCtx` literal. This is the crux component.

## Research questions

1. How exactly is the existing per-bead `model:` label parsed and resolved, and
   where do labels come off the bead record?
2. How does hk-pkugu's harness-aware agent-type resolution work, and precisely how
   must C3 inherit it (ordering)?
3. Where is the resolved value carried into the run and projected onto RunCtx?
4. What precedence applies if a bead has BOTH a `model:` label AND a `profile:`
   label?
5. Is there a second per-node leak path (like hk-lfrub) C3 must respect?

## Findings

**Q1 — model: label parse/resolve (the template).**
- Label prefix constant: `labelPrefixModel = "model:"` (modelpreference.go:166);
  sibling `labelPrefixEffort = "effort:"` (:169). Harness labels use
  `harnessLabelPrefix` ("harness:", harnessresolve.go:144).
- Labels come off `beadRecord.Labels` (`core.BeadRecord.Labels []string`). At claim
  time workloop passes `beadRecord.Labels` into `ResolveModelPreference`
  (workloop.go:3085).
- Resolution: `resolveModelField` (modelpreference.go:203-256) does a four-tier
  walk. TIER 1 collects all labels with the prefix (modelpreference.go:213-218);
  **exactly one** → `strings.TrimPrefix(modelLabels[0], labelPrefixModel)` accepted
  AS-IS, shape validation deferred (value-opacity, :220-224). **>1** → conflict:
  emit `bead_label_conflict`, treat tier-1 as absent, fall through (:225-232).
  **0** → tier-1 absent, no event (:233). Then tier-2 project config
  (`LookupAgent`), tier-2.5 env, tier-3 `defaultModelEntries`, tier-4 empty.
  **This collect→count(==1 accept / >1 conflict / 0 absent) pattern is the exact
  template for a `profile:<name>` collector.** The profile resolver is SIMPLER than
  model: it has no tier-2/2.5/3 cascade — a bead either names a profile (look it up
  in C2's validated map) or it doesn't (empty tuple → C4 h.* fallback).

**Q2 — hk-pkugu harness-aware discipline (load-bearing ordering).**
- `resolveHarnessAgentTypeQuiet(bead, queueDefault, nodeDefault, globalDefault)`
  (harnessresolve.go:135-166) is a NON-emitting clone of the four-tier harness walk
  (`resolveHarness`, harnessresolve.go:53-118). Tier-1: exactly one valid
  `harness:<type>` label with `.Valid()` (:148-153); else queue/node/global
  default; else built-in claude-code fallback (:164-165). It returns EXACTLY what
  `resolveHarness` returns but emits no events (the launch-time `resolveHarness`
  fires `harness_selected` later — avoids double-emit).
- In workloop this runs FIRST (workloop.go:3077-3082) with queue="" node=""
  global=`deps.defaultHarness`, producing `resolvedAgentType`, which is then passed
  to `ResolveModelPreference` (workloop.go:3083-3090) so the model tier-3 default
  matches the real harness. Comment 3066-3076 documents the leak: a hardcoded
  claude-code sealed `sonnet` into `rc.model` for pi runs.
- **C3 MUST run its profile resolver AFTER line 3082 (harness type known) and key
  off `resolvedAgentType`.** Concretely: only resolve a pi profile tuple when the
  resolved harness is the pi family; for a claude/codex-resolved bead the tuple must
  be empty (no profile). This mirrors the leak fix's contract exactly. Because the
  profile registry lives only under `harnesses.pi`, a `profile:` label on a
  claude-resolved bead should resolve to an EMPTY tuple (harness mismatch) — the
  change spec should decide whether that is silent (empty tuple) or a fail-loud
  conflict. Recommended: empty tuple + optional observability event, matching the
  quiet, non-fatal handling of tier-1 mismatches elsewhere.

**Q3 — carry + projection (both edits required).**
- `claudeRunCtx` struct: claudelaunchspec.go:50. `resolvedModel` lands at
  `claudeRunCtx.model` (workloop.go:4052), alongside `effort:` (:4053). ADD the
  five tuple fields to `claudeRunCtx` and populate them from the resolved profile
  right where `model:`/`effort:` are set (workloop.go:4052-4053).
- Projection: harnessregistry.go:240-264 is the ONLY place `claudeRunCtx` is
  projected onto the public `RunCtx` literal for the pi path (`Model: rc.model` at
  :258, `Effort: rc.effort` :259). ADD the five new fields to this literal too.
  Missing either edit = the tuple never reaches `LaunchSpec` (analysis §5).

**Q4 — model: vs profile: precedence (KEY open decision).** Today `model:` and the
profile tuple would BOTH feed `rc.Model`. A profile carries its own `model` field;
a bare `model:<alias>` label also sets `rc.Model`. If a bead has both, they can
disagree. Options for the change spec:
  (a) **profile is atomic, model: is ignored when a profile is present** — cleanest,
      preserves the "tuple travels as one unit" invariant (03-components rationale);
      but silently drops a model: label.
  (b) **model: overrides the profile's model field only** (provider/base_url/api
      from profile, model from the label) — flexible but re-introduces the
      wire-format-split risk only for model (model is not part of the coupled
      triple, so this is arguably safe); matches how `rc.Model` already overrides
      `h.model` per-field at C4.
  (c) **conflict → fail-loud / emit bead_label_conflict**, treat as absent.
  RECOMMENDATION: (b) is the most consistent with the existing per-field
  override-with-fallback mechanism (model is orthogonal to provider+base_url+api),
  but (a) is safest for the atomic-tuple story. FLAG FOR OPERATOR/CHANGE-SPEC
  DECISION. Whichever is chosen, if profile is absent the existing `model:` path
  must remain byte-identical (C6).

**Q5 — second leak path (hk-lfrub / DOT per-node).** A per-node DOT `model=`
attribute is applied via `nodeModelForHarness(resolvedModel, node.Model,
effectiveHarness)` (dot_cascade.go:1274-1281, tested in hk_lfrub test) which pins
`node.Model` ONLY for the claude-code family and leaves pi/codex at the run-level
`resolvedModel`. **The provider tuple is NOT threaded through the DOT per-node
path today** — it is set once at claim time (workloop.go:4052) and rides
`claudeRunCtx` unchanged through the cascade. C3 must confirm the tuple is not
clobbered by the DOT cascade (it operates on `model=`/`effort=`, not provider), and
that per-node re-resolution (dot_cascade.go:1281) does not need a parallel
profile-aware guard. Likely no DOT change needed, but the change spec must verify
the tuple survives `driveDotWorkflow` node re-launches unchanged (the C5
no-tier3-leak scenario should cover a DOT-path pi bead, not only single-mode).

## Value-opacity at this hop
- Profile NAME / model value are never value-validated at claim time (matches
  modelpreference.go:220-224). Existence of the profile name is checked against
  C2's validated map; an unknown reference fails loud here (C2 doesn't see the
  label, only the config). No shape re-check needed (C2 already validated).

## Risks / open decisions for change spec
- **model:/profile: precedence (Q4)** — the one genuine design decision; flag it.
- **harness-mismatched profile label** (profile: on a claude bead) — silent empty
  tuple vs fail-loud (Q2). Recommend silent empty + optional event.
- **DOT-path survival** — verify the tuple rides the cascade unclobbered; add a
  DOT-path variant to the C5 no-leak scenario (Q5).
- **Ordering is load-bearing:** profile resolve MUST be after
  resolveHarnessAgentTypeQuiet (workloop.go:3082); violating re-opens the pkugu leak.
- Label spelling: `profile:<name>` prefix, one new const beside `labelPrefixModel`.
