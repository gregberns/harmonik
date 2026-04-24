<img src="https://r2cdn.perplexity.ai/pplx-full-logo-primary-dark%402x.png" style="height:64px;margin-right:32px"/>

# I'm just a bit into The Three Body Problem (book). Don't ruin anything for me, but I'm wondering...

With two objects (sun and planet) the system can behave rather consistently. But I've heard a third object can cause unpredictibility.
Maybe this is similar to the way when you are trying to analyze a system and there are two input variable that its helpful to hold one stable - otherwise the combinations can be a problem to work with.

As I've been working with agent systems, it is easy to take your 'scientist hat' off and ask for the world, hoping that somehow the laws have changed or problem solving's rules are some how different.
Instead the opposite is true, its important to more rigorously think through how components relate, behave together, etc.

I've been wondering as we leverage AI agents more, how do we.... make them more consistent by making ourselves more consistent.

Any thoughts around that?

Yes — that intuition is pointing in a very useful direction. A two-body system can often be reduced to a clean relationship, while a third interacting body can make the system sensitive to small differences in starting conditions, which is a classic source of chaos and unpredictability.[^1_1][^1_2]

## The useful analogy

What you’re describing in systems work is basically this: once you add another moving part, you often stop being able to reason about each piece in isolation and have to reason about the *relations* among pieces instead. In practice, that means “more variables” is not just more complexity, but sometimes a different category of complexity entirely.[^1_3][^1_4]

That maps well to agent systems. A single agent can seem predictable in a narrow task, but once you have multiple agents, their interactions, timing, memory, and shared context can create emergent behaviors that are harder to forecast than any one agent alone.[^1_5][^1_3]

## Making agents more consistent

The best way to make AI agents more consistent is often to make the *system around them* more consistent. That includes clearer role boundaries, narrower task scopes, shared terminology, fixed evaluation criteria, and repeatable workflows, so the agent is not constantly guessing what game it is playing.[^1_6][^1_7]

A practical principle is: reduce degrees of freedom before you increase intelligence. In other words, constrain the input space, standardize prompts and tool contracts, and make the agent choose among a smaller set of well-defined actions rather than improvising across a huge ambiguous space.[^1_8][^1_9]

## Making yourself more consistent

Your deeper point is the important one: the agent often mirrors the *shape of the human system* around it. If your own goals, naming conventions, priorities, and decision rules shift from moment to moment, the agent is being asked to stabilize an unstable environment.[^1_9][^1_6]

So “making ourselves more consistent” means building habits like:

- Defining the problem before asking for a solution.
- Keeping one stable objective per session or per task.
- Using the same terms for the same concepts.
- Writing down constraints, success criteria, and tradeoffs instead of holding them loosely in your head.
- Separating exploration from execution, so the agent does not have to infer whether you want brainstorming or commitment.

That’s less about being rigid and more about being legible. Agents do better when the human system has fewer hidden variables.[^1_7][^1_8]

## A framing I like

Think of the human as the control surface and the agent as the actuator. If the control surface is noisy, contradictory, or changing mid-flight, the actuator looks “inconsistent” even when it is just responding faithfully to unstable inputs.[^1_3][^1_5]

So the goal is not to make agents magically deterministic. It is to make the whole coupled system — person, prompt, memory, tools, and workflow — more stable and easier to predict.[^1_4][^1_7]

## A simple practice

One concrete approach is to create a “stable operating mode” for yourself and your agents:

- One task type per prompt.
- One preferred output format.
- One naming scheme.
- One source of truth for context.
- One explicit review step before action.

That won’t remove uncertainty, but it can turn a chaotic interaction into a bounded one, which is usually the real win.[^1_8][^1_5]

Would you like me to turn this into a practical checklist for designing more consistent AI agent workflows?
<span style="display:none">[^1_10][^1_11][^1_12][^1_13][^1_14][^1_15]</span>

<div align="center">⁂</div>

[^1_1]: https://thevarsity.ca/2025/02/23/is-the-three-body-problem-unpredictable/

[^1_2]: https://www.polygon.com/24105247/3-three-body-problem-what-why-unsolvable-netflix-liu-cixin/

[^1_3]: https://openreview.net/forum?id=p5NfJAQGOn

[^1_4]: https://arxiv.org/abs/2312.11943

[^1_5]: https://www.scribd.com/document/971438046/doc4

[^1_6]: https://www.thenoah.ai/resources/blogs/why-semantic-consistency-is-key-to-scaling-ai-decisions

[^1_7]: https://www.anthropic.com/research/building-effective-agents

[^1_8]: https://galileo.ai/blog/ai-agent-reliability-metrics

[^1_9]: https://www.linkedin.com/posts/unlearn-dev_your-ai-agent-isnt-inconsistent-your-instructions-activity-7439708363044724736-IYK1

[^1_10]: https://www.gregasquith.com/blog/why-ai-agents-are-powerful-but-not-consistent

[^1_11]: https://www.reddit.com/r/AI_Agents/comments/1mqu13p/anyone_else_struggling_with_consistency_across/

[^1_12]: https://www.youtube.com/watch?v=zJXfnhrUQGE

[^1_13]: https://www.linkedin.com/pulse/dancing-chaos-unveiling-unexpected-links-between-ai-three-body-vohal-te2bc

[^1_14]: https://www.technologyreview.com/2019/10/26/132171/a-neural-net-solves-the-three-body-problem-100-million-times-faster/

[^1_15]: https://academic.oup.com/mnras/article/494/2/2465/5823775


---

# I'm unfamiliar with the control surface and actuator example. Could you provide a little more context.

For now when were talking about interacting with agents lets assume this is in the context of building software with coding agents.
In this case, I've gotten quite consistent and have a good process where I work with the agent through a plan, providing a lot of context, which results in a good spec, which is broken into tasks, and each task is implemented in its own session. Additionally, I've added a lot of rules and guide rails to the systems to ensure they know what they need.

But, my biggest challenge now is that when I'm building out these systems, I spend a ton of time working through all the little details with the agent during the planning phase to really tune in on the details - so they aren't missed at implementation. This takes an incredible amount of time. It does appear to help build exactly what I want - but its at a very high cost of not working on additional tasks.

So if the workflow is Idea -> Plan -> Spec -> Implement -> Test, I've got the last half, but how can we improve or speed up, or bring consistency to the first half? Or is the refinement process with the agent required to specify everything? If so, how can we improve that and is there a way to use them to make the process faster?

Yes — and in your case the bottleneck is probably not “the agent needs every detail spelled out,” but “your planning layer is doing too much precision work too early.” Anthropic explicitly recommends starting with the simplest workable structure, using predefined workflows when tasks are decomposable, and only adding more agentic complexity when it measurably improves results.[^2_1]

## Control surface analogy

A control surface is the thing a pilot moves, like an aileron or rudder, and the actuator is the mechanism that actually carries out that movement in the aircraft system. The point of the analogy is that if the command layer is noisy, ambiguous, or constantly changing, the mechanism downstream may look unstable even when it is responding correctly to the inputs it received.[^2_1]

For coding agents, *you* and your planning artifacts are the control surface, while the implementation agent is the actuator. If the spec keeps shifting, contains hidden assumptions, or mixes goals, constraints, and open questions together, the implementation phase absorbs that ambiguity as rework, drift, or over-interpretation.[^2_2][^2_1]

## What’s happening now

What you’ve built is already strong for the execution half: scoped tasks, isolated sessions, explicit rules, and verification-friendly implementation all line up with patterns that work well for coding agents, especially where code can be checked through tests and environmental feedback. The pain you’re feeling is that your planning phase has become a human-agent co-design loop with very high fidelity, and that loop is expensive because you are trying to collapse uncertainty *before* implementation starts.[^2_1]

That is often rational, but it can become locally optimal and globally slow. In other words, you are buying implementation correctness with planning labor, and the question is whether you can replace some of that up-front precision with better decomposition, better defaults, and tighter feedback loops later in the process.[^2_3][^2_1]

## Don’t specify everything

You usually do **not** need to fully specify everything in advance. Anthropic’s guidance is that workflows are best when tasks can be cleanly decomposed into fixed subtasks, while agents are better for open-ended problems where the exact path is not predictable, which implies that the right move is often to separate what must be fixed up front from what can be discovered safely during execution.[^2_1]

A good planning artifact for coding work should therefore distinguish three classes:

- Fixed requirements: must be true before coding starts.
- Defaultable choices: can be filled in from conventions, templates, or architecture rules.
- Discoverable details: can be resolved during implementation by an evaluator, tests, or a checkpoint loop.

If you force all three classes into the spec, planning becomes exhaustive. If you separate them, planning becomes selective.[^2_2][^2_1]

## Move from exhaustive specs to constraint specs

A faster first half usually comes from writing **constraint specs**, not exhaustive specs. Instead of describing every desired implementation detail, define invariants, interfaces, non-goals, acceptance tests, and architectural boundaries, then let the agent work within that box.[^2_4][^2_1]

For example, instead of specifying every internal design choice, define things like:

- Public API shape.
- Files or modules allowed to change.
- Performance or latency budget.
- Error-handling expectations.
- Required tests and examples.
- Patterns to prefer and patterns to avoid.

That gives the implementation agent a stable search space without requiring you to pre-negotiate every micro-decision.[^2_1]

## Add a planning funnel

You can speed the front half by breaking planning into passes, where each pass answers a different kind of question. Prompt chaining is specifically recommended for tasks that can be decomposed into fixed subtasks, because each step becomes easier and more reliable than one giant all-in planning conversation.[^2_1]

A useful funnel for coding work is:


| Pass | Goal | Output |
| :-- | :-- | :-- |
| Intent pass | Clarify user value and non-goals [^2_1] | 5-10 bullet problem statement |
| Constraint pass | Identify hard boundaries and interfaces [^2_1][^2_2] | Invariants and change budget |
| Risk pass | Surface ambiguity, edge cases, dependencies [^2_5][^2_2] | Open questions list |
| Task pass | Break into implementable units [^2_1] | Ordered task graph |
| Readiness pass | Check if each task is “good enough to start” [^2_5] | Go / revise decision |

This reduces the feeling that you need one marathon planning session. You’re converting planning from freeform refinement into a staged production line.[^2_1]

## Use agents to compress planning

Yes — agents can help make planning faster, but usually by acting as **specialized reviewers**, not by replacing your judgment. Anthropic highlights routing, parallelization, orchestrator-worker, and evaluator-optimizer patterns, and the most relevant ones here are parallel review and evaluator-optimizer loops.[^2_1]

A practical setup for your front half could be:

- Planner agent drafts a minimal spec.
- Critic agent checks for ambiguity, hidden assumptions, and missing acceptance criteria.
- Architecture agent checks consistency with your codebase patterns and allowed abstractions.
- Test agent derives candidate acceptance tests from the spec.
- Synthesizer agent merges those into a revised task package.

That works because each reviewer looks at one dimension instead of one giant omniscient planning prompt, which is exactly the kind of separation of concerns that tends to improve consistency.[^2_1]

## Create stronger defaults

The biggest speedup often comes from reducing the number of decisions that need discussion at all. Anthropic notes that success often depends heavily on well-designed tool interfaces and documentation, and even says they spent more time optimizing tools than the overall prompt for their coding agent in some cases.[^2_1]

In your context, “tools” can include your planning templates and repo conventions. If you define default answers for recurring questions, the planning phase becomes exception handling instead of bespoke design every time:

- Default module layout.
- Default error semantics.
- Default logging/tracing pattern.
- Default test scaffolding.
- Default dependency policy.
- Default naming rules.
- Default rollback strategy.

Then the planner only needs to specify where the task *deviates* from defaults.[^2_6][^2_1]

## Plan to the test boundary

Because coding tasks are verifiable, one of the best ways to shorten planning is to stop refining once the task has a strong test boundary. Anthropic specifically points to coding as a domain where solutions are verifiable through automated tests and agents can iterate using those test results as feedback.[^2_1]

So instead of asking, “Have we thought through every implementation detail?” ask:

- Can I write or derive acceptance tests?
- Are failure modes explicit?
- Is the allowed change surface bounded?
- Would two competent implementers produce roughly equivalent results?

If yes, the task is probably ready. You do not need total design closure; you need implementation containment.[^2_7][^2_1]

## Treat refinement as uncertainty triage

Your current refinement loop is valuable, but it should probably be used more selectively. Research on multi-agent learning and competitive dynamics shows that instability is driven less by raw agent count than by interaction structure, connectivity, and update behavior, which is a useful reminder that the issue is often how many things are coupled during planning, not just how many things exist.[^2_8][^2_9]

So instead of refining every detail, refine only the details with one of these properties:

- High blast radius if wrong.
- Hard to test later.
- Expensive to change after implementation.
- Architectural rather than local.
- User-visible and preference-sensitive.

Everything else can be left to defaults, local implementation judgment, or evaluator loops.[^2_5][^2_1]

## A more efficient workflow

Your current flow is Idea -> Plan -> Spec -> Implement -> Test. A better version may be:

Idea -> Brief -> Constraint Spec -> Test Sketch -> Implement -> Evaluate -> Patch[^2_5][^2_1]

The important differences are:

- “Brief” is fast and outcome-focused, not exhaustive.
- “Constraint Spec” captures invariants and boundaries, not every detail.
- “Test Sketch” defines what good looks like before implementation.
- “Evaluate -> Patch” absorbs some uncertainty that you currently try to eliminate too early.

This shifts effort from speculative refinement to measurable correction, which is often cheaper in coding environments with good tests.[^2_1]

## One concrete template

Here’s a compact planning package that tends to be enough for many coding tasks:

- Objective: what user-visible change should happen.
- Non-goals: what should not be touched.
- Constraints: architecture, API, performance, dependencies, files.
- Risks: 3-5 likely failure points.
- Acceptance tests: observable pass conditions.
- Defaults in force: conventions the agent should assume unless told otherwise.
- Open questions: only blocking questions, not all possible questions.

That structure pushes the agent toward consistent execution without requiring a fully negotiated novel every time.[^2_2][^2_1]

## Where to focus next

If I were optimizing your workflow, I would not try to make planning “more complete.” I would try to make planning **more typed**: separate invariants from preferences, defaults from exceptions, and blockers from nice-to-haves, because predictability improves when the agent knows which uncertainties must be resolved and which can be handled downstream.[^2_10][^2_1]

The real goal is not to remove refinement. It is to reserve high-cost refinement for the small subset of decisions that truly deserve it, and let better defaults, staged planning, and evaluator loops handle the rest.[^2_5][^2_1]

If you want, I can turn this into a concrete “fast planning pipeline” for coding agents with example prompts for each stage.
<span style="display:none">[^2_11][^2_12][^2_13][^2_14][^2_15][^2_16][^2_17]</span>

<div align="center">⁂</div>

[^2_1]: https://www.gregasquith.com/blog/why-ai-agents-are-powerful-but-not-consistent

[^2_2]: https://galileo.ai/blog/ai-agent-reliability-strategies

[^2_3]: https://resources.anthropic.com/hubfs/Building Effective AI Agents- Architecture Patterns and Implementation Frameworks.pdf

[^2_4]: https://logiciel.io/blog/multi-agent-systems-orchestration-collaboration

[^2_5]: https://www.getmaxim.ai/articles/10-essential-steps-for-evaluating-the-reliability-of-ai-agents/

[^2_6]: https://www.maryammiradi.com/blog/build-ai-agents-anthropic-lessons

[^2_7]: https://www.ibm.com/think/insights/building-evaluating-ai-agents-real-world

[^2_8]: https://arxiv.org/abs/2201.10483

[^2_9]: https://www.thenoah.ai/resources/blogs/why-semantic-consistency-is-key-to-scaling-ai-decisions

[^2_10]: https://galileo.ai/blog/stability-strategies-dynamic-multi-agents

[^2_11]: https://www.reddit.com/r/LocalLLaMA/comments/1p7siuu/anthropic_just_showed_how_to_make_ai_agents_work/

[^2_12]: https://blog.devgenius.io/why-anthropics-building-effective-agents-raises-the-bar-and-which-agent-patterns-to-avoid-e60a143940df

[^2_13]: https://arxiv.org/html/2508.02694v1

[^2_14]: https://www.linkedin.com/pulse/task-decomposition-autonomous-ai-agents-principles-andre-9nmee

[^2_15]: https://www.decodingai.com/p/ai-agents-planning

[^2_16]: https://www.youtube.com/watch?v=azuwSlLX3xY

[^2_17]: https://www.reddit.com/r/AI_Agents/comments/1pb861h/how_do_you_approach_reliability_and_debugging/

