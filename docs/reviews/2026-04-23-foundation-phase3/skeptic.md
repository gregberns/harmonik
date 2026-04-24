# Skeptic Review — Foundation 2026-04-21

## Summary verdict

The foundation has tightened considerably since the first-round critics worked on it. Most decisions are well-reasoned; the architecture is internally consistent and the cross-references are disciplined. However, three decisions still read as "first plausible answer that came up in a session" rather than "answer that survived adversarial analysis": **the reconciliation category count (six categories, with explicit granularity bias)**, **the per-project-daemon-as-multi-tenancy-answer move (§7.10)**, and **the fresh-worktree-on-re-run rule's interaction with intra-run rollbacks**. The Beads and DOT decisions hold up well under pressure. The no-DTW decision holds up as long as one assumption — that MVH stays single-machine and restart is cheap — stays true; if it doesn't, the cost of rebuilding is high. The centralized-controller principle is asserted more than it is argued, and I want to register that concern even though I think the decision itself is probably right.

## Decisions I want to challenge

### 1. Reconciliation taxonomy — six categories, "granularity bias toward more"

**Challenge.** The doc explicitly states (§9.2) "granularity bias: prefer more categories so the investigator-agent's decision surface stays simple." This is a shape choice that was probably made in <30 minutes of design and hasn't been stress-tested. Why six? Why not four (idempotent / non-idempotent / store-disagreement / clean)? Why not organize by **verdict-required**, i.e., "cases the daemon can handle alone" vs. "cases an investigator must adjudicate"?

**What the spec says.** The categories are carved by *what went wrong* (idempotent rerun, non-idempotent in-flight, store disagreement, recoverable known state, clean restart, integrity violation). The rationale offered is "the investigator's decision surface stays simple." But that argument cuts the opposite way: if the *daemon* has six categories, the investigator gets called for three of them (Cat 2, 3, 6), and each gets its own "playbook" — meaning three investigator codepaths rather than a unified one. The complexity didn't vanish; it moved from the daemon to the playbooks.

**Is the justification adequate?** No. The taxonomy is organized by *state classification* — which is "what is happening" — rather than by *workflow action needed* — which is "what we do about it." Two actions are auto-resume (Cat 1, 4, 5), one is normal-flow (Cat 5 is just "nothing was interrupted"), three are investigator-dispatch (Cat 2, 3, 6). That is a 3-action taxonomy dressed up as a 6-category taxonomy. The research pass should have been asked: "would fewer categories with more verdicts work better?" It wasn't, so the shape we got is the shape we decided first. **Weakly-justified; probably fine as long as someone does this audit before spec-draft lands.**

### 2. No-DTW, and deterministic-reconciliation-instead

**Challenge.** The spec claims (multiple places) that git-checkpoint + Beads + reconciliation-as-workflow is a drop-in replacement for DTW's crash-recovery semantics, with "no loss." That is a strong claim. DTW engines (Temporal, Restate) give you: **exactly-once side-effect guarantees via activity recording**, **waiting on external events with automatic resumption**, and **deterministic replay with compensation**. Reconciliation gives you: "rebuild state from git, guess at in-flight, ask an LLM when guessing fails."

**What the spec says.** "The existing framing [needing DTW] was overreach" (SESSION_HANDOFF). "Task re-execution is cheap" (QUESTIONS §Resolved Q-R2). "Git history = source of truth for completion" (locked decision #12).

**Is the justification adequate?** For **MVH on a single developer machine running short agent tasks**, yes. Task re-execution is genuinely cheap (re-run the agent, it costs seconds of LLM time; the workspace is re-derivable from git). The "workflow-cannot-die" semantics are wrong for this threat model. **But the rationale is conditional on single-machine, cheap-re-execution, no-external-side-effects assumptions.** The spec doesn't call out what breaks if those assumptions change. If harmonik ever needs workflows that (a) span machines, (b) include expensive irreversible external actions (deploy-to-prod nodes, send-email nodes), or (c) need to survive days with external wait-states, the "reconcile-from-git" model has no answer. **Probably fine but the conditions need to be spelled out.** I would add an explicit section: "Reconciliation replaces DTW only when the following conditions hold: X, Y, Z. If these change, reconciliation becomes insufficient and DTW must be revisited."

### 3. Per-project daemon as multi-tenancy answer

**Challenge.** §7.10 (Operator NFR) says: "Multi-tenancy / per-tenant cost attribution. Per-project daemon isolation means multi-tenancy reduces to 'run more daemons, one per project' — an OS-process-isolation concern, not a harmonik-code concern." This is very convenient. It is also hand-waving. OS-process-isolation does NOT give you: cross-project visibility in one attach UI, cross-project bead dependencies, shared skill registries, unified operator commands across projects, shared resource budgets across an operator's portfolio.

**What the spec says.** The per-project isolation is asserted as a structural benefit ("contains blast radius") — but only named in the decomposition; I don't see it pressure-tested. The operator use case of "I have 5 harmonik projects running; I want to see everything" is not addressed.

**Is the justification adequate?** Partial. The blast-radius claim is real (a corrupted JSONL or Beads in project A does not affect project B). But hidden coupling exists: (a) the same operator's LLM budget is shared across daemons in practice — per-daemon budget means nothing if the operator runs 10 daemons and blows the Anthropic quota, (b) the operator identity is shared — `harmonik attach` in project A and project B are the same human, and presumably the same skills / same `br` binary, (c) skill packages are likely global per machine, so provisioning failures in one project reflect a global install issue, not a project issue. **Probably fine for MVH but the "this scales to real multi-project use" claim isn't supported.** Specifically, I'd push back on §7.10's dismissal of multi-tenancy — this is a "we'll deal with it later" dressed up as "we don't need to deal with it because daemons."

### 4. Fresh-worktree-on-re-run rule

**Challenge.** §5.9 + §2.5 say: `reopen-bead` → fresh worktree, fresh branch, new run. `reset-to-checkpoint` → keep worktree, same run. The boundary between "fundamental failure" and "recoverable intra-run state" is supposedly mechanical (investigator verdict enum) but the *investigator's own decision* of which verdict to emit is cognitive.

**What the spec says.** "Mechanism-tagged: the decision between intra-run continuation and fresh-worktree re-run is deterministic based on the investigator's verdict enum." That is technically true but misleading — the *enum value* comes from a cognition-tagged agent. The daemon executes the verdict mechanically; the verdict itself is chosen by an LLM reading the state. So the "mechanism" tag applies only to one step of a two-step process whose net character is cognitive.

**Is the justification adequate?** It's defensible but the framing is squishy. The bigger concern: **fresh-worktree-on-re-run loses work**. If the builder agent got 80% of the way through a non-trivial implementation and crashed, a `reopen-bead` verdict throws it all away. The intra-run `reset-to-checkpoint` keeps the worktree but reverts to a named checkpoint — but there's no middle ground ("keep the WIP, resume where the agent was, but launch a new agent"). That middle ground is exactly `resume-with-context`, which is an intra-run verdict — so WIP is preserved in the intra-run path but lost in the re-run path. **Is this intended?** I think yes (the re-run path is "the work is not actually done" per §9.5), but the spec doesn't say *how much work gets lost* is bounded. If a run's worktree has WIP the investigator cannot recover from, the work is gone. **Weakly-justified; strengthen by adding: "before emitting `reopen-bead`, the investigator MUST capture any recoverable WIP in a reconciliation commit for post-hoc review."**

### 5. Centralized-controller principle — asserted more than argued

**Challenge.** §1.8 is load-bearing and unchallenged. The rationale given is "collapses an entire class of coordination problems (file reservations, agent-to-agent message routing, distributed consensus)." That is true for the problems it solves. But the counterfactual — "what does the decentralized model buy you?" — is dismissed, not refuted.

**What the spec says.** The Gas Town polecats/mayors pattern is named as the "explicit inverse" and rejected. The rejection rationale is process-efficiency: centralized dispatch avoids file-reservation and agent-to-agent IPC complexity. But the Gas Town model has one real benefit: **graceful degradation under centralized-component failure**. In a decentralized model, if the mayor dies, polecats still know how to negotiate. In harmonik, if the daemon dies mid-run, every agent goes silent and reconciliation is required on restart.

**Is the justification adequate?** For harmonik's scope (single-user, single-project, developer-machine) yes — the daemon is colocated with the work, the failure mode "daemon dies" is the same failure mode as "machine dies" and the recovery is the same reconciliation path either way. But the spec should acknowledge this: centralized-controller's weakness is that daemon availability is a precondition. In a decentralized design, you could have agents that continued working in isolation and sync'd on reconnect. Harmonik explicitly can't. **Probably fine for the MVH target, but the spec dismisses a real trade-off rather than acknowledging it.**

### 6. DOT lock-in

**Challenge.** DOT is a very old format. It has: no native schema, no typed attributes (everything is strings), poor tooling for large graphs (>1000 nodes), no native versioning, and cross-tool behavior differences (GraphViz vs. tool X may parse attributes differently). What foreclosed paths matter?

**What the spec says.** "Graphviz-renderable, diffable as text, NL→DOT ingestor path available" (SESSION_HANDOFF), "smallest standard graph-serialization format with native node/edge attribute support" (problem-space).

**Is the justification adequate?** For MVH scale (workflows of ≤50 nodes) DOT is fine. **Concerns:** (a) workflow schema validation — DOT attributes are untyped strings, so `policy_ref` pointing to a nonexistent policy is a runtime failure, not a parse failure; foundation needs a DOT-attribute schema enforced external to DOT itself; (b) large workflows — if workflow composition later needs hundreds of nodes (reconciliation workflows composed with the improvement loop composed with the task pipeline), DOT legibility breaks; (c) DOT has no native "sub-workflow" primitive — the spec uses `type=sub-workflow` as a node kind, but inlining vs. referencing has no standard. **Probably fine but doors that close:** typed-workflow-schema (e.g., Starlark or a custom typed DSL) is much harder to adopt once DOT workflows ship. I'd strengthen by adding: "the workflow-attribute schema is enforced by a separate validator at ingest time; DOT's lack of native typing is a known limitation absorbed by this validator."

## Hidden assumptions

1. **JSONL is readable fast enough that observational replay is useful.** With a busy project this file grows unboundedly; rotation policy (Q-P1) is still open. The spec assumes the event log is queryable in practice; at 10GB it isn't.
2. **Beads's SQLite fork stays maintained.** Pre-1.0, one-maintainer project. The adapter layer mitigates breakage but not abandonment. If `Dicklesworthstone/beads_rust` stops being maintained in 6 months, harmonik's "Beads is the task ledger" becomes "harmonik forks Beads" very quickly.
3. **Skills are package-installable and deterministically resolvable.** §4.11 and §6.11 treat skill provisioning as mechanical (given name → resolve → install). In practice, skill packages have transitive dependencies, version conflicts, and cross-project install conflicts. The spec doesn't address skill-version resolution.
4. **Agent processes are cheap to launch and kill.** The "restart = re-launch the agent" model assumes launching a Claude Code process is sub-second. For agents with large context preloading, launch cost is nontrivial. This assumption underpins the "task re-execution is cheap" claim for no-DTW.
5. **Session logs are stable per-agent and aggregatable.** The per-workspace session-log directory assumes handlers cooperate with harmonik's chosen path. What if Claude Code writes its session log somewhere else in a future version? §5.3's contract is harmonik-imposed, not handler-enforced.
6. **One operator per daemon.** §7.3's pause semantics assume a single human operator. What if two humans are attached via `harmonik attach`? The spec is silent on concurrent-operator semantics.

## Doors closed by recent choices

- **Multi-machine workflows.** The per-project-daemon + git-as-source-of-truth model assumes the daemon and the git repo live on the same machine. A workflow node that needs to execute on a different machine (e.g., a deploy step on a remote build server) has no answer. Reopening this requires rethinking state authority, not just adding RPC.
- **Multi-operator in the same daemon.** §7.10 dismisses this with "run more daemons." True at the OS level but not at the collaboration level — two operators on the same project can't share attach state without a shared daemon.
- **Feature as a primitive.** §1.9 explicitly rejects this. If user research later shows operators really do think in features (vs. specs + workflows + beads), unwinding this is hard because it's a glossary-level decision that propagates through every spec.
- **JSONL as state-authoritative.** §3.6 says JSONL is observational only. This closes the door on "walk JSONL to reconstruct if git is corrupted" — if git is lost, JSONL can't save you. The spec acknowledges this but doesn't propose any redundancy.
- **Synchronous consumers can't be >1 per event type.** §3.7's declaration-time check forecloses a design where two critical-path subsystems both need to synchronously process the same event. Probably fine but noted.

## Affirmations

- **Beads-as-task-ledger** survives skepticism well. The split (Beads = coarse status; harmonik = fine workflow state) is principled, the `br`-CLI-only access is simpler than an MCP dependency, and the pre-1.0 risk is explicitly absorbed via adapter. This reads as "the right answer," not "the first plausible answer."
- **Three-artifact separation (§1.9)** is well-reasoned. The refusal to introduce "feature" as a fourth primitive avoids a known anti-pattern (over-normalized product ontologies), and the many-to-many relationship with no projection is architecturally clean.
- **The lease-by-run rule (§5.1)** elegantly collapses worktree-ownership coordination. The explicit rejection of per-agent lease is load-bearing and correct.
- **Handler skill injection (§4.11)** is a genuinely good generalization of what started as a Beads-CLI motivating instance. The pattern closes a real gap (capability-supply as a first-class concern) and is mechanism-tagged cleanly.
- **The outcome-spine integrated contract (§2.2)** — specifically the "two views of one underlying record" framing for transition-vs-transition_event — resolves a duplication concern elegantly.
- **The reconciliation-as-workflow principle (§9.1)** is elegant. No separate subsystem is needed; reconciliation uses the same primitives as normal work. This is the right move, even if the category taxonomy inside it deserves a harder look (see challenge #1).
- **Three-store cross-reference with git-wins-on-completion (§2.6)** gives a clean arbitration rule for divergence. Arbitration rules are often fudged; this one isn't.

---

**Bottom line:** The foundation is substantially more defensible than the October version was. The decisions that feel most "freshly made" — reconciliation categories, per-project-daemon as multi-tenancy answer, fresh-worktree-on-re-run — deserve one more adversarial pass before spec-draft. The no-DTW and centralized-controller decisions are probably right for the declared scope but their conditional nature should be surfaced (what changes if scope changes). Beads, DOT, and skill-injection all hold up. The document is mostly convincing; the three or four places where it isn't are specific and surfaceable.
