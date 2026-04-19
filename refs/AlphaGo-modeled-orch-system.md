<img src="https://r2cdn.perplexity.ai/pplx-full-logo-primary-dark%402x.png" style="height:64px;margin-right:32px"/>

# > [AlphaGo](https://en.wikipedia.org/wiki/AlphaGo) was trained in this way: using Monte-Carlo Tree Search and self-play as the amplification step, and Reinforcement Learning as the distillation step. This led to superhuman performance in Go.

tell me more about the tree search, self play and RL in that process. How was it used? How could you implement something similar with a coding agent/orchestrator, LLMs, and a problem today?

AlphaGo used tree search as a **planner at decision time**, self-play as a **data generator and policy improver**, and reinforcement learning as the **mechanism that compressed what search discovered back into neural networks**. In modern terms, search was the “slow thinking” layer, while the networks were the “fast intuition” layer that kept getting better from the search traces they helped produce.[^1_1]

## AlphaGo pieces

In the 2016 AlphaGo system, DeepMind used a **policy network** to suggest promising moves and a **value network** to estimate who was likely to win from a position; these were then combined with Monte Carlo Tree Search, along with rollout simulations, to choose moves during play. The policy network was first trained on expert human moves and then refined with reinforcement learning from self-play, while the value network was trained on positions sampled from self-play games and labeled with the eventual winner.[^1_2][^1_1]

## Tree search

Monte Carlo Tree Search, or MCTS, was the part that looked ahead from the current board state by expanding a partial game tree rather than brute-forcing the full game space, which is intractably large in Go. In AlphaGo, the policy network narrowed the branching factor by prioritizing plausible moves, and the value network helped evaluate leaf positions so the search did not have to rely only on random playouts.[^1_1]

A useful way to think about it is: the tree search asked, “if I seriously consider these few candidate moves and explore their consequences, which root move keeps looking best after repeated simulations?”. In AlphaGo Zero, DeepMind explicitly describes MCTS as a **policy improvement operator** because the search probabilities over moves were usually stronger than the raw network probabilities before search.[^1_1]

## Self-play

Self-play was how the system created an effectively unlimited stream of training data without waiting for humans to label positions or play games. The system played games against itself using its current policy, so the training distribution kept shifting upward as the agent improved, which is why self-play can keep raising the bar instead of merely imitating past human behavior.[^1_2][^1_1]

This matters because imitation alone tends to cap performance near the quality of the demonstrations, whereas self-play lets the agent discover strategies humans missed or underused. DeepMind notes that AlphaGo Zero, trained from self-play without human examples, rapidly rediscovered standard Go knowledge and also found new joseki variants beyond traditional human play.[^1_1]

## Reinforcement learning

In AlphaGo Fan, the policy network was initially trained to predict expert moves and then refined by policy-gradient reinforcement learning using self-play games, while the value network learned to predict the winner from self-play positions. So RL was not just “reward at the end”; it was the process that turned win/loss outcomes from self-play into parameter updates that made strong actions more likely and improved evaluation of future positions.[^1_3][^1_2][^1_1]

In AlphaGo Zero, the loop became cleaner: at each position, MCTS produced an improved move distribution $\pi$, the finished self-play game produced an outcome $z$, and the network was trained so its policy matched $\pi$ and its value matched $z$. DeepMind describes this as approximate policy iteration: search improves the policy, self-play evaluates it, and training distills both back into the network for the next iteration.[^1_1]

## Amplification vs distillation

Your framing is a good one: **amplification** is the expensive procedure that makes a stronger decision than the base model alone, and **distillation** is the cheaper learner that absorbs those better decisions into its parameters. In AlphaGo Zero, MCTS is explicitly the stronger policy improvement step, because it turns a raw policy/value network into a better action distribution at the root, and then training forces the network to imitate that stronger distribution and the eventual game outcome.[^1_1]

So the loop is roughly:

- Base model proposes moves and values[^1_1]
- Tree search amplifies that into a stronger local decision policy $\pi$[^1_1]
- Self-play generates trajectories using that stronger policy[^1_1]
- RL/distillation updates the model so next time it starts closer to what search would have found[^1_1]

That is why the search can eventually need fewer simulations for the same strength: the “fast policy” has internalized more of the “slow search”.[^1_1]

## How this maps to coding agents

For a coding agent, tree search would not literally be a Go tree over legal moves; it would be a tree over **problem-solving actions** such as “inspect file,” “write test,” “propose patch,” “run linter,” “execute unit tests,” “revert,” or “try alternative architecture”. The role of MCTS would be to allocate limited compute to the most promising partial solution branches, using model predictions to rank branches and using actual tool feedback as the environment signal.[^1_4][^1_5]

Self-play becomes **self-generated tasks or adversarial task generation** rather than agent-vs-agent board play. A coding system can create or sample tasks, attempt solutions, run the code, collect outcomes such as tests passed, runtime, correctness, style, and regression counts, and then train on those traces so future attempts need less search.[^1_1]

Reinforcement learning then distills branch outcomes back into two things very analogous to AlphaGo’s networks: a **policy** over next actions and a **value** estimate over partial states like “given this repo state, failing tests, and current plan, how likely is this branch to finish correctly within budget?”. That is the key abstraction: policy says what to try next; value says whether this branch is worth more compute.[^1_4][^1_1]

## A concrete orchestrator design

Here is the closest practical analogue for a coding agent today:


| AlphaGo concept | Coding-agent analogue |
| :-- | :-- |
| Board state | Workspace state: files, diffs, logs, tests, issue text |
| Move | Tool/action choice: edit, search, run tests, inspect stack trace, ask subagent |
| Policy network | LLM that proposes next actions or patches |
| Value network | LLM or learned scorer predicting probability of successful completion |
| MCTS | Budgeted branching over candidate plans/patches with test feedback |
| Self-play | Auto-generated tasks, regression suites, red-team tasks, agent-vs-agent review |
| RL/distillation | Fine-tuning, preference optimization, or offline RL on successful traces |

A practical search node would contain:

- Current repo snapshot or diff state
- Current plan and subgoal
- Observations from tools, compiler, tests, logs
- Cost spent so far, tokens, wall-clock, branch depth
- Estimated value: chance of success under remaining budget

Edges would be actions like:

- Generate patch for function X
- Add failing test first
- Refactor module boundary
- Query code search for call sites
- Spawn reviewer agent
- Run unit tests or benchmark subset
- Backtrack to previous snapshot


## How tree search would work

At the root, the orchestrator asks an LLM policy for $k$ plausible next actions or patches, each with a prior probability, similar to AlphaGo’s move priors. It then selectively expands branches, runs real tools on them, and backs up observed rewards such as “tests improved,” “new failures introduced,” “benchmark regressed,” or “final task solved” to estimate branch quality, just as MCTS updates visit counts and action values in the search tree.[^1_1]

The search policy does not need to be pure MCTS to be useful; a modern variant could combine:

- Prior from an LLM policy
- Value from a learned critic or verifier LLM
- Rollout/evaluation from cheap executors, tests, linters, static analyzers
- UCB-style exploration bonus for underexplored branches

That is very close in spirit to AlphaGo: learned priors narrow the tree, learned values evaluate partial states, and search spends compute where uncertainty and promise are both high.[^1_1]

## How self-play would work

Coding lacks a neat symmetric two-player game, so “self-play” needs reinterpretation. The best analogues are:[^1_1]

- **Task self-generation:** Generate bugs, TODOs, refactor prompts, migration tasks, or failing tests in known codebases, then solve them and verify automatically.
- **Agent-vs-agent:** One agent writes buggy code or hidden tests, another tries to fix or satisfy them.
- **Curriculum self-play:** Start from easy synthetic bugs and progressively generate harder ones based on what the current agent can almost solve.
- **Debate/review self-play:** One branch proposes a patch, another critiques it, and a third adjudicates by running tests and static checks.

The important part is not literal competition; it is the **closed-loop generation of training situations from the system’s own current ability frontier**. That is what made AlphaGo self-play powerful, and that is what you want in a coding setting too.[^1_1]

## How RL/distillation would work today

A realistic 2026 implementation would probably use a hybrid rather than “pure online RL”. You can collect search traces and distill them with:[^1_1]

- Supervised fine-tuning on best branch trajectories
- Preference optimization from branch comparisons
- Outcome-conditioned training using verifier scores
- Offline RL or bandit-style policy updates on action sequences with rewards

For example, if search explored 20 branches and branch 7 solved the task with minimal edits and no regressions, you train the policy to increase the probability of the sequence of choices that led there, and train the value head to predict high success from the intermediate states on that path. This is the coding-agent analogue of using search probabilities $\pi$ and game outcome $z$ as training targets.[^1_3][^1_1]

## A minimal implementation sketch

A first version could be much simpler than AlphaGo and still useful:[^1_5][^1_4]

1. **State encoder:** Represent task + repo status + recent tool outputs + diff summary.
2. **Policy model:** Ask an LLM for top next actions and candidate patches with scores.
3. **Value model:** Use either a second LLM prompt or a small learned model to estimate “probability this branch succeeds within budget.”
4. **Search loop:** Expand top branches, run tests/lint/typecheck, keep snapshots, backpropagate branch scores.
5. **Replay buffer:** Store $(state, improved\_action\_distribution, final\_outcome)$ from searches.
6. **Distillation:** Periodically train or fine-tune the policy/value models on this buffer.
7. **Curriculum:** Continuously generate fresh tasks from real repos, synthetic bug injectors, and hidden test suites.

If you want the closest AlphaGo Zero-style target, save for each decision point:

- The state $s$
- The search visit distribution over actions $\pi$
- The final verified outcome $z$

Then train:

- Policy head to match $\pi$
- Value head to predict $z$

That is the cleanest “search as amplification, model training as distillation” recipe.[^1_1]

## Where this breaks

The biggest difference from Go is that coding environments are **messy, partial, and non-stationary** rather than fully specified by simple rules. In Go, the simulator is exact and cheap; in coding, “environment dynamics” include flaky tests, unclear specs, hidden dependencies, human taste, long execution times, and tasks where correctness is only partly machine-checkable.[^1_5][^1_1]

That means your coding version needs stronger verifiers and more structured rewards than a raw LLM loop. In practice, success depends on having:

- High-quality executable checks
- Cheap intermediate rewards, not just final pass/fail
- Snapshotting and rollback
- Strong branch deduplication
- Good task generation so the system does not overfit toy bugs


## Best problem types today

This works best right now for problems with **clear verification**:

- Bug fixing with hidden tests
- Refactoring with behavior-preservation tests
- Performance optimization with benchmarks
- Type error repair
- Dependency or API migrations
- SQL/query optimization with test fixtures

These look more like Go because they have explicit rules, fast feedback, and measurable wins. Open-ended product design or vague feature work can still use tree search, but the value function becomes noisier because the reward is less objective.[^1_5][^1_1]

## One example

Suppose the task is: “Fix failing integration tests after upgrading FastAPI and SQLAlchemy.”

A search-based coding agent could:

- Propose 6 first-step actions, such as inspecting stack traces, checking migration notes, diffing dependency APIs, or writing a compatibility shim.
- Expand the two best branches first.
- For each branch, generate a small patch, run targeted tests, and update branch value.
- If one branch fixes 8 of 10 failures but introduces warnings, and another fixes 6 cleanly, the search can keep exploring around both.
- After the run, the orchestrator stores which branch sequence ended best and distills that trajectory into future policy/value updates.

That is AlphaGo’s pattern in a software workflow: search spends compute on branch exploration, verification supplies the environment signal, and learning compresses the resulting strategy into the base agent.[^1_1]

## Practical takeaway

If you want to build this, don’t start with “full RL for coding.” Start with **search + verifier + trace collection**, because that already gives you the amplification layer and the data exhaust needed for distillation. Then add a learned value model, then train the policy on search-improved traces, and only after that consider heavier RL methods.[^1_1]

If you want, I can turn this into a concrete architecture doc for a coding orchestrator — with node schema, search algorithm, reward design, replay buffer format, and pseudocode for a Python/FastAPI implementation.
<span style="display:none">[^1_10][^1_11][^1_12][^1_13][^1_14][^1_15][^1_6][^1_7][^1_8][^1_9]</span>

<div align="center">⁂</div>

[^1_1]: https://www.nature.com/articles/nature16961

[^1_2]: https://julien-vitay.net/deeprl/src/4.5-AlphaGo.html

[^1_3]: https://airev.us/alpha-go

[^1_4]: https://arxiv.org/html/2411.04580v2

[^1_5]: https://www.youtube.com/watch?v=We20YSAJZSE

[^1_6]: https://discovery.ucl.ac.uk/10045895/1/agz_unformatted_nature.pdf

[^1_7]: https://pubmed.ncbi.nlm.nih.gov/26819042/

[^1_8]: https://pubsonline.informs.org/do/10.1287/orms.2016.05.10/full/

[^1_9]: https://realkm.com/2016/12/28/mastering-the-game-of-go-with-deep-neural-networks-and-tree-search/

[^1_10]: https://arxiv.org/html/2601.03306v1

[^1_11]: https://research.google/blog/alphago-mastering-the-ancient-game-of-go-with-machine-learning/

[^1_12]: https://www.reddit.com/r/reinforcementlearning/comments/15dn9wz/how_is_the_policy_network_updated_in_alphago/

[^1_13]: https://en.wikipedia.org/wiki/MuZero

[^1_14]: https://saas.studentorg.berkeley.edu/rp/an-introduction-to-go-alphago-and-quantifying-go-gameplay

[^1_15]: https://deepmind.google/research/alphago/


---

# I'm looking  to experimentally build semi-autonoumous planners that can figure out how to build out a software project, for example. So it could build out specs that grew larger, more detailed and precice.

The policy work sounds interesting because its like the OpenAI orchestration work - when you need the software developers to set the best practices of how to make a project well architected

build a planning mechanism that could extend a system over time - might use agents that collaborate, criteque, mediate, etc - maybe even decide what to build. (See Build in an Infinite loop - ask about this)

Design notes - use Z.AI GLM 5.1 for some of this (less expensive tokens but good results). could look at different models - reasoning for planning, then others for coding and building.

There needs to be this policy iterative building and improvign through metadata/reading past logs and improving.

Need some type of project that isn't totally useless that an agent can start building out and can keep improving on. Maybe its an orchestrator system - which starts as a very small main loop with some very simple components. It can then start iterating to improve itself.
If there was a scheduler or something that made sure to keep the flow going.
We took out Claude's inherent stalling - so had to use '-p'
Need to figure out a series of loops an agent could go through to design inself and continually iterate.
Maybe could run a couple processes at once that were responsible for different askpects.
Started off with a known working agentic loop. That would be used to build and run a standalone agentic loop. Which was used at the center of an orchestrator.
Once a loop was completed, the main looping mechanism could choose between several next choices and it had access to its previous choices. The choices and outcomes were documented in a log. An agent would figure out what next decisions should be chosen next.

Maybe it could be treated as a series of agents. Like a series of events would be fired, and agents would react - kick off next steps.
Ah- a policy of an agent would be to ALWAYS kick off a next agent after it was complete. Either its a review agent (B) - and A was the code writer. So it could define a policy that if the code writing was complete, that it next needed to be reviewed.

The policy was a state machine that defined what the agent or system could do next. Hooks could be used to force the agent to make its next choice. The hook could give the agent a reminder of what next steps were possible.

Since were using ntm to open close windows, that could be baked into the hooks calls. the underlying hooks would track what agents were which when starting up - what window they're running in. The state machine would be responsible for tracking. The agent only needed the couple actions it needed.
It could start a new agent, pass on a task, send a message and wait for it to come back (code review)(policy - for work to move through, one agent needs to be responsible for the work, another agent to review and sign off, review agent can complete work - other agent signs off it was completed.)

Agetns could have roles, roles determine actions that can be taken. Reviewers can send messages. Planning agents can't commit code (some how).

Yes — what you’re describing is a very plausible next step: a **policy-driven software planner/orchestrator** that starts with a small, reliable loop and then improves its own planning, role structure, and execution policy over time through logs, reviews, and verifier feedback. The safest way to build it is not as “fully autonomous coding from day one,” but as a **state-machine-based orchestrator with explicit agent roles, forced handoffs, logged outcomes, and iterative policy refinement**, which lines up both with current agent-orchestration guidance and your own need for hooks, recoverability, and non-stalling execution.[^2_1][^2_2]

## Core idea

Your strongest idea here is that the “policy” is not just a prompt; it is the **allowed transition system** for work moving through the project, such as plan $\rightarrow$ spec $\rightarrow$ code $\rightarrow$ review $\rightarrow$ test $\rightarrow$ merge $\rightarrow$ next decision. That means the planner should not merely ask an LLM “what next,” but should expose a constrained menu of next actions based on current state, role, and project history, with hooks that remind the agent what transitions are legal and expected.[^2_1][^2_2]

This is the closest analogue to the AlphaGo framing from earlier: the policy is the fast prior over next actions, while the orchestrator loop, reviews, and verifiers provide the slower corrective structure that improves decisions over time. In your case, “good architecture” is partly encoded by software developers up front as role rules, state transitions, code-review requirements, and evaluator checks rather than left entirely to free-form generation.[^2_2]

## Best first project

The best non-useless seed project is probably exactly what you suggested: **a self-improving orchestrator that begins as a tiny agent loop and then incrementally builds its own surrounding system**. It is useful on day one because even a minimal version can run planning/review/code/test workflows for other repos, and it is improvable because you can log every decision, every handoff, every failure mode, and every successful branch.

A good initial scope would be:

- A single project loop with explicit states.
- A planner agent that can expand a spec into more detailed specs.
- A builder agent that can create code or artifacts.
- A reviewer agent that must sign off before promotion.
- A scheduler that keeps the loop moving and prevents stalling.
- A policy log that records state, action, rationale, result, and next options.

That gives you something useful immediately while also generating the exact trace data you need for future policy refinement.[^2_1][^2_2]

## Architecture shape

I would structure this as an **event-driven orchestrator around a durable state machine**, not as a loose swarm of agents. Agents should feel interchangeable and narrow; the orchestrator owns workflow truth, state transitions, logs, and recovery, while agents react to events and produce proposals or approvals.[^2_3][^2_2]

A simple architecture could be:


| Component | Responsibility |
| :-- | :-- |
| Orchestrator core | Owns project state, transitions, scheduling, retries, budgets |
| Policy engine | Decides which transitions are legal and preferred next |
| Event bus | Emits events like `plan.completed`, `review.failed`, `task.ready` |
| Agent runner | Starts agents in tmux/NTM windows, tracks process/session metadata |
| Workspace manager | Handles repo snapshots, diffs, branches, artifacts |
| Verifier layer | Runs tests, lint, typecheck, architecture checks, policy checks |
| Memory/log layer | Stores decisions, outcomes, critiques, branch history |
| Improvement loop | Reads logs and proposes policy/spec/workflow upgrades |

This fits your idea that hooks should track window/process identity and that the state machine, not the agent, should carry most of the orchestration burden.

## State machine first

Your “policy as state machine” idea is the right backbone because it prevents the system from becoming vague, recursive, and expensive. Instead of allowing arbitrary agent behavior, each role gets a limited action surface, and every completed action must produce either a state transition or a blocked state with explicit reason codes.[^2_4][^2_1]

For example:


| State | Owner role | Allowed next actions |
| :-- | :-- | :-- |
| `idea_backlog` | Planner | expand spec, discard, prioritize, split epic |
| `spec_drafting` | Planner | revise spec, request research, handoff for critique |
| `spec_review` | Reviewer | approve, reject with critique, request clarification |
| `implementation_ready` | Scheduler | assign builder, defer, batch |
| `coding` | Builder | edit code, run tests, request clarification, submit for review |
| `code_review` | Reviewer | approve, reject, patch directly if policy allows |
| `verification` | Verifier | run test suite, benchmark, static checks |
| `done` | System | archive result, generate retrospective, enqueue next goal |

That gives you a durable backbone for “always kick off the next agent when complete” because the handoff is a transition rule, not an emergent behavior.[^2_2][^2_1]

## Roles and permissions

You also have a strong instinct that **roles determine actions**, which is critical for safety and legibility. A planning agent should not be able to merge code, a reviewer should not silently rewrite architecture unless explicitly allowed, and a builder should not bypass review just because it believes the task is done.

A clean role model might be:

- **Planner:** can create specs, split work, revise plans, propose priorities.
- **Researcher:** can inspect docs, summarize dependencies, surface constraints.
- **Builder:** can edit code, run scoped checks, propose implementation notes.
- **Reviewer:** can critique, approve, reject, or request changes.
- **Verifier:** can run deterministic evaluation tools only.
- **Scheduler:** can assign next work item and enforce budgets/timeouts.
- **Governor:** can change policy only through a separate reviewed path.

This directly supports your idea that one agent must own the work while another signs off, and that some agents should be intentionally unable to commit or advance state on their own.

## Hooks and forced progress

The hook system should act like **guardrails plus interrupts**, not just lifecycle notifications. If you removed Claude’s natural stalling behavior and had to force progress with `-p`, then your runtime needs hard-coded post-step hooks that ask: what state changed, what outcomes were observed, what next states are legal, and who is responsible now?

Useful hooks would be:

- `on_agent_started` — register tmux/NTM window, role, task id, timeout.
- `on_agent_output` — parse structured result, extract proposed next action.
- `on_agent_completed` — require transition decision, never “just stop.”
- `on_timeout` — kill, snapshot, requeue, or escalate.
- `on_review_required` — force creation of a review task.
- `on_verification_failed` — reopen prior state with failure metadata.
- `on_idle_project` — scheduler must select next eligible work item.
- `on_policy_violation` — block transition and open governance issue.

That hook model turns your “always kick off a next agent after completion” idea into a deterministic system rule rather than a best-effort suggestion.[^2_1][^2_2]

## Collaboration pattern

Multi-agent collaboration works best here if it is **structured and asymmetric**, not a free chat room. A strong baseline is:[^2_5][^2_2]

- Planner drafts or expands the spec.
- Critic reviews the plan for ambiguity, overreach, and missing acceptance criteria.
- Mediator resolves planner/critic disagreement into a final approved task packet.
- Builder implements against the packet.
- Reviewer checks implementation against spec and architecture rules.
- Verifier runs objective checks.
- Retrospective agent updates metadata and lessons learned.

This “collaborate, critique, mediate” pattern is a good fit for planning-heavy work because it separates idea generation from constraint enforcement.[^2_2][^2_5]

## How policy improves

The policy iteration loop should mostly come from **metadata, outcomes, and retrospective analysis of logs**, exactly as you described. Every task should produce not just an artifact, but a structured trace containing:

- State entered
- Agent selected
- Prompt or policy version used
- Allowed next actions shown
- Action chosen
- Verifier outcomes
- Human overrides, if any
- Final disposition
- Retrospective notes on why this branch succeeded or failed

Over time, that lets you improve at least four things:

- Transition rules
- Agent instructions
- Role permissions
- Task-selection heuristics

This is the practical analogue of distillation: the orchestrator becomes better at choosing its next loop, because past decisions and outcomes are made machine-readable and queryable.

## Model strategy

Using different models by role is exactly the right move if cost matters and tasks are heterogeneous. OpenAI’s practical guidance emphasizes incremental orchestration and model selection via evals, and Z.AI’s current pricing page shows GLM-5.1 priced below many top-end frontier options while being positioned by Z.AI as a model for long-horizon agentic engineering.[^2_6][^2_7][^2_2]

A practical split could be:

- **Planning/spec expansion:** stronger reasoning model.
- **Coding/building:** cheaper but capable coding model.
- **Review/critique:** mid-to-strong model with good judgment.
- **Verifier narration/summarization:** cheaper model.
- **Scheduler/policy routing:** very cheap model or rule engine first.

Given Z.AI’s published April 2026 pricing, GLM-5.1 currently lists input at \$1.4 and output at \$4.4, versus GLM-5 at \$1.0 and \$3.2, so it is not the cheapest option in absolute terms, but it may still be attractive if its long-horizon planning quality reduces retries and branch waste. The right answer here is to benchmark by workflow stage, not to pick one universal model.[^2_8][^2_6][^2_2]

## Scheduler design

Yes, you want a scheduler, because otherwise the system will drift, stall, or keep reopening locally appealing but globally low-value tasks. The scheduler should own:[^2_4][^2_1]

- Task queue ordering
- Concurrency limits
- Retry budgets
- Cooldowns for repeatedly failing items
- Project-level “what next” selection
- Idle detection and wake-up rules

A good scheduler policy is:

- Always maintain at least one “next actionable task.”
- Never allow work to remain completed-but-unreviewed.
- Prefer small, mergeable increments over giant speculative branches.
- Escalate blocked work after $n$ retries or $t$ hours.
- Force retrospectives on repeated failure patterns.

This gives the project a heartbeat and prevents the infinite-loop anti-pattern where the same reasoning step repeats without increasing verified progress.[^2_1][^2_4]

## Event-driven loop

Your “series of events fired, and agents react” model is a very good implementation pattern. I’d make the event types first-class, for example:[^2_3]

- `project.created`
- `goal.selected`
- `spec.expanded`
- `spec.approved`
- `task.assigned`
- `code.submitted`
- `review.requested`
- `review.approved`
- `verification.failed`
- `retrospective.created`
- `policy.change.proposed`
- `policy.change.approved`

Then each agent subscribes only to relevant event types, while the orchestrator enforces whether a resulting action is allowed in the current state. That keeps the system composable without making it anarchic.[^2_3][^2_2]

## Self-improvement boundary

The system should be able to improve **its prompts, decomposition strategies, project templates, and scheduling heuristics** much earlier than it is allowed to rewrite its own core governance logic. In other words, let it self-improve around the edges first, but require stricter review for:

- Role permission changes
- State machine changes
- Merge policy changes
- Tool access changes
- Network/data access changes

That matches your instinct that architecture and best practices still need developer-defined policy, especially early on.

## Practical MVP

If I were scoping your first experimental version, I would build this:

1. **One repo, one orchestrator, one durable SQLite/Postgres state store**
2. **Explicit state machine with 8-12 states**[^2_1]
3. **Three roles only at first:** planner, builder, reviewer[^2_2]
4. **One scheduler process** that prevents idle states and stalled windows[^2_4]
5. **tmux/NTM runtime adapter** that maps agent ids to panes/windows and records lifecycle metadata
6. **Structured logs** for every transition and tool result
7. **Verifier bundle** with tests, lint, typecheck, and spec conformance checks[^2_2]
8. **Retrospective loop** that summarizes failures and suggests prompt/policy tweaks, but does not auto-apply governance changes

That is enough to create a real, useful agentic system that can build out a small orchestrator project and keep improving it in bounded ways.

## Infinite loop angle

The “build in an infinite loop” version should be treated as a **research mode over a bounded production core**, not as your default operating mode. Public discussion around agent execution loops keeps emphasizing termination conditions, stop rules, and hard budgets because unconstrained recursive loops tend to produce cost blowups, duplicated effort, or self-referential busywork rather than genuine progress.[^2_9][^2_4][^2_1]

So if you want an infinite-improvement mode, I’d constrain it with:

- Hard wall-clock budgets
- Max transitions per cycle
- Required verifier delta before continuing
- Duplicate-branch detection
- Human approval gates for policy/governance changes
- “No verified progress” halting condition

That way, the loop is infinite in principle but episodic and measurable in practice.[^2_4][^2_1]

## My recommendation

I would define your system as a **policy-governed event/state orchestrator for software project growth**, not as a general autonomous coder. Make the first project the orchestrator itself, start with planner-builder-reviewer plus scheduler, log every transition, and use retrospectives to iteratively improve prompts, decomposition, and task routing before allowing deeper self-modification.[^2_2]

The most important design decision is this: **the agent does not decide what is possible; the policy/state machine does**. Once that boundary is clear, multi-agent collaboration, cheaper models like GLM-5.1 for some stages, and self-improving project growth all become much more tractable.[^2_7][^2_6][^2_1]

If you want, I can take this and turn it into a concrete spec for an MVP — state machine, event schema, agent roles, DB tables, hook API, and a main loop in Python/FastAPI.
<span style="display:none">[^2_10][^2_11][^2_12][^2_13][^2_14][^2_15]</span>

<div align="center">⁂</div>

[^2_1]: https://newsletter.victordibia.com/p/the-agent-execution-loop-how-to-build

[^2_2]: https://cdn.openai.com/business-guides-and-resources/a-practical-guide-to-building-agents.pdf

[^2_3]: https://www.techaheadcorp.com/blog/understanding-the-agent-loop/

[^2_4]: https://pub.towardsai.net/the-12-000-infinite-loop-how-my-ai-agent-bankrupted-a-sandbox-2fc6585b6716

[^2_5]: https://lanex.au/blog/building-agents-deep-dive-into-openai-best-practices

[^2_6]: https://docs.z.ai/guides/overview/pricing

[^2_7]: https://z.ai/blog/glm-5.1

[^2_8]: https://www.linkedin.com/posts/lewiswalkerai_must-read-on-ai-agents-activity-7318664705051381761-IJEj

[^2_9]: https://github.com/disler/infinite-agentic-loop

[^2_10]: https://www.reddit.com/r/AI_Agents/comments/1r7ooqj/i_built_an_agent_simulator_for_the_infinite_loop/

[^2_11]: https://www.youtube.com/watch?v=zksDUdutHgA

[^2_12]: https://dev.to/adgapar/a-loop-is-all-you-need-building-conversation-ai-agents-1039

[^2_13]: https://www.youtube.com/watch?v=7GOxUgVTz3s

[^2_14]: https://www.forbes.com/councils/forbestechcouncil/2025/12/16/agent-orchestration-best-practices-and-pitfalls/

[^2_15]: https://www.reddit.com/r/SillyTavernAI/comments/1sg25n3/glm51_api_pricing_is_25x_glm5_but_the_inference/


---

# [https://openai.com/index/harness-engineering/](https://openai.com/index/harness-engineering/)

What if the Policy Layer was partially a deterministic state machine? There were rules and paths - similar to Kilroy and the StrongDM Attractor pattern. But this would function on the 'team' level where the whole teams workflow was controlled by those policies.
[https://github.com/openai/symphony/blob/main/SPEC.md](https://github.com/openai/symphony/blob/main/SPEC.md)

Structure can lead to improved creativity - not less. There are certain aspects to the process that need more rigourous controls and process in place. Others that don't require as much.

> That gives you a durable backbone for “always kick off the next agent when complete” because the handoff is a transition rule, not an emergent behavior
"emergent behavior" - Thats an interesting phrase. It seems like too many places in these agents  we accept arbitrary outcomes, and that leads to chaotic, unpredictable, incomplete outcomes.
Can emergent behavior come from mostly deterministic systems? Seems like systems which combine mechanistic systems AND probabalistic outcomes may be preferable.
In cellular transport, if anything could happen - then nothing would happen. Its about controlling many variables/processes, and then relaxing the constraints in certain places.

So if we could define the rules and paths of the state machine (Kilroy has possibly a useful flow mechanism) - and each state had rules, process, etc. 1)  we can allow the agents choose particular paths based on the rules (choose to make another spec vs start on work)
2)  There could be VERY particular places where the policies 'opened up' and allowed more non-deterministic changes. Essentailly the parameters could be altered. Every time the parameters were altered, theyd be tracked and future logs track the success/failure rates.

Would need to figure out how and where the 'meta' process of self improvement came in. Also might want / need to support or promote back tracking in the case where there were issues.

Might even be able to look at 'hyper parameter' like options that could be tweaked and tuned over many iterations.

How could composible form be created, stitched together, logging at those places (to track failure rates). A reusable framework could helpprovide building blocks that could be composed together and abstractions created that would hide complexity.

Should look at what  structures generate and support positive, reinforcing emergent behavior.

Planning - how to structure, constrain and expand to explore idea pathways,

Yes — a **partially deterministic policy layer** is probably the right design, not a compromise. OpenAI’s harness-engineering writeup explicitly argues that agents work best when humans encode environments, scaffolding, invariants, and feedback loops, while agents operate with freedom *inside* those boundaries, and Symphony’s spec makes the orchestrator the single authority that converts worker outcomes into explicit state transitions rather than letting workflow state emerge informally from agent behavior.[^3_1][^3_2]

## Determinism helps

This matches your intuition that structure can increase creativity rather than suppress it. OpenAI says they enforce architectural boundaries, dependency directions, and “taste invariants” mechanically, while allowing the agent freedom in how solutions are expressed locally, because constraints become multipliers when encoded once and applied everywhere.[^3_1]

That is a strong argument for a team-level workflow policy that is partly a deterministic state machine: rigor where correctness, coordination, and safety matter; more freedom where exploration, decomposition, and synthesis matter. Symphony similarly separates a repo-defined policy layer from the coordination layer and makes orchestration transitions explicit, which is exactly the kind of backbone you’re pointing at.[^3_2][^3_1]

## Emergent behavior

Your use of “emergent behavior” is sharp here. In agent systems, a lot of bad outcomes come from allowing too many important workflow decisions to be implicit, under-specified, or purely conversational, which leads to stalled threads, duplicate work, skipped review, or incoherent next steps.[^3_2][^3_1]

But emergent behavior absolutely can arise from mostly deterministic systems. Cellular automata are the classic example: simple local rules can generate rich global behavior, so the real design question is not “deterministic or emergent,” but **which layers should be fixed and which should remain probabilistic or adaptive**. For software teams of agents, the best design is usually deterministic orchestration with probabilistic search *inside bounded spaces*.[^3_3][^3_1][^3_2]

## Good hybrid model

The hybrid you’re sketching is the one I’d recommend:


| Layer | Bias |
| :-- | :-- |
| Workflow transitions, ownership, review gates, retries | Deterministic |
| Candidate next actions within a state | Policy-guided, probabilistic |
| Evaluation, tests, structural checks | Deterministic where possible |
| Planning, ideation, spec expansion | More exploratory |
| Governance changes, permission changes | Highly constrained, review-gated |

That is very close to both Symphony and OpenAI’s harness view: the orchestrator owns runtime truth, retries, dispatch, and reconciliation, while the agent operates in a bounded environment with in-repo policy and strong feedback.[^3_1][^3_2]

## Team workflow machine

What you’re describing is larger than a single-agent loop; it is a **team workflow machine**. Symphony already gives a useful precedent: it separates policy, configuration, coordination, execution, integration, and observability, and its orchestrator alone mutates scheduling state while worker outcomes are turned into explicit transitions.[^3_2]

You could extend that upward to team-level states like:

- `goal_selected`
- `researching`
- `specifying`
- `spec_review`
- `implementation_ready`
- `building`
- `code_review`
- `verification`
- `release_ready`
- `released`
- `retro`
- `policy_tuning`

Each state would define:

- Allowed roles
- Required artifacts
- Allowed outgoing transitions
- Required checks
- Exploration budget
- Escalation conditions
- Rollback path

That gives you the Kilroy/Attractor-style flow control you want, but at the team-operating-system level rather than only the single-task level.[^3_4][^3_2]

## Controlled openings

Your idea of “opening up” only in particular places is especially strong. OpenAI explicitly says they care deeply about boundaries, correctness, and reproducibility centrally, but allow autonomy locally inside those bounds, which is essentially your “relax constraints in certain places” principle.[^3_1]

So each state can have a **freedom profile**, for example:

- `specifying`: high exploration, multiple branches, alternate decompositions allowed.
- `spec_review`: low exploration, strict checklist and contradiction checks.
- `building`: moderate exploration, but only inside assigned scope and architecture rules.
- `verification`: near-zero exploration; run checks, summarize, decide pass/fail.
- `policy_tuning`: high caution, low frequency, requires evidence and signoff.

This is how you avoid the “if anything could happen, nothing would happen” problem you mentioned. You are not maximizing freedom; you are **placing freedom where it compounds**.[^3_1]

## Policy parameters

Yes, you can think of many of these openings as hyperparameters. Symphony already has deterministic knobs like polling interval, concurrency, retry backoff, stall timeouts, and turn limits in workflow configuration, and OpenAI’s harness article emphasizes repeated refinement of documentation, tooling, and guardrails as the system evolves.[^3_2][^3_1]

You could define tunable parameters such as:

- Maximum spec depth before implementation
- Number of alternate plans explored
- Required reviewer count by task risk
- Retry budget by state
- Max concurrent builders
- Threshold for backtracking
- Acceptance of patch-vs-redesign bias
- Human-escalation thresholds
- Allowed agent autonomy by role
- Frequency of cleanup/refactor passes

Then track those parameter values against downstream outcomes like:

- Task completion rate
- Regression rate
- Review rejection rate
- Reopen rate
- Mean time to merge
- Number of retries
- Human intervention minutes
- Architectural violation counts

That gives you a real policy-optimization surface instead of vague prompt tweaking.[^3_2][^3_1]

## Logging points

The composable framework should make **transitions and openings** the primary logging boundaries. Symphony stresses structured logs and explicit orchestration states, and OpenAI’s harness writeup repeatedly highlights feedback loops, review artifacts, and codified repo-local knowledge as the basis for continual improvement.[^3_1][^3_2]

For each state transition, log:

- Prior state
- Actor role
- Candidate actions shown
- Action chosen
- Policy version
- Parameter vector
- Evidence consulted
- Outcome
- Verifier metrics
- Next state
- Confidence or rationale summary

For each “open” zone, also log:

- Exploration width
- Branches considered
- Selection reason
- Branch convergence or abandonment
- Whether backtracking occurred

This is what makes the meta-process possible later, because you’re not just logging text; you’re logging **decision structure**.[^3_2]

## Backtracking

Backtracking should be a first-class transition, not an error case. In Symphony, abnormal outcomes, retries, and reconciliation are all explicit state transitions rather than vague failures, which is a strong cue for your own design.[^3_2]

You could support:

- `verification -> building`
- `review -> specifying`
- `building -> researching`
- `policy_tuning -> previous_policy`
- `release_ready -> building`

Backtracking should also be typed:

- Local patchback, small defect
- Architectural rollback, wrong design direction
- Policy rollback, orchestration rule made outcomes worse
- Context rollback, insufficient or stale inputs

That way failure becomes navigable rather than chaotic.[^3_1][^3_2]

## Meta process

The meta self-improvement layer should probably sit **outside** the main production loop at first. OpenAI describes continuously capturing human taste, review feedback, and bugs, then promoting them into documentation, tooling, or mechanical rules when they recur; that is a cleaner model than having the live planner constantly mutate its own control logic in-band.[^3_1]

A good separation is:

- **Execution loop:** delivers project work.
- **Analysis loop:** mines logs, failure clusters, and slow points.
- **Policy proposal loop:** suggests workflow or parameter changes.
- **Governance loop:** approves or rejects those changes.

So self-improvement becomes:

1. Observe repeated failures or inefficiencies.
2. Cluster them into candidate policy gaps.
3. Propose a small rule or parameter change.
4. Run it in shadow mode or on a subset of work.
5. Compare metrics.
6. Promote, modify, or revert.

That is much safer than letting the main planner rewrite its own workflow on the fly.[^3_2][^3_1]

## Composable form

A reusable framework should treat workflow as **composable typed building blocks**, not just prompts chained together. The core abstractions I’d use are:


| Abstraction | Purpose |
| :-- | :-- |
| `State` | Declares allowed roles, inputs, outputs, checks, next states |
| `Transition` | Deterministic rule or policy-scored branch |
| `Gate` | Required validator before moving forward |
| `OpenZone` | A bounded exploratory subroutine with tunable parameters |
| `RolePolicy` | What each role may observe, change, approve, or emit |
| `Event` | Fact emitted by runtime or agent |
| `MetricHook` | Captures performance/failure metadata |
| `BacktrackRule` | Declares when and where rollback is allowed |
| `PolicyPatch` | Proposed change to workflow/parameters |
| `Experiment` | A/B or shadow-mode policy test |

That creates the stitching mechanism you asked about: complexity lives in the framework, while workflows are assembled from understandable pieces.[^3_2]

## Positive emergence

If you want “positive, reinforcing emergent behavior,” I’d focus on structures that make beneficial patterns self-reinforcing while making harmful patterns expensive or impossible. OpenAI’s harness work shows a few strong examples: short mapping docs instead of giant manuals, in-repo knowledge as system of record, mechanical invariants, recurring cleanup loops, and local agent legibility through tools and observability.[^3_1]

The main reinforcing structures are:

- Clear ownership per state
- Tight feedback loops
- Fast local verification
- Stable artifact formats
- Background cleanup/refactor agents
- Explicit quality grades
- Repo-local discoverable knowledge
- Small bounded tasks
- Automatic retries with caps
- Cheap rollback

Those create a system where good behavior compounds because it is easier to repeat, easier to inspect, and easier to encode into future policy.[^3_1]

## Planning design

For planning specifically, I’d structure it as **constrain, expand, evaluate, commit**:

1. Constrain the planning context: goal, scope, hard rules, architecture map, budget.
2. Expand into several candidate decompositions or spec branches.
3. Evaluate with critics, checklists, and dependency/feasibility tests.
4. Commit one branch into the deterministic workflow.
5. Reopen only if downstream evidence justifies it.

That gives you exploration without turning planning into an unbounded text-generation exercise.[^3_2][^3_1]

## Recommended principle

The core principle I’d use is: **deterministic skeleton, probabilistic organs**. The skeleton is the team workflow, transitions, gates, logging, rollback, and governance; the organs are planning, critique, synthesis, and local tactical choice.[^3_1][^3_2]

That is how you get emergence without chaos: not by allowing arbitrary outcomes everywhere, but by creating a system where rich behavior emerges from carefully bounded freedoms and measurable feedback.[^3_3][^3_1]

If you want, I can next turn this into a concrete design doc for a composable framework — states, transitions, open zones, parameter schema, logging schema, and an example workflow for a self-improving orchestrator project.
<span style="display:none">[^3_10][^3_11][^3_12][^3_13][^3_14][^3_15][^3_16][^3_5][^3_6][^3_7][^3_8][^3_9]</span>

<div align="center">⁂</div>

[^3_1]: https://discovery.ucl.ac.uk/10045895/1/agz_unformatted_nature.pdf

[^3_2]: https://www.nature.com/articles/nature16961

[^3_3]: https://arxiv.org/html/2604.05150

[^3_4]: https://news.ycombinator.com/item?id=46955602

[^3_5]: https://developer.harness.io/docs/platform/harness-ai/harness-agents

[^3_6]: https://martinfowler.com/articles/harness-engineering.html

[^3_7]: https://www.reddit.com/r/ClaudeAI/comments/1s9jm0d/i_had_claude_read_every_harness_engineering_guide/

[^3_8]: https://www.infoq.com/news/2026/02/openai-harness-engineering-codex/

[^3_9]: https://github.com/openai/symphony/blob/main/SPEC.md

[^3_10]: https://madplay.github.io/en/post/harness-engineering

[^3_11]: https://sjramblings.io/openai-symphony-autonomous-agent-orchestration/

[^3_12]: https://www.reddit.com/r/AI_Agents/comments/1jz07bs/how_do_you_manage_complex_deterministic_workflows/

[^3_13]: https://www.nxcode.io/resources/news/what-is-harness-engineering-complete-guide-2026

[^3_14]: https://www.digitalapplied.com/blog/openai-symphony-autonomous-code-orchestration-framework

[^3_15]: https://blog.dailydoseofds.com/p/the-anatomy-of-an-agent-harness

[^3_16]: https://dev.to/mscalessio/i-ported-openais-symphony-to-claude-code-a-complete-build-diary-4h61

