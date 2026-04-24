# Phase 3 Synthesis — 2026-04-23

> Synthesis across six reviewer personas after the 2026-04-23 amendment pass.

## Verdict

Foundation's **architectural direction holds up**. Five reviewers explicitly affirmed the core moves: three-artifact separation, handler-as-modularity-boundary, reconciliation-as-workflow, three-store cross-reference, daemon-vs-orchestrator-agent distinction. Skeptic's pressure on the decisions themselves mostly came back "defensible but could be argued harder."

**What reviewers converged on:** detail gaps, not direction gaps. Same pattern across all six: "semantics specified, surfaces hand-waved."

## Finding clusters (24+ findings across 6 reviews)

### Cluster 1 — Architectural gaps that block spec-draft (must fix before advancing)

- **Transition record storage** (Architect C-1, Critic C-10 — confirmed by 2 reviewers). Trailers can't hold `candidate_actions[]` etc. Pick a location: git notes, sibling tree file, or expanded envelope.
- **"Subsystem" term ambiguity** (Architect C-2). Pin to: Go package inside the daemon process for MVH; agents + orchestrator-agents are the only out-of-process actors.
- **JSONL divergence-evidence carve-out** (Architect C-3). Reconciliation reads JSONL for divergence detection while §3.6 forbids JSONL for state. Need explicit third category.
- **Node-level idempotency tag missing** (Critic C-4). §9.2 Cat 1 detector assumes a tag that doesn't exist in §2.1. Add to node type schema.
- **Recursive reconciliation unbounded** (Crash S6). Reconciliation workflows crash → need their own reconciliation → infinite regress. Needs one of: reconciliation nodes tagged Cat 1 always, OR reconciliation commits only the verdict (no intermediate checkpoints).
- **Orphan sweep on startup** (Crash S7). tmux sessions survive daemon crash; restarted daemon can collide with orphaned agents. Add explicit sweep to §8.2 step 1.

### Cluster 2 — Investigator-agent contract hardening (Crash S3/S4/S5)

- Mandatory wall-clock budget per reconciliation workflow; default verdict on budget exhaustion = `escalate-to-human`.
- Verdict schema validation; one-verdict-per-workflow structural rule; malformed verdict = `escalate-to-human`.
- Verdict-execution step must be durable and idempotent; a reconciliation commit whose verdict hasn't been executed is a discoverable condition.
- Snapshot token (git commit hash + Beads audit ID) to prevent investigator acting on stale state.

### Cluster 3 — Category taxonomy gaps (Crash S1/S2/S5/S8/S10/S11 + taxonomy-gaps)

- **Cat 0** "infrastructure unavailable" (Beads locked, `br` missing, git index locked) — halts before classification.
- **Cat 3a** "torn Beads write" — `br` adapter needs idempotency key.
- **Cat 3b** "verdict-unexecuted" — distinct auto-resolver.
- **Cat 6 split** "LLM-triageable" vs "mechanically unrecoverable" — don't burn tokens on hopeless cases.
- Missing scenario: "merge-commit-exists-but-not-closed-in-Beads" (inverse of premature-close).
- Missing scenario: "git worktree structurally broken" (detached HEAD, mid-rebase).
- **Detector must be run-scoped not bead-scoped** (Crash S8).

### Cluster 4 — Subsystem authoring clarity (Subsystem Implementer, 5 items)

- ControlPoint registry owner: S02 vs S05. Pin in foundation.
- Session-log pipeline end-to-end: no spec owns it. Add owner.
- `agent_type` stable identifier shape: pin string/enum.
- Goroutine ownership between daemon watcher and S04 handler: pin.
- Reconciliation workflow authoring owner: S07 or new subsystem.

### Cluster 5 — Operator surface (Operator Critical x5, Crash S11/S12)

- Startup failure mode catalog (git bad state, Beads corrupt, schema mismatch, stale pidfile race).
- `harmonik upgrade` contract (binary source, hash supply, drain-vs-reconciliation, cross-version state).
- Silent-hang detection.
- Multi-daemon commands (`harmonik list`, stop-by-cwd or socket, global budget).
- Reconciliation operator override (can veto verdict? promote to escalate-to-human?).
- Exit code taxonomy, config inventory.

### Cluster 6 — Design-defensibility to acknowledge (Skeptic, deferral candidates)

- Reconciliation taxonomy: 3-action-dressed-as-6-category → audit before spec-draft (may restructure).
- WIP-loss on `reopen-bead`: add obligation to capture recoverable WIP in reconciliation commit before verdict.
- No-DTW conditions (single machine, cheap re-execution, no irreversible external actions): state explicitly in problem-space.
- DOT untyped: add external validator as a named obligation.
- Multi-tenancy deferral in §7.10: acknowledge shared LLM budgets / operator identity / skill packages as real concerns.
- Centralized-controller trade-off: acknowledge graceful-degradation cost rather than dismiss.

## Affirmations (what held up well)

- Three-artifact separation (§1.9) — 4 reviewers.
- Handler-as-modularity-boundary (§4.12) — 3 reviewers.
- Reconciliation-as-workflow (§9.1) — 4 reviewers.
- Three-store cross-reference (§2.6) — 3 reviewers.
- Daemon-vs-orchestrator-agent distinction (§8.6) — 3 reviewers.
- Observational-vs-state-reconstruction split (§3.6) — 2 reviewers.
- Beads-as-task-ledger — 3 reviewers.
- Lease-by-run (§5.1) — 2 reviewers.
- Skill injection (§4.11) — 3 reviewers.
- Centralized-controller principle (§1.8) — 2 reviewers.
- Co-dependency resolution rules — 1 reviewer (Architect — called "a model for how other cross-cutting concerns should be handled").

## Proposed round-2 amendment plan

**In scope for round 2 (component-doc amendments):**
- Cluster 1 (6 items) — architectural gaps blocking spec-draft.
- Cluster 2 (4 items) — investigator-agent contract additions to §9.
- Cluster 3 (6 items) — taxonomy additions / refinements to §9.2-§9.3.
- Cluster 4 (5 items) — owner naming in relevant components.

**Acknowledged-only in round 2:**
- Cluster 6 (6 items) — add explicit acknowledgment paragraphs; no structural change.

**Deferred to spec-draft pass:**
- Cluster 5 (6 items) — operator surface; component doc should **name the obligations** so spec-draft knows what to produce, but detailed catalogs (exit codes, config inventory) belong in spec-draft.

**Execution options:**
1. Spawn a synthesis subagent to produce a concrete per-finding delta plan (like Phase 1 gap analysis), review with user, then execute.
2. Skip round 2 entirely; advance kerf to change-design + spec-draft, let those absorb findings ad-hoc (riskier — Cluster 1 findings are foundation-normative, not spec-draft-level detail).
3. Hybrid: fix Cluster 1 now as surgical amendments; bundle Cluster 2-4 into the change-design pass.

**Standing lean (for next session):** option 1 — a concrete delta plan, reviewed, then applied.

## Skeptic's taxonomy-restructure question — RESOLVED 2026-04-24

Skeptic had argued the 6-category reconciliation taxonomy is really a 3-action taxonomy (auto-resume for Cat 1, 4, 5; investigator for Cat 2, 3, 6) and wondered whether to restructure by action with state as sub-classification.

**Resolved 2026-04-24 by user:** keep 6 detection categories; the action layer is explicit as the new §9.2a action-mapping table. User rationale: the question "doesn't matter until it occurs, and an agent is going to figure it out at that time." This is runtime-resolvable detail, not a structural blocker. **Do not re-audit, do not reopen.** Any future reviewer flagging this as "critical" should downgrade to a note.
