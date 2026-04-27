# Planning Protocols -- Research Methodology

> How we are researching planning protocols. This is the durable process document. Session-to-session work follows what is written here. Diverging from it requires user sign-off.

## Working research question

> **What planning protocols minimize the human-attention cost of reaching a correct, implementable plan, while maintaining alignment between human intent and agent output?**

"Planning protocols" = reusable shapes of human-agent interaction -- micro-patterns of turn structure, question-asking, decision-delegation, scope-bounding, alignment-checking -- used during the idea -> plan -> spec phase of software work.

## Context / why this research exists

The harmonik project has found that planning is the dominant human-time cost: 10-20 hours iterating to reach a spec, with implementation comparatively cheap once the spec is right. Kerf (the project's planning CLI) already implements staged passes and reviewer sub-agents; implementation quality is strong when the plan is strong.

The next leverage is the *planning dialog itself*: how the human and the agent interact turn-by-turn. User-reported pain points include agent misaligned assumptions, agent deferring trivial decisions to the human, and the human doing too much of the writing. These suggest protocol-level (not just process-level) improvements.

## Evaluation criteria (priority order)

1. **Human writing / clarification effort.** Amount of human typing required to reach alignment.
2. **Targeting of agent questions.** Do agents ask the right questions, or punt everything?
3. **Correction-cycle count.** How many round-trips to surface and correct a misaligned agent assumption?
4. **Agent autonomy on trivia.** Does the agent handle implementation-detail decisions itself rather than asking?
5. **Human context-switch load.** Can the human take short turns separated by longer agent passes?

Not primary (already good): total wall-clock planning time, spec completeness, downstream implementation quality.

## Out of scope

- Implementation phase (post-spec). Already works.
- Kerf's execution primitives (jigs, beads, works). Only the interaction shape *within* them is in scope.
- Non-coding agent contexts.
- Generic agent-reliability research (tool design, model selection, etc.).

## Locked-in choices

Reopening these requires user sign-off.

1. **Empirical-first.** Real transcripts from `~/.claude/projects/` are the primary data source. External literature is secondary.
2. **Working term.** "Planning protocols" is the term.
3. **Test environment.** Harmonik is the primary context; findings feed back into kerf.
4. **Two-stage research.**
   - **Phase 1** (this track, current) scopes the problem and produces a focused **research statement**.
   - **Phase 2** (new session, future) uses the research statement to do deep investigation.
5. **Extraction-before-analysis.** The corpus is extracted and cataloged neutrally before any pattern-finding begins. This prevents early hypotheses from biasing extraction.
6. **No code.** This research writes documents, not code. Scripting in service of extraction/analysis is fine (small Python, shell); implementation for harmonik is out of scope here.

## Phased approach

### Phase 1 -- Scope the research (CURRENT)

The goal of Phase 1 is a **research statement** that a fresh session can use as the starting point for deep research, without needing to reconstruct the thinking that produced it. It should contain:

- Sharpened research question
- Candidate planning protocols identified empirically (with representative transcript excerpts)
- Refined evaluation criteria
- Hypotheses ranked by expected leverage
- Proposed Phase 2 methodology
- Open questions Phase 2 should answer

**Phase 1 sub-phases:**

- **1A -- Corpus discovery.** Catalog the sessions in `~/.claude/projects/{kerf, harmonik, machine-setup, secure-dev}` and related dirs. For each session: metadata, size, classification (planning / mixed / implementation / other), 1-3 sentence summary, "emblematic" flag if it looks especially rich. Output: `phases/phase-1/corpus/<project>/_catalog.md` per project, plus `phases/phase-1/corpus/INDEX.md` aggregating.
- **1B -- Session-type discriminator.** Investigate whether tool-call signatures mechanically distinguish planning from implementation sessions. If so, document the heuristic. Output: `phases/phase-1/session-type-discriminator.md`. (This is user-flagged "excellent research for another day" -- document findings even if small.)
- **1C -- Corpus extraction.** For each session flagged as planning-relevant, produce a clean dialog-only extract (human and agent text, no tool calls, no system events). Output: `phases/phase-1/corpus/<project>/<session-id>.md`.
- **1D -- Multi-lens analysis.** Apply several analytical lenses to the extracted corpus in parallel. Each lens is its own sub-agent. Candidate lenses (expand as warranted):
  - **Decision-delegation.** When did the agent ask vs decide? What should have gone the other way?
  - **Misaligned-assumption.** Where did agent drift show up? How many turns before the human caught it?
  - **Topic-tree.** What branches were explored? Which weren't? Where did conversations fork?
  - **Form-vs-content.** Does the shape of discussion (turn length, question style, batching) predict alignment speed independently of topic?
  - **Writing-load.** Where did human writing pile up? Could a better-structured agent question have compressed it?
  - **Context-switch.** How long are the runs of "agent working without human input"? Can they be longer?
  Output: `phases/phase-1/analysis/<lens>.md` per lens.
- **1E -- Synthesis.** Cross-cut the lens findings to identify candidate planning protocols. Draft the research statement. Output: `phases/phase-1/research-statement.md`.

### Phase 2 -- Deep research (CLOSED 2026-04-23)

Used the Phase 1 research statement as its only required input. Executed in seven steps; produced three top-level deliverables and a step-output catalog.

**Phase 2 steps:**

- **Step 1 -- Criteria interrogation.** Three parallel sub-agents (rival framings; operationalization audit; empirical-evaluation design). Synthesis at `phases/phase-2/analysis/evaluation-criteria-refinement.md`. Spinoff: `phases/phase-2/evaluation-framework.md` elevated to first-class deliverable.
- **Step 2 -- External-source pass.** 10 parallel sub-agents, one per domain. Outputs at `phases/phase-2/analysis/external-sources/<domain>.md`. ~70 candidate protocols extracted.
- **Step 3 -- Counter-pattern generation.** Single sub-agent steel-mans 8 counter-hypotheses from research-statement §6 into protocol instances. Output: `phases/phase-2/analysis/counter-pattern-candidates.md`.
- **Step 4 -- Unified catalog.** Single sub-agent consolidates observed + unexplored + external + counter-pattern into 87 protocols on shared 8-field schema. Output: `phases/phase-2/analysis/unified-protocol-catalog.md`.
- **Step 4.5 -- Corpus-signal filter.** DEFERRED pending user authorization. Plan + reviews on disk in `plans/`.
- **Step 5 -- Reviewer-challenged evaluation.** Six parallel reviewer sub-agents (ergonomics, cognitive-load, fatigue-robustness, adaptability, challenge-observed, multi-framing). Outputs at `phases/phase-2/analysis/reviewer-<frame>.md`.
- **Step 6 -- Ranked recommendations.** Main-thread synthesis at `phases/phase-2/findings.md`.
- **Step 7 -- Kerf integration draft.** DRAFT only at `phases/phase-2/kerf-integration-draft.md`. Not finalized; user review pending.

### Phase N pattern (for any new phase)

When opening a new phase, create `phases/phase-N/` with at minimum:
- One or more deliverable file(s) at the phase root (e.g., `phases/phase-N/findings.md`).
- An `analysis/` subdirectory for sub-agent step outputs, intermediate artifacts, and reviewer outputs.
- Any phase-specific reference material that isn't a step output (taxonomies, classifiers) at the phase root, not in `analysis/`.

Cross-phase work products (corpus, scripts, references, plans, prompts) stay in their cross-phase subdirectories at the track root, not duplicated per phase.

## Session-safety rules

This track will span many sessions. Each session must preserve prior state.

1. **Read STATUS.md first.** Every session starts there.
2. **Never overwrite prior research artifacts.** New findings go in new files or dated append-sections, never rewrites. If an artifact is wrong, add a correction, don't delete.
3. **Never reopen locked choices** without explicit user sign-off in the current session.
4. **Advance STATUS.md at session end.** Add a session-history entry describing what was done, what's next, and any decisions made.
5. **Sequence the phases.** Extraction (1C) must precede analysis (1D). Cataloging (1A) must precede extraction (1C). 1B can run parallel with 1A.
6. **No renaming load-bearing terms** ("planning protocols", the sub-phase names) without user sign-off.

## Sub-agent usage patterns

- **Fan-out per project.** During corpus discovery (1A) and extraction (1C), one sub-agent per project directory. They work on disjoint data and cannot collide.
- **Fan-out per lens.** During analysis (1D), one sub-agent per analytical lens. They share the corpus read-only.
- **Reviewer between sub-phases.** After a sub-phase completes, optionally spawn a reviewer sub-agent to sanity-check outputs (gaps, inconsistency, overclaim) before advancing STATUS.md.
- **Self-contained prompts.** Every sub-agent prompt MUST include: the research question, the sub-phase goal, any locked choices it needs to respect, and the absolute path of the methodology file for reference. Sub-agents do not inherit conversation context.
- **No sub-agent writes to another sub-agent's file.** If two sub-agents would write to the same target, split the target into per-sub-agent slices and aggregate after.
- **Sub-agents can sub-delegate.** If a sub-agent's workload is large (e.g., secure-dev 131 sessions), it can spawn its own sub-agents. Document the delegation in its output.

## Folder conventions

```
planning-protocols/
  -- Entry / governance (root) --
  CLAUDE.md                            -- entry point for agents
  METHODOLOGY.md                       -- this file
  STATUS.md                            -- live state and session history
  INDEX.md                             -- navigation map (reading paths, document map)

  -- Active forward-work (root) --
  protocol-trial-roadmap.md            -- current roadmap; calibration items; layered-in additions

  -- Per-phase content --
  phases/
    phase-1/
      research-statement.md            -- Phase 1 final output (produced in 1E)
      session-type-discriminator.md    -- 1B output
      tried-protocols.md               -- 1A output (interaction-variant taxonomy)
      corpus/                          -- extracted planning sessions
        INDEX.md                       -- aggregated catalog across projects (written last)
        <project>/
          _catalog.md                  -- per-project session catalog (1A)
          <session-id>.md              -- cleaned dialog-only extract (1C)
      analysis/                        -- Phase 1D lens reports
        <lens>.md
    phase-2/
      findings.md                      -- Phase 2 main deliverable (Step 6)
      evaluation-framework.md          -- Phase 2 durable instrument (Step 1 spinoff)
      kerf-integration-draft.md        -- Phase 2 deliverable (Step 7), DRAFT
      analysis/                        -- Phase 2 step outputs
        evaluation-criteria-refinement.md             -- Step 1
        evaluation-criteria-refinement.sub-*.md       -- Step 1 sub-analyses
        external-sources/<domain>.md                  -- Step 2
        counter-pattern-candidates.md                 -- Step 3
        unified-protocol-catalog.md                   -- Step 4
        reviewer-<frame>.md                           -- Step 5

  -- Cross-phase subdirectories (root) --
  references/                          -- imported/linked source material
  scripts/                             -- tooling (e.g., extract_dialog.py)
  plans/                               -- forward-work plans + their reviews
  prompts/                             -- paste-in session-starter prompts
                                          (NOT a place for /session-handoff outputs)
```

### Placement rules

When producing a new artifact, route it by *purpose*:

- **Per-phase deliverable** (a phase's canonical synthesis output) → `phases/phase-N/<name>.md`.
- **Step-internal analysis output** (a sub-agent's findings, one of many in a phase's process) → `phases/phase-N/analysis/`.
- **Phase input that isn't itself analysis** (e.g., classifier definitions, taxonomies discovered during a sub-step) → `phases/phase-N/` directly.
- **Cross-phase deliverable** (truly spans phases — rare; the active forward-work roadmap is one example) → root.
- **Forward-work plan or its review** (parked, awaiting authorization) → `plans/`. If the plan executes, its output graduates to the relevant per-phase location.
- **Paste-in prompt for a fresh session** → `prompts/`. The filename `HANDOFF.md` is reserved for the `/session-handoff` skill convention.
- **External source material being brought in** → `references/`.
- **Tooling reusable across phases** → `scripts/`.

Root must stay lean: governance + navigation + active forward-work only. If a returning agent shouldn't see a file on day one, it belongs in a subdirectory.

## What NOT to do

- Do not start pattern analysis before extraction is complete. The evidence shapes the questions.
- Do not conflate this research with harmonik spec work. Research findings are input to spec work; they are not specs.
- Do not delegate the final research statement (1E) fully to a sub-agent. The synthesis should be done in the main conversation with the user present to redirect.
- Do not exhaustively catalog very large projects (e.g., secure-dev 131 sessions) when sampling is sufficient. Document sampling choices.
- Do not include full transcripts in this repo. Extract dialog-only text; original JSONL files stay in `~/.claude/projects/`.
