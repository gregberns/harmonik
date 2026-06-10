# Harmonik Project Status

> **[HANDOFF.md](HANDOFF.md) is the per-session authoritative source for current state and next steps.** This file is a higher-level structural summary. Sections below labelled "*(historical)*" are preserved for reference.
>
> Last updated: 2026-06-10 — **Captain & Crew system fully landed** (15/15 tasks, `57c6fd94`). Persistent daemon model operational. Session-keeper mechanism complete; testing is the priority lane. Named-queues parked (superseded). Validation-net CORE landed. See [docs/INITIATIVES.md](docs/INITIATIVES.md) for the live initiatives board.
>
> Previously: 2026-05-06 — **Phase 0 closed.** 11 reviewed specs (~562 req IDs); 905 live beads in `<repo>/.beads/` with 3,589 edges, zero cycles; 376 beads carry `scope:bootstrap` (348 spec-corpus + 28 meta-epic); discipline at v0.12 (12 versions, 16 class-lane findings absorbed). Readiness gaps closed in beads: build/test scaffolding (`hk-pvcs`), twin-binary scaffolding (`hk-ahvq.48`), operational skills (`hk-jhob`), Phase-1 validation (`hk-kle6`), no-op PolicyEngine (`hk-b3f.89`). Parked-state lifecycle withdrawn per user 2026-05-05; loaded beads transition directly to dispatchable. Phase-1 starting point: `hk-pvcs` 8-bead local-scaffolding epic.

## What Harmonik Is

A composable agentic orchestration system. Core principle: **deterministic skeleton, probabilistic organs**. See [AGENT_INDEX.md](AGENT_INDEX.md) for the full map.

## Current Phase (2026-06-10)

**Phase 1 operational.** Harmonik runs Claude end-to-end on a bead with zero human input (smoke v13 GREEN, 2026-05-14). The daemon runs as a persistent background process; agents dispatch work via `harmonik queue submit`. Review-loop is on by default.

### Active work lanes

| Initiative | Epic | Status |
|---|---|---|
| Auto test/CI restoration | `hk-kjkbw` | 🟡 near-done (~26/27 beads closed) |
| Session-keeper testing | `hk-ekap1` | 🎯 priority — mechanism done, test plan ready |
| Spec-drift cleanup | ~17 beads | 🟡 ideal churn batch (`kind:spec-drift`) |
| Codex enablement | `hk-w4tmz` + `hk-n5lfz` | ⛔ blocked — `-a never` flag-fix + operator OAuth login |
| Release pipeline | `hk-brc3z` | ⛔ blocked — GitHub `workflow` permission (tabled ~days) |

See [docs/INITIATIVES.md](docs/INITIATIVES.md) for the full board with done/total counts.

### Recently completed

| Feature | When | Notes |
|---|---|---|
| Captain & Crew system | 2026-06-09 | 15/15 tasks landed (`57c6fd94`); crew runs via `claude --remote-control` |
| Validation-net CORE | 2026-06-09 | Scenario/test infra (8cfc75f8); VN4 revert-demo infeasible (filed `hk-d5twq`) |
| Daemon/infra reliability | 2026-06 | tapCh race fixed; spawn-semaphore wedge mitigated; auto-revive supervisor live |
| Codex harness code | 2026-06-09 | 10/20 tasks landed via bypass-SOP; T9+T11 banked on worktree branches |
| Productization P0 gate | 2026-06-03 | Integration-branch enforcement (`hk-6r6xv`); harmonik can target non-main |
| harmonik comms bus | 2026-06-01 | `harmonik comms send/recv/who/log`; file-outbox convention retired |
| Persistent daemon model | 2026-05-30 | `harmonik --project ... --no-auto-pull --max-concurrent N` + supervisor auto-revive |
| Phase 1 operational | 2026-05-14 | End-to-end zero-human bead dispatch (smoke v13 GREEN) |

### Named queues — parked

`hk-tigaf` (named-queues, 12-bead epic) is **parked / superseded** as of 2026-06-10. The multi-queue generalization use case was the flywheel investigate-handoff pattern; the current single-queue + crew-per-epic model satisfies this without a new subsystem.

*(Historical spec-corpus detail from Phase 0 / 2026-04-27 removed; see git history. All 10 spec IDs are FROZEN.)*

## Decisions in force

### 10 locked decisions (2026-04-19)

(Unchanged — see prior STATUS.md versions in git history.)

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
- `--no-auto-pull` is the safe default (prevents auto-billing API pool; hk-8vy18 will flip to opt-in).
- Supervisor (`harmonik supervise start`) auto-revives on crash; restart-backoff = 30s–1min after rapid kills.
- Queue-only: agents submit via `harmonik queue submit`; daemon dispatches, merges to main one-at-a-time, closes beads.
- Review-loop on by default (`--workflow-mode review-loop`).
- Work-project deployment: use `--target-branch`, `--protect-branch`, `--forbid-default-main` flags (or `.harmonik/branching.yaml`).

See CLAUDE.md §"Daily loop" for the full operating manual.

## Spec corpus inventory — current state (2026-04-25)

| Spec | File(s) | Version | Status | §4 req IDs |
|---|---|---|---|---|
| architecture | `architecture.md` | 0.3.1 | reviewed | 53 |
| execution-model | `execution-model.md` | **0.3.3** | reviewed | 65 (+EM-005a) |
| event-model | `event-model.md` | **0.3.3** | reviewed | 48 |
| handler-contract | `handler-contract.md` | **0.3.3** | reviewed | 63 (+HC-016a, HC-026b) |
| control-points | `control-points.md` | 0.3.2 | reviewed | 55 |
| workspace-model | `workspace-model.md` | **0.4.2** | reviewed | 53 |
| process-lifecycle | `process-lifecycle.md` | **0.4.1** | reviewed | 42 |
| operator-nfr | `operator-nfr.md` | **0.4.1** | reviewed | 61 |
| reconciliation | `reconciliation/{spec,schemas}.md` | 0.4.0 | reviewed/supplement | 43 |
| beads-integration | `beads-integration.md` | **0.4.1** | reviewed | 43 |

**~526 unique requirement IDs** across the corpus (sum of column 5 ≈ 526). EV's 7 new event-type identifiers (§8.x.NN) are not requirement IDs and don't count toward this number.

**ALL spec IDs (AR, EM, EV, HC, CP, WM, PL, ON, RC, BI) ARE PERMANENTLY FROZEN.** No renumbering or ID reuse in any future revision. Today's net-new IDs (EM-005a, HC-016a, HC-026b) were minted in pre-existing gaps.

## Where to start next session

1. Read [HANDOFF.md](HANDOFF.md) — most-recent `/session-handoff` output; authoritative for current state and blockers.
2. Read [docs/INITIATIVES.md](docs/INITIATIVES.md) — live initiatives board: done/total per epic, blocked items, loose bead clusters.
3. Read [docs/orchestrator-rules.md](docs/orchestrator-rules.md) — permanent dispatch/priority directives.
4. Run `harmonik queue status` to confirm the daemon is alive (exit 17 = not running; start it per CLAUDE.md §"Start the daemon once").
5. Run `kerf next` to get the prioritized dispatch feed for the session.
