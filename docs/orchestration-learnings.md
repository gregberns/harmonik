# Orchestration Learnings

Friction observed during orchestrator-led implementation of harmonik (the `kerf` + `br ready` corpus). Each entry captures: what we saw, what we think caused it, what we tried, what's still open, and what it implies for harmonik's product design.

This is **dual-purpose**:

1. **Process meta-knowledge.** Every session reads it on `/session-resume` and appends new observations. Rules that prove durable get promoted to `.claude/implementer-protocol.md` or the HANDOFF directives block; rules that prove false get marked `retracted` not deleted.
2. **Product input.** Every entry asks: "if harmonik existed today, would this friction still happen, or would the system absorb it?" Entries tagged `product-input` are candidate features for the eventual daemon.

**Don't tidy this file.** Keep dated raw observations. Synthesis lives in HANDOFF directives and the protocol doc; this file is the audit trail.

## Status tags

- `process-fix-applied` — rule landed in `.claude/implementer-protocol.md` or HANDOFF directives.
- `process-improvement-pending` — agreed direction, not yet codified.
- `product-input` — informs harmonik's daemon/orchestrator design, not just the bootstrap process.
- `open` — observed but not yet diagnosed.
- `retracted` — proved wrong; kept for the audit trail, but don't follow this rule.

## Index

- [L-001 — Wave-batch synchrony stalls slot reuse](#l-001) — `process-improvement-pending` · `product-input`
- [L-002 — Implementer bead-close ownership ambiguity](#l-002) — `process-fix-applied`
- [L-003 — Implementers stop short of the context budget](#l-003) — `process-fix-applied` · `product-input`
- [L-004 — Main-thread narrative overhead per cycle](#l-004) — `process-improvement-pending` · `product-input`
- [L-005 — Brief composition is per-bundle, not templated](#l-005) — `process-fix-applied` · `product-input`
- [L-006 — Queue structural drain is not surfaced proactively](#l-006) — `product-input`
- [L-007 — Sub-agents ask questions when they should decide](#l-007) — `process-fix-applied` · `product-input`
- [L-008 — SUBSUMED-detection improves dramatically with sibling pointers in the brief](#l-008) — `process-fix-applied` · `product-input`
- [L-009 — Spec discrepancies surface late (during implementation, not during spec review)](#l-009) — `open` · `product-input`
- [L-010 — Cherry-pick is the reliable merge fallback under worktree churn](#l-010) — `process-fix-applied`
- [L-011 — Beads parent-child edges gridlocked the dispatchable surface](#l-011) — `process-fix-applied` · `product-input`

---

## Entries

### L-001 — Wave-batch synchrony stalls slot reuse <a name="l-001"></a>

**Observed 2026-05-09 (session v20→v21).** Orchestrator dispatched 13 / 10 / 5 implementers in **batches**, then waited for the entire batch to return before sizing the next batch. With variance in implementer runtime (60 s SUBSUMED-only returns to 17 min full-bundle returns), there were multi-minute windows where 4 of 5 slots sat idle waiting on the slowest sibling.

The HANDOFF v20 directive already said *"Refill on review-dispatch, not review-return — when you spawn a reviewer, immediately spawn the next implementer if the ready queue has non-overlapping work."* I read it, agreed with it, and **still** drifted to batch-thinking, because writing wave-summary prose between dispatches is psychologically the lowest-friction next action.

**Hypothesis.** Batch-mind is the default; stream-mind requires explicit, mechanical refill discipline. The orchestrator agent doesn't have the local incentive to dispatch promptly — it has the global goal but the per-cycle path-of-least-resistance is to summarize.

**Fix applied.** HANDOFF v21 directives elevate the rule (STREAM-NOT-WAVES section) and forbid wave-narration prose between dispatches.

**Product input.** harmonik's daemon SHOULD NOT rely on a human-LLM orchestrator to maintain slot-floor discipline. The daemon-side dispatcher should be deterministic: on every implementer-completion event, it (a) merges or queues the merge, (b) inspects `br ready` minus in-flight claims, (c) auto-dispatches the next non-overlapping implementer if the floor isn't met. The orchestrator-agent (LLM) supervises decisions that genuinely need judgment (SUBSUMED-vs-implement, spec-discrepancy resolution, cognition gates) — not slot-keeping.

This is the central thesis: **deterministic skeleton, probabilistic organs.** Slot-keeping is skeleton.

---

### L-002 — Implementer bead-close ownership ambiguity <a name="l-002"></a>

**Observed 2026-05-09.** `.claude/implementer-protocol.md` line 90 said `Do NOT close the bead. Do NOT update the bead's status (the orchestrator owns claim/close transitions).` HANDOFF v20 + memory `feedback_br_ownership` say the agent owns all `br` operations including close. Implementers in this session split: about half closed their own beads, the other half deferred to the orchestrator. The orchestrator then ran cleanup passes to retroactively close beads whose code was already on main — wasted throughput.

**Fix applied.** `.claude/implementer-protocol.md` rewritten this session: explicit "Bead-close ownership (CLARIFIED — agent owns)" section, instructing implementers to `br close` after every commit including SUBSUMED-only closures.

**Product input.** Process-of-record ambiguity is a design smell. In the harmonik product, transitions should be encoded as control points with one and only one authority — the rule shouldn't have to be re-read to know who closes a bead. The daemon's claim/close discipline is part of the deterministic skeleton; the implementer agent only emits a verdict, the daemon transitions state.

---

### L-003 — Implementers stop short of the context budget <a name="l-003"></a>

**Observed 2026-05-09.** Wave 1 + 2 implementers averaged 80–160 k tokens of a 250 k budget per dispatch. Some stopped after 12 k. Reason: each brief assigned a fixed bundle (e.g. "hk-sx9r.75–.79"); the implementer worked the bundle, found no instruction to claim more from `br ready`, and stopped. With 1/3 to 2/3 of dispatch budget unused per implementer, total session throughput was ~50% of what the budget allowed.

**Fix applied.** `.claude/implementer-protocol.md` adds a HARD RULE section "Continue claiming until 250k": after each bead closes, scan `br ready` filtered to in-scope packages and claim the next ready bead; stop only on context exhaustion, queue empty, or hard blocker.

**Product input.** Budget-utilization is a daemon-side metric that should drive implementer-agent loops natively. The agent shouldn't be told "claim more" — the daemon should keep feeding it work until the budget envelope tightens. This is one of the genuinely deterministic disciplines: there's no judgment call in "you have budget, queue is non-empty, claim the next one." Promote it to skeleton.

---

### L-004 — Main-thread narrative overhead per cycle <a name="l-004"></a>

**Observed 2026-05-09.** The orchestrator main thread spent ~1–3 minutes per implementer-return writing user-facing prose summaries (wave totals, bead lists, spec-discrepancy notes). Multiplied across 28 dispatches in this session, that's roughly an hour of orchestrator wall-clock spent on summarization rather than dispatch.

The summaries have value (the user uses them to sanity-check), but they're per-return, while a per-session summary would carry the same signal at much lower cost.

**Hypothesis.** The orchestrator role is conflating two duties: (1) maintain slot-floor + merge correctness (mechanical), (2) keep the user informed (narrative). Mixing them at per-return cadence over-weights the narrative.

**Fix proposed (not yet applied).** Per-return: ≤2-line update ("merged X, dispatched Y"). Per-session: full summary at handoff time. HANDOFF v21 directives carry this as a rule.

**Product input.** harmonik's "operator UI" should be the narrative surface. The orchestrator-agent should emit terse structured events; the UI renders summary at user-pull cadence, not agent-push cadence. The agent shouldn't be writing prose for a UI it doesn't have to render.

---

### L-005 — Brief composition is per-bundle, not templated <a name="l-005"></a>

**Observed 2026-05-09.** Each implementer received a 25–35 line tailored brief (worktree setup, bundle list, sibling-pattern hints, protocol pointer, no-ask reminder, reporting format). Composition cost: roughly 1 minute per dispatch. With protocol clarifications now in `.claude/implementer-protocol.md`, the brief could collapse to ~8 lines: worktree path, package filter, starting-bead hint, sibling-pattern pointer.

**Hypothesis.** Brief inflation comes from re-stating protocol every time, against the (correct) instinct that an under-briefed implementer fails. Protocol consolidation removes that pressure.

**Fix applied 2026-05-10.** Brief template landed as the appendix of `.claude/implementer-protocol.md` ("Appendix — Brief template"). Orchestrator briefs are now parameter-fills against the template; the template forbids paraphrasing the bead body and codifies the SIBLING line as required when a prior sibling exists (per L-008).

**Product input.** Brief composition is structurally a deterministic operation: bead → package → sibling-pattern → worktree-name. The daemon should compose briefs; the orchestrator-agent shouldn't be a template engine.

---

### L-006 — Queue structural drain is not surfaced proactively <a name="l-006"></a>

**Observed 2026-05-09.** Started session with ~140 dispatchable ready beads. By session end, the dispatchable surface had shrunk to ~9, with most remaining ready beads being out-of-scope cognition spec drafts (`hk-zs0.*`). The orchestrator only realized this by repeatedly running `br ready --limit 0 | grep -v zs0`. There was no surfaced metric distinguishing "10 implementers idle because the agent didn't dispatch" from "10 implementers idle because there's nothing to claim."

**Product input.** harmonik's daemon should track *dispatchable depth* — not raw ready count, but ready beads minus in-flight claims minus excluded labels. Surface this as a first-class metric so the orchestrator-agent (and the operator UI) can distinguish orchestrator failure from corpus exhaustion. When dispatchable depth drops below floor, the daemon should signal "queue draining" before all slots actually idle.

---

### L-007 — Sub-agents ask questions when they should decide <a name="l-007"></a>

**Observed 2026-05-09.** Multiple implementers and (earlier sessions) reviewers ended their reports with A/B questions back to the orchestrator: "should I implement X or close SUBSUMED?", "is this the right path?". The orchestrator can't answer (it dispatches and moves on), so the question silently becomes a stop. The user's standing memory `feedback_resume_continue_directive` covers the orchestrator's resume behavior; sub-agents needed the equivalent rule.

**Fix applied.** `.claude/implementer-protocol.md` now carries an explicit "Don't ask questions back" section. HANDOFF v21 directives carry the rule for the orchestrator's own behavior under `/session-resume`.

**Product input.** Probabilistic agents at scale will always be tempted to defer. The harmonik product should make deferral structurally hard for normal-path beads (close-it-yourself transitions, judgment-call documentation in commit body) and structurally easy for the small fraction of beads that genuinely need human judgment (Cat-6 escalation, gate-pending). The current "ask the human" mode is a default that should be replaced with explicit escalation channels.

---

### L-008 — SUBSUMED-detection improves dramatically with sibling pointers in the brief <a name="l-008"></a>

**Observed 2026-05-09.** Wave 1 dispatches that included a "sibling pattern: prior-wave landed `<file:line>` covering `<acronym-id>`" line had ~30% SUBSUMED-detection rate (correctly identifying that previously-landed code already covered the bead). Briefs without sibling pointers had ~5% — the implementer dutifully reimplemented work.

**Fix applied.** Brief template in `.claude/implementer-protocol.md` (pending — see L-005) should require sibling-pointer when the orchestrator knows of one.

**Product input.** SUBSUMED-detection is a generalized "is this work already done" check. The daemon should run an automated pre-claim grep against the package's existing test/code identifiers (the bead's primary type, acronyms it cites, file-naming conventions). If the grep matches strongly, the bead is auto-tagged `likely-subsumed` and routed to a fast-path verification implementer rather than a full one. This collapses SUBSUMED-only dispatches (60 s) from the slow-path (10–15 min).

---

### L-009 — Spec discrepancies surface late (during implementation, not during spec review) <a name="l-009"></a>

**Observed 2026-05-09.** Two non-trivial spec discrepancies in `specs/operator-nfr.md` (ON-013c vs §7.1 disagreement on `resume`-while-`running` exit code; ON-027 saying "eight steps" while §7.2 pseudocode said "seven") only surfaced when implementers wrote test fixtures and noticed the contradictions. They were not caught by spec review.

A separate one (`HandlerRef RECORD` not defined in handler-contract.md) surfaced when an implementer needed the type. A fourth (`expr-lang/expr` lacking `expr.Timeout()`) surfaced when an implementer tried to use a spec-prescribed API.

**Hypothesis.** Spec review at write-time is heuristic-bounded; only fixture-grade contact with the spec catches contradictions. This is structural, not a process failure.

**Open.** Should there be a "fixture-first spec review" pass — where every new normative section in `specs/*.md` requires at least one fixture/sensor in `internal/specaudit/` before merge? AR-005 already enforces a tag check; a deeper "internal consistency" check would be valuable.

**Product input.** harmonik's spec subsystem should run a consistency-check pass over normative cross-references on every spec mutation. The check is mechanical (find every section X-### reference, verify it resolves; find every "N steps" / "N values" claim, verify against the enumerated list).

---

### L-010 — Cherry-pick is the reliable merge fallback under worktree churn <a name="l-010"></a>

**Observed 2026-05-09.** `git merge --ff-only` reported "Already up to date" multiple times during heavy worktree churn, even when the worktree branch clearly had a new commit ahead of main. Cherry-picking the worktree's tip directly onto main worked every time. Hypothesized cause is a CWD-drift bug in git's worktree implementation under fast iteration.

**Fix applied.** HANDOFF directives carry the cherry-pick fallback; this session validated it five additional times.

**Process note for future.** When this happens, do NOT use `git reset --soft main` from the worktree — it preserves the worktree's stale tree and stages deletions of files landed in other waves. Recovery cost is high (30+ min, possibly closed beads requiring re-implementation). This is documented in HANDOFF directives but worth re-stating here as it's a sharp edge that recurs.

---

### L-011 — Beads parent-child edges gridlocked the dispatchable surface <a name="l-011"></a>

**Observed 2026-05-10 (this session).** `br ready` returned only 2 beads (both epics) despite 487 open beads in the corpus. Multiple sessions in a row (v19, v20, v21) had reported "queue drained, only zs0 cognition spec drafts remain" — but that read was an artifact of the dep model, not the corpus.

**Root cause.** Beads' `idx_dependencies_blocking` SQL index treats `parent-child` edges as blocking, equivalent to `blocks`/`conditional-blocks`/`waits-for`. The corpus had 992 parent-child edges (sub-bead → parent epic). Every sub-bead was therefore "blocked by" its open parent epic, and every parent epic was open until its sub-beads closed → full DAG gridlock. 913 of 487 open beads were blocked solely by their open parent.

The orchestrator's gridlock-detection heuristic ("queue is drained") trusted `br ready` as authoritative. It was authoritative for "what `br ready` returns" but NOT for "what work is dispatchable" — those diverged hard.

**Fix applied.** `UPDATE dependencies SET type='related' WHERE type='parent-child'` — converted all 992 parent-child edges to `related` (non-blocking). Cache rebuilt. `br ready` jumped from 2 → 487. Trade-off: `br epic status` now reports 0/0 children for every epic (epic-progress tracking lost). For an MVH-stage corpus the flow gain dominates — epic-progress can be reconstructed from labels (`spec:event-model` etc.) when needed.

**Process change.** Future orchestrators MUST NOT trust `br ready` count as proof the corpus is drained. Cross-check against `br stats` Open count and `br blocked --json` blocker-distribution. If most blocked beads have a single blocker that is an open parent epic, the gridlock is structural not work-driven.

**Product input.** harmonik's bead model (or its choice of underlying ledger) MUST distinguish *structural* parent-child relationships (taxonomy / rollup, non-blocking) from *blocking* dependencies. Beads-CLI's choice to fold them into one index is an upstream design decision we should not silently inherit. Two options for harmonik's daemon: (a) use Beads but only emit `blocks`/`related` edges (encode hierarchy via labels or external `epic` table); (b) fork or replace the ledger to give parent-child its own non-blocking semantics. Either way, the daemon should never accept "queue drained" as ground truth without checking the structural-vs-blocking distinction itself.

---

## Adding to this log

When you (orchestrator agent or human collaborator) observe friction:

1. Append a new entry with a sequential `L-NNN` ID, today's date, and the status tags.
2. State **what** you saw, **why** you think it happened, and (if you applied a fix) **what** you did.
3. Cross-reference HANDOFF directives or `.claude/implementer-protocol.md` if the entry produced a process change.
4. Mark `product-input` if the friction would be absorbed by harmonik's daemon-side skeleton; the entry then becomes a candidate for a future kerf work.

Kept terse on purpose — three-paragraph entries, not essays.
