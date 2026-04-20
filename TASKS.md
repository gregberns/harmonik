# Harmonik Tasks

> Actionable work items. Grouped by phase. Check off as resolved or move to a log entry.
>
> Last updated: 2026-04-19

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
