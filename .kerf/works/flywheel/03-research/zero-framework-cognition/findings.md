# Research — Zero-Framework Cognition (Yegge) & the self-managing thesis

> Component: `zero-framework-cognition`. Round-2 research (self-management pivot). Source: sub-agent (sonnet), 2026-05-27. The user's guiding philosophy for the agentic layer.

## TL;DR
- **ZFC core thesis:** every decision involving judgment/classification/ranking/semantic-analysis routes back to the MODEL — never hardcoded in orchestration glue. The harness is a *dumb pipe* (I/O, schema validation, safety policy, mechanical execution); the model is the brain. ("When we reach a decision point, send it back to the model. Don't hack it client-side with regex.")
- **Gas Town extension ("thin harness, fat skills"):** even *workflow control flow* lives in skills + bead-level instructions the agent READS, not in a fat deterministic state machine. Harness = durable persistence + liveness enforcement (GUPP: "if there's work on your hook you MUST run it"), NOT intelligence.
- **The honest tension for flywheel:** Yegge solves context by *fragmenting work so no agent needs long context* — the OPPOSITE of a long-running orchestrator. And failure research shows the danger zone: **"the model experiencing context pressure is the worst judge of whether it needs a reset."** So pure self-management is risky → the ZFC-consistent compromise is a **thin structural trigger that HANDS the decision to the model** (asks it to self-assess/summarize/reset), not a heuristic that decides for it, and not blind trust.

## What you are NOT supposed to build (ZFC)
Keyword/regex routing; ranking/scoring in glue; semantic-complexity inference in the harness; heuristic fallback rules; quality judgment beyond structural schema checks; a state machine that decides "ready/blocked/retry" from heuristic signals; regex on model output to detect completion; a retry scheduler that decides re-dispatch "without asking the model."

## What you rely on the model to do
Classification, selection, ordering, validation, anomaly detection, completion-detection, edge-case handling. Harness handles I/O, schema enforcement, budget/rate/timeout policy, and mechanical execution of decisions the model already made. Lineage: "Smart Endpoints, Dumb Pipes" (Fowler/Lewis) + Karpathy Software 2.0 — the app is glue *between* models, not a second brain on top.

## Concrete prescriptions
Do: gather raw context by I/O only (no inference at collection); call the model for every decision ("which task next?", "is this done?", "what failed?"); validate structure AFTER the model decides (never as a gate before); execute mechanically what it chose; **model stratification** (decompose work into cognitive tiers, route to cheapest capable model Haiku→Sonnet→Opus — Yegge: 50-80% of agent traffic hits premium models unnecessarily); encode ZFC as an agent-facing rule ("ZFC violation!" is sufficient correction). Gas Town adds: molecular workflows (bead-sized chunks w/ acceptance criteria); the agent reads what-to-do-next from the bead/skill, not from a scheduler.

## Context/memory (the part that matters most for flywheel)
Yegge's answer is **structural: fragment work so no single agent needs a long context window** (wisps, molecular chaining; the agent's context is a scratchpad for one molecule; durable state in git/beads). He does NOT advocate the agent managing its own context via summarization/hierarchical memory — he shrinks the work unit so amnesia doesn't matter. **This does NOT directly solve flywheel** (a single long-running orchestrator managing state across hours/days). The flywheel-relevant move is the *compromise* below.

## Failure modes / critiques (anti-anchoring — the user is enthusiastic; here's the counter-case)
- ZFC is expensive (every heuristic → a model call) → requires model stratification to be viable.
- Models frequently *violate* ZFC (revert to heuristic behavior).
- **Off-canonical tool-call cascades:** each wrong call raises P(next wrong call) ~+22.7pp (arXiv 2602.19008) — handing more control to the model means one confused decision can spiral, with no external circuit-breaker.
- **"Accurate failure prediction ≠ effective prevention"** (arXiv 2602.03338): the model can recognize it's failing and keep going anyway.
- Intrinsic self-correction is unreliable, can degrade performance (arXiv 2601.16280).
- → These are exactly why "reset/manage your own context when you see fit" can fail (never resets, or resets badly). **A thin structural trigger + a hard backstop is required.**

## Mapping to flywheel — build / don't build
BUILD (ZFC-consistent): durable bead/event store (harmonik has it); **skill files** encoding "how to triage a failure / pick the next batch / when to reset context" (agent reads + decides); a thin liveness loop (GUPP: if work queued, agent must pick it up); schema-only completion detection (did a bead transition? did events.jsonl emit run_completed?); model stratification (route "what failed?" to Sonnet, "format this" to Haiku).
DON'T BUILD: a heuristic that detects "stuck" by counting retries; a deterministic state machine deciding "switch to context-reset mode" from token counts *and acting on it itself*; regex on agent output for completion/errors; a retry scheduler deciding re-dispatch without asking the model.

**The flywheel synthesis (YOUR-INTERPRETATION, ZFC-consistent):** keep the agent in charge of judgment (what to dispatch, what's wrong, what to keep on reset). Use a **thin structural trigger** for context pressure — a token/% threshold that *prompts the model to self-assess and reset* (writing notes first) — rather than (a) the harness deciding the reset itself (un-ZFC, "rarely correct") or (b) trusting the model to notice unprompted (fails per the research). The harness's only hard power is the **backstop**: if the model ignores the prompt past a hard limit, force a notes-flush + reset mechanically (a safety net, not the normal path).

## Sources
steve-yegge.medium.com: zero-framework-cognition · the-future-of-coding-agents · welcome-to-gas-town. moonsat.medium.com/thin-harness-fat-skills (Apr 2026). latent.space Yegge manifesto. github.com/steveyegge/vc. arXiv 2602.19008, 2602.03338, 2601.16280.
