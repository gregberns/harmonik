# Critic Review — Spec Template v1.0

## Verdict summary

The template is ~75% correctly rigorous and ~25% ceremony masquerading as rigor. The overall shape (RFC 2119 + numbered outline + requirement IDs + single cross-reference convention + the multi-file split) is solid and will survive contact with ten very different components. But the four-axis tag mandate on *every* requirement, the "mechanical linter checks this" framing on checks that are actually semantic, and the "do NOT reorder sections" rigidity applied uniformly to taxonomy-heavy and protocol-heavy specs — these are places where the template picked the first plausible answer and calcified it. The template will work; it will also produce a lot of `n/a`s and a lot of tickets for a linter that can't be written as promised.

## Challenges I want the template author to see

### 1. The four-axis tag on every requirement is ceremony for ~40% of requirements

- **Challenge** — Mandating all four axis values on every requirement produces `n/a` or `none` on three of four axes for a large class of requirements (definitional, structural, schema-shape), with no reader benefit.

- **What the template says** — "Every requirement MUST carry its mechanism/cognition tag AND its four-axis determinism tag per §Requirement tagging. No exceptions." plus:
  ```
  Axes: llm-freedom=<value>; io-determinism=<value>; replay-safety=<value>; idempotency=<value>
  ```
  with no option to omit.

- **Is the justification adequate?** — The reference point cited (architecture §1.1) *needs* every operation classified. But purely-structural requirements inherit nothing useful from a four-axis tag. Concrete cases from the content:
  - **Reconciliation §9.3 Cat 0 pre-check** — "daemon verifies `br --version` returns within timeout T" produces `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent`. Every detector in §9.3 produces the same four-tuple.
  - **Execution-model §2.1 `workflow_id` field-shape requirement** — axes are all `n/a` except `llm-freedom=none`. Three of four slots are ceremony.
  - **Event-model §3.1 "`event_id` is UUID v7"** — same all-baseline shape.

  The one informative slot (mechanism vs cognition) is already covered by the `Tags:` line. The implicit author argument "readers can scan the tag for surprise" is weaker than it sounds: if 70%+ of tags are identical baseline, the *surprise* tag buries itself in noise.

- **What the stronger alternative looks like** — Default-baseline rule: "a requirement without an explicit `Axes:` line inherits `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent`. Axes line is required ONLY when the requirement's classification deviates from baseline, OR when the requirement involves LLM invocation, external I/O, state mutation, or non-idempotent side effects." This flips the default to the common case, forces authors to write axes only when they matter, and makes the tag scannable again — a present `Axes:` line means *something interesting*.

- **How load-bearing** — Important. Not blocking, but the volume of boilerplate is going to train authors to tag reflexively without thought, which is the failure mode the user cares about most.

### 2. "Mechanical linters check this" is promised for several checks that require semantic judgment

- **Challenge** — The conformance checklist and §4.N+1 claim mechanical verification for rules that are actually semantic. A linter cannot verify them as stated, so the "enforced" framing is false comfort — exactly the failure mode the user named up front.

- **What the template says** — §4.N+1: "Axis values use exactly the enum tokens above (lowercase, hyphen-separated). Mechanical linters check this." Checklist items include:
  - "Every `cognition`-tagged requirement names the delegation path."
  - "Every `MUST / SHOULD / MAY` keyword appears inside a requirement/invariant block, not in loose prose or informative callouts."
  - "No markdown reverse-links."
  - "Total line count is under ~1000."

- **Is the justification adequate?** — Some of these *are* mechanical (enum tokens, reverse-link regex, line count). Others are not:

  - **"Cognition-tagged requirement names the delegation path."** A linter can check that a cognition-tagged block contains *words that look like* a delegation reference. It cannot verify the path is *correctly named* or *actually references a valid role/model-class/input-shape*. Gesture-at-a-path passes; actually-name-a-path requires a human reviewer. Concrete case: control-points §6.1a specifies Gate evaluators as "Mechanism OR cognition (allowed to delegate)." A requirement like "the evaluator MAY delegate to a reviewer agent" passes a naïve "contains the word `agent`" check while saying nothing about which reviewer, under what prompt, against what input.

  - **"Every MUST appears inside a requirement block, not loose prose."** Markdown structure is easy to parse, but "loose prose" is not: a MUST inside a table cell (perfectly legal in §6.3), a MUST in a list item under §5, a MUST in a numbered sub-bullet — these are hard to distinguish mechanically without committing to one fixed requirement-block regex. The template does commit to `#### <prefix>-NNN —` as the anchor, but requirement content can legally contain multiple MUSTs and the linter has to scope-track.

  - **"Every requirement block carries `Tags:` and `Axes:` lines."** This one IS mechanical. Good.

- **What the stronger alternative looks like** — Split the conformance checklist into two columns:
  - **Lint-enforced** (regex-provable): front-matter YAML valid, enum tokens match, `<prefix>-NNN` format, reverse-links absent, line count, Tags+Axes line present.
  - **Reviewer-enforced** (semantic): delegation path correctness, out-of-scope-item-actually-justifies-why, requirement-is-normative-rather-than-prose-with-MUST-in-it.

  Stop calling the second column "mechanical." This matters because the user explicitly fears rules that sound enforced but aren't — this is exactly that failure mode written down.

- **How load-bearing** — Blocking. The template's credibility rests on authors trusting the "enforced" label. If a reviewer catches a miss the template promised the linter would catch, trust in the template collapses.

### 3. The 12-section outline doesn't fit taxonomy-heavy specs without distortion — specifically reconciliation

- **Challenge** — "Do NOT reorder sections, do NOT rename required sections" is sensible for most components but makes reconciliation.md awkward: its natural shape is taxonomy-first (6 categories → detectors → verdicts → execution), not requirements-first.

- **What the template says** — §Section outline lists §4 Normative requirements before §6 Schemas, §7 Protocols, §8 Error taxonomy. Under the template, reconciliation's six-category taxonomy lives in §8 (error/failure taxonomy) and its requirements live in §4 — but §4 can't be written without the taxonomy already in hand, so §4 requirements will forward-reference §8 constantly.

- **Is the justification adequate?** — Components.md §9.2a has a single table carrying the load-bearing content of the whole spec (6 categories × 5 columns). Under the template that table goes in §8 per the "Reconciliation-category-style taxonomies belong here" note, but the actual requirements (Cat 0 pre-check procedure, Cat 3b auto-resolver, verdict-staleness check) are in §4. A reader trying to understand "what is Cat 3b?" must jump §4.3 → §8.3 → §4.5 → §6.3 (for the `store_divergence_detected` event schema). This is exactly the reading pattern the numbered outline is supposed to prevent.

  Similar but less acute: execution-model §2.3 failure taxonomy (six failure classes) and event-model §3.2 event taxonomy (~35 types) also live awkwardly under the "§4 requirements → §6 schemas → §8 taxonomies" ordering — the taxonomy IS the substance, and the requirements are "each entry in the taxonomy obeys this rule." Inverting that is natural for these three specs.

- **What the stronger alternative looks like** — Soften "do NOT reorder" to "do NOT rename, and preserve the section *numbers* as stable anchors, but sections MAY appear in taxonomy-first order when the spec's substance IS a taxonomy." Concretely: allow a spec's flow to be §3 → §8 → §4 → §6 → etc., while keeping numbers as navigational handles. Or introduce an explicit "taxonomy-first" spec shape as a second supported outline, chosen in front matter (`spec-shape: requirements-first | taxonomy-first`). The numbered-anchor discipline survives; the reading order adapts to content.

- **How load-bearing** — Important. Not blocking — reconciliation *can* be written under the current outline — but it will read worse than it should, and future taxonomy-shaped specs (new failure-category, new verdict-vocabulary post-MVH) will inherit the distortion.

### 4. Spec-wide requirement IDs with a hand-maintained prefix registry is a coordination problem waiting to happen

- **Challenge** — "Prefixes are globally unique. Current reservations: AR, EM, EV, HC, WM, CP, ON, PL, RC, BI" lives only in the template file. There is no separate registry; the first subsystem spec to be drafted collides with nothing, the fifth one silently picks a taken prefix if someone edits out of sync, and the template doesn't say who owns the registry or how new specs check uniqueness.

- **What the template says** — §Requirement-numbering convention: "Each spec reserves a 2–3 letter uppercase prefix declared in front matter. Prefixes are globally unique. Current reservations: …" No registry file, no CI check, no authority named. Checklist says: "`requirement-prefix` is globally unique (not already claimed by another spec)" — without saying *how* to check.

- **Is the justification adequate?** — For 10 foundation components authored in one kerf pass, the prefix list fits in the template. For the post-MVH world where subsystem specs are authored asynchronously by different agents, the template *is* the registry, which means every new spec must edit the template to claim a prefix, which means template edits and spec additions are coupled. That's a coordination bottleneck and a merge-conflict hotspot. The drafting agent has no mechanical way to verify uniqueness short of grepping across `specs/` and hoping nothing unmerged conflicts.

- **What the stronger alternative looks like** — One of:
  - (a) A separate file `specs/_registry.yaml` listing every reserved prefix with its spec-id and last-updated date, plus a lint check that each `spec.md`'s front matter prefix appears in it.
  - (b) A deterministic prefix-generation rule (e.g., "first two consonants of spec-id, uppercase") so two agents never collide without coordination.
  - (c) A namespace rule per spec family: foundation specs use a single `F-NNN` space, subsystem specs use `S01-NNN` tied to subsystem ID.

  Any of the three is a cleaner long-term shape than "register by editing the template." The current design is a classic first-plausible-answer.

- **How load-bearing** — Important. Blocking for post-MVH. Not blocking for the 10 foundation specs because they're being authored together.

### 5. The "Tags:" plus "Axes:" syntax is a mini-DSL with no parser spec

- **Challenge** — The template defines a per-requirement tag syntax (`Tags: mechanism | cognition` and `Axes: llm-freedom=…; io-determinism=…; …`) and asserts a linter parses it, but gives no grammar — whitespace rules, value-ordering requirements, what happens if an author reorders axes, whether `Tags: mechanism, cognition` (comma) is legal vs. `Tags: mechanism | cognition` (pipe), whether blank lines between `Tags:` and `Axes:` are allowed.

- **What the template says** — Shows one example: `Tags: mechanism` and `Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent`. That's it. No grammar, no ordering rule, no whitespace rule.

- **Is the justification adequate?** — For a hand-written spec corpus of 10 files, humans will probably do the right thing. But this is exactly the surface a drafting-agent fills in programmatically, and drafting agents *will* vary format (I have seen it happen — different models emit semicolons-then-spaces vs. semicolons-no-space vs. newline-separated). The template asserts mechanical enforcement; mechanical enforcement requires a grammar.

- **What the stronger alternative looks like** — Either:
  - (a) Embed tags as YAML inside the requirement block (`tags: [mechanism]` / `axes: {llm-freedom: none, …}`) which inherits YAML's parser, OR
  - (b) Write the grammar as a BNF/regex in the template itself with an explicit statement that axis ordering is fixed.

  Pick one and commit.

- **How load-bearing** — Nice-to-have for first-pass specs; Important once linters get written. A template that expects mechanical tooling should provide the grammar that tooling needs.

## Over-engineering flagged

- **Every requirement has its own `#### <prefix>-NNN — <title>` H4 heading.** Including invariants (`<prefix>-INV-NNN`) and open questions (`OQ-<prefix>-NNN`). Across 10 foundation components that's roughly 300-500 H4 headings. Markdown tables of contents become unreadable; `grep <prefix>-NNN` works fine, but the TOC-as-navigation path the template advertises fights the density of the content. A running-text requirement format with the ID as a bold inline marker (`**EM-012** Every checkpoint commit MUST…`) would be denser and still lintable.

- **Appendices have their own marker mandate (`> INFORMATIVE:`) at the top of each one.** §A already being under the "Appendices (examples, counter-examples, rationale)" header carries the non-normative signal. Requiring the callout is belt-and-suspenders. A reviewer-agent flag would suffice.

- **The `status: draft | reviewed | finalized | superseded` progression.** Four states where three would do (draft → finalized → superseded; the "reviewed" intermediate exists only to mark review completion, which git history also records). Minor, but a first-plausible-answer.

- **`depended-on-by` in front matter, "maintained mechanically."** This is a cache that can drift. Better to not store it at all and compute on demand from all specs' `depends-on` fields. The current shape invites a stale value to be checked in.

- **"Every `MUST/SHOULD/MAY` appears within a numbered-requirement block."** Combined with the requirement of `<prefix>-NNN` uniqueness and the mandated `Tags:`+`Axes:` lines, a three-word operational rule like "all agent subprocess output MUST be UTF-8" bloats into ~5 lines of markdown. For a spec with 40+ requirements this is real reader friction.

## Rules that LOOK enforced but aren't mechanical

From the conformance checklist, these require semantic judgment despite the "mechanical linter" framing elsewhere in the template:

- **"Every `cognition`-tagged requirement names the delegation path."** — Regex can detect text presence; it cannot verify path correctness. **Semantic.**
- **"Every `MUST / SHOULD / MAY` keyword appears inside a requirement/invariant block, not in loose prose."** — Scope-tracking through nested markdown is parser-complete; corner cases (MUST in a table cell, MUST in a requirement block's internal example) trip a naïve parser. **Partially mechanical, fragile.**
- **"Required-but-empty sections say 'None.' explicitly."** — "None." is detectable; whether the section actually HAS no content or the author wrote "None." while eliding real content requires reading. **Semantic.**
- **"No `TODO / TBD / FIXME` tokens."** — Trivially mechanical. This one IS enforced.
- **"All appendices carry the `> INFORMATIVE:` marker at their top."** — Mechanical given the parser knows which headings are appendices. Borderline.
- **"Requirement IDs appear exactly once as an anchor and zero or more times as references elsewhere."** — The "exactly once as anchor" is mechanical; the "zero or more times as references" is meaningless as a check. Half-mechanical.
- **"Every requirement block carries `<prefix>-NNN`, `Tags:`, `Axes:` lines."** — Mechanical with the grammar spec (see challenge 5). Without grammar, brittle.

Count of items promised-but-not-actually-mechanical: **4** (delegation path, MUST-in-prose, None.-really-empty, Axes-line-pre-grammar). The remaining checklist items are genuinely enforceable.

## Affirmations

- **RFC 2119 keyword discipline + capitalized MUST/SHOULD/MAY only in normative blocks.** Standard, well-scoped. The separation between normative prose and informative callouts (`> INFORMATIVE:`, `> RATIONALE:`) is clean and will survive.

- **Single cross-reference form (`[spec-id.md §N]` inter-spec, `§N` intra-spec).** The ban on markdown anchors is correct — anchors drift across editors; section numbers pinned by the template are stable. Forbidding relative paths is also right.

- **Multi-file split by content-type over by-topic.** For 9 of 10 foundation components this is correct. Split-by-topic fragments the requirement-ID space across files, which is exactly the problem. The one component where split-by-topic might serve better (reconciliation, where §8 taxonomy is the spec's center of gravity) — honestly, even there, split-by-content-type with the normative shell in `spec.md` and the taxonomy table in `schemas.md` is probably still better than a topic split.

- **Requirement IDs are permanent and never reused.** This is the invariant that makes cross-spec references stable over time. Retiring an ID rather than renumbering is the right call.

- **Out-of-scope list with WHY each item is out of scope.** Small but load-bearing — this is the discipline that stops reviewers from flagging "missing content" and redirects them to the owning spec.

## Hidden assumptions

- **Authors and reviewers are the same population.** The template reads as if one agent drafts and one agent reviews. In practice the draft agent may be a high-capability model and the review agent a constrained-budget lighter model (or vice versa). The assumption that both parties parse the tagging DSL identically is not stated.

- **Specs are stable in count and identity.** The template accommodates adding requirements to an existing spec cleanly, but adding a whole new spec means registering a new prefix (challenge 4) and the template itself becomes a coordination artifact. For MVH this is fine; post-MVH it is not.

- **The spec corpus will never exceed a human's ability to hold prefix reservations in working memory.** At ~10 foundation + ~9 subsystem + future = ~25-35 prefixes. With 2-3 letter uppercase codes, the namespace starts feeling tight around 50.

- **"Every subsystem spec will fit this template" is structurally true.** Subsystem specs (S01 Orchestrator Core through S09 Improvement Loop) may need sections the template doesn't provide: interface-contract-per-consumer, runtime-dependency matrix, specific-subsystem-state-ownership declaration. The template assumes the 12 sections are sufficient; the subsystem specs will tell.

- **`specs/` at repo root is the right location.** The template asserts this without engaging the question of whether future multi-spec-set organization (per-release-version snapshots, per-deployment-target variants) would need a hierarchy. Low risk for MVH.

- **DOT workflow documents and YAML policies are NOT specs.** Components.md makes clear these are normative artifacts (workflow graphs, policies), but the template does not mention how they should be specified. If a DOT workflow ships alongside a spec (reconciliation's workflow library per §9.1b), is it under spec normativity? The template is silent.

- **The template version itself won't change mid-authoring.** `spec-template-version: 1.0` is pinned in each spec's front matter, but there's no rule for what happens when the template moves to 1.1 — do existing specs auto-upgrade, stay pinned, require revision rows? A cross-cutting change to the template mid-corpus is a real risk the template doesn't address.
