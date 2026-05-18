# Research — Ralph-Loop Prior Art and Counter-Patterns

**Research question:** Stress-test our implementer-reviewer iteration design against published evidence on the same pattern. Find counter-patterns and failure modes we'd otherwise miss.

**Bottom line:** Three findings materially affect the design. **One challenges the user's explicit session-resume choice with strong external evidence.** Flagged as decision-pending.

## F1 — "Ralph loop" naming is a problem

"Ralph loop" was coined by Geoffrey Huntley (July 2025 blog post). Huntley's defining property is **ruthless context reset per iteration**: each iteration starts with a fresh context window; state survives only through the codebase, a `TODO.md` file, and git history. The name references Simpsons character Ralph Wiggum.

The design the user described — **same implementer session resumed across iterations** — is **not** Huntley's ralph loop. It's closer to:

- **Reflexion** (Shinn et al. 2023, arXiv:2303.11366) — verbal self-critique with sliding-window memory.
- **AutoGen's writer/critic/executor** pattern.
- **LangGraph / Google ADK LoopAgent** — generator-critic with hard max-iteration counter.
- **Claude Agent SDK with `maxTurns`** — same idea.

**Implication:** Calling our mode `ralph` in code and spec will confuse readers familiar with Huntley's usage. **Recommendation: rename to something neutral.** Candidates: `review-loop`, `iterate`, `critic`, `reflexion`. The user's original term "ralph loop" can stay in user-facing conversation; the spec/config name should be neutral.

## F2 — Compounded sycophancy & mode-collapse threaten same-session iteration (DECISION-PENDING)

The literature on iteration loops where the implementer's session persists across critique rounds is consistent on a small set of failure modes:

| Failure mode | Evidence |
|---|---|
| **Reviewer over-approval** when implementer and reviewer share a base model — reviewer is biased to approve its own family's output and misses logic errors that come from the same probabilistic patterns. | Reflexion-lineage papers; "Yes Man" effect in agent literature. |
| **Mode collapse / oscillation** — implementer reproduces nearly identical solutions across retries despite feedback. Persistent failure-mode oscillation rather than convergence when feedback is at the wrong abstraction level. | arXiv 2603.26942 "Observability Gap"; IaC feedback-loop study (arXiv 2411.19043) finds effectiveness decays **exponentially** per iteration. |
| **Sycophancy growing across turns** — RLHF-trained models prioritize agreement; implementer capitulates to reviewer even when reviewer is wrong. Effect grows with turn count. | ACL 2025 multi-turn sycophancy benchmark (findings.emnlp.121). |
| **Verdict-schema gaming** — Goodhart-style gaming of structured rubrics; models learn to phrase outputs that match rubric surface features. | Sycophancy / Giskard literature. |

**Critical contrast with Huntley's ralph:** Huntley's *empirical success* of the pattern is partly attributed to the context reset itself — fresh implementer per iteration breaks the sycophancy chain. The Google Developers Blog flags the inverse failure (fresh implementer simplifies away constraints that lived only in prior reasoning); Manus and LangChain's context-engineering writing converge on **rolling summary + structured session-state document** as the middle path.

**Carried-state shape for fresh-implementer-per-iteration is cheap in our case** because we already produce:
- Git diff (the implementer's prior work, exact).
- Reviewer JSON (verdict, flags, notes — anchored to file:line).
- The worktree's current file state.

These three artifacts ARE the durable session-state document. The implementer doesn't need its own session to recall what it did — the diff and reviewer JSON tell it. The session ID adds latent reasoning that the literature flags as a sycophancy attractor more often than as a help.

**Recommendation: switch the default to fresh-implementer-per-iteration.** Session-resume becomes an opt-in mode (e.g., `workflow:ralph-resume` vs `workflow:ralph`). v1 ships fresh-per-iteration; resume-mode lands later when we have evidence it helps a specific case.

**Important — this is a pivot from the user's stated design.** Surfaced as the kerf's pending decision point. User picked session-resume earlier in conversation; cited concern was context-bloat. The research finds a larger concern (sycophancy/mode-collapse) that points the other way. User should weigh and decide before design pass closes.

## F3 — Cap=3 is fine for local defects, dangerous for structural ones

The literature says: iteration works for **local defects** with **observable signal**; fails for **structural defects** or when feedback is mismatched to cause. At cap=3, structural defects exhaust the budget without converging.

**Mitigations to bake in:**

- **Early-exit on APPROVE.** Trivial: if reviewer returns APPROVE, exit immediately, don't run remaining iterations. (Implicit in user's design but make it explicit.)
- **No-progress detector.** Hash the diff between iteration N and iteration N-1. If they're identical or near-identical (Jaccard > 0.9 on changed-file set), exit early to `needs-attention` — don't burn iterations on oscillation.
- **Rubric-anchored verdicts.** Reviewer JSON's `notes` field should require file:line citations for any `REQUEST_CHANGES` flag. Approves require either "all flags resolved" or explicit "no flags raised." Reduces verdict gaming.

**Cap values in real systems for reference:**
- Aider: no public default for reflection-max.
- OpenHands: `MAX_ITERATIONS` ≈ 100.
- Claude Agent SDK `maxTurns`: unlimited by default.
- Google ADK LoopAgent: 15–25 typical.
- Reflexion paper: 3–5 trials.

Our cap=3 is at the low end of Reflexion convention and **two orders of magnitude** below agentic-coding defaults. Defensible if our iteration unit is *whole code-review round* (each round expensive — fresh subprocess + worktree commit + reviewer dispatch). Note in spec.

## F4 — Reviewer-side hardening

If we keep same-base-model reviewer (Claude reviewing Claude), the "Yes Man" effect is real. Mitigations:

- **Harsh-reviewer persona prompt.** Bias the reviewer prompt toward skepticism, not balance.
- **Require evidence in `notes`.** As above — file:line citations.
- **Rotate the reviewer prompt seed per iteration** (small change in opening framing — discourages reviewer copy-pasting prior verdicts).
- **(Future) different model on reviewer side.** Multi-Agent Reflexion (arXiv 2512.20845) finds distinct reasoner personas help when single-agent suffers degeneration of thought. Not v1; carry as a tasks-pass note.

## F5 — `needs-attention` queue is operational debt

OpenHands' `MAX_ITERATIONS`-related issue history (#2121, #6857, #9344) repeatedly surfaces cap-firing operational pain: when caps fire, **downstream triage IS the problem**. Whoever drains the `needs-attention` queue is on the hook for the gnarlier cases.

**Implication for our spec:** the operator-NFR section (C7) needs an explicit obligation: there is no automatic drain of `needs-attention`. Operator either manually reopens or recreates the task. State this so future implementers don't build phantom auto-retry logic.

## Decision summary for design pass

| Open decision | Recommendation | Evidence weight |
|---|---|---|
| Name in spec/config | Not `ralph`. Use `review-loop` or `critic`. | F1 — strong. Huntley's usage conflicts. |
| Session-resume vs fresh-per-iteration | **Default = fresh-per-iteration; resume = opt-in mode.** | F2 — strong external evidence; user pivot pending. |
| Iteration cap | Cap=3, but add early-exit on APPROVE and no-progress detector. | F3 — supports user's cap=3 with safety rails. |
| Reviewer hardening | Harsh persona + evidence-in-notes requirement. | F4 — light external evidence; cheap to do. |
| `needs-attention` drain policy | Operator-only; no auto-retry. State it in spec. | F5 — operational, evidence from OpenHands. |

## Implication for spec drafts

- **C1, C2, C3** must accommodate two iteration shapes: **fresh-implementer** (default) and **session-resume** (opt-in). The Run record's `context` field carries `session_id` only when in resume mode.
- The LaunchSpec's `phase` field distinguishes `implementer-initial`, `implementer-resume`, `implementer-fresh-next-iteration`, `reviewer`. Or simpler: `phase` is `implementer | reviewer` plus `iteration_count`, and a `resume_session_id` field that's present only in resume mode.
- Event-model gets a new event `no_progress_detected` (class O) for the diff-hash early-exit path.
- Spec language: "review-loop mode" or whichever name we land on. The user-facing label can stay "ralph" in conversation, but the on-disk artifact uses the neutral name.

## Sources

- [Geoffrey Huntley — everything is a ralph loop](https://ghuntley.com/loop/)
- [Reflexion paper (Shinn et al., arXiv:2303.11366)](https://arxiv.org/pdf/2303.11366)
- [Observability Gap in LLM coding agents (arXiv:2603.26942)](https://arxiv.org/html/2603.26942)
- [Sycophancy in LLMs: Causes and Mitigations (arXiv:2411.15287)](https://arxiv.org/html/2411.15287v1)
- [Measuring Sycophancy in Multi-turn (ACL 2025)](https://aclanthology.org/2025.findings-emnlp.121.pdf)
- [IaC feedback loop study (arXiv:2411.19043)](https://arxiv.org/abs/2411.19043)
- [Multi-Agent Reflexion (arXiv:2512.20845)](https://arxiv.org/html/2512.20845v1)
- [Google ADK LoopAgent docs](https://google.github.io/adk-docs/agents/workflow-agents/loop-agents/)
- [OpenHands GLOBAL_MAX_ITERATIONS issue thread](https://github.com/All-Hands-AI/OpenHands/issues/2121)
- [Claude Agent SDK loop / subagents docs](https://platform.claude.com/docs/en/agent-sdk/agent-loop)
- [Manus context engineering](https://manus.im/blog/Context-Engineering-for-AI-Agents-Lessons-from-Building-Manus)
