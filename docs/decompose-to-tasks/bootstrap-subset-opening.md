# `hk-ahvq.41` Bootstrap-Subset Analysis — Opening / Scoping Pass

**Date:** 2026-05-05
**Status:** scoping only. Not the final subset; not the final session. Names the inputs, candidate clusters, blockers, and recommends a session shape for the dedicated `.41` run.

## 1. Approach — operational definition of "minimum foothold"

**Working definition:** the smallest set of beads whose implementation produces a daemon binary that can:

1. Start cleanly (pidfile, socket, JSONL writer, daemon-state marker), pass startup self-checks, and reach `ready`.
2. Accept one trivial bead (e.g. `kind:non-agentic` no-op) via `br`, resolve it to a static linear DOT workflow (1–2 nodes), and execute end-to-end.
3. Spawn one twin handler subprocess (`claude-twin`) inside a `git worktree`, capture the watcher event stream, commit one checkpoint with structured trailers, merge back to integration, and close the bead.
4. Survive a clean shutdown + restart with zero state loss (resume / no-op reconciliation Cat 0).

That is the "compile-and-run skeleton that can host the first real handler integration test" language in the bead description (`hk-ahvq.41` body).

**Out of scope for v0 (deferred to first self-build cycles):** Pi handler, real `claude-code` handler, sub-workflow recursion, control-points / gates beyond a trivial pass-through, freedom profiles, policy-engine guards, Cat 1–6 reconciliation, improvement loop (S09), CASS/memory, multi-run concurrency, operator pause/upgrade, agent-mail, adze. These map to specs/beads tagged `post-mvh` or `gated-by-corpus-scale`, and most of `hk-63oh` (RC) and `hk-sx9r` (ON) by design (`docs/foundation/core-scope.md` §"Ground rules"; `docs/bootstrap.md` §2 MVH cut).

## 2. Inputs available

- **Bead corpus:** ~639 child beads + ~1700 edges, zero cycles (HANDOFF.md §"Phase-0 exit started"). Queryable via `br list`, `br show <epic>`, `br list -l <label>`, `br list --title-contains`.
- **Spec corpus:** all 10 specs `reviewed` at `specs/` (STATUS.md §"Spec corpus inventory").
- **Companion plan:** `docs/bootstrap.md` §5 already proposes a 10-step build order (S06 → S03 → S04 → twin → S01 → S05 → S02 → S07 → S08 → Pi).
- **Alignment record:** `docs/foundation/core-scope.md` §1–10, the 10-section walkthrough — most authoritative MVH cut.
- **Pilot yamls + narratives** at `docs/decompose-to-tasks/{ar,em,ev,hc,cp,wm,pl,on,rc,bi}-pilot{,-data.yaml}`.

**Input gaps blocking the analysis:**

- **No `scope:mvh` / `scope:bootstrap` label** exists on beads (`br label list` confirms). Only `post-mvh` is present (5 hits). Negative-space inference (everything not `post-mvh`) is too coarse — most reviewed beads are MVH-scoped already.
- **No `req:` → spec-section pre-classification** by bootstrap tier. Bead descriptions cite §-numbers but no per-bead "tier" axis.
- **Discipline `Axes:` taxonomy** (4 axes — idempotency / io-determinism / llm-freedom / replay-safety) is orthogonal to bootstrap-tier.

## 3. Candidate subsystem clusters (3–6, first-pass)

Cluster ↔ epic IDs verified via `br epic status`:

**Cluster A — Process skeleton (INCLUDE).** Epic `hk-8mup` (PL, 59 beads). Pidfile + socket + startup steps 0–9 + 8a marker-read + daemon-state + JSONL writer. Includes `hk-8mup.10` (deterministic startup), `.43` (8 entry-point command surface), `.44`–`.45` (one-daemon + deterministic-daemon sensors), `.49` (`DaemonStatus` ENUM). Roughly the entire epic minus `.51` (full crash harness) and the orphan-sweep coverage that pulls in WM/RC/BI cross-cutters. **Estimate: ~30–40 of PL's 59.**

**Cluster B — Workspace + checkpoint substrate (INCLUDE).** Epic `hk-8mwo` (WM, 71 beads) + selective slice of `hk-b3f` (EM, 88 beads). Need: `worktree add` lifecycle, lease per-run, branch naming, sidecar `.harmonik/transitions/<id>.json`, structured-trailer commit (`Checkpoint` record `hk-b3f.78`, trailer registry `.85`), failed-transition no-commit rule (`.32`). **Estimate: ~25–30 WM + ~20–25 EM = ~50.** Largest cluster. The §3 + §5 walkthrough alignment confirms these are foundational.

**Cluster C — Handler interface + twin (INCLUDE).** Epic `hk-8i31` (HC, 80 beads) — the twin-related slice (`.42`–`.47`, `.53`, `.59`, `.65`, `.77`) plus the `Handler` interface, `Launch(LaunchSpec)`, watcher goroutine event surface, `agent_ready`. Skip everything Pi-related, skill-injection beyond `beads-cli`, and rate-limit sophistication. **Estimate: ~20–25 of HC's 80.**

**Cluster D — Event-bus skeleton (INCLUDE).** Epic `hk-hqwn` (EV, 63 beads) — the in-process pub/sub + JSONL writer + ~10 event types load-bearing for the end-to-end happy path: `checkpoint_written`, `agent_started/output_chunk/completed/failed`, `workspace_leased`, `bead_terminal_transition_recovered` (post-MVH — defer), `daemon_startup_failed`. **Estimate: ~15–20 of EV's 63.**

**Cluster E — Beads adapter (INCLUDE).** Epic `hk-872` (BI, 54 beads) — `br --version` handshake (`.26`), idempotent write paths, BI-INV-001..003 sensors. Excludes orphan-`br`-subprocess sweep edges that loop into PL/RC. **Estimate: ~15 of BI's 54.**

**Cluster F — Static workflow execution (INCLUDE, narrow).** Epic `hk-b3f` (EM) — slice covering linear `RunState` machine, node dispatch (`hk-b3f.40` active-run discovery, plus the `Outcome` `kind` discriminator + `OutcomeKind` enum from EM v0.3.3). Skip sub-workflow recursion (`hk-b3f.46`–`.47`), control-point evaluation, revision loops. **Estimate: ~20 of EM's 88.**

**Clusters explicitly DEFERRED:**
- **`hk-zs0` (AR, 54 beads)** — declarations / cross-cutting principles. Mostly satisfied by structural conformance of A–F; few beads require their own implementation tasks. Include sensor beads only (`zs0.41`, `.50`).
- **`hk-a8bg` (CP, 85 beads)** — control-points are post-skeleton.
- **`hk-sx9r` (ON, 84 beads)** — operator-NFR is the operator-control surface; pause/stop/upgrade are between-task features (`bootstrap.md` §4) — defer all but startup-failure catalog `.4` and queue-schema version-check `.20`.
- **`hk-63oh` (RC, 79 beads)** — reconciliation is non-core for MVH per `core-scope.md` §"Ground rules". Cat 0 (no-op resume) only; everything else is first-self-build-cycle territory.

**Rough first-pass estimate: ~150–180 beads** out of the ~639 corpus form the bootstrap subset. Counting is approximate until the dedicated session enumerates by ID.

## 4. Open questions

**Needs user input:**
- **Q1.** Is the §1 "working definition" of foothold the right end-to-end scenario? Specifically: should it require a `claude-twin` round-trip (per `bootstrap.md` step 4) or is a pure non-agentic node sufficient?
- **Q2.** Pi handler in or out of bootstrap subset? `bootstrap.md` §2 leaves it open; `core-scope.md` says post-MVH explicitly. Confirm post-MVH = out of `.41`.
- **Q3.** Acceptance criterion for `.41` output: a markdown doc with a flat list of bead IDs, or a labelled subset (`scope:bootstrap` label on each bead via `br update`) that lets `br ready` drive the work? The latter is operationally useful but writes back to the corpus.
- **Q4.** Does scenario-harness (S07) skeleton sit in bootstrap subset, or is the first scenario authored as code in the first self-build cycle? `bootstrap.md` step 8 puts it in MVH.

**Needs further analysis (not user):**
- **A1.** Per-cluster bead enumeration with explicit include/exclude for each. Requires walking each pilot yaml to filter by §4-section tier.
- **A2.** Cross-cluster edge traversal: which Cluster B beads pull in Cluster D events transitively, and is the closure still bounded?
- **A3.** The "implementation order" question (separate from "subset" question) — clusters' implementation order likely follows `bootstrap.md` §5 step-1..8, but bead-level ordering inside a cluster needs its own pass.
- **A4.** Rounding the discipline-lane backlog: 13 pending findings (HANDOFF.md §"Discipline-patch lane batch") may surface bead-level corrections before `.41` lands; check whether any of those touch the bootstrap-cluster beads.

## 5. Session-shape recommendation

**Recommended shape: 3-pass split, not a single session.**

- **Pass 1 — Resolve user input (Q1–Q4).** Short interactive turn with the user, ~30 min. Produces the acceptance contract.
- **Pass 2 — Per-cluster enumeration (autonomous).** One agent per cluster (A–F), parallel. Each agent reads its pilot yaml + spec sections, emits a bead-ID list with rationale, flags edge dependencies into other clusters. Produces 6 cluster-subset reports. Inputs: pilot yamls, spec sections, `br show` for cited beads, the §1 working definition. Skill: `beads-cli`. ~2–3 hours wall, ~6 agents.
- **Pass 3 — Synthesis + edge-closure (interactive or autonomous).** Single agent merges the 6 reports, runs cross-cluster edge closure, produces the final `bootstrap-subset.md` with bead IDs, rationale, and (if Q3 = label) a `br update --label scope:bootstrap` script. Reviewer agent checks closure completeness. ~1–2 hours.

**Rationale for split, not single-session:** The full enumeration is ~150–180 beads with ~400+ relevant edges to walk. Single-agent context (even 1M) gets lossy past ~80 beads of detailed reasoning, and the per-cluster agents can apply spec-area expertise (HC-pilot-trained reasoning differs from EV-pilot-trained reasoning). Parallelism saves wall-clock. Cluster boundaries are stable (HANDOFF.md confirms zero corpus-wide cycles), so closure runs cheaply.

**Anti-pattern to avoid:** running `.41` as a single deep-dive without the user-input pass (Q1–Q4). Q1 alone changes whether ~30 HC beads are in or out.
