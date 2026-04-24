# Harmonik Spec Template

`spec-template-version: 1.1` — last updated 2026-04-24.

> **This file is the normative template every harmonik per-component spec MUST follow.** It is not itself a spec. The reference form for finalized specs is `specs/<component>.md` (single-file) or `specs/<component>/spec.md` + siblings (multi-file, see §Multi-file split).

## How to use this template (authoring-agent checklist)

You are an agent drafting a spec for one of the 10 foundation components (or a subsystem spec). Do these steps in order:

1. Copy this file to `specs/<component>.md` (single-file) OR `specs/<component>/spec.md` (multi-file — see §Multi-file split).
2. Reserve your `requirement-prefix` by adding an entry to `specs/_registry.yaml` (see §Requirement-numbering convention). The registry is the source of truth for prefix uniqueness; the template is not.
3. Fill in the Front matter block. Set `status: draft`. Set `requirement-prefix` to the code you reserved in step 2.
4. Pick a `spec-shape` (`requirements-first` or `taxonomy-first`) — see §Spec-shape selection. Default is `requirements-first`.
5. Work top-to-bottom in the chosen shape's reading order. Do NOT rename required sections. Do NOT change section numbers (they are stable navigational anchors). Optional sections may be omitted when empty — do NOT keep empty placeholders in the final file.
6. Every normative requirement MUST get an ID of the form `<prefix>-NNN` (three digits, zero-padded, assigned in source order, gaps allowed after deletions — never renumber). Example: `EM-001`, `EM-002`, `EM-017`. IDs are mutable while `status: draft`; they freeze permanently at `status: reviewed`.
7. Use RFC 2119 keywords (MUST, MUST NOT, SHOULD, SHOULD NOT, MAY) in all normative statements. Capitalize them. Non-normative prose uses lowercase "must/should/may" or descriptive verbs.
8. Mark non-normative content with the `> INFORMATIVE:` / `> RATIONALE:` / `> EXAMPLE:` blockquote callouts defined in §Normative vs informative markers.
9. Every requirement MUST carry a `Tags:` line. The `Axes:` line is REQUIRED only when the requirement deviates from baseline OR involves LLM invocation, external I/O, state mutation, or non-idempotent side effects — see §Requirement tagging.
10. Cross-reference other specs using the convention in §Cross-reference convention. Do not invent link styles.
11. Fill in Open questions for any deferred decision. Empty = "no open questions" — write that explicitly, do NOT delete the section.
12. Update the Revision history row before every commit that changes normative content.
13. Before declaring the spec `reviewed`, run the conformance checklist at the bottom of this template against the draft and fix every miss.

Required tooling invariants (if you skip these, the spec fails review):

- §1..§12 numbering is fixed. Subsection numbering inside §4 is per-spec.
- No section begins with a heading that is not in the outline unless it is under §A Appendices.
- Every `MUST/SHOULD/MAY` appears within a numbered-requirement block (i.e., not in loose prose). This applies to drafted specs, not to this template's own meta-text.
- Every requirement ID appears exactly once as an anchor (the leading token of the requirement block). References to it elsewhere are unconstrained.

---

## Spec-shape selection

A spec declares its `spec-shape` in front matter. Two shapes are supported:

- **`requirements-first`** (default) — sections appear in the order `0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, A`. Use for specs whose substance is a set of obligations the implementation must satisfy.
- **`taxonomy-first`** — sections appear in the reading order `0, 1, 2, 3, 8, 4, 5, 6, 7, 9, 10, 11, 12, A`. Use when the spec's substance IS a taxonomy and the requirements are "each entry in the taxonomy obeys this rule." Reconciliation (six categories), execution-model (failure classes), and event-model (event taxonomy) are the canonical fits.

Section *numbers* remain stable in both shapes — only the on-page reading order shifts. Cross-references like `[reconciliation.md §8.3]` resolve identically regardless of shape.

> INFORMATIVE: Soft-rule, not soft-discipline. Pick the shape that makes the spec read naturally; do not invert it solely to avoid forward references.

---

## Section outline

Required (R) sections MUST appear in every spec. Optional (O) sections appear only when they have content. The numbers below are stable anchors regardless of `spec-shape`.

| # | Section | Required? |
|---|---|---|
| 0 | Front matter | R |
| 1 | Purpose | R |
| 2 | Scope | R |
| 3 | Glossary | R |
| 4 | Normative requirements | R |
| 5 | Invariants | R |
| 6 | Schemas and data shapes | R (even if "none — see [other-spec.md §N]") |
| 7 | Protocols and state machines | O |
| 8 | Error and failure taxonomy | O |
| 9 | Cross-references | R |
| 10 | Conformance | R |
| 11 | Open questions | R |
| 12 | Revision history | R |
| A | Appendices (examples, counter-examples, rationale) | O |

---

## 0. Front matter

Place this block as the first content after the top-level `# <Spec Title>` H1. It is a fenced YAML block — mechanical validators parse it.

```yaml
---
title: <Human Spec Title, e.g., "Execution Model">
spec-id: <short-slug, e.g., "execution-model">
requirement-prefix: <2-3-letter-code, e.g., "EM">  # MUST appear in specs/_registry.yaml
status: draft              # one of: draft | reviewed | finalized | superseded
spec-shape: requirements-first  # one of: requirements-first | taxonomy-first
version: 0.1.0             # semver; bump on normative change
spec-template-version: 1.1 # the version of this template the spec conforms to
owner: <role or agent, e.g., "foundation-author">
last-updated: <YYYY-MM-DD>
depends-on:                # list of spec-ids this spec normatively depends on
  - architecture
  - event-model
---
```

`status` transitions: `draft` → `reviewed` → `finalized`. `superseded` is a terminal status used only when a spec is replaced wholesale by another.

> INFORMATIVE: There is intentionally no `depended-on-by` field. Reverse dependencies are computed on demand from all specs' `depends-on` lists; storing them invites stale values. The conformance checklist's lint column re-derives the reverse index when needed.

---

## 1. Purpose

One or two paragraphs stating what problem this spec solves and why it is a separate spec rather than content folded into another. SHOULD fit on one screen (≤200 words). MUST name the normative scope in one sentence ("This spec defines…").

> EXAMPLE: "This spec defines the core execution data model — workflow, node, edge, run, state, transition, checkpoint, outcome — and the outcome-spine contract that threads through handler → hook → gate → transition → event. It is normative for every subsystem that produces, consumes, or reasons about runs."

---

## 2. Scope

Two bullet lists. Both subsections MUST be present; use "None." as the single bullet if empty.

### 2.1 In scope

- <one-line item — a capability, contract, or invariant this spec owns>
- <one-line item>

### 2.2 Out of scope

- <one-line item — and WHY it is out of scope, usually naming the spec that owns it instead>
- <one-line item>

> INFORMATIVE: The Out-of-scope list is load-bearing. It prevents reviewers from flagging "missing content" that belongs in another spec.

> INFORMATIVE: DOT workflow files and YAML policy documents are ARTIFACTS governed by specs (e.g., the workflow schema is in [execution-model.md §6]); they are not themselves specs. Shipping a `.dot` or `.yaml` file alongside a spec requires the spec to declare the artifact's schema and versioning rules.

---

## 3. Glossary

Terms introduced or sharpened by this spec. MUST use the definition-list shape below.

- **<term>** — <one-sentence definition>. (see §<section introducing the term>)
- **<term>** — <one-sentence definition>.

Canonical-location rule: a term belongs to the first spec that introduces it. All downstream specs cross-reference via `[other-spec.md §3 <term>]` and MUST NOT redefine. A term with no prior spec is defined in the spec that first needs it.

> INFORMATIVE: Duplicate definitions drift. If you find yourself wanting to redefine a term, cross-reference instead.

---

## 4. Normative requirements

This is the bulk of the spec. Organize as numbered subsections (4.1, 4.2, …) grouped by topic. Every subsection MUST contain at least one requirement block of the shape defined below.

### 4.N Requirement-block shape

Every requirement MUST be written as a block with this exact structure:

```
#### <prefix>-NNN — <short requirement title>

<Normative sentence(s) using MUST/SHOULD/MAY.>

Tags: <mechanism | cognition>
Axes: llm-freedom=<value>; io-determinism=<value>; replay-safety=<value>; idempotency=<value>
```

The `Axes:` line is OPTIONAL per the rules in §4.N+1. The `Tags:` line is REQUIRED on every requirement.

> EXAMPLE:
>
> #### EM-012 — Checkpoint commit carries transition trailers
>
> Every checkpoint commit MUST carry the trailers `Harmonik-Run-ID`, `Harmonik-State-ID`, `Harmonik-Transition-ID`, and `Harmonik-Schema-Version`. The trailer `Harmonik-Bead-ID` MUST be present when the run is tied to a bead (see [beads-integration.md §10.6]) and MUST be absent otherwise.
>
> Tags: mechanism

> EXAMPLE (axes line present because the requirement performs external I/O):
>
> #### EM-018 — Outcome event flush on transition
>
> The orchestrator MUST flush the JSONL outcome event to disk before advancing the run state.
>
> Tags: mechanism
> Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.N+1 Requirement tagging

Each requirement MUST carry a `Tags:` line. The `Axes:` line is REQUIRED only when the requirement deviates from baseline OR involves LLM invocation, external I/O, state mutation, or non-idempotent side effects.

**Baseline (omitted `Axes:` line implies these values):**

```
llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
```

**Required-Axes triggers** — an `Axes:` line MUST be present when any of the following hold:

- Any axis value differs from baseline.
- The requirement invokes an LLM (mechanism-tagged or cognition-tagged dispatching).
- The requirement performs external I/O beyond pure CPU compute (file write, network call, process spawn).
- The requirement mutates persistent state (git, JSONL, Beads SQLite, workspace files).
- The requirement is non-idempotent or recoverable-non-idempotent.

**Exemption — declaration-only requirements.** Requirements that declare a structural fact ("RunID is a 128-bit value", "Handler is the interface defined in §6.1") MAY omit the `Axes:` line entirely. They have no runtime behavior to classify.

**Single-tag rule.** `Tags:` carries exactly ONE of `mechanism` or `cognition`. Combined `mechanism, cognition` tags are forbidden. If a requirement describes both a mechanism surface and a cognition surface, split it into two requirements. Example: "the daemon deterministically dispatches a reviewer agent" splits into (a) the dispatch mechanism (mechanism-tagged) and (b) the agent reasoning contract (cognition-tagged).

**Cognition-tagged requirements** MUST name the delegation path in their body: which role, which model-class, what input shape. A reviewer (not a linter) verifies the path is correctly named.

**Tag grammar (committed):**

- `Tags:` line — exactly one of `mechanism` or `cognition`. No commas, no pipes, no plurals.
- `Axes:` line — four axis assignments in FIXED ORDER: `llm-freedom`, `io-determinism`, `replay-safety`, `idempotency`. Separator: semicolon followed by a single space (`; `). Each assignment is `<name>=<token>` with no spaces around `=`. All lowercase.
- Whitespace — one blank line above `Tags:`. No blank line between `Tags:` and `Axes:` when both are present.
- Axis tokens (lowercase, hyphen-separated):
  - `llm-freedom ∈ {none, bounded, unbounded}`
  - `io-determinism ∈ {deterministic, best-effort, nondeterministic}`
  - `replay-safety ∈ {safe, unsafe, n/a}`
  - `idempotency ∈ {idempotent, non-idempotent, recoverable-non-idempotent, n/a}`

> INFORMATIVE: The present-tense `Axes:` line now means "this requirement is interesting." Authors who tag reflexively will produce nothing because baseline-shaped requirements omit the line entirely. Reviewers can scan for the present line to find the load-bearing surface.

### 4.N+2 Requirement grouping (recommended pattern)

Group requirements under topical subsections:

- `4.1 <Topic A>` — requirements <prefix>-001 … <prefix>-NNN
- `4.2 <Topic B>` — requirements <prefix>-NNN+1 … <prefix>-MMM
- …

The grouping is informative; the requirement IDs are the stable handles. Subsection numbering under §4 is per-spec; §4.1 in one spec is unrelated to §4.1 in another.

---

## 5. Invariants

Invariants are normative statements that span multiple subsystems and constrain more than one spec's requirements. Write them as `<prefix>-INV-NNN` blocks; tag rules from §4.N+1 apply.

**Selection test.** An invariant is a system-wide property that constrains multiple subsystems' requirements. If the rule fits inside one subsystem's §4 without reference to others, it is a requirement, not an invariant. If you write the same rule as both, delete the §4 copy.

> EXAMPLE:
>
> #### EM-INV-001 — Git is the state-reconstruction source
>
> The git checkpoint trail MUST be sufficient, on its own together with the Beads store, to reconstruct any run's current durable state. JSONL event replay MUST NOT be used for state reconstruction. (See [event-model.md §3.6].)
>
> Tags: mechanism

Invariants may also be phrased as tables when they map enumerated inputs to enumerated outcomes (see §6.3).

---

## 6. Schemas and data shapes

Schemas live here, inline, unless the spec is split (see §Multi-file split). Every schema MUST use one of the presentations below.

### 6.1 Pseudocode record and interface schemas

Use `RECORD` for data types introduced by this spec:

```
RECORD <TypeName>:
    <field_name>   : <Type>               -- <one-line description>
    <field_name>   : <Type> | None        -- <optional, default: <value>>
    <field_name>   : List<<ElementType>>  -- <one-line description>
```

Use `INTERFACE` for method-bearing contracts (Go interfaces, consumer-facing surfaces):

```
INTERFACE <TypeName>:
    <Method>(<args>) -> (<results>)  -- <one-line semantic contract>
    <Method>(<args>) -> error        -- <contract; idempotent on (<key fields>)>
```

> EXAMPLE:
>
> ```
> INTERFACE Handler:
>     Launch(ctx, spec) -> (Session, error)  -- starts an agent process; idempotent on (run_id, node_id)
>     Kill(ctx, session, signal) -> error    -- sends signal to agent process
>     Wait(ctx, session) -> (Outcome, error) -- blocks until the session terminates; safe to call multiple times
> ```

Each `INTERFACE` method MAY be cross-referenced by a requirement ID elsewhere in the spec.

Types in use: `String`, `Integer`, `Bool`, `UUID`, `Timestamp`, `Bytes`, `List<T>`, `Map<K, V>`, plus spec-defined types and the conventional Go-context arg `ctx`.

### 6.2 YAML / JSON schema snippets

Use for wire formats, on-disk formats, or policy documents. Fenced code blocks, language tag set correctly:

```yaml
<field>: <type>
  # <description>
  # Default: <value>
```

```json
{
  "<field>": "<type>",
  "<field>": "<type>"
}
```

### 6.3 Tabular schemas

Use for enumerations, category taxonomies, action maps, and state-transition rules. Every column MUST have a header; every cell MUST have a value (use `—` for "not applicable").

> EXAMPLE (category × action, reconciliation-style):
>
> | Category | Detector rule | Default action | Investigator? |
> |---|---|---|---|
> | Cat 1 | <rule> | auto-resume | No |
> | Cat 2 | <rule> | investigator workflow | Yes |

### 6.4 Schema evolution

If any schema in this spec is versioned across releases, this subsection MUST state the compatibility contract (typically "N-1 readable" — see [operator-nfr.md §7.5]) and name the version field.

### 6.5 Co-owned event payloads

When a spec EMITS an event whose payload schema is REGISTERED in [event-model.md §3.2], list the event under §6 with a one-line emission rule and a pointer to the payload schema, e.g.:

- `agent_ready` — emitted on first heartbeat from a launched session; payload schema in [event-model.md §3.2].

The emitting spec is normative for the *when*; event-model is normative for the *shape*.

---

## 7. Protocols and state machines

Optional. Use when this spec defines an interaction protocol (agent ↔ daemon, subsystem ↔ subsystem, CLI ↔ service) or a lifecycle state machine (daemon status, run status, reconciliation flow).

### 7.1 State machine

Document the states, transitions, and guards in one of these forms (the choice is editorial — pick the form that reads clearly for the machine in hand):

- Pseudocode transition functions — preferred for complex guards or branching.
- A transition table `| From | Event | Guard | To | Emits |` — preferred for small, dense state spaces (≤8 states, ≤20 transitions).
- An ASCII diagram — only when the table or pseudocode is ambiguous AND the spatial layout adds information.

### 7.2 Protocol pseudocode

Use pseudocode for sequential protocols (launch handshake, commit-and-emit sequence). Shape:

```
FUNCTION <protocol_name>(<inputs>):
    <step 1>
    IF <condition>:
        <branch>
    RETURN <outcome>
```

Every branch point MUST have a corresponding requirement ID.

---

## 8. Error and failure taxonomy

Optional. Use when this spec enumerates error classes the rest of the system routes on. Structure:

- List each failure class as a numbered subsection (`8.1 <class>`).
- For each class: detection rule (mechanism-tagged), default response, escalation path, emitted event type.

In a `taxonomy-first` spec (see §Spec-shape selection) §8 is read before §4, but §8's section number does not change. Reconciliation-category-style taxonomies belong here; see §6.3 for tabular form.

---

## 9. Cross-references

Three subsections.

### 9.1 Depends on

List every spec this one normatively depends on, with the sections cited and the reason. Shape:

- **[architecture.md §1.1]** — four-axis classification test; every requirement in this spec uses the axes defined there.
- **[event-model.md §3.2]** — event taxonomy; this spec's events are declared there.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand by walking every spec's `depends-on` list. They are NOT stored in front matter (see §0). When a reviewer needs the reverse index, the lint check produces it from the corpus; it is not maintained here.

### 9.3 Co-references (read-only consumption)

For one-way consumption relationships — this spec READS FROM another spec's declared surface but does NOT normatively depend on its internals. Co-references do not appear in `depends-on`. Shape:

- **[control-points.md §6.11 Skill declaration]** — this spec consumes the skill-declaration surface declared there; no reverse dependency.

> INFORMATIVE: Use this when the relationship is "I read X from spec Y" without committing your spec to Y's full contract surface. Symmetric `depends-on` over-states the coupling.

---

## 10. Conformance

What does it mean for an implementation to conform to this spec? Required shape:

### 10.1 Conformance profiles

List each profile (e.g., "Core MVH", "Extension X"). For each: which requirements (by ID) MUST pass, which MAY be deferred.

### 10.2 Test-surface obligations

For each profile, the normative test obligations the implementation MUST satisfy. Cite the test layer ([testing.md §<layer>]) and the requirement IDs each test proves.

> INFORMATIVE: During bootstrap (before [testing.md] exists) cite the test obligation in prose and add an Open Question linking to it. Migrate to the spec reference within one revision cycle once testing.md lands.

### 10.3 Excluded conformance claims

Aspects this spec explicitly does NOT grant conformance over (post-MVH features, external-system guarantees, etc.).

---

## 11. Open questions

Every deferred decision MUST be listed here. If there are none, state "None." explicitly — do not delete the section.

### 11.N Shape

```
#### OQ-<prefix>-NNN — <short title>

Question: <what must be decided>
Owner: <role or agent who decides>
Blocks: <requirement ID(s) that cannot be finalized until this resolves, or "none">
Default-if-unresolved: <what the spec assumes until decided>
```

> EXAMPLE:
>
> #### OQ-EM-001 — Failure-commit policy for `git bisect` over failures
>
> Question: Should failed transitions emit checkpoint commits to enable `git bisect` in the improvement loop?
> Owner: foundation-author
> Blocks: none (MVH decision: no failure commits)
> Default-if-unresolved: No failure commits; revisit when improvement-loop spec lands.

---

## 12. Revision history

Append-only. One row per normative change (additions, deletions, semantic edits). Typo-only edits do not require a row.

| Date | Version | Author | Summary |
|---|---|---|---|
| YYYY-MM-DD | 0.1.0 | <author> | Initial draft. |

---

## A. Appendices (optional)

Use for content that would bloat the normative body: worked examples, counter-examples, design rationale, migration notes. Non-normative by default — being under §A carries the non-normative signal; no per-appendix marker is required.

Appendix numbers are reserved slots:

- **A.1 Examples** — complete worked scenarios.
- **A.2 Counter-examples** — things that LOOK conformant but are not, and why.
- **A.3 Rationale** — design-decision narrative. Point to problem-space.md and kerf work artifacts rather than rewriting them.
- **A.4 Migration notes** — when replacing a prior spec, how to migrate.

Gaps are legal. A spec with only a rationale appendix uses **A.3** directly, not A.1. New appendix kinds claim the next unused number (A.5+).

When an appendix exceeds ~200 lines, move it to a sibling file per §Multi-file split. Longer worked examples for this template itself (the few that authors keep asking for) live in a supplement authored at first need: `docs/foundation/spec-template-examples.md` (file does not exist yet — create on first use).

---

## Normative vs informative markers

Use these blockquote callouts to mark non-normative content inline:

- `> INFORMATIVE: <text>` — general non-normative elaboration.
- `> RATIONALE: <text>` — why a requirement is shaped the way it is; belongs in §A.3 when long.
- `> EXAMPLE: <text or code>` — worked illustration; belongs in §A.1 when long.
- `> NOTE: <text>` — reserved for short operator-facing or implementer-facing hints.

Non-normative callouts MUST NOT contain `MUST/SHOULD/MAY` keywords. If you wrote a MUST inside an INFORMATIVE block, you have a bug — move it into a real requirement.

---

## Cross-reference convention

Exactly one form is allowed across all harmonik specs:

- **Inter-spec**: `[spec-id.md §N.N]` — brackets include the filename and the section number. Example: `[execution-model.md §2.1]`.
- **Intra-spec**: `§N.N` — no filename, no brackets. Example: `§4.3`.
- **Nested numbering**: `§N.N.N` is allowed for sub-sub-sections (e.g., `§6.1.2`). Letter suffixes inside parentheses (`§4.2(b)`) are forbidden — promote to a numbered sub-sub-section instead.
- **Requirement ID**: `<prefix>-NNN` — no brackets, no section. Example: `EM-012`. A requirement reference MAY be followed by a parenthetical section hint: `EM-012 (§4.2)`.
- **Cite a type defined in another spec**: `[other-spec.md §N <TypeName>]` — bracket includes the section the type is defined in. Example: `[execution-model.md §6.1 Outcome]`.
- **External URL**: full URL in `<>`. Example: `<https://example.com>`.
- **Bootstrap-only — citing foundation docs**: until all 10 foundation specs exist, citing `[docs/foundation/components.md §<N>]` is allowed as a transition-period source. Once the target spec is finalized, the citation MUST migrate to the spec reference within one revision cycle.

Forbidden forms: markdown reverse-links (`[text](#anchor)`), page numbers, "see above/below" without a number, relative paths (`../docs/foo.md`).

> INFORMATIVE: Linkable anchors are fragile across editors and renderers. Section numbers are stable because this template pins the numbering scheme.

---

## Requirement-numbering convention

- Each spec reserves a **2–3 letter uppercase prefix** declared in front matter (`requirement-prefix`). The source of truth for prefix uniqueness is **`specs/_registry.yaml`** at the repo root. The template is NOT the registry.
- To reserve a prefix: add an entry to `specs/_registry.yaml` (one line: `XX: {spec-id: <slug>, reserved: <YYYY-MM-DD>, status: draft}`) in the same commit that introduces the spec.
- Requirements are numbered `<prefix>-NNN` (three digits, zero-padded), assigned in source order.
- Invariants are numbered `<prefix>-INV-NNN`.
- Open questions are numbered `OQ-<prefix>-NNN`.
- IDs are **mutable while `status: draft`** — feel free to renumber while drafting.
- IDs **freeze permanently at `status: reviewed`**. After freeze, gaps are allowed when a requirement is deleted; never renumber and never reuse an ID.
- Subsections do not carry IDs; only requirement/invariant/open-question blocks do.

---

## Template-version policy

This template is itself versioned (`spec-template-version` in spec front matter).

- A bump from `N.N` to `N.(N+1)` is **additive**: existing specs may continue to declare the older template version; migrating to the new version MAY be required at the next finalize, but is not required mid-cycle.
- A bump from `N.N` to `(N+1).0` is **breaking**: existing specs MUST migrate at the next normative edit and MUST bump their own `version`.
- Pinning a spec to an older template version is ALLOWED until the next finalize, then re-evaluated.

---

## Multi-file split (for specs exceeding ~1000 lines)

A spec SHOULD split into sibling files when its single-file form exceeds ~1000 lines. The split structure is:

```
specs/<spec-id>/
  spec.md         # normative shell — front matter + §§1–12 + pointers into siblings
  schemas.md      # §6 content, when §6 exceeds ~300 lines
  protocols.md    # §7 content, when §7 exceeds ~300 lines
  examples.md     # §A.1 content
  rationale.md    # §A.3 content
  migration.md    # §A.4 content, when present
```

Rules for a split spec:

1. `spec.md` keeps the full front matter, §§1–5, §9–12, and a **stub** for §6/§7/§A-* sections that points into siblings. Stub shape:

   ```markdown
   ## 6. Schemas and data shapes

   Full schemas live in [schemas.md](schemas.md). This subsection lists the schema index and the compatibility contract; concrete records are in the sibling.

   - `<TypeName>` — [schemas.md §6.1]
   - `<TypeName>` — [schemas.md §6.2]

   Schema evolution: …
   ```

2. Sibling files are non-normative in their own front matter (`status: supplement`) but MUST carry the same `spec-id` so the linter treats them as one spec.

3. Cross-references into siblings use the same convention as inter-spec references: `[schemas.md §6.2]` when the cite is from `spec.md`; `[spec.md §4.3]` when the cite is from a sibling.

4. Requirement IDs are spec-wide, not per-file. `EM-042` may appear in any sibling; each ID is unique across the split.

5. A split spec's revision history lives in `spec.md`; siblings note their own last-updated timestamps in their front matter.

> RATIONALE: The split is by content type (schemas, protocols, examples, rationale), not by topic. This keeps the normative shell scannable and moves bulky material out of the review path for small changes. Alternative splits (by topic) were considered and rejected because they fragment the requirement-ID space in ways that make cross-spec review harder.

---

## Conformance checklist (run before marking `reviewed`)

The drafting agent works through this checklist before advancing the spec's status. Reviewers verify. The checklist is split into two columns: **lint-enforced** items can be regex-proved by tooling; **reviewer-enforced** items require semantic judgment by a human or reviewer agent.

### Lint-enforced (a regex/parser can prove pass/fail)

- [ ] Front matter is a valid YAML block with all required fields populated.
- [ ] `requirement-prefix` from front matter exists in `specs/_registry.yaml`.
- [ ] `spec-shape` field is one of `requirements-first` or `taxonomy-first`.
- [ ] `spec-template-version` matches a published template version.
- [ ] No `depended-on-by` field is present in front matter (reverse index is computed, not stored).
- [ ] §§1, 2, 3, 4, 5, 6, 9, 10, 11, 12 are all present (sections are recognized by their numbered headings).
- [ ] Every requirement block carries a `<prefix>-NNN` anchor heading and a `Tags:` line. `Axes:` line, when present, follows the grammar in §4.N+1 (fixed axis order, lowercase tokens, `; ` separator, no spaces around `=`).
- [ ] `Tags:` value is exactly one of `mechanism` or `cognition` (no commas, no plurals).
- [ ] Every cross-reference matches one of the forms in §Cross-reference convention. No markdown reverse-links (`[text](#anchor)`). No relative paths (`../docs/foo.md`).
- [ ] Revision history has at least one row.
- [ ] Total line count is under ~1000. If over, the spec is expected to be split per §Multi-file split (warning, not blocker).
- [ ] No `TODO` / `TBD` / `FIXME` tokens appear in the draft.
- [ ] Each requirement ID appears exactly once as an anchor (the leading token of a requirement block).
- [ ] Each spec listed in `depends-on` exists under `specs/`.

### Reviewer-enforced (requires reading and judgment)

- [ ] Required-but-empty sections actually say "None." explicitly AND the author confirms the section truly has no content (the literal "None." is regex-checkable; whether it is honest is not).
- [ ] Every `MUST / SHOULD / MAY` keyword appears inside a requirement/invariant block, not in loose prose or informative callouts. (Markdown scope-tracking is fragile; treat lint as advisory and have a reviewer confirm.)
- [ ] Every `cognition`-tagged requirement names the delegation path correctly (which role, which model-class, what input shape — gestures-at-a-path is not enough).
- [ ] Every requirement that performs LLM invocation, external I/O, state mutation, or non-idempotent side effects carries an `Axes:` line. (Detection of "involves I/O" requires reading.)
- [ ] Out-of-scope items each name a WHY (typically the spec that owns the excluded content).
- [ ] Glossary entries do not redefine terms already defined in another spec — instead they cross-reference per §3.
- [ ] `depends-on` accurately reflects the cross-references actually used in the body.
- [ ] Every appendix is genuinely non-normative content; nothing normative hides under §A.
- [ ] Open questions list every deferred decision (or says "None.").
- [ ] Conformance profiles' deferred-requirement lists are honest about what is excluded.

If any item is unchecked, the spec is not ready for review.

---

## Revision history (this template)

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-04-23 | 1.0 | foundation-author | Initial template. |
| 2026-04-24 | 1.1 | foundation-author | Integrated implementer + critic reviews: lint vs reviewer column split; default-baseline Axes line; declaration-only Axes exemption; single-tag split rule; INTERFACE schema shape; `specs/_registry.yaml` extraction; `spec-shape` (requirements-first / taxonomy-first); dropped `depended-on-by`; §9.3 Co-references; invariant-vs-requirement selection test; ID-freeze at `reviewed` not `draft`; glossary canonical-location rule; appendix reserved-number slots; template-version policy; DOT/YAML artifact note; bootstrap citation form; dropped per-appendix `> INFORMATIVE:` mandate; nested `§N.N.N` cross-ref form; type-cite form; co-owned event payload pattern (§6.5). |
