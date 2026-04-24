# Harmonik Project Status

> Snapshot of where the harmonik knowledge base stands. Updated when significant work lands.
>
> Last updated: 2026-04-24 (overnight) — **spec corpus complete**: 10 normative specs at `specs/`, 5 batch-1 reviewed (v0.3), 5 batch-2 draft (v0.1). Spec-template v1.1 + ~30 reviewer artifacts captured.
>
> Session handoff: see `SESSION_HANDOFF.md` for what the next session should pick up.

## What Harmonik Is

A composable agentic orchestration system. Core principle: **deterministic skeleton, probabilistic organs**. See [AGENT_INDEX.md](AGENT_INDEX.md) for the full map.

## Current Phase

**Spec corpus authored** (2026-04-24 overnight). Ten normative specs at `specs/` — five at status `reviewed` (architecture, execution-model, event-model, handler-contract, control-points), five at status `draft` (workspace-model, process-lifecycle, operator-nfr, reconciliation, beads-integration). No code has been written. Kerf use was paused per user direction ("disregard kerf for now"). Phase 0 (foundation alignment) complete; Phase 0b (spec corpus) in progress; Phase 1 (bootstrap implementation) blocked on batch-2 reviews + cross-cutting cleanup + user sign-off.

**Foundation expanded 7 → 10 components on 2026-04-23** to absorb 2026-04-20/21 decisions. Added: `process-lifecycle`, `reconciliation`, `beads-integration` as standalone foundation specs.

**Round-2 amendments landed 2026-04-24.** 36 findings applied across Clusters 1–6 from the Phase 3 review. Component doc grew 924 → 1,163 lines (+239); problem-space grew 367 → 377 lines (+10). New subsections added: §1.4a (subsystem pin), §1.6a/§1.6b (agent-type + verification naming), §2.1b (transition-record sibling file), §2.1c (idempotency tag), §3.1a (Go repr), §3.2a (taxonomy acceptance), §3.6 (c) divergence-evidence bullet, §5.3a (session-log pipeline owner), §6.1b (ControlPoint registry owner), §9.1a (reconciliation cadence exception), §9.1b (workflow-library owner), §9.2a (action-mapping layer), §9.3a (JSONL divergence scope), §9.4a (wall-clock budget), §9.4b (snapshot token), §9.5a (verdict schema), §9.5b (verdict execution), §10.8a (br-adapter idempotency). New taxonomy: Cat 0, 3a, 3b, 3c, 6a, 6b. New event types: 7 added to §3.2.

**Work location.** Kerf work artifacts live at `/Users/gb/.kerf/projects/gregberns-harmonik/harmonik-foundation/` (outside the repo until `kerf finalize`). The structure:
- `01-problem-space.md` — 377 lines; locked decision #12 (No DTW) expanded with explicit applicability conditions on 2026-04-24.
- `02-components.md` — 1,163 lines across 10 components (architecture, execution-model, event-model, handler-contract, workspace-model, control-points, operator-nfr, process-lifecycle, reconciliation, beads-integration). All round-2 structural amendments applied.
- `round2-delta-plan.md` — the 1,180-line per-finding delta plan that drove the amendments (36 findings with proposed text, application order, final verification checklist).
- `03-research/{component}/findings.md` — 7 per-component research outputs (pre-amendment).
- `03-research/SYNTHESIS.md` — cross-component synthesis (~20 recommended positions, ~37 user-decision items, 6 tensions resolved).
- `reviewers/PERSONAS.md` — reusable reviewer persona prompts.
- `reviews/01-problem-space/` and `reviews/02-components/` — early reviewer outputs (pre-amendment).

**Phase 3 review (6 personas, pre-round-2-amendments)** lives in `docs/reviews/2026-04-23-foundation-phase3/` in the repo (git-tracked):
- `SYNTHESIS.md` — 6-cluster finding synthesis + proposed round-2 amendment plan.
- `architect.md`, `critic.md`, `operator.md`, `skeptic.md`, `subsystem-implementer.md`, `crash-adversary.md` — per-persona reports.

**Recon findings** (inputs to the foundation work) live at `.kerf/recon/` inside the repo (gitignored):
- `kilroy-findings.md`, `attractor-findings.md`, `subsystem-audit.md`, `nfr-inventory.md`, `beads-findings.md`.

## What's In the Knowledge Base

| Area | Status | Notes |
|---|---|---|
| Problems (P01-P07) | Stable | Catalogued; not revisited this session |
| Goals (G01-G07) | Active | G06 Bootstrapping and G07 Testability added 2026-04-19 |
| Concepts | Active | Digital Twins added 2026-04-19; others stable |
| Components (internal/external) | Stable | Not revisited this session |
| Subsystems (S01-S09) | **Active** | S01, S03, S04, S06, S07, S08 substantially updated 2026-04-19; S07 Verifier archived and replaced with Scenario Harness; S02, S05, S09 still hold pre-2026-04-19 framing and need a refresh pass |
| Ideas (I01-I07) | Untouched this session | Worth revisiting once subsystem decisions firm up |
| Log | Underused | No new log entries from this session yet |

## Decisions Added 2026-04-20 and 2026-04-21

These are in addition to the 10 original locked-in decisions below. Treated as candidate positions in the design (not permanent commitments) but load-bearing for ongoing spec work.

11. **Workflow definition format: DOT** (2026-04-20). Declarative graph, Graphviz-renderable, NL→DOT ingestor path available. Policies reference YAML policy documents by name; policies themselves remain YAML.
12. **DTW is NOT adopted** (2026-04-20). Harmonik uses JSONL events + git checkpoints + SQLite queue + deterministic restart reconciliation. Git history is source of truth for completion; queue is a cache. Temporal / Restate / DBOS are conceptual references only.
13. **Beads (`Dicklesworthstone/beads_rust`) is the task ledger** (2026-04-21). SQLite-backed; harmonik is the workflow engine layered on top. Beads holds bead data + typed dependency edges + coarse status; harmonik holds fine-grained workflow state in its event log. Interaction via `br` CLI only (NOT Beads's MCP server). Agents get a Beads-CLI skill via handler-contract skill injection.
14. **Handler contract must support skill injection** (2026-04-21). Handlers are responsible for ensuring the agent process has access to skills/tools the assigned workflow node requires. Beads-CLI is the first instance; applies generally. Pending as a foundation amendment.

## Decisions Locked In (2026-04-19)

These are firm; reopening one needs strong new evidence.

1. **Implementation language: Go.** Rationale in [docs/01_architecture.md](docs/01_architecture.md).
2. **Orchestrator (S01):** Go-native, using Kilroy and Attractor as design references.
3. **Event bus (S03):** in-process pub/sub + JSONL on disk. JSONL is the source of truth; bus is notification.
4. **Agent runner (S04):** NTM-wrapped Go. Inspectability via tmux is a requirement, not a preference.
5. **Initial agent handlers:** Claude Code + Pi ([badlogic/pi-mono](https://github.com/badlogic/pi-mono)).
6. **Digital twins are separate binaries**, not in-process mocks. Selection happens at workflow/policy config layer; runner has zero test-mode branches. See [docs/concepts/digital-twins.md](docs/concepts/digital-twins.md).
7. **Workspace (S06):** worktree-per-workflow-branch + merges (Gas Town pattern). Multiple agents within a single workflow share its workspace sequentially. No agent-mail file reservations.
8. **Memory (S08) MVH:** just CASS pointed at the canonical session-log directory. Three-layer cognition deferred until concrete demand exists.
9. **No verifier subsystem.** Verification is a node type in the workflow graph (mechanical → non-agentic node, semantic → agentic node). Old S07 Verifier Layer archived; S07 slot now holds Scenario Harness.
10. **Operator controls operate between tasks**, not within tasks. The orchestrator processes a stream of tasks; pause means "finish in-flight, stop pulling new." Three controls: stop on major issue, pause for improvement cycle, pause to upgrade harmonik version. These are general harmonik features, not bootstrap-specific.

## Substantive Doc Awaiting Review

[docs/bootstrap.md](docs/bootstrap.md) is the thinking-through plan for G06. It has explicit "Decisions needed" sections at the end of each part. These are open and waiting for the user's call. The most important decisions:

- §2 MVH cut: is the proposed minimum viable harmonik the right scope?
- §3 Workflow lifecycle: human gate placement, revision retry caps, cycle granularity
- §4 Operator control specifics: stop default, queue compat contract, control interface, config scope
- §5 Bootstrap build order: especially "orchestrator before policy" question

## Open Architectural Questions (Parked Details)

These were noted during decisions but pushed to "later, before implementation begins":

- **Node types doc.** With verifier collapsed into the graph, non-agentic node types (test runs, lints, scripts) need a defined contract: how stdout/stderr/exit-codes are captured, how policy expressions consume that output to choose edges, how runner vs orchestrator divide responsibility for execution. Referenced from S01 and S04.
- **Pi session-log format and location.** Needs concrete investigation before the Pi handler can support memory ingestion.
- **JSONL rotation, retention, indexing.** Event log volume will be substantial; need a policy before storage costs surprise us.
- **Scenario definition format.** YAML vs Go vs DSL. Decision needed before scenario harness implementation begins.
- **Workspace conflict resolution role.** Which agent type resolves merge conflicts -- a dedicated "merge agent" or the original implementer or always-escalate?
- **Twin conformance.** How do we keep twin behavior honest against real-agent drift?
- **Queue state format compatibility window.** What's the contract harmonik versions must honor across upgrades?
- **Configuration scope.** Runtime config vs workflow definition vs operator-policy file -- where do cadence/upgrade rules live?

## What Hasn't Been Touched Yet (and Likely Should Be Soon)

- **S02 Policy Engine** — Holds pre-2026-04-19 framing. Should be refreshed to reflect the no-verifier decision (transition guards now include "did the non-agentic node exit 0?" semantics) and the Go language decision.
- **S05 Hook System** — Same situation. Needs to align with the post-verifier graph model and the twin-binary mechanism.
- **S09 Improvement Loop** — Needs to align with the corrected operator-control model (it's the engine for §4.2 pause-for-improvement, runs between tasks at configured cadence).
- **Ideas catalog (I01-I07)** — Worth revisiting; some may be subsumed by recent decisions, others may still be live.
- **Log entries** — This session produced significant decisions but no log entry. Consider a `docs/log/2026-04-19-subsystem-feedback-pass.md` summarizing the change set.
- **No code.** No `go.mod`, no source tree, no scenario stub. The bootstrap-build-order question (§5 of bootstrap.md) needs to resolve before code starts.

## Where to Start Next Session

1. Read [SESSION_HANDOFF.md](SESSION_HANDOFF.md) — rewritten 2026-04-24 (overnight) with full state of spec corpus.
2. Skim 2-3 batch-1 specs at `specs/` to verify the format matches expectations.
3. Decide: (a) sign off on batch-1 → proceed to batch-2 review cycles; (b) flag any spec for revision before proceeding; (c) cross-cutting cleanup pass (citation drift + handler_type rename) first.
4. Consult [TASKS.md](TASKS.md) for the broader backlog.

## Spec corpus inventory

Five reviewed specs (v0.3, status `reviewed`, IDs frozen):
- `specs/architecture.md` (AR, 643 lines, 52 reqs)
- `specs/execution-model.md` (EM, 1091 lines, 65 reqs)
- `specs/event-model.md` (EV, 1032 lines, 70 events + 42+ reqs)
- `specs/handler-contract.md` (HC, 949 lines, 57+ reqs)
- `specs/control-points.md` (CP, 1124 lines, 54+ reqs)

Five draft specs (v0.1, status `draft`, IDs mutable):
- `specs/workspace-model.md` (WM, 606 lines, 40 reqs)
- `specs/process-lifecycle.md` (PL, 489 lines, 28 reqs)
- `specs/operator-nfr.md` (ON, 696 lines, 46 reqs)
- `specs/reconciliation/{spec.md, schemas.md}` (RC, split, 929 lines, 31 reqs)
- `specs/beads-integration.md` (BI, 506 lines, 32 reqs)

Total: ~8,065 lines of normative spec across 10 specs. Plus `specs/_registry.yaml` (prefix reservations) + `docs/foundation/spec-template.md` v1.1 (594 lines).
