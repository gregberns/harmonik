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
- [L-013 — Bead-claim race when sibling implementers share a vague scope](#l-013) — `process-fix-applied` · `product-input`
- [L-014 — Sensor-to-sensor `blocks` edges are a structural smell](#l-014) — `open` · `product-input`
- [L-015 — "Continue claiming until 250k" was a main-thread rule mis-applied to implementers](#l-015) — `process-fix-applied` · `product-input`
- [L-016 — `git worktree remove` does not kill the sub-agent process](#l-016) — `process-improvement-pending` · `product-input`
- [L-017 — `defer_until` is a separate field from `status=deferred` and silently filters `br ready`](#l-017) — `process-fix-applied` · `product-input`
- [L-020 — Queue-with-context discipline (don't queue hygiene; when queuing, include enough to decide)](#l-020) — `process-fix-applied` · `product-input`
- [L-021 — Re-dispatch without investigation wastes slots (v60)](#l-021) — `process-fix-applied` · `product-input`

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

See also L-007.

---

### L-003 — RETIRED — merged into L-015 <a name="l-003"></a>

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

See also L-002 (bead-close ownership) as a specific historical instance of this general rule.

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

---

### L-013 — RETIRED — merged into L-015 <a name="l-013"></a>

---

### L-014 — Sensor-to-sensor `blocks` edges are a structural smell <a name="l-014"></a>

**Observed 2026-05-10.** Most `hk-hqwn.*` and `hk-i0tw.*` spec-corpus-sensor implementers had to use `br close --force` because the bead's declared `blocks` deps were themselves OPEN sensor beads. The pattern is sound on its face — the sensor is the deliverable, the blocker is design-level not code-level — but a sensor-to-sensor `blocks` edge encodes a phantom prerequisite.

**Why it's a smell.** A sensor (specaudit corpus test) closes when its target req has a passing test. It does not depend on another sensor's test being green; sensors are siblings, not a chain. Encoding `sensor-A blocks sensor-B` is leftover from copy-pasting taxonomy edges out of the parent spec at bead-load time.

**Possible automation.** Narrow analogue of L-011's parent-child→related conversion: scan sensor beads for `blocks` edges where both ends carry `kind:req` in the same `spec:*` namespace and convert to `related`. Scoped narrower than L-011 to avoid epic-progress collateral. Not yet pulled into a fix; opening as `open · product-input` so a future session can either apply the conversion or learn that the smell self-resolves as the corpus drains.

**Product input.** The daemon's edge-typing should distinguish "this code must compile before that one can" from "these are siblings under the same parent goal." Beads' `blocks`/`related` is a binary; harmonik's typed-edge story (per the spec drafts) wants finer granularity.

---

### L-015 — Sub-agent must exit on assigned scope (subsumes L-003 budget-utilization and L-013 partition-collision) <a name="l-015"></a>

**Motivating context (the path from L-003 → L-013 → L-015).** L-003 (2026-05-09): implementers were stopping at 80–160 k of a 250 k budget after working their assigned bundle, so a HARD RULE landed in `.claude/implementer-protocol.md` saying "Continue claiming until 250k" — scan `br ready` after each close and grab the next in-scope bead. L-013 (2026-05-10): that free-claim rule produced a partition-collision race — two implementers each claimed `hk-hqwn.11` (EV-008) under the vague scope "continue claiming `kind:req` in `spec:event-model`" and landed duplicate sensor files (cleaned up in `b8e2d73`). A mitigation landed requiring orchestrator briefs to partition the lane with an explicit non-overlapping key (section range, file glob, or req-id range). That patched within-spec collisions but not cross-spec ones.

**Observed 2026-05-10 (mid-session, the breaking case).** Two collisions in one session: (a) sx5860, dispatched on `hk-sx9r.58/.60` with continue-claim authorized within `spec:operator-nfr`, jumped spec boundaries and grabbed `hk-hqwn.8` from `spec:event-model` — exactly while the orchestrator was simultaneously dispatching the hqwn8 worktree on the same bead; (b) the L-013 race recurring on `hk-hqwn.11` despite the partition mitigation. User flagged the structural cause: the "Continue claiming until 250k" HARD RULE was a **main-thread** budget rule (orchestrator keeps the slot floor saturated until its own context approaches 250k) that had been copy-pasted into the implementer surface, where sub-agents dutifully enacted it. No brief partition could anticipate sx5860's cross-spec leap.

**HARD RULE (now in `.claude/implementer-protocol.md`).** Scope = brief. Exit after. An implementer works exactly the bead(s) named in its brief's SCOPE line and exits, regardless of remaining context budget and regardless of what `br ready` shows post-close. No free-claiming, period.

**Why the drift happened.** The 250k budget exists at the orchestrator level — it's the orchestrator's job to drain `br ready` until it approaches its own context ceiling, then write a fresh HANDOFF. Sub-agents are dispatched on a specific scope. When the rule landed in implementer-protocol.md, sub-agents (correctly reading the rule as authoritative) did exactly what the doc said: continued claiming after their assigned scope drained. The mitigation L-013 added (explicit per-brief partition lines) only patched within-spec collisions; sx5860's cross-spec leap was beyond what any brief partition could anticipate.

**Fix applied.** `.claude/implementer-protocol.md` rewritten: replaced the "Continue claiming until 250k" section with **"Do your assigned bead(s) and exit"** — implementer works the SCOPE line, then exits, regardless of remaining context budget. HANDOFF directives' IMPLEMENTER LIFECYCLE block updated: rule (b) now reads "DOES THE BEADS NAMED IN ITS BRIEF AND EXITS — no free-claiming." Briefs MUST NOT include "after close, continue claiming X" lines.

**Why this is safe now.** When the continue-claim rule landed (L-003 era), the orchestrator was wave-batching dispatches with multi-minute idle gaps; implementers free-claiming filled those gaps. With STREAM-NOT-WAVES (L-001) now enforced, the orchestrator refills slots on every implementer return — fast enough that implementer-side free-claim is structurally redundant and only adds collision surface.

**Product input.** The daemon's split-of-concerns should be unambiguous: orchestrator (main thread) owns the dispatch queue; implementers receive a scoped task and return. Atomic claim-on-dispatch at the daemon level eliminates the question entirely. Until then, "scope is what your brief says, not what `br ready` says" is the discipline.

---

### L-016 — `git worktree remove` does not kill the sub-agent process <a name="l-016"></a>

**Observed 2026-05-10 (session v23→v24, end of session).** After all three OLD-protocol implementers (i3152, mup11, sx5860) returned, I ran the standard merge dance: `git rebase main` → `git merge --ff-only` → `git worktree remove --force --force` → `git branch -D`. Wrote and committed the HANDOFF (`220a16e`). 90 minutes later — well after I'd considered the session complete on my side — I received task-completion notifications from BOTH sx5860 AND mup11 reporting they'd done substantial additional work. `br stats` confirmed: Open had dropped from 210 to 156 (54 more closures via direct `br close` calls), and `git worktree list` showed `agent-mup11` had RESURRECTED with five new commits ahead of main. The agents kept running inside their bash sessions long after the worktree directory was removed; their CWD-stale bash calls succeeded because (a) `br close` writes to a project-level SQLite file shared across worktrees, and (b) `git checkout` calls re-materialised the worktree.

**Why this matters more than L-013/L-015.** L-013 was scope-overlap; L-015 was a rule-drift in the implementer protocol. L-016 is a **platform-level escape hatch**: there's no kill signal an orchestrator can send to a runaway sub-agent. The agent runtime does not honour worktree-removal as a session boundary. Even if you write a perfect HANDOFF and consider the session closed, the sub-agent processes can continue accumulating writes against the project state for hours.

**Mitigation applied.** Final merge of the resurrected mup11 worktree landed 5 fresh commits cleanly (`d727453..36fd4fa`). HANDOFF updated to reflect Open=156 / Ready=11 / Closed=815 — the true post-everything state. Added a directive paragraph to HANDOFF: at session end, **before writing the handoff**, re-check `br stats` and `git worktree list`; if Open drops further or worktrees reappear, an OLD-protocol agent is still active and you must merge its tail.

**The only durable fix is the L-015 rule itself.** Once implementers stop free-claiming, this risk evaporates — a single-bead-and-exit implementer does its assigned work and reports back, and even if its bash sessions outlive the worktree, it has nothing to claim. The sx5860/mup11 cascades happened because they were dispatched *before* the L-015 fix landed mid-session; future sessions will not have OLD-protocol agents in flight.

**Product input.** The harmonik daemon's dispatch lifecycle MUST encode terminal sub-agent boundaries: signal-on-merge, dispatch-id-scoped writes, and a session-level fence so an orphaned agent's late writes are rejected rather than silently absorbed. The bootstrap process can't easily kill agent processes; the daemon (which owns the sub-agent runtime) can.

Note: L-015's exit-on-assigned-scope rule eliminates the conditions that produced L-016 — once sub-agents reliably exit, no stale writes accumulate. L-016 is conditionally obsolete; revisit if a violation reoccurs.

---

### L-017 — `defer_until` is a separate field from `status=deferred` and silently filters `br ready` <a name="l-017"></a>

**Observed 2026-05-10 (post-v25 audit session).** User flagged that recent agents had become confused about MVH scope. Audit of the 158 open beads showed 132 were correctly labeled `post-mvh`, 13 were genuinely MVH-relevant, and 0 of those 13 were dispatchable — every MVH-tagged task was blocked transitively by either deferred `hk-zs0.*` cognition beads or `post-mvh`-labeled tasks. After dragging structurally-needed blockers forward (4 zs0 beads un-deferred via `br update -s open`, 4 post-mvh beads relabeled, 5 over-aggressive `blocks` edges converted to `related`), `br ready` STILL did not surface the un-deferred beads. `br doctor --repair` rebuilt the stale `blocked_issues_cache` cleanly but ready depth did not change. Direct JSON inspection revealed the cause: each formerly-deferred bead carried `defer_until: 2027-01-01T16:00:00Z` as a separate field that survived the status flip and silently kept the bead out of `br ready`.

**Why this matters.** The L-011 directive ("trust but verify `br ready`") tells the orchestrator to suspect `blocked_issues_cache` and parent-child gridlock when Open ≫ Ready. It does NOT mention `defer_until`. An orchestrator un-defers a bead, sees `status=open`, sees `br ready` unchanged, and concludes the cache repair didn't take or the dep graph is still gridlocked — not that a third filter is in play. The previous v22→v25 handoffs all framed the corpus as "drained, blocked on cognition." It was actually drained, blocked on a labeling/dep mismatch, AND filtered by a stale `defer_until` field that no `br doctor` check surfaces.

**Fix applied.** Cleared via `br update <id> --defer ""`. After clearing the 8 affected beads, `br ready` immediately reflected the 3 root-unblocked beads (zs0.8, zs0.17, zs0.25) plus the previously-visible sx9r.22 + i0tw.15. Net: 5 dispatchable MVH tasks; 8 more chained behind them. HANDOFF directives' L-011 paragraph should be amended in a future session to add a third check: `br list --json` for nonempty `defer_until` on any bead with `status=open`.

**Product input.** Beads' filtering surfaces (`ready`, `stats`, status flags, defer_until field) are independent and don't cross-validate. Harmonik's daemon-side dispatch queue should expose a single "is this dispatchable right now" predicate rather than three orthogonal filters that compose by accident. The bootstrap can't fix Beads, but the daemon's wrapping layer can normalize.

---

### L-018 — Stalling on in-scope questions ("active dispatch") <a name="l-018"></a>

**Observed 2026-05-15 (session v43 transcript mining, prior session db8d1c56).** The session was user-judged "incredibly effective, low friction" yet still produced five distinct moments where the orchestrator paused to ask the user something it could have decided itself. The pattern: present competent analysis, then defer the obvious next action back to the user.

Concrete moments:
1. Critical path serialized — orchestrator asked "keep pulling from the broader queue, or hold?" Answer was in scope: dispatch non-conflicting parallel work.
2. Three spec-authoring beads on the bench — orchestrator listed all three and asked which to file. User had to add "remove constraints when small changes are needed."
3. Bead body offered three design candidates — orchestrator listed them and waited for a pick. Candidate 3 was clearly most consistent with existing `--dangerously-skip-permissions` usage.
4. Roadmap-drafting agent dispatched — orchestrator said it would pause for read before further dispatch. The output was informational, not decision-surfacing.
5. Dispatch-update message ended with "Want me to keep pulling?" after a clean block-chain analysis.

**Fix applied.** Added the **ACTIVE DISPATCH — DON'T PARK THE STREAM** directive paragraph to HANDOFF.md with five sub-rules, written as guidelines not policies per user preference (rules must include the *why* and the trigger condition; "fewer + more compact" was the user constraint). Existing DON'T ASK — EXECUTE was untouched but the new paragraph extends it from "resume-time say-back" to mid-session dispatch behavior.

**Why this matters.** The friction was not loud — the user answered each question in one short message. But every question consumed a user turn, broke the orchestrator's stream cadence, and slowed throughput. The compound cost is real even when no individual stall is dramatic.

**Product input.** Sub-agent dispatch could be the place this rule is encoded structurally — an implementer that finishes a bead and finds a dispatchable sibling shouldn't need the orchestrator to wake up, decide, and re-dispatch. STREAM-NOT-WAVES is the orchestrator-level expression; a daemon-level dispatcher could make it a property of the system instead of a behavior of the orchestrator.

---

## L-019 — Dispatch-priority ordering under multi-lane concurrency

Date: 2026-05-15
Tags: orchestrator, beads, dispatch

**Pattern.** With 100+ open beads and 5–7 parallel sub-agent slots, `br ready` returns more candidates than slots. FIFO ordering within the ready set is suboptimal — sibling beads have implicit sequencing (foundational before refine, impl before sensor, step-1 before step-N) that `br ready` doesn't see.

**Rule.** When two non-overlapping beads are both dispatchable in the same package/spec:
1. Prefer beads tagged `first-pass`/`foundational` before `refine`/`derive`.
2. Sensors and invariants queue AFTER their target implementation bead.
3. Multi-step beads (`-step-N` in title or body) queue in numeric order.
4. Where tags and body-analysis disagree, prefer body-analysis.

**Why this matters.** Out-of-order dispatch wastes a slot on work whose acceptance depends on a sibling not yet landed — the agent finishes, the sibling lands, and the original work needs touch-up. Cost: 1 extra agent cycle per inversion. Across a session of 8–12 dispatches, that's 1–2 wasted cycles.

**Product input.** harmonik's daemon should compute a `dispatch-priority` per bead from these signals (label-prefix scan, sibling-graph traversal, body-token match) so `br ready --sorted` returns the orchestrator-preferred order without judgment calls in the main thread.

---

### L-020 — Queue-with-context discipline (don't queue hygiene; when queuing, include enough to decide) <a name="l-020"></a>

**Observed 2026-05-15 (session v45, mid-stream).** Orchestrator queued an item to the user labeled `"AR-013 envelope drafts (A apply / B you read / C reviewer agent verifies first)"`. The change was actually trivial test-driven hygiene — a failing test wanted a missing markdown section and the fix was to add it. Queuing it (a) wasted a user turn on a decision that didn't need making, and (b) presented an undecodable surface: the user had no idea what `AR-013` referred to without grepping conversation history.

User feedback verbatim: *"For minor changes, corrections, bugs, etc I do not need to be consulted. If there are significant changes in the direction of the product, or user/agent impacting, that's where we should discuss before moving forward. But you also need to come to me with enough information I can make a decision on — that is in the instructions — and that wasn't done in the description above."*

**Root cause.** Information-asymmetry between orchestrator and user, compounded by a present-but-unenforced rule. The rules to *not* queue hygiene (resume-continue directive, autonomy dispatching fixers) and to *translate codes* (plain-language feedback) both existed. What was missing was a single mechanical pre-queue checklist that fires *before* the queue-the-user instinct.

**Fix applied.** This entry plus the standing memory `feedback_queue_with_context` codify the pre-queue test:

1. **Triage test (skip-queue):** "Would a competent collaborator object if I just did this?" Hygiene / test-driven fix / correction / internal rename / non-normative doc → just do it.
2. **If queuing is warranted (product direction, user/agent impact, irreversibility):** the surface MUST include (a) one plain-English sentence of what the change does — no bare codes; (b) why it's queued — what makes it non-routine; (c) concrete options with their consequences. A cold-drop-in reader must be able to decide without grepping conversation history.

The two rules compose: rule 1 catches the hygiene case; rule 2 catches the labeled-without-context case. Both failure modes appeared in the same friction event.

**Related discipline already in place.**
- `feedback_resume_continue_directive` (default = execute, not ask).
- `feedback_autonomy_dispatching_fixers` (dispatch obvious fix-up agents without confirmation).
- `feedback_plain_language` (translate codes on first mention).
- `feedback_queue_with_context` (the standing memory this entry cross-references).
- L-007 / L-018 (don't ask questions, don't park the stream).

L-020 is the integration point: a pre-queue checklist that takes the union of those rules and turns them into a single test the orchestrator runs *before* drafting any user-facing queue item.

**Product input.** A daemon-side dispatch surface should expose two channels: an autonomous-execute channel (hygiene, test-driven, routine) and a human-decision channel (direction-shaping, irreversible). The orchestrator-agent's judgment about *which channel a unit of work belongs in* is itself a deterministic function of the work's properties (label set, blast radius, reversibility). Encoding the channel choice in the bead — rather than re-deriving it per-event in the orchestrator — would eliminate the entire class of queue-the-wrong-thing failures. Candidate bead labels: `requires-direction` (always queue), `routine` (never queue), default behavior = follow rule 1.

---

### L-021 — Re-dispatch without investigation wastes slots (v60) <a name="l-021"></a>

**Observed 2026-05-26 (session v60).** 4 beads (hk-rnsjs, hk-24xn1, hk-aq17j, hk-7okmx) were dispatched across 3 consecutive batches, each failing with "close-without-impl." 12 dispatch slots consumed; 0 implementations landed. The orchestrator re-dispatched without investigating why the failures repeated.

**Root cause.** No post-failure triage step in the dispatch loop. The orchestrator treated "failed" as "retry-eligible" unconditionally, instead of checking whether the failure pattern indicated a structural problem (bad bead description, already-landed work, spec ambiguity).

**Fix applied.** Added a "Post-flight: failure triage" section to `AGENTS.md` (after the pre-flight checklist): first failure = retry-eligible; second failure = mandatory investigation before any further re-dispatch. Investigator checks: bead description quality, prior failure events in `events.jsonl`, already-landed grep.

**Product input.** Consider daemon-side tracking of per-bead failure count across batches. If a bead fails ≥2 times, the daemon could auto-flag it `needs-investigation` and exclude it from the dispatch queue until a human or investigator agent clears the flag. This removes reliance on the orchestrator-agent remembering cross-batch failure history.

Tags: `process-fix-applied` · `product-input`
