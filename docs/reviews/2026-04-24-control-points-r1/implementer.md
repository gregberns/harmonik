# Round 1 Implementer Review — control-points.md v0.1.0

## Verdict summary

Control-points is largely implementable: the per-Kind table (CP-005), the Go-ish record schemas in §6.1, and the registration sequence in §7.1 are concrete enough to stand up a registry, a policy-YAML loader, and dispatch paths for all four Kinds. The `expr-lang/expr` adoption is a defensible decision, and the environment binding (`run`, `outcome`, `event`, `context`, `policy_meta`) is named. Replay-safety via persisted verdict has a clear split between producing and persisting. Gaps are concentrated in four places: the Gate verdict persistence path, the policy-YAML evaluator schema for cognition-tagged vs mechanism-tagged Gates, the actual registration-surface Go signature, and a set of cross-reference mismatches between this spec and handler-contract (§6.11 / §6.7 / §6.9 cited from handler-contract but nonexistent in this spec). Roughly one in six requirements I attempted is genuinely stuck on missing detail; the remainder are implementable with minor clarifications.

## Implementation sketches

### ControlPoint registry (CP-043 — CP-046) — IMPLEMENTABLE

```go
type ControlPointRegistry struct {
    mu    sync.RWMutex
    byName map[string]*ControlPoint
    byTrigger map[string][]*ControlPoint
    byAttachPoint map[AttachPoint][]*ControlPoint
}

func (r *ControlPointRegistry) Register(cp *ControlPoint) error {
    r.mu.Lock(); defer r.mu.Unlock()
    if cp.Kind == KindGuard && cp.Evaluator.Mode == ModeCognition {
        return ErrStructural // CP-020
    }
    if existing, ok := r.byName[cp.Name]; ok {
        if bodyEqual(existing, cp) { return nil } // idempotent
        return fmt.Errorf("duplicate registration with divergent body: %s", cp.Name)
    }
    r.byName[cp.Name] = cp
    // index by trigger / attach_point for fast lookup
    return nil
}
```

Clean. Lookup methods (`LookupByName`, `LookupByTrigger`, `LookupByAttachPoint`) are named in CP-046 but their return shapes aren't. I inferred `(ControlPoint, bool)` for name and `[]ControlPoint` for the other two — the spec should declare the INTERFACE explicitly.

### Policy expression evaluator (CP-034 / §6.4) — IMPLEMENTABLE

```go
import "github.com/expr-lang/expr"

type PolicyEnv struct {
    Run         *Run                    // [execution-model.md §6.1 Run]
    Outcome     *Outcome                // [execution-model.md §6.1 Outcome]
    Event       *Event                  // Hook context only; nil otherwise
    Context     map[string]any          // run's shared context
    PolicyMeta  map[string]string       // policy document metadata
}

func CompileGate(cp *ControlPoint) (*vm.Program, error) {
    return expr.Compile(
        cp.Evaluator.Expression,
        expr.Env(PolicyEnv{}),
        expr.AsBool(),
    )
}

func EvaluateGate(program *vm.Program, env PolicyEnv) (bool, error) {
    out, err := expr.Run(program, env)
    if err != nil { return false, err }
    return out.(bool), nil
}
```

The worked example `outcome.status == "SUCCESS" && run.workflow_version == policy_meta.required_version` compiles against the declared env. `expr-lang/expr` is side-effect-free by construction (no I/O builtins, no loops by default, no recursion), matching CP-034 purity. Gate expressions must be `AsBool`; Hook subscription filters must be `AsBool`; Budget expressions (when expression-form) must be bool-or-numeric. The spec is silent on which expression-form is required where; I inferred bool from the `{allow, deny}` evaluator return.

Compile-time env binding works cleanly: expose structs with exported fields matching the env names (`Run`, `Outcome`, `Event`, `Context`, `PolicyMeta`), and `expr` produces helpful type errors at ingest against misspelled paths. This gives CP-034 "MUST be side-effect-free" plus ingest-time validation for free. What's missing: whether ingest-time type-checking is mandatory (I believe yes given CP-035 "Missing any required section is a validation error detected at registration time", but it's not explicitly stated for individual expressions).

### Gate dispatch (CP-006 — CP-011) — MOSTLY IMPLEMENTABLE

```go
func (s1 *Orchestrator) evaluateGate(ctx context.Context, cp *ControlPoint, txCtx TransitionContext) GateVerdict {
    if cp.Evaluator.Mode == ModeMechanism {
        ok, _ := Evaluate(cp.Evaluator.Expression, buildEnv(txCtx))
        if ok.(bool) { return GateVerdict{Action: Allow} }
        return GateVerdict{Action: Deny, Reason: "policy"}
    }
    return invokeCognitionEvaluator(ctx, cp, txCtx) // §7.2
}
```

Stuck on Gate verdict persistence: CP-040 says "the verdict is written into the Transition record's `evidence` field ... keyed by `gate_name` BEFORE the transition advances." Execution-model's `Transition.evidence: Map<String, Any>` supports this, and EM-021 externalizes large payloads. But CP-040 does not say what the verdict payload shape is (just the allow/deny? the full reason? the model response?), nor what happens on a pre-entry Gate that fires before any transition exists. I had to invent a `GateVerdictRecord{gate_name, action, reason, cognition_meta?}` shape.

### Hook dispatch (CP-012 — CP-017) — IMPLEMENTABLE with a caveat

The S05 sketch is straightforward: subscribe on the event bus, filter-match via `subscription_filter` expression, apply the side-effect per `side_effect_kind`, emit `hook_fired` / `hook_failed`. Ordering is (subsystem_priority, declaration_order) per CP-014.

Hook verdict persistence for cognition-tagged Hooks has a concrete path: `.harmonik/hooks/<run_id>/<hook_invocation_id>.json` (CP-040). OQ-CP-004 correctly flags the unscoped-hook case.

Caveat: CP-012 names three `side_effect_kind` values (`emit-event`, `state-mutation`, `external-action`), but "state-mutation via a mechanism-tagged helper, never direct write" is not pinned to a specific helper interface. An implementer has to invent `StateMutationHelper` without a spec-declared surface. Similarly `external-action via a handler-owned effector` — what's an effector? Not in handler-contract.md either.

### Guard dispatch (CP-018 — CP-021) — IMPLEMENTABLE

```go
func (s1 *Orchestrator) applyGuards(edges []Edge, state State, outcome *Outcome) []Edge {
    for _, g := range s1.registry.LookupByTrigger("edge-evaluation") {
        result, _ := Evaluate(g.Evaluator.Expression, env)
        edges = coerceToEdgeList(result, edges) // must be subset/permutation
    }
    return edges
}
```

Mechanism-only is enforced at registration (CP-020 / CP-INV-004). Reorder-only is enforced at evaluation-time by validating the returned list is a subset/permutation of the input — CP-018 names the rule; implementer writes a trivial set check. §8 says Guard error maps to `ErrStructural` and the cascade "proceeds on the un-reordered edge list" — good fallback semantics. Emits `guard_failed` (named in §8 but NOT in the §6.5 event list — **drift**).

### Budget enforcement (CP-022 — CP-027) — IMPLEMENTABLE

Dispatch-time check, per-chunk accrual, 0.8 warning, scope-target tightest-wins. All concrete. The "no `GetBudgetCounter()` surface" invariant (CP-INV-005) is a clear guardrail. The counter state living "internal to the handler tick" is implementable as a handler-local struct that emits on every mutation.

One gap: CP-027 defers "per-role vs per-run vs per-state tightest-wins arithmetic" composition when a reconciliation wall-clock Budget envelops a per-state Budget inside. "Tightest wins on any accrual check" (CP-022 last sentence) gives the rule; an implementer can produce it.

### Cognition-tagged evaluator with persisted verdict (CP-039 — CP-042) — IMPLEMENTABLE

```go
func invokeCognitionEvaluator(ctx context.Context, cp *ControlPoint, ic InvocationCtx) (Verdict, error) {
    key := verdictKey(cp, ic) // {control_point_name, run_id, transition_id|event_id}
    if existing, ok := readPersistedVerdict(ic.TaskBranch, key); ok {
        return existing, nil // CP-041 replay path; no model re-invocation
    }
    verdict, err := dispatchToRole(ctx, cp.Evaluator.DelegationPath, ic) // cognition-tagged
    if err != nil { return Verdict{}, err }
    if err := persistVerdictToGit(ic.TaskBranch, key, verdict); err != nil { // mechanism-tagged
        return Verdict{}, err
    }
    emit("hook_verdict_persisted", key, verdict)
    return verdict, nil
}
```

§7.2 is clear on the two sides of CP-042. For Hooks, the persistence path `.harmonik/hooks/<run_id>/<hook_invocation_id>.json` is explicit. For Gates, the path is "into Transition.evidence[gate_name]", which lands as a JSON key inside the sibling file at `.harmonik/transitions/<transition_id>.json` (or, for large payloads, externalized under `.harmonik/transitions/<transition_id>/evidence/*` per EM-021). That path works, with the caveat below.

### Skill declaration surface for handler-contract (CP-049 — CP-052) — IMPLEMENTABLE but under-specified for the consumer

Handler-contract.md §4.11 consumes `LaunchSpec.required_skills[]` + `skill_search_paths[]`. Control-points CP-049 declares nodes MAY carry `required_skills` in DOT, and CP-050 says "effective skill set = node ∪ role default." But:

- CP-049 says node-level is "comma-separated list of skill names as a DOT attribute value" OR "YAML policy reference via `policy_ref` naming a skill set." The YAML policy shape in §6.3 does NOT include a `skill_sets` section — where does a "skill set" get declared in YAML? The roles block carries `default_skills`, but not a standalone reusable skill-set.
- CP-050 names the handler's obligation to emit `skills_provisioned` but the skill-resolution step (name → package path) is declared in handler-contract HC-047 (first-match against `skill_search_paths`). Control-points does not declare who populates `skill_search_paths` in LaunchSpec. Is it the daemon? From where? This is the invisible seam between the two specs.
- CP-052 says Beads-CLI is the motivating default. Where does its package live? `skill_search_paths` is populated by ... ? Neither spec says. An implementer invents a daemon-level config key `harmonik.skill_paths`.

## Under-specified / missing details

1. **Registry API surface is prose, not a declared INTERFACE.** CP-044 says `RegisterControlPoint(cp) -> error`; CP-046 names `LookupByName`, `LookupByTrigger`, `LookupByAttachPoint` but gives no signatures. §6.1 declares the `ControlPoint` record but no `Registry` interface. An implementer invents the shape.

2. **Gate verdict payload shape on persistence (CP-040).** "Written into Transition.evidence keyed by gate_name" — what's the value type? `{action, reason, cognition_meta?}`? A free-form map? Naming the record shape (a la §6.1.5 DelegationPath) is needed.

3. **Hook verdict path for pre-run / pre-transition hooks.** OQ-CP-004 flags daemon-lifecycle hooks. But an `on_transition_attempted` Hook that fires BEFORE the transition is durable also lacks a `run_id`-scoped path that's guaranteed to exist. The spec's default ("daemon-scoped branch") is reasonable but not yet normative.

4. **`expr-lang/expr` version pinning.** OQ-CP-003 flags this. Two concrete consequences for an implementer: (a) `expr.Env` type-safety depends on the version; (b) evaluation-cost ceilings are version-dependent. Pinning to a semver range should be declared alongside adoption, not deferred.

5. **`state-mutation` side-effect: who is the mechanism-tagged helper?** CP-012 says "via a mechanism-tagged helper, never direct write." No such helper is declared anywhere. Concretely: can a Hook mutate `run.context`? If yes, through what API? Handler-contract doesn't expose one.

6. **`external-action` side-effect: what's an "effector"?** Same issue. Not declared. An implementer would have to invent a `HandlerEffector` interface.

7. **Cross-reference mismatches with handler-contract.** Handler-contract cites `[control-points.md §6.11]` for skill-declaration, `§6.7` for freedom profile, `§6.9` for budget. Control-points §6 only reaches §6.6. The content is present — skill declaration is §4.11, FreedomProfile in §6.2, Budget in §6.1.4 — but the cited section numbers don't exist. Both specs need to agree.

8. **Policy document cross-reference resolution (OQ-CP-005).** The default-if-unresolved is reasonable, but it's not normative yet. A multi-document operator deployment cannot be built without this.

9. **Schema versioning for Evaluator and per-Kind payloads.** CP-001 carries `schema_version` on ControlPoint; §6.1.1–6.1.4 payloads do not. If GatePayload adds a field post-MVH, does the ControlPoint's schema_version bump? The §6.6 rule is whole-document; but an implementer needs per-sub-record rules to know what N-1 readable means.

10. **`guard_failed` event is named in §8 but not in §6.5.** Minor, but §6.5's event list is supposed to be exhaustive.

11. **`registry.CP-044` idempotency body-comparison rule is opaque.** "Identical body" — field-by-field equality? Canonicalized serialization? Authors who re-emit the same YAML with a different field order need to know.

## Recommendations

1. **Add a §6.1.0 `Registry` INTERFACE schema** next to the `ControlPoint` RECORD — name the four method signatures (`Register`, `LookupByName`, `LookupByTrigger`, `LookupByAttachPoint`), their input types, and their return shapes. This is the handler-contract §6.1 INTERFACE pattern; applying it here closes the biggest sketch-time invention.

2. **Add a §6.1.6 `GateVerdictRecord` shape** covering both mechanism-tagged and cognition-tagged verdicts: `{gate_name, action, reason, cognition_meta: DelegationPathSnapshot | None}`. Declare that this is the value written to `Transition.evidence[gate_name]`.

3. **Declare the `StateMutationHelper` and `HandlerEffector` surfaces** (even as one-line INTERFACE stubs with a cross-reference to the handler-contract side) OR move `state-mutation` and `external-action` into an OQ for post-MVH. Leaving them named without an API invites three different implementations.

4. **Fix cross-references with handler-contract.** Either renumber control-points §6 to expose `§6.7 Freedom profile`, `§6.9 Budget`, `§6.11 Skill declaration` (promoting §4.6, §4.5, §4.11 content into §6), OR edit handler-contract to cite `§6.2 FreedomProfile`, `§6.1.4 BudgetPayload`, `§4.11 Skill declaration`. Cross-spec consistency is load-bearing.

5. **Add `guard_failed` to §6.5** (and confirm it's in event-model.md §3.2).

6. **Promote OQ-CP-003 (version-pin) to a normative requirement** — pin to a specific `expr-lang/expr` release in `go.mod`; a version bump is a harmonik upgrade. The default-if-unresolved is already the desired behavior.

7. **Add a skill-set declaration shape in §6.3 policy YAML** — currently a `policy_ref` naming a skill set has no target shape. Either add a `skill_sets[]` section to the YAML template, or restrict CP-049 node-level declarations to the inline comma-separated form for MVH.

## Affirmations

1. **The per-Kind table (CP-005) is the single biggest usability win.** One glance and an implementer knows the full state space: trigger → evaluator input → return type → outcome-action → boundary rule. Every Kind-specific subsection elaborates rather than re-specifies. Copying this pattern to other taxonomy-heavy specs would pay off.

2. **Replay-safety via persisted verdict is crisply split (CP-042).** The mechanism/cognition split between producing and persisting the verdict, with the explicit single-tag-rule application, is one of the cleanest cognition-safety stories I've read. CP-INV-003 pins the replay behavior unambiguously.

3. **The §7.1 registration sequence maps branch-by-branch to normative requirements.** Every `IF`/`FAIL` corresponds to a cited CP-NNN. An implementer can audit the code path against the spec line-by-line.

4. **The Budget-is-a-Kind decision is defended in-line (informative block after CP-005) and in A.3.** The rationale makes clear why forcing Budget into a Gate shape would be worse. Readers who might have proposed the collapse get the answer up front.

5. **The ownership split (CP-047) is explicit and exhaustive.** S02 owns the registry; S01 owns Gate+Guard invocation; S05 owns Hook dispatch. No ambiguity; no silent handoffs. This is exactly the kind of cross-subsystem pin that avoids kerf-time re-litigation.

6. **`expr-lang/expr` adoption (CP-034) is the right call and the rationale in A.3 lands it cleanly.** Side-effect-free, bounded evaluation, Go-native, maintained, type-checkable at ingest. Rejecting custom grammars and general-purpose languages with one paragraph each is exactly the right depth.

7. **The three-artifact separation (DOT / YAML / bead) is enforced at CP-036 (DOT references YAML by name; no inline policy bodies).** This is the architectural pin that keeps workflow graphs cheap to reason about.
