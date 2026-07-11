<!-- DRAFT — proposed replacement for /STATUS.md (repo root)
     (startup-doc revamp companion, per 02-cutover-and-open-questions.md §2.2[B] + Step 0.3 +
     00-SYNTHESIS.md §6 "STATUS.md decisions-in-force + frozen-spec-ID rule | MOVE →
     project.yaml / AGENT_INDEX; rest of STATUS.md CUT (stale, self-contradicting) — retire or
     gut to pointers"). Lands WITH the amended AGENTS.md at Step 4.1.

     What changed and why:
     - The live file's "Current Phase" / "Recently completed" / "Where to start next session"
       sections are frozen at 2026-06-20 and self-contradicting with the live lane tracker
       (00-SYNTHESIS.md §"Entry docs": "STATUS.md frozen at 2026-06-20, telling booters to start
       work (hk-538l) that closed three weeks ago"). Manifest agents don't read this file at
       boot anyway (`harmonik agent brief` is the boot context; live phase/lane state belongs in
       `.harmonik/context/project.yaml` and `.harmonik/context/captain-lanes.md`, which are
       actually kept current). GUT those sections to pointers rather than re-dating them here —
       re-dating a doc nobody boot-reads just recreates the next staleness cycle.
     - **MUST-PRESERVE (explicit requirement on this bead):** the `#decisions-locked-in-2026-04-19`
       anchor + the ten locked decisions. AGENTS.md links to this exact anchor
       (`STATUS.md#decisions-locked-in-2026-04-19`) as the authoritative "don't reopen without
       strong new evidence" pointer. The live file had SILENTLY BROKEN this — the heading was
       rewritten to "## Decisions in force" / "### 10 locked decisions (2026-04-19)" (a
       different anchor) and the actual ten-item text was replaced with "(Unchanged — see prior
       STATUS.md versions in git history.)", i.e. the referenced anchor did not exist and the
       content it pointed to required a git-archaeology detour. Restored below verbatim from the
       last commit that had it (`4a384fcf1`, "Lock in Phase 0 architectural decisions"), heading
       unchanged so the anchor regenerates identically.
     - Frozen spec-ID inventory KEPT (small, load-bearing, does not go stale the way phase prose
       does — "FROZEN" IDs are permanent by definition).
     This DRAFT comment is removed on deploy. -->

# Harmonik Project Status

> **Structural summary, not a boot doc.** Manifest agents (captain, crew, admiral, watch) get
> live phase/lane state from `harmonik agent brief` — this file is not in that boot path. Beads
> boot from `agent-task.md`, not this file. Come here for the durable architectural record: the
> ten locked decisions and the frozen spec-ID inventory. For live phase/lane state, see
> [`.harmonik/context/project.yaml`](.harmonik/context/project.yaml) and
> [`.harmonik/context/captain-lanes.md`](.harmonik/context/captain-lanes.md).

## What Harmonik Is

A composable agentic orchestration system. Core principle: **deterministic skeleton, probabilistic organs**. See [AGENT_INDEX.md](AGENT_INDEX.md) for the full map.

## Current phase & live lanes

Tracked in `.harmonik/context/project.yaml` (phase, locked decisions, guardrails) and
`.harmonik/context/captain-lanes.md` (lanes, epics-in-progress, parked, dated operator
directives) — both updated far more often than this file and both surfaced through
`harmonik agent brief`. For the running progress log / milestone history, see
[ROADMAP.md](ROADMAP.md).

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

### 4 candidate decisions (2026-04-20/21)

11. DOT workflow definition format. 12. No DTW. 13. Beads as task ledger (`br` CLI only). 14. Handler-contract skill injection.

### Decisions locked in later sessions

- Direct-to-main + agent-reviewer-every-commit + no-PRs (decision from phase-1).
- AGENTS.md canonical with CLAUDE.md symlink.
- CONSTITUTION.md as non-recursive trust anchor.
- JSON-structured agent-reviewer verdict.
- Aggressive coverage targets (95% core / 90% floor / <0.3% regression gate).
- `depguard` v2 alone (no `go-arch-lint`).
- Three-tier `make check-fast` / `check` / `check-full`.
- Spec-template structure locked.
- Daemon model: persistent background process, as of 2026-05-30 (supersedes the 2026-05-08 "daemonization deferred" decision). See CLAUDE.md §"Daily loop" / `.claude/skills/harmonik-dispatch` for the operating manual; `docs/orchestration-protocol-v2.md` for the design.
- Named queues (`hk-tigaf`) parked/superseded as of 2026-06-10 — single-queue + crew-per-epic model satisfies the use case without a new subsystem.

## Spec corpus inventory — current state

| Spec | File(s) | Version | Status | §4 req IDs |
|---|---|---|---|---|
| architecture | `architecture.md` | 0.3.1 | reviewed | 53 |
| execution-model | `execution-model.md` | 0.3.3 | reviewed | 65 (+EM-005a) |
| event-model | `event-model.md` | 0.3.3 | reviewed | 48 |
| handler-contract | `handler-contract.md` | 0.3.3 | reviewed | 63 (+HC-016a, HC-026b) |
| control-points | `control-points.md` | 0.3.2 | reviewed | 55 |
| workspace-model | `workspace-model.md` | 0.4.2 | reviewed | 53 |
| process-lifecycle | `process-lifecycle.md` | 0.4.1 | reviewed | 42 |
| operator-nfr | `operator-nfr.md` | 0.4.1 | reviewed | 61 |
| reconciliation | `reconciliation/{spec,schemas}.md` | 0.4.0 | reviewed/supplement | 43 |
| beads-integration | `beads-integration.md` | 0.4.1 | reviewed | 43 |

**~526 unique requirement IDs** across the corpus (sum of column 5 ≈ 526). EV's 7 new event-type identifiers (§8.x.NN) are not requirement IDs and don't count toward this number.

**ALL spec IDs (AR, EM, EV, HC, CP, WM, PL, ON, RC, BI) ARE PERMANENTLY FROZEN.** No renumbering or ID reuse in any future revision.
