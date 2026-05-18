# Harmonik Project Status

> **Phase 0: CLOSED 2026-05-06.** See [`docs/foundation/phase-0-milestone-close.md`](docs/foundation/phase-0-milestone-close.md) for the milestone-close record. Phase 1 (implementation) is active; agents claim work via `br ready -l scope:bootstrap`. **[HANDOFF.md](HANDOFF.md) remains the per-session authoritative source for current state and next steps.** This file is a higher-level structural summary; it may lag the handoff. Sections below labelled "*(historical)*" are preserved for reference.
>
> Last updated: 2026-05-12 — **Workflow-modes corpus shipped** (epic `hk-7om2q` + 32 child beads closed; daemon work-loop dispatch driver landed in `internal/daemon/reviewloop.go` with full §8.1a event coverage, exactly-once `review_loop_cycle_complete` across all 5 termination paths, and `run_id` propagation assertions). Workflow-modes is the central integration node that ties the spec corpus to a running daemon for the trivial-slice happy-path. See HANDOFF.md for v33 detail.
>
> Previously: 2026-05-06 — **Phase 0 closed.** 11 reviewed specs (~562 req IDs); 905 live beads in `<repo>/.beads/` with 3,589 edges, zero cycles; 376 beads carry `scope:bootstrap` (348 spec-corpus + 28 meta-epic); discipline at v0.12 (12 versions, 16 class-lane findings absorbed). Readiness gaps closed in beads: build/test scaffolding (`hk-pvcs`), twin-binary scaffolding (`hk-ahvq.48`), operational skills (`hk-jhob`), Phase-1 validation (`hk-kle6`), no-op PolicyEngine (`hk-b3f.89`). Parked-state lifecycle withdrawn per user 2026-05-05; loaded beads transition directly to dispatchable. Phase-1 starting point: `hk-pvcs` 8-bead local-scaffolding epic.

## What Harmonik Is

A composable agentic orchestration system. Core principle: **deterministic skeleton, probabilistic organs**. See [AGENT_INDEX.md](AGENT_INDEX.md) for the full map.

## Current Phase

**Decompose-to-tasks: BI loaded; 9 specs remaining; review protocol gates each.** End of 2026-04-27 session:
- BI's 66 beads loaded into `<repo>/.beads/` under prefix `hk`, epic `hk-872`. All status `draft`. 110 intra-spec `blocks` edges. `br dep cycles` clean.
- `docs/decompose-to-tasks/discipline.md` at **v0.4** (was v0.3). 6 deltas: F11 step→umbrella implicit (parent-child encodes the dep, not explicit `blocks`); F12 sensor↔impl one-way; F13 bidirectional inline cite disambiguation; F14 mnemonic vs Beads-assigned IDs (with zsh implementation pattern); F15 default priority `P2` accepted; F16 corpus single-DB at prefix `hk`.
- `docs/decompose-to-tasks/bi-pilot.md` at **v0.1.3** (was v0.1.2). 5 deltas: §7 tally arithmetic fixed (40 first-class req beads, total 66 — not the prior 36/61); removed bidirectional `bi-004 ↔ bi-027`; removed wrong-direction `bi-011 → bi-inv-001` and `bi-022 → bi-inv-003`; removed redundant step→umbrella edges; added `bi-schema.harmonik-write-status` per BI v0.4.1 §6.1 split.
- `docs/decompose-to-tasks/bi-smoke-load-findings.md` (new) — full report of the first smoke load against live `br` and the 11 findings that produced the v0.4 + v0.1.3 patches.
- `docs/decompose-to-tasks/pilot-review-protocol.md` at **v0.1** (new) — the 3-reviewer parallel pass (Coverage / Decomposition-quality / Reference) that gates every pilot before it loads into Beads. Plus synthesis rules (BLOCKER / MAJOR / MINOR) and a load gate.
- `.beads/` added to `.gitignore` at the phase-1 operational milestone (regenerable from JSONL).

**Spec corpus + v0.4.x coordination wave: COMPLETE.** All 10 normative specs `reviewed`. Six specs patched in the prior session (2026-04-25) with the cross-spec coordination items that the prior session's R2 integrations filed:

- architecture (v0.3.1), control-points (v0.3.2), reconciliation (v0.4.0) — unchanged this session.
- execution-model (v0.3.2 → **v0.3.3**) — Outcome `kind` discriminator + 2 RC-owned trailers.
- event-model (v0.3.2 → **v0.3.3**) — 7 new event types (§8.6.11–14, §8.7.16–17, §8.8.5), monotonic-companion fields, daemon_degraded enum exhaustive, `divergence_kind` post-phase-1 extension note.
- handler-contract (v0.3.2 → **v0.3.3**) — HC-016a orphan-reconnect retry + HC-026b drain-forced silent-hang acceptance.
- workspace-model (v0.4.1 → **v0.4.2**) — WM-036 `no-op-accept` row.
- process-lifecycle (v0.4.0 → **v0.4.1**) — 9 items including `daemon_instance_id` UUIDv7, marker-read step 8a, monotonic fields, `get-agent-count` RPC, `br` + reconciliation-locks orphan sweep.
- operator-nfr (v0.4.0 → **v0.4.1**) — OQ-RC-009 resolution acknowledgment (decline normative `quarantined` state in phase-1 scope).

**~526 unique requirement IDs** counted across the 10 specs (NEXT_AGENT.md's "~250" estimate was 2× low).

**Decompose-to-tasks: BI smoke-loaded; review protocol drafted.** Updated artifacts at `docs/decompose-to-tasks/`:
- `discipline.md` v0.4 (~410 lines) — 11 decomposition rules + 16 cumulative findings (F1–F16) + tag-mapping + cross-spec edge convention + corpus prefix `hk` decision (§2.12).
- `bi-pilot.md` v0.1.3 — 43 BI requirements → 66 beads (40 first-class req + 11 step + 4 sensor + 8 schema + 1 taxonomy + 1 test-infra + 1 spec-parent). Revised full-corpus estimate: **~795 beads** (range 700–950).
- `bi-smoke-load-findings.md` v0.1 — full report of the 2026-04-27 smoke load.
- `pilot-review-protocol.md` v0.1 — 3-reviewer parallel pass + load gate.

**`br` (Beads CLI v0.1.45) is INSTALLED** at `/Users/gb/.local/bin/br`. Verified by smoke load. Capabilities used so far: `br init --prefix <X>`, `br create --silent`, `br update --status draft`, `br dep add <citing> <prerequisite> -t blocks`, `br dep cycles`, `br ready`, `br list -l <label>`, `br epic status`, `br show`. Surprise discovery: IDs are auto-assigned in hierarchical-alphanumeric form (`hk-85z`, `hk-85z.37.4`) — discipline §2.10 + §2.12 documents the mnem→assigned-ID translation rule.

**No kerf use in this session.** User paused kerf: "disregard kerf for now; come back when something's working."

## What changed this session (2026-04-25)

**v0.4.x cross-spec coordination patch wave landed in 4 parallel patch agents.** Six specs patched per the request list accumulated in prior session's R2 integrations. Each patch was authored as a pre-baked plan (~150-line prompt per agent). Highlights:

- **PL v0.4.1 (9 items)** — PL-INTERIM markers dropped on codes 22/23 (ON v0.4.0 absorbed both); `.harmonik/daemon.upgrading` promoted to normative; `daemon_instance_id` (UUIDv7) minted at step 0; pidfile gains line 3; PL-009/PL-011a payloads gain `_at_ns_since_boot`; new step 8a reads `daemon.upgrading` + `daemon.state` markers (gates step 9 transition target); `get-agent-count` RPC added to PL-003a inventory; PL-006 orphan sweep extended to `br` subprocesses + stale `.harmonik/reconciliation-locks/*.lock`. PL-008a adds code 14 (`upgrade-hash-mismatch`).
- **EV v0.3.3 (5 items)** — 7 new event types (§8.6.11 dispatch_dedup, §8.6.12 detector_panic, §8.6.13 verdict_execution_retry, §8.6.14 bead_terminal_transition_recovered (post-phase-1), §8.7.16 operator_command_failed, §8.7.17 operator_escalation_cleared, §8.8.5 redaction_failed); `daemon_shutdown` durability F confirmed (resolves OQ-PL-012); `ready_at_ns_since_boot` + `shutdown_at_ns_since_boot` payload fields; `daemon_degraded` reason enum promoted exhaustive (6 values, including `cat0_post_ready`); `divergence_kind` post-phase-1 extension note.
- **EM v0.3.3 (2 items)** — new EM-005a + Outcome `kind` discriminator + `OutcomeKind` enum (resolves OQ-RC-010); trailer registry gains `Harmonik-Workflow-Class` + `Harmonik-Target-Run-ID` (resolves OQ-RC-002).
- **WM v0.4.2 (1 item)** — WM-036 verdict-disposition table seventh row for `no-op-accept` (resolves OQ-RC-011).
- **HC v0.3.3 (2 items)** — HC-016a orphan-reconnect-window retry rule (companion to PL-003b/PL-009b); HC-026b drain-forced silent-hang ON-classification acceptance.
- **ON v0.4.1 (1 item)** — OQ-RC-009 resolution acknowledgment: decline to add normative `quarantined` daemon-status in phase-1 scope (rationale: quarantine is the operator-escalation outcome per RC's `escalate-to-human` mechanical action).

Net new IDs added: PL (none — extensions only), EV (none — new event-type identifiers in §8 numbering), EM (EM-005a), WM (none), HC (HC-016a, HC-026b), ON (none).

**Decompose-to-tasks pilot.** First two artifacts at `docs/decompose-to-tasks/`:
- `discipline.md` v0.1 — 10 decomposition rules covering: granularity (one req → one bead default), multi-step protocols (umbrella + step beads), coalescible clusters, test-vs-impl combine-by-default, sensor/invariant per `XX-INV-NNN`, schema/data-shape per `RECORD`/`ENUM`, cross-spec edge derivation (mechanical from cite list), `Tags:` and `Axes:` mapping, status/priority assignment (load `parked`, no priority), parent-child grouping (per-spec parent + req children).
- `bi-pilot.md` — 43 BI requirements → 61 beads. Identifies 6 rough edges; surfaces 10 OQ-DTT-NNN open questions.
- Multiplier observation: ~1.42× requirement→total bead ratio in BI; full-corpus estimate ~790 beads (with PL/RC/CP being likely heavier-multipliers).

## What changed in prior session (2026-04-24 day-and-evening)

**Corpus citation cleanup — DONE.** Two coordinated passes migrated legacy cross-spec anchors:
- Pass 1: 57 `architecture.md §1.N → §4.N` cites + 7 `handler_type → agent_type` renames.
- Pass 2: ~145 cites across 7 specs (EV `§3.N`, WM `§5.N`, ON `§7.N`, PL `§8.N`, BI `§10.N`, CP misnumbered `§6.N`, reconciliation multi-file path form).
- Each batch-2 R1 integration also cleaned its own outbound cites as part of B3 blocking findings.
- Total ~350+ cites migrated corpus-wide. Known remaining: reviewer artifacts (informative docs), revision-history prose quoting legacy form (intentional).

**Workspace-model → reviewed v0.4.1.** Full 2-round cycle:
- R1 reviewers (implementer / cross-spec-architect / critic) → R1 integration v0.3.0 (+391 lines, 9 new requirements, 1 retired, 7 OQs, §8 error taxonomy).
- R2 reviewers (skeptic / crash-adversary / git-expert) → R2 integration v0.4.0 (+242 lines, 5 new requirements, 7 new OQs, new error classes).
- Citation cleanup patch to v0.4.1.
- WM IDs now FROZEN permanently.

**R2 review findings were NOT polish.** Real bugs caught:
- `git worktree add <path> <branch>` invocation would FAIL at runtime for new branches — corrected to `-b <branch> <path> <start-point>`.
- WM-022 cited `Harmonik-Actor-Role` git trailer that doesn't exist in EM — replaced with sidecar walk.
- Silent unreachable states after crash during worktree creation (`bare-worktree-no-lease`) — now classified as Cat 3.
- Sidecar atomicity missing temp+rename+fsync — matched to lock file discipline.
- `.harmonik/lease.lock` would get committed without gitignore hygiene — new WM-013e.

**Process-lifecycle → R1-integrated v0.3.0.** +298 lines, 11 new requirements, 2 new invariants, 6 new OQs. Added: fd-lifetime advisory lock, JSON-RPC 2.0 wire format, project-hash provenance marker, startup step 0 (bus + registries + JSONL writer), exit-code catalog, runner entry-point. Dropped `operator-nfr` from depends-on (cycle break).

**PL R2 reviews** (skeptic, crash-adversary, daemon-author) completed after R1 integration. Surface substantial blockers: PL-008a exit-code conflict with ON §8 authoritative table, `PL-027(iii)` socket-rebind self-contradicts MUST-NOT-unlink, `PL-014a` concurrency ceiling unbounded (macOS EMFILE footgun), ready protocol missing for external callers, stop-advancing predicate unnamed mechanically. **Must-fix before `reviewed`.**

**Operator-nfr → R1-integrated v0.3.0.** +177 lines. Applied: `in_flight(run)` mechanical definition, pause-state FSM consistency, event-name collapse (`operator_pause_status`), exit-code taxonomy expansion, §A.4 reverse-drift migration map, resource budget tables, RTO target 30s/300s, `spec-category: foundation-cross-cutting`.

**Reconciliation → R1-integrated v0.3.0** (both `spec.md` 752→880 and `schemas.md` 178→221 lines). Applied: ~52 citation fixes, reclaimed `Harmonik-Verdict-Executed` trailer ownership to RC/schemas, priority-ordered category first-match rule, concurrent-reconciliation workflow lock, evidence-corroboration rule per EV-023a, 3 invariants retired, 2 rewritten as cross-subsystem.

**Beads-integration — all 3 R1 reviews done; R1 integration PENDING.** Reviews surfaced: status-mapping hole (RunState → Beads doesn't cover `deferred`/`tombstone`), `br` CLI contract missing (exit codes, stderr format, JSON-vs-text), adapter idempotency rests on unstated Beads behavior, ~19 broken cross-references, all 4 invariants missing sensors.

**Review artifact inventory** — all 15 R1 files + 6 R2 files for batch-2 at `/Users/gb/github/harmonik/docs/reviews/2026-04-24-*-r{1,2}/`. Combined with batch-1 artifacts from the prior session, ~45 reviewer outputs now captured on disk.

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

### Phase-1 scope: daemonization deferred (2026-05-08)

**Phase 1 shipped as a foreground binary.** `harmonik run <workflow>` is a real binary you run in a terminal. Its lifecycle = the shell session. It logs to stdout, holds state in memory while alive, owns a real in-process event bus, runs workflow goroutines, exits when the workflow completes (or on SIGINT/SIGTERM). The "centralized controller" architectural thesis is unchanged; we are deferring **daemonization**, not the architecture.

**What's deferred (the daemonized version):**
- Detached / backgrounded execution (fork-and-detach; pidfile)
- Listening socket + JSON-RPC operator-control surface (queue pause/stop *while running*)
- Long-lived coordinator that survives between invocations
- Restart-recovery semantics that assume a daemon process to recover into
- The full process-lifecycle (PL) spec startup sequence, marker-files, fd-passing on exec-upgrade, etc.

**Phase-1 operator control (without daemonization):**
- SIGINT / SIGTERM = stop (graceful shutdown handler)
- SIGSTOP / SIGCONT = pause (kernel-level)
- Workflow completion = clean exit
- No socket-RPC needed in phase-1

**Concurrency-readiness is non-negotiable.** Concurrent workflow runs is the **first post-phase-1 unlock**. All baseline code MUST be:
- `run_id`-keyed (no globals representing "the current run")
- free of shared mutable state across runs
- safe for per-invocation reconciliation (no in-memory cache assumptions that depend on a long-lived process)
- locks/leases scoped to `run_id`, not process

The foreground-process shape supports concurrent runs naturally — the binary just needs to manage multiple `run_id`-keyed workflow goroutines in its goroutine pool. That's the post-phase-1 expansion, not a re-architecture.

**Beads parked behind daemonization (do not dispatch until daemonization is in scope; some may need a process-lifecycle spec amendment when reopened):**
- `hk-b3f.107` — daemon-initiated context-restore initiation-source enforcement (EM-046)
- `hk-b3f.108` — daemon Outcome synthesis for context-restore (EM-046)
- Cycle-counter git-history adapter (restart-recovery — rendered moot in phase-1 because there is no detached daemon to restart into)
- Operator pause/stop RPCs (PL/ON cross-spec — replaced in phase-1 by signal handling)
- Pidfile + socket + JSON-RPC startup sequence (PL §3a, §8a)
- The full process-lifecycle spec startup sequence

**What is NOT deferred:** EM-016 git-plumbing (`write-tree → commit-tree → update-ref`), checkpoint-write functions, event-bus in-process implementation, workflow runner, reconciliation logic. All of these are foreground-process baseline code, not daemon-only code.

**Decision rationale:** Daemonization adds substantial complexity (process-lifecycle spec ownership, IPC, operator-control RPC, startup/restart-recovery semantics) without being required for the phase-1 operational milestone. Bootstrap-subset is single-workflow trivial-slice. User wants minimum path to a running system in a terminal, then add concurrent runs as the first post-phase-1 unlock, then add daemonization later when there is a real reason to detach (long-lived multi-tenant service shape).

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

1. Read [HANDOFF.md](HANDOFF.md) — skill-formatted handoff produced by `/session-handoff` 2026-04-27.
2. Read [SESSION_HANDOFF.md](SESSION_HANDOFF.md) — prose form of the same.
3. Read [NEXT_AGENT.md](NEXT_AGENT.md) — ordered instructions for the next session.
4. Read `docs/decompose-to-tasks/discipline.md` v0.4 (especially the new §2.10 mnem→ID rule and §2.12 corpus prefix `hk` decision).
5. Read `docs/decompose-to-tasks/pilot-review-protocol.md` v0.1 — the 3-reviewer protocol that gates every pilot.
6. Priority: **draft AR pilot** (`docs/decompose-to-tasks/ar-pilot.md`) against `specs/architecture.md`, then run the 3-reviewer protocol on it, then load. AR is small (52 reqs, mostly declarations) — good shakedown for the protocol before scaling.
