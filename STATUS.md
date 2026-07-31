# Harmonik Project Status

> **[HANDOFF.md](HANDOFF.md) is the per-session authoritative source for current state and next steps.** This file is a higher-level structural summary. Sections below labelled "*(historical)*" are preserved for reference.
>
> **2026-07-28 (current program):** The active program is **delete-and-rewrite** — see
> **[`plans/2026-07-27-delete-and-rewrite/CHARTER.md`](plans/2026-07-27-delete-and-rewrite/CHARTER.md)**
> for why it exists, the phase sequence, the decided core subsystem set, and what "done" means. Phase 1
> (deletion) is COMPLETE: ~225,000 lines of tests and dead code removed, zero production behavior changed.
> Phase 2 (config-driven subsystem partition) has its first switch landed. Phase 3 rewrites
> `internal/daemon/workloop.go` + `internal/daemon/dot_cascade_core.go` as one unit.
> **Corrected 2026-07-30:** this line named `reviewloop.go` as a third file. That file no longer
> exists. Commit `3cec5afd7` (2026-07-28) retired review-loop mode and deleted its driver, so Phase 3
> is a two-file unit. `runWorkLoop` also moved out of `workloop.go` into `internal/daemon/scheduler.go`
> in commit `756b6604c`. The **daemon is intentionally DOWN** (no `harmonik --project` process on this
> machine, checked 2026-07-30). Work lands single-writer and human-reviewed on branch
> `phase1-session-restart-substrate`.
> **Corrected 2026-07-30:** this line called that branch unpushed. It is pushed —
> `refs/remotes/origin/phase1-session-restart-substrate` exists and the local branch is 10 commits
> ahead of it. The per-session authority is a `HANDOFF-*.md` file. That file is gitignored and
> machine-local, so it is absent from a fresh clone and from every worktree.
>
> **SUPERSEDED 2026-07-27:** the P2 god-package extraction (`plans/2026-07-21-p2-extraction/`) and the
> code-health audit (`plans/2026-07-24-code-health-audit/`). Do NOT resume their task cards. Their
> measurements survive where cited in the charter's sibling documents; their lane models and task
> indexes do not. The 2026-06-20 note below is historical.
>
> Last updated: 2026-06-20 — **Nine initiatives landed in a single-day burst** (keeper redesign, captain economy, doc/instruction audit, easy-start launchers, tmux session organization, RC session prefix, fleet sleep/wake Phase 0, remote-node telemetry, remote-substrate e2e proof). The project is now entering a **live-validation / testing phase**: most of the burst shipped as code but has not been exercised live. See "Recently completed (2026-06-20 burst)" below and [.harmonik/context/captain-lanes.md](.harmonik/context/captain-lanes.md) for the live initiatives board.
>
> Previously: 2026-06-10 — **Captain & Crew system fully landed** (15/15 tasks, `57c6fd94`). Persistent daemon model operational. Session-keeper mechanism complete. Named-queues parked (superseded). Validation-net CORE landed.
>
> Previously: 2026-05-06 — **Phase 0 closed.** 11 reviewed specs (~562 req IDs); 905 live beads in `<repo>/.beads/` with 3,589 edges, zero cycles; 376 beads carry `scope:bootstrap` (348 spec-corpus + 28 meta-epic); discipline at v0.12 (12 versions, 16 class-lane findings absorbed). Readiness gaps closed in beads: build/test scaffolding (`hk-pvcs`), twin-binary scaffolding (`hk-ahvq.48`), operational skills (`hk-jhob`), Phase-1 validation (`hk-kle6`), no-op PolicyEngine (`hk-b3f.89`). Parked-state lifecycle withdrawn per user 2026-05-05; loaded beads transition directly to dispatchable. Phase-1 starting point: `hk-pvcs` 8-bead local-scaffolding epic.

## What Harmonik Is

A composable agentic orchestration system. Core principle: **deterministic skeleton, probabilistic organs**. See [AGENT_INDEX.md](AGENT_INDEX.md) for the full map.

## Current Phase (2026-06-20) — *(historical)*

> **Superseded 2026-07-30.** The current phase is the delete-and-rewrite program in the block at the
> top of this file. Read that block and the charter, not this section. The two sections use the same
> word "Phase 1" for different things: here it means the old operational milestone, and in the charter
> it means the deletion phase. The corrections below are kept because this section was still being
> read as live.

**Phase 1 operational, entering live-validation.** Harmonik runs Claude end-to-end on a bead with zero human input (smoke v13 GREEN, 2026-05-14). The daemon runs as a persistent background process; agents dispatch work via `harmonik queue submit`.

**Corrected 2026-07-30:** this paragraph said "Review-loop is on by default." Review-loop is retired. `internal/core/workflowmode.go` `WorkflowModeRetiredReviewLoop` makes the value decode for history and fail validation, and `harmonik run --workflow-mode review-loop` is rejected with a pointer to `dot`. The default mode is `builtin`.

### Active work / priority lane — *(historical)*

**Live validation of the 2026-06-20 burst.** The nine initiatives below shipped as code, but most were never exercised live — testing the system means turning them on.

**Corrected 2026-07-30:** this paragraph named the remote-substrate end-to-end run as the critical-path blocker, on bead `hk-538l`. That bead is CLOSED. It closed on 2026-06-21 as proven fixed by a DOT end-to-end run on gb-mbp. Nothing here blocks anything today.

For live lanes (lane → crew → queue → epic), see [`.harmonik/context/captain-lanes.md`](.harmonik/context/captain-lanes.md). **Treat that file as stale:** its own current-truth block is dated 2026-07-22 and describes a dispatching seven-session fleet. The delete-and-rewrite program started 2026-07-27 and the daemon is down.

### Recently completed (2026-06-20 burst)

Nine initiatives landed in ~18 hours (135 commits). One plain-English line each; full reconciliation in [plans/2026-06-20-state-reassessment-and-doc-sync/README.md](plans/2026-06-20-state-reassessment-and-doc-sync/README.md).

- **Keeper redesign** — per-project config, zero hardcoded thresholds, actionable-warn self-restart handshake, hold/release co-working override, configurable hard-ceiling backstop, durable tmux↔session-id identity (epic `hk-gffc` + keeper config work).
- **Captain economy** — slimmed captain boot (~81k→~55-60k tokens), Sonnet ops-monitor offload, comms `--wake` fix, per-crew `--model` (epic `hk-unjy`).
- **Doc & instruction audit** — three-kinds tracking model (docs / behavioral-contracts=skills incl. the new `orchestrator-rules` skill / operational-state tiers), AGENTS.md→router, new `harmonik sync-assets` + supervisor skew-notify (epic `hk-vk7b`).
- **Easy-start launchers** — native Go `harmonik start captain|crew <name>`; bash `captain-launch.sh` retired; `captain respawn` self-heal; shared `agentlaunch` helper (`codename:easy-start`).
- **Tmux session organization** — unified `harmonik-<hash>-*` namespace, agent+keeper window-nesting, window-granular restart, `supervise reap` (epic `hk-0v9e`).
- **Remote-control session prefix** — per-project RC session-label prefix (`hk-captain`); core landed (epic `hk-dhe6`). **Corrected 2026-07-30:** this line said 4 tail beads were open. Epic `hk-dhe6` is CLOSED, since 2026-07-04.
- **Fleet sleep/wake (fleet-state Phase 0)** — sleep markers with source+level, fail-closed IsSleeping, live wake-pane resolution, boot reconcile of orphaned markers, ctx-watchdog skips parked sessions (`codename:fleet-state`).
- **Remote-node telemetry** — worker-report resource snapshots + problem flags (P1) and live resource-breach detection (P2); off-by-default, never live-run (`worker-report` / `worker-breach`).
- **Remote-substrate e2e** — SSHRunner quote fix, substrate-runner threading, agent_ready TCP loopback, SSH-direct branch fetch; proof committed on gb-mbp but not yet live-validated under the daemon (`hk-620j` / `hk-7bwx` / `hk-538l`).

For the running progress log / earlier milestones, see [ROADMAP.md](ROADMAP.md).

### Named queues — parked

`hk-tigaf` (named-queues, 12-bead epic) was **parked / superseded** as of 2026-06-10. The multi-queue generalization use case was the flywheel investigate-handoff pattern. The single-queue + crew-per-epic model satisfies this without a new subsystem. **Corrected 2026-07-30:** the epic is not parked in the ledger. It is CLOSED, since 2026-06-13.

*(Historical spec-corpus detail from Phase 0 / 2026-04-27 removed; see git history. All 10 spec IDs are FROZEN.)*

## Decisions in force

### 10 locked decisions (2026-04-19)

(Otherwise unchanged. One decision has been narrowed since. It is recorded below rather than left to git archaeology, because the narrowing is the kind of thing an agent needs at boot and will not find by reading a diff.)

**Where the ten decisions actually are — corrected 2026-07-30.** This stub used to say only "see prior
STATUS.md versions in git history". That pointer resolves, but only after a reader walks about
seventeen revisions of this file and guesses which one holds the text, and the section is titled
"Decisions Locked In (2026-04-19)" there, not "10 locked decisions". Run this instead:

```bash
git show 4a384fcf1:STATUS.md
```

The ten numbered decisions are under the heading "## Decisions Locked In (2026-04-19)". Commit
`4a384fcf1` added them and commit `b82b2affb` replaced them with this stub, so `4a384fcf1` is the
last revision that carries the full text.

**#4 (agent runner / tmux inspectability) — NARROWED 2026-07-28 by operator decision.** The decision reads *"Agent runner (S04): NTM-wrapped Go. Inspectability via tmux is a requirement, not a preference."* The operator reopened it and narrowed it:

> *"If we get more flexibility from removing that, then do so. Seems like another 'crossed wires' issue where the underlying reasoning was lost. Maybe it was from before when not run as a daemon. Doesn't matter — but we should probably be able to run it any way."*

Read the scope precisely, because the obvious misreading is wider than the decision:

- **The daemon must be able to run WITHOUT tmux. tmux is NOT being removed.** It stays the default host and stays the way an operator inspects live agents. What went is the hard fail-fast on a missing `$TMUX` that made tmux the only possibility.
- **A capability genuinely unavailable without tmux is announced loudly at boot and degraded honestly** — never faked, and never deferred to a crash on first dispatch. Silent degradation remains forbidden.
- The full statement of the operator's reasoning is in [`plans/2026-07-27-delete-and-rewrite/CHARTER.md`](plans/2026-07-27-delete-and-rewrite/CHARTER.md) §3.

Landed in code by `1f8781730` (confirmed an ancestor of HEAD on 2026-07-30). The specs were amended to match on 2026-07-28 — `specs/process-lifecycle.md` (PL-021a, PL-021b items 2 and 3, PL-028, PL-028b), `specs/workspace-model.md` (WM-002a), `specs/execution-model.md` (EM-015d-RIA), `specs/cognition-loop.md` (CL-081). All five requirement IDs are still present in those files. **Corrected 2026-07-30:** this line also pinned four spec version numbers. Three have moved on since the amendment, so the numbers are dropped and the requirement IDs are the handle instead.

### 4 candidate decisions (2026-04-20/21)

11. DOT workflow definition format. 12. No DTW. 13. Beads as task ledger (`br` CLI only). 14. Handler-contract skill injection.

### Decisions locked in this session's flow (from prior sessions)

- Direct-to-main + agent-reviewer-every-commit + no-PRs (decision from phase-1).
- AGENTS.md canonical with CLAUDE.md symlink.
- CONSTITUTION.md as non-recursive trust anchor.
- JSON-structured agent-reviewer verdict.
- Aggressive coverage targets (95% core / 90% floor / <0.3% regression gate).
- `depguard` v2 alone (no `go-arch-lint`).
- Three-tier `make check-fast` / `check` / `check-full`.
- Spec-template structure locked.

### Daemon model: persistent background process (as of 2026-05-30)

*(The 2026-05-08 "daemonization deferred" decision is superseded. The daemon is deployed.)*

**The daemon runs detached in a tmux session.** Launch with:
```bash
tmux new-session -d -s harmonik-daemon \
  'harmonik --project /Users/gb/github/harmonik --no-auto-pull --max-concurrent N'
```

Key properties:
- Single daemon per project (pidfile lock; exit 5 on collision).
- `--no-auto-pull` is now a no-op alias kept for back-compat. Queue-only is the default. **Corrected 2026-07-30:** this line called the flag "the safe default" and said bead `hk-8vy18` would flip it to opt-in. `cmd/harmonik/usage.go` documents the flag as a no-op alias, and `hk-8vy18` is not in the bead ledger.
- Supervisor (`harmonik supervise start`) auto-revives on crash; restart-backoff = 30s–1min after rapid kills.
- Queue-only: agents submit via `harmonik queue submit`; daemon dispatches, merges to main one-at-a-time, closes beads.
- **Corrected 2026-07-30:** this list said "Review-loop on by default (`--workflow-mode review-loop`)". Review-loop is retired and its driver is deleted. The default mode is `builtin`. Valid values are `builtin`, `single`, and `dot`.
- Work-project deployment: use `--target-branch`, `--protect-branch`, `--forbid-default-main` flags (or `.harmonik/branching.yaml`).

**Corrected 2026-07-30:** this section used to point at CLAUDE.md §"Daily loop" for the full operating manual. CLAUDE.md has no such section. It is a router now. The daily loop lives in the `harmonik-dispatch` skill (`.claude/skills/harmonik-dispatch/SKILL.md`), and the full design is in [docs/orchestration-protocol-v2.md](docs/orchestration-protocol-v2.md).

## Spec corpus inventory — the ten foundation specs (re-measured 2026-07-30)

**Corrected 2026-07-30.** Every version number and every requirement-ID count in this table was stale.
The old table was dated 2026-04-25 and had not been re-measured since. The values below were read
from the `version:` front-matter of each file and counted by matching each spec's own ID prefix.

| Spec | File(s) | Version | Status | Req IDs |
|---|---|---|---|---|
| architecture | `architecture.md` | 0.3.2 | reviewed | 54 |
| execution-model | `execution-model.md` | 0.10.0 | reviewed | 100 |
| event-model | `event-model.md` | 0.7.2 | reviewed | 64 |
| handler-contract | `handler-contract.md` | 0.8.1 | reviewed | 94 |
| control-points | `control-points.md` | 0.4.3 | reviewed | 63 |
| workspace-model | `workspace-model.md` | 0.4.8 | reviewed | 59 |
| process-lifecycle | `process-lifecycle.md` | 0.7.0 | reviewed | 60 |
| operator-nfr | `operator-nfr.md` | 0.5.5 | reviewed | 89 |
| reconciliation | `reconciliation/{spec,schemas}.md` | 0.4.7 / 0.4.0 | reviewed/supplement | 43 |
| beads-integration | `beads-integration.md` | 0.9.1 | reviewed | 53 |

**679 unique requirement IDs** across these ten specs. The old table claimed ~526. EV's event-type
identifiers (§8.x.NN) are not requirement IDs and do not count toward this number.

**These ten are no longer the whole of `specs/`.** The directory now holds 34 spec files at its top
level and 44 markdown files in total. `plans/2026-07-27-delete-and-rewrite/NEXT_STEPS.md` counts 1,180
unique requirement IDs across all of `specs/` and reports that 25% of them appear in no Go file. Read
that document before you treat any requirement as an oracle.

**ALL spec IDs (AR, EM, EV, HC, CP, WM, PL, ON, RC, BI) ARE PERMANENTLY FROZEN.** No renumbering or ID reuse in any future revision. Today's net-new IDs (EM-005a, HC-016a, HC-026b) were minted in pre-existing gaps.

## Where to start next session

1. Read in order: [PRINCIPLES.md](PRINCIPLES.md) → [`plans/2026-07-27-delete-and-rewrite/CHARTER.md`](plans/2026-07-27-delete-and-rewrite/CHARTER.md) → [AGENT_INDEX.md](AGENT_INDEX.md) → [STATUS.md](STATUS.md) → `HANDOFF.md`. **Corrected 2026-07-30:** this step used to put `.harmonik/context/captain-lanes.md` in everyone's reading order. That file is captain-tier. Its own header says a captain loads it at STARTUP Step 0b and crews and implementers do not. Its content is also stale, dated 2026-07-22. `HANDOFF.md` is the most recent `/session-handoff` output. It is gitignored and machine-local, so it is absent from a fresh clone.
2. Read the `orchestrator-rules` skill (`.claude/skills/orchestrator-rules/SKILL.md`) — permanent dispatch/priority directives.
3. Run `harmonik queue status` to confirm the daemon is alive. Exit 17 means the daemon socket is absent. **Corrected 2026-07-30:** this step used to point at CLAUDE.md §"Start the daemon once" for the start command. CLAUDE.md has no such section. Use the `harmonik-lifecycle` skill. Note that the daemon is intentionally DOWN for the delete-and-rewrite program, so exit 17 is the expected result today.
4. Run `kerf next` to get the prioritized dispatch feed for the session.
