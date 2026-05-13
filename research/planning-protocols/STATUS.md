# planning-protocols -- Status

> Current phase, active work, and session history. Every session updates this file before ending.

Last updated: 2026-04-30 (basata-trial scoping session; awaiting methodology sign-off)

## Current phase

**Phase 2 -- Deep research.** Steps 1–6 + Step 7 draft complete in one session. Step 4.5 (corpus-signal filter) deferred pending user authorization. See session history for details.

## What's done

- Research question agreed with user.
- Evaluation criteria agreed (see METHODOLOGY.md).
- Locked-in choices captured (see METHODOLOGY.md).
- Folder scaffolding created (CLAUDE.md, METHODOLOGY.md, this file).
- Initial Perplexity brainstorm captured in `references/perplexity-initial-research.md`.
- Reconnaissance of `~/.claude/projects/`: relevant dirs identified.
  - `-Users-gb-github-harmonik` -- 6 sessions
  - `-Users-gb-github-kerf` -- 52 sessions
  - `-Users-gb-github-machine-setup` -- 4 sessions
  - `-Users-gb-github-secure-dev` -- 131 sessions
  - `-Users-gb-Developer-secure-dev` -- 2 sessions
  - Various `-Users-gb-github-secure-dev--ntm-worktrees-*` dirs -- likely implementation-only (agent worktrees); lower priority.

## Current phase

**Phase 2 main session CLOSED.** `phase-2-findings.md`, `evaluation-framework.md`, and `phase-2-kerf-integration-draft.md` produced. Steps 1–6 + Step 7 draft complete. **Step 4.5 deferred** — user authorization needed to execute corpus-signal filter across all 195 sessions.

## What's in flight

**Blocked on user decisions:**
- Authorization for Step 4.5 corpus-signal filter (1–2 days of scripting).
- Review and disposition of `phase-2-findings.md` §10 recommendations (Layer 1 stack adoption, Layer 6 safe swaps, Layer 7 A/B candidates).
- Review of `phase-2-kerf-integration-draft.md` §8 open questions before turning it into a kerf work.

## Phase 1 closed

Phase 1 scoping work wrapped 2026-04-23 earlier the same day. `research-statement.md` remains the authoritative briefing; Phase 2 refined but did not replace it.

## What's done (this session)

- **1A -- Corpus discovery.** 4 per-project catalogs at `phases/phase-1/corpus/<project>/_catalog.md`.
- **1A -- Re-classification.** Whole-session classification was too coarse; re-classified all 195 sessions by `n_human_text_turns` signal.
- **1B -- Session-type discriminator.** `phases/phase-1/session-type-discriminator.md`.
- **1C -- Corpus extraction.** 10 sessions extracted to `phases/phase-1/corpus/<project>/<session-id>.md`. Compression ratio 0.7–14% vs raw JSONL. Extraction script at `scripts/extract_dialog.py`.
- **Tried-protocols catalog.** `phases/phase-1/tried-protocols.md` -- 5 interaction variants from user practice.
- **1D -- Multi-lens analysis.** 6 parallel sub-agents ran: decision-delegation, misaligned-assumption, writing-load, form-vs-content, topic-tree, context-switch. Outputs at `phases/phase-1/analysis/<lens>.md`.
- **1E -- Research statement.** `research-statement.md` drafted. Restructured mid-draft to guard against local-maxima anchoring (user's concern): dimensions-of-variation-first, observed-region-as-data-not-anchor, counter-hypotheses, external-source-first Phase 2 methodology.

## What Phase 2 should do

Fresh session, new context. Read `research-statement.md` (self-contained hand-off) + `METHODOLOGY.md`. Follow Phase 2 methodology in Section 7 of the research statement. Key discipline: do NOT refine observed patterns first; external-source pass first.

## 1A findings summary (REVISED 2026-04-23 after user correction)

**Initial whole-session classification was wrong.** The sub-agents classified sessions by overall tool profile (PLANNING / MIXED / IMPLEMENTATION / OTHER). This missed what we actually care about: **planning dialog is a within-session phase, not a whole-session label**. Many sessions tagged IMPLEMENTATION had rich planning dialog in their opening, followed by an autonomous run that dominated the tool profile. Conversely, some "emblematic" sessions were single-directive dispatches with almost no dialog.

**Mechanical filter for real human-text turns (verified on sample sessions):**

- `type == "user" AND isSidechain == false AND content-is-string` → real human text turn ✓
- `type == "user" AND content-is-array` → `tool_result` events (filter out)
- `isSidechain == true` → sub-agent sidechain events (filter out)
- `type == "assistant" AND isSidechain == false` → main-thread agent turn

Note: zero sidechain events appeared in top-10 dialog-dense sessions. Sub-agent dispatches via `Task` may not always surface as sidechain. Verify during extraction.

**Re-classification by n_human_text_turns (ht) across all 195 sessions:**

- ht=1: 106 sessions (pure autonomous dispatch — template directives)
- ht=2-3: 55 (short, mostly stubs or single-correction dispatches)
- ht=4-10: 22 (light dialog)
- ht=11-20: 4 (moderate dialog)
- ht=21+: 8 (heavy dialog, but includes controller-orchestrator sessions)

**Revised planning-dialog corpus (ht≥15, filtered to exclude controller-opener sessions):**

| ht | project | session | opening |
|---|---|---|---|
| 38 | secure-dev | 79a42399 | "project coming along... no idea how it works" |
| 31 | kerf | 38415843 | "spec-only project" framing |
| 25 | secure-dev | c6d1bd16 | beads + exploratory testing (⚠ 12MB raw) |
| 21 | harmonik | 3bf5774c | "catch up on what we're working on" |
| 20 | harmonik | f588ff0c | "figure out next direction" |
| 19 | machine-setup | 2a50e0fc | "create beads + orchestrate" (borderline) |
| 17 | secure-dev | d1704aa0 | "partially implemented... study specs" |

Plus 3 borderline worth inspecting: 13493c8d (harmonik, 5 huge-message turns — founding vision dump), 729dad16 (kerf, 14 turns, session-recovery handoff), 00eb9fc9 (harmonik recent, 4 substantive turns).

**Emergent taxonomy of interaction variants (user already developed these implicitly):**

1. **Planning dialog** — the 7 sessions above. Substantive opener, many back-and-forth turns.
2. **Controller orchestration** — opens "You are the controller agent"; dialog-dense (b7eca5d2 59, 3fb3dc80 42, 69050eec 24, 7ff17283 11) but human is directing an orchestrator, not co-designing. Examples: b7eca5d2, 3fb3dc80, 69050eec, 7ff17283.
3. **Autonomous dispatch** — ht=1, single-directive + long autonomous run (~100 secure-dev template sessions).
4. **Context-dump** — few but very long human messages (13493c8d had 5294/1903/3441-char turns with 164 assistant responses).
5. **Session-recovery handoff** — inter-session continuation (729dad16 opens "# Session Recovery Context").

Variants 2-5 are out of core scope but will be captured in `phases/phase-1/tried-protocols.md` for comparative analysis. This was user-approved as "list of things that have been tried."

**Reclassified data:** `/tmp/reclass.tsv` holds per-session metrics (ht, sidechain_count, assistant_count, size_kb, project, session_id, first_msg_preview) — regenerable via `/tmp/reclass.sh`.

## Open questions

(carrying forward from prior entry, resolving where findings allow)

- ~~How strict "planning session" filter?~~ RESOLVED: real dialog is MIXED + a few PLANNING; the 15-session shortlist is the working corpus. Can be expanded if needed.
- ~~ntm-worktree dirs in scope?~~ RESOLVED: excluded as worker sessions.
- **Very large session extraction feasibility** (f588ff0c at 3.2M / 621 msgs). Decide during 1C.
- **Should secure-dev's "never-ask-questions" template be studied as its own protocol variant** rather than excluded? Likely yes -- it's data on an already-attempted solution.
- **Other `.claude/projects/` dirs?** `ai-improvement-talk`, `gt-*` (gas-town) dirs might contain planning dialog. Worth a light pass if the current 15 prove thin.

## Open questions

- How strict should the "planning session" filter be? Sessions mix planning + exploration + research in real use; fully planning-pure sessions may be rare.
- Should agent-worktree session dirs (`-Users-gb-github-secure-dev--ntm-worktrees-*`) be included? Probably no: those are sub-agent implementation sessions, not human-agent planning. Confirm during 1A.
- For very large sessions (multi-MB), is full dialog extraction feasible, or should the extract summarize very long sub-threads? Decide during 1C based on what 1A surfaces.
- When is it safe to close Phase 1? The research statement (1E output) is the exit criterion, but we should define "good enough" for it before drafting.

## Session history

### 2026-04-23 -- Track kickoff + 1A/1B/1C complete

**What happened:**
- Discussed research shape with user across several turns. Converged on: working term "planning protocols", empirical-first stance, two-stage research (Phase 1 scoping / Phase 2 deep), harmonik as primary test context, findings feed back into kerf.
- Identified main pain points: agent defers too many trivial decisions to human (a likely top lever), agent's misaligned assumptions take many turns to surface, discussion *form* (not just content) seems to matter.
- Decided to work from transcripts first rather than external literature. Perplexity doc captured as a reference brainstorm, acknowledged as shallow.
- Built the track's scaffolding (CLAUDE.md, METHODOLOGY.md, this file, folder structure).
- Launched 4 parallel sub-agents for 1A (corpus discovery), one per project.
- All 4 catalogs written and synthesized. See "1A findings summary" above.

**Decisions made:**
- Use "planning protocols" as working term.
- Empirical-first: mine transcripts before external sources.
- Extraction must precede analysis; enforced in methodology.
- Multi-session safety rules encoded in CLAUDE.md and METHODOLOGY.md.

**Mid-session correction:** User flagged that the initial whole-session classification missed the real signal. Planning dialog is a within-session phase, not a whole-session label. Many sessions tagged IMPLEMENTATION had rich planning dialog at the front; some "emblematic" sessions were actually single-directive dispatches. Re-investigated JSONL mechanics, discovered `(type=="user") AND isSidechain==false AND content-is-string` filter cleanly isolates real human text turns. Re-classified all 195 sessions.

**Subsequent work:** wrote extraction script (`scripts/extract_dialog.py`), validated on small session, ran on 10 primary+borderline sessions. Wrote `phases/phase-1/session-type-discriminator.md` and `phases/phase-1/tried-protocols.md`. Updated `phases/phase-1/corpus/INDEX.md`.

**What the next session should do:**
1. Read the Phase 2 kickoff prompt at `prompts/phase-2-kickoff-prompt.md` OR have it pasted in by the user.
2. Follow its instructions (read order: CLAUDE.md → STATUS.md → research-statement.md → METHODOLOGY.md).
3. Execute Phase 2 methodology in research-statement §7, starting with Step 1 (criteria interrogation).

**Two mid-session course corrections in Phase 1 (important context for Phase 2):**

1. The user flagged the **local-maxima risk** before Phase 1E drafting. Observed patterns from Phase 1 could anchor Phase 2 onto incremental refinement instead of discovering genuinely different approaches. Research statement was restructured with dimensions-of-variation first, counter-pattern hypotheses (§6), and mandated external-source-first Phase 2 methodology (§7) to guard against this.

2. The user flagged the **evaluation criteria are uncertain**. They do not know what the correct evaluation criteria for planning protocols are. Research statement §2 now foregrounds this; §7 Step 1 mandates criteria interrogation before external-source work; "a durable evaluation framework" is named as a potentially-more-valuable output than specific protocol recommendations.

### 2026-04-23 -- Phase 2 main session (Steps 1-6 + Step 7 draft)

**What happened:**

- Executed the Phase 2 methodology from research-statement §7 in one session. Produced the Phase 2 main output (`phase-2-findings.md`) plus a first-class durable deliverable (`evaluation-framework.md`) plus a kerf-integration DRAFT (`phase-2-kerf-integration-draft.md`).
- **Step 1 (criteria interrogation).** Three parallel sub-agents with distinct challenge frames: rival framings, operationalization audit, empirical-evaluation design. Synthesis at `phases/phase-2/analysis/evaluation-criteria-refinement.md`. Three sub-analyses at `phases/phase-2/analysis/evaluation-criteria-refinement.sub-*.md`. No fundamental issues surfaced; Phase 2 proceeded without user pause. Refinements: provisional criteria replaced by pair-graph of required-paired metrics; multi-framing scoring requirement added (Framings A/B/C); formal evaluation framework elevated to first-class deliverable.
- **Step 2 (external-source pass).** 10 parallel sub-agents, one per domain, outputs at `phases/phase-2/analysis/external-sources/<domain>.md`. Domains: pair-programming, socratic-method, medical-handoffs, design-review, negotiation-mediation, incident-command, pilot-controller, therapy-intake, consulting-discovery, military-briefings. ~70 candidate protocols extracted.
- **Step 3 (counter-pattern generation).** Single sub-agent steel-manned 8 counter-hypotheses from research-statement §6 into specific protocol instances. Output at `phases/phase-2/analysis/counter-pattern-candidates.md`.
- **Step 4 (unified catalog).** Single sub-agent consolidated observed + unexplored + external + counter-pattern into 87 distinct protocols on shared 8-field schema. Output at `phases/phase-2/analysis/unified-protocol-catalog.md`. Surfaced gaps (scope-decomposition dependency-awareness; research-scoping question-quality).
- **Step 4.5 (corpus-signal filter) DEFERRED.** The evaluation framework specifies this as required before Step 5 ranking. It requires ~1–2 days of scripting over all 195 sessions; treated as user-authorization-gated per the "no code" locked choice interpretation. All Step 5 rankings carry `[filter-dep]` flags for candidates whose ranking needs filter validation.
- **Step 5 (reviewer-challenged evaluation).** Six parallel reviewer sub-agents: ergonomics, cognitive-load, fatigue-robustness, task-type-adaptability, challenge-observed-patterns (local-maxima guardian), and multi-framing. Outputs at `phases/phase-2/analysis/reviewer-*.md`. Strong cross-frame convergence on winners and losers.
- **Step 6 (ranked recommendations).** Main-thread synthesis at `phase-2-findings.md`. Organized as composition layers: Layer 1 always-on foundation; Layer 2 task-shape openers; Layer 3 mid-session stack; Layer 4 user-state adapters; Layer 5 close-of-session; Layer 6 safe swaps; Layer 7 experiments. Plus qualitative overlays (§5) and explicit honest-limits (§8).
- **Step 7 (kerf integration DRAFT).** Produced as DRAFT for user review at `phase-2-kerf-integration-draft.md`. Maps Layer 1 and Layer 6 onto kerf's pass/jig/reviewer structure. Explicit user-decision points throughout. Not final.

**Key findings:**

- 7 of 8 Phase 1 observed findings lost to counter-pattern or external-source rivals under the challenge-observed reviewer's frame. Strongest displacement case: `numbered-question-close` is an external-evidence-backed anti-pattern (aviation CRM decades of incident archive support interleaved slot-ack over end-close enumeration).
- Multi-framing reviewer identified `numbered-question-close`, `autonomous-dispatch` (bare), `context-dump` (bare), and `forced-choice-with-default` as "trap candidates" — high on provisional, low on Framing C (regret-adjusted). Exactly the user's flagged "unnamed-but-important criterion" risk.
- Hidden gems (under-rewarded by provisional, high on rivals): `example-led-emergence`, `emergent-partition`, `assumption-bundle`, `question-preserving-autonomy`, `asynchronous-navigator`, `dialogic-context-accretion`.
- Convergent winners (pass ≥4 reviewer frames): `commanders-intent`, `back-brief-plan-quality`, `autonomy-scope-grant`, `alternatives-considered-section`, `role-split-reviewer-library`, `premortem-reviewer`, `load-bearing-token-readback`, `recovery-handoff` (augmented), `single-text-procedure`.
- **The formal evaluation framework emerged as the most durable Phase 2 output** — confirming the research-statement §2 hypothesis. `evaluation-framework.md` will outlast specific protocol recommendations.

**What the next session should do:**

1. Read user's response to Phase 2 findings; disposition §10 recommendations.
2. If Step 4.5 corpus filter is authorized: build and run the transcript-only harness (specified in `evaluation-framework.md` §4) across all 195 sessions. Update Step 5 rankings and Step 6 recommendations with filter output.
3. If Layer 7 A/B experiments are authorized: begin with numbered-close vs load-bearing-token-readback (5–8 matched pairs; pre-registered per `evaluation-framework.md` §8).
4. If kerf integration is authorized: turn `phase-2-kerf-integration-draft.md` into a kerf work after user answers §8 open questions.
5. If behaviors-first plan expression is authorized as a follow-up research direction: targeted Step 2-style external-source pass into test-driven design, BDD, design-by-contract, specification-by-example.

**Course corrections during session:** None. The three sub-analyses for Step 1 converged without requiring user pause; Step 4.5 deferral was explicit rather than stumbled-upon. Cross-reviewer convergence was stronger than expected — the six frames produced high-agreement rankings on both winners and losers.

**Session discipline observations:**
- Multi-framing requirement (added in Step 1) was load-bearing in Step 5 multi-framing reviewer's output — it surfaced the trap-candidate pattern that no single-frame reviewer caught.
- Counter-pattern generation (Step 3) paid off: several counter-patterns survived all six reviewer frames as hidden gems. Without deliberate steel-manning, they would not have been represented in the catalog.
- External-source triangulation (Step 2) paid off: the numbered-close aviation-CRM anti-pattern finding is pure external validation against an observed pattern, and no amount of within-corpus analysis would have surfaced it.

### 2026-04-27 -- Skill trial active + root-folder cleanup

**What happened:**

- Skill trial of `/session-handoff` + `/session-resume` (5 embedded Layer-1/5/6 protocols) was set up earlier in the day in a prior session. Trial roadmap captured at `protocol-trial-roadmap.md`.
- The trial's `HANDOFF.md` was written to repo root, colliding with the harmonik-main session that also writes there. Resolved this session: skills updated to accept an optional path argument (default `./HANDOFF.md`); for this track, use `/session-handoff research/planning-protocols/HANDOFF.md`. Path-arg convention noted in the trial roadmap.
- Useful content from the root trial-handoff was integrated into `protocol-trial-roadmap.md` (new "Trial calibration items to watch" section + path-convention note); root `HANDOFF.md` deleted to free the canonical location for the harmonik-main session.
- Root of `research/planning-protocols/` had degraded into 14 mixed-purpose files. Reorganized into `plans/` (forward-work plans + reviews) and `prompts/` (paste-in session-starter prompts). Five files moved; one renamed (`HANDOFF.md` → `prompts/deep-dive-prompt.md` to disambiguate from the skill-output filename).
- `CLAUDE.md` and `METHODOLOGY.md` updated with new placement rules so future agents know where new artifacts go.
- `INDEX.md` updated: new "Forward work" and "Session-starter prompts" sections; phase-2-kickoff-prompt removed from misclassified Phase 1 row; current-state entry added for the active skill trial.

**Decisions made:**

- Path argument added to `/session-handoff` and `/session-resume` skills. Default unchanged (`./HANDOFF.md`); pair-by-path required when explicit path is used.
- Root `./HANDOFF.md` reserved for the harmonik-main session. This research track uses `research/planning-protocols/HANDOFF.md` (not currently present; will be created on next `/session-handoff` from this track).
- Root of `research/planning-protocols/` holds only entry/governance + canonical deliverables + the active forward-work roadmap. Step outputs live in `phases/phase-2/analysis/`, plans in `plans/`, paste-in prompts in `prompts/`. Encoded in CLAUDE.md and METHODOLOGY.md.
- `prompts/deep-dive-prompt.md` (formerly `HANDOFF.md`) retains its content as a paste-in prompt for fresh-session digestion of Phase 1+2 output. Filename change is the only modification.

**What the next session should do:**

1. If continuing the skill trial: run `/session-resume research/planning-protocols/HANDOFF.md` if a handoff exists; otherwise pick a real working session and produce a `/session-handoff` to that path at the end.
2. If continuing the research track (Step 4.5, kerf integration, etc.): standard read-in via `CLAUDE.md` → `STATUS.md` → `INDEX.md`. Forward-work options listed in `phase-2-findings.md` §10 and `protocol-trial-roadmap.md`.

**Open after this session:** Memory entry `project_planning_protocols_skill_trial.md` was updated to reflect the new path of `step-4.5-plan*.md` and the path-arg skill convention.

### 2026-04-27 -- Per-phase folder structure (phases/phase-N/)

**What happened:**

- Earlier in the day's cleanup, root still held 4 phase-2 deliverables + research-statement.md alongside `01-corpus/` and `02-analysis/` (the latter mixing Phase 1D lens reports with Phase 2 step outputs). Pattern wouldn't scale — by Phase 10 root would hold 9+ phase deliverables, and `02-analysis/` would be an undifferentiated grab bag.
- Introduced `phases/phase-N/` structure as the load-bearing organizing axis. Per-phase deliverables and per-phase work products both live in their phase's directory; cross-phase work products (corpus, scripts, references, plans, prompts) stay in cross-phase subdirectories at track root.
- Moves executed:
  - `research-statement.md` → `phases/phase-1/research-statement.md`
  - `phase-2-findings.md` → `phases/phase-2/findings.md` (drop redundant `phase-2-` prefix; folder context implies it)
  - `phase-2-kerf-integration-draft.md` → `phases/phase-2/kerf-integration-draft.md`
  - `evaluation-framework.md` → `phases/phase-2/evaluation-framework.md`
  - `01-corpus/` → `phases/phase-1/corpus/`
  - `02-analysis/{6 Phase 1D lens reports}` → `phases/phase-1/analysis/`
  - `02-analysis/{Phase 2 Step 1-5 outputs}` → `phases/phase-2/analysis/` (preserves the `external-sources/` subdirectory)
  - `references/session-type-discriminator.md` (Phase 1B output) → `phases/phase-1/`
  - `references/tried-protocols.md` (Phase 1A output) → `phases/phase-1/`
  - `references/perplexity-initial-research.md` stayed in `references/` (genuine external import)
- Path references updated across all governance docs (`CLAUDE.md`, `METHODOLOGY.md`, `STATUS.md`, `INDEX.md`), the active roadmap, and both prompt files. Internal cross-references inside moved files (e.g., `phases/phase-1/corpus/INDEX.md` referencing `../session-type-discriminator.md`) recomputed for the new depth.
- `METHODOLOGY.md` "Phase 2" section updated from "future, not started" (stale) to a Step-by-Step description of the closed Phase 2 work; new "Phase N pattern" subsection captures the convention so a future Phase 3 inherits the structure without rediscovery.

**Decisions made:**

- Per-phase content lives in `phases/phase-N/`. New phases create their own directory.
- Phase deliverables drop the redundant `phase-N-` filename prefix when the folder context implies it (e.g., `phases/phase-2/findings.md`, not `phases/phase-2/phase-2-findings.md`).
- Cross-phase work products (`corpus/` was Phase 1-specific; `scripts/`, `references/`, `plans/`, `prompts/` are cross-phase by nature) stay at track root.
- `references/` is reserved for genuine external imports. Phase outputs that happen to define classifiers/taxonomies belong in their phase folder, not in `references/`.
- The active forward-work roadmap (`protocol-trial-roadmap.md`) is the only "deliverable" allowed at track root — it's cross-phase by definition.

**What the next session should do:**

- Standard read-in via `CLAUDE.md` → `STATUS.md` → `INDEX.md`. Both files now describe the per-phase structure; new agents should not need to rediscover it.
- For the active skill trial: `/session-resume research/planning-protocols/HANDOFF.md` if a handoff exists; otherwise produce one at session end.
- For Phase 3 work (whenever it begins): `mkdir phases/phase-3/`, follow the convention captured in `METHODOLOGY.md` "Phase N pattern."

### 2026-04-30 -- Basata-trial scoping; Phase 3 proposed; awaiting methodology sign-off

**Context.** User returned with results from the basata project (take-home assignment, `~/github/basata`). Across roughly 30 sessions between 2026-04-28 and 2026-04-30 the user ran two parallel Claude Code agents — an "implementor" (drained the task queue, integrated changes) and a "tester" (manual-testing partner that wrote bug beads back into the implementor's queue). Both agents used the v2 `/session-handoff` + `/session-resume` skills heavily, restarting every 10-20 tasks as context grew past 200k tokens. User reported v2 worked "CRAZY GOOD" — a strong qualitative signal at n≫3, well past the 3-5 fires the methodology asked for before structural conclusions on v2.

**Three signals identified in the basata corpus:**

1. **v2 skill validation at n≫3.** The trial-finding-1 follow-up parked at "next real planning session produces n=3 signal" has accumulated ~10× that across basata. Can confirm whether v2 fixed the eight failure mechanisms from finding 1 and whether new failure modes appeared.
2. **Novel orchestration pattern (not in Phase 2 catalog).** Implementor + tester + dedicated bug-bead-queue. Closest catalog primitives — `controller-orchestration`, `role-split-reviewer-library`, `parallel-reviewer-mob` — each cover one slice; the synthesis (fast-loop tester writes beads → slow-loop implementor drains queue, with manual-testing-driven discovery) is not in the catalog. Directly relevant to harmonik's orchestration design.
3. **Restart-friction calibration item.** Two-track work needed `HANDOFF.md` + `HANDOFF-tester.md`; user reported "a little friction making sure the new session started with the right perspective." v2 was authored assuming one handoff per project; real practice broke that assumption.

**Planning decisions (signed off this session):**

- **Trial-finding 2 = v2 validation, basata-only scope.** One project, one bead system, one human, one cadence → uniform context for failure-mode signal. Output target: `trial-findings/2026-04-30-v2-validation.md`. Hypothesis-driven (the 8 mechanisms from finding 1) plus open coding for new modes. Restart-friction folds in as a calibration section, not its own finding (same class as finding 1: template assumed a shape real practice didn't fit).
- **Phase 3 opens as a new phase.** Epistemic shape distinct from Phase 1 (corpus scoping) and Phase 2 (external research + reviewer evaluation): *deep characterization of a single observed pattern, written so a fresh agent can operate the pattern from the doc alone.* First Phase 3 doc: `phases/phase-3/implementor-tester-pattern.md`.
- **Phase 3 eligibility criteria** (to prevent noise accumulation): pattern must be (a) observed in real practice, (b) not already in the Phase 2 unified catalog, (c) show evidence of effectiveness in real use.
- **Phase 3 per-pattern doc template** (inherited by all future Phase 3 docs): actors, contract between actors, mechanism, asymmetries, preconditions, failure modes, harmonik implications, evidence citations to session+line.
- **Reviewer gate for Phase 3 docs**: a fresh-agent-readability test — a reviewer sub-agent reads the finished doc cold and tries to describe how to operate the pattern; if they can, the doc passes.
- **Methodology-first execution.** User explicit: don't do random things — assume numerous agents come after, structure work so they can consume it. No analysis until methodology is written and signed off.
- **Pattern doc filename:** `implementor-tester-pattern.md` (confirmed).

**Planned methodology files (proposed; awaiting user sign-off before writing):**

1. `METHODOLOGY.md` — append a Phase 3 entry under the existing "Phase N pattern" subsection. Names eligibility criteria, doc template, reviewer gate.
2. `phases/phase-3/methodology.md` — Phase 3 internal process (corpus → extraction → characterization → review → publication). Mirrors how Phase 2's research-statement.md §7 worked.
3. `phases/phase-3/corpus/EXTRACTION.md` — extraction parameters (dialog filter, role classifier, neighbor-window heuristic). Reproducible by future agents. Cites `scripts/extract_dialog.py`.
4. `trial-findings/CONVENTIONS.md` — codifies the finding-1 doc shape (Part 1 observations → Part 2 analysis → Part 3 implications → Part 4 open questions → Source material). Notes hypothesis-driven variant adds a Part 0 stating tested hypotheses.
5. `STATUS.md` / `INDEX.md` updates — Phase 3 added, trial-findings convention linked.

**Planned execution sequence (after methodology sign-off):**

1. Build evidence layer — role-mapping sub-agents (first 1-2 user messages identify implementor vs tester) → extraction with user's "user turns + immediate neighbors, skip sub-agent runs" heuristic → corpus index.
2. Two parallel analyses on the shared corpus: v2-validation lens (finding 2) and pattern-characterization lens (Phase 3 doc).
3. Two writeups.
4. Reviewer gates: fresh-agent readability test (Phase 3); counter-evidence sweep (trial finding 2).
5. STATUS.md / INDEX.md updates at session close.

**Source sessions identified by user as starting points:**

- Implementor: `b9da1e6f-b499-4bb5-9888-595d1ce1428f` (~850KB)
- Tester: `a739cf46-b69e-4669-99c8-6f3feee4f83b` (~388KB)

Walk backward from these; identify role from first 1-2 user messages of each basata session in `~/.claude/projects/-Users-gb-github-basata/` from 2026-04-28 onward. User heuristic: ignore sub-agent stretches and long autonomous runs; "two messages before and after user messages" often suffices for context.

**Orchestrator instructions used in basata (recorded for the pattern doc):**

```
Act as the orchestrator - delegate your work.
Parallelize where it makes sense and is possible.
You are responsible for determining the order of tasks to work on.
```

**Open structural question (parked for user):**

- **Corpus placement.** Proposed `phases/phase-3/corpus/basata/` on the rationale that the corpus is *primary evidence* for the Phase 3 doc and trial-finding 2 is a secondary consumer; a future Phase 4 case study would then own its own corpus at `phases/phase-4/corpus/`. Alternative: `corpus/basata/` at track root as a cross-phase asset. Awaiting user call before any extraction.

**What the next session should do:**

1. Re-read this entry plus the methodology proposal above.
2. Resolve the open corpus-placement structural question.
3. If user has signed off on the methodology proposal: write the five methodology files. Stop and confirm before starting analysis.
4. If user has signed off on the methodology files: proceed with the execution sequence (evidence layer first, then parallel analyses, then writeups, then reviewer gates).
5. Do NOT skip ahead to analysis without methodology sign-off — user was explicit on this.

**Status: BLOCKED on user sign-off of the methodology proposal above.**

---

### 2026-04-28 -- Trial finding 1 + v2 skill iteration (overnight)

**What happened:**

- User-flagged failure mode in two real sessions (basata `426257cc` under `/session-resume`; harmonik `a121e7f1` independent, no `/session-resume`). Diagnostic conversation surfaced 8 named failure causes in the v1 skills. Documented in `trial-findings/2026-04-27-skills-too-verbose-and-procedural.md` (first named trial finding).
- User authorized acting on the n=2 signal rather than waiting for n=3-5 (methodology default). Reasoning: signal strength clear; cost of continuing v1 use was actively damaging the alignment the skills were meant to improve.
- Autonomous overnight iteration: research → draft → review → revise pipeline.
- **Round 1 (3 parallel research agents):** I-PASS deep-dive + translation; external-form comparison across medical/military/incident-command sources; anti-anchored fresh draft from user's two-sentence brief alone.
- **Round 2 (synthesis):** v2 draft 1 — 24 + 18 lines (vs v1's 104 + 102).
- **Round 3 (4 parallel reviews):** skeptic-of-fix (with trial-finding context), adversarial completeness (no context), self-application test (produced an actual 17-line handoff from a fake mini-session — strongest evidence of v2 working), plain-language read.
- **Round 4 (revision):** v2 draft 2 — 17 + 15 lines. Highest-leverage edit: collapsed bulleted slots into prose (R1's argument: a smaller schema is still a schema). Other edits: branch + date on first line (R2 freshness/wrong-tree concern); dropped explicit root-file list in resume (R2 nested-doc concern); wording polish (R4).
- Created two new cross-phase subdirectories: `trial-findings/` (one finding so far) and `skill-iterations/` (one iteration so far). Conventions captured at `skill-iterations/CONVENTIONS.md` so future iterations follow the same shape.

**Decisions made (autonomous):**

- `trial-findings/` and `skill-iterations/` placed at track root as cross-phase subdirectories, consistent with `references/`, `scripts/`, `plans/`, `prompts/`. METHODOLOGY.md, CLAUDE.md, INDEX.md updated.
- v2 trial flag: `<!-- PP-TRIAL:v2 YYYY-MM-DD <branch-name> -->`.
- v2 keeps the I-PASS one-word severity tag idea as `green / blocked / broken`.
- v2 has no Decisions Made / Decisions Parked / Open Questions / Out of Scope / Load-Bearing Tokens sections.
- The pre-review v2 cut and post-review v2 cut both kept on disk for traceability; revisions doc attributes each change to the review that surfaced it.

**Decisions made post-overnight (user signed off morning of 2026-04-28):**

- v2 revised drafts approved. Deployed to `~/.claude/skills/session-{handoff,resume}/SKILL.md`. v1 snapshotted at `skill-iterations/v1-baseline/` for revert/comparison.

**Decisions parked (for user):**

1. **Phase 3 question.** Should the trial + iteration work be formalized as Phase 3 (creating `phases/phase-3/`), or kept as cross-phase active forward-work (current placement)? See HANDOFF.md.
2. **Deeper pattern.** harmonik conversation showed the failure mode isn't unique to these skills — pilot-review and other structured protocols produce the same shape. Worth a separate trial finding or research thread; deferred.

**What the next session should do:**

1. Read `HANDOFF.md` (track-local, in `research/planning-protocols/`).
2. If continuing planning-protocols work: pick up the two parked decisions above, or move to other forward-work in `protocol-trial-roadmap.md`.
3. v2 skills are now live. The next real planning session that uses `/session-handoff` and `/session-resume` produces the n=3 trial signal; observe whether the v2 shape holds up or surfaces new failure modes.
