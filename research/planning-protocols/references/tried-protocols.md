# Tried Planning / Dispatch Protocols

> A catalog of human-agent interaction shapes that are empirically present in the user's existing Claude Code sessions. Not all are "planning protocols" in the narrow sense — some are post-planning dispatch or orchestration — but together they form a taxonomy of attempted approaches that comparative analysis will contrast.

Captured 2026-04-23 during sub-phase 1A. Sources: harmonik, kerf, machine-setup, secure-dev session transcripts under `~/.claude/projects/`.

This list is discoveries-from-data, not prescriptions.

## 1. Planning dialog

The shape we want to study most. Human and agent iterate turn-by-turn to refine an idea into a plan. Characterised by:
- Substantive opening message framing intent, context, or a question
- Many human-authored text turns (`ht >= 15` in our corpus, often 20-40)
- Tool activity spread across turns — Read-heavy, moderate Task/Agent, low Write/Edit
- Often produces spec fragments, decision decisions, or a plan document at the end

**Examples in corpus:** 79a42399 (secure-dev), 38415843 (kerf), c6d1bd16 (secure-dev), 3bf5774c (harmonik), f588ff0c (harmonik), d1704aa0 (secure-dev), 2a50e0fc (machine-setup, borderline).

**What seems to work** (preliminary, pending 1D analysis):
- Rich opening messages that share context and constraints up front
- Back-and-forth to surface misaligned assumptions
- Human provides strong framing; agent provides options

**What seems to cost human time** (preliminary):
- Agent asks about trivial implementation details the human doesn't care about
- Agent makes assumptions that don't surface until several turns later
- Human re-explains things that could have been inferred from context

## 2. Controller orchestration

Human writes a directive positioning the agent as a *controller* that coordinates other agents (typically ntm panes). Dialog-dense by turn count but qualitatively different from planning — the human is directing a running system, not co-designing.

**Opener signature:** `"You are the controller agent for session <X>. Current agents in this session: ..."`

**Examples in corpus:** b7eca5d2 (secure-dev, 59 turns), 3fb3dc80 (kerf, 42), 69050eec (kerf, 24), 7ff17283 (kerf, 11).

**Characteristic turns:** status updates, mid-course corrections, questions about pane state, requests for the controller to redirect workers.

**Note:** Multi-message structured directives inflate the turn count — the 59-turn b7eca5d2 opens with the controller directive split across ~5 user events (headers, bullet lists). True "conversations" in these sessions are fewer than the raw count suggests.

## 3. Autonomous dispatch

Single directive, then the agent runs without human input. The dominant pattern in secure-dev (~100 of 133 sessions).

**Opener signature:** Two templates observed —
1. `"study specs/ and pick the most important thing to do"` + test/commit rules
2. `"Study .scratch/fix_plan.md and pick the most important thing to do"` + test/commit rules

Both explicitly include instructions like "Never ask the human questions or wait for input."

**What this protocol represents in practice:** the user's solution to decision-delegation under time constraints — pre-solve the question "which decisions should the agent make autonomously?" by forcing *all* remaining decisions to the agent and removing the conversation entirely. The planning dialog happened in a *prior* session (possibly days earlier) that produced `specs/` or `.scratch/fix_plan.md`; this session is the execution-phase instrument.

**Open question for analysis:** What was the cost/benefit of this approach? Sessions that use it are fast to launch but may drift from intent in ways that require restart. A good analysis lens would trace: of the template-dispatch sessions, how often did the human have to interrupt or restart?

## 4. Context-dump

Few human text turns (often 4-8), but each is very long (thousands of characters). Agent produces many small responses. Less like a dialog, more like a briefing-plus-extended-research-session.

**Examples:** 13493c8d (harmonik, 5 human turns of 5294 / 1903 / 3441 chars; 164 assistant responses). Often the opening or founding-vision session for a new project.

**What this protocol represents:** the human spends time upfront writing a detailed brief rather than iterating turn-by-turn. Agent's work is more "respond to the brief" than "co-design."

**Potential advantage:** minimizes context-switch load — human writes once, agent runs long. Matches one of the hypothesized research goals (fewer but richer human turns).

**Potential cost:** higher up-front cognitive load on the human; no correction loop for misaligned assumptions until much later; less opportunity to shape the trajectory.

## 5. Session-recovery handoff

Opening message is `"# Session Recovery Context"` + reference to a prior checkpoint. Used when a long-running session was interrupted and a new session resumes.

**Examples:** 729dad16 (kerf).

**What this protocol represents:** the user's solution to "how do I carry state across sessions without losing context?" The agent gets a structured catch-up payload and picks up where the prior session stopped.

**Relevance:** Not a planning protocol itself, but it's adjacent — the moments *between* sessions are where the most context is at risk of being lost, and the handoff-message shape deserves its own study.

## Cross-cutting note

The user has already empirically developed a **taxonomy of interaction modes** without labeling them as such. The research reframes from "discover new planning protocols" to "formalize and compare the modes that are already in use, and find out when each is appropriate vs when it's being used by default." That's a sharper phase-2 research question than the Phase-1-opening framing.

## To add as analysis progresses

Potential entries worth watching for during 1D:
- **Socratic elicitation.** Agent asks a sequence of clarifying questions rather than proposing a plan. Is this present? Is it faster or slower than proposal-then-correct?
- **Parallel reviewer.** Human proposes; N review agents critique in parallel; human reviews synthesis. (Kerf uses this structurally; need to see how it shows up in dialog.)
- **Deferred-decision batching.** Agent lists all open decisions at end of round rather than interrupting mid-exploration. Present in any session?
- **Scope-decomposition.** Human opens with "plan this at 3 levels of detail" or similar. Seen in any opener?
