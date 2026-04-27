# planning-protocols/ -- Research Track Entry Point

Researching **planning protocols**: reusable shapes of human-agent interaction during the idea -> plan -> spec phase of software work. The research goal is to reduce the human-attention cost of reaching alignment between human intent and agent output, while preserving the plan quality that makes implementation cheap.

"Planning protocols" is a working term. Do not rename without user sign-off.

## Read order for every session

1. [STATUS.md](STATUS.md) -- Current phase, what's done, what's next, session history.
2. [METHODOLOGY.md](METHODOLOGY.md) -- The process we are following. Do not diverge without user sign-off.
3. Whatever STATUS.md tells you to read next.

## Hard rules

- **Do not reopen "Locked-in choices"** in METHODOLOGY.md without user sign-off.
- **Do not overwrite prior artifacts.** Append to them, or create dated new files. Prior research is not a scratchpad.
- **Do not skip phases.** The methodology is deliberately sequenced -- extraction before pattern analysis. Jumping ahead pollutes the evidence.
- **Append a session entry to STATUS.md** at session end so the next session knows what happened and why.
- **Use sub-agents liberally** when work fans out (per-project, per-lens). Default is 3+ parallel sub-agents for any corpus-wide task. User explicitly wants parallelism here.
- **Sub-agent prompts must be self-contained.** Include the research question, the sub-phase goal, and any locked choices. Do not assume the sub-agent has read this file.
- **Term "planning protocols"** is the chosen working term -- don't rename it or invent a replacement without user sign-off.

## What this track contains

**Entry / governance** (root-level — read these first):
- `CLAUDE.md` -- this file
- `METHODOLOGY.md` -- research question, phased approach, rules, folder conventions
- `STATUS.md` -- live state, session history
- `INDEX.md` -- navigation map (reading paths by audience, full document map)

**Active forward-work** (root-level):
- `protocol-trial-roadmap.md` -- current roadmap; calibration items to watch; layered-in additions parked behind trial signal

**Per-phase content** (`phases/phase-N/`):
- `phases/phase-1/` -- Phase 1 deliverable + Phase 1 work products (corpus, lens analyses, sub-step outputs)
  - `research-statement.md`, `session-type-discriminator.md`, `tried-protocols.md`
  - `corpus/` -- extracted planning session dialogs (input data, 1A + 1C)
  - `analysis/` -- Phase 1D lens reports
- `phases/phase-2/` -- Phase 2 deliverables + Phase 2 step outputs
  - `findings.md`, `evaluation-framework.md`, `kerf-integration-draft.md`
  - `analysis/` -- Step 1-5 sub-agent outputs (criteria refinement, external sources, counter-patterns, unified catalog, reviewers)

**Cross-phase subdirectories**:
- `references/` -- external/imported source material (e.g., starting-point brainstorms)
- `scripts/` -- tooling reusable across phases (e.g., `extract_dialog.py`)
- `plans/` -- forward-work plans + their reviews (parked work; e.g., `step-4.5-plan.md`)
- `prompts/` -- paste-in session-starter prompts (NOT a place for `/session-handoff` skill output)

## Where new artifacts go

When producing a new artifact, place it by *purpose*, not chronology:

- **Per-phase deliverable** (a phase's canonical synthesis output) → `phases/phase-N/<name>.md`.
- **Step-internal analysis output** (a sub-agent's findings, one of many in a phase's process) → `phases/phase-N/analysis/`.
- **Phase input that isn't itself analysis** (e.g., classifier definitions, taxonomies discovered during a sub-step) → `phases/phase-N/` directly.
- **Cross-phase deliverable** (truly spans phases — rare; the active forward-work roadmap is one example) → root.
- **Forward-work plan or its review** → `plans/`. If the plan executes, its output graduates to the relevant per-phase location.
- **Paste-in prompt for a fresh session** → `prompts/`. The filename `HANDOFF.md` is reserved for the `/session-handoff` skill convention.
- **External source material being imported** → `references/`.

Root must stay lean: governance + navigation + active forward-work only. New phases get their own `phases/phase-N/` directory; cross-phase work products go in the cross-phase subdirectories above.
