# Harmonik Tasks

> Actionable work items. Grouped by phase. Check off as resolved or move to a log entry.
>
> Last updated: 2026-04-24 (overnight) — **spec corpus pass complete**: 5 batch-1 specs reviewed (v0.3), 5 batch-2 specs draft (v0.1).

## Spec corpus — Phase 0b (in progress)

### Batch 1 — REVIEWED (v0.3)

All five specs went through full review cycle: draft → 3 round-1 reviewers → integrate → 3 round-2 reviewers → integrate → status: reviewed.

- [x] **`specs/architecture.md`** (AR, 643 lines, 52 reqs, 2 invariants).
- [x] **`specs/execution-model.md`** (EM, 1091 lines, 65 reqs, 3 invariants).
- [x] **`specs/event-model.md`** (EV, 1032 lines, 70 events + 42+ reqs, 6 invariants).
- [x] **`specs/handler-contract.md`** (HC, 949 lines, 57+ reqs, 5+ invariants).
- [x] **`specs/control-points.md`** (CP, 1124 lines, 54+ reqs, 3 invariants).

### Batch 2 — DRAFT (v0.1) — review cycles deferred

Drafts complete; need 2 review rounds + 2 integrations each to reach `reviewed`.

- [ ] **`specs/workspace-model.md`** (WM, 606 lines).
- [ ] **`specs/process-lifecycle.md`** (PL, 489 lines).
- [ ] **`specs/operator-nfr.md`** (ON, 696 lines).
- [ ] **`specs/reconciliation/{spec, schemas}.md`** (RC, split, 929 lines).
- [ ] **`specs/beads-integration.md`** (BI, 506 lines).

### Cross-cutting cleanup (must precede batch-2 reviews OR happen alongside)

- [ ] **Cross-spec citation drift cleanup.** Multiple specs cite `[architecture.md §1.x]`, `[control-points.md §6.x]`, `[reconciliation.md §9.x]`, `[beads-integration.md §10.x]` — legacy `components.md` numbering. Real headings are §4.x. Coordinated corpus-wide pass needed.
- [ ] **`handler_type` → `agent_type` rename.** AR-027 mandates; EV §8.3.x, HC-008, WM §5.3a still use `handler_type`. Corpus-wide rename.
- [ ] **`depended-on-by` reverse index.** Removed from front matter v1.1; computation tool not built.

### Process recommendations for next session

1. User reviews 1-2 batch-1 specs to verify format/depth meets expectations.
2. If sign-off: run cross-cutting cleanup pass (single subagent), then batch-2 review cycles.
3. After all 10 specs `reviewed`: decompose-to-tasks pass.

## Spec template + tooling (complete)

- [x] Spec template at `docs/foundation/spec-template.md` v1.1 (594 lines).
- [x] Prefix registry at `specs/_registry.yaml` (10 reservations).
- [x] Foundation spec corpus copied to `docs/foundation/{problem-space.md, components.md}`.
- [x] OVERVIEW.md scannable positions doc.
- [x] core-scope.md walkthrough alignment record (10 sections).
- [x] 5 project-level docs at `docs/foundation/project-level/` (subsystem-organization, testing, quality-checks, build-practices, agent-configuration).



## Foundation kerf work — Round 2 (COMPLETE)

Round 2 landed 2026-04-24. Delta plan at `/Users/gb/.kerf/projects/gregberns-harmonik/harmonik-foundation/round2-delta-plan.md` (1,180 lines, 36 findings with proposed text verbatim). Component doc grew 924 → 1,163 lines; problem-space grew 367 → 377 lines. Phase 3 reviewer findings remain at [docs/reviews/2026-04-23-foundation-phase3/](docs/reviews/2026-04-23-foundation-phase3/).

### Cluster 1 — Architectural gaps (all applied)

- [x] **Transition record storage.** Sibling file at `.harmonik/transitions/<transition_id>.json` inside checkpoint commit tree; trailers remain as cheap index. New §2.1b. (Architect C-1, Critic C-10.)
- [x] **Pin "subsystem" term.** New §1.4a: subsystem = Go package inside the daemon process for MVH; handlers + orchestrator-agents + `br` invocations are the only out-of-process actors; reconciliation is a workflow-library entry, not a subsystem. (Architect C-2, I-2.)
- [x] **JSONL divergence-evidence carve-out.** §3.6 third bullet (c) + new §9.3a — divergence-evidence read is permitted, distinct from observational replay and forbidden state reconstruction. (Architect C-3.)
- [x] **Node-level idempotency tag.** New §2.1c: `idempotency_class ∈ {idempotent, non-idempotent, recoverable-non-idempotent}` declared on DOT node attribute; consumed by Cat 1/2 detectors. (Critic C-4.)
- [x] **Recursive reconciliation resolution.** New §9.1a: reconciliation workflows are an explicit exception to §2.1a — they emit exactly one verdict commit and no intermediate checkpoints; crash-mid-investigation means re-spawn with no Cat-of-a-Cat question (Option B per main-agent decision). (Crash S6.)
- [x] **Orphan sweep on startup.** §8.2 step 1a: tmux sessions + worktree locks + orphan subprocesses swept before reconciliation; emits `daemon_orphan_sweep_completed`. (Crash S7.)

### Cluster 2 — Investigator-agent contract (all applied)

- [x] **Mandatory wall-clock budget.** New §9.4a: reconciliation workflows MUST declare a budget; budget exhaustion produces default `escalate-to-human`; emits `reconciliation_budget_exhausted`. (Crash S3.)
- [x] **Verdict schema + one-verdict rule.** New §9.5a: six-enum closed verdict schema, one-verdict-per-workflow; malformed verdicts produce `reconciliation_verdict_malformed` + fallback `escalate-to-human`. (Crash S4.)
- [x] **Durable verdict execution record.** New §9.5b: daemon emits `reconciliation_verdict_executed` + writes second commit with `Harmonik-Verdict-Executed: true` trailer. Unexecuted verdict is discoverable as Cat 3b. (Crash S5.)
- [x] **Snapshot token.** New §9.4b: investigator inputs bound to `{git_head_hash, beads_audit_entry_id, captured_at_timestamp}`; daemon refuses to execute verdicts on stale state; emits `reconciliation_verdict_stale`. (Crash adversary.)

### Cluster 3 — Category taxonomy (all applied)

- [x] **Cat 0 "Infrastructure unavailable"** (new §9.2 bullet + §9.3 pre-check). Halts classification; daemon enters `degraded`; emits `infrastructure_unavailable`. (Crash gaps.)
- [x] **Cat 3a "Torn Beads write"** (new §9.3 detector + §10.8a `br`-adapter idempotency rule). (Crash S2.)
- [x] **Cat 3b "Verdict-unexecuted"** (new §9.3 detector + dedicated auto-resolver re-running the verdict action). (Crash S5.)
- [x] **Cat 3c "Inverse premature-close"** (new §9.3 detector: merge exists but Beads still `in_progress` — auto-verdict `accept-close-with-note`). (Crash S1.)
- [x] **Cat 6a / 6b split** (§9.2 replaces Cat 6: 6a LLM-triageable, 6b mechanically-unrecoverable → auto-escalate without investigator). (Crash gaps.)
- [x] **Structurally-broken-worktree detector.** Integrated into Cat 6a detector list (§9.3). (Crash S10.)
- [x] **Run-scoped detectors.** §9.3 scoping invariant: detectors are RUN-scoped, not bead-scoped. (Crash S8.)
- [x] **DAG-parentage-only ordering.** §9.3 ordering invariant: git DAG + UUIDv7; wall-clock is display only. (Crash S12.)
- [x] **§9.2a action-mapping layer.** New §9.2a: table mapping each Cat to default resolution action; clarifies that 6 detection categories are deliberate and the 3-action layer is explicit. (Main-agent integration of Skeptic question + Crash sub-detectors.)

### Cluster 4 — Subsystem authoring clarity (all applied)

- [x] **ControlPoint registry owner.** New §6.1b: S02 owns registry; S05 owns Hook dispatch; S01 owns Gate + Guard invocation. (Subsystem Implementer.)
- [x] **Session-log pipeline owner.** New §5.3a: S04 emits, S06 sets path + metadata, S08 ingests. (Subsystem Implementer.)
- [x] **`agent_type` stable identifier shape.** §1.6a append: URN-like string identifiers (`harmonik.agent.claude-code`, etc.). (Subsystem Implementer.)
- [x] **Goroutine ownership.** §4.3 replacement: daemon owns watcher goroutine per session; S04 owns per-agent-type adapter (non-goroutine). (Subsystem Implementer.)
- [x] **Reconciliation workflow authoring owner.** New §9.1b: S01 ships the reconciliation workflow library (DOT + policies + prompt templates). (Subsystem Implementer.)

### Cluster 5 — Operational obligations (NAMED; full catalogs deferred to spec-draft)

Component doc names each obligation; spec-draft produces the full catalog.

- [x] **Startup failure-mode catalog — named.** §8.2 + §7.1 spec-draft obligations. (Operator.)
- [x] **`harmonik upgrade` contract — named.** §7.5 + §8.3 spec-draft obligations. (Operator.)
- [x] **Silent-hang detection — named.** §4.6 + §8.5 spec-draft obligations. (Operator.)
- [x] **Multi-daemon commands — named.** §7.10 obligation line (`harmonik list`, stop-by-cwd, machine-level budget). (Operator.)
- [x] **Reconciliation operator override — named.** §9.5 append: pre-execution-pause-on-verdict operator flag + veto path. (Operator.)
- [x] **Exit-code + config inventory — named.** §7.1 + §6.8 spec-draft obligations. (Operator.)

### Cluster 6 — Acknowledgments (all applied)

- [x] **No-DTW conditions.** `01-problem-space.md` locked decision #12 replaced with explicit applicability conditions (single-machine, cheap re-execution, no irreversible external actions, bounded waits). (Skeptic.)
- [x] **Multi-tenancy deferral.** §7.10 replaced: shared LLM budgets, operator identity, skill packages named as deferred-not-dismissed. (Skeptic.)
- [x] **Centralized-controller trade-off.** §1.8 append: graceful-degradation cost acknowledged. (Skeptic.)
- [x] **DOT untyped validator.** §2.1 DOT paragraph: external validator named as obligation. (Skeptic.)
- [x] **WIP-loss mitigation on reopen-bead.** §9.4 Outputs append: investigator MUST capture recoverable WIP in reconciliation commit before `reopen-bead` verdict. (Skeptic.)
- [x] **Taxonomy audit pointer.** §9.2a note + QUESTIONS.md Q-P3-1. **Resolved by user 2026-04-24**: 6 detection categories + action-mapping layer is the shape; do not reopen. Skeptic's "3-action restructure" framing was a style debate that did not change runtime behavior and is closed.

### Execution (all complete)

- [x] **Round-2 delta plan** produced (1,180 lines, 36 findings with proposed text).
- [x] **Round-2 amendments** applied to `01-problem-space.md` and `02-components.md`.
- [ ] **Round-3 re-review (optional).** Not triggered: taxonomy was NOT restructured; amendments preserve reviewer affirmations. Re-review worth it only if the user wants a fresh crash-adversary/critic pass against the amended text before advancing to change-design — otherwise advance straight to `change-design`.

### Follow-ups surfaced by the round-2 amendments

- [ ] **Advance kerf to change-design.** `kerf status harmonik-foundation change-design` once user signs off on round-2 output. Change-design maps the foundation component set onto concrete spec-draft work.
- [ ] **Consider snapshotting amended `02-components.md` into the repo.** File now 1,163 lines and lives outside the repo at `/Users/gb/.kerf/projects/gregberns-harmonik/harmonik-foundation/`. If that directory is lost, the amendment work is gone. Options: periodic copy to `docs/kerf-snapshots/`, or delay until `kerf finalize` will copy the spec drafts into `specs/` anyway.
- [x] **Q-P3-1 (taxonomy shape) — resolved by user 2026-04-24.** 6 detection categories + §9.2a action-mapping layer is the final shape. No re-amendment pass needed. Do not reopen.

## Phase 0: Refine the Plan (current)

The knowledge base captures decisions made so far, but several decisions are still open and several subsystems are out of date with the latest framing. Goal of this phase: get to a plan firm enough that bootstrap implementation can start.

### A. User decisions on bootstrap.md (highest priority)

[docs/bootstrap.md](docs/bootstrap.md) has explicit "Decisions needed" sections. Each one is a blocker for the next phase.

- [ ] **§2 MVH cut.** Is the proposed minimum viable harmonik the right scope? Specific sub-questions: Pi handler in MVH or post-MVH? Scenario harness CI integration in MVH or later?
- [ ] **§3 Workflow lifecycle.** Where do human gates sit (plan_review? pre-merge only? per-subsystem-risk)? What is the revision_loop retry cap? Does each cycle build one subsystem or one spec slice?
- [ ] **§4 Operator controls.** Stop default (graceful vs immediate)? Queue state compatibility contract across versions? Single CLI/API/dashboard interface or differentiated by risk? Where does pause/upgrade configuration live (runtime config vs workflow definition vs operator-policy file)?
- [ ] **§5 Build order.** Is the proposed step ordering right? Specifically: orchestrator (step 5) before policy engine (step 7) -- accept the temporary policy gap, or build a stub policy in step 5? Scenario stub timing? Twin format on day 1?
- [ ] **§6 Risk specifics.** Regression baseline shape (tagged commit + event log + scenario suite output)? Sample human review cadence in Phase 2?

### B. Refresh subsystems still on pre-2026-04-19 framing

These three subsystem docs were not updated in the recent feedback pass and now lag the rest:

- [ ] **S02 Policy Engine** — Reflect: no verifier subsystem (transition guards now consume non-agentic node exit codes / structured output); Go language; alignment with twin binary selection mechanism (policy can name which binary to use per agent role).
- [ ] **S05 Hook System** — Reflect: post-verifier graph model (hooks fire on agent completion, not as a verification trigger); twin-binary handler model; Go language.
- [ ] **S09 Improvement Loop** — Reflect: operates *between tasks* at configured cadence (the engine for bootstrap §4.2 pause-for-improvement); reads JSONL event log and CASS-indexed sessions; Go language.

### C. Resolve parked architectural details

Each of these is referenced from one or more subsystem docs as "open" and needs to be settled before the relevant implementation step:

- [ ] **Node types doc.** Define: non-agentic node taxonomy (script, test, lint, build, custom), the contract for capturing process output (stdout / stderr / exit code / structured artifacts), how policy expressions read that output to select edges, the orchestrator-vs-runner split for executing them. Likely lives at `docs/concepts/node-types.md`. Referenced by S01, S04.
- [ ] **Pi session-log investigation.** Concrete artifact: a doc or comment in S04 specifying where Pi writes its session log, what format, and whether CASS understands it natively or needs a translation layer.
- [ ] **JSONL event log policy.** Rotation strategy, retention policy, sidecar index format (sqlite? duckdb? deferred?). Referenced from S03.
- [ ] **Scenario definition format.** Pick: YAML, Go-as-code, or hybrid. Decision blocks S07 implementation.
- [ ] **Workspace conflict resolution role.** Pick: dedicated merge-agent type, original implementer responsibility, or always escalate. Referenced from S06.
- [ ] **Twin conformance plan.** How do we keep twins honest against real-agent drift? Probably needs a "conformance suite" concept. Referenced from digital-twins concept and S07.

### D. Knowledge-base hygiene

- [ ] **Log entry for the 2026-04-19 feedback pass.** A `docs/log/2026-04-19-subsystem-feedback-pass.md` summarizing what changed and why. Future agents need this to understand the decision history.
- [ ] **Revisit ideas catalog (I01-I07).** Some ideas may now be subsumed by decisions; others may still be live. Mark any that are now obsolete.
- [ ] **Methodology check.** The methodology says agents should add log entries and update INDEX files when work lands. We've done indexes; the log step has been skipped consistently. Consider whether the methodology should require log entries on substantive doc changes.

---

## Phase 1: Bootstrap Implementation (after Phase 0 lands)

Do not start until Phase 0 decisions are made. The build order proposed in [docs/bootstrap.md §5](docs/bootstrap.md) is a starting point, not yet a commitment.

Indicative tasks (will be expanded once Phase 0 resolves):

- [ ] Repo skeleton: `go.mod`, package structure, CI configuration, formatting/lint setup, documented per the [docs/01_architecture.md](docs/01_architecture.md) "scalable to 500K LOC" requirement.
- [ ] Step 1: Workspace Manager (S06) — worktree create/cleanup + adze invocation.
- [ ] Step 2: Event Bus (S03) — in-process pub/sub + JSONL persistence.
- [ ] Step 3: Agent Runner (S04) — NTM Go wrapper + Claude Code handler + twin binary support.
- [ ] Step 4: `claude-twin` binary — even if minimal at first, must exist by the time S07 lands.
- [ ] Step 5: Orchestrator Core (S01) — static graph + checkpoint + edge selection.
- [ ] Step 6: Hook System (S05) — Claude Code hooks for completion + state-transition triggers.
- [ ] Step 7: Policy Engine (S02) — YAML policies + transition guards + freedom profiles.
- [ ] Step 8: Scenario Harness (S07) — minimal scenario runner + first end-to-end scenario.
- [ ] Step 9: Memory Layer (S08) — CASS pointed at the canonical session-log directory.
- [ ] Step 10: Pi handler + `pi-twin` binary.

After step 8: MVH exists. After step 9: ready for Phase 2 self-build with reasonable observability.

---

## Phase 2: Self-Build (after MVH)

Operate harmonik against itself. Tasks will be defined by the subsystems' own backlogs and the improvement loop's findings; no static list belongs here yet.

Cross-cutting concerns to keep visible during Phase 2:

- [ ] Regression baseline: every release of harmonik passes the scenario suite that the prior release passed.
- [ ] Improvement-loop cadence configured (initially conservative, e.g., every 5-10 tasks).
- [ ] Sample human reviews on a defined cadence to catch quiet quality drift.

---

## Backlog (Unscheduled)

Things worth doing but not yet prioritized:

- [ ] Concept doc: error classification taxonomy (transient / structural / deterministic — Kilroy uses this; we adopt it informally; worth its own doc).
- [ ] Concept doc: budget enforcement (token + wall-clock budgets per task / per workflow / per cycle).
- [ ] Concept doc: agent inter-communication patterns (with agent-mail dropped, what's the canonical "agent A asks agent B a question" mechanism — orchestrator-mediated transition? direct via hooks?).
- [ ] Component digest update: NTM and CASS docs should reflect what we now know we'll use them for (and what we won't).

### Doc corrections surfaced during 2026-04-19 overnight recon

- [ ] **Correct Attractor framing** in `docs/subsystems/orchestrator-core.md` line 52. Current text calls Attractor "spec for distributed workflow coordination; likely covers patterns we need around durable execution and replay." Attractor is actually a DOT-based pipeline runner with JSON-snapshot durability and single-threaded traversal — same family as Kilroy, not DTW. See `.kerf/recon/attractor-findings.md` for details. (Related: QUESTIONS.md Q-R1.)
- [ ] **Update Kilroy concept digest** in `docs/concepts/kilroy.md`. Current digest says 3 failure classes and 4 fidelity modes; Kilroy actually has 6 failure classes (transient_infra, budget_exhausted, compilation_loop, deterministic, canceled, structural) and 6 fidelity modes (full, truncate, compact, summary:low/medium/high). Also note: `stack.manager_loop` is stubbed to FAIL in v1 per spec-compliance-audit. (Related: QUESTIONS.md Q-R3.)
- [ ] **Clarify in orchestrator-core.md** that Kilroy is fast-forward-only for fan-in, which means harmonik's merge-based convergence (Gas Town pattern) is a genuine divergence from Kilroy, not a parameter tweak.
- [ ] **Log entry for overnight 2026-04-19 run.** Create `docs/log/2026-04-19-overnight-recon-and-foundation-start.md` summarizing the recon pass, problem-space draft, review process, and any revisions.

### Beads integration (surfaced 2026-04-21)

- [ ] **Add Beads component doc.** New file at `docs/components/external/beads.md` summarizing `Dicklesworthstone/beads_rust` — what it is, schema, CLI surface, how harmonik integrates. Link from `docs/components/INDEX.md`.
- [ ] **Author the Beads-CLI skill.** An agent-facing skill document explaining how to use `br` in harmonik workflows: common commands, output formats, state-transition conventions (claim, close, reopen; coarse status only), how bead IDs relate to harmonik run IDs. Delivered via handler-contract skill injection per the 2026-04-21 decision.
- [ ] **Scenario-test suite: crash recovery with Beads+harmonik.** Per user 2026-04-21 request. A named set of S07 scenarios that execute multi-step workflows, terminate the system at critical junctions (between claim and first commit; mid-workflow; post-commit-pre-bead-close; during bead close), then restart and assert correct recovery. Should cover: no duplicate bead claims; no lost work; no false-completes; operator-decision path for mid-run interrupts.
- [ ] **Foundation-amendment: add handler-contract "skill injection" obligation.** Per 2026-04-21 decision. The handler-contract spec (Component 4) currently doesn't require handlers to ensure agents have workflow-required skills/tools. Amend via the foundation amendment protocol so the obligation is explicit; applies to Beads-CLI skill and any future skill/tool requirement.
- [ ] **Foundation-amendment: add `Harmonik-Bead-ID` trailer to checkpoint commit messages.** Per 2026-04-21 decision. execution-model.md checkpoint schema needs to include `Harmonik-Bead-ID: <id>` when the run is tied to a bead. Small addition to Component 2's checkpoint contract.
