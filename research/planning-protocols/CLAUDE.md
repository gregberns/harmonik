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

**Entry / governance** (read these first):
- `CLAUDE.md` -- this file
- `METHODOLOGY.md` -- research question, phased approach, rules, folder conventions
- `STATUS.md` -- live state, session history
- `INDEX.md` -- navigation map (reading paths by audience, full document map)

**Top-level deliverables** (the canonical research outputs — stay at root):
- `research-statement.md` -- Phase 1 final output, Phase 2 briefing
- `phase-2-findings.md` -- Phase 2 main deliverable
- `evaluation-framework.md` -- Phase 2 durable instrument
- `phase-2-kerf-integration-draft.md` -- Phase 2 deliverable, DRAFT
- `protocol-trial-roadmap.md` -- active forward-work roadmap

**Subdirectories** (purpose-bounded):
- `01-corpus/` -- extracted planning session dialogs (input data)
- `02-analysis/` -- analysis outputs (Phase 1D lenses + all Phase 2 step outputs)
- `references/` -- external/imported source material
- `scripts/` -- tooling (extraction, etc.)
- `plans/` -- forward-work plans + their reviews (parked work; e.g., `step-4.5-plan.md` and reviews)
- `prompts/` -- paste-in session-starter prompts (e.g., `phase-2-kickoff-prompt.md`, `deep-dive-prompt.md`)

## Where new artifacts go

When producing a new artifact, place it by *purpose*, not chronology:

- **New top-level deliverable** (a self-contained document anyone exploring the project should see) → root.
- **Step-internal analysis output** (a sub-agent's findings, one of many in a phase) → `02-analysis/`.
- **Forward-work plan or its review** → `plans/`. If the plan becomes active and produces output, that output graduates to the appropriate purpose location.
- **Paste-in prompt for a fresh session** → `prompts/`. Do not put `HANDOFF.md` here — that name is reserved for the `/session-handoff` skill convention.
- **External source material being imported** → `references/`.

If unsure between root and `02-analysis/`, ask: "would a returning agent need to find this on day one, or only when investigating a specific step?" Day-one → root; step-specific → `02-analysis/`.
