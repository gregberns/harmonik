# C4 Research Findings — control-points / node-type binding

## 1. Existing contract surface (already locked)

- **CP-002 / CP-036.** ControlPoints carry a unique `name`. DOT attributes `gate_ref`, `policy_ref`, `freedom_profile_ref`, `budget_ref` (per `execution-model.md §4.2`) MUST carry that name; inline policy bodies in DOT are forbidden.
- **CP-035 / §6.3.** Policy YAML required sections: `metadata`, `roles[]`, `freedom_profiles[]`, `gates[]`, `hooks[]`, `guards[]`, `budgets[]`, plus `skill_sets[]`. Each DOT attr resolves into one section.
- **CP-043 / CP-045.** Registry is one in-process Go map keyed by `name`, daemon-scoped, rebuilt at startup. No cross-daemon sharing.
- **CP-049 / CP-050.** Node `required_skills` may be inline DOT comma-list OR a `policy_ref` to `skill_sets[]`. Effective skills = union(node, role.default_skills). Ingest-time syntactic check here; launch-time package resolution is handler-contract's.
- **CP-037 / CP-038.** Source precedence runtime > operator > workflow > defaults, deep-merge, no mid-run reloads; per-doc `schema_version` integer, N-1 readable.
- **AR-INV-007 / AR-INV-008.** Daemon-owned in-process registry (forbids out-of-daemon stores); three-artifact separation with policy YAML as an exempt **configuration artifact**.

## 2. Control-point vs. gate (pass-1 Q1)

The CP spec models ControlPoint as a **policy-layer primitive** (4 Kinds: Gate/Hook/Guard/Budget) attached via `*_ref` attributes — NOT a node-type. The pass-1 5-type taxonomy lists `control-point` and `gate` as distinct node-types. Three reconcilable framings:

- **A — control-point isn't a node-type.** Drop from C1 catalog; `gate` becomes "node whose handler evaluates a Gate-kind ControlPoint." 5→4 type taxonomy. (Closest to Kilroy's `goal_gate=true` attribute form.)
- **B — control-point is a node-type that calls into S02 registry.** Gate stays as "inline `condition` on edge"; control-point is the "by-reference" form.
- **C — gate is a sub-type of control-point.** Single `control-point` node-type with a required `kind` attribute; the CP `Kind` enum becomes the discriminant.

Kilroy attribute-form precedent + minimal contract surface favors A or C. B fragments the catalog without buying anything.

## 3. Resolution scope

CP-045 pins scope to **daemon = project**. Within a project: all YAML in the operator's chain merges into one flat name-space; CP-044 makes re-registration safe iff body is identical, else startup fails. Open trade-offs:

- **Cross-document collisions** caught at daemon-init, not silently masked — but a project importing two libraries with same-named gates can't run. Consider per-document name-prefix convention.
- **Per-workflow override** (CP-037 §3 allows "workflow definition" overrides) is mechanically implied via deep-merge overlay but not spelled out. No worked example exists.
- **Project-wide library** = supported by construction (one daemon, one flat registry). **Cross-project library** forbidden by AR-INV-007; workaround = vendored YAML. Long-tail; out of MVH.

## 4. Schema-version drift

DOT graphs reference policy by name only — no version pinning. Two version surfaces exist independently (DOT graph `schema_version`, YAML doc `metadata.schema_version`). Three concerns:

1. Same name, different YAML schema across runs — CP-040a hash detects drift for **cognition-tagged** evaluators only; mechanism-tagged Gates silently accept new policy.
2. Should `gate_ref="approve_pr@v3"` versioned-ref form exist (parallel to skill `name@version` in CP-049)? Not in current contract; would require (name, version)-keyed registry.
3. CP-037 "no mid-run reloads" + CP-INV-003 escalation cover cognition path; mechanism-tagged equivalent missing.

## 5. Skill-injection binding

- C2 ingestion (per Q7 — does driver re-parse on resume) must run CP-049 syntactic check; if resume trusts prior parse, skill-validation must be persisted alongside.
- **Real ambiguity:** CP-049 says skill-set reference goes via `policy_ref` overloaded with the broader `policy_ref` that names gates/freedom-profiles. Every other CP kind has a typed attribute (`gate_ref`, `freedom_profile_ref`, `budget_ref`). Likely intended: a dedicated `skills_ref` attr; spec text muddies this.
- Beads-CLI default skill (CP-031/CP-052) flows through role assignment, not DOT — examples must not redeclare on every node.

## 6. Cross-component dependencies

- **C1 (workflow-graph.md node-type catalog).** Symmetric with C4 — §2 framing choice sets C1's catalog row(s). Neither can be drafted without naming the other.
- **C3 (handler-contract Outcome).** Gate `deny`-with-`escalate` action interacts with `failure_class` — escalated gate likely needs a typed failure class; not in current CP-005 table.
- **C2 (execution-model driver).** Must call Guard `LookupByTrigger` BEFORE EM-042 cascade per CP-019; must implement all four Gate attach-points (node pre/post, edge before/after-selection per CP-007) without DOT-syntax inflation.
- **C5 (examples).** review-loop.dot must demonstrate `gate_ref` + `freedom_profile_ref` round-trip; otherwise binding contract is unanchored.

## 7. Top-3 open design decisions for pass-4

1. **Q1 — control-point as node-type vs. policy primitive.** Blocks both C1 and C4. Recommend Framing A (drop control-point from node-type catalog; gate stays as node-type meaning Gate-kind-evaluator) OR Framing C (single control-point node-type with `kind` discriminant). Reject B.
2. **Disambiguate `policy_ref` overload.** Same attribute appears to bind gates, freedom profiles, AND skill sets. Either typed attributes (`skills_ref` joins the family) or declare deterministic kind-lookup order. The typed-attribute path is symmetric with existing CP-036 and recommended.
3. **Schema-version drift handling for mechanism-tagged Gates.** Cognition path is closed (CP-040a hash + CP-INV-003 escalation); mechanism path silently accepts policy edits. Decide: re-validate on `schema_version` bump (mechanism re-eval), require pin (`gate_ref@v`), or document as accepted behavior.

## 8. Cross-component dependencies summary

C4 depends symmetrically on **C1** (node-type catalog) for the Q1 resolution; on **C3** for `failure_class` interaction with escalated Gate denials; on **C2** for attach-point dispatch surface; on **C5** for round-trip example validation. C4 is NOT a leaf — it cannot land before C1's node-type table commits to one of Framings A/B/C.

## 9. Is CP-036 / CP-049 sufficient?

**Mostly yes, with three needed expansions:** (1) disambiguate `policy_ref` overload (typed `skills_ref`); (2) resolve Q1 (control-point as node-type vs. policy primitive); (3) name a skill-set-by-name attribute symmetric with `gate_ref`/`freedom_profile_ref`/`budget_ref`. Beyond those, the binding rails (by-name resolution, daemon-scoped registry, three-artifact separation, N-1 readability, source precedence) are load-bearing and DOT-mode-ready without further expansion.
