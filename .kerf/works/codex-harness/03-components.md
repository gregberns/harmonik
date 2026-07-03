# 03 — Decomposition: components for codex-harness

> Plan-jig Pass 3. The change broken into components with concrete, testable requirements, clean
> boundaries, and an explicit dependency DAG. Traceable to goals in `01-problem-space.md` (G1–G6)
> and the code map in `02-analysis.md` (areas A–J).

## Component overview (DAG)

```
C1 Harness seam (interface + AgentTypeCodex + registry)
      │
      ├──────────────► C2 Codex adapter (exec launch, capture-id, spawn-per-turn re-task)
      │                       │
      │                       └──► C3 Codex auth/billing guard (env strip + forced_login + assert)
      │
      └──────────────► C4 Selection & config surface (default + per-bead/per-queue override; DOT attr)
                              │
                              └──► C5 Workflow/review-loop integration (cascade routing, reviewer harness)
                                          │
                                          └──► C6 Migration, back-compat & test harness (N-1 safety, codex twin)
```

DAG order: **C1 → {C2, C4}; C2 → C3; C4 → C5; {C3,C5} → C6.** No cycles.

---

## C1 — Harness seam (the interface)
**Goal trace:** G1 (extract implicit contract), G3 (smallest seam). **Code areas:** A, G.

**Responsibility:** Define a Go `Harness` interface that captures exactly the harness-VARYING
surface (per `04-research/current-harness/findings.md` Part E), and an `AgentType`-keyed registry
entry for codex. Refactor the existing claude launch path to implement this interface **without
behavior change** (rename/relocate, not rewrite).

**Requirements (testable):**
- R1.1 A `Harness` interface exists exposing the methods that vary: `LaunchSpec(ctx) → (argv, env,
  cwd, error)`, `Seed(session, taskPath) error`, `Retask(session, feedback) error`, `Teardown(
  session) error`, `DetectReady(events) bool`, a session-id policy hook `SessionIDPolicy() {Minted |
  Captured}`, **and a `Completion() CompletionMode {EventStreamThenQuit | ProcessExit}` method** so
  the shared workloop branches deterministically on how the run signals "done" (claude:
  EventStreamThenQuit via `/quit`; codex: ProcessExit). This makes completion-signal source a
  first-class harness property rather than leaking codex exit-coded specifics into shared
  `sess.Wait` consumers. **[review B2]**
- R1.2 `core.AgentTypeCodex` is added; `AdapterRegistry.ForAgent(AgentTypeCodex)` resolves the codex
  adapter (`adapterregistry_hc012.go`).
- R1.3 The existing claude path is reimplemented as `ClaudeHarness` satisfying the interface, and a
  full daemon smoke on a real bead still passes (no behavior delta vs pre-change). The refactor MUST
  leave **shared scaffolding** (worktree-trust seeding, `agent-task.md` write, pre-exec messages)
  OUTSIDE the per-harness `LaunchSpec` so codex does not re-implement or skip it; only genuinely
  harness-specific bits move behind the interface. **[review M2]**
- R1.4 The seam plugs into the existing `deps.launchSpecBuilder` function field
  (`dot_cascade.go:521-523`) — no parallel dispatch path is introduced.

**Interfaces exposed:** the `Harness` Go interface; `core.AgentType` enum value `Codex`.
**Depends on:** nothing (foundational).

---

## C2 — Codex adapter (launch, capture-id, spawn-per-turn re-task)
**Goal trace:** G2 (codex surface), G3. **Code areas:** A, C, D, F.

**Responsibility:** Implement `Harness` for codex using `codex exec --json`. Because codex `exec`
runs-to-exit per turn and cannot pre-mint a session id, the adapter is **spawn-per-turn** with a
**captured** thread_id, not spawn-once-and-inject.

**Requirements (testable):**
- R2.1 `LaunchSpec` builds `codex exec --json --sandbox workspace-write -a never -C <worktree>` with
  the task delivered via stdin or a prompt arg (no TUI splash/paste). (`04-research/codex-cli` §1,4.)
- R2.2 The adapter parses stdout JSONL, captures `thread_id` from the first `thread.started` event,
  and records it as the run's harness session id (compensating for no caller-minted id).
  (`codex-cli` §3.)
- R2.3 `Retask` (review-loop iter ≥2) spawns `codex exec resume <thread_id> "<feedback>"` — a fresh
  process, NOT a paste into a live session. (`codex-cli` GAPS table.)
- R2.4 Completion is detected via process exit + the terminal `turn.completed`/`turn.failed` JSON
  event; `Teardown` is a no-op (exec self-terminates), and the claude `/quit`/splash/dual-Enter/
  post-quit-kill machinery is NOT invoked for codex. (`codex-cli` §5.)
- R2.5 The adapter guarantees the commit carries the `Refs:<beadID>` trailer — by instructing codex
  in the seed prompt AND verifying post-exit. **If codex edited the worktree but did not commit with
  the trailer, the adapter performs a deterministic commit-after-exit** (stage the worktree diff,
  commit with the `Refs:<beadID>` trailer) rather than only re-prompting — because codex's commit is
  a non-deterministic model decision with a `git add -A` footgun (#8548). If codex made no edits at
  all, the noChange path fires exactly as for claude (shared detection in area F, unchanged).
  **[review M4]** (`codex-cli` §6.)
- R2.6 `DetectReady` for codex keys on the `thread.started`/`turn.started` JSON event (codex's
  equivalent of `agent_ready`), not on claude's NDJSON `agent_ready`.
- R2.7 **Heartbeat/liveness:** the codex adapter MUST satisfy the shared staleness watchdogs
  (`launchHeartbeatTimeout` 180s, `heartbeatStalenessThreshold` 8m), which consume
  `agent_ready`/`agent_heartbeat` emitted today by a **timer loop inside the claude handler**
  (`RunHeartbeatLoop`, CHB-019, `claudehandler_chb006_024.go:588-617`) — NOT git-derived. The codex
  adapter EITHER (a) runs its own heartbeat emitter mapped from codex `item.*`/`turn.*` JSONL
  progress events, OR (b) via `Completion()=ProcessExit` declares no-liveness-polling so the
  stale-kill path (`pasteInjectQuitOnCommit`'s kill-on-stale) is **bypassed** for codex and only the
  absolute `commitHardCeiling` (90m) and process-exit govern the run. The change-spec picks one;
  (b) is simpler and matches codex's exit-on-completion shape. **[review B1 — was the biggest
  unmodeled risk]**

**Interfaces consumed:** C1 `Harness`; shared substrate (D), worktree (E), commit-detection (F).
**Depends on:** C1.

---

## C3 — Codex auth/billing guard
**Goal trace:** G4 (auth/billing, anti-credit-burn). **Code areas:** B.

**Responsibility:** Force codex onto the **ChatGPT subscription** billing path and fail-closed
against silent API-credit-pool billing — mirroring the `ANTHROPIC_API_KEY` env guard.

**Requirements (testable):**
- R3.1 The codex adapter's `LaunchSpec.env` **STRIPS `OPENAI_API_KEY` and `CODEX_API_KEY`** and
  emits empty overrides (mirror of `claudehandler_chb006_024.go:196-204,292-296`). Unit test asserts
  neither key reaches the child env even if present in the daemon env.
- R3.2 The daemon materializes/verifies `forced_login_method = "chatgpt"` in `$CODEX_HOME/config.toml`
  before the first codex run (analog of materializing `.claude/settings.json`).
- R3.3 A **pre-flight assertion** runs `codex login status` (or parses `/status`) at adapter init
  **before the first task turn** and **fails the run closed** if it does not report a ChatGPT plan —
  asserting post-spawn would be too late (a turn may have already billed the API pool). **[review
  M1]** (`04-research/auth-billing` Rec (c).)
- R3.4 `$CODEX_HOME` is set to a stable, writable path (default `~/.codex`) so token refresh works;
  the daemon does NOT spawn codex with an empty/sandboxed HOME. (`auth-billing` uncertainty #3.)
- R3.5 Documented MUST-TEST items (empirical, pre-production): does the pinned codex `exec` honor
  `OPENAI_API_KEY`/`CODEX_API_KEY`? is `forced_login_method` honored by `exec`? is there an
  auto-generated org key (#2000)? — captured as test/validation tasks, not assumptions.

**Interfaces consumed:** C2 (adds env + pre/post hooks to the codex launch).
**Depends on:** C2.

---

## C4 — Selection & config surface
**Goal trace:** G5 (selection), C4 constraint (N-1). **Code areas:** H, J.

**Responsibility:** Let a run pick its harness. **Resolution order (4 tiers):** per-bead override →
per-queue default → per-node DOT attr → global default (`claude`). Plumb the choice to
`deps.launchSpecBuilder`/registry.

**Requirements (testable):**
- R4.1 A global default-harness config exists (extends `Config`, default `claude`) so an absent
  selection resolves to claude — **N-1 back-compat** (existing beads/queues unchanged).
- R4.2 A DOT node attribute `harness` (alias `agent_runtime`) is parsed into the node attr-map
  (`dotparser.go`, `node.go`); absent → resolves to the global/queue default. Parsing is free
  (generic map) — test that a `harness=codex` node routes to the codex adapter.
- R4.3 A per-bead harness field (e.g. a `harness:codex` bead label or a queue-item field) overrides
  the default; a per-queue default sits between bead and global. The resolver is one pure function
  with a documented precedence and unit tests for each tier.
- R4.4 `Config.HandlerBinary` semantics are preserved for claude; codex's binary path is resolved by
  the codex adapter (default `codex`), not by overloading `HandlerBinary`.

**Interfaces exposed:** the harness-resolution function (bead/queue/node/global → `AgentType`).
**Depends on:** C1.

---

## C5 — Workflow / review-loop integration
**Goal trace:** G5 (DOT nodes + reviewer reference harness). **Code areas:** H, I.

**Responsibility:** Route the DOT cascade and review-loop off the resolved harness, so both the
implementer and the reviewer launch through the selected adapter.

**Requirements (testable):**
- R5.1 The cascade (`dot_cascade.go:499-525`) selects the launch-spec builder + adapter from the
  resolved harness (C4) instead of always claude; an end-to-end codex run executes implement →
  commit_gate → review → close on `standard-bead.dot` unchanged.
- R5.2 The review-loop (`reviewloop.go`) resolves the reviewer's harness. **Default:** reviewer uses
  the same harness as the implementer. **Decision to surface (see review):** whether to allow an
  independent reviewer-harness override (e.g. always-claude reviewer). Spec the default; gate the
  override behind a flag if cheap.
- R5.3 A codex run's reviewer correctly writes the `.harmonik/review.json` verdict (or codex's
  equivalent), and the existing verdict-parsing/iteration logic is reused unchanged.

**Interfaces consumed:** C4 resolver, C1 registry.
**Depends on:** C4.

---

## C6 — Migration, back-compat & test harness
**Goal trace:** G6 (filed beads, ready), C4 constraint. **Code areas:** all.

**Responsibility:** Prove N-1 safety, provide a codex twin/fake for deterministic tests, and document
the operator surface.

**Requirements (testable):**
- R6.1 A regression test proves an existing bead/queue/workflow with **no** harness selection runs on
  claude with byte-identical launch behavior to pre-change.
- R6.2 A **codex twin** (a fake `codex` binary, mirroring the existing claude-twin testing pattern)
  emits scripted JSONL (`thread.started`/`turn.completed`) + makes a `Refs:<bead>` commit, so the
  adapter is testable without a real OpenAI account or network.
- R6.3 Operator docs: how to log codex into the subscription, the env-guard posture, how to select
  codex per-bead/queue, and the MUST-TEST validation checklist from C3 — including an explicit
  **pre-production audit of the OpenAI org for a "Codex CLI (auto-generated)" API key** (#2000),
  since strip + `forced_login_method` do NOT defend a subscription-login that silently routes to an
  org key. **[review M1]**
- R6.4 The DOT/config additions are documented in the relevant `specs/` artifact (spec-first).
- R6.5 **MUST-TEST (not assumed):** verify a codex reviewer reliably writes the
  `.harmonik/review.json` verdict on instruction; if it does not, the reviewer-harness default falls
  back to claude (R5.2). **[review M3]**

**Interfaces consumed:** all prior.
**Depends on:** C3, C5.

---

## Component ↔ goal coverage matrix

| Goal (01-problem-space) | Components |
|---|---|
| G1 implicit Harness interface | C1 |
| G2 codex CLI surface characterized | C2 (+ research dim 2) |
| G3 smallest seam, named files | C1, C2 |
| G4 auth/billing + env guard | C3 |
| G5 selection + DOT + reviewer + migration | C4, C5 |
| G6 filed beads, ready, square | C6 (+ tasks pass) |

No component requirement exists that does not trace to a goal or a constraint (C1–C6 all mapped).
Component count = 6 (within the 3–7 guidance).
