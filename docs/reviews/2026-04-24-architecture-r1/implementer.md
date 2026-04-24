# Architecture R1 — Implementer Review

Reviewer lens: can a downstream spec author mechanically follow architecture.md's meta-contracts and produce a conformant spec? Stress-tested by picking five requirements and checking them against the two largest drafted sibling specs (`execution-model.md` v0.2.0, `handler-contract.md` v0.1.0), with spot-checks against `event-model.md` and `control-points.md` cross-ref lists.

## Verdict

**Revise before review pass.**

The meta-contracts are substantively sound and comprehensive. Ten locked-in architectural decisions are reified into testable lint rules in §10.2. The three-artifact separation, the ZFC test, and the envelope discipline all generate consistent answers when applied to concrete sibling specs. A downstream spec author reading architecture.md gets a clear set of obligations whose downstream realizations are observably consistent across execution-model.md and handler-contract.md.

However, the spec ships with two defects that need to be addressed before the next review pass.

**First, a corpus-wide citation-shape defect.** Architecture.md self-cites (in its §1 Purpose) and every sibling spec cites `§1.1`, `§1.2`, `§1.4`, `§1.5`, `§1.6`, `§1.6a`, `§1.8`, `§1.9` as the anchors for architecture's normative content — but those section numbers do not exist in the spec. The normative content lives at `§4.1` through `§4.10` and `§6.1`. This is silently resolved by a reader's charity today (there is only one four-axis rule in architecture.md, only one ZFC rule, etc.), but it will break any mechanical cross-reference validator. It also blocks AR-022 (every subsystem spec MUST cite the foundation version it conforms to) from being usefully linted, because the lint cannot tell which section the citation is pointing at.

**Second, AR-013's eight-element subsystem-envelope declaration is nowhere performed** by the two most-finalized sibling specs, and the spec gives no stub showing what a conformant envelope looks like. The information is distributed across each sibling spec's body, but no subsection titled "envelope" enumerates the eight required elements.

The other three requirements stress-tested here pass cleanly.

## Requirements applied

### AR-005 — Every evaluation point is tagged mechanism or cognition

**Applied to**: execution-model.md (58 requirement blocks) and handler-contract.md (57 requirement blocks).

**Outcome: pass with one caveat.** Every numbered requirement in both specs carries a `Tags:` line, every value is exactly one of `mechanism` or `cognition`, and the single-tag rule is honored — no combined values observed. Zero requirements in either spec are cognition-tagged, which is architecturally correct (both specs describe deterministic surfaces; cognition is pushed into per-handler agent behavior and reviewer-node contracts, not the daemon-side contracts). AR-007's delegation-path obligation cannot misfire on these specs because they have no cognition-tagged requirements to check. The caveat: architecture.md itself tags AR-007 and AR-021 as `cognition` with axes lines, which is correct per the rule, but AR-007's body ("the role performing the evaluation, the model class or handler, and the input shape the agent receives") is the only place in the foundation corpus that states what a delegation-path looks like; it is itself the meta-rule. Any downstream reviewer who tries to lint AR-007 conformance must parse free prose in the requirement body. **The rule generates a consistent answer; enforcement is reviewer-enforced per the template lint split, which is correct, but the answer it generates is "run a review" rather than "run a regex."**

One edge case worth surfacing: handler-contract.md HC-023 says "mapping a subprocess exit state or adapter-detected condition to a sentinel class MUST be deterministic from structured fields. No cognition MAY participate in classification." This is mechanism-tagged. But the adapter callback `DetectRateLimit(event) -> (Bool, Duration)` is implemented in a package (S04) that a given handler-type author may be tempted to implement with heuristic regex-over-free-text ("does the payload contain 'rate-limited'?"). AR-006 explicitly forbids "regex parsing of unstructured output" as a ZFC violation, but the rule lives in architecture.md while the implementation temptation lives in handler-contract.md. A downstream spec author reading only handler-contract.md could miss the structured-fields requirement. Recommendation noted below; this is an affirmation of AR-005/AR-006's value, not a critique.

### AR-013 — Subsystem envelope declaration (8 elements)

**Applied to**: execution-model.md §4 (topical subsections 4.1 through 4.10) and handler-contract.md §4 (topical subsections 4.1 through 4.12).

**Outcome: FAIL.** Neither spec contains a subsection titled "Subsystem envelope" that enumerates, as a declaration block, the eight required elements (events produced, events consumed, types introduced with tags, handlers implemented, state owned, control points provided, NFRs inherited/overridden, boundary classification per operation). The information is present in both specs, **but it is distributed across the body** — handler-contract.md's emitted event list is in §6.4 "Co-owned event payloads"; its owned types are in §6.1; its boundary classifications are implicit in per-requirement `Axes:` lines and explicit four-axis tagging is missing for most interface methods; its NFR inheritance is scattered across cross-references. Execution-model.md is worse — it declares types, emits events via other specs, owns state, and provides control-point-consumable handles, but there is no section one can point at and say "this is execution-model's envelope declaration."

A spec author following AR-013 literally has no template to follow. The requirement says "every subsystem spec MUST declare its envelope" with eight named elements; the template (`docs/foundation/spec-template.md`) does not reserve a section for this declaration, nor does AR-013 itself specify where in the §1..§12 layout the envelope goes. Should it be §4.0, preceding the topical subsections? A new §6.N slot? A sibling file per the multi-file split? The rule does not say, and neither extant spec has guessed the same structure.

**Recommendation**: pin envelope declaration to a fixed section (§4.0 "Subsystem envelope" as the first subsection of §4 is the cleanest), and update the template to require it. Failing that, a reference envelope exemplar — a paragraph in architecture.md's §4.4 or an appendix showing what a conformant envelope declaration reads like — would close this gap. AR-INV-004 ("Subsystem envelope is the only declaration surface") is currently unenforceable against either live spec because the surface it names does not exist as a locatable object.

Detail of what is missing, by element, for handler-contract.md as the cleanest test case:

- **(a) events produced**: present in §6.4 Co-owned event payloads (11 event types listed). OK.
- **(b) events consumed**: NOT declared anywhere. The watcher consumes handler-emitted events, but handler-contract.md does not enumerate events consumed FROM other subsystems. (It may not consume any; even so, AR-013 requires the declaration, even if empty.)
- **(c) types introduced in cross-subsystem event payloads**: `LaunchSpec`, `SessionID`, `Handler`, `Session`, `Adapter`, six sentinel errors — declared in §6.1 as schemas, but without the required four-axis + mechanism/cognition tags per AR-004. `Outcome` is cross-subsystem; defined in execution-model.md, referenced here.
- **(d) handlers implemented**: N/A for this spec (it defines the handler contract, doesn't implement it). Should be stated explicitly.
- **(e) state owned**: ambiguous. The daemon owns the watcher goroutine's per-session state (§4.3). Is that "state owned" in the envelope sense? Unclear.
- **(f) control points provided**: none. Should be stated.
- **(g) NFRs inherited/overridden**: scattered across cross-references to operator-nfr; not consolidated.
- **(h) boundary classification per operation**: implicit in per-requirement `Axes:` lines; not consolidated.

A spec author literally cannot copy-paste this structure from a sibling spec because no sibling spec has it. This is a corpus-wide omission traceable to architecture.md not reserving a section slot. Execution-model.md has the same gaps in a different distribution.

### AR-027 — Cross-subsystem agent-type reference points byte-for-byte identical

**Applied to**: handler-contract.md HC-006 (`LaunchSpec.agent_type` field); event-model.md §3.2 payloads (per handler-contract.md §6.4 co-owned event list — `agent_started`, `agent_ready`, etc.); control-points.md freedom-profile references (cited but not read here); architecture.md §6.1 (agent-type identifier regex).

**Outcome: partial pass, traceability broken by section citations.** The regex `^[a-z][a-z0-9-]{1,62}$` in architecture.md §6.1 is cited in handler-contract.md line 459 (`AgentType()` INTERFACE method) and line 485 (`LaunchSpec.agent_type` field), but both citations say `[architecture.md §1.6a]` — a section that does not exist. The actual definition is at §4.7.AR-025 and §6.1. A lint rule checking "does every `agent_type` reference point at the definition" would fail on a broken link, not on the intended "byte-for-byte identical" check. A human reader finds the right regex because there is only one regex in architecture.md; a mechanical validator does not.

If we patch the citation shape, AR-027 passes for the two surfaces touched here. I did not verify the DOT-node-attributes surface or YAML-policy surface because control-points.md is not being stress-tested in this round. **The rule is correct; its enforceability is blocked on the citation-shape defect called out below.**

The byte-for-byte requirement itself is well-shaped: by restricting to the four named surfaces (YAML policies, DOT node attributes, `LaunchSpec.agent_type`, event payloads), it scopes the check to places where a lint can confirm string identity. Compare to the alternative (a human enforces "agent type is the same concept across the codebase") — the AR-027 rule collapses that into four grep-able surfaces. This is a good illustration of the template's "lint-enforced vs reviewer-enforced" split applied at the meta-rule level: AR-027 is lint-enforced, AR-007 is reviewer-enforced, and each is written in a way that signals which column it belongs in.

### AR-037 + AR-INV-005 — Centralized-controller invariant, no agent-to-agent coordination (corrected below)

(Note: AR-INV-005 in architecture.md is the "centralized-controller routing" invariant forbidding file-based or ad-hoc IPC between agent processes. I am using AR-INV-005 consistently with architecture.md's numbering, which has no gaps. Execution-model.md also has an EM-INV-005 unrelated to this rule — they share the "-INV-005" numeric suffix by coincidence of prefix-scoped numbering.)

**Applied to**: handler-contract.md §4.3 (concurrency model), §4.12 (handler as modularity boundary), and the watcher/adapter ownership split.

**Outcome: pass, cleanly.** Handler-contract.md HC-011 pins "exactly one watcher goroutine per active session" owned by S01 (the daemon), HC-012 forbids S04 from holding per-session state or spawning per-session goroutines, HC-016 requires cross-queue handoff through "explicit transitions, never shared memory," and HC-051 declares the handler contract itself as the deterministic-daemon / execution-shape seam. The daemon-vs-orchestrator-agent distinction (AR-017's enumerated out-of-process actors) is honored: handlers are subprocesses, orchestrator-agents are external callers, `br` is an external subprocess. Zero design elements in the spec route coordination peer-to-peer between agents.

Execution-model.md EM-INV-001 ("git is the state-reconstruction source, JSONL replay MUST NOT be used") and EM-INV-005 ("git wins on completion disagreement") further reinforce the centralized-controller posture: not only is the daemon the coordination hub, it also owns a single canonical state-reconstruction path and a declared priority over disagreeing stores. A downstream spec author reading AR-037 + EM-INV-001 + EM-INV-005 gets an internally consistent answer to "where does state live and who wins when stores disagree," which is exactly what one would want from a meta-rule.

This is the architecture-r1 contract at its best — a single principle generates concrete, testable obligations downstream that compose into an invariant visible at the process-geometry level. Twin-parity (HC-INV-002: "the daemon MUST carry zero conditional logic that varies on handler-is-twin") closes the side-channel that would otherwise let test-mode branches smuggle agent-to-agent logic back in. The lint check is concrete: "reviewing the daemon codebase yields zero `if isTwin` / `if agent_type == \"*-twin\"` branches."

### AR-046 + AR-051 — Three-artifact separation, feature is not a primitive

**Applied to**: execution-model.md §2, §3, §4.1 (Workflow definition), §4.3 (Run/bead relationship); handler-contract.md §3, §4 (LaunchSpec vocabulary).

**Outcome: pass.** Neither spec uses "feature" as a normative term anywhere. Execution-model.md §3 explicitly replaces ambiguous "task / cycle / work item" with "run" and cross-references `bead` to beads-integration; the workflow type is DOT per AR-048; no fourth compositional artifact appears. Handler-contract.md's vocabulary (handler, session, adapter, watcher, LaunchSpec, skill) is all process/protocol plumbing — none of it overlaps the compositional-artifact layer. The three-artifact rule generates a clean answer here: every piece of work these specs describe maps onto `spec / workflow graph / bead`, and the many-to-many relationship of AR-050 holds (a run is tied to a bead via optional `bead_id` per EM-014, and a bead may have zero, one, or many runs).

Notably, "run" is NOT a fourth artifact in the AR-046 sense — it is an execution instance of a workflow, carried as a first-class data type in execution-model.md but not a compositional artifact an author writes. This distinction is clear in the spec corpus and a downstream author would not be tempted to promote it. Similarly, "state," "transition," and "checkpoint" are execution-time records, not compositional-artifact candidates. AR-046's strictness pays off: the vocabulary line between "things an author composes work from" and "things the runtime produces" stays bright.

## Ambiguities

### Citation-shape defect (corpus-wide)

Architecture.md's §1 Purpose reads: "Downstream specs cite §1.1, §1.2, §1.4, §1.8, and §1.9 at minimum." But §1 is Purpose (prose), not a normative anchor; the normative content is at §4.1..§4.10 and §6.1. Every sibling spec has dutifully cited `[architecture.md §1.1]`, `[architecture.md §1.2]`, `[architecture.md §1.4]`, `[architecture.md §1.6]`, `[architecture.md §1.6a]`, `[architecture.md §1.8]`, `[architecture.md §1.9]`. **None of those section numbers resolve to a normative section.** Readers infer the right target because architecture.md has exactly one four-axis rule, one ZFC rule, one envelope rule, one centralized-controller rule, one three-artifact rule, and one agent-type regex — but the spec corpus is one grep-for-dead-links away from a quiet failure cascade. Template §Cross-reference convention requires `[spec-id.md §N.N]` where `§N.N` resolves to an actual section; these citations do not. The cheapest fix is a one-line finder-replacer in each sibling spec (§1.1→§4.1, §1.2→§4.2, §1.4→§4.4, §1.5→§4.6, §1.6→§4.8, §1.6a→§4.7 or §6.1, §1.8→§4.9, §1.9→§4.10) plus editing the "Downstream specs cite §X" prose in architecture.md §1.

### AR-013 envelope location is unspecified

See the requirement stress-test above. The spec should either (a) pin a section slot for the envelope declaration, or (b) relax AR-013 to "declares the eight elements somewhere in the spec," with a reviewer-enforced check, not a lint rule.

### AR-023 overlap-detector target

AR-023 specifies that a mechanical overlap detector scans declared sections across open amendment proposals, but does not specify the format in which an amendment proposal declares its touched sections. OQ-AR-003 acknowledges the tooling locus is open; it does not acknowledge that the input format is also unspecified. Without a format, "mechanical" is aspirational.

### AR-033 vs AR-028 tension

AR-033 says Researcher, Verifier, Scheduler, Governor are "declared-but-deferred" and "activation MUST NOT require a foundation revision." AR-028 says adding a new agent type does not require foundation revision. But activating a declared role and introducing a new agent type both touch the envelope of some subsystem; if the role activation causes a new identifier to be reserved (e.g., a `verifier-agent` agent_type), which spec owns it? The rule as written does not distinguish. Minor; not blocking.

### AR-003 skeleton-vs-organ boundary is a design error, not a requirement violation

AR-003 says "operations profiling as `llm-freedom ∈ {bounded, unbounded}` inside the daemon process are a design error; such logic MUST be relocated to an agent handler per §4.9." This is phrased as a MUST — but its enforcement mechanism is unclear. A spec author drafting a new subsystem could in principle declare a daemon-side requirement with `Axes: llm-freedom=bounded` and pass the lint rule (which only checks tag grammar, not tag semantics). The reviewer-enforced check would need to read AR-003 and flag the violation. This is correct as-is (it IS a reviewer check, not a lint), but the requirement reads as if it were mechanically enforceable. A one-line clarification — "detection is reviewer-enforced per §10.2" — would prevent a future author from assuming the lint catches it.

### AR-041 "single source of truth" and session logs

AR-041 says "every normative artifact — specs, policies, workflow DOT files, skill registries, conventions — MUST live in the repository." Handler-contract.md HC-010 emits a `session_log_location` event carrying an absolute `log_path` that points at a filesystem location outside `specs/`. Session logs are ephemeral (per AR-044, "agent coordination artifacts MUST be filesystem-backed; conversations between agents are ephemeral and MUST NOT be load-bearing for any state the daemon consumes"), so they are explicitly not normative artifacts. But AR-041 and AR-044 together cover two different cases — normative artifacts (in repo) and ephemeral coordination (filesystem-backed, not in repo) — with no explicit statement of the complement (normative state the daemon DOES consume, which is git + Beads + JSONL per the three-stores model). A subsystem author could read AR-041 and conclude that JSONL event streams violate it because they are not in the repo. They don't — JSONL is in `.harmonik/` under the project root, which IS the repo — but the spec could state this explicitly. Minor.

## Recommendations

1. **Corpus citation-shape fix (blocking)**.

   Either update architecture.md's §1 prose and every sibling-spec citation to use the correct §4.X and §6.X anchors, or renumber architecture.md's §4 into §1 (so the Purpose's "§1.1..§1.9" resolve). The first is less disruptive; the second is more honest to the original intent.

   Specifically, the following mapping needs to be applied consistently across the corpus (architecture.md §1 prose plus every sibling spec's Depends-on and inline citations):

   - `architecture.md §1.1` → `architecture.md §4.1` (four-axis classification)
   - `architecture.md §1.2` → `architecture.md §4.2` (ZFC mechanism/cognition)
   - `architecture.md §1.3` → `architecture.md §4.3` (required triple)
   - `architecture.md §1.4` → `architecture.md §4.4` (subsystem envelope)
   - `architecture.md §1.5` → `architecture.md §4.6` (amendment protocol)
   - `architecture.md §1.6` → `architecture.md §4.8` (role taxonomy)
   - `architecture.md §1.6a` → `architecture.md §4.7` (agent-type abstraction) or `architecture.md §6.1` (identifier regex)
   - `architecture.md §1.8` → `architecture.md §4.9` (centralized-controller)
   - `architecture.md §1.9` → `architecture.md §4.10` (three-artifact separation)

   A one-session sweep across the six sibling specs would close this gap.

2. **Add envelope-declaration section slot to template and provide an exemplar (blocking)**.

   Reserve `§4.0 Subsystem envelope` as a required first subsection of §4 for every spec declaring a subsystem. Architecture.md's own §4.4.AR-013 should cite this template slot. Add one worked envelope exemplar to architecture.md §A.1 (or a spec-template-examples supplement) so downstream authors have a pattern to copy.

   The exemplar should show the eight elements in a compact form that a subsystem author can extend without rewriting. Something like:

   - Events produced: (enumerated list with cross-refs to event-model.md §3.2).
   - Events consumed: (enumerated list with cross-refs, or "none").
   - Types introduced: (table of type name, four-axis tags, mechanism/cognition tag).
   - Handlers implemented: (enumerated list or "none").
   - State owned: (types cited from execution-model.md §6.1, or "none").
   - Control points provided: (cited from control-points.md, or "none").
   - NFRs inherited/overridden: (cited from operator-nfr.md).
   - Boundary classification per operation: (table of operation name, four-axis tuple, mechanism/cognition tag).

   This form is copy-pasteable and lint-checkable.

3. **Specify amendment-proposal format (supporting AR-023)**. A one-paragraph spec in §4.6 of the shape of a proposal (front matter block with `touches:` list of `[spec-id.md §N.N]` entries) would let OQ-AR-003's overlap detector be written.

4. **Clarify AR-013 enforcement against foundation specs themselves (informative)**. Architecture.md is itself a foundation spec, not a subsystem; AR-013 applies to subsystem specs only. This is correct by omission but worth stating explicitly — a reviewer reading AR-013 could reasonably ask whether architecture.md is required to declare its own envelope (it is not). A one-sentence exclusion in §4.4 or §2.2 would head this off.

5. **Clarify AR-003 enforcement mechanism (informative)**. Add a one-line note in the requirement body that detection is reviewer-enforced per §10.2, not lint-enforced. This prevents a downstream author from assuming the lint catches daemon-side `llm-freedom=bounded` violations.

6. **State the repo/ephemeral/state-store trichotomy explicitly (informative)**. AR-041 + AR-044 implicitly partition artifacts into "normative in repo" and "ephemeral filesystem-backed, not load-bearing." Add a third explicit category: "durable state stores the daemon consumes (git checkpoints, Beads SQLite, JSONL event stream), located inside the project tree but not inside `specs/`." This prevents the "are JSONL event streams a violation of AR-041?" confusion.

7. **Consider lifting HC-023's structured-fields rule into architecture as a corollary of AR-006 (informative)**. AR-006 bans "regex parsing of unstructured output" generally; HC-023 re-states the same rule for handler error classification. A future spec author implementing a different mechanism-tagged classification (reconciliation Cat detector, for example) has to discover AR-006 independently. Either (a) AR-006 already suffices and HC-023's restatement is redundant but harmless, or (b) an architecture-level corollary "every mechanism-tagged classifier MUST operate on structured fields" would tighten the rule. I lean (a) — the redundancy is cheap and the specificity at HC-023 is useful — but flagging for architect consideration.

## Affirmations

- §4.3 (search / verifier / traces) generates correct downstream mappings: execution-model's `transition_kind` enum (EM-044) is the search substrate with the four declared kinds (`local-patchback`, `architectural-rollback`, `policy-rollback`, `context-restore`), `verification-node` is a node type in the workflow graph (EM-006 node type enum includes it via `non-agentic` + reviewer-agent delegation pattern), and `Transition` (EM-004) is the trace record carrying the full AlphaGo field set (prior state, actor role, candidate actions, chosen action, policy version, evidence, verifier metrics, confidence, next state, plus `schema_version`). AR-010/011/012 are load-bearing and get honored.
- §4.5 (MVH Go-package pin) correctly removes a whole class of ambient confusion from subsystem specs. Handler-contract's clarity about "daemon owns the watcher, S04 is a package, handlers are subprocesses" is directly downstream of AR-016's pin. AR-019's post-MVH escape hatch ("each Go package that declares an envelope is a subsystem regardless of which process binary hosts it") is the right abstraction to leave open without prematurely committing the corpus to multi-process geometry.
- §4.7 (verification-naming trichotomy) is paying off already: handler-contract.md distinguishes `agent_ready` (ready signal), `outcome_emitted` (outcome), and `agent_failed` (failure event) without confusing them with verification-node / verification-result / quality-gate — the spec corpus is not at risk of drifting toward a "verifier subsystem" even under pressure. The hyphenated canonical forms (AR-029/030/031) are a small investment that pays recurring returns in spec-prose clarity.
- §4.9 (centralized-controller) is the single strongest rule in the spec. Handler-contract.md's watcher/adapter split is a clean concrete realization. AR-040's explicit tradeoff acknowledgment ("if the daemon dies mid-run, everything stops; reconciliation is required on restart") pairs with reconciliation.md's role and saves future reviewers from relitigating the Gas Town vs. centralized-controller decision — the spec tells them what was traded and names the trigger conditions (remote-daemon, multi-operator-single-daemon) for revisiting it.
- §4.10 (three-artifact separation) is honored corpus-wide; "feature" does not appear as a normative term in any spec reviewed. AR-051's explicit exclusion is a forcing function that prevents the temptation to introduce a fourth store.
- §10.2 (test-surface obligations in prose) is a good bootstrap form; the per-group breakdown (AR-001..AR-004 lint, AR-005..AR-008 lint, etc.) maps cleanly onto how a future testing.md would absorb them. Each group has a concrete, implementable check — "every requirement carries exactly one `Tags:` value," "every subsystem spec declares the eight envelope elements," "no spec treats `feature` as a normative term." These are executable contracts, not aspirations.
- §A.3 Rationale covers the load-bearing design decisions (meta-rules in a separate spec, Go-package pin, tradeoff acknowledgment, feature exclusion, verification trichotomy). Each paragraph answers a question a subsystem author would actually ask. The "why subsystem-as-Go-package is pinned" paragraph in particular is a useful forcing function: it names the failure mode (proliferation of out-of-process shapes before envelope discipline is proven) and the mitigation (prove the discipline in-process first).
- Front matter `depends-on: []` is honest — architecture.md truly is the root of the foundation corpus. No sibling spec is pulled in as a prerequisite. This also means architecture.md can be reviewed in isolation, which is the right editorial decision for a meta-rules spec.
- Open questions are scoped appropriately: OQ-AR-001 (prose→testing.md migration), OQ-AR-002 (post-MVH agent-type namespace), OQ-AR-003 (overlap-detector tooling locus), OQ-AR-004 (decentralization-pivot trigger conditions). None of these block MVH; each names a default-if-unresolved that keeps the spec usable. This is the right discipline.
- §4.4.AR-015 (add-a-subsystem procedure) is the right escape-hatch design: new subsystems should be writable by citing existing foundation specs, not by revising foundation. This prevents the foundation from accreting subsystem-specific details over time. When combined with §4.6 (amendment protocol), it gives a clear signal: "if you cannot write your subsystem spec without touching foundation, pause and write an amendment first."
- The depends-on-none posture (no other foundation spec is a prerequisite) correctly pins architecture.md as the root of the corpus. Any downstream reader can read architecture.md in isolation and derive the complete set of meta-rules.
- The handling of "verifier" is consistently defensive: AR-011 bans a verifier subsystem, AR-018 repeats for reconciliation, AR-029/030/031 pin the three hyphenated forms. A future author tempted to reintroduce a verifier subsystem has to traverse three separate rules to do so, each phrased as a MUST. This is correct over-specification.

## Summary

The meta-contracts work. Five stress-tests, four pass cleanly, one (AR-013 envelope location) fails because the rule is under-specified and the template does not reserve a section for the output. The broader blocker is the citation-shape defect: the spec corpus uses `[architecture.md §1.X]` for content that lives at `§4.X`. This is cheap to fix but silently breaks every cross-reference validator until fixed, and it blocks AR-022 (version-pinned foundation citations) from being machine-checkable. With those two fixes applied, the spec is ready for the next review pass.

What works about this spec is its discipline in reifying each locked-in architectural decision into a requirement that generates downstream consequences. The centralized-controller principle (AR-037) produces concrete concurrency rules in handler-contract.md. The three-artifact separation (AR-046) produces a clean run/bead/workflow taxonomy in execution-model.md. The mechanism/cognition split (AR-INV-001) produces the watcher/adapter split and the error-classification rules. A subsystem author who reads architecture.md top-to-bottom gets a clear, consistent, and testable set of obligations — modulo the two fixes named above.

What needs work is enforcement surface. AR-013 says every subsystem spec MUST declare its envelope, but no template slot exists for the declaration and no extant spec has one. AR-023 says an overlap detector scans declared sections, but no declaration format exists. AR-003 says operations profiling as `llm-freedom=bounded` inside the daemon are a design error, but the lint cannot catch it and the requirement does not say so. These are not fatal flaws — the spec works as a reviewer's guide today — but they prevent the spec from graduating from reviewer-enforced to lint-enforced, which is the state the corpus will eventually need.

Verdict stands: revise before next review pass. Two blocking fixes (citation-shape defect corpus-wide, envelope-declaration section slot plus exemplar), five informative improvements (AR-003 enforcement clarification, repo/state-store trichotomy, HC-023 lift decision, AR-013 foundation-spec exclusion, AR-033/AR-028 activation-vs-registration distinction), otherwise the spec is doing its job as the root of the corpus.

The review pass required to close this out is small. The fixes are mechanical and the spec's substance does not need revision.
