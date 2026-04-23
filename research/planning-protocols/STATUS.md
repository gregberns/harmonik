# planning-protocols -- Status

> Current phase, active work, and session history. Every session updates this file before ending.

Last updated: 2026-04-23 (Phase 2 main session closed)

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

- **1A -- Corpus discovery.** 4 per-project catalogs at `01-corpus/<project>/_catalog.md`.
- **1A -- Re-classification.** Whole-session classification was too coarse; re-classified all 195 sessions by `n_human_text_turns` signal.
- **1B -- Session-type discriminator.** `references/session-type-discriminator.md`.
- **1C -- Corpus extraction.** 10 sessions extracted to `01-corpus/<project>/<session-id>.md`. Compression ratio 0.7–14% vs raw JSONL. Extraction script at `scripts/extract_dialog.py`.
- **Tried-protocols catalog.** `references/tried-protocols.md` -- 5 interaction variants from user practice.
- **1D -- Multi-lens analysis.** 6 parallel sub-agents ran: decision-delegation, misaligned-assumption, writing-load, form-vs-content, topic-tree, context-switch. Outputs at `02-analysis/<lens>.md`.
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

Variants 2-5 are out of core scope but will be captured in `references/tried-protocols.md` for comparative analysis. This was user-approved as "list of things that have been tried."

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

**Subsequent work:** wrote extraction script (`scripts/extract_dialog.py`), validated on small session, ran on 10 primary+borderline sessions. Wrote `references/session-type-discriminator.md` and `references/tried-protocols.md`. Updated `01-corpus/INDEX.md`.

**What the next session should do:**
1. Read the Phase 2 kickoff prompt at `phase-2-kickoff-prompt.md` OR have it pasted in by the user.
2. Follow its instructions (read order: CLAUDE.md → STATUS.md → research-statement.md → METHODOLOGY.md).
3. Execute Phase 2 methodology in research-statement §7, starting with Step 1 (criteria interrogation).

**Two mid-session course corrections in Phase 1 (important context for Phase 2):**

1. The user flagged the **local-maxima risk** before Phase 1E drafting. Observed patterns from Phase 1 could anchor Phase 2 onto incremental refinement instead of discovering genuinely different approaches. Research statement was restructured with dimensions-of-variation first, counter-pattern hypotheses (§6), and mandated external-source-first Phase 2 methodology (§7) to guard against this.

2. The user flagged the **evaluation criteria are uncertain**. They do not know what the correct evaluation criteria for planning protocols are. Research statement §2 now foregrounds this; §7 Step 1 mandates criteria interrogation before external-source work; "a durable evaluation framework" is named as a potentially-more-valuable output than specific protocol recommendations.

### 2026-04-23 -- Phase 2 main session (Steps 1-6 + Step 7 draft)

**What happened:**

- Executed the Phase 2 methodology from research-statement §7 in one session. Produced the Phase 2 main output (`phase-2-findings.md`) plus a first-class durable deliverable (`evaluation-framework.md`) plus a kerf-integration DRAFT (`phase-2-kerf-integration-draft.md`).
- **Step 1 (criteria interrogation).** Three parallel sub-agents with distinct challenge frames: rival framings, operationalization audit, empirical-evaluation design. Synthesis at `02-analysis/evaluation-criteria-refinement.md`. Three sub-analyses at `02-analysis/evaluation-criteria-refinement.sub-*.md`. No fundamental issues surfaced; Phase 2 proceeded without user pause. Refinements: provisional criteria replaced by pair-graph of required-paired metrics; multi-framing scoring requirement added (Framings A/B/C); formal evaluation framework elevated to first-class deliverable.
- **Step 2 (external-source pass).** 10 parallel sub-agents, one per domain, outputs at `02-analysis/external-sources/<domain>.md`. Domains: pair-programming, socratic-method, medical-handoffs, design-review, negotiation-mediation, incident-command, pilot-controller, therapy-intake, consulting-discovery, military-briefings. ~70 candidate protocols extracted.
- **Step 3 (counter-pattern generation).** Single sub-agent steel-manned 8 counter-hypotheses from research-statement §6 into specific protocol instances. Output at `02-analysis/counter-pattern-candidates.md`.
- **Step 4 (unified catalog).** Single sub-agent consolidated observed + unexplored + external + counter-pattern into 87 distinct protocols on shared 8-field schema. Output at `02-analysis/unified-protocol-catalog.md`. Surfaced gaps (scope-decomposition dependency-awareness; research-scoping question-quality).
- **Step 4.5 (corpus-signal filter) DEFERRED.** The evaluation framework specifies this as required before Step 5 ranking. It requires ~1–2 days of scripting over all 195 sessions; treated as user-authorization-gated per the "no code" locked choice interpretation. All Step 5 rankings carry `[filter-dep]` flags for candidates whose ranking needs filter validation.
- **Step 5 (reviewer-challenged evaluation).** Six parallel reviewer sub-agents: ergonomics, cognitive-load, fatigue-robustness, task-type-adaptability, challenge-observed-patterns (local-maxima guardian), and multi-framing. Outputs at `02-analysis/reviewer-*.md`. Strong cross-frame convergence on winners and losers.
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
