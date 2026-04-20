# Harmonik Project Status

> Snapshot of where the harmonik knowledge base stands. Updated when significant work lands.
>
> Last updated: 2026-04-19 (overnight autonomous run; see `OVERNIGHT_RUN_2026-04-19.md` for the narrative)

## What Harmonik Is

A composable agentic orchestration system. Core principle: **deterministic skeleton, probabilistic organs**. See [AGENT_INDEX.md](AGENT_INDEX.md) for the full map.

## Current Phase

**Spec-first foundation work.** No code has been written. Kerf initialized with `spec` as the default jig. The first kerf work, `harmonik-foundation`, has advanced through problem-space (converged round 3), decompose (converged round 2), and research (complete — 7 parallel sub-agents, one per component). Remaining passes for the foundation work: change-design, spec-draft, integration, tasks.

**Work location.** Kerf work artifacts live at `/Users/gb/.kerf/projects/gregberns-harmonik/harmonik-foundation/` (outside the repo until `kerf finalize`). The structure:
- `01-problem-space.md` — converged at round 3
- `02-components.md` — converged at round 2; 7 components (architecture, execution-model, event-model, handler-contract, workspace-model, control-points, operator-nfr)
- `03-research/{component}/findings.md` — 7 per-component research outputs
- `03-research/SYNTHESIS.md` — cross-component synthesis (~20 recommended positions, ~37 user-decision items, 6 tensions resolved)
- `reviewers/PERSONAS.md` — reusable reviewer persona prompts
- `reviews/01-problem-space/` and `reviews/02-components/` — all reviewer outputs

**Recon findings** (inputs to the foundation work) live at `.kerf/recon/` inside the repo (gitignored):
- `kilroy-findings.md`, `attractor-findings.md`, `subsystem-audit.md`, `nfr-inventory.md`

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

1. Read [OVERNIGHT_RUN_2026-04-19.md](OVERNIGHT_RUN_2026-04-19.md) for what happened overnight.
2. Review [QUESTIONS.md](QUESTIONS.md) — ~37 user-decision items awaiting confirmation or redirection. Two (Q-F1, Q-R4) are provisionally resolved by the orchestrator; confirm or overrule.
3. Decide whether to advance the foundation work to change-design (next kerf pass) or redirect based on research findings. The research findings in `/Users/gb/.kerf/projects/gregberns-harmonik/harmonik-foundation/03-research/` are the substantive input.
4. Consult [TASKS.md](TASKS.md) for the broader backlog.
