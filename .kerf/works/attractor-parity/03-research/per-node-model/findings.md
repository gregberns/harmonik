# Research — Per-node model/effort selection (D, hk-q8nqr)

> Pass 3 (`research`) of `attractor-parity`, component **D**. Grounds the design for letting a DOT node pick its own model/effort, overriding the run-level default. Evidence is code-touchpoint + spec-section + kilroy-source. Resolves OQ-2 (stylesheet normative vs informative).

## Research questions

- RQ-D1. How does harmonik resolve model/effort today, and at what granularity (run vs node)?
- RQ-D2. Where in the launch path is the resolved model/effort consumed, and is there an existing per-node seam to slot an override into?
- RQ-D3. What exactly does kilroy express — the `model_stylesheet` CSS + `class="hard"` mechanism — and what is the minimal harmonik attr that captures the same workload intent?
- RQ-D4. Is the `class`+`model_stylesheet` indirection NORMATIVE or INFORMATIVE at v1? (OQ-2)
- RQ-D5. What is the value-opacity / validation contract on `model`/`effort`, and does a per-node value have to satisfy it?
- RQ-D6. Is this clean-additive, and what (if anything) must be flagged for integration?

## Findings

### F-D1 — Resolution is run-level, sealed once at claim time (EM-012b)

`specs/execution-model.md` §4.3 **EM-012b** ("Model/effort resolution precedence") defines a 4-tier walk: (1) per-task bead labels `model:<alias>` / `effort:<level>`; (2) per-project `.harmonik/config.yaml`; (3) per-agent-type compiled default; (4) built-in fallback (empty). `model` and `effort` resolve independently. The spec states the result MUST be **"resolved exactly once per run at claim time"**, sealed into the Run record as `ModelPreference` (§6.1), and **"MUST NOT be re-evaluated for the lifetime of the run."** So today every node in a `dot` run inherits one `(model, effort)` pair.

Code: `internal/daemon/workloop.go:1160` — `resolvedModel, resolvedEffort := ResolveModelPreference(...)` runs once per `beadRunOne`, then is threaded as two `string` params into `runReviewLoop` (`workloop.go:1226`) and `driveDotWorkflow` (`workloop.go:1292`). `effort` enum (EM-012b): `low, medium, high, xhigh, max`.

**Evidence the run-level seal is the contract, not an accident.** EM-012b's "exactly once, never re-evaluated" clause is mechanism-tagged and load-bearing for replay determinism — a per-node override must therefore be framed as a *resolution input that is still computed once* (it is static graph data, not a runtime re-evaluation), NOT as a second runtime resolution pass. This is the key framing constraint for the design (see F-D4 / F-D6).

### F-D2 — The per-node seam already exists; `resolvedModel`/`resolvedEffort` are plumbed per-node into `dispatchDotAgenticNode`, currently constant

`internal/daemon/dot_cascade.go:133-134` — `driveDotWorkflow` receives `resolvedModel string, resolvedEffort string`. It forwards them per-node-dispatch at `dot_cascade.go:211-214` into `dispatchDotAgenticNode(...)`, whose params at `dot_cascade.go:357-358` are again `resolvedModel/resolvedEffort`. Inside, `dot_cascade.go:414-415` sets `claudeRunCtx{ model: resolvedModel, effort: resolvedEffort }`.

That `claudeRunCtx.model/.effort` is the single consumption point: `internal/daemon/claudelaunchspec.go:328-353` — when `rc.model != ""` it validates (`validateModel`) and appends `--model <value>` to argv; same for `rc.effort` → `--effort`. So **the launch path is already keyed on a per-call `(model, effort)` string pair.** The per-node override is a one-function change: compute a node-level `(model, effort)` inside the loop (or in `dispatchDotAgenticNode`) and pass the resolved value into `rc` instead of the constant run-level value. No new plumbing, no new field on `LaunchSpec` (it already carries `model_preference` per HC-006), no adapter-surface change.

This confirms the research-pass verdict (`_parity-research.md` §4): "CLEAN ADD … `resolvedModel`/`resolvedEffort` are already plumbed per-node … currently constant."

### F-D3 — What kilroy expresses: `model_stylesheet` CSS + node `class`

Both live pipelines (`/Users/gb/github-qwick/qwick-ai/pipelines/{sentry-triage,sentry-bugfix}/pipeline.dot:5-8`) carry an identical graph-level block:

```
model_stylesheet="
  * { llm_model: claude-sonnet-4.6; llm_provider: anthropic; }
  .hard { llm_model: claude-opus-4.6; llm_provider: anthropic; }
"
```

and exactly two nodes per pipeline tag `class="hard"` (the genuinely hard reasoning nodes: `investigate` in triage; `gather_reproduce` + `investigate_fix` in bugfix). Every other `box` node inherits the `*` default (Sonnet). The semantics are pure CSS-cascade: a universal selector sets the default, the `.hard` class selector overrides it for classed nodes. The stylesheet also carries `llm_provider` (always `anthropic`) — harmonik has no provider concept (handler binding is fixed to claude-code at v1 per HC-003); `llm_provider` is informational only.

**The workload intent is two-valued at v1: "this node is hard → use the stronger model."** The CSS machinery is over-general relative to what either pipeline uses (no compound selectors, no multi-class, no specificity conflicts). Note the kilroy spelling is `llm_model: claude-sonnet-4.6` (a CSS property), not the `--model` alias harmonik passes to the claude CLI; the alias forms differ (kilroy uses fully-qualified `claude-sonnet-4.6`/`claude-opus-4.6`; harmonik's EM-012b `model:<alias>` and `--model` are opaque strings validated only for shape per F-D5).

### F-D4 — OQ-2 RESOLVED: per-node `model`/`effort` attrs NORMATIVE; `class`+`model_stylesheet` INFORMATIVE at v1

The evidence pins the lean answer. Reasons:

1. **Direct attrs hit the existing seam with zero indirection.** `model`/`effort` as per-node attrs map 1:1 onto `claudeRunCtx.model/.effort` (F-D2). A stylesheet requires a new resolver (parse CSS, match selectors against node `class`, compute specificity) — net-new mechanism for an indirection neither pipeline's 2-class usage needs.
2. **The "resolve once, never re-evaluate" constraint (F-D1) favors direct attrs.** A per-node `model` attr is *static graph data read at the same claim-time pass* — it does not violate EM-012b's no-runtime-re-evaluation invariant because the value is fixed in the artifact. A stylesheet is also static, but adds a runtime selector-match step that is harder to argue is "the same single resolution."
3. **harmonik already rejected a parallel routing/selection channel** (`01-problem-space.md` §3 non-goals: "No `retry_target` or model-stylesheet-as-routing"). A normative stylesheet would re-introduce exactly that second channel.
4. **The CSS is sugar over the two-valued intent.** Authors can express "hard node uses Opus" directly with `model="opus"` on the two hard nodes — no loss of expressivity for the live workloads.

**Resolution:** `model` and `effort` are normative per-node optional attrs on `agentic` nodes, added to WG-002 + the WG-031 reserved set, slotting as a new **tier-0** (highest precedence) layer ahead of EM-012b's tier-1 run-level walk. `class` and `model_stylesheet` are **INFORMATIVE at v1**: a loader MUST accept them under the permissive policy (WG-031/WG-032, retained in `UnknownAttrs`, warning) and MUST NOT dispatch on them. A documented authoring-convention note maps the kilroy stylesheet idiom (`* { } / .hard { }`) onto the recommended direct-attr port (`model="opus"` on classed nodes). If a real workload later needs >2 model tiers or selector indirection, promoting the stylesheet to normative is a clean follow-up — the direct attrs remain the floor.

### F-D5 — Value-opacity: a per-node model value rides the same shape-only validation

EM-012b: "The `ModelPreference` descriptor is opaque to harmonik below the descriptor layer: harmonik validates the **shape** of `model` … not its value. Handler-side launch failure is the authoritative compatibility check." Code: `claudelaunchspec.go:328-334` calls `validateModel`/`validateEffort` (shape: e.g. `≤128 chars`, effort ∈ closed enum) and returns a typed `*ModelPreferenceError` — it does NOT verify the value names a real model. **A per-node `model`/`effort` value is validated by the same `validateModel`/`validateEffort` calls** (it flows into the same `rc.model`/`rc.effort`), so the per-node layer inherits value-opacity for free. `effort` per-node values MUST be drawn from the EM-012b closed enum (`low/medium/high/xhigh/max`); an out-of-enum per-node `effort` should be an ingest-time validation error (the graph is static, so we can fail at load rather than launch — stricter than EM-012b's tier-1 "treat as absent + emit bead_label_conflict", which is a runtime-label path that does not apply to static node attrs).

### F-D6 — Clean-additive; one framing nuance to flag for integration

Additive: new optional attrs on `agentic` (WG-002), new reserved names (WG-031), a new EM-012b tier-0 sub-clause. Schema stays minor / N-1 readable (WG-034) — no new node type, no new edge field, no new enum member. A graph using no per-node `model`/`effort` behaves exactly as today (run-level seal). **Flag for integration:** EM-012b currently says model/effort is "resolved exactly once per run at claim time" and "MUST NOT be re-evaluated for the lifetime of the run." The per-node tier-0 must be worded so it does not *contradict* that invariant — the cleanest framing is that the **per-node value is part of the same single resolution**: at claim time the run seals its run-level `ModelPreference` (the EM-012b walk, unchanged), and per-node attrs are static graph data that *layer on top at dispatch time without a second walk*. Equivalently: EM-012b §4.3 gains a sentence stating the run-level pair is the **default** for every node, and a node's own `model`/`effort` attr (if present) takes precedence for that node only, read from the already-loaded graph (no re-resolution of labels/config/defaults). This wording change is the one spot where the per-node design touches a load-bearing invariant clause — it is a clarification, not a relaxation, but pass-6 integration must land it carefully.

## Patterns to follow

- **Reuse the existing plumbing** (constraint in `01-problem-space.md` §4): do NOT invent a parallel mechanism. The override is a value-substitution at the existing `claudeRunCtx.model/.effort` seam.
- **Reserved-set discipline** (WG-031): any dispatcher-consumed attr (`model`, `effort`) must be added to the reserved set so a misplaced attr is a strict ingest error, not a silently-ignored permissive attr. `class`/`model_stylesheet` stay OUT of the reserved set (permissive/informative).
- **Value-opacity** (EM-012b): validate shape, not value; handler-side launch failure is authoritative.

## Risks / conflicts

- **R-D1 (LOW).** Wording collision with EM-012b's "resolve once, never re-evaluate." Mitigation: frame per-node as static-data layering, not runtime re-resolution (F-D6). Flag for integration.
- **R-D2 (LOW).** `effort` per-node enum-validation point differs from EM-012b's runtime-label path (fail-at-load vs treat-as-absent). Design must state the static-attr path fails at ingest (stricter, simpler for static graphs).
- **R-D3 (INFORMATIONAL).** kilroy `llm_provider` and the fully-qualified model aliases (`claude-opus-4.6`) don't map to harmonik's opaque `--model` aliases. The authoring-convention note must say: port `.hard { llm_model: claude-opus-4.6 }` -> `model="opus"` (or whatever alias the operator's config recognizes), and drop `llm_provider` (handler binding is fixed). No code impact (informative).
- **R-D4 (NONE for parity).** Per-node selection is P3 (quality/cost tuning, not correctness) — neither pipeline *requires* it to run end-to-end; with the stylesheet informative, both pipelines run correctly using the run-level default model for every node. Per-node Opus on hard nodes is a quality upgrade, not a gate.
